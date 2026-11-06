package assuranceplan

import (
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"

	"grc/internal/pageui"
)

var esc = html.EscapeString

// pageStyles is shared by both pages. Status pills carry explicit colour
// pairs (text and fill) rather than the theme's .pill, and every table has an
// explicit <thead>: the theme layer reaches thead th, and a header cell left in
// an implicit tbody kept its light fill under a dark palette.
const pageStyles = `
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin: 0; font-family: system-ui, -apple-system, "Segoe UI", Arial, sans-serif; font-size: 13px;
         color: var(--text, #e6e8ec); background: var(--bg, #0f1115); }
  main { max-width: 1560px; margin: 0 auto; padding: 14px 18px 40px; }
  h1 { font-size: 20px; margin: 4px 0; }
  h2 { font-size: 14px; margin: 14px 0 6px; }
  h3 { font-size: 13px; margin: 10px 0 4px; }
  a { color: var(--accent, #6ea8fe); }
  .lede { color: var(--muted, #8b93a3); max-width: 100ch; margin: 0 0 10px; }
  .muted { color: var(--muted, #8b93a3); }
  .toolbar { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; margin: 8px 0; }
  .spacer { flex: 1; }
  input, select, textarea, button { font: inherit; font-size: 13px; color: var(--text, #e6e8ec); }
  input, select, textarea { background: var(--surface, #171a21); border: 1px solid var(--border, #2a2f3a);
         border-radius: 6px; padding: 2px 6px; min-height: 26px; }
  textarea { width: 100%; min-height: 52px; resize: vertical; }
  button { padding: 2px 10px; min-height: 26px; border-radius: 6px; border: 1px solid var(--border, #2a2f3a);
         background: var(--surface-2, #1f232c); cursor: pointer; }
  button.primary { background: var(--accent, #6ea8fe); color: var(--on-accent, #0b0d12); border-color: var(--accent, #6ea8fe); }
  button.danger { color: var(--error, #ff8a8a); border-color: var(--error, #ff8a8a); }
  button.mini { padding: 0 6px; min-height: 20px; font-size: 11px; }
  table { border-collapse: collapse; width: 100%; }
  th, td { text-align: left; padding: 3px 6px; border-bottom: 1px solid var(--border, #2a2f3a); vertical-align: top; }
  thead th { background: var(--surface-2, #1f232c); color: var(--muted, #8b93a3); font-size: 11px;
         text-transform: uppercase; letter-spacing: .06em; position: sticky; top: 0; z-index: 1; }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  .table-wrap { overflow: auto; border: 1px solid var(--border, #2a2f3a); border-radius: 8px; }
  .ap-tabs { display: flex; flex-wrap: wrap; gap: 2px; border-bottom: 1px solid var(--border, #2a2f3a); margin: 12px 0 10px; }
  .ap-tabs button { border-radius: 6px 6px 0 0; border-bottom: 0; background: transparent; color: var(--muted, #8b93a3); }
  .ap-tabs button[aria-selected="true"] { background: var(--surface-2, #1f232c); color: var(--text, #e6e8ec); font-weight: 600; }
  section.tab-panel[hidden] { display: none; }
  .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 8px; margin: 8px 0; }
  .card { background: var(--surface, #171a21); border: 1px solid var(--border, #2a2f3a); border-radius: 8px; padding: 6px 10px; }
  .card .n { font-size: 18px; font-weight: 600; }
  .card .l { font-size: 11px; color: var(--muted, #8b93a3); text-transform: uppercase; letter-spacing: .06em; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 6px 12px; }
  .grid.wide { grid-template-columns: repeat(auto-fit, minmax(420px, 1fr)); }
  label { display: grid; gap: 2px; font-size: 11px; color: var(--muted, #8b93a3); }
  label input, label select, label textarea { width: 100%; }
  fieldset { border: 1px solid var(--border, #2a2f3a); border-radius: 8px; margin: 10px 0 0; padding: 6px 10px 10px; }
  legend { font-size: 11px; color: var(--muted, #8b93a3); text-transform: uppercase; letter-spacing: .07em; padding: 0 4px; }
  .status-line { min-height: 1.3em; margin: 4px 0; }
  .status-line.err { color: var(--error, #ff8a8a); }
  .notice { border-left: 3px solid var(--accent, #6ea8fe); padding: 4px 10px; background: var(--surface, #171a21); margin: 6px 0; }
  .notice.warn { border-left-color: #f5b83d; }
  .kv { display: grid; grid-template-columns: max-content 1fr; gap: 3px 14px; }
  .kv dt { color: var(--muted, #8b93a3); }
  .kv dd { margin: 0; }
  ul.lint { margin: 4px 0; padding-left: 18px; }
  ul.lint li { margin: 2px 0; }
  .edit-table td input, .edit-table td select, .edit-table td textarea { width: 100%; min-width: 56px; font-size: 12px; }
  .edit-table td textarea { min-height: 26px; }
  .edit-table td.ctl { white-space: nowrap; width: 1%; }
  .pre { white-space: pre-wrap; }

  .ap-st { display: inline-block; padding: 0 7px; border-radius: 999px; font-size: 11px; line-height: 18px;
         border: 1px solid; white-space: nowrap; font-weight: 600; }
  .ap-st-draft, .ap-st-not-started, .ap-st-closed, .ap-st-none, .ap-st-advisory {
         color: var(--muted, #8b93a3); background: var(--surface-2, #1f232c); border-color: var(--border, #2a2f3a); }
  .ap-st-in-review, .ap-st-in-progress {
         color: var(--accent, #6ea8fe); background: var(--surface, #171a21);
         background: color-mix(in srgb, var(--accent, #6ea8fe) 14%, var(--surface, #171a21)); border-color: var(--accent, #6ea8fe); }
  .ap-st-approved, .ap-st-active, .ap-st-complete {
         color: var(--gain, #7ddc9a); background: var(--surface, #171a21);
         background: color-mix(in srgb, var(--gain, #7ddc9a) 14%, var(--surface, #171a21)); border-color: var(--gain, #7ddc9a); }
  .ap-st-at-risk {
         color: #f5b83d; background: #2a2210; border-color: #f5b83d; }
  .ap-st-delayed, .ap-st-blocking {
         color: var(--error, #ff8a8a); background: var(--surface, #171a21);
         background: color-mix(in srgb, var(--error, #ff8a8a) 14%, var(--surface, #171a21)); border-color: var(--error, #ff8a8a); }

  .ap-gantt-wrap { overflow-x: auto; border: 1px solid var(--border, #2a2f3a); border-radius: 8px; }
  table.ap-gantt { min-width: 1240px; table-layout: fixed; }
  .ap-gantt .ap-c-ref { width: 52px; }
  .ap-gantt .ap-c-title { width: 270px; }
  .ap-gantt .ap-c-role { width: 160px; }
  .ap-gantt .ap-c-dates { width: 150px; }
  .ap-gantt .ap-c-status { width: 92px; }
  .ap-gantt td.ap-c-track { padding-top: 2px; padding-bottom: 2px; }
  .ap-months { display: flex; }
  .ap-month { border-left: 1px solid var(--border, #2a2f3a); padding-left: 2px; font-size: 10px; overflow: hidden; white-space: nowrap;
         text-transform: none; letter-spacing: 0; }
  .ap-lane { position: relative; height: 20px; }
  .ap-grid { position: absolute; top: 0; bottom: 0; width: 1px; background: var(--border, #2a2f3a); }
  .ap-today { position: absolute; top: 0; bottom: 0; width: 2px; background: var(--error, #ff8a8a); opacity: .65; }
  .ap-bar { position: absolute; top: 5px; height: 10px; background: var(--accent, #6ea8fe); border-radius: 2px; }
  .ap-band { position: absolute; top: 7px; height: 6px; background: var(--muted, #8b93a3); opacity: .5; border-radius: 2px; }
  .ap-done { opacity: .4; }
  .ap-ms { position: absolute; top: 0; transform: translateX(-50%); color: var(--accent, #6ea8fe); font-size: 14px; line-height: 20px; }
  .ap-risk { outline: 2px solid var(--error, #ff8a8a); outline-offset: 1px; }
  .ap-ms.ap-risk { outline: 0; color: var(--error, #ff8a8a); }
  .ap-phase-row td { background: var(--surface, #171a21); font-weight: 600; font-size: 12px; }
  .ap-legend { font-size: 11px; color: var(--muted, #8b93a3); margin: 4px 0 10px; }
  .ap-key-bar, .ap-key-band, .ap-key-risk, .ap-key-today { display: inline-block; vertical-align: middle; }
  .ap-key-bar { width: 22px; height: 8px; background: var(--accent, #6ea8fe); border-radius: 2px; }
  .ap-key-band { width: 22px; height: 6px; background: var(--muted, #8b93a3); opacity: .5; }
  .ap-key-risk { width: 22px; height: 8px; outline: 2px solid var(--error, #ff8a8a); }
  .ap-key-today { width: 2px; height: 12px; background: var(--error, #ff8a8a); }
  .ap-key-ms { color: var(--accent, #6ea8fe); }
`

