# grc

Internal Go web application for governance, risk and compliance work and
consulting practice management. It bundles a NIST control catalog, a security
NFR catalog with NFR-to-control linking, a security risk register, policy
authoring (including the collaborative Policy Studio), Jira reporting/connector,
an HTML/CSS-to-PDF GRC report generator, and an AI chat assistant — all backed
by SQLite (or PostgreSQL) and served via Gin. The consulting CRM moved to
wintermute.

## Quick start

```sh
# Build
go build ./...

# Run (login required) — see RUNTIME_ARGS.md for flags, env vars,
# and first-boot admin bootstrap (./admin_setup_login.sh)
go run ./cmd/api

# Local/no-login laptop mode (single user, all pages/admin open, no auth):
LOCAL_MODE=true ./grc    # or: ./run_local.sh
```

> Never run local/no-login mode on a server or anywhere reachable by other
> users.

The entrypoints `./cmd/api` and root `main.go` are functionally identical —
both call `app.BindFlags()` + `app.Run()`.

## Build & test gate

There is no CI pipeline; run these locally before every commit:

```sh
go fmt ./...
go vet ./...
go test ./...   # also runs TestGosec + TestGovulncheck (security gate)
```

A clean `go test ./...` is a security gate, not just a unit-test gate.

## Architecture

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) describes the process, every
package and how a request is served. In short:

- `main.go` / `cmd/api/main.go` — entrypoints
- `internal/app` — Gin route wiring, page HTML handlers, auth/admin middleware,
  login and user-management pages
- `internal/authn` — password/session auth: bcrypt hashing, sessions, per-user
  `allowed_pages`
- `internal/user` — basic user CRUD (distinct from `internal/authn`'s auth users)
- `internal/controlcatalog`, `internal/securitynfr`, `internal/nfrlink` — NIST
  control catalog, security NFR catalog, and their cross-linking
- `internal/riskregister`, `internal/reports` — risk register and reporting
  dashboards
- `internal/reporting` — HTML/CSS-to-PDF GRC report generation (see its
  [README](internal/reporting/README.md))
- `internal/exceptionbook`, `internal/xlsx` — the Exceptions Workbook: the
  Exceptions page as a working .xlsx file (catalog, weights, controls, links
  and the formulas that redo the page's logic), and the small writer behind it
- `internal/jira` — Jira Cloud REST connector (issues/projects/search)
- `internal/db` — the schema and the connection, for SQLite and PostgreSQL;
  `internal/dbsync` copies data between two databases and writes the JSON
  snapshot
- `internal/auditfinding` — Audit Findings & Remediation (see
  [AUDIT_FINDINGS.md](AUDIT_FINDINGS.md))
- `internal/policydocs` — the policy library: documents, sections, control
  mappings, lint, approval, versions and the coverage matrix; also letters
  written on a client's behalf (see [LETTERS.md](LETTERS.md))
- `internal/doctemplate` — typeset output (Typst, LaTeX, Word through Pandoc)
  for policies, letters and reports (see
  [DOCUMENT_TEMPLATES.md](DOCUMENT_TEMPLATES.md))
- `internal/nfrenrich` — NFR Enrichment: propose catalog text from a security
  document in the agent's library, for a person to accept or reject
- `internal/aiprovider` — the one way out to a model: the Wintermute client,
  usage logging, and the client for the agent's document library
- `internal/settings`, `internal/secrets` — settings an admin changes in the
  UI, and the master key that encrypts stored credentials
- `internal/crisisexercise` — risk and crisis scenario planning and exercises:
  the arc from red team through incident classification and its regulatory
  clocks to the board and the supervisor, with everything referenceable back to
  controls and frameworks (see [CRISIS_EXERCISE.md](CRISIS_EXERCISE.md))
- `internal/pageui` — shared side-nav tab rendering
- `internal/data` — embedded seed JSON for the control and NFR catalogs
- `internal/regmap` — the framework profiles that cut a regulation into its
  articles, and the segmenter that applies them; used by Regulation Coverage
- `internal/knowledge` — read-only, machine-facing query surface over this
  installation's catalogs, for an AI agent to consult (see
  [AI_AGENT.md](AI_AGENT.md))
- `internal/policystudio` — the Policy Studio: collaborative WYSIWYG editing of
  policy documents over an in-process Yjs server (ygo), projected into the
  policy module's section rows, with tracked suggestions, anchored comments,
  provenance, guest links for working with a client live, and workshop mode
  (see [POLICY_STUDIO.md](POLICY_STUDIO.md))
- `internal/policyai` — the Studio's AI proposals: an edit the AI proposes is
  validated on the server and placed as a suggestion a person accepts or
  rejects; it never writes the document itself
- `internal/clientprofile` — the clients policies are written for and the facts
  about each, which fill a document's fact tokens (a model or a template never
  supplies them)
