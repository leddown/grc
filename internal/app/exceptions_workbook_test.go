package app

import (
	"archive/zip"
	"bytes"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/exceptionbook"
)

func TestExceptionsWorkbookDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := sqliteNoopDB(t)
	userHandler, _, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler, err := buildHandlers(conn)
	if err != nil {
		t.Fatalf("buildHandlers: %v", err)
	}
	router := gin.New()
	registerRoutes(router, userHandler, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler,
		noAuthMiddleware, false, true)

	nfrs, err := nfrHandler.Service().List("", "")
	if err != nil || len(nfrs) == 0 {
		t.Fatalf("no seeded NFRs: %v", err)
	}

	download := func(query string) map[string]string {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/exceptions-workbook/download"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%.200s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != xlsxContentType {
			t.Fatalf("Content-Type=%q", got)
		}
		disposition := rec.Header().Get("Content-Disposition")
		if !strings.HasPrefix(disposition, `attachment; filename="grc-exceptions-workbook-`) || !strings.HasSuffix(disposition, `.xlsx"`) {
			t.Fatalf("Content-Disposition=%q", disposition)
		}
		archive, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil {
			t.Fatalf("the download is not a zip: %v", err)
		}
		parts := map[string]string{}
		for _, entry := range archive.File {
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			parts[entry.Name] = string(raw)
		}
		return parts
	}

	empty := download("")
	for _, name := range []string{exceptionbook.SheetGuide, exceptionbook.SheetExceptions, exceptionbook.SheetSummary,
		exceptionbook.SheetNFRDetail, exceptionbook.SheetControlDetail, exceptionbook.SheetLinks, exceptionbook.SheetControls} {
		if !strings.Contains(empty["xl/workbook.xml"], `<sheet name="`+name+`"`) {
			t.Errorf("the workbook has no %q sheet", name)
		}
	}
	// The Exceptions sheet is the second one.
	if !strings.Contains(empty["xl/worksheets/sheet2.xml"], "Exceptions: 0 | Linked Controls: 0 | C: 0 | I: 0 | A: 0") {
		t.Error("a workbook downloaded with no selection does not open empty")
	}
	if !strings.Contains(empty["xl/worksheets/sheet7.xml"], "<f>") {
		t.Error("the Controls sheet has no formulas")
	}

	total := strconv.Itoa(len(nfrs))
	// Downloaded with no selections, the workbook has none: nothing is in
	// scope and nothing is excepted.
	if !strings.Contains(empty["xl/worksheets/sheet2.xml"], "Scope: 0 of "+total+" Security NFRs") {
		t.Error("a workbook downloaded with no selections has requirements in scope")
	}
	if strings.Contains(empty["xl/worksheets/sheet2.xml"], "<v>Yes</v>") || strings.Contains(empty["xl/worksheets/sheet2.xml"], "<t>Yes</t>") {
		t.Error("a workbook downloaded with no selections has a Scope cell filled in")
	}

	// With exceptions and no scope named, the whole catalog is in scope, as on
	// the Exceptions page.
	selected := download("?key=" + nfrs[0].Key + "&key=" + nfrs[1].Key + "&key=no-such-key")
	if !strings.Contains(selected["xl/worksheets/sheet2.xml"], "Scope: "+total+" of "+total+" Security NFRs") {
		t.Error("?key= without ?scope= does not have the whole catalog in scope")
	}
	if !strings.Contains(selected["xl/worksheets/sheet2.xml"], "Exceptions: 2 | ") {
		t.Error("?key= did not select the requirements in the workbook")
	}

	// The second key is selected but outside the scope, so it is not excepted.
	scoped := download("?scope=" + nfrs[0].Key + "&scope=" + nfrs[2].Key + "&key=" + nfrs[0].Key + "&key=" + nfrs[1].Key)
	sheet := scoped["xl/worksheets/sheet2.xml"]
	if !strings.Contains(sheet, "Scope: 2 of "+total+" Security NFRs") || !strings.Contains(sheet, "Exceptions: 1 | ") {
		t.Error("?scope= did not set the scope the selection is drawn from")
	}
	// The plain download and one with selections are the same workbook but for
	// what is selected: the same sheets, and on the Exceptions sheet the same
	// header row.
	header := regexp.MustCompile(`<row r="5"[^>]*>.*?</row>`)
	plainHeader, selectedHeader := header.FindString(empty["xl/worksheets/sheet2.xml"]), header.FindString(scoped["xl/worksheets/sheet2.xml"])
	if plainHeader == "" || plainHeader != selectedHeader {
		t.Errorf("the two downloads do not have the same columns:\n%.300s\n%.300s", plainHeader, selectedHeader)
	}
	if empty["xl/workbook.xml"] != scoped["xl/workbook.xml"] {
		t.Error("the two downloads do not have the same sheets")
	}
	if none := download("?scope=")["xl/worksheets/sheet2.xml"]; !strings.Contains(none, "Scope: 0 of "+total+" Security NFRs") {
		t.Error("an empty ?scope= must put nothing in scope")
	}

	// Two requirements of weight 3, the second excepted in part: 3 + 1.5.
	if nfrs[0].Weight != 3 || nfrs[1].Weight != 3 {
		t.Fatalf("the seeded weights are %d and %d, want 3 and 3", nfrs[0].Weight, nfrs[1].Weight)
	}
	partial := download("?key=" + nfrs[0].Key + "&key=" + nfrs[1].Key + "&partial=" + nfrs[1].Key + "&partial=" + nfrs[2].Key)
	if !strings.Contains(partial["xl/worksheets/sheet2.xml"], "Exceptions: 2 | ") {
		t.Error("?partial= did not keep both exceptions")
	}
	// The weight of the selection is a cell of the Summary sheet, the third.
	if !strings.Contains(partial["xl/worksheets/sheet3.xml"], "<v>4.5</v>") {
		t.Error("?partial= did not count the second requirement at half its weight")
	}
}

