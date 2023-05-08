package main

import (
	"bytes"
	"compress/gzip"
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
	// We don't care about FeedLink. It's a link to the XML file.
	ToC []ToCEntry
}

var (
	indexTemplate   *template.Template
	podcastTemplate *template.Template
)

func init() {
	indexTemplate = template.Must(template.ParseFiles("./templates/index.html"))
	podcastTemplate = template.Must(template.ParseFiles("./templates/podcast.html"))
}

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

	podcasts := "./podcasts.yaml"
	_, err := os.Stat(podcasts)
	if err != nil {
		podcasts = "/etc/podfeeds/podcasts.yaml"
	}
	watcher.Add(podcasts)

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

	podcasts := "./podcasts.yaml"
	_, err := os.Stat(podcasts)
	if err != nil {
		podcasts = "/etc/podfeeds/podcasts.yaml"
	}

	buf, err := ioutil.ReadFile(podcasts)
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
	fmt.Println("Writing /tmp/podfeeds/index.html")

	buff := new(bytes.Buffer)
	indexTemplate.Execute(buff, subscriptions)

	err = os.WriteFile("/tmp/podfeeds/index.html", buff.Bytes(), 0644)
	if err != nil {
		log.Fatal(err)
	}
	podcastCachingChannel <- 1
}

func main() {

	// We just leave this directory around. Sorry.
	// https://groups.google.com/g/golang-nuts/c/vDd72SHUnbQ/m/Kj0xOa0AAQAJ
	_, err := os.Stat("/tmp/podfeeds")
	if err != nil {
		os.MkdirAll("/tmp/podfeeds", os.ModePerm)
	}

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
		http.ServeFile(w, r, "/tmp/podfeeds/index.html")
	})

	http.HandleFunc("/podcast", func(w http.ResponseWriter, r *http.Request) {

		url := r.URL.Query().Get("url")

		if url == "" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var client http.Client
		fmt.Println("url is ", url)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		etag := r.Header.Get("If-None-Match")
		if etag != "" {
			fmt.Println("Setting request etag to ", etag)
			req.Header.Set("If-None-Match", etag)
		}

		ifModifiedSince := r.Header.Get("If-Modified-Since")
		if ifModifiedSince != "" {
			req.Header.Set("If-Modified-Since", ifModifiedSince)
		}

		resp, err := client.Do(req)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Pass the upstream caching headers to the browser. This should be enough for speed optimization.
		for _, header := range []string{"Etag", "Last-Modified", "Cache-Control", "Expires", "Content-Location", "Date", "Vary"} {
			respHeader := resp.Header.Get(header)
			if respHeader != "" {
				w.Header().Set(header, respHeader)
			}
		}

		encodings := r.Header["Accept-Encoding"]
		compress := len(encodings) > 0 && strings.Contains(encodings[0], "gzip")

		if compress {
			fmt.Println("Compressing")
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}

		// Headers apparently need to be set before this.
		w.WriteHeader(resp.StatusCode)

		if resp.StatusCode == http.StatusNotModified {
			return
		}

		// This should handle cases where there are issues with the feed.
		// iI actually haven't tested it yet.
		if resp.StatusCode != http.StatusOK {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				http.Error(w, err.Error(), resp.StatusCode)
			}
			w.Write(body)
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

		if compress {
			gw := gzip.NewWriter(w)
			defer gw.Close()
			gw.Write(pageBuilder.Bytes())
		} else {
			w.Write(pageBuilder.Bytes())
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
		fmt.Printf("Using port: %d\n", port)
		if err := http.Serve(l, nil); err != nil {
			panic(err)
		}
	} else {
		fmt.Println("Using port:", port)
		log.Fatal(http.ListenAndServe(port, nil))
	}
}
