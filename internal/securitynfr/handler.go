package securitynfr

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/apiutil"
	"grc/internal/nfrfile"
	"grc/internal/pageui"
)

type Handler struct {
	service     *Service
	linkBuilder linkBuilder
}

type linkBuilder interface {
	Rebuild() error
}

func NewHandler(service *Service, linkBuilder linkBuilder) *Handler {
	return &Handler{service: service, linkBuilder: linkBuilder}
}

// Service exposes the catalog service to sibling modules that need to read or
// update NFRs — today internal/nfrenrich, which proposes enrichments and, on a
// reviewer's acceptance, writes one field. It is an accessor rather than a
// tenth return value from buildHandlers because the catalog service is already
// constructed there and threading it out separately buys nothing.
func (h *Handler) Service() *Service { return h.service }

func (h *Handler) ListNFRs(c *gin.Context) {
	search := c.Query("search")
	domain := c.Query("domain")
	page := apiutil.ParsePagination(c)

	items, err := h.service.List(search, domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list security NFRs"})
		return
	}
	if page.Enabled {
		pagedItems, total := apiutil.PaginateSlice(items, page)
		c.JSON(http.StatusOK, gin.H{
			"items":    pagedItems,
			"total":    total,
			"page":     page.Page,
			"per_page": page.PerPage,
		})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) ListDomains(c *gin.Context) {
	items, err := h.service.List("", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load domains"})
		return
	}

	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, item := range items {
		domain := strings.TrimSpace(item.Domain)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	sort.Strings(out)
	c.JSON(http.StatusOK, out)
}

type updateNFRRequest struct {
	ID                string `json:"id"`
	Summary           string `json:"summary"`
	IssueType         string `json:"issue_type"`
	Description       string `json:"description"`
	NISTMapping       string `json:"nist_mapping"`
	AdditionalDetails string `json:"additional_details"`
	Implementation    string `json:"implementation"`
	Domain            string `json:"domain"`
}

func (h *Handler) UpdateNFR(c *gin.Context) {
	key := c.Param("key")

	var req updateNFRRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	updated, err := h.service.Update(key, NFR{
		ID:                req.ID,
		Summary:           req.Summary,
		IssueType:         req.IssueType,
		Description:       req.Description,
		NISTMapping:       req.NISTMapping,
		AdditionalDetails: req.AdditionalDetails,
		Implementation:    req.Implementation,
		Domain:            req.Domain,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "required") {
			status = http.StatusBadRequest
		}
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if h.linkBuilder != nil {
		if err := h.linkBuilder.Rebuild(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rebuild NFR-control links"})
			return
		}
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) DeleteNFR(c *gin.Context) {
	key := c.Param("key")
	if err := h.service.Delete(key); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if h.linkBuilder != nil {
		if err := h.linkBuilder.Rebuild(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rebuild NFR-control links"})
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) SaveNFRsToJSON(c *gin.Context) {
	total, path, err := h.service.SaveToJSON()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save security NFRs to JSON"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"saved": total,
		"path":  path,
	})
}

func (h *Handler) NFRJSONData(c *gin.Context) {
	domain := strings.TrimSpace(c.Query("domain"))
	items, err := h.service.List("", domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load security NFRs"})
		return
	}

	payload := make(nfrfile.File, len(items))
	for _, item := range items {
		payload[item.Key] = nfrfile.Entry{
			Summary:           item.Summary,
			ID:                item.ID,
			IssueType:         item.IssueType,
			Description:       item.Description,
			NISTMapping:       item.NISTMapping,
			AdditionalDetails: item.AdditionalDetails,
			Implementation:    item.Implementation,
			Domain:            item.Domain,
		}
	}

	c.JSON(http.StatusOK, payload)
}

func (h *Handler) DetailPage(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	item, err := h.service.Get(key)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.Data(status, "text/plain; charset=utf-8", []byte("Security NFR not found"))
		return
	}

	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Security NFR Detail · GRC</title>
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
    h1 { margin: 0 0 10px; font-size: clamp(2rem, 4vw, 3rem); line-height: 1; }
    .meta {
      color: var(--muted);
      margin-bottom: 16px;
      font-family: Arial, sans-serif;
      font-size: 13px;
      text-transform: uppercase;
      letter-spacing: 0.06em;
    }
    .grid { display: grid; gap: 12px; }
    .card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 14px;
      background: var(--panel);
      padding: 14px;
    }
    .card h2 {
      margin: 0 0 8px;
      font-size: 12px;
      color: var(--muted);
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .text { white-space: pre-wrap; line-height: 1.5; }
    code { background: #efe6d6; padding: 2px 6px; border-radius: 6px; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/security-nfrs/detail/"+item.Key) + `

    <h1>` + htmlEscape(item.Summary) + `</h1>
    <div class="meta">Key ` + htmlEscape(item.Key) + ` | ` + htmlEscape(item.IssueType) + ` | ` + htmlEscape(item.Domain) + `</div>

    <section class="grid">
      <article class="card">
        <h2>NIST Mapping</h2>
        <div class="text">` + htmlEscape(item.NISTMapping) + `</div>
      </article>
      <article class="card">
        <h2>Description</h2>
        <div class="text">` + htmlEscape(orNA(item.Description)) + `</div>
      </article>
      <article class="card">
        <h2>Additional Details</h2>
        <div class="text">` + htmlEscape(orNA(item.AdditionalDetails)) + `</div>
      </article>
      <article class="card">
        <h2>Implementation</h2>
        <div class="text">` + htmlEscape(orNA(item.Implementation)) + `</div>
      </article>
    </section>
  </main>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

var detailHTMLReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
)

func htmlEscape(value string) string {
	return detailHTMLReplacer.Replace(value)
}

func orNA(value string) string {
	if strings.TrimSpace(value) == "" {
		return "N/A"
	}
	return value
}

func (h *Handler) Page(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Security NFRs · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --panel: rgba(255,252,246,0.92);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
      --chip: #ece3d2;
      /* The list rows are <button>s, and buttons don't inherit the page font,
         so they have always rendered at the UA's ~13px control default while
         the detail cards inherited the 16px body font. Both panels are pinned
         to this one size so they read as a matched pair. */
      --panel-text: 13px;
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
    .filters { margin: 22px 0 18px; }
    .filters-header {
      display: flex;
      align-items: center;
      gap: 10px;
      width: 100%;
      padding: 10px 16px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: #efe6d6;
      color: var(--ink);
      cursor: pointer;
      text-align: left;
      font-family: Arial, sans-serif;
      font-size: 13px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    .caret { display: inline-block; transition: transform 0.15s ease; }
    .filters.collapsed .caret, .layout.detail-collapsed .caret { transform: rotate(-90deg); }
    .filters.collapsed .toolbar { display: none; }
    .filters-summary { margin-left: auto; color: var(--muted); text-transform: none; letter-spacing: normal; }
    .filters:not(.collapsed) .filters-summary { display: none; }
    .toolbar {
      display: grid;
      grid-template-columns: minmax(260px, 1fr) minmax(320px, 1fr) auto;
      gap: 12px;
      margin: 12px 0 0;
      /* The domain chips wrap onto several rows and can end up much taller
         than the search input and Reset button sharing this grid row;
         align-items: center used to leave them floating in the middle of
         that empty height instead of sitting at the top like the chips. */
      align-items: start;
    }
    input {
      width: 100%;
      padding: 12px 14px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: var(--bg);
      font: inherit;
    }
    .chips {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
    }
    .chip {
      border: 1px solid rgba(184,157,120,0.35);
      padding: 10px 14px;
      border-radius: 999px;
      background: var(--chip);
      cursor: pointer;
      font: inherit;
    }
    .chip.active { background: #c48b54; border-color: #6f2d22; font-weight: 700; }
    .button {
      padding: 12px 16px;
      border-radius: 999px;
      border: 0;
      background: #dfc4b4;
      color: var(--ink);
      cursor: pointer;
      font: inherit;
    }
    .layout {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(360px, 1fr);
      gap: 18px;
      min-height: 70vh;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 22px;
      background: var(--panel);
      overflow: hidden;
    }
    .panel-header {
      padding: 16px 18px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
    }
    .detail-header { display: flex; align-items: center; gap: 12px; }
    .header-actions { margin-left: auto; display: flex; align-items: center; gap: 8px; }
    .panel-toggle {
      flex: none;
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 6px 12px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: #efe6d6;
      color: var(--ink);
      cursor: pointer;
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    .panel-toggle:disabled { opacity: 0.45; cursor: default; }
    /* Collapsing the detail panel drops the grid to a single column so the NFR
       list stretches the full page width; the collapsed panel keeps its header
       (and the toggle) as a bar under the list so it can be brought back. */
    /* align-content matters: .layout has a min-height, and the default stretch
       would inflate the now header-only detail row to fill half of it. */
    .layout.detail-collapsed { grid-template-columns: minmax(0, 1fr); align-content: start; }
    .layout.detail-collapsed .detail-body { display: none; }
    .layout.detail-collapsed .detail-header { border-bottom: 0; }
    .rows {
      max-height: 74vh;
      overflow: auto;
      font-size: var(--panel-text);
    }
    .row {
      width: 100%;
      text-align: left;
      border: 0;
      border-bottom: 1px solid rgba(215,206,191,0.6);
      background: transparent;
      padding: 16px 18px;
      cursor: pointer;
      font-family: inherit;
      font-size: var(--panel-text);
    }
    .row:hover, .row.active { background: rgba(139,61,46,0.08); }
    .row.active { box-shadow: inset 4px 0 0 var(--accent); }
    .row-title { font-weight: bold; }
    .row-sub { color: var(--muted); margin-top: 6px; }
    .detail-body { padding: 18px; display: grid; gap: 12px; font-size: var(--panel-text); }
    .card {
      border: 1px solid rgba(215,206,191,0.85);
      border-radius: 14px;
      padding: 12px;
      background: rgba(243,239,230,0.8);
    }
    .card h3 {
      margin: 0 0 8px;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: var(--muted);
      font-family: Arial, sans-serif;
    }
    .empty { padding: 30px 18px; color: var(--muted); }
    @media (max-width: 980px) {
      .toolbar, .layout { grid-template-columns: 1fr; }
      .rows { max-height: none; }
    }
  </style>
</head>
<body>
  <main>
	    ` + pageui.Nav("/security-nfrs") + `
    <h1>Security NFRs</h1>
    <p>SQLite-backed view of <code>internal/data/NFR_incremental_keys_with_domain.json</code>.</p>

    <section id="filters" class="filters">
      <button id="filtersToggle" class="filters-header" type="button" aria-expanded="true" aria-controls="filtersBody">
        <span class="caret" aria-hidden="true">&#9662;</span>
        <span>Search &amp; Domain</span>
        <span id="filtersSummary" class="filters-summary"></span>
      </button>
      <div id="filtersBody" class="toolbar">
        <input id="searchInput" type="search" placeholder="Search key, ID, or summary">
        <div id="domainChips" class="chips"></div>
        <button id="resetBtn" class="button" type="button">Reset</button>
      </div>
    </section>

    <section id="layout" class="layout">
      <div class="panel">
        <div class="panel-header"><strong id="status">Loading security NFRs...</strong></div>
        <div id="rows" class="rows"></div>
      </div>
      <div class="panel">
        <div class="panel-header detail-header">
          <strong id="selection">No NFR selected</strong>
          <div class="header-actions">
            <button id="editBtn" class="panel-toggle" type="button" disabled>Edit</button>
            <button id="detailToggle" class="panel-toggle" type="button" aria-expanded="true" aria-controls="detail">
              <span class="caret" aria-hidden="true">&#9662;</span>
              <span id="detailToggleLabel">Hide</span>
            </button>
          </div>
        </div>
        <div id="detail" class="detail-body">
          <div class="empty">Select an NFR row to inspect details.</div>
        </div>
      </div>
    </section>
  </main>
  <script>
    const state = { items: [], filtered: [], selectedKey: '', domain: '' };
    const controlIDPattern = /[A-Z]{2}-\d+(?:\(\d+\))?/g;
    const rows = document.getElementById('rows');
    const detail = document.getElementById('detail');
    const status = document.getElementById('status');
    const selection = document.getElementById('selection');
    const searchInput = document.getElementById('searchInput');
    const domainChips = document.getElementById('domainChips');
    const resetBtn = document.getElementById('resetBtn');
    const filters = document.getElementById('filters');
    const filtersToggle = document.getElementById('filtersToggle');
    const filtersSummary = document.getElementById('filtersSummary');
    const layout = document.getElementById('layout');
    const detailToggle = document.getElementById('detailToggle');
    const detailToggleLabel = document.getElementById('detailToggleLabel');
    const editBtn = document.getElementById('editBtn');
    const FILTERS_KEY = 'grc-nfr-filters-collapsed';
    const DETAIL_KEY = 'grc-nfr-detail-collapsed';

    // Blocked localStorage (private mode, hardened settings) must not break the
    // collapse toggles; they just stop remembering their state across loads.
    function readFlag(key) {
      try {
        return localStorage.getItem(key) === '1';
      } catch (err) {
        return false;
      }
    }

    function writeFlag(key, on) {
      try {
        localStorage.setItem(key, on ? '1' : '0');
      } catch (err) {
        /* nothing to do: the in-page toggle still works */
      }
    }

    function esc(v) {
      return String(v || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function selected() {
      return state.filtered.find((item) => item.key === state.selectedKey) || null;
    }

    function renderDomains() {
      const domains = [...new Set(state.items.map((item) => (item.domain || '').trim()).filter(Boolean))].sort();
      const parts = ['<button class="chip ' + (state.domain === '' ? 'active' : '') + '" data-domain="" type="button">All Domains</button>'];
      domains.forEach((domain) => {
        parts.push('<button class="chip ' + (state.domain === domain ? 'active' : '') + '" data-domain="' + esc(domain) + '" type="button">' + esc(domain) + '</button>');
      });
      domainChips.innerHTML = parts.join('');
      domainChips.querySelectorAll('.chip').forEach((chip) => {
        chip.addEventListener('click', () => {
          state.domain = chip.getAttribute('data-domain') || '';
          applyFilters();
        });
      });
    }

    function renderRows() {
      if (!state.filtered.length) {
        rows.innerHTML = '<div class="empty">No Security NFRs match the current filters.</div>';
        return;
      }
      rows.innerHTML = state.filtered.map((item) => {
        const active = item.key === state.selectedKey ? 'active' : '';
        return '<button class="row ' + active + '" data-key="' + esc(item.key) + '" type="button">' +
          '<div class="row-title">Key ' + esc(item.key) + '</div>' +
          '<div class="row-sub">' + esc(item.summary || 'No summary') + '</div>' +
          '<div class="row-sub">' + esc(item.domain || 'No domain') + '</div>' +
        '</button>';
      }).join('');
      rows.querySelectorAll('.row').forEach((row) => {
        row.addEventListener('click', () => {
          state.selectedKey = row.getAttribute('data-key') || '';
          // Picking a row is a request to see it, so a collapsed detail panel
          // reopens instead of leaving the click with no visible result.
          if (layout.classList.contains('detail-collapsed')) {
            setDetailCollapsed(false);
            writeFlag(DETAIL_KEY, false);
          }
          renderRows();
          renderDetail();
        });
      });
    }

    function renderDetail() {
      const item = selected();
      editBtn.disabled = !item;
      editBtn.title = item
        ? 'Open key ' + item.key + ' in the Security NFR editor'
        : 'Select an NFR row first';
      if (!item) {
        selection.textContent = 'No NFR selected';
        detail.innerHTML = '<div class="empty">Select an NFR row to inspect details.</div>';
        return;
      }
      selection.textContent = 'Viewing key ' + item.key;
      detail.innerHTML = [
        ['Summary', item.summary],
        ['Issue Type', item.issue_type],
        ['Domain', item.domain],
        ['NIST Mapping', item.nist_mapping],
        ['Additional Details', item.additional_details],
        ['Implementation', item.implementation],
        ['Description', item.description]
      ].map(([label, value]) => {
        if (label === 'NIST Mapping') {
          return '<section class="card"><h3>' + esc(label) + '</h3><div>' + renderNISTMapping(value) + '</div></section>';
        }
        return '<section class="card"><h3>' + esc(label) + '</h3><div>' + esc(value || 'N/A').replaceAll('\n', '<br>') + '</div></section>';
      }).join('');
    }

    function renderNISTMapping(value) {
      const raw = String(value || '').trim();
      if (!raw) {
        return 'N/A';
      }

      const matches = raw.toUpperCase().match(controlIDPattern) || [];
      const seen = new Set();
      const ids = [];
      matches.forEach((id) => {
        if (seen.has(id)) return;
        seen.add(id);
        ids.push(id);
      });

      if (!ids.length) {
        return esc(raw).replaceAll('\n', '<br>');
      }

      return ids.map((id) =>
        '<a class="control-chip" href="/controls/detail/' + encodeURIComponent(id) + '" target="_blank" rel="noopener noreferrer" ' +
        'style="display:inline-block;margin:0 8px 8px 0;padding:4px 8px;border:1px solid var(--line);border-radius:999px;background:var(--surface-2, #efe6d6);color:var(--ink);text-decoration:none;font-family:Courier New,monospace;font-size:13px;">' +
        esc(id) + '</a>'
      ).join('');
    }

    // Keeps the active filters visible while the section is collapsed, so a
    // collapsed toolbar can't silently hide why the row list is short.
    function renderFiltersSummary(term) {
      const parts = [];
      if (state.domain) parts.push(state.domain);
      if (term) parts.push('"' + searchInput.value.trim() + '"');
      filtersSummary.textContent = parts.length ? parts.join(' | ') : 'No filters';
    }

    function setFiltersCollapsed(collapsed) {
      filters.classList.toggle('collapsed', collapsed);
      filtersToggle.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
    }

    function setDetailCollapsed(collapsed) {
      layout.classList.toggle('detail-collapsed', collapsed);
      detailToggle.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
      detailToggleLabel.textContent = collapsed ? 'Show' : 'Hide';
    }

    function applyFilters() {
      const term = searchInput.value.trim().toUpperCase();
      state.filtered = state.items.filter((item) => {
        if (state.domain && (item.domain || '') !== state.domain) return false;
        if (!term) return true;
        return String(item.key || '').toUpperCase().includes(term) ||
          String(item.id || '').toUpperCase().includes(term) ||
          String(item.summary || '').toUpperCase().includes(term);
      });
      if (!state.filtered.some((item) => item.key === state.selectedKey)) {
        state.selectedKey = state.filtered.length ? state.filtered[0].key : '';
      }
      status.textContent = 'Showing ' + state.filtered.length + ' security NFRs';
      renderFiltersSummary(term);
      renderDomains();
      renderRows();
      renderDetail();
    }

    async function load() {
      try {
        const resp = await fetch('/security-nfrs/data');
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        state.items = await resp.json();
        applyFilters();
      } catch (err) {
        status.textContent = 'Load failed';
        rows.innerHTML = '<div class="empty">Failed to load security NFRs: ' + esc(err.message) + '</div>';
      }
    }

    filtersToggle.addEventListener('click', () => {
      const collapsed = !filters.classList.contains('collapsed');
      setFiltersCollapsed(collapsed);
      writeFlag(FILTERS_KEY, collapsed);
    });

    detailToggle.addEventListener('click', () => {
      const collapsed = !layout.classList.contains('detail-collapsed');
      setDetailCollapsed(collapsed);
      writeFlag(DETAIL_KEY, collapsed);
    });

    editBtn.addEventListener('click', () => {
      const item = selected();
      if (!item) return;
      window.location.assign('/security-nfrs/manage?key=' + encodeURIComponent(item.key));
    });

    setFiltersCollapsed(readFlag(FILTERS_KEY));
    setDetailCollapsed(readFlag(DETAIL_KEY));

    searchInput.addEventListener('input', applyFilters);
    resetBtn.addEventListener('click', () => {
      state.domain = '';
      searchInput.value = '';
      applyFilters();
    });

    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func (h *Handler) ManagePage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Security NFR Editor · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --panel: rgba(255,252,246,0.94);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
      --success: #0b5d3b;
      --warn: #9a6700;
      /* Same token, and same value, as the /security-nfrs list page: the NFR
         row lists on the two pages are meant to read identically. */
      --panel-text: 13px;
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
    .toolbar {
      display: grid;
      grid-template-columns: minmax(260px, 1fr) auto auto;
      gap: 12px;
      margin: 22px 0 18px;
    }
    input, textarea {
      width: 100%;
      padding: 12px 14px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: var(--bg);
      font: inherit;
    }
    textarea { min-height: 96px; resize: vertical; }
    button {
      padding: 12px 16px;
      border-radius: 999px;
      border: 0;
      background: #dfc4b4;
      color: var(--ink);
      cursor: pointer;
      font: inherit;
    }
    button.secondary { background: var(--surface-strong); border: 1px solid var(--line); }
    button.danger { background: #e9c6d4; color: #3f1022; }
    .layout {
      display: grid;
      grid-template-columns: minmax(0, 0.95fr) minmax(360px, 1.1fr);
      gap: 18px;
      min-height: 72vh;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 22px;
      background: var(--panel);
      overflow: hidden;
    }
    .panel-header {
      padding: 16px 18px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
    }
    .rows { max-height: 76vh; overflow: auto; font-size: var(--panel-text); }
    .row {
      width: 100%;
      text-align: left;
      border: 0;
      border-bottom: 1px solid rgba(215,206,191,0.6);
      background: transparent;
      padding: 16px 18px;
      cursor: pointer;
      /* Family already comes from this page's bare button font: inherit rule;
         only the size is pinned here, so the rows match the list page rather
         than the 16px editor form beside them. */
      font-size: var(--panel-text);
    }
    .row:hover, .row.active { background: rgba(139,61,46,0.08); }
    .row.active { box-shadow: inset 4px 0 0 var(--accent); }
    .row-title { font-weight: bold; }
    .row-sub { color: var(--muted); margin-top: 4px; }
    form { padding: 18px; display: grid; gap: 14px; }
    .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
    .field label {
      display: block;
      margin-bottom: 6px;
      color: var(--muted);
      font-size: 13px;
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.06em;
    }
    .form-actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      justify-content: space-between;
      align-items: center;
    }
    .status { color: var(--muted); }
    .status.success { color: var(--success); }
    .status.error { color: #b91c1c; }
    .status.warn { color: var(--warn); }
    .empty { padding: 36px 20px; color: var(--muted); text-align: center; }
    @media (max-width: 980px) {
      .toolbar, .layout, .grid { grid-template-columns: 1fr; }
      .rows { max-height: none; }
    }
  </style>
</head>
<body>
  <main>
	    ` + pageui.Nav("/security-nfrs/manage") + `
    <h1>Security NFR<br>Editor</h1>
    <p>Edit Security NFR rows in SQLite, then write them back to <code>internal/data/NFR_incremental_keys_with_domain.json</code>.</p>

    <section class="toolbar">
      <input id="searchInput" type="search" placeholder="Search by key, ID, summary, or domain">
      <button id="reloadBtn" class="secondary" type="button">Reload DB</button>
      <button id="exportBtn" type="button">Rewrite JSON</button>
    </section>

    <section class="layout">
      <div class="panel">
        <div class="panel-header">
          <strong>Security NFRs</strong>
          <span id="listStatus" class="status">Loading...</span>
        </div>
        <div id="rows" class="rows"></div>
      </div>
      <div class="panel">
        <div class="panel-header">
          <strong>Edit Security NFR</strong>
          <span id="saveStatus" class="status">Select an NFR</span>
        </div>
        <form id="editorForm">
          <div class="grid">
            <div class="field">
              <label for="nfrKey">Record Key</label>
              <input id="nfrKey" type="text" readonly>
            </div>
            <div class="field">
              <label for="nfrID">NFR ID</label>
              <input id="nfrID" type="text">
            </div>
          </div>

          <div class="field">
            <label for="nfrSummary">Summary</label>
            <input id="nfrSummary" type="text" required>
          </div>

          <div class="grid">
            <div class="field">
              <label for="nfrIssueType">Issue Type</label>
              <input id="nfrIssueType" type="text">
            </div>
            <div class="field">
              <label for="nfrDomain">Domain</label>
              <input id="nfrDomain" type="text">
            </div>
          </div>

          <div class="field">
            <label for="nfrNISTMapping">NIST Mapping</label>
            <input id="nfrNISTMapping" type="text">
          </div>

          <div class="field">
            <label for="nfrDescription">Description</label>
            <textarea id="nfrDescription"></textarea>
          </div>

          <div class="field">
            <label for="nfrAdditionalDetails">Additional Details</label>
            <textarea id="nfrAdditionalDetails"></textarea>
          </div>

          <div class="field">
            <label for="nfrImplementation">Implementation</label>
            <textarea id="nfrImplementation"></textarea>
          </div>

          <div class="form-actions">
            <div id="formMessage" class="status warn">Changes are saved to SQLite first. Use Rewrite JSON to persist back to the flat file.</div>
            <div style="display:flex; gap:10px; flex-wrap:wrap;">
              <button id="deleteBtn" class="danger" type="button">Delete NFR</button>
              <button id="saveBtn" type="submit">Save To SQLite</button>
            </div>
          </div>
        </form>
      </div>
    </section>
  </main>

  <script>
    const state = { items: [], filtered: [], selectedKey: '' };

    const rows = document.getElementById('rows');
    const listStatus = document.getElementById('listStatus');
    const saveStatus = document.getElementById('saveStatus');
    const formMessage = document.getElementById('formMessage');
    const searchInput = document.getElementById('searchInput');
    const reloadBtn = document.getElementById('reloadBtn');
    const exportBtn = document.getElementById('exportBtn');
    const deleteBtn = document.getElementById('deleteBtn');
    const editorForm = document.getElementById('editorForm');

    const nfrKey = document.getElementById('nfrKey');
    const nfrID = document.getElementById('nfrID');
    const nfrSummary = document.getElementById('nfrSummary');
    const nfrIssueType = document.getElementById('nfrIssueType');
    const nfrDomain = document.getElementById('nfrDomain');
    const nfrNISTMapping = document.getElementById('nfrNISTMapping');
    const nfrDescription = document.getElementById('nfrDescription');
    const nfrAdditionalDetails = document.getElementById('nfrAdditionalDetails');
    const nfrImplementation = document.getElementById('nfrImplementation');

    // Deep link from the Edit button in the /security-nfrs detail panel.
    let pendingKey = new URLSearchParams(window.location.search).get('key') || '';

    function esc(v) {
      return String(v || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function selected() {
      return state.items.find((item) => item.key === state.selectedKey) || null;
    }

    function renderRows() {
      if (!state.filtered.length) {
        rows.innerHTML = '<div class="empty">No security NFRs match the current search.</div>';
        return;
      }
      rows.innerHTML = state.filtered.map((item) => {
        const active = item.key === state.selectedKey ? 'active' : '';
        return '<button class="row ' + active + '" data-key="' + esc(item.key) + '" type="button">' +
          '<div class="row-title">Key ' + esc(item.key) + '</div>' +
          '<div class="row-sub"><strong>' + esc(item.summary || 'Untitled') + '</strong></div>' +
          '<div class="row-sub">' + esc(item.domain || 'No domain') + '</div>' +
        '</button>';
      }).join('');

      rows.querySelectorAll('.row').forEach((row) => {
        row.addEventListener('click', () => {
          state.selectedKey = row.getAttribute('data-key');
          fillForm(selected());
          renderRows();
        });
      });
    }

    function fillForm(item) {
      if (!item) {
        nfrKey.value = '';
        nfrID.value = '';
        nfrSummary.value = '';
        nfrIssueType.value = '';
        nfrDomain.value = '';
        nfrNISTMapping.value = '';
        nfrDescription.value = '';
        nfrAdditionalDetails.value = '';
        nfrImplementation.value = '';
        saveStatus.textContent = 'Select an NFR';
        return;
      }

      nfrKey.value = item.key || '';
      nfrID.value = item.id || '';
      nfrSummary.value = item.summary || '';
      nfrIssueType.value = item.issue_type || '';
      nfrDomain.value = item.domain || '';
      nfrNISTMapping.value = item.nist_mapping || '';
      nfrDescription.value = item.description || '';
      nfrAdditionalDetails.value = item.additional_details || '';
      nfrImplementation.value = item.implementation || '';
      saveStatus.textContent = 'Editing key ' + item.key;
    }

    function applySearch() {
      const value = searchInput.value.trim().toUpperCase();
      state.filtered = state.items.filter((item) => {
        if (!value) return true;
        return String(item.key || '').toUpperCase().includes(value) ||
          String(item.id || '').toUpperCase().includes(value) ||
          String(item.summary || '').toUpperCase().includes(value) ||
          String(item.domain || '').toUpperCase().includes(value);
      });

      if (!state.filtered.find((item) => item.key === state.selectedKey)) {
        state.selectedKey = state.filtered.length ? state.filtered[0].key : '';
      }

      renderRows();
      fillForm(selected());
      listStatus.textContent = 'Showing ' + state.filtered.length + ' security NFRs';
    }

    async function loadNFRs() {
      listStatus.textContent = 'Loading...';
      formMessage.textContent = 'Changes are saved to SQLite first. Use Rewrite JSON to persist back to the flat file.';
      formMessage.className = 'status warn';

      try {
        const resp = await fetch('/security-nfrs/data');
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        state.items = await resp.json();

        // Consumed on the first load only, so the Reload DB button can't drag
        // the selection back to the deep-linked key after the user moves on.
        let jumped = false;
        if (pendingKey) {
          const requested = pendingKey;
          pendingKey = '';
          if (state.items.some((item) => item.key === requested)) {
            state.selectedKey = requested;
            jumped = true;
          } else {
            formMessage.textContent = 'Security NFR key ' + requested + ' is not in the database; showing the full list instead.';
            formMessage.className = 'status error';
          }
        }

        applySearch();

        if (jumped) {
          const active = rows.querySelector('.row.active');
          if (active) active.scrollIntoView({ block: 'center' });
        }
      } catch (err) {
        listStatus.textContent = 'Load failed';
        rows.innerHTML = '<div class="empty">The SQLite security NFR database could not be loaded.</div>';
        formMessage.textContent = 'Failed to load security NFRs: ' + err.message;
        formMessage.className = 'status error';
      }
    }

    editorForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!state.selectedKey) return;

      const payload = {
        id: nfrID.value.trim(),
        summary: nfrSummary.value.trim(),
        issue_type: nfrIssueType.value.trim(),
        description: nfrDescription.value.trim(),
        nist_mapping: nfrNISTMapping.value.trim(),
        additional_details: nfrAdditionalDetails.value.trim(),
        implementation: nfrImplementation.value.trim(),
        domain: nfrDomain.value.trim()
      };

      formMessage.textContent = 'Saving to SQLite...';
      formMessage.className = 'status warn';

      try {
        const resp = await fetch('/security-nfrs/' + encodeURIComponent(state.selectedKey), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));

        const idx = state.items.findIndex((item) => item.key === data.key);
        if (idx >= 0) state.items[idx] = data;
        formMessage.textContent = 'Saved key ' + data.key + ' to SQLite.';
        formMessage.className = 'status success';
        applySearch();
      } catch (err) {
        formMessage.textContent = 'Save failed: ' + err.message;
        formMessage.className = 'status error';
      }
    });

    exportBtn.addEventListener('click', async () => {
      const confirmed = window.confirm('Rewrite internal/data/NFR_incremental_keys_with_domain.json from current SQLite data?');
      if (!confirmed) return;

      formMessage.textContent = 'Rewriting JSON file...';
      formMessage.className = 'status warn';
      try {
        const resp = await fetch('/security-nfrs/save', { method: 'POST' });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        formMessage.textContent = 'Rewrote ' + data.saved + ' security NFRs to ' + data.path + '.';
        formMessage.className = 'status success';
      } catch (err) {
        formMessage.textContent = 'Rewrite failed: ' + err.message;
        formMessage.className = 'status error';
      }
    });

    deleteBtn.addEventListener('click', async () => {
      const item = selected();
      if (!item) return;
      if (!window.confirm('Delete key ' + item.key + ' from SQLite and future JSON rewrites?')) return;

      formMessage.textContent = 'Deleting security NFR...';
      formMessage.className = 'status warn';
      try {
        const resp = await fetch('/security-nfrs/' + encodeURIComponent(item.key), { method: 'DELETE' });
        if (!resp.ok) {
          let message = 'HTTP ' + resp.status;
          try {
            const data = await resp.json();
            message = data.error || message;
          } catch (_) {}
          throw new Error(message);
        }
        state.items = state.items.filter((entry) => entry.key !== item.key);
        state.selectedKey = '';
        formMessage.textContent = 'Deleted key ' + item.key + ' from SQLite. Rewrite JSON to persist.';
        formMessage.className = 'status success';
        applySearch();
      } catch (err) {
        formMessage.textContent = 'Delete failed: ' + err.message;
        formMessage.className = 'status error';
      }
    });

    reloadBtn.addEventListener('click', loadNFRs);
    searchInput.addEventListener('input', applySearch);

    loadNFRs();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
