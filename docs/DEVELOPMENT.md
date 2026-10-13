# Development

How the code is written and tested, and what a change has to touch. Checked
against the repository on 2026-10-10. `Agents.md` is the authoritative
instruction file for anyone, human or agent, changing the code; this file
explains the same rules and adds what a reviewer or analyst needs.

## Ground rules

- **Go 1.26, module `grc`, no cgo.** The SQLite driver is pure Go so that a
  build for any target needs no C toolchain. A dependency that needs cgo is
  not acceptable.
- **Prefer what is already there** over a new dependency: Gin, `db.Conn`,
  bcrypt, the in-repository spreadsheet writer, the BM25 ranker in
  `internal/nfrenrich`.
- **Pages are Go string literals.** No template engine, no front-end
  framework. A new page follows the neighbouring pages.
- **Errors are wrapped** with `fmt.Errorf("...: %w", err)`.
- **Comments explain why, not what.** The codebase's comments are long where
  a decision is not obvious and absent elsewhere.
- **No document ingestion.** The application does not accept uploads or parse
  PDF, DOCX or images. Documents go to an agent's library on the Wintermute
  server; `internal/aiprovider/library.go` reads the text back.
- **AI never writes.** Model output becomes a proposal a person decides, with
  the model and a prompt hash recorded.
- **Every substantive change gets a `CHANGELOG.md` entry**, newest first, in
  the detail the existing entries have: what changed, why, files, tests, and
  how it was verified, including what was not.

## The gate

```sh
go fmt ./...
go vet ./...
go test ./...
```

All three before any commit. There is no CI; nothing else enforces it.

`go test ./...` is also the security gate: `security_test.go` in the root
package runs `gosec` and `govulncheck` over the module (both pinned in
`go.mod` through `tools.go`). `govulncheck` reads its database over the
network, so the root package's tests need network access and can start
failing without a code change when an advisory is published.

`go test -short ./...` skips the two security scans, the browser tests and
the LibreOffice test. It is for the inner loop, not for a commit.

## Tests

570 test functions; about 22,000 lines.

| Kind | Where | Needs |
|---|---|---|
| Unit and service tests | Every package, beside the code. Most open a temporary SQLite database. | Nothing |
| HTTP tests | `internal/app/*_test.go` and module handler tests, with `httptest`. Navigation tests render every listed page and check the sidebar. | Nothing |
| Route-table tests | Walk the router and assert every route under `/collab/` and `/shared/` passes its gate. | Nothing |
| Browser tests | `internal/app/*_e2e_test.go` (four files) drive the Policy Studio in headless Chrome through `chromedp`: co-editing, suggestions, lists, templates, guests. | Chrome on `PATH`; skipped otherwise and in `-short` |
| Workbook test | `internal/exceptionbook`: LibreOffice recalculates the Exceptions Workbook and every cell is compared with the Go statement of the logic. | LibreOffice 7.2 or later; skipped otherwise and in `-short` |
| Security scans | `security_test.go` | Network |
| Theme test | `ui_background_test.go`: no page stylesheet may hard-code a white or near-white background, because every theme is dark. Printed output is exempt by name. | Nothing |
| Bundle and schema tests | The embedded editor files must match their manifest; the Go schema must match `web/policy-studio/schema.snapshot.json`; fixtures shared between Go and JavaScript must project identically. | Nothing |
| `-tags live` | `internal/policyai/live_test.go`: one real call to a Wintermute server. | A configured server |
| `-tags pdf_smoke` | `internal/reporting/smoke_test.go`: one real PDF through Chrome. | Chrome |
| PostgreSQL | The same repositories run on PostgreSQL, but the suite runs on SQLite. PostgreSQL paths are verified by hand. | A PostgreSQL server |

Tests that need a database create one in `t.TempDir()`. When a server is
started by hand to check something, it gets a throwaway port and database and
is stopped and cleaned up afterwards.

## What a change has to touch

The application has several hand-maintained lists. A change that misses one
usually passes the compiler and fails a test, or shows up as a page that is
unreachable or unlisted.

### A new page

