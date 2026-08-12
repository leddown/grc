package nfrfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBytes_FallsBackToLegacyAdditionalDetailsField(t *testing.T) {
	raw := []byte(`{
		"1": {
			"Summary": "Example",
			"ID": 2.0,
			"Additional Details ": "legacy details"
		}
	}`)

	data, err := ReadBytes(raw)
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}

	if got := data["1"].AdditionalDetails; got != "legacy details" {
		t.Fatalf("AdditionalDetails=%q want=%q", got, "legacy details")
	}
}

func TestWrite_SortsKeysAndIDToStringFormatsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nfr.json")
	data := File{
		"10": {Summary: "Ten", ID: json.Number("10")},
		"2":  {Summary: "Two", ID: 2.0},
		"1":  {Summary: "One", ID: " 1.0 "},
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

	if got := IDToString(" 1.0 "); got != "1.0" {
		t.Fatalf("IDToString string=%q want=%q", got, "1.0")
	}
	if got := IDToString(2.500000); got != "2.5" {
		t.Fatalf("IDToString float=%q want=%q", got, "2.5")
	}
	if got := IDToString(json.Number("7")); got != "7" {
		t.Fatalf("IDToString json.Number=%q want=%q", got, "7")
	}
}
