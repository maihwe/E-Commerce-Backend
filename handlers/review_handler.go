package handlers

import (
	"errors"
	"net/http"
	"strings"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ListReviewsHandler returns a page of reviews for a
// product, together with the rating summary.
//
// The summary is included in the same response because a
// reviews page always needs both, and two requests would
// only give the client a chance to render one without the
// other.
func ListReviewsHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		pagination := utils.ParsePagination(r)

		reviews, total, err := storage.ListReviewsFromDB(
			pool,
			productID,
			pagination.PerPage,
			pagination.Offset(),
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load reviews",
			)

			return
		}

		summary, err := storage.GetReviewSummaryFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load the rating summary",
			)

			return
		}

		page := utils.NewPage(
			reviews,
			pagination,
			total,
		)

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"data": page.Data,

				"page": page.Page,

				"per_page": page.PerPage,

				"total": page.Total,

				"total_pages": page.TotalPages,

				"summary": summary,
			},
		)
	}
}

// CreateReviewHandler writes a review for a product.
//
// A shopper may only review a product once. The database
// enforces that with a uniqueness constraint, and this
// checks for an existing review first so the answer is a
// clear message rather than a raw duplicate-key error.
func CreateReviewHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

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

		var request models.ReviewRequest

		if !decodeJSONBody(w, r, &request) {
			return
		}

		rejection := validateReview(request)

		if rejection != "" {

			writeError(
				w,
				http.StatusBadRequest,
				rejection,
			)

			return
		}

		// One review per person per product.
		_, err :=
			storage.GetReviewByUserAndProductFromDB(
				pool,
				user.ID,
				productID,
			)

		if err == nil {

			writeError(
				w,
				http.StatusConflict,
				"You have already reviewed this product. Edit your review instead.",
			)

			return
		}

		if !errors.Is(err, pgx.ErrNoRows) {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not check your existing review",
			)

			return
		}

		// A review is marked as a verified purchase when
		// this shopper has had the product delivered.
		// That is what lets a reader tell a genuine
		// buyer's opinion from anyone else's.
		orderID, err :=
			storage.FindDeliveredOrderForProductFromDB(
				pool,
				user.ID,
				productID,
			)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not check your orders",
			)

			return
		}

		review, err := storage.CreateReviewInDB(
			pool,
			models.Review{
				ProductID: productID,

				UserID: user.ID,

				OrderID: orderID,

				Rating: request.Rating,

				Title: strings.TrimSpace(
					request.Title,
				),

				Body: strings.TrimSpace(request.Body),

				VerifiedPurchase: orderID != nil,
			},
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not save your review",
			)

			return
		}

		writeJSON(w, http.StatusCreated, review)
	}
}

// UpdateReviewHandler edits the signed-in shopper's own
// review of a product.
func UpdateReviewHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		var request models.ReviewRequest

		if !decodeJSONBody(w, r, &request) {
			return
		}

		rejection := validateReview(request)

		if rejection != "" {

			writeError(
				w,
				http.StatusBadRequest,
				rejection,
			)

			return
		}

		existing, err :=
			storage.GetReviewByUserAndProductFromDB(
				pool,
				user.ID,
				productID,
			)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"You have not reviewed this product",
			)

			return
		}

		updated, err := storage.UpdateReviewInDB(
			pool,
			existing.ID,
			user.ID,
			models.Review{
				Rating: request.Rating,

				Title: strings.TrimSpace(
					request.Title,
				),

				Body: strings.TrimSpace(request.Body),
			},
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not update your review",
			)

			return
		}

		writeJSON(w, http.StatusOK, updated)
	}
}

// DeleteReviewHandler removes the signed-in shopper's own
// review of a product.
func DeleteReviewHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		existing, err :=
			storage.GetReviewByUserAndProductFromDB(
				pool,
				user.ID,
				productID,
			)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"You have not reviewed this product",
			)

			return
		}

		removed, err := storage.DeleteReviewInDB(
			pool,
			existing.ID,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not delete your review",
			)

			return
		}

		if !removed {

			writeError(
				w,
				http.StatusNotFound,
				"Review not found",
			)

			return
		}

		writeMessage(
			w,
			http.StatusOK,
			"Your review has been removed",
		)
	}
}

// validateReview checks a review body.
//
// The rating bound is checked here and also by a CHECK
// constraint in the database. The database is the real
// guarantee; this exists so the shopper gets a sentence
// rather than a constraint violation.
func validateReview(
	request models.ReviewRequest,
) string {

	if request.Rating < 1 || request.Rating > 5 {

		return "Rating must be between 1 and 5"
	}

	if len(strings.TrimSpace(request.Title)) > 200 {

		return "Title cannot be longer than 200 characters"
	}

	if len(strings.TrimSpace(request.Body)) > 5000 {

		return "Review cannot be longer than 5000 characters"
	}

	return ""
}
