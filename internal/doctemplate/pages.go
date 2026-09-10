package doctemplate

import (
	"grc/internal/pageui"
)

// The pages are rendered as Go string literals, matching internal/app and
// internal/policydocs. See Agents.md: this app has no template engine, and
// introducing one for two pages would make these the only two that need it.

func galleryPageHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Document Templates · GRC</title>
  <style>` + templateCSS + `</style>
</head>
<body>
  <main>
    ` + pageui.Nav("/templates") + `
    <h1>Document Templates</h1>
    <p class="subtitle">Typeset a policy document as a client deliverable. Typst is the primary path; LaTeX is there for clients whose house style already is.</p>

    <section id="dirWarning" class="notice bad" hidden></section>

    <section class="panel">
      <div class="panel-header">
        <strong>Typesetting engines</strong>
        <button id="refreshEngines" type="button">Re-check</button>
      </div>
      <div id="engines" class="engine-grid"><p class="muted">Loading…</p></div>
      <p class="hint">The app renders by invoking these; it does not bundle them. Where an engine is missing, every action below still works and returns a zip of the sources plus a build script instead of a PDF.</p>
    </section>

    <section class="panel">
      <div class="panel-header"><strong>Render a policy document</strong></div>
      <div class="render-form">
        <label>Document
          <select id="docSelect"><option value="">Loading…</option></select>
        </label>
        <label>Template
          <select id="docTemplate"></select>
        </label>
        <label>PDF standard
          <select id="docStandard"></select>
        </label>
        <div class="actions">
          <button id="renderDoc" class="primary" type="button">Render</button>
          <button id="bundleDoc" type="button">Download sources</button>
        </div>
      </div>
      <p id="renderStatus" class="hint"></p>
      <pre id="renderLog" class="log" hidden></pre>
    </section>

    <h2>Templates</h2>
    <div id="templates" class="template-grid"></div>
  </main>
  <script>` + galleryJS + `</script>
</body>
</html>`
}

func brandPageHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Template Brand · GRC</title>
  <style>` + templateCSS + `</style>
</head>
<body>
  <main>
    ` + pageui.Nav("/templates/manage") + `
    <h1>Template Brand</h1>
    <p class="subtitle">Identity, palette and type for every rendered deliverable. This is templates/typst/brand.typ as a form — saved settings are applied at render time, so the file on disk stays the hand-editable original.</p>

    <div class="brand-layout">
      <form id="brandForm" class="panel">
        <div class="panel-header"><strong>Settings</strong><span id="savedAt" class="muted"></span></div>

        <fieldset>
          <legend>Identity</legend>
          <label>Firm<input name="firm" maxlength="120" required></label>
          <label>Tagline<input name="tagline" maxlength="120"></label>
          <label>Footer note<input name="footer_note" maxlength="120">
            <span class="hint">Bottom-left on every page. A provenance mark, not a disclaimer — keep it short.</span>
          </label>
        </fieldset>

        <fieldset>
          <legend>Palette</legend>
          <p class="hint">One accent, used for rules, table headers and the cover band. Body text is never coloured — multiple accents are the fastest way to make a deliverable look amateur.</p>
          <div class="colour-grid" id="colourGrid"></div>
        </fieldset>

        <fieldset>
          <legend>Type</legend>
          <p class="hint">Comma-separated fallback chains, most-wanted first. The first family present on the rendering machine wins, so a document still builds where a licensed face is absent.</p>
          <label>Serif (body)<input name="serif_stack"></label>
          <label>Sans (headings)<input name="sans_stack"></label>
          <label>Mono<input name="mono_stack"></label>
          <div class="row">
            <label>Body<input name="body_size" type="number" step="0.5" min="5" max="24"><span class="unit">pt</span></label>
            <label>Small<input name="small_size" type="number" step="0.5" min="5" max="24"><span class="unit">pt</span></label>
            <label>Micro<input name="micro_size" type="number" step="0.5" min="5" max="24"><span class="unit">pt</span></label>
          </div>
        </fieldset>

        <fieldset>
          <legend>Page</legend>
          <label>Paper<select name="paper"></select></label>
          <div class="row">
            <label>Sides<input name="margin_x" type="number" step="1" min="10" max="60"><span class="unit">mm</span></label>
            <label>Top<input name="margin_top" type="number" step="1" min="10" max="60"><span class="unit">mm</span></label>
            <label>Bottom<input name="margin_bottom" type="number" step="1" min="10" max="60"><span class="unit">mm</span></label>
          </div>
          <p class="hint">30mm sides at 11pt puts the measure near 86 characters — the top of the 45–90 band that reads comfortably. Narrowing the margins without dropping the type size makes the document denser and harder to read.</p>
        </fieldset>

        <div class="form-actions">
          <button id="saveBrand" class="primary" type="submit">Save</button>
          <button id="resetBrand" type="button" class="danger">Reset to shipped default</button>
          <span id="brandStatus" class="hint"></span>
        </div>
      </form>

      <div class="panel preview-panel">
        <div class="panel-header"><strong>Preview</strong></div>
        <div id="preview" class="preview"></div>
        <p class="hint">A swatch, not a render — it shows the palette and type, not the layout.</p>
        <div class="form-actions">
          <label class="inline">Template
            <select id="previewTemplate"></select>
          </label>
          <button id="previewPdf" type="button">Render sample PDF</button>
        </div>
        <p id="previewStatus" class="hint"></p>
        <p class="hint">Renders the bundled sample through the current <em>saved</em> settings. Save first, or you are previewing what is stored rather than what is on screen.</p>
      </div>
    </div>
  </main>
  <script>` + brandJS + `</script>
</body>
</html>`
}

