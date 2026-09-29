package policystudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reearth/ygo/crdt"
	ygws "github.com/reearth/ygo/provider/websocket"

	"grc/internal/db"
	"grc/internal/policydocs"
)

type studioFixture struct {
	t         *testing.T
	conn      *db.Conn
	policies  *policydocs.Service
	studio    *Service
	projected atomic.Int32
}

// newStudio builds a Studio over a temporary database. Requests identify
// themselves with an X-Test-User header: "admin", "reader", "noaccess".
func newStudio(t *testing.T) *studioFixture {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "studio.db"))
	if err != nil {
		t.Fatal(err)
	}
	f := &studioFixture{t: t, conn: conn, policies: policydocs.NewService(policydocs.NewSQLiteRepository(conn))}
	f.studio = NewService(conn, f.policies, Options{
		AllowedOrigins: []string{"https://policies.example.com"},
		Identity: func(r *http.Request) (Identity, bool) {
			switch r.Header.Get("X-Test-User") {
			case "admin":
				return Identity{Username: "alice", Admin: true, CanReadPolicies: true}, true
			case "reader":
				return Identity{Username: "rita", CanReadPolicies: true}, true
			case "noaccess":
				return Identity{Username: "nobody"}, true
			}
			return Identity{}, false
		},
		OnProjected: func(int64) { f.projected.Add(1) },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.studio.Shutdown(ctx)
		_ = conn.Close()
	})
	return f
}

// legacyDoc creates a Markdown document with the given section bodies.
func (f *studioFixture) legacyDoc(bodies ...string) policydocs.Document {
	f.t.Helper()
	doc, err := f.policies.CreateDocument(policydocs.Document{
		Title: "ICT and Information Security Policy", DocType: policydocs.TypePolicy, OwnerRole: "CISO",
		EffectiveDate: "2026-10-01", Author: "alice",
	})
	if err != nil {
		f.t.Fatal(err)
	}
	kinds := []string{policydocs.KindPurpose, policydocs.KindScope, policydocs.KindStatements, policydocs.KindRoles,
		policydocs.KindManagementCommitment, policydocs.KindCompliance}
	for i, body := range bodies {
		if _, err := f.policies.CreateSection(policydocs.Section{DocumentID: doc.ID, SectionKind: kinds[i%len(kinds)], Body: body}); err != nil {
			f.t.Fatal(err)
		}
	}
	return doc
}

func (f *studioFixture) studioDoc(bodies ...string) policydocs.Document {
	f.t.Helper()
	doc := f.legacyDoc(bodies...)
	if _, err := f.studio.Migrate(doc.ID, "alice"); err != nil {
		f.t.Fatal(err)
	}
	f.openRoom(doc.ID)
	return doc
}

// openRoom loads the document's room, which seeds it on first use.
func (f *studioFixture) openRoom(id int64) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := f.studio.collab.Apply(ctx, RoomName(id), func(*crdt.Doc, func(func(*crdt.Transaction))) {})
	if err != nil && !errors.Is(err, ygws.ErrNoChanges) {
		f.t.Fatal(err)
	}
}

// clientEdit applies an edit the way a connected editor would: as an update
// computed against the current document and merged into the live room.
// Nothing about it is trusted -- which is the point of several tests.
func (f *studioFixture) clientEdit(id int64, edit func(frag *crdt.YXmlFragment, txn *crdt.Transaction)) {
	f.t.Helper()
	data, err := f.studio.currentState(id)
	if err != nil {
		f.t.Fatal(err)
	}
	scratch := crdt.New()
	if err := crdt.ApplyUpdateV1(scratch, data, nil); err != nil {
		f.t.Fatal(err)
	}
	sv, err := crdt.DecodeStateVectorV1(crdt.EncodeStateVectorV1(scratch))
	if err != nil {
		f.t.Fatal(err)
	}
	frag := scratch.GetXmlFragment(Fragment)
	scratch.Transact(func(txn *crdt.Transaction) { edit(frag, txn) })
	diff := crdt.EncodeStateAsUpdateV1(scratch, sv)
	f.openRoom(id)
	live := f.studio.collab.GetDoc(RoomName(id))
	if live == nil {
		f.t.Fatal("room not loaded")
	}
	if err := crdt.ApplyUpdateV1(live, diff, nil); err != nil {
		f.t.Fatal(err)
	}
	if err := f.studio.Project(id); err != nil {
		f.t.Fatal(err)
	}
}

