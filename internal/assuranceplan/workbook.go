package assuranceplan

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"grc/internal/auditfinding"
	"grc/internal/xlsx"
)

// The plan workbook's sheets, in their order.
const (
	wbGuide      = "Guide"
	wbPlan       = "Plan"
	wbSchedule   = "Schedule"
	wbQuarters   = "Quarters"
	wbStrata     = "Strata"
	wbScript     = "Test script"
	wbPopulation = "Population"
	wbTests      = "Tests"
	wbResults    = "Results"
	wbDeviations = "Deviations"
	wbCoverage   = "Coverage"
	wbFindings   = "Findings"
	wbGovernance = "Governance"
	wbHistory    = "History"
)

// Columns of the sheets that other sheets' formulas read. Every table has its
// header on row 1 and its rows from row 2.
const (
	stCode = iota + 1
	stName
	stDefinition
	stSampling
	stConfidence
	stTolerable
	stFloorPct
	stEffortTest
	stEffortReview
	stCycleYears
	stTakes
	stN
	stNSource
	stInCycle
	stN0
	stFPC
	stFloor
	stAnnual
	stWave1
)

const (
	stTopUp1   = stWave1 + 4
	stTests1   = stTopUp1 + 4
	stTotal    = stTests1 + 4
	stCoverage = stTotal + 1
	stReviews1 = stCoverage + 1
	stTarget1  = stReviews1 + 4
	stError    = stTarget1 + 4
)

const (
	qtQuarter = iota + 1
	qtEnd
	qtWindow
	qtDeadline
	qtSelection
	qtFieldStart
	qtFieldEnd
	qtAgreed
	qtReport
	qtCommittee
	qtPlanned
	qtReviews
	qtTestEffort
	qtReviewEffort
	qtRetest
	qtOverhead
	qtEffort
	qtCapacity
	qtUtil
	qtOver
	qtPicks
	qtSelectedAt
)

const (
	poRef = iota + 1
	poName
	poStratum
	poCIF
	poLine
	poHosting
	poInternet
	poOwner
	poWave
	poAttested
	poDQ
	poDQOn
	poTopUpQ
	poTopUpWhy
	poLastTested
	poCycleDue
	poSourceKind
	poSourceRef
	poSelectedQ
	poBasis
	poRank
	poTestRef
	poOverall
	poSignedOff
	poCoveredOn
	poCoveredNow
	poCovered1
)

const (
	tsRef = iota + 1
	tsType
	tsQuarter
	tsItemRef
	tsItemName
	tsStratum
	tsBasis
	tsTester
	tsRequested
	tsDue
	tsReceived
	tsLate
	tsFieldwork
	tsQAReviewer
	tsQADate
	tsPass
	tsFail
	tsNA
	tsPending
	tsOverall
	tsFindings
	tsSummary
	tsNotes
	tsQANote
)

const (
	rsTest = iota + 1
	rsQuarter
	rsItemRef
	rsItemName
	rsStratum
	rsAttr
	rsDomain
	rsRequirement
	rsResult
	rsNote
	rsFinding
	rsSeverity
)

const (
	dvAttr = iota + 1
	dvDomain
	dvStratum
	dvSampling
	dvTested
	dvFails
	dvKind
	dvConclusion
	dvFailures
	dvExtendQ
	dvExtendBy
	dvRisk
)

const (
	cvCode = iota + 1
	cvName
	cvN
	cvAttested
	cvAttestedPct
	cvTested
	cvCovered
	cvCoveredPct
	cvDQ
	cvDQPct
	cvInCycle
	cvQ1
)

const (
	fdRef = iota + 1
	fdTitle
	fdSeverity
	fdStatus
	fdDue
	fdOverdue
	fdMissing
	fdTest
	fdAttr
	fdItem
)

// ExportXLSX writes the plan as a workbook to work the year in: the design and
// the year so far, with the rules of the plan page as formulas over its own
// rows.
func (s *Service) ExportXLSX(id int64) (string, []byte, error) {
	e, err := s.Export(id)
	if err != nil {
		return "", nil, err
	}
	var buf bytes.Buffer
	if err := PlanWorkbook(e).Write(&buf); err != nil {
		return "", nil, fmt.Errorf("write plan workbook: %w", err)
	}
	return fileStem(e.Reference, id) + ".xlsx", buf.Bytes(), nil
}

type wbCol struct {
	head  string
	width float64
}

// planBook holds what one sheet's formulas need to know about another's rows.
type planBook struct {
	e          PlanExport
	today      string
	strataLast int
	quarterRow map[int]int
	quarterEnd map[int]string
	param      map[string]string
	holidays   string
}

// PlanWorkbook lays the export out as a workbook. Nothing in it needs the
// application: each figure the plan page works out is a formula here, and the
// value beside it is what the application worked out, for a viewer that does
// not calculate.
func PlanWorkbook(e PlanExport) *xlsx.Workbook {
	b := &planBook{e: e, today: time.Now().UTC().Format(dateLayout), quarterRow: map[int]int{},
		quarterEnd: map[int]string{}, param: map[string]string{}}
	if len(e.GeneratedAt) >= 10 {
		if _, ok := parseDate(e.GeneratedAt[:10]); ok {
			b.today = e.GeneratedAt[:10]
		}
	}
	b.strataLast = 1 + max(1, len(e.Strata))
	for i, q := range e.Quarters {
		if q.Quarter >= 1 && q.Quarter <= 4 {
			b.quarterRow[q.Quarter] = i + 2
		}
	}
	for q := 1; q <= 4; q++ {
		if end, ok := QuarterEnd(e.PeriodStart, q); ok {
			b.quarterEnd[q] = end
		}
	}

	book := xlsx.New()
	b.guide(book.AddSheet(wbGuide))
	// The Plan sheet holds the parameters the formulas of the sheets after it
	// read, so it is written first.
	b.plan(book.AddSheet(wbPlan))
	b.schedule(book.AddSheet(wbSchedule))
	b.quarters(book.AddSheet(wbQuarters))
	b.strata(book.AddSheet(wbStrata))
	b.script(book.AddSheet(wbScript))
	b.population(book.AddSheet(wbPopulation))
	b.tests(book.AddSheet(wbTests))
	b.results(book.AddSheet(wbResults))
	b.deviations(book.AddSheet(wbDeviations))
	b.coverage(book.AddSheet(wbCoverage))
	b.findings(book.AddSheet(wbFindings))
	b.governance(book.AddSheet(wbGovernance))
	b.history(book.AddSheet(wbHistory))
	book.Active = 1
	return book
}

func header(s *xlsx.Sheet, row, freezeCols int, cols ...wbCol) {
	widths := make([]float64, len(cols))
	for i, c := range cols {
		s.Set(i+1, row, xlsx.Text(c.head).With(xlsx.Header))
		widths[i] = c.width
	}
	if row == 1 {
		s.Widths(widths...)
		s.Freeze(freezeCols, 1)
	}
}

// filter puts filter buttons on a table's header row. A table with no rows
// still gets one row under the header, so the range is not just the header.
func filter(s *xlsx.Sheet, cols, rows int) {
	s.AutoFilter(xlsx.Range(1, 1, cols, 1+max(1, rows)))
	s.FitPageWidth()
}

func whole(sheet string, col int) string {
	return xlsx.SheetRef(sheet, "$"+xlsx.Col(col)+":$"+xlsx.Col(col))
}

func span(sheet string, col, first, last int) string {
	return xlsx.SheetRef(sheet, xlsx.AbsRange(col, first, col, last))
}

// here is a cell of the row a formula is on, with its column fixed, so the
// formula still works when a row is copied down.
func here(col, row int) string { return "$" + xlsx.Col(col) + strconv.Itoa(row) }

