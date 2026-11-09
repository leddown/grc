package oscal

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"grc/internal/controlid"
)

var enhancementID = regexp.MustCompile(`^([A-Z]{2}-\d+)\((\d+)\)$`)

// Every control id in the catalog grc ships resolves to itself through the
// import: from NIST's label prop ("AC-2(1)"), and from the OSCAL token
// ("ac-2.1") when a catalog carries no label. A failure here would make an
// OSCAL import add a second copy of a control instead of updating it.
func TestShippedControlIDsResolveToThemselves(t *testing.T) {
	var shipped map[string]struct {
		NIST struct {
			ControlID string `json:"control_id"`
		} `json:"nist"`
	}
	if err := json.Unmarshal(readFixture(t, shippedCatalog), &shipped); err != nil {
		t.Fatal(err)
	}
	if len(shipped) != 1193 {
		t.Fatalf("the shipped catalog holds %d controls; this test was written for 1193", len(shipped))
	}
	for _, e := range shipped {
		id := e.NIST.ControlID
		if got := controlid.Normalize(id); got != id {
			t.Errorf("Normalize(%q) = %q", id, got)
		}
		label := []Property{{Name: "label", Value: id}, {Name: "label", Class: "zero-padded", Value: "XX-00"}}
		if got := controlIDOf(Control{ID: "x", Props: &label}); got != id {
			t.Errorf("label %q resolves to %q", id, got)
		}
		token := strings.ToLower(id)
		if m := enhancementID.FindStringSubmatch(id); m != nil {
			token = strings.ToLower(m[1]) + "." + m[2]
		}
		if got := controlIDOf(Control{ID: token}); got != id {
			t.Errorf("token %q resolves to %q, want %q", token, got, id)
		}
	}
}
