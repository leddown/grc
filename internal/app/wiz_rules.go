package app

import (
	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

func wizRulesPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Wiz Rules · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.92);
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
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.4rem); line-height: 0.95; }
    p { color: var(--muted); }
    .layout {
      display: grid;
      grid-template-columns: minmax(340px, 1fr) minmax(340px, 1fr) minmax(0, 1.3fr);
      gap: 14px;
      margin-top: 18px;
      align-items: start;
    }
    .panel {
      border: 1px solid var(--line);
      border-radius: 14px;
      background: var(--panel);
      overflow: hidden;
    }
    .panel-header {
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
      background: #efe6d6;
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .panel-body { padding: 14px; }
    .field { margin-bottom: 10px; }
    .field label {
      display: block;
      margin-bottom: 6px;
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    input, select, textarea, button {
      width: 100%;
      padding: 10px 11px;
      border-radius: 10px;
      border: 1px solid var(--line);
      background: var(--bg);
      color: var(--ink);
      font: inherit;
    }
    textarea { min-height: 94px; resize: vertical; }
    button {
      width: auto;
      cursor: pointer;
      background: #dfc4b4;
    }
    button:hover { background: #d2b09d; }
    .row {
      display: grid;
      grid-template-columns: 110px minmax(120px, 1fr) minmax(120px, 1fr) minmax(140px, 1.2fr) auto;
      gap: 8px;
      margin-bottom: 8px;
      align-items: center;
    }
    .mono {
      white-space: pre-wrap;
      background: var(--surface-strong);
      border: 1px solid var(--line);
      border-radius: 10px;
      font: 12px/1.45 "Courier New", monospace;
      padding: 12px;
      max-height: 68vh;
      overflow: auto;
    }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
    .tiny {
      font-family: Arial, sans-serif;
      font-size: 12px;
      color: var(--muted);
      line-height: 1.5;
    }
    .list {
      margin: 0;
      padding-left: 18px;
      color: var(--muted);
    }
    @media (max-width: 1180px) {
      .layout { grid-template-columns: 1fr; }
      .row { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/wiz-rules") + `

    <h1>Wiz Rule Interface</h1>
    <p>Create runtime-style detection rules using Wiz-aligned concepts (event type, scope, severity, boolean conditions, regex/string operators).</p>

    <section class="layout">
      <article class="panel">
        <div class="panel-header">Rule Metadata</div>
        <div class="panel-body">
          <div class="field">
            <label for="ruleName">Rule Name</label>
            <input id="ruleName" type="text" placeholder="Detect suspicious outbound shell">
          </div>
          <div class="field">
            <label for="ruleDescription">Description</label>
            <textarea id="ruleDescription" placeholder="What this rule detects and why it matters."></textarea>
          </div>
          <div class="field">
            <label for="severity">Severity</label>
            <select id="severity">
              <option>Low</option>
              <option selected>Medium</option>
              <option>High</option>
              <option>Critical</option>
            </select>
          </div>
          <div class="field">
            <label for="scopeProjects">Project Scope (comma-separated)</label>
            <input id="scopeProjects" type="text" placeholder="prod-platform, payments, customer-data">
          </div>
          <div class="field">
            <label for="enabled">Rule State</label>
            <select id="enabled">
              <option value="true" selected>Enabled</option>
              <option value="false">Disabled</option>
            </select>
          </div>
          <div class="tiny">
            This page builds a rule payload draft. You can export JSON and use your Wiz API integration workflow for actual creation.
          </div>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Detection Logic</div>
        <div class="panel-body">
          <div class="field">
            <label for="eventType">Event Type</label>
            <select id="eventType">
              <option value="PROCESS_EXECUTION">Process Execution</option>
              <option value="NETWORK_CONNECTION">Network Connection</option>
              <option value="DNS_QUERY">DNS Query</option>
              <option value="NETWORK_LISTEN">Network Listen</option>
              <option value="ACTOR">Actor</option>
            </select>
          </div>

          <div class="field">
            <label>Conditions</label>
            <div id="conditions"></div>
            <button id="addConditionBtn" type="button">Add Condition</button>
          </div>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Rule JSON + References</div>
        <div class="panel-body">
          <pre id="ruleJson" class="mono">{}</pre>
          <div class="actions">
            <button id="refreshBtn" type="button">Refresh JSON</button>
            <button id="copyBtn" type="button">Copy JSON</button>
            <button id="downloadBtn" type="button">Download JSON</button>
          </div>
          <div class="tiny" style="margin-top:12px;">Wiz references used for this page design:</div>
          <ul class="list">
            <li><a href="https://www.wiz.io/blog/custom-runtime-rules-and-response-policies" target="_blank" rel="noopener noreferrer">Custom runtime rules and response policies (Wiz Blog)</a></li>
            <li><a href="https://www.wiz.io/solutions/runtime-sensor" target="_blank" rel="noopener noreferrer">Wiz Runtime Sensor solution page</a></li>
            <li><a href="https://www.wiz.io/blog/askai-text-to-security-graph-query" target="_blank" rel="noopener noreferrer">AskAI text-to-query blog (security graph query model context)</a></li>
          </ul>
        </div>
      </article>
    </section>
  </main>

  <script>
    const conditions = document.getElementById('conditions');
    const ruleJson = document.getElementById('ruleJson');
    const ruleName = document.getElementById('ruleName');
    const ruleDescription = document.getElementById('ruleDescription');
    const severity = document.getElementById('severity');
    const scopeProjects = document.getElementById('scopeProjects');
    const enabled = document.getElementById('enabled');
    const eventType = document.getElementById('eventType');
    const addConditionBtn = document.getElementById('addConditionBtn');
    const refreshBtn = document.getElementById('refreshBtn');
    const copyBtn = document.getElementById('copyBtn');
    const downloadBtn = document.getElementById('downloadBtn');

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function conditionRow() {
      const row = document.createElement('div');
      row.className = 'row';
      row.innerHTML =
        '<select class="logic"><option value="AND">AND</option><option value="OR">OR</option></select>' +
        '<input class="fieldName" type="text" placeholder="field e.g. process.name">' +
        '<select class="operator">' +
          '<option value="EQUALS">EQUALS</option>' +
          '<option value="NOT_EQUALS">NOT_EQUALS</option>' +
          '<option value="CONTAINS">CONTAINS</option>' +
          '<option value="NOT_CONTAINS">NOT_CONTAINS</option>' +
          '<option value="REGEX">REGEX</option>' +
        '</select>' +
        '<input class="value" type="text" placeholder="value">' +
        '<button class="removeBtn" type="button">Remove</button>';

      row.querySelector('.removeBtn').addEventListener('click', () => {
        row.remove();
        buildJSON();
      });
      row.querySelectorAll('input,select').forEach((el) => {
        el.addEventListener('input', buildJSON);
        el.addEventListener('change', buildJSON);
      });
      return row;
    }

    function parseConditions() {
      const rows = Array.from(conditions.querySelectorAll('.row'));
      return rows.map((row, idx) => ({
        join_with_previous: idx === 0 ? 'AND' : row.querySelector('.logic').value,
        field: row.querySelector('.fieldName').value.trim(),
        operator: row.querySelector('.operator').value,
        value: row.querySelector('.value').value.trim()
      })).filter((item) => item.field && item.value);
    }

    function buildJSON() {
      const payload = {
        provider: 'wiz-runtime-rule',
        name: ruleName.value.trim(),
        description: ruleDescription.value.trim(),
        severity: severity.value,
        enabled: enabled.value === 'true',
        scope: {
          projects: scopeProjects.value.split(',').map((v) => v.trim()).filter(Boolean)
        },
        event_type: eventType.value,
        conditions: parseConditions()
      };
      ruleJson.textContent = JSON.stringify(payload, null, 2);
      return payload;
    }

    addConditionBtn.addEventListener('click', () => {
      conditions.appendChild(conditionRow());
      buildJSON();
    });
    [ruleName, ruleDescription, severity, scopeProjects, enabled, eventType].forEach((el) => {
      el.addEventListener('input', buildJSON);
      el.addEventListener('change', buildJSON);
    });
    refreshBtn.addEventListener('click', buildJSON);
    copyBtn.addEventListener('click', async () => {
      await navigator.clipboard.writeText(ruleJson.textContent);
    });
    downloadBtn.addEventListener('click', () => {
      const blob = new Blob([ruleJson.textContent], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = (ruleName.value.trim() || 'wiz-rule') + '.json';
      a.click();
      URL.revokeObjectURL(url);
    });

    conditions.appendChild(conditionRow());
    buildJSON();
  </script>
</body>
</html>`

	c.Data(200, "text/html; charset=utf-8", []byte(html))
}
