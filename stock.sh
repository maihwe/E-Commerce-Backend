#!/bin/sh
#
# Lists the pictures in ~/Downloads as shop listings.
#
# One product per picture, in the category the picture
# belongs to, and then the picture is copied into
# pictures/ under the product's own slug -- which is the
# name the scanner matches on.
#
# Nothing in ~/Downloads is moved or deleted.
#
#     . ~/ecb-env.sh
#     sh stock.sh
#
# Then restart the server, which is when the pictures are
# read and attached.
#
# Running it twice is harmless. A listing that already
# exists is refused by the unique slug, and a refusal here
# means "already done" rather than "broken", so it is
# reported as a skip and the run carries on.

set -e

BASE=${BASE:-http://localhost:8080}

JAR=/tmp/ecb-stock.jar

REPO=$(cd "$(dirname "$0")" && pwd)

PICS=$REPO/pictures

DOWNLOADS=$HOME/Downloads

# Nothing here works with the application stopped, and a
# run against a dead server would copy every picture in
# while listing nothing at all -- a folder of files
# matching no product. So the door is tried first, before
# anything on disk is touched.
if ! curl -sSf -m 5 "$BASE/health" > /dev/null; then

    echo "The server is not answering on $BASE."

    echo "Start it, then run this again."

    exit 1
fi

# The seller the listings are credited to. Registering an
# account that already exists is refused, which is the
# expected answer on every run after the first, so the
# refusal is ignored and the login below is what matters.
curl -sS -o /dev/null -X POST "$BASE/register" \
    -H 'Content-Type: application/json' \
    -d '{"name":"Stockroom","email":"stockroom@example.com","password":"password123","role":"seller"}' \
    || true

curl -sS -o /dev/null -c "$JAR" -X POST "$BASE/login" \
    -H 'Content-Type: application/json' \
    -d '{"email":"stockroom@example.com","password":"password123"}'

# file | category id | name | price | word to look for
#
# The fifth column is optional and only three rows have
# it. Those three names were long enough that the terminal
# wrapped them, and what came back had characters swapped
# and the end missing. Rather than guess the exact string,
# a word from the name is given instead: if the file named
# in column one is not there, the first file containing that
# word is used.
#
# The ids are from migration 013: 1 phones-tablets, 2
# computers, 3 electronics, 4 home-kitchen, 5 fashion,
# 6 health-beauty, 7 books-stationery, 8 groceries,
# 9 sports-outdoors.
#
# Every name was read off the filename, which is where the
# picture came from and what it calls the thing it shows.
# That is a guess about the contents of a photograph made
# without looking at it, and a few of these are the kind
# of guess worth correcting by hand: the two from Ghana
# and the shopping illustration are filed under groceries
# on the strength of a word each, and the Ukrainian one is
# filed as a suit because that is what the word means.
#
# Every price is a guess as well. They are plausible Naira
# figures for that kind of object and nothing more.
while IFS='|' read -r FILE CAT NAME PRICE MATCH; do

    [ -z "$FILE" ] && continue

    SOURCE=$DOWNLOADS/$FILE

    if [ ! -f "$SOURCE" ] && [ -n "$MATCH" ]; then

        for FOUND in "$DOWNLOADS"/*"$MATCH"*; do

            if [ -f "$FOUND" ]; then

                SOURCE=$FOUND

                break
            fi
        done
    fi

    if [ ! -f "$SOURCE" ]; then

        echo "  not in ~/Downloads: $FILE"

        continue
    fi

    # The body is built by jq rather than by interpolating
    # into a string, because half these names carry
    # apostrophes, ampersands, accents and emoji. A quoting
    # mistake here would not fail -- it would quietly create
    # a listing with a mangled name.
    RESPONSE=$(curl -sS -b "$JAR" -X POST "$BASE/products" \
        -H 'Content-Type: application/json' \
        -d "$(jq -nc \
            --arg name "$NAME" \
            --argjson category "$CAT" \
            --argjson price "$PRICE" \
            '{category_id:$category,name:$name,price:$price,currency:"NGN"}')")

    ID=$(printf '%s' "$RESPONSE" | jq -r '.id // empty')

    # The slug is normally read back from the server rather
    # than worked out here, because utils.Slugify keeps
    # unicode letters and a name in Cyrillic would produce a
    # slug no shell expression would have guessed.
    #
    # Every name in the table below is plain English, so
    # when there is no reply to read -- the listing was
    # already there and the server refused it -- the slug is
    # worked out here instead. That is what makes a second
    # run finish a first one: the picture is copied either
    # way, so a run that stopped halfway is repaired by
    # running it again rather than by deleting anything.
    SLUG=$(printf '%s' "$RESPONSE" | jq -r '.slug // empty')

    if [ -z "$SLUG" ]; then

        SLUG=$(printf '%s' "$NAME" |
            tr 'A-Z' 'a-z' |
            sed 's/[^a-z0-9][^a-z0-9]*/-/g; s/^-//; s/-$//')
    fi

    # A picture can arrive with no extension at all, and one
    # of these did. Taking whatever follows the last dot
    # would land inside the name -- "1122.333" is a dot --
    # and file the picture under something the scanner would
    # never look at. So only a known picture extension is
    # kept, and anything else is stored as a JPEG, which is
    # what a screenshot or a phone picture with no suffix
    # almost always is.
    LOWER=$(printf '%s' "$SOURCE" | tr 'A-Z' 'a-z')

    case $LOWER in
    *.jpg|*.jpeg|*.png|*.webp|*.gif|*.avif)
        EXT=${LOWER##*.}
        ;;
    *)
        EXT=jpeg
        ;;
    esac

    cp "$SOURCE" "$PICS/$SLUG.$EXT"

    # Ten of each, so a listing somebody clicks is not
    # immediately sold out. Stock is a ledger and this is a
    # real movement against it, not a number being set.
    #
    # Only for a listing that was just created. One that was
    # already there was restocked by whichever run created
    # it, and restocking on every run would inflate the
    # stock without a delivery ever having happened.
    if [ -n "$ID" ]; then

        curl -sS -o /dev/null -b "$JAR" -X POST \
            "$BASE/products/$ID/restock" \
            -H 'Content-Type: application/json' \
            -d '{"quantity":10}'
    fi

    echo "  $NAME -> $SLUG.$EXT"

