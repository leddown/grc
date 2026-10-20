# Architecture

How the application is put together: the process, the packages, how a request
is served, how pages are made, where data lives, how AI calls leave the
process, and what runs in the background. Checked against the code on
2026-10-10.

## The shape in one paragraph

One Go binary. It opens one database (SQLite file or PostgreSQL), creates or
upgrades the schema, seeds the two catalogs if their embedded source changed,
builds a Gin router with two global middlewares, and serves HTTP on one
address. Every page is rendered on the server as a complete HTML document and
then fetches its data from JSON routes in the same process. The only network
calls the process makes are to the Wintermute server (AI and the document
library) and, when a user asks, to a Jira Cloud site. PDF and Word output is
produced by external programs started as child processes.

```
                 ┌────────────────────────── grc process ───────────────────────────┐
 browser ──HTTP──▶ gin: logger, recovery                                             │
                 │   themeMiddleware      (rewrites every HTML response)             │
                 │   pageAccessMiddleware (session + page grants; absent in local mode)
                 │   routes ─▶ module handlers ─▶ services ─▶ repositories ─▶ db.Conn ─▶ SQLite | PostgreSQL
 browser ──WS────▶ /collab/policies/:id ─▶ ygo rooms (Yjs) ─▶ projection ─▶ policy_sections
 agent ──HTTP────▶ /api/knowledge/* (read-only, own token)                            │
                 │                                                                    │
                 │ aiprovider.Router ──HTTP──▶ Wintermute server (agents, models, library)
                 │ jira.Client ──HTTPS──▶ a Jira Cloud site the user names             │
                 │ chromedp ──▶ headless Chrome        doctemplate ──▶ typst | tectonic | latexmk | pandoc
                 └────────────────────────────────────────────────────────────────────┘
```

## Entry points and start-up

`main.go` and `cmd/api/main.go` are the same three lines: bind flags, parse,
`app.Run`. `cmd/userctl` is a separate small program that edits accounts
directly in the database. `cmd/grc-render` is a separate service for another
host: it runs Typst and Pandoc on workspaces GRC sends it when `RENDER_URL`
is set (`internal/doctemplate/remote.go`, `DOCUMENT_TEMPLATES.md`).

`app.Run` (`internal/app/app.go`):

1. Opens the database: PostgreSQL if `DATABASE_URL` is set, otherwise the
   SQLite file. Opening runs the schema script, so a new database is created
   and an old one upgraded with no separate step.
2. If `-sync-to` or `-sync-from` is set, copies data between two databases and
   exits without serving.
3. `buildRouter`:
   - `buildHandlers` constructs the catalog services and seeds them. A SHA-256
     of each embedded catalog file is compared with the one stored in
     `app_state`; on a difference (or an empty table) the catalog is reseeded,
     and if either catalog changed the NFR-control link table is rebuilt.
   - `gin.Default()` plus `themeMiddleware`, plus `pageAccessMiddleware`
     unless in local mode.
   - Registers the core routes, then each module's routes through a
     `register<Module>Routes` function. Each takes the admin middleware and
     the local-mode flag and applies the same pattern: read routes on the
     router, write routes on a group behind the admin gate.
   - Builds the settings service (loading or generating the master key), the
     AI router, the shared PDF renderer and the knowledge service, and hands
     them to the modules that need them.
4. `serve`: an `http.Server` with a 15-second read-header timeout. On SIGINT
   or SIGTERM it first shuts down the Policy Studio, which flushes every open
   document and disconnects its editors, and then the HTTP server, with 15
   seconds for both.

There is no dependency injection framework and no global service registry.
A few handlers in `internal/app` reach shared services through package
variables set once at start-up (`activeAIRouter`, `activeSettings`,
`activeStoredJSONDB`, the AI usage store).

## Packages

Dependencies point inwards to `internal/db`; `internal/app` is the only
package that knows about all the others. Sizes are non-blank lines including
tests.

### Composition and shared infrastructure

