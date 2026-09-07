# grc

Internal Go web application for RCSA (Risk and Control Self-Assessment) and
consulting practice management. It bundles a NIST control catalog, a security
NFR catalog with NFR-to-control linking, a security risk register, Jira
reporting/connector, an HTML/CSS-to-PDF GRC report generator, a lightweight
consulting CRM, and an AI chat assistant — all backed by SQLite and served via
Gin.

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
- `internal/crm` — consulting CRM: clients → engagements → billable time →
  billing
- `internal/jira` — Jira Cloud REST connector (issues/projects/search)
- `internal/db` — SQLite connection + schema setup
- `internal/crisisexercise` — risk and crisis scenario planning and exercises:
  the arc from red team through incident classification and its regulatory
  clocks to the board and the supervisor, with everything referenceable back to
  controls and frameworks (see [CRISIS_EXERCISE.md](CRISIS_EXERCISE.md))
- `internal/pageui` — shared side-nav tab rendering
- `internal/data` — embedded seed JSON for the control and NFR catalogs
- `internal/regmap` — regulation → NIST 800-53 crosswalk tool with human review
  gates, driven by `cmd/regmap` (see [REGMAP.md](REGMAP.md))
- `internal/knowledge` — read-only, machine-facing query surface over this
  installation's catalogs, for an AI agent to consult (see
  [AI_AGENT.md](AI_AGENT.md))
- `internal/regcoverage` — Regulation Coverage: upload an EU regulation, map
  every article to Security NFRs and 800-53, and get a versioned report you can
  question and revise (see
  [REGULATION_COVERAGE.md](REGULATION_COVERAGE.md))
- `scripts/` — goreleaser cross-build, smoke-test, and artifact-upload helpers

## Stack

Module `grc`, Go 1.25. Gin for HTTP, `modernc.org/sqlite` (pure Go, no cgo) for
storage, `golang.org/x/crypto/bcrypt` for password hashing.

## Docs

- [Agents.md](Agents.md) — agent instructions, conventions, security policy
- [RUNTIME_ARGS.md](RUNTIME_ARGS.md) — runtime flags, env vars, admin bootstrap
- [RISK_REGISTER_FRAMEWORK.md](RISK_REGISTER_FRAMEWORK.md) — risk register
  methodology
- [JIRA_CONNECTOR.md](JIRA_CONNECTOR.md) — Jira integration methodology
- [REGMAP.md](REGMAP.md) — `regmap` regulation → NIST 800-53 crosswalk CLI,
  its review gates, and how to add a framework profile
- [AI_AGENT.md](AI_AGENT.md) — how questions asked here get answered from this
  installation's own data, via an agent on your Wintermute server
- [REGULATION_COVERAGE.md](REGULATION_COVERAGE.md) — the Regulation Coverage
  web module: the analysis loop, how a mapping is checked, cost and limits
- [FAQ.md](FAQ.md) — `security-patches` → `main` merge procedure
- [CHANGELOG.md](CHANGELOG.md) — change history / rollback reference
