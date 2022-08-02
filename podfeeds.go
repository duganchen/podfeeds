package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"text/template"

	"github.com/fsnotify/fsnotify"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/sync/errgroup"

	"github.com/mmcdole/gofeed"
	"gopkg.in/yaml.v3"
)

type Subscription struct {
	Title string
	Url   string
}

type Metadata struct {
	Key   string
	Value string
}

type Enclosure struct {
	URL  string
	Type string
}

type Item struct {
	Enclosures  []Enclosure
	Metadata    []Metadata
	Title       string
	Description string
	GUID        string
}

type ToCEntry struct {
	GUID  string
	Title string
}

type Podcast struct {
	Title       string
	Description string
	Language    string
	Items       []Item
	Metadata    []Metadata
	// We don't care about FeedLink. It's a link to the XML file.
	ToC []ToCEntry
}

var (
	indexTemplate   *template.Template
	podcastTemplate *template.Template
)

// Start of the page cache API, which is a key-value store mapping feed URLs to StoredPages.

// A podcast page, rendered and stored in the cache.
type Page struct {
	ETag         string
	LastModified string
	HTML         []byte // Yes, they're stored gzipped.
}

type PageCache interface {
	Get(string) (Page, error)
	Set(string, Page) error
	Clear() error
	Erase(string) error
}

type SQLiteCache struct {
	db *sql.DB
}

func NewSQLiteCache() (SQLiteCache, error) {
	db, err := sql.Open("sqlite3", "./cache.sqlite3")
	if err != nil {
		return SQLiteCache{}, nil
	}

	statement, err := db.Prepare("CREATE TABLE IF NOT EXISTS Pages (URL TEXT PRIMARY KEY, ETag Text, LastModified TEXT, HTML BLOB)")
	if err != nil {
		return SQLiteCache{}, err
	}
	_, err = statement.Exec()
	if err != nil {
		return SQLiteCache{}, err
	}
	defer statement.Close()

	return SQLiteCache{db}, nil
}

func (cache SQLiteCache) Get(feed string) (Page, error) {
	statement, err := cache.db.Prepare("SELECT * FROM Pages WHERE URL = ?")
	if err != nil {
		return Page{}, err
	}
	row := statement.QueryRow(feed)
	defer statement.Close()
	var page Page
	var url string
	err = row.Scan(&url, &page.ETag, &page.LastModified, &page.HTML)
	if err == sql.ErrNoRows {
		return page, err
	}
	return page, nil
}

