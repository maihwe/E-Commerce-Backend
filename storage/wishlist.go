package storage

import (
	"context"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AddWishlistItemInDB saves a product for later.
//
// ON CONFLICT DO NOTHING means saving something that is
// already saved changes nothing, rather than producing an
// error. Pressing the same heart twice should not fail.
func AddWishlistItemInDB(
	pool *pgxpool.Pool,
	userID int,
	productID int,
) error {

	_, err := pool.Exec(
		context.Background(),
		`
		INSERT INTO wishlist_items (user_id, product_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, product_id) DO NOTHING
		`,
		userID,
		productID,
	)

	return err
}

// RemoveWishlistItemInDB takes a product off the list.
//
// It reports whether anything was actually removed, so the
// caller can answer 404 when the item was not there.
func RemoveWishlistItemInDB(
	pool *pgxpool.Pool,
	userID int,
	productID int,
) (bool, error) {

	tag, err := pool.Exec(
		context.Background(),
		`
		DELETE FROM wishlist_items
		WHERE user_id = $1
		AND product_id = $2
		`,
		userID,
		productID,
	)

	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil
}

// ListWishlistFromDB returns everything one person has
// saved, newest first, with the current product details.
//
// The product details are joined in rather than copied, so
// a wishlist always shows today's price and today's stock.
func ListWishlistFromDB(
	pool *pgxpool.Pool,
	userID int,
) ([]models.WishlistItem, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT
			w.id,
			w.user_id,
			w.product_id,
			w.added_at,
			`+productColumns+`
		FROM wishlist_items w
		JOIN products p ON p.id = w.product_id
		WHERE w.user_id = $1
		ORDER BY w.added_at DESC, w.id DESC
		`,
		userID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	items := make([]models.WishlistItem, 0)

	for rows.Next() {

		var item models.WishlistItem

		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ProductID,
			&item.AddedAt,
			&item.Product.ID,
			&item.Product.SellerID,
			&item.Product.CategoryID,
			&item.Product.Name,
			&item.Product.Slug,
			&item.Product.Description,
			&item.Product.Price,
			&item.Product.Currency,
			&item.Product.IsActive,
			&item.Product.StockAvailable,
			&item.Product.AverageRating,
			&item.Product.ReviewCount,
			&item.Product.CreatedAt,
			&item.Product.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
