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

type Item struct {
	Enclosures []Enclosure
	Metadata []Metadata
	Title string
	Description string
	ImageTitle string
	ImageURL string
}

type Podcast struct {
	Title string
	Description string
	Language string
	FeedLink string
	ImageURL string
	ImageTitle string
	Items []Item
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

		stat, err := os.Stat("./podcasts.yaml")
		if (err != nil) {
			log.Fatal(err)
		}

		feeds := make([]string, 0)

		buf, err := ioutil.ReadFile("./podcasts.yaml")

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = yaml.Unmarshal(buf, &feeds)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Seriously, just don't let the user enter duplicate feeds.
		seen := make(map[string]bool)
		for _, feed := range feeds {
			if seen[feed] {
				http.Error(w, "Duplicate feed", http.StatusInternalServerError)
				return
			}
			seen[feed] = true
		}

		statement, err  := database.Prepare("SELECT * FROM Pages WHERE URL = ?")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer statement.Close()


		row := statement.QueryRow("/")
		var index Page
		err = row.Scan(&index.URL, &index.ETag, &index.LastModified, &index.HTML)

		recache := false
		if (err == sql.ErrNoRows) {
			recache = true
		} else if index.LastModified != stat.ModTime().Format(http.TimeFormat) {
			recache = true
		}

		if recache {


			_, err := database.Query("DELETE FROM Pages")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			subscriptions := Subscriptions{}

			// Can we use this more than once?
			fp := gofeed.NewParser()
	
			// Seriously, just don't let the user enter duplicate feeds.
			seen := make(map[string]bool)
	
			fmt.Println("Looping through feeds")
	
			for _, feed := range feeds {

				resp, err := http.Get(feed)
				if err != nil {
					http.Error(w, err.Error(), 500)
				}
				parsed, err := fp.Parse(resp.Body)
				if err != nil {
					http.Error(w, err.Error(), 500)
				}
	
				subscription := Subscription{parsed.Title, "/podcast?url=" + url.QueryEscape(feed)}
				subscriptions.Subscriptions = append(subscriptions.Subscriptions, subscription)
	
				// TODO: Rendering and caching the feed page goes here.
				var podcast Podcast
				podcast.Language = parsed.Language
				podcast.FeedLink = parsed.FeedLink
				podcast.ImageURL = parsed.Image.URL
				podcast.ImageTitle = parsed.Image.Title
				podcast.Title = parsed.Title
				podcast.Description = parsed.Description
	
				if parsed.Updated != "" {
					podcast.Metadata = append(podcast.Metadata, Metadata{"Updated", parsed.Updated})
				}
	
				if parsed.Published != "" {
					podcast.Metadata = append(podcast.Metadata, Metadata{"Published", parsed.Published})
				}
	
				if len(parsed.Authors) > 0 {
					var authorsBuilder strings.Builder
					for _, author := range parsed.Authors {
						authorsBuilder.WriteString(author.Name)
						authorsBuilder.WriteString(" (")
						authorsBuilder.WriteString(author.Email)
						authorsBuilder.WriteString(") ")
					}
					podcast.Metadata = append(podcast.Metadata, Metadata{"Authors", authorsBuilder.String()})
				}
	
				if len(parsed.Categories) > 0 {
					podcast.Metadata = append(podcast.Metadata, Metadata{"Categories", strings.Join(parsed.Categories, "/")})
				}
	
				if parsed.Copyright != "" {
					podcast.Metadata = append(podcast.Metadata, Metadata{"Copyright", parsed.Copyright})
				}
	
				if parsed.Generator != "" {
					podcast.Metadata = append(podcast.Metadata, Metadata{"Generator", parsed.Generator})
				}
	
				for _, parsedItem := range parsed.Items {
					var item Item
					item.Description = parsedItem.Description
					item.Title = parsedItem.Title
	
					if parsedItem.Image != nil {
						item.ImageTitle = parsedItem.Image.Title
						item.ImageURL = parsedItem.Image.URL
					} else {
						item.ImageTitle = ""
						item.ImageURL = ""
					}
	
	
					for _, enclosure := range parsedItem.Enclosures {
						item.Enclosures = append(item.Enclosures, Enclosure{enclosure.URL, enclosure.Type})
					}
	
					if parsedItem.Updated != "" {
						item.Metadata = append(item.Metadata, Metadata{"Updated", parsedItem.Updated})
					}
	
					if parsedItem.Published != "" {
						item.Metadata = append(item.Metadata, Metadata{"Published", parsedItem.Published})
					}
	
					if parsedItem.Content != "" {
						item.Metadata = append(item.Metadata, Metadata{"Content", parsedItem.Content})
					}
	
					if parsedItem.Link != "" {
						item.Metadata = append(item.Metadata, Metadata{"Link", parsedItem.Link})
					}
	
					if len(parsedItem.Authors) > 0 {
						var authorsBuilder strings.Builder
						for _, author := range parsedItem.Authors {
							authorsBuilder.WriteString(author.Name)
							authorsBuilder.WriteString(" (")
							authorsBuilder.WriteString(author.Email)
							authorsBuilder.WriteString(") ")
						}
						item .Metadata = append(item.Metadata, Metadata{"Authors", authorsBuilder.String()})
					}
	
					if len(parsedItem.Categories) > 0 {
						item.Metadata = append(item.Metadata, Metadata{"Categories", strings.Join(parsedItem.Categories, "/")})
					}
	
					podcast.Items = append(podcast.Items, item)
				}
	
				fmt.Println("Parsing podcast template")
				pageTemplate, err := template.ParseFiles("./templates/podcast.html")
				if err != nil {
					http.Error(w, err.Error(), 500)
				}
				
				var pageBuilder bytes.Buffer
				err = pageTemplate.Execute(&pageBuilder, podcast)
				if err != nil {
					http.Error(w, err.Error(), 500)
				}
	
				page := new(bytes.Buffer)
				writer := gzip.NewWriter(page)
				writer.Write(pageBuilder.Bytes())
				writer.Close()
	
				statement, err = database.Prepare("INSERT INTO Pages VALUES (?, ?, ?, ?)")
				if (err != nil) {
					http.Error(w, err.Error(), 500)
				}
	
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
		
				_, err = statement.Exec(feed, etag, lastModified, page.Bytes())
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
		
				// TODO: use concurrency to render all the pages simultaneously
			
			}
	
	
			t, _ := template.ParseFiles("./templates/index.html")
			buff := new(bytes.Buffer)
			writer := gzip.NewWriter(buff)
			t.Execute(writer, subscriptions)
			writer.Close()
	
			statement, err = database.Prepare("INSERT INTO Pages VALUES (?, ?, ?, ?)")
			if (err != nil) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			
			index.URL = "/"
			index.ETag = ""
			index.LastModified = stat.ModTime().Format(http.TimeFormat)
			index.HTML = buff.Bytes()

			statement.Exec(index.URL, index.ETag, index.LastModified, index.HTML)
		}


		encodings := r.Header["Accept-Encoding"]
		compress := len(encodings) > 0 && strings.Contains(encodings[0], "gzip")


		if compress {
			fmt.Println("Compressed")
			w.Header().Add("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(index.HTML)
		} else {
			byteReader := bytes.NewReader(index.HTML)
			reader, err := gzip.NewReader(byteReader)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			body, err := ioutil.ReadAll(reader)
			reader.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Write(body)
		}

	})

	http.HandleFunc("/podcast", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Query().Get("url")

		if url == "" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}


		statement, err := database.Prepare("SELECT * FROM Pages WHERE URL = ?")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		row := statement.QueryRow(url)
		statement.Close()
		var page Page
		err = row.Scan(&page.URL, &page.ETag, &page.LastModified, &page.HTML)
		if err == sql.ErrNoRows {
			http.Error(w, "Podcast URL not found", http.StatusNotFound)
			return
		}

		// TODO: This is where we check for updates.
		// None of my current feeds support ETag. So only going by Last-Modified only for now.
		var client http.Client
		req, err := http.NewRequest("GET", page.URL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if page.LastModified != "" {
			fmt.Println(page.LastModified)
			req.Header.Add("If-Modified-Since", page.LastModified)
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if (resp.StatusCode != http.StatusNotModified) {
			// In this case, we need to recache.

			// TODO: No I am not copy and pasting several screens worth of code from above.
			// Extract it properly.
		}

		encodings := r.Header["Accept-Encoding"]
		compress := len(encodings) > 0 && strings.Contains(encodings[0], "gzip")


		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		
		if compress {
			fmt.Println("Compressed")
			w.Header().Add("Content-Encoding", "gzip")
			w.Write(page.HTML)
		} else {
			fmt.Println("NOt compressed")
			reader, err := gzip.NewReader(bytes.NewReader(page.HTML))
			if err != nil {
				log.Fatal(err)
			}

			text, err := ioutil.ReadAll(reader)
			if err != nil {
				log.Fatal(err)
			}
			reader.Close()
			w.Write(text)
		}
	})

	port, set := os.LookupEnv("PORT")
	if !set {
		port = "8080"
	}
	port = fmt.Sprintf(":%v", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
	
