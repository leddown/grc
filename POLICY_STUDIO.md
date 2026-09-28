# Policy Studio

Design, decision record and operator guide for the Policy Studio: WYSIWYG
policy authoring on top of `internal/policydocs`, live co-editing with the
customer, tracked suggestions, and an Ask AI agent that proposes changes as
suggestions a person accepts or rejects.

**Status: Phase 0 (design and de-risking spikes) complete, awaiting approval.**
Nothing in this document is wired into the application yet. The spike code
lives on the throwaway branch `spike/policy-studio` (see [§9](#9-phase-0-spike-results)).
Phase 1 does not start until the open questions in [§11](#11-open-questions-for-the-owner)
are answered.

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
| `golang.org/x/time` | v0.10.0 (via ygo) | BSD-3-Clause | Per-peer rate limiter inside ygo; the only new compiled transitive dependency |
| `github.com/yuin/goldmark` | v1.8.6 | MIT, no dependencies | Parses legacy section Markdown and AI `replacement_markdown` into the schema. The repo has no Markdown parser. Proposed; not spiked |

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
policy_documents      + editor_format TEXT NOT NULL DEFAULT 'markdown'   -- markdown | studio
                      + template_id TEXT NOT NULL DEFAULT '', template_version TEXT NOT NULL DEFAULT ''
                      + ai_policy TEXT NOT NULL DEFAULT 'inherit'        -- inherit | local_only | off
                      + projection_version INTEGER NOT NULL DEFAULT 0
                      + seeded_at TEXT NOT NULL DEFAULT ''               -- the seed-once guard
policy_sections       + uid TEXT (UNIQUE; backfilled with UUIDs), content_json TEXT NOT NULL DEFAULT '',
                        detached_at TEXT NOT NULL DEFAULT ''
                        (body stays the column name; for Studio documents it holds the projected Markdown)
policy_versions       + snapshot_sha256 TEXT NOT NULL DEFAULT '', content_json TEXT NOT NULL DEFAULT ''
policy_doc_state      document_id PK, state BLOB, updated_at
policy_doc_updates    id, document_id, upd BLOB, created_at
policy_doc_snapshots  id, document_id, reason, state BLOB, created_by, created_at
policy_comment_threads id, document_id, section_uid, anchor_start TEXT, anchor_end TEXT,  -- base64 Yjs relative positions
                      quote, visibility (internal|shared), kind (comment|ai_rationale), suggestion_suid,
                      status, created_by, created_at, resolved_by, resolved_at
policy_comments       id, thread_id, author, author_kind, body, created_at
policy_ai_proposals   id, document_id, requested_by, action, scope, instruction, provider, model, served_by,
                      agent, prompt_sha256, context_fingerprint, status, input_tokens, output_tokens, created_at
policy_ai_edits       id, proposal_id, suid UNIQUE, op, block_id, quote, fragment_json, rationale,
                      citations_json, validation, placement, decision, decided_by, decided_at
policy_share_links    id, document_id, token_sha256 UNIQUE, label, role, allow_ai, max_uses, uses,
                      created_by, created_at, expires_at, revoked_at
policy_guest_sessions id, link_id, session_sha256 UNIQUE, display_name, created_at, last_seen_at, expires_at
policy_studio_audit   id, document_id, actor, actor_kind, event, detail_json, created_at
client_profile_facts  client_ref, key, value, value_type, source, updated_by, updated_at  (PK client_ref, key)
```

**Changes from the sketch:**

- `seeded_at` replaces a separate guard table.
- The approval snapshot keeps its hash (Appendix D) and its ProseMirror JSON,
  so later rendering can re-typeset the approved content, not just its
  Markdown.
- `client_profile_facts` keys on `client_ref`, pending Q2.
- Update rows carry no `actor`. ygo coalesces updates from several peers into
  one write, so a per-row actor would be a guess. Who-did-what comes from the
  suggestion marks, the decision audit and the Studio audit table.

**`dbsync`:**

- All new tables join the snapshot, parent-first. `SnapshotVersion` goes to
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
| `/policies/:id/studio/comments` | all threads | read internal+shared | shared only | shared only (commenter posts) | 401 |
| `/policies/:id/studio/ai/proposals`, `…/ai/edits/:uid/decision` | ✓ (decision: admin only) | 403 | proposals only if `allow_ai`; never decisions | 403 | 401 |
| `/policies/:id/studio/migrate` | ✓ | 403 | — | — | 401 |
| `/policies/:id/share-links` | ✓ | 403 | — | — | 401 |
| `GET /collab/policies/:id` (WebSocket) | rw if draft, else ro | ro | rw if draft and link doc = :id | ro | 401 |
| `GET /shared/p/:token` → `/shared/…` | — | — | its one document | its one document | link token only |
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
| 5 | Prioritise with the owner | — |

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

| # | Question | Recommendation |
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
