package regcoverage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// analysisTimeout bounds one full analysis. A regulation is one model call per
// article plus a summary, so a hundred-article act is a long wait — but a
// bounded one, because the request is synchronous and a wedged run would
// otherwise hold the handler forever.
const analysisTimeout = 45 * time.Minute

// chatTimeout bounds one question or revision.
const chatTimeout = 5 * time.Minute

// ActorFunc resolves the acting user from a request, returning "" when there is
// none (local mode). A function rather than a dependency on internal/authn,
// matching internal/nfrenrich and internal/policydocs.
type ActorFunc func(c *gin.Context) string

type Handler struct {
	service *Service
	actor   ActorFunc
}

func NewHandler(service *Service, actor ActorFunc) *Handler {
	return &Handler{service: service, actor: actor}
}

// RegisterRoutes attaches the read surface: the pages, the report in its
// various forms, and the original document.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/regulation-coverage", h.IndexPage)
	r.GET("/regulation-coverage/:id", h.ReportPage)
	r.GET("/regulation-coverage/:id/report.json", h.ReportJSON)
	r.GET("/regulation-coverage/:id/report.pdf", h.ReportPDF)
	r.GET("/regulation-coverage/:id/source", h.Source)
	r.GET("/regulation-coverage/:id/versions", h.ListVersions)
	r.GET("/regulation-coverage/:id/chat", h.ListChat)
	r.GET("/regulation-coverage/regulations", h.ListRegulations)
}

// RegisterAdminRoutes attaches everything that writes or spends. Analysis and
// chat both cost model calls, which is reason enough to keep them behind the
// same gate as the mutations.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/regulation-coverage/regulations", h.Upload)
	r.DELETE("/regulation-coverage/:id", h.Delete)
	r.POST("/regulation-coverage/:id/analyze", h.Analyze)
	r.POST("/regulation-coverage/:id/chat", h.Ask)
	r.DELETE("/regulation-coverage/:id/chat", h.ClearChat)
	r.POST("/regulation-coverage/:id/revise", h.Revise)
}

func (h *Handler) actorOf(c *gin.Context) string {
	if h.actor == nil {
		return ""
	}
	return h.actor(c)
}

// ---- pages ----

func (h *Handler) IndexPage(c *gin.Context) {
	frameworks, err := h.service.Frameworks()
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8",
		[]byte(indexPageHTML(frameworks, h.service.Configured(), h.service.Model())))
}

func (h *Handler) ReportPage(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	report, err := h.service.Report(id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	versions, err := h.service.ListVersions(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	turns, err := h.service.ListChat(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(reportPageHTML(reportPageData{
		Report:       report,
		Versions:     versions,
		Chat:         turns,
		AIConfigured: h.service.Configured(),
		Model:        h.service.Model(),
		PDFAvailable: h.service.PDFAvailable(),
	})))
}

// ---- JSON ----

func (h *Handler) ListRegulations(c *gin.Context) {
	regs, err := h.service.ListRegulations()
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"regulations": regs,
		"configured":  h.service.Configured(),
		"model":       h.service.Model(),
	})
}

func (h *Handler) ReportJSON(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	report, err := h.service.Report(id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, report)
}

func (h *Handler) ListVersions(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	versions, err := h.service.ListVersions(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"versions": versions})
}

func (h *Handler) ReportPDF(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(c, pdfTimeout)
	defer cancel()

	pdf, filename, err := h.service.RenderPDF(ctx, id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Content-Disposition", contentDisposition("attachment", filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

// Source serves the uploaded document back, unmodified, so the report can be
// read against the regulation as published.
//
// It is served inline for the types a browser renders natively and as a
// download for the rest, with sniffing disabled and scripting refused: this is
// a file an operator uploaded, and it is served from the application's own
// origin.
func (h *Handler) Source(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	src, err := h.service.Source(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	disposition := "attachment"
	if src.MediaType == "application/pdf" || strings.HasPrefix(src.MediaType, "text/plain") {
		disposition = "inline"
	}
	c.Header("Content-Disposition", contentDisposition(disposition, src.Filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'; object-src 'none'")
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, src.MediaType, src.Content)
}

func (h *Handler) ListChat(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	turns, err := h.service.ListChat(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"turns": turns, "configured": h.service.Configured()})
}

// ---- mutations ----

func (h *Handler) Upload(c *gin.Context) {
	// Bounded before FormFile reads through it, so an oversized upload is
	// refused rather than buffered first.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxUploadBytes+(1<<20))

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
	defer func() { _ = file.Close() }()

	body, err := io.ReadAll(io.LimitReader(file, MaxUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not read the uploaded file"})
		return
	}
	if len(body) > MaxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("the document exceeds the %s limit", humanBytes(MaxUploadBytes))})
		return
	}

	reg, err := h.service.Upload(UploadInput{
		Title:      c.PostForm("title"),
		Filename:   fileHeader.Filename,
		Framework:  c.PostForm("framework"),
		Body:       body,
		UploadedBy: h.actorOf(c),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, reg)
}

func (h *Handler) Delete(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := h.service.DeleteRegulation(id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Analyze(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(c, analysisTimeout)
	defer cancel()

	result, err := h.service.Analyze(ctx, id, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

type askRequest struct {
	Question string `json:"question"`
}

func (h *Handler) Ask(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req askRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	ctx, cancel := contextWithTimeout(c, chatTimeout)
	defer cancel()

	turn, err := h.service.Ask(ctx, id, req.Question, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, turn)
}

func (h *Handler) ClearChat(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := h.service.ClearChat(id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type reviseRequest struct {
	Section     string `json:"section"`
	Instruction string `json:"instruction"`
}

func (h *Handler) Revise(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req reviseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	ctx, cancel := contextWithTimeout(c, chatTimeout)
	defer cancel()

	report, err := h.service.Revise(ctx, id, req.Section, req.Instruction, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, report)
}

// ---- helpers ----

func idParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid regulation id"})
		return 0, false
	}
	return id, true
}

// versionParam reads ?version=N. Anything unparseable means the latest, which
// is the same thing an absent parameter means.
func versionParam(c *gin.Context) int {
	n, err := strconv.Atoi(strings.TrimSpace(c.Query("version")))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// contextWithTimeout bounds the work and inherits the request's cancellation:
// a reader who closes the tab mid-analysis should not leave the run spending.
func contextWithTimeout(c *gin.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), d)
}

// contentDisposition builds the header with a filename that cannot break out
// of the quoted string.
func contentDisposition(kind, filename string) string {
	safe := strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == 127 {
			return -1
		}
		return r
	}, filename)
	if strings.TrimSpace(safe) == "" {
		safe = "document"
	}
	return fmt.Sprintf("%s; filename=%q", kind, safe)
}

// writeServiceError maps the module's error vocabulary onto status codes.
func writeServiceError(c *gin.Context, err error) {
	var notFoundErr ErrNotFound
	var invalidErr ErrInvalid
	switch {
	case errors.As(err, &notFoundErr):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.As(err, &invalidErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "the analysis timed out"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	}
}
