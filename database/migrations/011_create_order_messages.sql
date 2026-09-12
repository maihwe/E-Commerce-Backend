-- Migration 011
-- Create the order_messages table.
--
-- These are the chat messages exchanged between a buyer
-- and the sellers on one order.
--
-- Messages are stored in the database even though they
-- are delivered live over a WebSocket. A WebSocket
-- connection is lost whenever a browser tab closes or a
-- phone changes network, so anything held only in memory
-- would vanish. Storing first and broadcasting second
-- means the conversation survives, and a client that
-- reconnects receives the full history.

BEGIN;


CREATE TABLE order_messages (

    id SERIAL PRIMARY KEY,

    order_id INT NOT NULL
        REFERENCES orders(id) ON DELETE CASCADE,

    -- Who wrote this message.
    sender_id INT NOT NULL
        REFERENCES users(id) ON DELETE CASCADE,

    body TEXT NOT NULL
        CHECK (length(trim(body)) > 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()

);


-- Reading a conversation means fetching one order's
-- messages in order, which is exactly this index.
CREATE INDEX order_messages_order_id_created_at_idx
    ON order_messages (order_id, created_at);


COMMIT;
