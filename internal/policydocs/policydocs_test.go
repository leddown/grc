package policydocs

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"carelockconsulting/internal/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policydocs_test.db")
	sqliteDB, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })
	return NewService(NewSQLiteRepository(sqliteDB))
}

// completePolicy creates a policy carrying every section a policy needs to pass
// the approval gate, so tests about other behaviour aren't fighting the linter.
func completePolicy(t *testing.T, svc *Service, title string) Document {
	t.Helper()
	doc, err := svc.CreateDocument(Document{
		Title:         title,
		DocType:       TypePolicy,
		Reference:     "POL-AC-001",
		OwnerRole:     "Head of Security",
		Approver:      "CISO",
		EffectiveDate: "2026-01-01",
		Frameworks:    []string{"ISO 27001", "NIST 800-53"},
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	bodies := map[string]string{
		KindPurpose:              "Establish the access control requirements for the ISMS.",
		KindScope:                "All production systems and the personnel who administer them.",
		KindStatements:           "Access must be granted on a least-privilege basis and shall be reviewed quarterly.",
		KindRoles:                "The Head of Security must maintain this policy.",
		KindManagementCommitment: "Management commits to resourcing this policy.",
		KindCompliance:           "Violations must be handled under the disciplinary process.",
	}
	for _, kind := range RequiredKinds(TypePolicy) {
		if _, err := svc.CreateSection(Section{
			DocumentID:  doc.ID,
			SectionKind: kind,
			Body:        bodies[kind],
		}); err != nil {
			t.Fatalf("CreateSection(%s): %v", kind, err)
		}
	}

	reloaded, err := svc.GetDocument(doc.ID)
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	return reloaded
}

func TestCreateDocumentDefaultsAndReviewDate(t *testing.T) {
	svc := newTestService(t)

	doc, err := svc.CreateDocument(Document{Title: "Access Control Policy", EffectiveDate: "2026-01-15"})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	if doc.DocType != TypePolicy {
		t.Errorf("doc_type default = %q, want %q", doc.DocType, TypePolicy)
	}
	if doc.Status != StatusDraft {
		t.Errorf("new document status = %q, want draft", doc.Status)
	}
	if doc.ReviewCadenceMonths != 12 {
		t.Errorf("cadence default = %d, want 12", doc.ReviewCadenceMonths)
	}
	// The review clock runs from the effective date, not from creation.
	if doc.NextReviewDate != "2027-01-15" {
		t.Errorf("next_review_date = %q, want 2027-01-15", doc.NextReviewDate)
	}
}

// A caller must not be able to skip review by posting status: "approved".
func TestCreateIgnoresCallerSuppliedStatus(t *testing.T) {
	svc := newTestService(t)

	doc, err := svc.CreateDocument(Document{Title: "Sneaky", Status: StatusApproved})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if doc.Status != StatusDraft {
		t.Errorf("status = %q, want draft — approval must go through the gate", doc.Status)
	}
}

func TestApprovalRequiresReviewFirst(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	if _, err := svc.Approve(doc.ID, "CISO", ""); !IsValidation(err) {
		t.Fatalf("approving a draft should be refused, got %v", err)
	}
}

func TestApprovalGateBlocksMissingSections(t *testing.T) {
	svc := newTestService(t)

	doc, err := svc.CreateDocument(Document{
		Title: "Thin Policy", OwnerRole: "Head of Security", EffectiveDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if _, err := svc.CreateSection(Section{DocumentID: doc.ID, SectionKind: KindPurpose, Body: "Some purpose."}); err != nil {
		t.Fatalf("CreateSection: %v", err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}

	_, err = svc.Approve(doc.ID, "CISO", "")
	if !IsApproval(err) {
		t.Fatalf("expected ApprovalError, got %v", err)
	}
	var approval ApprovalError
	if !errors.As(err, &approval) || len(approval.Findings) == 0 {
		t.Fatal("ApprovalError should carry the blocking findings")
	}
	for _, f := range approval.Findings {
		if f.Severity != SeverityError {
			t.Errorf("blocking findings must all be errors, got %q", f.Severity)
		}
	}
}

// An unfilled client fact must stop the document rather than shipping a
// placeholder to a client. This is the check phase 4's AI drafting relies on.
func TestApprovalGateBlocksUnresolvedPlaceholder(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	sections, err := svc.ListSections(doc.ID)
	if err != nil {
		t.Fatalf("ListSections: %v", err)
	}
	var statements Section
	for _, s := range sections {
		if s.SectionKind == KindStatements {
			statements = s
		}
	}
	statements.Body = "Records must be retained for [[UNRESOLVED: retention_period]]."
	if _, err := svc.UpdateSection(doc.ID, statements.ID, statements); err != nil {
		t.Fatalf("UpdateSection: %v", err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}

	_, err = svc.Approve(doc.ID, "CISO", "")
	if !IsApproval(err) {
		t.Fatalf("expected the placeholder to block approval, got %v", err)
	}
}

func TestApproveSnapshotsAndLocksSections(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	approved, err := svc.Approve(doc.ID, "CISO", "Initial issue")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Fatalf("status = %q, want approved", approved.Status)
	}

	versions, err := svc.ListVersions(doc.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 || versions[0].VersionLabel != "v1.0" {
		t.Fatalf("expected one v1.0 version, got %+v", versions)
	}
	if !strings.Contains(versions[0].Snapshot, "least-privilege") {
		t.Error("snapshot should contain the approved policy text")
	}
	// The snapshot has to be self-contained: control metadata, not just prose.
	if !strings.Contains(versions[0].Snapshot, "Head of Security") {
		t.Error("snapshot should carry the document control block")
	}

	// Editing approved text in place would change what was approved.
	sections, err := svc.ListSections(doc.ID)
	if err != nil {
		t.Fatalf("ListSections: %v", err)
	}
	if _, err := svc.UpdateSection(doc.ID, sections[0].ID, sections[0]); !IsValidation(err) {
		t.Fatalf("editing an approved document should be refused, got %v", err)
	}

	// Reopening keeps the snapshot and re-enables editing.
	if _, err := svc.ReturnToDraft(doc.ID); err != nil {
		t.Fatalf("ReturnToDraft: %v", err)
	}
	if _, err := svc.UpdateSection(doc.ID, sections[0].ID, sections[0]); err != nil {
		t.Fatalf("editing after reopen: %v", err)
	}
	if versions, _ := svc.ListVersions(doc.ID); len(versions) != 1 {
		t.Error("reopening must not discard the approved version")
	}
}

func TestSecondApprovalIncrementsVersion(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	for i, want := range []string{"v1.0", "v2.0"} {
		if _, err := svc.SubmitForReview(doc.ID); err != nil {
			t.Fatalf("round %d SubmitForReview: %v", i, err)
		}
		if _, err := svc.Approve(doc.ID, "CISO", ""); err != nil {
			t.Fatalf("round %d Approve: %v", i, err)
		}
		versions, err := svc.ListVersions(doc.ID)
		if err != nil {
			t.Fatalf("ListVersions: %v", err)
		}
		if versions[0].VersionLabel != want {
			t.Fatalf("round %d latest label = %q, want %q", i, versions[0].VersionLabel, want)
		}
		if _, err := svc.ReturnToDraft(doc.ID); err != nil {
			t.Fatalf("round %d ReturnToDraft: %v", i, err)
		}
	}
}

func TestIllegalTransitionsRefused(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	// draft -> retired is not an edge; only approved documents get retired.
	if _, err := svc.Retire(doc.ID); !IsValidation(err) {
		t.Errorf("retiring a draft should be refused, got %v", err)
	}
	if CanTransition(StatusDraft, StatusApproved) {
		t.Error("draft -> approved must not be a legal edge; review is not skippable")
	}
	if !CanTransition(StatusApproved, StatusDraft) {
		t.Error("approved -> draft must be legal so documents can be revised")
	}
}

// The hierarchy only runs one way: a policy hanging off a work instruction is
// how document sets become unnavigable.
func TestParentMustSitHigherInHierarchy(t *testing.T) {
	svc := newTestService(t)

	policy, err := svc.CreateDocument(Document{Title: "Access Control Policy", DocType: TypePolicy})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	procedure, err := svc.CreateDocument(Document{
		Title: "Account Provisioning Procedure", DocType: TypeProcedure, ParentDocumentID: policy.ID,
	})
	if err != nil {
		t.Fatalf("procedure under policy should be allowed: %v", err)
	}

	policy.ParentDocumentID = procedure.ID
	if _, err := svc.UpdateDocument(policy.ID, policy); !IsValidation(err) {
		t.Fatalf("policy under procedure should be refused, got %v", err)
	}

	self := procedure
	self.ParentDocumentID = procedure.ID
	if _, err := svc.UpdateDocument(procedure.ID, self); !IsValidation(err) {
		t.Fatalf("self-parenting should be refused, got %v", err)
	}
}

func TestDeleteRefusedForParentAndApprovedDocuments(t *testing.T) {
	svc := newTestService(t)

	policy, err := svc.CreateDocument(Document{Title: "Parent Policy", DocType: TypePolicy})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if _, err := svc.CreateDocument(Document{
		Title: "Child Standard", DocType: TypeStandard, ParentDocumentID: policy.ID,
	}); err != nil {
		t.Fatalf("CreateDocument child: %v", err)
	}
	if err := svc.DeleteDocument(policy.ID); !IsValidation(err) {
		t.Fatalf("deleting a parent should be refused, got %v", err)
	}

	approved := completePolicy(t, svc, "Approved Policy")
	if _, err := svc.SubmitForReview(approved.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(approved.ID, "CISO", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := svc.DeleteDocument(approved.ID); !IsValidation(err) {
		t.Fatalf("deleting a document with approved versions should be refused, got %v", err)
	}
}

func TestReorderSectionsRequiresCompleteSet(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	sections, err := svc.ListSections(doc.ID)
	if err != nil {
		t.Fatalf("ListSections: %v", err)
	}
	ids := make([]int64, len(sections))
	for i, s := range sections {
		ids[i] = s.ID
	}

	if _, err := svc.ReorderSections(doc.ID, ids[:len(ids)-1]); !IsValidation(err) {
		t.Fatalf("a partial reorder should be refused, got %v", err)
	}
	dup := append([]int64{}, ids...)
	dup[1] = dup[0]
	if _, err := svc.ReorderSections(doc.ID, dup); !IsValidation(err) {
		t.Fatalf("a reorder listing a section twice should be refused, got %v", err)
	}

	reversed := make([]int64, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}
	got, err := svc.ReorderSections(doc.ID, reversed)
	if err != nil {
		t.Fatalf("ReorderSections: %v", err)
	}
	for i, s := range got {
		if s.ID != reversed[i] {
			t.Fatalf("section %d = %d, want %d", i, s.ID, reversed[i])
		}
		if s.Ordinal != i {
			t.Errorf("ordinal %d = %d, want dense ordering", i, s.Ordinal)
		}
	}
}

// A section belongs to exactly one document; guessing an id in the URL must not
// reach a section under a different document.
func TestSectionMutationsAreScopedToTheirDocument(t *testing.T) {
	svc := newTestService(t)
	a := completePolicy(t, svc, "Policy A")
	b, err := svc.CreateDocument(Document{Title: "Policy B", DocType: TypePolicy})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	sections, err := svc.ListSections(a.ID)
	if err != nil {
		t.Fatalf("ListSections: %v", err)
	}
	if _, err := svc.UpdateSection(b.ID, sections[0].ID, sections[0]); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-document update should be ErrNotFound, got %v", err)
	}
	if err := svc.DeleteSection(b.ID, sections[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-document delete should be ErrNotFound, got %v", err)
	}
}

func TestLintFlagsNonTestableLanguage(t *testing.T) {
	doc := Document{DocType: TypeStandard, Title: "Encryption Standard"}
	sections := []Section{
		{ID: 1, SectionKind: KindPurpose, Heading: "Purpose", Body: "Define encryption requirements."},
		{ID: 2, SectionKind: KindScope, Heading: "Scope", Body: "All systems."},
		{ID: 3, SectionKind: KindStatements, Heading: "Statements",
			Body: "The organization will endeavour to encrypt data where possible."},
	}

	findings := Lint(doc, sections, nil)
	if len(BlockingFindings(findings)) != 0 {
		t.Errorf("weak wording should warn, not block: %+v", BlockingFindings(findings))
	}

	var sawEndeavour, sawWherePossible, sawNoNormative bool
	for _, f := range findings {
		switch {
		case strings.Contains(f.Message, "endeavour"):
			sawEndeavour = true
		case strings.Contains(f.Message, "where possible"):
			sawWherePossible = true
		case strings.Contains(f.Message, "must/shall/should/may"):
			sawNoNormative = true
		}
	}
	if !sawEndeavour || !sawWherePossible {
		t.Errorf("expected weak-phrase findings, got %+v", findings)
	}
	if !sawNoNormative {
		t.Error("a statements section with no normative verb should be flagged")
	}
}

func TestLintAcceptsWellFormedPolicy(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	findings, err := svc.Lint(doc.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if blocking := BlockingFindings(findings); len(blocking) != 0 {
		t.Errorf("a complete policy should not be blocked: %+v", blocking)
	}
}

func TestExportsCarryControlBlock(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy")

	_, md, err := svc.ExportMarkdown(doc.ID)
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	for _, want := range []string{"# Access Control Policy", "## Document Control", "POL-AC-001", "ISO 27001"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown export missing %q", want)
		}
	}

	_, page, err := svc.ExportHTML(doc.ID)
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	if !strings.Contains(page, "<!doctype html>") || !strings.Contains(page, "Document Control") {
		t.Error("HTML export should be a standalone page with the control block")
	}
}

// Section bodies are user input rendered into a page; a body carrying markup
// must not become live HTML.
func TestExportHTMLEscapesSectionBodies(t *testing.T) {
	svc := newTestService(t)
	doc, err := svc.CreateDocument(Document{Title: "XSS <script>alert(1)</script>", DocType: TypePolicy})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if _, err := svc.CreateSection(Section{
		DocumentID: doc.ID, Heading: "Purpose", SectionKind: KindPurpose,
		Body: `<img src=x onerror="alert(1)">`,
	}); err != nil {
		t.Fatalf("CreateSection: %v", err)
	}

	_, page, err := svc.ExportHTML(doc.ID)
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	if strings.Contains(page, "<script>alert(1)</script>") || strings.Contains(page, `<img src=x`) {
		t.Fatal("export must escape section bodies and titles")
	}
	if !strings.Contains(page, "&lt;img src=x") {
		t.Error("expected the escaped form of the section body")
	}
}

func TestFilters(t *testing.T) {
	svc := newTestService(t)
	completePolicy(t, svc, "Access Control Policy")
	if _, err := svc.CreateDocument(Document{
		Title: "Encryption Standard", DocType: TypeStandard, Frameworks: []string{"PCI DSS"},
	}); err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	byType, err := svc.ListDocuments(Filter{DocType: TypeStandard})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(byType) != 1 || byType[0].Title != "Encryption Standard" {
		t.Errorf("doc_type filter = %+v", byType)
	}

	byFramework, err := svc.ListDocuments(Filter{Framework: "ISO 27001"})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(byFramework) != 1 || byFramework[0].Title != "Access Control Policy" {
		t.Errorf("framework filter = %+v", byFramework)
	}

	bySearch, err := svc.ListDocuments(Filter{Search: "encryption"})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(bySearch) != 1 {
		t.Errorf("search filter = %+v", bySearch)
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	svc := newTestService(t)

	cases := map[string]Document{
		"empty title":       {Title: "   "},
		"unknown type":      {Title: "X", DocType: "manifesto"},
		"bad date":          {Title: "X", EffectiveDate: "15/01/2026"},
		"cadence too large": {Title: "X", ReviewCadenceMonths: 999},
		"missing parent":    {Title: "X", DocType: TypeStandard, ParentDocumentID: 4242},
	}
	for name, doc := range cases {
		if _, err := svc.CreateDocument(doc); !IsValidation(err) {
			t.Errorf("%s: expected validation error, got %v", name, err)
		}
	}
}

// ---- Phase 2: control mapping and coverage ----

// seedControls inserts catalog rows directly. The policydocs tests must not
// depend on the real seed data, which changes when the JSON catalog is updated.
func seedControls(t *testing.T, conn *db.Conn, rows ...[]any) {
	t.Helper()
	for _, r := range rows {
		if _, err := conn.Exec(`INSERT INTO rcsa_controls
			(control_id, name, family, control_type, in_low, in_moderate, in_high, in_privacy,
			 mapping_baselines_json, threats_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, '[]', '[]')`, r...); err != nil {
			t.Fatalf("seed control %v: %v", r[0], err)
		}
	}
}

// newTestServiceWithDB returns the service plus the connection, so a test can
// seed the control catalog the mappings point at.
func newTestServiceWithDB(t *testing.T) (*Service, *db.Conn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policydocs_cov.db")
	conn, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewService(NewSQLiteRepository(conn)), conn
}

func firstSectionOfKind(t *testing.T, svc *Service, docID int64, kind string) Section {
	t.Helper()
	sections, err := svc.ListSections(docID)
	if err != nil {
		t.Fatalf("ListSections: %v", err)
	}
	for _, s := range sections {
		if s.SectionKind == kind {
			return s
		}
	}
	t.Fatalf("no %s section on document %d", kind, docID)
	return Section{}
}

func TestAttachControlValidatesAgainstCatalog(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)

	// A control the catalog does not have is a typo, not a forward reference.
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "ZZ-99"}); !IsValidation(err) {
		t.Fatalf("unknown control should be refused, got %v", err)
	}
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-2", Coverage: "mostly"}); !IsValidation(err) {
		t.Fatalf("unknown coverage level should be refused, got %v", err)
	}

	ref, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "ac-2"})
	if err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if ref.ControlID != "AC-2" {
		t.Errorf("control id = %q, want the catalog's canonical AC-2", ref.ControlID)
	}
	if ref.Coverage != CoverageFull {
		t.Errorf("coverage default = %q, want full", ref.Coverage)
	}
	if !ref.Known {
		t.Error("a freshly attached ref should resolve in the catalog")
	}

	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-2"}); !IsValidation(err) {
		t.Fatalf("duplicate mapping should be refused, got %v", err)
	}
}

// A coverage claim is a compliance assertion, so changing it costs a revision
// cycle exactly like changing the text does.
func TestMappingRequiresDraft(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	ref, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-1"})
	if err != nil {
		t.Fatalf("AttachControl: %v", err)
	}

	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "CISO", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-1"}); !IsValidation(err) {
		t.Errorf("mapping an approved document should be refused, got %v", err)
	}
	if err := svc.DetachControl(doc.ID, sec.ID, ref.ID); !IsValidation(err) {
		t.Errorf("unmapping an approved document should be refused, got %v", err)
	}
}

func TestMappingScopedToItsSection(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Policy A")
	statements := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	purpose := firstSectionOfKind(t, svc, doc.ID, KindPurpose)

	ref, err := svc.AttachControl(doc.ID, statements.ID, ControlRef{ControlID: "AC-2"})
	if err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if err := svc.DetachControl(doc.ID, purpose.ID, ref.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("detaching via the wrong section should be ErrNotFound, got %v", err)
	}
}

// Deleting a section must take its coverage claims with it, or the coverage
// report keeps counting text that no longer exists.
func TestDeletingSectionRemovesItsMappings(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-2"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if err := svc.DeleteSection(doc.ID, sec.ID); err != nil {
		t.Fatalf("DeleteSection: %v", err)
	}

	refs, err := svc.ListControlRefsForDocument(doc.ID)
	if err != nil {
		t.Fatalf("ListControlRefsForDocument: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("section delete should cascade its mappings, got %+v", refs)
	}
}

// The distinction the whole report exists for: a control claimed only by a
// draft reads as covered in a spreadsheet and is worth nothing in an audit.
func TestCoverageSeparatesApprovedFromDraftOnly(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn,
		[]any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0},
		[]any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0},
		[]any{"AC-3", "Access Enforcement", "AC", "Control", 0, 1, 1, 0},
	)

	approved := completePolicy(t, svc, "Approved Policy")
	sec := firstSectionOfKind(t, svc, approved.ID, KindStatements)
	if _, err := svc.AttachControl(approved.ID, sec.ID, ControlRef{ControlID: "AC-1"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if _, err := svc.SubmitForReview(approved.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(approved.ID, "CISO", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	draft := completePolicy(t, svc, "Draft Policy")
	draftSec := firstSectionOfKind(t, svc, draft.ID, KindStatements)
	if _, err := svc.AttachControl(draft.ID, draftSec.ID, ControlRef{ControlID: "AC-2"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}

	report, err := svc.Coverage(CoverageFilter{Baseline: "moderate"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if report.TotalControls != 3 {
		t.Fatalf("total = %d, want 3", report.TotalControls)
	}
	if report.Approved != 1 || report.DraftOnly != 1 || report.Uncovered != 1 {
		t.Errorf("approved/draft/uncovered = %d/%d/%d, want 1/1/1",
			report.Approved, report.DraftOnly, report.Uncovered)
	}

	byID := map[string]CoverageRow{}
	for _, row := range report.Rows {
		byID[row.ControlID] = row
	}
	if byID["AC-1"].Status != CoverageStatusApproved {
		t.Errorf("AC-1 status = %q, want approved", byID["AC-1"].Status)
	}
	if byID["AC-2"].Status != CoverageStatusDraftOnly {
		t.Errorf("AC-2 status = %q, want draft_only", byID["AC-2"].Status)
	}
	if byID["AC-3"].Status != CoverageStatusUncovered {
		t.Errorf("AC-3 status = %q, want uncovered", byID["AC-3"].Status)
	}
	if len(byID["AC-1"].Claims) != 1 || byID["AC-1"].Claims[0].Title != "Approved Policy" {
		t.Errorf("AC-1 claims = %+v", byID["AC-1"].Claims)
	}
}

// "Supporting" contributes context without asserting the control, so a control
// with only supporting claims is not covered by anything.
func TestCoverageTreatsSupportingOnlyAsNotAsserted(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Approved Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{
		ControlID: "AC-1", Coverage: CoverageSupporting,
	}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "CISO", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	report, err := svc.Coverage(CoverageFilter{Baseline: "low"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if report.SupportingOnly != 1 || report.Approved != 0 {
		t.Errorf("supporting/approved = %d/%d, want 1/0", report.SupportingOnly, report.Approved)
	}
}

// A family switched off in Family Filters is out of scope everywhere; counting
// it here would report gaps the practice has already declared irrelevant.
func TestCoverageRespectsFamilyVisibility(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn,
		[]any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0},
		[]any{"PM-1", "Program Plan", "PM", "Control", 1, 1, 1, 0},
	)
	if _, err := conn.Exec(`INSERT INTO control_family_visibility (family, enabled) VALUES ('PM', 0)`); err != nil {
		t.Fatalf("disable family: %v", err)
	}

	report, err := svc.Coverage(CoverageFilter{Baseline: "low"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if report.TotalControls != 1 || report.Rows[0].ControlID != "AC-1" {
		t.Errorf("a disabled family must be out of scope, got %+v", report.Rows)
	}
}

// A claim pointing at a control the catalog no longer has is a finding, not
// something to silently drop.
func TestCoverageReportsOrphanedMappings(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-9", "Retired Control", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-9"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}

	// The catalog is reseeded and the control disappears.
	if _, err := conn.Exec(`DELETE FROM rcsa_controls WHERE control_id = 'AC-9'`); err != nil {
		t.Fatalf("delete control: %v", err)
	}

	report, err := svc.Coverage(CoverageFilter{})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if len(report.Orphans) != 1 || report.Orphans[0].ControlID != "AC-9" {
		t.Fatalf("expected AC-9 reported as an orphan, got %+v", report.Orphans)
	}

	refs, err := svc.ListControlRefsForDocument(doc.ID)
	if err != nil {
		t.Fatalf("ListControlRefsForDocument: %v", err)
	}
	if len(refs) != 1 || refs[0].Known {
		t.Fatalf("the mapping must survive, flagged unknown: %+v", refs)
	}

	findings, err := svc.Lint(doc.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(BlockingFindings(findings)) != 0 {
		t.Error("a stale mapping is a warning, not an approval blocker")
	}
	var warned bool
	for _, f := range findings {
		if strings.Contains(f.Message, "no longer in the control catalog") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a stale-mapping warning, got %+v", findings)
	}
}

func TestCoverageExcludesEnhancementsByDefault(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn,
		[]any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0},
		[]any{"AC-2(1)", "Automated Account Management", "AC", "Control Enhancement", 0, 1, 1, 0},
	)

	base, err := svc.Coverage(CoverageFilter{Baseline: "moderate"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if base.TotalControls != 1 {
		t.Errorf("enhancements should be excluded by default, got %d rows", base.TotalControls)
	}

	withEnh, err := svc.Coverage(CoverageFilter{Baseline: "moderate", IncludeEnhancements: true})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if withEnh.TotalControls != 2 {
		t.Errorf("with enhancements = %d rows, want 2", withEnh.TotalControls)
	}
}

func TestCoverageGapsOnlyAndBaselineValidation(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn,
		[]any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0},
		[]any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0},
	)

	doc := completePolicy(t, svc, "Approved Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-1"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "CISO", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	gaps, err := svc.Coverage(CoverageFilter{Baseline: "low", OnlyGaps: true})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if len(gaps.Rows) != 1 || gaps.Rows[0].ControlID != "AC-2" {
		t.Errorf("gaps-only should return just AC-2, got %+v", gaps.Rows)
	}
	// The counts describe the whole scope, not the filtered rows — otherwise
	// "2 of 2 uncovered" would be reported for a set that is half covered.
	if gaps.TotalControls != 2 || gaps.Approved != 1 {
		t.Errorf("counts must cover the full scope: total=%d approved=%d", gaps.TotalControls, gaps.Approved)
	}

	if _, err := svc.Coverage(CoverageFilter{Baseline: "extreme"}); !IsValidation(err) {
		t.Errorf("unknown baseline should be refused, got %v", err)
	}
}

// A retired policy must stop counting as coverage.
func TestCoverageIgnoresRetiredDocuments(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Approved Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-1"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "CISO", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if before, _ := svc.Coverage(CoverageFilter{Baseline: "low"}); before.Approved != 1 {
		t.Fatalf("expected AC-1 covered before retirement")
	}

	if _, err := svc.Retire(doc.ID); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	after, err := svc.Coverage(CoverageFilter{Baseline: "low"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if after.Approved != 0 || after.Uncovered != 1 {
		t.Errorf("a retired policy must stop counting: approved=%d uncovered=%d", after.Approved, after.Uncovered)
	}
}

func TestExportsIncludeControlMapping(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{
		ControlID: "AC-2", Coverage: CoveragePartial,
	}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}

	_, md, err := svc.ExportMarkdown(doc.ID)
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	for _, want := range []string{"## Control Mapping", "AC-2", "Account Management", "Satisfies: AC-2 (partial)"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown export missing %q", want)
		}
	}

	_, page, err := svc.ExportHTML(doc.ID)
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	if !strings.Contains(page, "Control Mapping") || !strings.Contains(page, "AC-2") {
		t.Error("HTML export should carry the control mapping table")
	}
}

// The snapshot has to record the coverage claim as approved, which is what makes
// "mapping requires draft" coherent rather than merely strict.
func TestApprovalSnapshotRecordsMappings(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-1"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "CISO", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	versions, err := svc.ListVersions(doc.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if !strings.Contains(versions[0].Snapshot, "Control Mapping") ||
		!strings.Contains(versions[0].Snapshot, "AC-1") {
		t.Error("the approved snapshot must record the control mappings")
	}
}

func TestLintWarnsWhenFrameworkDeclaredButNothingMapped(t *testing.T) {
	svc := newTestService(t)
	doc := completePolicy(t, svc, "Access Control Policy") // declares ISO 27001 + NIST 800-53

	findings, err := svc.Lint(doc.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(BlockingFindings(findings)) != 0 {
		t.Error("an unmapped policy must not be blocked from approval")
	}
	var warned bool
	for _, f := range findings {
		if strings.Contains(f.Message, "maps no controls") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected an unmapped-framework warning, got %+v", findings)
	}
}

func TestSearchControlsBacksThePicker(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn,
		[]any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0},
		[]any{"SI-4", "System Monitoring", "SI", "Control", 0, 1, 1, 0},
	)

	byID, err := svc.SearchControls("ac-2")
	if err != nil {
		t.Fatalf("SearchControls: %v", err)
	}
	if len(byID) != 1 || byID[0].ControlID != "AC-2" {
		t.Errorf("search by id = %+v", byID)
	}
	if byID[0].Baselines != "Low Moderate High" {
		t.Errorf("baselines = %q, want the flags rendered", byID[0].Baselines)
	}

	byName, err := svc.SearchControls("monitoring")
	if err != nil {
		t.Fatalf("SearchControls: %v", err)
	}
	if len(byName) != 1 || byName[0].ControlID != "SI-4" {
		t.Errorf("search by name = %+v", byName)
	}
}

// ---- Template render payload ----

// The JSON export is a contract with templates/typst/policy-document.typ. If a
// field name drifts, the template renders a blank where that value should be
// and nothing errors, so the field names are pinned here explicitly.
func TestTemplateExportShapeMatchesTheTemplateContract(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-2", "Account Management", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{
		ControlID: "AC-2", Coverage: CoveragePartial,
	}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "CISO", "Initial issue"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	export, err := svc.ExportTemplateJSON(doc.ID)
	if err != nil {
		t.Fatalf("ExportTemplateJSON: %v", err)
	}

	raw, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Every key policy-document.typ reads.
	for _, key := range []string{
		`"reference"`, `"title"`, `"doc_type"`, `"status"`, `"classification"`,
		`"owner_role"`, `"approver"`, `"client_name"`, `"frameworks"`,
		`"effective_date"`, `"review_cadence_months"`, `"next_review_date"`,
		`"summary"`, `"sections"`, `"heading"`, `"body"`, `"section_kind"`,
		`"controls"`, `"control_id"`, `"coverage"`, `"control_name"`,
		`"section_heading"`, `"versions"`, `"version_label"`, `"approved_by"`,
		`"approved_at"`, `"change_summary"`,
	} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("export is missing the template key %s", key)
		}
	}

	if export.Title != "Access Control Policy" || export.Reference != "POL-AC-001" {
		t.Errorf("header fields wrong: %+v", export)
	}
	if len(export.Sections) != len(RequiredKinds(TypePolicy)) {
		t.Errorf("sections = %d, want %d", len(export.Sections), len(RequiredKinds(TypePolicy)))
	}
	if len(export.Controls) != 1 || export.Controls[0].ControlID != "AC-2" ||
		export.Controls[0].Coverage != CoveragePartial {
		t.Errorf("controls = %+v", export.Controls)
	}
	if len(export.Versions) != 1 || export.Versions[0].VersionLabel != "v1.0" {
		t.Errorf("versions = %+v", export.Versions)
	}

	// The section carrying the claim must also carry it inline, which is what
	// the template turns into the "Satisfies:" note.
	var statements TemplateSection
	for _, s := range export.Sections {
		if s.SectionKind == KindStatements {
			statements = s
		}
	}
	if len(statements.Controls) != 1 || statements.Controls[0].ControlID != "AC-2" {
		t.Errorf("statements section claims = %+v", statements.Controls)
	}
}

// Typst distinguishes an empty array from null and faults when looping over
// null, so a document with no mappings, frameworks or versions must still
// export arrays rather than nulls.
func TestTemplateExportEmitsEmptyArraysNotNulls(t *testing.T) {
	svc := newTestService(t)
	doc, err := svc.CreateDocument(Document{Title: "Bare Policy", DocType: TypePolicy})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if _, err := svc.CreateSection(Section{
		DocumentID: doc.ID, SectionKind: KindPurpose, Body: "Some purpose.",
	}); err != nil {
		t.Fatalf("CreateSection: %v", err)
	}

	export, err := svc.ExportTemplateJSON(doc.ID)
	if err != nil {
		t.Fatalf("ExportTemplateJSON: %v", err)
	}
	raw, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "null") {
		t.Errorf("export must not contain null: %s", raw)
	}
	if export.Frameworks == nil || export.Controls == nil || export.Versions == nil {
		t.Error("collections must be non-nil")
	}
	if export.Sections[0].Controls == nil {
		t.Error("per-section claims must be non-nil")
	}
}

