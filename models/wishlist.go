package models

import "time"

// WishlistItem is one product saved for later.
//
// A wishlist holds no quantities and no prices of its own.
// It is a reminder, not a plan to buy, so the current
// product details are joined in when the list is read.
type WishlistItem struct {

	ID int `json:"id"`

	UserID int `json:"user_id"`

	ProductID int `json:"product_id"`

	AddedAt time.Time `json:"added_at"`

	// Product carries the current details of the saved
	// product.
	Product Product `json:"product"`
}

// WishlistAddRequest is the body accepted when saving a
// product.
type WishlistAddRequest struct {

	ProductID int `json:"product_id"`
}
