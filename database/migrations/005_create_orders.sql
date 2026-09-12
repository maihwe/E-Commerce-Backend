-- Migration 005
-- Create the orders, order_items, and order_events
-- tables.

BEGIN;


-- Create the orders table.
CREATE TABLE orders (

    id SERIAL PRIMARY KEY,

    buyer_id INT NOT NULL
        REFERENCES users(id) ON DELETE RESTRICT,

    -- Where the order is in its life cycle.
    --
    -- The allowed moves between these states are
    -- defined in Go, in models/order_status.go.
    -- Listing the states here as well means the
    -- database itself refuses to store a status that
    -- the application does not understand.
    --
    -- pending   - created, not yet paid
    -- paid      - payment confirmed
    -- shipped   - seller has dispatched it
    -- delivered - buyer has received it
    -- cancelled - called off before payment
    -- refunded  - money returned after payment
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (
            status IN (
                'pending',
                'paid',
                'shipped',
                'delivered',
                'cancelled',
                'refunded'
            )
        ),

    -- Money on this order.
    --
    -- All three are calculated in SQL when the order
    -- is created, so they stay exact.
    subtotal NUMERIC(15, 2) NOT NULL
        CHECK (subtotal >= 0),

    discount_amount NUMERIC(15, 2) NOT NULL DEFAULT 0
        CHECK (discount_amount >= 0),

    -- What the buyer actually owes.
    total NUMERIC(15, 2) NOT NULL
        CHECK (total >= 0),

    -- Which coupon was used, if any.
    --
    -- ON DELETE SET NULL keeps the order intact if an
    -- admin ever removes a coupon: the order keeps its
    -- discount_amount, it just loses the link.
    coupon_id INT
        REFERENCES coupons(id) ON DELETE SET NULL,

    -- The reference sent to Paystack for this order.
    --
    -- UNIQUE is what lets an incoming webhook find
    -- exactly one order. It is NULL until payment is
    -- started. PostgreSQL allows many NULLs in a
    -- UNIQUE column, so unpaid orders do not collide.
    payment_reference TEXT UNIQUE,

    -- When the order reached each state. These stay
    -- NULL until the order gets there.
    paid_at TIMESTAMPTZ,
    shipped_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    refunded_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()

);


-- Create the order_items table.
CREATE TABLE order_items (

    id SERIAL PRIMARY KEY,

    order_id INT NOT NULL
        REFERENCES orders(id) ON DELETE CASCADE,

    product_id INT NOT NULL
        REFERENCES products(id) ON DELETE RESTRICT,

    -- Copied from the product when the order is placed.
    --
    -- A seller can then list their own orders directly,
    -- without a join back to products.
    seller_id INT NOT NULL
        REFERENCES users(id) ON DELETE RESTRICT,

    -- A snapshot of the product's name and price.
    --
    -- These are copied rather than looked up later so
    -- that renaming or repricing a product never
    -- rewrites history. The order shows what the buyer
    -- actually agreed to.
    product_name TEXT NOT NULL,
    unit_price NUMERIC(15, 2) NOT NULL
        CHECK (unit_price >= 0),

    quantity INT NOT NULL
        CHECK (quantity > 0),

    -- Unit price times quantity.
    --
    -- A generated column is computed by PostgreSQL
    -- itself, so it can never disagree with the two
    -- values it is made from.
    line_total NUMERIC(15, 2) NOT NULL
        GENERATED ALWAYS AS (unit_price * quantity) STORED

);


-- Create the order_events table.
--
-- This is an append-only history of an order's status
-- changes, in the same spirit as the inventory ledger.
-- Nothing here is ever updated or deleted.
CREATE TABLE order_events (

    id SERIAL PRIMARY KEY,

    order_id INT NOT NULL
        REFERENCES orders(id) ON DELETE CASCADE,

    -- The status the order moved from.
    --
    -- Empty for the very first row, which records the
    -- order being created rather than a change.
    from_status TEXT NOT NULL DEFAULT '',

    -- The status the order moved to.
    to_status TEXT NOT NULL,

    -- Who caused the change.
    --
    -- NULL when the change came from Paystack's webhook
    -- rather than from a signed-in person.
    actor_id INT
        REFERENCES users(id) ON DELETE SET NULL,

    note TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()

);


COMMIT;
