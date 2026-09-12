-- Migration 013
-- Seed a few starter categories.
--
-- ON CONFLICT DO NOTHING means this migration can be run
-- again safely: categories that already exist are left
-- alone rather than causing a duplicate key error.

BEGIN;


INSERT INTO categories (name, slug) VALUES
    ('Phones & Tablets',      'phones-tablets'),
    ('Computers',             'computers'),
    ('Electronics',           'electronics'),
    ('Home & Kitchen',        'home-kitchen'),
    ('Fashion',               'fashion'),
    ('Health & Beauty',       'health-beauty'),
    ('Books & Stationery',    'books-stationery'),
    ('Groceries',             'groceries'),
    ('Sports & Outdoors',     'sports-outdoors'),
    ('Everything Else',       'everything-else')
ON CONFLICT (slug) DO NOTHING;


COMMIT;
