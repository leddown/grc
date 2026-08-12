package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func themeContextWithCookie(value string) *gin.Context {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if value != "" {
		req.AddCookie(&http.Cookie{Name: themeCookieName, Value: value})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestCurrentThemeRecognizesEveryTheme(t *testing.T) {
	cases := map[string]string{
		themeDark:   themeDark,
		themeLight:  themeLight,
		themeMatrix: themeMatrix,
		themeChaos:  themeChaos,
		"MATRIX":    themeMatrix,
		"  chaos  ": themeChaos,
		"nonsense":  themeDark,
		"":          themeDark,
	}
	for cookie, want := range cases {
		if got := currentTheme(themeContextWithCookie(cookie)); got != want {
			t.Errorf("currentTheme(%q)=%q want=%q", cookie, got, want)
		}
	}
}

func TestThemeStyleSelectsMatchingPalette(t *testing.T) {
	chaos := headInsertTag(themeChaos)
	if !strings.Contains(chaos, ".chaos-char") {
		t.Error("chaos theme must carry the .chaos-char rule")
	}
	// Chaos is Matrix plus that rule, so the shared palette must be present.
	if !strings.Contains(chaos, "#00ff41") {
		t.Error("chaos theme must reuse the matrix palette")
	}

	matrix := headInsertTag(themeMatrix)
	if !strings.Contains(matrix, "#00ff41") {
		t.Error("matrix theme must carry the matrix palette")
	}
	if strings.Contains(matrix, ".chaos-char") {
		t.Error("matrix theme must not carry the chaos-only rule")
	}

	if !strings.Contains(headInsertTag(themeLight), "color-scheme: light") {
		t.Error("light theme must declare a light color-scheme")
	}
}

func TestChaosEngineInjectedOnlyForChaosTheme(t *testing.T) {
	page := []byte("<html><head></head><body>x</body></html>")

	if got := string(injectGlobalUI(page, themeChaos)); !strings.Contains(got, "global-chaos-engine") {
		t.Error("chaos theme must receive the glitch engine")
	}
	for _, theme := range []string{themeDark, themeLight, themeMatrix} {
		if got := string(injectGlobalUI(page, theme)); strings.Contains(got, "global-chaos-engine") {
			t.Errorf("%s theme must not receive the glitch engine", theme)
		}
	}
}

// The single-pass splice has to behave exactly like the six-pass chain it
// replaced: same insertion points, same ordering, and the same tolerance for
// documents that are missing one or both tags.
func TestInjectGlobalUIPlacesTagsCorrectly(t *testing.T) {
	page := []byte("<html><HEAD></HEAD><BODY>hello</BODY></html>")
	got := string(injectGlobalUI(page, themeDark))

	// Tag matching is case-insensitive: several pages in this app are hand
	// written and the casing is not guaranteed.
	headClose := strings.Index(strings.ToLower(got), "</head>")
	bodyClose := strings.LastIndex(strings.ToLower(got), "</body>")

	themeAt := strings.Index(got, `id="global-theme-style"`)
	mobileAt := strings.Index(got, `id="global-mobile-style"`)
	navAt := strings.Index(got, `id="global-side-nav-style"`)
	dockAt := strings.Index(got, `id="global-ai-dock"`)

	for name, idx := range map[string]int{
		"theme style": themeAt, "mobile style": mobileAt,
		"side nav": navAt, "ai dock": dockAt,
	} {
		if idx < 0 {
			t.Fatalf("%s missing from output", name)
		}
	}
	if !(themeAt < mobileAt && mobileAt < headClose) {
		t.Error("palette then responsive layer, both inside <head>")
	}
	if navAt < headClose || navAt > bodyClose || dockAt > bodyClose {
		t.Error("body tags must land inside <body>")
	}
	if !strings.Contains(got, "hello") {
		t.Error("original body content must survive")
	}

	// A fragment with neither tag passes through untouched.
	fragment := []byte("<div>partial</div>")
	if got := string(injectGlobalUI(fragment, themeDark)); got != string(fragment) {
		t.Errorf("a fragment with no head/body must pass through unchanged, got %q", got)
	}

	// Head-only and body-only documents each get just their own insertion.
	headOnly := string(injectGlobalUI([]byte("<html><head></head></html>"), themeDark))
	if !strings.Contains(headOnly, `id="global-theme-style"`) {
		t.Error("a head-only document should still receive the palette")
	}
	if strings.Contains(headOnly, `id="global-ai-dock"`) {
		t.Error("a head-only document must not receive body tags")
	}
}

func TestFoldedSearchMatchesRegardlessOfCase(t *testing.T) {
	hay := []byte("<HTML><Head></HeAd><body>x</BODY></html>")

	if got := indexFoldASCII(hay, []byte("</head>")); got != strings.Index(strings.ToLower(string(hay)), "</head>") {
		t.Errorf("indexFoldASCII = %d, want the lowercased strings.Index result", got)
	}
	if got := lastIndexFoldASCII(hay, []byte("</body>")); got != strings.LastIndex(strings.ToLower(string(hay)), "</body>") {
		t.Errorf("lastIndexFoldASCII = %d, want the lowercased strings.LastIndex result", got)
	}
	if got := indexFoldASCII(hay, []byte("</nope>")); got != -1 {
		t.Errorf("absent needle = %d, want -1", got)
	}
	// lastIndex has to find the *last* match, or a commented-out tag earlier in
	// the document would win.
	twice := []byte("<body>a</body><body>b</body>")
	if got := lastIndexFoldASCII(twice, []byte("</body>")); got != strings.LastIndex(string(twice), "</body>") {
		t.Errorf("lastIndexFoldASCII picked the wrong occurrence: %d", got)
	}
}

// The engine source is embedded in a Go raw string literal, which cannot
// contain a backtick. morpheus's chaos.js uses a template literal for the
// glitch colour, so the port has to concatenate instead; this pins that.
func TestChaosEngineSourceHasNoBacktick(t *testing.T) {
	if strings.Contains(chaosEngineTag, "`") {
		t.Error("chaosEngineTag must not contain a backtick")
	}
	if !strings.Contains(chaosEngineTag, "hsl(") {
		t.Error("chaosEngineTag lost its colour generator")
	}
}

// Same raw-string-literal constraint as the glitch engine: morpheus's
// matrix-rain.js builds the canvas font and its rgba() strings with template
// literals, so the port has to concatenate.
func TestMatrixRainSourceHasNoBacktick(t *testing.T) {
	if strings.Contains(matrixRainTag, "`") {
		t.Error("matrixRainTag must not contain a backtick")
	}
	if !strings.Contains(matrixRainTag, "アイウエオ") {
		t.Error("matrixRainTag lost its katakana glyph set")
	}
}

func TestMatrixRainInjectedOnlyForMatrixAndChaos(t *testing.T) {
	page := []byte("<html><head></head><body>x</body></html>")

	for _, theme := range []string{themeMatrix, themeChaos} {
		got := string(injectGlobalUI(page, theme))
		if !strings.Contains(got, `id="global-matrix-rain"`) {
			t.Errorf("%s theme must receive the rain canvas", theme)
		}
		if !strings.Contains(got, "global-matrix-rain-engine") {
			t.Errorf("%s theme must receive the rain engine", theme)
		}
	}
	for _, theme := range []string{themeDark, themeLight} {
		if got := string(injectGlobalUI(page, theme)); strings.Contains(got, "global-matrix-rain") {
			t.Errorf("%s theme must not receive the rain", theme)
		}
	}
}

// The canvas is a positioned z-index 0 element, so <main> has to be lifted above
// it or every pane's background paints underneath the glyphs. The sidebar shell
// resets <main> with a more specific "all: revert" injected later in the
// cascade, which only !important survives.
func TestMatrixRainLiftsMainAboveTheCanvas(t *testing.T) {
	if !strings.Contains(matrixRainTag, "position: relative !important") ||
		!strings.Contains(matrixRainTag, "z-index: 1 !important") {
		t.Error("matrixRainTag must lift main above the canvas with !important")
	}
	if !strings.Contains(sideNavTag, "all: revert") {
		t.Error("sideNavTag no longer reverts main; the !important above may be stale")
	}
}

// prefers-reduced-motion has to get the texture without the animation: a still
// frame rather than a blank canvas, since the preference is about movement and
// dropping the backdrop entirely would take the theme away with it.
func TestMatrixRainHonoursReducedMotion(t *testing.T) {
	if !strings.Contains(matrixRainTag, "prefers-reduced-motion") {
		t.Error("matrixRainTag must check prefers-reduced-motion")
	}
	if !strings.Contains(matrixRainTag, "paintStill") {
		t.Error("matrixRainTag must paint a still frame when motion is reduced")
	}
}

func TestThemeToggleCycleCoversAllFourThemes(t *testing.T) {
	toggle := themeToggleTag(themeDark)
	for _, theme := range []string{themeDark, themeLight, themeMatrix, themeChaos} {
		if !strings.Contains(toggle, "'"+theme+"'") {
			t.Errorf("toggle cycle is missing %q", theme)
		}
	}

	// The label must name the theme a click switches *to*, so the cycle
	// advances rather than dead-ending on the last entry.
	if !strings.Contains(themeToggleTag(themeMatrix), "Chaos theme") {
		t.Error("matrix should offer chaos next")
	}
	if !strings.Contains(themeToggleTag(themeChaos), "Dark theme") {
		t.Error("chaos should wrap around to dark")
	}
}

func TestInjectGlobalUIWiresChaosEndToEnd(t *testing.T) {
	page := []byte("<html><head></head><body>hello</body></html>")
	got := string(injectGlobalUI(page, themeChaos))

	for _, want := range []string{".chaos-char", "global-chaos-engine", "global-theme-toggle", "global-ai-dock"} {
		if !strings.Contains(got, want) {
			t.Errorf("chaos page missing %q", want)
		}
	}
}

// The responsive layer overrides the palettes' !important declarations (body
// padding-bottom, for one), which only works if it is emitted after them.
func TestMobileStyleFollowsThemeStyleInEveryTheme(t *testing.T) {
	page := []byte("<html><head></head><body>hello</body></html>")

	for _, theme := range []string{themeDark, themeLight, themeMatrix, themeChaos} {
		got := string(injectGlobalUI(page, theme))

		themeIdx := strings.Index(got, `id="global-theme-style"`)
		mobileIdx := strings.Index(got, `id="global-mobile-style"`)
		headIdx := strings.Index(got, "</head>")

		if themeIdx < 0 || mobileIdx < 0 {
			t.Fatalf("%s theme: missing style tag (theme=%d mobile=%d)", theme, themeIdx, mobileIdx)
		}
		if mobileIdx < themeIdx {
			t.Errorf("%s theme: mobile layer must come after the palette", theme)
		}
		if mobileIdx > headIdx {
			t.Errorf("%s theme: mobile layer must stay inside <head>", theme)
		}
	}
}

// These are the iOS-specific fixes the layer exists for; a refactor that drops
// one of them regresses the phone experience without breaking any page.
func TestMobileStyleCarriesIOSFixes(t *testing.T) {
	for _, want := range []string{
		"-webkit-text-size-adjust",    // no landscape text inflation
		"env(safe-area-inset-bottom)", // clear of the home indicator
		"font-size: max(16px, 1em)",   // no zoom-on-focus
		"touch-action: manipulation",  // no 300ms tap delay
		":focus-visible",              // visible keyboard focus
		"prefers-reduced-motion",      // honours the OS motion setting
	} {
		if !strings.Contains(mobileStyleTag, want) {
			t.Errorf("mobile layer lost %q", want)
		}
	}
}
