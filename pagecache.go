package main

// The page cache API, which is a key-value store mapping feed URLs to StoredPages.

type PageCache interface {
	Get(string) (Page, error)
	Set(string, Page) error
	Clear() error
	Erase(string) error
}

// A podcast page, rendered and stored in the cache.
type Page struct {
	ETag         string
	LastModified string
	HTML         []byte // Yes, they're stored gzipped.
}
