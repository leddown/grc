package assuranceplan

import (
	"errors"
	"slices"

	"grc/internal/auditfinding"
)

// CoverageCell is one stratum's coverage at a quarter end: the cumulative
// wave target against the items covered by that date, on the plan's coverage
// basis (attested by 1LoD, or tested and signed off by QA).
type CoverageCell struct {
	Quarter    int     `json:"quarter"`
	QuarterEnd string  `json:"quarter_end"`
	Planned    int     `json:"planned"`
	PlannedPct float64 `json:"planned_pct"`
	Covered    int     `json:"covered"`
	ActualPct  float64 `json:"actual_pct"`
	Due        bool    `json:"due"`
	RAG        string  `json:"rag"`
}

// CoverageRow is a stratum's coverage. Attested counts 1LoD attestations
// whatever the basis; Covered counts on the plan's basis, and includes the
// items of a multi-year stratum still inside their cycle (InCycle).
type CoverageRow struct {
	Code        string          `json:"stratum"`
	Name        string          `json:"name"`
	Basis       string          `json:"basis"`
	N           int             `json:"n"`
	Attested    int             `json:"attested"`
	AttestedPct float64         `json:"attested_pct"`
	Tested      int             `json:"tested"`
	Covered     int             `json:"covered"`
	CoveredPct  float64         `json:"covered_pct"`
	InCycle     int             `json:"in_cycle"`
	CycleYears  int             `json:"cycle_years"`
	DQAccepted  int             `json:"dq_accepted"`
	DQPct       float64         `json:"dq_accepted_pct"`
	Cells       [4]CoverageCell `json:"quarters"`
}

// TestingRow is a quarter's 2LoD testing: planned, drawn, opened, finished,
// signed off, and the results so far.
type TestingRow struct {
	Quarter      int            `json:"quarter"`
	Planned      int            `json:"planned"`
	Selected     int            `json:"selected"`
	Opened       int            `json:"opened"`
	Fieldwork    int            `json:"fieldwork_complete"`
	QA           int            `json:"qa_signed_off"`
	EvidenceLate int            `json:"evidence_late"`
	ByResult     map[string]int `json:"by_result"`
}

// LinkedFinding is a finding raised from the plan, read back from Audit
// Findings so its status, due date and overdue flag are that module's.
type LinkedFinding struct {
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
	FindingID int64  `json:"finding_id"`
}

type SeverityCount struct {
	Severity string `json:"severity"`
	Raised   int    `json:"raised"`
	Draft    int    `json:"draft"`
	Open     int    `json:"open"`
	Overdue  int    `json:"overdue"`
	Closed   int    `json:"closed"`
}

type DashboardView struct {
	Today        string          `json:"today"`
	Coverage     []CoverageRow   `json:"coverage"`
	Testing      []TestingRow    `json:"testing"`
	Deviations   []Deviation     `json:"deviations"`
	Findings     []LinkedFinding `json:"findings"`
	BySeverity   []SeverityCount `json:"findings_by_severity"`
	Missing      int             `json:"findings_missing"`
	Quarters     [4]QuarterLoad  `json:"utilisation"`
	Threshold    float64         `json:"utilisation_threshold"`
	TestsPlanned int             `json:"tests_planned"`
	TestsDone    int             `json:"tests_done"`
}

// computeCoverage compares each stratum's coverage with its cumulative wave
// target at each quarter end. A quarter that has not ended is Not due. An
// item of a multi-year stratum counts as covered while its last test before
// the plan is inside the cycle, and is planned to be covered from then on if
// its wave comes before the cycle lapses.
func computeCoverage(p Plan, pop []PopulationItem, view SamplingView, tests []Test, today string) []CoverageRow {
	signedOff := map[int64]string{}
	for _, t := range tests {
		if t.QADate != "" && (signedOff[t.ItemID] == "" || t.QADate < signedOff[t.ItemID]) {
			signedOff[t.ItemID] = t.QADate
		}
	}
	basis := firstNonEmpty(p.CoverageBasis, CoverageAttestation)
	out := []CoverageRow{}
	for i, st := range p.Strata {
		z := view.Strata[i]
		row := CoverageRow{Code: st.Code, Name: st.Name, Basis: basis, CycleYears: st.CycleYears}
		var items []PopulationItem
		for _, it := range pop {
			if it.StratumCode == st.Code {
				items = append(items, it)
			}
		}
		coveredOn := func(it PopulationItem) string {
			if basis == CoverageTests {
				return signedOff[it.ID]
			}
			return it.AttestedOn
		}
		coveredAt := func(it PopulationItem, date string) bool {
			if inCycleAt(st, it, date) {
				return true
			}
			d := coveredOn(it)
			return d != "" && (date == "" || d <= date)
		}
		for _, it := range items {
			row.N++
			if it.AttestedOn != "" {
				row.Attested++
			}
			if signedOff[it.ID] != "" {
				row.Tested++
			}
			if coveredAt(it, today) {
				row.Covered++
			}
			if inCycleAt(st, it, today) {
				row.InCycle++
			}
			if it.DQStatus == DQAccepted {
				row.DQAccepted++
			}
		}
		if row.N > 0 {
			row.AttestedPct = guard(float64(row.Attested) * 100 / float64(row.N))
			row.CoveredPct = guard(float64(row.Covered) * 100 / float64(row.N))
			row.DQPct = guard(float64(row.DQAccepted) * 100 / float64(row.N))
		}
		planned := 0
		for q := 1; q <= 4; q++ {
			end, _ := QuarterEnd(p.PeriodStart, q)
			planned += z.ReviewsDue[q-1]
			c := CoverageCell{Quarter: q, QuarterEnd: end, Planned: planned, PlannedPct: guard(z.CumTarget[q-1] * 100),
				Due: end != "" && today > end}
			if st.CycleYears > 1 {
				c.Planned = 0
				for _, it := range items {
					if inCycleAt(st, it, end) || (it.WaveQuarter >= 1 && it.WaveQuarter <= q) {
						c.Planned++
					}
				}
				c.PlannedPct = 0
				if row.N > 0 {
					c.PlannedPct = guard(float64(c.Planned) * 100 / float64(row.N))
				}
			}
			for _, it := range items {
				if coveredAt(it, end) {
					c.Covered++
				}
			}
			if row.N > 0 {
				c.ActualPct = guard(float64(c.Covered) * 100 / float64(row.N))
			}
			c.RAG = RAG(c.PlannedPct, c.ActualPct, p.Parameters.RAGAmberPoints, c.Due && row.N > 0)
			row.Cells[q-1] = c
		}
		out = append(out, row)
	}
	return out
}

