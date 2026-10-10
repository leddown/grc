package securitynfr

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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

func newWeightTestService(t *testing.T, fileData nfrfile.File) (*Service, *SQLiteRepository, string) {
	t.Helper()
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "nfr.json")
	if err := nfrfile.Write(jsonPath, fileData); err != nil {
		t.Fatalf("write json: %v", err)
	}
	sqliteDB, err := db.OpenSQLite(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqliteDB.Close() })
	repo := NewSQLiteRepository(sqliteDB)
	return NewService(repo, jsonPath), repo, jsonPath
}

func TestSeedDefaultsWeightAndKeepsOneSetInTheApplication(t *testing.T) {
	svc, _, jsonPath := newWeightTestService(t, nfrfile.File{
		"1": {Summary: "Unweighted"},
		"2": {Summary: "Weighted in the file", Weight: 5},
		"3": {Summary: "Out of range in the file", Weight: 9},
	})
	if _, err := svc.Seed(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for key, want := range map[string]int{"1": DefaultWeight, "2": 5, "3": DefaultWeight} {
		item, err := svc.Get(key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		if item.Weight != want {
			t.Fatalf("key %s weight=%d want=%d", key, item.Weight, want)
		}
	}

	if _, err := svc.Update("1", NFR{Summary: "Unweighted", Weight: 4}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := nfrfile.Write(jsonPath, nfrfile.File{
		"1": {Summary: "Unweighted, reworded"},
		"2": {Summary: "Weighted in the file", Weight: 2},
	}); err != nil {
		t.Fatalf("rewrite json: %v", err)
	}
	if _, err := svc.Seed(); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	first, err := svc.Get("1")
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}
	if first.Weight != 4 || first.Summary != "Unweighted, reworded" {
		t.Fatalf("reseed without a weight: got weight=%d summary=%q, want weight 4 kept", first.Weight, first.Summary)
	}
	second, err := svc.Get("2")
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if second.Weight != 2 {
		t.Fatalf("reseed with a weight: got %d want 2", second.Weight)
	}
}

func TestUpdateWeight(t *testing.T) {
	svc, _, _ := newWeightTestService(t, nfrfile.File{"1": {Summary: "Seeded"}})
	if _, err := svc.Seed(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	created, err := svc.Update("50", NFR{Summary: "New"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Weight != DefaultWeight {
		t.Fatalf("new NFR weight=%d want=%d", created.Weight, DefaultWeight)
	}

	for _, weight := range []int{MinWeight, MaxWeight} {
		updated, err := svc.Update("1", NFR{Summary: "Seeded", Weight: weight})
		if err != nil {
			t.Fatalf("update to %d: %v", weight, err)
		}
		if updated.Weight != weight {
			t.Fatalf("weight=%d want=%d", updated.Weight, weight)
		}
	}

	kept, err := svc.Update("1", NFR{Summary: "Seeded, edited"})
	if err != nil {
		t.Fatalf("update without weight: %v", err)
	}
	if kept.Weight != MaxWeight {
		t.Fatalf("an update that names no weight changed it: got %d want %d", kept.Weight, MaxWeight)
	}

	for _, weight := range []int{-1, MaxWeight + 1} {
		if _, err := svc.Update("1", NFR{Summary: "Seeded", Weight: weight}); !errors.Is(err, ErrInvalidWeight) {
			t.Fatalf("weight %d: err=%v want ErrInvalidWeight", weight, err)
		}
	}
}

func TestSaveToJSONWritesWeight(t *testing.T) {
	svc, _, jsonPath := newWeightTestService(t, nfrfile.File{"1": {Summary: "Seeded"}})
	if _, err := svc.Seed(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Update("1", NFR{Summary: "Seeded", Weight: 5}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, _, err := svc.SaveToJSON(); err != nil {
		t.Fatalf("save: %v", err)
	}
	written, err := nfrfile.Read(jsonPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if written["1"].Weight != 5 {
		t.Fatalf("weight in file=%d want 5", written["1"].Weight)
	}
}

func TestUpdateNFRHandlerWeight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := newWeightTestService(t, nfrfile.File{"1": {Summary: "Seeded"}})
	if _, err := svc.Seed(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	router := gin.New()
	router.PUT("/security-nfrs/:key", NewHandler(svc, nil).UpdateNFR)

	put := func(body string) (int, NFR) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/security-nfrs/1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var out NFR
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	if code, out := put(`{"summary":"Seeded","weight":4}`); code != http.StatusOK || out.Weight != 4 {
		t.Fatalf("set weight: code=%d weight=%d", code, out.Weight)
	}
	if code, out := put(`{"summary":"Seeded"}`); code != http.StatusOK || out.Weight != 4 {
		t.Fatalf("omitted weight: code=%d weight=%d want 4 kept", code, out.Weight)
	}
	for _, body := range []string{`{"summary":"Seeded","weight":0}`, `{"summary":"Seeded","weight":6}`, `{"summary":"Seeded","weight":2.5}`} {
		if code, _ := put(body); code != http.StatusBadRequest {
			t.Fatalf("%s: code=%d want 400", body, code)
		}
	}
	if item, err := svc.Get("1"); err != nil || item.Weight != 4 {
		t.Fatalf("rejected updates changed the weight: %d (%v)", item.Weight, err)
	}
}

func TestSetWeightChangesOnlyTheWeight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := newWeightTestService(t, nfrfile.File{
		"1": {Summary: "Seeded", NISTMapping: "AC-2", Domain: "Identity", Implementation: "impl"},
	})
	if _, err := svc.Seed(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := svc.Get("1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	router := gin.New()
	router.PUT("/security-nfrs/:key/weight", NewHandler(svc, nil).SetNFRWeight)

	put := func(key, body string) (int, NFR) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/security-nfrs/"+key+"/weight", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var out NFR
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, out := put("1", `{"weight":5}`)
	want := before
	want.Weight = 5
	if code != http.StatusOK || out != want {
		t.Fatalf("set weight: code=%d got=%+v want=%+v", code, out, want)
	}
	for _, body := range []string{`{}`, `{"weight":null}`, `{"weight":0}`, `{"weight":6}`, `{"weight":"4"}`, `{"weight":2.5}`} {
		if code, _ := put("1", body); code != http.StatusBadRequest {
			t.Fatalf("%s: code=%d want 400", body, code)
		}
	}
	if code, _ := put("404", `{"weight":2}`); code != http.StatusNotFound {
		t.Fatalf("unknown key: code=%d want 404", code)
	}
	if after, err := svc.Get("1"); err != nil || after != want {
		t.Fatalf("after the refused requests: got=%+v (%v) want=%+v", after, err, want)
	}
	if _, err := svc.Get("404"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("setting a weight on an unknown key created it: %v", err)
	}
}
