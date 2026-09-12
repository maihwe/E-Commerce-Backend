package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// VisionBaseURL is the address of the model API.
//
// It is a variable rather than a constant so tests can
// point the client at a local stand-in server instead of
// the real thing, which is the same arrangement
// PaystackBaseURL uses and for the same reason.
var VisionBaseURL = "https://api.anthropic.com"

// anthropicVersion is the API version sent with every
// call.
//
// The header is required, and the date in it is part of
// the address rather than a description of this program:
// it selects the shape of the request and the response.
// It is a constant here because it changes only when
// somebody decides to move this client to a newer
// version, which is a decision that deserves to be made
// on purpose.
const anthropicVersion = "2023-06-01"

// DefaultVisionModel is the model asked to look at a
// picture when VISION_MODEL says nothing.
//
// It is a variable so that naming a different model is a
// one-line change in one place, and an environment
// variable so that it can be changed without rebuilding.
// Which model to use is a judgement about cost against
// quality rather than a fact about this program, so it
// belongs somewhere a person can change their mind.
var DefaultVisionModel = "claude-sonnet-5"

// maxImageBytes caps the picture sent for classification.
//
// The limit is the API's own limit on an image sent as
// base64, and it is checked here rather than discovered
// from a refusal, because the refusal would arrive after
// the whole file had been read into memory and encoded.
// A shop's photographs are resized before they are copied
// in (see pictures/README.md), so this is a backstop
// against one enormous file rather than a limit anybody
// doing the right thing will meet.
const maxImageBytes = 5 << 20

// VisionClient asks a model what it can see in a
// picture.
//
// It is used for exactly one question: which of this
// shop's categories does the product in this photograph
// belong to. Nothing about the answer is trusted. The
// model is given the list of categories it may choose
// from and its reply is matched against that list, so the
// worst a wrong answer can do is choose the wrong one of
// ten things a person already decided the shop sells. It
// can never introduce a category, and no text it produces
// is stored anywhere.
type VisionClient struct {

	// APIKey authenticates this application to the model
	// API.
	APIKey string

	// BaseURL lets tests redirect calls to a local
	// server.
	BaseURL string

	// Model is the model asked to look at the picture.
	Model string

	HTTPClient *http.Client
}

