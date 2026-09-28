// Cross-language fixtures: the contract between the Go projection/seeder
// (internal/policystudio) and the editor.
//
//   node fixtures/gen.mjs gen      write fixtures/cases/*.json (ProseMirror JSON in,
//                                  the Yjs update the pinned binding writes, and what
//                                  JS reads back) and schema.snapshot.json
//   node fixtures/gen.mjs verify   read every Go-seeded update in fixtures/go-seeded/,
//                                  assert JS renders its case's expected JSON, and
//                                  record the verified update hashes in
//                                  fixtures/go-seeded/js-verified.json
//   node fixtures/gen.mjs read F   print the ProseMirror JSON JS renders from F
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import * as Y from 'yjs'
import { getSchema } from '@tiptap/core'
import crypto from 'node:crypto'
import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProseMirrorRootNode } from '@tiptap/y-tiptap'
import { studioExtensions, FRAGMENT } from '../src/schema.js'

const here = path.dirname(fileURLToPath(import.meta.url))
const schema = getSchema(studioExtensions())

const t = (text, marks) => (marks ? { type: 'text', text, marks } : { type: 'text', text })
const p = (bid, ...content) => ({ type: 'paragraph', attrs: { bid }, content })
const heading = (bid, text) => ({ type: 'sectionHeading', attrs: { bid }, content: [t(text)] })
const section = (uid, kind, head, ...blocks) => ({ type: 'policySection', attrs: { uid, kind }, content: [head, ...blocks] })
const li = (bid, ...content) => ({ type: 'listItem', attrs: { bid }, content })
const doc = (...sections) => ({ type: 'doc', content: sections })
const sugg = (type, id, extra) => ({ type, attrs: Object.assign({ id, authorId: 'u:alice', authorKind: 'human', authorName: 'Alice', proposalId: null, createdAt: '2026-09-28T10:00:00Z' }, extra || {}) })

