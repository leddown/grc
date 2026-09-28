// Patch @tiptap/y-tiptap so ProseMirror node marks survive Yjs.
//
// y-tiptap (and upstream y-prosemirror 1.3.7) writes an element's attrs to the
// Y.XmlElement but not its marks, so a block-level suggestion -- a whole
// paragraph inserted or deleted, a heading level changed -- is dropped on sync
// and the author's editor diverges from everyone else's. The patch stores the
// marks as one reserved attribute, NODE_MARKS_ATTR, holding a JSON string (a
// string so the binding's === attribute comparisons keep working).
//
// Every replacement must match exactly once; a changed upstream file fails the
// build instead of shipping a half-patched binding.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.join(here, '..', 'node_modules', '@tiptap', 'y-tiptap')
const pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'))
if (pkg.version !== '3.0.9') {
  console.error('y-tiptap is ' + pkg.version + '; the node-marks patch is written against 3.0.9. Re-check it.')
  process.exit(1)
}

const MARKER = '/* grc: node-marks patch */'
const helpers = MARKER + `
const NODE_MARKS_ATTR = '__marks';
const pAttrsOf = (pnode) => {
  const marks = pnode.marks && pnode.marks.length ? JSON.stringify(pnode.marks.map((m) => m.toJSON())) : null;
  return Object.assign({}, pnode.attrs, { [NODE_MARKS_ATTR]: marks })
};
const splitYAttrs = (attrs, schema) => {
  const raw = attrs[NODE_MARKS_ATTR];
  delete attrs[NODE_MARKS_ATTR];
  if (typeof raw !== 'string' || raw === '') return []
  return JSON.parse(raw).map((m) => schema.markFromJSON(m))
};
`

const edits = [
  // Reading: Y element -> ProseMirror node, with its marks.
  ['    const node = schema.node(el.nodeName, attrs, children);',
   '    const nodeMarks = splitYAttrs(attrs, schema);\n    const node = schema.node(el.nodeName, attrs, children, nodeMarks);'],
  // Writing a new element.
  ['const createTypeFromElementNode = (node, meta) => {\n  const type = new Y.XmlElement(node.type.name);\n  for (const key in node.attrs) {\n    const val = node.attrs[key];',
   'const createTypeFromElementNode = (node, meta) => {\n  const type = new Y.XmlElement(node.type.name);\n  const nodeAttrs = pAttrsOf(node);\n  for (const key in nodeAttrs) {\n    const val = nodeAttrs[key];'],
  // Comparing an element with a node must see a mark change as a change.
  ['      equalAttrs(ytype.getAttributes(), pnode.attrs) &&',
   '      equalAttrs(ytype.getAttributes(), pAttrsOf(pnode)) &&'],
  // Updating an existing element.
  ['    const yDomAttrs = yDomFragment.getAttributes();\n    const pAttrs = pNode.attrs;',
   '    const yDomAttrs = yDomFragment.getAttributes();\n    const pAttrs = pAttrsOf(pNode);'],
]

// Only the ESM build is bundled (esbuild and Node both resolve the import
// condition); the CJS build is never loaded.
for (const file of ['dist/y-tiptap.js']) {
  const target = path.join(root, file)
  let src = fs.readFileSync(target, 'utf8')
  if (src.includes(MARKER)) { console.log('already patched', file); continue }
  for (const [from, to] of edits) {
    const count = src.split(from).length - 1
    if (count !== 1) {
      console.error(file + ': expected exactly one match, found ' + count + ' for:\n' + from)
      process.exit(1)
    }
    src = src.replace(from, to)
  }
  // Helpers go after the last import/require so they can use nothing but plain JS.
  const anchor = file.endsWith('.cjs') ? "'use strict';\n" : "import * as Y from 'yjs';\n"
  if (!src.includes(anchor)) { console.error(file + ': no anchor for helpers'); process.exit(1) }
  src = src.replace(anchor, anchor + helpers)
  fs.writeFileSync(target, src)
  console.log('patched', file)
}
