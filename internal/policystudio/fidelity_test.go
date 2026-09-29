package policystudio

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"grc/internal/clientprofile"
	"grc/internal/doctemplate"
	"grc/internal/policydocs"
)

// Rich content written in the Studio reaches every output with nothing lost:
// lists, tables, bold, italic, links, callouts, client facts and control
// references, in Markdown, HTML, LaTeX and (when typst is installed) the PDF.
func TestRichContentSurvivesEveryRenderer(t *testing.T) {
	f := newStudio(t)
	clients := clientprofile.NewService(f.conn)
	f.studio.clients = clients
	client, err := clients.Create(clientprofile.Profile{Name: "Example Bank AG"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.SetFact(client.ID, "legal_entity_name", "Example Bank AG", "text", "alice"); err != nil {
		t.Fatal(err)
	}

	doc, err := f.policies.CreateDocument(policydocs.Document{Title: "Fidelity", DocType: policydocs.TypePolicy, ClientProfileID: client.ID, ClientName: client.Name})
	if err != nil {
		t.Fatal(err)
	}
	// One section per fixture, stored as the Studio stores content.
	for _, name := range []string{"marks", "lists", "table", "blocks", "tokens"} {
		for _, fx := range loadFixtures(t) {
			if fx.Name != name {
				continue
			}
			root, err := ParseDoc(fx.Expected)
			if err != nil {
				t.Fatal(err)
			}
			sec := root.Content[0]
			raw, _ := json.Marshal(sec)
			if _, err := f.policies.CreateSection(policydocs.Section{DocumentID: doc.ID, UID: sec.Attr("uid"), Heading: HeadingText(sec),
				SectionKind: sec.Attr("kind"), ContentJSON: string(raw)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.policies.MarkStudio(doc.ID); err != nil {
		t.Fatal(err)
	}
	f.openRoom(doc.ID)
	if err := f.studio.Project(doc.ID); err != nil {
		t.Fatal(err)
	}

	_, md, err := f.policies.ExportMarkdown(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, html, err := f.policies.ExportHTML(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	export, err := f.policies.ExportTemplateJSON(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(export)
	payload, err := doctemplate.DecodePolicyPayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	latex := doctemplate.GenerateLaTeX(payload, doctemplate.DefaultBrand())

	expect := map[string][]string{
		"Markdown": {"**bold**", "*italic*", "`code`", "[link](https://example.com/a?b=1)", "- Access must be approved.",
			"3. starts at three", "| Role | Responsibility |", "> Quoted.", "> **Important:** Read this.", "### Sub-heading",
			"This policy applies to Example Bank AG", "AC-1"},
		"HTML": {"<strong>bold</strong>", "<em>italic</em>", "<code>code</code>", `<a href="https://example.com/a?b=1"`, "<ul>",
			`<ol start="3">`, "<th>", "<blockquote>", "callout-important", "<h3>Sub-heading</h3>", "Example Bank AG", `class="control-ref">AC-1`},
		"LaTeX": {`\textbf{bold}`, `\emph{italic}`, `\texttt{code}`, `\href{https://example.com/a?b=1}`, `\begin{itemize}`,
			`\setcounter{enumi}{2}`, `\begin{tabularx}{\linewidth}{@{}XX@{}}`, `\begin{quote}`, `\cllabel{Important}`,
			`\subsection*{Sub-heading}`, "Example Bank AG", "AC-1"},
	}
	for format, wants := range map[string]string{"Markdown": md, "HTML": html, "LaTeX": latex} {
		for _, w := range expect[format] {
			if !strings.Contains(wants, w) {
				t.Errorf("%s lacks %q", format, w)
			}
		}
	}
	if strings.Contains(md+html+latex, "UNRESOLVED") {
		t.Error("a known fact rendered as unresolved")
	}

	bin := os.Getenv("TYPST")
	if bin == "" {
		bin, _ = exec.LookPath("typst")
	}
	if bin == "" {
		t.Log("typst not installed: PDF not checked")
		return
	}
	dir := t.TempDir()
	for _, name := range []string{"brand.typ", "lib.typ", "policy-document.typ"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "templates", "typst", name)) // #nosec G304 -- the repository's own templates
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(dir, "out.pdf")
	if out, err := exec.Command(bin, "compile", "--root", dir, "--input", "data=/data.json", filepath.Join(dir, "policy-document.typ"), pdf).CombinedOutput(); err != nil { // #nosec G204 -- test-only
		t.Fatalf("typst: %v\n%s", err, out)
	}
	if os.Getenv("KEEP_FIDELITY_PDF") != "" {
		_ = os.WriteFile(os.Getenv("KEEP_FIDELITY_PDF"), mustRead(t, pdf), 0o600)
	}
	pdftotext, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Log("pdftotext not installed: PDF text not checked")
		return
	}
	text, err := exec.Command(pdftotext, pdf, "-").Output() // #nosec G204 -- test-only
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"bold", "italic", "link", "Access must be approved.", "starts at three", "Responsibility", "Owns the policy.", "Quoted.", "Read this.", "Sub-heading", "Example Bank AG", "AC-1"} {
		if !strings.Contains(string(text), w) {
			t.Errorf("PDF lacks %q", w)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test-only
	if err != nil {
		t.Fatal(err)
	}
	return b
}
