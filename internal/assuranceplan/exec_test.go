package assuranceplan

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"grc/internal/auditfinding"
	"grc/internal/controlcatalog"
	"grc/internal/riskregister"
	"grc/internal/securitynfr"

	"github.com/gin-gonic/gin"
)

// newIntegratedService is the service with Audit Findings, the risk register
// and both catalogs over the same database, as app.go wires it.
func newIntegratedService(t *testing.T) (*Service, Integrations) {
	t.Helper()
	conn := newTestDB(t)
	in := Integrations{
		Findings: auditfinding.NewService(auditfinding.NewSQLiteRepository(conn)),
		Risks:    riskregister.NewService(riskregister.NewSQLiteRepository(conn)),
		Controls: controlcatalog.NewService(controlcatalog.NewSQLiteRepository(conn), ""),
		NFRs:     securitynfr.NewService(securitynfr.NewSQLiteRepository(conn), ""),
	}
	return newTestServiceOn(conn).WithIntegrations(in), in
}

// testingQ1 is the frozen Appendix A plan with Q1 drawn (13 Tier 1, 4 Tier 2)
// and its tests open.
func testingQ1(t *testing.T, svc *Service) (Plan, []Test) {
	t.Helper()
	p := frozenExample(t, svc)
	if _, err := svc.SelectQuarter(p.ID, 1, "alice"); err != nil {
		t.Fatalf("SelectQuarter: %v", err)
	}
	tests, err := svc.CreateTests(p.ID, CreateTestsInput{Quarter: 1, Tester: "tess", EvidenceRequestedOn: "2027-03-02"}, "alice")
	if err != nil {
		t.Fatalf("CreateTests: %v", err)
	}
	return mustGet(t, svc, p.ID), tests
}

func attrByRef(t *testing.T, p Plan, ref string) Attribute {
	t.Helper()
	for _, a := range p.Attributes {
		if a.AttrRef == ref {
			return a
		}
	}
	t.Fatalf("no attribute %s", ref)
	return Attribute{}
}

func firstTestIn(t *testing.T, tests []Test, stratum string, skip int) Test {
	t.Helper()
	for _, x := range tests {
		if x.StratumCode == stratum {
			if skip == 0 {
				return x
			}
			skip--
		}
	}
	t.Fatalf("no %s test", stratum)
	return Test{}
}

// results sets the listed attributes and leaves the rest as they are.
func results(p Plan, set map[string]string) []ResultInput {
	out := []ResultInput{}
	for _, a := range p.Attributes {
		if v, ok := set[a.AttrRef]; ok {
			out = append(out, ResultInput{AttributeID: a.ID, Result: v, Note: "note on " + a.AttrRef})
		}
	}
	return out
}

func allPass(p Plan, except map[string]string) []ResultInput {
	set := map[string]string{}
	for _, a := range p.Attributes {
		set[a.AttrRef] = ResultPass
	}
	for k, v := range except {
		set[k] = v
	}
	return results(p, set)
}

func resultFor(t *testing.T, tt Test, attrID int64) Result {
	t.Helper()
	for _, r := range tt.Results {
		if r.AttributeID == attrID {
			return r
		}
	}
	t.Fatalf("%s has no result for attribute %d", tt.TestRef, attrID)
	return Result{}
}

func TestOverallResultThresholds(t *testing.T) {
	pr := DefaultParameters()
	for _, c := range []struct {
		fails, tested int
		want          string
	}{{0, 0, OverallNotStarted}, {0, 16, OverallEffective}, {1, 16, OverallPartial}, {2, 16, OverallPartial},
		{3, 16, OverallIneffective}, {9, 16, OverallIneffective}} {
		if got := OverallResult(c.fails, c.tested, pr); got != c.want {
			t.Errorf("%d fails of %d tested = %q, want %q", c.fails, c.tested, got, c.want)
		}
	}
	pr.ResultPartialMin, pr.ResultIneffectiveMin = 2, 4
	if got := OverallResult(1, 10, pr); got != OverallEffective {
		t.Errorf("with the partial threshold at 2, one fail = %q", got)
	}
	if got := OverallResult(4, 10, pr); got != OverallIneffective {
		t.Errorf("with the ineffective threshold at 4, four fails = %q", got)
	}
}