// A stale mapping is reported in the deliverable rather than silently dropped.
func TestTemplateExportSurfacesStaleMappings(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-9", "Retired", "AC", "Control", 1, 1, 1, 0})

	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-9"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if _, err := conn.Exec(`DELETE FROM rcsa_controls WHERE control_id = 'AC-9'`); err != nil {
		t.Fatalf("delete control: %v", err)
	}

	export, err := svc.ExportTemplateJSON(doc.ID)
	if err != nil {
		t.Fatalf("ExportTemplateJSON: %v", err)
	}
	if len(export.Controls) != 1 || export.Controls[0].ControlName != "(not in catalog)" {
		t.Errorf("stale mapping should be labelled, got %+v", export.Controls)
	}
}

// ---- Separation of duties ----

// Off by default, because the app ships single-user: enforcing it there would
// make every document permanently unapprovable.
func TestSelfApprovalAllowedWhenSeparationIsOff(t *testing.T) {
	svc := newTestService(t)

	doc, err := svc.CreateDocument(Document{
		Title: "Access Control Policy", DocType: TypePolicy, Author: "alice",
		OwnerRole: "Head of Security", EffectiveDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	for _, kind := range RequiredKinds(TypePolicy) {
		if _, err := svc.CreateSection(Section{
			DocumentID: doc.ID, SectionKind: kind,
			Body: "Access must be granted on a least-privilege basis.",
		}); err != nil {
			t.Fatalf("CreateSection(%s): %v", kind, err)
		}
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "alice", ""); err != nil {
		t.Fatalf("self-approval must be allowed with separation off: %v", err)
	}
}

func TestSelfApprovalRefusedWhenSeparationIsOn(t *testing.T) {
	svc := newTestService(t)
	svc.SetRequireSeparateApprover(true)

	doc, err := svc.CreateDocument(Document{
		Title: "Access Control Policy", DocType: TypePolicy, Author: "alice",
		OwnerRole: "Head of Security", EffectiveDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	for _, kind := range RequiredKinds(TypePolicy) {
		if _, err := svc.CreateSection(Section{
			DocumentID: doc.ID, SectionKind: kind,
			Body: "Access must be granted on a least-privilege basis.",
		}); err != nil {
			t.Fatalf("CreateSection(%s): %v", kind, err)
		}
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}

	// Case-insensitive: usernames are not case-sensitive identities here, and a
	// capitalisation difference must not be a way around the control.
	if _, err := svc.Approve(doc.ID, "Alice", ""); !IsValidation(err) {
		t.Fatalf("self-approval must be refused, got %v", err)
	}
	if _, err := svc.Approve(doc.ID, "  alice  ", ""); !IsValidation(err) {
		t.Fatalf("whitespace must not defeat the check, got %v", err)
	}

	// A different reviewer can approve.
	approved, err := svc.Approve(doc.ID, "bob", "Reviewed.")
	if err != nil {
		t.Fatalf("approval by a second person must succeed: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Errorf("status = %q, want approved", approved.Status)
	}
	versions, _ := svc.ListVersions(doc.ID)
	if len(versions) != 1 || versions[0].ApprovedBy != "bob" {
		t.Errorf("version should record the real approver: %+v", versions)
	}
}

// A document created before the author column existed, or under local mode, has
// no author. It must not be blocked by a check it can never satisfy.
func TestSeparationSkippedWhenNoAuthorRecorded(t *testing.T) {
	svc := newTestService(t)
	svc.SetRequireSeparateApprover(true)

	doc := completePolicy(t, svc, "Legacy Policy") // created without an Author
	if doc.Author != "" {
		t.Fatalf("fixture should have no author, got %q", doc.Author)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if _, err := svc.Approve(doc.ID, "anyone", ""); err != nil {
		t.Fatalf("a document with no author must stay approvable: %v", err)
	}
}

// The author is set at creation and must survive edits — an edit that could
// clear or rewrite it would be a way to self-approve.
func TestAuthorIsImmutableAcrossEdits(t *testing.T) {
	svc := newTestService(t)

	doc, err := svc.CreateDocument(Document{
		Title: "Access Control Policy", DocType: TypePolicy, Author: "alice",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if doc.Author != "alice" {
		t.Fatalf("author = %q, want alice", doc.Author)
	}

	doc.Author = "mallory"
	doc.Title = "Renamed Policy"
	updated, err := svc.UpdateDocument(doc.ID, doc)
	if err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	if updated.Author != "alice" {
		t.Errorf("author = %q after edit, want it pinned to alice", updated.Author)
	}
	if updated.Title != "Renamed Policy" {
		t.Errorf("the rest of the edit should still apply, got %q", updated.Title)
	}
}
