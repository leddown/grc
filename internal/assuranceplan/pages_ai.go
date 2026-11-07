package assuranceplan

import (
	"fmt"
	"strconv"
	"strings"
)

type aiPageData struct {
	Configured bool         `json:"configured"`
	Model      string       `json:"model"`
	Proposals  []AIProposal `json:"proposals"`
}

func aiKindLabel(kind string) string {
	switch kind {
	case AIKindDraft:
		return "Plan draft"
	case AIKindAttributes:
		return "Test attributes"
	case AIKindNarrative:
		return "Committee narrative"
	}
	return kind
}

func aiHTML(d planPageData) string {
	p := d.Plan
	var b strings.Builder
	b.WriteString(`<h2>AI assist</h2>
<p class="lede">The model drafts and a person decides. Every answer is stored as a proposal with the model, the prompt hash and the time.
Its references are resolved against the shortlist it was shown, the catalogs and the plan's own references; one that resolves nowhere is
shown as unresolved and never stored on the plan. Nothing on the plan changes until a proposal is accepted, and an accepted draft or
attribute still goes through review and the approval gate like a hand edit.</p>`)
	if d.AI.Configured {
		b.WriteString(`<p class="notice">Answers come from ` + esc(d.AI.Model) + `, the model chosen in Settings, through the shared AI router; usage is logged with the rest of the application's AI usage.</p>`)
	} else {
		b.WriteString(`<p class="notice warn">No AI provider is configured. Set the Wintermute server in Settings to use these assists. Every other part of the plan works without one.</p>`)
	}
	locked := p.DesignLocked || p.Status == StatusClosed
	disabled := func(off bool) string {
		if off {
			return ` disabled`
		}
		return ""
	}
	if locked {
		b.WriteString(`<p class="muted">The design is locked, so a plan draft and test attributes cannot be proposed now; committee narrative can.</p>`)
	}
	b.WriteString(`<div class="grid wide">
<fieldset><legend>Draft the plan from a brief</legend>
<p class="muted">Proposes the plan's own steps, the quarter dates and the RACI. The quarterly cycle's steps are rebuilt from the quarter dates.</p>
<label>Objective<textarea id="aiObjective" maxlength="4000" placeholder="What the plan must achieve by the period end">` + esc(p.Objective) + `</textarea></label>
<label>Regulation<input id="aiRegulation" maxlength="4000" placeholder="e.g. DORA Arts. 6 and 8; RTS (EU) 2024/1774" value="` + esc(p.RegulatoryRefs) + `"></label>
<label>Population size<input id="aiPopulation" type="number" min="0" step="1" value="` + strconv.Itoa(plannedPopulation(p)) + `"></label>
<label>Constraints<textarea id="aiConstraints" maxlength="4000" placeholder="Committee dates, freeze periods, staffing, what must not move"></textarea></label>
<div class="toolbar"><button type="button" class="primary" id="aiDraftGo"` + disabled(!d.AI.Configured || locked) + `>Draft steps, quarters and RACI</button></div>
</fieldset>
<fieldset><legend>Propose test attributes</legend>
<p class="muted">Searches the NFR catalog, the 800-53 controls and Regulation Coverage for the focus, and asks the model which of the shortlist to test and how. It judges the shortlist; it does not browse.</p>
<label>Regulation or domain<input id="aiFocus" maxlength="500" placeholder="e.g. DORA Art. 9 access control, backup and restore"></label>
<label>How many, at most<input id="aiCount" type="number" min="1" max="15" value="8"></label>
<div class="toolbar"><button type="button" class="primary" id="aiAttrGo"` + disabled(!d.AI.Configured || locked) + `>Propose attributes</button></div>
</fieldset>
<fieldset><legend>Draft committee narrative</legend>
<p class="muted">An executive summary, and speaker notes for the pack's slides, written from the plan's own data. A figure or article that data does not contain is flagged. An accepted summary goes into its report, which says how it was written.</p>
<label>For<select id="aiDoc"><option value="pack">Committee approval pack</option><option value="q1">Q1 progress report</option><option value="q2">Q2 progress report</option>
<option value="q3">Q3 progress report</option><option value="q4">Q4 progress report</option><option value="annual">Annual conclusion</option></select></label>
<div class="toolbar"><button type="button" class="primary" id="aiNarrGo"` + disabled(!d.AI.Configured) + `>Draft narrative</button></div>
</fieldset>
</div>
<h3>Proposals</h3>`)
	if len(d.AI.Proposals) == 0 {
		b.WriteString(`<p class="muted">No proposals yet.</p>`)
	} else {
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th>#</th><th>Kind</th><th>For</th><th>Model</th><th>Prompt</th><th>Asked</th><th>By</th><th>Status</th><th>Decided</th><th></th></tr></thead><tbody>`)
		for _, pr := range d.AI.Proposals {
			doc := "—"
			if pr.Document != "" {
				doc = documentLabel(pr.Document)
			}
			decided := "—"
			if pr.DecidedAt != "" {
				decided = esc(pr.DecidedBy) + ` · ` + esc(clip(pr.DecidedAt, 10))
			}
			fmt.Fprintf(&b, `<tr><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td class="ap-nw" title="%s">%s</td><td class="ap-nw">%s</td><td>%s</td><td>%s</td><td>%s</td><td><button type="button" class="mini" data-ai-review="%d">Review</button></td></tr>`,
				pr.ID, esc(aiKindLabel(pr.Kind)), esc(doc), esc(pr.Model), esc(pr.PromptHash), esc(clip(pr.PromptHash, 12)), esc(clip(pr.CreatedAt, 16)),
				esc(pr.CreatedBy), statusPill(upperFirst(pr.Status)), decided, pr.ID)
		}
		b.WriteString(`</tbody></table></div>`)
	}
	b.WriteString(`<div id="aiReview"></div>`)
	return b.String()
}

