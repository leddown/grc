package assuranceplan

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"grc/internal/auditfinding"
)

// The document templates these payloads are written for, registered in
// internal/doctemplate.
const (
	PackTemplateID   = "assurance-pack-typst"
	ReportTemplateID = "report-typst"
)

// PackPayload is the committee approval pack's data: the export without the
// population and test log, which a 13-slide deck does not print, plus what
// the slides show that is worked out rather than stored.
type PackPayload struct {
	PlanExport
	Pack PackExtras `json:"pack"`
}

type PackExtras struct {
	AsOf          string        `json:"as_of"`
	Roadmap       []RoadmapCard `json:"roadmap"`
	NextSteps     []ExportStep  `json:"next_steps"`
	DeliveryRisks []string      `json:"delivery_risks"`
	PeakQuarter   int           `json:"peak_quarter"`
}

// RoadmapCard is one card of the roadmap slide: mobilisation, then a quarter.
type RoadmapCard struct {
	Label  string   `json:"label"`
	Period string   `json:"period"`
	Lines  []string `json:"lines"`
}

// PackData builds the committee pack's payload.
func (s *Service) PackData(id int64) (PackPayload, error) {
	e, err := s.Export(id)
	if err != nil {
		return PackPayload{}, err
	}
	return packOf(e, s.today()), nil
}

func packOf(e PlanExport, today string) PackPayload {
	e.Population, e.Tests = []ExportItem{}, []ExportTest{}
	x := PackExtras{AsOf: today, Roadmap: []RoadmapCard{}, NextSteps: []ExportStep{}, DeliveryRisks: []string{}}

	mob := RoadmapCard{Label: "Mobilise", Period: displayRange(e.MobiliseStart, dayBefore(e.PeriodStart)), Lines: []string{}}
	for _, st := range e.Steps {
		if st.StepType == StepMilestone && st.Quarter == 0 && st.EndDate != "" && st.EndDate < e.PeriodStart {
			mob.Lines = append(mob.Lines, st.Title+" ("+displayDate(st.EndDate)+")")
		}
	}
	if len(mob.Lines) == 0 {
		mob.Lines = append(mob.Lines, "Confirm scope, population, sampling and resourcing")
	}
	x.Roadmap = append(x.Roadmap, mob)
	drawn := drawWord(e.Strata)
	for _, q := range e.Quarters {
		start := ""
		if t, ok := parseDate(e.PeriodStart); ok {
			start = formatDate(t.AddDate(0, 3*(q.Quarter-1), 0))
		}
		card := RoadmapCard{Label: "Q" + strconv.Itoa(q.Quarter), Period: displayRange(start, q.QuarterEnd), Lines: []string{}}
		if q.ReviewsDue > 0 {
			if e.CoverageBasis == CoverageTests {
				card.Lines = append(card.Lines, fmt.Sprintf("Wave: %d item(s) due, 1LoD deadline %s", q.ReviewsDue, displayDate(q.LOD1Deadline)))
			} else {
				card.Lines = append(card.Lines, fmt.Sprintf("1LoD wave: %d item(s) attested by %s", q.ReviewsDue, displayDate(q.LOD1Deadline)))
			}
		}
		if q.PlannedTests > 0 {
			card.Lines = append(card.Lines, fmt.Sprintf("2LoD: %d test(s), %s %s", q.PlannedTests, drawn, displayDate(q.SelectionDate)))
		}
		for _, st := range e.Steps {
			if st.StepType == StepMilestone && st.Quarter == 0 && st.EndDate >= start && st.EndDate <= q.QuarterEnd && start != "" {
				card.Lines = append(card.Lines, st.Title)
			}
		}
		if q.ReportDate != "" {
			card.Lines = append(card.Lines, "Report "+displayDate(q.ReportDate)+committeeSuffix(q.CommitteeLabel))
		}
		x.Roadmap = append(x.Roadmap, card)
	}

	open := []ExportStep{}
	for _, st := range e.Steps {
		if st.Status != StepComplete && (st.EndDate == "" || st.EndDate >= today) {
			open = append(open, st)
		}
	}
	slices.SortStableFunc(open, func(a, b ExportStep) int {
		return strings.Compare(firstNonEmpty(a.StartDate, a.EndDate), firstNonEmpty(b.StartDate, b.EndDate))
	})
	x.NextSteps = open[:min(6, len(open))]

	peak := 0
	for i, q := range e.Capacity.Quarters {
		if q.Utilisation > e.Capacity.Quarters[peak].Utilisation {
			peak = i
		}
		if q.Over {
			x.DeliveryRisks = append(x.DeliveryRisks, fmt.Sprintf("Q%d needs %s of %s person-days (%s%%), above the %s%% threshold.",
				i+1, formatNumber(q.Effort), formatNumber(q.Capacity), formatNumber(guard(q.Utilisation*100)), formatNumber(guard(e.Capacity.Threshold*100))))
		}
	}
	x.PeakQuarter = peak + 1
	for _, f := range e.GateFindings {
		if f.Severity == LintBlocking || deliveryAdvisories[f.Code] {
			x.DeliveryRisks = append(x.DeliveryRisks, f.Message)
		}
	}
	if len(x.DeliveryRisks) > 8 {
		rest := len(x.DeliveryRisks) - 7
		x.DeliveryRisks = append(x.DeliveryRisks[:7], fmt.Sprintf("…and %d more in the plan's approval checks.", rest))
	}
	return PackPayload{PlanExport: e, Pack: x}
}

