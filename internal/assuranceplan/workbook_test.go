package assuranceplan

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/xlsx"
)

func sheetNamed(t *testing.T, book *xlsx.Workbook, name string) *xlsx.Sheet {
	t.Helper()
	for _, s := range book.Sheets() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("no sheet %q", name)
	return nil
}

func storedText(s *xlsx.Sheet, col, row int) string {
	text, number, isNumber := s.Stored(col, row)
	if isNumber {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return text
}

func TestPlanWorkbookLaysOutThePlan(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 2)
	item := mustPopulation(t, svc, p.ID)[0]
	formula := `=HYPERLINK("http://x","y")`
	if _, err := svc.UpdatePopulationItem(p.ID, item.ID, PopulationPatch{Name: &formula}, "alice"); err != nil {
		t.Fatal(err)
	}
	e, err := svc.Export(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	book := PlanWorkbook(e)

	var names []string
	for _, s := range book.Sheets() {
		names = append(names, s.Name())
	}
	want := []string{wbGuide, wbPlan, wbSchedule, wbQuarters, wbStrata, wbScript, wbPopulation, wbTests, wbResults,
		wbDeviations, wbCoverage, wbFindings, wbGovernance, wbHistory}
	if !slices.Equal(names, want) {
		t.Fatalf("sheets = %v", names)
	}
	if book.Active != 1 {
		t.Errorf("the workbook opens on sheet %d, not the Plan", book.Active)
	}

	// The column constants the formulas use have to be where the headers are.
	for _, c := range []struct {
		sheet string
		cols  int
		rows  int
	}{
		{wbSchedule, 12, len(e.Steps)},
		{wbQuarters, qtSelectedAt, len(e.Quarters)},
		{wbStrata, stError, len(e.Strata)},
		{wbScript, 12, len(e.Attributes)},
		{wbPopulation, poCovered1 + 3, len(e.Population)},
		{wbTests, tsQANote, len(e.Tests)},
		{wbDeviations, dvRisk, len(e.Deviations)},
		{wbCoverage, cvQ1 + 15, len(e.Coverage)},
		{wbFindings, fdItem, len(e.Findings)},
	} {
		s := sheetNamed(t, book, c.sheet)
		cols, rows := s.Size()
		if cols != c.cols || rows != c.rows+1 {
			t.Errorf("%s is %d columns by %d rows, want %d by %d", c.sheet, cols, rows, c.cols, c.rows+1)
		}
	}
	checks := map[string]map[int]string{
		wbPopulation: {poAttested: "Attested on", poDQ: "DQ status", poTestRef: "Test", poSignedOff: "Signed off on", poCoveredNow: "Covered today", poCovered1: "Covered at Q1 end"},
		wbTests:      {tsRequested: "Evidence requested", tsDue: "Evidence due", tsQADate: "QA signed off", tsPass: "Pass", tsPending: "Pending", tsOverall: "Overall"},
		wbResults:    {rsTest: "Test", rsQuarter: "Quarter", rsStratum: "Stratum", rsAttr: "Attribute", rsResult: "Result"},
		wbStrata:     {stEffortTest: "Effort per test (days)", stTests1: "Tests Q1", stTotal: "Total tests", stReviews1: "Reviews due Q1", stError: "Error"},
		wbQuarters:   {qtEnd: "Quarter end", qtUtil: "Utilisation", qtOver: "Over threshold"},
		wbDeviations: {dvSampling: "Sampling", dvKind: "Rule", dvExtendQ: "Extend in"},
		wbFindings:   {fdStatus: "Status", fdOverdue: "Overdue", fdMissing: "Missing"},
	}
	for sheet, heads := range checks {
		s := sheetNamed(t, book, sheet)
		for col, head := range heads {
			if got := storedText(s, col, 1); got != head {
				t.Errorf("%s!%s is headed %q, want %q", sheet, xlsx.Ref(col, 1), got, head)
			}
		}
	}

	results := sheetNamed(t, book, wbResults)
	_, rows := results.Size()
	applicable, fails := 0, 0
	for _, x := range e.Tests {
		applicable += x.Passes + x.Fails + x.NotApplicable + x.Pending
	}
	for row := 2; row <= rows; row++ {
		if storedText(results, rsResult, row) == ResultFail {
			fails++
		}
	}
	if rows-1 != applicable || fails != e.Totals.AttributeFails || fails == 0 {
		t.Errorf("Results has %d rows and %d fails; the tests count %d results and %d fails", rows-1, fails, applicable, e.Totals.AttributeFails)
	}

	pop := sheetNamed(t, book, wbPopulation)
	if got := storedText(pop, poName, 2); got != formula || pop.Formula(poName, 2) != "" {
		t.Errorf("an item name starting with = is %q with formula %q", got, pop.Formula(poName, 2))
	}
	if f := sheetNamed(t, book, wbTests).Formula(tsOverall, 2); !strings.Contains(f, "Plan!$B$") {
		t.Errorf("the overall result does not read the plan's thresholds: %s", f)
	}

	locked := map[string]bool{wbPlan: true, wbQuarters: true, wbStrata: true, wbScript: true, wbDeviations: true, wbCoverage: true,
		wbFindings: true, wbGovernance: true, wbHistory: true}
	for _, s := range book.Sheets() {
		if s.Protected() != locked[s.Name()] {
			t.Errorf("%s locked = %v", s.Name(), s.Protected())
		}
	}
	if err := book.Write(&bytes.Buffer{}); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestExportXLSXRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 1)
	r := gin.New()
	NewHandler(svc, func(*gin.Context) string { return "tess" }).RegisterRoutes(r)
	w := do(t, r, http.MethodGet, "/assurance-plans/"+strconv.FormatInt(p.ID, 10)+"/export.xlsx", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export.xlsx = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("content type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="AP-2027-001.xlsx"` {
		t.Errorf("content disposition = %q", cd)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
		t.Error("the body is not a zip")
	}
	if w := do(t, r, http.MethodGet, "/assurance-plans/999999/export.xlsx", nil); w.Code != http.StatusNotFound {
		t.Errorf("an unknown plan = %d", w.Code)
	}
	if !strings.Contains(documentsHTML(p), "/export.xlsx") {
		t.Error("the documents table does not link the workbook")
	}
}

// resilienceYear is the resilience programme with Q1 drawn and tested: one
// test with a fail, both signed off in April.
func resilienceYear(t *testing.T, svc *Service) Plan {
	t.Helper()
	p := resiliencePlan(t, svc)
	at(svc, "2027-03-29")
	if _, err := svc.SelectQuarter(p.ID, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	tests, err := svc.CreateTests(p.ID, CreateTestsInput{Quarter: 1, Tester: "tess", EvidenceRequestedOn: "2027-04-01"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	p = mustGet(t, svc, p.ID)
	rt1 := attrByRef(t, p, "RT-01")
	for i, x := range tests {
		except := map[string]string{}
		if i == 0 {
			except[rt1.AttrRef] = ResultFail
		}
		if _, err := svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", EvidenceRequestedOn: "2027-04-01", EvidenceReceivedOn: "2027-04-09",
			FieldworkCompletedOn: "2027-04-20", Results: passFor(p, "SYS", except)}, "tess"); err != nil {
			t.Fatal(err)
		}
	}
	at(svc, "2027-04-28")
	for _, x := range tests {
		if _, err := svc.SignOffQA(p.ID, x.ID, "quinn"); err != nil {
			t.Fatal(err)
		}
	}
	return mustGet(t, svc, p.ID)
}

// TestPlanWorkbookFormulasInLibreOffice is the check that the workbook works.
// Each workbook is written with its formulas and none of their results,
// LibreOffice calculates it, and every cell it shows has to be what the
// application worked out. The exports are taken on today's date, because
// TODAY() in the workbook is today's date, and LibreOffice runs in UTC as the
// service does.
func TestPlanWorkbookFormulasInLibreOffice(t *testing.T) {
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

	exports := map[string]func(t *testing.T, svc *Service) int64{
		"empty": func(t *testing.T, svc *Service) int64 {
			p, err := svc.Create(CreateInput{Type: "controls-testing", Title: "Empty plan", PeriodStart: "2027-01-01"}, "alice")
			if err != nil {
				t.Fatal(err)
			}
			return p.ID
		},
		"controls-q2":   func(t *testing.T, svc *Service) int64 { return sampleYear(t, svc, 2).ID },
		"controls-year": func(t *testing.T, svc *Service) int64 { return sampleYear(t, svc, 4).ID },
		"resilience":    func(t *testing.T, svc *Service) int64 { return resilienceYear(t, svc).ID },
	}
	books := map[string]*xlsx.Workbook{}
	for name, build := range exports {
		svc, _ := newIntegratedService(t)
		id := build(t, svc)
		svc.now = time.Now
		e, err := svc.Export(id)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		books[name] = PlanWorkbook(e)
	}

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
	cmd := exec.CommandContext(ctx, office, args...)
	cmd.Env = append(os.Environ(), "TZ=UTC")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("LibreOffice: %v\n%s", err, output)
	}

	for name, book := range books {
		checked, formulas := 0, 0
		for _, sheet := range book.Sheets() {
			raw, err := os.ReadFile(filepath.Join(out, name+"-"+sheet.Name()+".csv"))
			if err != nil {
				t.Skipf("LibreOffice did not write a file for the sheet %q (one file per sheet needs LibreOffice 7.2 or later): %v", sheet.Name(), err)
			}
			records := readSheetCSV(string(raw))
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
					if sheet.Formula(col, row) != "" {
						formulas++
					}
					if sameShownCell(shown, text, number, isNumber) {
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
		t.Logf("%s: %d cells agree, %d of them formulas", name, checked, formulas)
	}
}

// readSheetCSV keeps empty lines, which encoding/csv drops: a sheet's blank
// row is one, and dropping it moves every row below it up.
func readSheetCSV(raw string) [][]string {
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

// sameShownCell compares what LibreOffice shows with what the workbook holds.
// A date LibreOffice may show as its serial or as the date itself.
func sameShownCell(shown, text string, number float64, isNumber bool) bool {
	if !isNumber {
		clean := func(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
		return clean(shown) == clean(text)
	}
	value := strings.TrimSpace(shown)
	if d, err := time.Parse("2006-01-02", value); err == nil {
		return xlsx.Serial(d) == number
	}
	scale := 1.0
	if strings.HasSuffix(value, "%") {
		value, scale = strings.TrimSuffix(value, "%"), 100
	}
	value = strings.TrimPrefix(value, "Q")
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}
	return math.Abs(parsed/scale-number) < 1e-9
}
