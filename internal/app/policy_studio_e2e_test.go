package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"grc/internal/aiprovider"
	"grc/internal/authn"
	"grc/internal/clientprofile"
	"grc/internal/policydocs"
	"grc/internal/settings"
)

// These drive the real Studio -- the embedded bundle, the theme middleware,
// the page gate and the collaboration socket -- in headless Chrome. They are
// skipped with -short and on a machine without Chrome.
func headlessBrowser(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("headless browser tests are skipped in -short mode")
	}
	found := false
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "headless-shell"} {
		if _, err := exec.LookPath(name); err == nil {
			found = true
		}
	}
	if !found {
		t.Skip("no Chrome or Chromium on PATH")
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WindowSize(1440, 900))...)
	browser, cancelBrowser := chromedp.NewContext(alloc)
	if err := chromedp.Run(browser); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancelBrowser(); cancelAlloc() })
	return browser
}

// studioTab opens the Studio in a new browser context (its own cookie jar)
// signed in with token, and waits for the editor to mount.
func studioTab(t *testing.T, browser context.Context, a *studioApp, token string) context.Context {
	t.Helper()
	tab, cancel := chromedp.NewContext(browser, chromedp.WithNewBrowserContext())
	t.Cleanup(cancel)
	u, _ := url.Parse(a.server.URL)
	studioURL := a.server.URL + "/policies/" + strconv.FormatInt(a.doc.ID, 10) + "/studio"
	err := chromedp.Run(tab,
		network.SetCookie(authn.AuthSessionCookie, token).WithDomain(u.Hostname()).WithPath("/"),
		chromedp.Navigate(studioURL),
	)
	if err != nil {
		t.Fatal(err)
	}
	waitJS(t, tab, "the editor mounts", `!!(window.GRCPolicyStudio && window.GRCPolicyStudio.editor)`)
	return tab
}

func evalJS[T any](t *testing.T, ctx context.Context, expr string) T {
	t.Helper()
	var out T
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	return out
}

