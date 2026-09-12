// Package pictures attaches the photographs in a folder
// to the products they belong to.
//
// The folder is the whole interface. Somebody drops a
// picture into it, names the file after the product's
// slug, and restarts the server. There is no upload
// endpoint and no screen for managing pictures, because
// the person who has the photographs already has a file
// manager, and a marketplace's pictures are not something
// a shopper sends.
//
// The folder is read at startup and served straight off
// disk. That is a deliberate exception to the rule the
// rest of the storefront follows, which is that
// everything it serves is compiled into the binary. A
// picture that was compiled in could not be added without
// rebuilding, and photographs are exactly the thing that
// turns up after the code is finished. The one picture
// that is embedded is the hero band, which is part of the
// design rather than part of the catalog.
package pictures

import (
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// URLPrefix is where the folder is served from.
	//
	// It sits under /shop because it is part of the
	// storefront, and next to /shop/static because it is
	// the exact opposite of it: static is compiled into
	// the binary and can never change, and this is read
	// from disk and changes whenever somebody adds a
	// picture.
	URLPrefix = "/shop/img/"

	// DefaultDir is the folder read when PICTURES_DIR
	// says nothing.
	DefaultDir = "pictures"
)

// Dir is the folder the pictures are read from and
// served out of.
//
// It is read from the environment each time rather than
// captured in a variable at startup, so that a test can
// point it somewhere temporary without the package
// keeping state between cases.
func Dir() string {

	dir := strings.TrimSpace(
		os.Getenv("PICTURES_DIR"),
	)

	if dir == "" {
		return DefaultDir
	}

	return dir
}

// imageExtensions are the file types treated as pictures.
//
// The same list decides what gets attached to a product
// and what will be handed to a browser. That is on
// purpose: a folder is a place where somebody can leave
// anything at all, and two lists would eventually
// disagree about something, in the direction where a
// file the scan accepted is one the server refuses to
// serve. One list cannot.
//
// SVG is not on it, and its absence is the entry worth
// explaining. An SVG is a document that can contain
// script rather than a picture format, and it would be
// served from this site's own origin, where a script
// inside it would run with the same authority as the
// shop's own pages. That is a way to turn "add a
// product photo" into "run code on every visitor's
// browser", and no convenience is worth it. A JPEG
// cannot do that.
var imageExtensions = map[string]bool{

	".jpg": true,

	".jpeg": true,

	".png": true,

	".webp": true,

	".gif": true,

	".avif": true,
}

// isImage reports whether a name or a path ends in one
// of the picture extensions.
//
// The extension is lowercased before it is looked up, so
// that a camera that writes .JPG and a phone that writes
// .jpg are the same thing. Case is not information here.
func isImage(name string) bool {

	return imageExtensions[
		strings.ToLower(path.Ext(name)),
	]
}

// Attachment is one picture that a pass over the folder
// put in place.
//
// It carries the product's id, slug and category as well
// as the file name, because attaching a picture is not
// the only thing that happens to one. The picture is also
// looked at, to work out which category its product
// belongs in, and that step needs to know which product
// it is looking at and where that product sits now.
// Doing it from this record rather than by re-reading the
// folder is what keeps the two steps talking about the
// same set of pictures.
type Attachment struct {

	// Name is the file, as it was found.
	Name string

	// ProductID is the product it was attached to.
	ProductID int

	// Slug is that product's slug, which is the name the
	// file was matched by and the name worth printing.
	Slug string

	// CategoryID is the category the product was in when
	// the picture was attached.
	//
	// It is carried here rather than looked up again
	// because the lookup has already happened: this
	// record is built from the product row the scan read
	// to find out whose picture this is, and the category
	// came back with it. Classification uses it to tell a
	// product nobody has sorted from one its seller
	// placed, and asking the database again there would
	// be a second query for a question the first one
	// already answered.
	CategoryID int
}

// String renders the attachment the way the log prints
// it, so that a line naming a picture also names the
// product it landed on.
func (a Attachment) String() string {

	return a.Name + " -> " + a.Slug
}

// Report says what one pass over the folder did.
//
// It exists because this package's failure mode is
// silence. A picture whose file name is spelled slightly
// differently from the product's slug is not an error
// anywhere in this program: it is a file nothing ever
// referred to. If the scan did not say so, the operator
// would be looking at a shop with a missing picture and
// nothing at all to explain it.
type Report struct {

	// Attached and Removed name the things that changed,
	// so the log can point at them.
	//
	// Attached is a list of records rather than of
	// strings because the classification step runs over
	// exactly these products and nothing else. A picture
	// that was already in place is counted under Skipped
	// instead, which is what makes restarting the server
	// free: no writes, and nothing looked at twice.
	Attached []Attachment

	Removed []string

	// Unmatched names the files that belong to no
	// product. This is the list an operator actually
	// needs, and it is the reason the report exists.
	Unmatched []string

	// Conflicts names the files that matched a product
	// another file had already claimed.
	Conflicts []string

	// Skipped counts the pictures that were already in
	// place. It is a number rather than a list because
	// on a folder that has not changed it is every
	// picture there is, and printing them all on every
	// start would bury the lines that matter.
	Skipped int

	// FolderMissing is true when there was no folder to
	// read at all.
	FolderMissing bool
}

