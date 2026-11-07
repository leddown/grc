package assuranceplan

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ExportFormatVersion is stamped into every export. Bump it when a field is
// renamed or removed, so a consumer such as the pack template can tell.
const ExportFormatVersion = 1

// PlanExport is the plan as a deliverable's data file: the design, what was
// computed from it and what happened during the year. It carries no database
// ids; rows refer to each other by step_ref, item_ref, attr_ref and test_ref.
// A dedicated type rather than Plan serialised, so renaming an internal JSON
// tag cannot silently blank a field in the committee pack.
type PlanExport struct {
	FormatVersion      int      `json:"format_version"`
	GeneratedAt        string   `json:"generated_at"`
	Reference          string   `json:"reference"`
	Title              string   `json:"title"`
	PlanType           string   `json:"plan_type"`
	PlanTypeName       string   `json:"plan_type_name"`
	FunctionName       string   `json:"function_name"`
	ClientName         string   `json:"client_name"`
	PeriodStart        string   `json:"period_start"`
	PeriodEnd          string   `json:"period_end"`
	MobiliseStart      string   `json:"mobilise_start"`
	ClosingDate        string   `json:"closing_date"`
	Objective          string   `json:"objective"`
	DecisionsRequested []string `json:"decisions_requested"`
	RegulatoryRefs     string   `json:"regulatory_refs"`
	OwnerRole          string   `json:"owner_role"`
	AccountableRole    string   `json:"accountable_role"`
	ApprovalForum      string   `json:"approval_forum"`
	Status             string   `json:"status"`
	Revision           int      `json:"revision"`
	RevisionStatus     string   `json:"revision_status"`
	ApprovedBy         string   `json:"approved_by"`
	ApprovedAt         string   `json:"approved_at"`
	ActivatedAt        string   `json:"activated_at"`
	ClosedAt           string   `json:"closed_at"`
	LatestVersion      string   `json:"latest_version"`
	PopulationFrozenAt string   `json:"population_frozen_at"`
	SeedCommitment     string   `json:"seed_commitment"`
	// Seed is present only once every quarter is drawn, as on the page.
	Seed             string     `json:"seed"`
	NonWorkingDates  []string   `json:"non_working_dates"`
	Parameters       Parameters `json:"parameters"`
	TestTypes        []string   `json:"test_types"`
	CoverageBasis    string     `json:"coverage_basis"`
	PopulationSource string     `json:"population_source"`

	RegulatoryBasis []ExportBasis     `json:"regulatory_basis"`
	Steps           []ExportStep      `json:"steps"`
	Quarters        []ExportQuarter   `json:"quarters"`
	Strata          []ExportStratum   `json:"strata"`
	Capacity        ExportCapacity    `json:"capacity"`
	Attributes      []ExportAttribute `json:"attributes"`
	Population      []ExportItem      `json:"population"`
	Tests           []ExportTest      `json:"tests"`
	Deviations      []ExportDeviation `json:"deviations"`
	Coverage        []CoverageRow     `json:"coverage"`
	Testing         []TestingRow      `json:"testing"`
	Findings        []ExportFinding   `json:"findings"`
	FindingsBy      []SeverityCount   `json:"findings_by_severity"`
	FindingsMissing int               `json:"findings_missing"`
	Governance      ExportGovernance  `json:"governance"`
	Versions        []ExportVersion   `json:"versions"`
	GateFindings    []LintFinding     `json:"gate_findings"`
	Totals          ExportTotals      `json:"totals"`
}

type ExportBasis struct {
	Requirement string `json:"requirement"`
	Reference   string `json:"reference"`
	How         string `json:"how"`
}

