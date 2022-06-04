package main

// Parse CBC World Report feed to Lynx-friendly HTML

import (
	"fmt"
	"log"
	"net/http"

	"github.com/mmcdole/gofeed"
)

func main() {

	resp, err := http.Get("https://www.cbc.ca/podcasting/includes/wr.xml")
	if (err != nil) {
		log.Fatal(err)
	}

	fp := gofeed.NewParser()
	feed, err := fp.Parse(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	
	fmt.Println("<!DOCTYPE html>")
	fmt.Printf("<html lang=\"%v\">\n", feed.Language)
	fmt.Println("<head>")
	fmt.Println("<meta charset=\"utf-8\">")

	fmt.Printf("<title>%v</title>\n", feed.Title)
	fmt.Println(
`</head>
<body>`)
	fmt.Printf("<h1>%v</h1>\n", feed.Title)

	fmt.Printf("<p><img src=\"%v\" alt=\"%v\"/></p>\n", feed.Image.URL, feed.Image.Title)
	fmt.Println("<div>")
	fmt.Println(feed.Description)
	fmt.Println("</div>")
	fmt.Printf("<p><a href=\"%v\">Link</a></p>\n", feed.FeedLink)

	for _, item := range feed.Items {
		fmt.Printf("<h2>%v</h2>\n", item.Title)
		for _, enclosure := range item.Enclosures {
			// Object tag. Why not
			fmt.Printf("<object data=\"%v\" type=\"%v\">\n", enclosure.URL, enclosure.Type)
			// And an old-fashioned A tag for Lynx. Which does not support Objects.
			fmt.Printf("<p><a href=\"%v\">Enclosure</a></p>\n", enclosure.URL)
			fmt.Println("</object>")
		}
		fmt.Println("<div>")
		fmt.Println(item.Description)
		fmt.Println("</div>")
	}

	fmt.Println("</body>")
	fmt.Println("</html>")
}