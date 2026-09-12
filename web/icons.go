package web

import (
	"html/template"
	"strings"
)

// This file draws the pictures the tiles carry.
//
// A product with a photograph shows the photograph, and
// these are for the ones without. Rather than leave those
// as two letters on a colour, each category gets a small
// line drawing: a pot for Home & Kitchen, a shirt for
// Fashion, a basket for Groceries. It is not a photograph
// of the thing, but it does say what kind of thing it is,
// and at a glance down a grid that is most of what a
// photograph was doing.
//
// They are drawn rather than imported. There is no icon
// font and no sprite sheet, so an icon here is a few
// hundred bytes of path data in the binary that cannot
// fail to load, cannot be blocked, and cannot arrive
// looking different on a different phone.

// The attributes every icon shares.
//
// The stroke is currentColor, so the drawing takes the
// colour of whatever it is sitting in and the same markup
// works on a light tile and a dark one. The stroke width
// is set for the size these are actually drawn at, which
// is around forty pixels; a hairline looks weak there and
// a thick line closes up the small shapes.
const (
	svgOpen = `<svg viewBox="0 0 24 24" fill="none"` +
		` stroke="currentColor" stroke-width="1.7"` +
		` stroke-linecap="round"` +
		` stroke-linejoin="round"` +
		` aria-hidden="true">`

	svgClose = `</svg>`
)

// The ten drawings.
//
// They are all on a 24 by 24 grid so that they sit at the
// same weight beside each other, and all are outlines
// rather than solid shapes, which keeps them legible at
// the size a card tile gives them.
const (
	// A handset. The line is the speaker.
	iconPhone = template.HTML(
		svgOpen +
			`<rect x="7" y="2.5" width="10" height="19" rx="2.5"/>` +
			`<path d="M10.5 18.4h3"/>` +
			svgClose,
	)

	// A monitor on a stand.
	iconComputer = template.HTML(
		svgOpen +
			`<rect x="2.5" y="4" width="19" height="12.5" rx="2"/>` +
			`<path d="M12 16.5v4"/>` +
			`<path d="M9 20.5h6"/>` +
			svgClose,
	)

	// A chip with its pins, which reads as circuitry
	// without needing a circuit drawn.
	iconChip = template.HTML(
		svgOpen +
			`<rect x="7" y="7" width="10" height="10" rx="2"/>` +
			`<path d="M10 3.6V7M14 3.6V7"/>` +
			`<path d="M10 17v3.4M14 17v3.4"/>` +
			`<path d="M3.6 10H7M3.6 14H7"/>` +
			`<path d="M17 10h3.4M17 14h3.4"/>` +
			svgClose,
	)

	// A cooking pot, lid on.
	//
	// The lid is a semicircle sitting exactly on the rim,
	// which is how a pot is drawn when there is only one
	// line to draw it with.
	iconPot = template.HTML(
		svgOpen +
			`<path d="M2.5 9.5h19"/>` +
			`<path d="M4.5 9.5h15l-1 6.5a3.5 3.5 0 0 1` +
			`-3.5 3h-6a3.5 3.5 0 0 1-3.5-3z"/>` +
			`<path d="M8.5 9.5a3.5 3.5 0 0 1 7 0"/>` +
			`<path d="M12 4.7v1.3"/>` +
			svgClose,
	)

	// A shirt. The arc under the collar is what stops it
	// reading as a plain pentagon.
	iconShirt = template.HTML(
		svgOpen +
			`<path d="M9 3.5 4.5 5.5 2.5 9.5 6 11v9.5h12V11` +
			`l3.5-1.5-2-4L15 3.5a3 3 0 0 1-6 0z"/>` +
			svgClose,
	)

	// A bottle with a label band.
	iconBottle = template.HTML(
		svgOpen +
			`<path d="M9.5 2.5h5v3.2l1.6 2.4a4 4 0 0 1 .7 2.2` +
			`v9.3a2 2 0 0 1-2 2h-5.6a2 2 0 0 1-2-2v-9.3` +
			`a4 4 0 0 1 .7-2.2l1.6-2.4z"/>` +
			`<path d="M9.5 12.2h5"/>` +
			svgClose,
	)

	// An open book, both pages meeting at the spine.
	iconBook = template.HTML(
		svgOpen +
			`<path d="M12 6.5C10.5 5 8.5 4.5 4 4.5v13` +
			`c4.5 0 6.5 .5 8 2 1.5-1.5 3.5-2 8-2v-13` +
			`c-4.5 0-6.5 .5-8 2z"/>` +
			svgClose,
	)

	// A shopping basket: rim, tapering body, handle.
	iconBasket = template.HTML(
		svgOpen +
			`<path d="M3.5 9h17"/>` +
			`<path d="M5.3 9 7 19.5h10L18.7 9"/>` +
			`<path d="M8.5 9a3.5 3.5 0 0 1 7 0"/>` +
			svgClose,
	)

	// A ball, with two seams to say which kind.
	iconBall = template.HTML(
		svgOpen +
			`<circle cx="12" cy="12" r="8.5"/>` +
			`<path d="M3.9 9.4c4.6 1.7 11.6 1.7 16.2 0"/>` +
			`<path d="M3.9 14.6c4.6-1.7 11.6-1.7 16.2 0"/>` +
			svgClose,
	)

	// A price tag, for the category that means "none of
	// the above".
	iconTag = template.HTML(
		svgOpen +
			`<path d="M20.6 13.4 13.4 20.6a2 2 0 0 1-2.8 0` +
			`L3.5 13.5V3.5h10l7.1 7.1a2 2 0 0 1 0 2.8z"/>` +
			`<circle cx="8" cy="8" r="1.5"/>` +
			svgClose,
	)
)

