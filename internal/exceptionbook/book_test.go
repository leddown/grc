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
	got := Evaluate(fixture(), Options{Selected: []string{" 1 ", "10", "not-a-key"}})

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
	if got.SelectedWeight != 9 || got.ScopeWeight != 15 {
		t.Errorf("weight = %v of %d, want 9 of 15", got.SelectedWeight, got.ScopeWeight)
	}
	// With no scope given, the whole catalog is in scope.
	if want := "Scope: 5 of 5 Security NFRs"; got.ScopeLine() != want {
		t.Errorf("scope line = %q, want %q", got.ScopeLine(), want)
	}
	if want := "Linked Controls: 4 | C: 2 | I: 2 | A: 2"; got.CIALine() != want {
		t.Errorf("CIA line = %q, want %q", got.CIALine(), want)
	}
	if want := "Threat coverage: Tampering: 1 | Information Disclosure: 1 | Denial of Service: 1 | Lateral Movement: 1"; got.ThreatLine() != want {
		t.Errorf("threat line = %q, want %q", got.ThreatLine(), want)
	}

	// "01" is not "1": selecting it must not select the other, and the other's
	// controls must not come with it.
	other := Evaluate(fixture(), Options{Selected: []string{"01"}})
	if other.SelectedNFRs != 1 || other.LinkedControls != 1 || other.SelectedWeight != 2 {
		t.Errorf(`selecting "01": %d NFRs, %d controls, weight %v; want 1, 1, 2`, other.SelectedNFRs, other.LinkedControls, other.SelectedWeight)
	}

	none := Evaluate(fixture(), Options{})
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

// The scope is the set the exceptions are drawn from and the figures are
// measured against.
func TestEvaluateMeasuresAgainstTheScope(t *testing.T) {
	// Keys 1 (weight 5), 2 (weight 1) and 01 (weight 2) are in scope; key 10
	// is selected but outside it, so it is not an exception.
	got := Evaluate(fixture(), Options{
		Scope: []string{"1", " 2 ", "01", "not-a-key"}, Scoped: true,
		Selected: []string{"1", "10"},
	})
	if want := "Scope: 3 of 5 Security NFRs"; got.ScopeLine() != want {
		t.Errorf("scope line = %q, want %q", got.ScopeLine(), want)
	}
	if got.SelectedNFRs != 1 {
		t.Errorf("selected NFRs = %d, want 1: key 10 is out of scope", got.SelectedNFRs)
	}
	if got.LinkedControls != 2 {
		t.Errorf("linked controls = %d, want 2 (PL-8, CM-8)", got.LinkedControls)
	}
	if got.SelectedWeight != 5 || got.ScopeWeight != 8 {
		t.Errorf("weight = %v of %d, want 5 of 8", got.SelectedWeight, got.ScopeWeight)
	}

	// A scope that was given and is empty has nothing in it; that is not the
	// same as no scope.
	empty := Evaluate(fixture(), Options{Scoped: true, Selected: []string{"1"}})
	if empty.ScopeNFRs != 0 || empty.SelectedNFRs != 0 || empty.ScopeWeight != 0 {
		t.Errorf("an empty scope is not empty: %+v", empty)
	}
}

