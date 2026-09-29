package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"grc/internal/aiprovider"
	"grc/internal/authn"
	"grc/internal/policydocs"
)

// slowClaude stands in for api.anthropic.com and streams its answer in pieces
// with a pause between them, so a test can see the answer arrive.
type slowClaude struct {
	mu     sync.Mutex
	calls  int
	usage  int
	pieces []string
	pause  time.Duration
}

func (s *slowClaude) router(t *testing.T) *aiprovider.Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/models/") {
			id := strings.TrimPrefix(r.URL.Path, "/v1/models/")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "type": "model", "display_name": id, "created_at": "2026-01-01T00:00:00Z",
				"max_input_tokens": 1000000, "max_tokens": 128000, "capabilities": map[string]any{"structured_outputs": map[string]any{"supported": true}}})
			return
		}
		s.mu.Lock()
		s.calls++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(name string, v map[string]any) {
			v["type"] = name
			raw, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
			w.(http.Flusher).Flush()
		}
		event("message_start", map[string]any{"message": map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "claude-opus-5",
			"content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 900, "output_tokens": 0}}})
		event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		for _, p := range s.pieces {
			event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": p}})
			time.Sleep(s.pause)
		}
		event("content_block_stop", map[string]any{"index": 0})
		event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 120}})
		event("message_stop", map[string]any{})
	}))
	t.Cleanup(srv.Close)
	claude := aiprovider.NewClaude(func() string { return "sk-test" }, "claude-opus-5").WithBaseURL(srv.URL)
	return aiprovider.NewRouter(claude, nil, func() string { return "claude" }, func(string, string, int, int) {
		s.mu.Lock()
		s.usage++
		s.mu.Unlock()
	})
}

// An answer streamed by Claude is shown while it is written, in the Studio's
// AI panel and in the dock, and replaced by the checked result when it is
// complete; each request is one model call, logged once.
func TestStudioStreamsAIAnswersInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	stub := &slowClaude{pause: 300 * time.Millisecond, pieces: []string{
		`{"answer_markdown":"The purpose `, `states why `, `the policy exists, `, `but not what `, `anyone must do.`, `","proposal":null}`,
	}}
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "studio-stream.db"), nil, stub.router(t))
	alice := studioTab(t, browser, a, a.admin)
	if err := chromedp.Run(alice, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "AI is available", `!!document.querySelector('.ps-ai-menu')`)
	evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'Why this policy exists.') at = p });
		e.chain().focus().setTextSelection({ from: at, to: at + 23 }).run(); return true })()`)
	waitJS(t, alice, "the selection toolbar appears", `!!document.querySelector('.ps-bubble') && !document.querySelector('.ps-bubble').hidden`)
	if !evalJS[bool](t, alice, `(() => { const b = Array.from(document.querySelectorAll('.ps-bubble button')).find((x) => x.textContent === 'Tighten'); if (!b) return false; b.click(); return true })()`) {
		t.Fatal("no Tighten button")
	}
	waitJS(t, alice, "part of the answer shows while it is written",
		`(() => { const d = document.querySelector('.ps-ai-draft'); return !!d && !d.hidden && d.textContent.startsWith('The purpose') && !d.textContent.includes('must do') })()`)
	waitJS(t, alice, "the checked answer replaces it",
		`!document.querySelector('.ps-ai-draft') && document.querySelector('.ps-tabpanel').textContent.includes('but not what anyone must do.')`)

	if err := chromedp.Run(alice, chromedp.Click(`#global-ai-dock-toggle`, chromedp.ByQuery),
		chromedp.SendKeys(`#global-ai-dock-input`, "is the purpose testable?", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	evalJS[bool](t, alice, `(document.getElementById('global-ai-dock-form').requestSubmit(), true)`)
	waitJS(t, alice, "the dock shows the answer as it is written",
		`Array.from(document.querySelectorAll('#global-ai-dock .global-ai-dock-msg.ai')).some((m) => m.textContent.includes('The purpose') && !m.textContent.includes('must do'))`)
	waitJS(t, alice, "and then the whole answer",
		`Array.from(document.querySelectorAll('#global-ai-dock .global-ai-dock-msg.ai')).some((m) => m.textContent.includes('AI · claude-opus-5') && m.textContent.includes('anyone must do.'))`)
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.calls != 2 || stub.usage != 2 {
		t.Fatalf("%d model calls, usage logged %d times", stub.calls, stub.usage)
	}
}

