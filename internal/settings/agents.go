package settings

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/aiprovider"
)

// listAgents proxies the Wintermute server's agent list for the Settings page,
// which cannot fetch it itself: the client token lives here and must not be
// handed to a browser.
func (h *Handler) listAgents(c *gin.Context) {
	provider, ok := h.wintermuteProvider(c)
	if !ok {
		return
	}
	agents, err := provider.Agents(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"agents": agents})
}

// wintermuteProvider builds a provider over the stored server URL and token.
// It reports the misconfiguration itself — a missing URL or token, or one the
// provider would refuse at ask time — and returns false when it has.
func (h *Handler) wintermuteProvider(c *gin.Context) (*aiprovider.Wintermute, bool) {
	base := strings.TrimSpace(h.service.Preference(PrefWintermuteURL))
	token := strings.TrimSpace(h.service.Get(WintermuteToken))
	if base == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute server URL is configured"})
		return nil, false
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute client token is configured"})
		return nil, false
	}
	// The same validation the provider applies at ask time, so a URL that would
	// be refused there is refused here rather than fetched.
	if _, err := aiprovider.ValidateEndpoint(base); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, false
	}
	return aiprovider.NewWintermute(func() aiprovider.WintermuteConfig {
		return aiprovider.WintermuteConfig{URL: base, Token: token}
	}), true
}
