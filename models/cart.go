package models

import "time"

// Cart is a shopper's saved set of items.
//
// The cart is stored in the database rather than in the
// browser, so it follows the account: add something on a
// phone, open a laptop, and it is still there.
type Cart struct {

	ID int `json:"id"`

	UserID int `json:"user_id"`

	Items []CartItem `json:"items"`

	// Subtotal is the sum of the line totals.
	//
	// It is worked out when the cart is read rather than
	// stored, so it always reflects the current prices.
	// That is the opposite choice from an order, which
	// deliberately freezes its prices, because a cart is
	// a plan and an order is a record.
	Subtotal float64 `json:"subtotal"`

	// ItemCount is how many individual units are in the
	// cart, which is what a basket icon usually shows.
	ItemCount int `json:"item_count"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CartItem is one product in a cart.
type CartItem struct {

	ID int `json:"id"`

	CartID int `json:"cart_id"`

	ProductID int `json:"product_id"`

	Quantity int `json:"quantity"`

	AddedAt time.Time `json:"added_at"`

	// Product carries the current details of the item,
	// joined in when the cart is read.
	Product Product `json:"product"`

	// LineTotal is the current price multiplied by the
	// quantity.
	LineTotal float64 `json:"line_total"`

	// StockAvailable is carried here so a client can warn
	// the shopper that a quantity is no longer available
	// before they reach checkout.
	StockAvailable int `json:"stock_available"`
}

// CartAddRequest is the body accepted when adding an item.
type CartAddRequest struct {

	ProductID int `json:"product_id"`

	Quantity int `json:"quantity"`
}

// CartUpdateRequest is the body accepted when changing
// the quantity of an item already in the cart.
type CartUpdateRequest struct {

	Quantity int `json:"quantity"`
}