// deliveryAdvisories are the advisory checks that bear on delivering the
// year; the rest (catalog references, pass criteria, RACI) are about the
// plan's record and stay on the page.
var deliveryAdvisories = map[string]bool{
	"stratum_empty": true, "wave_counts": true, "no_wave": true, "no_attributes": true, "quarter_date_order": true,
}

func committeeSuffix(label string) string {
	if label == "" {
		return ""
	}
	return "; committee " + label
}

func dayBefore(date string) string {
	t, ok := parseDate(date)
	if !ok {
		return ""
	}
	return formatDate(t.AddDate(0, 0, -1))
}

// ---- reports ----

// ReportPayload is business-report.typ's contract (DOCUMENT_TEMPLATES.md),
// with the optional tables a section may carry.
type ReportPayload struct {
	Reference        string          `json:"reference"`
	Title            string          `json:"title"`
	Subtitle         string          `json:"subtitle"`
	ClientName       string          `json:"client_name"`
	Engagement       string          `json:"engagement"`
	Version          string          `json:"version"`
	Classification   string          `json:"classification"`
	PreparedBy       string          `json:"prepared_by"`
	ReviewedBy       string          `json:"reviewed_by"`
	ReportDate       string          `json:"report_date"`
	Period           string          `json:"period"`
	Distribution     string          `json:"distribution"`
	Frameworks       []string        `json:"frameworks"`
	ExecutiveSummary string          `json:"executive_summary"`
	KeyMessages      []string        `json:"key_messages"`
	Sections         []ReportSection `json:"sections"`
	Findings         []ReportFinding `json:"findings"`
	Appendices       []ReportSection `json:"appendices"`
	Limitations      string          `json:"limitations"`
}

type ReportSection struct {
	Heading string        `json:"heading"`
	Body    string        `json:"body"`
	Tables  []ReportTable `json:"tables"`
}

