// This file is the second thing that happens to a
// picture, after it has been attached to a product.
//
// It exists because of what a photograph arriving in the
// folder actually means. The file name says which
// product it is: that is a fact the operator typed. It
// says nothing at all about what the thing is, and a
// shop where the pictures are right and the categories
// are wrong is a shop that is wrong in the way a shopper
// notices, because the categories are the buttons they
// browse with.
//
// So the picture is looked at. A model is asked which of
// the shop's categories it belongs to, and the product is
// moved to that one.
package pictures

import (
	"context"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"e-commerce-backend/services"
	"e-commerce-backend/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

// mediaTypes maps a picture's extension to the type the
// model API is told it is sending.
//
// This list is deliberately not the same as
// imageExtensions, which is the list of things this shop
// will attach and serve, and the difference is the point
// rather than an oversight. The two lists answer
// different questions. imageExtensions answers "can a
// browser show this", and everything on it can. This one
// answers "can the model be sent this", which is a
// shorter list.
//
// So an AVIF picture is attached to its product and shown
// on the tile, and is not sent for classification: the
// product keeps whatever category its seller gave it. It
// is worth knowing which files fall in that gap, and the
// report says so by name rather than leaving it to be
// noticed.
//
// JPEG and PNG are the two worth having. Together they
// are what a phone and a camera produce, which is what
// actually lands in this folder.
var mediaTypes = map[string]string{

	".jpg": "image/jpeg",

	".jpeg": "image/jpeg",

	".png": "image/png",

	".gif": "image/gif",

	".webp": "image/webp",
}

// mediaTypeFor reports the type to send a picture as, and
// whether it can be sent at all.
func mediaTypeFor(name string) (string, bool) {

	mediaType, known := mediaTypes[
		strings.ToLower(path.Ext(name)),
	]

	return mediaType, known
}

// ClassificationReport says what one pass of looking at
// the pictures did.
//
// It is a separate report from Report rather than more
// fields on it, because the two describe different halves
// of the same startup and either can happen without the
// other. Attaching is a fact about the folder; this is a
// fact about what the pictures showed, and it is the half
// that can be switched off by leaving an environment
// variable empty.
//
// Every list holds lines already written out, in the same
// shape Report's lists do, because the only thing that
// ever happens to one is that it is printed.
type ClassificationReport struct {

	// Sorted names the products that came back with a
	// category, and which category. A product that was
	// already in the category the picture suggested is
	// in here too: nothing was written for it, and the
	// picture agreeing with its seller is worth the one
	// line rather than being counted as nothing
	// happening.
	Sorted []string

	// Unclear names the products where the model answered
	// with none of the categories it was given. Nothing
	// is written for these, and the product keeps the
	// category its seller chose.
	Unclear []string

	// Failed names the products whose picture could not
	// be looked at, with the reason. A failure here is
	// never fatal: the picture is attached and shown
	// either way, and only the sorting is missing.
	Failed []string

	// NotSent names the pictures in a format the model
	// cannot be sent, which is the one gap this pass
	// cannot close by trying harder.
	NotSent []string
}

// Classify looks at each newly attached picture and moves
// its product into the category the picture suggests.
//
// It is given the attachments from one pass over the
// folder rather than the folder itself, and that is the
// whole of its idempotency story. A picture that was
// already in place is not an attachment -- the scan
// counted it under Skipped -- so restarting the server
// sends nothing to the model and costs nothing. There is
// no record of what has been classified, because the
// folder and the database already agree about it.
//
// Every failure inside the loop belongs to one picture.
// One unreadable file, one refused call, one unclear
// answer: each is recorded and the next picture is
// tried. A picture that cannot be sorted is still a
// picture, and refusing the whole run over one of them
// would mean the rest never got sorted either.
func Classify(
	pool *pgxpool.Pool,
	client *services.VisionClient,
	dir string,
	attachments []Attachment,
) (ClassificationReport, error) {

	report := ClassificationReport{}

	if len(attachments) == 0 {
		return report, nil
	}

	// The categories are read once for the whole pass
	// rather than once per picture. They are the list the
	// model chooses from and the list its answer is
	// checked against, and both of those want the same
	// list: a category added between two pictures would
	// otherwise be offered to the second and unknown to
	// the check that reads the answer.
	categories, err := storage.ListCategoriesFromDB(pool)

	if err != nil {
		return report, err
	}

	// No categories means nothing to choose from, and a
	// prompt with an empty list is a question with no
	// answers.
	if len(categories) == 0 {
		return report, nil
	}

	options := make(
		[]services.CategoryOption,
		0,
		len(categories),
	)

	bySlug := make(map[string]int, len(categories))

	for _, category := range categories {

		options = append(
			options,
			services.CategoryOption{
				Slug: category.Slug,

				Name: category.Name,
			},
		)

		bySlug[category.Slug] = category.ID
	}

	for _, attachment := range attachments {

		mediaType, known := mediaTypeFor(attachment.Name)

		if !known {

			extension := strings.TrimPrefix(
				strings.ToLower(
					path.Ext(attachment.Name),
				),
				".",
			)

			report.NotSent = append(
				report.NotSent,
				attachment.Name+" ("+extension+")",
			)

			continue
		}

		image, err := os.ReadFile(
			filepath.Join(dir, attachment.Name),
		)

		if err != nil {

			report.Failed = append(
				report.Failed,
				attachment.Slug+": the picture could not be read: "+
					err.Error(),
			)

			continue
		}

		// The call is bounded by the client's own
		// timeout rather than by a second deadline
		// here. Two deadlines for one call means the
		// shorter one wins and the longer one is a
		// comment pretending to be a guarantee.
		slug, err := client.ChooseCategory(
			context.Background(),
			image,
			mediaType,
			options,
		)

		if err != nil {

			report.Failed = append(
				report.Failed,
				attachment.Slug+": "+err.Error(),
			)

			continue
		}

		categoryID, known := bySlug[slug]

		if !known {

			report.Unclear = append(
				report.Unclear,
				attachment.Slug+": the picture matched none of the categories",
			)

			continue
		}

		changed, err := storage.SetProductCategoryInDB(
			pool,
			attachment.ProductID,
			categoryID,
		)

		if err != nil {
			return report, err
		}

		if changed {

			report.Sorted = append(
				report.Sorted,
				attachment.Slug+" -> "+slug,
			)

		} else {

			report.Sorted = append(
				report.Sorted,
				attachment.Slug+" was already "+slug,
			)
		}
	}

	// The same rule the folder's report follows: two runs
	// that did the same thing should print the same lines
	// in the same order, or comparing them tells you
	// nothing.
	sort.Strings(report.Sorted)
	sort.Strings(report.Unclear)
	sort.Strings(report.Failed)
	sort.Strings(report.NotSent)

	return report, nil
}

// Log writes the report.
//
// A pass that sorted nothing, found nothing unclear and
// failed on nothing writes no lines at all. That is the
// ordinary case -- a startup where no picture was added --
// and a line saying so on every start would be noise
// around the lines that matter.
func (r ClassificationReport) Log() {

	if len(r.Sorted) == 0 &&
		len(r.Unclear) == 0 &&
		len(r.Failed) == 0 &&
		len(r.NotSent) == 0 {

		return
	}

	log.Printf(
		"pictures: %d sorted into a category, %d unclear, %d failed, %d not looked at",
		len(r.Sorted),
		len(r.Unclear),
		len(r.Failed),
		len(r.NotSent),
	)

	for _, line := range r.Sorted {
		log.Printf("pictures: sorted %s", line)
	}

	for _, line := range r.Unclear {
		log.Printf("pictures: unclear %s", line)
	}

	for _, line := range r.Failed {
		log.Printf("pictures: failed %s", line)
	}

	for _, line := range r.NotSent {

		log.Printf(
			"pictures: %s was not looked at. The model is only sent %s; the picture is still attached and still shown.",
			line,
			sentFormats(),
		)
	}
}

// sentFormats names the picture types the model is sent,
// for the line that explains why one was not.
//
// The list is built from mediaTypes rather than written
// into the sentence, so that adding a format to that map
// cannot leave the explanation saying otherwise. A
// message that disagrees with the code it is describing
// is worse than no message at all, and this is the kind
// of message that goes stale without anybody noticing.
//
// The duplicates are folded together: .jpg and .jpeg are
// two extensions and one format, and an operator reading
// this wants to know which formats are sent.
func sentFormats() string {

	seen := make(map[string]bool, len(mediaTypes))

	formats := make([]string, 0, len(mediaTypes))

	for _, mediaType := range mediaTypes {

		if seen[mediaType] {
			continue
		}

		seen[mediaType] = true

		formats = append(
			formats,
			strings.ToUpper(
				strings.TrimPrefix(
					mediaType,
					"image/",
				),
			),
		)
	}

	sort.Strings(formats)

	return strings.Join(formats, ", ")
}
