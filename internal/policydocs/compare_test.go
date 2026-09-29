package policydocs

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestDiffWordsReassembles(t *testing.T) {
	before := "Staff will review access every quarter."
	after := "Staff must review privileged access every quarter."
	words := diffWords(before, after)
	var b, a strings.Builder
	for _, w := range words {
		if w.Op != DiffAdded {
			b.WriteString(w.Text)
		}
		if w.Op != DiffRemoved {
			a.WriteString(w.Text)
		}
	}
	if b.String() != before || a.String() != after {
		t.Fatalf("the diff does not reassemble: %q / %q", b.String(), a.String())
	}
	var changed []string
	for _, w := range words {
		if w.Op != DiffSame {
			changed = append(changed, w.Op+":"+w.Text)
		}
	}
	if strings.Join(changed, ",") != "removed:will,added:must,added:privileged " {
		t.Fatalf("changed words: %v", changed)
	}
}

func TestCompareSections(t *testing.T) {
	a := []revSection{
		{heading: "Purpose", units: []string{"Why this policy exists."}},
		{heading: "Scope", units: []string{"• all staff", "• all systems"}},
		{heading: "Retired section", units: []string{"Old text."}},
	}
	b := []revSection{
		{heading: "Purpose", units: []string{"Why this policy exists."}},
		{heading: "scope", units: []string{"• all staff and contractors", "• all systems", "• all locations"}},
		{heading: "Exceptions", units: []string{"Exceptions must be approved."}},
	}
	secs, added, removed, changed := compareSections(a, b)
	if added != 1 || removed != 1 || changed != 1 {
		t.Fatalf("added %d removed %d changed %d: %+v", added, removed, changed, secs)
	}
	var scope DiffSection
	for _, s := range secs {
		if headingKey(s.Heading) == "scope" {
			scope = s
		}
	}
	ops := []string{}
	for _, u := range scope.Units {
		ops = append(ops, u.Op)
	}
	if scope.Op != DiffChanged || scope.Before != "Scope" || strings.Join(ops, ",") != "changed,same,added" {
		t.Fatalf("scope: %+v ops %v", scope, ops)
	}
}

// A document approved, then edited: the comparison with the approved version
// shows exactly the edit, whether the version is structured or a legacy
// Markdown snapshot.
func TestCompareAgainstAnApprovedVersion(t *testing.T) {
	svc := newTestService(t)
	doc, err := svc.CreateDocument(Document{Title: "Access", DocType: TypeGuideline, OwnerRole: "CISO", EffectiveDate: "2026-10-01"})
	if err != nil {
		t.Fatal(err)
	}
	purpose, _ := svc.CreateSection(Section{DocumentID: doc.ID, SectionKind: KindPurpose, Heading: "Purpose", Body: "Staff will review access."})
	if _, err := svc.CreateSection(Section{DocumentID: doc.ID, SectionKind: KindScope, Heading: "Scope", Body: "- all staff"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(doc.ID, "rev", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReturnToDraft(doc.ID); err != nil {
		t.Fatal(err)
	}
	purpose.Body = "Staff must review access."
	if _, err := svc.UpdateSection(doc.ID, purpose.ID, purpose); err != nil {
		t.Fatal(err)
	}
	versions, _ := svc.ListVersions(doc.ID)
	cmp, err := svc.Compare(doc.ID, strconv.FormatInt(versions[0].ID, 10), CurrentRevision)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Changed != 1 || cmp.Added != 0 || cmp.Removed != 0 || !cmp.From.Verified {
		t.Fatalf("comparison: %+v", cmp)
	}
	u := cmp.Sections[0].Units[0]
	if u.Op != DiffChanged || u.Before != "Staff will review access." || u.After != "Staff must review access." {
		t.Fatalf("unit: %+v", u)
	}
	// The same version read from its Markdown snapshot alone.
	legacy := versions[0]
	legacy.ContentJSON = ""
	if secs := sectionsFromVersion(legacy); len(secs) != 2 || secs[0].heading != "Purpose" || secs[1].units[0] != "• all staff" {
		t.Fatalf("legacy snapshot sections: %+v", secs)
	}
	if _, err := svc.Compare(doc.ID, "999999", CurrentRevision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown version: %v", err)
	}
	if _, err := svc.Compare(doc.ID, "v1", CurrentRevision); !IsValidation(err) {
		t.Fatalf("a malformed version ref: %v", err)
	}
}

// A list kept as Markdown and the same list held as blocks are the same text.
func TestMarkdownListsCompareEqualToBlocks(t *testing.T) {
	blocks := unitsFromBlocks([]Block{{Type: "bullet_list", Items: [][]Block{
		{{Type: "paragraph", Runs: []Run{{Text: "all staff"}}}}, {{Type: "paragraph", Runs: []Run{{Text: "all systems"}}}},
	}}})
	for _, body := range []string{"- all staff\n- all systems", "* all staff\n+  all systems"} {
		if got := unitsFromBody(body); strings.Join(got, "|") != strings.Join(blocks, "|") {
			t.Errorf("%q: %q, blocks %q", body, got, blocks)
		}
	}
}
