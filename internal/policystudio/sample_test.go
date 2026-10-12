package policystudio

import (
	"encoding/json"
	"os"
	"testing"
)

// TestWritePolicySampleBlocks regenerates the blocks of the committed render
// sample (templates/samples/policy-sample.json) from each section's body, with
// the same conversion the Studio uses. Opt-in: UPDATE_POLICY_SAMPLE=1.
func TestWritePolicySampleBlocks(t *testing.T) {
	if os.Getenv("UPDATE_POLICY_SAMPLE") == "" {
		t.Skip("set UPDATE_POLICY_SAMPLE=1 to regenerate the sample's blocks")
	}
	path := "../../templates/samples/policy-sample.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]any
	if err := json.Unmarshal(raw, &sample); err != nil {
		t.Fatal(err)
	}
	for _, s := range sample["sections"].([]any) {
		sec := s.(map[string]any)
		node := SectionFromMarkdown("s", "other", sec["heading"].(string), sec["body"].(string), counter())
		sec["blocks"] = SectionBlocks(Resolve(node, Baseline), map[string]string{})
	}
	out, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
