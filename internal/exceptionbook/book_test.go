package exceptionbook

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"grc/internal/controlcatalog"
	"grc/internal/db"
	"grc/internal/nfrlink"
	"grc/internal/securitynfr"
	"grc/internal/xlsx"
)

// fixture holds the cases the page's logic turns on: a requirement that
// reaches one control through two mappings, a mapping that matched nothing, a
// control two requirements share, a linked control the catalog does not list,
// a link whose requirement is gone, keys that differ only as text, a threat
// outside the usual six, and text a spreadsheet could mistake for a formula.
func fixture() Data {
	link := func(key, mapping, control string, matched bool) nfrlink.LinkRow {
		return nfrlink.LinkRow{NFRKey: key, NFRID: "id-" + key, NFRSummary: "Summary of " + key,
			MappingControlID: mapping, ControlID: control, ControlName: "Name of " + control, ControlFamily: "FAM", Matched: matched}
	}
	return Data{
		NFRs: []securitynfr.NFR{
			{Key: "10", ID: "2.1", Summary: "Boundaries are filtered", Domain: "Network", IssueType: "Control", Weight: 4, NISTMapping: "SC-7, AC-99"},
			{Key: "1", ID: "0.1", Summary: "Architecture is documented", Domain: "Design", IssueType: "Control", Weight: 5,
				NISTMapping: "PL-8, CM-8, CM-08, XX-1", AdditionalDetails: "line one\nline two", Implementation: "C4 levels 1 to 3"},
			{Key: "2", ID: "1.1", Summary: `=HYPERLINK("http://example.test","click")`, Domain: "Build & <Run>", Weight: 1, NISTMapping: "AC-2, SC-7"},
			{Key: "3", ID: "1.10", Summary: "Nothing is linked to this one", Weight: 3},
			{Key: "01", ID: "9.9", Summary: "A key that is not the key 1", Domain: "Network", Weight: 2, NISTMapping: "SC-7"},
		},
		Links: []nfrlink.LinkRow{
			link("1", "PL-8", "PL-8", true),
			link("1", "CM-8", "CM-8", true),
			link("1", "CM-08", "CM-8", true),
			link("1", "XX-1", "", false),
			link("2", "AC-2", "AC-2", true),
			link("2", "SC-7", "SC-7", true),
			link("10", "SC-7", "SC-7", true),
			link("10", "AC-99", "AC-99", true),
			link("01", "SC-7", "SC-7", true),
			link("404", "AU-2", "AU-2", true),
		},
		Controls: []controlcatalog.Control{
			{ID: "PL-8", Name: "Security and Privacy Architectures", Family: "PL", Confidentiality: "Moderate"},
			{ID: "CM-8", Name: "System Component Inventory", Family: "CM", Integrity: "High", Availability: "Low",
				Threats: []string{"Tampering", "Tampering", " "}},
			{ID: "AC-2", Name: "Account Management", Family: "AC", Confidentiality: "x", Integrity: "x",
				Threats: []string{"Spoofing", "Elevation of Privilege"}},
			{ID: "SC-7", Name: "Boundary Protection", Family: "SC", Confidentiality: "High", Integrity: "High", Availability: "High",
				Threats: []string{"Lateral Movement", "Denial of Service", "Information Disclosure"}},
			{ID: "AU-2", Name: "Event Logging", Family: "AU", Integrity: "Low", Threats: []string{"Repudiation"}},
			{ID: "SI-1", Name: "Policy and Procedures", Family: "SI", Availability: " "},
		},
	}
}

func TestEvaluateFollowsThePage(t *testing.T) {
	got := Evaluate(fixture(), []string{" 1 ", "10", "not-a-key"})

	if got.SelectedNFRs != 2 {
		t.Errorf("selected NFRs = %d, want 2", got.SelectedNFRs)
	}
	// CM-8 once although two mappings reach it; AC-99 although the catalog does
	// not list it; XX-1 matched nothing.
	if got.LinkedControls != 4 {
		t.Errorf("linked controls = %d, want 4 (CM-8, PL-8, SC-7, AC-99)", got.LinkedControls)
	}
	if got.Confidentiality != 2 || got.Integrity != 2 || got.Availability != 2 {
		t.Errorf("C/I/A = %d/%d/%d, want 2/2/2", got.Confidentiality, got.Integrity, got.Availability)
	}
	if got.SelectedWeight != 9 || got.CatalogWeight != 15 {
		t.Errorf("weight = %d of %d, want 9 of 15", got.SelectedWeight, got.CatalogWeight)
	}
	if want := "Linked Controls: 4 | C: 2 | I: 2 | A: 2"; got.CIALine() != want {
		t.Errorf("CIA line = %q, want %q", got.CIALine(), want)
	}
	if want := "Threat coverage: Tampering: 1 | Information Disclosure: 1 | Denial of Service: 1 | Lateral Movement: 1"; got.ThreatLine() != want {
		t.Errorf("threat line = %q, want %q", got.ThreatLine(), want)
	}

	// "01" is not "1": selecting it must not select the other, and the other's
	// controls must not come with it.
	other := Evaluate(fixture(), []string{"01"})
	if other.SelectedNFRs != 1 || other.LinkedControls != 1 || other.SelectedWeight != 2 {
		t.Errorf(`selecting "01": %d NFRs, %d controls, weight %d; want 1, 1, 2`, other.SelectedNFRs, other.LinkedControls, other.SelectedWeight)
	}

	none := Evaluate(fixture(), nil)
	if none.SelectedNFRs != 0 || none.LinkedControls != 0 || none.SelectedWeight != 0 {
		t.Errorf("an empty selection is not empty: %+v", none)
	}
	if want := "Threat coverage: no linked threat mappings"; none.ThreatLine() != want {
		t.Errorf("threat line = %q, want %q", none.ThreatLine(), want)
	}
	if len(none.Threats) != 7 || none.Threats[0].Threat != "Spoofing" || none.Threats[6].Threat != "Lateral Movement" {
		t.Errorf("threats = %+v, want the six in the page's order and then Lateral Movement", none.Threats)
	}
}

