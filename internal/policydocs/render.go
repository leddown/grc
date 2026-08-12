package policydocs

import (
	"html"
	"strconv"
	"strings"
)

// RenderMarkdown produces the document control block followed by the sections.
// This is both the export format and the approval snapshot, deliberately: the
// snapshot has to be a self-contained record of what was approved, which means
// it must carry the control metadata (owner, approver, dates, classification)
// and not just the prose.
func RenderMarkdown(doc Document, sections []Section, refs []ControlRef) string {
	var b strings.Builder

	b.WriteString("# " + doc.Title + "\n\n")
	if doc.Summary != "" {
		b.WriteString(doc.Summary + "\n\n")
	}

	b.WriteString("## Document Control\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	for _, row := range controlRows(doc) {
		b.WriteString("| " + row[0] + " | " + escapePipes(row[1]) + " |\n")
	}
	b.WriteString("\n")

	bySection := refsBySection(refs)
	for _, s := range sections {
		b.WriteString("## " + s.Heading + "\n\n")
		body := strings.TrimSpace(s.Body)
		if body == "" {
			body = "_No content._"
		}
		b.WriteString(body + "\n\n")
		if mapped := bySection[s.ID]; len(mapped) > 0 {
			b.WriteString("_Satisfies: " + strings.Join(refLabels(mapped), "; ") + "_\n\n")
		}
	}

	if len(refs) > 0 {
		b.WriteString("## Control Mapping\n\n")
		b.WriteString("| Control | Name | Coverage | Section |\n|---|---|---|---|\n")
		for _, ref := range refs {
			name := ref.ControlName
			if !ref.Known {
				name = "(not in catalog)"
			}
			b.WriteString("| " + escapePipes(ref.ControlID) + " | " + escapePipes(name) +
				" | " + ref.Coverage + " | " + escapePipes(ref.SectionHeading) + " |\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

func refsBySection(refs []ControlRef) map[int64][]ControlRef {
	out := make(map[int64][]ControlRef, len(refs))
	for _, ref := range refs {
		out[ref.SectionID] = append(out[ref.SectionID], ref)
	}
	return out
}

// refLabels renders the inline "Satisfies:" list. Partial and supporting claims
// are labelled; an unqualified id means the section satisfies the control on
// its own, which is the reading an assessor will take.
func refLabels(refs []ControlRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		label := ref.ControlID
		if ref.Coverage != CoverageFull {
			label += " (" + ref.Coverage + ")"
		}
		out = append(out, label)
	}
	return out
}

// RenderHTML produces a standalone, printable page. Section bodies are emitted
// preformatted rather than parsed as Markdown — the real rendering pipeline is
// the template module (phase 5), and a partial Markdown parser here would be
// thrown away when it lands.
func RenderHTML(doc Document, sections []Section, refs []ControlRef) string {
	var b strings.Builder

	b.WriteString(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + html.EscapeString(doc.Title) + ` · CareLock Consulting</title>
  <style>
    :root { color-scheme: light; --ink:#1c2431; --muted:#5e6672; --line:#d7cebf; --accent:#8b3d2e; }
    * { box-sizing: border-box; }
    body { margin:0; font-family: Georgia, "Times New Roman", serif; color:var(--ink);
           background:linear-gradient(135deg,#f7f3eb,#ece4d6 55%,#e4d8c4); }
    main { max-width:900px; margin:24px auto; padding:32px; background:rgba(255,252,246,0.96);
           border:1px solid rgba(215,206,191,0.8); border-radius:20px; }
    h1 { margin:0 0 6px; font-size:2rem; line-height:1.15; }
    h2 { font-family:Arial,sans-serif; font-size:13px; text-transform:uppercase;
         letter-spacing:0.08em; color:var(--muted); margin:26px 0 8px; }
    .summary { color:var(--muted); margin:0 0 20px; }
    table.control { width:100%; border-collapse:collapse; font-family:Arial,sans-serif; font-size:13px; }
    table.control th, table.control td { border:1px solid var(--line); padding:6px 10px; text-align:left; vertical-align:top; }
    table.control th { background:#efe6d6; width:34%; font-weight:600; }
    .body { white-space:pre-wrap; line-height:1.55; }
    .empty { color:var(--muted); font-style:italic; }
    .satisfies { color:var(--muted); font-family:Arial,sans-serif; font-size:12px;
                 margin:6px 0 0; font-style:italic; }
    .badge { display:inline-block; padding:2px 10px; border-radius:999px; background:#ece3d2;
             border:1px solid var(--line); font-family:Arial,sans-serif; font-size:11px;
             text-transform:uppercase; letter-spacing:0.06em; }
    @media print {
      body { background:#fff; }
      main { margin:0; border:0; border-radius:0; max-width:none; padding:0; }
    }
  </style>
</head>
<body>
  <main>
`)

	b.WriteString("    <h1>" + html.EscapeString(doc.Title) + "</h1>\n")
	b.WriteString(`    <p><span class="badge">` + html.EscapeString(statusLabel(doc.Status)) + `</span></p>` + "\n")
	if doc.Summary != "" {
		b.WriteString(`    <p class="summary">` + html.EscapeString(doc.Summary) + "</p>\n")
	}

	b.WriteString("    <h2>Document Control</h2>\n    <table class=\"control\">\n")
	for _, row := range controlRows(doc) {
		b.WriteString("      <tr><th>" + html.EscapeString(row[0]) + "</th><td>" +
			html.EscapeString(row[1]) + "</td></tr>\n")
	}
	b.WriteString("    </table>\n")

	bySection := refsBySection(refs)
	for _, s := range sections {
		b.WriteString("    <h2>" + html.EscapeString(s.Heading) + "</h2>\n")
		body := strings.TrimSpace(s.Body)
		if body == "" {
			b.WriteString(`    <p class="empty">No content.</p>` + "\n")
		} else {
			b.WriteString(`    <div class="body">` + html.EscapeString(body) + "</div>\n")
		}
		if mapped := bySection[s.ID]; len(mapped) > 0 {
			b.WriteString(`    <p class="satisfies">Satisfies: ` +
				html.EscapeString(strings.Join(refLabels(mapped), "; ")) + "</p>\n")
		}
	}

	if len(refs) > 0 {
		b.WriteString("    <h2>Control Mapping</h2>\n    <table class=\"control\">\n")
		b.WriteString("      <tr><th>Control</th><th>Name</th><th>Coverage</th><th>Section</th></tr>\n")
		for _, ref := range refs {
			name := ref.ControlName
			if !ref.Known {
				name = "(not in catalog)"
			}
			b.WriteString("      <tr><td>" + html.EscapeString(ref.ControlID) +
				"</td><td>" + html.EscapeString(name) +
				"</td><td>" + html.EscapeString(ref.Coverage) +
				"</td><td>" + html.EscapeString(ref.SectionHeading) + "</td></tr>\n")
		}
		b.WriteString("    </table>\n")
	}

	b.WriteString("  </main>\n</body>\n</html>\n")
	return b.String()
}

// controlRows is the shared document-control block, so the Markdown export, the
// HTML export and the approval snapshot can never disagree about what metadata
// a document carries.
func controlRows(doc Document) [][2]string {
	frameworks := strings.Join(doc.Frameworks, ", ")
	if frameworks == "" {
		frameworks = "None declared"
	}
	rows := [][2]string{
		{"Reference", orNone(doc.Reference)},
		{"Document Type", docTypeLabel(doc.DocType)},
		{"Status", statusLabel(doc.Status)},
		{"Classification", orNone(doc.Classification)},
		{"Owner Role", orNone(doc.OwnerRole)},
		{"Approver", orNone(doc.Approver)},
		{"Client", orNone(doc.ClientName)},
		{"Frameworks", frameworks},
		{"Effective Date", orNone(doc.EffectiveDate)},
		{"Review Cadence", strconv.Itoa(doc.ReviewCadenceMonths) + " months"},
		{"Next Review", orNone(doc.NextReviewDate)},
	}
	if doc.ParentTitle != "" {
		rows = append(rows, [2]string{"Parent Document", doc.ParentTitle})
	}
	if doc.LatestLabel != "" {
		rows = append(rows, [2]string{"Latest Approved Version", doc.LatestLabel})
	}
	return rows
}

func statusLabel(status string) string {
	switch status {
	case StatusInReview:
		return "In Review"
	case StatusDraft:
		return "Draft"
	case StatusApproved:
		return "Approved"
	case StatusRetired:
		return "Retired"
	default:
		return status
	}
}

func orNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Not set"
	}
	return value
}

// escapePipes keeps a value containing "|" from breaking the Markdown table it
// is rendered into.
func escapePipes(value string) string {
	return strings.ReplaceAll(value, "|", `\|`)
}
