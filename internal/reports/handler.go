package reports

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"carelockconsulting/internal/apiutil"
	"carelockconsulting/internal/pageui"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) UnlinkedControlsData(c *gin.Context) {
	controlType := strings.TrimSpace(c.DefaultQuery("type", "all"))
	mode := strings.TrimSpace(strings.ToLower(c.DefaultQuery("mode", "unlinked")))
	page := apiutil.ParsePagination(c)
	reportData, err := h.service.ReportData(controlType, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate report"})
		return
	}
	items := reportData.Items
	total := len(items)
	if page.Enabled {
		items, total = apiutil.PaginateSlice(items, page)
	}

	c.JSON(http.StatusOK, gin.H{
		"count":          len(items),
		"unlinked_count": reportData.UnlinkedCount,
		"linked_count":   reportData.LinkedCount,
		"total_controls": reportData.TotalControls,
		"type":           normalizeType(controlType),
		"mode":           normalizeMode(mode),
		"items":          items,
		"total":          total,
		"page":           page.Page,
		"per_page":       page.PerPage,
	})
}

func normalizeType(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "control":
		return "Control"
	case "control enhancement":
		return "Control Enhancement"
	default:
		return "All"
	}
}

func normalizeMode(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "linked":
		return "Linked"
	case "unlinked":
		return "Unlinked"
	}
	return "All"
}

