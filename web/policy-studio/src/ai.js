// AI proposals in the editor: finding where each validated edit goes in the
// document as it is now, previewing it privately, and placing it as a
// suggestion attributed to the AI. The server validated every edit against
// the document when the proposal was made; this re-checks each one against
// the live text, because people kept typing while the model was thinking.
import { Extension } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'
import { transformToSuggestionTransaction, suggestChangesKey } from '@handlewithcare/prosemirror-suggest-changes'

// blockText renders a block's baseline text exactly as the server does
// (internal/policyai/text.go): pending insertions left out, a hard break as a
// newline, tokens in their Markdown form. The two must agree, or no quote
// would ever be found.
export function blockText(node) {
  let text = ''
  node.forEach((child) => {
    if (child.marks.some((m) => m.type.name === 'insertion')) return
    text += childText(child)
  })
  return text
}

function childText(child) {
  if (child.isText) return child.text.replace(/​/g, '')
  switch (child.type.name) {
    case 'hardBreak': return '\n'
    case 'factToken': return '{{fact:' + child.attrs.key + '}}'
    case 'controlRef': return '[[control:' + child.attrs.controlId + ']]'
  }
  return ''
}

export function findBlock(doc, bid) {
  let found = null
  doc.descendants((node, pos) => {
    if (found) return false
    if (node.attrs && node.attrs.bid === bid && node.isTextblock) {
      found = { node, pos }
      return false
    }
    return true
  })
  return found
}

// textRange maps a range of a block's baseline text to document positions.
// It is null when either end falls inside a token or the range touches a
// pending suggestion: the edit would change text someone else has proposed
// changing.
function textRange(block, start, end) {
  let offset = 0
  let from = null
  let to = null
  let touchesPending = false
  let pos = block.pos + 1
  block.node.forEach((child) => {
    const size = child.nodeSize
    const inserted = child.marks.some((m) => m.type.name === 'insertion')
    if (inserted) {
      if (offset > start && offset < end) touchesPending = true
      pos += size
      return
    }
    const text = childText(child)
    const len = text.length
    const pending = child.marks.some((m) => m.type.name === 'deletion' || m.type.name === 'modification')
    if (pending && offset < end && start < offset + len) touchesPending = true
    if (from === null && start >= offset && start <= offset + len) {
      if (child.isText) from = pos + (start - offset)
      else if (start === offset) from = pos
      else if (start === offset + len) from = pos + size
    }
    if (to === null && end >= offset && end <= offset + len && (from !== null || end === start)) {
      if (child.isText) to = pos + (end - offset)
      else if (end === offset + len) to = pos + size
      else if (end === offset) to = pos
    }
    offset += len
    pos += size
  })
  if (from === null && start === offset) from = pos
  if (to === null && end === offset) to = pos
  if (from === null || to === null || touchesPending) return null
  return { from, to }
}

// locate finds where an edit goes now. The result carries a status the
// server records: ok, stale (the quoted text is no longer there) or conflict
// (it is there, but ambiguous or under someone else's pending suggestion).
// An edit is never moved to other text that happens to look similar.
export function locate(doc, edit) {
  const block = findBlock(doc, edit.block_id)
  if (!block) return { status: 'stale', reason: 'its paragraph was removed' }
  if (edit.op === 'insert_after') return { status: 'ok', block, at: block.pos + block.node.nodeSize }
  if (edit.op === 'insert_before') return { status: 'ok', block, at: block.pos }
  const text = blockText(block.node)
  if (edit.whole_block || edit.op === 'comment' && !edit.quote) {
    if (text.trim() !== (edit.quote || edit.block_text).trim()) return { status: 'stale', reason: 'the paragraph changed' }
    const range = textRange(block, 0, text.length)
    if (!range) return { status: 'conflict', reason: 'part of it is a pending suggestion' }
    return { status: 'ok', block, whole: true, from: block.pos, to: block.pos + block.node.nodeSize, textFrom: range.from, textTo: range.to }
  }
  const first = text.indexOf(edit.quote)
  if (first < 0) return { status: 'stale', reason: 'the quoted text changed' }
  if (text.indexOf(edit.quote, first + 1) >= 0) return { status: 'conflict', reason: 'the quoted text now appears twice' }
  const range = textRange(block, first, first + edit.quote.length)
  if (!range) return { status: 'conflict', reason: 'it overlaps a pending suggestion' }
  return { status: 'ok', block, from: range.from, to: range.to, textFrom: range.from, textTo: range.to }
}

function fragmentNodes(schema, edit) {
  return (edit.fragment || []).map((json) => schema.nodeFromJSON(json))
}

