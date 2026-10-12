package controlfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBytes_SupportsNestedAndLegacyEntries(t *testing.T) {
	raw := []byte(`{
		"2": {
			"control_id": "AU-2",
			"type": "Control",
			"name": "Legacy Entry",
			"requirements": "legacy req",
			"discussion": "legacy discussion"
		},
		"1": {
			"nist": {
				"control_id": "AU-1",
				"type": "Control",
				"name": "Nested Entry",
				"nist_requirements": "nested req",
				"nist_discussion": "nested discussion"
			},
			"appendix": {
				"owner": "team"
			}
		}
	}`)

	data, err := ReadBytes(raw)
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}

	if data["1"].NIST.Requirements != "nested req" || data["1"].NIST.Discussion != "nested discussion" {
		t.Fatalf("nested entry not decoded correctly: %+v", data["1"].NIST)
	}
	if data["1"].Appendix["owner"] != "team" {
		t.Fatalf("expected appendix owner=team, got %+v", data["1"].Appendix)
	}
	if data["2"].NIST.Requirements != "legacy req" || data["2"].NIST.Discussion != "legacy discussion" {
		t.Fatalf("legacy entry fallback not decoded correctly: %+v", data["2"].NIST)
	}
}

func TestWrite_SortsNumericKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controls.json")
	data := File{
		"10": {NIST: NISTEntry{ControlID: "AU-10", Name: "Ten"}},
		"2":  {NIST: NISTEntry{ControlID: "AU-2", Name: "Two"}},
		"1":  {NIST: NISTEntry{ControlID: "AU-1", Name: "One"}},
	}

	if err := Write(path, data); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	text := string(raw)
	first := strings.Index(text, `"1"`)
	second := strings.Index(text, `"2"`)
	third := strings.Index(text, `"10"`)
	if !(first < second && second < third) {
		t.Fatalf("expected numeric key order 1,2,10; got %q", text)
	}
}
