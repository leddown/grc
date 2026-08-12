package dbsync

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"grc/internal/db"
)

// SnapshotVersion is the format version stamped into every exported Snapshot.
// Bump it if the set of tables/columns ever changes in a way an old importer
// could not read.
//
// v2 added the policy_documents / policy_sections / policy_versions tables and
// policy_section_controls. Import accepts any version from 1 up to this one:
// it reads whatever tables a snapshot carries and leaves the rest empty, so an
// older backup restores with empty policy tables rather than being rejected.
//
// v3 added doc_template_brand, the document-template brand settings. A v2
// backup restores into it as an empty table, which is exactly right: the
// install falls back to the brand the templates ship with.
//
// v4 added the personal task module (todo_lists, todo_tasks) and the assistant
// (assistant_conversations, assistant_messages, assistant_tool_calls). Older
// backups restore with those empty, which is the correct reading of a backup
// taken before the feature existed.
//
// v5 added company_profile, the firm's own details. A v4 backup restores into
// it as an empty table, which reads as "not filled in yet" — the same state a
// fresh install is in.
//
// v6 added the Google Calendar link (google_calendar_accounts,
// google_calendar_events). A v5 backup restores with both empty, which reads as
// "not connected" — the state the feature starts in. Note that the stored
// tokens are ciphertext and the key file does not travel in a snapshot, so
// restoring onto a different host yields an account that must be reconnected
// rather than one that silently keeps writing to somebody's calendar.
//
// v7 added ai_usage_log, the AI Chat per-call token/cost history. It existed
// in the schema well before this, but was never added to syncOrder, so every
// "full data set" export was silently missing it. A v6 backup restores it
// empty, which reads as "no usage recorded yet" rather than losing anything
// that was actually in the older backup.
//
// The import handler enforces that range. Bumping this constant must never make
// an existing backup unrestorable — that was the effect when the handler
// compared for equality, and it is the failure mode to watch for on the next
// bump.
const SnapshotVersion = 7

// Snapshot is a portable, engine-independent dump of every managed table — the
// same tables (and columns) dbsync.Sync copies. It is the on-the-wire format
// for transferring a full data set between two disconnected deployments via the
// Utilities page: one server exports a Snapshot to a file, the other imports it
// to overwrite its database.
type Snapshot struct {
	Version    int                         `json:"version"`
	ExportedAt string                      `json:"exported_at"`
	Tables     map[string][]map[string]any `json:"tables"`
}

// Export reads every managed table from src into a Snapshot. The connection's
// schema must already be initialized (db.Open / OpenSQLite / OpenPostgres all
// do this).
func Export(src *db.Conn) (*Snapshot, error) {
	snap := &Snapshot{
		Version:    SnapshotVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Tables:     make(map[string][]map[string]any, len(syncOrder)),
	}
	for _, spec := range syncOrder {
		rows, err := exportTable(src, spec)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", spec.table, err)
		}
		snap.Tables[spec.table] = rows
	}
	return snap, nil
}

func exportTable(src *db.Conn, spec tableSpec) ([]map[string]any, error) {
	srcRows, err := src.Query(fmt.Sprintf("SELECT %s FROM %s", strings.Join(spec.cols, ", "), spec.table))
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}
	defer srcRows.Close()

	out := make([]map[string]any, 0)
	for srcRows.Next() {
		values := make([]any, len(spec.cols))
		ptrs := make([]any, len(spec.cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := srcRows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(spec.cols))
		for i, col := range spec.cols {
			v := values[i]
			// SQLite hands TEXT columns back as []byte; normalize to string so
			// they serialize as JSON strings rather than base64 blobs.
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[col] = v
		}
		out = append(out, row)
	}
	if err := srcRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Import overwrites every managed table in dst with the contents of snap. It is
// the destructive counterpart to Export: the destination's existing rows are
// discarded entirely (this is a full-replace, not a merge). The whole operation
// runs in one transaction — rows are deleted children-first and inserted
// parents-first so foreign keys stay satisfied, and a failure rolls the
// database back to its prior state rather than leaving a half-applied import.
func Import(dst *db.Conn, snap *Snapshot) (Report, error) {
	var report Report
	if snap == nil {
		return report, fmt.Errorf("nil snapshot")
	}

	tx, err := dst.Begin()
	if err != nil {
		return report, err
	}
	defer tx.Rollback()

	// Clear children before parents (reverse of the parent-first sync order).
	for i := len(syncOrder) - 1; i >= 0; i-- {
		spec := syncOrder[i]
		if _, err := tx.Exec("DELETE FROM " + spec.table); err != nil {
			return report, fmt.Errorf("clear %s: %w", spec.table, err)
		}
	}

	// Insert parents before children.
	for _, spec := range syncOrder {
		rows, err := importTable(tx, spec, snap.Tables[spec.table])
		report.Tables = append(report.Tables, TableResult{Table: spec.table, Rows: rows})
		if err != nil {
			return report, fmt.Errorf("import %s: %w", spec.table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return report, err
	}

	// Realign PostgreSQL identity sequences for id-keyed tables (no-op on
	// SQLite), since rows were inserted with explicit ids.
	for _, spec := range syncOrder {
		if spec.idKeyed {
			if err := resetSequence(dst, spec.table); err != nil {
				return report, fmt.Errorf("reset %s id sequence: %w", spec.table, err)
			}
		}
	}
	return report, nil
}

// importTable inserts a snapshot's rows for one table.
//
// A column the row does not carry is omitted from the INSERT rather than being
// written as NULL, so the schema DEFAULT applies. That is what makes a snapshot
// from an older build restorable after a column is added to an existing table:
// writing an explicit NULL defeats the DEFAULT and fails the NOT NULL
// constraint, which would turn every column addition into a silent break of the
// whole restore path.
//
// Statements are cached per column-set. In practice a snapshot's rows are
// homogeneous because one exporter wrote them all, so this prepares once.
func importTable(tx *db.Tx, spec tableSpec, rows []map[string]any) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	type prepared struct {
		stmt *sql.Stmt
		cols []string
	}
	cache := make(map[string]*prepared, 1)
	defer func() {
		for _, p := range cache {
			_ = p.stmt.Close()
		}
	}()

	count := 0
	for _, row := range rows {
		present := make([]string, 0, len(spec.cols))
		for _, col := range spec.cols {
			if _, ok := row[col]; ok {
				present = append(present, col)
			}
		}
		if len(present) == 0 {
			continue
		}

		key := strings.Join(present, ",")
		p, ok := cache[key]
		if !ok {
			placeholders := make([]string, len(present))
			for i := range placeholders {
				placeholders[i] = "?"
			}
			// #nosec G201 -- table and column names come from the compile-time
			// syncOrder specs, never from the snapshot being imported.
			stmt, err := tx.Prepare(fmt.Sprintf(
				"INSERT INTO %s (%s) VALUES (%s)",
				spec.table, strings.Join(present, ", "), strings.Join(placeholders, ", "),
			))
			if err != nil {
				return count, err
			}
			p = &prepared{stmt: stmt, cols: present}
			cache[key] = p
		}

		values := make([]any, len(p.cols))
		for i, col := range p.cols {
			values[i] = row[col]
		}
		if _, err := p.stmt.Exec(values...); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
