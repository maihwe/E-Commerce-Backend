package models

import (
	"testing"
	"time"
)

// TestComputeDiscountKoboPercentage checks percentage
// coupons.
func TestComputeDiscountKoboPercentage(t *testing.T) {

	cases := []struct {
		name string

		percent float64

		subtotalKobo int64

		want int64
	}{
		{
			name: "ten percent of one hundred naira",

			percent: 10, subtotalKobo: 10000,

			want: 1000,
		},

		{
			name: "a percentage that does not divide evenly rounds to the nearest kobo",

			// 7.5% of 3333 kobo is 249.975, which
			// is nearer 250 than 249.
			percent: 7.5, subtotalKobo: 3333,

			want: 250,
		},

		{
			name: "one hundred percent makes the order free",

			percent: 100, subtotalKobo: 5000,

			want: 5000,
		},

		{
			name: "a tiny percentage of a small order can round to nothing",

			// 1% of 40 kobo is 0.4 kobo, which
			// rounds to zero. That is the honest
			// answer: there is no such thing as a
			// fraction of a kobo.
			percent: 1, subtotalKobo: 40,

			want: 0,
		},

		{
			name: "nothing is taken off an empty order",

			percent: 50, subtotalKobo: 0,

			want: 0,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			coupon := Coupon{
				DiscountType: DiscountPercentage,

				DiscountValue: testCase.percent,
			}

			got := coupon.ComputeDiscountKobo(
				testCase.subtotalKobo,
			)

			if got != testCase.want {

				t.Errorf(
					"%v%% of %d kobo = %d, want %d",
					testCase.percent,
					testCase.subtotalKobo,
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestComputeDiscountKoboFixed checks fixed amount
// coupons.
func TestComputeDiscountKoboFixed(t *testing.T) {

	cases := []struct {
		name string

		amount float64

		subtotalKobo int64

		want int64
	}{
		{
			name: "five naira off",

			amount: 5, subtotalKobo: 10000,

			want: 500,
		},

		{
			name: "one kobo off",

			amount: 0.01, subtotalKobo: 10000,

			want: 1,
		},

		{
			name: "a discount larger than the order is clamped",

			amount: 500, subtotalKobo: 10000,

			want: 10000,
		},

		{
			name: "a discount exactly the size of the order is allowed",

			amount: 100, subtotalKobo: 10000,

			want: 10000,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			coupon := Coupon{
				DiscountType: DiscountFixed,

				DiscountValue: testCase.amount,
			}

			got := coupon.ComputeDiscountKobo(
				testCase.subtotalKobo,
			)

			if got != testCase.want {

				t.Errorf(
					"%v off %d kobo = %d, want %d",
					testCase.amount,
					testCase.subtotalKobo,
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestComputeDiscountKoboRefusesNonsense checks that a
// coupon that makes no sense discounts nothing.
//
// Failing towards zero is the safe direction: a
// misconfigured coupon should cost the seller nothing
// rather than give away an arbitrary amount.
func TestComputeDiscountKoboRefusesNonsense(t *testing.T) {

	cases := []struct {
		name string

		coupon Coupon

		want int64
	}{
		{
			name: "an unknown discount type",

			coupon: Coupon{
				DiscountType:  "buy-one-get-one",
				DiscountValue: 100,
			},

			want: 0,
		},

		{
			name: "an empty discount type",

			coupon: Coupon{
				DiscountType:  "",
				DiscountValue: 100,
			},

			want: 0,
		},

		{
			name: "a negative percentage",

			coupon: Coupon{
				DiscountType:  DiscountPercentage,
				DiscountValue: -10,
			},

			want: 0,
		},

		{
			name: "a negative fixed amount",

			coupon: Coupon{
				DiscountType:  DiscountFixed,
				DiscountValue: -5,
			},

			want: 0,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			got := testCase.coupon.ComputeDiscountKobo(
				10000,
			)

			if got != testCase.want {

				t.Errorf(
					"ComputeDiscountKobo returned %d, want %d",
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestRejectCoupon walks every rule that can stop a
// coupon being used.
//
// These rules live in one function, which checkout and the
// preview endpoint both call. That is what makes it
// impossible for a coupon to preview successfully and then
// be refused when the order is actually placed.
func TestRejectCoupon(t *testing.T) {

	now := time.Date(
		2026, time.September, 12, 12, 0, 0, 0, time.UTC,
	)

	startedEarlier := now.Add(-24 * time.Hour)

	endsLater := now.Add(24 * time.Hour)

	startedLater := now.Add(24 * time.Hour)

	endedEarlier := now.Add(-24 * time.Hour)

	cases := []struct {
		name string

		coupon Coupon

		subtotalKobo int64

		wantRejected bool
	}{
		{
			name: "an active coupon within its window is accepted",

			coupon: Coupon{
				IsActive: true,
				StartsAt: &startedEarlier,
				EndsAt:   &endsLater,
			},

			subtotalKobo: 10000,

			wantRejected: false,
		},

		{
			name: "a coupon with no window at all is always open",

			coupon: Coupon{IsActive: true},

			subtotalKobo: 10000,

			wantRejected: false,
		},

		{
			name: "an inactive coupon is refused",

			coupon: Coupon{
				IsActive: false,
				StartsAt: &startedEarlier,
				EndsAt:   &endsLater,
			},

			subtotalKobo: 10000,

			wantRejected: true,
		},

		{
			name: "a coupon that has not started yet is refused",

			coupon: Coupon{
				IsActive: true,
				StartsAt: &startedLater,
			},

			subtotalKobo: 10000,

			wantRejected: true,
		},

		{
			name: "an expired coupon is refused",

			coupon: Coupon{
				IsActive: true,
				EndsAt:   &endedEarlier,
			},

			subtotalKobo: 10000,

			wantRejected: true,
		},

		{
			name: "a coupon used up to its limit is refused",

			coupon: Coupon{
				IsActive: true,
				MaxUses:  intPointer(3),
				UsedCount: 3,
			},

			subtotalKobo: 10000,

			wantRejected: true,
		},

		{
			name: "a coupon with one use left is still accepted",

			coupon: Coupon{
				IsActive: true,
				MaxUses:  intPointer(3),
				UsedCount: 2,
			},

			subtotalKobo: 10000,

			wantRejected: false,
		},

		{
			name: "a coupon with no usage limit is never exhausted",

			coupon: Coupon{
				IsActive:  true,
				MaxUses:   nil,
				UsedCount: 100000,
			},

			subtotalKobo: 10000,

			wantRejected: false,
		},

		{
			name: "an order below the minimum is refused",

			coupon: Coupon{
				IsActive:       true,
				MinOrderAmount: 200,
			},

			// 199 naira, one naira short of the
			// 200 naira minimum.
			subtotalKobo: 19900,

			wantRejected: true,
		},

		{
			name: "an order exactly on the minimum is accepted",

			coupon: Coupon{
				IsActive:       true,
				MinOrderAmount: 200,
			},

			subtotalKobo: 20000,

			wantRejected: false,
		},

		{
			name: "an order above the minimum is accepted",

			coupon: Coupon{
				IsActive:       true,
				MinOrderAmount: 200,
			},

			subtotalKobo: 20001,

			wantRejected: false,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			rejection := RejectCoupon(
				testCase.coupon,
				testCase.subtotalKobo,
				now,
			)

			rejected := rejection != ""

			if rejected != testCase.wantRejected {

				t.Errorf(
					"RejectCoupon returned %q, wanted rejected=%v",
					rejection,
					testCase.wantRejected,
				)
			}
		})
	}
}

// intPointer is a small helper for the fields that
// distinguish "unlimited" from a number.
func intPointer(value int) *int {

	return &value
}
