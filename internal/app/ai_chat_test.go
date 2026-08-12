package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidatedClaudeEndpoint(t *testing.T) {
	got, err := validatedClaudeEndpoint("")
	if err != nil {
		t.Fatalf("validatedClaudeEndpoint default error: %v", err)
	}
	if got != claudeEndpointURL {
		t.Fatalf("validatedClaudeEndpoint default=%q want=%q", got, claudeEndpointURL)
	}

	got, err = validatedClaudeEndpoint("https://api.anthropic.com/v1/messages")
	if err != nil {
		t.Fatalf("validatedClaudeEndpoint official error: %v", err)
	}
	if got != claudeEndpointURL {
		t.Fatalf("validatedClaudeEndpoint official=%q want=%q", got, claudeEndpointURL)
	}
}

func TestValidatedClaudeEndpointRejectsUnsafeURLs(t *testing.T) {
	tests := []string{
		"http://api.anthropic.com/v1/messages",
		"https://evil.example.com/v1/messages",
		"https://user:pass@api.anthropic.com/v1/messages",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := validatedClaudeEndpoint(input); err == nil {
				t.Fatalf("validatedClaudeEndpoint(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestValidatedWintermuteEndpointAcceptsPrivateAndTLSHosts(t *testing.T) {
	tests := map[string]string{
		"http://127.0.0.1:8080":          "http://127.0.0.1:8080",
		"http://localhost:8080/":         "http://localhost:8080",
		"http://192.168.1.20:8080":       "http://192.168.1.20:8080",
		"http://10.0.0.5:8080":           "http://10.0.0.5:8080",
		"http://[::1]:8080":              "http://[::1]:8080",
		"https://wintermute.example.com": "https://wintermute.example.com",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := validatedWintermuteEndpoint(input)
			if err != nil {
				t.Fatalf("validatedWintermuteEndpoint(%q) error: %v", input, err)
			}
			if got != want {
				t.Fatalf("validatedWintermuteEndpoint(%q)=%q want=%q", input, got, want)
			}
		})
	}
}

func TestValidatedWintermuteEndpointRejectsUnsafeURLs(t *testing.T) {
	tests := []string{
		// Cleartext to a public host would make this handler a proxy for
		// arbitrary internet traffic.
		"http://wintermute.example.com",
		"http://8.8.8.8:8080",
		// A hostname could resolve anywhere, so only "localhost" is trusted.
		"http://internal-host:8080",
		"https://user:pass@wintermute.example.com",
		"ftp://127.0.0.1:8080",
		"/api/v1/sessions",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := validatedWintermuteEndpoint(input); err == nil {
				t.Fatalf("validatedWintermuteEndpoint(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestExtractWintermuteUsage(t *testing.T) {
	usage := extractWintermuteUsage(map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(120),
			"completion_tokens": float64(45),
			"total_tokens":      float64(165),
		},
	})
	if usage.InputTokens != 120 || usage.OutputTokens != 45 {
		t.Fatalf("extractWintermuteUsage=%+v want in=120 out=45", usage)
	}

	if got := extractWintermuteUsage(map[string]any{}); got != (aiChatTokenUsage{}) {
		t.Fatalf("extractWintermuteUsage(no usage)=%+v want zero", got)
	}
}

// stubWintermute stands in for a wintermuted server: it accepts a session
// create and one message post, recording what it was sent.
type stubWintermute struct {
	sessionBody map[string]any
	messageBody map[string]any
	messagePath string
	authHeader  string
	turn        map[string]any
}

func (s *stubWintermute) start(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.authHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)

		switch {
		case r.URL.Path == "/api/v1/sessions":
			_ = json.Unmarshal(body, &s.sessionBody)
			writeStubJSON(w, map[string]any{"id": "sess-1", "title": "x"})
		case strings.HasSuffix(r.URL.Path, "/messages"):
			s.messagePath = r.URL.Path
			_ = json.Unmarshal(body, &s.messageBody)
			writeStubJSON(w, s.turn)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeStubJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func TestAskWintermute(t *testing.T) {
	stub := &stubWintermute{turn: map[string]any{
		"session_id": "sess-1",
		"status":     "complete",
		"reply":      "  the answer  ",
		"backend":    "workstation",
		"model":      "qwen3-30b",
		"usage":      map[string]any{"prompt_tokens": 200, "completion_tokens": 30},
	}}
	srv := stub.start(t)

	got, err := askWintermute(aiChatRequest{
		Provider: "wintermute",
		Question: "which control covers this?",
		System:   "You are a controls assistant.",
		APIKey:   "wm_token",
		Endpoint: srv.URL,
		Backend:  "workstation",
		Model:    "qwen3-30b",
	})
	if err != nil {
		t.Fatalf("askWintermute: %v", err)
	}

	if got.Answer != "the answer" {
		t.Fatalf("Answer=%q want %q", got.Answer, "the answer")
	}
	if got.Backend != "workstation" || got.Model != "qwen3-30b" {
		t.Fatalf("served-by = %q/%q want workstation/qwen3-30b", got.Backend, got.Model)
	}
	if got.Usage.InputTokens != 200 || got.Usage.OutputTokens != 30 {
		t.Fatalf("usage=%+v want in=200 out=30", got.Usage)
	}

	if stub.authHeader != "Bearer wm_token" {
		t.Fatalf("Authorization=%q want %q", stub.authHeader, "Bearer wm_token")
	}
	if stub.sessionBody["backend"] != "workstation" || stub.sessionBody["model"] != "qwen3-30b" {
		t.Fatalf("session create body = %v", stub.sessionBody)
	}
	if stub.messagePath != "/api/v1/sessions/sess-1/messages" {
		t.Fatalf("message path = %q", stub.messagePath)
	}
	// wintermuted takes message text only and derives its own system prompt,
	// so the system prompt has to ride along in the question.
	text, _ := stub.messageBody["text"].(string)
	if !strings.HasPrefix(text, "You are a controls assistant.") ||
		!strings.HasSuffix(text, "which control covers this?") {
		t.Fatalf("message text = %q", text)
	}
}

// A turn that stops for client-side tools has no reply. This app declares no
// client tools, so that is a failure to report rather than an empty answer.
func TestAskWintermuteAwaitingClientIsAnError(t *testing.T) {
	stub := &stubWintermute{turn: map[string]any{
		"session_id": "sess-1",
		"status":     "awaiting_client",
		"pending_calls": []any{
			map[string]any{"id": "call-1", "name": "list_directory"},
		},
	}}
	srv := stub.start(t)

	_, err := askWintermute(aiChatRequest{
		Question: "rename my files",
		APIKey:   "wm_token",
		Endpoint: srv.URL,
	})
	if err == nil {
		t.Fatal("expected an error for a turn awaiting client tools")
	}
	if !strings.Contains(err.Error(), "awaiting_client") {
		t.Fatalf("error = %v, want it to name the status", err)
	}
}

func TestAskWintermuteRequiresEndpointAndToken(t *testing.T) {
	t.Setenv("WINTERMUTE_URL", "")
	t.Setenv("WINTERMUTE_TOKEN", "")

	if _, err := askWintermute(aiChatRequest{Question: "hi", APIKey: "wm_token"}); err == nil {
		t.Fatal("expected an error with no endpoint")
	}
	if _, err := askWintermute(aiChatRequest{Question: "hi", Endpoint: "http://127.0.0.1:9"}); err == nil {
		t.Fatal("expected an error with no token")
	}
}

// A blank field falls back to the server-side configuration, so an operator can
// set the server up once and leave the UI fields empty.
func TestAskWintermuteFallsBackToEnvironment(t *testing.T) {
	stub := &stubWintermute{turn: map[string]any{
		"status": "complete",
		"reply":  "from env",
	}}
	srv := stub.start(t)

	t.Setenv("WINTERMUTE_URL", srv.URL)
	t.Setenv("WINTERMUTE_TOKEN", "env-token")
	t.Setenv("WINTERMUTE_BACKEND", "env-backend")

	got, err := askWintermute(aiChatRequest{Question: "hi"})
	if err != nil {
		t.Fatalf("askWintermute: %v", err)
	}
	if got.Answer != "from env" {
		t.Fatalf("Answer=%q", got.Answer)
	}
	if stub.authHeader != "Bearer env-token" {
		t.Fatalf("Authorization=%q want the environment token", stub.authHeader)
	}
	if stub.sessionBody["backend"] != "env-backend" {
		t.Fatalf("session create body = %v", stub.sessionBody)
	}
}
