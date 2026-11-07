package assuranceplan

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// walkJSON calls fn on every key path and value of a decoded JSON document.
func walkJSON(path string, v any, fn func(path string, v any)) {
	fn(path, v)
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			walkJSON(path+"."+k, child, fn)
		}
	case []any:
		for _, child := range x {
			walkJSON(path+"[]", child, fn)
		}
	}
}

func exportProblems(t *testing.T, e PlanExport) []string {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var problems []string
	walkJSON("", v, func(path string, v any) {
		key := path[strings.LastIndex(path, ".")+1:]
		if v == nil {
			problems = append(problems, path+" is null")
		}
		if key == "id" || strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_ids") && key != "control_ids" {
			problems = append(problems, path+" is a database id")
		}
	})
	return problems
}

func TestExportHasNoIDsAndNoNullCollections(t *testing.T) {
	svc, _ := newIntegratedService(t)
	fresh, err := svc.Create(CreateInput{Type: "controls-testing", Title: "Empty plan", PeriodStart: "2027-01-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	e, err := svc.Export(fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p := exportProblems(t, e); len(p) > 0 {
		t.Errorf("a new plan's export: %v", p)
	}

	p := sampleYear(t, svc, 1)
	e, err = svc.Export(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p := exportProblems(t, e); len(p) > 0 {
		t.Errorf("a plan in Q1's export: %v", p)
	}
	if e.FormatVersion != ExportFormatVersion || e.Reference != p.Reference || len(e.Population) != 320 || len(e.Tests) != 17 {
		t.Errorf("export: v%d %s, %d items, %d tests", e.FormatVersion, e.Reference, len(e.Population), len(e.Tests))
	}
	if e.Seed != "" || e.SeedCommitment == "" {
		t.Errorf("the seed is in the export before every quarter is drawn (commitment %q)", e.SeedCommitment)
	}
	if e.Totals.TestsOpened != 17 || e.Totals.AttributeFails != 1 || e.Totals.FindingsRaised != 1 || len(e.RegulatoryBasis) != 5 {
		t.Errorf("totals = %+v, %d basis rows", e.Totals, len(e.RegulatoryBasis))
	}
	selected := 0
	for _, it := range e.Population {
		if it.SelectedQuarter == 1 {
			selected++
			if it.TestRef == "" || it.SelectionBasis == "" {
				t.Errorf("selected item %s has no test or basis: %+v", it.ItemRef, it)
			}
		}
	}
	if selected != 17 {
		t.Errorf("%d items selected in Q1", selected)
	}
}

func readCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("not CSV: %v", err)
	}
	return rows
}

func TestExportCSVTables(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 1)
	item := mustPopulation(t, svc, p.ID)[0]
	formula := `=HYPERLINK("http://x","y")`
	if _, err := svc.UpdatePopulationItem(p.ID, item.ID, PopulationPatch{Name: &formula}, "alice"); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"steps": len(p.Steps), "quarters": 4, "population": 320, "tests": 17, "deviations": 32}
	for _, table := range ExportTables {
		name, data, err := svc.ExportCSV(p.ID, table)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if name != "AP-2027-001-"+table+".csv" {
			t.Errorf("%s file is %q", table, name)
		}
		rows := readCSV(t, data)
		if len(rows)-1 != want[table] {
			t.Errorf("%s: %d rows, want %d", table, len(rows)-1, want[table])
		}
		if table == "population" && !strings.Contains(string(data), `'=HYPERLINK`) {
			t.Error("a population name starting with = is written as a formula")
		}
		if table == "quarters" && !strings.Contains(strings.Join(rows[0], ","), "T1_cumulative_target_pct") {
			t.Errorf("quarters header = %v", rows[0])
		}
		if table == "tests" {
			for _, r := range rows[1:] {
				if r[0] == "T27-Q1-001" && !strings.Contains(strings.Join(r, ","), "NFR-04") {
					t.Errorf("T27-Q1-001 does not name its failed attribute: %v", r)
				}
			}
		}
	}
	if _, _, err := svc.ExportCSV(p.ID, "secrets"); err == nil {
		t.Error("an unknown table was exported")
	}
}