// The weight that touches a rating or a threat is the figure the analysis is
// read by. An NFR touches one when a control it is linked to carries it,
// and then counts its whole weight once.
//
// In the fixture key 1 (weight 5) reaches PL-8 and CM-8: C, I, A and
// Tampering. Keys 2 (1), 10 (4) and 01 (2) reach SC-7: C, I, A, Information
// Disclosure, Denial of Service and Lateral Movement; key 2 reaches AC-2 as
// well: Spoofing and Elevation of Privilege. Key 3 (3) is linked to nothing,
// and AU-2, the only control with Repudiation, is reached by no NFR.
func TestEvaluateWeighsWhatTheExceptionsTouch(t *testing.T) {
	coverage := func(excepted float64, scope int) WeightedCoverage {
		return WeightedCoverage{Excepted: excepted, Scope: scope}
	}
	threats := func(r Result) map[string]WeightedCoverage {
		out := map[string]WeightedCoverage{}
		for _, threat := range r.Threats {
			out[threat.Threat] = threat.Weight
		}
		return out
	}

	// Keys 1 and 10 excepted in full: 9 of the 12 that touch each rating. Key
	// 3 touches nothing, so its weight is in the scope's total and in no rating.
	got := Evaluate(fixture(), Options{Selected: []string{"1", "10"}})
	for name, w := range map[string]WeightedCoverage{"C": got.ConfidentialityWeight, "I": got.IntegrityWeight, "A": got.AvailabilityWeight} {
		if w != coverage(9, 12) || w.Share() != 75 {
			t.Errorf("%s by weight = %+v (%d%%), want 9 of 12 (75%%)", name, w, w.Share())
		}
	}
	wantThreats := map[string]WeightedCoverage{
		"Spoofing":               coverage(0, 1),
		"Tampering":              coverage(5, 5),
		"Repudiation":            coverage(0, 0),
		"Information Disclosure": coverage(4, 7),
		"Denial of Service":      coverage(4, 7),
		"Elevation of Privilege": coverage(0, 1),
		"Lateral Movement":       coverage(4, 7),
	}
	if !reflect.DeepEqual(threats(got), wantThreats) {
		t.Errorf("threats by weight = %+v, want %+v", threats(got), wantThreats)
	}
	if want := "CIA by weight: C: 9 of 12 (75%) | I: 9 of 12 (75%) | A: 9 of 12 (75%)"; got.CIAWeightLine() != want {
		t.Errorf("CIA by weight line = %q, want %q", got.CIAWeightLine(), want)
	}
	if want := "Threats by weight: Tampering: 5 of 5 (100%) | Information Disclosure: 4 of 7 (57%) | Denial of Service: 4 of 7 (57%) | Lateral Movement: 4 of 7 (57%)"; got.ThreatWeightLine() != want {
		t.Errorf("threats by weight line = %q, want %q", got.ThreatWeightLine(), want)
	}
	// The counts of controls are still what they were.
	if got.Confidentiality != 2 || got.Integrity != 2 || got.Availability != 2 {
		t.Errorf("C/I/A controls = %d/%d/%d, want 2/2/2", got.Confidentiality, got.Integrity, got.Availability)
	}

	// Key 1 in part counts half its weight wherever it counts: 2.5 + 4.
	partial := Evaluate(fixture(), Options{Selected: []string{"1", "10"}, Partial: []string{"1"}})
	if partial.ConfidentialityWeight != coverage(6.5, 12) || partial.ConfidentialityWeight.Share() != 54 {
		t.Errorf("C by weight = %+v (%d%%), want 6.5 of 12 (54%%)", partial.ConfidentialityWeight, partial.ConfidentialityWeight.Share())
	}
	if w := threats(partial)["Tampering"]; w != coverage(2.5, 5) || w.Share() != 50 {
		t.Errorf("Tampering by weight = %+v (%d%%), want 2.5 of 5 (50%%)", w, w.Share())
	}

	// A scope of keys 1, 2 and 01: 8 touch each rating, and key 10, out of
	// scope, is neither excepted nor in the 8. 5 of 8 is 62.5%, which is 63%.
	scoped := Evaluate(fixture(), Options{Scope: []string{"1", "2", "01"}, Scoped: true, Selected: []string{"1", "10"}})
	if scoped.AvailabilityWeight != coverage(5, 8) || scoped.AvailabilityWeight.Share() != 63 {
		t.Errorf("A by weight = %+v (%d%%), want 5 of 8 (63%%)", scoped.AvailabilityWeight, scoped.AvailabilityWeight.Share())
	}
	if w := threats(scoped)["Lateral Movement"]; w != coverage(0, 3) {
		t.Errorf("Lateral Movement by weight = %+v, want 0 of 3", w)
	}

	none := Evaluate(fixture(), Options{})
	if want := "Threats by weight: no linked threat mappings"; none.ThreatWeightLine() != want {
		t.Errorf("threats by weight line = %q, want %q", none.ThreatWeightLine(), want)
	}
	if want := "CIA by weight: C: 0 of 12 (0%) | I: 0 of 12 (0%) | A: 0 of 12 (0%)"; none.CIAWeightLine() != want {
		t.Errorf("CIA by weight line = %q, want %q", none.CIAWeightLine(), want)
	}
}