// iconRule is one drawing and the words that select it.
type iconRule struct {
	words []string

	icon template.HTML
}

// iconRules is checked in order, and the first word found
// anywhere in the category's name wins.
//
// Matching on words rather than on the slugs in migration
// 013 is deliberate. The categories are rows, not code: an
// operator can add one, rename one, or reorder them, and a
// table of exact slugs would go on answering correctly
// right up until the day it silently stopped. Matching
// words means a new category called "Toys & Games" draws
// nothing rather than drawing the wrong thing, and a
// renamed "Kitchen" keeps its pot.
//
// The order matters where two words could both match, so
// the narrower words are listed before the broader ones.
var iconRules = []iconRule{

	{[]string{"phone", "tablet", "mobile"}, iconPhone},

	{[]string{"computer", "laptop", "desktop"}, iconComputer},

	{[]string{"electronic", "gadget", "circuit"}, iconChip},

	{[]string{"kitchen", "cook", "home"}, iconPot},

	{[]string{"fashion", "cloth", "apparel", "wear"}, iconShirt},

	{[]string{"health", "beauty", "care", "cosmetic"}, iconBottle},

	{[]string{"book", "stationery", "stationary"}, iconBook},

	{[]string{"grocer", "food", "drink"}, iconBasket},

	{[]string{"sport", "outdoor", "fitness", "game"}, iconBall},

	{[]string{"else", "other", "misc"}, iconTag},
}

// categoryIcon picks the drawing for a category name.
//
// An empty name, or a name no rule recognises, returns
// nothing at all. The template treats that as a signal to
// fall back to the product's initials, so an unrecognised
// category gets a tile that still looks deliberate rather
// than a blank square.
//
// What reaches the markup is only ever one of the
// constants above. The category name decides *which* one
// and is never itself written into the SVG, so there is no
// path by which a name typed by somebody could become
// markup.
func categoryIcon(name string) template.HTML {

	lowered := strings.ToLower(name)

	if strings.TrimSpace(lowered) == "" {
		return ""
	}

	for _, rule := range iconRules {

		for _, word := range rule.words {

			if strings.Contains(lowered, word) {
				return rule.icon
			}
		}
	}

	return ""
}
