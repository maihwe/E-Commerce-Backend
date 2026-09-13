package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"
)

// registerCatalogPages wires browsing the catalog and
// looking at one product.
//
// Both are public. A shopper must be able to see what is
// for sale without an account, which is the same rule the
// JSON endpoints follow, and the same reason those two
// have no requireUser in front of them.
func registerCatalogPages(
	mux *http.ServeMux,
	site *Site,
) {

	mux.HandleFunc(
		"GET /shop",
		site.catalogPage,
	)

	mux.HandleFunc(
		"GET /shop/products/{id}",
		site.productPage,
	)
}

// catalogPage is the shop front: a search, a category
// filter, a sort order, and one page of products.
type catalogPage struct {
	base

	// Page is the same envelope the JSON catalog returns,
	// so the total and the page count come from one place
	// rather than being counted a second time here.
	Page utils.Page[models.Product]

	Categories []models.Category

	CategoryID int

	Sort string
}

// catalogPage renders the shop front.
func (s *Site) catalogPage(
	w http.ResponseWriter,
	r *http.Request,
) {

	pagination := utils.ParsePagination(r)

	// Only the filters this page offers are read. The API
	// accepts price bounds and a seller as well, and those
	// stay reachable through the JSON endpoints; adding
	// them here would mean building a filter panel that
	// does not fit a phone screen.
	filter := models.ProductFilter{
		Query: strings.TrimSpace(
			r.URL.Query().Get("q"),
		),

		CategoryID: queryInt(r, "category", 0),

		Sort: strings.TrimSpace(
			r.URL.Query().Get("sort"),
		),

		Limit: pagination.PerPage,

		Offset: pagination.Offset(),
	}

	products, total, err := storage.ListProductsFromDB(
		s.pool,
		filter,
	)

	if err != nil {

		s.fail(
			w,
			r,
			http.StatusInternalServerError,
			"The catalog could not be loaded.",
		)

		return
	}

	// The categories are a filter on this page rather than
	// part of the header, so they are read here and not on
	// every other page, where the list would be a query
	// in service of nothing.
	categories, err := storage.ListCategoriesFromDB(s.pool)

	if err != nil {
		categories = nil
	}

	data := catalogPage{
		base: s.baseFor(r, "", "shop"),

		Page: utils.NewPage(
			products,
			pagination,
			total,
		),

		Categories: categories,

		CategoryID: filter.CategoryID,

		Sort: filter.Sort,
	}

	s.render(w, http.StatusOK, catalogFile, data)
}

// CategoryName turns a category id into the label a
// shopper would recognise.
//
// It exists so a template can reach from a product to its
// category's name without the page having to carry a
// second copy of every product, pre-resolved. The list is
// the one this page already loaded for the filter chips,
// and it holds every category, so a product's category is
// always in it.
//
// An id that is not found gives an empty string rather
// than an error. The only thing the caller does with the
// answer is choose a picture, and not choosing one is a
// perfectly good outcome.
func (p catalogPage) CategoryName(id int) string {

	for _, category := range p.Categories {

		if category.ID == id {
			return category.Name
		}
	}

	return ""
}

// heroSlides is how many pictures the hero band cycles
// through at most.
//
// The number is a fixed, small one rather than being
// "however many products happen to have a picture", and
// that is a limitation of doing this without JavaScript
// rather than a decision about the catalog.
//
// A pure-CSS crossfade needs one keyframe rule per number
// of slides, because the point in the cycle at which each
// picture gives way to the next is written as a
// percentage of the whole cycle, and a percentage cannot
// be calculated from how many elements are on the page.
// Percentages in a keyframe selector have to be literal.
// So the stylesheet carries a rule for two slides, one
// for three and one for four, and this is the largest of
// them. See the hero rules in static/app.css, and note
// that raising this number without adding a rule there
// does not break the page: the band stops cycling and
// shows one still picture instead.
//
// Four is also about the right size for a band that has
// to sell. A hero cycling twelve photographs is a
// slideshow, and a shopper scrolls past a slideshow.
const heroSlides = 4

