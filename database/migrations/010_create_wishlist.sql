-- Migration 010
-- Create the wishlist_items table.
--
-- A wishlist is a simple saved-for-later list. It holds
-- no quantities and no prices, because a wishlist is not
-- a commitment to buy anything.

BEGIN;


CREATE TABLE wishlist_items (

    id SERIAL PRIMARY KEY,

    user_id INT NOT NULL
        REFERENCES users(id) ON DELETE CASCADE,

    product_id INT NOT NULL
        REFERENCES products(id) ON DELETE CASCADE,

    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A product can only be saved once per account.
    --
    -- Adding something already on the list therefore
    -- changes nothing rather than creating a duplicate
    -- row, which the ON CONFLICT in the storage layer
    -- relies on.
    UNIQUE (user_id, product_id)

);


COMMIT;
