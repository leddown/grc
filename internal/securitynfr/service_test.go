package securitynfr

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"grc/internal/db"
	"grc/internal/nfrfile"
)

func TestImportReplaceRemovesStaleRows(t *testing.T) {
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
	result := importFile(t, svc, jsonPath, true)
	if result.Imported != 2 || result.Removed != 1 {
		t.Fatalf("import result=%+v want 2 imported, 1 removed", result)
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

func importFile(t *testing.T, svc *Service, path string, replace bool) ImportResult {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	result, err := svc.Import(raw, replace)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return result
}

func TestImportMergeKeepsRowsTheFileDoesNotCarry(t *testing.T) {
	svc, repo, jsonPath := newWeightTestService(t, nfrfile.File{"1": {Summary: "From the file"}})
	if err := repo.Upsert(NFR{Key: "7", Summary: "Added in the application", Weight: DefaultWeight}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if result := importFile(t, svc, jsonPath, false); result.Imported != 1 || result.Removed != 0 {
		t.Fatalf("merge result=%+v want 1 imported, 0 removed", result)
	}
	if _, err := svc.Get("7"); err != nil {
		t.Fatalf("merge removed a row the file does not carry: %v", err)
	}
}

func TestImportRefusesABadFileWithoutWriting(t *testing.T) {
	svc, repo, _ := newWeightTestService(t, nfrfile.File{})
	if err := repo.Upsert(NFR{Key: "1", Summary: "Existing", Weight: DefaultWeight}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for name, body := range map[string]string{
		"not json":       `not json`,
		"empty object":   `{}`,
		"no summary":     `{"2": {"Summary": "  "}}`,
		"weight too big": `{"2": {"Summary": "Heavy", "Weight": 9}}`,
		"wrong shape":    `[{"Summary": "a list, not a map"}]`,
	} {
		if _, err := svc.Import([]byte(body), true); !errors.Is(err, ErrInvalidImport) {
			t.Fatalf("%s: err=%v want ErrInvalidImport", name, err)
		}
	}
	items, err := repo.List("", "")
	if err != nil || len(items) != 1 || items[0].Key != "1" {
		t.Fatalf("a refused import changed the catalog: %+v (%v)", items, err)
	}
}

func TestImportNFRsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := newWeightTestService(t, nfrfile.File{})
	links := &countingLinker{}
	router := gin.New()
	router.POST("/security-nfrs/import", NewHandler(svc, links).ImportNFRs)

	post := func(query, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/security-nfrs/import"+query, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := post("", `{"1": {"Summary": "One", "NIST Mapping": "AC-2"}, "2": {"Summary": "Two"}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"imported":2`) {
		t.Fatalf("import: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if links.calls != 1 {
		t.Fatalf("links rebuilt %d times after an import, want 1", links.calls)
	}
	rec = post("?replace=true", `{"1": {"Summary": "One"}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"removed":1`) {
		t.Fatalf("replace import: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := post("", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty file: code=%d want 400", rec.Code)
	}
	if rec := post("", ``); rec.Code != http.StatusBadRequest {
		t.Fatalf("no body: code=%d want 400", rec.Code)
	}
	if links.calls != 2 {
		t.Fatalf("links rebuilt %d times, want 2 (refused imports must not rebuild)", links.calls)
	}
}

type countingLinker struct{ calls int }

func (l *countingLinker) Rebuild() error {
	l.calls++
	return nil
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

func TestImportDefaultsWeightAndKeepsOneSetInTheApplication(t *testing.T) {
	svc, _, jsonPath := newWeightTestService(t, nfrfile.File{
		"1": {Summary: "Unweighted"},
		"2": {Summary: "Weighted in the file", Weight: 5},
	})
	importFile(t, svc, jsonPath, true)
	for key, want := range map[string]int{"1": DefaultWeight, "2": 5} {
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
	importFile(t, svc, jsonPath, false)
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
	svc, _, jsonPath := newWeightTestService(t, nfrfile.File{"1": {Summary: "Seeded"}})
	importFile(t, svc, jsonPath, true)

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
	importFile(t, svc, jsonPath, true)
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
	svc, _, jsonPath := newWeightTestService(t, nfrfile.File{"1": {Summary: "Seeded"}})
	importFile(t, svc, jsonPath, true)
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
	svc, _, jsonPath := newWeightTestService(t, nfrfile.File{
		"1": {Summary: "Seeded", NISTMapping: "AC-2", Domain: "Identity", Implementation: "impl"},
	})
	importFile(t, svc, jsonPath, true)
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
