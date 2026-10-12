package authn

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"grc/internal/db"
)

func TestBootstrapAndLoginFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "authn-handler.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	handler := NewHandler(NewService(sqlite))
	router := gin.New()
	router.POST("/auth/bootstrap-admin", handler.BootstrapAdmin)
	router.POST("/auth/login", handler.Login)
	router.GET("/auth/me", handler.Me)

	_ = os.Setenv("SETUP_TOKEN", "setup-secret")
	t.Cleanup(func() { _ = os.Unsetenv("SETUP_TOKEN") })

	bootstrapBody := []byte(`{"username":"admin","password":"very-strong-pass-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap-admin", bytes.NewReader(bootstrapBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Token", "setup-secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}

	loginBody := []byte(`{"username":"admin","password":"very-strong-pass-1"}`)
	req = httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}

	cookie := rec.Header().Get("Set-Cookie")
	if cookie == "" {
		t.Fatalf("expected auth session cookie from login")
	}

	req = httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Cookie", cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", rec.Code, rec.Body.String())
	}
}