type ReportTable struct {
	Caption string     `json:"caption"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

type ReportFinding struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Severity           string `json:"severity"`
	ControlID          string `json:"control_id"`
	Status             string `json:"status"`
	Observation        string `json:"observation"`
	Risk               string `json:"risk"`
	Recommendation     string `json:"recommendation"`
	ManagementResponse string `json:"management_response"`
	Owner              string `json:"owner"`
	TargetDate         string `json:"target_date"`
}

// QuarterReport is the quarterly progress report to the approval forum.
func (s *Service) QuarterReport(id int64, q int) (ReportPayload, error) {
	if q < 1 || q > 4 {
		return ReportPayload{}, invalid("quarter must be 1 to 4")
	}
	e, err := s.Export(id)
	if err != nil {
		return ReportPayload{}, err
	}
	if e.Deviations, err = s.deviationsUpTo(id, q); err != nil {
		return ReportPayload{}, err
	}
	findings, err := s.reportFindings(id, func(t Test) bool { return t.Quarter == q })
	if err != nil {
		return ReportPayload{}, err
	}
	return quarterReport(e, q, findings, s.today()), nil
}

// deviationsUpTo applies the deviation rule to the tests of quarters 1 to q
// and the thematic risks pushed by that quarter's report date, so a quarter's
// report says the same when it is downloaded again later in the year.
func (s *Service) deviationsUpTo(id int64, q int) ([]ExportDeviation, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	tests, err := s.testsOf(p, 0)
	if err != nil {
		return nil, err
	}
	view, err := s.Sampling(id)
	if err != nil {
		return nil, err
	}
	pushed, err := s.repo.Thematics(id)
	if err != nil {
		return nil, err
	}
	cutoff := ""
	if i := slices.IndexFunc(p.Quarters, func(x Quarter) bool { return x.Quarter == q }); i >= 0 {
		cutoff = p.Quarters[i].ReportDate
	}
	tests = slices.DeleteFunc(tests, func(t Test) bool { return t.Quarter > q })
	pushed = slices.DeleteFunc(pushed, func(th Thematic) bool {
		day, _, _ := strings.Cut(th.PushedAt, "T")
		return cutoff != "" && day > cutoff
	})
	return exportDeviations(computeDeviations(p, tests, view, pushed)), nil
}

// AnnualReport is the year's conclusion, an input to the ICT risk management
// framework review.
func (s *Service) AnnualReport(id int64) (ReportPayload, error) {
	e, err := s.Export(id)
	if err != nil {
		return ReportPayload{}, err
	}
	findings, err := s.reportFindings(id, func(Test) bool { return true })
	if err != nil {
		return ReportPayload{}, err
	}
	return annualReport(e, findings, s.today()), nil
}

// reportFindings reads the findings raised from the chosen tests back from
// Audit Findings, so the report states what that module holds now.
func (s *Service) reportFindings(id int64, in func(Test) bool) ([]ReportFinding, error) {
	out := []ReportFinding{}
	if s.in.Findings == nil {
		return out, nil
	}
	tests, err := s.repo.Tests(id)
	if err != nil {
		return nil, err
	}
	for _, t := range tests {
		if !in(t) {
			continue
		}
		for _, r := range t.Results {
			if r.FindingID == 0 {
				continue
			}
			f, err := s.in.Findings.Get(r.FindingID)
			switch {
			case errors.Is(err, auditfinding.ErrNotFound):
				continue
			case err != nil:
				return nil, fmt.Errorf("read finding %s: %w", r.FindingRef, err)
			}
			out = append(out, ReportFinding{ID: f.Reference, Title: f.Title, Severity: strings.ToLower(f.Severity), ControlID: f.LinkedControls,
				Status: f.Status, Observation: f.Condition, Risk: f.Effect, Recommendation: f.Recommendation,
				ManagementResponse: strings.Join(nonEmpty(f.ManagementResponse, f.ManagementResponseText), ". "), Owner: f.Owner, TargetDate: f.DueDate})
		}
	}
	return out, nil
}

func reportShell(e PlanExport, today string) ReportPayload {
	version := "Draft plan"
	if e.LatestVersion != "" {
		version = "Plan " + e.LatestVersion
	}
	frameworks := []string{}
	for f := range strings.SplitSeq(e.RegulatoryRefs, ";") {
		if f = strings.TrimSpace(f); f != "" {
			frameworks = append(frameworks, f)
		}
	}
	return ReportPayload{
		ClientName: firstNonEmpty(e.ClientName, e.FunctionName), Engagement: e.Reference + " " + e.Title, Version: version,
		Classification: "Confidential", PreparedBy: e.OwnerRole, ReviewedBy: e.AccountableRole, ReportDate: today,
		Distribution: e.ApprovalForum, Frameworks: frameworks, KeyMessages: []string{}, Sections: []ReportSection{},
		Findings: []ReportFinding{}, Appendices: []ReportSection{},
	}
}

func quarterReport(e PlanExport, q int, findings []ReportFinding, today string) ReportPayload {
	r := reportShell(e, today)
	qr := e.Quarters[slices.IndexFunc(e.Quarters, func(x ExportQuarter) bool { return x.Quarter == q })]
	start := ""
	if t, ok := parseDate(e.PeriodStart); ok {
		start = formatDate(t.AddDate(0, 3*(q-1), 0))
	}
	year := e.PeriodStart[:min(4, len(e.PeriodStart))]
	r.Reference = fmt.Sprintf("%s-Q%d", e.Reference, q)
	r.Title = fmt.Sprintf("Q%d %s progress report", q, year)
	r.Subtitle = e.Title
	r.Period = displayRange(start, qr.QuarterEnd)
	if qr.ReportDate != "" {
		r.ReportDate = qr.ReportDate
	}
	r.Findings = findings

	var summary []string
	summary = append(summary, fmt.Sprintf("This is the Q%d progress report on %s, %s, to the %s. It covers %s and reflects the plan's records on %s.",
		q, e.Reference, e.Title, firstNonEmpty(e.ApprovalForum, "approval forum"), r.Period, displayDate(today)))

	word := coverageWord(e.CoverageBasis)
	covRows := [][]string{}
	for _, c := range e.Coverage {
		cell := c.Cells[q-1]
		dq := 0
		for _, it := range e.Population {
			if it.Stratum == c.Code && it.DQStatus == DQAccepted && it.DQReviewedOn != "" && it.DQReviewedOn <= cell.QuarterEnd {
				dq++
			}
		}
		covRows = append(covRows, []string{c.Name, strconv.Itoa(c.N), formatNumber(cell.PlannedPct) + "%", strconv.Itoa(cell.Covered),
			formatNumber(cell.ActualPct) + "%", cell.RAG, strconv.Itoa(dq)})
		if c.N > 0 {
			line := fmt.Sprintf("%s: %d of %d items %s by %s, %s%% against a plan of %s%% (%s).", c.Name, cell.Covered, c.N, word,
				displayDate(cell.QuarterEnd), formatNumber(cell.ActualPct), formatNumber(cell.PlannedPct), cell.RAG)
			r.KeyMessages = append(r.KeyMessages, line)
		}
	}
	r.Sections = append(r.Sections, ReportSection{Heading: "Coverage against plan",
		Body: coverageMethod(e.CoverageBasis, cycleIn(e.Strata)) + " The plan sets a cumulative target for each stratum at each quarter end; the RAG compares it with the share " + word + " by that date: Green on or above plan, Amber within " +
			formatNumber(e.Parameters.RAGAmberPoints) + " points below, Red further below, Not due before the quarter ends. " + upperFirst(word) + " and DQ accepted count the items dated on or before the quarter end.",
		Tables: []ReportTable{{Caption: fmt.Sprintf("Coverage at the end of Q%d", q),
			Columns: []string{"Stratum", "N", "Target", upperFirst(word), "Share", "RAG", "DQ accepted"}, Rows: covRows}}})

	var tr TestingRow
	if q-1 < len(e.Testing) {
		tr = e.Testing[q-1]
	}
	testRows := [][]string{}
	fails := 0
	for _, t := range e.Tests {
		if t.Quarter != q {
			continue
		}
		fails += t.Fails
		qa := "Awaiting"
		if t.QADate != "" {
			qa = displayDate(t.QADate)
		}
		testRows = append(testRows, []string{t.TestRef, t.ItemRef, t.ItemName, t.Stratum, t.Overall, strconv.Itoa(t.Fails), qa})
	}
	testing := fmt.Sprintf("2LoD planned %d test(s) for Q%d, drew %d item(s), opened %d test(s), completed fieldwork on %d and signed off %d. %d attribute fail(s) were recorded.",
		tr.Planned, q, tr.Selected, tr.Opened, tr.Fieldwork, tr.QA, fails)
	summary = append(summary, testing)
	r.KeyMessages = append(r.KeyMessages, fmt.Sprintf("Q%d testing: %d of %d planned tests opened, %d signed off by QA, %d attribute fail(s).", q, tr.Opened, tr.Planned, tr.QA, fails))
	sec := ReportSection{Heading: fmt.Sprintf("Testing in Q%d", q), Body: testing + " A test's result is Effective with no fails, Partially effective from " +
		strconv.Itoa(e.Parameters.ResultPartialMin) + " and Ineffective from " + strconv.Itoa(e.Parameters.ResultIneffectiveMin) + "; a result not yet signed off by QA is provisional.",
		Tables: []ReportTable{}}
	if len(testRows) > 0 {
		sec.Tables = append(sec.Tables, ReportTable{Caption: fmt.Sprintf("Q%d tests", q),
			Columns: []string{"Test", "Item", "Name", "Stratum", "Result", "Fails", "QA signed off"}, Rows: testRows})
	}
	r.Sections = append(r.Sections, sec)

	devSec, notEffective := deviationSection(e, "Deviations so far this year")
	r.Sections = append(r.Sections, devSec)
	if len(notEffective) > 0 {
		msg := "Under the deviation rule, " + strings.Join(notEffective, "; ") + " " + isAre(len(notEffective)) + " not effective for the year so far."
		summary = append(summary, msg)
		r.KeyMessages = append(r.KeyMessages, msg)
	}

	if len(findings) > 0 {
		summary = append(summary, fmt.Sprintf("%d finding(s) were raised in Audit Findings from Q%d's fails; they are set out below as that module holds them.", len(findings), q))
	}
	if e.FindingsMissing > 0 {
		summary = append(summary, fmt.Sprintf("%d finding(s) raised from the plan were since deleted in Audit Findings and are not reported.", e.FindingsMissing))
	}

	load := e.Capacity.Quarters[q-1]
	r.Sections = append(r.Sections, ReportSection{Heading: "Resourcing",
		Body: fmt.Sprintf("The plan budgets %s person-days for Q%d against a capacity of %s (%s%%, threshold %s%%).", formatNumber(load.Effort), q,
			formatNumber(load.Capacity), formatNumber(guard(load.Utilisation*100)), formatNumber(guard(e.Capacity.Threshold*100))), Tables: []ReportTable{}})

	next := "The annual conclusion goes to the " + firstNonEmpty(e.ApprovalForum, "approval forum") + " on " + firstNonEmpty(displayDate(e.ClosingDate), "the closing date") + "."
	if q < 4 {
		n := e.Quarters[slices.IndexFunc(e.Quarters, func(x ExportQuarter) bool { return x.Quarter == q+1 })]
		what := "the sample is drawn"
		if drawWord(e.Strata) == "selected" {
			what = "the wave is selected"
		}
		next = fmt.Sprintf("Q%d: the 1LoD wave is due by %s, %s on %s, fieldwork runs %s and the report is due on %s.",
			q+1, displayDate(n.LOD1Deadline), what, displayDate(n.SelectionDate), displayRange(n.FieldworkStart, n.FieldworkEnd), displayDate(n.ReportDate))
	}
	r.Sections = append(r.Sections, ReportSection{Heading: "Next quarter", Body: next, Tables: []ReportTable{}})

	r.ExecutiveSummary = strings.Join(summary, "\n\n")
	r.Appendices = append(r.Appendices, attributesAppendix(e))
	r.Limitations = limitations(e, today, false)
	return r
}

func annualReport(e PlanExport, findings []ReportFinding, today string) ReportPayload {
	r := reportShell(e, today)
	year := e.PeriodStart[:min(4, len(e.PeriodStart))]
	r.Reference = e.Reference + "-ANNUAL"
	r.Title = "Annual conclusion " + year
	r.Subtitle = "Input to the ICT risk management framework review report (RTS (EU) 2024/1774 Art. 27)"
	r.Period = displayRange(e.PeriodStart, e.PeriodEnd)
	if e.ClosingDate != "" {
		r.ReportDate = e.ClosingDate
	}
	r.Findings = findings

	popRows := [][]string{}
	objective := "The year's objective is every item of every stratum attested by its 1LoD owner and its data quality accepted by 2LoD."
	columns := []string{"Stratum", "N", "Attested", "DQ accepted"}
	if e.CoverageBasis == CoverageTests {
		objective = "The year's objective is every item due in the period tested, with the test signed off by QA. " + coverageMethod(e.CoverageBasis, cycleIn(e.Strata))
		columns = []string{"Stratum", "N", "Tested and signed off", "In cycle", "Covered"}
	}
	for _, c := range e.Coverage {
		if e.CoverageBasis == CoverageTests {
			popRows = append(popRows, []string{c.Name, strconv.Itoa(c.N), strconv.Itoa(c.Tested), strconv.Itoa(c.InCycle),
				fmt.Sprintf("%d (%s%%)", c.Covered, formatNumber(c.CoveredPct))})
			continue
		}
		popRows = append(popRows, []string{c.Name, strconv.Itoa(c.N), fmt.Sprintf("%d (%s%%)", c.Attested, formatNumber(c.AttestedPct)),
			fmt.Sprintf("%d (%s%%)", c.DQAccepted, formatNumber(c.DQPct))})
	}
	tables := []ReportTable{{Caption: "Coverage achieved", Columns: columns, Rows: popRows}}
	if byType := testsByType(e); len(byType.Rows) > 0 {
		tables = append(tables, byType)
	}
	r.Sections = append(r.Sections, ReportSection{Heading: "Population and coverage",
		Body:   fmt.Sprintf("The population is the %d item(s) in scope%s. %s", e.Totals.PopulationSize, frozenNote(e.PopulationFrozenAt), objective),
		Tables: tables})

	testRows := [][]string{}
	for _, t := range e.Testing {
		testRows = append(testRows, []string{"Q" + strconv.Itoa(t.Quarter), strconv.Itoa(t.Planned), strconv.Itoa(t.Opened), strconv.Itoa(t.QA),
			strconv.Itoa(t.ByResult[OverallEffective]), strconv.Itoa(t.ByResult[OverallPartial]), strconv.Itoa(t.ByResult[OverallIneffective])})
	}
	stratumRows := [][]string{}
	for _, st := range e.Strata {
		opened := 0
		for _, t := range e.Tests {
			if t.Stratum == st.Code {
				opened++
			}
		}
		stratumRows = append(stratumRows, []string{st.Name, strconv.Itoa(st.N), strconv.Itoa(st.AnnualSample), strconv.Itoa(st.TotalTests), strconv.Itoa(opened)})
	}
	r.Sections = append(r.Sections, ReportSection{Heading: "Tests performed",
		Body: fmt.Sprintf("2LoD planned %d test(s), opened %d and signed off %d. Samples were sized by attribute sampling at each stratum's confidence and tolerable deviation rate, with a coverage floor and risk-based top-ups, and drawn each quarter from a seed recorded when the population was frozen, so the draws can be re-performed.",
			e.Totals.PlannedTests, e.Totals.TestsOpened, e.Totals.TestsSignedOff),
		Tables: []ReportTable{
			{Caption: "By quarter", Columns: []string{"Quarter", "Planned", "Opened", "QA signed off", "Effective", "Partially effective", "Ineffective"}, Rows: testRows},
			{Caption: "By stratum", Columns: []string{"Stratum", "N", "Statistical sample", "Planned tests", "Tests performed"}, Rows: stratumRows},
		}})

	devSec, _ := deviationSection(e, "Results by attribute and stratum")
	devSec.Body = "The deviation rule is applied per attribute and stratum, cumulatively over the year: no deviations is effective; " +
		strconv.Itoa(e.Parameters.DeviationExtendAt) + " extends the sample; " + strconv.Itoa(e.Parameters.DeviationThematicAt) +
		" or more is not effective, a thematic finding. Rows with no tests are not listed; Appendix A names each attribute's domain."
	devSec.Tables = []ReportTable{{Caption: "Conclusions", Columns: []string{"Attribute", "Stratum", "Tested", "Deviations", "Conclusion", "Risk raised"},
		Rows: [][]string{}}}
	for _, d := range e.Deviations {
		if d.Tested == 0 {
			continue
		}
		devSec.Tables[0].Rows = append(devSec.Tables[0].Rows, []string{d.AttrRef, d.Stratum, strconv.Itoa(d.Tested), strconv.Itoa(d.Deviations), d.Conclusion, d.RiskRef})
	}
	r.Sections = append(r.Sections, devSec)

	sevRows := [][]string{}
	for _, c := range e.FindingsBy {
		sevRows = append(sevRows, []string{c.Severity, strconv.Itoa(c.Raised), strconv.Itoa(c.Open), strconv.Itoa(c.Overdue), strconv.Itoa(c.Closed)})
	}
	r.Sections = append(r.Sections, ReportSection{Heading: "Findings",
		Body: fmt.Sprintf("%d finding(s) were raised in Audit Findings from the year's fails: %d open, %d overdue. Each is set out below as Audit Findings holds it.",
			e.Totals.FindingsRaised, e.Totals.FindingsOpen, e.Totals.FindingsOverdue),
		Tables: []ReportTable{{Caption: "By severity", Columns: []string{"Severity", "Raised", "Open", "Overdue", "Closed or accepted"}, Rows: sevRows}}})

	conclusion := annualConclusion(e)
	r.Sections = append(r.Sections, ReportSection{Heading: "Conclusion", Body: strings.Join(conclusion, "\n\n"), Tables: []ReportTable{}})

	summary := []string{fmt.Sprintf("This is the annual conclusion of %s, %s, for %s. It is an input to the ICT risk management framework review report (DORA Art. 6(5); RTS (EU) 2024/1774 Art. 27).",
		e.Reference, e.Title, r.Period)}
	summary = append(summary, conclusion...)
	r.ExecutiveSummary = strings.Join(summary, "\n\n")
	for _, c := range e.Coverage {
		switch {
		case c.N == 0:
		case e.CoverageBasis == CoverageTests:
			r.KeyMessages = append(r.KeyMessages, fmt.Sprintf("%s: %d of %d items covered (%s%%), %d tested this year and %d inside their cycle.", c.Name, c.Covered, c.N, formatNumber(c.CoveredPct), c.Tested, c.InCycle))
		default:
			r.KeyMessages = append(r.KeyMessages, fmt.Sprintf("%s: %d of %d items attested (%s%%), %d with data quality accepted.", c.Name, c.Attested, c.N, formatNumber(c.AttestedPct), c.DQAccepted))
		}
	}
	r.KeyMessages = append(r.KeyMessages, fmt.Sprintf("%d of %d planned tests performed, %d signed off by QA.", e.Totals.TestsOpened, e.Totals.PlannedTests, e.Totals.TestsSignedOff))
	if e.Totals.NotEffective > 0 {
		r.KeyMessages = append(r.KeyMessages, fmt.Sprintf("%d attribute and stratum pair(s) concluded not effective.", e.Totals.NotEffective))
	}
	r.KeyMessages = append(r.KeyMessages, fmt.Sprintf("%d finding(s) raised, %d open, %d overdue.", e.Totals.FindingsRaised, e.Totals.FindingsOpen, e.Totals.FindingsOverdue))

	r.Appendices = append(r.Appendices, attributesAppendix(e))
	if len(e.RegulatoryBasis) > 0 {
		rows := [][]string{}
		for _, b := range e.RegulatoryBasis {
			rows = append(rows, []string{b.Reference, b.Requirement, b.How})
		}
		r.Appendices = append(r.Appendices, ReportSection{Heading: "Regulatory basis",
			Tables: []ReportTable{{Columns: []string{"Reference", "Requirement", "How the plan meets it"}, Rows: rows}}})
	}
	r.Limitations = limitations(e, today, true)
	return r
}

// annualConclusion words the year's conclusion per stratum from the deviation
// rule, and says plainly where testing was incomplete. It is the plan's
// conclusion as the data supports it; the control function signs it off.
func annualConclusion(e PlanExport) []string {
	out := []string{}
	for _, st := range e.Strata {
		tested := 0
		for _, t := range e.Tests {
			if t.Stratum == st.Code {
				tested++
			}
		}
		var notEffective, extended []string
		for _, d := range e.Deviations {
			if d.Stratum != st.Code {
				continue
			}
			switch d.Kind {
			case KindNotEffective:
				notEffective = append(notEffective, attrLabel(d.AttrRef, d.Domain))
			case KindExtend:
				extended = append(extended, attrLabel(d.AttrRef, d.Domain))
			}
		}
		switch {
		case tested == 0:
			out = append(out, st.Name+": no tests were performed, so no conclusion is given on its controls.")
		case len(notEffective) > 0:
			out = append(out, fmt.Sprintf("%s: on the %d test(s) performed, the controls tested are effective except for %s, concluded not effective under the deviation rule.",
				st.Name, tested, strings.Join(notEffective, ", ")))
		case len(extended) == 1:
			out = append(out, fmt.Sprintf("%s: on the %d test(s) performed, the controls tested are effective. %s had one deviation, and its sample was extended.",
				st.Name, tested, extended[0]))
		case len(extended) > 1:
			out = append(out, fmt.Sprintf("%s: on the %d test(s) performed, the controls tested are effective. %s each had one deviation, and their samples were extended.",
				st.Name, tested, strings.Join(extended, ", ")))
		default:
			out = append(out, fmt.Sprintf("%s: on the %d test(s) performed, the controls tested are effective, with no deviations.", st.Name, tested))
		}
	}
	if e.Totals.TestsOpened < e.Totals.PlannedTests || e.Totals.TestsSignedOff < e.Totals.TestsOpened {
		out = append(out, fmt.Sprintf("Testing is incomplete: %d of %d planned tests were performed and %d signed off by QA. The conclusion is limited to the tests performed.",
			e.Totals.TestsOpened, e.Totals.PlannedTests, e.Totals.TestsSignedOff))
	}
	return out
}

func attrLabel(ref, domain string) string {
	if domain == "" {
		return ref
	}
	return ref + " (" + domain + ")"
}

func deviationSection(e PlanExport, heading string) (ReportSection, []string) {
	rows := [][]string{}
	notEffective := []string{}
	for _, d := range e.Deviations {
		if d.Deviations == 0 {
			continue
		}
		next := ""
		switch {
		case d.RiskRef != "":
			next = "Risk " + d.RiskRef + " raised"
		case d.Kind == KindNotEffective:
			next = "Thematic finding to raise"
		case d.Kind == KindExtend && d.FullCoverage:
			next = "Findings raised on each fail"
		case d.Kind == KindExtend && d.ExtendQuarter > 0:
			next = fmt.Sprintf("+%d test(s) in Q%d", d.ExtendBy, d.ExtendQuarter)
		case d.Kind == KindExtend:
			next = "Carry into next year's sample"
		}
		if d.Kind == KindNotEffective {
			notEffective = append(notEffective, attrLabel(d.AttrRef, d.Domain)+" in "+d.Stratum)
		}
		rows = append(rows, []string{d.AttrRef, d.Stratum, strconv.Itoa(d.Tested), strconv.Itoa(d.Deviations), d.Conclusion, next})
	}
	sec := ReportSection{Heading: heading, Tables: []ReportTable{}}
	if len(rows) == 0 {
		sec.Body = "No deviations have been recorded."
		return sec, notEffective
	}
	sec.Body = "The deviation rule is cumulative for the year, per attribute and stratum. Only attributes with a deviation are listed."
	sec.Tables = append(sec.Tables, ReportTable{Columns: []string{"Attribute", "Stratum", "Tested", "Deviations", "Conclusion", "Next step"}, Rows: rows})
	return sec, notEffective
}

func attributesAppendix(e PlanExport) ReportSection {
	rows := [][]string{}
	for _, a := range e.Attributes {
		rows = append(rows, []string{a.AttrRef, a.Domain, a.Method, strings.Join(a.ControlIDs, ", ")})
	}
	sec := ReportSection{Heading: "Test attributes", Tables: []ReportTable{}}
	if len(rows) == 0 {
		sec.Body = "The plan has no test attributes."
		return sec
	}
	sec.Body = "What 2LoD tested on every selected item. The full requirement, procedure, evidence and pass criteria are in the plan's export."
	sec.Tables = append(sec.Tables, ReportTable{Columns: []string{"Ref", "Domain", "Method", "800-53"}, Rows: rows})
	return sec
}

func frozenNote(at string) string {
	if at == "" {
		return ", not yet frozen"
	}
	day, _, _ := strings.Cut(at, "T")
	return ", frozen on " + displayDate(day)
}

func limitations(e PlanExport, today string, annual bool) string {
	forum := firstNonEmpty(e.ApprovalForum, "the approval forum")
	parts := []string{
		fmt.Sprintf("This report is prepared by the %s for the %s, from the records of assurance plan %s as they stood on %s. It does not reflect changes made after that date.",
			firstNonEmpty(e.FunctionName, "control function"), forum, e.Reference, displayDate(today)),
		"Coverage is measured from the " + coverageSource(e.CoverageBasis) + " recorded in the plan. " + testingBasis(e.Strata) +
			" Results not signed off by QA are provisional.",
		"Findings are reported as Audit Findings holds them on the report date; their remediation is followed up there.",
	}
	if annual {
		parts = append(parts, "The conclusion is the control function's on the tests performed. It is not an opinion on the design or operation of controls outside the plan's scope, and it does not replace the work of the other assurance plans, such as resilience testing, or of internal audit.")
	}
	return strings.Join(parts, " ")
}

func coverageSource(basis string) string {
	if basis == CoverageTests {
		return "QA sign-off dates of the tests and the last-test dates"
	}
	return "attestation and data-quality review dates"
}

// testingBasis says what assurance the tests give: a sample gives reasonable
// assurance; full coverage tests every item due, which is still not absolute.
func testingBasis(strata []ExportStratum) string {
	sampled := slices.ContainsFunc(strata, func(st ExportStratum) bool { return st.SamplingEnabled })
	full := slices.ContainsFunc(strata, func(st ExportStratum) bool { return !st.SamplingEnabled })
	switch {
	case sampled && full:
		return "Sampled strata give reasonable rather than absolute assurance; full-coverage strata test every item due, on the evidence produced."
	case full:
		return "Every item due is tested, on the evidence produced; that gives reasonable rather than absolute assurance."
	}
	return "Testing is on samples, which gives reasonable rather than absolute assurance."
}

// drawWord is how a quarter's tests are chosen: drawn as a sample, or, when
// no stratum samples, selected as the whole wave.
func drawWord(strata []ExportStratum) string {
	if slices.ContainsFunc(strata, func(st ExportStratum) bool { return st.SamplingEnabled }) {
		return "drawn"
	}
	return "selected"
}

func cycleIn(strata []ExportStratum) bool {
	return slices.ContainsFunc(strata, func(st ExportStratum) bool { return st.CycleYears > 1 })
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// testsByType counts the year's tests by type, for a plan that runs more than
// one kind (a resilience testing programme).
func testsByType(e PlanExport) ReportTable {
	t := ReportTable{Caption: "Tests by type", Columns: []string{"Test type", "Opened", "Signed off by QA", "With a fail"}, Rows: [][]string{}}
	if len(e.TestTypes) < 2 {
		return t
	}
	type count struct{ opened, signed, failed int }
	counts := map[string]*count{}
	var order []string
	for _, typ := range e.TestTypes {
		counts[typ] = &count{}
		order = append(order, typ)
	}
	for _, x := range e.Tests {
		typ := firstNonEmpty(x.TestType, "Not set")
		c, ok := counts[typ]
		if !ok {
			c = &count{}
			counts[typ] = c
			order = append(order, typ)
		}
		c.opened++
		if x.QADate != "" {
			c.signed++
		}
		if x.Fails > 0 {
			c.failed++
		}
	}
	for _, typ := range order {
		c := counts[typ]
		t.Rows = append(t.Rows, []string{typ, strconv.Itoa(c.opened), strconv.Itoa(c.signed), strconv.Itoa(c.failed)})
	}
	return t
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
