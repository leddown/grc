# Runtime arguments

The API binary accepts command-line flags for runtime behavior.

## Available flags

- `-listen-addr`  
  HTTP bind address.  
  Default: `:8080`

- `-sqlite-path`  
  Path to SQLite database file (ignored when `-database-url` is set).  
  Default: `users.db`

- `-database-url`  
  PostgreSQL connection string (e.g.
  `postgres://user:pass@host:5432/db?sslmode=require`). When set, the app uses
  an external PostgreSQL backend instead of SQLite; the schema is created
  automatically on startup, the same as the SQLite path.  
  Default: empty (use SQLite)

- `-allow-json-save`  
  Enables writeback endpoints (admin-authenticated):
  - `POST /controls/save`
  - `POST /security-nfrs/save`
  - `POST /jira/json/export`
  
  Default: `false`

- `-admin-token`  
  Optional break-glass admin bearer token for privileged endpoints.  
  Default: empty (disabled unless set)

- `-local-mode`  
  Disables login and user management entirely: no `/login` page, no
  `/admin/user-management` page, no `/auth/*` routes, and every page/admin
  action is open with no session or token required. Intended for running
  the app locally on a single-user laptop, not for shared/networked use.  
  Default: `false`

- `-sync-to`  
  One-shot database sync: copy the active backend (the one selected by
  `-database-url`/`-sqlite-path`) **up to** the given target, then exit
  without serving. The target is a `postgres://` URL or a SQLite file path
  (engine auto-detected). Upsert/merge semantics. Mutually exclusive with
  `-sync-from`. See `docs/DATABASE.md`.  
  Default: empty (serve normally)

- `-sync-from`  
  One-shot database sync: copy **from** the given source into the active
  backend, then exit. Same spec format and semantics as `-sync-to`.  
  Default: empty (serve normally)

- `-trust-proxy`  
  Trusts the `X-Forwarded-Proto` header from the immediate connection when
  deciding whether auth/session cookies should be marked `Secure`. Only
  enable this when the app sits behind a reverse proxy that sets/overwrites
  this header itself — otherwise it's spoofable by any client and should
  stay disabled (TLS termination is then detected directly via the
  connection instead).  
  Default: `false`

- `-docs-dir`  
  Directory holding `CHANGELOG.md` and the other repository markdown files
  served by the **Change Log** page and `/knowledge/*`. Those are read from
  disk, so they have to be findable: when this is unset the app searches the
  working directory, the directory holding the binary, and
  `<bindir>/../share/carelockconsulting` (plus `/usr/local/share/` and
  `/usr/share/carelockconsulting`) — `scripts/setup.sh` and `update.sh`
  install them into the first of those. Set this explicitly only if the docs
  live somewhere else; a path given here is used as-is, so a wrong one is
  reported on the page rather than silently falling back to another copy.
  Note that a systemd service runs with `/` as its working directory, which
  is why an installed copy (or this flag) is required on a server.  
  Default: empty (search)

- `-templates-dir`  
  Directory holding the Typst/LaTeX document templates rendered by the
  **Templates** page (`templates/typst/*.typ`, `templates/latex/*`,
  `templates/samples/*.json`). Same reasoning and same search behaviour as
  `-docs-dir`: unset means search `./templates`, the working directory, the
  binary's directory and the share directories, identified by the presence of
  `typst/brand.typ`; a path given here is used as-is. Without a resolvable
  directory the Templates page says so and no document can be rendered.  
  Default: empty (search)

### Assistant, tasks and Google Calendar — moved

The assistant, the task module and its Google Calendar sync now live in
wintermute. `ANTHROPIC_API_KEY` is still read here, but only by the AI Chat
gateway (`/ai-chat`) and the NFR enrichment module; see wintermute's
`docs/tasks.md` for the moved features.

### Typesetting engines

Rendering shells out; the app bundles no typesetter. Where none is installed
the render endpoints return a **zip of the sources plus a build script**
instead of a PDF, which is a working outcome rather than an error — install an
engine only if you want PDFs produced on the server.

- `TYPST` — path to the `typst` binary, overriding the `PATH` lookup. Typst is
  a single static binary and is the primary path for both templates.
  `scripts/setup.sh INSTALL_TYPST=1` fetches it.
- `TECTONIC` — path to `tectonic`, the preferred LaTeX driver. It downloads
  the TeX packages it needs on first run, so the first LaTeX render on a host
  needs outbound network access and is slow.
