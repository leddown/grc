package controlcatalog

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// The Control Editor's OSCAL import: a panel with a file picker for the
// catalog and one per baseline profile, a preview, and an apply that is only
// offered for a preview without problems. The browser reads the files and posts
// their text in one JSON body.

const oscalImportCSS = `
    .oscal-panel { border: 1px solid var(--border); background: var(--surface-2); border-radius: 6px; padding: 8px 10px; margin: 0 0 12px; font-family: Arial, sans-serif; font-size: 13px; }
    .oscal-panel p { margin: 4px 0; }
    .oscal-files { display: flex; flex-wrap: wrap; gap: 6px 14px; margin: 6px 0; }
    .oscal-files label { display: inline-flex; gap: 6px; align-items: center; color: var(--muted); white-space: nowrap; }
    .oscal-files input[type=file] { width: auto; padding: 2px 4px; border-radius: 4px; }
    .oscal-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin: 6px 0; }
    .oscal-actions button { padding: 4px 12px; }
    .oscal-result h3 { font-size: 13px; margin: 10px 0 4px; }
    .oscal-result ul { margin: 2px 0; padding-left: 18px; }
    .oscal-result .table-wrap { max-height: 320px; overflow: auto; border: 1px solid var(--border); }
    .oscal-result table { width: 100%; }
    .oscal-result td, .oscal-result th { text-align: left; vertical-align: top; padding: 3px 6px; }
    .oscal-result details summary { cursor: pointer; }
    .oscal-diff { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; margin: 4px 0; }
    .oscal-diff pre { white-space: pre-wrap; margin: 0; padding: 4px 6px; border: 1px solid var(--border); font-size: 12px; max-height: 240px; overflow: auto; }
    .oscal-muted { color: var(--muted); }
`

const oscalImportPanelHTML = `
    <section id="oscalPanel" class="oscal-panel" hidden>
      <p><strong>Import an OSCAL catalog.</strong> <span class="oscal-muted">NIST SP 800-53 Rev. 5 in OSCAL JSON (usnistgov/oscal-content, nist.gov/SP800-53/rev5/json). The catalog is required; a baseline whose profile is not chosen is left as it is. Nothing is written until you apply. An OSCAL import never deletes a control, and never changes the CIA flags and justification, threats, potentially common/inheritable, the appendix or the record key.</span></p>
      <div class="oscal-files">
        <label>Catalog <input id="oscalCatalog" type="file" accept=".json,application/json"></label>
        <label>LOW profile <input class="oscal-profile" data-slot="low" type="file" accept=".json,application/json"></label>
        <label>MODERATE profile <input class="oscal-profile" data-slot="moderate" type="file" accept=".json,application/json"></label>
        <label>HIGH profile <input class="oscal-profile" data-slot="high" type="file" accept=".json,application/json"></label>
        <label>PRIVACY profile <input class="oscal-profile" data-slot="privacy" type="file" accept=".json,application/json"></label>
      </div>
      <div class="oscal-actions">
        <button id="oscalPreviewBtn" type="button">Preview</button>
        <button id="oscalApplyBtn" type="button" disabled title="Preview first; apply is offered for a preview without problems">Apply</button>
        <button id="oscalCloseBtn" class="secondary" type="button">Close</button>
        <span id="oscalStatus" class="status"></span>
      </div>
      <div id="oscalResult" class="oscal-result"></div>
    </section>
`

