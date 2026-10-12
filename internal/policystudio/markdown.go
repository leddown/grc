package policystudio

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"grc/internal/policydocs"
)

// ---- ProseMirror -> Markdown ----

// md renders Markdown with a client's fact values.
type md struct{ facts map[string]string }

// UnresolvedPlaceholder is what an unfilled fact renders as everywhere text is
// produced; policydocs' lint gate blocks approval on it.
func UnresolvedPlaceholder(key string) string { return "[[UNRESOLVED: " + key + "]]" }

// SectionMarkdown renders a resolved section's body -- everything after its
// heading -- as the Markdown the section row's body column holds. Section
// headings are "##" in the document export, so body headings sit one and two
// levels below.
//
// A fact token renders as its value from the document's client profile, or as
// the [[UNRESOLVED: key]] placeholder the lint gate blocks approval on when the
// profile has none: an approved policy cannot carry an unfilled fact.
func SectionMarkdown(section *Node, facts map[string]string) string {
	m := md{facts: facts}
	var blocks []string
	for i, c := range section.Content {
		if i == 0 && c.Type == "sectionHeading" {
			continue
		}
		if s := m.blockMarkdown(c, ""); s != "" {
			blocks = append(blocks, s)
		}
	}
	return strings.TrimSpace(strings.Join(blocks, "\n\n"))
}

func (m md) blockMarkdown(n *Node, indent string) string {
	switch n.Type {
	case "paragraph":
		return indent + m.inlineMarkdown(n.Content, indent)
	case "heading":
		level, _ := asInt(n.Attrs["level"])
		return indent + strings.Repeat("#", level+1) + " " + m.inlineMarkdown(n.Content, indent)
	case "blockquote":
		return quote(m.childBlocks(n, ""), "")
	case "callout":
		label := "Note"
		if n.Attr("kind") == "important" {
			label = "Important"
		}
		return quote("**"+label+":** "+m.childBlocks(n, ""), "")
	case "bulletList", "orderedList":
		return m.listMarkdown(n, indent)
	case "table":
		return m.tableMarkdown(n)
	}
	return ""
}

func (m md) childBlocks(n *Node, indent string) string {
	var parts []string
	for _, c := range n.Content {
		parts = append(parts, m.blockMarkdown(c, indent))
	}
	return strings.Join(parts, "\n\n")
}

