package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"e-commerce-backend/handlers"
	"e-commerce-backend/models"
)

// maxFormBytes caps a submitted form.
//
// The number matches the limit the JSON endpoints put on a
// request body. A form post is read into memory in full
// before anything can look at it, so without a cap the
// size of a request is the size of the memory it costs.
const maxFormBytes = 1 << 20

// registerAuthPages wires the sign-in form, the sign-up
// form, and signing out.
//
// These duplicate the JSON endpoints' job rather than
// calling them, because a form does not post JSON. What
// they do not duplicate is any of the rules: the checks
// and the session handling are the same functions the
// endpoints call, in handlers/auth.go.
func registerAuthPages(
	mux *http.ServeMux,
	site *Site,
) {

	mux.HandleFunc(
		"GET /shop/login",
		site.loginForm,
	)

	mux.HandleFunc(
		"POST /shop/login",
		site.loginSubmit,
	)

	mux.HandleFunc(
		"GET /shop/register",
		site.registerForm,
	)

	mux.HandleFunc(
		"POST /shop/register",
		site.registerSubmit,
	)

	mux.HandleFunc(
		"POST /shop/logout",
		site.logout,
	)
}

// loginPage is the sign-in form.
type loginPage struct {
	base

	// Email is echoed back when a sign-in is refused, so
	// that a mistyped password does not also cost somebody
	// their email address.
	Email string

	// Next is where to go after signing in, which is how
	// a visitor who was sent here from the cart arrives
	// back at the cart.
	Next string
}

// registerPage is the sign-up form.
type registerPage struct {
	base

	Name string

	Email string
}

// loginForm shows the sign-in form.
func (s *Site) loginForm(
	w http.ResponseWriter,
	r *http.Request,
) {

	data := loginPage{
		base: s.baseFor(r, "Sign in", "account"),

		Email: strings.TrimSpace(
			r.URL.Query().Get("email"),
		),

		Next: safeNext(
			r.URL.Query().Get("next"),
		),
	}

	s.render(w, http.StatusOK, loginFile, data)
}

// loginSubmit signs somebody in.
//
// A refusal re-renders the form rather than redirecting,
// so the message appears beside the fields that caused it
// and the browser's back button is not needed to try
// again. A success redirects, which is what stops a
// refresh from submitting the password a second time.
func (s *Site) loginSubmit(
	w http.ResponseWriter,
	r *http.Request,
) {

	if !parseForm(w, r) {
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))

	password := r.FormValue("password")

	next := safeNext(r.FormValue("next"))

	user, err := handlers.AuthenticateUser(
		s.pool,
		email,
		password,
	)

	if errors.Is(err, handlers.ErrInvalidCredentials) {

		data := loginPage{
			base: s.baseFor(r, "Sign in", "account"),

			Email: email,

			Next: next,
		}

		data.Error = "That email and password do not match an account."

		s.render(w, http.StatusUnauthorized, loginFile, data)

		return
	}

	// Anything else is a failure to look the account up,
	// which is a fault here rather than something the
	// visitor can correct by typing differently.
	if err != nil {

		s.fail(
			w,
			r,
			http.StatusInternalServerError,
			"The sign-in could not be completed.",
		)

		return
	}

	err = handlers.StartSession(w, user.ID)

	if err != nil {

		s.fail(
			w,
			r,
			http.StatusInternalServerError,
			"The sign-in could not be completed.",
		)

		return
	}

	http.Redirect(w, r, next, http.StatusSeeOther)
}

// registerForm shows the sign-up form.
func (s *Site) registerForm(
	w http.ResponseWriter,
	r *http.Request,
) {

	data := registerPage{
		base: s.baseFor(r, "Create an account", "account"),
	}

	s.render(w, http.StatusOK, registerFile, data)
}

