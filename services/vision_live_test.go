package services

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strings"
	"testing"
)

// TestChooseCategoryAgainstTheRealAPI is the one test in
// this package that talks to the model API.
//
// It skips unless ANTHROPIC_API_KEY is set, which is the
// arrangement the database-backed tests use with
// TEST_DATABASE_URL, and for the same reason: the thing
// it needs is a credential that most machines do not
// have, and a test that fails for want of a credential is
// a test people learn to ignore.
//
// It is worth having because everything else in this
// package is checked against a stand-in server, and a
// stand-in agrees with whatever it is told. The rest of
// the tests pin what this client *sends*; only this one
// finds out whether the API accepts it.
//
// What it checks is narrow and worth being exact about.
// It does not check that the model is right about the
// picture -- the picture is flat colour and has no right
// answer. It checks that the request this client builds
// is one the API will take: that the endpoint is where
// this code thinks it is, that the two headers are the
// ones it wants, that a picture sent as base64 in that
// body shape is understood, and that the reply parses.
// Every one of those arriving wrong shows up as an error
// here and as nothing at all against a stand-in.
func TestChooseCategoryAgainstTheRealAPI(t *testing.T) {

	if strings.TrimSpace(
		os.Getenv("ANTHROPIC_API_KEY"),
	) == "" {

		t.Skip(
			"ANTHROPIC_API_KEY is not set: skipping the live call",
		)
	}

	client := NewVisionClientFromEnv()

	options := []CategoryOption{
		{Slug: "home-kitchen", Name: "Home & Kitchen"},

		{Slug: "computers", Name: "Computers"},

		{Slug: "fashion", Name: "Fashion"},
	}

	slug, err := client.ChooseCategory(
		context.Background(),
		flatJPEG(t),
		"image/jpeg",
		options,
	)

	if err != nil {

		t.Fatalf(
			"the live call was refused: %v\n\n"+
				"A failure here is about the shape of the request rather than about the picture. "+
				"Check the endpoint, the two headers and the body against the API's documentation.",
			err,
		)
	}

	// The answer is reported whether or not it is one of
	// the options, because it is the interesting part of
	// running this: it is the only place anybody sees
	// what the model makes of a picture.
	t.Logf(
		"the model answered %q for a flat image of %s",
		slug,
		strings.Join(slugsOf(options), ", "),
	)

	// An empty answer is correct here: flat colour is not
	// a product in any of the three categories, and
	// matchCategory is what turns "nothing fits" into
	// nothing. What must not happen is an answer that is
	// neither.
	if slug == "" {
		return
	}

	for _, option := range options {

		if slug == option.Slug {
			return
		}
	}

	t.Errorf(
		"the model answered %q, which is not one of the options and not empty; matchCategory should have discarded it",
		slug,
	)
}

// slugsOf is the option slugs, for the log line.
func slugsOf(options []CategoryOption) []string {

	slugs := make([]string, 0, len(options))

	for _, option := range options {
		slugs = append(slugs, option.Slug)
	}

	return slugs
}

// flatJPEG builds a real JPEG, in memory.
//
// It is generated rather than read from disk so that this
// test does not depend on anybody's photography folder
// being present, and it is a real encoding rather than a
// handful of magic bytes so that the API has something it
// can actually decode. A truncated file would be refused
// for a reason that has nothing to do with the shape of
// the request, which is the one thing this test exists to
// check.
//
// The three bands are there so that the picture is not a
// single flat colour. What they are is not a product and
// is not meant to be.
func flatJPEG(t *testing.T) []byte {

	t.Helper()

	canvas := image.NewRGBA(
		image.Rect(0, 0, 64, 64),
	)

	bands := []color.RGBA{
		{R: 0xc0, G: 0x7a, B: 0x4a, A: 0xff},

		{R: 0x3a, G: 0x4a, B: 0x5a, A: 0xff},

		{R: 0xe8, G: 0xe2, B: 0xd8, A: 0xff},
	}

	for y := 0; y < 64; y++ {

		band := bands[y*len(bands)/64]

		for x := 0; x < 64; x++ {
			canvas.Set(x, y, band)
		}
	}

	var buffer bytes.Buffer

	err := jpeg.Encode(&buffer, canvas, nil)

	if err != nil {
		t.Fatalf("could not build the test picture: %v", err)
	}

	return buffer.Bytes()
}
