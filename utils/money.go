package utils

import (
	"fmt"
	"math"
)

// Money is handled in kobo, the smallest unit of the
// Nigerian Naira. One naira is 100 kobo.
//
// Why integers rather than decimals?
//
// A float64 cannot represent 0.10 exactly; it stores
// something very slightly different. Add a few of those
// together and the total is visibly wrong, which is
// unacceptable for money.
//
// Integers have no such problem. 10 kobo plus 20 kobo is
// exactly 30 kobo, always. This is also the format
// Paystack expects on the wire, so the conversion has to
// happen somewhere, and doing it at the edge keeps the
// rest of the arithmetic clean.
const (
	KoboPerNaira = 100
)

// ToKobo converts a naira amount into kobo.
//
// The rounding is what makes this safe. Every value that
// fits in a NUMERIC(15, 2) column, multiplied by 100, is
// small enough to be held exactly by a float64, so the
// rounded result is the exact kobo count rather than an
// approximation of it.
func ToKobo(amount float64) int64 {

	return int64(math.Round(amount * KoboPerNaira))
}

// FromKobo converts kobo back into a naira amount, for
// display or for JSON.
func FromKobo(kobo int64) float64 {

	return float64(kobo) / KoboPerNaira
}

// FormatKobo renders kobo as a decimal string such as
// "1234.56".
//
// This is used when passing an amount into SQL. A string
// cast to NUMERIC is exact, whereas sending a float would
// hand PostgreSQL a value that is already slightly off.
func FormatKobo(kobo int64) string {

	sign := ""

	// Work with the absolute value so the division and
	// remainder below stay simple, then put the sign
	// back at the end.
	if kobo < 0 {

		sign = "-"

		kobo = -kobo
	}

	return fmt.Sprintf(
		"%s%d.%02d",
		sign,
		kobo/KoboPerNaira,
		kobo%KoboPerNaira,
	)
}

// RemoveDiscount takes a discount off a subtotal.
//
// The discount can never push the total below zero, so a
// fixed coupon worth more than the order simply makes the
// order free instead of creating a negative total that
// the orders table would reject.
func RemoveDiscount(subtotalKobo int64, discountKobo int64) int64 {

	if discountKobo > subtotalKobo {
		return 0
	}

	return subtotalKobo - discountKobo
}

// RoundToTwoDecimals tidies a money amount for display.
//
// It exists because multiplying a price by a quantity in
// Go produces a float, which can leave a value looking
// like 1234.5600000000001 in a JSON response. Rounding to
// the nearest kobo first, then dividing back, gives a
// clean two-decimal result.
//
// Amounts that are going to be stored are not put through
// this. They are kept as integer kobo and formatted with
// FormatKobo, which is exact.
func RoundToTwoDecimals(amount float64) float64 {

	return float64(ToKobo(amount)) / KoboPerNaira
}
