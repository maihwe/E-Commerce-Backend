-- Migration 004
-- Create the coupons table.
--
-- A coupon is a discount code that an admin creates and
-- a shopper may apply at checkout.
--
-- Coupons are created before orders because an order
-- records which coupon it used, so the coupons table has
-- to exist first.

BEGIN;


CREATE TABLE coupons (

    id SERIAL PRIMARY KEY,

    -- The code a shopper types, for example "WELCOME10".
    --
    -- UNIQUE so two coupons can never share a code.
    -- Codes are stored in uppercase and compared in
    -- uppercase, so "welcome10" and "WELCOME10" are
    -- the same coupon rather than two confusing ones.
    code TEXT NOT NULL UNIQUE,

    -- How the discount is worked out.
    --
    -- 'percentage' takes discount_value percent off
    --              the subtotal.
    -- 'fixed'      takes discount_value naira off
    --              the subtotal.
    discount_type TEXT NOT NULL
        CHECK (discount_type IN ('percentage', 'fixed')),

    -- The size of the discount.
    --
    -- For a percentage this is a number from 0 to 100.
    -- The upper bound is enforced here so that no code
    -- can ever discount more than the whole order.
    discount_value NUMERIC(15, 2) NOT NULL
        CHECK (discount_value > 0),

    -- The order must reach this amount before the
    -- coupon applies. Zero means no minimum.
    min_order_amount NUMERIC(15, 2) NOT NULL DEFAULT 0
        CHECK (min_order_amount >= 0),

    -- How many times the coupon may be used in total.
    --
    -- NULL means unlimited.
    max_uses INT
        CHECK (max_uses IS NULL OR max_uses > 0),

    -- The window during which the coupon works.
    --
    -- Both are optional. A NULL start means the coupon
    -- is live immediately; a NULL end means it does
    -- not expire.
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,

    -- Lets an admin switch a coupon off without
    -- deleting the record of it.
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- An end date must not come before the start date.
    CHECK (
        ends_at IS NULL
        OR starts_at IS NULL
        OR ends_at > starts_at
    ),

    -- A percentage discount cannot exceed 100%.
    --
    -- The check only applies to percentage coupons,
    -- because a fixed discount of any size is fine as
    -- long as the order total never goes below zero,
    -- which the storage layer guards.
    CHECK (
        discount_type <> 'percentage'
        OR discount_value <= 100
    )

);


COMMIT;
