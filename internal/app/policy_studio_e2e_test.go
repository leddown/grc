package app

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"grc/internal/authn"
	"grc/internal/clientprofile"
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

	t.Run("submitting for review turns every editor read-only", func(t *testing.T) {
		if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`//button[normalize-space()='Submit for review']`, chromedp.BySearch)); err != nil {
			t.Fatal(err)
		}
		waitJS(t, alice, "the status changes", `document.querySelector('.ps-pill').textContent === 'In review'`)
		waitJS(t, alice, "Alice's editor turns read-only", `GRCPolicyStudio.editor.isEditable === false`)
		// A hidden tab does not poll; the server has already made its socket
		// read-only, and the editor follows when the tab is looked at.
		if err := chromedp.Run(bob, page.BringToFront()); err != nil {
			t.Fatal(err)
		}
		waitJS(t, bob, "Bob's editor turns read-only", `GRCPolicyStudio.editor.isEditable === false`)
		// Even forced editable, the server drops the write.
		evalJS[bool](t, bob, `(() => { const e = GRCPolicyStudio.editor; e.setEditable(true); e.commands.insertContentAt(3, 'SNEAKED IN'); return true })()`)
		time.Sleep(2 * time.Second)
		if evalJS[bool](t, alice, editorText+`.includes('SNEAKED IN')`) {
			t.Fatal("a write reached another peer while the document was in review")
		}
		if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`//button[normalize-space()='Return to draft']`, chromedp.BySearch)); err != nil {
			t.Fatal(err)
		}
		waitJS(t, alice, "Alice can edit again", `GRCPolicyStudio.editor.isEditable === true`)
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
