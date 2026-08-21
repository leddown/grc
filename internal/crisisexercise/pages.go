package crisisexercise

import (
	"encoding/json"
	"strings"

	"grc/internal/pageui"
)

// The two pages share one stylesheet.
//
// The exercise page renders from a JSON payload embedded in the document rather
// than from server-built markup. That is a departure from the older pages in
// this application and it is deliberate: an exercise is one deeply nested
// object that the reader moves around inside — phase to inject to response to
// citation — and building that as Go string concatenation produces a page that
// is painful to change and a module that is half template engine. The PDF is
// still server-rendered (see render.go), so the document that gets circulated
// never depends on a browser having run anything.

const pageStyles = `
  :root {
    color-scheme: light;
    --panel: rgba(255,252,246,0.94);
    --ink: #1c2431;
    --muted: #5e6672;
    --line: #d7cebf;
    --accent: #8b3d2e;
    --good: #0b5d3b;
    --warn: #9a6700;
    --bad: #8b3d2e;
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
    max-width: 1500px; margin: 24px auto; padding: 24px;
    background: var(--panel); border: 1px solid rgba(215,206,191,0.8);
    border-radius: 24px; box-shadow: 0 24px 60px rgba(28,36,49,0.12);
  }
  h1 { margin: 0 0 8px; font-size: clamp(1.7rem, 3.2vw, 2.6rem); line-height: 1.05; letter-spacing: -0.02em; }
  h2 { font-size: 1.05rem; margin: 22px 0 8px; padding-bottom: 4px; border-bottom: 1px solid var(--line); }
  h3 { font-size: 0.98rem; margin: 16px 0 4px; }
  h4 { font-size: 0.9rem; margin: 10px 0 2px; }
  p { margin: 0 0 8px; }
  a { color: var(--accent); }
  .lede { color: var(--muted); max-width: 82ch; }
  .toolbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 14px 0; }
  .card { border: 1px solid var(--line); border-radius: 16px; background: rgba(255,255,255,0.86);
          padding: 16px; margin-bottom: 16px; }
  label { display: block; font-family: Arial, sans-serif; font-size: 11px; letter-spacing: 0.07em;
          text-transform: uppercase; color: var(--muted); font-weight: 700; margin-bottom: 5px; }
  input, select, textarea {
    width: 100%; padding: 9px 11px; border-radius: 10px; border: 1px solid var(--line);
    background: white; color: var(--ink); font: inherit; font-size: 14px;
  }
  textarea { min-height: 76px; resize: vertical; font-family: inherit; }
  button { padding: 9px 14px; border-radius: 999px; border: 0; background: #dfc4b4;
           color: var(--ink); font: inherit; font-size: 14px; cursor: pointer; }
  button.secondary { background: white; border: 1px solid var(--line); }
  button.small { padding: 4px 11px; font-family: Arial, sans-serif; font-size: 11px;
                 text-transform: uppercase; letter-spacing: 0.06em; background: white;
                 border: 1px solid var(--line); }
  button[disabled] { opacity: 0.55; cursor: not-allowed; }
  .grid { display: grid; gap: 14px; }
  .fields { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); }
  .fields.wide { grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); }
  table.list { width: 100%; border-collapse: collapse; }
  table.list th, table.list td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--line); vertical-align: top; }
  table.list th { font-family: Arial, sans-serif; font-size: 11px; text-transform: uppercase;
                  letter-spacing: 0.06em; color: var(--muted); }
  .chip { display: inline-block; padding: 3px 10px; border-radius: 999px; border: 1px solid var(--line);
          font-family: Arial, sans-serif; font-size: 11px; text-transform: uppercase;
          letter-spacing: 0.05em; text-decoration: none; color: var(--ink); }
  .chip.on { border-color: #7fae94; color: var(--good); }
  .chip.off { border-color: #d7b271; color: var(--warn); }
  .chip.bad { border-color: #c39184; color: var(--bad); }
  .marking { font-family: Arial, sans-serif; font-size: 11px; font-weight: 700; letter-spacing: 0.09em;
             border: 1.5px solid var(--accent); color: var(--accent); padding: 3px 9px;
             border-radius: 4px; display: inline-block; }
  .status { font-family: Arial, sans-serif; font-size: 13px; color: var(--muted); min-height: 20px; }
  .status.warn { color: var(--warn); }
  .status.bad { color: var(--bad); }
  .meta { font-family: Arial, sans-serif; font-size: 11.5px; color: var(--muted); }
  .quiet { color: var(--muted); }
  .met { color: var(--good); font-weight: 700; }
  .missed { color: var(--bad); font-weight: 700; }
  .warnv { color: var(--warn); font-weight: 700; }
  .stats { display: flex; flex-wrap: wrap; gap: 10px; margin: 12px 0; }
  .stat { border: 1px solid var(--line); border-radius: 12px; padding: 9px 14px; min-width: 116px;
          background: rgba(255,255,255,0.7); }
  .stat .n { font-size: 1.4rem; font-weight: 700; display: block; line-height: 1.1; }
  .stat .l { font-family: Arial, sans-serif; font-size: 10px; text-transform: uppercase;
             letter-spacing: 0.06em; color: var(--muted); }
  .subtabs { display: flex; flex-wrap: wrap; gap: 6px; margin: 16px 0 12px;
             border-bottom: 1px solid var(--line); padding-bottom: 8px; }
  .subtab { padding: 6px 14px; border-radius: 999px; border: 1px solid transparent;
            font-family: Arial, sans-serif; font-size: 12px; letter-spacing: 0.04em;
            cursor: pointer; background: transparent; color: var(--muted); }
  .subtab.active { background: white; border-color: var(--line); color: var(--ink); font-weight: 700; }
  .panel { display: none; }
  .panel.active { display: block; }
  .phase { border: 1px solid var(--line); border-radius: 14px; background: rgba(255,255,255,0.75);
           padding: 14px 16px; margin-bottom: 14px; }
  .phase > header { display: flex; flex-wrap: wrap; gap: 10px; align-items: baseline;
                    justify-content: space-between; }
  .inject { border-left: 3px solid var(--line); padding: 4px 0 4px 12px; margin: 12px 0; }
  .inject.stress { border-left-color: #b8860b; }
  .inject.ambiguous { border-left-color: #6b7fa8; }
  .inject.decision { border-left-color: var(--accent); }
  .inject .head { font-family: Arial, sans-serif; font-size: 11px; text-transform: uppercase;
                  letter-spacing: 0.05em; color: var(--muted); }
  .inject .body { white-space: pre-wrap; margin: 5px 0; padding: 8px 10px; background: #fdfaf5;
                  border: 1px solid var(--line); border-radius: 8px; font-size: 14px; }
  .refs { font-family: Arial, sans-serif; font-size: 11px; color: var(--muted); margin-top: 5px; }
  .refs a { text-decoration: none; }
  .refs .unknown { color: var(--bad); }
  .row { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
  .chatlog { display: flex; flex-direction: column; gap: 10px; max-height: 60vh; overflow: auto;
             padding-right: 4px; margin-bottom: 12px; }
  .msg { border: 1px solid var(--line); border-radius: 12px; padding: 9px 13px; white-space: pre-wrap;
         word-break: break-word; font-size: 14px; }
  .msg.user { align-self: flex-end; border-left: 4px solid var(--accent); background: #fdf8f2; max-width: 78%; }
  .msg.ai { align-self: flex-start; border-left: 4px solid var(--good); background: white; max-width: 88%; }
  .msg .who { display: block; font-family: Arial, sans-serif; font-size: 10px; text-transform: uppercase;
              letter-spacing: 0.07em; color: var(--muted); margin-bottom: 4px; }
  details { margin: 6px 0; }
  summary { cursor: pointer; font-family: Arial, sans-serif; font-size: 12px; color: var(--muted);
            letter-spacing: 0.04em; }
  .hidden { display: none; }
  @media (max-width: 900px) { main { margin: 8px; padding: 16px; border-radius: 16px; } }
`

