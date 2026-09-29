package policystudio

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// assetsFS is the committed editor bundle, built by
// scripts/build-policy-studio.sh from web/policy-studio. It is embedded so the
// binary stays self-contained: Node is needed to change the editor, never to
// build or run the application.
//
//go:embed assets
var assetsFS embed.FS

// Manifest describes the embedded bundle: every file with its hash and size,
// and every npm package bundled into it with its version and licence.
type Manifest struct {
	Entry struct {
		JS  string `json:"js"`
		CSS string `json:"css"`
	} `json:"entry"`
	Files []struct {
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
		Bytes  int    `json:"bytes"`
	} `json:"files"`
	Packages []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		License string `json:"license"`
	} `json:"packages"`
}

var (
	manifestOnce sync.Once
	manifest     Manifest
	manifestErr  error
)

// LoadManifest reads and verifies the embedded bundle: a file whose hash does
// not match the manifest means the bundle was edited by hand or half-built.
func LoadManifest() (Manifest, error) {
	manifestOnce.Do(func() {
		raw, err := assetsFS.ReadFile("assets/manifest.json")
		if err != nil {
			manifestErr = fmt.Errorf("policy studio bundle missing: %w", err)
			return
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			manifestErr = fmt.Errorf("policy studio manifest: %w", err)
			return
		}
		for _, f := range manifest.Files {
			data, err := assetsFS.ReadFile("assets/" + f.File)
			if err != nil {
				manifestErr = fmt.Errorf("policy studio bundle: %s listed but missing", f.File)
				return
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != f.SHA256 || len(data) != f.Bytes {
				manifestErr = fmt.Errorf("policy studio bundle: %s does not match its manifest entry", f.File)
				return
			}
		}
	})
	return manifest, manifestErr
}

// serveAsset serves one bundle file. Only files the manifest lists are served;
// their names carry their content hash, so they are cached forever.
func serveAsset(c *gin.Context) {
	name := c.Param("file")
	m, err := LoadManifest()
	if err != nil {
		c.String(http.StatusServiceUnavailable, "policy studio bundle unavailable")
		return
	}
	listed := false
	for _, f := range m.Files {
		if f.File == name {
			listed = true
			break
		}
	}
	if !listed || strings.Contains(name, "/") {
		c.String(http.StatusNotFound, "not found")
		return
	}
	data, err := assetsFS.ReadFile("assets/" + name)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	contentType := map[string]string{
		".js":  "text/javascript; charset=utf-8",
		".css": "text/css; charset=utf-8",
	}[path.Ext(name)]
	if contentType == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, contentType, data)
}
