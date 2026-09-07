package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"grc/internal/authn"
	"grc/internal/controlcatalog"
	"grc/internal/db"
	"grc/internal/nfrlink"
	"grc/internal/reports"
	"grc/internal/riskregister"
	"grc/internal/securitynfr"
	"grc/internal/user"
)

// fullNavLinks is every destination the shared sidebar (pageui.Nav) renders.
// Because Nav renders the same grouped nav on every page, this same list
// applies whether the page under test is Home or a deep detail page — there
// is no longer a "primary" subset that leaves some pages unable to reach
// Risk Register, CRM, or API Docs the way the old per-page tab lists did.
var fullNavLinks = []string{
	`href="/"`,
	`href="/controls"`,
	`href="/controls/manage"`,
	`href="/controls/hierarchy"`,
	`href="/controls/family-visibility"`,
	`href="/security-nfrs"`,
	`href="/security-nfrs/manage"`,
	`href="/security-nfrs/links"`,
	`href="/asset-types"`,
	`href="/risk-register"`,
	`href="/risk-register/manage"`,
	`href="/exceptions"`,
	`href="/policies"`,
	`href="/policies/manage"`,
	`href="/policies/coverage"`,
	`href="/wiz-rules"`,
	`href="/reports"`,
	`href="/JSON_view"`,
	`href="/jira/json"`,
	`href="/jira/reports"`,
	`href="/changelog"`,
	`href="/ai-chat"`,
	`href="/templates"`,
	`href="/templates/manage"`,
	`href="/help"`,
	`href="/docs"`,
	`href="/utilities"`,
}

func TestPrimaryPagesRenderFullNavigation(t *testing.T) {
	router := newNavigationTestRouter(t)

	for _, path := range navigationTestPagePaths() {
		t.Run(path, func(t *testing.T) {
			body := getPageBody(t, router, path)
			if !strings.Contains(body, `class="tabs"`) {
				t.Fatalf("GET %s missing tabs nav", path)
			}

			for _, link := range fullNavLinks {
				if !strings.Contains(body, link) {
					t.Fatalf("GET %s missing nav link %s", path, link)
				}
			}
		})
	}
}

func TestHomePageTopNavigationIncludesAllPrimaryPages(t *testing.T) {
	router := newNavigationTestRouter(t)
	body := getPageBody(t, router, "/")

	for _, link := range fullNavLinks {
		if !strings.Contains(body, link) {
			t.Fatalf("home page missing nav link %s", link)
		}
	}
}

func TestJSONViewPageSmoke(t *testing.T) {
	router := newNavigationTestRouter(t)
	body := getPageBody(t, router, "/JSON_view")

	requiredFragments := []string{
		`<title>JSON_view · GRC</title>`,
		`Security NFR JSON`,
		`Family JSON`,
		`Stored SQLite JSON`,
		`/security-nfrs/json/data`,
		`/controls/family-json/data`,
		`/stored-json`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(body, fragment) {
			t.Fatalf("JSON_view missing fragment %q", fragment)
		}
	}
}

func TestLegacyJSONPagesRemoved(t *testing.T) {
	router := newNavigationTestRouter(t)

	legacyPaths := []string{
		"/controls/family-json",
		"/security-nfrs/json",
	}

	for _, path := range legacyPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s status=%d want=%d", path, rec.Code, http.StatusNotFound)
			}
		})
	}
}

func TestPrimaryPagesUseSharedDarkPalette(t *testing.T) {
	router := newNavigationTestRouter(t)

	requiredPaletteFragments := []string{
		`color-scheme: light;`,
		`--ink: `,
		`color: var(--ink);`,
	}

	for _, path := range navigationTestPagePaths() {
		t.Run(path, func(t *testing.T) {
			body := getPageBody(t, router, path)
			for _, fragment := range requiredPaletteFragments {
				if !strings.Contains(body, fragment) {
					t.Fatalf("GET %s missing palette fragment %s", path, fragment)
				}
			}
		})
	}
}

func TestPrimaryPagesUseSharedMainSpacing(t *testing.T) {
	router := newNavigationTestRouter(t)

	acceptableMaxWidthFragments := []string{
		`max-width: 1320px;`,
		`max-width: min(1600px, calc(100vw - 32px));`,
	}
	requiredSpacingFragments := []string{
		`margin: 24px auto;`,
		`padding: 24px;`,
	}

	for _, path := range navigationTestPagePaths() {
		t.Run(path, func(t *testing.T) {
			body := getPageBody(t, router, path)
			if !containsAny(body, acceptableMaxWidthFragments) {
				t.Fatalf("GET %s missing acceptable max-width fragment", path)
			}
			for _, fragment := range requiredSpacingFragments {
				if !strings.Contains(body, fragment) {
					t.Fatalf("GET %s missing spacing fragment %s", path, fragment)
				}
			}
		})
	}
}

