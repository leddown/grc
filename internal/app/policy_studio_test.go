package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"grc/internal/aiprovider"
	"grc/internal/authn"
	"grc/internal/clientprofile"
	"grc/internal/db"
	"grc/internal/knowledge"
	"grc/internal/policydocs"
	"grc/internal/policystudio"
	"grc/internal/settings"
)

type studioApp struct {
	router   *gin.Engine
	server   *httptest.Server
	studio   *policystudio.Service
	policies *policydocs.Service
	clients  *clientprofile.Service
	know     *knowledge.Service
	conn     *db.Conn
	settings *settings.Service
	doc      policydocs.Document
	admin    string // session tokens
	admin2   string // a second administrator, "bob"
	reader   string
	outside  string
}

// newStudioApp wires the Studio the way Run does, middleware included, with a
// Studio document and three sessions: an admin, a reader with the /policies
// grant, and a user whose grants do not reach the policy library.
func newStudioApp(t *testing.T) *studioApp {
	t.Helper()
	return newStudioAppAt(t, filepath.Join(t.TempDir(), "studio-app.db"), nil)
}

// newStudioAppAt builds the app over dbPath. prev, when given, is an earlier
// instance over the same database: its users, sessions and document are
// reused, which is what a restart looks like.
func newStudioAppAt(t *testing.T, dbPath string, prev *studioApp, ai ...*aiprovider.Router) *studioApp {
	t.Helper()
	gin.SetMode(gin.TestMode)
	conn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	auth := authn.NewService(conn)
	auth.SetSingleUserMode(false)
	session := func(id int64) string {
		s, err := auth.CreateSession(id, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return s.Token
	}

	router := gin.New()
	router.Use(themeMiddleware())
	router.Use(pageAccessMiddleware(auth))
	adminGate := adminTokenMiddleware(auth, "")
	registerPublicPageRoutes(router, false)
	policies := registerPolicyDocRoutes(router, conn, auth, adminGate, false)
	know := knowledge.NewService(knowledge.NewStore(conn))
	var aiRouter *aiprovider.Router
	if len(ai) > 0 {
		aiRouter = ai[0]
		registerAIAuxRoutes(router)
	}
	settingsService := settings.NewService(settings.NewSQLRepository(conn), nil).WithPreferences(settings.NewSQLPreferenceRepository(conn))
	studio, err := registerPolicyStudioRoutes(router, conn, policies, auth, know, aiRouter, settingsService, adminGate, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = studio.Shutdown(context.Background()) })
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	app := &studioApp{router: router, server: srv, studio: studio, policies: policies, clients: clientprofile.NewService(conn), know: know, conn: conn, settings: settingsService}
	if prev != nil {
		app.doc, app.admin, app.admin2, app.reader, app.outside = prev.doc, prev.admin, prev.admin2, prev.reader, prev.outside
		return app
	}

	admin, err := auth.BootstrapAdmin("admin", "very-strong-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	admin2, err := auth.CreateUserWithAccess("bob", "very-strong-pass-4", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := auth.CreateUserWithAccess("rita", "very-strong-pass-2", false, []string{"/policies"})
	if err != nil {
		t.Fatal(err)
	}
	outside, err := auth.CreateUserWithAccess("otto", "very-strong-pass-3", false, []string{"/controls"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := policies.CreateDocument(policydocs.Document{Title: "ICT policy", DocType: policydocs.TypePolicy, OwnerRole: "CISO"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []policydocs.Section{
		{SectionKind: policydocs.KindPurpose, Heading: "Purpose", Body: "Why this policy exists."},
		{SectionKind: policydocs.KindScope, Heading: "Scope", Body: "- all staff\n- all systems"},
	} {
		s.DocumentID = doc.ID
		if _, err := policies.CreateSection(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := studio.Migrate(doc.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	app.doc, app.admin, app.admin2, app.reader, app.outside = doc, session(admin.ID), session(admin2.ID), session(reader.ID), session(outside.ID)
	return app
}

func (a *studioApp) dial(path, token, origin string) int {
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	if token != "" {
		header.Set("Cookie", authn.AuthSessionCookie+"="+token)
	}
	conn, resp, _ := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.server.URL, "http")+path, header)
	if conn != nil {
		_ = conn.Close()
	}
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// Every route under /collab/ runs the collab decision: enumerated from the
// router itself, so a route added later without the gate fails here. /collab/
// is deliberately not a protectedPrefix -- that would answer an upgrade with a
// login redirect -- and nor is the static bundle.
func TestEveryCollabRouteRunsItsGate(t *testing.T) {
	a := newStudioApp(t)
	id := strconv.FormatInt(a.doc.ID, 10)
	found := 0
	for _, r := range a.router.Routes() {
		if !strings.HasPrefix(r.Path, "/collab/") {
			continue
		}
		found++
		path := strings.ReplaceAll(r.Path, ":id", id)
		if got := a.dial(path, "", a.server.URL); got != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d, want 401", r.Path, got)
		}
		if got := a.dial(path, a.admin, "https://evil.example"); got != http.StatusUnauthorized {
			t.Errorf("%s from another origin: %d, want 401", r.Path, got)
		}
		if got := a.dial(path, a.outside, a.server.URL); got != http.StatusUnauthorized {
			t.Errorf("%s for a user without the policy grant: %d, want 401", r.Path, got)
		}
		if got := a.dial(path, a.admin, a.server.URL); got != http.StatusSwitchingProtocols {
			t.Errorf("%s for an admin: %d, want 101 (through the theme middleware)", r.Path, got)
		}
		if got := a.dial(path, a.reader, a.server.URL); got != http.StatusSwitchingProtocols {
			t.Errorf("%s for a reader (read-only): %d, want 101", r.Path, got)
		}
	}
	if found == 0 {
		t.Fatal("no /collab/ routes registered")
	}
	for path, want := range map[string]bool{
		"/collab/policies/1":                  false,
		"/assets/policy-studio/x.js":          false,
		"/policies/1/studio":                  true,
		"/policies/1/studio/state":            true,
		"/policies/1/studio/sections/abc":     true,
		"/policies/1/studio/sections/reorder": true,
	} {
		if isProtectedPath(path) != want {
			t.Errorf("isProtectedPath(%s) = %v, want %v", path, !want, want)
		}
	}
}

func TestStudioPagesFollowThePolicyGrants(t *testing.T) {
	a := newStudioApp(t)
	id := strconv.FormatInt(a.doc.ID, 10)
	do := func(method, path, token string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"kind":"scope"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: token})
		}
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, req)
		return rec.Code
	}
	cases := []struct {
		method, path, token string
		want                int
	}{
		{http.MethodGet, "/policies/" + id + "/studio", a.admin, http.StatusOK},
		{http.MethodGet, "/policies/" + id + "/studio", a.reader, http.StatusOK},
		{http.MethodGet, "/policies/" + id + "/studio", a.outside, http.StatusForbidden},
		{http.MethodGet, "/policies/" + id + "/studio", "", http.StatusUnauthorized},
		{http.MethodGet, "/policies/" + id + "/studio/state", a.reader, http.StatusOK},
		{http.MethodPost, "/policies/" + id + "/studio/sections", a.reader, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/migrate", a.reader, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/sections", a.admin, http.StatusCreated},
		{http.MethodGet, "/policies/999999/studio", a.admin, http.StatusNotFound},
		{http.MethodGet, "/policies/templates", a.reader, http.StatusOK},
		{http.MethodGet, "/policies/clients", a.reader, http.StatusOK},
		{http.MethodGet, "/policies/clients", a.outside, http.StatusForbidden},
		{http.MethodPost, "/policies/clients", a.reader, http.StatusForbidden},
		{http.MethodPost, "/policies/from-template", a.reader, http.StatusForbidden},
		{http.MethodPut, "/policies/clients/1/facts/legal_entity_name", a.reader, http.StatusForbidden},
		{http.MethodGet, "/policies/" + id + "/studio/comments", a.reader, http.StatusOK},
		{http.MethodGet, "/policies/" + id + "/studio/comments", a.outside, http.StatusForbidden},
		{http.MethodGet, "/policies/" + id + "/studio/comments", "", http.StatusUnauthorized},
		{http.MethodGet, "/policies/" + id + "/studio/provenance", a.reader, http.StatusOK},
		{http.MethodGet, "/policies/" + id + "/studio/provenance", a.outside, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/comments", a.reader, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/comments/1/replies", a.reader, http.StatusForbidden},
		{http.MethodPatch, "/policies/" + id + "/studio/comments/1", a.reader, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/decisions", a.reader, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/decisions", "", http.StatusUnauthorized},
		{http.MethodGet, "/policies/" + id + "/studio/ai/status", a.reader, http.StatusOK},
		{http.MethodGet, "/policies/" + id + "/studio/ai/edits", a.reader, http.StatusOK},
		{http.MethodGet, "/policies/" + id + "/studio/ai/status", a.outside, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/ai/proposals", a.reader, http.StatusForbidden},
		{http.MethodPost, "/policies/" + id + "/studio/ai/proposals", "", http.StatusUnauthorized},
		{http.MethodPost, "/policies/" + id + "/studio/ai/proposals/1/placements", a.reader, http.StatusForbidden},
	}
	for _, tc := range cases {
		if got := do(tc.method, tc.path, tc.token); got != tc.want {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}

	// An admin creates a client and a document from the default template; the
	// document's own PUT route still reaches the policy module beside the
	// /policies/clients routes.
	send := func(method, path, token, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: token})
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := send(http.MethodPost, "/policies/clients", a.admin, `{"name":"Example Bank AG"}`); code != http.StatusCreated {
		t.Fatalf("create client: %d %s", code, body)
	}
	if code, body := send(http.MethodPut, "/policies/clients/1/facts/legal_entity_name", a.admin, `{"value":"Example Bank AG"}`); code != http.StatusOK {
		t.Fatalf("set fact: %d %s", code, body)
	}
	code, body := send(http.MethodPost, "/policies/from-template", a.admin, `{"template_id":"ict-infosec-policy","client_profile_id":1}`)
	if code != http.StatusCreated || !strings.Contains(body, `"template_id":"ict-infosec-policy"`) {
		t.Fatalf("from template: %d %s", code, body)
	}
	if code, body := send(http.MethodPut, "/policies/"+id, a.admin, `{"title":"Renamed","doc_type":"policy"}`); code != http.StatusOK {
		t.Fatalf("document update beside the client routes: %d %s", code, body)
	}
}

func TestStudioStateAnswersNotModified(t *testing.T) {
	a := newStudioApp(t)
	path := "/policies/" + strconv.FormatInt(a.doc.ID, 10) + "/studio/state"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: a.reader})
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	etag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || etag == "" {
		t.Fatalf("state: %d, etag %q", rec.Code, etag)
	}
	if !strings.Contains(rec.Body.String(), `"can_edit":false`) || !strings.Contains(rec.Body.String(), `"role":"reader"`) {
		t.Fatalf("a reader must not be offered editing: %s", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: a.reader})
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("unchanged state: %d, want 304", rec.Code)
	}
}

