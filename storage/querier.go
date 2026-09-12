package storage

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// dbQuerier is the set of database operations this
// package needs.
//
// Both *pgxpool.Pool and pgx.Tx satisfy it, so a storage
// function can be written once and used either on its own
// or as part of a larger transaction. That is what lets
// order confirmation run several writes atomically
// without a second copy of the SQL.
type dbQuerier interface {

	Query(
		context.Context,
		string,
		...any,
	) (pgx.Rows, error)

	QueryRow(
		context.Context,
		string,
		...any,
	) pgx.Row

	Exec(
		context.Context,
		string,
		...any,
	) (pgconn.CommandTag, error)
}
