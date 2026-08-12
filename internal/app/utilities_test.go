package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"carelockconsulting/internal/db"
	"carelockconsulting/internal/dbsync"
)

func newUtilitiesTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "utilities.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	router := gin.New()
	// localMode = true keeps the admin middleware out of the way; this test is
	// about the import gate, not about who may reach it.
	registerUtilitiesRoutes(router, conn, noAuthMiddleware, true)
	return router
}

func postSnapshot(t *testing.T, router *gin.Engine, snap dbsync.Snapshot) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/utilities/import", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// Bumping SnapshotVersion must never make an existing backup unrestorable. The
// handler compared for equality, so raising the constant to 2 silently rejected
// every snapshot taken by an earlier build.
func TestImportAcceptsOlderSnapshotVersions(t *testing.T) {
	router := newUtilitiesTestRouter(t)

	older := dbsync.Snapshot{
		Version:    1,
		ExportedAt: "2026-01-01T00:00:00Z",
		Tables: map[string][]map[string]any{
			"rcsa_controls": {{
				"control_id": "AC-1", "family": "AC", "name": "Policy and Procedures",
				"mapping_baselines_json": "[]", "threats_json": "[]",
			}},
		},
	}

	rec := postSnapshot(t, router, older)
	if rec.Code != http.StatusOK {
		t.Fatalf("importing a v1 snapshot = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// The tables a v1 snapshot does not carry come back empty rather than
	// failing the restore.
	if !strings.Contains(rec.Body.String(), "policy_documents") {
		t.Error("report should still list the tables absent from the older snapshot")
	}
}

func TestImportAcceptsCurrentSnapshotVersion(t *testing.T) {
	router := newUtilitiesTestRouter(t)

	rec := postSnapshot(t, router, dbsync.Snapshot{
		Version:    dbsync.SnapshotVersion,
		ExportedAt: "2026-08-01T00:00:00Z",
		Tables:     map[string][]map[string]any{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("importing the current version = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// A snapshot from a future build may reference tables this binary's schema does
// not have, so it is the one case that must still be refused.
func TestImportRejectsNewerAndInvalidSnapshotVersions(t *testing.T) {
	router := newUtilitiesTestRouter(t)

	for _, version := range []int{dbsync.SnapshotVersion + 1, 0, -1} {
		rec := postSnapshot(t, router, dbsync.Snapshot{
			Version: version,
			Tables:  map[string][]map[string]any{},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("version %d = %d, want 400", version, rec.Code)
		}
	}
}

// Export and import have to round-trip, or the restore path is theoretical.
func TestExportImportRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "roundtrip.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.Exec(`INSERT INTO policy_documents (id, title, doc_type, reference, status)
		VALUES (?, ?, ?, ?, ?)`, 3, "Access Control Policy", "policy", "POL-AC-001", "approved"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := gin.New()
	registerUtilitiesRoutes(router, conn, noAuthMiddleware, true)

	req := httptest.NewRequest(http.MethodGet, "/utilities/export", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d, want 200", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/utilities/import", bytes.NewReader(rec.Body.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("import of our own export = %d, want 200: %s", rec2.Code, rec2.Body.String())
	}

	var title string
	if err := conn.QueryRow(`SELECT title FROM policy_documents WHERE id = ?`, 3).Scan(&title); err != nil {
		t.Fatalf("row did not survive the round trip: %v", err)
	}
	if title != "Access Control Policy" {
		t.Errorf("title = %q after round trip", title)
	}
}