const templateCSS = `
:root { color-scheme: light; --ink:#1c2431; --muted:#5e6672; --line:#d7cebf; --accent:#8b3d2e; --green:#0b5d3b; --amber:#9a6700; --red:#a32b1c; }
* { box-sizing: border-box; }
body { margin:0; font-family: Georgia, "Times New Roman", serif; color:var(--ink);
       background:linear-gradient(135deg,#f7f3eb,#ece4d6 55%,#e4d8c4); }
main { max-width:1440px; margin:24px auto; padding:24px; background:var(--bg);
       border:1px solid rgba(215,206,191,0.8); border-radius:24px; }
.tabs { display:flex; gap:10px; flex-wrap:wrap; margin-bottom:20px; }
.tab { padding:10px 14px; border-radius:999px; background:#efe6d6; border:1px solid var(--line);
       color:var(--ink); text-decoration:none; font-family:Arial,sans-serif; font-size:13px;
       letter-spacing:0.04em; text-transform:uppercase; }
.tab.active { background:#e1d0b7; border-color:#b89d78; }
h1 { margin:0 0 6px; }
h2 { margin:26px 0 12px; font-size:20px; }
.subtitle { color:var(--muted); font-family:Arial,sans-serif; font-size:14px; margin:0 0 18px; max-width:80ch; }
input, select, button, textarea { font:inherit; }
button { padding:8px 12px; border-radius:999px; border:1px solid #b89d78; background:#e1d0b7; cursor:pointer; }
button.primary { background:var(--green); color:#fff; border-color:var(--green); }
button.danger { background:var(--surface-strong); color:var(--accent); border-color:var(--accent); }
button:disabled { opacity:0.5; cursor:not-allowed; }
/* Backgrounds come from the theme variables rather than literals wherever the
   text colour does. internal/app/theme_middleware.go overrides --ink and
   --muted with !important for the active theme; a hardcoded light panel under
   those rules renders light text on a light background. */
.panel { border:1px solid var(--line); border-radius:16px; background:var(--panel,#fffdf8); margin-bottom:18px; overflow:hidden; }
.panel-header { display:flex; justify-content:space-between; align-items:center; gap:12px;
                padding:12px 16px; background:var(--surface-strong); border-bottom:1px solid var(--line);
                font-family:Arial,sans-serif; font-size:13px; text-transform:uppercase; letter-spacing:0.05em; }
/* text-transform and letter-spacing are reset because a hint often sits inside
   a <label>, which sets both for its own uppercase caption. */
.hint { color:var(--muted); font-family:Arial,sans-serif; font-size:12.5px; margin:8px 16px 14px;
        max-width:82ch; line-height:1.55; text-transform:none; letter-spacing:normal; }
.muted { color:var(--muted); font-family:Arial,sans-serif; font-size:12.5px; text-transform:none; letter-spacing:0; }
.notice { padding:12px 16px; border-radius:12px; margin-bottom:16px; font-family:Arial,sans-serif; font-size:13px; }
.notice.bad { background:var(--surface-strong); border:1px solid var(--red); color:var(--red); }

.engine-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(280px,1fr)); gap:14px; padding:16px; }
.engine { border:1px solid var(--line); border-radius:12px; padding:12px 14px;
          background:var(--surface-strong,#fff); color:var(--ink); }
.engine h3 { margin:0 0 4px; font-size:15px; font-family:Arial,sans-serif; color:var(--ink); }
.engine p { margin:6px 0 0; font-family:Arial,sans-serif; font-size:12.5px; color:var(--muted); line-height:1.5; }
.engine code { font-size:12px; background:var(--panel); padding:1px 5px; border-radius:4px; }

.render-form { display:flex; gap:14px; flex-wrap:wrap; align-items:flex-end; padding:16px; }
.render-form label, form label { display:flex; flex-direction:column; gap:4px; font-family:Arial,sans-serif;
        font-size:12px; text-transform:uppercase; letter-spacing:0.05em; color:var(--muted); }
.render-form select { min-width:220px; }
input, select { padding:8px 10px; border:1px solid var(--line); border-radius:10px; background:var(--bg); color:var(--ink);
                font-family:Arial,sans-serif; font-size:13px; text-transform:none; letter-spacing:0; }
.actions { display:flex; gap:8px; }
.log { margin:0 16px 16px; padding:12px; background:#282420; color:#e8e2d8; border-radius:10px;
       font-size:12px; line-height:1.5; white-space:pre-wrap; word-break:break-word; max-height:320px; overflow:auto; }

/* Not .card-grid: the theme's [class*="card"] rule would claim the grid
   container as well as the cards inside it, and paint the gaps between them. */
.template-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(340px,1fr)); gap:16px; }
.card { border:1px solid var(--line); border-radius:16px; background:var(--panel,#fffdf8); padding:16px;
        display:flex; flex-direction:column; gap:10px; }
.card h3 { margin:0; font-size:17px; color:var(--ink); }
.badges { display:flex; gap:6px; flex-wrap:wrap; }
/* Badges set both background and text explicitly, so they read the same on a
   white card and a black one. Following .pill in internal/policydocs: anything
   that takes only one of the two from the theme inverts when the theme flips. */
.badge { font-family:Arial,sans-serif; font-size:11px; text-transform:uppercase; letter-spacing:0.06em;
         padding:3px 8px; border-radius:999px; border:1px solid var(--line); background:var(--surface-strong); color:var(--muted); }
.badge.ok { background:var(--surface-strong); color:var(--green); border-color:var(--green); }
.badge.off { background:var(--surface-strong); color:var(--red); border-color:var(--red); }
.card p.desc { margin:0; font-family:Arial,sans-serif; font-size:13px; color:var(--muted); line-height:1.6; }
.card .actions { margin-top:auto; }

.brand-layout { display:grid; grid-template-columns:minmax(0,1.15fr) minmax(0,1fr); gap:18px; align-items:start; }
@media (max-width:1000px) { .brand-layout { grid-template-columns:1fr; } }
fieldset { border:none; border-top:1px solid var(--line); margin:0; padding:14px 16px; display:flex; flex-direction:column; gap:12px; }
legend { font-family:Arial,sans-serif; font-size:12px; text-transform:uppercase; letter-spacing:0.06em; color:var(--muted); padding:0 6px; }
fieldset .hint { margin:0; }
.row { display:flex; gap:12px; flex-wrap:wrap; }
.row label { flex:1; min-width:110px; }
.unit { font-family:Arial,sans-serif; font-size:11px; color:var(--muted); }
.colour-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:10px; }
.colour { display:flex; flex-direction:column; gap:4px; font-family:Arial,sans-serif; font-size:11px;
          text-transform:uppercase; letter-spacing:0.05em; color:var(--muted); }
.colour .pair { display:flex; gap:6px; align-items:center; }
.colour input[type=color] { width:34px; height:34px; padding:2px; border-radius:8px; cursor:pointer; }
.colour input[type=text] { width:100%; font-family:ui-monospace,Menlo,Consolas,monospace; }
.form-actions { display:flex; gap:10px; align-items:center; flex-wrap:wrap; padding:14px 16px; border-top:1px solid var(--line); }
.form-actions .hint { margin:0; }
label.inline { flex-direction:row; align-items:center; gap:8px; }

.preview { margin:16px; border:1px solid var(--line); border-radius:8px; overflow:hidden; background:#fff; }
.preview .band { height:8px; }
.preview .inner { padding:18px 20px 22px; }
.preview .wordmark { font-weight:700; letter-spacing:0.06em; font-size:12px; }
.preview .doc-title { margin:14px 0 2px; line-height:1.2; }
.preview .doc-sub { font-size:12px; }
.preview .pill { display:inline-block; margin-top:10px; padding:2px 7px; border-radius:2px; border:1px solid;
                 font-size:9px; letter-spacing:0.08em; text-transform:uppercase; }
.preview hr { border:none; border-top:1px solid; margin:14px 0; }
.preview .lbl { font-size:8.5px; letter-spacing:0.09em; text-transform:uppercase; }
.preview table { width:100%; border-collapse:collapse; margin-top:12px; font-size:11px; }
.preview th { text-align:left; padding:5px 7px; font-size:9px; letter-spacing:0.07em; text-transform:uppercase; }
.preview td { padding:5px 7px; }
.preview .body-text { margin:6px 0 0; }
.preview .foot { margin-top:16px; font-size:9px; }
`

