package app

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"grc/internal/controlcatalog"
	"grc/internal/exceptionbook"
	"grc/internal/nfrlink"
	"grc/internal/pageui"
	"grc/internal/securitynfr"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// What is said of the partial exceptions comes in the body of a POST for the
// workbook, as a form with one field for each: the prefix and the NFR's key
// are the field's name. The page's script has the same length as
// DESCRIPTION_MAX.
const (
	descriptionField    = "description."
	descriptionMaxRunes = 1000
	descriptionMaxKey   = 256
	descriptionMaxBody  = 2 << 20
)

func exceptionsWorkbookPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Exceptions Workbook · GRC</title>
  <style>
    :root {
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
    .tabs { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 20px; }
    .tab {
      padding: 10px 14px;
      border-radius: 999px;
      background: #efe6d6;
      border: 1px solid var(--line);
      color: var(--ink);
      text-decoration: none;
      font-family: Arial, sans-serif;
      font-size: 13px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    .tab.active { background: #e1d0b7; border-color: #b89d78; }
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; letter-spacing: -0.03em; }
    p, li { color: var(--muted); line-height: 1.5; }
    .lead { max-width: 76ch; }
    .card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      padding: 18px;
      margin-top: 18px;
    }
    .card h2 { margin: 0 0 6px; font-size: 14px; }
    .actions { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin-top: 18px; }
    /* A link, so the theme's button rules do not reach it: it takes the
       theme's own tokens for the one action this page is for. */
    main a.download {
      display: inline-block;
      padding: 10px 16px;
      border-radius: 8px;
      background: var(--accent, #8b3d2e) !important;
      color: var(--on-accent, #ffffff) !important;
      text-decoration: none !important;
      font-family: Arial, sans-serif;
      font-weight: 600;
    }
    .muted { color: var(--muted); }
    table { width: 100%; border-collapse: collapse; }
    th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--line); vertical-align: top; }
    th { font-family: Arial, sans-serif; font-size: 12px; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); }
    td:first-child { white-space: nowrap; font-weight: 700; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/reports/exceptions-workbook") + `
    <h1>Exceptions Workbook</h1>
    <p class="lead">The <a href="/exceptions">Exceptions</a> page as an Excel workbook that works on its own. It
      holds the Security NFR catalog with its weights, the NIST controls and the links between them, and its
      formulas redo what the page does: mark the requirements in the scope of an exercise and the ones of them in the exception set, and the linked controls, the
      confidentiality, integrity and availability counts, the threat coverage and the detail view follow.</p>
    <div class="actions">
      <a class="download" href="/reports/exceptions-workbook/download">Download the workbook (.xlsx)</a>
      <span id="counts" class="muted"></span>
    </div>
    <div class="actions">
      <a id="sessionDownload" class="download" href="/reports/exceptions-workbook/download" style="display:none">Download with this session's scope and exceptions (.xlsx)</a>
      <span id="sessionCounts" class="muted"></span>
    </div>

    <section class="card">
      <h2>What is in it</h2>
      <table>
        <thead><tr><th>Sheet</th><th>Holds</th></tr></thead>
        <tbody>
          <tr><td>How to use</td><td>The steps, and where each button of the Exceptions page is in the workbook.</td></tr>
          <tr><td>Exceptions</td><td>Every Security NFR with its weight. Choose Yes in the Scope column (A) for the NFRs the exercise covers, and Full or Partial in the Exception column (B) to put one of them in the exception set; a partial exception counts half its weight. The summary lines at the top follow.</td></tr>
          <tr><td>Summary</td><td>The scope, the selection, the linked controls, the C / I / A counts, the threat coverage, and the weight of the selection against the scope.</td></tr>
          <tr><td>Selected NFRs</td><td>The detail view: each selected requirement in full.</td></tr>
          <tr><td>Selected controls</td><td>The detail view: each selected requirement's linked controls, with their ratings and threats.</td></tr>
          <tr><td>Links</td><td>Which NIST control each requirement's NIST mapping resolves to.</td></tr>
          <tr><td>Controls</td><td>The NIST controls, their ratings and the threats each addresses.</td></tr>
        </tbody>
      </table>
    </section>

    <section class="card">
      <h2>Good to know</h2>
      <ul>
        <li>The file is .xlsx, for Excel 2010 or later and LibreOffice Calc. It has no macros.</li>
        <li>It is a snapshot of the catalogs as they are now. Download it again after the catalogs change.</li>
        <li>Every figure in it is one the <a href="/reports/exceptions-report">Exceptions Report</a> shows, worked out the same way.</li>
        <li>The first download has nothing selected in it: the Scope and Exception columns are empty, and that is where to start. It is otherwise the same workbook as the second.</li>
        <li>The second download opens with the scope and the exceptions set on the Exceptions page in this browser tab. They are kept in the browser for the tab's session; the application does not store them.</li>
        <li>What was typed there of a partial exception is in the Comments column of that NFR's row. It is sent with the request for the workbook, in its body and not in its address, and the application does not keep it.</li>
        <li>Excel opens a downloaded file in Protected View. The numbers are already filled in; choose Enable Editing to change the selection.</li>
      </ul>
    </section>
  </main>
  <script>
` + exceptionScopeScript + `
    (async () => {
      const counts = document.getElementById('counts');
      const sessionDownload = document.getElementById('sessionDownload');
      const sessionCounts = document.getElementById('sessionCounts');
      // What was set on the Exceptions page in this browser tab, if anything.
      const stored = storedExercise();
      if (stored) {
        sessionDownload.href = '/reports/exceptions-workbook/download?' + stored;
        sessionDownload.style.display = '';
        // What is said of the partial exceptions goes in the body of the
        // request and not in its address, and only when there is something
        // to send. The text areas are where a form keeps a line break.
        sessionDownload.addEventListener('click', (event) => {
          const sent = descriptionsToSend(storedDescriptions(), partialFromAddress(stored));
          if (!sent.length) return;
          event.preventDefault();
          const form = document.createElement('form');
          form.method = 'post';
          form.action = sessionDownload.href;
          form.hidden = true;
          sent.forEach(([key, text]) => {
            const field = document.createElement('textarea');
            field.name = 'description.' + key;
            field.value = text;
            form.appendChild(field);
          });
          document.body.appendChild(form);
          form.submit();
          form.remove();
        });
      } else {
        sessionCounts.textContent = 'No scope or exceptions are set on the Exceptions page in this browser tab. The workbook above is the same one with nothing selected in it: no scope and no exceptions.';
      }
      try {
        const [nfrs, controls] = await Promise.all([
          fetch('/security-nfrs/data').then((r) => r.ok ? r.json() : Promise.reject(r.status)),
          fetch('/controls/data?slim=1').then((r) => r.ok ? r.json() : Promise.reject(r.status))
        ]);
        counts.textContent = nfrs.length + ' Security NFRs, ' + controls.length + ' NIST controls';
        if (stored) {
          const exercise = exerciseOf(nfrs, stored);
          sessionCounts.textContent = scopeLine(exercise.scope, nfrs) + ' | ' +
            exercise.selected.size + (exercise.selected.size === 1 ? ' exception' : ' exceptions') +
            (exercise.partial.size ? ' (' + exercise.partial.size + ' partial, ' + descriptionsToSend(storedDescriptions(), exercise.partial).length + ' described)' : '') +
            ', as set on the Exceptions page in this browser tab';
        }
      } catch (_) {
        counts.textContent = '';
      }
    })();
  </script>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// exceptionsWorkbookDownload reads what the Exceptions page reads -- the same
// three lists, with the same filters -- so the workbook and the page start
// from the same data. ?scope= names the requirements in the scope of the
// exercise and ?key= the ones of them in the exception set, as the Exceptions
// page's own address does: no scope parameter means the whole catalog is in
// scope. ?partial= names the selected ones whose exception is partial. With
// none of the three the workbook is the same one with nothing selected in it.
//
// A POST is the same request with what is said of the partial exceptions in
// its body; see exceptionDescriptions. Nothing of it is stored.
func exceptionsWorkbookDownload(nfrs *securitynfr.Handler, links *nfrlink.Handler, controls *controlcatalog.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		data, err := loadExceptionsWorkbookData(nfrs.Service(), links.Service(), controls.Service())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build the exceptions workbook"})
			return
		}

		// Downloaded with no selections at all, the workbook has none: no
		// requirement in scope and none excepted, the same workbook otherwise.
		// With selections it is read as the Exceptions page's address is, where
		// no scope parameter means the whole catalog.
		query := c.Request.URL.Query()
		scoped := query.Has("scope") || !(query.Has("key") || query.Has("partial"))

		descriptions, err := exceptionDescriptions(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		now := time.Now()
		book, _ := exceptionbook.Build(data, exceptionbook.Options{
			Selected:     c.QueryArray("key"),
			Scope:        c.QueryArray("scope"),
			Scoped:       scoped,
			Partial:      c.QueryArray("partial"),
			Descriptions: descriptions,
			GeneratedAt:  now,
		})
		var file bytes.Buffer
		if err := book.Write(&file); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build the exceptions workbook"})
			return
		}
		c.Header("Content-Disposition", `attachment; filename="grc-exceptions-workbook-`+now.Format("2006-01-02")+`.xlsx"`)
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, xlsxContentType, file.Bytes())
	}
}

