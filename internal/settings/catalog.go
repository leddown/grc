package settings

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/aiprovider"
)

// listCatalog proxies the Wintermute server's backend and model lists, so the
// Settings page can offer both as dropdowns rather than as free text.
func (h *Handler) listCatalog(c *gin.Context) {
	provider, ok := h.wintermuteProvider(c)
	if !ok {
		return
	}
	catalog, err := provider.Catalog(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

// listClaudeModels lists the models the stored Anthropic key can address, so
// the Claude model is a choice rather than a typed id that answers with a 404
// at ask time. It is a metadata call: nothing here is billed as tokens.
func (h *Handler) listClaudeModels(c *gin.Context) {
	key := strings.TrimSpace(h.service.Get(AnthropicAPIKey))
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Anthropic API key is configured"})
		return
	}
	models, err := aiprovider.NewClaude(func() string { return key }, "").Models(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models, "default": aiprovider.DefaultClaudeModel})
}
