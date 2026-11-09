package controlcatalog

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// seedJSONCatalog loads three controls the way POST /controls/import does, with
// grc's own content (CIA, justification, threats, appendix) on each.
func seedJSONCatalog(t *testing.T, svc *Service) {
	t.Helper()
	raw := `{
  "AC-1: Policy and Procedures": {"nist": {"control_id": "AC-1", "type": "Control", "name": "Policy and Procedures",
    "threats": ["Spoofing", "Tampering"], "baselines": {"low": true, "moderate": true, "high": true, "privacy": true},
    "cia": {"confidentiality": true, "integrity": true, "availability": false, "justification": "Policy sets the rules."},
    "nist_requirements": "a. Develop  a policy;", "nist_discussion": "Old discussion.", "related_controls": ["PM-9"]},
    "appendix": {"Control Objective": "Have a policy.", "KRI": 3, "References": "DORA Art. 6(2); ISO 27001 A.5.1", "Nested": {"z": 1, "a": [1.50, "<b>"]}}},
  "AC-2: Account Management": {"nist": {"control_id": "AC-2", "type": "Control", "name": "Account Management",
    "threats": ["Elevation of Privilege"], "baselines": {"low": true, "moderate": true, "high": true, "privacy": false},
    "cia": {"confidentiality": true, "integrity": false, "availability": false, "justification": ""},
    "nist_requirements": "a. Define account types;", "nist_discussion": "Accounts.", "related_controls": []},
    "appendix": {"References": "NIS2 Art. 21"}},
  "ZZ-1: Local Control": {"nist": {"control_id": "ZZ-1", "type": "Control", "name": "Local Control",
    "threats": [], "baselines": {"low": false, "moderate": false, "high": false, "privacy": false},
    "cia": {"confidentiality": false, "integrity": false, "availability": false, "justification": ""},
    "nist_requirements": "Only grc has this.", "nist_discussion": "", "related_controls": []}}
}`
	if _, err := svc.Import([]byte(raw), false); err != nil {
		t.Fatalf("Import: %v", err)
	}
	// Stored bytes no write path of this package would produce (spacing, key
	// order, an unnormalised flag, trailing blanks), so any rewrite of these
	// columns, even one with the same meaning, shows up as a changed byte.
	if _, err := svc.repo.(*SQLiteRepository).db.Exec(`UPDATE nist_controls SET
		potentially_common_inheritable = 'Common ',
		appendix_json = '{"References": "DORA Art. 6(2); ISO 27001 A.5.1",  "KRI": 3.50, "Control Objective": "Have a policy.", "z": {"b": 1, "a": "<b>"}}',
		threats_json = '[ "Tampering" , "Spoofing" ]',
		justification = 'Policy sets the rules.  ',
		confidentiality_status = 'yes',
		source_key = ' AC-1: Policy and Procedures '
		WHERE control_id = 'AC-1'`); err != nil {
		t.Fatal(err)
	}
}

func sampleOSCAL() OSCALCatalog {
	return OSCALCatalog{
		Source:   OSCALSource{Title: "NIST SP 800-53", Version: "5.2.0", OSCALVersion: "1.2.3", SHA256: "aaaa"},
		Profiles: map[string]OSCALSource{},
		Controls: []OSCALControl{
			{OSCALID: "ac-1", ID: "AC-1", Type: "Control", Name: "Policy and Procedures", Requirements: "a. Develop a policy;",
				Discussion: "New discussion.", Related: []string{"PM-9", "IA-1"},
				Params: `[{"id":"ac-1_prm_1","label":"personnel or roles"}]`, Statement: `{"id":"ac-1_smt","name":"statement"}`,
				Assessment: `[{"id":"ac-1_obj","name":"assessment-objective"}]`},
			{OSCALID: "ac-2", ID: "AC-2", Type: "Control", Name: "Account Management", Requirements: "a. Define account types;",
				Discussion: "Accounts.", Params: "null", Statement: "null", Assessment: "null"},
			{OSCALID: "sa-24", ID: "SA-24", Type: "Control", Name: "Design For Cyber Resiliency", Requirements: "Design for resiliency.",
				Params: "null", Statement: `{"id":"sa-24_smt","name":"statement"}`, Assessment: "null"},
			{OSCALID: "ac-13", ID: "AC-13", Type: "Control", Name: "Supervision and Review — Access Control", Withdrawn: true,
				Params: "null", Statement: "null", Assessment: "null"},
		},
		Baselines: map[string][]string{},
	}
}

