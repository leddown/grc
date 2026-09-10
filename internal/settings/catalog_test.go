package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/aiprovider"
)

// memPrefs is an in-memory PreferenceRepository.
type memPrefs struct{ rows map[string]string }

func newMemPrefs() *memPrefs { return &memPrefs{rows: map[string]string{}} }

func (m *memPrefs) GetPreference(key string) (string, error) { return m.rows[key], nil }

func (m *memPrefs) SetPreference(key, value string) error {
	m.rows[key] = value
	return nil
}

// catalogHandler wires a Handler against the stub Wintermute server at url.
func catalogHandler(t *testing.T, url string) *Handler {
	t.Helper()
	svc, _ := testService(t)
	prefs := newMemPrefs()
	svc = svc.WithPreferences(prefs)
	if err := svc.SetPreference(PrefWintermuteURL, url); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	if err := svc.Set(WintermuteToken, "client-token-value", "alice"); err != nil {
		t.Fatalf("Set token: %v", err)
	}
	return NewHandler(svc, nil)
}

func getCatalog(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterAdminRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings/ai-providers/catalog", nil))
	return rec
}

func TestListCatalogReturnsBackendsAndModels(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		// The client token must reach the server, and only from here — it is
		// never handed to the browser.
		if got := r.Header.Get("Authorization"); got != "Bearer client-token-value" {
			t.Errorf("Authorization = %q, want the configured client token", got)
		}
		switch r.URL.Path {
		case "/api/v1/backends":
			_, _ = w.Write([]byte(`{"backends":[{"name":"workshop","kind":"ollama","status":"ok"}],
				"default":"workshop","fallback":"claude"}`))
		case "/api/v1/models":
			_, _ = w.Write([]byte(`{"models":[{"backend":"workshop","id":"qwen3:8b","params_b":8,"loaded":true}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rec := getCatalog(t, catalogHandler(t, server.URL))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp aiprovider.Catalog
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Backends) != 1 || resp.Backends[0].Name != "workshop" || resp.Backends[0].Kind != "ollama" {
		t.Errorf("backends = %+v", resp.Backends)
	}
	if len(resp.Models) != 1 || resp.Models[0].ID != "qwen3:8b" || resp.Models[0].Backend != "workshop" {
		t.Errorf("models = %+v", resp.Models)
	}
	if resp.DefaultBackend != "workshop" || resp.Fallback != "claude" {
		t.Errorf("routing defaults = %q / %q", resp.DefaultBackend, resp.Fallback)
	}
	if resp.ModelsError != "" {
		t.Errorf("models_error = %q, want none", resp.ModelsError)
	}
	if len(seen) != 2 {
		t.Errorf("requested %v, want both lists", seen)
	}
}

// A server that cannot list models is still one whose backends can be chosen:
// a backend answers on its own default model.
func TestListCatalogSurvivesAModelListFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/backends" {
			_, _ = w.Write([]byte(`{"backends":[{"name":"workshop","kind":"ollama","status":"ok"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	rec := getCatalog(t, catalogHandler(t, server.URL))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp aiprovider.Catalog
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Backends) != 1 {
		t.Errorf("backends = %+v, want the list to survive", resp.Backends)
	}
	if resp.ModelsError == "" {
		t.Error("models_error is empty, so the page would show an empty list as if it were complete")
	}
	if resp.Models == nil {
		t.Error("models is null; the page expects an array")
	}
}

func TestListClaudeModelsWithoutAKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	svc, _ := testService(t)
	h := NewHandler(svc.WithPreferences(newMemPrefs()), nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterAdminRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings/ai-providers/claude-models", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Anthropic API key") {
		t.Errorf("body = %s, want it to name the missing credential", rec.Body.String())
	}
}

// The Claude model is saved through the same endpoint as the rest of the
// provider settings, and an empty value clears it back to the default.
func TestSetPreferencesStoresTheClaudeModel(t *testing.T) {
	svc, _ := testService(t)
	h := NewHandler(svc.WithPreferences(newMemPrefs()), nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterAdminRoutes(r)

	put := func(body string) providerResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/settings/ai-providers", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp providerResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	if got := put(`{"claude_model":" claude-sonnet-5 "}`).Preferences[PrefClaudeModel]; got != "claude-sonnet-5" {
		t.Errorf("stored claude model = %q, want it trimmed", got)
	}
	if got := put(`{"claude_model":""}`).Preferences[PrefClaudeModel]; got != "" {
		t.Errorf("cleared claude model = %q, want empty", got)
	}
}

func TestListCatalogWithoutAServerURL(t *testing.T) {
	// The URL preference falls back to the environment, so a developer machine
	// with one set must not turn this into a live request.
	t.Setenv("WINTERMUTE_URL", "")
	h := catalogHandler(t, "")
	rec := getCatalog(t, h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "server URL") {
		t.Errorf("body = %s, want it to name the missing setting", rec.Body.String())
	}
}
