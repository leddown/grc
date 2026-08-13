package settings

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ActorFunc names the signed-in user, for the audit trail on a stored
// credential. It matches the ActorFunc other modules take.
type ActorFunc func(*gin.Context) string

// Handler exposes the credential API.
type Handler struct {
	service *Service
	actor   ActorFunc
}

// NewHandler returns a Handler.
func NewHandler(service *Service, actor ActorFunc) *Handler {
	return &Handler{service: service, actor: actor}
}

// RegisterAdminRoutes attaches every route. There is deliberately no
// read-open half: even the status of a credential says something about the
// install, and nothing here is needed to use the AI features — only to
// configure them.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.GET("/api/settings/ai-credentials", h.list)
	r.PUT("/api/settings/ai-credentials/:name", h.set)
	r.DELETE("/api/settings/ai-credentials/:name", h.clear)
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
	case errors.Is(err, ErrUnknownCredential):
		return http.StatusNotFound
	case errors.Is(err, ErrNoKeyring):
		// The request was valid; the server cannot store credentials.
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}