// sharedScript is the fetch helper both pages use: errors come back as the
// server's message, and the gate's findings are listed rather than counted.
const sharedScript = `
const statusEl = document.getElementById('status');
function say(msg, err) { if (!statusEl) return; statusEl.textContent = msg; statusEl.className = 'status-line' + (err ? ' err' : ''); }
async function call(method, url, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const res = await fetch(url, opts);
  let data = null;
  try { data = await res.json(); } catch (_) {}
  if (!res.ok) {
    let msg = (data && data.error) || ('HTTP ' + res.status);
    if (data && Array.isArray(data.findings)) msg += ': ' + data.findings.map(f => f.message).join(' ');
    if (res.status === 401 || res.status === 403) msg = 'This change needs admin rights. ' + msg;
    throw new Error(msg);
  }
  return data;
}
async function act(method, url, body, then) {
  try { say('Working…'); const d = await call(method, url, body); if (then) { then(d); } else { location.reload(); } }
  catch (e) { say(e.message, true); }
}
function h(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
`

func jsonData(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(raw)
}

func optionList(values []string, selected string, blank string) string {
	var b strings.Builder
	if blank != "" {
		fmt.Fprintf(&b, `<option value="">%s</option>`, esc(blank))
	}
	for _, v := range values {
		sel := ""
		if v == selected {
			sel = " selected"
		}
		fmt.Fprintf(&b, `<option value="%s"%s>%s</option>`, esc(v), sel, esc(v))
	}
	return b.String()
}

// ---- list page ----

type listPageData struct {
	Plans     []Plan
	Filter    Filter
	Clients   []Client
	Templates []TemplateInfo
}

