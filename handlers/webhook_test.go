package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"e-commerce-backend/database/testdb"
	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// webhookTestSecret stands in for PAYSTACK_SECRET_KEY.
//
// It is deliberately not a real key. The webhook path only
// needs the secret to compute a signature, so a made-up
// value lets the whole mechanism be tested with no Paystack
// account and no public URL.
const webhookTestSecret = "sk_test_webhook_signature_only"

// webhookFixture is a fully set up order waiting to be
// paid for, which is what every test in this file needs
// before it can post a webhook.
type webhookFixture struct {

	pool *pgxpool.Pool

	buyer models.User

	seller models.User

	product models.Product

	order models.Order

	// reference is what the order was given at checkout
	// and what a webhook quotes back.
	reference string

	// amountKobo is the order total in kobo. A genuine
	// charge.success must carry exactly this.
	amountKobo int64
}

// newWebhookFixture builds a seller, a product with ten in
// stock, a buyer with two of them in a cart, and an order
// placed from that cart.
//
// The long way round is deliberate. Going through the real
// storage functions means the fixture cannot drift away
// from what the application actually does, which a pile of
// hand-written INSERT statements eventually would.
func newWebhookFixture(t *testing.T) webhookFixture {

	t.Helper()

	pool := testdb.Pool(t)

	seller := createTestUser(
		t,
		pool,
		"seller@example.com",
		models.RoleSeller,
	)

	buyer := createTestUser(
		t,
		pool,
		"buyer@example.com",
		models.RoleBuyer,
	)

	category, err := storage.CreateCategoryInDB(
		pool,
		models.Category{
			Name: "Test Category",
			Slug: "test-category",
		},
	)

	if err != nil {

		t.Fatalf(
			"could not create a category: %v",
			err,
		)
	}

	product, err := storage.CreateProductInDB(
		pool,
		models.Product{
			SellerID: seller.ID,

			CategoryID: category.ID,

			Name: "Test Widget",

			Slug: "test-widget",

			Description: "A widget, for testing.",

			Price: 25.50,

			Currency: "NGN",

			IsActive: true,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not create a product: %v",
			err,
		)
	}

	// Ten units arrive. Nothing else can put stock in:
	// the ledger is the only source of a stock number.
	_, err = storage.AddInventoryMovementInDB(
		pool,
		models.InventoryMovement{
			ProductID: product.ID,

			QuantityChange: 10,

			Reason: models.MovementRestock,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not restock the product: %v",
			err,
		)
	}

	cartID, err := storage.GetOrCreateCartInDB(
		pool,
		buyer.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not open a cart: %v",
			err,
		)
	}

	err = storage.AddCartItemInDB(
		pool,
		cartID,
		product.ID,
		2,
	)

	if err != nil {

		t.Fatalf(
			"could not add an item to the cart: %v",
			err,
		)
	}

	order, err := storage.CreateOrderFromCartInDB(
		pool,
		buyer.ID,
		"",
	)

	if err != nil {

		t.Fatalf(
			"could not place an order: %v",
			err,
		)
	}

	// A real checkout calls Paystack, which returns a
	// reference, which is then stored against the order.
	// Only the last step matters here, because that is
	// the only part the webhook depends on.
	reference := fmt.Sprintf(
		"ECB-test-%d",
		order.ID,
	)

	order, err = storage.SetOrderPaymentReferenceInDB(
		pool,
		order.ID,
		buyer.ID,
		reference,
	)

	if err != nil {

		t.Fatalf(
			"could not attach a payment reference: %v",
			err,
		)
	}

	amountKobo := utils.ToKobo(order.Total)

	if amountKobo != 5100 {

		t.Fatalf(
			"expected an order total of 5100 kobo, got %d",
			amountKobo,
		)
	}

	return webhookFixture{
		pool: pool,

		buyer: buyer,

		seller: seller,

		product: product,

		order: order,

		reference: reference,

		amountKobo: amountKobo,
	}
}

