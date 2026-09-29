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
import { ySyncPluginKey, absolutePositionToRelativePosition, relativePositionToAbsolutePosition } from '@tiptap/y-tiptap'
import {
  suggestChanges, suggestChangesKey, isSuggestChangesEnabled, transformToSuggestionTransaction,
  enableSuggestChanges, disableSuggestChanges, applySuggestion, revertSuggestion, selectSuggestion,
} from '@handlewithcare/prosemirror-suggest-changes'
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

function shortTime(iso) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

function isTyping(target) {
  if (!target || !target.closest) return false
  return !!target.closest('[contenteditable="true"], input, textarea, select')
}

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

// ---- suggestions ----

const SUGGESTION_TYPES = ['insertion', 'deletion', 'modification']
const authorKindLabels = { human: 'Consultant', guest: 'Customer', ai: 'AI' }
const suggestionTypeLabels = { insertion: 'Insert', deletion: 'Delete', modification: 'Change' }
const blockLabels = { paragraph: 'a paragraph', heading: 'a heading', listItem: 'a list item', bulletList: 'a list', orderedList: 'a list',
  table: 'a table', tableRow: 'a table row', tableCell: 'a table cell', tableHeader: 'a table cell', blockquote: 'a quotation', callout: 'a callout' }

// structuralSummary describes a suggestion that adds or removes blocks with no
// text of its own, such as an empty table row.
function structuralSummary(x) {
  const what = Array.from(new Set((x.blocks || []).map((b) => blockLabels[b] || 'a block'))).join(', ')
  if (!what) return 'A change to the document structure'
  return (x.types.includes('deletion') ? 'Removes ' : 'Adds ') + what
}

// Suggest mode: edits become suggestion marks rather than changes. Ids are
// UUIDs, not the library's max+1 counter, which two clients suggesting at once
// would both pick. The library only sets `id`; authorship is filled in here
// for the ids this client created, never for a mark that arrived from a peer.
function suggestMode(user) {
  const mine = new Set()
  const newSuid = () => { const id = crypto.randomUUID(); mine.add(id); return id }
  return Extension.create({
    name: 'suggestMode',
    addProseMirrorPlugins() {
      return [
        suggestChanges(),
        new Plugin({
          key: new PluginKey('suggestionAttribution'),
          appendTransaction(trs, _old, state) {
            if (!mine.size || !trs.some((tr) => tr.docChanged && !tr.getMeta('y-sync$'))) return null
            const now = new Date().toISOString()
            let tr = null
            state.doc.descendants((node, pos) => {
              for (const mark of node.marks) {
                if (!SUGGESTION_TYPES.includes(mark.type.name) || mark.attrs.authorId || !mine.has(mark.attrs.id)) continue
                const attrs = Object.assign({}, mark.attrs, { authorId: user.id, authorKind: 'human', authorName: user.name, createdAt: now })
                tr = tr || state.tr
                if (node.isText) tr.removeMark(pos, pos + node.nodeSize, mark).addMark(pos, pos + node.nodeSize, mark.type.create(attrs))
                else tr.removeNodeMark(pos, mark).addNodeMark(pos, mark.type.create(attrs))
              }
            })
            if (tr) tr.setMeta(suggestChangesKey, { skip: true }).setMeta('addToHistory', false)
            return tr
          },
        }),
      ]
    },
    dispatchTransaction({ transaction, next }) {
      const ysync = transaction.getMeta('y-sync$') || {}
      const wrap = isSuggestChangesEnabled(this.editor.state) && transaction.docChanged &&
        !ysync.isChangeOrigin && !ysync.isUndoRedoOperation &&
        !('skip' in (transaction.getMeta(suggestChangesKey) || {}))
      next(wrap ? transformToSuggestionTransaction(transaction, this.editor.state, newSuid) : transaction)
    },
  })
}

// suggestionsIn lists the pending suggestions in document order, one entry
// per id, with the ranges it covers.
function suggestionsIn(doc) {
  const byId = new Map()
  doc.forEach((section, offset) => {
    const heading = section.firstChild ? section.firstChild.textContent : ''
    section.descendants((node, rel) => {
      const pos = offset + 1 + rel
      for (const mark of node.marks) {
        if (!SUGGESTION_TYPES.includes(mark.type.name)) continue
        const a = mark.attrs
        let s = byId.get(a.id)
        if (!s) {
          s = { id: a.id, types: [], sectionUID: section.attrs.uid, heading, authorName: a.authorName || 'Someone', authorKind: a.authorKind || 'human',
            authorId: a.authorId || '', createdAt: a.createdAt || '', inserted: '', deleted: '', changed: '', blocks: [], from: pos, to: pos + node.nodeSize }
          byId.set(a.id, s)
        }
        if (!s.types.includes(mark.type.name)) s.types.push(mark.type.name)
        if (!node.isText && !node.isInline && !s.blocks.includes(node.type.name)) s.blocks.push(node.type.name)
        s.to = Math.max(s.to, pos + node.nodeSize)
        const text = node.textContent.replace(/\u200b/g, '')
        if (mark.type.name === 'insertion') s.inserted += text
        else if (mark.type.name === 'deletion') s.deleted += text
        else s.changed = (a.attrName || 'formatting') + ' changed'
      }
    })
  })
  return Array.from(byId.values())
}