const cases = {
  marks: doc(section('s-1', 'purpose', heading('b-h1', 'Purpose'),
    p('b-p1', t('Plain, '), t('bold', [{ type: 'bold' }]), t(', '), t('italic', [{ type: 'italic' }]), t(', '),
      t('code', [{ type: 'code' }]), t(' and a '),
      t('link', [{ type: 'link', attrs: { href: 'https://example.com/a?b=1', target: null, rel: 'noopener noreferrer', class: null, title: null } }]),
      { type: 'hardBreak' }, t('after a break.')),
    p('b-p2', t('all at once', [{ type: 'bold' }, { type: 'italic' }, { type: 'link', attrs: { href: 'mailto:ciso@example.com' } }])))),
  lists: doc(section('s-2', 'statements', heading('b-h2', 'Policy statements'),
    { type: 'bulletList', content: [
      li('b-l1', p('b-l1p', t('Access must be approved.'))),
      li('b-l2', p('b-l2p', t('Nested:')), { type: 'orderedList', attrs: { start: 1, type: null }, content: [
        li('b-l3', p('b-l3p', t('first'))), li('b-l4', p('b-l4p', t('second')))] })] },
    { type: 'orderedList', attrs: { start: 3, type: null }, content: [li('b-l5', p('b-l5p', t('starts at three')))] })),
  table: doc(section('s-3', 'roles', heading('b-h3', 'Roles'),
    { type: 'table', content: [
      { type: 'tableRow', content: [
        { type: 'tableHeader', attrs: { bid: 'b-th1', colspan: 1, rowspan: 1, colwidth: null }, content: [p('b-th1p', t('Role'))] },
        { type: 'tableHeader', attrs: { bid: 'b-th2', colspan: 1, rowspan: 1, colwidth: null }, content: [p('b-th2p', t('Responsibility'))] }] },
      { type: 'tableRow', content: [
        { type: 'tableCell', attrs: { bid: 'b-td1', colspan: 1, rowspan: 1, colwidth: null }, content: [p('b-td1p', t('CISO', [{ type: 'bold' }]))] },
        { type: 'tableCell', attrs: { bid: 'b-td2', colspan: 1, rowspan: 1, colwidth: null }, content: [p('b-td2p', t('Owns the policy.'))] }] }] })),
  blocks: doc(section('s-4', 'compliance', heading('b-h4', 'Compliance'),
    { type: 'heading', attrs: { bid: 'b-hh', level: 2 }, content: [t('Sub-heading')] },
    { type: 'heading', attrs: { bid: 'b-hhh', level: 3 }, content: [t('Sub-sub-heading')] },
    { type: 'blockquote', attrs: { bid: 'b-q' }, content: [p('b-qp', t('Quoted.'))] },
    { type: 'callout', attrs: { bid: 'b-c', kind: 'important' }, content: [p('b-cp', t('Read this.'))] })),
  tokens: doc(section('s-5', 'scope', heading('b-h5', 'Scope'),
    p('b-p5', t('This policy applies to '), { type: 'factToken', attrs: { key: 'legal_entity_name' } },
      t(' and satisfies ', [{ type: 'italic' }]), { type: 'controlRef', attrs: { controlId: 'AC-1' } }, t('.')))),
  suggestions: doc(section('s-6', 'statements', heading('b-h6', 'Statements'),
    p('b-p6', t('Staff '), t('will', [sugg('deletion', 'c0ffee00-0000-4000-8000-000000000001')]),
      t('must', [sugg('insertion', 'c0ffee00-0000-4000-8000-000000000002')]),
      t(' review ', [sugg('insertion', 'c0ffee00-0000-4000-8000-000000000003', { authorKind: 'ai', authorId: 'ai:claude-opus-5', authorName: 'AI · claude-opus-5', proposalId: 'prop-1' })]),
      t('access quarterly.')),
    p('b-p7', t('two modifications', [
      sugg('modification', 'c0ffee00-0000-4000-8000-000000000004', { type: 'attr', attrName: 'href', previousValue: 'a', newValue: 'b' }),
      sugg('modification', 'c0ffee00-0000-4000-8000-000000000005', { type: 'attr', attrName: 'href', previousValue: 'b', newValue: 'c' })])))),
  unicode: doc(section('s-7', 'other', heading('b-h7', 'Ünïcödé — “quotes”'),
    p('b-p8', t('emoji 🔐 then '), t('CJK 情報セキュリティ', [{ type: 'bold' }]), t(' é combining')))),
  // Block-level suggestions are node marks; they only survive Yjs with the
  // y-tiptap node-marks patch (patches/apply-y-tiptap-node-marks.mjs).
  blockSuggestions: doc(section('s-9', 'statements', heading('b-h9', 'Block suggestions'),
    Object.assign(p('b-p10', t('A whole new paragraph.', [sugg('insertion', 'c0ffee00-0000-4000-8000-000000000006')])), { marks: [sugg('insertion', 'c0ffee00-0000-4000-8000-000000000006')] }),
    { type: 'callout', attrs: { bid: 'b-c2', kind: 'note' }, marks: [sugg('deletion', 'c0ffee00-0000-4000-8000-000000000007')], content: [p('b-c2p', t('Going away.'))] },
    { type: 'heading', attrs: { bid: 'b-hm', level: 3 }, marks: [sugg('modification', 'c0ffee00-0000-4000-8000-000000000008', { type: 'attr', attrName: 'level', previousValue: 2, newValue: 3 })], content: [t('Level changed')] })),
  // What an empty room looks like after Tiptap initialises the required
  // content: the seeding spike checks the server never lets this happen twice.
  minimal: doc(section('s-8', 'other', { type: 'sectionHeading', attrs: { bid: 'b-h8' } }, p('b-p9'))),
}

function encode(json) {
  const ydoc = new Y.Doc()
  ydoc.clientID = 1 // deterministic output
  const frag = ydoc.getXmlFragment(FRAGMENT)
  prosemirrorJSONToYXmlFragment(schema, json, frag)
  return ydoc
}

function readBack(ydoc) {
  return yXmlFragmentToProseMirrorRootNode(ydoc.getXmlFragment(FRAGMENT), schema).toJSON()
}

