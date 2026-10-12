package policydocs

import (
	"strings"
	"testing"
)

var hostileText = []string{
	`<script>alert(1)</script>`, `"><img src=x onerror=alert(1)>`, `&lt;already escaped&gt;`,
	`#panic("pwned")`, `\input{/etc/passwd}`, `{}`, `$x$`, `]]><!--`,
}

func hostileBlocks() []Block {
	var runs []Run
	for _, t := range hostileText {
		runs = append(runs, Run{Text: t, Marks: []string{"bold"}}, Run{Text: t, Href: "javascript:alert(1)"},
			Run{Text: t, Href: `https://example.com/"onmouseover="alert(1)`}, Run{Text: t, Fact: &RunFact{Key: "k", Unresolved: true}})
	}
	p := Block{Type: "paragraph", Runs: runs}
	return []Block{p, {Type: "heading", Level: 2, Runs: runs},
		{Type: "bullet_list", Items: [][]Block{{p}}}, {Type: "ordered_list", Start: 3, Items: [][]Block{{p}}},
		{Type: "table", Rows: []TableRow{{Header: true, Cells: [][]Block{{p}}}}},
		{Type: "blockquote", Blocks: []Block{p}}, {Type: "callout", Kind: `important" onclick="x`, Blocks: []Block{p}}}
}

func TestBlocksHTMLEscapesEverything(t *testing.T) {
	out := BlocksHTML(hostileBlocks())
	for _, bad := range []string{"<script", "<img", "javascript:", `"onmouseover="`, `" onclick="`} {
		if strings.Contains(out, bad) {
			t.Errorf("rendered HTML contains %q", bad)
		}
	}
	if !strings.Contains(out, `href="https://example.com/&#34;onmouseover=&#34;alert(1)"`) {
		t.Error("an https link with quotes must be kept, with the quotes escaped")
	}
}

func FuzzBlocksHTML(f *testing.F) {
	for _, s := range hostileText {
		f.Add(s, "https://example.com/")
	}
	f.Fuzz(func(t *testing.T, text, href string) {
		out := BlocksHTML([]Block{{Type: "paragraph", Runs: []Run{{Text: text, Href: href}}}})
		// Every "<" in the output is one the renderer wrote.
		stripped := out
		for _, tag := range []string{"<p>", "</p>", "</a>", `<a href="`} {
			stripped = strings.ReplaceAll(stripped, tag, "")
		}
		if strings.Contains(stripped, "<") {
			t.Fatalf("unescaped markup from %q / %q: %s", text, href, out)
		}
	})
}