const galleryJS = `
const $ = (id) => document.getElementById(id);
let catalog = null;

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

function renderEngines(engines) {
  $('engines').innerHTML = engines.map((e) => {
    const state = e.available
      ? '<span class="badge ok">available</span>'
      : '<span class="badge off">not installed</span>';
    const version = e.version ? '<p><code>' + esc(e.version) + '</code></p>' : '';
    const path = e.available && e.path ? '<p>' + esc(e.path) + '</p>' : '';
    const detail = e.detail ? '<p>' + esc(e.detail) + '</p>' : '';
    return '<div class="engine"><h3>' + esc(e.engine) + '</h3>' + state + version + path + detail + '</div>';
  }).join('');
}

function renderTemplates(entries) {
  $('templates').innerHTML = entries.map((t) => {
    const engineBadge = t.engine_available
      ? '<span class="badge ok">' + esc(t.engine_command) + ' ready</span>'
      : '<span class="badge off">no ' + esc(t.engine) + ' engine</span>';
    const sample = t.has_sample
      ? '<button type="button" data-sample="' + esc(t.id) + '">Render sample</button>'
      : '';
    return '<div class="card">' +
      '<h3>' + esc(t.name) + '</h3>' +
      '<div class="badges"><span class="badge">' + esc(t.kind) + '</span>' +
      '<span class="badge">' + esc(t.engine) + '</span>' + engineBadge +
      (t.generated ? '<span class="badge">generated .tex</span>' : '') + '</div>' +
      '<p class="desc">' + esc(t.description) + '</p>' +
      '<div class="actions">' + sample +
      '<button type="button" data-bundle="' + esc(t.id) + '">Download sources</button></div>' +
      '</div>';
  }).join('');
}

function fillSelect(select, options, selected) {
  select.innerHTML = options.map((o) =>
    '<option value="' + esc(o.value) + '"' + (o.value === selected ? ' selected' : '') + '>' + esc(o.label) + '</option>'
  ).join('');
}

// The document list comes from the policy module's own endpoint rather than
// being duplicated here: this page renders whatever that module says exists.
async function loadDocuments() {
  const select = $('docSelect');
  try {
    const res = await fetch('/policies/data');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const body = await res.json();
    const docs = (body.documents || body || []).filter((d) => d && d.id);
    if (!docs.length) {
      select.innerHTML = '<option value="">No policy documents yet</option>';
      return;
    }
    select.innerHTML = docs.map((d) =>
      '<option value="' + esc(d.id) + '">' + esc((d.reference ? d.reference + ' — ' : '') + (d.title || 'Untitled')) + '</option>'
    ).join('');
  } catch (err) {
    select.innerHTML = '<option value="">Could not load documents</option>';
  }
}

async function load() {
  const res = await fetch('/templates/data');
  catalog = await res.json();

  if (!catalog.templates_dir_available) {
    const warn = $('dirWarning');
    warn.hidden = false;
    warn.textContent = 'The templates directory was not found (looked for ' + catalog.templates_dir +
      '). Nothing can be rendered until this installation has one — see DOCUMENT_TEMPLATES.md.';
  }

  renderEngines(catalog.engines);
  renderTemplates(catalog.templates);

  fillSelect($('docTemplate'),
    catalog.templates.filter((t) => t.kind === 'policy').map((t) => ({ value: t.id, label: t.name })));
  fillSelect($('docStandard'), catalog.pdf_standards.map((s) => ({ value: s.value, label: s.label })));
  await loadDocuments();
}

// Rendering is a download, but it is also the one action here that can fail
// with something worth reading. So it goes through fetch rather than a plain
// link: an error arrives as JSON with the compiler log, and a success is turned
// back into a download from the blob.
async function render(url, label) {
  const status = $('renderStatus');
  const log = $('renderLog');
  log.hidden = true;
  status.textContent = label + '…';
  try {
    const res = await fetch(url);
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      status.textContent = 'Failed: ' + (body.error || ('HTTP ' + res.status));
      if (body.log) { log.textContent = body.log; log.hidden = false; }
      return;
    }
    const blob = await res.blob();
    const disposition = res.headers.get('Content-Disposition') || '';
    const match = /filename="?([^"]+)"?/.exec(disposition);
    const name = match ? match[1] : 'document.pdf';

    const href = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = href; a.download = name; a.click();
    URL.revokeObjectURL(href);

    const reason = res.headers.get('X-GRC-Reason');
    status.textContent = reason ? name + ' — ' + reason : 'Downloaded ' + name;
    const engineLog = res.headers.get('X-GRC-Render-Log');
    if (engineLog) { log.textContent = engineLog; log.hidden = false; }
  } catch (err) {
    status.textContent = 'Failed: ' + err.message;
  }
}

function documentURL(extra) {
  const params = new URLSearchParams({
    doc: $('docSelect').value,
    template: $('docTemplate').value,
    standard: $('docStandard').value,
  });
  Object.entries(extra || {}).forEach(([k, v]) => params.set(k, v));
  return '/templates/render?' + params.toString();
}

$('renderDoc').addEventListener('click', () => {
  if (!$('docSelect').value) { $('renderStatus').textContent = 'Choose a document first.'; return; }
  render(documentURL(), 'Rendering');
});
$('bundleDoc').addEventListener('click', () => {
  if (!$('docSelect').value) { $('renderStatus').textContent = 'Choose a document first.'; return; }
  render(documentURL({ bundle: '1' }), 'Building the source bundle');
});

$('templates').addEventListener('click', (event) => {
  const sample = event.target.getAttribute('data-sample');
  const bundle = event.target.getAttribute('data-bundle');
  const standard = $('docStandard').value;
  if (sample) {
    render('/templates/render?sample=1&template=' + encodeURIComponent(sample) + '&standard=' + encodeURIComponent(standard), 'Rendering the sample');
  } else if (bundle) {
    render('/templates/render?sample=1&bundle=1&template=' + encodeURIComponent(bundle) + '&standard=' + encodeURIComponent(standard), 'Building the source bundle');
  }
});

$('refreshEngines').addEventListener('click', async () => {
  const button = $('refreshEngines');
  button.disabled = true;
  try {
    const res = await fetch('/templates/engines/refresh', { method: 'POST' });
    if (res.ok) {
      const body = await res.json();
      renderEngines(body.engines);
      await load();
    }
  } finally {
    button.disabled = false;
  }
});

load();
`

