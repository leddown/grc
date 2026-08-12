package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/jira"
	"grc/internal/pageui"
)

var jiraFilenameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// jiraHTTPClientFactory builds the HTTP client used for outbound Jira API
// calls. The base_url is user-supplied (to support self-hosted Jira on any
// domain), so the dialer below resolves the host and refuses to connect to
// private/loopback/link-local addresses (e.g. cloud metadata endpoints or
// internal services) to prevent SSRF via a malicious base_url. Tests
// override this var with a mock transport that never dials.
var jiraHTTPClientFactory = func() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: safeJiraDialContext,
		},
	}
}

func safeJiraDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for _, ipAddr := range resolved {
		if isDisallowedJiraTargetIP(ipAddr.IP) {
			lastErr = fmt.Errorf("connections to private or reserved addresses are not allowed")
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no usable address for %s", host)
	}
	return nil, lastErr
}

// cgnatBlock is the RFC 6598 Carrier-Grade NAT range (100.64.0.0/10), which
// Go's net.IP.IsPrivate() does not cover but which cloud/ISP environments
// sometimes route to internal management interfaces.
var cgnatBlock = func() *net.IPNet {
	_, block, _ := net.ParseCIDR("100.64.0.0/10")
	return block
}()

func isDisallowedJiraTargetIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		cgnatBlock.Contains(ip)
}

type jiraSnapshotRequest struct {
	BaseURL         string   `json:"base_url"`
	Email           string   `json:"email"`
	APIToken        string   `json:"api_token"`
	ProjectKey      string   `json:"project_key"`
	IssueKey        string   `json:"issue_key"`
	JQL             string   `json:"jql"`
	Fields          []string `json:"fields"`
	MaxResults      int      `json:"max_results"`
	IncludeChildren bool     `json:"include_children"`
	IncludeLinked   bool     `json:"include_linked"`
}

