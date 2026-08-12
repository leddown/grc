package nfrlink

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/apiutil"
	"grc/internal/pageui"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListLinkRows(c *gin.Context) {
	search := c.Query("search")
	onlyUnmatched := strings.EqualFold(c.DefaultQuery("onlyUnmatched", "false"), "true")
	page := apiutil.ParsePagination(c)

	rows, err := h.service.List(search, onlyUnmatched)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list NFR-control links"})
		return
	}
	if page.Enabled {
		items, total := apiutil.PaginateSlice(rows, page)
		c.JSON(http.StatusOK, gin.H{
			"items":    items,
			"total":    total,
			"page":     page.Page,
			"per_page": page.PerPage,
		})
		return
	}
	c.JSON(http.StatusOK, rows)
}

type setOverrideRequest struct {
	NFRKey            string `json:"nfr_key"`
	MappingControlID  string `json:"mapping_control_id"`
	OverrideControlID string `json:"override_control_id"`
	Matched           bool   `json:"matched"`
}

func (h *Handler) ListOverrides(c *gin.Context) {
	items, err := h.service.ListOverrides()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list overrides"})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) SetOverride(c *gin.Context) {
	var req setOverrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if err := h.service.SetOverride(Override{
		NFRKey:            req.NFRKey,
		MappingControlID:  req.MappingControlID,
		OverrideControlID: req.OverrideControlID,
		Matched:           req.Matched,
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.Rebuild(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rebuild NFR-control links"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) DeleteOverride(c *gin.Context) {
	err := h.service.DeleteOverride(c.Param("nfrKey"), c.Param("mappingControlID"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.Rebuild(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rebuild NFR-control links"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Rebuild(c *gin.Context) {
	if err := h.service.Rebuild(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rebuild NFR-control links"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) Page(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Security NFR ↔ Control Links · GRC</title>
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
    .toolbar {
      display: grid;
      grid-template-columns: minmax(260px, 1fr) auto auto;
      gap: 12px;
      align-items: center;
      margin: 20px 0 12px;
    }
    input[type="search"] {
      width: 100%;
      padding: 12px 14px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: white;
      font: inherit;
    }
    .check {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      padding: 10px 12px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: white;
      font-family: Arial, sans-serif;
      font-size: 13px;
    }
    button {
      padding: 12px 16px;
      border-radius: 999px;
      border: 0;
      background: #dfc4b4;
      color: var(--ink);
      cursor: pointer;
      font: inherit;
    }
    .summary {
      display: flex;
      gap: 18px;
      flex-wrap: wrap;
      margin-bottom: 12px;
      color: var(--muted);
    }
    .table-wrap {
      border: 1px solid #444;
      border-radius: 16px;
      overflow: auto;
      background: #242424;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      min-width: 1120px;
    }
    thead th {
      position: sticky;
      top: 0;
      background: #efe6d6;
      text-align: left;
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: #5a4630;
      padding: 10px 12px;
      border-bottom: 1px solid var(--line);
    }
    tbody td {
      padding: 10px 12px;
      border-bottom: 1px solid rgba(215,206,191,0.7);
      vertical-align: top;
      font-size: 14px;
    }
    tbody tr:nth-child(odd) { background: #2e2e2e; }
    .mono { font-family: "Courier New", monospace; }
    .empty { padding: 24px; color: var(--muted); text-align: center; }
    @media (max-width: 980px) {
      .toolbar { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/security-nfrs/links") + `

    <h1>Security NFR ↔ Control Linking Table</h1>
    <p>Cross-reference of Security NFR <code>NIST Mapping</code> tokens to control <code>control_id</code> values.</p>

    <section class="toolbar">
      <input id="searchInput" type="search" placeholder="Search NFR key, summary, mapped ID, or control ID">
      <label class="check"><input id="onlyUnmatched" type="checkbox"> Show unmatched only</label>
      <button id="reloadBtn" type="button">Rebuild & Reload</button>
    </section>

    <div class="summary">
      <div id="status">Loading link rows...</div>
      <div id="counts"></div>
    </div>

    <section class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Domain</th>
            <th>NFR Summary</th>
            <th>Linked Control ID</th>
            <th>Control Name</th>
            <th>Family</th>
          </tr>
        </thead>
        <tbody id="rows"></tbody>
      </table>
      <div id="empty" class="empty" style="display:none;">No rows to display.</div>
    </section>
  </main>

  <script>
    const state = { rows: [] };
    const rowsEl = document.getElementById('rows');
    const emptyEl = document.getElementById('empty');
    const statusEl = document.getElementById('status');
    const countsEl = document.getElementById('counts');
    const searchInput = document.getElementById('searchInput');
    const onlyUnmatched = document.getElementById('onlyUnmatched');
    const reloadBtn = document.getElementById('reloadBtn');

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function applyLocalFilter() {
      const q = searchInput.value.trim().toUpperCase();
      const filtered = state.rows.filter((row) => {
        if (!q) return true;
        return [row.nfr_key, row.nfr_summary, row.mapping_control_id, row.control_id]
          .some((value) => String(value || '').toUpperCase().includes(q));
      });

      if (!filtered.length) {
        rowsEl.innerHTML = '';
        emptyEl.style.display = 'block';
      } else {
        emptyEl.style.display = 'none';
        rowsEl.innerHTML = filtered.map((row) => {
          return '<tr>' +
            '<td>' + esc(row.nfr_domain || 'N/A') + '</td>' +
            '<td>' + esc(row.nfr_summary || 'N/A') + '</td>' +
            '<td class="mono">' + esc(row.control_id || 'N/A') + '</td>' +
            '<td>' + esc(row.control_name || 'N/A') + '</td>' +
            '<td class="mono">' + esc(row.control_family || 'N/A') + '</td>' +
          '</tr>';
        }).join('');
      }

      const matchedCount = filtered.filter((row) => row.matched).length;
      countsEl.textContent = 'Rows: ' + filtered.length + ' | Matched: ' + matchedCount + ' | Unmatched: ' + (filtered.length - matchedCount);
    }

    async function load() {
      statusEl.textContent = 'Loading link rows...';
      const params = new URLSearchParams();
      if (onlyUnmatched.checked) params.set('onlyUnmatched', 'true');
      try {
        const resp = await fetch('/security-nfrs/links/data?' + params.toString());
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        state.rows = await resp.json();
        statusEl.textContent = 'Loaded linked dataset.';
        applyLocalFilter();
      } catch (err) {
        statusEl.textContent = 'Failed to load links.';
        rowsEl.innerHTML = '';
        emptyEl.style.display = 'block';
        emptyEl.textContent = 'Error: ' + err.message;
      }
    }

    async function rebuildAndLoad() {
      statusEl.textContent = 'Rebuilding link table...';
      try {
        const rebuildResp = await fetch('/security-nfrs/links/rebuild', { method: 'POST' });
        if (!rebuildResp.ok) throw new Error('Rebuild failed: HTTP ' + rebuildResp.status);
        await load();
      } catch (err) {
        statusEl.textContent = 'Failed to rebuild links.';
        rowsEl.innerHTML = '';
        emptyEl.style.display = 'block';
        emptyEl.textContent = 'Error: ' + err.message;
      }
    }

    searchInput.addEventListener('input', applyLocalFilter);
    reloadBtn.addEventListener('click', rebuildAndLoad);
    onlyUnmatched.addEventListener('change', load);
    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