func TestBuildWritesTheWorkbook(t *testing.T) {
	book, result := Build(fixture(), Options{Selected: []string{"1", "10"}, GeneratedAt: time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)})
	if want := Evaluate(fixture(), []string{"1", "10"}); !reflect.DeepEqual(result, want) {
		t.Errorf("Build gave %+v, Evaluate %+v", result, want)
	}

	names := []string{}
	for _, sheet := range book.Sheets() {
		names = append(names, sheet.Name())
	}
	want := []string{SheetGuide, SheetExceptions, SheetSummary, SheetNFRDetail, SheetControlDetail, SheetLinks, SheetControls}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("sheets = %q, want %q", names, want)
	}
	if book.Active != 1 {
		t.Errorf("the workbook opens on sheet %d, want the Exceptions sheet", book.Active)
	}
	var file bytes.Buffer
	if err := book.Write(&file); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.HasPrefix(file.Bytes(), []byte("PK")) {
		t.Fatal("the workbook is not a zip file")
	}

	exceptions := book.Sheets()[1]
	// Rows are in the page's order: by NFR id, 0.1 first.
	marks := map[string]string{}
	for row := exFirstRow; row < exFirstRow+5; row++ {
		key, _, _ := exceptions.Stored(exKey, row)
		mark, _, _ := exceptions.Stored(exSel, row)
		marks[key] = mark
		if _, weight, isNumber := exceptions.Stored(exWeight, row); !isNumber || weight < 1 || weight > 5 {
			t.Errorf("key %s: weight %v is not a number from 1 to 5", key, weight)
		}
	}
	if first, _, _ := exceptions.Stored(exKey, exFirstRow); first != "1" {
		t.Errorf("the first requirement is key %q, want key 1 (NFR id 0.1)", first)
	}
	if marks["1"] != SelectedMark || marks["10"] != SelectedMark || marks["2"] != "" || marks["01"] != "" {
		t.Errorf("Except column = %v, want only keys 1 and 10 marked", marks)
	}
	if line, _, _ := exceptions.Stored(1, 2); line != "Selected Security NFRs: 2 | Linked Controls: 4 | C: 2 | I: 2 | A: 2" {
		t.Errorf("summary line = %q", line)
	}
	if line, _, _ := exceptions.Stored(1, 4); line != "Weight of the selection: 9 of 15 (60%)" {
		t.Errorf("weight line = %q", line)
	}
}