func TestDeviationRuleConclusions(t *testing.T) {
	pr := DefaultParameters()
	for _, c := range []struct {
		fails, tested int
		kind, text    string
	}{{0, 0, KindNotTested, ConclusionNotTested}, {0, 25, KindEffective, ConclusionEffective},
		{1, 25, KindExtend, "1 deviation – extend sample"}, {2, 25, KindNotEffective, ConclusionNotEffective},
		{5, 25, KindNotEffective, ConclusionNotEffective}} {
		kind, text := Conclude(c.fails, c.tested, pr)
		if kind != c.kind || text != c.text {
			t.Errorf("%d deviations in %d = %s %q, want %s %q", c.fails, c.tested, kind, text, c.kind, c.text)
		}
	}
	pr.DeviationThematicAt = 3
	if kind, text := Conclude(2, 25, pr); kind != KindExtend || text != "2 deviations – extend sample" {
		t.Errorf("with the thematic threshold at 3, two deviations = %s %q", kind, text)
	}
	if got := ExtendBy(7, 50); got != 4 {
		t.Errorf("+50%% of 7 = %d, want 4", got)
	}
	if got := ExtendBy(13, 50); got != 7 {
		t.Errorf("+50%% of 13 = %d, want 7", got)
	}
	if got := ExtendBy(0, 50); got != 0 {
		t.Errorf("+50%% of none = %d", got)
	}
}

func TestCoverageRAGAndDates(t *testing.T) {
	for _, c := range []struct {
		planned, actual float64
		due             bool
		want            string
	}{{35, 40, true, "Green"}, {35, 35, true, "Green"}, {35, 30, true, "Amber"}, {35, 25, true, "Amber"},
		{35, 24.9, true, "Red"}, {35, 0, false, "Not due"}} {
		if got := RAG(c.planned, c.actual, 10, c.due); got != c.want {
			t.Errorf("RAG(%v, %v, due=%v) = %s, want %s", c.planned, c.actual, c.due, got, c.want)
		}
	}
	for q, want := range []string{"2027-03-31", "2027-06-30", "2027-09-30", "2027-12-31"} {
		if got, _ := QuarterEnd("2027-01-01", q+1); got != want {
			t.Errorf("Q%d end = %s, want %s", q+1, got, want)
		}
	}
	cal := NewCalendar("2027-03-12")
	if got := EvidenceDue("2027-03-02", 10, cal); got != "2027-03-17" {
		t.Errorf("evidence due over a holiday = %s, want 2027-03-17", got)
	}
	if got := EvidenceDue("2027-03-02", 10, NewCalendar("")); got != "2027-03-16" {
		t.Errorf("evidence due = %s, want 2027-03-16", got)
	}
	if !EvidenceLate("2027-03-16", "2027-03-17", "2027-03-10") || EvidenceLate("2027-03-16", "2027-03-16", "2027-04-01") ||
		!EvidenceLate("2027-03-16", "", "2027-03-17") || EvidenceLate("2027-03-16", "", "2027-03-16") {
		t.Error("evidence lateness is wrong")
	}
	if got := testRef("2027-01-01", 3, 7); got != "T27-Q3-007" {
		t.Errorf("test ref = %s", got)
	}
}

func TestTheExampleCarriesItsTestScriptAndMarksUnresolvedReferences(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := createExample(t, svc, "alice")
	if len(p.Attributes) != 16 {
		t.Fatalf("example has %d attributes, want 16", len(p.Attributes))
	}
	a := attrByRef(t, p, "NFR-04")
	if a.Domain != "Access management & recertification" || !slices.Equal(a.ControlIDs, []string{"AC-2", "AC-6"}) || a.Method != MethodReperformance {
		t.Errorf("NFR-04 = %+v", a)
	}
	if !a.Checked || !slices.Equal(a.Unresolved, []string{"AC-2", "AC-6"}) {
		t.Errorf("with an empty control catalog NFR-04's controls should be unresolved: checked=%v %v", a.Checked, a.Unresolved)
	}
	found := 0
	for _, f := range Lint(p, nil) {
		if f.Code == "attribute_refs" && f.Severity == LintAdvisory {
			found++
		}
	}
	if found != 16 {
		t.Errorf("the gate lists %d attributes with unresolved references, want 16", found)
	}

	plain := createExample(t, newTestService(t), "alice")
	if b := attrByRef(t, plain, "NFR-04"); b.Checked || len(b.Unresolved) != 0 {
		t.Errorf("without the catalogs references are not checked, and not claimed unresolved either: %+v", b)
	}
}

