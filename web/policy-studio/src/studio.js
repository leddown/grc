// Policy Studio: the collaborative editor for Studio-format policy documents.
//
// The server holds the only authoritative copy of the document. This page
// edits it over the collaboration socket; everything else -- sections, lint,
// control mappings, status -- comes from /policies/:id/studio/state, polled
// while the tab is visible. Nothing here writes server metadata into the
// shared document.
//
// Content is never inserted with innerHTML: every string from the document or
// the server goes into the page through textContent / createTextNode.
import { Editor, Extension } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'
import * as Y from 'yjs'
import { WebsocketProvider } from 'y-websocket'
import Collaboration from '@tiptap/extension-collaboration'
import CollaborationCaret from '@tiptap/extension-collaboration-caret'
import { studioExtensions, FRAGMENT } from './schema.js'

const POLL_MS = 4000

// ---- small DOM helpers ----

function h(tag, props, ...children) {
  const node = document.createElement(tag)
  for (const [k, v] of Object.entries(props || {})) {
    if (v === null || v === undefined || v === false) continue
    if (k === 'class') node.className = v
    else if (k === 'text') node.textContent = v
    else if (k.startsWith('on')) node.addEventListener(k.slice(2), v)
    else node.setAttribute(k, v === true ? '' : String(v))
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue
    node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c)
  }
  return node
}

async function api(method, url, body, headers) {
  const resp = await fetch(url, {
    method,
    headers: Object.assign({ Accept: 'application/json' }, body !== undefined ? { 'Content-Type': 'application/json' } : {}, headers || {}),
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  })
  if (resp.status === 304) return { notModified: true, etag: resp.headers.get('ETag') }
  let data = null
  const text = await resp.text()
  if (text) { try { data = JSON.parse(text) } catch (_) { data = { error: text } } }
  if (!resp.ok) {
    const err = new Error((data && data.error) || ('The server answered ' + resp.status))
    err.status = resp.status
    err.findings = data && data.findings
    throw err
  }
  return { data, etag: resp.headers.get('ETag') }
}

function colorFor(name) {
  let hash = 0
  for (const ch of String(name)) hash = (hash * 31 + ch.codePointAt(0)) >>> 0
  return 'hsl(' + (hash % 360) + ' 70% 62%)'
}

const statusLabels = { draft: 'Draft', in_review: 'In review', approved: 'Approved', retired: 'Retired' }

// ---- paste ----

// Word and web pages paste styled HTML. The schema drops tags it does not
// know; this also drops what it would otherwise carry across on known tags.
function sanitizePastedHTML(html) {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  doc.querySelectorAll('style, script, meta, link, title, xml, o\\:p, img, svg, iframe, object, embed').forEach((n) => n.remove())
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_COMMENT)
  const comments = []
  while (walker.nextNode()) comments.push(walker.currentNode)
  comments.forEach((n) => n.remove())
  doc.body.querySelectorAll('*').forEach((n) => {
    for (const a of Array.from(n.attributes)) {
      const keep = (a.name === 'href' && n.tagName === 'A') || ((a.name === 'colspan' || a.name === 'rowspan') && /^T[DH]$/.test(n.tagName))
      if (!keep) n.removeAttribute(a.name)
    }
  })
  return doc.body.innerHTML
}

// ---- section integrity ----

function sectionUIDs(doc) {
  const out = []
  doc.forEach((node) => out.push(node.attrs.uid))
  return out.join('\n')
}

// Typing and pasting may change what a section says, never which sections
// exist: a transaction that would add, remove, merge or split one is refused.
// Structure changes go through the outline, which asks the server; they
// arrive here as remote (y-sync) transactions and pass.
function sectionIntegrity(onRefused) {
  return Extension.create({
    name: 'sectionIntegrity',
    addProseMirrorPlugins() {
      return [new Plugin({
        key: new PluginKey('sectionIntegrity'),
        filterTransaction(tr, state) {
          if (!tr.docChanged || tr.getMeta('y-sync$')) return true
          if (sectionUIDs(state.doc) === sectionUIDs(tr.doc)) return true
          onRefused()
          return false
        },
      })]
    },
  })
}

// ---- fact tokens: shown by value ----

