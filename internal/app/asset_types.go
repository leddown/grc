package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

func assetTypesPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Asset Types · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.92);
      --accent: #efe6d6;
      --accent-strong: #dfc4b4;
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
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.5rem); line-height: 0.95; letter-spacing: -0.03em; }
    p { color: var(--muted); }
    .lede {
      max-width: 860px;
      margin-bottom: 18px;
      font-size: 17px;
      line-height: 1.5;
    }
    .layout {
      display: grid;
      grid-template-columns: minmax(250px, 0.42fr) minmax(0, 1.58fr);
      gap: 20px;
      margin-top: 22px;
      align-items: start;
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
    .panel-body { padding: 18px 16px; }
    .section-note {
      margin: 0 0 16px;
      color: var(--muted);
      font-size: 15px;
      line-height: 1.5;
    }
    .field { margin-top: 16px; }
    .field label {
      display: block;
      margin-bottom: 6px;
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.08em;
      color: var(--muted);
      text-transform: uppercase;
      font-family: Arial, sans-serif;
    }
    input[type="text"], textarea, select {
      width: 100%;
      padding: 12px 14px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: var(--bg);
      font: inherit;
      color: var(--ink);
    }
    select {
      appearance: none;
      padding-right: 40px;
      background-image:
        linear-gradient(45deg, transparent 50%, var(--muted) 50%),
        linear-gradient(135deg, var(--muted) 50%, transparent 50%);
      background-position:
        calc(100% - 18px) calc(50% - 3px),
        calc(100% - 12px) calc(50% - 3px);
      background-size: 6px 6px, 6px 6px;
      background-repeat: no-repeat;
    }
    textarea {
      min-height: 108px;
      resize: vertical;
    }
    .actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      margin-top: 18px;
    }
    button {
      padding: 12px 16px;
      border: 0;
      border-radius: 999px;
      background: var(--accent-strong);
      color: var(--ink);
      font: inherit;
      cursor: pointer;
    }
    button:hover { background: #d1b09d; }
    .secondary {
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    .secondary:hover { background: var(--hover); }
    .summary {
      display: grid;
      gap: 14px;
    }
    .view-toggle {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-bottom: 14px;
    }
    .toggle-btn {
      padding: 10px 14px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--accent);
      color: var(--ink);
      font: inherit;
      cursor: pointer;
    }
    .toggle-btn.active {
      background: #e1d0b7;
      border-color: #b89d78;
      font-weight: 700;
    }
    .summary-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 14px;
    }
    .summary-card {
      padding: 14px;
      border-radius: 14px;
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    .summary-card h3 {
      margin: 0 0 8px;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: var(--muted);
      font-family: Arial, sans-serif;
    }
    .summary-value {
      font-size: 24px;
      font-weight: 700;
      line-height: 1.1;
    }
    .status {
      color: var(--muted);
      line-height: 1.5;
    }
    .status-card {
      padding: 14px;
      border-radius: 14px;
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    .control-list {
      display: grid;
      gap: 10px;
      max-height: 480px;
      overflow: auto;
    }
    .control-item {
      padding: 12px;
      border-radius: 12px;
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    .control-item code {
      font-weight: 700;
      color: var(--ink);
    }
    .control-item div:last-child {
      color: var(--muted);
      margin-top: 4px;
      font-size: 14px;
    }
    .nfr-item {
      padding: 12px;
      border-radius: 12px;
      background: var(--surface-strong);
      border: 1px solid var(--line);
      display: grid;
      gap: 6px;
    }
    .nfr-link {
      display: block;
      color: inherit;
      text-decoration: none;
    }
    .nfr-link:hover .nfr-meta:first-of-type {
      text-decoration: underline;
    }
    .nfr-meta {
      color: var(--muted);
      font-size: 13px;
      font-family: Arial, sans-serif;
    }
    @media (max-width: 920px) {
      .layout { grid-template-columns: 1fr; }
      .summary-grid { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
	    ` + pageui.Nav("/asset-types") + `

    <h1>Asset Type Selector</h1>
    <p class="lede">Select one of the six supported asset types: <code>TIER 1</code>, <code>TIER 2</code>, <code>TIER 3</code>, <code>TIER 4</code>, <code>TIER 0</code>, or <code>Other</code>. The page resolves the applicable baseline and returns the matching controls in a standard side-by-side review layout.</p>

    <section class="layout">
      <section class="panel">
        <div class="panel-header">Input</div>
        <div class="panel-body">
          <p class="section-note">Choose the asset type, optionally add notes, and apply the selection to see the required baseline controls.</p>
          <form id="assetForm">
            <div class="field" style="margin-top:0;">
              <label for="assetType">Asset Type</label>
              <select id="assetType" name="assetType">
                <option value="">Select asset type</option>
                <option value="TIER 1">TIER 1</option>
                <option value="TIER 2">TIER 2</option>
                <option value="TIER 3">TIER 3</option>
                <option value="TIER 4">TIER 4</option>
                <option value="TIER 0">TIER 0</option>
                <option value="Other">Other</option>
              </select>
            </div>

            <div class="field">
              <label for="otherValue">Other Label</label>
              <input id="otherValue" type="text" placeholder="Required only when Other is selected" disabled>
            </div>

            <div class="field">
              <label for="notes">Notes</label>
              <textarea id="notes" placeholder="Optional context about the selected asset type"></textarea>
            </div>

            <div class="actions">
              <button type="submit">Apply Selection</button>
              <button id="showNFRBtn" class="secondary" type="button">Show Security NFRs</button>
              <button id="resetBtn" class="secondary" type="button">Reset</button>
            </div>
          </form>
        </div>
      </section>

      <aside class="panel">
        <div class="panel-header">Current Selection</div>
        <div class="panel-body">
          <div class="view-toggle">
            <button id="selectionViewBtn" class="toggle-btn active" type="button">Current Selection</button>
            <button id="nfrViewBtn" class="toggle-btn" type="button">Linked Security NFRs</button>
          </div>

          <div id="selectionView" class="summary">
              <div class="summary-grid">
                <div class="summary-card">
                  <h3>Asset Type</h3>
                  <div id="selectedType" class="summary-value">None</div>
                </div>
                <div class="summary-card">
                  <h3>Resolved Value</h3>
                  <div id="resolvedValue" class="summary-value">None</div>
                </div>
              </div>
              <div class="summary-card">
                <h3>Notes</h3>
                <div id="selectedNotes" class="status">No notes entered.</div>
              </div>
              <div class="summary-card">
                <h3 id="controlSummaryLabel">Applicable Baseline Controls</h3>
                <div id="controlCount" class="summary-value">0</div>
              </div>
              <div class="status-card">
                <div id="status" class="status">Choose an asset type and apply it.</div>
              </div>
              <div class="summary-card">
                <h3>Returned Controls</h3>
                <div id="controlResults" class="control-list">
                  <div class="status">No controls loaded.</div>
                </div>
              </div>
          </div>

          <div id="nfrView" class="summary" style="display:none;">
            <div class="summary-card">
              <h3 id="nfrSummaryLabel">Linked Security NFRs</h3>
              <div id="nfrCount" class="summary-value">0</div>
            </div>
            <div class="status-card">
              <div id="nfrStatus" class="status">Select an asset type and apply it to load linked Security NFRs.</div>
            </div>
            <div class="summary-card">
              <h3>Linked NFR Results</h3>
              <div id="nfrResults" class="control-list">
                <div class="status">No Security NFRs loaded.</div>
              </div>
            </div>
          </div>
        </div>
      </aside>
    </section>
  </main>

  <script>
    const form = document.getElementById('assetForm');
    const assetType = document.getElementById('assetType');
    const showNFRBtn = document.getElementById('showNFRBtn');
    const resetBtn = document.getElementById('resetBtn');
    const otherValue = document.getElementById('otherValue');
    const notes = document.getElementById('notes');
    const selectedType = document.getElementById('selectedType');
    const resolvedValue = document.getElementById('resolvedValue');
    const selectedNotes = document.getElementById('selectedNotes');
    const controlCount = document.getElementById('controlCount');
    const controlResults = document.getElementById('controlResults');
    const controlSummaryLabel = document.getElementById('controlSummaryLabel');
    const status = document.getElementById('status');
    const selectionView = document.getElementById('selectionView');
    const nfrView = document.getElementById('nfrView');
    const selectionViewBtn = document.getElementById('selectionViewBtn');
    const nfrViewBtn = document.getElementById('nfrViewBtn');
    const nfrCount = document.getElementById('nfrCount');
    const nfrResults = document.getElementById('nfrResults');
    const nfrStatus = document.getElementById('nfrStatus');
    const nfrSummaryLabel = document.getElementById('nfrSummaryLabel');
    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function setView(view) {
      const showSelection = view === 'selection';
      selectionView.style.display = showSelection ? '' : 'none';
      nfrView.style.display = showSelection ? 'none' : '';
      selectionViewBtn.classList.toggle('active', showSelection);
      nfrViewBtn.classList.toggle('active', !showSelection);
    }

    function currentType() {
      return assetType.value;
    }

    function syncOtherState() {
      const isOther = currentType() === 'Other';
      otherValue.disabled = !isOther;
      if (!isOther) otherValue.value = '';
    }

    function baselineForTier(type) {
      if (type === 'TIER 1' || type === 'TIER 2') return 'High';
      if (type === 'TIER 3') return 'Moderate';
      if (type === 'TIER 4') return 'Low';
      return '';
    }

    function renderControls(items, baselineLabel) {
      controlCount.textContent = String(items.length);
      controlSummaryLabel.textContent = baselineLabel ? baselineLabel + ' Baseline Controls' : 'Applicable Baseline Controls';
      if (!items.length) {
        controlResults.innerHTML = '<div class="status">' + (baselineLabel ? ('No ' + baselineLabel.toUpperCase() + '-baseline controls returned for this asset type.') : 'No controls loaded.') + '</div>';
        return;
      }

      controlResults.innerHTML = items.map((item) => {
        return '<div class="control-item">' +
          '<div><code>' + esc(item.id) + '</code></div>' +
          '<div>' + esc(item.name || 'Unnamed control') + '</div>' +
        '</div>';
      }).join('');
    }

    async function loadBaselineControls(baseline) {
      const response = await fetch('/controls/data?baseline=' + encodeURIComponent(baseline));
      if (!response.ok) throw new Error('HTTP ' + response.status);
      return await response.json();
    }

    async function loadLinkedNFRs(controls, baseline, type) {
      nfrSummaryLabel.textContent = (baseline ? (baseline + ' Baseline Linked Security NFRs') : 'Linked Security NFRs');
      if (!baseline || !controls.length) {
        nfrCount.textContent = '0';
        nfrResults.innerHTML = '<div class="status">No Security NFRs available because no baseline controls were returned.</div>';
        return;
      }

      const controlSet = new Set(controls.map((item) => String(item.id || '').trim()).filter(Boolean));
      nfrStatus.textContent = 'Loading Security NFR links for ' + baseline.toUpperCase() + ' baseline...';
      try {
        const linksResponse = await fetch('/security-nfrs/links/data');
        if (!linksResponse.ok) throw new Error('Links HTTP ' + linksResponse.status);
        const rows = await linksResponse.json();
        const byNFR = new Map();

        rows.forEach((row) => {
          const controlID = String(row.control_id || '').trim();
          if (!row.matched || !controlSet.has(controlID)) return;

          const dedupeKey = String(row.nfr_key || row.nfr_id || row.nfr_summary);
          const existing = byNFR.get(dedupeKey) || {
            key: row.nfr_key || '',
            id: row.nfr_id || 'N/A',
            summary: row.nfr_summary || 'No summary',
            domain: row.nfr_domain || 'No domain',
            controls: new Set()
          };
          existing.controls.add(controlID);
          byNFR.set(dedupeKey, existing);
        });

        const items = Array.from(byNFR.values());
        items.sort((a, b) => {
          const an = parseFloat(String(a.id));
          const bn = parseFloat(String(b.id));
          if (!Number.isNaN(an) && !Number.isNaN(bn) && an !== bn) return an - bn;
          return String(a.id).localeCompare(String(b.id), undefined, { numeric: true, sensitivity: 'base' });
        });

        nfrCount.textContent = String(items.length);
        if (!items.length) {
          nfrResults.innerHTML = '<div class="status">No linked Security NFRs found for the controls returned by this baseline.</div>';
          nfrStatus.textContent = 'No linked Security NFRs were found for ' + type + '.';
          return;
        }

        nfrResults.innerHTML = items.map((item) => {
          const controlsLinked = Array.from(item.controls).sort((a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' }));
          const rowKey = String(item.key || '').trim();
          const detailURL = rowKey ? '/security-nfrs/detail/' + encodeURIComponent(rowKey) : '';
          const itemBody =
            '<div>' + esc(item.summary) + '</div>' +
            '<div class="nfr-meta">Domain: ' + esc(item.domain) + '</div>' +
            '<div class="nfr-meta">Linked Controls: ' + esc(controlsLinked.join(', ')) + '</div>';
          return '<div class="nfr-item">' +
            (detailURL
              ? '<a class="nfr-link" href="' + detailURL + '" target="_blank" rel="noopener noreferrer">' + itemBody + '</a>'
              : itemBody) +
          '</div>';
        }).join('');
        nfrStatus.textContent = 'Loaded linked Security NFRs for ' + type + ' (' + baseline.toUpperCase() + ' baseline).';
      } catch (error) {
        nfrCount.textContent = '0';
        nfrResults.innerHTML = '<div class="status">Failed to load linked Security NFRs.</div>';
        nfrStatus.textContent = 'Failed to load linked Security NFRs: ' + error.message;
      }
    }

    assetType.addEventListener('change', syncOtherState);
    selectionViewBtn.addEventListener('click', () => setView('selection'));
    nfrViewBtn.addEventListener('click', () => setView('nfr'));
    showNFRBtn.addEventListener('click', () => setView('nfr'));

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const type = currentType();
      if (!type) {
        status.textContent = 'Select an asset type first.';
        return;
      }

      if (type === 'Other' && !otherValue.value.trim()) {
        status.textContent = 'Enter a value for Other before applying.';
        otherValue.focus();
        return;
      }

      selectedType.textContent = type;
      resolvedValue.textContent = type === 'Other' ? otherValue.value.trim() : type;
      selectedNotes.textContent = notes.value.trim() || 'No notes entered.';
      status.textContent = 'Asset type captured locally on this page.';

      const baseline = baselineForTier(type);
      if (baseline) {
        status.textContent = 'Loading ' + baseline.toUpperCase() + '-baseline controls...';
        try {
          const controls = await loadBaselineControls(baseline);
          renderControls(controls, baseline);
          await loadLinkedNFRs(controls, baseline, type);
          status.textContent = 'Loaded ' + baseline.toUpperCase() + '-baseline controls required for ' + type + '.';
        } catch (error) {
          renderControls([], baseline);
          await loadLinkedNFRs([], baseline, type);
          status.textContent = 'Failed to load ' + baseline.toUpperCase() + '-baseline controls: ' + error.message;
        }
        return;
      }

      renderControls([], '');
      await loadLinkedNFRs([], '', type);
    });

    resetBtn.addEventListener('click', () => {
      form.reset();
      syncOtherState();
      selectedType.textContent = 'None';
      resolvedValue.textContent = 'None';
      selectedNotes.textContent = 'No notes entered.';
      renderControls([], '');
      nfrSummaryLabel.textContent = 'Linked Security NFRs';
      nfrCount.textContent = '0';
      nfrResults.innerHTML = '<div class="status">No Security NFRs loaded.</div>';
      nfrStatus.textContent = 'Select an asset type and apply it to load linked Security NFRs.';
      status.textContent = 'Choose an asset type and apply it.';
      setView('selection');
    });

    renderControls([], '');
    setView('selection');
    syncOtherState();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
