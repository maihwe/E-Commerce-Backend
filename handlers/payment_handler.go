package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"e-commerce-backend/models"
	"e-commerce-backend/services"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// paymentTargetResponse is what the client needs in order
// to send a shopper to Paystack.
type paymentTargetResponse struct {

	OrderID int `json:"order_id"`

	// AuthorizationURL is the page to open in a browser.
	//
	// The client redirects the shopper here. No card
	// details ever pass through this server, which is why
	// the integration is a redirect rather than a form.
	AuthorizationURL string `json:"authorization_url"`

	Reference string `json:"reference"`

	// AmountKobo is included so the client can show the
	// amount before the shopper leaves for Paystack.
	AmountKobo int64 `json:"amount_kobo"`
}

// PayOrderHandler starts a payment for an order.
//
// It does not charge anything. It asks Paystack to create a
// payment, and returns the page the shopper must visit. The
// money moves on Paystack's own site, and confirmation
// arrives later by webhook.
func PayOrderHandler(
	pool *pgxpool.Pool,
	paystack *services.PaystackClient,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, err := GetAuthenticatedUser(pool, r)

		if err != nil {

			writeError(
				w,
				http.StatusUnauthorized,
				"Authentication required",
			)

			return
		}

		orderID, err := strconv.Atoi(
			r.PathValue("id"),
		)

		if err != nil || orderID <= 0 {

			writeError(
				w,
				http.StatusBadRequest,
				"Invalid order ID",
			)

			return
		}

		order, err := storage.GetOrderByIDFromDB(
			pool,
			orderID,
		)

		// Only the buyer may start a payment. A seller
		// must not be able to trigger a charge against a
		// customer, and nor must a stranger.
		if err != nil || order.BuyerID != user.ID {

			writeError(
				w,
				http.StatusNotFound,
				"Order not found",
			)

			return
		}

		if order.Status != models.OrderPending {

			writeError(
				w,
				http.StatusConflict,
				"Only a pending order can be paid for",
			)

			return
		}

		// Build a fresh reference for this attempt.
		reference, err :=
			utils.GeneratePaymentReference(orderID)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not create a payment reference",
			)

			return
		}

		amountKobo := utils.ToKobo(order.Total)

		target, err := paystack.InitializeTransaction(
			r.Context(),
			services.InitializeTransactionRequest{
				Email: user.Email,

				Amount: amountKobo,

				Reference: reference,

				// The order number travels with the
				// payment so that it can be seen in the
				// Paystack dashboard.
				Metadata: map[string]string{
					"order_id": strconv.Itoa(
						order.ID,
					),
				},
			},
		)

		if err != nil {

			writeError(
				w,
				http.StatusBadGateway,
				"Could not reach the payment provider: "+
					err.Error(),
			)

			return
		}

		// Store the reference so that the webhook can
		// find this order.
		//
		// The update only applies while the order is
		// still pending, so a second attempt cannot
		// overwrite the reference of an order that has
		// already been paid.
		_, err = storage.SetOrderPaymentReferenceInDB(
			pool,
			orderID,
			user.ID,
			target.Reference,
		)

		if err != nil {

			writeError(
				w,
				http.StatusConflict,
				"Could not attach the payment to the order",
			)

			return
		}

		writeJSON(
			w,
			http.StatusOK,
			paymentTargetResponse{
				OrderID: orderID,

				AuthorizationURL: target.AuthorizationURL,

				Reference: target.Reference,

				AmountKobo: amountKobo,
			},
		)
	}
}

// VerifyPaymentHandler asks Paystack what really happened
// to a payment.
//
// This is the recovery path for a webhook that never
// arrived. Paystack itself recommends verifying on the
// server, because a webhook is something an outsider can
// try to forge, whereas this is this server asking the
// provider directly.
//
// It also reuses the same guarded confirmation as the
// webhook, so running both for one payment changes nothing
// the second time.
func VerifyPaymentHandler(
	pool *pgxpool.Pool,
	paystack *services.PaystackClient,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, err := GetAuthenticatedUser(pool, r)

		if err != nil {

			writeError(
				w,
				http.StatusUnauthorized,
				"Authentication required",
			)

			return
		}

		reference := r.PathValue("reference")

		if reference == "" {

			writeError(
				w,
				http.StatusBadRequest,
				"Invalid payment reference",
			)

			return
		}

		order, err :=
			storage.FindOrderByPaymentReferenceFromDB(
				pool,
				reference,
			)

		// Only the buyer or an admin may check a payment.
		if err != nil ||
			(order.BuyerID != user.ID &&
				user.Role != models.RoleAdmin) {

			writeError(
				w,
				http.StatusNotFound,
				"Payment not found",
			)

			return
		}

		verified, err := paystack.VerifyTransaction(
			r.Context(),
			reference,
		)

		if err != nil {

			writeError(
				w,
				http.StatusBadGateway,
				"Could not reach the payment provider: "+
					err.Error(),
			)

			return
		}

		// Only a genuinely successful payment is acted
		// on. Anything else is reported back, leaving the
		// order pending so the shopper can try again.
		if verified.Status == "success" {

			order, err =
				storage.ConfirmPaymentByReferenceInDB(
					pool,
					reference,
					verified.Amount,
					"Payment confirmed by Paystack verify",
				)

			if err != nil {

				// The payment went through but the
				// order cannot be fulfilled, usually
				// because the stock ran out between
				// checkout and payment. Reporting this
				// clearly matters: somebody has paid
				// and needs a refund.
				if errors.Is(
					err,
					storage.ErrInsufficientStock,
				) {

					writeError(
						w,
						http.StatusConflict,
						"Payment succeeded but the stock ran out. The order needs a refund.",
					)

					return
				}

				if errors.Is(
					err,
					storage.ErrAmountMismatch,
				) {

					writeError(
						w,
						http.StatusConflict,
						"The amount paid does not match the order total",
					)

					return
				}

				writeError(
					w,
					http.StatusInternalServerError,
					"Could not confirm the payment",
				)

				return
			}
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"provider_status": verified.Status,

				"amount_kobo": verified.Amount,

				"order": order,
			},
		)
	}
}