// ---- review decorations: suggestions, authors, comments, provenance ----

const reviewKey = new PluginKey('studioReview')

function relToAbs(state, b64) {
  const y = ySyncPluginKey.getState(state)
  if (!y || !y.binding || !b64) return null
  try {
    const bin = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
    return relativePositionToAbsolutePosition(y.doc, y.type, Y.decodeRelativePosition(bin), y.binding.mapping)
  } catch (_) { return null }
}

function absToRel(state, pos) {
  const y = ySyncPluginKey.getState(state)
  if (!y || !y.binding) return ''
  const rel = absolutePositionToRelativePosition(pos, y.type, y.binding.mapping)
  let bin = ''
  for (const b of Y.encodeRelativePosition(rel)) bin += String.fromCharCode(b)
  return btoa(bin)
}

// threadRange resolves a thread's anchors against the live document; null
// when the commented text is gone.
function threadRange(state, t) {
  const from = relToAbs(state, t.anchor_start)
  const to = relToAbs(state, t.anchor_end)
  if (from === null || to === null || to <= from || to > state.doc.content.size) return null
  return { from, to }
}

function reviewDecorations() {
  return Extension.create({
    name: 'studioReview',
    addProseMirrorPlugins() {
      return [new Plugin({
        key: reviewKey,
        state: {
          init: () => ({ threads: [], showAuthors: false, accepted: {}, currentSuggestion: '', currentThread: 0 }),
          apply: (tr, value) => {
            const next = tr.getMeta(reviewKey)
            return next ? Object.assign({}, value, next) : value
          },
        },
        props: {
          decorations(state) {
            const r = reviewKey.getState(state)
            const decos = []
            state.doc.descendants((node, pos) => {
              const mark = node.marks.find((m) => SUGGESTION_TYPES.includes(m.type.name))
              if (mark) {
                const a = mark.attrs
                const cls = ['ps-sugg', 'ps-sugg-' + mark.type.name]
                if (a.id === r.currentSuggestion) cls.push('ps-sugg-current')
                const attrs = { class: cls.join(' '), 'data-author-kind': a.authorKind || 'human', title: (suggestionTypeLabels[mark.type.name] || 'Change') + ' suggested by ' + (a.authorName || 'someone') }
                if (r.showAuthors) attrs.style = '--ps-author: ' + colorFor(a.authorName || a.authorId || '?')
                if (node.isText) decos.push(Decoration.inline(pos, pos + node.nodeSize, attrs))
                else if (node.isBlock) decos.push(Decoration.node(pos, pos + node.nodeSize, Object.assign({}, attrs, { class: attrs.class + ' ps-sugg-block' })))
              }
              if (r.showAuthors && node.isBlock && node.attrs.bid && r.accepted[node.attrs.bid]) {
                decos.push(Decoration.node(pos, pos + node.nodeSize, { class: 'ps-prov-block', title: r.accepted[node.attrs.bid] }))
              }
            })
            for (const t of r.threads) {
              if (t.status !== 'open') continue
              const range = threadRange(state, t)
              if (!range) continue
              decos.push(Decoration.inline(range.from, range.to, { class: 'ps-comment-mark' + (t.id === r.currentThread ? ' ps-comment-current' : ''), 'data-thread': String(t.id) }))
            }
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
    this.userSuggesting = false
    this.suggestions = []
    this.threads = []
    this.provenance = []
    this.reviewVersion = null
    this.currentSuggestion = ''
    this.currentThread = 0
    this.showAuthors = false
    this.showResolved = false
    this.replyDrafts = {}
    this.composer = null
    this.sideTab = 'readiness'
    try { this.sideTab = localStorage.getItem('grc.policyStudio.tab') || 'readiness' } catch (_) { /* storage may be unavailable */ }
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
    this.loadReview(true)
    this.poll()
    document.addEventListener('keydown', (e) => this.reviewKeys(e))
  }

  buildShell() {
    this.statusPill = h('span', { class: 'ps-pill' })
    this.connection = h('span', { class: 'ps-connection', role: 'status', text: 'Connecting…' })
    this.counts = h('span', { class: 'ps-counts' })
    this.presence = h('ul', { class: 'ps-presence', 'aria-label': 'Here now' })
    // Announcements for screen readers that do not need a visible toast.
    this.live = h('div', { class: 'ps-visually-hidden', 'aria-live': 'polite' })
    this.titleEl = h('h1', { class: 'ps-title' })
    this.actions = h('div', { class: 'ps-actions' })
    this.outline = h('nav', { class: 'ps-outline', 'aria-label': 'Outline' })
    this.canvas = h('div', { class: 'ps-canvas-inner' })
    this.side = h('aside', { class: 'ps-side', 'aria-label': 'Readiness and document control' })
    this.toast = h('div', { class: 'ps-toast', role: 'status', 'aria-live': 'polite' })
    this.root.replaceChildren(
      h('header', { class: 'ps-bar' },
        h('div', { class: 'ps-bar-title' }, h('a', { href: '/policies', class: 'ps-back', text: 'Policies' }), this.titleEl, this.statusPill, this.counts),
        h('div', { class: 'ps-bar-meta' }, this.connection, this.presence),
        this.actions),
      h('div', { class: 'ps-body' }, this.outline, h('section', { class: 'ps-canvas', 'aria-label': 'Document' }, this.canvas), this.side),
      this.toast, this.live)
    this.renderHeader()
    // The bar wraps to more rows as actions are added; the sticky panels
    // below it are offset by its real height.
    const bar = this.root.querySelector('.ps-bar')
    if (window.ResizeObserver) new ResizeObserver(() => this.root.style.setProperty('--ps-bar-h', bar.offsetHeight + 'px')).observe(bar)
  }

  announce(message) {
    this.live.textContent = ''
    setTimeout(() => { this.live.textContent = message }, 50)
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
    this.provider.awareness.on('change', () => this.renderPresence())
  }

  // Who has the document open: one entry per name, from the awareness state
  // the caret extension publishes.
  renderPresence() {
    const me = this.provider.awareness.clientID
    const seen = new Map()
    for (const [id, st] of this.provider.awareness.getStates()) {
      if (!st.user || !st.user.name) continue
      const prev = seen.get(st.user.name)
      seen.set(st.user.name, { name: st.user.name, color: st.user.color, you: (prev && prev.you) || id === me })
    }
    const people = Array.from(seen.values()).sort((a, b) => (b.you - a.you) || a.name.localeCompare(b.name))
    this.presence.replaceChildren(...people.map((p) => {
      const dot = h('span', { class: 'ps-presence-dot', 'aria-hidden': 'true' })
      dot.style.background = p.color || 'var(--accent)'
      return h('li', { class: 'ps-presence-person', title: p.you ? p.name + ' (you)' : p.name }, dot, p.you ? p.name + ' (you)' : p.name)
    }))
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
          suggestMode({ id: this.user, name: this.user }),
          reviewDecorations(),
        ],
      }),
      // Lets suggestMode rewrite local transactions into suggestions.
      enableExtensionDispatchTransaction: true,
      editorProps: {
        attributes: { 'aria-label': 'Policy text', class: 'ps-prosemirror' },
        transformPastedHTML: sanitizePastedHTML,
      },
    })
    this.pushChips()
    this.syncSuggestMode()
    this.pushReview()
    // The header's editor actions (suggest mode, comment, authors) need the
    // editor, which the first render did not have yet.
    this.renderActions()
    this.editor.on('update', () => this.scheduleSuggestionRefresh())
    host.addEventListener('click', (e) => {
      const mark = e.target.closest && e.target.closest('.ps-comment-mark')
      const t = mark && this.threads.find((x) => String(x.id) === mark.dataset.thread)
      if (!t) return
      if (this.sideTab !== 'comments') this.setTab('comments')
      this.focusThread(t)
    })
    this.refreshSuggestions(true)
    window.GRCPolicyStudio.editor = this.editor
    window.GRCPolicyStudio.suggestions = () => suggestionsIn(this.editor.state.doc)
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
          await this.loadReview()
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
    await this.loadReview()
  }

  // Comments and provenance are fetched when the state says they changed.
  async loadReview(force) {
    if (!force && this.reviewVersion === this.state.review_version) return
    this.reviewVersion = this.state.review_version
    try {
      const [{ data: threads }, { data: prov }] = await Promise.all([
        api('GET', '/policies/' + this.docId + '/studio/comments'),
        api('GET', '/policies/' + this.docId + '/studio/provenance'),
      ])
      this.threads = threads || []
      this.provenance = prov || []
      this.pushReview()
      if (this.sideTab === 'comments' || this.sideTab === 'provenance') this.renderSide()
      this.renderCounts()
    } catch (_) { this.reviewVersion = null }
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
    this.syncSuggestMode()
    this.renderActions()
    this.renderOutline()
    this.renderSide()
    this.renderCounts()
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
    const items = []
    if (this.editor && this.state.can_edit) {
      const forced = !!this.state.suggest_only
      items.push(h('button', {
        type: 'button', class: 'ps-suggest-toggle', 'aria-pressed': this.suggesting() ? 'true' : 'false', disabled: forced,
        title: forced ? 'In review, every edit is a suggestion' : 'Make your edits suggestions that someone accepts or rejects',
        text: forced ? 'Suggesting (in review)' : (this.suggesting() ? 'Suggesting' : 'Editing'),
        onclick: () => { this.userSuggesting = !this.userSuggesting; this.syncSuggestMode(); this.renderActions() },
      }))
    }
    if (this.editor && this.state.can_comment) items.push(btn('Comment', () => this.openComposer()))
    if (this.editor) {
      items.push(h('button', { type: 'button', 'aria-pressed': this.showAuthors ? 'true' : 'false', text: 'Show authors',
        title: 'Colour suggestions by author and mark accepted text', onclick: () => { this.showAuthors = !this.showAuthors; this.pushReview(); this.renderActions() } }))
    }
    items.push(
      h('button', { type: 'button', 'aria-pressed': paper ? 'true' : 'false', text: paper ? 'Screen view' : 'Paper view', onclick: () => this.togglePaper() }),
      link('/policies/' + d.id + '/view', 'Preview'), link('/templates/render?doc=' + d.id, 'Render PDF'))
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

  // ---- suggest mode and review state ----

  suggesting() { return !!(this.state.suggest_only || this.userSuggesting) }

  syncSuggestMode() {
    if (!this.editor) return
    const want = !!(this.state.can_edit && this.suggesting())
    if (want === isSuggestChangesEnabled(this.editor.state)) return
    ;(want ? enableSuggestChanges : disableSuggestChanges)(this.editor.state, this.editor.view.dispatch)
    this.canvas.classList.toggle('ps-suggesting', want)
  }

  pushReview() {
    if (!this.editor) return
    const accepted = {}
    for (const sec of this.provenance) {
      for (const d of sec.decisions) {
        if (d.decision !== 'accepted') continue
        for (const bid of d.block_ids || []) {
          const line = (d.author_name || 'Someone') + ' (' + (authorKindLabels[d.author_kind] || 'consultant').toLowerCase() + ') suggested this; ' + d.decided_by + ' accepted it ' + shortTime(d.decided_at)
          accepted[bid] = accepted[bid] ? accepted[bid] + '\n' + line : line
        }
      }
    }
    this.editor.view.dispatch(this.editor.state.tr.setMeta(reviewKey, {
      threads: this.threads, showAuthors: this.showAuthors, accepted, currentSuggestion: this.currentSuggestion, currentThread: this.currentThread,
    }).setMeta('addToHistory', false))
  }

  scheduleSuggestionRefresh() {
    clearTimeout(this.suggestionTimer)
    this.suggestionTimer = setTimeout(() => this.refreshSuggestions(false), 250)
  }

  // refreshSuggestions re-reads the pending suggestions from the editor and
  // re-renders the panel only when the list changed.
  refreshSuggestions(initial) {
    if (!this.editor) return
    const list = suggestionsIn(this.editor.state.doc)
    const sig = list.map((x) => x.id + ':' + x.types.join('') + ':' + x.inserted.length + ':' + x.deleted.length).join('|')
    if (sig === this.suggestionSig) return
    const before = new Set(this.suggestions.map((x) => x.id))
    const fresh = list.filter((x) => !before.has(x.id) && x.authorId !== this.user)
    this.suggestionSig = sig
    this.suggestions = list
    if (!initial && fresh.length) {
      const names = Array.from(new Set(fresh.map((x) => x.authorName))).join(', ')
      this.announce(fresh.length + ' new suggestion' + (fresh.length === 1 ? '' : 's') + ' from ' + names + '.')
    }
    if (this.currentSuggestion && !list.some((x) => x.id === this.currentSuggestion)) this.currentSuggestion = ''
    this.renderCounts()
    if (this.sideTab === 'suggestions' || this.sideTab === 'provenance') this.renderSide()
  }

  renderCounts() {
    const n = this.suggestions.length
    const open = this.threads.filter((t) => t.status === 'open').length
    const parts = []
    if (n) parts.push(n + ' suggestion' + (n === 1 ? '' : 's'))
    if (open) parts.push(open + ' comment' + (open === 1 ? '' : 's'))
    this.counts.textContent = parts.join(' · ')
    this.counts.hidden = !parts.length
    if (this.tabButtons) {
      this.tabButtons.suggestions.textContent = 'Suggestions' + (n ? ' (' + n + ')' : '')
      this.tabButtons.comments.textContent = 'Comments' + (open ? ' (' + open + ')' : '')
    }
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

  // ---- side panels ----

  setTab(key) {
    this.sideTab = key
    try { localStorage.setItem('grc.policyStudio.tab', key) } catch (_) { /* storage may be unavailable */ }
    this.renderSide()
  }

  renderSide() {
    const tabs = [['suggestions', 'Suggestions'], ['comments', 'Comments'], ['readiness', 'Readiness'], ['facts', 'Facts'],
      ['provenance', 'Provenance'], ['document', 'Document']]
    if (!tabs.some(([k]) => k === this.sideTab)) this.sideTab = 'readiness'
    const focusedId = this.side.contains(document.activeElement) && document.activeElement.dataset ? document.activeElement.dataset.focusKey : ''
    this.tabButtons = {}
    const bar = h('div', { class: 'ps-tabs', role: 'tablist', 'aria-label': 'Panels' })
    tabs.forEach(([key, label], i) => {
      const b = h('button', {
        type: 'button', role: 'tab', id: 'ps-tab-' + key, class: 'ps-tab', 'aria-selected': key === this.sideTab ? 'true' : 'false',
        'aria-controls': 'ps-tabpanel', tabindex: key === this.sideTab ? '0' : '-1', 'data-focus-key': 'tab-' + key, text: label,
        onclick: () => this.setTab(key),
        onkeydown: (e) => {
          if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
          e.preventDefault()
          const next = tabs[(i + (e.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length][0]
          this.setTab(next)
          this.tabButtons[next].focus()
        },
      })
      this.tabButtons[key] = b
      bar.appendChild(b)
    })
    const body = { suggestions: () => this.suggestionsPanel(), comments: () => this.commentsPanel(), readiness: () => this.readinessPanel(),
      facts: () => this.factsPanel(), provenance: () => this.provenancePanel(), document: () => this.documentControl() }[this.sideTab]()
    this.side.replaceChildren(bar, h('div', { class: 'ps-tabpanel', role: 'tabpanel', id: 'ps-tabpanel', 'aria-labelledby': 'ps-tab-' + this.sideTab }, body))
    this.renderCounts()
    if (focusedId) {
      const again = this.side.querySelector('[data-focus-key="' + CSS.escape(focusedId) + '"]')
      if (again) again.focus()
    }
  }

  readinessPanel() {
    const s = this.state
    const errors = s.findings.filter((f) => f.severity === 'error')
    const warnings = s.findings.filter((f) => f.severity !== 'error')
    const finding = (f) => h('li', { class: 'ps-finding ps-finding-' + f.severity },
      h('span', { class: 'ps-finding-kind', text: f.severity === 'error' ? 'Blocks approval' : 'Advisory' }),
      f.heading ? h('strong', { text: f.heading }) : null, ' ', f.message)
    return h('section', { class: 'ps-panel', 'aria-labelledby': 'ps-readiness' },
      h('h2', { class: 'ps-panel-title', id: 'ps-readiness', text: 'Readiness' }),
      h('p', { class: 'ps-muted', text: errors.length ? errors.length + ' issue(s) block approval.' : 'Nothing blocks approval.' }),
      h('ul', { class: 'ps-findings' }, errors.map(finding), warnings.map(finding)),
      h('p', { class: 'ps-muted ps-small', text: 'Checked against the saved text, which trails what you type by a few seconds.' }))
  }

  // ---- suggestions panel ----

  visibleSuggestions() {
    const f = this.suggestionFilter || 'all'
    return this.suggestions.filter((x) => f === 'all' || x.authorKind === f)
  }

  suggestionsPanel() {
    const canDecide = !!this.state.can_decide && !!this.editor
    const list = this.visibleSuggestions()
    const panel = h('section', { class: 'ps-panel', 'aria-labelledby': 'ps-suggestions' },
      h('h2', { class: 'ps-panel-title', id: 'ps-suggestions', text: 'Suggestions' }),
      h('p', { class: 'ps-muted ps-small', text: canDecide
        ? 'J and K move between suggestions; A accepts and R rejects. Approval waits until every suggestion is decided.'
        : 'J and K move between suggestions. Administrators accept or reject them; approval waits until every one is decided.' }))
    const filter = h('select', { 'aria-label': 'Show suggestions from', 'data-focus-key': 'sugg-filter',
      onchange: (e) => { this.suggestionFilter = e.target.value; this.renderSide() } })
    for (const [v, label] of [['all', 'Everyone'], ['human', 'Consultants'], ['guest', 'Customers'], ['ai', 'AI']]) {
      filter.appendChild(h('option', { value: v, selected: v === (this.suggestionFilter || 'all'), text: label }))
    }
    panel.appendChild(h('label', { class: 'ps-inline-label' }, 'Show suggestions from ', filter))
    if (!list.length) {
      panel.appendChild(h('p', { class: 'ps-muted', text: this.suggestions.length ? 'None from these authors.' : 'No open suggestions.' }))
      return panel
    }
    if (canDecide) {
      panel.appendChild(h('div', { class: 'ps-review-all' },
        h('button', { type: 'button', text: 'Accept all (' + list.length + ')', onclick: () => this.decide(list.map((x) => x.id), 'accept', true) }),
        h('button', { type: 'button', class: 'ps-danger', text: 'Reject all', onclick: () => this.decide(list.map((x) => x.id), 'reject', true) })))
    }
    const groups = []
    for (const x of list) {
      let g = groups.find((y) => y.uid === x.sectionUID)
      if (!g) { g = { uid: x.sectionUID, heading: x.heading, items: [] }; groups.push(g) }
      g.items.push(x)
    }
    for (const g of groups) {
      const head = h('div', { class: 'ps-sugg-group-head' }, h('h3', { class: 'ps-panel-subtitle', text: g.heading || 'Untitled section' }))
      if (canDecide) {
        head.appendChild(h('span', { class: 'ps-sugg-group-tools' },
          h('button', { type: 'button', text: 'Accept section', onclick: () => this.decide(g.items.map((x) => x.id), 'accept') }),
          h('button', { type: 'button', class: 'ps-danger', text: 'Reject section', onclick: () => this.decide(g.items.map((x) => x.id), 'reject') })))
      }
      const ul = h('ul', { class: 'ps-sugg-list' })
      for (const x of g.items) ul.appendChild(this.suggestionItem(x, canDecide))
      panel.appendChild(head)
      panel.appendChild(ul)
    }
    return panel
  }

  suggestionItem(x, canDecide) {
    const kind = authorKindLabels[x.authorKind] || 'Consultant'
    const what = x.types.map((t) => suggestionTypeLabels[t] || t).join(' and ')
    const li = h('li', {
      class: 'ps-sugg-item' + (x.id === this.currentSuggestion ? ' ps-sugg-item-current' : ''), tabindex: '0', 'data-focus-key': 'sugg-' + x.id,
      'aria-label': what + ' suggested by ' + x.authorName, onclick: (e) => { if (!e.target.closest('button')) this.focusSuggestion(x.id) },
    },
    h('div', { class: 'ps-sugg-meta' }, h('strong', { text: x.authorName }), ' ',
      h('span', { class: 'ps-kind-badge', 'data-kind': x.authorKind, text: kind }), ' ', h('span', { class: 'ps-muted', text: what })))
    const ex = h('p', { class: 'ps-sugg-excerpt' })
    if (x.deleted) ex.appendChild(h('del', { text: x.deleted.length > 160 ? x.deleted.slice(0, 160) + '…' : x.deleted }))
    if (x.deleted && x.inserted) ex.appendChild(document.createTextNode(' → '))
    if (x.inserted) ex.appendChild(h('ins', { text: x.inserted.length > 160 ? x.inserted.slice(0, 160) + '…' : x.inserted }))
    if (x.changed) ex.appendChild(h('span', { class: 'ps-muted', text: x.changed }))
    if (!x.inserted && !x.deleted && !x.changed) ex.appendChild(h('span', { class: 'ps-muted', text: structuralSummary(x) }))
    li.appendChild(ex)
    if (canDecide) {
      li.appendChild(h('div', { class: 'ps-sugg-actions' },
        h('button', { type: 'button', class: 'ps-primary', text: 'Accept', onclick: () => this.decide([x.id], 'accept') }),
        h('button', { type: 'button', text: 'Reject', onclick: () => this.decide([x.id], 'reject') })))
    }
    return li
  }

  focusSuggestion(id, focusItem) {
    this.currentSuggestion = id
    if (this.editor) {
      selectSuggestion(id)(this.editor.state, this.editor.view.dispatch)
      const s = this.suggestions.find((x) => x.id === id)
      if (s) {
        const dom = this.editor.view.domAtPos(s.from)
        const el = dom && (dom.node.nodeType === 1 ? dom.node : dom.node.parentElement)
        if (el) el.scrollIntoView({ block: 'center', behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
      }
    }
    this.pushReview()
    if (this.sideTab !== 'suggestions') this.setTab('suggestions')
    this.side.querySelectorAll('.ps-sugg-item').forEach((el) => el.classList.toggle('ps-sugg-item-current', el.dataset.focusKey === 'sugg-' + id))
    if (focusItem) {
      const item = this.side.querySelector('[data-focus-key="' + CSS.escape('sugg-' + id) + '"]')
      if (item) item.focus()
    }
  }

  // Keyboard review: J and K move, A accepts, R rejects -- whenever the caret
  // is not in the text or a form field, so typing a J is never a command.
  reviewKeys(e) {
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || isTyping(e.target)) return
    const key = e.key.toLowerCase()
    if (!['j', 'k', 'a', 'r'].includes(key) || !this.editor) return
    const list = this.visibleSuggestions()
    if (!list.length) return
    e.preventDefault()
    const i = list.findIndex((x) => x.id === this.currentSuggestion)
    if (key === 'j' || key === 'k') {
      const next = i < 0 ? (key === 'j' ? 0 : list.length - 1) : (i + (key === 'j' ? 1 : list.length - 1)) % list.length
      this.focusSuggestion(list[next].id, true)
      return
    }
    if (i < 0) { this.say('Press J to choose a suggestion first.', 'info'); return }
    if (!this.state.can_decide) { this.say('Only administrators accept or reject suggestions.', 'warn'); return }
    const after = list[i + 1] || list[i - 1]
    this.decide([list[i].id], key === 'a' ? 'accept' : 'reject').then((ok) => { if (ok && after) this.focusSuggestion(after.id, true) })
  }

  // decide accepts or rejects suggestions. It first checks, on a scratch
  // copy of the editor state, that the change can be made (the section
  // integrity filter refuses one that would add or remove a whole section),
  // then records the decision with the server -- which checks every id is
  // still pending and takes the actor from the session -- and only then
  // changes the shared text.
  async decide(ids, decision, confirmAll) {
    if (!this.editor || !ids.length) return false
    const verb = decision === 'accept' ? 'Accept' : 'Reject'
    if (confirmAll && !window.confirm(verb + ' all ' + ids.length + ' suggestion(s)?')) return false
    const cmd = decision === 'accept' ? applySuggestion : revertSuggestion
    let scratch = this.editor.state
    for (const id of ids) cmd(id)(scratch, (tr) => { scratch = scratch.apply(tr) })
    const left = new Set(suggestionsIn(scratch.doc).map((x) => x.id))
    if (ids.some((id) => left.has(id))) {
      this.say(decision === 'accept'
        ? "This suggestion adds or removes a whole section, which the text can't do. Change sections from the outline, then reject the suggestion."
        : "This suggestion couldn't be rejected here. Reload the page and try again.", 'error')
      return false
    }
    try {
      await api('POST', '/policies/' + this.docId + '/studio/decisions', { decision, suggestion_ids: ids })
    } catch (err) {
      this.say(err.message, 'error')
      this.refreshSuggestions(false)
      return false
    }
    for (const id of ids) cmd(id)(this.editor.state, this.editor.view.dispatch)
    const done = ids.length + ' suggestion' + (ids.length === 1 ? '' : 's') + (decision === 'accept' ? ' accepted.' : ' rejected.')
    this.say(done, 'info')
    this.announce(done)
    this.refreshSuggestions(false)
    this.loadReview(true)
    return true
  }

  // ---- comments ----

  openComposer() {
    if (!this.editor) return
    const st = this.editor.state
    const { from, to, empty } = st.selection
    if (empty) { this.say('Select the text you want to comment on first.', 'warn'); return }
    const section = st.doc.resolve(from).node(1)
    if (!section || !section.attrs.uid) { this.say('Select text inside a section.', 'warn'); return }
    this.composer = {
      uid: section.attrs.uid, start: absToRel(st, from), end: absToRel(st, to),
      quote: st.doc.textBetween(from, to, ' ').slice(0, 1000), body: '', visibility: 'internal',
    }
    this.setTab('comments')
    const box = this.side.querySelector('[data-focus-key="composer-body"]')
    if (box) box.focus()
  }

  async postComment() {
    const c = this.composer
    if (!c.body.trim()) { this.say('Write something first.', 'warn'); return }
    try {
      await api('POST', '/policies/' + this.docId + '/studio/comments', {
        section_uid: c.uid, anchor_start: c.start, anchor_end: c.end, quote: c.quote, visibility: c.visibility, body: c.body,
      })
      this.composer = null
      await this.loadReview(true)
      this.renderSide()
      this.say('Comment posted.', 'info')
    } catch (err) { this.say("The comment wasn't posted: " + err.message, 'error') }
  }

  async threadAction(t, method, path, body, done) {
    try {
      await api(method, '/policies/' + this.docId + '/studio/comments/' + t.id + path, body)
      delete this.replyDrafts[t.id]
      await this.loadReview(true)
      this.renderSide()
      this.say(done, 'info')
    } catch (err) { this.say(err.message, 'error') }
  }

  focusThread(t) {
    this.currentThread = t.id
    const range = this.editor ? threadRange(this.editor.state, t) : null
    if (range) {
      this.editor.commands.setTextSelection(range)
      const dom = this.editor.view.domAtPos(range.from)
      const el = dom && (dom.node.nodeType === 1 ? dom.node : dom.node.parentElement)
      if (el) el.scrollIntoView({ block: 'center', behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
    }
    this.pushReview()
    this.side.querySelectorAll('.ps-thread').forEach((el) => el.classList.toggle('ps-thread-current', el.dataset.thread === String(t.id)))
  }

  commentsPanel() {
    const can = !!this.state.can_comment
    const panel = h('section', { class: 'ps-panel', 'aria-labelledby': 'ps-comments' }, h('h2', { class: 'ps-panel-title', id: 'ps-comments', text: 'Comments' }))
    if (this.composer) {
      const c = this.composer
      const body = h('textarea', { 'aria-label': 'Comment', 'data-focus-key': 'composer-body', placeholder: 'Your comment', oninput: (e) => { c.body = e.target.value } })
      body.value = c.body
      const vis = h('select', { 'aria-label': 'Who can see it', onchange: (e) => { c.visibility = e.target.value } },
        h('option', { value: 'internal', selected: c.visibility === 'internal', text: 'Internal: your team only' }),
        h('option', { value: 'shared', selected: c.visibility === 'shared', text: 'Shared: the client sees it when they join' }))
      panel.appendChild(h('div', { class: 'ps-composer' },
        h('blockquote', { class: 'ps-quote', text: c.quote }), body, vis,
        h('div', { class: 'ps-fact-actions' },
          h('button', { type: 'button', class: 'ps-primary', text: 'Post comment', onclick: () => this.postComment() }),
          h('button', { type: 'button', text: 'Cancel', onclick: () => { this.composer = null; this.renderSide() } }))))
    } else if (can) {
      panel.appendChild(h('p', { class: 'ps-muted ps-small', text: 'Select text in the document and choose Comment.' }))
    }
    const resolved = this.threads.filter((t) => t.status !== 'open').length
    if (resolved) {
      const box = h('input', { type: 'checkbox', 'data-focus-key': 'show-resolved', onchange: (e) => { this.showResolved = e.target.checked; this.renderSide() } })
      box.checked = this.showResolved
      panel.appendChild(h('label', { class: 'ps-inline-label' }, box, ' Show resolved (' + resolved + ')'))
    }
    const shown = this.threads.filter((t) => this.showResolved || t.status === 'open')
    if (!shown.length && !this.composer) panel.appendChild(h('p', { class: 'ps-muted', text: 'No open comments.' }))
    const st = this.editor ? this.editor.state : null
    const pos = (t) => { const r = st && threadRange(st, t); return r ? r.from : Infinity }
    shown.sort((a, b) => pos(a) - pos(b))
    const ul = h('ul', { class: 'ps-threads' })
    for (const t of shown) ul.appendChild(this.threadItem(t, can, st))
    panel.appendChild(ul)
    return panel
  }

  threadItem(t, can, st) {
    const range = st ? threadRange(st, t) : null
    const li = h('li', { class: 'ps-thread' + (t.id === this.currentThread ? ' ps-thread-current' : '') + (t.status !== 'open' ? ' ps-thread-resolved' : ''), 'data-thread': String(t.id) },
      h('div', { class: 'ps-sugg-meta' },
        h('span', { class: 'ps-kind-badge', 'data-visibility': t.visibility, text: t.visibility === 'shared' ? 'Shared' : 'Internal' }), ' ',
        t.status === 'open' ? null : h('span', { class: 'ps-muted', text: 'Resolved by ' + t.resolved_by + ' ' })))
    if (range) {
      li.appendChild(h('button', { type: 'button', class: 'ps-quote ps-quote-link', 'data-focus-key': 'thread-' + t.id, text: t.quote, onclick: () => this.focusThread(t) }))
    } else {
      li.appendChild(h('blockquote', { class: 'ps-quote', text: t.quote }))
      li.appendChild(h('p', { class: 'ps-muted ps-small', text: 'The text this comment was on has been removed or rewritten.' }))
    }
    const ol = h('ol', { class: 'ps-comments' })
    for (const c of t.comments) {
      ol.appendChild(h('li', {}, h('div', { class: 'ps-small ps-muted' }, h('strong', { text: c.author || 'Someone' }), ' · ' + shortTime(c.created_at)),
        h('p', { class: 'ps-comment-body', text: c.body })))
    }
    li.appendChild(ol)
    if (can) {
      const reply = h('textarea', { 'aria-label': 'Reply', placeholder: 'Reply', 'data-focus-key': 'reply-' + t.id, oninput: (e) => { this.replyDrafts[t.id] = e.target.value } })
      reply.value = this.replyDrafts[t.id] || ''
      li.appendChild(reply)
      li.appendChild(h('div', { class: 'ps-fact-actions' },
        h('button', { type: 'button', text: 'Reply', onclick: () => {
          if (!(this.replyDrafts[t.id] || '').trim()) { this.say('Write a reply first.', 'warn'); return }
          this.threadAction(t, 'POST', '/replies', { body: this.replyDrafts[t.id] }, 'Reply posted.')
        } }),
        t.status === 'open'
          ? h('button', { type: 'button', text: 'Resolve', onclick: () => this.threadAction(t, 'PATCH', '', { status: 'resolved' }, 'Comment resolved.') })
          : h('button', { type: 'button', text: 'Reopen', onclick: () => this.threadAction(t, 'PATCH', '', { status: 'open' }, 'Comment reopened.') })))
    }
    return li
  }

  // ---- provenance ----

  provenancePanel() {
    const panel = h('section', { class: 'ps-panel', 'aria-labelledby': 'ps-provenance' },
      h('h2', { class: 'ps-panel-title', id: 'ps-provenance', text: 'Provenance' }),
      h('p', { class: 'ps-muted ps-small', text: 'Where each section came from, and who suggested, accepted or rejected each change. Show authors marks the accepted text in the document.' }))
    const byUID = {}
    for (const p of this.provenance) byUID[p.uid] = p
    for (const sec of this.state.sections.filter((x) => !x.detached)) {
      const p = byUID[sec.uid]
      const box = h('div', { class: 'ps-prov-section' },
        h('h3', { class: 'ps-panel-subtitle', text: sec.heading || 'Untitled section' }),
        h('p', { class: 'ps-small', text: 'Origin: ' + (p ? p.origin_label : '—') }))
      const pending = this.suggestions.filter((x) => x.sectionUID === sec.uid)
      if (pending.length) {
        const names = Array.from(new Set(pending.map((x) => x.authorName + ' (' + (authorKindLabels[x.authorKind] || 'Consultant').toLowerCase() + ')')))
        box.appendChild(h('p', { class: 'ps-small', text: pending.length + ' open suggestion(s) from ' + names.join(', ') }))
      }
      const decisions = p ? p.decisions : []
      if (decisions.length) {
        const ul = h('ul', { class: 'ps-prov-list' })
        for (const d of decisions) {
          const text = d.inserted || d.deleted || d.changed || structuralSummary(d).toLowerCase()
          ul.appendChild(h('li', { class: 'ps-small' },
            h('strong', { text: d.author_name || 'Someone' }), ' (' + (authorKindLabels[d.author_kind] || 'Consultant').toLowerCase() + ') suggested ',
            h(d.inserted ? 'ins' : (d.deleted ? 'del' : 'span'), { text: text.length > 120 ? text.slice(0, 120) + '…' : text }),
            ' — ' + d.decision + ' by ', h('strong', { text: d.decided_by || 'someone' }), ', ' + shortTime(d.decided_at)))
        }
        box.appendChild(ul)
      } else if (!pending.length) {
        box.appendChild(h('p', { class: 'ps-muted ps-small', text: 'No suggestions decided here yet.' }))
      }
      panel.appendChild(box)
    }
    return panel
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
