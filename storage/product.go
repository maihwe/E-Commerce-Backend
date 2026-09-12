package storage

import (
	"context"
	"fmt"
	"strings"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// productColumns is the SELECT list shared by every
// product query, so that a product always comes back
// with the same shape no matter which endpoint asked
// for it.
//
// Three values here are worth explaining, because none
// of them exist as columns in the products table:
//
//   - stock_available is the sum of the inventory
//     movements ledger. This is the same idea as the
//     Financial Tracker, where the balance is never
//     stored but always recalculated from the
//     transactions.
//   - average_rating and review_count are recalculated
//     from the reviews table, so a rating can never
//     drift away from the reviews that produced it.
const productColumns = `
	p.id,
	p.seller_id,
	p.category_id,
	p.name,
	p.slug,
	p.description,
	p.image_path,
	p.price,
	p.currency,
	p.is_active,
	COALESCE(
		(
			SELECT SUM(m.quantity_change)
			FROM inventory_movements m
			WHERE m.product_id = p.id
		),
		0
	) AS stock_available,
	COALESCE(
		(
			SELECT AVG(r.rating)
			FROM reviews r
			WHERE r.product_id = p.id
		),
		0
	) AS average_rating,
	(
		SELECT COUNT(*)
		FROM reviews r
		WHERE r.product_id = p.id
	) AS review_count,
	p.created_at,
	p.updated_at
`

// rowScanner is satisfied by both pgx.Row and pgx.Rows,
// which lets one scan helper serve single-row and
// multi-row queries alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanProduct reads the columns listed in productColumns
// into a Product.
func scanProduct(scanner rowScanner) (models.Product, error) {

	var product models.Product

	err := scanner.Scan(
		&product.ID,
		&product.SellerID,
		&product.CategoryID,
		&product.Name,
		&product.Slug,
		&product.Description,
		&product.ImagePath,
		&product.Price,
		&product.Currency,
		&product.IsActive,
		&product.StockAvailable,
		&product.AverageRating,
		&product.ReviewCount,
		&product.CreatedAt,
		&product.UpdatedAt,
	)

	if err != nil {
		return models.Product{}, err
	}

	return product, nil
}

// CreateProductInDB saves a new listing.
//
// A brand new product has no inventory movements yet,
// so its stock is zero and it has no reviews. That is
// exactly what the zero values below say, which is why
// this insert does not need a second query to read
// them back.
//
// image_path is read back but never written here. A
// listing is created without a picture and the picture
// scanner attaches one afterwards, so the column takes
// its default and the RETURNING list picks that default
// up. It is in the list because both of the statements
// that return a whole product must return the same
// columns: a product that came from a write and a
// product that came from a read are the same type, and
// a field that one of them filled in and the other left
// at zero would be a difference nobody could see from
// the outside until it mattered.
func CreateProductInDB(
	pool *pgxpool.Pool,
	product models.Product,
) (models.Product, error) {

	err := pool.QueryRow(
		context.Background(),
		`
		INSERT INTO products
			(
				seller_id,
				category_id,
				name,
				slug,
				description,
				price,
				currency,
				is_active
			)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING
			id,
			seller_id,
			category_id,
			name,
			slug,
			description,
			image_path,
			price,
			currency,
			is_active,
			created_at,
			updated_at
		`,
		product.SellerID,
		product.CategoryID,
		product.Name,
		product.Slug,
		product.Description,
		product.Price,
		product.Currency,
		product.IsActive,
	).Scan(
		&product.ID,
		&product.SellerID,
		&product.CategoryID,
		&product.Name,
		&product.Slug,
		&product.Description,
		&product.ImagePath,
		&product.Price,
		&product.Currency,
		&product.IsActive,
		&product.CreatedAt,
		&product.UpdatedAt,
	)

	if err != nil {
		return models.Product{}, err
	}

	product.StockAvailable = 0
	product.AverageRating = 0
	product.ReviewCount = 0

	return product, nil
}

// GetProductByIDFromDB finds one product.
//
// Inactive products are returned too. The caller
// decides whether a hidden listing is allowed to be
// seen, because a seller must still be able to open
// their own hidden product.
func GetProductByIDFromDB(
	pool *pgxpool.Pool,
	id int,
) (models.Product, error) {

	row := pool.QueryRow(
		context.Background(),
		`
		SELECT `+productColumns+`
		FROM products p
		WHERE p.id = $1
		`,
		id,
	)

	return scanProduct(row)
}

// GetProductBySlugFromDB finds one product by its slug.
//
// The slug is unique in the products table, so this
// returns at most one row, and a slug that matches
// nothing comes back as pgx.ErrNoRows for the caller to
// tell apart from a real failure with errors.Is.
//
// It exists for the picture scanner, which is handed a
// file rather than an id and has only the file's name to
// go on. Looking a product up by its slug is what lets a
// photograph called "cast-iron-pot.jpg" find the listing
// it belongs to without anybody writing down a number.
//
// A slug is not a secret and is not treated as one: it
// is a name that appears in every product URL. What
// makes the lookup safe is that it goes through
// products.slug, which the database will not let hold
// the same value twice, so there is no question of a
// picture landing on the wrong product because two
// listings share a name.
func GetProductBySlugFromDB(
	pool *pgxpool.Pool,
	slug string,
) (models.Product, error) {

	row := pool.QueryRow(
		context.Background(),
		`
		SELECT `+productColumns+`
		FROM products p
		WHERE p.slug = $1
		`,
		slug,
	)

	return scanProduct(row)
}

// productSortOptions maps a client's sort key onto a
// fixed SQL fragment.
//
// The client's text never reaches the SQL string. It is
// only ever used to look up one of these values, which
// is what keeps ORDER BY safe from injection.
var productSortOptions = map[string]string{

	"newest": "p.created_at DESC, p.id DESC",

	"oldest": "p.created_at ASC, p.id ASC",

	"price_asc": "p.price ASC, p.id ASC",

	"price_desc": "p.price DESC, p.id ASC",

	"name": "p.name ASC, p.id ASC",

	"rating": "average_rating DESC, p.id ASC",
}

// buildProductFilter turns a ProductFilter into a WHERE
// clause plus the list of arguments it needs.
//
// Every value is passed as a numbered placeholder. The
// client's search text is never pasted into the SQL
// string, so a search for something like
//
//	'; DROP TABLE products; --
//
// is treated as ordinary text to look for.
func buildProductFilter(
	filter models.ProductFilter,
) (string, []any) {

	conditions := make([]string, 0)

	args := make([]any, 0)

	// Hidden products are invisible unless the caller
	// asked for them, which only a seller or admin
	// should ever do.
	if !filter.IncludeInactive {
		conditions = append(
			conditions,
			"p.is_active = TRUE",
		)
	}

	// Search the name and the description.
	if filter.Query != "" {

		args = append(
			args,
			"%"+filter.Query+"%",
		)

		placeholder := fmt.Sprintf("$%d", len(args))

		conditions = append(
			conditions,
			fmt.Sprintf(
				"(p.name ILIKE %s OR p.description ILIKE %s)",
				placeholder,
				placeholder,
			),
		)
	}

	if filter.CategoryID > 0 {

		args = append(args, filter.CategoryID)

		conditions = append(
			conditions,
			fmt.Sprintf("p.category_id = $%d", len(args)),
		)
	}

	if filter.CategorySlug != "" {

		args = append(args, filter.CategorySlug)

		conditions = append(
			conditions,
			fmt.Sprintf(
				"p.category_id = (SELECT id FROM categories WHERE slug = $%d)",
				len(args),
			),
		)
	}

	if filter.SellerID > 0 {

		args = append(args, filter.SellerID)

		conditions = append(
			conditions,
			fmt.Sprintf("p.seller_id = $%d", len(args)),
		)
	}

	if filter.MinPrice != nil {

		args = append(args, *filter.MinPrice)

		conditions = append(
			conditions,
			fmt.Sprintf("p.price >= $%d", len(args)),
		)
	}

	if filter.MaxPrice != nil {

		args = append(args, *filter.MaxPrice)

		conditions = append(
			conditions,
			fmt.Sprintf("p.price <= $%d", len(args)),
		)
	}

	if len(conditions) == 0 {
		return "", args
	}

	return "WHERE " + strings.Join(conditions, " AND "), args
}

// ListProductsFromDB searches the catalog.
//
// It returns the products for the requested page and the
// total number of products that matched, so the caller
// can build a pagination envelope.
func ListProductsFromDB(
	pool *pgxpool.Pool,
	filter models.ProductFilter,
) ([]models.Product, int, error) {

	whereClause, args := buildProductFilter(filter)

	// Count how many products match before paging,
	// because the count must ignore LIMIT and OFFSET.
	var total int

	err := pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM products p `+whereClause,
		args...,
	).Scan(&total)

	if err != nil {
		return nil, 0, err
	}

	// Fall back to the default ordering when the
	// client sent something we do not recognise.
	orderBy, exists := productSortOptions[filter.Sort]

	if !exists {
		orderBy = productSortOptions["newest"]
	}

	// Add the page window. The two new placeholders
	// continue the numbering started by the filters.
	pageArgs := append(
		append([]any{}, args...),
		filter.Limit,
		filter.Offset,
	)

	query := fmt.Sprintf(
		`
		SELECT %s
		FROM products p
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
		`,
		productColumns,
		whereClause,
		orderBy,
		len(args)+1,
		len(args)+2,
	)

	rows, err := pool.Query(
		context.Background(),
		query,
		pageArgs...,
	)

	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	products := make([]models.Product, 0)

	for rows.Next() {

		product, err := scanProduct(rows)

		if err != nil {
			return nil, 0, err
		}

		products = append(products, product)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return products, total, nil
}

// UpdateProductInDB edits a listing that belongs to one
// specific seller.
//
// The seller_id in the WHERE clause is the safety net:
// if the product belongs to somebody else, no row
// matches and the update changes nothing.
//
// The image is deliberately not in the SET list. A
// seller editing a name or a price has not touched the
// picture, and an edit that happened to carry a stale
// copy of the product would otherwise be able to clear
// it. The column belongs to the picture scanner, and
// this statement leaves it exactly as it found it.
func UpdateProductInDB(
	pool *pgxpool.Pool,
	productID int,
	sellerID int,
	product models.Product,
) (models.Product, error) {

	err := pool.QueryRow(
		context.Background(),
		`
		UPDATE products
		SET
			category_id = $1,
			name = $2,
			slug = $3,
			description = $4,
			price = $5,
			currency = $6,
			is_active = $7,
			updated_at = NOW()
		WHERE id = $8
		AND seller_id = $9
		RETURNING
			id,
			seller_id,
			category_id,
			name,
			slug,
			description,
			image_path,
			price,
			currency,
			is_active,
			created_at,
			updated_at
		`,
		product.CategoryID,
		product.Name,
		product.Slug,
		product.Description,
		product.Price,
		product.Currency,
		product.IsActive,
		productID,
		sellerID,
	).Scan(
		&product.ID,
		&product.SellerID,
		&product.CategoryID,
		&product.Name,
		&product.Slug,
		&product.Description,
		&product.ImagePath,
		&product.Price,
		&product.Currency,
		&product.IsActive,
		&product.CreatedAt,
		&product.UpdatedAt,
	)

	if err != nil {
		return models.Product{}, err
	}

	// Read the derived values back so the caller
	// gets the same shape as any other product.
	return GetProductByIDFromDB(pool, product.ID)
}

// SetProductCategoryInDB moves one product into a
// category, and changes nothing else about it.
//
// It is a statement of its own rather than a call to
// UpdateProductInDB, and that is the whole reason it
// exists. UpdateProductInDB takes a complete product and
// writes every column of one, so handing it a structure
// holding only a category id would set the name, the
// price and the slug to their zero values and blank the
// listing. The picture classifier knows exactly one thing
// about a product -- which category its photograph looks
// like -- and this is a statement that can only write
// that one thing.
//
// Like SetProductImageInDB it reports whether anything
// changed, so that a photograph which agrees with the
// category the seller already chose costs no write and
// does not move updated_at.
//
// IS DISTINCT FROM is used rather than <> for the same
// reason it is used there: it treats NULL as an ordinary
// value, and a comparison against a column that could be
// NULL is exactly where the ordinary comparison quietly
// answers "not different".
func SetProductCategoryInDB(
	pool *pgxpool.Pool,
	productID int,
	categoryID int,
) (bool, error) {

	tag, err := pool.Exec(
		context.Background(),
		`
		UPDATE products
		SET
			category_id = $1,
			updated_at = NOW()
		WHERE id = $2
		AND category_id IS DISTINCT FROM $1
		`,
		categoryID,
		productID,
	)

	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil
}
