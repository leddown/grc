package nfrenrich

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// analysisTimeout bounds one run. A full-catalog analysis is many sequential
// model calls, so this is generous — but not unbounded, because the request is
// synchronous and a wedged run would otherwise hold the handler forever.
const analysisTimeout = 15 * time.Minute

// ActorFunc resolves the acting user from a request, returning "" when there
// is none (local mode). A function rather than a dependency on internal/authn,
// matching internal/policydocs and internal/todo — this module has no other
// reason to know how sessions work.
type ActorFunc func(c *gin.Context) string

type Handler struct {
	service *Service
	actor   ActorFunc
}

func NewHandler(service *Service, actor ActorFunc) *Handler {
	return &Handler{service: service, actor: actor}
}

// RegisterRoutes attaches the read surface. Mutations are registered separately
// by RegisterAdminRoutes, matching the read-open / write-admin split the other
// RCSA catalog modules use: importing a document and accepting a proposal both
// change what the catalog says, and the catalog is the deliverable.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/nfr-enrichment", h.Page)
	r.GET("/nfr-enrichment/documents", h.ListDocuments)
	r.GET("/nfr-enrichment/proposals", h.ListProposals)
}

// RegisterAdminRoutes attaches everything that writes.
//
// Listing the library sits here rather than on the read surface: it reaches
// out to the Wintermute server with this installation's client token, and that
// is an admin's credential rather than a page anyone may spend.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.GET("/nfr-enrichment/library", h.ListLibrary)
	r.POST("/nfr-enrichment/documents", h.AddDocument)
	r.DELETE("/nfr-enrichment/documents/:id", h.DeleteDocument)
	r.POST("/nfr-enrichment/documents/:id/analyze", h.Analyze)
	r.POST("/nfr-enrichment/proposals/:id/accept", h.AcceptProposal)
	r.POST("/nfr-enrichment/proposals/:id/reject", h.RejectProposal)
}

// Page renders the review UI.
func (h *Handler) Page(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pageHTML()))
}

// ---- documents ----

func (h *Handler) ListDocuments(c *gin.Context) {
	docs, err := h.service.ListDocuments()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list source documents"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"documents":   docs,
		"configured":  h.service.Configured(),
		"model":       h.service.Model(),
		"fields":      EnrichableFields,
		"library":     h.service.LibraryAvailable(),
		"library_url": h.service.LibraryURL(),
	})
}

// ListLibrary shows what the agent's library holds, so a document is chosen
// from what exists rather than named.
func (h *Handler) ListLibrary(c *gin.Context) {
	entries, err := h.service.ListLibrary(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documents": entries, "library_url": h.service.LibraryURL()})
}

type importRequest struct {
	LibraryDocID int64  `json:"library_doc_id"`
	Title        string `json:"title"`
}

// AddDocument imports one document from the agent's library.
//
// There is no upload path. Documents are uploaded to the Wintermute server,
// which extracts and chunks them — including the scans and office formats this
// application could never read — and this copies the text it produced.
func (h *Handler) AddDocument(c *gin.Context) {
	var req importRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if req.LibraryDocID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "library_doc_id is required"})
		return
	}

	doc, err := h.service.Import(c.Request.Context(), ImportInput{
		LibraryDocID: req.LibraryDocID,
		Title:        req.Title,
		ImportedBy:   h.actorOf(c),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, doc)
}

func (h *Handler) DeleteDocument(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := h.service.DeleteDocument(id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- analysis ----

type analyzeRequest struct {
	Field   string   `json:"field"`
	Domain  string   `json:"domain"`
	NFRKeys []string `json:"nfr_keys"`
	Limit   int      `json:"limit"`
}

func (h *Handler) Analyze(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req analyzeRequest
	// An empty body is a valid run: enrich the implementation field across the
	// whole catalog.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), analysisTimeout)
	defer cancel()

	result, err := h.service.Analyze(ctx, AnalyzeOptions{
		DocumentID: id,
		Field:      req.Field,
		Domain:     req.Domain,
		NFRKeys:    req.NFRKeys,
		Limit:      req.Limit,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---- proposals ----

func (h *Handler) ListProposals(c *gin.Context) {
	filter := ProposalFilter{
		Status: strings.TrimSpace(c.Query("status")),
		NFRKey: strings.TrimSpace(c.Query("nfr_key")),
	}
	if raw := strings.TrimSpace(c.Query("document_id")); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			filter.DocumentID = id
		}
	}

	proposals, err := h.service.ListProposals(filter)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"proposals": proposals})
}

type decisionRequest struct {
	// Text lets the reviewer accept a corrected version of the suggestion.
	Text string `json:"text"`
	Note string `json:"note"`
}

func (h *Handler) AcceptProposal(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req decisionRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
	}

	proposal, err := h.service.AcceptProposal(id, req.Text, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, proposal)
}

func (h *Handler) RejectProposal(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req decisionRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
	}

	proposal, err := h.service.RejectProposal(id, req.Note, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, proposal)
}

// ---- helpers ----

func idParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// actorOf names who made a decision, for the provenance record. Local mode has
// no session, and "local" is a more honest label in an audit trail than an
// empty string.
func (h *Handler) actorOf(c *gin.Context) string {
	if h.actor != nil {
		if name := strings.TrimSpace(h.actor(c)); name != "" {
			return name
		}
	}
	return "local"
}

// writeServiceError maps a service error onto a status code. Validation
// failures are the model's or the user's to fix and carry their message
// through; anything unrecognised is a 500 with a generic message, so an
// internal error never leaks a query or a path.
func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI analysis needs an AI provider: set one in Settings (no restart needed)"})
	case errors.Is(err, ErrNoLibrary):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, ErrEmptyDocument):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")})
	default:
		// Anything unrecognised is a fault, not a bad request. Reporting a
		// storage failure as a 400 with its own message tells the caller to fix
		// something they did not do wrong, and leaks the query while doing it.
		log.Printf("nfr-enrichment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