// exceptionDescriptions reads what is said of the partial exceptions from the
// body of a POST: a form of description.<key> fields and nothing else. A body
// that is anything else is refused whole, as is one with a description that
// is not text or is longer than the page lets one be. A description for a key
// that is not a partial exception of the request is not an error: the
// workbook leaves it out, as it leaves out a key the catalog does not have.
func exceptionDescriptions(c *gin.Context) (map[string]string, error) {
	if c.Request.Method != http.MethodPost {
		return nil, nil
	}
	if c.ContentType() != "application/x-www-form-urlencoded" {
		return nil, errors.New("the descriptions are not sent as a form")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, descriptionMaxBody)
	if err := c.Request.ParseForm(); err != nil {
		return nil, errors.New("the descriptions could not be read")
	}
	descriptions := map[string]string{}
	for name, values := range c.Request.PostForm {
		key, found := strings.CutPrefix(name, descriptionField)
		if !found || len(values) != 1 || !validDescriptionKey(key) {
			return nil, errors.New("the request has a field that is not a description of a partial exception")
		}
		text, ok := cleanDescription(values[0])
		if !ok {
			return nil, fmt.Errorf("a description is not text of at most %d characters", descriptionMaxRunes)
		}
		if text != "" {
			descriptions[key] = text
		}
	}
	return descriptions, nil
}

