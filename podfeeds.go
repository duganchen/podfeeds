package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"html/template"
	"net/http"

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

func main() {

	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feeds := make([]string, 0)
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
				return
			}
			seen[feed] = true

			parsed, err := fp.ParseURL(feed)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			
			subscription := Subscription{parsed.Title, feed}
			subscriptions.Subscriptions = append(subscriptions.Subscriptions, subscription)
		}

		t, _ := template.ParseFiles("./templates/index.html")
		t.Execute(w, subscriptions)
	}))

	port, set := os.LookupEnv("PORT")
	if !set {
		port = "8080"
	}
	port = fmt.Sprintf(":%v", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
	
