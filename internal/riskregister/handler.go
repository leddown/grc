package riskregister

import (
	"errors"
	"net/http"
	"strconv"
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

func (h *Handler) List(c *gin.Context) {
	page := apiutil.ParsePagination(c)
	items, err := h.service.List(c.Query("search"), c.Query("status"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list risk register items"})
		return
	}
	if page.Enabled {
		paged, total := apiutil.PaginateSlice(items, page)
		c.JSON(http.StatusOK, gin.H{
			"items":    paged,
			"total":    total,
			"page":     page.Page,
			"per_page": page.PerPage,
		})
		return
	}
	c.JSON(http.StatusOK, items)
}

type upsertRiskRequest struct {
	RiskID              string `json:"risk_id"`
	Title               string `json:"title"`
	BusinessUnit        string `json:"business_unit"`
	Asset               string `json:"asset"`
	ThreatSource        string `json:"threat_source"`
	Vulnerability       string `json:"vulnerability"`
	Likelihood          int    `json:"likelihood"`
	Impact              int    `json:"impact"`
	CurrentControls     string `json:"current_controls"`
	ResidualLikelihood  int    `json:"residual_likelihood"`
	ResidualImpact      int    `json:"residual_impact"`
	ResponseStrategy    string `json:"response_strategy"`
	ResponseAction      string `json:"response_action"`
	Owner               string `json:"owner"`
	Status              string `json:"status"`
	TargetDate          string `json:"target_date"`
	LastReviewDate      string `json:"last_review_date"`
	NextReviewDate      string `json:"next_review_date"`
	RiskAppetiteAligned bool   `json:"risk_appetite_aligned"`
	Notes               string `json:"notes"`
}

func (h *Handler) Create(c *gin.Context) {
	var req upsertRiskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	created, err := h.service.Create(toRisk(req))
	if err != nil {
		status := http.StatusInternalServerError
		if isValidationError(err) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req upsertRiskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	updated, err := h.service.Update(id, toRisk(req))
	if err != nil {
		status := http.StatusInternalServerError
		if isValidationError(err) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.service.Delete(id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Page(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(riskRegisterHTML(false)))
}

func (h *Handler) ManagePage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(riskRegisterHTML(true)))
}

func toRisk(req upsertRiskRequest) Risk {
	return Risk{
		RiskID:              req.RiskID,
		Title:               req.Title,
		BusinessUnit:        req.BusinessUnit,
		Asset:               req.Asset,
		ThreatSource:        req.ThreatSource,
		Vulnerability:       req.Vulnerability,
		Likelihood:          req.Likelihood,
		Impact:              req.Impact,
		CurrentControls:     req.CurrentControls,
		ResidualLikelihood:  req.ResidualLikelihood,
		ResidualImpact:      req.ResidualImpact,
		ResponseStrategy:    req.ResponseStrategy,
		ResponseAction:      req.ResponseAction,
		Owner:               req.Owner,
		Status:              req.Status,
		TargetDate:          req.TargetDate,
		LastReviewDate:      req.LastReviewDate,
		NextReviewDate:      req.NextReviewDate,
		RiskAppetiteAligned: req.RiskAppetiteAligned,
		Notes:               req.Notes,
	}
}

func isValidationError(err error) bool {
	msg := err.Error()
	return msg == "risk_id is required" ||
		msg == "title is required" ||
		msg == "asset is required" ||
		msg == "owner is required" ||
		strings.Contains(msg, "must be at most")
}

func riskRegisterHTML(manage bool) string {
	modeLabel := "Risk Register"
	saveHelp := ""
	activePath := "/risk-register"
	if manage {
		modeLabel = "Risk Register Editor"
		saveHelp = `<p style="font-family:Arial,sans-serif;color:#5e6672">Edit rows inline and click Save. Creating, saving, and deleting require admin auth.</p>`
		activePath = "/risk-register/manage"
	}
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + modeLabel + ` · GRC</title>
  <style>
    :root { color-scheme: light; --ink:#1c2431; --muted:#5e6672; --line:#d7cebf; }
    body { margin:0; font-family: Georgia, "Times New Roman", serif; color:var(--ink); background:linear-gradient(135deg,#f7f3eb,#ece4d6 55%,#e4d8c4);}
    main { max-width:1320px; margin:24px auto; padding:24px; background:var(--bg); border:1px solid rgba(215,206,191,0.8); border-radius:24px;}
    .tabs { display:flex; gap:10px; flex-wrap:wrap; margin-bottom:20px; }
    .tab { padding:10px 14px; border-radius:999px; background:#efe6d6; border:1px solid var(--line); color:var(--ink); text-decoration:none; font-family:Arial,sans-serif; font-size:13px; letter-spacing:0.04em; text-transform:uppercase; }
    .tab.active { background:#e1d0b7; border-color:#b89d78; }
    .toolbar { display:flex; gap:8px; flex-wrap:wrap; margin-bottom:10px; align-items:center; }
    input, select, button, textarea { font:inherit; }
    input, select { padding:8px 10px; border:1px solid var(--line); border-radius:10px; background:var(--bg); }
    button { padding:8px 12px; border-radius:999px; border:1px solid #b89d78; background:#e1d0b7; cursor:pointer; }
    table { width:100%; border-collapse:collapse; font-family:Arial,sans-serif; font-size:13px; background:var(--panel); border:1px solid var(--line); }
    th, td { border:1px solid var(--line); padding:6px; vertical-align:top; }
    th { background:#efe6d6; text-align:left; position:sticky; top:0; }
    td input, td select, td textarea { width:100%; border:1px solid #ddd; border-radius:6px; padding:4px 6px; font-size:12px; }
    td textarea { min-height:44px; resize:vertical; }
    .status { margin:8px 0; color:var(--muted); font-family:Arial,sans-serif; }
    .table-wrap { overflow:auto; max-height:72vh; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav(activePath) + `
    <h1 style="margin:0 0 10px">` + modeLabel + `</h1>` + saveHelp + `
    <div class="toolbar">
      <input id="search" placeholder="Search risk id/title/asset/owner">
      <select id="statusFilter">
        <option value="">All Statuses</option>
        <option>Open</option>
        <option>In Progress</option>
        <option>Accepted</option>
        <option>Closed</option>
      </select>
      <button id="refreshBtn">Refresh</button>
      ` + ternary(manage, `<button id="addBtn">Add Risk</button>`, ``) + `
    </div>
    <div class="status" id="status">Loading...</div>
    <div class="table-wrap"><table id="grid"></table></div>
  </main>
  <script>
    const manage = ` + ternary(manage, "true", "false") + `;
    const cols = [
      {k:"risk_id", l:"Risk ID"}, {k:"title", l:"Title"}, {k:"business_unit", l:"Business Unit"},
      {k:"asset", l:"Asset"}, {k:"threat_source", l:"Threat Source"}, {k:"vulnerability", l:"Vulnerability"},
      {k:"likelihood", l:"L"}, {k:"impact", l:"I"}, {k:"inherent_score", l:"Inherent"},
      {k:"current_controls", l:"Current Controls"}, {k:"residual_likelihood", l:"rL"}, {k:"residual_impact", l:"rI"},
      {k:"residual_score", l:"Residual"}, {k:"response_strategy", l:"Strategy"}, {k:"response_action", l:"Action"},
      {k:"owner", l:"Owner"}, {k:"status", l:"Status"}, {k:"target_date", l:"Target"},
      {k:"last_review_date", l:"Last Review"}, {k:"next_review_date", l:"Next Review"},
      {k:"risk_appetite_aligned", l:"Appetite?"}, {k:"notes", l:"Notes"}
    ];
    const grid = document.getElementById("grid");
    const status = document.getElementById("status");

    function esc(v){ return (v ?? "").toString().replace(/[&<>"']/g, m => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m])); }
    function num(v){ const n = parseInt(v,10); return Number.isNaN(n) ? 1 : Math.max(1, Math.min(5, n)); }

    async function load(){
      const q = new URLSearchParams();
      if (search.value.trim()) q.set("search", search.value.trim());
      if (statusFilter.value.trim()) q.set("status", statusFilter.value.trim());
      const res = await fetch("/risk-register/data?" + q.toString());
      const data = await res.json();
      const items = Array.isArray(data) ? data : (Array.isArray(data.items) ? data.items : []);
      render(items);
      status.textContent = "Loaded " + items.length + " risks";
    }

    function render(items){
      const head = "<tr><th>ID</th>" + cols.map(c => "<th>"+c.l+"</th>").join("") + (manage ? "<th>Actions</th>" : "") + "</tr>";
      const rows = items.map(item => {
        const cells = cols.map(c => {
          const v = item[c.k];
          if (!manage) return "<td>" + esc(v) + "</td>";
          if (c.k === "risk_appetite_aligned") return '<td><input data-k="'+c.k+'" type="checkbox" '+(v ? "checked":"")+'></td>';
          if (["likelihood","impact","residual_likelihood","residual_impact"].includes(c.k)) return '<td><input data-k="'+c.k+'" type="number" min="1" max="5" value="'+esc(v)+'"></td>';
          if (["current_controls","response_action","notes","vulnerability"].includes(c.k)) return '<td><textarea data-k="'+c.k+'">'+esc(v)+'</textarea></td>';
          return '<td><input data-k="'+c.k+'" value="'+esc(v)+'"></td>';
        }).join("");
        const actions = manage ? '<td><button data-save="'+item.id+'">Save</button> <button data-del="'+item.id+'">Delete</button></td>' : "";
        return '<tr data-id="'+item.id+'"><td>'+item.id+'</td>'+cells+actions+'</tr>';
      }).join("");
      grid.innerHTML = head + rows;
    }

    async function saveRow(id){
      const tr = grid.querySelector('tr[data-id="'+id+'"]');
      if (!tr) return;
      const body = {};
      tr.querySelectorAll("[data-k]").forEach(el => {
        const k = el.getAttribute("data-k");
        if (el.type === "checkbox") body[k] = el.checked;
        else if (el.type === "number") body[k] = num(el.value);
        else body[k] = el.value;
      });
      const res = await fetch("/risk-register/" + id, {
        method: "PUT",
        headers: {"Content-Type":"application/json"},
        body: JSON.stringify(body)
      });
      if (!res.ok) {
        const msg = await res.text();
        status.textContent = "Save failed: " + msg;
        return;
      }
      status.textContent = "Saved risk " + id;
      await load();
    }

    async function deleteRow(id){
      const res = await fetch("/risk-register/" + id, {method:"DELETE"});
      if (!res.ok) {
        const msg = await res.text();
        status.textContent = "Delete failed: " + msg;
        return;
      }
      status.textContent = "Deleted risk " + id;
      await load();
    }

    async function addRow(){
      const payload = {
        risk_id: "RISK-" + Math.floor(Date.now()/1000),
        title: "New risk",
        business_unit: "Security",
        asset: "TBD",
        threat_source: "TBD",
        vulnerability: "TBD",
        likelihood: 3,
        impact: 3,
        current_controls: "",
        residual_likelihood: 2,
        residual_impact: 2,
        response_strategy: "Mitigate",
        response_action: "",
        owner: "TBD",
        status: "Open",
        target_date: "",
        last_review_date: "",
        next_review_date: "",
        risk_appetite_aligned: false,
        notes: ""
      };
      const res = await fetch("/risk-register", {method:"POST", headers:{"Content-Type":"application/json"}, body: JSON.stringify(payload)});
      if (!res.ok) {
        const msg = await res.text();
        status.textContent = "Create failed: " + msg;
        return;
      }
      status.textContent = "Created new risk item";
      await load();
    }

    grid.addEventListener("click", async (e) => {
      const save = e.target.getAttribute("data-save");
      const del = e.target.getAttribute("data-del");
      if (save) await saveRow(save);
      if (del) await deleteRow(del);
    });
    refreshBtn.addEventListener("click", load);
    if (manage) addBtn.addEventListener("click", addRow);
    search.addEventListener("change", load);
    statusFilter.addEventListener("change", load);
    load();
  </script>
</body>
</html>`
}

func ternary(condition bool, yes, no string) string {
	if condition {
		return yes
	}
	return no
}
