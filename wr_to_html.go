package main

// Parse CBC World Report feed to Lynx-friendly HTML

import (
	"fmt"
	"log"

	"github.com/mmcdole/gofeed"
)

func main() {
	fmt.Println(
`<!DOCTYPE html>
<html lang="en">
	<head>
		<meta charset="utf-8">`)
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://www.cbc.ca/podcasting/includes/wr.xml")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("		<title>%v</title>\n", feed.Title)
	fmt.Println(`	</head>
	<body>`)
	fmt.Printf("		<h1>%v</h1>\n", feed.Title)
	fmt.Println("	<div>")
	fmt.Println(feed.Description)
	fmt.Println("	</div>")
	fmt.Println("	</body>")
	fmt.Println("</html>")
}