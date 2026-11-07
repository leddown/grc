package assuranceplan

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"grc/internal/aiprovider"
	"grc/internal/knowledge"
)

// fakeAsker answers from a script, one answer per question, and keeps every
// request so a test can read what was asked.
type fakeAsker struct {
	mu      sync.Mutex
	answers []aiprovider.Response
	err     error
	reqs    []aiprovider.Request
	off     bool
}

func (f *fakeAsker) Available() bool  { return !f.off }
func (f *fakeAsker) Describe() string { return "Wintermute · test-model" }
func (f *fakeAsker) Ask(_ context.Context, req aiprovider.Request) (aiprovider.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return aiprovider.Response{}, f.err
	}
	r := f.answers[min(len(f.reqs), len(f.answers))-1]
	if r.Model == "" {
		r.Model = "test-model"
	}
	return r, nil
}

func answer(v any) aiprovider.Response {
	if s, ok := v.(string); ok {
		return aiprovider.Response{Text: s}
	}
	b, _ := json.Marshal(v)
	return aiprovider.Response{Text: string(b)}
}

// fakeCatalog is the catalogs: Search returns every listed item of a kind,
// and Get also knows the hidden ones, which are in the catalog but never on a
// shortlist.
type fakeCatalog struct {
	listed, hidden []knowledge.Item
}

func (f fakeCatalog) Search(kind, _ string, limit int) (*knowledge.SearchResult, error) {
	res := &knowledge.SearchResult{Kind: kind}
	for _, it := range f.listed {
		if it.Kind == kind && len(res.Hits) < limit {
			res.Hits = append(res.Hits, knowledge.SearchHit{Item: it})
		}
	}
	return res, nil
}

func (f fakeCatalog) Get(kind, ref string) (*knowledge.Item, error) {
	for _, it := range slices.Concat(f.listed, f.hidden) {
		if it.Kind == kind && strings.EqualFold(it.Ref, ref) {
			return &it, nil
		}
	}
	return nil, errors.New("not found")
}

var testCatalog = fakeCatalog{
	listed: []knowledge.Item{
		{Kind: knowledge.KindNFR, Ref: "1.3", Title: "Privileged access management", Summary: "Privileged accounts are approved and reviewed", URL: "/security-nfrs/1.3"},
		{Kind: knowledge.KindControl, Ref: "AC-2", Title: "Account Management", Summary: "Manage system accounts", URL: "/controls/AC-2"},
		{Kind: knowledge.KindControl, Ref: "AC-6", Title: "Least Privilege", Summary: "Employ least privilege"},
		{Kind: knowledge.KindRegulationClause, Ref: "DORA-ART-9", Title: "Article 9 Protection and prevention", Group: "DORA",
			Fields: map[string]string{"requirement": "Financial entities shall implement ICT security policies limiting access"}},
	},
	hidden: []knowledge.Item{{Kind: knowledge.KindControl, Ref: "CP-9", Title: "System Backup"}},
}

func aiService(t *testing.T, asker *fakeAsker) *Service {
	t.Helper()
	return newTestService(t).WithAI(asker, testCatalog)
}

func payloadOf[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("payload: %v\n%s", err, raw)
	}
	return v
}

func TestEveryAssistIsOffWithoutAProvider(t *testing.T) {
	for name, svc := range map[string]*Service{
		"not wired":   newTestService(t),
		"no asker":    newTestService(t).WithAI(nil, testCatalog),
		"unavailable": newTestService(t).WithAI(&fakeAsker{off: true}, testCatalog),
	} {
		p, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.DraftPlan(context.Background(), p.ID, DraftInput{Objective: "Test the NFRs"}, "alice")
		wantConflict(t, err, "no AI provider is configured")
		_, err = svc.ProposeAttributes(context.Background(), p.ID, AttributesInput{Focus: "access"}, "alice")
		if name == "not wired" || name == "no asker" {
			wantConflict(t, err, "")
		} else {
			wantConflict(t, err, "no AI provider is configured")
		}
		_, err = svc.DraftNarrative(context.Background(), p.ID, NarrativeInput{Document: "pack"}, "alice")
		wantConflict(t, err, "no AI provider is configured")
		if list, _ := svc.AIProposals(p.ID); len(list) != 0 || svc.AIConfigured() {
			t.Errorf("%s: %d proposals, configured %v", name, len(list), svc.AIConfigured())
		}
	}
}

