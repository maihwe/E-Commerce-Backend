-- Migration 002
-- Create the categories and products tables.
--
-- Note that products has NO stock column.
-- Stock is worked out by adding up the rows in
-- inventory_movements (migration 004), the same way
-- the Financial Tracker works out a balance by adding
-- up income and expense rows.

BEGIN;


-- Create the categories table.
CREATE TABLE categories (

    id SERIAL PRIMARY KEY,

    -- Human-readable label, for example "Home & Kitchen".
    name TEXT NOT NULL UNIQUE,

    -- URL-friendly version of the name.
    --
    -- Clients may filter the catalog with
    -- /products?category=home-kitchen, which reads
    -- much better than a numeric id in a link.
    slug TEXT NOT NULL UNIQUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()

);


-- Create the products table.
CREATE TABLE products (

    id SERIAL PRIMARY KEY,

    -- The account that listed this product.
    --
    -- ON DELETE CASCADE means that removing a seller
    -- account also removes the listings that belonged
    -- to it, so no product is left without an owner.
    seller_id INT NOT NULL
        REFERENCES users(id) ON DELETE CASCADE,

    -- The category this product sits in.
    --
    -- Products are not deleted with a category, so
    -- RESTRICT is used: a category must be emptied or
    -- reassigned before it can be removed.
    category_id INT NOT NULL
        REFERENCES categories(id) ON DELETE RESTRICT,

    name TEXT NOT NULL,

    slug TEXT NOT NULL UNIQUE,

    -- Long description shown on the product page.
    description TEXT NOT NULL DEFAULT '',

    -- Price in the marketplace currency.
    --
    -- NUMERIC(15, 2) stores money exactly, with two
    -- decimal places, which floating-point numbers
    -- cannot guarantee.
    price NUMERIC(15, 2) NOT NULL
        CHECK (price >= 0),

    -- Currency the price is quoted in.
    --
    -- Nigerian Naira, since the marketplace settles
    -- through Paystack.
    currency TEXT NOT NULL DEFAULT 'NGN',

    -- Lets a seller hide a listing without deleting it.
    --
    -- Hidden products disappear from the catalog but
    -- still appear correctly on orders that were
    -- already placed, because each order item keeps
    -- its own copy of the name and price.
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()

);


COMMIT;
