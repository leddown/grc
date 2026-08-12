package securitynfr

import (
	"path/filepath"
	"testing"

	"grc/internal/db"
	"grc/internal/nfrfile"
)

func TestSeedTrueSyncRemovesStaleRows(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	jsonPath := filepath.Join(tmpDir, "nfr.json")

	fileData := nfrfile.File{
		"1": {
			Summary:           "0.1 Sample",
			ID:                0.1,
			IssueType:         "Control",
			Description:       "",
			NISTMapping:       "PL-8",
			AdditionalDetails: "details",
			Implementation:    "impl",
			Domain:            "Domain A",
		},
		"2": {
			Summary:           "0.2 Sample",
			ID:                0.2,
			IssueType:         "Control",
			Description:       "",
			NISTMapping:       "CM-8",
			AdditionalDetails: "details",
			Implementation:    "impl",
			Domain:            "Domain B",
		},
	}
	if err := nfrfile.Write(jsonPath, fileData); err != nil {
		t.Fatalf("write json: %v", err)
	}

	sqliteDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqliteDB.Close()

	repo := NewSQLiteRepository(sqliteDB)
	if err := repo.Upsert(NFR{
		Key:         "999",
		ID:          "9.9",
		Summary:     "stale",
		IssueType:   "Control",
		NISTMapping: "AC-1",
	}); err != nil {
		t.Fatalf("insert stale row: %v", err)
	}

	svc := NewService(repo, jsonPath)
	if _, err := svc.Seed(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	items, err := repo.List("", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items after sync, got %d", len(items))
	}
	for _, item := range items {
		if item.Key == "999" {
			t.Fatalf("stale key 999 should have been removed")
		}
	}
}
