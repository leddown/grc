package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/db"
)

func TestStoredJSONDocuments_SaveListAndLoad(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "stored-json.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	prevStore := activeStoredJSONDB
	configureStoredJSONStore(sqlite)
	t.Cleanup(func() { activeStoredJSONDB = prevStore })

	router := gin.New()
	router.POST("/stored-json", saveStoredJSONDocument)
	router.GET("/stored-json", listStoredJSONDocuments)
	router.GET("/stored-json/:id", getStoredJSONDocument)

	saveRec := performJSONRequest(t, router, http.MethodPost, "/stored-json", map[string]any{
		"name":   "fixture.json",
		"source": "unit-test",
		"content": map[string]any{
			"hello": "world",
			"items": []map[string]any{
				{"key": "SEC-1", "summary": "Stored issue"},
			},
		},
	})
	if saveRec.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", saveRec.Code, saveRec.Body.String())
	}

	var saved struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(saveRec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("unmarshal saved payload: %v", err)
	}
	if saved.ID == 0 || saved.Name != "fixture.json" || saved.Source != "unit-test" {
		t.Fatalf("unexpected saved payload: %+v", saved)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/stored-json", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listPayload struct {
		Items []storedJSONDocumentMetadata `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("unmarshal list payload: %v", err)
	}
	if len(listPayload.Items) != 1 || listPayload.Items[0].ID != saved.ID {
		t.Fatalf("unexpected list payload: %+v", listPayload)
	}

	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/stored-json/%d", saved.ID), nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var getPayload struct {
		ID      int64          `json:"id"`
		Name    string         `json:"name"`
		Source  string         `json:"source"`
		Content map[string]any `json:"content"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("unmarshal get payload: %v", err)
	}
	if getPayload.ID != saved.ID || getPayload.Content["hello"] != "world" {
		t.Fatalf("unexpected get payload: %+v", getPayload)
	}
}
