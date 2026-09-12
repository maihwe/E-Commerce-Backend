package storage

import (
	"context"
	"errors"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// couponColumns is the SELECT list for coupons.
//
// used_count is not a column in the coupons table. It is
// counted from coupon_redemptions every time a coupon is
// read, so the number can never disagree with the
// redemptions that produced it. The same reasoning as the
// inventory ledger.
const couponColumns = `
	c.id,
	c.code,
	c.discount_type,
	c.discount_value,
	c.min_order_amount,
	c.max_uses,
	(
		SELECT COUNT(*)
		FROM coupon_redemptions r
		WHERE r.coupon_id = c.id
	) AS used_count,
	c.starts_at,
	c.ends_at,
	c.is_active,
	c.created_at
`

// scanCoupon reads the columns listed in couponColumns.
func scanCoupon(scanner rowScanner) (models.Coupon, error) {

	var coupon models.Coupon

	err := scanner.Scan(
		&coupon.ID,
		&coupon.Code,
		&coupon.DiscountType,
		&coupon.DiscountValue,
		&coupon.MinOrderAmount,
		&coupon.MaxUses,
		&coupon.UsedCount,
		&coupon.StartsAt,
		&coupon.EndsAt,
		&coupon.IsActive,
		&coupon.CreatedAt,
	)

	if err != nil {
		return models.Coupon{}, err
	}

	return coupon, nil
}

// CreateCouponInDB adds a discount code.
func CreateCouponInDB(
	pool *pgxpool.Pool,
	coupon models.Coupon,
) (models.Coupon, error) {

	err := pool.QueryRow(
		context.Background(),
		`
		INSERT INTO coupons
			(
				code,
				discount_type,
				discount_value,
				min_order_amount,
				max_uses,
				starts_at,
				ends_at,
				is_active
			)
		VALUES
			($1, $2, $3::numeric, $4::numeric, $5, $6, $7, $8)
		RETURNING id
		`,
		models.NormaliseCode(coupon.Code),
		coupon.DiscountType,
		coupon.DiscountValue,
		coupon.MinOrderAmount,
		coupon.MaxUses,
		coupon.StartsAt,
		coupon.EndsAt,
		coupon.IsActive,
	).Scan(&coupon.ID)

	if err != nil {
		return models.Coupon{}, err
	}

	return GetCouponByIDFromDB(pool, coupon.ID)
}

// GetCouponByIDFromDB finds one coupon.
func GetCouponByIDFromDB(
	pool *pgxpool.Pool,
	id int,
) (models.Coupon, error) {

	row := pool.QueryRow(
		context.Background(),
		`
		SELECT `+couponColumns+`
		FROM coupons c
		WHERE c.id = $1
		`,
		id,
	)

	return scanCoupon(row)
}

// GetCouponByCodeFromDB finds a coupon by the code a
// shopper typed.
//
// The code is normalised first, so case and stray spaces
// do not matter.
func GetCouponByCodeFromDB(
	pool *pgxpool.Pool,
	code string,
) (models.Coupon, error) {

	row := pool.QueryRow(
		context.Background(),
		`
		SELECT `+couponColumns+`
		FROM coupons c
		WHERE c.code = $1
		`,
		models.NormaliseCode(code),
	)

	return scanCoupon(row)
}

// loadCouponByCodeInTx finds a coupon using an existing
// transaction.
func loadCouponByCodeInTx(
	ctx context.Context,
	querier dbQuerier,
	code string,
) (models.Coupon, error) {

	row := querier.QueryRow(
		ctx,
		`
		SELECT `+couponColumns+`
		FROM coupons c
		WHERE c.code = $1
		`,
		models.NormaliseCode(code),
	)

	coupon, err := scanCoupon(row)

	if err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			return models.Coupon{}, ErrCouponNotFound
		}

		return models.Coupon{}, err
	}

	return coupon, nil
}

// ListCouponsFromDB returns every coupon, newest first.
//
// Only an admin may call this.
func ListCouponsFromDB(
	pool *pgxpool.Pool,
) ([]models.Coupon, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT `+couponColumns+`
		FROM coupons c
		ORDER BY c.created_at DESC, c.id DESC
		`,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	coupons := make([]models.Coupon, 0)

	for rows.Next() {

		coupon, err := scanCoupon(rows)

		if err != nil {
			return nil, err
		}

		coupons = append(coupons, coupon)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return coupons, nil
}

// SetCouponActiveInDB switches a coupon on or off.
func SetCouponActiveInDB(
	pool *pgxpool.Pool,
	couponID int,
	isActive bool,
) (models.Coupon, error) {

	tag, err := pool.Exec(
		context.Background(),
		`
		UPDATE coupons
		SET is_active = $1
		WHERE id = $2
		`,
		isActive,
		couponID,
	)

	if err != nil {
		return models.Coupon{}, err
	}

	if tag.RowsAffected() == 0 {
		return models.Coupon{}, ErrCouponNotFound
	}

	return GetCouponByIDFromDB(pool, couponID)
}
