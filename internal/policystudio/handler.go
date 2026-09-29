package policystudio

import (
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
	"grc/internal/policydocs"
)

// Handler serves the Studio's pages and APIs.
type Handler struct {
	service *Service
	actor   func(c *gin.Context) string
	guest   GuestPage
}

func NewHandler(service *Service, actor func(c *gin.Context) string) *Handler {
	return &Handler{service: service, actor: actor}
}

// RegisterReadRoutes attaches what a /policies reader may use. They sit under
// /policies, so the page-access middleware gates them like the rest of the
// module.
func (h *Handler) RegisterReadRoutes(r gin.IRouter) {
	r.GET("/policies/:id/studio", h.Page)
	r.GET("/policies/:id/studio/state", h.State)
	r.GET("/policies/templates", h.ListTemplates)
	r.GET("/policies/:id/studio/comments", h.ListThreads)
	r.GET("/policies/:id/studio/provenance", h.Provenance)
}

// RegisterAdminRoutes attaches the structure commands and migration, behind
// the admin gate.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/policies/from-template", h.FromTemplate)
	r.POST("/policies/:id/studio/migrate", h.Migrate)
	r.POST("/policies/:id/studio/sections", h.AddSection)
	r.POST("/policies/:id/studio/reorder", h.Reorder)
	r.PATCH("/policies/:id/studio/sections/:uid", h.SetKind)
	r.DELETE("/policies/:id/studio/sections/:uid", h.DeleteSection)
	r.POST("/policies/:id/studio/sections/:uid/restore", h.Restore)
	r.POST("/policies/:id/studio/decisions", h.Decide)
	r.POST("/policies/:id/studio/comments", h.CreateThread)
	r.POST("/policies/:id/studio/comments/:threadID/replies", h.Reply)
	r.PATCH("/policies/:id/studio/comments/:threadID", h.SetThreadStatus)
}

// RegisterPublicRoutes attaches the two routes that are not behind the page
// gate. The collaboration socket authorizes itself -- one decision function,
// Service.decide, for every upgrade -- and the bundle is static code with no
// data in it.
func (h *Handler) RegisterPublicRoutes(r gin.IRouter) {
	r.GET("/collab/policies/:id", h.Collab)
	r.GET("/assets/policy-studio/:file", serveAsset)
}

func documentID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document id"})
		return 0, false
	}
	return id, true
}

func (h *Handler) fail(c *gin.Context, err error) {
	var approval policydocs.ApprovalError
	var conflict ErrConflict
	switch {
	case errors.As(err, &conflict):
		c.JSON(http.StatusConflict, gin.H{"error": conflict.Msg})
	case errors.Is(err, policydocs.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "document or section not found"})
	case policydocs.IsValidation(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.As(err, &approval):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "document is not ready for approval", "findings": approval.Findings})
	default:
		h.service.log.Error("policy studio: request failed", "path", c.FullPath(), "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the Studio could not complete that; try again"})
	}
}

func (h *Handler) identity(c *gin.Context) Identity {
	id, _ := h.service.identity(c.Request)
	return id
}

// Collab upgrades to the document's collaboration room.
func (h *Handler) Collab(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Request.SetPathValue("room", RoomName(id))
	h.service.collab.ServeHTTP(c.Writer, c.Request)
}

// State is what the Studio polls. It answers 304 when nothing changed.
func (h *Handler) State(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	st, etag, err := h.service.State(id, h.identity(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("ETag", etag)
	c.Header("Cache-Control", "no-cache")
	if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, st)
}

// ListThreads returns the document's comment threads. Everyone who reaches
// this route is internal staff; the guest API (Phase 4) asks for
// AudienceShared.
func (h *Handler) ListThreads(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	if _, err := h.service.policies.GetDocument(id); err != nil {
		h.fail(c, err)
		return
	}
	threads, err := h.service.Threads(id, AudienceInternal)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, threads)
}

// Provenance returns who wrote and who decided what, per section.
func (h *Handler) Provenance(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	if _, err := h.service.policies.GetDocument(id); err != nil {
		h.fail(c, err)
		return
	}
	out, err := h.service.Provenance(id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// Decide records who accepted or rejected which suggestions.
func (h *Handler) Decide(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	var req DecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	out, err := h.service.Decide(id, req, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"decided": out})
}

func threadID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("threadID"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread id"})
		return 0, false
	}
	return id, true
}

// CreateThread starts a comment thread.
func (h *Handler) CreateThread(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	var in NewThread
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	t, err := h.service.CreateThread(id, in, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, t)
}

