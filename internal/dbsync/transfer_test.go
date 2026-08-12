package dbsync

import (
	"encoding/json"
	"testing"
)

// roundTrip marshals a snapshot to JSON and back, mimicking the real transfer
// where the file is written on one server and read on another. This also
// exercises the int64 -> float64 coercion that JSON numbers undergo.
func roundTrip(t *testing.T, snap *Snapshot) *Snapshot {
	t.Helper()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	return &decoded
}

func TestExportImport_OverwritesDestination(t *testing.T) {
	src := openTemp(t, "src.db")
	dst := openTemp(t, "dst.db")

	// Representative rows across the table kinds dbsync manages.
	mustExec(t, src, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json) VALUES (?, ?, ?, '[]', '[]')`, "AC-1", "AC", "Policy")
	mustExec(t, src, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json) VALUES (?, ?, ?, '[]', '[]')`, "AC-2", "AC", "Accounts")
	mustExec(t, src, `INSERT INTO security_risk_register (risk_id, title, likelihood) VALUES (?, ?, ?)`, "R-1", "Risk one", 3)
	mustExec(t, src, `INSERT INTO auth_users (username, password_hash, is_admin) VALUES (?, ?, 1)`, "admin", "hash")
	// FK chain carried by explicit id.
	mustExec(t, src, `INSERT INTO nfr_source_documents (id, title, origin) VALUES (1, 'Transport Standard', 'upload')`)
	mustExec(t, src, `INSERT INTO nfr_source_chunks (id, document_id, ordinal, heading, body) VALUES (10, 1, 0, 'TLS', 'TLS 1.2 minimum')`)
	mustExec(t, src, `INSERT INTO nfr_enrichment_proposals (id, nfr_key, document_id, field, suggested_text) VALUES (100, 'NFR-1', 1, 'implementation', 'TLS 1.2 minimum')`)

	// Destination row that an overwrite must remove (this is what distinguishes
	// Import from the merge-oriented Sync).
	mustExec(t, dst, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json) VALUES (?, ?, ?, '[]', '[]')`, "ZZ-9", "ZZ", "Server only")

	snap, err := Export(src)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if snap.Version != SnapshotVersion {
		t.Fatalf("snapshot version=%d want %d", snap.Version, SnapshotVersion)
	}

	rep, err := Import(dst, roundTrip(t, snap))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if rep.Total() == 0 {
		t.Fatal("expected rows imported")
	}

	// Destination now mirrors source exactly: the server-only row is gone.
	if got := count(t, dst, "rcsa_controls"); got != 2 {
		t.Fatalf("rcsa_controls count=%d want 2 (overwrite removes dst-only row)", got)
	}
	if got := scalar(t, dst, `SELECT name FROM rcsa_controls WHERE control_id = ?`, "AC-1"); got != "Policy" {
		t.Fatalf("AC-1 name=%q want Policy", got)
	}
	if got := count(t, dst, "rcsa_controls"); got != 2 {
		t.Fatalf("unexpected control count %d", got)
	}
	// Integer column survives the JSON float round-trip as an integer.
	if got := scalar(t, dst, `SELECT likelihood FROM security_risk_register WHERE risk_id = ?`, "R-1"); got != "3" {
		t.Fatalf("R-1 likelihood=%q want 3", got)
	}
	// FK chain preserved with ids intact.
	if got := scalar(t, dst, `SELECT heading FROM nfr_source_chunks WHERE id = 10`); got != "TLS" {
		t.Fatalf("chunk heading=%q want TLS", got)
	}
	if got := count(t, dst, "nfr_enrichment_proposals"); got != 1 {
		t.Fatalf("nfr_enrichment_proposals count=%d want 1", got)
	}

	// A second import is idempotent (full replace, no duplicates).
	if _, err := Import(dst, roundTrip(t, snap)); err != nil {
		t.Fatalf("Import (second): %v", err)
	}
	if got := count(t, dst, "rcsa_controls"); got != 2 {
		t.Fatalf("rcsa_controls count after re-import=%d want 2", got)
	}
	if got := count(t, dst, "nfr_enrichment_proposals"); got != 1 {
		t.Fatalf("nfr_enrichment_proposals count after re-import=%d want 1", got)
	}
}
