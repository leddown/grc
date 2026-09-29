package aiprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// claudeStreamStub answers a streamed Messages request with the given pieces.
func claudeStreamStub(t *testing.T, pieces []string, stop string, sawStream *bool) *Claude {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*sawStream = body["stream"] == true
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(name string, v map[string]any) {
			v["type"] = name
			raw, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
		}
		event("message_start", map[string]any{"message": map[string]any{"id": "m", "type": "message", "role": "assistant", "model": body["model"],
			"content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 0}}})
		event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		for _, p := range pieces {
			event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": p}})
		}
		event("content_block_stop", map[string]any{"index": 0})
		event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 34}})
		event("message_stop", map[string]any{})
	}))
	t.Cleanup(srv.Close)
	return NewClaude(func() string { return "sk-test" }, "claude-opus-5").WithBaseURL(srv.URL)
}

func TestClaudeAskStreamHandsOverThePieces(t *testing.T) {
	var streamed bool
	c := claudeStreamStub(t, []string{"Access is ", "reviewed ", "quarterly."}, "end_turn", &streamed)
	var got []string
	resp, err := c.AskStream(context.Background(), Request{Prompt: "q"}, func(s string) { got = append(got, s) })
	if err != nil {
		t.Fatal(err)
	}
	if !streamed || strings.Join(got, "|") != "Access is |reviewed |quarterly." {
		t.Fatalf("stream %v, pieces %q", streamed, got)
	}
	if resp.Text != "Access is reviewed quarterly." || resp.StopReason != StopEndTurn || resp.Usage != (Usage{InputTokens: 12, OutputTokens: 34}) || resp.Model != "claude-opus-5" {
		t.Fatalf("response %+v", resp)
	}

	c = claudeStreamStub(t, []string{"I will not"}, "refusal", &streamed)
	resp, err = c.AskStream(context.Background(), Request{Prompt: "q"}, func(string) {})
	if err != nil || !resp.Refused {
		t.Fatalf("a refusal: %+v %v", resp, err)
	}
}

// A provider that cannot stream hands the whole answer over once, and the
// router logs the call once either way.
func TestRouterStreamsOrFallsBack(t *testing.T) {
	var streamed bool
	c := claudeStreamStub(t, []string{"a", "b"}, "end_turn", &streamed)
	var logged int
	r := NewRouter(c, nil, func() string { return preferClaude }, func(string, string, int, int) { logged++ })
	var pieces int
	if _, err := r.AskStream(context.Background(), Request{Prompt: "q"}, func(string) { pieces++ }); err != nil || pieces != 2 || logged != 1 {
		t.Fatalf("pieces %d, logged %d, err %v", pieces, logged, err)
	}

	var whole []string
	resp, err := AskStream(context.Background(), wholeProvider{}, Request{}, func(s string) { whole = append(whole, s) })
	if err != nil || len(whole) != 1 || whole[0] != resp.Text {
		t.Fatalf("fallback: %q %v", whole, err)
	}
}

type wholeProvider struct{}

func (wholeProvider) Name() string     { return "whole" }
func (wholeProvider) Available() bool  { return true }
func (wholeProvider) Describe() string { return "whole" }
func (wholeProvider) Ask(context.Context, Request) (Response, error) {
	return Response{Text: "the whole answer"}, nil
}
