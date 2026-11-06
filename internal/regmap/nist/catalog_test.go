package nist

import (
	"os"
	"strings"
	"testing"

	"grc/internal/controlfile"
	"grc/internal/regmap/profile"
)

// loadBundled builds the catalog from the 800-53 file shipped for import, the
// controls an installation has once it imports that file.
func loadBundled(t *testing.T, includeEnhancements bool) *Catalog {
	t.Helper()
	raw, err := os.ReadFile("../../data/merged_nist_controls_master_replaced_from_controls_all.json")
	if err != nil {
		t.Fatalf("read bundled catalog: %v", err)
	}
	file, err := controlfile.ReadBytes(raw)
	if err != nil {
		t.Fatalf("parse bundled catalog: %v", err)
	}
	controls := make([]Control, 0, len(file))
	for _, entry := range file {
		controls = append(controls, Control{
			ID:          entry.NIST.ControlID,
			Title:       entry.NIST.Name,
			ControlText: entry.NIST.Requirements,
			Enhancement: entry.NIST.Type == "Control Enhancement",
		})
	}
	c, err := FromControls(controls, includeEnhancements, "bundled")
	if err != nil {
		t.Fatalf("FromControls: %v", err)
	}
	return c
}

func TestFromControls(t *testing.T) {
	c := loadBundled(t, false)
	if len(c.Controls) < 100 {
		t.Errorf("base catalog has only %d controls; grc's dataset should yield far more", len(c.Controls))
	}

	// Enhancements are excluded by default: they would bloat the control-ID
	// list injected into every LLM suggestion prompt without improving the
	// mapping, which targets base controls.
	for _, ctl := range c.Controls {
		if strings.Contains(ctl.ID, "(") {
			t.Errorf("base catalog contains enhancement %q", ctl.ID)
			break
		}
	}

	withEnh := loadBundled(t, true)
	if len(withEnh.Controls) <= len(c.Controls) {
		t.Errorf("including enhancements did not grow the catalog: %d vs %d", len(withEnh.Controls), len(c.Controls))
	}

	// Spot-check a control the seed crosswalks rely on.
	ctl, ok := c.Lookup("ir-6")
	if !ok {
		t.Fatal("IR-6 missing from the bundled catalog")
	}
	if ctl.Family != "IR" {
		t.Errorf("IR-6 family = %q, want IR", ctl.Family)
	}
	if ctl.Title == "" {
		t.Error("IR-6 has no title")
	}
}

func TestCatalogIsDeterministic(t *testing.T) {
	// The bundled file decodes from a JSON map, whose iteration order is
	// randomised. Ordering has to be imposed by the loader or report output
	// would churn between runs.
	first := loadBundled(t, false)
	for i := 0; i < 5; i++ {
		next := loadBundled(t, false)
		if strings.Join(first.IDs(), ",") != strings.Join(next.IDs(), ",") {
			t.Fatal("catalog ordering changed between loads; report output would not be reproducible")
		}
	}
}

// TestSeedCrosswalksReferenceRealControls is the check that keeps the shipped
// profiles honest: every control ID a seed crosswalk proposes must exist in the
// catalog it will be validated against, or reviewers get offered IDs that
// cannot resolve to a title at GATE 2.
func TestFromControlsRefusesAnEmptyCatalog(t *testing.T) {
	if _, err := FromControls(nil, true, "none"); err == nil {
		t.Fatal("an empty control list built a catalog")
	}
	if _, err := FromControls([]Control{{ID: "AC-2(1)"}}, false, "enhancements only"); err == nil {
		t.Fatal("a list of only enhancements built a base-control catalog")
	}
}

func TestSeedCrosswalksReferenceRealControls(t *testing.T) {
	cat := loadBundled(t, false)
	reg, err := profile.LoadDir("../../../regmap/profiles")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	for _, p := range reg.All() {
		for _, seed := range p.SeedCrosswalk {
			_, unknown := cat.Validate(seed.Controls)
			if len(unknown) > 0 {
				t.Errorf("profile %s, seed group %q references controls not in the catalog: %s",
					p.ID, seed.Group, strings.Join(unknown, ", "))
			}
		}
	}
}
