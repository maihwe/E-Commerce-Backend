package models

// ProductFilter describes how a client wants the
// catalog searched, filtered, sorted, and paged.
//
// Every field is optional. A zero value means "do not
// filter on this", so an empty filter returns the whole
// catalog in the default order.
type ProductFilter struct {

	// Query matches against the product name and
	// description.
	Query string

	// CategoryID limits results to one category.
	CategoryID int

	// CategorySlug does the same as CategoryID but
	// using the readable slug instead of the number.
	CategorySlug string

	// SellerID limits results to one seller's
	// listings.
	SellerID int

	// MinPrice and MaxPrice bound the price.
	//
	// They are pointers so that a limit of zero can be
	// told apart from "no limit given". Filtering for
	// free products is unusual, but treating a missing
	// bound as a real one would be a bug.
	MinPrice *float64
	MaxPrice *float64

	// IncludeInactive lets a seller or admin see
	// listings that are hidden from shoppers.
	IncludeInactive bool

	// Sort is a key from the allowed sort list, for
	// example "price_asc". An unknown key falls back
	// to the default ordering.
	Sort string

	// Limit and Offset come from the pagination
	// helper and are passed as plain numbers so that
	// this package does not have to import utils,
	// which would create an import cycle.
	Limit  int
	Offset int
}
