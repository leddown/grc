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
    body { margin: 0; padding: 24px; background: #f7f3eb; color: #1c2431; font-family: Georgia, "Times New Roman", serif; }
    main { max-width: 1100px; margin: 0 auto; background: rgba(255,252,246,0.94); border: 1px solid #d7cebf; border-radius: 24px; padding: 24px; }
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
