#!/bin/sh
#
# A scripted walk through the whole commerce loop,
# end to end, against a running server.
#
# This exists as a script rather than as a page of curl
# commands in the README because of the terminal it was
# written on. That terminal wraps at about forty columns
# and inserts a real newline when it does, so a pasted
# curl command with a JSON body arrives at the shell in
# pieces and the shell runs the pieces. A file has no
# such problem: the text reaches curl exactly as written.
#
# It walks the whole arc: registering a seller and a
# buyer, listing a product, putting stock in through the
# ledger, filling a cart, minting a coupon as an admin,
# placing an order, taking payment through a signed
# webhook, delivering that same webhook twice and checking
# the retry changed nothing, shipping, delivering,
# reviewing, refusing the status changes that are not
# allowed, and finally a second order that gives the
# recommendation endpoint a co-purchase to learn from.
#
# The headings printed as it goes are the running order.
#
# Usage:
#
#     . ~/ecb-env.sh
#     export PAYSTACK_SECRET_KEY=sk_test_ecb
#     sh walkthrough.sh
#
# The server must have been started with the same
# PAYSTACK_SECRET_KEY, because the webhook signature is
# computed from it at both ends. Without a key the API
# answers 401 to every webhook.
#
# The key is deliberately short. A longer one got split in
# half by a terminal that wraps at forty-four columns,
# which started the server with a truncated key and would
# have made every signature fail to verify for a reason
# that looked nothing like the real one.
#
# Every account, product, and coupon this creates carries
# the run's timestamp in its name, so running it twice
# against the same database works and does not collide
# with the first run's rows.

set -e

BASE=${BASE:-http://localhost:8080}

if [ -z "$DATABASE_URL" ]; then

    echo "DATABASE_URL is not set." >&2

    echo "Run this first:  . ~/ecb-env.sh" >&2

    exit 1
fi

# The secret the API was started with. The fallback is the
# value above, so the two agree whether or not this shell
# exports it.
SECRET=${PAYSTACK_SECRET_KEY:-sk_test_ecb}

# RUN makes every name this script creates unique, so a
# second run does not trip over the first run's rows.
RUN=$(date +%s)

WORK=$(mktemp -d)

trap 'rm -rf "$WORK"' EXIT

SELLER_JAR="$WORK/seller.jar"
BUYER_JAR="$WORK/buyer.jar"
ADMIN_JAR="$WORK/admin.jar"
BODY_FILE="$WORK/webhook.json"
TAMPERED_FILE="$WORK/webhook-tampered.json"

FAILED=""

# A few values have to be read straight out of the
# database: an id to look something up by, and the stock
# level after the webhook, which is the number the whole
# exercise turns on.
#
# That is done with a small Go helper rather than with
# psql. The PostgreSQL this project runs is the embedded
# server from database/devdb, and that distribution ships
# the server and almost nothing else, so there is no psql
# to call and none can be installed without root. The
# helper uses the same driver and the same DATABASE_URL as
# the application, so the two cannot drift apart.
#
# It is built once here rather than invoked with "go run"
# at every call, which would recompile it a dozen times.
cd "$(dirname "$0")"

go build -o "$WORK/query" ./database/query

# step prints a heading.
step() {
    echo
    echo "--------------------------------------------------------------"
    echo "$1"
    echo "--------------------------------------------------------------"
}

# request runs curl and appends the status code, so every
# call shows both the answer and what HTTP said about it.
request() {
    curl -sS -w '\n-> HTTP %{http_code}\n' "$@"
}

# db_value runs a query and returns only the answer.
db_value() {
    "$WORK/query" "$1"
}

# check compares a value with what it should be, and
# records a failure rather than stopping, so one wrong
# number does not hide the rest of the run.
check() {
    if [ "$2" = "$3" ]; then
        echo "  ok    $1 = $2"
    else
        echo "  FAIL  $1 = $2  (expected $3)"
        FAILED=1
    fi
}

step "0. Is the server answering?"

request "$BASE/health"

step "1. Register Ada as a seller"

request -X POST "$BASE/register" \
    -H 'Content-Type: application/json' \
    -d "{
        \"name\": \"Ada\",
        \"email\": \"ada$RUN@example.com\",
        \"password\": \"password123\",
        \"role\": \"seller\"
    }"

