package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/sync/errgroup"
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

func help() {
	fmt.Println("Usage: podfeeds (build|serve)")
}

func fetchFeed(feed string, subscriptions []Subscription, index int, podcastTemplate *template.Template, fp *gofeed.Parser) func() error {

	return func() error {

		// This works well. Just using http.Get breaks with CBC Your World Tonight
		parsed, err := fp.ParseURL(feed)
		if err != nil {
			return err
		}

		renderedPodcastFilename := fmt.Sprintf("%s.html", base64.StdEncoding.EncodeToString(([]byte(feed))))

		subscriptions[index] = Subscription{parsed.Title, renderedPodcastFilename}

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

			if parsedItem.UpdatedParsed != nil {
				item.Metadata = append(item.Metadata, Metadata{"Updated", parsedItem.UpdatedParsed.Format(time.RFC822)})
			}

			if parsedItem.PublishedParsed != nil {
				item.Metadata = append(item.Metadata, Metadata{"Published", parsedItem.PublishedParsed.Format(time.RFC822)})
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

		renderedPodcastFilePath := fmt.Sprintf("_site.tmp/%s", renderedPodcastFilename)
		renderedPodcastFile, err := os.Create(renderedPodcastFilePath)
		if err != nil {
			return err
		}
		podcastTemplate.Execute(renderedPodcastFile, podcast)
		defer renderedPodcastFile.Close()
		return nil
	}
}

func build() error {
	feeds := make([]string, 0)

	buf, err := os.ReadFile("./podcasts.yaml")
	yaml.Unmarshal(buf, &feeds)

	if err != nil {
		return err
	}

	// Previous versions fetched and rendered feeds in parallel, and also
	// validated for duplicate feeds. I'm going to forego both for now.

	subscriptions := make([]Subscription, len(feeds))

	fp := gofeed.NewParser()

	podcastTemplate := template.Must(template.ParseFiles("./templates/podcast.html"))

	os.RemoveAll("_site.tmp")
	os.Mkdir("_site.tmp", 0755)

	g := new(errgroup.Group)
	for i, feed := range feeds {
		g.Go(fetchFeed(feed, subscriptions, i, podcastTemplate, fp))
	}

	err = g.Wait()
	if err != nil {
		return err
	}

	htmls, _ := filepath.Glob("_site/*.html")
	for _, html := range htmls {
		err := os.Remove(html)
		if err != nil {
			return err
		}
	}

	htmls, _ = filepath.Glob("_site.tmp/*.html")
	for _, html := range htmls {
		err := os.Rename(html, fmt.Sprintf("_site/%s", filepath.Base(html)))
		if err != nil {
			return err
		}
	}

	os.RemoveAll("_site.tmp")

	indexTemplate := template.Must(template.ParseFiles("templates/index.html"))
	buff := new(bytes.Buffer)
	err = indexTemplate.Execute(buff, subscriptions)
	if err != nil {
		return err
	}

	return os.WriteFile("_site/index.html", buff.Bytes(), 0644)
}

func serve() error {
	// Just copying this
	fs2 := http.FileServer(http.Dir("_site"))
	http.Handle("/", fs2)

	port, set := os.LookupEnv("PORT")

	if set && port == "0" {
		/* The Port 0 implementation is from here:
		https://youtu.be/bYSo78dwgH8
		*/
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			return err
		}

		freePort := l.Addr().(*net.TCPAddr).Port
		fmt.Printf("Using port: %d\n", freePort)

		return http.Serve(l, nil)
	}

	if !set {
		port = "8080"
	}

	fmt.Printf("Using port: %s\n", port)

	return http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}

func main() {

	if len(os.Args) != 2 {
		help()
		return
	}

	switch os.Args[1] {
	case "build":
		err := build()
		if err != nil {
			log.Fatal(err)
		}
	case "serve":
		err := serve()
		if err != nil {
			log.Fatal(err)
		}
	default:
		help()
	}
}
