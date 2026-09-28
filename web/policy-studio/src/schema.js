// The Policy Studio document schema: one definition for the browser editor and
// the Node fixture generator, dumped to schema.snapshot.json so the Go
// validator (internal/policystudio/schema.go) can be checked against it.
//
// Every node and mark here needs a Markdown, Typst, LaTeX and HTML rendering
// and a Go validator rule, so the set is kept small on purpose.
import { Node, Mark, mergeAttributes } from '@tiptap/core'
import Text from '@tiptap/extension-text'
import Paragraph from '@tiptap/extension-paragraph'
import Bold from '@tiptap/extension-bold'
import Italic from '@tiptap/extension-italic'
import Code from '@tiptap/extension-code'
import Link from '@tiptap/extension-link'
import HardBreak from '@tiptap/extension-hard-break'
import Heading from '@tiptap/extension-heading'
import Blockquote from '@tiptap/extension-blockquote'
import { BulletList, OrderedList, ListItem, ListKeymap } from '@tiptap/extension-list'
import { Table, TableRow, TableHeader, TableCell } from '@tiptap/extension-table'
import { Dropcursor, Gapcursor } from '@tiptap/extensions'
import UniqueID from '@tiptap/extension-unique-id'

// The Y.XmlFragment both the editor and the server read. Changing it orphans
// every stored document.
export const FRAGMENT = 'policy'
export const SUGGESTION_MARKS = 'insertion deletion modification'

// Block types that carry a stable block id (bid): anchors, comments and AI
// edits all address blocks by it.
export const BID_TYPES = ['sectionHeading', 'paragraph', 'heading', 'listItem', 'tableCell', 'tableHeader', 'blockquote', 'callout']

export const LINK_SCHEMES = /^(https?:|mailto:)/i

const PolicyDocument = Node.create({
  name: 'doc',
  topNode: true,
  content: 'policySection+',
})

const PolicySection = Node.create({
  name: 'policySection',
  content: 'sectionHeading block+',
  defining: true,
  isolating: true,
  marks: SUGGESTION_MARKS,
  addAttributes() {
    return {
      uid: { default: null, parseHTML: (el) => el.getAttribute('data-uid'), renderHTML: (a) => (a.uid ? { 'data-uid': a.uid } : {}) },
      kind: { default: 'other', parseHTML: (el) => el.getAttribute('data-kind') || 'other', renderHTML: (a) => ({ 'data-kind': a.kind }) },
    }
  },
  parseHTML() { return [{ tag: 'section[data-uid]' }] },
  renderHTML({ HTMLAttributes }) { return ['section', mergeAttributes(HTMLAttributes, { class: 'ps-section' }), 0] },
})

const SectionHeading = Node.create({
  name: 'sectionHeading',
  content: 'text*',
  // Only suggestion marks: the heading is plain text that maps to the row's
  // heading column, but editing it in suggest mode still has to be tracked.
  marks: SUGGESTION_MARKS,
  defining: true,
  parseHTML() { return [{ tag: 'h1.ps-section-heading' }] },
  renderHTML({ HTMLAttributes }) { return ['h1', mergeAttributes(HTMLAttributes, { class: 'ps-section-heading' }), 0] },
})

const Callout = Node.create({
  name: 'callout',
  group: 'block',
  content: 'paragraph+',
  defining: true,
  marks: SUGGESTION_MARKS,
  addAttributes() {
    return { kind: { default: 'note', parseHTML: (el) => el.getAttribute('data-kind') || 'note', renderHTML: (a) => ({ 'data-kind': a.kind }) } }
  },
  parseHTML() { return [{ tag: 'aside[data-callout]' }] },
  renderHTML({ HTMLAttributes }) { return ['aside', mergeAttributes(HTMLAttributes, { 'data-callout': '', class: 'ps-callout' }), 0] },
})

const FactToken = Node.create({
  name: 'factToken',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  addAttributes() { return { key: { default: '' } } },
  parseHTML() { return [{ tag: 'span[data-fact]', getAttrs: (el) => ({ key: el.getAttribute('data-fact') }) }] },
  renderHTML({ node }) { return ['span', { 'data-fact': node.attrs.key, class: 'ps-fact' }, node.attrs.key] },
})

const ControlRef = Node.create({
  name: 'controlRef',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  addAttributes() { return { controlId: { default: '' } } },
  parseHTML() { return [{ tag: 'span[data-control]', getAttrs: (el) => ({ controlId: el.getAttribute('data-control') }) }] },
  renderHTML({ node }) { return ['span', { 'data-control': node.attrs.controlId, class: 'ps-control' }, node.attrs.controlId] },
})

