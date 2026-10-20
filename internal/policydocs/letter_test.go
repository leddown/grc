package policydocs

import (
	"errors"
	"strings"
	"testing"
)

// completeLetter creates a letter carrying every part a letter needs to pass
// the approval gate.
func completeLetter(t *testing.T, svc *Service, body string) Document {
	t.Helper()
	doc, err := svc.CreateDocument(Document{
		Title:          "Response to the follow-up letter",
		DocType:        TypeLetter,
		Reference:      "REG-2026-017",
		OwnerRole:      "Head of Regulatory Affairs",
		EffectiveDate:  "2026-10-28",
		Classification: "Confidential",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	bodies := map[string]string{
		KindAddressee:   "European Central Bank\nSonnemannstrasse 20",
		KindLetterBody:  body,
		KindSignatories: "| Name | Function |\n| --- | --- |\n| A. Signer | Chief Executive Officer |",
	}
	for _, kind := range RequiredKinds(TypeLetter) {
		if _, err := svc.CreateSection(Section{DocumentID: doc.ID, SectionKind: kind, Body: bodies[kind]}); err != nil {
			t.Fatalf("CreateSection(%s): %v", kind, err)
		}
	}
	reloaded, err := svc.GetDocument(doc.ID)
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	return reloaded
}

func messages(findings []Finding) string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Message)
	}
	return strings.Join(out, " | ")
}

// A letter is sent once: a review clock would put it on the due-for-review
// list a year later.
func TestLetterHasNoReviewClock(t *testing.T) {
	svc := newTestService(t)
	doc := completeLetter(t, svc, "The Bank will close finding F-3 by 31 March 2027.")
	if doc.NextReviewDate != "" {
		t.Errorf("a letter got a next review date: %q", doc.NextReviewDate)
	}
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	approved, err := svc.Approve(doc.ID, "Chief Executive Officer", "")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.NextReviewDate != "" {
		t.Errorf("approval started a review clock on a letter: %q", approved.NextReviewDate)
	}
	versions, err := svc.ListVersions(doc.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("ListVersions: %v (%d)", err, len(versions))
	}
	snap := versions[0].Snapshot
	if !strings.Contains(snap, "| Letter Date | 2026-10-28 |") || strings.Contains(snap, "Next Review") {
		t.Errorf("the snapshot should date the letter and carry no review rows:\n%s", snap)
	}
}

func TestLetterLintRequiresItsPartsAndAllowsWill(t *testing.T) {
	doc := Document{DocType: TypeLetter, Title: "Response", EffectiveDate: "2026-10-28"}
	sections := []Section{
		{ID: 1, SectionKind: KindAddressee, Heading: "Addressee", Body: "European Central Bank"},
		{ID: 2, SectionKind: KindLetterBody, Heading: "Letter",
			Body: "The Bank will report progress quarterly. It will endeavour to close the finding in due course."},
	}
	findings := Lint(doc, sections, nil)
	all := messages(findings)
	if !strings.Contains(messages(BlockingFindings(findings)), "Letter: Signatories") {
		t.Errorf("a letter without signatories must be blocked: %s", all)
	}
	if strings.Contains(all, `"will be`) || strings.Contains(all, "must/shall") {
		t.Errorf("a letter is not held to policy wording: %s", all)
	}
	if !strings.Contains(all, `"endeavour to" commits to nothing`) || !strings.Contains(all, `"in due course" gives no date`) {
		t.Errorf("hedged commitments should be flagged: %s", all)
	}
}

func TestLintWarnsOnAnUndatedLetter(t *testing.T) {
	sections := []Section{{ID: 1, SectionKind: KindLetterBody, Heading: "Letter", Body: "Dear Ms Doe,"}}
	if !strings.Contains(messages(Lint(Document{DocType: TypeLetter}, sections, nil)), "no date") {
		t.Error("an undated letter should be flagged")
	}
	if strings.Contains(messages(Lint(Document{DocType: TypeLetter, EffectiveDate: "2026-10-28"}, sections, nil)), "no date") {
		t.Error("a dated letter was flagged as undated")
	}
}

// A template's [TO DO] is what only a person can supply; one left in the text
// must not reach a supervisor, whatever the document type.
func TestPlaceholderBlocksApproval(t *testing.T) {
	svc := newTestService(t)
	doc := completeLetter(t, svc, `Dear Ms Doe, \[TO DO: the reference and date of the letter answered.\] See [TO DO].`)
	if _, err := svc.SubmitForReview(doc.ID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	_, err := svc.Approve(doc.ID, "Chief Executive Officer", "")
	var approval ApprovalError
	if !errors.As(err, &approval) {
		t.Fatalf("a letter with placeholders was approved: %v", err)
	}
	if !strings.Contains(messages(approval.Findings), "2 [TO DO] placeholder(s) left") {
		t.Errorf("findings = %s", messages(approval.Findings))
	}
}

func TestLetterMakesNoCoverageClaims(t *testing.T) {
	svc, conn := newTestServiceWithDB(t)
	seedControls(t, conn, []any{"AC-1", "Policy and Procedures", "AC", "Control", 1, 1, 1, 0})

	letter := completeLetter(t, svc, "The Bank will report quarterly.")
	body := firstSectionOfKind(t, svc, letter.ID, KindLetterBody)
	if _, err := svc.AttachControl(letter.ID, body.ID, ControlRef{ControlID: "AC-1", Coverage: CoveragePartial}); !IsValidation(err) {
		t.Fatalf("a letter accepted a control claim: %v", err)
	}

	// A policy with a claim, re-typed as a letter, stops counting and says so.
	doc := completePolicy(t, svc, "Access Control Policy")
	sec := firstSectionOfKind(t, svc, doc.ID, KindStatements)
	if _, err := svc.AttachControl(doc.ID, sec.ID, ControlRef{ControlID: "AC-1"}); err != nil {
		t.Fatalf("AttachControl: %v", err)
	}
	if before, _ := svc.Coverage(CoverageFilter{Baseline: "low"}); before.DraftOnly != 1 {
		t.Fatalf("expected AC-1 claimed by the draft policy, got %+v", before)
	}
	doc.DocType = TypeLetter
	if _, err := svc.UpdateDocument(doc.ID, doc); err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	after, err := svc.Coverage(CoverageFilter{Baseline: "low"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if after.DraftOnly != 0 || after.Uncovered != 1 {
		t.Errorf("a letter's claims must not count: draft_only=%d uncovered=%d", after.DraftOnly, after.Uncovered)
	}
	findings, err := svc.Lint(doc.ID)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if !strings.Contains(messages(findings), "a letter makes no coverage claims") {
		t.Errorf("findings = %s", messages(findings))
	}
}
