package pictures

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestSlugForFile pins the rule that connects a picture
// to a product.
//
// This is the only thing an operator of this shop has to
// get right, and it is the thing they cannot get feedback
// on from anywhere else: a file named slightly wrong is
// not an error, it is a picture that quietly belongs to
// nothing. So the rule is written down here in the cases
// somebody will actually type.
func TestSlugForFile(t *testing.T) {

	cases := []struct {
		file string

		slug string
	}{

		// The plain case, and the one the README asks
		// for: the file is named after the slug.
		{"cast-iron-pot.jpg", "cast-iron-pot"},

		// Named after the product's title instead. The
		// slug is made from the name by the same
		// reduction, so this has to meet in the middle,
		// which is why the case of the letters and the
		// spaces in the file name do not matter.
		{"Cast Iron Pot.JPG", "cast-iron-pot"},

		// An ampersand, which is what a category name
		// looks like and what a file name may not always
		// be allowed to hold.
		{"Home & Kitchen Pot.png", "home-kitchen-pot"},

		// A name that already looks like a slug is left
		// exactly as it is, so re-saving a file cannot
		// change which product it belongs to.
		{"cast-iron-pot.jpeg", "cast-iron-pot"},

		// Only the last extension is dropped. This one
		// is written down because it is the case where
		// an operator's expectation and the code could
		// reasonably differ, and the answer is that
		// "pot.tar.gz" is a picture of "pot-tar".
		{"pot.tar.gz", "pot-tar"},

		// A name with nothing slug-like in it reduces to
		// nothing, which the scan treats as a file that
		// matched no product rather than as a product
		// with an empty name.
		{"___.jpg", ""},
	}

	for _, test := range cases {

		got := slugForFile(test.file)

		if got != test.slug {

			t.Errorf(
				"slugForFile(%q) = %q, want %q",
				test.file,
				got,
				test.slug,
			)
		}
	}
}

// TestIsImage checks the list that decides what counts
// as a picture.
//
// The SVG case is the one that matters. An SVG can carry
// script and would be served from this site's own origin,
// so accepting one would turn a product photograph into a
// way to run code in a visitor's browser. It is refused,
// and this test is where that decision is recorded.
func TestIsImage(t *testing.T) {

	accepted := []string{
		"pot.jpg",
		"pot.JPG",
		"pot.jpeg",
		"pot.png",
		"pot.webp",
		"pot.gif",
		"pot.avif",
		"/shop/img/pot.jpg",
	}

	for _, name := range accepted {

		if !isImage(name) {

			t.Errorf(
				"isImage(%q) = false, want true",
				name,
			)
		}
	}

	refused := []string{
		"pot.svg",

		// An SVG renamed to a JPEG extension is caught
		// here too, but only because the extension is
		// what is checked; what a file actually contains
		// is the browser's problem and not this
		// package's. What is being refused is publishing
		// anything whose type is not a picture.
		"pot.txt",
		"pot",
		"pot.jpg.txt",
		"",
	}

	for _, name := range refused {

		if isImage(name) {

			t.Errorf(
				"isImage(%q) = true, want false",
				name,
			)
		}
	}
}

// TestMediaTypeFor pins the second list, and the
// relationship between the two lists.
//
// There are two maps of extensions in this package and
// they are deliberately not the same one. imageExtensions
// answers "will this shop attach and serve this file",
// and mediaTypes answers "can this file be sent to the
// model to be sorted into a category". AVIF is on the
// first and not on the second, and that gap is the reason
// the report has a NotSent list at all.
//
// The direction that must hold is the one asserted below:
// everything this package would send is something it
// would serve. The reverse is allowed to be false, and is
// false today. If it were ever the other way round, the
// shop would be asking a model about a file it refuses to
// publish, which could only end in a product sorted by a
// picture no shopper can see.
func TestMediaTypeFor(t *testing.T) {

	cases := []struct {
		file string

		mediaType string

		known bool
	}{

		{"pot.jpg", "image/jpeg", true},

		// A camera that writes .JPG and a phone that
		// writes .jpg are the same format, and the case
		// of the extension is not information.
		{"POT.JPG", "image/jpeg", true},

		{"pot.jpeg", "image/jpeg", true},

		{"pot.png", "image/png", true},

		{"pot.webp", "image/webp", true},

		{"pot.gif", "image/gif", true},

		// Served, and not sent. The product keeps
		// whatever category its seller gave it.
		{"pot.avif", "", false},

		// Not a picture at all, so it was never
		// attached and never reached this step.
		{"pot.svg", "", false},

		{"pot", "", false},
	}

	for _, test := range cases {

		mediaType, known := mediaTypeFor(test.file)

		if known != test.known {

			t.Errorf(
				"mediaTypeFor(%q) known = %v, want %v",
				test.file,
				known,
				test.known,
			)

			continue
		}

		if mediaType != test.mediaType {

			t.Errorf(
				"mediaTypeFor(%q) = %q, want %q",
				test.file,
				mediaType,
				test.mediaType,
			)
		}
	}

	// The invariant, checked over the map itself rather
	// than over the cases above, so that an extension
	// added to one list and not the other is caught
	// wherever it is added.
	for extension := range mediaTypes {

		if !imageExtensions[extension] {

			t.Errorf(
				"%s would be sent to the model and never attached to a product",
				extension,
			)
		}
	}
}