// Suggestion marks mirror @handlewithcare/prosemirror-suggest-changes' specs
// (same names, excludes and inclusive) with authorship attributes added. The
// library only sets `id`; the Studio fills the rest in for local transactions.
function suggestionAttrs(extra) {
  return Object.assign({
    id: { default: null },
    authorId: { default: null },
    authorKind: { default: null },
    authorName: { default: null },
    proposalId: { default: null },
    createdAt: { default: null },
  }, extra || {})
}

const Insertion = Mark.create({
  name: 'insertion',
  inclusive: false,
  excludes: 'deletion modification insertion',
  addAttributes() { return suggestionAttrs() },
  parseHTML() { return [{ tag: 'ins[data-id]' }] },
  renderHTML({ mark, HTMLAttributes }) { return ['ins', mergeAttributes(HTMLAttributes, { 'data-id': JSON.stringify(mark.attrs.id), 'data-kind': mark.attrs.authorKind || '' }), 0] },
})

const Deletion = Mark.create({
  name: 'deletion',
  inclusive: false,
  excludes: 'insertion modification deletion',
  addAttributes() { return suggestionAttrs() },
  parseHTML() { return [{ tag: 'del[data-id]' }] },
  renderHTML({ mark, HTMLAttributes }) { return ['del', mergeAttributes(HTMLAttributes, { 'data-id': JSON.stringify(mark.attrs.id), 'data-kind': mark.attrs.authorKind || '' }), 0] },
})

const Modification = Mark.create({
  name: 'modification',
  inclusive: false,
  excludes: 'deletion insertion',
  addAttributes() {
    return suggestionAttrs({
      type: { default: null },
      attrName: { default: null },
      previousValue: { default: null },
      newValue: { default: null },
    })
  },
  parseHTML() { return [{ tag: 'span[data-type="modification"]' }] },
  renderHTML({ HTMLAttributes }) { return ['span', mergeAttributes(HTMLAttributes, { 'data-type': 'modification' }), 0] },
})

// Containers must allow the suggestion marks on their children, or a
// whole-block suggestion (insert or delete a paragraph, list item or row)
// cannot be expressed: suggest-changes records those as node marks. Tiptap's
// extendNodeSchema cannot do this -- a node's own `marks` field overwrites it --
// so each container is extended, and its content narrowed to what every
// renderer handles.
const container = (ext, content) => ext.extend(Object.assign({ marks: SUGGESTION_MARKS }, content ? { content } : {}))

// Cells lose Tiptap's `align` attribute: the brand decides appearance, and a
// per-cell alignment would be one more thing every renderer has to honour.
const cell = (ext) => container(ext, 'paragraph+').extend({
  addAttributes() {
    const attrs = Object.assign({}, this.parent ? this.parent() : {})
    delete attrs.align
    return attrs
  },
})

export function studioExtensions(opts) {
  const o = opts || {}
  return [
    PolicyDocument,
    PolicySection,
    SectionHeading,
    Text,
    Paragraph,
    Bold,
    Italic,
    // Tiptap's code mark excludes every other mark, which would drop a
    // suggestion typed inside inline code; it only needs to exclude formatting.
    Code.extend({ excludes: 'code bold italic link' }),
    Link.configure({
      openOnClick: false,
      autolink: false,
      linkOnPaste: true,
      protocols: [],
      defaultProtocol: 'https',
      HTMLAttributes: { rel: 'noopener noreferrer', target: null, class: null },
      isAllowedUri: (url) => LINK_SCHEMES.test(String(url || '')),
    }),
    HardBreak,
    Heading.configure({ levels: [2, 3] }),
    container(Blockquote, 'paragraph+'),
    container(BulletList),
    container(OrderedList),
    container(ListItem, 'paragraph (paragraph | bulletList | orderedList)*'),
    ListKeymap,
    container(Table).configure({ resizable: false }),
    container(TableRow),
    cell(TableHeader),
    cell(TableCell),
    Callout,
    FactToken,
    ControlRef,
    Insertion,
    Deletion,
    Modification,
    Dropcursor,
    Gapcursor,
    UniqueID.configure(Object.assign({ attributeName: 'bid', types: BID_TYPES }, o.generateID ? { generateID: o.generateID } : {})),
  ].concat(o.extra || [])
}