type jiraAuthTestRequest struct {
	BaseURL  string `json:"base_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
}

type jiraExportRequest struct {
	jiraSnapshotRequest
	FileName string `json:"file_name"`
}

type jiraReportPreset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	JQLTemplate string `json:"jql_template"`
}

type jiraReportRequest struct {
	BaseURL    string `json:"base_url"`
	Email      string `json:"email"`
	APIToken   string `json:"api_token"`
	ProjectKey string `json:"project_key"`
	PresetID   string `json:"preset_id"`
	JQL        string `json:"jql"`
	MaxResults int    `json:"max_results"`
}

type jiraReportItem struct {
	Key            string `json:"key"`
	Summary        string `json:"summary"`
	IssueType      string `json:"issue_type"`
	Status         string `json:"status"`
	StatusCategory string `json:"status_category"`
	Assignee       string `json:"assignee"`
	Priority       string `json:"priority"`
	Created        string `json:"created"`
	Updated        string `json:"updated"`
}

type jiraLinkedWorkItem struct {
	Link  jira.IssueLink `json:"link"`
	Issue *jira.Issue    `json:"issue"`
}

var jiraReportPresets = []jiraReportPreset{
	{
		ID:          "created_vs_resolved_30d",
		Name:        "Created vs Resolved (30d)",
		Description: "Issues created or resolved in the last 30 days, aligned to Created vs Resolved tracking.",
		JQLTemplate: `project = {{PROJECT}} AND (created >= -30d OR resolved >= -30d) ORDER BY updated DESC`,
	},
	{
		ID:          "resolution_time_30d",
		Name:        "Resolution Time (30d)",
		Description: "Recently resolved issues to evaluate resolution-time behavior.",
		JQLTemplate: `project = {{PROJECT}} AND statusCategory = Done AND resolved >= -30d ORDER BY resolved DESC`,
	},
	{
		ID:          "aging_open_work",
		Name:        "Aging Open Work",
		Description: "Open items ordered by age to surface long-running tickets.",
		JQLTemplate: `project = {{PROJECT}} AND statusCategory != Done ORDER BY created ASC`,
	},
	{
		ID:          "sprint_health_open",
		Name:        "Sprint Health (Open Sprint)",
		Description: "Work currently in open sprints, aligned with sprint-health and burndown-style review.",
		JQLTemplate: `project = {{PROJECT}} AND sprint IN openSprints() ORDER BY updated DESC`,
	},
	{
		ID:          "high_priority_open",
		Name:        "High Priority Open",
		Description: "Highest/high priority unfinished issues for escalations and risk review.",
		JQLTemplate: `project = {{PROJECT}} AND statusCategory != Done AND priority IN (Highest, High) ORDER BY updated DESC`,
	},
	{
		ID:          "assignee_workload",
		Name:        "Assignee Workload",
		Description: "Open issues sorted by assignee to quickly assess work distribution.",
		JQLTemplate: `project = {{PROJECT}} AND statusCategory != Done ORDER BY assignee ASC, priority DESC, updated DESC`,
	},
}

var jiraDefaultFields = []string{
	"summary",
	"status",
	"assignee",
	"priority",
	"issuetype",
	"created",
	"updated",
	"reporter",
}

func jiraJSONPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jira JSON Connector · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.94);
      --bg: linear-gradient(135deg, #f7f3eb, #ece4d6 55%, #e4d8c4);
    }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: Georgia, "Times New Roman", serif; color: var(--ink); background: var(--bg); }
    main {
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
      box-shadow: 0 24px 60px rgba(28,36,49,0.12);
    }
    h1 { margin: 0 0 8px; font-size: clamp(1.8rem, 3.2vw, 3rem); line-height: 1; }
    p { color: var(--muted); }
    .layout {
      display: grid;
      grid-template-columns: minmax(320px, 460px) minmax(0, 1fr);
      gap: 16px;
      margin-top: 16px;
      align-items: start;
    }
    .panel {
      border: 1px solid #334155;
      border-radius: 16px;
      background: #000;
      color: #e5e7eb;
      padding: 14px;
    }
    .field { display: grid; gap: 6px; margin-bottom: 10px; }
    .field label {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
    }
    fieldset {
      border: 1px solid #334155;
      border-radius: 12px;
      padding: 10px;
      margin: 0 0 10px;
      background: #111827;
    }
    legend {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      padding: 0 4px;
    }
    .checkbox-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 7px 10px;
    }
    .check-row {
      display: flex;
      align-items: center;
      gap: 7px;
      font-family: Arial, sans-serif;
      font-size: 13px;
      color: #e2e8f0;
      min-width: 0;
    }
    .check-row input {
      width: auto;
      padding: 0;
      margin: 0;
      accent-color: #38bdf8;
    }
    input, textarea, button {
      font: inherit;
      color: var(--ink);
    }
    input, textarea {
      width: 100%;
      padding: 10px 12px;
      border-radius: 10px;
      border: 1px solid var(--line);
      background: white;
    }
    textarea { min-height: 92px; resize: vertical; }
    .row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 8px;
    }
    .actions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-top: 4px;
    }
    button {
      padding: 10px 14px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: #dfc4b4;
      cursor: pointer;
    }
    button.secondary { background: white; }
    .panel button.secondary {
      background: #111827;
      border-color: #334155;
      color: #e5e7eb;
    }
    .field-tools {
      display: flex;
      gap: 8px;
      justify-content: flex-end;
      margin: -2px 0 10px;
    }
    .field-tools button {
      padding: 7px 11px;
      font-family: Arial, sans-serif;
      font-size: 12px;
    }
    .status {
      min-height: 20px;
      color: var(--muted);
      margin-top: 10px;
      font-family: Arial, sans-serif;
      font-size: 13px;
    }
    .mono {
      margin: 0;
      padding: 16px;
      border-radius: 14px;
      border: 1px solid #334155;
      background: #111827;
      overflow: auto;
      min-height: 72vh;
      font: 12.5px/1.55 "Courier New", monospace;
      color: #e2e8f0;
    }
    .hint { color: var(--muted); font-size: 13px; margin: 0 0 8px; }
    @media (max-width: 1020px) {
      .layout { grid-template-columns: 1fr; }
      .mono { min-height: 56vh; }
      .row { grid-template-columns: 1fr; }
      .checkbox-grid { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/jira/json") + `
    <h1>Jira JSON Connector</h1>
    <p>Connect to Jira Cloud REST APIs and view project/issue/search output as JSON. Export snapshots to static JSON files on this server.</p>
    <div class="layout">
      <section class="panel">
        <div class="field">
          <label for="baseUrl">Base URL</label>
          <input id="baseUrl" type="url" placeholder="https://your-domain.atlassian.net">
        </div>
        <div class="row">
          <div class="field">
            <label for="email">Atlassian Email</label>
            <input id="email" type="email" placeholder="name@company.com">
          </div>
          <div class="field">
            <label for="token">API Token</label>
            <input id="token" type="password" placeholder="Atlassian API token">
          </div>
        </div>
        <div class="row">
          <div class="field">
            <label for="projectKey">Project Key</label>
            <input id="projectKey" type="text" placeholder="SEC">
          </div>
          <div class="field">
            <label for="issueKey">Issue Key</label>
            <input id="issueKey" type="text" placeholder="SEC-123">
          </div>
        </div>
        <div class="field">
          <label for="jql">JQL (optional; auto-generated from project key if empty)</label>
          <textarea id="jql" placeholder='project = "SEC" AND statusCategory != Done ORDER BY updated DESC'></textarea>
        </div>
        <div class="row">
          <div class="field">
            <label for="maxResults">Max Results</label>
            <input id="maxResults" type="number" min="1" max="200" value="50">
          </div>
          <div class="field">
            <label for="fileName">Export File Name (optional)</label>
            <input id="fileName" type="text" placeholder="jira_snapshot.json">
          </div>
        </div>
        <fieldset>
          <legend>Related Work Items</legend>
          <div class="checkbox-grid">
            <label class="check-row"><input id="includeChildren" type="checkbox"> Child work items</label>
            <label class="check-row"><input id="includeLinked" type="checkbox"> Linked work items</label>
          </div>
        </fieldset>
        <fieldset>
          <legend>Fields To Download</legend>
          <div class="field-tools">
            <button id="selectAllFieldsBtn" type="button" class="secondary">Select All Fields</button>
            <button id="clearAllFieldsBtn" type="button" class="secondary">Clear All Fields</button>
          </div>
          <div class="checkbox-grid">
            <label class="check-row"><input name="jiraField" type="checkbox" value="summary" checked> Summary</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="status" checked> Status</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="assignee" checked> Assignee</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="priority" checked> Priority</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="issuetype" checked> Issue type</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="created" checked> Created</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="updated" checked> Updated</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="reporter" checked> Reporter</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="description"> Description</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="project"> Project</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="parent"> Parent</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="subtasks"> Subtasks</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="issuelinks"> Issue links</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="labels"> Labels</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="components"> Components</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="fixVersions"> Fix versions</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="versions"> Affects versions</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="resolution"> Resolution</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="resolutiondate"> Resolution date</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="duedate"> Due date</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="creator"> Creator</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="attachment"> Attachment</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="comment"> Comment</label>
            <label class="check-row"><input name="jiraField" type="checkbox" value="environment"> Environment</label>
          </div>
        </fieldset>
        <div class="actions">
          <button id="testAuthBtn" type="button" class="secondary">Test Authentication</button>
          <button id="loadBtn" type="button">Load JSON</button>
          <button id="exportBtn" type="button" class="secondary">Export JSON</button>
          <button id="localLoadBtn" type="button" class="secondary">Load Local JSON</button>
          <input id="localFile" type="file" accept=".json,application/json" style="display:none;">
        </div>
        <div id="status" class="status"></div>
      </section>
      <section class="panel">
        <p class="hint">Output JSON</p>
        <pre id="output" class="mono">{}</pre>
      </section>
    </div>
  </main>
  <script>
    const els = {
      baseUrl: document.getElementById('baseUrl'),
      email: document.getElementById('email'),
      token: document.getElementById('token'),
      projectKey: document.getElementById('projectKey'),
      issueKey: document.getElementById('issueKey'),
      jql: document.getElementById('jql'),
      fieldChecks: Array.from(document.querySelectorAll('input[name="jiraField"]')),
      includeChildren: document.getElementById('includeChildren'),
      includeLinked: document.getElementById('includeLinked'),
      maxResults: document.getElementById('maxResults'),
      fileName: document.getElementById('fileName'),
      selectAllFieldsBtn: document.getElementById('selectAllFieldsBtn'),
      clearAllFieldsBtn: document.getElementById('clearAllFieldsBtn'),
      testAuthBtn: document.getElementById('testAuthBtn'),
      loadBtn: document.getElementById('loadBtn'),
      exportBtn: document.getElementById('exportBtn'),
      localLoadBtn: document.getElementById('localLoadBtn'),
      localFile: document.getElementById('localFile'),
      status: document.getElementById('status'),
      output: document.getElementById('output'),
    };

    const jiraConnectionStorageKey = 'grc_jira_connection';

    function checkedFieldValues() {
      return els.fieldChecks.filter((el) => el.checked).map((el) => el.value);
    }

    function readSavedJiraConnection() {
      try {
        return JSON.parse(localStorage.getItem(jiraConnectionStorageKey) || '{}') || {};
      } catch (err) {
        return {};
      }
    }

    function persistJiraConnection() {
      const settings = {
        base_url: els.baseUrl.value.trim(),
        email: els.email.value.trim(),
        // api_token is intentionally never persisted to localStorage: it's a
        // long-lived secret, and localStorage is readable by any XSS or
        // browser extension with page access.
        project_key: els.projectKey.value.trim(),
        issue_key: els.issueKey.value.trim(),
        jql: els.jql.value.trim(),
        fields: checkedFieldValues(),
        include_children: els.includeChildren.checked,
        include_linked: els.includeLinked.checked,
        max_results: els.maxResults.value,
        file_name: els.fileName.value.trim(),
      };
      try {
        localStorage.setItem(jiraConnectionStorageKey, JSON.stringify(settings));
      } catch (err) {
        // Some browsers block localStorage; Jira calls still work without persistence.
      }
    }

    function loadSavedJiraConnection() {
      const saved = readSavedJiraConnection();
      if (saved.base_url) els.baseUrl.value = saved.base_url;
      if (saved.email) els.email.value = saved.email;
      if (saved.project_key) els.projectKey.value = saved.project_key;
      if (saved.issue_key) els.issueKey.value = saved.issue_key;
      if (saved.jql) els.jql.value = saved.jql;
      if (saved.max_results) els.maxResults.value = saved.max_results;
      if (saved.file_name) els.fileName.value = saved.file_name;
      if (typeof saved.include_children === 'boolean') els.includeChildren.checked = saved.include_children;
      if (typeof saved.include_linked === 'boolean') els.includeLinked.checked = saved.include_linked;
      if (Array.isArray(saved.fields)) {
        const selected = new Set(saved.fields);
        els.fieldChecks.forEach((el) => {
          el.checked = selected.has(el.value);
        });
      }
    }

    function bindJiraConnectionPersistence() {
      [
        els.baseUrl,
        els.email,
        els.token,
        els.projectKey,
        els.issueKey,
        els.jql,
        els.maxResults,
        els.fileName,
      ].forEach((el) => el.addEventListener('input', persistJiraConnection));

      [
        els.includeChildren,
        els.includeLinked,
        ...els.fieldChecks,
      ].forEach((el) => el.addEventListener('change', persistJiraConnection));
    }

    function selectAllFields() {
      els.fieldChecks.forEach((el) => {
        el.checked = true;
      });
      persistJiraConnection();
      els.status.textContent = 'All Jira fields selected.';
    }

    function clearAllFields() {
      els.fieldChecks.forEach((el) => {
        el.checked = false;
      });
      persistJiraConnection();
      els.status.textContent = 'All Jira fields cleared.';
    }

    async function saveStoredJSON(name, source, content) {
      const resp = await fetch('/stored-json', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, source, content }),
      });
      const data = await resp.json().catch(() => ({}));
      if (!resp.ok) {
        throw new Error(data.error || ('HTTP ' + resp.status));
      }
      return data;
    }

    function storedJSONName(prefix) {
      return prefix + '_' + new Date().toISOString().replaceAll(':', '-').replaceAll('.', '-') + '.json';
    }

    function buildPayload() {
      return {
        base_url: els.baseUrl.value.trim(),
        email: els.email.value.trim(),
        api_token: els.token.value,
        project_key: els.projectKey.value.trim(),
        issue_key: els.issueKey.value.trim(),
        jql: els.jql.value.trim(),
        fields: checkedFieldValues(),
        max_results: Number(els.maxResults.value || '50'),
        include_children: els.includeChildren.checked,
        include_linked: els.includeLinked.checked,
      };
    }

    function buildAuthPayload() {
      return {
        base_url: els.baseUrl.value.trim(),
        email: els.email.value.trim(),
        api_token: els.token.value,
      };
    }

    async function testAuthentication() {
      persistJiraConnection();
      els.status.textContent = 'Testing Jira authentication...';
      try {
        const resp = await fetch('/jira/json/test-auth', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(buildAuthPayload()),
        });
        const data = await resp.json();
        if (!resp.ok) {
          throw new Error(data.error || ('HTTP ' + resp.status));
        }
        els.output.textContent = JSON.stringify(data, null, 2);
        const name = data.user && (data.user.displayName || data.user.emailAddress || data.user.accountId);
        els.status.textContent = 'Authentication succeeded' + (name ? ': ' + name : '.');
      } catch (err) {
        els.status.textContent = 'Authentication failed: ' + err.message;
        els.output.textContent = '{}';
      }
    }

    async function loadJSON() {
      persistJiraConnection();
      const payload = buildPayload();
      els.status.textContent = 'Loading Jira data...';
      try {
        const resp = await fetch('/jira/json/data', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const data = await resp.json();
        if (!resp.ok) {
          throw new Error(data.error || ('HTTP ' + resp.status));
        }
        els.output.textContent = JSON.stringify(data, null, 2);
        let savedNote = '';
        try {
          const saved = await saveStoredJSON(storedJSONName('jira_snapshot'), 'jira-json', data);
          savedNote = ' Saved to SQLite #' + saved.id + '.';
        } catch (saveErr) {
          savedNote = ' SQLite save failed: ' + saveErr.message;
        }
        els.status.textContent = 'Loaded JSON snapshot.' + savedNote;
      } catch (err) {
        els.status.textContent = 'Load failed: ' + err.message;
        els.output.textContent = '{}';
      }
    }

    async function exportJSON() {
      persistJiraConnection();
      const payload = buildPayload();
      payload.file_name = els.fileName.value.trim();
      els.status.textContent = 'Exporting static JSON...';
      try {
        const resp = await fetch('/jira/json/export', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const data = await resp.json();
        if (!resp.ok) {
          throw new Error(data.error || ('HTTP ' + resp.status));
        }
        if (data.snapshot) {
          els.output.textContent = JSON.stringify(data.snapshot, null, 2);
        }
        els.status.textContent = 'Exported to ' + data.path + ' (' + data.bytes + ' bytes).';
      } catch (err) {
        els.status.textContent = 'Export failed: ' + err.message;
      }
    }

    function loadLocalJSON(file) {
      if (!file) {
        return;
      }
      els.status.textContent = 'Loading local JSON file...';
      const reader = new FileReader();
      reader.onload = async () => {
        try {
          const parsed = JSON.parse(String(reader.result));
          els.output.textContent = JSON.stringify(parsed, null, 2);
          let savedNote = '';
          try {
            const saved = await saveStoredJSON(file.name, 'local-json-file', parsed);
            savedNote = ' Saved to SQLite #' + saved.id + '.';
          } catch (saveErr) {
            savedNote = ' SQLite save failed: ' + saveErr.message;
          }
          els.status.textContent = 'Loaded local JSON file: ' + file.name + '.' + savedNote;
        } catch (err) {
          els.output.textContent = '{}';
          els.status.textContent = 'Local file is not valid JSON: ' + err.message;
        } finally {
          els.localFile.value = '';
        }
      };
      reader.onerror = () => {
        els.output.textContent = '{}';
        els.status.textContent = 'Failed to read local file.';
        els.localFile.value = '';
      };
      reader.readAsText(file);
    }

    loadSavedJiraConnection();
    bindJiraConnectionPersistence();
    els.selectAllFieldsBtn.addEventListener('click', selectAllFields);
    els.clearAllFieldsBtn.addEventListener('click', clearAllFields);
    els.testAuthBtn.addEventListener('click', testAuthentication);
    els.loadBtn.addEventListener('click', loadJSON);
    els.exportBtn.addEventListener('click', exportJSON);
    els.localLoadBtn.addEventListener('click', () => {
      els.localFile.click();
    });
    els.localFile.addEventListener('change', () => {
      const file = els.localFile.files && els.localFile.files[0];
      loadLocalJSON(file);
    });
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func jiraReportsPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jira Reports · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.94);
      --bg: linear-gradient(135deg, #f7f3eb, #ece4d6 55%, #e4d8c4);
    }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: Georgia, "Times New Roman", serif; color: var(--ink); background: var(--bg); }
    main {
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
      box-shadow: 0 24px 60px rgba(28,36,49,0.12);
    }
    h1 { margin: 0 0 8px; font-size: clamp(1.8rem, 3.2vw, 3rem); line-height: 1; }
    p { color: var(--muted); }
    .layout {
      display: grid;
      grid-template-columns: minmax(320px, 460px) minmax(0, 1fr);
      gap: 16px;
      margin-top: 16px;
      align-items: start;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 16px;
      background: rgba(255,255,255,0.88);
      padding: 14px;
    }
    .field { display: grid; gap: 6px; margin-bottom: 10px; }
    .field label {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
    }
    input, textarea, select, button {
      font: inherit;
      color: var(--ink);
    }
    input, textarea, select {
      width: 100%;
      padding: 10px 12px;
      border-radius: 10px;
      border: 1px solid var(--line);
      background: white;
    }
    textarea { min-height: 110px; resize: vertical; }
    .row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 8px;
    }
    button {
      padding: 10px 14px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: #dfc4b4;
      cursor: pointer;
    }
    .status {
      min-height: 20px;
      color: var(--muted);
      margin-top: 10px;
      font-family: Arial, sans-serif;
      font-size: 13px;
    }
    .kpis {
      display: grid;
      grid-template-columns: repeat(4, minmax(110px, 1fr));
      gap: 8px;
      margin-bottom: 10px;
    }
    .stored-json-tools {
      display: grid;
      grid-template-columns: minmax(220px, 1fr) auto;
      gap: 8px;
      align-items: end;
      margin-bottom: 10px;
    }
    .kpi {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 12px;
      background: #fbf8f2;
      padding: 10px;
    }
    .kpi .label {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      margin-bottom: 6px;
    }
    .kpi .value {
      font-family: "Courier New", monospace;
      font-size: 20px;
      line-height: 1;
    }
    .table-wrap {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 12px;
      overflow: auto;
      background: #fbf8f2;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      min-width: 900px;
      font-family: Arial, sans-serif;
      font-size: 13px;
    }
    th, td {
      text-align: left;
      padding: 9px 10px;
      border-bottom: 1px solid rgba(215,206,191,0.8);
      vertical-align: top;
    }
    th {
      position: sticky;
      top: 0;
      background: #f3ece0;
      color: #5a4630;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      font-size: 11px;
    }
    .mono {
      margin-top: 10px;
      padding: 12px;
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 12px;
      background: #fbf8f2;
      min-height: 220px;
      max-height: 42vh;
      overflow: auto;
      font: 12px/1.5 "Courier New", monospace;
    }
    @media (max-width: 1100px) {
      .layout { grid-template-columns: 1fr; }
      .row { grid-template-columns: 1fr; }
      .kpis { grid-template-columns: 1fr 1fr; }
      .stored-json-tools { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/jira/reports") + `
    <h1>Jira Reports (Read-Only)</h1>
    <p>Run report/search presets inspired by common Jira reporting dashboards and integrations. Uses read-only Jira search patterns and returns JSON + tabular output.</p>
    <div class="layout">
      <section class="panel">
        <div class="field">
          <label for="baseUrl">Base URL</label>
          <input id="baseUrl" type="url" placeholder="https://your-domain.atlassian.net">
        </div>
        <div class="row">
          <div class="field">
            <label for="email">Atlassian Email</label>
            <input id="email" type="email" placeholder="name@company.com">
          </div>
          <div class="field">
            <label for="token">API Token</label>
            <input id="token" type="password" placeholder="Atlassian API token">
          </div>
        </div>
        <div class="row">
          <div class="field">
            <label for="projectKey">Project Key</label>
            <input id="projectKey" type="text" placeholder="SEC">
          </div>
          <div class="field">
            <label for="maxResults">Max Results</label>
            <input id="maxResults" type="number" min="1" max="200" value="100">
          </div>
        </div>
        <div class="field">
          <label for="preset">Report Preset</label>
          <select id="preset"></select>
        </div>
        <div class="field">
          <label for="jql">JQL Override (optional)</label>
          <textarea id="jql" placeholder='Leave empty to use selected preset'></textarea>
        </div>
        <button id="runBtn" type="button">Run Report</button>
        <div id="status" class="status"></div>
      </section>
      <section class="panel">
        <div class="stored-json-tools">
          <div class="field">
            <label for="storedJSONSelect">Stored SQLite JSON</label>
            <select id="storedJSONSelect"></select>
          </div>
          <button id="loadStoredJSONBtn" type="button" class="secondary">Load Stored JSON</button>
        </div>
        <div class="kpis">
          <div class="kpi"><div class="label">Total</div><div id="kpiTotal" class="value">0</div></div>
          <div class="kpi"><div class="label">To Do</div><div id="kpiTodo" class="value">0</div></div>
          <div class="kpi"><div class="label">In Progress</div><div id="kpiInProgress" class="value">0</div></div>
          <div class="kpi"><div class="label">Done</div><div id="kpiDone" class="value">0</div></div>
        </div>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Key</th>
                <th>Summary</th>
                <th>Status</th>
                <th>Category</th>
                <th>Priority</th>
                <th>Assignee</th>
                <th>Issue Type</th>
                <th>Updated</th>
              </tr>
            </thead>
            <tbody id="rows"></tbody>
          </table>
        </div>
        <pre id="raw" class="mono">{}</pre>
      </section>
    </div>
  </main>
  <script>
    const els = {
      baseUrl: document.getElementById('baseUrl'),
      email: document.getElementById('email'),
      token: document.getElementById('token'),
      projectKey: document.getElementById('projectKey'),
      maxResults: document.getElementById('maxResults'),
      preset: document.getElementById('preset'),
      jql: document.getElementById('jql'),
      storedJSONSelect: document.getElementById('storedJSONSelect'),
      loadStoredJSONBtn: document.getElementById('loadStoredJSONBtn'),
      runBtn: document.getElementById('runBtn'),
      status: document.getElementById('status'),
      rows: document.getElementById('rows'),
      raw: document.getElementById('raw'),
      kpiTotal: document.getElementById('kpiTotal'),
      kpiTodo: document.getElementById('kpiTodo'),
      kpiInProgress: document.getElementById('kpiInProgress'),
      kpiDone: document.getElementById('kpiDone'),
    };

    const jiraConnectionStorageKey = 'grc_jira_connection';

    function esc(value) {
      return String(value ?? '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function readSavedJiraConnection() {
      try {
        return JSON.parse(localStorage.getItem(jiraConnectionStorageKey) || '{}') || {};
      } catch (err) {
        return {};
      }
    }

    function persistJiraConnection() {
      const previous = readSavedJiraConnection();
      const settings = {
        ...previous,
        base_url: els.baseUrl.value.trim(),
        email: els.email.value.trim(),
        // api_token is intentionally never persisted to localStorage (see
        // jiraJSONPage); drop any value carried over from a previous build.
        api_token: undefined,
        project_key: els.projectKey.value.trim(),
        max_results: els.maxResults.value,
        preset_id: els.preset.value,
        report_jql: els.jql.value.trim(),
      };
      try {
        localStorage.setItem(jiraConnectionStorageKey, JSON.stringify(settings));
      } catch (err) {
        // Some browsers block localStorage; Jira calls still work without persistence.
      }
    }

    function loadSavedJiraConnection() {
      const saved = readSavedJiraConnection();
      if (saved.base_url) els.baseUrl.value = saved.base_url;
      if (saved.email) els.email.value = saved.email;
      if (saved.project_key) els.projectKey.value = saved.project_key;
      if (saved.max_results) els.maxResults.value = saved.max_results;
      if (saved.report_jql) {
        els.jql.value = saved.report_jql;
      } else if (saved.jql) {
        els.jql.value = saved.jql;
      }
    }

    function bindJiraConnectionPersistence() {
      [
        els.baseUrl,
        els.email,
        els.token,
        els.projectKey,
        els.maxResults,
        els.jql,
      ].forEach((el) => el.addEventListener('input', persistJiraConnection));
      els.preset.addEventListener('change', persistJiraConnection);
    }

    async function saveStoredJSON(name, source, content) {
      const resp = await fetch('/stored-json', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, source, content }),
      });
      const data = await resp.json().catch(() => ({}));
      if (!resp.ok) {
        throw new Error(data.error || ('HTTP ' + resp.status));
      }
      return data;
    }

    function storedJSONName(prefix) {
      return prefix + '_' + new Date().toISOString().replaceAll(':', '-').replaceAll('.', '-') + '.json';
    }

    async function loadStoredJSONOptions(selectedID = '') {
      const resp = await fetch('/stored-json');
      const data = await resp.json();
      if (!resp.ok) {
        throw new Error(data.error || ('HTTP ' + resp.status));
      }
      const items = data.items || [];
      if (!items.length) {
        els.storedJSONSelect.innerHTML = '<option value="">No stored JSON documents</option>';
        els.loadStoredJSONBtn.disabled = true;
        return;
      }
      els.loadStoredJSONBtn.disabled = false;
      els.storedJSONSelect.innerHTML = items.map((item) => {
        const id = esc(item.id);
        const name = esc(item.name || 'Stored JSON');
        const source = esc(item.source || 'unknown');
        const created = esc(item.created_at || '');
        return '<option value="' + id + '">' + name + ' - ' + source + (created ? ' - ' + created : '') + '</option>';
      }).join('');
      if (selectedID) {
        els.storedJSONSelect.value = String(selectedID);
      }
    }

    async function loadStoredJSONDocument() {
      const id = els.storedJSONSelect.value || '';
      if (!id) {
        els.status.textContent = 'No stored JSON document selected.';
        return;
      }

      els.status.textContent = 'Loading stored JSON document...';
      try {
        const resp = await fetch('/stored-json/' + encodeURIComponent(id));
        const data = await resp.json();
        if (!resp.ok) {
          throw new Error(data.error || ('HTTP ' + resp.status));
        }
        const content = data.content || {};
        els.raw.textContent = JSON.stringify(content, null, 2);
        if (Array.isArray(content.items)) {
          renderRows(content.items);
          updateKPIs(content.summary || { total: content.items.length });
        } else {
          els.rows.innerHTML = '<tr><td colspan="8">Stored JSON loaded in raw output.</td></tr>';
          updateKPIs({});
        }
        els.status.textContent = 'Loaded stored JSON: ' + (data.name || ('#' + id)) + '.';
      } catch (err) {
        els.status.textContent = 'Failed to load stored JSON: ' + err.message;
      }
    }

    function renderRows(items) {
      if (!items.length) {
        els.rows.innerHTML = '<tr><td colspan="8">No rows returned.</td></tr>';
        return;
      }
      els.rows.innerHTML = items.map((item) => {
        return '<tr>' +
          '<td>' + esc(item.key) + '</td>' +
          '<td>' + esc(item.summary) + '</td>' +
          '<td>' + esc(item.status) + '</td>' +
          '<td>' + esc(item.status_category) + '</td>' +
          '<td>' + esc(item.priority) + '</td>' +
          '<td>' + esc(item.assignee || 'Unassigned') + '</td>' +
          '<td>' + esc(item.issue_type) + '</td>' +
          '<td>' + esc(item.updated) + '</td>' +
          '</tr>';
      }).join('');
    }

    function updateKPIs(summary) {
      els.kpiTotal.textContent = String(summary.total || 0);
      els.kpiTodo.textContent = String(summary.todo || 0);
      els.kpiInProgress.textContent = String(summary.in_progress || 0);
      els.kpiDone.textContent = String(summary.done || 0);
    }

    async function loadPresets() {
      const resp = await fetch('/jira/reports/presets');
      const data = await resp.json();
      if (!resp.ok) {
        throw new Error(data.error || ('HTTP ' + resp.status));
      }
      els.preset.innerHTML = data.items.map((item) => {
        return '<option value="' + esc(item.id) + '">' + esc(item.name) + '</option>';
      }).join('');
      const saved = readSavedJiraConnection();
      if (saved.preset_id) {
        els.preset.value = saved.preset_id;
      }
      if (data.items.length) {
        const selected = data.items.find((item) => item.id === els.preset.value) || data.items[0];
        els.status.textContent = selected.description;
      }
      els.preset.addEventListener('change', () => {
        const selected = data.items.find((item) => item.id === els.preset.value);
        if (selected) {
          els.status.textContent = selected.description;
        }
      });
    }

    async function runReport() {
      persistJiraConnection();
      els.status.textContent = 'Running report...';
      const payload = {
        base_url: els.baseUrl.value.trim(),
        email: els.email.value.trim(),
        api_token: els.token.value,
        project_key: els.projectKey.value.trim(),
        preset_id: els.preset.value,
        jql: els.jql.value.trim(),
        max_results: Number(els.maxResults.value || '100'),
      };
      try {
        const resp = await fetch('/jira/reports/data', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const data = await resp.json();
        if (!resp.ok) {
          throw new Error(data.error || ('HTTP ' + resp.status));
        }
        renderRows(data.items || []);
        updateKPIs(data.summary || {});
        els.raw.textContent = JSON.stringify(data, null, 2);
        let savedNote = '';
        try {
          const saved = await saveStoredJSON(storedJSONName('jira_report'), 'jira-report', data);
          savedNote = ' Saved to SQLite #' + saved.id + '.';
          await loadStoredJSONOptions(saved.id);
        } catch (saveErr) {
          savedNote = ' SQLite save failed: ' + saveErr.message;
        }
        els.status.textContent = 'Report completed.' + savedNote;
      } catch (err) {
        els.status.textContent = 'Report failed: ' + err.message;
        els.rows.innerHTML = '';
        updateKPIs({});
        els.raw.textContent = '{}';
      }
    }

    loadSavedJiraConnection();
    bindJiraConnectionPersistence();
    els.runBtn.addEventListener('click', runReport);
    els.loadStoredJSONBtn.addEventListener('click', loadStoredJSONDocument);
    loadPresets().catch((err) => {
      els.status.textContent = 'Failed to load presets: ' + err.message;
    });
    loadStoredJSONOptions().catch((err) => {
      els.storedJSONSelect.innerHTML = '<option value="">Stored JSON unavailable</option>';
      els.loadStoredJSONBtn.disabled = true;
      els.status.textContent = 'Failed to load stored JSON list: ' + err.message;
    });
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func jiraJSONData(c *gin.Context) {
	var req jiraSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	snapshot, err := buildJiraSnapshot(c, req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "jira api error") {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, snapshot)
}

func jiraJSONTestAuth(c *gin.Context) {
	var req jiraAuthTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	client, err := jiraClientFromInput(req.BaseURL, req.Email, req.APIToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := client.GetMyself(c.Request.Context())
	if err != nil {
		status := http.StatusBadGateway
		var apiErr *jira.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case http.StatusUnauthorized:
				status = http.StatusUnauthorized
			case http.StatusForbidden:
				status = http.StatusForbidden
			}
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":   true,
		"user": user,
	})
}

