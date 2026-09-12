package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sessionCookieName is the cookie a signed-in browser
// carries.
//
// It is named once here because the sign-in form and the
// JSON endpoint both write it, and both sign-outs read
// it. A reader and a writer that disagree about this
// string produce the worst kind of bug: a login that
// appears to work, followed by every later request
// behaving as though nobody had signed in.
const sessionCookieName = "session_token"

// GetAuthenticatedUserID identifies the user associated
// with the session cookie on an HTTP request.
//
// It returns the user's ID when the session is valid.
// It returns an error when the user is not authenticated.
func GetAuthenticatedUserID(r *http.Request) (int, error) {

	// Read the session cookie from the request.
	cookie, err := r.Cookie(sessionCookieName)

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

// ErrInvalidCredentials means the email or the password
// was wrong.
//
// One error covers both cases deliberately. Telling a
// stranger which of the two was wrong turns the sign-in
// form into a way of finding out which addresses have
// accounts here, one guess at a time.
var ErrInvalidCredentials = errors.New(
	"invalid email or password",
)

// AuthenticateUser checks an email and a password against
// the accounts table.
//
// It returns ErrInvalidCredentials for an unknown email, a
// wrong password, or both. Anything else it returns is a
// real failure to look the account up, and the caller
// should report that as a server error rather than as a
// rejected sign-in. A database that is unreachable and a
// password that is wrong are not the same event, and
// calling them by the same name is the difference between
// a five-minute diagnosis and an hour spent looking at the
// wrong thing.
//
// The password hash is left on the returned user. It is
// the caller's job to clear it before the user goes
// anywhere near a response.
func AuthenticateUser(
	pool *pgxpool.Pool,
	email string,
	password string,
) (models.User, error) {

	user, err := storage.GetUserByEmailFromDB(
		pool,
		email,
	)

	if errors.Is(err, pgx.ErrNoRows) {

		return models.User{},
			ErrInvalidCredentials
	}

	if err != nil {
		return models.User{}, err
	}

	if !utils.CheckPassword(
		password,
		user.PasswordHash,
	) {

		return models.User{},
			ErrInvalidCredentials
	}

	return user, nil
}

// StartSession begins a session for a user and sends the
// browser the cookie that carries it.
//
// This is the only place a session is created and the only
// place a session cookie is written, which is what makes
// the attributes below the attributes of every login in
// the application, however that login was made. The
// HttpOnly flag, the SameSite setting, the lifetime, and
// the Secure flag that has to become true before this runs
// on a real domain are all decided here rather than in
// each caller.
func StartSession(
	w http.ResponseWriter,
	userID int,
) error {

	token, err := utils.GenerateSessionToken()

	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(sessionLifetime)

	storage.CreateSession(
		models.Session{
			Token: token,

			UserID: userID,

			ExpiresAt: expiresAt,
		},
	)

	writeSessionCookie(w, token, expiresAt)

	return nil
}

// EndSession forgets the session named by the request's
// cookie and tells the browser to drop it.
//
// It is safe to call when no cookie was sent, which is
// what somebody who has already signed out looks like, and
// it reports nothing about whether a session existed. A
// sign-out that succeeds either way is both what people
// expect and the answer that gives away least.
func EndSession(
	w http.ResponseWriter,
	r *http.Request,
) {

	cookie, err := r.Cookie(sessionCookieName)

	if err == nil {
		storage.DeleteSession(cookie.Value)
	}

	clearSessionCookie(w)
}

// ValidationError is a complaint about what somebody
// typed, written to be read by the person who typed it.
//
// It is a distinct type rather than a plain error so that
// a caller can tell the two apart. A message written for a
// person is safe to send back in a response; a failure of
// the server is not, and a handler that treated them the
// same way would end up either hiding a useful message or
// publishing a database error.
type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

// ErrEmailTaken means an account already exists for that
// address.
//
// It is separate from ValidationError because it is
// discovered by the database rather than by the rules,
// and because it answers 409 rather than 400: the request
// was well formed, it just asked for something that is
// already taken.
var ErrEmailTaken = errors.New(
	"email is already registered",
)

// RegisterAccount creates an account.
//
// Every rule about what a new account may be lives here,
// which is the point of the function. In particular the
// role: a visitor may sign up as a buyer or as a seller,
// and anything else falls back to buyer. Admin is not
// reachable through this function at all, because admin
// rights may only be granted by an existing admin, and
// registration is a thing anybody on the internet can do.
//
// It returns a ValidationError for something the person
// can fix, ErrEmailTaken for an address already in use,
// and a plain error for anything else, which the caller
// should report as a server fault without repeating the
// detail to the client.
func RegisterAccount(
	pool *pgxpool.Pool,
	name string,
	email string,
	password string,
	role string,
) (models.User, error) {

	name = strings.TrimSpace(name)

	if name == "" {

		return models.User{}, ValidationError{
			Message: "Name is required",
		}
	}

	validationError := utils.ValidateUserRegistration(
		email,
		password,
	)

	if validationError != "" {

		return models.User{}, ValidationError{
			Message: validationError,
		}
	}

	role = strings.ToLower(strings.TrimSpace(role))

	if role != models.RoleSeller {
		role = models.RoleBuyer
	}

	passwordHash, err := utils.HashPassword(password)

	if err != nil {
		return models.User{}, err
	}

	user, err := storage.CreateUserInDB(
		pool,
		models.User{
			Name: name,

			Email: strings.ToLower(
				strings.TrimSpace(email),
			),

			PasswordHash: passwordHash,

			Role: role,
		},
	)

	if err != nil {

		// PostgreSQL error 23505 means a UNIQUE
		// constraint was broken, which here can only be
		// the email address.
		var pgError *pgconn.PgError

		if errors.As(err, &pgError) &&
			pgError.Code == "23505" {

			return models.User{}, ErrEmailTaken
		}

		return models.User{}, err
	}

	// Never send the password hash to the browser.
	user.PasswordHash = ""

	return user, nil
}
