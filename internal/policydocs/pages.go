package policydocs

import (
	"grc/internal/pageui"
)

// listPageHTML renders the policy library. manage=true turns on the mutating
// controls; the read-only variant is the same page with the editor hidden, so
// the two can never drift apart.
func listPageHTML(manage bool) string {
	manageJS := "false"
	title := "Policy Library"
	subtitle := "ISMS, ISO 27001, NIST, FedRAMP and PCI DSS documents — hierarchy, document control, and approval history."
	activePath := "/policies"
	if manage {
		manageJS = "true"
		title = "Policy Editor"
		subtitle = "Author and approve policy-family documents. Sections are editable in draft only; approving snapshots the text."
		activePath = "/policies/manage"
	}

	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + title + ` · GRC</title>
  <style>` + policyCSS + `</style>
</head>
<body>
  <main>
    ` + pageui.Nav(activePath) + `
    <h1>` + title + `</h1>
    <p class="subtitle">` + subtitle + `</p>

    <section class="toolbar">
      <input id="searchInput" type="search" placeholder="Search title, reference or summary">
      <select id="typeFilter"><option value="">All types</option></select>
      <select id="statusFilter"><option value="">All statuses</option></select>
      <select id="frameworkFilter"><option value="">All frameworks</option></select>
      <label class="check"><input id="dueReview" type="checkbox"> Due for review</label>
      <button id="newBtn" class="primary" type="button">New document</button>
    </section>

    <div class="summary"><span id="status">Loading…</span><span id="selected"></span></div>

    <section class="layout">
      <div class="panel">
        <div class="panel-header"><strong>Documents</strong></div>
        <div id="docList" class="rows"></div>
      </div>
      <div class="panel">
        <div class="panel-header"><strong>Document</strong></div>
        <div id="detail" class="detail-body">
          <p class="muted">Select a document, or create one.</p>
        </div>
      </div>
    </section>
  </main>
  <script>
    const MANAGE = ` + manageJS + `;
    ` + policyJS + `
  </script>
</body>
</html>`
}