| Package | Lines | Responsibility |
|---|---|---|
| `internal/app` | 18,400 | Options and flags, router construction, the two middlewares, the theme layer, and the pages with no package of their own: home, help, login, user management, settings page, AI chat and dock, Exceptions, Asset Types, Wiz Rules, JSON view, Jira pages, stored JSON, change log, API docs, utilities, the Exceptions Workbook page. |
| `internal/db` | 2,500 | `Conn` and `Tx` wrappers over `database/sql` with a dialect; the schema for both dialects; placeholder rewriting; `Insert` returning the new id on either backend. |
| `internal/dbsync` | 1,700 | Table-by-table copy between two databases, and the JSON snapshot export and import. |
| `internal/pageui` | 190 | The sidebar: one list of navigation groups rendered identically on every page. |
| `internal/apiutil` | 170 | Pagination parameters and slicing. |
| `internal/authn` | 900 | Accounts, bcrypt, sessions, login lockout, allowed pages, single-user mode. |
| `internal/user` | 370 | The original sample CRUD over the `users` table, and `/health`. |
| `internal/secrets` | 500 | The master key (environment, key file, or generated) and AES-256-GCM sealing. |
| `internal/settings` | 1,500 | Stored credentials (write-only) and preferences, with environment fallbacks; the Settings API. |
| `internal/aiprovider` | 2,400 | The one way out to a model: the Wintermute client, the router that logs usage, and the client for the agent's document library. |
| `internal/knowledge` | 2,200 | The read-only query surface over the installation's data, and the export bundle. |
| `internal/xlsx` | 730 | Writes `.xlsx` workbooks. |
| `internal/reporting` | 1,800 | HTML and CSS to PDF through headless Chrome; two sample reports. |
| `internal/data` | 40 | The two catalogs as embedded JSON. |

### Catalog and register modules

| Package | Lines | Responsibility |
|---|---|---|
| `internal/controlcatalog` | 4,100 | NIST controls: list, detail, hierarchy, editor, family visibility, CSV / Excel-XML / RTF exports. |
| `internal/controlfile`, `internal/controlid` | 390 | The control JSON file format; control id normalisation and natural ordering. |
| `internal/securitynfr`, `internal/nfrfile` | 2,200 | Security NFRs: list, detail, editor, weights; the NFR JSON file format. |
| `internal/nfrlink` | 930 | Derives NFR-control links from the NIST mapping text; overrides. |
| `internal/reports` | 1,200 | The linked / unlinked controls report. |
| `internal/exceptionbook` | 1,300 | The Exceptions Workbook. |
| `internal/riskregister` | 680 | The risk register. |
| `internal/auditfinding` | 1,800 | Audit findings, action plans, lifecycle rules, dashboard. |
| `internal/jira` | 480 | A Jira Cloud REST v3 client. |

### Policy modules

| Package | Lines | Responsibility |
|---|---|---|
| `internal/policydocs` | 6,000 | Documents, sections, control mappings, lint, status workflow and approval, versions, comparison, coverage matrix, HTML / Markdown / JSON exports. |
| `internal/policystudio` | 8,900 | The collaborative editor: the page and its embedded bundle, the Yjs server wiring, authorisation of the socket, persistence, projection into section rows, the schema validator, comments, suggestions and decisions, provenance, share links and guests, templates. |
| `internal/policyai` | 3,600 | AI proposals for the Studio: context, prompt, strict parsing of the answer, validation and anchoring of every edit, routing rules, limits, the record of proposals and decisions; drafting a template from a library document. |
| `internal/clientprofile` | 450 | Clients and their facts. |
| `internal/doctemplate` | 4,500 | The registry of typeset templates, brand settings, engine detection, and the render pipeline. |

### Analysis modules

| Package | Lines | Responsibility |
|---|---|---|
| `internal/regcoverage` | 4,800 | Import a regulation from the library, analyse each article against the catalogs, versioned report, chat, revision. |
| `internal/regmap/*`, `regmap/profiles` | 1,700 | Framework profiles (YAML) and what they turn a regulation's requirements into: ids, categories, seed crosswalk. |
| `internal/nfrenrich` | 3,100 | Import a security document, retrieve passages with BM25, propose NFR text with citations, review. Its BM25 ranker is reused by `knowledge`, `regcoverage` and `policyai`. |
| `internal/crisisexercise` | 11,300 | Exercises: scenario library, phases, injects, responses, decisions, DORA classification and notification clocks, findings, references, coverage, versions, AI generation and expert personas, PDF / CSV / Markdown outputs. |

### A module's internal layout

Most modules follow the same four files:

- `model.go`: types and vocabularies.
- `repository.go`: SQL. Usually an interface with one `SQLiteRepository`
  implementation that works on both dialects through `db.Conn`.
- `service.go`: rules and orchestration; no HTTP.
- `handler.go` (and `pages.go`): Gin handlers, the HTML pages, and
  `RegisterRoutes` / `RegisterAdminRoutes`.

Handlers receive `sessionUsername`, a function that reads the signed-in name
from the request context, so modules attribute actions to a person without
knowing how sessions work.

## Serving a request

