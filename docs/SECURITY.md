# Security

How the application authenticates and authorises, what it does with secrets,
where data leaves the process, and the open issues a review on 2026-10-10
found. Each statement was checked in the code or by calling a test instance;
where a statement comes from a design document and was not re-tested, it says
so.

The route-by-route access table is [HTTP_API.md](HTTP_API.md). The Policy
Studio has its own threat model in `POLICY_STUDIO.md` §8.

## Deployment modes

| Mode | Accounts | Who can do what |
|---|---|---|
| Normal | Password accounts and session cookies | Public routes need nothing; most pages need a session; changes need an admin. |
| Local (`LOCAL_MODE=true`) | None | Everything is open to anyone who can reach the port. `/login`, `/auth/*` and user management are not registered. The knowledge API is served without a token. Policy Studio share links are refused. |

Local mode is for one person on a loopback address. Nothing in the binary
stops it being bound to a public address.

## Authentication

Code: `internal/authn`.

- **Passwords** are hashed with bcrypt at the default cost. Length must be 5
  to 256 characters; there is no complexity rule. A failed lookup compares
  against a dummy hash so that an unknown username takes as long as a wrong
  password.
- **Lockout:** five failed attempts for one username inside 15 minutes lock
  that username for 15 minutes. The counters are in memory, per process, and
  keyed by username only, not by client address.
- **Sessions:** a 32-byte random token, stored as is in `auth_sessions` with
  a 24-hour expiry, and sent as the cookie `grc_auth_session`: `HttpOnly`,
  `SameSite=Lax`, and `Secure` when the request arrived over TLS or, with
  `TRUST_PROXY=true`, when `X-Forwarded-Proto` says https. A session is
  checked against the database on every request, so removing admin from an
  account or deleting it takes effect at once. A password change does not end
  the account's existing sessions.
- **First admin:** `POST /auth/bootstrap-admin` creates the first account and
  refuses once any account exists. If `SETUP_TOKEN` is set, the request must
  carry it in `X-Setup-Token`. If it is not set, the first request from
  anyone creates the admin. `scripts/setup.sh` always generates one.
- **Single-user mode** is on by default in the code: once one account exists,
  creating another is refused. No flag or setting turns it off; only tests
  do. `scripts/manage-user.sh set-admin` exists for the case where the one
  account is not an admin.
- **Break-glass:** if `ADMIN_TOKEN` is set, a request carrying it as
  `Authorization: Bearer` or `X-Admin-Token` passes the admin gate. It is
  compared in constant time. It has no user name, so actions taken with it are
  attributed to nobody.

## Authorisation

Two middlewares in `internal/app` make every decision outside the Policy
Studio.

**`pageAccessMiddleware`** (all requests, not in local mode) protects a fixed
list of path prefixes (`protectedPrefixes`): the catalogs, the registers,
policies, templates, reports, Jira, the AI chat, the change log, the API docs
and the utilities. Under those prefixes a request needs a session. A
non-admin whose account has an `allowed_pages` list is further limited:

- a grant for `/controls` covers `/controls` and the routes beneath it,
  except a route that is itself a grantable page (`/controls/manage`);
- a grant ending in `/*` covers everything beneath it;
- an account with an empty list can open every protected page.

**`adminTokenMiddleware`** sits on each module's write group, and on Settings,
Utilities and user management entirely.

The design choice to know: **a path is public unless its prefix is on the
list.** A module mounted under a new top-level path is reachable without a
session until someone adds it.

What a non-admin signed-in user can do beyond reading: ask the AI chat (which
spends model calls), run Jira requests with credentials they supply, start a
PDF render, and save a JSON document.

## Machine and guest credentials