func TestSeedAttributesFromTheCatalogs(t *testing.T) {
	svc, in := newIntegratedService(t)
	if _, err := in.NFRs.Import([]byte(`{"1": {"Summary": "0.1 Solution architecture must be documented", "ID": 0.1,
		"Issue Type": "Control", "Description": "", "NIST Mapping": "PL-8, CM-8", "Implementation": "Document the architecture."}}`), true); err != nil {
		t.Fatalf("import NFRs: %v", err)
	}
	if _, err := in.Controls.Import([]byte(`{"AC-2: Account Management": {"nist": {"control_id": "AC-2", "type": "Control",
		"name": "Account Management", "nist_requirements": "a. Define and document the types of accounts allowed."}}}`), true); err != nil {
		t.Fatalf("import controls: %v", err)
	}
	p, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01", PeriodEnd: "2027-12-31"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.SeedAttributes(p.ID, SeedInput{NFRKeys: []string{"0.1"}, ControlIDs: []string{"ac-2", "ZZ-9"}}, "alice")
	if err != nil {
		t.Fatalf("SeedAttributes: %v", err)
	}
	if !slices.Equal(res.Added, []string{"NFR-0.1", "AC-2"}) || !slices.Equal(res.NotFound, []string{"ZZ-9"}) {
		t.Fatalf("seed = added %v, not found %v", res.Added, res.NotFound)
	}
	nfr := attrByRef(t, res.Plan, "NFR-0.1")
	if nfr.NFRKey != "1" || nfr.Requirement != "Solution architecture must be documented" || nfr.PassCriteria != "Document the architecture." ||
		!slices.Equal(nfr.ControlIDs, []string{"PL-8", "CM-8"}) || !slices.Equal(nfr.Unresolved, []string{"PL-8", "CM-8"}) {
		t.Errorf("NFR attribute = %+v", nfr)
	}
	ctl := attrByRef(t, res.Plan, "AC-2")
	if ctl.Requirement != "Account Management" || len(ctl.Unresolved) != 0 || !ctl.Checked {
		t.Errorf("control attribute = %+v", ctl)
	}
	again, err := svc.SeedAttributes(p.ID, SeedInput{NFRKeys: []string{"NFR-0.1"}}, "alice")
	if err != nil || !slices.Equal(again.Skipped, []string{"NFR-0.1"}) || len(again.Added) != 0 {
		t.Errorf("a second seed of the same NFR = %+v, %v", again, err)
	}
	_, err = svc.SeedAttributes(p.ID, SeedInput{}, "alice")
	wantInvalid(t, err, "at least one")
}

func TestTestsAreOpenedPerSelectionWithTheirEvidenceSLA(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p, tests := testingQ1(t, svc)
	if len(tests) != 17 {
		t.Fatalf("Q1 opened %d tests, want one per pick (17)", len(tests))
	}
	if tests[0].TestRef != "T27-Q1-001" || tests[16].TestRef != "T27-Q1-017" {
		t.Errorf("test refs %s .. %s", tests[0].TestRef, tests[16].TestRef)
	}
	for _, x := range tests {
		if x.Tester != "tess" || x.EvidenceDueOn != "2027-03-16" || x.Overall != OverallNotStarted || x.Pending != 16 {
			t.Fatalf("new test = %+v", x)
		}
	}
	_, err := svc.CreateTests(p.ID, CreateTestsInput{Quarter: 1}, "alice")
	wantConflict(t, err, "already has a test")
	_, err = svc.CreateTests(p.ID, CreateTestsInput{Quarter: 2}, "alice")
	wantConflict(t, err, "no selection")

	draft := createExample(t, svc, "alice")
	_, err = svc.CreateTests(draft.ID, CreateTestsInput{Quarter: 1}, "alice")
	wantConflict(t, err, "approved")
}

