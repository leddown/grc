package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

func exceptionsPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Exceptions · GRC</title>
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
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; }
    p { color: var(--muted); }
    .layout {
      display: grid;
      grid-template-columns: minmax(0, 1.2fr) minmax(320px, 0.8fr);
      gap: 18px;
      margin-top: 18px;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      overflow: hidden;
    }
    .panel-header {
      padding: 14px 16px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .panel-body { padding: 14px; }
    .toolbar {
      display: grid;
      grid-template-columns: minmax(220px, 1fr) 220px auto auto;
      gap: 10px;
      margin-bottom: 12px;
    }
    input[type="search"],
    select {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 12px 14px;
      font: inherit;
      background: var(--bg);
    }
    button {
      border: 0;
      border-radius: 999px;
      padding: 10px 14px;
      background: #dfc4b4;
      color: var(--ink);
      font: inherit;
      cursor: pointer;
    }
    .secondary {
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    .list {
      max-height: 70vh;
      overflow: auto;
      display: grid;
      gap: 8px;
    }
    .nfr-row {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: #000;
      color: #fff;
      padding: 10px 12px;
      display: grid;
      gap: 6px;
    }
    .nfr-row label {
      display: flex;
      align-items: flex-start;
      gap: 10px;
      cursor: pointer;
    }
    .nfr-row input[type="checkbox"] {
      margin-top: 2px;
    }
    .nfr-meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
    }
    .selected-list {
      display: grid;
      gap: 8px;
      max-height: 64vh;
      overflow: auto;
    }
    .cia-summary {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: var(--surface-strong);
      padding: 10px 12px;
      margin-bottom: 10px;
      font-family: Arial, sans-serif;
      font-size: 13px;
    }
    .selected-item {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: #000;
      color: #fff;
      padding: 10px 12px;
      display: flex;
      justify-content: space-between;
      gap: 10px;
      align-items: flex-start;
    }
    .selected-main {
      flex: 1;
      min-width: 0;
    }
    .nfr-detail-link {
      color: #fff;
      font-weight: bold;
      text-decoration: none;
    }
    .nfr-detail-link:hover {
      text-decoration: underline;
    }
    .linked-controls {
      display: grid;
      gap: 8px;
      margin-top: 10px;
    }
    .linked-controls-title {
      color: #c8c8c8;
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .linked-control-card {
      display: block;
      border: 1px solid #444;
      border-radius: 10px;
      background: var(--surface-strong);
      color: var(--ink);
      padding: 10px 12px;
      text-decoration: none;
    }
    .linked-control-card:hover {
      border-color: #b89d78;
      background: var(--hover);
    }
    .linked-control-meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
      margin-top: 4px;
    }
    .nfr-row .nfr-meta,
    .selected-item .nfr-meta {
      color: #c8c8c8;
    }
    .muted { color: var(--muted); }
    @media (max-width: 980px) {
      .layout { grid-template-columns: 1fr; }
      .toolbar { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/exceptions") + `

    <h1>Exceptions</h1>
    <p>Select one or more Security NFRs to define an exception set.</p>

    <section class="layout">
      <section class="panel">
        <div class="panel-header">Security NFR Catalog</div>
        <div class="panel-body">
          <div class="toolbar">
            <input id="searchInput" type="search" placeholder="Search NFR ID, summary, domain, or key">
            <select id="domainFilter" aria-label="Filter by Security NFR domain">
              <option value="">All Domains</option>
            </select>
            <button id="selectFilteredBtn" type="button">Select Filtered</button>
            <button id="clearFilteredBtn" class="secondary" type="button">Clear Filtered</button>
          </div>
          <div id="status" class="muted">Loading Security NFRs...</div>
          <div id="nfrList" class="list"></div>
        </div>
      </section>

      <aside class="panel">
        <div class="panel-header">Selected Security NFRs</div>
        <div class="panel-body">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;gap:10px;">
            <strong id="selectedCount">0 selected</strong>
            <div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end;">
              <button id="openDetailsBtn" class="secondary" type="button">Open Details</button>
              <button id="printDetailsBtn" type="button">Print Details</button>
              <button id="clearAllBtn" class="secondary" type="button">Clear All</button>
            </div>
          </div>
          <div id="ciaSummary" class="cia-summary muted">CIA coverage: loading...</div>
          <div id="threatSummary" class="cia-summary muted">Threat coverage: loading...</div>
          <div id="selectedList" class="selected-list">
            <div class="muted">No Security NFRs selected.</div>
          </div>
        </div>
      </aside>
    </section>
  </main>

  <script>
    const state = {
      items: [],
      selected: new Map(),
      query: '',
      domain: '',
      linksByNFRKey: new Map(),
      controlsByID: new Map(),
      dataLoaded: false
    };

    const searchInput = document.getElementById('searchInput');
    const domainFilter = document.getElementById('domainFilter');
    const selectFilteredBtn = document.getElementById('selectFilteredBtn');
    const clearFilteredBtn = document.getElementById('clearFilteredBtn');
    const openDetailsBtn = document.getElementById('openDetailsBtn');
    const printDetailsBtn = document.getElementById('printDetailsBtn');
    const clearAllBtn = document.getElementById('clearAllBtn');
    const status = document.getElementById('status');
    const nfrList = document.getElementById('nfrList');
    const selectedCount = document.getElementById('selectedCount');
    const ciaSummary = document.getElementById('ciaSummary');
    const threatSummary = document.getElementById('threatSummary');
    const selectedList = document.getElementById('selectedList');
    const threatDisplayOrder = [
      'Spoofing',
      'Tampering',
      'Repudiation',
      'Information Disclosure',
      'Denial of Service',
      'Elevation of Privilege'
    ];

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function filteredItems() {
      const q = state.query.trim().toUpperCase();
      const selectedDomain = state.domain.trim().toUpperCase();
      return state.items.filter((item) => {
        if (selectedDomain && String(item.domain || '').trim().toUpperCase() !== selectedDomain) {
          return false;
        }
        if (!q) return true;
        return [
          item.id,
          item.summary,
          item.domain,
          item.key,
          item.issue_type
        ].some((value) => String(value || '').toUpperCase().includes(q));
      });
    }

    function renderDomainOptions() {
      const domains = Array.from(new Set(
        state.items
          .map((item) => String(item.domain || '').trim())
          .filter((value) => value !== '')
      )).sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }));

      domainFilter.innerHTML = '<option value="">All Domains</option>' +
        domains.map((domain) => {
          const selected = state.domain === domain ? ' selected' : '';
          return '<option value="' + esc(domain) + '"' + selected + '>' + esc(domain) + '</option>';
        }).join('');
    }

    function computeCIASummary() {
      const selectedControlIDs = new Set();
      state.selected.forEach((item) => {
        const key = String(item.key || '').trim();
        if (!key) return;
        const linkedControls = state.linksByNFRKey.get(key);
        if (!linkedControls) return;
        linkedControls.forEach((controlID) => selectedControlIDs.add(controlID));
      });

      let confidentiality = 0;
      let integrity = 0;
      let availability = 0;
      selectedControlIDs.forEach((controlID) => {
        const control = state.controlsByID.get(controlID);
        if (!control) return;
        if (String(control.confidentiality || '').trim() !== '') confidentiality++;
        if (String(control.integrity || '').trim() !== '') integrity++;
        if (String(control.availability || '').trim() !== '') availability++;
      });

      return {
        controls: selectedControlIDs.size,
        confidentiality,
        integrity,
        availability
      };
    }

    function selectedControlIDs() {
      const ids = new Set();
      state.selected.forEach((item) => {
        const key = String(item.key || '').trim();
        if (!key) return;
        const linkedControls = state.linksByNFRKey.get(key);
        if (!linkedControls) return;
        linkedControls.forEach((controlID) => ids.add(controlID));
      });
      return ids;
    }

    function computeThreatSummary() {
      const counts = new Map();
      selectedControlIDs().forEach((controlID) => {
        const control = state.controlsByID.get(controlID);
        if (!control || !Array.isArray(control.threats)) return;
        new Set(
          control.threats
            .map((threat) => String(threat || '').trim())
            .filter((threat) => threat !== '')
        ).forEach((threat) => {
          counts.set(threat, (counts.get(threat) || 0) + 1);
        });
      });
      return counts;
    }

    function renderCIASummary() {
      if (!state.dataLoaded) {
        ciaSummary.textContent = 'CIA coverage: loading...';
        return;
      }
      const summary = computeCIASummary();
      ciaSummary.textContent =
        'Linked Controls: ' + summary.controls +
        ' | C: ' + summary.confidentiality +
        ' | I: ' + summary.integrity +
        ' | A: ' + summary.availability;
    }

    function renderThreatSummary() {
      if (!state.dataLoaded) {
        threatSummary.textContent = 'Threat coverage: loading...';
        return;
      }

      const counts = computeThreatSummary();
      if (!counts.size) {
        threatSummary.textContent = 'Threat coverage: no linked threat mappings';
        return;
      }

      const orderedThreats = [
        ...threatDisplayOrder.filter((threat) => counts.has(threat)),
        ...Array.from(counts.keys())
          .filter((threat) => !threatDisplayOrder.includes(threat))
          .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
      ];

      threatSummary.textContent = 'Threat coverage: ' + orderedThreats
        .map((threat) => threat + ': ' + counts.get(threat))
        .join(' | ');
    }

    function linkedControlsForNFR(item) {
      const key = String(item.key || '').trim();
      if (!key) return [];
      const linkedControls = state.linksByNFRKey.get(key);
      if (!linkedControls) return [];

      return Array.from(linkedControls)
        .map((controlID) => {
          const control = state.controlsByID.get(controlID);
          return {
            id: controlID,
            name: control && control.name ? control.name : '',
            confidentiality: control && control.confidentiality ? control.confidentiality : '',
            integrity: control && control.integrity ? control.integrity : '',
            availability: control && control.availability ? control.availability : ''
          };
        })
        .sort((a, b) => String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' }));
    }

    function formatCIAValue(value) {
      return String(value || '').trim() === '' ? '-' : 'TRUE';
    }

    function selectedItemsSorted() {
      const items = Array.from(state.selected.values());
      items.sort((a, b) => {
        const left = parseFloat(String(a.id || '').trim());
        const right = parseFloat(String(b.id || '').trim());
        if (!Number.isNaN(left) && !Number.isNaN(right) && left !== right) return left - right;
        return String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' });
      });
      return items;
    }

    function selectedDetailURL() {
      const params = new URLSearchParams();
      selectedItemsSorted().forEach((item) => {
        const key = String(item.key || '').trim();
        if (key) params.append('key', key);
      });
      const query = params.toString();
      return '/exceptions/detail' + (query ? '?' + query : '');
    }

    function renderSelected() {
      const items = selectedItemsSorted();

      selectedCount.textContent = items.length + ' selected';
      if (!items.length) {
        selectedList.innerHTML = '<div class="muted">No Security NFRs selected.</div>';
        return;
      }

      selectedList.innerHTML = items.map((item) => {
        const key = String(item.key || '');
        const linkedControls = linkedControlsForNFR(item);
        const linkedControlsHTML = linkedControls.length
          ? '<div class="linked-controls">' +
              '<div class="linked-controls-title">Linked Controls</div>' +
              linkedControls.map((control) => {
                return '<a class="linked-control-card" href="/controls/detail/' + encodeURIComponent(control.id) + '" target="_blank" rel="noopener noreferrer">' +
                  '<strong>' + esc(control.id || 'N/A') + '</strong> - ' + esc(control.name || 'No control name') +
                  '<div class="linked-control-meta">C: ' + esc(formatCIAValue(control.confidentiality)) + ' | I: ' + esc(formatCIAValue(control.integrity)) + ' | A: ' + esc(formatCIAValue(control.availability)) + '</div>' +
                '</a>';
              }).join('') +
            '</div>'
          : '<div class="linked-controls">' +
              '<div class="linked-controls-title">Linked Controls</div>' +
              '<div class="linked-control-card"><strong>None</strong><div class="linked-control-meta">No linked controls found for this Security NFR.</div></div>' +
            '</div>';
        return '<div class="selected-item">' +
          '<div class="selected-main">' +
            '<a class="nfr-detail-link" href="/security-nfrs/detail/' + encodeURIComponent(key) + '" target="_blank" rel="noopener noreferrer">' + esc(item.summary || 'No summary') + '</a>' +
            '<div class="nfr-meta">Domain: ' + esc(item.domain || 'N/A') + '</div>' +
            linkedControlsHTML +
          '</div>' +
          '<button class="secondary remove-selected" data-key="' + esc(key) + '" type="button">Remove</button>' +
        '</div>';
      }).join('');

      selectedList.querySelectorAll('.remove-selected').forEach((btn) => {
        btn.addEventListener('click', () => {
          const key = btn.getAttribute('data-key') || '';
          state.selected.delete(key);
          render();
        });
      });
    }

    function renderList() {
      const items = filteredItems();
      if (!items.length) {
        nfrList.innerHTML = '<div class="muted">No Security NFRs match the current filter.</div>';
        return;
      }

      nfrList.innerHTML = items.map((item) => {
        const key = String(item.key || '');
        const checked = state.selected.has(key) ? 'checked' : '';
        return '<div class="nfr-row">' +
          '<label>' +
            '<input class="nfr-check" type="checkbox" data-key="' + esc(key) + '" ' + checked + '>' +
            '<div>' +
              '<div><strong>' + esc(item.summary || 'No summary') + '</strong></div>' +
              '<div class="nfr-meta">Domain: ' + esc(item.domain || 'N/A') + ' | Type: ' + esc(item.issue_type || 'N/A') + ' | Key: ' + esc(key || 'N/A') + '</div>' +
            '</div>' +
          '</label>' +
        '</div>';
      }).join('');

      nfrList.querySelectorAll('.nfr-check').forEach((check) => {
        check.addEventListener('change', () => {
          const key = check.getAttribute('data-key') || '';
          const item = state.items.find((it) => String(it.key || '') === key);
          if (!item) return;
          if (check.checked) {
            state.selected.set(key, item);
          } else {
            state.selected.delete(key);
          }
          renderSelected();
          renderCIASummary();
          renderThreatSummary();
        });
      });
    }

    function render() {
      renderList();
      renderSelected();
      renderCIASummary();
      renderThreatSummary();
    }

    async function load() {
      status.textContent = 'Loading Security NFRs...';
      try {
        const [nfrResp, linksResp, controlsResp] = await Promise.all([
          fetch('/security-nfrs/data'),
          fetch('/security-nfrs/links/data'),
          fetch('/controls/data')
        ]);
        if (!nfrResp.ok) throw new Error('/security-nfrs/data HTTP ' + nfrResp.status);
        if (!linksResp.ok) throw new Error('/security-nfrs/links/data HTTP ' + linksResp.status);
        if (!controlsResp.ok) throw new Error('/controls/data HTTP ' + controlsResp.status);

        const [nfrItems, linkRows, controls] = await Promise.all([
          nfrResp.json(),
          linksResp.json(),
          controlsResp.json()
        ]);

        state.items = nfrItems;
        state.linksByNFRKey = new Map();
        linkRows.forEach((row) => {
          if (!row || !row.matched) return;
          const nfrKey = String(row.nfr_key || '').trim();
          const controlID = String(row.control_id || '').trim();
          if (!nfrKey || !controlID) return;
          let set = state.linksByNFRKey.get(nfrKey);
          if (!set) {
            set = new Set();
            state.linksByNFRKey.set(nfrKey, set);
          }
          set.add(controlID);
        });

        state.controlsByID = new Map();
        controls.forEach((control) => {
          const controlID = String(control.id || '').trim();
          if (controlID) state.controlsByID.set(controlID, control);
        });

        state.dataLoaded = true;
        renderDomainOptions();
        status.textContent = 'Loaded ' + state.items.length + ' Security NFRs.';
        render();
      } catch (error) {
        status.textContent = 'Failed to load Exceptions datasets: ' + error.message;
        state.dataLoaded = true;
        ciaSummary.textContent = 'CIA coverage: unavailable';
        threatSummary.textContent = 'Threat coverage: unavailable';
        nfrList.innerHTML = '<div class="muted">Could not load Security NFR dataset.</div>';
      }
    }

    searchInput.addEventListener('input', () => {
      state.query = searchInput.value;
      renderList();
    });

    domainFilter.addEventListener('change', () => {
      state.domain = domainFilter.value;
      renderList();
    });

    selectFilteredBtn.addEventListener('click', () => {
      filteredItems().forEach((item) => {
        const key = String(item.key || '');
        if (key) state.selected.set(key, item);
      });
      render();
    });

    clearFilteredBtn.addEventListener('click', () => {
      filteredItems().forEach((item) => {
        state.selected.delete(String(item.key || ''));
      });
      render();
    });

    clearAllBtn.addEventListener('click', () => {
      state.selected.clear();
      render();
    });

    openDetailsBtn.addEventListener('click', () => {
      if (!selectedItemsSorted().length) {
        window.alert('Select at least one Security NFR before opening the detail page.');
        return;
      }
      window.open(selectedDetailURL(), '_blank', 'noopener,noreferrer');
    });

    printDetailsBtn.addEventListener('click', () => {
      if (!selectedItemsSorted().length) {
        window.alert('Select at least one Security NFR before printing.');
        return;
      }
      window.open(selectedDetailURL() + '&print=1', '_blank', 'noopener,noreferrer');
    });

    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func exceptionsDetailPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Exception Detail · GRC</title>
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
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; }
    p { color: var(--muted); }
    .topbar {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      margin-bottom: 18px;
      flex-wrap: wrap;
    }
    .actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    button, .button-link {
      border: 0;
      border-radius: 999px;
      padding: 10px 14px;
      background: #dfc4b4;
      color: var(--ink);
      font: inherit;
      cursor: pointer;
      text-decoration: none;
      display: inline-flex;
      align-items: center;
      justify-content: center;
    }
    .button-link.secondary, button.secondary {
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    .summary-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 18px;
    }
    .card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      padding: 16px;
    }
    .card h2 {
      margin: 0 0 8px;
      font-size: 12px;
      color: var(--muted);
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .summary-value {
      font-family: Arial, sans-serif;
      font-size: 14px;
      line-height: 1.6;
    }
    .detail-list {
      display: grid;
      gap: 14px;
    }
    .detail-card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      padding: 18px;
    }
    .detail-card h3 {
      margin: 0 0 8px;
      font-size: 1.25rem;
    }
    .meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 13px;
      margin-bottom: 14px;
    }
    .section-title {
      margin-top: 16px;
      margin-bottom: 8px;
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .detail-text {
      white-space: pre-wrap;
      line-height: 1.55;
    }
    .linked-controls {
      display: grid;
      gap: 8px;
    }
    .linked-control-card {
      display: block;
      border: 1px solid var(--line);
      border-radius: 12px;
      background: var(--surface-strong);
      color: var(--ink);
      text-decoration: none;
      padding: 12px 14px;
    }
    .linked-control-card:hover {
      border-color: #b89d78;
      background: var(--hover);
    }
    .linked-control-meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
      margin-top: 4px;
    }
    .empty {
      border: 1px dashed var(--line);
      border-radius: 14px;
      padding: 18px;
      color: var(--muted);
      background: var(--surface-strong);
    }
    @media (max-width: 980px) {
      .summary-grid { grid-template-columns: 1fr; }
    }
    @media print {
      body { background: #fff; }
      main {
        max-width: none;
        margin: 0;
        border: 0;
        border-radius: 0;
        box-shadow: none;
        padding: 0;
        background: #fff;
      }
      .tabs, .actions { display: none; }
      .card, .detail-card, .linked-control-card { break-inside: avoid; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/exceptions/detail") + `

    <div class="topbar">
      <div>
        <h1>Exception Detail</h1>
        <p>Detailed view of the selected Security NFR exception set.</p>
      </div>
      <div class="actions">
        <a class="button-link secondary" href="/exceptions">Back to Exceptions</a>
        <button id="printBtn" type="button">Print</button>
      </div>
    </div>

    <div id="summary" class="summary-grid">
      <div class="card"><h2>Status</h2><div class="summary-value">Loading exception details...</div></div>
    </div>
    <div id="detailList" class="detail-list"></div>
  </main>

  <script>
    const summary = document.getElementById('summary');
    const detailList = document.getElementById('detailList');
    const printBtn = document.getElementById('printBtn');
    const threatDisplayOrder = [
      'Spoofing',
      'Tampering',
      'Repudiation',
      'Information Disclosure',
      'Denial of Service',
      'Elevation of Privilege'
    ];

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function textOrNA(value) {
      const text = String(value || '').trim();
      return text === '' ? 'N/A' : text;
    }

    function formatCIAValue(value) {
      return String(value || '').trim() === '' ? '-' : 'TRUE';
    }

    function formatThreatList(threats) {
      const items = Array.isArray(threats)
        ? threats.map((threat) => String(threat || '').trim()).filter((threat) => threat !== '')
        : [];
      return items.length ? items.join(', ') : 'None';
    }

    function selectedKeys() {
      return Array.from(new URLSearchParams(window.location.search).getAll('key'))
        .map((key) => String(key || '').trim())
        .filter((key) => key !== '');
    }

    function orderedThreatSummaryEntries(controls) {
      const counts = new Map();
      controls.forEach((control) => {
        if (!control || !Array.isArray(control.threats)) return;
        new Set(
          control.threats
            .map((threat) => String(threat || '').trim())
            .filter((threat) => threat !== '')
        ).forEach((threat) => {
          counts.set(threat, (counts.get(threat) || 0) + 1);
        });
      });
      if (!counts.size) return [];

      const orderedThreats = [
        ...threatDisplayOrder.filter((threat) => counts.has(threat)),
        ...Array.from(counts.keys())
          .filter((threat) => !threatDisplayOrder.includes(threat))
          .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
      ];
      return orderedThreats.map((threat) => ({ threat, count: counts.get(threat) }));
    }

    function linkedControlsForKey(key, linksByNFRKey, controlsByID) {
      const linkedControls = linksByNFRKey.get(key);
      if (!linkedControls) return [];
      return Array.from(linkedControls)
        .map((controlID) => {
          const control = controlsByID.get(controlID);
          return {
            id: controlID,
            name: control && control.name ? control.name : '',
            confidentiality: control && control.confidentiality ? control.confidentiality : '',
            integrity: control && control.integrity ? control.integrity : '',
            availability: control && control.availability ? control.availability : '',
            threats: control && Array.isArray(control.threats) ? control.threats : []
          };
        })
        .sort((a, b) => String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' }));
    }

    async function load() {
      const keys = selectedKeys();
      if (!keys.length) {
        summary.innerHTML = '<div class="card"><h2>Status</h2><div class="summary-value">No Security NFR keys were provided.</div></div>';
        detailList.innerHTML = '<div class="empty">Open this page from <a href="/exceptions">/exceptions</a> after selecting one or more Security NFRs.</div>';
        return;
      }

      try {
        const [nfrResp, linksResp, controlsResp] = await Promise.all([
          fetch('/security-nfrs/data'),
          fetch('/security-nfrs/links/data'),
          fetch('/controls/data')
        ]);
        if (!nfrResp.ok) throw new Error('/security-nfrs/data HTTP ' + nfrResp.status);
        if (!linksResp.ok) throw new Error('/security-nfrs/links/data HTTP ' + linksResp.status);
        if (!controlsResp.ok) throw new Error('/controls/data HTTP ' + controlsResp.status);

        const [nfrItems, linkRows, controls] = await Promise.all([
          nfrResp.json(),
          linksResp.json(),
          controlsResp.json()
        ]);

        const itemByKey = new Map();
        (nfrItems || []).forEach((item) => {
          const key = String(item.key || '').trim();
          if (key) itemByKey.set(key, item);
        });

        const linksByNFRKey = new Map();
        (linkRows || []).forEach((row) => {
          if (!row || !row.matched) return;
          const nfrKey = String(row.nfr_key || '').trim();
          const controlID = String(row.control_id || '').trim();
          if (!nfrKey || !controlID) return;
          let set = linksByNFRKey.get(nfrKey);
          if (!set) {
            set = new Set();
            linksByNFRKey.set(nfrKey, set);
          }
          set.add(controlID);
        });

        const controlsByID = new Map();
        (controls || []).forEach((control) => {
          const controlID = String(control.id || '').trim();
          if (controlID) controlsByID.set(controlID, control);
        });

        const items = keys
          .map((key) => itemByKey.get(key))
          .filter((item) => !!item)
          .sort((a, b) => String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' }));

        const selectedControlIDs = new Set();
        items.forEach((item) => {
          linkedControlsForKey(String(item.key || '').trim(), linksByNFRKey, controlsByID)
            .forEach((control) => selectedControlIDs.add(String(control.id || '').trim()));
        });

        const selectedControls = Array.from(selectedControlIDs)
          .map((controlID) => controlsByID.get(controlID))
          .filter((control) => !!control);

        const cia = {
          controls: selectedControlIDs.size,
          confidentiality: selectedControls.filter((control) => String(control.confidentiality || '').trim() !== '').length,
          integrity: selectedControls.filter((control) => String(control.integrity || '').trim() !== '').length,
          availability: selectedControls.filter((control) => String(control.availability || '').trim() !== '').length
        };
        const threats = orderedThreatSummaryEntries(selectedControls);

        summary.innerHTML =
          '<div class="card"><h2>Selection</h2><div class="summary-value">Selected Security NFRs: ' + esc(String(items.length)) + '<br>Linked Controls: ' + esc(String(cia.controls)) + '</div></div>' +
          '<div class="card"><h2>CIA Coverage</h2><div class="summary-value">C: ' + esc(String(cia.confidentiality)) + '<br>I: ' + esc(String(cia.integrity)) + '<br>A: ' + esc(String(cia.availability)) + '</div></div>' +
          '<div class="card"><h2>Threat Coverage</h2><div class="summary-value">' + esc(threats.length ? threats.map((entry) => entry.threat + ': ' + entry.count).join(' | ') : 'No linked threat mappings') + '</div></div>' +
          '<div class="card"><h2>Status</h2><div class="summary-value">Loaded ' + esc(String(items.length)) + ' selected Security NFR detail entries.</div></div>';

        if (!items.length) {
          detailList.innerHTML = '<div class="empty">None of the requested Security NFR keys were found.</div>';
          return;
        }

        detailList.innerHTML = items.map((item) => {
          const key = String(item.key || '').trim();
          const linkedControls = linkedControlsForKey(key, linksByNFRKey, controlsByID);
          const linkedControlsHTML = linkedControls.length
            ? linkedControls.map((control) => {
                return '<a class="linked-control-card" href="/controls/detail/' + encodeURIComponent(control.id) + '" target="_blank" rel="noopener noreferrer">' +
                  '<strong>' + esc(control.id || 'N/A') + '</strong> - ' + esc(control.name || 'No control name') +
                  '<div class="linked-control-meta">C: ' + esc(formatCIAValue(control.confidentiality)) + ' | I: ' + esc(formatCIAValue(control.integrity)) + ' | A: ' + esc(formatCIAValue(control.availability)) + '</div>' +
                  '<div class="linked-control-meta">Threats: ' + esc(formatThreatList(control.threats)) + '</div>' +
                '</a>';
              }).join('')
            : '<div class="empty">No linked controls found for this Security NFR.</div>';

          return '<article class="detail-card">' +
            '<h3><a href="/security-nfrs/detail/' + encodeURIComponent(key) + '" target="_blank" rel="noopener noreferrer">' + esc(item.summary || 'No summary') + '</a></h3>' +
            '<div class="meta">Domain: ' + esc(item.domain || 'N/A') + ' | Type: ' + esc(item.issue_type || 'N/A') + ' | Key: ' + esc(key || 'N/A') + '</div>' +
            '<div class="section-title">Linked Controls</div>' +
            '<div class="linked-controls">' + linkedControlsHTML + '</div>' +
            '<div class="section-title">NIST Mapping</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.nist_mapping)) + '</div>' +
            '<div class="section-title">Description</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.description)) + '</div>' +
            '<div class="section-title">Additional Details</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.additional_details)) + '</div>' +
            '<div class="section-title">Implementation</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.implementation)) + '</div>' +
          '</article>';
        }).join('');

        if (new URLSearchParams(window.location.search).get('print') === '1') {
          window.setTimeout(() => window.print(), 150);
        }
      } catch (error) {
        summary.innerHTML = '<div class="card"><h2>Status</h2><div class="summary-value">Failed to load exception details: ' + esc(error.message) + '</div></div>';
        detailList.innerHTML = '<div class="empty">The detail page could not load the required datasets.</div>';
      }
    }

    printBtn.addEventListener('click', () => {
      window.print();
    });

    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
