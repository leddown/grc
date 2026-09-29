package policyai

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"grc/internal/policydocs"
	"grc/internal/policystudio"
)

// Handler serves the proposal engine's routes, under /policies/:id/studio/ai.
type Handler struct {
	engine *Engine
	actor  func(c *gin.Context) string
}

func NewHandler(engine *Engine, actor func(c *gin.Context) string) *Handler {
	return &Handler{engine: engine, actor: actor}
}

// RegisterReadRoutes attaches what a /policies reader may use: whether AI is
// available and where it goes, and the rationale behind AI suggestions in the
// document.
func (h *Handler) RegisterReadRoutes(r gin.IRouter) {
	r.GET("/policies/:id/studio/ai/status", h.Status)
	r.GET("/policies/:id/studio/ai/edits", h.PlacedEdits)
}

// RegisterAdminRoutes attaches the routes that spend model calls or change
// what is recorded; AI is admin-only, like every feature that spends model
// calls.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/policies/:id/studio/ai/proposals", h.Propose)
	r.POST("/policies/:id/studio/ai/proposals/:proposalID/placements", h.Placements)
}

func documentID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document id"})
		return 0, false
	}
	return id, true
}

// Fail writes an engine error. It is exported for the AI dock, which answers
// through the same rules.
func Fail(c *gin.Context, err error) {
	var r Refusal
	switch {
	case errors.As(err, &r):
		c.JSON(r.Status, gin.H{"error": r.Msg})
	case errors.Is(err, policydocs.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
	case policydocs.IsValidation(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		// Never the document or the prompt: the path and the error only.
		slog.Error("policy ai: request failed", "path", c.FullPath(), "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the AI request could not be completed; try again"})
	}
}

func (h *Handler) Status(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	st, err := h.engine.Status(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *Handler) PlacedEdits(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	edits, err := h.engine.PlacedEdits(id)
	if err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, edits)
}

// maxRequestBytes bounds a proposal request body.
const maxRequestBytes = 64 << 10

func (h *Handler) Propose(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	// The request's context: a person who navigates away cancels the call.
	res, err := h.engine.Propose(c.Request.Context(), id, req, h.actor(c))
	if err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) Placements(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	pid, err := strconv.ParseInt(c.Param("proposalID"), 10, 64)
	if err != nil || pid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proposal id"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	var in struct {
		Placements []Placement `json:"placements"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.Placements) == 0 || len(in.Placements) > maxEdits {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid placements"})
		return
	}
	if err := h.engine.RecordPlacements(id, pid, in.Placements, h.actor(c), false); err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"recorded": len(in.Placements)})
}

// RegisterGuestRoutes attaches the guest's AI routes under /shared/api,
// behind the Studio's guest gate. They answer only when the guest's link
// allows AI; a guest may preview a proposal, and only an editor may place it.
func (h *Handler) RegisterGuestRoutes(r gin.IRouter, gate gin.HandlerFunc) {
	g := r.Group("/shared/api/ai", gate)
	g.GET("/status", h.GuestStatus)
	g.POST("/proposals", h.GuestPropose)
	g.POST("/proposals/:proposalID/placements", h.GuestPlacements)
}

func guestWithAI(c *gin.Context) (policystudio.GuestSession, bool) {
	g, ok := policystudio.GuestFrom(c)
	if !ok || !g.AllowAI {
		c.JSON(http.StatusForbidden, gin.H{"error": "This invitation does not include the AI assistant."})
		return policystudio.GuestSession{}, false
	}
	return g, true
}

func (h *Handler) GuestStatus(c *gin.Context) {
	g, ok := policystudio.GuestFrom(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no guest session"})
		return
	}
	if !g.AllowAI {
		c.JSON(http.StatusOK, Status{Reason: "This invitation does not include the AI assistant."})
		return
	}
	st, err := h.engine.Status(c.Request.Context(), g.DocumentID)
	if err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *Handler) GuestPropose(c *gin.Context) {
	g, ok := guestWithAI(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	res, err := h.engine.Propose(c.Request.Context(), g.DocumentID, req, g.Actor())
	if err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) GuestPlacements(c *gin.Context) {
	g, ok := guestWithAI(c)
	if !ok {
		return
	}
	pid, err := strconv.ParseInt(c.Param("proposalID"), 10, 64)
	if err != nil || pid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proposal id"})
		return
	}
	var in struct {
		Placements []Placement `json:"placements"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.Placements) == 0 || len(in.Placements) > maxEdits {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid placements"})
		return
	}
	for _, p := range in.Placements {
		if p.Status == PlacementPlaced && g.Role != policystudio.RoleEditor {
			c.JSON(http.StatusForbidden, gin.H{"error": "This invitation lets you preview proposals, not place them."})
			return
		}
	}
	if err := h.engine.RecordPlacements(g.DocumentID, pid, in.Placements, g.Actor(), true); err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"recorded": len(in.Placements)})
}