const policyCSS = `
:root { color-scheme: light; --ink:#1c2431; --muted:#5e6672; --line:#d7cebf; --accent:#8b3d2e; --green:#0b5d3b; --amber:#9a6700; }
* { box-sizing: border-box; }
body { margin:0; font-family: Georgia, "Times New Roman", serif; color:var(--ink);
       background:linear-gradient(135deg,#f7f3eb,#ece4d6 55%,#e4d8c4); }
main { max-width:1440px; margin:24px auto; padding:24px; background:rgba(255,252,246,0.92);
       border:1px solid rgba(215,206,191,0.8); border-radius:24px; }
.tabs { display:flex; gap:10px; flex-wrap:wrap; margin-bottom:20px; }
.tab { padding:10px 14px; border-radius:999px; background:#efe6d6; border:1px solid var(--line);
       color:var(--ink); text-decoration:none; font-family:Arial,sans-serif; font-size:13px;
       letter-spacing:0.04em; text-transform:uppercase; }
.tab.active { background:#e1d0b7; border-color:#b89d78; }
h1 { margin:0 0 6px; }
.subtitle { color:var(--muted); font-family:Arial,sans-serif; font-size:14px; margin:0 0 14px; }
input, select, button, textarea { font:inherit; }
.toolbar { display:flex; gap:8px; flex-wrap:wrap; align-items:center; margin-bottom:10px; }
.toolbar input[type=search], .toolbar select { padding:8px 10px; border:1px solid var(--line); border-radius:10px; background:#fff; }
.check { font-family:Arial,sans-serif; font-size:13px; display:flex; align-items:center; gap:6px; }
button { padding:8px 12px; border-radius:999px; border:1px solid #b89d78; background:#e1d0b7; cursor:pointer; }
button.primary { background:var(--green); color:#fff; border-color:var(--green); }
button.danger { background:#f0e2dd; color:var(--accent); border-color:var(--accent); }
button:disabled { opacity:0.5; cursor:not-allowed; }
.summary { display:flex; justify-content:space-between; gap:12px; flex-wrap:wrap;
           color:var(--muted); font-family:Arial,sans-serif; font-size:13px; margin-bottom:12px; }
.layout { display:grid; grid-template-columns:minmax(0,0.8fr) minmax(0,1.2fr); gap:16px; align-items:start; }
.panel { border:1px solid var(--line); border-radius:16px; background:#fff; overflow:hidden; }
.panel-header { padding:10px 14px; border-bottom:1px solid var(--line); background:#efe6d6;
                font-family:Arial,sans-serif; font-size:12px; text-transform:uppercase; letter-spacing:0.06em; }
.rows { max-height:70vh; overflow:auto; }
.row { padding:10px 14px; border-bottom:1px solid rgba(215,206,191,0.6); cursor:pointer; }
.row:hover, .row.active { background:rgba(139,61,46,0.07); }
.row.active { box-shadow: inset 3px 0 0 var(--accent); }
.row-top { display:flex; justify-content:space-between; gap:8px; align-items:baseline; }
.row-ref { font-family:"Courier New",monospace; font-weight:bold; font-size:13px; }
.row-title { font-size:14px; }
.row-meta { font-family:Arial,sans-serif; font-size:11px; color:var(--muted); margin-top:3px; }
.detail-body { padding:14px; max-height:70vh; overflow:auto; }
/* The status pills carry their own light backgrounds, so they also have to
   carry their own text colour: the injected dark theme sets a light colour on
   the containing .panel, and an inherited value would leave the pill's text
   invisible against its own background. */
.pill { display:inline-block; padding:2px 9px; border-radius:999px; font-family:Arial,sans-serif;
        font-size:11px; text-transform:uppercase; letter-spacing:0.05em; border:1px solid var(--line);
        background:#ece3d2; color:#1c2431; }
.pill.draft { background:#ece3d2; color:#1c2431; }
.pill.in_review { background:#f6e6c2; border-color:var(--amber); color:#6b4700; }
.pill.approved { background:#d8ecdf; border-color:var(--green); color:var(--green); }
.pill.retired { background:#e6e6e6; color:var(--muted); }
.fields { display:grid; grid-template-columns:repeat(auto-fit,minmax(190px,1fr)); gap:10px; margin:10px 0 14px; }
.field { display:flex; flex-direction:column; gap:4px; }
.field label { font-family:Arial,sans-serif; font-size:11px; text-transform:uppercase;
               letter-spacing:0.05em; color:var(--muted); }
.field input, .field select, .field textarea { padding:7px 9px; border:1px solid var(--line); border-radius:8px; background:#fff; }
.field textarea { min-height:60px; resize:vertical; }
.frameworks { display:flex; flex-wrap:wrap; gap:8px; }
.frameworks label { font-family:Arial,sans-serif; font-size:12px; display:flex; align-items:center; gap:5px; }
h3 { font-family:Arial,sans-serif; font-size:12px; text-transform:uppercase; letter-spacing:0.06em;
     color:var(--muted); margin:18px 0 8px; }
/* No background of its own: the injected themes recolour the surrounding
   panel but not this class, so a hardcoded light fill would strand the
   inherited light text on it. The border alone carries the grouping. */
.section { border:1px solid var(--line); border-radius:12px; padding:10px 12px; margin-bottom:10px; }
.section-top { display:flex; justify-content:space-between; gap:8px; align-items:center; flex-wrap:wrap; margin-bottom:6px; }
.section-body { white-space:pre-wrap; font-size:14px; line-height:1.5; }
.section textarea { width:100%; min-height:110px; }
.tag { display:inline-block; padding:2px 8px; border-radius:999px; background:#ece3d2;
       font-family:Arial,sans-serif; font-size:11px; }
.tag.ai { background:#e3ddf3; }
.tag.imported { background:#dde8f3; }
/* Severity is carried by the left border rather than a fill, for the same
   reason as .section — it reads correctly in all four themes. */
.finding { border-left:3px solid var(--amber); padding:6px 10px; margin-bottom:6px;
           font-family:Arial,sans-serif; font-size:12.5px; }
.finding.error { border-left-color:var(--accent); }
.actions { display:flex; gap:8px; flex-wrap:wrap; margin:14px 0; }
.muted { color:var(--muted); font-family:Arial,sans-serif; font-size:13px; }
/* Control-mapping chips. Like .section and .finding these carry no fill, so
   they read correctly under every injected theme. */
.ctlrow { display:flex; flex-wrap:wrap; gap:6px; align-items:center; margin-top:8px; }
.ctlchip { display:inline-flex; align-items:center; gap:5px; padding:2px 4px 2px 9px;
           border:1px solid var(--line); border-radius:999px;
           font-family:"Courier New",monospace; font-size:12px; }
.ctlchip button { border:0; background:transparent; cursor:pointer; padding:0 5px;
                  font-size:14px; line-height:1; border-radius:999px; }
.ctladd { position:relative; display:inline-flex; gap:5px; align-items:center; }
.ctladd .ctl_q { padding:5px 9px; border:1px solid var(--line); border-radius:999px; min-width:190px; font-size:13px; }
.ctladd .ctl_cov { padding:4px 6px; border:1px solid var(--line); border-radius:999px; font-size:12px; }
.ctl_results { position:absolute; top:100%; left:0; z-index:20; min-width:340px; max-height:230px;
               overflow:auto; border:1px solid var(--line); border-radius:10px; margin-top:4px;
               background:var(--panel, #fff); box-shadow:0 8px 22px rgba(0,0,0,0.22); }
.ctl_results:empty { display:none; }
.ctl_pick { display:block; width:100%; text-align:left; border:0; border-bottom:1px solid var(--line);
            border-radius:0; padding:7px 10px; cursor:pointer; font-family:Arial,sans-serif; font-size:12.5px; }
.ctl_pick:last-child { border-bottom:0; }
.version { border-bottom:1px solid rgba(215,206,191,0.6); padding:7px 0; font-family:Arial,sans-serif; font-size:12.5px; }
.version:last-child { border-bottom:0; }
.err { color:var(--accent); font-family:Arial,sans-serif; font-size:13px; margin:8px 0; }
@media (max-width:900px) { .layout { grid-template-columns:1fr; } .rows { max-height:40vh; } .detail-body { max-height:none; } }
`

