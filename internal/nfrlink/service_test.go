package nfrlink

import (
	"path/filepath"
	"reflect"
	"testing"

	"grc/internal/db"
)

func TestExtractMappingTokens(t *testing.T) {
	raw := "PL-8, cm-8, AC-2.1, AC-2(1), not-a-control, SR-11"
	got := extractMappingTokens(raw)
	want := []string{"AC-2(1)", "CM-8", "PL-8", "SR-11"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractMappingTokens() = %#v, want %#v", got, want)
	}
}

func TestService_RebuildAndListMatchesControlsWithoutPerTokenLookupBehaviorChange(t *testing.T) {
	sqlite := openNFRLinkTestDB(t)
	seedNFRLinkFixture(t, sqlite)

	svc := NewService(sqlite)
	if err := svc.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rows, err := svc.List("", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []LinkRow{
		{
			NFRKey:           "1",
			NFRID:            "1.0",
			NFRSummary:       "Summary 1",
			NFRDomain:        "IAM",
			NISTMappingRaw:   "AU-1, AU-1, CM-1",
			MappingControlID: "AU-1",
			ControlID:        "AU-1",
			ControlName:      "Audit Policy",
			ControlFamily:    "AU",
			Matched:          true,
			OverrideApplied:  false,
		},
		{
			NFRKey:           "1",
			NFRID:            "1.0",
			NFRSummary:       "Summary 1",
			NFRDomain:        "IAM",
			NISTMappingRaw:   "AU-1, AU-1, CM-1",
			MappingControlID: "CM-1",
			ControlID:        "",
			ControlName:      "",
			ControlFamily:    "",
			Matched:          false,
			OverrideApplied:  false,
		},
		{
			NFRKey:           "2",
			NFRID:            "2.0",
			NFRSummary:       "Summary 2",
			NFRDomain:        "SecOps",
			NISTMappingRaw:   "",
			MappingControlID: "",
			ControlID:        "",
			ControlName:      "",
			ControlFamily:    "",
			Matched:          false,
			OverrideApplied:  false,
		},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("List got=%#v want=%#v", rows, want)
	}
}

func TestService_RebuildWithMissingOverrideTargetFallsBackToUnmatched(t *testing.T) {
	sqlite := openNFRLinkTestDB(t)
	seedNFRLinkFixture(t, sqlite)

	if _, err := sqlite.Exec(`
		INSERT INTO nfr_control_link_overrides (nfr_key, mapping_control_id, override_control_id, matched)
		VALUES ('1', 'CM-1', 'CM-999', 1)
	`); err != nil {
		t.Fatalf("insert override: %v", err)
	}

	svc := NewService(sqlite)
	if err := svc.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rows, err := svc.List("", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var cm1 *LinkRow
	for i := range rows {
		if rows[i].MappingControlID == "CM-1" {
			cm1 = &rows[i]
			break
		}
	}
	if cm1 == nil {
		t.Fatalf("expected row for CM-1 mapping token")
	}
	if cm1.Matched {
		t.Fatalf("expected CM-1 to be unmatched when override target is missing: %+v", *cm1)
	}
	if cm1.ControlID != "" || cm1.ControlName != "" || cm1.ControlFamily != "" {
		t.Fatalf("expected empty control fields for missing override target: %+v", *cm1)
	}
	if !cm1.OverrideApplied {
		t.Fatalf("expected override_applied=true for CM-1 row")
	}
}

func openNFRLinkTestDB(t *testing.T) *db.Conn {
	t.Helper()

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "nfrlink.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	return sqlite
}

func seedNFRLinkFixture(t *testing.T, sqlite *db.Conn) {
	t.Helper()

	run := func(query string, args ...any) {
		t.Helper()
		if _, err := sqlite.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	run(`INSERT INTO rcsa_controls (source_key, control_id, control_type, name, family, mapping_baselines_json, threats_json)
		VALUES ('1', 'AU-1', 'Control', 'Audit Policy', 'AU', '[]', '[]')`)

	run(`INSERT INTO security_nfrs (
		record_key, nfr_id, summary, issue_type, description, nist_mapping, additional_details, implementation, domain
	) VALUES ('1', '1.0', 'Summary 1', 'Issue', 'Desc', 'AU-1, AU-1, CM-1', '', '', 'IAM')`)
	run(`INSERT INTO security_nfrs (
		record_key, nfr_id, summary, issue_type, description, nist_mapping, additional_details, implementation, domain
	) VALUES ('2', '2.0', 'Summary 2', 'Issue', 'Desc', '', '', '', 'SecOps')`)
}