type ExportStep struct {
	StepRef         string `json:"step_ref"`
	Phase           string `json:"phase"`
	Title           string `json:"title"`
	StepType        string `json:"step_type"`
	ResponsibleRole string `json:"responsible_role"`
	AccountableRole string `json:"accountable_role"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	Deliverable     string `json:"deliverable"`
	Refs            string `json:"refs"`
	Status          string `json:"status"`
	Quarter         int    `json:"quarter"`
}

type ExportQuarter struct {
	Quarter          int     `json:"quarter"`
	QuarterEnd       string  `json:"quarter_end"`
	LOD1WindowStart  string  `json:"lod1_window_start"`
	LOD1Deadline     string  `json:"lod1_deadline"`
	SelectionDate    string  `json:"selection_date"`
	FieldworkStart   string  `json:"fieldwork_start"`
	FieldworkEnd     string  `json:"fieldwork_end"`
	FindingsAgreedBy string  `json:"findings_agreed_by"`
	ReportDate       string  `json:"report_date"`
	CommitteeLabel   string  `json:"committee_label"`
	RetestDays       float64 `json:"retest_days"`
	OverheadDays     float64 `json:"overhead_days"`
	SelectedAt       string  `json:"selected_at"`
	Picks            int     `json:"picks"`
	PlannedTests     int     `json:"planned_tests"`
	ReviewsDue       int     `json:"reviews_due"`
}

type ExportStratum struct {
	Code             string     `json:"code"`
	Name             string     `json:"name"`
	Definition       string     `json:"definition"`
	SamplingEnabled  bool       `json:"sampling_enabled"`
	Confidence       float64    `json:"confidence"`
	TolerableRate    float64    `json:"tolerable_rate"`
	FloorPct         float64    `json:"floor_pct"`
	EffortPerTest    float64    `json:"effort_per_test"`
	EffortPerReview  float64    `json:"effort_per_review"`
	Waves            [4]float64 `json:"waves"`
	TopUps           [4]int     `json:"top_ups"`
	PlannedSize      int        `json:"planned_size"`
	CycleYears       int        `json:"cycle_years"`
	Takes            []string   `json:"takes"`
	N                int        `json:"n"`
	InCycle          int        `json:"in_cycle"`
	NSource          string     `json:"n_source"`
	N0               int        `json:"n0"`
	FPC              int        `json:"fpc"`
	Floor            int        `json:"floor"`
	AnnualSample     int        `json:"annual_sample"`
	Statistical      [4]int     `json:"statistical"`
	Tests            [4]int     `json:"tests"`
	TotalTests       int        `json:"total_tests"`
	CoveragePct      float64    `json:"coverage_pct"`
	ReviewsDue       [4]int     `json:"reviews_due"`
	CumulativeTarget [4]float64 `json:"cumulative_target_pct"`
	Error            string     `json:"error"`
}

type ExportCapacity struct {
	Quarters      [4]QuarterLoad `json:"quarters"`
	TotalEffort   float64        `json:"total_effort"`
	TotalCapacity float64        `json:"total_capacity"`
	Utilisation   float64        `json:"utilisation"`
	Threshold     float64        `json:"utilisation_threshold"`
}

type ExportAttribute struct {
	AttrRef           string   `json:"attr_ref"`
	Domain            string   `json:"domain"`
	Requirement       string   `json:"requirement"`
	Procedure         string   `json:"procedure"`
	Method            string   `json:"method"`
	EvidenceRequested string   `json:"evidence_requested"`
	PassCriteria      string   `json:"pass_criteria"`
	AppliesTo         []string `json:"applies_to"`
	RegulatoryRefs    string   `json:"regulatory_refs"`
	ControlIDs        []string `json:"control_ids"`
	NFRKey            string   `json:"nfr_key"`
	Unresolved        []string `json:"unresolved_refs"`
}

type ExportItem struct {
	ItemRef         string `json:"item_ref"`
	Name            string `json:"name"`
	Stratum         string `json:"stratum"`
	CIF             string `json:"cif"`
	BusinessLine    string `json:"business_line"`
	Hosting         string `json:"hosting"`
	InternetFacing  bool   `json:"internet_facing"`
	Owner           string `json:"owner"`
	Wave            int    `json:"wave"`
	AttestedOn      string `json:"attested_on"`
	DQStatus        string `json:"dq_status"`
	DQReviewedOn    string `json:"dq_reviewed_on"`
	TopUpQuarter    int    `json:"topup_quarter"`
	TopUpRationale  string `json:"topup_rationale"`
	LastTestedOn    string `json:"last_tested_on"`
	CycleDue        string `json:"cycle_due"`
	SourceKind      string `json:"source_kind"`
	SourceRef       string `json:"source_ref"`
	SelectedQuarter int    `json:"selected_quarter"`
	SelectionBasis  string `json:"selection_basis"`
	RankOrder       int    `json:"rank_order"`
	TestRef         string `json:"test_ref"`
	OverallResult   string `json:"overall_result"`
}

type ExportResult struct {
	AttrRef    string `json:"attr_ref"`
	Result     string `json:"result"`
	Note       string `json:"note"`
	FindingRef string `json:"finding_ref"`
	Severity   string `json:"severity"`
}

type ExportTest struct {
	TestRef              string         `json:"test_ref"`
	TestType             string         `json:"test_type"`
	Quarter              int            `json:"quarter"`
	ItemRef              string         `json:"item_ref"`
	ItemName             string         `json:"item_name"`
	Stratum              string         `json:"stratum"`
	Basis                string         `json:"basis"`
	Tester               string         `json:"tester"`
	EvidenceRequestedOn  string         `json:"evidence_requested_on"`
	EvidenceDueOn        string         `json:"evidence_due_on"`
	EvidenceReceivedOn   string         `json:"evidence_received_on"`
	FieldworkCompletedOn string         `json:"fieldwork_completed_on"`
	EvidenceSummary      string         `json:"evidence_summary"`
	QAReviewer           string         `json:"qa_reviewer"`
	QADate               string         `json:"qa_date"`
	QANote               string         `json:"qa_note"`
	Notes                string         `json:"notes"`
	Passes               int            `json:"passes"`
	Fails                int            `json:"fails"`
	NotApplicable        int            `json:"not_applicable"`
	Pending              int            `json:"pending"`
	Overall              string         `json:"overall"`
	EvidenceLate         bool           `json:"evidence_late"`
	Results              []ExportResult `json:"results"`
}

type ExportDeviation struct {
	AttrRef       string   `json:"attr_ref"`
	Domain        string   `json:"domain"`
	Stratum       string   `json:"stratum"`
	Tested        int      `json:"tested"`
	Deviations    int      `json:"deviations"`
	Kind          string   `json:"kind"`
	Conclusion    string   `json:"conclusion"`
	Failures      []string `json:"failures"`
	ExtendQuarter int      `json:"extend_quarter"`
	ExtendBy      int      `json:"extend_by"`
	FullCoverage  bool     `json:"full_coverage"`
	RiskRef       string   `json:"risk_ref"`
}

type ExportFinding struct {
	Reference string `json:"reference"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	Status    string `json:"status"`
	DueDate   string `json:"due_date"`
	Overdue   bool   `json:"overdue"`
	Missing   bool   `json:"missing"`
	TestRef   string `json:"test_ref"`
	AttrRef   string `json:"attr_ref"`
	ItemRef   string `json:"item_ref"`
}

