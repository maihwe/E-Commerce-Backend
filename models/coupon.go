package models

import (
	"math"
	"strings"
	"time"
)

// The two ways a coupon can take money off an order.
const (
	DiscountPercentage = "percentage"
	DiscountFixed      = "fixed"
)

// Coupon is a discount code.
//
// Notice that the coupon does not store how many times it
// has been used. That number is counted from the
// coupon_redemptions table when it is needed, so it can
// never disagree with the redemptions that produced it.
type Coupon struct {

	ID int `json:"id"`

	// Code is stored and compared in uppercase.
	Code string `json:"code"`

	// DiscountType is DiscountPercentage or
	// DiscountFixed.
	DiscountType string `json:"discount_type"`

	// DiscountValue is either a percentage from 0 to
	// 100, or an amount in the marketplace currency.
	DiscountValue float64 `json:"discount_value"`

	// MinOrderAmount is the subtotal an order must
	// reach before the coupon applies.
	MinOrderAmount float64 `json:"min_order_amount"`

	// MaxUses is the total number of redemptions
	// allowed. Nil means unlimited.
	MaxUses *int `json:"max_uses"`

	// UsedCount is counted from coupon_redemptions.
	UsedCount int `json:"used_count"`

	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`

	IsActive bool `json:"is_active"`

	CreatedAt time.Time `json:"created_at"`
}

// NormaliseCode tidies a coupon code for comparison.
//
// Shoppers type codes by hand, so " welcome10 " and
// "WELCOME10" must mean the same coupon. Storing and
// comparing everything in uppercase with the spaces
// trimmed away makes that happen.
func NormaliseCode(code string) string {

	return strings.ToUpper(
		strings.TrimSpace(code),
	)
}

// ComputeDiscountKobo works out how much a coupon takes
// off an order, given the subtotal in kobo.
//
// All the arithmetic is integer arithmetic, so the result
// is exact. A percentage is rounded to the nearest kobo
// using math.Round, which rounds halves up, matching what
// a shopper would expect to see.
func (c Coupon) ComputeDiscountKobo(subtotalKobo int64) int64 {

	var discountKobo int64

	switch c.DiscountType {

	case DiscountPercentage:

		// Work in float only to form the ratio, then
		// round straight back to an integer.
		discountKobo = int64(math.Round(
			float64(subtotalKobo) * c.DiscountValue / 100,
		))

	case DiscountFixed:

		discountKobo = int64(math.Round(
			c.DiscountValue * 100,
		))

	default:

		// An unknown type discounts nothing, which is
		// the safe direction to fail in.
		return 0
	}

	if discountKobo < 0 {
		return 0
	}

	// Never take off more than the order is worth.
	if discountKobo > subtotalKobo {
		return subtotalKobo
	}

	return discountKobo
}

// CouponRejection explains why a coupon cannot be used,
// and is empty when the coupon is fine.
type CouponRejection string

// HasExpired reports whether the coupon's window has
// closed at the given moment.
func (c Coupon) HasExpired(now time.Time) bool {

	return c.EndsAt != nil && now.After(*c.EndsAt)
}

// HasNotStarted reports whether the coupon's window has
// not opened yet at the given moment.
func (c Coupon) HasNotStarted(now time.Time) bool {

	return c.StartsAt != nil && now.Before(*c.StartsAt)
}

// IsExhausted reports whether the coupon has been used
// as many times as it is allowed to be.
func (c Coupon) IsExhausted() bool {

	if c.MaxUses == nil {
		return false
	}

	return c.UsedCount >= *c.MaxUses
}

// RejectCoupon returns a human-readable reason why the
// coupon cannot be applied to an order of this size, or
// an empty string when it can be applied.
//
// Every business rule about coupons lives in this one
// function, so the checkout path and the "preview my
// discount" endpoint can never disagree about whether a
// code is usable.
func RejectCoupon(
	c Coupon,
	subtotalKobo int64,
	now time.Time,
) string {

	if !c.IsActive {
		return "This coupon is no longer active"
	}

	if c.HasNotStarted(now) {
		return "This coupon is not active yet"
	}

	if c.HasExpired(now) {
		return "This coupon has expired"
	}

	if c.IsExhausted() {
		return "This coupon has reached its usage limit"
	}

	// Compare the minimum in kobo so both sides of the
	// comparison are integers.
	if subtotalKobo < int64(math.Round(
		c.MinOrderAmount*100,
	)) {

		return "This coupon requires a larger order"
	}

	return ""
}
