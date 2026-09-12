-- Migration 007
-- Create the inventory_movements ledger.
--
-- This is the inventory equivalent of the transactions
-- table from the Financial Tracker.
--
-- There is NO stock column on products. Available stock
-- is always the sum of the rows here:
--
--     SELECT SUM(quantity_change)
--     FROM inventory_movements
--     WHERE product_id = ?
--
-- The table is append-only. Rows are never updated or
-- deleted, so it doubles as a full audit trail of every
-- stock change that has ever happened.

BEGIN;


CREATE TABLE inventory_movements (

    id SERIAL PRIMARY KEY,

    product_id INT NOT NULL
        REFERENCES products(id) ON DELETE CASCADE,

    -- How much stock moved.
    --
    -- Negative when stock leaves (a sale), positive
    -- when stock arrives (a restock).
    --
    -- The CHECK forbids zero, because a movement of
    -- nothing is not a movement and would only add
    -- noise to the ledger.
    quantity_change INT NOT NULL
        CHECK (quantity_change <> 0),

    -- Why the stock moved.
    reason TEXT NOT NULL
        CHECK (
            reason IN (
                'restock',
                'sale',
                'adjustment',
                'refund',
                'cancel'
            )
        ),

    -- The order that caused this movement.
    --
    -- NULL for restocks and adjustments, which are not
    -- tied to any order.
    --
    -- ON DELETE CASCADE is what makes the UNIQUE
    -- constraint below behave sensibly: deleting an
    -- order clears its movements rather than leaving
    -- orphaned rows behind.
    order_id INT
        REFERENCES orders(id) ON DELETE CASCADE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- This constraint is one of the guards that makes
    -- payment processing safe.
    --
    -- It says that a given order may move a given
    -- product's stock only once for a given reason. So
    -- even if a payment webhook were somehow delivered
    -- twice and processed twice, PostgreSQL would
    -- refuse the second set of sale rows outright,
    -- rather than decrementing the stock twice.
    UNIQUE (order_id, product_id, reason)

);


-- Serve the stock sum efficiently.
--
-- Every product read adds up the movements for that
-- product, so this index is what keeps the catalog fast
-- as the ledger grows.
CREATE INDEX inventory_movements_product_id_idx
    ON inventory_movements (product_id);


COMMIT;
