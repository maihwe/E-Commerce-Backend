-- Migration 003
-- Create the carts and cart_items tables.
--
-- A cart belongs to exactly one account and is stored
-- in the database, so it survives logging out and
-- signing in again, or switching to another device.

BEGIN;


-- Create the carts table.
CREATE TABLE carts (

    id SERIAL PRIMARY KEY,

    -- One cart per user.
    --
    -- UNIQUE means we can never end up with two carts
    -- for the same account, which would split a
    -- shopper's items across both.
    user_id INT NOT NULL UNIQUE
        REFERENCES users(id) ON DELETE CASCADE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()

);


-- Create the cart_items table.
CREATE TABLE cart_items (

    id SERIAL PRIMARY KEY,

    cart_id INT NOT NULL
        REFERENCES carts(id) ON DELETE CASCADE,

    product_id INT NOT NULL
        REFERENCES products(id) ON DELETE CASCADE,

    -- How many of this product the shopper wants.
    --
    -- The CHECK keeps a quantity from ever being zero
    -- or negative. Removing an item means deleting the
    -- row, not setting the quantity to zero.
    quantity INT NOT NULL CHECK (quantity > 0),

    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A product may only appear once per cart.
    --
    -- Adding the same product again updates the
    -- existing row instead of creating a duplicate,
    -- which the ON CONFLICT in the storage layer uses.
    UNIQUE (cart_id, product_id)

);


COMMIT;