// coverageWord is how the pages and reports say an item is covered.
func coverageWord(basis string) string {
	if basis == CoverageTests {
		return "tested"
	}
	return "attested"
}

// coverageMethod says how coverage is measured, for a page or a report.
func coverageMethod(basis string, cycle bool) string {
	out := "1LoD owners attest their items in quarterly waves, and 2LoD reviews the data quality of every submission. An item counts as covered on its attestation date."
	if basis == CoverageTests {
		out = "Every item due in the period is tested in its quarter's wave. An item counts as covered on the date its test is signed off by QA."
	}
	if cycle {
		out += " An item of a multi-year stratum (TLPT every three years) also counts while its last test before the plan is inside the cycle, and falls due when the cycle lapses."
	}
	return out
}

func hasCycle(strata []Stratum) bool {
	return slices.ContainsFunc(strata, func(st Stratum) bool { return st.CycleYears > 1 })
}

func (s *Service) Dashboard(id int64) (DashboardView, error) {
	p, err := s.Get(id)
	if err != nil {
		return DashboardView{}, err
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return DashboardView{}, err
	}
	view, err := s.Sampling(id)
	if err != nil {
		return DashboardView{}, err
	}
	tests, err := s.testsOf(p, 0)
	if err != nil {
		return DashboardView{}, err
	}
	pushed, err := s.repo.Thematics(id)
	if err != nil {
		return DashboardView{}, err
	}
	today := s.today()
	d := DashboardView{Today: today, Coverage: computeCoverage(p, pop, view, tests, today), Testing: []TestingRow{},
		Deviations: computeDeviations(p, tests, view, pushed), Findings: []LinkedFinding{}, BySeverity: []SeverityCount{},
		Quarters: view.Quarters, Threshold: view.Threshold, TestsPlanned: view.TotalTests}

	for q := 1; q <= 4; q++ {
		row := TestingRow{Quarter: q, ByResult: map[string]int{}}
		for _, z := range view.Strata {
			row.Planned += z.Tests[q-1]
		}
		row.Selected = view.Selection[q-1].Picks
		for _, t := range tests {
			if t.Quarter != q {
				continue
			}
			row.Opened++
			if t.FieldworkCompletedOn != "" {
				row.Fieldwork++
			}
			if t.QADate != "" {
				row.QA++
				d.TestsDone++
			}
			if t.EvidenceLate {
				row.EvidenceLate++
			}
			row.ByResult[t.Overall]++
		}
		d.Testing = append(d.Testing, row)
	}

	counts := map[string]*SeverityCount{}
	for _, sev := range auditfinding.Severities {
		counts[sev] = &SeverityCount{Severity: sev}
	}
	for _, t := range tests {
		for _, r := range t.Results {
			if r.FindingID == 0 {
				continue
			}
			lf := LinkedFinding{Reference: r.FindingRef, Severity: r.Severity, TestRef: t.TestRef, AttrRef: r.AttrRef,
				ItemRef: t.ItemRef, FindingID: r.FindingID}
			if s.in.Findings != nil {
				f, err := s.in.Findings.Get(r.FindingID)
				switch {
				case errors.Is(err, auditfinding.ErrNotFound):
					lf.Missing = true
				case err != nil:
					return DashboardView{}, err
				default:
					lf.Reference, lf.Title, lf.Severity, lf.Status = f.Reference, f.Title, f.Severity, f.Status
					lf.DueDate, lf.Overdue = f.DueDate, f.Overdue
				}
			}
			d.Findings = append(d.Findings, lf)
			if lf.Missing {
				d.Missing++
				continue
			}
			c := counts[lf.Severity]
			if c == nil {
				continue
			}
			c.Raised++
			switch lf.Status {
			case auditfinding.StatusDraft:
				c.Draft++
			case auditfinding.StatusClosed, auditfinding.StatusRiskAccepted:
				c.Closed++
			default:
				c.Open++
				if lf.Overdue {
					c.Overdue++
				}
			}
		}
	}
	for _, sev := range auditfinding.Severities {
		d.BySeverity = append(d.BySeverity, *counts[sev])
	}
	slices.SortStableFunc(d.Findings, func(a, b LinkedFinding) int {
		return slices.Index(auditfinding.Severities, a.Severity) - slices.Index(auditfinding.Severities, b.Severity)
	})
	return d, nil
}
