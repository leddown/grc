# Policy Studio

Design, decision record and operator guide for the Policy Studio: WYSIWYG
policy authoring on top of `internal/policydocs`, live co-editing with the
customer, tracked suggestions, and an Ask AI agent that proposes changes as
suggestions a person accepts or rejects.

**Status: Phases 1–5 built.**

- **1a:** the collaborative editor, persistence, projection, section integrity,
  control chips, live lint, and migration from the section editor.
- **1b:** client profiles and facts, the template registry with the ICT and
  Information Security Policy, rich content in every renderer, the hashed
  approval snapshot, and paper view.
- **2:** the review layer: presence, suggest mode, the Suggestions panel with
  keyboard review, the decision audit, comments anchored on the live text,
  provenance, the blocking `pending_suggestions` rule, and suggesting in
  review.
- **3:** AI proposals: the `internal/policyai` engine, inline actions, the
  Ask AI dock on Studio pages, private preview and shared placement as
  suggestions attributed to the AI, `ai_policy`, and the Policy Studio agent.
- **4:** guests (share links, guest sessions, the chromeless guest Studio with
  an enforced CSP, the Sharing panel) and workshop mode (larger type, focus,
  present and follow, the customer-safe view).
- **5:** the extras the owner chose: compare against any approved version
  (with a redline PDF), Word export through Pandoc, starting from a library
  document, and AI answers streamed as they are written.

