package handlers

import (
	"errors"
	"net/http"
	"time"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GetAuthenticatedUserID identifies the user associated
// with the session cookie on an HTTP request.
//
// It returns the user's ID when the session is valid.
// It returns an error when the user is not authenticated.
func GetAuthenticatedUserID(r *http.Request) (int, error) {

	// Read the session cookie from the request.
	cookie, err := r.Cookie("session_token")

	if err != nil {
		return 0, errors.New("authentication required")
	}

	// Find the session using the token.
	session, exists :=
		storage.GetSession(cookie.Value)

	if !exists {
		return 0, errors.New("invalid session")
	}

	// Check whether the session has expired.
	if time.Now().After(session.ExpiresAt) {

		// Remove the expired session from storage.
		storage.DeleteSession(session.Token)

		return 0, errors.New("session expired")
	}

	// The session is valid.
	return session.UserID, nil
}

// GetAuthenticatedUser returns the full user associated
// with the current session.
//
// Handlers use this when they need the user's role, and
// not just their ID.
func GetAuthenticatedUser(
	pool *pgxpool.Pool,
	r *http.Request,
) (models.User, error) {

	userID, err :=
		GetAuthenticatedUserID(r)

	if err != nil {
		return models.User{}, err
	}

	user, err :=
		storage.GetUserByIDFromDB(
			pool,
			userID,
		)

	if err != nil {
		return models.User{}, errors.New("user not found")
	}

	return user, nil
}

// RequireRole checks that the current user is signed in
// and holds one of the listed roles.
//
// When access is allowed it returns true and writes
// nothing. When access is denied it writes the correct
// HTTP error and returns false, so the caller can
// simply return straight away:
//
//	if !RequireRole(pool, w, r, models.RoleSeller) {
//		return
//	}
func RequireRole(
	pool *pgxpool.Pool,
	w http.ResponseWriter,
	r *http.Request,
	roles ...string,
) bool {

	user, err :=
		GetAuthenticatedUser(
			pool,
			r,
		)

	if err != nil {

		http.Error(
			w,
			"Authentication required",
			http.StatusUnauthorized,
		)

		return false
	}

	// Compare the user's role against every role
	// that would be accepted here.
	for _, role := range roles {

		if user.Role == role {
			return true
		}
	}

	http.Error(
		w,
		"Access denied for role: "+user.Role,
		http.StatusForbidden,
	)

	return false
}

// RequireSeller allows sellers and admins through.
//
// Admins are included because they oversee the whole
// marketplace and may need to correct a listing.
func RequireSeller(
	pool *pgxpool.Pool,
	w http.ResponseWriter,
	r *http.Request,
) bool {

	return RequireRole(
		pool,
		w,
		r,
		models.RoleSeller,
		models.RoleAdmin,
	)
}

// RequireAdmin allows only administrators through.
func RequireAdmin(
	pool *pgxpool.Pool,
	w http.ResponseWriter,
	r *http.Request,
) bool {

	return RequireRole(
		pool,
		w,
		r,
		models.RoleAdmin,
	)
}

// CurrentUserOrNil returns the signed-in user, or an
// empty user when nobody is signed in.
//
// Public endpoints use this when they behave slightly
// differently for a signed-in visitor but must still
// work for a stranger.
func CurrentUserOrNil(
	pool *pgxpool.Pool,
	r *http.Request,
) models.User {

	user, err :=
		GetAuthenticatedUser(
			pool,
			r,
		)

	if err != nil {
		return models.User{}
	}

	return user
}