func listPageHTML(d listPageData) string {
	var typeOpts, tplOpts strings.Builder
	typeOpts.WriteString(`<option value="">All types</option>`)
	for _, t := range d.Templates {
		if t.Example {
			continue
		}
		sel := ""
		if t.Key == d.Filter.PlanType {
			sel = " selected"
		}
		fmt.Fprintf(&typeOpts, `<option value="%s"%s>%s</option>`, esc(t.Key), sel, esc(t.Name))
	}
	for _, t := range d.Templates {
		kind := "type"
		if t.Example {
			kind = "example"
		}
		fmt.Fprintf(&tplOpts, `<option value="%s" data-kind="%s" title="%s">%s</option>`, esc(t.Key), kind, esc(t.Description), esc(t.Name))
	}
	var clientOpts, newClientOpts strings.Builder
	clientOpts.WriteString(`<option value="">All clients</option>`)
	newClientOpts.WriteString(`<option value="0">No client (own plan)</option>`)
	for _, c := range d.Clients {
		sel := ""
		if c.ID == d.Filter.ClientID {
			sel = " selected"
		}
		fmt.Fprintf(&clientOpts, `<option value="%d"%s>%s</option>`, c.ID, sel, esc(c.Name))
		fmt.Fprintf(&newClientOpts, `<option value="%d">%s</option>`, c.ID, esc(c.Name))
	}

	var rows strings.Builder
	for _, p := range d.Plans {
		state := statusPill(p.Status)
		if p.RevisionStatus != "" {
			state += ` <span class="muted">rev. ` + strconv.Itoa(p.Revision) + `</span> ` + statusPill(p.RevisionStatus)
		}
		fmt.Fprintf(&rows, `<tr><td><a href="/assurance-plans/%d">%s</a></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td>
<td><button class="mini" type="button" data-clone="%d" data-title="%s">Clone</button></td></tr>`,
			p.ID, esc(p.Reference), esc(p.Title), esc(p.PlanTypeName), state, esc(displayRange(p.PeriodStart, p.PeriodEnd)),
			esc(firstNonEmpty(p.ClientName, "—")), esc(p.OwnerRole), esc(firstNonEmpty(p.LatestVersion, "—")),
			esc(displayDate(strings.SplitN(p.UpdatedAt, "T", 2)[0])), p.ID, esc(p.Title))
	}
	if len(d.Plans) == 0 {
		rows.WriteString(`<tr><td colspan="10" class="muted">No plans match. Create one below, from a plan type or from the worked example.</td></tr>`)
	}

	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Assurance Plans · GRC</title>
<style>` + pageStyles + `</style>
</head>
<body>
<main>
` + pageui.Nav("/assurance-plans") + `
<h1>Assurance Plans</h1>
<p class="lede">Annual plans of the second line: a controls testing plan with statistically sized quarterly samples,
a risk assessment cycle, policy review, resilience testing or a remediation roadmap. Each plan is drafted, reviewed and
approved by its forum, then run quarter by quarter; an approved design changes only through a change request.
Reading is open; creating, editing and approving require admin. See <a href="/knowledge/assurance-plans">ASSURANCE_PLANS.md</a>.</p>

<form class="toolbar" method="get" action="/assurance-plans">
  <select name="type">` + typeOpts.String() + `</select>
  <select name="status">` + optionList(Statuses, d.Filter.Status, "All statuses") + `</select>
  <input name="year" placeholder="Year" value="` + esc(d.Filter.Year) + `" size="6" inputmode="numeric">
  <select name="client">` + clientOpts.String() + `</select>
  <input type="search" name="q" placeholder="Reference or title" value="` + esc(d.Filter.Search) + `">
  <button type="submit">Filter</button>
  <a href="/assurance-plans">Clear</a>
</form>

<div class="table-wrap">
<table>
<thead><tr><th>Reference</th><th>Title</th><th>Type</th><th>Status</th><th>Period</th><th>Client</th><th>Owner</th><th>Version</th><th>Updated</th><th></th></tr></thead>
<tbody>` + rows.String() + `</tbody>
</table>
</div>

<fieldset>
<legend>New plan</legend>
<div class="grid">
  <label>From<select id="nTemplate">` + tplOpts.String() + `</select></label>
  <label>Title (optional)<input id="nTitle" placeholder="Taken from the template when blank"></label>
  <label>Period start<input id="nStart" type="date"></label>
  <label>Period end<input id="nEnd" type="date"></label>
  <label>Client<select id="nClient">` + newClientOpts.String() + `</select></label>
  <label>Function (optional)<input id="nFunction" placeholder="e.g. 2LoD CISO Office"></label>
</div>
<p class="muted" id="nHint"></p>
<div class="toolbar"><button class="primary" id="nCreate" type="button">Create plan</button></div>
<div id="status" class="status-line"></div>
</fieldset>
</main>
<script>
` + sharedScript + `
const tplSel = document.getElementById('nTemplate');
function hint() {
  const o = tplSel.selectedOptions[0];
  document.getElementById('nHint').textContent = (o.dataset.kind === 'example'
    ? 'A worked example with its own dates; the period fields are ignored. '
    : 'Period defaults to next calendar year. ') + (o.title || '');
}
tplSel.addEventListener('change', hint); hint();
document.getElementById('nCreate').addEventListener('click', () => {
  const o = tplSel.selectedOptions[0];
  const body = {
    title: document.getElementById('nTitle').value,
    client_profile_id: Number(document.getElementById('nClient').value || 0),
    function_name: document.getElementById('nFunction').value,
  };
  if (o.dataset.kind === 'example') { body.example = o.value; }
  else { body.type = o.value; body.period_start = document.getElementById('nStart').value; body.period_end = document.getElementById('nEnd').value; }
  act('POST', '/assurance-plans', body, p => { location.href = '/assurance-plans/' + p.id; });
});
document.querySelectorAll('[data-clone]').forEach(b => b.addEventListener('click', () => {
  const t = prompt('Title of the clone (blank: the same title with its years moved on by one):', '');
  if (t === null) return;
  act('POST', '/assurance-plans/' + b.dataset.clone + '/clone', { title: t }, p => { location.href = '/assurance-plans/' + p.id; });
}));
</script>
</body>
</html>`
}

// ---- plan page ----

type planPageData struct {
	Plan        Plan
	Lint        []LintFinding
	History     []HistoryEntry
	Versions    []Version
	Clients     []Client
	Today       string
	Viewer      string
	SepEnforced bool
}

var planTabs = []struct{ id, label string }{
	{"overview", "Overview"},
	{"timeline", "Plan & timeline"},
	{"quarters", "Quarterly plan"},
	{"governance", "Governance"},
	{"history", "History & versions"},
}

func planPageHTML(d planPageData) string {
	p := d.Plan
	var tabs strings.Builder
	for i, t := range planTabs {
		fmt.Fprintf(&tabs, `<button type="button" role="tab" data-tab="%s" aria-selected="%t">%s</button>`, t.id, i == 0, esc(t.label))
	}
	state := statusPill(p.Status)
	if p.RevisionStatus != "" {
		state += ` · revision ` + strconv.Itoa(p.Revision) + ` ` + statusPill(p.RevisionStatus)
	}
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>` + esc(p.Reference) + ` · Assurance Plans · GRC</title>
<style>` + pageStyles + `</style>
</head>
<body>
<main>
` + pageui.Nav("/assurance-plans") + `
<p class="muted"><a href="/assurance-plans">Assurance Plans</a> / ` + esc(p.Reference) + `</p>
<h1>` + esc(p.Title) + `</h1>
<p class="lede">` + esc(p.Reference) + ` · ` + esc(p.PlanTypeName) + ` · ` + esc(displayRange(p.PeriodStart, p.PeriodEnd)) + ` · ` + state + `</p>
<div class="toolbar">` + lifecycleButtons(p) + `</div>
<div id="status" class="status-line"></div>
<nav class="ap-tabs" role="tablist">` + tabs.String() + `</nav>

<section class="tab-panel" id="tab-overview">` + overviewHTML(d) + `</section>
<section class="tab-panel" id="tab-timeline" hidden>
  <h2>Timeline</h2>` + ganttHTML(p, d.Today) + `
  <h2>Steps</h2>
  ` + lockNotice(p) + `
  <div id="stepsEditor"></div>
</section>
<section class="tab-panel" id="tab-quarters" hidden>` + quartersHTML(p) + `</section>
<section class="tab-panel" id="tab-governance" hidden>` + governanceHTML(p) + `</section>
<section class="tab-panel" id="tab-history" hidden>` + historyHTML(d) + `</section>
</main>
<script type="application/json" id="plan-data">` + jsonData(p) + `</script>
<script type="application/json" id="vocab-data">` + jsonData(Vocab()) + `</script>
<script type="application/json" id="page-data">` + jsonData(map[string]any{"viewer": d.Viewer, "sep_enforced": d.SepEnforced}) + `</script>
<script>
` + sharedScript + planScript + `
</script>
</body>
</html>`
}

