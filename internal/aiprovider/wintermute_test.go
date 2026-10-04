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
	// seenSession records the session-create payload for assertions, and
	// sessionCreates how many times one was opened.
	seenSession    map[string]any
	sessionCreates int
	seenText       string
	// failWith, when non-zero, makes every authenticated call fail.
	failWith int
	// pending is what a turn reports as awaiting this client, and seenResults
	// records what was posted back to close them out.
	pending     []any
	seenResults []any
	// seenMessages records every message posted, whole, and turn, when set,
	// is returned in place of the turn built from the fields above.
	seenMessages []map[string]any
	turn         map[string]any
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
		s.sessionCreates++
		writeJSON(w, map[string]any{"id": "sess-123"})
	}))

	mux.HandleFunc("/api/v1/sessions/sess-123/messages", authed(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.seenText, _ = body["text"].(string)
		s.seenMessages = append(s.seenMessages, body)
		if s.turn != nil {
			writeJSON(w, s.turn)
			return
		}
		status := s.status
		if status == "" {
			status = "complete"
		}
		writeJSON(w, map[string]any{
			"reply":         s.reply,
			"status":        status,
			"backend":       s.backend,
			"model":         s.model,
			"pending_calls": s.pending,
			"usage":         map[string]any{"input_tokens": float64(11), "output_tokens": float64(22)},
		})
	}))

	mux.HandleFunc("/api/v1/sessions/sess-123/tool_results", authed(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.seenResults, _ = body["results"].([]any)
		writeJSON(w, map[string]any{"status": "complete", "reply": "closed"})
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

// A caller that names an agent opens its session on that agent; one that names
// none gets the configured agent.
func TestWintermuteAskOpensTheSessionOnTheRequestedAgent(t *testing.T) {
	for _, tc := range []struct{ name, requested, want string }{
		{"configured", "", "grc"},
		{"requested", " crisis-exercises ", "crisis-exercises"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubWintermute{token: "tok", reply: "ok"}
			srv := stub.server(t)
			w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok", Agent: "grc"})
			if _, err := w.Ask(context.Background(), Request{Prompt: "hello", Agent: tc.requested}); err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if got := stub.seenSession["agent"]; got != tc.want {
				t.Errorf("session agent = %v, want %q", got, tc.want)
			}
		})
	}
}

// A conversation continues in the session the last answer came from: the
// server holds the transcript, so nothing is resent and no session is opened.
func TestWintermuteAskResumesSession(t *testing.T) {
	stub := &stubWintermute{token: "tok", reply: "still me"}
	srv := stub.server(t)
	w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok"})

	first, err := w.Ask(context.Background(), Request{Prompt: "who are you?"})
	if err != nil {
		t.Fatalf("first Ask: %v", err)
	}
	if first.SessionID != "sess-123" {
		t.Fatalf("SessionID = %q, want the server's session id", first.SessionID)
	}

	resp, err := w.Ask(context.Background(), Request{
		Prompt:    "and again?",
		SessionID: first.SessionID,
		History: []Message{
			{Role: RoleUser, Text: "who are you?"},
			{Role: RoleAssistant, Text: "still me"},
		},
	})
	if err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	if resp.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want it carried through", resp.SessionID)
	}
	if stub.sessionCreates != 1 {
		t.Errorf("opened %d sessions, want 1 — the second turn must reuse the first", stub.sessionCreates)
	}
	if stub.seenText != "and again?" {
		t.Errorf("posted text = %q, want the question alone: the server already has the transcript", stub.seenText)
	}
}

