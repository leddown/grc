package policystudio

import (
	"grc/internal/policydocs"
)

// SectionBlocks turns a resolved section (see Resolve) into the render
// contract's blocks, everything after its heading. Fact tokens take their value
// from facts, or the unresolved placeholder with Unresolved set.
func SectionBlocks(section *Node, facts map[string]string) []policydocs.Block {
	out := []policydocs.Block{}
	for i, c := range section.Content {
		if i == 0 && c.Type == "sectionHeading" {
			continue
		}
		if b, ok := toBlock(c, facts); ok {
			out = append(out, b)
		}
	}
	return out
}

func toBlocks(nodes []*Node, facts map[string]string) []policydocs.Block {
	out := []policydocs.Block{}
	for _, n := range nodes {
		if b, ok := toBlock(n, facts); ok {
			out = append(out, b)
		}
	}
	return out
}

func toBlock(n *Node, facts map[string]string) (policydocs.Block, bool) {
	switch n.Type {
	case "paragraph":
		return policydocs.Block{Type: "paragraph", Runs: toRuns(n.Content, facts)}, true
	case "heading":
		level, _ := asInt(n.Attrs["level"])
		return policydocs.Block{Type: "heading", Level: level, Runs: toRuns(n.Content, facts)}, true
	case "bulletList", "orderedList":
		b := policydocs.Block{Type: "bullet_list"}
		if n.Type == "orderedList" {
			b.Type = "ordered_list"
			b.Start, _ = asInt(n.Attrs["start"])
			if b.Start < 1 {
				b.Start = 1
			}
		}
		for _, item := range n.Content {
			b.Items = append(b.Items, toBlocks(item.Content, facts))
		}
		return b, true
	case "table":
		b := policydocs.Block{Type: "table"}
		for _, row := range n.Content {
			r := policydocs.TableRow{Header: len(row.Content) > 0}
			for _, cell := range row.Content {
				if cell.Type != "tableHeader" {
					r.Header = false
				}
				r.Cells = append(r.Cells, toBlocks(cell.Content, facts))
			}
			b.Rows = append(b.Rows, r)
		}
		return b, true
	case "blockquote":
		return policydocs.Block{Type: "blockquote", Blocks: toBlocks(n.Content, facts)}, true
	case "callout":
		return policydocs.Block{Type: "callout", Kind: n.Attr("kind"), Blocks: toBlocks(n.Content, facts)}, true
	}
	return policydocs.Block{}, false
}

func toRuns(content []*Node, facts map[string]string) []policydocs.Run {
	out := []policydocs.Run{}
	for _, n := range content {
		switch n.Type {
		case "text":
			r := policydocs.Run{Text: n.Text}
			for _, m := range n.Marks {
				switch m.Type {
				case "bold", "italic", "code":
					r.Marks = append(r.Marks, m.Type)
				case "link":
					r.Href, _ = m.Attrs["href"].(string)
				}
			}
			out = append(out, r)
		case "hardBreak":
			out = append(out, policydocs.Run{Break: true})
		case "factToken":
			key := n.Attr("key")
			v, ok := facts[key]
			r := policydocs.Run{Text: v, Fact: &policydocs.RunFact{Key: key, Value: v, Unresolved: !ok}}
			if !ok {
				r.Text = UnresolvedPlaceholder(key)
			}
			out = append(out, r)
		case "controlRef":
			id := n.Attr("controlId")
			out = append(out, policydocs.Run{Text: id, Control: &policydocs.RunControl{ID: id}})
		}
	}
	return out
}
