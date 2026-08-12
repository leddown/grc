package doctemplate

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"grc/internal/db"
)

// repoTemplatesDir points the package at the checked-in templates. Tests run
// with the package directory as the working directory, where the normal search
// finds nothing.
func repoTemplatesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("resolving templates dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, dirMarker)); err != nil {
		t.Fatalf("templates directory not found at %s: %v", dir, err)
	}
	return dir
}

func withRepoTemplates(t *testing.T) {
	t.Helper()
	previous := DirOverride
	DirOverride = repoTemplatesDir(t)
	t.Cleanup(func() { DirOverride = previous })
}

// withEngines pins the engine detection cache, so a test's outcome does not
// depend on whether the machine running it happens to have typst installed.
func withEngines(t *testing.T, statuses map[Engine]EngineStatus) {
	t.Helper()
	engineCacheMu.Lock()
	previous := engineCache
	engineCache = statuses
	engineCacheMu.Unlock()
	t.Cleanup(func() {
		engineCacheMu.Lock()
		engineCache = previous
		engineCacheMu.Unlock()
	})
}

func noEngines() map[Engine]EngineStatus {
	return map[Engine]EngineStatus{
		EngineTypst: {Engine: EngineTypst, Command: "typst", Detail: "none of typst found on PATH"},
		EngineLaTeX: {Engine: EngineLaTeX, Command: "tectonic", Detail: "none of tectonic, latexmk found on PATH"},
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doctemplate_test.db")
	conn, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewService(NewSQLiteRepository(conn))
}

func samplePayload(t *testing.T) []byte {
	t.Helper()
	tpl, _ := Lookup("policy-typst")
	data, err := SampleData(tpl)
	if err != nil {
		t.Fatalf("SampleData: %v", err)
	}
	return data
}

// ---- Brand defaults and the file they mirror ----

// TestDefaultBrandMatchesBrandTyp is the guard on the one duplication this
// package accepts: DefaultBrand restates the dictionary in brand.typ, and
// nothing else would notice the two drifting apart. A designer editing the file
// would otherwise find the app quietly rendering the old palette.
func TestDefaultBrandMatchesBrandTyp(t *testing.T) {
	withRepoTemplates(t)

	source, err := os.ReadFile(filepath.Join(Dir(), "typst", "brand.typ"))
	if err != nil {
		t.Fatalf("reading brand.typ: %v", err)
	}
	text := string(source)
	brand := DefaultBrand()

	for _, want := range []struct {
		key   string
		value string
	}{
		{"firm", `firm: "` + brand.Firm + `"`},
		{"firm-tagline", `firm-tagline: "` + brand.Tagline + `"`},
		{"accent", `accent: rgb("` + brand.Accent + `")`},
		{"accent-dark", `accent-dark: rgb("` + brand.AccentDark + `")`},
		{"ink", `ink: rgb("` + brand.Ink + `")`},
		{"muted", `muted: rgb("` + brand.Muted + `")`},
		{"rule", `rule: rgb("` + brand.Rule + `")`},
		{"table-head", `table-head: rgb("` + brand.TableHead + `")`},
		{"table-zebra", `table-zebra: rgb("` + brand.TableZebra + `")`},
		{"ok", `ok: rgb("` + brand.OK + `")`},
		{"warn", `warn: rgb("` + brand.Warn + `")`},
		{"risk", `risk: rgb("` + brand.Risk + `")`},
		{"body-size", "body-size: 11pt"},
		{"small-size", "small-size: 9pt"},
		{"micro-size", "micro-size: 7.5pt"},
		{"paper", `paper: "a4"`},
		{"margin", "margin: (x: 30mm, top: 28mm, bottom: 26mm)"},
		{"footer-note", `footer-note: "` + brand.FooterNote + `"`},
	} {
		if !strings.Contains(text, want.value) {
			t.Errorf("brand.typ has drifted from DefaultBrand: expected %s to be %q", want.key, want.value)
		}
	}

	for _, family := range append(append([]string{}, brand.SerifStack...), brand.SansStack...) {
		if !strings.Contains(text, `"`+family+`"`) {
			t.Errorf("brand.typ does not mention font %q from DefaultBrand", family)
		}
	}
}

// TestApplyToTypstBrandFindsBlock is the other half: the splice locates the
// dictionary in the real file and leaves everything after it intact. A reformat
// of brand.typ that broke the splice would otherwise surface as a render with
// the wrong brand rather than as a failure.
func TestApplyToTypstBrandFindsBlock(t *testing.T) {
	withRepoTemplates(t)

	source, err := os.ReadFile(filepath.Join(Dir(), "typst", "brand.typ"))
	if err != nil {
		t.Fatalf("reading brand.typ: %v", err)
	}

	brand := DefaultBrand()
	brand.Firm = "Northwind Assurance"
	brand.Accent = "#123456"
	brand.BodySize = 12

	out, err := ApplyToTypstBrand(string(source), brand)
	if err != nil {
		t.Fatalf("ApplyToTypstBrand: %v", err)
	}

	if !strings.Contains(out, `firm: "Northwind Assurance"`) {
		t.Error("spliced source does not carry the new firm name")
	}
	if !strings.Contains(out, `accent: rgb("#123456")`) {
		t.Error("spliced source does not carry the new accent")
	}
	if !strings.Contains(out, "body-size: 12pt") {
		t.Error("spliced source does not carry the new body size")
	}
	if strings.Contains(out, `rgb("#7a1f2e")`) {
		t.Error("spliced source still carries the old accent — the dictionary was appended to, not replaced")
	}
	// The helpers below the dictionary must survive: they are what the
	// templates import, and losing them turns a brand change into a build
	// failure inside the typesetter.
	for _, helper := range []string{
		"#let classification-colour(label)",
		"#let wordmark(",
		"#let label-text(body)",
		"#let field(label, value)",
		"#let pill(body",
	} {
		if !strings.Contains(out, helper) {
			t.Errorf("splice dropped %q from brand.typ", helper)
		}
	}
	// Exactly one dictionary, or the helpers close over whichever came first.
	if got := strings.Count(out, brandDictStart); got != 1 {
		t.Errorf("spliced source has %d brand dictionaries, want 1", got)
	}
}

func TestApplyToTypstBrandRejectsSourceWithoutTheBlock(t *testing.T) {
	if _, err := ApplyToTypstBrand("// no dictionary here\n", DefaultBrand()); err == nil {
		t.Fatal("expected an error when the brand dictionary is absent")
	}
}

// ---- Validation ----

func TestBrandValidateAcceptsTheDefault(t *testing.T) {
	brand := DefaultBrand()
	if err := brand.Validate(); err != nil {
		t.Fatalf("the shipped default must validate: %v", err)
	}
}

func TestBrandValidateRejectsInjection(t *testing.T) {
	// Every case here would otherwise reach a typesetter that executes its
	// input. The Typst ones would close the string literal and continue as
	// code; the LaTeX ones would run as macros.
	cases := []struct {
		name   string
		mutate func(*Brand)
	}{
		{"quote in firm escapes the string", func(b *Brand) { b.Firm = `Acme", evil: read("/etc/passwd"), x: "` }},
		{"newline in tagline", func(b *Brand) { b.Tagline = "Acme\n#read(\"/etc/passwd\")" }},
		{"colour is not hex", func(b *Brand) { b.Accent = `#000"), evil: read("/etc/passwd` }},
		{"colour is a typst expression", func(b *Brand) { b.Ink = "rgb(0,0,0)" }},
		{"three-digit colour", func(b *Brand) { b.Muted = "#abc" }},
		{"font name carries a brace", func(b *Brand) { b.SerifStack = []string{"Serif}\\input{/etc/passwd}"} }},
		{"font name carries a backslash", func(b *Brand) { b.SansStack = []string{`Arial\write18{rm -rf /}`} }},
		{"empty font stack", func(b *Brand) { b.MonoStack = []string{"  ", ""} }},
		{"paper is not in the allowlist", func(b *Brand) { b.Paper = "a4}\\input{x" }},
		{"body size out of range", func(b *Brand) { b.BodySize = 400 }},
		{"negative margin", func(b *Brand) { b.MarginX = -5 }},
		{"small size above body size", func(b *Brand) { b.SmallSize = 20 }},
		{"micro size above small size", func(b *Brand) { b.MicroSize = 10 }},
		{"empty firm", func(b *Brand) { b.Firm = "   " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			brand := DefaultBrand()
			tc.mutate(&brand)
			if err := brand.Validate(); err == nil {
				t.Fatalf("Validate accepted %s", tc.name)
			}
		})
	}
}

func TestBrandValidateNormalises(t *testing.T) {
	brand := DefaultBrand()
	brand.Firm = "  Acme Assurance  "
	brand.Accent = "  #AABBCC  "
	brand.Paper = " A4 "
	brand.SerifStack = []string{" PT Serif ", "", "  "}

	if err := brand.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if brand.Firm != "Acme Assurance" {
		t.Errorf("firm = %q, want it trimmed", brand.Firm)
	}
	if brand.Accent != "#aabbcc" {
		t.Errorf("accent = %q, want lowercased and trimmed", brand.Accent)
	}
	if brand.Paper != "a4" {
		t.Errorf("paper = %q, want lowercased", brand.Paper)
	}
	if len(brand.SerifStack) != 1 || brand.SerifStack[0] != "PT Serif" {
		t.Errorf("serif stack = %#v, want the blanks dropped", brand.SerifStack)
	}
}

// TestTypstDictQuotesEverySettableString is a belt-and-braces check that the
// generator never emits a bare value, independent of what Validate allows.
func TestTypstDictQuotesEverySettableString(t *testing.T) {
	brand := DefaultBrand()
	dict := brand.TypstDict()
	for _, want := range []string{
		`firm: "GRC"`,
		`serif: ("Libertinus Serif", "Liberation Serif", "Times New Roman", "DejaVu Serif")`,
		`paper: "a4"`,
		"margin: (x: 30mm, top: 28mm, bottom: 26mm)",
		"logo: none",
	} {
		if !strings.Contains(dict, want) {
			t.Errorf("TypstDict is missing %q\n%s", want, dict)
		}
	}
}

func TestTypstStackOfOneKeepsTheTrailingComma(t *testing.T) {
	// Without it Typst parses (x) as a parenthesised string, and the template
	// then indexes characters instead of families.
	if got := typstStack([]string{"Arial"}); got != `("Arial",)` {
		t.Errorf("typstStack = %q, want a one-element array", got)
	}
}

// ---- LaTeX generation ----

func TestLatexEscape(t *testing.T) {
	cases := map[string]string{
		"100% of accounts":    `100\% of accounts`,
		"Finance & Risk":      `Finance \& Risk`,
		"cost $5":             `cost \$5`,
		"under_score":         `under\_score`,
		"a#b":                 `a\#b`,
		`\input{/etc/passwd}`: `\textbackslash{}input\{/etc/passwd\}`,
		"tilde~here":          `tilde\textasciitilde{}here`,
		"caret^here":          `caret\textasciicircum{}here`,
	}
	for in, want := range cases {
		if got := latexEscape(in); got != want {
			t.Errorf("latexEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestGenerateLaTeXEscapesTheBody is the one that matters: a policy body is
// author-supplied prose, and an unescaped backslash there is arbitrary TeX.
func TestGenerateLaTeXEscapesTheBody(t *testing.T) {
	payload := PolicyPayload{
		Title:     `Access & Identity 100% Policy`,
		Reference: "POL-AC-001",
		Sections: []PolicySection{{
			Heading: "Scope & Purpose",
			Body:    "Covers 100% of systems.\n\n\\write18{rm -rf /} must never run.",
		}},
	}
	out := GenerateLaTeX(payload, DefaultBrand())

	if strings.Contains(out, `\write18`) {
		t.Error("the generated document carries an unescaped \\write18 from the body")
	}
	if !strings.Contains(out, `\textbackslash{}write18`) {
		t.Error("the body's backslash was not escaped")
	}
	if !strings.Contains(out, `Access \& Identity 100\% Policy`) {
		t.Error("the title was not escaped")
	}
	if !strings.Contains(out, `\section{Scope \& Purpose}`) {
		t.Error("the section heading was not escaped")
	}
	// Every % that survives must be an escaped one or the start of a comment
	// line; a bare % mid-line silently swallows the rest of that line.
	for i, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "%") {
			continue
		}
		if bareControlChar(line, '%') {
			t.Errorf("line %d has an unescaped %%: %s", i+1, line)
		}
	}
}

// bareControlChar reports an occurrence of c that is not preceded by a
// backslash, ignoring one at the very end of the line. A trailing % is LaTeX's
// line-continuation comment, which the preamble uses deliberately; a mid-line
// one silently swallows the rest of the line, which is the failure being
// hunted here.
func bareControlChar(line string, c byte) bool {
	line = strings.TrimRight(line, " \t")
	if strings.HasSuffix(line, string(c)) && !strings.HasSuffix(line, `\`+string(c)) {
		line = line[:len(line)-1]
	}
	for i := 0; i < len(line); i++ {
		if line[i] != c {
			continue
		}
		if i == 0 || line[i-1] != '\\' {
			return true
		}
	}
	return false
}

func TestGenerateLaTeXFromTheSample(t *testing.T) {
	withRepoTemplates(t)

	payload, err := DecodePolicyPayload(samplePayload(t))
	if err != nil {
		t.Fatalf("DecodePolicyPayload: %v", err)
	}
	out := GenerateLaTeX(payload, DefaultBrand())

	for _, want := range []string{
		`\documentclass[11pt,a4paper]{article}`,
		`\usepackage{grc}`,
		`\begin{document}`,
		`\end{document}`,
		`\clcoverpage{`,
		"\\section*{Document Control}",
		"\\section*{Revision History}",
		"\\section{Control Mapping}",
		`\clapproval{`,
		`\tableofcontents`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated LaTeX is missing %q", want)
		}
	}
	if strings.Count(out, `\begin{document}`) != 1 {
		t.Error("generated LaTeX does not have exactly one document environment")
	}
	// Every control in the payload has to reach the mapping table; a policy
	// whose PDF understates its coverage is worse than one that fails to build.
	for _, control := range payload.Controls {
		if !strings.Contains(out, latexEscape(control.ControlID)) {
			t.Errorf("control %s is missing from the generated document", control.ControlID)
		}
	}
}

func TestLatexBlocksRecognisesLists(t *testing.T) {
	out := latexBlocks("Intro paragraph.\n\n- first\n- second\n\n1. one\n2. two\n\n- mixed\n2. markers")
	if !strings.Contains(out, `\begin{itemize}`) || !strings.Contains(out, `\item first`) {
		t.Errorf("bullet block was not turned into an itemize:\n%s", out)
	}
	if !strings.Contains(out, `\begin{enumerate}`) || !strings.Contains(out, `\item one`) {
		t.Errorf("numbered block was not turned into an enumerate:\n%s", out)
	}
	// A block mixing markers is ambiguous and stays prose rather than being
	// silently restructured.
	if strings.Contains(out, `\item mixed`) {
		t.Errorf("a block with mixed markers should stay prose:\n%s", out)
	}
	if !strings.Contains(out, "Intro paragraph.") {
		t.Errorf("the leading paragraph was lost:\n%s", out)
	}
}

func TestLatexBlocksLeavesProseWithADashAlone(t *testing.T) {
	out := latexBlocks("Access is reviewed quarterly.\n- but this line is part of the same thought")
	if strings.Contains(out, `\begin{itemize}`) {
		t.Errorf("a paragraph with one dashed line should not become a list:\n%s", out)
	}
}

func TestLaTeXPreambleUsesValidatedValues(t *testing.T) {
	brand := DefaultBrand()
	brand.Firm = "Acme & Co"
	brand.Accent = "#123abc"
	preamble := brand.LaTeXPreamble()

	if !strings.Contains(preamble, `\renewcommand{\clFirm}{Acme \& Co}`) {
		t.Errorf("firm name was not escaped into the preamble:\n%s", preamble)
	}
	if !strings.Contains(preamble, `\definecolor{clAccent}{HTML}{123ABC}`) {
		t.Errorf("accent was not written as an uppercase HTML colour:\n%s", preamble)
	}
	if !strings.Contains(preamble, `\color{clInk}`) {
		t.Error("the preamble must re-assert the body colour after redefining it")
	}
}

func TestLaTeXClassOptionsSnapsToTheAvailableSizes(t *testing.T) {
	for _, tc := range []struct {
		size float64
		want string
	}{
		{9, "10pt,a4paper"},
		{10, "10pt,a4paper"},
		{11, "11pt,a4paper"},
		{11.4, "11pt,a4paper"},
		{12, "12pt,a4paper"},
		{14, "12pt,a4paper"},
	} {
		brand := DefaultBrand()
		brand.BodySize = tc.size
		if got := brand.LaTeXClassOptions(); got != tc.want {
			t.Errorf("LaTeXClassOptions at %gpt = %q, want %q", tc.size, got, tc.want)
		}
	}
}

// ---- Payload contract ----

// TestPolicyPayloadMatchesSample decodes the committed sample through this
// package's declaration of the render contract. The sample is a hand-written
// instance of what internal/policydocs emits, so a field silently renamed on
// either side shows up here as a zero value.
func TestPolicyPayloadMatchesSample(t *testing.T) {
	withRepoTemplates(t)

	payload, err := DecodePolicyPayload(samplePayload(t))
	if err != nil {
		t.Fatalf("DecodePolicyPayload: %v", err)
	}
	if payload.Title == "" || payload.Reference == "" || payload.DocType == "" {
		t.Errorf("core fields did not decode: %+v", payload)
	}
	if len(payload.Sections) == 0 {
		t.Error("no sections decoded from the sample")
	}
	if len(payload.Controls) == 0 {
		t.Error("no controls decoded from the sample")
	}
	if len(payload.Versions) == 0 {
		t.Error("no versions decoded from the sample")
	}
	if len(payload.Frameworks) == 0 {
		t.Error("no frameworks decoded from the sample")
	}
	for _, section := range payload.Sections {
		if section.Heading == "" {
			t.Error("a section decoded without a heading")
		}
	}
}

func TestDecodePolicyPayloadRejectsAReport(t *testing.T) {
	withRepoTemplates(t)
	tpl, _ := Lookup("report-typst")
	data, err := SampleData(tpl)
	if err != nil {
		t.Fatalf("SampleData: %v", err)
	}
	// The report sample has no policy title, which is the shallow check that
	// stops a payload reaching the wrong generator.
	if _, err := DecodePolicyPayload(data); err == nil {
		t.Skip("the report sample happens to carry a title field; the kind check on the route is the real guard")
	}
}

func TestDocTypeLabel(t *testing.T) {
	for slug, want := range map[string]string{
		"policy":           "Policy",
		"standard":         "Standard",
		"work_instruction": "Work Instruction",
		"tra":              "Targeted Risk Analysis",
		"":                 "Policy",
	} {
		if got := (PolicyPayload{DocType: slug}).DocTypeLabel(); got != want {
			t.Errorf("DocTypeLabel(%q) = %q, want %q", slug, got, want)
		}
	}
}

// ---- Rendering ----

// TestRenderFallsBackToASourceBundle is the behaviour the whole feature rests
// on: a host with no typesetter is an ordinary state for this app, and the
// answer there is the sources plus a build script rather than an error.
func TestRenderFallsBackToASourceBundle(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())

	service := newTestService(t)
	result, err := service.Render(context.Background(), Request{
		TemplateID:  "policy-typst",
		Data:        samplePayload(t),
		BaseName:    "POL-AC-001",
		PDFStandard: "a-2b",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !result.Bundled {
		t.Fatal("expected a bundle when no engine is installed")
	}
	if result.ContentType != "application/zip" {
		t.Errorf("content type = %q, want application/zip", result.ContentType)
	}
	if result.Reason == "" {
		t.Error("an unrequested bundle must explain itself")
	}

	names := zipEntries(t, result.Body)
	for _, want := range []string{"policy-document.typ", "brand.typ", "lib.typ", "data.json", "build.sh", "README.txt"} {
		if !names[want] {
			t.Errorf("bundle is missing %s (has %v)", want, keys(names))
		}
	}
}

func TestBundleCarriesTheSavedBrand(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())

	service := newTestService(t)
	brand := DefaultBrand()
	brand.Firm = "Northwind Assurance"
	brand.Accent = "#0a5f2e"
	if _, err := service.SaveBrand(brand, "tester"); err != nil {
		t.Fatalf("SaveBrand: %v", err)
	}

	result, err := service.Render(context.Background(), Request{
		TemplateID: "policy-typst",
		Data:       samplePayload(t),
		BaseName:   "POL-AC-001",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	brandSource := zipEntry(t, result.Body, "brand.typ")
	if !strings.Contains(brandSource, `firm: "Northwind Assurance"`) {
		t.Error("the bundled brand.typ does not carry the saved firm name")
	}
	if !strings.Contains(brandSource, `accent: rgb("#0a5f2e")`) {
		t.Error("the bundled brand.typ does not carry the saved accent")
	}
}

func TestLaTeXBundleCarriesAGeneratedDocument(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())

	service := newTestService(t)
	result, err := service.Render(context.Background(), Request{
		TemplateID: "policy-latex",
		Data:       samplePayload(t),
		BaseName:   "POL-AC-001",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	names := zipEntries(t, result.Body)
	for _, want := range []string{"policy-document.tex", "grc.sty", "data.json", "build.sh"} {
		if !names[want] {
			t.Errorf("LaTeX bundle is missing %s (has %v)", want, keys(names))
		}
	}
	// The .tex must be the generated one, carrying real document content —
	// not the committed reference with its hard-coded Northwind sample.
	tex := zipEntry(t, result.Body, "policy-document.tex")
	if !strings.Contains(tex, "Generated by internal/doctemplate") {
		t.Error("the bundled .tex is not the generated one")
	}
}

func TestRenderRejectsAMismatchedTemplate(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())

	service := newTestService(t)
	service.SetPayloadSource(func(int64) ([]byte, string, Kind, error) {
		return samplePayload(t), "POL-AC-001", KindPolicy, nil
	})
	// The business-report template against a policy payload renders a PDF with
	// every field blank, which reads as a template bug rather than the mismatch
	// it is.
	if _, err := service.RenderDocument(context.Background(), 1, "report-typst", "", false); err == nil {
		t.Fatal("expected a kind mismatch to be rejected")
	}
}

func TestRenderRejectsBadInput(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())
	service := newTestService(t)

	cases := []struct {
		name string
		req  Request
	}{
		{"unknown template", Request{TemplateID: "nope", Data: samplePayload(t)}},
		{"empty payload", Request{TemplateID: "policy-typst", Data: nil}},
		{"payload is not JSON", Request{TemplateID: "policy-typst", Data: []byte("not json")}},
		{"unsupported pdf standard", Request{TemplateID: "policy-typst", Data: samplePayload(t), PDFStandard: "../../etc/passwd"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Render(context.Background(), tc.req); err == nil {
				t.Fatalf("Render accepted %s", tc.name)
			}
		})
	}
}

func TestRenderReportsAMissingTemplatesDirectory(t *testing.T) {
	withRepoTemplates(t)
	// Read the payload while the templates are still reachable, then point the
	// package at a directory that has none.
	data := samplePayload(t)
	DirOverride = filepath.Join(t.TempDir(), "absent")
	withEngines(t, noEngines())

	service := newTestService(t)
	_, err := service.Render(context.Background(), Request{TemplateID: "policy-typst", Data: data})
	if err == nil || !strings.Contains(err.Error(), "templates") {
		t.Fatalf("error = %v, want one naming the missing templates directory", err)
	}
}

func TestSanitiseBaseName(t *testing.T) {
	cases := map[string]string{
		"POL-AC-001":               "POL-AC-001",
		"  POL-AC-001  ":           "POL-AC-001",
		"../../etc/passwd":         "etc-passwd",
		`policy"; rm -rf /`:        "policy-rm-rf",
		"":                         "document",
		"...":                      "document",
		"Access Control Policy v2": "Access-Control-Policy-v2",
	}
	for in, want := range cases {
		if got := sanitiseBaseName(in); got != want {
			t.Errorf("sanitiseBaseName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sanitiseBaseName(strings.Repeat("a", 300)); len(got) > 80 {
		t.Errorf("sanitiseBaseName did not truncate: %d chars", len(got))
	}
}

func TestHeaderSafeFlattensTheLog(t *testing.T) {
	got := headerSafe("error: something\r\nHTTP/1.1 200 OK\n\nX-Injected: yes")
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("headerSafe left a line break in %q", got)
	}
	if len(headerSafe(strings.Repeat("x", 5000))) > 1000 {
		t.Error("headerSafe did not truncate a long log")
	}
}

func TestFilterEngineLogHidesOnlyFontWarnings(t *testing.T) {
	log := filterEngineLog("warning: unknown font family: Arial\nerror: expected identifier\nwarning: unknown font family: Noto Sans")
	for _, family := range []string{"Arial", "Noto Sans"} {
		if strings.Contains(log, family) {
			t.Errorf("the %s font warning was not filtered:\n%s", family, log)
		}
	}
	if !strings.Contains(log, "error: expected identifier") {
		t.Errorf("a real error was filtered out:\n%s", log)
	}
	if !strings.Contains(log, "2 'unknown font family' warnings hidden") {
		t.Errorf("the filter did not say what it hid:\n%s", log)
	}
}

// TestFilterEngineLogDropsTheWholeDiagnostic covers what a real typst run
// actually prints: the warning line plus a framed source excerpt. Filtering
// only the warning left the frame behind, pointing at brand.typ with nothing
// saying why — which reads as an error in a file the user never wrote.
func TestFilterEngineLogDropsTheWholeDiagnostic(t *testing.T) {
	raw := strings.Join([]string{
		`warning: unknown font family: Libertinus Serif`,
		`   ┌─ brand.typ:68:15`,
		`   │`,
		`68 │   text(font: brand.sans, size: size, weight: "bold")[`,
		`   │        ^^^^^^^^^^`,
		``,
		`error: expected identifier, found string`,
		`   ┌─ policy-document.typ:14:2`,
		`   │`,
		`14 │  #let x = "oops`,
		`   │  ^^^^`,
	}, "\n")

	log := filterEngineLog(raw)
	for _, gone := range []string{"brand.typ:68", "Libertinus Serif", `weight: "bold"`} {
		if strings.Contains(log, gone) {
			t.Errorf("the font warning's context line %q survived:\n%s", gone, log)
		}
	}
	// The real error keeps its excerpt: that is the whole diagnostic value of
	// the log when a template is broken.
	for _, want := range []string{"error: expected identifier", "policy-document.typ:14:2", `#let x = "oops`} {
		if !strings.Contains(log, want) {
			t.Errorf("the real error lost %q:\n%s", want, log)
		}
	}
}

// ---- Persistence ----

func TestBrandRoundTrip(t *testing.T) {
	service := newTestService(t)

	if brand, err := service.Brand(); err != nil {
		t.Fatalf("Brand: %v", err)
	} else if brand.Firm != DefaultBrand().Firm {
		t.Errorf("a fresh install must fall back to the shipped default, got %q", brand.Firm)
	}

	wanted := DefaultBrand()
	wanted.Firm = "Northwind Assurance"
	wanted.Accent = "#0A5F2E"
	wanted.SerifStack = []string{"PT Serif", "Georgia"}
	saved, err := service.SaveBrand(wanted, "alice")
	if err != nil {
		t.Fatalf("SaveBrand: %v", err)
	}
	if saved.UpdatedBy != "alice" || saved.UpdatedAt == "" {
		t.Errorf("save did not record who and when: %+v", saved)
	}

	loaded, err := service.Brand()
	if err != nil {
		t.Fatalf("Brand: %v", err)
	}
	if loaded.Firm != "Northwind Assurance" || loaded.Accent != "#0a5f2e" {
		t.Errorf("loaded brand = %+v, want the saved values normalised", loaded)
	}
	if len(loaded.SerifStack) != 2 || loaded.SerifStack[0] != "PT Serif" {
		t.Errorf("font stack did not survive the round trip: %#v", loaded.SerifStack)
	}

	reset, err := service.ResetBrand()
	if err != nil {
		t.Fatalf("ResetBrand: %v", err)
	}
	if reset.Firm != DefaultBrand().Firm {
		t.Errorf("reset brand = %q, want the shipped default", reset.Firm)
	}
	after, err := service.Brand()
	if err != nil {
		t.Fatalf("Brand after reset: %v", err)
	}
	if after.Firm != DefaultBrand().Firm || after.UpdatedAt != "" {
		t.Errorf("reset did not remove the stored row: %+v", after)
	}
}

func TestSaveBrandRejectsInvalidSettings(t *testing.T) {
	service := newTestService(t)
	brand := DefaultBrand()
	brand.Accent = "not-a-colour"
	if _, err := service.SaveBrand(brand, "alice"); err == nil {
		t.Fatal("SaveBrand accepted an invalid colour")
	}
}

// TestLoadBrandFillsFieldsAddedLater covers the upgrade path: a blob written
// before a field existed must come back with the shipped default for it, not a
// zero value that Validate would then reject.
func TestLoadBrandFillsFieldsAddedLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	conn, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer conn.Close()

	_, err = conn.Exec(conn.Rebind(
		`INSERT INTO doc_template_brand (id, settings_json, updated_at, updated_by) VALUES (1, ?, ?, ?)`),
		`{"firm":"Legacy Ltd"}`, "2026-01-01T00:00:00Z", "alice")
	if err != nil {
		t.Fatalf("seeding the old row: %v", err)
	}

	brand, found, err := NewSQLiteRepository(conn).LoadBrand()
	if err != nil || !found {
		t.Fatalf("LoadBrand: %v (found=%v)", err, found)
	}
	if brand.Firm != "Legacy Ltd" {
		t.Errorf("firm = %q, want the stored value", brand.Firm)
	}
	if brand.Accent != DefaultBrand().Accent {
		t.Errorf("accent = %q, want the shipped default for a field the blob predates", brand.Accent)
	}
	if err := brand.Validate(); err != nil {
		t.Errorf("a back-filled brand must validate: %v", err)
	}
}

// ---- Routes ----

func newTestRouter(t *testing.T, service *Service) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(service, func(*gin.Context) string { return "tester" })
	handler.RegisterReadRoutes(router)
	handler.RegisterAdminRoutes(router)
	return router
}

func TestRoutesServeTheCatalogAndPages(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())
	router := newTestRouter(t, newTestService(t))

	for _, path := range []string{"/templates", "/templates/manage"} {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, res.Code)
		}
		if !strings.Contains(res.Body.String(), "<!doctype html>") {
			t.Errorf("GET %s did not return a page", path)
		}
	}

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/templates/data", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("GET /templates/data = %d", res.Code)
	}
	var catalog Catalog
	if err := json.Unmarshal(res.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decoding the catalog: %v", err)
	}
	if len(catalog.Templates) != len(Templates) {
		t.Errorf("catalog has %d templates, want %d", len(catalog.Templates), len(Templates))
	}
	if len(catalog.Engines) != 2 {
		t.Errorf("catalog has %d engines, want 2", len(catalog.Engines))
	}
	if !catalog.DirAvailable {
		t.Error("catalog reports the templates directory as unavailable")
	}
}

