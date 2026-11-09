package oscal_test

import (
	"os"
	"path/filepath"
	"testing"

	"grc/internal/oscal"
	"grc/internal/oscal/oscaltest"
)

// The fixture is a cut of NIST's file; a cut that broke the document would
// make every other test here meaningless.
func TestFixturesAreValidOSCAL(t *testing.T) {
	for _, path := range []string{
		"testdata/NIST_SP-800-53_rev5_catalog_cut.json",
		"testdata/NIST_SP-800-53_rev5_LOW-baseline_profile_cut.json",
		"testdata/NIST_SP-800-53_rev5_MODERATE-baseline_profile_cut.json",
	} {
		if err := oscaltest.Validate(mustRead(t, path)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Every document grc writes validates against the OSCAL 1.2.1 schema.
func TestExportsAreValidOSCAL(t *testing.T) {
	catalog, err := oscal.ExportCatalog(oscal.SampleCatalogExport(t))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := oscal.ExportProfile(oscal.ProfileExport{Baseline: "moderate", Title: "grc: NIST SP 800-53 MODERATE baseline",
		LastModified: oscal.SampleTime, CatalogHref: "/controls/oscal/catalog.json", ControlIDs: []string{"AC-1", "AC-2", "AC-2(1)"}})
	if err != nil {
		t.Fatal(err)
	}
	nfrCatalog, err := oscal.ExportNFRCatalog(oscal.NFRCatalogExport{Title: "grc: Security NFR catalog", LastModified: oscal.SampleTime, NFRs: oscal.SampleNFRs()})
	if err != nil {
		t.Fatal(err)
	}
	nfrMapping, err := oscal.ExportNFRMapping(oscal.SampleNFRMapping())
	if err != nil {
		t.Fatal(err)
	}
	regCatalog, err := oscal.ExportRegulationCatalog(oscal.SampleRegulation(true))
	if err != nil {
		t.Fatal(err)
	}
	regMapping, err := oscal.ExportRegulationMapping(oscal.SampleRegulation(true), oscal.SampleRegulationHrefs())
	if err != nil {
		t.Fatal(err)
	}
	assessmentPlan, err := oscal.ExportAssessmentPlan(oscal.SamplePlan(), oscal.SampleCatalogHrefs())
	if err != nil {
		t.Fatal(err)
	}
	assessmentResults, err := oscal.ExportAssessmentResults(oscal.SampleQuarterResults(), oscal.SampleAssessmentPlanRef(), oscal.SampleCatalogHrefs())
	if err != nil {
		t.Fatal(err)
	}
	poam, err := oscal.ExportPOAM(oscal.SamplePOAM())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"catalog": catalog, "profile": profile, "nfr catalog": nfrCatalog,
		"nfr mapping": nfrMapping, "regulation catalog": regCatalog, "regulation mapping": regMapping,
		"assessment plan": assessmentPlan, "assessment results": assessmentResults, "poam": poam,
	} {
		if err := oscaltest.Validate(body); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