func lockNotice(p Plan) string {
	switch {
	case p.Status == StatusClosed:
		return `<p class="notice">The plan is closed: nothing on it changes any more.</p>`
	case !p.DesignLocked:
		if p.RevisionStatus == StatusDraft {
			return `<p class="notice warn">Revision ` + strconv.Itoa(p.Revision) + ` is open (reason: ` + esc(p.ChangeReason) + `). Design changes made now go to review and approval as the next version; execution continues against the plan meanwhile.</p>`
		}
		return ""
	case p.Status == StatusInReview || p.RevisionStatus == StatusInReview:
		return `<p class="notice">In review: the design is read-only until it is approved or returned to draft. Step status can still be updated.</p>`
	}
	return `<p class="notice">The design is locked since approval. Step status stays editable; anything else needs a change request with a reason.</p>`
}

func lifecycleButtons(p Plan) string {
	var b []string
	btn := func(action, label, class string) {
		b = append(b, fmt.Sprintf(`<button type="button" class="%s" data-action="%s">%s</button>`, class, action, esc(label)))
	}
	switch {
	case p.Status == StatusDraft:
		btn("submit", "Submit for review", "primary")
	case p.Status == StatusInReview:
		btn("approve", "Approve", "primary")
		btn("return", "Return to draft", "")
	case p.RevisionStatus == StatusDraft:
		btn("submit", fmt.Sprintf("Submit revision %d for review", p.Revision), "primary")
	case p.RevisionStatus == StatusInReview:
		btn("approve", fmt.Sprintf("Approve revision %d", p.Revision), "primary")
		btn("return", "Return revision to draft", "")
	}
	if p.Status == StatusApproved {
		btn("activate", "Activate (start execution)", "")
	}
	if (p.Status == StatusApproved || p.Status == StatusActive) && p.RevisionStatus == "" {
		btn("change-request", "Raise change request", "")
	}
	if p.Status == StatusActive && p.RevisionStatus == "" {
		btn("close", "Close plan", "")
	}
	b = append(b, `<span class="spacer"></span>`)
	btn("clone", "Clone for next year", "")
	if p.Status == StatusDraft && p.VersionCount == 0 {
		btn("delete", "Delete draft", "danger")
	}
	return strings.Join(b, "")
}

func overviewHTML(d planPageData) string {
	p := d.Plan
	counts := map[string]int{}
	for _, s := range p.Steps {
		counts[s.StepType]++
	}
	blocking := len(BlockingFindings(d.Lint))
	advisory := len(d.Lint) - blocking
	card := func(n, label string) string {
		return `<div class="card"><div class="n">` + esc(n) + `</div><div class="l">` + esc(label) + `</div></div>`
	}
	var b strings.Builder
	b.WriteString(`<div class="cards">`)
	b.WriteString(card(strconv.Itoa(len(p.Steps)), "Steps"))
	b.WriteString(card(strconv.Itoa(counts[StepMilestone]), "Milestones"))
	b.WriteString(card(strconv.Itoa(counts[StepOngoing]), "Ongoing items"))
	b.WriteString(card(strconv.Itoa(len(p.Quarters)), "Quarters"))
	b.WriteString(card(firstNonEmpty(p.LatestVersion, "none"), "Approved version"))
	b.WriteString(card(strconv.Itoa(blocking), "Blocking issues"))
	b.WriteString(card(strconv.Itoa(advisory), "Advisory notes"))
	b.WriteString(`</div>`)

	b.WriteString(`<div class="grid wide"><div>`)
	b.WriteString(`<h2>Objective</h2><p class="pre">` + esc(firstNonEmpty(p.Objective, "Not set.")) + `</p>`)
	b.WriteString(`<h2>Decisions requested</h2>`)
	if strings.TrimSpace(p.DecisionsRequested) == "" {
		b.WriteString(`<p class="muted">None recorded.</p>`)
	} else {
		b.WriteString(`<ol>`)
		for _, line := range strings.Split(p.DecisionsRequested, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				b.WriteString(`<li>` + esc(line) + `</li>`)
			}
		}
		b.WriteString(`</ol>`)
	}
	b.WriteString(`</div><div><h2>Status and approval</h2><dl class="kv">`)
	kv := func(k, v string) { b.WriteString(`<dt>` + esc(k) + `</dt><dd>` + v + `</dd>`) }
	kv("Status", statusPill(p.Status))
	if p.RevisionStatus != "" {
		kv("Open revision", fmt.Sprintf("%d %s · %s", p.Revision, statusPill(p.RevisionStatus), esc(p.ChangeReason)))
	}
	kv("Approval forum", esc(firstNonEmpty(p.ApprovalForum, "—")))
	kv("Owner / accountable", esc(firstNonEmpty(p.OwnerRole, "—"))+" / "+esc(firstNonEmpty(p.AccountableRole, "—")))
	kv("Function", esc(firstNonEmpty(p.FunctionName, "—")))
	kv("Client", esc(firstNonEmpty(p.ClientName, "—")))
	kv("Author", esc(firstNonEmpty(p.Author, "—")))
	if p.ApprovedBy != "" {
		kv("Last approved", esc(p.ApprovedBy)+" · "+esc(p.ApprovedAt))
	}
	kv("Mobilise / close", esc(firstNonEmpty(displayDate(p.MobiliseStart), "—"))+" / "+esc(firstNonEmpty(displayDate(p.ClosingDate), "—")))
	kv("Regulatory basis", esc(firstNonEmpty(p.RegulatoryRefs, "—")))
	if d.SepEnforced {
		kv("Separation of duties", "Enforced: the approver cannot be the author of the revision.")
	} else {
		kv("Separation of duties", `<span class="muted">Not enforced (single-user or local mode): an approval by the author is recorded with a warning.</span>`)
	}
	b.WriteString(`</dl></div></div>`)

	b.WriteString(`<h2>Approval gate</h2>`)
	if len(d.Lint) == 0 {
		b.WriteString(`<p>No issues: the plan can be approved once it is in review.</p>`)
	} else {
		b.WriteString(`<ul class="lint">`)
		for _, f := range d.Lint {
			label := "Blocking"
			if f.Severity == LintAdvisory {
				label = "Advisory"
			}
			b.WriteString(`<li>` + statusPill(label) + ` ` + esc(f.Message) + `</li>`)
		}
		b.WriteString(`</ul>`)
	}

	b.WriteString(`<h2>Plan header and parameters</h2>` + lockNotice(p) + headerForm(p, d.Clients))
	return b.String()
}

