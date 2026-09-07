package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/settings"
)

// catalogRequest runs one GET against the AI Chat catalog route.
func catalogRequest(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerAIAuxRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ai-chat/wintermute/catalog"+query, nil))
	return rec
}

func TestAIChatCatalogUsesTheStoredServer(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("WINTERMUTE_URL", "")
	t.Setenv("WINTERMUTE_TOKEN", "")

	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		// The client token comes from Settings, never from the request.
		if got := r.Header.Get("Authorization"); got != "Bearer stored-token" {
			t.Errorf("Authorization = %q, want the stored client token", got)
		}
		switch r.URL.Path {
		case "/api/v1/backends":
			_, _ = w.Write([]byte(`{"backends":[{"name":"workshop","kind":"ollama","status":"ok"}],"default":"workshop"}`))
		default:
			_, _ = w.Write([]byte(`{"models":[{"backend":"workshop","id":"qwen3:8b"}]}`))
		}
	}))
	defer server.Close()

	svc := newTestSettings(t)
	if err := svc.Set(settings.WintermuteToken, "stored-token", "alice"); err != nil {
		t.Fatalf("Set token: %v", err)
	}
	if err := svc.SetPreference(settings.PrefWintermuteURL, server.URL); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	configureAICredentials(svc)

	rec := catalogRequest(t, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"workshop"`) || !strings.Contains(rec.Body.String(), `"qwen3:8b"`) {
		t.Errorf("body = %s, want both lists", rec.Body.String())
	}
	if len(asked) != 2 {
		t.Errorf("asked for %v, want both lists", asked)
	}
}

// The page's server-URL override reaches no further than the ask path already
// does: the same rules refuse a plaintext URL to a host off the local network.
func TestAIChatCatalogRefusesAnUnacceptableURL(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("WINTERMUTE_URL", "")
	t.Setenv("WINTERMUTE_TOKEN", "")

	svc := newTestSettings(t)
	if err := svc.Set(settings.WintermuteToken, "stored-token", "alice"); err != nil {
		t.Fatalf("Set token: %v", err)
	}
	configureAICredentials(svc)

	rec := catalogRequest(t, "?url=http://wintermute.example.com")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https") {
		t.Errorf("body = %s, want it to explain the rule", rec.Body.String())
	}
}

func TestAIChatCatalogWithoutAToken(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("WINTERMUTE_URL", "")
	t.Setenv("WINTERMUTE_TOKEN", "")
	configureAICredentials(newTestSettings(t))

	rec := catalogRequest(t, "?url=http://127.0.0.1:8080")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "client token") {
		t.Errorf("body = %s, want it to name the missing credential", rec.Body.String())
	}
}

// agentsRequest runs one GET against the AI Chat agent route.
func agentsRequest(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerAIAuxRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ai-chat/wintermute/agents"+query, nil))
	return rec
}

func TestAIChatAgentsUsesTheStoredServer(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("WINTERMUTE_URL", "")
	t.Setenv("WINTERMUTE_TOKEN", "")

	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		// The client token comes from Settings, never from the request.
		if got := r.Header.Get("Authorization"); got != "Bearer stored-token" {
			t.Errorf("Authorization = %q, want the stored client token", got)
		}
		_, _ = w.Write([]byte(`{"agents":[{"id":"grc","name":"GRC","description":"the catalogs","sources":["policies"]}]}`))
	}))
	defer server.Close()

	svc := newTestSettings(t)
	if err := svc.Set(settings.WintermuteToken, "stored-token", "alice"); err != nil {
		t.Fatalf("Set token: %v", err)
	}
	if err := svc.SetPreference(settings.PrefWintermuteURL, server.URL); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	configureAICredentials(svc)

	rec := agentsRequest(t, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"grc"`) {
		t.Errorf("body = %s, want the server's agent list", rec.Body.String())
	}
	if len(asked) != 1 || asked[0] != "/api/v1/agents" {
		t.Errorf("asked for %v, want the agent list", asked)
	}
}

// The page's server-URL override reaches no further than the ask path already
// does, exactly as it does for the backend and model lists.
func TestAIChatAgentsRefusesAnUnacceptableURL(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("WINTERMUTE_URL", "")
	t.Setenv("WINTERMUTE_TOKEN", "")

	svc := newTestSettings(t)
	if err := svc.Set(settings.WintermuteToken, "stored-token", "alice"); err != nil {
		t.Fatalf("Set token: %v", err)
	}
	configureAICredentials(svc)

	rec := agentsRequest(t, "?url=http://wintermute.example.com")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https") {
		t.Errorf("body = %s, want it to explain the rule", rec.Body.String())
	}
}

func TestAIChatAgentsWithoutAToken(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("WINTERMUTE_URL", "")
	t.Setenv("WINTERMUTE_TOKEN", "")
	configureAICredentials(newTestSettings(t))

	rec := agentsRequest(t, "?url=http://127.0.0.1:8080")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "client token") {
		t.Errorf("body = %s, want it to name the missing credential", rec.Body.String())
	}
}

// modelsRequest runs one GET against the AI Chat Claude model route.
func modelsRequest(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerAIAuxRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ai-chat/claude/models"+query, nil))
	return rec
}

func TestAIChatClaudeModelsWithoutAKey(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("ANTHROPIC_API_KEY", "")
	configureAICredentials(newTestSettings(t))

	rec := modelsRequest(t, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Anthropic API key") {
		t.Errorf("body = %s, want it to name the missing credential", rec.Body.String())
	}
}

// The endpoint override stays restricted to Anthropic's own host, exactly as it
// is on the ask path — a listing request carries the same key.
func TestAIChatClaudeModelsRefusesAForeignEndpoint(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })
	t.Setenv("ANTHROPIC_API_KEY", "")

	svc := newTestSettings(t)
	if err := svc.Set(settings.AnthropicAPIKey, "sk-ant-stored", "alice"); err != nil {
		t.Fatalf("Set key: %v", err)
	}
	configureAICredentials(svc)

	rec := modelsRequest(t, "?endpoint=https://models.example.com")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
}
