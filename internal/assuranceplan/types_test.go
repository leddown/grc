package assuranceplan

import (
	"encoding/csv"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"grc/internal/auditfinding"
	"grc/internal/db"
	"grc/internal/policydocs"
)

// TestEveryPlanTypeIsAConsistentTemplate instantiates each plan type for 2027
// and checks it against itself: no blocking gate finding, every role it names
// is one of its RACI roles, every attribute applies to a stratum it has, and
// what a stratum takes from a proposal is in that source's vocabulary.
func TestEveryPlanTypeIsAConsistentTemplate(t *testing.T) {
	want := map[string]struct {
		basis, source string
		types         int
		sampled       bool
	}{
		"controls-testing":    {CoverageAttestation, "", 1, true},
		"rcsa":                {CoverageAttestation, "", 1, true},
		"policy-review":       {CoverageTests, SourcePolicies, 1, false},
		"resilience-testing":  {CoverageTests, "", 6, false},
		"remediation-roadmap": {CoverageTests, SourceFindings, 1, false},
		"blank":               {CoverageAttestation, "", 0, false},
	}
	svc := newTestService(t)
	seen := map[string]bool{}
	for _, info := range Templates() {
		if info.Example {
			continue
		}
		seen[info.Key] = true
		w, ok := want[info.Key]
		if !ok {
			t.Errorf("plan type %s is not covered by this test", info.Key)
			continue
		}
		p, err := svc.Create(CreateInput{Type: info.Key, PeriodStart: "2027-01-01"}, "alice")
		if err != nil {
			t.Fatalf("%s: %v", info.Key, err)
		}
		if p.CoverageBasis != w.basis || p.PopulationSource != w.source || len(p.TestTypes) != w.types {
			t.Errorf("%s: basis %q source %q types %v", info.Key, p.CoverageBasis, p.PopulationSource, p.TestTypes)
		}
		if got := slices.ContainsFunc(p.Strata, func(st Stratum) bool { return st.SamplingEnabled }); got != w.sampled {
			t.Errorf("%s: a sampled stratum = %v, want %v", info.Key, got, w.sampled)
		}
		if info.Key == "blank" {
			continue
		}
		for _, f := range BlockingFindings(Lint(p, nil)) {
			t.Errorf("%s: blocking %s: %s", info.Key, f.Code, f.Message)
		}
		roles := map[string]bool{}
		for _, r := range p.RACIRoles {
			roles[r] = true
		}
		for _, s := range p.Steps {
			for _, r := range []string{s.ResponsibleRole, s.AccountableRole} {
				if r != "" && !roles[r] {
					t.Errorf("%s: step %s names %q, which is not a RACI role", info.Key, s.StepRef, r)
				}
			}
		}
		for _, row := range p.RACI {
			for r := range row.Cells {
				if !roles[r] {
					t.Errorf("%s: RACI %q names %q, which is not a RACI role", info.Key, row.Activity, r)
				}
			}
		}
		codes := map[string]bool{}
		for _, st := range p.Strata {
			codes[st.Code] = true
		}
		for _, a := range p.Attributes {
			for _, c := range a.AppliesTo {
				if !codes[c] {
					t.Errorf("%s: attribute %s applies to unknown stratum %s", info.Key, a.AttrRef, c)
				}
			}
		}
		if (len(p.Attributes) == 0 && info.Key != "controls-testing") || len(p.Basis) == 0 || len(p.Cadence) == 0 || len(p.Escalations) == 0 {
			t.Errorf("%s: attributes %d, basis %d, cadence %d, escalations %d", info.Key, len(p.Attributes), len(p.Basis), len(p.Cadence), len(p.Escalations))
		}
		for _, f := range Lint(p, nil) {
			if f.Code == "takes_unknown" || f.Code == "source_unmapped" {
				t.Errorf("%s: %s", info.Key, f.Message)
			}
		}
		quarterly := 0
		for _, s := range p.Steps {
			if s.Generated {
				quarterly++
			}
		}
		if quarterly < 16 {
			t.Errorf("%s: only %d steps generated from the quarterly cycle", info.Key, quarterly)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("plan type %s is missing", k)
		}
	}
	res, _ := svc.Create(CreateInput{Type: "resilience-testing", PeriodStart: "2027-01-01"}, "alice")
	tlpt := res.Strata[slices.IndexFunc(res.Strata, func(st Stratum) bool { return st.Code == "TLPT" })]
	if tlpt.CycleYears != 3 || tlpt.SamplingEnabled {
		t.Errorf("TLPT stratum = %+v", tlpt)
	}
}

