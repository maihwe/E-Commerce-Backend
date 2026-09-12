// Package web serves the storefront: the same marketplace
// the JSON API exposes, rendered as pages a phone browser
// can use.
//
// It is part of this binary rather than a separate
// frontend for one concrete reason. Sessions live in an
// in-memory map inside the storage package, so a second
// process would hold a session map of its own, and a
// sign-in made in the browser would be invisible to the
// API. One process means one map, and one answer to the
// question of who is signed in.
//
// The pages are mounted under /shop because they cannot
// share a path with the API. Every meaningful path is
// already registered, and http.ServeMux panics on a
// duplicate pattern, so sharing would be a crash at
// startup rather than a routing preference. Root is the
// one path the API left free, and it redirects here.
//
// The rule this package follows is that it repeats the
// shape of a request but never the rules. Turning a form
// into arguments is done here, because a form is not a
// JSON body. Everything after that is the same storage,
// models and utils code the JSON handlers call, so a page
// and an endpoint cannot come to different conclusions
// about what is in stock or which status changes are
// allowed.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"

	"e-commerce-backend/handlers"
	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// assets carries the templates and the stylesheet into
// the binary, so there is nothing to deploy alongside it
// and nothing to read from disk while it runs.
//
// The database package embeds its migrations the same
// way, for the same reason.
//
//go:embed templates static
var assets embed.FS

// The name of each page file. They are named here rather
// than spelled out at the call site so that a page cannot
// be rendered under a name it was never parsed as, which
// would fail at the moment a visitor asked for it.
const (
	layoutFile   = "layout.html"
	catalogFile  = "catalog.html"
	productFile  = "product.html"
	loginFile    = "login.html"
	registerFile = "register.html"
	errorFile    = "error.html"
)

// pageFiles is every page that can be asked for by name.
var pageFiles = []string{
	catalogFile,
	productFile,
	loginFile,
	registerFile,
	errorFile,
}

// Site is the storefront. It holds the database pool and
// the parsed templates, and every handler below is a
// method on it so that neither has to be passed around.
type Site struct {

	pool *pgxpool.Pool

	pages map[string]*template.Template
}

// base is what every page has in common: who is looking
// at it, and what the header needs to draw itself.
//
// Page structs embed this rather than holding it in a
// field, so that a template reaches .User and .Product at
// the same level instead of nesting one inside the other.
type base struct {

	// Title is the page's own name, used in the browser
	// tab. It is empty on the catalog, which is the front
	// page and does not need to say so.
	Title string

	// SignedIn is true when a valid session cookie was
	// presented. User is only meaningful when it is.
	SignedIn bool

	User models.User

	IsAdmin bool

	// Query is the search box's current contents, echoed
	// back so that the words a shopper typed are still
	// there once the results have loaded.
	Query string

	// Nav names the section this page belongs to, so the
	// header can mark it. It is unused while the header
	// has only one section.
	Nav string

	// Error carries the message for the error page. It is
	// the only field here that is not about the header.
	Error string
}

// RegisterWebRoutes mounts the storefront on the mux.
//
// A template that will not parse is a fault in this
// package rather than anything a visitor can cause, so it
// is reported here, at startup, where main can log it and
// stop. The alternative is a process that answers
// requests for the rest of its life with half a site.
func RegisterWebRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
) error {

	pages, err := loadPages()

	if err != nil {
		return err
	}

	site := &Site{
		pool: pool,

		pages: pages,
	}

	// Root is the one path the API left free. Somebody
	// typing the bare address of the server should land
	// on the shop rather than on a 404, and the same
	// goes for the trailing slash, which is what a
	// browser adds when a person types a folder.
	mux.HandleFunc(
		"GET /{$}",
		redirectToShop,
	)

	mux.HandleFunc(
		"GET /shop/{$}",
		redirectToShop,
	)

	static, err := fs.Sub(assets, "static")

	if err != nil {

		return fmt.Errorf(
			"could not open the embedded stylesheet: %w",
			err,
		)
	}

	// The stylesheet is served out of the embedded copy,
	// so the file in the repository and the file a
	// browser receives are the same bytes by construction.
	mux.Handle(
		"GET /shop/static/",
		http.StripPrefix(
			"/shop/static/",
			http.FileServerFS(static),
		),
	)

	registerCatalogPages(mux, site)

	registerAuthPages(mux, site)

	return nil
}