// HeroSlides are the pictures the hero band rotates
// through, in the order it shows them.
//
// They are drawn from the products already on this page,
// which means the band shows what the shop actually has
// rather than a fixed set of decorations that a listing
// could quietly stop matching. It also means the band
// follows the sort the shopper chose: on the default
// ordering the newest four listings appear at the top of
// the front page.
//
// A search returns nothing, for the same reason the
// single photograph is dropped on a search. The pictures
// are of specific products, and three of them with
// nothing to do with what was asked for is worse than no
// picture at all.
//
// Fewer than two pictures is nothing as well. One
// picture cannot cycle, and a band that is not cycling
// should be the band that was designed for standing
// still.
func (p catalogPage) HeroSlides() []string {

	if p.Query != "" {
		return nil
	}

	slides := make([]string, 0, heroSlides)

	for _, product := range p.Page.Data {

		if product.ImagePath == "" {
			continue
		}

		slides = append(slides, product.ImagePath)

		if len(slides) == heroSlides {
			break
		}
	}

	if len(slides) < 2 {
		return nil
	}

	return slides
}

// filterQuery is the search and category part of a catalog
// link, without a page number.
//
// Every link on the page is built from this, so that
// changing the sort or turning a page keeps the search
// that produced the results. Dropping the search on the
// second page is the classic way a catalog loses somebody.
func (p catalogPage) filterQuery() url.Values {

	values := url.Values{}

	if p.Query != "" {
		values.Set("q", p.Query)
	}

	if p.CategoryID > 0 {
		values.Set(
			"category",
			strconv.Itoa(p.CategoryID),
		)
	}

	return values
}

// CategoryURL is a link to one category, keeping the
// current search.
//
// Category zero means every category, which is what the
// "All" button links to.
func (p catalogPage) CategoryURL(categoryID int) string {

	values := p.filterQuery()

	if categoryID > 0 {

		values.Set(
			"category",
			strconv.Itoa(categoryID),
		)
	}

	// A new filter starts at the first page. Staying on
	// page four of a narrower result would usually land on
	// nothing at all.
	return "/shop?" + values.Encode()
}

// SortURL is a link to one ordering, keeping the current
// search and category.
func (p catalogPage) SortURL(sort string) string {

	values := p.filterQuery()

	if sort != "" {
		values.Set("sort", sort)
	}

	return "/shop?" + values.Encode()
}

// PageURL is a link to one page of the current results.
func (p catalogPage) PageURL(page int) string {

	values := p.filterQuery()

	values.Set(
		"page",
		strconv.Itoa(page),
	)

	return "/shop?" + values.Encode()
}

// HasPrev and HasNext decide whether the pager draws its
// two buttons, so the template does not have to work out
// where the edges are.
func (p catalogPage) HasPrev() bool {
	return p.Page.Page > 1
}

func (p catalogPage) HasNext() bool {
	return p.Page.Page < p.Page.TotalPages
}

// PrevURL and NextURL are the pager's two links.
//
// They are methods rather than arithmetic written into the
// markup, because "the page before this one" spelled out
// as a template expression is hard to read and is not
// checked by the compiler.
func (p catalogPage) PrevURL() string {
	return p.PageURL(p.Page.Page - 1)
}

func (p catalogPage) NextURL() string {
	return p.PageURL(p.Page.Page + 1)
}

// relatedLimit is how many other products the strip
// underneath a listing offers.
//
// Four fills two rows on a phone and one row and a third
// on a wide screen. It is a handful rather than a page
// because the strip is a shelf to glance along, not a
// second catalog: a shopper who wants the whole category
// has the link to it in the breadcrumb above, which is
// the same link the heading names.
const relatedLimit = 4

