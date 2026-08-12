package app

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"carelockconsulting/internal/jira"
)

type jiraRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f jiraRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestJiraJSONPage_IncludesLocalFileLoader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/jira/json", jiraJSONPage)

	req := httptest.NewRequest(http.MethodGet, "/jira/json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	required := []string{
		`id="testAuthBtn"`,
		`Test Authentication`,
		`/jira/json/test-auth`,
		`function testAuthentication()`,
		`id="includeChildren"`,
		`Child work items`,
		`id="includeLinked"`,
		`Linked work items`,
		`id="selectAllFieldsBtn"`,
		`Select All Fields`,
		`function selectAllFields()`,
		`id="clearAllFieldsBtn"`,
		`Clear All Fields`,
		`function clearAllFields()`,
		`name="jiraField"`,
		`carelockconsulting_jira_connection`,
		`localStorage.setItem(jiraConnectionStorageKey`,
		`background: #000;`,
		`background: #111827;`,
		`value="summary" checked`,
		`value="fixVersions"`,
		`/stored-json`,
		`function saveStoredJSON(name, source, content)`,
		`id="localLoadBtn"`,
		`Load Local JSON`,
		`id="localFile"`,
		`type="file"`,
		`accept=".json,application/json"`,
		`function loadLocalJSON(file)`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("jira json page missing fragment %q", fragment)
		}
	}
}

func TestJiraReportsPage_PersistsConnectionAndDisplaysStoredJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/jira/reports", jiraReportsPage)

	req := httptest.NewRequest(http.MethodGet, "/jira/reports", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	required := []string{
		`carelockconsulting_jira_connection`,
		`function loadSavedJiraConnection()`,
		`function persistJiraConnection()`,
		`id="storedJSONSelect"`,
		`Stored SQLite JSON`,
		`id="loadStoredJSONBtn"`,
		`Load Stored JSON`,
		`/stored-json`,
		`function loadStoredJSONDocument()`,
		`function saveStoredJSON(name, source, content)`,
	}
	for _, fragment := range required {
		if !strings.Contains(body, fragment) {
			t.Fatalf("jira reports page missing fragment %q", fragment)
		}
	}
}

func TestJiraJSONTestAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotAuth string
	var gotPath string
	prevFactory := jiraHTTPClientFactory
	jiraHTTPClientFactory = func() *http.Client {
		return &http.Client{
			Transport: jiraRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotAuth = r.Header.Get("Authorization")
				gotPath = r.URL.Path
				if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/myself" {
					return jsonHTTPResponse(r, http.StatusNotFound, `{"error":"not found"}`), nil
				}
				return jsonHTTPResponse(r, http.StatusOK, `{
					"accountId":"abc123",
					"accountType":"atlassian",
					"emailAddress":"user@example.com",
					"displayName":"Alice Example",
					"active":true
				}`), nil
			}),
		}
	}
	t.Cleanup(func() { jiraHTTPClientFactory = prevFactory })

	router := gin.New()
	router.POST("/jira/json/test-auth", jiraJSONTestAuth)

	authBody := map[string]any{
		"base_url":  "https://jira.example.com",
		"email":     "user@example.com",
		"api_token": "token123",
	}
	rec := performJSONRequest(t, router, http.MethodPost, "/jira/json/test-auth", authBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("test auth status=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK   bool      `json:"ok"`
		User jira.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal auth payload: %v", err)
	}
	if !payload.OK || payload.User.AccountID != "abc123" || payload.User.DisplayName != "Alice Example" {
		t.Fatalf("unexpected auth payload: %+v", payload)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:token123"))
	if gotAuth != wantAuth {
		t.Fatalf("authorization mismatch: got=%q want=%q", gotAuth, wantAuth)
	}
	if gotPath != "/rest/api/3/myself" {
		t.Fatalf("path mismatch: got=%q", gotPath)
	}
}

func TestJiraJSONDataAndExport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotAuth string
	var gotSearchJQL string
	var gotSearchFields []string
	prevFactory := jiraHTTPClientFactory
	jiraHTTPClientFactory = func() *http.Client {
		return &http.Client{
			Transport: jiraRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotAuth = r.Header.Get("Authorization")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/project/SEC":
					return jsonHTTPResponse(r, http.StatusOK, `{"id":"10000","key":"SEC","name":"Security"}`), nil
				case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/SEC-1":
					return jsonHTTPResponse(r, http.StatusOK, `{
						"id":"10001",
						"key":"SEC-1",
						"fields":{
							"summary":"Issue summary",
							"status":{"name":"In Progress","statusCategory":{"name":"In Progress"}},
							"assignee":{"displayName":"Alice Example"},
							"priority":{"name":"High"},
							"issuetype":{"name":"Task"},
							"created":"2026-05-01T12:00:00.000+0000",
							"updated":"2026-05-31T12:00:00.000+0000"
						}
					}`), nil
				case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql":
					var req jira.SearchRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatalf("decode search request: %v", err)
					}
					gotSearchJQL = req.JQL
					gotSearchFields = append([]string(nil), req.Fields...)
					return jsonHTTPResponse(r, http.StatusOK, `{
						"isLast":true,
						"issues":[{"id":"10001","key":"SEC-1","fields":{"summary":"Issue summary"}}]
					}`), nil
				default:
					return jsonHTTPResponse(r, http.StatusNotFound, `{"error":"not found"}`), nil
				}
			}),
		}
	}
	t.Cleanup(func() { jiraHTTPClientFactory = prevFactory })

	router := gin.New()
	router.POST("/jira/json/data", jiraJSONData)
	router.POST("/jira/json/export", jiraJSONExport)

	body := map[string]any{
		"base_url":    "https://jira.example.com",
		"email":       "user@example.com",
		"api_token":   "token123",
		"project_key": "SEC",
		"issue_key":   "SEC-1",
		"max_results": 50,
		"fields":      []string{"summary", "status", "assignee"},
	}
	dataRec := performJSONRequest(t, router, http.MethodPost, "/jira/json/data", body)
	if dataRec.Code != http.StatusOK {
		t.Fatalf("json data status=%d body=%s", dataRec.Code, dataRec.Body.String())
	}

	var dataPayload struct {
		ProjectKey string `json:"project_key"`
		IssueKey   string `json:"issue_key"`
		JQL        string `json:"jql"`
		Search     struct {
			IsLast bool         `json:"isLast"`
			Issues []jira.Issue `json:"issues"`
		} `json:"search"`
	}
	if err := json.Unmarshal(dataRec.Body.Bytes(), &dataPayload); err != nil {
		t.Fatalf("unmarshal data payload: %v", err)
	}
	if dataPayload.ProjectKey != "SEC" || dataPayload.IssueKey != "SEC-1" {
		t.Fatalf("unexpected keys: %+v", dataPayload)
	}
	if !dataPayload.Search.IsLast || len(dataPayload.Search.Issues) != 1 {
		t.Fatalf("unexpected search payload: %+v", dataPayload.Search)
	}
	if !strings.Contains(dataPayload.JQL, `project = "SEC"`) {
		t.Fatalf("auto JQL not generated from project key: %q", dataPayload.JQL)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:token123"))
	if gotAuth != wantAuth {
		t.Fatalf("authorization mismatch: got=%q want=%q", gotAuth, wantAuth)
	}
	if gotSearchJQL == "" {
		t.Fatalf("expected search JQL to be sent")
	}
	if len(gotSearchFields) == 0 {
		t.Fatalf("expected fields to be sent")
	}

	exportBody := map[string]any{
		"base_url":    "https://jira.example.com",
		"email":       "user@example.com",
		"api_token":   "token123",
		"project_key": "SEC",
		"issue_key":   "SEC-1",
		"file_name":   "jira-export-test.json",
	}
	exportRec := performJSONRequest(t, router, http.MethodPost, "/jira/json/export", exportBody)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("json export status=%d body=%s", exportRec.Code, exportRec.Body.String())
	}

	var exportPayload struct {
		Path  string `json:"path"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exportPayload); err != nil {
		t.Fatalf("unmarshal export payload: %v", err)
	}
	if exportPayload.Path == "" || exportPayload.Bytes <= 0 {
		t.Fatalf("unexpected export payload: %+v", exportPayload)
	}
	t.Cleanup(func() { _ = os.Remove(exportPayload.Path) })

	raw, err := os.ReadFile(exportPayload.Path)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	if !strings.Contains(string(raw), `"project_key": "SEC"`) {
		t.Fatalf("export file missing project key: %s", string(raw))
	}
}

func TestJiraJSONDataIncludesRelatedWorkItems(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotRootFields string
	var gotChildJQL string
	var gotChildFields []string
	var gotLinkedFields string
	prevFactory := jiraHTTPClientFactory
	jiraHTTPClientFactory = func() *http.Client {
		return &http.Client{
			Transport: jiraRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/SEC-1":
					gotRootFields = r.URL.Query().Get("fields")
					return jsonHTTPResponse(r, http.StatusOK, `{
						"id":"10001",
						"key":"SEC-1",
						"fields":{
							"summary":"Root issue",
							"issuelinks":[
								{
									"id":"90001",
									"type":{"name":"Blocks","outward":"blocks","inward":"is blocked by"},
									"outwardIssue":{"id":"10002","key":"SEC-2"}
								}
							]
						}
					}`), nil
				case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/SEC-2":
					gotLinkedFields = r.URL.Query().Get("fields")
					return jsonHTTPResponse(r, http.StatusOK, `{
						"id":"10002",
						"key":"SEC-2",
						"fields":{"summary":"Linked issue","status":{"name":"Done"}}
					}`), nil
				case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql":
					var req jira.SearchRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatalf("decode child search request: %v", err)
					}
					gotChildJQL = req.JQL
					gotChildFields = append([]string(nil), req.Fields...)
					return jsonHTTPResponse(r, http.StatusOK, `{
						"isLast":true,
						"issues":[{"id":"10003","key":"SEC-3","fields":{"summary":"Child issue"}}]
					}`), nil
				default:
					return jsonHTTPResponse(r, http.StatusNotFound, `{"error":"not found"}`), nil
				}
			}),
		}
	}
	t.Cleanup(func() { jiraHTTPClientFactory = prevFactory })

	router := gin.New()
	router.POST("/jira/json/data", jiraJSONData)

	body := map[string]any{
		"base_url":         "https://jira.example.com",
		"email":            "user@example.com",
		"api_token":        "token123",
		"issue_key":        "SEC-1",
		"fields":           []string{"summary", "status"},
		"max_results":      25,
		"include_children": true,
		"include_linked":   true,
	}
	rec := performJSONRequest(t, router, http.MethodPost, "/jira/json/data", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("json data status=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Fields        []string `json:"fields"`
		RequestFields []string `json:"request_fields"`
		Related       struct {
			RootIssueKeys    []string                        `json:"root_issue_keys"`
			ChildrenByParent map[string]jira.SearchResponse  `json:"children_by_parent"`
			LinkedByIssue    map[string][]jiraLinkedWorkItem `json:"linked_by_issue"`
		} `json:"related_work_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal related payload: %v", err)
	}

	if !containsAll(payload.Fields, "summary", "status") {
		t.Fatalf("selected fields changed: %+v", payload.Fields)
	}
	if !containsAll(payload.RequestFields, "summary", "status", "subtasks", "parent", "issuelinks") {
		t.Fatalf("request fields missing relationship fields: %+v", payload.RequestFields)
	}
	if !strings.Contains(gotRootFields, "issuelinks") || !strings.Contains(gotRootFields, "subtasks") {
		t.Fatalf("root issue fields missing relationship metadata: %q", gotRootFields)
	}
	if gotChildJQL != `parent = "SEC-1" ORDER BY updated DESC` {
		t.Fatalf("child JQL mismatch: %q", gotChildJQL)
	}
	if !containsAll(gotChildFields, "summary", "status") {
		t.Fatalf("child fields mismatch: %+v", gotChildFields)
	}
	if gotLinkedFields != "summary,status" {
		t.Fatalf("linked issue fields mismatch: %q", gotLinkedFields)
	}
	if len(payload.Related.RootIssueKeys) != 1 || payload.Related.RootIssueKeys[0] != "SEC-1" {
		t.Fatalf("unexpected root keys: %+v", payload.Related.RootIssueKeys)
	}
	if got := payload.Related.ChildrenByParent["SEC-1"].Issues; len(got) != 1 || got[0].Key != "SEC-3" {
		t.Fatalf("unexpected child issues: %+v", got)
	}
	if got := payload.Related.LinkedByIssue["SEC-1"]; len(got) != 1 || got[0].Issue.Key != "SEC-2" {
		t.Fatalf("unexpected linked issues: %+v", got)
	}
}

