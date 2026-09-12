package utils

import (
	"strings"
	"unicode"
)

// Slugify converts a name into a URL-friendly slug.
//
// "Home & Kitchen" becomes "home-kitchen", which reads
// nicely in a URL and can be used to filter the catalog
// without exposing a numeric ID.
func Slugify(value string) string {

	value = strings.ToLower(strings.TrimSpace(value))

	var builder strings.Builder

	// lastWasDash stops two separators in a row from
	// producing "home--kitchen".
	lastWasDash := false

	for _, character := range value {

		switch {

		// Letters and digits are kept as they are.
		case unicode.IsLetter(character) ||
			unicode.IsDigit(character):

			builder.WriteRune(character)

			lastWasDash = false

		// Anything else becomes a single dash.
		default:

			if !lastWasDash && builder.Len() > 0 {

				builder.WriteRune('-')

				lastWasDash = true
			}
		}
	}

	// Trim any dash left hanging at the end.
	return strings.Trim(builder.String(), "-")
}