func jiraJSONExport(c *gin.Context) {
	var req jiraExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	snapshot, err := buildJiraSnapshot(c, req.jiraSnapshotRequest)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "jira api error") {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	fileBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal export JSON"})
		return
	}

	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "jira_snapshot_" + time.Now().UTC().Format("20060102_150405") + ".json"
	}
	fileName = sanitizeJiraFileName(fileName)
	if !strings.HasSuffix(strings.ToLower(fileName), ".json") {
		fileName += ".json"
	}

	exportDir := filepath.Join("exports", "jira")
	if err := os.MkdirAll(exportDir, 0o750); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create export directory"})
		return
	}
	path := filepath.Join(exportDir, fileName)
	if err := os.WriteFile(path, fileBytes, 0o600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write export file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":     path,
		"file":     fileName,
		"bytes":    len(fileBytes),
		"snapshot": snapshot,
	})
}

func jiraReportPresetsData(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": jiraReportPresets})
}

func jiraReportsData(c *gin.Context) {
	var req jiraReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	client, err := jiraClientFromInput(req.BaseURL, req.Email, req.APIToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	projectKey := strings.TrimSpace(req.ProjectKey)
	if projectKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_key is required"})
		return
	}

	jql, err := resolveReportJQL(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	searchResp, err := client.SearchIssues(c.Request.Context(), jira.SearchRequest{
		JQL:        jql,
		MaxResults: normalizeMaxResults(req.MaxResults),
		Fields:     jiraDefaultFields,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	items := make([]jiraReportItem, 0, len(searchResp.Issues))
	summary := map[string]int{
		"total":       0,
		"todo":        0,
		"in_progress": 0,
		"done":        0,
	}
	for _, issue := range searchResp.Issues {
		item := jiraReportItem{
			Key:            issue.Key,
			Summary:        strings.TrimSpace(issue.Fields.Summary),
			IssueType:      strings.TrimSpace(issue.Fields.IssueType.Name),
			Status:         strings.TrimSpace(issue.Fields.Status.Name),
			StatusCategory: strings.TrimSpace(issue.Fields.Status.StatusCategory.Name),
			Assignee:       strings.TrimSpace(issue.Fields.Assignee.DisplayName),
			Priority:       strings.TrimSpace(issue.Fields.Priority.Name),
			Created:        strings.TrimSpace(issue.Fields.Created),
			Updated:        strings.TrimSpace(issue.Fields.Updated),
		}
		items = append(items, item)
		summary["total"]++
		switch strings.ToLower(item.StatusCategory) {
		case "to do":
			summary["todo"]++
		case "in progress":
			summary["in_progress"]++
		case "done":
			summary["done"]++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"preset_id":   strings.TrimSpace(req.PresetID),
		"project_key": projectKey,
		"jql":         jql,
		"returned":    len(items),
		"max_results": normalizeMaxResults(req.MaxResults),
		"summary":     summary,
		"items":       items,
		"raw":         searchResp,
	})
}

func buildJiraSnapshot(c *gin.Context, req jiraSnapshotRequest) (gin.H, error) {
	client, err := jiraClientFromInput(req.BaseURL, req.Email, req.APIToken)
	if err != nil {
		return nil, err
	}

	projectKey := strings.TrimSpace(req.ProjectKey)
	issueKey := strings.TrimSpace(req.IssueKey)
	jql := strings.TrimSpace(req.JQL)
	fields := normalizeJiraFields(req.Fields)
	requestFields := appendRelationshipFields(fields, req.IncludeChildren, req.IncludeLinked)
	if projectKey == "" && issueKey == "" && jql == "" {
		return nil, fmt.Errorf("provide at least one of project_key, issue_key, or jql")
	}
	if jql == "" && projectKey != "" {
		jql = fmt.Sprintf(`project = "%s" ORDER BY updated DESC`, escapeJQLString(projectKey))
	}

	payload := gin.H{
		"generated_at":     time.Now().UTC().Format(time.RFC3339),
		"source":           "jira-cloud-rest-v3",
		"project_key":      projectKey,
		"issue_key":        issueKey,
		"jql":              jql,
		"fields":           fields,
		"request_fields":   requestFields,
		"max_results":      normalizeMaxResults(req.MaxResults),
		"include_children": req.IncludeChildren,
		"include_linked":   req.IncludeLinked,
	}

	if projectKey != "" {
		project, err := client.GetProject(c.Request.Context(), projectKey)
		if err != nil {
			return nil, err
		}
		payload["project"] = project
	}
	rootIssues := make([]jira.Issue, 0)
	if issueKey != "" {
		issue, err := client.GetIssue(c.Request.Context(), issueKey, requestFields)
		if err != nil {
			return nil, err
		}
		payload["issue"] = issue
		rootIssues = append(rootIssues, *issue)
	}
	if jql != "" {
		searchResp, err := client.SearchIssues(c.Request.Context(), jira.SearchRequest{
			JQL:        jql,
			MaxResults: normalizeMaxResults(req.MaxResults),
			Fields:     requestFields,
		})
		if err != nil {
			return nil, err
		}
		payload["search"] = searchResp
		rootIssues = append(rootIssues, searchResp.Issues...)
	}
	if req.IncludeChildren || req.IncludeLinked {
		related, err := buildJiraRelatedWorkItems(c.Request.Context(), client, rootIssues, fields, normalizeMaxResults(req.MaxResults), req.IncludeChildren, req.IncludeLinked)
		if err != nil {
			return nil, err
		}
		payload["related_work_items"] = related
	}
	return payload, nil
}

func buildJiraRelatedWorkItems(ctx context.Context, client *jira.Client, rootIssues []jira.Issue, fields []string, maxResults int, includeChildren bool, includeLinked bool) (gin.H, error) {
	roots := dedupeJiraIssues(rootIssues)
	out := gin.H{
		"root_issue_keys": issueKeys(roots),
	}

	if includeChildren {
		childrenByParent := make(map[string]*jira.SearchResponse, len(roots))
		for _, root := range roots {
			if strings.TrimSpace(root.Key) == "" {
				continue
			}
			children, err := client.SearchIssues(ctx, jira.SearchRequest{
				JQL:        fmt.Sprintf(`parent = "%s" ORDER BY updated DESC`, escapeJQLString(root.Key)),
				MaxResults: maxResults,
				Fields:     fields,
			})
			if err != nil {
				return nil, fmt.Errorf("fetch child work items for %s: %w", root.Key, err)
			}
			childrenByParent[root.Key] = children
		}
		out["children_by_parent"] = childrenByParent
	}

	if includeLinked {
		linkedByIssue := make(map[string][]jiraLinkedWorkItem, len(roots))
		linkedIssueCache := make(map[string]*jira.Issue)
		for _, root := range roots {
			if strings.TrimSpace(root.Key) == "" {
				continue
			}
			items := make([]jiraLinkedWorkItem, 0, len(root.Fields.IssueLinks))
			for _, link := range root.Fields.IssueLinks {
				linkedKey := linkedIssueKey(link, root.Key)
				if linkedKey == "" {
					continue
				}
				linkedIssue, ok := linkedIssueCache[linkedKey]
				if !ok {
					var err error
					linkedIssue, err = client.GetIssue(ctx, linkedKey, fields)
					if err != nil {
						return nil, fmt.Errorf("fetch linked work item %s from %s: %w", linkedKey, root.Key, err)
					}
					linkedIssueCache[linkedKey] = linkedIssue
				}
				items = append(items, jiraLinkedWorkItem{
					Link:  link,
					Issue: linkedIssue,
				})
			}
			linkedByIssue[root.Key] = items
		}
		out["linked_by_issue"] = linkedByIssue
	}

	return out, nil
}

func appendRelationshipFields(fields []string, includeChildren bool, includeLinked bool) []string {
	out := normalizeJiraFields(fields)
	if includeChildren {
		out = appendUniqueJiraFields(out, "subtasks", "parent")
	}
	if includeLinked {
		out = appendUniqueJiraFields(out, "issuelinks")
	}
	return out
}

func appendUniqueJiraFields(fields []string, values ...string) []string {
	seen := make(map[string]struct{}, len(fields)+len(values))
	out := make([]string, 0, len(fields)+len(values))
	for _, field := range fields {
		normalized := strings.TrimSpace(field)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func dedupeJiraIssues(issues []jira.Issue) []jira.Issue {
	seen := make(map[string]struct{}, len(issues))
	out := make([]jira.Issue, 0, len(issues))
	for _, issue := range issues {
		key := strings.TrimSpace(issue.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, issue)
	}
	return out
}

func issueKeys(issues []jira.Issue) []string {
	keys := make([]string, 0, len(issues))
	for _, issue := range issues {
		if strings.TrimSpace(issue.Key) != "" {
			keys = append(keys, issue.Key)
		}
	}
	return keys
}

func linkedIssueKey(link jira.IssueLink, rootKey string) string {
	rootKey = strings.TrimSpace(rootKey)
	if link.OutwardIssue != nil {
		key := strings.TrimSpace(link.OutwardIssue.Key)
		if key != "" && key != rootKey {
			return key
		}
	}
	if link.InwardIssue != nil {
		key := strings.TrimSpace(link.InwardIssue.Key)
		if key != "" && key != rootKey {
			return key
		}
	}
	return ""
}

func jiraClientFromInput(baseURL, email, apiToken string) (*jira.Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	email = strings.TrimSpace(email)
	apiToken = strings.TrimSpace(apiToken)
	if baseURL == "" || email == "" || apiToken == "" {
		return nil, fmt.Errorf("base_url, email, and api_token are required")
	}
	validatedBaseURL, err := validatedEndpoint(baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	cfg := jira.Config{
		BaseURL:  validatedBaseURL,
		Email:    email,
		APIToken: apiToken,
	}
	if client := jiraHTTPClientFactory(); client != nil {
		cfg.HTTPClient = client
	}
	return jira.NewClient(cfg)
}

func normalizeJiraFields(fields []string) []string {
	if len(fields) == 0 {
		return append([]string(nil), jiraDefaultFields...)
	}
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		normalized := strings.TrimSpace(field)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return append([]string(nil), jiraDefaultFields...)
	}
	return out
}

func normalizeMaxResults(value int) int {
	if value <= 0 {
		return 50
	}
	if value > 200 {
		return 200
	}
	return value
}

func resolveReportJQL(req jiraReportRequest) (string, error) {
	override := strings.TrimSpace(req.JQL)
	if override != "" {
		return override, nil
	}
	projectKey := strings.TrimSpace(req.ProjectKey)
	if projectKey == "" {
		return "", fmt.Errorf("project_key is required")
	}

	presetID := strings.TrimSpace(req.PresetID)
	for _, preset := range jiraReportPresets {
		if preset.ID != presetID {
			continue
		}
		return strings.ReplaceAll(preset.JQLTemplate, "{{PROJECT}}", `"`+escapeJQLString(projectKey)+`"`), nil
	}
	return "", fmt.Errorf("unsupported preset_id")
}

func sanitizeJiraFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "jira_snapshot_" + strconv.FormatInt(time.Now().UTC().Unix(), 10) + ".json"
	}
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	name = jiraFilenameSanitizer.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._-")
	if name == "" {
		return "jira_snapshot_" + strconv.FormatInt(time.Now().UTC().Unix(), 10) + ".json"
	}
	return name
}

func escapeJQLString(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}
