package crisisexercise

// exercisePageScript renders and drives the exercise page from the JSON payload
// embedded in the document.
//
// It is one string rather than a served asset because every other page in this
// application inlines its script, and a single served file would be the first
// thing in this codebase to need a build step, a cache header and a
// content-security-policy exception.
const exercisePageScript = `
"use strict";
(() => {
  const state = JSON.parse(document.getElementById('payload').textContent);
  const D = () => state.dossier;
  const EX = () => state.dossier.exercise;
  const ID = EX().id;

  const esc = v => (v ?? "").toString().replace(/[&<>"']/g, m =>
    ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]));
  const $ = id => document.getElementById(id);
  const el = (html) => { const t = document.createElement('template'); t.innerHTML = html.trim(); return t.content.firstElementChild; };

  function tplus(m) {
    if (m === null || m === undefined || m < 0) return '—';
    const d = Math.floor(m / 1440), r = m % 1440, h = Math.floor(r / 60), mm = r % 60;
    if (d > 0 && h > 0) return 'T+' + d + 'd ' + h + 'h';
    if (d > 0) return 'T+' + d + 'd';
    if (h > 0 && mm > 0) return 'T+' + h + 'h ' + String(mm).padStart(2, '0') + 'm';
    if (h > 0) return 'T+' + h + 'h';
    return 'T+' + mm + 'm';
  }

  async function api(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
    const res = await fetch(path, opts);
    if (res.status === 204) return {};
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || (method + ' ' + path + ' failed'));
    return data;
  }

  async function reload() {
    const fresh = await api('GET', '/crisis-exercises/' + ID + '/report.json');
    state.dossier = fresh;
    const cov = await api('GET', '/crisis-exercises/' + ID + '/coverage');
    state.coverage = cov.coverage || [];
    render();
  }

  function say(node, text, cls) {
    node.textContent = text;
    node.className = 'status' + (cls ? ' ' + cls : '');
  }

  // ---- reference rendering: the thread that ties an exercise to the catalog ----

  function refsHTML(refs, ownerKind, ownerId) {
    const list = (refs || []).map(r => {
      const label = esc(r.ref) + (r.title ? ' · ' + esc(r.title) : '');
      const inner = r.url ? '<a href="' + esc(r.url) + '">' + label + '</a>' : label;
      const cls = r.known ? '' : ' class="unknown"';
      const gone = '<button class="small" data-delref="' + r.id + '" title="Remove this citation">×</button>';
      return '<span' + cls + '>' + inner + (r.known ? '' : ' (unresolved)') + ' ' + gone + '</span>';
    }).join(' · ');
    const add = '<button class="small" data-addref="' + ownerKind + ':' + ownerId + '">+ cite</button>';
    return '<div class="refs">' + (list || '<span class="quiet">nothing cited yet</span>') + ' ' + add + '</div>';
  }

  function wireRefs(root) {
    root.querySelectorAll('[data-delref]').forEach(b => b.addEventListener('click', async () => {
      if (!confirm('Remove this citation?')) return;
      await api('DELETE', '/crisis-exercises/' + ID + '/references/' + b.getAttribute('data-delref'));
      await reload();
    }));
    root.querySelectorAll('[data-addref]').forEach(b => b.addEventListener('click', () => {
      const [kind, id] = b.getAttribute('data-addref').split(':');
      openRefPicker(kind, parseInt(id, 10), b);
    }));
  }

  // The picker searches this installation's own catalogs — the same records the
  // knowledge API serves an AI agent — plus the seeded framework catalog, so a
  // citation means the same thing wherever it is read.
  function openRefPicker(ownerKind, ownerId, anchor) {
    document.querySelectorAll('.refpicker').forEach(p => p.remove());
    const box = el('<div class="card refpicker" style="margin:8px 0">' +
      '<label>Cite a control, requirement, clause, risk or framework</label>' +
      '<div class="row"><input class="q" placeholder="e.g. incident escalation, IR-4, DORA classification" style="flex:1;min-width:240px">' +
      '<button class="small go">Search</button><button class="small close">Close</button></div>' +
      '<div class="results" style="margin-top:8px"></div></div>');
    anchor.parentElement.appendChild(box);
    const q = box.querySelector('.q'), results = box.querySelector('.results');
    box.querySelector('.close').addEventListener('click', () => box.remove());

    async function search() {
      if (!q.value.trim()) return;
      results.innerHTML = '<p class="status">Searching…</p>';
      try {
        const data = await api('GET', '/crisis-exercises/suggest?limit=6&q=' + encodeURIComponent(q.value));
        const c = data.candidates || [];
        if (!c.length) { results.innerHTML = '<p class="status">Nothing matched.</p>'; return; }
        results.innerHTML = c.map((x, i) =>
          '<div class="row" style="padding:4px 0;border-bottom:1px solid var(--line)">' +
          '<button class="small pick" data-i="' + i + '">cite</button>' +
          '<span><strong>' + esc(x.ref) + '</strong> ' + esc(x.title) +
          '<br><span class="meta">' + esc(x.kind) + (x.summary ? ' — ' + esc(x.summary.slice(0, 160)) : '') + '</span></span></div>').join('');
        results.querySelectorAll('.pick').forEach(btn => btn.addEventListener('click', async () => {
          const x = c[parseInt(btn.getAttribute('data-i'), 10)];
          await api('POST', '/crisis-exercises/' + ID + '/references',
            { owner_kind: ownerKind, owner_id: ownerId, ref_kind: x.kind, ref: x.ref, title: x.title });
          box.remove();
          await reload();
        }));
      } catch (e) { results.innerHTML = '<p class="status bad">' + esc(e.message) + '</p>'; }
    }
    box.querySelector('.go').addEventListener('click', search);
    q.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); search(); } });
    q.focus();
  }

  // ---- header ----

  function renderHeader() {
    const ex = EX(), s = D().stats;
    $('statusChip').textContent = ex.status;
    $('summaryLine').textContent = ex.summary || '';

    const stats = [
      [s.phases, 'Phases'], [s.injects, 'Injects'],
      [s.injects_played + '/' + s.injects, 'Played'],
      [s.clocks_met + '/' + s.clocks, 'Clocks met'],
      [s.findings, 'Findings'], [s.distinct_refs, 'References cited']
    ];
    $('stats').innerHTML = stats.map(([n, l]) =>
      '<div class="stat"><span class="n">' + esc(n) + '</span><span class="l">' + esc(l) + '</span></div>').join('');

    const docs = [
      '<a class="chip" href="/crisis-exercises/' + ID + '/msel.csv">Controller MSEL (CSV)</a>',
      '<a class="chip" href="/crisis-exercises/' + ID + '/handout.md">Player handout</a>'
    ];
    if (state.pdf_available) docs.push('<a class="chip" href="/crisis-exercises/' + ID + '/report.pdf">Report (PDF)</a>');
    docs.push('<a class="chip" href="/crisis-exercises/' + ID + '/report.json">JSON</a>');
    docs.push('<select id="statusSelect" style="width:auto">' +
      ['draft','scheduled','in_progress','completed','closed'].map(v =>
        '<option' + (v === ex.status ? ' selected' : '') + '>' + v + '</option>').join('') + '</select>');
    docs.push('<button class="small" id="cutVersion">Cut a version</button>');
    docs.push('<button class="small" id="cloneBtn">Clone for a re-run</button>');
    docs.push('<span id="headStatus" class="status"></span>');
    $('docLinks').innerHTML = docs.join(' ');

    $('statusSelect').addEventListener('change', async e => {
      try {
        await api('PATCH', '/crisis-exercises/' + ID, { status: e.target.value });
        await reload();
      } catch (err) { say($('headStatus'), err.message, 'bad'); }
    });
    $('cutVersion').addEventListener('click', async () => {
      const played = D().stats.injects_played > 0;
      const note = prompt(played ? 'Note for this after-action version:' : 'Note for this design version:');
      if (note === null) return;
      try {
        await api('POST', '/crisis-exercises/' + ID + '/versions',
          { kind: played ? 'after_action' : 'design', note });
        say($('headStatus'), 'Version cut.');
        await reload();
      } catch (err) { say($('headStatus'), err.message, 'bad'); }
    });
    $('cloneBtn').addEventListener('click', async () => {
      const title = prompt('Title for the re-run:', EX().title.replace(/ \(re-run\)$/, '') + ' (re-run)');
      if (title === null) return;
      try {
        const created = await api('POST', '/crisis-exercises/' + ID + '/clone', { title });
        window.location.href = '/crisis-exercises/' + created.id;
      } catch (err) { say($('headStatus'), err.message, 'bad'); }
    });
  }

  // ---- tabs ----

  const TABS = [
    ['brief', 'Brief', renderBrief],
    ['run', 'Run sheet', renderRun],
    ['classification', 'Classification & clocks', renderClassification],
    ['decisions', 'Decision log', renderDecisions],
    ['findings', 'Findings', renderFindings],
    ['coverage', 'Coverage', renderCoverage],
    ['advisers', 'Advisers', renderAdvisers]
  ];
  let active = location.hash.replace('#', '') || 'brief';

  function render() {
    renderHeader();
    $('subtabs').innerHTML = TABS.map(([k, l]) =>
      '<button class="subtab' + (k === active ? ' active' : '') + '" data-tab="' + k + '">' + esc(l) + '</button>').join('');
    $('subtabs').querySelectorAll('[data-tab]').forEach(b => b.addEventListener('click', () => {
      active = b.getAttribute('data-tab');
      location.hash = active;
      render();
    }));
    const panels = $('panels');
    panels.innerHTML = '';
    const tab = TABS.find(t => t[0] === active) || TABS[0];
    const node = document.createElement('div');
    node.className = 'panel active';
    panels.appendChild(node);
    tab[2](node);
    wireRefs(node);
  }

  // ---- brief ----

  function renderBrief(root) {
    const ex = EX(), d = D();
    const facts = [
      ['Reference', ex.reference], ['Format', ex.format], ['Audience', ex.audience],
      ['Entity', ex.entity_name], ['Entity type', ex.entity_type], ['Jurisdiction', ex.jurisdiction],
      ['Supervision', ex.supervision], ['Scheduled', ex.scheduled_for],
      ['Duration', ex.duration_minutes + ' minutes'], ['Facilitator', ex.facilitator],
      ['Control team', ex.control_team], ['Evaluators', ex.evaluators],
      ['Started', ex.started_at], ['Ended', ex.ended_at]
    ].filter(([, v]) => v);

    root.appendChild(el('<section class="card"><h2>The exercise</h2>' +
      '<table class="list">' + facts.map(([k, v]) =>
        '<tr><th style="width:220px">' + esc(k) + '</th><td>' + esc(v) + '</td></tr>').join('') +
      '</table>' +
      (ex.critical_functions ? '<h3>Critical or important functions in scope</h3><p style="white-space:pre-wrap">' +
        esc(ex.critical_functions) + '</p>' : '') +
      refsHTML(d.references.filter(r => r.owner_kind === 'exercise'), 'exercise', ID) +
      '</section>'));

    if (ex.threat_actor || ex.threat_narrative) {
      root.appendChild(el('<section class="card"><h2>Scenario</h2>' +
        (ex.threat_actor ? '<h3>Adversary</h3><p>' + esc(ex.threat_actor) + '</p>' : '') +
        (ex.initial_vector ? '<h3>Initial vector</h3><p>' + esc(ex.initial_vector) + '</p>' : '') +
        (ex.threat_narrative ? '<h3>Narrative</h3><p style="white-space:pre-wrap">' + esc(ex.threat_narrative) + '</p>' : '') +
        (state.scenario ? '<details><summary>Control team only — what this scenario is designed to expose</summary>' +
          '<p class="quiet">' + esc(state.scenario.trap_door) + '</p></details>' : '') +
        '</section>'));
    }

    const objRows = d.objectives.map(o =>
      '<tr><td>' + esc(o.code) + '</td><td>' + esc(o.text) +
      (o.capability ? '<br><span class="meta">Capability: ' + esc(o.capability) + '</span>' : '') +
      (o.success_criteria ? '<br><span class="meta">Success: ' + esc(o.success_criteria) + '</span>' : '') +
      refsHTML(o.references, 'objective', o.id) + '</td>' +
      '<td><select data-rate="' + o.id + '" style="width:auto">' +
      ['untested','met','partially_met','not_met','not_applicable'].map(r =>
        '<option value="' + r + '"' + (r === o.rating ? ' selected' : '') + '>' + r.replace(/_/g, ' ') + '</option>').join('') +
      '</select></td></tr>').join('');

    const objSection = el('<section class="card"><h2>Objectives</h2>' +
      (d.objectives.length ? '<table class="list"><thead><tr><th>#</th><th>Objective</th><th>Rating</th></tr></thead><tbody>' +
        objRows + '</tbody></table>' : '<p class="status">No objectives yet.</p>') +
      '<h3>Add an objective</h3><div class="fields wide">' +
      '<div><label>Objective</label><input id="objText"></div>' +
      '<div><label>Capability tested</label><input id="objCap"></div>' +
      '<div><label>Success criteria</label><input id="objCrit"></div></div>' +
      '<div class="toolbar"><button id="objAdd">Add</button><span id="objStatus" class="status"></span></div>' +
      '</section>');
    root.appendChild(objSection);

    objSection.querySelectorAll('[data-rate]').forEach(sel => sel.addEventListener('change', async () => {
      const id = parseInt(sel.getAttribute('data-rate'), 10);
      const o = d.objectives.find(x => x.id === id);
      try { await api('POST', '/crisis-exercises/' + ID + '/objectives', Object.assign({}, o, { rating: sel.value })); await reload(); }
      catch (e) { say($('objStatus'), e.message, 'bad'); }
    }));
    $('objAdd').addEventListener('click', async () => {
      if (!$('objText').value.trim()) return;
      try {
        await api('POST', '/crisis-exercises/' + ID + '/objectives',
          { text: $('objText').value, capability: $('objCap').value, success_criteria: $('objCrit').value });
        await reload();
      } catch (e) { say($('objStatus'), e.message, 'bad'); }
    });

    const parts = d.participants || [];
    const partSection = el('<section class="card"><h2>Participants</h2>' +
      (parts.length ? '<table class="list"><thead><tr><th>Name</th><th>Role</th><th>Organisation</th><th>Capacity</th><th>Attended</th></tr></thead><tbody>' +
        parts.map((p, i) => '<tr><td>' + esc(p.name) + '</td><td>' + esc(p.role_label || p.role_key) + '</td><td>' + esc(p.org || '') +
          '</td><td>' + (p.player ? 'Player' : 'Exercise staff') + '</td>' +
          '<td><input type="checkbox" data-att="' + i + '"' + (p.attended ? ' checked' : '') + ' style="width:auto"></td></tr>').join('') +
        '</tbody></table>' : '<p class="status">No roster yet. A supervisor asking whether the management body has been exercised wants names.</p>') +
      '<h3>Add a participant</h3><div class="fields">' +
      '<div><label>Name</label><input id="pName"></div>' +
      '<div><label>Seat</label><select id="pRole">' + state.roles.map(r =>
        '<option value="' + esc(r.key) + '">' + esc(r.label) + ' — ' + esc(r.group) + '</option>').join('') + '</select></div>' +
      '<div><label>Organisation</label><input id="pOrg"></div>' +
      '<div><label>Capacity</label><select id="pPlayer"><option value="1">Player — being exercised</option>' +
      '<option value="0">Exercise staff — running it</option></select></div></div>' +
      '<div class="toolbar"><button id="pAdd">Add</button><span id="pStatus" class="status"></span></div>' +
      '<p class="status" id="roleRemit"></p></section>');
    root.appendChild(partSection);

    const remit = () => {
      const r = state.roles.find(x => x.key === $('pRole').value);
      $('roleRemit').textContent = r ? r.remit : '';
    };
    $('pRole').addEventListener('change', remit); remit();

    async function saveParticipants(list) {
      await api('POST', '/crisis-exercises/' + ID + '/participants', { participants: list });
      await reload();
    }
    partSection.querySelectorAll('[data-att]').forEach(box => box.addEventListener('change', async () => {
      const list = parts.map(p => Object.assign({}, p));
      list[parseInt(box.getAttribute('data-att'), 10)].attended = box.checked;
      try { await saveParticipants(list); } catch (e) { say($('pStatus'), e.message, 'bad'); }
    }));
    $('pAdd').addEventListener('click', async () => {
      if (!$('pName').value.trim()) return;
      const list = parts.map(p => Object.assign({}, p));
      list.push({ name: $('pName').value, role_key: $('pRole').value, org: $('pOrg').value,
                  player: $('pPlayer').value === '1', attended: false });
      try { await saveParticipants(list); } catch (e) { say($('pStatus'), e.message, 'bad'); }
    });
  }

  // ---- run sheet ----

  function renderRun(root) {
    const d = D();

    const gen = el('<section class="card"><h2>Master scenario events list</h2>' +
      '<p class="status">' + (state.ai_configured
        ? 'Generation writes the injects a phase needs, each with the behaviour it should provoke and the controls, requirements and instruments it exercises. Phases that already have injects are left alone unless you ask for a regeneration; anything you have edited by hand is never overwritten. Model: ' + esc(state.model)
        : 'No AI provider is configured, so injects are written by hand below. Set one in Settings to generate them.') + '</p>' +
      '<div class="fields">' +
      '<div><label>Phases</label><select id="genPhases" multiple size="6">' +
        d.phases.map(p => '<option value="' + esc(p.key) + '">' + esc(p.name) + (p.injects && p.injects.length ? ' (' + p.injects.length + ')' : '') + '</option>').join('') +
      '</select></div>' +
      '<div><label>Injects per phase</label><input id="genCount" type="number" min="1" max="8" value="4"></div>' +
      '<div><label>Difficulty</label><select id="genDiff">' +
        '<option value="foundation">Foundation — winnable by following the playbook</option>' +
        '<option value="challenge" selected>Challenge — the playbook is not enough</option>' +
        '<option value="advanced">Advanced — no obvious right answer</option></select></div>' +
      '<div><label>Regenerate</label><select id="genRegen"><option value="0">Add to what is there</option>' +
        '<option value="1">Replace the generated injects</option></select></div></div>' +
      '<div><label>Your steer (optional)</label><textarea id="genGuide" placeholder="The incident lead is deliberately unreachable from T+90. Keep the board phase to reserved decisions. Assume the CISO is new in post."></textarea></div>' +
      '<div class="toolbar"><button id="genRun"' + (state.ai_configured ? '' : ' disabled') + '>Generate injects</button>' +
      '<span id="genStatus" class="status"></span></div></section>');
    root.appendChild(gen);

    $('genRun').addEventListener('click', async () => {
      const keys = Array.from($('genPhases').selectedOptions).map(o => o.value);
      say($('genStatus'), 'Writing injects — this takes a model call per phase…');
      $('genRun').disabled = true;
      try {
        const res = await api('POST', '/crisis-exercises/' + ID + '/design', {
          phase_keys: keys, injects_per_phase: parseInt($('genCount').value, 10) || 4,
          difficulty: $('genDiff').value, guidance: $('genGuide').value,
          regenerate: $('genRegen').value === '1'
        });
        let msg = (res.injects || []).length + ' injects written.';
        if (res.failures && res.failures.length) msg += ' Failed: ' + res.failures.join('; ');
        say($('genStatus'), msg, res.failures && res.failures.length ? 'warn' : '');
        await reload();
      } catch (e) { say($('genStatus'), e.message, 'bad'); }
      finally { const b = $('genRun'); if (b) b.disabled = false; }
    });

    d.phases.forEach(p => root.appendChild(phaseCard(p)));
  }

  function phaseCard(p) {
    const injects = p.injects || [];
    const card = el('<div class="phase">' +
      '<header><h3 style="margin:0">' + esc(tplus(p.offset_minutes)) + ' · ' + esc(p.name) + '</h3>' +
      '<span class="meta">' + esc(p.duration_minutes) + ' min · lead: ' + esc(p.lead_role.replace(/_/g, ' ')) +
      ' · <select data-phasestatus="' + p.id + '" style="width:auto;display:inline-block">' +
      ['pending','active','complete','skipped','curtailed'].map(s =>
        '<option' + (s === p.status ? ' selected' : '') + '>' + s + '</option>').join('') + '</select></span></header>' +
      '<p class="quiet">' + esc(p.purpose) + '</p>' +
      '<details><summary>Entry and exit criteria, and the facilitator note</summary>' +
      (p.entry_criteria ? '<p><strong>Entry.</strong><br><span style="white-space:pre-wrap">' + esc(p.entry_criteria) + '</span></p>' : '') +
      (p.exit_criteria ? '<p><strong>Exit.</strong><br><span style="white-space:pre-wrap">' + esc(p.exit_criteria) + '</span></p>' : '') +
      (p.notes ? '<p class="quiet">' + esc(p.notes) + '</p>' : '') + '</details>' +
      refsHTML(p.references, 'phase', p.id) +
      '<div class="injects"></div>' +
      '<div class="toolbar"><button class="small" data-addinject="' + p.id + '">+ write an inject here</button></div>' +
      '</div>');

    const holder = card.querySelector('.injects');
    injects.forEach(i => holder.appendChild(injectCard(i)));

    card.querySelector('[data-phasestatus]').addEventListener('change', async e => {
      try { await api('POST', '/crisis-exercises/' + ID + '/phases', Object.assign({}, p, { status: e.target.value, injects: undefined, references: undefined })); await reload(); }
      catch (err) { alert(err.message); }
    });
    card.querySelector('[data-addinject]').addEventListener('click', () => openInjectForm(card, p));
    return card;
  }

  function injectCard(i) {
    const r = i.response;
    const outcomeClass = !r || r.outcome === 'not_played' ? 'quiet'
      : r.outcome === 'as_expected' ? 'met' : r.outcome === 'missed' ? 'missed' : 'warnv';
    const node = el('<div class="inject ' + esc(i.inject_type) + '">' +
      '<div class="head">' + esc(i.code) + ' · ' + esc(tplus(i.offset_minutes)) + ' · ' + esc(i.inject_type) +
      ' · ' + esc((i.channel || '').replace(/_/g, ' ')) +
      (i.from_actor || i.to_actor ? ' · ' + esc(i.from_actor || '—') + ' → ' + esc(i.to_actor || '—') : '') +
      (i.ai_generated ? ' · <span title="Written by ' + esc(i.model || 'a model') + ', prompt ' + esc(i.prompt_hash || '') + '">generated</span>' : '') +
      '</div>' +
      (i.title ? '<h4>' + esc(i.title) + '</h4>' : '') +
      (i.body ? '<div class="body">' + esc(i.body) + '</div>' : '') +
      (i.expected_actions ? '<p class="meta"><strong>Expected.</strong> ' + esc(i.expected_actions) + '</p>' : '') +
      (i.expected_decision ? '<p class="meta"><strong>Decision sought.</strong> ' + esc(i.expected_decision) +
        (i.decision_owner ? ' (' + esc(i.decision_owner.replace(/_/g, ' ')) + ')' : '') + '</p>' : '') +
      (i.evaluation_notes ? '<p class="meta"><strong>Watch for.</strong> ' + esc(i.evaluation_notes) + '</p>' : '') +
      refsHTML(i.references, 'inject', i.id) +
      '<div class="row" style="margin-top:6px">' +
      '<span class="' + outcomeClass + '">' + esc(r && r.outcome ? r.outcome.replace(/_/g, ' ') : 'not played') + '</span>' +
      (r && r.responded_offset >= 0 ? '<span class="meta">at ' + esc(tplus(r.responded_offset)) + '</span>' : '') +
      '<button class="small" data-record="' + i.id + '">record what happened</button>' +
      '<button class="small" data-probe="' + i.id + '"' + (state.ai_configured ? '' : ' disabled') + '>hot seat</button>' +
      '<button class="small" data-delinject="' + i.id + '">delete</button></div>' +
      (r && r.actual_actions ? '<p class="meta" style="margin-top:4px"><strong>Observed.</strong> ' + esc(r.actual_actions) +
        (r.observations ? '<br><span class="quiet">' + esc(r.observations) + '</span>' : '') + '</p>' : '') +
      '<div class="slot"></div></div>');

    node.querySelector('[data-record]').addEventListener('click', () => openResponseForm(node.querySelector('.slot'), i));
    node.querySelector('[data-probe]').addEventListener('click', () => openProbe(node.querySelector('.slot'), i));
    node.querySelector('[data-delinject]').addEventListener('click', async () => {
      if (!confirm('Delete inject ' + i.code + '?')) return;
      await api('DELETE', '/crisis-exercises/' + ID + '/injects/' + i.id);
      await reload();
    });
    return node;
  }

  function openInjectForm(card, phase) {
    const box = el('<div class="card" style="margin-top:10px">' +
      '<h4>New inject in ' + esc(phase.name) + '</h4>' +
      '<div class="fields"><div><label>Title</label><input class="t"></div>' +
      '<div><label>T+ minutes (exercise clock)</label><input class="o" type="number" value="' + phase.offset_minutes + '"></div>' +
      '<div><label>Type</label><select class="ty">' +
        ['event','stress','ambiguous','decision','contingency','information'].map(v => '<option>' + v + '</option>').join('') + '</select></div>' +
      '<div><label>Channel</label><select class="ch">' +
        ['email','phone','chat','sms','siem_alert','ticket','news','social_media','regulator','board','customer','third_party','in_person','law_enforcement']
          .map(v => '<option>' + v + '</option>').join('') + '</select></div>' +
      '<div><label>From</label><input class="fr"></div><div><label>To (role)</label><input class="to"></div></div>' +
      '<div><label>The message, as it would be delivered</label><textarea class="b"></textarea></div>' +
      '<div><label>Expected action</label><textarea class="e" placeholder="What should the players do? Specific enough for an evaluator to mark."></textarea></div>' +
      '<div class="toolbar"><button class="save">Save inject</button><button class="secondary cancel">Cancel</button>' +
      '<span class="status st"></span></div></div>');
    card.appendChild(box);
    box.querySelector('.cancel').addEventListener('click', () => box.remove());
    box.querySelector('.save').addEventListener('click', async () => {
      try {
        await api('POST', '/crisis-exercises/' + ID + '/injects', {
          phase_id: phase.id, title: box.querySelector('.t').value,
          offset_minutes: parseInt(box.querySelector('.o').value, 10) || phase.offset_minutes,
          inject_type: box.querySelector('.ty').value, channel: box.querySelector('.ch').value,
          from_actor: box.querySelector('.fr').value, to_actor: box.querySelector('.to').value,
          body: box.querySelector('.b').value, expected_actions: box.querySelector('.e').value
        });
        await reload();
      } catch (e) { say(box.querySelector('.st'), e.message, 'bad'); }
    });
  }

  function openResponseForm(slot, inject) {
    slot.innerHTML = '';
    const r = inject.response || {};
    const box = el('<div class="card" style="margin-top:8px">' +
      '<div class="fields"><div><label>Outcome</label><select class="oc">' +
      ['as_expected','partial','deviation','missed','not_played'].map(v =>
        '<option value="' + v + '"' + (r.outcome === v ? ' selected' : '') + '>' + v.replace(/_/g, ' ') + '</option>').join('') +
      '</select></div>' +
      '<div><label>Responded at (T+ minutes)</label><input class="ro" type="number" value="' +
        (r.responded_offset >= 0 ? r.responded_offset : inject.offset_minutes) + '"></div></div>' +
      '<div><label>What the team actually did</label><textarea class="aa">' + esc(r.actual_actions || '') + '</textarea></div>' +
      '<div><label>Evaluator observations</label><textarea class="ob">' + esc(r.observations || '') + '</textarea></div>' +
      '<div class="toolbar"><button class="save">Save</button><button class="secondary cancel">Cancel</button>' +
      '<span class="status st"></span></div></div>');
    slot.appendChild(box);
    box.querySelector('.cancel').addEventListener('click', () => { slot.innerHTML = ''; });
    box.querySelector('.save').addEventListener('click', async () => {
      try {
        await api('POST', '/crisis-exercises/' + ID + '/injects/' + inject.id + '/response', {
          outcome: box.querySelector('.oc').value,
          responded_offset: parseInt(box.querySelector('.ro').value, 10),
          actual_actions: box.querySelector('.aa').value,
          observations: box.querySelector('.ob').value
        });
        await reload();
      } catch (e) { say(box.querySelector('.st'), e.message, 'bad'); }
    });
  }

  // The hot seat: hand one inject and the team's handling of it to an expert
  // persona and let them push back, in character, while the room is still there.
  function openProbe(slot, inject) {
    slot.innerHTML = '';
    const inPlay = state.personas.filter(p => p.in_play);
    const box = el('<div class="card" style="margin-top:8px">' +
      '<label>Put this in front of…</label>' +
      '<div class="row"><select class="p" style="flex:1">' + inPlay.map(p =>
        '<option value="' + esc(p.key) + '">' + esc(p.name) + ' — ' + esc(p.remit) + '</option>').join('') + '</select>' +
      '<button class="small go">Ask</button><button class="small close">Close</button></div>' +
      '<div class="out" style="margin-top:8px"></div></div>');
    slot.appendChild(box);
    box.querySelector('.close').addEventListener('click', () => { slot.innerHTML = ''; });
    box.querySelector('.go').addEventListener('click', async () => {
      const out = box.querySelector('.out');
      out.innerHTML = '<p class="status">Thinking…</p>';
      try {
        const turn = await api('POST', '/crisis-exercises/' + ID + '/injects/' + inject.id + '/probe',
          { persona: box.querySelector('.p').value });
        out.innerHTML = '<div class="msg ai"><span class="who">' + esc(turn.persona.replace(/_/g, ' ')) +
          ' · ' + esc(turn.model || '') + '</span>' + esc(turn.content) + '</div>';
      } catch (e) { out.innerHTML = '<p class="status bad">' + esc(e.message) + '</p>'; }
    });
  }

  // ---- classification and clocks ----

  function renderClassification(root) {
    const c = D().classification, clocks = D().clocks || [];

    const crit = (id, label, valueField, materialField, placeholder) =>
      '<div><label>' + esc(label) + '</label>' +
      '<input id="' + id + 'V" value="' + esc(c[valueField] || '') + '" placeholder="' + esc(placeholder || '') + '">' +
      '<label style="margin-top:6px"><input type="checkbox" id="' + id + 'M"' + (c[materialField] ? ' checked' : '') +
      ' style="width:auto"> threshold met</label></div>';

    const form = el('<section class="card"><h2>Incident classification</h2>' +
      '<p class="status">Assess each criterion against your own thresholds — that judgement is what this phase exists to rehearse. ' +
      'The combination rule is applied here: under the DORA technical standard an incident is major where critical services are affected ' +
      'and either the data-losses criterion is met, or two or more of the others are.</p>' +
      '<div class="fields">' +
      '<div><label>Became aware at (T+ minutes)</label><input id="awareOff" type="number" value="' + (c.aware_offset || 0) + '"></div>' +
      '<div><label>Classified as major at (T+ minutes)</label><input id="classOff" type="number" value="' + (c.classified_offset || 0) + '"></div>' +
      '<div><label>Critical or important services affected</label>' +
      '<label><input type="checkbox" id="critSvc"' + (c.critical_services_affected ? ' checked' : '') + ' style="width:auto"> yes</label></div>' +
      '</div><div class="fields">' +
      crit('cli', 'Clients and financial counterparts affected', 'clients_affected', 'clients_material', 'e.g. 41,000 retail customers') +
      crit('trx', 'Transactions affected', 'transactions_affected', 'transactions_material', 'e.g. 12% of daily card volume') +
      crit('rep', 'Reputational impact', 'reputational_impact', 'reputational_material', 'e.g. national press, leak site listing') +
      crit('geo', 'Geographical spread', 'geographical_spread', 'geographical_material', 'e.g. LT, LV and EE') +
      crit('dat', 'Data losses', 'data_losses', 'data_losses_material', 'e.g. 340 GB exfiltrated, contents unknown') +
      crit('eco', 'Economic impact', 'economic_impact', 'economic_material', 'e.g. €2.4m direct, recovery excluded') +
      '<div><label>Service downtime (minutes)</label><input id="downMin" type="number" value="' + (c.downtime_minutes || 0) + '">' +
      '<label style="margin-top:6px"><input type="checkbox" id="durM"' + (c.duration_material ? ' checked' : '') +
      ' style="width:auto"> threshold met</label></div>' +
      '<div><label>Other regimes</label>' +
      '<label><input type="checkbox" id="pdb"' + (c.personal_data_breach ? ' checked' : '') + ' style="width:auto"> personal data breach</label>' +
      '<label><input type="checkbox" id="nis2"' + (c.nis2_significant ? ' checked' : '') + ' style="width:auto"> significant under NIS2</label></div>' +
      '</div>' +
      '<div><label>What the team concluded (before seeing the computed answer)</label><textarea id="verdict">' + esc(c.team_verdict || '') + '</textarea></div>' +
      '<div><label>Notes</label><textarea id="clsNotes">' + esc(c.notes || '') + '</textarea></div>' +
      '<div class="toolbar"><button id="clsSave">Save and recompute the clocks</button><span id="clsStatus" class="status"></span></div>' +
      '<h3>Computed classification</h3>' +
      '<p class="' + (c.major ? 'missed' : 'quiet') + '">' + (c.major ? 'Major incident' : 'Not a major incident') + '</p>' +
      '<p class="quiet">' + esc(c.rationale || '') + '</p>' +
      '</section>');
    root.appendChild(form);

    $('clsSave').addEventListener('click', async () => {
      say($('clsStatus'), 'Saving…');
      try {
        await api('PUT', '/crisis-exercises/' + ID + '/classification', {
          aware_offset: parseInt($('awareOff').value, 10) || 0,
          classified_offset: parseInt($('classOff').value, 10) || 0,
          critical_services_affected: $('critSvc').checked,
          clients_affected: $('cliV').value, clients_material: $('cliM').checked,
          transactions_affected: $('trxV').value, transactions_material: $('trxM').checked,
          reputational_impact: $('repV').value, reputational_material: $('repM').checked,
          geographical_spread: $('geoV').value, geographical_material: $('geoM').checked,
          data_losses: $('datV').value, data_losses_material: $('datM').checked,
          economic_impact: $('ecoV').value, economic_material: $('ecoM').checked,
          downtime_minutes: parseInt($('downMin').value, 10) || 0, duration_material: $('durM').checked,
          personal_data_breach: $('pdb').checked, nis2_significant: $('nis2').checked,
          team_verdict: $('verdict').value, notes: $('clsNotes').value
        });
        await reload();
      } catch (e) { say($('clsStatus'), e.message, 'bad'); }
    });

    const rows = clocks.map(k => {
      const cls = k.status === 'met' ? 'met' : k.status === 'missed' ? 'missed' : 'quiet';
      const late = k.actual_offset >= 0 && k.actual_offset > k.due_offset
        ? ' (late by ' + tplus(k.actual_offset - k.due_offset).replace('T+', '') + ')' : '';
      return '<tr><td>' + esc(k.label) + '<br><span class="meta">' + esc(k.basis) + '</span>' +
        (k.notes ? '<br><span class="meta quiet">' + esc(k.notes) + '</span>' : '') +
        refsHTML(k.references, 'clock', k.id) + '</td>' +
        '<td>' + esc(k.authority) + '</td>' +
        '<td>' + (k.status === 'not_applicable' ? '—' : esc(tplus(k.due_offset))) + '</td>' +
        '<td>' + esc(tplus(k.actual_offset)) + '</td>' +
        '<td class="' + cls + '">' + esc(k.status.replace(/_/g, ' ')) + esc(late) + '</td>' +
        '<td>' + (k.status === 'not_applicable' ? '' :
          '<button class="small" data-clock="' + k.id + '">record</button>') + '</td></tr>';
    }).join('');

    const clockSection = el('<section class="card"><h2>Notification clocks</h2>' +
      '<p class="status">Derived from the classification above and this entity\'s supervisory perimeter. ' +
      'Record when each notification actually went and the report will say whether it was met, and by how much it was missed.</p>' +
      '<table class="list"><thead><tr><th>Obligation</th><th>Owed to</th><th>Due</th><th>Sent</th><th>Outcome</th><th></th></tr></thead>' +
      '<tbody>' + rows + '</tbody></table></section>');
    root.appendChild(clockSection);

    clockSection.querySelectorAll('[data-clock]').forEach(b => b.addEventListener('click', async () => {
      const at = prompt('Notification sent at T+ how many minutes? (blank or -1 if it never went)');
      if (at === null) return;
      const evidence = prompt('Evidence — what was sent, by whom, through what channel:') || '';
      try {
        await api('POST', '/crisis-exercises/' + ID + '/clocks/' + b.getAttribute('data-clock'),
          { actual_offset: at.trim() === '' ? -1 : (parseInt(at, 10)), evidence });
        await reload();
      } catch (e) { alert(e.message); }
    }));
  }

  // ---- decisions ----

  function renderDecisions(root) {
    const rows = (D().decisions || []).map(d =>
      '<tr><td>' + esc(tplus(d.offset_minutes)) + '</td>' +
      '<td><strong>' + esc(d.title) + '</strong><br>' + esc(d.decision) +
      (d.reversible ? '' : '<br><span class="missed">Irreversible</span>') +
      refsHTML(d.references, 'decision', d.id) + '</td>' +
      '<td>' + esc(d.options || '') + '</td>' +
      '<td>' + esc(d.rationale || '') +
      (d.regulatory_implication ? '<br><span class="meta">Regulatory: ' + esc(d.regulatory_implication) + '</span>' : '') +
      (d.customer_impact ? '<br><span class="meta">Customers: ' + esc(d.customer_impact) + '</span>' : '') + '</td>' +
      '<td>' + esc(d.made_by || '—') + '<br><span class="meta">' + esc((d.role || '').replace(/_/g, ' ')) + '</span>' +
      (d.authority ? '<br><span class="meta">Authority: ' + esc(d.authority) + '</span>' : '') + '</td>' +
      '<td><button class="small" data-deldec="' + d.id + '">delete</button></td></tr>').join('');

    const section = el('<section class="card"><h2>Decision log</h2>' +
      '<p class="status">The record a supervisor asks for after a real event: what was decided, what else was ' +
      'considered, why, and who was entitled to decide it. Teams that have never written this under time pressure ' +
      'write it badly when it counts.</p>' +
      '<table class="list"><thead><tr><th>T+</th><th>Decision</th><th>Options considered</th><th>Rationale</th><th>Taken by</th><th></th></tr></thead>' +
      '<tbody>' + (rows || '<tr><td colspan="6" class="status">No decisions logged yet.</td></tr>') + '</tbody></table>' +
      '<h3>Log a decision</h3><div class="fields wide">' +
      '<div><label>Decision point</label><input id="dTitle"></div>' +
      '<div><label>T+ minutes</label><input id="dOff" type="number" value="0"></div>' +
      '<div><label>Taken by</label><input id="dBy"></div>' +
      '<div><label>Seat</label><select id="dRole">' + state.roles.filter(r => r.group === 'entity').map(r =>
        '<option value="' + esc(r.key) + '">' + esc(r.label) + '</option>').join('') + '</select></div>' +
      '<div><label>Who was entitled to decide it</label><input id="dAuth" placeholder="Board · CEO · crisis director · nobody knew"></div>' +
      '<div><label>Reversible?</label><select id="dRev"><option value="1">Yes</option><option value="0">No — this one cannot be walked back</option></select></div>' +
      '</div>' +
      '<div><label>What was decided</label><textarea id="dDec"></textarea></div>' +
      '<div><label>Options considered</label><textarea id="dOpt"></textarea></div>' +
      '<div><label>Rationale</label><textarea id="dRat"></textarea></div>' +
      '<div class="fields"><div><label>Regulatory implication</label><input id="dReg"></div>' +
      '<div><label>Customer impact</label><input id="dCust"></div></div>' +
      '<div class="toolbar"><button id="dAdd">Log it</button><span id="dStatus" class="status"></span></div></section>');
    root.appendChild(section);

    section.querySelectorAll('[data-deldec]').forEach(b => b.addEventListener('click', async () => {
      if (!confirm('Delete this decision?')) return;
      await api('DELETE', '/crisis-exercises/' + ID + '/decisions/' + b.getAttribute('data-deldec'));
      await reload();
    }));
    $('dAdd').addEventListener('click', async () => {
      if (!$('dTitle').value.trim()) return;
      try {
        await api('POST', '/crisis-exercises/' + ID + '/decisions', {
          title: $('dTitle').value, offset_minutes: parseInt($('dOff').value, 10) || 0,
          made_by: $('dBy').value, role: $('dRole').value, authority: $('dAuth').value,
          reversible: $('dRev').value === '1', decision: $('dDec').value,
          options: $('dOpt').value, rationale: $('dRat').value,
          regulatory_implication: $('dReg').value, customer_impact: $('dCust').value
        });
        await reload();
      } catch (e) { say($('dStatus'), e.message, 'bad'); }
    });
  }

  // ---- findings ----

  function renderFindings(root) {
    const sevClass = s => (s === 'critical' || s === 'high') ? 'missed' : s === 'medium' ? 'warnv' : 'quiet';
    const cards = (D().findings || []).map(f =>
      '<div class="card"><h3>' + esc(f.code) + ' — ' + esc(f.title) + '</h3>' +
      '<p class="meta"><span class="' + sevClass(f.severity) + '">' + esc(f.severity.toUpperCase()) + '</span> · ' +
      esc((f.category || '').replace(/_/g, ' ')) + ' · ' + esc(f.status) +
      (f.phase_key ? ' · ' + esc(f.phase_key.replace(/_/g, ' ')) : '') +
      (f.ai_generated ? ' · <span title="Drafted by ' + esc(f.model || 'a model') + ' — review before the report is cut">drafted</span>' : '') + '</p>' +
      (f.description ? '<p>' + esc(f.description) + '</p>' : '') +
      (f.evidence ? '<p class="meta"><strong>Evidence.</strong> ' + esc(f.evidence) + '</p>' : '') +
      (f.root_cause ? '<p class="meta"><strong>Root cause.</strong> ' + esc(f.root_cause) + '</p>' : '') +
      (f.recommendation ? '<p class="meta"><strong>Recommendation.</strong> ' + esc(f.recommendation) + '</p>' : '') +
      '<div class="row"><span class="meta">Owner: ' + esc(f.owner || '—') + ' · Due: ' + esc(f.due_date || '—') +
      (f.risk_ref ? ' · Risk: ' + esc(f.risk_ref) : '') + '</span>' +
      '<button class="small" data-editfind="' + f.id + '">edit</button>' +
      '<button class="small" data-delfind="' + f.id + '">delete</button></div>' +
      refsHTML(f.references, 'finding', f.id) + '</div>').join('');

    const section = el('<section class="card"><h2>Findings</h2>' +
      '<p class="status">' + (state.ai_configured
        ? 'The evaluator can draft findings from what was recorded against the injects, the decisions and the clocks. Every draft is the model\'s proposal, marked as such — read, edit, reassign or delete before the report is cut.'
        : 'No AI provider is configured. Write the findings below.') + '</p>' +
      '<div class="toolbar"><button id="aarRun"' + (state.ai_configured ? '' : ' disabled') + '>Draft findings from the record</button>' +
      '<span id="aarStatus" class="status"></span></div></section>');
    root.appendChild(section);

    $('aarRun').addEventListener('click', async () => {
      say($('aarStatus'), 'Reading the record…');
      $('aarRun').disabled = true;
      try {
        const res = await api('POST', '/crisis-exercises/' + ID + '/after-action', {});
        say($('aarStatus'), res.summary || 'Done.');
        await reload();
      } catch (e) { say($('aarStatus'), e.message, 'bad'); }
      finally { const b = $('aarRun'); if (b) b.disabled = false; }
    });

    const list = el('<div>' + (cards || '<p class="status">No findings recorded yet.</p>') + '</div>');
    root.appendChild(list);
    list.querySelectorAll('[data-delfind]').forEach(b => b.addEventListener('click', async () => {
      if (!confirm('Delete this finding?')) return;
      await api('DELETE', '/crisis-exercises/' + ID + '/findings/' + b.getAttribute('data-delfind'));
      await reload();
    }));
    list.querySelectorAll('[data-editfind]').forEach(b => b.addEventListener('click', () => {
      const f = D().findings.find(x => x.id === parseInt(b.getAttribute('data-editfind'), 10));
      openFindingForm(root, f);
    }));

    root.appendChild(findingFormCard());
  }

  function findingFormCard(f) {
    f = f || {};
    const phases = D().phases || [];
    const box = el('<section class="card"><h3>' + (f.id ? 'Edit ' + esc(f.code) : 'Add a finding') + '</h3>' +
      '<div class="fields wide">' +
      '<div><label>Finding</label><input class="t" value="' + esc(f.title || '') + '"></div>' +
      '<div><label>Phase</label><select class="ph"><option value="0">—</option>' + phases.map(p =>
        '<option value="' + p.id + '"' + (f.phase_id === p.id ? ' selected' : '') + '>' + esc(p.name) + '</option>').join('') + '</select></div>' +
      '<div><label>Category</label><select class="c">' +
        ['people','process','technology','governance','communication','third_party','regulatory'].map(v =>
          '<option value="' + v + '"' + (f.category === v ? ' selected' : '') + '>' + v.replace(/_/g, ' ') + '</option>').join('') + '</select></div>' +
      '<div><label>Severity</label><select class="s">' +
        ['critical','high','medium','low','observation'].map(v =>
          '<option value="' + v + '"' + (f.severity === v ? ' selected' : '') + '>' + v + '</option>').join('') + '</select></div>' +
      '<div><label>Owner</label><input class="o" value="' + esc(f.owner || '') + '"></div>' +
      '<div><label>Due</label><input class="d" type="date" value="' + esc(f.due_date || '') + '"></div>' +
      '<div><label>Status</label><select class="st2">' +
        ['open','in_progress','closed','accepted'].map(v =>
          '<option value="' + v + '"' + (f.status === v ? ' selected' : '') + '>' + v.replace(/_/g, ' ') + '</option>').join('') + '</select></div>' +
      '<div><label>Risk register entry</label><input class="r" value="' + esc(f.risk_ref || '') + '" placeholder="RISK-014"></div>' +
      '</div>' +
      '<div><label>What was observed</label><textarea class="de">' + esc(f.description || '') + '</textarea></div>' +
      '<div><label>Evidence</label><textarea class="ev">' + esc(f.evidence || '') + '</textarea></div>' +
      '<div><label>Root cause</label><textarea class="rc">' + esc(f.root_cause || '') + '</textarea></div>' +
      '<div><label>Recommendation</label><textarea class="re">' + esc(f.recommendation || '') + '</textarea></div>' +
      '<div class="toolbar"><button class="save">Save</button><span class="status st"></span></div></section>');

    box.querySelector('.save').addEventListener('click', async () => {
      if (!box.querySelector('.t').value.trim()) return;
      try {
        await api('POST', '/crisis-exercises/' + ID + '/findings', {
          id: f.id || 0, code: f.code || '', ordinal: f.ordinal || 0,
          title: box.querySelector('.t').value, phase_id: parseInt(box.querySelector('.ph').value, 10) || 0,
          category: box.querySelector('.c').value, severity: box.querySelector('.s').value,
          owner: box.querySelector('.o').value, due_date: box.querySelector('.d').value,
          status: box.querySelector('.st2').value, risk_ref: box.querySelector('.r').value,
          description: box.querySelector('.de').value, evidence: box.querySelector('.ev').value,
          root_cause: box.querySelector('.rc').value, recommendation: box.querySelector('.re').value
        });
        await reload();
      } catch (e) { say(box.querySelector('.st'), e.message, 'bad'); }
    });
    return box;
  }

  function openFindingForm(root, f) {
    const card = findingFormCard(f);
    root.appendChild(card);
    card.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  // ---- coverage ----

  function renderCoverage(root) {
    const rows = (state.coverage || []).map(r =>
      '<tr><td>' + esc(r.kind.replace(/_/g, ' ')) + '</td>' +
      '<td>' + (r.url ? '<a href="' + esc(r.url) + '">' + esc(r.ref) + '</a>' : esc(r.ref)) +
      (r.known ? '' : ' <span class="missed">(unresolved)</span>') + '</td>' +
      '<td>' + esc(r.title || '') + '</td>' +
      '<td>' + esc((r.owners || []).join(', ')) + '</td>' +
      '<td>' + esc(r.citations) + '</td>' +
      '<td>' + (r.findings ? '<span class="missed">' + esc(r.findings) + '</span>' : '0') + '</td></tr>').join('');

    root.appendChild(el('<section class="card"><h2>What this exercise tested</h2>' +
      '<p class="status">Every citation made anywhere in this exercise, inverted. This is the table that ' +
      'turns "we ran a crisis exercise" into an answer to "which controls, requirements and obligations did you ' +
      'test, and did any of them fail". An unresolved reference is one that does not exist in this ' +
      'installation\'s catalogs — usually a model that invented an identifier, and always worth deleting.</p>' +
      '<table class="list"><thead><tr><th>Kind</th><th>Reference</th><th>Title</th><th>Cited by</th><th>Citations</th><th>Findings against it</th></tr></thead>' +
      '<tbody>' + (rows || '<tr><td colspan="6" class="status">Nothing cited yet.</td></tr>') + '</tbody></table></section>'));
  }

  // ---- advisers ----

  function renderAdvisers(root) {
    const personas = state.personas;
    const section = el('<section class="card"><h2>Expert advisers</h2>' +
      '<p class="status">' + (state.ai_configured
        ? 'A second opinion from the seat you are trying to exercise. Pointed at a Wintermute agent with the grc source, these answer from this installation\'s own catalogs rather than from general knowledge. Model: ' + esc(state.model)
        : 'No AI provider is configured. Set one in Settings.') + '</p>' +
      '<div class="fields"><div><label>Adviser</label><select id="advWho">' + personas.map(p =>
        '<option value="' + esc(p.key) + '">' + esc(p.name) + ' — ' + esc(p.title) + '</option>').join('') + '</select></div>' +
      '<div><label>About (optional)</label><input id="advScope" placeholder="the board phase · inject INJ-014 · our holding statement"></div></div>' +
      '<p class="status" id="advRemit"></p>' +
      '<div id="advExamples" class="toolbar"></div>' +
      '<div><label>Question</label><textarea id="advQ" placeholder="Ask it the way you would ask a colleague."></textarea></div>' +
      '<div class="toolbar"><button id="advAsk"' + (state.ai_configured ? '' : ' disabled') + '>Ask</button>' +
      '<button class="secondary" id="advClear">Clear the transcript</button>' +
      '<span id="advStatus" class="status"></span></div></section>');
    root.appendChild(section);

    const log = el('<section class="card"><h2>Transcript</h2><div class="chatlog" id="advLog"></div></section>');
    root.appendChild(log);

    function drawLog() {
      const who = $('advWho').value;
      const turns = (state.chat || []).filter(t => t.persona === who);
      $('advLog').innerHTML = turns.length ? turns.map(t =>
        '<div class="msg ' + (t.role === 'user' ? 'user' : 'ai') + '"><span class="who">' +
        esc(t.role === 'user' ? (t.actor || 'you') : t.persona.replace(/_/g, ' ')) +
        (t.scope ? ' · ' + esc(t.scope) : '') + '</span>' + esc(t.content) + '</div>').join('')
        : '<p class="status">Nothing asked of this adviser yet.</p>';
      $('advLog').scrollTop = $('advLog').scrollHeight;
    }

    function drawPersona() {
      const p = personas.find(x => x.key === $('advWho').value);
      $('advRemit').textContent = p ? p.remit : '';
      $('advExamples').innerHTML = p ? (p.ask_about || []).map((q, i) =>
        '<button class="small" data-ex="' + i + '">' + esc(q) + '</button>').join('') : '';
      $('advExamples').querySelectorAll('[data-ex]').forEach(b => b.addEventListener('click', () => {
        $('advQ').value = p.ask_about[parseInt(b.getAttribute('data-ex'), 10)];
        $('advQ').focus();
      }));
      drawLog();
    }
    $('advWho').addEventListener('change', drawPersona);
    drawPersona();

    $('advAsk').addEventListener('click', async () => {
      if (!$('advQ').value.trim()) return;
      say($('advStatus'), 'Thinking…');
      $('advAsk').disabled = true;
      try {
        await api('POST', '/crisis-exercises/' + ID + '/advise',
          { persona: $('advWho').value, scope: $('advScope').value, question: $('advQ').value });
        $('advQ').value = '';
        const fresh = await api('GET', '/crisis-exercises/' + ID + '/chat');
        state.chat = fresh.turns || [];
        say($('advStatus'), '');
        drawLog();
      } catch (e) { say($('advStatus'), e.message, 'bad'); }
      finally { const b = $('advAsk'); if (b) b.disabled = false; }
    });
    $('advClear').addEventListener('click', async () => {
      if (!confirm('Clear every adviser transcript for this exercise?')) return;
      await api('DELETE', '/crisis-exercises/' + ID + '/chat');
      state.chat = [];
      drawLog();
    });
  }

  render();
})();
`