// withoutExtension drops the last extension from a file
// name, so that "cast-iron-pot.jpg" becomes
// "cast-iron-pot".
func withoutExtension(name string) string {

	return strings.TrimSuffix(
		name,
		path.Ext(name),
	)
}

// slugForFile is the rule that connects a picture to a
// product, written down once.
//
// It is a function of its own rather than an expression
// inside the scan because it is the whole convention this
// package asks an operator to follow, and a convention is
// worth being able to read on its own and test on its
// own. Everything else in Attach is bookkeeping around
// this one line.
//
// The reduction is deliberately the same one the API
// applies to a product's name when it creates the slug
// (see utils.Slugify, and the create handler that calls
// it), so "Cast Iron Pot.JPG" and "Cast Iron Pot" meet.
func slugForFile(name string) string {

	return utils.Slugify(
		withoutExtension(name),
	)
}

// Attach reads the folder and points every product at
// its picture.
//
// A file finds its product by its name. The file
// "cast-iron-pot.jpg" is reduced to "cast-iron-pot" and
// looked up against products.slug, which is the same
// name the product already appears under in every URL,
// so the rule an operator has to remember is the rule
// they can read off any product page.
//
// Running it twice does nothing the second time. A
// picture that is already in place is counted and left
// alone, so restarting the server neither writes to the
// database nor changes an updated_at timestamp, and the
// scan is safe to run on every start.
func Attach(
	pool *pgxpool.Pool,
	dir string,
) (Report, error) {

	report := Report{}

	entries, err := os.ReadDir(dir)

	if err != nil {

		// A folder that is not there is not a failure.
		// Shops have no pictures before somebody takes
		// some, and the folder is expected to appear
		// later.
		if errors.Is(err, fs.ErrNotExist) {

			report.FolderMissing = true

			return report, nil
		}

		return report, err
	}

	// claimed remembers which product each slug has
	// already been matched to, so that two files wanting
	// the same product are noticed rather than silently
	// fighting over it.
	claimed := make(map[string]string)

	// served remembers which addresses this pass saw, so
	// that the check at the end can tell what has gone
	// without looking at the disk a second time.
	served := make(map[string]bool)

	// ReadDir returns entries sorted by name, which is
	// what makes a clash resolve the same way on every
	// run rather than depending on the order the
	// filesystem happened to hand them over.
	for _, entry := range entries {

		// Subfolders are not walked. The folder is flat
		// on purpose: the picture's address is the
		// folder plus its own name, and a subfolder
		// would put a second decision between the two.
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		if !isImage(name) {
			continue
		}

		slug := slugForFile(name)

		// A name made entirely of punctuation reduces to
		// nothing, and an empty slug would match no
		// product in a way that looks like a bug rather
		// than like a strange file name.
		if slug == "" {

			report.Unmatched = append(
				report.Unmatched,
				name,
			)

			continue
		}

		if other, taken := claimed[slug]; taken {

			report.Conflicts = append(
				report.Conflicts,
				name+" and "+other+
					" both match "+slug,
			)

			continue
		}

		product, err := storage.GetProductBySlugFromDB(
			pool,
			slug,
		)

		if err != nil {

			// A slug that matches nothing is an ordinary
			// outcome: the folder holds a picture of
			// something the shop does not sell, or the
			// file is named after the product's title
			// rather than its slug.
			if errors.Is(err, pgx.ErrNoRows) {

				report.Unmatched = append(
					report.Unmatched,
					name,
				)

				continue
			}

			return report, err
		}

		claimed[slug] = name

		// The address is built from the prefix and the
		// file's own name, escaped, so that a name with
		// a space or an ampersand in it still produces a
		// URL a browser will fetch correctly. It is
		// built here and stored in the products table,
		// so that the browser is handed the address the
		// picture was actually attached at, and a
		// product's picture does not depend on the
		// server's route table being the same as it was
		// the day the picture arrived.
		address := URLPrefix + url.PathEscape(name)

		served[address] = true

		if product.ImagePath == address {

			report.Skipped++

			continue
		}

		changed, err := storage.SetProductImageInDB(
			pool,
			product.ID,
			address,
		)

		if err != nil {
			return report, err
		}

		if changed {

			report.Attached = append(
				report.Attached,
				Attachment{
					Name: name,

					ProductID: product.ID,

					Slug: product.Slug,

					CategoryID: product.CategoryID,
				},
			)
		}
	}

	err = clearMissingImages(pool, served, &report)

	if err != nil {
		return report, err
	}

	// Every list is sorted before it leaves. A report
	// whose lines come out in a different order on every
	// start is a report nobody can compare with the last
	// one, and comparing the last one is the whole point
	// of printing it.
	//
	// The attachments are sorted by file name, which is
	// the order they were read in and so the order the
	// lines have always come out in.
	sort.Slice(
		report.Attached,
		func(i, j int) bool {

			return report.Attached[i].Name <
				report.Attached[j].Name
		},
	)

	sort.Strings(report.Removed)
	sort.Strings(report.Unmatched)
	sort.Strings(report.Conflicts)

	return report, nil
}

