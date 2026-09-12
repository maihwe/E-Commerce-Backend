package handlers

import (
	"net/http"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GetCartHandler returns the signed-in shopper's cart.
//
// An empty cart is a normal answer, not an error, so this
// creates the cart row if it is the first visit and
// returns an empty list of items.
func GetCartHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		cartID, err := storage.GetOrCreateCartInDB(
			pool,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not open your cart",
			)

			return
		}

		cart, err := storage.GetCartFromDB(pool, cartID)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load your cart",
			)

			return
		}

		writeJSON(w, http.StatusOK, cart)
	}
}

// AddCartItemHandler puts a product in the cart.
func AddCartItemHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		var request models.CartAddRequest

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

		if request.Quantity < 1 {

			writeError(
				w,
				http.StatusBadRequest,
				"Quantity must be at least 1",
			)

			return
		}

		// The product must exist and still be on sale.
		// Adding a withdrawn product to a cart would
		// only fail later at checkout, which is a worse
		// moment to find out.
		product, err := storage.GetProductByIDFromDB(
			pool,
			request.ProductID,
		)

		if err != nil || !product.IsActive {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		cartID, err := storage.GetOrCreateCartInDB(
			pool,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not open your cart",
			)

			return
		}

		err = storage.AddCartItemInDB(
			pool,
			cartID,
			request.ProductID,
			request.Quantity,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not add the item to your cart",
			)

			return
		}

		respondWithCart(pool, w, cartID)
	}
}

// UpdateCartItemHandler changes the quantity of an item
// already in the cart.
func UpdateCartItemHandler(
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

		var request models.CartUpdateRequest

		if !decodeJSONBody(w, r, &request) {
			return
		}

		// Setting a quantity to zero would leave a row
		// saying "none of this", which is what removal
		// is for. Refusing it keeps the cart honest.
		if request.Quantity < 1 {

			writeError(
				w,
				http.StatusBadRequest,
				"Quantity must be at least 1. To remove an item, delete it instead.",
			)

			return
		}

		cartID, err := storage.GetOrCreateCartInDB(
			pool,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not open your cart",
			)

			return
		}

		found, err := storage.SetCartItemQuantityInDB(
			pool,
			cartID,
			productID,
			request.Quantity,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not update the item",
			)

			return
		}

		if !found {

			writeError(
				w,
				http.StatusNotFound,
				"That item is not in your cart",
			)

			return
		}

		respondWithCart(pool, w, cartID)
	}
}

// RemoveCartItemHandler takes an item out of the cart.
func RemoveCartItemHandler(
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

		cartID, err := storage.GetOrCreateCartInDB(
			pool,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not open your cart",
			)

			return
		}

		found, err := storage.RemoveCartItemInDB(
			pool,
			cartID,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not remove the item",
			)

			return
		}

		if !found {

			writeError(
				w,
				http.StatusNotFound,
				"That item is not in your cart",
			)

			return
		}

		respondWithCart(pool, w, cartID)
	}
}

// ClearCartHandler empties the cart.
func ClearCartHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		cartID, err := storage.GetOrCreateCartInDB(
			pool,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not open your cart",
			)

			return
		}

		err = storage.ClearCartInDB(pool, cartID)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not empty your cart",
			)

			return
		}

		respondWithCart(pool, w, cartID)
	}
}

// respondWithCart returns the whole cart after a change.
//
// Every cart endpoint answers with the full cart rather
// than just the item that moved. The subtotal and the
// item count both change when a line changes, and a
// client that had to work those out itself would
// eventually disagree with the server about them.
func respondWithCart(
	pool *pgxpool.Pool,
	w http.ResponseWriter,
	cartID int,
) {

	cart, err := storage.GetCartFromDB(pool, cartID)

	if err != nil {

		writeError(
			w,
			http.StatusInternalServerError,
			"Could not load your cart",
		)

		return
	}

	writeJSON(w, http.StatusOK, cart)
}
