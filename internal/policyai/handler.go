package policyai

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

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
	r.GET("/policies/library", h.Library)
	r.GET("/policies/app-templates/draft/status", h.TemplateDraftStatus)
	r.POST("/policies/app-templates/draft", h.DraftTemplate)
}

// templateHeartbeat is how often a draft's stream says it is still working.
var templateHeartbeat = 15 * time.Second

// TemplateDraftStatus says whether a template can be drafted from the library
// and where the request would go; ?local_only=1 asks about a local-only one.
func (h *Handler) TemplateDraftStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.engine.TemplateDraftStatus(c.Request.Context(), c.Query("local_only") == "1"))
}

// DraftTemplate drafts a template from a library document. Asked for a
// stream, it reports progress (event: progress, {chars}) while the model
// writes, then the stored draft (event: result).
func (h *Handler) DraftTemplate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	var in TemplateDraftRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if !WantsStream(c) {
		t, err := h.engine.DraftTemplate(c.Request.Context(), in, h.actor(c), nil)
		if err != nil {
			Fail(c, err)
			return
		}
		c.JSON(http.StatusCreated, t)
		return
	}
	sse := NewSSE(c)
	sse.Event("progress", gin.H{"chars": 0})
	var mu sync.Mutex
	last := 0
	// A provider that answers whole (Wintermute) is silent until it is done,
	// and a proxy closes a silent connection (nginx after proxy_read_timeout):
	// the heartbeat keeps a long draft's stream open. It stops before the
	// handler returns, after which the response must not be written.
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(templateHeartbeat)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				mu.Lock()
				n := last
				mu.Unlock()
				sse.Event("progress", gin.H{"chars": n})
			}
		}
	}()
	t, err := h.engine.DraftTemplate(c.Request.Context(), in, h.actor(c), func(chars int) {
		mu.Lock()
		defer mu.Unlock()
		// A few events a second is plenty to show it moving.
		if chars-last >= 400 {
			last = chars
			sse.Event("progress", gin.H{"chars": chars})
		}
	})
	close(done)
	wg.Wait()
	if err != nil {
		sse.Fail(err)
		return
	}
	sse.Event("result", t)
}

// Library lists the Wintermute library documents a new document can start
// from.
func (h *Handler) Library(c *gin.Context) {
	docs, err := h.engine.LibraryDocuments(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, docs)
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
	h.propose(c, id, req, h.actor(c))
}

// propose answers with the result as JSON, or as a stream of events ending in
// it when the client asked for one.
func (h *Handler) propose(c *gin.Context, id int64, req Request, actor string) {
	if !WantsStream(c) {
		res, err := h.engine.Propose(c.Request.Context(), id, req, actor)
		if err != nil {
			Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, res)
		return
	}
	sse := NewSSE(c)
	res, err := h.engine.ProposeStream(c.Request.Context(), id, req, actor, sse)
	if err != nil {
		sse.Fail(err)
		return
	}
	sse.Event("result", res)
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
	h.propose(c, g.DocumentID, req, g.Actor())
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
