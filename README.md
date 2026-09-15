# E-Commerce Backend

[![CI](https://github.com/maihwe/E-Commerce-Backend/actions/workflows/ci.yml/badge.svg)](https://github.com/maihwe/E-Commerce-Backend/actions/workflows/ci.yml)

A REST API for a small marketplace, written in Go against PostgreSQL.

Shoppers browse a catalog, keep a cart, place orders, and pay for them through
Paystack. Sellers list products, restock them, and ship orders. Admins oversee
categories, roles, coupons, and refunds. The whole commerce loop is here:
catalog → cart → order → payment → fulfilment → review.

The same marketplace is also served as pages, under `/shop`, from this same
binary and this same database. See [The storefront](#the-storefront).

The two things this project is really about are **not taking stock twice and
not processing a payment twice**, and a lot of the design below exists for
those two reasons.

---

## Running it

### In short

Two terminals, and one line that is easy to forget.

```sh
# Terminal 1 — the database. Leave it running; this is the server,
# not a helper that finishes and exits.
go run ./database/devdb

# Terminal 2 — the API. Leave it running too.
. ~/ecb-env.sh
go run .
```

Then open <http://localhost:8080/shop>.

The line to forget is `. ~/ecb-env.sh`. Without it the API stops immediately
with `DATABASE_URL is not set`, which names the variable but not the file that
holds it. Each terminal has its own environment, so sourcing that file in one
does not help the next one — it must be sourced in **every** terminal that runs
the app or the tests. Sourcing a `.env` is not a substitute: the program has no
`.env` loader and never reads one.

Nothing else needs doing. The catalogue lives in PostgreSQL, in
`~/.ecb-postgres/data`, and survives every restart of both the database and the
API. It is not rebuilt, and it does not need refilling.

If the shop is *empty* — no listings at all — the catalogue has never been
created, which happens once per new database. That is the only time this third
command is needed:

```sh
# Terminal 3 — once, ever, and only against a running API.
. ~/ecb-env.sh
sh seed.sh
```

`seed.sh` creates one listing per row of the table inside `stock.sh`, so the
names, categories and prices are read from one place rather than two. Running it
twice is harmless: a listing that already exists is refused by its unique slug
and reported as a skip. It does not copy pictures — those are committed in
`pictures/` — so the listings it makes attach to them the next time the API
starts.

Two things worth knowing on the day something looks wrong:

- **A new photograph needs a restart of terminal 2.** `pictures/` is read once,
  at startup. Browsing never needs anything, and neither does an existing
  picture.
- **`go test ./...` cannot damage the catalogue.** The suite drops and rebuilds
  its schema on every run, which is exactly why it is pointed at a separate
  `ecommerce_test` database.

### 0. Go

The code is written to the Go 1.22 language level — `http.ServeMux`'s method and
wildcard patterns are the newest thing used here, and those arrived in 1.22 — but
the module declares `go 1.26.0`, because it has to. `pgx/v5 v5.10.0` declares
`go 1.25.0` and `golang.org/x/crypto v0.57.0` declares `go 1.26.0`, and a module
may not declare a language version older than its dependencies require. `go mod
tidy` rewrote that line from `1.22` to `1.26.0`, which is correct rather than a
mistake.

So a Go 1.26 toolchain does the building. Normally that is invisible: the `go`
command notices it needs a newer toolchain than the one installed and fetches it.
On a machine with no network, that fetch is exactly what fails — and the
toolchain is already in the module cache here, so there is a way through. See
[If the toolchain will not resolve](#if-the-toolchain-will-not-resolve) at the
end of this file, or just use `database/devdb`, which writes the right
`GOTOOLCHAIN` into `~/ecb-env.sh` for you.

Nothing needs doing if `go build ./...` works.

### 1. PostgreSQL

Any PostgreSQL 12 or newer will do. Point `DATABASE_URL` at it and skip to
step 3.

This was developed on a machine with **no PostgreSQL installed and no way to
install one**, because installing it needs root and the account has no sudo. A
database server does not actually need root: it needs a directory it can write
to and a port it can listen on. `database/devdb` therefore downloads a complete
PostgreSQL distribution, runs `initdb`, and starts the server inside your home
directory, as you.

```sh
go run ./database/devdb
```

It prints what it is doing, applies the migrations, and then waits. Press Ctrl-C
to stop it, and run everything else in a second terminal:

```sh
. ~/ecb-env.sh
```

That file is written by `devdb` and holds `DATABASE_URL`, `TEST_DATABASE_URL`,
and `GOTOOLCHAIN`. It is readable only by its owner, since it holds a password,
even though that password is a throwaway for a server on localhost.

The server listens on **5433**, deliberately not 5432, so that it can coexist
with a real installation. Its data lives in `~/.ecb-postgres/data` and survives
restarts. `ECB_PGDIR` and `ECB_PGPORT` move those if you need them moved.

This is the real PostgreSQL, not a stand-in. Every constraint, row lock, and
`ON CONFLICT` clause behaves exactly as it would against a system installation,
which matters a great deal here: the two things this project is about are a
`UNIQUE` constraint deciding whether a webhook is a duplicate and a `FOR UPDATE`
lock deciding whether two buyers can both take the last unit. A fake database
would have to reimplement both, and a test of a reimplementation proves nothing.

### 2. Configuration

Everything is read from the environment. There is no `.env` loader, so a `.env`
file on its own does nothing — it has to be sourced, or the variables exported
by hand:

```sh
export DATABASE_URL="postgres://postgres@localhost:5432/ecommerce?sslmode=disable"
export PAYSTACK_SECRET_KEY=sk_test_...
```

`.env.example` lists every variable the application reads, with a note on what
each does and what happens when it is missing. Copying it is a note to yourself
rather than something the program does:

```sh
cp .env.example .env
set -a; . ./.env; set +a
```

`.env` is in `.gitignore`. No credential should ever be committed, and
`.env.example` exists so the list of required variables is in the repository
without any of their values being.

Only `DATABASE_URL` is genuinely required. Without `PAYSTACK_SECRET_KEY` the
payment endpoints return an error and everything else works, which is the state
the test suite runs in.

### 3. Schema

Migrations are numbered SQL files applied in filename order. The numbers are
what makes sorting them as text the correct order, and each one may only
reference tables an earlier number has already created.

`devdb` applies them itself and records each in a `schema_migrations` table, so
starting it twice is harmless and adding a migration applies only the new one.
That ledger also lets it recognise a schema left half-built by an interrupted
run: such a database has some of the tables but no record of them, which no
single-table check could tell apart from a finished one, so `devdb` says so and
rebuilds it.

Against a PostgreSQL you are running yourself, apply them one at a time:

```sh
go run ./database/cmd 001_create_users.sql
```

`database/cmd` takes one filename and reads it from `database/migrations/`
relative to the working directory, so run it from the repository root.

### 4. Run

```sh
. ~/ecb-env.sh
export PAYSTACK_SECRET_KEY=sk_test_ecb
go run .

# Or with a shorter key, which the next paragraph explains.
```

The server listens on `:8080` unless `PORT` says otherwise. The storefront is
at `http://localhost:8080/shop`, and `/` redirects to it.

The secret key does two jobs: it authenticates calls to Paystack, and it is the
value an incoming webhook's signature is checked against. **Whatever signs a
webhook must use the same value the server was started with**, or every delivery
is answered `401` for a reason that looks nothing like a mismatched key. With no
key at all, the payment endpoints fail and everything else keeps working.

Keep the key short if you are typing it into a narrow terminal. A long one gets
split across lines, and the server starts happily with the first fragment — the
symptom is then every signature failing to verify, which points nowhere near
the real cause.

### 5. Tests

```sh
. ~/ecb-env.sh

go test ./...
```

`TEST_DATABASE_URL` is what the database-backed tests need. When it is unset
they skip rather than fail, so the pure unit tests still run on a machine with
no PostgreSQL at all. Against a server you run yourself:

```sh
TEST_DATABASE_URL="postgres://$USER@localhost:5432/ecommerce_test?sslmode=disable" \
    go test ./...
```

`TEST_DATABASE_URL` is deliberately a *different* variable from
`DATABASE_URL`, and the database it names is never written to. It is only a
place to connect to, so that `CREATE DATABASE` can be issued from it.

`ANTHROPIC_API_KEY` is the other variable that turns a skipping test on. One
test in `services` makes a real call to the model API, and skips without a key
rather than failing:

```sh
set -a; . ./.env; set +a

go test ./services -run RealAPI -v
```

Everything else in that package is checked against a local stand-in server, and
a stand-in agrees with whatever it is told: those tests pin what the client
*sends*. That one test is the only place that finds out whether the API accepts
it, so it is worth running once after any change to `services/vision.go`. It
checks the shape of the request and not the model's judgement — the picture it
sends is flat colour and has no right answer, so an empty reply is a passing
one.

Each test process then makes its own scratch database — the name in that
variable with the process id appended — applies the migrations to it, and
drops it when the tests finish. The isolation matters because `go test ./...`
runs the test binaries of different packages at the same time: the `handlers`
tests and the `storage` tests execute concurrently, and if they shared one
database, one of them could drop the schema while the other was still creating
tables in it. The resulting failures are spectacular and completely
misleading — migrations reporting that a table they had just created does not
exist.

The one requirement this places on the role in `TEST_DATABASE_URL` is
`CREATEDB`. When the variable is unset, the database-backed tests skip and the
pure unit tests still run.

### Continuous integration

`.github/workflows/ci.yml` runs the suite twice on every push and every pull
request, and the split follows the rule above rather than inventing a second
one. The first job builds, vets and tests with no `TEST_DATABASE_URL`, so the
database-backed tests skip and the job finishes in seconds. The second runs the
same command against a PostgreSQL service container, and waits on the first —
so a build that does not compile is reported before a database is started for
it.

Both jobs set `GOTOOLCHAIN=local` and take their Go version from `go.mod`
rather than from a number written into the workflow, so the two cannot drift
apart and bumping the `go` line is the only edit a new toolchain needs. Between
them, the runner's Go is the version this module asks for, and a runner whose
Go is older fails loudly instead of fetching a newer toolchain mid-build. That
is the same failure described at the end of this file, arriving as a red check
rather than as a confusing error on somebody else's machine.

---

## The three parts that had to be right

### 1. Idempotent payment webhooks

Payment providers retry. Paystack resends a webhook whenever it does not get a
prompt `200`, and it may keep resending for days. If a retry were processed as
though it were new, a shopper would be charged once and credited twice.

There are **three independent guards**, and each one on its own is enough to
prevent a double-process:

| # | Guard | Where |
|---|---|---|
| 1 | `UNIQUE (provider, provider_event_id)` on `webhook_events` | the claim insert |
| 2 | `UPDATE orders ... WHERE status = 'pending'` | the status change |
| 3 | `UNIQUE (order_id, product_id, reason)` on `inventory_movements` | the stock movement |

The handler:

1. **Reads the raw body first.** The signature is over the exact bytes
   Paystack sent, so parsing and re-encoding the JSON would change them and
   every genuine webhook would be rejected. Reading happens before any decode.
2. **Verifies `x-paystack-signature`**, an HMAC-SHA512 of the raw body, using
   `hmac.Equal` so the comparison takes the same time whatever the values are.
   A mismatch is `401` and nothing else happens.
3. **Claims the event** with `INSERT ... ON CONFLICT (provider,
   provider_event_id) DO UPDATE ... WHERE status = 'failed' RETURNING id`. No
   row returned means this delivery has already been handled, and the answer
   is `200` with `{"status":"duplicate"}` so the provider stops retrying.
4. **Does the work in one transaction.** On `charge.success` it compares the
   paid amount against the order total, moves the order `pending → paid`,
   appends the `sale` movements, and stamps the event processed.

A failure is recorded as `failed` — and because the claim only matches rows
that are `failed`, a retry is allowed to take the event over. That matters:
without it, one transient error would lose a real payment forever.

`GET /payments/verify/{reference}` is the recovery path for a webhook that
never arrived. It calls Paystack directly, which is also the safer of the two
sources, since a webhook is something an outsider can try to forge. It reuses
the same guarded confirmation, so the two paths cannot double-process either.

### 2. Inventory as a ledger

There is no stock column. Available stock is always:

```sql
SELECT COALESCE(SUM(quantity_change), 0)
FROM inventory_movements
WHERE product_id = $1
```

The same pattern as the Financial Tracker in this series, where an account
balance is never stored either. One source of truth, so a stored total can
never drift away from the history that produced it. The table is append-only,
which also makes it a complete audit trail of every restock, sale, refund, and
correction.

Stock is **not** reduced when an order is placed, only when payment is
confirmed. Placing an order checks that the stock is there so the shopper is
told early, but that check is a courtesy rather than a reservation.

**Oversell protection.** A `SUM` cannot carry a `CHECK` constraint, so nothing
in the schema stops the total going negative. The guarantee comes from locking
the product rows inside the confirming transaction:

```sql
SELECT id FROM products WHERE id = ANY($1::int[]) ORDER BY id FOR UPDATE
```

The rows are locked in a consistent order, which is what stops two concurrent
confirmations deadlocking. The stock is then re-derived while the lock is
held, and if any line is short the whole transaction is rejected. Two
confirmations for the last unit therefore serialise, and the second one finds
nothing left.

`storage/inventory_test.go` runs exactly that race and asserts that precisely
one confirmation succeeds.

### 3. The order state machine

```
pending   → paid, cancelled
paid      → shipped, refunded
shipped   → delivered, refunded
delivered → refunded
```

`cancelled` and `refunded` are terminal. `cancelled` is reachable only from
`pending`: once money has changed hands the honest exit is a refund, not a
cancellation, so the records show that a payment really happened and was
returned.

`models/order_status.go` is the single source of truth. It is enforced twice —
once in Go, so the client gets a clear `409` naming both statuses, and once in
the SQL's `WHERE status = $from`, so two requests racing each other still
cannot both win. Every accepted change appends to `order_events`, which is
append-only like the ledger.

---

## Endpoints

Session cookie required unless marked public. `buyer` means any signed-in
account.

### Auth

| Method | Path | Who |
|---|---|---|
| `POST` | `/register` | public |
| `POST` | `/login` | public |
| `POST` | `/logout` | anyone |
| `GET` | `/me` | signed in |

`POST /register` creates the account and does **not** sign you in, so `POST
/login` is a second call. It always creates a `buyer`, or a `seller` if `role`
asks for one; `admin` cannot be requested at all, which is deliberate but also
leaves a fresh database with no admin in it. See the note under the walkthrough.

### Catalog

| Method | Path | Who |
|---|---|---|
| `GET` | `/products` | public |
| `GET` | `/products/{id}` | public |
| `POST` | `/products` | seller |
| `PUT` | `/products/{id}` | owning seller, or admin |
| `DELETE` | `/products/{id}` | owning seller, or admin |
| `POST` | `/products/{id}/restock` | owning seller, or admin |
| `GET` | `/products/{id}/inventory` | owning seller, or admin |
| `GET` | `/products/{id}/reviews` | public |
| `POST` | `/products/{id}/reviews` | signed in |
| `PUT` | `/products/{id}/reviews` | the reviewer |
| `DELETE` | `/products/{id}/reviews` | the reviewer |
| `GET` | `/products/{id}/recommendations` | public |
| `GET` | `/categories` | public |
| `POST` | `/categories` | admin |

`GET /products` accepts `q`, `category`, `category_slug`, `seller`,
`min_price`, `max_price`, `sort`, `page`, `per_page`, and answers with
`{data, page, per_page, total, total_pages}`.

Sort keys are `newest`, `oldest`, `price_asc`, `price_desc`, `name`, `rating`.
An unknown key falls back to the default rather than failing, and the client's
text is never pasted into the SQL string: it is only ever used to look up one
of a fixed set of fragments.

`POST /products/{id}/reviews` answers with the row as stored, so its
`reviewer_name` is empty; `GET /products/{id}/reviews` is the one that joins
the name in and adds a `summary` with the count and the average. The create
response is the row, the list response is the row with its context around it.
The create request takes `rating`, `title`, and `body` — a field the struct
does not know about is ignored in silence rather than refused, which is how Go
decodes JSON and is worth knowing before wondering where a `comment` went.

### Cart, wishlist, orders

| Method | Path | Who |
|---|---|---|
| `GET` `DELETE` | `/cart` | signed in |
| `POST` | `/cart/items` | signed in |
| `PATCH` `DELETE` | `/cart/items/{productID}` | signed in |
| `GET` | `/wishlist` | signed in |
| `POST` | `/wishlist/items` | signed in |
| `DELETE` | `/wishlist/items/{productID}` | signed in |
| `POST` | `/orders` | signed in |
| `GET` | `/orders` | own orders, per role |
| `GET` | `/orders/{id}` | buyer, seller on the order, or admin |
| `GET` | `/orders/{id}/events` | same |
| `POST` | `/orders/{id}/cancel` | buyer, or admin |
| `POST` | `/orders/{id}/ship` | seller with an item on the order |
| `POST` | `/orders/{id}/deliver` | buyer, or admin |
| `POST` | `/orders/{id}/refund` | admin, with a reason |
| `GET` | `/orders/{id}/chat` | WebSocket, same rule as above |

### Payments and coupons

| Method | Path | Who |
|---|---|---|
| `POST` | `/orders/{id}/pay` | the buyer |
| `GET` | `/payments/verify/{reference}` | the buyer, or admin |
| `POST` | `/webhooks/paystack` | public, signature-verified |
| `POST` | `/coupons` | admin |
| `GET` | `/coupons` | admin |
| `PATCH` | `/coupons/{id}` | admin |
| `POST` | `/coupons/validate` | signed in |

### Admin

| Method | Path |
|---|---|
| `GET` | `/admin/users` |
| `PUT` | `/admin/users/{id}/role` |
| `GET` | `/admin/orders` |

### Live order chat

`GET /orders/{id}/chat` upgrades to a WebSocket. Only the buyer, a seller with
an item on the order, and admins may connect. The full conversation is sent on
connect, so reopening a chat shows everything said while it was closed, and
every message is written to the database *before* it is broadcast — a message
that appeared and then vanished on refresh would be worse than one that
arrived late.

Status changes are broadcast into the same room, which is what makes this live
**order-status** chat rather than a plain message box:

```json
{"type":"message","message":{...}}
{"type":"order_status","status":"shipped","note":"Shipped by the seller"}
```

---

## The storefront

The same marketplace is served as pages at `/shop`, from this binary and against
this database. Pages rather than a separate frontend, for one concrete reason:
sessions live in an in-memory map inside the `storage` package, so a second
process would hold a map of its own and a sign-in made in a browser would be
invisible to the API. One process means one map, and one answer to the question
of who is signed in.

| Method | Path |
|---|---|
| `GET` | `/shop` |
| `GET` | `/shop/products/{id}` |
| `GET` `POST` | `/shop/login` |
| `GET` `POST` | `/shop/register` |
| `POST` | `/shop/logout` |
| `GET` | `/shop/static/…` |
| `GET` | `/shop/img/…` |

It is under `/shop` rather than at the root because every meaningful path at the
root is already registered by the API, and `http.ServeMux` panics on a duplicate
pattern — sharing would be a crash at startup rather than a routing preference.
Root was the one path the API left free, and it redirects here.

### It repeats the shape of a request, never the rules

Turning a form into arguments happens in `web/`, because a form is not a JSON
body. Everything after that is the same code the JSON handlers call: `storage`
for the queries, `models` for the order state machine and the coupon rules,
`handlers` for authentication and sessions. A page and an endpoint cannot come to
different conclusions about what is in stock, which status changes are allowed,
or how much a coupon takes off, because each of those answers is written in
exactly one place.

Concretely, `POST /shop/login` calls `handlers.AuthenticateUser` and
`handlers.StartSession`, the same two functions `POST /login` calls, and a form
body is capped at the same 1 MiB the JSON endpoints allow.

The one deliberate disagreement is registration. `POST /register` creates the
account and does **not** sign you in; `POST /shop/register` does both. That is
not the page departing from the rule, it is the two documented steps performed
without making somebody type their brand-new password a second time.

### The band at the top of the catalog

It has three states, and two of them exist in order to *not* show a picture.

- **While a search is running** it is a plain tint. The pictures are of specific
  products, and three of them with nothing to do with what was asked for are
  worse than no picture at all.
- **With pictures to show** it cycles through up to four of them, drawn from the
  products on this page — so the band shows what the shop actually has, and it
  follows the sort the shopper chose.
- **With nothing photographed** it falls back to `web/static/img/hero.jpg`, the
  one photograph committed to the repository rather than dropped into a folder.
  A shop where nobody has taken a picture yet should still open with something
  to look at.

The rotation is pure CSS, one `@keyframes` per slide count, and that is why it
stops at four. The moment each picture gives way to the next is a percentage of
the whole cycle, and a percentage in a keyframe selector has to be literal — it
cannot be computed from how many elements are on the page. So the stylesheet
carries a rule for two slides, one for three and one for four, and
`web/catalog.go` never sends a fifth. Raising that cap without adding a rule does
not break the page: the band stops cycling and shows one still picture rather
than a black gap.

`prefers-reduced-motion` turns the rotation off entirely.

### Product pictures

A tile shows the product's photograph when it has one, its category's icon when
it does not, and the product's initials when the category has no icon either — so
a catalog nobody has photographed yet still reads as a shop rather than as a grid
of grey boxes.

Photographs are the one thing the storefront serves that is not compiled into the
binary. They live in a folder on disk, are read once at startup, are served from
`/shop/img/`, and the URL is what `products.image_path` holds. The naming rule,
the line the server logs at startup, and how to prepare a photograph off a phone
are in [pictures/README.md](pictures/README.md).

`image_path` is the one column no request can write. It is set by the picture
scanner and by nothing else, which is what stops a seller pointing a listing at
an address of their choosing.

#### Sorting a picture into a category

Attaching a photograph does a second thing. The picture is sent to a Claude
vision model, which is asked which of the shop's categories the product in it
belongs to, and the product is moved to that one. That is the difference between
a shop whose pictures are right and a shop that is right in the way a shopper
notices, because the categories are the buttons they browse with.

**The picture only sorts what nobody has sorted.** `category_id` is `NOT NULL` and
the create handler refuses a product that arrives without one, so there is no such
thing as a listing with a blank category for a picture to fill in. What there is
instead is the seeded catch-all, `everything-else`, whose whole meaning is "not
decided yet": a product parked there is moved to the category the picture
suggests. A product in any other category is left where its seller put it — not
written, not read, not sent. A picture is one photograph; the seller had the thing
in their hand, and a model that has seen one photograph is in no position to
overrule them. The cost is that a product sitting in the wrong *real* category
stays there, which is the seller's edit to make and the same one they would have
had to make if the picture had guessed wrong.

That rule lives in one small function, `maySort`, beside the column that makes it
necessary, and a shop that renames or deletes its catch-all sorts nothing at all —
conservatively, since the alternative is a pass that treats every category as fair
game the moment somebody edits a name. The report counts those pictures rather
than falling silent, so the reason is on screen instead of being guessed at.

**Only the pictures a scan has just attached are looked at.** A picture that was
already in place was counted under `Skipped`, so restarting the server sends
nothing anywhere and costs nothing. There is no "classified" flag and no table of
what has been done, because the folder and the database already agree about it —
the same idempotency the scan itself uses, doing a second job.

**The answer is never stored.** The model is handed the shop's own categories and
its reply is looked up in that list, so a reply that is anything else is discarded
and the product keeps the category its seller chose. The worst a wrong answer can
do is choose the wrong one of ten things somebody already decided the shop sells;
it cannot introduce a category, and no sentence the model writes reaches the
database. It is the same discipline the catalog's sort keys follow, where the
client's text is only ever used to look up one of a fixed set of fragments.

Every failure belongs to one picture. An unreadable file, a refused call, an
answer that matches nothing: each is logged with its reason and the next picture
is tried. The picture is attached and shown either way.

Without `ANTHROPIC_API_KEY` nothing is sent, every product keeps the category its
seller gave it, and the rest of the shop is unaffected — the same arrangement
`PAYSTACK_SECRET_KEY` has. AVIF is the one format that is attached and shown but
not sent, since the API takes JPEG, PNG, GIF and WebP; the report names those
files rather than leaving the gap to be noticed.

`services/vision.go` is the client and `pictures/classify.go` is the pass over the
folder. The client is shaped like `services/paystack.go`: raw `net/http`, no SDK,
and no new dependency.

**The live call has not been made from this repository.** It needs a key and
billing that this project does not have, which is the same position the Paystack
integration is in — a real client, real parameters, and no credentials to point
it at. Everything short of the call is tested against a local stand-in server,
and a stand-in agrees with whatever it is told. So the tests here pin what this
client *sends*; whether the API accepts it is a separate question, and
`services/vision_live_test.go` is the one test that answers it. It skips without
a key and makes the real call with one, so it is worth running once after any
change to that file.

### The shelf under a listing

Under every listing is a short strip of other products from the same category,
under a heading that names it: *More in Phones & Tablets*.

It is drawn from `ListTopRatedInCategoryFromDB`, and not from the
`GetRecommendationsFromDB` that backs `GET /products/{id}/recommendations`. That
is a deliberate disagreement between the page and the endpoint, and it is worth
being exact about, because the endpoint's answer is the better one in general.

The endpoint prefers what people bought together, which is the right answer to
"what goes with this" and the wrong answer to the question the strip is asking.
Two reasons, and they are the same reason twice.

The heading is the first. A strip that mixes categories cannot be titled *More
in Phones & Tablets*, and a heading that names nothing — *You might also like* —
turns the strip into decoration. Naming the category is what makes it a way into
the rest of the shelf, so that following one product is staying where the shopper
already is, which is the whole point of a catalog sorted into categories.

The tiles are the second. A tile with no photograph is drawn from its product's
category, and this page knows the name of exactly one category, its own. A
recommendation from somewhere else could only be drawn with initials where its
own category had an icon. Four products, all the same kind of thing, is what lets
the strip reuse the catalog's card markup exactly — so a product looks the same
in the strip as it does in the grid it came from.

A failure to load the strip draws nothing rather than failing the page. The
product is what the visitor came for; the strip is below it.

The one thing the strip does not carry is the catalog's rating line. It shows the
price and how many are left, which is what choosing between two of something
turns on.

---

## A walkthrough

The whole loop, end to end, as a script:

```sh
. ~/ecb-env.sh
export PAYSTACK_SECRET_KEY=sk_test_ecb
sh walkthrough.sh
```

It takes a few seconds and prints every request, every response, and the HTTP
status of each. At the end it asserts the things that actually matter, and exits
non-zero if any of them is wrong rather than stopping at the first:

```
  ok    order status after the underpayment = pending
  ok    order status = paid
  ok    stock remaining = 8
  ok    sale movements for this order = 1
  ok    paid events in the history = 1
  ok    webhook event rows = 1
```

The interesting part is the middle. Paystack's notification is delivered
**twice**, byte for byte, exactly as a retry arrives, and the run then checks
that the second delivery changed nothing at all: the order is still `paid`, the
stock is still 8 rather than 6, and there is exactly one `sale` movement, one
`paid` event, and one event row. Before that, a webhook with a bad signature is
refused with `401`, and one carrying a genuine signature but claiming one kobo
instead of ₦33,300 is refused with `500` — with the order left `pending`.

It is a script rather than a page of commands to paste because of the terminal
it was written on: that one wraps at about forty columns and inserts a real
newline when it does, so a pasted `curl` with a JSON body arrives at the shell
in pieces and the shell runs the pieces. A file has no such problem.

### The one thing it does by hand, and why

**It cannot really pay.** `POST /orders/{id}/pay` calls Paystack for an
authorization URL, and a made-up secret key gets refused. The script shows that
refusal rather than hiding it: the response is a real `502` from a real call to
`api.paystack.co`, which is rather the point — the integration is live, the
credentials are not. It then sets `payment_reference` directly, which is the one
step a real payment would have performed and nothing more, and which is exactly
what `TestPaystackWebhookIsProcessedExactlyOnce` does. With a real sandbox key
that line disappears and nothing else changes.

**There is no way to create the first admin through the API.** `POST /register`
downgrades anything that is not `seller` to `buyer`, so admin rights may only
ever be granted by an existing admin — and a fresh database has no admin to do
the granting. Coupons, every `/admin` route, and refunds are unreachable until
one exists.

An endpoint is the wrong answer here, because an endpoint is a way for anyone to
ask for the privilege. The right answer is an operator-run step, and that is
`database/promote`:

```sh
go run ./database/promote you@example.com
```

It promotes exactly one named account and has no way to promote everyone, so a
mistyped or missing argument cannot leave a database in which every account is
an administrator. That is the accident a bare `UPDATE` invites, and it is why
this is a program rather than a line of SQL in a README — the SQL would be
shorter. Running it twice is harmless, and a name matching no account is
answered with the list of the accounts that do exist.

`walkthrough.sh` runs it at step 7, so the bootstrap path documented here is the
same one the walkthrough exercises rather than a parallel arrangement that only
the script knows about.

### Reading values back out of the database

`database/query` runs one statement and prints the result, one row per line. It
exists because the PostgreSQL described in step 1 ships no `psql` client and none
can be installed without root, so the walkthrough would otherwise have no way to
check what actually landed in the tables:

```sh
go run ./database/query "SELECT COALESCE(SUM(quantity_change), 0) FROM inventory_movements WHERE product_id = 1"
```

It uses the same driver and the same `DATABASE_URL` as the application, so the
connection settings cannot drift apart from the ones the application uses. A
`numeric` column comes back in the driver's own representation rather than as a
decimal string, so a query needing a plain number should cast it:
`(total * 100)::bigint`.

### Watching the chat

Log in and keep the cookie somewhere, then open the socket as that buyer:

```sh
curl -s -c /tmp/ecb-buyer.txt -X POST http://localhost:8080/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"chidi@example.com","password":"password123"}'

websocat -H="Cookie: session_token=$(awk '/session_token/{print $7}' /tmp/ecb-buyer.txt)" \
  ws://localhost:8080/orders/1/chat
```

The walkthrough creates its accounts with a timestamp in the email so that
repeated runs do not collide, so use whichever address you actually registered.
Then have the seller ship the order from another terminal: the status change
arrives on the open socket as it happens, alongside the message history that was
sent when the socket connected.

---

## Design notes

**Money is integer kobo everywhere.** A `float64` cannot hold `0.10` exactly,
and a few of those added together give a visibly wrong total, which is not
acceptable for money. Amounts are `NUMERIC(15,2)` in PostgreSQL and `int64`
kobo in Go, converted once at the Paystack boundary with `math.Round`. The
same reasoning applies to discounts: `ComputeDiscountKobo` does all its
arithmetic in integers.

**Every coupon rule lives in one function.** `models.RejectCoupon` holds all of
them, and both the preview endpoint and the checkout path call it. That is
what makes it impossible for a coupon to preview successfully and then be
refused when the order is actually placed.

**Orders freeze their prices; carts do not.** `order_items` keeps its own copy
of the product name and unit price, because an order is a record of a
historical event and must still show what the shopper agreed to pay even after
the seller renames or reprices the product. A cart is a plan, so it always
shows today's prices.

**Roles gate selling, not browsing.** Buying is open to everyone —
`role` only decides who may list products, fulfil orders, and administer the
marketplace. The buyer of an order, a seller with an item on it, and admins
may see it; everyone else gets `404` rather than `403`, because a `403` would
confirm that an order with that number exists.

**Recommendations are a co-occurrence count.** `order_items` is joined to
itself on `order_id`, one side pinned to the product being viewed and the
other counting what else appeared on those same orders, with
`COUNT(DISTINCT order_id)` so one large order cannot dominate. A product that
has never been bought falls back to the best rated items in its category.
There is no machine learning here and the code does not pretend otherwise: it
reports what people actually bought together.

**Deleting a product archives it.** Order lines point at products, so removing
the row would either break the reference or force the order history to be
rewritten. `DELETE /products/{id}` marks the product inactive instead.

---

## Layout

```
main.go                 routes and startup

walkthrough.sh          the whole commerce loop, end to end

models/                 plain data types, plus the order state machine
                        and the coupon rules
storage/                every SQL query, one file per subject
handlers/               HTTP: parse, authorise, delegate, answer
web/                    the same marketplace as pages, under /shop
services/               Paystack client, and the chat hub
utils/                  money, passwords, sessions, pagination,
                        signatures, validation
pictures/               the photograph folder, and the scanner that
                        attaches a file to the product it is named after
database/
  connection.go         the connection pool
  migrations.go         the migrations, embedded for tests
  migrations/*.sql      the schema, numbered
  cmd/migrate.go        applies one migration file
  devdb/                runs PostgreSQL with no installation and no root
  query/                runs one statement, standing in for psql
  promote/              makes an account an admin, for the first one
  testdb/               prepares a scratch database for the tests
```

Storage functions take `pool *pgxpool.Pool` first and are named
`GetXFromDB` / `CreateXInDB` / `UpdateXInDB`, following the convention used
elsewhere in this series. Handlers are factories returning `http.HandlerFunc`,
so each one closes over exactly the dependencies it needs.

Routing uses the standard library's `ServeMux` with method and wildcard
patterns, which Go 1.22 added:

```go
mux.HandleFunc("GET /products/{id}", handlers.GetProductHandler(pool))
mux.HandleFunc("POST /orders/{id}/ship", handlers.ShipOrderHandler(pool, hub))
```

At this number of endpoints, parsing paths by hand would be a great deal of
brittle string work for no benefit, and this keeps the whole routing table
readable in one place with no router dependency.

---

## Security notes

- **Secrets come from the environment** and are never committed. `DATABASE_URL`
  and `PAYSTACK_SECRET_KEY` are read at startup.
- **The development database's password is not a secret, and is not treated as
  one.** `database/devdb` runs PostgreSQL on localhost with a fixed password, so
  that the connection string in `~/ecb-env.sh` stays the same from one run to the
  next. It listens only on the loopback interface, it exists only while that
  program runs, and its data directory is inside your home directory. None of
  that would be true of a real deployment, where the password belongs in the
  environment like everything else.
- **The repo is not a substitute for a secret store.** `.env.example` documents
  which variables exist; `.env` and `~/ecb-env.sh` hold values and are both
  outside version control.
- **Passwords are hashed** with bcrypt and the hash is `json:"-"`, so it cannot
  be serialised into a response by accident. `ListUsersInDB` does not select
  the column at all, which is stronger than remembering to strip it.
- **The webhook signature is checked before anything is parsed**, using the raw
  bytes and a constant-time comparison.
- **Search text is never interpolated into SQL.** Every filter value is a
  numbered placeholder, and sort keys are looked up in a fixed map.
- **The picture folder serves pictures and nothing else.** `/shop/img/` answers
  `404` to a directory listing, to a file that is not there, and to a file whose
  extension is not one of six image types — the same answer in every case, so the
  address cannot be used to ask which files exist. SVG is deliberately not among
  the six: an SVG is a document that can carry script, and it would be served
  from this site's own origin.
- **The sign-in page cannot be used as an open redirect.** `next` arrives in the
  query string, so it is honoured only when it points inside `/shop`, and a value
  beginning `//` or `/\` is refused even though it starts with a slash. Without
  that check, a link really would begin at this site and end at another one's.
- **No text a model produces is ever stored.** The picture classifier returns a
  slug which is looked up in the shop's own list of categories, and an answer
  that is not one of them is discarded. The model cannot add a category and
  cannot write anything a shopper will read.
- **Two things must be changed before this is exposed publicly:**
  - the session cookie's `Secure` flag, currently `false` so that it works over
    local HTTP;
  - `CHAT_ALLOWED_ORIGINS`, currently empty, which makes the WebSocket accept
    any origin. A WebSocket is not covered by the browser's same-origin
    policy, so without this check any website could open a socket to this
    server using a signed-in visitor's cookies.

---

## If the toolchain will not resolve

If `go build ./...` stops with something like:

```
go: golang.org/x/crypto@v0.57.0 requires go >= 1.26.0 (running go 1.22.2)
```

then the `go` command could not fetch the newer toolchain and fell back to the
installed 1.22.2, which is too old for the dependencies. The toolchain it wants
is already on this machine, at:

```
~/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.0.linux-amd64/
```

That directory is a complete Go distribution, not just a module: it contains
`VERSION`, `go.env`, and `bin/`. Naming it as the toolchain is enough, and needs
no network:

```sh
export GOTOOLCHAIN=go1.26.0
```

That is the form to prefer, and it is what `~/ecb-env.sh` sets. It is one short
line, which matters on a terminal narrow enough to wrap: a longer spelling gets
split across two lines, and the second half arrives at the shell as a command of
its own.

If that still will not resolve, call the buried `go` directly. `GOTOOLCHAIN=local`
is what stops it from looking for a *newer* toolchain of its own and undoing the
point:

```sh
GOTOOLCHAIN=local \
  ~/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.0.linux-amd64/bin/go \
  version
```

Expect `go version go1.26.0 linux/amd64`. The same three words work in front of
any other `go` subcommand.

Two things to expect from `go mod tidy`, both of which have already happened
here. It rewrote the `go` line in `go.mod` from `1.22` to `1.26.0`, which is
correct rather than a mistake, and it is the same rule that makes the automatic
toolchain switch necessary in the first place. It may also drop the `toolchain`
line if the language version already implies it. Neither changes what the source
depends on; the code is still written to the Go 1.22 language level, and the
`go` line is a statement about what the module requires, not about what it uses.

---

## License

MIT — see [LICENSE](LICENSE).

It covers the code. The photographs in `pictures/` are product images rather
than source, and this file makes no claim about them one way or the other.

