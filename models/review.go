package models

import "time"

// Review is a rating and comment left on a product.
type Review struct {

	ID int `json:"id"`

	ProductID int `json:"product_id"`

	UserID int `json:"user_id"`

	// ReviewerName is joined in when reviews are listed,
	// so a client can show who wrote the review without
	// a second request.
	ReviewerName string `json:"reviewer_name"`

	// OrderID links the review to the purchase it came
	// from. It is nil when the reviewer never bought
	// the product.
	OrderID *int `json:"order_id"`

	// Rating is one to five stars.
	Rating int `json:"rating"`

	Title string `json:"title"`

	Body string `json:"body"`

	// VerifiedPurchase is true when the reviewer had
	// this product delivered to them.
	VerifiedPurchase bool `json:"verified_purchase"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReviewSummary is the overall rating picture for a
// product.
type ReviewSummary struct {

	ProductID int `json:"product_id"`

	AverageRating float64 `json:"average_rating"`

	ReviewCount int `json:"review_count"`

	// Distribution counts how many reviews gave each
	// number of stars, which is what a rating breakdown
	// chart needs.
	//
	// It always has exactly five entries, for one star
	// through five stars, with zeros where there are no
	// reviews.
	Distribution map[int]int `json:"distribution"`
}

// ReviewRequest is the body accepted when writing a
// review.
type ReviewRequest struct {

	Rating int `json:"rating"`

	Title string `json:"title"`

	Body string `json:"body"`
}
