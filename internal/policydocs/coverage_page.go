package policydocs

import (
	"grc/internal/pageui"
)

// coveragePageHTML renders the framework coverage matrix: every in-scope
// catalog control against whatever policy text claims it. The counts at the top
// are the point — "how many required controls have no approved policy" is the
// question a client actually asks.
func coveragePageHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Policy Coverage · GRC</title>
  <style>` + policyCSS + `
    .cards { display:grid; grid-template-columns:repeat(auto-fit,minmax(140px,1fr)); gap:10px; margin:6px 0 16px; }
    .card { border:1px solid var(--line); border-radius:14px; padding:10px 14px; }
    .card .num { font-size:26px; font-weight:700; font-family:Arial,sans-serif; }
    .card .lbl { font-family:Arial,sans-serif; font-size:11px; text-transform:uppercase;
                 letter-spacing:0.05em; color:var(--muted); margin-top:2px; }
    .card.approved .num { color:var(--green); }
    .card.draft_only .num { color:var(--amber); }
    .card.supporting_only .num { color:var(--amber); }
    .card.uncovered .num { color:var(--accent); }
    table { width:100%; border-collapse:collapse; font-family:Arial,sans-serif; font-size:13px; }
    th, td { border:1px solid var(--line); padding:6px 9px; text-align:left; vertical-align:top; }
    /* Explicit colour, not inherited: these are bare <th> in an implicit tbody,
       so the injected dark theme's "thead th" rule does not reach them and the
       inherited light text would vanish against this light background. */
    th { position:sticky; top:0; background:#efe6d6; color:#1c2431; }
    /* Coverage-specific statuses. draft-only and supporting-only are amber
       rather than green on purpose: both read as covered in a spreadsheet and
       neither survives an assessor asking which approved document says so. */
    .pill.draft_only, .pill.supporting_only { background:#f6e6c2; border-color:var(--amber); color:#6b4700; }
    .pill.uncovered { background:#f0e2dd; border-color:var(--accent); color:var(--accent); }
    .cid { font-family:"Courier New",monospace; font-weight:bold; white-space:nowrap; }
    .claim { display:block; font-size:12px; margin-bottom:2px; }
    .claim:last-child { margin-bottom:0; }
    .none { color:var(--muted); font-style:italic; }
    .orphans { border:1px solid var(--accent); border-radius:12px; padding:10px 14px; margin:16px 0; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/policies/coverage") + `
    <h1>Policy Coverage</h1>
    <p class="subtitle">Which catalog controls have policy text behind them — and which only have a draft.</p>

    <section class="toolbar">
      <select id="baseline">
        <option value="">All controls</option>
        <option value="low">Low baseline</option>
        <option value="moderate" selected>Moderate baseline</option>
        <option value="high">High baseline</option>
        <option value="privacy">Privacy baseline</option>
      </select>
      <select id="family"><option value="">All families</option></select>
      <label class="check"><input id="enhancements" type="checkbox"> Include enhancements</label>
      <label class="check"><input id="gaps" type="checkbox"> Gaps only</label>
      <button id="exportBtn" type="button">Export CSV</button>
    </section>

    <div class="cards" id="cards"></div>
    <div id="orphans"></div>
    <div class="summary"><span id="status">Loading…</span></div>
    <div class="table-wrap" style="max-height:66vh;overflow:auto;"><table id="grid"></table></div>
  </main>
  <script>
` + coverageJS + `
  </script>
</body>
</html>`
}

const coverageJS = `
const el = (id) => document.getElementById(id);
function esc(v){ return (v ?? "").toString().replace(/[&<>"']/g, m => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m])); }
let lastReport = null;

const STATUS_LABEL = {
  approved: "Approved policy",
  draft_only: "Draft only",
  supporting_only: "Supporting only",
  uncovered: "No coverage"
};

function params(){
  const p = new URLSearchParams();
  if (el("baseline").value) p.set("baseline", el("baseline").value);
  if (el("family").value) p.set("family", el("family").value);
  if (el("enhancements").checked) p.set("enhancements", "1");
  if (el("gaps").checked) p.set("gaps", "1");
  return p.toString();
}

async function load(){
  el("status").textContent = "Loading…";
  try {
    const res = await fetch("/policies/coverage/data?" + params());
    const d = await res.json();
    if (!res.ok) throw new Error(d.error || ("HTTP " + res.status));
    lastReport = d;
    render(d);
  } catch(e){ el("status").textContent = "Load failed: " + e.message; }
}

function card(cls, num, lbl){
  return '<div class="card '+cls+'"><div class="num">'+num+'</div><div class="lbl">'+esc(lbl)+'</div></div>';
}

function render(d){
  el("cards").innerHTML =
    card("", d.total_controls, "In scope") +
    card("approved", d.approved, "Approved policy") +
    card("draft_only", d.draft_only, "Draft only") +
    card("supporting_only", d.supporting_only, "Supporting only") +
    card("uncovered", d.uncovered, "No coverage");

  el("orphans").innerHTML = (d.orphans && d.orphans.length)
    ? '<div class="orphans"><strong>' + d.orphans.length + ' stale mapping(s)</strong> point at controls the catalog no longer has: ' +
      esc(d.orphans.map(o => o.control_id).join(", ")) +
      '. These are reported rather than deleted — a stale claim is a finding.</div>'
    : "";

  const rows = d.rows || [];
  el("status").textContent = "Showing " + rows.length + " control(s)";
  el("grid").innerHTML =
    "<tr><th>Control</th><th>Name</th><th>Family</th><th>Status</th><th>Claimed by</th></tr>" +
    (rows.length ? rows.map(r =>
      "<tr>" +
      '<td class="cid">'+esc(r.control_id)+"</td>" +
      "<td>"+esc(r.name)+"</td>" +
      "<td>"+esc(r.family)+"</td>" +
      '<td><span class="pill '+esc(r.status)+'">'+esc(STATUS_LABEL[r.status] || r.status)+"</span></td>" +
      "<td>" + ((r.claims||[]).length
        ? r.claims.map(c => '<span class="claim"><a href="/policies/'+c.document_id+'/view" target="_blank" rel="noopener noreferrer">'+
            esc((c.reference ? c.reference+" — " : "")+c.title)+'</a> · '+esc(c.section_heading)+
            ' · '+esc(c.coverage)+' · '+esc(c.doc_status)+'</span>').join("")
        : '<span class="none">Nothing claims this control.</span>') + "</td>" +
      "</tr>").join("")
    : "<tr><td colspan='5' class='none'>No controls match.</td></tr>");
}

function csvCell(v){
  const s = (v ?? "").toString();
  return /[",\n]/.test(s) ? '"' + s.replaceAll('"', '""') + '"' : s;
}

function exportCSV(){
  if (!lastReport) return;
  const lines = [["Control","Name","Family","Status","Claimed By","Section","Coverage","Document Status"].join(",")];
  for (const r of (lastReport.rows||[])) {
    if (!(r.claims||[]).length) {
      lines.push([r.control_id, r.name, r.family, STATUS_LABEL[r.status]||r.status, "", "", "", ""].map(csvCell).join(","));
      continue;
    }
    for (const c of r.claims) {
      lines.push([r.control_id, r.name, r.family, STATUS_LABEL[r.status]||r.status,
        (c.reference ? c.reference+" — " : "")+c.title, c.section_heading, c.coverage, c.doc_status].map(csvCell).join(","));
    }
  }
  const blob = new Blob([lines.join("\n")], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "policy-coverage.csv";
  a.click();
  URL.revokeObjectURL(url);
}

(async function init(){
  try {
    const res = await fetch("/controls/family-visibility/data");
    if (res.ok) {
      const families = await res.json();
      el("family").innerHTML = '<option value="">All families</option>' +
        families.filter(f => f.enabled).map(f => '<option value="'+esc(f.family)+'">'+esc(f.family)+'</option>').join("");
    }
  } catch(e){ /* the family filter is a convenience; the report works without it */ }

  ["baseline","family","enhancements","gaps"].forEach(id => el(id).addEventListener("change", load));
  el("exportBtn").addEventListener("click", exportCSV);
  await load();
})();
`