type ownColumns struct {
	sourceKey, conf, integ, avail, justification, threats, pci, appendix string
}

func readOwnColumns(t *testing.T, repo *SQLiteRepository, id string) ownColumns {
	t.Helper()
	var c ownColumns
	if err := repo.db.QueryRow(`SELECT source_key, confidentiality_status, integrity_status, availability_status,
		justification, threats_json, potentially_common_inheritable, appendix_json FROM nist_controls WHERE control_id = ?`, id).
		Scan(&c.sourceKey, &c.conf, &c.integ, &c.avail, &c.justification, &c.threats, &c.pci, &c.appendix); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return c
}

func TestOSCALPreviewSaysWhatWouldChangeAndWritesNothing(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, "")
	seedJSONCatalog(t, svc)
	before, _ := repo.catalogDigest()

	res, err := svc.PreviewOSCAL(sampleOSCAL())
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := repo.catalogDigest(); after != before {
		t.Fatal("a preview wrote to the catalog")
	}
	if len(res.Added) != 2 || res.Added[0].ID != "AC-13" || res.Added[1].ID != "SA-24" {
		t.Fatalf("added %+v", res.Added)
	}
	if !reflect.DeepEqual(res.Absent, []string{"ZZ-1"}) {
		t.Fatalf("absent %v", res.Absent)
	}
	if !reflect.DeepEqual(res.Unchanged, []string{"AC-2"}) {
		t.Fatalf("unchanged %v", res.Unchanged)
	}
	if len(res.Changed) != 1 || res.Changed[0].ID != "AC-1" {
		t.Fatalf("changed %+v", res.Changed)
	}
	fields := map[string]OSCALFieldChange{}
	for _, f := range res.Changed[0].Fields {
		fields[f.Field] = f
	}
	for _, want := range []string{fieldRequirements, fieldDiscussion, fieldRelated, fieldParams, fieldStatement, fieldAssessment} {
		if _, ok := fields[want]; !ok {
			t.Errorf("AC-1 should change %q; fields %+v", want, res.Changed[0].Fields)
		}
	}
	if !fields[fieldRequirements].WhitespaceOnly || fields[fieldDiscussion].WhitespaceOnly {
		t.Errorf("whitespace-only flags wrong: %+v", res.Changed[0].Fields)
	}
	if fields[fieldRelated].After != "IA-1, PM-9" {
		t.Errorf("related after %q", fields[fieldRelated].After)
	}
	if res.Withdrawn != 1 || res.Base == "" || len(res.Problems) != 0 {
		t.Fatalf("withdrawn %d base %q problems %v", res.Withdrawn, res.Base, res.Problems)
	}
	if !strings.Contains(strings.Join(res.Notes, " "), "No baseline profile was posted") {
		t.Fatalf("notes %v", res.Notes)
	}
}

