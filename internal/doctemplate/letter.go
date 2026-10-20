package doctemplate

import (
	"strings"
	"time"
)

// The letter's parts are found by section kind, as in templates/typst/letter.typ;
// a section of any other kind is part of the body.
var letterPlaced = map[string]bool{
	"sender": true, "addressee": true, "signatories": true, "enclosures": true, "copy_to": true, "annex": true,
}

// LongDate writes 2026-10-28 as 28 October 2026, and anything else as given.
func LongDate(iso string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(iso))
	if err != nil {
		return iso
	}
	return t.Format("2 January 2006")
}

// GenerateLetterMarkdown writes a letter payload as the Markdown Pandoc turns
// into a Word document, in the order of the printed letter. The Word file is
// for finishing on the client's own letterhead template, so it carries the
// text and structure and no house styling. Every string is escaped as in
// GenerateMarkdown.
func GenerateLetterMarkdown(p PolicyPayload) string {
	var b strings.Builder
	marking := strings.ToUpper(strings.TrimSpace(p.Classification))
	if p.Status != "approved" {
		marking = strings.TrimSpace("DRAFT — NOT APPROVED " + marking)
	}
	if marking != "" {
		b.WriteString("**" + mdText(marking) + "**\n\n")
	}

	sections := func(kind string) []PolicySection {
		var out []PolicySection
		for _, s := range p.Sections {
			if s.SectionKind == kind {
				out = append(out, s)
			}
		}
		return out
	}
	body := func(s PolicySection) {
		if len(s.Blocks) > 0 {
			mdBlocks(&b, s.Blocks, "")
		} else {
			b.WriteString(mdLetterLines(s.Body))
		}
	}

	if strings.TrimSpace(p.ClientName) != "" {
		b.WriteString("**" + mdText(p.ClientName) + "**\n\n")
	}
	for _, s := range sections("sender") {
		body(s)
	}
	for _, s := range sections("addressee") {
		body(s)
	}

	var dateLine []string
	if strings.TrimSpace(p.Reference) != "" {
		dateLine = append(dateLine, "Our reference: "+mdText(p.Reference))
	}
	if strings.TrimSpace(p.EffectiveDate) == "" {
		dateLine = append(dateLine, "**Date not set**")
	} else {
		dateLine = append(dateLine, mdText(LongDate(p.EffectiveDate)))
	}
	b.WriteString(strings.Join(dateLine, "\\\n") + "\n\n")
	b.WriteString("**" + mdText(p.Title) + "**\n\n")

	for _, s := range p.Sections {
		if letterPlaced[s.SectionKind] {
			continue
		}
		if s.SectionKind != "letter_body" && strings.TrimSpace(s.Heading) != "" {
			b.WriteString("### " + mdText(s.Heading) + "\n\n")
		}
		body(s)
	}

	for _, signer := range letterSigners(sections("signatories")) {
		b.WriteString(mdText(strings.Repeat("_", 32)) + "\n\n")
		b.WriteString(signer + "\n\n")
	}

	for _, part := range []struct{ label, kind string }{{"Enclosures", "enclosures"}, {"Copy to", "copy_to"}} {
		found := sections(part.kind)
		if len(found) == 0 {
			continue
		}
		b.WriteString("**" + part.label + "**\n\n")
		for _, s := range found {
			body(s)
		}
	}

	for _, s := range sections("annex") {
		b.WriteString("## " + mdText(s.Heading) + "\n\n")
		body(s)
	}
	return b.String()
}

// letterSigners reads the signatories: a table of name and function, its
// header row skipped, or one paragraph per signatory. Each comes back as the
// Markdown set under its signature line.
func letterSigners(sections []PolicySection) []string {
	var out []string
	cellText := func(blocks []PolicyBlock) string {
		var parts []string
		for _, blk := range blocks {
			parts = append(parts, mdRuns(blk.Runs))
		}
		return strings.Join(parts, " ")
	}
	for _, s := range sections {
		for _, blk := range s.Blocks {
			switch blk.Type {
			case "table":
				for _, row := range blk.Rows {
					if row.Header || len(row.Cells) == 0 {
						continue
					}
					signer := "**" + cellText(row.Cells[0]) + "**"
					if len(row.Cells) > 1 {
						signer += "\\\n" + cellText(row.Cells[1])
					}
					out = append(out, signer)
				}
			case "paragraph":
				out = append(out, mdRuns(blk.Runs))
			}
		}
		if len(s.Blocks) == 0 {
			for _, para := range strings.Split(strings.ReplaceAll(s.Body, "\r\n", "\n"), "\n\n") {
				if strings.TrimSpace(para) != "" {
					out = append(out, strings.TrimSpace(mdLetterLines(para)))
				}
			}
		}
	}
	return out
}

// mdLetterLines sets a section-editor body as a letter reads it: a blank line
// between paragraphs and a hard break for each newline within one, since an
// address is written one line at a time.
func mdLetterLines(body string) string {
	var b strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		var lines []string
		for _, line := range strings.Split(strings.Trim(para, "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, mdText(strings.TrimSpace(line)))
			}
		}
		if len(lines) > 0 {
			b.WriteString(strings.Join(lines, "\\\n") + "\n\n")
		}
	}
	return b.String()
}