done <<'LIST'
Apple iPhone 14.jpeg|1|Apple iPhone 14|1250000.00
Apple Watch.jpeg|1|Apple Watch|385000.00
Apple Watch Series 10.jpeg|1|Apple Watch Series 10|620000.00
Apple's 2020 iPad Air is giving the iPad Pro a run for its money.jpeg|1|Apple iPad Air 2020|540000.00
Wristcam, Smart Dual-Camera Band for Apple Watch….jpeg|1|Wristcam Smart Camera Band|180000.00
Lenovo ThinkVision T24t-20 23_8_ Full HD 10-Point Touch USB-C Business Monitor _ 62C5GAT1UK.jpeg|2|Lenovo ThinkVision T24t-20 Monitor|385000.00
Пристрій комп'ютера.jpeg|2|Desktop Computer|295000.00
Beats Solo 4 – Wireless Bluetooth On-Ear Headphones, Apple & Android Compatible, Up to 50 Hours of Battery Life – Matte Black.jpeg|3|Beats Solo 4 Wireless Headphones|215000.00
Amazon_com_ MOVSSOU E7 Active Noise Cancelling….jpeg|3|MOVSSOU E7 Noise Cancelling Headphones|95000.00
Bluetooth Speaker Gifts_ Portable Wireless, IPX5 Waterproof.jpeg|3|Portable Bluetooth Speaker|28000.00
Amazon_com_ [2 Pack] INIU Portable Charger, 20W PD….jpeg|3|INIU 20W Portable Charger 2 Pack|32000.00
Portable Charger, 42800mAh Power Bank Built-in Cable, 22_5W PD USB C In & Out Fast Charging.jpeg|3|42800mAh Power Bank|45000.00
🎧 Fone de Ouvido Headphone Gamer Bluetooth.jpeg|3|Bluetooth Gaming Headset|38000.00
Dyon Movie Smart 32 Vx 32 Zoll 80cm Smart Tv Fernseher Wlan Hd Triple Tuner Neu.jpeg|3|32 Inch Smart TV|260000.00
Home Appliance Washing Machine Refrigerator PNG.jpeg|4|Washing Machine and Refrigerator|480000.00
2 Stücke_Set Teenager Jungen Streetwear Lässig Kurzarm T-Shirt und Shorts, geeignet für stilvolle Jungen.jpeg|5|Teen Boys T-Shirt and Shorts Set|18500.00
African Suits for Men Single Breasted Blazer and….jpeg|5|Men's Single Breasted Blazer|65000.00
Amazon_com_ Wine Red Mermaid Prom Dresses 2026….jpeg|5|Wine Red Mermaid Prom Dress|85000.00
Darlene Crepe Super Wide Leg Pant 33″ _ Fashion Nova.jpeg|5|Darlene Crepe Wide Leg Pant|32000.00
Emilia _ Damen Geblümtes Plissee-Neckholder Midi-Kleid - Orange _ S.jpeg|5|Floral Plisse Midi Dress|42000.00
GlowEve Calça Longa Feminina de Cor Sólida Casual Versátil para Uso Diário.jpeg|5|GlowEve Long Leg Trousers|28000.00
Old Money Streetwear Outfit for Men _ Minimalist Look.jpeg|5|Men's Minimalist Streetwear Outfit|45000.00
SHOP THIS SET 👇_Olive Green Ribbed Henley + Wide Pants Set _ Casual Athleisure Look.jpeg|5|Olive Green Ribbed Henley and Wide Pants Set|38000.00
Xpluswear Design Plus Size Formal Gold Oblique Collar One Shoulder Long Sleeve Ruffle Elegant Asymmetric Hem Irregular Hem Maxi Dresses.jpeg|5|Gold One Shoulder Maxi Dress|72000.00
❤️ЦІНА ЗНИЖЕНА❤️_Костюм_Мод_ 1122.333_Тканина….jpeg|5|Women's Tailored Two Piece Suit|48000.00|1122.333
Beauty & Personal Care - skin moisturizer.jpeg|6|Skin Moisturiser|9500.00
360 Seiten Premium-Lederschnalle, Weiches Leder A5….jpeg|7|A5 Leather Notebook|12500.00
6 blocchi notes A5 a spirale con citazioni ispiratrici - 60 pagine a righe ciascuno, in una varietà di colori pastello (verde menta, giallo, viola, rosa, beige, azzurro chiaro) - carta di alta qualità….jpeg|7|A5 Spiral Notebooks 6 Pack|14500.00
Amazon_com _ EUSOAR Hardcover Spiral Lined Subject….jpeg|7|EUSOAR Hardcover Spiral Notebook|8500.00
Book Annotation Kit_ Book Review, Tabs, Highlighter, Stickers.jpeg|7|Book Annotation Kit|11500.00
Seajan 20 Pack Composition Notebooks, Wide Ruled Paper, 7-1_2_ x 9-3_4_ Marble Hard Covers, 60 Sheets, Composition Notebooks Bulk for Students to.jpeg|7|Composition Notebooks 20 Pack|22000.00|Seajan
SURARD NOTEBOOK Flexible Business 4 Supplies.jpeg|7|SURARD Flexible Notebook|7500.00
Daily Provisions in Ghana.jpeg|8|Daily Provisions Grocery Pack|18000.00
Shopping And Buying Products At Grocery Store, Supermarket, Store, Shop PNG Transparent Image and Clipart for Free Download.jpeg|8|Grocery Shopping Basket|12000.00
Essenclo Men's Gym Clothes Set - 4-Pcs Athletic Outfits w_ 2 Workout Shirts & 2 Shorts - Lightweight, Quick-Dry, Breathable.jpeg|9|Essenclo 4 Piece Gym Set|34000.00|Essenclo
Boyfriend Style Men Quick-Dry Short-Sleeved T-Shirt And Shorts Summer Fitness Soccer Training Clothes Sportswear Equip Boyfriend Style Ment Set Workout Sets Boyfriend Style Men Two Pieces Outfits.jpeg|9|Men's Quick-Dry Training Set|26500.00
LIST

echo
echo "Restart the server to attach the pictures."
