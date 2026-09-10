package settings

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/aiprovider"
)

// ActorFunc names the signed-in user, for the audit trail on a stored
// credential. It matches the ActorFunc other modules take.
type ActorFunc func(*gin.Context) string

// ProviderInspector reports on the AI provider harness. It is satisfied by
// aiprovider.Router. The dependency points this way — settings knows about the
// harness, the harness does not know where its configuration is stored — so
// there is no import cycle.
type ProviderInspector interface {
	Status() aiprovider.Status
	Probe(ctx context.Context) aiprovider.Probe
}

// Handler exposes the credential and preference API.
type Handler struct {
	service   *Service
	actor     ActorFunc
	inspector ProviderInspector
	// storage names where these settings are written, for the page to show.
	storage string
}

// NewHandler returns a Handler.
func NewHandler(service *Service, actor ActorFunc) *Handler {
	return &Handler{service: service, actor: actor}
}

// WithInspector attaches the provider harness so the page can show which
// provider is active and test the connection to the local-model server.
func (h *Handler) WithInspector(inspector ProviderInspector) *Handler {
	h.inspector = inspector
	return h
}

// WithStorage names the database these settings are written to, so the page can
// say where they live.
//
// It is worth showing because this application is run two ways against two
// different files — the service on users.db, run_local.sh on local.db — and
// settings configured under one are simply absent under the other. Without this
// line that reads as "the settings were not saved", which is the one
// explanation that is not true.
func (h *Handler) WithStorage(description string) *Handler {
	h.storage = strings.TrimSpace(description)
	return h
}

// RegisterAdminRoutes attaches every route. There is deliberately no
// read-open half: even the status of a credential says something about the
// install, and nothing here is needed to use the AI features — only to
// configure them.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.GET("/api/settings/ai-credentials", h.list)
	r.PUT("/api/settings/ai-credentials/:name", h.set)
	r.DELETE("/api/settings/ai-credentials/:name", h.clear)

	r.GET("/api/settings/ai-providers", h.providers)
	r.PUT("/api/settings/ai-providers", h.setPreferences)
	r.POST("/api/settings/ai-providers/test", h.testProvider)
	r.GET("/api/settings/ai-providers/agents", h.listAgents)
	r.GET("/api/settings/ai-providers/catalog", h.listCatalog)
	r.GET("/api/settings/ai-providers/claude-models", h.listClaudeModels)
}

// providerResponse describes provider routing for the Settings page.
type providerResponse struct {
	Preferences map[string]string `json:"preferences"`
	// Status is absent when the harness was not wired, which is the case in
	// tests that construct a handler without one.
	Status *aiprovider.Status `json:"status,omitempty"`
}

func (h *Handler) providers(c *gin.Context) {
	resp := providerResponse{Preferences: h.service.Preferences()}
	if h.inspector != nil {
		st := h.inspector.Status()
		resp.Status = &st
	}
	c.JSON(http.StatusOK, resp)
}

// preferenceRequest carries the provider settings. Every field is optional so
// the page can save one without resending the others.
type preferenceRequest struct {
	Provider          *string `json:"provider"`
	ClaudeModel       *string `json:"claude_model"`
	WintermuteURL     *string `json:"wintermute_url"`
	WintermuteBackend *string `json:"wintermute_backend"`
	WintermuteModel   *string `json:"wintermute_model"`
	WintermuteAgent   *string `json:"wintermute_agent"`
}

func (h *Handler) setPreferences(c *gin.Context) {
	var req preferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected a JSON body"})
		return
	}

	updates := []struct {
		key   string
		value *string
	}{
		{PrefAIProvider, req.Provider},
		{PrefClaudeModel, req.ClaudeModel},
		{PrefWintermuteURL, req.WintermuteURL},
		{PrefWintermuteBackend, req.WintermuteBackend},
		{PrefWintermuteModel, req.WintermuteModel},
		{PrefWintermuteAgent, req.WintermuteAgent},
	}
	for _, u := range updates {
		if u.value == nil {
			continue
		}
		value := strings.TrimSpace(*u.value)
		// Reject a malformed server URL here rather than at ask time, where
		// the failure would surface as an opaque request error.
		if u.key == PrefWintermuteURL && value != "" {
			if _, err := aiprovider.ValidateEndpoint(value); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
		if err := h.service.SetPreference(u.key, value); err != nil {
			c.JSON(statusForError(err), gin.H{"error": err.Error()})
			return
		}
	}
	h.providers(c)
}

func (h *Handler) testProvider(c *gin.Context) {
	if h.inspector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the AI provider harness is not wired"})
		return
	}
	c.JSON(http.StatusOK, h.inspector.Probe(c.Request.Context()))
}

// listResponse is the payload the Settings page renders. It carries status
// only — a stored credential is never sent back to a browser.
type listResponse struct {
	Credentials []Status `json:"credentials"`
	// StorageAvailable is false when no master key could be resolved, in which
	// case the page explains that storing is disabled rather than silently
	// failing on save.
	StorageAvailable bool `json:"storage_available"`
	// Keyring describes where the master key came from. It never contains key
	// material.
	Keyring string `json:"keyring"`
	// Storage names the database these settings persist in.
	Storage string `json:"storage,omitempty"`
}

func (h *Handler) list(c *gin.Context) {
	statuses, err := h.service.Statuses()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, listResponse{
		Credentials:      statuses,
		StorageAvailable: h.service.StorageAvailable(),
		Keyring:          h.service.KeyringDescription(),
		Storage:          h.storage,
	})
}

type setRequest struct {
	Value string `json:"value"`
}

func (h *Handler) set(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	var req setRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected a JSON body with a value field"})
		return
	}

	actor := ""
	if h.actor != nil {
		actor = h.actor(c)
	}
	if err := h.service.Set(name, req.Value, actor); err != nil {
		c.JSON(statusForError(err), gin.H{"error": err.Error()})
		return
	}
	h.respondStatus(c, name)
}

func (h *Handler) clear(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if err := h.service.Clear(name); err != nil {
		c.JSON(statusForError(err), gin.H{"error": err.Error()})
		return
	}
	h.respondStatus(c, name)
}

// respondStatus returns the credential's new state, so the page can re-render
// from the response rather than issuing a second request.
func (h *Handler) respondStatus(c *gin.Context, name string) {
	st, err := h.service.Status(name)
	if err != nil {
		c.JSON(statusForError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, ErrUnknownCredential), errors.Is(err, ErrUnknownPreference):
		return http.StatusNotFound
	case errors.Is(err, ErrNoKeyring):
		// The request was valid; the server cannot store credentials.
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}
