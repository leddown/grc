package aiprovider

import (
	"context"
	"errors"
	"testing"
)

func routerWith(t *testing.T, wintermuteCfg WintermuteConfig) (*Router, *[]string) {
	t.Helper()
	var logged []string
	r := NewRouter(newWintermute(wintermuteCfg), func(provider, model string, in, out int) {
		logged = append(logged, provider+"/"+model)
	})
	return r, &logged
}

// Every question goes to the Wintermute server; with none configured there is
// no provider, and the error says what to set. There is no Claude to fall back
// to: Claude is one of that server's backends now.
func TestRouterSelection(t *testing.T) {
	r, _ := routerWith(t, WintermuteConfig{URL: "http://nas.local:8080", Token: "tok"})
	if p, err := r.Selected(); err != nil || p.Name() != NameWintermute {
		t.Errorf("configured: %v, %v", p, err)
	}
	for _, cfg := range []WintermuteConfig{{}, {URL: "http://nas.local:8080"}, {Token: "tok"}} {
		r, _ := routerWith(t, cfg)
		if _, err := r.Selected(); !errors.Is(err, ErrNotConfigured) {
			t.Errorf("with %+v: %v, want ErrNotConfigured", cfg, err)
		}
	}
}

func TestRouterAskLogsUsageAgainstTheServingProvider(t *testing.T) {
	stub := &stubWintermute{token: "tok", reply: "answer", backend: "local-8b", model: "llama-3.1-8b"}
	srv := stub.server(t)

	r, logged := routerWith(t, WintermuteConfig{URL: srv.URL, Token: "tok"})
	resp, err := r.Ask(context.Background(), Request{Prompt: "q"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "answer" {
		t.Errorf("Text = %q", resp.Text)
	}
	// Usage is logged against what actually served the turn, not what was
	// requested, so the spend log stays truthful when a fallback kicks in.
	if len(*logged) != 1 || (*logged)[0] != "wintermute/llama-3.1-8b" {
		t.Errorf("logged usage = %v, want one wintermute/llama-3.1-8b entry", *logged)
	}
}

func TestRouterAskWithoutAProviderDoesNotLog(t *testing.T) {
	r, logged := routerWith(t, WintermuteConfig{})
	if _, err := r.Ask(context.Background(), Request{Prompt: "q"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Ask error = %v, want ErrNotConfigured", err)
	}
	if len(*logged) != 0 {
		t.Errorf("usage logged for a request that never ran: %v", *logged)
	}
}

func TestRouterStatus(t *testing.T) {
	r, _ := routerWith(t, WintermuteConfig{URL: "http://nas.local", Token: "tok"})
	if st := r.Status(); st.Active != NameWintermute || !st.WintermuteAvailable {
		t.Errorf("status = %+v", st)
	}
	empty, _ := routerWith(t, WintermuteConfig{})
	if st := empty.Status(); st.Active != "" || st.Detail == "" {
		t.Errorf("status with nothing configured = %+v, want no active provider and an explanation", st)
	}
}