func validDescriptionKey(key string) bool {
	if key == "" || len(key) > descriptionMaxKey || key != strings.TrimSpace(key) || !utf8.ValidString(key) {
		return false
	}
	return !strings.ContainsFunc(key, unicode.IsControl)
}

// cleanDescription is text as a cell of the workbook holds it: line ends as
// one character, no other control characters, no space around it. It is false
// for text that is not UTF-8 or is longer than a description can be.
func cleanDescription(text string) (string, bool) {
	if !utf8.ValidString(text) {
		return "", false
	}
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, text))
	if utf8.RuneCountInString(text) > descriptionMaxRunes {
		return "", false
	}
	return text, true
}

func loadExceptionsWorkbookData(nfrs *securitynfr.Service, links *nfrlink.Service, controls *controlcatalog.Service) (exceptionbook.Data, error) {
	if nfrs == nil || links == nil || controls == nil {
		return exceptionbook.Data{}, fmt.Errorf("the catalog services are not available")
	}
	var data exceptionbook.Data
	var err error
	if data.NFRs, err = nfrs.List("", ""); err != nil {
		return exceptionbook.Data{}, fmt.Errorf("security NFRs: %w", err)
	}
	if data.Links, err = links.List("", false); err != nil {
		return exceptionbook.Data{}, fmt.Errorf("NFR-control links: %w", err)
	}
	if data.Controls, err = controls.List("", "", ""); err != nil {
		return exceptionbook.Data{}, fmt.Errorf("controls: %w", err)
	}
	return data, nil
}
