package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

// exceptionScopeScript is the scope of an exercise for both Exceptions pages:
// the Security NFRs the exceptions are drawn from. internal/exceptionbook
// restates it for the workbook; a change here needs the same change there.
const exceptionScopeScript = `
    function nfrKey(item) {
      return String((item && item.key) || '').trim();
    }

    // The keys of the catalog, each once.
    function catalogKeys(items) {
      return Array.from(new Set((items || []).map(nfrKey).filter((key) => key !== '')));
    }

    // The scope of an exercise travels in the address: one scope parameter for
    // each Security NFR in it. No scope parameter means the whole catalog is in
    // scope; an empty one means nothing is.
    function scopeFromAddress(items, search) {
      const params = new URLSearchParams(search === undefined ? window.location.search : search);
      const keys = catalogKeys(items);
      if (!params.has('scope')) return new Set(keys);
      const named = new Set(params.getAll('scope').map((key) => String(key || '').trim()));
      return new Set(keys.filter((key) => named.has(key)));
    }

    function appendScope(params, scope, items) {
      const keys = catalogKeys(items);
      const inScope = keys.filter((key) => scope.has(key));
      if (inScope.length === keys.length) return;
      if (!inScope.length) {
        params.append('scope', '');
        return;
      }
      inScope.forEach((key) => params.append('scope', key));
    }

    function scopeLine(scope, items) {
      const keys = catalogKeys(items);
      return 'Scope: ' + keys.filter((key) => scope.has(key)).length + ' of ' + keys.length + ' Security NFRs';
    }

    // An exception is full or partial. The partial ones travel in the address
    // as partial parameters, beside the key parameter every exception has.
    function partialFromAddress(search) {
      return new Set(
        new URLSearchParams(search === undefined ? window.location.search : search).getAll('partial')
          .map((key) => String(key || '').trim())
          .filter((key) => key !== '')
      );
    }

    // The exercise of this browser tab -- the scope, the selection and the
    // partial exceptions, as the Exceptions page's address carries them -- is
    // kept for the tab's session. The Exceptions Workbook page offers a
    // workbook with it, and the Exceptions page comes back to it. It stays in
    // the browser: the application is not sent it and does not store it.
    const EXERCISE_SESSION_KEY = 'exceptions.exercise';

    function storedExercise() {
      try {
        return window.sessionStorage.getItem(EXERCISE_SESSION_KEY) || '';
      } catch (error) {
        return '';
      }
    }

    function storeExercise(query) {
      try {
        if (query) {
          window.sessionStorage.setItem(EXERCISE_SESSION_KEY, query);
        } else {
          window.sessionStorage.removeItem(EXERCISE_SESSION_KEY);
        }
      } catch (error) {
        // Without session storage the exercise lives in the address alone.
      }
    }

    // exerciseOf reads an exercise from a query string against the catalog:
    // the keys in scope, the selected ones of them, and the partial ones of
    // those.
    function exerciseOf(items, search) {
      const scope = scopeFromAddress(items, search);
      const selected = new Set(
        new URLSearchParams(search).getAll('key')
          .map((key) => String(key || '').trim())
          .filter((key) => scope.has(key))
      );
      const partial = new Set(Array.from(partialFromAddress(search)).filter((key) => selected.has(key)));
      return { scope, selected, partial };
    }

    // What is said of a partial exception -- what is met and what is not --
    // is kept beside the exercise for the tab's session, by key. It is not in
    // the address. The application is sent it only in the body of a request
    // for a workbook, and does not store it.
    const DESCRIPTIONS_SESSION_KEY = 'exceptions.descriptions';
    const DESCRIPTION_MAX = 1000;

    // cleanDescription is text as a cell of the workbook holds it: line ends
    // as one character, no other control characters, and no longer than the
    // application accepts.
    function cleanDescription(text) {
      return Array.from(
        String(text || '').replace(/\r\n?/g, '\n').replace(/[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/g, '')
      ).slice(0, DESCRIPTION_MAX).join('');
    }

    function storedDescriptions() {
      const descriptions = new Map();
      try {
        const stored = JSON.parse(window.sessionStorage.getItem(DESCRIPTIONS_SESSION_KEY) || '{}');
        if (stored && typeof stored === 'object' && !Array.isArray(stored)) {
          Object.keys(stored).forEach((key) => {
            const text = typeof stored[key] === 'string' ? cleanDescription(stored[key]) : '';
            if (key.trim() && text.trim()) descriptions.set(key.trim(), text);
          });
        }
      } catch (error) {
        // Nothing kept, or nothing that can be read: there are no descriptions.
      }
      return descriptions;
    }

    function storeDescriptions(descriptions) {
      try {
        const kept = {};
        descriptions.forEach((text, key) => {
          if (String(text || '').trim()) kept[key] = text;
        });
        if (Object.keys(kept).length) {
          window.sessionStorage.setItem(DESCRIPTIONS_SESSION_KEY, JSON.stringify(kept));
        } else {
          window.sessionStorage.removeItem(DESCRIPTIONS_SESSION_KEY);
        }
      } catch (error) {
        // Without session storage the descriptions last as long as the page.
      }
    }

    // descriptionsToSend is what goes with a request for a workbook: the
    // descriptions of the partial exceptions, and of nothing else.
    function descriptionsToSend(descriptions, partial) {
      const sent = [];
      descriptions.forEach((text, key) => {
        const cleaned = cleanDescription(text).trim();
        if (cleaned && partial.has(key)) sent.push([key, cleaned]);
      });
      return sent;
    }

    function exceptionLabel(partial, key) {
      return partial.has(String(key || '').trim()) ? 'Partial' : 'Full';
    }
`

