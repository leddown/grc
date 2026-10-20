# Operations

Configuring, running, deploying, updating, backing up and releasing the
application. Checked against the code and scripts on 2026-10-10.
`RUNTIME_ARGS.md` has the long form of every flag and the first-boot
walkthrough; `DATABASE.md` covers switching and syncing databases.

## Building

```sh
go build -o grc ./cmd/api        # or: go build ./...
```

Needs Go 1.26.9 or later. With the default `GOTOOLCHAIN=auto` the `go`
command fetches that toolchain itself; a host that pins `GOTOOLCHAIN=local`
or uses a distribution's older Go must be updated first. No C compiler, no
Node: the editor bundle is committed.

The install scripts stamp the commit into the binary:

```sh
go build -ldflags "-X grc/internal/app.BuildVersion=$(git rev-parse --short HEAD)" -o grc ./cmd/api
```

`GET /version` then reports it, which is how a deploy is checked against the
checkout.

## Running

| Way | Command | Result |
|---|---|---|
| Server | `./grc` or `go run ./cmd/api` | Sign-in required. SQLite at `users.db`, port 8080, unless configured. |
| Laptop, no accounts | `./run_local.sh` or `LOCAL_MODE=true ./grc` | Everything open. `run_local.sh` uses `local.db`. |
| Chosen database profile | `scripts/run-app.sh` | Runs the local SQLite or remote PostgreSQL profile picked with `scripts/db-switch.sh`. |
| One-shot sync | `./grc -sync-to <target>` or `-sync-from <source>` | Copies data and exits. |

First start on a server: set `SETUP_TOKEN`, start, create the admin with
`POST /auth/bootstrap-admin`, then remove the token. `admin_setup_login.sh`
does those steps; `scripts/setup.sh` does them as part of an install.

## Configuration

A flag overrides its environment variable.

### Flags and their variables

| Flag | Variable | Default | Purpose |
|---|---|---|---|
| `-listen-addr` | `LISTEN_ADDR` | `:8080` | HTTP bind address. |
| `-sqlite-path` | `SQLITE_PATH` | `users.db` | SQLite file. Ignored when a database URL is set. |
| `-database-url` | `DATABASE_URL` | empty | PostgreSQL connection string. Selects PostgreSQL. |
| `-local-mode` | `LOCAL_MODE` | false | No accounts; everything open. |
| `-admin-token` | `ADMIN_TOKEN` | empty | Break-glass admin bearer token. |
| `-knowledge-token` | `KNOWLEDGE_TOKEN` | empty | Read-only token for `/api/knowledge`. Without it the API is off outside local mode. |
| `-allow-json-save` | `ALLOW_JSON_SAVE` | false | Registers `/controls/save`, `/security-nfrs/save` and `/jira/json/export`. |
| `-trust-proxy` | `TRUST_PROXY` | false | Trust `X-Forwarded-Proto` when deciding whether cookies are `Secure`. Set it behind a proxy that sets the header. |
| `-docs-dir` | `DOCS_DIR` | search | Where `CHANGELOG.md` and the other served markdown files are. |
| `-templates-dir` | `TEMPLATES_DIR` | search | Where the Typst and LaTeX templates are. |
| `-render-url` | `RENDER_URL` | empty | A grc-render host that runs Typst and Pandoc for this installation (`DOCUMENT_TEMPLATES.md`, "Rendering on another host"). Empty renders here. |
| `-render-token` | `RENDER_TOKEN` | empty | grc-render's token. Required with `-render-url`. |
| `-render-ca-file` | `RENDER_CA_FILE` | empty | CA certificate (PEM) that issued grc-render's certificate, for an `https` render URL. |
| `-studio-allowed-origins` | `STUDIO_ALLOWED_ORIGINS` | empty | Extra origins allowed to open the collaboration socket. |
| `-studio-snapshot-retention` | `STUDIO_SNAPSHOT_RETENTION` | 50 | Snapshots kept per Studio document. |
| `-sync-to`, `-sync-from` | `SYNC_TO`, `SYNC_FROM` | empty | One-shot database copy; mutually exclusive. |

### Variables with no flag

| Variable | Purpose |
|---|---|
| `SETUP_TOKEN` | Required in `X-Setup-Token` to create the first admin, when set. |
| `GRC_SECRET_KEY` | The master key for stored credentials, base64, 32 bytes. |
| `GRC_SECRET_KEY_FILE` | Path of the master key file, overriding the search. |
| `STATE_DIRECTORY` | Set by systemd from `StateDirectory=`. The key file goes there. |
| `WINTERMUTE_URL`, `WINTERMUTE_TOKEN`, `WINTERMUTE_BACKEND`, `WINTERMUTE_MODEL`, `WINTERMUTE_AGENT` | Fallbacks for the values the Settings page stores. A stored value wins. |
| `NFR_ENRICHMENT_MODEL` | A model name for NFR Enrichment's analysis. |
| `REPORTING_CHROME_PATH`, `REPORTING_CHROME_NO_SANDBOX` | Chrome's location and sandbox setting for HTML-to-PDF reports. |
| `TYPST`, `PANDOC`, `TECTONIC`, `LATEXMK` | Paths of the typesetting programs, overriding the `PATH` lookup. |

### Set in the application, stored in the database

Settings page (admin): the Wintermute server URL, client token, backend,
model and agent; the Crisis Exercise agent and the Policy Studio agent with
their "send the record" switches; whether Policy Studio guest links may be
created and the default invitation length. Family visibility, template brand
and user accounts are also data.

Because these live in the database, a value set while running against
`local.db` is absent when the service runs against `users.db`. The Settings
page names the database it is writing to.

## Optional programs on the host

None is required to start. Each enables one kind of output.