- `LATEXMK` — path to `latexmk`, used when `tectonic` is absent. Drives
  XeLaTeX, not pdfLaTeX: `carelock.sty` uses `fontspec`.

Engines are detected once per process and cached; **Re-check** on the
Templates page re-probes after an install. Subprocesses run with a minimal
environment (`PATH`, `HOME`, `TMPDIR`, locale) rather than the app's own, so
`ADMIN_TOKEN`, the Jira token and the AI provider key are never visible to a
typesetter.

## Environment variable equivalents

- `LISTEN_ADDR`
- `SQLITE_PATH`
- `DATABASE_URL`
- `SYNC_TO`
- `SYNC_FROM`
- `ALLOW_JSON_SAVE`
- `ADMIN_TOKEN`
- `SETUP_TOKEN`
- `LOCAL_MODE`
- `TRUST_PROXY`
- `DOCS_DIR`
- `TEMPLATES_DIR`

Flags override environment variables when both are set.
`TYPST`, `TECTONIC` and `LATEXMK` are environment-only — they name external
binaries rather than configure the app, and have no flag equivalent.

## Local (no-login) laptop mode

```bash
LOCAL_MODE=true SQLITE_PATH=./local.db ./carelockconsulting
```

There is no first-boot bootstrap step in this mode — every page and admin
action (editing controls, security NFRs, the risk register, etc.) is
available immediately with no credentials. Do not run `-local-mode` on a
server or anywhere reachable by other users.

`SETUP_TOKEN` is used only by `POST /auth/bootstrap-admin` for first-boot admin provisioning.

## Examples

Build:

```bash
go build -o carelockconsulting ./cmd/api
```

Run with defaults (save endpoints disabled):

```bash
./carelockconsulting
```

Run with save endpoints enabled:

```bash
./carelockconsulting -allow-json-save=true
```

Run with custom address and DB:

```bash
./carelockconsulting -listen-addr=:9090 -sqlite-path=./data/app.db -allow-json-save=true
```

Run using environment variables:

```bash
ALLOW_JSON_SAVE=true LISTEN_ADDR=:9090 SQLITE_PATH=./data/app.db ./carelockconsulting
```

Run against an external PostgreSQL database (overrides SQLite):

```bash
DATABASE_URL='postgres://carelockconsulting:secret@db.internal:5432/carelockconsulting?sslmode=require' ./carelockconsulting
```

`scripts/setup.sh` can provision the role/database for you: set `PG_ADMIN_URL`
to a superuser connection (plus optional `PG_DB`/`PG_USER`/`PG_PASSWORD`/
`PG_HOST`/`PG_PORT`/`PG_SSLMODE`) and it will `CREATE ROLE`/`CREATE DATABASE`
idempotently and write the assembled `DATABASE_URL` into the service env file.

Switch backends, run either profile, and push/pull data between local and
remote with the helper scripts (`scripts/db-switch.sh`, `scripts/run-app.sh`,
`scripts/db-sync.sh`). Export the local database up to the remote one in a
single command:

```bash
# local SQLite -> remote PostgreSQL (upsert/merge), then exit
SQLITE_PATH=./local.db ./carelockconsulting -sync-to "$DATABASE_URL"
```

Full guide: `docs/DATABASE.md`.

Run with break-glass admin token fallback:

```bash
ADMIN_TOKEN='replace-with-long-random-secret' ./carelockconsulting
```

## Production setup (first boot)

Recommended model:
- Use password-based auth users and session cookies for normal admin operations.
- Keep `ADMIN_TOKEN` only as emergency fallback.
- Set `SETUP_TOKEN` for one-time admin bootstrap.

### 1. Start service with setup token

```bash
SETUP_TOKEN='replace-with-one-time-setup-secret' \
LISTEN_ADDR=':8080' \
SQLITE_PATH='./data/app.db' \
./carelockconsulting
```

### 2. Bootstrap first admin user (one time)

```bash
curl -sS -X POST 'http://localhost:8080/auth/bootstrap-admin' \
  -H 'Content-Type: application/json' \
  -H 'X-Setup-Token: replace-with-one-time-setup-secret' \
  -d '{
    "username": "admin",
    "password": "replace-with-strong-password-12+-chars"
  }'
```

Expected behavior:
- First call succeeds with `201`.
- Subsequent calls return `409` (`admin is already bootstrapped`).

### 3. Remove or rotate setup token

After bootstrap:
- Remove `SETUP_TOKEN` from runtime env, or
- Rotate it to a new secret stored in your secret manager.

### 4. Login as admin and use session cookie

