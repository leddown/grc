package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetIssueSendsBasicAuthAndFieldsQuery(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotQuery string

	c, err := NewClient(Config{
		BaseURL:  "https://jira.example.com",
		Email:    "user@example.com",
		APIToken: "token123",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotAuth = r.Header.Get("Authorization")
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"10000","key":"SEC-1"}`)),
				Request:    r,
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	issue, err := c.GetIssue(context.Background(), "SEC-1", []string{"summary", "status"})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Key != "SEC-1" {
		t.Fatalf("unexpected issue key: %s", issue.Key)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:token123"))
	if gotAuth != wantAuth {
		t.Fatalf("authorization mismatch: got=%q want=%q", gotAuth, wantAuth)
	}
	if gotPath != "/rest/api/3/issue/SEC-1" {
		t.Fatalf("path mismatch: got=%q", gotPath)
	}
	if !strings.Contains(gotQuery, "fields=summary%2Cstatus") {
		t.Fatalf("query missing fields: %q", gotQuery)
	}
}

func TestGetMyselfUsesAuthenticatedUserEndpoint(t *testing.T) {
	var gotAuth string
	var gotPath string

	c, err := NewClient(Config{
		BaseURL:  "https://jira.example.com",
		Email:    "user@example.com",
		APIToken: "token123",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotAuth = r.Header.Get("Authorization")
			gotPath = r.URL.Path
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"accountId":"abc123","displayName":"Alice Example","active":true}`)),
				Request:    r,
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	user, err := c.GetMyself(context.Background())
	if err != nil {
		t.Fatalf("GetMyself: %v", err)
	}
	if user.AccountID != "abc123" || user.DisplayName != "Alice Example" || !user.Active {
		t.Fatalf("unexpected user: %+v", user)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:token123"))
	if gotAuth != wantAuth {
		t.Fatalf("authorization mismatch: got=%q want=%q", gotAuth, wantAuth)
	}
	if gotPath != "/rest/api/3/myself" {
		t.Fatalf("path mismatch: got=%q", gotPath)
	}
}

func TestCreateIssueValidatesRequiredFields(t *testing.T) {
	c, err := NewClient(Config{
		BaseURL:  "https://example.atlassian.net",
		Email:    "user@example.com",
		APIToken: "token123",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.CreateIssue(context.Background(), CreateIssueRequest{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestSearchIssuesPostsJQL(t *testing.T) {
	var gotMethod string
	var gotPath string
	var body SearchRequest

	c, err := NewClient(Config{
		BaseURL:  "https://jira.example.com",
		Email:    "user@example.com",
		APIToken: "token123",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"isLast":true,"issues":[{"id":"10000","key":"SEC-1"}]}`,
				)),
				Request: r,
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := c.SearchIssues(context.Background(), SearchRequest{
		JQL:        "project = SEC ORDER BY created DESC",
		MaxResults: 1,
		Fields:     []string{"summary"},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if !resp.IsLast || len(resp.Issues) != 1 {
		t.Fatalf("unexpected search response: %+v", resp)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method mismatch: %s", gotMethod)
	}
	if gotPath != "/rest/api/3/search/jql" {
		t.Fatalf("path mismatch: %s", gotPath)
	}
	if body.JQL == "" {
		t.Fatalf("expected JQL in request body")
	}
}
