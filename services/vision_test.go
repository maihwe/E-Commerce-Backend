package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// options are the categories the tests choose from. They
// are two of the ten the shop seeds, which is enough to
// tell "matched" apart from "did not match" without
// writing out a list nobody reads.
var testOptions = []CategoryOption{
	{Slug: "home-kitchen", Name: "Home & Kitchen"},

	{Slug: "fashion", Name: "Fashion"},
}

// recording keeps what the stand-in server was sent.
//
// Everything is copied out inside the handler rather than
// the request being held on to, which is not tidiness.
// Two things make holding it wrong: the handler runs on
// the server's own goroutine and the test reads on its
// own, and the request body is not readable once the
// handler has returned, because the server has moved on
// to the next request on that connection by then. So the
// handler reads the body while it is still its body, and
// a mutex is what puts the read on the test's side after
// the write on the handler's side.
type recording struct {
	mutex sync.Mutex

	method string

	path string

	header http.Header

	body []byte
}

// record copies one request out of the handler.
func (rec *recording) record(r *http.Request) {

	body, err := io.ReadAll(r.Body)

	if err != nil {
		body = nil
	}

	rec.mutex.Lock()

	defer rec.mutex.Unlock()

	rec.method = r.Method

	rec.path = r.URL.Path

	rec.header = r.Header.Clone()

	rec.body = body
}

// Method, Path, Header and Body read back what was
// recorded.
func (rec *recording) Method() string {

	rec.mutex.Lock()

	defer rec.mutex.Unlock()

	return rec.method
}

func (rec *recording) Path() string {

	rec.mutex.Lock()

	defer rec.mutex.Unlock()

	return rec.path
}

func (rec *recording) Header() http.Header {

	rec.mutex.Lock()

	defer rec.mutex.Unlock()

	return rec.header
}

func (rec *recording) Body() []byte {

	rec.mutex.Lock()

	defer rec.mutex.Unlock()

	return rec.body
}

// visionServer stands in for the model API.
//
// It answers with whatever it is told to answer with, and
// records the request it was sent, so that a test can look
// at what this client actually put on the wire rather than
// only at what it made of the reply.
func visionServer(
	t *testing.T,
	status int,
	body string,
) (*VisionClient, *recording) {

	t.Helper()

	seen := &recording{}

	server := httptest.NewServer(
		http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {

				seen.record(r)

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				w.WriteHeader(status)

				w.Write([]byte(body))
			},
		),
	)

	t.Cleanup(server.Close)

	return &VisionClient{
		APIKey: "test-key",

		BaseURL: server.URL,

		Model: DefaultVisionModel,

		HTTPClient: server.Client(),
	}, seen
}

// reply builds a response body carrying one text block.
func reply(answer string) string {

	return `{"content":[{"type":"text","text":` +
		mustJSON(answer) + `}]}`
}

// mustJSON encodes a string as a JSON literal, so that an
// answer containing a quote or a newline does not have to
// be escaped by hand in every case below.
func mustJSON(value string) string {

	encoded, err := json.Marshal(value)

	if err != nil {
		panic(err)
	}

	return string(encoded)
}

// TestMatchCategory pins what an answer is allowed to
// become.
//
// This is the function the whole file's safety rests on.
// The model is given a fixed list of ten categories and
// its reply is looked up in that list, so a reply that is
// not one of them has to come back empty. Every case here
// is a thing a model does to a word without meaning
// anything by it, plus the cases where it means something
// else entirely.
//
// A near miss accepted is a product filed in the wrong
// part of the shop. A near miss refused is one product
// left exactly where its seller put it, which is why the
// trimming is narrow and the comparison is not.
func TestMatchCategory(t *testing.T) {

	cases := []struct {
		name string

		answer string

		want string
	}{

		{
			name: "the slug on its own",

			answer: "home-kitchen",

			want: "home-kitchen",
		},

		{
			name: "with the whitespace of a chatty reply",

			answer: "\n  home-kitchen  \n",

			want: "home-kitchen",
		},

		{
			name: "wearing a full stop",

			answer: "fashion.",

			want: "fashion",
		},

		{
			name: "in the wrong case",

			answer: "Home-Kitchen",

			want: "home-kitchen",
		},

		{
			name: "in quotes",

			answer: "\"fashion\"",

			want: "fashion",
		},

		{
			// The one that matters. A model that
			// explains itself must not have its
			// explanation read as an answer, or the
			// column ends up holding a sentence.
			name: "a paragraph of reasoning",

			answer: "This looks like a cooking pot, so I would say home-kitchen is the best fit.",

			want: "",
		},

		{
			name: "the word none",

			answer: "none",

			want: "",
		},

		{
			name: "a category the shop does not have",

			answer: "kitchenware",

			want: "",
		},

		{
			// The display name is not the slug, and
			// only the slug is a key.
			name: "the label rather than the slug",

			answer: "Home & Kitchen",

			want: "",
		},

		{
			name: "nothing at all",

			answer: "",

			want: "",
		},
	}

	for _, test := range cases {

		got := matchCategory(
			test.answer,
			testOptions,
		)

		if got != test.want {

			t.Errorf(
				"%s: matchCategory(%q) = %q, want %q",
				test.name,
				test.answer,
				got,
				test.want,
			)
		}
	}
}