// ---- index page ----

func indexPageHTML(exercises []Exercise, configured bool, model string) string {
	aiChip := `<span class="chip off">AI: not configured</span>`
	if configured {
		aiChip = `<span class="chip on">AI: ` + esc(model) + `</span>`
	}

	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Risk &amp; Crisis Exercises · GRC</title>
  <style>` + pageStyles + `</style>
</head>
<body>
  <main>
    ` + pageui.Nav("/crisis-exercises") + `
    <h1>Risk &amp; Crisis Exercises</h1>
    <p class="lede">Plan, run and report the exercises that prove this institution can survive a bad
    day — from a red-team detonation, through incident response, the classification that starts the
    regulatory clock, crisis and continuity activation, communications, the board, and the
    supervisor. Every objective, phase, inject and finding can cite the controls, requirements,
    regulation clauses and frameworks it exercises, so the report answers <em>what did we
    test</em> and not only <em>what did we discuss</em>.</p>
    <div class="toolbar">` + aiChip + `
      <a class="chip" href="/controls">800-53 controls</a>
      <a class="chip" href="/security-nfrs">Security NFRs</a>
      <a class="chip" href="/risk-register">Risk register</a>
      <a class="chip" href="/regulation-coverage">Regulation coverage</a>
      <a class="chip" href="/settings">Settings</a>
    </div>

    <section class="card">
      <h2>Start from a scenario</h2>
      <p class="status">Each of these is drawn from something that has happened to a European
      financial institution or to Baltic critical infrastructure. Picking one fills in the
      intelligence, the objectives and the citations; everything stays editable.</p>
      <div id="scenarios" class="grid"><p class="status">Loading…</p></div>
    </section>

    <section class="card">
      <h2>New exercise</h2>
      <form id="createForm" class="grid">
        <div class="fields">
          <div><label for="title">Title</label>
            <input id="title" required placeholder="Annual crisis simulation — ransomware in core banking"></div>
          <div><label for="scenario">Scenario</label><select id="scenario"></select></div>
          <div><label for="format">Format</label><select id="format"></select></div>
          <div><label for="audience">Audience</label><select id="audience"></select></div>
          <div><label for="entityName">Entity</label><input id="entityName" placeholder="Legal entity being exercised"></div>
          <div><label for="entityType">Entity type</label><select id="entityType"></select></div>
          <div><label for="jurisdiction">Jurisdiction</label><select id="jurisdiction"></select></div>
          <div><label for="supervision">Supervision</label>
            <select id="supervision">
              <option value="national">National competent authority (less significant institution)</option>
              <option value="significant">Significant institution — directly ECB supervised</option>
              <option value="none">Not prudentially supervised</option>
            </select></div>
          <div><label for="duration">Duration (minutes)</label><input id="duration" type="number" min="30" step="15" value="240"></div>
          <div><label for="scheduled">Scheduled for</label><input id="scheduled" type="date"></div>
          <div><label for="facilitator">Facilitator</label><input id="facilitator"></div>
          <div><label for="tlp">Marking</label>
            <select id="tlp">
              <option>TLP:AMBER</option><option>TLP:AMBER+STRICT</option>
              <option>TLP:RED</option><option>TLP:GREEN</option><option>TLP:CLEAR</option>
            </select></div>
        </div>
        <div><label for="criticalFuncs">Critical or important functions in scope (one per line)</label>
          <textarea id="criticalFuncs" placeholder="Core banking ledger&#10;Card authorisation&#10;SEPA payment initiation"></textarea></div>
        <div class="toolbar">
          <button type="submit">Create exercise</button>
          <span id="createStatus" class="status"></span>
        </div>
      </form>
      <p class="status">Creating an exercise lays out the phase arc and seeds its citations. Nothing
      is sent to a model until you ask for the master scenario events list to be generated.</p>
    </section>

    <section class="card">
      <h2>Exercises</h2>
      <table class="list">
        <thead><tr><th>Reference</th><th>Exercise</th><th>Format</th><th>Entity</th>
          <th>Scheduled</th><th>Injects</th><th>Findings</th><th>Status</th></tr></thead>
        <tbody id="rows">` + indexRowsHTML(exercises) + `</tbody>
      </table>
    </section>
  </main>
<script>
(() => {
  "use strict";
  const esc = v => (v ?? "").toString().replace(/[&<>"']/g, m =>
    ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]));
  const $ = id => document.getElementById(id);
  let catalog = null;

  function fillSelect(el, items, valueKey, labelKey, blank) {
    el.innerHTML = (blank ? '<option value="">' + esc(blank) + '</option>' : '') +
      items.map(i => '<option value="' + esc(i[valueKey]) + '">' + esc(i[labelKey]) + '</option>').join('');
  }

  async function loadCatalog() {
    const res = await fetch('/crisis-exercises/catalog');
    if (!res.ok) { $('createStatus').textContent = 'Could not load the catalog.'; return; }
    catalog = await res.json();

    fillSelect($('scenario'), catalog.scenarios, 'key', 'name', 'Bespoke — write my own');
    fillSelect($('format'), catalog.formats, 'key', 'label');
    fillSelect($('audience'), catalog.audiences, 'key', 'label');
    fillSelect($('entityType'), catalog.entity_types, 'key', 'label');
    fillSelect($('jurisdiction'), catalog.jurisdictions, 'key', 'label');
    $('format').value = 'tabletop';

    $('scenarios').innerHTML = catalog.scenarios.map(s => ` + "`" + `
      <div class="card" style="margin:0 0 10px">
        <h3>${esc(s.name)}</h3>
        <p>${esc(s.summary)}</p>
        <p class="meta"><strong>Adversary.</strong> ${esc(s.threat_actor)}</p>
        <details><summary>What this scenario is really designed to expose</summary>
          <p class="quiet">${esc(s.trap_door)}</p></details>
        <div class="toolbar"><button type="button" class="small" data-pick="${esc(s.key)}">Use this scenario</button></div>
      </div>` + "`" + `).join('');

    document.querySelectorAll('[data-pick]').forEach(btn => btn.addEventListener('click', () => {
      const key = btn.getAttribute('data-pick');
      const s = catalog.scenarios.find(x => x.key === key);
      if (!s) return;
      $('scenario').value = key;
      if (!$('title').value) $('title').value = s.name;
      $('criticalFuncs').value = s.critical_functions || '';
      if (s.suggested_format) $('format').value = s.suggested_format;
      if (s.suggested_audience) $('audience').value = s.suggested_audience;
      const fmt = catalog.formats.find(f => f.key === $('format').value);
      if (fmt) $('duration').value = fmt.default_minutes;
      $('title').scrollIntoView({behavior:'smooth', block:'center'});
    }));

    $('format').addEventListener('change', () => {
      const fmt = catalog.formats.find(f => f.key === $('format').value);
      if (fmt) $('duration').value = fmt.default_minutes;
    });
  }

  $('createForm').addEventListener('submit', async ev => {
    ev.preventDefault();
    const status = $('createStatus');
    status.textContent = 'Creating…';
    const res = await fetch('/crisis-exercises/exercises', {
      method: 'POST', headers: {'Content-Type':'application/json'},
      body: JSON.stringify({
        title: $('title').value, scenario_key: $('scenario').value,
        format: $('format').value, audience: $('audience').value,
        entity_name: $('entityName').value, entity_type: $('entityType').value,
        jurisdiction: $('jurisdiction').value, supervision: $('supervision').value,
        duration_minutes: parseInt($('duration').value, 10) || 0,
        scheduled_for: $('scheduled').value, facilitator: $('facilitator').value,
        tlp: $('tlp').value, critical_functions: $('criticalFuncs').value
      })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) { status.textContent = data.error || 'Could not create the exercise.'; return; }
    window.location.href = '/crisis-exercises/' + data.id;
  });

  loadCatalog();
})();
</script>
</body>
</html>`
}

func indexRowsHTML(exercises []Exercise) string {
	if len(exercises) == 0 {
		return `<tr><td colspan="8" class="status">No exercises yet. Pick a scenario above, or write your own.</td></tr>`
	}
	var b strings.Builder
	for _, ex := range exercises {
		format, _ := FormatByKey(ex.Format)
		b.WriteString(`<tr>`)
		b.WriteString(`<td><a href="/crisis-exercises/` + itoa(ex.ID) + `">` + esc(ex.Reference) + `</a></td>`)
		b.WriteString(`<td>` + esc(ex.Title))
		if ex.Summary != "" {
			b.WriteString(`<br><span class="meta">` + esc(truncate(ex.Summary, 140)) + `</span>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td>` + esc(format.Label) + `</td>`)
		b.WriteString(`<td>` + esc(orDash(ex.EntityName)) + `<br><span class="meta">` +
			esc(JurisdictionByKey(ex.Jurisdiction).Label) + `</span></td>`)
		b.WriteString(`<td>` + esc(orDash(ex.ScheduledFor)) + `</td>`)
		b.WriteString(`<td>` + itoa(int64(ex.InjectCount)) + `</td>`)
		b.WriteString(`<td>` + itoa(int64(ex.FindingCount)) + `</td>`)
		b.WriteString(`<td><span class="chip">` + esc(ex.Status) + `</span></td>`)
		b.WriteString(`</tr>`)
	}
	return b.String()
}

// ---- exercise page ----

type exercisePageData struct {
	Dossier      Dossier
	Versions     []Version
	Chat         []ChatTurn
	AIConfigured bool
	Model        string
	PDFAvailable bool
}

// payload is what the page renders from. It is assembled here rather than
// reusing Dossier directly so that the seeded catalogs the page needs — the
// personas, the phase arc, the reference kinds — travel with it and the page
// does not have to make a second request before it can show anything.
type payload struct {
	Dossier      Dossier       `json:"dossier"`
	Versions     []Version     `json:"versions"`
	Chat         []ChatTurn    `json:"chat"`
	Personas     []Persona     `json:"personas"`
	Roles        []RoleDef     `json:"roles"`
	RefKinds     []string      `json:"ref_kinds"`
	Coverage     []CoverageRow `json:"coverage"`
	AIConfigured bool          `json:"ai_configured"`
	Model        string        `json:"model"`
	PDFAvailable bool          `json:"pdf_available"`
	Scenario     *Scenario     `json:"scenario,omitempty"`
}

func exercisePageHTML(data exercisePageData) string {
	p := payload{
		Dossier:      data.Dossier,
		Versions:     data.Versions,
		Chat:         data.Chat,
		Personas:     Personas(),
		Roles:        Roles(),
		RefKinds:     ReferenceKinds(),
		Coverage:     Coverage(data.Dossier.References),
		AIConfigured: data.AIConfigured,
		Model:        data.Model,
		PDFAvailable: data.PDFAvailable,
	}
	if s, ok := ScenarioByKey(data.Dossier.Exercise.ScenarioKey); ok {
		p.Scenario = &s
	}

	encoded, err := json.Marshal(p)
	if err != nil {
		encoded = []byte(`{"error":"could not encode the exercise"}`)
	}

	ex := data.Dossier.Exercise
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + esc(ex.Reference+" "+ex.Title) + ` · GRC</title>
  <style>` + pageStyles + `</style>
</head>
<body>
  <main>
    ` + pageui.Nav("/crisis-exercises") + `
    <div class="toolbar">
      <span class="marking">` + esc(ex.TLP) + `</span>
      <a class="chip" href="/crisis-exercises">All exercises</a>
      <span class="chip">` + esc(ex.Reference) + `</span>
      <span class="chip" id="statusChip">` + esc(ex.Status) + `</span>
    </div>
    <h1>` + esc(ex.Title) + `</h1>
    <p class="lede" id="summaryLine"></p>
    <div class="toolbar" id="docLinks"></div>
    <div class="stats" id="stats"></div>

    <div class="subtabs" id="subtabs"></div>
    <div id="panels"></div>
  </main>
<script id="payload" type="application/json">` + jsonScriptSafe(string(encoded)) + `</script>
<script>` + exercisePageScript + `</script>
</body>
</html>`
}

// jsonScriptSafe escapes the characters that could end the <script> block
// early, so an inject body containing "</script>" is data rather than markup.
//
// encoding/json already escapes these three by default, which makes this
// belt-and-braces. It is here anyway because that default is a property of
// json.Marshal rather than of JSON, and a future change to an Encoder with
// SetEscapeHTML(false) would silently turn a user-supplied inject body into an
// injection point. The guard costs one pass over a string; the alternative
// costs a cross-site scripting bug in a page that renders text people paste in
// from incident reports.
func jsonScriptSafe(s string) string {
	s = strings.ReplaceAll(s, "<", `\u003c`)
	s = strings.ReplaceAll(s, ">", `\u003e`)
	s = strings.ReplaceAll(s, "&", `\u0026`)
	return s
}