func (h *Handler) Page(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Reports · CareLock Consulting</title>
  <style>
    :root {
      color-scheme: light;
      --panel: rgba(42,42,42,0.94);
      --ink: #f2eee6;
      --muted: #d0c7b8;
      --line: #5a544c;
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
    h1 { margin: 0 0 8px; font-size: clamp(1.7rem, 3vw, 2.5rem); line-height: 1; letter-spacing: -0.03em; }
    p { color: var(--muted); font-size: 14px; }
    .report-card {
      border: 1px solid #444;
      border-radius: 18px;
      background: #1f1f1f;
      padding: 16px;
      margin-top: 18px;
    }
    .report-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      flex-wrap: wrap;
      margin-bottom: 12px;
    }
    .report-title {
      margin: 0;
      font-family: Arial, sans-serif;
      font-size: 13px;
      text-transform: uppercase;
      letter-spacing: 0.07em;
      color: var(--muted);
    }
    .count {
      font-size: 28px;
      font-weight: 700;
      color: #dfc4b4;
      line-height: 1;
    }
    .metrics {
      display: flex;
      gap: 18px;
      flex-wrap: wrap;
      align-items: baseline;
    }
    .metric-label {
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.06em;
      font-size: 11px;
      color: var(--muted);
      margin-bottom: 4px;
    }
    button {
      padding: 10px 14px;
      border-radius: 999px;
      border: 0;
      background: #dfc4b4;
      color: var(--ink);
      cursor: pointer;
      font: inherit;
    }
    .status { color: var(--muted); margin: 8px 0 12px; }
    .filters {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
      gap: 12px 16px;
      margin-bottom: 12px;
    }
    .filter-group {
      margin: 0;
      padding: 10px 14px 14px;
      border: 1px solid #444;
      border-radius: 16px;
      background: #242424;
    }
    .filter-group legend {
      display: inline-block;
      margin: 0 0 8px;
      padding: 2px 8px;
      border-radius: 999px;
      background: var(--panel);
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.06em;
      font-size: 12px;
      color: var(--muted);
    }
    .filter-options {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 10px;
      margin-top: 0;
    }
    .filter-option {
      display: flex;
      align-items: flex-start;
      gap: 8px;
      min-width: 0;
      padding: 9px 11px;
      border: 1px solid #444;
      border-radius: 12px;
      background: #000;
      color: #fff;
      font-family: Arial, sans-serif;
      font-size: 13px;
      line-height: 1.3;
      cursor: pointer;
      transition: background-color 0.16s ease, border-color 0.16s ease, box-shadow 0.16s ease;
    }
    .filter-option input {
      margin: 0;
      flex: 0 0 auto;
      margin-top: 2px;
      accent-color: #b89d78;
    }
    .filter-option:hover {
      border-color: #b89d78;
      background: #151515;
    }
    .filter-option:has(input:checked) {
      border-color: #b89d78;
      background: #1f1f1f;
      box-shadow: inset 0 0 0 1px rgba(184,157,120,0.18);
    }
    .table-wrap {
      border: 1px solid #444;
      border-radius: 14px;
      overflow: auto;
      background: #242424;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      min-width: 760px;
    }
    th, td {
      text-align: left;
      padding: 10px 12px;
      border-bottom: 1px solid rgba(215,206,191,0.7);
    }
    thead th {
      background: #efe6d6;
      font-family: Arial, sans-serif;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.06em;
      color: #5a4630;
    }
    tbody tr:nth-child(odd) { background: #2e2e2e; }
    .mono { font-family: "Courier New", monospace; }
    .empty { padding: 18px; color: var(--muted); }
    .summary {
      margin: 12px 0 14px;
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 14px;
      line-height: 1.5;
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/reports") + `

    <h1>Reports</h1>
    <p>Operational reports generated from the SQLite-backed control and Security NFR datasets.</p>

    <section class="report-card">
      <div class="report-head">
        <h2 class="report-title">Security Controls Not Linked To Security NFRs</h2>
        <div>
          <button id="reloadBtn" type="button">Refresh Report</button>
        </div>
      </div>
      <div class="metrics">
        <div>
          <div class="metric-label">Unlinked Controls</div>
          <div class="count" id="countValue">...</div>
        </div>
        <div>
          <div class="metric-label">Total Controls</div>
          <div class="count" id="totalValue">...</div>
        </div>
        <div>
          <div class="metric-label">Linked Controls</div>
          <div class="count" id="linkedValue">...</div>
        </div>
      </div>
      <div class="filters">
        <fieldset class="filter-group" id="typeFilters">
          <legend>Control Type</legend>
          <div class="filter-options">
            <label class="filter-option"><input type="radio" name="report-type" value="all" checked> All</label>
            <label class="filter-option"><input type="radio" name="report-type" value="control"> Control</label>
            <label class="filter-option"><input type="radio" name="report-type" value="control enhancement"> Control Enhancement</label>
          </div>
        </fieldset>
        <fieldset class="filter-group" id="modeFilters">
          <legend>Link Status</legend>
          <div class="filter-options">
            <label class="filter-option"><input type="radio" name="report-mode" value="unlinked" checked> Unlinked Controls</label>
            <label class="filter-option"><input type="radio" name="report-mode" value="linked"> Linked Controls</label>
            <label class="filter-option"><input type="radio" name="report-mode" value="all"> All Controls</label>
          </div>
        </fieldset>
      </div>
      <div class="status" id="status">Loading report...</div>
      <div id="ciaSummary" class="summary">CIA coverage: loading...</div>
      <div id="threatSummary" class="summary">Threat coverage: loading...</div>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Control ID</th>
              <th>Name</th>
              <th>Family</th>
              <th>Security NFR</th>
            </tr>
          </thead>
          <tbody id="rows"></tbody>
        </table>
        <div id="empty" class="empty" style="display:none;">All controls are linked.</div>
      </div>
    </section>
  </main>

  <script>
    const countValue = document.getElementById('countValue');
    const totalValue = document.getElementById('totalValue');
    const linkedValue = document.getElementById('linkedValue');
    const status = document.getElementById('status');
    const rows = document.getElementById('rows');
    const empty = document.getElementById('empty');
    const ciaSummary = document.getElementById('ciaSummary');
    const threatSummary = document.getElementById('threatSummary');
    const reloadBtn = document.getElementById('reloadBtn');
    const typeFilters = document.getElementById('typeFilters');
    const modeFilters = document.getElementById('modeFilters');
    const state = { type: 'all', mode: 'unlinked' };
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

    function renderLinkedNFRs(value) {
      const links = Array.isArray(value) ? value : [];
      const html = links.map((item) => {
        const key = String(item && item.key ? item.key : '').trim();
        if (!key) return '';
        return '<a href="/security-nfrs/detail/' + encodeURIComponent(key) + '">' + esc(key) + '</a>';
      }).filter(Boolean).join(', ');
      return html || 'N/A';
    }

    function renderControlLink(value) {
      const controlID = String(value || '').trim();
      if (!controlID) return 'N/A';
      return '<a href="/controls/detail/' + encodeURIComponent(controlID) + '">' + esc(controlID) + '</a>';
    }

    function computeCIASummary(items) {
      const list = Array.isArray(items) ? items : [];
      let confidentiality = 0;
      let integrity = 0;
      let availability = 0;

      list.forEach((item) => {
        if (String(item && item.confidentiality || '').trim() !== '') confidentiality++;
        if (String(item && item.integrity || '').trim() !== '') integrity++;
        if (String(item && item.availability || '').trim() !== '') availability++;
      });

      return {
        controls: list.length,
        confidentiality,
        integrity,
        availability
      };
    }

    function renderCIASummary(items) {
      const summary = computeCIASummary(items);
      ciaSummary.textContent =
        'Controls in view: ' + summary.controls +
        ' | C: ' + summary.confidentiality +
        ' | I: ' + summary.integrity +
        ' | A: ' + summary.availability;
    }

    function computeThreatSummary(items) {
      const counts = new Map();
      (Array.isArray(items) ? items : []).forEach((item) => {
        if (!item || !Array.isArray(item.threats)) return;
        new Set(
          item.threats
            .map((threat) => String(threat || '').trim())
            .filter((threat) => threat !== '')
        ).forEach((threat) => {
          counts.set(threat, (counts.get(threat) || 0) + 1);
        });
      });
      return counts;
    }

    function renderThreatSummary(items) {
      const counts = computeThreatSummary(items);
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

    async function load() {
      status.textContent = 'Refreshing report...';
      ciaSummary.textContent = 'CIA coverage: loading...';
      threatSummary.textContent = 'Threat coverage: loading...';
      try {
        const resp = await fetch('/reports/data/unlinked-controls?type=' + encodeURIComponent(state.type) + '&mode=' + encodeURIComponent(state.mode));
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        const data = await resp.json();
        countValue.textContent = String(data.unlinked_count || 0);
        totalValue.textContent = String(data.total_controls || 0);
        linkedValue.textContent = String(data.linked_count || 0);

        const items = Array.isArray(data.items) ? data.items : [];
        renderCIASummary(items);
        renderThreatSummary(items);
        if (!items.length) {
          rows.innerHTML = '';
          empty.style.display = 'block';
          empty.textContent = state.mode === 'linked'
            ? 'No linked controls found for this filter.'
            : state.mode === 'all'
              ? 'No controls found for this filter.'
              : 'All controls are linked.';
        } else {
          empty.style.display = 'none';
          empty.textContent = 'All controls are linked.';
          rows.innerHTML = items.map((item) =>
            '<tr>' +
              '<td class="mono">' + renderControlLink(item.control_id) + '</td>' +
              '<td>' + esc(item.name) + '</td>' +
              '<td class="mono">' + esc(item.family) + '</td>' +
              '<td class="mono">' + renderLinkedNFRs(item.linked_security_nfrs) + '</td>' +
            '</tr>'
          ).join('');
        }

        status.textContent = 'Report loaded for type: ' + (data.type || 'All') + ' | mode: ' + (data.mode || 'All') + '.';
      } catch (err) {
        countValue.textContent = '0';
        totalValue.textContent = '0';
        linkedValue.textContent = '0';
        ciaSummary.textContent = 'CIA coverage: unavailable';
        threatSummary.textContent = 'Threat coverage: unavailable';
        rows.innerHTML = '';
        empty.style.display = 'block';
        empty.textContent = 'Failed to load report: ' + err.message;
        status.textContent = 'Report load failed.';
      }
    }

    reloadBtn.addEventListener('click', load);
    typeFilters.querySelectorAll('input[name="report-type"]').forEach((input) => {
      input.addEventListener('change', () => {
        if (!input.checked) return;
        state.type = input.value || 'all';
        load();
      });
    });
    modeFilters.querySelectorAll('input[name="report-mode"]').forEach((input) => {
      input.addEventListener('change', () => {
        if (!input.checked) return;
        state.mode = input.value || 'unlinked';
        load();
      });
    });
    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