func typeInto(el *crdt.YXmlElement, txn *crdt.Transaction, text string) {
	for _, c := range el.Children() {
		if p, ok := c.(*crdt.YXmlElement); ok && p.NodeName == "paragraph" {
			for _, t := range p.Children() {
				if xt, ok := t.(*crdt.YXmlText); ok {
					xt.Insert(txn, xt.Len(), text, nil)
					return
				}
			}
			xt := crdt.NewYXmlText()
			p.InsertText(txn, 0, xt)
			xt.Insert(txn, 0, text, nil)
			return
		}
	}
}

func sectionEl(frag *crdt.YXmlFragment, i int) *crdt.YXmlElement {
	return frag.Children()[i].(*crdt.YXmlElement)
}

func (f *studioFixture) sections(id int64) []policydocs.Section {
	f.t.Helper()
	rows, err := f.policies.ListAllSections(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return rows
}

func TestMigrateSeedAndProject(t *testing.T) {
	f := newStudio(t)
	doc := f.legacyDoc("Protect the organisation's information.\nSecond line.", "- all staff\n- all contractors")
	before := f.sections(doc.ID)

	migrated, err := f.studio.Migrate(doc.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if migrated.EditorFormat != policydocs.EditorStudio {
		t.Fatalf("editor format %q", migrated.EditorFormat)
	}
	if again, err := f.studio.Migrate(doc.ID, "alice"); err != nil || again.EditorFormat != policydocs.EditorStudio {
		t.Fatalf("migration must be idempotent: %v", err)
	}
	snaps, _ := f.studio.store.ListSnapshots(doc.ID)
	if len(snaps) != 1 || snaps[0].Reason != SnapshotPreMigration {
		t.Fatalf("expected one pre-migration snapshot, got %+v", snaps)
	}

	f.openRoom(doc.ID)
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) {
		typeInto(sectionEl(frag, 0), txn, " Typed live.")
	})
	rows := f.sections(doc.ID)
	if len(rows) != 2 || rows[0].UID != before[0].UID {
		t.Fatalf("rows changed identity: %+v", rows)
	}
	if !strings.Contains(rows[0].Body, "Protect the organisation's information. Typed live.\\\nSecond line.") {
		t.Fatalf("projected body: %q", rows[0].Body)
	}
	if !strings.Contains(rows[1].Body, "- all staff\n- all contractors") {
		t.Fatalf("projected list: %q", rows[1].Body)
	}
	if rows[0].ContentJSON == "" {
		t.Fatal("content_json not stored")
	}
	after, _ := f.policies.GetDocument(doc.ID)
	if after.ProjectionVersion == 0 || f.projected.Load() == 0 {
		t.Fatal("projection version not bumped or knowledge not told")
	}
}

