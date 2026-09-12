package models

// OrderFilter describes which orders a client wants to
// see.
//
// The same struct serves a buyer looking at their own
// orders, a seller looking at orders they must fulfil,
// and an admin looking at everything, because the only
// difference between those three is which fields are set.
type OrderFilter struct {

	// BuyerID limits the results to orders placed by
	// one account.
	BuyerID int

	// SellerID limits the results to orders that
	// contain at least one item sold by this account.
	// This is the seller's fulfilment queue.
	SellerID int

	// Status limits the results to one order state.
	Status string

	// Limit and Offset come from the pagination
	// helper.
	Limit  int
	Offset int
}
