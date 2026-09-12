package web

import (
	"strconv"
	"strings"
	"unicode"
)

// tileColours is how many colours the stylesheet defines,
// as .t0 through .t7.
//
// It is repeated here because Go has to pick one and CSS has
// to have it. That is a real coupling rather than a
// decorative one: if this number were larger than the
// stylesheet's palette, a product would be handed a class
// that does not exist and would render as a blank white
// square. The two are kept adjacent in the comments so that
// changing one is a reminder to change the other.
const tileColours = 8

// tileClass picks a colour for a product's tile.
//
// It is derived from the product's id rather than from a
// column, because there is no image column and no colour
// column to read. Deriving it has the useful property that
// a product keeps the same colour everywhere it appears --
// the catalog, the product page, a cart line -- without
// anything being stored, and that two products next to each
// other in the grid are usually different colours, since
// ids are handed out in sequence.
func tileClass(id int) string {

	// A negative id cannot occur, but a modulo of one would
	// still be negative and would index nothing, so the sign
	// is dropped rather than trusted.
	if id < 0 {
		id = -id
	}

	return "t" + strconv.Itoa(id%tileColours)
}

// tileInitials reduces a product name to the one or two
// letters that will be drawn on its tile.
//
// The first and last words are used rather than the first
// two, because the names this catalog actually contains are
// things like "Cast Iron Pot" and "Cast Iron Lid": their
// first two words are identical, so "CI" would appear on
// both and the letters would carry no information at all.
// First and last gives "CP" and "CL", which do.
//
// Words that begin with a digit are skipped. A listing
// created twice gets a number appended to keep its slug
// unique, and nobody wants a tile that reads "C1".
func tileInitials(name string) string {

	var first, last rune

	var firstWord []rune

	count := 0

	for _, word := range strings.Fields(name) {

		letters := []rune(word)

		if len(letters) == 0 {
			continue
		}

		if unicode.IsDigit(letters[0]) {
			continue
		}

		if count == 0 {
			first = unicode.ToUpper(letters[0])

			firstWord = letters
		}

		last = unicode.ToUpper(letters[0])

		count++
	}

	switch count {

	case 0:

		// A name made entirely of digits or punctuation.
		// There is nothing to draw, but a tile that is
		// empty looks broken, so it gets a mark.
		return "?"

	case 1:

		// One word has no last to pair with, so it lends
		// its own second letter instead.
		if len(firstWord) >= 2 {

			return strings.ToUpper(
				string(firstWord[:2]),
			)
		}

		return string(first)
	}

	return string([]rune{first, last})
}
