package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// The formatting toolbar's list buttons, clicked with the mouse: the caret
// stays in the text, lists toggle and convert, items indent and outdent, the
// projection carries the list, and a reader never sees the toolbar.
func TestStudioListToolbarInTheBrowser(t *testing.T) {
	browser := headlessBrowser(t)
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "studio-lists.db"), nil)
	alice := studioTab(t, browser, a, a.admin)
	rita := studioTab(t, browser, a, a.reader)

	if !evalJS[bool](t, rita, `document.querySelector('.ps-format').hidden`) {
		t.Fatal("a reader sees the formatting toolbar")
	}
	if evalJS[bool](t, alice, `document.querySelector('.ps-format').hidden`) {
		t.Fatal("an admin on a draft has no formatting toolbar")
	}

	click := func(tool string) {
		t.Helper()
		if err := chromedp.Run(alice, page.BringToFront(), chromedp.Click(`.ps-format-btn[data-tool="`+tool+`"]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
	}
	pressed := func(tool string) bool {
		return evalJS[bool](t, alice, `document.querySelector('.ps-format-btn[data-tool="`+tool+`"]').getAttribute('aria-pressed') === 'true'`)
	}
	disabled := func(tool string) bool {
		return evalJS[bool](t, alice, `document.querySelector('.ps-format-btn[data-tool="`+tool+`"]').disabled`)
	}
	firstSection := `GRCPolicyStudio.editor.state.doc.firstChild`
	// The caret at the end of the first section's first paragraph.
	evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.type.name === 'paragraph') at = p + 1 + n.content.size });
		e.chain().focus().setTextSelection(at).run(); return true })()`)
	if pressed("bullets") || !disabled("indent") {
		t.Fatal("outside a list, bullets show pressed or indent is enabled")
	}

	click("bullets")
	waitJS(t, alice, "the paragraph becomes a bulleted list", firstSection+`.child(1).type.name === 'bulletList'`)
	if !pressed("bullets") || pressed("numbering") {
		t.Fatal("in a bulleted list, the bullets button is not the pressed one")
	}
	if !evalJS[bool](t, alice, `GRCPolicyStudio.editor.state.selection.$from.parent.type.name === 'paragraph' && GRCPolicyStudio.editor.isFocused`) {
		t.Fatal("the click moved the caret out of the text")
	}

	click("numbering")
	waitJS(t, alice, "the bulleted list becomes numbered", firstSection+`.child(1).type.name === 'orderedList'`)
	if !pressed("numbering") || pressed("bullets") {
		t.Fatal("in a numbered list, the numbering button is not the pressed one")
	}

	evalJS[bool](t, alice, `GRCPolicyStudio.editor.chain().focus().splitListItem('listItem').insertContent('Second item').run()`)
	if disabled("indent") {
		t.Fatal("the second item cannot be indented")
	}
	click("indent")
	waitJS(t, alice, "the second item nests under the first", firstSection+`.child(1).childCount === 1 && `+firstSection+`.child(1).firstChild.lastChild.type.name === 'orderedList'`)
	if style := evalJS[string](t, alice, `getComputedStyle(document.querySelector('.ps-prosemirror ol ol')).listStyleType`); style != "lower-alpha" {
		t.Fatalf("a nested numbered list is styled %q, want lower-alpha", style)
	}
	if style := evalJS[string](t, alice, `getComputedStyle(document.querySelector('.ps-prosemirror ol')).listStyleType`); style != "decimal" {
		t.Fatalf("a numbered list is styled %q, want decimal", style)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, _ := a.policies.ListSections(a.doc.ID)
		if len(rows) > 0 && strings.Contains(rows[0].Body, "1. ") && strings.Contains(rows[0].Body, "   1. Second item") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the section row never received the nested numbered list: %q", rows[0].Body)
		}
		time.Sleep(200 * time.Millisecond)
	}

	click("outdent")
	waitJS(t, alice, "the second item comes back out", firstSection+`.child(1).childCount === 2`)

	click("numbering")
	// Like Word, only the item with the caret leaves the list.
	waitJS(t, alice, "numbering toggles off for the current item", `(() => { const $f = GRCPolicyStudio.editor.state.selection.$from;
		return $f.depth === 2 && $f.parent.textContent === 'Second item' })()`)
	if pressed("numbering") {
		t.Fatal("the numbering button stays pressed outside the list")
	}

	// A section heading is never a list item.
	evalJS[bool](t, alice, `GRCPolicyStudio.editor.chain().focus().setTextSelection(3).run()`)
	if !evalJS[bool](t, alice, `GRCPolicyStudio.editor.state.selection.$from.parent.type.name === 'sectionHeading'`) {
		t.Fatal("the caret is not in the section heading")
	}
	if !disabled("bullets") || !disabled("numbering") {
		t.Fatal("the list buttons are enabled in a section heading")
	}

	// In suggest mode the list is a suggestion someone accepts or rejects.
	if err := chromedp.Run(alice, chromedp.Click(`.ps-suggest-toggle`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	evalJS[bool](t, alice, `(() => { const e = GRCPolicyStudio.editor; let at = -1;
		e.state.doc.descendants((n, p) => { if (at < 0 && n.type.name === 'paragraph') at = p + 1 });
		e.chain().focus().setTextSelection(at).run(); return true })()`)
	click("bullets")
	waitJS(t, alice, "the list is a pending suggestion", `GRCPolicyStudio.suggestions().length > 0`)
}
