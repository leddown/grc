package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"grc/internal/authn"
	"grc/internal/db"
	"grc/internal/doctemplate"
	"grc/internal/policystudio"
)

// fullApp is the whole application, built as Run builds it, over a temporary
// database, with an administrator signed in and guest links switched on.
type fullApp struct {
	t      *testing.T
	router *gin.Engine
	server *httptest.Server
	conn   *db.Conn
	admin  string
}

func newFullApp(t *testing.T, options Options) *fullApp {
	t.Helper()
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "full.db")
	conn, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	options.SQLitePath = path
	router, studio, err := buildRouter(conn, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = studio.Shutdown(t.Context()) })
	a := &fullApp{t: t, router: router, conn: conn}
	a.server = httptest.NewServer(router)
	t.Cleanup(a.server.Close)
	if !options.LocalMode {
		auth := authn.NewService(conn)
		admin, err := auth.BootstrapAdmin("admin", "very-strong-pass-1")
		if err != nil {
			t.Fatal(err)
		}
		s, err := auth.CreateSession(admin.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		a.admin = s.Token
	}
	return a
}

func (a *fullApp) do(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if strings.HasPrefix(body, "name=") {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

func (a *fullApp) asAdmin(method, path, body string) *httptest.ResponseRecorder {
	if a.admin == "" {
		return a.do(method, path, body)
	}
	return a.do(method, path, body, &http.Cookie{Name: authn.AuthSessionCookie, Value: a.admin})
}

// studioDocument creates a Studio document from the default template.
func (a *fullApp) studioDocument() int64 {
	a.t.Helper()
	rec := a.asAdmin(http.MethodPost, "/policies/from-template", `{"template_id":"ict-infosec-policy"}`)
	if rec.Code != http.StatusCreated {
		a.t.Fatalf("from template: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Document struct {
			ID int64 `json:"id"`
		} `json:"document"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Document.ID
}

// invite creates a link and redeems it, returning the guest cookie.
func (a *fullApp) invite(doc int64, body string) (*http.Cookie, int64, string) {
	a.t.Helper()
	rec := a.asAdmin(http.MethodPost, "/policies/"+strconv.FormatInt(doc, 10)+"/share-links", body)
	if rec.Code != http.StatusCreated {
		a.t.Fatalf("create link: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Link struct {
			ID int64 `json:"id"`
		} `json:"link"`
		Path string `json:"path"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	join := a.do(http.MethodPost, out.Path, "name=Carla")
	if join.Code != http.StatusSeeOther || join.Header().Get("Location") != "/shared/studio" {
		a.t.Fatalf("join: %d %s", join.Code, join.Body.String())
	}
	for _, c := range join.Result().Cookies() {
		if c.Name == policystudio.GuestCookie {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				a.t.Fatalf("guest cookie flags: %+v", c)
			}
			return c, out.Link.ID, out.Path
		}
	}
	a.t.Fatal("no guest cookie")
	return nil, 0, ""
}

var routeParam = regexp.MustCompile(`[:*][A-Za-z]+`)

// A guest cookie grants nothing outside /shared and /collab: on every other
// route of the application, a request that carries one is answered exactly as
// a request that carries nothing.
func TestGuestsReachNothingElse(t *testing.T) {
	a := newFullApp(t, Options{})
	if rec := a.asAdmin(http.MethodPut, "/api/settings/policy-studio", `{"guest_links":true}`); rec.Code != http.StatusOK {
		t.Fatalf("enable guest links: %d %s", rec.Code, rec.Body.String())
	}
	doc := a.studioDocument()
	guest, _, _ := a.invite(doc, `{"role":"editor","label":"workshop"}`)
	id := strconv.FormatInt(doc, 10)

	checked := 0
	for _, r := range a.router.Routes() {
		if strings.HasPrefix(r.Path, "/shared/") || strings.HasPrefix(r.Path, "/collab/") {
			continue
		}
		path := routeParam.ReplaceAllStringFunc(r.Path, func(p string) string {
			if p == ":id" {
				return id
			}
			return "1"
		})
		anonymous := a.do(r.Method, path, "{}")
		withGuest := a.do(r.Method, path, "{}", guest)
		if anonymous.Code != withGuest.Code {
			t.Errorf("%s %s: %d with a guest cookie, %d with none", r.Method, r.Path, withGuest.Code, anonymous.Code)
		}
		checked++
	}
	if checked < 150 {
		t.Fatalf("only %d routes checked; the router looks incomplete", checked)
	}

	// Inside /shared the cookie reaches its own document, and only that.
	state := a.do(http.MethodGet, "/shared/api/state", "", guest)
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"id":`+id+`,`) || strings.Contains(state.Body.String(), `"findings":[{`) {
		t.Fatalf("guest state: %d %s", state.Code, state.Body.String()[:min(300, len(state.Body.String()))])
	}
	if rec := a.do(http.MethodGet, "/shared/api/state", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest API without a session: %d", rec.Code)
	}
	if rec := a.do(http.MethodGet, "/shared/api/ai/status", "", guest); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "does not include the AI assistant") {
		t.Fatalf("AI for a link without it: %d %s", rec.Code, rec.Body.String())
	}
}

// An internal comment never reaches a guest; a shared one does.
func TestGuestsNeverReceiveInternalComments(t *testing.T) {
	a := newFullApp(t, Options{})
	a.asAdmin(http.MethodPut, "/api/settings/policy-studio", `{"guest_links":true}`)
	doc := a.studioDocument()
	id := strconv.FormatInt(doc, 10)
	var uid string
	_ = a.conn.QueryRow(`SELECT uid FROM policy_sections WHERE document_id = ? ORDER BY ordinal LIMIT 1`, doc).Scan(&uid)
	for _, c := range []struct{ vis, body string }{{"internal", "INTERNAL-9d1e: they will push back"}, {"shared", "SHARED-4b2a: please confirm"}} {
		if rec := a.asAdmin(http.MethodPost, "/policies/"+id+"/studio/comments", `{"section_uid":"`+uid+`","anchor_start":"AQID","anchor_end":"AQIE","visibility":"`+c.vis+`","body":"`+c.body+`"}`); rec.Code != http.StatusCreated {
			t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
		}
	}
	guest, _, _ := a.invite(doc, `{"role":"commenter"}`)
	for _, path := range []string{"/shared/api/state", "/shared/api/comments", "/shared/studio"} {
		body := a.do(http.MethodGet, path, "", guest).Body.String()
		if strings.Contains(body, "INTERNAL-9d1e") {
			t.Fatalf("%s carries an internal comment", path)
		}
	}
	if body := a.do(http.MethodGet, "/shared/api/comments", "", guest).Body.String(); !strings.Contains(body, "SHARED-4b2a") {
		t.Fatalf("the shared comment is missing: %s", body)
	}
	// A guest's own comment is shared, whatever it asks for.
	rec := a.do(http.MethodPost, "/shared/api/comments", `{"section_uid":"`+uid+`","anchor_start":"AQID","anchor_end":"AQIE","visibility":"internal","body":"From Carla"}`, guest)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"visibility":"shared"`) || !strings.Contains(rec.Body.String(), `"author_kind":"guest"`) {
		t.Fatalf("guest comment: %d %s", rec.Code, rec.Body.String())
	}
}

// Revocation ends the guest's access at once, and an expired link is refused.
func TestGuestRevocationAndExpiry(t *testing.T) {
	a := newFullApp(t, Options{})
	a.asAdmin(http.MethodPut, "/api/settings/policy-studio", `{"guest_links":true}`)
	doc := a.studioDocument()
	id := strconv.FormatInt(doc, 10)
	guest, linkID, path := a.invite(doc, `{"role":"viewer"}`)
	if rec := a.do(http.MethodGet, "/shared/api/state", "", guest); rec.Code != http.StatusOK {
		t.Fatalf("before revocation: %d", rec.Code)
	}
	if rec := a.asAdmin(http.MethodDelete, "/policies/"+id+"/share-links/"+strconv.FormatInt(linkID, 10), ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := a.do(http.MethodGet, "/shared/api/state", "", guest); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after revocation: %d", rec.Code)
	}
	if rec := a.do(http.MethodGet, path, ""); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "withdrawn") {
		t.Fatalf("a revoked link's page: %d", rec.Code)
	}

	_, _, expiring := a.invite(doc, `{"role":"viewer"}`)
	if _, err := a.conn.Exec(`UPDATE policy_share_links SET expires_at = '2000-01-01T00:00:00Z' WHERE revoked_at = ''`); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(http.MethodGet, expiring, ""); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("an expired link's page: %d %s", rec.Code, rec.Body.String())
	}
	if rec := a.do(http.MethodPost, expiring, "name=Late"); rec.Code != http.StatusGone {
		t.Fatalf("redeeming an expired link: %d", rec.Code)
	}
}

// A guest for document A cannot open room B, and a cross-origin upgrade is
// refused even with a valid guest cookie.
func TestGuestCollabIsScopedToItsDocument(t *testing.T) {
	a := newFullApp(t, Options{})
	a.asAdmin(http.MethodPut, "/api/settings/policy-studio", `{"guest_links":true}`)
	docA, docB := a.studioDocument(), a.studioDocument()
	guest, _, _ := a.invite(docA, `{"role":"editor"}`)
	dial := func(doc int64, origin string) int {
		header := http.Header{"Origin": {origin}, "Cookie": {guest.Name + "=" + guest.Value}}
		conn, resp, _ := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.server.URL, "http")+"/collab/policies/"+strconv.FormatInt(doc, 10), header)
		if conn != nil {
			_ = conn.Close()
		}
		if resp == nil {
			return 0
		}
		return resp.StatusCode
	}
	if got := dial(docA, a.server.URL); got != http.StatusSwitchingProtocols {
		t.Fatalf("its own room: %d", got)
	}
	if got := dial(docB, a.server.URL); got != http.StatusUnauthorized {
		t.Fatalf("another document's room: %d", got)
	}
	if got := dial(docA, "https://evil.example"); got != http.StatusUnauthorized {
		t.Fatalf("cross-origin: %d", got)
	}
}