// A fact renders as the client's value, or as a warning chip naming the
// missing key. The value arrives as a node decoration (see controlChips), so a
// changed fact re-renders every token without touching the document.
function factNodeView(props) {
  const dom = document.createElement('span')
  dom.className = 'ps-fact'
  dom.contentEditable = 'false'
  const render = (node, decorations) => {
    const deco = (decorations || []).find((d) => d.spec && d.spec.fact)
    const key = node.attrs.key
    const resolved = !!(deco && deco.spec.resolved)
    dom.textContent = resolved ? deco.spec.value : key
    dom.dataset.fact = key
    dom.dataset.resolved = resolved ? 'true' : 'false'
    dom.title = resolved ? 'Client fact: ' + key : 'Missing client fact: ' + key + ' (fill it in under Facts)'
    dom.setAttribute('aria-label', resolved ? deco.spec.value : 'missing fact ' + key)
  }
  render(props.node, props.decorations)
  return {
    dom,
    update(node, decorations) {
      if (node.type.name !== 'factToken') return false
      render(node, decorations)
      return true
    },
    ignoreMutation: () => true,
  }
}

// ---- control chips under each section heading ----

const chipsKey = new PluginKey('controlChips')

function controlChips(render) {
  return Extension.create({
    name: 'controlChips',
    addProseMirrorPlugins() {
      return [new Plugin({
        key: chipsKey,
        state: {
          init: () => ({ bySection: {}, facts: {}, version: 0 }),
          apply: (tr, value) => tr.getMeta(chipsKey) || value,
        },
        props: {
          decorations(state) {
            const { bySection, facts, version } = chipsKey.getState(state)
            const decos = []
            state.doc.descendants((node, pos) => {
              if (node.type.name !== 'factToken') return
              const f = facts[node.attrs.key]
              decos.push(Decoration.node(pos, pos + node.nodeSize, {}, { fact: true, resolved: !!(f && f.resolved), value: f ? f.value : '', version }))
            })
            state.doc.forEach((section, offset) => {
              const info = bySection[section.attrs.uid]
              if (!info) return
              const heading = section.firstChild
              const pos = offset + 1 + (heading ? heading.nodeSize : 0)
              decos.push(Decoration.widget(pos, () => render(info), { key: 'chips-' + section.attrs.uid + '-' + version, side: -1, ignoreSelection: true }))
              if (info.errors) decos.push(Decoration.node(offset, offset + section.nodeSize, { class: 'ps-section-has-errors' }))
            })
            return DecorationSet.create(state.doc, decos)
          },
        },
      })]
    },
  })
}

// ---- the application ----

class Studio {
  constructor(root) {
    this.root = root
    this.docId = root.dataset.documentId
    this.canManage = root.dataset.canManage === 'true'
    this.user = root.dataset.user || 'You'
    this.state = null
    this.etag = null
    this.meta = null
    this.editor = null
    this.provider = null
    this.canEdit = false
    this.chipsVersion = 0
  }

  async start() {
    try {
      const [{ data: meta }, { data: state, etag }] = await Promise.all([
        api('GET', '/policies/meta'),
        api('GET', '/policies/' + this.docId + '/studio/state'),
      ])
      this.meta = meta
      this.state = state
      this.etag = etag
    } catch (err) {
      this.root.replaceChildren(h('p', { class: 'ps-error', role: 'alert', text: "The Studio couldn't load this document: " + err.message }))
      return
    }
    this.buildShell()
    if (this.state.document.editor_format !== 'studio') {
      this.renderMigrate()
      return
    }
    this.connect()
    this.renderPanels()
    this.poll()
  }

  buildShell() {
    this.statusPill = h('span', { class: 'ps-pill' })
    this.connection = h('span', { class: 'ps-connection', role: 'status', text: 'Connecting…' })
    this.titleEl = h('h1', { class: 'ps-title' })
    this.actions = h('div', { class: 'ps-actions' })
    this.outline = h('nav', { class: 'ps-outline', 'aria-label': 'Outline' })
    this.canvas = h('div', { class: 'ps-canvas-inner' })
    this.side = h('aside', { class: 'ps-side', 'aria-label': 'Readiness and document control' })
    this.toast = h('div', { class: 'ps-toast', role: 'status', 'aria-live': 'polite' })
    this.root.replaceChildren(
      h('header', { class: 'ps-bar' },
        h('div', { class: 'ps-bar-title' }, h('a', { href: '/policies', class: 'ps-back', text: 'Policies' }), this.titleEl, this.statusPill),
        h('div', { class: 'ps-bar-meta' }, this.connection),
        this.actions),
      h('div', { class: 'ps-body' }, this.outline, h('section', { class: 'ps-canvas', 'aria-label': 'Document' }, this.canvas), this.side),
      this.toast)
    this.renderHeader()
  }

