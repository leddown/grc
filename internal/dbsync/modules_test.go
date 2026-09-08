package dbsync

import (
	"bytes"
	"testing"

	"grc/internal/db"
)

// pdfBytes is a source file's first bytes, invalid UTF-8 on purpose: coercing a
// BLOB to a Go string and letting encoding/json replace every bad byte with
// U+FFFD is exactly how a snapshot ends up importing cleanly and restoring a
// corrupt document.
var pdfBytes = []byte{0x25, 0x50, 0x44, 0x46, 0x2d, 0x31, 0x2e, 0x37, 0x00, 0xff, 0xfe, 0x80, 0x0a}

func seedModules(t *testing.T, conn *db.Conn) {
	t.Helper()

	mustExec(t, conn, `INSERT INTO reg_coverage_regulations (id, title, framework, body_text, status)
		VALUES (1, 'DORA', 'dora', 'Article 5 text', 'analyzed')`)
	mustExec(t, conn, `INSERT INTO reg_coverage_sources (regulation_id, media_type, filename, byte_size, content)
		VALUES (1, 'application/pdf', 'dora.pdf', ?, ?)`, len(pdfBytes), pdfBytes)
	mustExec(t, conn, `INSERT INTO reg_coverage_sections (id, regulation_id, ref, title, body, position)
		VALUES (5, 1, 'Art. 5', 'Governance', 'The management body shall...', 0)`)
	mustExec(t, conn, `INSERT INTO reg_coverage_findings (id, regulation_id, section_id, relevant, requirement, revision)
		VALUES (9, 1, 5, 1, 'Board oversight of ICT risk', 0)`)
	mustExec(t, conn, `INSERT INTO reg_coverage_mappings (id, finding_id, kind, ref, title)
		VALUES (11, 9, 'control', 'PM-1', 'Information Security Program Plan')`)
	mustExec(t, conn, `INSERT INTO reg_coverage_versions (id, regulation_id, number, summary, snapshot)
		VALUES (2, 1, 1, 'First pass', '{}')`)
	mustExec(t, conn, `INSERT INTO reg_coverage_chat (id, regulation_id, role, content)
		VALUES (3, 1, 'user', 'Which articles are unmapped?')`)

	mustExec(t, conn, `INSERT INTO crisis_ex_exercises (id, reference, title, format, status)
		VALUES (4, 'CX-2026-01', 'Ransomware in payments', 'tabletop', 'reported')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_objectives (id, exercise_id, ordinal, code, text)
		VALUES (6, 4, 0, 'OBJ-1', 'Classify the incident under DORA within the clock')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_phases (id, exercise_id, ordinal, phase_key, name)
		VALUES (7, 4, 0, 'detection', 'Detection')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_injects (id, exercise_id, phase_id, ordinal, code, title, body)
		VALUES (8, 4, 7, 0, 'INJ-1', 'Payments queue stalls', 'The settlement queue stops draining.')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_responses (id, exercise_id, inject_id, outcome, observations)
		VALUES (10, 4, 8, 'partial', 'Escalation took 40 minutes.')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_decisions (id, exercise_id, phase_id, title, decision)
		VALUES (12, 4, 7, 'Invoke crisis management', 'Invoked at T+35')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_classification (exercise_id, major, rationale)
		VALUES (4, 1, 'Critical service affected beyond the threshold')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_clocks (id, exercise_id, ordinal, regime, authority, label, due_offset)
		VALUES (13, 4, 0, 'DORA', 'NCA', 'Initial notification', 240)`)
	mustExec(t, conn, `INSERT INTO crisis_ex_findings (id, exercise_id, phase_id, ordinal, code, title, severity)
		VALUES (14, 4, 7, 0, 'F-1', 'No owner for the classification call', 'high')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_participants (id, exercise_id, name, role_key, player)
		VALUES (15, 4, 'A. Okafor', 'ciso', 1)`)
	mustExec(t, conn, `INSERT INTO crisis_ex_references (id, exercise_id, owner_kind, owner_id, ref_kind, ref, title)
		VALUES (16, 4, 'objective', 6, 'control', 'IR-4', 'Incident Handling')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_versions (id, exercise_id, number, kind, summary, snapshot)
		VALUES (17, 4, 1, 'report', 'Final report', '{}')`)
	mustExec(t, conn, `INSERT INTO crisis_ex_chat (id, exercise_id, role, content)
		VALUES (18, 4, 'user', 'Summarise the findings')`)
}

