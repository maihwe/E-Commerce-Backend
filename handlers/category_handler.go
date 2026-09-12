package handlers

import (
	"net/http"
	"strings"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListCategoriesHandler returns every category.
//
// Public, because the catalog menu is drawn before anyone
// signs in.
func ListCategoriesHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		categories, err :=
			storage.ListCategoriesFromDB(pool)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load categories",
			)

			return
		}

		writeJSON(w, http.StatusOK, categories)
	}
}

// CreateCategoryHandler adds a category.
//
// Only an admin may call this. Categories are the shape
// of the whole catalog, so letting any seller invent one
// would fragment it quickly.
func CreateCategoryHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if !RequireAdmin(pool, w, r) {
			return
		}

		var request struct {
			Name string `json:"name"`
		}

		if !decodeJSONBody(w, r, &request) {
			return
		}

		name := strings.TrimSpace(request.Name)

		if name == "" {

			writeError(
				w,
				http.StatusBadRequest,
				"Name is required",
			)

			return
		}

		slug := utils.Slugify(name)

		if slug == "" {

			writeError(
				w,
				http.StatusBadRequest,
				"Name must contain at least one letter or number",
			)

			return
		}

		category, err := storage.CreateCategoryInDB(
			pool,
			models.Category{
				Name: name,
				Slug: slug,
			},
		)

		if err != nil {

			// The slug column is UNIQUE, so a repeat
			// arrives here. Saying so plainly is more
			// useful than a generic failure.
			if strings.Contains(
				err.Error(),
				"duplicate key",
			) {

				writeError(
					w,
					http.StatusConflict,
					"A category with that name already exists",
				)

				return
			}

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not create the category",
			)

			return
		}

		writeJSON(w, http.StatusCreated, category)
	}
}
