package policystudio

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
	"grc/internal/policydocs"
)

// TemplatesPage is where administrators draft, import, edit, publish and
// retire the templates New document offers. The built-in templates are listed
// read-only, to copy from.
func (h *Handler) TemplatesPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(templatesPageHTML()))
}

// The page is built with createElement and textContent throughout: template
// text, drafted by a model from a client's document, is never parsed as HTML.
func templatesPageHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Policy Templates · GRC</title>
  <style>` + policydocs.PageCSS() + `
    .tpl-list .group { padding:8px 14px 4px; font-family:Arial,sans-serif; font-size:11px; text-transform:uppercase; letter-spacing:0.06em; color:var(--muted); }
    .tpl-list .empty { padding:10px 14px; color:var(--muted); font-family:Arial,sans-serif; font-size:13px; }
    .pill.published { border-color:var(--green); color:var(--green); }
    .pill.changed { border-color:var(--amber); color:var(--amber); }
    .pill.builtin { color:var(--muted); }
    .tpl-list .pill { white-space:nowrap; }
    .editor { padding:14px; }
    .editor .origin { font-family:Arial,sans-serif; font-size:12.5px; color:var(--muted); margin:4px 0 10px; }
    .editor .fields { grid-template-columns:repeat(auto-fit,minmax(170px,1fr)); }
    .editor .wide { grid-column:1 / -1; }
    .editor input, .editor select, .editor textarea { color:var(--ink); }
    .rowline { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr) auto; gap:6px; margin-bottom:6px; align-items:start; }
    .rowline.fact { grid-template-columns:minmax(0,0.8fr) minmax(0,1fr) minmax(0,1.4fr) minmax(0,1fr) 7.5em auto; }
    .rowline.mapping { grid-template-columns:8em 8.5em minmax(0,1fr) auto; }
    .rowline input, .rowline select { width:100%; min-width:0; padding:6px 8px; border:1px solid var(--line); border-radius:8px; background:var(--bg); }
    .section .sec-head { display:grid; grid-template-columns:minmax(0,1fr) 13em auto; gap:6px; align-items:center; }
    .section .sec-head input, .section .sec-head select { min-width:0; padding:6px 8px; border:1px solid var(--line); border-radius:8px; background:var(--bg); }
    .section textarea { font-family:"Courier New",monospace; font-size:13px; line-height:1.45; }
    .section .guidance textarea { min-height:52px; font-family:Arial,sans-serif; }
    .section .sub { font-family:Arial,sans-serif; font-size:11px; text-transform:uppercase; letter-spacing:0.05em; color:var(--muted); margin:8px 0 4px; }
    .section .sources { font-family:Arial,sans-serif; font-size:12px; color:var(--muted); margin-top:6px; }
    .mini { padding:4px 9px; font-size:12px; }
    .ok-box { border-left:3px solid var(--green); padding:6px 10px; margin-bottom:6px; font-family:Arial,sans-serif; font-size:12.5px; }
    dialog.tpl-dialog { width:min(620px, calc(100vw - 32px)); background:var(--panel); color:var(--ink); border:1px solid var(--line); border-radius:12px; padding:20px; }
    dialog.tpl-dialog::backdrop { background:rgba(0,0,0,0.55); }
    dialog.tpl-dialog h2 { margin:0 0 12px; font-size:1.2rem; }
    dialog.tpl-dialog label { display:block; margin:12px 0 4px; font-size:13px; color:var(--muted); font-family:Arial,sans-serif; }
    dialog.tpl-dialog label.check { display:flex; }
    dialog.tpl-dialog select, dialog.tpl-dialog input[type=text], dialog.tpl-dialog textarea { width:100%; padding:7px 9px; border:1px solid var(--line); border-radius:8px; background:var(--bg); color:var(--ink); }
    dialog.tpl-dialog textarea { min-height:90px; }
    dialog.tpl-dialog .hint { font-family:Arial,sans-serif; font-size:12.5px; color:var(--muted); margin:8px 0 0; }
    dialog.tpl-dialog .err { color:var(--bad); min-height:1em; font-family:Arial,sans-serif; font-size:13px; }
    dialog.tpl-dialog .progress { font-family:Arial,sans-serif; font-size:13px; min-height:1em; }
    button:not(.primary):not(.danger) { background:var(--panel); color:var(--ink); border-color:var(--line); }
    @media (max-width: 900px) {
      .layout { grid-template-columns:minmax(0,1fr); }
      .rowline, .rowline.fact, .rowline.mapping, .section .sec-head { grid-template-columns:minmax(0,1fr); }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/policies/templates/manage") + `
    <h1>Policy Templates</h1>
    <p class="subtitle">The starting points <em>New document</em> offers. Draft one from a document in the AI library, import one as JSON, or copy an existing one; edit it, and publish it when it has no problems left.</p>
    <section class="toolbar">
      <button id="draftBtn" class="primary" type="button">Draft from a library document</button>
      <button id="importBtn" type="button">Import JSON</button>
      <span id="status" class="check"></span>
    </section>
    <div class="layout">
      <section class="panel">
        <div class="panel-header">Templates</div>
        <div class="rows tpl-list" id="list"><div class="empty">Loading…</div></div>
      </section>
      <section class="panel">
        <div class="panel-header" id="editorHeader">Template</div>
        <div class="detail-body editor" id="editor"><p class="subtitle">Choose a template, or draft a new one.</p></div>
      </section>
    </div>
  </main>
<script>
(() => {
const el = (id) => document.getElementById(id);
const state = { meta: null, builtIn: [], mine: [], selected: null, dirty: false };

function node(tag, props, kids){
  const n = document.createElement(tag);
  Object.entries(props || {}).forEach(([k, v]) => {
    if (v === undefined || v === null || v === false) return;
    if (k === "text") n.textContent = v;
    else if (k === "value") n.value = v;
    else if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
    else n.setAttribute(k, v === true ? "" : String(v));
  });
  (kids || []).forEach(c => { if (c) n.appendChild(typeof c === "string" ? document.createTextNode(c) : c); });
  return n;
}

async function api(method, url, body){
  const opts = { method, headers: {} };
  if (body !== undefined) { opts.headers["Content-Type"] = "application/json"; opts.body = typeof body === "string" ? body : JSON.stringify(body); }
  const res = await fetch(url, opts);
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { const err = new Error(data.error || ("HTTP " + res.status)); err.payload = data; throw err; }
  return data;
}

function say(text){ el("status").textContent = text || ""; }
function titleCase(v){ return String(v || "").replace(/_/g, " ").replace(/\b\w/g, c => c.toUpperCase()); }
function kindLabel(k){ const m = (state.meta.section_kinds || []).find(x => x.value === k); return m ? m.label : k; }

function statusPills(t){
  const out = [];
  if (t.status === "draft") out.push(node("span", { class: "pill draft", text: "Draft" }));
  if (t.status === "published") out.push(node("span", { class: "pill published", text: "Published " + t.published_version }));
  if (t.status === "retired") out.push(node("span", { class: "pill retired", text: "Retired" }));
  if (t.unpublished_changes) out.push(node("span", { class: "pill changed", text: "Unpublished changes" }));
  return out;
}

function renderList(){
  const list = el("list");
  list.replaceChildren();
  list.appendChild(node("div", { class: "group", text: "Made here" }));
  if (!state.mine.length) list.appendChild(node("div", { class: "empty", text: "None yet." }));
  for (const t of state.mine) {
    const active = state.selected && state.selected.kind === "mine" && state.selected.id === t.id;
    list.appendChild(node("div", { class: "row" + (active ? " active" : ""), tabindex: "0", role: "button", "data-template": t.template_id,
      onclick: () => select("mine", t.id), onkeydown: (e) => { if (e.key === "Enter") select("mine", t.id); } }, [
      node("div", { class: "row-top" }, [node("span", { class: "row-title", text: t.draft.title || t.template_id }), node("span", {}, statusPills(t))]),
      node("div", { class: "row-meta", text: titleCase(t.draft.doc_type) + " · " + t.draft.sections.length + " sections" +
        (t.problems.length ? " · " + t.problems.length + " problem(s)" : "") + (t.documents ? " · used by " + t.documents + " document(s)" : "") }),
    ]));
  }
  list.appendChild(node("div", { class: "group", text: "Built in" }));
  for (const t of state.builtIn) {
    const active = state.selected && state.selected.kind === "builtin" && state.selected.id === t.id;
    list.appendChild(node("div", { class: "row" + (active ? " active" : ""), tabindex: "0", role: "button",
      onclick: () => select("builtin", t.id), onkeydown: (e) => { if (e.key === "Enter") select("builtin", t.id); } }, [
      node("div", { class: "row-top" }, [node("span", { class: "row-title", text: t.title }), node("span", { class: "pill builtin", text: "Built in " + t.version })]),
      node("div", { class: "row-meta", text: titleCase(t.doc_type) + " · " + t.section_count + " sections" }),
    ]));
  }
}

async function load(selectID){
  try {
    const [meta, all, mine] = await Promise.all([state.meta ? Promise.resolve(state.meta) : api("GET", "/policies/app-templates/meta"),
      api("GET", "/policies/templates"), api("GET", "/policies/app-templates")]);
    state.meta = meta;
    state.builtIn = all.filter(t => t.built_in);
    state.mine = mine;
  } catch (e) { say("Could not load the templates: " + e.message); return; }
  if (selectID) state.selected = { kind: "mine", id: selectID };
  renderList();
  if (state.selected) renderEditor();
}

function confirmLeave(){ return !state.dirty || confirm("Discard the unsaved changes to this template?"); }

function select(kind, id){
  if (!confirmLeave()) return;
  state.selected = { kind, id };
  state.dirty = false;
  renderList();
  renderEditor();
}

function renderEditor(){
  const box = el("editor");
  box.replaceChildren();
  if (state.selected.kind === "builtin") { renderBuiltIn(box); return; }
  const t = state.mine.find(x => x.id === state.selected.id);
  if (!t) { box.appendChild(node("p", { class: "subtitle", text: "That template is gone." })); return; }
  el("editorHeader").textContent = t.draft.title || t.template_id;
  const work = JSON.parse(JSON.stringify(t.draft));
  const touched = () => { state.dirty = true; saveBtn.disabled = false; publishBtn.disabled = t.status === "retired"; publishBtn.textContent = "Save and publish"; };

  box.appendChild(node("div", {}, statusPills(t)));
  const origin = { library: "Drafted by " + (t.ai_model || t.ai_provider || "the AI") + " from “" + t.source_title + "” in the library",
    import: "Imported from JSON", copy: "Copied from another template" }[t.origin] || "";
  box.appendChild(node("p", { class: "origin", text: [origin, "last changed by " + t.updated_by + " " + (t.updated_at || "").slice(0, 16).replace("T", " ")].filter(Boolean).join(" · ") }));

  if (t.problems.length) {
    box.appendChild(node("h3", { text: "To fix before publishing (" + t.problems.length + ")" }));
    t.problems.forEach(p => box.appendChild(node("div", { class: "finding error", text: p })));
  } else {
    box.appendChild(node("div", { class: "ok-box", text: t.status === "published" && !t.unpublished_changes ? "Published, and offered by New document." : "No problems: this template can be published." }));
  }
  if (t.notes.length) {
    box.appendChild(node("h3", { text: "Notes from drafting" }));
    t.notes.forEach(n => box.appendChild(node("div", { class: "finding", text: n })));
  }
  if (t.origin === "library") box.appendChild(node("p", { class: "origin", text: "Check that nothing specific to the client the sample was written for is left in the text: names, people, systems and numbers belong in facts." }));

  const field = (label, input, wide) => node("div", { class: "field" + (wide ? " wide" : "") }, [node("label", { text: label }), input]);
  const text = (value, onchange, attrs) => node("input", Object.assign({ type: "text", value: value || "", oninput: (e) => { onchange(e.target.value); touched(); } }, attrs || {}));
  const select = (options, value, onchange, labels) => {
    const s = node("select", { onchange: (e) => { onchange(e.target.value); touched(); } }, options.map(o => node("option", { value: o, text: labels ? labels(o) : o })));
    s.value = value;
    return s;
  };
  const idInput = text(work.id, v => { work.id = v.trim(); }, t.status === "draft" ? {} : { disabled: true, title: "Documents refer to a published template by its id, so it cannot change." });
  const cadence = node("input", { type: "number", min: "1", max: "60", value: work.review_cadence_months || 12, oninput: (e) => { work.review_cadence_months = Number(e.target.value) || 0; touched(); } });
  const desc = node("textarea", { oninput: (e) => { work.description = e.target.value; touched(); } });
  desc.value = work.description || "";
  box.appendChild(node("div", { class: "fields" }, [
    field("Title", text(work.title, v => { work.title = v; el("editorHeader").textContent = v; })),
    field("Id", idInput),
    field("Document type", select(state.meta.doc_types, work.doc_type, v => { work.doc_type = v; }, titleCase)),
    field("Classification", select(state.meta.classifications, work.classification || "Internal", v => { work.classification = v; })),
    field("Review every (months)", cadence),
    field("Description", desc, true),
    field("Frameworks", node("div", { class: "frameworks" }, state.meta.frameworks.map(f => node("label", {}, [
      node("input", { type: "checkbox", checked: work.frameworks.includes(f), onchange: (e) => {
        work.frameworks = e.target.checked ? work.frameworks.concat([f]) : work.frameworks.filter(x => x !== f); touched(); } }), f]))), true),
  ]));

  box.appendChild(node("h3", { text: "Client facts (" + work.facts.length + ")" }));
  const facts = node("div");
  const drawFacts = () => {
    facts.replaceChildren();
    work.facts.forEach((f, i) => facts.appendChild(node("div", { class: "rowline fact" }, [
      text(f.key, v => { f.key = v.trim(); }, { placeholder: "key", "aria-label": "Fact key" }),
      text(f.label, v => { f.label = v; }, { placeholder: "Label", "aria-label": "Fact label" }),
      text(f.description, v => { f.description = v; }, { placeholder: "What it is", "aria-label": "Fact description" }),
      text(f.example, v => { f.example = v; }, { placeholder: "Example", "aria-label": "Fact example" }),
      select(state.meta.value_types, f.value_type || "text", v => { f.value_type = v; }),
      node("button", { type: "button", class: "mini", text: "Remove", onclick: () => { work.facts.splice(i, 1); touched(); drawFacts(); } }),
    ])));
    facts.appendChild(node("button", { type: "button", class: "mini", text: "Add fact", onclick: () => {
      work.facts.push({ key: "", label: "", description: "", example: "", value_type: "text" }); touched(); drawFacts(); } }));
  };
  drawFacts();
  box.appendChild(facts);
  box.appendChild(node("p", { class: "origin", text: "Use a fact in the text as {{fact:key}}, and a control as [[control:AC-2]]." }));

  box.appendChild(node("h3", { text: "Sections" }));
  const required = new Set((state.meta.required_kinds[work.doc_type] || []));
  const sections = node("div");
  const drawSections = () => {
    sections.replaceChildren();
    work.sections.forEach((s, i) => {
      const content = node("textarea", { "aria-label": "Section text", oninput: (e) => { s.content = e.target.value; touched(); } });
      content.value = s.content || "";
      const guidance = node("textarea", { "aria-label": "Guidance for the consultant", oninput: (e) => { s.guidance = e.target.value; touched(); } });
      guidance.value = s.guidance || "";
      const move = (d) => { const j = i + d; if (j < 0 || j >= work.sections.length) return; [work.sections[i], work.sections[j]] = [work.sections[j], work.sections[i]]; touched(); drawSections(); };
      const mappings = node("div");
      const drawMappings = () => {
        mappings.replaceChildren();
        s.proposed_mappings.forEach((m, k) => mappings.appendChild(node("div", { class: "rowline mapping" }, [
          text(m.control_id, v => { m.control_id = v.trim().toUpperCase(); }, { placeholder: "AC-2", "aria-label": "Control id" }),
          select(state.meta.coverages, m.coverage, v => { m.coverage = v; }, titleCase),
          text(m.note, v => { m.note = v; }, { placeholder: "Why it applies", "aria-label": "Mapping note" }),
          node("button", { type: "button", class: "mini", text: "Remove", onclick: () => { s.proposed_mappings.splice(k, 1); touched(); drawMappings(); } }),
        ])));
        mappings.appendChild(node("button", { type: "button", class: "mini", text: "Add control", onclick: () => {
          s.proposed_mappings.push({ control_id: "", coverage: "partial", note: "" }); touched(); drawMappings(); } }));
      };
      drawMappings();
      const refs = (t.sources[s.uid_seed] || []).map(r => "S" + r.n + (r.heading ? " " + r.heading : ""));
      sections.appendChild(node("div", { class: "section", "data-seed": s.uid_seed }, [
        node("div", { class: "sec-head" }, [
          text(s.heading, v => { s.heading = v; }, { "aria-label": "Section heading" }),
          select(state.meta.section_kinds.map(k => k.value), s.kind, v => { s.kind = v; }, kindLabel),
          node("span", {}, [
            node("button", { type: "button", class: "mini", text: "↑", "aria-label": "Move up", disabled: i === 0, onclick: () => move(-1) }), " ",
            node("button", { type: "button", class: "mini", text: "↓", "aria-label": "Move down", disabled: i === work.sections.length - 1, onclick: () => move(1) }), " ",
            node("button", { type: "button", class: "mini danger", text: "Remove", onclick: () => { work.sections.splice(i, 1); touched(); drawSections(); } }),
          ]),
        ]),
        required.has(s.kind) ? node("div", { class: "sub", text: "Required for a " + titleCase(work.doc_type).toLowerCase() }) : null,
        node("div", { class: "sub", text: "Text" }), content,
        node("div", { class: "guidance" }, [node("div", { class: "sub", text: "Guidance for the consultant (never in the document)" }), guidance]),
        node("div", { class: "sub", text: "Suggested controls (partial or supporting)" }), mappings,
        refs.length ? node("div", { class: "sources", text: "From the sample: " + refs.join("; ") }) : null,
      ]));
    });
    sections.appendChild(node("button", { type: "button", class: "mini", text: "Add section", onclick: () => {
      const taken = new Set(work.sections.map(x => x.uid_seed));
      let seed = "section-" + (work.sections.length + 1);
      for (let n = 2; taken.has(seed); n++) seed = "section-" + (work.sections.length + n);
      work.sections.push({ uid_seed: seed, heading: "New section", kind: "other", content: "", guidance: "", proposed_mappings: [] }); touched(); drawSections(); } }));
  };
  drawSections();
  box.appendChild(sections);

  const save = async () => {
    const saved = await api("PUT", "/policies/app-templates/" + t.id, work);
    state.dirty = false;
    await load(saved.id);
    return saved;
  };
  const saveBtn = node("button", { type: "button", class: "primary", text: "Save", disabled: true, onclick: async () => {
    try { await save(); say("Saved."); } catch (e) { say(e.message); } } });
  const publishBtn = node("button", { type: "button", text: t.status === "published" ? "Publish changes" : "Publish",
    disabled: t.status === "retired" || (t.status === "published" && !t.unpublished_changes) || t.problems.length > 0 && !state.dirty,
    onclick: async () => {
      try {
        if (state.dirty) await save();
        const p = await api("POST", "/policies/app-templates/" + t.id + "/publish");
        await load(p.id);
        say("Published " + p.template_id + " " + p.published_version + ". New document offers it now.");
      } catch (e) { say(e.message); }
    } });
  const actions = [saveBtn, publishBtn];
  if (t.status === "published") actions.push(node("button", { type: "button", text: "Retire", onclick: async () => {
    if (!confirmLeave()) return;
    try { state.dirty = false; const r = await api("POST", "/policies/app-templates/" + t.id + "/retire"); await load(r.id); say("Retired: New document no longer offers it."); } catch (e) { say(e.message); } } }));
  if (t.status === "retired") actions.push(node("button", { type: "button", text: "Offer again", onclick: async () => {
    try { const r = await api("POST", "/policies/app-templates/" + t.id + "/restore"); await load(r.id); say("Offered again, at " + r.published_version + "."); } catch (e) { say(e.message); } } }));
  actions.push(node("a", { class: "tab", href: "/policies/app-templates/" + t.id + "/template.json", text: "Download JSON" }));
  if (!t.documents) actions.push(node("button", { type: "button", class: "danger", text: "Delete", onclick: async () => {
    if (!confirm("Delete this template? This cannot be undone.")) return;
    try { await api("DELETE", "/policies/app-templates/" + t.id); state.selected = null; state.dirty = false; el("editor").replaceChildren(); el("editorHeader").textContent = "Template"; await load(); say("Deleted."); } catch (e) { say(e.message); } } }));
  box.appendChild(node("div", { class: "actions" }, actions));
}

function renderBuiltIn(box){
  const t = state.builtIn.find(x => x.id === state.selected.id);
  el("editorHeader").textContent = t ? t.title : "Template";
  if (!t) return;
  box.appendChild(node("span", { class: "pill builtin", text: "Built in " + t.version }));
  box.appendChild(node("p", { class: "origin", text: t.description }));
  box.appendChild(node("p", { class: "origin", text: titleCase(t.doc_type) + " · " + t.section_count + " sections · " + t.facts.length + " client facts" + (t.frameworks.length ? " · " + t.frameworks.join(", ") : "") }));
  box.appendChild(node("p", { class: "origin", text: "Built-in templates ship with the application and cannot be edited here. Copy one to make your own version." }));
  box.appendChild(node("div", { class: "actions" }, [node("button", { type: "button", class: "primary", text: "Copy to edit", onclick: async () => {
    try { const c = await api("POST", "/policies/app-templates/copy", { template_id: t.id }); state.dirty = false; await load(c.id); say("Copied. Edit it, then publish it."); } catch (e) { say(e.message); } } })]));
}

async function readEvents(body, on){
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  for (;;) {
    const chunk = await reader.read();
    if (chunk.value) buf += decoder.decode(chunk.value, { stream: true });
    let end;
    while ((end = buf.indexOf("\n\n")) >= 0) {
      const block = buf.slice(0, end);
      buf = buf.slice(end + 2);
      let name = "message", raw = "";
      block.split("\n").forEach(line => {
        if (line.indexOf("event: ") === 0) name = line.slice(7);
        else if (line.indexOf("data: ") === 0) raw += line.slice(6);
      });
      let data = {};
      try { data = JSON.parse(raw); } catch (_) {}
      if (name === "result") { reader.cancel().catch(() => {}); return data; }
      if (name === "error") { reader.cancel().catch(() => {}); throw new Error(data.error || "the draft failed"); }
      if (on[name]) on[name](data);
    }
    if (chunk.done) throw new Error("the draft stopped before it was complete");
  }
}

async function draftDialog(){
  if (!confirmLeave()) return;
  const docSel = node("select", { id: "td-library" }, [node("option", { value: "", text: "Loading the library…" })]);
  const typeSel = node("select", { id: "td-type" }, [node("option", { value: "", text: "Let the AI choose" })].concat(
    state.meta.doc_types.map(t => node("option", { value: t, text: titleCase(t) }))));
  const title = node("input", { type: "text", id: "td-title", placeholder: "Leave empty to let the AI name it" });
  const instr = node("textarea", { id: "td-instruction", placeholder: "Optional: what to keep, what to leave out, the frameworks to aim at" });
  const local = node("input", { type: "checkbox", id: "td-local" });
  const dest = node("p", { class: "hint" });
  const err = node("p", { class: "err", role: "alert" });
  const progress = node("p", { class: "progress", role: "status" });
  const go = node("button", { type: "button", class: "primary", text: "Draft", disabled: true });
  const cancel = node("button", { type: "button", text: "Cancel" });
  const dlg = node("dialog", { class: "tpl-dialog", "aria-labelledby": "td-heading" }, [
    node("h2", { id: "td-heading", text: "Draft a template from a library document" }),
    node("label", { for: "td-library", text: "Sample document" }), docSel,
    node("p", { class: "hint", text: "Documents are uploaded to the AI agent's library on the Wintermute server, which reads them; only those it has finished reading are listed." }),
    node("label", { for: "td-type", text: "Document type" }), typeSel,
    node("label", { for: "td-title", text: "Title" }), title,
    node("label", { for: "td-instruction", text: "Instruction" }), instr,
    node("label", { class: "check" }, [local, " Local only: never send the sample to the cloud"]),
    dest, progress, err,
    node("div", { class: "actions" }, [go, cancel]),
  ]);
  document.body.appendChild(dlg);
  dlg.showModal();
  let controller = null;
  cancel.addEventListener("click", () => { if (controller) controller.abort(); dlg.close(); dlg.remove(); });
  const checkRoute = async () => {
    try {
      const st = await api("GET", "/policies/app-templates/draft/status" + (local.checked ? "?local_only=1" : ""));
      dest.textContent = st.available ? "The sample goes to " + st.destination + "." : st.reason;
      go.disabled = !st.available || !docSel.value;
      go.dataset.available = st.available ? "1" : "";
    } catch (e) { dest.textContent = e.message; }
  };
  local.addEventListener("change", checkRoute);
  docSel.addEventListener("change", () => { go.disabled = !go.dataset.available || !docSel.value; });
  try {
    const docs = (await api("GET", "/policies/library")).filter(d => d.ready);
    docSel.replaceChildren(node("option", { value: "", text: docs.length ? "Choose a document" : "The library has no documents ready" }),
      ...docs.map(d => node("option", { value: String(d.id), text: d.title })));
  } catch (e) { docSel.replaceChildren(node("option", { value: "", text: "The library is not available" })); err.textContent = e.message; }
  await checkRoute();
  go.addEventListener("click", async () => {
    err.textContent = "";
    go.disabled = true;
    progress.textContent = "Reading the sample…";
    controller = new AbortController();
    try {
      const res = await fetch("/policies/app-templates/draft", { method: "POST", signal: controller.signal,
        headers: { "Content-Type": "application/json", "Accept": "text/event-stream" },
        body: JSON.stringify({ library_document_id: Number(docSel.value), doc_type: typeSel.value, title: title.value.trim(), instruction: instr.value.trim(), local_only: local.checked }) });
      let t;
      if ((res.headers.get("Content-Type") || "").indexOf("text/event-stream") === 0 && res.body) {
        t = await readEvents(res.body, { progress: (d) => { progress.textContent = d.chars ? "Drafting… " + d.chars.toLocaleString() + " characters written" : "Waiting for the AI…"; } });
      } else {
        t = await res.json();
        if (!res.ok) throw new Error(t.error || ("HTTP " + res.status));
      }
      dlg.close(); dlg.remove();
      state.dirty = false;
      await load(t.id);
      say("Drafted " + (t.draft.title || t.template_id) + ". Review it, fix what is listed, then publish it.");
    } catch (e) {
      if (e.name === "AbortError") return;
      progress.textContent = "";
      err.textContent = e.message;
      go.disabled = false;
    }
  });
}

function importDialog(){
  if (!confirmLeave()) return;
  const file = node("input", { type: "file", id: "ti-file", accept: "application/json,.json" });
  const area = node("textarea", { id: "ti-json", placeholder: "{ \"id\": \"…\", \"title\": \"…\", … }" });
  const err = node("p", { class: "err", role: "alert" });
  const go = node("button", { type: "button", class: "primary", text: "Import" });
  const cancel = node("button", { type: "button", text: "Cancel" });
  const dlg = node("dialog", { class: "tpl-dialog", "aria-labelledby": "ti-heading" }, [
    node("h2", { id: "ti-heading", text: "Import a template" }),
    node("p", { class: "hint", text: "A template in JSON, as Download JSON or the built-in files write it. It arrives as a draft to review and publish." }),
    node("label", { for: "ti-file", text: "File" }), file,
    node("label", { for: "ti-json", text: "Or paste it" }), area,
    err, node("div", { class: "actions" }, [go, cancel]),
  ]);
  document.body.appendChild(dlg);
  dlg.showModal();
  cancel.addEventListener("click", () => { dlg.close(); dlg.remove(); });
  file.addEventListener("change", async () => { if (file.files[0]) area.value = await file.files[0].text(); });
  go.addEventListener("click", async () => {
    err.textContent = "";
    try {
      const t = await api("POST", "/policies/app-templates/import", area.value);
      dlg.close(); dlg.remove();
      state.dirty = false;
      await load(t.id);
      say("Imported " + t.template_id + ".");
    } catch (e) { err.textContent = e.message; }
  });
}

window.addEventListener("beforeunload", (e) => { if (state.dirty) { e.preventDefault(); e.returnValue = ""; } });
el("draftBtn").addEventListener("click", draftDialog);
el("importBtn").addEventListener("click", importDialog);
load();
})();
</script>
</body>
</html>`
}
