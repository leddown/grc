package controlcatalog

import (
	"path/filepath"
	"reflect"
	"testing"

	"grc/internal/controlfile"
	"grc/internal/db"
)

func TestServiceListHonorsHiddenFamilies(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, filepath.Join(t.TempDir(), "controls.json"))

	if err := repo.Upsert(Control{SourceKey: "1", ID: "AU-1", Name: "Audit", Family: "AU"}); err != nil {
		t.Fatalf("Upsert AU-1: %v", err)
	}
	if err := repo.Upsert(Control{SourceKey: "2", ID: "AC-1", Name: "Access", Family: "AC"}); err != nil {
		t.Fatalf("Upsert AC-1: %v", err)
	}
	if err := repo.SetFamilyVisibility("AC", false); err != nil {
		t.Fatalf("SetFamilyVisibility: %v", err)
	}

	got, err := svc.List("", "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "AU-1" {
		t.Fatalf("List returned %+v want only AU-1", got)
	}
}

func TestServiceListByFamilyHonorsHiddenFamilies(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, filepath.Join(t.TempDir(), "controls.json"))

	if err := repo.Upsert(Control{SourceKey: "1", ID: "AU-1", Name: "Audit", Family: "AU"}); err != nil {
		t.Fatalf("Upsert AU-1: %v", err)
	}
	if err := repo.Upsert(Control{SourceKey: "2", ID: "AC-1", Name: "Access", Family: "AC"}); err != nil {
		t.Fatalf("Upsert AC-1: %v", err)
	}
	if err := repo.SetFamilyVisibility("AC", false); err != nil {
		t.Fatalf("SetFamilyVisibility: %v", err)
	}

	visible, err := svc.ListByFamily(" au ")
	if err != nil {
		t.Fatalf("ListByFamily(AU): %v", err)
	}
	if len(visible) != 1 || visible[0].ID != "AU-1" {
		t.Fatalf("ListByFamily(AU) returned %+v want only AU-1", visible)
	}

	hidden, err := svc.ListByFamily("ac")
	if err != nil {
		t.Fatalf("ListByFamily(AC): %v", err)
	}
	if len(hidden) != 0 {
		t.Fatalf("ListByFamily(AC) returned %+v want empty", hidden)
	}
}

func TestServiceUpdatePreservesStoredFieldsAndNormalizes(t *testing.T) {
	repo := openControlCatalogRepo(t)
	svc := NewService(repo, filepath.Join(t.TempDir(), "controls.json"))

	original := Control{
		SourceKey: "7",
		ID:        "AU-1",
		Type:      "Control",
		Name:      "Original",
		Appendix:  map[string]any{"owner": "security"},
	}
	if err := repo.Upsert(original); err != nil {
		t.Fatalf("Upsert original: %v", err)
	}

	updated, err := svc.Update(" au-1 ", Control{
		Name:             " Updated Name ",
		Threats:          []string{"Tampering", "Tampering", ""},
		Baselines:        []string{"Low", "Low"},
		MappingBaselines: Baselines{Low: []string{"AU-1", "AU-1"}},
		Confidentiality:  "yes",
		RelatedControls:  []string{"AC-1", " AC-1 "},
		Requirements:     " req ",
		Discussion:       " disc ",
		Justification:    " why ",
		Availability:     " ",
		Integrity:        "",
		Appendix:         nil,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.SourceKey != "7" || updated.Type != "Control" {
		t.Fatalf("expected SourceKey/Type to be preserved, got %+v", updated)
	}
	if updated.ID != "AU-1" || updated.Family != "AU" || updated.Name != "Updated Name" {
		t.Fatalf("unexpected normalized identity fields: %+v", updated)
	}
	if updated.Confidentiality != "Yes" || updated.Integrity != "" || updated.Availability != "" {
		t.Fatalf("unexpected CIA flags: %+v", updated)
	}
	if !reflect.DeepEqual(updated.Threats, []string{"Tampering"}) {
		t.Fatalf("Threats=%v want=[Tampering]", updated.Threats)
	}
	if !reflect.DeepEqual(updated.Baselines, []string{"Low"}) {
		t.Fatalf("Baselines=%v want=[Low]", updated.Baselines)
	}
	if !reflect.DeepEqual(updated.RelatedControls, []string{"AC-1"}) {
		t.Fatalf("RelatedControls=%v want=[AC-1]", updated.RelatedControls)
	}
	if updated.Appendix["owner"] != "security" {
		t.Fatalf("expected existing appendix to be preserved, got %+v", updated.Appendix)
	}
}

func TestServiceSaveToJSONWritesCurrentControls(t *testing.T) {
	repo := openControlCatalogRepo(t)
	path := filepath.Join(t.TempDir(), "controls.json")
	if err := controlfile.Write(path, controlfile.File{}); err != nil {
		t.Fatalf("seed empty control file: %v", err)
	}

	svc := NewService(repo, path)
	control := Control{
		SourceKey:        "1",
		ID:               "AU-1",
		Type:             "Control",
		Name:             "Audit Policy",
		Threats:          []string{"Repudiation"},
		Confidentiality:  "Yes",
		Requirements:     "Requirement text",
		Discussion:       "Discussion text",
		RelatedControls:  []string{"AU-2"},
		MappingBaselines: Baselines{Low: []string{"AU-1"}},
	}
	if err := repo.Upsert(control); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	count, gotPath, err := svc.SaveToJSON()
	if err != nil {
		t.Fatalf("SaveToJSON: %v", err)
	}
	if count != 1 || gotPath != path {
		t.Fatalf("SaveToJSON=(%d,%q) want (1,%q)", count, gotPath, path)
	}

	fileData, err := controlfile.Read(path)
	if err != nil {
		t.Fatalf("Read written file: %v", err)
	}
	entry := fileData["1"]
	if entry.NIST.ControlID != "AU-1" || entry.NIST.Name != "Audit Policy" {
		t.Fatalf("unexpected entry: %+v", entry.NIST)
	}
	if !entry.NIST.CIA.Confidentiality {
		t.Fatalf("expected confidentiality flag to be written: %+v", entry.NIST.CIA)
	}
}

func openControlCatalogRepo(t *testing.T) *SQLiteRepository {
	t.Helper()

	sqlite, err := db.OpenSQLite(filepath.Join(t.TempDir(), "controls.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	return NewSQLiteRepository(sqlite)
}
