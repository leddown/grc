package doctemplate

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func letterPayload(t *testing.T) PolicyPayload {
	t.Helper()
	payload, err := DecodePolicyPayload(mustSample(t, "letter-typst"))
	if err != nil {
		t.Fatalf("DecodePolicyPayload: %v", err)
	}
	return payload
}

func TestLetterPayloadsRenderAsLetters(t *testing.T) {
	for docType, want := range map[string]Kind{"letter": KindLetter, "policy": KindPolicy, "procedure": KindPolicy} {
		source := PolicyPayloadSource(func(int64) (any, string, error) {
			return map[string]any{"title": "T", "doc_type": docType}, "REF", nil
		})
		if _, _, kind, err := source(1); err != nil || kind != want {
			t.Errorf("%s: kind %q, %v; want %q", docType, kind, err, want)
		}
	}
}

// The pages link "Render PDF" and "Word" by the policy templates; a letter
// takes the letter template for the same engine, and nothing else is let
// through a kind mismatch.
func TestALetterTakesTheLetterTemplateForItsEngine(t *testing.T) {
	s := newTestService(t)
	for requested, want := range map[string]string{"": "letter-typst", "policy-typst": "letter-typst", "policy-docx": "letter-docx", "letter-docx": "letter-docx"} {
		tpl, err := s.resolveTemplate(requested, KindLetter)
		if err != nil || tpl.ID != want {
			t.Errorf("%q for a letter: %q, %v; want %q", requested, tpl.ID, err, want)
		}
	}
	for _, c := range []struct {
		id   string
		kind Kind
	}{{"policy-latex", KindLetter}, {"report-typst", KindLetter}, {"letter-typst", KindPolicy}, {"policy-redline-typst", KindLetter}} {
		if tpl, err := s.resolveTemplate(c.id, c.kind); err == nil {
			t.Errorf("%s for a %s was accepted as %s", c.id, c.kind, tpl.ID)
		}
	}
}

