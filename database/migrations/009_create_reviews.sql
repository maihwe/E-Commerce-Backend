-- Migration 009
-- Create the reviews table.

BEGIN;


CREATE TABLE reviews (

    id SERIAL PRIMARY KEY,

    product_id INT NOT NULL
        REFERENCES products(id) ON DELETE CASCADE,

    -- The account that wrote the review.
    user_id INT NOT NULL
        REFERENCES users(id) ON DELETE CASCADE,

    -- The order the review came from.
    --
    -- SET NULL rather than CASCADE, because deleting an
    -- order should not delete somebody's review. The
    -- review simply stops being linked to an order.
    order_id INT
        REFERENCES orders(id) ON DELETE SET NULL,

    -- Stars, one to five.
    --
    -- The CHECK means no review can ever carry a
    -- rating that would break the average, such as a
    -- six or a negative number.
    rating INT NOT NULL
        CHECK (rating BETWEEN 1 AND 5),

    title TEXT NOT NULL DEFAULT '',

    body TEXT NOT NULL DEFAULT '',

    -- True when the reviewer actually had the product
    -- delivered to them.
    --
    -- It is worked out when the review is written and
    -- stored, because it is a fact about what happened
    -- at that moment.
    verified_purchase BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One review per person per product.
    --
    -- Without this, a single account could post the
    -- same review fifty times and drag the average
    -- rating wherever it liked.
    UNIQUE (product_id, user_id)

);


-- Product reads average the ratings for one product.
CREATE INDEX reviews_product_id_idx
    ON reviews (product_id);


COMMIT;
