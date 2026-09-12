package web

import (
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"

	"e-commerce-backend/utils"
)

// templateFuncs are the small formatters the templates use.
//
// They live here rather than being spelled out inside the
// markup because a template is a poor place to do
// arithmetic, and because a price should be rendered the
// same way on every page that shows one.
var templateFuncs = template.FuncMap{

	"money": formatNaira,

	"stars": formatStars,

	"date": formatDate,

	"status": formatStatus,

	"tileClass": tileClass,

	"tileInitials": tileInitials,

	"categoryIcon": categoryIcon,
}

// nairaSymbol is the naira sign, U+20A6.
//
// It is written as the character itself rather than as an
// escape, which means this file has to be read and written
// as UTF-8. Go source is UTF-8 by definition, so that is
// no burden, and a currency sign spelled out is easier to
// recognise than a number to look up.
const nairaSymbol = "₦"

// formatNaira renders an amount the way a shopper expects
// to read it: a currency sign, thousands separated, and
// exactly two decimal places.
//
// The arithmetic goes through utils rather than being done
// here. An amount is rounded to the nearest kobo and then
// converted to that integer, which is the same route every
// stored amount takes, so a price displayed on a page and
// the same price sent to Paystack cannot disagree over a
// fraction of a kobo.
func formatNaira(amount float64) string {

	rounded := utils.RoundToTwoDecimals(amount)

	sign := ""

	// Work with the positive value so that the split below
	// stays simple, then put the sign back at the end. A
	// discount can be negative, and a basket can show a
	// negative adjustment.
	if rounded < 0 {

		sign = "-"

		rounded = -rounded
	}

	kobo := utils.ToKobo(rounded)

	return sign +
		nairaSymbol +
		groupThousands(kobo/utils.KoboPerNaira) +
		fmt.Sprintf(".%02d", kobo%utils.KoboPerNaira)
}

// groupThousands puts a comma between every three digits
// of a whole number.
//
// strconv and the standard library will format a decimal
// but not group one, and pulling in a formatting package
// to place a few commas would be the largest dependency in
// the project.
func groupThousands(number int64) string {

	digits := strconv.FormatInt(number, 10)

	// Short numbers are already grouped.
	if len(digits) <= 3 {
		return digits
	}

	var grouped strings.Builder

	// The first group is however many digits are left over
	// once the rest have been divided into threes.
	lead := len(digits) % 3

	if lead > 0 {
		grouped.WriteString(digits[:lead])
	}

	for i := lead; i < len(digits); i += 3 {

		if grouped.Len() > 0 {
			grouped.WriteByte(',')
		}

		grouped.WriteString(digits[i : i+3])
	}

	return grouped.String()
}

// formatStars draws a five-star rating.
//
// The rating is rounded to a whole star, because a half
// star cannot be drawn with characters and an empty star
// that is really a half is a smaller lie than a wrong
// number would be. The precise figure is always printed
// next to it.
func formatStars(rating float64) string {

	filled := int(math.Round(rating))

	if filled < 0 {
		filled = 0
	}

	if filled > 5 {
		filled = 5
	}

	return strings.Repeat(
		"★",
		filled,
	) + strings.Repeat(
		"☆",
		5-filled,
	)
}

// formatDate renders a timestamp in the form the rest of
// the site writes dates, which is short and unambiguous
// enough for a phone screen.
func formatDate(when time.Time) string {

	if when.IsZero() {
		return ""
	}

	return when.Format("2 Jan 2006")
}

// formatStatus turns a stored status into something worth
// reading.
//
// The values in the database are lowercase single words,
// because that is what a CHECK constraint can hold. What
// a person reads should not look like a database column.
func formatStatus(status string) string {

	if status == "" {
		return ""
	}

	return strings.ToUpper(status[:1]) + status[1:]
}
