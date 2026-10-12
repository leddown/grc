package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestAdminTokenMiddleware_RequiresAuthenticationWhenTokenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	admin := router.Group("/")
	admin.Use(adminTokenMiddleware(nil, ""))
	admin.POST("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAdminTokenMiddleware_RejectsMissingOrInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	admin := router.Group("/")
	admin.Use(adminTokenMiddleware(nil, "secret-token"))
	admin.POST("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d want=%d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/probe", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status=%d want=%d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminTokenMiddleware_AllowsValidBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	admin := router.Group("/")
	admin.Use(adminTokenMiddleware(nil, "secret-token"))
	admin.POST("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

// A signed-in non-admin hitting an admin-only page must see an explanation,
// not a bounce back through /login that reads as "you got signed out" — see
// the adminTokenMiddleware doc comment for why the two cases are kept apart.
func TestAdminTokenMiddleware_SignedInNonAdminGetsExplanationNotLoginRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "admin-gate.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	authService := authn.NewService(sqlite)
	authService.SetSingleUserMode(false)
	if _, err := authService.BootstrapAdmin("admin", "very-strong-pass-1"); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	nonAdmin, err := authService.CreateUserWithAccess("analyst", "very-strong-pass-2", false, nil)
	if err != nil {
		t.Fatalf("CreateUserWithAccess: %v", err)
	}
	session, err := authService.CreateSession(nonAdmin.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	router := gin.New()
	router.Use(pageAccessMiddleware(authService))
	admin := router.Group("/")
	admin.Use(adminTokenMiddleware(authService, ""))
	admin.GET("/utilities", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/utilities", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: session.Token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if rec.Header().Get("Location") != "" {
		t.Fatalf("non-admin session must not be redirected, got Location=%q", rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Body.String(), "Admin Access Required") {
		t.Fatalf("body should explain the admin gate, got: %s", rec.Body.String())
	}
}

func TestRegisterRoutes_UsersMutationRequiresAdminToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "auth-routes.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	userRepo := user.NewSQLiteRepository(sqlite)
	userSvc := user.NewService(userRepo)
	userHandler := user.NewHandler(userSvc)
	controlHandler := controlcatalog.NewHandler(controlcatalog.NewService(controlcatalog.NewSQLiteRepository(sqlite), ""), nfrlink.NewService(sqlite))
	nfrHandler := securitynfr.NewHandler(securitynfr.NewService(securitynfr.NewSQLiteRepository(sqlite), ""), nfrlink.NewService(sqlite))
	linkHandler := nfrlink.NewHandler(nfrlink.NewService(sqlite))
	reportsHandler := reports.NewHandler(reports.NewService(sqlite))
	riskRegisterHandler := riskregister.NewHandler(riskregister.NewService(riskregister.NewSQLiteRepository(sqlite)))

	router := gin.New()
	registerRoutes(
		router,
		userHandler,
		authn.NewHandler(authn.NewService(sqlite)),
		controlHandler,
		nfrHandler,
		linkHandler,
		reportsHandler,
		riskRegisterHandler,
		adminTokenMiddleware(nil, ""),
		false,
		false,
	)

	req := httptest.NewRequest(http.MethodPost, "/users", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
