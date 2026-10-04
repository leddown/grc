package policyai

import (
	"bufio"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Fed in pieces of every size, including ones that split an escape or a
// surrogate pair, the reader hands on exactly answer_markdown's text.
func TestAnswerReaderDecodesAcrossPieces(t *testing.T) {
	want := "Line one\nwith \"quotes\", a \\ backslash, tab\t, é, 😀 and   — done."
	raw, _ := json.Marshal(map[string]any{"answer_markdown": want})
	answer := strings.Replace(string(raw), "}", `,"proposal":{"summary":"\"answer_markdown\": \"no\""}}`, 1)
	// Encoded as a model may write it: some characters as \u escapes.
	answer = strings.Replace(answer, "😀", `😀`, 1)
	answer = strings.Replace(answer, "é", `é`, 1)
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 200; trial++ {
		var got strings.Builder
		r := &answerReader{out: func(s string) { got.WriteString(s) }}
		for rest := answer; rest != ""; {
			n := 1 + rng.Intn(9)
			if n > len(rest) {
				n = len(rest)
			}
			r.write(rest[:n])
			rest = rest[n:]
		}
		if got.String() != want {
			t.Fatalf("trial %d: got %q", trial, got.String())
		}
	}
}

func TestAnswerReaderPreviewsOnlyTheContract(t *testing.T) {
	for _, answer := range []string{
		"Sure! Here is the JSON: {\"answer_markdown\":\"x\"}",
		`{"proposal":null,"answer_markdown":"x"}`,
		"```json\n{\"answer_markdown\":\"x\"}",
	} {
		var got strings.Builder
		r := &answerReader{out: func(s string) { got.WriteString(s) }}
		r.write(answer)
		if got.Len() != 0 {
			t.Errorf("%q previewed %q", answer, got.String())
		}
	}
}

type recordedEvent struct {
	name string
	data map[string]any
}

func readSSE(t *testing.T, body string) []recordedEvent {
	t.Helper()
	var out []recordedEvent
	sc := bufio.NewScanner(strings.NewReader(body))
	var name string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
				t.Fatalf("event %s: %v", name, err)
			}
			out = append(out, recordedEvent{name, data})
		}
	}
	return out
}

func (f *fixture) streamPropose(t *testing.T, body string) (*httptest.ResponseRecorder, []recordedEvent) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(f.engine, func(*gin.Context) string { return "alice" }).RegisterAdminRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/policies/"+strconv.FormatInt(f.doc.ID, 10)+"/studio/ai/proposals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec, readSSE(t, rec.Body.String())
}

// A proposal asked for as a stream arrives as the answer's text and then the
// validated result; the model call is logged once, and the preview never
// carries the edits. Wintermute answers whole, so the text is one piece.
func TestProposeStreams(t *testing.T) {
	f := newFixture(t)
	bid := f.blockOf("review access every quarter")
	f.replies = []string{answerJSON(t, map[string]any{
		"answer_markdown": "Made the review testable: a named owner, a quarter, and evidence.",
		"proposal": map[string]any{"summary": "testable", "control_mappings": []any{}, "new_facts": []any{},
			"edits": []any{edit("replace", bid, "every quarter", "every quarter, recorded by the system owner", "testable")}},
	})}
	rec, events := f.streamPropose(t, `{"action":"testable","scope":"document"}`)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	var text strings.Builder
	answers := 0
	for _, e := range events[:len(events)-1] {
		if e.name != "answer" {
			t.Fatalf("unexpected event before the result: %s", e.name)
		}
		answers++
		text.WriteString(e.data["text"].(string))
	}
	if answers < 1 || text.String() != "Made the review testable: a named owner, a quarter, and evidence." {
		t.Fatalf("%d pieces: %q", answers, text.String())
	}
	if strings.Contains(rec.Body.String()[:strings.LastIndex(rec.Body.String(), "event: result")], "recorded by the system owner") {
		t.Fatal("an edit was streamed before it was validated")
	}
	last := events[len(events)-1]
	if last.name != "result" || last.data["has_proposal"] != true || last.data["id"] == nil {
		t.Fatalf("result: %v", last)
	}
	if len(f.usage) != 1 {
		t.Fatalf("usage %v", f.usage)
	}
}

// An answer that needs the repair turn voids the preview first; a request the
// engine refuses before asking is an ordinary JSON error.
func TestStreamRestartsAndRefusesPlainly(t *testing.T) {
	f := newFixture(t)
	good := `{"answer_markdown":"Done.","proposal":null}`
	f.replies = []string{`{"answer_markdown":"Half an answer","proposal":`, good}
	_, events := f.streamPropose(t, `{"action":"review","scope":"document"}`)
	var names []string
	for _, e := range events {
		names = append(names, e.name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "answer,restart,answer") || !strings.HasSuffix(joined, "result") || len(f.usage) != 2 {
		t.Fatalf("events %s, usage %v", joined, f.usage)
	}

	rec, _ := f.streamPropose(t, `{"action":"no-such-action"}`)
	if rec.Code != http.StatusBadRequest || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("a refused request: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	f.stop = "refusal"
	f.replies = []string{`{"answer_markdown":"I can start","proposal":null}`}
	rec, events = f.streamPropose(t, `{"action":"review","scope":"document"}`)
	if last := events[len(events)-1]; rec.Code != http.StatusOK || last.name != "error" || !strings.Contains(last.data["error"].(string), "declined") {
		t.Fatalf("a refusal mid-stream: %d %v", rec.Code, events)
	}
}
