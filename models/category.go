package models

import "time"

// Category groups products so that shoppers can
// browse a section of the catalog at a time.
type Category struct {

	// ID is the unique identifier for the category.
	ID int `json:"id"`

	// Name is the human-readable label.
	Name string `json:"name"`

	// Slug is the URL-friendly version of the name,
	// for example "home-kitchen" for "Home & Kitchen".
	//
	// Clients may filter the catalog by either ID
	// or slug, and slugs make links readable.
	Slug string `json:"slug"`

	// ProductCount is how many active products sit in
	// this category. It is derived when the category
	// list is read, not stored, so it can never go
	// stale.
	ProductCount int `json:"product_count"`

	// CreatedAt records when the category was added.
	CreatedAt time.Time `json:"created_at"`
}