func TestDetailPagesRenderFullNavigation(t *testing.T) {
	router := newDetailNavigationTestRouter(t)

	// A detail page has no nav entry of its own (there is no href for
	// /controls/detail/AU-1), so instead of a page-specific tab it should
	// highlight its nearest ancestor as active — /controls for a control
	// detail page, /security-nfrs for an NFR detail page.
	tests := []struct {
		path              string
		activeAncestorTab string
	}{
		{path: "/controls/detail/AU-1", activeAncestorTab: `class="tab active" href="/controls"`},
		{path: "/security-nfrs/detail/1", activeAncestorTab: `class="tab active" href="/security-nfrs"`},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			body := getPageBody(t, router, tt.path)
			if !strings.Contains(body, `class="tabs"`) {
				t.Fatalf("GET %s missing tabs nav", tt.path)
			}

			for _, link := range fullNavLinks {
				if !strings.Contains(body, link) {
					t.Fatalf("GET %s missing nav link %s", tt.path, link)
				}
			}
			if !strings.Contains(body, tt.activeAncestorTab) {
				t.Fatalf("GET %s missing active ancestor tab %s", tt.path, tt.activeAncestorTab)
			}
		})
	}
}

func TestDetailPagesUseSharedMainSpacing(t *testing.T) {
	router := newDetailNavigationTestRouter(t)

	acceptableMaxWidthFragments := []string{
		`max-width: 1320px;`,
		`max-width: min(1600px, calc(100vw - 32px));`,
	}
	requiredSpacingFragments := []string{
		`margin: 24px auto;`,
		`padding: 24px;`,
	}

	paths := []string{
		"/controls/detail/AU-1",
		"/security-nfrs/detail/1",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			body := getPageBody(t, router, path)
			if !containsAny(body, acceptableMaxWidthFragments) {
				t.Fatalf("GET %s missing acceptable max-width fragment", path)
			}
			for _, fragment := range requiredSpacingFragments {
				if !strings.Contains(body, fragment) {
					t.Fatalf("GET %s missing spacing fragment %s", path, fragment)
				}
			}
		})
	}
}

func newNavigationTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	registerRoutes(
		router,
		user.NewHandler(nil),
		authn.NewHandler(authn.NewService(sqliteNoopDB(t))),
		controlcatalog.NewHandler(nil, nil),
		securitynfr.NewHandler(nil, nil),
		nfrlink.NewHandler(nil),
		reports.NewHandler(nil),
		riskregister.NewHandler(riskregister.NewService(riskregister.NewSQLiteRepository(sqliteNoopDB(t)))),
		adminTokenMiddleware(nil, ""),
		false,
		false,
	)
	return router
}

func newDetailNavigationTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "nav-detail.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	controlRepo := controlcatalog.NewSQLiteRepository(sqlite)
	if err := controlRepo.Upsert(controlcatalog.Control{
		SourceKey:    "1",
		ID:           "AU-1",
		Type:         "Control",
		Name:         "Audit Policy",
		Family:       "AU",
		Baselines:    []string{"Low"},
		Threats:      []string{"Repudiation"},
		Requirements: "Requirement",
		Discussion:   "Discussion",
		Appendix:     map[string]any{},
	}); err != nil {
		t.Fatalf("control upsert: %v", err)
	}

	nfrRepo := securitynfr.NewSQLiteRepository(sqlite)
	if err := nfrRepo.Upsert(securitynfr.NFR{
		Key:         "1",
		ID:          "1.0",
		Summary:     "Audit requirement",
		IssueType:   "Issue",
		Description: "Description",
		Domain:      "IAM",
	}); err != nil {
		t.Fatalf("nfr upsert: %v", err)
	}

	router := gin.New()
	registerRoutes(
		router,
		user.NewHandler(nil),
		authn.NewHandler(authn.NewService(sqlite)),
		controlcatalog.NewHandler(controlcatalog.NewService(controlRepo, ""), nil),
		securitynfr.NewHandler(securitynfr.NewService(nfrRepo, ""), nil),
		nfrlink.NewHandler(nil),
		reports.NewHandler(nil),
		riskregister.NewHandler(riskregister.NewService(riskregister.NewSQLiteRepository(sqlite))),
		adminTokenMiddleware(nil, ""),
		false,
		false,
	)
	return router
}

func sqliteNoopDB(t *testing.T) *db.Conn {
	t.Helper()
	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "noop.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	return sqlite
}

func navigationTestPagePaths() []string {
	return []string{
		"/",
		"/JSON_view",
		"/jira/json",
		"/jira/reports",
		"/changelog",
		"/asset-types",
		"/exceptions",
		"/exceptions/detail",
		"/ai-chat",
		"/wiz-rules",
		"/help",
		"/controls",
		"/controls/manage",
		"/controls/family-visibility",
		"/controls/hierarchy",
		"/security-nfrs",
		"/security-nfrs/manage",
		"/security-nfrs/links",
		"/reports",
	}
}

func getPageBody(t *testing.T, router *gin.Engine, path string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
	}

	return rec.Body.String()
}

func containsAny(body string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(body, fragment) {
			return true
		}
	}
	return false
}
