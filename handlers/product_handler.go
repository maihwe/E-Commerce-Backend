package handlers

import (
	"net/http"
	"strings"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// productRequest is the body accepted when a seller
// creates or edits a listing.
//
// The seller cannot set stock here. Stock only ever
// changes by appending to the inventory ledger, and the
// only way to add stock is the restock endpoint. Letting
// a listing edit set a stock number would give the
// project a second, contradictory source of truth.
type productRequest struct {

	CategoryID int `json:"category_id"`

	Name string `json:"name"`

	Description string `json:"description"`

	Price float64 `json:"price"`

	Currency string `json:"currency"`

	// IsActive is a pointer so that "not mentioned"
	// can be told apart from "set to false". A seller
	// editing a description should not accidentally
	// publish a product they had hidden.
	IsActive *bool `json:"is_active"`
}

// ListProductsHandler searches the catalog.
//
// This endpoint is public. A shopper must be able to
// browse without an account.
func ListProductsHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		minPrice, err := queryFloat(r, "min_price")

		if err != nil {

			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

			return
		}

		maxPrice, err := queryFloat(r, "max_price")

		if err != nil {

			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

			return
		}

		// A range that cannot contain anything is
		// almost always a client bug, so it is worth
		// saying so rather than returning an empty
		// list that looks like "no products match".
		if minPrice != nil &&
			maxPrice != nil &&
			*minPrice > *maxPrice {

			writeError(
				w,
				http.StatusBadRequest,
				"min_price cannot be greater than max_price",
			)

			return
		}

		pagination := utils.ParsePagination(r)

		filter := models.ProductFilter{
			Query: strings.TrimSpace(
				r.URL.Query().Get("q"),
			),

			CategoryID: queryInt(
				r,
				"category",
				0,
			),

			CategorySlug: strings.TrimSpace(
				r.URL.Query().Get("category_slug"),
			),

			SellerID: queryInt(
				r,
				"seller",
				0,
			),

			MinPrice: minPrice,

			MaxPrice: maxPrice,

			Sort: strings.TrimSpace(
				r.URL.Query().Get("sort"),
			),

			Limit: pagination.PerPage,

			Offset: pagination.Offset(),
		}

		// Hidden listings are only visible to the
		// seller who owns them, or to an admin. The
		// query parameter is honoured only after that
		// has been established, so a shopper cannot
		// ask to see withdrawn products.
		if r.URL.Query().Get("include_inactive") ==
			"true" {

			user, ok := requireUser(pool, w, r)

			if !ok {
				return
			}

			if user.Role != models.RoleAdmin {

				filter.SellerID = user.ID
			}

			filter.IncludeInactive = true
		}

		products, total, err :=
			storage.ListProductsFromDB(pool, filter)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load the catalog",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			utils.NewPage(
				products,
				pagination,
				total,
			),
		)
	}
}

// GetProductHandler returns one product.
//
// A hidden product answers 404 to a shopper, but is still
// returned to the seller who owns it and to an admin,
// because a seller must be able to look at a listing they
// have taken down.
func GetProductHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		product, err := storage.GetProductByIDFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		if !product.IsActive {

			user := CurrentUserOrNil(pool, r)

			isOwner := user.ID == product.SellerID

			isAdmin := user.Role == models.RoleAdmin

			if !isOwner && !isAdmin {

				writeError(
					w,
					http.StatusNotFound,
					"Product not found",
				)

				return
			}
		}

		writeJSON(w, http.StatusOK, product)
	}
}

// CreateProductHandler lists a new product.
//
// Only a seller or an admin may call this.
func CreateProductHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		if user.Role != models.RoleSeller &&
			user.Role != models.RoleAdmin {

			writeError(
				w,
				http.StatusForbidden,
				"Only a seller can list products",
			)

			return
		}

		var request productRequest

		if !decodeJSONBody(w, r, &request) {
			return
		}

		product, rejection :=
			buildProduct(request)

		if rejection != "" {

			writeError(
				w,
				http.StatusBadRequest,
				rejection,
			)

			return
		}

		product.SellerID = user.ID

		created, err := storage.CreateProductInDB(
			pool,
			product,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not create the product: "+
					err.Error(),
			)

			return
		}

		writeJSON(w, http.StatusCreated, created)
	}
}

// UpdateProductHandler edits a listing.
//
// The seller_id guard lives in the SQL, so a seller
// editing somebody else's product updates no rows and is
// told the product was not found. That is deliberate: a
// 403 would confirm the product exists, and a seller has
// no business knowing that.
func UpdateProductHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		existing, err := storage.GetProductByIDFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		// An admin may correct any listing; a seller
		// may only touch their own.
		if user.Role != models.RoleAdmin &&
			existing.SellerID != user.ID {

			writeError(
				w,
				http.StatusForbidden,
				"You can only edit your own products",
			)

			return
		}

		var request productRequest

		if !decodeJSONBody(w, r, &request) {
			return
		}

		product, rejection :=
			buildProduct(request)

		if rejection != "" {

			writeError(
				w,
				http.StatusBadRequest,
				rejection,
			)

			return
		}

		// An edit that does not mention is_active keeps
		// whatever the product already had.
		if request.IsActive == nil {
			product.IsActive = existing.IsActive
		}

		updated, err := storage.UpdateProductInDB(
			pool,
			productID,
			existing.SellerID,
			product,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not update the product: "+
					err.Error(),
			)

			return
		}

		writeJSON(w, http.StatusOK, updated)
	}
}

