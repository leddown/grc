# grc — Agent Instructions & Policy

Internal Go web application for RCSA (Risk and Control Self-Assessment): a
NIST control catalog, security NFR catalog, NFR-to-control linking, a
security risk register, Jira reporting/connector, and an AI chat assistant,
all backed by SQLite and served via Gin.

## Build & Test

- Build: `go build ./...` (entrypoints `./cmd/api` and root `main.go` are
  functionally identical — both just call `app.BindFlags()` + `app.Run()`)
- Run tests: `go test ./...` — this also runs `TestGosec` and
  `TestGovulncheck` (in `security_test.go`), so a clean `go test ./...` is a
  security gate, not just a unit-test gate
- Format & vet: `go fmt ./...` && `go vet ./...` — run both before
  considering any change done
- There is no CI pipeline configured (no `.github/workflows`) — the above
  commands must be run locally before every commit

## Running the app

- Normal mode (login required): `go run ./cmd/api` or `./grc`. See
  `RUNTIME_ARGS.md` for flags/env vars and first-boot admin bootstrap
  (`./admin_setup_login.sh`).
- Local/no-login laptop mode (single user, every page and admin action
  open, no auth at all): `LOCAL_MODE=true ./grc` or `./run_local.sh`.
  Never run `-local-mode` on a server or anywhere reachable by other users.

## Architecture Overview

- `main.go` / `cmd/api/main.go` — entrypoints
- `internal/app` — Gin route wiring (`app.go`), page HTML handlers,
  auth/admin middleware (`auth.go`, `access_control.go`), login and
  user-management pages
- `internal/authn` — password/session auth service: bcrypt hashing,
  sessions, per-user `allowed_pages`
- `internal/user` — basic user CRUD, distinct from `internal/authn`'s auth
  users
- `internal/controlcatalog`, `internal/securitynfr`, `internal/nfrlink` —
  NIST control catalog, security NFR catalog, and their cross-linking
- `internal/riskregister`, `internal/reports` — risk register and
  reporting dashboards
- `internal/jira` — Jira Cloud REST connector (issues/projects/search)
- `internal/regcoverage` — Regulation Coverage: uploads an EU regulation,
  maps every article to Security NFRs and 800-53 with AI, and keeps a
  versioned report that can be questioned and revised. Reuses
  `internal/regmap`'s extraction, segmentation and framework profiles rather
  than duplicating them — see `REGULATION_COVERAGE.md`
- `internal/crisisexercise` — Risk & Crisis Exercises: plans, runs and reports
  the exercise arc from red team through incident response, DORA incident
  classification and its notification clocks, crisis and continuity activation,
  communications, the board and the supervisory authorities. Every objective,
  phase, inject, decision and finding can cite the controls, NFRs, regulation
  clauses, risks and frameworks it exercises — see `CRISIS_EXERCISE.md`
- `internal/knowledge` — the read-only `/api/knowledge` surface an external AI
  agent queries: NFRs, controls, regulation coverage, policies, risks and crisis
  exercises, behind its own read-only token. The agent itself lives in wintermuted, not here —
  see `AI_AGENT.md`
- `internal/db` — SQLite connection + schema setup
- `internal/pageui` — shared side-nav tab rendering used across pages
- `internal/data` — embedded/static seed JSON for the control and NFR
  catalogs
- `scripts/` — goreleaser cross-build, smoke-test, and artifact-upload
  helpers
- Root helper scripts: `admin_setup_login.sh` (first-boot bootstrap),
  `run_local.sh` (local no-auth launch)

## Conventions & Code Style

- Module `grc`, Go 1.25 (see `go.mod` for the exact toolchain version)
  — don't downgrade
- Gin for HTTP, `modernc.org/sqlite` for storage (pure Go — the module has no
  cgo, and must not gain any: that is what keeps a build from needing a C
  toolchain per target), `golang.org/x/crypto/bcrypt` for password hashing —
  prefer these existing deps over adding new ones
- Wrap errors with `fmt.Errorf("...: %w", err)`
- No comments unless they explain a non-obvious WHY; avoid speculative
  abstractions or unrequested refactors
- Page handlers render HTML directly as Go string literals (no template
  engine) — match the existing style in `internal/app/*.go` rather than
  introducing a templating library

## Git / Branch / PR Workflow

- This repo currently commits directly to `master`; no enforced
  branch-naming or PR process exists today, but prefer a short-lived
  feature branch + merge for any non-trivial change if asked to use one
- Always update `CHANGELOG.md` for every substantive code, test, security,
  UX, or build change — do not leave changelog maintenance for later (see
  existing entries for the expected level of detail)

## Security & Sensitive Data

- Don't commit secrets (`SETUP_TOKEN`, `ADMIN_TOKEN`, DB files) — `users.db`,
  `local.db`, and their `-shm`/`-wal` files are gitignored; keep it that way
- Use HTTPS in production so auth cookies are sent with `Secure`
- Agents must check for security patches for all internal libraries/modules
  before finalizing changes
- A full security patch audit across all internal libraries and modules
  should run periodically; no automated cadence exists yet, so treat this
  as a manual review duty when working in this repo
- If a vulnerability is found and a patch is available, apply it on a
  `security-patches` branch and add a `CHANGELOG.md` entry noting the
  impacted library/module and remediation status; after patching, run
  `go test ./...` and update `CHANGELOG.md` to confirm remediation
- See `FAQ.md` / `RUNTIME_ARGS.md` for the `security-patches` → `master`
  merge procedure

## CI / Deployment

- No CI pipeline exists yet (no `.github/workflows`). Until one exists,
  treat `go fmt ./...`, `go vet ./...`, and `go test ./...` as mandatory
  local pre-commit checks
- Release artifacts are built via `goreleaser` (`.goreleaser.yaml`) for
  linux/windows/darwin amd64; see `scripts/build-goreleaser-binaries.sh`,
  `scripts/smoke-test-goreleaser-binaries.sh`, and
  `scripts/push-goreleaser-binaries-to-gdrive.sh`
- There is no Docker/Kubernetes deployment path in this repo today