func waitJS(t *testing.T, ctx context.Context, what, expr string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err == nil && ok {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	var body string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.body ? document.body.innerText.slice(0, 600) : ''`, &body))
	t.Fatalf("timed out waiting for %s (%s); page shows: %s", what, expr, body)
}

const editorText = `GRCPolicyStudio.editor.state.doc.textContent`

func TestStudioInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	dbPath := filepath.Join(t.TempDir(), "studio-e2e.db")
	a := newStudioAppAt(t, dbPath, nil)

	alice := studioTab(t, browser, a, a.admin)
	bob := studioTab(t, browser, a, a.admin)
	rita := studioTab(t, browser, a, a.reader)

	t.Run("two admins see each other's edits live", func(t *testing.T) {
		// Real input: a click into the purpose paragraph (focus), End, typing.
		err := chromedp.Run(alice,
			page.BringToFront(),
			chromedp.Click(`.ps-prosemirror section p`, chromedp.ByQuery),
			chromedp.KeyEvent(kb.End),
			chromedp.KeyEvent(" Typed by Alice."),
		)
		if err != nil {
			t.Fatal(err)
		}
		waitJS(t, bob, "Bob sees Alice's typing", editorText+`.includes('Typed by Alice.')`)
		waitJS(t, rita, "the reader sees it too", editorText+`.includes('Typed by Alice.')`)
		if evalJS[bool](t, rita, `GRCPolicyStudio.editor.isEditable`) {
			t.Fatal("a non-admin reader's editor is editable")
		}
		// The projection follows within the debounce.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			rows, _ := a.policies.ListSections(a.doc.ID)
			if len(rows) > 0 && strings.Contains(rows[0].Body, "Typed by Alice.") {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("the section row never received the typed text")
	})

	t.Run("paste across a section boundary is refused", func(t *testing.T) {
		before := evalJS[int](t, alice, `GRCPolicyStudio.editor.state.doc.childCount`)
		evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; const d = e.state.doc;
			e.commands.setTextSelection({ from: 4, to: d.content.size - 4 });
			e.view.pasteHTML('<p style="color:red" class="MsoNormal">Pasted over everything</p>'); return true })()`)
		if got := evalJS[int](t, alice, `GRCPolicyStudio.editor.state.doc.childCount`); got != before {
			t.Fatalf("sections went from %d to %d after a paste across them", before, got)
		}
		if evalJS[bool](t, alice, editorText+`.includes('Pasted over everything')`) {
			t.Fatal("the cross-section paste was applied")
		}
		waitJS(t, alice, "the refusal is explained", `document.querySelector('.ps-toast').textContent.includes('Use the outline')`)
		// A paste inside one paragraph is fine, and loses its styling.
		evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
			e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text.includes('Typed by Alice.')) at = p + n.nodeSize });
			e.commands.setTextSelection(at); e.view.pasteHTML('<span style="font-family:Comic Sans MS"> <b>Pasted</b> fine.</span>'); return true })()`)
		waitJS(t, bob, "a paste within a paragraph syncs", editorText+`.includes('Pasted fine.')`)
		if html := evalJS[string](t, bob, `document.querySelector('.ps-prosemirror').innerHTML`); strings.Contains(html, "Comic Sans") || strings.Contains(html, "MsoNormal") {
			t.Fatal("pasted styling survived")
		}
	})

	t.Run("in review admins suggest and readers stay read-only", func(t *testing.T) {
		if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`//button[normalize-space()='Submit for review']`, chromedp.BySearch)); err != nil {
			t.Fatal(err)
		}
		waitJS(t, alice, "the status changes", `document.querySelector('.ps-pill').textContent === 'In review'`)
		waitJS(t, alice, "Alice's editor switches to suggest mode", `document.querySelector('.ps-suggest-toggle') && document.querySelector('.ps-suggest-toggle').textContent === 'Suggesting (in review)' && GRCPolicyStudio.editor.isEditable`)
		// A hidden tab does not poll; the editor follows when it is looked at.
		if err := chromedp.Run(rita, page.BringToFront()); err != nil {
			t.Fatal(err)
		}
		waitJS(t, rita, "the reader's status updates", `document.querySelector('.ps-pill').textContent === 'In review'`)
		if evalJS[bool](t, rita, `GRCPolicyStudio.editor.isEditable`) {
			t.Fatal("a reader's editor is editable in review")
		}
		// Even forced editable, the server drops a reader's write.
		evalJS[bool](t, rita, `(() => { const e = GRCPolicyStudio.editor; e.setEditable(true); e.commands.insertContentAt(3, 'SNEAKED IN'); return true })()`)
		time.Sleep(2 * time.Second)
		if evalJS[bool](t, alice, editorText+`.includes('SNEAKED IN')`) {
			t.Fatal("a reader's write reached another peer")
		}
		if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`//button[normalize-space()='Return to draft']`, chromedp.BySearch)); err != nil {
			t.Fatal(err)
		}
		waitJS(t, alice, "Alice edits directly again", `document.querySelector('.ps-suggest-toggle') && document.querySelector('.ps-suggest-toggle').textContent === 'Editing'`)
	})

	t.Run("a restart mid-edit loses nothing", func(t *testing.T) {
		evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
			e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text.includes('fine.')) at = p + n.nodeSize });
			e.commands.insertContentAt(at, ' Written just before the restart.'); return true })()`)
		waitJS(t, bob, "the edit reaches the server", editorText+`.includes('just before the restart')`)
		before := evalJS[string](t, alice, `JSON.stringify(GRCPolicyStudio.editor.getJSON())`)
		// The graceful path Run takes on SIGTERM: flush the Studio, then stop.
		if err := a.studio.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		a.server.Close()

		b := newStudioAppAt(t, dbPath, a)
		after := evalJS[string](t, studioTab(t, browser, b, b.admin), `JSON.stringify(GRCPolicyStudio.editor.getJSON())`)
		if before != after {
			t.Fatalf("content changed across the restart\nbefore: %s\n after: %s", before, after)
		}
	})
}

// Phase 1b in the browser: a document from the default template, its missing
// facts shown as warning chips, filled in from the Facts panel (every chip for
// that fact resolves, the text untouched), and the paper view.
func TestStudioTemplatesAndFactsInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	a := newStudioApp(t)
	clientID := strconv.FormatInt(createClient(t, a, "Example Bank AG"), 10)

	tab, cancel := chromedp.NewContext(browser, chromedp.WithNewBrowserContext())
	t.Cleanup(cancel)
	u, _ := url.Parse(a.server.URL)
	if err := chromedp.Run(tab,
		network.SetCookie(authn.AuthSessionCookie, a.admin).WithDomain(u.Hostname()).WithPath("/"),
		chromedp.Navigate(a.server.URL+"/policies"),
		chromedp.WaitVisible(`#newBtn`, chromedp.ByQuery),
		chromedp.Click(`#newBtn`, chromedp.ByQuery),
		chromedp.WaitVisible(`#nd-template`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if got := evalJS[string](t, tab, `document.getElementById('nd-template').value`); got != "ict-infosec-policy" {
		t.Fatalf("the default template is not preselected: %q", got)
	}
	evalJS[bool](t, tab, `(() => { document.getElementById('nd-client').value = '`+clientID+`'; return true })()`)
	if err := chromedp.Run(tab, chromedp.Click(`//dialog//button[normalize-space()='Create']`, chromedp.BySearch)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, tab, "the new document opens in the Studio", `location.pathname.endsWith('/studio') && !!(window.GRCPolicyStudio && window.GRCPolicyStudio.editor)`)
	waitJS(t, tab, "missing facts show as warning chips", `document.querySelectorAll('.ps-fact[data-resolved="false"][data-fact="legal_entity_name"]').length > 0`)
	if n := evalJS[int](t, tab, `document.querySelectorAll('.ps-guidance').length`); n != 10 {
		t.Errorf("guidance notes: %d, want one per section", n)
	}

	// Fill legal_entity_name from the Facts panel.
	evalJS[bool](t, tab, `(document.getElementById('ps-tab-facts').click(), true)`)
	waitJS(t, tab, "the Facts tab lists the template's facts", `document.querySelectorAll('.ps-fact-row').length > 0`)
	evalJS[bool](t, tab, `(() => { const row = Array.from(document.querySelectorAll('.ps-fact-row')).find(r => r.querySelector('code').textContent === 'legal_entity_name');
		row.querySelector('input, textarea').value = 'Example Bank AG'; row.querySelector('button').click(); return true })()`)
	waitJS(t, tab, "every legal_entity_name chip resolves", `(() => { const chips = document.querySelectorAll('.ps-fact[data-fact="legal_entity_name"]');
		return chips.length > 0 && Array.from(chips).every(c => c.dataset.resolved === 'true' && c.textContent === 'Example Bank AG') })()`)
	if evalJS[bool](t, tab, `JSON.stringify(GRCPolicyStudio.editor.getJSON()).includes('Example Bank AG')`) {
		t.Fatal("a fact value was written into the document; tokens must render by reference")
	}

	if err := chromedp.Run(tab, chromedp.Click(`//button[normalize-space()='Paper view']`, chromedp.BySearch)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, tab, "paper view shows the classification band", `document.querySelector('.ps-canvas-inner').classList.contains('ps-paper-on') && document.querySelector('.ps-paper-band').textContent === 'INTERNAL'`)
}

