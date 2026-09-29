package policystudio

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"grc/internal/policydocs"
)

func counter() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("b%d", n) }
}

func TestLegacyMarkdownBecomesValidSections(t *testing.T) {
	cases := map[string]struct {
		body string
		want []string // substrings of the resulting JSON
	}{
		"plain text keeps its line breaks": {"Line one\nLine two", []string{`"hardBreak"`, `"Line one"`, `"Line two"`}},
		"empty body":                       {"", []string{`"paragraph"`}},
		"emphasis and links":               {"Staff **must** review *access* at [the portal](https://example.com/p).", []string{`"bold"`, `"italic"`, `"href":"https://example.com/p"`}},
		"unsafe link keeps text only":      {"[click](javascript:alert(1))", []string{`"click"`}},
		"lists":                            {"- one\n- two\n  1. nested", []string{`"bulletList"`, `"orderedList"`}},
		"ordered start":                    {"3. three\n4. four", []string{`"start":3`}},
		"table":                            {"| Role | Duty |\n|---|---|\n| CISO | Owns |", []string{`"tableHeader"`, `"tableCell"`, `"CISO"`}},
		"heading levels clamp":             {"# Top\n\n#### Deep", []string{`"level":2`, `"level":3`}},
		"raw html is text":                 {"<script>alert(1)</script>", []string{`script`}},
		"code block keeps text":            {"```\nshow run\n```", []string{`"show run"`}},
		"unresolved placeholder":           {"Retention is [[UNRESOLVED: retention_period]].", []string{`"factToken"`, `"key":"retention_period"`}},
		"blockquote with a list":           {"> quoted\n>\n> - item", []string{`"blockquote"`, `"item"`}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sec := SectionFromMarkdown("s1", "purpose", "Purpose", tc.body, counter())
			doc := &Node{Type: "doc", Content: []*Node{sec}}
			if err := Validate(doc); err != nil {
				t.Fatalf("invalid: %v", err)
			}
			raw, _ := json.Marshal(doc)
			for _, w := range tc.want {
				if !strings.Contains(string(raw), w) {
					t.Errorf("missing %s in %s", w, raw)
				}
			}
			if strings.Contains(string(raw), "javascript:") {
				t.Errorf("unsafe href survived: %s", raw)
			}
		})
	}
}

func TestSectionMarkdownOfEveryFixture(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		root, err := ParseDoc(fx.Expected)
		if err != nil {
			t.Fatal(err)
		}
		for _, sec := range root.Content {
			md := SectionMarkdown(Resolve(sec, Baseline), nil)
			if fx.Name != "minimal" && md == "" {
				t.Errorf("%s: empty Markdown", fx.Name)
			}
			if strings.Contains(md, "​") {
				t.Errorf("%s: zero-width space in Markdown", fx.Name)
			}
		}
	}
}

func TestMarkdownRendering(t *testing.T) {
	root, err := ParseDoc([]byte(`{"type":"doc","content":[{"type":"policySection","attrs":{"uid":"s","kind":"scope"},"content":[
	{"type":"sectionHeading","content":[{"type":"text","text":"Scope"}]},
	{"type":"paragraph","content":[{"type":"text","text":"Applies to "},{"type":"factToken","attrs":{"key":"legal_entity_name"}},{"type":"text","text":" per "},{"type":"controlRef","attrs":{"controlId":"AC-1"}},{"type":"text","text":" *not* emphasis "},{"type":"text","text":"bold","marks":[{"type":"bold"}]}]},
	{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Sub"}]},
	{"type":"orderedList","attrs":{"start":2},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"inner"}]}]}]}]}]},
	{"type":"callout","attrs":{"kind":"important"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Read"}]}]},
	{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"a|b"}]}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"c"}]}]}]}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := SectionMarkdown(root.Content[0], nil)
	for _, want := range []string{
		"Applies to [[UNRESOLVED: legal_entity_name]] per AC-1 \\*not\\* emphasis **bold**",
		"### Sub",
		"2. two\n   - inner",
		"> **Important:** Read",
		"| a\\|b |\n|---|\n| c |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBaselineAndProposedViews(t *testing.T) {
	var fx fixture
	for _, f := range loadFixtures(t) {
		if f.Name == "suggestions" || f.Name == "blockSuggestions" {
			root, err := ParseDoc(f.Expected)
			if err != nil {
				t.Fatal(err)
			}
			base := Resolve(root, Baseline).TextContent()
			prop := Resolve(root, Proposed).TextContent()
			if policydocs.PendingSuggestions(string(f.Expected)) == 0 {
				t.Fatalf("%s: no pending suggestions counted", f.Name)
			}
			switch f.Name {
			case "suggestions":
				if !strings.Contains(base, "Staff willaccess") || strings.Contains(base, "must") {
					t.Errorf("baseline keeps deletions and drops insertions: %q", base)
				}
				if !strings.Contains(prop, "Staff must review access") || strings.Contains(prop, "will") {
					t.Errorf("proposed applies everything: %q", prop)
				}
			case "blockSuggestions":
				if strings.Contains(base, "A whole new paragraph") || !strings.Contains(base, "Going away") {
					t.Errorf("baseline block view wrong: %q", base)
				}
				if !strings.Contains(prop, "A whole new paragraph") || strings.Contains(prop, "Going away") {
					t.Errorf("proposed block view wrong: %q", prop)
				}
				var level any
				for _, n := range Resolve(root, Baseline).Content[0].Content {
					if n.Type == "heading" {
						level = n.Attrs["level"]
					}
				}
				if l, _ := asInt(level); l != 2 {
					t.Errorf("baseline must revert a suggested attribute change, level = %v", level)
				}
			}
			fx = f
		}
	}
	if fx.Name == "" {
		t.Fatal("suggestion fixtures missing")
	}
}

// Whatever a legacy body holds, migration must produce a valid section.
func FuzzSectionFromMarkdown(f *testing.F) {
	for _, s := range []string{"", "a\nb", "- x\n  - y\n    1. z", "| a |\n|---|\n| b |", "> q\n> ```\n> c\n> ```", "[[UNRESOLVED: k]] <b>x</b> ![i](http://x/y.png)", "***x***", "#\n##\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		sec := SectionFromMarkdown("s1", "purpose", "H", body, counter())
		if err := Validate(&Node{Type: "doc", Content: []*Node{sec}}); err != nil {
			t.Fatalf("invalid section from %q: %v", body, err)
		}
		_ = SectionMarkdown(sec, nil)
	})
}
