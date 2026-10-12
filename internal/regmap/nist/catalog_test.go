package nist

import (
	"strings"
	"testing"

	"grc/internal/regmap/profile"
)

func TestLoadEmbedded(t *testing.T) {
	c, err := LoadEmbedded(false)
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
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

	withEnh, err := LoadEmbedded(true)
	if err != nil {
		t.Fatalf("LoadEmbedded(true): %v", err)
	}
	if len(withEnh.Controls) <= len(c.Controls) {
		t.Errorf("including enhancements did not grow the catalog: %d vs %d", len(withEnh.Controls), len(c.Controls))
	}

	// Spot-check a control the seed crosswalks rely on.
	ctl, ok := c.Lookup("ir-6")
	if !ok {
		t.Fatal("IR-6 missing from the embedded catalog")
	}
	if ctl.Family != "IR" {
		t.Errorf("IR-6 family = %q, want IR", ctl.Family)
	}
	if ctl.Title == "" {
		t.Error("IR-6 has no title")
	}
}

func TestEmbeddedCatalogIsDeterministic(t *testing.T) {
	// The embedded dataset decodes from a JSON map, whose iteration order is
	// randomised. Ordering has to be imposed by the loader or report output
	// would churn between runs.
	first, err := LoadEmbedded(false)
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := LoadEmbedded(false)
		if err != nil {
			t.Fatalf("LoadEmbedded: %v", err)
		}
		if strings.Join(first.IDs(), ",") != strings.Join(next.IDs(), ",") {
			t.Fatal("catalog ordering changed between loads; report output would not be reproducible")
		}
	}
}

// TestSeedCrosswalksReferenceRealControls is the check that keeps the shipped
// profiles honest: every control ID a seed crosswalk proposes must exist in the
// catalog it will be validated against, or reviewers get offered IDs that
// cannot resolve to a title at GATE 2.
func TestSeedCrosswalksReferenceRealControls(t *testing.T) {
	cat, err := LoadEmbedded(false)
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
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