// The guest pages enforce their CSP, and its style hashes are those of what
// the theme layer injects, for every theme.
func TestGuestPagesEnforceTheirCSP(t *testing.T) {
	a := newFullApp(t, Options{})
	a.asAdmin(http.MethodPut, "/api/settings/policy-studio", `{"guest_links":true}`)
	doc := a.studioDocument()
	guest, _, _ := a.invite(doc, `{"role":"viewer"}`)
	for _, theme := range []string{themeDark, themeMatrix, themeChaos, themeK40} {
		rec := a.do(http.MethodGet, "/shared/studio", "", guest, &http.Cookie{Name: themeCookieName, Value: theme})
		csp := rec.Header().Get("Content-Security-Policy")
		if rec.Code != http.StatusOK || csp == "" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s: %d csp=%q", theme, rec.Code, csp)
		}
		for _, want := range []string{"frame-ancestors 'none'", "base-uri 'none'", "object-src 'none'", "script-src 'self' 'nonce-"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: CSP lacks %q", theme, want)
			}
		}
		if strings.Contains(csp, "'unsafe-inline'") && !strings.Contains(csp, "style-src-attr 'unsafe-inline'") || strings.Contains(csp, "script-src 'self' 'unsafe") {
			t.Errorf("%s: unsafe-inline beyond style attributes: %s", theme, csp)
		}
		body := rec.Body.String()
		for _, m := range regexp.MustCompile(`(?s)<style[^>]*>(.*?)</style>`).FindAllStringSubmatch(body, -1) {
			sum := sha256.Sum256([]byte(m[1]))
			if !strings.Contains(csp, "sha256-"+base64.StdEncoding.EncodeToString(sum[:])) {
				t.Errorf("%s: a style block on the page is not in the CSP", theme)
			}
		}
		if strings.Contains(body, `id="global-ai-dock"`) || strings.Contains(body, `id="global-sidenav"`) || strings.Contains(body, `id="global-command-palette"`) {
			t.Errorf("%s: the guest page carries the application chrome", theme)
		}
		// Every inline script is one the CSP names by hash; nothing else runs.
		for rest := body; ; {
			open := strings.Index(rest, "<script")
			if open < 0 {
				break
			}
			tagEnd := open + strings.Index(rest[open:], ">")
			end := tagEnd + strings.Index(rest[tagEnd:], "</script>")
			tag, content := rest[open:tagEnd], rest[tagEnd+1:end]
			if !strings.Contains(tag, " src=") {
				sum := sha256.Sum256([]byte(content))
				if !strings.Contains(csp, "sha256-"+base64.StdEncoding.EncodeToString(sum[:])) {
					t.Errorf("%s: an inline script on the page is not in the CSP", theme)
				}
			} else if !strings.Contains(tag, "nonce=") {
				t.Errorf("%s: the bundle script carries no nonce", theme)
			}
			rest = rest[end:]
		}
	}
}