func TestSpreadsheetSafe(t *testing.T) {
	for in, want := range map[string]string{"=1+1": "'=1+1", "+44 20": "'+44 20", "-x": "'-x", "@sum": "'@sum", "\tx": "'\tx", "plain": "plain", "": ""} {
		if got := spreadsheetSafe(in); got != want {
			t.Errorf("spreadsheetSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

type rendered struct {
	templateID, base string
	data             []byte
}

func TestDocumentRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 1)
	var got []rendered
	h := NewHandler(svc, func(*gin.Context) string { return "tess" }).WithRenderer(func(c *gin.Context, templateID string, data []byte, base string) {
		got = append(got, rendered{templateID, base, data})
		c.Data(http.StatusOK, "application/pdf", []byte("%PDF"))
	})
	r := gin.New()
	h.RegisterRoutes(r)
	base := "/assurance-plans/" + strconv.FormatInt(p.ID, 10)

	for _, c := range []struct {
		path, template, base, reference string
	}{
		{"/pack.pdf", PackTemplateID, "AP-2027-001-pack", "AP-2027-001"},
		{"/report/quarter/2", ReportTemplateID, "AP-2027-001-Q2", "AP-2027-001-Q2"},
		{"/report/quarter/Q1.pdf", ReportTemplateID, "AP-2027-001-Q1", "AP-2027-001-Q1"},
		{"/report/annual.pdf", ReportTemplateID, "AP-2027-001-ANNUAL", "AP-2027-001-ANNUAL"},
	} {
		got = nil
		w := do(t, r, http.MethodGet, base+c.path, nil)
		if w.Code != http.StatusOK || len(got) != 1 {
			t.Fatalf("%s = %d, %d renders", c.path, w.Code, len(got))
		}
		var payload struct {
			Reference string `json:"reference"`
		}
		if err := json.Unmarshal(got[0].data, &payload); err != nil || got[0].templateID != c.template || got[0].base != c.base || payload.Reference != c.reference {
			t.Errorf("%s rendered %s as %s with reference %q (%v)", c.path, got[0].templateID, got[0].base, payload.Reference, err)
		}
	}

	got = nil
	w := do(t, r, http.MethodGet, base+"/report/annual.pdf?format=json", nil)
	if w.Code != http.StatusOK || len(got) != 0 || !strings.Contains(w.Body.String(), `"limitations"`) {
		t.Errorf("?format=json = %d, %d renders", w.Code, len(got))
	}
	for path, code := range map[string]int{
		"/report/quarter/5": http.StatusBadRequest, "/report/quarter/Qx": http.StatusBadRequest,
		"/export/steps.txt": http.StatusNotFound, "/export/secrets.csv": http.StatusBadRequest,
	} {
		if w := do(t, r, http.MethodGet, base+path, nil); w.Code != code {
			t.Errorf("%s = %d, want %d", path, w.Code, code)
		}
	}
	if w := do(t, r, http.MethodGet, "/assurance-plans/999/pack.pdf", nil); w.Code != http.StatusNotFound {
		t.Errorf("an unknown plan's pack = %d", w.Code)
	}

	w = do(t, r, http.MethodGet, base+"/export.json", nil)
	var e PlanExport
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Reference != "AP-2027-001" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), `AP-2027-001.json`) {
		t.Errorf("export.json = %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	w = do(t, r, http.MethodGet, base+"/export/tests.csv", nil)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || len(readCSV(t, w.Body.Bytes())) != 18 {
		t.Errorf("tests.csv = %d %s", w.Code, w.Header().Get("Content-Type"))
	}

	bare := gin.New()
	NewHandler(svc, func(*gin.Context) string { return "tess" }).RegisterRoutes(bare)
	if w := do(t, bare, http.MethodGet, base+"/pack.pdf", nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("a pack with no renderer = %d, want 503", w.Code)
	}
	if w := do(t, bare, http.MethodGet, base+"/pack.pdf?format=json", nil); w.Code != http.StatusOK {
		t.Errorf("the pack's data with no renderer = %d", w.Code)
	}
}

func TestRegulatoryBasisInGovernance(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	if len(p.Basis) != 5 || p.Basis[0].Reference != "DORA Art. 6(4)" {
		t.Fatalf("the example's basis = %+v", p.Basis)
	}
	g := Governance{RACIRoles: p.RACIRoles, RACI: p.RACI, Cadence: p.Cadence, Escalations: p.Escalations}
	kept, err := svc.ReplaceGovernance(p.ID, g, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Basis) != 5 {
		t.Errorf("a governance save without regulatory_basis dropped the rows: %d left", len(kept.Basis))
	}
	g.Basis = []BasisRow{{Requirement: "  Test CIF systems yearly ", Reference: "DORA Art. 24(6)", How: "Quarterly sampling"}, {}}
	one, err := svc.ReplaceGovernance(p.ID, g, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Basis) != 1 || one.Basis[0].Requirement != "Test CIF systems yearly" {
		t.Errorf("basis after replace = %+v", one.Basis)
	}
	g.Basis = []BasisRow{}
	none, err := svc.ReplaceGovernance(p.ID, g, "alice")
	if err != nil || len(none.Basis) != 0 {
		t.Errorf("an empty regulatory_basis did not clear the rows: %+v, %v", none.Basis, err)
	}
	g.Basis = []BasisRow{{Requirement: strings.Repeat("x", maxFieldLength+1)}}
	if _, err := svc.ReplaceGovernance(p.ID, g, "alice"); err == nil {
		t.Error("an over-long requirement was saved")
	}

	g.Basis = []BasisRow{{Requirement: "Review the framework yearly", Reference: "DORA Art. 6(5)", How: "Annual conclusion"}}
	if _, err := svc.ReplaceGovernance(p.ID, g, "alice"); err != nil {
		t.Fatal(err)
	}
	loadExamplePopulation(t, svc, p.ID)
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(p.ID, "bob", ""); err != nil {
		t.Fatal(err)
	}
	v1, err := svc.repo.Version(p.ID, 1)
	if err != nil || !strings.Contains(v1.SnapshotJSON, "Review the framework yearly") {
		t.Errorf("the approval snapshot does not hold the basis (%v)", err)
	}
	clone, err := svc.Clone(p.ID, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(clone.Basis) != 1 || clone.Basis[0].Reference != "DORA Art. 6(5)" || clone.Basis[0].ID == p.Basis[0].ID {
		t.Errorf("clone basis = %+v", clone.Basis)
	}
}
