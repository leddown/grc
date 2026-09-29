package doctemplate

import (
	"net/url"
	"strconv"
	"strings"
)

// The Word path: the render payload written out as Markdown for Pandoc, which
// is found at runtime like Typst and never bundled. Every string is escaped
// here, and Pandoc reads it as GitHub-flavoured Markdown with raw HTML off and
// in its sandbox (see compile), so no text in a document can become markup,
// include a file or fetch anything.

var mdEscaper = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "{", `\{`, "}", `\}`, "[", `\[`, "]", `\]`,
	"<", `\<`, ">", `\>`, "(", `\(`, ")", `\)`, "#", `\#`, "+", `\+`, "-", `\-`, ".", `\.`,
	"!", `\!`, "|", `\|`, "~", `\~`, "$", `\$`, "&", `\&`, "\n", " ",
)

func mdText(s string) string { return mdEscaper.Replace(s) }

// mdHref is a link target Pandoc can take as a URL and nothing else: the
// scheme allowlist, and the characters that could end the destination
// percent-encoded.
func mdHref(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		if u.Host == "" {
			return ""
		}
	case "mailto":
	default:
		return ""
	}
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\\", "%5C").Replace(u.String())
}

func mdRuns(runs []PolicyRun) string {
	var b strings.Builder
	for _, r := range runs {
		if r.Break {
			b.WriteString("\\\n")
			continue
		}
		t := mdText(r.Text)
		if r.Fact != nil && r.Fact.Unresolved {
			t = "**" + t + "**"
		}
		// A code mark is set as plain text: a code span cannot hold the
		// escapes that keep the text inert.
		if r.hasMark("italic") {
			t = "*" + t + "*"
		}
		if r.hasMark("bold") {
			t = "**" + t + "**"
		}
		if href := mdHref(r.Href); href != "" {
			t = "[" + t + "](<" + href + ">)"
		}
		b.WriteString(t)
	}
	return b.String()
}

func mdBlocks(b *strings.Builder, blocks []PolicyBlock, indent string) {
	for _, blk := range blocks {
		switch blk.Type {
		case "paragraph":
			b.WriteString(indent + mdRuns(blk.Runs) + "\n\n")
		case "heading":
			marks := "### "
			if blk.Level == 3 {
				marks = "#### "
			}
			b.WriteString(indent + marks + mdRuns(blk.Runs) + "\n\n")
		case "bullet_list", "ordered_list":
			for i, item := range blk.Items {
				marker := "- "
				if blk.Type == "ordered_list" {
					start := blk.Start
					if start < 1 {
						start = 1
					}
					marker = strconv.Itoa(start+i) + ". "
				}
				var inner strings.Builder
				mdBlocks(&inner, item, "")
				lines := strings.Split(strings.TrimRight(inner.String(), "\n"), "\n")
				pad := strings.Repeat(" ", len(marker))
				for j, line := range lines {
					switch {
					case j == 0:
						b.WriteString(indent + marker + line + "\n")
					case line == "":
						b.WriteString("\n")
					default:
						b.WriteString(indent + pad + line + "\n")
					}
				}
			}
			b.WriteString("\n")
		case "table":
			if len(blk.Rows) == 0 {
				continue
			}
			width := 0
			for _, r := range blk.Rows {
				if len(r.Cells) > width {
					width = len(r.Cells)
				}
			}
			row := func(r PolicyRow) string {
				cells := make([]string, width)
				for i := range cells {
					if i < len(r.Cells) {
						var parts []string
						for _, cb := range r.Cells[i] {
							parts = append(parts, mdRuns(cb.Runs))
						}
						cells[i] = strings.Join(parts, " ")
					}
				}
				return indent + "| " + strings.Join(cells, " | ") + " |\n"
			}
			rows := blk.Rows
			if rows[0].Header {
				b.WriteString(row(rows[0]))
				rows = rows[1:]
			} else {
				b.WriteString(indent + "|" + strings.Repeat("  |", width) + "\n")
			}
			b.WriteString(indent + "|" + strings.Repeat(" --- |", width) + "\n")
			for _, r := range rows {
				b.WriteString(row(r))
			}
			b.WriteString("\n")
		case "blockquote", "callout":
			var inner strings.Builder
			if blk.Type == "callout" {
				label := "Note:"
				if blk.Kind == "important" {
					label = "Important:"
				}
				inner.WriteString("**" + label + "**\n\n")
			}
			mdBlocks(&inner, blk.Blocks, "")
			for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
				b.WriteString(indent + "> " + line + "\n")
			}
			b.WriteString("\n")
		}
	}
}