// NewVisionClientFromEnv builds a client from
// environment variables.
//
// The client is created even when the key is missing.
// That is the rule the Paystack client follows and it is
// followed here for the same reason: a shop that cannot
// classify its photographs is still a shop, and refusing
// to start would make the whole application unavailable
// over one optional feature.
func NewVisionClientFromEnv() *VisionClient {

	baseURL := strings.TrimSpace(
		os.Getenv("VISION_BASE_URL"),
	)

	if baseURL == "" {
		baseURL = VisionBaseURL
	}

	model := strings.TrimSpace(
		os.Getenv("VISION_MODEL"),
	)

	if model == "" {
		model = DefaultVisionModel
	}

	return &VisionClient{
		APIKey: strings.TrimSpace(
			os.Getenv("ANTHROPIC_API_KEY"),
		),

		BaseURL: strings.TrimRight(baseURL, "/"),

		Model: model,

		// The timeout is longer than the Paystack
		// client's twenty seconds because this call
		// uploads a picture and asks a model to reason
		// about it, which is slower than a payment
		// lookup. It is still a timeout: a request that
		// hangs must not hold up every start of the
		// server for ever.
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// HasAPIKey reports whether a key was configured.
//
// Callers use this only to warn, the way they use
// HasSecretKey, and for the same reason.
func (client *VisionClient) HasAPIKey() bool {

	return client.APIKey != ""
}

// CategoryOption is one of the categories a picture may
// be sorted into.
//
// Both halves are here because both are needed and they
// are needed by different readers. The model is shown the
// label, because "Home & Kitchen" is a thing it can
// reason about and "home-kitchen" is a thing it has to
// decode. The caller is given back the slug, because the
// slug is what identifies the category in the database.
type CategoryOption struct {
	Slug string

	Name string
}

// visionSource is a picture sent to the API.
//
// The picture travels inside the JSON body as base64
// rather than as an upload, because this application
// already has the file on disk and there is nothing to
// gain from a second round trip.
type visionSource struct {

	// Type is always "base64" for a picture sent this
	// way.
	Type string `json:"type"`

	// MediaType is the picture's type, such as
	// "image/jpeg". The API does not sniff it.
	MediaType string `json:"media_type"`

	// Data is the picture itself, base64 encoded.
	Data string `json:"data"`
}

// visionBlock is one piece of a message.
//
// It is one struct holding both a text block and an image
// block rather than two types behind an interface,
// because the wire format is genuinely a single object
// with a "type" field and the unused half is simply
// absent. omitempty is what keeps the unused half from
// being sent as a null.
type visionBlock struct {

	// Type is "text" or "image".
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	Source *visionSource `json:"source,omitempty"`
}

// visionMessage is one turn of the conversation.
type visionMessage struct {
	Role string `json:"role"`

	Content []visionBlock `json:"content"`
}

// visionRequest is the body sent to the API.
type visionRequest struct {
	Model string `json:"model"`

	// MaxTokens is the room the model has to answer in.
	//
	// The answer is one word, so the number looks
	// absurdly large. It is not room to think: it is
	// the guarantee that the answer is never cut off
	// partway through. A truncated answer would be a
	// slug that matches nothing, and the operator would
	// be told the model was unclear when in fact it was
	// interrupted.
	MaxTokens int `json:"max_tokens"`

	System string `json:"system,omitempty"`

	Messages []visionMessage `json:"messages"`
}

// visionResponse is the envelope the API answers with.
type visionResponse struct {
	Content []visionBlock `json:"content"`

	// Error is set instead of Content when the call was
	// refused, which the API reports in the body rather
	// than only in the status line.
	Error *visionAPIError `json:"error"`
}

// visionAPIError is what the API says when it declines.
type visionAPIError struct {
	Type string `json:"type"`

	Message string `json:"message"`
}

// ChooseCategory asks the model which category a picture
// belongs to.
//
// It returns the slug of one of the options, or an empty
// string when the model did not name any of them. An
// empty string is not an error: "the picture is of
// nothing I can place" is a real answer about a real
// photograph, and the caller's job is to leave the
// product's category exactly as it was.
//
// This is the whole safety story of this file. The model
// is handed a fixed list and its reply is looked up in
// that list, so a reply of any other kind is discarded
// rather than stored. It is the same discipline the
// catalog's sort keys follow, where the client's text is
// only ever used to look up one of a fixed set of
// fragments and never pasted into a statement.
func (client *VisionClient) ChooseCategory(
	ctx context.Context,
	image []byte,
	mediaType string,
	options []CategoryOption,
) (string, error) {

	if client.APIKey == "" {
		return "", fmt.Errorf(
			"ANTHROPIC_API_KEY is not set",
		)
	}

	if len(options) == 0 {
		return "", fmt.Errorf(
			"there are no categories to choose from",
		)
	}

	if len(image) == 0 {
		return "", fmt.Errorf("the picture is empty")
	}

	// The limit is checked before the encoding rather
	// than after, because the encoding is what costs the
	// memory: base64 is a third larger than the bytes it
	// is made from, so a file that only just fits would
	// be refused after the copy had already been made.
	if len(image) > maxImageBytes {

		return "", fmt.Errorf(
			"the picture is %d bytes, over the %d byte limit",
			len(image),
			maxImageBytes,
		)
	}

	body, err := json.Marshal(visionRequest{
		Model: client.Model,

		MaxTokens: 64,

		System: visionSystemPrompt,

		Messages: []visionMessage{
			{
				Role: "user",

				Content: []visionBlock{
					{
						Type: "image",

						Source: &visionSource{
							Type: "base64",

							MediaType: mediaType,

							Data: base64.StdEncoding.EncodeToString(
								image,
							),
						},
					},

					{
						Type: "text",

						Text: categoryPrompt(options),
					},
				},
			},
		},
	})

	if err != nil {
		return "", err
	}

	answer, err := client.do(ctx, body)

	if err != nil {
		return "", err
	}

	return matchCategory(answer, options), nil
}

// visionSystemPrompt sets the model's job.
//
// It is deliberately short. The instruction that actually
// matters is in the message, beside the list of
// categories and the picture it is describing, because
// that is where a model reading a long prompt pays
// attention.
const visionSystemPrompt = "You sort photographs of products into a shop's categories. You answer with a single category slug and nothing else."

// categoryPrompt is the question, with the categories
// written out.
//
// The list is sorted here rather than by the caller. A
// prompt built from a map would come out in a different
// order on every call, and a question that changes shape
// between two identical pictures is a question whose
// answers cannot be compared.
func categoryPrompt(options []CategoryOption) string {

	sorted := make([]CategoryOption, len(options))

	copy(sorted, options)

	sort.Slice(
		sorted,
		func(i, j int) bool {

			return sorted[i].Slug < sorted[j].Slug
		},
	)

	var builder strings.Builder

	builder.WriteString(
		"Which of this shop's categories does the product in this photograph belong to?\n\n",
	)

	for _, option := range sorted {

		builder.WriteString("- ")

		builder.WriteString(option.Slug)

		builder.WriteString(" (")

		builder.WriteString(option.Name)

		builder.WriteString(")\n")
	}

	builder.WriteString(
		"\nAnswer with one of those slugs, exactly as written, and nothing else. " +
			"If the photograph does not show a product that belongs in any of them, answer with the single word none.",
	)

	return builder.String()
}

// matchCategory reduces an answer to one of the options,
// or to nothing.
//
// The comparison is deliberately forgiving about the
// things a model does to a word without meaning anything
// by it -- a stray capital, a full stop, a pair of quotes
// -- and deliberately unforgiving about the word itself.
// A near miss that is accepted is a product in the wrong
// part of the shop; a near miss that is refused is one
// product left where its seller put it.
func matchCategory(
	answer string,
	options []CategoryOption,
) string {

	answer = strings.TrimSpace(answer)

	answer = strings.Trim(answer, "\"'`.")

	answer = strings.ToLower(answer)

	// An answer with anything else in it is not one of
	// the options, whatever it looks like. This is the
	// line that keeps a model's prose out of the
	// database: a paragraph explaining its reasoning
	// contains none of these slugs as a whole line, so
	// it matches nothing.
	for _, option := range options {

		if answer == option.Slug {
			return option.Slug
		}
	}

	return ""
}

// do performs one HTTP call against the model API and
// returns the text of the answer.
func (client *VisionClient) do(
	ctx context.Context,
	body []byte,
) (string, error) {

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.BaseURL+"/v1/messages",
		bytes.NewReader(body),
	)

	if err != nil {
		return "", err
	}

	request.Header.Set("x-api-key", client.APIKey)

	request.Header.Set(
		"anthropic-version",
		anthropicVersion,
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	response, err := client.HTTPClient.Do(request)

	if err != nil {
		return "", err
	}

	defer response.Body.Close()

	responseBody, err := io.ReadAll(
		io.LimitReader(response.Body, 1<<20),
	)

	if err != nil {
		return "", err
	}

	var envelope visionResponse

	err = json.Unmarshal(responseBody, &envelope)

	if err != nil {

		// A body that will not parse is worth reporting
		// with the status, because the usual cause is a
		// refusal from something in front of the API --
		// a proxy, a gateway -- rather than from the API
		// itself.
		return "", fmt.Errorf(
			"could not read the vision response (HTTP %d): %w",
			response.StatusCode,
			err,
		)
	}

	if envelope.Error != nil {

		return "", fmt.Errorf(
			"the vision request was refused: %s",
			envelope.Error.Message,
		)
	}

	if response.StatusCode != http.StatusOK {

		return "", fmt.Errorf(
			"the vision request was answered %d",
			response.StatusCode,
		)
	}

	// Every text block is joined rather than the first
	// one being taken. The answer is one block in
	// practice, and joining is what makes that an
	// observation about today's answer rather than an
	// assumption this code would break on.
	var answer strings.Builder

	for _, block := range envelope.Content {

		if block.Type != "text" {
			continue
		}

		answer.WriteString(block.Text)
	}

	return answer.String(), nil
}
