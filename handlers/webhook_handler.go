package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// maxWebhookBodyBytes caps how large a webhook body may
// be. A payment notification is small, so anything very
// much larger is either a mistake or an attack.
const maxWebhookBodyBytes = 1 << 20

// paystackWebhookData is the data object inside a
// Paystack webhook.
type paystackWebhookData struct {

	// ID is Paystack's identifier for this notification.
	//
	// It is read as raw JSON rather than as a number,
	// because it is only ever used as text. Keeping it
	// raw also means the field still works if Paystack
	// ever sends it as a string, and that a large id
	// cannot lose precision on the way through a float.
	ID json.RawMessage `json:"id"`

	// Reference is the reference this application
	// generated when it started the payment. It is what
	// links the notification to an order.
	Reference string `json:"reference"`

	// Amount is in kobo, the smallest unit of the Naira.
	Amount int64 `json:"amount"`

	Status string `json:"status"`
}

// paystackWebhookPayload is the whole webhook body.
type paystackWebhookPayload struct {

	// Event is the event type, for example
	// "charge.success".
	Event string `json:"event"`

	Data paystackWebhookData `json:"data"`
}

// PaystackWebhookHandler receives payment notifications
// from Paystack.
//
// This endpoint is the one piece of the application that
// anyone on the internet may call, because Paystack has no
// session cookie. It is protected by a signature instead.
//
// The order of the steps below matters a great deal, and
// each one is deliberate.
func PaystackWebhookHandler(
	pool *pgxpool.Pool,
	secretKey string,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		// Step 1: read the body exactly as it arrived.
		//
		// This must happen before any parsing. Paystack
		// signs the raw bytes, so re-encoding the JSON
		// would produce different bytes and the
		// signature would never match.
		body, err := io.ReadAll(
			io.LimitReader(
				r.Body,
				maxWebhookBodyBytes,
			),
		)

		if err != nil {

			writeError(
				w,
				http.StatusBadRequest,
				"Could not read request body",
			)

			return
		}

		// Step 2: check the signature.
		//
		// Without this, anybody who found the URL could
		// post a fake "payment succeeded" and receive
		// goods for free.
		signature := r.Header.Get(
			"x-paystack-signature",
		)

		if !utils.VerifyPaystackSignature(
			secretKey,
			body,
			signature,
		) {

			writeError(
				w,
				http.StatusUnauthorized,
				"Invalid signature",
			)

			return
		}

		// Step 3: parse the payload now that the bytes
		// are known to have come from Paystack.
		var payload paystackWebhookPayload

		err = json.Unmarshal(body, &payload)

		if err != nil {

			writeError(
				w,
				http.StatusBadRequest,
				"Invalid webhook payload",
			)

			return
		}

		// Step 4: work out the idempotency key.
		//
		// Paystack's own event id is the right key,
		// because every retry carries the same one.
		//
		// If it is missing, which should not happen, the
		// event type and reference are combined instead.
		// That is not as precise, but it is far better
		// than falling back to no key at all, which would
		// let a retry through as though it were new.
		eventID := strings.TrimSpace(
			strings.Trim(
				string(payload.Data.ID),
				`"`,
			),
		)

		if eventID == "" {

			eventID = payload.Event + ":" +
				payload.Data.Reference

			if payload.Data.Reference == "" {
				eventID = ""
			}
		}

		if eventID == "" {

			writeError(
				w,
				http.StatusBadRequest,
				"Webhook has no usable event id",
			)

			return
		}

		input := models.PaystackWebhookInput{
			ProviderEventID: eventID,

			EventType: payload.Event,

			Reference: payload.Data.Reference,

			AmountKobo: payload.Data.Amount,

			ProviderStatus: payload.Data.Status,

			Payload: body,
		}

		// Step 5: hand it to the storage layer, which
		// claims the event and does the work in one
		// transaction.
		outcome, err :=
			storage.ProcessPaystackWebhookInDB(
				pool,
				input,
			)

		if err != nil {

			// Answering 500 tells Paystack to try
			// again, which is right for a genuine
			// failure: the event was recorded as
			// failed, and a retry is allowed to take
			// it over.
			writeError(
				w,
				http.StatusInternalServerError,
				"Could not process webhook",
			)

			return
		}

		// Duplicates, ignored events, and successful
		// processing all answer 200, so Paystack stops
		// retrying.
		writeJSON(
			w,
			http.StatusOK,
			map[string]string{
				"status": string(outcome),
			},
		)
	}
}
