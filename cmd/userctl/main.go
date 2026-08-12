// Command userctl manages auth users directly against the database, without a
// running server or an admin session. It is the offline counterpart to the
// /admin/user-management page and the /auth/users API: create a user, reset a
// password, grant or remove admin, list users, delete a user.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"grc/internal/authn"
	"grc/internal/db"
)

const usage = `usage: userctl [-db SPEC] <command> [flags]

Commands:
  list                                  list auth users
  create -u USER [-admin] [-pages P]    create a user (password on stdin)
  set-password -u USER [-revoke]        reset a password (password on stdin)
  set-admin -u USER [-remove]           grant (or with -remove, take away) admin
  delete -u USER                        delete a user

The password is always read from stdin, never from a flag, so it does not
appear in the process list or shell history.

-db SPEC selects the backend: a postgres:// URL or a SQLite file path.
Defaults to $DATABASE_URL, then $SQLITE_PATH, then users.db.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "userctl: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	root := flag.NewFlagSet("userctl", flag.ContinueOnError)
	root.SetOutput(os.Stderr)
	root.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	spec := root.String("db", "", "database spec (postgres:// URL or SQLite path)")
	if err := root.Parse(args); err != nil {
		return err
	}
	rest := root.Args()
	if len(rest) == 0 {
		root.Usage()
		return errors.New("no command given")
	}

	resolved := resolveSpec(*spec)
	// Always report the resolved backend: every "user not found" confusion so
	// far has been this command opening a different database than expected
	// (sudo dropping DATABASE_URL/SQLITE_PATH from the environment, a
	// different checkout, etc.). stderr so it never pollutes parsed output.
	fmt.Fprintf(os.Stderr, "userctl: database: %s\n", describeSpec(resolved))

	conn, err := db.Open(resolved)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer conn.Close()
	service := authn.NewService(conn)

	command, commandArgs := rest[0], rest[1:]
	switch command {
	case "list":
		return listUsers(service)
	case "create":
		return createUser(service, commandArgs)
	case "set-password", "passwd":
		return setPassword(service, conn, commandArgs)
	case "set-admin":
		return setAdmin(service, commandArgs)
	case "delete":
		return deleteUser(service, commandArgs)
	default:
		root.Usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func resolveSpec(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	if path := os.Getenv("SQLITE_PATH"); path != "" {
		return path
	}
	return "users.db"
}

// describeSpec renders a database spec for diagnostics. A PostgreSQL password
// is redacted so the line is safe to paste into a bug report; a SQLite path is
// made absolute and flagged when the file does not exist yet, which is the
// usual cause of an unexpectedly empty database (db.Open creates it).
func describeSpec(spec string) string {
	if strings.HasPrefix(spec, "postgres://") || strings.HasPrefix(spec, "postgresql://") {
		parsed, err := url.Parse(spec)
		if err != nil {
			return "(unparseable postgres url)"
		}
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword {
				parsed.User = url.UserPassword(parsed.User.Username(), "****")
			}
		}
		return parsed.String()
	}
	path, err := filepath.Abs(spec)
	if err != nil {
		path = spec
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path + "  (did not exist — created empty)"
	}
	return path
}

func listUsers(service *authn.Service) error {
	users, err := service.ListUsers()
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	if len(users) == 0 {
		fmt.Println("(no auth users)")
		return nil
	}
	fmt.Printf("%-4s %-24s %-6s %s\n", "ID", "USERNAME", "ADMIN", "ALLOWED PAGES")
	for _, user := range users {
		pages := "(all)"
		if len(user.AllowedPages) > 0 {
			pages = strings.Join(user.AllowedPages, ",")
		}
		fmt.Printf("%-4d %-24s %-6t %s\n", user.ID, user.Username, user.IsAdmin, pages)
	}
	return nil
}

func createUser(service *authn.Service, args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	username := fs.String("u", "", "username")
	isAdmin := fs.Bool("admin", false, "grant admin role")
	pages := fs.String("pages", "", "comma-separated allowed page paths (empty means every page)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("create: -u USER is required")
	}
	exists, err := userExists(service, *username)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("user %q already exists; use set-password to change its password",
			strings.ToLower(strings.TrimSpace(*username)))
	}
	password, err := readPassword()
	if err != nil {
		return err
	}
	user, err := service.CreateUserWithAccess(*username, password, *isAdmin, splitPages(*pages))
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	fmt.Printf("created user %q (id %d, admin=%t)\n", user.Username, user.ID, user.IsAdmin)
	return nil
}

func setPassword(service *authn.Service, conn *db.Conn, args []string) error {
	fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
	username := fs.String("u", "", "username")
	revoke := fs.Bool("revoke", false, "also delete that user's existing sessions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("set-password: -u USER is required")
	}
	password, err := readPassword()
	if err != nil {
		return err
	}
	user, err := service.UpdateUser(*username, &password, nil, nil)
	if err != nil {
		if errors.Is(err, authn.ErrNotFound) {
			return fmt.Errorf("update password: no auth user %q in this database — run 'list' to see which accounts exist, and check the database line above is the one you meant", strings.ToLower(strings.TrimSpace(*username)))
		}
		return fmt.Errorf("update password: %w", err)
	}
	fmt.Printf("password updated for %q\n", user.Username)
	if *revoke {
		if _, err := conn.Exec(`DELETE FROM auth_sessions WHERE user_id = ?`, user.ID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		fmt.Printf("existing sessions for %q revoked\n", user.Username)
	}
	return nil
}

// setAdmin flips an existing account's admin flag. It exists because single-user
// mode makes admin otherwise unrecoverable: `create` defaults to a non-admin
// account, the single-user guard then refuses a second account, and every other
// route to admin (the user-management page, the /auth/users API) is itself
// behind an admin session. Without this the only way out is hand-editing
// is_admin in SQLite.
//
// No password is read, so this does not disturb the account's credentials, and
// no sessions are revoked: the session lookup joins auth_users on every
// request, so a change here takes effect on the user's next page load rather
// than requiring them to sign in again.
func setAdmin(service *authn.Service, args []string) error {
	fs := flag.NewFlagSet("set-admin", flag.ContinueOnError)
	username := fs.String("u", "", "username")
	remove := fs.Bool("remove", false, "remove admin instead of granting it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("set-admin: -u USER is required")
	}
	grant := !*remove
	user, err := service.UpdateUser(*username, nil, &grant, nil)
	if err != nil {
		switch {
		case errors.Is(err, authn.ErrNotFound):
			return fmt.Errorf("set-admin: no auth user %q in this database — run 'list' to see which accounts exist, and check the database line above is the one you meant", strings.ToLower(strings.TrimSpace(*username)))
		case errors.Is(err, authn.ErrLastAdmin):
			return fmt.Errorf("set-admin: %q is the only admin left; grant admin to another account first, or this deployment would be left with nobody able to administer it: %w", strings.ToLower(strings.TrimSpace(*username)), err)
		default:
			return fmt.Errorf("set-admin: %w", err)
		}
	}
	if grant {
		fmt.Printf("granted admin to %q (id %d)\n", user.Username, user.ID)
	} else {
		fmt.Printf("removed admin from %q (id %d)\n", user.Username, user.ID)
	}
	return nil
}

func deleteUser(service *authn.Service, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	username := fs.String("u", "", "username")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("delete: -u USER is required")
	}
	if err := service.DeleteUser(*username); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	fmt.Printf("deleted user %q\n", strings.ToLower(strings.TrimSpace(*username)))
	return nil
}

func userExists(service *authn.Service, username string) (bool, error) {
	users, err := service.ListUsers()
	if err != nil {
		return false, fmt.Errorf("list users: %w", err)
	}
	username = strings.ToLower(strings.TrimSpace(username))
	for _, user := range users {
		if user.Username == username {
			return true, nil
		}
	}
	return false, nil
}

func splitPages(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

// readPassword consumes the whole of stdin as the password, so callers pipe it
// in rather than passing it as an argument.
func readPassword() (string, error) {
	data, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", errors.New("no password on stdin")
	}
	return password, nil
}