func TestParseStudioOrigins(t *testing.T) {
	good, err := parseStudioOrigins(" https://Policies.Example.com/ , http://10.0.0.5:8443")
	if err != nil || len(good) != 2 || good[0] != "https://policies.example.com" || good[1] != "http://10.0.0.5:8443" {
		t.Fatalf("got %v, %v", good, err)
	}
	for _, bad := range []string{"policies.example.com", "https://*.example.com", "https://a.example/path", "javascript:alert(1)", "https://"} {
		if _, err := parseStudioOrigins(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// An internal comment appears in the internal comment listing and nowhere
// else: no other policy read route, and not the knowledge API an external
// agent reads.
func TestInternalCommentsStayInternal(t *testing.T) {
	a := newStudioApp(t)
	id := strconv.FormatInt(a.doc.ID, 10)
	get := func(path, token string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: token})
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	send := func(method, path, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: a.admin})
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	sections, err := a.policies.ListSections(a.doc.ID)
	if err != nil || len(sections) == 0 {
		t.Fatalf("sections: %v", err)
	}
	const marker = "INTERNAL-7f3c: the client's auditor is not to see this"
	body, _ := json.Marshal(map[string]string{"section_uid": sections[0].UID, "anchor_start": "AQID", "anchor_end": "AQIE",
		"quote": "Why this policy exists.", "visibility": "internal", "body": marker})
	if code, out := send(http.MethodPost, "/policies/"+id+"/studio/comments", string(body)); code != http.StatusCreated {
		t.Fatalf("create comment: %d %s", code, out)
	}
	if code, out := get("/policies/"+id+"/studio/comments", a.reader); code != http.StatusOK || !strings.Contains(out, "INTERNAL-7f3c") {
		t.Fatalf("the internal listing shows it: %d %s", code, out)
	}

	checked := 0
	for _, r := range a.router.Routes() {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.Path, "/policies") || r.Path == "/policies/:id/studio/comments" {
			continue
		}
		path := strings.NewReplacer(":id", id, ":sectionID", strconv.FormatInt(sections[0].ID, 10), ":clientID", "1", ":tid", "1").Replace(r.Path)
		if strings.Contains(path, ":") || strings.Contains(path, "*") {
			t.Errorf("route %s has a parameter this test does not fill in", r.Path)
			continue
		}
		_, out := get(path, a.admin)
		if strings.Contains(out, "INTERNAL-7f3c") {
			t.Errorf("GET %s carries an internal comment", r.Path)
		}
		checked++
	}
	if checked < 20 {
		t.Fatalf("only %d policy read routes checked", checked)
	}
	bundle, err := a.know.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(bundle)
	if strings.Contains(string(raw), "INTERNAL-7f3c") {
		t.Fatal("the knowledge export carries an internal comment")
	}
}

