// Package testdb prepares a real PostgreSQL database for
// the test suite.
//
// The tests in this project run against a real PostgreSQL
// server rather than a fake, because the things worth
// testing here are the things a fake would have to
// reimplement and could get wrong: a UNIQUE constraint
// deciding whether a webhook is a duplicate, and row locks
// deciding whether two confirmations can both take the
// same stock.
//
// # One database per test process
//
// Every test process creates its own scratch database,
// named after the database in TEST_DATABASE_URL with the
// process id appended, applies the migrations to it, and
// drops it when the tests finish.
//
// That isolation is not a nicety. `go test ./...` runs the
// test binaries of different packages at the same time, so
// the handlers tests and the storage tests are executing
// concurrently. An earlier version of this file had them
// share one database and rebuild its schema, which meant
// one package could drop the schema while the other was
// halfway through creating tables in it. The failures that
// produced were impressive and completely misleading:
// migrations reporting that a table they had just created
// did not exist.
//
// # TEST_DATABASE_URL is not wiped
//
// The database that variable names is used only as a place
// to connect to, so that CREATE DATABASE and DROP DATABASE
// can be issued. Its own contents are never touched, and
// neither is anything else on the server except the
// scratch databases this package creates.
//
// That is why the variable is separate from DATABASE_URL
// even though it is no longer destructive. The tests need
// permission to create databases, and pointing that at the
// application's own database would be asking for trouble
// for no benefit.
//
// Set it to something like:
//
//	TEST_DATABASE_URL=postgres://postgres@localhost:5432/ecommerce_test?sslmode=disable
//
// When it is not set, every database-backed test skips
// rather than failing, so the pure unit tests still run on
// a machine with no PostgreSQL at all.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"e-commerce-backend/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxIdentifierLength is the longest name PostgreSQL will
// accept. Longer ones are silently truncated by the server,
// which would turn two different names into the same one,
// so names are trimmed here rather than left to chance.
const maxIdentifierLength = 63

// Pool returns a connection to a freshly migrated scratch
// database, or skips the test when none is configured.
//
// The database is created for this process alone and
// dropped again when the test finishes, so two packages
// running at the same time cannot see each other's tables.
func Pool(t *testing.T) *pgxpool.Pool {

	t.Helper()

	baseURL := strings.TrimSpace(
		os.Getenv("TEST_DATABASE_URL"),
	)

	if baseURL == "" {

		t.Skip(
			"TEST_DATABASE_URL is not set, so database-backed tests are skipped",
		)
	}

	// Creating a database copies a template, which is
	// slower than anything else this package does, so the
	// timeout is generous.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		120*time.Second,
	)

	defer cancel()

	name, err := scratchDatabaseName(baseURL)

	if err != nil {
		t.Fatalf("could not work out a scratch database name: %v", err)
	}

	createScratchDatabase(t, ctx, baseURL, name)

	scratchURL, err := databaseURLFor(baseURL, name)

	if err != nil {
		t.Fatalf("could not build the scratch database URL: %v", err)
	}

	pool, err := pgxpool.New(ctx, scratchURL)

	if err != nil {

		t.Fatalf(
			"could not connect to the scratch database %s: %v",
			name,
			err,
		)
	}

	// Cleanups run last-registered-first, so registering
	// the drop before the close means the pool is closed
	// first and the database can then be dropped without
	// connections still attached to it.
	t.Cleanup(func() {
		dropScratchDatabase(t, baseURL, name)
	})

	t.Cleanup(pool.Close)

	err = pool.Ping(ctx)

	if err != nil {

		t.Fatalf(
			"the scratch database %s did not answer: %v",
			name,
			err,
		)
	}

	applyMigrations(t, ctx, pool)

	return pool
}