1. **Gin** logs the request and recovers from panics.
2. **`themeMiddleware`** replaces the response writer with one that buffers.
   After the handler returns, a non-HTML response is written through
   unchanged. An HTML response is spliced: the theme's palette, the scale of
   sizes and the layout CSS before `</head>`, and the sidebar, command palette, theme toggle and AI
   dock before `</body>`. A page can opt out of the chrome (the guest Studio
   does). Streaming responses and WebSocket upgrades pass through.
3. **`pageAccessMiddleware`** (not in local mode) looks at the path. If it is
   not under one of the protected prefixes it lets the request through. If it
   is, it requires a valid session; a browser navigation without one is
   redirected to `/login?next=...`, anything else gets a JSON 401. For a
   non-admin user with an `allowed_pages` list, the path must be covered by a
   grant.
4. **The route.** Write routes sit in a group behind `adminTokenMiddleware`,
   which passes an admin session or the break-glass token.
5. **The handler** calls its service, which calls its repository.

The protected prefixes are a list in `internal/app/access_control.go`. A new
top-level path is public until it is added to that list; see
[SECURITY.md](SECURITY.md).

## Pages

There is no template engine and no front-end build for pages. A page handler
concatenates a Go string literal: a full HTML document with its own `<style>`
and `<script>`. The page's JavaScript fetches JSON from sibling routes and
builds the DOM with string concatenation and an `esc()` helper.

Consequences an analyst should know:

- Each page carries its own CSS, written for an older light, serif design.
  What a user sees is the **theme layer**, which overrides those styles from
  outside with `!important` rules and its own tokens (`--bg`, `--surface-2`,
  `--text`, `--accent`, `--border`). There are four themes, all dark: dark
  (default), matrix, chaos and 40k. There is no light theme.
- The theme layer restyles elements by tag and by common class names. A new
  control that is not a `button`, `input`, `select` or `textarea` takes none of
  it and must use the tokens itself.
- Navigation is one list in `internal/pageui`. Adding a page means adding it
  there, and to the lists in the help page, the home page, the API docs and
  the navigation tests.
- The Policy Studio is the exception: its editor is a JavaScript bundle built
  from `web/policy-studio`, committed under `internal/policystudio/assets`,
  embedded in the binary, and served under a content-hashed name.

The **AI dock** is part of the injected chrome. It posts to `/ai-chat/ask`
with the path of the page it was opened on, so the server can route a question
asked on a Crisis Exercise page or a Studio page to that module's agent with
the record on screen as context.

## Storage

`db.Conn` wraps `database/sql` and knows its dialect. Code writes SQL once,
with `?` placeholders; `Conn` rewrites them for PostgreSQL. Where the dialects
differ (`GROUP_CONCAT` and `STRING_AGG`, returning an inserted id) `Conn` has a
helper. Repositories are named `SQLiteRepository` for history; they run on
both.

SQLite is opened with WAL journaling, a 5-second busy timeout and foreign
keys on. SQLite's FTS5 is compiled in, though retrieval in the application is
done in Go (BM25 in `internal/nfrenrich`), not with FTS5.

The schema, the tables and how they are backed up are in
[DATA_MODEL.md](DATA_MODEL.md). Switching backends and syncing between them is
in [DATABASE.md](DATABASE.md).

## AI

Every model call goes through `aiprovider.Router`, which has one provider:
`Wintermute`.

- **Configuration is resolved per request** from Settings (with environment
  fallbacks): server URL, client token, backend, model and agent. A change in
  Settings applies to the next question without a restart. The URL must be
  `https`, or `http` to a loopback or private address.
- **A question** opens a session on the Wintermute server
  (`POST /api/v1/sessions`), posts the message, and reads the answer. The
  router records provider, model and token counts in `ai_usage_log`.
  Streaming, progress polling (`/progress`) and stopping a turn (`/stop`) are
  supported, which is how the AI Chat page shows a long turn while it runs.
- **Agents.** The Wintermute server holds agents; an agent is a library plus
  the sources it may consult, one of which is this application's knowledge
  API. Settings names up to three: the general agent, a Crisis Exercise agent
  and a Policy Studio agent.
- **The library.** `aiprovider.Library` lists an agent's documents and reads
  their extracted passages. Regulation Coverage, NFR Enrichment and the Policy
  Studio read documents this way. This application accepts no uploads and
  parses no PDF, DOCX or image.
- **The knowledge API** is the other direction: the agent calls
  `/api/knowledge/*` on this application to read the catalogs, policies,
  regulation reports, risks and exercises. The corpora are cached for 45
  seconds.