func headerForm(p Plan, clients []Client) string {
	dis := ""
	if p.DesignLocked {
		dis = " disabled"
	}
	field := func(label, key, value, typ string) string {
		return fmt.Sprintf(`<label>%s<input data-h="%s" type="%s" value="%s"%s></label>`, esc(label), key, typ, esc(value), dis)
	}
	area := func(label, key, value string) string {
		return fmt.Sprintf(`<label>%s<textarea data-h="%s"%s>%s</textarea></label>`, esc(label), key, dis, esc(value))
	}
	param := func(label, key string, value float64, step string) string {
		return fmt.Sprintf(`<label>%s<input data-p="%s" type="number" step="%s" value="%s"%s></label>`, esc(label), key, step, strconv.FormatFloat(value, 'f', -1, 64), dis)
	}
	var clientOpts strings.Builder
	clientOpts.WriteString(`<option value="0">No client (own plan)</option>`)
	for _, c := range clients {
		sel := ""
		if c.ID == p.ClientProfileID {
			sel = " selected"
		}
		fmt.Fprintf(&clientOpts, `<option value="%d"%s>%s</option>`, c.ID, sel, esc(c.Name))
	}
	pr := p.Parameters
	var b strings.Builder
	b.WriteString(`<fieldset><legend>Identity and roles</legend><div class="grid">`)
	b.WriteString(field("Title", "title", p.Title, "text"))
	b.WriteString(field("Function", "function_name", p.FunctionName, "text"))
	b.WriteString(`<label>Client<select data-h="client_profile_id"` + dis + `>` + clientOpts.String() + `</select></label>`)
	b.WriteString(field("Owner role", "owner_role", p.OwnerRole, "text"))
	b.WriteString(field("Accountable role", "accountable_role", p.AccountableRole, "text"))
	b.WriteString(field("Approval forum", "approval_forum", p.ApprovalForum, "text"))
	b.WriteString(`</div></fieldset><fieldset><legend>Period</legend><div class="grid">`)
	b.WriteString(field("Mobilisation start", "mobilise_start", p.MobiliseStart, "date"))
	b.WriteString(field("Period start", "period_start", p.PeriodStart, "date"))
	b.WriteString(field("Period end", "period_end", p.PeriodEnd, "date"))
	b.WriteString(field("Annual conclusion (closing) date", "closing_date", p.ClosingDate, "date"))
	b.WriteString(`</div>`)
	b.WriteString(area("Non-working dates (YYYY-MM-DD, separated by commas): used for “next business day” and “+10 business days”", "non_working_dates", p.NonWorkingDates))
	b.WriteString(`</fieldset><fieldset><legend>Objective and basis</legend><div class="grid wide">`)
	b.WriteString(area("Objective", "objective", p.Objective))
	b.WriteString(area("Decisions requested (one per line)", "decisions_requested", p.DecisionsRequested))
	b.WriteString(area("Regulatory references", "regulatory_refs", p.RegulatoryRefs))
	b.WriteString(`</div></fieldset><fieldset><legend>Parameters</legend><div class="grid">`)
	b.WriteString(param("Tester FTE", "tester_fte", pr.TesterFTE, "0.1"))
	b.WriteString(param("Productive days per FTE per quarter", "days_per_fte_quarter", pr.DaysPerFTEQuarter, "1"))
	b.WriteString(param("Utilisation threshold (fraction)", "utilisation_threshold", pr.UtilisationThreshold, "0.01"))
	b.WriteString(param("Amber band (points below plan)", "rag_amber_points", pr.RAGAmberPoints, "1"))
	b.WriteString(param("Partially effective from (fails)", "result_partial_min", float64(pr.ResultPartialMin), "1"))
	b.WriteString(param("Ineffective from (fails)", "result_ineffective_min", float64(pr.ResultIneffectiveMin), "1"))
	b.WriteString(param("Extend sample at (deviations)", "deviation_extend_at", float64(pr.DeviationExtendAt), "1"))
	b.WriteString(param("Not effective at (deviations)", "deviation_thematic_at", float64(pr.DeviationThematicAt), "1"))
	b.WriteString(param("Sample extension (%)", "extend_pct", pr.ExtendPct, "1"))
	b.WriteString(param("Evidence SLA (business days)", "evidence_sla_days", float64(pr.EvidenceSLADays), "1"))
	b.WriteString(`</div></fieldset>`)
	if !p.DesignLocked {
		b.WriteString(`<div class="toolbar"><button class="primary" type="button" id="saveHeader">Save header</button></div>`)
	}
	return b.String()
}

