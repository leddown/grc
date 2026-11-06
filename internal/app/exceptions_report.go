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

// exceptionsReportPage shows the Summary sheet of the Exceptions Workbook for a scope
// and an exception set, and under it the exceptions, heaviest first, and the
// controls they miss. internal/exceptionbook works the figures out for
// the sheet and for this page alike, so the two cannot differ.
//
// The scope and the set are read from the address as the Exceptions page
// writes them: ?scope=, ?key= and ?partial=. Opened with a plain address, the
// page goes to the exercise of the browser tab if it has one.
func exceptionsReportPage(nfrs *securitynfr.Handler, links *nfrlink.Handler, controls *controlcatalog.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		opts, note := reportExercise(c)

		summary, lists := `<p class="muted">The catalogs could not be read, so there is nothing to report.</p>`, ""
		if data, err := loadExceptionsWorkbookData(nfrs.Service(), links.Service(), controls.Service()); err == nil {
			report := exceptionbook.ReportFor(data, opts)
			summary = summaryReportHTML(report.Result.Summary())
			lists = exceptionsTableHTML(report.Exceptions) + missedControlsHTML(report.Missed)
		}

		page := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Exceptions Report · GRC</title>
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
    ` + pageui.Nav("/reports/exceptions-report") + `
    <h1>Exceptions Report</h1>
    <p class="lead">The Summary sheet of the <a href="/reports/exceptions-workbook">Exceptions Workbook</a>, worked out here:
      the scope and the exceptions, the weight that touches each of confidentiality, integrity and availability and each
      threat, with the missed controls that carry it, and, as a minor figure, the total weight of the selection.
      Under it are the exceptions, heaviest first, and the controls they miss.</p>
    <p class="muted">` + note + `</p>
    <div class="reports">
` + summary + `
    </div>
    <div class="lists">
` + lists + `
    </div>
  </main>
</body>
</html>`
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
	}
}

// reportExercise reads the scope and the exception set from a report page's
// address, and says on what the page's figures rest.
func reportExercise(c *gin.Context) (exceptionbook.Options, string) {
	query := c.Request.URL.Query()
	note := `No scope or exceptions are set: the figures are for the whole catalog with nothing selected. Set them on the <a href="/exceptions">Exceptions</a> page and come back.`
	if query.Has("scope") || query.Has("key") || query.Has("partial") {
		note = `For the scope and the exceptions set on the <a href="/exceptions">Exceptions</a> page, as this page's address carries them.`
	}
	return exceptionbook.Options{
		Selected: c.QueryArray("key"),
		Scope:    c.QueryArray("scope"),
		Scoped:   query.Has("scope"),
		Partial:  c.QueryArray("partial"),
	}, note
}

const reportPageStyle = `    :root {
      color-scheme: light;
      --panel: rgba(255,252,246,0.92);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
    }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: Georgia, "Times New Roman", serif; color: var(--ink); background: linear-gradient(135deg, #f7f3eb, #ece4d6 55%, #e4d8c4); }
    main {
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
    }
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; letter-spacing: -0.03em; }
    p { color: var(--muted); line-height: 1.5; }
    .lead { max-width: 76ch; }
    .muted { color: var(--muted); }
    /* The tables are not all one width: one with four columns of figures
       needs more room than one with a single figure. Each card asks for the
       width its table needs and the row wraps, so no column is pushed past
       the edge of its card at any window width. */
    .reports { display: flex; flex-wrap: wrap; gap: 18px; margin-top: 18px; align-items: flex-start; }
    .reports .card { flex: 1 1 320px; min-width: 0; }
    .reports .card.wide { flex-basis: 540px; }
    .card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      padding: 12px 18px;
    }
    table { width: 100%; border-collapse: collapse; }
    th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid var(--line); vertical-align: top; }
    th { font-family: Arial, sans-serif; font-size: 12px; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); }
    tr:last-child td { border-bottom: 0; }
    .reports td:first-child, .reports th:first-child { width: 40%; }
    .reports .wide td:first-child, .reports .wide th:first-child { width: 30%; }
    /* A heading breaks between its words and not inside one. */
    .reports th { overflow-wrap: normal; word-break: normal; hyphens: none; }
    /* Should a table still be wider than its card, the card scrolls: nothing is cut off. */
    .card { overflow-x: auto; }
    .lists { display: grid; gap: 18px; margin-top: 18px; }
    .lists h2 { margin: 4px 0 8px; font-size: 14px; }
    .lists .number { white-space: nowrap; }
    /* A lesser figure: the total weight, beside what the exceptions touch. */
    .card.minor th, .card.minor td { font-size: 11px !important; color: var(--muted); }
`