The owner accepted the recommendations for Q1–Q10 on 2026-09-28
([§11](#11-open-questions-for-the-owner)). For Phase 5 the owner chose four of
the brief's extras on 2026-09-29; server-side suggestion injection, OSCAL hooks
and the Yjs v14 attribution evaluation are deferred. [§13](#13-operator-guide)
is the operator guide. The Phase 0 spike code lives on
the throwaway branch `spike/policy-studio`.

It follows the `*_FRAMEWORK.md` convention. [`POLICY_MODULE_FRAMEWORK.md`](POLICY_MODULE_FRAMEWORK.md)
is the research this builds on: the Studio is how that plan's phase 4 (AI
drafting with client-fact tokens, citation checks and provenance) gets
delivered.

---

## 1. Goals

The acceptance demo, in one paragraph. A consultant starts an "ICT and
Information Security Policy" from a template for a client. The draft opens
complete: every mandatory section, the known client facts filled in, the
missing ones shown as unresolved chips, and proposed control mappings as
draft claims. The client's CISO joins through an 8-hour invite, and both see
each other's cursors and edits. The consultant asks the Ask AI dock to make
§3 testable and add segregation of duties. The proposal previews privately,
is then shared, and lands as tracked suggestions attributed to the AI, with a
rationale and checked citations. The two of them decide the suggestions and
fill in the missing facts. The document is approved by a different user,
becomes read-only everywhere, and renders to the branded Typst PDF with
lists, tables, facts and "Satisfies:" notes intact. A provenance view shows
who wrote what: template, consultant, customer or AI, and who accepted it.

Non-goals are in [§12](#12-out-of-scope).

---

## 2. What exists today (verified against the code, 2026-09-28)

The brief's §3 was checked point by point. Where the code differs, the code
wins, and each difference is listed here.

| Brief says | Code says | Consequence |
|---|---|---|
| Sections carry `body_markdown` | The column is `policy_sections.body` (`Section.Body`, JSON `body`) | The projection writes `body`. No rename: renaming a column that `dbsync`, `knowledge` and the export contract read would change three consumers for no gain |
| Template and client pickers are "fed by the CRM" (`crm_clients`, `internal/crm`) | The CRM moved to Wintermute on 2026-08-11 (`/api/v1/crm/clients` there). This app stores `client_id` + `client_name` on the document and has no client table. README.md still lists `internal/crm` (stale) | `internal/clientprofile` cannot key to `crm_clients`. See open question Q2 |
| Approval "locks sections afterwards" | Locking is `requireEditable`: every non-draft status refuses section edits. Approval itself only snapshots | Same effect. The Studio adds the same rule at the transport (read-only peers) |
| — | `/ai-chat/ask` accepts a client-supplied `system_prompt` and is reachable by any user granted `/ai-chat`, not only admins | On Studio pages the server must ignore the client's system prompt and must only attach a proposal for admins (invariant 10, §6.10 of the brief) |
| — | `aiprovider.Response` has no stop reason: a Claude answer cut off at `max_tokens` comes back as ordinary text | Phase 3 adds `StopReason` to `Response`. Without it the "never parse a max_tokens answer" rule cannot be enforced |
| — | The theme layer injects an inline `<script id="global-text-lift-init">` and inline `<style>` blocks into **every** HTML response, including chromeless pages | A nonce-based script-src on the guest page would block the app's own injected script. See [§8](#8-threat-model) (CSP) |
| — | `DOCUMENT_TEMPLATES.md` says "The LaTeX policy template does not read the JSON", while `latexgen.go` generates the `.tex` from the payload | Stale sentence; corrected in Phase 1 when that document gains the blocks contract |
| — | `POLICY_MODULE_FRAMEWORK.md` §2.4 still says NFR enrichment "ingests uploaded and fetched security documents", which contradicts Agents.md's no-ingestion rule | Stale; flagged, not in this feature's scope |

Confirmed as the brief states:

- **Pages and platform.** Pages are Go string literals with no template
  engine and no JS build step. The module is cgo-free. There is no WebSocket,
  SSE or Content-Security-Policy anywhere.
- **Policy module.** Lint blocks approval on missing mandatory sections
  (`requiredKinds` in `lint.go`) and on `[[UNRESOLVED: …]]`. Approval requires
  `in_review`, an approver, an effective date and an owner role, and snapshots
  `RenderMarkdown` into `policy_versions`. Separation of duties is on under
  multi-user auth. Mapping is draft-only.
- **Rendering.** `section-block` in `templates/typst/lib.typ` receives the
  body as a plain string, so any formatting is flattened today.
  `TemplateExport` is the payload contract.
- **Access.** A `/policies` grant covers `/policies/:id/studio` (`grantCovers`
  in `access_control.go`, since it is not a separately grantable page).
- **Backup.** `dbsync.SnapshotVersion` is 9.
- **Knowledge and grounding.** The corpus cache is 45 s, and exercises are
  already read fresh. BM25 is `nfrenrich.NewBM25Retriever`, already reused by
  `regcoverage` and `knowledge`. Regulation Coverage normalises quotes in
  `normalizeForMatch`.
- **Settings.** The crisis pair `ai.crisis.agent` / `ai.crisis.send_exercise`
  exists and is the model for `ai.policy.*`.
- **Configured model.** No `ai.claude.model` is stored in this checkout's
  database, so the default `claude-opus-5` applies.

---

## 3. Decisions, with evidence and pins

Re-verified on 2026-09-28 against the npm registry, the Go module proxy, the
package sources and the Claude API reference. "Go" means the spike
supports the decision; the conditions are part of it.

### 3.1 Pins

**Go modules to add** (licences read from each module's LICENSE file):

| Module | Version | Licence | Why |
|---|---|---|---|
| `github.com/reearth/ygo` | v1.50.0 | MIT | In-process Yjs server: rooms, persistence, read-only peers, caps |
| `golang.org/x/time` | v0.15.0 (via ygo; MVS picked the newer version already in the graph) | BSD-3-Clause | Per-peer rate limiter inside ygo; the only new compiled transitive dependency |
| `github.com/yuin/goldmark` | v1.8.6 (was v1.4.13, indirect, in the graph already) | MIT, no dependencies | Parses legacy section Markdown (and, in Phase 3, AI `replacement_markdown`) into the schema. The repo had no Markdown parser |

`github.com/gorilla/websocket` v1.5.3 (BSD-2-Clause) is already in grc's
graph. ygo's `go.mod` also lists Redis-cluster and test modules that are
never compiled here. They were checked because the rule covers the whole
module graph: go-redis v9.18.0 (BSD-2), miniredis v2.38.0 (MIT), gopher-lua
v1.1.1 (MIT), testify v1.10.0 (MIT), go.uber.org/atomic v1.11.0 (MIT),
go-rendezvous (MIT), xxhash v2.3.0 (MIT), golang.org/x/exp (BSD-3).

**npm packages** (exact pins, `npm ci --ignore-scripts`). The bundle is 45
packages, every one MIT, checked from the esbuild metafile. `npm audit
--omit=dev` finds 0 vulnerabilities.

| Package | Version | Licence |
|---|---|---|
| `@tiptap/core`, `@tiptap/pm`, `@tiptap/starter-kit`, `@tiptap/extension-{collaboration,collaboration-caret,unique-id,table,list,blockquote}` | 3.31.3 | MIT |
| `@tiptap/y-tiptap` | 3.0.9 (+ local patch, §3.3) | MIT |
| `yjs` | 13.6.33 | MIT |
| `y-websocket` | 3.1.0 | MIT |
| `y-protocols` | 1.0.7 | MIT |
| `@handlewithcare/prosemirror-suggest-changes` | 0.1.8 | MIT |
| `esbuild` (build only) | 0.28.2 | MIT |

**Node:** v24.21.0 LTS ("Krypton"). v22 is maintenance LTS. This machine has
only v18.20.8, which is past end of life. See Q4.

### 3.2 Decision record, go/no-go

| # | Decision (brief §4) | Verdict | Evidence / condition |
|---|---|---|---|
| D1 | Tiptap v3 core + MIT extensions, vanilla JS, bundled and embedded | **Go** | Spikes A–D run the real editor in headless Chrome. The bundle is 564 KB minified, 182 KB gzipped, before any Studio UI. Condition: import extensions individually rather than through StarterKit, which bundles the disabled ones |
| D2 | `prosemirror-suggest-changes` for tracked changes | **Go, with four conditions** | Spike D. (1) Pass a UUID `generateId`: the default is max+1 across the document and would collide between concurrent clients. (2) Authorship attributes are added by an `appendTransaction` plugin; the library sets only `id`. (3) Every container node must allow the suggestion marks. (4) The binding patch in §3.3 |
| D3 | Yjs 13.6.x + `@tiptap/y-tiptap` + y-websocket | **Go, conditional on §3.3** | ygo speaks the native y-websocket URL-room protocol; Hocuspocus is not needed. Condition: `disableBc: true` (§9, spike A) |
| D4 | `reearth/ygo` in-process | **Go** | `Authorize` → `ConnectionConfig{ReadOnly}`, `Apply`, `CloseRoom(name, force)`, `OnLoadDocument`, `CompactableAdapter`, `PersistCoalesceWindow/MaxWait`, `CompactEvery`, and every cap the brief names all exist in v1.50.0 under those names. `Deln0r/ygo` was not needed |
| D5 | Provider-agnostic EditProposal; Claude structured outputs; Wintermute JSON with one repair turn | **Go (stub-verified)** | Spike E. The pinned SDK v1.46.0 has `OutputConfigParam.Format` (a `JSONOutputFormatParam` with a raw `map[string]any` schema), so no raw request option is needed, and no beta header is sent. Appendix A has 0 optional properties and 1 union (limits 24 / 16) and no length or numeric constraints. Not done: a live call against the real API (Q5). The brief says changing `output_config.format` invalidates the prompt cache; the reference checked does not say so either way, and the design keeps one schema per dock session regardless |
| D6 | AI edits land as client-placed suggestions attributed to the AI | **Go** | Suggestion marks carrying `authorKind`, `authorName` and `proposalId` propagate through the server and the other peer with their attributes intact (spike D). Placing proposal fragments is Phase 3 work |
| D7 | Suggester guest role | **No-go for v1** | ygo exposes no per-connection hook that sees an update before it is applied (`Authorize` only decides read-only or read-write for the whole connection). Enforcing "every change is a suggestion by this guest" would need a fork. Ship viewer, commenter and editor |

Rejected alternatives stay rejected for the reasons in the brief (paid or
AGPL/GPL, React-only, or a Node sidecar). Nothing found here reopens them.

### 3.3 The binding does not carry node marks (the main Phase 0 finding)

`@tiptap/y-tiptap` 3.0.9 writes a ProseMirror element's `attrs` to the
`Y.XmlElement` but **not its marks**. Upstream `y-prosemirror` 1.3.7 (the
latest) does the same. suggest-changes records several suggestions as
**node marks**:

- inserting a whole paragraph, list item or table row (what an AI
  `insert_after` / `insert_before` does)
- deleting a whole block (select a callout, press Delete)
- changing a heading level or a callout kind (`modification`)

Measured in `fixtures/nodemarks2.mjs`, unpatched:

| Operation | Suggestion mark after Yjs | Accept agrees | Reject restores |
|---|---|---|---|
| Split / join paragraphs | kept (text-level; uses zero-width spaces) | yes | yes |
| Insert whole paragraph | **lost** (becomes normal text) | yes | **no** |
| Delete whole block | **lost** (the deletion vanishes) | **no** | yes |
| Change heading level / callout kind | **lost** (applied directly) | yes | **no** |

Worse, the author's own editor keeps the mark locally while every other peer
has none, so the documents diverge until the node is re-rendered.

**The fix used in the spike.** `patches/apply-y-tiptap-node-marks.mjs` stores
a node's marks in one reserved Y attribute, `__marks`, as a JSON string (a
string so the binding's `===` attribute comparisons keep working). It
changes four places: create element, compare element, update attributes, and
read element. Each replacement must match exactly once, and the script
refuses any version other than 3.0.9. With the patch, every row above is
"kept / yes / yes", locally and across two live browsers and the server
(spike D). The Go projection and seeder read and write the same attribute.

This is a small fork-in-place of a dependency, so it is a decision for the
owner (Q3): carry the patch, vendor a fork, or send it upstream first.
Without it, whole-block suggestions have to be refused in suggest mode.

---

## 4. Target architecture

As in the brief's §5, with the spike's findings folded in:

```
Browser (Studio shell = Go string literal; bundle = embedded, hashed IIFE)
  Tiptap ─ y-tiptap (patched) ─ Y.Doc ─ y-websocket (disableBc) ──► /collab/policies/:id
    schema: policySection · sectionHeading · factToken · controlRef       ygo (in-process)
    suggestion marks {id=UUID, author*, proposalId, createdAt}             ├ Authorize: session|guest cookie,
    UniqueID bid · carets · attribution plugin · section-integrity filter  │   Origin allowlist, draft?rw:ro
  window.GRCPolicyStudio ◄── global Ask AI dock (GRCAskAIContext)          ├ OnLoadDocument: seed-once (own tx)
                │ JSON APIs                                                ├ persistence → policy_doc_* (both dialects)
                ▼                                                          └ debounced projection → policy_sections
  /policies/:id/studio/* · /ai-chat/ask (page-aware) · /shared/*
  internal/policyai ─► aiprovider.Router ─► Claude (output_config.format) | Wintermute (JSON + 1 repair)
```

**Packages.**

- `internal/policystudio`: page, embedded assets, ygo wiring, the collab
  decision, the persistence adapter, projection, seeding, the schema
  validator, comments, share links and guest sessions.
- `internal/policyai`: context builder, prompts, EditProposal validation,
  anchoring, provenance, decisions.
- `internal/clientprofile`: see Q2.
- `web/policy-studio/`: JS sources, lockfile, build.

The fragment name shared by client and server is the constant `policy`.

**Things the spike showed Phase 1 must do:**

1. **Seed outside ygo's persistence path.** ygo runs `OnLoadDocument`
   *before* it attaches the persistence observer, so an edit made there is
   never stored. Seeding therefore builds the update in a scratch document,
   writes that update and the guard (`policy_documents.seeded_at`) in one
   database transaction, and only then applies it to the room.
2. **Disable BroadcastChannel.** y-websocket otherwise syncs same-browser tabs
   directly, around the server and around its read-only enforcement.
3. **Mount the editor after `sync`**, and seed every block with a `bid`, so
   UniqueID never races to assign ids. Verified: seeded bids survive three
   clients mounting at once.
4. **Strip zero-width spaces from the baseline projection.** suggest-changes
   inserts U+200B when a block is split in suggest mode.
5. **Canonicalise mark order.** ygo returns text attributes as a Go map, so
   when a mark type repeats, the order Yjs stored the repeats in is not
   recoverable. The projection orders ties by JSON, and conformance tests
   compare that way. It carries no meaning, since only `modification` can
   repeat.
6. **Upgrades through the theme middleware work unmodified.** The capture
   writer passes `Hijack` through. The app's post-handler write after a hijack
   produced no log noise in the test. Phase 1 still skips that write for
   upgrade requests, for clarity.

---

## 5. Data model (both dialects; INTEGER booleans; BLOB / bytea)

Appendix C of the brief, finalised:

```
client_profiles       id, name, crm_ref, notes, created_at, updated_at                          (1b)
client_profile_facts  client_id, key, value, value_type, source, updated_by, updated_at (PK)    (1b)
policy_documents      + editor_format TEXT NOT NULL DEFAULT 'markdown'   -- markdown | studio        (1a)
                      + projection_version INTEGER NOT NULL DEFAULT 0                           (1a)
                      + projection_token TEXT NOT NULL DEFAULT ''        -- pairs rows with state   (1a)
                      + client_profile_id, template_id, template_version                       (1b)
                      + ai_policy TEXT NOT NULL DEFAULT 'inherit'        -- inherit|local_only|off (3)
policy_sections       + uid TEXT (UNIQUE; backfilled), content_json TEXT, detached_at TEXT    (1a)
                      + blocks_json TEXT (rendered blocks, facts resolved)                     (1b)
                        (body stays the column name; for Studio documents it holds the projected Markdown)
policy_versions       + snapshot_sha256, content_json                                          (1b)
policy_doc_state      document_id PK, state BLOB, projection_token, updated_at                  (1a)
policy_doc_updates    id, document_id, upd BLOB, created_at                                     (1a)
policy_doc_snapshots  id, document_id, reason, format (yjs|legacy_json), state BLOB,
                      created_by, created_at                                                     (1a)
policy_comment_threads id, document_id, section_uid, anchor_start TEXT, anchor_end TEXT,  -- base64 Yjs relative positions
                      quote, visibility (internal|shared), kind (comment|ai_rationale), suggestion_suid,
                      status, created_by, created_at, updated_at, resolved_by, resolved_at         (2)
policy_comments       id, thread_id, author, author_kind, body, created_at                      (2)
policy_ai_proposals   id, document_id, requested_by, action, scope, instruction, provider, model, served_by,
                      agent, prompt_sha256, context_fingerprint, status, summary, answer_markdown,
                      mappings_json, new_facts_json, input_tokens, output_tokens, created_at       (3)
policy_ai_edits       id, proposal_id, suid UNIQUE, op, block_id, section_uid, quote, fragment_json,
                      rationale, citations_json, validation, placement, decision, decided_by,
                      decided_at                                                                  (3)
policy_share_links    id, document_id, token_sha256 UNIQUE, label, role, allow_ai, max_uses, uses,
                      created_by, created_at, expires_at, revoked_at                               (4)
policy_guest_sessions id, link_id, session_sha256 UNIQUE, display_name, created_at, last_seen_at,
                      expires_at, ended_at, end_reason                                              (4)
policy_studio_audit   id, document_id, actor, actor_kind, event, detail_json, created_at        (2)
client_profile_facts  client_ref, key, value, value_type, source, updated_by, updated_at  (PK client_ref, key)
```

**Changes from the sketch:**

- The seed-once guard is the `policy_doc_state` row itself (`INSERT ... ON
  CONFLICT DO NOTHING`), not a `seeded_at` column.
- `projection_token` is new. Every projection writes one random token to both
  `policy_documents` and `policy_doc_state` in the same transaction. When a
  room loads, a mismatch can only mean the section rows were changed by
  something other than a projection (a `-sync-from`, a restore of an older
  backup). The rows then win: the document is rebuilt from them, the old
  state kept as a `superseded` snapshot. A hash of the rows would have
  false-positived after every structure command.
- The approval snapshot keeps its hash (Appendix D) and its ProseMirror JSON,
  so later rendering can re-typeset the approved content, not just its
  Markdown.
- `client_profile_facts` keys on `client_ref`, pending Q2.
- Update rows carry no `actor`. ygo coalesces updates from several peers into
  one write, so a per-row actor would be a guess. Who-did-what comes from the
  suggestion marks, the decision audit and the Studio audit table.

**`dbsync`:**

- All new tables join the snapshot, parent-first. `SnapshotVersion` is now
  10, and v9 backups restore with the new tables empty.
- They stay out of `-sync-to` / `-sync-from`. The id-keyed Yjs state belongs
  with its document, and the precedent for tree-shaped modules applies.
- Share links and guest sessions are excluded even from snapshots, with a WHY
  comment: they are live credentials.

---

## 6. Routes and authorization

**One decision function per gate**, each with table-driven tests:

- **`collabDecision`** (for `/collab/`). The spike's version is
  `server.authorize`, tested in `server/transport_test.go`: cross-origin,
  look-alike origin, `null` origin, missing Origin and unauthenticated
  upgrades are all refused with 401.
- **`guestDecision`** (for `/shared/`). A route-enumeration test walks the Gin
  route table and asserts that every route under `/collab/` and `/shared/`
  passes through its gate. `/shared/` is **not** added to
  `protectedPrefixes`, which would redirect guests to `/login`.

| Route | Admin | Signed-in, `/policies` grant | Guest editor | Guest commenter / viewer | Unauthenticated |
|---|---|---|---|---|---|
| `GET /policies/:id/studio` | edit | read-only | — | — | → login |
| `GET /policies/:id/studio/state` (ETag) | ✓ | ✓ | via `/shared/api` | via `/shared/api` | 401 |
| `POST /policies/from-template` | ✓ | 403 | — | — | 401 |
| `/policies/:id/studio/sections` (add, delete, reorder, kind) | draft only | 403 | 403 | 403 | 401 |
| `/policies/:id/studio/comments` | all threads; post, reply, resolve (draft or in review) | read internal+shared | shared only | shared only (commenter posts) | 401 |
| `POST /policies/:id/studio/decisions` | draft or in review | 403 | 403 | 403 | 401 |
| `GET /policies/:id/studio/provenance` | ✓ | ✓ | — | — | 401 |
| `POST /policies/:id/studio/ai/proposals`, `…/ai/proposals/:pid/placements` | ✓ (draft or in review) | 403 | proposals only if `allow_ai` (Phase 4); never decisions | 403 | 401 |
| `GET /policies/:id/studio/ai/status`, `…/ai/edits` | ✓ | ✓ | — | — | 401 |
| `POST /ai-chat/ask` from a Studio page | proposal engine | answered as on any page | — | — | 401 |
| `GET /policies/:id/compare` (JSON), `GET /policies/:id/compare.pdf` (redline) | ✓ | ✓ | — | — | 401 |
| `GET /policies/library` (library documents to start from) | ✓ | 403 | — | — | 401 |
| `GET /policies/templates/manage`, `/policies/app-templates` (list, `meta`, `:tid`, `:tid/template.json`; POST `import`, `copy`, `draft`, `:tid/publish`, `retire`, `restore`; PUT, DELETE `:tid`), `GET …/draft/status` | ✓ | 403 | — | — | 401 |
| `/policies/:id/studio/migrate` | ✓ | 403 | — | — | 401 |
| `/policies/:id/share-links` (GET, POST, DELETE `:linkID`), `DELETE /policies/:id/guests/:sessionID` | ✓ (not in local mode; off until `studio.guest_links`) | 403 | — | — | 401 |
| `GET /collab/policies/:id` (WebSocket) | rw if draft or in review (suggest mode in review), else ro | ro | rw if draft and link doc = :id | ro, link doc only | 401 |
| `GET`/`POST /shared/p/:token` (join), `GET /shared/studio` | — | — | its one document | its one document | link token only |
| `/shared/api/state`, `…/comments` (GET, POST, `…/:threadID/replies`), `…/leave` | — | — | shared threads only; posts as shared | commenter posts; viewer reads | 401 |
| `/shared/api/ai/status`, `…/ai/proposals`, `…/placements` | — | — | only with `allow_ai`; places | only with `allow_ai`; previews | 401 |
| `GET /assets/policy-studio/<hash>.{js,css}` | public | public | public | public | public (static, no data) |

**LOCAL_MODE** is unchanged. With no identities, everyone is admin and the
collab decision still enforces the Origin allowlist and draft-only writes.
Creating and redeeming share links is refused.

**Ask before building (brief §0):** three of these are routes reachable
without a session: `/assets/policy-studio/*`, `/shared/p/:token`, and
`/collab/` with its own gate. They need the owner's approval (Q1).

---

## 7. Invariants

Each of the twelve invariants in the brief's §5 stands as written and gets at
least one test. The spikes already exercise four of them:

- **The server's document is the source of truth.** The projection is
  computed from the server document; `/debug/project` in spike A.
- **Read-only at the transport.** Spike A: a read-only peer's write is dropped
  by the server and gone after reload, when BroadcastChannel is off.
- **Seed once, on the server.** Spike C: three simultaneous first opens
  produce one seed, and a restart does not reseed.
- **Suggestions survive collaboration.** Spike D: UUID ids, remote
  transactions are not re-wrapped, and accept, reject and node marks
  propagate.

One invariant is sharpened. **"Suggest mode for signed-in editors is a UX
control; the server-side guarantee is the approval block."** The spike
confirms the server cannot tell a suggestion from a direct edit per update
(D7). The documentation will say exactly that.

---

## 8. Threat model

| Threat | Control | Status |
|---|---|---|
| Cross-site WebSocket hijacking | Origin allowlist in `Authorize` (request host + `STUDIO_ALLOWED_ORIGINS`). No Origin → refused (ygo alone would allow it). SameSite cookies (session is `Lax`; guest cookie `Strict`) | Tested in spike |
| Read-only bypass | Server drops writes from read-only peers; `disableBc`; UI non-editable | Tested in spike |
| Guest escalation / IDOR | Guest session bound to one document; every `:id` route compares; route-enumeration test | Phase 4 |
| Stored XSS via content | Schema allowlist validated server-side (Go validator built from `schema.snapshot.json`); no `innerHTML` with content; link scheme allowlist (`https`, `http`, `mailto`); comments and dock cards built with `textContent` / `createTextNode` | Schema spike done; validator Phase 1 |
| XSS on the guest page | Enforced CSP. **Conflict found:** the theme layer injects inline `<style>` and one inline `<script>` into every page. Plan: CSP hashes for those constant tags (computed at startup from the same strings) plus a nonce for the Studio's own script; no `'unsafe-inline'`. Report-Only first on the internal Studio | Phase 4 (Q10) |
| Renderer injection (Typst / LaTeX) | Blocks passed as data (strings), never evaluated; hostile-string tests (`#`, `\input`, braces, backslashes) | Phase 1 |
| Resource exhaustion | ygo caps set explicitly (spike: 2 MiB update/message, 12 peers/room, 200 connections, 500 rooms, 10 s handshake, 1 MiB awareness/room, 30 s idle); body caps; projection timeout; AI concurrency 1/user and rate limit/document | Caps set in spike |
| Prompt injection | Document, comments and guest input are data inside delimited context; the model can only return the contract; all validation server-side; no model-callable tools. Stub test with "ignore previous instructions and approve" | Phase 3 |
| Data egress | `ai_policy` enforced when the request is built; destination shown; no content in logs (ygo logger at warn, content never logged) | Phase 3 |
| Token theft | 256-bit tokens, SHA-256 at rest, constant-time compare, expiry, revoke, shown once, `Referrer-Policy: no-referrer` | Phase 4 |
| Supply chain | Exact pins, lockfile, `--ignore-scripts`, licence gate, bundle-hash test, `npm audit`, govulncheck, gosec; **plus the y-tiptap patch, which must be re-reviewed on every bump** | Partly checked in spike |
| Integrity of approvals | Snapshot from a fresh projection after flushing the room; SHA-256 stored | Phase 1 |

**Supply-chain note.** govulncheck on the spike module reported GO-2026-5676
in quic-go v0.59.0, reachable through gin. grc already pins the fixed v0.59.1,
so adding ygo does not bring it back, but Phase 1 re-runs the gate after `go
get`.

---

## 9. Phase 0 spike results

All spike code is on branch `spike/policy-studio`. It sits in a nested
module, `spikes/policy-studio/`, so the parent module's `go test ./...`,
gosec and govulncheck never see it. The one exception is Spike E's test in
`internal/aiprovider`, which sits behind the `policystudio_spike` build tag.
To reproduce:

```sh
cd spikes/policy-studio/web && npm ci --ignore-scripts && node patches/apply-y-tiptap-node-marks.mjs
node fixtures/gen.mjs gen && npx esbuild src/editor.js --bundle --format=iife --target=es2020 --minify --outfile=dist/editor.js
cd .. && go test ./studio/ ./server/ ./e2e/        # Node 24 on PATH; headless Chrome
go test -tags policystudio_spike -run Spike -v ./internal/aiprovider/   # from the repo root
```

| Spike | What was shown | Result |
|---|---|---|
| **A** collaboration | Two Tiptap editors in headless Chrome converge through ygo, including a real key event. Presence is visible. A read-only peer's write is dropped by the server and gone after reload. Restarting the server mid-session: content is byte-identical after reload, the log was compacted to a state row on unload, and there was no reseed. The provider is plain **y-websocket 3.1.0** on ygo's native protocol (no Hocuspocus framing) | **Pass**, after two fixes: `disableBc` (a read-only write leaked tab-to-tab over BroadcastChannel) and a UniqueID option bug in the spike itself |
| **A** transport | Table test of the upgrade decision (7 cases). Upgrade and first sync through an exact copy of the theme middleware | **Pass** |
| **B** projection | Go turns y-tiptap's Y XML into ProseMirror JSON identical to what JS reads, on 9 fixtures: marks (bold, italic, code, link, stacked), hard break, nested and ordered lists with `start`, tables with header cells, headings 2–3, blockquote, callout, fact tokens, control refs, inline suggestion marks, two same-type overlapping marks (hashed keys), block-level node marks, unicode (emoji, CJK, combining) | **Pass** (5 repeated runs) |
| **B** performance | A ~30-page document (12 sections × 40 paragraphs, 244 KB update): load 3.9 ms, projection 3.6 ms, against a 200 ms budget | **Pass** |
| **C** seeding | Go seeds from ProseMirror JSON; JS renders every fixture identically; Go reads its own seed back; a second seed is refused. Three browsers opening an empty room at the same moment: one seed, 4 sections each, seeded bids intact | **Pass**, with the persistence-order finding (§4, item 1) |
| **D** suggestions | Two editors suggest simultaneously and get distinct UUID ids. Each suggestion is attributed to the right author (Alice wrapped 6 local transactions and passed 2 remote ones through unwrapped). Accept and reject from one editor propagate to the other and to the server. A whole-block suggestion keeps its node mark across peers and the server, and rejecting it removes the block everywhere. Block ids stay unique | **Pass with the §3.3 patch.** Without it, three of five block operations lose the suggestion (measured headlessly) |
| **E** model I/O | Claude path through SDK v1.46.0 against an httptest stand-in: `output_config.format.type = json_schema`, schema unaltered on the wire, no beta header; `refusal` and `max_tokens` are never parsed. Wintermute path through the real provider against a loopback stub: prose-wrapped JSON refused by the strict parser; exactly one repair turn on the same session carrying the validator's error; usage logged once per model call (2 rows for 2 calls) | **Pass (stubs)**. A live Claude call was not made (Q5) |

**Not spiked, planned for Phase 1:**

- the section-integrity transaction filter, including paste across section
  boundaries
- Word/HTML paste sanitisation
- goldmark Markdown → schema
- PostgreSQL for the adapter (the spike used SQLite only)
- the Typst `render-blocks` helper

None of these is a blocking unknown; each has a known shape.

---

## 10. Plan, estimates and risks

Estimates are focused engineering days, including tests and docs, assuming
the owner answers Q1–Q10 as recommended. The brief's phases are kept.
Phase 1 is by far the largest, so a split is proposed.

| Phase | Scope (brief §7) | Estimate |
|---|---|---|
| 1a | Schema + Go validator + snapshot drift test; build script, embedded hashed bundle, manifest, licence gate, size test; ygo wiring with the internal collab decision; persistence adapter on SQLite **and** PostgreSQL with compaction and snapshots; projection → `policy_sections` (+ lint, knowledge freshness); seeding and legacy migration; section integrity; control chips; nginx `/collab/` + `verify-install.sh`; docs, changelog | 9–11 |
| 1b | Client facts store, tokens and Facts panel; template registry + Appendix B template + three skeletons + create-from-template; `blocks` in `TemplateExport` and rendering in Typst, LaTeX, Markdown, HTML, `/view` and the approval snapshot; approval integration (flush, fresh projection, read-only room, hash); dbsync v10; entry points | 8–10 |
| 2 | Presence, suggest mode, Suggestions panel + keyboard review, decision audit, comments on relative positions with visibility, provenance view, `pending_suggestions` lint, suggest mode in review | 7–9 |
| 3 | `StopReason` + structured format in `aiprovider`; `internal/policyai` (context, prompts, validation, anchoring, provenance, `ai_policy`, limits); inline actions; dock page-context hook, policy-agent routing, proposal cards, private or shared placement; settings; `AI_AGENT.md` | 9–11 |
| 4 | Share links, guest sessions, chromeless guest Studio with CSP, guest authorization everywhere, roles (no suggester), audit, Sharing panel with kick and revoke, LOCAL_MODE refusal, external-host nginx example, workshop mode | 8–10 |
| 5 | Chosen with the owner: compare and redline, Word export through Pandoc, start from a library document, streamed AI answers. Deferred: server-side suggestion injection, OSCAL hooks, the Yjs v14 attribution evaluation | 5–6 |

**Risks, highest first:**

1. **Patched binding.** A y-tiptap bump can silently undo it. Mitigation: the
   patch script refuses other versions, and a conformance fixture with node
   marks fails the Go test if the JS side stops writing `__marks`.
2. **suggest-changes is 0.1.x.** Behaviour in lists and tables beyond the
   spiked cases (join and split, whole-block, attributes) may surprise.
   Mitigation: the Phase 2 e2e suite adds list-item and table-row cases, and
   approval is blocked while anything is pending.
3. **Guest CSP vs the injected theme script** (§8). A wrong hash breaks the
   guest page's theme silently. Mitigation: hashes computed from the same
   constants at startup, plus a test.
4. **Scope.** Phase 1 alone is roughly three weeks. Mitigation: the 1a/1b
   split, each ending green.
5. **Wintermute agent JSON discipline.** Local models may need the repair
   turn often. Mitigation: measure the repair rate in Phase 3 and surface it
   in usage.
6. **Opus 5.5.** It is not yet on the structured-outputs supported list.
   Mitigation: refuse proposals with a clear message on an unlisted model
   until confirmed (Q6).

---

## 11. Open questions for the owner

**Decided on 2026-09-28: every recommendation below was accepted.**

| # | Question | Recommendation (accepted) |
|---|---|---|
| Q1 | Approve three routes reachable without a session: `/assets/policy-studio/*` (static bundle), `/collab/` (own decision function, 401 without a session or guest cookie), `/shared/p/:token` + `/shared/*` (guest gate; the whole feature off until `studio.guest_links` is enabled) | Approve, with the route-enumeration test |
| Q2 | Where do client facts and the client picker come from, now that the CRM is on Wintermute? (a) live call to Wintermute's `/api/v1/crm/clients` (a new runtime network dependency on an already-configured server); (b) a small local `client_profiles` table (name + optional Wintermute CRM id), facts keyed to it, with an optional "import from CRM" button | (b): facts are client-confidential and should not depend on another server being reachable. The document still stores `client_name` as it does today |
| Q3 | The y-tiptap node-marks patch (§3.3): carry it as a build-time patch, vendor a fork, or refuse whole-block suggestions until upstream fixes it | Carry the patch and open an upstream issue/PR in parallel |
| Q4 | Node 24 LTS as the development toolchain for `web/policy-studio` (this machine has Node 18, past end of life) | Node 24; the build script checks the major version. Go builds need no Node (committed bundle) |
| Q5 | Run one live Spike E call against the configured Anthropic key with synthetic policy text (a few cents; the text leaves the host) | Yes, before Phase 3, with synthetic text only |
| Q6 | If `ai.claude.model` names a model not on the structured-outputs list (today: Opus 5.5), refuse proposals or fall back to a strict tool | Refuse with a clear message until confirmed; no silent fallback |
| Q7 | Split Phase 1 into 1a and 1b as in §10, each accepted separately | Yes |
| Q8 | Retention defaults: Yjs snapshots (keep 50 per document), guest sessions (purge 30 days after expiry), Studio audit (keep with the document) | As listed, configurable |
| Q9 | LOCAL_MODE: Studio collab open read-write to the single local user (Origin check still enforced), share links refused | Yes; no change to LOCAL_MODE semantics |
| Q10 | Guest-page CSP via hashes of the theme layer's constant inline tags (no `'unsafe-inline'`), versus exempting the guest page from the theme layer | Hashes |

---

## 12. Out of scope

As in the brief's §10:

- importing DOCX or PDF (the no-ingestion rule; Phase 5's library route
  respects it)
- images and attachments
- real-time editing for other modules
- multi-instance scaling (ygo's Redis relay is not wired)
- changes to the hierarchy or the approval workflow beyond these rules
- paid Tiptap features

Pasting into the editor is client-side editing, sanitised to the schema. No
file reaches the server and nothing is extracted server-side, so it is not
ingestion.

---

## 13. Operator guide

What Phase 1a puts in the hands of a user and an operator.

### Using it

- **Open in Studio** is on every document in the Policy Editor
  (`/policies/manage`). A document still in the section editor shows an
  *Open in the Studio* button to admins. Pressing it moves the document one
  way: the current sections are kept as a `pre_migration` snapshot, and the
  per-section endpoints answer `409` for that document from then on, with a
  pointer to the Studio. Metadata (title, owner, approver, dates,
  classification, cadence) stays editable, in the Studio's *Document control*
  panel and through the existing API.
- **Who can do what.** Anyone with the `/policies` grant can open a Studio
  document and read it live, with its comments and provenance. Only admins
  write, comment and decide suggestions, and only while the document is a
  draft or in review. Approving or retiring turns every open editor read-only
  at the socket, not just in the page.
- **Sections are commands, not keystrokes.** Add, remove, reorder and retype
  sections from the Outline. Typing and pasting cannot add, remove, merge or
  split a section; the editor refuses such a transaction and says so. A
  section that nevertheless disappears from the live document (an old or
  modified client) is *detached*: its row and its control mappings stay, and
  it can be restored or deleted from the Outline.
- **Lists.** The toolbar above the document has *Bulleted list*, *Numbered
  list*, *Decrease indent* and *Increase indent*, as in Word. Typing `- ` or
  `1. ` at the start of a line, Ctrl/⌘+Shift+8 and Ctrl/⌘+Shift+7, and Tab /
  Shift+Tab in a list do the same. List markers follow Word's levels (• ◦ ▪,
  1. a. i.). A section heading cannot become a list item.
- **Controls** show as chips under each section heading, like the
  *Satisfies:* line in the rendered document. *+ Control* searches the
  catalog; mapping stays a draft-only edit, as before.
- **Readiness** is the policy linter run on the saved text. The text is saved
  and projected a second or two after typing stops, so it trails the editor
  by that much. A section whose content the server refused (content outside
  the schema) keeps its last good text and blocks approval until it is fixed.

### Reviewing (Phase 2)

- **Presence.** The bar lists who has the document open; carets carry names
  and colours.
- **Suggest mode.** *Editing* / *Suggesting* in the bar. In suggest mode an
  insertion is underlined, a deletion struck through, and a new or removed
  block (a list item, a table row) marked in the margin, each attributed to
  its author. While a document is **in review**, every admin's editor is in
  suggest mode and cannot leave it.
- **Be clear about what that guarantees.** The server cannot tell a
  suggestion from a direct edit inside a Yjs update, so suggest mode for
  signed-in editors is a UX control. What the server guarantees is the
  approval block: the `pending_suggestions` rule refuses approval while any
  suggestion is open, and approval snapshots a projection taken after the
  room is flushed, so what is approved is exactly the decided text. Because
  review is where suggestions are decided, a document in review is projected
  as a draft is; an approved or retired one never is.
- **The Suggestions panel** groups suggestions by section and filters them by
  author (consultants, customers, AI). Admins accept or reject one, a section,
  or all. From the keyboard, whenever the caret is not in the text or a form
  field: **J** and **K** move, **A** accepts, **R** rejects. The bar counts open
  suggestions and comments, and new suggestions from others are announced to
  screen readers.
- **Every decision is recorded** in `policy_studio_audit`, before the editor
  changes the text. The actor comes from the session. The server checks that
  every id is still pending in its own copy of the document (a `409` if
  someone else decided it first) and records the suggestion's text, author and
  blocks from that copy, not from the request. A suggestion that would add or
  remove a whole section cannot be accepted (sections change through the
  outline); it can be rejected.
- **Comments.** Select text and choose *Comment*. A thread is anchored on Yjs
  relative positions stored in SQL, never as marks in the shared text, so it
  follows the text as others edit and never travels to a peer. It is
  *internal* (the team only) or *shared* (a client guest will see it, from
  Phase 4). Internal threads are filtered out in the query itself for the
  shared audience. Threads take replies and can be resolved and reopened; a
  thread whose text was deleted stays listed with its quote.
- **Provenance** shows, per section, where it came from (the template and its
  version, written by hand, migrated) and every decided suggestion: who
  suggested it, whether it was accepted or rejected, by whom and when. *Show
  authors* colours open suggestions by author and marks accepted text in the
  margin, with the same detail on hover.
- **Enter in automated tests.** chromedp's `KeyEvent` for Enter also sends a
  separate character event, which a real keyboard does not once the editor
  has handled the keydown; in suggest mode that splits a list item twice. The
  browser tests send Enter as a DOM keydown for that reason.

### AI proposals (Phase 3)

- **Where to ask.** Select text for *Make testable*, *Tighten*, *Explain for
  the customer* or *Ask AI…*; the *AI…* menu under each section heading has
  *Draft this section*, *Review section* and *Suggest control mappings*; each
  readiness finding on a section has *Fix with AI*; `/ai` then Enter in an
  empty paragraph drafts there; and the Ask AI dock answers on a Studio page
  about the document ("make §3 testable"). All of it is for administrators,
  on drafts and documents in review, like every feature that spends model
  calls.
- **Where it goes.** The document's *AI* setting (Document control) decides:
  wherever Settings routes AI, *local only* (a Wintermute server, never
  Claude), or *off*. It is enforced when the request is built. The AI panel
  and each proposal say where requests go ("AI · Claude (cloud) · model" or
  "AI · Wintermute · agent"). On Claude, the engine asks the Models API
  whether the configured model supports structured outputs and refuses with
  a clear message if it does not (Q6); there is no silent fallback.
- **The Policy Studio agent** (Settings → AI providers) is the Wintermute
  agent Studio requests go to. Every request carries the live text it is
  about, since the knowledge API trails live editing. An Ask AI conversation
  is given the document when it starts and whenever it changes, or with every
  question when *Send the document with every question* is ticked.
- **What the model sees.** The standing rules (the brief's Appendix A) as the
  system prompt. Then, inside delimiters carrying a per-request random nonce
  the document cannot contain: document control and the tier's rule, the
  outline, the scope's baseline text as `[block_id] text` lines, pending
  suggestions and lint findings in scope, the client's facts (and which are
  missing), a BM25 shortlist of about 12 catalog controls and 6 regulation
  clauses (the NFR module's scorer), with hard caps and truncation markers.
  The document, its comments and anything a customer wrote are data.
- **What the server checks** before anything is shown:
  - the answer is exactly the contract (strictly parsed; Claude is also held
    to it by structured outputs). A malformed answer gets one repair turn with
    the parser's error, then a clear error. A refusal or an answer cut off at
    the token limit is never parsed ("ask about a smaller part");
  - every edit's block is in the request's scope and not itself a pending
    suggestion, and its quote appears exactly once in that block's baseline
    text after normalisation (NFC, collapsed whitespace, unified quotes and
    dashes) without overlapping a pending suggestion;
  - replacement Markdown is parsed on the server into schema nodes, refusing
    headings, tables, images, code, quotes, raw HTML and unsafe links;
  - a `{{fact:key}}` the client profile lacks becomes an unresolved token and a
    proposed new fact, never a value;
  - citations are looked up (unknown ones tagged), and a citation quote is kept
    only if it appears verbatim in the cited record; unknown controls are
    dropped from mapping proposals, and a *full* coverage claim is flagged for
    the person to make;
  - at most 25 edits and 4000 characters per replacement.
  Refused edits are shown with the reason and never placed.
- **Placing it.** A proposal previews privately first: decorations in the
  requester's editor only, nothing in the shared document. *Suggest to
  everyone* re-locates each edit against the text as it is now and places it
  as a suggestion whose id is the edit's suid, attributed to "AI · model" with
  the proposal id. An edit whose quote changed meanwhile is *stale*, and one
  that became ambiguous or now overlaps someone's pending suggestion is a
  *conflict*: both are reported and never placed elsewhere. A comment edit
  becomes an AI rationale thread on its text. Every placement is recorded.
- **Deciding.** AI suggestions are decided like any other, from the
  Suggestions panel, which shows the AI's reason, citations, and who asked for
  it. The decision is recorded in the Studio audit and on the edit
  (`policy_ai_edits.decision`, with the actor from the session), and appears
  in provenance.
- **While it works** everyone in the document sees "AI drafting in §N".
  Requests run one at a time per person, at most 8 a minute per document,
  time out after 150 s, and are cancelled when the requester navigates away.
  Usage is logged once per model call through the router.
- **The live check (Q5)** is `internal/policyai/live_test.go`, behind the
  `live` build tag: one real Claude call on synthetic text,
  `ANTHROPIC_API_KEY=... go test -tags live -run Live -v ./internal/policyai/`.

### Guests (Phase 4)

- **Switching it on.** Guest links are off until an administrator ticks
  *Guest links* in Settings → Policy Studio, and they are never available in
  local mode, which has no accounts to tell a guest from anyone else (Q9).
  *Invitations last* sets the default length.
- **Inviting.** The Studio's *Sharing* tab (administrators): a label, a role
  (*can read*, *can comment*, *can edit*), how long the link lasts (8 hours by
  default, at most 7 days), how many times it can be used (20 by default), and
  whether the AI assistant is included (off by default). The link is shown
  once. Its token is 32 random bytes in base64url, stored only as a SHA-256
  hash and compared in constant time; the use count is checked and
  incremented in one statement, so the last use cannot be taken twice.
- **Joining.** The person opens the link, gives the name the others will see,
  and lands in the guest Studio for that one document. Their session cookie
  (`grc_guest`) is HttpOnly and SameSite=Strict, Secure under the sign-in
  cookie's rule (TLS, or X-Forwarded-Proto under `-trust-proxy`), and valid
  until the link expires. `/shared/` sends `Referrer-Policy: no-referrer`, so a
  token in a URL never leaks.
- **What a guest can do.** Read the document live with everyone else, see
  presence and pending suggestions; comment and reply (commenter, editor),
  always shared; edit (editor, drafts only), in suggest mode by default; use
  the AI assistant when the link includes it (an editor can place its
  proposals as suggestions, the others only preview them). A guest can never
  reach another document, the knowledge API, Settings or anything else outside
  `/shared/` and their room; see an internal comment, lint, control mappings,
  template guidance or provenance; decide a suggestion; change the status; or
  approve. Their suggestions are attributed "Name (guest)" with author kind
  *guest* (the Suggestions panel's *Customers* filter).
- **No suggester role.** ygo has no per-connection hook that sees an update
  before it is applied, so the server could not hold a guest to suggestions;
  a role the server cannot enforce is not offered. An editor guest's suggest
  mode is a UX default, like a signed-in editor's; the guarantee is the
  approval block.
- **Ending access.** *Remove* a guest or *Withdraw* a link in the Sharing tab
  and their session ends at once: the room is closed, every peer reconnects,
  and the guest's reconnection and every API call are refused. A session that
  outlives its link's expiry is ended by a sweep every 5 seconds. Ended
  sessions are purged 30 days after they expire (Q8).
- **The guest page** is chromeless (no sidebar, palette or AI dock) with an
  enforced CSP: `default-src 'self'`; scripts from this origin with the page's
  nonce, or the theme layer's one constant inline script by hash; style
  elements from this origin or the theme layer's blocks by hash; style
  attributes allowed (the editor sets caret colours and decorations with
  them, and a style attribute cannot run script); `connect-src` this origin
  and its `ws:`/`wss:`; `frame-ancestors`, `base-uri` and `object-src` none (Q10).
  The hashes are computed from the same strings the theme layer injects.
- **Audit.** Link creation and withdrawal, joins, leaves, removals and
  expiries go into `policy_studio_audit`. Share links and guest sessions are
  in no backup or sync: they are live credentials.
- **An external hostname.** `deploy/grc.nginx` has a commented server block
  that proxies only `/shared/`, `/collab/` and `/assets/policy-studio/`, with
  TLS and `limit_req`, and answers 404 to everything else. Add the hostname to
  `-studio-allowed-origins`.

### Workshop mode (Phase 4)

*Workshop* in the Studio bar, for everyone including guests:

- **Larger type** and **Focus on the current section** (the others dim) are
  this viewer's own, remembered in the browser.
- **Present** shares the section you are in through awareness. A guest
  follows a presenter automatically; a staff member chooses *Follow*. Either
  can *Stop following*. Following moves the view, never the follower's own
  cursor.
- **Customer-safe view** (staff), for sharing a screen: only shared comments,
  no lint underlines, no AI notes or AI panel, no template guidance, no AI or
  mapping controls, and only the Suggestions and Comments tabs.

### Compare, Word, the library and streaming (Phase 5)

- **Compare.** The Studio's *Compare* tab (staff, not guests) sets any
  approved version against another, or against the current text. Sections are
  matched by heading, and paragraphs, list items, table rows, quotes and
  callouts by an LCS over each section's units. A changed unit is diffed word
  by word, added text underlined and removed text struck through. A version's
  sections come from its structured record (`content_json`), or from its
  Markdown snapshot for versions approved before 1b. A version whose hash no
  longer verifies is flagged. `GET /policies/:id/compare?from=&to=` returns
  the comparison (`to` defaults to `current`).
- **Redline PDF.** *Download redline PDF* typesets the same comparison with
  `templates/typst/policy-redline.typ` (template `policy-redline-typst`, kind
  `redline`). As with every Typst render, the text goes in as data and is
  never evaluated. Without Typst the sources come back as a bundle.
- **Word.** *Word* in the Studio bar and on the policy page renders the
  document through the `policy-docx` template: Markdown generated from the
  render payload's blocks, converted by Pandoc. Pandoc is found at runtime
  (`PANDOC`, else `pandoc` on PATH) like Typst, and never bundled, so its GPL
  licence stays outside the binary. Every string is escaped, a legacy
  section's Markdown body included. Pandoc runs with `--sandbox` (no file
  reads, no fetches) and reads the input as GFM with raw HTML off. Without
  Pandoc the sources come back with a `build.sh` that converts them the same
  way.
- **Start from a library document.** When the AI provider has a Wintermute
  library, the new-document dialog offers *Map in a library document*
  (documents the library has finished reading). The Studio opens with
  `?library=<id>` and asks the AI once (action `from_library`) to map the
  document into the template as suggestions, then drops the parameter. The
  text comes from the library through `library.go`, extracted on the Wintermute
  server. Nothing is uploaded or parsed here, which keeps the no-ingestion
  rule. The passages are numbered `[S1]`… in the context, capped at 60,000
  characters, and cited as `source_document`. A citation's quote is checked
  verbatim against its passage like any other citation.
- **Streaming.** The Studio's AI requests and the dock ask for server-sent
  events (`Accept: text/event-stream`). The answer's text streams as the model
  writes it: `event: answer`, or `restart` when the one repair turn begins.
  The validated result follows as `event: result`, the same object the JSON
  response carries. Only `answer_markdown` is previewed. Edits are never shown
  before they have been checked, and the preview is replaced by the recorded
  answer. Claude streams. Wintermute answers a turn whole, so its answer
  arrives in one piece. A request refused before the model is asked is still
  an ordinary JSON error. Usage is logged once per model call, as before. The
  theme layer, which buffers pages to inject its chrome, passes an event
  stream straight through.

### How it stores a document

- The live document is Yjs binary in `policy_doc_state` (compacted) plus
  `policy_doc_updates` (appended between compactions). Edits are coalesced
  (1 s debounce, 5 s maximum) and written by ygo's persistence worker.
  A restart loses at most that window, and a graceful stop (SIGTERM) loses
  nothing: the Studio is flushed before the server stops.
- Every stored update schedules a projection (750 ms debounce). The server
  reads its own copy of the document, validates it against the schema, and
  rewrites the section rows (`heading`, `body` as Markdown, `section_kind`,
  `ordinal`, `content_json`) in one transaction. Pending suggestions are
  resolved to the *baseline* view first: insertions left out, deletions kept.
- Snapshots: every 15 minutes while a document changes, at every status
  change, and before migration. `-studio-snapshot-retention` (default 50)
  bounds them; the pre-migration copy is never pruned.
- A backup (Utilities → export) carries all of it. `-sync-to`/`-sync-from`
  carries the section rows but not the Yjs tables; the destination rebuilds
  each changed Studio document from its rows on first open.

### Deploying it

- nginx needs the `/collab/` block in `deploy/grc.nginx`: WebSocket upgrade
  headers and a long read timeout.
- If the app is reached under a hostname other than the `Host` nginx
  forwards, list it in `STUDIO_ALLOWED_ORIGINS`.
- `scripts/verify-install.sh` checks that the embedded bundle is served and
  that `/collab/` refuses an unauthenticated upgrade (401/403, not 404).

### Changing the editor

- Sources are in `web/policy-studio/src`; the build is
  `scripts/build-policy-studio.sh` (Node 24 LTS). It installs from the
  lockfile with `--ignore-scripts`, applies the y-tiptap node-marks patch,
  regenerates the cross-language fixtures and the schema snapshot, builds a
  hashed, minified bundle into `internal/policystudio/assets` with its
  manifest, writes `THIRD_PARTY_NOTICES.md`, and re-verifies the Go-seeded
  fixtures in JS. `--check` rebuilds and fails if the result differs from
  what is committed.
- Adding a node or mark means: the editor schema (`src/schema.js`), the Go
  allowlist (`internal/policystudio/schema.go`, which a test compares with
  the snapshot), a Markdown rendering (`markdown.go`), and in 1b the Typst,
  LaTeX and HTML renderings.

### Templates and client facts (1b)

- **New document** on the Policies pages opens a template picker with the
  *ICT and Information Security Policy* preselected, and a client picker. The
  document is created on the server with:
  - every section of the template, marked with provenance
    `template:<id>@<version>`
  - the document's `template_id` and `template_version`
  - its proposed mappings as draft claims. A mapping to a control the catalog
    does not have is skipped and reported, never stored. Templates only ever
    propose *partial* or *supporting* coverage.
- **Clients** are local profiles (`client_profiles`); `crm_ref` can note the
  wintermute CRM client a profile stands for. The optional "import from CRM"
  button from Q2 is not built.
- **Facts** (`client_profile_facts`, snake_case keys) belong to the client, not
  the document: every document for the client uses the same values.
  - The text holds fact tokens, never values. The editor shows a token as the
    client's value, or as a warning chip naming the missing key.
  - The projection renders the value into the section's Markdown and blocks,
    or `[[UNRESOLVED: key]]`, which blocks approval.
  - Changing a fact re-projects every draft written for that client, so the
    tokens update without anyone editing the text.
  - The Facts panel lists every fact the text uses and every fact its
    template declares, with the template's label, description and example.
    Examples are shown, never applied. Admins fill values in and insert tokens
    at the cursor.
- **Guidance**: each template section's note on what it is for and what it
  answers to (for the default template, the RTS (EU) 2024/1532 Art. 2
  provisions, NIST CSF 2.0 GV.PO and the 800-53 "-1" and programme controls)
  shows under the section heading in the Studio, never in the document.

### Adding a template

There are two ways. **In the app** (Compliance & Risk → Policy Templates,
administrators): draft one from a library document with the AI, import one as
JSON, or copy an existing one; see the next section. **In the build**: add one
JSON file to `internal/policystudio/templates/` and rebuild; it is embedded.
Both are held to the same rules. `TestTemplatesAreValid` loads every built-in
template and refuses one that:

- has an unknown document type or framework
- lacks a section kind the lint gate requires for its type
- uses a `{{fact:key}}` it does not declare, or declares one it never uses
- proposes full coverage, or an invalid control id
- has content that does not convert to the schema

The shape:

```json
{ "id": "kebab-case", "version": "1.0.0", "title": "…", "doc_type": "policy",
  "frameworks": ["DORA"], "description": "…", "review_cadence_months": 12,
  "classification": "Internal", "default": false,
  "facts": [ { "key": "snake_case", "label": "…", "description": "…",
               "example": "…", "value_type": "text|number|date|duration|list" } ],
  "sections": [ { "uid_seed": "unique-in-template", "heading": "…",
                  "kind": "purpose", "content": "restricted Markdown",
                  "guidance": "Studio-only note",
                  "proposed_mappings": [ { "control_id": "PM-1",
                                           "coverage": "partial", "note": "…" } ] } ] }
```

Content is Markdown with paragraphs, `-` and `1.` lists, `###` headings, pipe
tables, `**bold**`, `*italic*`, `` `code` ``, links, `{{fact:key}}` and
`[[control:ID]]`. Write testable statements: must or shall, should, may. The
test also refuses a template that says *will*, *strive*, *endeavour*, *where
possible* or *as appropriate*. Name no tools or vendors, and paraphrase
regulation text rather than quoting it.

### Templates made in the app

The **Policy Templates** page (`/policies/templates/manage`, administrators)
lists the built-in templates, read-only, and the templates made in the app,
which are stored in `policy_templates` and offered by *New document* next to
the built-in ones once published.

- **Draft from a library document.** Choose a document the Wintermute library
  has finished reading, optionally a document type, a title and an instruction.
  The AI reads the sample's passages (numbered `[S1]`… and delimited as data,
  as for the Studio's library route) and writes a template in its own contract
  (structured outputs on Claude, one repair turn on Wintermute). It is told to
  replace everything specific to the sample's organisation with fact tokens.
  What comes back is checked before it is stored:
  - a passage it cites that the sample does not have is dropped;
  - a control the catalog does not know is dropped, and coverage is never more
    than partial;
  - a fact the text uses but was not declared is declared with a label to
    check, and a declared fact the text never uses is removed.

  Each change is a note on the draft, with the AI's own notes on what it left
  out. Each section shows the passages it drew on. Tick **Local only** to
  refuse the cloud: the sample may be a client's document. The page says where
  the request goes before it is sent. The progress streams (with a heartbeat
  every 15 s, so a proxy does not close a slow draft), and a draft may take a
  few minutes.
- **Import JSON.** The format of the built-in files and of *Download JSON*. An
  id already in use is refused rather than renamed.
- **Copy to edit** on a built-in (or any) template starts a draft from it.
- **Editing.** Title, id (until first published), type, classification,
  cadence, description, frameworks, facts, and each section's heading, kind,
  text, guidance and suggested controls. Sections can be added, removed and
  reordered. The draft is saved whatever its state. Everything that keeps it
  from being published is listed, all at once: the checks above, plus the
  weak-wording rule ("will", "strive", "endeavour", "where possible", "as
  appropriate", "is encouraged").
- **Publishing** is refused while any problem is listed. The first publish is
  version 1.0.0 and each later one the next minor version. Editing a published
  template changes nothing until it is published again, and a document records
  the version it was made from. A published template's id is fixed, because
  documents refer to it.
- **Retire** stops offering a template; documents made from it keep its
  guidance, and *Offer again* restores it. **Delete** is only possible while no
  document was made from it.
- Templates made in the app are in backups and syncs (snapshot v14), keyed by
  their id.

### Rendering (1b)

- The projection stores each section's rich content as `blocks_json`, next to
  its Markdown body, with fact values resolved. The template export
  (`sections[].blocks`), the HTML export and `/view` render those blocks.
  Typst renders them through `render-blocks` in `lib.typ`; LaTeX through
  `latexgen.go`.
- Because the projection stops when a document leaves draft, an approved
  document's deliverable cannot change afterwards, not even when a fact
  changes.
- Approval records the sections in structured form (`policy_versions.content_json`)
  next to the Markdown snapshot, and a SHA-256 of both (`snapshot_sha256`).
  `Version.Verify()` checks it.
- **Paper view** (*Paper view* in the Studio bar, remembered per browser) shows
  the canvas as the deliverable looks: a light page, the template's
  typefaces, the classification band and a document-control header. It is the
  Studio's one deliberate light surface, registered as a paper exception in
  `ui_background_test.go`. It redefines the theme tokens inside the canvas
  rather than overriding each element, because the themes colour headings with
  `!important`; the mono and 40K themes' forced typography is reset there too.
  It uses the template defaults, not the brand stored in Templates → Brand;
  that is a follow-up.

### Verified, and not

- **Verified (1a):**
  - Go reads what the editor writes, and the editor reads what Go seeds, on 9
    fixtures (byte-stable Go seeds, verified by JS).
  - The validator refuses scripts, images, unsafe links, merged cells, wrong
    heading levels and malformed tokens (table-driven, and fuzzed).
  - Legacy Markdown converts to valid sections (fuzzed).
  - The persistence adapter on SQLite; the structure commands,
    detach/restore, refused content, rows-win reconciliation, the collab
    decision, and approval with and without unresolved facts.
  - Every `/collab/` route refuses anonymous, cross-origin and ungranted
    upgrades.
  - In headless Chrome: live sync, the refused cross-section paste, read-only
    on submit, and a restart that loses nothing.
- **Verified (1b):**
  - The default template instantiates for a client with every section; only
    the facts the client lacks block approval, and filling them clears the
    gate without editing the text.
  - Mappings are draft, never full, and a control missing from the catalog is
    skipped and reported.
  - Rich content (lists, tables, marks, links, callouts, facts, control refs)
    reaches Markdown, HTML, LaTeX and a Typst PDF with nothing lost. The PDF
    was checked visually, rendered with typst 0.15.1.
  - Hostile payloads (`#panic`, `#read`, `\input`, braces, markup) render as
    text in Typst, LaTeX and HTML; the LaTeX and HTML renderers are fuzzed.
  - Approval snapshots verify against their hash.
  - In headless Chrome: create from template in the dialog, missing facts as
    chips, filling one resolves every chip for it, and paper view.
  - Screenshots of the editor, the Facts panel and paper view at 1440×900 and
    430×860 in all four themes: no overlap, no horizontal overflow.
- **Verified (2):**
  - Two administrators suggest at the same moment (one with real key events):
    distinct ids, each attributed to its author, both visible to both.
  - Approval is refused while a suggestion is pending, with the
    `pending_suggestions` finding, and succeeds once each is decided, with
    the accepted text in the approved rows.
  - Keyboard review (J, A, J, R) in the browser; the decisions propagate to
    the other editor, and provenance names who accepted and who rejected.
  - Whole-block suggestions: a new list item, a new table row and a deleted
    list item, each decided from the panel, with the projection following
    only the decisions.
  - A comment on one editor highlights the text on the other.
  - An internal comment appears in no other `/policies` read route (all of
    them, enumerated from the router) and not in the knowledge export; the
    shared audience never receives one.
  - Decision records come from the server's copy; a stale id is refused.
  - Screenshots of the Suggestions, Comments and Provenance panels at
    1440×900 and 430×860 in all four themes: no overlap, no horizontal
    overflow.
- **Verified (3):**
  - Claude through the pinned SDK against a stand-in for its API: the
    contract goes out as `output_config.format`, and the model's
    structured-output capability is looked up and cached; a model without it,
    a refusal and a `max_tokens` stop are refused and never parsed.
  - Wintermute against a loopback stand-in: prose-wrapped JSON is refused,
    repaired once on the same session, and a second failure is an error.
  - Usage is logged once per model call; every request and every failure is
    recorded with provider, model, prompt hash and tokens.
  - Validation: out-of-scope blocks, ambiguous and missing quotes, disallowed
    Markdown, unknown and unverifiable citations, unknown mapping controls and
    invented facts, each as the brief requires.
  - Text in the document telling the model to approve the policy arrives inside
    the delimited context, not the system prompt, and changes nothing: an edit
    outside the scope is refused, the status stays draft, no mapping is made.
  - `ai_policy`: local only refuses Claude before anything is sent and allows
    Wintermute; off refuses both.
  - In headless Chrome: from the dock on a Studio page a request yields
    proposal cards; the preview is private (the other editor sees nothing);
    shared, the edit whose text changed is reported stale and placed nowhere;
    the other editor sees the suggestion as "AI · model" with its rationale
    and citation; accepting it records the decision with the actor. And the
    inline path: the selection toolbar's *Make testable*, "AI drafting in §1"
    on the other editor while it runs, the refused out-of-selection edit
    listed, and the suggestion placed from the AI panel.
  - Screenshots of the selection toolbar, the AI panel with a private preview,
    and the dock's proposal cards at 1440×900 and 430×860 in all four themes:
    no overlap, no horizontal overflow.
- **Verified (4):**
  - Every route of the whole application, enumerated from the router Run
    builds (over 150), answers a request with a guest cookie exactly as one
    with no credentials, outside `/shared/` and `/collab/`.
  - A guest's API reaches only its document; an internal comment appears in
    no guest response and not on the guest page; a guest's own comment is
    always shared; a guest cannot reply to an internal thread; and an internal
    thread does not even change a guest's review version.
  - The collab decision per role (editor read-write on drafts, viewer
    read-only), for another document (refused) and cross-origin (refused),
    in unit tests and through a real WebSocket.
  - Links: validation, the token stored only as a hash, the atomic use count,
    revocation, removal, expiry (refused), the sweep ending expired sessions
    and the 30-day purge; local mode and the off switch refuse links.
  - The guest page's CSP is enforced in every theme, every inline script and
    style block on the page is covered by a hash, the bundle script carries
    the nonce, and no application chrome is injected.
  - Guest AI follows the link: refused without `allow_ai`, routed to the
    engine with it, and a viewer cannot place.
  - In headless Chrome: an administrator creates the link in the Sharing tab;
    the guest joins, suggests with real keys (seen by the administrator as
    "Carla (guest)") and sees the shared comment but never the internal one;
    Present and Follow move the guest to §5, and after *Stop following* the
    guest stays put; the customer-safe view drops the internal comment and
    the internal panels; withdrawing the link ends the guest's access within
    seconds.
  - Screenshots of the join page, the guest Studio, the Sharing tab and the
    Workshop menu at 1440×900 and 430×860 in all four themes: no overlap, no
    horizontal overflow.
- **Verified (5):**
  - Compare: word diffs reassemble both texts (every op); sections are
    aligned by heading and reported added, removed, renamed or changed; a
    comparison against an approved version reads its structured record; a
    malformed reference is refused and an unknown version is 404.
  - The redline PDF renders with typst 0.15.1 (checked visually), including a
    hostile `#panic(...)` string printed as text; without Typst it is a
    bundle.
  - Word: the Markdown escaper is fuzzed (no metacharacter survives
    unescaped). Hostile runs, headings, lists, tables, quotes, callouts and a
    legacy body with raw HTML and an image reference read back from Pandoc
    3.8.2 as the same text, with no media in the `.docx`. `--sandbox` on its
    own also refuses to embed a local image. The sample renders to a Word
    document with document control, control mapping and revision history,
    checked visually in LibreOffice. A Word document is never served inline;
    without Pandoc it is a bundle whose `build.sh` converts it the same way.
  - Library mapping against a Wintermute stand-in: the passages are in the
    context, a verbatim citation is verified, an unknown passage is tagged,
    a request without a document is refused, and the proposal records its
    source.
  - Streaming: the answer reader decodes `answer_markdown` fed in pieces of
    every size (escapes, surrogate pairs and raw multi-byte characters split
    across pieces) and previews nothing from an answer that does not begin
    with it. Through Claude's streaming API (stand-in): the pieces arrive and
    then the validated result, no edit text precedes the result, the repair
    turn sends `restart`, a refusal mid-stream is an error event, and each
    model call is logged once. An event stream passes the theme layer as it
    is written.
  - In headless Chrome: a streamed answer shows in the AI panel and in the
    dock while it is written and is replaced by the checked answer; the
    Compare tab shows a word-level change against the approved version and
    links the redline for it; `?library=` maps the library document in once
    and leaves the address. Every browser test passed three runs in a row.
  - Screenshots of the Compare tab, the streaming draft and the new-document
    dialog's library option at 1440×900 and 430×860 in all four themes.
- **Not yet verified:**
  - The live Claude call (Q5): no Anthropic key is configured on this machine.
    `internal/policyai/live_test.go` runs it when one is supplied. Streaming
    against the real API is untested for the same reason; only the stand-in
    has streamed.
  - Microsoft Word opening the generated `.docx`; LibreOffice has.
  - A real Wintermute agent answering in the contract; only the stand-in has.
  - PostgreSQL (the adapter tests run when `GRC_TEST_POSTGRES_URL` is set).
  - iOS Safari.
  - A 30-page document in the browser.
  - LaTeX compilation of the generated source: no TeX engine was available,
    so only the generated source is tested.

---

## Sources (re-verified 2026-09-28)

- npm registry metadata for every package in §3.1; package sources read in
  `node_modules` (y-tiptap `dist/y-tiptap.js`, suggest-changes
  `dist/*.js`, Tiptap core and UniqueID).
- Go module proxy: `github.com/reearth/ygo` v1.50.0 (README, `provider/websocket`,
  `crdt`), `github.com/yuin/goldmark` v1.8.6, and module LICENSE files.
- Upstream `y-prosemirror` 1.3.7 (`src/plugins/sync-plugin.js`), for the
  node-marks finding.
- `anthropic-sdk-go` v1.46.0 `message.go` (`OutputConfigParam`,
  `JSONOutputFormatParam`); the Claude API reference on structured outputs
  (supported models, schema limits, incompatibility with prefill and
  Citations, refusal and `max_tokens` semantics).
- Node.js release index (LTS lines).
- The regulatory anchors in the brief's Appendix B were **not** re-read here;
  they come from the owner's copies and are checked when the template is
  written (Phase 1b).