func quartersHTML(p Plan) string {
	var b strings.Builder
	b.WriteString(`<h2>Quarterly operating cycle</h2>
<p class="lede">Default cycle, offset from each quarter's 1LoD deadline (week 0): selection the next business day;
fieldwork from a week later to +6 weeks; QA four-eyes to +7 weeks; findings agreed by +8 weeks; quarterly report at +9 weeks.
The 2LoD data-quality review runs from a week after the window opens to a week after the deadline.</p>` + lockNotice(p))
	b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Q</th><th>1LoD window</th><th>1LoD deadline</th><th>Selection</th><th>Fieldwork</th><th>Findings agreed</th><th>Report</th><th>Committee</th><th class="num">Retest days</th><th class="num">Overhead days</th><th>Sample</th></tr></thead><tbody>`)
	for _, q := range p.Quarters {
		frozen := `<span class="muted">not selected</span>`
		if q.SelectedAt != "" {
			frozen = "frozen " + esc(q.SelectedAt) + " by " + esc(firstNonEmpty(q.SelectedBy, "—"))
		}
		fmt.Fprintf(&b, `<tr><td>Q%d</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td class="num">%g</td><td class="num">%g</td><td>%s</td></tr>`,
			q.Quarter, esc(displayDate(q.LOD1WindowStart)), esc(displayDate(q.LOD1Deadline)), esc(displayDate(q.SelectionDate)),
			esc(displayRange(q.FieldworkStart, q.FieldworkEnd)), esc(displayDate(q.FindingsAgreedBy)), esc(displayDate(q.ReportDate)),
			esc(q.CommitteeLabel), q.RetestDays, q.OverheadDays, frozen)
	}
	if len(p.Quarters) == 0 {
		b.WriteString(`<tr><td colspan="11" class="muted">No quarters. Generate the default cycle, or add them below.</td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	if !p.DesignLocked {
		b.WriteString(`<h3>Edit quarters</h3><div id="quartersEditor"></div>
<div class="toolbar">
  <button class="primary" type="button" id="saveQuarters">Save quarters</button>
  <button type="button" id="genCycle" title="Replace the quarter dates with the default cycle for the period, then regenerate the quarterly steps">Reset to default cycle</button>
  <button type="button" id="genSteps" title="Rebuild the per-quarter steps (x.1 to x.8) from the quarter dates; step status is kept by reference">Regenerate quarterly steps</button>
</div>`)
	}
	return b.String()
}

