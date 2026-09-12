package storage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the two statements the picture scanner
// needs, and nothing else.
//
// They live apart from the rest of the product queries
// because they are the only ones that write image_path,
// and image_path is the only product column no endpoint
// can set. Keeping them in one small file is what makes
// that easy to check: a reader who wants to know every
// way a picture can be attached reads these fifty lines
// rather than searching five hundred.

// SetProductImageInDB points a product at a picture.
//
// It returns whether anything changed. Attaching the
// picture a product already has is not an error and does
// nothing at all, which is what makes the scanner safe
// to run on every start: a folder nobody has touched
// produces no writes and no churn in updated_at.
//
// The guard is written into the statement rather than
// left to the caller, so "the same picture twice is a
// no-op" is a property of this function instead of a
// habit the caller has to remember. IS DISTINCT FROM is
// used rather than <> because it is the comparison that
// treats NULL as an ordinary value, and a column added
// by ALTER TABLE is exactly where a stray NULL would
// hide.
//
// The path is passed as a placeholder and never pasted
// into the statement, the same way every other value in
// this package is.
func SetProductImageInDB(
	pool *pgxpool.Pool,
	productID int,
	imagePath string,
) (bool, error) {

	tag, err := pool.Exec(
		context.Background(),
		`
		UPDATE products
		SET
			image_path = $1,
			updated_at = NOW()
		WHERE id = $2
		AND image_path IS DISTINCT FROM $1
		`,
		imagePath,
		productID,
	)

	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil
}

// ListProductImagesFromDB reads which products have a
// picture, and where each one points.
//
// The result is a map from product id to path rather
// than a slice of structs, because that is what the
// caller does with it: for each path it has to answer
// "is this product's picture still on disk", and a map
// answers that without a second loop. A struct would
// mean inventing a type to hold two fields that are
// never read apart from each other.
//
// Only rows with a path are returned. A product with no
// picture has nothing that could have gone missing, so
// there is nothing for the caller to check.
func ListProductImagesFromDB(
	pool *pgxpool.Pool,
) (map[int]string, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT id, image_path
		FROM products
		WHERE image_path <> ''
		`,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	images := make(map[int]string)

	for rows.Next() {

		var id int

		var path string

		err := rows.Scan(&id, &path)

		if err != nil {
			return nil, err
		}

		images[id] = path
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return images, nil
}
