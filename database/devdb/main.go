// Command devdb runs a real PostgreSQL server without
// needing to install one.
//
// It exists because this project was developed on a machine
// where PostgreSQL is not installed and cannot be, since
// installing it needs root and the account has no sudo. A
// database server does not actually need root to run: it
// needs a directory it can write to and a port it can
// listen on. The embedded-postgres library used here
// downloads a complete PostgreSQL distribution, runs initdb
// and starts the server inside your home directory, as you.
//
// It is the real server, not a stand-in. Every constraint,
// row lock, and ON CONFLICT clause behaves exactly as it
// would against a system installation, which matters a
// great deal here: the two things this project is about are
// a UNIQUE constraint deciding whether a webhook is a
// duplicate and a FOR UPDATE lock deciding whether two
// buyers can both take the last unit. A fake database would
// have to reimplement both, and a test of a reimplementation
// proves nothing.
//
// Usage:
//
//	go run ./database/devdb
//
// It starts the server, creates the ecommerce and
// ecommerce_test databases, applies the migrations to the
// first, writes ~/ecb-env.sh holding the connection
// strings, and then waits. Press Ctrl-C to stop it.
//
// In another terminal:
//
//	. ~/ecb-env.sh
//	go test ./...
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"e-commerce-backend/database"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"
)

const (

	// defaultPort is deliberately not 5432. That port is
	// where a system PostgreSQL would be, and using it
	// would mean this helper and an installed server could
	// not coexist.
	defaultPort = 5433

	// The credentials for the cluster this program owns.
	//
	// They are not secrets. The server listens only on
	// localhost, it exists only while this program runs,
	// and its data directory is inside your home
	// directory. They are fixed rather than random so the
	// connection string in ~/ecb-env.sh stays the same
	// from one run to the next.
	defaultUser = "ecb"

	defaultPassword = "ecb"

	// mainDatabase is the one the application uses.
	// embedded-postgres creates exactly one database when
	// it initialises, so this is the one it is asked for.
	mainDatabase = "ecommerce"

	// testDatabase is the one the test suite is allowed to
	// wipe. It is created separately because the suite
	// drops and rebuilds its schema on every run, and that
	// must never happen to the application's database.
	testDatabase = "ecommerce_test"
)

func main() {

	err := run()

	if err != nil {

		fmt.Fprintln(os.Stderr, "devdb:", err)

		os.Exit(1)
	}
}

func run() error {

	ctx := context.Background()

	base, err := baseDirectory()

	if err != nil {
		return err
	}

	port := portFromEnv()

	// The data directory is deliberately NOT inside the
	// runtime path.
	//
	// embedded-postgres deletes its runtime path on every
	// start, and by default the data directory lives
	// inside it. Pointing them at different places is what
	// lets the database keep its contents between runs
	// instead of being rebuilt from nothing each time.
	runtimePath := filepath.Join(base, "run")

	config := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(uint32(port)).
		Username(defaultUser).
		Password(defaultPassword).
		Database(mainDatabase).
		CachePath(filepath.Join(base, "cache")).
		BinariesPath(filepath.Join(base, "bin")).
		RuntimePath(runtimePath).
		DataPath(filepath.Join(base, "data")).
		StartParameters(
			map[string]string{
				// PostgreSQL normally puts its Unix
				// socket wherever it was compiled to,
				// which on this machine is a directory
				// owned by root. It would fail to start
				// there. Since the runtime path is ours
				// and is created before the server
				// starts, asking for the socket to go
				// there removes a dependency on how
				// this particular build was configured.
				"unix_socket_directories": runtimePath,
			},
		).
		StartTimeout(120 * time.Second).
		Logger(os.Stdout)

	server := embeddedpostgres.NewDatabase(config)

	fmt.Println("starting PostgreSQL")

	fmt.Println("the first run downloads a PostgreSQL distribution and extracts it")

	fmt.Println("data directory:", filepath.Join(base, "data"))

	err = server.Start()

	if err != nil {
		return fmt.Errorf(
			"could not start PostgreSQL: %w",
			err,
		)
	}

	// The deferred stop runs when run returns, whether that
	// is normally or because something failed. That is the
	// point of putting it here rather than after the wait:
	// a failure below must not leave a server running.
	defer func() {

		fmt.Println("stopping PostgreSQL")

		stopErr := server.Stop()

		if stopErr != nil {

			fmt.Fprintln(
				os.Stderr,
				"could not stop PostgreSQL cleanly:",
				stopErr,
			)
		}
	}()

	// The server's own connection, used for the two setup
	// steps. The application's database is created by
	// embedded-postgres during initialisation; the test one
	// is not, so it is made here.
	mainURL := connectionURL(port, mainDatabase)

	err = ensureDatabase(ctx, mainURL, testDatabase)

	if err != nil {
		return err
	}

	applied, err := ensureSchema(ctx, mainURL)

	if err != nil {
		return err
	}

	err = writeEnvFile(port)

	if err != nil {
		return err
	}

	printSummary(port, applied)

	// Wait for Ctrl-C. Nothing below this line runs until
	// the user asks for the server to stop, which is what
	// keeps this program alive as the database's owner.
	stop := make(chan os.Signal, 1)

	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop

	return nil
}