// An exception is full or partial, and a partial one counts half the weight
// of its NFR. Nothing else about it is halved.
func TestEvaluateCountsAPartialExceptionAsHalfItsWeight(t *testing.T) {
	// Key 1 (weight 5) in full, key 10 (weight 4) in part; key 2 is named as
	// partial without being selected, which means nothing.
	got := Evaluate(fixture(), Options{Selected: []string{"1", "10"}, Partial: []string{" 10 ", "2", "not-a-key"}})
	if got.SelectedNFRs != 2 || got.LinkedControls != 4 {
		t.Errorf("%d NFRs linked to %d, want 2 and 4: a partial exception is still an exception", got.SelectedNFRs, got.LinkedControls)
	}
	if got.SelectedWeight != 7 {
		t.Errorf("weight = %v, want 7 (5 + half of 4)", got.SelectedWeight)
	}

	// Key 1 in part: 2.5 + 4 of 15.
	half := Evaluate(fixture(), Options{Selected: []string{"1", "10"}, Partial: []string{"1"}})
	if half.SelectedWeight != 6.5 {
		t.Errorf("weight = %v, want 6.5 (half of 5 + 4)", half.SelectedWeight)
	}

	book, _ := Build(fixture(), Options{Selected: []string{"1", "10"}, Partial: []string{"1"}})
	sheet := book.Sheets()[1]
	marks := map[string]string{}
	for row := exFirstRow; row < exFirstRow+5; row++ {
		key, _, _ := sheet.Stored(exKey, row)
		marks[key], _, _ = sheet.Stored(exSel, row)
	}
	if marks["1"] != PartialMark || marks["10"] != FullMark || marks["2"] != "" {
		t.Errorf("Exception column = %v, want key 1 partial and key 10 full", marks)
	}
	summary := book.Sheets()[2]
	weightRow := layout{threats: len(half.Threats)}.smSelectedWeight()
	if _, weight, isNumber := summary.Stored(2, weightRow); !isNumber || weight != 6.5 {
		t.Errorf("the Summary sheet's weight of the selection = %v, want 6.5", weight)
	}
	if _, scope, isNumber := summary.Stored(2, weightRow+1); !isNumber || scope != 15 {
		t.Errorf("the Summary sheet's weight of the scope = %v, want 15", scope)
	}
}

// What is said of a partial exception is its cell in the Comments column, on
// the Exceptions sheet and on the Selected NFRs sheet that reads it.
func TestBuildPutsADescriptionInTheCommentsColumn(t *testing.T) {
	opts := Options{
		Selected: []string{"1", "10", "2"}, Partial: []string{"10", "2"},
		Descriptions: map[string]string{
			" 10 ": "  Filtered at the edge, not between zones.\nReview due in March.  ",
			"1":    "said of a full exception, so not used",
			"3":    "said of an NFR that is not excepted",
			"2":    `=HYPERLINK("http://example.test","a formula?")`,
		},
	}
	book, result := Build(fixture(), opts)
	if !reflect.DeepEqual(result, Evaluate(fixture(), Options{Selected: opts.Selected, Partial: opts.Partial})) {
		t.Error("a description changed a figure")
	}

	exceptions := book.Sheets()[1]
	comments := map[string]string{}
	for row := exFirstRow; row < exFirstRow+5; row++ {
		key, _, _ := exceptions.Stored(exKey, row)
		comments[key], _, _ = exceptions.Stored(exComments, row)
		if exceptions.Formula(exComments, row) != "" {
			t.Errorf("key %s: the Comments cell is a formula", key)
		}
	}
	want := map[string]string{
		"10": "Filtered at the edge, not between zones.\nReview due in March.",
		"2":  `=HYPERLINK("http://example.test","a formula?")`,
		"1":  "", "3": "", "01": "",
	}
	if !reflect.DeepEqual(comments, want) {
		t.Errorf("Comments column = %q, want %q", comments, want)
	}
	if header, _, _ := exceptions.Stored(exComments, exHeaderRow); header != "Comments" {
		t.Errorf("the column is headed %q", header)
	}

	// The Selected NFRs sheet lists the three exceptions, in the sheet's order.
	detail := book.Sheets()[3]
	if detail.Name() != SheetNFRDetail {
		t.Fatalf("sheet 4 is %q", detail.Name())
	}
	column := 0
	for col := 1; col <= 14; col++ {
		if header, _, _ := detail.Stored(col, detailHeaderRow); header == "Comments" {
			column = col
		}
	}
	if column == 0 {
		t.Fatal("the Selected NFRs sheet has no Comments column")
	}
	var listed []string
	for row := detailFirstRow; row < detailFirstRow+3; row++ {
		text, _, _ := detail.Stored(column, row)
		listed = append(listed, text)
	}
	if !reflect.DeepEqual(listed, []string{"", want["2"], want["10"]}) {
		t.Errorf("Selected NFRs comments = %q", listed)
	}
}