// checkModules asserts the seeded rows arrived intact, ids and all.
func checkModules(t *testing.T, dst *db.Conn) {
	t.Helper()

	for table, want := range map[string]int{
		"reg_coverage_regulations": 1, "reg_coverage_sources": 1, "reg_coverage_sections": 1,
		"reg_coverage_findings": 1, "reg_coverage_mappings": 1, "reg_coverage_versions": 1,
		"reg_coverage_chat":   1,
		"crisis_ex_exercises": 1, "crisis_ex_objectives": 1, "crisis_ex_phases": 1,
		"crisis_ex_injects": 1, "crisis_ex_responses": 1, "crisis_ex_decisions": 1,
		"crisis_ex_classification": 1, "crisis_ex_clocks": 1, "crisis_ex_findings": 1,
		"crisis_ex_participants": 1, "crisis_ex_references": 1, "crisis_ex_versions": 1,
		"crisis_ex_chat": 1,
	} {
		if got := count(t, dst, table); got != want {
			t.Errorf("%s count=%d want %d", table, got, want)
		}
	}

	// Foreign keys still resolve, which is what carrying the ids is for.
	if got := scalar(t, dst, `SELECT s.title FROM reg_coverage_findings f
		JOIN reg_coverage_sections s ON s.id = f.section_id`); got != "Governance" {
		t.Errorf("finding's section title=%q want Governance", got)
	}
	if got := scalar(t, dst, `SELECT i.title FROM crisis_ex_responses r
		JOIN crisis_ex_injects i ON i.id = r.inject_id`); got != "Payments queue stalls" {
		t.Errorf("response's inject title=%q want 'Payments queue stalls'", got)
	}
	if got := scalar(t, dst, `SELECT rationale FROM crisis_ex_classification WHERE exercise_id = 4`); got == "" {
		t.Error("classification rationale is empty")
	}

	var content []byte
	if err := dst.QueryRow(`SELECT content FROM reg_coverage_sources WHERE regulation_id = 1`).Scan(&content); err != nil {
		t.Fatalf("read source blob: %v", err)
	}
	if !bytes.Equal(content, pdfBytes) {
		t.Errorf("source blob = %v, want %v (binary column must survive byte for byte)", content, pdfBytes)
	}
}

// The modules that were missing from the backup entirely: a snapshot has to
// carry Regulation Coverage and the crisis exercises, or a restore comes up
// looking healthy with both of them empty.
func TestExportImport_CarriesRegulationCoverageAndCrisisExercises(t *testing.T) {
	src := openTemp(t, "src.db")
	dst := openTemp(t, "dst.db")
	seedModules(t, src)

	snap, err := Export(src)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, err := Import(dst, roundTrip(t, snap)); err != nil {
		t.Fatalf("Import: %v", err)
	}
	checkModules(t, dst)
}

// Sync carries the same rows over a live connection, where the blob stays bytes
// rather than passing through base64.
func TestSync_CarriesRegulationCoverageAndCrisisExercises(t *testing.T) {
	src := openTemp(t, "src.db")
	dst := openTemp(t, "dst.db")
	seedModules(t, src)

	if _, err := Sync(src, dst); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	checkModules(t, dst)
}

// An older snapshot predates both modules. It has to restore with them empty
// rather than be rejected — the rule every version bump here is bound by.
func TestImport_ToleratesSnapshotWithoutTheNewModules(t *testing.T) {
	dst := openTemp(t, "dst.db")

	older := &Snapshot{
		Version:    7,
		ExportedAt: "2026-01-01T00:00:00Z",
		Tables: map[string][]map[string]any{
			"rcsa_controls": {{
				"control_id": "AC-1", "family": "AC", "name": "Policy",
				"mapping_baselines_json": "[]", "threats_json": "[]",
			}},
		},
	}
	if _, err := Import(dst, roundTrip(t, older)); err != nil {
		t.Fatalf("Import of a v7 snapshot: %v", err)
	}
	if got := count(t, dst, "rcsa_controls"); got != 1 {
		t.Fatalf("rcsa_controls count=%d want 1", got)
	}
	if got := count(t, dst, "reg_coverage_regulations"); got != 0 {
		t.Fatalf("reg_coverage_regulations count=%d want 0", got)
	}
	if got := count(t, dst, "crisis_ex_exercises"); got != 0 {
		t.Fatalf("crisis_ex_exercises count=%d want 0", got)
	}
}

// A blob column that is NULL-free but absent from the row, and one that is not
// valid base64, are the two ways a hand-edited snapshot reaches decodeBlob.
func TestDecodeBlobRejectsNonBase64(t *testing.T) {
	if _, err := decodeBlob("not base64!!"); err == nil {
		t.Fatal("expected an error for a non-base64 blob value")
	}
	got, err := decodeBlob(nil)
	if err != nil || got != nil {
		t.Fatalf("decodeBlob(nil) = %v, %v; want nil, nil", got, err)
	}
}
