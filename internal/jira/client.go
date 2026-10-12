package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout    = 20 * time.Second
	defaultMaxRetries = 3
	// maxJiraResponseBytes bounds a single Jira response. A full issue search
	// with expanded fields runs to a few megabytes at most, so 32 MiB is far
	// past any legitimate reply while still capping what a caller-supplied host
	// can make this process allocate.
	maxJiraResponseBytes = 32 << 20
)

type Client struct {
	baseURL    string
	email      string
	apiToken   string
	httpClient *http.Client
	maxRetries int
}

type Config struct {
	BaseURL    string
	Email      string
	APIToken   string
	HTTPClient *http.Client
	MaxRetries int
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jira api error: status=%d body=%s", e.StatusCode, e.Body)
}

func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	email := strings.TrimSpace(cfg.Email)
	apiToken := strings.TrimSpace(cfg.APIToken)
	if baseURL == "" || email == "" || apiToken == "" {
		return nil, fmt.Errorf("jira config requires base url, email, and api token")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid jira base url: %w", err)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		email:      email,
		apiToken:   apiToken,
		httpClient: httpClient,
		maxRetries: maxRetries,
	}, nil
}

type IssueFields struct {
	Summary     string `json:"summary,omitempty"`
	Description any    `json:"description,omitempty"`
	Status      struct {
		Name           string `json:"name,omitempty"`
		StatusCategory struct {
			Key  string `json:"key,omitempty"`
			Name string `json:"name,omitempty"`
		} `json:"statusCategory,omitempty"`
	} `json:"status,omitempty"`
	Assignee struct {
		AccountID   string `json:"accountId,omitempty"`
		DisplayName string `json:"displayName,omitempty"`
	} `json:"assignee,omitempty"`
	Priority struct {
		Name string `json:"name,omitempty"`
	} `json:"priority,omitempty"`
	Reporter       UserRef       `json:"reporter,omitempty"`
	Creator        UserRef       `json:"creator,omitempty"`
	Created        string        `json:"created,omitempty"`
	Updated        string        `json:"updated,omitempty"`
	ResolutionDate string        `json:"resolutiondate,omitempty"`
	DueDate        string        `json:"duedate,omitempty"`
	Labels         []string      `json:"labels,omitempty"`
	Components     []NamedEntity `json:"components,omitempty"`
	FixVersions    []NamedEntity `json:"fixVersions,omitempty"`
	Versions       []NamedEntity `json:"versions,omitempty"`
	Resolution     *NamedEntity  `json:"resolution,omitempty"`
	IssueType      struct {
		Name string `json:"name,omitempty"`
		ID   string `json:"id,omitempty"`
	} `json:"issuetype,omitempty"`
	Project struct {
		Key string `json:"key,omitempty"`
		ID  string `json:"id,omitempty"`
	} `json:"project,omitempty"`
	Parent      *IssueRef   `json:"parent,omitempty"`
	Subtasks    []IssueRef  `json:"subtasks,omitempty"`
	IssueLinks  []IssueLink `json:"issuelinks,omitempty"`
	Attachment  any         `json:"attachment,omitempty"`
	Comment     any         `json:"comment,omitempty"`
	Environment any         `json:"environment,omitempty"`
}

type UserRef struct {
	AccountID    string `json:"accountId,omitempty"`
	AccountType  string `json:"accountType,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       bool   `json:"active,omitempty"`
	Self         string `json:"self,omitempty"`
}

type NamedEntity struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Self        string `json:"self,omitempty"`
}

type IssueRef struct {
	ID     string      `json:"id,omitempty"`
	Key    string      `json:"key,omitempty"`
	Self   string      `json:"self,omitempty"`
	Fields IssueFields `json:"fields,omitempty"`
}

type IssueLinkType struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Inward  string `json:"inward,omitempty"`
	Outward string `json:"outward,omitempty"`
	Self    string `json:"self,omitempty"`
}

type IssueLink struct {
	ID           string        `json:"id,omitempty"`
	Self         string        `json:"self,omitempty"`
	Type         IssueLinkType `json:"type,omitempty"`
	InwardIssue  *IssueRef     `json:"inwardIssue,omitempty"`
	OutwardIssue *IssueRef     `json:"outwardIssue,omitempty"`
}

type Issue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Self   string      `json:"self"`
	Fields IssueFields `json:"fields"`
}

type CreateIssueRequest struct {
	Fields IssueFields `json:"fields"`
}

type CreateIssueResponse struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

type SearchRequest struct {
	JQL           string   `json:"jql"`
	MaxResults    int      `json:"maxResults,omitempty"`
	Fields        []string `json:"fields,omitempty"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type SearchResponse struct {
	StartAt       int     `json:"startAt,omitempty"`
	MaxResults    int     `json:"maxResults,omitempty"`
	Total         int     `json:"total,omitempty"`
	IsLast        bool    `json:"isLast,omitempty"`
	NextPageToken string  `json:"nextPageToken,omitempty"`
	Issues        []Issue `json:"issues"`
}

type Project struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Self string `json:"self"`
}

