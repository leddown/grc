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
// sampleExports are the documents every exporter builds from the samples,
// named as trestle workspace models.
func sampleExports(t *testing.T) map[string][]byte {
	t.Helper()
	docs := map[string][]byte{}
	add := func(name string, body []byte, err error) {
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		docs[name] = body
	}
	body, err := oscal.ExportCatalog(oscal.SampleCatalogExport(t))
	add("catalog", body, err)
	body, err = oscal.ExportProfile(oscal.ProfileExport{Baseline: "moderate", Title: "grc: NIST SP 800-53 MODERATE baseline",
		LastModified: oscal.SampleTime, CatalogHref: "/controls/oscal/catalog.json", ControlIDs: []string{"AC-1", "AC-2", "AC-2(1)"}})
	add("profile", body, err)
	body, err = oscal.ExportNFRCatalog(oscal.NFRCatalogExport{Title: "grc: Security NFR catalog", LastModified: oscal.SampleTime, NFRs: oscal.SampleNFRs()})
	add("nfr-catalog", body, err)
	body, err = oscal.ExportNFRMapping(oscal.SampleNFRMapping())
	add("nfr-mapping", body, err)
	body, err = oscal.ExportRegulationCatalog(oscal.SampleRegulation(true))
	add("regulation-catalog", body, err)
	body, err = oscal.ExportRegulationMapping(oscal.SampleRegulation(true), oscal.SampleRegulationHrefs())
	add("regulation-mapping", body, err)
	body, err = oscal.ExportAssessmentPlan(oscal.SamplePlan(), oscal.SampleCatalogHrefs())
	add("assessment-plan", body, err)
	body, err = oscal.ExportAssessmentResults(oscal.SampleQuarterResults(), oscal.SampleAssessmentPlanRef(), oscal.SampleCatalogHrefs())
	add("assessment-results", body, err)
	body, err = oscal.ExportPOAM(oscal.SamplePOAM())
	add("poam", body, err)
	return docs
}

func TestExportsAreValidOSCAL(t *testing.T) {
	for name, body := range sampleExports(t) {
		if err := oscaltest.Validate(body); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// compliance-trestle reads every kind of document grc exports with its own
// model, a second reading besides the schema. The samples carry what a
// seeded database does not reach (TestOSCALExportsPassTrestle in internal/app
// runs the served documents): attributes naming NFRs, items that joined after
// approval, every finding status.
func TestExportsPassTrestle(t *testing.T) {
	oscaltest.Trestle(t, sampleExports(t))
}
