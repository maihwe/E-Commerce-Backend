package storage

import (
	"context"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AddInventoryMovementInDB appends one row to the
// inventory ledger.
//
// This is the only way stock ever changes. Nothing in
// this project updates a stock number, because no stock
// number exists.
func AddInventoryMovementInDB(
	pool *pgxpool.Pool,
	movement models.InventoryMovement,
) (models.InventoryMovement, error) {

	return addInventoryMovementInTx(
		context.Background(),
		pool,
		movement,
	)
}

// addInventoryMovementInTx appends a movement using an
// existing transaction.
//
// Order confirmation needs to write movements as part of
// a larger transaction, so the work is separated out and
// both the pool and the transaction wrappers call it.
func addInventoryMovementInTx(
	ctx context.Context,
	querier dbQuerier,
	movement models.InventoryMovement,
) (models.InventoryMovement, error) {

	err := querier.QueryRow(
		ctx,
		`
		INSERT INTO inventory_movements
			(
				product_id,
				quantity_change,
				reason,
				order_id
			)
		VALUES
			($1, $2, $3, $4)
		RETURNING
			id,
			product_id,
			quantity_change,
			reason,
			order_id,
			created_at
		`,
		movement.ProductID,
		movement.QuantityChange,
		movement.Reason,
		movement.OrderID,
	).Scan(
		&movement.ID,
		&movement.ProductID,
		&movement.QuantityChange,
		&movement.Reason,
		&movement.OrderID,
		&movement.CreatedAt,
	)

	if err != nil {
		return models.InventoryMovement{}, err
	}

	return movement, nil
}

// GetProductStockFromDB works out how much of a product
// is available.
//
// The answer is the sum of the ledger, never a stored
// number. COALESCE turns the NULL that SUM returns for a
// product with no movements at all into a plain zero.
func GetProductStockFromDB(
	pool *pgxpool.Pool,
	productID int,
) (int, error) {

	return getProductStockInTx(
		context.Background(),
		pool,
		productID,
	)
}

// getProductStockInTx works out available stock using an
// existing transaction.
func getProductStockInTx(
	ctx context.Context,
	querier dbQuerier,
	productID int,
) (int, error) {

	var stock int

	err := querier.QueryRow(
		ctx,
		`
		SELECT COALESCE(SUM(quantity_change), 0)
		FROM inventory_movements
		WHERE product_id = $1
		`,
		productID,
	).Scan(&stock)

	if err != nil {
		return 0, err
	}

	return stock, nil
}

// ListInventoryMovementsFromDB returns the full ledger
// for one product, oldest first.
//
// Because the table is append-only, this is a complete
// audit trail: every restock, sale, refund, and
// correction that has ever touched this product.
func ListInventoryMovementsFromDB(
	pool *pgxpool.Pool,
	productID int,
) ([]models.InventoryMovement, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT
			id,
			product_id,
			quantity_change,
			reason,
			order_id,
			created_at
		FROM inventory_movements
		WHERE product_id = $1
		ORDER BY id
		`,
		productID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	movements := make([]models.InventoryMovement, 0)

	for rows.Next() {

		var movement models.InventoryMovement

		err := rows.Scan(
			&movement.ID,
			&movement.ProductID,
			&movement.QuantityChange,
			&movement.Reason,
			&movement.OrderID,
			&movement.CreatedAt,
		)

		if err != nil {
			return nil, err
		}

		movements = append(movements, movement)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return movements, nil
}
