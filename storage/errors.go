package storage

import "errors"

// Errors the order layer returns so that handlers can
// tell one kind of failure from another and choose the
// right HTTP status.
var (

	// ErrCartEmpty means there is nothing to check out.
	ErrCartEmpty = errors.New("cart is empty")

	// ErrProductUnavailable means a product in the cart
	// has been deactivated by its seller.
	ErrProductUnavailable = errors.New(
		"a product in the cart is no longer available",
	)

	// ErrInsufficientStock means there are not enough
	// units to fulfil an order.
	ErrInsufficientStock = errors.New(
		"not enough stock to fulfil this order",
	)

	// ErrOrderNotFound means no order matched.
	ErrOrderNotFound = errors.New("order not found")

	// ErrInvalidTransition means the requested status
	// change is not allowed by the state machine.
	ErrInvalidTransition = errors.New(
		"that status change is not allowed",
	)

	// ErrAmountMismatch means the amount the provider
	// says was paid does not equal the order total.
	//
	// This is a serious signal: it means either the
	// webhook was tampered with, or something is wrong
	// with the payment. It must never be treated as a
	// successful payment.
	ErrAmountMismatch = errors.New(
		"the amount paid does not match the order total",
	)

	// ErrCouponRejected wraps a coupon that cannot be
	// applied.
	ErrCouponRejected = errors.New("coupon cannot be applied")

	// ErrCouponNotFound means the code did not match
	// any coupon.
	ErrCouponNotFound = errors.New("coupon not found")

	// ErrAlreadyPaid means the order was not pending, so
	// there was nothing to confirm.
	//
	// This is not a failure. It is what a duplicate
	// webhook delivery looks like from the inside, and
	// the caller answers the provider with a success so
	// that it stops retrying.
	ErrAlreadyPaid = errors.New(
		"order is not awaiting payment",
	)
)