type ExportRACI struct {
	Activity string            `json:"activity"`
	Cells    map[string]string `json:"cells"`
}

type ExportCadence struct {
	Forum     string `json:"forum"`
	Frequency string `json:"frequency"`
	Content   string `json:"content"`
	Owner     string `json:"owner"`
}

type ExportEscalation struct {
	Trigger    string `json:"trigger"`
	EscalateTo string `json:"escalate_to"`
	Within     string `json:"within"`
}

type ExportGovernance struct {
	RACIRoles   []string           `json:"raci_roles"`
	RACI        []ExportRACI       `json:"raci"`
	Cadence     []ExportCadence    `json:"cadence"`
	Escalations []ExportEscalation `json:"escalations"`
}

type ExportVersion struct {
	Number         int    `json:"number"`
	Label          string `json:"label"`
	ApprovedBy     string `json:"approved_by"`
	ApprovedAt     string `json:"approved_at"`
	ChangeReason   string `json:"change_reason"`
	SnapshotSHA256 string `json:"snapshot_sha256"`
}

// ExportTotals are the headline figures, so a template need not add them up.
type ExportTotals struct {
	PopulationSize  int `json:"population_size"`
	PlannedTests    int `json:"planned_tests"`
	ItemsSelected   int `json:"items_selected"`
	TestsOpened     int `json:"tests_opened"`
	TestsSignedOff  int `json:"tests_signed_off"`
	AttributeFails  int `json:"attribute_fails"`
	FindingsRaised  int `json:"findings_raised"`
	FindingsOpen    int `json:"findings_open"`
	FindingsOverdue int `json:"findings_overdue"`
	NotEffective    int `json:"not_effective"`
	Extensions      int `json:"extensions"`
}

