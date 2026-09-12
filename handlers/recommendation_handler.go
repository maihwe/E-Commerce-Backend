package handlers

import (
	"net/http"

	"e-commerce-backend/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Default number of suggestions to return.
const defaultRecommendationLimit = 5

// Maximum number of suggestions a client may ask for.
const maxRecommendationLimit = 20

// GetRecommendationsHandler suggests products to show
// alongside one a shopper is looking at.
//
// It is public, because a product page is public, and the
// suggestions are drawn from what has actually been
// bought together rather than from anything personal.
func GetRecommendationsHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		// Confirm the product exists, so a request for
		// suggestions on a product that is not there
		// answers 404 instead of silently returning
		// the top rated items in no category.
		if _, err := storage.GetProductByIDFromDB(
			pool,
			productID,
		); err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		limit := queryInt(
			r,
			"limit",
			defaultRecommendationLimit,
		)

		if limit < 1 {
			limit = defaultRecommendationLimit
		}

		if limit > maxRecommendationLimit {
			limit = maxRecommendationLimit
		}

		products, err := storage.GetRecommendationsFromDB(
			pool,
			productID,
			limit,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load recommendations",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"product_id": productID,

				"data": products,

				"total": len(products),
			},
		)
	}
}
