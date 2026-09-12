package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"e-commerce-backend/models"
	"e-commerce-backend/storage"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// couponRequest is the body accepted when an admin
// creates a coupon.
type couponRequest struct {

	Code string `json:"code"`

	// DiscountType is "percentage" or "fixed".
	DiscountType string `json:"discount_type"`

	DiscountValue float64 `json:"discount_value"`

	MinOrderAmount float64 `json:"min_order_amount"`

	// MaxUses is a pointer because nil means unlimited,
	// which is different from zero.
	MaxUses *int `json:"max_uses"`

	// StartsAt and EndsAt are optional RFC3339
	// timestamps.
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`

	IsActive *bool `json:"is_active"`
}

// CreateCouponHandler adds a discount code.
//
// Only an admin may call this: a coupon is money, and a
// seller who could mint their own could discount the
// whole marketplace.
func CreateCouponHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if !RequireAdmin(pool, w, r) {
			return
		}

		var request couponRequest

		if !decodeJSONBody(w, r, &request) {
			return
		}

		coupon, rejection := buildCoupon(request)

		if rejection != "" {

			writeError(
				w,
				http.StatusBadRequest,
				rejection,
			)

			return
		}

		created, err := storage.CreateCouponInDB(
			pool,
			coupon,
		)

		if err != nil {

			if strings.Contains(
				err.Error(),
				"duplicate key",
			) {

				writeError(
					w,
					http.StatusConflict,
					"That coupon code already exists",
				)

				return
			}

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not create the coupon",
			)

			return
		}

		writeJSON(w, http.StatusCreated, created)
	}
}

// ListCouponsHandler returns every coupon.
func ListCouponsHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if !RequireAdmin(pool, w, r) {
			return
		}

		coupons, err := storage.ListCouponsFromDB(pool)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load coupons",
			)

			return
		}

		writeJSON(w, http.StatusOK, coupons)
	}
}

// ValidateCouponHandler previews what a coupon would take
// off an order.
//
// It applies the coupon to the shopper's current cart, so
// the answer is a real number rather than a guess. This is
// what lets a cart page say "you save ₦500" before the
// shopper reaches checkout.
//
// The rules live in models.RejectCoupon, which the
// checkout path calls too, so a coupon that previews
// successfully cannot then be refused at checkout.
func ValidateCouponHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		var request struct {
			Code string `json:"code"`
		}

		if !decodeJSONBody(w, r, &request) {
			return
		}

		code := models.NormaliseCode(request.Code)

		if code == "" {

			writeError(
				w,
				http.StatusBadRequest,
				"Coupon code is required",
			)

			return
		}

		// The subtotal comes from the shopper's own
		// cart, so a client cannot claim a larger
		// subtotal to unlock a coupon it has not
		// earned.
		cartID, err := storage.GetOrCreateCartInDB(
			pool,
			user.ID,
		)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not open your cart",
			)

			return
		}

		cart, err := storage.GetCartFromDB(pool, cartID)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not load your cart",
			)

			return
		}

		if len(cart.Items) == 0 {

			writeError(
				w,
				http.StatusBadRequest,
				"Your cart is empty",
			)

			return
		}

		coupon, err := storage.GetCouponByCodeFromDB(
			pool,
			code,
		)

		if err != nil {

			if errors.Is(err, storage.ErrCouponNotFound) {

				writeError(
					w,
					http.StatusNotFound,
					"That coupon code is not valid",
				)

				return
			}

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not check the coupon",
			)

			return
		}

		subtotalKobo := utils.ToKobo(cart.Subtotal)

		rejection := models.RejectCoupon(
			coupon,
			subtotalKobo,
			time.Now(),
		)

		if rejection != "" {

			writeJSON(
				w,
				http.StatusOK,
				map[string]any{
					"valid": false,

					"code": coupon.Code,

					"reason": rejection,
				},
			)

			return
		}

		discountKobo := coupon.ComputeDiscountKobo(
			subtotalKobo,
		)

		writeJSON(
			w,
			http.StatusOK,
			map[string]any{
				"valid": true,

				"code": coupon.Code,

				"discount_type": coupon.DiscountType,

				"discount_kobo": discountKobo,

				"discount_amount": utils.FromKobo(
					discountKobo,
				),

				"subtotal_kobo": subtotalKobo,

				"total_kobo": utils.RemoveDiscount(
					subtotalKobo,
					discountKobo,
				),
			},
		)
	}
}

// SetCouponActiveHandler switches a coupon on or off.
func SetCouponActiveHandler(
	pool *pgxpool.Pool,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if !RequireAdmin(pool, w, r) {
			return
		}

		couponID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		var request struct {
			IsActive bool `json:"is_active"`
		}

		if !decodeJSONBody(w, r, &request) {
			return
		}

		coupon, err := storage.SetCouponActiveInDB(
			pool,
			couponID,
			request.IsActive,
		)

		if err != nil {

			if errors.Is(err, storage.ErrCouponNotFound) {

				writeError(
					w,
					http.StatusNotFound,
					"Coupon not found",
				)

				return
			}

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not update the coupon",
			)

			return
		}

		writeJSON(w, http.StatusOK, coupon)
	}
}

// buildCoupon validates a coupon request.
//
// It returns an empty rejection string when the request
// is usable.
func buildCoupon(
	request couponRequest,
) (models.Coupon, string) {

	code := models.NormaliseCode(request.Code)

	if code == "" {
		return models.Coupon{}, "Coupon code is required"
	}

	if len(code) > 40 {

		return models.Coupon{},
			"Coupon code cannot be longer than 40 characters"
	}

	discountType := strings.ToLower(
		strings.TrimSpace(request.DiscountType),
	)

	switch discountType {

	case models.DiscountPercentage:

		if request.DiscountValue <= 0 ||
			request.DiscountValue > 100 {

			return models.Coupon{},
				"A percentage discount must be more than 0 and at most 100"
		}

	case models.DiscountFixed:

		if request.DiscountValue <= 0 {

			return models.Coupon{},
				"A fixed discount must be more than 0"
		}

	default:

		return models.Coupon{},
			"Discount type must be percentage or fixed"
	}

	if request.MinOrderAmount < 0 {

		return models.Coupon{},
			"Minimum order amount cannot be negative"
	}

	if request.MaxUses != nil && *request.MaxUses < 1 {

		return models.Coupon{},
			"Maximum uses must be at least 1, or left out for unlimited"
	}

	if request.StartsAt != nil &&
		request.EndsAt != nil &&
		!request.EndsAt.After(*request.StartsAt) {

		return models.Coupon{},
			"The end date must be after the start date"
	}

	isActive := true

	if request.IsActive != nil {
		isActive = *request.IsActive
	}

	coupon := models.Coupon{
		Code: code,

		DiscountType: discountType,

		DiscountValue: utils.RoundToTwoDecimals(
			request.DiscountValue,
		),

		MinOrderAmount: utils.RoundToTwoDecimals(
			request.MinOrderAmount,
		),

		MaxUses: request.MaxUses,

		StartsAt: request.StartsAt,

		EndsAt: request.EndsAt,

		IsActive: isActive,
	}

	return coupon, ""
}