const brandJS = `
const $ = (id) => document.getElementById(id);
const form = $('brandForm');
let defaults = null;

const COLOURS = [
  ['accent', 'Accent'], ['accent_dark', 'Accent dark'], ['ink', 'Ink (body text)'],
  ['muted', 'Muted (labels)'], ['rule', 'Hairlines'], ['table_head', 'Table header'],
  ['table_zebra', 'Table zebra'], ['ok', 'Status: ok'], ['warn', 'Status: warn'], ['risk', 'Status: risk'],
];

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

$('colourGrid').innerHTML = COLOURS.map(([name, label]) =>
  '<div class="colour"><span>' + esc(label) + '</span><div class="pair">' +
  '<input type="color" data-colour="' + name + '">' +
  '<input type="text" name="' + name + '" pattern="#[0-9a-fA-F]{6}" maxlength="7"></div></div>'
).join('');

// The picker and the hex field are two views of one value, so each writes the
// other. Typing a hex is what a brand guide gives you; the picker is for
// choosing one.
$('colourGrid').addEventListener('input', (event) => {
  const picker = event.target.getAttribute('data-colour');
  if (picker) {
    form.elements[picker].value = event.target.value;
  } else if (/^#[0-9a-fA-F]{6}$/.test(event.target.value)) {
    const swatch = $('colourGrid').querySelector('[data-colour="' + event.target.name + '"]');
    if (swatch) swatch.value = event.target.value;
  }
  updatePreview();
});
form.addEventListener('input', updatePreview);

function fill(brand) {
  ['firm', 'tagline', 'footer_note', 'body_size', 'small_size', 'micro_size',
   'margin_x', 'margin_top', 'margin_bottom', 'paper'].forEach((name) => {
    if (form.elements[name]) form.elements[name].value = brand[name];
  });
  COLOURS.forEach(([name]) => {
    form.elements[name].value = brand[name];
    const swatch = $('colourGrid').querySelector('[data-colour="' + name + '"]');
    if (swatch) swatch.value = brand[name];
  });
  ['serif_stack', 'sans_stack', 'mono_stack'].forEach((name) => {
    form.elements[name].value = (brand[name] || []).join(', ');
  });
  $('savedAt').textContent = brand.updated_at
    ? 'Saved ' + brand.updated_at + (brand.updated_by ? ' by ' + brand.updated_by : '')
    : 'Using the shipped default';
  updatePreview();
}

function collect() {
  const value = (name) => form.elements[name].value.trim();
  const number = (name) => parseFloat(form.elements[name].value);
  const stack = (name) => value(name).split(',').map((s) => s.trim()).filter(Boolean);
  const brand = {
    firm: value('firm'), tagline: value('tagline'), footer_note: value('footer_note'),
    serif_stack: stack('serif_stack'), sans_stack: stack('sans_stack'), mono_stack: stack('mono_stack'),
    body_size: number('body_size'), small_size: number('small_size'), micro_size: number('micro_size'),
    paper: value('paper'), margin_x: number('margin_x'),
    margin_top: number('margin_top'), margin_bottom: number('margin_bottom'),
  };
  COLOURS.forEach(([name]) => { brand[name] = value(name).toLowerCase(); });
  return brand;
}

// The swatch is honest about what it is: the palette and the type, at roughly
// the proportions the cover page uses. It does not attempt the layout — a CSS
// approximation of a typeset page would be a promise the PDF then breaks.
function updatePreview() {
  const b = collect();
  const serif = (b.serif_stack[0] || 'Georgia') + ', Georgia, serif';
  const sans = (b.sans_stack[0] || 'Arial') + ', Arial, sans-serif';
  $('preview').innerHTML =
    '<div class="band" style="background:' + esc(b.accent) + '"></div>' +
    '<div class="inner" style="font-family:' + esc(serif) + ';color:' + esc(b.ink) + '">' +
      '<div class="wordmark" style="font-family:' + esc(sans) + ';color:' + esc(b.accent) + '">' +
        esc((b.firm || '').toUpperCase()) + '</div>' +
      '<div class="lbl" style="font-family:' + esc(sans) + ';color:' + esc(b.muted) + '">' +
        esc(b.tagline) + '</div>' +
      '<h2 class="doc-title" style="font-family:' + esc(sans) + ';font-size:' + (b.body_size * 2.1) + 'px">' +
        'Access Control Policy</h2>' +
      '<div class="doc-sub" style="color:' + esc(b.muted) + ';font-size:' + (b.small_size * 1.25) + 'px">Policy · POL-AC-001</div>' +
      '<span class="pill" style="font-family:' + esc(sans) + ';border-color:' + esc(b.warn) + ';color:' + esc(b.warn) +
        ';font-size:' + (b.micro_size * 1.2) + 'px">Confidential</span>' +
      '<hr style="border-color:' + esc(b.rule) + '">' +
      '<div class="lbl" style="font-family:' + esc(sans) + ';color:' + esc(b.muted) + '">Policy statements</div>' +
      // A div, not a p: the global theme sets a colour on every p with
      // !important, which outranks the inherited brand colour and would show
      // the preview's body text in the UI's grey rather than the brand's.
      '<div class="body-text" style="font-size:' + (b.body_size * 1.3) + 'px;line-height:1.5">' +
        'Access <strong>must</strong> be granted on the principle of least privilege: an account holds only ' +
        'the permissions required for the role it supports, and no more.</div>' +
      '<table style="font-family:' + esc(sans) + '">' +
        '<tr style="background:' + esc(b.table_head) + '"><th>Control</th><th>Coverage</th></tr>' +
        '<tr><td>AC-2</td><td style="color:' + esc(b.ok) + '">Full</td></tr>' +
        '<tr style="background:' + esc(b.table_zebra) + '"><td>AC-6</td><td style="color:' + esc(b.warn) + '">Partial</td></tr>' +
        '<tr><td>AC-17</td><td style="color:' + esc(b.risk) + '">Not covered</td></tr>' +
      '</table>' +
      '<div class="foot" style="font-family:' + esc(sans) + ';color:' + esc(b.muted) + '">' +
        esc(b.footer_note) + '</div>' +
    '</div>';
}

async function load() {
  const res = await fetch('/templates/brand');
  const body = await res.json();
  defaults = body.defaults;
  form.elements['paper'].innerHTML = body.papers.map((p) =>
    '<option value="' + esc(p) + '">' + esc(p) + '</option>').join('');
  fill(body.brand);

  const catalogRes = await fetch('/templates/data');
  const catalog = await catalogRes.json();
  $('previewTemplate').innerHTML = catalog.templates.map((t) =>
    '<option value="' + esc(t.id) + '">' + esc(t.name) + '</option>').join('');
}

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  const status = $('brandStatus');
  status.textContent = 'Saving…';
  const res = await fetch('/templates/brand', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(collect()),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) { status.textContent = body.error || ('HTTP ' + res.status); return; }
  fill(body.brand);
  status.textContent = 'Saved.';
});

$('resetBrand').addEventListener('click', async () => {
  if (!confirm('Discard the saved brand and go back to what the templates ship with?')) return;
  const res = await fetch('/templates/brand/reset', { method: 'POST' });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) { $('brandStatus').textContent = body.error || ('HTTP ' + res.status); return; }
  fill(body.brand);
  $('brandStatus').textContent = 'Reset to the shipped default.';
});

$('previewPdf').addEventListener('click', async () => {
  const status = $('previewStatus');
  status.textContent = 'Rendering…';
  const url = '/templates/render?sample=1&inline=1&template=' + encodeURIComponent($('previewTemplate').value);
  try {
    const res = await fetch(url);
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      status.textContent = 'Failed: ' + (body.error || ('HTTP ' + res.status));
      return;
    }
    const blob = await res.blob();
    const href = URL.createObjectURL(blob);
    if (res.headers.get('X-GRC-Bundled')) {
      const a = document.createElement('a');
      a.href = href; a.download = 'template-sources.zip'; a.click();
      status.textContent = res.headers.get('X-GRC-Reason') || 'No engine installed — downloaded the sources instead.';
    } else {
      window.open(href, '_blank');
      status.textContent = 'Opened in a new tab.';
    }
    URL.revokeObjectURL(href);
  } catch (err) {
    status.textContent = 'Failed: ' + err.message;
  }
});

load();
`