func TestLegacyEndpointsRefuseStudioDocuments(t *testing.T) {
	f := newStudio(t)
	legacy := f.legacyDoc("text")
	studio := f.studioDoc("text")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := policydocs.NewHandler(f.policies, nil)
	h.RegisterReadRoutes(r)
	h.RegisterAdminRoutes(r)

	do := func(method, path, body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	sec := func(d policydocs.Document) string {
		return strconv.FormatInt(f.sections(d.ID)[0].ID, 10)
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/policies/%d/sections/%s", `{"heading":"H","section_kind":"purpose","body":"overwrite"}`},
		{http.MethodPost, "/policies/%d/sections", `{"heading":"H","section_kind":"scope"}`},
		{http.MethodDelete, "/policies/%d/sections/%s", ``},
		{http.MethodPost, "/policies/%d/sections/reorder", `{"section_ids":[]}`},
	} {
		path := func(d policydocs.Document) string {
			p := strings.Replace(tc.path, "%d", strconv.FormatInt(d.ID, 10), 1)
			return strings.Replace(p, "%s", sec(d), 1)
		}
		if got := do(tc.method, path(studio), tc.body); got != http.StatusConflict {
			t.Errorf("%s %s on a Studio document: %d, want 409", tc.method, tc.path, got)
		}
		if got := do(tc.method, path(legacy), tc.body); got == http.StatusConflict {
			t.Errorf("%s %s on a Markdown document must not be refused", tc.method, tc.path)
		}
	}
	// Metadata stays editable through the existing API, and a payload that
	// does not mention the client (the section editor's) keeps it.
	if _, err := f.conn.Exec(`UPDATE policy_documents SET client_profile_id = 7 WHERE id = ?`, studio.ID); err != nil {
		t.Fatal(err)
	}
	if got := do(http.MethodPut, "/policies/"+strconv.FormatInt(studio.ID, 10), `{"title":"Renamed","doc_type":"policy"}`); got != http.StatusOK {
		t.Errorf("metadata update on a Studio document: %d", got)
	}
	if d, _ := f.policies.GetDocument(studio.ID); d.Title != "Renamed" || d.ClientProfileID != 7 {
		t.Errorf("after an update without client_profile_id: title %q, client %d", d.Title, d.ClientProfileID)
	}
}

func TestStructureCommands(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc("one", "two")
	rows := f.sections(doc.ID)

	added, err := f.studio.AddSection(doc.ID, rows[0].UID, policydocs.KindStatements, "Policy statements")
	if err != nil {
		t.Fatal(err)
	}
	rows = f.sections(doc.ID)
	if len(rows) != 3 || rows[1].UID != added.UID || rows[1].Heading != "Policy statements" {
		t.Fatalf("added section not in place: %+v", rows)
	}

	order := []string{rows[2].UID, rows[0].UID, rows[1].UID}
	if err := f.studio.ReorderSections(doc.ID, order); err != nil {
		t.Fatal(err)
	}
	for i, r := range f.sections(doc.ID) {
		if r.UID != order[i] {
			t.Fatalf("order after reorder: %d is %s, want %s", i, r.UID, order[i])
		}
	}
	if err := f.studio.ReorderSections(doc.ID, order[:2]); err == nil {
		t.Fatal("a partial order must be refused")
	}

	if err := f.studio.SetSectionKind(doc.ID, added.UID, policydocs.KindExceptions); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.policies.GetSectionByUID(doc.ID, added.UID); got.SectionKind != policydocs.KindExceptions {
		t.Fatalf("kind not changed: %s", got.SectionKind)
	}
	if err := f.studio.SetSectionKind(doc.ID, added.UID, "shell"); err == nil {
		t.Fatal("an unknown kind must be refused")
	}

	if err := f.studio.DeleteSection(doc.ID, added.UID); err != nil {
		t.Fatal(err)
	}
	if len(f.sections(doc.ID)) != 2 {
		t.Fatal("section not deleted")
	}
	if err := f.studio.DeleteSection(doc.ID, f.sections(doc.ID)[0].UID); err != nil {
		t.Fatal(err)
	}
	if err := f.studio.DeleteSection(doc.ID, f.sections(doc.ID)[0].UID); err == nil {
		t.Fatal("deleting the last section must be refused")
	}
}

// A section node that vanishes from the live document -- a client that got
// past the editor's own filter -- detaches its row; the row and its mappings
// survive until someone restores or deletes it.
func TestVanishedSectionDetachesAndRestores(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc("keep me", "and me")
	rows := f.sections(doc.ID)
	if _, err := f.conn.Exec(`INSERT INTO rcsa_controls (control_id, name, family, control_type, in_low, in_moderate, in_high, in_privacy, mapping_baselines_json, threats_json)
		VALUES ('AC-5', 'Separation of Duties', 'AC', 'control', 0, 1, 1, 0, '[]', '[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.policies.AttachControl(doc.ID, rows[1].ID, policydocs.ControlRef{ControlID: "AC-5", Coverage: policydocs.CoverageSupporting}); err != nil {
		t.Fatal(err)
	}

	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) { frag.Delete(txn, 1, 1) })
	all := f.sections(doc.ID)
	attached, _ := f.policies.ListSections(doc.ID)
	if len(all) != 2 || len(attached) != 1 || all[1].DetachedAt == "" {
		t.Fatalf("expected the second row detached, got %+v", all)
	}
	refs, _ := f.policies.ListControlRefsForDocument(doc.ID)
	if len(refs) != 1 {
		t.Fatal("a detached section's mappings must survive")
	}
	if _, out, _ := f.policies.ExportMarkdown(doc.ID); strings.Contains(out, "and me") {
		t.Fatal("a detached section must not reach exports")
	}

	if err := f.studio.RestoreSection(doc.ID, rows[1].UID); err != nil {
		t.Fatal(err)
	}
	all = f.sections(doc.ID)
	if all[1].DetachedAt != "" || !strings.Contains(all[1].Body, "and me") {
		t.Fatalf("restore did not bring the section back: %+v", all[1])
	}
}

func TestProjectionRefusesWhatTheServerDidNotAllow(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc("original text", "second")
	rows := f.sections(doc.ID)

	// A section the server never created.
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) {
		sec := SectionFromMarkdown("rogue-section", "purpose", "Rogue", "injected", counter())
		el, _ := buildElement(sec)
		frag.InsertElement(txn, frag.Len(), el)
	})
	if len(f.sections(doc.ID)) != 2 {
		t.Fatal("a section the server did not create became a row")
	}

	// Content the schema forbids, inside an existing section.
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) {
		script := crdt.NewYXmlElement("script")
		sectionEl(frag, 0).InsertElement(txn, 1, script)
	})
	after := f.sections(doc.ID)
	if after[0].DetachedAt != "" || !strings.Contains(after[0].Body, "original text") || after[0].Body != rows[0].Body {
		t.Fatalf("the refused section must keep its last good row: %+v", after[0])
	}
	if len(f.studio.Problems(doc.ID)) < 2 {
		t.Fatalf("problems: %+v", f.studio.Problems(doc.ID))
	}

	// And the refusal blocks approval.
	if _, err := f.policies.SubmitForReview(doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.policies.Approve(doc.ID, "bob", ""); !policydocs.IsApproval(err) {
		t.Fatalf("approval with refused content: %v", err)
	}
}

// Rows changed by something other than a projection -- a sync from another
// deployment -- win over the stored Studio document.
func TestRowsChangedOutsideTheStudioWin(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc("studio text")
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) { typeInto(sectionEl(frag, 0), txn, " edited") })
	f.studio.closeRoom(doc.ID)

	row := f.sections(doc.ID)[0]
	synced := SectionFromMarkdown(row.UID, row.SectionKind, row.Heading, "text from the other deployment", counter())
	raw, _ := json.Marshal(synced)
	if _, err := f.conn.Exec(`UPDATE policy_sections SET body = ?, content_json = ? WHERE id = ?`, "text from the other deployment", string(raw), row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(`UPDATE policy_documents SET projection_token = 'from-elsewhere' WHERE id = ?`, doc.ID); err != nil {
		t.Fatal(err)
	}

	f.openRoom(doc.ID)
	data, _ := f.studio.currentState(doc.ID)
	if !bytes.Contains(data, []byte("text from the other deployment")) || bytes.Contains(data, []byte("edited")) {
		t.Fatal("the document was not rebuilt from the changed rows")
	}
	snaps, _ := f.studio.store.ListSnapshots(doc.ID)
	found := false
	for _, s := range snaps {
		found = found || s.Reason == SnapshotSuperseded
	}
	if !found {
		t.Fatal("the replaced document was not kept as a snapshot")
	}
	// And the ordinary restart path does not rebuild.
	f.studio.closeRoom(doc.ID)
	f.openRoom(doc.ID)
	if n := len(snaps); n != len(mustSnapshots(t, f, doc.ID)) {
		t.Fatal("reopening an unchanged document must not rebuild it")
	}
}

func mustSnapshots(t *testing.T, f *studioFixture, id int64) []SnapshotInfo {
	t.Helper()
	s, err := f.studio.store.ListSnapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func req(user, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://grc.example.com/collab/policies/1", nil)
	r.Host = "grc.example.com"
	if user != "" {
		r.Header.Set("X-Test-User", user)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

// The collab authorization decision, by table.
func TestCollabDecision(t *testing.T) {
	f := newStudio(t)
	draft := f.studioDoc("x")
	review := f.studioDoc("y")
	if _, err := f.policies.SubmitForReview(review.ID); err != nil {
		t.Fatal(err)
	}
	approved := f.studioDoc("w")
	if _, err := f.conn.Exec(`UPDATE policy_documents SET status = 'approved' WHERE id = ?`, approved.ID); err != nil {
		t.Fatal(err)
	}
	legacy := f.legacyDoc("z")
	const own = "https://grc.example.com"
	cases := []struct {
		name     string
		r        *http.Request
		doc      int64
		allow    bool
		readOnly bool
	}{
		{"admin, draft", req("admin", own), draft.ID, true, false},
		{"admin, allowlisted external origin", req("admin", "https://policies.example.com"), draft.ID, true, false},
		// In review admins keep writing, as suggestions (the editor's suggest
		// mode); the approval block is what the server guarantees.
		{"admin, in review", req("admin", own), review.ID, true, false},
		{"reader, in review", req("reader", own), review.ID, true, true},
		{"admin, approved", req("admin", own), approved.ID, true, true},
		{"reader, draft", req("reader", own), draft.ID, true, true},
		{"no page access", req("noaccess", own), draft.ID, false, false},
		{"not signed in", req("", own), draft.ID, false, false},
		{"no Origin", req("admin", ""), draft.ID, false, false},
		{"cross origin", req("admin", "https://evil.example"), draft.ID, false, false},
		{"lookalike origin", req("admin", "https://grc.example.com.evil.example"), draft.ID, false, false},
		{"null origin", req("admin", "null"), draft.ID, false, false},
		{"markdown document", req("admin", own), legacy.ID, false, false},
		{"missing document", req("admin", own), 99999, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := f.studio.decide(tc.r, tc.doc)
			if d.Allow != tc.allow || (d.Allow && d.ReadOnly != tc.readOnly) {
				t.Fatalf("decision %+v, want allow=%v readOnly=%v", d, tc.allow, tc.readOnly)
			}
		})
	}
	t.Run("admin, draft, status change in progress", func(t *testing.T) {
		f.studio.lockForTransition(draft.ID)
		defer f.studio.unlockTransition(draft.ID)
		if d := f.studio.decide(req("admin", own), draft.ID); !d.Allow || !d.ReadOnly {
			t.Fatalf("decision %+v, want read-only", d)
		}
	})
}

// Unresolved facts block approval; once the text is fixed, a different user
// approves, the room is read-only, the snapshot matches the projection, and
// the legacy API refuses body edits.
func TestApprovalOfAStudioDocument(t *testing.T) {
	f := newStudio(t)
	f.policies.SetRequireSeparateApprover(true)
	bodies := []string{"Why.", "Retention is [[UNRESOLVED: retention_period]].", "Staff must comply.", "The CISO owns this.", "Management endorses it.", "Breaches are sanctioned."}
	doc := f.studioDoc(bodies...)
	if _, err := f.policies.SubmitForReview(doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.policies.Approve(doc.ID, "bob", ""); !policydocs.IsApproval(err) {
		t.Fatalf("an unresolved fact must block approval, got %v", err)
	}
	if _, err := f.policies.ReturnToDraft(doc.ID); err != nil {
		t.Fatal(err)
	}
	f.openRoom(doc.ID)
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) {
		// Replace the scope paragraph (fact token and all) with plain text.
		sec := sectionEl(frag, 1)
		sec.Delete(txn, 1, 1)
		p := crdt.NewYXmlElement("paragraph")
		sec.InsertElement(txn, 1, p)
		xt := crdt.NewYXmlText()
		p.InsertText(txn, 0, xt)
		xt.Insert(txn, 0, "Retention follows the records schedule.", nil)
	})
	if _, err := f.policies.SubmitForReview(doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.policies.Approve(doc.ID, "alice", ""); err == nil {
		t.Fatal("the author must not approve their own document")
	}
	approved, err := f.policies.Approve(doc.ID, "bob", "first issue")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != policydocs.StatusApproved {
		t.Fatalf("status %s", approved.Status)
	}
	if d := f.studio.decide(req("admin", "https://grc.example.com"), doc.ID); !d.Allow || !d.ReadOnly {
		t.Fatalf("an approved document's room must be read-only: %+v", d)
	}
	versions, _ := f.policies.ListVersions(doc.ID)
	_, md, _ := f.policies.ExportMarkdown(doc.ID)
	for _, row := range f.sections(doc.ID) {
		if len(versions) != 1 || !strings.Contains(versions[0].Snapshot, "## "+row.Heading+"\n\n"+row.Body) {
			t.Fatalf("the approval snapshot does not carry the projected section %q", row.Heading)
		}
	}
	if !strings.Contains(versions[0].Snapshot, "Retention follows the records schedule.") || strings.Contains(versions[0].Snapshot, "UNRESOLVED") {
		t.Fatal("the approval snapshot is not the fixed text")
	}
	// The approval is hashed: it verifies, and a changed snapshot does not.
	if !versions[0].Verify() || versions[0].ContentJSON == "" {
		t.Fatal("the approved version does not verify against its hash")
	}
	tampered := versions[0]
	tampered.Snapshot += " (edited)"
	if tampered.Verify() {
		t.Fatal("a changed snapshot still verifies")
	}
	// A late edit to the approved document's live state is never projected.
	f.clientEdit(doc.ID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) { typeInto(sectionEl(frag, 0), txn, " late") })
	if _, md2, _ := f.policies.ExportMarkdown(doc.ID); md2 != md {
		t.Fatal("rows of an approved document changed")
	}
}
