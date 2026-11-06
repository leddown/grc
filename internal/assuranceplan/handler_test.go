package assuranceplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTestRouter(t *testing.T, actor string) (*gin.Engine, *Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := newTestService(t)
	h := NewHandler(svc, func(*gin.Context) string { return actor })
	r := gin.New()
	h.RegisterRoutes(r)
	h.RegisterAdminRoutes(r)
	return r, svc
}

func do(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateIgnoresAClientSuppliedStatus(t *testing.T) {
	r, _ := newTestRouter(t, "alice")
	w := do(t, r, http.MethodPost, "/assurance-plans", map[string]any{"type": "blank", "status": "Approved", "approved_by": "alice"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var p Plan
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Status != StatusDraft || p.ApprovedBy != "" || p.Author != "alice" {
		t.Errorf("status=%s approved_by=%q author=%q", p.Status, p.ApprovedBy, p.Author)
	}
}

func TestApproveAnswers422WithTheGateFindings(t *testing.T) {
	r, svc := newTestRouter(t, "bob")
	p, _ := svc.Create(CreateInput{Type: "blank"}, "alice")
	if w := do(t, r, http.MethodPost, "/assurance-plans/"+strconv.FormatInt(p.ID, 10)+"/approve", map[string]any{}); w.Code != http.StatusConflict {
		t.Fatalf("approving a draft = %d, want 409", w.Code)
	}
	if w := do(t, r, http.MethodPost, "/assurance-plans/"+strconv.FormatInt(p.ID, 10)+"/submit", nil); w.Code != http.StatusOK {
		t.Fatalf("submit = %d %s", w.Code, w.Body.String())
	}
	w := do(t, r, http.MethodPost, "/assurance-plans/"+strconv.FormatInt(p.ID, 10)+"/approve", map[string]any{})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("approve = %d %s, want 422", w.Code, w.Body.String())
	}
	var body struct {
		Error    string        `json:"error"`
		Findings []LintFinding `json:"findings"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Findings) == 0 || body.Findings[0].Severity != LintBlocking {
		t.Errorf("422 body = %s", w.Body.String())
	}
}

func TestApproverIsTheSignedInUserNotTheNameSent(t *testing.T) {
	r, svc := newTestRouter(t, "alice")
	svc.SetRequireSeparateApprover(true)
	p := createExample(t, svc, "alice")
	id := strconv.FormatInt(p.ID, 10)
	do(t, r, http.MethodPost, "/assurance-plans/"+id+"/submit", nil)
	w := do(t, r, http.MethodPost, "/assurance-plans/"+id+"/approve", map[string]any{"approved_by": "bob"})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "cannot also approve") {
		t.Fatalf("self-approval under another name = %d %s", w.Code, w.Body.String())
	}
}

func TestVersionServesTheStoredSnapshotBytes(t *testing.T) {
	r, svc := newTestRouter(t, "")
	p := createExample(t, svc, "alice")
	id := strconv.FormatInt(p.ID, 10)
	do(t, r, http.MethodPost, "/assurance-plans/"+id+"/submit", nil)
	if w := do(t, r, http.MethodPost, "/assurance-plans/"+id+"/approve", map[string]any{"approved_by": "bob"}); w.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", w.Code, w.Body.String())
	}
	w := do(t, r, http.MethodGet, "/assurance-plans/"+id+"/versions/1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("version = %d", w.Code)
	}
	sum := sha256.Sum256(w.Body.Bytes())
	if hex.EncodeToString(sum[:]) != w.Header().Get("X-Snapshot-SHA256") {
		t.Error("the served snapshot does not hash to its recorded SHA-256")
	}
	if w := do(t, r, http.MethodGet, "/assurance-plans/"+id+"/versions/9", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing version = %d, want 404", w.Code)
	}
}

func TestLockedDesignAnswers409(t *testing.T) {
	r, svc := newTestRouter(t, "")
	p := approveExample(t, svc)
	w := do(t, r, http.MethodPut, "/assurance-plans/"+strconv.FormatInt(p.ID, 10)+"/steps", p.Steps)
	if w.Code != http.StatusConflict {
		t.Fatalf("editing steps after approval = %d, want 409", w.Code)
	}
}

func TestPlanPageRendersEveryTabWithExplicitStatusColours(t *testing.T) {
	r, svc := newTestRouter(t, "")
	p := createExample(t, svc, "alice")
	w := do(t, r, http.MethodGet, "/assurance-plans/"+strconv.FormatInt(p.ID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`<span class="ap-st ap-st-draft">Draft</span>`,
		`<span class="ap-st ap-st-not-started">Not started</span>`,
		`.ap-st-draft, .ap-st-not-started`,
		`.ap-st-at-risk {`, `.ap-st-delayed, .ap-st-blocking {`, `.ap-st-approved, .ap-st-active, .ap-st-complete {`,
		`id="tab-overview"`, `id="tab-timeline"`, `id="tab-quarters"`, `id="tab-governance"`, `id="tab-history"`,
		`diamond: milestone`, `grey band: ongoing`, `solid bar: activity`,
		`class="ap-ms"`, `class="ap-band"`, `class="ap-bar"`,
		`Confirm mandate, scope and independence`,
		`ICT Risk Committee / management body`,
		`href="/assurance-plans"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plan page lacks %q", want)
		}
	}
	if strings.Contains(body, `class="pill`) {
		t.Error("the page uses the theme's .pill, whose colours the theme overrides")
	}
	rendered := body[:strings.Index(body, "<script")]
	if n := strings.Count(rendered, "<table"); n != strings.Count(rendered, "<thead>") {
		t.Errorf("%d server-rendered tables but %d explicit <thead>", n, strings.Count(rendered, "<thead>"))
	}
}

func TestStatusPillsAlwaysPrintTheirWord(t *testing.T) {
	for _, word := range append(append([]string{}, Statuses...), StepStatuses...) {
		pill := statusPill(word)
		if !strings.Contains(pill, ">"+word+"<") || !strings.Contains(pill, "ap-st-") {
			t.Errorf("pill for %q = %s", word, pill)
		}
		if !strings.Contains(pageStyles, "."+strings.TrimPrefix(strings.Fields(statusClass(word))[1], "")) {
			t.Errorf("no colour pair for %s", statusClass(word))
		}
	}
}

func TestListPageFiltersAndOffersTheExample(t *testing.T) {
	r, svc := newTestRouter(t, "")
	createExample(t, svc, "alice")
	if _, err := svc.Create(CreateInput{Type: "blank", PeriodStart: "2028-01-01"}, ""); err != nil {
		t.Fatal(err)
	}
	body := do(t, r, http.MethodGet, "/assurance-plans?year=2027", nil).Body.String()
	if !strings.Contains(body, "AP-2027-001") || strings.Contains(body, "AP-2028-001") {
		t.Error("the year filter did not apply")
	}
	if !strings.Contains(body, `value="dora-nfr-2027" data-kind="example"`) || !strings.Contains(body, `value="controls-testing" data-kind="type"`) {
		t.Error("the new-plan form does not offer the type and the example")
	}
}
