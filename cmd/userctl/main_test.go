package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"carelockconsulting/internal/authn"
	"carelockconsulting/internal/db"
)

func newTestService(t *testing.T) *authn.Service {
	t.Helper()
	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "userctl.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { sqlite.Close() })
	return authn.NewService(sqlite)
}

func findUser(t *testing.T, service *authn.Service, username string) authn.User {
	t.Helper()
	users, err := service.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	for _, user := range users {
		if user.Username == username {
			return user
		}
	}
	t.Fatalf("user %q not found", username)
	return authn.User{}
}

// The situation set-admin exists for: single-user mode, and the one account
// that exists was created without -admin, so admin is otherwise unreachable —
// a second account is refused and every in-app route to admin needs an admin
// session already.
func TestSetAdminRecoversANonAdminSoleAccount(t *testing.T) {
	service := newTestService(t)
	if _, err := service.CreateUserWithAccess("operator", "recovery-pass", false, nil); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := service.CreateUserWithAccess("second", "another-pass", true, nil); !errors.Is(err, authn.ErrSingleUserMode) {
		t.Fatalf("expected single-user mode to refuse a second account, got %v", err)
	}

	if err := setAdmin(service, []string{"-u", "operator"}); err != nil {
		t.Fatalf("set-admin: %v", err)
	}

	if !findUser(t, service, "operator").IsAdmin {
		t.Fatal("expected operator to be admin after set-admin")
	}
}

// -remove must not be usable to re-create the lockout it exists to undo.
func TestSetAdminRemoveRefusesToStrandTheLastAdmin(t *testing.T) {
	service := newTestService(t)
	if _, err := service.CreateUserWithAccess("operator", "recovery-pass", true, nil); err != nil {
		t.Fatalf("create user: %v", err)
	}

	err := setAdmin(service, []string{"-u", "operator", "-remove"})
	if !errors.Is(err, authn.ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin, got %v", err)
	}

	if !findUser(t, service, "operator").IsAdmin {
		t.Fatal("expected operator to still be admin after a refused -remove")
	}
}

func TestSetAdminRemoveWorksWhenAnotherAdminRemains(t *testing.T) {
	service := newTestService(t)
	service.SetSingleUserMode(false)
	if _, err := service.CreateUserWithAccess("first", "first-pass", true, nil); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := service.CreateUserWithAccess("second", "second-pass", true, nil); err != nil {
		t.Fatalf("create second: %v", err)
	}

	if err := setAdmin(service, []string{"-u", "second", "-remove"}); err != nil {
		t.Fatalf("set-admin -remove: %v", err)
	}

	if findUser(t, service, "second").IsAdmin {
		t.Fatal("expected second to no longer be admin")
	}
}

// The password is left untouched: set-admin reads no stdin, so an operator
// recovering admin does not also have to reset credentials.
func TestSetAdminLeavesThePasswordIntact(t *testing.T) {
	service := newTestService(t)
	if _, err := service.CreateUserWithAccess("operator", "recovery-pass", false, nil); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := setAdmin(service, []string{"-u", "operator"}); err != nil {
		t.Fatalf("set-admin: %v", err)
	}

	if _, err := service.Authenticate("operator", "recovery-pass"); err != nil {
		t.Fatalf("expected the original password to still authenticate: %v", err)
	}
}

func TestSetAdminUnknownUserNamesTheDatabaseConfusion(t *testing.T) {
	service := newTestService(t)
	err := setAdmin(service, []string{"-u", "nobody"})
	if err == nil {
		t.Fatal("expected an error for an unknown user")
	}
	if !strings.Contains(err.Error(), "no auth user") {
		t.Fatalf("expected a not-found message, got %v", err)
	}
}

func TestSetAdminRequiresAUsername(t *testing.T) {
	service := newTestService(t)
	if err := setAdmin(service, nil); err == nil {
		t.Fatal("expected an error when -u is omitted")
	}
}
