package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"e-commerce-backend/database"
	"e-commerce-backend/handlers"
	"e-commerce-backend/services"

	"github.com/jackc/pgx/v5/pgxpool"
)

// main wires the whole application together and starts
// the server.
//
// Routing uses the standard library's ServeMux with
// method and wildcard patterns, which Go 1.22 added. That
// keeps this project free of a router dependency while
// still naming each route in one readable place, rather
// than splitting paths apart by hand inside every handler.
func main() {

	pool, err := database.Connect()

	if err != nil {
		log.Fatalf(
			"could not connect to the database: %v",
			err,
		)
	}

	defer pool.Close()

	log.Println("connected to the database")

	// The Paystack client is created even when no keys are
	// configured. Nothing breaks until a payment endpoint
	// is actually called, which means the rest of the API
	// can be exercised without payment credentials.
	paystack := services.NewPaystackClientFromEnv()

	if !paystack.HasSecretKey() {

		log.Println(
			"PAYSTACK_SECRET_KEY is not set: payment endpoints will fail, everything else works",
		)
	}

	// One hub is shared by every chat connection and by
	// every handler that moves an order, because the hub is
	// what lets a status change reach the people watching
	// that order.
	hub := services.NewHub()

	chatOrigins := splitAndTrim(
		os.Getenv("CHAT_ALLOWED_ORIGINS"),
	)

	if len(chatOrigins) == 0 {

		log.Println(
			"CHAT_ALLOWED_ORIGINS is not set: the chat socket will accept any origin. Set it before exposing this server publicly.",
		)
	}

	mux := http.NewServeMux()

	registerAuthRoutes(mux, pool)

	registerCatalogRoutes(mux, pool)

	registerCartRoutes(mux, pool)

	registerWishlistRoutes(mux, pool)

	registerOrderRoutes(mux, pool, hub)

	registerPaymentRoutes(
		mux,
		pool,
		paystack,
		os.Getenv("PAYSTACK_SECRET_KEY"),
	)

	registerCouponRoutes(mux, pool)

	registerChatRoutes(mux, pool, hub, chatOrigins)

	registerAdminRoutes(mux, pool)

	mux.HandleFunc(
		"GET /health",
		func(w http.ResponseWriter, r *http.Request) {

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			w.Write(
				[]byte(`{"status":"ok"}`),
			)
		},
	)

	port := os.Getenv("PORT")

	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr: ":" + port,

		Handler: mux,

		// Timeouts are set so that a slow or idle client
		// cannot hold a connection open indefinitely.
		//
		// WriteTimeout is deliberately left at zero. The
		// chat socket stays open for as long as somebody
		// is watching an order, and a write deadline
		// would cut every conversation off partway
		// through. The per-frame deadlines in the chat
		// handler cover that connection instead.
		ReadHeaderTimeout: 10 * time.Second,

		IdleTimeout: 120 * time.Second,
	}

	log.Printf("listening on %s", server.Addr)

	err = server.ListenAndServe()

	if err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

// registerAuthRoutes wires registration, sign in, sign
// out, and the current user.
func registerAuthRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
) {

	mux.HandleFunc(
		"POST /register",
		handlers.RegisterHandler(pool),
	)

	mux.HandleFunc(
		"POST /login",
		handlers.LoginHandler(pool),
	)

	mux.HandleFunc(
		"POST /logout",
		handlers.LogoutHandler(pool),
	)

	mux.HandleFunc(
		"GET /me",
		handlers.MeHandler(pool),
	)
}

// registerCatalogRoutes wires products, categories,
// reviews, inventory, and recommendations.
func registerCatalogRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
) {

	mux.HandleFunc(
		"GET /products",
		handlers.ListProductsHandler(pool),
	)

	mux.HandleFunc(
		"POST /products",
		handlers.CreateProductHandler(pool),
	)

	mux.HandleFunc(
		"GET /products/{id}",
		handlers.GetProductHandler(pool),
	)

	mux.HandleFunc(
		"PUT /products/{id}",
		handlers.UpdateProductHandler(pool),
	)

	mux.HandleFunc(
		"DELETE /products/{id}",
		handlers.DeleteProductHandler(pool),
	)

	mux.HandleFunc(
		"POST /products/{id}/restock",
		handlers.RestockProductHandler(pool),
	)

	mux.HandleFunc(
		"GET /products/{id}/inventory",
		handlers.ListInventoryMovementsHandler(pool),
	)

	mux.HandleFunc(
		"GET /products/{id}/reviews",
		handlers.ListReviewsHandler(pool),
	)

	mux.HandleFunc(
		"POST /products/{id}/reviews",
		handlers.CreateReviewHandler(pool),
	)

	mux.HandleFunc(
		"PUT /products/{id}/reviews",
		handlers.UpdateReviewHandler(pool),
	)

	mux.HandleFunc(
		"DELETE /products/{id}/reviews",
		handlers.DeleteReviewHandler(pool),
	)

	mux.HandleFunc(
		"GET /products/{id}/recommendations",
		handlers.GetRecommendationsHandler(pool),
	)

	mux.HandleFunc(
		"GET /categories",
		handlers.ListCategoriesHandler(pool),
	)

	mux.HandleFunc(
		"POST /categories",
		handlers.CreateCategoryHandler(pool),
	)
}

