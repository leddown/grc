package reports

import (
	"path/filepath"
	"reflect"
	"testing"

	"carelockconsulting/internal/db"
)

func TestService_ListsLinkedAndUnlinkedControlsWithFilters(t *testing.T) {
	sqlite := openTestDB(t)
	seedReportFixture(t, sqlite)

	svc := NewService(sqlite)

	unlinked, err := svc.UnlinkedControls("all")
	if err != nil {
		t.Fatalf("UnlinkedControls(all): %v", err)
	}

	gotUnlinked := extractIDs(unlinked)
	wantUnlinked := []string{"AU-1", "AU-10"}
	if !reflect.DeepEqual(gotUnlinked, wantUnlinked) {
		t.Fatalf("unlinked ids=%v want=%v", gotUnlinked, wantUnlinked)
	}

	linkedEnh, err := svc.LinkedControls("control enhancement")
	if err != nil {
		t.Fatalf("LinkedControls(control enhancement): %v", err)
	}

	gotLinkedEnh := extractIDs(linkedEnh)
	wantLinkedEnh := []string{"AU-3(1)"}
	if !reflect.DeepEqual(gotLinkedEnh, wantLinkedEnh) {
		t.Fatalf("linked enhancement ids=%v want=%v", gotLinkedEnh, wantLinkedEnh)
	}
	if len(linkedEnh) != 1 {
		t.Fatalf("expected one linked enhancement, got %d", len(linkedEnh))
	}
	if !reflect.DeepEqual(linkedEnh[0].LinkedSecurityNFRs, []LinkedSecurityNFR{{Key: "2", ID: "2.0"}}) {
		t.Fatalf("linked enhancement nfrs=%v want=[2.0]", linkedEnh[0].LinkedSecurityNFRs)
	}
	if !reflect.DeepEqual(linkedEnh[0].Threats, []string{"Spoofing", "Tampering", "Tampering"}) {
		t.Fatalf("linked enhancement threats=%v want=%v", linkedEnh[0].Threats, []string{"Spoofing", "Tampering", "Tampering"})
	}
	if linkedEnh[0].Confidentiality != "TRUE" || linkedEnh[0].Integrity != "" || linkedEnh[0].Availability != "TRUE" {
		t.Fatalf("linked enhancement CIA=%q/%q/%q want TRUE/\"\"/TRUE", linkedEnh[0].Confidentiality, linkedEnh[0].Integrity, linkedEnh[0].Availability)
	}
}

func TestService_TotalControlsRespectsTypeAndVisibility(t *testing.T) {
	sqlite := openTestDB(t)
	seedReportFixture(t, sqlite)

	svc := NewService(sqlite)

	totalAll, err := svc.TotalControls("all")
	if err != nil {
		t.Fatalf("TotalControls(all): %v", err)
	}
	if totalAll != 4 {
		t.Fatalf("TotalControls(all)=%d want=4", totalAll)
	}

	totalControl, err := svc.TotalControls("control")
	if err != nil {
		t.Fatalf("TotalControls(control): %v", err)
	}
	if totalControl != 3 {
		t.Fatalf("TotalControls(control)=%d want=3", totalControl)
	}

	totalEnh, err := svc.TotalControls("control enhancement")
	if err != nil {
		t.Fatalf("TotalControls(control enhancement): %v", err)
	}
	if totalEnh != 1 {
		t.Fatalf("TotalControls(control enhancement)=%d want=1", totalEnh)
	}
}

func TestService_ReportDataRebuildsOnceAndReturnsCounts(t *testing.T) {
	sqlite := openTestDB(t)
	seedReportFixture(t, sqlite)

	svc := NewService(sqlite)

	report, err := svc.ReportData("control enhancement", "linked")
	if err != nil {
		t.Fatalf("ReportData(control enhancement, linked): %v", err)
	}
	if report.UnlinkedCount != 0 || report.LinkedCount != 1 || report.TotalControls != 1 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if got := extractIDs(report.Items); !reflect.DeepEqual(got, []string{"AU-3(1)"}) {
		t.Fatalf("report items=%v want=%v", got, []string{"AU-3(1)"})
	}
}