func TestADraftIsAProposalUntilAPersonAcceptsIt(t *testing.T) {
	asker := &fakeAsker{}
	svc := aiService(t, asker)
	at(svc, "2026-11-02")
	p, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	own := planRefs(p)
	if len(own) == 0 {
		t.Fatal("the template has no references to cite")
	}
	var generated []string
	for _, st := range p.Steps {
		if st.Generated {
			generated = append(generated, st.StepRef)
		}
	}
	q1 := p.Quarters[0]
	roles := []string{"ICT Risk Committee", "CISO (2LoD)", "PM (2LoD)"}
	asker.answers = []aiprovider.Response{answer("Here is the plan you asked for: steps: [0.1 ..."), answer(map[string]any{
		"objective": "By 31 December 2027, every Tier 1 asset's Security NFR data is attested and sample-tested.",
		"steps": []map[string]any{
			{"step_ref": "0.1", "phase": "Mobilise", "title": "Confirm mandate and scope", "step_type": "activity", "responsible_role": "PM (2LoD)",
				"accountable_role": "CISO (2LoD)", "start_date": "2026-11-09", "end_date": "2026-11-20", "deliverable": "Mandate note",
				"basis": "regulation", "cites": []map[string]string{{"kind": "plan", "ref": own[0]}}},
			{"step_ref": "0.2", "title": "Agree the population", "start_date": "2026-13-01", "end_date": "2026-12-04", "responsible_role": "PM (2LoD)"},
			{"step_ref": "0.3", "title": "Brief the committee", "step_type": "milestone", "end_date": "2026-12-15", "responsible_role": "Somebody",
				"basis": "regulation", "cites": []map[string]string{{"kind": "regulation_clause", "ref": "DORA-ART-99"}}},
			{"step_ref": generated[0], "title": "A quarterly step", "start_date": "2027-03-01", "end_date": "2027-03-02", "responsible_role": "PM (2LoD)"},
		},
		"quarters": []map[string]any{
			{"quarter": 1, "lod1_window_start": "2027-01-18", "lod1_deadline": "2027-03-05", "selection_date": "2027-03-08", "fieldwork_start": "2027-03-15",
				"fieldwork_end": "2027-04-16", "findings_agreed_by": "2027-04-30", "report_date": "2027-05-07", "committee_label": "May 2027"},
			{"quarter": 2, "lod1_window_start": "2027-06-01", "lod1_deadline": "2027-04-01", "selection_date": "2027-06-08", "fieldwork_start": "2027-06-15",
				"fieldwork_end": "2027-07-16", "findings_agreed_by": "2027-07-30", "report_date": "2027-08-06"},
		},
		"raci_roles": roles,
		"raci": []map[string]any{
			{"activity": "Approve the plan", "cells": map[string]string{"ICT Risk Committee": "A", "CISO (2LoD)": "R", "Nobody": "C"}},
			{"activity": "Select the sample", "cells": map[string]string{"PM (2LoD)": "A/R", "CISO (2LoD)": "A", "ICT Risk Committee": "X"}},
		},
		"unknowns": []string{"the committee's 2027 meeting dates"},
	})}
	pr, err := svc.DraftPlan(context.Background(), p.ID, DraftInput{Objective: "Attest and test Tier 1", Regulation: "DORA", PopulationSize: 80}, "alice")
	if err != nil {
		t.Fatalf("DraftPlan: %v", err)
	}
	if len(asker.reqs) != 2 || len(asker.reqs[1].History) != 2 || !strings.Contains(asker.reqs[1].Prompt, "did not match the contract") {
		t.Fatalf("the malformed answer was not repaired once: %d requests", len(asker.reqs))
	}
	first := asker.reqs[0]
	for _, rule := range []string{"Say when you do not know", "Never invent a control identifier, an NFR key, a regulatory article, a threshold or a deadline",
		"Keep what the regulation requires separate from good practice"} {
		if !strings.Contains(first.System, rule) {
			t.Errorf("the standing prompt lacks %q", rule)
		}
	}
	if !strings.Contains(first.Prompt, `{"kind": "plan", "ref": "`+own[0]+`"}`) || !strings.Contains(first.Prompt, "about 80 items") {
		t.Errorf("the prompt does not offer the plan's references or the brief:\n%s", first.Prompt)
	}
	if pr.Status != AIProposed || pr.Model != "test-model" || pr.PromptHash != promptHash(aiRules, first.Prompt) || pr.CreatedAt == "" || pr.CreatedBy != "alice" {
		t.Errorf("proposal record = %+v", pr)
	}
	if now := mustGet(t, svc, p.ID); len(now.Steps) != len(p.Steps) || now.Objective != p.Objective || now.AIGenerated {
		t.Error("a proposal changed the plan before anyone accepted it")
	}

	d := payloadOf[DraftProposal](t, pr.Payload)
	if len(d.Steps) != 2 || d.Steps[0].Basis != BasisRegulation || d.Steps[1].Basis != BasisGoodPractice || len(d.Steps[1].Unresolved) != 1 {
		t.Errorf("steps = %+v", d.Steps)
	}
	notes := strings.Join(d.Notes, "\n")
	for _, want := range []string{"0.2 has no valid dates", "cites DORA-ART-99, which do not resolve", "0.3 is marked regulation but cites nothing that resolves",
		generated[0] + " is generated from the quarter dates", "the Q2 dates are missing, out of order", `names "Somebody"`, `names "Nobody"`, `"X", which is not R`,
		`"Select the sample" has 2 accountable roles`} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if len(d.Quarters) != 1 || d.Unknowns[0] != "the committee's 2027 meeting dates" {
		t.Errorf("quarters %+v, unknowns %v", d.Quarters, d.Unknowns)
	}

	edited := d
	edited.Steps[1].Cites = append(edited.Steps[1].Cites, AIRef{Kind: knowledge.KindControl, Ref: "XX-99"})
	body, _ := json.Marshal(edited)
	res, err := svc.Decide(p.ID, pr.ID, DecideInput{Decision: "accept", Parts: []string{"steps", "quarters", "raci"}, Payload: body}, "bob")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	after := res.Plan
	st := after.Steps[slices.IndexFunc(after.Steps, func(s Step) bool { return s.StepRef == "0.1" })]
	if st.Title != "Confirm mandate and scope" || st.Refs != own[0] {
		t.Errorf("step 0.1 = %+v", st)
	}
	for _, s := range after.Steps {
		if strings.Contains(s.Refs, "DORA-ART-99") || strings.Contains(s.Refs, "XX-99") {
			t.Errorf("an unresolved reference was stored on step %s: %q", s.StepRef, s.Refs)
		}
	}
	gen := 0
	for _, s := range after.Steps {
		if s.Generated {
			gen++
		}
	}
	if gen != len(generated) {
		t.Errorf("generated steps %d, want %d", gen, len(generated))
	}
	if after.Quarters[0].LOD1Deadline != "2027-03-05" || after.Quarters[1].LOD1Deadline != p.Quarters[1].LOD1Deadline || after.Quarters[0].RetestDays != q1.RetestDays {
		t.Errorf("quarters = %+v", after.Quarters[:2])
	}
	if !slices.Equal(after.RACIRoles, roles) || len(after.RACI) != 2 {
		t.Errorf("RACI = %v %+v", after.RACIRoles, after.RACI)
	}
	if after.Objective != p.Objective || !after.AIGenerated || after.Model != "test-model" || after.PromptHash != pr.PromptHash {
		t.Errorf("objective left out of the parts was applied, or the provenance is missing: %+v", after)
	}
	if res.Proposal.Status != AIAccepted || res.Proposal.DecidedBy != "bob" || len(res.Proposal.Applied) == 0 {
		t.Errorf("decided = %+v", res.Proposal)
	}
	_, err = svc.Decide(p.ID, pr.ID, DecideInput{Decision: "reject"}, "bob")
	wantConflict(t, err, "already accepted")
	history, _ := svc.History(p.ID)
	events := 0
	for _, hh := range history {
		if hh.Event == "ai proposal" {
			events++
		}
	}
	if events != 2 {
		t.Errorf("history has %d AI proposal events, want 2", events)
	}

	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.DraftPlan(context.Background(), p.ID, DraftInput{Objective: "again"}, "alice")
	wantConflict(t, err, "in review")
}

