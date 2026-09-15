#!/bin/sh
#
# Fills the shop with the demo pictures sitting in ~/Downloads.
#
# This is demo content, not stock. The pictures are wallpapers,
# nature photography and quote images rather than photographs of
# things for sale, and they all go in Everything Else because
# that is what that category is: the seeded bucket whose whole
# meaning is "not sorted yet". When the shop is filled for real,
# delete these listings and their files.
#
# It follows the shape of stock.sh -- same table, same API calls,
# same naming rule -- with one difference that matters. stock.sh
# copies the picture into pictures/ before it knows whether the
# listing was created, so a run whose sign in failed leaves a
# folder full of correctly named pictures attached to nothing.
# Here the copy happens after the listing exists, so a failure
# stops rather than leaving debris behind.
#
# Safe to run twice: a listing that already exists is refused by
# its unique slug and reported as a skip.

BASE=${BASE:-http://localhost:8080}

REPO=${REPO_DIR:-/home/student/E-Commerce-Backend}

PICS=$REPO/pictures

DOWNLOADS=${DOWNLOADS:-$HOME/Downloads}

JAR=/tmp/ecb-demo.jar

# Everything Else. See the note above for why.
EVERYTHING_ELSE=10

if [ ! -d "$DOWNLOADS" ]; then
    echo "No $DOWNLOADS."
    exit 1
fi

if [ ! -d "$PICS" ]; then
    echo "No $PICS. Set REPO_DIR to the repository."
    exit 1
fi

if ! curl -sSf -m 5 "$BASE/health" > /dev/null; then
    echo "The server is not answering on $BASE."
    exit 1
fi

curl -sS -o /dev/null -X POST "$BASE/register" \
    -H 'Content-Type: application/json' \
    -d '{"name":"Stockroom","email":"stockroom@example.com","password":"password123","role":"seller"}' \
    2>/dev/null || true

LOGIN=$(jq -nc \
    --arg email stockroom@example.com \
    --arg password password123 \
    '{email:$email,password:$password}')

curl -sS -o /dev/null -c "$JAR" -X POST "$BASE/login" \
    -H 'Content-Type: application/json' \
    -d "$LOGIN"

# curl succeeds on a 401, so the session is proved before any
# listing is attempted. Without this, a failed sign in would look
# exactly like a run that created nothing.
if ! curl -sS -b "$JAR" "$BASE/me" \
    | grep -q 'stockroom@example.com'; then

    echo "The seller sign in did not take. Nothing was created."
    exit 1
fi

CREATED=0
SKIPPED=0
MISSING=0

echo

while IFS='|' read -r FILE CAT NAME PRICE MATCH; do

    [ -z "$FILE$MATCH" ] && continue

    SOURCE=""

    if [ -n "$FILE" ] && [ -f "$DOWNLOADS/$FILE" ]; then
        SOURCE=$DOWNLOADS/$FILE
    fi

    # A name too long or too awkward to write out exactly -- an
    # emoji, a curly apostrophe -- is found by a distinctive
    # fragment instead. Same idea as stock.sh's fifth column.
    if [ -z "$SOURCE" ] && [ -n "$MATCH" ]; then

        for FOUND in "$DOWNLOADS"/*"$MATCH"*; do

            if [ -f "$FOUND" ]; then
                SOURCE=$FOUND
                break
            fi
        done
    fi

    if [ -z "$SOURCE" ]; then
        echo "  missing  $NAME"
        MISSING=$((MISSING + 1))
        continue
    fi

    SLUG=$(printf '%s' "$NAME" \
        | tr 'A-Z' 'a-z' \
        | sed 's/[^a-z0-9][^a-z0-9]*/-/g; s/^-//; s/-$//')

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
        echo "  already  $SLUG"
        SKIPPED=$((SKIPPED + 1))
        continue
    fi

    # Only reached once the listing exists. The extension is kept
    # only when it is one the scanner accepts, so a file with no
    # suffix is stored as a JPEG rather than under a name made of
    # whatever followed the last dot.
    LOWER=$(printf '%s' "$SOURCE" | tr 'A-Z' 'a-z')

    case $LOWER in
    *.jpg) EXT=jpg ;;
    *.png) EXT=png ;;
    *.webp) EXT=webp ;;
    *.gif) EXT=gif ;;
    *) EXT=jpeg ;;
    esac

    cp "$SOURCE" "$PICS/$SLUG.$EXT"

    curl -sS -o /dev/null -b "$JAR" -X POST \
        "$BASE/products/$ID/restock" \
        -H 'Content-Type: application/json' \
        -d '{"quantity":10}' < /dev/null

    echo "  created  $SLUG.$EXT"

    CREATED=$((CREATED + 1))

done <<'LIST'
296182113011441412.jpeg|10|Purple Blossom Sunset Print|18500.00
3518505955497549.jpeg|10|Violet Planet Landscape Print|19500.00
42080577763158520.jpeg|10|Be Yourself Quote Print|12500.00
64k Ultra Hd Wallpaper For Mobile.jpeg|10|Ultra HD Mobile Wallpaper|9500.00
668080926020184889.jpeg|10|Anything Else Text Print|8500.00
Beautiful Pinterest Background.jpeg|10|Beautiful Pinterest Background|11500.00
Beautiful Views Nature.jpeg|10|Beautiful Nature Views|16500.00
Beautiful Village Photography.jpeg|10|Village Life Photograph|17500.00
Be Different.jpeg|10|Be Different Print|12500.00
Bright Exotic Bird In A Tropical Garden Sunlight Generative Ai Photo _ JPG Free Download - Pikbest.jpeg|10|Tropical Garden Bird Print|15500.00
Dreamy Japan Travel.jpeg|10|Dreamy Japan Travel Print|14500.00
eee.jpeg|10|Jeweled Letter E Print|10500.00
Flowers Nature Wallpaper.jpeg|10|Flowers Nature Wallpaper|11500.00
Ig Story ideas.jpeg|10|Instagram Story Ideas Print|9500.00
nature.jpeg|10|Green Meadow Tree Print|13500.00
|10|Self Love Quote Print|12500.00|One Day
Pinterest Images For Wallpaper.jpeg|10|Pinterest Wallpaper Collection|10500.00
Rainy Photography Nature.jpeg|10|Rainy Nature Photograph|16500.00
Saved Images.jpeg|10|Saved Images Collection|9500.00
|10|Cute Energy Print|11500.00|This App
LIST

echo
echo "$CREATED created, $SKIPPED already there, $MISSING not in ~/Downloads"
echo
echo "Restart the server to attach the pictures."
