package regcoverage

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"grc/internal/reporting"
)

// Renderer converts a complete HTML document into PDF bytes. It is the
// reporting module's interface, taken as a dependency rather than imported as
// a concrete type so this module shares the one pooled headless browser the
// app already starts, instead of running a second.
type Renderer interface {
	Render(ctx context.Context, html []byte, opts reporting.RenderOptions) ([]byte, error)
}

// WithRenderer attaches the PDF renderer. Without one, the report is still
// available as a page — a deployment with no Chrome loses the download, not
// the feature.
func (s *Service) WithRenderer(r Renderer) *Service {
	s.renderer = r
	return s
}

// PDFAvailable reports whether the PDF download can be offered.
func (s *Service) PDFAvailable() bool { return s.renderer != nil }

// pdfTimeout bounds one render. A long regulation is a long document, and the
// engine paginates the whole thing before it returns anything.
const pdfTimeout = 120 * time.Second

// RenderPDF produces the report as a PDF. Version 0 means the latest.
func (s *Service) RenderPDF(ctx context.Context, id int64, version int) ([]byte, string, error) {
	if s.renderer == nil {
		return nil, "", invalid("PDF rendering is not available on this deployment")
	}
	report, err := s.Report(id, version)
	if err != nil {
		return nil, "", err
	}

	pdf, err := s.renderer.Render(ctx, []byte(ReportHTML(report)), reporting.RenderOptions{
		PaperWidthInches:   8.27,
		PaperHeightInches:  11.69,
		MarginTopInches:    0.6,
		MarginBottomInches: 0.6,
		MarginLeftInches:   0.55,
		MarginRightInches:  0.55,
		PrintBackground:    true,
		Timeout:            pdfTimeout,
	})
	if err != nil {
		return nil, "", fmt.Errorf("render report PDF: %w", err)
	}
	return pdf, pdfFilename(report), nil
}

func pdfFilename(report *Report) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return r
		case r == ' ', r == '.', r == '/':
			return '-'
		default:
			return -1
		}
	}, report.Regulation.Title)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "regulation"
	}
	return fmt.Sprintf("%s-coverage-v%d.pdf", name, report.Version.Number)
}