func TestAnAnswerThatCannotBeUsedIsNotStored(t *testing.T) {
	for name, c := range map[string]struct {
		asker *fakeAsker
		code  int
	}{
		"refused":       {&fakeAsker{answers: []aiprovider.Response{{Refused: true}}}, http.StatusUnprocessableEntity},
		"cut off":       {&fakeAsker{answers: []aiprovider.Response{{Text: `{"steps": [`, StopReason: aiprovider.StopMaxTokens}}}, http.StatusUnprocessableEntity},
		"twice garbled": {&fakeAsker{answers: []aiprovider.Response{answer("no"), answer(`{"steps": "many"}`)}}, http.StatusBadGateway},
		"provider down": {&fakeAsker{err: errors.New("connection refused")}, http.StatusBadGateway},
	} {
		svc := aiService(t, c.asker)
		p, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.DraftPlan(context.Background(), p.ID, DraftInput{Constraints: "none"}, "alice")
		var u *UpstreamError
		var r *RefusedError
		got := 0
		switch {
		case errors.As(err, &u):
			got = http.StatusBadGateway
		case errors.As(err, &r):
			got = http.StatusUnprocessableEntity
		}
		if got != c.code {
			t.Errorf("%s: error %v, want %d", name, err, c.code)
		}
		if list, _ := svc.AIProposals(p.ID); len(list) != 0 {
			t.Errorf("%s: %d proposals stored", name, len(list))
		}
		if done, err := svc.begin(p.ID); err != nil {
			t.Errorf("%s: the plan stayed claimed after the failure: %v", name, err)
		} else {
			done()
		}
	}
	svc := aiService(t, &fakeAsker{})
	done, err := svc.begin(1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.begin(1)
	wantConflict(t, err, "already running")
	done()
	_, err = svc.DraftPlan(context.Background(), 1, DraftInput{}, "alice")
	wantInvalid(t, err, "the brief is empty")
	_, err = svc.DraftPlan(context.Background(), 1, DraftInput{Constraints: strings.Repeat("x", maxBrief+1)}, "alice")
	wantInvalid(t, err, "longer than")
}

func TestProposedAttributesCiteOnlyWhatResolves(t *testing.T) {
	asker := &fakeAsker{}
	svc := aiService(t, asker)
	p := createExample(t, svc, "alice")
	existing := p.Attributes[0].AttrRef
	asker.answers = []aiprovider.Response{answer(map[string]any{
		"attributes": []map[string]any{
			{"attr_ref": "PAM-01", "domain": "Access", "requirement": "Privileged accounts are approved and reviewed quarterly.", "procedure": "Inspect the review records.",
				"method": "inspection", "evidence_requested": "Review records", "pass_criteria": "Every privileged account reviewed", "applies_to": []string{"T1", "T9"},
				"basis": "regulation", "why": "Privileged access is the commonest route in",
				"cites": []map[string]string{{"kind": "nfr", "ref": "1.3"}, {"kind": "control", "ref": "ac-2"}, {"kind": "control", "ref": "CP-9"},
					{"kind": "control", "ref": "ZZ-99"}, {"kind": "regulation_clause", "ref": "DORA-ART-9"}}},
			{"attr_ref": "LP-01", "requirement": "Least privilege is enforced.", "method": "Vibes", "basis": "regulation",
				"cites": []map[string]string{{"kind": "control", "ref": "AC-6"}}},
			{"attr_ref": existing, "requirement": "A duplicate reference.", "cites": []map[string]string{}},
			{"attr_ref": "EMPTY", "requirement": ""},
		},
		"unknowns": []string{"the review frequency the institution has set"},
	})}
	pr, err := svc.ProposeAttributes(context.Background(), p.ID, AttributesInput{Focus: "DORA Art. 9 access control", Count: 40}, "alice")
	if err != nil {
		t.Fatalf("ProposeAttributes: %v", err)
	}
	prompt := asker.reqs[0].Prompt
	if !strings.Contains(prompt, `{"kind": "control", "ref": "AC-2"}`) || !strings.Contains(prompt, `{"kind": "regulation_clause", "ref": "DORA-ART-9"}`) ||
		strings.Contains(prompt, "CP-9") || !strings.Contains(prompt, "propose up to 15 test attributes") || !strings.Contains(prompt, existing) {
		t.Errorf("prompt:\n%s", prompt)
	}
	a := payloadOf[AttributesProposal](t, pr.Payload)
	if len(a.Attributes) != 3 || len(a.Shortlist) != 4 {
		t.Fatalf("proposal = %+v", a)
	}
	pam, lp, dup := a.Attributes[0], a.Attributes[1], a.Attributes[2]
	if pam.NFRKey != "1.3" || !slices.Equal(pam.ControlIDs, []string{"AC-2", "CP-9"}) || pam.RegulatoryRefs != "DORA-ART-9" || pam.Basis != BasisRegulation ||
		len(pam.Unresolved) != 1 || pam.Unresolved[0].Ref != "ZZ-99" || !slices.Equal(pam.AppliesTo, []string{"T1"}) || pam.Method != MethodInspection {
		t.Errorf("PAM-01 = %+v", pam)
	}
	if lp.Basis != BasisGoodPractice || lp.Method != MethodInspection || dup.AttrRef == existing {
		t.Errorf("LP-01 = %+v, duplicate named %s", lp, dup.AttrRef)
	}
	notes := strings.Join(a.Notes, "\n")
	for _, want := range []string{"CP-9, which is in the catalog but was not on the shortlist", "cites ZZ-99, which do not resolve", "T9 is not one of the plan's strata",
		"LP-01 is marked regulation but cites no regulation clause", `"Vibes" is not a method`, existing + " is already used", "attribute 4 has no requirement"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}

	pam.Requirement = "Privileged accounts are approved, and reviewed every quarter."
	pam.Cites = append(pam.Cites, AIRef{Kind: knowledge.KindControl, Ref: "FAKE-1"})
	a.Attributes[0] = pam
	body, _ := json.Marshal(a)
	res, err := svc.Decide(p.ID, pr.ID, DecideInput{Decision: "accept", Attributes: []string{"PAM-01", dup.AttrRef}, Payload: body}, "bob")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	got := res.Plan.Attributes
	if len(got) != len(p.Attributes)+2 {
		t.Fatalf("attributes %d, want %d", len(got), len(p.Attributes)+2)
	}
	added := got[slices.IndexFunc(got, func(x Attribute) bool { return x.AttrRef == "PAM-01" })]
	if !added.AIGenerated || added.Model != "test-model" || added.PromptHash != pr.PromptHash || added.Requirement != pam.Requirement ||
		!slices.Equal(added.ControlIDs, []string{"AC-2", "CP-9"}) || added.NFRKey != "1.3" || added.RegulatoryRefs != "DORA-ART-9" {
		t.Errorf("PAM-01 on the plan = %+v", added)
	}
	if slices.ContainsFunc(got, func(x Attribute) bool { return x.AttrRef == "LP-01" }) {
		t.Error("an attribute left unchosen was added")
	}

	empty := newTestService(t).WithAI(&fakeAsker{}, fakeCatalog{})
	p2, _ := empty.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
	_, err = empty.ProposeAttributes(context.Background(), p2.ID, AttributesInput{Focus: "quantum"}, "alice")
	wantInvalid(t, err, "nothing in the NFR catalog")
	blind := newTestService(t).WithAI(&fakeAsker{}, nil)
	p3, _ := blind.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
	_, err = blind.ProposeAttributes(context.Background(), p3.ID, AttributesInput{Focus: "access"}, "alice")
	wantConflict(t, err, "catalogs are not available")
}

func TestNarrativeIsFlaggedAndAcceptedIntoItsReport(t *testing.T) {
	svc, _ := newIntegratedService(t)
	asker := &fakeAsker{}
	svc.WithAI(asker, testCatalog)
	p := sampleYear(t, svc, 2)
	at(svc, "2027-07-20")

	asker.answers = []aiprovider.Response{answer(map[string]any{
		"executive_summary": "Q1 testing of " + p.Reference + " is complete; 4242 widgets were reviewed against DORA Article 77.",
		"speaker_notes":     []map[string]any{{"slide": 2, "notes": "ignored for a report"}},
		"unknowns":          []string{"whether the committee will accept the extension"},
	})}
	pr, err := svc.DraftNarrative(context.Background(), p.ID, NarrativeInput{Document: "quarter", Quarter: 1}, "alice")
	if err != nil {
		t.Fatalf("DraftNarrative: %v", err)
	}
	if pr.Document != "q1" || !strings.Contains(asker.reqs[0].Prompt, `"executive_summary"`) || !strings.Contains(asker.reqs[0].Prompt, p.Reference) {
		t.Errorf("proposal %+v, prompt lacks the report data", pr)
	}
	n := payloadOf[NarrativeProposal](t, pr.Payload)
	flags := strings.Join(n.Flags, "\n")
	if !strings.Contains(flags, "the figure 4242 is not in the plan's data") || !strings.Contains(flags, "Article 77 is not among the plan's references") ||
		strings.Contains(flags, "2027") || len(n.SpeakerNotes) != 0 || !strings.Contains(strings.Join(n.Notes, " "), "speaker notes are for the pack") {
		t.Errorf("narrative = %+v", n)
	}
	n.ExecutiveSummary = "Q1 is complete and on plan."
	body, _ := json.Marshal(n)
	if _, err := svc.Decide(p.ID, pr.ID, DecideInput{Decision: "accept", Payload: body}, "bob"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	q1, err := svc.QuarterReport(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if q1.ExecutiveSummary != "Q1 is complete and on plan." || !strings.Contains(q1.Limitations, "drafted with AI assistance (test-model)") ||
		!strings.Contains(q1.Limitations, "accepted by bob") {
		t.Errorf("Q1 report: %q / %q", q1.ExecutiveSummary, q1.Limitations)
	}
	q2, _ := svc.QuarterReport(p.ID, 2)
	if strings.Contains(q2.Limitations, "AI assistance") {
		t.Error("the Q1 narrative went into the Q2 report")
	}

	asker.answers = []aiprovider.Response{answer(map[string]any{
		"executive_summary": "The plan asks the committee to approve it.",
		"speaker_notes": []map[string]any{{"slide": 1, "notes": "Introduce the plan."}, {"slide": 2, "notes": "The decisions."},
			{"slide": 2, "notes": "A second note for slide 2."}, {"slide": 14, "notes": "There is no slide 14."}},
		"unknowns": []string{},
	})}
	asker.reqs = nil
	pack, err := svc.DraftNarrative(context.Background(), p.ID, NarrativeInput{Document: "pack"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	pn := payloadOf[NarrativeProposal](t, pack.Payload)
	if len(pn.SpeakerNotes) != 2 || pn.SpeakerNotes[1].Title != "Decisions requested" || !strings.Contains(strings.Join(pn.Notes, " "), "11 slide(s) have no speaker notes") ||
		!strings.Contains(asker.reqs[0].Prompt, `"slides"`) {
		t.Errorf("pack narrative = %+v", pn)
	}
	if _, err := svc.Decide(p.ID, pack.ID, DecideInput{Decision: "accept"}, "bob"); err != nil {
		t.Fatal(err)
	}
	payload, err := svc.PackData(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Pack.Narrative == nil || len(payload.Pack.Narrative.SpeakerNotes) != 2 || payload.Pack.Narrative.AcceptedBy != "bob" {
		t.Errorf("pack narrative = %+v", payload.Pack.Narrative)
	}

	asker.answers = []aiprovider.Response{answer(map[string]any{"executive_summary": "Rejected text.", "speaker_notes": []any{}, "unknowns": []any{}})}
	ann, err := svc.DraftNarrative(context.Background(), p.ID, NarrativeInput{Document: "annual"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(p.ID, ann.ID, DecideInput{Decision: "reject", Note: "Not our voice"}, "bob"); err != nil {
		t.Fatal(err)
	}
	if r, _ := svc.AnnualReport(p.ID); r.ExecutiveSummary == "Rejected text." {
		t.Error("a rejected narrative went into the annual report")
	}
	_, err = svc.DraftNarrative(context.Background(), p.ID, NarrativeInput{Document: "quarter", Quarter: 7}, "alice")
	wantInvalid(t, err, "1 to 4")
}

func TestAIRoutesAndTab(t *testing.T) {
	r, svc := newTestRouter(t, "alice")
	p, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	path := "/assurance-plans/" + itoa(p.ID)
	if w := do(t, r, http.MethodPost, path+"/ai/draft", DraftInput{Objective: "x"}); w.Code != http.StatusConflict {
		t.Errorf("draft with no provider = %d %s", w.Code, w.Body)
	}
	page := do(t, r, http.MethodGet, path, nil).Body.String()
	for _, want := range []string{`id="tab-ai"`, "No AI provider is configured", `id="aiDraftGo" disabled`, "No proposals yet."} {
		if !strings.Contains(page, want) {
			t.Errorf("the page without a provider lacks %q", want)
		}
	}

	asker := &fakeAsker{answers: []aiprovider.Response{answer(map[string]any{"executive_summary": "Summary.", "speaker_notes": []any{}, "unknowns": []any{}})}}
	svc.WithAI(asker, testCatalog)
	w := do(t, r, http.MethodPost, path+"/ai/narrative", NarrativeInput{Document: "annual"})
	if w.Code != http.StatusCreated {
		t.Fatalf("narrative = %d %s", w.Code, w.Body)
	}
	var pr AIProposal
	_ = json.Unmarshal(w.Body.Bytes(), &pr)
	var list struct {
		Configured bool         `json:"configured"`
		Proposals  []AIProposal `json:"proposals"`
	}
	_ = json.Unmarshal(do(t, r, http.MethodGet, path+"/ai/proposals", nil).Body.Bytes(), &list)
	if !list.Configured || len(list.Proposals) != 1 || list.Proposals[0].ID != pr.ID {
		t.Errorf("proposals = %+v", list)
	}
	if w := do(t, r, http.MethodGet, path+"/ai/proposals/"+itoa(pr.ID), nil); w.Code != http.StatusOK {
		t.Errorf("one proposal = %d", w.Code)
	}
	if w := do(t, r, http.MethodGet, path+"/ai/proposals/999", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing proposal = %d", w.Code)
	}
	if w := do(t, r, http.MethodPost, path+"/ai/proposals/"+itoa(pr.ID)+"/decide", DecideInput{Decision: "maybe"}); w.Code != http.StatusBadRequest {
		t.Errorf("bad decision = %d", w.Code)
	}
	long := strings.Repeat("x", 100<<10)
	if w := do(t, r, http.MethodPost, path+"/ai/proposals/"+itoa(pr.ID)+"/decide", DecideInput{Decision: "maybe", Note: long}); !strings.Contains(w.Body.String(), "accept or reject") {
		t.Errorf("a long decision was cut off before the service: %d %s", w.Code, w.Body)
	}
	if w := do(t, r, http.MethodPost, path+"/ai/narrative", NarrativeInput{Document: long}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid JSON body") {
		t.Errorf("a long brief = %d %s", w.Code, w.Body)
	}
	page = do(t, r, http.MethodGet, path, nil).Body.String()
	for _, want := range []string{"Answers come from Wintermute · test-model", `data-ai-review="` + itoa(pr.ID) + `"`, `ap-st ap-st-proposed`, ".ap-st-proposed"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if w := do(t, r, http.MethodPost, path+"/ai/proposals/"+itoa(pr.ID)+"/decide", DecideInput{Decision: "accept"}); w.Code != http.StatusOK {
		t.Errorf("accept = %d %s", w.Code, w.Body)
	}
	asker.answers, asker.reqs = []aiprovider.Response{{Refused: true}}, nil
	if w := do(t, r, http.MethodPost, path+"/ai/narrative", NarrativeInput{Document: "pack"}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("refusal = %d", w.Code)
	}
	asker.err = errors.New("down")
	if w := do(t, r, http.MethodPost, path+"/ai/attributes", AttributesInput{Focus: "access"}); w.Code != http.StatusBadGateway {
		t.Errorf("provider down = %d", w.Code)
	}
}
