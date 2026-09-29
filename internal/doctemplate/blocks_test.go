package doctemplate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var hostile = []string{
	`#panic("pwned")`, `#read("/etc/passwd")`, `#import "/etc/passwd"`, `\input{/etc/passwd}`, `\write18{rm -rf /}`,
	`}{`, `$x^2$`, `%comment`, `# _ & ~ ^`, `]`, `[`, `*not bold*`, `<label>`, `@ref`, "`raw`", `\`, `\\`,
}

func hostileSection() PolicySection {
	var runs []PolicyRun
	for _, h := range hostile {
		runs = append(runs, PolicyRun{Text: h}, PolicyRun{Text: h, Marks: []string{"bold", "italic", "code"}},
			PolicyRun{Text: h, Href: `https://example.com/a{b}c%d#e\f g^h~i`}, PolicyRun{Text: h, Href: `javascript:alert(1)`},
			PolicyRun{Text: h, Fact: &PolicyRunFact{Key: "k", Unresolved: true}}, PolicyRun{Break: true})
	}
	p := PolicyBlock{Type: "paragraph", Runs: runs}
	return PolicySection{Heading: `#panic("heading") \input{x}`, Body: "body", Controls: []PolicyClaim{}, Blocks: []PolicyBlock{
		p, {Type: "heading", Level: 3, Runs: runs},
		{Type: "bullet_list", Items: [][]PolicyBlock{{p}}}, {Type: "ordered_list", Start: 4, Items: [][]PolicyBlock{{p}}},
		{Type: "table", Rows: []PolicyRow{{Header: true, Cells: [][]PolicyBlock{{p}, {p}}}, {Cells: [][]PolicyBlock{{p}}}}},
		{Type: "blockquote", Blocks: []PolicyBlock{p}}, {Type: "callout", Kind: "important", Blocks: []PolicyBlock{p}},
	}}
}

// Every backslash the LaTeX generator emits for payload text is one of the
// escapes it knows; nothing from a policy reaches LaTeX as a command.
var latexEscapeSeq = regexp.MustCompile(`\\(textbackslash\{\}|textasciitilde\{\}|textasciicircum\{\}|[{}&%$#_])`)

func TestLaTeXBlocksEscapeHostileText(t *testing.T) {
	out := latexRichBlocks(hostileSection().Blocks)
	for _, bad := range []string{`\input{`, `\write18`, "javascript:"} {
		if strings.Contains(out, bad) {
			t.Errorf("generated LaTeX contains %q", bad)
		}
	}
	// Inside \href a literal % is written \%, so the percent-encoded characters
	// appear as \%7B and so on.
	if !strings.Contains(out, `\href{https://example.com/a\%7Bb\%7Dc\%d\#e\%5Cf\%20g\%5Eh\%7Ei}`) {
		t.Errorf("link target not escaped as expected:\n%s", out)
	}
	if !strings.Contains(out, `\setcounter{enumi}{3}`) {
		t.Error("ordered list start not carried")
	}
}

func FuzzLaTeXRuns(f *testing.F) {
	for _, h := range hostile {
		f.Add(h)
	}
	f.Fuzz(func(t *testing.T, text string) {
		out := latexRuns([]PolicyRun{{Text: text}})
		if rest := latexEscapeSeq.ReplaceAllString(out, ""); strings.Contains(rest, `\`) {
			t.Fatalf("an unescaped backslash from %q: %s", text, out)
		}
	})
}

// With a typst binary available (TYPST or PATH), the policy template compiles a
// payload full of Typst syntax: had any of it been evaluated, #panic would fail
// the compile and #read would reach outside the render root.
func TestTypstRendersHostileBlocksAsText(t *testing.T) {
	bin := os.Getenv("TYPST")
	if bin == "" {
		bin, _ = exec.LookPath("typst")
	}
	if bin == "" {
		t.Skip("typst is not installed")
	}
	root := t.TempDir()
	for _, f := range []string{"brand.typ", "lib.typ", "policy-document.typ"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "templates", "typst", f)) // #nosec G304 -- the repository's own templates
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	payload := PolicyPayload{Title: `#panic("title")`, Reference: `\input{x}`, Classification: "Internal",
		Sections: []PolicySection{hostileSection()}, Frameworks: []string{}, Controls: []PolicyControl{}, Versions: []PolicyVersion{}}
	raw, _ := json.Marshal(payload)
	if err := os.WriteFile(filepath.Join(root, "data.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "compile", "--root", root, "--input", "data=/data.json", // #nosec G204 -- test-only, fixed arguments
		filepath.Join(root, "policy-document.typ"), filepath.Join(root, "out.pdf"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typst refused the hostile payload: %v\n%s", err, out)
	}
	if pdftotext, err := exec.LookPath("pdftotext"); err == nil {
		out, err := exec.Command(pdftotext, filepath.Join(root, "out.pdf"), "-").Output() // #nosec G204 -- test-only
		if err == nil && !strings.Contains(string(out), `#panic("pwned")`) {
			t.Fatalf("the hostile text was not printed literally")
		}
	}
}
