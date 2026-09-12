-- Migration 012
-- Add the indexes the catalog and order lookups need.
--
-- The indexes that matter most are the ones behind the
-- inventory sum and the order listings, because those
-- queries run on every product read and every order
-- list. The rest keep filters from scanning whole
-- tables as the marketplace grows.

BEGIN;


-- Browsing the catalog.
--
-- The default listing shows active products, newest
-- first, so one index over both columns serves that
-- query directly.
CREATE INDEX products_active_created_at_idx
    ON products (is_active, created_at DESC);

-- Filtering by category.
CREATE INDEX products_category_id_idx
    ON products (category_id, is_active);

-- A seller listing their own products.
CREATE INDEX products_seller_id_idx
    ON products (seller_id, is_active);

-- Sorting by price.
CREATE INDEX products_price_idx
    ON products (price);


-- Listing a buyer's orders, newest first.
CREATE INDEX orders_buyer_id_created_at_idx
    ON orders (buyer_id, created_at DESC);

-- Filtering orders by status, which an admin dashboard
-- and the fulfilment queue both do.
CREATE INDEX orders_status_idx
    ON orders (status, created_at DESC);


-- Loading the lines of an order.
CREATE INDEX order_items_order_id_idx
    ON order_items (order_id);

-- A seller listing the orders they need to fulfil.
CREATE INDEX order_items_seller_id_idx
    ON order_items (seller_id);

-- The "customers also bought" query joins order_items
-- to itself on product_id, so this index is what keeps
-- the recommendation lookup quick.
CREATE INDEX order_items_product_id_idx
    ON order_items (product_id);


-- Reading a wishlist.
CREATE INDEX wishlist_items_user_id_idx
    ON wishlist_items (user_id);


-- A NOTE ON SEARCH
--
-- Searching the catalog uses ILIKE with a leading
-- wildcard, for example:
--
--     WHERE name ILIKE '%phone%'
--
-- A normal index cannot help with that, because the
-- match can begin anywhere in the text. At the size this
-- project runs at, that is perfectly fast.
--
-- If the catalog grew into many thousands of products,
-- the fix would be the pg_trgm extension and a GIN
-- index over the name, which lets PostgreSQL search
-- inside text quickly. That is deliberately left out
-- here to keep the migrations free of extension
-- requirements.


COMMIT;
