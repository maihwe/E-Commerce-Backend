package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegistrationRequest is the JSON body accepted by
// the register and login endpoints.
type RegistrationRequest struct {

	Name string `json:"name"`

	Email string `json:"email"`

	Password string `json:"password"`

	// Role is optional. A visitor may sign up as a
	// seller, but never as an admin, because admin
	// rights can only be granted by another admin.
	Role string `json:"role"`
}

// sessionLifetime is how long a login lasts before
// the user has to sign in again.
const sessionLifetime = 24 * time.Hour

// writeSessionCookie sends the session token to the
// browser as an HttpOnly cookie.
//
// HttpOnly means JavaScript cannot read the token,
// which protects it from cross-site scripting.
func writeSessionCookie(
	w http.ResponseWriter,
	token string,
	expiresAt time.Time,
) {

	http.SetCookie(
		w,
		&http.Cookie{
			Name: "session_token",

			Value: token,

			Path: "/",

			HttpOnly: true,

			// Secure stays false so the cookie also
			// works over plain HTTP during local
			// development. It must be set to true
			// before this runs on a real domain.
			Secure: false,

			SameSite: http.SameSiteLaxMode,

			Expires: expiresAt,
		},
	)
}

// RegisterHandler creates a new user account.
func RegisterHandler(pool *pgxpool.Pool) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set(
			"Content-Type",
			"application/json",
		)

		var request RegistrationRequest

		err := json.NewDecoder(
			r.Body,
		).Decode(&request)

		if err != nil {

			http.Error(
				w,
				"Invalid JSON",
				http.StatusBadRequest,
			)

			return
		}

		request.Name =
			strings.TrimSpace(request.Name)

		if request.Name == "" {

			http.Error(
				w,
				"Name is required",
				http.StatusBadRequest,
			)

			return
		}

		validationError :=
			utils.ValidateUserRegistration(
				request.Email,
				request.Password,
			)

		if validationError != "" {

			http.Error(
				w,
				validationError,
				http.StatusBadRequest,
			)

			return
		}

		// A visitor may choose to sign up as a buyer
		// or as a seller. Anything else, including
		// admin, is rejected and falls back to buyer.
		role := strings.ToLower(
			strings.TrimSpace(request.Role),
		)

		if role != models.RoleSeller {
			role = models.RoleBuyer
		}

		passwordHash, err :=
			utils.HashPassword(
				request.Password,
			)

		if err != nil {

			http.Error(
				w,
				"Could not secure password",
				http.StatusInternalServerError,
			)

			return
		}

		user := models.User{
			Name: strings.TrimSpace(request.Name),

			Email: strings.ToLower(
				strings.TrimSpace(request.Email),
			),

			PasswordHash: passwordHash,

			Role: role,
		}

		createdUser, err :=
			storage.CreateUserInDB(
				pool,
				user,
			)

		if err != nil {

			// PostgreSQL error 23505 means a UNIQUE
			// constraint was broken, which here can
			// only be the email address.
			var pgError *pgconn.PgError

			if errors.As(err, &pgError) &&
				pgError.Code == "23505" {

				http.Error(
					w,
					"Email is already registered",
					http.StatusConflict,
				)

				return
			}

			http.Error(
				w,
				"Could not create account",
				http.StatusInternalServerError,
			)

			return
		}

		// Never send the password hash to the browser.
		createdUser.PasswordHash = ""

		w.WriteHeader(
			http.StatusCreated,
		)

		json.NewEncoder(w).Encode(
			createdUser,
		)
	}
}

// LoginHandler authenticates an existing user and
// starts a session for them.
func LoginHandler(pool *pgxpool.Pool) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set(
			"Content-Type",
			"application/json",
		)

		var request RegistrationRequest

		err := json.NewDecoder(
			r.Body,
		).Decode(&request)

		if err != nil {

			http.Error(
				w,
				"Invalid JSON",
				http.StatusBadRequest,
			)

			return
		}

		user, err :=
			storage.GetUserByEmailFromDB(
				pool,
				request.Email,
			)

		if err != nil {

			// The same message is used whether the
			// email or the password was wrong, so an
			// attacker cannot discover which email
			// addresses have accounts.
			http.Error(
				w,
				"Invalid email or password",
				http.StatusUnauthorized,
			)

			return
		}

		if !utils.CheckPassword(
			request.Password,
			user.PasswordHash,
		) {

			http.Error(
				w,
				"Invalid email or password",
				http.StatusUnauthorized,
			)

			return
		}

		token, err :=
			utils.GenerateSessionToken()

		if err != nil {

			http.Error(
				w,
				"Could not create session",
				http.StatusInternalServerError,
			)

			return
		}

		expiresAt := time.Now().Add(
			sessionLifetime,
		)

		session := models.Session{
			Token: token,

			UserID: user.ID,

			ExpiresAt: expiresAt,
		}

		storage.CreateSession(
			session,
		)

		writeSessionCookie(
			w,
			token,
			expiresAt,
		)

		// Never send the password hash to the browser.
		user.PasswordHash = ""

		json.NewEncoder(w).Encode(
			user,
		)
	}
}

// LogoutHandler ends the current user's session.
func LogoutHandler(pool *pgxpool.Pool) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set(
			"Content-Type",
			"application/json",
		)

		// Look for the session cookie.
		cookie, err :=
			r.Cookie("session_token")

		// If there is no cookie, the user is
		// already logged out and there is
		// nothing to clean up.
		if err != nil {

			json.NewEncoder(w).Encode(
				map[string]string{
					"message": "Logged out successfully",
				},
			)

			return
		}

		// Remove the session from our
		// in-memory session storage.
		storage.DeleteSession(
			cookie.Value,
		)

		// Remove the cookie from the browser by
		// overwriting it with an empty value that
		// has already expired.
		http.SetCookie(
			w,
			&http.Cookie{
				Name: "session_token",

				Value: "",

				Path: "/",

				HttpOnly: true,

				Secure: false,

				SameSite: http.SameSiteLaxMode,

				MaxAge: -1,

				Expires: time.Unix(1, 0),
			},
		)

		json.NewEncoder(w).Encode(
			map[string]string{
				"message": "Logged out successfully",
			},
		)
	}
}

// MeHandler returns the account that is currently
// signed in.
//
// It is a convenient way for a client to check
// whether a session is still valid and which role
// the signed-in user holds.
func MeHandler(pool *pgxpool.Pool) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set(
			"Content-Type",
			"application/json",
		)

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

			return
		}

		// Never send the password hash to the browser.
		user.PasswordHash = ""

		json.NewEncoder(w).Encode(
			user,
		)
	}
}