Rules the AI modules share, enforced in code and not left to a prompt:

- The model drafts; a person approves. Nothing a model returns is written to
  a catalog or a document without a review action.
- Generated content records the model and a hash of the prompt.
- Retrieval comes first and is lexical. The model is given a shortlist and
  told not to go outside it; references it returns are checked against the
  catalog, and quotes are checked against the source text.

## Collaborative editing

A Studio document is a Yjs document held in the process by `ygo`.

- The browser connects to `/collab/policies/:id` over WebSocket. The server
  authorises the upgrade itself: session or guest cookie, an `Origin` header
  that matches the request host or `STUDIO_ALLOWED_ORIGINS`, and read-write
  only when the document's status and the person's role allow it. Writes from
  a read-only peer are dropped by the server.
- Updates are persisted to `policy_doc_updates` and compacted into
  `policy_doc_state`.
- 750 ms after a change the server **projects** the document: it reads its own
  copy, validates it against the schema, and writes `policy_sections`. Lint,
  coverage, exports, the knowledge API and approval read those rows.
- Snapshots are taken every 15 minutes while a document changes, at each
  status change, and before a section-editor document is moved into the
  Studio. `STUDIO_SNAPSHOT_RETENTION` (default 50) bounds them.
- Limits are set explicitly: 2 MiB per update and per message, 12 peers per
  room, 200 connections, 500 rooms, a 10-second handshake, rooms closed after
  60 seconds idle.

Behind a reverse proxy the socket needs its own location block with the
upgrade headers and a long read timeout; `deploy/grc.nginx` has one.

## Rendering documents

| Output | Path | Needs |
|---|---|---|
| Regulation report, crisis after-action report, two sample reports (PDF) | `internal/reporting`: HTML templates rendered by one pooled headless Chrome, started on first use. 30-second render timeout. | Chrome or Chromium on the host |
| Policy document, business report, redline (PDF) | `internal/doctemplate`: the document as JSON plus a Typst template. | `typst` |
| Policy document (PDF, LaTeX) | The same JSON and a LaTeX template. | `tectonic`, or `latexmk` with XeLaTeX |
| Policy document (Word) | Through Pandoc. | `pandoc` 2.15 or later |
| Exceptions Workbook (`.xlsx`), control exports (CSV, Excel XML, RTF), MSEL (CSV), handout (Markdown) | Written in process. | Nothing |

Typesetting engines are detected once and cached. When none is installed a
render returns a zip of the sources and a build script, which is a normal
outcome. Child processes get a minimal environment, so tokens in the
application's environment are not visible to them. A render is bounded at
three minutes.

## Background work and in-memory state

There is no job queue and no scheduler. What runs outside a request:

- Policy Studio: the Yjs rooms, the debounced projection, snapshots, and a
  janitor that ends expired guest sessions every five seconds.
- The headless Chrome pool, once started.

Long operations run inside the request that started them: a regulation
analysis holds its request for up to 45 minutes and is cancelled if the
client disconnects; an AI chat turn is streamed with a progress event every
two seconds so a proxy's read timeout does not cut it.

State that lives only in memory and is lost on restart: failed-login
counters, the knowledge cache, AI dock sessions for the Studio, the detected
typesetting engines. Sessions, documents and everything else are in the
database.

The process assumes it is the only instance. Login lockout, the Studio rooms
and the caches are per process, so two instances behind a load balancer would
not behave as one.

## Configuration

Flags, with environment variables as defaults, are bound in
`internal/app/config.go`; a flag wins over its variable. A few variables have
no flag because they are read where they are used (`SETUP_TOKEN`, the master
key, the typesetter paths, Chrome's path, the Wintermute fallbacks). Settings
that an admin changes at run time live in the database and are edited on the
Settings page. [OPERATIONS.md](OPERATIONS.md) lists all of them.

## Build and embedding

`go build ./cmd/api` produces a self-contained binary: the catalogs, the
framework profiles, the Studio bundle and its templates, and the report
templates are embedded. Three things are read from disk at run time and have
to be installed beside the binary or pointed at: the markdown documents the
Change Log and `/knowledge/*` pages serve (`DOCS_DIR`), the Typst and LaTeX
templates (`TEMPLATES_DIR`), and the SQLite file.

Because nothing uses cgo, a cross-build is a `GOOS` setting
(`scripts/build-windows-cross.sh`). The release scripts are written around
goreleaser, but the `.goreleaser.yaml` they read is not in the repository; see
[OPERATIONS.md](OPERATIONS.md).