func createClient(t *testing.T, a *studioApp, name string) int64 {
	t.Helper()
	c, err := a.clients.Create(clientprofile.Profile{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// Phase 2 in the browser: two administrators suggest at once, approval waits
// for every suggestion, the review is done from the keyboard, a comment
// reaches the other editor, and provenance names who accepted what.
func TestStudioReviewInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	a := newStudioApp(t)
	d, err := a.policies.GetDocument(a.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A guideline needs only the purpose and scope the fixture has, so the
	// pending suggestions are the only thing between it and approval.
	d.EffectiveDate, d.DocType = "2026-10-01", policydocs.TypeGuideline
	if _, err := a.policies.UpdateDocument(d.ID, d); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(a.doc.ID, 10)
	alice := studioTab(t, browser, a, a.admin)
	bob := studioTab(t, browser, a, a.admin2)

	suggestions := `GRCPolicyStudio.suggestions()`
	// The editor's header actions are there as soon as it mounts, not only
	// after the next state change.
	if !evalJS[bool](t, alice, `!!document.querySelector('.ps-suggest-toggle') && [...document.querySelectorAll('.ps-actions button')].some((b) => b.textContent === 'Comment')`) {
		t.Fatal("the suggest and comment actions are missing after the editor mounted")
	}
	// Both turn suggest mode on and type at the same moment, Bob with real
	// key events.
	for _, tab := range []context.Context{alice, bob} {
		if err := chromedp.Run(tab, page.BringToFront(), chromedp.Click(`.ps-suggest-toggle`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		waitJS(t, tab, "suggest mode is on", `document.querySelector('.ps-suggest-toggle').getAttribute('aria-pressed') === 'true'`)
	}
	// Bob's caret is placed by position: a click lands wherever the layout
	// has just moved to. His keys are real.
	evalJS[bool](t, bob, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'all staff') at = p + n.nodeSize });
		e.chain().focus().setTextSelection(at).run(); return true })()`)
	waitJS(t, bob, "Bob's editor has focus", `document.activeElement.classList.contains('ProseMirror')`)
	done := make(chan error, 1)
	go func() {
		done <- chromedp.Run(bob, page.BringToFront(), chromedp.KeyEvent(" and visitors"))
	}()
	evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text.includes('Why this policy exists.')) at = p + n.nodeSize });
		e.commands.insertContentAt(at, ' It binds every employee.'); return true })()`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, tab := range []context.Context{alice, bob} {
		waitJS(t, tab, "both suggestions reach both editors", suggestions+`.length === 2`)
	}
	got := evalJS[string](t, alice, suggestions+`.map((s) => s.authorName + ':' + s.inserted).sort().join('|')`)
	if got != "admin: It binds every employee.|bob: and visitors" || !evalJS[bool](t, alice, editorText+`.includes('all staff and visitors')`) {
		t.Fatalf("suggestions as Alice sees them: %q", got)
	}
	if ids := evalJS[int](t, bob, `new Set(`+suggestions+`.map((s) => s.id)).size`); ids != 2 {
		t.Fatalf("suggestion ids collide: %d distinct", ids)
	}
	if rows, _ := a.policies.ListSections(a.doc.ID); strings.Contains(rows[0].Body, "binds every employee") {
		t.Fatal("a pending suggestion reached the baseline")
	}

	// A comment on Alice's side reaches Bob as a highlight.
	evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text.includes('all staff')) at = p + n.text.indexOf('all staff') });
		e.commands.setTextSelection({ from: at, to: at + 9 }); return true })()`)
	if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`//button[normalize-space()='Comment']`, chromedp.BySearch)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the composer opens with the quote", `document.querySelector('.ps-composer .ps-quote').textContent === 'all staff'`)
	if err := chromedp.Run(alice,
		chromedp.SendKeys(`.ps-composer textarea`, "Does this include contractors?", chromedp.ByQuery),
		chromedp.Click(`//button[normalize-space()='Post comment']`, chromedp.BySearch)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the thread is listed", `document.querySelectorAll('.ps-thread').length === 1`)
	if err := chromedp.Run(bob, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	waitJS(t, bob, "Bob sees the commented text highlighted", `document.querySelector('.ps-comment-mark') && document.querySelector('.ps-comment-mark').textContent === 'all staff'`)

	// Submitted for review, approval is refused while anything is pending.
	if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`//button[normalize-space()='Submit for review']`, chromedp.BySearch)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the document is in review", `document.querySelector('.ps-pill').textContent === 'In review'`)
	_, err = a.policies.Approve(a.doc.ID, "rev", "")
	var refused policydocs.ApprovalError
	if !errors.As(err, &refused) {
		t.Fatalf("approval with pending suggestions: %v", err)
	}
	for _, f := range refused.Findings {
		if f.Rule != policydocs.RulePendingSuggestions {
			t.Fatalf("approval refused for something else: %+v", f)
		}
	}
	if code, body := getAs(t, a, "/policies/"+id+"/studio/state", a.admin); code != 200 || !strings.Contains(body, `"rule":"pending_suggestions"`) {
		t.Fatalf("the pending_suggestions finding: %d", code)
	}

	// Keyboard review in the Suggestions panel: J, A, then J, R.
	if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`#ps-tab-suggestions`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the panel lists both", `document.querySelectorAll('.ps-sugg-item').length === 2`)
	if err := chromedp.Run(alice, chromedp.KeyEvent("j")); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "J selects the first", `document.querySelectorAll('.ps-sugg-item-current').length === 1`)
	first := evalJS[string](t, alice, `document.querySelector('.ps-sugg-item-current strong').textContent`)
	if err := chromedp.Run(alice, chromedp.KeyEvent("a")); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "A accepts it", suggestions+`.length === 1`)
	if err := chromedp.Run(alice, chromedp.KeyEvent("r")); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "R rejects the other", suggestions+`.length === 0`)
	waitJS(t, bob, "the decisions reach Bob", suggestions+`.length === 0`)
	text := evalJS[string](t, bob, editorText)
	accepted, rejected := "It binds every employee.", "and visitors"
	if first == "bob" {
		accepted, rejected = rejected, accepted
	}
	if !strings.Contains(text, accepted) || strings.Contains(text, rejected) {
		t.Fatalf("after accepting %s's suggestion Bob reads %q", first, text)
	}

	// Provenance names who accepted and rejected which suggestion.
	code, prov := getAs(t, a, "/policies/"+id+"/studio/provenance", a.reader)
	if code != 200 || !strings.Contains(prov, `"decision":"accepted","decided_by":"admin"`) || !strings.Contains(prov, `"decision":"rejected","decided_by":"admin"`) {
		t.Fatalf("provenance: %d %s", code, prov)
	}
	if err := chromedp.Run(alice, chromedp.Click(`#ps-tab-provenance`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the Provenance panel says who accepted it", `document.querySelector('.ps-tabpanel').textContent.includes('accepted by admin')`)

	// With every suggestion decided, approval goes through and takes the
	// accepted text.
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, _ := a.policies.ListSections(a.doc.ID)
		joined := rows[0].Body + rows[1].Body
		if strings.Contains(joined, accepted) && !strings.Contains(joined, rejected) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the rows never followed the decisions: %q", joined)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := a.policies.Approve(a.doc.ID, "rev", ""); err != nil {
		t.Fatalf("approval after every suggestion was decided: %v", err)
	}
}