// createTestUser makes an account directly, without going
// through registration, because these tests are about
// payments rather than about signing up.
func createTestUser(
	t *testing.T,
	pool *pgxpool.Pool,
	email string,
	role string,
) models.User {

	t.Helper()

	hash, err := utils.HashPassword("password123")

	if err != nil {

		t.Fatalf(
			"could not hash a password: %v",
			err,
		)
	}

	user, err := storage.CreateUserInDB(
		pool,
		models.User{
			Name: email,

			Email: email,

			PasswordHash: hash,

			Role: role,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not create the user %s: %v",
			email,
			err,
		)
	}

	return user
}

// chargeSuccessBody builds a Paystack charge.success
// payload.
//
// The id is a JSON number and the reference is a string,
// which is exactly the shape Paystack sends.
func chargeSuccessBody(
	eventID int,
	reference string,
	amountKobo int64,
) []byte {

	return []byte(fmt.Sprintf(
		`{"event":"charge.success","data":{"id":%d,"reference":%q,"amount":%d,"status":"success"}}`,
		eventID,
		reference,
		amountKobo,
	))
}

// postWebhook sends a body to the webhook handler with a
// signature computed from the test secret, and returns the
// response.
func postWebhook(
	t *testing.T,
	pool *pgxpool.Pool,
	secretKey string,
	body []byte,
	signature string,
) *httptest.ResponseRecorder {

	t.Helper()

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/paystack",
		bytes.NewReader(body),
	)

	if signature == "" {
		signature = utils.ComputePaystackSignature(
			secretKey,
			body,
		)
	}

	request.Header.Set(
		"x-paystack-signature",
		signature,
	)

	recorder := httptest.NewRecorder()

	PaystackWebhookHandler(
		pool,
		secretKey,
	)(recorder, request)

	return recorder
}

// webhookStatus pulls the "status" field out of a webhook
// response.
func webhookStatus(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) string {

	t.Helper()

	var body struct {
		Status string `json:"status"`
	}

	err := json.Unmarshal(
		recorder.Body.Bytes(),
		&body,
	)

	if err != nil {

		t.Fatalf(
			"could not read the webhook response %q: %v",
			recorder.Body.String(),
			err,
		)
	}

	return body.Status
}

// countRows runs a counting query.
func countRows(
	t *testing.T,
	pool *pgxpool.Pool,
	query string,
	args ...any,
) int {

	t.Helper()

	var count int

	err := pool.QueryRow(
		context.Background(),
		query,
		args...,
	).Scan(&count)

	if err != nil {

		t.Fatalf(
			"counting query failed: %v",
			err,
		)
	}

	return count
}

