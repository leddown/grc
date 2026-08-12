package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/db"
)

func TestServiceParseID(t *testing.T) {
	svc := NewService(nil)

	if _, err := svc.ParseID("0"); err != ErrInvalidID {
		t.Fatalf("ParseID(0) err=%v want=%v", err, ErrInvalidID)
	}
	if _, err := svc.ParseID("abc"); err != ErrInvalidID {
		t.Fatalf("ParseID(abc) err=%v want=%v", err, ErrInvalidID)
	}
	got, err := svc.ParseID("12")
	if err != nil || got != 12 {
		t.Fatalf("ParseID(12)=(%d,%v) want (12,nil)", got, err)
	}
}

func TestHandlerUserLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	handler := NewHandler(NewService(NewSQLiteRepository(sqlite)))
	router := gin.New()
	router.GET("/health", handler.Health)
	router.POST("/users", handler.CreateUser)
	router.GET("/users", handler.ListUsers)
	router.GET("/users/:id", handler.GetUser)
	router.PUT("/users/:id", handler.UpdateUser)
	router.DELETE("/users/:id", handler.DeleteUser)

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	router.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK || !strings.Contains(healthRec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health response unexpected: status=%d body=%s", healthRec.Code, healthRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Ada","email":"ada@example.com"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	var created User
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created user: %v", err)
	}
	if created.ID <= 0 || created.Email != "ada@example.com" {
		t.Fatalf("unexpected created user: %+v", created)
	}

	dupReq := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Ada 2","email":"ada@example.com"}`))
	dupReq.Header.Set("Content-Type", "application/json")
	dupRec := httptest.NewRecorder()
	router.ServeHTTP(dupRec, dupReq)
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d body=%s", dupRec.Code, dupRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "ada@example.com") {
		t.Fatalf("list response unexpected: status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/users/1", strings.NewReader(`{"name":"Ada Lovelace","email":"ada.l@example.com"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	router.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK || !strings.Contains(updateRec.Body.String(), "ada.l@example.com") {
		t.Fatalf("update response unexpected: status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || !strings.Contains(getRec.Body.String(), "Ada Lovelace") {
		t.Fatalf("get response unexpected: status=%d body=%s", getRec.Code, getRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	missingRec := httptest.NewRecorder()
	router.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d body=%s", missingRec.Code, missingRec.Body.String())
	}
}