func TestExceptionsWorkbookWithoutServices(t *testing.T) {
	router := newNavigationTestRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/exceptions-workbook/download", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
	}
}

// The Exceptions Report page is the Summary sheet of the workbook, worked out
// by the same code for the scope and the exceptions in its address.
func TestExceptionsReportPageShowsTheSummarySheet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := sqliteNoopDB(t)
	userHandler, _, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler, err := buildHandlers(conn)
	if err != nil {
		t.Fatalf("buildHandlers: %v", err)
	}
	router := gin.New()
	registerRoutes(router, userHandler, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler,
		noAuthMiddleware, false, true)

	data, err := loadExceptionsWorkbookData(nfrHandler.Service(), linkHandler.Service(), controlHandler.Service())
	if err != nil || len(data.NFRs) < 4 {
		t.Fatalf("no seeded catalog: %v", err)
	}
	keys := []string{data.NFRs[0].Key, data.NFRs[1].Key, data.NFRs[2].Key, data.NFRs[3].Key}

	page := func(query string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/exceptions-report"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%.200s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	// Every line of the summary is on the page, in order, in one table row.
	holds := func(name, body string, opts exceptionbook.Options) {
		t.Helper()
		at := 0
		for _, section := range exceptionbook.Evaluate(data, opts).Summary() {
			lines := []string{"<th>" + html.EscapeString(section.Title) + "</th>"}
			for _, row := range section.Rows {
				line := "<tr><td>" + html.EscapeString(row.Label) + "</td>"
				for _, value := range row.Values {
					line += "<td>" + html.EscapeString(value) + "</td>"
				}
				lines = append(lines, line)
			}
			for _, line := range lines {
				next := strings.Index(body[at:], line)
				if next < 0 {
					t.Fatalf("%s: the page does not have %q after position %d", name, line, at)
				}
				at += next + len(line)
			}
		}
	}

	plain := page("")
	holds("no exercise", plain, exceptionbook.Options{})
	if !strings.Contains(plain, "No scope or exceptions are set") {
		t.Error("a plain address does not say that nothing is set")
	}
	for _, want := range []string{"<title>Exceptions Report · GRC</title>", "<h1>Exceptions Report</h1>", "storedExercise()", "window.location.replace(window.location.pathname + '?' + storedExercise());"} {
		if !strings.Contains(plain, want) {
			t.Errorf("/reports/exceptions-report is missing %q", want)
		}
	}

	// Three NFRs in scope, two of them excepted, one in part; the fourth is
	// selected but out of scope.
	query := "?scope=" + keys[0] + "&scope=" + keys[1] + "&scope=" + keys[2] +
		"&key=" + keys[0] + "&key=" + keys[1] + "&key=" + keys[3] + "&partial=" + keys[1]
	opts := exceptionbook.Options{Scope: keys[:3], Scoped: true, Selected: []string{keys[0], keys[1], keys[3]}, Partial: []string{keys[1]}}
	scoped := page(query)
	holds("an exercise", scoped, opts)
	for _, want := range []string{
		"<tr><td>Security NFRs in scope</td><td>3</td>",
		"<tr><td>Selected Security NFRs</td><td>2</td>",
		"<tr><td>Weight of the selection</td><td>4.5</td>",
		"<tr><td>Weight of the scope</td><td>9</td>",
		"<tr><td>Share of the scope&#39;s weight</td><td>50%</td>",
		"<tr><td>Average weight of a selected NFR</td><td>2.25</td>",
		"<tr><td>3</td><td>3</td><td>2</td>",
	} {
		if !strings.Contains(scoped, want) {
			t.Errorf("the report for the exercise is missing %q", want)
		}
	}
	if strings.Contains(scoped, "No scope or exceptions are set") {
		t.Error("the report for an exercise says that nothing is set")
	}
}

// Without the catalogs the page still opens, with its menu, and says so.
func TestExceptionsReportPageWithoutServices(t *testing.T) {
	router := newNavigationTestRouter(t)
	body := getPageBody(t, router, "/reports/exceptions-report")
	if !strings.Contains(body, "The catalogs could not be read") {
		t.Error("the page does not say that the catalogs could not be read")
	}
}