func lit(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func qCell(q int) xlsx.Cell {
	if q < 1 || q > 4 {
		return xlsx.Cell{}
	}
	return xlsx.Int(q).With(xlsx.Quarter)
}

func dateValue(iso string) (float64, bool) {
	t, ok := parseDate(iso)
	if !ok {
		return 0, false
	}
	return xlsx.Serial(t), true
}

func dateFormula(expr, iso string) xlsx.Cell {
	if v, ok := dateValue(iso); ok {
		return xlsx.FormulaNumber(expr, v).With(xlsx.ISODate)
	}
	return xlsx.FormulaText(expr, "").With(xlsx.ISODate)
}

func percent(v float64) xlsx.Cell { return xlsx.Number(v).With(xlsx.Percent) }

func ratio(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

func text(s string) xlsx.Cell { return xlsx.Text(s) }

func wrap(s string) xlsx.Cell { return xlsx.Text(s).With(xlsx.Wrap) }

// listable reports whether values can be a dropdown: the list is written
// comma-separated and quoted, in at most 255 characters.
func listable(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		if v == "" || strings.ContainsAny(v, `,"`) {
			return false
		}
	}
	return len(strings.Join(values, ",")) <= 255
}

// quarterEndRef is a quarter's end as a formula reads it: the Quarters
// sheet's cell, or the date itself when that sheet has no row for it.
func (b *planBook) quarterEndRef(q int) string {
	if row, ok := b.quarterRow[q]; ok {
		return xlsx.SheetRef(wbQuarters, xlsx.Abs(qtEnd, row))
	}
	v, _ := dateValue(b.quarterEnd[q])
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func (b *planBook) guide(s *xlsx.Sheet) {
	e := b.e
	s.Widths(118)
	row := 1
	line := func(t string, style xlsx.Style) {
		s.Set(1, row, xlsx.Text(t).With(style))
		row++
	}
	line("Assurance plan workbook: "+strings.TrimSpace(e.Reference+" "+e.Title), xlsx.Title)
	line("A working copy of the plan from the GRC application: its design, its quarters and the year so far. Nothing here needs the application. What the plan page works out (a test's result, the deviation rule, coverage against the waves, capacity) is a formula over the rows of this file, with the plan's own thresholds.", xlsx.Wrap)
	line("Exported "+e.GeneratedAt+", plan status "+e.Status+", revision "+strconv.Itoa(e.Revision)+". It is a snapshot: what is changed here does not go back to the application, and later changes there do not come here. The application stays the record; enter the year's work there, or download a new workbook to bring this one up to date.", xlsx.Wrap)
	row++

	line("Working the year here", xlsx.Section)
	line("1. Results: one row per test and attribute. Choose Pass, Fail or N/A in the Result column (yellow). Leave it empty while the attribute is not assessed yet. A row in yellow is a Fail.", xlsx.Wrap)
	line("2. Tests: the tester, the dates and QA. Enter dates as dates (2027-03-31). Evidence due is the requested date plus the evidence SLA in business days, skipping weekends and the plan's non-working dates. Pass, Fail, N/A, Pending and Overall count the test's rows on Results, and Overall grades the fails at the plan's thresholds.", xlsx.Wrap)
	line("3. Population: the 1LoD attestation date, the 2LoD data-quality status (yellow) and its review date. The columns from Signed off on are worked out: they say whether each item counts as covered today and at each quarter end, and Coverage counts them.", xlsx.Wrap)
	line("4. Schedule: set each step's status. A step past its end date and not Complete is shown in yellow.", xlsx.Wrap)
	line("5. Deviations, Coverage, Quarters and the headline figures on Plan follow by themselves.", xlsx.Wrap)
	line("To add a row, copy the row above it and change what it holds: the formulas come with it. The formulas count whole columns, so a copied row counts too.", xlsx.Wrap)
	row++

	line("The sheets", xlsx.Section)
	line(wbPlan+": the plan's header, its parameters, the headline figures, the regulatory basis and the non-working dates. Locked.", xlsx.Wrap)
	line(wbSchedule+": the plan's steps with their roles and dates, and a Status column to keep up to date.", xlsx.Wrap)
	line(wbQuarters+": the quarterly cycle (1LoD window, selection, fieldwork, findings, report, committee) and each quarter's planned tests, effort, capacity and utilisation. Locked.", xlsx.Wrap)
	line(wbStrata+": the strata, their sampling parameters and the sizes worked out from them: n0, the finite population correction, the floor, the annual sample, the waves, the top-ups and the tests in each quarter. Locked.", xlsx.Wrap)
	line(wbScript+": the attributes each test assesses, with the procedure, the evidence and the pass criteria. Locked.", xlsx.Wrap)
	line(wbPopulation+": the frozen scope population, what was selected and in which quarter, and the coverage of each item.", xlsx.Wrap)
	line(wbTests+": one row per test opened, with its evidence, fieldwork, QA and result.", xlsx.Wrap)
	line(wbResults+": one row per test and attribute: the result, the note and the finding raised.", xlsx.Wrap)
	line(wbDeviations+": the deviation rule, per attribute and stratum, over the year so far. Locked.", xlsx.Wrap)
	line(wbCoverage+": each stratum's coverage today and at each quarter end against the cumulative wave target, with its RAG. Locked.", xlsx.Wrap)
	line(wbFindings+": the findings raised from the plan, as Audit Findings had them at the export. Locked.", xlsx.Wrap)
	line(wbGovernance+": the RACI, the reporting cadence and the escalations. Locked.", xlsx.Wrap)
	line(wbHistory+": the approved versions and what the approval gate says about the plan. Locked.", xlsx.Wrap)
	row++

	line("How the figures are worked out", xlsx.Section)
	line("A test's overall result: Not started while nothing is Pass or Fail; Ineffective from "+strconv.Itoa(e.Parameters.ResultIneffectiveMin)+" fails; Partially effective from "+strconv.Itoa(e.Parameters.ResultPartialMin)+"; Effective otherwise. N/A is not tested and does not count. Evidence is late when it came after its due date, or has not come and the due date has passed.", xlsx.Wrap)
	line("The deviation rule, per attribute and stratum, counts the Pass and Fail results of the year so far. No fails is effective. From "+strconv.Itoa(e.Parameters.DeviationExtendAt)+" fail the sample is extended in the next quarter by "+formatNumber(e.Parameters.ExtendPct)+"% of that quarter's tests in the stratum, rounded up; in a stratum tested in full there is no sample to extend, and the fails are raised as findings. From "+strconv.Itoa(e.Parameters.DeviationThematicAt)+" fails the attribute is not effective: a thematic finding for the risk register.", xlsx.Wrap)
	line("Coverage: "+coverageMethod(e.CoverageBasis, b.hasCycle())+" The target at each quarter end is the cumulative share of the waves. A quarter that has not ended is Not due; after it, coverage on or above target is Green, up to "+formatNumber(e.Parameters.RAGAmberPoints)+" percentage points below is Amber, further below is Red.", xlsx.Wrap)
	line("Capacity: each quarter's effort is its tests times the effort per test, plus the reviews due times the effort per review, plus retest and overhead days, against tester FTE times productive days. Utilisation above "+formatNumber(e.Parameters.UtilisationThreshold*100)+"% is shown in yellow.", xlsx.Wrap)
	line("The sample sizes on Strata, the selection, the failing tests listed on Deviations and the findings are as the application had them at the export: they come from the frozen population, the seed and Audit Findings, which this file does not hold.", xlsx.Wrap)
	line("The locked sheets hold what the plan's approval fixed. Review > Unprotect Sheet lifts the lock; there is no password. A change to the design is made in the application, through a change request once the plan is approved.", xlsx.Wrap)
	line("The formulas use MINIFS and MAXIFS: Excel 2019 or Microsoft 365, or LibreOffice.", xlsx.Muted)
	s.FitPageWidth()
}

func (b *planBook) hasCycle() bool {
	for _, st := range b.e.Strata {
		if st.CycleYears > 1 {
			return true
		}
	}
	return false
}

func (b *planBook) plan(s *xlsx.Sheet) {
	e := b.e
	pr := e.Parameters
	s.Widths(42, 70, 60)
	row := 1
	s.Set(1, row, xlsx.Text(strings.TrimSpace(e.Reference+" "+e.Title)).With(xlsx.Title))
	row++
	s.Set(1, row, xlsx.Text("Exported from the GRC application "+e.GeneratedAt+". The headline figures follow the Tests, Results, Deviations and Findings sheets.").With(xlsx.Muted))
	row++
	section := func(title string) {
		row++
		for c := 1; c <= 3; c++ {
			s.Set(c, row, xlsx.Text("").With(xlsx.Section))
		}
		s.Set(1, row, xlsx.Text(title).With(xlsx.Section))
		row++
	}
	field := func(label string, c xlsx.Cell) string {
		s.Set(1, row, xlsx.Text(label).With(xlsx.Label))
		s.Set(2, row, c)
		ref := xlsx.SheetRef(wbPlan, xlsx.Abs(2, row))
		row++
		return ref
	}

	section("Plan")
	field("Reference", text(e.Reference))
	field("Title", wrap(e.Title))
	field("Plan type", text(firstNonEmpty(e.PlanTypeName, e.PlanType)))
	field("Function", text(e.FunctionName))
	field("Client", text(e.ClientName))
	field("Period start", xlsx.Date(e.PeriodStart))
	field("Period end", xlsx.Date(e.PeriodEnd))
	field("Mobilisation starts", xlsx.Date(e.MobiliseStart))
	field("Closing date", xlsx.Date(e.ClosingDate))
	field("Objective", wrap(e.Objective))
	field("Decisions requested", wrap(strings.Join(e.DecisionsRequested, "\n")))
	field("Regulatory references", wrap(e.RegulatoryRefs))
	field("Owner", text(e.OwnerRole))
	field("Accountable", text(e.AccountableRole))
	field("Approval forum", text(e.ApprovalForum))
	field("Status", text(e.Status))
	field("Revision", xlsx.Int(e.Revision).With(xlsx.Left))
	field("Revision status", text(e.RevisionStatus))
	field("Approved by", text(e.ApprovedBy))
	field("Approved at", text(e.ApprovedAt))
	field("Activated at", text(e.ActivatedAt))
	field("Closed at", text(e.ClosedAt))
	field("Latest approved version", text(e.LatestVersion))
	field("Population frozen at", text(e.PopulationFrozenAt))
	field("Seed commitment", text(e.SeedCommitment))
	field("Selection seed", text(firstNonEmpty(e.Seed, "Revealed once every quarter is drawn")))
	field("Coverage basis", wrap(firstNonEmpty(e.CoverageBasis, CoverageAttestation)+": "+coverageMethod(e.CoverageBasis, b.hasCycle())))
	field("Population source", text(e.PopulationSource))
	field("Test types", text(strings.Join(e.TestTypes, ", ")))

	section("Parameters")
	fte := field("Tester FTE", xlsx.Number(pr.TesterFTE).With(xlsx.Left))
	days := field("Productive days per FTE per quarter", xlsx.Number(pr.DaysPerFTEQuarter).With(xlsx.Left))
	b.param["capacity"] = field("Capacity per quarter (person-days)", xlsx.FormulaNumber(fte+"*"+days, pr.TesterFTE*pr.DaysPerFTEQuarter).With(xlsx.Left))
	b.param["threshold"] = field("Utilisation threshold", percent(pr.UtilisationThreshold))
	b.param["amber"] = field("RAG amber band (percentage points)", xlsx.Number(pr.RAGAmberPoints).With(xlsx.Left))
	b.param["partial"] = field("A test is partially effective from (fails)", xlsx.Int(pr.ResultPartialMin).With(xlsx.Left))
	b.param["ineffective"] = field("A test is ineffective from (fails)", xlsx.Int(pr.ResultIneffectiveMin).With(xlsx.Left))
	b.param["extendAt"] = field("A deviation extends the sample from (fails)", xlsx.Int(pr.DeviationExtendAt).With(xlsx.Left))
	b.param["thematicAt"] = field("A deviation is thematic from (fails)", xlsx.Int(pr.DeviationThematicAt).With(xlsx.Left))
	b.param["extendPct"] = field("Extension (% of the next quarter's tests)", xlsx.Number(pr.ExtendPct).With(xlsx.Left))
	b.param["sla"] = field("Evidence SLA (business days)", xlsx.Int(pr.EvidenceSLADays).With(xlsx.Left))

	section("Headline figures")
	b.headline(field)

	section("Regulatory basis")
	header(s, row, 0, wbCol{"Requirement", 0}, wbCol{"Reference", 0}, wbCol{"How the plan meets it", 0})
	row++
	for _, r := range e.RegulatoryBasis {
		s.SetRow(row, wrap(r.Requirement), wrap(r.Reference), wrap(r.How))
		row++
	}

	section("Non-working dates besides weekends")
	first := row
	for _, d := range e.NonWorkingDates {
		if _, ok := dateValue(d); ok {
			s.Set(2, row, xlsx.Date(d))
			row++
		}
	}
	if row > first {
		b.holidays = xlsx.SheetRef(wbPlan, xlsx.AbsRange(2, first, 2, row-1))
	} else {
		s.Set(2, row, xlsx.Text("None").With(xlsx.Muted))
	}
	s.FitPageWidth()
	s.Protect()
}

// headline writes the plan's headline figures, each a formula over the
// working sheets and stored with what those sheets hold.
func (b *planBook) headline(field func(string, xlsx.Cell) string) {
	e := b.e
	selected, fieldwork, signed, late, pending, fails := 0, 0, 0, 0, 0, 0
	for _, it := range e.Population {
		if it.SelectedQuarter >= 1 && it.SelectedQuarter <= 4 {
			selected++
		}
	}
	for _, t := range e.Tests {
		if _, ok := dateValue(t.FieldworkCompletedOn); ok {
			fieldwork++
		}
		if _, ok := dateValue(t.QADate); ok {
			signed++
		}
		if t.EvidenceLate {
			late++
		}
		pending += t.Pending
	}
	for _, row := range b.resultRows() {
		if row.result == ResultFail {
			fails++
		}
	}
	notEffective, extend := 0, 0
	for _, d := range e.Deviations {
		switch d.Kind {
		case KindNotEffective:
			notEffective++
		case KindExtend:
			extend++
		}
	}
	raised, open, overdue := 0, 0, 0
	for _, f := range e.Findings {
		if f.Missing {
			continue
		}
		raised++
		if f.Status != auditfinding.StatusDraft && f.Status != auditfinding.StatusClosed && f.Status != auditfinding.StatusRiskAccepted {
			open++
			if f.Overdue {
				overdue++
			}
		}
	}
	peak := 0.0
	for _, q := range e.Quarters {
		if q.Quarter >= 1 && q.Quarter <= 4 {
			peak = max(peak, b.utilisation(q.Quarter))
		}
	}
	planned := 0
	for _, st := range e.Strata {
		planned += st.TotalTests
	}
	count := func(expr string, n int) xlsx.Cell { return xlsx.FormulaNumber(expr, float64(n)).With(xlsx.Left) }
	notClosed := whole(wbFindings, fdStatus)
	open1 := whole(wbFindings, fdMissing) + `,"no",` + notClosed + `,"<>` + auditfinding.StatusDraft + `",` + notClosed + `,"<>` +
		auditfinding.StatusClosed + `",` + notClosed + `,"<>` + auditfinding.StatusRiskAccepted + `"`

	field("Population size", count("COUNTA("+whole(wbPopulation, poRef)+")-1", len(e.Population)))
	field("Tests planned for the year", count("SUM("+span(wbStrata, stTotal, 2, b.strataLast)+")", planned))
	field("Items selected", count("COUNTIF("+whole(wbPopulation, poSelectedQ)+`,">0")`, selected))
	field("Tests opened", count("COUNTA("+whole(wbTests, tsRef)+")-1", len(e.Tests)))
	field("Tests with fieldwork complete", count("COUNT("+whole(wbTests, tsFieldwork)+")", fieldwork))
	field("Tests signed off by QA", count("COUNT("+whole(wbTests, tsQADate)+")", signed))
	field("Tests with late evidence", count("COUNTIF("+whole(wbTests, tsLate)+`,"yes")`, late))
	field("Attribute results pending", count("SUM("+whole(wbTests, tsPending)+")", pending))
	field("Attribute fails", count("COUNTIF("+whole(wbResults, rsResult)+","+lit(ResultFail)+")", fails))
	field("Deviations that extend the sample", count("COUNTIF("+whole(wbDeviations, dvKind)+","+lit(KindExtend)+")", extend))
	field("Attributes not effective (thematic)", count("COUNTIF("+whole(wbDeviations, dvKind)+","+lit(KindNotEffective)+")", notEffective))
	field("Findings raised", count("COUNTIF("+whole(wbFindings, fdMissing)+`,"no")`, raised))
	field("Findings open", count("COUNTIFS("+open1+")", open))
	field("Findings open and overdue", count("COUNTIFS("+open1+","+whole(wbFindings, fdOverdue)+`,"yes")`, overdue))
	if len(b.quarterRow) > 0 {
		first, last := 1<<30, 0
		for _, r := range b.quarterRow {
			first, last = min(first, r), max(last, r)
		}
		field("Peak quarterly utilisation", xlsx.FormulaNumber("MAX("+span(wbQuarters, qtUtil, first, last)+")", peak).With(xlsx.Percent))
	}
}

// utilisation is a quarter's effort over capacity, as the Quarters sheet
// works it out.
func (b *planBook) utilisation(q int) float64 {
	load := b.e.Capacity.Quarters[q-1]
	capacity := b.e.Parameters.TesterFTE * b.e.Parameters.DaysPerFTEQuarter
	if capacity <= 0 {
		return 0
	}
	return load.Effort / capacity
}

func (b *planBook) schedule(s *xlsx.Sheet) {
	header(s, 1, 3, wbCol{"Step", 8}, wbCol{"Phase", 16}, wbCol{"Title", 44}, wbCol{"Type", 11}, wbCol{"Responsible", 22},
		wbCol{"Accountable", 22}, wbCol{"Start", 11}, wbCol{"End", 11}, wbCol{"Status", 13}, wbCol{"Quarter", 8},
		wbCol{"Deliverable", 36}, wbCol{"References", 30})
	row := 2
	for _, st := range b.e.Steps {
		s.SetRow(row, text(st.StepRef), text(st.Phase), text(st.Title), text(st.StepType), text(st.ResponsibleRole),
			text(st.AccountableRole), xlsx.Date(st.StartDate), xlsx.Date(st.EndDate), xlsx.Text(st.Status).With(xlsx.Input),
			qCell(st.Quarter), text(st.Deliverable), text(st.Refs))
		row++
	}
	last := 1 + max(1, len(b.e.Steps))
	s.ListValidation(xlsx.Range(9, 2, 9, last), StepStatuses...)
	s.Highlight(xlsx.Range(1, 2, 12, last), "AND(ISNUMBER("+here(8, 2)+"),"+here(8, 2)+"<TODAY(),"+here(9, 2)+"<>"+lit(StepComplete)+")")
	filter(s, 12, len(b.e.Steps))
}

func (b *planBook) quarters(s *xlsx.Sheet) {
	header(s, 1, 1, wbCol{"Quarter", 8}, wbCol{"Quarter end", 11}, wbCol{"1LoD window opens", 12}, wbCol{"1LoD deadline", 12},
		wbCol{"Selection", 11}, wbCol{"Fieldwork starts", 12}, wbCol{"Fieldwork ends", 12}, wbCol{"Findings agreed by", 12},
		wbCol{"Report", 11}, wbCol{"Committee", 20}, wbCol{"Planned tests", 9}, wbCol{"Reviews due", 9},
		wbCol{"Test effort (days)", 10}, wbCol{"Review effort (days)", 10}, wbCol{"Retest days", 9}, wbCol{"Overhead days", 9},
		wbCol{"Effort (days)", 10}, wbCol{"Capacity (days)", 10}, wbCol{"Utilisation", 10}, wbCol{"Over threshold", 10},
		wbCol{"Items drawn", 9}, wbCol{"Drawn at", 21})
	pr := b.e.Parameters
	row := 2
	for _, q := range b.e.Quarters {
		s.SetRow(row, qCell(q.Quarter), xlsx.Date(q.QuarterEnd), xlsx.Date(q.LOD1WindowStart), xlsx.Date(q.LOD1Deadline),
			xlsx.Date(q.SelectionDate), xlsx.Date(q.FieldworkStart), xlsx.Date(q.FieldworkEnd), xlsx.Date(q.FindingsAgreedBy),
			xlsx.Date(q.ReportDate), text(q.CommitteeLabel))
		s.Set(qtRetest, row, xlsx.Number(q.RetestDays).With(xlsx.Decimal))
		s.Set(qtOverhead, row, xlsx.Number(q.OverheadDays).With(xlsx.Decimal))
		s.Set(qtPicks, row, xlsx.Int(q.Picks))
		s.Set(qtSelectedAt, row, text(q.SelectedAt))
		if q.Quarter >= 1 && q.Quarter <= 4 {
			load := b.e.Capacity.Quarters[q.Quarter-1]
			tests := span(wbStrata, stTests1+q.Quarter-1, 2, b.strataLast)
			reviews := span(wbStrata, stReviews1+q.Quarter-1, 2, b.strataLast)
			capacity := pr.TesterFTE * pr.DaysPerFTEQuarter
			util := b.utilisation(q.Quarter)
			over := load.Effort > 0
			if capacity > 0 {
				over = util > pr.UtilisationThreshold
			}
			s.Set(qtPlanned, row, xlsx.FormulaNumber("SUM("+tests+")", float64(q.PlannedTests)))
			s.Set(qtReviews, row, xlsx.FormulaNumber("SUM("+reviews+")", float64(q.ReviewsDue)))
			s.Set(qtTestEffort, row, xlsx.FormulaNumber("SUMPRODUCT("+tests+","+span(wbStrata, stEffortTest, 2, b.strataLast)+")", load.TestEffort).With(xlsx.Decimal))
			s.Set(qtReviewEffort, row, xlsx.FormulaNumber("SUMPRODUCT("+reviews+","+span(wbStrata, stEffortReview, 2, b.strataLast)+")", load.ReviewEffort).With(xlsx.Decimal))
			s.Set(qtEffort, row, xlsx.FormulaNumber(here(qtTestEffort, row)+"+"+here(qtReviewEffort, row)+"+"+here(qtRetest, row)+"+"+here(qtOverhead, row), load.Effort).With(xlsx.Decimal))
			s.Set(qtCapacity, row, xlsx.FormulaNumber(b.param["capacity"], capacity).With(xlsx.Decimal))
			s.Set(qtUtil, row, xlsx.FormulaNumber("IF("+here(qtCapacity, row)+">0,"+here(qtEffort, row)+"/"+here(qtCapacity, row)+",0)", util).With(xlsx.Percent))
			s.Set(qtOver, row, xlsx.FormulaText("IF("+here(qtCapacity, row)+">0,IF("+here(qtUtil, row)+">"+b.param["threshold"]+`,"yes","no"),IF(`+here(qtEffort, row)+`>0,"yes","no"))`, yesNo(over)))
		}
		row++
	}
	s.Highlight(xlsx.Range(qtOver, 2, qtOver, 1+max(1, len(b.e.Quarters))), here(qtOver, 2)+`="yes"`)
	filter(s, qtSelectedAt, len(b.e.Quarters))
	s.Protect()
}

func (b *planBook) strata(s *xlsx.Sheet) {
	cols := []wbCol{{"Code", 8}, {"Name", 22}, {"Definition", 40}, {"Sampling", 9}, {"Confidence", 10}, {"Tolerable rate", 10},
		{"Coverage floor", 10}, {"Effort per test (days)", 10}, {"Effort per review (days)", 10}, {"Cycle (years)", 8},
		{"Takes", 18}, {"N", 7}, {"N from", 12}, {"In cycle", 8}, {"n0", 7}, {"With FPC", 8}, {"Floor", 7}, {"Annual sample", 9}}
	for q := 1; q <= 4; q++ {
		cols = append(cols, wbCol{"Wave Q" + strconv.Itoa(q), 8})
	}
	for q := 1; q <= 4; q++ {
		cols = append(cols, wbCol{"Top-up Q" + strconv.Itoa(q), 8})
	}
	for q := 1; q <= 4; q++ {
		cols = append(cols, wbCol{"Tests Q" + strconv.Itoa(q), 8})
	}
	cols = append(cols, wbCol{"Total tests", 8}, wbCol{"Tests over N", 9})
	for q := 1; q <= 4; q++ {
		cols = append(cols, wbCol{"Reviews due Q" + strconv.Itoa(q), 9})
	}
	for q := 1; q <= 4; q++ {
		cols = append(cols, wbCol{"Cumulative target Q" + strconv.Itoa(q), 10})
	}
	cols = append(cols, wbCol{"Error", 30})
	header(s, 1, 2, cols...)
	row := 2
	for _, st := range b.e.Strata {
		s.SetRow(row, text(st.Code), text(st.Name), text(st.Definition), text(yesNo(st.SamplingEnabled)), percent(st.Confidence),
			percent(st.TolerableRate), percent(st.FloorPct), xlsx.Number(st.EffortPerTest).With(xlsx.Decimal),
			xlsx.Number(st.EffortPerReview).With(xlsx.Decimal), xlsx.Int(max(1, st.CycleYears)), text(strings.Join(st.Takes, ", ")),
			xlsx.Int(st.N), text(st.NSource), xlsx.Int(st.InCycle), xlsx.Int(st.N0), xlsx.Int(st.FPC), xlsx.Int(st.Floor),
			xlsx.Int(st.AnnualSample))
		for q := range 4 {
			s.Set(stWave1+q, row, percent(st.Waves[q]))
			s.Set(stTopUp1+q, row, xlsx.Int(st.TopUps[q]))
			s.Set(stTests1+q, row, xlsx.Int(st.Tests[q]))
			s.Set(stReviews1+q, row, xlsx.Int(st.ReviewsDue[q]))
			s.Set(stTarget1+q, row, percent(st.CumulativeTarget[q]/100))
		}
		s.Set(stTotal, row, xlsx.FormulaNumber("SUM("+xlsx.Range(stTests1, row, stTests1+3, row)+")", float64(st.TotalTests)))
		s.Set(stCoverage, row, percent(st.CoveragePct/100))
		s.Set(stError, row, text(st.Error))
		row++
	}
	filter(s, stError, len(b.e.Strata))
	s.Protect()
}

func (b *planBook) script(s *xlsx.Sheet) {
	header(s, 1, 2, wbCol{"Attribute", 10}, wbCol{"Domain", 18}, wbCol{"Requirement", 40}, wbCol{"Procedure", 44},
		wbCol{"Method", 13}, wbCol{"Evidence requested", 36}, wbCol{"Pass criteria", 36}, wbCol{"Applies to", 12},
		wbCol{"Regulatory references", 24}, wbCol{"Controls", 16}, wbCol{"NFR", 12}, wbCol{"Unresolved references", 18})
	row := 2
	for _, a := range b.e.Attributes {
		applies := strings.Join(a.AppliesTo, ", ")
		if applies == "" {
			applies = "All strata"
		}
		s.SetRow(row, wrap(a.AttrRef), wrap(a.Domain), wrap(a.Requirement), wrap(a.Procedure), wrap(a.Method),
			wrap(a.EvidenceRequested), wrap(a.PassCriteria), wrap(applies), wrap(a.RegulatoryRefs),
			wrap(strings.Join(a.ControlIDs, ", ")), wrap(a.NFRKey), wrap(strings.Join(a.Unresolved, ", ")))
		row++
	}
	filter(s, 12, len(b.e.Attributes))
	s.Protect()
}

// itemCoverage is one item's coverage as the Population sheet works it out:
// the date it counts from on the plan's basis, and whether it is covered on a
// date (inside its multi-year cycle, or covered by then).
type itemCoverage struct {
	signedOff, coveredOn, cycleDue string
}

func (c itemCoverage) at(date string) bool {
	if _, ok := dateValue(c.cycleDue); ok && date < c.cycleDue {
		return true
	}
	_, ok := dateValue(c.coveredOn)
	return ok && c.coveredOn <= date
}

func (b *planBook) itemCoverages() []itemCoverage {
	signed := map[string]string{}
	for _, t := range b.e.Tests {
		if _, ok := dateValue(t.QADate); ok && (signed[t.ItemRef] == "" || t.QADate < signed[t.ItemRef]) {
			signed[t.ItemRef] = t.QADate
		}
	}
	out := make([]itemCoverage, len(b.e.Population))
	for i, it := range b.e.Population {
		c := itemCoverage{signedOff: signed[it.ItemRef], coveredOn: it.AttestedOn, cycleDue: it.CycleDue}
		if b.e.CoverageBasis == CoverageTests {
			c.coveredOn = c.signedOff
		}
		if _, ok := dateValue(c.coveredOn); !ok {
			c.coveredOn = ""
		}
		out[i] = c
	}
	return out
}

func (b *planBook) population(s *xlsx.Sheet) {
	cols := []wbCol{{"Item", 12}, {"Name", 30}, {"Stratum", 8}, {"CIF", 6}, {"Business line", 16}, {"Hosting", 12},
		{"Internet facing", 9}, {"Owner", 20}, {"Wave", 7}, {"Attested on", 11}, {"DQ status", 16}, {"DQ reviewed on", 11},
		{"Top-up quarter", 8}, {"Top-up rationale", 28}, {"Last tested on", 11}, {"Cycle due", 11}, {"Source", 14},
		{"Source ref", 14}, {"Selected in", 8}, {"Selection basis", 16}, {"Rank", 6}, {"Test", 13}, {"Test result", 16},
		{"Signed off on", 11}, {"Covered on", 11}, {"Covered today", 9}}
	for q := 1; q <= 4; q++ {
		cols = append(cols, wbCol{"Covered at Q" + strconv.Itoa(q) + " end", 9})
	}
	header(s, 1, 2, cols...)
	overall := map[string]string{}
	for _, t := range b.e.Tests {
		overall[t.TestRef] = t.Overall
	}
	covers := b.itemCoverages()
	coveredOnSource := poAttested
	if b.e.CoverageBasis == CoverageTests {
		coveredOnSource = poSignedOff
	}
	row := 2
	for i, it := range b.e.Population {
		rank := xlsx.Cell{}
		if it.RankOrder > 0 {
			rank = xlsx.Int(it.RankOrder)
		}
		s.SetRow(row, text(it.ItemRef), text(it.Name), text(it.Stratum), text(it.CIF), text(it.BusinessLine), text(it.Hosting),
			text(yesNo(it.InternetFacing)), text(it.Owner), qCell(it.Wave), xlsx.Date(it.AttestedOn),
			xlsx.Text(it.DQStatus).With(xlsx.Input), xlsx.Date(it.DQReviewedOn), qCell(it.TopUpQuarter), text(it.TopUpRationale),
			xlsx.Date(it.LastTestedOn), xlsx.Date(it.CycleDue), text(it.SourceKind), text(it.SourceRef), qCell(it.SelectedQuarter),
			text(it.SelectionBasis), rank, text(it.TestRef))
		testRef := here(poTestRef, row)
		result := ""
		if it.TestRef != "" {
			result = overall[it.TestRef]
		}
		s.Set(poOverall, row, xlsx.FormulaText("IF("+testRef+`="","",IFERROR(INDEX(`+whole(wbTests, tsOverall)+",MATCH("+testRef+","+whole(wbTests, tsRef)+`,0)),""))`, result))
		minifs := "_xlfn.MINIFS(" + whole(wbTests, tsQADate) + "," + whole(wbTests, tsItemRef) + "," + here(poRef, row) + ")"
		c := covers[i]
		s.Set(poSignedOff, row, dateFormula("IF("+minifs+`=0,"",`+minifs+")", c.signedOff))
		src := here(coveredOnSource, row)
		s.Set(poCoveredOn, row, dateFormula("IF(ISNUMBER("+src+"),"+src+`,"")`, c.coveredOn))
		coveredBy := func(date, iso string) xlsx.Cell {
			due, on := here(poCycleDue, row), here(poCoveredOn, row)
			expr := "IF(IF(ISNUMBER(" + due + ")," + date + "<" + due + ",FALSE),1,IF(ISNUMBER(" + on + "),IF(" + on + "<=" + date + ",1,0),0))"
			v := 0.0
			if c.at(iso) {
				v = 1
			}
			return xlsx.FormulaNumber(expr, v).With(xlsx.Center)
		}
		s.Set(poCoveredNow, row, coveredBy("TODAY()", b.today))
		for q := 1; q <= 4; q++ {
			if end, ok := b.quarterEnd[q]; ok {
				s.Set(poCovered1+q-1, row, coveredBy(b.quarterEndRef(q), end))
			}
		}
		row++
	}
	last := 1 + max(1, len(b.e.Population))
	s.ListValidation(xlsx.Range(poDQ, 2, poDQ, last), DQStatuses...)
	filter(s, poCovered1+3, len(b.e.Population))
}

func (b *planBook) tests(s *xlsx.Sheet) {
	header(s, 1, 1, wbCol{"Test", 13}, wbCol{"Type", 14}, wbCol{"Quarter", 8}, wbCol{"Item", 12}, wbCol{"Item name", 26},
		wbCol{"Stratum", 8}, wbCol{"Basis", 16}, wbCol{"Tester", 14}, wbCol{"Evidence requested", 11}, wbCol{"Evidence due", 11},
		wbCol{"Evidence received", 11}, wbCol{"Evidence late", 8}, wbCol{"Fieldwork complete", 11}, wbCol{"QA reviewer", 14},
		wbCol{"QA signed off", 11}, wbCol{"Pass", 6}, wbCol{"Fail", 6}, wbCol{"N/A", 6}, wbCol{"Pending", 7},
		wbCol{"Overall", 17}, wbCol{"Findings", 22}, wbCol{"Evidence summary", 40}, wbCol{"Notes", 30}, wbCol{"QA note", 30})
	results := whole(wbResults, rsResult)
	row := 2
	for _, t := range b.e.Tests {
		var findings []string
		for _, r := range t.Results {
			if r.FindingRef != "" {
				findings = append(findings, r.FindingRef+" ("+r.Severity+")")
			}
		}
		s.SetRow(row, text(t.TestRef), text(t.TestType), qCell(t.Quarter), text(t.ItemRef), text(t.ItemName), text(t.Stratum),
			text(t.Basis), text(t.Tester), xlsx.Date(t.EvidenceRequestedOn))
		requested, due, received := here(tsRequested, row), here(tsDue, row), here(tsReceived, row)
		workday := "WORKDAY(" + requested + "," + b.param["sla"]
		if b.holidays != "" {
			workday += "," + b.holidays
		}
		s.Set(tsDue, row, dateFormula("IF("+requested+`="","",`+workday+"))", t.EvidenceDueOn))
		s.Set(tsReceived, row, xlsx.Date(t.EvidenceReceivedOn))
		s.Set(tsLate, row, xlsx.FormulaText("IF("+due+`="","no",IF(`+received+`<>"",IF(`+received+">"+due+`,"yes","no"),IF(TODAY()>`+due+`,"yes","no")))`, yesNo(t.EvidenceLate)))
		s.Set(tsFieldwork, row, xlsx.Date(t.FieldworkCompletedOn))
		s.Set(tsQAReviewer, row, text(t.QAReviewer))
		s.Set(tsQADate, row, xlsx.Date(t.QADate))
		count := func(result string) string {
			return "COUNTIFS(" + whole(wbResults, rsTest) + "," + here(tsRef, row) + "," + results + "," + lit(result) + ")"
		}
		pass, fail, na := here(tsPass, row), here(tsFail, row), here(tsNA, row)
		s.Set(tsPass, row, xlsx.FormulaNumber(count(ResultPass), float64(t.Passes)))
		s.Set(tsFail, row, xlsx.FormulaNumber(count(ResultFail), float64(t.Fails)))
		s.Set(tsNA, row, xlsx.FormulaNumber(count(ResultNA), float64(t.NotApplicable)))
		s.Set(tsPending, row, xlsx.FormulaNumber("COUNTIF("+whole(wbResults, rsTest)+","+here(tsRef, row)+")-"+pass+"-"+fail+"-"+na, float64(t.Pending)))
		s.Set(tsOverall, row, xlsx.FormulaText("IF("+pass+"+"+fail+"=0,"+lit(OverallNotStarted)+",IF("+fail+">="+b.param["ineffective"]+","+
			lit(OverallIneffective)+",IF("+fail+">="+b.param["partial"]+","+lit(OverallPartial)+","+lit(OverallEffective)+")))", t.Overall))
		s.Set(tsFindings, row, text(strings.Join(findings, "; ")))
		s.Set(tsSummary, row, text(t.EvidenceSummary))
		s.Set(tsNotes, row, text(t.Notes))
		s.Set(tsQANote, row, text(t.QANote))
		row++
	}
	last := 1 + max(1, len(b.e.Tests))
	if listable(b.e.TestTypes) {
		s.ListValidation(xlsx.Range(tsType, 2, tsType, last), b.e.TestTypes...)
	}
	s.Highlight(xlsx.Range(tsLate, 2, tsLate, last), here(tsLate, 2)+`="yes"`)
	s.Highlight(xlsx.Range(tsOverall, 2, tsOverall, last), "OR("+here(tsOverall, 2)+"="+lit(OverallPartial)+","+here(tsOverall, 2)+"="+lit(OverallIneffective)+")")
	filter(s, tsQANote, len(b.e.Tests))
}

type resultRow struct {
	test                  ExportTest
	attr                  ExportAttribute
	result, note, finding string
	severity              string
}

// resultRows is one row per test and attribute that applies to the test's
// stratum, as the plan page counts them: a result not yet given is pending.
func (b *planBook) resultRows() []resultRow {
	var out []resultRow
	for _, t := range b.e.Tests {
		byAttr := map[string]ExportResult{}
		for _, r := range t.Results {
			byAttr[r.AttrRef] = r
		}
		for _, a := range b.e.Attributes {
			if len(a.AppliesTo) > 0 && !slices.Contains(a.AppliesTo, t.Stratum) {
				continue
			}
			r := byAttr[a.AttrRef]
			out = append(out, resultRow{test: t, attr: a, result: r.Result, note: r.Note, finding: r.FindingRef, severity: r.Severity})
		}
	}
	return out
}

func (b *planBook) results(s *xlsx.Sheet) {
	header(s, 1, 1, wbCol{"Test", 13}, wbCol{"Quarter", 8}, wbCol{"Item", 12}, wbCol{"Item name", 26}, wbCol{"Stratum", 8},
		wbCol{"Attribute", 10}, wbCol{"Domain", 18}, wbCol{"Requirement", 44}, wbCol{"Result", 9}, wbCol{"Note", 40},
		wbCol{"Finding", 14}, wbCol{"Severity", 10})
	rows := b.resultRows()
	row := 2
	for _, r := range rows {
		s.SetRow(row, text(r.test.TestRef), qCell(r.test.Quarter), text(r.test.ItemRef), text(r.test.ItemName), text(r.test.Stratum),
			text(r.attr.AttrRef), text(r.attr.Domain), text(r.attr.Requirement), xlsx.Text(r.result).With(xlsx.Input), text(r.note),
			text(r.finding), text(r.severity))
		row++
	}
	last := 1 + max(1, len(rows))
	s.ListValidation(xlsx.Range(rsResult, 2, rsResult, last), Results...)
	s.Highlight(xlsx.Range(1, 2, rsSeverity, last), here(rsResult, 2)+"="+lit(ResultFail))
	filter(s, rsSeverity, len(rows))
}

func (b *planBook) deviations(s *xlsx.Sheet) {
	header(s, 1, 3, wbCol{"Attribute", 10}, wbCol{"Domain", 18}, wbCol{"Stratum", 8}, wbCol{"Sampling", 9}, wbCol{"Tested", 7},
		wbCol{"Deviations", 9}, wbCol{"Rule", 12}, wbCol{"Conclusion", 36}, wbCol{"Failing tests (at export)", 36},
		wbCol{"Extend in", 8}, wbCol{"Extend by", 8}, wbCol{"Risk", 14})
	sampling := map[string]bool{}
	for _, st := range b.e.Strata {
		sampling[st.Code] = st.SamplingEnabled
	}
	pr := b.param
	row := 2
	for _, d := range b.e.Deviations {
		attr, stratum := here(dvAttr, row), here(dvStratum, row)
		match := whole(wbResults, rsAttr) + "," + attr + "," + whole(wbResults, rsStratum) + "," + stratum + "," + whole(wbResults, rsResult) + ","
		fails := "COUNTIFS(" + match + lit(ResultFail) + ")"
		tested, failsCell, kind, sampled := here(dvTested, row), here(dvFails, row), here(dvKind, row), here(dvSampling, row)
		lastQ := "MAX(_xlfn.MAXIFS(" + whole(wbResults, rsQuarter) + "," + match + lit(ResultPass) + "),_xlfn.MAXIFS(" + whole(wbResults, rsQuarter) + "," + match + lit(ResultFail) + "))"
		extendQ := here(dvExtendQ, row)
		next := "INDEX(" + xlsx.SheetRef(wbStrata, xlsx.AbsRange(stTests1, 2, stTests1+3, b.strataLast)) + ",MATCH(" + stratum + "," +
			span(wbStrata, stCode, 2, b.strataLast) + ",0)," + extendQ + ")"

		s.SetRow(row, text(d.AttrRef), text(d.Domain), text(d.Stratum), text(yesNo(sampling[d.Stratum])))
		s.Set(dvTested, row, xlsx.FormulaNumber("COUNTIFS("+match+lit(ResultPass)+")+"+fails, float64(d.Tested)))
		s.Set(dvFails, row, xlsx.FormulaNumber(fails, float64(d.Deviations)))
		s.Set(dvKind, row, xlsx.FormulaText("IF("+tested+"=0,"+lit(KindNotTested)+",IF("+failsCell+">="+pr["thematicAt"]+","+lit(KindNotEffective)+
			",IF(AND("+failsCell+">="+pr["extendAt"]+","+failsCell+">0),"+lit(KindExtend)+","+lit(KindEffective)+")))", d.Kind))
		s.Set(dvConclusion, row, xlsx.FormulaText("IF("+kind+"="+lit(KindNotTested)+","+lit(ConclusionNotTested)+",IF("+kind+"="+lit(KindNotEffective)+","+
			lit(ConclusionNotEffective)+",IF("+kind+"="+lit(KindExtend)+","+failsCell+`&IF(`+failsCell+`=1," deviation"," deviations")&IF(`+sampled+
			`="no"," – full coverage, raise as findings"," – extend sample"),`+lit(ConclusionEffective)+")))", d.Conclusion))
		s.Set(dvFailures, row, text(strings.Join(d.Failures, "; ")))
		exprQ := "IF(AND(" + kind + "=" + lit(KindExtend) + "," + sampled + `="yes"),IF(` + lastQ + "<4," + lastQ + `+1,""),"")`
		exprBy := "IF(" + extendQ + `="","",IF(OR(` + next + "<=0," + pr["extendPct"] + "<=0),0,CEILING(ROUND(" + next + "*" + pr["extendPct"] + "/100,6),1)))"
		if d.ExtendQuarter >= 1 && d.ExtendQuarter <= 4 {
			s.Set(dvExtendQ, row, xlsx.FormulaNumber(exprQ, float64(d.ExtendQuarter)).With(xlsx.Quarter))
			s.Set(dvExtendBy, row, xlsx.FormulaNumber(exprBy, float64(d.ExtendBy)))
		} else {
			s.Set(dvExtendQ, row, xlsx.FormulaText(exprQ, "").With(xlsx.Quarter))
			s.Set(dvExtendBy, row, xlsx.FormulaText(exprBy, ""))
		}
		s.Set(dvRisk, row, text(d.RiskRef))
		row++
	}
	last := 1 + max(1, len(b.e.Deviations))
	s.Highlight(xlsx.Range(1, 2, dvRisk, last), "OR("+here(dvKind, 2)+"="+lit(KindExtend)+","+here(dvKind, 2)+"="+lit(KindNotEffective)+")")
	filter(s, dvRisk, len(b.e.Deviations))
	s.Protect()
}

func (b *planBook) coverage(s *xlsx.Sheet) {
	cols := []wbCol{{"Stratum", 8}, {"Name", 22}, {"N", 7}, {"Attested", 8}, {"Attested %", 9}, {"Tested", 7}, {"Covered", 8},
		{"Covered %", 9}, {"DQ accepted", 9}, {"DQ accepted %", 9}, {"In cycle", 8}}
	for q := 1; q <= 4; q++ {
		n := strconv.Itoa(q)
		cols = append(cols, wbCol{"Q" + n + " target", 9}, wbCol{"Q" + n + " covered", 9}, wbCol{"Q" + n + " actual", 9}, wbCol{"Q" + n + " RAG", 9})
	}
	header(s, 1, 2, cols...)
	stratum := whole(wbPopulation, poStratum)
	amber := b.param["amber"]
	row := 2
	for _, c := range b.e.Coverage {
		code, n := here(cvCode, row), here(cvN, row)
		of := func(col int, criterion string) string {
			return "COUNTIFS(" + stratum + "," + code + "," + whole(wbPopulation, col) + "," + criterion + ")"
		}
		share := func(col int) string { return "IF(" + n + ">0," + here(col, row) + "/" + n + ",0)" }
		s.SetRow(row, text(c.Code), text(c.Name))
		s.Set(cvN, row, xlsx.FormulaNumber("COUNTIF("+stratum+","+code+")", float64(c.N)))
		s.Set(cvAttested, row, xlsx.FormulaNumber(of(poAttested, `">0"`), float64(c.Attested)))
		s.Set(cvAttestedPct, row, xlsx.FormulaNumber(share(cvAttested), ratio(c.Attested, c.N)).With(xlsx.Percent))
		s.Set(cvTested, row, xlsx.FormulaNumber(of(poSignedOff, `">0"`), float64(c.Tested)))
		s.Set(cvCovered, row, xlsx.FormulaNumber(of(poCoveredNow, "1"), float64(c.Covered)))
		s.Set(cvCoveredPct, row, xlsx.FormulaNumber(share(cvCovered), ratio(c.Covered, c.N)).With(xlsx.Percent))
		s.Set(cvDQ, row, xlsx.FormulaNumber(of(poDQ, lit(DQAccepted)), float64(c.DQAccepted)))
		s.Set(cvDQPct, row, xlsx.FormulaNumber(share(cvDQ), ratio(c.DQAccepted, c.N)).With(xlsx.Percent))
		s.Set(cvInCycle, row, xlsx.FormulaNumber(of(poCycleDue, `">"&TODAY()`), float64(c.InCycle)))
		for q := 1; q <= 4; q++ {
			if _, ok := b.quarterEnd[q]; !ok {
				continue
			}
			cell := c.Cells[q-1]
			base := cvQ1 + 4*(q-1)
			target, actual := here(base, row), here(base+2, row)
			gap := "ROUND((" + target + "-" + actual + ")*100,6)"
			s.Set(base, row, percent(cell.PlannedPct/100))
			s.Set(base+1, row, xlsx.FormulaNumber(of(poCovered1+q-1, "1"), float64(cell.Covered)))
			s.Set(base+2, row, xlsx.FormulaNumber(share(base+1), ratio(cell.Covered, c.N)).With(xlsx.Percent))
			s.Set(base+3, row, xlsx.FormulaText("IF(OR(TODAY()<="+b.quarterEndRef(q)+","+n+`=0),"Not due",IF(`+gap+`<=0,"Green",IF(`+gap+"<="+amber+`,"Amber","Red")))`, cell.RAG))
		}
		row++
	}
	last := 1 + max(1, len(b.e.Coverage))
	for q := range 4 {
		col := cvQ1 + 4*q + 3
		s.Highlight(xlsx.Range(col, 2, col, last), xlsx.Ref(col, 2)+`="Red"`)
	}
	filter(s, cvQ1+15, len(b.e.Coverage))
	s.Protect()
}

func (b *planBook) findings(s *xlsx.Sheet) {
	header(s, 1, 1, wbCol{"Finding", 14}, wbCol{"Title", 44}, wbCol{"Severity", 10}, wbCol{"Status", 16}, wbCol{"Due", 11},
		wbCol{"Overdue", 8}, wbCol{"Missing", 8}, wbCol{"Test", 13}, wbCol{"Attribute", 10}, wbCol{"Item", 12})
	row := 2
	for _, f := range b.e.Findings {
		s.SetRow(row, text(f.Reference), text(f.Title), text(f.Severity), text(f.Status), xlsx.Date(f.DueDate),
			text(yesNo(f.Overdue)), text(yesNo(f.Missing)), text(f.TestRef), text(f.AttrRef), text(f.ItemRef))
		row++
	}
	filter(s, fdItem, len(b.e.Findings))
	s.Protect()
}

func (b *planBook) governance(s *xlsx.Sheet) {
	g := b.e.Governance
	widths := []float64{52, 22, 44, 26}
	for i := 3; i < len(g.RACIRoles); i++ {
		widths = append(widths, 14)
	}
	s.Widths(widths...)
	row := 1
	section := func(title string) {
		s.Set(1, row, xlsx.Text(title).With(xlsx.Title))
		row++
	}
	section("RACI")
	cols := []wbCol{{"Activity", 0}}
	for _, r := range g.RACIRoles {
		cols = append(cols, wbCol{r, 0})
	}
	header(s, row, 0, cols...)
	row++
	for _, r := range g.RACI {
		s.Set(1, row, text(r.Activity))
		for i, role := range g.RACIRoles {
			s.Set(i+2, row, xlsx.Text(r.Cells[role]).With(xlsx.Center))
		}
		row++
	}
	row++
	section("Reporting cadence")
	header(s, row, 0, wbCol{"Forum", 0}, wbCol{"Frequency", 0}, wbCol{"Content", 0}, wbCol{"Owner", 0})
	row++
	for _, c := range g.Cadence {
		s.SetRow(row, wrap(c.Forum), wrap(c.Frequency), wrap(c.Content), wrap(c.Owner))
		row++
	}
	row++
	section("Escalations")
	header(s, row, 0, wbCol{"Trigger", 0}, wbCol{"Escalate to", 0}, wbCol{"Within", 0})
	row++
	for _, x := range g.Escalations {
		s.SetRow(row, wrap(x.Trigger), wrap(x.EscalateTo), wrap(x.Within))
		row++
	}
	s.FitPageWidth()
	s.Protect()
}

func (b *planBook) history(s *xlsx.Sheet) {
	s.Widths(12, 24, 24, 70, 22, 66)
	row := 1
	s.Set(1, row, xlsx.Text("Approved versions").With(xlsx.Title))
	row++
	header(s, row, 0, wbCol{"Version", 0}, wbCol{"Label", 0}, wbCol{"Approved by", 0}, wbCol{"Change reason", 0},
		wbCol{"Approved at", 0}, wbCol{"Snapshot SHA-256", 0})
	row++
	for _, v := range b.e.Versions {
		s.SetRow(row, wrap(strconv.Itoa(v.Number)), wrap(v.Label), wrap(v.ApprovedBy), wrap(v.ChangeReason), wrap(v.ApprovedAt), wrap(v.SnapshotSHA256))
		row++
	}
	if len(b.e.Versions) == 0 {
		s.Set(1, row, xlsx.Text("No version approved yet.").With(xlsx.Muted))
		row++
	}
	row++
	s.Set(1, row, xlsx.Text("Approval gate").With(xlsx.Title))
	row++
	header(s, row, 0, wbCol{"Severity", 0}, wbCol{"Check", 0}, wbCol{"Subject", 0}, wbCol{"Message", 0})
	row++
	for _, f := range b.e.GateFindings {
		s.SetRow(row, wrap(f.Severity), wrap(f.Code), wrap(f.Subject), wrap(f.Message))
		row++
	}
	if len(b.e.GateFindings) == 0 {
		s.Set(1, row, xlsx.Text("The plan passes every check.").With(xlsx.Muted))
	}
	s.FitPageWidth()
	s.Protect()
}