step "2. Sign Ada in"

echo "Registration deliberately does not start a session, so this is a"
echo "separate call. The cookie lands in a jar file, which is what every"
echo "later call passes back with -b."

request -c "$SELLER_JAR" -X POST "$BASE/login" \
    -H 'Content-Type: application/json' \
    -d "{
        \"email\": \"ada$RUN@example.com\",
        \"password\": \"password123\"
    }"

step "3. Ada lists a product"

CATEGORY_ID=$(db_value \
    "SELECT id FROM categories WHERE slug = 'home-kitchen'")

echo "The categories come from migration 013. 'home-kitchen' is id $CATEGORY_ID."
echo

request -b "$SELLER_JAR" -X POST "$BASE/products" \
    -H 'Content-Type: application/json' \
    -d "{
        \"category_id\": $CATEGORY_ID,
        \"name\": \"Cast Iron Pot $RUN\",
        \"description\": \"A heavy pot that will outlive its owner.\",
        \"price\": 18500.00,
        \"currency\": \"NGN\"
    }"

PRODUCT_ID=$(db_value \
    "SELECT id FROM products WHERE name = 'Cast Iron Pot $RUN'")

echo
echo "product id: $PRODUCT_ID"

step "4. Ten units arrive"

echo "There is no stock column to set. This appends a movement to the"
echo "ledger, and the stock level is the sum of the ledger."

request -b "$SELLER_JAR" -X POST "$BASE/products/$PRODUCT_ID/restock" \
    -H 'Content-Type: application/json' \
    -d '{"quantity": 10}'

step "5. Register Chidi as a buyer"

request -X POST "$BASE/register" \
    -H 'Content-Type: application/json' \
    -d "{
        \"name\": \"Chidi\",
        \"email\": \"chidi$RUN@example.com\",
        \"password\": \"password123\"
    }"

request -c "$BUYER_JAR" -X POST "$BASE/login" \
    -H 'Content-Type: application/json' \
    -d "{
        \"email\": \"chidi$RUN@example.com\",
        \"password\": \"password123\"
    }"

BUYER_ID=$(db_value \
    "SELECT id FROM users WHERE email = 'chidi$RUN@example.com'")

step "6. Chidi puts two pots in the cart"

request -b "$BUYER_JAR" -X POST "$BASE/cart/items" \
    -H 'Content-Type: application/json' \
    -d "{
        \"product_id\": $PRODUCT_ID,
        \"quantity\": 2
    }"

step "7. An admin appears, and mints a coupon"

echo "Registration can only ever produce a buyer or a seller, which is"
echo "deliberate: admin rights must be granted by an existing admin."
echo "There is no first admin in a fresh database, so one is promoted"
echo "here directly. That bootstrap step is a real gap in the API and"
echo "is noted in the README."
echo

request -X POST "$BASE/register" \
    -H 'Content-Type: application/json' \
    -d "{
        \"name\": \"Root\",
        \"email\": \"root$RUN@example.com\",
        \"password\": \"password123\"
    }"

db_value \
    "UPDATE users SET role = 'admin' WHERE email = 'root$RUN@example.com'" \
    > /dev/null

echo "promoted root$RUN@example.com to admin"

request -c "$ADMIN_JAR" -X POST "$BASE/login" \
    -H 'Content-Type: application/json' \
    -d "{
        \"email\": \"root$RUN@example.com\",
        \"password\": \"password123\"
    }"

COUPON_CODE="SAVE$RUN"

request -b "$ADMIN_JAR" -X POST "$BASE/coupons" \
    -H 'Content-Type: application/json' \
    -d "{
        \"code\": \"$COUPON_CODE\",
        \"discount_type\": \"percentage\",
        \"discount_value\": 10,
        \"min_order_amount\": 0
    }"

step "8. What would the coupon take off?"

echo "The subtotal is read from Chidi's own cart, so a client cannot"
echo "claim a larger one to unlock a discount it has not earned."

request -b "$BUYER_JAR" -X POST "$BASE/coupons/validate" \
    -H 'Content-Type: application/json' \
    -d "{\"code\": \"$COUPON_CODE\"}"

