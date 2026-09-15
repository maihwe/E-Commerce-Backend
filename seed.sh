#!/bin/sh
#
# Creates the shop listings that stock.sh meant to create.
#
# It reads the same table out of stock.sh and makes the same
# API calls, but skips the copy step: stock.sh copies each
# picture out of ~/Downloads into pictures/, and those copies
# are already committed while ~/Downloads no longer holds the
# originals.
#
# The pictures are attached by the server at startup, which
# matches them to products by slug. So: run this, then
# restart the server.
#
# Safe to run twice. A listing that already exists is refused
# by the unique slug and reported as a skip.

BASE=${BASE:-http://localhost:8080}

REPO=${REPO_DIR:-/home/student/E-Commerce-Backend}

JAR=/tmp/ecb-seed.jar

if [ ! -f "$REPO/stock.sh" ]; then
    echo "No stock.sh under $REPO."
    exit 1
fi

# The rows between the <<LIST marker and the closing LIST.
# Read out of stock.sh rather than copied here, so the two
# cannot drift apart.
#
# The marker sits at the end of a line -- "done <<'LIST'" --
# rather than at the start of one, so the pattern matches it
# anywhere on the line.
awk '/<<.LIST./{f=1;next} /^LIST$/{f=0} f' \
    "$REPO/stock.sh" > /tmp/ecb-list.txt

if [ ! -s /tmp/ecb-list.txt ]; then
    echo "Could not read the listing table."
    exit 1
fi

if ! curl -sSf -m 5 "$BASE/health" > /dev/null; then
    echo "The server is not answering on $BASE."
    exit 1
fi

# The body is built by jq rather than by interpolating into a
# string: half these names carry apostrophes, ampersands and
# emoji, and a quoting mistake would quietly create a listing
# with a mangled name rather than fail.
LOGIN=$(jq -nc \
    --arg email stockroom@example.com \
    --arg password password123 \
    '{email:$email,password:$password}')

curl -sS -o /dev/null -c "$JAR" -X POST "$BASE/login" \
    -H 'Content-Type: application/json' \
    -d "$LOGIN"

# curl succeeds on a 401, so the session is proved by asking
# who we are before any listing is attempted. Without this a
# failed sign in would look exactly like a run that created
# nothing.
if ! curl -sS -b "$JAR" "$BASE/me" \
    | grep -q 'stockroom@example.com'; then

    echo "The seller sign in did not take. Nothing created."
    exit 1
fi

CREATED=0
SKIPPED=0

while IFS='|' read -r FILE CAT NAME PRICE MATCH; do

    [ -z "$FILE" ] && continue

    # Same reduction the server applies to a name to make its
    # slug, so this agrees with the filenames on disk.
    SLUG=$(printf '%s' "$NAME" | tr 'A-Z' 'a-z' \
        | sed 's/[^a-z0-9][^a-z0-9]*/-/g; s/^-//; s/-$//')

    if [ -z "$(ls "$REPO/pictures/$SLUG".* 2>/dev/null)" ]; then
        echo "  no picture  $SLUG"
        continue
    fi

    BODY=$(jq -nc \
        --arg name "$NAME" \
        --argjson category "$CAT" \
        --argjson price "$PRICE" \
        '{category_id:$category,name:$name,price:$price,currency:"NGN"}')

    ID=$(curl -sS -b "$JAR" -X POST "$BASE/products" \
        -H 'Content-Type: application/json' \
        -d "$BODY" < /dev/null \
        | jq -r '.id // empty')

    if [ -z "$ID" ]; then
        echo "  already     $SLUG"
        SKIPPED=$((SKIPPED + 1))
        continue
    fi

    # Ten of each, so a listing somebody clicks is not
    # immediately sold out. Stock is a ledger, so this is a
    # movement rather than a number being set.
    curl -sS -o /dev/null -b "$JAR" -X POST \
        "$BASE/products/$ID/restock" \
        -H 'Content-Type: application/json' \
        -d '{"quantity":10}' < /dev/null

    echo "  created     $SLUG"

    CREATED=$((CREATED + 1))

done < /tmp/ecb-list.txt

echo
echo "$CREATED created, $SKIPPED already there"
echo
echo "Now restart the server to attach the pictures."
