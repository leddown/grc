package assuranceplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grc/internal/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	svc := NewService(NewSQLiteRepository(conn))
	svc.now = func() time.Time { return time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC) }
	return svc
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, ok := parseDate(s)
	if !ok {
		t.Fatalf("bad date %q", s)
	}
	return d
}

func stepByRef(t *testing.T, p Plan, ref string) Step {
	t.Helper()
	for _, s := range p.Steps {
		if s.StepRef == ref {
			return s
		}
	}
	t.Fatalf("plan has no step %s", ref)
	return Step{}
}

func wantConflict(t *testing.T, err error, fragment string) {
	t.Helper()
	var c *ConflictError
	if !errors.As(err, &c) {
		t.Fatalf("err=%v, want a ConflictError mentioning %q", err, fragment)
	}
	if !strings.Contains(c.Error(), fragment) {
		t.Fatalf("err=%q, want it to mention %q", c.Error(), fragment)
	}
}

func wantInvalid(t *testing.T, err error, fragment string) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err=%v, want a ValidationError mentioning %q", err, fragment)
	}
	if !strings.Contains(v.Error(), fragment) {
		t.Fatalf("err=%q, want it to mention %q", v.Error(), fragment)
	}
}

func createExample(t *testing.T, svc *Service, actor string) Plan {
	t.Helper()
	p, err := svc.Create(CreateInput{Example: "dora-nfr-2027"}, actor)
	if err != nil {
		t.Fatalf("Create example: %v", err)
	}
	return p
}

// ---- business days ----

func TestNextBusinessDaySkipsTheWeekend(t *testing.T) {
	cal := NewCalendar("")
	if got := formatDate(cal.NextBusinessDay(mustDate(t, "2027-02-26"))); got != "2027-03-01" {
		t.Errorf("after Fri 26 Feb 2027: %s, want Mon 1 Mar", got)
	}
	if got := formatDate(cal.NextBusinessDay(mustDate(t, "2027-05-29"))); got != "2027-05-31" {
		t.Errorf("after Sat 29 May 2027: %s, want Mon 31 May", got)
	}
	if got := formatDate(cal.Roll(mustDate(t, "2027-03-01"))); got != "2027-03-01" {
		t.Errorf("rolling a business day moved it to %s", got)
	}
}

func TestAddTenBusinessDaysCountsAConfiguredHoliday(t *testing.T) {
	start := mustDate(t, "2027-03-08")
	if got := formatDate(NewCalendar("").AddBusinessDays(start, 10)); got != "2027-03-22" {
		t.Errorf("+10 business days from Mon 8 Mar 2027 = %s, want 22 Mar", got)
	}
	cal := NewCalendar("2027-03-17, 2027-03-19")
	if got := formatDate(cal.AddBusinessDays(start, 10)); got != "2027-03-24" {
		t.Errorf("+10 business days over two holidays = %s, want 24 Mar", got)
	}
	if got := formatDate(cal.NextBusinessDay(mustDate(t, "2027-03-16"))); got != "2027-03-18" {
		t.Errorf("next business day after 16 Mar with 17 Mar a holiday = %s", got)
	}
}

func TestNonWorkingDatesRejectNonDates(t *testing.T) {
	if _, err := parseNonWorkingDates("2027-12-24, Christmas"); err == nil {
		t.Fatal("a non-date was accepted as a non-working date")
	}
	got, err := parseNonWorkingDates("2027-12-26\n2027-12-24;2027-12-24")
	if err != nil || strings.Join(got, ",") != "2027-12-24,2027-12-26" {
		t.Errorf("parse = %v, %v; want sorted and de-duplicated", got, err)
	}
}

// ---- the default cycle ----

