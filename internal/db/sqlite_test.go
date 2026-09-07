package db

import (
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
		"rcsa_controls",
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
