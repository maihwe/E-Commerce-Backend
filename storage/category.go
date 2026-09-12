package storage

import (
	"context"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateCategoryInDB adds a new category.
func CreateCategoryInDB(
	pool *pgxpool.Pool,
	category models.Category,
) (models.Category, error) {

	err := pool.QueryRow(
		context.Background(),
		`
		INSERT INTO categories (name, slug)
		VALUES ($1, $2)
		RETURNING id, name, slug, created_at
		`,
		category.Name,
		category.Slug,
	).Scan(
		&category.ID,
		&category.Name,
		&category.Slug,
		&category.CreatedAt,
	)

	if err != nil {
		return models.Category{}, err
	}

	return category, nil
}

// ListCategoriesFromDB returns every category,
// ordered by name so the catalog menu is stable.
func ListCategoriesFromDB(
	pool *pgxpool.Pool,
) ([]models.Category, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT
			c.id,
			c.name,
			c.slug,
			c.created_at,
			COUNT(p.id) AS product_count
		FROM categories c
		LEFT JOIN products p
			ON p.category_id = c.id
			AND p.is_active = TRUE
		GROUP BY c.id, c.name, c.slug, c.created_at
		ORDER BY c.name
		`,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	categories := make([]models.Category, 0)

	for rows.Next() {

		var category models.Category

		err := rows.Scan(
			&category.ID,
			&category.Name,
			&category.Slug,
			&category.CreatedAt,
			&category.ProductCount,
		)

		if err != nil {
			return nil, err
		}

		categories = append(categories, category)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return categories, nil
}

// GetCategoryByIDFromDB finds one category.
func GetCategoryByIDFromDB(
	pool *pgxpool.Pool,
	id int,
) (models.Category, error) {

	var category models.Category

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT id, name, slug, created_at
		FROM categories
		WHERE id = $1
		`,
		id,
	).Scan(
		&category.ID,
		&category.Name,
		&category.Slug,
		&category.CreatedAt,
	)

	if err != nil {
		return models.Category{}, err
	}

	return category, nil
}