type User struct {
	AccountID    string `json:"accountId"`
	AccountType  string `json:"accountType,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	Active       bool   `json:"active"`
	Self         string `json:"self,omitempty"`
}

func (c *Client) GetMyself(ctx context.Context) (*User, error) {
	var out User
	if err := c.doJSON(ctx, http.MethodGet, "/rest/api/3/myself", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetIssue(ctx context.Context, issueIDOrKey string, fields []string) (*Issue, error) {
	if strings.TrimSpace(issueIDOrKey) == "" {
		return nil, fmt.Errorf("issue id or key is required")
	}
	escaped := url.PathEscape(issueIDOrKey)
	path := "/rest/api/3/issue/" + escaped
	if len(fields) > 0 {
		q := url.Values{}
		q.Set("fields", strings.Join(fields, ","))
		path = path + "?" + q.Encode()
	}
	var out Issue
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SearchIssues(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if strings.TrimSpace(req.JQL) == "" {
		return nil, fmt.Errorf("jql is required")
	}
	var out SearchResponse
	if err := c.doJSON(ctx, http.MethodPost, "/rest/api/3/search/jql", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateIssue(ctx context.Context, req CreateIssueRequest) (*CreateIssueResponse, error) {
	if strings.TrimSpace(req.Fields.Project.Key) == "" && strings.TrimSpace(req.Fields.Project.ID) == "" {
		return nil, fmt.Errorf("project key or id is required")
	}
	if strings.TrimSpace(req.Fields.IssueType.Name) == "" && strings.TrimSpace(req.Fields.IssueType.ID) == "" {
		return nil, fmt.Errorf("issue type name or id is required")
	}
	if strings.TrimSpace(req.Fields.Summary) == "" {
		return nil, fmt.Errorf("summary is required")
	}

	var out CreateIssueResponse
	if err := c.doJSON(ctx, http.MethodPost, "/rest/api/3/issue", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetProject(ctx context.Context, projectIDOrKey string) (*Project, error) {
	if strings.TrimSpace(projectIDOrKey) == "" {
		return nil, fmt.Errorf("project id or key is required")
	}
	escaped := url.PathEscape(projectIDOrKey)
	var out Project
	if err := c.doJSON(ctx, http.MethodGet, "/rest/api/3/project/"+escaped, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) doJSON(ctx context.Context, method string, path string, requestBody any, out any) error {
	var payload []byte
	var err error
	if requestBody != nil {
		payload, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
	}

	attempt := 0
	for {
		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.SetBasicAuth(c.email, c.apiToken)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("jira request failed: %w", err)
		}

		// Bounded read. The base URL is caller-supplied, and while
		// safeJiraDialContext constrains *where* a connection may go, it says
		// nothing about how much comes back — a large JQL result or a hostile
		// endpoint would otherwise be read into memory without limit.
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxJiraResponseBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response body: %w", readErr)
		}
		if int64(len(bodyBytes)) >= maxJiraResponseBytes {
			return fmt.Errorf("jira response exceeded %d bytes; narrow the query or lower max_results", maxJiraResponseBytes)
		}

		if shouldRetry(resp.StatusCode) && attempt < c.maxRetries {
			delay := retryDelay(resp.Header.Get("Retry-After"), attempt)
			attempt++
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &APIError{
				StatusCode: resp.StatusCode,
				Body:       string(bodyBytes),
			}
		}

		if out == nil || len(bodyBytes) == 0 {
			return nil
		}
		if err := json.Unmarshal(bodyBytes, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}
}

func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	base := 500 * time.Millisecond
	multiplier := math.Pow(2, float64(attempt))
	delay := time.Duration(float64(base) * multiplier)
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}