// Local mode has no accounts to tell a guest from anyone else, so it refuses
// links outright (Q9).
func TestLocalModeRefusesGuestLinks(t *testing.T) {
	a := newFullApp(t, Options{LocalMode: true})
	a.do(http.MethodPut, "/api/settings/policy-studio", `{"guest_links":true}`)
	doc := a.studioDocument()
	rec := a.do(http.MethodPost, "/policies/"+strconv.FormatInt(doc, 10)+"/share-links", `{"role":"viewer"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "local mode") {
		t.Fatalf("a link in local mode: %d %s", rec.Code, rec.Body.String())
	}
	if rec := a.do(http.MethodGet, "/shared/p/"+url.PathEscape(strings.Repeat("A", 43)), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("a join page in local mode: %d", rec.Code)
	}
}

// Guest AI: without allow_ai every AI route refuses; with it the request
// reaches the engine, which applies the document's rules as for anyone (here:
// no provider is configured), and a viewer may not place what it proposes.
func TestGuestAIFollowsTheLink(t *testing.T) {
	a := newFullApp(t, Options{})
	a.asAdmin(http.MethodPut, "/api/settings/policy-studio", `{"guest_links":true}`)
	doc := a.studioDocument()
	without, _, _ := a.invite(doc, `{"role":"editor"}`)
	if rec := a.do(http.MethodPost, "/shared/api/ai/proposals", `{"action":"tighten"}`, without); rec.Code != http.StatusForbidden {
		t.Fatalf("AI without allow_ai: %d %s", rec.Code, rec.Body.String())
	}
	with, _, _ := a.invite(doc, `{"role":"viewer","allow_ai":true}`)
	rec := a.do(http.MethodPost, "/shared/api/ai/proposals", `{"action":"tighten"}`, with)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "AI provider") {
		t.Fatalf("AI with allow_ai and no provider: %d %s", rec.Code, rec.Body.String())
	}
	rec = a.do(http.MethodPost, "/shared/api/ai/proposals/1/placements", `{"placements":[{"suid":"x","status":"placed"}]}`, with)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a viewer placing a proposal: %d %s", rec.Code, rec.Body.String())
	}
}

// Compare and the redline through the whole application: a document approved,
// then edited, compared with its approved version as JSON and as a PDF (or,
// without Typst on the machine, the source bundle the renderer falls back to).
func TestCompareAndRedline(t *testing.T) {
	templates, err := filepath.Abs("../../templates")
	if err != nil {
		t.Fatal(err)
	}
	a := newFullApp(t, Options{TemplatesDir: templates})
	t.Cleanup(func() { doctemplate.DirOverride = "" })
	rec := a.asAdmin(http.MethodPost, "/policies", `{"title":"Access guideline","doc_type":"guideline","owner_role":"CISO","effective_date":"2026-10-01"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	id := strconv.FormatInt(doc.ID, 10)
	var sec struct {
		ID int64 `json:"id"`
	}
	for i, body := range []string{`{"section_kind":"purpose","heading":"Purpose","body":"Staff will review access."}`, `{"section_kind":"scope","heading":"Scope","body":"- all staff"}`} {
		rec := a.asAdmin(http.MethodPost, "/policies/"+id+"/sections", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("section: %d %s", rec.Code, rec.Body.String())
		}
		if i == 0 {
			_ = json.Unmarshal(rec.Body.Bytes(), &sec)
		}
	}
	for _, step := range []string{"submit", "approve", "reopen"} {
		if rec := a.asAdmin(http.MethodPost, "/policies/"+id+"/"+step, `{}`); rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", step, rec.Code, rec.Body.String())
		}
	}
	if rec := a.asAdmin(http.MethodPut, "/policies/"+id+"/sections/"+strconv.FormatInt(sec.ID, 10), `{"section_kind":"purpose","heading":"Purpose","body":"Staff must review access."}`); rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	var versionID int64
	_ = a.conn.QueryRow(`SELECT id FROM policy_versions WHERE document_id = ?`, doc.ID).Scan(&versionID)
	from := strconv.FormatInt(versionID, 10)

	rec = a.asAdmin(http.MethodGet, "/policies/"+id+"/compare?from="+from, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changed":1`) || !strings.Contains(rec.Body.String(), `{"op":"added","text":"must"}`) {
		t.Fatalf("compare: %d %s", rec.Code, rec.Body.String())
	}
	rec = a.asAdmin(http.MethodGet, "/policies/"+id+"/compare.pdf?from="+from, "")
	switch {
	case rec.Code == http.StatusOK && rec.Header().Get("X-GRC-Bundled") == "1":
		t.Log("no Typst here: the redline came back as the source bundle")
	case rec.Code == http.StatusOK && strings.HasPrefix(rec.Body.String(), "%PDF"):
	default:
		t.Fatalf("redline: %d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()[:min(200, rec.Body.Len())])
	}
	if rec := a.asAdmin(http.MethodGet, "/policies/"+id+"/compare?from=999999", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown version: %d", rec.Code)
	}

	rec = a.asAdmin(http.MethodGet, "/templates/render?doc="+id+"&template=policy-docx&inline=1", "")
	switch {
	case rec.Code == http.StatusOK && rec.Header().Get("X-GRC-Bundled") == "1":
		t.Log("no Pandoc here: the Word document came back as the source bundle")
	case rec.Code == http.StatusOK && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/vnd.openxmlformats-officedocument.wordprocessingml.document"):
		if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
			t.Errorf("a Word document is served inline: %s", rec.Header().Get("Content-Disposition"))
		}
	default:
		t.Fatalf("word: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}
