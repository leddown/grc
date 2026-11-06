package assuranceplan

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
	actor   func(*gin.Context) string
}

func NewHandler(service *Service, actor func(*gin.Context) string) *Handler {
	return &Handler{service: service, actor: actor}
}

// RegisterRoutes is the read surface, open to anyone with access to the page.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/assurance-plans", h.ListPage)
	r.GET("/assurance-plans/data", h.List)
	r.GET("/assurance-plans/data/:id", h.GetOne)
	r.GET("/assurance-plans/types", h.Types)
	r.GET("/assurance-plans/vocabulary", h.Vocabulary)
	r.GET("/assurance-plans/:id", h.PlanPage)
	r.GET("/assurance-plans/:id/lint", h.Lint)
	r.GET("/assurance-plans/:id/history", h.History)
	r.GET("/assurance-plans/:id/versions", h.Versions)
	r.GET("/assurance-plans/:id/versions/:n", h.Version)
}

// RegisterAdminRoutes carries every write, approval included. An approved plan
// is what a supervisor reads as the control function's commitment, so changing
// it is not left to anyone who can read it.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/assurance-plans", h.Create)
	r.PUT("/assurance-plans/:id", h.UpdateHeader)
	r.DELETE("/assurance-plans/:id", h.Delete)
	r.PUT("/assurance-plans/:id/steps", h.ReplaceSteps)
	r.PATCH("/assurance-plans/:id/steps/:stepID", h.UpdateStepStatus)
	r.PUT("/assurance-plans/:id/quarters", h.ReplaceQuarters)
	r.POST("/assurance-plans/:id/quarters/generate", h.GenerateQuarters)
	r.PUT("/assurance-plans/:id/governance", h.ReplaceGovernance)
	r.POST("/assurance-plans/:id/submit", h.Submit)
	r.POST("/assurance-plans/:id/return", h.Return)
	r.POST("/assurance-plans/:id/approve", h.Approve)
	r.POST("/assurance-plans/:id/activate", h.Activate)
	r.POST("/assurance-plans/:id/close", h.Close)
	r.POST("/assurance-plans/:id/change-request", h.ChangeRequest)
	r.POST("/assurance-plans/:id/clone", h.Clone)
}

func (h *Handler) actorOf(c *gin.Context) string {
	if h.actor == nil {
		return ""
	}
	return h.actor(c)
}

func (h *Handler) filter(c *gin.Context) Filter {
	clientID, _ := strconv.ParseInt(c.Query("client"), 10, 64)
	return Filter{
		PlanType: strings.TrimSpace(c.Query("type")),
		Status:   strings.TrimSpace(c.Query("status")),
		Year:     strings.TrimSpace(c.Query("year")),
		ClientID: clientID,
		Search:   c.Query("q"),
	}
}

func (h *Handler) ListPage(c *gin.Context) {
	f := h.filter(c)
	plans, err := h.service.List(f)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to list assurance plans")
		return
	}
	clients, err := h.service.Clients()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to list clients")
		return
	}
	page := listPageHTML(listPageData{Plans: plans, Filter: f, Clients: clients, Templates: Templates()})
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

func (h *Handler) PlanPage(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	p, err := h.service.Get(id)
	if errors.Is(err, ErrNotFound) {
		c.String(http.StatusNotFound, "assurance plan not found")
		return
	}
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the assurance plan")
		return
	}
	history, err := h.service.History(id)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the plan's history")
		return
	}
	versions, err := h.service.Versions(id)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the plan's versions")
		return
	}
	clients, err := h.service.Clients()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to list clients")
		return
	}
	page := planPageHTML(planPageData{
		Plan: p, Lint: Lint(p), History: history, Versions: versions, Clients: clients,
		Today: time.Now().UTC().Format(dateLayout), Viewer: h.actorOf(c), SepEnforced: h.service.SeparationEnforced(),
	})
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

