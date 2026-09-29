package app

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"grc/internal/authn"
)

func (a *studioApp) call(t *testing.T, method, path, body, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, a.server.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

const badgeTemplate = `{"id":"badge-access","version":"x","title":"Badge access","doc_type":"work_instruction","frameworks":[],
	"description":"How staff badge in.","review_cadence_months":12,"classification":"Internal","default":false,
	"facts":[{"key":"site_name","label":"Site","description":"","example":"Head office","value_type":"text"}],
	"sections":[{"uid_seed":"purpose","heading":"Purpose","kind":"purpose","content":"Staff at {{fact:site_name}} must badge in.","guidance":"","proposed_mappings":[]},
		{"uid_seed":"steps","heading":"Steps","kind":"procedure_steps","content":"1. Present the badge.","guidance":"","proposed_mappings":[]}]}`

// Templates are administration: a reader with the /policies grant can list
// what New document offers, but not see or change the templates made here.
// An administrator imports one, publishes it, and a document is made from it.
func TestPolicyTemplatesEndToEnd(t *testing.T) {
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "templates.db"), nil)
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/policies/templates/manage", ""},
		{http.MethodGet, "/policies/app-templates", ""},
		{http.MethodGet, "/policies/app-templates/meta", ""},
		{http.MethodPost, "/policies/app-templates/import", badgeTemplate},
		{http.MethodPost, "/policies/app-templates/copy", `{"template_id":"standard-skeleton"}`},
		{http.MethodPut, "/policies/app-templates/1", badgeTemplate},
		{http.MethodPost, "/policies/app-templates/1/publish", ""},
		{http.MethodDelete, "/policies/app-templates/1", ""},
		{http.MethodPost, "/policies/app-templates/draft", `{"library_document_id":7}`},
	} {
		if code, _ := a.call(t, r.method, r.path, r.body, a.reader); code != http.StatusForbidden {
			t.Errorf("%s %s as a reader: %d, want 403", r.method, r.path, code)
		}
		if code, _ := a.call(t, r.method, r.path, r.body, a.outside); code != http.StatusForbidden && code != http.StatusUnauthorized {
			t.Errorf("%s %s without the grant: %d", r.method, r.path, code)
		}
	}

	code, body := a.call(t, http.MethodGet, "/policies/templates/manage", "", a.admin)
	if code != http.StatusOK || !strings.Contains(body, "<h1>Policy Templates</h1>") || !strings.Contains(body, `href="/policies/templates/manage"`) {
		t.Fatalf("the page: %d", code)
	}
	code, body = a.call(t, http.MethodPost, "/policies/app-templates/import", badgeTemplate, a.admin)
	if code != http.StatusCreated {
		t.Fatalf("import: %d %s", code, body)
	}
	var tpl struct {
		ID       int64    `json:"id"`
		Problems []string `json:"problems"`
	}
	_ = json.Unmarshal([]byte(body), &tpl)
	id := strconv.FormatInt(tpl.ID, 10)
	if code, body := a.call(t, http.MethodPost, "/policies/app-templates/"+id+"/publish", "", a.admin); code != http.StatusOK || !strings.Contains(body, `"published_version":"1.0.0"`) {
		t.Fatalf("publish: %d %s", code, body)
	}
	if code, body := a.call(t, http.MethodGet, "/policies/templates", "", a.reader); code != http.StatusOK || !strings.Contains(body, `"id":"badge-access"`) {
		t.Fatalf("New document's list: %d %s", code, body)
	}
	if code, body := a.call(t, http.MethodPost, "/policies/from-template", `{"template_id":"badge-access"}`, a.admin); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("a document from the published template: %d %s", code, body)
	}
	if code, _ := a.call(t, http.MethodDelete, "/policies/app-templates/"+id, "", a.admin); code != http.StatusConflict {
		t.Fatalf("deleting a template a document uses: %d", code)
	}

	// A template with problems is refused with the list of them.
	broken := strings.Replace(badgeTemplate, "must badge in", "will badge in", 1)
	broken = strings.Replace(broken, `"badge-access"`, `"badge-access-two"`, 1)
	_, body = a.call(t, http.MethodPost, "/policies/app-templates/import", broken, a.admin)
	_ = json.Unmarshal([]byte(body), &tpl)
	code, body = a.call(t, http.MethodPost, "/policies/app-templates/"+strconv.FormatInt(tpl.ID, 10)+"/publish", "", a.admin)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `says \"will\"`) {
		t.Fatalf("publishing a template with problems: %d %s", code, body)
	}
	if code, body := a.call(t, http.MethodGet, "/policies/app-templates/"+id+"/template.json", "", a.admin); code != http.StatusOK || !strings.Contains(body, `"version": "1.0.0"`) {
		t.Fatalf("download: %d %s", code, body)
	}
}
