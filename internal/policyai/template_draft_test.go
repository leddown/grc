package policyai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/policystudio"
)

func draftedAnswer(t *testing.T) string {
	return answerJSON(t, map[string]any{
		"title": "Access Review Standard", "doc_type": "standard", "frameworks": []any{"ISO 27001"},
		"description": "How access is reviewed.", "review_cadence_months": 12, "classification": "Internal",
		"facts": []any{
			map[string]any{"key": "review_period", "label": "Review period", "description": "How often", "example": "six months", "value_type": "duration"},
			map[string]any{"key": "unused_fact", "label": "Unused", "description": "", "example": "", "value_type": "text"},
		},
		"sections": []any{
			map[string]any{"heading": "Purpose", "kind": "purpose", "content": "Access rights must be reviewed every {{fact:review_period}} by the {{fact:system_owner_role}}.",
				"guidance": "Agree the period.", "source_passages": []any{2, 9}, "proposed_mappings": []any{}},
			map[string]any{"heading": "Scope", "kind": "scope", "content": "All systems that hold regulated data.", "guidance": "",
				"source_passages": []any{1}, "proposed_mappings": []any{}},
			map[string]any{"heading": "Requirements", "kind": "statements", "content": "- Reviews must be recorded.\n- Findings must be remediated.", "guidance": "",
				"source_passages": []any{}, "proposed_mappings": []any{
					map[string]any{"control_id": "ac-2", "coverage": "partial", "note": "account reviews"},
					map[string]any{"control_id": "XX-99", "coverage": "partial", "note": ""},
					map[string]any{"control_id": "AC-5", "coverage": "full", "note": ""},
				}},
		},
		"notes": []any{"Left out the sample's appendix of system names."},
	})
}

