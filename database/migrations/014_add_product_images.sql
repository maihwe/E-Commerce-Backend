-- Migration 014
-- Give a product somewhere to keep a picture.
--
-- The column holds a path, not image data. Storefront
-- pictures live in a folder on disk and are served
-- straight out of it, so what the database records is
-- where the browser should look, and the folder stays
-- the thing an operator adds to. A bytea column would
-- put the pictures inside every backup and every query
-- that touched a product, which is a lot of weight for
-- a value that is only ever read to write one attribute.
--
-- TEXT NOT NULL DEFAULT '' matches how description is
-- declared in migration 002. An empty string means "no
-- picture yet" and is a perfectly ordinary value, so no
-- Go code has to deal with a NULL here. That matters
-- more than it sounds: a nullable column would turn
-- every scan into a sql.NullString and every caller
-- into a check.
--
-- An empty default is also what makes the picture
-- scanner idempotent. It only ever has to look at rows
-- where this is still '', so restarting the server does
-- not re-examine products that already have a picture.

BEGIN;


ALTER TABLE products
    ADD COLUMN image_path TEXT NOT NULL DEFAULT '';


COMMIT;