const oscalImportScript = `
  <script>
  (function () {
    const panel = document.getElementById('oscalPanel');
    const openBtn = document.getElementById('oscalBtn');
    const catalogInput = document.getElementById('oscalCatalog');
    const profileInputs = Array.from(document.querySelectorAll('.oscal-profile'));
    const previewBtn = document.getElementById('oscalPreviewBtn');
    const applyBtn = document.getElementById('oscalApplyBtn');
    const statusEl = document.getElementById('oscalStatus');
    const resultEl = document.getElementById('oscalResult');
    let previewedBase = null;
    let lastChanged = [];

    function esc(value) {
      return String(value == null ? '' : value)
        .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;').replaceAll("'", '&#39;');
    }
    function say(text, kind) { statusEl.textContent = text; statusEl.className = 'status ' + (kind || ''); }
    function invalidate() { previewedBase = null; applyBtn.disabled = true; }
    async function body(base) {
      const file = catalogInput.files[0];
      if (!file) throw new Error('Choose the OSCAL catalog file.');
      const out = { catalog: await file.text(), profiles: {} };
      for (const input of profileInputs) {
        if (input.files[0]) out.profiles[input.dataset.slot] = await input.files[0].text();
      }
      if (base) out.base = base;
      return out;
    }
    async function post(url, payload) {
      const resp = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
      let data = {};
      try { data = await resp.json(); } catch (e) { data = {}; }
      if (!resp.ok) { const err = new Error(data.error || ('HTTP ' + resp.status)); err.status = resp.status; err.data = data; throw err; }
      return data;
    }
    function source(label, s) {
      const mb = !s.bytes ? '' : s.bytes >= 1048576 ? (s.bytes / 1048576).toFixed(1) + ' MB' : Math.max(1, Math.round(s.bytes / 1024)) + ' KB';
      return '<li>' + esc(label) + ': ' + esc(s.title || '(no title)') + ' · version ' + esc(s.version || '—') + ' · OSCAL ' + esc(s.oscal_version || '—') +
        ' · last modified ' + esc(s.last_modified || '—') + ' · ' + esc(mb) + ' · SHA-256 <code>' + esc(s.sha256) + '</code></li>';
    }
    function idList(ids) {
      if (!ids.length) return '<span class="oscal-muted">none</span>';
      return esc(ids.join(', '));
    }
    function changedRows(filter) {
      const rows = lastChanged.map((c, i) => ({ c, i })).filter(({ c }) => !filter || c.fields.some(f => f.field === filter));
      if (!rows.length) return '<tr><td colspan="3" class="oscal-muted">No changed control has that field.</td></tr>';
      return rows.slice(0, 2000).map(({ c, i }) => {
        const names = c.fields.map(f => esc(f.field) + (f.whitespace_only ? ' (whitespace only)' : '')).join(', ');
        return '<tr><td>' + esc(c.id) + '</td><td>' + esc(c.name) + '</td><td><details data-i="' + i + '"><summary>' + names + '</summary><div class="oscal-detail"></div></details></td></tr>';
      }).join('');
    }
    function fillDetail(details) {
      const host = details.querySelector('.oscal-detail');
      if (host.dataset.filled) return;
      host.dataset.filled = '1';
      const change = lastChanged[Number(details.dataset.i)];
      host.innerHTML = change.fields.map(f => {
        if (f.before === undefined && f.after === undefined) return '<p class="oscal-muted">' + esc(f.field) + ': the stored OSCAL structure differs from the file.</p>';
        return '<p><strong>' + esc(f.field) + '</strong></p><div class="oscal-diff"><pre>' + esc(f.before || '(empty)') + '</pre><pre>' + esc(f.after || '(empty)') + '</pre></div>';
      }).join('');
    }
    function show(d) {
      lastChanged = d.changed || [];
      let html = '<h3>Files</h3><ul>' + source('Catalog', d.source);
      for (const slot of ['low', 'moderate', 'high', 'privacy']) {
        if (d.profiles && d.profiles[slot]) html += source(slot.toUpperCase() + ' profile', d.profiles[slot]);
      }
      html += '</ul>';
      if (d.problems.length) {
        html += '<h3 class="status error">' + d.problems.length + ' problem(s): nothing can be applied until they are fixed</h3><ul>' +
          d.problems.map(p => '<li>' + esc(p) + '</li>').join('') + '</ul>';
      }
      html += '<ul class="oscal-muted">' + d.notes.map(n => '<li>' + esc(n) + '</li>').join('') + '</ul>';
      html += '<h3>' + d.added.length + ' added · ' + d.changed.length + ' changed · ' + d.unchanged.length + ' unchanged · ' + d.absent.length + ' absent from the file · ' + d.withdrawn + ' withdrawn in the file</h3>';
      const counts = Object.entries(d.field_counts || {}).sort((a, b) => b[1] - a[1]);
      if (counts.length) html += '<p class="oscal-muted">Changes by field: ' + counts.map(([k, v]) => esc(k) + ' ' + v).join(' · ') + '</p>';
      if (d.added.length) {
        html += '<h3>Added (' + d.added.length + ')</h3><div class="table-wrap"><table><thead><tr><th>Control</th><th>Type</th><th>Name</th></tr></thead><tbody>' +
          d.added.map(a => '<tr><td>' + esc(a.id) + '</td><td>' + esc(a.type) + '</td><td>' + esc(a.name) + '</td></tr>').join('') + '</tbody></table></div>';
      }
      if (d.changed.length) {
        html += '<h3>Changed (' + d.changed.length + ') <select id="oscalFieldFilter"><option value="">every field</option>' +
          counts.map(([k]) => '<option value="' + esc(k) + '">' + esc(k) + '</option>').join('') + '</select></h3>' +
          '<p class="oscal-muted">Open a row to compare: the catalog now on the left, the file on the right.</p>' +
          '<div class="table-wrap"><table><thead><tr><th>Control</th><th>Name</th><th>Changes</th></tr></thead><tbody id="oscalChangedRows">' + changedRows('') + '</tbody></table></div>';
      }
      html += '<details><summary>Unchanged (' + d.unchanged.length + ')</summary><p>' + idList(d.unchanged) + '</p></details>';
      html += '<h3>Absent from the file (' + d.absent.length + ')</h3><p>' + idList(d.absent) + '</p>';
      resultEl.innerHTML = html;
      const filter = document.getElementById('oscalFieldFilter');
      if (filter) filter.addEventListener('change', () => { document.getElementById('oscalChangedRows').innerHTML = changedRows(filter.value); });
    }
    resultEl.addEventListener('toggle', (event) => {
      const details = event.target;
      if (details.tagName === 'DETAILS' && details.dataset.i !== undefined && details.open) fillDetail(details);
    }, true);

    openBtn.addEventListener('click', () => { panel.hidden = !panel.hidden; });
    document.getElementById('oscalCloseBtn').addEventListener('click', () => { panel.hidden = true; });
    catalogInput.addEventListener('change', invalidate);
    profileInputs.forEach(input => input.addEventListener('change', invalidate));

    previewBtn.addEventListener('click', async () => {
      invalidate();
      try {
        say('Reading the files and working out the preview…', 'warn');
        const d = await post('/controls/import-oscal/preview', await body());
        show(d);
        const changes = d.added.length + d.changed.length;
        if (d.problems.length) { say('The files have problems: see the list.', 'error'); return; }
        if (!changes) { say('Nothing would change.', 'success'); return; }
        previewedBase = d.base;
        applyBtn.disabled = false;
        say('Preview ready: ' + d.added.length + ' added, ' + d.changed.length + ' changed. Apply to write it.', 'success');
      } catch (err) { say('Preview failed: ' + err.message, 'error'); }
    });

    applyBtn.addEventListener('click', async () => {
      if (!previewedBase) return;
      applyBtn.disabled = true;
      try {
        say('Applying…', 'warn');
        const d = await post('/controls/import-oscal', await body(previewedBase));
        show(d);
        previewedBase = null;
        say('Applied: ' + d.added.length + ' added, ' + d.changed.length + ' changed' + (d.links_rebuilt ? '. NFR-control links rebuilt.' : '.'), 'success');
        if (typeof loadControls === 'function') await loadControls();
      } catch (err) {
        if (err.status === 409) { invalidate(); say(err.message, 'error'); return; }
        applyBtn.disabled = false;
        say('Apply failed: ' + err.message, 'error');
      }
    });
  })();
  </script>
`

