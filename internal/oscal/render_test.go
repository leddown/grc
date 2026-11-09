package oscal

import (
	"strings"
	"testing"
)

func strp(values ...string) *[]string { return &values }

func TestRenderParametersAndLinksIntoCatalogText(t *testing.T) {
	params := map[string]Parameter{
		"time":   {ID: "time", Label: "time period"},
		"od":     {ID: "od", Label: "organization-defined personnel or roles"},
		"lock":   {ID: "lock", Select: &ParameterSelection{HowMany: "one-or-more", Choice: strp("lock the account for {{ insert: param, time }} ", "notify the administrator")}},
		"single": {ID: "single", Select: &ParameterSelection{Choice: strp("remove", "disable")}},
	}
	cases := []struct{ in, want string }{
		{"Notify {{ insert: param, od }} within {{ insert: param, time }} ; and",
			"Notify [Assignment: organization-defined personnel or roles] within [Assignment: organization-defined time period]; and"},
		{"Automatically {{ insert: param, lock }} when exceeded.",
			"Automatically [Selection (one or more): lock the account for [Assignment: organization-defined time period]; notify the administrator] when exceeded."},
		{"Automatically {{ insert: param, single }} accounts.", "Automatically [Selection: remove; disable] accounts."},
		{"as defined in [AU-2a](#au-2_smt.a) ; and in [AU-3](#au-3).", "as defined in AU-2a; and in AU-3."},
		{"before {{ insert: param, time }} (if any)", "before [Assignment: organization-defined time period] (if any)"},
	}
	for _, c := range cases {
		r := newTextRenderer(params)
		if got := r.text(c.in, 0); got != c.want {
			t.Errorf("%q\n got  %q\n want %q", c.in, got, c.want)
		}
		if len(r.missing) != 0 {
			t.Errorf("%q: unexpected missing parameters %v", c.in, r.missing)
		}
	}
}

func TestRenderKeepsAnUnknownParameterAndReportsIt(t *testing.T) {
	r := newTextRenderer(map[string]Parameter{})
	in := "Use {{ insert: param, nowhere }}."
	if got := r.text(in, 0); got != in {
		t.Fatalf("got %q", got)
	}
	problems := r.missingProblems("AC-99")
	if len(problems) != 1 || !strings.Contains(problems[0], "nowhere") {
		t.Fatalf("problems %v", problems)
	}
}

func TestRenderSurvivesAParameterThatInsertsItself(t *testing.T) {
	r := newTextRenderer(map[string]Parameter{"loop": {ID: "loop", Label: "{{ insert: param, loop }}"}})
	_ = r.text("x {{ insert: param, loop }}", 0)
	if !r.missing["loop"] {
		t.Fatal("a self-inserting parameter must be reported, not rendered forever")
	}
}

// An enhancement can insert a parameter its parent defines: NIST's IA-13(3)
// inserts ia-13_odp.01.
func TestImportResolvesAParameterDefinedByAnotherControl(t *testing.T) {
	catalog := `{"catalog":{"uuid":"00000000-0000-4000-8000-000000000001","metadata":{"title":"t","last-modified":"2026-01-01T00:00:00Z","version":"1","oscal-version":"1.2.3"},
"groups":[{"id":"ia","title":"Identification and Authentication","controls":[
 {"id":"ia-13","title":"Identity Providers","props":[{"name":"label","value":"IA-13"}],
  "params":[{"id":"ia-13_odp.01","label":"policy"}],
  "parts":[{"id":"ia-13_smt","name":"statement","prose":"Employ providers in accordance with {{ insert: param, ia-13_odp.01 }}."}],
  "controls":[{"id":"ia-13.3","title":"Access Tokens","props":[{"name":"label","value":"IA-13(3)"}],
   "parts":[{"id":"ia-13.3_smt","name":"statement","prose":"In accordance with {{ insert: param, ia-13_odp.01 }}, tokens are:","parts":[
    {"id":"ia-13.3_smt.a","name":"item","props":[{"name":"label","value":"(a)"}],"prose":"generated; and"},
    {"id":"ia-13.3_smt.b","name":"item","props":[{"name":"label","value":"(b)"}],"prose":"revoked."}]}]}]}]}]}}`
	imp, err := ReadCatalogImport([]byte(catalog), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Problems) != 0 {
		t.Fatalf("problems %v", imp.Problems)
	}
	if len(imp.Controls) != 2 {
		t.Fatalf("controls %d", len(imp.Controls))
	}
	child := imp.Controls[1]
	want := "In accordance with [Assignment: organization-defined policy], tokens are:\n(a) generated; and\n(b) revoked."
	if child.ID != "IA-13(3)" || child.Type != TypeEnhancement || child.Requirements != want {
		t.Fatalf("got %s %s %q", child.ID, child.Type, child.Requirements)
	}
	if child.ParamsJSON != "null" {
		t.Fatalf("the enhancement defines no parameters of its own: %s", child.ParamsJSON)
	}
}
