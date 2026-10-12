package reports

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizeType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "control", want: "Control"},
		{in: " CONTROL ENHANCEMENT ", want: "Control Enhancement"},
		{in: "all", want: "All"},
		{in: "unexpected", want: "All"},
	}

	for _, tt := range tests {
		got := normalizeType(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeType(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "linked", want: "Linked"},
		{in: " LINKED ", want: "Linked"},
		{in: "unlinked", want: "Unlinked"},
		{in: "all", want: "All"},
		{in: "anything", want: "All"},
	}

	for _, tt := range tests {
		got := normalizeMode(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeMode(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestUnlinkedControlsData_ReturnsLinkedModePayloadWithCIAAndThreats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite := openTestDB(t)
	seedReportFixture(t, sqlite)

	handler := NewHandler(NewService(sqlite))
	router := gin.New()
	router.GET("/reports/data/unlinked-controls", handler.UnlinkedControlsData)

	req := httptest.NewRequest(http.MethodGet, "/reports/data/unlinked-controls?type=control%20enhancement&mode=linked", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Count         int              `json:"count"`
		UnlinkedCount int              `json:"unlinked_count"`
		LinkedCount   int              `json:"linked_count"`
		TotalControls int              `json:"total_controls"`
		Type          string           `json:"type"`
		Mode          string           `json:"mode"`
		Items         []ControlSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.Count != 1 || payload.LinkedCount != 1 || payload.UnlinkedCount != 0 || payload.TotalControls != 1 {
		t.Fatalf("unexpected counts: %+v", payload)
	}
	if payload.Type != "Control Enhancement" || payload.Mode != "Linked" {
		t.Fatalf("type/mode=%q/%q want %q/%q", payload.Type, payload.Mode, "Control Enhancement", "Linked")
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items len=%d want=1", len(payload.Items))
	}

	item := payload.Items[0]
	if item.ControlID != "AU-3(1)" {
		t.Fatalf("control_id=%q want=%q", item.ControlID, "AU-3(1)")
	}
	if item.Confidentiality != "TRUE" || item.Integrity != "" || item.Availability != "TRUE" {
		t.Fatalf("CIA=%q/%q/%q want TRUE/\"\"/TRUE", item.Confidentiality, item.Integrity, item.Availability)
	}
	if len(item.Threats) != 3 || item.Threats[0] != "Spoofing" || item.Threats[1] != "Tampering" || item.Threats[2] != "Tampering" {
		t.Fatalf("threats=%v want=[Spoofing Tampering Tampering]", item.Threats)
	}
}

func TestUnlinkedControlsData_DefaultsToUnlinkedModeItems(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite := openTestDB(t)
	seedReportFixture(t, sqlite)

	handler := NewHandler(NewService(sqlite))
	router := gin.New()
	router.GET("/reports/data/unlinked-controls", handler.UnlinkedControlsData)

	req := httptest.NewRequest(http.MethodGet, "/reports/data/unlinked-controls?type=control", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Count         int              `json:"count"`
		UnlinkedCount int              `json:"unlinked_count"`
		LinkedCount   int              `json:"linked_count"`
		TotalControls int              `json:"total_controls"`
		Type          string           `json:"type"`
		Mode          string           `json:"mode"`
		Items         []ControlSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.Count != 2 || payload.UnlinkedCount != 2 || payload.LinkedCount != 1 || payload.TotalControls != 3 {
		t.Fatalf("unexpected counts: %+v", payload)
	}
	if payload.Type != "Control" || payload.Mode != "Unlinked" {
		t.Fatalf("type/mode=%q/%q want %q/%q", payload.Type, payload.Mode, "Control", "Unlinked")
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items len=%d want=2", len(payload.Items))
	}
	if payload.Items[0].ControlID != "AU-1" || payload.Items[1].ControlID != "AU-10" {
		t.Fatalf("unexpected item ids=%v want=[AU-1 AU-10]", []string{payload.Items[0].ControlID, payload.Items[1].ControlID})
	}
}

func TestUnlinkedControlsData_ReturnsAllModeItems(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlite := openTestDB(t)
	seedReportFixture(t, sqlite)

	handler := NewHandler(NewService(sqlite))
	router := gin.New()
	router.GET("/reports/data/unlinked-controls", handler.UnlinkedControlsData)

	req := httptest.NewRequest(http.MethodGet, "/reports/data/unlinked-controls?type=control&mode=all", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Count         int              `json:"count"`
		UnlinkedCount int              `json:"unlinked_count"`
		LinkedCount   int              `json:"linked_count"`
		TotalControls int              `json:"total_controls"`
		Type          string           `json:"type"`
		Mode          string           `json:"mode"`
		Items         []ControlSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.Count != 3 || payload.UnlinkedCount != 2 || payload.LinkedCount != 1 || payload.TotalControls != 3 {
		t.Fatalf("unexpected counts: %+v", payload)
	}
	if payload.Type != "Control" || payload.Mode != "All" {
		t.Fatalf("type/mode=%q/%q want %q/%q", payload.Type, payload.Mode, "Control", "All")
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items len=%d want=3", len(payload.Items))
	}
	if payload.Items[0].ControlID != "AU-1" || payload.Items[1].ControlID != "AU-2" || payload.Items[2].ControlID != "AU-10" {
		t.Fatalf("unexpected item ids=%v want=[AU-1 AU-2 AU-10]", []string{payload.Items[0].ControlID, payload.Items[1].ControlID, payload.Items[2].ControlID})
	}
}

func TestPage_UsesRadioFiltersForExclusiveReportOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil)
	router := gin.New()
	router.GET("/reports", handler.Page)

	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	required := []string{
		`type="radio" name="report-type" value="all" checked`,
		`type="radio" name="report-type" value="control"`,
		`type="radio" name="report-type" value="control enhancement"`,
		`type="radio" name="report-mode" value="unlinked" checked`,
		`type="radio" name="report-mode" value="linked"`,
		`type="radio" name="report-mode" value="all"`,
		`<legend>Control Type</legend>`,
		`<legend>Link Status</legend>`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("page missing fragment %q", fragment)
		}
	}
}

func TestPage_UsesSharedFilterPalette(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil)
	router := gin.New()
	router.GET("/reports", handler.Page)

	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	required := []string{
		`border: 1px solid #444;`,
		`color: #fff;`,
		`background: #000;`,
		`accent-color: #b89d78;`,
		`background: #151515;`,
		`background: #1f1f1f;`,
		`display: inline-block;`,
		`background: var(--panel);`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("page missing palette fragment %q", fragment)
		}
	}

	forbidden := []string{
		`accent-color: #1c2431;`,
		`background: white;`,
		`background: rgba(255,255,255,0.84);`,
		`background: rgba(255,255,255,0.72);`,
		`background: rgba(255,255,255,0.7);`,
	}
	for _, fragment := range forbidden {
		if strings.Contains(body, fragment) {
			t.Fatalf("page contains outdated palette fragment %q", fragment)
		}
	}
}

func TestPage_UsesReportsTablePalette(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(nil)
	router := gin.New()
	router.GET("/reports", handler.Page)

	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	required := []string{
		`border: 1px solid #444;`,
		`background: #242424;`,
		`background: #efe6d6;`,
		`color: #5a4630;`,
		`border-bottom: 1px solid rgba(215,206,191,0.7);`,
		`background: #2e2e2e;`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("page missing table palette fragment %q", fragment)
		}
	}
}
