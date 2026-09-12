package models

import "time"

// Product is one item for sale on the marketplace.
//
// Notice that a product does not store its stock level.
// Available stock is worked out by adding up the rows in
// the inventory_movements ledger, exactly the way the
// balance in the Financial Tracker is worked out by
// adding up income and expenses. Storing a running total
// would mean two sources of truth that can drift apart.
type Product struct {

	// ID is the unique identifier for the product.
	ID int `json:"id"`

	// SellerID identifies the account that listed
	// the product and is allowed to edit it.
	SellerID int `json:"seller_id"`

	// CategoryID is the category this product
	// belongs to.
	CategoryID int `json:"category_id"`

	// Name is the product title.
	Name string `json:"name"`

	// Slug is the URL-friendly version of the name.
	Slug string `json:"slug"`

	// Description explains the product to shoppers.
	Description string `json:"description"`

	// ImagePath is where the product's picture is served
	// from, or an empty string when it has none.
	//
	// It holds a path and not image data. The picture
	// itself sits in a folder on disk, and this is the
	// address the browser is given for it, so the same
	// value is both what the API reports and what a page
	// puts in an img tag.
	//
	// Nothing a client sends can write this field. It is
	// set by the picture scanner and by nothing else, so
	// a seller cannot point a listing at an address of
	// their choosing.
	ImagePath string `json:"image_path"`

	// Price is the amount in the marketplace currency.
	//
	// It is a NUMERIC(15,2) column in PostgreSQL, so
	// amounts stay exact. The Go side only carries
	// the value; every sum and discount is calculated
	// in SQL to avoid floating-point drift.
	Price float64 `json:"price"`

	// Currency is the currency the price is quoted in.
	Currency string `json:"currency"`

	// IsActive lets a seller hide a product without
	// deleting it.
	//
	// Inactive products disappear from the catalog
	// but stay visible on past orders, because the
	// order items keep their own snapshot of the name
	// and price.
	IsActive bool `json:"is_active"`

	// StockAvailable is derived from the inventory
	// ledger when the product is read.
	//
	// It is never stored in the products table.
	StockAvailable int `json:"stock_available"`

	// AverageRating and ReviewCount are derived from
	// the reviews table when the product is read.
	AverageRating float64 `json:"average_rating"`
	ReviewCount   int     `json:"review_count"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