func TestPlanTypeSettingsInTheHeader(t *testing.T) {
	svc := newTestService(t)
	p, err := svc.Create(CreateInput{Type: "policy-review", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	in := p
	in.TestTypes, in.CoverageBasis, in.PopulationSource = nil, "", ""
	kept, err := svc.UpdateHeader(p.ID, in, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if kept.CoverageBasis != CoverageTests || kept.PopulationSource != SourcePolicies || len(kept.TestTypes) != 1 {
		t.Errorf("settings left out of the request were not kept: %q %q %v", kept.CoverageBasis, kept.PopulationSource, kept.TestTypes)
	}
	in.TestTypes = []string{" Policy review ", "Event-driven review", "policy review", ""}
	in.CoverageBasis, in.PopulationSource = "Attestation", "none"
	got, err := svc.UpdateHeader(p.ID, in, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.CoverageBasis != CoverageAttestation || got.PopulationSource != "" || strings.Join(got.TestTypes, "|") != "Policy review|Event-driven review" {
		t.Errorf("header = %q %q %v", got.CoverageBasis, got.PopulationSource, got.TestTypes)
	}
	history, _ := svc.History(p.ID)
	if !strings.Contains(history[0].Note, "population source") || !strings.Contains(history[0].Note, "coverage basis") {
		t.Errorf("history = %q", history[0].Note)
	}
	in.CoverageBasis = "vibes"
	_, err = svc.UpdateHeader(p.ID, in, "alice")
	wantInvalid(t, err, "coverage basis")
	in.CoverageBasis, in.PopulationSource = "", "spreadsheets"
	_, err = svc.UpdateHeader(p.ID, in, "alice")
	wantInvalid(t, err, "population source")
}

func TestStrataCycleAndTakesAreValidated(t *testing.T) {
	base := Stratum{Code: "X", Waves: [4]float64{1, 0, 0, 0}}
	for _, c := range []struct {
		edit func(*Stratum)
		want string
	}{
		{func(s *Stratum) { s.CycleYears = 11 }, "cycle"},
		{func(s *Stratum) { s.CycleYears = -1 }, "cycle"},
		{func(s *Stratum) { s.Takes = []string{"policy, standard"} }, "separator"},
	} {
		s := base
		c.edit(&s)
		wantInvalid(t, normalizeStrata([]Stratum{s}), c.want)
	}
	s := base
	s.CycleYears, s.Takes = 1, []string{" High ", "high", "", "Critical"}
	strata := []Stratum{s}
	if err := normalizeStrata(strata); err != nil {
		t.Fatal(err)
	}
	if strata[0].CycleYears != 0 || strings.Join(strata[0].Takes, "|") != "High|Critical" {
		t.Errorf("normalised = %d %v", strata[0].CycleYears, strata[0].Takes)
	}
}

// policyDoc records a document in force, as an approval would leave it: the
// test writes the status directly rather than running the policy gate.
func policyDoc(t *testing.T, conn *db.Conn, docs *policydocs.Service, ref, typ, status, effective string) policydocs.Document {
	t.Helper()
	d, err := docs.CreateDocument(policydocs.Document{Reference: ref, Title: "Document " + ref, DocType: typ, OwnerRole: "Owner of " + ref,
		Classification: "Internal", ReviewCadenceMonths: 12, EffectiveDate: effective})
	if err != nil {
		t.Fatalf("CreateDocument %s: %v", ref, err)
	}
	if _, err := conn.Exec(`UPDATE policy_documents SET status = ? WHERE id = ?`, status, d.ID); err != nil {
		t.Fatal(err)
	}
	return d
}

func proposalRows(t *testing.T, csvText string) map[string][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rows[0], ",") != strings.Join(proposalColumns, ",") {
		t.Fatalf("header = %v", rows[0])
	}
	out := map[string][]string{}
	for _, r := range rows[1:] {
		out[r[0]] = r
	}
	return out
}

func TestProposeAPolicyReviewPopulation(t *testing.T) {
	conn := newTestDB(t)
	docs := policydocs.NewService(policydocs.NewSQLiteRepository(conn))
	svc := newTestServiceOn(conn).WithIntegrations(Integrations{Policies: docs})
	at(svc, "2026-12-01")

	dueQ1 := policyDoc(t, conn, docs, "POL-AC-001", policydocs.TypePolicy, policydocs.StatusApproved, "2026-03-01")
	policyDoc(t, conn, docs, "POL-IS-001", policydocs.TypePolicy, policydocs.StatusInReview, "2025-05-01")
	policyDoc(t, conn, docs, "STD-CR-001", policydocs.TypeStandard, policydocs.StatusApproved, "2026-08-15")
	policyDoc(t, conn, docs, "TRA-PCI-01", policydocs.TypeTRA, policydocs.StatusApproved, "2026-11-20")
	policyDoc(t, conn, docs, "STD-LATER", policydocs.TypeStandard, policydocs.StatusApproved, "2027-03-01")
	policyDoc(t, conn, docs, "POL-DRAFT", policydocs.TypePolicy, policydocs.StatusDraft, "2026-03-01")
	policyDoc(t, conn, docs, "POL-RETIRED", policydocs.TypePolicy, policydocs.StatusRetired, "2026-03-01")
	policyDoc(t, conn, docs, "LTR-001", policydocs.TypeLetter, policydocs.StatusApproved, "2026-03-01")
	policyDoc(t, conn, docs, "POL-NOCLOCK", policydocs.TypePolicy, policydocs.StatusApproved, "")

	p, err := svc.Create(CreateInput{Type: "policy-review", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	prop, err := svc.ProposePopulation(p.ID, "")
	if err != nil {
		t.Fatalf("ProposePopulation: %v", err)
	}
	rows := proposalRows(t, prop.CSV)
	want := map[string][2]string{
		"POL-AC-001": {"POL", "Q1"}, "POL-IS-001": {"POL", "Q1"}, "STD-CR-001": {"STD", "Q3"}, "TRA-PCI-01": {"STD", "Q4"},
	}
	if len(rows) != len(want) || prop.Items != 4 || prop.New != 4 || !prop.Merge {
		t.Fatalf("proposal = %+v\n%s", prop, prop.CSV)
	}
	for ref, w := range want {
		r := rows[ref]
		if r == nil || r[2] != w[0] || r[5] != w[1] || r[6] != SourceKindPolicy || r[3] != "Owner of "+ref {
			t.Errorf("%s = %v, want stratum %s wave %s", ref, r, w[0], w[1])
		}
	}
	if rows["POL-AC-001"][7] != itoa(dueQ1.ID) {
		t.Errorf("source_ref = %q, want the document id", rows["POL-AC-001"][7])
	}
	notes := strings.Join(prop.Notes, "\n")
	for _, w := range []string{"1 document(s) were already overdue", "1 document(s) are next due for review after the period", "1 document(s) in force have no next review date"} {
		if !strings.Contains(notes, w) {
			t.Errorf("notes lack %q:\n%s", w, notes)
		}
	}

	diff, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: prop.CSV, Merge: true}, "alice")
	if err != nil || len(diff.Errors) > 0 || len(diff.Added) != 4 {
		t.Fatalf("apply = %+v, %v", diff, err)
	}
	item := itemByRef(t, svc, p.ID, "STD-CR-001")
	if item.SourceKind != SourceKindPolicy || item.SourceRef == "" || item.WaveQuarter != 3 {
		t.Errorf("applied item = %+v", item)
	}

	if _, err := conn.Exec(`UPDATE policy_documents SET next_review_date = '2027-05-02' WHERE reference = 'STD-CR-001'`); err != nil {
		t.Fatal(err)
	}
	again, err := svc.ProposePopulation(p.ID, SourcePolicies)
	if err != nil {
		t.Fatal(err)
	}
	if again.New != 0 || proposalRows(t, again.CSV)["STD-CR-001"][5] != "Q3" || !strings.Contains(strings.Join(again.Notes, "\n"), "STD-CR-001: the source now places it in STD Q2; it stays in STD Q3") {
		t.Errorf("re-proposal moved an item or did not say so: %+v", again)
	}
	diff, err = svc.PreviewPopulation(p.ID, PopulationImport{CSV: again.CSV, Merge: true})
	if err != nil || len(diff.Errors) > 0 || len(diff.Added)+len(diff.Changed)+len(diff.Removed) != 0 {
		t.Errorf("re-proposal preview = %+v, %v", diff, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestProposeARemediationRoadmapFromOpenFindings(t *testing.T) {
	conn := newTestDB(t)
	findings := auditfinding.NewService(auditfinding.NewSQLiteRepository(conn))
	svc := newTestServiceOn(conn).WithIntegrations(Integrations{Findings: findings})
	at(svc, "2026-12-01")
	for _, f := range []auditfinding.Finding{
		{Reference: "FND-1", Title: "Privileged access not recertified", Severity: "High", Owner: "Head of IAM", BusinessUnit: "Group IT", DueDate: "2027-05-15"},
		{Reference: "FND-2", Title: "Backups not restored in test", Severity: "Critical", Owner: "CIO", DueDate: "2026-10-01"},
		{Reference: "FND-3", Title: "Logging gaps", Severity: "Medium", Owner: "SOC lead", DueDate: "2027-11-30"},
		{Reference: "FND-4", Title: "Next year's work", Severity: "Low", Owner: "Architect", DueDate: "2028-03-01"},
		{Reference: "FND-5", Title: "Already closed", Severity: "High", Owner: "CIO", DueDate: "2027-02-01", Status: auditfinding.StatusRiskAccepted,
			RiskAcceptedBy: "CRO", RiskAcceptanceRationale: "Compensating control", RiskAcceptanceExpiry: "2027-12-31"},
	} {
		if _, err := findings.Create(f, "auditor"); err != nil {
			t.Fatalf("finding %s: %v", f.Reference, err)
		}
	}
	p, err := svc.Create(CreateInput{Type: "remediation-roadmap", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	prop, err := svc.ProposePopulation(p.ID, "")
	if err != nil {
		t.Fatalf("ProposePopulation: %v", err)
	}
	rows := proposalRows(t, prop.CSV)
	want := map[string][2]string{"FND-1": {"F-CH", "Q2"}, "FND-2": {"F-CH", "Q1"}, "FND-3": {"F-ML", "Q4"}}
	if len(rows) != len(want) || prop.Skipped != 1 {
		t.Fatalf("proposal = %+v\n%s", prop, prop.CSV)
	}
	for ref, w := range want {
		if r := rows[ref]; r == nil || r[2] != w[0] || r[5] != w[1] || r[6] != SourceKindFinding {
			t.Errorf("%s = %v, want %v", ref, r, w)
		}
	}
	if rows["FND-1"][4] != "Group IT" || rows["FND-1"][3] != "Head of IAM" {
		t.Errorf("FND-1 owner and business line = %v", rows["FND-1"])
	}
	notes := strings.Join(prop.Notes, "\n")
	if !strings.Contains(notes, "1 finding(s) were already overdue") || !strings.Contains(notes, "left for next year's roadmap") {
		t.Errorf("notes = %s", notes)
	}
}

func TestProposalNeedsASourceAMappingAndTheIntegration(t *testing.T) {
	svc := newTestService(t)
	ctl, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ProposePopulation(ctl.ID, "")
	wantInvalid(t, err, "no population source")
	_, err = svc.ProposePopulation(ctl.ID, "findings")
	wantInvalid(t, err, "no stratum takes anything")
	_, err = svc.ProposePopulation(ctl.ID, "inventory")
	wantInvalid(t, err, "population source is")
	road, err := svc.Create(CreateInput{Type: "remediation-roadmap", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ProposePopulation(road.ID, "")
	wantConflict(t, err, "Audit Findings is not available")
	if f := Lint(ctl, nil); slices.ContainsFunc(f, func(x LintFinding) bool { return x.Code == "source_unmapped" }) {
		t.Error("a plan without a source is linted for its mapping")
	}
	road.Strata[0].Takes = []string{"Severe"}
	road.Strata[1].Takes = nil
	codes := []string{}
	for _, f := range Lint(road, nil) {
		codes = append(codes, f.Code)
	}
	if !slices.Contains(codes, "takes_unknown") {
		t.Errorf("lint codes = %v", codes)
	}
	road.Strata[0].Takes = nil
	codes = codes[:0]
	for _, f := range Lint(road, nil) {
		codes = append(codes, f.Code)
	}
	if !slices.Contains(codes, "source_unmapped") {
		t.Errorf("lint codes = %v", codes)
	}
}

// resiliencePlan is a 2027 resilience testing programme, approved and frozen,
// with eight systems in quarterly waves and three TLPT scopes: one due in Q4
// of its cycle, one inside its cycle, one never tested.
func resiliencePlan(t *testing.T, svc *Service) Plan {
	t.Helper()
	at(svc, "2026-12-01")
	p, err := svc.Create(CreateInput{Type: "resilience-testing", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	paste := "item_ref,name,stratum,wave,last_tested_on\n" +
		"SYS-1,Payments gateway,SYS,Q1,\nSYS-2,Core ledger,SYS,Q1,\nSYS-3,Card switch,SYS,Q2,\nSYS-4,Online banking,SYS,Q2,\n" +
		"SYS-5,Mobile API,SYS,Q3,\nSYS-6,Treasury,SYS,Q3,\nSYS-7,Data hub,SYS,Q4,\nSYS-8,Identity,SYS,Q4,\n" +
		"CIF-PAY,Payments,TLPT,Q4,2024-10-01\nCIF-DEP,Deposits,TLPT,,2025-06-15\nCIF-LND,Lending,TLPT,Q3,\n"
	diff, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: paste}, "alice")
	if err != nil || len(diff.Errors) > 0 {
		t.Fatalf("population: %+v, %v", diff, err)
	}
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(p.ID, "bob", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if p, err = svc.FreezePopulation(p.ID, "alice"); err != nil {
		t.Fatalf("an item inside its cycle without a wave stopped the freeze: %v", err)
	}
	return p
}

func TestResilienceProgrammeCoversEverySystemAndRunsTheTLPTCycle(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := resiliencePlan(t, svc)

	view, err := svc.Sampling(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	sys, tlpt := view.Strata[0], view.Strata[1]
	if sys.N != 8 || sys.Statistical != [4]int{2, 2, 2, 2} || sys.CumTarget != [4]float64{0.25, 0.5, 0.75, 1} {
		t.Errorf("SYS sizing = N %d, statistical %v, target %v", sys.N, sys.Statistical, sys.CumTarget)
	}
	if tlpt.N != 2 || tlpt.InCycle != 1 || tlpt.CycleYears != 3 || tlpt.Statistical != [4]int{0, 0, 1, 1} {
		t.Errorf("TLPT sizing = N %d, in cycle %d, statistical %v", tlpt.N, tlpt.InCycle, tlpt.Statistical)
	}

	at(svc, "2027-03-29")
	sel, err := svc.SelectQuarter(p.ID, 1, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.Picks) != 2 || sel.Picks[0].Basis != BasisFull {
		t.Errorf("Q1 picks = %+v", sel.Picks)
	}
	tests, err := svc.CreateTests(p.ID, CreateTestsInput{Quarter: 1, Tester: "tess", EvidenceRequestedOn: "2027-04-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if tests[0].TestType != "Vulnerability assessment" {
		t.Errorf("default test type = %q", tests[0].TestType)
	}
	p, _ = svc.Get(p.ID)
	rt1 := attrByRef(t, p, "RT-01")
	saved, err := svc.SaveTest(p.ID, tests[0].ID, TestInput{TestType: "penetration TEST", Tester: "tess", EvidenceRequestedOn: "2027-04-01",
		FieldworkCompletedOn: "2027-04-20", Results: passFor(p, "SYS", map[string]string{rt1.AttrRef: ResultFail})}, "tess")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Test.TestType != "Penetration test" {
		t.Errorf("test type = %q, want the plan's spelling", saved.Test.TestType)
	}
	_, err = svc.SaveTest(p.ID, tests[1].ID, TestInput{TestType: "Gut feeling", Tester: "tess"}, "tess")
	wantInvalid(t, err, "not one of the plan's")
	if _, err := svc.SaveTest(p.ID, tests[1].ID, TestInput{Tester: "tess", EvidenceRequestedOn: "2027-04-01", FieldworkCompletedOn: "2027-04-22",
		Results: passFor(p, "SYS", nil)}, "tess"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.loadTest(p, tests[1].ID); got.TestType != "Vulnerability assessment" {
		t.Errorf("a blank test type did not keep the test's type: %q", got.TestType)
	}
	at(svc, "2027-04-28")
	for _, tt := range tests {
		if _, err := svc.SignOffQA(p.ID, tt.ID, "quinn"); err != nil {
			t.Fatal(err)
		}
	}

	at(svc, "2027-04-30")
	dash, err := svc.Dashboard(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	sysCov, tlptCov := dash.Coverage[0], dash.Coverage[1]
	if sysCov.Basis != CoverageTests || sysCov.Tested != 2 || sysCov.Covered != 2 || sysCov.Attested != 0 {
		t.Errorf("SYS coverage = %+v", sysCov)
	}
	if q1 := sysCov.Cells[0]; q1.Covered != 0 || q1.PlannedPct != 25 || q1.RAG != "Red" {
		t.Errorf("SYS at Q1 end = %+v (the tests were signed off in April)", q1)
	}
	if q2 := sysCov.Cells[1]; q2.Covered != 2 || q2.PlannedPct != 50 || q2.Due {
		t.Errorf("SYS at Q2 end = %+v", q2)
	}
	if tlptCov.N != 3 || tlptCov.InCycle != 2 || tlptCov.Covered != 2 {
		t.Errorf("TLPT coverage = %+v", tlptCov)
	}
	if q1, q4 := tlptCov.Cells[0], tlptCov.Cells[3]; q1.Planned != 2 || q1.Covered != 2 || q4.Planned != 3 || q4.Covered != 1 {
		t.Errorf("TLPT cells: Q1 %+v, Q4 %+v (CIF-PAY lapses on 2027-10-01; by Q4 every scope is planned)", q1, q4)
	}

	var dev Deviation
	for _, d := range dash.Deviations {
		if d.AttrRef == "RT-01" && d.StratumCode == "SYS" {
			dev = d
		}
	}
	if !dev.FullCoverage || dev.ExtendQuarter != 0 || dev.Conclusion != "1 deviation – full coverage, raise as findings" {
		t.Errorf("full-coverage deviation = %+v", dev)
	}

	e, err := svc.Export(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.CoverageBasis != CoverageTests || len(e.TestTypes) != 6 || e.Tests[0].TestType == "" || e.Strata[1].CycleYears != 3 || e.Strata[1].InCycle != 1 {
		t.Errorf("export lacks the plan type settings: basis %q types %v strata %+v", e.CoverageBasis, e.TestTypes, e.Strata[1])
	}
	for _, it := range e.Population {
		if it.ItemRef == "CIF-PAY" && (it.LastTestedOn != "2024-10-01" || it.CycleDue != "2027-10-01") {
			t.Errorf("CIF-PAY export = %+v", it)
		}
	}
	ann, err := svc.AnnualReport(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var byType *ReportTable
	for _, s := range ann.Sections {
		for i := range s.Tables {
			if s.Tables[i].Caption == "Tests by type" {
				byType = &s.Tables[i]
			}
		}
	}
	if byType == nil || len(byType.Rows) != 6 || byType.Rows[0][1] != "1" || byType.Rows[2][0] != "Penetration test" || byType.Rows[2][3] != "1" {
		t.Errorf("tests by type = %+v", byType)
	}
	if !strings.Contains(ann.Limitations, "Every item due is tested") {
		t.Errorf("limitations = %q", ann.Limitations)
	}

	pack, err := svc.PackData(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if q1 := strings.Join(pack.Pack.Roadmap[1].Lines, " | "); !strings.Contains(q1, "Wave: 2 item(s) due") || !strings.Contains(q1, "2 test(s), selected") {
		t.Errorf("the roadmap describes a sampled, attested plan: %s", q1)
	}
	qr, err := svc.QuarterReport(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if next := qr.Sections[len(qr.Sections)-1].Body; !strings.Contains(next, "the wave is selected on") {
		t.Errorf("next quarter = %q", next)
	}

	clone, err := svc.Clone(p.ID, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if it := itemByRef(t, svc, clone.ID, "SYS-1"); it.LastTestedOn != "2027-04-28" {
		t.Errorf("the clone does not carry this year's test as the last test: %+v", it)
	}
	if it := itemByRef(t, svc, clone.ID, "CIF-DEP"); it.LastTestedOn != "2025-06-15" {
		t.Errorf("the clone lost CIF-DEP's last TLPT: %+v", it)
	}
}

// passFor passes every attribute that applies to the stratum, apart from the
// ones listed.
func passFor(p Plan, stratum string, except map[string]string) []ResultInput {
	set := map[string]string{}
	for _, a := range p.Attributes {
		if len(a.AppliesTo) == 0 || slices.Contains(a.AppliesTo, stratum) {
			set[a.AttrRef] = ResultPass
		}
	}
	for k, v := range except {
		set[k] = v
	}
	return results(p, set)
}

func TestFreezeStillNeedsAWaveForAnItemDueInTheCycle(t *testing.T) {
	svc := newTestService(t)
	at(svc, "2026-12-01")
	p, err := svc.Create(CreateInput{Type: "resilience-testing", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	paste := "item_ref,stratum,wave,last_tested_on\nSYS-1,SYS,Q1,\nCIF-PAY,TLPT,,2024-10-01\nCIF-DEP,TLPT,,2025-06-15\n"
	if diff, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: paste}, "alice"); err != nil || len(diff.Errors) > 0 {
		t.Fatalf("%+v %v", diff, err)
	}
	codes := []string{}
	for _, f := range Lint(mustGet(t, svc, p.ID), mustPopulation(t, svc, p.ID)) {
		codes = append(codes, f.Code)
	}
	if !slices.Contains(codes, "in_cycle") || !slices.Contains(codes, "no_wave") {
		t.Errorf("lint codes = %v", codes)
	}
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(p.ID, "bob", ""); err != nil {
		t.Fatal(err)
	}
	_, err = svc.FreezePopulation(p.ID, "alice")
	wantInvalid(t, err, "CIF-PAY")
}

func TestCycleDates(t *testing.T) {
	st := Stratum{Code: "TLPT", CycleYears: 3}
	it := PopulationItem{LastTestedOn: "2024-10-01"}
	if got := cycleDue(st, it); got != "2027-10-01" {
		t.Errorf("due = %s", got)
	}
	if !dueInPeriod(st, it, "2027-12-31") || dueInPeriod(st, it, "2027-09-30") {
		t.Error("due in period is wrong at the boundary")
	}
	if !inCycleAt(st, it, "2027-09-30") || inCycleAt(st, it, "2027-10-01") {
		t.Error("in cycle is wrong at the boundary")
	}
	if !dueInPeriod(st, PopulationItem{}, "2027-12-31") || inCycleAt(st, PopulationItem{}, "2027-01-01") {
		t.Error("an item never tested is due and not in its cycle")
	}
	annual := Stratum{Code: "SYS"}
	if !dueInPeriod(annual, it, "2027-12-31") || inCycleAt(annual, it, "2027-01-01") || cycleDue(annual, it) != "" {
		t.Error("an annual stratum has no cycle")
	}
}

func TestPopulationCSVCarriesLastTestAndSource(t *testing.T) {
	strata := []Stratum{{Code: "T1", Waves: [4]float64{1, 0, 0, 0}}}
	parsed, err := parsePopulationCSV("item_ref,stratum,Last test date,source_kind,source_ref\nA-1,T1,2025-01-31,audit_finding,12\nA-2,T1,31/01/2025,,\n", strata)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.rows) != 1 || parsed.rows[0].item.LastTestedOn != "2025-01-31" || parsed.rows[0].item.SourceRef != "12" {
		t.Errorf("rows = %+v", parsed.rows)
	}
	if len(parsed.errs) != 1 || !strings.Contains(parsed.errs[0].Message, "last tested on") {
		t.Errorf("errors = %+v", parsed.errs)
	}
	out := populationCSV([]PopulationItem{{ItemRef: "A-1", StratumCode: "T1", LastTestedOn: "2025-01-31", SourceKind: "audit_finding", SourceRef: "12"}})
	if !strings.HasSuffix(strings.TrimSpace(out), "2025-01-31,audit_finding,12") {
		t.Errorf("csv = %s", out)
	}
}

func TestProposeRouteAnswersTheCSV(t *testing.T) {
	r, svc := newTestRouter(t, "alice")
	svc.now = func() time.Time { return time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC) }
	p, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if w := do(t, r, http.MethodPost, "/assurance-plans/"+itoa(p.ID)+"/population/propose?source=policies", nil); w.Code != http.StatusBadRequest {
		t.Errorf("propose without a mapping = %d %s", w.Code, w.Body)
	}
	page := do(t, r, http.MethodGet, "/assurance-plans/"+itoa(p.ID), nil).Body.String()
	if strings.Contains(page, `id="popPropose"`) {
		t.Error("a plan without a source offers a proposal")
	}
	road, _ := svc.Create(CreateInput{Type: "remediation-roadmap", PeriodStart: "2027-01-01"}, "alice")
	page = do(t, r, http.MethodGet, "/assurance-plans/"+itoa(road.ID), nil).Body.String()
	for _, want := range []string{`id="popPropose" data-source="findings"`, "Propose from open audit findings", "Plan type settings", "Test coverage against plan", "Takes from the proposal"} {
		if !strings.Contains(page, want) {
			t.Errorf("the roadmap's page lacks %q", want)
		}
	}
}
