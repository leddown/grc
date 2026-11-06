package app

import (
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/controlcatalog"
	"grc/internal/exceptionbook"
	"grc/internal/nfrlink"
	"grc/internal/pageui"
	"grc/internal/securitynfr"
)

// doraReportPage shows the workbook's DORA sheet for a scope and an exception
// set, read from the address as on the Exceptions Report. internal/exceptionbook
// works the figures out for the sheet and for this page alike.
func doraReportPage(nfrs *securitynfr.Handler, links *nfrlink.Handler, controls *controlcatalog.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		opts, note := reportExercise(c)

		exercise := reportExerciseQuery(c)
		summary, table := `<p class="muted">The catalogs could not be read, so there is nothing to report.</p>`, ""
		if data, err := loadExceptionsWorkbookData(nfrs.Service(), links.Service(), controls.Service()); err == nil {
			report := exceptionbook.DORAReportFor(data, opts)
			summary = summaryReportHTML([]exceptionbook.SummarySection{report.Summary()})
			table = doraTableHTML(report.Rows, exercise)
		}

		page := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>DORA Report · GRC</title>
  <style>
` + reportPageStyle + `  </style>
</head>
<body>
  <script>
` + exceptionScopeScript + `
    // A plain address goes to the exercise of this browser tab, if it has one.
    if (!window.location.search && storedExercise()) {
      window.location.replace(window.location.pathname + '?' + storedExercise());
    }
  </script>
  <main>
    ` + pageui.Nav("/reports/dora") + `
    <h1>DORA Report</h1>
    <p class="lead">The DORA and RTS articles that the controls linked to Security NFRs in scope cite in their
      References, and the exceptions against each. An NFR counts against an article when a control it is linked to
      cites it. One NFR can count against several articles, so the Exceptions column does not add up to the number of
      exceptions. A partial exception counts as one. Click a figure under NFRs in scope or Controls in scope to see
      what it counts. The DORA sheet of the <a href="/reports/exceptions-workbook">Exceptions Workbook</a> shows the same
      figures.</p>
    <p class="muted">` + note + `</p>
    <div class="reports">
` + summary + `
    </div>
    <div class="lists">
` + table + `
    </div>
  </main>
</body>
</html>`
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
	}
}

// reportExerciseQuery is the scope and the exception set of the address, for a
// link that has to keep them.
func reportExerciseQuery(c *gin.Context) url.Values {
	query := c.Request.URL.Query()
	exercise := url.Values{}
	for _, name := range []string{"scope", "key", "partial"} {
		if query.Has(name) {
			exercise[name] = query[name]
		}
	}
	return exercise
}

// doraDetailHref is the page behind a figure of a row: show is doraShowNFRs or
// doraShowControls. It is escaped for an attribute.
func doraDetailHref(row exceptionbook.DORARow, show string, exercise url.Values) string {
	query := url.Values{}
	for name, values := range exercise {
		query[name] = values
	}
	query.Set("regulation", row.Regulation)
	query.Set("article", row.Reference)
	query.Set("show", show)
	return html.EscapeString("/reports/dora/detail?" + query.Encode())
}

func doraTableHTML(rows []exceptionbook.DORARow, exercise url.Values) string {
	var b strings.Builder
	b.WriteString(`      <section class="card"><h2>Articles and the exceptions against them</h2>`)
	if len(rows) == 0 {
		b.WriteString(`<p class="muted">No control linked to an NFR in scope cites DORA or an RTS.</p></section>` + "\n")
		return b.String()
	}
	b.WriteString(`<table><thead><tr><th>Regulation</th><th>Article</th><th>Title</th><th>NFRs in scope</th><th>Controls in scope</th><th>Exceptions</th><th>Missed Controls</th></tr></thead><tbody>`)
	for _, row := range rows {
		b.WriteString(`<tr><td>` + html.EscapeString(row.Regulation) + `</td>` +
			`<td class="number">` + html.EscapeString(orDash(row.Reference)) + `</td>` +
			`<td>` + html.EscapeString(orDash(row.Title)) + `</td>` +
			`<td class="number"><a href="` + doraDetailHref(row, doraShowNFRs, exercise) + `">` + strconv.Itoa(row.NFRsInScope) + `</a></td>` +
			`<td class="number"><a href="` + doraDetailHref(row, doraShowControls, exercise) + `">` + strconv.Itoa(row.ControlsInScope) + `</a></td>` +
			`<td class="number">` + strconv.Itoa(row.Exceptions) + `</td>` +
			`<td class="number">` + strconv.Itoa(row.MissedControls) + `</td></tr>`)
	}
	b.WriteString("</tbody></table></section>\n")
	return b.String()
}

const (
	doraShowNFRs     = "nfrs"
	doraShowControls = "controls"
)

// doraDetailPage lists what stands behind one figure of the DORA Report, for the
// same scope and exceptions: the Security NFRs in scope, or the controls.
func doraDetailPage(nfrs *securitynfr.Handler, links *nfrlink.Handler, controls *controlcatalog.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		opts, _ := reportExercise(c)
		regulation, reference, show := c.Query("regulation"), c.Query("article"), c.Query("show")

		back := "/reports/dora"
		if exercise := reportExerciseQuery(c).Encode(); exercise != "" {
			back += "?" + exercise
		}

		status := http.StatusOK
		heading := "DORA Report"
		lead, table := "", ""
		data, err := loadExceptionsWorkbookData(nfrs.Service(), links.Service(), controls.Service())
		switch {
		case err != nil:
			status = http.StatusInternalServerError
			lead = "The catalogs could not be read, so there is nothing to list."
		case show != doraShowNFRs && show != doraShowControls:
			status = http.StatusNotFound
			lead = "Choose a figure on the DORA Report to see what it counts."
		default:
			detail, ok := exceptionbook.DORADetailFor(data, opts, regulation, reference)
			if !ok {
				status = http.StatusNotFound
				lead = "No Security NFR in this scope reaches a control that cites this."
				break
			}
			heading = strings.TrimSpace(detail.Row.Regulation + " " + detail.Row.Reference)
			title := ""
			if detail.Row.Title != "" {
				title = detail.Row.Title + ". "
			}
			if show == doraShowNFRs {
				lead = title + strconv.Itoa(detail.Row.NFRsInScope) + " Security NFRs in scope are linked to a control that cites this; " +
					strconv.Itoa(detail.Row.Exceptions) + " of them are exceptions."
				table = doraNFRsHTML(detail.NFRs)
			} else {
				lead = title + strconv.Itoa(detail.Row.ControlsInScope) + " controls cite this and are linked to a Security NFR in scope; " +
					strconv.Itoa(detail.Row.MissedControls) + " of them are missed."
				table = doraControlsHTML(detail.Controls)
			}
		}

		page := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>` + html.EscapeString(heading) + ` · GRC</title>
  <style>
` + reportPageStyle + `  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/reports/dora") + `
    <h1>` + html.EscapeString(heading) + `</h1>
    <p class="lead">` + html.EscapeString(lead) + `</p>
    <p><a href="` + html.EscapeString(back) + `">Back to the DORA Report</a></p>
    <div class="lists">
` + table + `
    </div>
  </main>
</body>
</html>`
		c.Data(status, "text/html; charset=utf-8", []byte(page))
	}
}

func doraNFRsHTML(rows []exceptionbook.DORANFR) string {
	var b strings.Builder
	b.WriteString(`      <section class="card"><h2>Security NFRs in scope</h2>`)
	b.WriteString(`<table><thead><tr><th>NFR ID</th><th>Summary</th><th>Domain</th><th>Weight</th><th>Exception</th></tr></thead><tbody>`)
	for _, nfr := range rows {
		kind := "-"
		if nfr.Excepted {
			kind = exceptionbook.FullMark
			if nfr.Partial {
				kind = exceptionbook.PartialMark
			}
		}
		b.WriteString(`<tr><td class="number">` + html.EscapeString(orDash(nfr.ID)) + `</td>` +
			`<td><a href="/security-nfrs/detail/` + url.PathEscape(nfr.Key) + `" target="_blank" rel="noopener noreferrer">` + html.EscapeString(orDash(nfr.Summary)) + `</a></td>` +
			`<td>` + html.EscapeString(orDash(nfr.Domain)) + `</td>` +
			`<td class="number">` + strconv.Itoa(nfr.Weight) + `</td>` +
			`<td>` + kind + `</td></tr>`)
	}
	b.WriteString("</tbody></table></section>\n")
	return b.String()
}

func doraControlsHTML(rows []exceptionbook.DORAControl) string {
	var b strings.Builder
	b.WriteString(`      <section class="card"><h2>NIST SP 800-53 controls in scope</h2>`)
	b.WriteString(`<table><thead><tr><th>Control ID</th><th>Name</th><th>Family</th><th>C / I / A</th><th>Missed</th></tr></thead><tbody>`)
	for _, control := range rows {
		missed := "-"
		if control.Missed {
			missed = "Yes"
		}
		b.WriteString(`<tr><td class="number"><a href="/controls/detail/` + url.PathEscape(control.ID) + `" target="_blank" rel="noopener noreferrer">` + html.EscapeString(control.ID) + `</a></td>` +
			`<td>` + html.EscapeString(orDash(control.Name)) + `</td>` +
			`<td>` + html.EscapeString(orDash(control.Family)) + `</td>` +
			`<td class="number">` + ratingLetters(control.MissedControl) + `</td>` +
			`<td>` + missed + `</td></tr>`)
	}
	b.WriteString("</tbody></table></section>\n")
	return b.String()
}
