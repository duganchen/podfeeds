package main

import (
	"fmt"
	"log"

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

func main() {
	podcasts := []string{"aa", "bb"}

	d, err := yaml.Marshal(podcasts)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(d))

}