// registerSubmit creates an account and signs it in.
//
// The JSON endpoint deliberately does not start a session,
// and this does. That is not a disagreement: it is the two
// steps the API documents, a registration followed by a
// sign-in, performed here without making somebody type
// their brand new password a second time.
func (s *Site) registerSubmit(
	w http.ResponseWriter,
	r *http.Request,
) {

	if !parseForm(w, r) {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))

	email := strings.TrimSpace(r.FormValue("email"))

	password := r.FormValue("password")

	role := strings.TrimSpace(r.FormValue("role"))

	user, err := handlers.RegisterAccount(
		s.pool,
		name,
		email,
		password,
		role,
	)

	if err != nil {

		data := registerPage{
			base: s.baseFor(
				r,
				"Create an account",
				"account",
			),

			Name: name,

			Email: email,
		}

		status := http.StatusBadRequest

		// A ValidationError is written to be read by the
		// person who caused it, so it is shown as it
		// stands. An address already in use is answered
		// as the conflict it is.
		var invalid handlers.ValidationError

		if errors.As(err, &invalid) {

			data.Error = invalid.Message

		} else if errors.Is(err, handlers.ErrEmailTaken) {

			status = http.StatusConflict

			data.Error = "That email address already has an account. Try signing in instead."

		} else {

			s.fail(
				w,
				r,
				http.StatusInternalServerError,
				"The account could not be created.",
			)

			return
		}

		s.render(w, status, registerFile, data)

		return
	}

	// RegisterAccount already cleared the password hash,
	// so the user can be handed to StartSession as it is.
	err = handlers.StartSession(w, user.ID)

	if err != nil {

		s.fail(
			w,
			r,
			http.StatusInternalServerError,
			"The account was created, but signing in failed.",
		)

		return
	}

	http.Redirect(w, r, "/shop", http.StatusSeeOther)
}

// logout ends the session and returns to the catalog.
func (s *Site) logout(
	w http.ResponseWriter,
	r *http.Request,
) {

	handlers.EndSession(w, r)

	http.Redirect(w, r, "/shop", http.StatusSeeOther)
}

// requireUser returns the signed-in user, or sends the
// visitor to the sign-in form and reports false.
//
// The page they were trying to reach is carried through,
// so that signing in returns them to it rather than to the
// front page.
func (s *Site) requireUser(
	w http.ResponseWriter,
	r *http.Request,
) (models.User, bool) {

	user := handlers.CurrentUserOrNil(s.pool, r)

	if user.ID == 0 {

		http.Redirect(
			w,
			r,
			"/shop/login?next="+url.QueryEscape(
				r.URL.RequestURI(),
			),
			http.StatusSeeOther,
		)

		return models.User{}, false
	}

	return user, true
}

// requireRole is requireUser with a check on what the
// account is allowed to do.
//
// Somebody who is signed in but holds the wrong role is
// not sent to the sign-in form, because signing in again
// would change nothing. They get the error page instead,
// which is the honest answer to "this page is not for
// you".
func (s *Site) requireRole(
	w http.ResponseWriter,
	r *http.Request,
	roles ...string,
) (models.User, bool) {

	user, ok := s.requireUser(w, r)

	if !ok {
		return models.User{}, false
	}

	for _, role := range roles {

		if user.Role == role {
			return user, true
		}
	}

	s.fail(
		w,
		r,
		http.StatusForbidden,
		"This page is not available to your account.",
	)

	return models.User{}, false
}

// parseForm reads a submitted form, refusing one that is
// too large to be a form.
func parseForm(
	w http.ResponseWriter,
	r *http.Request,
) bool {

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		maxFormBytes,
	)

	err := r.ParseForm()

	if err != nil {

		http.Error(
			w,
			"That form was too large to read.",
			http.StatusRequestEntityTooLarge,
		)

		return false
	}

	return true
}

// safeNext decides where to send somebody after they sign
// in.
//
// The address comes from the query string, which means it
// is under a visitor's control, so it is honoured only
// when it points at a page inside this storefront.
// Accepting any address would turn the sign-in page into a
// way of borrowing this site's name: the link really would
// begin here, and it really would end somewhere else.
func safeNext(value string) string {

	value = strings.TrimSpace(value)

	// A path that is exactly /shop, or starts with /shop/
	// or /shop?, is inside the storefront. Anything else,
	// including another site's address, is not.
	insideShop := value == "/shop" ||
		strings.HasPrefix(value, "/shop/") ||
		strings.HasPrefix(value, "/shop?")

	if !insideShop {
		return "/shop"
	}

	// "//example.com" begins with a slash and is still
	// another site, and so is "/\example.com", which some
	// browsers read as the same thing. A single leading
	// slash is not enough on its own.
	if strings.HasPrefix(value, "//") ||
		strings.HasPrefix(value, "/\\") {

		return "/shop"
	}

	return value
}