// The heart of the import's promise: grc's own content on a control is never
// touched, down to the stored bytes.
func TestOSCALApplyLeavesGRCContentByteIdentical(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, "")
	seedJSONCatalog(t, svc)
	in := sampleOSCAL()
	in.Baselines = map[string][]string{"low": {"AC-2"}, "moderate": {"AC-1", "AC-2"}, "high": {"AC-1", "AC-2"}, "privacy": {"AC-1"}}
	in.Profiles = map[string]OSCALSource{"low": {SHA256: "l"}, "moderate": {SHA256: "m"}, "high": {SHA256: "h"}, "privacy": {SHA256: "p"}}
	ids := []string{"AC-1", "AC-2", "ZZ-1"}
	before := map[string]ownColumns{}
	for _, id := range ids {
		before[id] = readOwnColumns(t, repo, id)
	}
	if !strings.Contains(before["AC-1"].appendix, "3.50") || before["AC-1"].pci != "Common " || before["AC-1"].threats != `[ "Tampering" , "Spoofing" ]` {
		t.Fatalf("the seed did not set grc's own content: %+v", before["AC-1"])
	}

	preview, err := svc.PreviewOSCAL(in)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ApplyOSCAL(in, preview.Base)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		t.Fatal("not applied")
	}
	for _, id := range ids {
		if got := readOwnColumns(t, repo, id); got != before[id] {
			t.Errorf("%s: grc's own columns changed\n before %+v\n after  %+v", id, before[id], got)
		}
	}

	ac1, err := svc.Get("AC-1")
	if err != nil {
		t.Fatal(err)
	}
	if ac1.Requirements != "a. Develop a policy;" || ac1.Discussion != "New discussion." || !reflect.DeepEqual(ac1.RelatedControls, []string{"IA-1", "PM-9"}) {
		t.Fatalf("AC-1 upstream fields not written: %+v", ac1)
	}
	if string(ac1.Params) != `[{"id":"ac-1_prm_1","label":"personnel or roles"}]` || string(ac1.Statement) == "" || string(ac1.Assessment) == "" {
		t.Fatalf("AC-1 structure: %s / %s / %s", ac1.Params, ac1.Statement, ac1.Assessment)
	}
	if !reflect.DeepEqual(ac1.Baselines, []string{"Moderate", "High", "Privacy"}) {
		t.Fatalf("AC-1 baselines %v (LOW must be cleared, the rest set)", ac1.Baselines)
	}
	sa24, err := svc.Get("SA-24")
	if err != nil {
		t.Fatalf("SA-24 was not added: %v", err)
	}
	if sa24.SourceKey != "SA-24: Design For Cyber Resiliency" || sa24.Family != "SA" || len(sa24.Baselines) != 0 || sa24.Appendix != nil {
		t.Fatalf("SA-24 %+v", sa24)
	}
	ac13, _ := svc.Get("AC-13")
	if ac13.Name != "Supervision and Review — Access Control" || ac13.Requirements != "" {
		t.Fatalf("AC-13 %+v", ac13)
	}
	// Never delete: the control the file does not carry is still there.
	if _, err := svc.Get("ZZ-1"); err != nil {
		t.Fatalf("ZZ-1 was deleted: %v", err)
	}

	again, err := svc.PreviewOSCAL(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Added)+len(again.Changed) != 0 {
		t.Fatalf("a second import of the same files would still change %d/%d controls: %+v", len(again.Added), len(again.Changed), again.Changed)
	}
}

func TestOSCALApplyWithoutProfilesLeavesBaselinesAlone(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, "")
	seedJSONCatalog(t, svc)
	var before string
	_ = repo.db.QueryRow(`SELECT in_low || in_moderate || in_high || in_privacy || mapping_baselines_json FROM nist_controls WHERE control_id = 'AC-1'`).Scan(&before)

	in := sampleOSCAL()
	preview, _ := svc.PreviewOSCAL(in)
	if _, err := svc.ApplyOSCAL(in, preview.Base); err != nil {
		t.Fatal(err)
	}
	var after string
	_ = repo.db.QueryRow(`SELECT in_low || in_moderate || in_high || in_privacy || mapping_baselines_json FROM nist_controls WHERE control_id = 'AC-1'`).Scan(&after)
	if before != after {
		t.Fatalf("baselines changed with no profile posted: %q -> %q", before, after)
	}

	// Only the posted slot moves.
	in = sampleOSCAL()
	in.Baselines = map[string][]string{"low": {}}
	in.Profiles = map[string]OSCALSource{"low": {SHA256: "l"}}
	preview, _ = svc.PreviewOSCAL(in)
	if _, err := svc.ApplyOSCAL(in, preview.Base); err != nil {
		t.Fatal(err)
	}
	ac1, _ := svc.Get("AC-1")
	if !reflect.DeepEqual(ac1.Baselines, []string{"Moderate", "High", "Privacy"}) {
		t.Fatalf("a LOW profile must clear LOW only: %v", ac1.Baselines)
	}
}

