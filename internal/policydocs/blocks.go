package policydocs

import (
	"encoding/json"
	"html"
	"net/url"
	"strconv"
	"strings"
)

// Block is one typed block of a section's content: the rich form of a section
// that the Policy Studio projects into policy_sections.blocks_json and the
// render templates read as sections[].blocks. Every string in it is text to be
// set, never markup to be interpreted: the HTML renderer escapes it, and the
// Typst and LaTeX templates place it as data.
//
// A section without blocks (a section-editor document) renders from its body,
// as before.
type Block struct {
	// Type is paragraph, heading, bullet_list, ordered_list, table,
	// blockquote or callout.
	Type string `json:"type"`
	// Level is 2 or 3 for a heading (the section heading is level 1).
	Level int `json:"level,omitempty"`
	// Start is an ordered list's first number.
	Start int `json:"start,omitempty"`
	// Kind is a callout's note or important.
	Kind string `json:"kind,omitempty"`
	// Runs is the inline content of a paragraph or heading.
	Runs []Run `json:"runs,omitempty"`
	// Items are a list's items, each a sequence of blocks (a paragraph, and
	// possibly a nested list).
	Items [][]Block `json:"items,omitempty"`
	// Rows are a table's rows.
	Rows []TableRow `json:"rows,omitempty"`
	// Blocks is the content of a blockquote or callout.
	Blocks []Block `json:"blocks,omitempty"`
}

// TableRow is one table row; Header marks a row of header cells.
type TableRow struct {
	Header bool      `json:"header"`
	Cells  [][]Block `json:"cells"`
}

// Run is a stretch of inline content with uniform formatting.
type Run struct {
	Text string `json:"text"`
	// Marks are any of bold, italic, code.
	Marks []string `json:"marks,omitempty"`
	// Href is a link target (https, http or mailto; validated upstream).
	Href string `json:"href,omitempty"`
	// Fact marks a client fact. Text holds its value, or the unresolved
	// placeholder when the client profile has none.
	Fact *RunFact `json:"fact,omitempty"`
	// Control marks a reference to a catalog control; Text is its id.
	Control *RunControl `json:"control,omitempty"`
	// Break is a hard line break; Text is empty.
	Break bool `json:"break,omitempty"`
}

type RunFact struct {
	Key        string `json:"key"`
	Value      string `json:"value"`
	Unresolved bool   `json:"unresolved"`
}

type RunControl struct {
	ID string `json:"id"`
}

// HasMark reports whether the run carries a mark.
func (r Run) HasMark(m string) bool {
	for _, x := range r.Marks {
		if x == m {
			return true
		}
	}
	return false
}

// decodeBlocks reads a section's stored blocks; nil when it has none or they
// do not parse (the body is then used, which is never wrong, only plainer).
func decodeBlocks(raw string) []Block {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []Block
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// ---- HTML ----

// BlocksHTML renders blocks as HTML. Every string is escaped; a link target is
// re-checked here as well, so a renderer never trusts that the projection did.
func BlocksHTML(blocks []Block) string {
	var b strings.Builder
	writeBlocksHTML(&b, blocks)
	return b.String()
}

func writeBlocksHTML(b *strings.Builder, blocks []Block) {
	for _, blk := range blocks {
		switch blk.Type {
		case "paragraph":
			b.WriteString("<p>")
			writeRunsHTML(b, blk.Runs)
			b.WriteString("</p>\n")
		case "heading":
			tag := "h3"
			if blk.Level == 3 {
				tag = "h4"
			}
			b.WriteString("<" + tag + ">")
			writeRunsHTML(b, blk.Runs)
			b.WriteString("</" + tag + ">\n")
		case "bullet_list", "ordered_list":
			tag := "ul"
			open := "<ul>"
			if blk.Type == "ordered_list" {
				tag = "ol"
				open = "<ol>"
				if blk.Start > 1 {
					open = `<ol start="` + strconv.Itoa(blk.Start) + `">`
				}
			}
			b.WriteString(open + "\n")
			for _, item := range blk.Items {
				b.WriteString("<li>")
				writeBlocksHTML(b, item)
				b.WriteString("</li>\n")
			}
			b.WriteString("</" + tag + ">\n")
		case "table":
			b.WriteString("<table class=\"content\">\n")
			for _, row := range blk.Rows {
				cell := "td"
				if row.Header {
					cell = "th"
				}
				b.WriteString("<tr>")
				for _, c := range row.Cells {
					b.WriteString("<" + cell + ">")
					writeBlocksHTML(b, c)
					b.WriteString("</" + cell + ">")
				}
				b.WriteString("</tr>\n")
			}
			b.WriteString("</table>\n")
		case "blockquote":
			b.WriteString("<blockquote>\n")
			writeBlocksHTML(b, blk.Blocks)
			b.WriteString("</blockquote>\n")
		case "callout":
			label := "Note"
			if blk.Kind == "important" {
				label = "Important"
			}
			b.WriteString(`<aside class="callout callout-` + html.EscapeString(blk.Kind) + `"><strong>` + label + ":</strong>\n")
			writeBlocksHTML(b, blk.Blocks)
			b.WriteString("</aside>\n")
		}
	}
}

func writeRunsHTML(b *strings.Builder, runs []Run) {
	for _, r := range runs {
		if r.Break {
			b.WriteString("<br>")
			continue
		}
		s := html.EscapeString(r.Text)
		switch {
		case r.Fact != nil && r.Fact.Unresolved:
			s = `<mark class="unresolved">` + s + `</mark>`
		case r.Control != nil:
			s = `<span class="control-ref">` + s + `</span>`
		}
		if r.HasMark("code") {
			s = "<code>" + s + "</code>"
		}
		if r.HasMark("italic") {
			s = "<em>" + s + "</em>"
		}
		if r.HasMark("bold") {
			s = "<strong>" + s + "</strong>"
		}
		if r.Href != "" && SafeHref(r.Href) {
			s = `<a href="` + html.EscapeString(r.Href) + `" rel="noopener noreferrer">` + s + "</a>"
		}
		b.WriteString(s)
	}
}

// SafeHref reports whether a link target is one a rendered document may carry:
// https, http (with a host) or mailto.
func SafeHref(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	for _, r := range href {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		return u.Host != ""
	case "mailto":
		return true
	}
	return false
}
