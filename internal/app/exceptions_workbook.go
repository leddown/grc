package app

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/controlcatalog"
	"grc/internal/exceptionbook"
	"grc/internal/nfrlink"
	"grc/internal/pageui"
	"grc/internal/securitynfr"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

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
      formulas redo what the page does: mark the requirements in an exception set and the linked controls, the
      confidentiality, integrity and availability counts, the threat coverage and the detail view follow.</p>
    <div class="actions">
      <a class="download" href="/reports/exceptions-workbook/download">Download the workbook (.xlsx)</a>
      <span id="counts" class="muted"></span>
    </div>

    <section class="card">
      <h2>What is in it</h2>
      <table>
        <thead><tr><th>Sheet</th><th>Holds</th></tr></thead>
        <tbody>
          <tr><td>How to use</td><td>The steps, and where each button of the Exceptions page is in the workbook.</td></tr>
          <tr><td>Exceptions</td><td>Every Security NFR with its weight. Choose Yes in the Except column to put one in the exception set; the summary lines at the top follow.</td></tr>
          <tr><td>Summary</td><td>The selection, the linked controls, the C / I / A counts, the threat coverage, and the weight of the selection against the whole catalog.</td></tr>
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
        <li>The weights of the selection are worked out in the workbook only; the Exceptions page does not show them.</li>
        <li>Excel opens a downloaded file in Protected View. The numbers are already filled in; choose Enable Editing to change the selection.</li>
      </ul>
    </section>
  </main>
  <script>
    (async () => {
      const counts = document.getElementById('counts');
      try {
        const [nfrs, controls] = await Promise.all([
          fetch('/security-nfrs/data').then((r) => r.ok ? r.json() : Promise.reject(r.status)),
          fetch('/controls/data?slim=1').then((r) => r.ok ? r.json() : Promise.reject(r.status))
        ]);
        counts.textContent = nfrs.length + ' Security NFRs, ' + controls.length + ' NIST controls';
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
// from the same data. ?key= selects requirements in the workbook as it opens.
func exceptionsWorkbookDownload(nfrs *securitynfr.Handler, links *nfrlink.Handler, controls *controlcatalog.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		data, err := loadExceptionsWorkbookData(nfrs.Service(), links.Service(), controls.Service())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build the exceptions workbook"})
			return
		}

		now := time.Now()
		book, _ := exceptionbook.Build(data, exceptionbook.Options{
			Selected:    c.QueryArray("key"),
			GeneratedAt: now,
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
