package policyai

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"grc/internal/policystudio"
)

// A block's text, as the model sees it and as a quote is matched against:
// the block's baseline (pending insertions left out, pending deletions kept),
// with a hard break as a newline and the tokens in their Markdown form. The
// Studio's editor renders a block the same way when it places an edit
// (blockText in web/policy-studio/src/ai.js); the two must agree.

// textBlockTypes are the blocks an edit can anchor on.
var textBlockTypes = map[string]bool{"paragraph": true, "heading": true, "sectionHeading": true}

// block is one anchorable block of the live document.
type block struct {
	ID         string
	SectionUID string
	Type       string
	// Text is the block's baseline text.
	Text string
	// Protected marks the byte ranges of Text under a pending deletion or
	// change, and Inserts the offsets where a pending insertion sits; a quote
	// may cover neither.
	Protected [][2]int
	Inserts   []int
	// Pending is set when the whole block is itself a pending suggestion.
	Pending bool
	// Prefix is how the block reads in context: "- " in a list, "## " for a
	// heading.
	Prefix string
}

func hasSuggestion(n *policystudio.Node, types ...string) bool {
	for _, m := range n.Marks {
		for _, t := range types {
			if m.Type == t {
				return true
			}
		}
	}
	return false
}

// documentBlocks lists every anchorable block, in document order.
func documentBlocks(sections []*policystudio.Node) []block {
	var out []block
	var walk func(n *policystudio.Node, sectionUID, prefix string, pending bool)
	walk = func(n *policystudio.Node, sectionUID, prefix string, pending bool) {
		pending = pending || hasSuggestion(n, "insertion", "deletion")
		switch n.Type {
		case "bulletList", "orderedList":
			prefix = "- "
		case "tableCell", "tableHeader":
			prefix = "| "
		case "blockquote":
			prefix = "> "
		}
		if textBlockTypes[n.Type] {
			b := inlineText(n)
			b.ID, b.SectionUID, b.Type, b.Pending = n.Attr("bid"), sectionUID, n.Type, pending
			b.Prefix = prefix
			switch n.Type {
			case "sectionHeading":
				b.Prefix = "# "
			case "heading":
				b.Prefix = "## "
			}
			if b.ID != "" {
				out = append(out, b)
			}
			return
		}
		for _, c := range n.Content {
			walk(c, sectionUID, prefix, pending)
		}
	}
	for _, s := range sections {
		walk(s, s.Attr("uid"), "", false)
	}
	return out
}

func inlineText(n *policystudio.Node) block {
	var b strings.Builder
	var out block
	for _, c := range n.Content {
		if hasSuggestion(c, "insertion") {
			out.Inserts = append(out.Inserts, b.Len())
			continue
		}
		start := b.Len()
		switch c.Type {
		case "text":
			b.WriteString(strings.ReplaceAll(c.Text, "​", ""))
		case "hardBreak":
			b.WriteString("\n")
		case "factToken":
			b.WriteString("{{fact:" + c.Attr("key") + "}}")
		case "controlRef":
			b.WriteString("[[control:" + c.Attr("controlId") + "]]")
		}
		if hasSuggestion(c, "deletion", "modification") && b.Len() > start {
			out.Protected = append(out.Protected, [2]int{start, b.Len()})
		}
	}
	out.Text = b.String()
	return out
}

// normalized is a string prepared for quote matching, with the original byte
// span of every byte it holds.
type normalized struct {
	s    string
	from []int
	to   []int
}

var quoteFold = map[rune]rune{
	'‘': '\'', '’': '\'', '‚': '\'', '‛': '\'', '′': '\'',
	'“': '"', '”': '"', '„': '"', '‟': '"', '″': '"',
	'‐': '-', '‑': '-', '‒': '-', '–': '-', '—': '-', '―': '-', '−': '-',
}

// normalize applies NFC, unifies quotes and dashes, and collapses whitespace
// runs to one space, keeping the map back to the original.
func normalize(s string) normalized {
	var out normalized
	var b strings.Builder
	emit := func(r rune, from, to int) {
		start := b.Len()
		b.WriteRune(r)
		for i := start; i < b.Len(); i++ {
			out.from = append(out.from, from)
			out.to = append(out.to, to)
		}
	}
	space := false
	var it norm.Iter
	it.InitString(norm.NFC, s)
	pos := 0
	for !it.Done() {
		seg := it.Next()
		end := it.Pos()
		for _, r := range string(seg) {
			if f, ok := quoteFold[r]; ok {
				r = f
			}
			if unicode.IsSpace(r) {
				if !space && b.Len() > 0 {
					emit(' ', pos, end)
				}
				space = true
				continue
			}
			space = false
			emit(r, pos, end)
		}
		pos = end
	}
	out.s = strings.TrimRight(b.String(), " ")
	out.from, out.to = out.from[:len(out.s)], out.to[:len(out.s)]
	return out
}

// findQuote locates quote in text: exactly once, after normalization. It
// returns the byte range in text, or a reason it cannot be anchored.
func findQuote(text, quote string) (int, int, string) {
	q := normalize(quote).s
	if q == "" {
		return 0, 0, "the quote is empty"
	}
	t := normalize(text)
	first := strings.Index(t.s, q)
	if first < 0 {
		return 0, 0, "the quoted text is not in the block"
	}
	if strings.Contains(t.s[first+1:], q) {
		return 0, 0, "the quoted text appears more than once in the block"
	}
	last := first + len(q) - 1
	return t.from[first], t.to[last], ""
}

// overlapsPending reports whether [from, to) touches a pending suggestion.
func (b block) overlapsPending(from, to int) bool {
	for _, p := range b.Protected {
		if from < p[1] && p[0] < to {
			return true
		}
	}
	for _, at := range b.Inserts {
		if from < at && at < to {
			return true
		}
	}
	return false
}
