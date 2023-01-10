package main

import "database/sql"

type SQLiteCache struct {
	db *sql.DB
}

func NewSQLiteCache() (SQLiteCache, error) {
	db, err := sql.Open("sqlite3", "./cache.sqlite3")
	if err != nil {
		return SQLiteCache{}, nil
	}

	statement, err := db.Prepare("CREATE TABLE IF NOT EXISTS Pages (URL TEXT PRIMARY KEY, ETag Text, LastModified TEXT, HTML BLOB)")
	if err != nil {
		return SQLiteCache{}, err
	}
	_, err = statement.Exec()
	if err != nil {
		return SQLiteCache{}, err
	}
	defer statement.Close()

	return SQLiteCache{db}, nil
}

func (cache SQLiteCache) Get(feed string) (Page, error) {
	statement, err := cache.db.Prepare("SELECT * FROM Pages WHERE URL = ?")
	if err != nil {
		return Page{}, err
	}
	row := statement.QueryRow(feed)
	defer statement.Close()
	var page Page
	var url string
	err = row.Scan(&url, &page.ETag, &page.LastModified, &page.HTML)
	if err == sql.ErrNoRows {
		return page, err
	}
	return page, nil
}

func (cache SQLiteCache) Set(feed string, page Page) error {
	statement, err := cache.db.Prepare("INSERT INTO Pages VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer statement.Close()

	_, err = statement.Exec(feed, page.ETag, page.LastModified, page.HTML)
	if err != nil {
		return err
	}

	return nil
}

func (cache SQLiteCache) Erase(feed string) error {
	stmt, err := cache.db.Prepare("DELETE FROM Pages WHERE URL = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(feed)
	return err
}

func (cache SQLiteCache) Clear() error {
	_, err := cache.db.Exec("DELETE FROM Pages")
	return err
}

func CloseSQLiteCache(cache SQLiteCache) {
	cache.db.Close()
}
