package nfrlink

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPage_RendersLinkTable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil)
	router := gin.New()
	router.GET("/security-nfrs/links", handler.Page)

	req := httptest.NewRequest(http.MethodGet, "/security-nfrs/links", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	required := []string{
		`<section class="table-wrap">`,
		`<table>`,
		`<th>NFR Summary</th>`,
		`<th>Linked Control ID</th>`,
		`fetch('/security-nfrs/links/rebuild', { method: 'POST' })`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("page missing fragment %q", fragment)
		}
	}
}

func TestPage_UsesReportsTablePalette(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil)
	router := gin.New()
	router.GET("/security-nfrs/links", handler.Page)

	req := httptest.NewRequest(http.MethodGet, "/security-nfrs/links", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	required := []string{
		`border: 1px solid #444;`,
		`background: #242424;`,
		`background: #efe6d6;`,
		`color: #5a4630;`,
		`border-bottom: 1px solid rgba(215,206,191,0.7);`,
		`background: #2e2e2e;`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("page missing table palette fragment %q", fragment)
		}
	}

	forbidden := []string{
		`background: rgba(255,255,255,0.86);`,
		`background: rgba(255,255,255,0.5);`,
	}
	for _, fragment := range forbidden {
		if strings.Contains(body, fragment) {
			t.Fatalf("page contains outdated table fragment %q", fragment)
		}
	}
}
