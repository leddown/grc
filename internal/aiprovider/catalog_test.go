package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// catalogServer stands in for a wintermuted server's discovery endpoints.
// A route left out of routes answers 404, as an older server would.
func catalogServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer client-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no such route"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// catalogJSON renders a catalog the way a handler hands it to a browser.
func catalogJSON(t *testing.T, catalog Catalog) string {
	t.Helper()
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func catalogProvider(url string) *Wintermute {
	return NewWintermute(func() WintermuteConfig {
		return WintermuteConfig{URL: url, Token: "client-token"}
	})
}

func TestCatalogListsBackendsAndModels(t *testing.T) {
	server := catalogServer(t, map[string]string{
		"/api/v1/backends": `{"backends":[
			{"name":"workshop","kind":"ollama","base_url":"http://workshop:11434","status":"ok","api_key_env":"SECRET_ENV"}],
			"default":"workshop","fallback":"claude"}`,
		"/api/v1/models": `{"models":[{"backend":"workshop","id":"qwen3:8b","params_b":8,"loaded":true}]}`,
	})

	catalog, err := catalogProvider(server.URL).Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if len(catalog.Backends) != 1 || catalog.Backends[0].Name != "workshop" || catalog.Backends[0].Kind != "ollama" {
		t.Fatalf("backends = %+v", catalog.Backends)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].ID != "qwen3:8b" || catalog.Models[0].Backend != "workshop" {
		t.Errorf("models = %+v", catalog.Models)
	}
	if catalog.DefaultBackend != "workshop" || catalog.Fallback != "claude" {
		t.Errorf("routing defaults = %q / %q", catalog.DefaultBackend, catalog.Fallback)
	}
	if catalog.ModelsError != "" {
		t.Errorf("models_error = %q, want none", catalog.ModelsError)
	}
}

// A backend record on the server carries its base URL and the name of the
// environment variable holding its vendor key. Neither has any business
// reaching a browser, so the catalog must not be a passthrough.
func TestCatalogDropsBackendConnectionDetail(t *testing.T) {
	server := catalogServer(t, map[string]string{
		"/api/v1/backends": `{"backends":[
			{"name":"workshop","kind":"ollama","base_url":"http://workshop:11434","api_key_env":"SECRET_ENV"}]}`,
		"/api/v1/models": `{"models":[]}`,
	})

	catalog, err := catalogProvider(server.URL).Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	rendered := catalogJSON(t, catalog)
	for _, leaked := range []string{"workshop:11434", "SECRET_ENV", "base_url", "api_key_env"} {
		if strings.Contains(rendered, leaked) {
			t.Errorf("the catalog carries %q: %s", leaked, rendered)
		}
	}
}

// A server that cannot list models is still one whose backends can be chosen:
// a backend answers on its own default.
func TestCatalogSurvivesAMissingModelList(t *testing.T) {
	server := catalogServer(t, map[string]string{
		"/api/v1/backends": `{"backends":[{"name":"workshop","kind":"ollama"}]}`,
	})

	catalog, err := catalogProvider(server.URL).Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if len(catalog.Backends) != 1 {
		t.Errorf("backends = %+v, want the list to survive", catalog.Backends)
	}
	if !strings.Contains(catalog.ModelsError, "no models endpoint") {
		t.Errorf("models_error = %q, want it to name the missing endpoint", catalog.ModelsError)
	}
	if catalog.Models == nil {
		t.Error("models is nil; callers render it as a list")
	}
}

// A 404 on the backends list is an older server, not a wrong URL, and saying
// so is the difference between checking the address and checking the version.
func TestCatalogNamesAnOlderServer(t *testing.T) {
	server := catalogServer(t, map[string]string{})
	_, err := catalogProvider(server.URL).Catalog(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no backends endpoint") {
		t.Fatalf("err = %v, want it to name the missing endpoint", err)
	}
}

func TestCatalogWithoutConfiguration(t *testing.T) {
	_, err := NewWintermute(func() WintermuteConfig { return WintermuteConfig{} }).
		Catalog(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestAgentsList(t *testing.T) {
	server := catalogServer(t, map[string]string{
		"/api/v1/agents": `{"agents":[{"id":"grc","name":"GRC","description":"catalog agent"}]}`,
	})
	agents, err := catalogProvider(server.URL).Agents(context.Background())
	if err != nil {
		t.Fatalf("Agents: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "grc" || agents[0].Name != "GRC" {
		t.Fatalf("agents = %+v", agents)
	}
}

func TestAgentsOnAServerWithoutThem(t *testing.T) {
	server := catalogServer(t, map[string]string{})
	_, err := catalogProvider(server.URL).Agents(context.Background())
	if err == nil || !strings.Contains(err.Error(), "predates agent profiles") {
		t.Fatalf("err = %v, want it to explain the missing endpoint", err)
	}
}

// TestClaudeModels checks the model list against a stand-in for the Anthropic
// API: the SDK's pagination is followed, and only what a page renders is kept.
