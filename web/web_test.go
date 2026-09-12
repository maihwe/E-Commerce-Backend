package web

import (
	"testing"

	"e-commerce-backend/models"
	"e-commerce-backend/utils"
)

// TestPagesParse parses every page the storefront serves,
// and checks that each one kept its own "content".
//
// A page that will not parse is a fault in this package and
// not something a visitor can cause, so the only place
// RegisterWebRoutes can report it is at startup, where main
// logs it and stops. That is a late moment to find out that
// one page file has a missing brace: the whole application
// refuses to run, and nothing points at the page.
//
// The second half is the thing loadPages goes to the
// trouble of guaranteeing. Parsing each page beside the
// layout, one set at a time, is what gives every page its
// own template called "content". Parsing them all together
// would leave only the last one under that name, and every
// address would quietly render the same page.
//
// Neither check needs a database, so they run wherever the
// pure unit tests run.
func TestPagesParse(t *testing.T) {

	pages, err := loadPages()

	if err != nil {

		t.Fatalf(
			"a page did not parse: %v",
			err,
		)
	}

	for _, file := range pageFiles {

		set, exists := pages[file]

		if !exists {

			t.Errorf(
				"%s: was not parsed",
				file,
			)

			continue
		}

		if set.Lookup(layoutFile) == nil {

			t.Errorf(
				"%s: parsed without the layout",
				file,
			)
		}

		if set.Lookup("content") == nil {

			t.Errorf(
				"%s: defines no content template",
				file,
			)
		}
	}
}

// TestHeroSlides pins what the band at the top of the
// catalog cycles through.
//
// It is worth testing on its own because almost every
// rule it follows is a rule about not showing something.
// Four of the seven cases below come back empty, and each
// of those is the band falling back to the one
// photograph. That is the easy thing to break without
// noticing: a wrong answer here still renders a page, and
// nobody sees the difference until a search for one thing
// is illustrated with a picture of another.
func TestHeroSlides(t *testing.T) {

	// pictured and plain are two products, one with a
	// picture and one without, so that the cases can say
	// which of them the band should be picking up.
	pictured := func(id int) models.Product {

		return models.Product{
			ID: id,

			ImagePath: "/shop/img/thing-" +
				string(rune('a'+id)) + ".jpg",
		}
	}

	plain := func(id int) models.Product {

		return models.Product{ID: id}
	}

	cases := []struct {
		name string

		query string

		products []models.Product

		want []string
	}{

		{
			name: "a search drops the pictures",

			query: "cast iron",

			products: []models.Product{
				pictured(0),
				pictured(1),
			},

			want: nil,
		},

		{
			name: "nothing photographed",

			products: []models.Product{
				plain(0),
				plain(1),
			},

			want: nil,
		},

		{
			// One picture cannot cycle, and a band
			// that cannot cycle should be the band
			// that was designed to stand still rather
			// than a one-slide carousel.
			name: "one picture is not a rotation",

			products: []models.Product{
				pictured(0),
			},

			want: nil,
		},

		{
			name: "three pictures, in the order the catalog put them",

			products: []models.Product{
				pictured(0),
				pictured(1),
				pictured(2),
			},

			want: []string{
				"/shop/img/thing-a.jpg",
				"/shop/img/thing-b.jpg",
				"/shop/img/thing-c.jpg",
			},
		},

		{
			// The products without pictures are
			// stepped over rather than counted, or a
			// page of twelve listings where two have
			// been photographed would produce a band
			// of two.
			name: "products without pictures are skipped",

			products: []models.Product{
				pictured(0),
				plain(1),
				pictured(2),
			},

			want: []string{
				"/shop/img/thing-a.jpg",
				"/shop/img/thing-c.jpg",
			},
		},

		{
			// The cap. There is a rule in the
			// stylesheet for two, three and four
			// slides and none for more, so a band
			// handed a fifth picture would stop
			// cycling and stand still. This is where
			// that is not allowed to happen.
			name: "no more than the stylesheet can animate",

			products: []models.Product{
				pictured(0),
				pictured(1),
				pictured(2),
				pictured(3),
				pictured(4),
				pictured(5),
			},

			want: []string{
				"/shop/img/thing-a.jpg",
				"/shop/img/thing-b.jpg",
				"/shop/img/thing-c.jpg",
				"/shop/img/thing-d.jpg",
			},
		},

		{
			name: "an empty catalog",

			products: nil,

			want: nil,
		},
	}

	for _, test := range cases {

		page := catalogPage{
			base: base{Query: test.query},

			Page: utils.Page[models.Product]{
				Data: test.products,
			},
		}

		got := page.HeroSlides()

		if len(got) != len(test.want) {

			t.Errorf(
				"%s: got %d slides, want %d",
				test.name,
				len(got),
				len(test.want),
			)

			continue
		}

		for index := range got {

			if got[index] != test.want[index] {

				t.Errorf(
					"%s: slide %d is %q, want %q",
					test.name,
					index,
					got[index],
					test.want[index],
				)
			}
		}
	}
}
