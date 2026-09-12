package storage

import (
	"context"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FindDeliveredOrderForProductFromDB looks for a delivered
// order in which a buyer received a particular product.
//
// A review is marked as a verified purchase when this
// finds a match, which is how a shopper can tell the
// difference between a review from somebody who actually
// received the item and one from somebody who did not.
func FindDeliveredOrderForProductFromDB(
	pool *pgxpool.Pool,
	userID int,
	productID int,
) (*int, error) {

	var orderID int

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT o.id
		FROM orders o
		JOIN order_items oi ON oi.order_id = o.id
		WHERE o.buyer_id = $1
		AND oi.product_id = $2
		AND o.status = 'delivered'
		ORDER BY o.id DESC
		LIMIT 1
		`,
		userID,
		productID,
	).Scan(&orderID)

	// No delivered order is not an error, it just means
	// this is not a verified purchase.
	if err != nil {

		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	return &orderID, nil
}

// CreateReviewInDB saves a review.
func CreateReviewInDB(
	pool *pgxpool.Pool,
	review models.Review,
) (models.Review, error) {

	err := pool.QueryRow(
		context.Background(),
		`
		INSERT INTO reviews
			(
				product_id,
				user_id,
				order_id,
				rating,
				title,
				body,
				verified_purchase
			)
		VALUES
			($1, $2, $3, $4, $5, $6, $7)
		RETURNING
			id,
			product_id,
			user_id,
			order_id,
			rating,
			title,
			body,
			verified_purchase,
			created_at,
			updated_at
		`,
		review.ProductID,
		review.UserID,
		review.OrderID,
		review.Rating,
		review.Title,
		review.Body,
		review.VerifiedPurchase,
	).Scan(
		&review.ID,
		&review.ProductID,
		&review.UserID,
		&review.OrderID,
		&review.Rating,
		&review.Title,
		&review.Body,
		&review.VerifiedPurchase,
		&review.CreatedAt,
		&review.UpdatedAt,
	)

	if err != nil {
		return models.Review{}, err
	}

	return review, nil
}

// ListReviewsFromDB returns a page of reviews for one
// product, newest first.
//
// It returns the reviews and the total number that exist,
// so the caller can build a pagination envelope.
func ListReviewsFromDB(
	pool *pgxpool.Pool,
	productID int,
	limit int,
	offset int,
) ([]models.Review, int, error) {

	ctx := context.Background()

	var total int

	err := pool.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM reviews
		WHERE product_id = $1
		`,
		productID,
	).Scan(&total)

	if err != nil {
		return nil, 0, err
	}

	rows, err := pool.Query(
		ctx,
		`
		SELECT
			r.id,
			r.product_id,
			r.user_id,
			u.name AS reviewer_name,
			r.order_id,
			r.rating,
			r.title,
			r.body,
			r.verified_purchase,
			r.created_at,
			r.updated_at
		FROM reviews r
		JOIN users u ON u.id = r.user_id
		WHERE r.product_id = $1
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $2 OFFSET $3
		`,
		productID,
		limit,
		offset,
	)

	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	reviews := make([]models.Review, 0)

	for rows.Next() {

		var review models.Review

		err := rows.Scan(
			&review.ID,
			&review.ProductID,
			&review.UserID,
			&review.ReviewerName,
			&review.OrderID,
			&review.Rating,
			&review.Title,
			&review.Body,
			&review.VerifiedPurchase,
			&review.CreatedAt,
			&review.UpdatedAt,
		)

		if err != nil {
			return nil, 0, err
		}

		reviews = append(reviews, review)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return reviews, total, nil
}

// GetReviewSummaryFromDB builds the rating picture for a
// product.
//
// Everything here is calculated from the reviews table on
// the fly. Nothing is cached in a column, so the average
// can never fall out of step with the reviews that
// produced it.
//
// The distribution is always returned with all five star
// levels present, because a chart with a missing bar is
// harder to read than one with a zero-height bar.
func GetReviewSummaryFromDB(
	pool *pgxpool.Pool,
	productID int,
) (models.ReviewSummary, error) {

	ctx := context.Background()

	summary := models.ReviewSummary{
		ProductID: productID,

		Distribution: map[int]int{
			1: 0,
			2: 0,
			3: 0,
			4: 0,
			5: 0,
		},
	}

	// The average and the count come from one query.
	err := pool.QueryRow(
		ctx,
		`
		SELECT
			COALESCE(AVG(rating), 0),
			COUNT(*)
		FROM reviews
		WHERE product_id = $1
		`,
		productID,
	).Scan(
		&summary.AverageRating,
		&summary.ReviewCount,
	)

	if err != nil {
		return models.ReviewSummary{}, err
	}

	// The distribution comes from a second query,
	// grouped by the number of stars.
	rows, err := pool.Query(
		ctx,
		`
		SELECT rating, COUNT(*)
		FROM reviews
		WHERE product_id = $1
		GROUP BY rating
		`,
		productID,
	)

	if err != nil {
		return models.ReviewSummary{}, err
	}

	defer rows.Close()

	for rows.Next() {

		var rating int

		var count int

		err := rows.Scan(&rating, &count)

		if err != nil {
			return models.ReviewSummary{}, err
		}

		summary.Distribution[rating] = count
	}

	if err := rows.Err(); err != nil {
		return models.ReviewSummary{}, err
	}

	return summary, nil
}

// GetReviewByUserAndProductFromDB finds the review one
// person left on one product.
//
// The reviews table allows only one per person per
// product, so this can return at most one row.
func GetReviewByUserAndProductFromDB(
	pool *pgxpool.Pool,
	userID int,
	productID int,
) (models.Review, error) {

	var review models.Review

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT
			id,
			product_id,
			user_id,
			order_id,
			rating,
			title,
			body,
			verified_purchase,
			created_at,
			updated_at
		FROM reviews
		WHERE user_id = $1
		AND product_id = $2
		`,
		userID,
		productID,
	).Scan(
		&review.ID,
		&review.ProductID,
		&review.UserID,
		&review.OrderID,
		&review.Rating,
		&review.Title,
		&review.Body,
		&review.VerifiedPurchase,
		&review.CreatedAt,
		&review.UpdatedAt,
	)

	if err != nil {
		return models.Review{}, err
	}

	return review, nil
}

// UpdateReviewInDB changes a review that belongs to one
// specific person.
//
// The user_id in the WHERE clause is the safety net: a
// person cannot edit somebody else's review even if they
// know its ID.
func UpdateReviewInDB(
	pool *pgxpool.Pool,
	reviewID int,
	userID int,
	review models.Review,
) (models.Review, error) {

	err := pool.QueryRow(
		context.Background(),
		`
		UPDATE reviews
		SET
			rating = $1,
			title = $2,
			body = $3,
			updated_at = NOW()
		WHERE id = $4
		AND user_id = $5
		RETURNING
			id,
			product_id,
			user_id,
			order_id,
			rating,
			title,
			body,
			verified_purchase,
			created_at,
			updated_at
		`,
		review.Rating,
		review.Title,
		review.Body,
		reviewID,
		userID,
	).Scan(
		&review.ID,
		&review.ProductID,
		&review.UserID,
		&review.OrderID,
		&review.Rating,
		&review.Title,
		&review.Body,
		&review.VerifiedPurchase,
		&review.CreatedAt,
		&review.UpdatedAt,
	)

	if err != nil {
		return models.Review{}, err
	}

	return review, nil
}

// DeleteReviewInDB removes a review that belongs to one
// specific person.
func DeleteReviewInDB(
	pool *pgxpool.Pool,
	reviewID int,
	userID int,
) (bool, error) {

	tag, err := pool.Exec(
		context.Background(),
		`
		DELETE FROM reviews
		WHERE id = $1
		AND user_id = $2
		`,
		reviewID,
		userID,
	)

	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil
}