func quote(body, _ string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

func (m md) listMarkdown(n *Node, indent string) string {
	start, _ := asInt(n.Attrs["start"])
	if start < 1 {
		start = 1
	}
	var items []string
	for i, item := range n.Content {
		marker := "- "
		if n.Type == "orderedList" {
			marker = strconv.Itoa(start+i) + ". "
		}
		inner := indent + strings.Repeat(" ", len(marker))
		var parts []string
		for j, c := range item.Content {
			s := m.blockMarkdown(c, inner)
			if j == 0 {
				s = indent + marker + strings.TrimPrefix(s, inner)
			}
			parts = append(parts, s)
		}
		items = append(items, strings.Join(parts, "\n"))
	}
	return strings.Join(items, "\n")
}

func (m md) tableMarkdown(n *Node) string {
	var rows [][]string
	for _, row := range n.Content {
		var cells []string
		for _, cell := range row.Content {
			var parts []string
			for _, p := range cell.Content {
				parts = append(parts, m.inlineMarkdown(p.Content, ""))
			}
			cells = append(cells, strings.ReplaceAll(strings.Join(parts, "<br>"), "|", `\|`))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	line := func(cells []string) string {
		for len(cells) < width {
			cells = append(cells, "")
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	out := []string{line(rows[0]), "|" + strings.Repeat("---|", width)}
	for _, r := range rows[1:] {
		out = append(out, line(r))
	}
	return strings.Join(out, "\n")
}

var markdownSpecial = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", `\<`)

func (m md) inlineMarkdown(content []*Node, indent string) string {
	var b strings.Builder
	for _, n := range content {
		switch n.Type {
		case "text":
			s := markdownSpecial.Replace(n.Text)
			if _, ok := n.HasMark("code"); ok {
				s = "`" + strings.ReplaceAll(n.Text, "`", "'") + "`"
			}
			if _, ok := n.HasMark("italic"); ok {
				s = "*" + s + "*"
			}
			if _, ok := n.HasMark("bold"); ok {
				s = "**" + s + "**"
			}
			if m, ok := n.HasMark("link"); ok {
				href, _ := m.Attrs["href"].(string)
				s = "[" + s + "](" + strings.NewReplacer(" ", "%20", ")", "%29", "(", "%28").Replace(href) + ")"
			}
			b.WriteString(s)
		case "hardBreak":
			b.WriteString("\\\n" + indent)
		case "factToken":
			if v, ok := m.facts[n.Attr("key")]; ok {
				b.WriteString(markdownSpecial.Replace(v))
			} else {
				b.WriteString(UnresolvedPlaceholder(n.Attr("key")))
			}
		case "controlRef":
			b.WriteString(n.Attr("controlId"))
		}
	}
	return b.String()
}

// HeadingText is the plain text of a section's heading.
func HeadingText(section *Node) string {
	if len(section.Content) > 0 && section.Content[0].Type == "sectionHeading" {
		return strings.TrimSpace(section.Content[0].TextContent())
	}
	return ""
}

// ---- Markdown -> ProseMirror ----

var markdownParser = goldmark.New(goldmark.WithExtensions(extension.Table))

// Tokens the restricted Markdown of templates and legacy bodies may carry:
// {{fact:key}} and a legacy [[UNRESOLVED: key]] become fact tokens,
// [[control:ID]] a control reference.
var (
	inlineToken = regexp.MustCompile(`\{\{fact:([a-z][a-z0-9_]{0,63})\}\}|\[\[UNRESOLVED:\s*([a-z][a-z0-9_]{0,63})\s*\]\]|\[\[control:([A-Za-z0-9][A-Za-z0-9 ._()/-]{0,39})\]\]`)
)

// SectionFromMarkdown builds a Studio section from a legacy section row.
//
// The legacy editor rendered bodies preformatted, so a single newline was a
// visible line break; it becomes a hard break here rather than being folded
// into a space. Anything the schema has no place for -- raw HTML, images, code
// blocks -- keeps its text and loses its structure. Nothing is fetched and
// nothing is executed: this is the only Markdown this application parses.
func SectionFromMarkdown(uid, kind, heading, body string, newID func() string) *Node {
	section := &Node{Type: "policySection", Attrs: map[string]any{"uid": uid, "kind": kind}}
	h := &Node{Type: "sectionHeading", Attrs: map[string]any{"bid": newID()}}
	if t := strings.TrimSpace(heading); t != "" {
		h.Content = []*Node{{Type: "text", Text: t}}
	}
	section.Content = append(section.Content, h)

	source := []byte(body)
	doc := markdownParser.Parser().Parse(text.NewReader(source))
	c := converter{source: source, newID: newID}
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		section.Content = append(section.Content, c.blocks(child)...)
	}
	if len(section.Content) == 1 {
		section.Content = append(section.Content, c.paragraph(nil))
	}
	return section
}

type converter struct {
	source []byte
	newID  func() string
}

func (c converter) paragraph(inline []*Node) *Node {
	return &Node{Type: "paragraph", Attrs: map[string]any{"bid": c.newID()}, Content: inline}
}

func (c converter) blocks(n ast.Node) []*Node {
	switch v := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return []*Node{c.paragraph(c.inlines(n, nil))}
	case *ast.Heading:
		level := 2
		if v.Level > 2 {
			level = 3
		}
		return []*Node{{Type: "heading", Attrs: map[string]any{"bid": c.newID(), "level": level}, Content: c.inlines(n, nil)}}
	case *ast.Blockquote:
		content := c.paragraphsOf(n)
		if len(content) == 0 {
			content = []*Node{c.paragraph(nil)}
		}
		return []*Node{{Type: "blockquote", Attrs: map[string]any{"bid": c.newID()}, Content: content}}
	case *ast.List:
		list := &Node{Type: "bulletList"}
		if v.IsOrdered() {
			start := v.Start
			if start < 1 {
				start = 1
			}
			list = &Node{Type: "orderedList", Attrs: map[string]any{"start": start}}
		}
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			li := &Node{Type: "listItem", Attrs: map[string]any{"bid": c.newID()}}
			for child := item.FirstChild(); child != nil; child = child.NextSibling() {
				if _, nested := child.(*ast.List); nested && len(li.Content) > 0 {
					li.Content = append(li.Content, c.blocks(child)...)
					continue
				}
				li.Content = append(li.Content, c.asParagraphs(child)...)
			}
			if len(li.Content) == 0 {
				li.Content = []*Node{c.paragraph(nil)}
			}
			list.Content = append(list.Content, li)
		}
		return []*Node{list}
	case *east.Table:
		table := &Node{Type: "table"}
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			cellType := "tableCell"
			if _, header := row.(*east.TableHeader); header {
				cellType = "tableHeader"
			}
			tr := &Node{Type: "tableRow"}
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				tr.Content = append(tr.Content, &Node{Type: cellType, Attrs: map[string]any{"bid": c.newID()},
					Content: []*Node{c.paragraph(c.inlines(cell, nil))}})
			}
			if len(tr.Content) > 0 {
				table.Content = append(table.Content, tr)
			}
		}
		if len(table.Content) == 0 {
			return nil
		}
		return []*Node{table}
	case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock:
		lines := n.Lines()
		var inline []*Node
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			line := strings.TrimRight(string(seg.Value(c.source)), "\n")
			if i > 0 {
				inline = append(inline, &Node{Type: "hardBreak"})
			}
			if line != "" {
				inline = append(inline, &Node{Type: "text", Text: line})
			}
		}
		return []*Node{c.paragraph(inline)}
	case *ast.ThematicBreak:
		return nil
	}
	return c.paragraphsOf(n)
}

