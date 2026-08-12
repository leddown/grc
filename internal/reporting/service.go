package reporting

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Content template file names, one per report type.
const (
	riskAssessmentTemplate    = "risk_assessment.tmpl"
	controlAssessmentTemplate = "control_assessment.tmpl"
)

// Service ties templating (html/template) to a Renderer to produce finished
// PDF reports. It is the clean entry point the rest of the app calls.
type Service struct {
	renderer Renderer
	logger   *slog.Logger
}

// NewService builds a reporting Service. If logger is nil, slog.Default() is
// used. The renderer is required (see NewChromeRenderer).
func NewService(renderer Renderer, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{renderer: renderer, logger: logger}
}

// RenderRiskAssessment renders the Risk Assessment Report to PDF bytes. This is
// the template every new report type follows: build a Document, render HTML,
// render the running header/footer, then hand both to the Renderer.
func (s *Service) RenderRiskAssessment(ctx context.Context, report RiskAssessmentReport, opts ReportOptions) ([]byte, error) {
	return s.render(ctx, "risk_assessment", riskAssessmentTemplate, report.withDerived(), opts)
}

// RiskAssessmentHTML returns the rendered HTML (no PDF). Useful for golden-file
// tests, previews, and debugging without a browser dependency.
func (s *Service) RiskAssessmentHTML(report RiskAssessmentReport, opts ReportOptions) ([]byte, error) {
	opts.applyDefaults()
	doc := buildDocument(report.withDerived(), opts)
	return renderHTML(riskAssessmentTemplate, doc)
}

// RenderControlAssessment renders the Control Assessment Report to PDF bytes.
func (s *Service) RenderControlAssessment(ctx context.Context, report ControlAssessmentReport, opts ReportOptions) ([]byte, error) {
	return s.render(ctx, "control_assessment", controlAssessmentTemplate, report.withDerived(), opts)
}

// ControlAssessmentHTML returns the rendered HTML (no PDF) for the Control
// Assessment Report — useful for golden-file tests and previews.
func (s *Service) ControlAssessmentHTML(report ControlAssessmentReport, opts ReportOptions) ([]byte, error) {
	opts.applyDefaults()
	doc := buildDocument(report.withDerived(), opts)
	return renderHTML(controlAssessmentTemplate, doc)
}

// render is the shared HTML->PDF pipeline used by every report type.
func (s *Service) render(ctx context.Context, reportType, templateFile string, data any, opts ReportOptions) ([]byte, error) {
	start := time.Now()
	opts.applyDefaults()
	doc := buildDocument(data, opts)

	html, err := renderHTML(templateFile, doc)
	if err != nil {
		return nil, fmt.Errorf("reporting: build html for %s: %w", reportType, err)
	}
	header, footer, err := renderHeaderFooter(templateFile, doc)
	if err != nil {
		return nil, fmt.Errorf("reporting: build header/footer for %s: %w", reportType, err)
	}

	pdf, err := s.renderer.Render(ctx, html, opts.renderOptions(header, footer))
	if err != nil {
		return nil, fmt.Errorf("reporting: render %s: %w", reportType, err)
	}

	s.logger.Info("reporting.generated",
		slog.String("report_type", reportType),
		slog.String("classification", string(opts.Classification)),
		slog.Bool("watermarked", opts.Watermark != ""),
		slog.Int("html_bytes", len(html)),
		slog.Int("pdf_bytes", len(pdf)),
		slog.Duration("elapsed", time.Since(start)),
	)
	return pdf, nil
}

// buildDocument assembles the root template value from options and report data.
func buildDocument(data any, opts ReportOptions) Document {
	return Document{
		Branding:       opts.Branding,
		Classification: opts.Classification,
		Watermark:      opts.Watermark,
		Cover:          opts.Cover,
		GeneratedAt:    opts.GeneratedAt,
		Data:           data,
	}
}

// renderOptions maps the caller-facing ReportOptions onto engine RenderOptions.
func (o ReportOptions) renderOptions(header, footer string) RenderOptions {
	return RenderOptions{
		PaperWidthInches:    o.Page.WidthInches,
		PaperHeightInches:   o.Page.HeightInches,
		MarginTopInches:     o.Page.MarginTop,
		MarginBottomInches:  o.Page.MarginBottom,
		MarginLeftInches:    o.Page.MarginLeft,
		MarginRightInches:   o.Page.MarginRight,
		Landscape:           o.Page.Landscape,
		PrintBackground:     true,
		DisplayHeaderFooter: true,
		HeaderHTML:          header,
		FooterHTML:          footer,
		EnableJavaScript:    false,
	}
}

// treatmentPalette gives each treatment strategy a stable color for charts.
var treatmentPalette = map[string]string{
	"Mitigate": "#2f7d4f",
	"Transfer": "#3a6b8a",
	"Accept":   "#c08a1e",
	"Avoid":    "#7a1f1f",
}

// treatmentOrder fixes chart ordering for reproducible output.
var treatmentOrder = []string{"Mitigate", "Transfer", "Accept", "Avoid"}

// withDerived returns a copy of the report with the summary and charts filled
// in from the underlying rows when the caller left them empty. This keeps
// fixtures and callers terse while guaranteeing the charts match the data.
func (r RiskAssessmentReport) withDerived() RiskAssessmentReport {
	out := r

	counts := map[Severity]int{}
	for _, risk := range r.Risks {
		counts[risk.Severity]++
	}

	if out.Summary.TotalRisks == 0 {
		out.Summary.TotalRisks = len(r.Risks)
		out.Summary.Critical = counts[SeverityCritical]
		out.Summary.High = counts[SeverityHigh]
		out.Summary.Medium = counts[SeverityMedium]
		out.Summary.Low = counts[SeverityLow]
	}
	if out.Summary.OpenFindings == 0 {
		out.Summary.OpenFindings = len(r.Findings)
	}

	if out.SeverityChart == "" {
		out.SeverityChart = DonutChartSVG([]ChartSegment{
			{Label: "Critical", Value: float64(counts[SeverityCritical]), Color: SeverityCritical.Color()},
			{Label: "High", Value: float64(counts[SeverityHigh]), Color: SeverityHigh.Color()},
			{Label: "Medium", Value: float64(counts[SeverityMedium]), Color: SeverityMedium.Color()},
			{Label: "Low", Value: float64(counts[SeverityLow]), Color: SeverityLow.Color()},
		}, 150)
	}

	if out.TreatmentChart == "" {
		treatmentCounts := map[string]int{}
		for _, risk := range r.Risks {
			treatmentCounts[risk.Treatment]++
		}
		segments := make([]ChartSegment, 0, len(treatmentOrder))
		for _, name := range treatmentOrder {
			color := treatmentPalette[name]
			if color == "" {
				color = "#5a544c"
			}
			segments = append(segments, ChartSegment{
				Label: name,
				Value: float64(treatmentCounts[name]),
				Color: color,
			})
		}
		out.TreatmentChart = BarChartSVG(segments, 240)
	}

	return out
}
