package doctemplate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hostileStrings replaces every string in a JSON value with Typst code, so a
// template that evaluated any payload string would panic or read a file.
func hostileStrings(v any) any {
	switch x := v.(type) {
	case string:
		return `#panic("pwned") #read("/etc/passwd")`
	case []any:
		for i := range x {
			x[i] = hostileStrings(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = hostileStrings(x[k])
		}
		return x
	}
	return v
}

// TestTypstRendersTheAssurancePlanDocuments compiles the approval pack and
// the quarterly and annual reports from their samples, and from the same
// payloads with every string replaced by Typst code, which must print.
func TestTypstRendersTheAssurancePlanDocuments(t *testing.T) {
	bin := os.Getenv("TYPST")
	if bin == "" {
		bin, _ = exec.LookPath("typst")
	}
	if bin == "" {
		t.Skip("typst is not installed")
	}
	root := t.TempDir()
	for _, f := range []string{"brand.typ", "lib.typ", "assurance-plan-pack.typ", "business-report.typ"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "templates", "typst", f)) // #nosec G304 -- the repository's own templates
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ name, entry, sample, want string }{
		{"pack", "assurance-plan-pack.typ", "assurance-plan-sample.json", "What the regulation asks, and how the plan meets it"},
		{"quarterly", "business-report.typ", "assurance-quarterly-sample.json", "Coverage against plan"},
		{"annual", "business-report.typ", "assurance-annual-sample.json", "Input to the ICT risk management framework review report"},
	} {
		sample, err := os.ReadFile(filepath.Join("..", "..", "templates", "samples", c.sample)) // #nosec G304 -- the repository's own samples
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(sample, &v); err != nil {
			t.Fatalf("%s: %v", c.sample, err)
		}
		hostile, _ := json.Marshal(hostileStrings(v))
		for variant, payload := range map[string][]byte{"sample": sample, "hostile": hostile} {
			if err := os.WriteFile(filepath.Join(root, "data.json"), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(root, c.name+"-"+variant+".pdf")
			cmd := exec.Command(bin, "compile", "--root", root, "--input", "data=/data.json", // #nosec G204 -- test-only, fixed arguments
				filepath.Join(root, c.entry), out)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("typst refused the %s %s: %v\n%s", variant, c.name, err, output)
			}
			pdftotext, err := exec.LookPath("pdftotext")
			if err != nil {
				continue
			}
			text, err := exec.Command(pdftotext, out, "-").Output() // #nosec G204 -- test-only
			if err != nil {
				continue
			}
			want := c.want
			if variant == "hostile" {
				want = `#panic("pwned")`
			}
			if !strings.Contains(strings.Join(strings.Fields(string(text)), " "), want) {
				t.Errorf("the %s %s does not print %q", variant, c.name, want)
			}
		}
	}
}

func TestAssurancePackIsRegistered(t *testing.T) {
	tpl, ok := Lookup("assurance-pack-typst")
	if !ok || tpl.Kind != KindAssurancePack || tpl.Engine != EngineTypst || tpl.Entry != "assurance-plan-pack.typ" {
		t.Fatalf("assurance-pack-typst = %+v, %v", tpl, ok)
	}
	var payload struct {
		Reference string `json:"reference"`
		Pack      struct {
			Roadmap []any `json:"roadmap"`
		} `json:"pack"`
	}
	if err := json.Unmarshal(mustSample(t, tpl.ID), &payload); err != nil || payload.Reference == "" || len(payload.Pack.Roadmap) == 0 {
		t.Errorf("the pack's sample is not a pack payload: %v", err)
	}
	if _, err := os.Stat(filepath.Join("..", "..", "templates", tpl.Sources[0])); err != nil {
		t.Errorf("source: %v", err)
	}
}
