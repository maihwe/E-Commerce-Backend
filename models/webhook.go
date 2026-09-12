package models

import "time"

// WebhookEvent is one delivery from a payment provider.
//
// The row is written before any work is done, which is
// what makes a retry harmless.
type WebhookEvent struct {

	ID int `json:"id"`

	// Provider is 'paystack' for now.
	Provider string `json:"provider"`

	// ProviderEventID is the provider's own id for
	// this delivery. Paystack sends it as data.id.
	//
	// Every retry of the same delivery carries this
	// same id, which is exactly why it works as an
	// idempotency key.
	ProviderEventID string `json:"provider_event_id"`

	EventType string `json:"event_type"`

	Status string `json:"status"`

	ErrorMessage string `json:"error_message"`

	ReceivedAt time.Time `json:"received_at"`

	ProcessedAt *time.Time `json:"processed_at"`
}

// The states a webhook event can be in.
const (
	WebhookProcessing = "processing"
	WebhookProcessed  = "processed"
	WebhookFailed     = "failed"
)

// WebhookOutcome says what happened to a webhook, so the
// HTTP handler knows what to reply.
type WebhookOutcome string

const (

	// WebhookOutcomeProcessed means this delivery was
	// new and the work was done successfully.
	WebhookOutcomeProcessed WebhookOutcome = "processed"

	// WebhookOutcomeDuplicate means this exact delivery
	// had already been handled. Nothing was changed.
	// The handler still answers 200, so the provider
	// stops retrying.
	WebhookOutcomeDuplicate WebhookOutcome = "duplicate"

	// WebhookOutcomeIgnored means the delivery was
	// genuine and new, but concerns an event type this
	// application does not act on. It is recorded so
	// that it can be inspected later.
	WebhookOutcomeIgnored WebhookOutcome = "ignored"

	// WebhookOutcomeFailed means the work was attempted
	// and did not succeed. The claim is rolled back, so
	// a provider retry has a real chance of working.
	WebhookOutcomeFailed WebhookOutcome = "failed"
)

// The Paystack event types this application knows about.
//
// Every other event type is still recorded when it
// arrives, so nothing is silently thrown away, but it is
// marked ignored rather than acted on.
const (

	// PaystackEventChargeSuccess means a payment
	// completed successfully.
	PaystackEventChargeSuccess = "charge.success"

	// PaystackEventChargeFailed means a payment did not
	// complete.
	PaystackEventChargeFailed = "charge.failed"

	// PaystackEventRefundProcessed means Paystack has
	// finished returning money to a buyer.
	PaystackEventRefundProcessed = "refund.processed"
)

// PaystackProvider is the provider name stored alongside
// every webhook event.
const PaystackProvider = "paystack"

// PaystackWebhookInput is the part of a webhook this
// application needs, pulled out of the payload by the
// handler so that the storage layer does not have to know
// the shape of Paystack's JSON.
type PaystackWebhookInput struct {

	// ProviderEventID is Paystack's own id for this
	// delivery, sent as data.id.
	ProviderEventID string

	EventType string

	// Reference is the payment reference this
	// application generated when it started the
	// payment. It is what links the event back to an
	// order.
	Reference string

	// AmountKobo is what the provider says was paid, in
	// the smallest currency unit.
	//
	// It is compared against the order total before
	// anything is confirmed.
	AmountKobo int64

	// ProviderStatus is the status inside the data
	// object, usually "success" or "failed".
	ProviderStatus string

	// Payload is the complete original request body,
	// kept for later inspection.
	Payload []byte
}