function schemaSnapshot() {
  const nodes = []
  schema.spec.nodes.forEach((name, spec) => {
    const type = schema.nodes[name]
    const attrs = {}
    for (const [k, a] of Object.entries(type.attrs)) attrs[k] = { default: a.hasDefault ? a.default : '__required__' }
    nodes.push({ name, content: spec.content || '', group: spec.group || '', inline: !!spec.inline, atom: !!spec.atom, marks: spec.marks === undefined ? null : spec.marks, attrs })
  })
  const marks = []
  schema.spec.marks.forEach((name, spec) => {
    const type = schema.marks[name]
    const attrs = {}
    for (const [k, a] of Object.entries(type.attrs)) attrs[k] = { default: a.hasDefault ? a.default : '__required__' }
    marks.push({ name, rank: type.rank, excludes: spec.excludes === undefined ? null : spec.excludes, inclusive: spec.inclusive === undefined ? null : spec.inclusive, overlapping: !type.excludes(type), attrs })
  })
  return { fragment: FRAGMENT, nodes, marks }
}

function gen() {
  const out = path.join(here, 'cases')
  fs.mkdirSync(out, { recursive: true })
  for (const [name, json] of Object.entries(cases)) {
    schema.nodeFromJSON(json).check()
    const ydoc = encode(json)
    const update = Y.encodeStateAsUpdate(ydoc)
    const expected = readBack(ydoc)
    fs.writeFileSync(path.join(out, name + '.json'), JSON.stringify({ name, input: json, update: Buffer.from(update).toString('base64'), expected }, null, 2) + '\n')
    console.log('fixture', name, update.length, 'bytes')
  }
  fs.writeFileSync(path.join(here, '..', 'schema.snapshot.json'), JSON.stringify(schemaSnapshot(), null, 2) + '\n')
  console.log('schema snapshot written')
}

function read(file) {
  const { update } = JSON.parse(fs.readFileSync(file, 'utf8'))
  const ydoc = new Y.Doc()
  Y.applyUpdate(ydoc, Buffer.from(update, 'base64'))
  const node = yXmlFragmentToProseMirrorRootNode(ydoc.getXmlFragment(FRAGMENT), schema)
  node.check()
  process.stdout.write(JSON.stringify(node.toJSON()) + '\n')
}

function verify() {
  const dir = path.join(here, 'go-seeded')
  const verified = {}
  for (const file of fs.readdirSync(dir).filter((f) => f.endsWith('.json') && f !== 'js-verified.json').sort()) {
    const name = file.replace(/\.json$/, '')
    const { update } = JSON.parse(fs.readFileSync(path.join(dir, file), 'utf8'))
    const ydoc = new Y.Doc()
    Y.applyUpdate(ydoc, Buffer.from(update, 'base64'))
    const node = yXmlFragmentToProseMirrorRootNode(ydoc.getXmlFragment(FRAGMENT), schema)
    node.check()
    const want = JSON.parse(fs.readFileSync(path.join(here, 'cases', name + '.json'), 'utf8')).expected
    if (!sameDoc(node.toJSON(), want)) {
      console.error('JS renders the Go seed of ' + name + ' differently:\n' + JSON.stringify(node.toJSON()))
      process.exit(1)
    }
    verified[name] = crypto.createHash('sha256').update(Buffer.from(update, 'base64')).digest('hex')
  }
  fs.writeFileSync(path.join(dir, 'js-verified.json'), JSON.stringify(verified, null, 2) + '\n')
  console.log('verified', Object.keys(verified).length, 'Go-seeded documents')
}

// Two marks of one type are ordered by rank only, so their relative order is
// not meaningful (the Go side cannot recover the order Yjs stored them in);
// compare with ties ordered by JSON, as the Go conformance test does.
function canonical(node) {
  if (Array.isArray(node.marks) && node.marks.length > 1) {
    const rank = (m) => schema.marks[m.type].rank
    node.marks.sort((a, b) => rank(a) - rank(b) || (JSON.stringify(a) < JSON.stringify(b) ? -1 : 1))
  }
  for (const c of node.content || []) canonical(c)
  return node
}

function sameDoc(a, b) {
  return JSON.stringify(canonical(structuredClone(a))) === JSON.stringify(canonical(structuredClone(b)))
}

const [cmd, arg] = process.argv.slice(2)
if (cmd === 'gen') gen()
else if (cmd === 'read') read(arg)
else if (cmd === 'verify') verify()
else { console.error('usage: gen | verify | read <file>'); process.exit(2) }