func TestDefaultCycleReproducesTheHandBuilt2027Quarters(t *testing.T) {
	qs := DefaultQuarters(mustDate(t, "2027-01-01"), NewCalendar(""))
	want := [][6]string{
		{"2027-02-26", "2027-03-01", "2027-03-08", "2027-04-09", "2027-04-23", "2027-04-30"},
		{"2027-05-28", "2027-05-31", "2027-06-07", "2027-07-09", "2027-07-23", "2027-07-30"},
		{"2027-08-27", "2027-08-30", "2027-09-06", "2027-10-08", "2027-10-22", "2027-10-29"},
	}
	for i, w := range want {
		q := qs[i]
		got := [6]string{q.LOD1Deadline, q.SelectionDate, q.FieldworkStart, q.FieldworkEnd, q.FindingsAgreedBy, q.ReportDate}
		if got != w {
			t.Errorf("Q%d = %v, want %v", i+1, got, w)
		}
	}
	if qs[0].CommitteeLabel != "May 2027 (date TBC)" {
		t.Errorf("Q1 committee label = %q", qs[0].CommitteeLabel)
	}
	if qs[3].LOD1Deadline != "2027-11-26" {
		t.Errorf("Q4 deadline = %s, want 26 Nov", qs[3].LOD1Deadline)
	}
}

func TestDateExpressions(t *testing.T) {
	q := Quarter{Quarter: 1, LOD1Deadline: "2027-02-26", FindingsAgreedBy: "2027-04-23"}
	ctx := dateContext{cal: NewCalendar(""), anchors: map[string]string{"period_start": "2027-01-01", "period_end": "2027-12-31"},
		quarter: &q, quarters: map[int]Quarter{1: q}}
	for expr, want := range map[string]string{
		"period_start-53d":       "2026-11-09",
		"lod1_deadline+5bd":      "2027-03-05",
		"findings_agreed_by-5bd": "2027-04-16",
		"q1.lod1_deadline":       "2027-02-26",
		"period_end":             "2027-12-31",
		"2027-07-12":             "2027-07-12",
		"":                       "",
	} {
		got, err := ctx.eval(expr)
		if err != nil || got != want {
			t.Errorf("eval(%q) = %q, %v; want %q", expr, got, err, want)
		}
	}
	if _, err := ctx.eval("tuesday+1d"); err == nil {
		t.Error("an unknown anchor was accepted")
	}
}

func TestStepReferencesSortAsNumbers(t *testing.T) {
	steps := []Step{{StepRef: "O.1"}, {StepRef: "0.10"}, {StepRef: "4.13"}, {StepRef: "0.2"}, {StepRef: "4.8"}}
	sortStepsByRef(steps)
	var got []string
	for _, s := range steps {
		got = append(got, s.StepRef)
	}
	if strings.Join(got, " ") != "0.2 0.10 4.8 4.13 O.1" {
		t.Errorf("order = %v", got)
	}
}

// ---- seeding ----

func TestEveryTemplateLoadsAndInstantiates(t *testing.T) {
	list, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	keys := map[string]bool{}
	for _, tpl := range list {
		keys[tpl.Key] = true
		if _, err := instantiate(tpl, "", "", time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Errorf("template %s: %v", tpl.Key, err)
		}
	}
	for _, k := range []string{"controls-testing", "blank", "dora-nfr-2027"} {
		if !keys[k] {
			t.Errorf("template %s is missing", k)
		}
	}
}