// Export builds the plan's export. It reads what a reader of the plan page
// may read, so the seed stays out until it is revealed.
func (s *Service) Export(id int64) (PlanExport, error) {
	p, err := s.Get(id)
	if err != nil {
		return PlanExport{}, err
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return PlanExport{}, err
	}
	sels, err := s.repo.Selections(id)
	if err != nil {
		return PlanExport{}, err
	}
	tests, err := s.testsOf(p, 0)
	if err != nil {
		return PlanExport{}, err
	}
	dash, err := s.Dashboard(id)
	if err != nil {
		return PlanExport{}, err
	}
	versions, err := s.repo.Versions(id)
	if err != nil {
		return PlanExport{}, err
	}
	view := computeSampling(p, pop, sels)
	return buildExport(p, pop, sels, tests, view, dash, versions, s.stamp()), nil
}

func buildExport(p Plan, pop []PopulationItem, sels []Selection, tests []Test, view SamplingView, dash DashboardView,
	versions []Version, generated string) PlanExport {
	e := PlanExport{
		FormatVersion: ExportFormatVersion, GeneratedAt: generated,
		Reference: p.Reference, Title: p.Title, PlanType: p.PlanType, PlanTypeName: p.PlanTypeName,
		FunctionName: p.FunctionName, ClientName: p.ClientName,
		PeriodStart: p.PeriodStart, PeriodEnd: p.PeriodEnd, MobiliseStart: p.MobiliseStart, ClosingDate: p.ClosingDate,
		Objective: p.Objective, DecisionsRequested: lines(p.DecisionsRequested), RegulatoryRefs: p.RegulatoryRefs,
		OwnerRole: p.OwnerRole, AccountableRole: p.AccountableRole, ApprovalForum: p.ApprovalForum,
		Status: p.Status, Revision: p.Revision, RevisionStatus: p.RevisionStatus,
		ApprovedBy: p.ApprovedBy, ApprovedAt: p.ApprovedAt, ActivatedAt: p.ActivatedAt, ClosedAt: p.ClosedAt,
		LatestVersion: p.LatestVersion, PopulationFrozenAt: p.FrozenAt, SeedCommitment: p.SeedCommitment,
		NonWorkingDates: splitList(p.NonWorkingDates), Parameters: p.Parameters,
		TestTypes: nonNil(p.TestTypes), CoverageBasis: p.CoverageBasis, PopulationSource: p.PopulationSource,
		RegulatoryBasis: []ExportBasis{}, Steps: []ExportStep{}, Quarters: []ExportQuarter{}, Strata: []ExportStratum{},
		Attributes: []ExportAttribute{}, Population: []ExportItem{}, Tests: []ExportTest{}, Deviations: []ExportDeviation{},
		Coverage: nonNil(dash.Coverage), Testing: nonNil(dash.Testing), Findings: []ExportFinding{},
		FindingsBy: nonNil(dash.BySeverity), FindingsMissing: dash.Missing, Versions: []ExportVersion{},
		GateFindings: nonNil(Lint(p, pop)),
		Capacity: ExportCapacity{Quarters: view.Quarters, TotalEffort: view.TotalEffort, TotalCapacity: view.TotalCapacity,
			Utilisation: view.Utilisation, Threshold: view.Threshold},
	}
	if p.SeedRevealed {
		e.Seed = p.SelectionSeed
	}
	for _, b := range p.Basis {
		e.RegulatoryBasis = append(e.RegulatoryBasis, ExportBasis{Requirement: b.Requirement, Reference: b.Reference, How: b.How})
	}
	for _, st := range p.Steps {
		e.Steps = append(e.Steps, ExportStep{StepRef: st.StepRef, Phase: st.Phase, Title: st.Title, StepType: st.StepType,
			ResponsibleRole: st.ResponsibleRole, AccountableRole: st.AccountableRole, StartDate: st.StartDate, EndDate: st.EndDate,
			Deliverable: st.Deliverable, Refs: st.Refs, Status: st.Status, Quarter: st.Quarter})
	}
	for _, q := range p.Quarters {
		end, _ := QuarterEnd(p.PeriodStart, q.Quarter)
		eq := ExportQuarter{Quarter: q.Quarter, QuarterEnd: end, LOD1WindowStart: q.LOD1WindowStart, LOD1Deadline: q.LOD1Deadline,
			SelectionDate: q.SelectionDate, FieldworkStart: q.FieldworkStart, FieldworkEnd: q.FieldworkEnd,
			FindingsAgreedBy: q.FindingsAgreedBy, ReportDate: q.ReportDate, CommitteeLabel: q.CommitteeLabel,
			RetestDays: q.RetestDays, OverheadDays: q.OverheadDays, SelectedAt: q.SelectedAt}
		if q.Quarter >= 1 && q.Quarter <= 4 {
			for _, z := range view.Strata {
				eq.PlannedTests += z.Tests[q.Quarter-1]
				eq.ReviewsDue += z.ReviewsDue[q.Quarter-1]
			}
			if len(view.Selection) >= q.Quarter {
				eq.Picks = view.Selection[q.Quarter-1].Picks
			}
		}
		e.Quarters = append(e.Quarters, eq)
	}
	for i, st := range p.Strata {
		es := ExportStratum{Code: st.Code, Name: st.Name, Definition: st.Definition, SamplingEnabled: st.SamplingEnabled,
			Confidence: st.Confidence, TolerableRate: st.TolerableRate, FloorPct: st.FloorPct,
			EffortPerTest: st.EffortPerTest, EffortPerReview: st.EffortPerReview, Waves: st.Waves, TopUps: st.TopUps,
			PlannedSize: st.PlannedSize, CycleYears: st.CycleYears, Takes: nonNil(st.Takes)}
		if i < len(view.Strata) {
			z := view.Strata[i]
			es.InCycle = z.InCycle
			es.N, es.NSource, es.N0, es.FPC, es.Floor, es.AnnualSample = z.N, z.NSource, z.Size.N0, z.Size.FPC, z.Size.Floor, z.Size.Annual
			es.Statistical, es.Tests, es.TotalTests, es.ReviewsDue, es.Error = z.Statistical, z.Tests, z.TotalTests, z.ReviewsDue, z.Error
			es.CoveragePct = guard(z.Coverage * 100)
			for q := range 4 {
				es.CumulativeTarget[q] = guard(z.CumTarget[q] * 100)
			}
		}
		e.Strata = append(e.Strata, es)
		e.Totals.PlannedTests += es.TotalTests
	}
	for _, a := range p.Attributes {
		e.Attributes = append(e.Attributes, ExportAttribute{AttrRef: a.AttrRef, Domain: a.Domain, Requirement: a.Requirement,
			Procedure: a.Procedure, Method: a.Method, EvidenceRequested: a.EvidenceRequested, PassCriteria: a.PassCriteria,
			AppliesTo: nonNil(a.AppliesTo), RegulatoryRefs: a.RegulatoryRefs, ControlIDs: nonNil(a.ControlIDs), NFRKey: a.NFRKey,
			Unresolved: nonNil(a.Unresolved)})
	}

	rank := map[int64]Selection{}
	for _, sel := range sels {
		rank[sel.PopulationID] = sel
	}
	testOf := map[int64]Test{}
	for _, t := range tests {
		testOf[t.ItemID] = t
	}
	strata := map[string]Stratum{}
	for _, st := range p.Strata {
		strata[st.Code] = st
	}
	for _, it := range pop {
		ei := ExportItem{LastTestedOn: it.LastTestedOn, CycleDue: cycleDue(strata[it.StratumCode], it),
			SourceKind: it.SourceKind, SourceRef: it.SourceRef, ItemRef: it.ItemRef, Name: it.Name, Stratum: it.StratumCode, CIF: it.CIF, BusinessLine: it.BusinessLine,
			Hosting: it.Hosting, InternetFacing: it.InternetFacing, Owner: it.Owner, Wave: it.WaveQuarter, AttestedOn: it.AttestedOn,
			DQStatus: it.DQStatus, DQReviewedOn: it.DQReviewedOn, TopUpQuarter: it.TopUpQuarter, TopUpRationale: it.TopUpRationale,
			SelectedQuarter: it.SelectedQuarter, SelectionBasis: it.SelectionBasis}
		if sel, ok := rank[it.ID]; ok {
			ei.SelectedQuarter, ei.SelectionBasis, ei.RankOrder = sel.Quarter, sel.Basis, sel.RankOrder
			e.Totals.ItemsSelected++
		}
		if t, ok := testOf[it.ID]; ok {
			ei.TestRef, ei.OverallResult = t.TestRef, t.Overall
		}
		e.Population = append(e.Population, ei)
	}
	e.Totals.PopulationSize = len(pop)

	for _, t := range tests {
		et := ExportTest{TestRef: t.TestRef, TestType: t.TestType, Quarter: t.Quarter, ItemRef: t.ItemRef, ItemName: t.ItemName, Stratum: t.StratumCode,
			Basis: t.Basis, Tester: t.Tester, EvidenceRequestedOn: t.EvidenceRequestedOn, EvidenceDueOn: t.EvidenceDueOn,
			EvidenceReceivedOn: t.EvidenceReceivedOn, FieldworkCompletedOn: t.FieldworkCompletedOn, EvidenceSummary: t.EvidenceSummary,
			QAReviewer: t.QAReviewer, QADate: t.QADate, QANote: t.QANote, Notes: t.Notes, Passes: t.Passes, Fails: t.Fails,
			NotApplicable: t.NotApplicable, Pending: t.Pending, Overall: t.Overall, EvidenceLate: t.EvidenceLate, Results: []ExportResult{}}
		for _, r := range t.Results {
			et.Results = append(et.Results, ExportResult{AttrRef: r.AttrRef, Result: r.Result, Note: r.Note, FindingRef: r.FindingRef, Severity: r.Severity})
		}
		e.Tests = append(e.Tests, et)
		e.Totals.TestsOpened++
		e.Totals.AttributeFails += t.Fails
		if t.QADate != "" {
			e.Totals.TestsSignedOff++
		}
	}
	e.Deviations = exportDeviations(dash.Deviations)
	for _, d := range e.Deviations {
		switch d.Kind {
		case KindNotEffective:
			e.Totals.NotEffective++
		case KindExtend:
			e.Totals.Extensions++
		}
	}
	for _, f := range dash.Findings {
		e.Findings = append(e.Findings, ExportFinding{Reference: f.Reference, Title: f.Title, Severity: f.Severity, Status: f.Status,
			DueDate: f.DueDate, Overdue: f.Overdue, Missing: f.Missing, TestRef: f.TestRef, AttrRef: f.AttrRef, ItemRef: f.ItemRef})
	}
	for _, c := range dash.BySeverity {
		e.Totals.FindingsRaised += c.Raised
		e.Totals.FindingsOpen += c.Open
		e.Totals.FindingsOverdue += c.Overdue
	}

	g := stripGovIDs(p)
	e.Governance = ExportGovernance{RACIRoles: nonNil(g.RACIRoles), RACI: []ExportRACI{}, Cadence: []ExportCadence{}, Escalations: []ExportEscalation{}}
	for _, r := range g.RACI {
		cells := r.Cells
		if cells == nil {
			cells = map[string]string{}
		}
		e.Governance.RACI = append(e.Governance.RACI, ExportRACI{Activity: r.Activity, Cells: cells})
	}
	for _, c := range g.Cadence {
		e.Governance.Cadence = append(e.Governance.Cadence, ExportCadence{Forum: c.Forum, Frequency: c.Frequency, Content: c.Content, Owner: c.Owner})
	}
	for _, x := range g.Escalations {
		e.Governance.Escalations = append(e.Governance.Escalations, ExportEscalation{Trigger: x.Trigger, EscalateTo: x.EscalateTo, Within: x.Within})
	}
	for _, v := range versions {
		e.Versions = append(e.Versions, ExportVersion{Number: v.Number, Label: v.Label, ApprovedBy: v.ApprovedBy,
			ApprovedAt: v.ApprovedAt, ChangeReason: v.ChangeReason, SnapshotSHA256: v.SnapshotSHA256})
	}
	return e
}

