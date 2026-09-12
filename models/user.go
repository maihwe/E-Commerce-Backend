package models

import "time"

// Role values that a user account can hold.
//
// Every account starts as a buyer. A buyer can browse
// the catalog, keep a cart, place orders, leave reviews,
// and manage a wishlist.
//
// A seller can do everything a buyer can do, and in
// addition can create products and move an order
// through its fulfilment steps.
//
// An admin oversees the whole marketplace: categories,
// user roles, coupons, and refunds.
const (
	RoleBuyer  = "buyer"
	RoleSeller = "seller"
	RoleAdmin  = "admin"
)

// User represents one account on the marketplace.
type User struct {

	// ID is the unique identifier for the account.
	ID int `json:"id"`

	// Name is the display name chosen at registration.
	Name string `json:"name"`

	// Email is the login address.
	//
	// It is stored in lowercase so that two accounts
	// cannot register the same address with different
	// capitalisation.
	Email string `json:"email"`

	// PasswordHash holds the bcrypt hash of the password.
	//
	// The json:"-" tag means this field is never
	// included when a user is encoded as JSON, so the
	// hash can never leak to a client by accident.
	PasswordHash string `json:"-"`

	// Role is one of RoleBuyer, RoleSeller, or RoleAdmin.
	Role string `json:"role"`

	// CreatedAt records when the account was created.
	CreatedAt time.Time `json:"created_at"`
}