func TestRecordingResultsQAAndTheirRules(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p, tests := testingQ1(t, svc)
	x := firstTestIn(t, tests, "T1", 0)

	res, err := svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", EvidenceRequestedOn: "2027-03-02",
		Results: results(p, map[string]string{"NFR-01": ResultPass, "NFR-02": "n/a", "NFR-04": ResultFail})}, "tess")
	if err != nil {
		t.Fatalf("SaveTest: %v", err)
	}
	got := res.Test
	if got.Passes != 1 || got.Fails != 1 || got.NotApplicable != 1 || got.Tested != 2 || got.Pending != 13 || got.Overall != OverallPartial {
		t.Errorf("after one pass, one fail, one N/A: %+v", got)
	}
	if !strings.Contains(res.Changes, "NFR-04 blank → Fail") {
		t.Errorf("changes = %q", res.Changes)
	}

	_, err = svc.SaveTest(p.ID, x.ID, TestInput{Results: []ResultInput{{AttributeID: attrByRef(t, p, "NFR-05").ID, Result: "Maybe"}}}, "tess")
	wantInvalid(t, err, "Pass, Fail, N/A")
	_, err = svc.SignOffQA(p.ID, x.ID, "quinn")
	wantInvalid(t, err, "fieldwork")

	if _, err := svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", EvidenceRequestedOn: "2027-03-02", EvidenceReceivedOn: "2027-03-10",
		FieldworkCompletedOn: "2027-03-20", EvidenceSummary: "Recertification last run in 2025.",
		Results: allPass(p, map[string]string{"NFR-02": ResultNA, "NFR-04": ResultFail})}, "tess"); err != nil {
		t.Fatal(err)
	}

	svc.SetRequireSeparateApprover(true)
	_, err = svc.SignOffQA(p.ID, x.ID, "tess")
	wantConflict(t, err, "someone else")
	qa, err := svc.SignOffQA(p.ID, x.ID, "quinn")
	if err != nil || qa.Test.QAReviewer != "quinn" || qa.Test.QADate != "2026-11-06" || qa.Warning != "" {
		t.Fatalf("QA by another person = %+v, %v", qa, err)
	}
	_, err = svc.SignOffQA(p.ID, x.ID, "quinn")
	wantConflict(t, err, "signed off")

	after, err := svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", EvidenceRequestedOn: "2027-03-02", EvidenceReceivedOn: "2027-03-10",
		FieldworkCompletedOn: "2027-03-20", EvidenceSummary: "Recertification last run in 2025; no 2026 run.",
		Results: allPass(p, map[string]string{"NFR-02": ResultNA, "NFR-04": ResultFail})}, "tess")
	if err != nil || !after.QAWithdrawn || after.Test.QADate != "" {
		t.Fatalf("an edit after QA should withdraw the sign-off: %+v, %v", after, err)
	}

	svc.SetRequireSeparateApprover(false)
	single, err := svc.SignOffQA(p.ID, x.ID, "tess")
	if err != nil || single.Warning == "" || single.Test.QANote == "" {
		t.Errorf("single-user QA by the tester should pass with a warning: %+v, %v", single, err)
	}

	history, _ := svc.History(p.ID)
	var events []string
	for _, h := range history {
		events = append(events, h.Event)
	}
	for _, want := range []string{"test", "qa"} {
		if !slices.Contains(events, want) {
			t.Errorf("history has no %q event: %v", want, events)
		}
	}
}