func getAs(t *testing.T, a *studioApp, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, a.server.URL+path, nil)
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// Whole-block suggestions, where suggest-changes 0.1.x is least proven: a new
// list item, a deleted list item and a new table row, each suggested, then
// decided, and the projection following only the decisions.
func TestStudioBlockSuggestionsInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	a := newStudioApp(t)
	tab := studioTab(t, browser, a, a.admin)
	suggestions := `GRCPolicyStudio.suggestions()`
	rows := func() string {
		r, _ := a.policies.ListSections(a.doc.ID)
		return r[1].Body + "\n" + r[1].BlocksJSON
	}
	waitRows := func(what string, ok func(string) bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !ok(rows()) {
			if time.Now().After(deadline) {
				t.Fatalf("%s; the scope section reads %q", what, rows())
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// A table, added directly (not as a suggestion) at the end of the scope.
	evalJS[bool](t, tab, `(() => { const e = GRCPolicyStudio.editor; const scope = e.state.doc.child(1);
		let end = 0; e.state.doc.forEach((n, off, i) => { if (i === 1) end = off + n.nodeSize - 1 });
		e.chain().insertContentAt(end, { type: 'table', content: [
			{ type: 'tableRow', content: [{ type: 'tableHeader', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'System' }] }] }] },
			{ type: 'tableRow', content: [{ type: 'tableCell', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Payments' }] }] }] }] }).run(); return true })()`)
	waitRows("the table is projected", func(s string) bool { return strings.Contains(s, "Payments") })

	if err := chromedp.Run(tab, page.BringToFront(), chromedp.Click(`.ps-suggest-toggle`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	// A new list item: Enter at the end of "all systems", then type. The caret
	// is placed by position (a click lands wherever the layout has moved to).
	// Enter is a DOM keydown: chromedp's KeyEvent for Enter also sends a
	// separate character event, which a real keyboard does not once the
	// editor has handled the keydown, and which would split the item twice.
	evalJS[bool](t, tab, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'all systems') at = p + n.nodeSize });
		e.chain().focus().setTextSelection(at).run(); return true })()`)
	waitJS(t, tab, "the editor has focus", `document.activeElement.classList.contains('ProseMirror')`)
	evalJS[bool](t, tab, `(document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })), true)`)
	if err := chromedp.Run(tab, chromedp.KeyEvent("all contractors")); err != nil {
		t.Fatal(err)
	}
	waitJS(t, tab, "the new list item is one suggestion", suggestions+`.length === 1 && `+suggestions+`[0].inserted.includes('all contractors')`)
	// A new table row after "Payments".
	evalJS[bool](t, tab, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'Payments') at = p + 1 });
		e.chain().setTextSelection(at).addRowAfter().run(); return true })()`)
	waitJS(t, tab, "the new row is a second suggestion", suggestions+`.length === 2`)
	// Delete the first list item's text: a suggested deletion.
	evalJS[bool](t, tab, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'all staff') at = p });
		e.chain().setTextSelection({ from: at, to: at + 9 }).deleteSelection().run(); return true })()`)
	waitJS(t, tab, "the deletion is a third suggestion", suggestions+`.length === 3`)
	if !evalJS[bool](t, tab, editorText+`.includes('all staff')`) {
		t.Fatal("a suggested deletion removed the text")
	}
	waitRows("the baseline leaves every pending block out", func(s string) bool {
		return !strings.Contains(s, "contractors") && strings.Contains(s, "all staff") && strings.Count(s, `"cells"`) == 2
	})

	decide := func(match, button string) {
		t.Helper()
		if err := chromedp.Run(tab, chromedp.Click(`#ps-tab-suggestions`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		// The panel follows the text a moment later (it is debounced).
		waitJS(t, tab, "the panel lists the suggestion", `Array.from(document.querySelectorAll('.ps-sugg-item')).some((li) => `+match+`)`)
		ok := evalJS[bool](t, tab, `(() => { const item = Array.from(document.querySelectorAll('.ps-sugg-item')).find((li) => `+match+`);
			if (!item) return false; Array.from(item.querySelectorAll('button')).find((b) => b.textContent === '`+button+`').click(); return true })()`)
		if !ok {
			t.Fatalf("no suggestion item matching %s", match)
		}
	}
	decide(`li.textContent.includes('all contractors')`, "Accept")
	waitJS(t, tab, "the list item is accepted", suggestions+`.length === 2`)
	decide(`li.textContent.includes('all staff')`, "Reject")
	waitJS(t, tab, "the deletion is rejected", suggestions+`.length === 1`)
	decide(`li.textContent.includes('Adds a table row')`, "Accept")
	waitJS(t, tab, "the table row is accepted", suggestions+`.length === 0`)
	waitRows("the projection follows the decisions", func(s string) bool {
		md, _, _ := strings.Cut(s, "\n|")
		return strings.Contains(md, "- all staff\n- all systems\n- all contractors") && strings.Count(md, "- ") == 3 && strings.Count(s, `"cells"`) == 3
	})
	if n := evalJS[int](t, tab, `document.querySelectorAll('.ps-prosemirror section:nth-of-type(2) tr').length`); n != 3 {
		t.Fatalf("the table has %d rows, want 3", n)
	}
}