step "9. Chidi places the order"

echo "Two pots at 18500.00 is a subtotal of 37000.00. Ten percent off"
echo "is 3700.00, leaving 33300.00 to pay."
echo
echo "Nothing is taken from stock here. The order is pending, and stock"
echo "only moves when payment is confirmed."

request -b "$BUYER_JAR" -X POST "$BASE/orders" \
    -H 'Content-Type: application/json' \
    -d "{\"coupon_code\": \"$COUPON_CODE\"}"

ORDER_ID=$(db_value \
    "SELECT id FROM orders WHERE buyer_id = $BUYER_ID ORDER BY id DESC LIMIT 1")

echo
echo "order id: $ORDER_ID"

step "10. Chidi starts the payment"

echo "POST /orders/$ORDER_ID/pay is the real endpoint, and it calls"
echo "Paystack for an authorization URL. With a made-up secret key"
echo "Paystack refuses it, which is the correct outcome and is shown"
echo "here rather than hidden: the integration is real, the"
echo "credentials are not."
echo

request -b "$BUYER_JAR" -X POST "$BASE/orders/$ORDER_ID/pay"

# A real payment would have stored the reference Paystack returned.
# There is no real payment here, so the step that would have stored
# it is done directly. Everything after this point is unaffected:
# the webhook only needs the order to carry a reference, not to care
# how it got one.
REFERENCE="ECB-walkthrough-$RUN-$ORDER_ID"

db_value \
    "UPDATE orders SET payment_reference = '$REFERENCE' WHERE id = $ORDER_ID" \
    > /dev/null

# The amount the webhook must quote, taken from the order itself in
# kobo, which is the unit Paystack counts in. Multiplying by 100 in
# SQL keeps it exact, where a float would not.
AMOUNT_KOBO=$(db_value \
    "SELECT (total * 100)::bigint FROM orders WHERE id = $ORDER_ID")

EVENT_ID=$RUN

# The body is written once to a file. Every later delivery posts
# those exact bytes, which is what makes the signature reproducible:
# Paystack signs the raw body, not a re-encoding of it.
printf '%s' \
    "{\"event\":\"charge.success\",\"data\":{\"id\":$EVENT_ID,\"reference\":\"$REFERENCE\",\"amount\":$AMOUNT_KOBO,\"status\":\"success\"}}" \
    > "$BODY_FILE"

SIGNATURE=$(openssl dgst -sha512 -hmac "$SECRET" "$BODY_FILE" | awk '{print $NF}')

echo
echo "order total in kobo: $AMOUNT_KOBO"
echo "reference:           $REFERENCE"
echo "signature:           $SIGNATURE"
echo
echo "That signature is the same HMAC-SHA512 the server computes over"
echo "the same bytes with the same secret."

step "11. A webhook with a bad signature"

echo "Anyone who finds this URL can post to it, since Paystack has no"
echo "cookie. The signature is what stands in for authentication, and"
echo "this must be refused before anything is parsed."

request -X POST "$BASE/webhooks/paystack" \
    -H 'x-paystack-signature: 0000000000000000' \
    -H 'Content-Type: application/json' \
    --data-binary "@$BODY_FILE"

step "12. A webhook claiming a smaller payment"

echo "One kobo instead of $AMOUNT_KOBO. The signature on this one is"
echo "genuine, so it passes the first check and is caught by the amount"
echo "check instead. The answer is 500 rather than 200, so Paystack"
echo "retries and somebody can look into it. Crucially, the order must"
echo "not move: an underpaid order that shipped would be a real loss."

printf '%s' \
    "{\"event\":\"charge.success\",\"data\":{\"id\":$((EVENT_ID + 1)),\"reference\":\"$REFERENCE\",\"amount\":1,\"status\":\"success\"}}" \
    > "$TAMPERED_FILE"

TAMPERED_SIGNATURE=$(openssl dgst -sha512 -hmac "$SECRET" "$TAMPERED_FILE" | awk '{print $NF}')

request -X POST "$BASE/webhooks/paystack" \
    -H "x-paystack-signature: $TAMPERED_SIGNATURE" \
    -H 'Content-Type: application/json' \
    --data-binary "@$TAMPERED_FILE"