  say(message, kind) {
    this.toast.textContent = message
    this.toast.dataset.kind = kind || 'info'
    this.toast.classList.add('ps-toast-visible')
    clearTimeout(this.toastTimer)
    this.toastTimer = setTimeout(() => this.toast.classList.remove('ps-toast-visible'), 6000)
  }

  renderHeader() {
    const d = this.state.document
    this.titleEl.textContent = d.title
    document.title = d.title + ' · Policy Studio'
    this.statusPill.textContent = statusLabels[d.status] || d.status
    this.statusPill.dataset.status = d.status
  }

  renderMigrate() {
    const d = this.state.document
    const body = h('div', { class: 'ps-migrate' },
      h('h2', { text: 'This document is edited in the section editor' }),
      h('p', { text: 'Opening it in the Studio moves its text into one live document that several people can edit at once. The move is one way: afterwards the section editor shows this document read-only. The current sections are kept as a snapshot first.' }))
    if (this.canManage) {
      body.appendChild(h('button', {
        type: 'button', class: 'ps-primary', text: 'Open in the Studio',
        onclick: async (e) => {
          e.target.disabled = true
          try {
            await api('POST', '/policies/' + d.id + '/studio/migrate', {})
            location.reload()
          } catch (err) {
            e.target.disabled = false
            this.say("The document couldn't be moved: " + err.message, 'error')
          }
        },
      }))
    } else {
      body.appendChild(h('p', { class: 'ps-muted', text: 'An administrator can move it into the Studio.' }))
    }
    body.appendChild(h('p', {}, h('a', { href: '/policies/' + d.id + '/view', text: 'Read the document' })))
    this.canvas.replaceChildren(body)
    this.connection.textContent = ''
  }

