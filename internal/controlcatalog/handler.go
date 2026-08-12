package controlcatalog

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/apiutil"
	"grc/internal/controlfile"
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

// slimControl strips the long-form fields from a control. Requirements,
// discussion, the NIST appendix and the related-control list account for the
// overwhelming majority of the catalog's bytes — an unfiltered list of all 1193
// controls is ~1.9 MB with them and ~150 KB without — and none of them are shown
// in the list pane. The detail pane fetches the full record for the one control
// it is displaying instead.
func slimControl(control Control) Control {
	control.Requirements = ""
	control.Discussion = ""
	control.RelatedControls = nil
	control.Appendix = nil
	control.Justification = ""
	control.PotentiallyCommonInheritable = ""
	// mapping_baselines is deliberately left alone. It looks like an obvious
	// candidate — four arrays per control, read only by the detail pane — but
	// measuring showed dropping it made the response ~3 KB *larger*: the zero
	// value marshals its four nil slices as "null", which is longer than the
	// mostly-empty "[]" they carry today.
	return control
}

func (h *Handler) ListControls(c *gin.Context) {
	search := c.Query("search")
	baseline := c.Query("baseline")
	cia := c.Query("cia")
	page := apiutil.ParsePagination(c)

	controls, err := h.service.List(search, baseline, cia)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list controls"})
		return
	}

	// Opt-in so existing API consumers keep the full payload they expect.
	if c.Query("slim") == "1" {
		slimmed := make([]Control, len(controls))
		for i, control := range controls {
			slimmed[i] = slimControl(control)
		}
		controls = slimmed
	}
	if page.Enabled {
		items, total := apiutil.PaginateSlice(controls, page)
		c.JSON(http.StatusOK, gin.H{
			"items":    items,
			"total":    total,
			"page":     page.Page,
			"per_page": page.PerPage,
		})
		return
	}

	c.JSON(http.StatusOK, controls)
}

// GetControl returns one full control record as JSON. It backs the detail pane,
// which needs the long-form fields the slim list omits.
func (h *Handler) GetControl(c *gin.Context) {
	controlID := strings.TrimSpace(c.Param("controlID"))
	if controlID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "controlID is required"})
		return
	}
	control, err := h.service.Get(controlID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "control not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load control"})
		return
	}
	c.JSON(http.StatusOK, control)
}

func (h *Handler) ExportControls(c *gin.Context) {
	search := c.Query("search")
	baseline := c.Query("baseline")
	cia := c.Query("cia")
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "excel")))

	controls, err := h.service.List(search, baseline, cia)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to export controls"})
		return
	}

	filenameSuffix := time.Now().UTC().Format("20060102_150405")
	switch format {
	case "csv":
		payload, err := buildControlsCSV(controls)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build CSV export"})
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="controls_%s.csv"`, filenameSuffix))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", payload)
	case "excel", "xls", "xml":
		payload, err := buildControlsExcelXML(controls)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build Excel export"})
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="controls_%s.xml"`, filenameSuffix))
		c.Data(http.StatusOK, "application/vnd.ms-excel; charset=utf-8", payload)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported export format"})
	}
}

func (h *Handler) ExportHierarchyRTF(c *gin.Context) {
	controlID := strings.TrimSpace(c.Query("controlID"))
	if controlID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "controlID is required"})
		return
	}

	control, err := h.service.Get(controlID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("hierarchy_%s.rtf", sanitizeFilename(control.ID))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/rtf; charset=utf-8", []byte(buildHierarchyRTF(control)))
}

func (h *Handler) FamilyJSONData(c *gin.Context) {
	family := strings.ToUpper(strings.TrimSpace(c.Query("family")))
	controls, err := h.service.ListByFamily(family)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load controls"})
		return
	}

	payload := make(map[string]controlfile.Entry)
	for _, control := range controls {
		recordKey := strings.TrimSpace(control.SourceKey)
		if recordKey == "" {
			recordKey = control.ID
		}
		payload[recordKey] = controlfile.Entry{
			NIST: controlfile.NISTEntry{
				ControlID: control.ID,
				Type:      control.Type,
				Name:      control.Name,
				Threats:   control.Threats,
				Baselines: baselinesToFlags(control.MappingBaselines),
				CIA: struct {
					Confidentiality bool   `json:"confidentiality"`
					Integrity       bool   `json:"integrity"`
					Availability    bool   `json:"availability"`
					Justification   string `json:"justification"`
				}{
					Confidentiality: control.Confidentiality != "",
					Integrity:       control.Integrity != "",
					Availability:    control.Availability != "",
					Justification:   control.Justification,
				},
				Requirements:    control.Requirements,
				Discussion:      control.Discussion,
				RelatedControls: control.RelatedControls,
			},
			Appendix: control.Appendix,
		}
	}

	c.JSON(http.StatusOK, payload)
}

type updateFamilyVisibilityRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) ListFamilyVisibility(c *gin.Context) {
	items, err := h.service.ListFamilySettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load family visibility"})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) UpdateFamilyVisibility(c *gin.Context) {
	family := c.Param("family")

	var req updateFamilyVisibilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	if err := h.service.SetFamilyVisibility(family, req.Enabled); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "required") {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

type updateControlRequest struct {
	SourceKey        string         `json:"source_key"`
	Type             string         `json:"type"`
	Name             string         `json:"name"`
	Threats          []string       `json:"threats"`
	MappingBaselines Baselines      `json:"mapping_baselines"`
	Confidentiality  bool           `json:"confidentiality"`
	Integrity        bool           `json:"integrity"`
	Availability     bool           `json:"availability"`
	Justification    string         `json:"justification"`
	Requirements     string         `json:"requirements"`
	Discussion       string         `json:"discussion"`
	RelatedControls  []string       `json:"related_controls"`
	Appendix         map[string]any `json:"appendix"`
}

func (h *Handler) UpdateControl(c *gin.Context) {
	controlID := c.Param("controlID")

	var req updateControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	updated, err := h.service.Update(controlID, Control{
		SourceKey:        req.SourceKey,
		Type:             req.Type,
		Name:             req.Name,
		Threats:          req.Threats,
		MappingBaselines: req.MappingBaselines,
		Confidentiality:  boolMarker(req.Confidentiality),
		Integrity:        boolMarker(req.Integrity),
		Availability:     boolMarker(req.Availability),
		Justification:    req.Justification,
		Requirements:     req.Requirements,
		Discussion:       req.Discussion,
		RelatedControls:  req.RelatedControls,
		Appendix:         req.Appendix,
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

func (h *Handler) SaveControlsToJSON(c *gin.Context) {
	total, path, err := h.service.SaveToJSON()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save controls to JSON"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"saved": total,
		"path":  path,
	})
}

func (h *Handler) DeleteControl(c *gin.Context) {
	controlID := c.Param("controlID")
	if err := h.service.Delete(controlID); err != nil {
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

func (h *Handler) DetailPage(c *gin.Context) {
	controlID := strings.TrimSpace(c.Param("controlID"))
	if controlID == "" {
		c.String(http.StatusBadRequest, "controlID is required")
		return
	}

	control, err := h.service.Get(controlID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.String(status, "failed to load control detail")
		return
	}

	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Control Detail · GRC</title>
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
    .summary-row {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px;
    }
    .card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 14px;
      background: rgba(255,255,255,0.86);
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
    .chips { display: flex; gap: 8px; flex-wrap: wrap; }
    .chip {
      border: 1px solid #000;
      border-radius: 999px;
      background: #000;
      color: #fff;
      padding: 6px 10px;
      font-family: "Courier New", monospace;
      font-size: 13px;
    }
    .nfr-card {
      background: #000;
    }
    .nfr-card h2 {
      color: #c8c8c8;
    }
    .nfr-link {
      display: block;
      border: 1px solid #444;
      border-radius: 12px;
      background: #000;
      padding: 10px 12px;
      color: #fff;
      text-decoration: none;
      margin-bottom: 8px;
    }
    .nfr-link:hover {
      background: #1a1a1a;
      border-color: #b89d78;
    }
    .nfr-card .text {
      color: #c8c8c8;
    }
    .nfr-meta {
      color: #c8c8c8;
      font-family: Arial, sans-serif;
      font-size: 12px;
      margin-top: 4px;
    }
    @media (max-width: 980px) {
      .summary-row { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main data-control-id="` + detailHTMLEscape(control.ID) + `">
    ` + pageui.Nav("/controls/detail/"+control.ID) + `

    <h1>` + detailHTMLEscape(control.ID) + ` - ` + detailHTMLEscape(control.Name) + `</h1>
    <div class="meta">Family ` + detailHTMLEscape(control.Family) + ` | Type ` + detailHTMLEscape(orNA(control.Type)) + `</div>

    <section class="grid">
      <div class="summary-row">
        <article class="card">
          <h2>Baselines</h2>
          <div class="chips">` + detailChipList(control.Baselines) + `</div>
        </article>
        <article class="card">
          <h2>CIA Classification</h2>
          <div class="text">Confidentiality: ` + detailHTMLEscape(orNA(control.Confidentiality)) + `
Integrity: ` + detailHTMLEscape(orNA(control.Integrity)) + `
Availability: ` + detailHTMLEscape(orNA(control.Availability)) + `</div>
        </article>
        <article class="card">
          <h2>Mapped Threats</h2>
          <div class="chips">` + detailChipList(control.Threats) + `</div>
        </article>
      </div>
      ` + detailTopDescriptionCard(control.Appendix) + `
      <article class="card nfr-card">
        <h2>Linked Security NFRs</h2>
        <div id="linkedNFRs"><div class="text">Loading linked Security NFRs...</div></div>
      </article>
      ` + detailNISTCardsHTML(control.Requirements, control.Discussion, control.Appendix) + `
      <article class="card">
        <h2>Justification</h2>
        <div class="text">` + detailHTMLEscape(orNA(control.Justification)) + `</div>
      </article>
      <article class="card">
        <h2>Related Controls</h2>
        <div class="chips">` + detailChipList(control.RelatedControls) + `</div>
      </article>
    </section>
  </main>
  <script>
    const controlID = document.querySelector('main')?.getAttribute('data-control-id') || '';
    const linkedNFRs = document.getElementById('linkedNFRs');

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    async function loadLinkedNFRs() {
      if (!controlID) {
        linkedNFRs.innerHTML = '<div class="text">No control ID available.</div>';
        return;
      }
      try {
        const resp = await fetch('/security-nfrs/links/data');
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        const rows = await resp.json();

        const target = controlID.trim().toUpperCase();
        const byNFR = new Map();
        (rows || []).forEach((row) => {
          if (!row || !row.matched) return;
          if (String(row.control_id || '').trim().toUpperCase() !== target) return;
          const key = String(row.nfr_key || row.nfr_id || row.nfr_summary || '').trim();
          if (!key || byNFR.has(key)) return;
          byNFR.set(key, row);
        });

        const items = Array.from(byNFR.values());
        items.sort((a, b) => {
          const leftID = parseFloat(String(a.nfr_id || '').trim());
          const rightID = parseFloat(String(b.nfr_id || '').trim());
          if (!Number.isNaN(leftID) && !Number.isNaN(rightID) && leftID !== rightID) return leftID - rightID;
          return String(a.nfr_id || '').localeCompare(String(b.nfr_id || ''), undefined, { numeric: true, sensitivity: 'base' });
        });

        if (!items.length) {
          linkedNFRs.innerHTML = '<div class="text">No linked Security NFRs.</div>';
          return;
        }

        linkedNFRs.innerHTML = items.map((item) => {
          const key = String(item.nfr_key || '').trim();
          const href = key ? '/security-nfrs/detail/' + encodeURIComponent(key) : '';
          const body =
            '<strong>' + esc(item.nfr_summary || 'No summary') + '</strong>' +
            '<div class="nfr-meta">Domain: ' + esc(item.nfr_domain || 'N/A') + '</div>';
          if (!href) return '<div class="nfr-link">' + body + '</div>';
          return '<a class="nfr-link" href="' + href + '" target="_blank" rel="noopener noreferrer">' + body + '</a>';
        }).join('');
      } catch (error) {
        linkedNFRs.innerHTML = '<div class="text">Failed to load linked Security NFRs: ' + esc(error.message || 'unknown error') + '</div>';
      }
    }

    loadLinkedNFRs();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func (h *Handler) Page(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>RCSA Control Database · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f1efe8;
      --panel: rgba(255,252,246,0.92);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
      --accent-dark: #6f2d22;
      --chip: #ece3d2;
      --chip-active: #8b3d2e;
      --chip-active-ink: #fff9f4;
      --cia-c: #0b5d3b;
      --cia-i: #9a6700;
      --cia-a: #8a1c4a;
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
	      max-width: min(1600px, calc(100vw - 32px));
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
      box-shadow: 0 24px 60px rgba(28,36,49,0.12);
      -webkit-backdrop-filter: blur(10px);
      backdrop-filter: blur(10px);
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
      grid-template-columns: minmax(240px, 1.2fr) minmax(220px, 1fr) minmax(220px, 1fr) auto auto auto;
      gap: 12px;
      align-items: center;
      margin: 24px 0 18px;
    }
    input {
      width: 100%;
      padding: 14px 16px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.95);
      font: inherit;
    }
    .chips, .subchips {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
    }
    .chip, .subchip {
      border: 1px solid transparent;
      padding: 10px 14px;
      border-radius: 999px;
      cursor: pointer;
      font: inherit;
      transition: background 140ms ease, border-color 140ms ease, box-shadow 140ms ease, transform 140ms ease;
    }
    .chip {
      background: var(--chip);
      color: var(--ink);
      border-color: rgba(184,157,120,0.35);
    }
    .chip:hover, .subchip:hover {
      transform: translateY(-1px);
    }
    .chip.active {
      background: #c48b54;
      color: var(--ink);
      border-color: #6f2d22;
      box-shadow: 0 10px 20px rgba(111,45,34,0.22);
      font-weight: 700;
    }
    .subchip {
      border-color: var(--line);
      background: rgba(255,255,255,0.92);
      color: var(--ink);
    }
	    .subchip.active {
        color: var(--ink);
        border-color: transparent;
        font-weight: 700;
        box-shadow: 0 10px 20px rgba(28,36,49,0.14);
      }
    .subchip[data-cia="c"].active {
      background: #96d0b6;
      border-color: var(--cia-c);
      box-shadow: 0 10px 20px rgba(11,93,59,0.16);
    }
    .subchip[data-cia="i"].active {
      background: #e6c57f;
      border-color: var(--cia-i);
      box-shadow: 0 10px 20px rgba(154,103,0,0.16);
    }
    .subchip[data-cia="a"].active {
      background: #d7a6ba;
      border-color: var(--cia-a);
      box-shadow: 0 10px 20px rgba(138,28,74,0.16);
    }
	    .button {
	      padding: 12px 16px;
	      border-radius: 999px;
	      border: 0;
	      background: #dfc4b4;
	      color: var(--ink);
	      cursor: pointer;
	      font: inherit;
	    }
	    .button:hover { background: #d1b09d; }
    .summary {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      flex-wrap: wrap;
      margin-bottom: 14px;
      color: var(--muted);
    }
    .layout {
      display: grid;
      grid-template-columns: minmax(0, 0.95fr) minmax(320px, 1.05fr);
      gap: 18px;
      min-height: 70vh;
    }
    .list, .detail {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 22px;
      background: rgba(255,255,255,0.84);
      overflow: hidden;
    }
    .list-header, .detail-header {
      padding: 16px 18px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
    }
    .detail-header {
      padding: 10px 14px;
      font-size: 13px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      font-family: Arial, sans-serif;
    }
    .rows {
      max-height: 72vh;
      overflow: auto;
    }
    /* iOS Safari sizes vh against the largest viewport, so a vh-capped pane
       runs under the dynamic toolbar; dvh tracks the visible height instead. */
    @supports (height: 100dvh) {
      .rows { max-height: 72dvh; }
    }
    .row {
      width: 100%;
      text-align: left;
      border: 0;
      border-bottom: 1px solid rgba(215,206,191,0.6);
      background: transparent;
      padding: 16px 18px;
      cursor: pointer;
      transition: background 160ms ease, transform 160ms ease;
    }
    .row:hover, .row.active {
      background: rgba(139,61,46,0.08);
    }
    .row.active {
      box-shadow: inset 4px 0 0 var(--accent);
    }
    .row-open-link, .detail-open-link {
      color: var(--ink);
      text-decoration: none;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 4px 8px;
      background: rgba(255,255,255,0.9);
      font-family: Arial, sans-serif;
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.06em;
    }
    .row-open-link:hover, .detail-open-link:hover {
      background: rgba(139,61,46,0.12);
      border-color: #b89d78;
    }
    .row-top {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      margin-bottom: 8px;
    }
    .row-id, .detail-id {
      font-family: "Courier New", monospace;
      font-weight: bold;
      letter-spacing: 0.04em;
    }
    .baseline-tags, .threat-tags {
      display: flex;
      gap: 6px;
      flex-wrap: wrap;
    }
    .tag {
      padding: 4px 8px;
      border-radius: 999px;
      background: rgba(139,61,46,0.1);
      color: var(--accent-dark);
      font-size: 12px;
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.04em;
    }
    .tag.cia-c { background: rgba(11,93,59,0.12); color: var(--cia-c); }
    .tag.cia-i { background: rgba(154,103,0,0.14); color: var(--cia-i); }
    .tag.cia-a { background: rgba(138,28,74,0.12); color: var(--cia-a); }
    /* The Control Record pane is one long stack of cards and was set at
       body-copy size throughout, so a single control ran several screens.
       Everything below is a deliberate density pass: the pane reads as a
       compact reference sheet, and "Open Full Detail" remains for long reads. */
    .detail-body {
      padding: 12px 12px 16px;
      font-size: 13px;
      line-height: 1.45;
      /* Matches .rows so the two panes end level. Without a cap, a control
         with a long NIST discussion stretched the page to many screens and
         left the list scrolled off the top. */
      max-height: 72vh;
      overflow: auto;
    }
    @supports (height: 100dvh) {
      .detail-body { max-height: 72dvh; }
    }
    .detail-grid {
      display: grid;
      gap: 10px;
    }
    .detail-body .detail-id { font-size: 14px; }
    .detail-body p { margin: 4px 0; }
    .detail-body .tag { font-size: 11px; padding: 3px 7px; }
    .detail-card {
      padding: 10px 12px;
      border-radius: 12px;
      background: #000;
      color: #fff;
      border: 1px solid #1f2937;
    }
    .detail-card h3 {
      margin: 0 0 6px;
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.07em;
      color: #cbd5e1;
      font-family: Arial, sans-serif;
    }
    .data-points {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(92px, 1fr));
      gap: 8px;
    }
    .metric {
      padding: 8px 10px;
      border-radius: 10px;
      background: #000;
      color: #fff;
      border: 1px solid #1f2937;
    }
    .metric h4 {
      margin: 0 0 4px;
      font-size: 10px;
      text-transform: uppercase;
      letter-spacing: 0.07em;
      color: #cbd5e1;
      font-family: Arial, sans-serif;
    }
	    .metric strong { font-size: 15px; }
	    .cia-card .metric {
	      background: #000;
	      color: #fff;
	      border-color: #1f2937;
	    }
	    .cia-card .metric h4 { color: #cbd5e1; }
    .mono-list {
      display: flex;
      gap: 6px;
      flex-wrap: wrap;
    }
    .mono {
      padding: 3px 7px;
      border-radius: 8px;
      background: white;
      border: 1px solid var(--line);
      font-family: "Courier New", monospace;
      font-size: 11.5px;
    }
    .mono-link {
      cursor: pointer;
      color: var(--ink);
      text-decoration: none;
    }
    .mono-link:hover {
      background: rgba(139,61,46,0.12);
      border-color: #b89d78;
    }
    .empty {
      padding: 36px 20px;
      color: var(--muted);
      text-align: center;
    }
    @media (max-width: 1360px) {
      .toolbar {
        grid-template-columns: minmax(220px, 1fr) minmax(220px, 1fr) minmax(220px, 1fr) auto auto;
      }
      #clearBtn {
        grid-column: 1 / -1;
        justify-self: start;
      }
    }
    @media (max-width: 1200px) {
      .toolbar {
        grid-template-columns: minmax(220px, 1fr) minmax(220px, 1fr);
      }
      .layout { grid-template-columns: 1fr; }
      .rows { max-height: 52vh; }
    }
    @media (max-width: 980px) {
      .toolbar, .layout { grid-template-columns: 1fr; }
      .rows { max-height: none; }
    }
    @media (max-width: 700px) {
      /* Element+class beats the global responsive layer's plain .toolbar rule,
         which stacks every child. Stacking all six children here costs a whole
         phone screen of filters before the first control is visible, so the
         three actions share one row and only the search and chips go full
         width. */
      section.toolbar {
        grid-template-columns: repeat(3, minmax(0, 1fr));
        gap: 8px;
        margin: 12px 0 12px;
      }
      section.toolbar > input,
      section.toolbar > .chips,
      section.toolbar > .subchips { grid-column: 1 / -1; }
      section.toolbar .button {
        padding: 10px 6px;
        font-size: 13px;
        font-family: Arial, sans-serif;
      }
      #clearBtn { grid-column: auto; justify-self: stretch; }
      .chip, .subchip { padding: 7px 12px; font-size: 14px; }
      h1 { font-size: 1.9rem; }
      main > p, .global-content > p { font-size: 14px; }

      /* Stacked on a phone, an uncapped list would bury the Control Record
         pane below hundreds of rows, so the list keeps its own scroll. */
      .rows { max-height: 52vh; }
      .list, .detail { border-radius: 14px; }
      .row { padding: 12px 14px; }
      .detail-body { padding: 10px 10px 14px; max-height: 60vh; }
      .summary { font-size: 13px; }
      .empty { padding: 24px 16px; }
    }
    @supports (height: 100dvh) {
      @media (max-width: 700px) {
        .rows { max-height: 52dvh; }
        .detail-body { max-height: 60dvh; }
      }
    }
  </style>
</head>
<body>
  <main>
	    ` + pageui.Nav("/controls") + `
    <h1>RCSA Control<br>Database</h1>
    <p>Unified SQLite view of the flat NIST control dataset, including names, baseline mappings, threats, and CIA classification.</p>

    <section class="toolbar">
      <input id="searchInput" type="search" placeholder="Search control id or family, e.g. AC-2 or SI">
      <div id="chips" class="chips"></div>
      <div id="ciaChips" class="subchips"></div>
      <button id="excelExportBtn" class="button" type="button">Export Excel</button>
      <button id="csvExportBtn" class="button" type="button">Export CSV</button>
      <button id="clearBtn" class="button" type="button">Reset</button>
    </section>

    <div class="summary">
      <div id="status">Loading controls...</div>
      <div id="selectionSummary">No control selected</div>
    </div>

    <section class="layout">
      <div class="list">
        <div class="list-header"><strong>Controls</strong></div>
        <div id="rows" class="rows"></div>
      </div>
      <aside class="detail">
        <div class="detail-header"><strong>Control Record</strong></div>
        <div id="detail" class="detail-body">
          <div class="empty">Choose a control to inspect the unified record.</div>
        </div>
      </aside>
    </section>
  </main>

  <script>
    const baselineOptions = ['', 'Low', 'Moderate', 'High', 'Privacy'];
    const ciaOptions = [
      { value: '', label: 'All CIA' },
      { value: 'c', label: 'C' },
      { value: 'i', label: 'I' },
      { value: 'a', label: 'A' }
    ];
    const state = {
      controls: [],
      search: '',
      baseline: '',
      cia: '',
      selectedId: ''
    };
    const nfrLinkState = {
      loaded: false,
      loadingPromise: null,
      rows: [],
      error: ''
    };

    const chips = document.getElementById('chips');
    const ciaChips = document.getElementById('ciaChips');
    const rows = document.getElementById('rows');
    const status = document.getElementById('status');
    const selectionSummary = document.getElementById('selectionSummary');
    const detail = document.getElementById('detail');
    const searchInput = document.getElementById('searchInput');
    const excelExportBtn = document.getElementById('excelExportBtn');
    const csvExportBtn = document.getElementById('csvExportBtn');
    const clearBtn = document.getElementById('clearBtn');
    const minVisibleControlRows = 10;

    function escapeHtml(value) {
      return String(value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function renderChips() {
      chips.innerHTML = baselineOptions.map((baseline) => {
        const label = baseline || 'All';
        const active = baseline === state.baseline ? 'active' : '';
        return '<button class="chip ' + active + '" data-baseline="' + label + '" type="button">' + label + '</button>';
      }).join('');

      chips.querySelectorAll('.chip').forEach((chip) => {
        chip.addEventListener('click', () => {
          const value = chip.getAttribute('data-baseline');
          state.baseline = value === 'All' ? '' : value;
          fetchControls();
        });
      });
    }

    function renderCIAChips() {
      ciaChips.innerHTML = ciaOptions.map((item) => {
        const active = item.value === state.cia ? 'active' : '';
        return '<button class="subchip ' + active + '" data-cia="' + item.value + '" type="button">' + item.label + '</button>';
      }).join('');

      ciaChips.querySelectorAll('.subchip').forEach((chip) => {
        chip.addEventListener('click', () => {
          state.cia = chip.getAttribute('data-cia');
          fetchControls();
        });
      });
    }

    function renderCIA(control) {
      const tags = [];
      if (control.confidentiality) tags.push('<span class="tag cia-c">C</span>');
      if (control.integrity) tags.push('<span class="tag cia-i">I</span>');
      if (control.availability) tags.push('<span class="tag cia-a">A</span>');
      return tags.join('') || '<span class="tag">No CIA flag</span>';
    }

    const familyNames = {
      AC: 'Access Control',
      AT: 'Awareness and Training',
      AU: 'Audit and Accountability',
      CA: 'Assessment, Authorization, and Monitoring',
      CM: 'Configuration Management',
      CP: 'Contingency Planning',
      IA: 'Identification and Authentication',
      IR: 'Incident Response',
      MA: 'Maintenance',
      MP: 'Media Protection',
      PE: 'Physical and Environmental Protection',
      PL: 'Planning',
      PM: 'Program Management',
      PS: 'Personnel Security',
      PT: 'Personally Identifiable Information Processing and Transparency',
      RA: 'Risk Assessment',
      SA: 'System and Services Acquisition',
      SC: 'System and Communications Protection',
      SI: 'System and Information Integrity',
      SR: 'Supply Chain Risk Management'
    };

    function familyLabel(family) {
      return familyNames[family] || family || 'Unknown family';
    }

    function ensureVisibleRowCount(container, rowSelector, minimumRows) {
      if (!container) return;
      const items = Array.from(container.querySelectorAll(rowSelector));
      if (!items.length) {
        container.style.removeProperty('min-height');
        return;
      }

      const requiredHeight = items
        .slice(0, minimumRows)
        .reduce((sum, item) => sum + item.getBoundingClientRect().height, 0);
      if (!requiredHeight) {
        container.style.removeProperty('min-height');
        return;
      }

      container.style.minHeight = Math.ceil(requiredHeight) + 'px';
    }

    async function ensureNFRLinksLoaded() {
      if (nfrLinkState.loaded) return;
      if (nfrLinkState.loadingPromise) {
        await nfrLinkState.loadingPromise;
        return;
      }

      nfrLinkState.loadingPromise = (async () => {
        try {
          const resp = await fetch('/security-nfrs/links/data');
          if (!resp.ok) throw new Error('HTTP ' + resp.status);
          nfrLinkState.rows = await resp.json();
          nfrLinkState.error = '';
        } catch (error) {
          nfrLinkState.rows = [];
          nfrLinkState.error = error.message || 'unknown error';
        } finally {
          nfrLinkState.loaded = true;
        }
      })();

      await nfrLinkState.loadingPromise;
    }

    function linkedNFRsForControl(controlID) {
      const target = String(controlID || '').trim().toUpperCase();
      const byNFR = new Map();
      (nfrLinkState.rows || []).forEach((row) => {
        if (!row || !row.matched) return;
        const rowControlID = String(row.control_id || '').trim().toUpperCase();
        if (!rowControlID || rowControlID !== target) return;

        const key = String(row.nfr_key || row.nfr_id || row.nfr_summary || '').trim();
        if (!key || byNFR.has(key)) return;
        byNFR.set(key, row);
      });

      const items = Array.from(byNFR.values());
      items.sort((a, b) => {
        const leftID = parseFloat(String(a.nfr_id || '').trim());
        const rightID = parseFloat(String(b.nfr_id || '').trim());
        if (!Number.isNaN(leftID) && !Number.isNaN(rightID) && leftID !== rightID) {
          return leftID - rightID;
        }
        return String(a.nfr_id || '').localeCompare(String(b.nfr_id || ''), undefined, { numeric: true, sensitivity: 'base' });
      });
      return items;
    }

    function renderLinkedNFRContent(controlID) {
      if (nfrLinkState.error) {
        return '<p>Failed to load linked Security NFRs: ' + escapeHtml(nfrLinkState.error) + '</p>';
      }

      const items = linkedNFRsForControl(controlID);
      if (!items.length) {
        return '<p>No linked Security NFRs.</p>';
      }

      return '<div style="display:grid;gap:8px;">' + items.map((item) => {
        const summaryLabel = item.nfr_summary || 'No summary';
        const domainLabel = item.nfr_domain || 'N/A';
        const key = String(item.nfr_key || '').trim();
        const body =
          '<strong>' + escapeHtml(summaryLabel) + '</strong>' +
          '<br><span style="color:var(--muted);font-family:Arial,sans-serif;font-size:12px;">Domain: ' + escapeHtml(domainLabel) + '</span>';
        if (!key) {
          return '<div class="mono">' + body + '</div>';
        }
        return '<a class="mono mono-link" href="/security-nfrs/detail/' + encodeURIComponent(key) + '" target="_blank" rel="noopener noreferrer">' + body + '</a>';
      }).join('') + '</div>';
    }

    function renderRows() {
      if (!state.controls.length) {
        rows.innerHTML = '<div class="empty">No controls match the current filter.</div>';
        rows.style.removeProperty('min-height');
        renderDetail(null);
        return;
      }

      rows.innerHTML = state.controls.map((control) => {
        const active = control.id === state.selectedId ? 'active' : '';
        const threats = (control.threats || []).map((item) => '<span class="tag">' + escapeHtml(item) + '</span>').join('');
        const baselines = (control.baselines || []).map((item) => '<span class="tag">' + escapeHtml(item) + '</span>').join('');
        return '<div class="row ' + active + '" data-id="' + escapeHtml(control.id) + '" role="button" tabindex="0">' +
          '<div class="row-top">' +
          '<span class="row-id">' + escapeHtml(control.id) + '</span>' +
          '<span style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;">' +
            '<span class="tag">' + escapeHtml(control.family) + '</span>' +
            '<span class="tag">' + escapeHtml(control.type || 'Control') + '</span>' +
            '<a class="row-open-link" href="/controls/detail/' + encodeURIComponent(control.id) + '" target="_blank" rel="noopener noreferrer">Open Detail</a>' +
          '</span>' +
          '</div>' +
          '<div><strong>' + escapeHtml(control.name || 'Unnamed control') + '</strong> <span style="color:var(--muted);">(' + escapeHtml(familyLabel(control.family)) + ')</span></div>' +
          '<div class="baseline-tags">' + baselines + '</div>' +
          '<div class="threat-tags" style="margin-top:10px;">' + renderCIA(control) + '</div>' +
          '<div class="threat-tags" style="margin-top:10px;">' + (threats || '<span class="tag">No STRIDE mapping</span>') + '</div>' +
          '</div>';
      }).join('');

      rows.querySelectorAll('.row').forEach((row) => {
        row.addEventListener('click', () => {
          state.selectedId = row.getAttribute('data-id');
          renderRows();
          renderDetail(state.controls.find((item) => item.id === state.selectedId) || null);
        });
        row.addEventListener('keydown', (event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return;
          event.preventDefault();
          state.selectedId = row.getAttribute('data-id');
          renderRows();
          renderDetail(state.controls.find((item) => item.id === state.selectedId) || null);
        });
      });

      rows.querySelectorAll('.row-open-link').forEach((link) => {
        link.addEventListener('click', (event) => {
          event.stopPropagation();
        });
      });

      // Ten rows is a sensible floor on a desktop split view; on a phone the
      // same floor is taller than the screen and pushes the detail pane out
      // of reach, so the list keeps a shorter minimum there.
      const minRows = window.matchMedia('(max-width: 700px)').matches ? 3 : minVisibleControlRows;
      ensureVisibleRowCount(rows, '.row', minRows);
    }

    async function renderDetail(control) {
      if (!control) {
        detail.innerHTML = '<div class="empty">Choose a control to inspect the unified record.</div>';
        selectionSummary.textContent = 'No control selected';
        return;
      }

      selectionSummary.textContent = 'Selected: ' + control.id;

      // The list payload omits the long-form NIST fields, so hydrate the one
      // control being shown. On failure the pane still renders from the slim
      // record — a missing discussion is better than a blank panel.
      const requestedID = control.id;
      try {
        control = await loadFullControl(requestedID);
      } catch (error) {
        // Fall through with the slim record. The long-form cards will read
        // "None", which is a better outcome than a blank pane.
        status.textContent = 'Showing ' + state.controls.length +
          ' controls (full record for ' + requestedID + ' unavailable)';
      }
      // The selection can change while the fetch is in flight.
      if (state.selectedId !== requestedID) {
        return;
      }
      const baselineLists = control.mapping_baselines || {};
      detail.innerHTML =
        '<div class="detail-grid">' +
          '<div>' +
            '<div class="detail-id">' + escapeHtml(control.id) + ' <a class="detail-open-link" href="/controls/detail/' + encodeURIComponent(control.id) + '" target="_blank" rel="noopener noreferrer">Open Full Detail</a></div>' +
            '<p><strong>' + escapeHtml(control.name || 'Unnamed control') + '</strong> <span style="color:var(--muted);">(' + escapeHtml(familyLabel(control.family)) + ')</span></p>' +
            '<p>Family ' + escapeHtml(control.family) + ' · Type ' + escapeHtml(control.type || 'Control') + '</p>' +
          '</div>' +
	          '<div class="detail-card cia-card">' +
	            '<h3>CIA Classification</h3>' +
	            '<div class="data-points">' +
              '<div class="metric"><h4>Confidentiality</h4><strong>' + escapeHtml(control.confidentiality || '-') + '</strong></div>' +
              '<div class="metric"><h4>Integrity</h4><strong>' + escapeHtml(control.integrity || '-') + '</strong></div>' +
              '<div class="metric"><h4>Availability</h4><strong>' + escapeHtml(control.availability || '-') + '</strong></div>' +
            '</div>' +
          '</div>' +
          '<div class="detail-card">' +
            '<h3>Baselines</h3>' +
            '<div class="baseline-tags">' + (control.baselines || []).map((item) => '<span class="tag">' + escapeHtml(item) + '</span>').join('') + '</div>' +
          '</div>' +
          '<div class="detail-card">' +
            '<h3>Mapped Threats</h3>' +
            '<div class="threat-tags">' + ((control.threats || []).length ? control.threats.map((item) => '<span class="tag">' + escapeHtml(item) + '</span>').join('') : '<span class="tag">No STRIDE mapping</span>') + '</div>' +
          '</div>' +
          '<div class="detail-card">' +
            '<h3>Linked Security NFRs</h3>' +
            '<div id="linkedNFRsBody"><p>Loading linked Security NFRs...</p></div>' +
          '</div>' +
          '<div class="detail-card">' +
            '<h3>Baseline Entries In Mapping JSON</h3>' +
            baselineSection('Low', baselineLists.Low) +
            baselineSection('Moderate', baselineLists.Moderate) +
            baselineSection('High', baselineLists.High) +
            baselineSection('Privacy', baselineLists.Privacy) +
          '</div>' +
	          '<div class="detail-card">' +
	            '<h3>Source Notes</h3>' +
	            '<p><strong>Justification:</strong> ' + escapeHtml(control.justification || 'None') + '</p>' +
	            '<p><strong>Related Controls:</strong> ' + ((control.related_controls || []).length ? control.related_controls.map((item) =>
                '<button class="mono mono-link related-control-link" data-id="' + escapeHtml(item) + '" type="button">' + escapeHtml(item) + '</button>'
              ).join(' ') : 'None') + '</p>' +
	          '</div>' +
	          nistSectionCards(control.requirements, control.discussion, control.appendix) +
        '</div>';

      const selectedControlID = control.id;
      await ensureNFRLinksLoaded();
      if (state.selectedId !== selectedControlID) {
        return;
      }
      const linkedNFRsBody = detail.querySelector('#linkedNFRsBody');
      if (linkedNFRsBody) {
        linkedNFRsBody.innerHTML = renderLinkedNFRContent(selectedControlID);
      }

      detail.querySelectorAll('.related-control-link').forEach((link) => {
        link.addEventListener('click', () => {
          const controlID = link.getAttribute('data-id');
          state.search = controlID;
          searchInput.value = controlID;
          state.selectedId = controlID;
          fetchControls();
        });
      });
    }

    function baselineSection(label, values) {
      const items = values || [];
      const body = items.length
        ? '<div class="mono-list">' + items.map((item) => '<span class="mono">' + escapeHtml(item) + '</span>').join('') + '</div>'
        : '<p>No entries</p>';
      return '<div style="margin-bottom:8px;"><strong>' + escapeHtml(label) + '</strong>' + body + '</div>';
    }

    function appendixValueText(value) {
      if (value === null || value === undefined) return '';
      if (typeof value === 'string') return value;
      return JSON.stringify(value, null, 2);
    }

    function functionalRequirementBullets(value) {
      const text = String(value || '').replaceAll('\r\n', '\n').replaceAll('\r', '\n');
      return text
        .split(/[\n;]+/)
        .map((item) => item.trim())
        .filter(Boolean);
    }

    function appendixSection(value) {
      if (!value || typeof value !== 'object') {
        return '';
      }
      const keys = Object.keys(value);
      if (!keys.length) {
        return '';
      }
      keys.sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }));
      return '<div style="display:grid;gap:8px;">' + keys.map((key) => {
        const rawValue = appendixValueText(value[key]);
        const isFunctionalRequirements = key.trim().toLowerCase() === 'functional requirements';
        const body = isFunctionalRequirements
          ? (function() {
              const bullets = functionalRequirementBullets(rawValue);
              if (!bullets.length) return '<div style="white-space:pre-wrap;">N/A</div>';
              return '<ul style="margin:0 0 0 20px;padding:0;">' +
                bullets.map((item) => '<li style="margin:0 0 6px 0;white-space:pre-wrap;">' + escapeHtml(item) + '</li>').join('') +
                '</ul>';
            })()
          : '<div style="white-space:pre-wrap;">' + escapeHtml(rawValue) + '</div>';
        return '<div class="metric">' +
          '<h4>' + escapeHtml(key) + '</h4>' +
          body +
        '</div>';
      }).join('') + '</div>';
    }

    function nistSectionCards(requirements, discussion, appendix) {
      const cards = [];
      cards.push('<div class="detail-card"><h3>NIST Requirements</h3><div style="white-space:pre-wrap;">' + escapeHtml(requirements || 'None') + '</div></div>');
      cards.push('<div class="detail-card"><h3>NIST Discussion</h3><div style="white-space:pre-wrap;">' + escapeHtml(discussion || 'None') + '</div></div>');
      const appendixBody = appendixSection(appendix);
      if (appendixBody) {
        cards.push('<div class="detail-card"><h3>NIST Details</h3>' + appendixBody + '</div>');
      }
      return cards.join('');
    }

    async function fetchControls() {
      const params = currentFilterParams();

      status.textContent = 'Loading controls...';
      const query = params.toString();
      const url = '/controls/data?slim=1' + (query ? '&' + query : '');

      try {
        const response = await fetch(url);
        if (!response.ok) throw new Error('HTTP ' + response.status);
        state.controls = await response.json();
        if (!state.controls.find((item) => item.id === state.selectedId)) {
          state.selectedId = state.controls.length ? state.controls[0].id : '';
        }
        status.textContent = 'Showing ' + state.controls.length + ' controls';
        renderRows();
        renderDetail(state.controls.find((item) => item.id === state.selectedId) || null);
      } catch (error) {
        status.textContent = 'Failed to load controls: ' + error.message;
        rows.innerHTML = '<div class="empty">The control catalog could not be loaded.</div>';
        renderDetail(null);
      }
    }

    function currentFilterParams() {
      const params = new URLSearchParams();
      if (state.search) params.set('search', state.search);
      if (state.baseline) params.set('baseline', state.baseline);
      if (state.cia) params.set('cia', state.cia);
      return params;
    }

    // The list is fetched without the long-form NIST fields; the detail pane
    // pulls the full record for whichever control is selected. Cached because
    // clicking back and forth between two controls is the common interaction.
    const fullControlCache = new Map();
    async function loadFullControl(controlID) {
      if (fullControlCache.has(controlID)) return fullControlCache.get(controlID);
      const response = await fetch('/controls/data/' + encodeURIComponent(controlID));
      if (!response.ok) throw new Error('HTTP ' + response.status);
      const control = await response.json();
      fullControlCache.set(controlID, control);
      return control;
    }

    function exportControls(format) {
      const params = currentFilterParams();
      params.set('format', format);
      window.location.href = '/controls/export?' + params.toString();
    }

    // Debounced. Each keystroke used to fire a full catalog fetch, so typing
    // "AC-2" cost four round trips; the unfiltered response is the whole
    // catalog, which is the single largest payload the app serves.
    let searchTimer = null;
    searchInput.addEventListener('input', () => {
      clearTimeout(searchTimer);
      searchTimer = setTimeout(() => {
        state.search = searchInput.value.trim();
        fetchControls();
      }, 250);
    });

    clearBtn.addEventListener('click', () => {
      clearTimeout(searchTimer);
      state.search = '';
      state.baseline = '';
      state.cia = '';
      state.selectedId = '';
      searchInput.value = '';
      renderChips();
      renderCIAChips();
      fetchControls();
    });

    excelExportBtn.addEventListener('click', () => exportControls('excel'));
    csvExportBtn.addEventListener('click', () => exportControls('csv'));

    renderChips();
    renderCIAChips();
    fetchControls();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func buildControlsCSV(controls []Control) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	header := []string{
		"Control ID",
		"Name",
		"Family",
		"Baselines",
		"Threats",
		"Confidentiality",
		"Integrity",
		"Availability",
		"Justification",
		"Requirements",
		"Discussion",
		"Related Controls",
		"Low Baseline IDs",
		"Moderate Baseline IDs",
		"High Baseline IDs",
		"Privacy Baseline IDs",
		"Appendix JSON",
	}
	if err := writer.Write(header); err != nil {
		return nil, err
	}

	for _, control := range controls {
		row := []string{
			control.ID,
			control.Name,
			control.Family,
			strings.Join(control.Baselines, "; "),
			strings.Join(control.Threats, "; "),
			boolText(control.Confidentiality != ""),
			boolText(control.Integrity != ""),
			boolText(control.Availability != ""),
			control.Justification,
			control.Requirements,
			control.Discussion,
			strings.Join(control.RelatedControls, "; "),
			strings.Join(control.MappingBaselines.Low, "; "),
			strings.Join(control.MappingBaselines.Moderate, "; "),
			strings.Join(control.MappingBaselines.High, "; "),
			strings.Join(control.MappingBaselines.Privacy, "; "),
			appendixJSONString(control.Appendix),
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func buildControlsExcelXML(controls []Control) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0"?>` + "\n")
	buf.WriteString(`<?mso-application progid="Excel.Sheet"?>` + "\n")
	buf.WriteString(`<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">` + "\n")
	buf.WriteString(`<Worksheet ss:Name="Controls"><Table>` + "\n")

	header := []string{
		"Control ID",
		"Name",
		"Family",
		"Baselines",
		"Threats",
		"Confidentiality",
		"Integrity",
		"Availability",
		"Justification",
		"Requirements",
		"Discussion",
		"Related Controls",
		"Low Baseline IDs",
		"Moderate Baseline IDs",
		"High Baseline IDs",
		"Privacy Baseline IDs",
		"Appendix JSON",
	}
	if err := writeExcelRow(&buf, header); err != nil {
		return nil, err
	}

	for _, control := range controls {
		if err := writeExcelRow(&buf, []string{
			control.ID,
			control.Name,
			control.Family,
			strings.Join(control.Baselines, "; "),
			strings.Join(control.Threats, "; "),
			boolText(control.Confidentiality != ""),
			boolText(control.Integrity != ""),
			boolText(control.Availability != ""),
			control.Justification,
			control.Requirements,
			control.Discussion,
			strings.Join(control.RelatedControls, "; "),
			strings.Join(control.MappingBaselines.Low, "; "),
			strings.Join(control.MappingBaselines.Moderate, "; "),
			strings.Join(control.MappingBaselines.High, "; "),
			strings.Join(control.MappingBaselines.Privacy, "; "),
			appendixJSONString(control.Appendix),
		}); err != nil {
			return nil, err
		}
	}

	buf.WriteString(`</Table></Worksheet></Workbook>` + "\n")
	return buf.Bytes(), nil
}

func writeExcelRow(buf *bytes.Buffer, values []string) error {
	buf.WriteString("<Row>")
	for _, value := range values {
		buf.WriteString(`<Cell><Data ss:Type="String">`)
		if err := xml.EscapeText(buf, []byte(value)); err != nil {
			return err
		}
		buf.WriteString(`</Data></Cell>`)
	}
	buf.WriteString("</Row>\n")
	return nil
}

func boolText(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func appendixJSONString(appendix map[string]any) string {
	if len(appendix) == 0 {
		return ""
	}
	raw, err := json.Marshal(appendix)
	if err != nil {
		return ""
	}
	return string(raw)
}

var detailHTMLReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
)

func detailHTMLEscape(value string) string {
	return detailHTMLReplacer.Replace(value)
}

func detailChipList(items []string) string {
	if len(items) == 0 {
		return `<span class="chip">N/A</span>`
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		parts = append(parts, `<span class="chip">`+detailHTMLEscape(trimmed)+`</span>`)
	}
	if len(parts) == 0 {
		return `<span class="chip">N/A</span>`
	}
	return strings.Join(parts, "")
}

func orNA(value string) string {
	if strings.TrimSpace(value) == "" {
		return "N/A"
	}
	return value
}

func detailNISTCardsHTML(requirements, discussion string, appendix map[string]any) string {
	keys := make([]string, 0, len(appendix))
	for key := range appendix {
		if strings.EqualFold(strings.TrimSpace(key), "Control Description") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(`<article class="card"><h2>NIST Discussion</h2><div class="text">`)
	b.WriteString(detailHTMLEscape(orNA(discussion)))
	b.WriteString(`</div></article>`)

	requirementsRendered := false
	for _, key := range keys {
		if !requirementsRendered && strings.EqualFold(strings.TrimSpace(key), "Related NIST Controls") {
			b.WriteString(`<article class="card"><h2>NIST Requirements</h2><div class="text">`)
			b.WriteString(detailHTMLEscape(orNA(requirements)))
			b.WriteString(`</div></article>`)
			requirementsRendered = true
		}

		value := appendix[key]
		valueText := ""
		switch v := value.(type) {
		case nil:
			valueText = ""
		case string:
			valueText = v
		default:
			raw, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				valueText = fmt.Sprintf("%v", v)
			} else {
				valueText = string(raw)
			}
		}
		b.WriteString(`<article class="card"><h2>`)
		b.WriteString(detailHTMLEscape(key))
		b.WriteString(`</h2>`)
		if strings.EqualFold(strings.TrimSpace(key), "Functional Requirements") {
			bullets := functionalRequirementBullets(valueText)
			if len(bullets) == 0 {
				b.WriteString(`<div class="text">N/A</div>`)
			} else {
				b.WriteString(`<ul style="margin:0 0 0 20px;padding:0;">`)
				for _, bullet := range bullets {
					b.WriteString(`<li class="text" style="margin:0 0 6px 0;">`)
					b.WriteString(detailHTMLEscape(bullet))
					b.WriteString(`</li>`)
				}
				b.WriteString(`</ul>`)
			}
		} else {
			b.WriteString(`<div class="text">`)
			b.WriteString(detailHTMLEscape(valueText))
			b.WriteString(`</div>`)
		}
		b.WriteString(`</article>`)
	}

	if !requirementsRendered {
		b.WriteString(`<article class="card"><h2>NIST Requirements</h2><div class="text">`)
		b.WriteString(detailHTMLEscape(orNA(requirements)))
		b.WriteString(`</div></article>`)
	}
	return b.String()
}

func detailTopDescriptionCard(appendix map[string]any) string {
	keys := make([]string, 0, len(appendix))
	for key := range appendix {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if !strings.EqualFold(strings.TrimSpace(key), "Control Description") {
			continue
		}
		value := appendix[key]
		valueText := ""
		switch v := value.(type) {
		case nil:
			valueText = "N/A"
		case string:
			valueText = strings.TrimSpace(v)
			if valueText == "" {
				valueText = "N/A"
			}
		default:
			raw, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				valueText = fmt.Sprintf("%v", v)
			} else {
				valueText = string(raw)
			}
		}
		return `<article class="card"><h2>Control Description</h2><div class="text">` + detailHTMLEscape(valueText) + `</div></article>`
	}
	return ""
}

func functionalRequirementBullets(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == ';'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func (h *Handler) FamilyVisibilityPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Family Filter Visibility · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.94);
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
    .status { margin: 16px 0; color: var(--muted); }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: rgba(255,255,255,0.84);
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
    .rows { display: grid; }
    .row {
      display: grid;
      grid-template-columns: minmax(120px, 160px) minmax(0, 1fr) auto;
      gap: 12px;
      align-items: center;
      padding: 14px 16px;
      border-bottom: 1px solid rgba(215,206,191,0.6);
    }
    .row:last-child { border-bottom: 0; }
    .family {
      font-family: "Courier New", monospace;
      font-weight: 700;
    }
    .family-name { color: var(--muted); }
    .toggle {
      appearance: none;
      width: 52px;
      height: 30px;
      border-radius: 999px;
      border: 1px solid #c7b7a0;
      background: #e8ddd0;
      position: relative;
      cursor: pointer;
      transition: background 140ms ease, border-color 140ms ease;
    }
    .toggle::after {
      content: "";
      position: absolute;
      top: 3px;
      left: 3px;
      width: 22px;
      height: 22px;
      border-radius: 50%;
      background: #fff;
      box-shadow: 0 1px 4px rgba(28,36,49,0.28);
      transition: transform 140ms ease;
    }
    .toggle:checked {
      background: #9dce9f;
      border-color: #4f8d52;
    }
    .toggle:checked::after { transform: translateX(22px); }
    .empty { padding: 26px 18px; color: var(--muted); text-align: center; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/controls/family-visibility") + `
    <h1>Family Filter Visibility</h1>
    <p>Toggle control families on or off. Disabled families will not appear in filter-driven pages that use the control dataset.</p>

    <div id="status" class="status">Loading family visibility...</div>
    <section class="panel">
      <div class="panel-header">Control Families</div>
      <div id="rows" class="rows"></div>
    </section>
  </main>

  <script>
    const rows = document.getElementById('rows');
    const status = document.getElementById('status');

    function escapeHtml(value) {
      return String(value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    async function updateFamily(family, enabled) {
      const response = await fetch('/controls/family-visibility/' + encodeURIComponent(family), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled })
      });
      if (!response.ok) {
        let message = 'HTTP ' + response.status;
        try {
          const data = await response.json();
          message = data.error || message;
        } catch (_) {}
        throw new Error(message);
      }
    }

    function render(items) {
      if (!items.length) {
        rows.innerHTML = '<div class="empty">No control families found.</div>';
        return;
      }
      rows.innerHTML = items.map((item) => {
        const checked = item.enabled ? 'checked' : '';
        return '<label class="row">' +
          '<div class="family">' + escapeHtml(item.family) + '</div>' +
          '<div class="family-name">' + escapeHtml(item.name || 'Unknown family') + '</div>' +
          '<input class="toggle" type="checkbox" data-family="' + escapeHtml(item.family) + '" ' + checked + '>' +
        '</label>';
      }).join('');

      rows.querySelectorAll('.toggle').forEach((toggle) => {
        toggle.addEventListener('change', async () => {
          const family = toggle.getAttribute('data-family');
          const enabled = toggle.checked;
          status.textContent = (enabled ? 'Enabling ' : 'Disabling ') + family + '...';
          try {
            await updateFamily(family, enabled);
            status.textContent = 'Saved visibility for ' + family + '.';
          } catch (error) {
            toggle.checked = !enabled;
            status.textContent = 'Failed to update ' + family + ': ' + error.message;
          }
        });
      });
    }

    async function load() {
      status.textContent = 'Loading family visibility...';
      try {
        const response = await fetch('/controls/family-visibility/data');
        if (!response.ok) throw new Error('HTTP ' + response.status);
        const items = await response.json();
        render(items);
        status.textContent = 'Loaded ' + items.length + ' families.';
      } catch (error) {
        rows.innerHTML = '<div class="empty">Family visibility data unavailable.</div>';
        status.textContent = 'Failed to load family visibility: ' + error.message;
      }
    }

    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func (h *Handler) HierarchyPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Control Hierarchy · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f1efe8;
      --panel: rgba(255,252,246,0.94);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
      --chip: #ece3d2;
      --chip-active: #e1d0b7;
      --chip-border: #b89d78;
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
    .tab.active { background: var(--chip-active); border-color: var(--chip-border); }
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.5rem); line-height: 0.95; letter-spacing: -0.03em; }
    p { color: var(--muted); }
    .toolbar {
      display: grid;
      grid-template-columns: minmax(260px, 1fr) auto auto;
      gap: 12px;
      margin: 22px 0 16px;
      align-items: center;
    }
    input, button {
      padding: 12px 14px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: white;
      color: var(--ink);
      font: inherit;
    }
    button {
      background: #dfc4b4;
      cursor: pointer;
    }
    button:hover { background: #d1b09d; }
    .status { color: var(--muted); margin-bottom: 14px; }
    .layout {
      display: grid;
      grid-template-columns: minmax(220px, 0.7fr) minmax(260px, 0.9fr) minmax(260px, 0.9fr) minmax(320px, 1.1fr);
      gap: 16px;
      min-height: 70vh;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 20px;
      background: rgba(255,255,255,0.84);
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
    .rows { max-height: 74vh; overflow: auto; }
    .row {
      width: 100%;
      text-align: left;
      border: 0;
      border-bottom: 1px solid rgba(215,206,191,0.6);
      background: transparent;
      padding: 14px 16px;
      color: var(--ink);
      cursor: pointer;
    }
    .row:hover, .row.active { background: rgba(139,61,46,0.08); }
    .row.active { box-shadow: inset 4px 0 0 var(--accent); }
    .row-title { font-weight: bold; }
    .row-sub { color: var(--muted); margin-top: 4px; font-size: 14px; }
    .detail { padding: 18px; display: grid; gap: 14px; }
    .detail-card {
      padding: 14px;
      border-radius: 16px;
      background: #fbf8f2;
      border: 1px solid rgba(215,206,191,0.85);
    }
    .detail-card h3 {
      margin: 0 0 10px;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: var(--muted);
      font-family: Arial, sans-serif;
    }
    .tags, .mono-list { display: flex; gap: 8px; flex-wrap: wrap; }
    .tag, .mono {
      padding: 6px 8px;
      border-radius: 10px;
      border: 1px solid var(--line);
      background: white;
      font-size: 13px;
    }
    .mono { font-family: "Courier New", monospace; }
    .mono-link {
      cursor: pointer;
      color: var(--ink);
      text-decoration: none;
    }
    .mono-link:hover {
      background: rgba(139,61,46,0.12);
      border-color: var(--chip-border);
    }
    .empty { padding: 26px 18px; color: var(--muted); text-align: center; }
    @media (max-width: 1160px) {
      .layout { grid-template-columns: 1fr; }
      .rows { max-height: none; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/controls/hierarchy") + `
    <h1>Control Hierarchy</h1>
    <p>Select controls hierarchically from control family to base control to control enhancement.</p>

    <div class="toolbar">
      <input id="searchInput" type="search" placeholder="Filter families, controls, or enhancements">
      <button id="exportBtn" type="button" disabled>Export RTF</button>
      <button id="reloadBtn" type="button">Reload</button>
    </div>
    <div id="status" class="status">Loading hierarchy...</div>

    <section class="layout">
      <div class="panel">
        <div class="panel-header">Control Family</div>
        <div id="familyRows" class="rows"></div>
      </div>
      <div class="panel">
        <div class="panel-header">Control</div>
        <div id="controlRows" class="rows"></div>
      </div>
      <div class="panel">
        <div class="panel-header">Control Enhancement</div>
        <div id="enhancementRows" class="rows"></div>
      </div>
      <aside class="panel">
        <div class="panel-header">Selection Details</div>
        <div id="detail" class="detail">
          <div class="empty">Choose a family, then a control or enhancement.</div>
        </div>
      </aside>
    </section>
  </main>

  <script>
    const state = {
      controls: [],
      search: '',
      selectedFamily: '',
      selectedBaseID: '',
      selectedEnhancementID: ''
    };

    const familyRows = document.getElementById('familyRows');
    const controlRows = document.getElementById('controlRows');
    const enhancementRows = document.getElementById('enhancementRows');
    const detail = document.getElementById('detail');
    const status = document.getElementById('status');
    const searchInput = document.getElementById('searchInput');
    const exportBtn = document.getElementById('exportBtn');
    const reloadBtn = document.getElementById('reloadBtn');
    const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

    const familyNames = {
      AC: 'Access Control',
      AT: 'Awareness and Training',
      AU: 'Audit and Accountability',
      CA: 'Assessment, Authorization, and Monitoring',
      CM: 'Configuration Management',
      CP: 'Contingency Planning',
      IA: 'Identification and Authentication',
      IR: 'Incident Response',
      MA: 'Maintenance',
      MP: 'Media Protection',
      PE: 'Physical and Environmental Protection',
      PL: 'Planning',
      PM: 'Program Management',
      PS: 'Personnel Security',
      PT: 'Personally Identifiable Information Processing and Transparency',
      RA: 'Risk Assessment',
      SA: 'System and Services Acquisition',
      SC: 'System and Communications Protection',
      SI: 'System and Information Integrity',
      SR: 'Supply Chain Risk Management'
    };

    function escapeHtml(value) {
      return String(value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function familyLabel(family) {
      return familyNames[family] || family || 'Unknown family';
    }

    function isEnhancement(control) {
      return control.id.includes('(');
    }

    function baseID(controlID) {
      const idx = controlID.indexOf('(');
      return idx === -1 ? controlID : controlID.slice(0, idx);
    }

    function findControl(controlID) {
      const target = String(controlID || '').trim().toUpperCase();
      return state.controls.find((control) => String(control.id || '').trim().toUpperCase() === target) || null;
    }

    function matchesSearch(control) {
      if (!state.search) return true;
      const q = state.search;
      return control.id.toUpperCase().includes(q) ||
        (control.name || '').toUpperCase().includes(q) ||
        (control.family || '').toUpperCase().includes(q) ||
        familyLabel(control.family).toUpperCase().includes(q);
    }

    function familyList() {
      const seen = new Map();
      state.controls.filter(matchesSearch).forEach((control) => {
        if (!seen.has(control.family)) seen.set(control.family, []);
        seen.get(control.family).push(control);
      });
      return Array.from(seen.entries())
        .sort((a, b) => collator.compare(a[0], b[0]))
        .map(([family, items]) => ({ family, count: items.length }));
    }

    function baseControls() {
      return state.controls
        .filter((control) => control.family === state.selectedFamily)
        .filter(matchesSearch)
        .filter((control) => !isEnhancement(control))
        .sort((a, b) => collator.compare(a.id, b.id));
    }

    function enhancements() {
      return state.controls
        .filter((control) => control.family === state.selectedFamily)
        .filter(matchesSearch)
        .filter((control) => isEnhancement(control) && baseID(control.id) === state.selectedBaseID)
        .sort((a, b) => collator.compare(a.id, b.id));
    }

    function selectedControl() {
      const targetID = state.selectedEnhancementID || state.selectedBaseID;
      return findControl(targetID);
    }

    function scrollSelectionIntoView() {
      [familyRows, controlRows, enhancementRows].forEach((container) => {
        const active = container.querySelector('.row.active');
        if (active) active.scrollIntoView({ block: 'nearest' });
      });
    }

    function navigateToControl(controlID) {
      const control = findControl(controlID);
      if (!control) return;

      state.search = String(control.id || '').toUpperCase();
      searchInput.value = control.id || '';
      state.selectedFamily = control.family || '';
      state.selectedBaseID = isEnhancement(control) ? baseID(control.id) : control.id;
      state.selectedEnhancementID = isEnhancement(control) ? control.id : '';
      render();
      scrollSelectionIntoView();
    }

    function renderFamilies() {
      const families = familyList();
      if (!families.length) {
        familyRows.innerHTML = '<div class="empty">No families match the current filter.</div>';
        return;
      }
      familyRows.innerHTML = families.map((item) => {
        const active = item.family === state.selectedFamily ? 'active' : '';
        return '<button class="row ' + active + '" data-family="' + escapeHtml(item.family) + '" type="button">' +
          '<div class="row-title">' + escapeHtml(item.family) + '</div>' +
          '<div class="row-sub">' + escapeHtml(familyLabel(item.family)) + ' · ' + item.count + ' entries</div>' +
        '</button>';
      }).join('');

      familyRows.querySelectorAll('.row').forEach((row) => {
        row.addEventListener('click', () => {
          state.selectedFamily = row.getAttribute('data-family');
          state.selectedBaseID = '';
          state.selectedEnhancementID = '';
          syncSelections();
          render();
        });
      });
    }

    function renderBaseControls() {
      if (!state.selectedFamily) {
        controlRows.innerHTML = '<div class="empty">Choose a family first.</div>';
        return;
      }

      const controls = baseControls();
      if (!controls.length) {
        controlRows.innerHTML = '<div class="empty">No base controls match the current family/filter.</div>';
        return;
      }

      controlRows.innerHTML = controls.map((control) => {
        const active = control.id === state.selectedBaseID ? 'active' : '';
        return '<button class="row ' + active + '" data-id="' + escapeHtml(control.id) + '" type="button">' +
          '<div class="row-title">' + escapeHtml(control.id) + '</div>' +
          '<div class="row-sub">' + escapeHtml(control.name || 'Unnamed control') + '</div>' +
        '</button>';
      }).join('');

      controlRows.querySelectorAll('.row').forEach((row) => {
        row.addEventListener('click', () => {
          state.selectedBaseID = row.getAttribute('data-id');
          state.selectedEnhancementID = '';
          syncSelections();
          render();
        });
      });
    }

    function renderEnhancements() {
      if (!state.selectedBaseID) {
        enhancementRows.innerHTML = '<div class="empty">Choose a control to view enhancements.</div>';
        return;
      }

      const items = enhancements();
      if (!items.length) {
        enhancementRows.innerHTML = '<div class="empty">No enhancements for this control.</div>';
        return;
      }

      enhancementRows.innerHTML = items.map((control) => {
        const active = control.id === state.selectedEnhancementID ? 'active' : '';
        return '<button class="row ' + active + '" data-id="' + escapeHtml(control.id) + '" type="button">' +
          '<div class="row-title">' + escapeHtml(control.id) + '</div>' +
          '<div class="row-sub">' + escapeHtml(control.name || 'Unnamed enhancement') + '</div>' +
        '</button>';
      }).join('');

      enhancementRows.querySelectorAll('.row').forEach((row) => {
        row.addEventListener('click', () => {
          state.selectedEnhancementID = row.getAttribute('data-id');
          renderDetail();
          renderEnhancements();
        });
      });
    }

    function renderDetail() {
      const control = selectedControl();
      if (!control) {
        detail.innerHTML = '<div class="empty">Choose a family, then a control or enhancement.</div>';
        exportBtn.disabled = true;
        return;
      }
      exportBtn.disabled = false;

      detail.innerHTML =
        '<div>' +
          '<div class="row-title" style="font-size:20px;">' + escapeHtml(control.id) + '</div>' +
          '<div class="row-sub"><strong>' + escapeHtml(control.name || 'Unnamed control') + '</strong></div>' +
          '<div class="row-sub">' + escapeHtml(familyLabel(control.family)) + '</div>' +
        '</div>' +
        '<div class="detail-card">' +
          '<h3>Baselines</h3>' +
          '<div class="tags">' + ((control.baselines || []).length ? control.baselines.map((item) => '<span class="tag">' + escapeHtml(item) + '</span>').join('') : '<span class="tag">None</span>') + '</div>' +
        '</div>' +
        '<div class="detail-card">' +
          '<h3>Threats</h3>' +
          '<div class="tags">' + ((control.threats || []).length ? control.threats.map((item) => '<span class="tag">' + escapeHtml(item) + '</span>').join('') : '<span class="tag">None</span>') + '</div>' +
        '</div>' +
        '<div class="detail-card">' +
          '<h3>Related Controls</h3>' +
          '<div class="mono-list">' + ((control.related_controls || []).length ? control.related_controls.map((item) => {
            const target = findControl(item);
            if (!target) return '<span class="mono">' + escapeHtml(item) + '</span>';
            return '<button class="mono mono-link related-control-link" type="button" data-id="' + escapeHtml(target.id) + '">' + escapeHtml(item) + '</button>';
          }).join('') : '<span class="tag">None</span>') + '</div>' +
        '</div>' +
        '<div class="detail-card">' +
          '<h3>Nist</h3>' +
          '<p><strong>NIST Requirements</strong></p>' +
          '<p style="white-space:pre-wrap;">' + escapeHtml(control.requirements || 'None') + '</p>' +
          '<p><strong>NIST Discussion</strong></p>' +
          '<p style="white-space:pre-wrap;">' + escapeHtml(control.discussion || 'None') + '</p>' +
        '</div>';

      detail.querySelectorAll('.related-control-link').forEach((link) => {
        link.addEventListener('click', () => {
          navigateToControl(link.getAttribute('data-id'));
        });
      });
    }

    function syncSelections() {
      const families = familyList().map((item) => item.family);
      if (!families.includes(state.selectedFamily)) {
        state.selectedFamily = families.length ? families[0] : '';
      }

      const bases = baseControls().map((control) => control.id);
      if (!bases.includes(state.selectedBaseID)) {
        state.selectedBaseID = bases.length ? bases[0] : '';
      }

      const enh = enhancements().map((control) => control.id);
      if (!enh.includes(state.selectedEnhancementID)) {
        state.selectedEnhancementID = '';
      }
    }

    function render() {
      syncSelections();
      renderFamilies();
      renderBaseControls();
      renderEnhancements();
      renderDetail();
      status.textContent = 'Families: ' + familyList().length + ' · Controls: ' + state.controls.length;
    }

    async function loadControls() {
      status.textContent = 'Loading hierarchy...';
      try {
        const response = await fetch('/controls/data');
        if (!response.ok) throw new Error('HTTP ' + response.status);
        state.controls = await response.json();
        render();
      } catch (error) {
        status.textContent = 'Failed to load controls: ' + error.message;
        familyRows.innerHTML = '<div class="empty">Hierarchy unavailable.</div>';
        controlRows.innerHTML = '<div class="empty">Hierarchy unavailable.</div>';
        enhancementRows.innerHTML = '<div class="empty">Hierarchy unavailable.</div>';
        detail.innerHTML = '<div class="empty">Hierarchy unavailable.</div>';
      }
    }

    searchInput.addEventListener('input', () => {
      state.search = searchInput.value.trim().toUpperCase();
      render();
    });

    reloadBtn.addEventListener('click', loadControls);
    exportBtn.addEventListener('click', () => {
      const control = selectedControl();
      if (!control) return;
      window.location.href = '/controls/hierarchy/export?controlID=' + encodeURIComponent(control.id);
    });

    loadControls();
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
  <title>RCSA Control Editor · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f1efe8;
      --panel: rgba(255,252,246,0.94);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
      --accent-dark: #6f2d22;
      --success: #0b5d3b;
      --warn: #9a6700;
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
      background: rgba(255,255,255,0.96);
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
	    button.secondary { background: white; color: var(--ink); border: 1px solid var(--line); }
	    button.danger { background: #e9c6d4; color: #3f1022; }
	    button:hover { background: #d1b09d; }
	    button.secondary:hover { background: #f7f3eb; }
	    button.danger:hover { background: #ddb0c2; }
    .layout {
      display: grid;
      grid-template-columns: minmax(0, 0.95fr) minmax(360px, 1.1fr);
      gap: 18px;
      min-height: 72vh;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 22px;
      background: rgba(255,255,255,0.84);
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
    .rows { max-height: 76vh; overflow: auto; }
    .row {
      width: 100%;
      text-align: left;
      border: 0;
      border-bottom: 1px solid rgba(215,206,191,0.6);
      background: transparent;
      padding: 16px 18px;
      cursor: pointer;
    }
    .row:hover, .row.active { background: rgba(139,61,46,0.08); }
    .row.active { box-shadow: inset 4px 0 0 var(--accent); }
    .row-title { font-weight: bold; }
    .row-sub { color: var(--muted); margin-top: 4px; }
    .tags { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 10px; }
    .tag {
      padding: 4px 8px;
      border-radius: 999px;
      background: rgba(139,61,46,0.1);
      color: var(--accent-dark);
      font-size: 12px;
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.04em;
    }
    form { padding: 18px; display: grid; gap: 16px; }
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
    .checklist { display: flex; gap: 10px; flex-wrap: wrap; }
	    .check {
	      display: inline-flex;
	      gap: 8px;
	      align-items: center;
	      padding: 10px 12px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: white;
	      font-family: Arial, sans-serif;
	      font-size: 13px;
	    }
	    .cia-flags .check {
	      background: #000;
	      color: #fff;
	      border-color: #1f2937;
	    }
	    .cia-flags .check input {
	      accent-color: #fff;
	    }
    .form-actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      justify-content: space-between;
      align-items: center;
    }
    .appendix-rows { display: grid; gap: 10px; }
    .appendix-row {
      display: grid;
      grid-template-columns: minmax(180px, 0.8fr) minmax(220px, 1.2fr) auto;
      gap: 8px;
      align-items: start;
    }
    .appendix-row textarea { min-height: 74px; }
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
	    ` + pageui.Nav("/controls/manage") + `
    <h1>RCSA Control<br>Editor</h1>
    <p>Edit the SQLite-backed control catalog, then write the updated records back to <code>internal/data/merged_nist_controls_master_replaced_from_controls_all.json</code>.</p>

    <section class="toolbar">
      <input id="searchInput" type="search" placeholder="Search control id, name, or family">
      <button id="reloadBtn" class="secondary" type="button">Reload DB</button>
      <button id="exportBtn" type="button">Rewrite JSON</button>
    </section>

    <section class="layout">
      <div class="panel">
        <div class="panel-header">
          <strong>Controls</strong>
          <span id="listStatus" class="status">Loading...</span>
        </div>
        <div id="rows" class="rows"></div>
      </div>

      <div class="panel">
        <div class="panel-header">
          <strong>Edit Control</strong>
          <span id="saveStatus" class="status">Select a control</span>
        </div>
        <form id="editorForm">
          <div class="grid">
            <div class="field">
              <label for="sourceKey">Record Key</label>
              <input id="sourceKey" type="text" readonly>
            </div>
            <div class="field">
              <label for="controlId">Control ID</label>
              <input id="controlId" type="text" readonly>
            </div>
            <div class="field">
              <label for="controlFamily">Family</label>
              <input id="controlFamily" type="text" readonly>
            </div>
            <div class="field">
              <label for="controlType">Type</label>
              <input id="controlType" type="text">
            </div>
          </div>

          <div class="field">
            <label for="controlName">Name</label>
            <input id="controlName" type="text" required>
          </div>

          <div class="field">
            <label for="controlThreats">Threats</label>
            <input id="controlThreats" type="text" placeholder="Comma-separated threats">
          </div>

          <div class="grid">
            <div class="field">
              <label for="baselineLow">Low Baseline IDs</label>
              <textarea id="baselineLow" placeholder="Comma-separated control IDs"></textarea>
            </div>
            <div class="field">
              <label for="baselineModerate">Moderate Baseline IDs</label>
              <textarea id="baselineModerate" placeholder="Comma-separated control IDs"></textarea>
            </div>
            <div class="field">
              <label for="baselineHigh">High Baseline IDs</label>
              <textarea id="baselineHigh" placeholder="Comma-separated control IDs"></textarea>
            </div>
            <div class="field">
              <label for="baselinePrivacy">Privacy Baseline IDs</label>
              <textarea id="baselinePrivacy" placeholder="Comma-separated control IDs"></textarea>
            </div>
          </div>

          <div class="field">
            <label>CIA Flags</label>
	            <div class="checklist cia-flags">
	              <label class="check"><input id="ciaC" type="checkbox"> Confidentiality</label>
	              <label class="check"><input id="ciaI" type="checkbox"> Integrity</label>
	              <label class="check"><input id="ciaA" type="checkbox"> Availability</label>
	            </div>
          </div>

	          <div class="field">
	            <label for="controlJustification">Justification</label>
	            <textarea id="controlJustification" placeholder="Source justification"></textarea>
	          </div>

	          <div class="field">
	            <label for="controlRequirements">Requirements</label>
	            <textarea id="controlRequirements" placeholder="Control requirements text"></textarea>
	          </div>

	          <div class="field">
	            <label for="controlDiscussion">Discussion</label>
	            <textarea id="controlDiscussion" placeholder="Control discussion text"></textarea>
	          </div>

          <div class="field">
            <label for="controlRelatedControls">Related Controls</label>
            <input id="controlRelatedControls" type="text" placeholder="Comma-separated related controls">
          </div>

          <div class="field">
            <label>Appendix</label>
            <div id="appendixRows" class="appendix-rows"></div>
            <div style="margin-top:8px;">
              <button id="addAppendixRowBtn" class="secondary" type="button">Add Appendix Field</button>
            </div>
          </div>

          <div class="form-actions">
            <div id="formMessage" class="status warn">Changes are saved to SQLite first. Use Rewrite JSON to persist back to the flat file.</div>
            <div style="display:flex; gap:10px; flex-wrap:wrap;">
              <button id="deleteBtn" class="danger" type="button">Delete Control</button>
              <button id="saveBtn" type="submit">Save To SQLite</button>
            </div>
          </div>
        </form>
      </div>
    </section>
  </main>

  <script>
    const state = {
      controls: [],
      filtered: [],
      selectedId: ''
    };
    const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
    const minVisibleControlRows = 10;

    const rows = document.getElementById('rows');
    const listStatus = document.getElementById('listStatus');
    const saveStatus = document.getElementById('saveStatus');
    const formMessage = document.getElementById('formMessage');
    const searchInput = document.getElementById('searchInput');
    const reloadBtn = document.getElementById('reloadBtn');
    const exportBtn = document.getElementById('exportBtn');
    const deleteBtn = document.getElementById('deleteBtn');
    const editorForm = document.getElementById('editorForm');

    const controlId = document.getElementById('controlId');
    const sourceKey = document.getElementById('sourceKey');
    const controlFamily = document.getElementById('controlFamily');
    const controlType = document.getElementById('controlType');
    const controlName = document.getElementById('controlName');
    const controlThreats = document.getElementById('controlThreats');
    const baselineLow = document.getElementById('baselineLow');
    const baselineModerate = document.getElementById('baselineModerate');
    const baselineHigh = document.getElementById('baselineHigh');
    const baselinePrivacy = document.getElementById('baselinePrivacy');
	    const ciaC = document.getElementById('ciaC');
	    const ciaI = document.getElementById('ciaI');
	    const ciaA = document.getElementById('ciaA');
	    const controlJustification = document.getElementById('controlJustification');
	    const controlRequirements = document.getElementById('controlRequirements');
	    const controlDiscussion = document.getElementById('controlDiscussion');
	    const controlRelatedControls = document.getElementById('controlRelatedControls');
	    const appendixRows = document.getElementById('appendixRows');
	    const addAppendixRowBtn = document.getElementById('addAppendixRowBtn');

    const familyNames = {
      AC: 'Access Control',
      AT: 'Awareness and Training',
      AU: 'Audit and Accountability',
      CA: 'Assessment, Authorization, and Monitoring',
      CM: 'Configuration Management',
      CP: 'Contingency Planning',
      IA: 'Identification and Authentication',
      IR: 'Incident Response',
      MA: 'Maintenance',
      MP: 'Media Protection',
      PE: 'Physical and Environmental Protection',
      PL: 'Planning',
      PM: 'Program Management',
      PS: 'Personnel Security',
      PT: 'Personally Identifiable Information Processing and Transparency',
      RA: 'Risk Assessment',
      SA: 'System and Services Acquisition',
      SC: 'System and Communications Protection',
      SI: 'System and Information Integrity',
      SR: 'Supply Chain Risk Management'
    };

    function escapeHtml(value) {
      return String(value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function familyLabel(family) {
      return familyNames[family] || family || 'Unknown family';
    }

    function ensureVisibleRowCount(container, rowSelector, minimumRows) {
      if (!container) return;
      const items = Array.from(container.querySelectorAll(rowSelector));
      if (!items.length) {
        container.style.removeProperty('min-height');
        return;
      }

      const requiredHeight = items
        .slice(0, minimumRows)
        .reduce((sum, item) => sum + item.getBoundingClientRect().height, 0);
      if (!requiredHeight) {
        container.style.removeProperty('min-height');
        return;
      }

      container.style.minHeight = Math.ceil(requiredHeight) + 'px';
    }

    function parseCSV(value) {
      return value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
    }

    function formatCSV(values) {
      return (values || []).join(', ');
    }

    function bindAppendixRowActions() {
      appendixRows.querySelectorAll('.appendix-remove').forEach((button) => {
        button.addEventListener('click', () => {
          const row = button.closest('.appendix-row');
          if (row) row.remove();
          if (!appendixRows.querySelector('.appendix-row')) {
            renderAppendixRows(null);
          }
        });
      });
    }

    function renderAppendixRows(value) {
      const entries = [];
      if (value && typeof value === 'object') {
        Object.keys(value).sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' })).forEach((key) => {
          const raw = value[key];
          entries.push({ key, value: raw === null || raw === undefined ? '' : String(raw) });
        });
      }
      if (!entries.length) {
        entries.push({ key: '', value: '' });
      }

      appendixRows.innerHTML = entries.map((entry) => {
        return '<div class="appendix-row">' +
          '<input class="appendix-key" type="text" placeholder="Appendix field name" value="' + escapeHtml(entry.key) + '">' +
          '<textarea class="appendix-value" placeholder="Appendix field value">' + escapeHtml(entry.value) + '</textarea>' +
          '<button class="danger appendix-remove" type="button">Remove</button>' +
        '</div>';
      }).join('');

      bindAppendixRowActions();
    }

    function parseAppendixRows() {
      const result = {};
      const seen = new Set();
      const rows = appendixRows.querySelectorAll('.appendix-row');
      for (const row of rows) {
        const keyInput = row.querySelector('.appendix-key');
        const valueInput = row.querySelector('.appendix-value');
        const key = String(keyInput && keyInput.value || '').trim();
        if (!key) continue;
        const keyCanon = key.toUpperCase();
        if (seen.has(keyCanon)) {
          throw new Error('Duplicate appendix key: ' + key);
        }
        seen.add(keyCanon);
        result[key] = String(valueInput && valueInput.value || '');
      }
      return Object.keys(result).length ? result : null;
    }

    function confirmJSONWrite(actionLabel) {
      return window.confirm(actionLabel + ' This will overwrite internal/data/merged_nist_controls_master_replaced_from_controls_all.json with the current SQLite contents.');
    }

    function selectedControl() {
      return state.controls.find((item) => item.id === state.selectedId) || null;
    }

    function renderRows() {
      if (!state.filtered.length) {
        rows.innerHTML = '<div class="empty">No controls match the current search.</div>';
        rows.style.removeProperty('min-height');
        return;
      }

      rows.innerHTML = state.filtered.map((control) => {
        const active = control.id === state.selectedId ? 'active' : '';
        const threats = (control.threats || []).map((item) => '<span class="tag">' + escapeHtml(item) + '</span>').join('');
        return '<button class="row ' + active + '" data-id="' + escapeHtml(control.id) + '" type="button">' +
          '<div class="row-title">' + escapeHtml(control.id) + '</div>' +
          '<div class="row-sub"><strong>' + escapeHtml(control.name || 'Unnamed control') + '</strong> (' + escapeHtml(familyLabel(control.family)) + ' · ' + escapeHtml(control.type || 'Control') + ')</div>' +
          '<div class="tags">' + (threats || '<span class="tag">No threats</span>') + '</div>' +
        '</button>';
      }).join('');

      rows.querySelectorAll('.row').forEach((row) => {
        row.addEventListener('click', () => {
          state.selectedId = row.getAttribute('data-id');
          fillForm(selectedControl());
          renderRows();
        });
      });

      ensureVisibleRowCount(rows, '.row', minVisibleControlRows);
    }

    function fillForm(control) {
      if (!control) {
        controlId.value = '';
        sourceKey.value = '';
        controlFamily.value = '';
        controlType.value = '';
        controlName.value = '';
        controlThreats.value = '';
        baselineLow.value = '';
        baselineModerate.value = '';
        baselineHigh.value = '';
        baselinePrivacy.value = '';
        ciaC.checked = false;
        ciaI.checked = false;
	        ciaA.checked = false;
	        controlJustification.value = '';
	        controlRequirements.value = '';
	        controlDiscussion.value = '';
	        controlRelatedControls.value = '';
	        renderAppendixRows(null);
	        saveStatus.textContent = 'Select a control';
	        return;
	      }

      controlId.value = control.id;
      sourceKey.value = control.source_key || '';
      controlFamily.value = familyLabel(control.family) + ' (' + control.family + ')';
      controlType.value = control.type || 'Control';
      controlName.value = control.name || '';
      controlThreats.value = formatCSV(control.threats || []);
      baselineLow.value = formatCSV((control.mapping_baselines && control.mapping_baselines.Low) || []);
      baselineModerate.value = formatCSV((control.mapping_baselines && control.mapping_baselines.Moderate) || []);
      baselineHigh.value = formatCSV((control.mapping_baselines && control.mapping_baselines.High) || []);
      baselinePrivacy.value = formatCSV((control.mapping_baselines && control.mapping_baselines.Privacy) || []);
	      ciaC.checked = Boolean(control.confidentiality);
	      ciaI.checked = Boolean(control.integrity);
	      ciaA.checked = Boolean(control.availability);
	      controlJustification.value = control.justification || '';
	      controlRequirements.value = control.requirements || '';
	      controlDiscussion.value = control.discussion || '';
	      controlRelatedControls.value = formatCSV(control.related_controls || []);
	      renderAppendixRows(control.appendix);
	      saveStatus.textContent = 'Editing ' + control.id;
	    }

    function applySearch() {
      const value = searchInput.value.trim().toUpperCase();
      state.filtered = state.controls.filter((control) => {
        return !value ||
          control.id.includes(value) ||
          (control.name || '').toUpperCase().includes(value) ||
          (control.type || '').toUpperCase().includes(value) ||
          (control.family || '').toUpperCase().includes(value) ||
          familyLabel(control.family).toUpperCase().includes(value);
      });

      if (!state.filtered.find((item) => item.id === state.selectedId)) {
        state.selectedId = state.filtered.length ? state.filtered[0].id : '';
      }

      renderRows();
      fillForm(selectedControl());
      listStatus.textContent = 'Showing ' + state.filtered.length + ' controls';
    }

    async function loadControls() {
      listStatus.textContent = 'Loading...';
      formMessage.textContent = 'Changes are saved to SQLite first. Use Rewrite JSON to persist back to the flat file.';
      formMessage.className = 'status warn';

      try {
        const resp = await fetch('/controls/data');
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        state.controls = await resp.json();
        applySearch();
      } catch (err) {
        listStatus.textContent = 'Load failed';
        rows.innerHTML = '<div class="empty">The SQLite control database could not be loaded.</div>';
        formMessage.textContent = 'Failed to load controls: ' + err.message;
        formMessage.className = 'status error';
      }
    }

    editorForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!state.selectedId) return;

	      const payload = {
	        source_key: sourceKey.value.trim(),
	        type: controlType.value.trim(),
        name: controlName.value.trim(),
        threats: parseCSV(controlThreats.value),
        mapping_baselines: {
          Low: parseCSV(baselineLow.value),
          Moderate: parseCSV(baselineModerate.value),
          High: parseCSV(baselineHigh.value),
          Privacy: parseCSV(baselinePrivacy.value)
        },
	        confidentiality: ciaC.checked,
	        integrity: ciaI.checked,
	        availability: ciaA.checked,
	        justification: controlJustification.value.trim(),
	        requirements: controlRequirements.value.trim(),
	        discussion: controlDiscussion.value.trim(),
	        related_controls: parseCSV(controlRelatedControls.value)
	      };
      try {
        payload.appendix = parseAppendixRows();
      } catch (appendixErr) {
        formMessage.textContent = appendixErr.message;
        formMessage.className = 'status error';
        return;
      }

      formMessage.textContent = 'Saving to SQLite...';
      formMessage.className = 'status warn';

      try {
        const resp = await fetch('/controls/' + encodeURIComponent(state.selectedId), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));

        const idx = state.controls.findIndex((item) => item.id === data.id);
        if (idx >= 0) state.controls[idx] = data;
        formMessage.textContent = 'Saved to SQLite for ' + data.id + '.';
        formMessage.className = 'status success';
        applySearch();
      } catch (err) {
        formMessage.textContent = 'Save failed: ' + err.message;
        formMessage.className = 'status error';
      }
    });

    exportBtn.addEventListener('click', async () => {
      if (!confirmJSONWrite('Rewrite the JSON file?')) {
        formMessage.textContent = 'JSON rewrite canceled.';
        formMessage.className = 'status warn';
        return;
      }

      formMessage.textContent = 'Rewriting JSON file...';
      formMessage.className = 'status warn';
      try {
        const resp = await fetch('/controls/save', { method: 'POST' });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        formMessage.textContent = 'Rewrote ' + data.saved + ' controls to ' + data.path + '.';
        formMessage.className = 'status success';
      } catch (err) {
        formMessage.textContent = 'Rewrite failed: ' + err.message;
        formMessage.className = 'status error';
      }
    });

    deleteBtn.addEventListener('click', async () => {
      const control = selectedControl();
      if (!control) return;

      const confirmed = window.confirm('Delete ' + control.id + ' from SQLite and remove it from future JSON rewrites?');
      if (!confirmed) return;

      formMessage.textContent = 'Deleting control...';
      formMessage.className = 'status warn';

      try {
        const resp = await fetch('/controls/' + encodeURIComponent(control.id), {
          method: 'DELETE'
        });
        if (!resp.ok) {
          let message = 'HTTP ' + resp.status;
          try {
            const data = await resp.json();
            message = data.error || message;
          } catch (_) {}
          throw new Error(message);
        }

        state.controls = state.controls.filter((item) => item.id !== control.id);
        state.selectedId = '';
        formMessage.textContent = 'Deleted ' + control.id + ' from SQLite. Rewrite JSON to persist the deletion to disk.';
        formMessage.className = 'status success';
        applySearch();
      } catch (err) {
        formMessage.textContent = 'Delete failed: ' + err.message;
        formMessage.className = 'status error';
      }
    });

    reloadBtn.addEventListener('click', loadControls);
    searchInput.addEventListener('input', applySearch);
    addAppendixRowBtn.addEventListener('click', () => {
      const row = document.createElement('div');
      row.className = 'appendix-row';
      row.innerHTML =
        '<input class="appendix-key" type="text" placeholder="Appendix field name">' +
        '<textarea class="appendix-value" placeholder="Appendix field value"></textarea>' +
        '<button class="danger appendix-remove" type="button">Remove</button>';
      appendixRows.appendChild(row);
      bindAppendixRowActions();
    });

    loadControls();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func boolMarker(value bool) string {
	if value {
		return "Yes"
	}
	return ""
}

func buildHierarchyRTF(control Control) string {
	var buf strings.Builder
	buf.WriteString("{\\rtf1\\ansi\\deff0\n")
	buf.WriteString("{\\fonttbl{\\f0 Times New Roman;}{\\f1 Courier New;}}\n")
	buf.WriteString("\\fs24\n")
	buf.WriteString("\\b Selection Details\\b0\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Control ID:\\b0 " + rtfEscape(control.ID) + "\\par\n")
	buf.WriteString("\\b Name:\\b0 " + rtfEscape(valueOrNone(control.Name)) + "\\par\n")
	buf.WriteString("\\b Family:\\b0 " + rtfEscape(control.Family) + " - " + rtfEscape(controlFamilyName(control.Family)) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Baselines\\b0\\par\n")
	buf.WriteString(rtfList(control.Baselines) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Threats\\b0\\par\n")
	buf.WriteString(rtfList(control.Threats) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Related Controls\\b0\\par\n")
	buf.WriteString(rtfList(control.RelatedControls) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b CIA Classification\\b0\\par\n")
	buf.WriteString("\\tab Confidentiality: " + rtfEscape(valueOrNone(control.Confidentiality)) + "\\par\n")
	buf.WriteString("\\tab Integrity: " + rtfEscape(valueOrNone(control.Integrity)) + "\\par\n")
	buf.WriteString("\\tab Availability: " + rtfEscape(valueOrNone(control.Availability)) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Mapping Baseline Entries\\b0\\par\n")
	buf.WriteString("\\tab Low: " + rtfEscape(listOrNone(control.MappingBaselines.Low)) + "\\par\n")
	buf.WriteString("\\tab Moderate: " + rtfEscape(listOrNone(control.MappingBaselines.Moderate)) + "\\par\n")
	buf.WriteString("\\tab High: " + rtfEscape(listOrNone(control.MappingBaselines.High)) + "\\par\n")
	buf.WriteString("\\tab Privacy: " + rtfEscape(listOrNone(control.MappingBaselines.Privacy)) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Justification\\b0\\par\n")
	buf.WriteString(rtfEscape(valueOrNone(control.Justification)) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Requirements\\b0\\par\n")
	buf.WriteString(rtfEscape(valueOrNone(control.Requirements)) + "\\par\n")
	buf.WriteString("\\par\n")
	buf.WriteString("\\b Discussion\\b0\\par\n")
	buf.WriteString(rtfEscape(valueOrNone(control.Discussion)) + "\\par\n")
	buf.WriteString("}\n")
	return buf.String()
}

func rtfEscape(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"{", "\\{",
		"}", "\\}",
		"\r\n", "\\line ",
		"\n", "\\line ",
		"\r", "\\line ",
	)
	return replacer.Replace(value)
}

func rtfList(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, rtfEscape(value))
	}
	return strings.Join(escaped, "; ")
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	return strings.Join(values, "; ")
}

func valueOrNone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "None"
	}
	return value
}

func sanitizeFilename(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	if value == "" {
		return "control"
	}
	return replacer.Replace(value)
}
