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
// RCSA catalog modules use: ingesting a document and accepting a proposal both
// change what the catalog says, and the catalog is the deliverable.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/nfr-enrichment", h.Page)
	r.GET("/nfr-enrichment/documents", h.ListDocuments)
	r.GET("/nfr-enrichment/proposals", h.ListProposals)
}

// RegisterAdminRoutes attaches everything that writes.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
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
		"documents":  docs,
		"configured": h.service.Configured(),
		"model":      h.service.Model(),
		"fields":     EnrichableFields,
	})
}

type addURLRequest struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// AddDocument accepts either a multipart upload or a JSON body naming a URL.
func (h *Handler) AddDocument(c *gin.Context) {
	actor := h.actorOf(c)

	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		// The body is bounded before FormFile reads through it, so an oversized
		// upload is refused rather than buffered first.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxDocumentBytes+1024)

		fileHeader, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "a file is required"})
			return
		}
		file, err := fileHeader.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "could not read the uploaded file"})
			return
		}
		defer file.Close()

		body := make([]byte, 0, fileHeader.Size)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := file.Read(buf)
			body = append(body, buf[:n]...)
			if len(body) > MaxDocumentBytes {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{
					"error": "the document exceeds the size limit"})
				return
			}
			if readErr != nil {
				break
			}
		}

		doc, err := h.service.AddUpload(UploadInput{
			Title:      c.PostForm("title"),
			Filename:   fileHeader.Filename,
			MediaType:  fileHeader.Header.Get("Content-Type"),
			Body:       body,
			UploadedBy: actor,
		})
		if err != nil {
			writeServiceError(c, err)
			return
		}
		c.JSON(http.StatusCreated, doc)
		return
	}

	var req addURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	doc, err := h.service.AddURL(c.Request.Context(), req.URL, req.Title, actor)
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
			"error": "AI analysis needs an Anthropic API key: set one in Settings (no restart needed)"})
	case errors.Is(err, ErrUnsupportedMedia), errors.Is(err, ErrEmptyDocument):
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": err.Error()})
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