func TestLongDate(t *testing.T) {
	for in, want := range map[string]string{"2026-10-28": "28 October 2026", "2027-01-05": "5 January 2027", "": "", "28/10/2026": "28/10/2026"} {
		if got := LongDate(in); got != want {
			t.Errorf("LongDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateLetterMarkdownFollowsTheLetter(t *testing.T) {
	p := letterPayload(t)
	md := GenerateLetterMarkdown(p)
	order := []string{"Northwind Bank AG", "Jane Doe", "Our reference: NWB\\-REG\\-2026\\-041", "28 October 2026",
		"**Action plan addressing AI\\-enabled", "Dear Ms Doe,", "### Approval and resources", "Yours sincerely,",
		"**John Smith**\\\nChief Executive Officer", "**Maria Rossi**", "**Enclosures**", "**Copy to**",
		"## Annex 1: Action plan", "| Focus area | Measures |"}
	at := 0
	for _, want := range order {
		i := strings.Index(md[at:], want)
		if i < 0 {
			t.Fatalf("%q is missing or out of order in:\n%s", want, md)
		}
		at += i + len(want)
	}
	if strings.Contains(md, "DRAFT") || !strings.HasPrefix(md, "**CONFIDENTIAL**") {
		t.Errorf("an approved letter is marked with its classification only:\n%s", md[:80])
	}
	for _, heading := range []string{"Letter", "Sender", "Addressee", "Signatories"} {
		if strings.Contains(md, "## "+heading+"\n") {
			t.Errorf("the %q section heading is printed", heading)
		}
	}

	p.Status, p.EffectiveDate = "draft", ""
	md = GenerateLetterMarkdown(p)
	if !strings.HasPrefix(md, "**DRAFT — NOT APPROVED CONFIDENTIAL**") || !strings.Contains(md, "**Date not set**") {
		t.Errorf("a draft without a date is not marked as one:\n%s", md[:120])
	}
}

func TestGenerateLetterMarkdownFromTheSectionEditor(t *testing.T) {
	p := PolicyPayload{Title: "Subject", Classification: "Internal", Status: "approved", EffectiveDate: "2026-10-28", Sections: []PolicySection{
		{SectionKind: "addressee", Body: "European Central Bank\nSonnemannstrasse 20"},
		{SectionKind: "letter_body", Body: "Dear Ms Doe,\n\nThe Bank will report # quarterly."},
		{SectionKind: "signatories", Body: "A. Signer\nChief Executive Officer\n\nB. Signer"},
	}}
	md := GenerateLetterMarkdown(p)
	for _, want := range []string{"European Central Bank\\\nSonnemannstrasse 20\n\n", "The Bank will report \\# quarterly\\.\n\n",
		"A\\. Signer\\\nChief Executive Officer\n\n", "B\\. Signer\n\n"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestLetterWordBundleCarriesTheGeneratedMarkdown(t *testing.T) {
	withRepoTemplates(t)
	withEngines(t, noEngines())
	result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "letter-docx", Data: mustSample(t, "letter-docx"), BaseName: "NWB-REG-2026-041"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if md := zipEntry(t, result.Body, "letter.md"); !strings.Contains(md, "Yours sincerely,") {
		t.Errorf("the bundle does not carry the generated letter:\n%s", md)
	}
}

func TestRenderProducesAWordLetter(t *testing.T) {
	bin := pandocBinary(t)
	withRepoTemplates(t)
	engines := noEngines()
	engines[EnginePandoc] = EngineStatus{Engine: EnginePandoc, Command: "pandoc", Path: bin, Available: true}
	withEngines(t, engines)
	result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "letter-docx", Data: mustSample(t, "letter-docx"), BaseName: "NWB-REG-2026-041"})
	if err != nil {
		t.Fatalf("Render: %v\n%s", err, result.Log)
	}
	if result.Bundled || result.Filename != "NWB-REG-2026-041.docx" {
		t.Fatalf("got %q bundled=%v", result.Filename, result.Bundled)
	}
	zr, err := zip.NewReader(bytes.NewReader(result.Body), int64(len(result.Body)))
	if err != nil {
		t.Fatalf("not a docx: %v", err)
	}
	var doc bytes.Buffer
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			_, _ = doc.ReadFrom(rc)
			_ = rc.Close()
		}
	}
	for _, want := range []string{"Dear Ms Doe,", "Yours sincerely,", "Chief Information Security Officer", "Annex 1: Action plan"} {
		if !strings.Contains(doc.String(), want) {
			t.Errorf("the Word letter has no %q", want)
		}
	}
}

// With a typst binary available (TYPST or PATH), the letter compiles from the
// sample, and from a payload whose every part is full of Typst syntax, which
// is printed rather than run: had any of it been evaluated, #panic would fail
// the compile and #read would reach outside the render root.
func TestTypstRendersALetterAndItsHostileTextAsText(t *testing.T) {
	bin := os.Getenv("TYPST")
	if bin == "" {
		bin, _ = exec.LookPath("typst")
	}
	if bin == "" {
		t.Skip("typst is not installed")
	}
	root := t.TempDir()
	for _, f := range []string{"brand.typ", "lib.typ", "letter.typ"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "templates", "typst", f)) // #nosec G304 -- the repository's own templates
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hostileLetter := PolicyPayload{Title: `#panic("title")`, Reference: `\input{x}`, Classification: `#panic("c")`,
		EffectiveDate: `#read("/etc/passwd")`, ClientName: `#panic("client")`, Frameworks: []string{}, Controls: []PolicyControl{}, Versions: []PolicyVersion{}}
	for _, kind := range []string{"sender", "addressee", "letter_body", "other", "signatories", "enclosures", "copy_to", "annex"} {
		s := hostileSection()
		s.SectionKind = kind
		hostileLetter.Sections = append(hostileLetter.Sections, s)
	}
	hostileLetter.Sections = append(hostileLetter.Sections, PolicySection{SectionKind: "signatories", Body: `#panic("body")` + "\n" + `\input{x}`, Controls: []PolicyClaim{}})
	hostile, _ := json.Marshal(hostileLetter)

	for name, payload := range map[string][]byte{"sample": mustSample(t, "letter-typst"), "hostile": hostile} {
		if err := os.WriteFile(filepath.Join(root, "data.json"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(root, name+".pdf")
		cmd := exec.Command(bin, "compile", "--root", root, "--input", "data=/data.json", // #nosec G204 -- test-only, fixed arguments
			filepath.Join(root, "letter.typ"), out)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("typst refused the %s letter: %v\n%s", name, err, output)
		}
		pdftotext, err := exec.LookPath("pdftotext")
		if err != nil {
			continue
		}
		text, err := exec.Command(pdftotext, out, "-").Output() // #nosec G204 -- test-only
		if err != nil {
			continue
		}
		want := map[string]string{"sample": "Yours sincerely,", "hostile": `#panic("pwned")`}[name]
		if !strings.Contains(string(text), want) {
			t.Errorf("the %s letter does not print %q", name, want)
		}
		if name == "hostile" && !strings.Contains(string(text), "DRAFT — NOT APPROVED") {
			t.Error("a letter that is not approved is not marked as a draft")
		}
	}
}

func mustSample(t *testing.T, id string) []byte {
	t.Helper()
	withRepoTemplates(t)
	tpl, _ := Lookup(id)
	data, err := SampleData(tpl)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