func TestService_ReportDataAllModeReturnsLinkedAndUnlinkedItems(t *testing.T) {
	sqlite := openTestDB(t)
	seedReportFixture(t, sqlite)

	svc := NewService(sqlite)

	report, err := svc.ReportData("control", "all")
	if err != nil {
		t.Fatalf("ReportData(control, all): %v", err)
	}
	if report.UnlinkedCount != 2 || report.LinkedCount != 1 || report.TotalControls != 3 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if got := extractIDs(report.Items); !reflect.DeepEqual(got, []string{"AU-1", "AU-2", "AU-10"}) {
		t.Fatalf("report items=%v want=%v", got, []string{"AU-1", "AU-2", "AU-10"})
	}
}

func openTestDB(t *testing.T) *db.Conn {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "reports.db")
	sqlite, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	return sqlite
}

func seedReportFixture(t *testing.T, sqlite *db.Conn) {
	t.Helper()

	run := func(query string, args ...any) {
		t.Helper()
		if _, err := sqlite.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	run(`INSERT INTO rcsa_controls (
		source_key, control_id, control_type, name, family, mapping_baselines_json, threats_json,
		confidentiality_status, integrity_status, availability_status
	) VALUES ('1', 'AU-1', 'Control', 'Audit policy', 'AU', '[]', '[]', '', '', '')`)
	run(`INSERT INTO rcsa_controls (
		source_key, control_id, control_type, name, family, mapping_baselines_json, threats_json,
		confidentiality_status, integrity_status, availability_status
	) VALUES ('2', 'AU-2', 'Control', 'Events', 'AU', '[]', '["Repudiation"]', '', 'TRUE', '')`)
	run(`INSERT INTO rcsa_controls (
		source_key, control_id, control_type, name, family, mapping_baselines_json, threats_json,
		confidentiality_status, integrity_status, availability_status
	) VALUES ('3', 'AU-10', 'Control', 'Non-repudiation', 'AU', '[]', '["Denial of Service"]', '', '', '')`)
	run(`INSERT INTO rcsa_controls (
		source_key, control_id, control_type, name, family, mapping_baselines_json, threats_json,
		confidentiality_status, integrity_status, availability_status
	) VALUES ('4', 'AU-3(1)', 'Control Enhancement', 'Content details', 'AU', '[]', '["Spoofing", "Tampering", "Tampering"]', 'TRUE', '', 'TRUE')`)
	run(`INSERT INTO rcsa_controls (
		source_key, control_id, control_type, name, family, mapping_baselines_json, threats_json,
		confidentiality_status, integrity_status, availability_status
	) VALUES ('5', 'CM-1', 'Control', 'Config policy', 'CM', '[]', '[]', 'TRUE', 'TRUE', 'TRUE')`)

	run(`INSERT INTO control_family_visibility (family, enabled) VALUES ('CM', 0)`)

	run(`INSERT INTO security_nfr_control_links (
		nfr_key, nfr_id, nfr_summary, nfr_domain, nist_mapping_raw,
		mapping_control_id, control_id, control_name, control_family, matched
	) VALUES ('1', '1.0', 'nfr1', 'domain', 'AU-2', 'AU-2', 'AU-2', 'Events', 'AU', 1)`)
	run(`INSERT INTO security_nfr_control_links (
		nfr_key, nfr_id, nfr_summary, nfr_domain, nist_mapping_raw,
		mapping_control_id, control_id, control_name, control_family, matched
	) VALUES ('2', '2.0', 'nfr2', 'domain', 'AU-3(1)', 'AU-3(1)', 'AU-3(1)', 'Content details', 'AU', 1)`)
}

func extractIDs(items []ControlSummary) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ControlID)
	}
	return ids
}

func TestSplitLinkedNFRRefs(t *testing.T) {
	got := splitLinkedNFRRefs(" 2|2.0,1|1.0,2|2.0 ,, 3|3.1 ")
	want := []LinkedSecurityNFR{
		{Key: "1", ID: "1.0"},
		{Key: "2", ID: "2.0"},
		{Key: "3", ID: "3.1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitLinkedNFRRefs got=%v want=%v", got, want)
	}
}
