package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenSQLite_CreatesExpectedSchema(t *testing.T) {
	sqlite, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	tables := []string{
		"users",
		"nist_stride_mappings",
		"nist_controls",
		"control_family_visibility",
		"security_nfrs",
		"security_nfr_control_links",
		"nfr_control_link_overrides",
		"stored_json_documents",
		"auth_users",
		"auth_sessions",
		"app_secrets",
	}
	for _, table := range tables {
		var name string
		if err := sqlite.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("expected table %q to exist: %v", table, err)
		}
		if name != table {
			t.Fatalf("table name=%q want=%q", name, table)
		}
	}
}

// A database whose control table still has its earlier name keeps its rows: the
// table is renamed before the schema could create an empty one beside it.
func TestOpenSQLite_RenamesLegacyControlTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE ` + LegacyControlTable + ` (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			control_id TEXT NOT NULL UNIQUE,
			family TEXT NOT NULL,
			mapping_baselines_json TEXT NOT NULL,
			threats_json TEXT NOT NULL
		)`,
		`CREATE INDEX idx_` + LegacyControlTable + `_control_id ON ` + LegacyControlTable + `(control_id)`,
		`INSERT INTO ` + LegacyControlTable + ` (control_id, family, mapping_baselines_json, threats_json) VALUES ('AC-1', 'AC', '[]', '[]')`,
	} {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for pass := 1; pass <= 2; pass++ {
		sqlite, err := OpenSQLite(path)
		if err != nil {
			t.Fatalf("OpenSQLite, pass %d: %v", pass, err)
		}
		var family, name string
		if err := sqlite.QueryRow(`SELECT family, name FROM nist_controls WHERE control_id = 'AC-1'`).Scan(&family, &name); err != nil {
			t.Fatalf("pass %d: the row did not follow the table: %v", pass, err)
		}
		if family != "AC" {
			t.Errorf("pass %d: family=%q want AC", pass, family)
		}
		var leftovers int
		if err := sqlite.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE '%' || ? || '%' OR sql LIKE '%' || ? || '%'`,
			LegacyControlTable, LegacyControlTable).Scan(&leftovers); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if leftovers != 0 {
			t.Errorf("pass %d: %d schema objects still carry the old name", pass, leftovers)
		}
		var indexes int
		if err := sqlite.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ('idx_nist_controls_control_id', 'idx_nist_controls_family_type')`).Scan(&indexes); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if indexes != 2 {
			t.Errorf("pass %d: %d of the 2 control indexes exist", pass, indexes)
		}
		if err := sqlite.Close(); err != nil {
			t.Fatalf("close, pass %d: %v", pass, err)
		}
	}
}

// TestFTS5IsCompiledIn proves the claim /version makes.
//
// FTS5 used to depend on -tags fts5, and a binary built without it failed only
// when the policy-document corpus search first ran — a deferred failure that a
// deploy check had to catch. The pure-Go driver compiles FTS5 in
// unconditionally, and this asserts that against a real database rather than
// against a build tag, so a driver change that quietly dropped it would fail
// here instead of in production.
func TestFTS5IsCompiledIn(t *testing.T) {
	sqlite, err := OpenSQLite(filepath.Join(t.TempDir(), "fts.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	if _, err := sqlite.Exec(`CREATE VIRTUAL TABLE docs USING fts5(body)`); err != nil {
		t.Fatalf("FTS5 is unavailable, so corpus search would fail at runtime: %v", err)
	}
	if _, err := sqlite.Exec(`INSERT INTO docs(body) VALUES ('network segmentation control')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var matches int
	if err := sqlite.QueryRow(`SELECT count(*) FROM docs WHERE docs MATCH 'segmentation'`).Scan(&matches); err != nil {
		t.Fatalf("MATCH: %v", err)
	}
	if matches != 1 {
		t.Errorf("MATCH found %d rows, want 1", matches)
	}
}

// The WAL journal mode set at open is a durable property of the database file,
// and the app relies on it: a reader must not be blocked by a writer while a
// page is rendering.
func TestOpenSQLiteUsesWAL(t *testing.T) {
	sqlite, err := OpenSQLite(filepath.Join(t.TempDir(), "wal.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	var mode string
	if err := sqlite.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}
