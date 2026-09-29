package policystudio

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
)

const webDir = "../../web/policy-studio"

var updateGolden = flag.Bool("update", false, "rewrite the Go-seeded fixtures in web/policy-studio/fixtures/go-seeded")

type snapshotAttr struct {
	Default any `json:"default"`
}

type snapshot struct {
	Fragment string `json:"fragment"`
	Nodes    []struct {
		Name    string                  `json:"name"`
		Content string                  `json:"content"`
		Group   string                  `json:"group"`
		Inline  bool                    `json:"inline"`
		Atom    bool                    `json:"atom"`
		Marks   *string                 `json:"marks"`
		Attrs   map[string]snapshotAttr `json:"attrs"`
	} `json:"nodes"`
	Marks []struct {
		Name      string                  `json:"name"`
		Rank      int                     `json:"rank"`
		Excludes  *string                 `json:"excludes"`
		Inclusive *bool                   `json:"inclusive"`
		Attrs     map[string]snapshotAttr `json:"attrs"`
	} `json:"marks"`
}

// The Go allowlist and the editor's schema are one contract. A node, mark,
// attribute or default added on either side without the other fails here.
func TestSchemaMatchesEditorSnapshot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(webDir, "schema.snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Fragment != Fragment {
		t.Errorf("fragment: editor %q, server %q", snap.Fragment, Fragment)
	}
	if len(snap.Nodes) != len(nodeSpecs) {
		t.Errorf("editor has %d node types, server %d", len(snap.Nodes), len(nodeSpecs))
	}
	for i, n := range snap.Nodes {
		if i >= len(nodeSpecs) {
			break
		}
		g := nodeSpecs[i]
		if g.name != n.Name || g.content != n.Content || g.group != n.Group || g.inline != n.Inline || g.atom != n.Atom {
			t.Errorf("node %d: editor %s{content %q group %q inline %v atom %v}, server %s{content %q group %q inline %v atom %v}",
				i, n.Name, n.Content, n.Group, n.Inline, n.Atom, g.name, g.content, g.group, g.inline, g.atom)
		}
		if (g.marks == nil) != (n.Marks == nil) || (g.marks != nil && *g.marks != *n.Marks) {
			t.Errorf("node %s: allowed marks differ", n.Name)
		}
		compareAttrs(t, "node "+n.Name, g.attrs, n.Attrs)
	}
	if len(snap.Marks) != len(markSpecs) {
		t.Errorf("editor has %d mark types, server %d", len(snap.Marks), len(markSpecs))
	}
	for i, m := range snap.Marks {
		if i >= len(markSpecs) {
			break
		}
		g := markSpecs[i]
		if g.name != m.Name || m.Rank != i {
			t.Errorf("mark %d: editor %s (rank %d), server %s", i, m.Name, m.Rank, g.name)
		}
		if (g.excludes == nil) != (m.Excludes == nil) || (g.excludes != nil && *g.excludes != *m.Excludes) {
			t.Errorf("mark %s: excludes differ", m.Name)
		}
		if (g.inclusive == nil) != (m.Inclusive == nil) || (g.inclusive != nil && *g.inclusive != *m.Inclusive) {
			t.Errorf("mark %s: inclusive differs", m.Name)
		}
		compareAttrs(t, "mark "+m.Name, g.attrs, m.Attrs)
	}
}

func compareAttrs(t *testing.T, owner string, got map[string]attrSpec, want map[string]snapshotAttr) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: editor attrs %v, server has %d", owner, keysOf(want), len(got))
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s: server lacks attribute %s", owner, name)
			continue
		}
		if !equalScalar(g.def, w.Default) {
			t.Errorf("%s.%s: default editor %v, server %v", owner, name, w.Default, g.def)
		}
	}
}

