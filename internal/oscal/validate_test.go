package oscal_test

import (
	"os"
	"path/filepath"
	"testing"

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
