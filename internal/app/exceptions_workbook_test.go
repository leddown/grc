package app

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/exceptionbook"
)

func TestExceptionsWorkbookDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := sqliteNoopDB(t)
	userHandler, _, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler, err := buildHandlers(conn)
	if err != nil {
		t.Fatalf("buildHandlers: %v", err)
	}
	router := gin.New()
	registerRoutes(router, userHandler, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler,
		noAuthMiddleware, false, true)

	nfrs, err := nfrHandler.Service().List("", "")
	if err != nil || len(nfrs) == 0 {
		t.Fatalf("no seeded NFRs: %v", err)
	}

	download := func(query string) map[string]string {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/exceptions-workbook/download"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%.200s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != xlsxContentType {
			t.Fatalf("Content-Type=%q", got)
		}
		disposition := rec.Header().Get("Content-Disposition")
		if !strings.HasPrefix(disposition, `attachment; filename="grc-exceptions-workbook-`) || !strings.HasSuffix(disposition, `.xlsx"`) {
			t.Fatalf("Content-Disposition=%q", disposition)
		}
		archive, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil {
			t.Fatalf("the download is not a zip: %v", err)
		}
		parts := map[string]string{}
		for _, entry := range archive.File {
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			parts[entry.Name] = string(raw)
		}
		return parts
	}

	empty := download("")
	for _, name := range []string{exceptionbook.SheetGuide, exceptionbook.SheetExceptions, exceptionbook.SheetSummary,
		exceptionbook.SheetNFRDetail, exceptionbook.SheetControlDetail, exceptionbook.SheetLinks, exceptionbook.SheetControls} {
		if !strings.Contains(empty["xl/workbook.xml"], `<sheet name="`+name+`"`) {
			t.Errorf("the workbook has no %q sheet", name)
		}
	}
	// The Exceptions sheet is the second one.
	if !strings.Contains(empty["xl/worksheets/sheet2.xml"], "Selected Security NFRs: 0 | Linked Controls: 0 | C: 0 | I: 0 | A: 0") {
		t.Error("a workbook downloaded with no selection does not open empty")
	}
	if !strings.Contains(empty["xl/worksheets/sheet7.xml"], "<f>") {
		t.Error("the Controls sheet has no formulas")
	}

	selected := download("?key=" + nfrs[0].Key + "&key=" + nfrs[1].Key + "&key=no-such-key")
	if !strings.Contains(selected["xl/worksheets/sheet2.xml"], "Selected Security NFRs: 2 | ") {
		t.Error("?key= did not select the requirements in the workbook")
	}
}

func TestExceptionsWorkbookWithoutServices(t *testing.T) {
	router := newNavigationTestRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/exceptions-workbook/download", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
	}
}