- `web/policy-studio` — the Studio's editor sources (Tiptap), built with Node
  into a committed, embedded bundle by `scripts/build-policy-studio.sh`; Node is
  never needed to build or run the app
- `internal/regcoverage` — Regulation Coverage: import an EU regulation from
  the AI agent's library, map every article to Security NFRs and 800-53, and
  get a versioned report you can question and revise (see
  [REGULATION_COVERAGE.md](REGULATION_COVERAGE.md))
- `scripts/` — goreleaser cross-build, smoke-test, and artifact-upload helpers

## Stack

Module `grc`, Go 1.26. Gin for HTTP, `modernc.org/sqlite` (pure Go, no cgo) for
storage, `golang.org/x/crypto/bcrypt` for password hashing.

## Docs

The `docs/` directory is the reference set, written to be read without the
code (it is what to give an AI assistant asked to analyse the project):

- [docs/PROJECT_OVERVIEW.md](docs/PROJECT_OVERVIEW.md) — start here: what the
  application is, a glossary, and which document answers which question
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — the process, the packages,
  the request path, storage, AI, collaboration, rendering
- [docs/FEATURES.md](docs/FEATURES.md) — every module and page, and its rules
- [docs/HTTP_API.md](docs/HTTP_API.md) — every route and who can call it
- [docs/DATA_MODEL.md](docs/DATA_MODEL.md) — every table and how it travels
- [docs/SECURITY.md](docs/SECURITY.md) — the security model and open issues
- [docs/OPERATIONS.md](docs/OPERATIONS.md) — configure, run, deploy, back up
- [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) — conventions, tests, and what a
  change has to touch
- [docs/ANALYSIS_NOTES.md](docs/ANALYSIS_NOTES.md) — an assessment: strengths,
  risks, inconsistencies, open questions
- [docs/DATABASE.md](docs/DATABASE.md) — SQLite and PostgreSQL, switching and
  syncing

Module and operating documents:

- [Agents.md](Agents.md) — agent instructions, conventions, security policy
- [RUNTIME_ARGS.md](RUNTIME_ARGS.md) — runtime flags, env vars, admin bootstrap
- [RISK_REGISTER_FRAMEWORK.md](RISK_REGISTER_FRAMEWORK.md) — risk register
  methodology
- [JIRA_CONNECTOR.md](JIRA_CONNECTOR.md) — Jira integration methodology
- [AI_AGENT.md](AI_AGENT.md) — how questions asked here get answered from this
  installation's own data, via an agent on your Wintermute server
- [REGULATION_COVERAGE.md](REGULATION_COVERAGE.md) — the Regulation Coverage
  web module: the analysis loop, how a mapping is checked, cost and limits
- [POLICY_STUDIO.md](POLICY_STUDIO.md) — the Policy Studio: design, decisions,
  operator guide
- [POLICY_MODULE_FRAMEWORK.md](POLICY_MODULE_FRAMEWORK.md) — the research
  behind policy authoring and AI-assisted drafting
- [DOCUMENT_TEMPLATES.md](DOCUMENT_TEMPLATES.md) — the Typst and LaTeX
  templates and the JSON they render
- [LETTERS.md](LETTERS.md) — letters on a client's behalf, such as to an ECB
  Joint Supervisory Team: writing, AI, output, and the research behind them
- [CRISIS_EXERCISE.md](CRISIS_EXERCISE.md) — Risk & Crisis Exercises
- [AUDIT_FINDINGS.md](AUDIT_FINDINGS.md) — Audit Findings & Remediation
- [FAQ.md](FAQ.md) — `security-patches` → `main` merge procedure
- [CHANGELOG.md](CHANGELOG.md) — change history / rollback reference