  connect() {
    const ydoc = new Y.Doc()
    const base = (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/collab/policies'
    // disableBc: same-browser tabs would otherwise sync over
    // BroadcastChannel, around the server and its read-only enforcement.
    this.provider = new WebsocketProvider(base, this.docId, ydoc, { disableBc: true })
    this.provider.on('status', (e) => {
      const labels = { connected: 'Live', connecting: 'Connecting…', disconnected: 'Offline — changes will sync when the connection returns' }
      this.connection.textContent = labels[e.status] || e.status
      this.connection.dataset.status = e.status
    })
    // Mount only after the first sync: the editor would otherwise create its
    // own empty content and write it into a document that already exists.
    this.provider.on('sync', (synced) => { if (synced && !this.editor) this.mount(ydoc) })
  }

  mount(ydoc) {
    this.canEdit = !!this.state.can_edit
    const host = h('div', { class: 'ps-editor' })
    this.canvas.replaceChildren(host)
    this.editor = new Editor({
      element: host,
      editable: this.canEdit,
      extensions: studioExtensions({
        factNodeView,
        extra: [
          Collaboration.configure({ document: ydoc, field: FRAGMENT }),
          CollaborationCaret.configure({ provider: this.provider, user: { name: this.user, color: colorFor(this.user) } }),
          sectionIntegrity(() => this.say("Sections can't be added, removed, merged or split by typing. Use the outline on the left.", 'warn')),
          controlChips((info) => this.chipRow(info)),
        ],
      }),
      editorProps: {
        attributes: { 'aria-label': 'Policy text', class: 'ps-prosemirror' },
        transformPastedHTML: sanitizePastedHTML,
      },
    })
    this.pushChips()
    window.GRCPolicyStudio.editor = this.editor
    let paper = false
    try { paper = localStorage.getItem('grc.policyStudio.paper') === '1' } catch (_) { /* storage may be unavailable */ }
    if (paper) this.togglePaper()
  }

  // ---- state polling ----

  poll() {
    const tick = async () => {
      if (document.visibilityState === 'visible') {
        try {
          const res = await api('GET', '/policies/' + this.docId + '/studio/state', undefined, this.etag ? { 'If-None-Match': this.etag } : {})
          if (!res.notModified) {
            this.state = res.data
            this.etag = res.etag
            this.renderPanels()
          }
        } catch (_) { /* the connection indicator already says the server is unreachable */ }
      }
      this.pollTimer = setTimeout(tick, POLL_MS)
    }
    this.pollTimer = setTimeout(tick, POLL_MS)
    document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') { clearTimeout(this.pollTimer); tick() } })
  }

  async refresh() {
    const res = await api('GET', '/policies/' + this.docId + '/studio/state')
    this.state = res.data
    this.etag = res.etag
    this.renderPanels()
  }

  renderPanels() {
    this.renderHeader()
    if (this.paperHeader) this.renderPaperHeader()
    const editable = !!this.state.can_edit
    if (this.editor && editable !== this.canEdit) {
      this.canEdit = editable
      this.editor.setEditable(editable)
      // The server decides read-only per connection, at connect time:
      // reconnect so it decides again with the new status.
      this.provider.disconnect()
      this.provider.connect()
      this.say(editable ? 'The document is a draft again; you can edit it.' : 'The document is ' + (statusLabels[this.state.document.status] || this.state.document.status).toLowerCase() + ' and read-only now.', 'info')
    }
    this.renderActions()
    this.renderOutline()
    this.renderSide()
    this.pushChips()
  }

  pushChips() {
    if (!this.editor) return
    const bySection = {}
    const errorsBySection = {}
    for (const f of this.state.findings) {
      if (f.severity === 'error' && f.section_id) errorsBySection[f.section_id] = (errorsBySection[f.section_id] || 0) + 1
    }
    for (const s of this.state.sections) {
      bySection[s.uid] = { section: s, errors: errorsBySection[s.id] || 0 }
    }
    const facts = {}
    for (const f of this.state.facts || []) facts[f.key] = f
    this.chipsVersion++
    this.editor.view.dispatch(this.editor.state.tr.setMeta(chipsKey, { bySection, facts, version: this.chipsVersion }).setMeta('addToHistory', false))
  }

  // ---- header actions ----

  renderActions() {
    const d = this.state.document
    const link = (href, text) => h('a', { class: 'ps-button', href, text })
    const btn = (text, fn, cls) => h('button', { type: 'button', class: cls || '', text, onclick: fn })
    const paper = this.canvas.classList.contains('ps-paper-on')
    const items = [
      h('button', { type: 'button', 'aria-pressed': paper ? 'true' : 'false', text: paper ? 'Screen view' : 'Paper view', onclick: () => this.togglePaper() }),
      link('/policies/' + d.id + '/view', 'Preview'), link('/templates/render?doc=' + d.id, 'Render PDF')]
    if (this.canManage) {
      if (d.status === 'draft') items.push(btn('Submit for review', () => this.transition('submit', 'Submitted for review.')))
      if (d.status === 'in_review') {
        items.push(btn('Approve', () => this.approve(), 'ps-primary'))
        items.push(btn('Return to draft', () => this.transition('reopen', 'Returned to draft.')))
      }
      if (d.status === 'approved') {
        items.push(btn('Revise', () => this.transition('reopen', 'Returned to draft for revision.')))
        items.push(btn('Retire', () => this.transition('retire', 'Retired.')))
      }
      if (d.status === 'retired') items.push(btn('Reinstate as draft', () => this.transition('reopen', 'Reinstated as a draft.')))
    }
    this.actions.replaceChildren(...items)
  }

  // Paper view: the canvas as the deliverable will look -- light page, the
  // template's typefaces, the classification band and document control
  // header. A per-viewer preference, remembered in this browser.
  togglePaper() {
    const on = !this.canvas.classList.contains('ps-paper-on')
    this.canvas.classList.toggle('ps-paper-on', on)
    try { localStorage.setItem('grc.policyStudio.paper', on ? '1' : '0') } catch (_) { /* storage may be unavailable */ }
    this.renderPaperHeader()
    this.renderActions()
  }

  renderPaperHeader() {
    const d = this.state.document
    if (!this.paperHeader) {
      this.paperHeader = h('header', { class: 'ps-paper-header', 'aria-hidden': 'true' })
      this.canvas.prepend(this.paperHeader)
    }
    const rows = [['Reference', d.reference || '—'], ['Status', statusLabels[d.status] || d.status], ['Owner', d.owner_role || '—'],
      ['Approver', d.approver || '—'], ['Effective', d.effective_date || '—'], ['Client', d.client_name || '—']]
    this.paperHeader.replaceChildren(
      h('div', { class: 'ps-paper-band', text: (d.classification || 'Internal').toUpperCase() }),
      h('div', { class: 'ps-paper-title', text: d.title }),
      h('dl', { class: 'ps-paper-control' }, rows.flatMap(([k, v]) => [h('dt', { text: k }), h('dd', { text: v })])))
  }

  async transition(action, done) {
    try {
      await api('POST', '/policies/' + this.docId + '/' + action, {})
      await this.refresh()
      this.say(done, 'info')
    } catch (err) {
      this.say(err.message, 'error')
    }
  }

  async approve() {
    const summary = window.prompt('What changed in this version? (optional)', '')
    if (summary === null) return
    try {
      await api('POST', '/policies/' + this.docId + '/approve', { change_summary: summary })
      await this.refresh()
      this.say('Approved. The document is read-only now.', 'info')
    } catch (err) {
      if (err.findings && err.findings.length) {
        this.say(err.findings.length + ' issue(s) block approval — see Readiness.', 'error')
      } else {
        this.say(err.message, 'error')
      }
      await this.refresh()
    }
  }

  // ---- outline ----

  renderOutline() {
    const s = this.state
    const editable = s.can_edit
    const attached = s.sections.filter((x) => !x.detached)
    const detached = s.sections.filter((x) => x.detached)
    const list = h('ol', { class: 'ps-outline-list' })
    attached.forEach((sec, i) => {
      const row = h('li', { class: 'ps-outline-item' + (sec.controls.length ? '' : ' ps-unmapped') },
        h('button', { type: 'button', class: 'ps-outline-link', onclick: () => this.scrollTo(sec.uid) },
          h('span', { class: 'ps-outline-num', text: String(i + 1) }),
          h('span', { class: 'ps-outline-heading', text: sec.heading || 'Untitled section' })),
        h('span', { class: 'ps-outline-kind', text: sec.kind_label || sec.kind }))
      if (sec.pending_suggestions) row.appendChild(h('span', { class: 'ps-badge', text: sec.pending_suggestions + ' pending' }))
      if (editable) {
        const kinds = h('select', { 'aria-label': 'Section type for ' + (sec.heading || 'this section'), onchange: (e) => this.command('PATCH', '/studio/sections/' + encodeURIComponent(sec.uid), { kind: e.target.value }, 'Section type changed.') })
        for (const k of this.meta.section_kinds) kinds.appendChild(h('option', { value: k.value, selected: k.value === sec.kind, text: k.label }))
        row.appendChild(h('div', { class: 'ps-outline-tools' },
          kinds,
          h('button', { type: 'button', 'aria-label': 'Move up', title: 'Move up', disabled: i === 0, text: '↑', onclick: () => this.move(i, -1) }),
          h('button', { type: 'button', 'aria-label': 'Move down', title: 'Move down', disabled: i === attached.length - 1, text: '↓', onclick: () => this.move(i, 1) }),
          h('button', { type: 'button', text: 'Add after', onclick: () => this.addSection(sec.uid) }),
          h('button', { type: 'button', class: 'ps-danger', text: 'Remove', onclick: () => this.removeSection(sec) })))
      }
      list.appendChild(row)
    })
    const parts = [h('h2', { class: 'ps-panel-title', text: 'Outline' }), list]
    if (editable && !attached.length) parts.push(h('button', { type: 'button', text: 'Add section', onclick: () => this.addSection('') }))
    if (detached.length) {
      parts.push(h('h3', { class: 'ps-panel-subtitle', text: 'Removed from the text' }))
      parts.push(h('p', { class: 'ps-muted', text: 'These sections no longer appear in the document. Their control mappings are kept until you restore or delete them.' }))
      const dl = h('ul', { class: 'ps-outline-list' })
      for (const sec of detached) {
        dl.appendChild(h('li', { class: 'ps-outline-item' }, h('span', { class: 'ps-outline-heading', text: sec.heading }),
          editable ? h('div', { class: 'ps-outline-tools' },
            h('button', { type: 'button', text: 'Restore', onclick: () => this.command('POST', '/studio/sections/' + encodeURIComponent(sec.uid) + '/restore', {}, 'Section restored.') }),
            h('button', { type: 'button', class: 'ps-danger', text: 'Delete', onclick: () => this.removeSection(sec) })) : null))
      }
      parts.push(dl)
    }
    this.outline.replaceChildren(...parts)
  }

  scrollTo(uid) {
    const el = this.canvas.querySelector('section[data-uid="' + CSS.escape(uid) + '"]')
    if (el) {
      el.scrollIntoView({ behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start' })
      const heading = el.querySelector('h1')
      if (heading && this.editor) {
        const pos = this.editor.view.posAtDOM(heading, 0)
        this.editor.commands.focus(pos)
      }
    }
  }

  async command(method, path, body, done) {
    try {
      await api(method, '/policies/' + this.docId + path, body)
      await this.refresh()
      if (done) this.say(done, 'info')
    } catch (err) {
      this.say(err.message, 'error')
      await this.refresh()
    }
  }

  move(index, delta) {
    const uids = this.state.sections.filter((x) => !x.detached).map((x) => x.uid)
    const [moved] = uids.splice(index, 1)
    uids.splice(index + delta, 0, moved)
    this.command('POST', '/studio/reorder', { uids }, 'Section moved.')
  }

  addSection(afterUID) {
    const heading = window.prompt('Heading for the new section', '')
    if (heading === null) return
    this.command('POST', '/studio/sections', { after_uid: afterUID, kind: 'other', heading: heading.trim() }, 'Section added.')
  }

  removeSection(sec) {
    const mapped = sec.controls.length ? ' and its ' + sec.controls.length + ' control mapping(s)' : ''
    if (!window.confirm('Delete "' + (sec.heading || 'this section') + '"' + mapped + '? This cannot be undone.')) return
    this.command('DELETE', '/studio/sections/' + encodeURIComponent(sec.uid), undefined, 'Section deleted.')
  }

  // ---- control chips ----

  chipRow(info) {
    const sec = info.section
    const editable = this.state.can_edit
    const row = h('div', { class: 'ps-chips', contenteditable: 'false', 'aria-label': 'Controls this section satisfies' },
      h('span', { class: 'ps-chips-label', text: 'Satisfies:' }))
    if (!sec.controls.length) row.appendChild(h('span', { class: 'ps-muted', text: 'no controls mapped' }))
    for (const ref of sec.controls) {
      const chip = h('span', { class: 'ps-chip' + (ref.known ? '' : ' ps-chip-unknown'), title: ref.control_name || 'Not in the control catalog' },
        h('span', { text: ref.control_id }), h('span', { class: 'ps-chip-coverage', text: ref.coverage }))
      if (editable) {
        chip.appendChild(h('button', {
          type: 'button', class: 'ps-chip-remove', 'aria-label': 'Remove ' + ref.control_id, text: '×',
          onclick: () => this.command('DELETE', '/sections/' + sec.id + '/controls/' + ref.id, undefined, ref.control_id + ' removed.'),
        }))
      }
      row.appendChild(chip)
    }
    if (editable) row.appendChild(h('button', { type: 'button', class: 'ps-chip-add', text: '+ Control', onclick: (e) => this.openPicker(sec, e.currentTarget) }))
    if (!sec.guidance) return row
    // Template guidance: what the section is for and what it answers to. It is
    // shown here only, never in the document.
    return h('div', { class: 'ps-section-extras', contenteditable: 'false' }, row,
      h('details', { class: 'ps-guidance' }, h('summary', { text: 'About this section' }), h('p', { text: sec.guidance })))
  }

  openPicker(sec, anchor) {
    const input = h('input', { type: 'search', placeholder: 'Search controls, e.g. AC-5', 'aria-label': 'Search the control catalog' })
    const coverage = h('select', { 'aria-label': 'Coverage' })
    for (const c of this.meta.coverage_levels) coverage.appendChild(h('option', { value: c, selected: c === 'partial', text: c }))
    const results = h('ul', { class: 'ps-picker-results', role: 'listbox' })
    const close = () => picker.remove()
    const picker = h('div', { class: 'ps-picker', role: 'dialog', 'aria-label': 'Map a control' },
      h('div', { class: 'ps-picker-row' }, input, coverage, h('button', { type: 'button', text: 'Close', onclick: close })), results)
    let timer = null
    input.addEventListener('input', () => {
      clearTimeout(timer)
      timer = setTimeout(async () => {
        const q = input.value.trim()
        if (!q) { results.replaceChildren(); return }
        try {
          const { data } = await api('GET', '/policies/control-search?q=' + encodeURIComponent(q))
          results.replaceChildren(...(data || []).slice(0, 20).map((opt) => h('li', {},
            h('button', {
              type: 'button', class: 'ps-picker-option',
              onclick: async () => {
                close()
                await this.command('POST', '/sections/' + sec.id + '/controls', { control_id: opt.control_id, coverage: coverage.value }, opt.control_id + ' mapped.')
              },
            }, h('strong', { text: opt.control_id }), ' ', opt.name))))
        } catch (err) { results.replaceChildren(h('li', { class: 'ps-error', text: err.message })) }
      }, 200)
    })
    input.addEventListener('keydown', (e) => { if (e.key === 'Escape') close() })
    anchor.parentNode.insertAdjacentElement('afterend', picker)
    input.focus()
  }

  // ---- side panel: readiness and document control ----

  renderSide() {
    const s = this.state
    const errors = s.findings.filter((f) => f.severity === 'error')
    const warnings = s.findings.filter((f) => f.severity !== 'error')
    const finding = (f) => h('li', { class: 'ps-finding ps-finding-' + f.severity },
      h('span', { class: 'ps-finding-kind', text: f.severity === 'error' ? 'Blocks approval' : 'Advisory' }),
      f.heading ? h('strong', { text: f.heading }) : null, ' ', f.message)
    const readiness = h('section', { class: 'ps-panel', 'aria-labelledby': 'ps-readiness' },
      h('h2', { class: 'ps-panel-title', id: 'ps-readiness', text: 'Readiness' }),
      h('p', { class: 'ps-muted', text: errors.length ? errors.length + ' issue(s) block approval.' : 'Nothing blocks approval.' }),
      h('ul', { class: 'ps-findings' }, errors.map(finding), warnings.map(finding)),
      h('p', { class: 'ps-muted ps-small', text: 'Checked against the saved text, which trails what you type by a few seconds.' }))
    this.side.replaceChildren(readiness, this.factsPanel(), this.documentControl())
  }

  // ---- facts ----

  factsPanel() {
    const s = this.state
    const d = s.document
    const editable = this.canManage && d.status === 'draft'
    const panel = h('section', { class: 'ps-panel', 'aria-labelledby': 'ps-facts' }, h('h2', { class: 'ps-panel-title', id: 'ps-facts', text: 'Facts' }))
    if (!s.client) {
      panel.appendChild(h('p', { class: 'ps-muted', text: 'Choose the client this document is written for. Its facts fill the document\'s fact tokens; until then every token is unresolved.' }))
      if (editable) panel.appendChild(this.clientPicker())
    } else {
      panel.appendChild(h('p', { class: 'ps-muted ps-small' },
        'Facts about ', h('strong', { text: s.client.name }), '. They belong to the client, so every document written for it uses the same values.'))
      if (editable) panel.appendChild(h('button', { type: 'button', class: 'ps-link-button', text: 'Change client', onclick: (e) => { e.currentTarget.replaceWith(this.clientPicker()) } }))
    }
    const facts = s.facts || []
    const missing = facts.filter((f) => f.used && !f.resolved).length
    if (facts.length) panel.appendChild(h('p', { class: 'ps-small ' + (missing ? 'ps-warn' : 'ps-muted'), text: missing ? missing + ' fact(s) used in the text are missing.' : 'Every fact the text uses is filled in.' }))
    const list = h('ul', { class: 'ps-facts' })
    for (const f of facts) {
      const item = h('li', { class: 'ps-fact-row' + (f.used && !f.resolved ? ' ps-fact-missing' : '') },
        h('div', { class: 'ps-fact-head' }, h('strong', { text: f.label }), ' ', h('code', { text: f.key }),
          h('span', { class: 'ps-fact-state', text: f.resolved ? 'filled' : (f.used ? 'missing' : 'not used') })))
      if (f.description) item.appendChild(h('p', { class: 'ps-muted ps-small', text: f.description }))
      if (editable && s.client) {
        const input = h(f.value_type === 'list' ? 'textarea' : 'input', { 'aria-label': f.label, placeholder: f.example ? 'e.g. ' + f.example : '' })
        input.value = f.value || ''
        item.appendChild(input)
        item.appendChild(h('div', { class: 'ps-fact-actions' },
          h('button', { type: 'button', text: 'Save', onclick: () => this.saveFact(f, input.value) }),
          this.editor && this.canEdit ? h('button', { type: 'button', text: 'Insert at cursor', onclick: () => this.insertFact(f.key) }) : null))
      } else if (f.value) {
        item.appendChild(h('p', { class: 'ps-fact-value', text: f.value }))
      }
      list.appendChild(item)
    }
    panel.appendChild(list)
    if (editable && this.editor && this.canEdit) {
      const key = h('input', { 'aria-label': 'New fact key', placeholder: 'another_fact_key' })
      panel.appendChild(h('div', { class: 'ps-fact-new' }, key, h('button', {
        type: 'button', text: 'Insert new fact',
        onclick: () => {
          const k = key.value.trim()
          if (!/^[a-z][a-z0-9_]{0,63}$/.test(k)) { this.say('A fact key is lower-case letters, digits and underscores, starting with a letter.', 'error'); return }
          this.insertFact(k)
          key.value = ''
        },
      })))
    }
    return panel
  }

  insertFact(key) {
    if (!this.editor) return
    this.editor.chain().focus().insertContent({ type: 'factToken', attrs: { key } }).run()
    this.say('Fact ' + key + ' inserted.', 'info')
  }

  async saveFact(f, value) {
    try {
      await api('PUT', '/policies/clients/' + this.state.client.id + '/facts/' + encodeURIComponent(f.key), { value, value_type: f.value_type || 'text' })
      await this.refresh()
      this.say(value.trim() ? f.label + ' saved.' : f.label + ' cleared.', 'info')
    } catch (err) { this.say(f.label + " wasn't saved: " + err.message, 'error') }
  }

  clientPicker() {
    const select = h('select', { 'aria-label': 'Client' }, h('option', { value: '', text: 'Loading clients…' }))
    const box = h('div', { class: 'ps-client-picker' }, select,
      h('button', { type: 'button', text: 'Use this client', onclick: () => this.setClient(Number(select.value), select.options[select.selectedIndex] ? select.options[select.selectedIndex].text : '') }),
      h('button', { type: 'button', text: 'New client', onclick: async () => {
        const name = window.prompt('Client name', '')
        if (!name || !name.trim()) return
        try {
          const { data } = await api('POST', '/policies/clients', { name: name.trim() })
          await this.setClient(data.id, data.name)
        } catch (err) { this.say("The client wasn't created: " + err.message, 'error') }
      } }))
    api('GET', '/policies/clients').then(({ data }) => {
      select.replaceChildren(h('option', { value: '', text: data.length ? 'Choose a client' : 'No clients yet' }),
        ...data.map((c) => h('option', { value: String(c.id), selected: this.state.client && this.state.client.id === c.id, text: c.name })))
    }).catch((err) => { select.replaceChildren(h('option', { value: '', text: err.message })) })
    return box
  }

  async setClient(id, name) {
    if (!id) { this.say('Choose a client first.', 'warn'); return }
    try {
      await api('PUT', '/policies/' + this.docId, Object.assign({}, this.state.document, { client_profile_id: id, client_name: name }))
      await this.refresh()
      this.say('This document is now written for ' + name + '.', 'info')
    } catch (err) { this.say("The client wasn't changed: " + err.message, 'error') }
  }

  documentControl() {
    const d = this.state.document
    const editable = this.canManage && d.status === 'draft'
    const fields = [
      ['title', 'Title', 'text'], ['reference', 'Reference', 'text'], ['owner_role', 'Owner role', 'text'],
      ['approver', 'Approver', 'text'], ['effective_date', 'Effective date', 'date'],
      ['review_cadence_months', 'Review cadence (months)', 'number'],
    ]
    const form = h('form', { class: 'ps-form' })
    const inputs = {}
    for (const [key, label, type] of fields) {
      const input = h('input', { type, value: d[key] === undefined ? '' : String(d[key]), disabled: !editable, id: 'ps-f-' + key })
      inputs[key] = input
      form.appendChild(h('label', { for: 'ps-f-' + key }, label, input))
    }
    const cls = h('select', { id: 'ps-f-classification', disabled: !editable })
    for (const c of this.meta.classifications) cls.appendChild(h('option', { value: c, selected: c === d.classification, text: c }))
    form.appendChild(h('label', { for: 'ps-f-classification' }, 'Classification', cls))
    if (editable) {
      form.appendChild(h('button', { type: 'submit', class: 'ps-primary', text: 'Save document control' }))
      form.addEventListener('submit', async (e) => {
        e.preventDefault()
        const next = Object.assign({}, d)
        for (const [key, , type] of fields) next[key] = type === 'number' ? Number(inputs[key].value || 0) : inputs[key].value
        next.classification = cls.value
        try {
          await api('PUT', '/policies/' + d.id, next)
          await this.refresh()
          this.say('Document control saved.', 'info')
        } catch (err) { this.say("Document control wasn't saved: " + err.message, 'error') }
      })
    }
    return h('section', { class: 'ps-panel' }, h('h2', { class: 'ps-panel-title', text: 'Document control' }), form,
      editable ? null : h('p', { class: 'ps-muted ps-small', text: d.status === 'draft' ? 'Only administrators change document control.' : 'Return the document to draft to change it.' }))
  }
}

function boot() {
  const root = document.getElementById('policy-studio')
  if (!root) return
  const studio = new Studio(root)
  window.GRCPolicyStudio = { version: 1, documentId: root.dataset.documentId }
  studio.start()
}

if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot)
else boot()
