package reporting

import "html/template"

// ControlAssessmentReport is the second example report type. It documents the
// result of assessing a set of security controls against a framework (e.g.
// NIST SP 800-53), and exercises the same layout features as the risk report:
// summary + charts, a long paginating results table, and keep-together
// deficiency blocks.
type ControlAssessmentReport struct {
	Framework      string
	System         string
	Scope          string
	AssessmentDate string
	Assessor       string
	Narrative      string
	Summary        ControlAssessmentSummary
	Results        []ControlResult
	Deficiencies   []Deficiency
	ResultChart    template.HTML // server-rendered inline SVG
	FamilyChart    template.HTML // server-rendered inline SVG
}

// ControlAssessmentSummary aggregates the assessment for the executive block.
type ControlAssessmentSummary struct {
	TotalControls      int
	Satisfied          int
	PartiallySatisfied int
	NotSatisfied       int
	NotApplicable      int
	Deficiencies       int
}

// ImplementationRate returns the percentage of assessed (non-N/A) controls that
// are fully satisfied, rounded to a whole number.
func (s ControlAssessmentSummary) ImplementationRate() int {
	assessed := s.TotalControls - s.NotApplicable
	if assessed <= 0 {
		return 0
	}
	return int(float64(s.Satisfied)/float64(assessed)*100 + 0.5)
}

// ControlResult is one row in the control assessment results table.
type ControlResult struct {
	ControlID            string
	Name                 string
	Family               string
	ImplementationStatus string // Implemented / Planned / Inherited / N/A
	Result               AssessmentResult
	Method               string // Examine / Interview / Test (NIST SP 800-53A)
}

// Deficiency is an identified control weakness, rendered as a keep-together
// block (the assessment equivalent of a POA&M line item).
type Deficiency struct {
	ControlID   string
	Weakness    string
	Severity    Severity
	Remediation string
	Owner       string
	Milestone   string
}

// AssessmentResult is the outcome of assessing a single control.
type AssessmentResult string

const (
	ResultSatisfied          AssessmentResult = "Satisfied"
	ResultPartiallySatisfied AssessmentResult = "Partially Satisfied"
	ResultNotSatisfied       AssessmentResult = "Not Satisfied"
	ResultNotApplicable      AssessmentResult = "Not Applicable"
)

// Color returns a hex fill used for result badges and chart segments.
func (r AssessmentResult) Color() string {
	switch r {
	case ResultSatisfied:
		return "#2f7d4f"
	case ResultPartiallySatisfied:
		return "#c08a1e"
	case ResultNotSatisfied:
		return "#7a1f1f"
	case ResultNotApplicable:
		return "#5a6473"
	default:
		return "#5a544c"
	}
}

// ResultsTable renders the assessment results as a long, paginating table.
func (r ControlAssessmentReport) ResultsTable() Table {
	t := Table{
		Headers: []TableHeader{
			{Text: "Control"},
			{Text: "Name"},
			{Text: "Family"},
			{Text: "Implementation"},
			{Text: "Method"},
			{Text: "Result"},
		},
		EmptyText: "No controls were assessed.",
	}
	for _, res := range r.Results {
		t.Rows = append(t.Rows, TableRow{Cells: []TableCell{
			monoCell(res.ControlID),
			cell(res.Name),
			cell(res.Family),
			cell(res.ImplementationStatus),
			cell(res.Method),
			badgeCell(string(res.Result), res.Result.Color()),
		}})
	}
	return t
}