func TestExamplePlanSeedsAppendixA(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	if p.Reference != "AP-2027-001" || p.Status != StatusDraft || p.Author != "alice" {
		t.Fatalf("ref=%s status=%s author=%s", p.Reference, p.Status, p.Author)
	}
	if p.PeriodStart != "2027-01-01" || p.PeriodEnd != "2027-12-31" || p.MobiliseStart != "2026-11-09" || p.ClosingDate != "2028-01-28" {
		t.Errorf("period %s..%s mobilise %s closing %s", p.PeriodStart, p.PeriodEnd, p.MobiliseStart, p.ClosingDate)
	}
	if p.OwnerRole != "PM (2LoD)" || p.AccountableRole != "CISO (2LoD)" || p.ApprovalForum != "ICT Risk Committee" {
		t.Errorf("roles %q / %q / %q", p.OwnerRole, p.AccountableRole, p.ApprovalForum)
	}
	if len(p.Quarters) != 4 || p.Quarters[3].FindingsAgreedBy != "2028-01-14" || p.Quarters[1].RetestDays != 3 || p.Quarters[3].OverheadDays != 15 {
		t.Errorf("quarters = %+v", p.Quarters)
	}
	// 22 fixed steps, 8 quarterly steps in Q1-Q3 and 7 in Q4 (the Q4 report is the annual conclusion).
	if len(p.Steps) != 22+8*3+7 {
		t.Errorf("steps = %d, want %d", len(p.Steps), 22+8*3+7)
	}
	for ref, want := range map[string][2]string{
		"0.1":  {"2026-11-09", "2026-11-13"},
		"0.8":  {"2026-12-16", "2026-12-16"},
		"1.1":  {"2027-01-11", "2027-02-26"},
		"1.3":  {"2027-03-01", "2027-03-01"},
		"1.4":  {"2027-03-08", "2027-03-22"},
		"1.6":  {"2027-04-12", "2027-04-16"},
		"1.7":  {"2027-04-19", "2027-04-23"},
		"1.8":  {"2027-04-30", "2027-04-30"},
		"4.5":  {"2027-12-01", "2027-12-17"},
		"4.8":  {"2027-10-04", "2027-12-17"},
		"4.10": {"2027-12-15", "2027-12-15"},
		"4.12": {"2028-01-28", "2028-01-28"},
		"O.4":  {"2027-01-04", "2027-12-31"},
	} {
		s := stepByRef(t, p, ref)
		if s.StartDate != want[0] || s.EndDate != want[1] {
			t.Errorf("step %s %s..%s, want %s..%s", ref, s.StartDate, s.EndDate, want[0], want[1])
		}
	}
	if s := stepByRef(t, p, "4.9"); s.Title != "Draft the 2028 plan" {
		t.Errorf("4.9 title = %q", s.Title)
	}
	if got := stepByRef(t, p, "4.8").Title; got != "Retests of remediated findings" {
		t.Errorf("4.8 is %q: the Q4 quarterly report must not be generated", got)
	}
	if p.Steps[0].StepRef != "0.1" || p.Steps[len(p.Steps)-1].StepRef != "O.4" {
		t.Errorf("first/last steps %s / %s", p.Steps[0].StepRef, p.Steps[len(p.Steps)-1].StepRef)
	}
	if len(p.RACIRoles) != 7 || len(p.RACI) == 0 || len(p.Cadence) != 5 || len(p.Escalations) != 4 {
		t.Errorf("governance roles=%d raci=%d cadence=%d escalations=%d", len(p.RACIRoles), len(p.RACI), len(p.Cadence), len(p.Escalations))
	}
	if blocking := BlockingFindings(Lint(p)); len(blocking) != 0 {
		t.Errorf("the example should pass the gate, got %+v", blocking)
	}
	if p.Parameters != DefaultParameters() {
		t.Errorf("parameters = %+v", p.Parameters)
	}
}