// registerCartRoutes wires the cart.
func registerCartRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
) {

	mux.HandleFunc(
		"GET /cart",
		handlers.GetCartHandler(pool),
	)

	mux.HandleFunc(
		"DELETE /cart",
		handlers.ClearCartHandler(pool),
	)

	mux.HandleFunc(
		"POST /cart/items",
		handlers.AddCartItemHandler(pool),
	)

	mux.HandleFunc(
		"PATCH /cart/items/{productID}",
		handlers.UpdateCartItemHandler(pool),
	)

	mux.HandleFunc(
		"DELETE /cart/items/{productID}",
		handlers.RemoveCartItemHandler(pool),
	)
}

// registerWishlistRoutes wires the wishlist.
func registerWishlistRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
) {

	mux.HandleFunc(
		"GET /wishlist",
		handlers.ListWishlistHandler(pool),
	)

	mux.HandleFunc(
		"POST /wishlist/items",
		handlers.AddWishlistItemHandler(pool),
	)

	mux.HandleFunc(
		"DELETE /wishlist/items/{productID}",
		handlers.RemoveWishlistItemHandler(pool),
	)
}

// registerOrderRoutes wires checkout and the order state
// machine.
//
// The hub is passed to every handler that moves an order
// to a new status, so that the change is pushed into the
// order's chat room and anyone watching sees it straight
// away.
func registerOrderRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
	hub *services.Hub,
) {

	mux.HandleFunc(
		"POST /orders",
		handlers.CreateOrderHandler(pool),
	)

	mux.HandleFunc(
		"GET /orders",
		handlers.ListOrdersHandler(pool),
	)

	mux.HandleFunc(
		"GET /orders/{id}",
		handlers.GetOrderHandler(pool),
	)

	mux.HandleFunc(
		"GET /orders/{id}/events",
		handlers.ListOrderEventsHandler(pool),
	)

	mux.HandleFunc(
		"POST /orders/{id}/cancel",
		handlers.CancelOrderHandler(pool, hub),
	)

	mux.HandleFunc(
		"POST /orders/{id}/ship",
		handlers.ShipOrderHandler(pool, hub),
	)

	mux.HandleFunc(
		"POST /orders/{id}/deliver",
		handlers.DeliverOrderHandler(pool, hub),
	)

	mux.HandleFunc(
		"POST /orders/{id}/refund",
		handlers.RefundOrderHandler(pool, hub),
	)
}

// registerPaymentRoutes wires paying for an order, the
// recovery verification, and the provider's webhook.
func registerPaymentRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
	paystack *services.PaystackClient,
	webhookSecret string,
) {

	mux.HandleFunc(
		"POST /orders/{id}/pay",
		handlers.PayOrderHandler(pool, paystack),
	)

	mux.HandleFunc(
		"GET /payments/verify/{reference}",
		handlers.VerifyPaymentHandler(pool, paystack),
	)

	// This route is deliberately not behind a session:
	// Paystack has no cookie. It is protected by a
	// signature over the raw request body instead, which
	// is checked before anything is parsed.
	mux.HandleFunc(
		"POST /webhooks/paystack",
		handlers.PaystackWebhookHandler(
			pool,
			webhookSecret,
		),
	)
}

// registerCouponRoutes wires discount codes.
func registerCouponRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
) {

	mux.HandleFunc(
		"GET /coupons",
		handlers.ListCouponsHandler(pool),
	)

	mux.HandleFunc(
		"POST /coupons",
		handlers.CreateCouponHandler(pool),
	)

	mux.HandleFunc(
		"POST /coupons/validate",
		handlers.ValidateCouponHandler(pool),
	)

	mux.HandleFunc(
		"PATCH /coupons/{id}",
		handlers.SetCouponActiveHandler(pool),
	)
}

// registerChatRoutes wires the live order chat.
func registerChatRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
	hub *services.Hub,
	allowedOrigins []string,
) {

	mux.HandleFunc(
		"GET /orders/{id}/chat",
		handlers.OrderChatHandler(
			pool,
			hub,
			allowedOrigins,
		),
	)
}

// registerAdminRoutes wires the administrative views.
func registerAdminRoutes(
	mux *http.ServeMux,
	pool *pgxpool.Pool,
) {

	mux.HandleFunc(
		"GET /admin/users",
		handlers.ListUsersHandler(pool),
	)

	mux.HandleFunc(
		"PUT /admin/users/{id}/role",
		handlers.UpdateUserRoleHandler(pool),
	)

	mux.HandleFunc(
		"GET /admin/orders",
		handlers.ListAllOrdersHandler(pool),
	)
}

// splitAndTrim turns a comma-separated environment
// variable into a list, dropping empty entries.
//
// It means "http://localhost:3000, http://localhost:5173"
// and "http://localhost:3000,http://localhost:5173" both
// work, which is a small kindness when somebody is typing
// these by hand.
func splitAndTrim(value string) []string {

	parts := strings.Split(value, ",")

	cleaned := make([]string, 0, len(parts))

	for _, part := range parts {

		part = strings.TrimSpace(part)

		if part != "" {
			cleaned = append(cleaned, part)
		}
	}

	return cleaned
}
