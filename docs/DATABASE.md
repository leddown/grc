# Database backends, switching, and local → remote sync

grc runs on either of two interchangeable storage backends that
share one logical schema:

| Backend                | Selected by                          | Typical use                     |
| ---------------------- | ------------------------------------ | ------------------------------- |
| Embedded **SQLite**    | `SQLITE_PATH` / `-sqlite-path` (default) | local laptop, single user   |
| External **PostgreSQL**| `DATABASE_URL` / `-database-url`     | shared server / production      |

`DATABASE_URL` takes precedence: if it is set (non-empty), the app uses
PostgreSQL and ignores `SQLITE_PATH`. The schema is created automatically on
first connection for **both** backends — there is no separate migration step.

This is one binary. You do not recompile to change backends; you change
configuration. The helper scripts below make that a one-liner, and the binary
has a built-in sync mode to move data between the two.

---

## 1. Switching between database configurations

### Option A — the helper scripts (recommended)

Configuration for the helpers lives in `config/db.env` (copied from
`config/db.env.example` on first use; gitignored because it holds the remote
credentials). It defines both profiles and which one is active:

```bash
# one-time: create config/db.env and edit REMOTE_DATABASE_URL
cp config/db.env.example config/db.env
$EDITOR config/db.env
```

```bash
scripts/db-switch.sh local     # use the embedded SQLite (laptop) profile
scripts/db-switch.sh remote    # use the external PostgreSQL profile
scripts/db-switch.sh status    # show the active profile (password masked)

scripts/run-app.sh             # build + run whichever profile is active
scripts/run-app.sh --skip-build
```

The two profiles (one binary, two run configurations) are:

- **local** — SQLite at `LOCAL_SQLITE_PATH`, `LOCAL_MODE=true` (no login,
  every page open). Intended for a single-user laptop.
- **remote** — PostgreSQL at `REMOTE_DATABASE_URL`, normal authentication.

`run-app.sh` deliberately unsets the other backend's variables so the local
profile never accidentally connects to Postgres and vice-versa.

### Option B — environment variables / flags directly

```bash
# SQLite
./grc -sqlite-path ./local.db
SQLITE_PATH=./local.db ./grc

# PostgreSQL (overrides SQLite)
./grc -database-url 'postgres://user:pass@host:5432/db?sslmode=require'
DATABASE_URL='postgres://user:pass@host:5432/db?sslmode=require' ./grc
```

For a server install, `scripts/setup.sh` can also provision the PostgreSQL role
and database for you and bake `DATABASE_URL` into the systemd env file — see
`RUNTIME_ARGS.md` and the script header.

---

## 2. Exporting local changes and uploading them to the remote database

The application has a built-in, one-shot **sync mode**: it copies the managed
tables from one backend to another and then exits instead of serving. Because
both backends use the same logical schema, the same code path works in either
direction (SQLite ↔ PostgreSQL).

### The easy way

```bash
scripts/db-sync.sh push      # local SQLite  -> remote PostgreSQL
scripts/db-sync.sh pull      # remote PostgreSQL -> local SQLite
scripts/db-sync.sh push --yes --skip-build
```

`push`/`pull` read `LOCAL_SQLITE_PATH` and `REMOTE_DATABASE_URL` from
`config/db.env`, prompt for confirmation (the remote is usually production), and
then invoke the binary's sync mode.

### The underlying flags

The active backend (`-sqlite-path` / `-database-url`) is one side of the sync;
the flag value is the other side. A sync target/source is engine-detected: a
`postgres://` URL is PostgreSQL, anything else is a SQLite file path.

```bash
# push: active SQLite -> remote Postgres
SQLITE_PATH=./local.db ./grc \
  -sync-to 'postgres://user:pass@host:5432/db?sslmode=require'

# pull: remote Postgres -> active SQLite
SQLITE_PATH=./local.db ./grc \
  -sync-from 'postgres://user:pass@host:5432/db?sslmode=require'
```

`-sync-to` and `-sync-from` are mutually exclusive. The run prints a per-table
row count and exits non-zero on the first error.

### What the sync does (semantics)

- **Merge by natural key (upsert).** Catalog/registry tables are matched on
  their business key (`control_id`, `record_key`, `risk_id`, `username`,
  `email`, `family`, `session_id`, …). Existing destination rows are updated;
  rows that exist **only** on the destination are left untouched. Re-running a
  sync is therefore safe and idempotent.
- **CRM tables preserve ids.** `crm_clients` / `crm_engagements` /
  `crm_time_entries` (and `stored_json_documents`) carry their primary keys
  across so foreign-key relationships stay intact; on PostgreSQL the identity
  sequences are realigned afterwards so future inserts don't collide.
- **The derived link table is rebuilt.** `security_nfr_control_links` has no
  stable key (it is regenerated from the catalogs + overrides), so it is
  replaced wholesale on the destination to stay consistent with the rows just
  copied.
- **Login sessions are skipped.** `auth_sessions` is ephemeral and never synced.

> **Tip:** the catalogs (`rcsa_controls`, `security_nfrs`) are seeded from
> embedded data the first time the app *serves*. If you push a brand-new local
> DB that has never been run as a server, those tables may be empty. Run the app
> once (`scripts/run-app.sh`) to seed/populate before pushing.

---

## 3. Alternatives considered (research)

The in-app sync is the recommended path because it needs no extra tooling, knows
the exact schema, and works symmetrically in both directions with merge
semantics. For completeness, other approaches and why they were not adopted:

- **`pgloader`** — a capable SQLite→PostgreSQL migrator. Good for a one-time
  bulk import, but it is an external system dependency, is one-directional
  (SQLite→PG), and does full loads rather than incremental upsert/merge.
- **`pg_dump` / `.dump` + transform** — dumping SQL from one engine and
  replaying it on the other is brittle: the dialects differ (`AUTOINCREMENT`
  vs `BIGSERIAL`, `PRAGMA`, quoting, `GROUP_CONCAT` vs `STRING_AGG`), so the
  dump must be hand-edited. Fine for an emergency, not for a repeatable
  workflow.
- **Application-level export/import to JSON** — essentially what the sync does,
  but routing through files adds a serialization format to maintain with no
  benefit over a direct table-to-table copy.

---

## 4. Safety notes

- `config/db.env` contains the remote connection string with its password; it is
  gitignored. Don't commit it. `db-switch.sh status` and `db-sync.sh` mask the
  password when printing.
- A `push` writes to the remote (often production). It is merge-only (no deletes
  except rebuilding the derived link table), but always review what you're
  pushing; the confirmation prompt is there for a reason (`--yes` to skip in
  automation).
- Use `sslmode=require` (or stricter) in `REMOTE_DATABASE_URL` for any
  non-local PostgreSQL so credentials and data are encrypted in transit.
- Take a backup of the remote database before the first push.
