// Command promote makes an existing account an administrator.
//
// It exists to fill the one gap in the API's account model.
// Registration can produce a buyer or a seller and nothing
// else, which is deliberate: admin rights may only be
// granted by an existing admin, and a marketplace where
// anybody can sign up as an administrator is not a
// marketplace. The consequence of that rule is that a
// fresh database contains no admin at all, so coupons,
// every /admin route, and refunds are unreachable until
// one exists.
//
// The answer is this program rather than an endpoint. An
// operator runs it against a database they already have
// access to, so the privilege comes from being able to
// reach the deployment rather than from a request anybody
// on the internet can send.
//
// Usage:
//
//	go run ./database/promote you@example.com
//
// It is deliberately narrow: it promotes one named account
// and offers no way to promote every account at once. That
// is what stops a missing or mistyped argument from leaving
// a database in which everybody is an administrator, which
// is precisely the accident a bare UPDATE invites. It is
// the reason this is a program rather than a line of SQL
// in the README, even though the SQL would be shorter.
//
// Running it twice is harmless. An account that is already
// an admin is reported and left alone, and the exit status
// is still zero, so it can sit in a setup script.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// adminRole is spelled here rather than imported from the
// models package. This command is a small standalone tool
// and importing the application's models would drag the
// whole application into its build for the sake of one
// three-letter string, which is also the string the
// database's own CHECK constraint names.
const adminRole = "admin"

func main() {

	err := run()

	if err != nil {

		fmt.Fprintln(os.Stderr, "promote:", err)

		os.Exit(1)
	}
}

func run() error {

	if len(os.Args) != 2 {

		return errors.New(
			`usage: go run ./database/promote you@example.com`,
		)
	}

	email := strings.TrimSpace(os.Args[1])

	if email == "" {

		return errors.New(
			"the email is empty",
		)
	}

	url := strings.TrimSpace(
		os.Getenv("DATABASE_URL"),
	)

	if url == "" {

		return errors.New(
			"DATABASE_URL is not set: run . ~/ecb-env.sh first",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)

	defer cancel()

	// Connect rather than opening a pool, because this is
	// one short-lived command rather than a server. It
	// also means a bad DATABASE_URL fails here, before
	// anything has been attempted, rather than surfacing
	// later as a confusing query error.
	conn, err := pgx.Connect(ctx, url)

	if err != nil {

		return fmt.Errorf(
			"could not connect: %w",
			err,
		)
	}

	defer conn.Close(context.Background())

	var (
		id   int
		role string
	)

	err = conn.QueryRow(
		ctx,
		`SELECT id, role FROM users WHERE email = $1`,
		email,
	).Scan(&id, &role)

	if errors.Is(err, pgx.ErrNoRows) {

		return fmt.Errorf(
			"no account with the email %q%s",
			email,
			knownAccounts(ctx, conn),
		)
	}

	if err != nil {

		return fmt.Errorf(
			"could not look up %q: %w",
			email,
			err,
		)
	}

	if role == adminRole {

		fmt.Printf(
			"%s is already an admin. Nothing changed.\n",
			email,
		)

		return nil
	}

	// The row is addressed by its id, and the id came from
	// a lookup that matched exactly one row, so this can
	// only ever change the account named on the command
	// line.
	tag, err := conn.Exec(
		ctx,
		`UPDATE users SET role = $1 WHERE id = $2`,
		adminRole,
		id,
	)

	if err != nil {

		return fmt.Errorf(
			"could not promote %q: %w",
			email,
			err,
		)
	}

	if tag.RowsAffected() != 1 {

		return fmt.Errorf(
			"expected to change 1 row, changed %d",
			tag.RowsAffected(),
		)
	}

	fmt.Printf(
		"Promoted %s from %s to admin.\n\n%s\n",
		email,
		role,
		nextSteps,
	)

	return nil
}

// nextSteps is printed after a successful promotion,
// because the first admin is usually created by somebody
// who has not used this application before and does not
// yet know which routes the role unlocks.
const nextSteps = `Coupons, /admin/users, /admin/orders, and refunds are now
reachable as this account. Sign in with POST /login to get
a session cookie before calling them.`

// knownAccounts lists the accounts that do exist.
//
// A mistyped address is answered with the list of ones that
// would have worked, which is a far better reply than a
// bare "not found" when the whole point of the command is
// that there is no other way to enumerate them yet.
//
// An empty table produces no list, which is the honest
// answer: there is no account to name, and registering one
// is the step that has to come first.
func knownAccounts(
	ctx context.Context,
	conn *pgx.Conn,
) string {

	rows, err := conn.Query(
		ctx,
		`SELECT email, role FROM users ORDER BY id`,
	)

	if err != nil {

		return ""
	}

	defer rows.Close()

	var lines []string

	for rows.Next() {

		var (
			email string
			role  string
		)

		err := rows.Scan(&email, &role)

		if err != nil {

			return ""
		}

		lines = append(
			lines,
			fmt.Sprintf("  %s (%s)", email, role),
		)
	}

	if len(lines) == 0 {

		return ".\n\nThe users table is empty, so there is " +
			"nothing to promote yet. Register an account " +
			"first, then run this again with its email."
	}

	return ".\n\nAccounts that do exist:\n" +
		strings.Join(lines, "\n")
}
