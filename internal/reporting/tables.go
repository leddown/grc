package reporting

import "strconv"

// Table is the view model consumed by the reusable "datatable" template
// partial. Building tables as data (rather than bespoke template markup per
// report) keeps the pagination/keep-together behaviour in one place and makes
// the row mapping unit-testable.
type Table struct {
	Caption string
	Headers []TableHeader
	Rows    []TableRow
	// EmptyText is shown when Rows is empty.
	EmptyText string
}

// TableHeader is a column header; Align is an optional CSS text-align value.
type TableHeader struct {
	Text  string
	Align string
}

// TableRow is one row of cells.
type TableRow struct {
	Cells []TableCell
}

// TableCell is a single cell. Text is always escaped by html/template. Set
// Badge to render Text as a colored pill (Color must be a hex value, supplied
// by trusted server code such as Severity.Color()).
type TableCell struct {
	Text  string
	Mono  bool
	Align string
	Badge bool
	Color string
}

func cell(text string) TableCell             { return TableCell{Text: text} }
func monoCell(text string) TableCell         { return TableCell{Text: text, Mono: true} }
func numCell(n int) TableCell                { return TableCell{Text: strconv.Itoa(n), Align: "right"} }
func badgeCell(text, color string) TableCell { return TableCell{Text: text, Badge: true, Color: color} }

// RegisterTable renders the risk register as a long, paginating table.
func (r RiskAssessmentReport) RegisterTable() Table {
	t := Table{
		Headers: []TableHeader{
			{Text: "Risk ID"},
			{Text: "Title"},
			{Text: "Category"},
			{Text: "Owner"},
			{Text: "L", Align: "right"},
			{Text: "I", Align: "right"},
			{Text: "Score", Align: "right"},
			{Text: "Severity"},
			{Text: "Treatment"},
			{Text: "Status"},
		},
		EmptyText: "No risks recorded for this assessment.",
	}
	for _, risk := range r.Risks {
		t.Rows = append(t.Rows, TableRow{Cells: []TableCell{
			monoCell(risk.ID),
			cell(risk.Title),
			cell(risk.Category),
			cell(risk.Owner),
			numCell(risk.Likelihood),
			numCell(risk.Impact),
			numCell(risk.Score()),
			badgeCell(string(risk.Severity), risk.Severity.Color()),
			cell(risk.Treatment),
			cell(risk.Status),
		}})
	}
	return t
}

// ControlMatrixTable renders the control matrix as a long, paginating table.
func (r RiskAssessmentReport) ControlMatrixTable() Table {
	t := Table{
		Headers: []TableHeader{
			{Text: "Control"},
			{Text: "Name"},
			{Text: "Family"},
			{Text: "Owner"},
			{Text: "Status"},
			{Text: "Effectiveness"},
		},
		EmptyText: "No controls assessed.",
	}
	for _, ctrl := range r.Controls {
		t.Rows = append(t.Rows, TableRow{Cells: []TableCell{
			monoCell(ctrl.ID),
			cell(ctrl.Name),
			cell(ctrl.Family),
			cell(ctrl.Owner),
			cell(ctrl.Status),
			cell(ctrl.Effectiveness),
		}})
	}
	return t
}