// TestChooseCategorySendsWhatTheAPIExpects checks the
// request this client puts on the wire.
//
// It is worth checking in full because the call cannot be
// made against the real API in this project's tests, the
// same way the Paystack calls cannot: there are no
// credentials here and there never will be. So the shape
// of the request is the thing that can be pinned, and it
// is the thing that would be wrong if this client were
// written against a remembered version of the API rather
// than a current one.
func TestChooseCategorySendsWhatTheAPIExpects(t *testing.T) {

	client, seen := visionServer(
		t,
		200,
		reply("home-kitchen"),
	)

	image := []byte{0xff, 0xd8, 0xff, 0xe0}

	slug, err := client.ChooseCategory(
		context.Background(),
		image,
		"image/jpeg",
		testOptions,
	)

	if err != nil {
		t.Fatalf("ChooseCategory: %v", err)
	}

	if slug != "home-kitchen" {

		t.Fatalf(
			"got %q, want %q",
			slug,
			"home-kitchen",
		)
	}

	if seen.Body() == nil {
		t.Fatal("the request never reached the server")
	}

	if got := seen.Method(); got != http.MethodPost {

		t.Errorf(
			"method is %s, want %s",
			got,
			http.MethodPost,
		)
	}

	if got := seen.Path(); got != "/v1/messages" {

		t.Errorf(
			"path is %s, want /v1/messages",
			got,
		)
	}

	headers := map[string]string{
		"X-Api-Key":         "test-key",
		"Anthropic-Version": anthropicVersion,
		"Content-Type":      "application/json",
	}

	for name, want := range headers {

		if got := seen.Header().Get(name); got != want {

			t.Errorf(
				"header %s is %q, want %q",
				name,
				got,
				want,
			)
		}
	}

	var sent visionRequest

	err = json.Unmarshal(seen.Body(), &sent)

	if err != nil {
		t.Fatalf("the request body is not JSON: %v", err)
	}

	if sent.Model != DefaultVisionModel {

		t.Errorf(
			"model is %q, want %q",
			sent.Model,
			DefaultVisionModel,
		)
	}

	if sent.MaxTokens < 1 {

		t.Errorf(
			"max_tokens is %d, which asks for no answer",
			sent.MaxTokens,
		)
	}

	if len(sent.Messages) != 1 {

		t.Fatalf(
			"got %d messages, want 1",
			len(sent.Messages),
		)
	}

	blocks := sent.Messages[0].Content

	if len(blocks) != 2 {

		t.Fatalf(
			"got %d blocks, want an image and a question",
			len(blocks),
		)
	}

	// The picture has to be the picture. A client that
	// sent the wrong bytes, or sent them unencoded,
	// would be answered about something else entirely
	// and nothing else in this test would notice.
	if blocks[0].Type != "image" || blocks[0].Source == nil {

		t.Fatalf(
			"the first block is %q with source %v, want an image",
			blocks[0].Type,
			blocks[0].Source,
		)
	}

	if blocks[0].Source.MediaType != "image/jpeg" {

		t.Errorf(
			"media type is %q, want image/jpeg",
			blocks[0].Source.MediaType,
		)
	}

	if blocks[0].Source.Data !=
		base64.StdEncoding.EncodeToString(image) {

		t.Error(
			"the picture sent is not the picture given",
		)
	}

	if blocks[1].Type != "text" {

		t.Fatalf(
			"the second block is %q, want text",
			blocks[1].Type,
		)
	}

	// Every category is offered, by slug and by label.
	// A category left out of the prompt is one the model
	// can never choose, which would quietly make the
	// shop smaller than it is.
	for _, option := range testOptions {

		if !strings.Contains(
			blocks[1].Text,
			option.Slug,
		) {

			t.Errorf(
				"the prompt never mentions %s",
				option.Slug,
			)
		}

		if !strings.Contains(
			blocks[1].Text,
			option.Name,
		) {

			t.Errorf(
				"the prompt never mentions %q",
				option.Name,
			)
		}
	}
}