// paragraphsOf flattens any block into paragraphs, for places the schema only
// allows paragraphs (blockquotes, list items after their first block).
func (c converter) paragraphsOf(n ast.Node) []*Node {
	var out []*Node
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		out = append(out, c.asParagraphs(child)...)
	}
	return out
}

// asParagraphs converts one block, keeping its text and dropping structure
// the enclosing node cannot hold.
func (c converter) asParagraphs(n ast.Node) []*Node {
	switch n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return pruneEmptyText([]*Node{c.paragraph(c.inlines(n, nil))})
	}
	var out []*Node
	for _, b := range c.blocks(n) {
		if b.Type == "paragraph" {
			out = append(out, b)
			continue
		}
		if t := strings.TrimSpace(Resolve(b, Baseline).TextContent()); t != "" {
			out = append(out, c.paragraph([]*Node{{Type: "text", Text: t}}))
		}
	}
	return pruneEmptyText(out)
}

func pruneEmptyText(ps []*Node) []*Node {
	for _, p := range ps {
		var kept []*Node
		for _, c := range p.Content {
			if c.IsText() && c.Text == "" {
				continue
			}
			kept = append(kept, c)
		}
		p.Content = kept
	}
	return ps
}

// inlines converts n's inline children, carrying the marks of enclosing
// emphasis and links.
func (c converter) inlines(n ast.Node, marks []Mark) []*Node {
	var out []*Node
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch v := child.(type) {
		case *ast.Text:
			out = append(out, textNode(string(v.Segment.Value(c.source)), marks))
			if v.HardLineBreak() || v.SoftLineBreak() {
				out = append(out, &Node{Type: "hardBreak"})
			}
		case *ast.String:
			out = append(out, textNode(string(v.Value), marks))
		case *ast.CodeSpan:
			var b strings.Builder
			for t := v.FirstChild(); t != nil; t = t.NextSibling() {
				if seg, ok := t.(*ast.Text); ok {
					b.Write(seg.Segment.Value(c.source))
				}
			}
			out = append(out, textNode(b.String(), withMark(onlyLink(marks), Mark{Type: "code"})))
		case *ast.Emphasis:
			m := Mark{Type: "italic"}
			if v.Level >= 2 {
				m = Mark{Type: "bold"}
			}
			out = append(out, c.inlines(v, withMark(marks, m))...)
		case *ast.Link:
			out = append(out, c.inlines(v, c.linkMarks(marks, string(v.Destination)))...)
		case *ast.AutoLink:
			url := string(v.URL(c.source))
			out = append(out, textNode(url, c.linkMarks(marks, url)))
		case *ast.Image:
			out = append(out, c.inlines(v, marks)...)
		case *ast.RawHTML:
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				out = append(out, textNode(string(seg.Value(c.source)), marks))
			}
		default:
			out = append(out, c.inlines(child, marks)...)
		}
	}
	// Merge and drop what the schema would not hold: empty text and a trailing
	// hard break at the end of a block.
	var merged []*Node
	for _, n := range out {
		if n.IsText() && n.Text == "" {
			continue
		}
		merged = appendText(merged, n)
	}
	for len(merged) > 0 && merged[len(merged)-1].Type == "hardBreak" {
		merged = merged[:len(merged)-1]
	}
	// Placeholders are found only now: the parser splits "[[" into separate
	// text segments.
	var withTokens []*Node
	for _, n := range merged {
		if !n.IsText() {
			withTokens = append(withTokens, n)
			continue
		}
		for _, t := range c.textWithTokens(n.Text, n.Marks) {
			if t.IsText() && t.Text == "" {
				continue
			}
			withTokens = append(withTokens, t)
		}
	}
	return withTokens
}

func (c converter) linkMarks(marks []Mark, href string) []Mark {
	if linkHref(href) != nil {
		return marks
	}
	return withMark(marks, Mark{Type: "link", Attrs: map[string]any{"href": href}})
}

func (c converter) textWithTokens(s string, marks []Mark) []*Node {
	var out []*Node
	last := 0
	for _, loc := range inlineToken.FindAllStringSubmatchIndex(s, -1) {
		out = append(out, textNode(s[last:loc[0]], marks))
		switch {
		case loc[2] >= 0:
			out = append(out, &Node{Type: "factToken", Attrs: map[string]any{"key": s[loc[2]:loc[3]]}})
		case loc[4] >= 0:
			out = append(out, &Node{Type: "factToken", Attrs: map[string]any{"key": s[loc[4]:loc[5]]}})
		default:
			out = append(out, &Node{Type: "controlRef", Attrs: map[string]any{"controlId": s[loc[6]:loc[7]]}})
		}
		last = loc[1]
	}
	return append(out, textNode(s[last:], marks))
}

