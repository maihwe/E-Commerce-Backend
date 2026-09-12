package handlers

// The rules that used to live in these handlers have
// moved to auth.go, where the two callers that need them
// can share them: the JSON endpoints below, and the
// storefront's forms. What is left here is the part that
// is genuinely about JSON — decoding a body and choosing
// a status code.
import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
//
// It is only ever called by StartSession, so the
// attributes here are the attributes of every login in
// the application.
func writeSessionCookie(
	w http.ResponseWriter,
	token string,
	expiresAt time.Time,
) {

	http.SetCookie(
		w,
		&http.Cookie{
			Name: sessionCookieName,

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

// clearSessionCookie removes the session cookie from the
// browser by overwriting it with an empty value that has
// already expired.
//
// The attributes have to match the ones the cookie was set
// with. A browser matches a deletion to a cookie by name,
// path and domain, so a deletion with a different path
// leaves the original cookie in place and the visitor
// still carrying a token that no longer names a session.
func clearSessionCookie(w http.ResponseWriter) {

	http.SetCookie(
		w,
		&http.Cookie{
			Name: sessionCookieName,

			Value: "",

			Path: "/",

			HttpOnly: true,

			Secure: false,

			SameSite: http.SameSiteLaxMode,

			MaxAge: -1,

			Expires: time.Unix(1, 0),
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

		createdUser, err := RegisterAccount(
			pool,
			request.Name,
			request.Email,
			request.Password,
			request.Role,
		)

		if err != nil {

			// A message written for a person is safe to
			// send back to one. Anything else is a fault
			// here rather than in the request, so the
			// detail stays out of the response.
			var invalid ValidationError

			if errors.As(err, &invalid) {

				http.Error(
					w,
					invalid.Message,
					http.StatusBadRequest,
				)

				return
			}

			if errors.Is(err, ErrEmailTaken) {

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

		user, err := AuthenticateUser(
			pool,
			request.Email,
			request.Password,
		)

		if errors.Is(err, ErrInvalidCredentials) {

			http.Error(
				w,
				"Invalid email or password",
				http.StatusUnauthorized,
			)

			return
		}

		// Anything that is not a rejected sign-in is a
		// genuine failure to look the account up. It is
		// reported as one rather than being disguised as
		// a wrong password, which is what a handler that
		// treated every error the same way would do.
		if err != nil {

			http.Error(
				w,
				"Could not sign in",
				http.StatusInternalServerError,
			)

			return
		}

		err = StartSession(w, user.ID)

		if err != nil {

			http.Error(
				w,
				"Could not create session",
				http.StatusInternalServerError,
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

// LogoutHandler ends the current user's session.
func LogoutHandler(pool *pgxpool.Pool) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set(
			"Content-Type",
			"application/json",
		)

		// Forget the session and clear the cookie. This
		// is safe whether or not the request carried one,
		// which covers somebody who has already signed
		// out and is asking again.
		EndSession(w, r)

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
