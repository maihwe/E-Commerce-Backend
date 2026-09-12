package storage

import (
	"context"

	"e-commerce-backend/models"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GetOrCreateCartInDB returns the buyer's cart, creating
// one the first time it is needed.
//
// ON CONFLICT DO NOTHING makes this safe when two requests
// arrive together for a shopper who has never had a cart:
// one insert wins, the other does nothing, and both then
// find the same cart.
func GetOrCreateCartInDB(
	pool *pgxpool.Pool,
	userID int,
) (int, error) {

	ctx := context.Background()

	tag, err := pool.Exec(
		ctx,
		`
		INSERT INTO carts (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
		`,
		userID,
	)

	if err != nil {
		return 0, err
	}

	// If nothing was inserted the cart already existed,
	// which is not an error, so either way the cart is
	// read back below.
	_ = tag.RowsAffected()

	var cartID int

	err = pool.QueryRow(
		ctx,
		`SELECT id FROM carts WHERE user_id = $1`,
		userID,
	).Scan(&cartID)

	if err != nil {
		return 0, err
	}

	return cartID, nil
}

// AddCartItemInDB puts a product in the cart, or increases
// the quantity if it is already there.
//
// ON CONFLICT means a shopper who adds the same item twice
// gets three of it, rather than two separate rows that
// would each need their own handling at checkout.
func AddCartItemInDB(
	pool *pgxpool.Pool,
	cartID int,
	productID int,
	quantity int,
) error {

	ctx := context.Background()

	_, err := pool.Exec(
		ctx,
		`
		INSERT INTO cart_items (cart_id, product_id, quantity)
		VALUES ($1, $2, $3)
		ON CONFLICT (cart_id, product_id)
		DO UPDATE SET quantity = cart_items.quantity + EXCLUDED.quantity
		`,
		cartID,
		productID,
		quantity,
	)

	if err != nil {
		return err
	}

	return touchCartInDB(pool, cartID)
}

// SetCartItemQuantityInDB replaces the quantity of an item
// that is already in the cart.
func SetCartItemQuantityInDB(
	pool *pgxpool.Pool,
	cartID int,
	productID int,
	quantity int,
) (bool, error) {

	ctx := context.Background()

	tag, err := pool.Exec(
		ctx,
		`
		UPDATE cart_items
		SET quantity = $1
		WHERE cart_id = $2
		AND product_id = $3
		`,
		quantity,
		cartID,
		productID,
	)

	if err != nil {
		return false, err
	}

	if tag.RowsAffected() == 0 {
		return false, nil
	}

	return true, touchCartInDB(pool, cartID)
}

// RemoveCartItemInDB takes an item out of the cart.
func RemoveCartItemInDB(
	pool *pgxpool.Pool,
	cartID int,
	productID int,
) (bool, error) {

	ctx := context.Background()

	tag, err := pool.Exec(
		ctx,
		`
		DELETE FROM cart_items
		WHERE cart_id = $1
		AND product_id = $2
		`,
		cartID,
		productID,
	)

	if err != nil {
		return false, err
	}

	if tag.RowsAffected() == 0 {
		return false, nil
	}

	return true, touchCartInDB(pool, cartID)
}

// ClearCartInDB empties a cart.
func ClearCartInDB(
	pool *pgxpool.Pool,
	cartID int,
) error {

	ctx := context.Background()

	_, err := pool.Exec(
		ctx,
		`DELETE FROM cart_items WHERE cart_id = $1`,
		cartID,
	)

	if err != nil {
		return err
	}

	return touchCartInDB(pool, cartID)
}

// touchCartInDB records that the cart changed.
//
// The carts table keeps an updated_at so that abandoned
// carts can be found later, which is the kind of thing a
// reminder email would be built on.
func touchCartInDB(
	pool *pgxpool.Pool,
	cartID int,
) error {

	_, err := pool.Exec(
		context.Background(),
		`
		UPDATE carts
		SET updated_at = NOW()
		WHERE id = $1
		`,
		cartID,
	)

	return err
}

// GetCartFromDB reads a cart with its items.
//
// Each item is joined to its product and to the inventory
// ledger, so the response carries everything a cart page
// needs: the price, the line total, and how many are left.
// The subtotal is summed by PostgreSQL in one pass.
func GetCartFromDB(
	pool *pgxpool.Pool,
	cartID int,
) (models.Cart, error) {

	ctx := context.Background()

	var cart models.Cart

	err := pool.QueryRow(
		ctx,
		`
		SELECT id, user_id, created_at, updated_at
		FROM carts
		WHERE id = $1
		`,
		cartID,
	).Scan(
		&cart.ID,
		&cart.UserID,
		&cart.CreatedAt,
		&cart.UpdatedAt,
	)

	if err != nil {
		return models.Cart{}, err
	}

	rows, err := pool.Query(
		ctx,
		`
		SELECT
			ci.id,
			ci.cart_id,
			ci.product_id,
			ci.quantity,
			ci.added_at,
			`+productColumns+`
		FROM cart_items ci
		JOIN products p ON p.id = ci.product_id
		WHERE ci.cart_id = $1
		ORDER BY ci.added_at, ci.id
		`,
		cartID,
	)

	if err != nil {
		return models.Cart{}, err
	}

	defer rows.Close()

	cart.Items = make([]models.CartItem, 0)

	var subtotal float64

	itemCount := 0

	for rows.Next() {

		var item models.CartItem

		err := rows.Scan(
			&item.ID,
			&item.CartID,
			&item.ProductID,
			&item.Quantity,
			&item.AddedAt,
			&item.Product.ID,
			&item.Product.SellerID,
			&item.Product.CategoryID,
			&item.Product.Name,
			&item.Product.Slug,
			&item.Product.Description,
			&item.Product.Price,
			&item.Product.Currency,
			&item.Product.IsActive,
			&item.Product.StockAvailable,
			&item.Product.AverageRating,
			&item.Product.ReviewCount,
			&item.Product.CreatedAt,
			&item.Product.UpdatedAt,
		)

		if err != nil {
			return models.Cart{}, err
		}

		// The line total and subtotal are computed in
		// Go from the exact price, and the result is
		// rounded to two decimal places so the JSON is
		// clean.
		item.LineTotal = utils.RoundToTwoDecimals(
			item.Product.Price * float64(item.Quantity),
		)

		item.StockAvailable = item.Product.StockAvailable

		subtotal += item.LineTotal

		itemCount += item.Quantity

		cart.Items = append(cart.Items, item)
	}

	if err := rows.Err(); err != nil {
		return models.Cart{}, err
	}

	cart.Subtotal = utils.RoundToTwoDecimals(subtotal)

	cart.ItemCount = itemCount

	return cart, nil
}
