package authn

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"carelockconsulting/internal/db"
)

func TestBootstrapAdminOnce(t *testing.T) {
	svc := newAuthService(t)

	admin, err := svc.BootstrapAdmin("admin", "very-strong-pass-1")
	if err != nil {
		t.Fatalf("BootstrapAdmin first: %v", err)
	}
	if admin.Username != "admin" || !admin.IsAdmin {
		t.Fatalf("unexpected admin: %+v", admin)
	}

	if _, err := svc.BootstrapAdmin("second", "very-strong-pass-2"); err != ErrAlreadyBootstrapped {
		t.Fatalf("BootstrapAdmin second err=%v want=%v", err, ErrAlreadyBootstrapped)
	}
}

func TestAuthenticateAndSessionLifecycle(t *testing.T) {
	svc := newAuthService(t)
	if _, err := svc.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	user, err := svc.Authenticate("admin", "very-strong-pass-1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	session, err := svc.CreateSession(user.ID, 2*time.Minute)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	resolved, err := svc.GetSessionUser(session.Token)
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if resolved.Username != "admin" || !resolved.IsAdmin {
		t.Fatalf("resolved=%+v", resolved)
	}

	if err := svc.DeleteSession(session.Token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := svc.GetSessionUser(session.Token); err == nil {
		t.Fatalf("expected deleted session to be invalid")
	}
}

func TestGetSessionUserPopulatesAllowedPages(t *testing.T) {
	svc := newAuthService(t)
	svc.SetSingleUserMode(false) // exercises multi-user allowed_pages behaviour
	if _, err := svc.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	user, err := svc.CreateUserWithAccess("analyst", "very-strong-pass-2", false, []string{"/reports"})
	if err != nil {
		t.Fatalf("CreateUserWithAccess: %v", err)
	}
	session, err := svc.CreateSession(user.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	resolved, err := svc.GetSessionUser(session.Token)
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if len(resolved.AllowedPages) != 1 || resolved.AllowedPages[0] != "/reports" {
		t.Fatalf("resolved.AllowedPages=%v want=[/reports]", resolved.AllowedPages)
	}
}

func TestAuthenticateUnknownUsernameReturnsInvalidCredentials(t *testing.T) {
	svc := newAuthService(t)
	if _, err := svc.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	if _, err := svc.Authenticate("no-such-user", "whatever-password"); err != ErrInvalidCredentials {
		t.Fatalf("Authenticate unknown user err=%v want=%v", err, ErrInvalidCredentials)
	}
}

func TestSingleUserModeRefusesSecondAccount(t *testing.T) {
	svc := newAuthService(t)
	if _, err := svc.BootstrapAdmin("admin", "pass1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if _, err := svc.CreateUser("second", "pass2", false); !errors.Is(err, ErrSingleUserMode) {
		t.Fatalf("CreateUser err=%v want=%v", err, ErrSingleUserMode)
	}
	// The single account must still be able to change its own password.
	newPassword := "pass3"
	if _, err := svc.UpdateUser("admin", &newPassword, nil, nil); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, err := svc.Authenticate("admin", newPassword); err != nil {
		t.Fatalf("Authenticate with reset password: %v", err)
	}
}

func TestSingleUserModeAllowsDeletingSoleAdmin(t *testing.T) {
	svc := newAuthService(t)
	if _, err := svc.BootstrapAdmin("admin", "pass1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if err := svc.DeleteUser("admin"); err != nil {
		t.Fatalf("DeleteUser sole admin: %v", err)
	}
	// Deleting the sole account must leave the database bootstrappable again.
	if _, err := svc.BootstrapAdmin("admin2", "pass2"); err != nil {
		t.Fatalf("BootstrapAdmin after delete: %v", err)
	}
}

func TestSingleUserModeStillRefusesLastAdminWithOtherAccounts(t *testing.T) {
	svc := newAuthService(t)
	svc.SetSingleUserMode(false)
	if _, err := svc.BootstrapAdmin("admin", "pass1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if _, err := svc.CreateUser("analyst", "pass2", false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Re-enabling single-user mode must not open a path to an unrecoverable
	// state: a non-admin account still exists, so bootstrap could not recover.
	svc.SetSingleUserMode(true)
	if err := svc.DeleteUser("admin"); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("DeleteUser err=%v want=%v", err, ErrLastAdmin)
	}
}

func TestCreateAndUpdateAuthUser(t *testing.T) {
	svc := newAuthService(t)
	svc.SetSingleUserMode(false) // exercises multi-user create/update behaviour
	if _, err := svc.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	user, err := svc.CreateUser("analyst", "very-strong-pass-2", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.IsAdmin {
		t.Fatalf("expected non-admin user")
	}

	newPass := "very-strong-pass-3"
	makeAdmin := true
	updated, err := svc.UpdateUser("analyst", &newPass, &makeAdmin, nil)
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if !updated.IsAdmin {
		t.Fatalf("expected upgraded admin user")
	}

	if _, err := svc.Authenticate("analyst", "very-strong-pass-2"); err == nil {
		t.Fatalf("old password should no longer work")
	}
	if _, err := svc.Authenticate("analyst", "very-strong-pass-3"); err != nil {
		t.Fatalf("new password failed: %v", err)
	}
}

func TestCannotDemoteLastAdmin(t *testing.T) {
	svc := newAuthService(t)
	if _, err := svc.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	makeNonAdmin := false
	_, err := svc.UpdateUser("admin", nil, &makeNonAdmin, nil)
	if err != ErrLastAdmin {
		t.Fatalf("UpdateUser err=%v want=%v", err, ErrLastAdmin)
	}
}

func TestCannotDeleteLastAdmin(t *testing.T) {
	svc := newAuthService(t)
	svc.SetSingleUserMode(false) // guard applies to multi-user deployments
	if _, err := svc.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	err := svc.DeleteUser("admin")
	if err != ErrLastAdmin {
		t.Fatalf("DeleteUser err=%v want=%v", err, ErrLastAdmin)
	}
}

func TestCanDeleteAdminWhenAnotherAdminExists(t *testing.T) {
	svc := newAuthService(t)
	svc.SetSingleUserMode(false) // exercises multi-admin delete behaviour
	if _, err := svc.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if _, err := svc.CreateUser("admin2", "very-strong-pass-2", true); err != nil {
		t.Fatalf("CreateUser admin2: %v", err)
	}

	if err := svc.DeleteUser("admin"); err != nil {
		t.Fatalf("DeleteUser admin: %v", err)
	}
}

func newAuthService(t *testing.T) *Service {
	t.Helper()
	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "authn.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	return NewService(sqlite)
}