// A transcript with no session id belongs to a conversation this server has
// never seen, so it is folded into the message text instead.
func TestWintermuteAskFoldsHistoryWithoutSession(t *testing.T) {
	stub := &stubWintermute{token: "tok", reply: "sure"}
	srv := stub.server(t)
	w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok"})

	if _, err := w.Ask(context.Background(), Request{
		System: "be terse",
		Prompt: "and the second?",
		History: []Message{
			{Role: RoleUser, Text: "name a control family"},
			{Role: RoleAssistant, Text: "AC — Access Control"},
			{Role: RoleUser, Text: "  "},
		},
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	for _, want := range []string{"be terse", "User: name a control family", "Assistant: AC — Access Control", "and the second?"} {
		if !strings.Contains(stub.seenText, want) {
			t.Errorf("posted text %q is missing %q", stub.seenText, want)
		}
	}
	if !strings.HasPrefix(stub.seenText, "be terse") {
		t.Errorf("posted text = %q, want the system prompt first", stub.seenText)
	}
	if strings.Index(stub.seenText, "name a control family") > strings.Index(stub.seenText, "and the second?") {
		t.Error("the transcript must come before the new question")
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

// This app declares no client tools, so a turn should never come back waiting
// on one. If it ever does, the calls must be refused rather than left hanging:
// sessions here are resumable, and an unanswered call on the server is replayed
// on every later turn until something answers it.
func TestWintermuteRefusesPendingCallsBeforeGivingUp(t *testing.T) {
	stub := &stubWintermute{
		token:  "tok",
		status: "awaiting_client",
		pending: []any{
			map[string]any{"id": "call_1", "name": "rename_file"},
		},
	}
	srv := stub.server(t)
	w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok"})

	if _, err := w.Ask(context.Background(), Request{Prompt: "q"}); err == nil {
		t.Fatal("Ask() succeeded on a turn awaiting client tools")
	}

	if len(stub.seenResults) != 1 {
		t.Fatalf("posted %d refusals for 1 pending call: %+v", len(stub.seenResults), stub.seenResults)
	}
	result, _ := stub.seenResults[0].(map[string]any)
	if result["call_id"] != "call_1" {
		t.Errorf("refusal named call %v, want call_1", result["call_id"])
	}
	if result["is_error"] != true {
		t.Errorf("refusal was not marked as an error: %+v", result)
	}
}

// A turn that completes with nothing to say is a failed turn, not an answer.
// The same rule applies in morpheus and in wintermute's own harness.
func TestWintermuteEmptyReplyPostsNothing(t *testing.T) {
	stub := &stubWintermute{token: "tok", status: "complete", reply: ""}
	srv := stub.server(t)
	w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok"})

	_, err := w.Ask(context.Background(), Request{Prompt: "q"})
	if !errors.Is(err, errNoAnswer) {
		t.Fatalf("Ask() error = %v, want errNoAnswer", err)
	}
	if stub.seenResults != nil {
		t.Errorf("posted results for a turn with no pending calls: %+v", stub.seenResults)
	}
}

// The server measures the model's context window and says when it stopped an
// answer short; both are carried to the caller, and the turn's cost is read in
// the spelling the server uses. ClearReads is named on a resumed session only,
// and only when asked: a server that predates the field refuses one carrying it.
func TestWintermuteWindowMeasureAndClearReads(t *testing.T) {
	stub := &stubWintermute{token: "tok", turn: map[string]any{
		"status": "complete", "reply": "AC-2 lands mostly on Art.", "backend": "core", "model": "qwen",
		"usage":   map[string]any{"prompt_tokens": float64(6478), "completion_tokens": float64(1714)},
		"room":    map[string]any{"used": float64(9343), "window": float64(32768), "budget": float64(21846), "reads": float64(2574)},
		"cut_off": "This answer was cut off: qwen on core was sent 6478 tokens and was stopped after writing 1714 more.",
	}}
	srv := stub.server(t)
	w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok"})

	first, err := w.Ask(context.Background(), Request{Prompt: "map AC-2 to DORA", ClearReads: true})
	if err != nil {
		t.Fatalf("first Ask: %v", err)
	}
	if first.Room == nil || *first.Room != (Room{Used: 9343, Window: 32768, Budget: 21846, Reads: 2574}) {
		t.Errorf("Room = %+v", first.Room)
	}
	if first.Room.Full() {
		t.Errorf("a conversation under its budget reads as full")
	}
	if first.StopReason != StopMaxTokens || !strings.Contains(first.CutOff, "cut off") {
		t.Errorf("a cut-off answer came back as stop %q, cut off %q", first.StopReason, first.CutOff)
	}
	if first.Usage != (Usage{InputTokens: 6478, OutputTokens: 1714}) {
		t.Errorf("Usage = %+v, want the server's prompt and completion counts", first.Usage)
	}

	if _, err := w.Ask(context.Background(), Request{Prompt: "and AC-3?", SessionID: first.SessionID, ClearReads: true}); err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	if _, err := w.Ask(context.Background(), Request{Prompt: "and AC-4?", SessionID: first.SessionID}); err != nil {
		t.Fatalf("third Ask: %v", err)
	}
	if len(stub.seenMessages) != 3 {
		t.Fatalf("%d messages posted, want 3", len(stub.seenMessages))
	}
	for i, want := range []bool{false, true, false} {
		if _, named := stub.seenMessages[i]["clear_reads"]; named != want {
			t.Errorf("message %d names clear_reads: %v, want %v", i+1, named, want)
		}
	}

	full := Room{Used: 22000, Window: 32768, Budget: 21846}
	if !full.Full() {
		t.Errorf("a conversation past its budget does not read as full")
	}
}

// A turn the server could not answer comes back as a sentence saying which
// limit was reached. It is shown whole and as words, not as JSON cut at the
// point where it says what to change.
func TestWintermuteErrorIsTheServersOwnWords(t *testing.T) {
	said := "The model ran out of room before it answered: qwen3.8-27b-q8_0 on core was sent 32618 tokens and was " +
		"stopped after writing 150 more, reasoning included: its context window was full. Nothing of an answer had " +
		"been written. Nearly all of that room went to the request: what the turn had read left none to answer in. " +
		"Raise the context size where the model is served, ask a narrower question, or start a new conversation if " +
		"this one has grown long."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		writeJSON(w, map[string]any{"id": "sess-123", "error": said})
	}))
	t.Cleanup(srv.Close)
	w := newWintermute(WintermuteConfig{URL: srv.URL, Token: "tok"})

	_, err := w.Ask(context.Background(), Request{Prompt: "map AC-2 to DORA"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 422: "+said) {
		t.Errorf("err = %v", err)
	}
}
