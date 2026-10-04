package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"grc/internal/aiprovider"
	"grc/internal/authn"
)

// A template drafted from a library document, reviewed, fixed and published
// in the browser, and then offered by New document.
func TestPolicyTemplatesInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	answer, _ := json.Marshal(map[string]any{
		"title": "Access Review Standard", "doc_type": "standard", "frameworks": []any{"ISO 27001"},
		"description": "How access rights are reviewed.", "review_cadence_months": 12, "classification": "Internal",
		"facts": []any{map[string]any{"key": "review_period", "label": "Review period", "description": "How often access is reviewed", "example": "six months", "value_type": "duration"}},
		"sections": []any{
			map[string]any{"heading": "Purpose", "kind": "purpose", "content": "Access rights must be reviewed every {{fact:review_period}}.", "guidance": "Agree the period with the client.", "source_passages": []any{2}, "proposed_mappings": []any{}},
			map[string]any{"heading": "Scope", "kind": "scope", "content": "All systems that hold regulated data.", "guidance": "", "source_passages": []any{1}, "proposed_mappings": []any{}},
			map[string]any{"heading": "Requirements", "kind": "statements", "content": "- Reviews must be recorded.\n- Findings must be remediated.", "guidance": "", "source_passages": []any{}, "proposed_mappings": []any{}},
		},
		"notes": []any{"Left out the sample's list of system names."},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "sess-tpl"})
		case strings.HasSuffix(r.URL.Path, "/agents/general/documents"):
			_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{map[string]any{"id": 7, "title": "Old access policy.pdf", "chunk_count": 2}}})
		case strings.HasSuffix(r.URL.Path, "/agents/general/documents/7/text"):
			_ = json.NewEncoder(w).Encode(map[string]any{"document": map[string]any{"id": 7, "title": "Old access policy.pdf", "chunk_count": 2},
				"chunks": []any{map[string]any{"ordinal": 1, "heading": "1 Scope", "body": "This policy covers the bank's systems."},
					map[string]any{"ordinal": 2, "heading": "2 Access reviews", "body": "Access rights are reviewed every six months by the system owner."}}})
		case strings.HasSuffix(r.URL.Path, "/messages"):
			_ = json.NewEncoder(w).Encode(map[string]any{"reply": string(answer), "status": "complete", "backend": "local", "model": "qwen-policy",
				"usage": map[string]any{"input_tokens": 1200, "output_tokens": 600}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	wm := aiprovider.NewWintermute(func() aiprovider.WintermuteConfig {
		return aiprovider.WintermuteConfig{URL: srv.URL, Token: "t", Agent: "general"}
	})
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "templates-e2e.db"), nil, aiprovider.NewRouter(wm, nil))

	tab, cancel := chromedp.NewContext(browser, chromedp.WithNewBrowserContext())
	t.Cleanup(cancel)
	u, _ := url.Parse(a.server.URL)
	if err := chromedp.Run(tab, network.SetCookie(authn.AuthSessionCookie, a.admin).WithDomain(u.Hostname()).WithPath("/"),
		chromedp.Navigate(a.server.URL+"/policies/templates/manage")); err != nil {
		t.Fatal(err)
	}
	waitJS(t, tab, "the built-in templates are listed", `document.getElementById('list').textContent.includes('ICT and Information Security Policy')`)
	evalJS[bool](t, tab, `(document.getElementById('draftBtn').click(), true)`)
	waitJS(t, tab, "the library is offered", `(() => { const s = document.getElementById('td-library'); return !!s && Array.from(s.options).some(o => o.value === '7') })()`)
	waitJS(t, tab, "the destination is shown", `document.querySelector('dialog.tpl-dialog').textContent.includes('The sample goes to AI · Wintermute')`)
	evalJS[bool](t, tab, `(() => { const s = document.getElementById('td-library'); s.value = '7'; s.dispatchEvent(new Event('change')); return true })()`)
	evalJS[bool](t, tab, `(Array.from(document.querySelectorAll('dialog.tpl-dialog button')).find(b => b.textContent === 'Draft').click(), true)`)
	waitJS(t, tab, "the draft opens in the editor", `document.getElementById('editorHeader').textContent === 'Access Review Standard'`)
	body := evalJS[string](t, tab, `document.getElementById('editor').textContent`)
	for _, want := range []string{"No problems: this template can be published.", "Left out the sample's list of system names.", "From the sample: S2 2 Access reviews", "Drafted by qwen-policy from “Old access policy.pdf”"} {
		if !strings.Contains(body, want) {
			t.Errorf("the editor lacks %q", want)
		}
	}

	// An edit that breaks a rule is listed as a problem once saved, and
	// publishing waits for it to be fixed.
	setPurpose := func(text string) {
		evalJS[bool](t, tab, `(() => { const ta = document.querySelector('.section[data-seed="purpose"] textarea'); ta.value = `+jsString(text)+`; ta.dispatchEvent(new Event('input')); return true })()`)
	}
	clickAction := func(label string) {
		t.Helper()
		if !evalJS[bool](t, tab, `(() => { const b = Array.from(document.querySelectorAll('#editor .actions button')).find(x => x.textContent === `+jsString(label)+`); if (!b || b.disabled) return false; b.click(); return true })()`) {
			t.Fatalf("no enabled %q button", label)
		}
	}
	setPurpose("Access rights will be reviewed every {{fact:review_period}}.")
	clickAction("Save")
	waitJS(t, tab, "the problem is listed", `document.getElementById('editor').textContent.includes('says "will"')`)
	if evalJS[bool](t, tab, `!Array.from(document.querySelectorAll('#editor .actions button')).find(x => x.textContent === 'Publish').disabled`) {
		t.Fatal("Publish is enabled while the template has a problem")
	}
	setPurpose("Access rights must be reviewed every {{fact:review_period}}.")
	clickAction("Save and publish")
	waitJS(t, tab, "it is published", `document.getElementById('editor').textContent.includes('Published 1.0.0') && document.getElementById('status').textContent.includes('New document offers it now')`)

	// New document offers it, and a document is made from it.
	if err := chromedp.Run(tab, chromedp.Navigate(a.server.URL+"/policies/manage")); err != nil {
		t.Fatal(err)
	}
	waitJS(t, tab, "the editor loads", `!!document.getElementById('newBtn')`)
	evalJS[bool](t, tab, `(document.getElementById('newBtn').click(), true)`)
	waitJS(t, tab, "New document offers the template", `(() => { const s = document.getElementById('nd-template'); return !!s && Array.from(s.options).some(o => o.value === 'access-review-standard') })()`)
	evalJS[bool](t, tab, `(() => { const s = document.getElementById('nd-template'); s.value = 'access-review-standard'; s.dispatchEvent(new Event('change')); return true })()`)
	evalJS[bool](t, tab, `(Array.from(document.querySelectorAll('dialog.new-doc button')).find(b => b.textContent === 'Create').click(), true)`)
	waitJS(t, tab, "the Studio opens on the new document", `!!(window.GRCPolicyStudio && window.GRCPolicyStudio.editor) && GRCPolicyStudio.editor.state.doc.textContent.includes('Findings must be remediated.')`)
}

func jsString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
