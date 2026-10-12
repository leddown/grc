package policystudio

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"

	"grc/internal/policydocs"
)

var sixSections = []string{
	"This policy sets out how the organisation protects its information.",
	"It applies to all staff.",
	"Staff must protect information.",
	"The CISO owns this policy.",
	"Management must support this policy.",
	"Breaches must be reported.",
}

// suggest appends a suggested insertion, attributed as a suggesting client
// would attribute it, to the first paragraph of section i.
func suggest(frag *crdt.YXmlFragment, txn *crdt.Transaction, i int, id, author, text string) {
	for _, c := range sectionEl(frag, i).Children() {
		p, ok := c.(*crdt.YXmlElement)
		if !ok || p.NodeName != "paragraph" {
			continue
		}
		attrs := textAttributes([]Mark{{Type: "insertion", Attrs: map[string]any{
			"id": id, "authorId": author, "authorKind": "human", "authorName": strings.ToUpper(author[:1]) + author[1:],
			"createdAt": "2026-09-29T08:00:00Z",
		}}})
		for _, t := range p.Children() {
			if xt, ok := t.(*crdt.YXmlText); ok {
				xt.Insert(txn, xt.Len(), text, attrs)
				return
			}
		}
	}
}

// dropText removes the last n characters of section i's first paragraph,
// which is what rejecting a suggested insertion does to the shared text.
func dropText(frag *crdt.YXmlFragment, txn *crdt.Transaction, i, n int) {
	for _, c := range sectionEl(frag, i).Children() {
		if p, ok := c.(*crdt.YXmlElement); ok && p.NodeName == "paragraph" {
			for _, t := range p.Children() {
				if xt, ok := t.(*crdt.YXmlText); ok {
					xt.Delete(txn, xt.Len()-n, n)
					return
				}
			}
		}
	}
}

func hasRule(findings []policydocs.Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// Approval is refused while any suggestion is pending; decisions are
// recorded with the session's actor and what the server's copy says the
// suggestion was, and provenance shows them.
func TestPendingSuggestionsBlockApprovalAndDecisionsAreAudited(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc(sixSections...)
	const bobText, carolText = " Staff must also lock screens.", " Contractors too."
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) {
		suggest(frag, txn, 2, "0b7c1a52-7d7b-4c1e-9d44-1b0f3c0e6a01", "bob", bobText)
		suggest(frag, txn, 1, "0b7c1a52-7d7b-4c1e-9d44-1b0f3c0e6a02", "carol", carolText)
	})

	st, _, err := f.studio.State(doc.ID, Identity{Username: "alice", Admin: true, CanReadPolicies: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.PendingTotal != 2 || !hasRule(st.Findings, policydocs.RulePendingSuggestions) {
		t.Fatalf("pending=%d findings=%+v, want 2 pending and a pending_suggestions finding", st.PendingTotal, st.Findings)
	}
	rows := f.sections(doc.ID)
	if strings.Contains(rows[2].Body, "lock screens") {
		t.Fatalf("the baseline must leave a pending insertion out: %q", rows[2].Body)
	}

	if _, err := f.policies.SubmitForReview(doc.ID); err != nil {
		t.Fatal(err)
	}
	st, _, _ = f.studio.State(doc.ID, Identity{Username: "alice", Admin: true, CanReadPolicies: true})
	if !st.CanEdit || !st.SuggestOnly || !st.CanDecide {
		t.Fatalf("in review an admin suggests and decides: %+v", st)
	}
	_, err = f.policies.Approve(doc.ID, "rev", "")
	var approval policydocs.ApprovalError
	if !errors.As(err, &approval) || !hasRule(approval.Findings, policydocs.RulePendingSuggestions) {
		t.Fatalf("approval with pending suggestions: %v", err)
	}
	f.openRoom(doc.ID)

	if _, err := f.studio.Decide(doc.ID, DecisionRequest{Decision: "maybe", SuggestionIDs: []string{"x"}}, "alice"); !policydocs.IsValidation(err) {
		t.Fatalf("an unknown decision: %v", err)
	}
	var conflict ErrConflict
	if _, err := f.studio.Decide(doc.ID, DecisionRequest{Decision: "accept", SuggestionIDs: []string{"not-pending"}}, "alice"); !errors.As(err, &conflict) {
		t.Fatalf("deciding a suggestion that is not pending: %v", err)
	}
	decided, err := f.studio.Decide(doc.ID, DecisionRequest{Decision: "accept", SuggestionIDs: []string{"0b7c1a52-7d7b-4c1e-9d44-1b0f3c0e6a01"}}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(decided) != 1 || decided[0].Inserted != bobText || decided[0].AuthorName != "Bob" || decided[0].SectionUID != rows[2].UID || len(decided[0].BlockIDs) != 1 {
		t.Fatalf("decision detail comes from the server's copy: %+v", decided)
	}
	// The editor applies the decision to the shared text: accepting keeps the
	// text without its mark (written here as plain text), rejecting drops it.
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) {
		dropText(frag, txn, 2, len([]rune(bobText)))
		typeInto(sectionEl(frag, 2), txn, bobText)
	})
	if _, err := f.studio.Decide(doc.ID, DecisionRequest{Decision: "reject", SuggestionIDs: []string{"0b7c1a52-7d7b-4c1e-9d44-1b0f3c0e6a02"}}, "dave"); err != nil {
		t.Fatal(err)
	}
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) { dropText(frag, txn, 1, len([]rune(carolText))) })

	// In review the projection follows the decided text, so approval takes it.
	rows = f.sections(doc.ID)
	if !strings.Contains(rows[2].Body, "lock screens") || strings.Contains(rows[1].Body, "Contractors") {
		t.Fatalf("the accepted text is in, the rejected text out: %q / %q", rows[2].Body, rows[1].Body)
	}
	approved, err := f.policies.Approve(doc.ID, "rev", "")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != policydocs.StatusApproved {
		t.Fatalf("status %s", approved.Status)
	}

	prov, err := f.studio.Provenance(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var recs []DecisionRecord
	for _, p := range prov {
		recs = append(recs, p.Decisions...)
		if p.OriginLabel == "" {
			t.Errorf("section %s has no origin", p.UID)
		}
	}
	if len(recs) != 2 {
		t.Fatalf("provenance decisions: %+v", recs)
	}
	for _, r := range recs {
		switch r.AuthorName {
		case "Bob":
			if r.Decision != "accepted" || r.DecidedBy != "alice" || r.DecidedAt == "" {
				t.Errorf("Bob's suggestion: %+v", r)
			}
		case "Carol":
			if r.Decision != "rejected" || r.DecidedBy != "dave" {
				t.Errorf("Carol's suggestion: %+v", r)
			}
		default:
			t.Errorf("unexpected record %+v", r)
		}
	}

	if _, err := f.studio.Decide(doc.ID, DecisionRequest{Decision: "accept", SuggestionIDs: []string{"x"}}, "alice"); !policydocs.IsValidation(err) {
		t.Fatalf("an approved document takes no decisions: %v", err)
	}
}

