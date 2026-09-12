package storage

import (
	"context"
	"strings"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateUserInDB saves a new account.
//
// If the caller did not choose a role, the account
// becomes a buyer, which matches the column default
// in the users table.
func CreateUserInDB(
	pool *pgxpool.Pool,
	user models.User,
) (models.User, error) {

	email := strings.ToLower(strings.TrimSpace(user.Email))
	name := strings.TrimSpace(user.Name)

	err := pool.QueryRow(
		context.Background(),
		`
		INSERT INTO users (name, email, password_hash, role)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4, ''), 'buyer'))
		RETURNING id, name, email, password_hash, role, created_at
		`,
		name,
		email,
		user.PasswordHash,
		user.Role,
	).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
	)

	if err != nil {
		return models.User{}, err
	}

	return user, nil
}

// GetUserByEmailFromDB finds the account that owns
// an email address.
//
// This is used during login. The password hash comes
// back with the row so the caller can compare it.
func GetUserByEmailFromDB(
	pool *pgxpool.Pool,
	email string,
) (models.User, error) {

	var user models.User

	email = strings.ToLower(strings.TrimSpace(email))

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT id, name, email, password_hash, role, created_at
		FROM users
		WHERE email = $1
		`,
		email,
	).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
	)

	if err != nil {
		return models.User{}, err
	}

	return user, nil
}

// GetUserByIDFromDB finds one account by its ID.
func GetUserByIDFromDB(
	pool *pgxpool.Pool,
	id int,
) (models.User, error) {

	var user models.User

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT id, name, email, password_hash, role, created_at
		FROM users
		WHERE id = $1
		`,
		id,
	).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
	)

	if err != nil {
		return models.User{}, err
	}

	return user, nil
}

// ListUsersInDB returns every account.
//
// Only an admin may call this, and the password hash
// is deliberately left out of the result.
func ListUsersInDB(pool *pgxpool.Pool) ([]models.User, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT id, name, email, role, created_at
		FROM users
		ORDER BY id
		`,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	users := make([]models.User, 0)

	for rows.Next() {

		var user models.User

		err := rows.Scan(
			&user.ID,
			&user.Name,
			&user.Email,
			&user.Role,
			&user.CreatedAt,
		)

		if err != nil {
			return nil, err
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// UpdateUserRoleInDB changes what an account is
// allowed to do.
func UpdateUserRoleInDB(
	pool *pgxpool.Pool,
	userID int,
	role string,
) (models.User, error) {

	var user models.User

	err := pool.QueryRow(
		context.Background(),
		`
		UPDATE users
		SET role = $1
		WHERE id = $2
		RETURNING id, name, email, role, created_at
		`,
		role,
		userID,
	).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.CreatedAt,
	)

	if err != nil {
		return models.User{}, err
	}

	return user, nil
}
