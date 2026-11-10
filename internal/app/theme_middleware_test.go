package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
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
		themeDark: themeDark,
		// A browser still carrying the retired light cookie falls through to
		// dark rather than to a palette that no longer exists.
		"light":     themeDark,
		themeMatrix: themeMatrix,
		themeChaos:  themeChaos,
		"MATRIX":    themeMatrix,
		"  chaos  ": themeChaos,
		"nonsense":  themeDark,
		"":          themeDark,
		themeK40:    themeK40,
		themeTron:   themeTron,
		themeTrench: themeTrench,
		" Trench ":  themeTrench,
	}
	for cookie, want := range cases {
		if got := currentTheme(themeContextWithCookie(cookie)); got != want {
			t.Errorf("currentTheme(%q)=%q want=%q", cookie, got, want)
		}
	}
}

func TestThemeStyleSelectsMatchingPalette(t *testing.T) {
	// Chaos is Matrix plus the glitch rule, so the shared palette must be there.
	chaos := headInsertTag(themeChaos)
	if !strings.Contains(chaos, "#00ff41") {
		t.Error("chaos theme must reuse the matrix palette")
	}

	matrix := headInsertTag(themeMatrix)
	if !strings.Contains(matrix, "#00ff41") {
		t.Error("matrix theme must carry the matrix palette")
	}

	// The 40K palette is the one that is not a variation on the others.
	if !strings.Contains(headInsertTag(themeK40), "#d8a730") {
		t.Error("40K theme must carry its brass accent")
	}
}

// Every theme has to define the full token set. A palette that omits one leaves
// the pages that read it painting with an invalid colour, which on these
// backgrounds means unreadable rather than merely wrong.
func TestEveryThemeDefinesTheWholePalette(t *testing.T) {
	tokens := []string{
		"--bg:", "--surface:", "--surface-2:", "--border:",
		"--text-base:", "--muted-base:", "--accent:", "--on-accent:",
		"--error:", "--gain:", "--loss:",
	}
	for _, theme := range allThemes() {
		css := headInsertTag(theme)
		for _, token := range tokens {
			if !strings.Contains(css, token) {
				t.Errorf("theme %q does not define %s", theme, token)
			}
		}
		// The pages were written against the older names; the aliases are what
		// keeps them themed.
		for _, alias := range []string{"--ink:", "--line:", "--panel:", "--muted:"} {
			if !strings.Contains(css, alias) {
				t.Errorf("theme %q does not alias %s for the page CSS", theme, alias)
			}
		}
	}
}

// The brightness lift has to be applied before first paint, or it is a visible
// flicker on every page load.
func TestTextLiftAppliedInHead(t *testing.T) {
	css := headInsertTag(themeDark)
	if !strings.Contains(css, "global-text-lift-init") {
		t.Error("the head insert must carry the brightness init")
	}
	if !strings.Contains(css, "color-mix(in srgb") {
		t.Error("the text tokens must be derived, or one control cannot lift every theme")
	}
	// A dropped custom property does not fall back to the palette, so the
	// plain assignment has to come first for browsers without color-mix.
	plain := strings.Index(css, "--text: var(--text-base)")
	mixed := strings.Index(css, "color-mix(in srgb")
	if plain < 0 || plain > mixed {
		t.Error("the un-mixed fallback must precede the color-mix derivation")
	}
}

// The 40K overlay is injected the way the rain is: only for the theme built
// around it. The others must not pay for a timer that appends elements.
func TestFritzOverlayInjectedOnlyFor40K(t *testing.T) {
	page := []byte("<html><head></head><body>x</body></html>")
	if got := string(injectGlobalUI(page, themeK40)); !strings.Contains(got, "global-fritz") {
		t.Error("40K theme must receive the fritz overlay")
	}
	for _, theme := range []string{themeDark, themeMatrix, themeChaos, themeTron, themeTrench} {
		if got := string(injectGlobalUI(page, theme)); strings.Contains(got, "global-fritz") {
			t.Errorf("theme %q must not receive the fritz overlay", theme)
		}
	}
}

// The AI panel slides in from the right edge. A question is asked while working
// on a page, so the page has to stay beside it rather than under it.
func TestAIDockIsARightHandPanel(t *testing.T) {
	for _, want := range []string{
		"#global-ai-dock-toggle",      // the handle on the right edge
		"transform: translateX(100%)", // parked off-screen
		"#global-ai-dock.open",        // and slid in
		"width: min(460px, 100%)",
		"border-left: 1px solid var(--line)",
	} {
		if !strings.Contains(aiQuickPromptDockTag, want) {
			t.Errorf("the AI panel is missing %q", want)
		}
	}
	if strings.Contains(aiQuickPromptDockTag, "left: 0;\n  right: 0;") {
		t.Error("the AI panel must not be a full-width strip along the bottom any more")
	}
}

