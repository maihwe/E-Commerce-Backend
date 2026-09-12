package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// PaystackBaseURL is the address of the Paystack API.
//
// It is a variable rather than a constant so tests can
// point the client at a local stand-in server instead of
// the real thing.
var PaystackBaseURL = "https://api.paystack.co"

// PaystackClient talks to Paystack.
//
// The secret key is read from the environment and never
// written into the source, because it is the key that can
// move money.
type PaystackClient struct {

	// SecretKey authenticates this application to
	// Paystack. It is also the key used to check that
	// an incoming webhook really came from Paystack.
	SecretKey string

	// BaseURL lets tests redirect calls to a local
	// server.
	BaseURL string

	HTTPClient *http.Client
}

// NewPaystackClientFromEnv builds a client from
// environment variables.
//
// The client is created even when the keys are missing.
// That is deliberate: every test in this project runs
// without Paystack credentials, because the webhook path
// only needs the secret key to check a signature, and the
// rest is exercised against a local server. If a key is
// truly needed and absent, the call that needs it fails
// with a clear message rather than the program refusing
// to start.
func NewPaystackClientFromEnv() *PaystackClient {

	baseURL := os.Getenv("PAYSTACK_BASE_URL")

	if baseURL == "" {
		baseURL = PaystackBaseURL
	}

	return &PaystackClient{
		SecretKey: os.Getenv("PAYSTACK_SECRET_KEY"),

		BaseURL: strings.TrimRight(baseURL, "/"),

		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// HasSecretKey reports whether a secret key was configured.
//
// Callers use this only to warn. A missing key is not fatal
// at startup, because everything except the payment calls
// still works without one, and failing to boot would make
// the rest of the API impossible to try out.
func (client *PaystackClient) HasSecretKey() bool {

	return client.SecretKey != ""
}

// InitializeTransactionRequest is the body sent to
// Paystack to begin a payment.
type InitializeTransactionRequest struct {

	// Email is required by Paystack, which sends the
	// receipt there.
	Email string `json:"email"`

	// Amount is in kobo, the smallest unit of the Naira.
	//
	// Paystack has no concept of a decimal amount, which
	// is exactly why this project also works in kobo.
	Amount int64 `json:"amount"`

	// Reference is this application's own identifier for
	// the payment.
	//
	// It is what the webhook quotes back, so it is how a
	// payment is matched to an order.
	Reference string `json:"reference"`

	// Metadata travels with the payment and comes back
	// on the webhook, which is handy for tracing.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// paystackResponse is the envelope every Paystack call
// returns.
type paystackResponse struct {
	Status  bool            `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// InitializeTransactionData is the part of the response
// that matters when starting a payment.
type InitializeTransactionData struct {

	// AuthorizationURL is the page the shopper is sent
	// to in order to pay.
	AuthorizationURL string `json:"authorization_url"`

	AccessCode string `json:"access_code"`

	Reference string `json:"reference"`
}

// InitializeTransaction asks Paystack to start a payment.
//
// Nothing is charged here. This call only creates the
// payment and returns the page the shopper should be sent
// to. The money moves later, on Paystack's own site, and
// confirmation arrives by webhook.
func (client *PaystackClient) InitializeTransaction(
	ctx context.Context,
	request InitializeTransactionRequest,
) (InitializeTransactionData, error) {

	if client.SecretKey == "" {
		return InitializeTransactionData{}, fmt.Errorf(
			"PAYSTACK_SECRET_KEY is not set",
		)
	}

	body, err := json.Marshal(request)

	if err != nil {
		return InitializeTransactionData{}, err
	}

	envelope, err := client.do(
		ctx,
		http.MethodPost,
		"/transaction/initialize",
		body,
	)

	if err != nil {
		return InitializeTransactionData{}, err
	}

	var data InitializeTransactionData

	err = json.Unmarshal(envelope.Data, &data)

	if err != nil {
		return InitializeTransactionData{}, err
	}

	return data, nil
}

// VerifyTransactionData is what Paystack reports about a
// payment.
type VerifyTransactionData struct {
	Status string `json:"status"`

	// Amount is in kobo, and is compared against the
	// order total before the order is treated as paid.
	Amount int64 `json:"amount"`

	Reference string `json:"reference"`

	Currency string `json:"currency"`
}

// VerifyTransaction asks Paystack what really happened to
// a payment.
//
// This is the recovery path for a webhook that never
// arrived. Webhooks can be lost if the server was down or
// the network dropped, so a payment must never depend on
// one arriving.
//
// It is also the safer of the two sources: a webhook is
// something an attacker can try to forge, whereas this is
// this server asking Paystack directly.
func (client *PaystackClient) VerifyTransaction(
	ctx context.Context,
	reference string,
) (VerifyTransactionData, error) {

	if client.SecretKey == "" {
		return VerifyTransactionData{}, fmt.Errorf(
			"PAYSTACK_SECRET_KEY is not set",
		)
	}

	envelope, err := client.do(
		ctx,
		http.MethodGet,
		"/transaction/verify/"+reference,
		nil,
	)

	if err != nil {
		return VerifyTransactionData{}, err
	}

	var data VerifyTransactionData

	err = json.Unmarshal(envelope.Data, &data)

	if err != nil {
		return VerifyTransactionData{}, err
	}

	return data, nil
}

// do performs one HTTP call against Paystack and unwraps
// the envelope.
func (client *PaystackClient) do(
	ctx context.Context,
	method string,
	path string,
	body []byte,
) (paystackResponse, error) {

	var reader io.Reader

	if body != nil {
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		method,
		client.BaseURL+path,
		reader,
	)

	if err != nil {
		return paystackResponse{}, err
	}

	request.Header.Set(
		"Authorization",
		"Bearer "+client.SecretKey,
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	response, err := client.HTTPClient.Do(request)

	if err != nil {
		return paystackResponse{}, err
	}

	defer response.Body.Close()

	responseBody, err := io.ReadAll(
		io.LimitReader(response.Body, 1<<20),
	)

	if err != nil {
		return paystackResponse{}, err
	}

	var envelope paystackResponse

	err = json.Unmarshal(responseBody, &envelope)

	if err != nil {
		return paystackResponse{}, fmt.Errorf(
			"could not read Paystack response: %w",
			err,
		)
	}

	if !envelope.Status {
		return paystackResponse{}, fmt.Errorf(
			"Paystack refused the request: %s",
			envelope.Message,
		)
	}

	return envelope, nil
}
