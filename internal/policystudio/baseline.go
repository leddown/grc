package policystudio

import "strings"

// zeroWidthSpace is what prosemirror-suggest-changes inserts, marked as an
// insertion, where a block was split in suggest mode.
const zeroWidthSpace = "​"

// View selects how pending suggestions are resolved.
type View int

const (
	// Baseline is what the document says until suggestions are decided:
	// pending insertions are left out, pending deletions are kept, and
	// suggested attribute changes are reverted. Lint, knowledge, exports and
	// approval read this.
	Baseline View = iota
	// Proposed is the document as if every pending suggestion were accepted.
	Proposed
)

var suggestionMarkTypes = map[string]bool{"insertion": true, "deletion": true, "modification": true}

// Resolve returns a copy of n with pending suggestions resolved per v and the
// suggestion marks removed. It returns nil when n itself is resolved away.
func Resolve(n *Node, v View) *Node {
	drop := "insertion"
	if v == Proposed {
		drop = "deletion"
	}
	if _, ok := n.HasMark(drop); ok {
		return nil
	}
	out := &Node{Type: n.Type, Text: n.Text, Attrs: map[string]any{}}
	for k, val := range n.Attrs {
		out.Attrs[k] = val
	}
	for _, m := range n.Marks {
		if m.Type == "modification" {
			if v == Baseline {
				if name, _ := m.Attrs["attrName"].(string); name != "" {
					if _, known := out.Attrs[name]; known {
						out.Attrs[name] = m.Attrs["previousValue"]
					}
				}
			}
			continue
		}
		if !suggestionMarkTypes[m.Type] {
			out.Marks = append(out.Marks, m)
		}
	}
	if out.IsText() {
		out.Text = strings.ReplaceAll(out.Text, zeroWidthSpace, "")
		if out.Text == "" {
			return nil
		}
		return out
	}
	for _, c := range n.Content {
		if r := Resolve(c, v); r != nil {
			out.Content = appendText(out.Content, r)
		}
	}
	return out
}
