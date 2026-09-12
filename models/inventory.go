package models

import "time"

// InventoryMovement is one entry in the inventory
// ledger.
//
// This table is append-only. A row is written and never
// changed or deleted, so the ledger doubles as a complete
// audit trail of everything that ever happened to a
// product's stock.
//
// Available stock is never stored anywhere. It is always
// this sum:
//
//	SELECT SUM(quantity_change)
//	FROM inventory_movements
//	WHERE product_id = ?
//
// That is the same pattern as the Financial Tracker,
// where the account balance is never stored either, but
// recalculated from the income and expense rows. The
// advantage is that there is only one source of truth,
// so a stored total can never drift out of step with
// the history that produced it.
type InventoryMovement struct {

	ID int `json:"id"`

	ProductID int `json:"product_id"`

	// QuantityChange is how much the stock moved.
	//
	// It is positive when stock arrives, such as a
	// restock, and negative when stock leaves, such as
	// a sale. It is never zero, because a movement of
	// nothing is not a movement.
	QuantityChange int `json:"quantity_change"`

	// Reason explains why the stock moved.
	//
	// "restock"    - the seller added stock.
	// "sale"       - an order was confirmed and paid.
	// "adjustment" - the seller corrected a count.
	// "refund"     - a refunded order returned stock.
	// "cancel"     - a cancelled order returned stock.
	Reason string `json:"reason"`

	// OrderID links the movement to the order that
	// caused it.
	//
	// It is nil for restocks and adjustments, which
	// have nothing to do with an order.
	OrderID *int `json:"order_id"`

	CreatedAt time.Time `json:"created_at"`
}

// The reasons a stock movement can have.
//
// They are constants so the compiler catches a typo in
// Go, and the same list appears in a CHECK constraint in
// migration 004 so PostgreSQL catches it too.
const (
	MovementRestock    = "restock"
	MovementSale       = "sale"
	MovementAdjustment = "adjustment"
	MovementRefund     = "refund"
	MovementCancel     = "cancel"
)
