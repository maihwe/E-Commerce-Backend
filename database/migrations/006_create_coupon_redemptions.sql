-- Migration 006
-- Create the coupon_redemptions table.
--
-- This is an append-only record of every time a coupon
-- was actually used.
--
-- Counting the rows here is how we know how many times a
-- coupon has been used, rather than keeping a running
-- total on the coupons table. The same reasoning as the
-- inventory ledger: one source of truth cannot drift.

BEGIN;


CREATE TABLE coupon_redemptions (

    id SERIAL PRIMARY KEY,

    coupon_id INT NOT NULL
        REFERENCES coupons(id) ON DELETE CASCADE,

    order_id INT NOT NULL
        REFERENCES orders(id) ON DELETE CASCADE,

    -- How much this redemption actually took off.
    --
    -- Stored rather than recalculated, because the
    -- discount depends on the order subtotal at the
    -- time and must not change afterwards.
    discount_amount NUMERIC(15, 2) NOT NULL
        CHECK (discount_amount >= 0),

    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One coupon may be redeemed once per order.
    --
    -- This also makes applying a coupon idempotent:
    -- if the same order somehow tried to redeem the
    -- same coupon twice, PostgreSQL would refuse the
    -- second row.
    UNIQUE (coupon_id, order_id)

);


-- Counting how many times a coupon has been used is a
-- frequent lookup, so index the coupon side.
CREATE INDEX coupon_redemptions_coupon_id_idx
    ON coupon_redemptions (coupon_id);


COMMIT;
