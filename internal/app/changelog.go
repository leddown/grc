package app

import (
	"bytes"
	"html"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

func changeLogPage(c *gin.Context) {
	path := docPath(changeLogPath)
	// #nosec G304 -- the file name is a package constant; only the directory
	// varies, and it comes from -docs-dir/DOCS_DIR or the fixed search list in
	// docs.go. No request data reaches this path.
	content, err := os.ReadFile(path)
	if err != nil {
		c.Data(http.StatusOK, "text/html; charset=utf-8", renderChangeLogHTML(path, nil, err))
		return
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", renderChangeLogHTML(path, content, nil))
}

func renderChangeLogHTML(path string, content []byte, readErr error) []byte {
	var body bytes.Buffer
	body.WriteString(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Change Log · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --panel: rgba(255,252,246,0.92);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: Georgia, "Times New Roman", serif;
      color: var(--ink);
      background:
        radial-gradient(circle at top left, rgba(139,61,46,0.12), transparent 30%),
        radial-gradient(circle at bottom right, rgba(11,93,59,0.12), transparent 28%),
        linear-gradient(135deg, #f7f3eb, #ece4d6 55%, #e4d8c4);
    }
    main {
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
      box-shadow: 0 24px 60px rgba(28,36,49,0.12);
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
    p { color: var(--muted); }
    .card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: rgba(255,255,255,0.84);
      padding: 18px;
      margin-top: 18px;
    }
    .path {
      font-family: "Courier New", monospace;
      color: var(--accent);
      font-size: 14px;
    }
    .error {
      color: var(--accent);
      font-family: Arial, sans-serif;
      font-size: 14px;
      margin-bottom: 14px;
    }
    pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
      font-family: "Courier New", monospace;
      font-size: 14px;
      line-height: 1.5;
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/changelog") + `
    <h1>Change Log</h1>
    <p>Use this file as a local change history when you need to review or revert recent updates.</p>
    <section class="card">
      <div class="path">File: ` + html.EscapeString(path) + `</div>`)

	if readErr != nil {
		body.WriteString(`<div class="error">Failed to read change log: ` + html.EscapeString(readErr.Error()) +
			`<br>Set DOCS_DIR (or -docs-dir) to the directory holding CHANGELOG.md, or re-run scripts/update.sh to install the documentation alongside the binary.</div>`)
	}

	body.WriteString(`<pre>`)
	if len(content) == 0 {
		body.WriteString(`No change log entries found.`)
	} else {
		body.WriteString(html.EscapeString(string(content)))
	}
	body.WriteString(`</pre>
    </section>
  </main>
</body>
</html>`)

	return body.Bytes()
}
