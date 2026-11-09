package oscal

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTokens(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{ControlToken("AC-2(1)"), "ac-2.1"},
		{ControlToken("SA-24"), "sa-24"},
		{NFRToken("1"), "nfr-1"},
		{NFRToken("k-5"), "nfr-k-5"},
		{NFRToken("Data Retention"), "nfr-data-retention"},
		{Token("section", "DORA-ART-17"), "dora-art-17"},
		{Token("section", "17(2)"), "section-17-2"},
		{Token("section", ""), "section"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

// The export is the import run backwards: reading an export gives back every
// field the import writes, so an export applied to the catalog it came from
// changes nothing.
func TestCatalogExportReadsBackAsItWasStored(t *testing.T) {
	in := SampleCatalogExport(t)
	stored := map[string]ExportControl{}
	for i := range in.Controls {
		c := &in.Controls[i]
		switch c.ID {
		case "AC-1":
			// A hand edit after the OSCAL import: the text no longer renders from
			// NIST's structure.
			c.Requirements = "a. Develop an access control policy and keep it current."
		case "SA-24":
			// A control that never came from OSCAL: text only, no structure.
			c.ParamsJSON, c.StatementJSON, c.AssessmentJSON = "null", "null", "null"
		case "AC-2":
			// grc's own analysis, which the import must not read back.
			c.Confidentiality, c.Integrity = true, true
			c.Threats = []string{"Spoofing", " ", "Lateral Movement"}
			c.PotentiallyCommonInheritable = "Common"
			c.Justification = "Account data is personal data."
			c.Appendix = map[string]any{"References": "DORA Art. 9(4)(c)", "Control Objective": "\nAccounts are managed.", "KPI": "  ", "KRI": nil, "NIST CSF v2.0": "PR.AA-01"}
		}
		stored[c.ID] = *c
	}
	body, err := ExportCatalog(in)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ReadCatalogImport(body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Problems) != 0 {
		t.Fatalf("problems reading the export back: %v", back.Problems)
	}
	if len(back.Controls) != len(stored) {
		t.Fatalf("%d controls back, want %d", len(back.Controls), len(stored))
	}
	types := map[string]string{"AC-1": TypeControl, "AC-2": TypeControl, "AC-2(1)": TypeEnhancement, "AC-13": TypeControl, "SA-24": TypeControl}
	for _, got := range back.Controls {
		want := stored[got.ID]
		if got.Type != types[got.ID] {
			t.Errorf("%s: type %q, want %q", got.ID, got.Type, types[got.ID])
		}
		if got.Name != want.Name || got.Requirements != want.Requirements || got.Discussion != want.Discussion {
			t.Errorf("%s: text changed\n name %q / %q\n requirements %q / %q\n discussion %.80q / %.80q",
				got.ID, got.Name, want.Name, got.Requirements, want.Requirements, got.Discussion, want.Discussion)
		}
		if strings.Join(got.Related, ",") != strings.Join(want.Related, ",") || got.Withdrawn != want.Withdrawn {
			t.Errorf("%s: related %v withdrawn %v, want %v %v", got.ID, got.Related, got.Withdrawn, want.Related, want.Withdrawn)
		}
		if got.ParamsJSON != jsonOrNullTest(want.ParamsJSON) || got.AssessmentJSON != jsonOrNullTest(want.AssessmentJSON) {
			t.Errorf("%s: the stored params or assessment parts did not come back as stored", got.ID)
		}
		switch got.ID {
		case "AC-1", "SA-24":
			if !got.KeepStatement || got.StatementJSON != "null" {
				t.Errorf("%s: grc's own text should come back as text that keeps the stored structure (keep %v, statement %.60s)", got.ID, got.KeepStatement, got.StatementJSON)
			}
		default:
			if got.KeepStatement || got.StatementJSON != jsonOrNullTest(want.StatementJSON) {
				t.Errorf("%s: NIST's statement should come back as stored (keep %v)", got.ID, got.KeepStatement)
			}
		}
	}
	if !bytes.Contains(body, []byte(`"status"`)) || !bytes.Contains(body, []byte(`"withdrawn"`)) {
		t.Errorf("the withdrawn AC-13 carries no status prop")
	}

	var doc struct {
		Catalog Catalog `json:"catalog"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	ac2 := (*(*doc.Catalog.Groups)[0].Controls)[1]
	if ac2.ID != "ac-2" {
		t.Fatalf("expected AC-2, got %s", ac2.ID)
	}
	var grcProps []string
	for _, p := range *ac2.Props {
		if p.Ns == Namespace {
			grcProps = append(grcProps, p.Name+"="+p.Value)
		}
	}
	wantProps := []string{"confidentiality=yes", "integrity=yes", "availability=no", "threat=Spoofing", "threat=Lateral Movement", "potentially-common-inheritable=Common"}
	if !reflect.DeepEqual(grcProps, wantProps) {
		t.Errorf("AC-2's grc props\n got  %v\n want %v", grcProps, wantProps)
	}
	parts := map[string]Part{}
	for _, p := range *ac2.Parts {
		if p.Ns == Namespace {
			parts[p.Name] = p
		}
	}
	if parts["cia-justification"].Prose != "Account data is personal data." {
		t.Errorf("the CIA justification part %+v", parts["cia-justification"])
	}
	var fields []string
	if apx := parts["appendix"]; apx.Parts != nil {
		for _, f := range *apx.Parts {
			fields = append(fields, f.Name+"|"+f.Title+"|"+f.Prose)
		}
	}
	wantFields := []string{"control-objective|Control Objective|Accounts are managed.", "nist-csf-v2.0|NIST CSF v2.0|PR.AA-01", "references|References|DORA Art. 9(4)(c)"}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Errorf("the appendix part, in name order without empty fields\n got  %v\n want %v", fields, wantFields)
	}
}

// resolveResource follows a "#uuid" reference into the back-matter and gives
// the rlink it names.
func resolveResource(t *testing.T, bm *BackMatter, ref string) ResourceLink {
	t.Helper()
	if bm == nil || bm.Resources == nil || !strings.HasPrefix(ref, "#") {
		t.Fatalf("%q is not a back-matter reference", ref)
	}
	for _, r := range *bm.Resources {
		if "#"+r.UUID == ref && r.Rlinks != nil && len(*r.Rlinks) == 1 && r.Title != "" {
			return (*r.Rlinks)[0]
		}
	}
	t.Fatalf("no titled back-matter resource with one rlink for %s", ref)
	return ResourceLink{}
}

func jsonOrNullTest(s string) string {
	if strings.TrimSpace(s) == "" {
		return "null"
	}
	return s
}

func TestProfileExportReadsBackAsTheBaseline(t *testing.T) {
	in := SampleCatalogExport(t)
	catalog, err := ExportCatalog(in)
	if err != nil {
		t.Fatal(err)
	}
	profiles := map[string][]byte{}
	want := map[string][]string{"low": {"AC-1", "AC-2"}, "moderate": {"AC-2(1)", "AC-1", "AC-2"}}
	for slot, ids := range want {
		body, err := ExportProfile(ProfileExport{Baseline: slot, Title: "grc: NIST SP 800-53 " + strings.ToUpper(slot) + " baseline",
			LastModified: SampleTime, CatalogHref: "/controls/oscal/catalog.json", CatalogTitle: "grc: NIST SP 800-53 control catalog",
			TierNote: "TIER 3 assets take this baseline.", ControlIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		profiles[slot] = body
	}
	back, err := ReadCatalogImport(catalog, profiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Problems) != 0 {
		t.Fatalf("problems: %v", back.Problems)
	}
	if got := back.Baselines["low"]; !reflect.DeepEqual(got, []string{"AC-1", "AC-2"}) {
		t.Errorf("LOW back as %v", got)
	}
	if got := back.Baselines["moderate"]; !reflect.DeepEqual(got, []string{"AC-1", "AC-2", "AC-2(1)"}) {
		t.Errorf("MODERATE back as %v", got)
	}
	var doc struct {
		Profile struct {
			Imports []struct {
				Href            string `json:"href"`
				IncludeControls []struct {
					WithIDs []string `json:"with-ids"`
				} `json:"include-controls"`
			} `json:"imports"`
			Merge      map[string]any `json:"merge"`
			BackMatter *BackMatter    `json:"back-matter"`
			Metadata   Metadata       `json:"metadata"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(profiles["moderate"], &doc); err != nil {
		t.Fatal(err)
	}
	imp := doc.Profile.Imports[0]
	if !reflect.DeepEqual(imp.IncludeControls[0].WithIDs, []string{"ac-1", "ac-2", "ac-2.1"}) || doc.Profile.Merge["as-is"] != true {
		t.Errorf("profile %+v merge %v", imp, doc.Profile.Merge)
	}
	if rl := resolveResource(t, doc.Profile.BackMatter, imp.Href); rl.Href != "/controls/oscal/catalog.json" || rl.MediaType != CatalogMediaType {
		t.Errorf("the profile should import the catalog through a back-matter resource, as NIST's do: %+v", rl)
	}
	if !strings.HasSuffix(doc.Profile.Metadata.Remarks, " TIER 3 assets take this baseline.") {
		t.Errorf("the tier note belongs in the remarks: %q", doc.Profile.Metadata.Remarks)
	}
	if _, err := ExportProfile(ProfileExport{Baseline: "high", ControlIDs: nil}); !errors.Is(err, ErrEmptyBaseline) {
		t.Errorf("an empty baseline: %v", err)
	}
	if _, err := ExportProfile(ProfileExport{Baseline: "extreme", ControlIDs: []string{"AC-1"}}); err == nil {
		t.Error("an unknown baseline was exported")
	}
}

// Two exports of unchanged data differ in last-modified and nothing else; a
// change of content gives a new UUID and version; input order does not matter.
func TestExportsAreDeterministic(t *testing.T) {
	later := SampleTime.Add(26 * time.Hour)
	stamp := func(tm time.Time) string { return tm.UTC().Format(time.RFC3339) }
	check := func(name string, export func(time.Time, bool) ([]byte, error)) {
		t.Helper()
		a, err := export(SampleTime, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := export(later, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bytes.Equal(a, b) {
			t.Fatalf("%s: last-modified did not change", name)
		}
		if !bytes.Equal(a, bytes.ReplaceAll(b, []byte(stamp(later)), []byte(stamp(SampleTime)))) {
			t.Errorf("%s: two exports of the same data differ beyond last-modified", name)
		}
		if again, _ := export(SampleTime, false); !bytes.Equal(a, again) {
			t.Errorf("%s: the same export twice gave different bytes", name)
		}
	}
	reverse := func(n int, swap func(i, j int)) {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			swap(i, j)
		}
	}
	check("catalog", func(tm time.Time, shuffle bool) ([]byte, error) {
		in := SampleCatalogExport(t)
		in.LastModified = tm
		if shuffle {
			reverse(len(in.Controls), func(i, j int) { in.Controls[i], in.Controls[j] = in.Controls[j], in.Controls[i] })
		}
		return ExportCatalog(in)
	})
	check("profile", func(tm time.Time, shuffle bool) ([]byte, error) {
		ids := []string{"AC-1", "AC-2(1)", "AC-2"}
		if shuffle {
			ids = []string{"AC-2", "AC-1", "AC-2(1)"}
		}
		return ExportProfile(ProfileExport{Baseline: "moderate", Title: "t MODERATE", LastModified: tm, CatalogHref: "/c", ControlIDs: ids})
	})
	check("nfr catalog", func(tm time.Time, shuffle bool) ([]byte, error) {
		nfrs := SampleNFRs()
		if shuffle {
			reverse(len(nfrs), func(i, j int) { nfrs[i], nfrs[j] = nfrs[j], nfrs[i] })
		}
		return ExportNFRCatalog(NFRCatalogExport{Title: "t", LastModified: tm, NFRs: nfrs})
	})
	check("nfr mapping", func(tm time.Time, shuffle bool) ([]byte, error) {
		in := SampleNFRMapping()
		in.LastModified = tm
		if shuffle {
			reverse(len(in.Links), func(i, j int) { in.Links[i], in.Links[j] = in.Links[j], in.Links[i] })
			reverse(len(in.ControlIDs), func(i, j int) { in.ControlIDs[i], in.ControlIDs[j] = in.ControlIDs[j], in.ControlIDs[i] })
		}
		return ExportNFRMapping(in)
	})

	uuidOf := func(body []byte) (string, string) {
		var doc map[string]struct {
			UUID     string `json:"uuid"`
			Metadata struct {
				Version string `json:"version"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		for _, d := range doc {
			return d.UUID, d.Metadata.Version
		}
		return "", ""
	}
	in := SampleCatalogExport(t)
	a, _ := ExportCatalog(in)
	in.Controls[0].Name += " (revised)"
	b, _ := ExportCatalog(in)
	ua, va := uuidOf(a)
	ub, vb := uuidOf(b)
	if ua == ub || va == vb || len(va) != documentDigits {
		t.Errorf("a changed catalog should get a new uuid and version: %s %s / %s %s", ua, va, ub, vb)
	}
}

func TestNFRCatalogExport(t *testing.T) {
	body, err := ExportNFRCatalog(NFRCatalogExport{Title: "grc: Security NFR catalog", LastModified: SampleTime, NFRs: SampleNFRs()})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Catalog Catalog `json:"catalog"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	groups := *doc.Catalog.Groups
	if len(groups) != 2 || groups[0].Title != "Application Security" || groups[1].Title != "Logging" || groups[0].ID != "domain-application-security" {
		t.Fatalf("groups %+v", groups)
	}
	if loose := *doc.Catalog.Controls; len(loose) != 1 || loose[0].ID != "nfr-3" || loose[0].Title != "Unsorted requirement" {
		t.Errorf("an NFR without a domain should sit at the top level: %+v", loose)
	}
	nfr := (*groups[0].Controls)[0]
	if nfr.ID != "nfr-2" || nfr.Title != "1.1 Applications must be free of known vulnerabilities" {
		t.Errorf("control %s %q", nfr.ID, nfr.Title)
	}
	props := map[string]string{}
	for _, p := range *nfr.Props {
		props[p.Ns+"#"+p.Name] = p.Value
	}
	if props["#label"] != "1.1" || props[Namespace+"#weight"] != "5" || props[Namespace+"#record-key"] != "2" || props[Namespace+"#issue-type"] != "Control" || props[Namespace+"#domain"] != "Application Security" {
		t.Errorf("props %v", props)
	}
	parts := map[string]Part{}
	for _, p := range *nfr.Parts {
		parts[p.Name] = p
	}
	if parts["statement"].Prose != "Scan and patch." || parts["statement"].Ns != "" {
		t.Errorf("the description should be the statement: %+v", parts["statement"])
	}
	if parts["additional-details"].Ns != Namespace || parts["nist-mapping"].Prose != "SI-2, RA-5" || parts["implementation"].Prose != "" {
		t.Errorf("grc's parts %+v", parts)
	}
	if _, err := ExportNFRCatalog(NFRCatalogExport{NFRs: []ExportNFR{{Key: "a b", Summary: "x"}, {Key: "a-b", Summary: "y"}}}); err == nil {
		t.Error("two keys with one OSCAL id should be refused")
	}
}

func TestNFRMappingExport(t *testing.T) {
	body, err := ExportNFRMapping(SampleNFRMapping())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MC mappingCollection `json:"mapping-collection"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.MC.Mappings) != 1 {
		t.Fatalf("%d mappings", len(doc.MC.Mappings))
	}
	m := doc.MC.Mappings[0]
	if m.Method != "human" || m.MatchingRationale != "semantic" || m.Status != "not-complete" || doc.MC.Provenance.Status != "not-complete" {
		t.Errorf("provenance %+v / mapping %s %s %s", doc.MC.Provenance, m.Method, m.MatchingRationale, m.Status)
	}
	if src := resolveResource(t, doc.MC.BackMatter, m.SourceResource.Href); src.Href != "/security-nfrs/oscal/catalog.json" || m.SourceResource.Type != "catalog" {
		t.Errorf("source %+v -> %+v", m.SourceResource, src)
	}
	if tgt := resolveResource(t, doc.MC.BackMatter, m.TargetResource.Href); tgt.Href != "/controls/oscal/catalog.json" || m.TargetResource.Type != "catalog" {
		t.Errorf("target %+v -> %+v", m.TargetResource, tgt)
	}
	var pairs []string
	for _, mp := range m.Maps {
		if mp.Relationship != Relationship || mp.Ns != Namespace {
			t.Errorf("map relationship %q ns %q", mp.Relationship, mp.Ns)
		}
		pair := mp.Sources[0].IDRef + ">" + mp.Targets[0].IDRef
		if mp.Props != nil {
			for _, p := range *mp.Props {
				pair += " " + p.Name + "=" + p.Value
			}
		}
		pairs = append(pairs, pair)
	}
	want := []string{"nfr-2>ra-5", "nfr-2>si-2", "nfr-10>au-2", "nfr-10>au-12 override=yes written-as=AU-11"}
	if !reflect.DeepEqual(pairs, want) {
		t.Errorf("maps\n got  %v\n want %v", pairs, want)
	}
	if !strings.Contains(m.Remarks, "NFR 2.1 names ZZ-9") {
		t.Errorf("the unresolved reference should be named: %q", m.Remarks)
	}
	if m.SourceGapSummary == nil || !reflect.DeepEqual(*m.SourceGapSummary.UnmappedControls[0].WithIds, []string{"nfr-3"}) {
		t.Errorf("source gap %+v", m.SourceGapSummary)
	}
	if m.TargetGapSummary == nil || !reflect.DeepEqual(*m.TargetGapSummary.UnmappedControls[0].WithIds, []string{"ac-1"}) {
		t.Errorf("target gap %+v", m.TargetGapSummary)
	}

	in := SampleNFRMapping()
	in.Links = []ExportNFRLink{{NFRKey: "2", WrittenAs: "SI-2", ControlID: "SI-2"}, {NFRKey: "10", WrittenAs: "AU-2", ControlID: "AU-2"}, {NFRKey: "3", WrittenAs: "AC-1", ControlID: "AC-1"}}
	body, _ = ExportNFRMapping(in)
	if !bytes.Contains(body, []byte(`"status":"complete"`)) {
		t.Error("every NFR mapped and every reference resolved should be complete")
	}
	in.Links = []ExportNFRLink{{NFRKey: "2", WrittenAs: "ZZ-1"}}
	if _, err := ExportNFRMapping(in); !errors.Is(err, ErrNothingMapped) {
		t.Errorf("no map: %v", err)
	}
}

func TestRegulationExports(t *testing.T) {
	reg := SampleRegulation(true)
	body, err := ExportRegulationCatalog(reg)
	if err != nil {
		t.Fatal(err)
	}
	var cat struct {
		Catalog Catalog `json:"catalog"`
	}
	if err := json.Unmarshal(body, &cat); err != nil {
		t.Fatal(err)
	}
	if cat.Catalog.Metadata.Version != "2" || !cat.Catalog.Metadata.LastModified.Equal(reg.LastModified) {
		t.Errorf("a report version is dated and numbered by itself: %s %s", cat.Catalog.Metadata.Version, cat.Catalog.Metadata.LastModified)
	}
	var ids []string
	var controls []Control
	for _, g := range *cat.Catalog.Groups {
		for _, c := range *g.Controls {
			ids = append(ids, g.ID+"/"+c.ID)
			controls = append(controls, c)
		}
	}
	for _, c := range *cat.Catalog.Controls {
		ids = append(ids, c.ID)
		controls = append(controls, c)
	}
	want := []string{"category-scope/dora-art-1", "category-ict-risk-management/dora-art-6", "dora-art-6-2", "dora-art-9"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("a group per category in first-appearance order, the rest at the top level\n got  %v\n want %v", ids, want)
	}
	art6 := controls[1]
	var finding *Part
	for i := range *art6.Parts {
		if p := (*art6.Parts)[i]; p.Name == "finding" {
			finding = &p
		}
	}
	if finding == nil || finding.Ns != Namespace || len(*finding.Parts) != 4 {
		t.Fatalf("the finding part %+v", finding)
	}
	if controls[2].Title != "Article 6a" || controls[3].Parts == nil || len(*controls[3].Parts) != 1 {
		t.Errorf("a section without a title takes its label; one without a finding has only its text")
	}

	body, err = ExportRegulationMapping(reg, SampleRegulationHrefs())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MC mappingCollection `json:"mapping-collection"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.MC.Mappings) != 2 {
		t.Fatalf("%d mappings, want one to the NFRs and one to 800-53", len(doc.MC.Mappings))
	}
	nfr, ctl := doc.MC.Mappings[0], doc.MC.Mappings[1]
	if nfr.Method != "automation" || nfr.Maps[0].Targets[0].IDRef != "nfr-2" {
		t.Errorf("NFR mapping %+v", nfr)
	}
	if rl := resolveResource(t, doc.MC.BackMatter, nfr.TargetResource.Href); rl.Href != "/security-nfrs/oscal/catalog.json" {
		t.Errorf("the NFR mapping's target %+v", rl)
	}
	if rl := resolveResource(t, doc.MC.BackMatter, nfr.SourceResource.Href); rl.Href != "/regulation-coverage/7/oscal/catalog.json?version=2" {
		t.Errorf("the source is the version's own catalog: %+v", rl)
	}
	if ctl.Method != "human" || len(ctl.Maps) != 1 || ctl.Maps[0].Targets[0].IDRef != "ra-3" || ctl.Maps[0].ConfidenceScore.Category != "high" {
		t.Errorf("800-53 mapping %+v", ctl)
	}
	if !strings.Contains(ctl.Remarks, "1 of the report's mappings to this catalog are left out") || !strings.Contains(ctl.Remarks, "Article 6 → ZZ-1") {
		t.Errorf("the unknown reference should be counted and named: %q", ctl.Remarks)
	}
	if ctl.SourceGapSummary == nil || !reflect.DeepEqual(*ctl.SourceGapSummary.UnmappedControls[0].WithIds, []string{"dora-art-6-2"}) {
		t.Errorf("relevant sections without a map are the gap: %+v", ctl.SourceGapSummary)
	}
	if doc.MC.Provenance.Method != "hybrid" || doc.MC.Provenance.Status != "not-complete" {
		t.Errorf("provenance %+v: a seed and a model map make hybrid; a section without a finding makes not-complete", doc.MC.Provenance)
	}
	if !strings.Contains(doc.MC.Metadata.Remarks, "may have changed since this version was cut") {
		t.Errorf("the remarks should say the targets are the live catalogs: %q", doc.MC.Metadata.Remarks)
	}

	// Every section analysed: complete until a mapping is left out. A mapping on
	// a finding whose quote was not verified is left out like an unknown
	// reference, and a catalog left with no map is named in the collection's
	// remarks.
	statuses := func(r ExportRegulation) (string, map[string]string, string) {
		t.Helper()
		body, err := ExportRegulationMapping(r, SampleRegulationHrefs())
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			MC mappingCollection `json:"mapping-collection"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, m := range doc.MC.Mappings {
			out[m.Maps[0].Targets[0].IDRef[:3]] = m.Status
		}
		return doc.MC.Provenance.Status, out, doc.MC.Metadata.Remarks
	}
	full := SampleRegulation(true)
	full.Sections[3].Finding = &ExportFinding{Relevant: true, Grounded: true, Confidence: "high",
		Mappings: []ExportRegMapping{{Kind: "control", Ref: "SC-7", Source: "model", Known: true}}}
	full.Sections[1].Finding.Mappings = full.Sections[1].Finding.Mappings[:2]
	if all, each, _ := statuses(full); all != "complete" || each["nfr"] != "complete" || each["ra-"] != "complete" {
		t.Errorf("every section analysed, nothing left out: %s %v", all, each)
	}
	full.Sections[2].Finding.Mappings = []ExportRegMapping{{Kind: "control", Ref: "RA-5", Source: "model", Known: true}}
	if all, each, _ := statuses(full); all != "not-complete" || each["nfr"] != "complete" || each["ra-"] != "not-complete" {
		t.Errorf("an unverified quote's mapping is left out and makes its mapping not-complete: %s %v", all, each)
	}
	full.Sections[2].Finding.Mappings = nil
	full.Sections[1].Finding.Grounded = false
	full.Sections[3].Finding.Mappings = append(full.Sections[3].Finding.Mappings, ExportRegMapping{Kind: "control", Ref: "RA-3", Source: "seed", Known: true})
	if all, each, remarks := statuses(full); all != "not-complete" || len(each) != 1 || !strings.Contains(remarks, "No map to the Security NFRs: 1 of the report's mappings") {
		t.Errorf("a catalog whose every mapping is left out has no mapping and is named in the remarks: %s %v %q", all, each, remarks)
	}

	old, _ := ExportRegulationMapping(SampleRegulation(false), SampleRegulationHrefs())
	if !bytes.Contains(old, []byte(`"status":"superseded"`)) {
		t.Error("an earlier version should be superseded")
	}
	empty := SampleRegulation(true)
	for i := range empty.Sections {
		if empty.Sections[i].Finding != nil {
			empty.Sections[i].Finding.Mappings = nil
		}
	}
	if _, err := ExportRegulationMapping(empty, SampleRegulationHrefs()); !errors.Is(err, ErrNothingMapped) {
		t.Errorf("no map: %v", err)
	}
}