// Reply adds a comment to a thread.
func (h *Handler) Reply(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	tid, ok := threadID(c)
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	t, err := h.service.Reply(id, tid, in.Body, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// SetThreadStatus resolves or reopens a thread.
func (h *Handler) SetThreadStatus(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	tid, ok := threadID(c)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	t, err := h.service.SetThreadStatus(id, tid, in.Status, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// ListTemplates describes the templates a document can start from.
func (h *Handler) ListTemplates(c *gin.Context) {
	type summary struct {
		ID           string         `json:"id"`
		Version      string         `json:"version"`
		Title        string         `json:"title"`
		DocType      string         `json:"doc_type"`
		Description  string         `json:"description"`
		Frameworks   []string       `json:"frameworks"`
		Default      bool           `json:"default"`
		SectionCount int            `json:"section_count"`
		Facts        []TemplateFact `json:"facts"`
	}
	out := []summary{}
	for _, t := range h.service.Templates() {
		out = append(out, summary{ID: t.ID, Version: t.Version, Title: t.Title, DocType: t.DocType, Description: t.Description,
			Frameworks: t.Frameworks, Default: t.Default, SectionCount: len(t.Sections), Facts: t.Facts})
	}
	c.JSON(http.StatusOK, out)
}

// FromTemplate creates a Studio document from a template, server-side.
func (h *Handler) FromTemplate(c *gin.Context) {
	var in CreateRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	res, err := h.service.CreateFromTemplate(in, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) Migrate(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	doc, err := h.service.Migrate(id, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (h *Handler) AddSection(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	var in struct {
		AfterUID string `json:"after_uid"`
		Kind     string `json:"kind"`
		Heading  string `json:"heading"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid section payload"})
		return
	}
	sec, err := h.service.AddSection(id, in.AfterUID, in.Kind, in.Heading)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, sec)
}

func (h *Handler) Reorder(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	var in struct {
		UIDs []string `json:"uids"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reorder payload"})
		return
	}
	if err := h.service.ReorderSections(id, in.UIDs); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) SetKind(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	var in struct {
		Kind string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if err := h.service.SetSectionKind(id, c.Param("uid"), in.Kind); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) DeleteSection(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteSection(id, c.Param("uid")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Restore(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	if err := h.service.RestoreSection(id, c.Param("uid")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// studioCSP is sent Report-Only on the internal Studio page: it measures what
// an enforced policy would break (the theme layer's injected inline script
// among it) before the guest page enforces one.
const studioCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; form-action 'self'"

// Page renders the Studio shell; the embedded bundle builds the rest.
func (h *Handler) Page(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	doc, err := h.service.policies.GetDocument(id)
	if err != nil {
		if errors.Is(err, policydocs.ErrNotFound) {
			c.String(http.StatusNotFound, "policy document not found")
			return
		}
		h.fail(c, err)
		return
	}
	m, err := LoadManifest()
	if err != nil {
		h.service.log.Error("policy studio: bundle", "error", err)
		c.String(http.StatusServiceUnavailable, "The Policy Studio editor bundle is missing or damaged in this build; rebuild with scripts/build-policy-studio.sh.")
		return
	}
	who := h.identity(c)
	c.Header("Content-Security-Policy-Report-Only", studioCSP)
	c.Header("Referrer-Policy", "same-origin")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(studioPage(doc, who, m)))
}

func studioPage(doc policydocs.Document, who Identity, m Manifest) string {
	attr := html.EscapeString
	canManage := "false"
	if who.Admin {
		canManage = "true"
	}
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + attr(doc.Title) + ` · Policy Studio</title>
<link rel="stylesheet" href="/assets/policy-studio/` + attr(m.Entry.CSS) + `">
<script src="/assets/policy-studio/` + attr(m.Entry.JS) + `" defer></script>
</head>
<body>
<main>
` + pageui.Nav("/policies") + `
<div id="policy-studio" class="ps-app"
  data-document-id="` + strconv.FormatInt(doc.ID, 10) + `"
  data-user="` + attr(who.Username) + `"
  data-can-manage="` + canManage + `">
  <noscript>The Policy Studio needs JavaScript. <a href="/policies/` + strconv.FormatInt(doc.ID, 10) + `/view">Read the document instead.</a></noscript>
  <p class="ps-loading" role="status">Opening ` + attr(doc.Title) + `…</p>
</div>
</main>
</body>
</html>`)
	return b.String()
}
