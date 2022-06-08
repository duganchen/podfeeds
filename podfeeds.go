package main

import (
	"fmt"
	"log"
	"net/http"
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

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		for key, values := range r.Header {
			fmt.Println(key, values)
		}
		fmt.Println()

		w.Header().Set("ETag", "aaa")
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")

		fmt.Fprintf(w, "Hii there, I love %s!", r.URL.Path[1:])

	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