// summaryReportHTML lays the sections out as the Summary sheet does: a table
// for each, headed by its title and the headings of its value columns.
func summaryReportHTML(sections []exceptionbook.SummarySection) string {
	var b strings.Builder
	for _, section := range sections {
		columns := len(section.Columns)
		for _, row := range section.Rows {
			columns = max(columns, len(row.Values))
		}
		cell := func(tag string, values []string, i int) {
			text := ""
			if i < len(values) {
				text = values[i]
			}
			b.WriteString("<" + tag + ">" + html.EscapeString(text) + "</" + tag + ">")
		}

		class := "card"
		if section.Minor {
			class = "card minor"
		}
		// A table with several columns of figures asks for a wider card.
		if columns >= 3 {
			class += " wide"
		}
		b.WriteString(`      <section class="` + class + `"><table><thead><tr><th>` + html.EscapeString(section.Title) + `</th>`)
		for i := 0; i < columns; i++ {
			cell("th", section.Columns, i)
		}
		b.WriteString("</tr></thead><tbody>")
		for _, row := range section.Rows {
			b.WriteString("<tr><td>" + html.EscapeString(row.Label) + "</td>")
			for i := 0; i < columns; i++ {
				cell("td", row.Values, i)
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</tbody></table></section>\n")
	}
	return b.String()
}

// exceptionsTableHTML lists the Security NFRs that have exceptions, in the
// order it is given them: heaviest first.
func exceptionsTableHTML(exceptions []exceptionbook.Exception) string {
	var b strings.Builder
	b.WriteString(`      <section class="card"><h2>Exceptions, by weight</h2>`)
	if len(exceptions) == 0 {
		b.WriteString(`<p class="muted">No Security NFR has an exception.</p></section>` + "\n")
		return b.String()
	}
	b.WriteString(`<table><thead><tr><th>NFR ID</th><th>Summary</th><th>Domain</th><th>Weight</th><th>Exception</th></tr></thead><tbody>`)
	for _, exception := range exceptions {
		kind := exceptionbook.FullMark
		if exception.Partial {
			kind = exceptionbook.PartialMark
		}
		b.WriteString(`<tr><td class="number">` + html.EscapeString(orDash(exception.ID)) + `</td>` +
			`<td><a href="/security-nfrs/detail/` + url.PathEscape(exception.Key) + `" target="_blank" rel="noopener noreferrer">` + html.EscapeString(orDash(exception.Summary)) + `</a></td>` +
			`<td>` + html.EscapeString(orDash(exception.Domain)) + `</td>` +
			`<td class="number">` + strconv.Itoa(exception.Weight) + `</td>` +
			`<td>` + kind + `</td></tr>`)
	}
	b.WriteString("</tbody></table></section>\n")
	return b.String()
}

// missedControlsHTML lists the controls the exceptions are linked to,
// each once. A control the catalog does not hold has no page to link to.
func missedControlsHTML(missed []exceptionbook.MissedControl) string {
	var b strings.Builder
	b.WriteString(`      <section class="card"><h2>Missed controls</h2>`)
	if len(missed) == 0 {
		b.WriteString(`<p class="muted">No control is missed.</p></section>` + "\n")
		return b.String()
	}
	b.WriteString(`<table><thead><tr><th>Control ID</th><th>Name</th><th>Family</th><th>C / I / A</th></tr></thead><tbody>`)
	for _, control := range missed {
		id := html.EscapeString(control.ID)
		if control.Listed {
			id = `<a href="/controls/detail/` + url.PathEscape(control.ID) + `" target="_blank" rel="noopener noreferrer">` + id + `</a>`
		}
		b.WriteString(`<tr><td class="number">` + id + `</td>` +
			`<td>` + html.EscapeString(orDash(control.Name)) + `</td>` +
			`<td>` + html.EscapeString(orDash(control.Family)) + `</td>` +
			`<td class="number">` + ratingLetters(control) + `</td></tr>`)
	}
	b.WriteString("</tbody></table></section>\n")
	return b.String()
}

// ratingLetters is only the letters of the ratings a control carries, as on
// the Exceptions page.
func ratingLetters(control exceptionbook.MissedControl) string {
	var ratings []string
	for _, rating := range []struct {
		letter  string
		carried bool
	}{{"C", control.Confidentiality}, {"I", control.Integrity}, {"A", control.Availability}} {
		if rating.carried {
			ratings = append(ratings, rating.letter)
		}
	}
	return strings.Join(ratings, " | ")
}

func orDash(text string) string {
	if strings.TrimSpace(text) == "" {
		return "-"
	}
	return text
}