// TestPaystackWebhookIsProcessedExactlyOnce is the test
// the whole payment design exists to pass.
//
// Paystack retries a webhook whenever it does not receive a
// prompt 200, and it may keep retrying for days. The same
// signed body is therefore posted twice here, exactly as a
// retry would arrive, and the test asserts that the second
// delivery changed nothing at all.
func TestPaystackWebhookIsProcessedExactlyOnce(t *testing.T) {

	fixture := newWebhookFixture(t)

	body := chargeSuccessBody(
		7712001,
		fixture.reference,
		fixture.amountKobo,
	)

	// The first delivery does the work.
	first := postWebhook(
		t,
		fixture.pool,
		webhookTestSecret,
		body,
		"",
	)

	if first.Code != http.StatusOK {

		t.Fatalf(
			"first delivery answered %d, want 200: %s",
			first.Code,
			first.Body.String(),
		)
	}

	if status := webhookStatus(t, first); status != "processed" {

		t.Fatalf(
			"first delivery was reported as %q, want processed",
			status,
		)
	}

	// The order is now paid.
	paid, err := storage.GetOrderByIDFromDB(
		fixture.pool,
		fixture.order.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the order: %v",
			err,
		)
	}

	if paid.Status != models.OrderPaid {

		t.Fatalf(
			"order status is %q, want %q",
			paid.Status,
			models.OrderPaid,
		)
	}

	if paid.PaidAt == nil {

		t.Fatal(
			"the order is paid but paid_at was not stamped",
		)
	}

	// Now the retry. It is byte-for-byte the same body,
	// with the same signature, which is what Paystack
	// actually sends.
	second := postWebhook(
		t,
		fixture.pool,
		webhookTestSecret,
		body,
		"",
	)

	// The retry must still be answered 200. Answering
	// anything else would invite Paystack to retry
	// again, and again, forever.
	if second.Code != http.StatusOK {

		t.Fatalf(
			"the retry answered %d, want 200: %s",
			second.Code,
			second.Body.String(),
		)
	}

	if status := webhookStatus(t, second); status != "duplicate" {

		t.Fatalf(
			"the retry was reported as %q, want duplicate",
			status,
		)
	}

	// Everything below is the point of the exercise: the
	// retry must have changed nothing.

	if paidAgain, err := storage.GetOrderByIDFromDB(
		fixture.pool,
		fixture.order.ID,
	); err != nil {

		t.Fatalf(
			"could not re-read the order: %v",
			err,
		)

	} else if paidAgain.Status != models.OrderPaid {

		t.Fatalf(
			"the retry moved the order to %q",
			paidAgain.Status,
		)
	}

	paidEvents := countRows(
		t,
		fixture.pool,
		`SELECT COUNT(*) FROM order_events
		 WHERE order_id = $1 AND to_status = $2`,
		fixture.order.ID,
		models.OrderPaid,
	)

	if paidEvents != 1 {

		t.Errorf(
			"expected exactly 1 paid event, found %d",
			paidEvents,
		)
	}

	saleMovements := countRows(
		t,
		fixture.pool,
		`SELECT COUNT(*) FROM inventory_movements
		 WHERE order_id = $1 AND reason = $2`,
		fixture.order.ID,
		models.MovementSale,
	)

	if saleMovements != 1 {

		t.Errorf(
			"expected exactly 1 sale movement, found %d",
			saleMovements,
		)
	}

	// Ten units arrived and two were sold, so eight are
	// left. If the retry had taken stock again there
	// would be six.
	stock, err := storage.GetProductStockFromDB(
		fixture.pool,
		fixture.product.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the stock: %v",
			err,
		)
	}

	if stock != 8 {

		t.Errorf(
			"stock is %d, want 8: the retry took stock a second time",
			stock,
		)
	}

	// Only one webhook_events row should exist, and it
	// should be recorded as processed rather than left
	// half-finished.
	event, err := storage.GetWebhookEventFromDB(
		fixture.pool,
		"7712001",
	)

	if err != nil {

		t.Fatalf(
			"could not read the recorded webhook event: %v",
			err,
		)
	}

	if event.Status != models.WebhookProcessed {

		t.Errorf(
			"the recorded event is %q, want %q",
			event.Status,
			models.WebhookProcessed,
		)
	}
}

// TestPaystackWebhookTreatsANewEventForAPaidOrderAsADuplicate
// covers the same guarantee through the other route.
//
// A provider can send a genuinely new event about a payment
// that has already been confirmed — after an outage, or if
// it reissues the notification for its own reasons. The
// event id is new, so the idempotency key does not catch
// it. What catches it is the order no longer being pending,
// and the test proves that guard works on its own.
func TestPaystackWebhookTreatsANewEventForAPaidOrderAsADuplicate(
	t *testing.T,
) {

	fixture := newWebhookFixture(t)

	first := postWebhook(
		t,
		fixture.pool,
		webhookTestSecret,
		chargeSuccessBody(
			8801001,
			fixture.reference,
			fixture.amountKobo,
		),
		"",
	)

	if status := webhookStatus(t, first); status != "processed" {

		t.Fatalf(
			"the first delivery was reported as %q",
			status,
		)
	}

	// A different event id, so this is not the same
	// delivery arriving twice. It is a new notification
	// about a payment that is already settled.
	second := postWebhook(
		t,
		fixture.pool,
		webhookTestSecret,
		chargeSuccessBody(
			8801002,
			fixture.reference,
			fixture.amountKobo,
		),
		"",
	)

	if second.Code != http.StatusOK {

		t.Fatalf(
			"the second event answered %d, want 200",
			second.Code,
		)
	}

	if status := webhookStatus(t, second); status != "duplicate" {

		t.Fatalf(
			"the second event was reported as %q, want duplicate",
			status,
		)
	}

	saleMovements := countRows(
		t,
		fixture.pool,
		`SELECT COUNT(*) FROM inventory_movements
		 WHERE order_id = $1 AND reason = $2`,
		fixture.order.ID,
		models.MovementSale,
	)

	if saleMovements != 1 {

		t.Errorf(
			"expected exactly 1 sale movement, found %d",
			saleMovements,
		)
	}

	stock, err := storage.GetProductStockFromDB(
		fixture.pool,
		fixture.product.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the stock: %v",
			err,
		)
	}

	if stock != 8 {

		t.Errorf(
			"stock is %d, want 8",
			stock,
		)
	}
}

