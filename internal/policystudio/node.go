package policystudio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Node is a ProseMirror node. Its JSON is exactly what the editor's
// node.toJSON() produces, so the two can be compared byte for byte after
// normalisation.
type Node struct {
	Type    string
	Attrs   map[string]any
	Content []*Node
	Marks   []Mark
	Text    string
}

// Mark is a ProseMirror mark.
type Mark struct {
	Type  string
	Attrs map[string]any
}

func (n *Node) IsText() bool { return n.Type == "text" }

// Attr returns an attribute as a string ("" when absent or not a string).
func (n *Node) Attr(name string) string {
	s, _ := n.Attrs[name].(string)
	return s
}

// TextContent is the concatenated text of the node and its descendants.
func (n *Node) TextContent() string {
	if n.IsText() {
		return n.Text
	}
	var b bytes.Buffer
	for _, c := range n.Content {
		b.WriteString(c.TextContent())
	}
	return b.String()
}

// HasMark reports whether the node carries a mark of the given type.
func (n *Node) HasMark(name string) (Mark, bool) {
	for _, m := range n.Marks {
		if m.Type == name {
			return m, true
		}
	}
	return Mark{}, false
}

// MarshalJSON writes the node the way ProseMirror's toJSON does: attrs only
// when the type declares any, content and marks only when non-empty, and the
// attrs in declaration order.
func (n *Node) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"type":`)
	writeJSON(&b, n.Type)
	if spec, ok := schema.nodes[n.Type]; ok && len(spec.attrOrder) > 0 {
		b.WriteString(`,"attrs":`)
		if err := writeOrdered(&b, spec.attrOrder, n.Attrs); err != nil {
			return nil, err
		}
	}
	if len(n.Content) > 0 {
		b.WriteString(`,"content":`)
		raw, err := json.Marshal(n.Content)
		if err != nil {
			return nil, err
		}
		b.Write(raw)
	}
	if len(n.Marks) > 0 {
		b.WriteString(`,"marks":`)
		raw, err := json.Marshal(n.Marks)
		if err != nil {
			return nil, err
		}
		b.Write(raw)
	}
	if n.IsText() {
		b.WriteString(`,"text":`)
		writeJSON(&b, n.Text)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (m Mark) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"type":`)
	writeJSON(&b, m.Type)
	if spec, ok := schema.marks[m.Type]; ok && len(spec.attrOrder) > 0 {
		b.WriteString(`,"attrs":`)
		if err := writeOrdered(&b, spec.attrOrder, m.Attrs); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func writeJSON(b *bytes.Buffer, v any) {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	// Encode appends a newline.
	b.Truncate(b.Len() - 1)
}

func writeOrdered(b *bytes.Buffer, order []string, values map[string]any) error {
	b.WriteByte('{')
	for i, k := range order {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSON(b, k)
		b.WriteByte(':')
		writeJSON(b, values[k])
	}
	b.WriteByte('}')
	return nil
}

type rawNode struct {
	Type    string          `json:"type"`
	Attrs   map[string]any  `json:"attrs"`
	Content []*Node         `json:"content"`
	Marks   []Mark          `json:"marks"`
	Text    *string         `json:"text"`
	Extra   json.RawMessage `json:"-"`
}

// UnmarshalJSON reads ProseMirror JSON. It does not validate; Validate does.
func (n *Node) UnmarshalJSON(data []byte) error {
	var r rawNode
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	n.Type, n.Attrs, n.Content, n.Marks = r.Type, r.Attrs, r.Content, r.Marks
	if r.Text != nil {
		n.Text = *r.Text
	}
	return nil
}

func (m *Mark) UnmarshalJSON(data []byte) error {
	var r struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	m.Type, m.Attrs = r.Type, r.Attrs
	return nil
}

// ParseDoc reads and validates a ProseMirror document.
func ParseDoc(data []byte) (*Node, error) {
	var n Node
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, fmt.Errorf("parse document: %w", err)
	}
	if err := Validate(&n); err != nil {
		return nil, err
	}
	return &n, nil
}

// Validate checks a node tree against the schema and fills attribute
// defaults, the way ProseMirror's schema.node would. It is the gate every
// piece of content passes before the server stores, projects or renders it.
func Validate(n *Node) error {
	spec, ok := schema.nodes[n.Type]
	if !ok {
		return fmt.Errorf("node type %q is not allowed", n.Type)
	}
	attrs, err := completeAttrs(n.Type, spec.attrs, n.Attrs)
	if err != nil {
		return err
	}
	n.Attrs = attrs
	if n.IsText() {
		if n.Text == "" {
			return fmt.Errorf("empty text node")
		}
		if len(n.Content) > 0 {
			return fmt.Errorf("text node with content")
		}
	} else if n.Text != "" {
		return fmt.Errorf("%s: text on a non-text node", n.Type)
	}
	if spec.atom && len(n.Content) > 0 {
		return fmt.Errorf("%s: an atom has no content", n.Type)
	}

	var names bytes.Buffer
	for _, c := range n.Content {
		if err := Validate(c); err != nil {
			return err
		}
		names.WriteString(c.Type)
		names.WriteByte(' ')
		for i := range c.Marks {
			if !schema.allowsMark(spec, c.Marks[i].Type) {
				return fmt.Errorf("mark %s is not allowed inside %s", c.Marks[i].Type, n.Type)
			}
		}
	}
	if !n.IsText() && !schema.content[n.Type].MatchString(names.String()) {
		return fmt.Errorf("%s: content does not match %q", n.Type, spec.content)
	}
	return validateMarks(n)
}

func validateMarks(n *Node) error {
	for i := range n.Marks {
		m := &n.Marks[i]
		spec, ok := schema.marks[m.Type]
		if !ok {
			return fmt.Errorf("mark type %q is not allowed", m.Type)
		}
		attrs, err := completeAttrs(m.Type, spec.attrs, m.Attrs)
		if err != nil {
			return err
		}
		m.Attrs = attrs
		for j := range n.Marks {
			if i != j && schema.excludes(m.Type, n.Marks[j].Type) {
				return fmt.Errorf("marks %s and %s cannot be combined", m.Type, n.Marks[j].Type)
			}
		}
	}
	sortMarks(n.Marks)
	return nil
}

func completeAttrs(owner string, spec map[string]attrSpec, given map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(spec))
	for name, a := range spec {
		v, ok := given[name]
		if !ok {
			v = a.def
		}
		if a.check != nil {
			if err := a.check(v); err != nil {
				return nil, fmt.Errorf("%s.%s %v", owner, name, err)
			}
		}
		out[name] = v
	}
	return out, nil
}

// sortMarks orders marks by schema rank, as ProseMirror's Mark.setFrom does.
// Two marks of one type (only modification can repeat) are ordered by their
// JSON: ygo returns text attributes as a map, so the order Yjs stored them in
// is not recoverable, and it carries no meaning.
func sortMarks(marks []Mark) {
	sort.SliceStable(marks, func(i, j int) bool {
		ri, rj := schema.rank[marks[i].Type], schema.rank[marks[j].Type]
		if ri != rj {
			return ri < rj
		}
		a, _ := json.Marshal(marks[i])
		b, _ := json.Marshal(marks[j])
		return string(a) < string(b)
	})
}
