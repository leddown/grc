package policystudio

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/reearth/ygo/crdt"
)

// hashedMarkKey matches the key y-tiptap gives a mark whose type does not
// exclude itself, so several can coexist: "<name>--<8 base64 characters>".
var hashedMarkKey = regexp.MustCompile(`^(.*)(--[a-zA-Z0-9+/=]{8})$`)

func markNameOf(attrKey string) string {
	if m := hashedMarkKey.FindStringSubmatch(attrKey); m != nil {
		return m[1]
	}
	return attrKey
}

type xmlChild interface{ ToXML() string }

// ReadDoc reads the document's fragment the way @tiptap/y-tiptap's
// yXmlFragmentToProseMirrorRootNode does and returns the ProseMirror tree.
//
// Content that does not fit the schema is dropped section by section rather
// than failing the whole read: the editor binding deletes nodes it cannot
// build, and a client that writes something the schema forbids must not be
// able to stop the projection of everything else. Each dropped section is
// reported in the returned problems, keyed by section uid when it has one.
func ReadDoc(doc *crdt.Doc) (*Node, []Problem) {
	root := &Node{Type: "doc"}
	var problems []Problem
	for _, r := range readSections(doc) {
		if r.Problem != nil {
			problems = append(problems, *r.Problem)
			continue
		}
		root.Content = append(root.Content, r.Node)
	}
	return root, problems
}

// sectionRead is one top-level element of the fragment: a valid section, or
// the reason it was refused. Keeping both in document order lets the
// projection hold a refused section's place.
type sectionRead struct {
	Node    *Node
	Problem *Problem
}

func readSections(doc *crdt.Doc) []sectionRead {
	frag := doc.GetXmlFragment(Fragment)
	var out []sectionRead
	for _, c := range frag.Children() {
		el, ok := c.(*crdt.YXmlElement)
		if !ok {
			out = append(out, sectionRead{Problem: &Problem{Message: "text directly inside the document"}})
			continue
		}
		n, err := readElement(el)
		if err == nil && n.Type != "policySection" {
			err = fmt.Errorf("%s directly inside the document", n.Type)
		}
		if err == nil {
			err = Validate(n)
		}
		if err != nil {
			uid, _ := el.GetAttribute("uid")
			out = append(out, sectionRead{Problem: &Problem{SectionUID: uid, Message: err.Error()}})
			continue
		}
		out = append(out, sectionRead{Node: n})
	}
	return out
}

// Problem is content the server refused.
type Problem struct {
	SectionUID string `json:"section_uid,omitempty"`
	Message    string `json:"message"`
}

func readChildren(children []xmlChild) ([]*Node, error) {
	var out []*Node
	for _, child := range children {
		switch c := child.(type) {
		case *crdt.YXmlElement:
			n, err := readElement(c)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		case *crdt.YXmlText:
			texts, err := readText(c)
			if err != nil {
				return nil, err
			}
			for _, t := range texts {
				out = appendText(out, t)
			}
		default:
			return nil, fmt.Errorf("unexpected Yjs type %T", child)
		}
	}
	return out, nil
}

// appendText joins a text node onto the previous one when their marks are
// equal, as ProseMirror's Fragment.fromArray does.
func appendText(out []*Node, t *Node) []*Node {
	if n := len(out); n > 0 && t.IsText() && out[n-1].IsText() && sameMarks(out[n-1].Marks, t.Marks) {
		out[n-1].Text += t.Text
		return out
	}
	return append(out, t)
}

func sameMarks(a, b []Mark) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func readElement(el *crdt.YXmlElement) (*Node, error) {
	if _, ok := schema.nodes[el.NodeName]; !ok {
		return nil, fmt.Errorf("node type %q is not allowed", el.NodeName)
	}
	raw := el.GetAttributeValues()
	n := &Node{Type: el.NodeName, Attrs: map[string]any{}}
	if encoded, ok := raw[NodeMarksAttr].(string); ok && encoded != "" {
		var marks []Mark
		if err := json.Unmarshal([]byte(encoded), &marks); err != nil {
			return nil, fmt.Errorf("%s: unreadable node marks: %w", el.NodeName, err)
		}
		n.Marks = marks
	}
	for k, v := range raw {
		if k == NodeMarksAttr || k == "ychange" {
			continue
		}
		n.Attrs[k] = v
	}
	var children []xmlChild
	for _, c := range el.Children() {
		children = append(children, c)
	}
	content, err := readChildren(children)
	if err != nil {
		return nil, err
	}
	n.Content = content
	return n, nil
}