func TestRaiseFindingCreatesALinkedAuditFinding(t *testing.T) {
	svc, in := newIntegratedService(t)
	p, tests := testingQ1(t, svc)
	x := firstTestIn(t, tests, "T1", 0)
	nfr04 := attrByRef(t, p, "NFR-04")
	saved, err := svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", FieldworkCompletedOn: "2027-03-20",
		EvidenceSummary: "Last recertification of the ledger's accounts was in 2025.",
		Results:         []ResultInput{{AttributeID: nfr04.ID, Result: ResultFail, Note: "Three leavers still active."}}}, "tess")
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, saved.Test, nfr04.ID)
	item := itemByRef(t, svc, p.ID, x.ItemRef)

	_, err = svc.RaiseFinding(p.ID, r.ID, RaiseInput{}, "tess")
	wantInvalid(t, err, "severity")
	raised, err := svc.RaiseFinding(p.ID, r.ID, RaiseInput{Severity: "High"}, "tess")
	if err != nil {
		t.Fatalf("RaiseFinding: %v", err)
	}
	f, err := in.Findings.Get(raised.Finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantOrigin := "AP-2027-001 / " + x.TestRef + " / NFR-04"
	for _, c := range []struct{ field, got, want string }{
		{"source", f.Source, "Second Line Review"},
		{"engagement", f.Engagement, "AP-2027-001 " + p.Title},
		{"origin_ref", f.OriginRef, wantOrigin},
		{"linked_controls", f.LinkedControls, "AC-2, AC-6"},
		{"deficiency_type", f.DeficiencyType, "Operating Effectiveness"},
		{"severity", f.Severity, "High"},
		{"owner", f.Owner, item.Owner},
		{"auditor", f.Auditor, "tess"},
		{"identified_date", f.IdentifiedDate, "2027-03-20"},
		{"due_date", f.DueDate, "2027-06-18"},
		{"status", f.Status, auditfinding.StatusOpen},
	} {
		if c.got != c.want {
			t.Errorf("finding %s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if !strings.Contains(f.Condition, "2025") || !strings.Contains(f.Condition, "Three leavers") || !strings.Contains(f.Criteria, nfr04.Requirement) ||
		!strings.Contains(f.Criteria, nfr04.PassCriteria) {
		t.Errorf("condition %q / criteria %q", f.Condition, f.Criteria)
	}
	if raised.Result.FindingRef != f.Reference || raised.Result.Severity != "High" {
		t.Errorf("result link = %+v", raised.Result)
	}

	// An edit in Audit Findings keeps the origin.
	f.Notes = "edited"
	if _, err := in.Findings.Update(f.ID, f, "auditor"); err != nil {
		t.Fatal(err)
	}
	f.OriginRef = "tampered"
	if _, err := in.Findings.Update(f.ID, f, "auditor"); err != nil {
		t.Fatal(err)
	}
	if again, _ := in.Findings.Get(f.ID); again.OriginRef != wantOrigin {
		t.Errorf("origin after edits = %q", again.OriginRef)
	}

	_, err = svc.RaiseFinding(p.ID, r.ID, RaiseInput{Severity: "High"}, "tess")
	wantConflict(t, err, "already raised")
	_, err = svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", FieldworkCompletedOn: "2027-03-20",
		Results: []ResultInput{{AttributeID: nfr04.ID, Result: ResultPass}}}, "tess")
	wantConflict(t, err, "stays a Fail")
	pass := attrByRef(t, p, "NFR-01")
	saved, _ = svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", FieldworkCompletedOn: "2027-03-20",
		Results: []ResultInput{{AttributeID: pass.ID, Result: ResultPass}}}, "tess")
	_, err = svc.RaiseFinding(p.ID, resultFor(t, saved.Test, pass.ID).ID, RaiseInput{Severity: "Low"}, "tess")
	wantConflict(t, err, "raised on a Fail")

	dash, err := svc.Dashboard(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	high := dash.BySeverity[slices.IndexFunc(dash.BySeverity, func(s SeverityCount) bool { return s.Severity == "High" })]
	if high.Raised != 1 || high.Open != 1 || len(dash.Findings) != 1 || dash.Findings[0].Reference != f.Reference {
		t.Errorf("dashboard findings = %+v %+v", high, dash.Findings)
	}

	if err := in.Findings.Delete(f.ID); err != nil {
		t.Fatal(err)
	}
	dash, _ = svc.Dashboard(p.ID)
	if dash.Missing != 1 {
		t.Errorf("a deleted finding should show as missing, got %d", dash.Missing)
	}
	if _, err := svc.RaiseFinding(p.ID, r.ID, RaiseInput{Severity: "Medium", Owner: "IAM lead"}, "tess"); err != nil {
		t.Errorf("a result whose finding was deleted should raise again: %v", err)
	}
}

