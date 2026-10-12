package reporting

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

const (
	riskAssessmentGolden    = "testdata/risk_assessment.golden.html"
	controlAssessmentGolden = "testdata/control_assessment.golden.html"
)

// TestRiskAssessmentHTMLGolden renders the example report to HTML against fixed
// fixtures and compares it to a checked-in golden file. This is fast and needs
// no browser, so it runs in the normal (and security-gated) test suite.
func TestRiskAssessmentHTMLGolden(t *testing.T) {
	svc := NewService(nil, nil) // HTML path does not touch the renderer
	html, err := svc.RiskAssessmentHTML(SampleRiskAssessment(), SampleReportOptions())
	if err != nil {
		t.Fatalf("RiskAssessmentHTML: %v", err)
	}

	if *update {
		if err := os.WriteFile(riskAssessmentGolden, html, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(riskAssessmentGolden)
	if err != nil {
		t.Fatalf("read golden (run `go test -run Golden -update` to create it): %v", err)
	}
	if !bytes.Equal(html, want) {
		t.Errorf("rendered HTML differs from golden %s; run `go test -run Golden -update` if the change is intended", riskAssessmentGolden)
	}
}

// TestControlAssessmentHTMLGolden is the browser-free golden test for the
// second report type.
func TestControlAssessmentHTMLGolden(t *testing.T) {
	svc := NewService(nil, nil)
	html, err := svc.ControlAssessmentHTML(SampleControlAssessment(), SampleControlAssessmentOptions())
	if err != nil {
		t.Fatalf("ControlAssessmentHTML: %v", err)
	}

	if *update {
		if err := os.WriteFile(controlAssessmentGolden, html, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(controlAssessmentGolden)
	if err != nil {
		t.Fatalf("read golden (run `go test -run Golden -update` to create it): %v", err)
	}
	if !bytes.Equal(html, want) {
		t.Errorf("rendered HTML differs from golden %s; run `go test -run Golden -update` if the change is intended", controlAssessmentGolden)
	}
}

func TestControlAssessmentDerived(t *testing.T) {
	report := SampleControlAssessment().withDerived()
	if report.Summary.TotalControls != len(sampleControlResults()) {
		t.Errorf("TotalControls = %d, want %d", report.Summary.TotalControls, len(sampleControlResults()))
	}
	if report.Summary.Deficiencies != len(sampleDeficiencies()) {
		t.Errorf("Deficiencies = %d, want %d", report.Summary.Deficiencies, len(sampleDeficiencies()))
	}
	sum := report.Summary.Satisfied + report.Summary.PartiallySatisfied + report.Summary.NotSatisfied + report.Summary.NotApplicable
	if sum != report.Summary.TotalControls {
		t.Errorf("result breakdown %d does not sum to total %d", sum, report.Summary.TotalControls)
	}
	if rate := report.Summary.ImplementationRate(); rate < 0 || rate > 100 {
		t.Errorf("ImplementationRate out of range: %d", rate)
	}
	if report.ResultChart == "" || report.FamilyChart == "" {
		t.Error("expected derived charts to be populated")
	}
}

// TestHTMLAutoEscaping verifies untrusted report data cannot inject markup —
// the core reason the package uses html/template.
func TestHTMLAutoEscaping(t *testing.T) {
	report := RiskAssessmentReport{
		Risks: []RiskRow{{
			ID:        "RISK-XSS",
			Title:     `<script>alert('xss')</script>`,
			Owner:     `"><img src=x onerror=alert(1)>`,
			Severity:  SeverityHigh,
			Treatment: "Mitigate",
			Status:    "Open",
		}},
	}
	svc := NewService(nil, nil)
	html, err := svc.RiskAssessmentHTML(report, ReportOptions{})
	if err != nil {
		t.Fatalf("RiskAssessmentHTML: %v", err)
	}
	out := string(html)
	if strings.Contains(out, "<script>alert('xss')</script>") {
		t.Error("script payload was not escaped")
	}
	// The owner payload tries to break out of an attribute; the raw tag must
	// not survive. ("onerror=alert(1)" as inert escaped text is fine — only
	// < > & " are escaped in HTML text context, which is sufficient.)
	if strings.Contains(out, "<img src=x") {
		t.Error("attribute-breaking <img> payload was not escaped")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("expected escaped script entity in output")
	}
}

func TestWithDerivedSummaryAndCharts(t *testing.T) {
	report := SampleRiskAssessment().withDerived()
	if report.Summary.TotalRisks != len(sampleRisks()) {
		t.Errorf("TotalRisks = %d, want %d", report.Summary.TotalRisks, len(sampleRisks()))
	}
	if report.Summary.OpenFindings != len(sampleFindings()) {
		t.Errorf("OpenFindings = %d, want %d", report.Summary.OpenFindings, len(sampleFindings()))
	}
	sum := report.Summary.Critical + report.Summary.High + report.Summary.Medium + report.Summary.Low
	if sum != report.Summary.TotalRisks {
		t.Errorf("severity breakdown %d does not sum to total %d", sum, report.Summary.TotalRisks)
	}
	if report.SeverityChart == "" || report.TreatmentChart == "" {
		t.Error("expected derived charts to be populated")
	}
	if !strings.Contains(string(report.SeverityChart), "<svg") {
		t.Error("severity chart is not inline SVG")
	}
}

func TestSeverityForScore(t *testing.T) {
	cases := []struct {
		score int
		want  Severity
	}{
		{25, SeverityCritical},
		{20, SeverityCritical},
		{19, SeverityHigh},
		{12, SeverityHigh},
		{11, SeverityMedium},
		{6, SeverityMedium},
		{5, SeverityLow},
		{1, SeverityLow},
	}
	for _, c := range cases {
		if got := SeverityForScore(c.score); got != c.want {
			t.Errorf("SeverityForScore(%d) = %s, want %s", c.score, got, c.want)
		}
	}
}

func TestThemeCSSRejectsUnsafeBranding(t *testing.T) {
	// A non-hex color must be rejected in favor of the fallback.
	if got := sanitizeColor("red; } body { background: url(javascript:alert(1))", "#1f2a44"); got != "#1f2a44" {
		t.Errorf("sanitizeColor accepted unsafe value: %q", got)
	}
	// A font stack must not retain the structural tokens needed to escape the
	// value and start a function call, declaration, or selector/block.
	font := sanitizeFontStack(`Arial"; x:expression(alert(1))`, "fallback")
	for _, bad := range []string{"(", ")", ";", ":", "{", "}"} {
		if strings.Contains(font, bad) {
			t.Errorf("sanitizeFontStack retained unsafe token %q: %q", bad, font)
		}
	}
	// The assembled theme block falls back to a safe default primary color.
	css := string(Document{Branding: Branding{PrimaryColor: "not-a-color"}}.ThemeCSS())
	if !strings.Contains(css, DefaultBranding().PrimaryColor) {
		t.Errorf("expected fallback primary color in CSS: %q", css)
	}
}

func TestDonutChartSVGZeroTotal(t *testing.T) {
	svg := string(DonutChartSVG([]ChartSegment{{Label: "None", Value: 0, Color: "#000000"}}, 120))
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "n/a") {
		t.Errorf("zero-total donut should render an empty ring with n/a, got %q", svg)
	}
}

func TestDonutChartSVGSafeColor(t *testing.T) {
	// A malicious color must not break out of the SVG attribute.
	svg := string(DonutChartSVG([]ChartSegment{{Label: "Bad", Value: 1, Color: `#000" onload="alert(1)`}}, 120))
	if strings.Contains(svg, "onload") {
		t.Errorf("unsafe color leaked into SVG: %q", svg)
	}
}

func TestRenderHeaderFooter(t *testing.T) {
	opts := SampleReportOptions()
	opts.applyDefaults()
	doc := buildDocument(SampleRiskAssessment(), opts)
	header, footer, err := renderHeaderFooter(riskAssessmentTemplate, doc)
	if err != nil {
		t.Fatalf("renderHeaderFooter: %v", err)
	}
	if !strings.Contains(footer, `class="pageNumber"`) || !strings.Contains(footer, `class="totalPages"`) {
		t.Errorf("footer missing page-number placeholders: %q", footer)
	}
	if !strings.Contains(header, string(ClassificationConfidential)) {
		t.Errorf("header missing classification: %q", header)
	}
}

func TestGotenbergRendererNotImplemented(t *testing.T) {
	_, err := (&GotenbergRenderer{}).Render(nil, []byte("x"), RenderOptions{})
	if err != ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}
