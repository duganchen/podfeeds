package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	// It is my misfortune to start this right before json v2 is becoming available
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

type Subscription struct {
	Title string
	URL   string
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

type TOCEntry struct {
	GUID  string
	Title string
}

type Podcast struct {
	Title       string
	Description string
	Language    string
	Items       []Item
	// We don't care about FeedLink. It's a link to the XML file.
	TOC []TOCEntry
}

// No I am not going to use a library for this
type Spinner struct {
	index  int
	frames string
	count  int
	mutex  sync.Mutex
}

// Works well for json, which works well for the static site generator design
type Page struct {
	ETag         string
	LastModified string
	HTML         string // Full HTML page. gzipped and base64 encoded
	Title        string
	URL          string
}

func NewSpinner() *Spinner {
	// AI also suggested this. Which, being UTF-8, is a bit more complicated to implement.
	// "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

	return &Spinner{index: 0, frames: "-\\|/", count: 4}
}

func help() {
	fmt.Fprintln(os.Stderr, "Usage: podfeeds (build [clean]|serve)")
}

func savedFeedFilename(feed string) string {
	return fmt.Sprintf("%x.html", sha256.Sum256([]byte(feed)))
}

func fetchFeed(feed string, i int, pages []Page, podcastTemplate *template.Template, client *http.Client, spinner *Spinner) func() error {

	return func() error {

		req, err := http.NewRequest(http.MethodGet, feed, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")

		// Add the caching headers from the last build to the request

		if pages[i].LastModified != "" {
			req.Header.Set("If-Modified-Since", pages[i].LastModified)
		}

		if pages[i].ETag != "" {
			req.Header.Set("If-None-Match", pages[i].ETag)
		}

		resp, err := client.Do(req)

		if err != nil {
			return err
		}

		renderedPodcastFilename := savedFeedFilename(feed)

		if resp.StatusCode == http.StatusNotModified {
			// Got it. In the case of a cache hit, we just write out the HTML page that we saved from the last build
			// (which is stored gzipped and base64-encoded)
			// The rest of what we need has already been loaded from the cache.

			rawReader := strings.NewReader(pages[i].HTML)
			b64Reader := base64.NewDecoder(base64.StdEncoding, rawReader)
			gzipReader, err := gzip.NewReader(b64Reader)
			if err != nil {
				return err
			}
			defer gzipReader.Close()
			htmlBytes, err := io.ReadAll(gzipReader)

			renderedPodcastFilePath := fmt.Sprintf("_site.tmp/%s", renderedPodcastFilename)
			renderedPodcastFile, err := os.Create(renderedPodcastFilePath)
			if err != nil {
				return err
			}
			defer renderedPodcastFile.Close()
			_, err = renderedPodcastFile.Write(htmlBytes)
			if err != nil {
				return err
			}

			return nil
		}

		pages[i].ETag = resp.Header.Get("Etag")
		pages[i].LastModified = resp.Header.Get("Last-Modified")

		pages[i].URL = feed

		fp := gofeed.NewParser()
		parsed, err := fp.Parse(resp.Body)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		pages[i].Title = parsed.Title

		var podcast Podcast
		podcast.Language = parsed.Language

		podcast.Title = parsed.Title
		podcast.Description = parsed.Description

		for _, parsedItem := range parsed.Items {
			var item Item
			item.Description = parsedItem.Description
			item.Title = parsedItem.Title

			item.GUID = parsedItem.GUID

			podcast.TOC = append(podcast.TOC, TOCEntry{GUID: item.GUID, Title: item.Title})

			for _, enclosure := range parsedItem.Enclosures {
				item.Enclosures = append(item.Enclosures, Enclosure{URL: enclosure.URL, Type: enclosure.Type})
			}

			if parsedItem.UpdatedParsed != nil {
				item.Metadata = append(item.Metadata, Metadata{Key: "Updated", Value: parsedItem.UpdatedParsed.Format(time.RFC822)})
			}

			if parsedItem.PublishedParsed != nil {
				item.Metadata = append(item.Metadata, Metadata{Key: "Published", Value: parsedItem.PublishedParsed.Format(time.RFC822)})
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
				item.Metadata = append(item.Metadata, Metadata{Key: "Authors", Value: authorsBuilder.String()})
			}

			podcast.Items = append(podcast.Items, item)
		}

		if len(podcast.TOC) == 1 {
			podcast.TOC = nil
		}

		renderedPodcastFilePath := fmt.Sprintf("_site.tmp/%s", renderedPodcastFilename)
		renderedPodcastFile, err := os.Create(renderedPodcastFilePath)
		if err != nil {
			return err
		}

		var podcastBuffer bytes.Buffer
		err = podcastTemplate.Execute(&podcastBuffer, podcast)
		if err != nil {
			return err
		}

		w := bufio.NewWriter(renderedPodcastFile)
		_, err = w.Write(podcastBuffer.Bytes())
		defer renderedPodcastFile.Close()
		if err != nil {
			return err
		}

		var cachedHTMLBuffer bytes.Buffer
		zw := gzip.NewWriter(&cachedHTMLBuffer)
		_, err = zw.Write(podcastBuffer.Bytes())
		if err != nil {
			return err
		}
		err = zw.Close()
		if err != nil {
			return err
		}
		pages[i].HTML = base64.StdEncoding.EncodeToString(cachedHTMLBuffer.Bytes())

		spinner.mutex.Lock()
		fmt.Printf("\r	%c", spinner.frames[spinner.index])
		// Every CS student knows this pattern
		spinner.index = (spinner.index + 1) % spinner.count
		spinner.mutex.Unlock()
		return nil
	}
}

func build(clean bool) error {
	feeds := make([]string, 0)

	buf, err := os.ReadFile("./podcasts.yaml")
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(buf, &feeds)
	if err != nil {
		return err
	}

	feedMap := make(map[string]struct{})
	for _, feed := range feeds {
		feedMap[feed] = struct{}{}
	}

	if len(feedMap) < len(feeds) {
		return errors.New("Duplicate feed")
	}

	podcastTemplate := template.Must(template.ParseFiles("./templates/podcast.html"))

	siteTmpErr := os.Mkdir("_site.tmp", 0755)
	if siteTmpErr != nil && os.IsExist(err) {
		fmt.Printf("A build is already in progress")
		return nil
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	g := new(errgroup.Group)
	g.SetLimit(20)
	spinner := NewSpinner()

	if clean {
		// Ignoring the error here is deliberate
		os.Remove("cache.json")
	}

	pages := make([]Page, len(feeds))

	cacheBytes, err := os.ReadFile("cache.json")
	cacheLoaded := false
	if err == nil {
		jsonErr := json.Unmarshal(cacheBytes, &pages)
		if jsonErr != nil {
			return jsonErr
		}
		cacheLoaded = true
	}

	if cacheLoaded {
		if len(pages) != len(feeds) {
			return errors.New("Cache build needed. Please use podfeeds build clean")
		}

		for i, feed := range feeds {
			if pages[i].URL != feed {
				fmt.Println(pages[i].URL, feed)
				return errors.New("Cache build needed. Please use podfeeds build clean")
			}
		}
	}

	for i, feed := range feeds {
		g.Go(fetchFeed(feed, i, pages, podcastTemplate, client, spinner))
	}

	err = g.Wait()
	if err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(pages, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile("cache.json", jsonData, 0644)
	if err != nil {
		return err
	}

	// This was to erase the spinner, right?
	fmt.Print("\r                 \r")

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

	subscriptions := make([]Subscription, len(feeds))
	for i, feed := range feeds {
		subscriptions[i].Title = pages[i].Title
		subscriptions[i].URL = savedFeedFilename(feed)
	}

	indexTemplate := template.Must(template.ParseFiles("templates/index.html"))
	buff := new(bytes.Buffer)
	err = indexTemplate.Execute(buff, subscriptions)
	if err != nil {
		return err
	}

	return os.WriteFile("_site/index.html", buff.Bytes(), 0644)
}

func serve() error {
	_, err := os.Stat("_site/index.html")
	if err != nil {
		return errors.New("Site is not built.")
	}

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

	if len(os.Args) != 2 && len(os.Args) != 3 {
		help()
		return
	}

	if len(os.Args) == 3 && os.Args[2] != "clean" {
		help()
		return
	}

	switch os.Args[1] {
	case "build":
		buildErr := build(len(os.Args) == 3 && os.Args[2] == "clean")
		if buildErr != nil {
			stat, notFound := os.Stat("_site.tmp")
			if notFound == nil && stat.IsDir() {
				cleanupErr := os.RemoveAll("_site.tmp")
				if cleanupErr != nil {
					fmt.Println(cleanupErr)
				}
			}
			log.Fatal(buildErr)
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
