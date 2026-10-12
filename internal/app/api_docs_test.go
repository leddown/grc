package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOpenAPIJSON_ContainsAdminProtectedPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/openapi.json", openAPIJSON)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	required := []string{
		`"/security-nfrs/links/rebuild"`,
		`"AdminBearer"`,
		`"/controls/{controlID}"`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("openapi missing fragment %q", fragment)
		}
	}
}
