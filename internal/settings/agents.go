package settings

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/aiprovider"
)

// agentListTimeout bounds the lookup. It is a small GET against a server on the
// operator's own network.
const agentListTimeout = 15 * time.Second

// agentsClient fetches the agent list from the configured Wintermute server.
//
// The Settings page cannot call that server itself: the client token lives here
// and must not be handed to a browser, and the server is often on a network the
// browser cannot reach. So this proxies one read — the list of agents — and
// nothing else.
var agentsClient = &http.Client{Timeout: agentListTimeout}

// Agent is one agent profile on the Wintermute server, as the page needs it.
type Agent struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Sources     []string `json:"sources"`
}

// listAgents proxies GET /api/v1/agents on the configured Wintermute server.
func (h *Handler) listAgents(c *gin.Context) {
	base := strings.TrimRight(strings.TrimSpace(h.service.Preference(PrefWintermuteURL)), "/")
	token := strings.TrimSpace(h.service.Get(WintermuteToken))
	if base == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute server URL is configured"})
		return
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute client token is configured"})
		return
	}
	// The same validation the provider applies, so a URL that would be refused
	// at ask time is refused here rather than fetched.
	validated, err := aiprovider.ValidateEndpoint(base)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet,
		validated+"/api/v1/agents", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not build the request"})
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := agentsClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("could not reach Wintermute: %v", err)})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not read Wintermute's reply"})
		return
	}
	if resp.StatusCode == http.StatusNotFound {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "this Wintermute server has no agents endpoint — it predates agent profiles"})
		return
	}
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": fmt.Sprintf("Wintermute returned HTTP %d", resp.StatusCode)})
		return
	}

	var payload struct {
		Agents []Agent `json:"agents"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Wintermute returned unreadable JSON"})
		return
	}
	if payload.Agents == nil {
		payload.Agents = []Agent{}
	}
	c.JSON(http.StatusOK, gin.H{"agents": payload.Agents})
}
