package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProcessPaystackWebhookInDB handles one delivery from
// Paystack, exactly once.
//
// The shape of this function is the answer to the hardest
// requirement in the project: a retried webhook must never
// be processed twice.
//
// The mechanism is the INSERT below. It tries to write a
// row for this delivery, and the webhook_events table has
// a UNIQUE constraint on (provider, provider_event_id).
// Paystack sends the same event id on every retry, so:
//
//   - The first delivery finds no row, inserts one, and
//     gets an id back. It goes on to do the work.
//
//   - Every retry collides with that row and inserts
//     nothing. No id comes back, so the function returns
//     straight away without touching the order.
//
// There is one exception. If the previous attempt ended in
// the 'failed' state, the retry is allowed to take the row
// over. That matters because a failure is often something
// temporary, such as the database being briefly
// unreachable, and a payment must not be permanently
// stranded by a moment of bad luck.
//
// The claim and the work share one transaction. That means
// the row lock taken by the INSERT is held while the order
// is being confirmed, so two deliveries arriving at the
// same instant cannot both decide they are the first. The
// second one waits, then sees the finished row and is
// turned away.
func ProcessPaystackWebhookInDB(
	pool *pgxpool.Pool,
	input models.PaystackWebhookInput,
) (models.WebhookOutcome, error) {

	ctx := context.Background()

	tx, err := pool.Begin(ctx)

	if err != nil {
		return models.WebhookOutcomeFailed, err
	}

	// Roll back unless a commit is reached further
	// down. Committing twice is harmless, so every path
	// that finishes its work commits explicitly.
	defer tx.Rollback(ctx)

	eventID, claimed, err := claimWebhookEventInTx(
		ctx,
		tx,
		input,
	)

	if err != nil {
		return models.WebhookOutcomeFailed, err
	}

	// This delivery has already been handled. There is
	// nothing to do, and the caller should still answer
	// with a success so that Paystack stops retrying.
	//
	// The deferred rollback runs here, which is correct:
	// this path wrote nothing.
	if !claimed {
		return models.WebhookOutcomeDuplicate, nil
	}

	outcome, workErr := handleWebhookEventInTx(
		ctx,
		tx,
		input,
	)

	// The work failed. Record why, and commit that
	// record so the next retry can see it and try again
	// rather than being mistaken for a duplicate.
	if workErr != nil {

		markWebhookFailedInTx(
			ctx,
			tx,
			eventID,
			workErr.Error(),
		)

		if err := tx.Commit(ctx); err != nil {
			return models.WebhookOutcomeFailed, err
		}

		return models.WebhookOutcomeFailed, workErr
	}

	err = markWebhookProcessedInTx(ctx, tx, eventID)

	if err != nil {
		return models.WebhookOutcomeFailed, err
	}

	err = tx.Commit(ctx)

	if err != nil {
		return models.WebhookOutcomeFailed, err
	}

	return outcome, nil
}

// claimWebhookEventInTx tries to take ownership of a
// delivery.
//
// It returns true when this caller now owns the event and
// should do the work, and false when the event has already
// been dealt with.
func claimWebhookEventInTx(
	ctx context.Context,
	querier dbQuerier,
	input models.PaystackWebhookInput,
) (int, bool, error) {

	var eventID int

	err := querier.QueryRow(
		ctx,
		`
		INSERT INTO webhook_events
			(
				provider,
				provider_event_id,
				event_type,
				payload
			)
		VALUES
			($1, $2, $3, $4::jsonb)
		ON CONFLICT (provider, provider_event_id)
		DO UPDATE SET
			status = 'processing',
			error_message = '',
			received_at = NOW()
		WHERE webhook_events.status = 'failed'
		RETURNING id
		`,
		models.PaystackProvider,
		input.ProviderEventID,
		input.EventType,
		string(input.Payload),
	).Scan(&eventID)

	// No row came back, which means the conflict clause
	// decided not to take the row over. The event is
	// either being handled right now or was handled
	// successfully before. Either way, this delivery is
	// not ours to process.
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, err
	}

	return eventID, true, nil
}

