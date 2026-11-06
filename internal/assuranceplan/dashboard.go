package assuranceplan

import (
	"errors"
	"slices"

	"grc/internal/auditfinding"
)

// CoverageCell is one stratum's 1LoD coverage at a quarter end: the
// cumulative wave target against the items attested by that date.
type CoverageCell struct {
	Quarter    int     `json:"quarter"`
	QuarterEnd string  `json:"quarter_end"`
	Planned    int     `json:"planned"`
	PlannedPct float64 `json:"planned_pct"`
	Attested   int     `json:"attested"`
	ActualPct  float64 `json:"actual_pct"`
	Due        bool    `json:"due"`
	RAG        string  `json:"rag"`
}

type CoverageRow struct {
	Code        string          `json:"stratum"`
	Name        string          `json:"name"`
	N           int             `json:"n"`
	Attested    int             `json:"attested"`
	DQAccepted  int             `json:"dq_accepted"`
	AttestedPct float64         `json:"attested_pct"`
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

// computeCoverage compares each stratum's attestations with its cumulative
// wave target at each quarter end. A quarter that has not ended is Not due.
func computeCoverage(p Plan, pop []PopulationItem, view SamplingView, today string) []CoverageRow {
	out := []CoverageRow{}
	for i, st := range p.Strata {
		z := view.Strata[i]
		row := CoverageRow{Code: st.Code, Name: st.Name}
		var dates []string
		for _, it := range pop {
			if it.StratumCode != st.Code {
				continue
			}
			row.N++
			if it.AttestedOn != "" {
				row.Attested++
				dates = append(dates, it.AttestedOn)
			}
			if it.DQStatus == DQAccepted {
				row.DQAccepted++
			}
		}
		if row.N > 0 {
			row.AttestedPct = guard(float64(row.Attested) * 100 / float64(row.N))
			row.DQPct = guard(float64(row.DQAccepted) * 100 / float64(row.N))
		}
		planned := 0
		for q := 1; q <= 4; q++ {
			end, _ := QuarterEnd(p.PeriodStart, q)
			planned += z.ReviewsDue[q-1]
			c := CoverageCell{Quarter: q, QuarterEnd: end, Planned: planned, PlannedPct: guard(z.CumTarget[q-1] * 100),
				Due: end != "" && today > end}
			for _, d := range dates {
				if end == "" || d <= end {
					c.Attested++
				}
			}
			if row.N > 0 {
				c.ActualPct = guard(float64(c.Attested) * 100 / float64(row.N))
			}
			c.RAG = RAG(c.PlannedPct, c.ActualPct, p.Parameters.RAGAmberPoints, c.Due && row.N > 0)
			row.Cells[q-1] = c
		}
		out = append(out, row)
	}
	return out
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
	d := DashboardView{Today: today, Coverage: computeCoverage(p, pop, view, today), Testing: []TestingRow{},
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