// redirectToShop sends a visitor to the front page of the
// storefront.
func redirectToShop(
	w http.ResponseWriter,
	r *http.Request,
) {

	http.Redirect(
		w,
		r,
		"/shop",
		http.StatusSeeOther,
	)
}

// loadPages parses one template set per page.
//
// This is deliberately not a single set holding every
// page. Each page file defines a template called
// "content", and parsing all of them together would leave
// only the last one parsed under that name, so every URL
// would quietly render the same page. Parsing each page
// beside the layout gives each its own "content", and the
// map is what keeps them apart.
func loadPages() (
	map[string]*template.Template,
	error,
) {

	pages := make(
		map[string]*template.Template,
		len(pageFiles),
	)

	for _, file := range pageFiles {

		set, err := template.New("storefront").
			Funcs(templateFuncs).
			ParseFS(
				assets,
				"templates/"+layoutFile,
				"templates/"+file,
			)

		if err != nil {

			return nil, fmt.Errorf(
				"could not parse %s: %w",
				file,
				err,
			)
		}

		pages[file] = set
	}

	return pages, nil
}

// render writes one page.
//
// The status is passed in rather than assumed, because a
// page that reports a problem is still a page and should
// carry the header, the search box and the styling of
// every other page.
func (s *Site) render(
	w http.ResponseWriter,
	status int,
	page string,
	data any,
) {

	set, exists := s.pages[page]

	if !exists {

		// A page name that was never parsed is a mistake
		// in this package, not something a visitor did,
		// so it is answered plainly rather than dressed
		// up as a page.
		http.Error(
			w,
			"template not found: "+page,
			http.StatusInternalServerError,
		)

		return
	}

	w.Header().Set(
		"Content-Type",
		"text/html; charset=utf-8",
	)

	w.WriteHeader(status)

	err := set.ExecuteTemplate(w, layoutFile, data)

	if err != nil {

		// The status line and part of the page have
		// already gone out by now, so this cannot be
		// turned into an error page. Logging it is the
		// only honest thing left, and it is enough,
		// because a template that fails here fails on
		// every request rather than occasionally.
		log.Printf("render %s: %v", page, err)
	}
}

// fail renders the error page.
//
// It is a method rather than a bare http.Error so that a
// dead end still looks like part of the shop: a visitor
// who lands on one can search or go back to the catalog
// instead of being handed a line of plain text.
func (s *Site) fail(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	message string,
) {

	data := s.baseFor(
		r,
		http.StatusText(status),
		"",
	)

	data.Error = message

	s.render(w, status, errorFile, data)
}

// baseFor gathers what every page's header needs.
//
// The signed-in user is looked up on every page, including
// the public ones, because a page has to know who is
// reading it before it can decide whether to offer the
// seller's own controls or a sign-in link. That lookup is
// the same CurrentUserOrNil the JSON endpoints use, so a
// signed-out visitor costs nothing but an error path.
func (s *Site) baseFor(
	r *http.Request,
	title string,
	nav string,
) base {

	user := handlers.CurrentUserOrNil(s.pool, r)

	return base{
		Title: title,

		SignedIn: user.ID > 0,

		User: user,

		IsAdmin: user.Role == models.RoleAdmin,

		Query: strings.TrimSpace(
			r.URL.Query().Get("q"),
		),

		Nav: nav,
	}
}

// pathID reads a whole number out of the URL path.
//
// It mirrors the helper of the same name in the handlers
// package, which is unexported and so cannot be called
// from here. Only the HTTP-shaped part is repeated, and
// it is repeated deliberately: the question of what the
// number is allowed to mean is answered by storage and
// models, which are called rather than copied.
func pathID(r *http.Request, name string) (int, bool) {

	number, err := strconv.Atoi(
		strings.TrimSpace(
			r.PathValue(name),
		),
	)

	if err != nil || number < 1 {
		return 0, false
	}

	return number, true
}

// queryInt reads a whole number from the query string,
// falling back to a default when it is missing or
// unreadable.
//
// Query strings are typed and edited by hand, so an
// unusable value falls back rather than failing the
// request. Somebody who mistypes a category still
// deserves to see the shop.
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