| Credential | Grants | Storage and handling |
|---|---|---|
| `KNOWLEDGE_TOKEN` | Read-only `/api/knowledge/*`: the whole catalog, policy library, risk register, regulation reports and exercises | Environment or flag. Without it the API is not registered outside local mode. |
| `ADMIN_TOKEN` | Everything an admin can do | Environment or flag. Optional. |
| `SETUP_TOKEN` | Creating the first admin | Environment. |
| Wintermute client token | Calls to the AI server | Encrypted in `app_secrets`, or `WINTERMUTE_TOKEN`. Never returned to a browser. |
| Share-link token | Joining one Studio document as a guest | 256-bit; only its SHA-256 is stored; shown once; expiry, use limit, revocation. (From the design record.) |
| Guest session cookie | One document, in the link's role | Only its SHA-256 is stored; `SameSite=Strict`; ended by expiry, by leaving, or by an admin. (From the design record.) |

The collaboration socket authorises its own upgrade and refuses a request
whose `Origin` is missing or is neither the request's host nor on
`STUDIO_ALLOWED_ORIGINS`. Tests walk the route table and assert that every
route under `/collab/` and `/shared/` passes through its gate.

## Secrets at rest

Code: `internal/secrets`, `internal/settings`.

Stored credentials are sealed with AES-256-GCM under a 32-byte master key,
with the credential's name as associated data. The key is taken from, in
order: `GRC_SECRET_KEY` (base64); the file named by `GRC_SECRET_KEY_FILE`;
`secret.key` in systemd's `$STATE_DIRECTORY`; `secret.key` beside the SQLite
file; `secret.key` in the working directory. If none exists one is generated,
with mode 0600. A key file readable by other accounts is refused. With no
usable key, storing a credential is disabled and the environment fallback
still works.

A JSON snapshot and a database sync leave out `app_secrets` and `app_state`.
A raw SQLite backup includes them; it is useless for the credentials without
the key file, and the key file is not in it.

## Browser-side protections

- There are no CSRF tokens. State-changing routes take JSON bodies and rely
  on the session cookie being `SameSite=Lax`.
- Pages build HTML in JavaScript by concatenation through an `esc()` helper.
  Server-rendered values pass through an HTML escaper.
- No Content-Security-Policy, `X-Frame-Options` or `X-Content-Type-Options`
  header is set globally. The guest Studio pages set an enforced CSP,
  `X-Frame-Options: DENY`, `nosniff` and `Referrer-Policy: no-referrer`. The
  signed-in Studio page sets a CSP in report-only mode.
- Studio content is validated on the server against a schema allowlist, and
  link schemes are limited to https, http and mailto.
- Spreadsheet and CSV exports write text cells as text; a value that begins
  with `=` is not a formula in the Exceptions Workbook.

## Where data leaves the process

| Destination | What goes | Control |
|---|---|---|
| The Wintermute server | Every AI question with the context its module builds: catalog shortlists, a regulation section, an exercise, a policy document or section, the conversation. | The URL must be https unless the host is a loopback or private address, and may not carry credentials. A policy document's `ai_policy` can refuse AI or require a local backend. Which backend answered is recorded with each proposal. |
| A model behind Wintermute | Whatever Wintermute forwards. This application cannot see or limit it beyond `ai_policy`. | On the Wintermute server. |
| A Jira Cloud site | The JQL or issue key and the credentials the user typed. | https only. Any host. |
| Child processes (Typst, LaTeX, Pandoc) | The document as data files. | A minimal environment (`PATH`, `HOME`, `TMPDIR`, locale), so tokens are not inherited. Document text is passed as data, not interpolated into template source. |
| A grc-render host (`RENDER_URL`) | For a Typst or Pandoc render, the whole workspace: template sources, the brand, the generated file and the document as `data.json`. | Off unless configured. Bearer token (`RENDER_TOKEN`), https with `RENDER_CA_FILE` when the host has a certificate, and a firewall rule on its port. grc-render keeps nothing after a request, logs no content, and runs only Typst or Pandoc on flat file names (`DOCUMENT_TEMPLATES.md`). |
| Headless Chrome | Report HTML generated by the application. | See `internal/reporting/README.md`. |

The application accepts no document uploads. The only upload is the JSON
snapshot import on the Utilities page, admin only, capped at 128 MiB.

## The security gate

