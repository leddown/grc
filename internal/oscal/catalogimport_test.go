package oscal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	fixtureCatalog  = "testdata/NIST_SP-800-53_rev5_catalog_cut.json"
	fixtureLow      = "testdata/NIST_SP-800-53_rev5_LOW-baseline_profile_cut.json"
	fixtureModerate = "testdata/NIST_SP-800-53_rev5_MODERATE-baseline_profile_cut.json"
	shippedCatalog  = "../data/merged_nist_controls_master_replaced_from_controls_all.json"
)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func fixtureImport(t *testing.T) CatalogImport {
	t.Helper()
	imp, err := ReadCatalogImport(readFixture(t, fixtureCatalog), map[string][]byte{
		"low":      readFixture(t, fixtureLow),
		"moderate": readFixture(t, fixtureModerate),
	})
	if err != nil {
		t.Fatal(err)
	}
	return imp
}

func byID(imp CatalogImport) map[string]ImportedControl {
	out := map[string]ImportedControl{}
	for _, c := range imp.Controls {
		out[c.ID] = c
	}
	return out
}

func TestImportReadsTheSourceMetadata(t *testing.T) {
	raw := readFixture(t, fixtureCatalog)
	imp := fixtureImport(t)
	sum := sha256.Sum256(raw)
	want := SourceInfo{
		Title:        "Electronic (OSCAL) Version of NIST SP 800-53 Rev 5.2.0 Controls and SP 800-53A Rev 5.2.0 Assessment Procedures",
		Version:      "5.2.0",
		OSCALVersion: "1.2.3",
		LastModified: "2026-10-22T11:30:00Z",
		UUID:         imp.Source.UUID,
		SHA256:       hex.EncodeToString(sum[:]),
		Bytes:        len(raw),
	}
	if imp.Source != want {
		t.Fatalf("source\n got  %+v\n want %+v", imp.Source, want)
	}
	if imp.Source.UUID == "" {
		t.Fatal("the catalog uuid was not read")
	}
	if len(imp.Problems) != 0 {
		t.Fatalf("problems: %v", imp.Problems)
	}
}

func TestImportMapsControlsAsTheCatalogStoresThem(t *testing.T) {
	imp := fixtureImport(t)
	var ids []string
	for _, c := range imp.Controls {
		ids = append(ids, c.ID)
	}
	if want := []string{"AC-1", "AC-2", "AC-2(1)", "AC-13", "SA-24"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids %v want %v", ids, want)
	}
	got := byID(imp)

	ac1 := got["AC-1"]
	if ac1.OSCALID != "ac-1" || ac1.Type != TypeControl || ac1.Name != "Policy and Procedures" || ac1.Withdrawn {
		t.Fatalf("AC-1 %+v", ac1)
	}
	if want := []string{"IA-1", "PM-9", "PM-24", "PS-8", "SI-12"}; !reflect.DeepEqual(ac1.Related, want) {
		t.Fatalf("AC-1 related %v want %v", ac1.Related, want)
	}
	if !strings.HasPrefix(ac1.Discussion, "Access control policy and procedures address the controls in the AC family") {
		t.Fatalf("AC-1 discussion %q", ac1.Discussion)
	}

	enh := got["AC-2(1)"]
	if enh.Type != TypeEnhancement || enh.Name != "Automated System Account Management" {
		t.Fatalf("AC-2(1) %+v", enh)
	}
	if want := "Support the management of system accounts using [Assignment: organization-defined automated mechanisms]."; enh.Requirements != want {
		t.Fatalf("AC-2(1) requirements %q", enh.Requirements)
	}

	// Withdrawn: the name is kept and there is nothing else, which is how the
	// catalog has always held a withdrawn control.
	ac13 := got["AC-13"]
	if !ac13.Withdrawn || ac13.Name != "Supervision and Review — Access Control" || ac13.Requirements != "" || ac13.Discussion != "" || len(ac13.Related) != 0 {
		t.Fatalf("AC-13 %+v", ac13)
	}
	if ac13.ParamsJSON != "null" || ac13.StatementJSON != "null" || ac13.AssessmentJSON != "null" {
		t.Fatalf("AC-13 carries structure: %s %s %s", ac13.ParamsJSON, ac13.StatementJSON, ac13.AssessmentJSON)
	}

	sa24 := got["SA-24"]
	if sa24.Type != TypeControl || sa24.Name != "Design For Cyber Resiliency" || sa24.Requirements == "" {
		t.Fatalf("SA-24 %+v", sa24)
	}
}

