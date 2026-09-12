package handlers

import (
	"errors"
	"net/http"
	"strings"

	"e-commerce-backend/models"
	"e-commerce-backend/services"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateOrderHandler turns the signed-in shopper's cart
// into an order.
//
// Note what does not happen here: no stock is taken and
// no money moves. The order is created as pending, and
// stock is only reduced when payment is confirmed. The
// stock is checked here so a shopper is told early if
// something has sold out, but that check is a courtesy,
// not a reservation.
func CreateOrderHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		var request models.OrderCreateRequest

		// The body is optional. Placing an order without
		// a coupon is the common case, so an empty body
		// must not be a failure.
		if r.ContentLength > 0 {

			if !decodeJSONBody(w, r, &request) {
				return
			}
		}

		order, err := storage.CreateOrderFromCartInDB(
			pool,
			user.ID,
			request.CouponCode,
		)

		if err != nil {

			writeOrderCreationError(w, err)

			return
		}

		writeJSON(w, http.StatusCreated, order)
	}
}

// writeOrderCreationError turns an order creation failure
// into the right HTTP response.
//
// Each of these is a different problem for the shopper and
// deserves a different message. A sold-out line and an
// expired coupon need completely different actions, so
// collapsing them into one generic error would leave the
// shopper with nothing to do.
func writeOrderCreationError(
	w http.ResponseWriter,
	err error,
) {

	switch {

	case errors.Is(err, storage.ErrCartEmpty):

		writeError(
			w,
			http.StatusBadRequest,
			"Your cart is empty",
		)

	case errors.Is(err, storage.ErrProductUnavailable):

		writeError(
			w,
			http.StatusConflict,
			"A product in your cart is no longer available",
		)

	case errors.Is(err, storage.ErrInsufficientStock):

		writeError(
			w,
			http.StatusConflict,
			"Not enough stock to fulfil your order",
		)

	case errors.Is(err, storage.ErrCouponNotFound):

		writeError(
			w,
			http.StatusNotFound,
			"That coupon code is not valid",
		)

	case errors.Is(err, storage.ErrCouponRejected):

		writeError(
			w,
			http.StatusConflict,
			// The storage layer wraps the human-readable
			// reason, so it is passed straight through.
			err.Error(),
		)

	default:

		writeError(
			w,
			http.StatusInternalServerError,
			"Could not place your order",
		)
	}
}

// ListOrdersHandler lists the orders a user is entitled to
// see.
//
// The filter follows the role, and the client cannot widen
// it. A buyer sees orders they placed, a seller sees
// orders containing their products, and an admin sees
// everything. Letting a client pass an arbitrary buyer_id
// would turn this into a list of other people's orders.
func ListOrdersHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		pagination := utils.ParsePagination(r)

		filter := models.OrderFilter{
			Status: strings.TrimSpace(
				r.URL.Query().Get("status"),
			),

			Limit: pagination.PerPage,

			Offset: pagination.Offset(),
		}

		if filter.Status != "" &&
			!models.IsValidOrderStatus(filter.Status) {

			writeError(
				w,
				http.StatusBadRequest,
				"Unknown order status",
			)

			return
		}

		switch user.Role {

		case models.RoleAdmin:

			// An admin sees everything, so neither
			// id is set and no narrowing happens.

		case models.RoleSeller:

			filter.SellerID = user.ID

		default:

			filter.BuyerID = user.ID
		}

		orders, total, err := storage.ListOrdersFromDB(
			pool,
			filter,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load orders",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			utils.NewPage(
				orders,
				pagination,
				total,
			),
		)
	}
}

// GetOrderHandler returns one order with its lines and its
// status history.
func GetOrderHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		orderID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		order, ok := loadOrderForUser(
			pool,
			w,
			user,
			orderID,
		)

		if !ok {
			return
		}

		writeJSON(w, http.StatusOK, order)
	}
}

// ListOrderEventsHandler returns an order's status
// history.
//
// Because order_events is append-only, this is the whole
// story of the order: who moved it, when, and why.
func ListOrderEventsHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		orderID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		order, ok := loadOrderForUser(
			pool,
			w,
			user,
			orderID,
		)

		if !ok {
			return
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"order_id": orderID,

				"status": order.Status,

				"allowed_transitions": order.AllowedTransitions,

				"events": order.Events,
			},
		)
	}
}

// CancelOrderHandler cancels an unpaid order.
//
// Only the buyer may cancel, and only while the order is
// pending. Once money has changed hands the honest exit is
// a refund, not a cancellation, which is why the state
// machine does not allow cancelled from paid.
func CancelOrderHandler(
	pool *pgxpool.Pool,
	hub *services.Hub,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		orderID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		order, ok := loadOrderForUser(
			pool,
			w,
			user,
			orderID,
		)

		if !ok {
			return
		}

		// The buyer owns the decision to cancel. An
		// admin is allowed too, because a marketplace
		// needs somebody who can undo a mistake.
		if order.BuyerID != user.ID &&
			user.Role != models.RoleAdmin {

			writeError(
				w,
				http.StatusForbidden,
				"Only the buyer can cancel this order",
			)

			return
		}

		transitionOrder(
			pool,
			hub,
			w,
			orderID,
			models.OrderCancelled,
			&user.ID,
			"Cancelled before payment by the buyer",
		)
	}
}