// TestPaystackWebhookRejectsABadSignature checks that a
// forged payment cannot buy anything.
//
// This endpoint is the one route on the whole API that
// anybody on the internet may call, because Paystack has
// no session cookie. Without the signature check, anyone
// who found the URL could post "payment succeeded" and
// receive goods for free.
func TestPaystackWebhookRejectsABadSignature(t *testing.T) {

	fixture := newWebhookFixture(t)

	body := chargeSuccessBody(
		9901001,
		fixture.reference,
		fixture.amountKobo,
	)

	cases := []struct {
		name string

		signature string
	}{
		{
			name: "a signature made with the wrong key",

			signature: utils.ComputePaystackSignature(
				"sk_test_the_attackers_key",
				body,
			),
		},

		{
			name: "no signature at all",

			signature: "none",
		},

		{
			name: "nonsense instead of a signature",

			signature: "definitely-not-a-signature",
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			recorder := postWebhook(
				t,
				fixture.pool,
				webhookTestSecret,
				body,
				testCase.signature,
			)

			if recorder.Code != http.StatusUnauthorized {

				t.Fatalf(
					"answered %d, want 401",
					recorder.Code,
				)
			}
		})
	}

	// Nothing at all should have happened: no order
	// change, no stock taken, and no event recorded.
	order, err := storage.GetOrderByIDFromDB(
		fixture.pool,
		fixture.order.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the order: %v",
			err,
		)
	}

	if order.Status != models.OrderPending {

		t.Errorf(
			"a forged webhook moved the order to %q",
			order.Status,
		)
	}

	stock, err := storage.GetProductStockFromDB(
		fixture.pool,
		fixture.product.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the stock: %v",
			err,
		)
	}

	if stock != 10 {

		t.Errorf(
			"a forged webhook took stock: %d left, want 10",
			stock,
		)
	}

	events := countRows(
		t,
		fixture.pool,
		`SELECT COUNT(*) FROM webhook_events
		 WHERE provider_event_id = $1`,
		"9901001",
	)

	if events != 0 {

		t.Errorf(
			"a forged webhook was recorded %d times",
			events,
		)
	}
}

// TestPaystackWebhookRejectsATamperedAmount checks the
// anti-tamper comparison.
//
// A payload can carry a valid signature and still claim
// the wrong amount, if the payment really was for less
// than the order. Paying a shilling for a car must not
// settle the order, so the amount is compared against the
// order total before anything is confirmed.
func TestPaystackWebhookRejectsATamperedAmount(t *testing.T) {

	fixture := newWebhookFixture(t)

	// One kobo instead of fifty-one naira.
	body := chargeSuccessBody(
		6601001,
		fixture.reference,
		1,
	)

	recorder := postWebhook(
		t,
		fixture.pool,
		webhookTestSecret,
		body,
		"",
	)

	// The handler answers 500 so that Paystack retries,
	// which is right: this may be a genuine problem with
	// the payment that somebody needs to look at. It
	// must not be answered 200, which would tell the
	// provider the matter is settled.
	if recorder.Code != http.StatusInternalServerError {

		t.Fatalf(
			"answered %d, want 500: %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	order, err := storage.GetOrderByIDFromDB(
		fixture.pool,
		fixture.order.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the order: %v",
			err,
		)
	}

	if order.Status != models.OrderPending {

		t.Errorf(
			"an underpaid order became %q",
			order.Status,
		)
	}

	stock, err := storage.GetProductStockFromDB(
		fixture.pool,
		fixture.product.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the stock: %v",
			err,
		)
	}

	if stock != 10 {

		t.Errorf(
			"an underpaid order took stock: %d left, want 10",
			stock,
		)
	}

	// The event is recorded as failed, so a retry is
	// allowed to take it over and a human can find out
	// what happened.
	event, err := storage.GetWebhookEventFromDB(
		fixture.pool,
		"6601001",
	)

	if err != nil {

		t.Fatalf(
			"the failed event was not recorded: %v",
			err,
		)
	}

	if event.Status != models.WebhookFailed {

		t.Errorf(
			"the event is recorded as %q, want %q",
			event.Status,
			models.WebhookFailed,
		)
	}

	if event.ErrorMessage == "" {

		t.Error(
			"the failed event has no error message explaining why",
		)
	}
}