func TestChaosEngineInjectedOnlyForChaosTheme(t *testing.T) {
	page := []byte("<html><head></head><body>x</body></html>")

	if got := string(injectGlobalUI(page, themeChaos)); !strings.Contains(got, "global-chaos-engine") {
		t.Error("chaos theme must receive the glitch engine")
	}
	for _, theme := range []string{themeDark, themeMatrix} {
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
	layoutAt := strings.Index(got, `id="global-layout-style"`)
	navAt := strings.Index(got, `id="global-side-nav-style"`)
	dockAt := strings.Index(got, `id="global-ai-dock"`)

	for name, idx := range map[string]int{
		"theme style": themeAt, "layout style": layoutAt,
		"side nav": navAt, "ai dock": dockAt,
	} {
		if idx < 0 {
			t.Fatalf("%s missing from output", name)
		}
	}
	if !(themeAt < layoutAt && layoutAt < headClose) {
		t.Error("palette then layout layer, both inside <head>")
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
// contain a backtick, so the glitch colour is built by concatenation rather
// than with a template literal; this pins that.
func TestChaosEngineSourceHasNoBacktick(t *testing.T) {
	if strings.Contains(chaosEngineTag, "`") {
		t.Error("chaosEngineTag must not contain a backtick")
	}
	if !strings.Contains(chaosEngineTag, "hsl(") {
		t.Error("chaosEngineTag lost its colour generator")
	}
}

// Same raw-string-literal constraint as the glitch engine: the canvas font and
// its rgba() strings are concatenated rather than written as template literals.
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
	for _, theme := range []string{themeDark} {
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

// The picker lists every theme, in order, and the one you are in is the one
// selected: it is the only place a theme is ever named.
func TestThemePickerListsEveryThemeAndSelectsTheCurrent(t *testing.T) {
	want := []string{themeDark, themeMatrix, themeChaos, themeK40, themeTron, themeTrench}
	if got := allThemes(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("themes = %v, want %v", got, want)
	}
	option := regexp.MustCompile(`<option value="([^"]+)"( selected)?>([^<]+)</option>`)
	for _, theme := range want {
		picker := themeToggleTag(theme)
		if !strings.Contains(picker, `<select id="global-theme-toggle" aria-label="Theme"`) {
			t.Fatalf("%s: the picker is not a labelled select", theme)
		}
		var listed, selected []string
		for _, m := range option.FindAllStringSubmatch(picker, -1) {
			listed = append(listed, m[1])
			if m[2] != "" {
				selected = append(selected, m[1])
			}
		}
		if strings.Join(listed, ",") != strings.Join(want, ",") {
			t.Errorf("%s: picker lists %v", theme, listed)
		}
		if len(selected) != 1 || selected[0] != theme {
			t.Errorf("%s: selected %v", theme, selected)
		}
	}
	if !strings.Contains(themeToggleTag(themeTrench), ">✠ Trench Crusade</option>") {
		t.Error("the Trench Crusade theme is named in full")
	}
	// An unknown theme falls back to dark everywhere else; the picker must not
	// show a theme as current that the server is not rendering.
	if got := themeToggleTag("nonsense"); strings.Contains(got, " selected") {
		t.Error("an unknown theme must select nothing rather than a theme it is not")
	}
	if !strings.Contains(themeToggleTag(themeDark), "addEventListener('change'") {
		t.Error("the picker must switch on change")
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

// The layout layer overrides the palettes' !important declarations, which only
// works if it is emitted after them.
func TestLayoutStyleFollowsThemeStyleInEveryTheme(t *testing.T) {
	page := []byte("<html><head></head><body>hello</body></html>")

	for _, theme := range allThemes() {
		got := string(injectGlobalUI(page, theme))

		themeIdx := strings.Index(got, `id="global-theme-style"`)
		layoutIdx := strings.Index(got, `id="global-layout-style"`)
		headIdx := strings.Index(got, "</head>")

		if themeIdx < 0 || layoutIdx < 0 {
			t.Fatalf("%s theme: missing style tag (theme=%d layout=%d)", theme, themeIdx, layoutIdx)
		}
		if layoutIdx < themeIdx {
			t.Errorf("%s theme: layout layer must come after the palette", theme)
		}
		if layoutIdx > headIdx {
			t.Errorf("%s theme: layout layer must stay inside <head>", theme)
		}
	}
}

// The layout layer keeps what a keyboard user and the OS motion setting need.
func TestLayoutStyleKeepsFocusAndMotionRules(t *testing.T) {
	for _, want := range []string{
		":focus-visible",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(layoutStyleTag, want) {
			t.Errorf("layout layer lost %q", want)
		}
	}
}

// The application is laid out for a desktop window only. A rule written for a
// phone costs space on every screen it was not written for: the 16px floor on
// form controls alone made every input larger than the text around it.
func TestInjectedChromeHasNoPhoneLayout(t *testing.T) {
	for _, theme := range allThemes() {
		injected := stripCSSComments(headInsertTag(theme) + bodyInsertTag(theme))
		for _, unwanted := range []string{
			"max(16px",
			"safe-area-inset",
			"touch-action",
			"pointer: coarse",
			"nav-open",
		} {
			if strings.Contains(injected, unwanted) {
				t.Errorf("%s theme: the injected chrome carries a phone rule: %q", theme, unwanted)
			}
		}
	}
}

// The AI panel says which provider and agent will answer.
//
// Without it, "I chose an agent in Settings and it did not reach the Ask AI
// box" is unanswerable from the screen: a grounded answer and an ungrounded one
// look identical, and the three things that can be wrong — the provider is
// Claude, no agent is set, or the conversation was opened before the change —
// are all invisible.
func TestAIDockNamesWhatWillAnswer(t *testing.T) {
	for _, want := range []string{
		"global-ai-dock-who",
		"/ai-chat/wintermute/status", // where the answer comes from
		"Wintermute",
		"no agent",
		"Wintermute not configured",
	} {
		if !strings.Contains(aiQuickPromptDockTag, want) {
			t.Errorf("the AI panel does not report what will answer: missing %q", want)
		}
	}

	// wintermuted pins a session to the agent it was opened with and never
	// re-reads it, so a conversation already in progress has to be dropped when
	// the agent changes or it keeps answering as the old one.
	if !strings.Contains(aiQuickPromptDockTag, "if (answeringAs !== null && answeringAs !== label) sessionID = '';") {
		t.Error("the panel must drop a session opened against a different agent")
	}
}

// The sign-in page is outside the application shell. A visitor who is not
// signed in can use none of the chrome, and the page's own bare
// "button { width: 100% }" rule stretched the fixed AI-dock handle into a
// banner lying across the sign-in form.
func TestLoginPageGetsStylesButNotChrome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(themeMiddleware())
	router.GET("/login", loginPage)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	body := rec.Body.String()
	for _, absent := range []string{
		"global-ai-dock",
		"global-side-nav-style",
		"global-theme-toggle",
		"global-command-palette",
	} {
		if strings.Contains(body, absent) {
			t.Errorf("the sign-in page must not carry %q", absent)
		}
	}
	// The palette and the layout layer are not chrome and stay.
	if !strings.Contains(body, "global-layout-style") {
		t.Error("the sign-in page still needs the layout layer")
	}
	if !strings.Contains(body, "--surface-strong") {
		t.Error("the sign-in page still needs the palette")
	}
	// The page itself must be intact either way.
	if !strings.Contains(body, `id="loginForm"`) {
		t.Error("the sign-in form went missing")
	}
}

func stripCSSComments(s string) string {
	var out strings.Builder
	for {
		open := strings.Index(s, "/*")
		if open < 0 {
			out.WriteString(s)
			return out.String()
		}
		out.WriteString(s[:open])
		shut := strings.Index(s[open:], "*/")
		if shut < 0 {
			return out.String()
		}
		s = s[open+shut+2:]
	}
}

// The injected layer gives every input, select and textarea a padding and a
// border the page did not ask for. Pages size their controls with width:100%
// inside a table cell or a flex row, so without border-box sizing that padding
// is added outside the column and the control draws past it.
func TestInjectedControlsAreSizedWithinTheirColumn(t *testing.T) {
	for _, theme := range allThemes() {
		css := stripCSSComments(themeStyleTag(theme))
		start := strings.Index(css, "input, select, textarea {")
		if start < 0 {
			t.Fatalf("%s: control rule not found", theme)
		}
		end := strings.Index(css[start:], "}")
		if end < 0 {
			t.Fatalf("%s: unterminated control rule", theme)
		}
		rule := css[start : start+end]
		for _, want := range []string{"box-sizing: border-box", "max-width: 100%"} {
			if !strings.Contains(rule, want) {
				t.Errorf("%s: control rule must declare %q so a width:100%% control stays inside its column", theme, want)
			}
		}
	}
}

// Anything injected over a page it does not control has to pin its own box: an
// element selector in the page wins every property the injected rule leaves
// unset, and most pages here style bare `button`.
func TestInjectedFixedControlsPinTheirOwnBox(t *testing.T) {
	cases := map[string]struct {
		source string
		width  string
	}{
		"#global-ai-dock-toggle": {aiQuickPromptDockTag, "width: auto"},
		// The picker has a fixed width so the top bar can leave room for it.
		"#global-theme-toggle": {themeToggleTag(themeDark), "width: 136px !important"},
		"#global-text-lift":    {themeToggleTag(themeDark), "width: auto"},
	}
	for selector, tc := range cases {
		source := tc.source
		// Comments in these rules quote CSS of their own, braces included, so
		// the block has to be located in the declarations alone.
		source = stripCSSComments(source)
		start := strings.Index(source, selector+" {")
		if start < 0 {
			t.Errorf("%s: rule not found", selector)
			continue
		}
		end := strings.Index(source[start:], "}")
		if end < 0 {
			t.Errorf("%s: unterminated rule", selector)
			continue
		}
		rule := source[start : start+end]
		for _, want := range []string{tc.width, "margin: 0"} {
			if !strings.Contains(rule, want) {
				t.Errorf("%s must declare %q so a page's own control rule cannot resize it", selector, want)
			}
		}
	}
}

func allThemes() []string {
	out := make([]string, 0, len(themeChoices))
	for _, choice := range themeChoices {
		out = append(out, choice.id)
	}
	return out
}