func readText(t *crdt.YXmlText) ([]*Node, error) {
	var out []*Node
	for _, d := range t.ToDelta() {
		text, ok := d.Insert.(string)
		if !ok {
			return nil, fmt.Errorf("embedded content in text is not allowed")
		}
		if text == "" {
			continue
		}
		n := &Node{Type: "text", Text: text}
		for key, value := range d.Attributes {
			attrs, _ := value.(map[string]any)
			n.Marks = append(n.Marks, Mark{Type: markNameOf(key), Attrs: attrs})
		}
		out = append(out, n)
	}
	return out, nil
}

// WriteDoc writes a validated document into an empty fragment in one
// transaction, building the Y XML y-tiptap's prosemirrorJSONToYXmlFragment
// builds. It refuses a fragment that already has content: two seeds of one
// room would duplicate the document, and the server is the only seeder.
func WriteDoc(doc *crdt.Doc, root *Node) error {
	if root.Type != "doc" {
		return fmt.Errorf("seed: top node must be doc")
	}
	if err := Validate(root); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	frag := doc.GetXmlFragment(Fragment)
	if frag.Len() > 0 {
		return fmt.Errorf("seed: document already has content")
	}
	built := make([]*crdt.YXmlElement, 0, len(root.Content))
	for _, c := range root.Content {
		el, err := buildElement(c)
		if err != nil {
			return err
		}
		built = append(built, el)
	}
	doc.Transact(func(txn *crdt.Transaction) {
		for i, el := range built {
			frag.InsertElement(txn, i, el)
		}
	})
	return nil
}

// buildElement builds a detached Y element for n. Attributes go on after the
// children, the order Yjs integrates a prelim element in.
func buildElement(n *Node) (*crdt.YXmlElement, error) {
	el := crdt.NewYXmlElement(n.Type)
	children, err := buildChildren(n.Content)
	if err != nil {
		return nil, err
	}
	for i, c := range children {
		switch v := c.(type) {
		case *crdt.YXmlElement:
			el.InsertElement(nil, i, v)
		case *crdt.YXmlText:
			el.InsertText(nil, i, v)
		}
	}
	for _, k := range schema.nodes[n.Type].attrOrder {
		if v := n.Attrs[k]; v != nil {
			el.SetAttributeValue(nil, k, v)
		}
	}
	if len(n.Marks) > 0 {
		raw, err := json.Marshal(n.Marks)
		if err != nil {
			return nil, err
		}
		el.SetAttribute(nil, NodeMarksAttr, string(raw))
	}
	return el, nil
}

// buildChildren groups consecutive text nodes into one Y.XmlText, as
// y-tiptap's normalizePNodeContent does.
func buildChildren(content []*Node) ([]any, error) {
	var out []any
	var run []crdt.Delta
	flush := func() {
		if len(run) == 0 {
			return
		}
		t := crdt.NewYXmlText()
		t.ApplyDelta(nil, run)
		out = append(out, t)
		run = nil
	}
	for _, c := range content {
		if c.IsText() {
			run = append(run, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: c.Text, Attributes: textAttributes(c.Marks)})
			continue
		}
		flush()
		el, err := buildElement(c)
		if err != nil {
			return nil, err
		}
		out = append(out, el)
	}
	flush()
	return out, nil
}

func textAttributes(marks []Mark) crdt.Attributes {
	if len(marks) == 0 {
		return nil
	}
	sorted := append([]Mark(nil), marks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, _ := json.Marshal(sorted[i])
		b, _ := json.Marshal(sorted[j])
		return string(a) < string(b)
	})
	attrs := crdt.Attributes{}
	for _, m := range sorted {
		key := m.Type
		if !schema.excludes(m.Type, m.Type) {
			// Any stable 8-character suffix will do: y-tiptap strips it by
			// pattern and never recomputes it.
			raw, _ := json.Marshal(m)
			sum := sha256.Sum256(raw)
			key = m.Type + "--" + base64.StdEncoding.EncodeToString(sum[:6])
		}
		values := map[string]any{}
		for k, v := range m.Attrs {
			values[k] = v
		}
		attrs[key] = values
	}
	return attrs
}