// TestPaystackWebhookRecordsButIgnoresUnknownEvents checks
// that an event this application does not act on is still
// recorded and still answered 200.
//
// Recording it means nothing is silently thrown away.
// Answering 200 means Paystack stops retrying something
// that will never be acted on.
func TestPaystackWebhookRecordsButIgnoresUnknownEvents(t *testing.T) {

	fixture := newWebhookFixture(t)

	body := []byte(
		`{"event":"subscription.create","data":{"id":5501001,"reference":"unrelated"}}`,
	)

	recorder := postWebhook(
		t,
		fixture.pool,
		webhookTestSecret,
		body,
		"",
	)

	if recorder.Code != http.StatusOK {

		t.Fatalf(
			"answered %d, want 200: %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	if status := webhookStatus(t, recorder); status != "ignored" {

		t.Fatalf(
			"reported as %q, want ignored",
			status,
		)
	}

	// It was still written down.
	event, err := storage.GetWebhookEventFromDB(
		fixture.pool,
		"5501001",
	)

	if err != nil {

		t.Fatalf(
			"the ignored event was not recorded: %v",
			err,
		)
	}

	if event.EventType != "subscription.create" {

		t.Errorf(
			"the recorded event type is %q",
			event.EventType,
		)
	}

	order, err := storage.GetOrderByIDFromDB(
		fixture.pool,
		fixture.order.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the order: %v",
			err,
		)
	}

	if order.Status != models.OrderPending {

		t.Errorf(
			"an unrelated event moved the order to %q",
			order.Status,
		)
	}
}

// TestVerifyThenWebhookIsStillProcessedOnce checks the
// recovery path and the webhook path cannot both confirm
// the same payment.
//
// A lost webhook is recovered by asking Paystack directly,
// and that store's confirmation goes through the same
// guarded function as the webhook. Whichever arrives
// second must find the work already done.
func TestVerifyThenWebhookIsStillProcessedOnce(t *testing.T) {

	fixture := newWebhookFixture(t)

	// The recovery path runs first, as though the webhook
	// had been lost.
	confirmed, err := storage.ConfirmPaymentByReferenceInDB(
		fixture.pool,
		fixture.reference,
		fixture.amountKobo,
		"Payment confirmed by Paystack verify",
	)

	if err != nil {

		t.Fatalf(
			"the verify path failed: %v",
			err,
		)
	}

	if confirmed.Status != models.OrderPaid {

		t.Fatalf(
			"the verify path left the order as %q",
			confirmed.Status,
		)
	}

	// The webhook now turns up late.
	recorder := postWebhook(
		t,
		fixture.pool,
		webhookTestSecret,
		chargeSuccessBody(
			4401001,
			fixture.reference,
			fixture.amountKobo,
		),
		"",
	)

	if recorder.Code != http.StatusOK {

		t.Fatalf(
			"the late webhook answered %d, want 200",
			recorder.Code,
		)
	}

	if status := webhookStatus(t, recorder); status != "duplicate" {

		t.Fatalf(
			"the late webhook was reported as %q, want duplicate",
			status,
		)
	}

	saleMovements := countRows(
		t,
		fixture.pool,
		`SELECT COUNT(*) FROM inventory_movements
		 WHERE order_id = $1 AND reason = $2`,
		fixture.order.ID,
		models.MovementSale,
	)

	if saleMovements != 1 {

		t.Errorf(
			"expected exactly 1 sale movement, found %d",
			saleMovements,
		)
	}

	stock, err := storage.GetProductStockFromDB(
		fixture.pool,
		fixture.product.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the stock: %v",
			err,
		)
	}

	if stock != 8 {

		t.Errorf(
			"stock is %d, want 8",
			stock,
		)
	}
}
