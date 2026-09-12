package handlers

import (
	"net/http"
	"strings"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListUsersHandler returns every account.
//
// The password hash is never included, because
// storage.ListUsersInDB does not select it in the first
// place. Leaving a secret out of the query is stronger
// than remembering to strip it afterwards.
func ListUsersHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if !RequireAdmin(pool, w, r) {
			return
		}

		users, err := storage.ListUsersInDB(pool)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load users",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"data": users,

				"total": len(users),
			},
		)
	}
}

// UpdateUserRoleHandler changes what an account may do.
func UpdateUserRoleHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if !RequireAdmin(pool, w, r) {
			return
		}

		userID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		var request struct {
			Role string `json:"role"`
		}

		if !decodeJSONBody(w, r, &request) {
			return
		}

		role := strings.ToLower(
			strings.TrimSpace(request.Role),
		)

		rejection := utils.ValidateRole(role)

		if rejection != "" {

			writeError(
				w,
				http.StatusBadRequest,
				rejection,
			)

			return
		}

		updated, err := storage.UpdateUserRoleInDB(
			pool,
			userID,
			role,
		)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"User not found",
			)

			return
		}

		writeJSON(w, http.StatusOK, updated)
	}
}

// ListAllOrdersHandler returns every order on the
// marketplace.
//
// The same query backs the buyer and seller views, but the
// filter is left empty here, which is what makes this the
// admin's view of everything.
func ListAllOrdersHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if !RequireAdmin(pool, w, r) {
			return
		}

		pagination := utils.ParsePagination(r)

		filter := models.OrderFilter{
			Status: strings.TrimSpace(
				r.URL.Query().Get("status"),
			),

			BuyerID: queryInt(r, "buyer", 0),

			SellerID: queryInt(r, "seller", 0),

			Limit: pagination.PerPage,

			Offset: pagination.Offset(),
		}

		if filter.Status != "" &&
			!models.IsValidOrderStatus(filter.Status) {

			writeError(
				w,
				http.StatusBadRequest,
				"Unknown order status",
			)

			return
		}

		orders, total, err := storage.ListOrdersFromDB(
			pool,
			filter,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load orders",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			utils.NewPage(
				orders,
				pagination,
				total,
			),
		)
	}
}