check "order status after the underpayment" \
    "$(db_value "SELECT status FROM orders WHERE id = $ORDER_ID")" \
    "pending"

step "13. The genuine payment notification"

request -X POST "$BASE/webhooks/paystack" \
    -H "x-paystack-signature: $SIGNATURE" \
    -H 'Content-Type: application/json' \
    --data-binary "@$BODY_FILE"

step "14. The very same notification, delivered again"

echo "This is not a hypothetical. Paystack retries until it receives a"
echo "prompt 200, and will keep retrying for days. This is byte for byte"
echo "the same body carrying the same signature and the same event id."

request -X POST "$BASE/webhooks/paystack" \
    -H "x-paystack-signature: $SIGNATURE" \
    -H 'Content-Type: application/json' \
    --data-binary "@$BODY_FILE"

step "15. Did the retry change anything?"

ORDER_STATUS=$(db_value \
    "SELECT status FROM orders WHERE id = $ORDER_ID")

STOCK=$(db_value \
    "SELECT COALESCE(SUM(quantity_change), 0) FROM inventory_movements WHERE product_id = $PRODUCT_ID")

SALE_MOVEMENTS=$(db_value \
    "SELECT COUNT(*) FROM inventory_movements WHERE order_id = $ORDER_ID AND reason = 'sale'")

PAID_EVENTS=$(db_value \
    "SELECT COUNT(*) FROM order_events WHERE order_id = $ORDER_ID AND to_status = 'paid'")

WEBHOOK_ROWS=$(db_value \
    "SELECT COUNT(*) FROM webhook_events WHERE provider_event_id = '$EVENT_ID'")

echo "Ten units arrived and two were sold, so eight should be left."
echo "If the retry had taken stock a second time there would be six."
echo

check "order status" "$ORDER_STATUS" "paid"
check "stock remaining" "$STOCK" "8"
check "sale movements for this order" "$SALE_MOVEMENTS" "1"
check "paid events in the history" "$PAID_EVENTS" "1"
check "webhook event rows" "$WEBHOOK_ROWS" "1"

step "16. Ada ships it"

request -b "$SELLER_JAR" -X POST "$BASE/orders/$ORDER_ID/ship"

step "17. Chidi confirms delivery"

request -b "$BUYER_JAR" -X POST "$BASE/orders/$ORDER_ID/deliver"

step "18. Chidi reviews the pot"

echo "The field names matter. The body takes a rating, a title, and a"
echo "body, and a field the struct does not know about is ignored in"
echo "silence rather than refused, which is simply how Go decodes JSON."

request -b "$BUYER_JAR" -X POST "$BASE/products/$PRODUCT_ID/reviews" \
    -H 'Content-Type: application/json' \
    -d '{
        "rating": 5,
        "title": "Exactly as advertised",
        "body": "Heavy, which is rather the point of it."
    }'

echo "Read back, with the reviewer's name joined in and a rating"
echo "summary alongside."

request "$BASE/products/$PRODUCT_ID/reviews"

step "19. The transitions that must be refused"

echo "A delivered order cannot be shipped again. The state machine"
echo "rejects it in Go with a clear message, and the UPDATE carries a"
echo "status guard so a racing request updates no rows and loses."

request -b "$SELLER_JAR" -X POST "$BASE/orders/$ORDER_ID/ship"

echo "A buyer cannot cancel an order that has already been paid for."
echo "The honest exit from a paid order is a refund, not a cancellation."

request -b "$BUYER_JAR" -X POST "$BASE/orders/$ORDER_ID/cancel"

echo "A buyer cannot refund. That is an admin decision, because it"
echo "returns stock to the ledger as well as money."

request -b "$BUYER_JAR" -X POST "$BASE/orders/$ORDER_ID/refund" \
    -H 'Content-Type: application/json' \
    -d '{"reason": "I changed my mind"}'

echo "And a buyer cannot list products."

request -b "$BUYER_JAR" -X POST "$BASE/products" \
    -H 'Content-Type: application/json' \
    -d "{\"category_id\": $CATEGORY_ID, \"name\": \"Sneaky\", \"price\": 1}"

