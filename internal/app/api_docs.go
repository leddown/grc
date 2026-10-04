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
			"/policies/{id}/studio/ai/proposals": gin.H{
				"post": gin.H{
					"summary":     "Ask the AI for a proposal on a Studio document",
					"description": "Body: {action: testable|tighten|rewrite|fix_lint|draft_section|expand|explain_for_customer|map_controls|review|from_library, scope: selection|section|document, selection {section_uid, block_ids, quote}, instruction, library_document_id (from_library)}. The document's ai_policy decides where it may go (local_only refuses a Wintermute server that could answer on a cloud backend; off refuses AI). Every edit comes back validated against the live document with status ok or rejected and a reason; nothing is written: the editor places accepted edits as suggestions. One request at a time per person, a rate limit per document. With Accept: text/event-stream the answer's text streams as it arrives (event: answer, and restart before the one repair turn), then event: result carries the same object; a request refused before the model is asked is still a JSON error. POST /ai-chat/ask streams the same way, and adds event: session {session_id} once the conversation is open and event: progress {live} every two seconds while the turn runs; POST /ai-chat/stop {session_id} stops that turn.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/studio/ai/proposals/{proposalID}/placements": gin.H{
				"post": gin.H{
					"summary":     "Record where the editor placed a proposal's edits",
					"description": "Body: {placements: [{suid, status: previewed|placed|stale|conflict|discarded, section_uid, anchor_start, anchor_end, quote}]}. A placed comment edit starts an AI rationale thread at the anchors.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/compare": gin.H{
				"get": gin.H{
					"summary":     "Compare two revisions of a policy document",
					"description": "Query: from and to, each an approved version id or current (to defaults to current). Sections are aligned by heading, units by an LCS, and a changed unit is diffed word by word. GET /policies/{id}/compare.pdf?from=&to= typesets the same comparison as a redline PDF (the sources as a zip without Typst).",
				},
			},
			"/policies/app-templates": gin.H{
				"get": gin.H{
					"summary":     "The templates made in the app, with their working copy, status, problems and source",
					"description": "Administrators. GET .../meta is the editor's vocabulary; GET .../{tid} one template; GET .../{tid}/template.json downloads the working copy; PUT .../{tid} saves it (kept whatever its problems); POST .../{tid}/publish offers it as the next version (422 with the problems while any remain); POST .../{tid}/retire and .../restore; DELETE .../{tid} only while no document was made from it; POST .../import (template JSON) and .../copy {template_id}.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/app-templates/draft": gin.H{
				"post": gin.H{
					"summary":     "Draft a template from a Wintermute library document with the AI",
					"description": "Body: {library_document_id, doc_type (optional), title (optional), instruction (optional), local_only}. The answer is checked (passages, controls, coverage, facts) and stored as a draft to review and publish. With Accept: text/event-stream: event: progress {chars} while the model writes (and every 15 s), then event: result. GET .../draft/status?local_only=1 says whether it can run and where it would go.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/library": gin.H{
				"get": gin.H{
					"summary":     "The Wintermute library documents a new policy can be started from",
					"description": "Each with id, title and ready (the library has finished reading it). The text is read from the library when the AI maps it in; nothing is uploaded here.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/policies/{id}/studio/ai/status": gin.H{
				"get": gin.H{"summary": "Whether AI proposals are available on a document, and where they go"},
			},
			"/policies/{id}/studio/ai/edits": gin.H{
				"get": gin.H{"summary": "The AI edits placed in a document as suggestions, with rationale, checked citations and the decision"},
			},
			"/policies/{id}/share-links": gin.H{
				"get": gin.H{"summary": "A Studio document's guest links and the guests they let in", "security": []gin.H{{"AdminBearer": []string{}}}},
				"post": gin.H{
					"summary":     "Invite someone from outside the installation into one document",
					"description": "Body: {label, role: viewer|commenter|editor, expires_hours (default 8, at most 168), max_uses (default 20), allow_ai}. Refused in local mode and while Settings → Policy Studio → Guest links is off. The response's path (/shared/p/{token}) is shown once; only its hash is stored. DELETE .../share-links/{linkID} withdraws a link and disconnects its guests; DELETE /policies/{id}/guests/{sessionID} removes one guest.",
					"security":    []gin.H{{"AdminBearer": []string{}}},
				},
			},
			"/shared/p/{token}": gin.H{
				"get":  gin.H{"summary": "The join page for a guest link (public; refused when guest links are off or in local mode)"},
				"post": gin.H{"summary": "Join with a display name (form field name); sets the grc_guest session cookie and redirects to /shared/studio"},
			},
			"/shared/api/state": gin.H{
				"get": gin.H{
					"summary":     "The guest's document state, reduced to what a guest may see",
					"description": "Requires the grc_guest cookie. The document comes from the session only. Also under /shared/api: comments (GET shared threads, POST as a shared thread), comments/{threadID}/replies, leave, and ai/status, ai/proposals, ai/proposals/{proposalID}/placements when the link allows AI.",
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
