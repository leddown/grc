package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// /version must be reachable without a session. scripts/verify-install.sh uses
// it to tell a current deploy from a stale one, and it has no credentials — so
// putting this behind auth would silently disable deploy verification.
func TestVersionEndpointIsPublicAndReportsTheBuild(t *testing.T) {
	gin.SetMode(gin.TestMode)

	original := BuildVersion
	BuildVersion = "abc1234"
	t.Cleanup(func() { BuildVersion = original })

	router := gin.New()
	router.GET("/version", versionHandler)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"version":"abc1234"`) {
		t.Errorf("body did not report the stamped revision: %s", body)
	}
	if !strings.Contains(body, `"go":"go`) {
		t.Errorf("body should report the Go version: %s", body)
	}
	// Reported so a deploy check can tell a binary that has full-text search
	// from one that does not, whose absence would otherwise surface only when
	// corpus search first runs.
	if !strings.Contains(body, `"fts5":`) {
		t.Errorf("body should report FTS5 availability: %s", body)
	}
}

// FTS5Enabled has to agree with what the driver can actually do, or /version
// tells a deploy check something untrue.
//
// It is a plain constant now rather than a build-tag one: modernc.org/sqlite
// compiles FTS5 in unconditionally, so there is no build that can turn it off.
// What the constant claims is checked against a real database by
// TestFTS5IsCompiledIn in internal/db — the assertion belongs where the driver
// is opened, not here.
func TestFTS5EnabledIsTrueWithThePureGoDriver(t *testing.T) {
	if !FTS5Enabled {
		t.Error("FTS5Enabled is false, but the driver always compiles FTS5 in")
	}
}

// The path must not fall under a guarded prefix, or verify-install.sh gets a
// 401 instead of the revision and cannot distinguish stale from current.
func TestVersionPathIsNotProtected(t *testing.T) {
	if isProtectedPath("/version") {
		t.Error("/version must stay outside protectedPrefixes so deploy verification works")
	}
	// /health is the readiness probe setup.sh waits on; same requirement.
	if isProtectedPath("/health") {
		t.Error("/health must stay unprotected")
	}
}