// The text is what the Exceptions page, the reports and the workbook read, so
// the import must render it exactly as the catalog file grc ships holds it.
func TestRenderedRequirementsMatchTheShippedCatalog(t *testing.T) {
	var shipped map[string]struct {
		NIST struct {
			ControlID    string `json:"control_id"`
			Requirements string `json:"nist_requirements"`
		} `json:"nist"`
	}
	if err := json.Unmarshal(readFixture(t, shippedCatalog), &shipped); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, e := range shipped {
		want[e.NIST.ControlID] = strings.TrimSpace(e.NIST.Requirements)
	}
	got := byID(fixtureImport(t))
	for _, id := range []string{"AC-1", "AC-2", "AC-2(1)", "AC-13"} {
		if got[id].Requirements != want[id] {
			t.Errorf("%s\n got:\n%s\n shipped:\n%s", id, got[id].Requirements, want[id])
		}
	}
}

func TestImportKeepsTheUpstreamStructure(t *testing.T) {
	ac1 := byID(fixtureImport(t))["AC-1"]

	var params []Parameter
	if err := json.Unmarshal([]byte(ac1.ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 9 || params[0].ID != "ac-1_prm_1" || params[0].Props == nil {
		t.Fatalf("params %d, first %+v", len(params), params[0])
	}
	var aggregates []string
	for _, p := range *params[0].Props {
		if p.Name == "aggregates" {
			aggregates = append(aggregates, p.Value)
		}
	}
	if want := []string{"ac-01_odp.01", "ac-01_odp.02"}; !reflect.DeepEqual(aggregates, want) {
		t.Fatalf("aggregates %v", aggregates)
	}
	if params[3].Select == nil || params[3].Select.HowMany != "one-or-more" || len(*params[3].Select.Choice) != 3 {
		t.Fatalf("ac-01_odp.03 selection %+v", params[3].Select)
	}

	var statement Part
	if err := json.Unmarshal([]byte(ac1.StatementJSON), &statement); err != nil {
		t.Fatal(err)
	}
	if statement.ID != "ac-1_smt" || statement.Name != "statement" || statement.Parts == nil || len(*statement.Parts) != 3 {
		t.Fatalf("statement %+v", statement)
	}

	var assessment []Part
	if err := json.Unmarshal([]byte(ac1.AssessmentJSON), &assessment); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range assessment {
		names = append(names, p.Name+":"+p.ID)
	}
	if want := []string{"assessment-objective:ac-1_obj", "assessment-method:ac-1_asm-examine", "assessment-method:ac-1_asm-interview"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("assessment parts %v", names)
	}
}

func TestImportReadsBaselinesFromThePostedProfiles(t *testing.T) {
	imp := fixtureImport(t)
	if want := []string{"AC-1", "AC-2"}; !reflect.DeepEqual(imp.Baselines["low"], want) {
		t.Fatalf("low %v", imp.Baselines["low"])
	}
	if want := []string{"AC-1", "AC-2", "AC-2(1)"}; !reflect.DeepEqual(imp.Baselines["moderate"], want) {
		t.Fatalf("moderate %v", imp.Baselines["moderate"])
	}
	if _, ok := imp.Baselines["high"]; ok {
		t.Fatal("no HIGH profile was posted, so there must be no HIGH baseline")
	}
	if imp.Profiles["low"].Version != "5.2.0" || imp.Profiles["low"].SHA256 == "" {
		t.Fatalf("low profile %+v", imp.Profiles["low"])
	}
}

func TestImportReportsProfileProblems(t *testing.T) {
	cat := readFixture(t, fixtureCatalog)
	moderate := readFixture(t, fixtureModerate)

	imp, err := ReadCatalogImport(cat, map[string][]byte{"low": moderate})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Problems) != 1 || !strings.Contains(imp.Problems[0], "MODERATE slot") {
		t.Fatalf("a MODERATE profile posted as LOW must be a problem: %v", imp.Problems)
	}

	unknown := strings.Replace(string(moderate), `"ac-2.1"`, `"zz-9"`, 1)
	imp, err = ReadCatalogImport(cat, map[string][]byte{"moderate": []byte(unknown), "extra": moderate})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(imp.Problems, "\n")
	if !strings.Contains(joined, "zz-9") || !strings.Contains(joined, `"extra" is not a baseline`) {
		t.Fatalf("problems %v", imp.Problems)
	}

	imp, err = ReadCatalogImport(cat, map[string][]byte{"high": []byte("{not json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Problems) != 1 || !strings.Contains(imp.Problems[0], "HIGH profile") {
		t.Fatalf("an unreadable profile must be a problem: %v", imp.Problems)
	}
}

func TestImportRefusesWhatIsNotACatalog(t *testing.T) {
	if _, err := ReadCatalogImport(readFixture(t, fixtureLow), nil); err == nil || !strings.Contains(err.Error(), "not an OSCAL catalog") {
		t.Fatalf("a profile posted as the catalog: %v", err)
	}
	if _, err := ReadCatalogImport([]byte("[1,2]"), nil); err == nil {
		t.Fatal("JSON that is not OSCAL must be refused")
	}
}

func TestImportIsDeterministic(t *testing.T) {
	a, b := fixtureImport(t), fixtureImport(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two reads of the same files differ")
	}
}
