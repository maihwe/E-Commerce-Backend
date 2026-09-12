package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"e-commerce-backend/models"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// orderColumns is the SELECT list shared by every order
// query.
//
// The coupon code is joined in so that an order can show
// which code was used without the caller having to look
// the coupon up separately.
const orderColumns = `
	o.id,
	o.buyer_id,
	o.status,
	o.subtotal,
	o.discount_amount,
	o.total,
	o.coupon_id,
	COALESCE(c.code, '') AS coupon_code,
	o.payment_reference,
	o.paid_at,
	o.shipped_at,
	o.delivered_at,
	o.cancelled_at,
	o.refunded_at,
	o.created_at,
	o.updated_at
`

// scanOrder reads the columns listed in orderColumns
// into an Order.
func scanOrder(scanner rowScanner) (models.Order, error) {

	var order models.Order

	// payment_reference is NULL until payment is
	// started, so it is scanned through a pointer.
	var paymentReference *string

	err := scanner.Scan(
		&order.ID,
		&order.BuyerID,
		&order.Status,
		&order.Subtotal,
		&order.DiscountAmount,
		&order.Total,
		&order.CouponID,
		&order.CouponCode,
		&paymentReference,
		&order.PaidAt,
		&order.ShippedAt,
		&order.DeliveredAt,
		&order.CancelledAt,
		&order.RefundedAt,
		&order.CreatedAt,
		&order.UpdatedAt,
	)

	if err != nil {
		return models.Order{}, err
	}

	if paymentReference != nil {
		order.PaymentReference = *paymentReference
	}

	return order, nil
}

// itemColumns is the SELECT list for order items.
const itemColumns = `
	id,
	order_id,
	product_id,
	seller_id,
	product_name,
	unit_price,
	quantity,
	line_total
`

// scanOrderItem reads the columns listed in itemColumns.
func scanOrderItem(scanner rowScanner) (models.OrderItem, error) {

	var item models.OrderItem

	err := scanner.Scan(
		&item.ID,
		&item.OrderID,
		&item.ProductID,
		&item.SellerID,
		&item.ProductName,
		&item.UnitPrice,
		&item.Quantity,
		&item.LineTotal,
	)

	if err != nil {
		return models.OrderItem{}, err
	}

	return item, nil
}

// appendOrderEventInTx writes one row into an order's
// history.
//
// Nothing is ever updated or deleted here, so the events
// table is a complete record of how an order reached its
// current state.
func appendOrderEventInTx(
	ctx context.Context,
	querier dbQuerier,
	orderID int,
	fromStatus string,
	toStatus string,
	actorID *int,
	note string,
) error {

	_, err := querier.Exec(
		ctx,
		`
		INSERT INTO order_events
			(
				order_id,
				from_status,
				to_status,
				actor_id,
				note
			)
		VALUES
			($1, $2, $3, $4, $5)
		`,
		orderID,
		fromStatus,
		toStatus,
		actorID,
		note,
	)

	return err
}

// lockProductsInTx takes a row lock on a set of products.
//
// This is the mechanism that stops two people buying the
// last item at the same moment.
//
// Because stock is a sum over the inventory ledger rather
// than a single column, PostgreSQL cannot enforce "never
// below zero" with a CHECK constraint. The guarantee has
// to come from somewhere else, and it comes from here: any
// code that is about to change a product's stock locks the
// product row first, reads the ledger, and only then
// writes. Two such transactions touching the same product
// are serialised by the lock, so the second one sees the
// first one's movements and can correctly refuse.
//
// The ids are sorted before locking so that two
// transactions holding overlapping sets of products always
// take the locks in the same order. Locking in a
// consistent order is what prevents a deadlock, where each
// transaction waits forever for a lock the other holds.
//
// The rows are read through Query rather than Exec so that
// the statement is definitely driven to completion and the
// locks are definitely held.
func lockProductsInTx(
	ctx context.Context,
	querier dbQuerier,
	productIDs []int,
) error {

	if len(productIDs) == 0 {
		return nil
	}

	rows, err := querier.Query(
		ctx,
		`
		SELECT id
		FROM products
		WHERE id = ANY($1::int[])
		ORDER BY id
		FOR UPDATE
		`,
		productIDs,
	)

	if err != nil {
		return err
	}

	defer rows.Close()

	// Reading every row is what makes PostgreSQL
	// actually take the locks.
	for rows.Next() {

		var id int

		if err := rows.Scan(&id); err != nil {
			return err
		}
	}

	return rows.Err()
}

