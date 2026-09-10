package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

func jsonViewPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>JSON_view · GRC</title>
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
    .toolbar {
      display: grid;
      grid-template-columns: minmax(220px, 320px) minmax(220px, 420px) auto;
      gap: 12px;
      margin: 22px 0 18px;
    }
    .field { display: grid; gap: 6px; }
    .field label { color: var(--muted); font-family: Arial, sans-serif; font-size: 12px; letter-spacing: 0.03em; text-transform: uppercase; }
    select, button {
      padding: 12px 14px;
      border-radius: 12px;
      border: 1px solid var(--line);
      background: var(--bg);
      color: var(--ink);
      font: inherit;
    }
    button { background: #dfc4b4; cursor: pointer; align-self: end; }
    button:hover { background: #d1b09d; }
    .status { color: var(--muted); margin-bottom: 14px; }
    pre {
      margin: 0;
      padding: 20px;
      border-radius: 18px;
      border: 1px solid rgba(215,206,191,0.85);
      background: var(--surface-strong);
      overflow: auto;
      font: 13px/1.6 "Courier New", monospace;
      color: #18202c;
      min-height: 60vh;
    }
    @media (max-width: 980px) {
      .toolbar { grid-template-columns: 1fr; }
      button { width: fit-content; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/JSON_view") + `
    <h1>JSON_view</h1>
    <p>Use one output panel for Security NFR JSON, Family JSON, or JSON documents saved into SQLite.</p>

    <div class="toolbar">
      <div class="field">
        <label for="sourceSelect">Dataset</label>
        <select id="sourceSelect">
          <option value="security-nfr">Security NFR JSON</option>
          <option value="family-json">Family JSON</option>
          <option value="stored-json">Stored SQLite JSON</option>
        </select>
      </div>
      <div class="field">
        <label id="filterLabel" for="filterSelect">Domain</label>
        <select id="filterSelect"></select>
      </div>
      <button id="loadBtn" type="button">Load JSON</button>
    </div>

    <div id="status" class="status">Loading selectors...</div>
    <pre id="jsonOutput">{}</pre>
  </main>

  <script>
    const sourceSelect = document.getElementById('sourceSelect');
    const filterLabel = document.getElementById('filterLabel');
    const filterSelect = document.getElementById('filterSelect');
    const loadBtn = document.getElementById('loadBtn');
    const status = document.getElementById('status');
    const jsonOutput = document.getElementById('jsonOutput');

    function esc(value) {
      return String(value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    async function loadFilterOptions() {
      const source = sourceSelect.value;
      if (source === 'security-nfr') {
        filterLabel.textContent = 'Domain';
        const response = await fetch('/security-nfrs/domains');
        if (!response.ok) throw new Error('HTTP ' + response.status);
        const items = await response.json();
        const options = ['<option value="">All Domains</option>'];
        items.forEach((domain) => {
          const safe = esc(domain);
          options.push('<option value="' + safe + '">' + safe + '</option>');
        });
        filterSelect.innerHTML = options.join('');
        return;
      }

      if (source === 'stored-json') {
        filterLabel.textContent = 'Saved JSON';
        const response = await fetch('/stored-json');
        if (!response.ok) throw new Error('HTTP ' + response.status);
        const data = await response.json();
        const items = data.items || [];
        if (!items.length) {
          filterSelect.innerHTML = '';
          return;
        }
        filterSelect.innerHTML = items.map((item) => {
          const id = esc(item.id);
          const name = esc(item.name || 'Stored JSON');
          const source = esc(item.source || 'unknown');
          const created = esc(item.created_at || '');
          return '<option value="' + id + '">' + name + ' - ' + source + (created ? ' - ' + created : '') + '</option>';
        }).join('');
        return;
      }

      filterLabel.textContent = 'Family';
      const response = await fetch('/controls/family-visibility/data');
      if (!response.ok) throw new Error('HTTP ' + response.status);
      const items = await response.json();
      const enabled = items.filter((item) => item.enabled);
      if (!enabled.length) {
        filterSelect.innerHTML = '';
        return;
      }
      filterSelect.innerHTML = enabled.map((item) => {
        const family = esc(item.family);
        const name = esc(item.name || 'Unknown family');
        return '<option value="' + family + '">' + family + ' - ' + name + '</option>';
      }).join('');
    }

    async function loadJSON() {
      const source = sourceSelect.value;

      if (source === 'security-nfr') {
        const domain = filterSelect.value || '';
        status.textContent = domain ? ('Loading Security NFR JSON for domain: ' + domain + '...') : 'Loading Security NFR JSON for all domains...';
        try {
          const query = domain ? ('?domain=' + encodeURIComponent(domain)) : '';
          const response = await fetch('/security-nfrs/json/data' + query);
          if (!response.ok) throw new Error('HTTP ' + response.status);
          const data = await response.json();
          jsonOutput.textContent = JSON.stringify(data, null, 2);
          status.textContent = 'Loaded ' + Object.keys(data).length + ' security NFR entries.';
        } catch (error) {
          jsonOutput.textContent = '{}';
          status.textContent = 'Failed to load Security NFR JSON: ' + error.message;
        }
        return;
      }

      if (source === 'stored-json') {
        const id = filterSelect.value || '';
        if (!id) {
          jsonOutput.textContent = '{}';
          status.textContent = 'No stored JSON documents are available.';
          return;
        }
        status.textContent = 'Loading stored JSON document...';
        try {
          const response = await fetch('/stored-json/' + encodeURIComponent(id));
          if (!response.ok) throw new Error('HTTP ' + response.status);
          const data = await response.json();
          jsonOutput.textContent = JSON.stringify(data.content || {}, null, 2);
          status.textContent = 'Loaded stored JSON: ' + (data.name || ('#' + id)) + '.';
        } catch (error) {
          jsonOutput.textContent = '{}';
          status.textContent = 'Failed to load stored JSON: ' + error.message;
        }
        return;
      }

      const family = filterSelect.value || '';
      if (!family) {
        jsonOutput.textContent = '{}';
        status.textContent = 'No visible family is enabled.';
        return;
      }

      status.textContent = 'Loading Family JSON for ' + family + '...';
      try {
        const response = await fetch('/controls/family-json/data?family=' + encodeURIComponent(family));
        if (!response.ok) throw new Error('HTTP ' + response.status);
        const data = await response.json();
        jsonOutput.textContent = JSON.stringify(data, null, 2);
        status.textContent = 'Loaded ' + Object.keys(data).length + ' controls for family ' + family + '.';
      } catch (error) {
        jsonOutput.textContent = '{}';
        status.textContent = 'Failed to load Family JSON: ' + error.message;
      }
    }

    sourceSelect.addEventListener('change', async () => {
      status.textContent = 'Loading selectors...';
      try {
        await loadFilterOptions();
        await loadJSON();
      } catch (error) {
        jsonOutput.textContent = '{}';
        status.textContent = 'Failed to load selectors: ' + error.message;
      }
    });
    filterSelect.addEventListener('change', loadJSON);
    loadBtn.addEventListener('click', loadJSON);

    (async () => {
      try {
        await loadFilterOptions();
        await loadJSON();
      } catch (error) {
        jsonOutput.textContent = '{}';
        status.textContent = 'Failed to initialize JSON_view: ' + error.message;
      }
    })();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
