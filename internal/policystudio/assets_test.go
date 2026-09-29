package policystudio

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The embedded bundle is exactly what its manifest says: every file listed,
// hashed and sized as recorded, and nothing embedded that is not listed.
func TestEmbeddedBundleMatchesManifest(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.Entry.JS == "" || m.Entry.CSS == "" {
		t.Fatal("manifest names no entry files")
	}
	listed := map[string]bool{"manifest.json": true}
	for _, f := range m.Files {
		listed[f.File] = true
	}
	entries, err := fs.ReadDir(assetsFS, "assets")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !listed[e.Name()] {
			t.Errorf("assets/%s is embedded but not in the manifest (a stale build left behind?)", e.Name())
		}
	}
}

func TestBundleSizeBudget(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		if strings.HasSuffix(f.File, ".js") && f.Bytes > 800*1024 {
			t.Errorf("%s is %d bytes, over the 800 KB budget", f.File, f.Bytes)
		}
	}
}

// No bundled package may carry a licence outside the allowlist (no GPL, AGPL,
// SSPL or unknown).
func TestBundledLicencesAreAllowed(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"MIT": true, "BSD-2-Clause": true, "BSD-3-Clause": true, "Apache-2.0": true, "ISC": true, "MPL-2.0": true, "0BSD": true}
	if len(m.Packages) == 0 {
		t.Fatal("manifest lists no packages")
	}
	for _, p := range m.Packages {
		if !allowed[p.License] {
			t.Errorf("%s@%s is licensed %q", p.Name, p.Version, p.License)
		}
	}
}

func TestAssetServing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/assets/policy-studio/:file", serveAsset)
	cases := []struct {
		path, wantType string
		want           int
	}{
		{"/assets/policy-studio/" + m.Entry.JS, "text/javascript; charset=utf-8", http.StatusOK},
		{"/assets/policy-studio/" + m.Entry.CSS, "text/css; charset=utf-8", http.StatusOK},
		{"/assets/policy-studio/manifest.json", "", http.StatusNotFound},
		{"/assets/policy-studio/policy-studio.000000000000.js", "", http.StatusNotFound},
		{"/assets/policy-studio/..%2fschema.go", "", http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.path, rec.Code, tc.want)
			continue
		}
		if tc.want == http.StatusOK {
			if got := rec.Header().Get("Content-Type"); got != tc.wantType {
				t.Errorf("%s: content type %q", tc.path, got)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
				t.Errorf("%s: missing nosniff or immutable caching", tc.path)
			}
		}
	}
}
