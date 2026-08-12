package dbsync

import (
	"path/filepath"
	"testing"

	"carelockconsulting/internal/db"
)

func openTemp(t *testing.T, name string) *db.Conn {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("OpenSQLite(%s): %v", name, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func mustExec(t *testing.T, conn *db.Conn, query string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func count(t *testing.T, conn *db.Conn, table string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func scalar(t *testing.T, conn *db.Conn, query string, args ...any) string {
	t.Helper()
	var v string
	if err := conn.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("scalar %q: %v", query, err)
	}
	return v
}

func TestSync_CopiesMergesAndReplaces(t *testing.T) {
	src := openTemp(t, "src.db")
	dst := openTemp(t, "dst.db")

	// Source data across representative table kinds.
	mustExec(t, src, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json) VALUES (?, ?, ?, '[]', '[]')`, "AC-1", "AC", "Policy")
	mustExec(t, src, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json) VALUES (?, ?, ?, '[]', '[]')`, "AC-2", "AC", "Accounts")
	mustExec(t, src, `INSERT INTO security_risk_register (risk_id, title) VALUES (?, ?)`, "R-1", "Risk one")
	mustExec(t, src, `INSERT INTO auth_users (username, password_hash) VALUES (?, ?)`, "admin", "hash")
	// FK chain with explicit ids.
	mustExec(t, src, `INSERT INTO nfr_source_documents (id, title, origin) VALUES (1, 'Transport Standard', 'upload')`)
	mustExec(t, src, `INSERT INTO nfr_source_chunks (id, document_id, ordinal, heading, body) VALUES (10, 1, 0, 'TLS', 'TLS 1.2 minimum')`)
	mustExec(t, src, `INSERT INTO nfr_enrichment_proposals (id, nfr_key, document_id, field, suggested_text) VALUES (100, 'NFR-1', 1, 'implementation', 'TLS 1.2 minimum')`)
	// Derived/replace table.
	mustExec(t, src, `INSERT INTO security_nfr_control_links (nfr_key, control_id, matched) VALUES ('1', 'AC-1', 1)`)
	mustExec(t, src, `INSERT INTO security_nfr_control_links (nfr_key, control_id, matched) VALUES ('2', 'AC-2', 1)`)

	// Destination-only row that must survive a merge.
	mustExec(t, dst, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json) VALUES (?, ?, ?, '[]', '[]')`, "ZZ-9", "ZZ", "Server only")

	rep, err := Sync(src, dst)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if rep.Total() == 0 {
		t.Fatal("expected rows copied")
	}

	// Copied rows present; destination-only row preserved (merge).
	if got := count(t, dst, "rcsa_controls"); got != 3 {
		t.Fatalf("rcsa_controls count=%d want 3 (2 synced + 1 dst-only)", got)
	}
	if got := scalar(t, dst, `SELECT name FROM rcsa_controls WHERE control_id = ?`, "AC-1"); got != "Policy" {
		t.Fatalf("AC-1 name=%q", got)
	}
	// FK chain copied with ids intact.
	if got := scalar(t, dst, `SELECT heading FROM nfr_source_chunks WHERE id = 10`); got != "TLS" {
		t.Fatalf("chunk heading=%q, want TLS", got)
	}

	// Update in source + shrink the derived table, then re-sync.
	mustExec(t, src, `UPDATE rcsa_controls SET name = 'Policy v2' WHERE control_id = ?`, "AC-1")
	mustExec(t, src, `DELETE FROM security_nfr_control_links WHERE nfr_key = '2'`)

	rep2, err := Sync(src, dst)
	if err != nil {
		t.Fatalf("Sync (second): %v", err)
	}

	// Upsert updated the row, no duplicate, dst-only row still there.
	if got := count(t, dst, "rcsa_controls"); got != 3 {
		t.Fatalf("rcsa_controls count after re-sync=%d want 3", got)
	}
	if got := scalar(t, dst, `SELECT name FROM rcsa_controls WHERE control_id = ?`, "AC-1"); got != "Policy v2" {
		t.Fatalf("AC-1 name after update=%q want Policy v2", got)
	}
	// Replace table mirrors source exactly (1 row now, not appended to 2).
	if got := count(t, dst, "security_nfr_control_links"); got != 1 {
		t.Fatalf("links count after replace=%d want 1", got)
	}
	// id-keyed tables not duplicated on re-sync.
	if got := count(t, dst, "nfr_enrichment_proposals"); got != 1 {
		t.Fatalf("nfr_enrichment_proposals count after re-sync=%d want 1", got)
	}
	_ = rep2
}

// Policy documents carry ids across the copy because sections and versions hold
// a foreign key to the document and parent_document_id points back into the
// same table — a renumbering sync would silently detach both.
func TestSync_PreservesPolicyDocumentHierarchy(t *testing.T) {
	src := openTemp(t, "policy_src.db")
	dst := openTemp(t, "policy_dst.db")

	mustExec(t, src, `INSERT INTO policy_documents (id, title, doc_type, reference, status)
		VALUES (?, ?, ?, ?, ?)`, 7, "Access Control Policy", "policy", "POL-AC-001", "approved")
	mustExec(t, src, `INSERT INTO policy_documents (id, title, doc_type, parent_document_id, status)
		VALUES (?, ?, ?, ?, ?)`, 9, "Provisioning Procedure", "procedure", 7, "draft")
	mustExec(t, src, `INSERT INTO policy_sections (id, document_id, ordinal, heading, body, section_kind)
		VALUES (?, ?, ?, ?, ?, ?)`, 21, 7, 0, "Purpose", "Establish access control.", "purpose")
	mustExec(t, src, `INSERT INTO policy_versions (id, document_id, version_label, approved_by, snapshot)
		VALUES (?, ?, ?, ?, ?)`, 31, 7, "v1.0", "CISO", "# Access Control Policy")

	if _, err := Sync(src, dst); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := count(t, dst, "policy_documents"); got != 2 {
		t.Fatalf("policy_documents = %d, want 2", got)
	}
	if got := scalar(t, dst, `SELECT parent_document_id FROM policy_documents WHERE id = ?`, 9); got != "7" {
		t.Errorf("parent_document_id = %q, want 7 — the hierarchy did not survive the copy", got)
	}
	if got := scalar(t, dst, `SELECT document_id FROM policy_sections WHERE id = ?`, 21); got != "7" {
		t.Errorf("section document_id = %q, want 7", got)
	}
	if got := scalar(t, dst, `SELECT snapshot FROM policy_versions WHERE id = ?`, 31); got != "# Access Control Policy" {
		t.Errorf("approved snapshot did not survive: %q", got)
	}
}

// A snapshot written before the policy tables existed must still import; the
// tables simply come back empty rather than failing the whole restore.
func TestImport_ToleratesSnapshotWithoutPolicyTables(t *testing.T) {
	src := openTemp(t, "old_src.db")
	dst := openTemp(t, "old_dst.db")

	mustExec(t, src, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json)
		VALUES (?, ?, ?, '[]', '[]')`, "AC-1", "AC", "Policy")

	snap, err := Export(src)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// Simulate a v1 snapshot: drop the policy tables entirely.
	snap.Version = 1
	for _, table := range []string{"policy_documents", "policy_sections", "policy_versions"} {
		delete(snap.Tables, table)
	}

	if _, err := Import(dst, snap); err != nil {
		t.Fatalf("Import of a pre-policy snapshot must succeed: %v", err)
	}
	if got := count(t, dst, "policy_documents"); got != 0 {
		t.Errorf("policy_documents = %d, want 0", got)
	}
	if got := count(t, dst, "rcsa_controls"); got != 1 {
		t.Errorf("rcsa_controls = %d, want 1 — the rest of the snapshot must still land", got)
	}
}

// The document-template brand is a single row pinned to id 1 by a CHECK
// constraint, so an id-keyed upsert is the only strategy that can carry it. A
// plain insert would violate the constraint on the second sync into the same
// destination — which is the shape a periodic sync takes.
func TestSync_CarriesTheDocumentTemplateBrand(t *testing.T) {
	src := openTemp(t, "brand_src.db")
	dst := openTemp(t, "brand_dst.db")

	mustExec(t, src, `INSERT INTO doc_template_brand (id, settings_json, updated_at, updated_by)
		VALUES (1, ?, ?, ?)`, `{"firm":"Northwind Assurance"}`, "2026-08-03T00:00:00Z", "alice")

	for i := 0; i < 2; i++ {
		if _, err := Sync(src, dst); err != nil {
			t.Fatalf("Sync (pass %d): %v", i+1, err)
		}
	}
	if got := count(t, dst, "doc_template_brand"); got != 1 {
		t.Fatalf("doc_template_brand = %d rows, want exactly 1", got)
	}
	if got := scalar(t, dst, `SELECT settings_json FROM doc_template_brand WHERE id = 1`); got != `{"firm":"Northwind Assurance"}` {
		t.Errorf("settings_json = %q, want the source's brand", got)
	}
	if got := scalar(t, dst, `SELECT updated_by FROM doc_template_brand WHERE id = 1`); got != "alice" {
		t.Errorf("updated_by = %q, want alice", got)
	}
}

// The template brand is a single-row table constrained to id 1, so a repeated
// sync has to upsert rather than insert.
func TestSync_CarriesTheTemplateBrand(t *testing.T) {
	src := openTemp(t, "brand_src.db")
	dst := openTemp(t, "brand_dst.db")

	mustExec(t, src, `INSERT INTO doc_template_brand (id, settings_json, updated_at, updated_by)
		VALUES (1, ?, ?, ?)`, `{"wordmark":"Northwind Assurance Ltd"}`, "2026-08-04T00:00:00Z", "alice")

	for i := 0; i < 2; i++ {
		if _, err := Sync(src, dst); err != nil {
			t.Fatalf("Sync (pass %d): %v", i+1, err)
		}
	}
	if got := count(t, dst, "doc_template_brand"); got != 1 {
		t.Fatalf("doc_template_brand = %d rows, want exactly 1", got)
	}
	if got := scalar(t, dst, `SELECT settings_json FROM doc_template_brand WHERE id = 1`); got != `{"wordmark":"Northwind Assurance Ltd"}` {
		t.Errorf("settings_json = %q, want the source's brand", got)
	}
}

// An older backup predates the NFR enrichment corpus. Restoring one must leave
// those tables empty rather than failing — "nothing ingested yet", which is a
// state the page renders.
func TestImport_ToleratesSnapshotWithoutTheEnrichmentTables(t *testing.T) {
	src := openTemp(t, "preenrich_src.db")
	dst := openTemp(t, "preenrich_dst.db")

	mustExec(t, src, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json)
		VALUES (?, ?, ?, '[]', '[]')`, "AC-1", "AC", "Policy")

	snap, err := Export(src)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	snap.Version = 4
	delete(snap.Tables, "nfr_source_documents")

	if _, err := Import(dst, snap); err != nil {
		t.Fatalf("Import of a pre-enrichment snapshot must succeed: %v", err)
	}
	if got := count(t, dst, "nfr_source_documents"); got != 0 {
		t.Errorf("nfr_source_documents = %d, want 0", got)
	}
	if got := count(t, dst, "rcsa_controls"); got != 1 {
		t.Errorf("rcsa_controls = %d, want 1 — the rest of the snapshot must still land", got)
	}
}

// A v2 backup predates doc_template_brand. Restoring one must leave the table
// empty rather than failing, so the install falls back to the shipped brand.
func TestImport_ToleratesSnapshotWithoutTheBrandTable(t *testing.T) {
	src := openTemp(t, "prebrand_src.db")
	dst := openTemp(t, "prebrand_dst.db")

	mustExec(t, src, `INSERT INTO rcsa_controls (control_id, family, name, mapping_baselines_json, threats_json)
		VALUES (?, ?, ?, '[]', '[]')`, "AC-1", "AC", "Policy")

	snap, err := Export(src)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	snap.Version = 2
	delete(snap.Tables, "doc_template_brand")

	if _, err := Import(dst, snap); err != nil {
		t.Fatalf("Import of a pre-brand snapshot must succeed: %v", err)
	}
	if got := count(t, dst, "doc_template_brand"); got != 0 {
		t.Errorf("doc_template_brand = %d, want 0", got)
	}
	if got := count(t, dst, "rcsa_controls"); got != 1 {
		t.Errorf("rcsa_controls = %d, want 1 — the rest of the snapshot must still land", got)
	}
}

// A snapshot row that omits a column must fall back to the schema DEFAULT
// rather than writing an explicit NULL. Writing NULL defeats the DEFAULT and
// fails the NOT NULL constraint, which would make every future column addition
// silently break restore of every older backup.
func TestImport_MissingColumnsFallBackToSchemaDefaults(t *testing.T) {
	dst := openTemp(t, "sparse_dst.db")

	snap := &Snapshot{
		Version:    SnapshotVersion,
		ExportedAt: "2026-01-01T00:00:00Z",
		Tables: map[string][]map[string]any{
			// A minimal row as an older exporter might have written it: the
			// columns that existed then, and nothing else.
			"rcsa_controls": {{
				"control_id":             "AC-1",
				"family":                 "AC",
				"name":                   "Policy and Procedures",
				"mapping_baselines_json": "[]",
				"threats_json":           "[]",
			}},
		},
	}

	if _, err := Import(dst, snap); err != nil {
		t.Fatalf("import of a row missing later columns must succeed: %v", err)
	}
	if got := count(t, dst, "rcsa_controls"); got != 1 {
		t.Fatalf("rcsa_controls = %d, want 1", got)
	}
	// The omitted NOT NULL column took its schema default rather than NULL.
	if got := scalar(t, dst, `SELECT source_key FROM rcsa_controls WHERE control_id = ?`, "AC-1"); got != "" {
		t.Errorf("source_key = %q, want the empty-string default", got)
	}
	if got := scalar(t, dst, `SELECT name FROM rcsa_controls WHERE control_id = ?`, "AC-1"); got != "Policy and Procedures" {
		t.Errorf("supplied column did not survive: %q", got)
	}
}
