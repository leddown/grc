package app

import (
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"carelockconsulting/internal/pageui"
)

func homePage(c *gin.Context) {
	scheme := "http"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if c.Request.TLS != nil {
		scheme = "https"
	}

	host := c.Request.Host
	if host == "" {
		host = "localhost:8080"
	}

	// scheme/host are reflected verbatim from request headers below, so they
	// must be HTML-escaped before being spliced into href attributes and link
	// text to avoid a reflected HTML/script injection via a crafted Host header.
	baseURL := html.EscapeString(fmt.Sprintf("%s://%s", scheme, host))

	authLinks := ""
	if !localModeEnabled {
		authLinks = `<li><a href="/login">/login</a> (sign in)</li>`
	}

	html := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Database UI · CareLock Consulting</title>
  <style>
    :root {
      color-scheme: light;
      --panel: rgba(42,42,42,0.94);
      --ink: #f2eee6;
      --muted: #d0c7b8;
      --line: #5a544c;
    }
    body {
      margin: 0;
      font-family: Georgia, "Times New Roman", serif;
      background:
        radial-gradient(circle at top left, rgba(139,61,46,0.12), transparent 30%%),
        radial-gradient(circle at bottom right, rgba(11,93,59,0.12), transparent 28%%),
        linear-gradient(135deg, #f7f3eb, #ece4d6 55%%, #e4d8c4);
      color: var(--ink);
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
    h1 { margin-top: 0; font-size: clamp(2rem, 4vw, 3.5rem); line-height: 0.95; letter-spacing: -0.03em; }
    p { color: var(--muted); }
    .grid { display: grid; grid-template-columns: repeat(auto-fit,minmax(280px,1fr)); gap: 18px; }
    .card {
      border: 1px solid #444;
      border-radius: 18px;
      padding: 16px;
      background: #1f1f1f;
    }
    .card h2 {
      margin-top: 0;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: var(--muted);
      font-family: Arial, sans-serif;
    }
    a { color: #dfc4b4; text-decoration: none; }
    a:hover { text-decoration: underline; }
    code { background: #000; padding: 2px 6px; border-radius: 6px; }
    ul { margin: 8px 0 0; padding-left: 18px; }
  </style>
</head>
<body>
  <main>
    %[3]s
    <h1>CareLock Consulting SQLite UI</h1>
    <p>Internal pages for the SQLite-backed control catalog seeded from %[2]s, plus supporting APIs.</p>

    <div class="grid">
      <section class="card">
        <h2>Database Views</h2>
        <ul>
          <li><a href="/">/ (Homepage)</a></li>
	  %[4]s
          <li><a href="/health">/health</a></li>
          <li><a href="/users">/users</a></li>
	          <li><a href="/asset-types">/asset-types</a> (asset type selector)</li>
	          <li><a href="/controls">/controls</a> (unified SQLite control database)</li>
	          <li><a href="/controls/manage">/controls/manage</a> (edit SQLite and rewrite JSON)</li>
	          <li><a href="/security-nfrs">/security-nfrs</a> (SQLite security NFR catalog)</li>
	          <li><a href="/security-nfrs/manage">/security-nfrs/manage</a> (edit security NFRs and rewrite JSON)</li>
	          <li><a href="/JSON_view">/JSON_view</a> (single output panel for Security NFR JSON and Family JSON)</li>
	          <li><a href="/jira/json">/jira/json</a> (connect to Jira API and inspect project/issue/search JSON)</li>
	          <li><a href="/jira/reports">/jira/reports</a> (read-only report/search presets for Jira)</li>
	          <li><a href="/security-nfrs/links">/security-nfrs/links</a> (linking table: NFR NIST mapping to control IDs)</li>
	          <li><a href="/reports">/reports</a> (reporting dashboard)</li>
	          <li><a href="/risk-register">/risk-register</a> (security risk register dashboard)</li>
	          <li><a href="/risk-register/manage">/risk-register/manage</a> (risk register editor with save)</li>




	          <li><a href="/changelog">/changelog</a> (view repository change history for rollback reference)</li>
	          <li><a href="/controls/family-visibility">/controls/family-visibility</a> (toggle family visibility in filters)</li>
	          <li><a href="/controls/hierarchy">/controls/hierarchy</a> (family → control → enhancement)</li>
	          <li><a href="/exceptions">/exceptions</a> (select multiple Security NFRs for exception sets)</li>
	          <li><a href="/ai-chat">/ai-chat</a> (chat with the Anthropic Claude API or a self-hosted Wintermute server)</li>
	          <li><a href="/wiz-rules">/wiz-rules</a> (Wiz-style rule creation interface)</li>
	          <li><a href="/docs">/docs</a> (OpenAPI JSON and in-app API reference)</li>
	          <li><a href="/utilities">/utilities</a> (export full data set / import to overwrite database)</li>
        </ul>
      </section>

      <section class="card">
        <h2>JSON APIs</h2>
        <ul>
          <li><a href="%[1]s/health">%[1]s/health</a></li>
          <li><a href="%[1]s/users">%[1]s/users</a></li>
          <li><code>%[1]s/users/1</code> (example user details)</li>
	          <li><a href="%[1]s/asset-types">%[1]s/asset-types</a></li>
	          <li><a href="%[1]s/controls">%[1]s/controls</a></li>
	          <li><a href="%[1]s/controls/manage">%[1]s/controls/manage</a></li>
	          <li><a href="%[1]s/security-nfrs">%[1]s/security-nfrs</a></li>
	          <li><a href="%[1]s/security-nfrs/manage">%[1]s/security-nfrs/manage</a></li>
	          <li><a href="%[1]s/JSON_view">%[1]s/JSON_view</a></li>
	          <li><a href="%[1]s/jira/json">%[1]s/jira/json</a></li>
	          <li><a href="%[1]s/jira/reports">%[1]s/jira/reports</a></li>
	          <li><a href="%[1]s/security-nfrs/links">%[1]s/security-nfrs/links</a></li>
	          <li><a href="%[1]s/reports">%[1]s/reports</a></li>
	          <li><a href="%[1]s/risk-register">%[1]s/risk-register</a></li>
	          <li><a href="%[1]s/risk-register/data">%[1]s/risk-register/data</a></li>
	          <li><a href="%[1]s/changelog">%[1]s/changelog</a></li>
	          <li><a href="%[1]s/controls/family-visibility">%[1]s/controls/family-visibility</a></li>
	          <li><a href="%[1]s/controls/hierarchy">%[1]s/controls/hierarchy</a></li>
	          <li><a href="%[1]s/exceptions">%[1]s/exceptions</a></li>
	          <li><a href="%[1]s/ai-chat">%[1]s/ai-chat</a></li>
	          <li><a href="%[1]s/wiz-rules">%[1]s/wiz-rules</a></li>
	          <li><a href="%[1]s/openapi.json">%[1]s/openapi.json</a></li>
	          <li><code>POST %[1]s/ai-chat/ask</code> (proxy question/response request)</li>
	          <li><a href="%[1]s/controls/family-json/data?family=AC">%[1]s/controls/family-json/data?family=AC</a></li>
	          <li><a href="%[1]s/controls/data">%[1]s/controls/data</a> with <code>?search=AC&baseline=High&cia=c</code></li>
          <li><code>PUT %[1]s/controls/:controlID</code> (update SQLite row)</li>
          <li><code>DELETE %[1]s/controls/:controlID</code> (delete SQLite row)</li>
          <li><code>POST %[1]s/controls/save</code> (rewrite flat JSON from SQLite)</li>
          <li><a href="%[1]s/security-nfrs/data">%[1]s/security-nfrs/data</a></li>
	          <li><a href="%[1]s/security-nfrs/json/data">%[1]s/security-nfrs/json/data</a></li>
	          <li><code>POST %[1]s/jira/json/data</code> (Jira project/issue/search snapshot JSON)</li>
	          <li><code>POST %[1]s/jira/json/export</code> (write static Jira snapshot JSON file)</li>
	          <li><a href="%[1]s/jira/reports/presets">%[1]s/jira/reports/presets</a></li>
	          <li><code>POST %[1]s/jira/reports/data</code> (run read-only Jira report/search preset)</li>
	          <li><a href="%[1]s/security-nfrs/links/data">%[1]s/security-nfrs/links/data</a></li>
          <li><a href="%[1]s/reports/data/unlinked-controls">%[1]s/reports/data/unlinked-controls</a></li>
          <li><code>PUT %[1]s/security-nfrs/:key</code> (update SQLite row)</li>
          <li><code>DELETE %[1]s/security-nfrs/:key</code> (delete SQLite row)</li>
          <li><code>POST %[1]s/security-nfrs/save</code> (rewrite NFR JSON from SQLite)</li>
        </ul>
      </section>

      <section class="card">
        <h2>Knowledge & How-To Files</h2>
        <ul>
          <li><a href="/knowledge/agents">Agents.md</a> (agent behavior and policy)</li>
          <li><a href="/knowledge/changelog">CHANGELOG.md</a> (change history and rollback reference)</li>
          <li><a href="/knowledge/runtime">RUNTIME_ARGS.md</a> (runtime and operational how-to guide)</li>
          <li><a href="/knowledge/faq">FAQ.md</a> (security branch merge FAQ)</li>
          <li><a href="/knowledge/jira">JIRA_CONNECTOR.md</a> (Atlassian Jira connector design and integration guide)</li>
          <li><a href="/knowledge/risk-register">RISK_REGISTER_FRAMEWORK.md</a> (risk register structure and best practices)</li>
        </ul>
      </section>
    </div>
  </main>
  <footer style="max-width:1320px;margin:0 auto 24px;padding:16px 24px;color:#6b6155;border-top:1px solid rgba(90,84,76,0.25);font-size:0.85rem;text-align:center;">
    &copy; %[5]s CareLock Consulting &middot; Risk &amp; Control Self-Assessment platform
  </footer>
</body>
</html>`, baseURL, controlDataPath, pageui.Nav("/"), authLinks, fmt.Sprintf("%d", time.Now().Year()))

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