// GenerateMarkdown writes a policy payload as the Markdown Pandoc turns into
// a Word document: title, document control, the sections with their control
// claims, the control mapping and the revision history.
func GenerateMarkdown(p PolicyPayload, _ Brand) string {
	var b strings.Builder
	b.WriteString("# " + mdText(p.Title) + "\n\n")
	if p.Summary != "" {
		b.WriteString(mdText(p.Summary) + "\n\n")
	}
	b.WriteString("## Document control\n\n| Field | Value |\n| --- | --- |\n")
	for _, kv := range [][2]string{
		{"Reference", p.Reference}, {"Classification", p.Classification}, {"Status", p.Status}, {"Owner", p.OwnerRole},
		{"Approver", p.Approver}, {"Client", p.ClientName}, {"Effective", p.EffectiveDate}, {"Next review", p.NextReviewDate},
		{"Frameworks", strings.Join(p.Frameworks, ", ")},
	} {
		if strings.TrimSpace(kv[1]) != "" {
			b.WriteString("| " + kv[0] + " | " + mdText(kv[1]) + " |\n")
		}
	}
	b.WriteString("\n")
	for _, s := range p.Sections {
		b.WriteString("## " + mdText(s.Heading) + "\n\n")
		if len(s.Blocks) > 0 {
			mdBlocks(&b, s.Blocks, "")
		} else {
			b.WriteString(mdLegacyBody(s.Body))
		}
		if len(s.Controls) > 0 {
			var claims []string
			for _, c := range s.Controls {
				claims = append(claims, mdText(c.ControlID+" ("+c.Coverage+")"))
			}
			b.WriteString("*Satisfies: " + strings.Join(claims, "; ") + "*\n\n")
		}
	}
	if len(p.Controls) > 0 {
		b.WriteString("## Control mapping\n\n| Control | Name | Coverage | Section |\n| --- | --- | --- | --- |\n")
		for _, c := range p.Controls {
			b.WriteString("| " + mdText(c.ControlID) + " | " + mdText(c.ControlName) + " | " + mdText(c.Coverage) + " | " + mdText(c.SectionHeading) + " |\n")
		}
		b.WriteString("\n")
	}
	if len(p.Versions) > 0 {
		b.WriteString("## Revision history\n\n| Version | Approved by | Date | Changes |\n| --- | --- | --- | --- |\n")
		for _, v := range p.Versions {
			b.WriteString("| " + mdText(v.VersionLabel) + " | " + mdText(v.ApprovedBy) + " | " + mdText(v.ApprovedAt) + " | " + mdText(v.ChangeSummary) + " |\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// mdLegacyBody sets a section-editor body the way the LaTeX path does: lists
// where every line of a block is an item, everything else escaped prose. The
// author's own Markdown is never handed to Pandoc to interpret.
func mdLegacyBody(body string) string {
	var b strings.Builder
	for _, block := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		block = strings.Trim(block, "\n")
		if strings.TrimSpace(block) == "" {
			continue
		}
		if items, ordered, ok := listItems(strings.Split(block, "\n")); ok {
			for i, item := range items {
				marker := "- "
				if ordered {
					marker = strconv.Itoa(i+1) + ". "
				}
				b.WriteString(marker + mdText(item) + "\n")
			}
		} else {
			b.WriteString(mdText(block))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}
