package utils

import "testing"

// TestToKobo checks the conversion from naira to the
// integer kobo that Paystack expects.
func TestToKobo(t *testing.T) {

	cases := []struct {
		name string

		amount float64

		want int64
	}{
		{name: "whole naira", amount: 25, want: 2500},

		{name: "two decimals", amount: 12.34, want: 1234},

		{name: "one decimal", amount: 0.5, want: 50},

		{name: "zero", amount: 0, want: 0},

		{name: "one kobo", amount: 0.01, want: 1},

		{
			name: "a sum that floats badly in binary",
			amount: 0.1 + 0.2,
			want:   30,
		},

		{
			name: "a large but realistic order",
			amount: 1234567.89,
			want:   123456789,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			got := ToKobo(testCase.amount)

			if got != testCase.want {

				t.Errorf(
					"ToKobo(%v) = %d, want %d",
					testCase.amount,
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestKoboRoundTrip checks that converting to kobo and
// back gives the original amount.
//
// This is the property that matters in practice: an order
// total is stored as decimals, converted to kobo to be
// paid, and converted back for display. If that round trip
// were not exact, a shopper could be shown one total and
// charged another.
func TestKoboRoundTrip(t *testing.T) {

	amounts := []float64{
		0,
		0.01,
		0.99,
		1,
		12.34,
		99.99,
		1000,
		4999.95,
	}

	for _, amount := range amounts {

		got := FromKobo(ToKobo(amount))

		if got != amount {

			t.Errorf(
				"round trip of %v produced %v",
				amount,
				got,
			)
		}
	}
}

// TestFormatKobo checks the decimal string used when
// passing an amount into SQL.
func TestFormatKobo(t *testing.T) {

	cases := []struct {
		name string

		kobo int64

		want string
	}{
		{name: "whole amount", kobo: 2500, want: "25.00"},

		{name: "with kobo", kobo: 1234, want: "12.34"},

		{
			name: "a trailing zero is kept",
			kobo: 1230,
			want: "12.30",
		},

		{
			name: "less than one naira keeps the leading zero",
			kobo: 5,
			want: "0.05",
		},

		{name: "zero", kobo: 0, want: "0.00"},

		{name: "negative", kobo: -500, want: "-5.00"},

		{
			name: "negative with kobo",
			kobo: -1234,
			want: "-12.34",
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			got := FormatKobo(testCase.kobo)

			if got != testCase.want {

				t.Errorf(
					"FormatKobo(%d) = %q, want %q",
					testCase.kobo,
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestRemoveDiscount checks that a discount can never
// push a total below zero.
//
// A negative total would be rejected by the orders table,
// so a coupon worth more than the order must make the
// order free rather than break checkout.
func TestRemoveDiscount(t *testing.T) {

	cases := []struct {
		name string

		subtotalKobo int64

		discountKobo int64

		want int64
	}{
		{
			name: "an ordinary discount",

			subtotalKobo: 10000, discountKobo: 2500,

			want: 7500,
		},

		{
			name: "no discount",

			subtotalKobo: 10000, discountKobo: 0,

			want: 10000,
		},

		{
			name: "a discount equal to the subtotal makes it free",

			subtotalKobo: 10000, discountKobo: 10000,

			want: 0,
		},

		{
			name: "a discount larger than the subtotal stops at zero",

			subtotalKobo: 10000, discountKobo: 99999,

			want: 0,
		},

		{
			name: "an empty subtotal stays at zero",

			subtotalKobo: 0, discountKobo: 500,

			want: 0,
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			got := RemoveDiscount(
				testCase.subtotalKobo,
				testCase.discountKobo,
			)

			if got != testCase.want {

				t.Errorf(
					"RemoveDiscount(%d, %d) = %d, want %d",
					testCase.subtotalKobo,
					testCase.discountKobo,
					got,
					testCase.want,
				)
			}
		})
	}
}

// TestRoundToTwoDecimals checks the tidying applied to
// amounts built by multiplication in Go, which is what
// stops a JSON response from showing 1234.5600000000001.
func TestRoundToTwoDecimals(t *testing.T) {

	cases := []struct {
		name string

		amount float64

		want float64
	}{
		{
			name: "already clean",

			amount: 12.34, want: 12.34,
		},

		{
			name: "a messy binary result is tidied",

			amount: 1234.5600000000001, want: 1234.56,
		},

		{
			name: "a price multiplied by a quantity",

			amount: 19.99 * 3, want: 59.97,
		},

		{
			name: "rounds to the nearest kobo",

			amount: 1.994, want: 1.99,
		},

		{
			name: "rounds up at half a kobo",

			// 1.995 as a float64 is very slightly
			// above 1.995, so it rounds up. This
			// case is here to be honest about the
			// edge: the value is approximately
			// half a kobo, and the result is the
			// nearer kobo either way.
			amount: 1.995, want: 2.00,
		},

		{name: "zero", amount: 0, want: 0},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			got := RoundToTwoDecimals(testCase.amount)

			if got != testCase.want {

				t.Errorf(
					"RoundToTwoDecimals(%v) = %v, want %v",
					testCase.amount,
					got,
					testCase.want,
				)
			}
		})
	}
}