// productPage is one listing.
type productPage struct {
	base

	Product models.Product

	Category models.Category

	Seller models.User

	// OwnsProduct is true when the seller who listed this
	// product is the person looking at it, which is what
	// lets them see a listing they have withdrawn.
	OwnsProduct bool

	InStock bool

	// Related is a few other products from the same
	// category, offered underneath this one.
	//
	// It is the answer to the question a shopper asks
	// once they have read a listing: what else is there
	// like this. A catalog that is sorted into categories
	// can answer that without being asked, which is the
	// whole reason the categories are worth having.
	Related []models.Product
}

// RelatedTitle is the heading over the strip.
//
// It is written here rather than assembled in the markup
// because one of its two cases is a sentence rather than
// a value, and a template choosing between a category
// name and a phrase is a template that reads as neither.
//
// The fallback is for a category that could not be read.
// That is close to impossible -- the product carries a
// category id the database will not let dangle -- but a
// heading that says "More in" and stops would be worse
// than one that does not name the category at all. It
// deliberately does not promise the strip is the whole
// category, because it is four products and not the
// shelf.
func (p productPage) RelatedTitle() string {

	if p.Category.Name == "" {
		return "More to look at"
	}

	return "More in " + p.Category.Name
}

// productPage renders one listing.
func (s *Site) productPage(
	w http.ResponseWriter,
	r *http.Request,
) {

	productID, ok := pathID(r, "id")

	if !ok {

		s.fail(
			w,
			r,
			http.StatusNotFound,
			"There is no product with that number.",
		)

		return
	}

	product, err := storage.GetProductByIDFromDB(
		s.pool,
		productID,
	)

	if err != nil {

		s.fail(
			w,
			r,
			http.StatusNotFound,
			"There is no product with that number.",
		)

		return
	}

	data := productPage{
		base: s.baseFor(r, product.Name, "shop"),

		Product: product,

		OwnsProduct: false,
	}

	data.OwnsProduct = data.SignedIn &&
		data.User.ID == product.SellerID

	// A withdrawn listing is invisible to a shopper but
	// stays reachable by the seller who owns it and by an
	// admin, which is exactly the rule GetProductHandler
	// applies. The two are written separately because one
	// answers with JSON and the other with a page, but
	// they agree on who may look.
	if !product.IsActive &&
		!data.OwnsProduct &&
		!data.IsAdmin {

		s.fail(
			w,
			r,
			http.StatusNotFound,
			"There is no product with that number.",
		)

		return
	}

	// A missing category is not worth failing the page
	// over. The product is the reason the visitor came,
	// and the category is a breadcrumb above it.
	category, err := storage.GetCategoryByIDFromDB(
		s.pool,
		product.CategoryID,
	)

	if err == nil {
		data.Category = category
	}

	// The seller's name is worth the second query: a
	// marketplace where a listing has no visible seller
	// reads as a vending machine rather than a market.
	seller, err := storage.GetUserByIDFromDB(
		s.pool,
		product.SellerID,
	)

	if err == nil {
		data.Seller = seller
	}

	data.InStock = product.StockAvailable > 0

	// The strip underneath. It is drawn from this
	// product's own category rather than from
	// GetRecommendationsFromDB, which would prefer
	// whatever people bought alongside it. Two reasons,
	// and both are about the strip being a way into a
	// shelf rather than a row of loose suggestions: the
	// heading can name the category, because there is
	// only one, and every tile in it is drawn for the
	// same category, so the strip cannot show a pot where
	// a phone belongs.
	//
	// A failure here is not worth failing the page over.
	// The product is what the visitor came for; this is a
	// strip below it. An empty list draws nothing at all.
	related, err := storage.ListTopRatedInCategoryFromDB(
		s.pool,
		product.CategoryID,
		productID,
		relatedLimit,
	)

	if err == nil {
		data.Related = related
	}

	s.render(w, http.StatusOK, productFile, data)
}
