package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grc/internal/authn"
)

// oscalRoutes are the routes the OSCAL work adds. The access model fails open
// (docs/SECURITY.md, issue 1): a path is public unless a prefix list covers it,
// so each new route is checked here against an anonymous request.
var oscalRoutes = []struct {
	method, path string
	adminOnly    bool
}{
	{http.MethodPost, "/controls/import-oscal/preview", true},
	{http.MethodPost, "/controls/import-oscal", true},
}

func TestOSCALRoutesRefuseAnonymousAndNonAdminRequests(t *testing.T) {
	a := newFullApp(t, Options{})
	auth := authn.NewService(a.conn)
	auth.SetSingleUserMode(false)
	reader, err := auth.CreateUser("reader", "very-strong-pass-2", false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSession(reader.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	readerCookie := &http.Cookie{Name: authn.AuthSessionCookie, Value: session.Token}

	for _, r := range oscalRoutes {
		if rec := a.do(r.method, r.path, `{}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d, want 401", r.method, r.path, rec.Code)
		}
		rec := a.do(r.method, r.path, `{}`, readerCookie)
		if r.adminOnly && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a non-admin: %d, want 403", r.method, r.path, rec.Code)
		}
	}
}

func oscalFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "oscal", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// shippedEntries is a catalog file in the JSON import's shape holding the
// named controls from the catalog grc ships, appendix and all.
func shippedEntries(t *testing.T, ids ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "data", "merged_nist_controls_master_replaced_from_controls_all.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[string]json.RawMessage{}
	for key, entry := range all {
		var e struct {
			NIST struct {
				ControlID string `json:"control_id"`
			} `json:"nist"`
		}
		if err := json.Unmarshal(entry, &e); err != nil {
			t.Fatal(err)
		}
		if want[e.NIST.ControlID] {
			out[key] = entry
		}
	}
	if len(out) != len(ids) {
		t.Fatalf("found %d of %d controls in the shipped catalog", len(out), len(ids))
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type grcOwnColumns struct {
	SourceKey, C, I, A, Justification, Threats, PCI, Appendix string
}

func readGRCOwnColumns(t *testing.T, a *fullApp, id string) grcOwnColumns {
	t.Helper()
	var c grcOwnColumns
	if err := a.conn.QueryRow(`SELECT source_key, confidentiality_status, integrity_status, availability_status,
		justification, threats_json, potentially_common_inheritable, appendix_json FROM nist_controls WHERE control_id = ?`, id).
		Scan(&c.SourceKey, &c.C, &c.I, &c.A, &c.Justification, &c.Threats, &c.PCI, &c.Appendix); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return c
}

// The whole path: the fixture cut from NIST's catalog, read by internal/oscal,
// adapted in internal/app, previewed and applied by the control catalog over
// controls first loaded by the JSON import.
func TestOSCALImportThroughTheApp(t *testing.T) {
	a := newFullApp(t, Options{})
	ids := []string{"AC-1", "AC-2", "AC-2(1)", "AC-13"}
	if rec := a.asAdmin(http.MethodPost, "/controls/import", shippedEntries(t, ids...)); rec.Code != http.StatusOK {
		t.Fatalf("JSON import: %d %s", rec.Code, rec.Body.String())
	}
	before := map[string]grcOwnColumns{}
	for _, id := range ids {
		before[id] = readGRCOwnColumns(t, a, id)
	}
	if !strings.Contains(before["AC-1"].Appendix, "References") {
		t.Fatalf("the shipped AC-1 should carry References in its appendix: %s", before["AC-1"].Appendix)
	}

	body, _ := json.Marshal(map[string]any{
		"catalog": oscalFixture(t, "NIST_SP-800-53_rev5_catalog_cut.json"),
		"profiles": map[string]string{
			"low":      oscalFixture(t, "NIST_SP-800-53_rev5_LOW-baseline_profile_cut.json"),
			"moderate": oscalFixture(t, "NIST_SP-800-53_rev5_MODERATE-baseline_profile_cut.json"),
		},
	})
	rec := a.asAdmin(http.MethodPost, "/controls/import-oscal/preview", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Source struct {
			Version, OSCALVersion, SHA256 string
		} `json:"source"`
		Problems []string `json:"problems"`
		Added    []struct {
			ID string `json:"id"`
		} `json:"added"`
		Changed []struct {
			ID     string `json:"id"`
			Fields []struct {
				Field string `json:"field"`
			} `json:"fields"`
		} `json:"changed"`
		Unchanged []string `json:"unchanged"`
		Absent    []string `json:"absent"`
		Base      string   `json:"base"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Problems) != 0 || preview.Base == "" {
		t.Fatalf("problems %v base %q", preview.Problems, preview.Base)
	}
	if len(preview.Added) != 1 || preview.Added[0].ID != "SA-24" {
		t.Fatalf("added %+v", preview.Added)
	}
	if len(preview.Absent) != 0 {
		t.Fatalf("absent %v", preview.Absent)
	}
	changed := map[string][]string{}
	for _, c := range preview.Changed {
		for _, f := range c.Fields {
			changed[c.ID] = append(changed[c.ID], f.Field)
		}
	}
	// AC-1's requirements render exactly as the shipped catalog holds them, so
	// the import adds the structure and the newer discussion but not new text.
	for _, f := range changed["AC-1"] {
		if f == "requirements" || f == "name" || f == "type" || strings.HasPrefix(f, "baseline") {
			t.Errorf("AC-1 should not change %s", f)
		}
	}
	if len(changed["AC-13"]) != 0 {
		t.Errorf("the withdrawn AC-13 is already as NIST states it; changes %v", changed["AC-13"])
	}

	body, _ = json.Marshal(map[string]any{
		"catalog": oscalFixture(t, "NIST_SP-800-53_rev5_catalog_cut.json"),
		"profiles": map[string]string{
			"low":      oscalFixture(t, "NIST_SP-800-53_rev5_LOW-baseline_profile_cut.json"),
			"moderate": oscalFixture(t, "NIST_SP-800-53_rev5_MODERATE-baseline_profile_cut.json"),
		},
		"base": preview.Base,
	})
	rec = a.asAdmin(http.MethodPost, "/controls/import-oscal", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"links_rebuilt":true`) || !strings.Contains(rec.Body.String(), `"applied":true`) {
		t.Fatalf("apply result %s", rec.Body.String())
	}
	for _, id := range ids {
		if got := readGRCOwnColumns(t, a, id); got != before[id] {
			t.Errorf("%s: grc's own columns changed\n before %+v\n after  %+v", id, before[id], got)
		}
	}

	// The same base again: the catalog it describes is gone.
	if rec := a.asAdmin(http.MethodPost, "/controls/import-oscal", string(body)); rec.Code != http.StatusConflict {
		t.Fatalf("a second apply of the same preview: %d %s", rec.Code, rec.Body.String())
	}

	rec = a.asAdmin(http.MethodGet, "/controls/detail/AC-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: %d", rec.Code)
	}
	for _, want := range []string{"Parameters (ODPs) · 9", "AC-01_ODP[01]", "Assessment objectives (800-53A)", "EXAMINE"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the AC-1 detail page lacks %q", want)
		}
	}
	rec = a.asAdmin(http.MethodGet, "/controls/data/SA-24", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"params":[`) {
		t.Fatalf("SA-24: %d %.300s", rec.Code, rec.Body.String())
	}

	page := a.asAdmin(http.MethodGet, "/controls/manage", "").Body.String()
	for _, want := range []string{`id="oscalBtn"`, "Import OSCAL", `id="oscalCatalog"`, `data-slot="privacy"`, "/controls/import-oscal/preview"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Control Editor lacks %q", want)
		}
	}
}