// clearMissingImages unpoints the products whose picture
// is no longer in the folder.
//
// Without this, deleting a photograph would leave every
// page showing a broken image for a file that is not
// there, and the folder would stop being the place that
// decides what a product looks like. With it, the folder
// is the only thing an operator has to think about: a
// picture that is in it is shown, and a picture that is
// not is not.
//
// Only addresses under this package's own prefix are
// considered. A product pointed somewhere else was
// pointed there by something that is not this scan, and
// this scan has no business overruling a decision it
// knows nothing about.
func clearMissingImages(
	pool *pgxpool.Pool,
	served map[string]bool,
	report *Report,
) error {

	images, err := storage.ListProductImagesFromDB(pool)

	if err != nil {
		return err
	}

	for id, address := range images {

		if !strings.HasPrefix(address, URLPrefix) {
			continue
		}

		if served[address] {
			continue
		}

		changed, err := storage.SetProductImageInDB(
			pool,
			id,
			"",
		)

		if err != nil {
			return err
		}

		if changed {
			report.Removed = append(
				report.Removed,
				address,
			)
		}
	}

	return nil
}

// RegisterRoutes serves the folder over HTTP.
func RegisterRoutes(
	mux *http.ServeMux,
	dir string,
) {

	mux.Handle(
		"GET "+URLPrefix,
		serve(dir),
	)
}

// serve answers requests for one picture, and nothing
// else.
//
// It is deliberately narrower than a file server. The
// folder is somewhere a person can leave anything at
// all, and a file server pointed at it would hand out
// whatever it found there.
func serve(dir string) http.Handler {

	files := http.StripPrefix(
		URLPrefix,
		http.FileServer(http.Dir(dir)),
	)

	return http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			// A path that ends in a slash is a folder,
			// and a file server answers a folder with a
			// listing of everything inside it. The names
			// of the pictures are not secret, but a
			// listing is a page this site never meant to
			// publish, and not having one costs a line.
			if strings.HasSuffix(r.URL.Path, "/") {

				http.NotFound(w, r)

				return
			}

			// A request for anything that is not a
			// picture is answered as though it were not
			// there, which is what it is as far as this
			// site is concerned. The file may well exist;
			// it is simply not something this shop
			// publishes.
			if !isImage(r.URL.Path) {

				http.NotFound(w, r)

				return
			}

			files.ServeHTTP(w, r)
		},
	)
}

// Log writes the report.
//
// Everything that did not match is printed in full
// rather than counted, because it is the only clue an
// operator gets. A picture attached to nothing is not an
// error anywhere in this program: if this did not name
// it, the file would simply never appear anywhere, and
// there would be nothing to go and look at.
func (r Report) Log() {

	if r.FolderMissing {

		log.Printf(
			"no %s folder, so no product pictures were attached. Set PICTURES_DIR to read a different folder.",
			Dir(),
		)

		return
	}

	log.Printf(
		"pictures: %d attached, %d already in place, %d removed, %d unmatched, %d clashing",
		len(r.Attached),
		r.Skipped,
		len(r.Removed),
		len(r.Unmatched),
		len(r.Conflicts),
	)

	for _, attachment := range r.Attached {
		log.Printf("pictures: attached %s", attachment)
	}

	for _, name := range r.Removed {
		log.Printf("pictures: removed %s", name)
	}

	for _, name := range r.Conflicts {
		log.Printf("pictures: %s", name)
	}

	for _, name := range r.Unmatched {

		log.Printf(
			"pictures: %s matches no product. Name it after a product's slug, as printed by: go run ./database/query \"SELECT slug FROM products\"",
			name,
		)
	}
}
