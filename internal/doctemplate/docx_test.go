package doctemplate

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const mdSpecials = "\\`*_{}[]<>()#+-.!|~$&"

// inertMarkdown reports the first Markdown metacharacter in s that is not
// preceded by the backslash that makes it literal.
func inertMarkdown(s string) (byte, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if strings.IndexByte(mdSpecials, s[i]) >= 0 {
			return s[i], false
		}
	}
	return 0, true
}

func FuzzMarkdownText(f *testing.F) {
	for _, h := range hostile {
		f.Add(h)
	}
	f.Fuzz(func(t *testing.T, text string) {
		out := mdText(text)
		if c, ok := inertMarkdown(out); !ok {
			t.Fatalf("an unescaped %q from %q: %s", c, text, out)
		}
		if strings.Contains(out, "\n") {
			t.Fatalf("a newline from %q survived: it could start a block", text)
		}
	})
}

func TestMarkdownHrefAllowsOnlyWebAndMail(t *testing.T) {
	for href, want := range map[string]string{
		"https://example.com/a b(c)":  "https://example.com/a%20b%28c%29",
		"mailto:dpo@example.com":      "mailto:dpo@example.com",
		"javascript:alert(1)":         "",
		"file:///etc/passwd":          "",
		"/etc/passwd":                 "",
		"http:///no-host":             "",
		"https://example.com/<x>\\y>": "https://example.com/%3Cx%3E%5Cy%3E",
	} {
		if got := mdHref(href); got != want {
			t.Errorf("mdHref(%q) = %q, want %q", href, got, want)
		}
	}
}

func pandocBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("PANDOC")
	if bin == "" {
		bin, _ = exec.LookPath("pandoc")
	}
	if bin == "" {
		t.Skip("pandoc is not installed")
	}
	return bin
}

// With a pandoc binary available (PANDOC or PATH), the generated Markdown for a
// payload full of Markdown, HTML and TeX reads back as the same text, and a
// legacy body's image reference stays text rather than embedding the file.
func TestPandocRendersHostileBlocksAsText(t *testing.T) {
	bin := pandocBinary(t)
	root := t.TempDir()
	secret := filepath.Join(root, "secret.png")
	if err := os.WriteFile(secret, []byte("\x89PNG\r\n\x1a\nnot really"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := PolicySection{Heading: "Legacy", Body: "![leak](" + secret + ") <img src=\"" + secret + "\"> <script>x()</script>"}
	payload := PolicyPayload{Title: `<b>#panic("title")</b>`, Reference: `\input{x}`, Classification: "Internal",
		Sections: []PolicySection{hostileSection(), legacy}}
	md := GenerateMarkdown(payload, DefaultBrand())
	entry := filepath.Join(root, "in.md")
	if err := os.WriteFile(entry, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}

	plain, err := exec.Command(bin, "--sandbox", "--from", "gfm-raw_html", "--to", "plain", "--wrap", "none", entry).CombinedOutput() // #nosec G204 -- test-only, fixed arguments
	if err != nil {
		t.Fatalf("pandoc refused the Markdown: %v\n%s", err, plain)
	}
	text := string(plain)
	for _, h := range append([]string{`<b>#panic("title")</b>`, `<script>x()</script>`}, hostile...) {
		if !strings.Contains(text, h) {
			t.Errorf("%q did not come through as text", h)
		}
	}

	out := filepath.Join(root, "out.docx")
	if log, err := exec.Command(bin, "--sandbox", "--from", "gfm-raw_html", "--to", "docx", "--output", out, entry).CombinedOutput(); err != nil { // #nosec G204 -- test-only, fixed arguments
		t.Fatalf("pandoc could not write the document: %v\n%s", err, log)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			t.Errorf("the sandbox let %s into the document", f.Name)
		}
	}
}

func TestRenderProducesAWordDocument(t *testing.T) {
	bin := pandocBinary(t)
	withRepoTemplates(t)
	engines := noEngines()
	engines[EnginePandoc] = EngineStatus{Engine: EnginePandoc, Command: "pandoc", Path: bin, Available: true}
	withEngines(t, engines)

	result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "policy-docx", Data: samplePayload(t), BaseName: "POL-AC-001"})
	if err != nil {
		t.Fatalf("Render: %v\n%s", err, result.Log)
	}
	if result.Bundled || result.ContentType != docxContentType || result.Filename != "POL-AC-001.docx" {
		t.Fatalf("got %q %q bundled=%v", result.ContentType, result.Filename, result.Bundled)
	}
	zr, err := zip.NewReader(bytes.NewReader(result.Body), int64(len(result.Body)))
	if err != nil {
		t.Fatalf("not a docx: %v", err)
	}
	var doc string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(rc)
			_ = rc.Close()
			doc = buf.String()
		}
	}
	for _, want := range []string{"Document control", "Control mapping", "Revision history"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document has no %q", want)
		}
	}
}

func TestWordBundleCarriesTheGeneratedMarkdown(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())

	result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "policy-docx", Data: samplePayload(t), BaseName: "POL-AC-001"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	names := zipEntries(t, result.Body)
	for _, want := range []string{"policy-document.md", "data.json", "build.sh", "README.txt"} {
		if !names[want] {
			t.Errorf("Word bundle is missing %s (has %v)", want, keys(names))
		}
	}
	if script := zipEntry(t, result.Body, "build.sh"); !strings.Contains(script, "--sandbox --from gfm-raw_html --to docx") {
		t.Errorf("build.sh does not convert the way the app does:\n%s", script)
	}
}
