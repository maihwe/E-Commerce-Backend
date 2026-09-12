-- Migration 001
-- Create the users table.
--
-- Every account on the marketplace lives here.
-- The role column decides what the account is
-- allowed to do: buy, sell, or administer.

BEGIN;


-- Create the users table.
CREATE TABLE users (

    -- Unique identifier for each user.
    id SERIAL PRIMARY KEY,

    -- Display name shown on reviews and orders.
    name TEXT NOT NULL,

    -- User's login email.
    --
    -- UNIQUE prevents two accounts from registering
    -- with the same email address.
    email TEXT NOT NULL UNIQUE,

    -- Securely hashed password.
    --
    -- We will NEVER store the user's plain-text
    -- password in this column.
    password_hash TEXT NOT NULL,

    -- What the account is allowed to do.
    --
    -- 'buyer'  - browse, keep a cart, place orders.
    -- 'seller' - everything a buyer can do, plus
    --            listing products and fulfilling orders.
    -- 'admin'  - oversees categories, roles, coupons,
    --            and refunds.
    --
    -- New accounts default to buyer.
    role TEXT NOT NULL DEFAULT 'buyer'
        CHECK (role IN ('buyer', 'seller', 'admin')),

    -- When the account was created.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()

);


-- PostgreSQL already builds an index for the UNIQUE
-- constraint on email, which is exactly the index
-- login needs, so no separate index is required here.


COMMIT;