func TestOSCALApplyRefusesAStalePreviewOrProblems(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, "")
	seedJSONCatalog(t, svc)
	in := sampleOSCAL()
	preview, err := svc.PreviewOSCAL(in)
	if err != nil {
		t.Fatal(err)
	}

	// Someone edits a control between the preview and the apply.
	if _, err := svc.Update("AC-2", Control{Name: "Account Management (edited)"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyOSCAL(in, preview.Base); !errors.Is(err, ErrOSCALConflict) {
		t.Fatalf("an apply over an edited catalog: %v", err)
	}

	// Different files from the ones previewed.
	preview, _ = svc.PreviewOSCAL(in)
	other := sampleOSCAL()
	other.Source.SHA256 = "bbbb"
	if _, err := svc.ApplyOSCAL(other, preview.Base); !errors.Is(err, ErrOSCALConflict) {
		t.Fatalf("an apply of other files: %v", err)
	}
	if _, err := svc.ApplyOSCAL(in, ""); !errors.Is(err, ErrOSCALConflict) {
		t.Fatalf("an apply with no preview: %v", err)
	}

	bad := sampleOSCAL()
	bad.Problems = []string{"the LOW profile names zz-9"}
	preview, _ = svc.PreviewOSCAL(bad)
	if _, err := svc.ApplyOSCAL(bad, preview.Base); !errors.Is(err, ErrOSCALProblems) {
		t.Fatalf("an apply with problems: %v", err)
	}
	if _, err := svc.Get("SA-24"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a refused apply wrote SA-24")
	}
}

// The repository refuses inside its transaction too, for a write that lands
// after the service's own check.
func TestOSCALRepositoryChecksTheDigestInsideTheTransaction(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, "")
	seedJSONCatalog(t, svc)
	_, plan, err := svc.planOSCAL(sampleOSCAL())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE nist_controls SET name = 'moved' WHERE control_id = 'ZZ-1'`); err != nil {
		t.Fatal(err)
	}
	if err := repo.applyOSCAL(plan.writes, plan.digest); !errors.Is(err, errCatalogMoved) {
		t.Fatalf("applyOSCAL over a moved catalog: %v", err)
	}
}

func TestDetailSectionsRenderTheStoredStructure(t *testing.T) {
	c := Control{
		Params: []byte(`[{"id":"ac-1_prm_1","props":[{"name":"aggregates","ns":"http://csrc.nist.gov/ns/rmf","value":"ac-01_odp.01"}],"label":"organization-defined personnel or roles"},
			{"id":"ac-01_odp.01","props":[{"name":"label","value":"AC-01_ODP[01]","class":"sp800-53a"}],"label":"personnel or roles","guidelines":[{"prose":"personnel <b>or</b> roles are defined;"}]},
			{"id":"ac-01_odp.03","props":[{"name":"label","value":"AC-01_ODP[03]","class":"sp800-53a"}],"select":{"how-many":"one-or-more","choice":["organization-level","system-level"]}}]`),
		Assessment: []byte(`[{"id":"ac-1_obj","name":"assessment-objective","props":[{"name":"label","value":"AC-01","class":"sp800-53a"}],"parts":[
			{"id":"ac-1_obj.a-1","name":"assessment-objective","props":[{"name":"label","value":"AC-01a.[01]","class":"sp800-53a"}],"prose":"the policy is disseminated to {{ insert: param, ac-01_odp.01 }};"}]},
			{"id":"ac-1_asm-examine","name":"assessment-method","props":[{"name":"method","ns":"http://csrc.nist.gov/ns/rmf","value":"EXAMINE"}],"parts":[{"name":"assessment-objects","prose":"Access control policy\n\nsystem security plan"}]}]`),
	}
	html := detailOSCALSections(c)
	for _, want := range []string{
		"Parameters (ODPs) · 3",
		"AC-01_ODP[01]",
		"Assignment: organization-defined personnel or roles (aggregates AC-01_ODP[01])",
		"Selection (one or more): organization-level; system-level",
		"personnel &lt;b&gt;or&lt;/b&gt; roles are defined;",
		"Assessment objectives (800-53A) · 1",
		"the policy is disseminated to &lt;AC-01_ODP[01]&gt;;",
		"EXAMINE",
		"Access control policy; system security plan",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
	if detailOSCALSections(Control{}) != "" {
		t.Fatal("a control never imported from OSCAL must show neither section")
	}
}