// DeleteProductHandler takes a listing down.
//
// It archives rather than deletes. Order lines point at
// products, and a past order must keep working: deleting
// the row would either break the reference or require the
// order history to be rewritten, and neither is
// acceptable. So the product disappears from the catalog
// while the record of it stays.
func DeleteProductHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		existing, err := storage.GetProductByIDFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		if user.Role != models.RoleAdmin &&
			existing.SellerID != user.ID {

			writeError(
				w,
				http.StatusForbidden,
				"You can only remove your own products",
			)

			return
		}

		existing.IsActive = false

		updated, err := storage.UpdateProductInDB(
			pool,
			productID,
			existing.SellerID,
			existing,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not remove the product",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"message": "Product removed from the catalog",

				"product": updated,
			},
		)
	}
}

// buildProduct validates a create or update request and
// turns it into a product ready to be stored.
//
// It returns an empty rejection string when the request
// is usable, matching the ValidateX convention used
// elsewhere in the project, so the handler turns a
// non-empty string into a 400.
func buildProduct(
	request productRequest,
) (models.Product, string) {

	name := strings.TrimSpace(request.Name)

	if name == "" {
		return models.Product{}, "Name is required"
	}

	if len(name) > 200 {

		return models.Product{},
			"Name cannot be longer than 200 characters"
	}

	if request.CategoryID < 1 {

		return models.Product{},
			"Category is required"
	}

	// Prices are held as decimal strings in the
	// database, so a value with more than two decimal
	// places would be silently rounded. Refusing it is
	// more honest than storing something different from
	// what the seller typed.
	if request.Price < 0 {

		return models.Product{},
			"Price cannot be negative"
	}

	if utils.RoundToTwoDecimals(request.Price) !=
		request.Price {

		return models.Product{},
			"Price cannot have more than two decimal places"
	}

	currency := strings.ToUpper(
		strings.TrimSpace(request.Currency),
	)

	if currency == "" {
		currency = "NGN"
	}

	if len(currency) != 3 {

		return models.Product{},
			"Currency must be a three-letter code"
	}

	isActive := true

	if request.IsActive != nil {
		isActive = *request.IsActive
	}

	product := models.Product{
		CategoryID: request.CategoryID,

		Name: name,

		// The slug comes from the name, so a product
		// always has a readable URL without the seller
		// having to think about it.
		Slug: utils.Slugify(name),

		Description: strings.TrimSpace(
			request.Description,
		),

		Price: request.Price,

		Currency: currency,

		IsActive: isActive,
	}

	return product, ""
}

// RestockProductHandler adds stock to a product.
//
// This appends a movement to the ledger. It never sets a
// number, because there is no number to set.
func RestockProductHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		product, err := storage.GetProductByIDFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		if user.Role != models.RoleAdmin &&
			product.SellerID != user.ID {

			writeError(
				w,
				http.StatusForbidden,
				"You can only restock your own products",
			)

			return
		}

		var request struct {
			Quantity int `json:"quantity"`

			// Reason lets a seller record a correction
			// rather than a delivery. It defaults to a
			// restock, which is the common case.
			Reason string `json:"reason"`
		}

		if !decodeJSONBody(w, r, &request) {
			return
		}

		if request.Quantity < 1 {

			writeError(
				w,
				http.StatusBadRequest,
				"Quantity must be at least 1",
			)

			return
		}

		reason := strings.TrimSpace(request.Reason)

		if reason == "" {
			reason = models.MovementRestock
		}

		// Only these two make sense from a seller
		// action. "sale", "refund", and "cancel" belong
		// to order handling and would corrupt the
		// ledger's meaning if a seller could write them
		// by hand.
		if reason != models.MovementRestock &&
			reason != models.MovementAdjustment {

			writeError(
				w,
				http.StatusBadRequest,
				"Reason must be restock or adjustment",
			)

			return
		}

		movement, err :=
			storage.AddInventoryMovementInDB(
				pool,
				models.InventoryMovement{
					ProductID: productID,

					QuantityChange: request.Quantity,

					Reason: reason,
				},
			)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not record the stock movement",
			)

			return
		}

		// Read the stock back from the ledger so the
		// seller sees the new total, still derived and
		// never stored.
		stock, err := storage.GetProductStockFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not read the stock level",
			)

			return
		}

		writeJSON(
			w,
			http.StatusCreated,
			map[string]any{
				"movement": movement,

				"stock_available": stock,
			},
		)
	}
}

// ListInventoryMovementsHandler returns the full stock
// history of one product.
//
// This is the audit trail the ledger exists to provide,
// so it is restricted to the seller who owns the product
// and to admins.
func ListInventoryMovementsHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		productID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		product, err := storage.GetProductByIDFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusNotFound,
				"Product not found",
			)

			return
		}

		if user.Role != models.RoleAdmin &&
			product.SellerID != user.ID {

			writeError(
				w,
				http.StatusForbidden,
				"You can only see the stock history of your own products",
			)

			return
		}

		movements, err :=
			storage.ListInventoryMovementsFromDB(
				pool,
				productID,
			)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load the stock history",
			)

			return
		}

		stock, err := storage.GetProductStockFromDB(
			pool,
			productID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not read the stock level",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"product_id": productID,

				"stock_available": stock,

				"movements": movements,
			},
		)
	}
}