// aiStub stands in for a wintermuted server over loopback. It reads the block
// ids out of the prompt it is sent, the way a model reads the excerpt, and
// answers with a proposal: one edit on the purpose paragraph, one on the "all
// staff" list item.
type aiStub struct {
	mu    sync.Mutex
	calls int
	usage int
	delay time.Duration
}

var excerptLine = regexp.MustCompile(`\[([0-9a-f-]{36})\] (?:- |# |## )?(.*)`)

func (s *aiStub) router(t *testing.T) *aiprovider.Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "sess-e2e"})
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		bids := map[string]string{}
		for _, m := range excerptLine.FindAllStringSubmatch(in["text"], -1) {
			bids[strings.TrimSpace(m[2])] = m[1]
		}
		s.mu.Lock()
		s.calls++
		delay := s.delay
		s.mu.Unlock()
		time.Sleep(delay)
		answer, _ := json.Marshal(map[string]any{
			"answer_markdown": "I made the purpose testable and widened the scope.",
			"proposal": map[string]any{
				"summary": "Testable purpose",
				"edits": []any{
					map[string]any{"op": "replace", "block_id": bids["Why this policy exists."], "quote": "Why this policy exists.",
						"replacement_markdown": "This policy **must** be followed by all staff.", "rationale": "States an obligation an assessor can test.",
						"citations": []any{map[string]any{"kind": "control", "ref": "AC-5", "quote": ""}}},
					map[string]any{"op": "replace", "block_id": bids["all staff"], "quote": "all staff",
						"replacement_markdown": "all staff and contractors", "rationale": "The scope should name contractors.", "citations": []any{}},
				},
				"control_mappings": []any{}, "new_facts": []any{},
			},
		})
		_ = json.NewEncoder(w).Encode(map[string]any{"reply": string(answer), "status": "complete", "backend": "local", "model": "qwen-policy",
			"usage": map[string]any{"input_tokens": 1500, "output_tokens": 300}})
	}))
	t.Cleanup(srv.Close)
	wm := aiprovider.NewWintermute(func() aiprovider.WintermuteConfig {
		return aiprovider.WintermuteConfig{URL: srv.URL, Token: "t"}
	})
	return aiprovider.NewRouter(wm, func(string, string, int, int) {
		s.mu.Lock()
		s.usage++
		s.mu.Unlock()
	})
}

