package app

import (
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"
)

// BuildVersion identifies the source revision this binary was built from. It is
// stamped at build time by scripts/setup.sh and update.sh:
//
//	go build -ldflags "-X grc/internal/app.BuildVersion=$(git rev-parse --short HEAD)"
//
// and stays "dev" for an ordinary `go build ./cmd/api`.
//
// This exists because there is no other reliable way to tell, from outside,
// whether a deployed binary matches the source. Probing routes does not work:
// pageAccessMiddleware guards whole path prefixes and aborts before routing, so
// a route that was never registered and a route that exists but requires a
// session both answer 401. A deploy that silently kept the old binary therefore
// looks completely healthy until somebody clicks a new page.
var BuildVersion = "dev"

// versionHandler reports the build revision. Deliberately unauthenticated, so
// scripts/verify-install.sh can compare a running service against the checkout
// without needing credentials — the same reason /health is open. It exposes a
// short commit hash, the Go version and which optional build tags are in
// effect -- nothing about the data or configuration.
func versionHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": BuildVersion,
		"go":      runtime.Version(),
		// Whether this binary carries -tags fts5. Reported so a deploy check can
		// catch a build that would only fail later, when corpus search runs.
		"fts5": FTS5Enabled,
	})
}
