package oscal

import (
	"os/exec"
	"strings"
	"testing"
)

// go-oscal's module also carries a CLI (cobra), a schema generator
// (swaggest/jsonschema-go) and a validator (santhosh-tekuri/jsonschema with ten
// embedded schemas). The binary must link the generated types package and
// nothing else of it; the validator is for tests only (oscaltest).
func TestBinaryLinksOnlyTheOSCALTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("lists the binary's dependencies with the go tool")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}
	// #nosec G204 -- fixed arguments; goTool is the go toolchain found on PATH.
	out, err := exec.Command(goTool, "list", "-deps", "grc/cmd/api").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	const allowed = "github.com/defenseunicorns/go-oscal/src/types/oscal-1-2-1"
	linked := false
	for _, pkg := range strings.Fields(string(out)) {
		switch {
		case pkg == allowed:
			linked = true
		case strings.HasPrefix(pkg, "github.com/defenseunicorns/go-oscal/"),
			strings.HasPrefix(pkg, "github.com/spf13/"),
			strings.HasPrefix(pkg, "github.com/swaggest/"),
			strings.HasPrefix(pkg, "github.com/santhosh-tekuri/"),
			strings.HasPrefix(pkg, "github.com/inconshreveable/"):
			t.Errorf("the binary links %s", pkg)
		}
	}
	if !linked {
		t.Logf("%s is not linked yet (nothing in the binary imports internal/oscal)", allowed)
	}
}