// ShipOrderHandler marks a paid order as shipped.
//
// Only a seller who actually has an item on the order may
// do this. A seller shipping somebody else's order would
// be nonsense, and the check is what stops it.
func ShipOrderHandler(
	pool *pgxpool.Pool,
	hub *services.Hub,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		orderID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		order, ok := loadOrderForUser(
			pool,
			w,
			user,
			orderID,
		)

		if !ok {
			return
		}

		if !userSellsOnOrder(order, user) {

			writeError(
				w,
				http.StatusForbidden,
				"Only a seller with an item on this order can ship it",
			)

			return
		}

		transitionOrder(
			pool,
			hub,
			w,
			orderID,
			models.OrderShipped,
			&user.ID,
			"Shipped by the seller",
		)
	}
}

// DeliverOrderHandler marks a shipped order as delivered.
//
// The buyer confirms receipt, because the buyer is the
// only person who actually knows whether the parcel
// arrived.
func DeliverOrderHandler(
	pool *pgxpool.Pool,
	hub *services.Hub,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		orderID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		order, ok := loadOrderForUser(
			pool,
			w,
			user,
			orderID,
		)

		if !ok {
			return
		}

		if order.BuyerID != user.ID &&
			user.Role != models.RoleAdmin {

			writeError(
				w,
				http.StatusForbidden,
				"Only the buyer can confirm delivery",
			)

			return
		}

		transitionOrder(
			pool,
			hub,
			w,
			orderID,
			models.OrderDelivered,
			&user.ID,
			"Delivered and confirmed by the buyer",
		)
	}
}

// RefundOrderHandler refunds an order.
//
// Only an admin may do this. A refund returns stock to
// the ledger as well as changing the status, so it is a
// decision with consequences beyond one order.
func RefundOrderHandler(
	pool *pgxpool.Pool,
	hub *services.Hub,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		if user.Role != models.RoleAdmin {

			writeError(
				w,
				http.StatusForbidden,
				"Only an admin can refund an order",
			)

			return
		}

		orderID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		// Confirm the order exists before asking for a
		// reason, so a request against a missing order
		// fails for the right reason.
		if _, ok := loadOrderForUser(
			pool,
			w,
			user,
			orderID,
		); !ok {
			return
		}

		// A reason is required. A refund moves money and
		// stock, and an unexplained one in the history
		// would be impossible to review later.
		var request struct {
			Reason string `json:"reason"`
		}

		if r.ContentLength > 0 {

			if !decodeJSONBody(w, r, &request) {
				return
			}
		}

		reason := strings.TrimSpace(request.Reason)

		if reason == "" {

			writeError(
				w,
				http.StatusBadRequest,
				"A reason is required to refund an order",
			)

			return
		}

		transitionOrder(
			pool,
			hub,
			w,
			orderID,
			models.OrderRefunded,
			&user.ID,
			"Refunded by an admin: "+reason,
		)
	}
}

// transitionOrder performs a status change and answers.
//
// Every status change goes through here so that the
// success path, the failure paths, and the broadcast into
// the order's chat room all happen the same way. A
// transition applied without broadcasting would leave
// anyone watching the chat looking at a stale status.
func transitionOrder(
	pool *pgxpool.Pool,
	hub *services.Hub,
	w http.ResponseWriter,
	orderID int,
	toStatus string,
	actorID *int,
	note string,
) {

	order, err := storage.TransitionOrderInDB(
		pool,
		orderID,
		toStatus,
		actorID,
		note,
	)

	if err != nil {

		// A transition the state machine forbids is a
		// conflict between what the client believed
		// and what is true now, which is exactly what
		// 409 means. The message names both statuses so
		// the client can see why.
		if errors.Is(err, storage.ErrInvalidTransition) {

			writeError(
				w,
				http.StatusConflict,
				err.Error(),
			)

			return
		}

		if errors.Is(err, storage.ErrOrderNotFound) {

			writeError(
				w,
				http.StatusNotFound,
				"Order not found",
			)

			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Could not update the order",
		)

		return
	}

	// Tell anyone watching the order's chat room.
	if hub != nil {
		hub.BroadcastOrderStatus(
			orderID,
			order.Status,
			note,
		)
	}

	writeJSON(w, http.StatusOK, order)
}

// loadOrderForUser fetches an order and checks the caller
// is entitled to see it.
//
// The rule is the same one the chat room uses: the buyer,
// a seller with an item on the order, or an admin. An
// order is private, and every endpoint that touches one
// goes through this, so the rule cannot be forgotten in
// one place and enforced in another.
func loadOrderForUser(
	pool *pgxpool.Pool,
	w http.ResponseWriter,
	user models.User,
	orderID int,
) (models.Order, bool) {

	allowed, err :=
		storage.CanUserAccessOrderChatFromDB(
			pool,
			orderID,
			user.ID,
		)

	if err != nil {

		writeError(
			w,
			http.StatusInternalServerError,
			"Could not check access to the order",
		)

		return models.Order{}, false
	}

	// A stranger gets 404 rather than 403. Answering 403
	// would confirm that an order with that number
	// exists, which is information a stranger has no
	// business having.
	if !allowed {

		writeError(
			w,
			http.StatusNotFound,
			"Order not found",
		)

		return models.Order{}, false
	}

	order, err := storage.GetOrderByIDFromDB(pool, orderID)

	if err != nil {

		writeError(
			w,
			http.StatusNotFound,
			"Order not found",
		)

		return models.Order{}, false
	}

	return order, true
}

// userSellsOnOrder reports whether a user has at least one
// item on an order.
func userSellsOnOrder(
	order models.Order,
	user models.User,
) bool {

	if user.Role == models.RoleAdmin {
		return true
	}

	for _, item := range order.Items {

		if item.SellerID == user.ID {
			return true
		}
	}

	return false
}
