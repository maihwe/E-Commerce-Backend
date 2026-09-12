package storage

import (
	"context"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GetFrequentlyBoughtTogetherFromDB finds products that
// were bought alongside a given product.
//
// The query joins order_items to itself on order_id. One
// side is pinned to the product being viewed; the other
// side is every other line on those same orders. Counting
// those rows, grouped by product, ranks what tends to be
// bought with it.
//
// Two details worth noting:
//
//   - COUNT(DISTINCT order_id) means one order counts
//     once no matter how many times a product appears on
//     it, so a single large order cannot dominate.
//
//   - Only active products are suggested, because
//     recommending something a shopper cannot buy would
//     be a poor recommendation.
//
// This is a simple co-occurrence count. It needs no
// machine learning and is honest about what it does: it
// reports what people actually bought together.
func GetFrequentlyBoughtTogetherFromDB(
	pool *pgxpool.Pool,
	productID int,
	limit int,
) ([]models.Product, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT `+productColumns+`
		FROM products p
		JOIN (
			SELECT
				other.product_id AS pid,
				COUNT(DISTINCT other.order_id) AS score
			FROM order_items mine
			JOIN order_items other
				ON other.order_id = mine.order_id
				AND other.product_id <> mine.product_id
			WHERE mine.product_id = $1
			GROUP BY other.product_id
		) AS ranked ON ranked.pid = p.id
		WHERE p.is_active = TRUE
		ORDER BY ranked.score DESC, p.id
		LIMIT $2
		`,
		productID,
		limit,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	products := make([]models.Product, 0)

	for rows.Next() {

		product, err := scanProduct(rows)

		if err != nil {
			return nil, err
		}

		products = append(products, product)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return products, nil
}

// ListTopRatedInCategoryFromDB returns the best rated
// products in a category.
//
// This is the fallback for a product that has never been
// bought, so there is no purchase history to learn from.
// Suggesting the best rated items in the same category is
// a reasonable answer, and it is more useful than
// returning an empty list.
func ListTopRatedInCategoryFromDB(
	pool *pgxpool.Pool,
	categoryID int,
	excludeProductID int,
	limit int,
) ([]models.Product, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT `+productColumns+`
		FROM products p
		WHERE p.is_active = TRUE
		AND p.category_id = $1
		AND p.id <> $2
		ORDER BY
			average_rating DESC,
			review_count DESC,
			p.id
		LIMIT $3
		`,
		categoryID,
		excludeProductID,
		limit,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	products := make([]models.Product, 0)

	for rows.Next() {

		product, err := scanProduct(rows)

		if err != nil {
			return nil, err
		}

		products = append(products, product)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return products, nil
}

// GetRecommendationsFromDB returns products to suggest
// alongside a product.
//
// It tries purchase history first and falls back to the
// category's best rated items, so the endpoint always
// answers with something useful.
func GetRecommendationsFromDB(
	pool *pgxpool.Pool,
	productID int,
	limit int,
) ([]models.Product, error) {

	products, err := GetFrequentlyBoughtTogetherFromDB(
		pool,
		productID,
		limit,
	)

	if err != nil {
		return nil, err
	}

	if len(products) > 0 {
		return products, nil
	}

	// No purchase history to learn from, so fall back to
	// the product's own category.
	product, err := GetProductByIDFromDB(pool, productID)

	if err != nil {
		return nil, err
	}

	return ListTopRatedInCategoryFromDB(
		pool,
		product.CategoryID,
		productID,
		limit,
	)
}