func TestBuildWritesTheWorkbook(t *testing.T) {
	book, result := Build(fixture(), Options{Selected: []string{"1", "10"}, GeneratedAt: time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)})
	if want := Evaluate(fixture(), Options{Selected: []string{"1", "10"}}); !reflect.DeepEqual(result, want) {
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
	// The two sheets that hold what the application exported are locked; the
	// ones a person works on are not.
	for _, sheet := range book.Sheets() {
		locked := sheet.Name() == SheetLinks || sheet.Name() == SheetControls
		if sheet.Protected() != locked {
			t.Errorf("sheet %q: protected = %v, want %v", sheet.Name(), sheet.Protected(), locked)
		}
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
	if marks["1"] != FullMark || marks["10"] != FullMark || marks["2"] != "" || marks["01"] != "" {
		t.Errorf("Exception column = %v, want only keys 1 and 10 marked, in full", marks)
	}
	if exScope != 1 || exSel != 2 {
		t.Errorf("Scope is column %d and Exception column %d, want A and B", exScope, exSel)
	}
	if scopeHeader, _, _ := exceptions.Stored(1, exHeaderRow); scopeHeader != "Scope" {
		t.Errorf("column A is headed %q, want Scope", scopeHeader)
	}
	if exceptionHeader, _, _ := exceptions.Stored(2, exHeaderRow); exceptionHeader != "Exception" {
		t.Errorf("column B is headed %q, want Exception", exceptionHeader)
	}

	// The shown columns have no key, no NFR id and no issue type, and the
	// description is headed Comments. The key is a working column: the Links sheet needs it.
	shown := []string{"Scope", "Exception", "Summary", "Domain", "Weight", "Linked Controls",
		"NIST Mapping", "Comments", "Additional Details", "Implementation"}
	for i, want := range shown {
		if header, _, _ := exceptions.Stored(i+1, exHeaderRow); header != want {
			t.Errorf("column %s is headed %q, want %q", xlsx.Col(i+1), header, want)
		}
	}
	columns, _ := exceptions.Size()
	for col := 1; col <= columns; col++ {
		header, _, _ := exceptions.Stored(col, exHeaderRow)
		if header == "Issue Type" || header == "Description" || header == "NFR ID" {
			t.Errorf("column %s is still headed %q", xlsx.Col(col), header)
		}
		if header == "Key" && col <= len(shown) {
			t.Errorf("the key is column %s, among the shown columns", xlsx.Col(col))
		}
	}
	if exKey <= exImplementation {
		t.Errorf("the key is column %d, want it after the shown columns", exKey)
	}

	guide, described := book.Sheets()[0], false
	_, guideRows := guide.Size()
	for row := 1; row <= guideRows; row++ {
		if text, _, _ := guide.Stored(1, row); strings.Contains(text, "Describe every partial exception in the Comments column ("+xlsx.Col(exComments)+")") {
			described = true
		}
	}
	if !described {
		t.Error("the How to use sheet does not say that a partial exception should have a description")
	}
	if line, _, _ := exceptions.Stored(1, 2); line != "Scope: 5 of 5 Security NFRs" {
		t.Errorf("scope line = %q", line)
	}
	if line, _, _ := exceptions.Stored(1, 3); line != "Exceptions: 2 | Linked Controls: 4 | C: 2 | I: 2 | A: 2" {
		t.Errorf("selection line = %q", line)
	}
	// Two lines and the hint, and then the header: the figures are on the
	// Summary sheet and are not repeated here.
	if exHeaderRow != 5 || exFirstRow != 6 {
		t.Errorf("the header is row %d and the first NFR row %d, want 5 and 6", exHeaderRow, exFirstRow)
	}
	for row := 1; row < exHeaderRow; row++ {
		if line, _, _ := exceptions.Stored(1, row); strings.Contains(line, "by weight") || strings.HasPrefix(line, "Threat coverage") || strings.HasPrefix(line, "Weight of the selection") {
			t.Errorf("row %d still holds a summary line: %q", row, line)
		}
	}
	// Nothing shows after Implementation: the working columns are hidden, and
	// they are still there for the formulas.
	for col := 1; col <= columns; col++ {
		if hidden := exceptions.Hidden(col); hidden != (col > exImplementation) {
			t.Errorf("column %s: hidden = %v", xlsx.Col(col), hidden)
		}
	}
	if columns <= exImplementation || exceptions.Formula(exSelected, exFirstRow) == "" {
		t.Error("the working columns are gone: the formulas of the other sheets read them")
	}

	scoped, _ := Build(fixture(), Options{Scope: []string{"1", "2", "01"}, Scoped: true, Selected: []string{"1", "10"}})
	sheet := scoped.Sheets()[1]
	inScope, excepted := map[string]string{}, map[string]string{}
	for row := exFirstRow; row < exFirstRow+5; row++ {
		key, _, _ := sheet.Stored(exKey, row)
		inScope[key], _, _ = sheet.Stored(exScope, row)
		excepted[key], _, _ = sheet.Stored(exSel, row)
	}
	if inScope["1"] != SelectedMark || inScope["2"] != SelectedMark || inScope["01"] != SelectedMark || inScope["10"] != "" || inScope["3"] != "" {
		t.Errorf("Scope column = %v, want only keys 1, 2 and 01 marked", inScope)
	}
	if excepted["1"] != FullMark || excepted["10"] != "" {
		t.Errorf("Exception column = %v, want key 1 marked and key 10, out of scope, not", excepted)
	}
	if line, _, _ := sheet.Stored(1, 2); line != "Scope: 3 of 5 Security NFRs" {
		t.Errorf("scope line = %q", line)
	}
	if line, _, _ := sheet.Stored(1, 3); line != "Exceptions: 1 | Linked Controls: 2 | C: 1 | I: 1 | A: 1" {
		t.Errorf("selection line = %q", line)
	}
}

// A page shows Result.Summary in place of the Summary sheet, so it has to be
// the sheet line for line: the same titles, headings, labels and figures.
func TestSummaryIsTheSummarySheet(t *testing.T) {
	shown := func(sheet *xlsx.Sheet, col, row int) string {
		text, number, isNumber := sheet.Stored(col, row)
		if isNumber {
			return strconv.FormatFloat(number, 'f', -1, 64)
		}
		return text
	}
	// The sheet holds a share as a fraction and an average as a number, and
	// leaves the average empty when nothing is selected.
	same := func(onSheet, onPage string) bool {
		if onSheet == onPage || onSheet == "" && onPage == "-" {
			return true
		}
		value, err := strconv.ParseFloat(onSheet, 64)
		if err != nil {
			return false
		}
		if percent, ok := strings.CutSuffix(onPage, "%"); ok {
			whole, err := strconv.Atoi(percent)
			return err == nil && roundHalfUp(value*100) == whole
		}
		figure, err := strconv.ParseFloat(onPage, 64)
		return err == nil && math.Abs(value-figure) < 1e-9
	}

	for name, opts := range map[string]Options{
		"nothing selected":     {},
		"two selected":         {Selected: []string{"1", "10"}},
		"a scope and partials": {Scope: []string{"1", "2", "01", "10"}, Scoped: true, Selected: []string{"1", "10", "2"}, Partial: []string{"2", "10"}},
		"an empty scope":       {Scoped: true, Selected: []string{"1"}},
	} {
		book, result := Build(fixture(), opts)
		sheet := book.Sheets()[2]
		if sheet.Name() != SheetSummary {
			t.Fatalf("sheet 3 is %q, want %q", sheet.Name(), SheetSummary)
		}
		// From the first section down to the last row, without the lines that
		// are empty.
		l := layout{threats: len(result.Threats)}
		var onSheet [][5]string
		for row := smCatalog - 1; row <= l.smLastRow(); row++ {
			line := [5]string{}
			for col := range line {
				line[col] = shown(sheet, col+1, row)
			}
			if line != [5]string{} {
				onSheet = append(onSheet, line)
			}
		}

		var onPage [][5]string
		for _, section := range result.Summary() {
			line := [5]string{section.Title}
			copy(line[1:], section.Columns)
			onPage = append(onPage, line)
			for _, row := range section.Rows {
				line := [5]string{row.Label}
				copy(line[1:], row.Values)
				onPage = append(onPage, line)
			}
		}

		if len(onSheet) != len(onPage) {
			t.Errorf("%s: the sheet has %d lines and the summary %d\nsheet:   %q\nsummary: %q", name, len(onSheet), len(onPage), onSheet, onPage)
			continue
		}
		for i := range onSheet {
			agree := onSheet[i][0] == onPage[i][0]
			for col := 1; col < len(onSheet[i]); col++ {
				agree = agree && same(onSheet[i][col], onPage[i][col])
			}
			if !agree {
				t.Errorf("%s, line %d: the sheet has %q and the summary %q", name, i+1, onSheet[i], onPage[i])
			}
		}
	}
}

func TestBuildWithNothingInTheCatalogs(t *testing.T) {
	book, result := Build(Data{}, Options{})
	if result.SelectedNFRs != 0 || result.ScopeWeight != 0 {
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
	keys, scope := []string{}, []string{}
	for i, nfr := range catalog.NFRs {
		// A spread of weights, so the weight figures are not all threes.
		catalog.NFRs[i].Weight = 1 + i%5
		if i%9 == 0 {
			keys = append(keys, nfr.Key)
		}
		// Two of every three NFRs, so the scope is not the whole catalog.
		if i%3 != 1 {
			scope = append(scope, nfr.Key)
		}
	}
	books := map[string]*xlsx.Workbook{}
	books["fixture"], _ = Build(fixture(), Options{Selected: []string{"1", "10"}})
	books["fixture-01"], _ = Build(fixture(), Options{Selected: []string{"01", "2"}})
	books["fixture-none"], _ = Build(fixture(), Options{})
	books["fixture-scope"], _ = Build(fixture(), Options{Scope: []string{"1", "2", "01"}, Scoped: true, Selected: []string{"1", "10", "2"}})
	books["fixture-scope-empty"], _ = Build(fixture(), Options{Scoped: true, Selected: []string{"1"}})
	books["fixture-described"], _ = Build(fixture(), Options{Selected: []string{"1", "10", "2"}, Partial: []string{"10", "2"},
		Descriptions: map[string]string{"10": "Filtered at the edge, not between zones.\nReview due in March.", "2": `=HYPERLINK("http://example.test","a formula?")`}})
	books["fixture-partial"], _ = Build(fixture(), Options{Selected: []string{"1", "10", "2"}, Partial: []string{"1", "2"}})
	books["fixture-scope-partial"], _ = Build(fixture(), Options{Scope: []string{"1", "2", "01"}, Scoped: true, Selected: []string{"1", "10", "2"}, Partial: []string{"2", "10"}})
	books["catalog"], _ = Build(catalog, Options{Selected: keys})
	books["catalog-scope"], _ = Build(catalog, Options{Scope: scope, Scoped: true, Selected: keys})
	books["catalog-partial"], _ = Build(catalog, Options{Scope: scope, Scoped: true, Selected: keys, Partial: keys[:len(keys)/2]})

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