`go test ./...` runs `gosec` and `govulncheck` over the module as tests
(`security_test.go`), so a known vulnerability reachable from the code fails
the test run. There is no CI; the gate is run by hand before a commit. The
editor's JavaScript dependencies are pinned exactly, installed with scripts
disabled, checked against a licence allowlist at build time, and audited with
`npm audit --omit=dev`. Vulnerability fixes go on a `security-patches` branch
and are recorded in `CHANGELOG.md`.

On 2026-10-10 the module moved to Go 1.26 (toolchain go1.26.9) and
`golang.org/x/net` v0.60.0 for twelve advisories; govulncheck then reported
none reachable. One advisory remains against a required module with no fix
and no call path (the unmaintained `openpgp` package in `golang.org/x/crypto`).

---

## Open issues found in review

Verified on 2026-10-10. None has been changed by this review. Ordered by how
much they matter on a server other people can reach.

### 1. Routes that answer without a session

A test instance in normal mode was called with no cookie. These answered:

| Route | What an anonymous caller gets |
|---|---|
| `GET /stored-json`, `GET /stored-json/:id` | The list and the content of every saved JSON document. The Jira pages save search results and issue snapshots here. |
| `POST /stored-json` | Stores any valid JSON, with no size limit of its own. |
| `GET /users`, `GET /users/:id` | The rows of the `users` table (names and e-mail addresses). |
| `GET /knowledge/:name` | Ten repository documents raw, including `Agents.md`, `RUNTIME_ARGS.md` and the whole change log. |

The cause is the same for all of them: none of `/stored-json`, `/users` and
`/knowledge` is in `protectedPrefixes`, and the routes are registered on the
open router. `/`, `/help`, `/login`, `/health`, `/version` and the Studio's
static bundle are public by design.

### 2. A database and two key files are committed

`git ls-files` lists `users.db`, `users.db.aichat.key` and
`local.db.aichat.key`. All three are in `.gitignore`, but they were added in
the first commit and are on `origin/main`. The committed `users.db` is an old
schema (18 tables) holding one sign-in account with its bcrypt hash. No code
reads the two `.aichat.key` files any more.

### 3. The first admin can be claimed when `SETUP_TOKEN` is unset

With no `SETUP_TOKEN` and no account yet, `POST /auth/bootstrap-admin` makes
whoever calls it first the admin. `scripts/setup.sh` and
`admin_setup_login.sh` set a token; a binary started by hand on a reachable
address does not.

### 4. The master key can land in the checkout

When the SQLite path has no directory (`users.db`, `local.db`) and
`$STATE_DIRECTORY` is unset, the key file is `./secret.key` in the working
directory, generated at start-up. `.gitignore` does not list `secret.key`.
`run_local.sh` runs from the repository root with `local.db`. This is the
shape of the earlier incident the `internal/secrets` package comment
describes.

### 5. Smaller points

- **Open redirect after sign-in.** The login page follows `?next=` when it
  starts with `/`, which `//other.example` also does.
- **Passwords can be five characters.** `RUNTIME_ARGS.md` shows twelve or
  more in its examples; the code accepts five.
- **Session tokens are stored unhashed**, unlike guest and share-link tokens.
  Anyone who can read the database or a raw backup can use a live session.
- **Lockout by username** lets anyone lock a known account out for 15 minutes
  at a time, and is forgotten on restart.
- **Jira requests go to any https host** a signed-in user names, from the
  server, with the response returned to them.
- **No global security headers**, as described above.
- **`ADMIN_TOKEN` actions are unattributed** in the modules that record who
  changed something.
- **The release configuration is missing.** `Agents.md` and the build script
  refer to `.goreleaser.yaml`, which is not in the repository.

### Not an issue, but easy to misread

- A signed-in non-admin can read almost everything. That is the model:
  reading is open inside the firm, changing is restricted. `allowed_pages`
  narrows it per account.
- The knowledge token cannot write. The package has no mutating method.
- "Suggest mode" in the Studio is a convention in the editor. The server
  guarantee is that open suggestions block approval.