// The dock on a Studio page answers through the proposal engine, which applies
// the document's AI policy before anything is sent.
func TestStudioDockAppliesTheDocumentsAIPolicy(t *testing.T) {
	stub := &aiStub{}
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "dock.db"), nil, stub.router(t))
	d, err := a.policies.GetDocument(a.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	d.AIPolicy = "off"
	if _, err := a.policies.UpdateDocument(d.ID, d); err != nil {
		t.Fatal(err)
	}
	body := `{"question":"make it testable","page":"/policies/` + strconv.FormatInt(a.doc.ID, 10) + `/studio"}`
	req := httptest.NewRequest(http.MethodPost, "/ai-chat/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: a.admin})
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "AI is switched off for this document") || stub.calls != 0 {
		t.Fatalf("dock with AI off: %d %s, %d model calls", rec.Code, rec.Body.String(), stub.calls)
	}
}

// Asked for a stream, the dock on a Studio page gets the answer's text and
// then the result with its proposal, through the theme layer.
func TestStudioDockStreams(t *testing.T) {
	stub := &aiStub{}
	a := newStudioAppAt(t, filepath.Join(t.TempDir(), "dock.db"), nil, stub.router(t))
	body := `{"question":"make it testable","page":"/policies/` + strconv.FormatInt(a.doc.ID, 10) + `/studio"}`
	req := httptest.NewRequest(http.MethodPost, "/ai-chat/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(&http.Cookie{Name: authn.AuthSessionCookie, Value: a.admin})
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	out := rec.Body.String()
	answer := strings.Index(out, "event: answer\ndata: {\"text\":\"I made the purpose testable and widened the scope.\"}")
	result := strings.Index(out, "event: result\n")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") || answer < 0 || result < answer ||
		!strings.Contains(out[result:], `"proposal":{`) || strings.Contains(out, "<script") || stub.calls != 1 {
		t.Fatalf("%d %s %d calls\n%s", rec.Code, rec.Header().Get("Content-Type"), stub.calls, out)
	}
}

// The theme layer buffers a page to inject its chrome; an event stream goes
// through as it is written.
func TestThemeLayerPassesAStreamThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(themeMiddleware())
	read := make(chan struct{})
	r.GET("/s", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		_, _ = c.Writer.WriteString("event: answer\ndata: {}\n\n")
		c.Writer.Flush()
		select {
		case <-read:
		case <-time.After(5 * time.Second):
		}
		_, _ = c.Writer.WriteString("event: result\ndata: {}\n\n")
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/s")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	first := make([]byte, len("event: answer"))
	done := make(chan error, 1)
	go func() { _, err := io.ReadFull(resp.Body, first); done <- err }()
	select {
	case err := <-done:
		if err != nil || string(first) != "event: answer" {
			t.Fatalf("%q %v", first, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first event was held back until the handler finished")
	}
	close(read)
	rest, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(rest), "event: result") {
		t.Fatalf("rest: %q", rest)
	}
}