// scratchDatabaseName builds a name that no other test
// process will pick.
//
// The process id is what makes it unique: two packages
// running at the same time are two processes, so they get
// two names.
func scratchDatabaseName(baseURL string) (string, error) {

	parsed, err := url.Parse(baseURL)

	if err != nil {
		return "", err
	}

	if parsed.Scheme == "" {

		return "", errors.New(
			"TEST_DATABASE_URL must be a URL, such as postgres://user@host:5432/database",
		)
	}

	base := strings.TrimPrefix(parsed.Path, "/")

	if base == "" {
		base = "postgres"
	}

	suffix := fmt.Sprintf("_%d", os.Getpid())

	// Trim the base rather than the suffix, so that the
	// part making the name unique is never the part that
	// gets cut off.
	if len(base)+len(suffix) > maxIdentifierLength {

		base = base[:maxIdentifierLength-len(suffix)]
	}

	return base + suffix, nil
}

// databaseURLFor returns the same connection settings,
// aimed at a different database.
func databaseURLFor(
	baseURL string,
	name string,
) (string, error) {

	parsed, err := url.Parse(baseURL)

	if err != nil {
		return "", err
	}

	parsed.Path = "/" + name

	return parsed.String(), nil
}

// createScratchDatabase makes an empty database to work in.
//
// Any leftover database of the same name is dropped first.
// The name contains a process id, and process ids get
// reused, so a run that was killed before it could clean up
// would otherwise leave a database behind that a later run
// would inherit with its old tables still in it.
func createScratchDatabase(
	t *testing.T,
	ctx context.Context,
	baseURL string,
	name string,
) {

	t.Helper()

	admin, err := pgx.Connect(ctx, baseURL)

	if err != nil {

		t.Fatalf(
			"could not connect to TEST_DATABASE_URL: %v",
			err,
		)
	}

	defer admin.Close(context.Background())

	identifier := pgx.Identifier{name}.Sanitize()

	_, err = admin.Exec(
		ctx,
		`DROP DATABASE IF EXISTS `+identifier,
	)

	if err != nil {

		t.Fatalf(
			"could not drop the leftover scratch database %s: %v",
			name,
			err,
		)
	}

	_, err = admin.Exec(ctx, `CREATE DATABASE `+identifier)

	if err != nil {

		t.Fatalf(
			"could not create the scratch database %s: %v",
			name,
			err,
		)
	}
}

// dropScratchDatabase removes the database this process
// used.
//
// A failure here is logged rather than fatal. The tests
// have already run by this point, so a database that could
// not be dropped is a mess to clean up later, not a reason
// to report a passing suite as a failure.
func dropScratchDatabase(
	t *testing.T,
	baseURL string,
	name string,
) {

	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)

	defer cancel()

	admin, err := pgx.Connect(ctx, baseURL)

	if err != nil {

		t.Logf(
			"could not connect to drop the scratch database %s: %v",
			name,
			err,
		)

		return
	}

	defer admin.Close(context.Background())

	_, err = admin.Exec(
		ctx,
		`DROP DATABASE IF EXISTS `+
			pgx.Identifier{name}.Sanitize(),
	)

	if err != nil {

		t.Logf(
			"could not drop the scratch database %s: %v",
			name,
			err,
		)
	}
}

// applyMigrations runs every migration in filename order.
//
// The files are numbered precisely so that this ordering is
// the correct one: each migration may only reference tables
// that an earlier number has already created.
func applyMigrations(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {

	t.Helper()

	entries, err := fs.ReadDir(
		database.Migrations,
		"migrations",
	)

	if err != nil {

		t.Fatalf(
			"could not read the embedded migrations: %v",
			err,
		)
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {

		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		names = append(names, entry.Name())
	}

	if len(names) == 0 {
		t.Fatal("no migrations were found")
	}

	sort.Strings(names)

	for _, name := range names {

		sql, err := database.Migrations.ReadFile(
			"migrations/" + name,
		)

		if err != nil {

			t.Fatalf(
				"could not read migration %s: %v",
				name,
				err,
			)
		}

		_, err = pool.Exec(ctx, string(sql))

		if err != nil {

			t.Fatalf(
				"migration %s failed: %v",
				name,
				err,
			)
		}
	}
}
