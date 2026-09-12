-- Migration 008
-- Create the webhook_events table.
--
-- This table is the reason a repeated payment webhook
-- cannot be processed twice.
--
-- Payment providers retry webhooks. Paystack will send
-- the same charge.success event again if it does not
-- receive a 200 response quickly enough, and it may
-- resend for days. Every one of those retries carries
-- the same event id.
--
-- By storing each event id once, with a UNIQUE
-- constraint, the application can try to insert the
-- event before doing any work. If the insert conflicts,
-- the event was already handled, and the handler returns
-- 200 without touching the order.

BEGIN;


CREATE TABLE webhook_events (

    id SERIAL PRIMARY KEY,

    -- Which provider sent this. 'paystack' for now.
    --
    -- Storing it means a second provider could be added
    -- later without the two ever colliding on ids.
    provider TEXT NOT NULL,

    -- The provider's own id for this event.
    --
    -- Paystack sends this as data.id inside the body.
    provider_event_id TEXT NOT NULL,

    -- The kind of event, for example 'charge.success'.
    event_type TEXT NOT NULL,

    -- The complete original body, kept exactly as it
    -- arrived. This is invaluable when investigating a
    -- payment that did not behave as expected.
    payload JSONB NOT NULL,

    -- How far processing got.
    --
    -- 'processing' - claimed, work in progress
    -- 'processed'  - finished successfully
    -- 'failed'     - work was attempted and errored
    status TEXT NOT NULL DEFAULT 'processing'
        CHECK (
            status IN ('processing', 'processed', 'failed')
        ),

    -- Why processing failed, if it did.
    error_message TEXT NOT NULL DEFAULT '',

    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    processed_at TIMESTAMPTZ,

    -- The idempotency key.
    --
    -- This is the constraint the whole mechanism rests
    -- on: one row per event per provider, enforced by
    -- the database rather than by application logic.
    UNIQUE (provider, provider_event_id)

);


-- Finding stuck events, for example after an outage.
CREATE INDEX webhook_events_status_idx
    ON webhook_events (status);


COMMIT;