// detailOSCALSections renders the upstream structure an OSCAL import stored
// for one control: its parameters (the organization-defined parameters, ODPs)
// and its 800-53A assessment objectives and methods. A control never imported
// from OSCAL shows neither.
func detailOSCALSections(control Control) string {
	params := decodeOSCALParams(control.Params)
	parts := decodeOSCALParts(control.Assessment)
	if len(params) == 0 && len(parts) == 0 {
		return ""
	}
	labels := map[string]string{}
	for _, p := range params {
		labels[p.ID] = p.display()
	}
	var b strings.Builder
	if len(params) > 0 {
		b.WriteString(`<article class="card oscal-card"><h2>Parameters (ODPs) · ` + strconv.Itoa(len(params)) + `</h2><div class="table-wrap"><table><thead><tr><th>ODP</th><th>Asks for</th><th>Guideline</th></tr></thead><tbody>`)
		for _, p := range params {
			asks := p.asks(labels)
			if aggregates := p.propValues("aggregates"); len(aggregates) > 0 {
				names := make([]string, 0, len(aggregates))
				for _, a := range aggregates {
					if l, ok := labels[a]; ok {
						names = append(names, l)
					} else {
						names = append(names, a)
					}
				}
				asks += " (aggregates " + strings.Join(names, ", ") + ")"
			}
			guidelines := make([]string, 0, len(p.Guidelines))
			for _, g := range p.Guidelines {
				guidelines = append(guidelines, oscalInline(g.Prose, labels))
			}
			b.WriteString(`<tr><td>` + detailHTMLEscape(p.display()) + `</td><td>` + detailHTMLEscape(asks) + `</td><td>` + detailHTMLEscape(strings.Join(guidelines, " ")) + `</td></tr>`)
		}
		b.WriteString(`</tbody></table></div></article>`)
	}
	var objectives, methods []oscalPartView
	for _, p := range parts {
		switch p.Name {
		case "assessment-objective":
			objectives = append(objectives, p)
		case "assessment-method":
			methods = append(methods, p)
		}
	}
	if len(objectives) > 0 || len(methods) > 0 {
		count := 0
		for _, o := range objectives {
			count += o.leaves()
		}
		b.WriteString(`<article class="card oscal-card"><details><summary><h2 style="display:inline">Assessment objectives (800-53A) · ` + strconv.Itoa(count) + `</h2></summary>`)
		for _, o := range objectives {
			b.WriteString(o.objectiveHTML(labels))
		}
		if len(methods) > 0 {
			b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Method</th><th>Objects</th></tr></thead><tbody>`)
			for _, m := range methods {
				var objects []string
				for _, child := range m.Parts {
					if child.Name == "assessment-objects" {
						objects = append(objects, strings.Join(strings.Fields(strings.ReplaceAll(child.Prose, "\n\n", "; ")), " "))
					}
				}
				b.WriteString(`<tr><td>` + detailHTMLEscape(m.propValue("method", "")) + `</td><td>` + detailHTMLEscape(strings.Join(objects, "; ")) + `</td></tr>`)
			}
			b.WriteString(`</tbody></table></div>`)
		}
		b.WriteString(`</details></article>`)
	}
	return b.String()
}

const oscalDetailCSS = `
    .oscal-card table { width: 100%; font-family: Arial, sans-serif; }
    .oscal-card td, .oscal-card th { text-align: left; vertical-align: top; padding: 3px 6px; }
    .oscal-card summary { cursor: pointer; }
    .oscal-objectives { margin: 4px 0 8px; padding-left: 18px; font-family: Arial, sans-serif; font-size: 13px; }
    .oscal-objectives .oscal-label { color: var(--muted); margin-right: 6px; }
`

type oscalPropView struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Class string `json:"class"`
	Ns    string `json:"ns"`
}

type oscalParamView struct {
	ID     string          `json:"id"`
	Label  string          `json:"label"`
	Props  []oscalPropView `json:"props"`
	Select *struct {
		HowMany string   `json:"how-many"`
		Choice  []string `json:"choice"`
	} `json:"select"`
	Guidelines []struct {
		Prose string `json:"prose"`
	} `json:"guidelines"`
}

type oscalPartView struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Props []oscalPropView `json:"props"`
	Prose string          `json:"prose"`
	Parts []oscalPartView `json:"parts"`
}

func decodeOSCALParams(raw json.RawMessage) []oscalParamView {
	var out []oscalParamView
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func decodeOSCALParts(raw json.RawMessage) []oscalPartView {
	var out []oscalPartView
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func propValueOf(props []oscalPropView, name, class string) string {
	for _, p := range props {
		if p.Name == name && (class == "" || p.Class == class) {
			return p.Value
		}
	}
	return ""
}

// display is the ODP's 800-53A label ("AC-01_ODP[01]"), or its id when the
// catalog gives none (an aggregating parameter such as ac-1_prm_1).
func (p oscalParamView) display() string {
	if l := propValueOf(p.Props, "label", "sp800-53a"); l != "" {
		return l
	}
	return p.ID
}

func (p oscalParamView) propValues(name string) []string {
	var out []string
	for _, prop := range p.Props {
		if prop.Name == name {
			out = append(out, prop.Value)
		}
	}
	return out
}

func (p oscalParamView) asks(labels map[string]string) string {
	if p.Select != nil {
		head := "Selection"
		if p.Select.HowMany == "one-or-more" {
			head = "Selection (one or more)"
		}
		choices := make([]string, 0, len(p.Select.Choice))
		for _, c := range p.Select.Choice {
			choices = append(choices, strings.TrimSpace(oscalInline(c, labels)))
		}
		return head + ": " + strings.Join(choices, "; ")
	}
	label := strings.TrimSpace(oscalInline(p.Label, labels))
	if label != "" && !strings.HasPrefix(label, "organization-defined") {
		label = "organization-defined " + label
	}
	return "Assignment: " + label
}

func (p oscalPartView) propValue(name, class string) string { return propValueOf(p.Props, name, class) }

func (p oscalPartView) leaves() int {
	if len(p.Parts) == 0 {
		if strings.TrimSpace(p.Prose) != "" {
			return 1
		}
		return 0
	}
	n := 0
	for _, c := range p.Parts {
		n += c.leaves()
	}
	return n
}

func (p oscalPartView) objectiveHTML(labels map[string]string) string {
	var b strings.Builder
	b.WriteString(`<ul class="oscal-objectives">`)
	var walk func(part oscalPartView)
	walk = func(part oscalPartView) {
		label := part.propValue("label", "sp800-53a")
		if strings.TrimSpace(part.Prose) != "" || label != "" {
			b.WriteString(`<li><span class="oscal-label">` + detailHTMLEscape(label) + `</span>` + detailHTMLEscape(oscalInline(part.Prose, labels)))
		}
		if len(part.Parts) > 0 {
			b.WriteString(`<ul>`)
			for _, c := range part.Parts {
				if c.Name == "assessment-objective" {
					walk(c)
				}
			}
			b.WriteString(`</ul>`)
		}
		if strings.TrimSpace(part.Prose) != "" || label != "" {
			b.WriteString(`</li>`)
		}
	}
	walk(p)
	b.WriteString(`</ul>`)
	return b.String()
}

var oscalInlineToken = regexp.MustCompile(`\{\{\s*insert:\s*param,\s*([^\s}]+)\s*\}\}|\[([^\]]+)\]\(#[^)]*\)`)

// oscalInline shows a parameter insertion as the ODP it names ("<AC-01_ODP[01]>")
// and a catalog link as its text, which is how SP 800-53A prints its
// objectives.
func oscalInline(prose string, labels map[string]string) string {
	return oscalInlineToken.ReplaceAllStringFunc(prose, func(m string) string {
		sub := oscalInlineToken.FindStringSubmatch(m)
		if sub[1] != "" {
			if l, ok := labels[sub[1]]; ok {
				return "<" + l + ">"
			}
			return "<" + sub[1] + ">"
		}
		return sub[2]
	})
}

// detailWithdrawn marks a control NIST has withdrawn, which only an OSCAL
// import records.
func detailWithdrawn(withdrawn bool) string {
	if !withdrawn {
		return ""
	}
	return " | <strong>Withdrawn by NIST</strong>"
}
