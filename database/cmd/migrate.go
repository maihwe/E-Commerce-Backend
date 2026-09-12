package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// This small program applies one migration file at a time.
//
// Usage:
//
//	go run ./database/cmd 001_create_users.sql
//
// Each migration file is written so that it can be read
// on its own, which makes the database history easy to follow.
func main() {

	// Read the PostgreSQL connection string
	// from the environment.
	databaseURL := os.Getenv("DATABASE_URL")

	// Stop if the connection string is missing.
	if databaseURL == "" {
		fmt.Println("DATABASE_URL is not set")
		return
	}

	// Make sure a migration file was provided.
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run ./database/cmd <migration-file>")
		return
	}

	// Get the migration file from the command line.
	migrationFile := os.Args[1]

	// Connect to PostgreSQL.
	conn, err := pgx.Connect(
		context.Background(),
		databaseURL,
	)

	if err != nil {
		fmt.Println("Connection failed:", err)
		return
	}

	// Close the connection when the program finishes.
	defer conn.Close(context.Background())

	// Read the selected migration file.
	sqlBytes, err := os.ReadFile(
		"database/migrations/" + migrationFile,
	)

	if err != nil {
		fmt.Println("Could not read migration:", err)
		return
	}

	// Convert the file contents from bytes to a string.
	sql := strings.TrimSpace(string(sqlBytes))

	// Execute the migration against PostgreSQL.
	_, err = conn.Exec(
		context.Background(),
		sql,
	)

	if err != nil {
		fmt.Println("Migration failed:", err)
		return
	}

	// Confirm that the migration succeeded.
	fmt.Println("Migration completed successfully:", migrationFile)
}