// TestMaySort pins the rule that decides whether a
// picture is allowed to move its product.
//
// This is the safety property of the classification pass,
// and it is worth being exact about what it protects.
// products.category_id is NOT NULL and the create handler
// refuses a product that arrives without a category, so
// every listing already sits somewhere its seller put it.
// The only thing a picture may do is take a product out
// of the catch-all, whose meaning is "nobody has decided
// yet". A picture is never allowed to overrule a person.
//
// The last case is the one that decides the design. A
// shop with no catch-all gets nothing sorted: the
// conservative direction is the right one, because the
// alternative is a pass that treats every category as
// fair game the moment somebody renames a category. The
// report counts the pictures it left alone, so an
// operator in that position is told why nothing happened
// rather than left to guess.
func TestMaySort(t *testing.T) {

	// Standing in for real category ids. What matters
	// about the numbers is only that neither is zero,
	// because zero is how "there is no catch-all" is
	// spelled.
	const (
		unsorted = 7

		fashion = 3
	)

	attachment := func(categoryID int) Attachment {

		return Attachment{
			Name: "cast-iron-pot.jpg",

			ProductID: 1,

			Slug: "cast-iron-pot",

			CategoryID: categoryID,
		}
	}

	cases := []struct {
		name string

		categoryID int

		unsortedID int

		want bool
	}{

		// The product nobody has sorted, which is the
		// whole of what this pass is for.
		{
			"a product in the catch-all is sorted",

			unsorted,

			unsorted,

			true,
		},

		// A seller who chose. The picture has an opinion
		// and does not get to act on it.
		{
			"a product in a chosen category is left alone",

			fashion,

			unsorted,

			false,
		},

		// No catch-all in this shop, so nothing is marked
		// as unsorted and nothing is moved.
		{
			"no catch-all means nothing is sorted",

			fashion,

			0,

			false,
		},

		// The case the first guard exists for. Without
		// it, "category 0 equals catch-all 0" would be
		// true, and a shop that deleted its catch-all
		// would sort everything rather than nothing --
		// the opposite of what the rule is for. No
		// product has category zero, so this cannot come
		// up in the database; it comes up when a missing
		// category is represented by the zero value, which
		// is exactly what the map lookup produces.
		{
			"a missing catch-all does not match a missing category",

			0,

			0,

			false,
		},
	}

	for _, test := range cases {

		got := maySort(
			attachment(test.categoryID),
			test.unsortedID,
		)

		if got != test.want {

			t.Errorf(
				"%s: maySort(category %d, catch-all %d) = %v, want %v",
				test.name,
				test.categoryID,
				test.unsortedID,
				got,
				test.want,
			)
		}
	}
}

// TestServeServesPicturesAndNothingElse asks the handler
// what it will hand out.
//
// The folder is somewhere a person can leave anything at
// all, so the question is not only whether a picture is
// served but whether everything else is refused. A file
// server pointed at this folder would answer all four of
// the refusals below with a 200.
func TestServeServesPicturesAndNothingElse(t *testing.T) {

	dir := t.TempDir()

	write := func(name string) {

		err := os.WriteFile(
			filepath.Join(dir, name),
			[]byte("not really a picture"),
			0o600,
		)

		if err != nil {
			t.Fatalf("could not write %s: %v", name, err)
		}
	}

	write("pot.jpg")

	write("notes.txt")

	handler := serve(dir)

	cases := []struct {
		path string

		status int
	}{

		// The picture itself.
		{"/shop/img/pot.jpg", http.StatusOK},

		// A file that is in the folder but is not a
		// picture. It exists; this site simply does not
		// publish it.
		{"/shop/img/notes.txt", http.StatusNotFound},

		// A directory, which a file server answers with
		// a listing of everything in the folder.
		{"/shop/img/", http.StatusNotFound},

		// A file that is not there. This is the ordinary
		// miss, and it has to look exactly like the two
		// refusals above: a shop that says "forbidden"
		// to one name and "not found" to another is a
		// shop that can be asked which files exist.
		{"/shop/img/missing.jpg", http.StatusNotFound},

		// An SVG, which is the refusal that is doing
		// real work rather than tidying up.
		{"/shop/img/pot.svg", http.StatusNotFound},
	}

	for _, test := range cases {

		request := httptest.NewRequest(
			http.MethodGet,
			test.path,
			nil,
		)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		if recorder.Code != test.status {

			t.Errorf(
				"GET %s: status %d, want %d",
				test.path,
				recorder.Code,
				test.status,
			)
		}
	}
}