// exceptionWeightScript is the weight figures for both Exceptions pages.
// internal/exceptionbook restates them, so the weight of a selection on the
// pages and in the workbook is one figure. A change here needs the same
// change there.
const exceptionWeightScript = `
    const WEIGHT_MIN = 1;
    const WEIGHT_MAX = 5;
    // A partial exception counts this share of its weight.
    const PARTIAL_FACTOR = 0.5;

    function nfrWeight(item) {
      const weight = Number(item && item.weight);
      return Number.isFinite(weight) ? weight : 0;
    }

    // scope is the Security NFRs in the scope of the exercise; selected is the
    // ones of them in the exception set, and partial the keys of the selected
    // ones that are excepted in part: those count half their weight. A key
    // counts once in each.
    function computeWeightSummary(scope, selected, partial) {
      const byWeight = [];
      for (let weight = WEIGHT_MIN; weight <= WEIGHT_MAX; weight++) {
        byWeight.push({ weight, scope: 0, selected: 0 });
      }
      const tally = (items, field, halved) => {
        const seen = new Set();
        let total = 0;
        (items || []).forEach((item) => {
          const key = nfrKey(item);
          if (!key || seen.has(key)) return;
          seen.add(key);
          const weight = nfrWeight(item);
          total += halved && halved.has(key) ? weight * PARTIAL_FACTOR : weight;
          const row = byWeight.find((entry) => entry.weight === weight);
          if (row) row[field]++;
        });
        return { total, count: seen.size };
      };
      const all = tally(scope, 'scope');
      const chosen = tally(selected, 'selected', partial);
      return {
        selectedWeight: chosen.total,
        scopeWeight: all.total,
        // A whole percentage, a half rounding up.
        share: all.total > 0 ? Math.floor(chosen.total * 100 / all.total + 0.5) : 0,
        // To two places; null when nothing is selected.
        average: chosen.count > 0 ? Math.floor(chosen.total * 100 / chosen.count + 0.5) / 100 : null,
        byWeight
      };
    }

    // touchedBy is what an NFR touches: the ratings and the threats that the
    // controls it is linked to carry. links maps an NFR's key to the ids of
    // those controls, and byID an id to its control.
    function touchedBy(item, links, byID) {
      const touched = { c: false, i: false, a: false, threats: new Set() };
      const linked = links.get(nfrKey(item));
      if (!linked) return touched;
      linked.forEach((id) => {
        const control = byID.get(id);
        if (!control) return;
        if (String(control.confidentiality || '').trim() !== '') touched.c = true;
        if (String(control.integrity || '').trim() !== '') touched.i = true;
        if (String(control.availability || '').trim() !== '') touched.a = true;
        (Array.isArray(control.threats) ? control.threats : []).forEach((threat) => {
          const name = String(threat || '').trim();
          if (name) touched.threats.add(name);
        });
      });
      return touched;
    }

    // computeWeightedCoverage is the weight that touches each rating and each
    // threat: the figures an exception set is read by. An NFR that touches
    // one counts its whole weight once, however many of its controls carry
    // it. excepted is the weight of the selected NFRs that touch it, a partial
    // exception counting half; scope is the weight of the NFRs in scope that
    // do; share is a whole percentage, a half rounding up.
    function computeWeightedCoverage(scope, selected, partial, links, byID) {
      const blank = () => ({ excepted: 0, scope: 0, share: 0 });
      const coverage = { c: blank(), i: blank(), a: blank(), threats: new Map() };
      const tally = (items, field, halved) => {
        const seen = new Set();
        (items || []).forEach((item) => {
          const key = nfrKey(item);
          if (!key || seen.has(key)) return;
          seen.add(key);
          const weight = halved && halved.has(key) ? nfrWeight(item) * PARTIAL_FACTOR : nfrWeight(item);
          const touched = touchedBy(item, links, byID);
          if (touched.c) coverage.c[field] += weight;
          if (touched.i) coverage.i[field] += weight;
          if (touched.a) coverage.a[field] += weight;
          touched.threats.forEach((threat) => {
            if (!coverage.threats.has(threat)) coverage.threats.set(threat, blank());
            coverage.threats.get(threat)[field] += weight;
          });
        });
      };
      tally(scope, 'scope');
      tally(selected, 'excepted', partial);
      [coverage.c, coverage.i, coverage.a, ...coverage.threats.values()].forEach((entry) => {
        entry.share = entry.scope > 0 ? Math.floor(entry.excepted * 100 / entry.scope + 0.5) : 0;
      });
      return coverage;
    }

    function weightedText(entry) {
      return entry.excepted + ' of ' + entry.scope + ' (' + entry.share + '%)';
    }

    function ciaWeightLine(coverage) {
      return 'CIA by weight: C: ' + weightedText(coverage.c) + ' | I: ' + weightedText(coverage.i) + ' | A: ' + weightedText(coverage.a);
    }

    // The threats the exceptions touch, in the order of every threat list
    // here: the six of threatDisplayOrder, then the others by name.
    function threatWeightEntries(coverage) {
      const touched = Array.from(coverage.threats.keys()).filter((threat) => coverage.threats.get(threat).excepted > 0);
      return [
        ...threatDisplayOrder.filter((threat) => touched.includes(threat)),
        ...touched.filter((threat) => !threatDisplayOrder.includes(threat))
          .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
      ].map((threat) => threat + ': ' + weightedText(coverage.threats.get(threat)));
    }

    function threatWeightLine(coverage) {
      const entries = threatWeightEntries(coverage);
      return 'Threats by weight: ' + (entries.length ? entries.join(' | ') : 'no linked threat mappings');
    }

    // ciaLetters is the ratings a control carries, as their letters. A
    // rating it does not carry is left out, so one with none gives nothing.
    function ciaLetters(control) {
      return [['C', control.confidentiality], ['I', control.integrity], ['A', control.availability]]
        .filter((rating) => String(rating[1] || '').trim() !== '')
        .map((rating) => rating[0])
        .join(' | ');
    }

    function weightSummaryLines(summary) {
      return [
        'Weight of the selection: ' + summary.selectedWeight + ' of ' + summary.scopeWeight + ' (' + summary.share + '%)',
        'Average weight of a selected NFR: ' + (summary.average === null ? '-' : summary.average.toFixed(2)),
        'By weight (selected of scope): ' + summary.byWeight
          .map((entry) => entry.weight + ': ' + entry.selected + ' of ' + entry.scope)
          .join(' | ')
      ];
    }
`

func exceptionsPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Exceptions · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.92);
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
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
      box-shadow: 0 24px 60px rgba(28,36,49,0.12);
    }
    .tabs { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 20px; }
    .tab {
      padding: 10px 14px;
      border-radius: 999px;
      background: #efe6d6;
      border: 1px solid var(--line);
      color: var(--ink);
      text-decoration: none;
      font-family: Arial, sans-serif;
      font-size: 13px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    .tab.active { background: #e1d0b7; border-color: #b89d78; }
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; }
    p { color: var(--muted); }
    .layout {
      display: grid;
      grid-template-columns: minmax(0, 1.2fr) minmax(320px, 0.8fr);
      gap: 18px;
      margin-top: 18px;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      overflow: hidden;
    }
    .panel-header {
      padding: 14px 16px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .panel-body { padding: 14px; }
    .toolbar {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-bottom: 12px;
    }
    .toolbar input[type="search"] { flex: 1 1 220px; }
    .toolbar select { flex: 0 1 220px; }
    .toolbar-actions { display: flex; gap: 10px; }
    .toolbar-actions button { white-space: nowrap; }
    .toolbar-actions [hidden] { display: none !important; }
    .steps {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 10px;
      margin-bottom: 12px;
    }
    .steps button { white-space: nowrap; }
    .steps button[aria-pressed="true"] { outline: 2px solid var(--accent); outline-offset: 1px; }
    .steps .scope-line { font-family: Arial, sans-serif; font-size: 13px; }
    input[type="search"],
    select {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 12px 14px;
      font: inherit;
      background: var(--bg);
    }
    button {
      border: 0;
      border-radius: 999px;
      padding: 10px 14px;
      background: #dfc4b4;
      color: var(--ink);
      font: inherit;
      cursor: pointer;
    }
    .secondary {
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    /* Both lists end at the bottom of the window: the subtracted height is
       what sits above each plus the page's bottom padding. */
    .list {
      max-height: max(240px, calc(100vh - 304px));
      overflow: auto;
      display: grid;
      gap: 8px;
    }
    .nfr-row {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: #000;
      color: #fff;
      padding: 10px 12px;
      display: grid;
      gap: 6px;
    }
    .nfr-row label {
      display: flex;
      align-items: flex-start;
      gap: 10px;
      cursor: pointer;
    }
    /* Full or Partial, beside the checkbox of an NFR that is excepted. */
    .nfr-row select { width: auto; flex: 0 0 auto; padding: 2px 6px; }
    .nfr-row select[hidden] { display: none !important; }
    .nfr-row input[type="checkbox"] {
      margin-top: 2px;
    }
    .nfr-meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
    }
    .selected-list {
      display: grid;
      gap: 8px;
      max-height: max(240px, calc(100vh - 256px));
      overflow: auto;
    }
    .selected-item {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: #000;
      color: #fff;
      padding: 10px 12px;
      display: flex;
      justify-content: space-between;
      gap: 10px;
      align-items: flex-start;
    }
    .selected-actions { display: flex; flex-direction: column; gap: 6px; align-items: stretch; }
    .exception-description { display: grid; gap: 4px; margin-top: 8px; }
    .exception-description[hidden] { display: none; }
    .exception-description span {
      color: #c8c8c8;
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .exception-description textarea { width: 100%; min-height: 54px; resize: vertical; font-family: Arial, sans-serif; }
    .selected-actions select { width: auto; }
    .selected-main {
      flex: 1;
      min-width: 0;
    }
    .nfr-detail-link {
      color: #fff;
      font-weight: bold;
      text-decoration: none;
    }
    .nfr-detail-link:hover {
      text-decoration: underline;
    }
    .linked-controls {
      display: grid;
      gap: 8px;
      margin-top: 10px;
    }
    .linked-controls-title {
      color: #c8c8c8;
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .linked-control-card {
      display: block;
      border: 1px solid #444;
      border-radius: 10px;
      background: var(--surface-strong);
      color: var(--ink);
      padding: 10px 12px;
      text-decoration: none;
    }
    .linked-control-card:hover {
      border-color: #b89d78;
      background: var(--hover);
    }
    .linked-control-meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
      margin-top: 4px;
    }
    .nfr-row .nfr-meta,
    .selected-item .nfr-meta {
      color: #c8c8c8;
    }
    .muted { color: var(--muted); }
    @media (max-width: 980px) {
      .layout { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/exceptions") + `

    <h1>Exceptions</h1>
    <p>Set the scope of the exercise, then select the Security NFRs of that scope that make up the exception set.</p>

    <section class="layout">
      <section class="panel">
        <div class="panel-header">Security NFR Catalog</div>
        <div class="panel-body">
          <div class="steps">
            <button id="scopeStepBtn" class="secondary" type="button" aria-pressed="false">1. Scope</button>
            <button id="exceptionsStepBtn" class="secondary" type="button" aria-pressed="false">2. Exceptions</button>
            <span id="scopeSummary" class="scope-line muted">Scope: loading...</span>
          </div>
          <div class="toolbar">
            <input id="searchInput" type="search" placeholder="Search NFR ID, summary, domain, or key">
            <select id="domainFilter" aria-label="Filter by Security NFR domain">
              <option value="">All Domains</option>
            </select>
            <div class="toolbar-actions">
              <button id="selectFilteredBtn" type="button">Select All</button>
              <button id="clearFilteredBtn" class="secondary" type="button">Clear Filtered</button>
              <button id="exportScopeBtn" class="secondary" type="button">Export Scope</button>
              <button id="importScopeBtn" class="secondary" type="button">Import Scope</button>
              <input id="importScopeFile" type="file" accept=".json,application/json" hidden>
            </div>
          </div>
          <div id="status" class="muted">Loading Security NFRs...</div>
          <div id="nfrList" class="list"></div>
        </div>
      </section>

      <aside class="panel">
        <div class="panel-header">Selected Security NFRs</div>
        <div class="panel-body">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;gap:10px;">
            <strong id="selectedCount">0 selected</strong>
            <div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end;">
              <button id="openDetailsBtn" class="secondary" type="button">Open Details</button>
              <button id="printDetailsBtn" type="button">Print Details</button>
              <button id="clearAllBtn" class="secondary" type="button">Clear All</button>
            </div>
          </div>
          <div id="selectedList" class="selected-list">
            <div class="muted">No Security NFRs selected.</div>
          </div>
        </div>
      </aside>
    </section>
  </main>

  <script>
    const state = {
      items: [],
      // The step the catalog list is on: 'scope' ticks the Security NFRs the
      // exercise covers, 'exceptions' the ones of them the system does not meet.
      step: 'scope',
      scope: new Set(),
      selected: new Map(),
      // The keys of the selected NFRs that are excepted in part.
      partial: new Set(),
      // What is said of the exceptions, by key. It shows, and goes into the
      // workbook, for the partial ones.
      descriptions: new Map(),
      query: '',
      domain: '',
      linksByNFRKey: new Map(),
      controlsByID: new Map(),
      dataLoaded: false
    };

    const scopeStepBtn = document.getElementById('scopeStepBtn');
    const exceptionsStepBtn = document.getElementById('exceptionsStepBtn');
    const scopeSummary = document.getElementById('scopeSummary');
    const searchInput = document.getElementById('searchInput');
    const domainFilter = document.getElementById('domainFilter');
    const selectFilteredBtn = document.getElementById('selectFilteredBtn');
    const clearFilteredBtn = document.getElementById('clearFilteredBtn');
    const exportScopeBtn = document.getElementById('exportScopeBtn');
    const importScopeBtn = document.getElementById('importScopeBtn');
    const importScopeFile = document.getElementById('importScopeFile');
    const openDetailsBtn = document.getElementById('openDetailsBtn');
    const printDetailsBtn = document.getElementById('printDetailsBtn');
    const clearAllBtn = document.getElementById('clearAllBtn');
    const status = document.getElementById('status');
    const nfrList = document.getElementById('nfrList');
    const selectedCount = document.getElementById('selectedCount');
    const selectedList = document.getElementById('selectedList');

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }
` + exceptionScopeScript + exceptionWeightScript + `
    function scopeItems() {
      return state.items.filter((item) => state.scope.has(nfrKey(item)));
    }

    // The exception set is drawn from the scope: taking an NFR out of scope
    // takes it out of the set.
    function dropOutOfScope() {
      Array.from(state.selected.keys()).forEach((key) => {
        if (!state.scope.has(String(key).trim())) state.selected.delete(key);
      });
    }

    // Only an exception can be partial.
    function dropPartialWithoutException() {
      const selected = new Set(Array.from(state.selected.keys()).map((key) => String(key).trim()));
      Array.from(state.partial).forEach((key) => {
        if (!selected.has(key)) state.partial.delete(key);
      });
    }

    // Only an exception has a description.
    function dropDescriptionsWithoutException() {
      const selected = new Set(Array.from(state.selected.keys()).map((key) => String(key).trim()));
      Array.from(state.descriptions.keys()).forEach((key) => {
        if (!selected.has(key)) state.descriptions.delete(key);
      });
    }

    function filteredItems() {
      const q = state.query.trim().toUpperCase();
      const selectedDomain = state.domain.trim().toUpperCase();
      const listed = state.step === 'scope' ? state.items : scopeItems();
      return listed.filter((item) => {
        if (selectedDomain && String(item.domain || '').trim().toUpperCase() !== selectedDomain) {
          return false;
        }
        if (!q) return true;
        return [
          item.id,
          item.summary,
          item.domain,
          item.key,
          item.issue_type
        ].some((value) => String(value || '').toUpperCase().includes(q));
      });
    }

    function renderDomainOptions() {
      const domains = Array.from(new Set(
        state.items
          .map((item) => String(item.domain || '').trim())
          .filter((value) => value !== '')
      )).sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }));

      domainFilter.innerHTML = '<option value="">All Domains</option>' +
        domains.map((domain) => {
          const selected = state.domain === domain ? ' selected' : '';
          return '<option value="' + esc(domain) + '"' + selected + '>' + esc(domain) + '</option>';
        }).join('');
    }

    function linkedControlsForNFR(item) {
      const key = String(item.key || '').trim();
      if (!key) return [];
      const linkedControls = state.linksByNFRKey.get(key);
      if (!linkedControls) return [];

      return Array.from(linkedControls)
        .map((controlID) => {
          const control = state.controlsByID.get(controlID);
          return {
            id: controlID,
            name: control && control.name ? control.name : '',
            confidentiality: control && control.confidentiality ? control.confidentiality : '',
            integrity: control && control.integrity ? control.integrity : '',
            availability: control && control.availability ? control.availability : ''
          };
        })
        .sort((a, b) => String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' }));
    }

    function selectedItemsSorted() {
      const items = Array.from(state.selected.values());
      items.sort((a, b) => {
        const left = parseFloat(String(a.id || '').trim());
        const right = parseFloat(String(b.id || '').trim());
        if (!Number.isNaN(left) && !Number.isNaN(right) && left !== right) return left - right;
        return String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' });
      });
      return items;
    }

    function selectedDetailURL() {
      const params = new URLSearchParams();
      selectedItemsSorted().forEach((item) => {
        const key = String(item.key || '').trim();
        if (key) params.append('key', key);
      });
      appendPartial(params);
      appendScope(params, state.scope, state.items);
      const query = params.toString();
      return '/exceptions/detail' + (query ? '?' + query : '');
    }

    function appendPartial(params) {
      dropPartialWithoutException();
      selectedItemsSorted().forEach((item) => {
        const key = String(item.key || '').trim();
        if (key && state.partial.has(key)) params.append('partial', key);
      });
    }

    // The address of this page follows the scope and the selection, so a
    // reload or a bookmark comes back to the same exercise.
    function syncAddress() {
      if (!state.dataLoaded) return;
      const params = new URLSearchParams();
      appendScope(params, state.scope, state.items);
      selectedItemsSorted().forEach((item) => {
        const key = String(item.key || '').trim();
        if (key) params.append('key', key);
      });
      appendPartial(params);
      const query = params.toString();
      window.history.replaceState(null, '', window.location.pathname + (query ? '?' + query : ''));
      storeExercise(query);
      dropDescriptionsWithoutException();
      storeDescriptions(state.descriptions);
    }

    function renderSteps() {
      scopeStepBtn.setAttribute('aria-pressed', state.step === 'scope' ? 'true' : 'false');
      exceptionsStepBtn.setAttribute('aria-pressed', state.step === 'exceptions' ? 'true' : 'false');
      // The scope file belongs to the step that sets the scope.
      exportScopeBtn.hidden = state.step !== 'scope';
      importScopeBtn.hidden = state.step !== 'scope';
      if (!state.dataLoaded) return;
      scopeSummary.textContent = scopeLine(state.scope, state.items);
      status.textContent = state.step === 'scope'
        ? 'Step 1 of 2: tick the Security NFRs the exercise covers.'
        : 'Step 2 of 2: tick the Security NFRs of the scope that the system does not meet.';
    }

` + exceptionScopeFileScript + `
    function scopeCounts() {
      const keys = catalogKeys(state.items);
      return keys.filter((key) => state.scope.has(key)).length + ' of ' + keys.length + ' Security NFRs in scope';
    }

    function scopeFileText() {
      const seen = new Set();
      const entries = [];
      state.items.forEach((item) => {
        const key = nfrKey(item);
        if (!key || seen.has(key)) return;
        seen.add(key);
        entries.push({
          key,
          id: String(item.id || '').trim(),
          summary: String(item.summary || ''),
          in_scope: state.scope.has(key)
        });
      });
      return scopeFileTextOf(entries, new Date().toISOString());
    }

    function exportScope() {
      if (!state.dataLoaded) return;
      const text = scopeFileText();
      // What is saved has to be a file Import Scope reads.
      const check = readScopeFile(text, catalogKeys(state.items));
      if (check.error) {
        status.textContent = 'Scope not exported: ' + check.error;
        return;
      }
      const link = document.createElement('a');
      link.href = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
      link.download = 'security-nfr-scope-' + new Date().toISOString().slice(0, 10) + '.json';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(link.href);
      status.textContent = 'Scope exported to ' + link.download + ': ' + scopeCounts() + '.';
    }

    // importScopeText replaces the scope with the one in a scope file and says
    // what it did. A file that readScopeFile refuses changes nothing. An NFR
    // the file marks as not in scope, or does not list, is out of scope. The
    // exception set is left alone but for the NFRs that are no longer in scope.
    function importScopeText(text) {
      if (!state.dataLoaded) return 'Scope not imported: the catalog is not loaded.';
      const read = readScopeFile(text, catalogKeys(state.items));
      if (read.error) return 'Scope not imported: ' + read.error;
      state.scope = new Set(read.inScope);
      dropOutOfScope();
      state.step = 'scope';
      render();
      if (!read.unlisted) return 'Scope imported: ' + scopeCounts() + '.';
      return 'Scope imported: ' + scopeCounts() + '; ' + (read.unlisted === 1
        ? '1 Security NFR of the catalog is not in the file and is out of scope.'
        : read.unlisted + ' Security NFRs of the catalog are not in the file and are out of scope.');
    }

    function renderSelected() {
      const items = selectedItemsSorted();

      selectedCount.textContent = items.length + ' selected';
      if (!items.length) {
        selectedList.innerHTML = '<div class="muted">No Security NFRs selected.</div>';
        return;
      }

      selectedList.innerHTML = items.map((item) => {
        const key = String(item.key || '');
        const linkedControls = linkedControlsForNFR(item);
        const linkedControlsHTML = linkedControls.length
          ? '<div class="linked-controls">' +
              '<div class="linked-controls-title">Linked Controls</div>' +
              linkedControls.map((control) => {
                return '<a class="linked-control-card" href="/controls/detail/' + encodeURIComponent(control.id) + '" target="_blank" rel="noopener noreferrer">' +
                  '<strong>' + esc(control.id || 'N/A') + '</strong> - ' + esc(control.name || 'No control name') +
                  (ciaLetters(control) ? '<div class="linked-control-meta">' + esc(ciaLetters(control)) + '</div>' : '') +
                '</a>';
              }).join('') +
            '</div>'
          : '<div class="linked-controls">' +
              '<div class="linked-controls-title">Linked Controls</div>' +
              '<div class="linked-control-card"><strong>None</strong><div class="linked-control-meta">No linked controls found for this Security NFR.</div></div>' +
            '</div>';
        return '<div class="selected-item">' +
          '<div class="selected-main">' +
            '<a class="nfr-detail-link" href="/security-nfrs/detail/' + encodeURIComponent(key) + '" target="_blank" rel="noopener noreferrer">' + esc(item.summary || 'No summary') + '</a>' +
            '<div class="nfr-meta">Domain: ' + esc(item.domain || 'N/A') + ' | Weight: ' + esc(String(nfrWeight(item))) + '</div>' +
            // A text area drops a line break that comes first, so one is put there.
            '<label class="exception-description"' + (state.partial.has(key.trim()) ? '' : ' hidden') + '>' +
              '<span>Description of the partial exception</span>' +
              '<textarea class="exception-note" data-key="' + esc(key) + '" rows="3" maxlength="' + DESCRIPTION_MAX + '" placeholder="What is met and what is not. It goes into the Comments column of the workbook.">\n' +
                esc(state.descriptions.get(key.trim()) || '') + '</textarea>' +
            '</label>' +
            linkedControlsHTML +
          '</div>' +
          '<div class="selected-actions">' +
            '<select class="exception-kind" data-key="' + esc(key) + '" aria-label="Exception: full or partial">' +
              '<option value="full">Full</option>' +
              '<option value="partial"' + (state.partial.has(key.trim()) ? ' selected' : '') + '>Partial</option>' +
            '</select>' +
            '<button class="secondary remove-selected" data-key="' + esc(key) + '" type="button">Remove</button>' +
          '</div>' +
        '</div>';
      }).join('');

      selectedList.querySelectorAll('.exception-kind').forEach((kind) => {
        kind.addEventListener('change', () => {
          const key = String(kind.getAttribute('data-key') || '').trim();
          setPartial(key, kind.value === 'partial');
          const inList = rowKindFor(key);
          if (inList) inList.value = kind.value;
          const description = kind.closest('.selected-item').querySelector('.exception-description');
          if (description) description.hidden = kind.value !== 'partial';
          syncAddress();
        });
      });

      // Typing does not draw the panel again: the text area keeps its place.
      selectedList.querySelectorAll('.exception-note').forEach((note) => {
        note.addEventListener('input', () => {
          state.descriptions.set(String(note.getAttribute('data-key') || '').trim(), cleanDescription(note.value));
          storeDescriptions(state.descriptions);
        });
      });

      selectedList.querySelectorAll('.remove-selected').forEach((btn) => {
        btn.addEventListener('click', () => {
          const key = btn.getAttribute('data-key') || '';
          state.selected.delete(key);
          render();
        });
      });
    }

    // Full or Partial for one NFR of the list. It is the setting the Selected
    // Security NFRs panel has for the same NFR: state.partial holds it, and
    // each of the two is set from it when the other changes. It shows only
    // for an NFR that is excepted.
    function rowKindHTML(key, excepted) {
      return '<select class="row-kind" data-key="' + esc(key) + '" aria-label="Exception: full or partial"' + (excepted ? '' : ' hidden') + '>' +
        '<option value="full">Full</option>' +
        '<option value="partial"' + (state.partial.has(String(key).trim()) ? ' selected' : '') + '>Partial</option>' +
      '</select>';
    }

    function setPartial(key, partial) {
      const trimmed = String(key || '').trim();
      if (partial) {
        state.partial.add(trimmed);
      } else {
        state.partial.delete(trimmed);
      }
    }

    // The list's own choice for a key, where the list shows that NFR.
    function rowKindFor(key) {
      return Array.from(nfrList.querySelectorAll('.row-kind'))
        .find((kind) => String(kind.getAttribute('data-key') || '').trim() === String(key || '').trim());
    }

    function renderList() {
      const items = filteredItems();
      const scoping = state.step === 'scope';
      if (!items.length) {
        nfrList.innerHTML = !scoping && !state.scope.size
          ? '<div class="muted">No Security NFRs are in scope. Set the scope in step 1.</div>'
          : '<div class="muted">No Security NFRs match the current filter.</div>';
        return;
      }

      nfrList.innerHTML = items.map((item) => {
        const key = String(item.key || '');
        const checked = (scoping ? state.scope.has(nfrKey(item)) : state.selected.has(key)) ? 'checked' : '';
        return '<div class="nfr-row">' +
          '<label>' +
            '<input class="nfr-check" type="checkbox" data-key="' + esc(key) + '" ' + checked + '>' +
            (scoping ? '' : rowKindHTML(key, checked !== '')) +
            '<div>' +
              '<div><strong>' + esc(item.summary || 'No summary') + '</strong></div>' +
              '<div class="nfr-meta">Domain: ' + esc(item.domain || 'N/A') + ' | Type: ' + esc(item.issue_type || 'N/A') + ' | Key: ' + esc(key || 'N/A') + ' | Weight: ' + esc(String(nfrWeight(item))) + '</div>' +
            '</div>' +
          '</label>' +
        '</div>';
      }).join('');

      nfrList.querySelectorAll('.nfr-check').forEach((check) => {
        check.addEventListener('change', () => {
          const key = check.getAttribute('data-key') || '';
          const item = state.items.find((it) => String(it.key || '') === key);
          if (!item) return;
          if (scoping) {
            if (check.checked) {
              state.scope.add(nfrKey(item));
            } else {
              state.scope.delete(nfrKey(item));
              dropOutOfScope();
            }
          } else if (check.checked) {
            state.selected.set(key, item);
          } else {
            state.selected.delete(key);
            setPartial(key, false);
          }
          const kind = rowKindFor(key);
          if (kind) {
            kind.hidden = !check.checked;
            kind.value = state.partial.has(String(key).trim()) ? 'partial' : 'full';
          }
          renderSteps();
          renderSelected();
          syncAddress();
        });
      });

      nfrList.querySelectorAll('.row-kind').forEach((kind) => {
        kind.addEventListener('change', () => {
          setPartial(kind.getAttribute('data-key'), kind.value === 'partial');
          renderSelected();
          syncAddress();
        });
      });
    }

    function render() {
      renderSteps();
      renderList();
      renderSelected();
      syncAddress();
    }

    async function load() {
      status.textContent = 'Loading Security NFRs...';
      try {
        const [nfrResp, linksResp, controlsResp] = await Promise.all([
          fetch('/security-nfrs/data'),
          fetch('/security-nfrs/links/data'),
          fetch('/controls/data')
        ]);
        if (!nfrResp.ok) throw new Error('/security-nfrs/data HTTP ' + nfrResp.status);
        if (!linksResp.ok) throw new Error('/security-nfrs/links/data HTTP ' + linksResp.status);
        if (!controlsResp.ok) throw new Error('/controls/data HTTP ' + controlsResp.status);

        const [nfrItems, linkRows, controls] = await Promise.all([
          nfrResp.json(),
          linksResp.json(),
          controlsResp.json()
        ]);

        state.items = nfrItems;
        state.linksByNFRKey = new Map();
        linkRows.forEach((row) => {
          if (!row || !row.matched) return;
          const nfrKey = String(row.nfr_key || '').trim();
          const controlID = String(row.control_id || '').trim();
          if (!nfrKey || !controlID) return;
          let set = state.linksByNFRKey.get(nfrKey);
          if (!set) {
            set = new Set();
            state.linksByNFRKey.set(nfrKey, set);
          }
          set.add(controlID);
        });

        state.controlsByID = new Map();
        controls.forEach((control) => {
          const controlID = String(control.id || '').trim();
          if (controlID) state.controlsByID.set(controlID, control);
        });

        // An address that names a scope or a selection opens on the exceptions
        // of that exercise; a plain one starts at the scope.
        // A plain address comes back to the exercise of this tab's session.
        const plain = new URLSearchParams(window.location.search);
        if (!plain.has('scope') && !plain.has('key') && !plain.has('partial') && storedExercise()) {
          window.history.replaceState(null, '', window.location.pathname + '?' + storedExercise());
        }
        const address = new URLSearchParams(window.location.search);
        state.scope = scopeFromAddress(state.items);
        state.selected = new Map();
        new Set(address.getAll('key').map((key) => String(key || '').trim())).forEach((key) => {
          const item = state.items.find((it) => nfrKey(it) === key);
          if (item && state.scope.has(key)) state.selected.set(String(item.key || ''), item);
        });
        state.partial = partialFromAddress();
        dropPartialWithoutException();
        state.descriptions = storedDescriptions();
        state.step = address.has('scope') || state.selected.size ? 'exceptions' : 'scope';

        state.dataLoaded = true;
        renderDomainOptions();
        render();
      } catch (error) {
        status.textContent = 'Failed to load Exceptions datasets: ' + error.message;
        state.dataLoaded = true;
        nfrList.innerHTML = '<div class="muted">Could not load Security NFR dataset.</div>';
      }
    }

    searchInput.addEventListener('input', () => {
      state.query = searchInput.value;
      renderList();
    });

    domainFilter.addEventListener('change', () => {
      state.domain = domainFilter.value;
      renderList();
    });

    scopeStepBtn.addEventListener('click', () => {
      state.step = 'scope';
      render();
    });

    exceptionsStepBtn.addEventListener('click', () => {
      state.step = 'exceptions';
      render();
    });

    // Select All and Clear Filtered work on the step the list is on.
    selectFilteredBtn.addEventListener('click', () => {
      filteredItems().forEach((item) => {
        const key = String(item.key || '');
        if (!key) return;
        if (state.step === 'scope') {
          state.scope.add(nfrKey(item));
        } else {
          state.selected.set(key, item);
        }
      });
      render();
    });

    clearFilteredBtn.addEventListener('click', () => {
      filteredItems().forEach((item) => {
        if (state.step === 'scope') {
          state.scope.delete(nfrKey(item));
        } else {
          state.selected.delete(String(item.key || ''));
        }
      });
      dropOutOfScope();
      render();
    });

    exportScopeBtn.addEventListener('click', exportScope);

    importScopeBtn.addEventListener('click', () => importScopeFile.click());

    importScopeFile.addEventListener('change', async () => {
      const files = importScopeFile.files;
      if (!files || !files.length) return;
      // The name, size and type are checked before the file is read at all.
      const refusal = files.length === 1
        ? scopeFileRefusal(files[0].name, files[0].size, files[0].type)
        : 'choose one file.';
      const message = refusal ? 'Scope not imported: ' + refusal : importScopeText(await files[0].text());
      // Cleared so that choosing the same file again is a change.
      importScopeFile.value = '';
      status.textContent = message;
    });

    clearAllBtn.addEventListener('click', () => {
      state.selected.clear();
      render();
    });

    openDetailsBtn.addEventListener('click', () => {
      if (!selectedItemsSorted().length) {
        window.alert('Select at least one Security NFR before opening the detail page.');
        return;
      }
      window.open(selectedDetailURL(), '_blank', 'noopener,noreferrer');
    });

    printDetailsBtn.addEventListener('click', () => {
      if (!selectedItemsSorted().length) {
        window.alert('Select at least one Security NFR before printing.');
        return;
      }
      window.open(selectedDetailURL() + '&print=1', '_blank', 'noopener,noreferrer');
    });

    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func exceptionsDetailPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Exception Detail · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.92);
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
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
      box-shadow: 0 24px 60px rgba(28,36,49,0.12);
    }
    .tabs { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 20px; }
    .tab {
      padding: 10px 14px;
      border-radius: 999px;
      background: #efe6d6;
      border: 1px solid var(--line);
      color: var(--ink);
      text-decoration: none;
      font-family: Arial, sans-serif;
      font-size: 13px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    .tab.active { background: #e1d0b7; border-color: #b89d78; }
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; }
    p { color: var(--muted); }
    .topbar {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      margin-bottom: 18px;
      flex-wrap: wrap;
    }
    .actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    button, .button-link {
      border: 0;
      border-radius: 999px;
      padding: 10px 14px;
      background: #dfc4b4;
      color: var(--ink);
      font: inherit;
      cursor: pointer;
      text-decoration: none;
      display: inline-flex;
      align-items: center;
      justify-content: center;
    }
    .button-link.secondary, button.secondary {
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    .summary-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 18px;
    }
    .card {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      padding: 16px;
    }
    .card h2 {
      margin: 0 0 8px;
      font-size: 12px;
      color: var(--muted);
      font-family: Arial, sans-serif;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .summary-value {
      font-family: Arial, sans-serif;
      font-size: 14px;
      line-height: 1.6;
    }
    /* A lesser figure: the total weight, beside what the exceptions touch. */
    .card.minor .summary-value { font-size: 11px !important; color: var(--muted); }
    .detail-list {
      display: grid;
      grid-template-columns: minmax(0, 1fr);
      gap: 14px;
    }
    .detail-card {
      border: 1px solid rgba(215,206,191,0.9);
      overflow-wrap: anywhere;
      border-radius: 18px;
      background: var(--panel);
      padding: 18px;
    }
    .detail-card h3 {
      margin: 0 0 4px;
      font-size: 14px;
    }
    .meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 13px;
      margin-bottom: 14px;
    }
    .section-title {
      margin-top: 16px;
      margin-bottom: 8px;
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .detail-text {
      white-space: pre-wrap;
      line-height: 1.55;
    }
    .linked-controls {
      display: grid;
      grid-template-columns: minmax(0, 1fr);
      gap: 8px;
    }
    .linked-control-card {
      display: block;
      border: 1px solid var(--line);
      border-radius: 12px;
      background: var(--surface-strong);
      color: var(--ink);
      text-decoration: none;
      padding: 12px 14px;
    }
    .linked-control-card:hover {
      border-color: #b89d78;
      background: var(--hover);
    }
    .linked-control-meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
      margin-top: 4px;
    }
    .empty {
      border: 1px dashed var(--line);
      border-radius: 14px;
      padding: 18px;
      color: var(--muted);
      background: var(--surface-strong);
    }
    @media (max-width: 980px) {
      .summary-grid { grid-template-columns: minmax(0, 1fr); }
    }
    @media print {
      body { background: #fff; }
      main {
        max-width: none;
        margin: 0;
        border: 0;
        border-radius: 0;
        box-shadow: none;
        padding: 0;
        background: #fff;
      }
      .tabs, .actions { display: none; }
      .card, .detail-card, .linked-control-card { break-inside: avoid; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/exceptions/detail") + `

    <div class="topbar">
      <div>
        <h1>Exception Detail</h1>
        <p>Detailed view of the selected Security NFR exception set.</p>
      </div>
      <div class="actions">
        <a class="button-link secondary" href="/exceptions">Back to Exceptions</a>
        <button id="printBtn" type="button">Print</button>
      </div>
    </div>

    <div id="summary" class="summary-grid">
      <div class="card"><h2>Status</h2><div class="summary-value">Loading exception details...</div></div>
    </div>
    <div id="detailList" class="detail-list"></div>
  </main>

  <script>
    const summary = document.getElementById('summary');
    const detailList = document.getElementById('detailList');
    const printBtn = document.getElementById('printBtn');
    const threatDisplayOrder = [
      'Spoofing',
      'Tampering',
      'Repudiation',
      'Information Disclosure',
      'Denial of Service',
      'Elevation of Privilege'
    ];

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }
` + exceptionScopeScript + exceptionWeightScript + `

    function textOrNA(value) {
      const text = String(value || '').trim();
      return text === '' ? 'N/A' : text;
    }

    function formatThreatList(threats) {
      const items = Array.isArray(threats)
        ? threats.map((threat) => String(threat || '').trim()).filter((threat) => threat !== '')
        : [];
      return items.length ? items.join(', ') : 'None';
    }

    function selectedKeys() {
      return Array.from(new URLSearchParams(window.location.search).getAll('key'))
        .map((key) => String(key || '').trim())
        .filter((key) => key !== '');
    }

    function orderedThreatSummaryEntries(controls) {
      const counts = new Map();
      controls.forEach((control) => {
        if (!control || !Array.isArray(control.threats)) return;
        new Set(
          control.threats
            .map((threat) => String(threat || '').trim())
            .filter((threat) => threat !== '')
        ).forEach((threat) => {
          counts.set(threat, (counts.get(threat) || 0) + 1);
        });
      });
      if (!counts.size) return [];

      const orderedThreats = [
        ...threatDisplayOrder.filter((threat) => counts.has(threat)),
        ...Array.from(counts.keys())
          .filter((threat) => !threatDisplayOrder.includes(threat))
          .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
      ];
      return orderedThreats.map((threat) => ({ threat, count: counts.get(threat) }));
    }

    function linkedControlsForKey(key, linksByNFRKey, controlsByID) {
      const linkedControls = linksByNFRKey.get(key);
      if (!linkedControls) return [];
      return Array.from(linkedControls)
        .map((controlID) => {
          const control = controlsByID.get(controlID);
          return {
            id: controlID,
            name: control && control.name ? control.name : '',
            confidentiality: control && control.confidentiality ? control.confidentiality : '',
            integrity: control && control.integrity ? control.integrity : '',
            availability: control && control.availability ? control.availability : '',
            threats: control && Array.isArray(control.threats) ? control.threats : []
          };
        })
        .sort((a, b) => String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' }));
    }

    async function load() {
      const keys = selectedKeys();
      if (!keys.length) {
        summary.innerHTML = '<div class="card"><h2>Status</h2><div class="summary-value">No Security NFR keys were provided.</div></div>';
        detailList.innerHTML = '<div class="empty">Open this page from <a href="/exceptions">/exceptions</a> after selecting one or more Security NFRs.</div>';
        return;
      }

      try {
        const [nfrResp, linksResp, controlsResp] = await Promise.all([
          fetch('/security-nfrs/data'),
          fetch('/security-nfrs/links/data'),
          fetch('/controls/data')
        ]);
        if (!nfrResp.ok) throw new Error('/security-nfrs/data HTTP ' + nfrResp.status);
        if (!linksResp.ok) throw new Error('/security-nfrs/links/data HTTP ' + linksResp.status);
        if (!controlsResp.ok) throw new Error('/controls/data HTTP ' + controlsResp.status);

        const [nfrItems, linkRows, controls] = await Promise.all([
          nfrResp.json(),
          linksResp.json(),
          controlsResp.json()
        ]);

        const itemByKey = new Map();
        (nfrItems || []).forEach((item) => {
          const key = String(item.key || '').trim();
          if (key) itemByKey.set(key, item);
        });

        const linksByNFRKey = new Map();
        (linkRows || []).forEach((row) => {
          if (!row || !row.matched) return;
          const nfrKey = String(row.nfr_key || '').trim();
          const controlID = String(row.control_id || '').trim();
          if (!nfrKey || !controlID) return;
          let set = linksByNFRKey.get(nfrKey);
          if (!set) {
            set = new Set();
            linksByNFRKey.set(nfrKey, set);
          }
          set.add(controlID);
        });

        const controlsByID = new Map();
        (controls || []).forEach((control) => {
          const controlID = String(control.id || '').trim();
          if (controlID) controlsByID.set(controlID, control);
        });

        // An NFR outside the scope is not an exception of the exercise.
        const scope = scopeFromAddress(nfrItems);
        const partial = partialFromAddress();
        const items = keys
          .filter((key) => scope.has(key))
          .map((key) => itemByKey.get(key))
          .filter((item) => !!item)
          .sort((a, b) => String(a.id || '').localeCompare(String(b.id || ''), undefined, { numeric: true, sensitivity: 'base' }));

        const selectedControlIDs = new Set();
        items.forEach((item) => {
          linkedControlsForKey(String(item.key || '').trim(), linksByNFRKey, controlsByID)
            .forEach((control) => selectedControlIDs.add(String(control.id || '').trim()));
        });

        const selectedControls = Array.from(selectedControlIDs)
          .map((controlID) => controlsByID.get(controlID))
          .filter((control) => !!control);

        const cia = {
          controls: selectedControlIDs.size,
          confidentiality: selectedControls.filter((control) => String(control.confidentiality || '').trim() !== '').length,
          integrity: selectedControls.filter((control) => String(control.integrity || '').trim() !== '').length,
          availability: selectedControls.filter((control) => String(control.availability || '').trim() !== '').length
        };
        const threats = orderedThreatSummaryEntries(selectedControls);
        const scopeItems = (nfrItems || []).filter((item) => scope.has(nfrKey(item)));
        const weights = weightSummaryLines(computeWeightSummary(scopeItems, items, partial));
        const coverage = computeWeightedCoverage(scopeItems, items, partial, linksByNFRKey, controlsByID);
        const threatWeights = threatWeightEntries(coverage);

        summary.innerHTML =
          '<div class="card"><h2>Selection</h2><div class="summary-value">' + esc(scopeLine(scope, nfrItems)) + '<br>Selected Security NFRs: ' + esc(String(items.length)) + '<br>Linked Controls: ' + esc(String(cia.controls)) + '</div></div>' +
          '<div class="card"><h2>CIA by weight</h2><div class="summary-value">C: ' + esc(weightedText(coverage.c)) + '<br>I: ' + esc(weightedText(coverage.i)) + '<br>A: ' + esc(weightedText(coverage.a)) + '</div></div>' +
          '<div class="card"><h2>Threats by weight</h2><div class="summary-value">' + esc(threatWeights.length ? threatWeights.join(' | ') : 'No linked threat mappings') + '</div></div>' +
          '<div class="card"><h2>CIA Coverage</h2><div class="summary-value">C: ' + esc(String(cia.confidentiality)) + '<br>I: ' + esc(String(cia.integrity)) + '<br>A: ' + esc(String(cia.availability)) + '</div></div>' +
          '<div class="card"><h2>Threat Coverage</h2><div class="summary-value">' + esc(threats.length ? threats.map((entry) => entry.threat + ': ' + entry.count).join(' | ') : 'No linked threat mappings') + '</div></div>' +
          '<div class="card minor"><h2>Total weight (minor)</h2><div class="summary-value">' + weights.map((line) => esc(line)).join('<br>') + '</div></div>' +
          '<div class="card"><h2>Status</h2><div class="summary-value">Loaded ' + esc(String(items.length)) + ' selected Security NFR detail entries.</div></div>';

        if (!items.length) {
          detailList.innerHTML = '<div class="empty">None of the requested Security NFR keys were found in the scope.</div>';
          return;
        }

        detailList.innerHTML = items.map((item) => {
          const key = String(item.key || '').trim();
          const linkedControls = linkedControlsForKey(key, linksByNFRKey, controlsByID);
          const linkedControlsHTML = linkedControls.length
            ? linkedControls.map((control) => {
                return '<a class="linked-control-card" href="/controls/detail/' + encodeURIComponent(control.id) + '" target="_blank" rel="noopener noreferrer">' +
                  '<strong>' + esc(control.id || 'N/A') + '</strong> - ' + esc(control.name || 'No control name') +
                  (ciaLetters(control) ? '<div class="linked-control-meta">' + esc(ciaLetters(control)) + '</div>' : '') +
                  '<div class="linked-control-meta">Threats: ' + esc(formatThreatList(control.threats)) + '</div>' +
                '</a>';
              }).join('')
            : '<div class="empty">No linked controls found for this Security NFR.</div>';

          return '<article class="detail-card">' +
            '<h3><a href="/security-nfrs/detail/' + encodeURIComponent(key) + '" target="_blank" rel="noopener noreferrer">' + esc(item.summary || 'No summary') + '</a></h3>' +
            '<div class="meta">Domain: ' + esc(item.domain || 'N/A') + ' | Type: ' + esc(item.issue_type || 'N/A') + ' | Key: ' + esc(key || 'N/A') + ' | Weight: ' + esc(String(nfrWeight(item))) + ' | Exception: ' + esc(exceptionLabel(partial, key)) + '</div>' +
            '<div class="section-title">Linked Controls</div>' +
            '<div class="linked-controls">' + linkedControlsHTML + '</div>' +
            '<div class="section-title">NIST Mapping</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.nist_mapping)) + '</div>' +
            '<div class="section-title">Description</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.description)) + '</div>' +
            '<div class="section-title">Additional Details</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.additional_details)) + '</div>' +
            '<div class="section-title">Implementation</div>' +
            '<div class="detail-text">' + esc(textOrNA(item.implementation)) + '</div>' +
          '</article>';
        }).join('');

        if (new URLSearchParams(window.location.search).get('print') === '1') {
          window.setTimeout(() => window.print(), 150);
        }
      } catch (error) {
        summary.innerHTML = '<div class="card"><h2>Status</h2><div class="summary-value">Failed to load exception details: ' + esc(error.message) + '</div></div>';
        detailList.innerHTML = '<div class="empty">The detail page could not load the required datasets.</div>';
      }
    }

    printBtn.addEventListener('click', () => {
      window.print();
    });

    load();
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
