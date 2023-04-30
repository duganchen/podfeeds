package main

import (
	"bytes"
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
	"time"

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
	// We don't care about FeedLink. It's a link to the XML file.
	ToC []ToCEntry
}

type CachedPage struct {
	LastModified time.Time
	ETag         string

	// List from here.
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/If-None-Match
	CacheControl    string
	ContentLocation string
	Date            string
	Expires         string
	Vary            string
}

var (
	indexTemplate   *template.Template
	podcastTemplate *template.Template
)

func init() {
	indexTemplate = template.Must(template.ParseFiles("./templates/index.html"))
	podcastTemplate = template.Must(template.ParseFiles("./templates/podcast.html"))
}

// URL to cached page
var pageCache = make(map[string]CachedPage)

func FetchSubscription(feed string) (Subscription, error) {
	// We have the URL. We just need the title.

	resp, err := http.Get(feed)
	if err != nil {
		return Subscription{}, err
	}

	fp := gofeed.NewParser()

	parsed, err := fp.Parse(resp.Body)
	if err != nil {
		return Subscription{}, err
	}

	fmt.Println("Loaded", feed)
	return Subscription{parsed.Title, "/podcast?url=" + url.QueryEscape(feed)}, nil

}

func FetchSubscriptionAsync(feed string, feedIndexes map[string]int, mutex *sync.Mutex, Subscriptions []Subscription) func() error {
	return func() error {
		subscription, err := FetchSubscription(feed)
		mutex.Lock()
		Subscriptions[feedIndexes[feed]] = subscription
		mutex.Unlock()
		return err
	}
}

// Only allow one podcasts-caching thread a a time
var podcastCachingChannel = make(chan int, 1)

func SetupWatcher(watcher *fsnotify.Watcher) {
	watcher.Add("./podcasts.yaml")
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Chmod == fsnotify.Chmod {
					go CacheSubscriptions()
				}
			case err := <-watcher.Errors:
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	}()
}

func CacheSubscriptions() {
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

	var mutex sync.Mutex
	subscriptions := make([]Subscription, len(feeds))
	g := new(errgroup.Group)
	for _, feed := range feeds {
		g.Go(FetchSubscriptionAsync(feed, indexes, &mutex, subscriptions))
	}

	err = g.Wait()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Writing index.html")

	buff := new(bytes.Buffer)
	indexTemplate.Execute(buff, subscriptions)

	err = os.WriteFile("./index.html", buff.Bytes(), 0644)
	if err != nil {
		log.Fatal(err)
	}
	podcastCachingChannel <- 1
}

// Going by this:
// https://dev.to/theghostmac/understanding-and-crafting-http-middlewares-in-go-3183
func serverSideCache(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	})
}

func main() {

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	go SetupWatcher(watcher)
	defer watcher.Close()

	podcastCachingChannel <- 1
	go CacheSubscriptions()

	// Why not just have a global parser
	g_fp := gofeed.NewParser()

	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir("modest/css"))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	pageCacheMutex := sync.Mutex{}

	http.Handle("/podcast", serverSideCache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		url := r.URL.Query().Get("url")

		if url == "" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		pageCacheMutex.Lock()
		oldCachedPage, ok := pageCache[r.URL.String()]
		pageCacheMutex.Unlock()
		if ok {
			etag := r.Header.Get("If-None-Match")
			if etag != "" && etag == oldCachedPage.ETag {
				w.Header().Set("Etag", etag)

				if oldCachedPage.CacheControl != "" {
					w.Header().Set("Cache-Control", oldCachedPage.CacheControl)
				}

				if oldCachedPage.ContentLocation != "" {
					w.Header().Set("Content-Location", oldCachedPage.ContentLocation)
				}

				if oldCachedPage.Date != "" {
					w.Header().Set("Date", oldCachedPage.Date)
				}

				if oldCachedPage.Expires != "" {
					w.Header().Set("Expires", oldCachedPage.Expires)
				}

				if oldCachedPage.Vary != "" {
					w.Header().Set("Vary", oldCachedPage.Vary)
				}
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		var client http.Client
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		/*
			for _, reqCacheHeader := range []string{"cache-control", "if-modified-since", "if-none-match", "if-match"} {
				reqHeader := r.Header.Get(reqCacheHeader)
				if reqHeader != "" {
					r.Header.Set(reqCacheHeader, reqHeader)
				}
			}
		*/

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		parsed, err := g_fp.Parse(resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		var podcast Podcast
		podcast.Language = parsed.Language

		podcast.Title = parsed.Title
		podcast.Description = parsed.Description

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
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		cache := false
		newCachedPage := CachedPage{}

		// Pass the upstream caching headers to the browser. This should be enough for speed optimization.
		for _, header := range []string{"Etag", "Last-Modified", "Cache-Control", "Expires", "Content-Location", "Date", "Vary"} {
			respHeader := resp.Header.Get(header)
			if respHeader != "" {
				w.Header().Set(header, respHeader)
			}

			if header == "Etag" {
				cache = true
				newCachedPage.ETag = respHeader
			}

			if header == "Last-Modified" {
				cache = true
				lastModified, err := http.ParseTime(respHeader)
				if err == nil {
					newCachedPage.LastModified = lastModified
				}
			}

			if header == "Cache-Control" {
				newCachedPage.CacheControl = respHeader
			}

			if header == "Expires" {
				newCachedPage.Expires = respHeader
			}

			if header == "Content-Location" {
				newCachedPage.ContentLocation = respHeader
			}

			if header == "Date" {
				newCachedPage.Date = respHeader
			}

			if header == "Vary" {
				newCachedPage.Vary = respHeader
			}
		}

		if cache {
			pageCacheMutex.Lock()
			pageCache[r.URL.String()] = newCachedPage
			pageCacheMutex.Unlock()
		}

		w.Write(pageBuilder.Bytes())

		/*
			// These responses can be large, and I'm finding that there can be a delay even when Squid has a
			// cache hit. What if we gzip them?
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "text/html")
			gw := gzip.NewWriter(w)
			defer gw.Close()

			gw.Write(pageBuilder.Bytes())
		*/
	})))

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
		fmt.Printf("Using port: %d\n", port)
		if err := http.Serve(l, nil); err != nil {
			panic(err)
		}
	} else {
		fmt.Println("Using port: 8080")
		log.Fatal(http.ListenAndServe(port, nil))
	}
}
