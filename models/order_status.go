package models

import "sort"

// The states an order can be in.
//
// An order starts as pending and walks a fixed path
// towards delivered. It can leave that path only by
// being cancelled before payment, or refunded after.
const (
	OrderPending   = "pending"
	OrderPaid      = "paid"
	OrderShipped   = "shipped"
	OrderDelivered = "delivered"
	OrderCancelled = "cancelled"
	OrderRefunded  = "refunded"
)

// orderTransitions is the single source of truth for
// which order status changes are legal.
//
// Read it as: "an order that is currently X may become
// one of these". Anything not listed here is refused.
//
// Notice two deliberate choices:
//
//   - cancelled is only reachable from pending. Once
//     money has changed hands, the honest exit is a
//     refund, not a cancellation, so the records show
//     that a payment really did happen and was
//     returned.
//
//   - delivered and refunded are both terminal. Nothing
//     leads out of them, so a completed order cannot
//     quietly change state afterwards.
var orderTransitions = map[string][]string{

	OrderPending: {
		OrderPaid,
		OrderCancelled,
	},

	OrderPaid: {
		OrderShipped,
		OrderRefunded,
	},

	OrderShipped: {
		OrderDelivered,
		OrderRefunded,
	},

	OrderDelivered: {
		OrderRefunded,
	},

	OrderCancelled: {},

	OrderRefunded: {},
}

// OrderStatuses lists every valid state, which is handy
// for validating query filters and for tests.
func OrderStatuses() []string {

	statuses := make(
		[]string,
		0,
		len(orderTransitions),
	)

	for status := range orderTransitions {
		statuses = append(statuses, status)
	}

	sort.Strings(statuses)

	return statuses
}

// IsValidOrderStatus reports whether a string names a
// real order state.
func IsValidOrderStatus(status string) bool {

	_, exists := orderTransitions[status]

	return exists
}

// CanTransitionOrder reports whether an order may move
// from one status to another.
//
// This is the check the handlers use to produce a clear
// "409 Conflict" message. The SQL in storage/order.go
// enforces the same rule a second time, so two requests
// racing each other still cannot both win.
func CanTransitionOrder(from string, to string) bool {

	allowed, exists := orderTransitions[from]

	if !exists {
		return false
	}

	for _, status := range allowed {

		if status == to {
			return true
		}
	}

	return false
}

// AllowedOrderTransitions returns every status an order
// may move to next.
//
// The API reports this alongside an order so a client
// can show only the buttons that will actually work,
// instead of guessing and collecting errors.
func AllowedOrderTransitions(from string) []string {

	allowed, exists := orderTransitions[from]

	if !exists {
		return []string{}
	}

	// Copy the slice so a caller cannot modify the
	// transition table by accident.
	result := make([]string, len(allowed))

	copy(result, allowed)

	return result
}
