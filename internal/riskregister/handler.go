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
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(riskRegisterHTML()))
}

func (h *Handler) ManagePage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(riskRegisterEditorHTML()))
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

func riskRegisterHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Risk Register · GRC</title>
  <style>
    :root { color-scheme: light; --ink:#1c2431; --muted:#5e6672; --line:#d7cebf; }
    body { margin:0; font-family: Georgia, "Times New Roman", serif; color:var(--ink); background:linear-gradient(135deg,#f7f3eb,#ece4d6 55%,#e4d8c4);}
    main { max-width:1320px; margin:24px auto; padding:24px; background:var(--bg); border:1px solid rgba(215,206,191,0.8); border-radius:24px;}
    .toolbar { display:flex; gap:8px; flex-wrap:wrap; margin-bottom:10px; align-items:center; }
    input, select, button { font:inherit; box-sizing:border-box; }
    input, select { padding:8px 10px; border:1px solid var(--line); border-radius:10px; background:var(--bg); }
    button { padding:8px 12px; border-radius:999px; border:1px solid #b89d78; background:#e1d0b7; cursor:pointer; }
    table { width:100%; border-collapse:collapse; font-family:Arial,sans-serif; font-size:13px; background:var(--panel); border:1px solid var(--line); }
    th, td { border:1px solid var(--line); padding:6px; vertical-align:top; }
    th { background:#efe6d6; text-align:left; position:sticky; top:0; }
    .status { margin:8px 0; color:var(--muted); font-family:Arial,sans-serif; }
    .table-wrap { overflow:auto; max-height:72vh; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/risk-register") + `
    <h1 style="margin:0 0 10px">Risk Register</h1>
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
    </div>
    <div class="status" id="status">Loading...</div>
    <div class="table-wrap"><table id="grid"></table></div>
  </main>
  <script>
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
      const head = "<tr><th>ID</th>" + cols.map(c => "<th>"+c.l+"</th>").join("") + "</tr>";
      const rows = items.map(item => {
        const cells = cols.map(c => "<td>" + esc(item[c.k]) + "</td>").join("");
        return '<tr data-id="'+item.id+'"><td>'+item.id+'</td>'+cells+'</tr>';
      }).join("");
      grid.innerHTML = head + rows;
    }

    refreshBtn.addEventListener("click", load);
    search.addEventListener("change", load);
    statusFilter.addEventListener("change", load);
    load();
  </script>
</body>
</html>`
}

// riskRegisterEditorHTML is a list beside a form for one risk, its fields one
// above the other. It used to be the register table with an input in every
// cell: twenty-two columns of inputs had no room on a laptop screen.
func riskRegisterEditorHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Risk Register Editor · GRC</title>
  <style>
    :root { color-scheme: light; --ink:#1c2431; --muted:#5e6672; --line:#d7cebf; --accent:#8b3d2e; }
    body { margin:0; font-family: Georgia, "Times New Roman", serif; color:var(--ink); background:linear-gradient(135deg,#f7f3eb,#ece4d6 55%,#e4d8c4);}
    main { max-width:1320px; margin:24px auto; padding:24px; background:var(--bg); border:1px solid rgba(215,206,191,0.8); border-radius:24px;}
    .toolbar { display:flex; gap:8px; flex-wrap:wrap; margin-bottom:10px; align-items:center; }
    input, select, button, textarea { font:inherit; box-sizing:border-box; }
    input, select, textarea { padding:4px 8px; border:1px solid var(--line); border-radius:6px; background:var(--bg); }
    button { padding:4px 10px; border-radius:6px; border:1px solid #b89d78; background:#e1d0b7; cursor:pointer; }
    .layout { display:grid; grid-template-columns:minmax(200px, 300px) minmax(0, 1fr); gap:10px; align-items:start; }
    .panel { border:1px solid var(--line); border-radius:8px; overflow:hidden; min-width:0; }
    .panel-header { display:flex; justify-content:space-between; align-items:center; gap:8px; padding:6px 10px; border-bottom:1px solid var(--line); }
    .panel-actions { display:flex; align-items:center; gap:6px; flex:none; }
    #formTitle { flex:1 1 0; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
    .status { color:var(--muted); font-family:Arial,sans-serif; font-size:12px; }
    /* Both panels end at the bottom of the window, so Save and Delete in the
       form's header stay in reach however long the risk is. */
    .rows, .form-body { max-height:max(240px, calc(100vh - 230px)); overflow:auto; }
    .row { display:block; width:100%; text-align:left; border:0; border-bottom:1px solid var(--line); border-radius:0; background:transparent; padding:6px 10px; cursor:pointer; }
    .row.active { box-shadow:inset 3px 0 0 var(--accent); }
    .row-title { font-weight:600; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
    .row-sub { color:var(--muted); font-size:11px; margin-top:2px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
    .form-body { padding:10px; }
    fieldset { border:0; margin:0; padding:0; min-width:0; display:grid; gap:8px; }
    .field { min-width:0; }
    .field > label { display:block; margin-bottom:2px; color:var(--muted); font-family:Arial,sans-serif; font-size:11px; text-transform:uppercase; letter-spacing:0.06em; }
    .field input, .field textarea { display:block; width:100%; }
    .field input[type="number"] { width:80px; }
    .field textarea { min-height:52px; resize:vertical; }
    .field.check > label { display:flex; align-items:center; gap:6px; text-transform:none; letter-spacing:0; font-size:13px; color:inherit; }
    .field.check input { width:auto; display:inline-block; }
    .calc { font-variant-numeric:tabular-nums; font-weight:600; }
    .empty { padding:16px; color:var(--muted); text-align:center; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/risk-register/manage") + `
    <h1 style="margin:0 0 10px">Risk Register Editor</h1>
    <p class="lede" style="font-family:Arial,sans-serif;color:#5e6672">Pick a risk to edit it. Creating, saving, and deleting require admin auth.</p>
    <div class="toolbar">
      <input id="search" placeholder="Search risk id/title/asset/owner">
      <select id="statusFilter">
        <option value="">All Statuses</option>
        <option>Open</option>
        <option>In Progress</option>
        <option>Accepted</option>
        <option>Closed</option>
      </select>
      <button id="refreshBtn" type="button">Refresh</button>
      <button id="addBtn" type="button">Add Risk</button>
    </div>
    <section class="layout">
      <div class="panel">
        <div class="panel-header">
          <strong>Risks</strong>
          <span id="listStatus" class="status">Loading...</span>
        </div>
        <div id="rows" class="rows"></div>
      </div>
      <div class="panel">
        <div class="panel-header">
          <strong id="formTitle">No risk selected</strong>
          <span class="panel-actions">
            <span id="saveStatus" class="status"></span>
            <button id="deleteBtn" class="danger" type="button" disabled>Delete</button>
            <button id="saveBtn" type="submit" form="editorForm" disabled>Save</button>
          </span>
        </div>
        <form id="editorForm" class="form-body" novalidate>
          <fieldset id="fields" disabled></fieldset>
        </form>
      </div>
    </section>
    <datalist id="statusList"><option>Open</option><option>In Progress</option><option>Accepted</option><option>Closed</option></datalist>
    <datalist id="strategyList"><option>Mitigate</option><option>Accept</option><option>Transfer</option><option>Avoid</option></datalist>
  </main>
  <script>
    const fields = [
      {k:"risk_id", l:"Risk ID", req:true}, {k:"title", l:"Title", req:true},
      {k:"business_unit", l:"Business Unit"}, {k:"asset", l:"Asset", req:true},
      {k:"threat_source", l:"Threat Source"}, {k:"vulnerability", l:"Vulnerability", area:true},
      {k:"likelihood", l:"Likelihood (1-5)", score:true}, {k:"impact", l:"Impact (1-5)", score:true},
      {k:"inherent_score", l:"Inherent Score", calc:["likelihood","impact"]},
      {k:"current_controls", l:"Current Controls", area:true},
      {k:"residual_likelihood", l:"Residual Likelihood (1-5)", score:true}, {k:"residual_impact", l:"Residual Impact (1-5)", score:true},
      {k:"residual_score", l:"Residual Score", calc:["residual_likelihood","residual_impact"]},
      {k:"response_strategy", l:"Response Strategy", list:"strategyList"}, {k:"response_action", l:"Response Action", area:true},
      {k:"owner", l:"Owner", req:true}, {k:"status", l:"Status", list:"statusList"},
      {k:"target_date", l:"Target Date", date:true}, {k:"last_review_date", l:"Last Review", date:true},
      {k:"next_review_date", l:"Next Review", date:true},
      {k:"risk_appetite_aligned", l:"Within risk appetite", check:true}, {k:"notes", l:"Notes", area:true}
    ];
    const rowsEl = document.getElementById("rows");
    const listStatus = document.getElementById("listStatus");
    const saveStatus = document.getElementById("saveStatus");
    const formTitle = document.getElementById("formTitle");
    const fieldset = document.getElementById("fields");
    let items = [];
    let current = null;
    let dirty = false;

    function esc(v){ return (v ?? "").toString().replace(/[&<>"']/g, m => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m])); }
    function num(v){ const n = parseInt(v,10); return Number.isNaN(n) ? 1 : Math.max(1, Math.min(5, n)); }
    function el(k){ return document.getElementById("f_" + k); }

    function fieldHTML(f){
      const id = "f_" + f.k;
      if (f.check) return '<div class="field check"><label><input id="'+id+'" data-k="'+f.k+'" type="checkbox"> '+f.l+'</label></div>';
      if (f.calc) return '<div class="field"><label>'+f.l+'</label><output id="'+id+'" class="calc"></output></div>';
      let control;
      if (f.area) control = '<textarea id="'+id+'" data-k="'+f.k+'"></textarea>';
      else if (f.score) control = '<input id="'+id+'" data-k="'+f.k+'" type="number" min="1" max="5" step="1">';
      else control = '<input id="'+id+'" data-k="'+f.k+'"'+(f.list ? ' list="'+f.list+'"' : '')+(f.date ? ' placeholder="YYYY-MM-DD"' : '')+(f.req ? ' required' : '')+'>';
      return '<div class="field"><label for="'+id+'">'+f.l+(f.req ? ' *' : '')+'</label>'+control+'</div>';
    }
    fieldset.innerHTML = fields.map(fieldHTML).join("");

    function updateScores(){
      fields.filter(f => f.calc).forEach(f => {
        el(f.k).textContent = num(el(f.calc[0]).value) * num(el(f.calc[1]).value);
      });
    }

    function setDirty(value){
      dirty = value;
      saveStatus.textContent = value ? "Unsaved changes" : "";
    }

    function confirmDiscard(){
      return !dirty || window.confirm("Discard unsaved changes to " + (current ? current.risk_id : "this risk") + "?");
    }

    function fill(item){
      current = item;
      fields.forEach(f => {
        if (f.calc) return;
        const input = el(f.k);
        if (f.check) input.checked = !!item[f.k];
        else input.value = item[f.k] ?? "";
      });
      updateScores();
      formTitle.textContent = item.risk_id + " · " + item.title;
      fieldset.disabled = false;
      saveBtn.disabled = false;
      deleteBtn.disabled = false;
      setDirty(false);
      markActive();
    }

    function clearForm(){
      current = null;
      fieldset.querySelectorAll("[data-k]").forEach(input => {
        if (input.type === "checkbox") input.checked = false; else input.value = "";
      });
      fields.filter(f => f.calc).forEach(f => { el(f.k).textContent = ""; });
      formTitle.textContent = "No risk selected";
      fieldset.disabled = true;
      saveBtn.disabled = true;
      deleteBtn.disabled = true;
      setDirty(false);
      markActive();
    }

    function markActive(){
      rowsEl.querySelectorAll(".row").forEach(r => r.classList.toggle("active", !!current && r.dataset.id === String(current.id)));
    }

    function renderList(){
      if (!items.length) {
        rowsEl.innerHTML = '<div class="empty">No risks match.</div>';
        return;
      }
      rowsEl.innerHTML = items.map(item =>
        '<button type="button" class="row" data-id="'+item.id+'">' +
          '<div class="row-title">'+esc(item.title)+'</div>' +
          '<div class="row-sub">'+esc(item.risk_id)+' · '+esc(item.status)+' · '+esc(item.owner)+' · residual '+esc(item.residual_score)+'</div>' +
        '</button>').join("");
      markActive();
    }

    async function load(){
      const q = new URLSearchParams();
      if (search.value.trim()) q.set("search", search.value.trim());
      if (statusFilter.value.trim()) q.set("status", statusFilter.value.trim());
      const res = await fetch("/risk-register/data?" + q.toString());
      if (!res.ok) {
        listStatus.textContent = "Load failed";
        return;
      }
      const data = await res.json();
      items = Array.isArray(data) ? data : (Array.isArray(data.items) ? data.items : []);
      renderList();
      listStatus.textContent = items.length + " risks";
      if (!current && items.length) fill(items[0]);
    }

    async function errorText(res){
      const text = await res.text();
      try { return JSON.parse(text).error || text; } catch (_) { return text; }
    }

    function collect(){
      const body = {};
      fieldset.querySelectorAll("[data-k]").forEach(input => {
        const k = input.getAttribute("data-k");
        if (input.type === "checkbox") body[k] = input.checked;
        else if (input.type === "number") body[k] = num(input.value);
        else body[k] = input.value;
      });
      return body;
    }

    async function save(){
      if (!current) return;
      const missing = fields.find(f => f.req && !el(f.k).value.trim());
      if (missing) {
        saveStatus.textContent = missing.l + " is required";
        el(missing.k).focus();
        return;
      }
      const res = await fetch("/risk-register/" + current.id, {
        method: "PUT",
        headers: {"Content-Type":"application/json"},
        body: JSON.stringify(collect())
      });
      if (!res.ok) {
        saveStatus.textContent = "Save failed: " + await errorText(res);
        return;
      }
      fill(await res.json());
      saveStatus.textContent = "Saved";
      await load();
    }

    async function remove(){
      if (!current || !window.confirm("Delete risk " + current.risk_id + "?")) return;
      const res = await fetch("/risk-register/" + current.id, {method:"DELETE"});
      if (!res.ok) {
        saveStatus.textContent = "Delete failed: " + await errorText(res);
        return;
      }
      clearForm();
      await load();
    }

    async function addRisk(){
      if (!confirmDiscard()) return;
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
        saveStatus.textContent = "Create failed: " + await errorText(res);
        return;
      }
      fill(await res.json());
      saveStatus.textContent = "Created";
      await load();
      el("title").focus();
      el("title").select();
    }

    rowsEl.addEventListener("click", (e) => {
      const row = e.target.closest(".row");
      if (!row || (current && row.dataset.id === String(current.id))) return;
      const item = items.find(i => String(i.id) === row.dataset.id);
      if (item && confirmDiscard()) fill(item);
    });
    fieldset.addEventListener("input", () => { setDirty(true); updateScores(); });
    editorForm.addEventListener("submit", (e) => { e.preventDefault(); save(); });
    deleteBtn.addEventListener("click", remove);
    addBtn.addEventListener("click", addRisk);
    refreshBtn.addEventListener("click", load);
    search.addEventListener("change", load);
    statusFilter.addEventListener("change", load);
    window.addEventListener("beforeunload", (e) => { if (dirty) e.preventDefault(); });
    load();
  </script>
</body>
</html>`
}