func TestBuildWithNothingInTheCatalogs(t *testing.T) {
	book, result := Build(Data{}, Options{})
	if result.SelectedNFRs != 0 || result.CatalogWeight != 0 {
		t.Errorf("an empty catalog gave %+v", result)
	}
	if err := book.Write(&bytes.Buffer{}); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestFormulasInLibreOffice is the check that the workbook works. Each
// workbook is written with its formulas and none of their results, LibreOffice
// calculates it, and every cell it shows has to be what evaluate said that
// cell holds.
func TestFormulasInLibreOffice(t *testing.T) {
	if testing.Short() {
		t.Skip("the LibreOffice check is skipped in -short mode")
	}
	office := ""
	for _, name := range []string{"soffice", "libreoffice"} {
		if path, err := exec.LookPath(name); err == nil {
			office = path
			break
		}
	}
	if office == "" {
		t.Skip("no LibreOffice on PATH")
	}

	catalog := seededCatalog(t)
	keys := []string{}
	for i, nfr := range catalog.NFRs {
		// A spread of weights, so the weight figures are not all threes.
		catalog.NFRs[i].Weight = 1 + i%5
		if i%9 == 0 {
			keys = append(keys, nfr.Key)
		}
	}
	books := map[string]*xlsx.Workbook{}
	books["fixture"], _ = Build(fixture(), Options{Selected: []string{"1", "10"}})
	books["fixture-01"], _ = Build(fixture(), Options{Selected: []string{"01", "2"}})
	books["fixture-none"], _ = Build(fixture(), Options{})
	books["catalog"], _ = Build(catalog, Options{Selected: keys})

	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	args := []string{"--headless", "--norestore", "-env:UserInstallation=file://" + filepath.Join(dir, "profile"),
		"--convert-to", "csv:Text - txt - csv (StarCalc):44,34,76,1,,0,false,false,false,false,false,-1", "--outdir", out}
	for name, book := range books {
		book.OmitFormulaValues = true
		path := filepath.Join(dir, name+".xlsx")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := book.Write(file); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		args = append(args, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(ctx, office, args...).CombinedOutput(); err != nil {
		t.Fatalf("LibreOffice: %v\n%s", err, output)
	}

	for name, book := range books {
		checked := 0
		for _, sheet := range book.Sheets() {
			raw, err := os.ReadFile(filepath.Join(out, name+"-"+sheet.Name()+".csv"))
			if err != nil {
				// One file for each sheet needs LibreOffice 7.2 or later.
				t.Skipf("LibreOffice did not write a file for the sheet %q: %v", sheet.Name(), err)
			}
			records := readCSV(string(raw))
			cols, rows := sheet.Size()
			failures := 0
			for row := 1; row <= rows; row++ {
				for col := 1; col <= cols; col++ {
					shown := ""
					if row <= len(records) && col <= len(records[row-1]) {
						shown = records[row-1][col-1]
					}
					text, number, isNumber := sheet.Stored(col, row)
					checked++
					if sameCell(shown, text, number, isNumber) {
						continue
					}
					if failures++; failures <= 8 {
						t.Errorf("%s, %s!%s: LibreOffice shows %q, the workbook says %q / %v\n  formula: %s",
							name, sheet.Name(), xlsx.Ref(col, row), shown, text, number, sheet.Formula(col, row))
					}
				}
			}
			if failures > 8 {
				t.Errorf("%s, %s: %d cells differ in all", name, sheet.Name(), failures)
			}
		}
		t.Logf("%s: %d cells agree", name, checked)
	}
}

// readCSV keeps empty lines, which encoding/csv drops: a sheet's blank row is
// one, and dropping it moves every row below it up.
func readCSV(raw string) [][]string {
	records := [][]string{}
	record := []string{}
	var field strings.Builder
	quoted := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case quoted && c == '"' && i+1 < len(raw) && raw[i+1] == '"':
			field.WriteByte('"')
			i++
		case c == '"':
			quoted = !quoted
		case !quoted && c == ',':
			record = append(record, field.String())
			field.Reset()
		case !quoted && c == '\n':
			record = append(record, strings.TrimSuffix(field.String(), "\r"))
			field.Reset()
			records = append(records, record)
			record = []string{}
		default:
			field.WriteByte(c)
		}
	}
	if field.Len() > 0 || len(record) > 0 {
		records = append(records, append(record, field.String()))
	}
	return records
}

func sameCell(shown, text string, number float64, isNumber bool) bool {
	if !isNumber {
		clean := func(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
		return clean(shown) == clean(text)
	}
	value := strings.TrimSpace(shown)
	scale := 1.0
	if strings.HasSuffix(value, "%") {
		value, scale = strings.TrimSuffix(value, "%"), 100
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}
	return math.Abs(parsed/scale-number) < 1e-9
}

// seededCatalog is the catalog a new installation starts with, linked the way
// the application links it.
func seededCatalog(t *testing.T) Data {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	controls := controlcatalog.NewService(controlcatalog.NewSQLiteRepository(conn), "")
	if _, err := controls.Seed(); err != nil {
		t.Fatalf("seed controls: %v", err)
	}
	nfrs := securitynfr.NewService(securitynfr.NewSQLiteRepository(conn), "")
	if _, err := nfrs.Seed(); err != nil {
		t.Fatalf("seed NFRs: %v", err)
	}
	links := nfrlink.NewService(conn)
	if err := links.Rebuild(); err != nil {
		t.Fatalf("link: %v", err)
	}

	var data Data
	if data.NFRs, err = nfrs.List("", ""); err != nil {
		t.Fatal(err)
	}
	if data.Links, err = links.List("", false); err != nil {
		t.Fatal(err)
	}
	if data.Controls, err = controls.List("", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(data.NFRs) == 0 || len(data.Links) == 0 || len(data.Controls) == 0 {
		t.Fatalf("the seeded catalog is empty: %d NFRs, %d links, %d controls", len(data.NFRs), len(data.Links), len(data.Controls))
	}
	return data
}
