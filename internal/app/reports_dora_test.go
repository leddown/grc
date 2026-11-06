package app

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/exceptionbook"
)

// The DORA Report shows what internal/exceptionbook works out for the workbook's
// DORA sheet, for the exercise in its address.
func TestDORAPageShowsTheReport(t *testing.T) {
	router, data := newDORATestRouter(t)
	page := func(query string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/dora"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%.200s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	holds := func(name, body string, report exceptionbook.DORAReport, exercise url.Values) {
		t.Helper()
		for _, row := range report.Summary().Rows {
			want := "<tr><td>" + html.EscapeString(row.Label) + "</td><td>" + row.Values[0] + "</td>"
			if !strings.Contains(body, want) {
				t.Errorf("%s: the page does not have %q", name, want)
			}
		}
		at := 0
		for _, row := range report.Rows {
			want := `<tr><td>` + html.EscapeString(row.Regulation) + `</td><td class="number">` + html.EscapeString(orDash(row.Reference)) + `</td>` +
				`<td>` + html.EscapeString(orDash(row.Title)) + `</td>` +
				`<td class="number"><a href="` + doraDetailHref(row, doraShowNFRs, exercise) + `">` + strconv.Itoa(row.NFRsInScope) + `</a></td>` +
				`<td class="number"><a href="` + doraDetailHref(row, doraShowControls, exercise) + `">` + strconv.Itoa(row.ControlsInScope) + `</a></td>` +
				`<td class="number">` + strconv.Itoa(row.Exceptions) + `</td><td class="number">` + strconv.Itoa(row.MissedControls) + `</td></tr>`
			next := strings.Index(body[at:], want)
			if next < 0 {
				t.Fatalf("%s: the page does not have %q after position %d", name, want, at)
			}
			at += next + len(want)
		}
	}

	plain := page("")
	whole := exceptionbook.DORAReportFor(data, exceptionbook.Options{})
	if len(whole.Rows) == 0 {
		t.Fatal("no DORA or RTS article is reached by the shipped catalog")
	}
	holds("no exercise", plain, whole, url.Values{})
	for _, want := range []string{"<title>DORA Report · GRC</title>", "<h1>DORA Report</h1>", "No scope or exceptions are set", "window.location.replace(window.location.pathname + '?' + storedExercise());"} {
		if !strings.Contains(plain, want) {
			t.Errorf("/reports/dora is missing %q", want)
		}
	}

	keys := []string{data.NFRs[0].Key, data.NFRs[1].Key, data.NFRs[2].Key}
	body := page("?scope=" + keys[0] + "&scope=" + keys[1] + "&key=" + keys[0] + "&key=" + keys[2] + "&partial=" + keys[0])
	holds("an exercise", body, exceptionbook.DORAReportFor(data, exceptionbook.Options{
		Scope: []string{keys[0], keys[1]}, Scoped: true, Selected: []string{keys[0], keys[2]}, Partial: []string{keys[0]},
	}), url.Values{"scope": {keys[0], keys[1]}, "key": {keys[0], keys[2]}, "partial": {keys[0]}})
	if strings.Contains(body, "No scope or exceptions are set") {
		t.Error("a page with an exercise says that nothing is set")
	}
}

// A figure of the DORA Report leads to a page listing exactly what it counts, for
// the same exercise, and the way back keeps the exercise too.
func TestDORADetailPageListsWhatAFigureCounts(t *testing.T) {
	router, data := newDORATestRouter(t)
	get := func(target string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Code, rec.Body.String()
	}

	keys := []string{data.NFRs[0].Key, data.NFRs[1].Key}
	opts := exceptionbook.Options{Selected: keys, Partial: keys[:1]}
	exercise := url.Values{"key": keys, "partial": keys[:1]}
	report := exceptionbook.DORAReportFor(data, opts)
	if len(report.Rows) == 0 {
		t.Fatal("no rows")
	}
	// exceptionbook's test holds every row to its detail; a few are enough here.
	for _, row := range report.Rows[:min(3, len(report.Rows))] {
		detail, ok := exceptionbook.DORADetailFor(data, opts, row.Regulation, row.Reference)
		if !ok {
			t.Fatalf("%s %s has no detail", row.Regulation, row.Reference)
		}
		for show, want := range map[string]int{doraShowNFRs: row.NFRsInScope, doraShowControls: row.ControlsInScope} {
			target := html.UnescapeString(doraDetailHref(row, show, exercise))
			code, body := get(target)
			if code != http.StatusOK {
				t.Fatalf("%s: status %d", target, code)
			}
			if got := strings.Count(body, "<tr><td"); got != want {
				t.Errorf("%s lists %d rows, the figure is %d", target, got, want)
			}
			if !strings.Contains(body, `class="tab active" href="/reports/dora"`) {
				t.Errorf("%s does not have the navigation with DORA active", target)
			}
			if !strings.Contains(body, html.EscapeString("/reports/dora?"+exercise.Encode())) {
				t.Errorf("%s: the way back loses the exercise", target)
			}
		}
		_, body := get(html.UnescapeString(doraDetailHref(row, doraShowNFRs, exercise)))
		for _, nfr := range detail.NFRs {
			if !strings.Contains(body, `/security-nfrs/detail/`+url.PathEscape(nfr.Key)+`"`) {
				t.Errorf("%s %s does not list NFR %s", row.Regulation, row.Reference, nfr.Key)
			}
		}
	}

	for _, target := range []string{
		"/reports/dora/detail",
		"/reports/dora/detail?show=nfrs",
		"/reports/dora/detail?show=nfrs&regulation=DORA&article=Article+99",
		"/reports/dora/detail?show=other&regulation=DORA&article=Article+6",
		"/reports/dora/detail?show=nfrs&regulation=%3Cscript%3E&article=x",
	} {
		code, body := get(target)
		if code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", target, code)
		}
		if strings.Contains(body, "<script>") {
			t.Errorf("%s puts what it was given into the page", target)
		}
		if !strings.Contains(body, "Back to the DORA Report") {
			t.Errorf("%s has no way back", target)
		}
	}
}

// With nothing cited the page says so rather than showing an empty table.
func TestDORATableWithNothingCited(t *testing.T) {
	if got := doraTableHTML(nil, url.Values{}); !strings.Contains(got, "cites DORA or an RTS") || strings.Contains(got, "<table>") {
		t.Errorf("got %q", got)
	}
}

func newDORATestRouter(t *testing.T) (*gin.Engine, exceptionbook.Data) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	conn := sqliteNoopDB(t)
	userHandler, _, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler, err := buildHandlers(conn)
	if err != nil {
		t.Fatalf("buildHandlers: %v", err)
	}
	importBundledCatalogs(t, controlHandler, nfrHandler)
	router := gin.New()
	registerRoutes(router, userHandler, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler,
		noAuthMiddleware, false, true)
	data, err := loadExceptionsWorkbookData(nfrHandler.Service(), linkHandler.Service(), controlHandler.Service())
	if err != nil || len(data.NFRs) < 3 {
		t.Fatalf("no shipped catalog: %v", err)
	}
	return router, data
}