// TestChooseCategoryReadsOnlyTextBlocks checks that a
// response carrying something other than an answer is not
// read as one.
//
// Today's replies are a single text block. This is here
// so that a reply which also carries a block of another
// kind is still answered from the text alone, rather than
// having every block's contents run together into
// something that matches no category and looks like the
// model being confused.
func TestChooseCategoryReadsOnlyTextBlocks(t *testing.T) {

	client, _ := visionServer(
		t,
		200,
		`{"content":[`+
			`{"type":"thinking","text":"fashion"},`+
			`{"type":"text","text":"home-kitchen"}`+
			`]}`,
	)

	slug, err := client.ChooseCategory(
		context.Background(),
		[]byte{0xff},
		"image/png",
		testOptions,
	)

	if err != nil {
		t.Fatalf("ChooseCategory: %v", err)
	}

	if slug != "home-kitchen" {

		t.Errorf(
			"got %q, want home-kitchen",
			slug,
		)
	}
}

// TestChooseCategoryReportsARefusal checks that a refusal
// is reported with what the API said about it.
//
// The message matters more than it looks. Every other
// failure in this client is a shape this program
// controls, and a refusal is the one that comes from
// outside: an expired key, a model name that does not
// exist, a spent balance. All three look identical from
// here and are told apart only by this string.
func TestChooseCategoryReportsARefusal(t *testing.T) {

	client, _ := visionServer(
		t,
		http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"model: not found"}}`,
	)

	_, err := client.ChooseCategory(
		context.Background(),
		[]byte{0xff},
		"image/jpeg",
		testOptions,
	)

	if err == nil {
		t.Fatal("a refused request was reported as a success")
	}

	if !strings.Contains(err.Error(), "model: not found") {

		t.Errorf(
			"the refusal does not quote the API: %v",
			err,
		)
	}
}

// TestChooseCategoryRefusesWhatItCannotSend checks the
// four things this client turns down before it opens a
// connection.
//
// The picture limit is the one worth having: it is
// checked before the encoding rather than after, so a
// file over the limit is refused without ever being
// copied into base64, which is a third larger again.
func TestChooseCategoryRefusesWhatItCannotSend(t *testing.T) {

	cases := []struct {
		name string

		key string

		image []byte

		options []CategoryOption

		want string
	}{

		{
			name: "no key",

			image: []byte{0xff},

			options: testOptions,

			want: "ANTHROPIC_API_KEY",
		},

		{
			name: "nothing to choose from",

			key: "test-key",

			image: []byte{0xff},

			want: "no categories",
		},

		{
			name: "an empty picture",

			key: "test-key",

			options: testOptions,

			want: "empty",
		},

		{
			name: "a picture over the limit",

			key: "test-key",

			image: make([]byte, maxImageBytes+1),

			options: testOptions,

			want: "over the",
		},
	}

	for _, test := range cases {

		// A server that fails the test if it is ever
		// reached. Every case here is supposed to be
		// turned down before a request is made, and one
		// that quietly made the call anyway would
		// otherwise pass on the error it got back.
		server := httptest.NewServer(
			http.HandlerFunc(
				func(
					w http.ResponseWriter,
					r *http.Request,
				) {

					t.Errorf(
						"%s: the request was sent anyway",
						test.name,
					)
				},
			),
		)

		client := &VisionClient{
			APIKey: test.key,

			BaseURL: server.URL,

			Model: DefaultVisionModel,

			HTTPClient: server.Client(),
		}

		_, err := client.ChooseCategory(
			context.Background(),
			test.image,
			"image/jpeg",
			test.options,
		)

		server.Close()

		if err == nil {

			t.Errorf(
				"%s: got no error",
				test.name,
			)

			continue
		}

		if !strings.Contains(err.Error(), test.want) {

			t.Errorf(
				"%s: error is %q, want it to mention %q",
				test.name,
				err.Error(),
				test.want,
			)
		}
	}
}
