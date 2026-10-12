package reporting

import (
	"html/template"
	"time"
)

// Document is the root value passed to every report template. It carries the
// shared chrome (branding, classification, cover, watermark) plus the report
// specific payload in Data. Templates reach report data via {{ .Data }}.
//
// All string fields are rendered through html/template, so any user-supplied
// content (risk titles, finding descriptions, owner names, etc.) is auto
// escaped. Do not bypass this with template.HTML for untrusted input — that
// type is reserved for server-generated markup such as inline SVG charts.
type Document struct {
	Branding       Branding
	Classification Classification
	Watermark      string
	Cover          Cover
	GeneratedAt    time.Time
	// Data is the report-specific model (e.g. RiskAssessmentReport).
	Data any
}

// RiskAssessmentReport is the example report payload. It is intentionally rich
// enough to exercise every layout feature: a summary with charts, a long
// paginating risk register table, a control matrix, and keep-together findings.
type RiskAssessmentReport struct {
	Scope           string
	Period          string
	Methodology     string
	Summary         RiskSummary
	Risks           []RiskRow
	Controls        []ControlRow
	Findings        []Finding
	SeverityChart   template.HTML // server-rendered inline SVG
	TreatmentChart  template.HTML // server-rendered inline SVG
	ExecutiveReview string
}

// RiskSummary aggregates the risk register for the executive summary block.
type RiskSummary struct {
	TotalRisks    int
	Critical      int
	High          int
	Medium        int
	Low           int
	AcceptedRisks int
	OpenFindings  int
}

// RiskRow is a single line in the risk register table.
type RiskRow struct {
	ID         string
	Title      string
	Category   string
	Owner      string
	Likelihood int
	Impact     int
	Severity   Severity
	Treatment  string // Mitigate / Accept / Transfer / Avoid
	Status     string
	TargetDate string
}

// Score is the inherent/residual risk score (likelihood * impact).
func (r RiskRow) Score() int { return r.Likelihood * r.Impact }

// ControlRow is a single line in the control matrix table.
type ControlRow struct {
	ID            string
	Name          string
	Family        string
	Status        string // Implemented / Partial / Planned / Not Implemented
	Effectiveness string // Effective / Needs Improvement / Ineffective
	Owner         string
}

// Finding is an audit/assessment finding rendered as a keep-together block.
type Finding struct {
	ID             string
	Title          string
	Severity       Severity
	Description    string
	Recommendation string
	Owner          string
	DueDate        string
}

// Severity is a shared ordinal severity used for color coding across the report.
type Severity string

const (
	SeverityCritical Severity = "Critical"
	SeverityHigh     Severity = "High"
	SeverityMedium   Severity = "Medium"
	SeverityLow      Severity = "Low"
	SeverityInfo     Severity = "Informational"
)

// Color returns a hex fill used for severity badges and chart segments.
func (s Severity) Color() string {
	switch s {
	case SeverityCritical:
		return "#7a1f1f"
	case SeverityHigh:
		return "#b3431f"
	case SeverityMedium:
		return "#c08a1e"
	case SeverityLow:
		return "#2f7d4f"
	case SeverityInfo:
		return "#3a6b8a"
	default:
		return "#5a544c"
	}
}

// SeverityForScore maps a 1-25 risk score onto a Severity band.
func SeverityForScore(score int) Severity {
	switch {
	case score >= 20:
		return SeverityCritical
	case score >= 12:
		return SeverityHigh
	case score >= 6:
		return SeverityMedium
	default:
		return SeverityLow
	}
}