func keysOf(m map[string]snapshotAttr) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type fixture struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Update   string          `json:"update"`
	Expected json.RawMessage `json:"expected"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(webDir, "fixtures", "cases", "*.json"))
	if len(files) == 0 {
		t.Fatal("no fixtures: run scripts/build-policy-studio.sh")
	}
	var out []fixture
	for _, f := range files {
		raw, err := os.ReadFile(f) // #nosec G304 -- the repository's own fixtures
		if err != nil {
			t.Fatal(err)
		}
		var fx fixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatal(err)
		}
		out = append(out, fx)
	}
	return out
}

// canonicalJSON parses ProseMirror JSON and re-encodes it with the tie-break
// sortMarks applies to repeated marks, so the JS and Go sides compare equal.
func canonicalJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var n Node
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	var walk func(*Node)
	walk = func(x *Node) {
		sortMarks(x.Marks)
		for _, c := range x.Content {
			walk(c)
		}
	}
	walk(&n)
	b, err := json.Marshal(&n)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func asAny(t *testing.T, n *Node) any {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Go reads the Y XML the pinned editor binding writes exactly as JS does.
func TestReadDocMatchesEditor(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			update, err := base64.StdEncoding.DecodeString(fx.Update)
			if err != nil {
				t.Fatal(err)
			}
			doc := crdt.New()
			if err := crdt.ApplyUpdateV1(doc, update, nil); err != nil {
				t.Fatal(err)
			}
			got, problems := ReadDoc(doc)
			if len(problems) > 0 {
				t.Fatalf("problems: %+v", problems)
			}
			if want := canonicalJSON(t, fx.Expected); !reflect.DeepEqual(asAny(t, got), want) {
				g, _ := json.Marshal(got)
				t.Fatalf("differs from the editor\n got: %s\nwant: %s", g, fx.Expected)
			}
		})
	}
}

// Go-seeded documents are deterministic, read back identically, and are the
// exact bytes the editor verified (fixtures/go-seeded/js-verified.json, written
// by `node fixtures/gen.mjs verify`). Run with -update after a schema change,
// then re-run the JS verification.
func TestSeededDocumentsAreTheOnesTheEditorVerified(t *testing.T) {
	dir := filepath.Join(webDir, "fixtures", "go-seeded")
	raw, err := os.ReadFile(filepath.Join(dir, "js-verified.json"))
	if err != nil && !*updateGolden {
		t.Fatal(err)
	}
	verified := map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &verified); err != nil {
			t.Fatal(err)
		}
	}
	for _, fx := range loadFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			root, err := ParseDoc(fx.Input)
			if err != nil {
				t.Fatal(err)
			}
			doc := crdt.New(crdt.WithClientID(1))
			if err := WriteDoc(doc, root); err != nil {
				t.Fatal(err)
			}
			if err := WriteDoc(doc, root); err == nil {
				t.Fatal("a second seed into a non-empty document must be refused")
			}
			back, problems := ReadDoc(doc)
			if len(problems) > 0 {
				t.Fatalf("problems: %+v", problems)
			}
			if want := canonicalJSON(t, fx.Expected); !reflect.DeepEqual(asAny(t, back), want) {
				t.Fatal("Go does not read its own seed back")
			}
			update := crdt.EncodeStateAsUpdateV1(doc, nil)
			path := filepath.Join(dir, fx.Name+".json")
			if *updateGolden {
				payload, _ := json.Marshal(map[string]string{"update": base64.StdEncoding.EncodeToString(update)})
				if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			sum := sha256.Sum256(update)
			if got := hex.EncodeToString(sum[:]); got != verified[fx.Name] {
				t.Fatalf("the seed of %s is not the update the editor verified (%s vs %s); run go test -update, then node fixtures/gen.mjs verify",
					fx.Name, got, verified[fx.Name])
			}
		})
	}
}

func TestValidateRefusesWhatTheSchemaDoesNot(t *testing.T) {
	section := func(blocks string) string {
		return `{"type":"doc","content":[{"type":"policySection","attrs":{"uid":"s1","kind":"purpose"},"content":[` +
			`{"type":"sectionHeading","content":[{"type":"text","text":"H"}]},` + blocks + `]}]}`
	}
	cases := map[string]string{
		"script node":           section(`{"type":"script","content":[{"type":"text","text":"x"}]}`),
		"image":                 section(`{"type":"image","attrs":{"src":"https://x/y.png"}}`),
		"javascript link":       section(`{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}]}`),
		"data link":             section(`{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"data:text/html,x"}}]}]}`),
		"relative link":         section(`{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"/admin"}}]}]}`),
		"link target":           section(`{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"https://a.b","target":"_blank"}}]}]}`),
		"heading level 1":       section(`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"x"}]}`),
		"merged cell":           section(`{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","attrs":{"colspan":2},"content":[{"type":"paragraph"}]}]}]}`),
		"unknown callout kind":  section(`{"type":"callout","attrs":{"kind":"danger"},"content":[{"type":"paragraph"}]}`),
		"marks on heading text": `{"type":"doc","content":[{"type":"policySection","attrs":{"uid":"s1","kind":"purpose"},"content":[{"type":"sectionHeading","content":[{"type":"text","text":"H","marks":[{"type":"bold"}]}]},{"type":"paragraph"}]}]}`,
		"section without body":  `{"type":"doc","content":[{"type":"policySection","attrs":{"uid":"s1","kind":"purpose"},"content":[{"type":"sectionHeading"}]}]}`,
		"unknown section kind":  `{"type":"doc","content":[{"type":"policySection","attrs":{"uid":"s1","kind":"shell"},"content":[{"type":"sectionHeading"},{"type":"paragraph"}]}]}`,
		"section without uid":   `{"type":"doc","content":[{"type":"policySection","attrs":{"kind":"purpose"},"content":[{"type":"sectionHeading"},{"type":"paragraph"}]}]}`,
		"bad fact key":          section(`{"type":"paragraph","content":[{"type":"factToken","attrs":{"key":"Robert'); DROP"}}]}`),
		"block in paragraph":    section(`{"type":"paragraph","content":[{"type":"paragraph"}]}`),
		"table in list item":    section(`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph"},{"type":"table","content":[]}]}]}`),
		"bold with code":        section(`{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"bold"},{"type":"code"}]}]}`),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDoc([]byte(doc)); err == nil {
				t.Fatalf("accepted: %s", doc)
			}
		})
	}
	if _, err := ParseDoc([]byte(section(`{"type":"paragraph","content":[{"type":"text","text":"ok","marks":[{"type":"link","attrs":{"href":"mailto:ciso@example.com"}}]}]}`))); err != nil {
		t.Fatalf("refused a valid document: %v", err)
	}
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{
		`{"type":"doc","content":[{"type":"policySection","attrs":{"uid":"a","kind":"scope"},"content":[{"type":"sectionHeading"},{"type":"paragraph"}]}]}`,
		`{"type":"text","text":"<script>"}`,
		`{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"https://a"}}]}]}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := ParseDoc([]byte(s))
		if err != nil {
			return
		}
		// Whatever validates must re-validate from its own JSON.
		raw, err := json.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseDoc(raw); err != nil && !strings.Contains(s, "\x00") {
			t.Fatalf("round trip broke validation: %v\n%s", err, raw)
		}
	})
}