func TestJiraReportPresetsAndReportData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotSearchJQL string
	prevFactory := jiraHTTPClientFactory
	jiraHTTPClientFactory = func() *http.Client {
		return &http.Client{
			Transport: jiraRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql" {
					var req jira.SearchRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatalf("decode search request: %v", err)
					}
					gotSearchJQL = req.JQL
					return jsonHTTPResponse(r, http.StatusOK, `{
						"isLast":true,
						"issues":[
							{
								"id":"20001",
								"key":"SEC-10",
								"fields":{
									"summary":"First",
									"issuetype":{"name":"Story"},
									"status":{"name":"In Progress","statusCategory":{"name":"In Progress"}},
									"assignee":{"displayName":"Alice"},
									"priority":{"name":"High"},
									"updated":"2026-05-31T10:00:00.000+0000"
								}
							},
							{
								"id":"20002",
								"key":"SEC-11",
								"fields":{
									"summary":"Second",
									"issuetype":{"name":"Task"},
									"status":{"name":"Done","statusCategory":{"name":"Done"}},
									"priority":{"name":"Medium"},
									"updated":"2026-05-30T10:00:00.000+0000"
								}
							}
						]
					}`), nil
				}
				return jsonHTTPResponse(r, http.StatusNotFound, `{"error":"not found"}`), nil
			}),
		}
	}
	t.Cleanup(func() { jiraHTTPClientFactory = prevFactory })

	router := gin.New()
	router.GET("/jira/reports/presets", jiraReportPresetsData)
	router.POST("/jira/reports/data", jiraReportsData)

	presetsRec := httptest.NewRecorder()
	presetsReq := httptest.NewRequest(http.MethodGet, "/jira/reports/presets", nil)
	router.ServeHTTP(presetsRec, presetsReq)
	if presetsRec.Code != http.StatusOK {
		t.Fatalf("presets status=%d body=%s", presetsRec.Code, presetsRec.Body.String())
	}
	var presetsPayload struct {
		Items []jiraReportPreset `json:"items"`
	}
	if err := json.Unmarshal(presetsRec.Body.Bytes(), &presetsPayload); err != nil {
		t.Fatalf("unmarshal presets: %v", err)
	}
	if len(presetsPayload.Items) == 0 {
		t.Fatalf("expected at least one preset")
	}

	reportBody := map[string]any{
		"base_url":    "https://jira.example.com",
		"email":       "user@example.com",
		"api_token":   "token123",
		"project_key": "SEC",
		"preset_id":   "high_priority_open",
		"max_results": 100,
	}
	reportRec := performJSONRequest(t, router, http.MethodPost, "/jira/reports/data", reportBody)
	if reportRec.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", reportRec.Code, reportRec.Body.String())
	}

	var reportPayload struct {
		JQL      string           `json:"jql"`
		Returned int              `json:"returned"`
		Items    []jiraReportItem `json:"items"`
		Summary  struct {
			Total      int `json:"total"`
			ToDo       int `json:"todo"`
			InProgress int `json:"in_progress"`
			Done       int `json:"done"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(reportRec.Body.Bytes(), &reportPayload); err != nil {
		t.Fatalf("unmarshal report payload: %v", err)
	}
	if reportPayload.Returned != 2 || len(reportPayload.Items) != 2 {
		t.Fatalf("unexpected report items: returned=%d len=%d", reportPayload.Returned, len(reportPayload.Items))
	}
	if reportPayload.Summary.Total != 2 || reportPayload.Summary.InProgress != 1 || reportPayload.Summary.Done != 1 {
		t.Fatalf("unexpected summary: %+v", reportPayload.Summary)
	}
	if !strings.Contains(reportPayload.JQL, `project = "SEC"`) {
		t.Fatalf("report JQL missing project substitution: %q", reportPayload.JQL)
	}
	if gotSearchJQL != reportPayload.JQL {
		t.Fatalf("server JQL mismatch: got=%q payload=%q", gotSearchJQL, reportPayload.JQL)
	}
}

func TestJiraReportsData_UsesJQLOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotSearchJQL string
	prevFactory := jiraHTTPClientFactory
	jiraHTTPClientFactory = func() *http.Client {
		return &http.Client{
			Transport: jiraRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql" {
					var req jira.SearchRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatalf("decode search request: %v", err)
					}
					gotSearchJQL = req.JQL
					return jsonHTTPResponse(r, http.StatusOK, `{"isLast":true,"issues":[]}`), nil
				}
				return jsonHTTPResponse(r, http.StatusNotFound, `{"error":"not found"}`), nil
			}),
		}
	}
	t.Cleanup(func() { jiraHTTPClientFactory = prevFactory })

	router := gin.New()
	router.POST("/jira/reports/data", jiraReportsData)

	override := `project = "SEC" AND statusCategory != Done ORDER BY updated DESC`
	reportBody := map[string]any{
		"base_url":    "https://jira.example.com",
		"email":       "user@example.com",
		"api_token":   "token123",
		"project_key": "SEC",
		"preset_id":   "high_priority_open",
		"jql":         override,
	}
	reportRec := performJSONRequest(t, router, http.MethodPost, "/jira/reports/data", reportBody)
	if reportRec.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", reportRec.Code, reportRec.Body.String())
	}
	if gotSearchJQL != override {
		t.Fatalf("override JQL was not used: got=%q want=%q", gotSearchJQL, override)
	}
}

func performJSONRequest(t *testing.T, router *gin.Engine, method string, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestJiraClientFromInputRejectsNonHTTPSBaseURL(t *testing.T) {
	if _, err := jiraClientFromInput("http://jira.internal.example.com", "user@example.com", "token"); err == nil {
		t.Fatal("expected error for non-https base_url")
	}
}

func TestJiraClientFromInputRejectsMalformedBaseURL(t *testing.T) {
	if _, err := jiraClientFromInput("not-a-url", "user@example.com", "token"); err == nil {
		t.Fatal("expected error for malformed base_url")
	}
}

func TestJiraClientFromInputAcceptsValidHTTPSBaseURL(t *testing.T) {
	if _, err := jiraClientFromInput("https://jira.example.com", "user@example.com", "token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsDisallowedJiraTargetIP(t *testing.T) {
	disallowed := []string{
		"127.0.0.1",       // loopback
		"10.0.0.5",        // RFC1918 private
		"169.254.169.254", // link-local / cloud metadata
		"::1",             // IPv6 loopback
		"fd00::1",         // IPv6 ULA (private)
		"0.0.0.0",         // unspecified
	}
	for _, raw := range disallowed {
		if !isDisallowedJiraTargetIP(net.ParseIP(raw)) {
			t.Errorf("expected %s to be disallowed", raw)
		}
	}

	allowed := []string{
		"93.184.216.34", // public IPv4
		"8.8.8.8",       // public IPv4
	}
	for _, raw := range allowed {
		if isDisallowedJiraTargetIP(net.ParseIP(raw)) {
			t.Errorf("expected %s to be allowed", raw)
		}
	}
}

func containsAll(values []string, expected ...string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range expected {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}
