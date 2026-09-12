package models

import "testing"

// TestCanTransitionOrder walks the whole state machine.
//
// The table below is written out by hand rather than
// derived from orderTransitions, so that changing the
// transition table cannot quietly change what the tests
// expect. If somebody loosens a rule, these tests fail and
// the change has to be argued for.
func TestCanTransitionOrder(t *testing.T) {

	cases := []struct {
		name string

		from string

		to string

		want bool
	}{
		// The happy path, in order.
		{
			name: "pending to paid",
			from: OrderPending, to: OrderPaid,
			want: true,
		},
		{
			name: "paid to shipped",
			from: OrderPaid, to: OrderShipped,
			want: true,
		},
		{
			name: "shipped to delivered",
			from: OrderShipped, to: OrderDelivered,
			want: true,
		},

		// Cancelling is only allowed before money moves.
		{
			name: "pending to cancelled",
			from: OrderPending, to: OrderCancelled,
			want: true,
		},
		{
			name: "paid cannot be cancelled",
			from: OrderPaid, to: OrderCancelled,
			want: false,
		},
		{
			name: "shipped cannot be cancelled",
			from: OrderShipped, to: OrderCancelled,
			want: false,
		},

		// Refunding is allowed once money has moved.
		{
			name: "paid to refunded",
			from: OrderPaid, to: OrderRefunded,
			want: true,
		},
		{
			name: "shipped to refunded",
			from: OrderShipped, to: OrderRefunded,
			want: true,
		},
		{
			name: "delivered to refunded",
			from: OrderDelivered, to: OrderRefunded,
			want: true,
		},

		// Terminal states lead nowhere.
		{
			name: "cancelled is terminal",
			from: OrderCancelled, to: OrderPaid,
			want: false,
		},
		{
			name: "refunded is terminal",
			from: OrderRefunded, to: OrderShipped,
			want: false,
		},

		// Skipping ahead is not allowed. A seller must
		// not be able to mark something delivered that
		// was never paid for.
		{
			name: "pending cannot skip to shipped",
			from: OrderPending, to: OrderShipped,
			want: false,
		},
		{
			name: "pending cannot skip to delivered",
			from: OrderPending, to: OrderDelivered,
			want: false,
		},
		{
			name: "paid cannot skip to delivered",
			from: OrderPaid, to: OrderDelivered,
			want: false,
		},

		// Going backwards is not allowed either.
		{
			name: "shipped cannot go back to paid",
			from: OrderShipped, to: OrderPaid,
			want: false,
		},
		{
			name: "delivered cannot go back to shipped",
			from: OrderDelivered, to: OrderShipped,
			want: false,
		},

		// A status cannot move to itself.
		{
			name: "no status moves to itself",
			from: OrderPaid, to: OrderPaid,
			want: false,
		},

		// Unknown statuses are refused in both
		// directions rather than defaulting to open.
		{
			name: "unknown source status is refused",
			from: "banana", to: OrderPaid,
			want: false,
		},
		{
			name: "unknown target status is refused",
			from: OrderPending, to: "banana",
			want: false,
		},
		{
			name: "empty statuses are refused",
			from: "", to: "",
			want: false,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			got := CanTransitionOrder(
				testCase.from,
				testCase.to,
			)

			if got != testCase.want {

				t.Errorf(
					"CanTransitionOrder(%q, %q) = %v, want %v",
					testCase.from,
					testCase.to,
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestOrderStatusesAreAllKnown checks that every state is
// reachable from somewhere, and that unknown names are
// rejected.
func TestOrderStatusesAreAllKnown(t *testing.T) {

	statuses := OrderStatuses()

	if len(statuses) != 6 {

		t.Fatalf(
			"expected 6 order statuses, got %d: %v",
			len(statuses),
			statuses,
		)
	}

	for _, status := range statuses {

		if !IsValidOrderStatus(status) {

			t.Errorf(
				"%q is listed as a status but is not valid",
				status,
			)
		}
	}

	if IsValidOrderStatus("banana") {

		t.Error(
			"IsValidOrderStatus accepted an unknown status",
		)
	}

	if IsValidOrderStatus("") {

		t.Error(
			"IsValidOrderStatus accepted an empty status",
		)
	}
}

// TestAllowedOrderTransitionsIsACopy checks that a caller
// cannot reach into the transition table and change it.
//
// The slice a caller receives is handed out to build API
// responses, so if it shared its backing array with the
// table, a handler that appended to it could corrupt the
// state machine for every other request in the process.
func TestAllowedOrderTransitionsIsACopy(t *testing.T) {

	first := AllowedOrderTransitions(OrderPending)

	if len(first) != 2 {

		t.Fatalf(
			"expected 2 transitions from pending, got %v",
			first,
		)
	}

	// Scribble over the returned slice.
	for index := range first {
		first[index] = "scribbled"
	}

	second := AllowedOrderTransitions(OrderPending)

	for _, status := range second {

		if status == "scribbled" {

			t.Fatal(
				"changing the returned slice changed the transition table",
			)
		}
	}

	if !CanTransitionOrder(OrderPending, OrderPaid) {

		t.Fatal(
			"the transition table was damaged by a caller",
		)
	}
}

// TestAllowedOrderTransitionsForUnknownStatus checks that
// an unknown status returns an empty list rather than a
// nil slice, which would serialise as JSON null and make a
// client handle two different shapes.
func TestAllowedOrderTransitionsForUnknownStatus(t *testing.T) {

	allowed := AllowedOrderTransitions("banana")

	if allowed == nil {

		t.Fatal(
			"expected an empty slice, got nil",
		)
	}

	if len(allowed) != 0 {

		t.Fatalf(
			"expected no transitions, got %v",
			allowed,
		)
	}
}
