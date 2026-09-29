package aiprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// claudeStub stands in for api.anthropic.com behind the pinned SDK: it records
// each Messages request body and answers with the given stop reason and text.
func claudeStub(t *testing.T, stopReason, text string, captured *map[string]any, modelLookups *atomic.Int32) *Claude {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/models/") {
			modelLookups.Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/v1/models/")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": id, "type": "model", "display_name": id, "created_at": "2026-01-01T00:00:00Z",
				"max_input_tokens": 1000000, "max_tokens": 128000,
				"capabilities": map[string]any{"structured_outputs": map[string]any{"supported": id != "claude-no-schema"}},
			})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*captured = body
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": body["model"], "stop_reason": stopReason,
			"content": []any{map[string]any{"type": "text", "text": text}},
			"usage":   map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	t.Cleanup(srv.Close)
	return NewClaude(func() string { return "sk-test" }, "claude-opus-5").WithBaseURL(srv.URL)
}

func TestClaudeSendsTheOutputSchemaAndReportsStopReasons(t *testing.T) {
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []any{"a"},
		"properties": map[string]any{"a": map[string]any{"type": "string"}}}
	var body map[string]any
	var lookups atomic.Int32

	c := claudeStub(t, "end_turn", `{"a":"x"}`, &body, &lookups)
	resp, err := c.Ask(context.Background(), Request{Prompt: "q", OutputSchema: schema})
	if err != nil {
		t.Fatal(err)
	}
	format, _ := body["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Fatalf("output_config.format = %v", body["output_config"])
	}
	if resp.StopReason != StopEndTurn || resp.Text != `{"a":"x"}` {
		t.Fatalf("response %+v", resp)
	}

	c = claudeStub(t, "end_turn", "plain", &body, &lookups)
	if _, err := c.Ask(context.Background(), Request{Prompt: "q"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["output_config"]; ok {
		t.Fatal("a request without a schema must not send output_config")
	}

	// An answer cut short is returned with its stop reason, not as an error,
	// so the caller can tell the person to narrow the request.
	c = claudeStub(t, "max_tokens", `{"a":"trunc`, &body, &lookups)
	if resp, err = c.Ask(context.Background(), Request{Prompt: "q", OutputSchema: schema}); err != nil || resp.StopReason != StopMaxTokens {
		t.Fatalf("max_tokens: %+v %v", resp, err)
	}
	c = claudeStub(t, "refusal", "", &body, &lookups)
	if resp, err = c.Ask(context.Background(), Request{Prompt: "q"}); err != nil || !resp.Refused || resp.StopReason != StopRefusal {
		t.Fatalf("refusal: %+v %v", resp, err)
	}
}

// Structured-output support comes from the Models API, once per model.
func TestClaudeStructuredOutputSupportIsLookedUpAndCached(t *testing.T) {
	var body map[string]any
	var lookups atomic.Int32
	c := claudeStub(t, "end_turn", "x", &body, &lookups)
	for i := 0; i < 3; i++ {
		ok, err := c.SupportsStructuredOutputs(context.Background(), "claude-opus-5")
		if err != nil || !ok {
			t.Fatalf("claude-opus-5: %v %v", ok, err)
		}
	}
	if ok, err := c.SupportsStructuredOutputs(context.Background(), "claude-no-schema"); err != nil || ok {
		t.Fatalf("a model that says no: %v %v", ok, err)
	}
	if n := lookups.Load(); n != 2 {
		t.Fatalf("%d lookups for two models, want 2", n)
	}
}