func (cache SQLiteCache) Set(feed string, page Page) error {
	statement, err := cache.db.Prepare("INSERT INTO Pages VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer statement.Close()

	_, err = statement.Exec(feed, page.ETag, page.LastModified, page.HTML)
	if err != nil {
		return err
	}

	return nil
}

func (cache SQLiteCache) Erase(feed string) error {
	stmt, err := cache.db.Prepare("DELETE FROM Pages WHERE URL = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(feed)
	return err
}

func (cache SQLiteCache) Clear() error {
	_, err := cache.db.Exec("DELETE FROM Pages")
	return err
}

func CloseSQLiteCache(cache SQLiteCache) {
	cache.db.Close()
}

// End of cache API

type FetchedInfo struct {
	Page         Page
	Subscription Subscription
}

func init() {
	indexTemplate = template.Must(template.ParseFiles("./templates/index.html"))
	podcastTemplate = template.Must(template.ParseFiles("./templates/podcast.html"))
}

func FetchPage(feed string) (FetchedInfo, error) {
	resp, err := http.Get(feed)
	if err != nil {
		return FetchedInfo{Page{}, Subscription{}}, err
	}

	fp := gofeed.NewParser()

	parsed, err := fp.Parse(resp.Body)
	if err != nil {
		return FetchedInfo{Page{}, Subscription{}}, err
	}

	var podcast Podcast
	podcast.Language = parsed.Language

	podcast.Title = parsed.Title
	podcast.Description = parsed.Description

	if parsed.Updated != "" {
		podcast.Metadata = append(podcast.Metadata, Metadata{"Updated", parsed.Updated})
	}

	if parsed.Published != "" {
		podcast.Metadata = append(podcast.Metadata, Metadata{"Published", parsed.Published})
	}

	var authorsBuilder strings.Builder
	for _, author := range parsed.Authors {
		name := strings.TrimSpace(author.Name)
		if name != "" {
			authorsBuilder.WriteString(name)
		}

		email := strings.TrimSpace(author.Email)
		if email != "" {
			authorsBuilder.WriteString(" (")
			authorsBuilder.WriteString(email)
			authorsBuilder.WriteString(") ")
		}
	}

	authors := strings.TrimSpace(authorsBuilder.String())
	if authors != "" {
		podcast.Metadata = append(podcast.Metadata, Metadata{"Authors", authors})
	}

	// We don't present categories. They're for search engines to look at, not for
	// end users to look at.

	// Don't bother with Copyright. If it's there, it gets rendered as
	// Copyright	Copyright © CBC 2022

	// Don't need the 'Generator'. It's what was used to generate the feed.

	for _, parsedItem := range parsed.Items {
		var item Item
		item.Description = parsedItem.Description
		item.Title = parsedItem.Title

		item.GUID = parsedItem.GUID

		podcast.ToC = append(podcast.ToC, ToCEntry{item.GUID, item.Title})

		for _, enclosure := range parsedItem.Enclosures {
			item.Enclosures = append(item.Enclosures, Enclosure{enclosure.URL, enclosure.Type})
		}

		if parsedItem.Updated != "" {
			item.Metadata = append(item.Metadata, Metadata{"Updated", parsedItem.Updated})
		}

		if parsedItem.Published != "" {
			item.Metadata = append(item.Metadata, Metadata{"Published", parsedItem.Published})
		}

		// Skipping "Content". In the feed where I saw it, it has the same content as the
		// description.

		if len(parsedItem.Authors) > 0 {
			var authorsBuilder strings.Builder
			for _, author := range parsedItem.Authors {
				if author.Name != "" {
					authorsBuilder.WriteString(author.Name)
				}

				if author.Name != "" && author.Email != "" {
					authorsBuilder.WriteString(" (")
				}

				if author.Email != "" {
					authorsBuilder.WriteString(author.Email)
				}

				if author.Name != "" && author.Email != "" {
					authorsBuilder.WriteString(")")
				}

				authorsBuilder.WriteString(" ")
			}
			item.Metadata = append(item.Metadata, Metadata{"Authors", authorsBuilder.String()})
		}

		podcast.Items = append(podcast.Items, item)
	}

	if len(podcast.ToC) == 1 {
		podcast.ToC = nil
	}

	var pageBuilder bytes.Buffer
	err = podcastTemplate.Execute(&pageBuilder, podcast)
	if err != nil {
		return FetchedInfo{Page{}, Subscription{}}, err
	}

	page := new(bytes.Buffer)
	writer := gzip.NewWriter(page)
	_, err = writer.Write(pageBuilder.Bytes())
	if err != nil {
		return FetchedInfo{Page{}, Subscription{}}, err
	}
	writer.Close()

	etags := resp.Header[http.CanonicalHeaderKey("ETag")]
	var etag string
	if len(etags) > 0 {
		etag = etags[0]
	} else {
		etag = ""
	}

	lastModifieds := resp.Header[http.CanonicalHeaderKey("Last-Modified")]
	var lastModified string
	if len(lastModifieds) > 0 {
		lastModified = lastModifieds[0]
	} else {
		lastModified = ""
	}

	var value Page
	value.ETag = etag
	value.HTML = page.Bytes()
	value.LastModified = lastModified
	var subscription Subscription
	subscription.Url = feed
	subscription.Title = podcast.Title
	return FetchedInfo{value, subscription}, nil
}

func FetchPageAsync(feed string, feedIndexes map[string]int, mutex *sync.Mutex, fetchedInfos []FetchedInfo) func() error {
	return func() error {
		fetchedInfo, err := FetchPage(feed)
		mutex.Lock()
		fetchedInfos[feedIndexes[feed]] = fetchedInfo
		mutex.Unlock()
		return err
	}
}

func WriteResponse(w http.ResponseWriter, body []byte, r *http.Request) error {
	encodings := r.Header["Accept-Encoding"]
	compress := len(encodings) > 0 && strings.Contains(encodings[0], "gzip")

	if compress {
		w.Header().Add("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(body)
	} else {
		byteReader := bytes.NewReader(body)
		reader, err := gzip.NewReader(byteReader)
		if err != nil {
			return err
		}
		defer reader.Close()
		body, err := ioutil.ReadAll(reader)
		if err != nil {
			return err
		}
		w.Write(body)
	}

	return nil
}

// Only allow one podcasts-caching thread a a time
var podcastCachingChannel = make(chan int, 1)

func SetupWatcher(cache PageCache, watcher *fsnotify.Watcher) {
	// The synchronization is: cache podcasts on program start, then set up a watcher
	// to recache every time podcasts.yaml changes.

	<-podcastCachingChannel
	watcher.Add("podcasts.yaml")
	podcastCachingChannel <- 1
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					go CachePodcasts(cache)
				}
			case err := <-watcher.Errors:
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	}()
}

func CachePodcasts(cache PageCache) {
	// Note that errors thrown here crash the server.

	<-podcastCachingChannel
	feeds := make([]string, 0)

	buf, err := ioutil.ReadFile("./podcasts.yaml")
	if err != nil {
		log.Fatal(err)
	}
	err = yaml.Unmarshal(buf, &feeds)

	if err != nil {
		log.Fatal(err)
	}

	// Used to reassemble the podcasts in their original order
	indexes := make(map[string]int)
	for i, feed := range feeds {
		// Seriously, just don't let the user enter duplicate feeds.
		if indexes[feed] > 0 {
			log.Fatal("Duplicate feed")
		}
		indexes[feed] = i
	}

	cache.Clear()
	if err != nil {
		log.Fatal(err)
	}

	var subscriptions []Subscription

	var mutex sync.Mutex
	fetchedInfos := make([]FetchedInfo, len(feeds))
	g := new(errgroup.Group)
	for _, feed := range feeds {
		g.Go(FetchPageAsync(feed, indexes, &mutex, fetchedInfos))
	}

	err = g.Wait()
	if err != nil {
		log.Fatal(err)
	}

	for _, fetchedInfo := range fetchedInfos {
		subscription := Subscription{fetchedInfo.Subscription.Title, "/podcast?url=" + url.QueryEscape(fetchedInfo.Subscription.Url)}
		subscriptions = append(subscriptions, subscription)

		err = cache.Set(fetchedInfo.Subscription.Url, fetchedInfo.Page)
		if err != nil {
			log.Fatal(err)
		}

	}

	buff := new(bytes.Buffer)
	writer := gzip.NewWriter(buff)
	indexTemplate.Execute(writer, subscriptions)
	writer.Close()

	stat, err := os.Stat("./podcasts.yaml")
	if err != nil {
		log.Fatal(err)
	}

	var index Page
	index.ETag = ""
	index.LastModified = stat.ModTime().Format(http.TimeFormat)
	index.HTML = buff.Bytes()

	err = cache.Set("/", index)
	if err != nil {
		log.Fatal(err)
	}
	podcastCachingChannel <- 1
}

func main() {
	cache, err := NewSQLiteCache()
	if err != nil {
		log.Fatal(err)
	}
	defer CloseSQLiteCache(cache)

	// Build the page cache when we start. If necessary.
	podcastCachingChannel <- 1
	go CachePodcasts(cache)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	go SetupWatcher(cache, watcher)
	defer watcher.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		page, err := cache.Get("/")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = WriteResponse(w, page.HTML, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	http.HandleFunc("/podcast", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Query().Get("url")

		if url == "" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		page, err := cache.Get(url)
		if err == sql.ErrNoRows {
			http.Error(w, "Podcast URL not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var client http.Client
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Chrome sends both if it can.

		if page.ETag != "" {
			req.Header.Add("If-None-Match", page.ETag)
		}

		if page.LastModified != "" {
			req.Header.Add("If-Modified-Since", page.LastModified)
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if resp.StatusCode != http.StatusNotModified {
			err = cache.Erase(url)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			fetchedInfo, err := FetchPage(url)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			page = Page{fetchedInfo.Page.ETag, fetchedInfo.Page.LastModified, fetchedInfo.Page.HTML}
			err = cache.Set(url, page)

			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		err = WriteResponse(w, page.HTML, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	port, set := os.LookupEnv("PORT")

	if set {
		port = fmt.Sprintf(":%v", port)
	} else {
		port = ":8080"
	}

	/* The Port 0 implementation is from here:
	https://youtu.be/bYSo78dwgH8
	*/
	if port == ":0" {
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			panic(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		fmt.Printf("Using port: %d", port)
		if err := http.Serve(l, nil); err != nil {
			panic(err)
		}
	} else {
		fmt.Print("Using port: 8080")
		log.Fatal(http.ListenAndServe(port, nil))
	}
}