// lines splits a multi-line field into its non-empty lines.
func lines(raw string) []string {
	out := []string{}
	for l := range strings.SplitSeq(raw, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func exportDeviations(devs []Deviation) []ExportDeviation {
	out := []ExportDeviation{}
	for _, d := range devs {
		ed := ExportDeviation{AttrRef: d.AttrRef, Domain: d.Domain, Stratum: d.StratumCode, Tested: d.Tested, Deviations: d.Fails,
			Kind: d.Kind, Conclusion: d.Conclusion, Failures: nonNil(d.Failures), ExtendQuarter: d.ExtendQuarter, ExtendBy: d.ExtendBy,
			FullCoverage: d.FullCoverage}
		if d.Thematic != nil {
			ed.RiskRef = d.Thematic.RiskRef
		}
		out = append(out, ed)
	}
	return out
}

// ExportTables are the CSV exports, in the order the page lists them.
var ExportTables = []string{"steps", "quarters", "population", "tests", "deviations"}

// ExportCSV writes one table of the export. Cells a spreadsheet would read as
// a formula are prefixed with an apostrophe: tester notes and item names are
// free text, and these files are opened in Excel.
func (s *Service) ExportCSV(id int64, table string) (string, []byte, error) {
	if !slices.Contains(ExportTables, table) {
		return "", nil, invalid("unknown export table %q: choose %s", table, strings.Join(ExportTables, ", "))
	}
	e, err := s.Export(id)
	if err != nil {
		return "", nil, err
	}
	var rows [][]string
	switch table {
	case "steps":
		rows = append(rows, []string{"step_ref", "phase", "title", "step_type", "responsible_role", "accountable_role",
			"start_date", "end_date", "status", "quarter", "deliverable", "refs"})
		for _, st := range e.Steps {
			rows = append(rows, []string{st.StepRef, st.Phase, st.Title, st.StepType, st.ResponsibleRole, st.AccountableRole,
				st.StartDate, st.EndDate, st.Status, quarterCell(st.Quarter), st.Deliverable, st.Refs})
		}
	case "quarters":
		head := []string{"quarter", "quarter_end", "lod1_window_start", "lod1_deadline", "selection_date", "fieldwork_start",
			"fieldwork_end", "findings_agreed_by", "report_date", "committee", "retest_days", "overhead_days",
			"planned_tests", "reviews_due", "effort_days", "capacity_days", "utilisation_pct", "selected_at", "picks"}
		for _, st := range e.Strata {
			head = append(head, st.Code+"_tests", st.Code+"_cumulative_target_pct")
		}
		rows = append(rows, head)
		for _, q := range e.Quarters {
			row := []string{quarterCell(q.Quarter), q.QuarterEnd, q.LOD1WindowStart, q.LOD1Deadline, q.SelectionDate, q.FieldworkStart,
				q.FieldworkEnd, q.FindingsAgreedBy, q.ReportDate, q.CommitteeLabel, num(q.RetestDays), num(q.OverheadDays),
				strconv.Itoa(q.PlannedTests), strconv.Itoa(q.ReviewsDue)}
			load := QuarterLoad{}
			if q.Quarter >= 1 && q.Quarter <= 4 {
				load = e.Capacity.Quarters[q.Quarter-1]
			}
			row = append(row, num(load.Effort), num(load.Capacity), num(guard(load.Utilisation*100)), q.SelectedAt, strconv.Itoa(q.Picks))
			for _, st := range e.Strata {
				if q.Quarter >= 1 && q.Quarter <= 4 {
					row = append(row, strconv.Itoa(st.Tests[q.Quarter-1]), num(st.CumulativeTarget[q.Quarter-1]))
				} else {
					row = append(row, "", "")
				}
			}
			rows = append(rows, row)
		}
	case "population":
		rows = append(rows, []string{"item_ref", "name", "stratum", "cif", "business_line", "hosting", "internet_facing", "owner",
			"wave", "attested_on", "dq_status", "dq_reviewed_on", "topup_quarter", "topup_rationale", "last_tested_on", "cycle_due",
			"source_kind", "source_ref", "selected_quarter", "selection_basis", "rank_order", "test_ref", "overall_result"})
		for _, it := range e.Population {
			rank := ""
			if it.RankOrder > 0 {
				rank = strconv.Itoa(it.RankOrder)
			}
			rows = append(rows, []string{it.ItemRef, it.Name, it.Stratum, it.CIF, it.BusinessLine, it.Hosting, yesNo(it.InternetFacing),
				it.Owner, quarterCell(it.Wave), it.AttestedOn, it.DQStatus, it.DQReviewedOn, quarterCell(it.TopUpQuarter), it.TopUpRationale,
				it.LastTestedOn, it.CycleDue, it.SourceKind, it.SourceRef, quarterCell(it.SelectedQuarter), it.SelectionBasis, rank, it.TestRef, it.OverallResult})
		}
	case "tests":
		rows = append(rows, []string{"test_ref", "test_type", "quarter", "item_ref", "item_name", "stratum", "basis", "tester",
			"evidence_requested_on", "evidence_due_on", "evidence_received_on", "evidence_late", "fieldwork_completed_on",
			"qa_reviewer", "qa_date", "pass", "fail", "n_a", "pending", "overall", "failed_attributes", "findings", "evidence_summary"})
		for _, t := range e.Tests {
			var failed, findings []string
			for _, r := range t.Results {
				if r.Result == ResultFail {
					failed = append(failed, r.AttrRef)
				}
				if r.FindingRef != "" {
					findings = append(findings, r.FindingRef+" ("+r.Severity+")")
				}
			}
			rows = append(rows, []string{t.TestRef, t.TestType, quarterCell(t.Quarter), t.ItemRef, t.ItemName, t.Stratum, t.Basis, t.Tester,
				t.EvidenceRequestedOn, t.EvidenceDueOn, t.EvidenceReceivedOn, yesNo(t.EvidenceLate), t.FieldworkCompletedOn,
				t.QAReviewer, t.QADate, strconv.Itoa(t.Passes), strconv.Itoa(t.Fails), strconv.Itoa(t.NotApplicable),
				strconv.Itoa(t.Pending), t.Overall, strings.Join(failed, "; "), strings.Join(findings, "; "), t.EvidenceSummary})
		}
	case "deviations":
		rows = append(rows, []string{"attr_ref", "domain", "stratum", "tested", "deviations", "conclusion", "failing_tests",
			"extend_quarter", "extend_by", "risk_ref"})
		for _, d := range e.Deviations {
			rows = append(rows, []string{d.AttrRef, d.Domain, d.Stratum, strconv.Itoa(d.Tested), strconv.Itoa(d.Deviations),
				d.Conclusion, strings.Join(d.Failures, "; "), quarterCell(d.ExtendQuarter), countCell(d.ExtendBy), d.RiskRef})
		}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for i, row := range rows {
		if i > 0 {
			for j := range row {
				row[j] = spreadsheetSafe(row[j])
			}
		}
		if err := w.Write(row); err != nil {
			return "", nil, fmt.Errorf("write %s CSV: %w", table, err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", nil, fmt.Errorf("write %s CSV: %w", table, err)
	}
	return e.Reference + "-" + table + ".csv", buf.Bytes(), nil
}

func quarterCell(q int) string {
	if q < 1 || q > 4 {
		return ""
	}
	return "Q" + strconv.Itoa(q)
}

func countCell(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func spreadsheetSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}
