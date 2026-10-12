# Analysis notes

An assessment of the codebase as it stood on 2026-10-10, written after reading
the code, the existing documents, the route table and the schema. It is
opinion built on the facts in the other documents; each point says what it
rests on. Security issues are in [SECURITY.md](SECURITY.md) and are only
summarised here.

Nothing listed here was changed as part of the review.

## What the project is, in one judgement

A single-owner, single-process application that grew from a control catalog
into a consulting workbench in about two months (61 commits from 2026-08-12).
It is unusually careful in its newer modules, where the design was written
down first and the rules are enforced in code, and plainer in its older ones,
which are CRUD pages over a table. Its operating assumption throughout is one
trusted firm, one server, one process.

## Strengths

**The AI modules are built to be checked.** Regulation Coverage, NFR
Enrichment, the Studio's proposals and the exercise generator share a
discipline that is in the code, not in a prompt: the model is given a
retrieved shortlist; ids it returns are looked up in the catalog; quotes are
compared with the source text; every generated record keeps the model and a
hash of the prompt; nothing reaches a catalog or a document without a person's
decision. See ARCHITECTURE.md, "AI".

**One way out to a model.** All calls pass through `aiprovider.Router`, so
credentials, routing and usage logging are in one place, and the browser never
carries a key or a model name.

**The collaborative editor treats the browser as untrusted.** The server holds
the document, validates it against a schema, and writes the projection itself.
The socket authorises its own upgrade with an origin allowlist. Route-table
tests assert that every guest and collaboration route passes its gate.

**The build is simple and reproducible.** One static binary, no cgo, catalogs
and editor bundle embedded, schema created on start. The security scanners run
as tests.

**Decisions are written down.** Package comments and the module documents say
why a thing is the way it is, often naming the bug that led to it. The change
log records what was verified and what was not for each change.

**Backups were thought through.** The sync, snapshot and raw backup each state
which tables they carry and why the rest are left out.

## Risks and weaknesses

### Security (detail in SECURITY.md)

1. Some routes answer without a session: saved JSON documents (which include
   Jira data) can be listed, read and written; the `users` table can be read;
   ten repository documents are served raw.
2. A database file with an account's password hash and two old key files are
   committed and pushed.
3. The access model fails open: a path is public unless its prefix is on a
   hand-maintained list.
4. Smaller: first-admin claim when `SETUP_TOKEN` is unset, the master key can
   be generated in the checkout and is not ignored by git, an open redirect
   after sign-in, a five-character minimum password, unhashed session tokens,
   no global security headers.

### Structure

- **Pages are string literals.** About 12,900 lines of HTML, CSS and
  JavaScript live inside Go strings in 34 files, with no syntax checking,
  no linting and no reuse between pages beyond copy and paste. Each page
  carries a light-theme stylesheet that the theme layer then overrides with
  `!important`. It works and it is consistent with itself, but every page
  change is made blind, and the two layers of styling are a standing source of
  surprises.
- **`internal/app` is the largest package** (18,400 lines) and holds both the
  composition root and nine unrelated pages. Shared services reach some of its
  handlers through package variables.
- **Hand-maintained lists.** Adding a page means editing the sidebar, two test
  lists, the help page, the home page, the API description, possibly the
  access list and the grantable-page list. Adding a table means two schema
  files, the sync order, a test list and possibly the knowledge store.
  DEVELOPMENT.md has the checklists. Tests catch some omissions, not all: a
  missing `protectedPrefixes` entry is silent.
- **Logic stated more than once.** The Exceptions page's rules are in
  JavaScript, in Go and in spreadsheet formulas. Only the last two are held
  together by a test.
- **Cross-module references are text.** Findings, exercises, mappings and
  policy sections refer to controls, NFRs and risks by id strings checked when
  read. Renaming or deleting a catalog entry leaves dangling references that
  only some modules report.

### Operations

- **Single process.** Lockout counters, Studio rooms and caches are in memory.
  Nothing prevents a second instance from being started against the same
  database, and it would misbehave quietly.
- **Long work runs inside a request.** A regulation analysis makes about one
  model call per article inside one HTTP request, with no progress output, and
  is cancelled when the connection closes. The shipped nginx file allows 300
  seconds. The same failure was fixed for AI Chat on 2026-10-04 by streaming.
  There is no job queue.