// Phase 3's core flow in the browser: from the dock on a Studio page a
// request yields a proposal; it previews privately (the other editor sees
// nothing), then is suggested to everyone. The edit whose text changed in the
// meantime is reported stale and placed nowhere. The other participant sees
// the suggestion attributed to the AI with its rationale and citation, and
// accepting it is recorded with the actor.
func TestStudioAIProposalInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	stub := &aiStub{}
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "studio-ai.db"), nil, stub.router(t))
	id := strconv.FormatInt(a.doc.ID, 10)
	alice := studioTab(t, browser, a, a.admin)
	bob := studioTab(t, browser, a, a.admin2)
	suggestions := `GRCPolicyStudio.suggestions()`

	// The question is typed for real; the form is sent with requestSubmit,
	// which fires the submit event the dock listens for (chromedp's Submit
	// calls form.submit(), which does not).
	if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`#global-ai-dock-toggle`, chromedp.ByQuery),
		chromedp.SendKeys(`#global-ai-dock-input`, "make §1 testable", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	evalJS[bool](t, alice, `(document.getElementById('global-ai-dock-form').requestSubmit(), true)`)
	waitJS(t, alice, "the dock shows the proposal as cards", `!!document.querySelector('#global-ai-dock .ps-ai-proposal') && document.querySelectorAll('#global-ai-dock .ps-ai-edit').length === 2`)
	if stub.calls != 1 || stub.usage != 1 {
		t.Fatalf("%d model calls, usage logged %d times", stub.calls, stub.usage)
	}
	clickDockButton := func(label string) {
		t.Helper()
		if !evalJS[bool](t, alice, `(() => { const b = Array.from(document.querySelectorAll('#global-ai-dock .ps-ai-proposal button')).find((x) => x.textContent === '`+label+`'); if (!b) return false; b.click(); return true })()`) {
			t.Fatalf("no %q button in the dock", label)
		}
	}

	clickDockButton("Preview in document")
	waitJS(t, alice, "Alice sees the preview", `document.querySelectorAll('.ps-ai-preview-ins').length === 2`)
	if evalJS[int](t, bob, `document.querySelectorAll('.ps-ai-preview-ins').length`) != 0 || evalJS[int](t, bob, suggestions+`.length`) != 0 {
		t.Fatal("a private preview reached the other editor")
	}

	// Bob rewrites the list item the second edit quotes.
	evalJS[bool](t, bob, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'all staff') at = p });
		e.commands.insertContentAt({ from: at, to: at + 9 }, 'all employees'); return true })()`)
	waitJS(t, alice, "Alice sees Bob's edit", editorText+`.includes('all employees')`)

	clickDockButton("Suggest to everyone")
	waitJS(t, alice, "the button keeps its name", `Array.from(document.querySelectorAll('#global-ai-dock .ps-ai-proposal button')).some((b) => b.textContent === 'Suggested to everyone')`)
	waitJS(t, alice, "the stale edit is reported", `document.querySelector('.ps-toast').textContent.includes("couldn't be placed because the text changed")`)
	var placements []string
	rows, err := a.conn.Query(`SELECT placement FROM policy_ai_edits ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		placements = append(placements, p)
	}
	rows.Close()
	if strings.Join(placements, ",") != "placed,stale" {
		t.Fatalf("placements %v, want placed,stale", placements)
	}
	if evalJS[bool](t, alice, editorText+`.includes('and contractors')`) {
		t.Fatal("the stale edit was placed somewhere else")
	}

	if err := chromedp.Run(bob, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	waitJS(t, bob, "Bob sees the AI's suggestion", suggestions+`.length === 1 && `+suggestions+`[0].authorKind === 'ai' && `+suggestions+`[0].authorName === 'AI · qwen-policy'`)
	if err := chromedp.Run(bob, chromedp.Click(`#ps-tab-suggestions`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, bob, "with its rationale and citation", `(() => { const li = document.querySelector('.ps-sugg-item'); return !!li && li.textContent.includes('Why: States an obligation') && li.textContent.includes('Cites AC-5') })()`)
	evalJS[bool](t, bob, `(document.querySelector('.ps-sugg-item .ps-primary').click(), true)`)
	waitJS(t, bob, "Bob accepts it", suggestions+`.length === 0 && `+editorText+`.includes('must be followed by all staff')`)
	code, body := getAs(t, a, "/policies/"+id+"/studio/ai/edits", a.reader)
	if code != 200 || !strings.Contains(body, `"decision":"accept","decided_by":"bob"`) {
		t.Fatalf("the decision on the AI edit: %d %s", code, body)
	}
}

// The inline path: select text, "Make testable" from the selection toolbar.
// While the model works, the other editor sees that the AI is drafting and
// where; the proposal arrives in the AI panel previewed privately, and is
// suggested to everyone from there. An edit outside the selection is refused
// by the server and listed as such.
func TestStudioAIInlineActionsInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	stub := &aiStub{delay: 2 * time.Second}
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "studio-inline.db"), nil, stub.router(t))
	alice := studioTab(t, browser, a, a.admin)
	bob := studioTab(t, browser, a, a.admin2)

	waitJS(t, alice, "AI is available", `!!document.querySelector('.ps-ai-menu')`)
	// The toolbar follows a selection in a focused editor.
	if err := chromedp.Run(alice, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text === 'Why this policy exists.') at = p });
		e.chain().focus().setTextSelection({ from: at, to: at + 23 }).run(); return true })()`)
	waitJS(t, alice, "the selection toolbar appears", `!!document.querySelector('.ps-bubble') && !document.querySelector('.ps-bubble').hidden`)
	if !evalJS[bool](t, alice, `(() => { const b = Array.from(document.querySelectorAll('.ps-bubble button')).find((x) => x.textContent === 'Make testable'); if (!b) return false; b.click(); return true })()`) {
		t.Fatal("no Make testable button")
	}
	if err := chromedp.Run(bob, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	waitJS(t, bob, "Bob sees the AI drafting", `Array.from(document.querySelectorAll('.ps-presence-ai')).some((li) => li.textContent.includes('drafting in §1'))`)
	if err := chromedp.Run(alice, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the proposal is in the AI panel, previewed", `!!document.querySelector('.ps-tabpanel .ps-ai-proposal') && document.querySelectorAll('.ps-ai-preview-ins').length === 1`)
	if !evalJS[bool](t, alice, `document.querySelector('.ps-tabpanel .ps-ai-proposal').textContent.includes("1 edit(s) the server refused")`) {
		t.Fatal("the edit outside the selection was not listed as refused")
	}
	waitJS(t, bob, "Bob's drafting indicator clears", `document.querySelectorAll('.ps-presence-ai').length === 0`)
	evalJS[bool](t, alice, `(Array.from(document.querySelectorAll('.ps-tabpanel .ps-ai-proposal button')).find((b) => b.textContent === 'Suggest to everyone').click(), true)`)
	waitJS(t, bob, "Bob sees the AI's suggestion", `GRCPolicyStudio.suggestions().length === 1 && GRCPolicyStudio.suggestions()[0].authorKind === 'ai'`)
}

// Phase 4 in the browser. An administrator invites a guest from the Sharing
// panel; the guest joins by the link and suggests and comments live, and never
// sees an internal comment. The administrator presents and the guest follows
// until they opt out; the customer-safe view hides the internal comment and
// the lint; withdrawing the link ends the guest's access within seconds.
func TestStudioGuestsInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	a := newStudioApp(t)
	if err := a.settings.SetPreference(settings.PrefStudioGuestLinks, "true"); err != nil {
		t.Fatal(err)
	}
	send := func(method, path, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: a.admin})
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	code, body := send(http.MethodPost, "/policies/from-template", `{"template_id":"ict-infosec-policy"}`)
	if code != http.StatusCreated {
		t.Fatalf("from template: %d %s", code, body)
	}
	var created struct {
		Document struct {
			ID int64 `json:"id"`
		} `json:"document"`
	}
	_ = json.Unmarshal([]byte(body), &created)
	a.doc.ID = created.Document.ID
	id := strconv.FormatInt(a.doc.ID, 10)
	var uids []string
	rows, _ := a.conn.Query(`SELECT uid FROM policy_sections WHERE document_id = ? ORDER BY ordinal`, a.doc.ID)
	for rows.Next() {
		var u string
		_ = rows.Scan(&u)
		uids = append(uids, u)
	}
	rows.Close()
	for _, c := range []struct{ vis, body string }{{"internal", "INTERNAL-5c1d: they will resist the annual review"}, {"shared", "SHARED-7e2f: does this match your org chart?"}} {
		if code, body := send(http.MethodPost, "/policies/"+id+"/studio/comments", `{"section_uid":"`+uids[0]+`","anchor_start":"AQID","anchor_end":"AQIE","visibility":"`+c.vis+`","body":"`+c.body+`"}`); code != http.StatusCreated {
			t.Fatalf("comment: %d %s", code, body)
		}
	}

	alice := studioTab(t, browser, a, a.admin)
	if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`#ps-tab-sharing`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "the Sharing panel loads", `!!document.querySelector('.ps-tabpanel select[aria-label="Role"]')`)
	evalJS[bool](t, alice, `(() => { const p = document.querySelector('.ps-tabpanel');
		p.querySelector('input[aria-label="Label"]').value = 'CISO workshop';
		p.querySelector('select[aria-label="Role"]').value = 'editor';
		Array.from(p.querySelectorAll('button')).find((b) => b.textContent === 'Create link').click(); return true })()`)
	waitJS(t, alice, "the link is shown once", `!!document.querySelector('.ps-share-url')`)
	link := evalJS[string](t, alice, `document.querySelector('.ps-share-url').value`)
	if !strings.Contains(link, "/shared/p/") {
		t.Fatalf("link %q", link)
	}

	guest, cancel := chromedp.NewContext(browser, chromedp.WithNewBrowserContext())
	t.Cleanup(cancel)
	if err := chromedp.Run(guest, chromedp.Navigate(link), chromedp.WaitVisible(`#guest-name`, chromedp.ByQuery),
		chromedp.SendKeys(`#guest-name`, "Carla", chromedp.ByQuery), chromedp.Click(`button[type="submit"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, guest, "the guest's editor mounts", `!!(window.GRCPolicyStudio && window.GRCPolicyStudio.editor) && location.pathname === '/shared/studio'`)
	if !evalJS[bool](t, guest, `GRCPolicyStudio.editor.isEditable && document.querySelector('.ps-suggest-toggle').getAttribute('aria-pressed') === 'true'`) {
		t.Fatal("an editor guest starts editable, in suggest mode")
	}
	if evalJS[bool](t, guest, `!!document.getElementById('ps-tab-readiness') || !!document.getElementById('ps-tab-sharing') || !!document.querySelector('.ps-chip')`) {
		t.Fatal("the guest sees internal panels or control chips")
	}

	// The guest suggests, with real keys.
	evalJS[bool](t, guest, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.isText && n.text.includes('This policy sets out how')) at = p + 8 });
		e.chain().focus().setTextSelection(at).run(); return true })()`)
	waitJS(t, guest, "the guest's editor has focus", `document.activeElement.classList.contains('ProseMirror')`)
	if err := chromedp.Run(guest, page.BringToFront(), chromedp.KeyEvent("formally ")); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(alice, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	waitJS(t, alice, "Alice sees the guest's suggestion", `GRCPolicyStudio.suggestions().some((s) => s.authorKind === 'guest' && s.authorName === 'Carla (guest)' && s.inserted.includes('formally'))`)

	// The guest sees the shared comment and never the internal one.
	if err := chromedp.Run(guest, page.BringToFront(), chromedp.Click(`#ps-tab-comments`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitJS(t, guest, "the shared comment is listed", `document.querySelector('.ps-tabpanel').textContent.includes('SHARED-7e2f')`)
	if evalJS[bool](t, guest, `document.documentElement.innerHTML.includes('INTERNAL-5c1d')`) {
		t.Fatal("an internal comment reached the guest's page")
	}

	// Alice presents; the guest follows her to the fifth section.
	if err := chromedp.Run(alice, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	evalJS[bool](t, alice, `(() => { const d = document.querySelector('.ps-workshop'); d.open = true;
		const box = d.querySelector('input[data-workshop="presenting"]'); box.checked = true; box.dispatchEvent(new Event('change')); return true })()`)
	moveTo := func(uid string) {
		evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
			e.state.doc.forEach((n, off) => { if (n.attrs.uid === '`+uid+`') at = off + 2 });
			e.chain().focus().setTextSelection(at).run(); return true })()`)
	}
	moveTo(uids[4])
	if err := chromedp.Run(guest, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	inView := func(uid string) string {
		return `(() => { const r = document.querySelector('section[data-uid="` + uid + `"]').getBoundingClientRect(); return r.top > -80 && r.top < 320 })()`
	}
	waitJS(t, guest, "the guest follows Alice to §5", inView(uids[4])+` && document.querySelector('.ps-following') !== null`)
	evalJS[bool](t, guest, `(Array.from(document.querySelectorAll('.ps-following button')).find((b) => b.textContent === 'Stop following').click(), true)`)
	if err := chromedp.Run(alice, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	moveTo(uids[8])
	time.Sleep(1500 * time.Millisecond)
	if !evalJS[bool](t, guest, inView(uids[4])) {
		t.Fatal("a guest who stopped following was moved anyway")
	}

	// The customer-safe view hides the internal comment and the lint.
	evalJS[bool](t, alice, `(() => { const d = document.querySelector('.ps-workshop'); d.open = true;
		const box = d.querySelector('input[data-workshop="safe"]'); box.checked = true; box.dispatchEvent(new Event('change')); return true })()`)
	waitJS(t, alice, "the customer-safe view drops the internal panels", `!document.getElementById('ps-tab-readiness') && !document.getElementById('ps-tab-ai') && !!document.getElementById('ps-tab-comments')`)
	evalJS[bool](t, alice, `(document.getElementById('ps-tab-comments').click(), true)`)
	waitJS(t, alice, "only the shared comment is listed", `(() => { const p = document.querySelector('.ps-tabpanel').textContent; return p.includes('SHARED-7e2f') && !p.includes('INTERNAL-5c1d') })()`)
	evalJS[bool](t, alice, `(() => { const box = document.querySelector('input[data-workshop="safe"]'); box.checked = false; box.dispatchEvent(new Event('change')); return true })()`)

	// Withdrawing the link ends the guest's access within seconds.
	evalJS[bool](t, alice, `(document.getElementById('ps-tab-sharing').click(), true)`)
	waitJS(t, alice, "the link is listed", `!!Array.from(document.querySelectorAll('.ps-tabpanel button')).find((b) => b.textContent === 'Withdraw')`)
	evalJS[bool](t, alice, `(window.confirm = () => true, Array.from(document.querySelectorAll('.ps-tabpanel button')).find((b) => b.textContent === 'Withdraw').click(), true)`)
	if err := chromedp.Run(guest, page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	waitJS(t, guest, "the guest is told their access has ended", `document.body.textContent.includes('Your access has ended')`)
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("revocation took %v to reach the guest", took)
	}
}
