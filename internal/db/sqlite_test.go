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