// Compare in the browser: after an approval and an edit, the Compare tab shows
// the change against the approved version and links the redline for it.
func TestStudioCompareInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "studio-compare.db"), nil)
	d, err := a.policies.GetDocument(a.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A guideline needs only the purpose and scope the fixture has.
	d.EffectiveDate, d.DocType = "2026-10-01", policydocs.TypeGuideline
	if _, err := a.policies.UpdateDocument(d.ID, d); err != nil {
		t.Fatal(err)
	}
	if _, err := a.policies.SubmitForReview(a.doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.policies.Approve(a.doc.ID, "rev", "first issue"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.policies.ReturnToDraft(a.doc.ID); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	if err := a.conn.QueryRow(`SELECT id FROM policy_versions WHERE document_id = ?`, a.doc.ID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	alice := studioTab(t, browser, a, a.admin)
	evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'Why this policy exists.') at = p });
		e.commands.insertContentAt({ from: at + 16, to: at + 22 }, 'must exist'); return true })()`)
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, _ := a.policies.ListSections(a.doc.ID)
		if len(rows) > 0 && strings.Contains(rows[0].Body, "must exist") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the edit never reached the sections")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := chromedp.Run(alice, chromedp.Click(`#ps-tab-compare`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the versions load", `!!document.querySelector('select[aria-label="Compare from"]')`)
	evalJS[bool](t, alice, `(Array.from(document.querySelectorAll('.ps-tabpanel button')).find((b) => b.textContent === 'Compare').click(), true)`)
	waitJS(t, alice, "the change is shown word by word",
		`Array.from(document.querySelectorAll('.ps-diff-unit ins')).some((x) => x.textContent.includes('must')) && Array.from(document.querySelectorAll('.ps-diff-unit del')).some((x) => x.textContent.includes('exists'))`)
	href := evalJS[string](t, alice, `Array.from(document.querySelectorAll('.ps-tabpanel a')).find((x) => x.textContent === 'Download redline PDF').getAttribute('href')`)
	if want := "/policies/" + strconv.FormatInt(a.doc.ID, 10) + "/compare.pdf?from=" + strconv.FormatInt(versionID, 10) + "&to=current"; href != want {
		t.Fatalf("redline link %q, want %q", href, want)
	}
}

// Start from a library document, in the Studio: opened with ?library=, the
// Studio asks the AI to map that document in once, drops the parameter so a
// reload does not ask again, and shows the proposal; the model was given the
// library's passages, which were extracted on the Wintermute server.
func TestStudioStartsFromALibraryDocumentInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	var mu sync.Mutex
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "sess-lib"})
		case strings.HasSuffix(r.URL.Path, "/agents/general/documents"):
			_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{map[string]any{"id": 7, "title": "Old access policy.pdf", "chunk_count": 1}}})
		case strings.HasSuffix(r.URL.Path, "/agents/general/documents/7/text"):
			_ = json.NewEncoder(w).Encode(map[string]any{"document": map[string]any{"id": 7, "title": "Old access policy.pdf", "chunk_count": 1},
				"chunks": []any{map[string]any{"ordinal": 1, "heading": "2 Access reviews", "body": "Access rights are reviewed every six months by the system owner."}}})
		case strings.HasSuffix(r.URL.Path, "/messages"):
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			prompts = append(prompts, in["text"])
			mu.Unlock()
			answer, _ := json.Marshal(map[string]any{"answer_markdown": "Mapped the old policy's access reviews into Scope.", "proposal": nil})
			_ = json.NewEncoder(w).Encode(map[string]any{"reply": string(answer), "status": "complete", "backend": "local", "model": "qwen-policy",
				"usage": map[string]any{"input_tokens": 1500, "output_tokens": 300}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	wm := aiprovider.NewWintermute(func() aiprovider.WintermuteConfig {
		return aiprovider.WintermuteConfig{URL: srv.URL, Token: "t", Agent: "general"}
	})
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "studio-library.db"), nil, aiprovider.NewRouter(nil, wm, func() string { return "wintermute" }, nil))

	if code, body := getAs(t, a, "/policies/library", a.admin); code != http.StatusOK || !strings.Contains(body, `"title":"Old access policy.pdf"`) {
		t.Fatalf("the library list: %d %s", code, body)
	}
	tab, cancel := chromedp.NewContext(browser, chromedp.WithNewBrowserContext())
	t.Cleanup(cancel)
	u, _ := url.Parse(a.server.URL)
	if err := chromedp.Run(tab,
		network.SetCookie(authn.AuthSessionCookie, a.admin).WithDomain(u.Hostname()).WithPath("/"),
		chromedp.Navigate(a.server.URL+"/policies/"+strconv.FormatInt(a.doc.ID, 10)+"/studio?library=7"),
	); err != nil {
		t.Fatal(err)
	}
	waitJS(t, tab, "the mapping arrives in the AI panel", `document.querySelector('.ps-tabpanel') && document.querySelector('.ps-tabpanel').textContent.includes("Mapped the old policy's access reviews")`)
	if q := evalJS[string](t, tab, `location.search`); q != "" {
		t.Fatalf("the library parameter stayed in the address: %q", q)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Access rights are reviewed every six months") || !strings.Contains(prompts[0], "Old access policy.pdf") {
		t.Fatalf("%d prompts; the first: %.400q", len(prompts), strings.Join(prompts, "\n---\n"))
	}
}
