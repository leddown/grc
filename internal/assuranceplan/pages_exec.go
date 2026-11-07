package assuranceplan

import (
	"fmt"
	"strconv"
	"strings"
)

// ---- test script ----

func attributesHTML(d planPageData) string {
	p := d.Plan
	var b strings.Builder
	b.WriteString(`<h2>Test script</h2>
<p class="lede">What 2LoD tests on every selected item: the requirement, the procedure and its method (NIST SP 800-53A: examine, interview,
test), the evidence requested and what passes. The attributes are copied onto the plan, so a later catalog edit does not change an
approved plan. Control ids and NFR keys are checked against the catalogs; one that does not resolve is marked, never presented as found.</p>`)
	b.WriteString(lockNotice(p))
	if len(p.Attributes) == 0 {
		b.WriteString(`<p class="muted">No test attributes yet.</p>`)
	} else {
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Ref</th><th>Domain</th><th>Requirement</th><th>Procedure</th><th>Method</th>
<th>Evidence requested</th><th>Pass criteria</th><th>Applies to</th><th>800-53</th><th>Regulatory basis</th><th>Source</th></tr></thead><tbody>`)
		for _, a := range p.Attributes {
			applies := "All strata"
			if len(a.AppliesTo) > 0 {
				applies = strings.Join(a.AppliesTo, ", ")
			}
			var ctl []string
			for _, c := range a.ControlIDs {
				if a.Checked && containsString(a.Unresolved, c) {
					ctl = append(ctl, esc(c)+` `+statusPill("Unresolved"))
				} else {
					ctl = append(ctl, esc(c))
				}
			}
			source := "Plan-local"
			if a.NFRKey != "" {
				source = "NFR " + esc(a.NFRKey)
				if a.Checked && containsString(a.Unresolved, "NFR "+a.NFRKey) {
					source += ` ` + statusPill("Unresolved")
				}
			}
			if !a.Checked {
				source += ` <span class="muted">(not checked)</span>`
			}
			fmt.Fprintf(&b, `<tr><td class="ap-nw">%s</td><td>%s</td><td class="pre">%s</td><td class="pre">%s</td><td>%s</td><td class="pre">%s</td><td class="pre">%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				esc(a.AttrRef), esc(a.Domain), esc(a.Requirement), esc(a.Procedure), esc(a.Method), esc(a.EvidenceRequested),
				esc(a.PassCriteria), esc(applies), strings.Join(ctl, "<br>"), esc(a.RegulatoryRefs), source)
		}
		b.WriteString(`</tbody></table></div>`)
	}
	if !p.DesignLocked {
		b.WriteString(`<h3>Edit the test script</h3>
<p class="muted">Applies to and 800-53 take comma-separated lists; leave Applies to blank for every stratum. An attribute with results cannot be removed,
or stop applying to a stratum it has results in.</p>
<div id="attrEditor"></div>
<div class="toolbar"><button type="button" class="primary" id="saveAttrs">Save test script</button></div>
<fieldset><legend>Copy from the catalogs</legend>
<div class="grid">
  <label>Security NFRs: catalog key or ID (comma or line separated)<textarea id="seedNFRs" placeholder="e.g. 0.1, 1.1 or NFR-0.1"></textarea></label>
  <label>800-53 control ids<textarea id="seedControls" placeholder="e.g. AC-2, AC-6, CM-8"></textarea></label>
</div>
<div class="toolbar"><button type="button" id="seedAttrs">Copy into the test script</button><span id="seedOut" class="muted"></span></div>
</fieldset>`)
	}
	return b.String()
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- tests ----