// editTransaction builds the change an edit makes, as a plain transaction.
// It returns null when the change does not fit the schema where it now lands.
export function editTransaction(state, edit, where) {
  const tr = state.tr
  try {
    switch (edit.op) {
      case 'replace':
        if (edit.inline) tr.replaceWith(where.textFrom, where.textTo, fragmentNodes(state.schema, edit))
        else tr.replaceWith(where.from, where.to, fragmentNodes(state.schema, edit))
        break
      case 'delete':
        if (where.whole) tr.delete(where.from, where.to)
        else tr.delete(where.textFrom, where.textTo)
        break
      case 'insert_after':
      case 'insert_before':
        tr.insert(where.at, fragmentNodes(state.schema, edit))
        break
      default:
        return null
    }
    state.schema.nodeFromJSON(tr.doc.toJSON()).check()
  } catch (_) {
    return null
  }
  return tr
}

// suggestionTransaction turns an edit's change into suggestion marks whose id
// is the edit's suid, so the decision on the suggestion is the decision on
// the edit.
export function suggestionTransaction(state, edit, where) {
  const plain = editTransaction(state, edit, where)
  if (!plain) return null
  const tr = transformToSuggestionTransaction(plain, state, () => edit.suid)
  return tr.setMeta(suggestChangesKey, { skip: true })
}

// ---- private preview ----

export const previewKey = new PluginKey('aiPreview')

// fragmentText is how an edit's new text reads: inline content run together,
// blocks one per line, tokens in their Markdown form.
export function fragmentText(edit) {
  const text = (n) => {
    if (n.type === 'text') return n.text || ''
    if (n.type === 'hardBreak') return ' '
    if (n.type === 'factToken') return '{{fact:' + ((n.attrs && n.attrs.key) || '') + '}}'
    if (n.type === 'controlRef') return '[[control:' + ((n.attrs && n.attrs.controlId) || '') + ']]'
    return (n.content || []).map(text).join(n.type === 'bulletList' || n.type === 'orderedList' ? ' / ' : '')
  }
  const nodes = edit.fragment || []
  return nodes.map(text).join(edit.inline ? '' : ' / ')
}

function previewText(_schema, edit) {
  return fragmentText(edit) || edit.replacement_markdown || ''
}

// A preview is decorations in this editor only: nothing of it is in the
// shared document, and it follows the text as other people type.
export function aiPreview() {
  return Extension.create({
    name: 'aiPreview',
    addProseMirrorPlugins() {
      return [new Plugin({
        key: previewKey,
        state: {
          init: () => ({ items: [] }),
          apply: (tr, value) => {
            const meta = tr.getMeta(previewKey)
            if (meta) return meta
            if (!tr.docChanged || !value.items.length) return value
            return {
              items: value.items.map((it) => Object.assign({}, it, {
                from: tr.mapping.map(it.from, 1), to: tr.mapping.map(it.to, -1), at: tr.mapping.map(it.at, 1),
              })),
            }
          },
        },
        props: {
          decorations(state) {
            const { items } = previewKey.getState(state)
            if (!items.length) return null
            const decos = []
            for (const it of items) {
              const widget = (text, block) => () => {
                const el = document.createElement(block ? 'div' : 'span')
                el.className = 'ps-ai-preview-ins' + (block ? ' ps-ai-preview-block' : '')
                el.textContent = text
                el.title = 'Proposed by the AI: ' + it.rationale
                return el
              }
              if (it.op === 'comment') {
                if (it.to > it.from) decos.push(Decoration.inline(it.from, it.to, { class: 'ps-ai-preview-comment', title: 'AI comment: ' + it.rationale }))
                continue
              }
              if ((it.op === 'replace' || it.op === 'delete') && it.to > it.from) {
                decos.push(Decoration.inline(it.from, it.to, { class: 'ps-ai-preview-del', title: 'The AI proposes changing this: ' + it.rationale }))
              }
              if (it.op === 'replace') decos.push(Decoration.widget(it.to, widget(it.text, !it.inline), { side: 1, key: 'ai-' + it.suid }))
              if (it.op === 'insert_after' || it.op === 'insert_before') decos.push(Decoration.widget(it.at, widget(it.text, true), { side: it.op === 'insert_after' ? 1 : -1, key: 'ai-' + it.suid }))
            }
            return DecorationSet.create(state.doc, decos)
          },
        },
      })]
    },
  })
}

// previewItem is what the preview keeps for one located edit.
export function previewItem(state, edit, where) {
  return {
    suid: edit.suid, op: edit.op, inline: edit.inline, rationale: edit.rationale || '',
    from: where.textFrom !== undefined ? where.textFrom : (where.from || 0),
    to: where.textTo !== undefined ? where.textTo : (where.to || 0),
    at: where.at || 0, text: previewText(state.schema, edit),
  }
}
