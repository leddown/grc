package policyai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"grc/internal/policydocs"
)

// useLetter replaces the fixture's document with a letter.
func (f *fixture) useLetter() {
	f.t.Helper()
	doc, err := f.policies.CreateDocument(policydocs.Document{Title: "Response to the follow-up letter", DocType: policydocs.TypeLetter,
		OwnerRole: "Head of Regulatory Affairs", Author: "alice"})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, s := range []policydocs.Section{
		{SectionKind: policydocs.KindAddressee, Heading: "Addressee", Body: "European Central Bank"},
		{SectionKind: policydocs.KindLetterBody, Heading: "Letter", Body: "The Bank will endeavour to close the finding."},
		{SectionKind: policydocs.KindSignatories, Heading: "Signatories", Body: "A. Signer, Chief Executive Officer"},
	} {
		s.DocumentID = doc.ID
		if _, err := f.policies.CreateSection(s); err != nil {
			f.t.Fatal(err)
		}
	}
	if _, err := f.studio.Migrate(doc.ID, "alice"); err != nil {
		f.t.Fatal(err)
	}
	f.doc = doc
}

// A letter is not held to policy wording and makes no coverage claims: its
// requests carry the letter rules and the letter's reading of the action, and
// any mapping the model proposes anyway is dropped.
func TestLetterRequestsUseTheLetterRules(t *testing.T) {
	f := newFixture(t)
	if _, err := f.engine.Propose(context.Background(), f.doc.ID, Request{Action: "testable"}, "alice"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "information security policy documents") || strings.Contains(f.prompts[0], "formal letters") {
		t.Fatal("a guideline was not sent the policy rules")
	}

	f.useLetter()
	body := f.blockOf("endeavour to close")
	f.replies = []string{answerJSON(t, map[string]any{
		"answer_markdown": "Made the commitment specific.",
		"proposal": map[string]any{
			"summary": "Commitment with an owner and a date",
			"edits": []any{
				edit("replace", body, "will endeavour to close the finding", "will close the finding by [TO DO: the date]", "\"endeavour\" commits to nothing."),
			},
			"control_mappings": []any{
				map[string]any{"section_uid": f.section(1), "control_id": "AC-2", "coverage": "partial", "rationale": "mentions access"},
			},
			"new_facts": []any{},
		},
	})}
	res, err := f.engine.Propose(context.Background(), f.doc.ID, Request{Action: "testable"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	sent := f.prompts[len(f.prompts)-1]
	if !strings.Contains(sent, "formal letters on a client's behalf") || strings.Contains(sent, "information security policy documents") {
		t.Error("a letter was not sent the letter rules")
	}
	if !strings.Contains(sent, letterInstructions["testable"]) || !strings.Contains(sent, "Tier: A letter is formal correspondence") {
		t.Error("a letter was not sent the letter's reading of the action")
	}
	if ok, rejected := byStatus(res); len(ok) != 1 || len(rejected) != 0 {
		t.Errorf("edits ok=%d rejected=%d", len(ok), len(rejected))
	}
	if len(res.Mappings) != 0 {
		t.Errorf("a letter kept proposed mappings: %+v", res.Mappings)
	}

	before := len(f.prompts)
	var r Refusal
	if _, err := f.engine.Propose(context.Background(), f.doc.ID, Request{Action: "map_controls"}, "alice"); !errors.As(err, &r) || len(f.prompts) != before {
		t.Fatalf("map_controls on a letter: %v, %d prompts sent", err, len(f.prompts)-before)
	}
}