func TestDeviationRuleExtendsThenPushesAThematicRisk(t *testing.T) {
	svc, in := newIntegratedService(t)
	p, tests := testingQ1(t, svc)
	nfr04, nfr01 := attrByRef(t, p, "NFR-04"), attrByRef(t, p, "NFR-01")
	fail := func(x Test, a Attribute) {
		t.Helper()
		if _, err := svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", Results: []ResultInput{{AttributeID: a.ID, Result: ResultFail}}}, "tess"); err != nil {
			t.Fatal(err)
		}
	}
	fail(firstTestIn(t, tests, "T1", 0), nfr04)
	fail(firstTestIn(t, tests, "T2", 0), nfr01)

	devs, err := svc.Deviations(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	find := func(ref, stratum string) Deviation {
		for _, d := range devs {
			if d.AttrRef == ref && d.StratumCode == stratum {
				return d
			}
		}
		t.Fatalf("no deviation row for %s %s", ref, stratum)
		return Deviation{}
	}
	if d := find("NFR-04", "T1"); d.Kind != KindExtend || d.ExtendQuarter != 2 || d.ExtendBy != 7 {
		t.Errorf("one T1 deviation should extend Q2 by half of its 13 tests: %+v", d)
	}
	if d := find("NFR-01", "T2"); d.Kind != KindExtend || d.ExtendBy != 4 {
		t.Errorf("one T2 deviation should extend Q2 by half of its 7 tests: %+v", d)
	}
	if d := find("NFR-05", "T1"); d.Kind != KindNotTested {
		t.Errorf("an attribute with no results is not tested: %+v", d)
	}
	_, err = svc.PushThematic(p.ID, ThematicInput{AttributeID: nfr04.ID, Stratum: "T1", Owner: "CISO", Likelihood: 3, Impact: 4}, "alice")
	wantConflict(t, err, "only a not-effective")

	fail(firstTestIn(t, tests, "T1", 1), nfr04)
	devs, _ = svc.Deviations(p.ID)
	if d := find("NFR-04", "T1"); d.Kind != KindNotEffective || d.Fails != 2 || len(d.Failures) != 2 {
		t.Fatalf("two T1 deviations = %+v", d)
	}
	_, err = svc.PushThematic(p.ID, ThematicInput{AttributeID: nfr04.ID, Stratum: "T1", Owner: "CISO"}, "alice")
	wantInvalid(t, err, "1 to 5")
	pushed, err := svc.PushThematic(p.ID, ThematicInput{AttributeID: nfr04.ID, Stratum: "T1", Owner: "CISO", Likelihood: 3, Impact: 4}, "alice")
	if err != nil {
		t.Fatalf("PushThematic: %v", err)
	}
	risks, _ := in.Risks.List("", "")
	if len(risks) != 1 {
		t.Fatalf("register holds %d risks", len(risks))
	}
	rk := risks[0]
	if rk.RiskID != "AP-2027-001-NFR-04-T1" || rk.Owner != "CISO" || rk.InherentScore != 12 || rk.ResidualScore != 12 ||
		!strings.Contains(rk.Vulnerability, "2 deviation(s)") || !strings.Contains(rk.Notes, "AP-2027-001") || rk.CurrentControls != "AC-2, AC-6" {
		t.Errorf("risk = %+v", rk)
	}
	if pushed.Thematic.RiskRef != rk.RiskID {
		t.Errorf("thematic = %+v", pushed.Thematic)
	}
	_, err = svc.PushThematic(p.ID, ThematicInput{AttributeID: nfr04.ID, Stratum: "T1", Owner: "CISO", Likelihood: 3, Impact: 4}, "alice")
	wantConflict(t, err, "was pushed")
	devs, _ = svc.Deviations(p.ID)
	if d := find("NFR-04", "T1"); d.Thematic == nil || d.Thematic.RiskRef != rk.RiskID {
		t.Errorf("the deviation row should show its risk: %+v", d)
	}
}

