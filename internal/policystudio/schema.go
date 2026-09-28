package policystudio

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"grc/internal/policydocs"
)

// Fragment is the Y.XmlFragment the editor binds to and the server reads. The
// editor's src/schema.js exports the same constant; changing either orphans
// every stored document.
const Fragment = "policy"

// NodeMarksAttr is the reserved Y.XmlElement attribute in which the patched
// y-tiptap binding (web/policy-studio/patches) stores a node's marks, as a JSON
// string. Upstream bindings drop node marks, which would lose every whole-block
// suggestion; see POLICY_STUDIO.md §3.3.
const NodeMarksAttr = "__marks"

const suggestionMarks = "insertion deletion modification"

// attrRule validates one attribute value. v has already been defaulted.
type attrRule func(v any) error

type attrSpec struct {
	def   any
	check attrRule
}

type nodeSpec struct {
	name    string
	content string
	group   string
	inline  bool
	atom    bool
	// marks is ProseMirror's NodeSpec.marks: nil means the default ("all" for
	// a node with inline content, none otherwise).
	marks *string
	attrs map[string]attrSpec
	// attrOrder is the declaration order, which is also the order the editor's
	// JSON lists them in.
	attrOrder []string
}

type markSpec struct {
	name      string
	excludes  *string
	inclusive *bool
	attrs     map[string]attrSpec
	attrOrder []string
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

var (
	bidPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	factKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	controlPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._()/-]{0,39}$`)
)

func optionalString(max int) attrRule {
	return func(v any) error {
		if v == nil {
			return nil
		}
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		if utf8.RuneCountInString(s) > max {
			return fmt.Errorf("longer than %d characters", max)
		}
		return nil
	}
}

func matching(re *regexp.Regexp, nullable bool) attrRule {
	return func(v any) error {
		if v == nil && nullable {
			return nil
		}
		s, ok := v.(string)
		if !ok || !re.MatchString(s) {
			return fmt.Errorf("has an invalid value")
		}
		return nil
	}
}

func oneOf(values ...any) attrRule {
	return func(v any) error {
		for _, want := range values {
			if equalScalar(v, want) {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", values)
	}
}

func mustBeNull(v any) error {
	if v != nil {
		return fmt.Errorf("is not supported")
	}
	return nil
}

func positiveInt(max int) attrRule {
	return func(v any) error {
		n, ok := asInt(v)
		if !ok || n < 1 || n > max {
			return fmt.Errorf("must be a whole number from 1 to %d", max)
		}
		return nil
	}
}

func anyScalar(v any) error {
	switch x := v.(type) {
	case nil, bool, float64, int, int64:
		return nil
	case string:
		if utf8.RuneCountInString(x) > 2000 {
			return fmt.Errorf("is too long")
		}
		return nil
	}
	return fmt.Errorf("must be a scalar")
}

// linkHref allows https, http and mailto only. Anything else -- javascript:,
// data:, a relative path -- is refused rather than rewritten.
func linkHref(v any) error {
	s, ok := v.(string)
	if !ok || s == "" || len(s) > 2048 {
		return fmt.Errorf("must be a link of at most 2048 characters")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("contains control characters")
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("is not a valid URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		if u.Host == "" {
			return fmt.Errorf("has no host")
		}
		return nil
	case "mailto":
		return nil
	}
	return fmt.Errorf("must use https, http or mailto")
}

func sectionKind(v any) error {
	s, _ := v.(string)
	for _, k := range policydocs.SectionKinds {
		if s == k {
			return nil
		}
	}
	return fmt.Errorf("is not a known section kind")
}

func attrs(pairs ...any) (map[string]attrSpec, []string) {
	out := map[string]attrSpec{}
	var order []string
	for i := 0; i+2 < len(pairs); i += 3 {
		name := pairs[i].(string)
		out[name] = attrSpec{def: pairs[i+1], check: pairs[i+2].(attrRule)}
		order = append(order, name)
	}
	return out, order
}

func node(name, content, group string, inline, atom bool, marks *string, attrPairs ...any) nodeSpec {
	a, order := attrs(attrPairs...)
	return nodeSpec{name: name, content: content, group: group, inline: inline, atom: atom, marks: marks, attrs: a, attrOrder: order}
}

func mark(name string, excludes *string, inclusive *bool, attrPairs ...any) markSpec {
	a, order := attrs(attrPairs...)
	return markSpec{name: name, excludes: excludes, inclusive: inclusive, attrs: a, attrOrder: order}
}

var bid = []any{"bid", nil, matching(bidPattern, true)}

func withBid(rest ...any) []any { return append(append([]any{}, bid...), rest...) }

func suggestionAttrs(extra ...any) []any {
	base := []any{
		"id", nil, optionalString(64),
		"authorId", nil, optionalString(200),
		"authorKind", nil, oneOf(nil, "human", "ai", "guest"),
		"authorName", nil, optionalString(200),
		"proposalId", nil, optionalString(64),
		"createdAt", nil, optionalString(40),
	}
	return append(base, extra...)
}

// The schema, in the order web/policy-studio/src/schema.js declares it (the
// order is also mark rank). TestSchemaMatchesEditorSnapshot fails when this
// drifts from the editor's schema.snapshot.json.
var nodeSpecs = []nodeSpec{
	node("paragraph", "inline*", "block", false, false, nil, withBid()...),
	node("doc", "policySection+", "", false, false, nil),
	node("policySection", "sectionHeading block+", "", false, false, strPtr(suggestionMarks),
		"uid", nil, matching(bidPattern, false),
		"kind", "other", attrRule(sectionKind)),
	node("sectionHeading", "text*", "", false, false, strPtr(suggestionMarks), withBid()...),
	node("text", "", "inline", false, false, nil),
	node("hardBreak", "", "inline", true, false, nil),
	node("heading", "inline*", "block", false, false, nil, withBid("level", 1, oneOf(2, 3))...),
	node("blockquote", "paragraph+", "block", false, false, strPtr(suggestionMarks), withBid()...),
	node("bulletList", "listItem+", "block list", false, false, strPtr(suggestionMarks)),
	node("orderedList", "listItem+", "block list", false, false, strPtr(suggestionMarks),
		"start", 1, positiveInt(10000),
		"type", nil, attrRule(mustBeNull)),
	node("listItem", "paragraph (paragraph | bulletList | orderedList)*", "", false, false, strPtr(suggestionMarks), withBid()...),
	node("table", "tableRow+", "block", false, false, strPtr(suggestionMarks)),
	node("tableRow", "(tableCell | tableHeader)*", "", false, false, strPtr(suggestionMarks)),
	node("tableHeader", "paragraph+", "", false, false, strPtr(suggestionMarks),
		withBid("colspan", 1, oneOf(1), "rowspan", 1, oneOf(1), "colwidth", nil, attrRule(mustBeNull))...),
	node("tableCell", "paragraph+", "", false, false, strPtr(suggestionMarks),
		withBid("colspan", 1, oneOf(1), "rowspan", 1, oneOf(1), "colwidth", nil, attrRule(mustBeNull))...),
	node("callout", "paragraph+", "block", false, false, strPtr(suggestionMarks), withBid("kind", "note", oneOf("note", "important"))...),
	node("factToken", "", "inline", true, true, nil, "key", "", matching(factKeyPattern, false)),
	node("controlRef", "", "inline", true, true, nil, "controlId", "", matching(controlPattern, false)),
}

var markSpecs = []markSpec{
	mark("link", nil, boolPtr(false),
		"href", nil, attrRule(linkHref),
		"target", nil, attrRule(mustBeNull),
		"rel", "noopener noreferrer", oneOf(nil, "noopener noreferrer"),
		"class", nil, attrRule(mustBeNull),
		"title", nil, optionalString(200)),
	mark("bold", nil, nil),
	mark("italic", nil, nil),
	mark("code", strPtr("code bold italic link"), nil),
	mark("insertion", strPtr("deletion modification insertion"), boolPtr(false), suggestionAttrs()...),
	mark("deletion", strPtr("insertion modification deletion"), boolPtr(false), suggestionAttrs()...),
	mark("modification", strPtr("deletion insertion"), boolPtr(false), suggestionAttrs(
		"type", nil, optionalString(40),
		"attrName", nil, optionalString(40),
		"previousValue", nil, attrRule(anyScalar),
		"newValue", nil, attrRule(anyScalar))...),
}

// Schema is the compiled form of nodeSpecs and markSpecs.
type Schema struct {
	nodes   map[string]*nodeSpec
	marks   map[string]*markSpec
	rank    map[string]int
	groups  map[string][]string
	content map[string]*regexp.Regexp
}

// schema is built once; the specs are fixed at compile time.
var schema = mustCompileSchema()

func mustCompileSchema() *Schema {
	s := &Schema{
		nodes:   map[string]*nodeSpec{},
		marks:   map[string]*markSpec{},
		rank:    map[string]int{},
		groups:  map[string][]string{},
		content: map[string]*regexp.Regexp{},
	}
	for i := range nodeSpecs {
		n := &nodeSpecs[i]
		s.nodes[n.name] = n
		for _, g := range strings.Fields(n.group) {
			s.groups[g] = append(s.groups[g], n.name)
		}
	}
	for i := range markSpecs {
		s.marks[markSpecs[i].name] = &markSpecs[i]
		s.rank[markSpecs[i].name] = i
	}
	for _, n := range nodeSpecs {
		s.content[n.name] = regexp.MustCompile("^" + s.contentRegexp(n.content) + "$")
	}
	return s
}

// contentRegexp turns a ProseMirror content expression into a regular
// expression over the space-terminated list of child type names. It handles the
// subset the schema uses: names, groups, parenthesised alternatives and the
// ? * + quantifiers.
var contentToken = regexp.MustCompile(`\(|\)|\||[?*+]|[A-Za-z]+`)

func (s *Schema) contentRegexp(expr string) string {
	var b strings.Builder
	for _, tok := range contentToken.FindAllString(expr, -1) {
		switch tok {
		case "(":
			b.WriteString("(?:")
		case ")":
			b.WriteString(")")
		case "|", "?", "*", "+":
			b.WriteString(tok)
		default:
			names := []string{tok}
			if members, ok := s.groups[tok]; ok {
				names = members
			}
			b.WriteString("(?:(?:" + strings.Join(names, "|") + ") )")
		}
	}
	return b.String()
}

// allowsMark reports whether a child of parent may carry mark.
func (s *Schema) allowsMark(parent *nodeSpec, markName string) bool {
	if parent.marks == nil {
		return s.hasInlineContent(parent)
	}
	switch *parent.marks {
	case "_":
		return true
	case "":
		return false
	}
	for _, m := range strings.Fields(*parent.marks) {
		if m == markName {
			return true
		}
	}
	return false
}

func (s *Schema) hasInlineContent(n *nodeSpec) bool {
	return strings.Contains(n.content, "inline") || strings.Contains(n.content, "text")
}

// excludes reports whether mark a excludes mark b.
func (s *Schema) excludes(a, b string) bool {
	spec := s.marks[a]
	if spec.excludes == nil {
		return a == b
	}
	for _, name := range strings.Fields(*spec.excludes) {
		if name == "_" || name == b {
			return true
		}
	}
	return false
}

// sortedMarkNames is for error messages.
func (s *Schema) sortedMarkNames() []string {
	out := make([]string, 0, len(s.marks))
	for name := range s.marks {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func equalScalar(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if ai, ok := asInt(a); ok {
		bi, ok := asInt(b)
		return ok && ai == bi
	}
	return a == b
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
	}
	return 0, false
}