func (h *Handler) List(c *gin.Context) {
	plans, err := h.service.List(h.filter(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, plans)
}

func (h *Handler) GetOne(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	p, err := h.service.Get(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) Types(c *gin.Context) { c.JSON(http.StatusOK, Templates()) }

func (h *Handler) Vocabulary(c *gin.Context) { c.JSON(http.StatusOK, Vocab()) }

func (h *Handler) Lint(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	findings, err := h.service.Lint(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"findings": findings, "blocking": len(BlockingFindings(findings))})
}

func (h *Handler) History(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.History(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) Versions(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.Versions(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// Version serves the approved snapshot exactly as it was stored, so its
// SHA-256 can be checked against the bytes received.
func (h *Handler) Version(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	n, err := strconv.Atoi(c.Param("n"))
	if err != nil || n <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version number"})
		return
	}
	v, err := h.service.Version(id, n)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("X-Snapshot-SHA256", v.SnapshotSHA256)
	c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(v.SnapshotJSON))
}

func (h *Handler) Create(c *gin.Context) {
	var in CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.Create(in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (h *Handler) UpdateHeader(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in Plan
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.UpdateHeader(id, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) Delete(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(id); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ReplaceSteps(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var steps []Step
	if err := c.ShouldBindJSON(&steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the list of steps"})
		return
	}
	p, err := h.service.ReplaceSteps(id, steps, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) UpdateStepStatus(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	stepID, ok := parseID(c, "stepID")
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.UpdateStepStatus(id, stepID, in.Status, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) ReplaceQuarters(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var quarters []Quarter
	if err := c.ShouldBindJSON(&quarters); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the list of quarters"})
		return
	}
	p, err := h.service.ReplaceQuarters(id, quarters, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) GenerateQuarters(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Dates bool `json:"dates"`
	}
	_ = c.ShouldBindJSON(&in)
	p, err := h.service.GenerateQuarters(id, in.Dates, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) ReplaceGovernance(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var g Governance
	if err := c.ShouldBindJSON(&g); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.ReplaceGovernance(id, g, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

type noteBody struct {
	Note       string `json:"note"`
	Reason     string `json:"reason"`
	Title      string `json:"title"`
	ApprovedBy string `json:"approved_by"`
}

func (h *Handler) lifecycle(c *gin.Context, run func(id int64, body noteBody, actor string) (any, error)) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body noteBody
	_ = c.ShouldBindJSON(&body)
	out, err := run(id, body, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) Submit(c *gin.Context) {
	h.lifecycle(c, func(id int64, _ noteBody, actor string) (any, error) { return h.service.Submit(id, actor) })
}

func (h *Handler) Return(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) { return h.service.Return(id, b.Note, actor) })
}

func (h *Handler) Activate(c *gin.Context) {
	h.lifecycle(c, func(id int64, _ noteBody, actor string) (any, error) { return h.service.Activate(id, actor) })
}

func (h *Handler) Close(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) { return h.service.Close(id, b.Note, actor) })
}

func (h *Handler) ChangeRequest(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) {
		return h.service.ChangeRequest(id, firstNonEmpty(b.Reason, b.Note), actor)
	})
}

// Approve records the signed-in user as the approver whenever there is one: a
// name typed into the request would let an author approve their own plan by
// typing someone else's. Only without a session (local mode) is the name taken
// from the body.
func (h *Handler) Approve(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) {
		approver := b.ApprovedBy
		if actor != "" {
			approver = actor
		}
		return h.service.Approve(id, approver, b.Note)
	})
}

func (h *Handler) Clone(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body noteBody
	_ = c.ShouldBindJSON(&body)
	p, err := h.service.Clone(id, body.Title, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

func parseID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, false
	}
	return id, true
}

func writeError(c *gin.Context, err error) {
	var verr *ValidationError
	var cerr *ConflictError
	var aerr ApprovalError
	switch {
	case errors.As(err, &aerr):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": aerr.Error(), "findings": aerr.Findings})
	case errors.As(err, &verr):
		c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
	case errors.As(err, &cerr):
		c.JSON(http.StatusConflict, gin.H{"error": cerr.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "assurance plan storage failed"})
	}
}
