package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/authn"
	"grc/internal/db"
)

func newAccessControlTestRouter(t *testing.T) (*gin.Engine, *authn.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "access-control.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	authService := authn.NewService(sqlite)

	router := gin.New()
	router.Use(pageAccessMiddleware(authService))
	router.GET("/controls", func(c *gin.Context) { c.String(http.StatusOK, "controls page") })
	router.GET("/unprotected", func(c *gin.Context) { c.String(http.StatusOK, "open page") })
	return router, authService
}

func TestPageAccessMiddlewareRejectsAnonymousRequestsToProtectedPaths(t *testing.T) {
	router, _ := newAccessControlTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/controls", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestPageAccessMiddlewareRejectsInvalidSessionCookie(t *testing.T) {
	router, _ := newAccessControlTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/controls", nil)
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: "not-a-real-session-token"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestPageAccessMiddlewareAllowsUnprotectedPathsWithoutAuth(t *testing.T) {
	router, _ := newAccessControlTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/unprotected", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestPageAccessMiddlewareAllowsAuthenticatedSession(t *testing.T) {
	router, authService := newAccessControlTestRouter(t)

	admin, err := authService.BootstrapAdmin("admin", "very-strong-pass-1")
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	session, err := authService.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls", nil)
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: session.Token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestPageAccessMiddlewareEnforcesAllowedPages(t *testing.T) {
	router, authService := newAccessControlTestRouter(t)
	authService.SetSingleUserMode(false) // exercises multi-user page access

	if _, err := authService.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	user, err := authService.CreateUserWithAccess("analyst", "very-strong-pass-2", false, []string{"/unprotected"})
	if err != nil {
		t.Fatalf("CreateUserWithAccess: %v", err)
	}
	session, err := authService.CreateSession(user.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/controls", nil)
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: session.Token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// A granted page has to be usable. Every page in this app renders a shell and
// then fetches "<page>/data"; the previous exact-match rule let the shell load
// and then returned 403 for the data, so the page showed a load error and the
// grant was effectively worthless.
func TestGrantCoversSupportingEndpointsButNotSiblingPages(t *testing.T) {
	cases := []struct {
		grant string
		path  string
		want  bool
		why   string
	}{
		{"/controls", "/controls", true, "the page itself"},
		{"/controls", "/controls/data", true, "the data endpoint the page cannot work without"},
		{"/controls", "/controls/detail/AC-2", true, "a nested read endpoint"},
		{"/controls", "/controls/manage", false, "the editor is a separately grantable page"},
		{"/policies", "/policies/data", true, "same shape as controls"},
		{"/policies", "/policies/meta", true, "editor vocabularies"},
		{"/policies", "/policies/manage", false, "the policy editor is granted separately"},
		{"/policies", "/policies/coverage", false, "coverage is granted separately"},
		{"/policies/manage", "/policies/manage", true, "an explicit editor grant"},
		{"/controls", "/security-nfrs", false, "an unrelated page"},
		{"/controls", "/controlsomething", false, "prefix must respect the segment boundary"},

		// The explicit glob keeps its broad meaning, and now also covers the
		// base path — previously "/controls/*" granted every sub-path but 403'd
		// on "/controls" itself, which was the same bug mirrored.
		{"/controls/*", "/controls", true, "glob covers the base path it is named after"},
		{"/controls/*", "/controls/data", true, "glob covers sub-paths"},
		{"/controls/*", "/controls/manage", true, "glob deliberately includes sibling pages"},
		{"/controls/*", "/controls/", true, "glob covers the trailing-slash form"},
		{"/controls/*", "/controlsomething", false, "glob still respects the segment boundary"},

		{"", "/controls", false, "an empty grant covers nothing"},
	}

	for _, tc := range cases {
		if got := grantCovers(tc.grant, tc.path); got != tc.want {
			t.Errorf("grantCovers(%q, %q) = %v, want %v — %s", tc.grant, tc.path, got, tc.want, tc.why)
		}
	}
}

// End-to-end through the middleware, since the unit table above cannot catch a
// wiring mistake between the matcher and the request path.
func TestPageAccessMiddlewareLetsAGrantedPageLoadItsData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "grant.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	authService := authn.NewService(sqlite)
	authService.SetSingleUserMode(false)

	router := gin.New()
	router.Use(pageAccessMiddleware(authService))
	for _, p := range []string{"/controls", "/controls/data", "/controls/manage"} {
		router.GET(p, func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	}

	if _, err := authService.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	user, err := authService.CreateUserWithAccess("analyst", "very-strong-pass-2", false, []string{"/controls"})
	if err != nil {
		t.Fatalf("CreateUserWithAccess: %v", err)
	}
	session, err := authService.CreateSession(user.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	check := func(path string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: session.Token})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
	check("/controls", http.StatusOK)
	check("/controls/data", http.StatusOK)
	check("/controls/manage", http.StatusForbidden)
}
