// Command query runs one SQL statement against the
// application's database and prints what came back.
//
// It exists because the PostgreSQL this project runs does
// not come with a psql client. That PostgreSQL is the
// embedded server from database/devdb, and the
// distribution it downloads ships the server binaries and
// very little else, so there is no command-line client to
// reach for and none can be installed without root.
//
// Rather than depend on one being present, this uses the
// same pgx driver and the same DATABASE_URL that the
// application itself uses. That has a second benefit: the
// connection settings cannot drift apart from the ones the
// application connects with, which a separately configured
// psql could.
//
// Usage:
//
//	go run ./database/query "SELECT id FROM categories WHERE slug = 'groceries'"
//
// Each row is printed on its own line, with the columns
// separated by a tab. A statement that returns no rows,
// such as an UPDATE without RETURNING, prints the command
// tag and the number of rows it changed, so it still
// reports that it did something rather than printing
// nothing at all.
//
// One caveat worth knowing before relying on it: a numeric
// column arrives in the driver's own representation rather
// than as a decimal string. A query that needs a plain
// number should cast it, for example (total * 100)::bigint.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {

	err := run()

	if err != nil {

		fmt.Fprintln(os.Stderr, "query:", err)

		os.Exit(1)
	}
}

func run() error {

	if len(os.Args) < 2 {

		return fmt.Errorf(
			`usage: go run ./database/query "SELECT ..."`,
		)
	}

	// The statement is normally passed as one quoted
	// argument. Joining whatever arrived means an
	// unquoted statement still works, which is a small
	// kindness given how often a shell eats the quotes
	// before this program ever sees them.
	statement := strings.Join(os.Args[1:], " ")

	url := strings.TrimSpace(
		os.Getenv("DATABASE_URL"),
	)

	if url == "" {

		return fmt.Errorf(
			"DATABASE_URL is not set: run . ~/ecb-env.sh first",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)

	defer cancel()

	conn, err := pgx.Connect(ctx, url)

	if err != nil {

		return fmt.Errorf(
			"could not connect: %w",
			err,
		)
	}

	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx, statement)

	if err != nil {

		return fmt.Errorf(
			"the statement failed: %w",
			err,
		)
	}

	defer rows.Close()

	printed := false

	for rows.Next() {

		values, err := rows.Values()

		if err != nil {

			return fmt.Errorf(
				"could not read a row: %w",
				err,
			)
		}

		printRow(values)

		printed = true
	}

	err = rows.Err()

	if err != nil {

		return fmt.Errorf(
			"reading the results failed: %w",
			err,
		)
	}

	// A statement with no result set has nothing to
	// print above. Reporting how many rows it touched is
	// better than silence, which would look exactly like
	// a statement that matched nothing.
	if !printed {

		tag := rows.CommandTag()

		fmt.Printf(
			"%s %d\n",
			tag.String(),
			tag.RowsAffected(),
		)
	}

	return nil
}

// printRow writes one row as tab-separated values.
//
// A NULL becomes an empty field rather than the text
// "<nil>". Every caller of this program is a shell script
// comparing the output against a string, and an empty
// string is already how a shell says "no value".
func printRow(values []any) {

	fields := make([]string, 0, len(values))

	for _, value := range values {

		if value == nil {

			fields = append(fields, "")

			continue
		}

		fields = append(fields, fmt.Sprint(value))
	}

	fmt.Println(strings.Join(fields, "\t"))
}