// ensureDatabase creates a database if it is not there
// already.
//
// CREATE DATABASE cannot be given a parameter, so the name
// is quoted as an identifier instead. The names in this
// file are constants rather than anything a user typed, so
// there is nothing to inject into, but quoting is still the
// correct way to put an identifier into SQL.
func ensureDatabase(
	ctx context.Context,
	url string,
	name string,
) error {

	conn, err := pgx.Connect(ctx, url)

	if err != nil {
		return fmt.Errorf(
			"could not connect to %s: %w",
			url,
			err,
		)
	}

	defer conn.Close(ctx)

	var exists bool

	err = conn.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_database WHERE datname = $1
		)`,
		name,
	).Scan(&exists)

	if err != nil {
		return fmt.Errorf(
			"could not look for the %s database: %w",
			name,
			err,
		)
	}

	if exists {

		fmt.Println("database already present:", name)

		return nil
	}

	_, err = conn.Exec(
		ctx,
		`CREATE DATABASE `+pgx.Identifier{name}.Sanitize(),
	)

	if err != nil {
		return fmt.Errorf(
			"could not create the %s database: %w",
			name,
			err,
		)
	}

	fmt.Println("created database:", name)

	return nil
}

// createSchemaMigrationsTable records which migrations a
// database has had applied.
//
// Without it the only way to tell whether a schema is up to
// date is to guess, and the guess is wrong in exactly the
// case that matters: a run that was interrupted part way
// through leaves a database that has some of the tables and
// looks, to any single-table check, like one that has all
// of them.
const createSchemaMigrationsTable = `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)
`

// ensureSchema brings the application's database up to date
// with the migrations, and reports how many it applied.
//
// A table called schema_migrations records which of them
// have already run. That makes starting this program twice
// harmless, and starting it after adding a migration apply
// only the new one rather than everything again.
//
// A database that has a schema but no such record cannot
// have been built by this version of the program, and the
// migrations cannot repair it: they are ordinary CREATE
// TABLE statements, so they fail on the tables an earlier
// run did get through. Rebuilding is the reliable way out.
//
// The migrations are read from the embedded copy the
// application itself uses, so this cannot apply a different
// schema from the one the code expects.
func ensureSchema(
	ctx context.Context,
	url string,
) (int, error) {

	conn, err := pgx.Connect(ctx, url)

	if err != nil {
		return 0, fmt.Errorf(
			"could not connect to %s: %w",
			url,
			err,
		)
	}

	defer conn.Close(ctx)

	hasUsers, err := tableExists(ctx, conn, "users")

	if err != nil {
		return 0, err
	}

	hasRecord, err := tableExists(
		ctx,
		conn,
		"schema_migrations",
	)

	if err != nil {
		return 0, err
	}

	if hasUsers && !hasRecord {

		fmt.Println(
			"this schema was left by an interrupted run and will be rebuilt",
		)

		err = rebuildSchema(ctx, conn)

		if err != nil {
			return 0, err
		}
	}

	_, err = conn.Exec(ctx, createSchemaMigrationsTable)

	if err != nil {

		return 0, fmt.Errorf(
			"could not create the migration record: %w",
			err,
		)
	}

	applied, err := applyMissingMigrations(ctx, conn)

	if err != nil {
		return applied, err
	}

	if applied == 0 {
		fmt.Println("the schema is already up to date")
	}

	return applied, nil
}

// tableExists asks PostgreSQL whether a table is there.
//
// to_regclass returns NULL rather than failing when it is
// not, which is exactly the question being asked.
func tableExists(
	ctx context.Context,
	conn *pgx.Conn,
	name string,
) (bool, error) {

	var present *string

	err := conn.QueryRow(
		ctx,
		`SELECT to_regclass($1)::text`,
		"public."+name,
	).Scan(&present)

	if err != nil {

		return false, fmt.Errorf(
			"could not look for the %s table: %w",
			name,
			err,
		)
	}

	return present != nil, nil
}

// rebuildSchema throws the schema away and starts again.
//
// Dropping and recreating the schema is faster and far more
// reliable than dropping tables one at a time in dependency
// order, which would have to be kept in step with the
// migrations.
func rebuildSchema(
	ctx context.Context,
	conn *pgx.Conn,
) error {

	statements := []string{
		`DROP SCHEMA IF EXISTS public CASCADE`,
		`CREATE SCHEMA public`,
	}

	for _, statement := range statements {

		_, err := conn.Exec(ctx, statement)

		if err != nil {

			return fmt.Errorf(
				"could not rebuild the schema with %q: %w",
				statement,
				err,
			)
		}
	}

	return nil
}

// applyMissingMigrations runs the migrations this database
// has no record of, in filename order, and records each one
// as it goes.
func applyMissingMigrations(
	ctx context.Context,
	conn *pgx.Conn,
) (int, error) {

	names, err := migrationNames()

	if err != nil {
		return 0, err
	}

	alreadyApplied, err := recordedMigrations(ctx, conn)

	if err != nil {
		return 0, err
	}

	applied := 0

	for _, name := range names {

		if alreadyApplied[name] {
			continue
		}

		sql, err := database.Migrations.ReadFile(
			"migrations/" + name,
		)

		if err != nil {

			return applied, fmt.Errorf(
				"could not read migration %s: %w",
				name,
				err,
			)
		}

		_, err = conn.Exec(ctx, string(sql))

		if err != nil {

			return applied, fmt.Errorf(
				"migration %s failed: %w",
				name,
				err,
			)
		}

		_, err = conn.Exec(
			ctx,
			`INSERT INTO schema_migrations (name)
			 VALUES ($1)`,
			name,
		)

		if err != nil {

			return applied, fmt.Errorf(
				"could not record migration %s: %w",
				name,
				err,
			)
		}

		fmt.Println("applied", name)

		applied++
	}

	return applied, nil
}

// recordedMigrations reads the names this database has
// already had applied.
func recordedMigrations(
	ctx context.Context,
	conn *pgx.Conn,
) (map[string]bool, error) {

	rows, err := conn.Query(
		ctx,
		`SELECT name FROM schema_migrations`,
	)

	if err != nil {

		return nil, fmt.Errorf(
			"could not read the migration record: %w",
			err,
		)
	}

	defer rows.Close()

	recorded := make(map[string]bool)

	for rows.Next() {

		var name string

		err = rows.Scan(&name)

		if err != nil {
			return nil, err
		}

		recorded[name] = true
	}

	return recorded, rows.Err()
}

// migrationNames lists the migration files in the order
// they must be applied.
//
// The numbers in the filenames are what makes sorting them
// as text the correct order, which is why they are there.
func migrationNames() ([]string, error) {

	entries, err := fs.ReadDir(
		database.Migrations,
		"migrations",
	)

	if err != nil {
		return nil, fmt.Errorf(
			"could not read the embedded migrations: %w",
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
		return nil, fmt.Errorf("no migrations were found")
	}

	sort.Strings(names)

	return names, nil
}

// writeEnvFile records the connection strings somewhere the
// shell can read them.
//
// They are written to a file because they are long: pasting
// a URL like this into a terminal that wraps at forty
// columns breaks it in half. Sourcing a file has no such
// problem.
func writeEnvFile(port int) error {

	path, err := envFilePath()

	if err != nil {
		return err
	}

	lines := []string{
		"# Written by database/devdb. Do not edit by hand;",
		"# it is rewritten every time that program starts.",
		"#",
		"# Load it with:",
		"#",
		"#     . " + path,
		"",

		// Named rather than left to chance, because the
		// module's dependencies need a newer toolchain
		// than the one installed system-wide.
		"export GOTOOLCHAIN=go1.26.0",
		"",

		"export DATABASE_URL='" +
			connectionURL(port, mainDatabase) + "'",
		"",

		"export TEST_DATABASE_URL='" +
			connectionURL(port, testDatabase) + "'",
		"",
	}

	contents := strings.Join(lines, "\n")

	// The file holds a password, even if it is a throwaway
	// one for a server on localhost, so it is written
	// readable only by its owner.
	err = os.WriteFile(path, []byte(contents), 0o600)

	if err != nil {
		return fmt.Errorf(
			"could not write %s: %w",
			path,
			err,
		)
	}

	fmt.Println("wrote", path)

	return nil
}

// connectionURL builds a connection string for one of the
// databases on the server this program started.
func connectionURL(port int, name string) string {

	return fmt.Sprintf(
		"postgres://%s:%s@localhost:%d/%s?sslmode=disable",
		defaultUser,
		defaultPassword,
		port,
		name,
	)
}

// baseDirectory is where the server's files live.
func baseDirectory() (string, error) {

	configured := strings.TrimSpace(
		os.Getenv("ECB_PGDIR"),
	)

	if configured != "" {
		return configured, nil
	}

	home, err := os.UserHomeDir()

	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".ecb-postgres"), nil
}

// envFilePath is the file the shell is asked to source.
func envFilePath() (string, error) {

	home, err := os.UserHomeDir()

	if err != nil {
		return "", err
	}

	return filepath.Join(home, "ecb-env.sh"), nil
}

// portFromEnv reads the port to listen on.
//
// A port that is already in use is the most likely reason
// for this program to fail, so being able to move it is
// worth the few lines.
func portFromEnv() int {

	configured := strings.TrimSpace(
		os.Getenv("ECB_PGPORT"),
	)

	if configured == "" {
		return defaultPort
	}

	port, err := strconv.Atoi(configured)

	if err != nil || port <= 0 || port > 65535 {

		fmt.Println(
			"ECB_PGPORT is not a usable port, using",
			defaultPort,
		)

		return defaultPort
	}

	return port
}

// printSummary tells the user what to do next.
func printSummary(port int, applied int) {

	fmt.Println()

	if applied > 0 {
		fmt.Println("applied", applied, "migrations")
	}

	fmt.Println("PostgreSQL is listening on port", port)

	fmt.Println("it will keep running until you press Ctrl-C")

	fmt.Println()

	fmt.Println("in another terminal, run:")

	fmt.Println()

	fmt.Println("    . ~/ecb-env.sh")

	fmt.Println("    go test ./...")

	fmt.Println()

	fmt.Println("then, to start the API itself:")

	fmt.Println()

	fmt.Println("    go run .")

	fmt.Println()
}
