package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTronAndTrenchCarryTheirPalettes(t *testing.T) {
	for theme, want := range map[string][]string{
		themeTron:   {"--accent: #18e6ff", "--error: #ff7a1a", "--radius: 2px", "global-light-cycles"},
		themeTrench: {"--accent: #c9a862", "--button-fill: #a51d22", "--radius: 1px", "global-no-mans-land"},
	} {
		page := string(injectGlobalUI([]byte("<html><head></head><body>x</body></html>"), theme))
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Errorf("%s: missing %q", theme, w)
			}
		}
	}
	// The filled button reads the theme's own fill where it has one, and the
	// accent everywhere else.
	if !strings.Contains(themeStyleTag(themeDark), "var(--button-fill, var(--accent))") {
		t.Error("the primary button must fall back to the accent")
	}
}

// Each engine is a canvas repainting behind the page, so it goes to its own
// theme and no other.
func TestThemeEnginesInjectedOnlyForTheirTheme(t *testing.T) {
	page := []byte("<html><head></head><body>x</body></html>")
	engines := map[string]string{
		"global-matrix-rain-engine":  themeMatrix + "," + themeChaos,
		"global-chaos-engine":        themeChaos,
		"global-fritz-engine":        themeK40,
		"global-light-cycles-engine": themeTron,
		"global-no-mans-land-engine": themeTrench,
	}
	for _, theme := range allThemes() {
		got := string(injectGlobalUI(page, theme))
		for engine, owners := range engines {
			want := strings.Contains(","+owners+",", ","+theme+",")
			if strings.Contains(got, engine) != want {
				t.Errorf("%s: %s present=%v, want %v", theme, engine, !want, want)
			}
		}
	}
}

// Less motion asked for is a still frame, not a missing backdrop, and a
// background tab is not animated.
func TestNewEnginesHonourReducedMotionAndHiddenTabs(t *testing.T) {
	for name, src := range map[string]string{"light cycles": lightCyclesTag, "no man's land": noMansLandTag} {
		for _, want := range []string{"prefers-reduced-motion", "paintStill", "visibilitychange", "canvas.clientWidth"} {
			if !strings.Contains(src, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
	}
	if !strings.Contains(lightCyclesTag, "window.GRCCycles") || !strings.Contains(noMansLandTag, "window.GRCFront") {
		t.Error("the engines' settings are reachable from the console")
	}
}

func themeAssetRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/theme-assets/:file", themeAsset)
	return r
}

// Every asset a theme's CSS asks for is on the allowlist, embedded, and served
// with its own type; anything else is a 404.
func TestThemeAssetsServeWhatTheCSSReferences(t *testing.T) {
	r := themeAssetRouter()
	refs := regexp.MustCompile(`/theme-assets/([A-Za-z0-9._-]+)`).FindAllStringSubmatch(themeStyleTag(themeTrench)+themeStyleTag(themeTron), -1)
	if len(refs) < 6 {
		t.Fatalf("expected the Trench theme to reference its fonts and ornaments, found %d", len(refs))
	}
	for _, m := range refs {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[0], nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("%s: %d", m[0], rec.Code)
			continue
		}
		if got, want := rec.Header().Get("Content-Type"), themeAssetTypes[m[1]]; got != want {
			t.Errorf("%s: Content-Type %q, want %q", m[0], got, want)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff", m[0])
		}
	}
	for _, path := range []string{"/theme-assets/missing.woff2", "/theme-assets/..%2Ftheme_assets.go", "/theme-assets/themeassets"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rec.Code)
		}
	}
}

// The faces are SIL OFL, which requires the licence to travel with them.
func TestThemeFontsShipWithTheirLicences(t *testing.T) {
	for name := range themeAssetTypes {
		if _, err := themeAssetsFS.ReadFile("themeassets/" + name); err != nil {
			t.Errorf("%s is on the allowlist but not embedded: %v", name, err)
		}
	}
	for _, name := range []string{"OFL-EBGaramond.txt", "OFL-GrenzeGotisch.txt"} {
		data, err := themeAssetsFS.ReadFile("themeassets/" + name)
		if err != nil || !strings.Contains(string(data), "SIL OPEN FONT LICENSE") {
			t.Errorf("%s: missing or not the OFL (%v)", name, err)
		}
	}
	// The ornaments are images and nothing else.
	for _, name := range []string{"grain.svg", "wire.svg", "cross.svg", "idol.svg"} {
		data, _ := themeAssetsFS.ReadFile("themeassets/" + name)
		lower := strings.ToLower(string(data))
		for _, bad := range []string{"<script", "href", "onload", "javascript:"} {
			if strings.Contains(lower, bad) {
				t.Errorf("%s carries %q", name, bad)
			}
		}
	}
}

// The sign-in page loads the Trench faces before anyone has a session, so the
// asset path must stay outside the protected prefixes.
func TestThemeAssetsArePublic(t *testing.T) {
	if isProtectedPath("/theme-assets/GrenzeGotisch.woff2") {
		t.Error("/theme-assets must not require a session")
	}
}

// The Policy Studio's document has its own type and a paper view that stands
// for the deliverable. The heading faces, Tron's glow and hard-coded cyan, and
// Trench's red and Garamond adjustment must not reach it.
func TestCostumesLeaveTheStudioDocumentAlone(t *testing.T) {
	for name, css := range map[string]string{"tron": tronThemeCSS, "trench": trenchThemeCSS} {
		plain := stripCSSComments(css)
		for _, want := range []string{"h1:not(.ps-prosemirror *)", ".global-content h2:not(.ps-prosemirror *)", "th:not(.ps-prosemirror *)"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
		for _, bare := range []string{"\nh1 {", "\nh1, ", "thead th", ".global-content h2 {", ".global-content h2,"} {
			if strings.Contains(plain, bare) {
				t.Errorf("%s: an unscoped head or table rule %q reaches the Studio document", name, bare)
			}
		}
	}
	if !strings.Contains(trenchThemeCSS, ".ps-prosemirror { font-size-adjust: none !important; }") {
		t.Error("the Garamond size adjustment is inherited and must stop at the Studio document")
	}
}
