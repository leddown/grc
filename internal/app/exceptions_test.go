package app

import (
	"strconv"
	"strings"
	"testing"

	"grc/internal/exceptionbook"
	"grc/internal/securitynfr"
)

// Both Exceptions pages read the scope and the partial exceptions from the
// address with one script, and say whether an exception is full or partial.
func TestExceptionsPagesCarryScopeAndPartialExceptions(t *testing.T) {
	router := newNavigationTestRouter(t)

	page := getPageBody(t, router, "/exceptions")
	for _, want := range []string{
		`id="scopeStepBtn"`,
		`id="exceptionsStepBtn"`,
		`class="exception-kind"`,
		"params.append('partial', key)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/exceptions is missing %q", want)
		}
	}

	detail := getPageBody(t, router, "/exceptions/detail")
	if !strings.Contains(detail, "' | Exception: '") {
		t.Error("/exceptions/detail does not say whether an exception is full or partial")
	}

	// Full or Partial beside the checkbox, the same setting as the one in the
	// Selected Security NFRs panel.
	for _, want := range []string{"function rowKindHTML(key, excepted)", `'<select class="row-kind" data-key="'`, "if (inList) inList.value = kind.value;"} {
		if !strings.Contains(page, want) {
			t.Errorf("/exceptions is missing %q", want)
		}
	}
	// The summaries are on the Exceptions Report; the Exceptions page builds
	// the set and does not report on it.
	for _, gone := range []string{`id="ciaWeightSummary"`, `id="ciaSummary"`, `id="threatWeightSummary"`, `id="threatSummary"`, `id="weightSummary"`, "cia-summary", "Total weight (minor)"} {
		if strings.Contains(page, gone) {
			t.Errorf("/exceptions still has %q", gone)
		}
	}
	for _, want := range []string{"<h2>CIA by weight</h2>", "<h2>Threats by weight</h2>", `<div class="card minor"><h2>Total weight (minor)</h2>`} {
		if !strings.Contains(detail, want) {
			t.Errorf("/exceptions/detail is missing %q", want)
		}
	}

	for name, body := range map[string]string{"/exceptions": page, "/exceptions/detail": detail} {
		for _, function := range []string{"function scopeFromAddress(", "function partialFromAddress(", "function computeWeightSummary("} {
			if strings.Count(body, function) != 1 {
				t.Errorf("%s must carry %q once", name, function)
			}
		}
	}
}

// The pages and the workbook weigh a selection the same way: the same factor
// for a partial exception and the same rounding. The workbook holds the
// figures as cells of its Summary sheet; the pages word them in a line.
func TestWeightLineIsWordedAsTheWorkbookWordsIt(t *testing.T) {
	_, result := exceptionbook.Build(exceptionbook.Data{NFRs: []securitynfr.NFR{
		{Key: "a", ID: "1", Weight: 1111},
		{Key: "b", ID: "2", Weight: 1111},
	}}, exceptionbook.Options{Selected: []string{"a", "b"}, Partial: []string{"b"}})
	if result.SelectedWeight != 1666.5 || result.ScopeWeight != 2222 {
		t.Fatalf("the workbook weighs the selection at %v of %d, want 1666.5 of 2222", result.SelectedWeight, result.ScopeWeight)
	}

	script := "'Weight of the selection: ' + summary.selectedWeight + ' of ' + summary.scopeWeight + ' (' + summary.share + '%)'"
	if !strings.Contains(exceptionWeightScript, script) {
		t.Errorf("the pages do not word the weight line: want %s in the script", script)
	}

	for _, want := range []string{
		"const PARTIAL_FACTOR = " + strconv.FormatFloat(exceptionbook.PartialFactor, 'f', -1, 64) + ";",
		"weight * PARTIAL_FACTOR",
		// The same rounding as the workbook: a half rounds up.
		"Math.floor(chosen.total * 100 / all.total + 0.5)",
		"Math.floor(chosen.total * 100 / chosen.count + 0.5) / 100",
	} {
		if !strings.Contains(exceptionWeightScript, want) {
			t.Errorf("the weight script does not work as the workbook does: missing %q", want)
		}
	}
}

// The scope of an exercise can be kept as a local JSON file: every Security
// NFR, marked in scope or not, and no exceptions. Writing and reading it is
// the page's own work: there is no route behind it.
func TestExceptionsPageExportsAndImportsAScopeFile(t *testing.T) {
	router := newNavigationTestRouter(t)
	page := getPageBody(t, router, "/exceptions")
	for _, want := range []string{
		`id="exportScopeBtn"`,
		`id="importScopeBtn"`,
		`id="importScopeFile" type="file" accept=".json,application/json" hidden`,
		"const SCOPE_FILE_FORMAT = 'security-nfr-scope';",
		"const SCOPE_FILE_VERSION = 2;",
		"const SCOPE_FILE_APPLICATION = '" + scopeFileApplication + "';",
		"function readScopeFile(text, catalog)",
		"scopeFileRefusal(files[0].name, files[0].size, files[0].type)",
		"function scopeFileText()",
		"function importScopeText(text)",
		"in_scope: state.scope.has(key)",
		">Select All<",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/exceptions is missing %q", want)
		}
	}
	// The file is a scope and nothing else: it must not carry the exception set.
	text := page[strings.Index(page, "function scopeFileText()"):strings.Index(page, "function exportScope()")]
	for _, unwanted := range []string{"exception", "partial", "state.selected"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("the scope file is written with %q: it must hold no exceptions", unwanted)
		}
	}
	if strings.Contains(page, "fetch('/exceptions") || strings.Contains(page, `method: 'POST'`) {
		t.Error("the scope file must be written and read in the browser, not sent to the application")
	}
}

// The scope and the exceptions set on the Exceptions page are kept in the
// browser for the tab's session, and the Exceptions Workbook page offers a
// workbook with them. The application is not told and stores nothing.
func TestWorkbookPageOffersTheSessionsSelections(t *testing.T) {
	router := newNavigationTestRouter(t)

	page := getPageBody(t, router, "/exceptions")
	for _, want := range []string{
		"const EXERCISE_SESSION_KEY = 'exceptions.exercise';",
		"storeExercise(query);",
		"window.sessionStorage.setItem(EXERCISE_SESSION_KEY, query);",
		"window.history.replaceState(null, '', window.location.pathname + '?' + storedExercise());",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/exceptions is missing %q", want)
		}
	}
	if strings.Contains(page, "localStorage.setItem(EXERCISE_SESSION_KEY") {
		t.Error("the exercise must not outlive the tab's session")
	}

	workbook := getPageBody(t, router, "/reports/exceptions-workbook")
	for _, want := range []string{
		`<a class="download" href="/reports/exceptions-workbook/download">`,
		`<a id="sessionDownload" class="download" href="/reports/exceptions-workbook/download" style="display:none">`,
		"sessionDownload.href = '/reports/exceptions-workbook/download?' + stored;",
		"function storedExercise()",
		"function exerciseOf(items, search)",
	} {
		if !strings.Contains(workbook, want) {
			t.Errorf("/reports/exceptions-workbook is missing %q", want)
		}
	}
}
