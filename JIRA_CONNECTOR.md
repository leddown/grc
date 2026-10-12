# Jira API Connector (Atlassian Cloud)

This repository now includes a Jira Cloud API connector at:

- `internal/jira/client.go`

It is designed for server-side integration use with Atlassian Cloud Jira REST API v3.

## What was added

- Configured Jira client with Basic Auth using email + API token.
- Core methods:
  - `GetIssue(issueIDOrKey, fields)`
  - `SearchIssues(jql, pagination, fields)`
  - `CreateIssue(fields)`
  - `GetProject(projectIDOrKey)`
- Retry behavior for `429` and `503`, including `Retry-After` support.
- Unit tests for auth header, request shape, and validation.

## Atlassian-recommended methodology

1. Authentication model
- For script/service integrations, use Atlassian account email + API token over HTTPS.
- Do not use account passwords.
- Prefer service accounts for automation.

2. Credential lifecycle
- Create scoped API tokens where possible.
- Set expiration and rotate tokens regularly.
- Store tokens in secret manager or environment variables, never in git.

3. Permission model
- The token identity must have Jira project permissions required by each endpoint.
- Validate both Jira project roles and issue-level security where used.

4. API consumption model
- Use REST API v3 endpoints under:
  - `https://<your-site>.atlassian.net/rest/api/3/...`
- Request only fields needed.
- Use pagination for search/list calls.
- Handle `429 Too Many Requests` and `503 Service Unavailable` with backoff.

5. Operational controls
- Add structured logs for request ID, endpoint, status, latency, retry count.
- Avoid logging tokens or full sensitive payloads.
- Monitor error rate and rate-limit responses.

## Configuration

Required:

- `JIRA_BASE_URL` (example: `https://your-domain.atlassian.net`)
- `JIRA_EMAIL`
- `JIRA_API_TOKEN`

Example wiring:

```go
jiraClient, err := jira.NewClient(jira.Config{
	BaseURL:  os.Getenv("JIRA_BASE_URL"),
	Email:    os.Getenv("JIRA_EMAIL"),
	APIToken: os.Getenv("JIRA_API_TOKEN"),
})
if err != nil {
	return err
}
```

## Examples

Search issues:

```go
resp, err := jiraClient.SearchIssues(ctx, jira.SearchRequest{
	JQL:        `project = SEC ORDER BY created DESC`,
	MaxResults: 25,
	Fields:     []string{"summary", "status", "assignee"},
})
```

Create issue:

```go
var req jira.CreateIssueRequest
req.Fields.Project.Key = "SEC"
req.Fields.IssueType.Name = "Task"
req.Fields.Summary = "Patch vulnerable dependency"

created, err := jiraClient.CreateIssue(ctx, req)
```

Get issue:

```go
issue, err := jiraClient.GetIssue(ctx, "SEC-123", []string{"summary", "status", "priority"})
```

## Official Atlassian docs reviewed

- Jira Cloud REST API v3 overview and endpoint groups:
  - https://developer.atlassian.com/cloud/jira/platform/rest/v3/
- Jira Cloud Issues API group (`Create issue`, `Get issue`, etc.):
  - https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/
- Jira Cloud Issue Search API group:
  - https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/
- Jira Cloud auth guidance (Basic Auth for scripts/bots):
  - https://developer.atlassian.com/cloud/jira/service-desk/basic-auth-for-rest-apis/
- Atlassian support API token management:
  - https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/
- Jira Cloud rate limiting guidance:
  - https://developer.atlassian.com/cloud/jira/platform/rate-limiting/

## Notes

- For distributed third-party apps, Atlassian generally recommends OAuth 2.0 (3LO) or Forge/Connect app auth instead of collecting user API tokens directly.
- This connector is intended for internal service integration where API-token auth is an accepted fit.
