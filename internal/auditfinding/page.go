package auditfinding

import "grc/internal/pageui"

func pageHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Audit Findings · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
    }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: Georgia, "Times New Roman", serif; color: var(--ink); background: var(--bg); }
    main {
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--bg);
      border: 1px solid var(--line);
      border-radius: 24px;
    }
    h1 { margin: 0 0 6px; font-size: clamp(2rem, 4vw, 3rem); line-height: 1; }
    h2 { font-size: 1.1rem; margin: 0; }
    p.lede { color: var(--muted); max-width: 80ch; margin: 0 0 16px; }
    .sans { font-family: Arial, sans-serif; }
    .kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; margin: 16px 0; }
    .kpi { background: var(--panel); border: 1px solid var(--line); border-radius: 14px; padding: 12px 14px; font-family: Arial, sans-serif; }
    .kpi .n { font-size: 1.8rem; font-weight: bold; }
    .kpi .l { color: var(--muted); font-size: 12px; letter-spacing: .06em; text-transform: uppercase; }
    .kpi.alert .n { color: var(--danger); }
    .toolbar { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: 10px; font-family: Arial, sans-serif; font-size: 13px; }
    input, select, textarea, button { font: inherit; color: var(--ink); }
    input, select, textarea { padding: 7px 9px; border: 1px solid var(--line); border-radius: 10px; background: var(--bg); }
    textarea { width: 100%; min-height: 64px; resize: vertical; }
    button { padding: 8px 14px; border-radius: 999px; border: 1px solid var(--line); background: var(--surface-strong); cursor: pointer; }
    button.primary { background: var(--accent); color: var(--on-accent, var(--bg)); border-color: var(--accent); }
    button.danger { border-color: var(--danger); color: var(--danger); }
    .table-wrap { overflow: auto; max-height: 56vh; border: 1px solid var(--line); border-radius: 14px; }
    table { width: 100%; border-collapse: collapse; font-family: Arial, sans-serif; font-size: 13px; background: var(--panel); }
    th, td { border-bottom: 1px solid var(--line); padding: 7px 8px; text-align: left; vertical-align: top; }
    th { background: var(--surface-strong); position: sticky; top: 0; font-size: 12px; letter-spacing: .04em; text-transform: uppercase; color: var(--muted); }
    tbody tr { cursor: pointer; }
    tbody tr:hover, tbody tr.sel { background: var(--surface-strong); }
    .pill { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 11px; border: 1px solid var(--line); white-space: nowrap; }
    .sev-Critical { border-color: var(--danger); color: var(--danger); background: color-mix(in srgb, var(--danger) 18%, transparent); }
    .sev-High { border-color: var(--bad); color: var(--bad); }
    .sev-Medium { border-color: var(--accent); color: var(--accent); }
    .late { color: var(--danger); font-weight: bold; }
    .muted { color: var(--muted); }
    .panel { margin-top: 18px; background: var(--panel); border: 1px solid var(--line); border-radius: 18px; padding: 16px; }
    .panel[hidden] { display: none; }
    .panel-head { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 8px; }
    fieldset { border: 1px solid var(--line); border-radius: 14px; margin: 12px 0 0; padding: 10px 14px 14px; }
    legend { font-family: Arial, sans-serif; font-size: 12px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); padding: 0 6px; }
    .hint { font-family: Arial, sans-serif; font-size: 12px; color: var(--muted); margin: 2px 0 8px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; }
    .grid.two { grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); }
    label { display: grid; gap: 4px; font-family: Arial, sans-serif; font-size: 12px; color: var(--muted); }
    label.check { display: flex; align-items: center; gap: 6px; }
    label input:not([type=checkbox]), label select { width: 100%; }
    .actions td input, .actions td select, .actions td textarea { width: 100%; min-width: 0; font-size: 12px; }
    .actions td textarea { min-height: 38px; }
    .history { font-family: Arial, sans-serif; font-size: 12px; max-height: 240px; overflow: auto; }
    .history div { border-bottom: 1px solid var(--line); padding: 5px 0; }
    #status { font-family: Arial, sans-serif; font-size: 13px; min-height: 1.2em; }
    #status.err { color: var(--danger); }
    .readonly { color: var(--muted); font-family: Arial, sans-serif; font-size: 13px; padding: 7px 0; }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/audit-findings") + `
    <h1>Audit Findings &amp; Remediation</h1>
    <p class="lede">Findings from internal and external audit, regulators, certification audits and
    assessments. Each is recorded as criteria, condition, cause and effect, gets a management
    response and a dated action plan with named owners, and closes only on independently validated
    evidence or a formal, expiring risk acceptance. Reading is open; creating and editing require admin.</p>

    <div class="kpis" id="kpis"></div>

    <div class="toolbar">
      <input type="search" id="q" placeholder="Search reference, title, owner, control, risk" style="min-width:260px">
      <select id="fStatus"><option value="">All statuses</option></select>
      <select id="fSeverity"><option value="">All severities</option></select>
      <select id="fSource"><option value="">All sources</option></select>
      <label class="check"><input type="checkbox" id="fOpen" checked> Open only</label>
      <label class="check"><input type="checkbox" id="fOverdue"> Overdue only</label>
      <span style="flex:1"></span>
      <button class="primary" id="newBtn" type="button">New finding</button>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th>Ref</th><th>Title</th><th>Severity</th><th>Type</th><th>Status</th><th>Owner</th><th>Due</th><th>Age</th><th>Actions</th></tr></thead>
        <tbody id="rows"></tbody>
      </table>
    </div>

    <section class="panel" id="editor" hidden>
      <div class="panel-head">
        <h2 id="edTitle">New finding</h2>
        <div style="display:flex;gap:8px">
          <button class="danger" id="delBtn" type="button">Delete</button>
          <button id="closeBtn" type="button">Close</button>
          <button class="primary" id="saveBtn" type="button">Save</button>
        </div>
      </div>
      <div id="status"></div>

      <fieldset>
        <legend>Identification</legend>
        <div class="grid">
          <label>Reference<input data-f="reference" placeholder="auto: AF-YYYY-NNN"></label>
          <label>Status<select data-f="status" data-v="statuses"></select></label>
          <label>Severity<select data-f="severity" data-v="severities"></select></label>
          <label>Finding type<select data-f="finding_type" data-v="finding_types"></select></label>
          <label>Source<select data-f="source" data-v="sources" data-blank="1"></select></label>
          <label>Engagement / audit<input data-f="engagement" placeholder="e.g. 2026 ITGC review"></label>
          <label>Auditor / examiner<input data-f="auditor"></label>
          <label>Deficiency type<select data-f="deficiency_type" data-v="deficiency_types" data-blank="1"></select></label>
          <label>Identified<input type="date" data-f="identified_date"></label>
          <label>Report issued<input type="date" data-f="report_date"></label>
        </div>
        <label style="margin-top:10px">Title<input data-f="title" placeholder="Short statement of the problem"></label>
      </fieldset>

      <fieldset>
        <legend>The finding</legend>
        <p class="hint">Criteria is what should be; condition is what is; cause is why the gap exists;
        effect is the risk or consequence. The recommendation should address the cause.</p>
        <div class="grid two">
          <label>Criteria<textarea data-f="criteria" placeholder="Policy, regulation, standard or control requirement"></textarea></label>
          <label>Condition<textarea data-f="condition" placeholder="What the auditor observed, with the extent (sample, count)"></textarea></label>
          <label>Cause (root cause)<textarea data-f="cause"></textarea></label>
          <label>Effect<textarea data-f="effect" placeholder="Actual or potential impact"></textarea></label>
          <label>Recommendation<textarea data-f="recommendation"></textarea></label>
          <label>Root cause category<select data-f="root_cause_category" data-v="root_cause_categories" data-blank="1"></select></label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Ownership &amp; management response</legend>
        <div class="grid">
          <label>Accountable owner<input data-f="owner"></label>
          <label>Business unit<input data-f="business_unit"></label>
          <label>Process / area<input data-f="process_area"></label>
          <label>Management response<select data-f="management_response" data-v="management_responses" data-blank="1"></select></label>
        </div>
        <label style="margin-top:10px">Response detail<textarea data-f="management_response_text"></textarea></label>
      </fieldset>

      <fieldset>
        <legend>Remediation plan</legend>
        <div class="grid">
          <label>Due date<input type="date" data-f="due_date"></label>
          <label>Original due date<div class="readonly" id="origDue">—</div></label>
          <label>Extensions<div class="readonly" id="extCount">0</div></label>
          <label>Extension reason &amp; approver<input data-f="extension_reason" placeholder="Required when the due date moves later"></label>
        </div>
        <p class="hint" id="slaHint"></p>
        <div style="overflow-x:auto"><table class="actions" style="min-width:980px">
          <thead><tr><th style="width:26%">Action</th><th style="width:135px">Type</th><th style="width:150px">Owner</th><th style="width:140px">Due</th><th style="width:145px">Status</th><th style="width:140px">Completed</th><th>Evidence</th><th style="width:44px"></th></tr></thead>
          <tbody id="actionRows"></tbody>
        </table></div>
        <button type="button" id="addAction" style="margin-top:8px">Add action</button>
      </fieldset>

      <fieldset>
        <legend>Links &amp; recurrence</legend>
        <div class="grid">
          <label>Linked risks<input data-f="linked_risks" placeholder="Risk register IDs, comma separated"></label>
          <label>Linked controls<input data-f="linked_controls" placeholder="e.g. AC-2, AC-6(7)"></label>
          <label>Framework / regulation refs<input data-f="framework_refs" placeholder="e.g. ISO 27001 A.5.18, DORA Art. 9"></label>
          <label class="check"><input type="checkbox" data-f="repeat_finding"> Repeat finding</label>
          <label>Prior finding reference<input data-f="prior_reference"></label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Validation &amp; closure</legend>
        <p class="hint">Closing needs a validator independent of the owner, a validation date and the
        evidence that the fix works in design and in operation.</p>
        <div class="grid">
          <label>Validated by<input data-f="validated_by"></label>
          <label>Validation date<input type="date" data-f="validation_date"></label>
          <label>Closed<div class="readonly" id="closedDate">—</div></label>
        </div>
        <div class="grid two" style="margin-top:10px">
          <label>Closure evidence<textarea data-f="closure_evidence" placeholder="What was tested and where the evidence is kept"></textarea></label>
          <label>Validation notes<textarea data-f="validation_notes"></textarea></label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Risk acceptance</legend>
        <div class="grid">
          <label>Accepted by<input data-f="risk_accepted_by" placeholder="Name and role with the authority"></label>
          <label>Expires<input type="date" data-f="risk_acceptance_expiry"></label>
        </div>
        <label style="margin-top:10px">Rationale &amp; compensating controls<textarea data-f="risk_acceptance_rationale"></textarea></label>
      </fieldset>

      <fieldset>
        <legend>Notes &amp; history</legend>
        <label>Notes<textarea data-f="notes"></textarea></label>
        <div class="history" id="history"></div>
      </fieldset>
    </section>
  </main>
  <script>
    var vocab = null, current = null, rows = [];
    var $ = function (id) { return document.getElementById(id); };
    function esc(v) { return (v == null ? "" : String(v)).replace(/[&<>"']/g, function (m) { return {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[m]; }); }
    function opts(list, blank) { return (blank ? '<option value=""></option>' : "") + list.map(function (v) { return '<option>' + esc(v) + '</option>'; }).join(""); }
    function setStatus(msg, err) { var s = $("status"); s.textContent = msg || ""; s.className = err ? "err" : ""; }

    async function init() {
      vocab = await (await fetch("/audit-findings/vocabulary")).json();
      $("fStatus").innerHTML += opts(vocab.statuses);
      $("fSeverity").innerHTML += opts(vocab.severities);
      $("fSource").innerHTML += opts(vocab.sources);
      document.querySelectorAll("select[data-v]").forEach(function (sel) { sel.innerHTML = opts(vocab[sel.dataset.v], sel.dataset.blank); });
      $("slaHint").textContent = "Left blank, the due date defaults from severity: " +
        vocab.severities.map(function (s) { return s + " " + vocab.severity_sla_days[s] + " days"; }).join(", ") + ".";
      ["q","fStatus","fSeverity","fSource","fOpen","fOverdue"].forEach(function (id) { $(id).addEventListener(id === "q" ? "input" : "change", load); });
      $("newBtn").onclick = function () { edit(null); };
      $("closeBtn").onclick = function () { $("editor").hidden = true; current = null; markSelected(); };
      $("saveBtn").onclick = save;
      $("delBtn").onclick = remove;
      $("addAction").onclick = function () { addActionRow({}); };
      await load();
      var m = location.hash.match(/^#(\d+)$/);
      if (m) edit(Number(m[1]));
    }

    async function loadSummary() {
      var s = await (await fetch("/audit-findings/summary")).json();
      var tiles = [
        ["Open", s.open], ["Overdue", s.overdue, s.overdue > 0], ["Pending validation", s.pending_validation],
        ["Open critical / high", (s.open_by_severity.Critical || 0) + (s.open_by_severity.High || 0)],
        ["Repeat findings open", s.repeat_open, s.repeat_open > 0], ["Extended", s.extended],
        ["Risk accepted", s.risk_accepted + (s.acceptance_expired ? " (" + s.acceptance_expired + " expired)" : ""), s.acceptance_expired > 0],
        ["Closed, last 90 days", s.closed_last_90_days], ["Avg days to close", s.avg_days_to_close]
      ];
      $("kpis").innerHTML = tiles.map(function (t) { return '<div class="kpi' + (t[2] ? " alert" : "") + '"><div class="n">' + esc(t[1]) + '</div><div class="l">' + esc(t[0]) + '</div></div>'; }).join("");
    }

    async function load() {
      var q = new URLSearchParams();
      if ($("q").value) q.set("search", $("q").value);
      if ($("fStatus").value) q.set("status", $("fStatus").value);
      if ($("fSeverity").value) q.set("severity", $("fSeverity").value);
      if ($("fSource").value) q.set("source", $("fSource").value);
      if ($("fOpen").checked) q.set("open", "true");
      if ($("fOverdue").checked) q.set("overdue", "true");
      var res = await fetch("/audit-findings/data?" + q.toString());
      rows = res.ok ? await res.json() : [];
      $("rows").innerHTML = rows.length ? rows.map(function (f) {
        var live = f.actions.filter(function (a) { return a.status !== "Cancelled"; });
        var done = live.filter(function (a) { return a.status === "Implemented" || a.status === "Validated"; }).length;
        var due = f.due_date ? esc(f.due_date) + (f.overdue ? ' <span class="late">+' + f.days_overdue + 'd</span>' : "") : '<span class="muted">—</span>';
        return '<tr data-id="' + f.id + '"><td>' + esc(f.reference) + (f.repeat_finding ? ' <span class="pill" title="Repeat finding">R</span>' : "") +
          '</td><td>' + esc(f.title) + '</td><td><span class="pill sev-' + esc(f.severity) + '">' + esc(f.severity) +
          '</span></td><td>' + esc(f.finding_type) + '</td><td>' + esc(f.status) + '</td><td>' + esc(f.owner) +
          '</td><td>' + due + '</td><td>' + f.days_open + 'd</td><td>' + (live.length ? done + "/" + live.length : '<span class="muted">none</span>') + '</td></tr>';
      }).join("") : '<tr><td colspan="9" class="muted">No findings match.</td></tr>';
      document.querySelectorAll("#rows tr[data-id]").forEach(function (tr) { tr.onclick = function () { edit(Number(tr.dataset.id)); }; });
      markSelected();
      loadSummary();
    }

    function markSelected() {
      document.querySelectorAll("#rows tr").forEach(function (tr) { tr.classList.toggle("sel", !!current && Number(tr.dataset.id) === current.id); });
    }

    async function edit(id) {
      setStatus("");
      if (id) {
        var res = await fetch("/audit-findings/data/" + id);
        if (!res.ok) { setStatus("Could not load finding", true); return; }
        current = await res.json();
      } else {
        current = { id: 0, status: "Open", severity: "Medium", finding_type: vocab.finding_types[0], actions: [], history: [] };
      }
      fill(current);
      $("editor").hidden = false;
      $("delBtn").hidden = !current.id;
      history.replaceState(null, "", current.id ? "#" + current.id : location.pathname);
      markSelected();
      $("editor").scrollIntoView({ behavior: "smooth", block: "start" });
    }

    function fill(f) {
      $("edTitle").textContent = f.id ? f.reference + " · " + f.title : "New finding";
      document.querySelectorAll("#editor [data-f]").forEach(function (el) {
        var v = f[el.dataset.f];
        if (el.type === "checkbox") el.checked = !!v; else el.value = v == null ? "" : v;
      });
      $("origDue").textContent = f.original_due_date || "—";
      $("extCount").textContent = f.extension_count || 0;
      $("closedDate").textContent = f.closed_date || "—";
      $("actionRows").innerHTML = "";
      (f.actions || []).forEach(addActionRow);
      $("history").innerHTML = (f.history || []).map(function (h) {
        return '<div><span class="muted">' + esc(h.at.replace("T", " ").replace("Z", "")) + " · " + esc(h.actor || "local") + "</span> — <b>" + esc(h.event) + "</b>" +
          (h.from_value || h.to_value ? ": " + esc(h.from_value) + " → " + esc(h.to_value) : "") + (h.note ? ' <span class="muted">(' + esc(h.note) + ")</span>" : "") + "</div>";
      }).join("") || '<div class="muted">No history yet.</div>';
    }

    function addActionRow(a) {
      var tr = document.createElement("tr");
      tr.innerHTML = '<td><textarea data-a="description">' + esc(a.description) + '</textarea></td>' +
        '<td><select data-a="action_type">' + opts(vocab.action_types) + '</select></td>' +
        '<td><input data-a="owner" value="' + esc(a.owner) + '"></td>' +
        '<td><input type="date" data-a="due_date" value="' + esc(a.due_date) + '"' + (a.overdue ? ' style="border-color:var(--danger)" title="Overdue"' : "") + '></td>' +
        '<td><select data-a="status">' + opts(vocab.action_statuses) + '</select></td>' +
        '<td><input type="date" data-a="completed_date" value="' + esc(a.completed_date) + '"></td>' +
        '<td><textarea data-a="evidence">' + esc(a.evidence) + '</textarea></td>' +
        '<td><button type="button" title="Remove">✕</button></td>';
      tr.querySelector('[data-a="action_type"]').value = a.action_type || vocab.action_types[0];
      tr.querySelector('[data-a="status"]').value = a.status || vocab.action_statuses[0];
      tr.dataset.reference = a.reference || "";
      tr.querySelector("button").onclick = function () { tr.remove(); };
      $("actionRows").appendChild(tr);
    }

    function collect() {
      var f = {};
      document.querySelectorAll("#editor [data-f]").forEach(function (el) { f[el.dataset.f] = el.type === "checkbox" ? el.checked : el.value; });
      f.actions = Array.prototype.map.call(document.querySelectorAll("#actionRows tr"), function (tr) {
        var a = { reference: tr.dataset.reference };
        tr.querySelectorAll("[data-a]").forEach(function (el) { a[el.dataset.a] = el.value; });
        return a;
      });
      return f;
    }

    async function save() {
      setStatus("Saving…");
      var isNew = !current.id;
      var res = await fetch(isNew ? "/audit-findings" : "/audit-findings/" + current.id, {
        method: isNew ? "POST" : "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(collect())
      });
      var body = await res.json().catch(function () { return {}; });
      if (!res.ok) { setStatus(body.error || ("Save failed (" + res.status + ")"), true); return; }
      current = body;
      fill(current);
      $("delBtn").hidden = false;
      history.replaceState(null, "", "#" + current.id);
      setStatus("Saved " + current.reference + ".");
      load();
    }

    async function remove() {
      if (!current || !current.id || !confirm("Delete " + current.reference + " and its action plan and history? Closed findings are usually kept as evidence.")) return;
      var res = await fetch("/audit-findings/" + current.id, { method: "DELETE" });
      if (!res.ok) { var b = await res.json().catch(function () { return {}; }); setStatus(b.error || "Delete failed", true); return; }
      $("editor").hidden = true; current = null; history.replaceState(null, "", location.pathname); load();
    }

    init();
  </script>
</body>
</html>`
}