// policyJS is the page controller. Dependency-free, matching the rest of the
// app's hand-written page scripts.
const policyJS = `
const state = { meta:null, docs:[], selected:null, sections:[], findings:[], versions:[], refs:[], error:"" };

const el = (id) => document.getElementById(id);
function esc(v){ return (v ?? "").toString().replace(/[&<>"']/g, m => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m])); }
function labelOf(kind){ const m=(state.meta&&state.meta.section_kinds||[]).find(k=>k.value===kind); return m?m.label:kind; }
function titleCase(v){ return String(v||"").replace(/_/g," ").replace(/\b\w/g, c=>c.toUpperCase()); }

async function api(method, url, body){
  const opts = { method, headers: {} };
  if (body !== undefined) { opts.headers["Content-Type"]="application/json"; opts.body = JSON.stringify(body); }
  const res = await fetch(url, opts);
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { const err = new Error(data.error || ("HTTP " + res.status)); err.payload = data; throw err; }
  return data;
}

async function loadMeta(){
  state.meta = await api("GET", "/policies/meta");
  fillSelect(el("typeFilter"), state.meta.doc_types, "All types", titleCase);
  fillSelect(el("statusFilter"), state.meta.statuses, "All statuses", titleCase);
  fillSelect(el("frameworkFilter"), state.meta.frameworks, "All frameworks");
}

function fillSelect(sel, values, allLabel, fmt){
  sel.innerHTML = '<option value="">' + esc(allLabel) + '</option>' +
    values.map(v => '<option value="'+esc(v)+'">'+esc(fmt?fmt(v):v)+'</option>').join("");
}

function filterParams(){
  const p = new URLSearchParams();
  if (el("searchInput").value.trim()) p.set("search", el("searchInput").value.trim());
  if (el("typeFilter").value) p.set("doc_type", el("typeFilter").value);
  if (el("statusFilter").value) p.set("status", el("statusFilter").value);
  if (el("frameworkFilter").value) p.set("framework", el("frameworkFilter").value);
  if (el("dueReview").checked) p.set("due_review", "1");
  return p.toString();
}

async function loadDocs(){
  const q = filterParams();
  try {
    state.docs = await api("GET", "/policies/data" + (q ? "?"+q : ""));
    el("status").textContent = "Showing " + state.docs.length + " document(s)";
    if (state.selected && !state.docs.find(d => d.id === state.selected.id)) state.selected = null;
    // Land on a document rather than an empty pane, the way /controls does.
    if (!state.selected && state.docs.length) state.selected = state.docs[0];
    renderList();
    if (state.selected) await selectDoc(state.selected.id); else renderDetail();
  } catch(e){ el("status").textContent = "Load failed: " + e.message; }
}

function renderList(){
  if (!state.docs.length){ el("docList").innerHTML = '<p class="muted" style="padding:14px;">No documents match.</p>'; return; }
  el("docList").innerHTML = state.docs.map(d => {
    const active = state.selected && state.selected.id === d.id ? " active" : "";
    const due = d.next_review_date && d.status !== "retired" && d.next_review_date <= new Date().toISOString().slice(0,10);
    return '<div class="row'+active+'" data-id="'+d.id+'">' +
      '<div class="row-top"><span class="row-ref">'+esc(d.reference || "—")+'</span>' +
        '<span class="pill '+esc(d.status)+'">'+esc(titleCase(d.status))+'</span></div>' +
      '<div class="row-title">'+esc(d.title)+'</div>' +
      '<div class="row-meta">'+esc(titleCase(d.doc_type))+
        (d.client_name ? " · " + esc(d.client_name) : "") +
        " · " + d.section_count + " section(s)" +
        (d.latest_version_label ? " · " + esc(d.latest_version_label) : "") +
        (due ? ' · <strong style="color:#8b3d2e;">review due</strong>' : "") +
      '</div></div>';
  }).join("");
  el("docList").querySelectorAll(".row").forEach(r =>
    r.addEventListener("click", () => selectDoc(Number(r.getAttribute("data-id")))));
}

async function selectDoc(id){
  try {
    state.selected = await api("GET", "/policies/"+id+"/data");
    const [sections, lint, versions, refs] = await Promise.all([
      api("GET", "/policies/"+id+"/sections"),
      api("GET", "/policies/"+id+"/lint"),
      api("GET", "/policies/"+id+"/versions"),
      api("GET", "/policies/"+id+"/controls")
    ]);
    state.sections = sections; state.findings = lint.findings || [];
    state.versions = versions; state.refs = refs;
    state.error = "";
    el("selected").textContent = "Selected: " + state.selected.title;
    renderList(); renderDetail();
  } catch(e){ state.error = e.message; renderDetail(); }
}

function renderDetail(){
  const d = state.selected;
  if (!d){ el("detail").innerHTML = '<p class="muted">Select a document, or create one.</p>'; return; }
  const editable = MANAGE && d.status === "draft";

  let html = "";
  if (state.error) html += '<div class="err">'+esc(state.error)+'</div>';

  html += '<div class="row-top"><h2 style="margin:0;font-size:1.3rem;">'+esc(d.title)+'</h2>' +
          '<span class="pill '+esc(d.status)+'">'+esc(titleCase(d.status))+'</span></div>';

  html += '<div class="actions">' +
    '<a class="tab" href="/policies/'+d.id+'/view" target="_blank" rel="noopener noreferrer">Preview</a>' +
    '<a class="tab" href="/policies/'+d.id+'/export.md">Export Markdown</a>' +
    '<a class="tab" href="/policies/'+d.id+'/export.html">Export HTML</a>' +
    '<a class="tab" href="/policies/'+d.id+'/export.json" title="Render payload for templates/build.sh">Export JSON</a>' +
    '<a class="tab" href="/templates/render?doc='+d.id+'" title="Typeset as a client deliverable. Returns the render sources when this host has no typesetting engine.">Render PDF</a>' +
    (MANAGE ? statusActions(d) : "") + '</div>';

  html += '<h3>Document Control</h3>' + controlFields(d, MANAGE);

  html += '<h3>Readiness</h3>';
  if (!state.findings.length) html += '<p class="muted">No issues found.</p>';
  else html += state.findings.map(f =>
    '<div class="finding '+(f.severity==="error"?"error":"")+'">' +
    '<strong>'+esc(f.severity==="error"?"Blocks approval":"Advisory")+'</strong>' +
    (f.heading ? " · " + esc(f.heading) : "") + " — " + esc(f.message) + '</div>').join("");

  html += '<h3>Sections</h3>';
  if (!state.sections.length) html += '<p class="muted">No sections yet.</p>';
  else html += state.sections.map((s,i) => sectionHTML(s, i, editable)).join("");
  if (editable) html += '<button id="addSection" type="button">Add section</button>';
  else if (MANAGE && d.status !== "draft")
    html += '<p class="muted">Sections are locked while the document is '+esc(titleCase(d.status))+'. Reopen it to draft to edit.</p>';

  html += '<h3>Approval History</h3>';
  if (!state.versions.length) html += '<p class="muted">Never approved.</p>';
  else html += state.versions.map(v =>
    '<div class="version"><strong>'+esc(v.version_label)+'</strong> — approved by '+esc(v.approved_by)+
    ' on '+esc((v.approved_at||"").slice(0,10))+
    (v.change_summary ? '<br><span class="muted">'+esc(v.change_summary)+'</span>' : '')+'</div>').join("");

  el("detail").innerHTML = html;
  wireDetail(d, editable);
}

function statusActions(d){
  const b = [];
  if (d.status === "draft") b.push('<button id="submitBtn" type="button">Submit for review</button>');
  if (d.status === "in_review") {
    b.push('<button id="approveBtn" class="primary" type="button">Approve</button>');
    b.push('<button id="reopenBtn" type="button">Return to draft</button>');
  }
  if (d.status === "approved") {
    b.push('<button id="reopenBtn" type="button">Revise (return to draft)</button>');
    b.push('<button id="retireBtn" type="button">Retire</button>');
  }
  if (d.status === "retired") b.push('<button id="reopenBtn" type="button">Reinstate as draft</button>');
  b.push('<button id="deleteBtn" class="danger" type="button">Delete</button>');
  return b.join("");
}

function controlFields(d, editing){
  if (!editing) {
    const rows = [
      ["Reference", d.reference], ["Type", titleCase(d.doc_type)], ["Classification", d.classification],
      ["Owner role", d.owner_role], ["Approver", d.approver], ["Client", d.client_name],
      ["Frameworks", (d.frameworks||[]).join(", ")], ["Effective", d.effective_date],
      ["Review cadence", d.review_cadence_months + " months"], ["Next review", d.next_review_date],
      ["Parent", d.parent_title]
    ];
    return '<div class="fields">' + rows.map(([k,v]) =>
      '<div class="field"><label>'+esc(k)+'</label><div>'+esc(v || "Not set")+'</div></div>').join("") + '</div>';
  }

  const opts = (vals, cur, fmt) => vals.map(v =>
    '<option value="'+esc(v)+'"'+(v===cur?" selected":"")+'>'+esc(fmt?fmt(v):v)+'</option>').join("");
  const parents = state.docs.filter(x => x.id !== d.id);

  return '<div class="fields">' +
    field("Reference", '<input id="f_reference" value="'+esc(d.reference)+'">') +
    field("Title", '<input id="f_title" value="'+esc(d.title)+'">') +
    field("Type", '<select id="f_doc_type">'+opts(state.meta.doc_types, d.doc_type, titleCase)+'</select>') +
    field("Classification", '<select id="f_classification">'+opts(state.meta.classifications, d.classification)+'</select>') +
    field("Owner role", '<input id="f_owner_role" value="'+esc(d.owner_role)+'" placeholder="e.g. Head of Security">') +
    field("Approver", '<input id="f_approver" value="'+esc(d.approver)+'">') +
    field("Effective date", '<input id="f_effective_date" type="date" value="'+esc(d.effective_date)+'">') +
    field("Review cadence (months)", '<input id="f_review_cadence_months" type="number" min="1" max="120" value="'+d.review_cadence_months+'">') +
    field("Parent document", '<select id="f_parent_document_id"><option value="0">None</option>' +
      parents.map(p => '<option value="'+p.id+'"'+(p.id===d.parent_document_id?" selected":"")+'>'+
        esc((p.reference?p.reference+" — ":"")+p.title)+'</option>').join("") + '</select>') +
  '</div>' +
  '<div class="field"><label>Summary</label><textarea id="f_summary">'+esc(d.summary)+'</textarea></div>' +
  '<div class="field" style="margin-top:10px;"><label>Frameworks</label><div class="frameworks">' +
    state.meta.frameworks.map(f => '<label><input type="checkbox" class="fw" value="'+esc(f)+'"'+
      ((d.frameworks||[]).includes(f)?" checked":"")+'> '+esc(f)+'</label>').join("") + '</div></div>' +
  '<div class="actions"><button id="saveDoc" class="primary" type="button">Save document control</button></div>';
}

function field(label, control){
  return '<div class="field"><label>'+esc(label)+'</label>'+control+'</div>';
}

function refsFor(sectionID){
  return (state.refs || []).filter(r => r.section_id === sectionID);
}

// Mapped controls render as chips under the section that makes the claim, so
// the coverage assertion sits next to the text that backs it.
function refChips(sectionID, editable){
  const refs = refsFor(sectionID);
  if (!refs.length && !editable) return "";
  const chips = refs.map(r => {
    const stale = r.known ? "" : ' title="No longer in the control catalog" style="border-color:var(--accent);"';
    const label = esc(r.control_id) + (r.coverage !== "full" ? " · " + esc(r.coverage) : "") + (r.known ? "" : " ⚠");
    return '<span class="ctlchip"'+stale+'>' + label +
      (editable ? '<button class="ref_del" data-ref="'+r.id+'" type="button" title="Remove mapping">×</button>' : '') +
      '</span>';
  }).join("");
  const adder = editable
    ? '<span class="ctladd"><input class="ctl_q" type="search" placeholder="Map a control (e.g. AC-2)" autocomplete="off">' +
      '<select class="ctl_cov">' + (state.meta.coverage_levels||["full"]).map(c =>
        '<option value="'+esc(c)+'">'+esc(c)+'</option>').join("") + '</select>' +
      '<div class="ctl_results"></div></span>'
    : "";
  return '<div class="ctlrow">' + (chips || (editable ? "" : "")) + adder + '</div>';
}

function sectionHTML(s, index, editable){
  const prov = s.provenance && s.provenance !== "human"
    ? '<span class="tag '+esc(s.provenance)+'">'+esc(s.provenance)+'</span>' : "";
  if (!editable) {
    return '<div class="section" data-section="'+s.id+'"><div class="section-top"><strong>'+esc(s.heading)+'</strong>' +
      '<span><span class="tag">'+esc(labelOf(s.section_kind))+'</span> '+prov+'</span></div>' +
      '<div class="section-body">'+esc(s.body || "No content.")+'</div>' +
      refChips(s.id, false) + '</div>';
  }
  const kinds = state.meta.section_kinds.map(k =>
    '<option value="'+esc(k.value)+'"'+(k.value===s.section_kind?" selected":"")+'>'+esc(k.label)+'</option>').join("");
  return '<div class="section" data-section="'+s.id+'">' +
    '<div class="section-top">' +
      '<input class="s_heading" value="'+esc(s.heading)+'" style="flex:1;min-width:160px;padding:6px 8px;border:1px solid var(--line);border-radius:8px;">' +
      '<select class="s_kind">'+kinds+'</select>' +
      '<button class="s_up" type="button" title="Move up"'+(index===0?" disabled":"")+'>↑</button>' +
      '<button class="s_down" type="button" title="Move down"'+(index===state.sections.length-1?" disabled":"")+'>↓</button>' +
      '<button class="s_del danger" type="button">Delete</button>' +
    '</div>' +
    '<textarea class="s_body">'+esc(s.body)+'</textarea>' +
    refChips(s.id, true) +
    '<div class="actions"><button class="s_save" type="button">Save section</button>'+prov+'</div>' +
  '</div>';
}

function wireDetail(d, editable){
  const on = (id, fn) => { const n = el(id); if (n) n.addEventListener("click", fn); };

  on("submitBtn", () => act("POST", "/policies/"+d.id+"/submit"));
  on("retireBtn", () => act("POST", "/policies/"+d.id+"/retire"));
  on("reopenBtn", () => act("POST", "/policies/"+d.id+"/reopen"));
  on("approveBtn", async () => {
    const by = prompt("Approved by (name or role):", d.approver || "");
    if (by === null) return;
    const summary = prompt("Change summary for this version (optional):", "") || "";
    await act("POST", "/policies/"+d.id+"/approve", { approved_by: by, change_summary: summary });
  });
  on("deleteBtn", async () => {
    if (!confirm("Delete \"" + d.title + "\"? This cannot be undone.")) return;
    try { await api("DELETE", "/policies/"+d.id); state.selected = null; await loadDocs(); }
    catch(e){ state.error = e.message; renderDetail(); }
  });

  on("saveDoc", async () => {
    const payload = {
      client_id: d.client_id,
      client_name: d.client_name,
      reference: el("f_reference").value,
      title: el("f_title").value,
      doc_type: el("f_doc_type").value,
      classification: el("f_classification").value,
      owner_role: el("f_owner_role").value,
      approver: el("f_approver").value,
      effective_date: el("f_effective_date").value,
      review_cadence_months: Number(el("f_review_cadence_months").value) || 12,
      parent_document_id: Number(el("f_parent_document_id").value) || 0,
      summary: el("f_summary").value,
      frameworks: Array.from(document.querySelectorAll(".fw:checked")).map(n => n.value)
    };
    try { await api("PUT", "/policies/"+d.id, payload); await loadDocs(); }
    catch(e){ state.error = e.message; renderDetail(); }
  });

  on("addSection", async () => {
    try {
      await api("POST", "/policies/"+d.id+"/sections",
        { heading:"New section", body:"", section_kind:"statements", provenance:"human" });
      await selectDoc(d.id);
    } catch(e){ state.error = e.message; renderDetail(); }
  });

  wireControlChips(d, editable);

  if (!editable) return;
  el("detail").querySelectorAll(".section[data-section]").forEach(node => {
    const id = Number(node.getAttribute("data-section"));
    node.querySelector(".s_save").addEventListener("click", async () => {
      try {
        await api("PUT", "/policies/"+d.id+"/sections/"+id, {
          heading: node.querySelector(".s_heading").value,
          body: node.querySelector(".s_body").value,
          section_kind: node.querySelector(".s_kind").value,
          provenance: "human"
        });
        await selectDoc(d.id);
      } catch(e){ state.error = e.message; renderDetail(); }
    });
    node.querySelector(".s_del").addEventListener("click", async () => {
      if (!confirm("Delete this section?")) return;
      try { await api("DELETE", "/policies/"+d.id+"/sections/"+id); await selectDoc(d.id); }
      catch(e){ state.error = e.message; renderDetail(); }
    });
    node.querySelector(".s_up").addEventListener("click", () => move(d.id, id, -1));
    node.querySelector(".s_down").addEventListener("click", () => move(d.id, id, 1));
  });
}

function wireControlChips(d, editable){
  if (!editable) return;
  el("detail").querySelectorAll(".section[data-section]").forEach(node => {
    const sectionID = Number(node.getAttribute("data-section"));

    node.querySelectorAll(".ref_del").forEach(btn =>
      btn.addEventListener("click", async () => {
        try {
          await api("DELETE", "/policies/"+d.id+"/sections/"+sectionID+"/controls/"+btn.getAttribute("data-ref"));
          await selectDoc(d.id);
        } catch(e){ state.error = e.message; renderDetail(); }
      }));

    const q = node.querySelector(".ctl_q");
    const results = node.querySelector(".ctl_results");
    if (!q || !results) return;

    let timer = null;
    q.addEventListener("input", () => {
      clearTimeout(timer);
      const term = q.value.trim();
      if (term.length < 2){ results.innerHTML = ""; return; }
      // Debounced: the catalog search runs per keystroke otherwise.
      timer = setTimeout(async () => {
        try {
          const options = await api("GET", "/policies/control-search?q=" + encodeURIComponent(term));
          results.innerHTML = options.length
            ? options.map(o => '<button class="ctl_pick" type="button" data-cid="'+esc(o.control_id)+'">' +
                '<strong>'+esc(o.control_id)+'</strong> '+esc(o.name)+
                (o.baselines ? ' <span class="muted">'+esc(o.baselines)+'</span>' : '') + '</button>').join("")
            : '<div class="muted" style="padding:6px 8px;">No matching controls.</div>';
          results.querySelectorAll(".ctl_pick").forEach(btn =>
            btn.addEventListener("click", async () => {
              try {
                await api("POST", "/policies/"+d.id+"/sections/"+sectionID+"/controls", {
                  control_id: btn.getAttribute("data-cid"),
                  coverage: node.querySelector(".ctl_cov").value
                });
                await selectDoc(d.id);
              } catch(e){ state.error = e.message; renderDetail(); }
            }));
        } catch(e){ results.innerHTML = '<div class="muted" style="padding:6px 8px;">'+esc(e.message)+'</div>'; }
      }, 220);
    });
  });
}

async function move(docID, sectionID, delta){
  const ids = state.sections.map(s => s.id);
  const i = ids.indexOf(sectionID);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= ids.length) return;
  ids[i] = ids[j]; ids[j] = sectionID;
  try { await api("POST", "/policies/"+docID+"/sections/reorder", { section_ids: ids }); await selectDoc(docID); }
  catch(e){ state.error = e.message; renderDetail(); }
}

async function act(method, url, body){
  try { await api(method, url, body); await loadDocs(); }
  catch(e){
    // The approval gate answers with the findings that blocked it, which is
    // more useful than the generic message.
    if (e.payload && e.payload.findings) {
      state.findings = e.payload.findings;
      state.error = "Approval blocked: " + e.payload.findings.length + " issue(s) must be fixed.";
    } else { state.error = e.message; }
    renderDetail();
  }
}

async function createDoc(){
  const title = prompt("Document title:");
  if (!title) return;
  try {
    const created = await api("POST", "/policies", { title: title, doc_type: "policy" });
    await loadDocs();
    await selectDoc(created.id);
  } catch(e){ el("status").textContent = "Create failed: " + e.message; }
}

(async function init(){
  try { await loadMeta(); } catch(e){ el("status").textContent = "Metadata failed: " + e.message; return; }
  ["searchInput","typeFilter","statusFilter","frameworkFilter","dueReview"].forEach(id => {
    const n = el(id);
    n.addEventListener(id === "searchInput" ? "input" : "change", () => loadDocs());
  });
  const newBtn = el("newBtn");
  if (MANAGE) newBtn.addEventListener("click", createDoc); else newBtn.style.display = "none";
  await loadDocs();
})();
`