func plannedPopulation(p Plan) int {
	n := 0
	for _, st := range p.Strata {
		n += st.PlannedSize
	}
	return n
}

const aiScript = `
const AI = JSON.parse(document.getElementById('ai-data').textContent);
const aiHost = document.getElementById('aiReview');
function aiAsk(path, body) {
  say('Asking the model; this can take a minute…');
  act('POST', base + '/ai/' + path, body, pr => { sessionStorage.setItem('apReview', String(pr.id)); location.hash = 'ai'; location.reload(); });
}
const aiDraftGo = document.getElementById('aiDraftGo');
if (aiDraftGo) aiDraftGo.addEventListener('click', () => aiAsk('draft', {
  objective: document.getElementById('aiObjective').value, regulation: document.getElementById('aiRegulation').value,
  population_size: Number(document.getElementById('aiPopulation').value || 0), constraints: document.getElementById('aiConstraints').value }));
const aiAttrGo = document.getElementById('aiAttrGo');
if (aiAttrGo) aiAttrGo.addEventListener('click', () => aiAsk('attributes', {
  focus: document.getElementById('aiFocus').value, count: Number(document.getElementById('aiCount').value || 0) }));
const aiNarrGo = document.getElementById('aiNarrGo');
if (aiNarrGo) aiNarrGo.addEventListener('click', () => aiAsk('narrative', { document: document.getElementById('aiDoc').value }));

function aiRef(r) {
  const label = h(r.kind === 'plan' ? r.ref : r.kind + ' ' + r.ref);
  const t = h(r.title || '') + (r.on_shortlist ? '' : ' (not on the shortlist)');
  return r.url ? '<a href="' + h(r.url) + '" title="' + t + '">' + label + '</a>' : '<span title="' + t + '">' + label + '</span>';
}
function aiRefs(list) { return (list || []).map(aiRef).join('<br>'); }
function aiUnresolved(list) { return (list || []).map(r => '<s>' + h(r.kind + ' ' + r.ref) + '</s> ' + pill('Unresolved')).join('<br>'); }
function aiBasis(b) { return pill(b === 'regulation' ? 'Regulation' : 'Good practice'); }
function aiList(title, items, cls) {
  if (!items || !items.length) return '';
  return '<div class="notice' + (cls ? ' ' + cls : '') + '"><strong>' + h(title) + '</strong><ul>' + items.map(x => '<li>' + h(x) + '</li>').join('') + '</ul></div>';
}
function aiTable(head, rows) {
  return '<div class="table-wrap"><table><thead><tr>' + head.map(x => '<th>' + h(x) + '</th>').join('') + '</tr></thead><tbody>' + rows.join('') + '</tbody></table></div>';
}
function aiDecide(pr, extra) {
  const note = document.getElementById('aiNote');
  const body = Object.assign({ note: note ? note.value : '' }, extra);
  act('POST', base + '/ai/proposals/' + pr.id + '/decide', body, () => { sessionStorage.setItem('apReview', String(pr.id)); location.hash = 'ai'; location.reload(); });
}
function aiReview(pr) {
  const open = pr.status === 'proposed';
  const data = (!open && pr.applied) ? pr.applied : pr.payload;
  let html = '<h3>Proposal ' + pr.id + ' · ' + h({draft: 'Plan draft', attributes: 'Test attributes', narrative: 'Committee narrative'}[pr.kind] || pr.kind) + ' ' + pill(pr.status.charAt(0).toUpperCase() + pr.status.slice(1)) + '</h3>' +
    '<p class="muted">' + h(pr.model) + ' · prompt ' + h(pr.prompt_hash) + ' · asked ' + h(pr.created_at) + ' by ' + h(pr.created_by || '—') +
    (pr.decided_at ? ' · ' + h(pr.status) + ' ' + h(pr.decided_at) + ' by ' + h(pr.decided_by) + (pr.decision_note ? ': ' + h(pr.decision_note) : '') : '') +
    (!open && pr.applied ? ' · showing what was applied' : '') + '</p>';
  html += aiList('Flags: not found in the plan\'s data', data.flags, 'warn') + aiList('What the model did not know', data.unknowns) + aiList('Checks', data.notes);
  if (pr.kind === 'draft') {
    if (data.objective) html += '<h4>Objective</h4><p class="pre">' + h(data.objective) + '</p>';
    if (data.steps && data.steps.length) html += '<h4>Steps</h4>' + aiTable(['Ref', 'Title', 'Type', 'Start', 'End', 'Responsible', 'Accountable', 'Deliverable', 'Basis', 'Cites', 'Unresolved'],
      data.steps.map(s => '<tr><td>' + h(s.step_ref) + '</td><td>' + h(s.title) + '</td><td>' + h(s.step_type) + '</td><td class="ap-nw">' + h(s.start_date) + '</td><td class="ap-nw">' + h(s.end_date) +
        '</td><td>' + h(s.responsible_role) + '</td><td>' + h(s.accountable_role) + '</td><td>' + h(s.deliverable) + '</td><td>' + aiBasis(s.basis) + '</td><td>' + aiRefs(s.cites) + '</td><td>' + aiUnresolved(s.unresolved) + '</td></tr>'));
    if (data.quarters && data.quarters.length) html += '<h4>Quarter dates</h4>' + aiTable(['Q', '1LoD window', 'Deadline', 'Selection', 'Fieldwork', 'Findings agreed', 'Report', 'Committee'],
      data.quarters.map(q => '<tr><td>Q' + q.quarter + '</td><td>' + h(q.lod1_window_start) + '</td><td>' + h(q.lod1_deadline) + '</td><td>' + h(q.selection_date) + '</td><td>' + h(q.fieldwork_start) + ' – ' + h(q.fieldwork_end) +
        '</td><td>' + h(q.findings_agreed_by) + '</td><td>' + h(q.report_date) + '</td><td>' + h(q.committee_label) + '</td></tr>'));
    const roles = data.raci_roles || [];
    if (data.raci && data.raci.length) html += '<h4>RACI</h4>' + aiTable(['Activity'].concat(roles),
      data.raci.map(r => '<tr><td>' + h(r.activity) + '</td>' + roles.map(role => '<td>' + h((r.cells || {})[role] || '') + '</td>').join('') + '</tr>'));
    if (open) {
      const parts = [['objective', data.objective, 'Replace the objective'], ['steps', (data.steps || []).length, 'Replace the steps (the generated quarterly steps stay)'],
        ['quarters', (data.quarters || []).length, 'Change these quarters\' dates (their quarterly steps follow)'], ['raci', (data.raci || []).length, 'Replace the RACI matrix']].filter(x => x[1]);
      html += '<p>' + parts.map(x => '<label class="ap-inline"><input type="checkbox" data-ai-part="' + x[0] + '" checked> ' + h(x[2]) + '</label>').join(' ') + '</p>';
    }
  } else if (pr.kind === 'attributes') {
    html += '<p class="muted">For: ' + h(data.focus) + ' · ' + (data.shortlist || []).length + ' shortlisted references</p>';
    html += aiTable([open ? 'Keep' : '', 'Ref', 'Domain', 'Requirement', 'Procedure', 'Method', 'Evidence requested', 'Pass criteria', 'Applies to', 'Basis', 'Cites', 'Unresolved', 'Why'],
      (data.attributes || []).map(a => '<tr><td>' + (open ? '<input type="checkbox" data-ai-attr="' + h(a.attr_ref) + '" checked>' : '') + '</td><td class="ap-nw">' + h(a.attr_ref) + '</td><td>' + h(a.domain) +
        '</td><td class="pre">' + h(a.requirement) + '</td><td class="pre">' + h(a.procedure) + '</td><td>' + h(a.method) + '</td><td class="pre">' + h(a.evidence_requested) + '</td><td class="pre">' + h(a.pass_criteria) +
        '</td><td>' + h((a.applies_to || []).join(', ') || 'All strata') + '</td><td>' + aiBasis(a.basis) + '</td><td>' + aiRefs(a.cites) + '</td><td>' + aiUnresolved(a.unresolved) + '</td><td class="pre">' + h(a.why) + '</td></tr>'));
  } else if (pr.kind === 'narrative') {
    html += '<h4>Executive summary</h4>' + (open ? '<textarea id="aiSummary" rows="8">' + h(data.executive_summary) + '</textarea>' : '<p class="pre">' + h(data.executive_summary) + '</p>');
    if (data.speaker_notes && data.speaker_notes.length) html += '<h4>Speaker notes</h4>' + aiTable(['Slide', 'Notes'],
      data.speaker_notes.map(n => '<tr><td class="ap-nw">' + n.slide + ' · ' + h(n.title) + '</td><td>' + (open ? '<textarea data-ai-slide="' + n.slide + '" data-ai-title="' + h(n.title) + '" rows="3">' + h(n.notes) + '</textarea>' : '<span class="pre">' + h(n.notes) + '</span>') + '</td></tr>'));
  }
  if (open) {
    html += '<label>Decision note<input id="aiNote" maxlength="4000" placeholder="Why you accept or reject it (optional)"></label>' +
      '<div class="toolbar"><button type="button" class="primary" id="aiAccept">' + (pr.kind === 'narrative' ? 'Accept with my edits' : 'Accept selected') + '</button><button type="button" class="danger" id="aiReject">Reject</button></div>';
  }
  aiHost.innerHTML = html;
  if (!open) return;
  document.getElementById('aiReject').addEventListener('click', () => aiDecide(pr, { decision: 'reject' }));
  document.getElementById('aiAccept').addEventListener('click', () => {
    const extra = { decision: 'accept' };
    if (pr.kind === 'draft') {
      extra.parts = [...aiHost.querySelectorAll('[data-ai-part]')].filter(x => x.checked).map(x => x.dataset.aiPart);
      if (!extra.parts.length) { say('Choose at least one part to apply, or reject the draft.', true); return; }
    } else if (pr.kind === 'attributes') {
      extra.attributes = [...aiHost.querySelectorAll('[data-ai-attr]')].filter(x => x.checked).map(x => x.dataset.aiAttr);
      if (!extra.attributes.length) { say('Keep at least one attribute, or reject the proposal.', true); return; }
    } else if (pr.kind === 'narrative') {
      extra.payload = Object.assign({}, pr.payload, { executive_summary: document.getElementById('aiSummary').value,
        speaker_notes: [...aiHost.querySelectorAll('[data-ai-slide]')].map(t => ({ slide: Number(t.dataset.aiSlide), title: t.dataset.aiTitle, notes: t.value })) });
    }
    aiDecide(pr, extra);
  });
}
document.querySelectorAll('[data-ai-review]').forEach(b => b.addEventListener('click', () => {
  const pr = AI.proposals.find(x => x.id === Number(b.dataset.aiReview));
  if (pr) { aiReview(pr); aiHost.scrollIntoView({ block: 'start' }); }
}));
document.querySelectorAll('[data-goto]').forEach(a => a.addEventListener('click', e => { e.preventDefault(); history.replaceState(null, '', '#' + a.dataset.goto); showTab(a.dataset.goto); }));
const aiOpen = Number(sessionStorage.getItem('apReview') || 0);
if (aiOpen) {
  sessionStorage.removeItem('apReview');
  const pr = AI.proposals.find(x => x.id === aiOpen);
  if (pr) { showTab('ai'); aiReview(pr); }
}
`