// ReportHTML renders the standalone report document — the thing that becomes
// the PDF. It carries its own styles and no scripts, because the PDF engine
// runs with JavaScript disabled and the document has to stand on its own once
// it leaves this application.
func ReportHTML(report *Report) string {
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>` + esc(report.Regulation.Title) + ` — Coverage Report</title>
<style>
  @page { size: A4 portrait; }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    padding: 28px 32px 40px;
    font: 11pt/1.5 Georgia, "Times New Roman", serif;
    color: #1c2431;
    background: #ffffff;
  }
  h1 { font-size: 22pt; margin: 0 0 4px; line-height: 1.15; }
  h2 { font-size: 14pt; margin: 26px 0 8px; padding-bottom: 4px; border-bottom: 1px solid #d7cebf; }
  h3 { font-size: 11.5pt; margin: 18px 0 4px; }
  p { margin: 0 0 8px; }
  .subtitle { color: #5e6672; font-size: 10pt; margin: 0 0 4px; }
  .meta { color: #5e6672; font-size: 9pt; font-family: Arial, sans-serif; }
  .stats { display: flex; flex-wrap: wrap; gap: 8px; margin: 14px 0 4px; }
  .stat {
    border: 1px solid #d7cebf; border-radius: 8px; padding: 7px 12px;
    font-family: Arial, sans-serif; min-width: 110px; background: #faf7f1;
  }
  .stat .n { display: block; font-size: 16pt; font-weight: 700; }
  .stat .l { font-size: 8pt; text-transform: uppercase; letter-spacing: 0.06em; color: #5e6672; }
  .summary { white-space: pre-wrap; }
  .section { page-break-inside: avoid; margin: 0 0 14px; padding: 10px 12px; border: 1px solid #e4dccd; border-radius: 8px; }
  .section.quiet { border-style: dashed; color: #5e6672; padding: 6px 12px; }
  .section h3 { margin-top: 0; }
  .label { font-family: Arial, sans-serif; font-size: 8.5pt; text-transform: uppercase;
           letter-spacing: 0.07em; color: #5e6672; margin: 8px 0 2px; }
  table { width: 100%; border-collapse: collapse; margin: 4px 0 2px; font-size: 9.5pt; }
  th, td { text-align: left; vertical-align: top; padding: 4px 6px; border-bottom: 1px solid #ece4d6; }
  th { font-family: Arial, sans-serif; font-size: 8pt; text-transform: uppercase;
       letter-spacing: 0.06em; color: #5e6672; }
  td.ref { font-family: "Courier New", monospace; white-space: nowrap; }
  .tag { display: inline-block; font-family: Arial, sans-serif; font-size: 7.5pt;
         text-transform: uppercase; letter-spacing: 0.05em; padding: 1px 6px;
         border: 1px solid #d7cebf; border-radius: 999px; }
  .tag.low { border-color: #d7b271; color: #8a5b00; }
  .tag.warn { border-color: #b3564a; color: #8b2f22; }
  .tag.seed { border-color: #7fae94; color: #0b5d3b; }
  .quote { font-style: italic; color: #3d4653; border-left: 3px solid #d7cebf; padding-left: 8px; margin: 4px 0; }
  .refs { font-family: "Courier New", monospace; font-size: 9pt; line-height: 1.7; }
  .footnote { color: #5e6672; font-size: 8.5pt; font-family: Arial, sans-serif; margin-top: 20px;
              border-top: 1px solid #d7cebf; padding-top: 8px; }
</style>
</head>
<body>
`)
	b.WriteString(reportBodyHTML(report, false))
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// reportBodyHTML is the report itself, shared by the PDF document and the
// in-app page so the two cannot drift. inApp switches on the bits that only
// make sense with the application around them.
func reportBodyHTML(report *Report, inApp bool) string {
	var b strings.Builder
	reg := report.Regulation
	stats := report.Stats

	b.WriteString(`<header>`)
	b.WriteString("<h1>" + esc(reg.Title) + "</h1>\n")
	b.WriteString(`<p class="subtitle">Regulation coverage against the Security NFR catalog and NIST SP 800-53</p>`)

	frameworkLine := reg.FrameworkName
	if reg.SourceRef != "" {
		frameworkLine += " · " + reg.SourceRef
	}
	if !reg.Detected {
		frameworkLine += " · generic segmentation (no framework profile matched this document)"
	}
	b.WriteString(`<p class="meta">` + esc(frameworkLine) + "</p>\n")
	b.WriteString(`<p class="meta">Version ` + fmt.Sprint(report.Version.Number))
	if report.Version.CreatedAt != "" {
		b.WriteString(" · " + esc(report.Version.CreatedAt))
	}
	if report.Version.CreatedBy != "" {
		b.WriteString(" · " + esc(report.Version.CreatedBy))
	}
	if report.Version.Note != "" {
		b.WriteString(" · " + esc(report.Version.Note))
	}
	b.WriteString("</p>\n</header>\n")

	b.WriteString(`<div class="stats">`)
	stat := func(n int, label string) {
		b.WriteString(fmt.Sprintf(`<div class="stat"><span class="n">%d</span><span class="l">%s</span></div>`, n, esc(label)))
	}
	stat(stats.Sections, "sections")
	stat(stats.Relevant, "security-relevant")
	stat(stats.Mapped, "mapped")
	stat(stats.Unmapped, "unmapped")
	b.WriteString(fmt.Sprintf(`<div class="stat"><span class="n">%d%%</span><span class="l">coverage</span></div>`,
		stats.CoveragePercent()))
	stat(stats.DistinctControls, "800-53 controls")
	stat(stats.DistinctNFRs, "security NFRs")
	b.WriteString("</div>\n")

	if stats.LowConfidence > 0 || stats.Ungrounded > 0 || stats.UnknownRefs > 0 {
		var caveats []string
		if stats.LowConfidence > 0 {
			caveats = append(caveats, fmt.Sprintf("%d section(s) analysed with low confidence", stats.LowConfidence))
		}
		if stats.Ungrounded > 0 {
			caveats = append(caveats, fmt.Sprintf("%d finding(s) could not be traced to a verbatim quote", stats.Ungrounded))
		}
		if stats.UnknownRefs > 0 {
			caveats = append(caveats, fmt.Sprintf("%d reference(s) are not in the catalog", stats.UnknownRefs))
		}
		b.WriteString(`<p class="meta">Review first: ` + esc(strings.Join(caveats, "; ")) + ".</p>\n")
	}

	if report.Version.Summary != "" {
		b.WriteString("<h2>Executive summary</h2>\n")
		b.WriteString(`<div class="summary">` + esc(report.Version.Summary) + "</div>\n")
	}

	b.WriteString("<h2>Section analysis</h2>\n")
	for _, entry := range report.Sections {
		b.WriteString(sectionHTML(entry, inApp))
	}

	controls := report.SortedRefs(KindControl)
	nfrs := report.SortedRefs(KindNFR)
	if len(controls) > 0 || len(nfrs) > 0 {
		b.WriteString("<h2>Catalog items referenced</h2>\n")
		if len(controls) > 0 {
			b.WriteString(`<p class="label">NIST SP 800-53</p>`)
			b.WriteString(`<p class="refs">` + esc(strings.Join(controls, "  ")) + "</p>\n")
		}
		if len(nfrs) > 0 {
			b.WriteString(`<p class="label">Security NFRs</p>`)
			b.WriteString(`<p class="refs">` + esc(strings.Join(nfrs, "  ")) + "</p>\n")
		}
	}

	b.WriteString(`<p class="footnote">`)
	b.WriteString("Every finding in this report was produced by a language model from the section text and a retrieved shortlist of catalog items, and carries the model name and the hash of the prompt that produced it. ")
	b.WriteString("It is analysis to be reviewed, not a compliance determination. ")
	// Which extractor read the document belongs in the footnote of a report
	// somebody circulates: a mapping built on an OCR'd scan deserves to be read
	// differently from one built on a PDF's own text layer.
	b.WriteString(esc(fmt.Sprintf(
		"Source document: %s (%s, %s), held in the AI agent's library. Text read by %s.",
		reg.Filename, reg.MediaType, humanBytes(reg.ByteSize), reg.ExtractMethod)))
	b.WriteString("</p>\n")
	return b.String()
}

func sectionHTML(entry Entry, inApp bool) string {
	var b strings.Builder
	sec := entry.Section
	heading := strings.TrimSpace(sec.Label + " " + sec.Title)
	if heading == "" {
		heading = sec.Ref
	}

	// A section the analysis found irrelevant is kept on one line: dropping it
	// would leave the reader unable to tell "not applicable" from "not looked
	// at", and expanding it would bury the sections that matter.
	if entry.Finding != nil && !entry.Finding.Relevant {
		return `<div class="section quiet"><strong>` + esc(heading) + `</strong> — ` +
			`<span class="meta">no security obligation` + confidenceTag(entry.Finding) + `</span></div>` + "\n"
	}

	b.WriteString(`<div class="section" id="section-` + esc(sec.Ref) + `">`)
	b.WriteString("<h3>" + esc(heading) + "</h3>\n")

	meta := []string{sec.Ref}
	if sec.Category != "" {
		meta = append(meta, sec.Category)
	}
	if entry.Finding != nil {
		meta = append(meta, entry.Finding.Confidence+" confidence")
		if entry.Finding.Revision > 0 {
			meta = append(meta, fmt.Sprintf("revision %d", entry.Finding.Revision))
		}
	}
	b.WriteString(`<p class="meta">` + esc(strings.Join(meta, " · ")))
	if entry.Finding != nil && !entry.Finding.Grounded {
		b.WriteString(` <span class="tag warn">unverified quote</span>`)
	}
	b.WriteString("</p>\n")

	if entry.Finding == nil {
		b.WriteString(`<p class="meta">Not analysed.</p></div>` + "\n")
		return b.String()
	}
	f := entry.Finding

	if f.Requirement != "" {
		b.WriteString("<p>" + esc(f.Requirement) + "</p>\n")
	}
	if f.Quote != "" {
		b.WriteString(`<p class="quote">“` + esc(f.Quote) + `”</p>` + "\n")
	}

	b.WriteString(`<p class="label">Mapped to</p>`)
	if len(f.Mappings) == 0 {
		b.WriteString(`<p class="meta">Nothing in the catalog covers this section.</p>` + "\n")
	} else {
		b.WriteString("<table><thead><tr><th>Ref</th><th>Catalog item</th><th>Why</th><th>Confidence</th></tr></thead><tbody>\n")
		for _, m := range f.Mappings {
			b.WriteString("<tr><td class=\"ref\">" + esc(m.Ref))
			if m.Source == SourceSeed {
				b.WriteString(` <span class="tag seed">curated</span>`)
			}
			if !m.Known {
				b.WriteString(` <span class="tag warn">unknown</span>`)
			}
			b.WriteString("</td><td>" + esc(mappingTitle(m)) + "</td><td>" + esc(m.Rationale) +
				"</td><td>" + esc(m.Confidence) + "</td></tr>\n")
		}
		b.WriteString("</tbody></table>\n")
	}

	if f.Gaps != "" {
		b.WriteString(`<p class="label">Gap</p><p>` + esc(f.Gaps) + "</p>\n")
	}
	if f.Commentary != "" {
		b.WriteString(`<p class="label">Commentary</p><p>` + esc(f.Commentary) + "</p>\n")
	}

	provenance := f.Model
	if f.PromptHash != "" {
		provenance += " · prompt " + shortHash(f.PromptHash)
	}
	b.WriteString(`<p class="meta">` + esc(provenance) + "</p>\n")

	if inApp {
		b.WriteString(`<button type="button" class="revise-btn" data-ref="` + esc(sec.Ref) + `">Revise this section</button>` + "\n")
	}
	b.WriteString("</div>\n")
	return b.String()
}

func mappingTitle(m Mapping) string {
	if m.Title != "" {
		return m.Title
	}
	if m.Kind == KindNFR {
		return "(not in the Security NFR catalog)"
	}
	return "(not in the 800-53 catalog)"
}

func confidenceTag(f *Finding) string {
	if f == nil || f.Confidence != ConfidenceLow {
		return ""
	}
	return ` <span class="tag low">low confidence</span>`
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// esc is html.EscapeString under a shorter name; every value below comes from
// an upload or a model, so nothing reaches the page unescaped.
func esc(s string) string { return html.EscapeString(s) }
