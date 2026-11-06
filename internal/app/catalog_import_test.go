package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/controlcatalog"
	"grc/internal/nfrlink"
	"grc/internal/securitynfr"
)

const (
	bundledControlsFile = "../data/merged_nist_controls_master_replaced_from_controls_all.json"
	bundledNFRsFile     = "../data/NFR_incremental_keys_with_domain.json"
)

// importBundledCatalogs fills a fresh database the way an administrator does:
// the 800-53 file and the NFR file through their import endpoints.
func importBundledCatalogs(t *testing.T, controls *controlcatalog.Handler, nfrs *securitynfr.Handler) {
	t.Helper()
	router := gin.New()
	router.POST("/controls/import", controls.ImportControls)
	router.POST("/security-nfrs/import", nfrs.ImportNFRs)
	for path, file := range map[string]string{"/controls/import": bundledControlsFile, "/security-nfrs/import": bundledNFRsFile} {
		if rec := postImport(t, router, path, file, true); rec.Code != http.StatusOK {
			t.Fatalf("POST %s: code=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

// postImport sends a file the way the editor pages do: a multipart form with
// the file in the "file" field.
func postImport(t *testing.T, router http.Handler, path, file string, replace bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(raw)
	_ = form.Close()
	if replace {
		path += "?replace=true"
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCatalogsStartEmptyAndImportLinksThem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := sqliteNoopDB(t)
	userHandler, _, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler, err := buildHandlers(conn)
	if err != nil {
		t.Fatalf("buildHandlers: %v", err)
	}
	router := gin.New()
	registerRoutes(router, userHandler, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler,
		noAuthMiddleware, false, true)

	if controls, err := controlHandler.Service().List("", "", ""); err != nil || len(controls) != 0 {
		t.Fatalf("a new database holds %d controls (%v); the catalog must start empty", len(controls), err)
	}
	if nfrs, err := nfrHandler.Service().List("", ""); err != nil || len(nfrs) != 0 {
		t.Fatalf("a new database holds %d NFRs (%v); the catalog must start empty", len(nfrs), err)
	}

	// NFRs first: until the controls arrive, nothing they map to exists.
	rec := postImport(t, router, "/security-nfrs/import", bundledNFRsFile, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("NFR import: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var nfrResult securitynfr.ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &nfrResult); err != nil || nfrResult.Imported < 100 {
		t.Fatalf("NFR import result=%+v (%v)", nfrResult, err)
	}
	if matched := countMatchedLinks(t, linkHandler.Service()); matched != 0 {
		t.Fatalf("%d links matched with no controls imported", matched)
	}

	rec = postImport(t, router, "/controls/import", bundledControlsFile, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("control import: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if matched := countMatchedLinks(t, linkHandler.Service()); matched < 100 {
		t.Fatalf("only %d NFR-control links matched after both imports", matched)
	}

	// A bad file is refused and leaves the catalog as it was.
	req := httptest.NewRequest(http.MethodPost, "/controls/import?replace=true", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty file: code=%d want 400", rec.Code)
	}
	if controls, err := controlHandler.Service().List("", "", ""); err != nil || len(controls) < 1000 {
		t.Fatalf("a refused import left %d controls (%v)", len(controls), err)
	}
}

func countMatchedLinks(t *testing.T, links *nfrlink.Service) int {
	t.Helper()
	rows, err := links.List("", false)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	matched := 0
	for _, row := range rows {
		if row.Matched {
			matched++
		}
	}
	return matched
}