func testsHTML(d planPageData) string {
	p := d.Plan
	var b strings.Builder
	b.WriteString(`<h2>Tests</h2>
<p class="lede">One test per selected item. Evidence is due ` + strconv.Itoa(p.Parameters.EvidenceSLADays) + ` business days after it is requested.
A test's result is Effective with no fails, Partially effective from ` + strconv.Itoa(p.Parameters.ResultPartialMin) + ` fail(s) and Ineffective from ` +
		strconv.Itoa(p.Parameters.ResultIneffectiveMin) + `; N/A is not counted as tested. QA sign-off is four-eyes: the tester cannot sign off their own test
when more than one person can sign in, and a change after sign-off withdraws it.</p>`)
	canRun := p.Status == StatusApproved || p.Status == StatusActive
	if p.Status == StatusClosed {
		b.WriteString(`<p class="notice">The plan is closed: its tests no longer change.</p>`)
	}
	opened := map[int]int{}
	for _, t := range d.Tests {
		opened[t.Quarter]++
	}
	b.WriteString(`<div class="table-wrap"><table class="ap-nowrap"><thead><tr><th>Q</th><th class="num">Picks</th><th class="num">Tests opened</th><th>Open tests</th></tr></thead><tbody>`)
	for _, qs := range d.Sampling.Selection {
		action := ""
		if canRun && qs.SelectedAt != "" && qs.Picks > opened[qs.Quarter] {
			action = fmt.Sprintf(`<button type="button" class="mini primary" data-open-tests="%d">Open %d test(s) for Q%d</button>`, qs.Quarter, qs.Picks-opened[qs.Quarter], qs.Quarter)
		} else if qs.SelectedAt == "" {
			action = `<span class="muted">not selected yet</span>`
		}
		fmt.Fprintf(&b, `<tr><td>Q%d</td><td class="num">%d</td><td class="num">%d</td><td>%s</td></tr>`, qs.Quarter, qs.Picks, opened[qs.Quarter], action)
	}
	b.WriteString(`</tbody></table></div>`)
	if canRun {
		b.WriteString(`<div class="toolbar"><label>Tester for new tests<input id="newTester" placeholder="blank: you"></label>
<label>Evidence requested on<input type="date" id="newRequested"></label></div>`)
	}
	b.WriteString(`<div class="toolbar"><select id="testQ"><option value="">All quarters</option><option>1</option><option>2</option><option>3</option><option>4</option></select>
<select id="testState"><option value="">Any state</option><option value="pending">Results pending</option><option value="fail">With a fail</option>
<option value="qa">Awaiting QA</option><option value="late">Evidence late</option></select>
<input id="testFind" placeholder="Search test, item or tester"><span class="spacer"></span><span id="testCount" class="muted"></span></div>
<div class="table-wrap"><table class="ap-nowrap"><thead><tr><th>Test</th><th>Type</th><th>Item</th><th>Name</th><th>Stratum</th><th>Basis</th><th>Tester</th>
<th>Requested</th><th>Due</th><th>Received</th><th>Fieldwork</th><th class="num">Pass</th><th class="num">Fail</th><th class="num">N/A</th><th class="num">Pending</th>
<th>Result</th><th>QA</th><th></th></tr></thead><tbody id="testRows"></tbody></table></div>
<div id="testEdit" hidden></div>`)
	return b.String()
}

// ---- dashboard ----

func ragCell(c CoverageCell) string {
	return fmt.Sprintf(`%s%% / %s%% %s`, formatNumber(c.PlannedPct), formatNumber(c.ActualPct), statusPill(c.RAG))
}

