package settings

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