func anchor(b ...byte) string { return base64.StdEncoding.EncodeToString(b) }

// Internal threads are withheld from the shared audience in the query itself.
func TestCommentThreads(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc(sixSections...)
	uid := f.sections(doc.ID)[0].UID
	good := NewThread{SectionUID: uid, AnchorStart: anchor(1, 2, 3), AnchorEnd: anchor(1, 2, 9), Quote: "protects its information", Body: "INTERNAL-MARKER: check with legal"}

	for name, in := range map[string]NewThread{
		"empty body":      {SectionUID: uid, AnchorStart: good.AnchorStart, AnchorEnd: good.AnchorEnd, Body: "  "},
		"bad anchor":      {SectionUID: uid, AnchorStart: "not base64!", AnchorEnd: good.AnchorEnd, Body: "x"},
		"huge anchor":     {SectionUID: uid, AnchorStart: anchor(make([]byte, 600)...), AnchorEnd: good.AnchorEnd, Body: "x"},
		"unknown section": {SectionUID: "nope", AnchorStart: good.AnchorStart, AnchorEnd: good.AnchorEnd, Body: "x"},
		"bad visibility":  {SectionUID: uid, AnchorStart: good.AnchorStart, AnchorEnd: good.AnchorEnd, Body: "x", Visibility: "public"},
		"too long":        {SectionUID: uid, AnchorStart: good.AnchorStart, AnchorEnd: good.AnchorEnd, Body: strings.Repeat("x", maxCommentRunes+1)},
	} {
		if _, err := f.studio.CreateThread(doc.ID, in, "alice"); !policydocs.IsValidation(err) {
			t.Errorf("%s: %v", name, err)
		}
	}

	internal, err := f.studio.CreateThread(doc.ID, good, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if internal.Visibility != VisibilityInternal || internal.Status != ThreadOpen || len(internal.Comments) != 1 || internal.Comments[0].Author != "alice" {
		t.Fatalf("new thread: %+v", internal)
	}
	shared := good
	shared.Visibility, shared.Body = VisibilityShared, "Shared with the client"
	if _, err := f.studio.CreateThread(doc.ID, shared, "alice"); err != nil {
		t.Fatal(err)
	}

	all, _ := f.studio.Threads(doc.ID, AudienceInternal)
	guestView, _ := f.studio.Threads(doc.ID, AudienceShared)
	if len(all) != 2 || len(guestView) != 1 || guestView[0].Visibility != VisibilityShared {
		t.Fatalf("internal sees %d, shared sees %d", len(all), len(guestView))
	}
	for _, th := range guestView {
		for _, c := range th.Comments {
			if strings.Contains(c.Body, "INTERNAL-MARKER") {
				t.Fatal("an internal comment reached the shared audience")
			}
		}
	}

	resolved, err := f.studio.SetThreadStatus(doc.ID, internal.ID, ThreadResolved, "bob")
	if err != nil || resolved.Status != ThreadResolved || resolved.ResolvedBy != "bob" {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	reopened, err := f.studio.Reply(doc.ID, internal.ID, "Still open: legal has not answered.", "carol")
	if err != nil || reopened.Status != ThreadOpen || len(reopened.Comments) != 2 || reopened.ResolvedBy != "" {
		t.Fatalf("a reply reopens: %+v %v", reopened, err)
	}
	if _, err := f.studio.Reply(doc.ID, 9999, "x", "carol"); !errors.Is(err, policydocs.ErrNotFound) {
		t.Fatalf("reply to a missing thread: %v", err)
	}
	other := f.studioDoc(sixSections...)
	if _, err := f.studio.Reply(other.ID, internal.ID, "x", "carol"); !errors.Is(err, policydocs.ErrNotFound) {
		t.Fatalf("a thread is reached only through its own document: %v", err)
	}

	events, err := f.studio.auditEvents(doc.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("audit: %d events, %v", len(events), err)
	}

	if _, err := f.conn.Exec(`UPDATE policy_documents SET status = 'approved' WHERE id = ?`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.studio.CreateThread(doc.ID, good, "alice"); !policydocs.IsValidation(err) {
		t.Fatalf("comments on an approved document: %v", err)
	}
}