func TestAttributesWithResultsCannotBeRemovedOrNarrowed(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p, tests := testingQ1(t, svc)
	nfr04 := attrByRef(t, p, "NFR-04")
	if _, err := svc.SaveTest(p.ID, firstTestIn(t, tests, "T1", 0).ID, TestInput{Tester: "tess",
		Results: []ResultInput{{AttributeID: nfr04.ID, Result: ResultPass}}}, "tess"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ReplaceAttributes(p.ID, p.Attributes, "alice")
	wantConflict(t, err, "change request")

	if _, err := svc.ChangeRequest(p.ID, "re-scope the test script", "alice"); err != nil {
		t.Fatal(err)
	}
	var without []Attribute
	for _, a := range p.Attributes {
		if a.AttrRef != "NFR-04" {
			without = append(without, a)
		}
	}
	_, err = svc.ReplaceAttributes(p.ID, without, "alice")
	wantInvalid(t, err, "has results")
	narrowed := slices.Clone(p.Attributes)
	for i := range narrowed {
		if narrowed[i].AttrRef == "NFR-04" {
			narrowed[i].AppliesTo = []string{"T2"}
		}
	}
	_, err = svc.ReplaceAttributes(p.ID, narrowed, "alice")
	wantInvalid(t, err, "keep applying to T1")

	edited := slices.Clone(p.Attributes)
	for i := range edited {
		if edited[i].AttrRef == "NFR-04" {
			edited[i].PassCriteria = "Recertified within 12 months."
		}
	}
	got, err := svc.ReplaceAttributes(p.ID, edited, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if a := attrByRef(t, got, "NFR-04"); a.ID != nfr04.ID || a.PassCriteria != "Recertified within 12 months." {
		t.Errorf("an edited attribute keeps its id so its results stay attached: %+v", a)
	}
	after, _ := svc.Tests(p.ID, 1)
	if r := resultFor(t, firstTestIn(t, after, "T1", 0), nfr04.ID); r.Result != ResultPass {
		t.Errorf("the result survived the edit as %q", r.Result)
	}
}

func TestDashboardCoverageAgainstPlan(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p, _ := testingQ1(t, svc)
	svc.now = func() time.Time { return time.Date(2027, 4, 15, 9, 0, 0, 0, time.UTC) }
	pop := mustPopulation(t, svc, p.ID)
	attested := 0
	for _, it := range pop {
		if it.StratumCode == "T1" && it.WaveQuarter == 1 && attested < 24 {
			date := "2027-02-20"
			if attested == 23 {
				date = "2027-04-02"
			}
			on := date
			if _, err := svc.UpdatePopulationItem(p.ID, it.ID, PopulationPatch{AttestedOn: &on}, "alice"); err != nil {
				t.Fatal(err)
			}
			attested++
		}
	}
	d, err := svc.Dashboard(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	t1 := d.Coverage[0]
	q1, q2 := t1.Cells[0], t1.Cells[1]
	// 23 of 80 attested by 31 March is 28.75% against 35%: 6.25 points behind.
	if t1.Code != "T1" || q1.Covered != 23 || q1.PlannedPct != 35 || q1.ActualPct != 28.75 || q1.RAG != "Amber" || q1.Planned != 28 {
		t.Errorf("T1 Q1 = %+v", q1)
	}
	if q2.RAG != "Not due" || q2.Covered != 24 {
		t.Errorf("T1 Q2 = %+v", q2)
	}
	if d.Coverage[1].Cells[0].RAG != "Red" {
		t.Errorf("T2 with nothing attested against 15%% should be Red: %+v", d.Coverage[1].Cells[0])
	}
	if t1.Attested != 24 || t1.AttestedPct != 30 {
		t.Errorf("T1 year-end tracker = %d (%v%%)", t1.Attested, t1.AttestedPct)
	}
	if d.Testing[0].Planned != 19 || d.Testing[0].Selected != 17 || d.Testing[0].Opened != 17 || d.TestsPlanned != 64 {
		t.Errorf("Q1 testing = %+v, planned %d", d.Testing[0], d.TestsPlanned)
	}
}

func TestCloneAndSnapshotCarryTheTestScriptButNotTheTests(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p, _ := testingQ1(t, svc)
	versions, _ := svc.Versions(p.ID)
	v, err := svc.Version(p.ID, versions[0].Number)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Attributes []Attribute `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(v.SnapshotJSON), &snap); err != nil || len(snap.Attributes) != 16 {
		t.Fatalf("snapshot attributes = %d, %v", len(snap.Attributes), err)
	}
	c, err := svc.Clone(p.ID, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Attributes) != 16 || c.Attributes[0].ID == p.Attributes[0].ID {
		t.Errorf("clone attributes = %d", len(c.Attributes))
	}
	tests, _ := svc.Tests(c.ID, 0)
	if len(tests) != 0 {
		t.Errorf("clone carried %d tests", len(tests))
	}
}

func newIntegratedRouter(t *testing.T, actor string) (*gin.Engine, *Service, Integrations) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc, in := newIntegratedService(t)
	h := NewHandler(svc, func(*gin.Context) string { return actor })
	r := gin.New()
	h.RegisterRoutes(r)
	h.RegisterAdminRoutes(r)
	return r, svc, in
}

func TestExecutionRoutes(t *testing.T) {
	r, svc, in := newIntegratedRouter(t, "tess")
	p := frozenExample(t, svc)
	if _, err := svc.SelectQuarter(p.ID, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	base := "/assurance-plans/" + strconv.FormatInt(p.ID, 10)
	w := do(t, r, http.MethodPost, base+"/tests", map[string]any{"quarter": 1, "evidence_requested_on": "2027-03-02"})
	if w.Code != http.StatusCreated {
		t.Fatalf("open tests = %d %s", w.Code, w.Body)
	}
	if w := do(t, r, http.MethodPost, base+"/tests", map[string]any{"quarter": 1}); w.Code != http.StatusConflict {
		t.Errorf("opening them twice = %d", w.Code)
	}
	if w := do(t, r, http.MethodGet, base+"/tests?quarter=5", nil); w.Code != http.StatusBadRequest {
		t.Errorf("quarter 5 = %d", w.Code)
	}
	var tests []Test
	w = do(t, r, http.MethodGet, base+"/tests?quarter=1", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &tests); err != nil || len(tests) != 17 || tests[0].Tester != "tess" {
		t.Fatalf("Q1 tests = %d, %v", len(tests), err)
	}
	p = mustGet(t, svc, p.ID)
	nfr04 := attrByRef(t, p, "NFR-04")
	testPath := base + "/tests/" + strconv.FormatInt(tests[0].ID, 10)

	if w := do(t, r, http.MethodPost, testPath+"/qa", nil); w.Code != http.StatusBadRequest {
		t.Errorf("QA before fieldwork = %d", w.Code)
	}
	var saved TestSaveResult
	w = do(t, r, http.MethodPut, testPath, TestInput{Tester: "tess", EvidenceRequestedOn: "2027-03-02",
		Results: []ResultInput{{AttributeID: nfr04.ID, Result: ResultFail, Note: "Leavers still active."}}})
	if err := json.Unmarshal(w.Body.Bytes(), &saved); w.Code != http.StatusOK || err != nil || saved.Test.Fails != 1 {
		t.Fatalf("save = %d %s", w.Code, w.Body)
	}
	resultID := resultFor(t, saved.Test, nfr04.ID).ID
	w = do(t, r, http.MethodPost, base+"/results/"+strconv.FormatInt(resultID, 10)+"/finding", RaiseInput{Severity: "High"})
	if w.Code != http.StatusCreated {
		t.Fatalf("raise = %d %s", w.Code, w.Body)
	}
	var raised RaiseResult
	_ = json.Unmarshal(w.Body.Bytes(), &raised)
	if f, err := in.Findings.Get(raised.Finding.ID); err != nil || f.OriginRef == "" {
		t.Errorf("raised finding = %+v, %v", f, err)
	}

	var dash DashboardView
	w = do(t, r, http.MethodGet, base+"/dashboard", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &dash); err != nil || len(dash.Findings) != 1 || dash.Testing[0].Opened != 17 {
		t.Errorf("dashboard = %d %s", w.Code, w.Body)
	}
	var devs []Deviation
	w = do(t, r, http.MethodGet, base+"/deviations", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &devs); err != nil || len(devs) == 0 {
		t.Errorf("deviations = %d %s", w.Code, w.Body)
	}
	if w := do(t, r, http.MethodPost, base+"/thematic", ThematicInput{AttributeID: nfr04.ID, Stratum: "T1", Owner: "CISO", Likelihood: 3, Impact: 3}); w.Code != http.StatusConflict {
		t.Errorf("pushing an extend conclusion = %d", w.Code)
	}
	if w := do(t, r, http.MethodPut, base+"/attributes", p.Attributes); w.Code != http.StatusConflict {
		t.Errorf("editing the approved test script = %d", w.Code)
	}
	if w := do(t, r, http.MethodPost, base+"/attributes/seed", SeedInput{ControlIDs: []string{"AC-2"}}); w.Code != http.StatusConflict {
		t.Errorf("seeding the approved test script = %d", w.Code)
	}
}

func TestPlanPageShowsTheTestScriptTestsAndDashboard(t *testing.T) {
	r, svc, _ := newIntegratedRouter(t, "")
	p := frozenExample(t, svc)
	if _, err := svc.SelectQuarter(p.ID, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	body := do(t, r, http.MethodGet, "/assurance-plans/"+strconv.FormatInt(p.ID, 10), nil).Body.String()
	for _, want := range []string{
		`id="tab-script"`, `id="tab-tests"`, `id="tab-dashboard"`, `id="tests-data"`,
		`data-open-tests="1"`, `Open 17 test(s) for Q1`,
		`Access management &amp; recertification`,
		`<span class="ap-st ap-st-unresolved">Unresolved</span>`,
		`<span class="ap-st ap-st-not-due">Not due</span>`,
		`<span class="ap-st ap-st-not-tested">Not tested</span>`,
		`1LoD coverage against plan`, `2LoD testing progress`, `Deviations by attribute and stratum`, `Findings raised from the plan`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plan page lacks %q", want)
		}
	}
	for _, class := range []string{".ap-st-pass", ".ap-st-fail", ".ap-st-n-a", ".ap-st-effective", ".ap-st-partially-effective", ".ap-st-ineffective",
		".ap-st-extend", ".ap-st-not-effective", ".ap-st-green", ".ap-st-amber", ".ap-st-red", ".ap-st-evidence-late", ".ap-st-awaiting-qa", ".ap-st-signed-off"} {
		if !strings.Contains(pageStyles, class) {
			t.Errorf("no colour pair for %s", class)
		}
	}
	rendered := body[:strings.Index(body, "<script")]
	if n := strings.Count(rendered, "<table"); n != strings.Count(rendered, "<thead>") {
		t.Errorf("%d server-rendered tables but %d explicit <thead>", n, strings.Count(rendered, "<thead>"))
	}
}