| Program | Enables | Without it |
|---|---|---|
| Chrome or Chromium | Regulation report PDF, crisis after-action PDF, the two sample report PDFs | Those renders fail. |
| `typst` | Policy, report and redline PDFs | The render returns a zip of sources and a build script. |
| `tectonic`, or `latexmk` with XeLaTeX | The LaTeX policy PDF | As above. |
| `pandoc` 2.15 or later | Word export of a policy | As above. |
| LibreOffice | Only a test: the Exceptions Workbook check | That test is skipped. |

A reachable Wintermute server is needed for every AI feature and for
importing documents. Without one, the catalogs, registers, policies, exports
and reports still work.

## Installing on a server

`scripts/setup.sh` is a first-time bare-metal install and is safe to re-run.
It checks prerequisites, creates the data directory (`/var/lib/grc`) and a
service user, writes `/etc/grc/grc.env` (mode 0600) with generated admin and
setup tokens, builds and installs `/usr/local/bin/grc`, installs the
repository's root markdown files to `/usr/local/share/grc`, creates the first
admin with a generated password, and installs and starts the systemd unit. It
prints the generated secrets once. Options: `INSTALL_TYPST=1`,
`INSTALL_CHROME=1`, and PostgreSQL provisioning through `PG_ADMIN_URL`.

`deploy/grc.service` runs the binary as its own user with
`ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `NoNewPrivileges`, a
`StateDirectory`, and only the capability to bind a low port.

`deploy/grc.nginx` terminates TLS and proxies to `127.0.0.1:8081`. It has a
separate location for `/collab/` with the WebSocket upgrade headers and a
one-hour read timeout; other locations have 300 seconds. A commented server
block shows a second hostname that exposes only `/shared/` for guests.

Behind the proxy set `LISTEN_ADDR=127.0.0.1:8081` and `TRUST_PROXY=true`. If
guests or users reach the application under a second hostname, add it to
`STUDIO_ALLOWED_ORIGINS`.

## Updating

`update.sh` on the host: fetch and reset the checkout to `origin/main`,
rebuild with the commit stamped in, reinstall the markdown files, restart the
service, and run the verification. Schema changes apply on start; there is no
migration step.

The reset means the host always runs `origin/main`, even after its history has
been rewritten and force-pushed. Edits to tracked files on the host are
discarded (the script lists them first); untracked and ignored files such as
the database, the key files and `config/db.env` are kept. Make changes in a
development checkout and push them, not on the host.

`scripts/verify-install.sh` checks that the running service reports the
checkout's commit, that the routes the source registers are served, that the
schema has the current tables, and warns about missing optional programs. It
exists because a stale binary still answers `/health` with 200.

## Accounts

- In the application: User Management, as an admin.
- Offline: `scripts/manage-user.sh` (`list`, `create`, `passwd`, `set-admin`,
  `delete`), which runs `cmd/userctl` against the database directly and works
  while the service is up. Passwords are prompted or generated, never passed
  as arguments. `--pages` limits a non-admin to named pages.
- Single-user mode is the default: a second account is refused. If the one
  account is not an admin, use `set-admin`.

## Backup, restore and moving data

| Need | Use |
|---|---|
| A complete copy of a SQLite installation | Utilities → Backup (a `VACUUM INTO` copy). Also keep the master key file; it is not in the copy. |
| A PostgreSQL installation | `pg_dump`. The in-app raw backup is SQLite only. |
| A portable copy of the data, or moving between installations | Utilities → Export, then Import on the other side. One JSON file; import replaces everything in one transaction. Leaves out settings, stored credentials, sessions and guest links. |
| Merging catalogs and policies from a laptop into a server | `scripts/db-sync.sh push` (or `-sync-to`). Merges by key. Skips regulation reports, exercises, audit findings and live Studio state; move those with Export / Import. |
| Checking a database | Utilities → Integrity check. |
| Giving an AI agent the data as a file | Utilities → Export for the AI agent. |

After a restore onto another server, set the Wintermute connection again and
re-create guest links.

## Logs and monitoring

The process logs to standard output: Gin's access line per request and the
application's own messages (which master key source was used, whether the
knowledge API is on, sync progress). Under systemd that is the journal. There
are no metrics, no tracing and no structured log format.

`GET /health` and `GET /version` need no session. AI spend is in
`ai_usage_log` and on the AI Chat page.

## Releasing

`scripts/build-windows-cross.sh` builds a Windows binary with `GOOS=windows`.

`scripts/build-goreleaser-binaries.sh`, `smoke-test-goreleaser-binaries.sh`
and `push-goreleaser-binaries-to-gdrive.sh` build, smoke-test and upload
Linux, Windows and macOS binaries with goreleaser. They read
`.goreleaser.yaml` from the repository root, and that file is not in the
repository, so this path does not run from a clean checkout.

There is no CI, no container image and no Kubernetes manifest.

## Editor bundle

The Policy Studio editor is built from `web/policy-studio` into
`internal/policystudio/assets` with `scripts/build-policy-studio.sh` (Node 24,
`npm ci --ignore-scripts`). The result is committed. The build refuses a
dependency whose licence is not on the allowlist and a bundle over 800 KB.
`--check` rebuilds into a scratch copy and fails if it differs from what is
committed.

## Known operating constraints

- One process per database. Lockout counters, Studio rooms and caches are in
  memory.
- A regulation analysis runs inside its HTTP request, sends nothing until it
  finishes, and is cancelled when the connection closes. It can take many
  minutes; `deploy/grc.nginx` gives a request 300 seconds. AI Chat and the
  Studio's AI answers avoid this by streaming; the analysis does not.
- The first LaTeX render with Tectonic downloads packages and needs network
  access.
- The Change Log and `/knowledge/*` pages need the markdown files installed
  where the binary can find them, because a service's working directory is
  `/`.
