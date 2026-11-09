package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"grc/internal/oscal"
	"grc/internal/oscal/oscaltest"
	"grc/internal/regcoverage"
)

var lastModifiedField = regexp.MustCompile(`"last-modified":"[^"]*"`)

func oscalDownload(t *testing.T, a *fullApp, path, wantKind string) []byte {
	t.Helper()
	rec := a.asAdmin(http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	want := regexp.MustCompile(`^attachment; filename="grc-` + regexp.QuoteMeta(wantKind) + `-\d{4}-\d{2}-\d{2}\.oscal\.json"$`)
	if got := rec.Header().Get("Content-Disposition"); !want.MatchString(got) {
		t.Errorf("GET %s: Content-Disposition %q", path, got)
	}
	body := rec.Body.Bytes()
	if err := oscaltest.Validate(body); err != nil {
		t.Errorf("GET %s: %v", path, err)
	}
	return body
}

// The catalog grc exports imports back into the catalog it came from without
// a single change: controls loaded from the JSON file (text only), controls
// imported from NIST's OSCAL (with their structure), one edited by hand after
// that, and all four baselines.
func TestOSCALCatalogExportRoundTripsWithoutAChange(t *testing.T) {
	a := newFullApp(t, Options{})
	shipped, err := os.ReadFile(filepath.Join("..", "data", "merged_nist_controls_master_replaced_from_controls_all.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := a.asAdmin(http.MethodPost, "/controls/import", string(shipped)); rec.Code != http.StatusOK {
		t.Fatalf("JSON import: %d %s", rec.Code, rec.Body.String())
	}
	files := map[string]any{
		"catalog": oscalFixture(t, "NIST_SP-800-53_rev5_catalog_cut.json"),
		"profiles": map[string]string{
			"low":      oscalFixture(t, "NIST_SP-800-53_rev5_LOW-baseline_profile_cut.json"),
			"moderate": oscalFixture(t, "NIST_SP-800-53_rev5_MODERATE-baseline_profile_cut.json"),
		},
	}
	applyOSCAL(t, a, files)

	rec := a.asAdmin(http.MethodGet, "/controls/data/AC-2", "")
	var ac2 map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ac2); err != nil {
		t.Fatal(err)
	}
	if ac2["statement"] == nil {
		t.Fatal("AC-2 should hold NIST's statement after the OSCAL import")
	}
	// The editor's save: the control as read, with the CIA flags as booleans
	// and the requirements edited.
	for _, flag := range []string{"confidentiality", "integrity", "availability"} {
		ac2[flag] = ac2[flag] == "Yes"
	}
	ac2["requirements"] = "a. Define the account types allowed and prohibited on the system, edited by hand."
	edited, _ := json.Marshal(ac2)
	if rec := a.asAdmin(http.MethodPut, "/controls/AC-2", string(edited)); rec.Code != http.StatusOK {
		t.Fatalf("edit AC-2: %d %s", rec.Code, rec.Body.String())
	}

	catalog := oscalDownload(t, a, "/controls/oscal/catalog.json", "sp800-53-catalog")
	profiles := map[string]string{}
	for _, b := range []string{"low", "moderate", "high", "privacy"} {
		profiles[b] = string(oscalDownload(t, a, "/controls/oscal/profile/"+b+".json", "sp800-53-"+b+"-profile"))
	}
	if again := a.asAdmin(http.MethodGet, "/controls/oscal/catalog.json", "").Body.Bytes(); lastModifiedField.ReplaceAllString(string(again), "") != lastModifiedField.ReplaceAllString(string(catalog), "") {
		t.Error("two exports of the same catalog differ beyond last-modified")
	}
	// grc's own analysis of AC-2 travels in the grc namespace: the shipped
	// threats as one prop each, the CIA flags, and the appendix with References.
	for _, want := range []string{
		`{"name":"threat","ns":"` + oscal.Namespace + `","value":"Spoofing"}`,
		`{"name":"confidentiality","ns":"` + oscal.Namespace + `","value":"yes"}`,
		`"id":"ac-2_apx","name":"appendix","ns":"` + oscal.Namespace + `"`,
		`"name":"references","ns":"` + oscal.Namespace + `"`,
	} {
		if !strings.Contains(string(catalog), want) {
			t.Errorf("the catalog export lacks %s", want)
		}
	}
	if !strings.Contains(profiles["high"], "applies this baseline to TIER 1 and TIER 2 assets") {
		t.Error("the HIGH profile should record the asset tiers it serves in its remarks")
	}

	body, _ := json.Marshal(map[string]any{"catalog": string(catalog), "profiles": profiles})
	rec = a.asAdmin(http.MethodPost, "/controls/import-oscal/preview", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview of the export: %d %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Problems  []string          `json:"problems"`
		Added     []json.RawMessage `json:"added"`
		Changed   []json.RawMessage `json:"changed"`
		Unchanged []string          `json:"unchanged"`
		Absent    []string          `json:"absent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Problems)+len(preview.Added)+len(preview.Changed)+len(preview.Absent) != 0 {
		t.Fatalf("the export does not import back unchanged: problems %v, added %d, changed %d (first %s), absent %v",
			preview.Problems, len(preview.Added), len(preview.Changed), first(preview.Changed), preview.Absent)
	}
	if len(preview.Unchanged) != 1194 {
		t.Errorf("%d controls unchanged, want the 1,193 shipped and SA-24", len(preview.Unchanged))
	}

	if rec := a.asAdmin(http.MethodGet, "/controls/oscal/profile/extreme.json", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown baseline: %d", rec.Code)
	}
	if !strings.Contains(a.asAdmin(http.MethodGet, "/controls", "").Body.String(), `href="/controls/oscal/profile/privacy.json"`) {
		t.Error("the Controls page should link the exports")
	}
}

func first(raw []json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw[0])
}

func applyOSCAL(t *testing.T, a *fullApp, files map[string]any) {
	t.Helper()
	body, _ := json.Marshal(files)
	rec := a.asAdmin(http.MethodPost, "/controls/import-oscal/preview", string(body))
	var preview struct {
		Base string `json:"base"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || preview.Base == "" {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	files["base"] = preview.Base
	body, _ = json.Marshal(files)
	if rec := a.asAdmin(http.MethodPost, "/controls/import-oscal", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOSCALNFRExports(t *testing.T) {
	a := newFullApp(t, Options{})
	if rec := a.asAdmin(http.MethodGet, "/security-nfrs/oscal/catalog.json", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an empty NFR catalog: %d", rec.Code)
	}
	if rec := a.asAdmin(http.MethodPost, "/controls/import", shippedEntries(t, "PL-8", "CM-8", "SI-2", "RA-5")); rec.Code != http.StatusOK {
		t.Fatalf("controls: %d %s", rec.Code, rec.Body.String())
	}
	nfrs, err := os.ReadFile(filepath.Join("..", "data", "NFR_incremental_keys_with_domain.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := a.asAdmin(http.MethodPost, "/security-nfrs/import", string(nfrs)); rec.Code != http.StatusOK {
		t.Fatalf("NFRs: %d %s", rec.Code, rec.Body.String())
	}
	catalog := oscalDownload(t, a, "/security-nfrs/oscal/catalog.json", "nfr-catalog")
	if got := strings.Count(string(catalog), `"class":"security-nfr"`); got != 109 {
		t.Errorf("%d NFRs in the catalog export, want 109", got)
	}
	mapping := oscalDownload(t, a, "/security-nfrs/oscal/mapping.json", "nfr-mapping")

	var links []struct {
		NFRKey    string `json:"nfr_key"`
		ControlID string `json:"control_id"`
		Matched   bool   `json:"matched"`
	}
	if err := json.Unmarshal(a.asAdmin(http.MethodGet, "/security-nfrs/links/data", "").Body.Bytes(), &links); err != nil {
		t.Fatal(err)
	}
	pairs := map[string]bool{}
	for _, l := range links {
		if l.Matched {
			pairs[l.NFRKey+">"+l.ControlID] = true
		}
	}
	if len(pairs) == 0 {
		t.Fatal("no NFR resolved to one of the four controls")
	}
	var doc struct {
		MC struct {
			Mappings []struct {
				Status string            `json:"status"`
				Maps   []json.RawMessage `json:"maps"`
			} `json:"mappings"`
		} `json:"mapping-collection"`
	}
	if err := json.Unmarshal(mapping, &doc); err != nil {
		t.Fatal(err)
	}
	if got := len(doc.MC.Mappings[0].Maps); got != len(pairs) {
		t.Errorf("%d maps, want one per resolved NFR-control pair (%d)", got, len(pairs))
	}
	if doc.MC.Mappings[0].Status != "not-complete" {
		t.Errorf("most NFRs name controls outside the four loaded; status %q", doc.MC.Mappings[0].Status)
	}
	if !strings.Contains(a.asAdmin(http.MethodGet, "/security-nfrs", "").Body.String(), `href="/security-nfrs/oscal/mapping.json"`) {
		t.Error("the Security NFRs page should link the exports")
	}
}

// A report version is immutable, so its exports read the snapshot and are
// dated by the version; an earlier version's mapping is superseded.
func TestOSCALRegulationExports(t *testing.T) {
	a := newFullApp(t, Options{})
	repo := regcoverage.NewSQLiteRepository(a.conn)
	reg := doraRegulation(t, repo)
	if rec := a.asAdmin(http.MethodGet, "/regulation-coverage/"+itoa(reg.ID)+"/oscal/catalog.json", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a regulation with no version: %d %s", rec.Code, rec.Body.String())
	}
	cut := func(note, createdAt string, grounded bool, refs ...string) {
		cutRegulationVersion(t, repo, reg, note, createdAt, grounded, refs...)
	}
	cut("Initial analysis", "2026-10-01T08:00:00Z", true, "RA-3")
	cut("Revised", "2026-10-05T09:30:00Z", true, "RA-3", "PM-9")

	base := "/regulation-coverage/" + itoa(reg.ID) + "/oscal/"
	id := itoa(reg.ID)
	latest := oscalDownload(t, a, base+"mapping.json", "regulation-"+id+"-v2-mapping")
	old := oscalDownload(t, a, base+"mapping.json?version=1", "regulation-"+id+"-v1-mapping")
	catalog := oscalDownload(t, a, base+"catalog.json", "regulation-"+id+"-v2-catalog")
	for name, check := range map[string]bool{
		"latest is complete with two maps":   strings.Contains(string(latest), `"status":"complete"`) && strings.Count(string(latest), `"relationship":"related"`) == 2,
		"v1 is superseded with one map":      strings.Contains(string(old), `"status":"superseded"`) && strings.Count(string(old), `"relationship":"related"`) == 1,
		"dated by the version":               strings.Contains(string(latest), `"last-modified":"2026-10-05T09:30:00Z"`),
		"v1 points at its own catalog":       strings.Contains(string(old), `/oscal/catalog.json?version=1"`),
		"the catalog holds the article text": strings.Contains(string(catalog), "Financial entities shall have a sound framework."),
	} {
		if !check {
			t.Errorf("%s", name)
		}
	}
	if again := a.asAdmin(http.MethodGet, base+"mapping.json", "").Body.String(); again != string(latest) {
		t.Error("an immutable version should export the same bytes every time")
	}
	if page := a.asAdmin(http.MethodGet, "/regulation-coverage/"+id, "").Body.String(); !strings.Contains(page, base+"mapping.json?version=2") {
		t.Error("the report page should link the exports of the version it shows")
	}

	// A finding whose quote was not verified maps nothing: its version has a
	// catalog but no mapping to export.
	cut("Unverified", "2026-10-09T10:00:00Z", false, "RA-3")
	if rec := a.asAdmin(http.MethodGet, base+"mapping.json?version=3", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "verified quote") {
		t.Errorf("a version with only unverified mappings: %d %s", rec.Code, rec.Body.String())
	}
	oscalDownload(t, a, base+"catalog.json?version=3", "regulation-"+id+"-v3-catalog")
}

// doraRegulation stores one analysed regulation with one article and no
// report version yet.
func doraRegulation(t *testing.T, repo *regcoverage.SQLiteRepository) regcoverage.Regulation {
	t.Helper()
	reg, err := repo.CreateRegulation(regcoverage.Regulation{Title: "Digital Operational Resilience Act", Framework: "dora", SHA256: "abc123", Status: regcoverage.StatusAnalyzed}, "text",
		[]regcoverage.Section{doraArticle6})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

var doraArticle6 = regcoverage.Section{Ref: "DORA-ART-6", Label: "Article 6", Title: "ICT risk management framework", Body: "Financial entities shall have a sound framework."}

// cutRegulationVersion saves a report version of reg whose one finding maps
// the article to refs, controls or (with an "NFR " prefix) Security NFRs.
func cutRegulationVersion(t *testing.T, repo *regcoverage.SQLiteRepository, reg regcoverage.Regulation, note, createdAt string, grounded bool, refs ...string) {
	t.Helper()
	v, err := repo.CreateVersion(regcoverage.Version{RegulationID: reg.ID, Note: note, CreatedAt: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	finding := &regcoverage.Finding{Relevant: true, Grounded: grounded, Requirement: "Keep a framework.", Confidence: "high", Model: "m"}
	for _, ref := range refs {
		kind := regcoverage.KindControl
		if key, ok := strings.CutPrefix(ref, "NFR "); ok {
			kind, ref = regcoverage.KindNFR, key
		}
		finding.Mappings = append(finding.Mappings, regcoverage.Mapping{Kind: kind, Ref: ref, Source: regcoverage.SourceSeed, Confidence: "high", Known: true})
	}
	report := regcoverage.Report{Regulation: reg, Version: v, Sections: []regcoverage.Entry{{Section: doraArticle6, Finding: finding}}}
	snapshot, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveVersionSnapshot(v.ID, string(snapshot)); err != nil {
		t.Fatal(err)
	}
}

// The profiles' remarks repeat the Asset Types page's tier rule, which lives
// in that page's script; this holds the two together.
func TestOSCALTierRuleMatchesTheAssetTypesPage(t *testing.T) {
	a := newFullApp(t, Options{})
	page := a.asAdmin(http.MethodGet, "/asset-types", "").Body.String()
	for baseline, rule := range map[string]string{
		"high":     "if (type === 'TIER 1' || type === 'TIER 2') return 'High';",
		"moderate": "if (type === 'TIER 3') return 'Moderate';",
		"low":      "if (type === 'TIER 4') return 'Low';",
	} {
		if !strings.Contains(page, rule) {
			t.Errorf("the Asset Types page no longer says %q: update oscalBaselineTiers", rule)
		}
		condition := strings.SplitN(rule, ")", 2)[0]
		for _, tier := range regexp.MustCompile(`TIER \d`).FindAllString(condition, -1) {
			if !strings.Contains(oscalBaselineTiers[baseline], tier) {
				t.Errorf("%s: the profile note %q omits %s", baseline, oscalBaselineTiers[baseline], tier)
			}
		}
	}
	if !strings.Contains(oscalTierNote("privacy"), "applies this baseline to no asset tier") {
		t.Errorf("PRIVACY has no tier: %q", oscalTierNote("privacy"))
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
