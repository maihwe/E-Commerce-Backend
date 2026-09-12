# Product pictures

Drop a photograph in this folder and it becomes that
product's picture.

Nothing here is committed. This folder is listed in
`.gitignore`, apart from this file, because everything
that lands in it is somebody's own photograph.

## The rule

**Name the file after the product's slug.**

    cast-iron-pot.jpg   ->  the product whose slug is cast-iron-pot

The slug is the name a product already appears under in
every URL, so `Mart` at `/shop/products/12` shows you
nothing about it, but `/products?q=pot` and the product's
own page both print it.

To see every slug in the shop:

    go run ./database/query "SELECT slug FROM products"

A file named `Cast Iron Pot.JPG` works too. The name is
reduced the same way a product's own name was reduced to
make its slug, so the capitals, the spaces and the
extension do not matter. What matters is the words.

Only the last extension is dropped, so `pot.tar.gz` is
looking for a product called "pot-tar".

## What it does on startup

The server reads this folder once, when it starts, and
says what it found:

    pictures: 3 attached, 12 already in place, 0 removed, 1 unmatched, 0 clashing

Anything that matched nothing is printed by name, along
with the command above. That line is the only feedback
you get, because a picture that belongs to no product is
not an error: it is a file nothing ever referred to, and
without that line it would just never appear.

Adding a picture means restarting the server. Deleting
one means deleting the file and restarting: the product
stops pointing at a picture that is not there.

## Sorted into a category

Adding a picture does a second thing besides attaching
it. The picture is looked at, and the product is moved
into the category the picture suggests.

Only the pictures that were **just added** are looked
at. A picture that is already in place is counted under
"already in place" and nothing is sent anywhere, so
restarting the server costs nothing and there is no
record of what has been done because none is needed.

It needs a key:

    ANTHROPIC_API_KEY=sk-ant-...

Without one, the pictures are still attached and still
shown, and every product keeps the category its seller
gave it. Nothing else in the shop is affected either
way.

The answer is checked against the shop's own list of
categories before anything is written. A model that
names something else — or explains itself at length
instead of answering — is ignored, and the product stays
where it was. So the worst a wrong answer can do is put
a product under the wrong one of your categories; it
cannot invent a category.

It says what it did:

    pictures: 2 sorted into a category, 0 unclear, 0 failed, 0 not looked at
    pictures: sorted cast-iron-pot -> home-kitchen
    pictures: sorted desk-lamp was already home-kitchen

`unclear` means the picture matched none of the
categories. `failed` means the picture could not be read
or the call did not go through, and prints why.

One format is left out. The model is sent JPEG, PNG, GIF
and WebP, so an **AVIF** picture is attached and shown
and is not sorted. It is named in the report so that the
gap is visible rather than mysterious.

## Before you copy them in

Two things are worth doing to a photograph off a phone
or a camera, and both are one command.

**Make them smaller.** A product tile is about 200
pixels wide. A 4000 pixel photograph is four hundred
times the pixels it will ever be shown at, and a shopper
on a phone pays for every one of them.

    mogrify -resize 1200x1200\> *.jpg

**Strip the metadata.** A photograph carries more than
the picture. It carries the camera, the time, and very
often the coordinates of where it was taken, which for a
photograph of your own stock is your own address.

    mogrify -strip *.jpg

`mogrify` edits the files in place, so run it on a copy
if that makes you nervous. Both lines are long enough to
be split by a narrow terminal if you paste them: if that
happens, write the command into a file and run the file.

## Somewhere else

The folder is `pictures` unless `PICTURES_DIR` says
otherwise.

    PICTURES_DIR=/srv/photos