func TestControlsTestingTypeComputesDatesFromThePeriod(t *testing.T) {
	svc := newTestService(t)
	p, err := svc.Create(CreateInput{Type: "controls-testing", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.PeriodEnd != "2027-12-31" || p.MobiliseStart != "2026-11-09" || p.ClosingDate != "2028-01-28" {
		t.Errorf("period end %s, mobilise %s, closing %s", p.PeriodEnd, p.MobiliseStart, p.ClosingDate)
	}
	if p.Title != "2027 Security NFR Controls Testing Plan" {
		t.Errorf("title = %q", p.Title)
	}
	for ref, want := range map[string][2]string{
		"0.1":  {"2026-11-09", "2026-11-13"},
		"0.8":  {"2026-12-16", "2026-12-16"},
		"1.3":  {"2027-03-01", "2027-03-01"},
		"2.9":  {"2027-07-12", "2027-07-23"},
		"3.9":  {"2027-08-27", "2027-08-27"},
		"4.11": {"2027-12-31", "2027-12-31"},
		"4.12": {"2028-01-28", "2028-01-28"},
	} {
		s := stepByRef(t, p, ref)
		if s.StartDate != want[0] || s.EndDate != want[1] {
			t.Errorf("step %s %s..%s, want %s..%s", ref, s.StartDate, s.EndDate, want[0], want[1])
		}
	}
	if blocking := BlockingFindings(Lint(p)); len(blocking) != 0 {
		t.Errorf("a fresh controls testing plan should pass the gate, got %+v", blocking)
	}
}

func TestCreateRefusesUnknownTemplatesAndNumbersReferences(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Create(CreateInput{Type: "nonsense"}, "")
	wantInvalid(t, err, "unknown plan type")
	_, err = svc.Create(CreateInput{Example: "controls-testing"}, "")
	wantInvalid(t, err, "not an example")
	a, _ := svc.Create(CreateInput{Type: "blank", PeriodStart: "2027-01-01"}, "")
	b, _ := svc.Create(CreateInput{Type: "blank", PeriodStart: "2027-04-01"}, "")
	c, _ := svc.Create(CreateInput{Type: "blank"}, "")
	if a.Reference != "AP-2027-001" || b.Reference != "AP-2027-002" || c.Reference != "AP-2027-003" {
		t.Errorf("references %s %s %s", a.Reference, b.Reference, c.Reference)
	}
}

// ---- lifecycle ----

func TestThereIsNoEdgeFromDraftToApproved(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	_, err := svc.Approve(p.ID, "bob", "")
	wantConflict(t, err, "submit it for review")
	_, err = svc.Activate(p.ID, "bob")
	wantConflict(t, err, "only an approved plan")
}

func TestApprovalGateReturnsItsFindings(t *testing.T) {
	svc := newTestService(t)
	p, err := svc.Create(CreateInput{Type: "blank"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	_, err = svc.Approve(p.ID, "bob", "")
	var gate ApprovalError
	if !errors.As(err, &gate) {
		t.Fatalf("err=%v, want an ApprovalError", err)
	}
	codes := map[string]bool{}
	for _, f := range gate.Findings {
		codes[f.Code] = true
	}
	for _, want := range []string{"owner_missing", "accountable_missing", "forum_missing", "no_steps"} {
		if !codes[want] {
			t.Errorf("gate findings %v lack %s", codes, want)
		}
	}
}

func TestLintBlocksStepsWithoutRolesOrDatesAndQuartersWithoutDeadlines(t *testing.T) {
	p := Plan{OwnerRole: "PM", AccountableRole: "CISO", ApprovalForum: "Committee", PeriodStart: "2027-01-01", PeriodEnd: "2027-12-31",
		Steps: []Step{
			{StepRef: "1", Title: "No role", StepType: StepActivity, StartDate: "2027-01-04", EndDate: "2027-01-08"},
			{StepRef: "2", Title: "No dates", StepType: StepActivity, ResponsibleRole: "PM"},
			{StepRef: "3", Title: "Milestone", StepType: StepMilestone, ResponsibleRole: "PM"},
			{StepRef: "4", Title: "Fine", StepType: StepOngoing, ResponsibleRole: "PM", StartDate: "2027-01-04", EndDate: "2027-12-31"},
		},
		Quarters: []Quarter{{Quarter: 1, LOD1Deadline: "2027-02-26"}, {Quarter: 2, ReportDate: "2027-07-30"}},
	}
	got := map[string]int{}
	for _, f := range BlockingFindings(Lint(p)) {
		got[f.Code]++
	}
	want := map[string]int{"step_no_responsible": 1, "step_no_dates": 2, "quarter_no_report": 1, "quarter_no_deadline": 1}
	for code, n := range want {
		if got[code] != n {
			t.Errorf("%s: %d findings, want %d (all: %v)", code, got[code], n, got)
		}
	}
}

func TestSeparationOfDutiesUnderMultiUser(t *testing.T) {
	svc := newTestService(t)
	svc.SetRequireSeparateApprover(true)
	p := createExample(t, svc, "alice")
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Approve(p.ID, "ALICE", "")
	wantInvalid(t, err, "cannot also approve")
	res, err := svc.Approve(p.ID, "bob", "Minute ICTRC-2026-12")
	if err != nil {
		t.Fatalf("Approve by bob: %v", err)
	}
	if res.Plan.Status != StatusApproved || res.Plan.ApprovedBy != "bob" || res.Version.Label != "v1" || len(res.Warnings) != 0 {
		t.Errorf("status=%s by=%s version=%s warnings=%v", res.Plan.Status, res.Plan.ApprovedBy, res.Version.Label, res.Warnings)
	}
}

func TestSelfApprovalInSingleUserModeIsAWarning(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Approve(p.ID, "alice", "")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "separation of duties is not enforced") {
		t.Errorf("warnings = %v", res.Warnings)
	}
	history, _ := svc.History(p.ID)
	if !strings.Contains(history[0].Note, "separation of duties") {
		t.Errorf("the warning is not in the history: %+v", history[0])
	}
}

func approveExample(t *testing.T, svc *Service) Plan {
	t.Helper()
	p := createExample(t, svc, "alice")
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Approve(p.ID, "bob", "")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	return res.Plan
}

func TestDesignIsLockedAfterApprovalButStepStatusIsNot(t *testing.T) {
	svc := newTestService(t)
	svc.SetRequireSeparateApprover(true)
	p := approveExample(t, svc)
	if !p.DesignLocked {
		t.Fatal("an approved plan's design is not locked")
	}
	_, err := svc.ReplaceSteps(p.ID, p.Steps[:3], "alice")
	wantConflict(t, err, "change request")
	_, err = svc.ReplaceQuarters(p.ID, p.Quarters, "alice")
	wantConflict(t, err, "change request")
	_, err = svc.ReplaceGovernance(p.ID, Governance{}, "alice")
	wantConflict(t, err, "change request")
	edit := p
	edit.Title = "Changed"
	_, err = svc.UpdateHeader(p.ID, edit, "alice")
	wantConflict(t, err, "change request")
	_, err = svc.GenerateQuarters(p.ID, true, "alice")
	wantConflict(t, err, "change request")

	step := stepByRef(t, p, "0.1")
	got, err := svc.UpdateStepStatus(p.ID, step.ID, StepComplete, "carol")
	if err != nil {
		t.Fatalf("UpdateStepStatus: %v", err)
	}
	if stepByRef(t, got, "0.1").Status != StepComplete {
		t.Error("step status did not change")
	}
	_, err = svc.UpdateStepStatus(p.ID, step.ID, "Done-ish", "carol")
	wantInvalid(t, err, "step status must be")
}

func TestChangeRequestOpensARevisionAndKeepsTheApprovedSnapshot(t *testing.T) {
	svc := newTestService(t)
	svc.SetRequireSeparateApprover(true)
	p := approveExample(t, svc)
	if _, err := svc.Activate(p.ID, "alice"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	v1, err := svc.Version(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ChangeRequest(p.ID, "  ", "carol")
	wantInvalid(t, err, "needs a reason")
	p, err = svc.ChangeRequest(p.ID, "Three Tier 1 assets re-tiered", "carol")
	if err != nil {
		t.Fatalf("ChangeRequest: %v", err)
	}
	if p.Status != StatusActive || p.Revision != 2 || p.RevisionStatus != StatusDraft || p.DesignLocked {
		t.Fatalf("status=%s revision=%d %s locked=%v", p.Status, p.Revision, p.RevisionStatus, p.DesignLocked)
	}
	_, err = svc.ChangeRequest(p.ID, "again", "carol")
	wantConflict(t, err, "already has revision 2")

	steps := append(p.Steps, Step{StepRef: "4.14", Title: "Retest re-tiered assets", StepType: StepActivity,
		ResponsibleRole: "2LoD Test Lead & testers", StartDate: "2027-12-06", EndDate: "2027-12-10"})
	if p, err = svc.ReplaceSteps(p.ID, steps, "carol"); err != nil {
		t.Fatalf("ReplaceSteps in revision: %v", err)
	}

	after, err := svc.Version(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if after.SnapshotJSON != v1.SnapshotJSON || after.SnapshotSHA256 != v1.SnapshotSHA256 {
		t.Fatal("the approved snapshot changed when the revision was edited")
	}
	sum := sha256.Sum256([]byte(v1.SnapshotJSON))
	if hex.EncodeToString(sum[:]) != v1.SnapshotSHA256 {
		t.Error("the stored SHA-256 does not match the snapshot")
	}
	var snap Plan
	if err := json.Unmarshal([]byte(v1.SnapshotJSON), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Steps) != len(steps)-1 {
		t.Errorf("v1 snapshot has %d steps, want %d", len(snap.Steps), len(steps)-1)
	}

	_, err = svc.Close(p.ID, "", "alice")
	wantConflict(t, err, "approve or return it")

	if _, err := svc.Submit(p.ID, "carol"); err != nil {
		t.Fatalf("Submit revision: %v", err)
	}
	_, err = svc.Approve(p.ID, "carol", "")
	wantInvalid(t, err, "cannot also approve")
	res, err := svc.Approve(p.ID, "alice", "")
	if err != nil {
		t.Fatalf("Approve revision: %v", err)
	}
	if res.Version.Label != "v2" || res.Version.ChangeReason != "Three Tier 1 assets re-tiered" ||
		res.Plan.Status != StatusActive || res.Plan.RevisionStatus != "" || !res.Plan.DesignLocked {
		t.Errorf("version=%s reason=%q status=%s rev=%q locked=%v", res.Version.Label, res.Version.ChangeReason,
			res.Plan.Status, res.Plan.RevisionStatus, res.Plan.DesignLocked)
	}
	if _, err := svc.Close(p.ID, "Year complete", "alice"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = svc.UpdateStepStatus(p.ID, p.Steps[0].ID, StepComplete, "alice")
	wantConflict(t, err, "closed")
	err = svc.Delete(p.ID)
	wantConflict(t, err, "only a draft")
}

func TestReturnToDraftAndHistory(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ReplaceSteps(p.ID, p.Steps, "alice")
	wantConflict(t, err, "in review")
	p, err = svc.Return(p.ID, "Add the IA coordination dates", "bob")
	if err != nil || p.Status != StatusDraft {
		t.Fatalf("Return: %v, status %s", err, p.Status)
	}
	h, _ := svc.History(p.ID)
	if h[0].Event != "status" || h[0].FromValue != StatusInReview || h[0].ToValue != StatusDraft || h[0].Note == "" || h[0].Actor != "bob" {
		t.Errorf("latest history = %+v", h[0])
	}
}

func TestStepEditsAreValidatedAndSummarisedInHistory(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	dup := append([]Step{}, p.Steps...)
	dup[1].StepRef = dup[0].StepRef
	_, err := svc.ReplaceSteps(p.ID, dup, "alice")
	wantInvalid(t, err, "used twice")
	bad := append([]Step{}, p.Steps...)
	bad[0].StartDate = "9 Nov"
	_, err = svc.ReplaceSteps(p.ID, bad, "alice")
	wantInvalid(t, err, "not a date")

	steps := append([]Step{}, p.Steps[1:]...)
	steps[0].Title = "Extract the population (revised)"
	steps = append(steps, Step{StepRef: "O.5", Title: "Quarterly IA liaison", StepType: StepMilestone, ResponsibleRole: "PM (2LoD)", EndDate: "2027-06-30"})
	got, err := svc.ReplaceSteps(p.ID, steps, "alice")
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	if s := stepByRef(t, got, "O.5"); s.StartDate != "2027-06-30" || s.EndDate != "2027-06-30" || s.Status != StepNotStarted {
		t.Errorf("milestone normalised to %+v", s)
	}
	h, _ := svc.History(p.ID)
	if h[0].Subject != "steps" || h[0].Note != "added O.5; removed 0.1; changed 0.2" {
		t.Errorf("history note = %q", h[0].Note)
	}
}

func TestQuarterEditsKeepTheFrozenSelectionStamp(t *testing.T) {
	prev := []Quarter{{Quarter: 1, SelectedAt: "2027-03-01T09:00:00Z", SelectedBy: "tl"}}
	next := []Quarter{{Quarter: 1, LOD1Deadline: "2027-02-25", SelectedAt: "forged"}, {Quarter: 2, SelectedAt: "forged"}}
	keepSelection(prev, next)
	if next[0].SelectedAt != "2027-03-01T09:00:00Z" || next[0].SelectedBy != "tl" || next[1].SelectedAt != "" {
		t.Errorf("selection stamps = %+v", next)
	}
}

func TestGenerateQuartersResetsTheCycleAndKeepsStepStatus(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	steps := append([]Step{}, p.Steps...)
	for i := range steps {
		if steps[i].StepRef == "1.1" {
			steps[i].Status = StepInProgress
		}
	}
	if _, err := svc.ReplaceSteps(p.ID, steps, "alice"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GenerateQuarters(p.ID, true, "alice")
	if err != nil {
		t.Fatalf("GenerateQuarters: %v", err)
	}
	if got.Quarters[0].LOD1WindowStart != "2027-01-01" || got.Quarters[3].FieldworkEnd != "2028-01-07" {
		t.Errorf("Q1 window %s, Q4 fieldwork end %s", got.Quarters[0].LOD1WindowStart, got.Quarters[3].FieldworkEnd)
	}
	if got.Quarters[3].OverheadDays != 15 {
		t.Errorf("Q4 overhead = %g; regenerating the dates must keep retest and overhead days", got.Quarters[3].OverheadDays)
	}
	if s := stepByRef(t, got, "1.1"); s.Status != StepInProgress || s.StartDate != "2027-01-01" {
		t.Errorf("1.1 = %+v", s)
	}
	if len(got.Steps) != len(p.Steps) {
		t.Errorf("steps %d -> %d", len(p.Steps), len(got.Steps))
	}
}

func TestGovernanceIsValidated(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	_, err := svc.ReplaceGovernance(p.ID, Governance{RACIRoles: []string{"CISO"}, RACI: []RACIRow{{Activity: "Approve", Cells: map[string]string{"CISO": "X"}}}}, "alice")
	wantInvalid(t, err, "is not one of")
	got, err := svc.ReplaceGovernance(p.ID, Governance{
		RACIRoles: []string{"CISO", " CISO ", "PM"},
		RACI:      []RACIRow{{Activity: "Approve", Cells: map[string]string{"CISO": "r/a", "Ghost": "R"}}, {Activity: " "}},
		Cadence:   []Cadence{{Forum: "Committee", Frequency: "Quarterly"}, {}},
	}, "alice")
	if err != nil {
		t.Fatalf("ReplaceGovernance: %v", err)
	}
	if len(got.RACIRoles) != 2 || len(got.RACI) != 1 || got.RACI[0].Cells["CISO"] != "A/R" || len(got.RACI[0].Cells) != 1 || len(got.Cadence) != 1 {
		t.Errorf("governance = roles %v raci %+v cadence %+v", got.RACIRoles, got.RACI, got.Cadence)
	}
}

func TestHeaderUpdateValidatesAndCannotSetLifecycleFields(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	edit := p
	edit.Status = StatusApproved
	edit.Author = "mallory"
	edit.ApprovedBy = "mallory"
	edit.Title = "2027 plan (revised)"
	edit.NonWorkingDates = "2027-12-24, 2027-12-27"
	edit.Parameters.UtilisationThreshold = 0.9
	got, err := svc.UpdateHeader(p.ID, edit, "alice")
	if err != nil {
		t.Fatalf("UpdateHeader: %v", err)
	}
	if got.Status != StatusDraft || got.Author != "alice" || got.ApprovedBy != "" {
		t.Errorf("lifecycle fields taken from the request: status=%s author=%s approved_by=%s", got.Status, got.Author, got.ApprovedBy)
	}
	if got.Title != "2027 plan (revised)" || got.Parameters.UtilisationThreshold != 0.9 || got.NonWorkingDates != "2027-12-24, 2027-12-27" {
		t.Errorf("header not saved: %+v", got)
	}
	edit.Parameters.ResultIneffectiveMin = 1
	_, err = svc.UpdateHeader(p.ID, edit, "alice")
	wantInvalid(t, err, "test result thresholds")
	edit.Parameters = Parameters{}
	edit.PeriodEnd = "2026-01-01"
	_, err = svc.UpdateHeader(p.ID, edit, "alice")
	wantInvalid(t, err, "before it starts")
}

// ---- clone ----

func TestCloneShiftsDatesAndDropsExecutionData(t *testing.T) {
	svc := newTestService(t)
	svc.SetRequireSeparateApprover(true)
	p := approveExample(t, svc)
	step := stepByRef(t, p, "1.1")
	if _, err := svc.UpdateStepStatus(p.ID, step.ID, StepComplete, "carol"); err != nil {
		t.Fatal(err)
	}

	c, err := svc.Clone(p.ID, "", "dave")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if c.Reference != "AP-2028-001" || c.Status != StatusDraft || c.Author != "dave" || c.VersionCount != 0 ||
		c.ApprovedBy != "" || c.Revision != 1 || c.SelectionSeed != "" {
		t.Errorf("clone header: ref=%s status=%s author=%s versions=%d approved_by=%q", c.Reference, c.Status, c.Author, c.VersionCount, c.ApprovedBy)
	}
	if c.Title != "2028 Security NFR Controls Testing Plan — Tier 1 & Tier 2 ICT assets" {
		t.Errorf("title = %q", c.Title)
	}
	if c.RegulatoryRefs != p.RegulatoryRefs || !strings.Contains(c.RegulatoryRefs, "2024/1774") {
		t.Errorf("regulatory references changed: %q", c.RegulatoryRefs)
	}
	if !strings.Contains(c.Objective, "31 December 2028") {
		t.Errorf("objective = %q", c.Objective)
	}
	if c.PeriodStart != "2028-01-01" || c.PeriodEnd != "2028-12-31" || c.MobiliseStart != "2027-11-09" || c.ClosingDate != "2029-01-29" {
		t.Errorf("period %s..%s mobilise %s closing %s", c.PeriodStart, c.PeriodEnd, c.MobiliseStart, c.ClosingDate)
	}
	for ref, want := range map[string][2]string{
		"0.1":  {"2027-11-09", "2027-11-15"},
		"1.3":  {"2028-03-01", "2028-03-01"},
		"4.11": {"2028-12-29", "2028-12-29"},
		"O.1":  {"2028-01-04", "2028-12-29"},
	} {
		s := stepByRef(t, c, ref)
		if s.StartDate != want[0] || s.EndDate != want[1] {
			t.Errorf("clone step %s %s..%s, want %s..%s", ref, s.StartDate, s.EndDate, want[0], want[1])
		}
	}
	if s := stepByRef(t, c, "4.9"); s.Title != "Draft the 2029 plan" {
		t.Errorf("4.9 = %q", s.Title)
	}
	for _, s := range c.Steps {
		if s.Status != StepNotStarted {
			t.Fatalf("step %s kept status %s", s.StepRef, s.Status)
		}
	}
	if c.Quarters[0].SelectionDate != "2028-03-01" || c.Quarters[0].CommitteeLabel != "May 2028 (date TBC)" || c.Quarters[3].OverheadDays != 15 {
		t.Errorf("Q1 = %+v, Q4 overhead %g", c.Quarters[0], c.Quarters[3].OverheadDays)
	}
	if len(c.RACI) != len(p.RACI) || len(c.Cadence) != len(p.Cadence) {
		t.Error("governance did not come across")
	}
	h, _ := svc.History(c.ID)
	if len(h) != 1 || !strings.Contains(h[0].Note, "cloned from AP-2027-001") {
		t.Errorf("clone history = %+v", h)
	}
}

func TestYearShifterLeavesCitationsAlone(t *testing.T) {
	shift := yearShifter(2026, 2028)
	for in, want := range map[string]string{
		"Draft the 2028 plan":                   "Draft the 2029 plan",
		"RTS (EU) 2024/1774 Art. 27":            "RTS (EU) 2024/1774 Art. 27",
		"Regulation (EU) 2022/2554, 2027 cycle": "Regulation (EU) 2022/2554, 2028 cycle",
		"Q4 2027–2028":                          "Q4 2028–2029",
		"12027 and 1999":                        "12027 and 1999",
	} {
		if got := shift(in); got != want {
			t.Errorf("shift(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeleteOnlyANeverApprovedDraft(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	if err := svc.Delete(p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v", err)
	}
}
