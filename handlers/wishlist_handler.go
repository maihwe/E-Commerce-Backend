package handlers

import (
	"net/http"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListWishlistHandler returns everything the shopper has
// saved for later.
func ListWishlistHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		items, err := storage.ListWishlistFromDB(
			pool,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load your wishlist",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"data": items,

				"total": len(items),
			},
		)
	}
}

// AddWishlistItemHandler saves a product.
//
// Saving something twice is not an error. A shopper who
// presses the heart a second time has changed nothing,
// and answering with a failure would be a confusing
// response to a harmless action.
func AddWishlistItemHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		var request models.WishlistAddRequest

		if !decodeJSONBody(w, r, &request) {
			return
		}

		if request.ProductID < 1 {

			writeError(
				w,
				http.StatusBadRequest,
				"Product is required",
			)

			return
		}

		// The product must exist. Saving a product that
		// is not there would leave an entry the shopper
		// could never open.
		_, err := storage.GetProductByIDFromDB(
			pool,
			request.ProductID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		err = storage.AddWishlistItemInDB(
			pool,
			user.ID,
			request.ProductID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not save the product",
			)

			return
		}

		writeMessage(
			w,
			http.StatusCreated,
			"Saved to your wishlist",
		)
	}
}

// RemoveWishlistItemHandler removes a saved product.
func RemoveWishlistItemHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "productID")

		if !ok {
			return
		}

		removed, err := storage.RemoveWishlistItemInDB(
			pool,
			user.ID,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not remove the product",
			)

			return
		}

		if !removed {

			writeError(
				w,
				http.StatusNotFound,
				"That product is not in your wishlist",
			)

			return
		}

		writeMessage(
			w,
			http.StatusOK,
			"Removed from your wishlist",
		)
	}
}
