package database

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a connection pool to our PostgreSQL database.
//
// The connection string is read from the DATABASE_URL
// environment variable so that credentials never live
// inside the source code.
func Connect() (*pgxpool.Pool, error) {

	// Read the database connection string
	// from the DATABASE_URL environment variable.
	databaseURL := os.Getenv("DATABASE_URL")

	// Make sure the connection string exists.
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	// Create a connection pool.
	//
	// A pool keeps a small number of connections open
	// and reuses them, which is much faster than
	// connecting again for every request.
	pool, err := pgxpool.New(
		context.Background(),
		databaseURL,
	)

	// Return the error if the pool could not be created.
	if err != nil {
		return nil, err
	}

	// Test that the database is actually reachable.
	//
	// Creating a pool does not connect immediately,
	// so we ping to be sure the credentials and
	// network are working before the server starts.
	err = pool.Ping(context.Background())

	if err != nil {

		// Close the pool we just created so we do
		// not leak connections when the ping fails.
		pool.Close()

		return nil, err
	}

	// Return the working connection pool.
	return pool, nil
}