// markWebhookProcessedInTx records that the event was
// handled successfully.
func markWebhookProcessedInTx(
	ctx context.Context,
	querier dbQuerier,
	eventID int,
) error {

	_, err := querier.Exec(
		ctx,
		`
		UPDATE webhook_events
		SET
			status = 'processed',
			processed_at = NOW(),
			error_message = ''
		WHERE id = $1
		`,
		eventID,
	)

	return err
}

// markWebhookFailedInTx records that handling the event
// did not succeed.
//
// The error text is kept because a payment that did not go
// through cleanly is exactly the thing somebody will need
// to investigate later.
func markWebhookFailedInTx(
	ctx context.Context,
	querier dbQuerier,
	eventID int,
	message string,
) {

	// Truncate first, so that a very long error cannot
	// cause this bookkeeping write to fail as well and
	// hide the original problem.
	const maxErrorMessageLength = 2000

	if len(message) > maxErrorMessageLength {
		message = message[:maxErrorMessageLength]
	}

	_, _ = querier.Exec(
		ctx,
		`
		UPDATE webhook_events
		SET
			status = 'failed',
			error_message = $1,
			processed_at = NULL
		WHERE id = $2
		`,
		message,
		eventID,
	)
}

// handleWebhookEventInTx does the actual work for an event
// this caller now owns.
func handleWebhookEventInTx(
	ctx context.Context,
	querier dbQuerier,
	input models.PaystackWebhookInput,
) (models.WebhookOutcome, error) {

	switch input.EventType {

	case models.PaystackEventChargeSuccess:

		// A success event must carry a reference and a
		// positive amount. Anything else is malformed,
		// and guessing at it would be worse than
		// refusing it.
		if input.Reference == "" {
			return models.WebhookOutcomeFailed,
				errors.New(
					"charge.success arrived without a reference",
				)
		}

		if input.AmountKobo <= 0 {
			return models.WebhookOutcomeFailed,
				errors.New(
					"charge.success arrived with a non-positive amount",
				)
		}

		_, err := ConfirmOrderPaymentInTx(
			ctx,
			querier,
			input.Reference,
			input.AmountKobo,
			"Payment confirmed by Paystack",
		)

		// The order was not waiting for payment. The
		// most ordinary reason is that an earlier
		// delivery of this same payment already
		// confirmed it, which is a success from the
		// provider's point of view, not a failure.
		if errors.Is(err, ErrAlreadyPaid) {
			return models.WebhookOutcomeDuplicate, nil
		}

		if err != nil {
			return models.WebhookOutcomeFailed, err
		}

		return models.WebhookOutcomeProcessed, nil

	case models.PaystackEventChargeFailed:

		// A failed charge leaves the order pending on
		// purpose. The shopper is free to try again,
		// and the cart was already emptied onto the
		// order, so nothing is lost.
		//
		// The event is still recorded, which is what
		// makes a "why did this payment not go
		// through?" question answerable later.
		return models.WebhookOutcomeIgnored, nil

	default:

		// Anything else, including refund.processed, is
		// recorded and left alone. Refunds in this
		// project are driven by an admin through the
		// order state machine, so acting on the
		// provider's refund event as well would risk
		// doing the same thing twice.
		return models.WebhookOutcomeIgnored, nil
	}
}

// GetWebhookEventFromDB reads one recorded webhook event.
//
// This exists mainly so a payment problem can be looked up
// by its event id.
func GetWebhookEventFromDB(
	pool *pgxpool.Pool,
	providerEventID string,
) (models.WebhookEvent, error) {

	var event models.WebhookEvent

	var processedAt *time.Time

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT
			id,
			provider,
			provider_event_id,
			event_type,
			status,
			error_message,
			received_at,
			processed_at
		FROM webhook_events
		WHERE provider = $1
		AND provider_event_id = $2
		`,
		models.PaystackProvider,
		providerEventID,
	).Scan(
		&event.ID,
		&event.Provider,
		&event.ProviderEventID,
		&event.EventType,
		&event.Status,
		&event.ErrorMessage,
		&event.ReceivedAt,
		&processedAt,
	)

	if err != nil {
		return models.WebhookEvent{}, fmt.Errorf(
			"webhook event not found: %w",
			err,
		)
	}

	event.ProcessedAt = processedAt

	return event, nil
}
