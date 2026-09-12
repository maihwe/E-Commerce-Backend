package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// requireUser fetches the signed-in user, or writes a 401
// and reports false.
//
// Nearly every handler below opens with the same three
// lines. Folding them into one helper means the
// authentication decision is made in exactly one place,
// and a handler cannot accidentally forget it:
//
//	user, ok := requireUser(pool, w, r)
//	if !ok {
//		return
//	}
func requireUser(
	pool *pgxpool.Pool,
	w http.ResponseWriter,
	r *http.Request,
) (models.User, bool) {

	user, err := GetAuthenticatedUser(pool, r)

	if err != nil {

		writeError(
			w,
			http.StatusUnauthorized,
			"Authentication required",
		)

		return models.User{}, false
	}

	return user, true
}

// pathID reads a whole number out of the URL path.
//
// It answers 400 itself when the value is missing or is
// not a positive number, so a handler that receives a
// usable ID can carry straight on.
func pathID(
	w http.ResponseWriter,
	r *http.Request,
	name string,
) (int, bool) {

	value := strings.TrimSpace(
		r.PathValue(name),
	)

	number, err := strconv.Atoi(value)

	if err != nil || number < 1 {

		writeError(
			w,
			http.StatusBadRequest,
			"Invalid "+name,
		)

		return 0, false
	}

	return number, true
}

// queryInt reads a whole number from the query string,
// falling back to a default when it is missing or
// unreadable.
//
// Query strings are typed by hand, so an unusable value
// falls back rather than failing the request. A client
// that sends ?page=abc still deserves the first page.
func queryInt(
	r *http.Request,
	name string,
	fallback int,
) int {

	value := strings.TrimSpace(
		r.URL.Query().Get(name),
	)

	if value == "" {
		return fallback
	}

	number, err := strconv.Atoi(value)

	if err != nil {
		return fallback
	}

	return number
}

// queryFloat reads a decimal number from the query
// string.
//
// Unlike queryInt this reports failure, because the two
// places it is used are price bounds, and silently
// ignoring a price filter would show a shopper products
// they explicitly asked to exclude.
func queryFloat(
	r *http.Request,
	name string,
) (*float64, error) {

	value := strings.TrimSpace(
		r.URL.Query().Get(name),
	)

	if value == "" {
		return nil, nil
	}

	number, err := strconv.ParseFloat(value, 64)

	if err != nil {

		return nil, errors.New(
			name + " must be a number",
		)
	}

	if number < 0 {

		return nil, errors.New(
			name + " cannot be negative",
		)
	}

	return &number, nil
}