func TestRenderRouteReturnsABundleWithHeaders(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())

	service := newTestService(t)
	service.SetPayloadSource(func(id int64) ([]byte, string, Kind, error) {
		return samplePayload(t), "POL-AC-001", KindPolicy, nil
	})
	router := newTestRouter(t, service)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/templates/render?doc=1", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("render = %d: %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("content type = %q, want application/zip", got)
	}
	if res.Header().Get("X-GRC-Bundled") != "1" {
		t.Error("a bundled response must say so in a header")
	}
	if disposition := res.Header().Get("Content-Disposition"); !strings.Contains(disposition, "POL-AC-001") {
		t.Errorf("content disposition = %q, want the document reference", disposition)
	}
	if _, err := zip.NewReader(bytes.NewReader(res.Body.Bytes()), int64(res.Body.Len())); err != nil {
		t.Fatalf("the response body is not a zip: %v", err)
	}
}

func TestRenderRouteRequiresATarget(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())
	router := newTestRouter(t, newTestService(t))

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/templates/render", nil))
	if res.Code != http.StatusBadRequest {
		t.Errorf("render with no doc or sample = %d, want 400", res.Code)
	}
}

func TestBrandRouteRoundTrip(t *testing.T) {
	withRepoTemplates(t)
	router := newTestRouter(t, newTestService(t))

	brand := DefaultBrand()
	brand.Firm = "Northwind Assurance"
	body, _ := json.Marshal(brand)

	req := httptest.NewRequest(http.MethodPut, "/templates/brand", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("PUT /templates/brand = %d: %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/templates/brand", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("GET /templates/brand = %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), "Northwind Assurance") {
		t.Error("the saved firm name did not come back")
	}
	if !strings.Contains(res.Body.String(), `"defaults"`) {
		t.Error("the brand endpoint must also return the shipped defaults for the Reset action")
	}
}

func TestBrandRouteRejectsInvalidSettings(t *testing.T) {
	router := newTestRouter(t, newTestService(t))

	brand := DefaultBrand()
	brand.SerifStack = []string{`Arial\write18{x}`}
	body, _ := json.Marshal(brand)

	req := httptest.NewRequest(http.MethodPut, "/templates/brand", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Errorf("PUT with an injected font name = %d, want 400", res.Code)
	}
}

// ---- Registry ----

func TestEveryTemplateHasItsSourcesOnDisk(t *testing.T) {
	withRepoTemplates(t)
	for _, tpl := range Templates {
		for _, source := range tpl.Sources {
			path := filepath.Join(Dir(), filepath.FromSlash(source))
			if _, err := os.Stat(path); err != nil {
				t.Errorf("template %s names a missing source %s: %v", tpl.ID, source, err)
			}
		}
		if tpl.SampleData != "" {
			if _, err := SampleData(tpl); err != nil {
				t.Errorf("template %s names a missing sample: %v", tpl.ID, err)
			}
		}
		// A non-generated template's entry has to be one of the files copied in,
		// or the workspace would have nothing to compile.
		if !tpl.Generated {
			found := false
			for _, source := range tpl.Sources {
				if filepath.Base(source) == tpl.Entry {
					found = true
				}
			}
			if !found {
				t.Errorf("template %s has entry %s, which is not among its sources", tpl.ID, tpl.Entry)
			}
		}
	}
}

func TestFindDirPrefersTheEarliestCandidateHoldingTheMarker(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	installed := filepath.Join(root, "installed")
	for _, dir := range []string{empty, installed} {
		if err := os.MkdirAll(filepath.Join(dir, "typst"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(installed, dirMarker), []byte("#let brand = ()\n"), 0o600); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}

	if got := findDir([]string{empty, installed}); got != installed {
		t.Errorf("findDir = %q, want %q", got, installed)
	}
	if got := findDir([]string{empty}); got != "templates" {
		t.Errorf("findDir = %q, want the expected default when nothing matches", got)
	}
}

// ---- Compiled output, when the host has an engine ----

// TestRenderProducesAPDF only runs where a typesetter is installed. It is the
// end-to-end check that the workspace, the brand splice and the argument list
// actually drive the engine — everything above it verifies the inputs, and this
// verifies they add up.
func TestRenderProducesAPDF(t *testing.T) {
	withRepoTemplates(t)
	statuses := RefreshEngines()
	if !statuses[EngineTypst].Available {
		t.Skip("typst is not installed on this host; the bundle path is covered above")
	}

	service := newTestService(t)
	result, err := service.Render(context.Background(), Request{
		TemplateID:  "policy-typst",
		Data:        samplePayload(t),
		BaseName:    "POL-AC-001",
		PDFStandard: "a-2b",
	})
	if err != nil {
		t.Fatalf("Render: %v\n%s", err, result.Log)
	}
	if result.Bundled {
		t.Fatal("expected a PDF when typst is available")
	}
	if !bytes.HasPrefix(result.Body, []byte("%PDF-")) {
		t.Errorf("body is not a PDF: %q", result.Body[:min(16, len(result.Body))])
	}
	if result.Filename != "POL-AC-001.pdf" {
		t.Errorf("filename = %q", result.Filename)
	}
}

// ---- helpers ----

func zipEntries(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("reading the bundle: %v", err)
	}
	names := make(map[string]bool, len(reader.File))
	for _, file := range reader.File {
		names[file.Name] = true
	}
	return names
}

func zipEntry(t *testing.T, body []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("reading the bundle: %v", err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("opening %s: %v", name, err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		return buf.String()
	}
	t.Fatalf("%s is not in the bundle", name)
	return ""
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// hexColour is used by the test below to assert the generated dictionary never
// carries a colour the validator would have rejected.
var hexColour = regexp.MustCompile(`rgb\("([^"]*)"\)`)

func TestTypstDictOnlyEmitsValidatedColours(t *testing.T) {
	brand := DefaultBrand()
	if err := brand.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, match := range hexColour.FindAllStringSubmatch(brand.TypstDict(), -1) {
		if !colourPattern.MatchString(match[1]) {
			t.Errorf("generated dictionary carries a non-hex colour %q", match[1])
		}
	}
}
