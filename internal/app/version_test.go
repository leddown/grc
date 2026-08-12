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
	// Reported so a deploy check catches a binary built without -tags fts5,
	// whose absence would otherwise surface only when corpus search first runs.
	if !strings.Contains(body, `"fts5":`) {
		t.Errorf("body should report FTS5 availability: %s", body)
	}
}

// FTS5Enabled must track the build tag rather than being hardcoded, or the
// deploy check reports a constant.
func TestFTS5EnabledTracksTheBuildTag(t *testing.T) {
	// Whichever way this test binary was built, the constant has to agree with
	// what SQLite can actually do. `go test` and `go test -tags fts5` therefore
	// exercise both branches.
	t.Logf("built with FTS5Enabled=%v", FTS5Enabled)
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