step "20. The order's history, and the stock ledger"

echo "Both tables are append-only. Nothing is ever updated or deleted,"
echo "so each of these is the complete story rather than a snapshot."

request -b "$BUYER_JAR" "$BASE/orders/$ORDER_ID/events"

request -b "$SELLER_JAR" "$BASE/products/$PRODUCT_ID/inventory"

step "21. The public catalog"

echo "No cookie is sent here. Browsing must work without an account."

request "$BASE/products?q=Cast+Iron&per_page=5"

step "22. A second product, and a second order"

echo "This is here to give the recommendation endpoint something real to"
echo "work with. With a single product in the catalog it can only ever"
echo "return an empty list, which proves nothing at all."

request -b "$SELLER_JAR" -X POST "$BASE/products" \
    -H 'Content-Type: application/json' \
    -d "{
        \"category_id\": $CATEGORY_ID,
        \"name\": \"Cast Iron Lid $RUN\",
        \"description\": \"Goes on top of the pot.\",
        \"price\": 4500.00,
        \"currency\": \"NGN\"
    }"

LID_ID=$(db_value \
    "SELECT id FROM products WHERE name = 'Cast Iron Lid $RUN'")

request -b "$SELLER_JAR" -X POST "$BASE/products/$LID_ID/restock" \
    -H 'Content-Type: application/json' \
    -d '{"quantity": 5}'

echo "Chidi now buys the pot and the lid together. That is what makes"
echo "them frequently bought together rather than merely similar."

request -b "$BUYER_JAR" -X POST "$BASE/cart/items" \
    -H 'Content-Type: application/json' \
    -d "{\"product_id\": $PRODUCT_ID, \"quantity\": 1}"

request -b "$BUYER_JAR" -X POST "$BASE/cart/items" \
    -H 'Content-Type: application/json' \
    -d "{\"product_id\": $LID_ID, \"quantity\": 1}"

request -b "$BUYER_JAR" -X POST "$BASE/orders"

SECOND_ORDER_ID=$(db_value \
    "SELECT id FROM orders WHERE buyer_id = $BUYER_ID ORDER BY id DESC LIMIT 1")

SECOND_REFERENCE="ECB-walkthrough-$RUN-$SECOND_ORDER_ID"

db_value \
    "UPDATE orders SET payment_reference = '$SECOND_REFERENCE' WHERE id = $SECOND_ORDER_ID" \
    > /dev/null

SECOND_AMOUNT_KOBO=$(db_value \
    "SELECT (total * 100)::bigint FROM orders WHERE id = $SECOND_ORDER_ID")

echo
echo "second order total in kobo: $SECOND_AMOUNT_KOBO"

# The event id is stepped past the two already used, so this is a
# genuinely new event rather than a duplicate of an earlier one. The
# body file is reused, which is safe because its last use was the
# replay in step 14.
printf '%s' \
    "{\"event\":\"charge.success\",\"data\":{\"id\":$((EVENT_ID + 2)),\"reference\":\"$SECOND_REFERENCE\",\"amount\":$SECOND_AMOUNT_KOBO,\"status\":\"success\"}}" \
    > "$BODY_FILE"

SECOND_SIGNATURE=$(openssl dgst -sha512 -hmac "$SECRET" "$BODY_FILE" | awk '{print $NF}')

request -X POST "$BASE/webhooks/paystack" \
    -H "x-paystack-signature: $SECOND_SIGNATURE" \
    -H 'Content-Type: application/json' \
    --data-binary "@$BODY_FILE"

step "23. Recommendations for the pot"

echo "The pot and the lid have now been bought together, so the lid is"
echo "recommended. Had nobody ever bought them together this would fall"
echo "back to the best rated products in the same category, and with"
echo "nothing in that category either, to an empty list."

request "$BASE/products/$PRODUCT_ID/recommendations"

step "24. An admin view"

request -b "$ADMIN_JAR" "$BASE/admin/orders?per_page=5"

echo
echo "=============================================================="

if [ -n "$FAILED" ]; then

    echo "SOME CHECKS FAILED. Scroll up for the lines marked FAIL."

    exit 1
fi

echo "Every check passed."

echo "=============================================================="