func textNode(s string, marks []Mark) *Node {
	return &Node{Type: "text", Text: s, Marks: append([]Mark(nil), marks...)}
}

func withMark(marks []Mark, m Mark) []Mark {
	out := []Mark{}
	for _, existing := range marks {
		if schema.excludes(m.Type, existing.Type) || schema.excludes(existing.Type, m.Type) {
			continue
		}
		out = append(out, existing)
	}
	return append(out, m)
}

func onlyLink(marks []Mark) []Mark {
	var out []Mark
	for _, m := range marks {
		if m.Type == "link" {
			out = append(out, m)
		}
	}
	return out
}

// DocumentFromRows builds a Studio document from a document's section rows:
// from a row's stored content when it has valid content, otherwise from its
// Markdown body. Detached rows are left out.
func DocumentFromRows(sections []policydocs.Section, newID func() string) (*Node, error) {
	root := &Node{Type: "doc"}
	for _, s := range sections {
		if s.DetachedAt != "" {
			continue
		}
		if s.ContentJSON != "" {
			if n, err := parseSection(s.ContentJSON, s.UID); err == nil {
				root.Content = append(root.Content, n)
				continue
			}
		}
		root.Content = append(root.Content, SectionFromMarkdown(s.UID, s.SectionKind, s.Heading, s.Body, newID))
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("the document has no sections to start from")
	}
	if err := Validate(root); err != nil {
		return nil, fmt.Errorf("build document from sections: %w", err)
	}
	return root, nil
}

func parseSection(raw, uid string) (*Node, error) {
	var n Node
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		return nil, err
	}
	if n.Type != "policySection" || n.Attr("uid") != uid {
		return nil, fmt.Errorf("stored content is not this section")
	}
	return &n, Validate(&n)
}

// restrictedParser has no table extension: AI output may not build tables.
var restrictedParser = goldmark.New()

// RestrictedMarkdown parses Markdown written by a model into schema blocks.
// Unlike SectionFromMarkdown, which keeps whatever text a legacy body had, it
// refuses anything outside the restricted set -- paragraphs, - and 1. lists,
// **bold**, *italic*, `code`, links, {{fact:key}} and [[control:ID]] -- rather
// than converting it, so raw HTML, headings, tables, images and code blocks
// never reach the document. The blocks returned are schema-valid.
func RestrictedMarkdown(body string, newID func() string) ([]*Node, error) {
	source := []byte(strings.TrimSpace(body))
	if len(source) == 0 {
		return nil, fmt.Errorf("the replacement is empty")
	}
	// A model states a missing fact as {{fact:key}}; the legacy placeholder
	// would otherwise be converted like one.
	if strings.Contains(body, "[[UNRESOLVED") {
		return nil, fmt.Errorf("write a missing fact as {{fact:key}}, not [[UNRESOLVED: key]]")
	}
	doc := restrictedParser.Parser().Parse(text.NewReader(source))
	if err := restrictedAST(doc); err != nil {
		return nil, err
	}
	c := converter{source: source, newID: newID}
	var blocks []*Node
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		blocks = append(blocks, c.blocks(child)...)
	}
	probe := &Node{Type: "doc", Content: []*Node{{Type: "policySection", Attrs: map[string]any{"uid": "probe", "kind": "other"},
		Content: append([]*Node{{Type: "sectionHeading", Attrs: map[string]any{"bid": "probe"}}}, blocks...)}}}
	if err := Validate(probe); err != nil {
		return nil, err
	}
	return blocks, nil
}

func restrictedAST(n ast.Node) error {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch v := child.(type) {
		case *ast.Paragraph, *ast.TextBlock, *ast.List, *ast.ListItem, *ast.Text, *ast.String, *ast.CodeSpan, *ast.Emphasis:
		case *ast.Link:
			if linkHref(string(v.Destination)) != nil {
				return fmt.Errorf("the link to %q is not an https, http or mailto link", string(v.Destination))
			}
		case *ast.Heading:
			return fmt.Errorf("headings are not allowed in a proposed edit")
		case *ast.HTMLBlock, *ast.RawHTML:
			return fmt.Errorf("raw HTML is not allowed in a proposed edit")
		default:
			return fmt.Errorf("%s is not allowed in a proposed edit", strings.ToLower(strings.TrimPrefix(fmt.Sprintf("%T", child), "*ast.")))
		}
		if err := restrictedAST(child); err != nil {
			return err
		}
	}
	return nil
}
