package aiprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubWintermute stands in for a wintermuted server: it answers /api/v1/me for
// discovery, opens a session, and replies to the posted message.
type stubWintermute struct {
	token string
	// backends is what /api/v1/me advertises.
	backends []string
	// reply is what a turn returns. status controls the turn's state.
	reply   string
	status  string
	backend string
	model   string
	// seenSession records the session-create payload for assertions.
	seenSession map[string]any
	seenText    string
	// failWith, when non-zero, makes every authenticated call fail.
	failWith int
}

func (s *stubWintermute) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+s.token {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"bad token"}`))
				return
			}
			if s.failWith != 0 {
				w.WriteHeader(s.failWith)
				_, _ = w.Write([]byte(`{"error":"backend unavailable"}`))
				return
			}
			h(w, r)
		}
	}

	mux.HandleFunc("/api/v1/me", authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"name":            "grc",
			"kind":            "service",
			"backends":        toAny(s.backends),
			"default_backend": "local-8b",
			"fallback":        "claude",
		})
	}))

	mux.HandleFunc("/api/v1/sessions", authed(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&s.seenSession)
		writeJSON(w, map[string]any{"id": "sess-123"})
	}))

	mux.HandleFunc("/api/v1/sessions/sess-123/messages", authed(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.seenText, _ = body["text"].(string)
		status := s.status
		if status == "" {
			status = "complete"
		}
		writeJSON(w, map[string]any{
			"reply":   s.reply,
			"status":  status,
			"backend": s.backend,
			"model":   s.model,
			"usage":   map[string]any{"input_tokens": float64(11), "output_tokens": float64(22)},
		})
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func toAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

func newWintermute(cfg WintermuteConfig) *Wintermute {
	return NewWintermute(func() WintermuteConfig { return cfg })
}

func TestWintermuteAsk(t *testing.T) {
	stub := &stubWintermute{
		token:   "tok",
		reply:   "  the answer  ",
		backend: "local-8b",
		model:   "llama-3.1-8b",
	}
	srv := stub.server(t)

	w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok", Backend: "local-8b", Model: "llama-3.1-8b"})
	if !w.Available() {
		t.Fatal("Available = false with URL and token set")
	}

	resp, err := w.Ask(context.Background(), Request{System: "be terse", Prompt: "why?"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Text != "the answer" {
		t.Errorf("Text = %q, want the trimmed answer", resp.Text)
	}
	if resp.Provider != NameWintermute {
		t.Errorf("Provider = %q, want %q", resp.Provider, NameWintermute)
	}
	// What actually served the turn is reported, which is not always what was
	// asked for when a backend fails and the server retries its fallback.
	if resp.Backend != "local-8b" || resp.Model != "llama-3.1-8b" {
		t.Errorf("served by %q/%q, want local-8b/llama-3.1-8b", resp.Backend, resp.Model)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 22 {
		t.Errorf("usage = %+v, want 11/22", resp.Usage)
	}

	// The backend and model pin must reach the session-create call.
	if got := stub.seenSession["backend"]; got != "local-8b" {
		t.Errorf("session backend = %v, want local-8b", got)
	}
	// wintermuted takes message text only, so the system prompt is folded in.
	if !strings.HasPrefix(stub.seenText, "be terse") || !strings.Contains(stub.seenText, "why?") {
		t.Errorf("posted text = %q, want the system prompt prepended to the question", stub.seenText)
	}
}

func TestWintermuteAskFailures(t *testing.T) {
	tests := []struct {
		name    string
		stub    *stubWintermute
		token   string
		wantErr string
	}{
		{
			name:    "bad token",
			stub:    &stubWintermute{token: "right", reply: "hi"},
			token:   "wrong",
			wantErr: "rejected the client token",
		},
		{
			name:    "server error",
			stub:    &stubWintermute{token: "tok", failWith: http.StatusBadGateway},
			token:   "tok",
			wantErr: "HTTP 502",
		},
		{
			name:    "turn ended waiting on tools",
			stub:    &stubWintermute{token: "tok", reply: "", status: "awaiting_tool_results"},
			token:   "tok",
			wantErr: "awaiting_tool_results",
		},
		{
			name:    "empty reply on a complete turn",
			stub:    &stubWintermute{token: "tok", reply: "", status: "complete"},
			token:   "tok",
			wantErr: "empty answer",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := tc.stub.server(t)
			w := newWintermute(WintermuteConfig{URL: srv.URL, Token: tc.token})
			_, err := w.Ask(context.Background(), Request{Prompt: "q"})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Ask error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestWintermuteNotConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  WintermuteConfig
	}{
		{"no url", WintermuteConfig{Token: "tok"}},
		{"no token", WintermuteConfig{URL: "http://localhost:9000"}},
		{"neither", WintermuteConfig{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWintermute(tc.cfg)
			if w.Available() {
				t.Error("Available = true without full configuration")
			}
			if _, err := w.Ask(context.Background(), Request{Prompt: "q"}); err == nil {
				t.Error("Ask succeeded without configuration")
			}
		})
	}
}

func TestWintermuteProbe(t *testing.T) {
	t.Run("reports the advertised backends", func(t *testing.T) {
		stub := &stubWintermute{token: "tok", backends: []string{"local-8b", "big-70b", "claude"}}
		srv := stub.server(t)
		w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok"})

		probe := w.Probe(context.Background())
		if !probe.OK {
			t.Fatalf("Probe not OK: %s", probe.Detail)
		}
		if len(probe.Backends) != 3 {
			t.Errorf("backends = %v, want three", probe.Backends)
		}
		if probe.DefaultBackend != "local-8b" || probe.Fallback != "claude" {
			t.Errorf("defaults = %q/%q, want local-8b/claude", probe.DefaultBackend, probe.Fallback)
		}
	})

	// A backend pinned in Settings that the server does not have would
	// otherwise fail later with a much less obvious message.
	t.Run("flags a pinned backend the server does not have", func(t *testing.T) {
		stub := &stubWintermute{token: "tok", backends: []string{"local-8b"}}
		srv := stub.server(t)
		w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok", Backend: "typo-backend"})

		probe := w.Probe(context.Background())
		if probe.OK {
			t.Error("Probe reported OK despite an unknown pinned backend")
		}
		if !strings.Contains(probe.Detail, "typo-backend") || !strings.Contains(probe.Detail, "local-8b") {
			t.Errorf("detail %q should name the bad backend and the available ones", probe.Detail)
		}
	})

	t.Run("reports a bad token", func(t *testing.T) {
		stub := &stubWintermute{token: "right"}
		srv := stub.server(t)
		w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "wrong"})
		if probe := w.Probe(context.Background()); probe.OK {
			t.Error("Probe reported OK with a bad token")
		}
	})

	t.Run("reports missing configuration without a request", func(t *testing.T) {
		if probe := newWintermute(WintermuteConfig{}).Probe(context.Background()); probe.OK {
			t.Error("Probe reported OK with nothing configured")
		}
	})
}

func TestValidateEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "private ip over http", in: "http://192.168.1.50:8080", want: "http://192.168.1.50:8080"},
		{name: "loopback over http", in: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "localhost by name over http", in: "http://localhost:8080", want: "http://localhost:8080"},
		{name: "trailing slash trimmed", in: "https://w.example.com/", want: "https://w.example.com"},
		{name: "path preserved", in: "https://w.example.com/wintermute/", want: "https://w.example.com/wintermute"},
		{name: "empty", in: "  ", wantErr: "required"},
		{name: "no scheme", in: "nas.local:8080", wantErr: "must be http or https"},
		{name: "wrong scheme", in: "ftp://nas.local", wantErr: "must be http or https"},
		{name: "no host", in: "http://", wantErr: "no host"},
		// The client token rides on every request, so plaintext off the local
		// network is refused.
		{name: "http to a public host", in: "http://wintermute.example.com", wantErr: "must use https unless"},
		// A hostname is not resolved to decide this: that would be a TOCTOU
		// race and a lookup of an attacker-chosen name.
		{name: "http to a hostname is not resolved", in: "http://nas.local:8080", wantErr: "must use https unless"},
		{name: "http to a public ip", in: "http://8.8.8.8", wantErr: "must use https unless"},
		// Credentials in the URL would be sent on every request and logged.
		{name: "embedded credentials", in: "https://user:pass@w.example.com", wantErr: "must not contain credentials"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateEndpoint(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateEndpoint: %v", err)
			}
			if got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}