// Through Wintermute: the library passages go in delimited, the answer is
// checked (passages, controls, coverage, facts) with a note for each change,
// and the result is stored as a working copy with its source.
func TestDraftTemplateFromTheLibrary(t *testing.T) {
	f := newFixture(t, "wintermute")
	f.replies = []string{draftedAnswer(t)}
	var progress int
	tpl, err := f.engine.DraftTemplate(context.Background(), TemplateDraftRequest{LibraryDocumentID: 7, DocType: "standard"}, "alice", func(n int) { progress = n })
	if err != nil {
		t.Fatal(err)
	}
	prompt := f.prompts[len(f.prompts)-1]
	if !strings.Contains(prompt, "<<SAMPLE ") || !strings.Contains(prompt, "[S2] 2 Access reviews") || !strings.Contains(prompt, "Document type: standard.") {
		t.Fatalf("the prompt: %.600s", prompt)
	}
	if !strings.Contains(prompt, "Replace everything specific to that organisation") {
		t.Fatal("a Wintermute agent is not given the rules in the message")
	}
	if tpl.TemplateID != "access-review-standard" || tpl.Status != policystudio.TemplateDraft || tpl.Origin != policystudio.OriginLibrary ||
		tpl.SourceLibraryID != 7 || tpl.SourceTitle != "Old access policy.pdf" || tpl.AIModel != "qwen-policy" || tpl.OutputTokens != 250 {
		t.Fatalf("stored: %+v", tpl)
	}
	if len(tpl.Problems) != 0 {
		t.Fatalf("problems: %v", tpl.Problems)
	}
	if refs := tpl.Sources["purpose"]; len(refs) != 1 || refs[0].N != 2 || refs[0].Heading != "2 Access reviews" {
		t.Fatalf("sources: %+v", tpl.Sources)
	}
	notes := strings.Join(tpl.Notes, "\n")
	for _, want := range []string{"appendix of system names", "passage S9", "XX-99, which is not in the control catalog", "unused_fact was declared but never used", "{{fact:system_owner_role}}, which the answer did not declare"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	maps := tpl.Draft.Sections[2].ProposedMappings
	if len(maps) != 2 || maps[0].ControlID != "AC-2" || maps[1].ControlID != "AC-5" || maps[1].Coverage != "partial" {
		t.Fatalf("mappings: %+v", maps)
	}
	if progress == 0 || len(f.usage) != 1 {
		t.Fatalf("progress %d, usage %v", progress, f.usage)
	}
	if _, ok := f.studio.Template("access-review-standard"); ok {
		t.Fatal("a drafted template is offered before anyone published it")
	}
}

// Through Claude: the template contract goes out as the output schema, and
// Local only refuses the cloud before anything is read or sent.
func TestDraftTemplateThroughClaude(t *testing.T) {
	f := newFixture(t, "claude")
	if _, err := f.engine.DraftTemplate(context.Background(), TemplateDraftRequest{LibraryDocumentID: 7, LocalOnly: true}, "alice", nil); err == nil ||
		!strings.Contains(err.Error(), "Local only") || len(f.prompts) != 0 {
		t.Fatalf("local only through Claude: %v, %d prompts", err, len(f.prompts))
	}
	if st := f.engine.TemplateDraftStatus(context.Background(), true); st.Available {
		t.Fatal("status says a local-only draft can go to Claude")
	}
	if st := f.engine.TemplateDraftStatus(context.Background(), false); !st.Available || !strings.Contains(st.Destination, "Claude (cloud)") {
		t.Fatalf("status: %+v", st)
	}
	if _, err := f.engine.DraftTemplate(context.Background(), TemplateDraftRequest{LibraryDocumentID: 7, DocType: "memo"}, "alice", nil); err == nil {
		t.Fatal("an unknown document type was accepted")
	}
}

func TestDraftTemplateThroughClaudeStreams(t *testing.T) {
	f := newFixture(t, "claude-library")
	f.replies = []string{draftedAnswer(t)}
	chars := 0
	tpl, err := f.engine.DraftTemplate(context.Background(), TemplateDraftRequest{LibraryDocumentID: 7, Title: "Access reviews"}, "alice", func(n int) { chars = n })
	if err != nil {
		t.Fatal(err)
	}
	if f.schemas != 1 || f.streams != 1 || chars == 0 || !strings.HasPrefix(f.systems[0], "You write reusable templates") {
		t.Fatalf("schemas %d, streams %d, chars %d", f.schemas, f.streams, chars)
	}
	if tpl.Draft.Title != "Access reviews" || tpl.AIProvider != "claude" || tpl.AIModel != "claude-opus-5" || tpl.InputTokens != 1000 {
		t.Fatalf("stored: %+v", tpl)
	}
}

// Asked for a stream, the route reports progress and then the stored draft;
// a second malformed answer is an error event, after the one repair.
func TestDraftTemplateRouteStreams(t *testing.T) {
	f := newFixture(t, "wintermute")
	f.replies = []string{"Here is your template!", draftedAnswer(t)}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(f.engine, func(*gin.Context) string { return "alice" }).RegisterAdminRoutes(r)
	post := func() (*httptest.ResponseRecorder, []recordedEvent) {
		req := httptest.NewRequest(http.MethodPost, "/policies/app-templates/draft", strings.NewReader(`{"library_document_id":7}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec, readSSE(t, rec.Body.String())
	}
	rec, events := post()
	last := events[len(events)-1]
	if rec.Code != http.StatusOK || events[0].name != "progress" || last.name != "result" || last.data["template_id"] != "access-review-standard" || len(f.usage) != 2 {
		t.Fatalf("%d %v usage %v", rec.Code, events, f.usage)
	}
	f.replies = []string{"still prose", "still prose"}
	_, events = post()
	if last := events[len(events)-1]; last.name != "error" || !strings.Contains(last.data["error"].(string), "even after asking again") {
		t.Fatalf("a malformed answer: %v", events)
	}

	// A provider that answers whole is silent while it works; the heartbeat
	// keeps the stream moving so a proxy does not close it.
	defer func(d time.Duration) { templateHeartbeat = d }(templateHeartbeat)
	templateHeartbeat = 20 * time.Millisecond
	f.delay = 300 * time.Millisecond
	f.replies = []string{draftedAnswer(t)}
	_, events = post()
	beats := 0
	for _, e := range events {
		if e.name == "progress" {
			beats++
		}
	}
	if beats < 5 || events[len(events)-1].name != "result" {
		t.Fatalf("%d progress events while a slow provider worked: %v", beats, events)
	}
}