```bash
curl -i -sS -X POST 'http://localhost:8080/auth/login' \
  -H 'Content-Type: application/json' \
  -d '{
    "username": "admin",
    "password": "replace-with-strong-password-12+-chars"
  }'
```

This returns `Set-Cookie: carelockconsulting_auth_session=...`.

### 5. Manage authentication users (admin only)

List auth users:

```bash
curl -sS 'http://localhost:8080/auth/users' \
  -H 'Cookie: carelockconsulting_auth_session=<session-cookie>'
```

Create auth user:

```bash
curl -sS -X POST 'http://localhost:8080/auth/users' \
  -H 'Content-Type: application/json' \
  -H 'Cookie: carelockconsulting_auth_session=<session-cookie>' \
  -d '{
    "username": "ops-admin",
    "password": "replace-with-strong-password-12+-chars",
    "is_admin": true
  }'
```

Update auth user password and/or admin role:

```bash
curl -sS -X PUT 'http://localhost:8080/auth/users/ops-admin' \
  -H 'Content-Type: application/json' \
  -H 'Cookie: carelockconsulting_auth_session=<session-cookie>' \
  -d '{
    "password": "replace-with-new-strong-password-12+-chars",
    "is_admin": true
  }'
```

Delete auth user:

```bash
curl -sS -X DELETE 'http://localhost:8080/auth/users/ops-admin' \
  -H 'Cookie: carelockconsulting_auth_session=<session-cookie>'
```

### 5b. Manage users offline (no server, no session)

`scripts/manage-user.sh` edits `auth_users` directly, which is the way to add a
user or reset a password when nobody can log in (the API routes above all need
an admin session, and `admin_setup_login.sh` only provisions the *first*
admin).

```bash
scripts/manage-user.sh list
scripts/manage-user.sh create -u alice --admin            # prompts for the password
scripts/manage-user.sh create -u bob --pages '/reports,/crm/*' --generate
scripts/manage-user.sh passwd -u alice --revoke           # reset + kill existing sessions
scripts/manage-user.sh set-admin -u alice                 # grant admin
scripts/manage-user.sh set-admin -u alice --remove        # take admin away
scripts/manage-user.sh delete -u bob
```

**Locked out of admin?** If the only account is not an admin, `create` cannot
help: single-user mode (the shipped default) refuses a second account, and the
in-app user-management page needs an admin session you do not have. Use
`set-admin`:

```bash
scripts/manage-user.sh list                    # confirm the username and its admin flag
scripts/manage-user.sh set-admin -u alice
```

`set-admin` needs no password and does not touch credentials or sessions. The
session lookup reads `is_admin` from `auth_users` on every request, so it takes
effect on the next page load — no restart, no re-login. `--remove` refuses to
remove the last admin, so it cannot be used to re-create the lockout.

- Passwords are prompted with no echo, or `--generate` (random, printed once)
  or `--password-stdin`; they are never passed as arguments, so they stay out
  of the process list and shell history.
- `--pages` limits a non-admin user to those page paths (`/foo/*` wildcards
  allowed); omit it to leave the user with access to every page. Admins always
  have full access.
- Backend: `--db SPEC` (a `postgres://` URL or SQLite path), else
  `$DATABASE_URL`/`$SQLITE_PATH`, else the active `config/db.env` profile, else
  `users.db`. Point it at the same database the service runs on.
- The script runs `go run ./cmd/userctl`; on a host without the Go toolchain,
  build `cmd/userctl` elsewhere and set `USERCTL_BIN=/path/to/userctl`.
- Safe to run while the service is up (SQLite WAL + busy timeout). A password
  change does not by itself invalidate that user's existing sessions — add
  `--revoke` if it should.

### 6. Use break-glass token only for recovery

If session auth is unavailable and `ADMIN_TOKEN` is configured, call privileged routes with:

```bash
-H 'Authorization: Bearer <admin-token>'
```

or:

```bash
-H 'X-Admin-Token: <admin-token>'
```

## Security notes

- Use HTTPS in production so auth cookies are sent with `Secure`.
- Store `SETUP_TOKEN`, `ADMIN_TOKEN`, and initial admin password in a secret manager.
- Enforce long random secrets and rotate `ADMIN_TOKEN` regularly.
- Prefer dedicated named admin users over shared credentials.

## How-to: Merge tested security patches back to master

After `security-patches` is fully tested and approved:

```bash
git checkout master
git pull origin master
git merge --no-ff security-patches
go test ./...
git push origin master
```

Optional cleanup:

```bash
git branch -d security-patches
git push origin --delete security-patches
```
