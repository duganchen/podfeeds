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

func CacheFeed(feed string, database *sql.DB) (string, error) {
	resp, err := http.Get(feed)
	if err != nil {
		return "", err
	}

	fp := gofeed.NewParser()

	parsed, err := fp.Parse(resp.Body)
	if err != nil {
		return "", err
	}

	var podcast Podcast
	podcast.Language = parsed.Language
	podcast.FeedLink = parsed.FeedLink
	if parsed.Image != nil {
		podcast.ImageURL = parsed.Image.URL
		podcast.ImageTitle = parsed.Image.Title
	} else {
		podcast.ImageURL = ""
		podcast.ImageTitle = ""
	}

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

	pageTemplate, err := template.ParseFiles("./templates/podcast.html")
	if err != nil {
		return "", err
	}
	
	var pageBuilder bytes.Buffer
	err = pageTemplate.Execute(&pageBuilder, podcast)
	if err != nil {
		return "", err
	}

	page := new(bytes.Buffer)
	writer := gzip.NewWriter(page)
	_, err = writer.Write(pageBuilder.Bytes())
	if err != nil {
		return "", err
	}
	writer.Close()

	statement, err := database.Prepare("INSERT INTO Pages VALUES (?, ?, ?, ?)")
	if (err != nil) {
		return "", err
	}
	defer statement.Close()

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
		return "", err
	}

	return parsed.Title, nil
}

func LoadPage(url string, database *sql.DB) (Page, error) {
	var page Page
	statement, err := database.Prepare("SELECT * FROM Pages WHERE URL = ?")
	if err != nil {
		return page, err
	}
	row := statement.QueryRow(url)
	defer statement.Close()
	err = row.Scan(&page.URL, &page.ETag, &page.LastModified, &page.HTML)
	if err == sql.ErrNoRows {
		return page, err
	}
	return page, nil
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
		// Not defering this results in a ver, very noticeable drop in performance.
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
			_, err := database.Exec("DELETE FROM Pages")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			subscriptions := Subscriptions{}


			for _, feed := range feeds {
				// Look. We *could* fetch and render every page at the same time because we're using go, but
				// that would be optimizing the path where the cache misses, and we kinda don't care about that
				// by definition.
				title, err := CacheFeed(feed, database)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				subscription := Subscription{title, "/podcast?url=" + url.QueryEscape(feed)}
				subscriptions.Subscriptions = append(subscriptions.Subscriptions, subscription)		
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
			defer statement.Close()
			
			index.URL = "/"
			index.ETag = ""
			index.LastModified = stat.ModTime().Format(http.TimeFormat)
			index.HTML = buff.Bytes()

			_, err = statement.Exec(index.URL, index.ETag, index.LastModified, index.HTML)
			if (err != nil) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		err = WriteResponse(w, index.HTML, r)
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

		page, err := LoadPage(url, database)
		if err == sql.ErrNoRows {
			http.Error(w, "Podcast URL not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// TODO:
		// None of my current feeds support ETag. So only going by Last-Modified only for now.
		var client http.Client
		req, err := http.NewRequest("GET", page.URL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if page.LastModified != "" {
			req.Header.Add("If-Modified-Since", page.LastModified)
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if (resp.StatusCode != http.StatusNotModified) {

			stmt, err := database.Prepare("DELETE FROM Pages WHERE URL = ?")
			if (err != nil) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer stmt.Close()
			_, err = stmt.Exec(page.URL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			_, err = CacheFeed(page.URL, database)
			if (err != nil) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			page, err = LoadPage(url, database)
			if err == sql.ErrNoRows {
				http.Error(w, "Podcast URL not found", http.StatusNotFound)
				return
			}
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
	if !set {
		port = "8080"
	}
	port = fmt.Sprintf(":%v", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
	