| Place | Why |
|---|---|
| The route, in the module's `RegisterRoutes` or `internal/app/app.go` | |
| `protectedPrefixes` in `internal/app/access_control.go`, if the path is a new top-level prefix | Otherwise the page and its data routes are public. |
| `userManagementAllowedPages` in `internal/app/user_management.go` | If it should be grantable to a non-admin on its own. A listed page is no longer covered by its parent's grant. |
| `navGroups` in `internal/pageui/nav.go` | The sidebar. |
| `fullNavLinks` and `navigationTestPagePaths` in `internal/app/navigation_test.go` | The tests assert both. |
| The page table and the workflow text in `internal/app/help.go`; the lists in `internal/app/home.go` | |
| `internal/app/api_docs.go` | For a JSON route worth documenting. |
| `scripts/verify-install.sh` | It checks that deployed routes exist. |

### A new table or column

| Place | Why |
|---|---|
| `internal/db/sqlite.go`: the `CREATE TABLE`, and an `ensureColumn` for a column added to an existing table | Existing databases are upgraded on start. |
| `internal/db/postgres.go`: the same, with `ADD COLUMN IF NOT EXISTS` | Both dialects, every time. |
| `syncOrder` in `internal/dbsync/dbsync.go` | Otherwise the table or column is silently missing from sync and from the JSON snapshot. New tables have each come with a new `SnapshotVersion` and a note in `transfer.go` on what an older backup restores without. A column an older backup lacks takes its schema default on import. |
| `internal/db/sqlite_test.go` | It lists the expected tables. |
| `internal/knowledge/store.go` | If an AI agent should see the data. |
| The module's export and report code | A new field that matters should appear in exports. |

### A change to the Exceptions page logic

The logic exists three times: JavaScript in `internal/app/exceptions.go`, Go
in `internal/exceptionbook` (`evaluate`), and spreadsheet formulas in the same
package. Change all three. The LibreOffice test holds the last two together.
`TestPageFiguresAreTheWorkbooksUnderNode` holds the first to the second for
the weights: it runs the pages' script under node on the seeded catalog and
compares each line with `exceptionbook.Evaluate`. It is skipped where node is
not installed, and it does not cover the counts of linked controls, which the
pages work out in their own functions.

### A change to the Studio editor

Edit `web/policy-studio/src`, rebuild with `scripts/build-policy-studio.sh`,
and commit the rebuilt assets, `THIRD_PARTY_NOTICES.md`,
`schema.snapshot.json` and the fixtures together. The build applies a patch to
`@tiptap/y-tiptap` that refuses any version but the one it was written for.

### A dependency or security update

Vulnerability fixes are made on a `security-patches` branch, recorded in
`CHANGELOG.md` with the affected library and the remediation status, tested
with `go test ./...`, and merged with `git merge --no-ff` as `FAQ.md`
describes. For the editor: `npm audit --omit=dev` in `web/policy-studio`, an
updated lockfile and a rebuilt bundle.

## Conventions worth knowing before reading the code

- **Module layout:** `model.go`, `repository.go`, `service.go`, `handler.go`,
  sometimes `pages.go`. Services take a repository interface; tests use the
  SQLite one, and a few modules have an in-memory one.
- **Repositories are called `SQLiteRepository`** and run on PostgreSQL too.
  Variables named `sqliteDB` hold a `*db.Conn` of either dialect.
- **Read routes on the router, write routes on an admin group**, in a
  `register<Module>Routes` function in `internal/app/app.go` that skips the
  admin middleware in local mode.
- **Validation errors** are a typed error per module (`invalid(...)`,
  `IsValidation`) mapped to 400; not-found to 404; state conflicts to 409.
- **Identity** reaches a module as a `func(*gin.Context) string`.
- **Page CSS** must not hard-code light backgrounds; use the theme tokens
  (`--bg`, `--surface-2`, `--text`, `--muted`, `--accent`, `--on-accent`,
  `--border`). Paper output is exempt.
- **Page JavaScript** escapes everything it inserts with the page's `esc()`.

## Branches and history

Work is committed directly to `main`; a short-lived branch and a merge are
used for larger changes and for `security-patches`. The history is short (61
commits since 2026-08-12) and many commits are large; `CHANGELOG.md` is the
real record of what changed and why.

## Working with a coding agent

`CLAUDE.md` loads `Agents.md` and adds three session rules: track multi-step
work with a task list, use a throwaway port and database for manual checks and
clean up afterwards, and when `go build` fails in `main.go`, look for stray
trailing characters before assuming a real error.
