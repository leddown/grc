package app

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

var markdownKnowledgeDocs = map[string]string{
	"agents":        "Agents.md",
	"changelog":     "CHANGELOG.md",
	"runtime":       "RUNTIME_ARGS.md",
	"faq":           "FAQ.md",
	"jira":          "JIRA_CONNECTOR.md",
	"risk-register": "RISK_REGISTER_FRAMEWORK.md",
	"policy-module": "POLICY_MODULE_FRAMEWORK.md",
	"templates":     "DOCUMENT_TEMPLATES.md",
}

func markdownKnowledgeDocPage(c *gin.Context) {
	name := c.Param("name")
	file, ok := markdownKnowledgeDocs[name]
	if !ok {
		c.String(http.StatusNotFound, "knowledge file not found")
		return
	}
	path := docPath(file)
	// c.File on a missing path yields a bare 404 indistinguishable from an
	// unknown doc name, which is what made the working-directory bug look like
	// a routing problem. Say which file was looked for and where.
	if _, err := os.Stat(path); err != nil {
		c.String(http.StatusNotFound, "knowledge file %s not found at %s; set DOCS_DIR (or -docs-dir) to the directory holding the documentation", file, path)
		return
	}
	c.File(path)
}
