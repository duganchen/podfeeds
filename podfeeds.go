package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"fmt"
	"io/ioutil"
	"log"
	"os"

	"html/template"
	"net/http"
	"net/url"

	"github.com/mmcdole/gofeed"
	"gopkg.in/yaml.v3"
)

/*
Chrome:

For an etag value of aaa:
If-None-Match [aaa]

For "Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT"
We get
If-Modified-Since [Wed, 21 Oct 2015 07:28:00 GMT]

If both are set, then Chome sets both

Lynx just caches unless you explictly specify no cache
(x on a link, or C-r)

The Go client just sends these by default:
	User-Agent [Go-http-client/1.1]
	Accept-Encoding [gzip]

*/

type Subscription struct {
	Title string
	Url string
}

type Subscriptions struct {
	Subscriptions []Subscription
}

type Metadata struct {
	Key string
	Value string
}

type Enclosure struct {
	URL string
	Type string
}

type Podcast struct {
	Language string
	FeedLink string
	ImageURL string
	ImageTitle string
	Enclosures []string
	Metadata []Metadata
}

type Page struct {
	URL string
	ETag string
	LastModified string
	HTML []byte
}

func main() {
	
	database, err := sql.Open("sqlite3", "./cache.sqlite3")
	if (err != nil) {
		log.Fatal(err)
	}

	statement, err := database.Prepare("CREATE TABLE IF NOT EXISTS Pages (URL TEXT PRIMARY KEY, ETag Text, LastModified TEXT, HTML BLOB)")
	if (err != nil) {
		log.Fatal(err)
	}
	_, err = statement.Exec()
	if (err != nil) {
		log.Fatal(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		/*

		Okay, how to do this...

		Check for "/" in the cache.
		If it exists, check its modification time.

		If it exists in the cache and its modification time matches the
		subscriptions list, then return what's cached.

		Otherwise, clear the cache, and rebuild it by fetching and rendering
		all the pages including this one.

		*/

		// stat, err := os.Stat("./podcasts.yaml")
		// if (err != nil) {
		// 	log.Fatal(err)
		// }

		// Check syntax when there's actually something here
		// cached, err := database.Query("SELECT * FROM Pages WHERE URL = '/'")

		// Okay, I guess we need to loop through this manually.


		// mtime := stat.ModTime().Format((http.TimeFormat))

		
		// Look. We know there's nothing in the cache yet. Keep going.

		// Print out all the headers
		// https://stackoverflow.com/a/47557484/240515
		for name, values := range r.Header {
			// Loop over all values for the name.
			for _, value := range values {
				fmt.Println(name, value)
			}
		}

		feeds := make([]string, 0)

		stat, err := os.Stat("./podcasts.yaml")
		if (err != nil) {
			log.Fatal(err)
		}

		buf, err := ioutil.ReadFile("./podcasts.yaml")

		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		err = yaml.Unmarshal(buf, &feeds)

		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		subscriptions := Subscriptions{}

		// Can we use this more than once?
		fp := gofeed.NewParser()

		// Seriously, just don't let the user enter duplicate feeds.
		seen := make(map[string]bool)

		for _, feed := range feeds {
			if seen[feed] {
				http.Error(w, "Duplicate feed", 500)
			}
			seen[feed] = true

			parsed, err := fp.ParseURL(feed)
			if err != nil {
				http.Error(w, err.Error(), 500)
			}
			
			subscription := Subscription{parsed.Title, "/podcast?url=" + url.QueryEscape(feed)}
			subscriptions.Subscriptions = append(subscriptions.Subscriptions, subscription)
		}


		t, _ := template.ParseFiles("./templates/index.html")
		buff := new(bytes.Buffer)
		writer := gzip.NewWriter(buff)
		t.Execute(writer, subscriptions)
		writer.Close()


		// ONLY DO THIS ONCE!!!!

		statement, err := database.Prepare("INSERT INTO Pages VALUES (?, ?, ?, ?)")
		if (err != nil) {
			log.Fatal(err)
		}
		statement.Exec("/", "", stat.ModTime().Format(http.TimeFormat), buff.Bytes())

		encodings := r.Header["Accept-Encoding"]
		compress := len(encodings) > 0 && strings.Contains(encodings[0], "gzip")


		if compress {
			fmt.Println("Compressed")
			w.Header().Add("Content-Encoding", "gzip")
			w.Write(buff.Bytes())
		} else {
			fmt.Println("NOt compressed")
			reader, err := gzip.NewReader(buff)
			if err != nil {
				log.Fatal(err)
			}

			body, err := ioutil.ReadAll(reader)
			if err != nil {
				log.Fatal(err)
			} 
			w.Write(body)
		}

	})

	http.HandleFunc("/podcast", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Query().Get("url")

		if url == "" {
			http.Error(w, "Missing parameter 'url'", 400)
		}
		fmt.Fprint(w, url)
	})

	port, set := os.LookupEnv("PORT")
	if !set {
		port = "8080"
	}
	port = fmt.Sprintf(":%v", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
	
