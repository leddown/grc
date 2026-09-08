package dbsync

import (
	"sort"
	"strings"
	"testing"
)

// skippedTables are the tables deliberately absent from syncOrder, with the
// reason each one is absent. Anything not listed here and not in syncOrder is
// a table nobody decided to leave out — which is how Regulation Coverage and
// the crisis exercises spent several releases missing from every "full data
// set" export without anyone noticing.
var skippedTables = map[string]string{
	"auth_sessions":   "ephemeral login sessions; a restored session token would be meaningless on the destination",
	"app_state":       "this deployment's own configuration, which should not follow the data to another server",
	"app_secrets":     "ciphertext whose key file never travels in a snapshot, so copying it hides that the credential must be set again",
	"sqlite_sequence": "SQLite's internal AUTOINCREMENT bookkeeping, rebuilt by the inserts themselves",
}

// TestSyncOrderCoversEverySchemaTable is the guard on the whole backup path: a
// table added to the schema is either copied or explicitly excluded, and a new
// module cannot quietly ship without being in the backup.
func TestSyncOrderCoversEverySchemaTable(t *testing.T) {
	conn := openTemp(t, "coverage.db")

	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	defer func() { _ = rows.Close() }()

	managed := map[string]bool{}
	for _, spec := range syncOrder {
		managed[spec.table] = true
	}

	var missing []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		if managed[table] {
			continue
		}
		if _, skipped := skippedTables[table]; skipped {
			continue
		}
		missing = append(missing, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema: %v", err)
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("tables in the schema but not in the backup: %s\n"+
			"add a tableSpec to syncOrder (and bump SnapshotVersion), or add the table to skippedTables with the reason",
			strings.Join(missing, ", "))
	}
}

// TestSyncOrderCoversEveryColumn is the same guard one level down: a column
// added to a managed table has to be listed, or it is silently dropped from
// every backup while the table itself still looks covered.
//
// The one column a spec may omit is "id": tables matched on a natural key
// deliberately leave their auto-increment id behind so the destination assigns
// its own.
func TestSyncOrderCoversEveryColumn(t *testing.T) {
	conn := openTemp(t, "columns.db")

	for _, spec := range syncOrder {
		listed := map[string]bool{}
		for _, col := range spec.cols {
			listed[col] = true
		}

		rows, err := conn.Query(`SELECT name FROM pragma_table_info(?)`, spec.table)
		if err != nil {
			t.Fatalf("%s: read columns: %v", spec.table, err)
		}
		var missing []string
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				t.Fatalf("%s: scan column: %v", spec.table, err)
			}
			if col == "id" || listed[col] {
				continue
			}
			missing = append(missing, col)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("%s: iterate columns: %v", spec.table, err)
		}
		_ = rows.Close()

		if len(missing) > 0 {
			t.Errorf("%s: columns in the schema but not in the backup: %s", spec.table, strings.Join(missing, ", "))
		}
	}
}