func dashboardHTML(d planPageData) string {
	p, v := d.Plan, d.Dashboard
	var b strings.Builder
	open, overdue, raised := 0, 0, 0
	for _, s := range v.BySeverity {
		open += s.Open
		overdue += s.Overdue
		raised += s.Raised
	}
	selected, opened, fails := 0, 0, 0
	for _, r := range v.Testing {
		selected += r.Selected
		opened += r.Opened
	}
	for _, t := range d.Tests {
		fails += t.Fails
	}
	card := func(n, label string) string {
		return `<div class="card"><div class="n">` + esc(n) + `</div><div class="l">` + esc(label) + `</div></div>`
	}
	b.WriteString(`<div class="cards">`)
	b.WriteString(card(strconv.Itoa(v.TestsPlanned), "Tests planned"))
	b.WriteString(card(strconv.Itoa(selected), "Items selected"))
	b.WriteString(card(strconv.Itoa(opened), "Tests opened"))
	b.WriteString(card(strconv.Itoa(v.TestsDone), "Signed off by QA"))
	b.WriteString(card(strconv.Itoa(fails), "Attribute fails"))
	b.WriteString(card(strconv.Itoa(raised), "Findings raised"))
	b.WriteString(card(strconv.Itoa(open), "Findings open"))
	b.WriteString(card(strconv.Itoa(overdue), "Findings overdue"))
	b.WriteString(`</div>`)

	word := coverageWord(p.CoverageBasis)
	heading, last, legend := "1LoD coverage against plan", "DQ accepted",
		"The last two columns are the year-end objective: every item of every stratum attested by 1LoD and its data quality accepted by 2LoD."
	if p.CoverageBasis == CoverageTests {
		heading, last, legend = "Test coverage against plan", "In cycle",
			"The year-end objective is every item due in the period tested and signed off by QA."
	}
	b.WriteString(`<h2>` + heading + `</h2>
<p class="lede">` + esc(coverageMethod(p.CoverageBasis, hasCycle(p.Strata))) + ` Each cell is the cumulative target against the share of the stratum ` + word + ` by the quarter's end, then its RAG: Green on or
above plan, Amber within ` + formatNumber(p.Parameters.RAGAmberPoints) + ` points below, Red further below, Not due before the quarter ends.</p>
<div class="table-wrap"><table class="ap-nowrap"><thead><tr><th>Stratum</th><th class="num">N</th><th>Q1</th><th>Q2</th><th>Q3</th><th>Q4</th>
<th class="num">` + upperFirst(word) + `</th><th class="num">` + last + `</th></tr></thead><tbody>`)
	for _, r := range v.Coverage {
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td>`, esc(r.Name), r.N)
		for _, c := range r.Cells {
			fmt.Fprintf(&b, `<td title="to %s: %d covered against %d planned">%s</td>`, esc(displayDate(c.QuarterEnd)), c.Covered, c.Planned, ragCell(c))
		}
		if p.CoverageBasis == CoverageTests {
			fmt.Fprintf(&b, `<td class="num">%d</td><td class="num">%d</td></tr>`, r.Tested, r.InCycle)
			continue
		}
		fmt.Fprintf(&b, `<td class="num">%d (%s%%)</td><td class="num">%d (%s%%)</td></tr>`, r.Attested, formatNumber(r.AttestedPct), r.DQAccepted, formatNumber(r.DQPct))
	}
	b.WriteString(`</tbody></table></div>
<p class="ap-legend">` + legend + `</p>`)

	b.WriteString(`<h2>2LoD testing progress</h2><div class="table-wrap"><table class="ap-nowrap"><thead><tr><th>Q</th><th class="num">Planned</th>
<th class="num">Selected</th><th class="num">Opened</th><th class="num">Fieldwork done</th><th class="num">QA signed off</th><th class="num">Evidence late</th>`)
	for _, o := range OverallResults {
		b.WriteString(`<th class="num">` + esc(o) + `</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, r := range v.Testing {
		fmt.Fprintf(&b, `<tr><td>Q%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td>`,
			r.Quarter, r.Planned, r.Selected, r.Opened, r.Fieldwork, r.QA, r.EvidenceLate)
		for _, o := range OverallResults {
			fmt.Fprintf(&b, `<td class="num">%d</td>`, r.ByResult[o])
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></div>`)

	b.WriteString(`<h2>Deviations by attribute and stratum</h2>
<p class="lede">Cumulative for the year: no deviations is effective; from ` + strconv.Itoa(p.Parameters.DeviationExtendAt) + ` the sample is extended by ` +
		formatNumber(p.Parameters.ExtendPct) + `% of the next quarter's tests; from ` + strconv.Itoa(p.Parameters.DeviationThematicAt) + ` the attribute is not effective
for the stratum, a thematic finding that can go to the risk register. Every individual fail is still its own finding.</p>`)
	if len(v.Deviations) == 0 {
		b.WriteString(`<p class="muted">No attributes to conclude on yet.</p>`)
	} else {
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Attribute</th><th>Domain</th><th>Stratum</th><th class="num">Tested</th>
<th class="num">Deviations</th><th>Conclusion</th><th>Fails</th><th>Next step</th></tr></thead><tbody>`)
		canPush := p.Status == StatusApproved || p.Status == StatusActive
		for _, dv := range v.Deviations {
			next := ""
			switch {
			case dv.Thematic != nil:
				day, _, _ := strings.Cut(dv.Thematic.PushedAt, "T")
				next = `Risk ` + esc(dv.Thematic.RiskRef) + ` raised ` + esc(displayDate(day))
			case dv.Kind == KindNotEffective && canPush:
				next = fmt.Sprintf(`<button type="button" class="mini" data-push="%d" data-stratum="%s">Push to risk register</button>`, dv.AttributeID, esc(dv.StratumCode))
			case dv.Kind == KindExtend && dv.FullCoverage:
				next = `Full coverage: raise each fail as a finding`
			case dv.Kind == KindExtend && dv.ExtendQuarter > 0:
				next = fmt.Sprintf(`Extend: +%d test(s) of %s in Q%d`, dv.ExtendBy, esc(dv.AttrRef), dv.ExtendQuarter)
			case dv.Kind == KindExtend:
				next = `Extend: no later quarter this year; carry into next year's sample`
			}
			fmt.Fprintf(&b, `<tr><td class="ap-nw">%s</td><td>%s</td><td>%s</td><td class="num">%d</td><td class="num">%d</td><td><span class="ap-st ap-st-%s">%s</span></td><td>%s</td><td>%s</td></tr>`,
				esc(dv.AttrRef), esc(dv.Domain), esc(dv.StratumCode), dv.Tested, dv.Fails, dv.Kind, esc(dv.Conclusion),
				esc(strings.Join(dv.Failures, ", ")), next)
		}
		b.WriteString(`</tbody></table></div>
<div id="pushForm" hidden><fieldset><legend>Push to the risk register</legend><p id="pushWhat" class="muted"></p>
<div class="toolbar"><label>Risk owner<input id="pushOwner"></label>
<label>Likelihood (1–5)<select id="pushL"><option></option><option>1</option><option>2</option><option>3</option><option>4</option><option>5</option></select></label>
<label>Impact (1–5)<select id="pushI"><option></option><option>1</option><option>2</option><option>3</option><option>4</option><option>5</option></select></label>
<button type="button" class="primary" id="pushGo">Create the risk</button><button type="button" id="pushCancel">Cancel</button></div>
<p class="muted">The residual rating starts equal to the inherent one; the risk's owner assesses what the controls are worth.</p></fieldset></div>`)
	}

	b.WriteString(`<h2>Findings raised from the plan</h2>`)
	b.WriteString(`<div class="table-wrap"><table class="ap-nowrap"><thead><tr><th>Severity</th><th class="num">Raised</th><th class="num">Draft</th>
<th class="num">Open</th><th class="num">Overdue</th><th class="num">Closed or accepted</th></tr></thead><tbody>`)
	for _, s := range v.BySeverity {
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td></tr>`,
			statusPill(s.Severity), s.Raised, s.Draft, s.Open, s.Overdue, s.Closed)
	}
	b.WriteString(`</tbody></table></div>`)
	if v.Missing > 0 {
		fmt.Fprintf(&b, `<p class="notice warn">%d linked finding(s) are no longer in Audit Findings; their results can raise a finding again.</p>`, v.Missing)
	}
	if len(v.Findings) > 0 {
		b.WriteString(`<details><summary>` + strconv.Itoa(len(v.Findings)) + ` linked finding(s)</summary><div class="table-wrap"><table><thead><tr><th>Finding</th><th>Title</th><th>Severity</th>
<th>Status</th><th>Due</th><th>Test</th><th>Attribute</th><th>Item</th></tr></thead><tbody>`)
		for _, f := range v.Findings {
			status := statusPill(f.Status)
			if f.Missing {
				status = statusPill("Missing")
			} else if f.Overdue {
				status += ` ` + statusPill("Overdue")
			}
			fmt.Fprintf(&b, `<tr><td><a href="/audit-findings#%d">%s</a></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				f.FindingID, esc(f.Reference), esc(f.Title), statusPill(f.Severity), status, esc(displayDate(f.DueDate)), esc(f.TestRef), esc(f.AttrRef), esc(f.ItemRef))
		}
		b.WriteString(`</tbody></table></div></details>`)
	}

	b.WriteString(`<h2>Utilisation</h2><div class="table-wrap"><table class="ap-nowrap"><thead><tr><th>Q</th><th class="num">Effort (person-days)</th>
<th class="num">Capacity</th><th class="num">Utilisation</th><th>Against ` + pct(v.Threshold) + `</th></tr></thead><tbody>`)
	for _, l := range v.Quarters {
		fmt.Fprintf(&b, `<tr><td>Q%d</td><td class="num">%s</td><td class="num">%s</td><td class="num">%s</td><td>%s</td></tr>`,
			l.Quarter, formatNumber(l.Effort), formatNumber(l.Capacity), pct(l.Utilisation), utilisationWord(l))
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

// execScript drives the test script editor, the tests and the thematic push.
const execScript = `
const TESTS = JSON.parse(document.getElementById('tests-data').textContent);
const listOf = s => String(s || '').split(/[,;\n]/).map(x => x.trim()).filter(Boolean);

const attrHost = document.getElementById('attrEditor');
if (attrHost) {
  const ed = editor(attrHost, [
    { key: 'attr_ref', label: 'Ref', width: '70px' },
    { key: 'domain', label: 'Domain', width: '130px' },
    { key: 'requirement', label: 'Requirement', type: 'textarea', width: '200px' },
    { key: 'procedure', label: 'Procedure', type: 'textarea', width: '200px' },
    { key: 'method', label: 'Method', type: 'select', options: VOCAB.methods },
    { key: 'evidence_requested', label: 'Evidence requested', type: 'textarea', width: '160px' },
    { key: 'pass_criteria', label: 'Pass criteria', type: 'textarea', width: '180px' },
    { key: 'applies_to', label: 'Applies to', width: '70px' },
    { key: 'control_ids', label: '800-53', width: '90px' },
    { key: 'regulatory_refs', label: 'Regulatory basis', width: '120px' },
    { key: 'nfr_key', label: 'NFR key', width: '70px' },
  ], PLAN.attributes.map(a => Object.assign({}, a, { applies_to: (a.applies_to || []).join(', '), control_ids: (a.control_ids || []).join(', ') })),
  () => ({ method: VOCAB.methods[0] }));
  document.getElementById('saveAttrs').addEventListener('click', () => {
    const rows = ed.rows().map(r => Object.assign({}, r, { applies_to: listOf(r.applies_to), control_ids: listOf(r.control_ids) }));
    act('PUT', base + '/attributes', rows);
  });
  document.getElementById('seedAttrs').addEventListener('click', () => {
    const body = { nfr_keys: listOf(document.getElementById('seedNFRs').value), control_ids: listOf(document.getElementById('seedControls').value) };
    act('POST', base + '/attributes/seed', body, r => {
      const parts = [];
      if (r.added.length) parts.push('added ' + r.added.join(', '));
      if (r.skipped.length) parts.push('already there: ' + r.skipped.join(', '));
      if (r.not_found.length) parts.push('not in the catalogs: ' + r.not_found.join(', '));
      say(parts.join('; ') || 'Nothing to add.');
      if (r.added.length) setTimeout(() => location.reload(), 900);
    });
  });
}

document.querySelectorAll('[data-open-tests]').forEach(b => b.addEventListener('click', () => {
  act('POST', base + '/tests', { quarter: Number(b.dataset.openTests), tester: document.getElementById('newTester').value,
    evidence_requested_on: document.getElementById('newRequested').value });
}));

const testRows = document.getElementById('testRows');
const testFilters = ['testQ', 'testState', 'testFind'].map(id => document.getElementById(id));
function testState(t) {
  if (t.qa_date) return pill('Signed off');
  if (t.fieldwork_completed_on && !t.pending) return pill('Awaiting QA');
  return '';
}
function drawTests() {
  if (!testRows) return;
  const [q, st, find] = testFilters.map(f => f.value);
  const needle = find.trim().toLowerCase();
  const rows = TESTS.filter(t => (!q || t.quarter === Number(q))
    && (!st || (st === 'pending' && t.pending > 0) || (st === 'fail' && t.fails > 0) || (st === 'qa' && !t.qa_date && t.fieldwork_completed_on) || (st === 'late' && t.evidence_late))
    && (!needle || [t.test_ref, t.test_type, t.item_ref, t.item_name, t.tester].join(' ').toLowerCase().includes(needle)));
  document.getElementById('testCount').textContent = rows.length + ' of ' + TESTS.length + ' tests';
  testRows.innerHTML = rows.map(t => '<tr><td>' + h(t.test_ref) + '</td><td>' + h(t.test_type) + '</td><td>' + h(t.item_ref) + '</td><td>' + h(t.item_name) + '</td><td>' + h(t.stratum) +
    '</td><td>' + pill(t.basis) + '</td><td>' + h(t.tester) + '</td><td>' + h(t.evidence_requested_on) + '</td><td>' + h(t.evidence_due_on) +
    '</td><td>' + h(t.evidence_received_on) + (t.evidence_late ? ' ' + pill('Evidence late') : '') + '</td><td>' + h(t.fieldwork_completed_on) +
    '</td><td class="num">' + t.passes + '</td><td class="num">' + t.fails + '</td><td class="num">' + t.not_applicable + '</td><td class="num">' + t.pending +
    '</td><td>' + pill(t.overall) + '</td><td>' + testState(t) + '</td><td><button type="button" class="link-btn" data-test="' + t.id + '">Open</button></td></tr>').join('')
    || '<tr><td colspan="18" class="muted">No tests.</td></tr>';
  testRows.querySelectorAll('[data-test]').forEach(b => b.addEventListener('click', () => openTest(TESTS.find(t => t.id === Number(b.dataset.test)))));
}
testFilters.forEach(f => f && f.addEventListener('input', drawTests));
drawTests();

const testEdit = document.getElementById('testEdit');
function openTest(t) {
  const results = {};
  (t.results || []).forEach(r => { results[r.attribute_id] = r; });
  const attrs = PLAN.attributes.filter(a => !a.applies_to.length || a.applies_to.includes(t.stratum));
  const running = PLAN.status === 'Approved' || PLAN.status === 'Active';
  const dis = running ? '' : ' disabled';
  const opt = (v, cur) => '<option value="' + h(v) + '"' + (v === cur ? ' selected' : '') + '>' + h(v || '—') + '</option>';
  testEdit.innerHTML = '<h3>' + h(t.test_ref) + ' · ' + h(t.item_ref) + ' ' + h(t.item_name) + ' · ' + h(t.stratum) + ' · ' + pill(t.basis) + ' · ' + pill(t.overall) + '</h3>' +
    (t.qa_date ? '<p class="notice">Signed off by ' + h(t.qa_reviewer || '—') + ' on ' + h(t.qa_date) + (t.qa_note ? ' — ' + h(t.qa_note) : '') + '. A change withdraws the sign-off.</p>' : '') +
    '<div class="grid">' + (PLAN.test_types.length ? '<label>Test type<select data-t="test_type"' + dis + '>' +
      PLAN.test_types.concat(t.test_type && !PLAN.test_types.includes(t.test_type) ? [t.test_type] : []).map(v => opt(v, t.test_type)).join('') + '</select></label>' : '') +
    '<label>Tester<input data-t="tester" value="' + h(t.tester) + '"' + dis + '></label>' +
    '<label>Evidence requested<input type="date" data-t="evidence_requested_on" value="' + h(t.evidence_requested_on) + '"' + dis + '></label>' +
    '<label>Evidence due (requested + ' + PLAN.parameters.evidence_sla_days + ' business days)<input value="' + h(t.evidence_due_on) + '" disabled></label>' +
    '<label>Evidence received<input type="date" data-t="evidence_received_on" value="' + h(t.evidence_received_on) + '"' + dis + '></label>' +
    '<label>Fieldwork completed<input type="date" data-t="fieldwork_completed_on" value="' + h(t.fieldwork_completed_on) + '"' + dis + '></label></div>' +
    '<div class="grid wide"><label>Evidence summary (the condition a finding records)<textarea data-t="evidence_summary"' + dis + '>' + h(t.evidence_summary) + '</textarea></label>' +
    '<label>Notes<textarea data-t="notes"' + dis + '>' + h(t.notes) + '</textarea></label></div>' +
    '<div class="table-wrap"><table><thead><tr><th>Attribute</th><th>Domain</th><th>Pass criteria</th><th>Result</th><th>Note</th><th>Finding</th></tr></thead><tbody>' +
    attrs.map(a => {
      const r = results[a.id] || {};
      let finding = '';
      if (r.finding_id) finding = '<a href="/audit-findings#' + r.finding_id + '">' + h(r.finding_ref) + '</a> ' + pill(r.severity);
      else if (r.id && r.result === 'Fail' && running) finding = '<button type="button" class="mini" data-raise="' + r.id + '">Raise finding</button>';
      return '<tr data-attr="' + a.id + '"><td class="ap-nw">' + h(a.attr_ref) + '</td><td>' + h(a.domain) + '</td><td class="pre" style="max-width:340px">' + h(a.pass_criteria) + '</td>' +
        '<td><select data-r="result"' + dis + '>' + ['', 'Pass', 'Fail', 'N/A'].map(v => opt(v, r.result || '')).join('') + '</select></td>' +
        '<td><input data-r="note" value="' + h(r.note) + '"' + dis + ' style="min-width:220px"></td><td>' + finding + '</td></tr>';
    }).join('') + '</tbody></table></div>' +
    '<div id="raiseForm" hidden><fieldset><legend>Raise a finding in Audit Findings</legend><div class="toolbar">' +
    '<label>Severity<select id="raiseSev"><option></option>' + VOCAB.severities.map(s => '<option>' + h(s) + '</option>').join('') + '</select></label>' +
    '<label>Owner<input id="raiseOwner" value="' + h(t.item_owner) + '"></label>' +
    '<label>Status<select id="raiseStatus"><option>Open</option><option>Draft</option></select></label>' +
    '<label>Due date (blank: the severity\'s SLA)<input type="date" id="raiseDue"></label>' +
    '<button type="button" class="primary" id="raiseGo">Raise</button><button type="button" id="raiseCancel">Cancel</button></div>' +
    '<p class="muted">The finding carries the attribute as criteria, the evidence summary and the result note as condition, the 800-53 ids, ' +
    'deficiency type Operating Effectiveness, source Second Line Review, and this plan, test and attribute as its origin.</p></fieldset></div>' +
    (running ? '<div class="toolbar"><button type="button" class="primary" id="saveTest">Save test</button>' +
      (!t.qa_date ? '<button type="button" id="qaTest">QA sign-off</button>' : '') + '<button type="button" id="closeTest">Close</button></div>' : '');
  testEdit.hidden = false;
  testEdit.scrollIntoView({ behavior: 'smooth', block: 'start' });
  const close = document.getElementById('closeTest');
  if (close) close.addEventListener('click', () => { testEdit.hidden = true; });
  const save = document.getElementById('saveTest');
  if (save) save.addEventListener('click', () => {
    const body = { results: [] };
    testEdit.querySelectorAll('[data-t]').forEach(el => { body[el.dataset.t] = el.value; });
    testEdit.querySelectorAll('tr[data-attr]').forEach(tr => {
      body.results.push({ attribute_id: Number(tr.dataset.attr), result: tr.querySelector('[data-r=result]').value, note: tr.querySelector('[data-r=note]').value });
    });
    act('PUT', base + '/tests/' + t.id, body, r => {
      say('Saved ' + t.test_ref + (r.changes ? ': ' + r.changes : ': no change') + (r.qa_withdrawn ? '. QA sign-off withdrawn.' : '.'));
      setTimeout(() => location.reload(), 900);
    });
  });
  const qa = document.getElementById('qaTest');
  if (qa) qa.addEventListener('click', () => act('POST', base + '/tests/' + t.id + '/qa', {}, r => {
    say('Signed off ' + t.test_ref + (r.warning ? '. ' + r.warning : '.'));
    setTimeout(() => location.reload(), 1400);
  }));
  let raiseFor = 0;
  testEdit.querySelectorAll('[data-raise]').forEach(b => b.addEventListener('click', () => {
    raiseFor = Number(b.dataset.raise);
    document.getElementById('raiseForm').hidden = false;
  }));
  document.getElementById('raiseCancel').addEventListener('click', () => { document.getElementById('raiseForm').hidden = true; });
  document.getElementById('raiseGo').addEventListener('click', () => {
    act('POST', base + '/results/' + raiseFor + '/finding', { severity: document.getElementById('raiseSev').value,
      owner: document.getElementById('raiseOwner').value, status: document.getElementById('raiseStatus').value,
      due_date: document.getElementById('raiseDue').value }, r => {
      say('Raised ' + r.finding.reference + ' (' + r.finding.severity + ', due ' + (r.finding.due_date || '—') + ').');
      setTimeout(() => location.reload(), 1200);
    });
  });
}

let pushing = null;
document.querySelectorAll('[data-push]').forEach(b => b.addEventListener('click', () => {
  pushing = { attribute_id: Number(b.dataset.push), stratum: b.dataset.stratum };
  const a = PLAN.attributes.find(x => x.id === pushing.attribute_id) || {};
  document.getElementById('pushWhat').textContent = (a.attr_ref || '') + ' ' + (a.domain || '') + ', stratum ' + pushing.stratum + ': not effective.';
  document.getElementById('pushForm').hidden = false;
}));
const pushGo = document.getElementById('pushGo');
if (pushGo) {
  pushGo.addEventListener('click', () => act('POST', base + '/thematic', Object.assign({}, pushing, {
    owner: document.getElementById('pushOwner').value, likelihood: Number(document.getElementById('pushL').value || 0),
    impact: Number(document.getElementById('pushI').value || 0) })));
  document.getElementById('pushCancel').addEventListener('click', () => { document.getElementById('pushForm').hidden = true; });
}
`
