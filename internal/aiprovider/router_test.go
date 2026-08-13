package aiprovider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func routerWith(t *testing.T, preference string, claudeKey string, wintermuteCfg WintermuteConfig) (*Router, *[]string) {
	t.Helper()
	var logged []string
	claude := NewClaude(func() string { return claudeKey }, "")
	wintermute := newWintermute(wintermuteCfg)
	r := NewRouter(claude, wintermute, func() string { return preference },
		func(provider, model string, in, out int) {
			logged = append(logged, provider+"/"+model)
		})
	return r, &logged
}

func TestRouterSelection(t *testing.T) {
	configured := WintermuteConfig{URL: "http://nas.local:8080", Token: "tok"}

	tests := []struct {
		name       string
		preference string
		claudeKey  string
		wintermute WintermuteConfig
		want       string
		wantErr    string
	}{
		{
			// Auto prefers the local model: it is cheaper and more private,
			// and an operator who configured one meant to use it.
			name: "auto prefers wintermute when configured", preference: "auto",
			claudeKey: "sk-ant-key", wintermute: configured, want: NameWintermute,
		},
		{
			name: "auto falls back to claude", preference: "auto",
			claudeKey: "sk-ant-key", wintermute: WintermuteConfig{}, want: NameClaude,
		},
		{
			name: "auto with nothing configured", preference: "auto",
			wantErr: "set an Anthropic API key",
		},
		{
			name: "explicit claude", preference: "claude",
			claudeKey: "sk-ant-key", wintermute: configured, want: NameClaude,
		},
		{
			name: "explicit wintermute", preference: "wintermute",
			claudeKey: "sk-ant-key", wintermute: configured, want: NameWintermute,
		},
		{
			// An explicit choice must never silently reach for the other
			// provider: someone who picks Wintermute may be doing so because
			// questions must not leave the network.
			name: "explicit wintermute does not fall back to claude", preference: "wintermute",
			claudeKey: "sk-ant-key", wintermute: WintermuteConfig{},
			wantErr: "Wintermute is selected but has no server URL",
		},
		{
			name: "explicit claude does not fall back to wintermute", preference: "claude",
			wintermute: configured, wantErr: "Claude is selected but has no API key",
		},
		{
			name: "an unrecognised preference behaves as auto", preference: "nonsense",
			claudeKey: "sk-ant-key", want: NameClaude,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := routerWith(t, tc.preference, tc.claudeKey, tc.wintermute)
			provider, err := r.Selected()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Selected error = %v, want it to contain %q", err, tc.wantErr)
				}
				if !errors.Is(err, ErrNotConfigured) {
					t.Errorf("error %v should wrap ErrNotConfigured", err)
				}
				if r.Available() {
					t.Error("Available = true when Selected failed")
				}
				return
			}
			if err != nil {
				t.Fatalf("Selected: %v", err)
			}
			if provider.Name() != tc.want {
				t.Errorf("selected %q, want %q", provider.Name(), tc.want)
			}
			if !r.Available() {
				t.Error("Available = false when Selected succeeded")
			}
		})
	}
}

// TestRouterReadsPreferencePerRequest is what makes a Settings change take
// effect without a restart.
func TestRouterReadsPreferencePerRequest(t *testing.T) {
	preference := "claude"
	claude := NewClaude(func() string { return "sk-ant-key" }, "")
	wintermute := newWintermute(WintermuteConfig{URL: "http://nas.local", Token: "tok"})
	r := NewRouter(claude, wintermute, func() string { return preference }, nil)

	if p, _ := r.Selected(); p.Name() != NameClaude {
		t.Fatalf("initial selection = %q, want claude", p.Name())
	}
	preference = "wintermute"
	if p, _ := r.Selected(); p.Name() != NameWintermute {
		t.Errorf("after changing the preference = %q, want wintermute", p.Name())
	}
}

func TestRouterAskLogsUsageAgainstTheServingProvider(t *testing.T) {
	stub := &stubWintermute{token: "tok", reply: "answer", backend: "local-8b", model: "llama-3.1-8b"}
	srv := stub.server(t)

	r, logged := routerWith(t, "wintermute", "", WintermuteConfig{URL: srv.URL, Token: "tok"})
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
	r, logged := routerWith(t, "auto", "", WintermuteConfig{})
	if _, err := r.Ask(context.Background(), Request{Prompt: "q"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Ask error = %v, want ErrNotConfigured", err)
	}
	if len(*logged) != 0 {
		t.Errorf("usage logged for a request that never ran: %v", *logged)
	}
}

func TestRouterStatus(t *testing.T) {
	r, _ := routerWith(t, "auto", "sk-ant-key", WintermuteConfig{URL: "http://nas.local", Token: "tok"})
	st := r.Status()
	if st.Preference != "auto" || st.Active != NameWintermute {
		t.Errorf("status = %+v, want auto preferring wintermute", st)
	}
	if !st.ClaudeAvailable || !st.WintermuteAvailable {
		t.Errorf("status = %+v, want both providers reported available", st)
	}

	empty, _ := routerWith(t, "auto", "", WintermuteConfig{})
	if st := empty.Status(); st.Active != "" || st.Detail == "" {
		t.Errorf("status with nothing configured = %+v, want no active provider and an explanation", st)
	}
}

func TestClaudeProviderConfiguration(t *testing.T) {
	c := NewClaude(func() string { return "" }, "")
	if c.Available() {
		t.Error("Available = true without a key")
	}
	if _, err := c.Ask(context.Background(), Request{Prompt: "q"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Ask error = %v, want ErrNotConfigured", err)
	}
	if !strings.Contains(c.Describe(), "no API key") {
		t.Errorf("Describe = %q, want it to say the key is missing", c.Describe())
	}

	// The model defaults rather than being sent empty.
	withKey := NewClaude(func() string { return "sk-ant-key" }, "")
	if !strings.Contains(withKey.Describe(), DefaultClaudeModel) {
		t.Errorf("Describe = %q, want the default model", withKey.Describe())
	}
}
