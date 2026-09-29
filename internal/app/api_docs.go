package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func openAPIPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>API Docs · GRC</title>
  <style>
    body { margin: 0; padding: 24px; background: var(--bg); color: #1c2431; font-family: Georgia, "Times New Roman", serif; }
    main { max-width: 1100px; margin: 0 auto; background: var(--bg); border: 1px solid #d7cebf; border-radius: 24px; padding: 24px; }
    pre { overflow: auto; background: #1f1f1f; color: #f2eee6; padding: 16px; border-radius: 16px; }
    a { color: #8b3d2e; }
  </style>
</head>
<body>
  <main>
    <h1>API Docs</h1>
    <p>The OpenAPI document is available at <a href="/openapi.json">/openapi.json</a>.</p>
    <pre id="spec">Loading...</pre>
  </main>
  <script>
    fetch('/openapi.json').then((resp) => resp.json()).then((data) => {
      document.getElementById('spec').textContent = JSON.stringify(data, null, 2);
    }).catch((err) => {
      document.getElementById('spec').textContent = 'Failed to load spec: ' + err.message;
    });
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func openAPIJSON(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"openapi": "3.1.0",
		"info": gin.H{
			"title":   "grc API",
			"version": "0.1.0",
		},
		"components": gin.H{
			"securitySchemes": gin.H{
				"AdminBearer": gin.H{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "opaque",
				},
			},
		},
		"paths": gin.H{
			"/health": gin.H{
				"get": gin.H{"summary": "Health check"},
			},
			"/controls/data": gin.H{
				"get": gin.H{
					"summary":     "List controls",
					"description": "Supports optional pagination with page and per_page query parameters.",
				},
			},
			"/controls/{controlID}": gin.H{
				"put": gin.H{
					"summary":  "Update control",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
				"delete": gin.H{
					"summary":  "Delete control",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/security-nfrs/data": gin.H{
				"get": gin.H{
					"summary":     "List security NFRs",
					"description": "Supports optional pagination with page and per_page query parameters.",
				},
			},
			"/security-nfrs/{key}": gin.H{
				"put": gin.H{
					"summary":  "Update security NFR",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
				"delete": gin.H{
					"summary":  "Delete security NFR",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/security-nfrs/links/data": gin.H{
				"get": gin.H{
					"summary":     "List NFR-control links",
					"description": "Supports optional pagination with page and per_page query parameters.",
				},
			},
			"/security-nfrs/links/overrides": gin.H{
				"get": gin.H{
					"summary":  "List manual link overrides",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
				"put": gin.H{
					"summary":  "Set manual link override",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/security-nfrs/links/overrides/{nfrKey}/{mappingControlID}": gin.H{
				"delete": gin.H{
					"summary":  "Delete manual link override",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/security-nfrs/links/rebuild": gin.H{
				"post": gin.H{
					"summary":  "Rebuild derived link table",
					"security": []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/studio/state": gin.H{
				"get": gin.H{
					"summary":     "Policy Studio document state",
					"description": "Sections, control mappings, lint findings, projection version and whether the caller may edit. Answers 304 to a matching If-None-Match. The text itself travels over /collab/policies/{id}.",
				},
			},
			"/policies/{id}/studio/sections": gin.H{
				"post": gin.H{
					"summary":     "Add a section to a Studio document",
					"description": "Body: {after_uid, kind, heading}. Draft documents only. Removing (DELETE .../sections/{uid}), retyping (PATCH .../sections/{uid}), restoring (POST .../sections/{uid}/restore) and reordering (POST /policies/{id}/studio/reorder) follow the same rules.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/studio/migrate": gin.H{
				"post": gin.H{
					"summary":     "Move a section-editor document into the Policy Studio (one way)",
					"description": "Keeps the current sections as a snapshot first. Idempotent.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/studio/decisions": gin.H{
				"post": gin.H{
					"summary":     "Record who accepted or rejected suggestions",
					"description": "Body: {decision: accept|reject, suggestion_ids}. Draft or in-review documents. Every id must still be pending in the server's copy (409 otherwise); the actor is the session, and what is recorded about each suggestion is read from the server's copy. The editor applies the change to the text itself.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/studio/comments": gin.H{
				"get": gin.H{"summary": "List a Studio document's comment threads, internal and shared"},
				"post": gin.H{
					"summary":     "Start a comment thread on a span of text",
					"description": "Body: {section_uid, anchor_start, anchor_end, quote, visibility: internal|shared, body}. Anchors are base64 Yjs relative positions. Replies: POST .../comments/{threadID}/replies {body}; resolve or reopen: PATCH .../comments/{threadID} {status: resolved|open}.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/studio/provenance": gin.H{
				"get": gin.H{"summary": "Where each section came from, and who suggested, accepted or rejected each decided suggestion, and when"},
			},
			"/policies/templates": gin.H{
				"get": gin.H{"summary": "List the Policy Studio document templates, the default first, with the facts each declares"},
			},
			"/policies/from-template": gin.H{
				"post": gin.H{
					"summary":     "Create a Policy Studio document from a template",
					"description": "Body: {template_id, client_profile_id, title, reference, owner_role}. Known client facts render by value; missing ones stay unresolved and block approval. Proposed mappings the catalog lacks are skipped and listed in skipped_mappings.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/clients": gin.H{
				"get":  gin.H{"summary": "List client profiles"},
				"post": gin.H{"summary": "Create a client profile", "security": []gin.H{{"AdminBearer": []string{}}}},
			},
			"/policies/clients/{clientID}/facts/{key}": gin.H{
				"put": gin.H{
					"summary":     "Record a client fact",
					"description": "Body: {value, value_type}. Every Studio draft written for the client is re-projected, so its fact tokens resolve without editing the text. GET /policies/clients/{clientID}/facts lists them.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
				"delete": gin.H{"summary": "Delete a client fact", "security": []gin.H{{"AdminBearer": []string{}}}},
			},
			"/collab/policies/{id}": gin.H{
				"get": gin.H{
					"summary":     "Policy Studio collaboration socket (WebSocket, y-websocket protocol)",
					"description": "Authorized per upgrade: the session cookie, a /policies page grant and an Origin on the allowlist. Admins get read-write while the document is a draft or in review (in review the editor makes every edit a suggestion); everyone else, and every approved or retired document, is read-only.",
				},
			},
			"/reports/data/unlinked-controls": gin.H{
				"get": gin.H{
					"summary":     "List report rows",
					"description": "Supports optional pagination with page and per_page query parameters.",
				},
			},
			"/jira/json/data": gin.H{
				"post": gin.H{
					"summary": "Load Jira project/issue/search snapshot as JSON",
				},
			},
			"/jira/json/test-auth": gin.H{
				"post": gin.H{
					"summary": "Test Jira connection authentication",
				},
			},
			"/jira/json/export": gin.H{
				"post": gin.H{
					"summary": "Export Jira snapshot to static JSON file",
				},
			},
			"/jira/reports/presets": gin.H{
				"get": gin.H{
					"summary": "List Jira report/search presets",
				},
			},
			"/jira/reports/data": gin.H{
				"post": gin.H{
					"summary": "Run read-only Jira report/search preset",
				},
			},
		},
	})
}