func governanceHTML(p Plan) string {
	var b strings.Builder
	b.WriteString(`<h2>RACI</h2>` + lockNotice(p))
	b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Activity</th>`)
	for _, r := range p.RACIRoles {
		b.WriteString(`<th>` + esc(r) + `</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, row := range p.RACI {
		b.WriteString(`<tr><td>` + esc(row.Activity) + `</td>`)
		for _, r := range p.RACIRoles {
			b.WriteString(`<td>` + esc(row.Cells[r]) + `</td>`)
		}
		b.WriteString(`</tr>`)
	}
	if len(p.RACI) == 0 {
		b.WriteString(`<tr><td class="muted">No RACI yet.</td></tr>`)
	}
	b.WriteString(`</tbody></table></div><p class="ap-legend">R responsible · A accountable · C consulted · I informed · A/R accountable and responsible.</p>`)

	b.WriteString(`<h2>Reporting cadence</h2><div class="table-wrap"><table><thead><tr><th>Forum</th><th>Frequency</th><th>Content</th><th>Owner</th></tr></thead><tbody>`)
	for _, c := range p.Cadence {
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`, esc(c.Forum), esc(c.Frequency), esc(c.Content), esc(c.Owner))
	}
	if len(p.Cadence) == 0 {
		b.WriteString(`<tr><td colspan="4" class="muted">No reporting cadence yet.</td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)

	b.WriteString(`<h2>Escalation triggers</h2><div class="table-wrap"><table><thead><tr><th>Trigger</th><th>Escalate to</th><th>Within</th></tr></thead><tbody>`)
	for _, e := range p.Escalations {
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td></tr>`, esc(e.Trigger), esc(e.EscalateTo), esc(e.Within))
	}
	if len(p.Escalations) == 0 {
		b.WriteString(`<tr><td colspan="3" class="muted">No escalation triggers yet.</td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	if !p.DesignLocked {
		b.WriteString(`<h3>Edit governance</h3><div id="govEditor"></div><div class="toolbar"><button class="primary" type="button" id="saveGov">Save governance</button></div>`)
	}
	return b.String()
}

func historyHTML(d planPageData) string {
	var b strings.Builder
	b.WriteString(`<h2>Approved versions</h2><p class="lede">Each approval stores the plan as approved, as JSON with its SHA-256. A version is never rewritten.</p>`)
	b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Version</th><th>Approved by</th><th>Approved at</th><th>Change reason / note</th><th>SHA-256</th><th></th></tr></thead><tbody>`)
	for _, v := range d.Versions {
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td><code>%s…</code></td><td><a href="/assurance-plans/%d/versions/%d">JSON</a></td></tr>`,
			esc(v.Label), esc(v.ApprovedBy), esc(v.ApprovedAt), esc(v.ChangeReason), esc(clip(v.SnapshotSHA256, 16)), d.Plan.ID, v.Number)
	}
	if len(d.Versions) == 0 {
		b.WriteString(`<tr><td colspan="6" class="muted">Not approved yet.</td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	b.WriteString(`<h2>History</h2><div class="table-wrap"><table><thead><tr><th>When</th><th>Who</th><th>Event</th><th>Subject</th><th>From</th><th>To</th><th>Note</th></tr></thead><tbody>`)
	for _, e := range d.History {
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			esc(e.At), esc(firstNonEmpty(e.Actor, "—")), esc(e.Event), esc(e.Subject), esc(clip(e.FromValue, 200)), esc(clip(e.ToValue, 200)), esc(e.Note))
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

// planScript drives the tabs, the editors and the lifecycle buttons. The
// editors post whole lists; the server validates them and writes the history.
const planScript = `
const PLAN = JSON.parse(document.getElementById('plan-data').textContent);
const VOCAB = JSON.parse(document.getElementById('vocab-data').textContent);
const PAGE = JSON.parse(document.getElementById('page-data').textContent);
const base = '/assurance-plans/' + PLAN.id;

function showTab(id) {
  document.querySelectorAll('[data-tab]').forEach(b => b.setAttribute('aria-selected', String(b.dataset.tab === id)));
  document.querySelectorAll('section.tab-panel').forEach(s => { s.hidden = s.id !== 'tab-' + id; });
}
document.querySelectorAll('[data-tab]').forEach(b => b.addEventListener('click', () => { history.replaceState(null, '', '#' + b.dataset.tab); showTab(b.dataset.tab); }));
if (location.hash && document.getElementById('tab-' + location.hash.slice(1))) showTab(location.hash.slice(1));

function control(col, value) {
  let el;
  if (col.type === 'select') {
    el = document.createElement('select');
    (col.options || []).forEach(o => { const opt = document.createElement('option'); opt.value = o; opt.textContent = o === '' ? '—' : o; el.appendChild(opt); });
  } else if (col.type === 'textarea') {
    el = document.createElement('textarea');
  } else {
    el = document.createElement('input');
    el.type = col.type || 'text';
    if (col.type === 'number') el.step = col.step || 'any';
  }
  el.value = value == null ? '' : value;
  el.dataset.k = col.key;
  if (col.width) el.style.minWidth = col.width;
  return el;
}

function editor(host, cols, rows, blank) {
  const table = document.createElement('table');
  table.className = 'edit-table';
  table.innerHTML = '<thead><tr>' + cols.map(c => '<th>' + h(c.label) + '</th>').join('') + '<th></th></tr></thead>';
  const body = document.createElement('tbody');
  table.appendChild(body);
  function add(obj, before) {
    const tr = document.createElement('tr');
    tr._obj = Object.assign({}, obj);
    cols.forEach(c => { const td = document.createElement('td'); td.appendChild(control(c, obj[c.key])); tr.appendChild(td); });
    const ctl = document.createElement('td');
    ctl.className = 'ctl';
    [['↑', () => tr.previousElementSibling && body.insertBefore(tr, tr.previousElementSibling)],
     ['↓', () => tr.nextElementSibling && body.insertBefore(tr.nextElementSibling, tr)],
     ['+', () => add(blank(), tr.nextElementSibling)],
     ['✕', () => tr.remove()]].forEach(([label, fn]) => {
      const b = document.createElement('button'); b.type = 'button'; b.className = 'mini'; b.textContent = label;
      b.title = {'↑': 'Move up', '↓': 'Move down', '+': 'Insert a row below', '✕': 'Remove'}[label];
      b.addEventListener('click', fn); ctl.appendChild(b);
    });
    tr.appendChild(ctl);
    body.insertBefore(tr, before || null);
  }
  rows.forEach(r => add(r));
  const wrap = document.createElement('div'); wrap.className = 'table-wrap'; wrap.appendChild(table);
  host.appendChild(wrap);
  const addBtn = document.createElement('button'); addBtn.type = 'button'; addBtn.textContent = 'Add row';
  addBtn.addEventListener('click', () => add(blank()));
  const tb = document.createElement('div'); tb.className = 'toolbar'; tb.appendChild(addBtn); host.appendChild(tb);
  return {
    rows() {
      return [...body.children].map(tr => {
        const o = Object.assign({}, tr._obj);
        tr.querySelectorAll('[data-k]').forEach(el => { o[el.dataset.k] = el.type === 'number' ? Number(el.value || 0) : el.value; });
        return o;
      });
    }
  };
}

// Steps: the full editor while the design is open; afterwards, status only.
const stepsHost = document.getElementById('stepsEditor');
if (!PLAN.design_locked) {
  const ed = editor(stepsHost, [
    { key: 'step_ref', label: 'Ref', width: '48px' },
    { key: 'phase', label: 'Phase', width: '120px' },
    { key: 'title', label: 'Title', type: 'textarea', width: '200px' },
    { key: 'step_type', label: 'Type', type: 'select', options: VOCAB.step_types },
    { key: 'responsible_role', label: 'Responsible', width: '120px' },
    { key: 'accountable_role', label: 'Accountable', width: '120px' },
    { key: 'start_date', label: 'Start (or milestone date)', type: 'date' },
    { key: 'end_date', label: 'End', type: 'date' },
    { key: 'deliverable', label: 'Deliverable / evidence', type: 'textarea', width: '160px' },
    { key: 'refs', label: 'References', width: '110px' },
    { key: 'status', label: 'Status', type: 'select', options: VOCAB.step_statuses },
  ], PLAN.steps, () => ({ step_type: 'activity', status: 'Not started' }));
  const save = document.createElement('button'); save.type = 'button'; save.className = 'primary'; save.textContent = 'Save steps';
  save.addEventListener('click', () => act('PUT', base + '/steps', ed.rows(), () => { location.hash = 'timeline'; location.reload(); }));
  stepsHost.appendChild(save);
} else if (PLAN.status !== 'Closed') {
  const rows = PLAN.steps.map(s => '<tr><td>' + h(s.step_ref) + '</td><td>' + h(s.title) + '</td><td>' + h(s.responsible_role) +
    '</td><td><select data-step="' + s.id + '">' + VOCAB.step_statuses.map(v => '<option' + (v === s.status ? ' selected' : '') + '>' + h(v) + '</option>').join('') + '</select></td></tr>').join('');
  stepsHost.innerHTML = '<div class="table-wrap"><table><thead><tr><th>Ref</th><th>Step</th><th>Responsible</th><th>Status</th></tr></thead><tbody>' + rows + '</tbody></table></div>';
  stepsHost.querySelectorAll('[data-step]').forEach(sel => sel.addEventListener('change', () =>
    act('PATCH', base + '/steps/' + sel.dataset.step, { status: sel.value }, () => say('Status of the step saved.'))));
}

const saveHeader = document.getElementById('saveHeader');
if (saveHeader) saveHeader.addEventListener('click', () => {
  const body = { parameters: {} };
  document.querySelectorAll('[data-h]').forEach(el => { body[el.dataset.h] = el.dataset.h === 'client_profile_id' ? Number(el.value || 0) : el.value; });
  document.querySelectorAll('[data-p]').forEach(el => { body.parameters[el.dataset.p] = Number(el.value || 0); });
  act('PUT', base, body);
});

const qHost = document.getElementById('quartersEditor');
if (qHost) {
  const qed = editor(qHost, [
    { key: 'quarter', label: 'Q', type: 'number', width: '44px' },
    { key: 'lod1_window_start', label: '1LoD window', type: 'date' },
    { key: 'lod1_deadline', label: '1LoD deadline', type: 'date' },
    { key: 'selection_date', label: 'Selection', type: 'date' },
    { key: 'fieldwork_start', label: 'Fieldwork start', type: 'date' },
    { key: 'fieldwork_end', label: 'Fieldwork end', type: 'date' },
    { key: 'findings_agreed_by', label: 'Findings agreed', type: 'date' },
    { key: 'report_date', label: 'Report', type: 'date' },
    { key: 'committee_label', label: 'Committee', width: '130px' },
    { key: 'retest_days', label: 'Retest days', type: 'number', width: '64px' },
    { key: 'overhead_days', label: 'Overhead days', type: 'number', width: '64px' },
  ], PLAN.quarters, () => ({ quarter: PLAN.quarters.length + 1 }));
  document.getElementById('saveQuarters').addEventListener('click', () => act('PUT', base + '/quarters', qed.rows(), () => { location.hash = 'quarters'; location.reload(); }));
  document.getElementById('genCycle').addEventListener('click', () => {
    if (!confirm('Replace every quarter date with the default cycle for the period, and regenerate the quarterly steps?')) return;
    act('POST', base + '/quarters/generate', { dates: true }, () => { location.hash = 'quarters'; location.reload(); });
  });
  document.getElementById('genSteps').addEventListener('click', () => {
    if (!confirm('Rebuild the per-quarter steps from the quarter dates? Edits made to those steps by hand are replaced; status is kept.')) return;
    act('POST', base + '/quarters/generate', { dates: false }, () => { location.hash = 'timeline'; location.reload(); });
  });
}

const gHost = document.getElementById('govEditor');
if (gHost) {
  let roles = PLAN.raci_roles.slice();
  let raci = PLAN.raci.map(r => ({ activity: r.activity, cells: Object.assign({}, r.cells) }));
  const raciHost = document.createElement('div'); gHost.appendChild(raciHost);
  function readRaci() {
    raciHost.querySelectorAll('[data-role-i]').forEach(el => {
      const i = Number(el.dataset.roleI), old = roles[i], name = el.value;
      if (old !== name) raci.forEach(r => { if (old in r.cells) { r.cells[name] = r.cells[old]; delete r.cells[old]; } });
      roles[i] = name;
    });
    raciHost.querySelectorAll('[data-act-i]').forEach(el => { raci[Number(el.dataset.actI)].activity = el.value; });
    raciHost.querySelectorAll('[data-cell]').forEach(el => { const [i, j] = el.dataset.cell.split(':').map(Number); raci[i].cells[roles[j]] = el.value; });
  }
  function drawRaci() {
    const opts = v => ['', ...VOCAB.raci_letters].map(o => '<option value="' + o + '"' + (o === v ? ' selected' : '') + '>' + (o || '—') + '</option>').join('');
    raciHost.innerHTML = '<h3>RACI</h3><div class="table-wrap"><table class="edit-table"><thead><tr><th>Activity</th>' +
      roles.map((r, j) => '<th><input data-role-i="' + j + '" value="' + h(r) + '"><button type="button" class="mini" data-del-role="' + j + '" title="Remove role">✕</button></th>').join('') +
      '<th></th></tr></thead><tbody>' +
      raci.map((row, i) => '<tr><td><input data-act-i="' + i + '" value="' + h(row.activity) + '"></td>' +
        roles.map((r, j) => '<td><select data-cell="' + i + ':' + j + '">' + opts(row.cells[r] || '') + '</select></td>').join('') +
        '<td class="ctl"><button type="button" class="mini" data-del-act="' + i + '" title="Remove activity">✕</button></td></tr>').join('') +
      '</tbody></table></div><div class="toolbar"><button type="button" id="addRole">Add role</button><button type="button" id="addAct">Add activity</button></div>';
    raciHost.querySelectorAll('[data-del-role]').forEach(b => b.addEventListener('click', () => { readRaci(); roles.splice(Number(b.dataset.delRole), 1); drawRaci(); }));
    raciHost.querySelectorAll('[data-del-act]').forEach(b => b.addEventListener('click', () => { readRaci(); raci.splice(Number(b.dataset.delAct), 1); drawRaci(); }));
    raciHost.querySelector('#addRole').addEventListener('click', () => { readRaci(); roles.push('New role'); drawRaci(); });
    raciHost.querySelector('#addAct').addEventListener('click', () => { readRaci(); raci.push({ activity: '', cells: {} }); drawRaci(); });
  }
  drawRaci();
  const cadHost = document.createElement('div'); cadHost.innerHTML = '<h3>Reporting cadence</h3>'; gHost.appendChild(cadHost);
  const cad = editor(cadHost, [
    { key: 'forum', label: 'Forum' }, { key: 'frequency', label: 'Frequency' },
    { key: 'content', label: 'Content', type: 'textarea' }, { key: 'owner', label: 'Owner' },
  ], PLAN.cadence, () => ({}));
  const escHost = document.createElement('div'); escHost.innerHTML = '<h3>Escalation triggers</h3>'; gHost.appendChild(escHost);
  const escEd = editor(escHost, [
    { key: 'trigger', label: 'Trigger', type: 'textarea' }, { key: 'escalate_to', label: 'Escalate to' }, { key: 'within', label: 'Within' },
  ], PLAN.escalations, () => ({}));
  document.getElementById('saveGov').addEventListener('click', () => {
    readRaci();
    act('PUT', base + '/governance', { raci_roles: roles, raci: raci, cadence: cad.rows(), escalations: escEd.rows() },
      () => { location.hash = 'governance'; location.reload(); });
  });
}

document.querySelectorAll('[data-action]').forEach(b => b.addEventListener('click', () => {
  const a = b.dataset.action;
  if (a === 'submit' || a === 'activate') return act('POST', base + '/' + a, {});
  if (a === 'return') { const note = prompt('Why is it going back to draft?', ''); if (note === null) return; return act('POST', base + '/return', { note }); }
  if (a === 'close') { const note = prompt('Closing note (optional):', ''); if (note === null) return; return act('POST', base + '/close', { note }); }
  if (a === 'change-request') {
    const reason = prompt('Reason for the change (new, decommissioned or re-tiered assets, a recalibration…):', '');
    if (!reason) return;
    return act('POST', base + '/change-request', { reason });
  }
  if (a === 'approve') {
    let by = PAGE.viewer;
    if (!by) { by = prompt('Approved by (name or role):', ''); if (!by) return; }
    const note = prompt('Approval note (optional, e.g. the committee minute reference):', '');
    if (note === null) return;
    return act('POST', base + '/approve', { approved_by: by, note }, r => {
      const lines = (r.warnings || []).concat((r.advisory || []).map(f => 'Advisory: ' + f.message));
      if (lines.length) alert('Approved as ' + r.version.label + '.\n\n' + lines.join('\n'));
      location.reload();
    });
  }
  if (a === 'clone') {
    const t = prompt('Title of the clone (blank: the same title with its years moved on by one):', '');
    if (t === null) return;
    return act('POST', base + '/clone', { title: t }, p => { location.href = '/assurance-plans/' + p.id; });
  }
  if (a === 'delete') {
    if (!confirm('Delete this draft plan? It has never been approved, so nothing else refers to it.')) return;
    return act('DELETE', base, undefined, () => { location.href = '/assurance-plans'; });
  }
}));
`