// cartLine is one row of a cart, joined to its product,
// as read during checkout.
type cartLine struct {
	ProductID int
	Quantity  int
	SellerID  int
	Name      string
	Price     float64
	IsActive  bool
}

// getCartLinesInTx reads the cart and locks the products
// in it.
//
// FOR UPDATE OF p locks only the products, not the cart
// rows, which is what we want: the cart itself is about to
// be emptied by this same transaction.
func getCartLinesInTx(
	ctx context.Context,
	querier dbQuerier,
	cartID int,
) ([]cartLine, error) {

	rows, err := querier.Query(
		ctx,
		`
		SELECT
			ci.product_id,
			ci.quantity,
			p.seller_id,
			p.name,
			p.price,
			p.is_active
		FROM cart_items ci
		JOIN products p ON p.id = ci.product_id
		WHERE ci.cart_id = $1
		ORDER BY p.id
		FOR UPDATE OF p
		`,
		cartID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	lines := make([]cartLine, 0)

	for rows.Next() {

		var line cartLine

		err := rows.Scan(
			&line.ProductID,
			&line.Quantity,
			&line.SellerID,
			&line.Name,
			&line.Price,
			&line.IsActive,
		)

		if err != nil {
			return nil, err
		}

		lines = append(lines, line)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// CreateOrderFromCartInDB turns a buyer's cart into a
// pending order.
//
// Everything happens inside one transaction, so a failure
// anywhere leaves the cart untouched and no half-made
// order behind.
//
// Note what does NOT happen here: stock is not reduced.
// Stock is only reduced when payment is confirmed, in
// ConfirmOrderPaymentInTx. This function checks that the
// stock is there, so a shopper is told early if it is not,
// but the check is not a reservation.
func CreateOrderFromCartInDB(
	pool *pgxpool.Pool,
	buyerID int,
	couponCode string,
) (models.Order, error) {

	ctx := context.Background()

	tx, err := pool.Begin(ctx)

	if err != nil {
		return models.Order{}, err
	}

	// Roll back unless the commit at the end is
	// reached. Rolling back a finished transaction is
	// harmless, so this is safe to always call.
	defer tx.Rollback(ctx)

	// Find the buyer's cart.
	var cartID int

	err = tx.QueryRow(
		ctx,
		`SELECT id FROM carts WHERE user_id = $1`,
		buyerID,
	).Scan(&cartID)

	if err != nil {
		return models.Order{}, ErrCartEmpty
	}

	// Read the cart and lock its products.
	lines, err := getCartLinesInTx(ctx, tx, cartID)

	if err != nil {
		return models.Order{}, err
	}

	if len(lines) == 0 {
		return models.Order{}, ErrCartEmpty
	}

	// The subtotal is built from integer kobo, so it is
	// exact no matter how many lines there are.
	var subtotalKobo int64

	for _, line := range lines {

		// A seller may have hidden a product since it
		// was added to the cart. Refusing is friendlier
		// than silently charging for something the
		// shopper can no longer see.
		if !line.IsActive {
			return models.Order{}, fmt.Errorf(
				"%w: %s",
				ErrProductUnavailable,
				line.Name,
			)
		}

		// Check the stock is there before taking the
		// order. The binding check happens again at
		// payment time, when the lock guarantees no
		// other transaction can slip in between.
		stock, err := getProductStockInTx(
			ctx,
			tx,
			line.ProductID,
		)

		if err != nil {
			return models.Order{}, err
		}

		if stock < line.Quantity {
			return models.Order{}, fmt.Errorf(
				"%w: %s has %d left",
				ErrInsufficientStock,
				line.Name,
				stock,
			)
		}

		subtotalKobo += utils.ToKobo(line.Price) *
			int64(line.Quantity)
	}

	// Work out the discount, if the shopper used a
	// coupon.
	var couponID *int

	discountKobo := int64(0)

	code := models.NormaliseCode(couponCode)

	if code != "" {

		coupon, err := loadCouponByCodeInTx(ctx, tx, code)

		if err != nil {
			return models.Order{}, err
		}

		// Every coupon rule is checked in one place, so
		// checkout and the discount preview endpoint can
		// never disagree.
		rejection := models.RejectCoupon(
			coupon,
			subtotalKobo,
			time.Now(),
		)

		if rejection != "" {
			return models.Order{}, fmt.Errorf(
				"%w: %s",
				ErrCouponRejected,
				rejection,
			)
		}

		discountKobo = coupon.ComputeDiscountKobo(
			subtotalKobo,
		)

		couponID = &coupon.ID
	}

	totalKobo := utils.RemoveDiscount(
		subtotalKobo,
		discountKobo,
	)

	// Insert the order.
	//
	// Amounts are sent as decimal strings and cast to
	// NUMERIC, so PostgreSQL stores exactly what we
	// calculated rather than a float that is very
	// slightly off.
	row := tx.QueryRow(
		ctx,
		`
		INSERT INTO orders
			(
				buyer_id,
				status,
				subtotal,
				discount_amount,
				total,
				coupon_id
			)
		VALUES
			($1, 'pending', $2::numeric, $3::numeric, $4::numeric, $5)
		RETURNING id
		`,
		buyerID,
		utils.FormatKobo(subtotalKobo),
		utils.FormatKobo(discountKobo),
		utils.FormatKobo(totalKobo),
		couponID,
	)

	var orderID int

	err = row.Scan(&orderID)

	if err != nil {
		return models.Order{}, err
	}

	// Copy the cart lines onto the order.
	//
	// The product name and price are copied rather than
	// referenced, so that renaming or repricing a
	// product later cannot rewrite what this order says
	// the buyer agreed to.
	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO order_items
			(
				order_id,
				product_id,
				seller_id,
				product_name,
				unit_price,
				quantity
			)
		SELECT
			$1,
			p.id,
			p.seller_id,
			p.name,
			p.price,
			ci.quantity
		FROM cart_items ci
		JOIN products p ON p.id = ci.product_id
		WHERE ci.cart_id = $2
		`,
		orderID,
		cartID,
	)

	if err != nil {
		return models.Order{}, err
	}

	// Record the order being created.
	err = appendOrderEventInTx(
		ctx,
		tx,
		orderID,
		"",
		models.OrderPending,
		&buyerID,
		"Order placed",
	)

	if err != nil {
		return models.Order{}, err
	}

	// Record the redemption. Counting these rows is how
	// a coupon's usage is known, so this is what makes
	// a usage limit work.
	if couponID != nil {

		_, err = tx.Exec(
			ctx,
			`
			INSERT INTO coupon_redemptions
				(coupon_id, order_id, discount_amount)
			VALUES ($1, $2, $3::numeric)
			`,
			*couponID,
			orderID,
			utils.FormatKobo(discountKobo),
		)

		if err != nil {
			return models.Order{}, err
		}
	}

	// Empty the cart now that its contents are on the
	// order.
	_, err = tx.Exec(
		ctx,
		`DELETE FROM cart_items WHERE cart_id = $1`,
		cartID,
	)

	if err != nil {
		return models.Order{}, err
	}

	err = tx.Commit(ctx)

	if err != nil {
		return models.Order{}, err
	}

	return GetOrderByIDFromDB(pool, orderID)
}

// getOrderItemsFromDB loads the lines for a set of orders
// in one query.
//
// Fetching them all at once avoids the "N+1" problem,
// where listing twenty orders would otherwise mean twenty
// extra round trips to the database.
func getOrderItemsFromDB(
	ctx context.Context,
	querier dbQuerier,
	orderIDs []int,
) (map[int][]models.OrderItem, error) {

	grouped := make(map[int][]models.OrderItem)

	if len(orderIDs) == 0 {
		return grouped, nil
	}

	rows, err := querier.Query(
		ctx,
		`
		SELECT `+itemColumns+`
		FROM order_items
		WHERE order_id = ANY($1::int[])
		ORDER BY order_id, id
		`,
		orderIDs,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {

		item, err := scanOrderItem(rows)

		if err != nil {
			return nil, err
		}

		grouped[item.OrderID] = append(
			grouped[item.OrderID],
			item,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return grouped, nil
}

// getOrderEventsFromDB loads the history for a set of
// orders in one query.
func getOrderEventsFromDB(
	ctx context.Context,
	querier dbQuerier,
	orderIDs []int,
) (map[int][]models.OrderEvent, error) {

	grouped := make(map[int][]models.OrderEvent)

	if len(orderIDs) == 0 {
		return grouped, nil
	}

	rows, err := querier.Query(
		ctx,
		`
		SELECT
			id,
			order_id,
			from_status,
			to_status,
			actor_id,
			note,
			created_at
		FROM order_events
		WHERE order_id = ANY($1::int[])
		ORDER BY order_id, id
		`,
		orderIDs,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {

		var event models.OrderEvent

		err := rows.Scan(
			&event.ID,
			&event.OrderID,
			&event.FromStatus,
			&event.ToStatus,
			&event.ActorID,
			&event.Note,
			&event.CreatedAt,
		)

		if err != nil {
			return nil, err
		}

		grouped[event.OrderID] = append(
			grouped[event.OrderID],
			event,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return grouped, nil
}

// GetOrderByIDFromDB loads one order with its lines, its
// history, and the status changes that are legal next.
func GetOrderByIDFromDB(
	pool *pgxpool.Pool,
	orderID int,
) (models.Order, error) {

	return getOrderByIDFromDB(
		context.Background(),
		pool,
		orderID,
	)
}

// getOrderByIDFromDB loads one order using an existing
// transaction, so that a caller in the middle of a
// transaction sees its own uncommitted changes.
func getOrderByIDFromDB(
	ctx context.Context,
	querier dbQuerier,
	orderID int,
) (models.Order, error) {

	row := querier.QueryRow(
		ctx,
		`
		SELECT `+orderColumns+`
		FROM orders o
		LEFT JOIN coupons c ON c.id = o.coupon_id
		WHERE o.id = $1
		`,
		orderID,
	)

	order, err := scanOrder(row)

	if err != nil {
		return models.Order{}, ErrOrderNotFound
	}

	items, err := getOrderItemsFromDB(
		ctx,
		querier,
		[]int{orderID},
	)

	if err != nil {
		return models.Order{}, err
	}

	events, err := getOrderEventsFromDB(
		ctx,
		querier,
		[]int{orderID},
	)

	if err != nil {
		return models.Order{}, err
	}

	order.Items = items[orderID]

	if order.Items == nil {
		order.Items = make([]models.OrderItem, 0)
	}

	order.Events = events[orderID]

	// Tell the client which actions will actually work,
	// so it can hide the ones that would be refused.
	order.AllowedTransitions =
		models.AllowedOrderTransitions(order.Status)

	return order, nil
}

// ListOrdersFromDB lists orders matching a filter,
// together with the total number that matched.
func ListOrdersFromDB(
	pool *pgxpool.Pool,
	filter models.OrderFilter,
) ([]models.Order, int, error) {

	ctx := context.Background()

	conditions := make([]string, 0)

	args := make([]any, 0)

	if filter.BuyerID > 0 {

		args = append(args, filter.BuyerID)

		conditions = append(
			conditions,
			fmt.Sprintf("o.buyer_id = $%d", len(args)),
		)
	}

	// A seller only sees orders that contain one of
	// their products, which is exactly their
	// fulfilment queue.
	if filter.SellerID > 0 {

		args = append(args, filter.SellerID)

		conditions = append(
			conditions,
			fmt.Sprintf(
				`EXISTS (
					SELECT 1
					FROM order_items oi
					WHERE oi.order_id = o.id
					AND oi.seller_id = $%d
				)`,
				len(args),
			),
		)
	}

	if filter.Status != "" {

		args = append(args, filter.Status)

		conditions = append(
			conditions,
			fmt.Sprintf("o.status = $%d", len(args)),
		)
	}

	whereClause := ""

	if len(conditions) > 0 {
		whereClause = "WHERE " +
			strings.Join(conditions, " AND ")
	}

	var total int

	err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM orders o `+whereClause,
		args...,
	).Scan(&total)

	if err != nil {
		return nil, 0, err
	}

	pageArgs := append(
		append([]any{}, args...),
		filter.Limit,
		filter.Offset,
	)

	query := fmt.Sprintf(
		`
		SELECT %s
		FROM orders o
		LEFT JOIN coupons c ON c.id = o.coupon_id
		%s
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT $%d OFFSET $%d
		`,
		orderColumns,
		whereClause,
		len(args)+1,
		len(args)+2,
	)

	rows, err := pool.Query(ctx, query, pageArgs...)

	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	orders := make([]models.Order, 0)

	orderIDs := make([]int, 0)

	for rows.Next() {

		order, err := scanOrder(rows)

		if err != nil {
			return nil, 0, err
		}

		orders = append(orders, order)

		orderIDs = append(orderIDs, order.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Load every order's lines in a single extra query
	// rather than one query per order.
	itemsByOrder, err := getOrderItemsFromDB(
		ctx,
		pool,
		orderIDs,
	)

	if err != nil {
		return nil, 0, err
	}

	for index := range orders {

		items := itemsByOrder[orders[index].ID]

		if items == nil {
			items = make([]models.OrderItem, 0)
		}

		orders[index].Items = items

		orders[index].AllowedTransitions =
			models.AllowedOrderTransitions(
				orders[index].Status,
			)
	}

	return orders, total, nil
}

// TransitionOrderInDB moves an order to a new status.
//
// The state machine in models/order_status.go decides what
// is allowed. The check happens twice on purpose:
//
//  1. In Go, so the caller gets a clear message naming
//     both statuses.
//  2. In the UPDATE's WHERE clause, so that even if two
//     requests arrive at the same instant, only one can
//     succeed. The second finds no matching row and is
//     rejected. The SELECT FOR UPDATE below already
//     serialises them, but the guard costs nothing and
//     means the SQL is safe on its own.
func TransitionOrderInDB(
	pool *pgxpool.Pool,
	orderID int,
	toStatus string,
	actorID *int,
	note string,
) (models.Order, error) {

	ctx := context.Background()

	tx, err := pool.Begin(ctx)

	if err != nil {
		return models.Order{}, err
	}

	defer tx.Rollback(ctx)

	// Lock the order and read its current status.
	var fromStatus string

	err = tx.QueryRow(
		ctx,
		`SELECT status FROM orders WHERE id = $1 FOR UPDATE`,
		orderID,
	).Scan(&fromStatus)

	if err != nil {
		return models.Order{}, ErrOrderNotFound
	}

	// Ask the state machine whether this move is legal.
	if !models.CanTransitionOrder(fromStatus, toStatus) {

		return models.Order{}, fmt.Errorf(
			"%w: cannot move from %s to %s",
			ErrInvalidTransition,
			fromStatus,
			toStatus,
		)
	}

	// Apply the change.
	//
	// The CASE expressions stamp the matching timestamp
	// column. Only the column for the status being
	// entered is set, so an order keeps a full record of
	// when it passed through each stage.
	tag, err := tx.Exec(
		ctx,
		`
		UPDATE orders
		SET
			status = $1,
			updated_at = NOW(),
			paid_at = CASE
				WHEN $1 = 'paid' THEN NOW()
				ELSE paid_at
			END,
			shipped_at = CASE
				WHEN $1 = 'shipped' THEN NOW()
				ELSE shipped_at
			END,
			delivered_at = CASE
				WHEN $1 = 'delivered' THEN NOW()
				ELSE delivered_at
			END,
			cancelled_at = CASE
				WHEN $1 = 'cancelled' THEN NOW()
				ELSE cancelled_at
			END,
			refunded_at = CASE
				WHEN $1 = 'refunded' THEN NOW()
				ELSE refunded_at
			END
		WHERE id = $2
		AND status = $3
		`,
		toStatus,
		orderID,
		fromStatus,
	)

	if err != nil {
		return models.Order{}, err
	}

	if tag.RowsAffected() == 0 {

		return models.Order{}, fmt.Errorf(
			"%w: the order changed while it was being updated",
			ErrInvalidTransition,
		)
	}

	// A refund sends the stock back to the seller.
	//
	// Only a refund needs this. The other transitions
	// either happen before any stock left (cancelling a
	// pending order) or do not affect stock at all.
	if toStatus == models.OrderRefunded {

		err = restoreStockForOrderInTx(ctx, tx, orderID)

		if err != nil {
			return models.Order{}, err
		}
	}

	err = appendOrderEventInTx(
		ctx,
		tx,
		orderID,
		fromStatus,
		toStatus,
		actorID,
		note,
	)

	if err != nil {
		return models.Order{}, err
	}

	order, err := getOrderByIDFromDB(ctx, tx, orderID)

	if err != nil {
		return models.Order{}, err
	}

	err = tx.Commit(ctx)

	if err != nil {
		return models.Order{}, err
	}

	return order, nil
}

// restoreStockForOrderInTx writes refund movements for
// every line on an order.
//
// The movements are positive, because stock is coming
// back. The UNIQUE constraint on
// (order_id, product_id, reason) means an order can only
// ever be refunded into stock once, so a duplicate refund
// cannot invent stock out of nothing.
func restoreStockForOrderInTx(
	ctx context.Context,
	querier dbQuerier,
	orderID int,
) error {

	rows, err := querier.Query(
		ctx,
		`
		SELECT product_id, quantity
		FROM order_items
		WHERE order_id = $1
		ORDER BY product_id
		`,
		orderID,
	)

	if err != nil {
		return err
	}

	type line struct {
		productID int
		quantity  int
	}

	lines := make([]line, 0)

	for rows.Next() {

		var current line

		err := rows.Scan(
			&current.productID,
			&current.quantity,
		)

		if err != nil {

			rows.Close()

			return err
		}

		lines = append(lines, current)
	}

	if err := rows.Err(); err != nil {

		rows.Close()

		return err
	}

	rows.Close()

	// Lock the products before changing their stock, in
	// the same order the other stock-changing paths use.
	productIDs := make([]int, 0, len(lines))

	for _, current := range lines {
		productIDs = append(productIDs, current.productID)
	}

	err = lockProductsInTx(ctx, querier, productIDs)

	if err != nil {
		return err
	}

	for _, current := range lines {

		_, err := appendMovementInTx(
			ctx,
			querier,
			models.InventoryMovement{
				ProductID: current.productID,

				// Positive: stock returning.
				QuantityChange: current.quantity,

				Reason: models.MovementRefund,

				OrderID: &orderID,
			},
		)

		if err != nil {
			return err
		}
	}

	return nil
}

// appendMovementInTx is the transaction-aware version of
// AddInventoryMovementInDB.
func appendMovementInTx(
	ctx context.Context,
	querier dbQuerier,
	movement models.InventoryMovement,
) (models.InventoryMovement, error) {

	return addInventoryMovementInTx(ctx, querier, movement)
}
// ConfirmOrderPaymentInTx marks a paid order as paid and
// takes the stock off the ledger.
//
// This is the single most safety-critical function in the
// project. It is written so that running it twice for the
// same order cannot double-charge, double-decrement, or
// corrupt anything.
//
// Three separate guards stand in the way of a duplicate:
//
//  1. The caller has already claimed the webhook event in
//     the webhook_events table, which has a UNIQUE
//     constraint on the provider's event id. A retried
//     delivery never reaches this function at all.
//
//  2. The UPDATE below carries "AND status = 'pending'",
//     so if the order is already paid, no row changes and
//     this returns ErrAlreadyPaidAfterPayment rather than
//     acting twice.
//
//  3. inventory_movements has a UNIQUE constraint on
//     (order_id, product_id, reason), so a second set of
//     'sale' rows for the same order is refused by
//     PostgreSQL itself.
//
// Any one of those would be enough. Together they mean a
// mistake in one layer cannot become a double charge.
func ConfirmOrderPaymentInTx(
	ctx context.Context,
	querier dbQuerier,
	reference string,
	paidKobo int64,
	note string,
) (models.Order, error) {

	// Find the order and lock it, so that two webhooks
	// arriving together cannot both read "pending".
	var orderID int

	var status string

	var total float64

	err := querier.QueryRow(
		ctx,
		`
		SELECT id, status, total
		FROM orders
		WHERE payment_reference = $1
		FOR UPDATE
		`,
		reference,
	).Scan(&orderID, &status, &total)

	if err != nil {
		return models.Order{}, ErrOrderNotFound
	}

	// Check the money before changing anything.
	//
	// The amount is compared in kobo, as integers. If a
	// webhook claimed a payment of one naira for a
	// hundred thousand naira order, this is where it
	// stops, before any order is marked paid and before
	// any stock moves.
	if utils.ToKobo(total) != paidKobo {

		return models.Order{}, fmt.Errorf(
			"%w: order total is %s kobo but %d kobo was paid",
			ErrAmountMismatch,
			utils.FormatKobo(utils.ToKobo(total)),
			paidKobo,
		)
	}

	// Guard two: only a pending order may become paid.
	//
	// This is not an error worth retrying, so it is
	// reported with its own value and the caller answers
	// the provider with a success.
	if status != models.OrderPending {

		return models.Order{}, ErrAlreadyPaid
	}

	// Read the order's lines.
	rows, err := querier.Query(
		ctx,
		`
		SELECT product_id, quantity
		FROM order_items
		WHERE order_id = $1
		ORDER BY product_id
		`,
		orderID,
	)

	if err != nil {
		return models.Order{}, err
	}

	type line struct {
		productID int
		quantity  int
	}

	lines := make([]line, 0)

	for rows.Next() {

		var current line

		err := rows.Scan(
			&current.productID,
			&current.quantity,
		)

		if err != nil {

			rows.Close()

			return models.Order{}, err
		}

		lines = append(lines, current)
	}

	if err := rows.Err(); err != nil {

		rows.Close()

		return models.Order{}, err
	}

	rows.Close()

	// Lock the products, then re-read each ledger sum.
	//
	// This second stock check is the one that actually
	// counts. The check at checkout time was only
	// advice; by the time payment arrives, other orders
	// may have taken the stock. Because we hold the row
	// locks, nothing can move underneath us while we
	// decide.
	productIDs := make([]int, 0, len(lines))

	for _, current := range lines {
		productIDs = append(productIDs, current.productID)
	}

	err = lockProductsInTx(ctx, querier, productIDs)

	if err != nil {
		return models.Order{}, err
	}

	for _, current := range lines {

		stock, err := getProductStockInTx(
			ctx,
			querier,
			current.productID,
		)

		if err != nil {
			return models.Order{}, err
		}

		if stock < current.quantity {

			// The money has been taken but the goods
			// are gone. The transaction is rolled back
			// so the order stays pending and can be
			// refunded by hand, which is the honest
			// outcome: no order is silently marked
			// paid for goods that cannot ship.
			return models.Order{}, fmt.Errorf(
				"%w: product %d has %d left but %d are needed",
				ErrInsufficientStock,
				current.productID,
				stock,
				current.quantity,
			)
		}
	}

	// Write the sale movements.
	//
	// They are negative, because stock is leaving.
	// Guard three lives here: the UNIQUE constraint on
	// (order_id, product_id, reason) refuses a second
	// set of 'sale' rows for this order.
	for _, current := range lines {

		_, err := appendMovementInTx(
			ctx,
			querier,
			models.InventoryMovement{
				ProductID: current.productID,

				// Negative: stock leaving.
				QuantityChange: -current.quantity,

				Reason: models.MovementSale,

				OrderID: &orderID,
			},
		)

		if err != nil {
			return models.Order{}, err
		}
	}

	// Guard two, applied.
	tag, err := querier.Exec(
		ctx,
		`
		UPDATE orders
		SET
			status = 'paid',
			paid_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
		AND status = 'pending'
		`,
		orderID,
	)

	if err != nil {
		return models.Order{}, err
	}

	if tag.RowsAffected() == 0 {
		return models.Order{}, ErrAlreadyPaid
	}

	// The actor is NULL here because nobody clicked
	// anything: the change came from the provider.
	err = appendOrderEventInTx(
		ctx,
		querier,
		orderID,
		models.OrderPending,
		models.OrderPaid,
		nil,
		note,
	)

	if err != nil {
		return models.Order{}, err
	}

	return getOrderByIDFromDB(ctx, querier, orderID)
}

// SetOrderPaymentReferenceInDB records the reference that
// Paystack issued for an order.
//
// It is only set while the order is still pending, so a
// second attempt cannot overwrite the reference of an
// order that has already been paid.
func SetOrderPaymentReferenceInDB(
	pool *pgxpool.Pool,
	orderID int,
	buyerID int,
	reference string,
) (models.Order, error) {

	ctx := context.Background()

	tag, err := pool.Exec(
		ctx,
		`
		UPDATE orders
		SET
			payment_reference = $1,
			updated_at = NOW()
		WHERE id = $2
		AND buyer_id = $3
		AND status = 'pending'
		`,
		reference,
		orderID,
		buyerID,
	)

	if err != nil {
		return models.Order{}, err
	}

	if tag.RowsAffected() == 0 {
		return models.Order{}, ErrOrderNotFound
	}

	return GetOrderByIDFromDB(pool, orderID)
}

// FindOrderByPaymentReferenceFromDB looks an order up by
// the reference used at the payment provider.
func FindOrderByPaymentReferenceFromDB(
	pool *pgxpool.Pool,
	reference string,
) (models.Order, error) {

	var orderID int

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT id
		FROM orders
		WHERE payment_reference = $1
		`,
		reference,
	).Scan(&orderID)

	if err != nil {
		return models.Order{}, ErrOrderNotFound
	}

	return GetOrderByIDFromDB(pool, orderID)
}

// ConfirmPaymentByReferenceInDB confirms a payment outside
// the webhook path.
//
// This is what the manual verify endpoint uses. It exists
// because a webhook can be lost: if this server was down
// or the network dropped, Paystack's message never
// arrived, and the payment would sit unacknowledged
// forever.
//
// It deliberately runs the same guarded logic as the
// webhook. ConfirmOrderPaymentInTx only acts on an order
// that is still pending, so calling this endpoint for a
// payment that a webhook already handled changes nothing.
// That means the two paths cannot double-process, even if
// both run for the same payment.
func ConfirmPaymentByReferenceInDB(
	pool *pgxpool.Pool,
	reference string,
	paidKobo int64,
	note string,
) (models.Order, error) {

	ctx := context.Background()

	tx, err := pool.Begin(ctx)

	if err != nil {
		return models.Order{}, err
	}

	defer tx.Rollback(ctx)

	order, err := ConfirmOrderPaymentInTx(
		ctx,
		tx,
		reference,
		paidKobo,
		note,
	)

	// The order was already past pending, so there is
	// nothing to change. This is not a failure, so the
	// existing order is returned as it stands.
	if errors.Is(err, ErrAlreadyPaid) {

		return FindOrderByPaymentReferenceFromDB(
			pool,
			reference,
		)
	}

	if err != nil {
		return models.Order{}, err
	}

	err = tx.Commit(ctx)

	if err != nil {
		return models.Order{}, err
	}

	return order, nil
}