- **No CI.** The gate is `go test ./...` run by hand. Because the vulnerability
  scan is one of the tests and reads a live database, the suite can fail
  between two runs with no code change, as it did on 2026-10-10.
- **PostgreSQL is supported but not in the automated tests.** Every schema
  change is written twice and one copy is verified by hand.
- **Observability is the access log.** No metrics, no structured logs, no
  audit trail outside the modules that keep their own history (audit findings,
  Studio, policy versions).
- **The release path is incomplete.** The goreleaser scripts read a
  `.goreleaser.yaml` that is not in the repository.
- **`scripts/verify-install.sh` has not kept up.** It checks thirteen routes
  from the catalog and policy modules; the newer modules are not on its list.

### Product

- **Single-user by default.** The authentication service refuses a second
  account unless a code-level switch is turned off, and nothing in
  configuration turns it off. Features that assume several people (page
  grants, separate approver, user management) are built and tested but not
  reachable in a stock build.
- **The weight on an NFR is stored and shown, and used only by the Exceptions
  Workbook.** No report or score in the application uses it yet.
- **The Exceptions page does not store a selection.** An exception set exists
  as a URL or as a saved workbook. There is no record of who was granted which
  exception, for how long, or with what approval, which is what an exception
  register usually is.
- **The risk register is the simplest module.** No link to controls or
  findings beyond text, no history, no workflow. Its own document lists these
  as possible extensions.
- **Hard dependency on a second system.** Every AI feature and every document
  import needs the Wintermute server, which is outside this repository.
  Without it the application is a catalog, two registers, a policy editor and
  the reports.

## Inconsistencies between documents and code

Found during the review. The first group was corrected in the documents on
2026-10-10; the second is in code comments and was left.

Corrected:

- `RUNTIME_ARGS.md` did not list `-knowledge-token`, the master-key variables,
  the Wintermute fallbacks, the Chrome variables or `NFR_ENRICHMENT_MODEL`.
- `docs/DATABASE.md` said two modules are outside sync; audit findings make it
  three.
- `README.md` and `Agents.md` did not list several packages, and described
  storage as SQLite only.

Left as they are:

- The package comment of `internal/policydocs` says AI drafting and template
  rendering are "deliberately absent"; both exist now in other packages.
- The package comment of `internal/nfrenrich` says FTS5 is a build tag; it is
  compiled in unconditionally (`internal/app/fts5.go` says so).
- `POLICY_STUDIO.md` §4 draws the AI router reaching Claude directly as well
  as Wintermute; the application reaches Claude only through Wintermute.
  The file is a design record and its later sections say so.
- `AUDIT_FINDINGS.md` gives the snapshot version as 9, the version that added
  the module; the current format is 14.
- `Agents.md` and the release scripts name `.goreleaser.yaml`, which does not
  exist.
- `nist_stride_mappings` is created and synced but not read.
- `reg_coverage_sources` is created and backed up but no longer written.
- The repository contains a `users` table and `/users` routes from the
  original sample that nothing else uses.

## Questions the documents cannot answer

For the owner, or for someone with the code and a running instance:

1. Is multi-user operation intended? If so, what turns single-user mode off in
   a deployment?
2. Are the routes in SECURITY.md issue 1 meant to be public?
3. Is the committed `users.db` a fixture, and has its account's password been
   retired?
4. What should weights drive: a weighted exception score, report ordering,
   coverage figures?
5. Is PostgreSQL in use anywhere, or kept as an option?
6. What is the intended scale: how many users, documents and concurrent Studio
   sessions?
7. Which parts of the Wintermute server's behaviour does this application
   depend on that are not written down here (the framework detection, the
   segmenter, the agent tool names)?

## Suggested reading order for an analysis task

| Task | Read |
|---|---|
| Security review | SECURITY.md, HTTP_API.md, then `POLICY_STUDIO.md` §6 to §8 |
| Data protection or records question | DATA_MODEL.md, SECURITY.md "Where data leaves the process", `AI_AGENT.md` |
| Estimating a change | DEVELOPMENT.md "What a change has to touch", ARCHITECTURE.md "Packages" |
| Product or roadmap | FEATURES.md, this file's "Product" section, the top of `CHANGELOG.md` |
| Operating it | OPERATIONS.md, `RUNTIME_ARGS.md`, DATABASE.md |
| A specific module | Its section in FEATURES.md, then its own document at the repository root |
