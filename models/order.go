package models

import "time"

// Order is a confirmed purchase.
//
// An order keeps its own copy of the product name and
// price for every line. This is deliberate: if a seller
// later renames a product or changes its price, the old
// order must still show what the shopper actually agreed
// to pay. An order is a record of a historical event, so
// it is never rewritten from current data.
type Order struct {

	ID int `json:"id"`

	// BuyerID is the account that placed the order.
	BuyerID int `json:"buyer_id"`

	// Status is one of the order state constants in
	// models/order_status.go.
	Status string `json:"status"`

	// Subtotal is the sum of the line totals before
	// any discount is taken off.
	Subtotal float64 `json:"subtotal"`

	// DiscountAmount is how much a coupon took off.
	DiscountAmount float64 `json:"discount_amount"`

	// Total is what the buyer actually owes, which is
	// Subtotal minus DiscountAmount.
	Total float64 `json:"total"`

	// CouponID records which coupon was used, if any.
	CouponID *int `json:"coupon_id"`

	// CouponCode is the coupon's code, read back for
	// display. It is empty when no coupon was used.
	CouponCode string `json:"coupon_code"`

	// PaymentReference is the reference sent to
	// Paystack when payment was started.
	//
	// It is UNIQUE in the database, which is what
	// lets an incoming webhook find exactly one order.
	PaymentReference string `json:"payment_reference"`

	// The timestamps below record when the order
	// entered each state. They are all nil until the
	// order reaches that state.
	PaidAt      *time.Time `json:"paid_at"`
	ShippedAt   *time.Time `json:"shipped_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
	CancelledAt *time.Time `json:"cancelled_at"`
	RefundedAt  *time.Time `json:"refunded_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Items are the product lines on this order.
	Items []OrderItem `json:"items"`

	// Events is the append-only history of status
	// changes, oldest first.
	Events []OrderEvent `json:"events,omitempty"`

	// AllowedTransitions tells a client which status
	// changes are legal next, so it can show only the
	// actions that will actually be accepted.
	AllowedTransitions []string `json:"allowed_transitions,omitempty"`
}

// OrderItem is one product line on an order.
type OrderItem struct {

	ID int `json:"id"`

	OrderID int `json:"order_id"`

	ProductID int `json:"product_id"`

	// SellerID is copied from the product so that a
	// seller can list their own orders without having
	// to join back to the products table, which may
	// since have changed hands.
	SellerID int `json:"seller_id"`

	// ProductName and UnitPrice are snapshots taken
	// when the order was placed.
	ProductName string `json:"product_name"`
	UnitPrice   float64 `json:"unit_price"`

	Quantity int `json:"quantity"`

	// LineTotal is UnitPrice multiplied by Quantity.
	//
	// PostgreSQL computes it as a generated column, so
	// it can never disagree with the two values above.
	LineTotal float64 `json:"line_total"`
}

// OrderEvent is one entry in an order's history.
//
// Like the inventory ledger, this table is append-only.
// Nothing is ever updated or deleted, so the full story
// of an order stays available.
type OrderEvent struct {

	ID int `json:"id"`

	OrderID int `json:"order_id"`

	// FromStatus and ToStatus describe the change.
	//
	// FromStatus is empty for the first row, which
	// records the order being created.
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`

	// ActorID is the account that caused the change.
	//
	// It is nil when the change came from the payment
	// provider's webhook rather than from a person.
	ActorID *int `json:"actor_id"`

	// Note explains the change, for example
	// "payment confirmed by Paystack".
	Note string `json:"note"`

	CreatedAt time.Time `json:"created_at"`
}

// OrderCreateRequest is the JSON body accepted when
// placing an order.
type OrderCreateRequest struct {

	// CouponCode is optional.
	CouponCode string `json:"coupon_code"`
}
