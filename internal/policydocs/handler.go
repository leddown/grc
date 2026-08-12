package policydocs

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// IdentityFunc resolves the acting user from a request, and returns "" when
// there is none (local mode, or an unauthenticated route).
//
// A function rather than a dependency on internal/authn: this module has no
// other reason to know how sessions work, and injecting the lookup keeps the
// direction of that dependency pointing outward.
type IdentityFunc func(c *gin.Context) string

type Handler struct {
	service  *Service
	identity IdentityFunc
}

// NewHandler builds the handler. identity may be nil, in which case documents
// are created without an author and the separation-of-duties check has nothing
// to compare.
func NewHandler(service *Service, identity IdentityFunc) *Handler {
	return &Handler{service: service, identity: identity}
}

// actingUser resolves who is making the request.
func (h *Handler) actingUser(c *gin.Context) string {
	if h.identity == nil {
		return ""
	}
	return h.identity(c)
}

// RegisterReadRoutes attaches the read-only pages and JSON endpoints.
func (h *Handler) RegisterReadRoutes(r gin.IRouter) {
	r.GET("/policies", h.ListPage)
	r.GET("/policies/manage", h.ManagePage)
	r.GET("/policies/meta", h.Meta)
	r.GET("/policies/data", h.DocumentsData)
	r.GET("/policies/coverage", h.CoveragePage)
	r.GET("/policies/coverage/data", h.CoverageData)
	r.GET("/policies/control-search", h.ControlSearch)
	r.GET("/policies/:id/controls", h.DocumentControlRefs)
	r.GET("/policies/:id/sections/:sectionID/controls", h.SectionControlRefs)
	r.GET("/policies/:id/data", h.DocumentData)
	r.GET("/policies/:id/sections", h.SectionsData)
	r.GET("/policies/:id/versions", h.VersionsData)
	r.GET("/policies/:id/lint", h.LintData)
	r.GET("/policies/:id/export.md", h.ExportMarkdown)
	r.GET("/policies/:id/export.html", h.ExportHTML)
	r.GET("/policies/:id/export.json", h.ExportTemplateJSON)
	r.GET("/policies/:id/view", h.ViewPage)
}

// RegisterAdminRoutes attaches the mutating endpoints, guarded by the same
// admin middleware as the rest of the app in the wiring.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/policies", h.CreateDocument)
	r.PUT("/policies/:id", h.UpdateDocument)
	r.DELETE("/policies/:id", h.DeleteDocument)

	r.POST("/policies/:id/submit", h.SubmitForReview)
	r.POST("/policies/:id/approve", h.Approve)
	r.POST("/policies/:id/reopen", h.ReturnToDraft)
	r.POST("/policies/:id/retire", h.Retire)

	// Sections are nested under their document so the router never has to
	// choose between a static child and a wildcard at the same segment, and so
	// every section mutation carries the document it belongs to.
	r.POST("/policies/:id/sections", h.CreateSection)
	r.POST("/policies/:id/sections/reorder", h.ReorderSections)
	r.PUT("/policies/:id/sections/:sectionID", h.UpdateSection)
	r.DELETE("/policies/:id/sections/:sectionID", h.DeleteSection)

	r.POST("/policies/:id/sections/:sectionID/controls", h.AttachControl)
	r.DELETE("/policies/:id/sections/:sectionID/controls/:refID", h.DetachControl)
}

// ---- Pages ----

func writeHTML(c *gin.Context, body string) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(body))
}

func (h *Handler) ListPage(c *gin.Context)   { writeHTML(c, listPageHTML(false)) }
func (h *Handler) ManagePage(c *gin.Context) { writeHTML(c, listPageHTML(true)) }

func (h *Handler) ViewPage(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	_, body, err := h.service.ExportHTML(id)
	if err != nil {
		h.fail(c, err, "failed to render document")
		return
	}
	writeHTML(c, body)
}

// ---- Metadata ----

// Meta gives the editor its vocabularies so the option lists cannot drift from
// what the service will actually accept.
func (h *Handler) Meta(c *gin.Context) {
	kinds := make([]gin.H, 0, len(SectionKinds))
	for _, kind := range SectionKinds {
		kinds = append(kinds, gin.H{"value": kind, "label": SectionKindLabels[kind]})
	}
	required := make(map[string][]string, len(DocTypes))
	for _, docType := range DocTypes {
		required[docType] = RequiredKinds(docType)
	}
	c.JSON(http.StatusOK, gin.H{
		"doc_types":       DocTypes,
		"statuses":        Statuses,
		"section_kinds":   kinds,
		"classifications": Classifications,
		"frameworks":      Frameworks,
		"provenances":     Provenances,
		"required_kinds":  required,
		"coverage_levels": CoverageLevels,
	})
}

// ---- Data (JSON) ----

func (h *Handler) DocumentsData(c *gin.Context) {
	clientID, _ := strconv.ParseInt(c.Query("client_id"), 10, 64)
	items, err := h.service.ListDocuments(Filter{
		ClientID:  clientID,
		DocType:   c.Query("doc_type"),
		Status:    c.Query("status"),
		Framework: c.Query("framework"),
		Search:    c.Query("search"),
		DueReview: c.Query("due_review") == "1",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list documents"})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) DocumentData(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	doc, err := h.service.GetDocument(id)
	if err != nil {
		h.fail(c, err, "failed to load document")
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (h *Handler) SectionsData(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListSections(id)
	if err != nil {
		h.fail(c, err, "failed to load sections")
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) VersionsData(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListVersions(id)
	if err != nil {
		h.fail(c, err, "failed to load versions")
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) LintData(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	findings, err := h.service.Lint(id)
	if err != nil {
		h.fail(c, err, "failed to lint document")
		return
	}
	c.JSON(http.StatusOK, gin.H{"findings": findings, "blocking": len(BlockingFindings(findings))})
}

// ---- Export ----

func (h *Handler) ExportMarkdown(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	doc, body, err := h.service.ExportMarkdown(id)
	if err != nil {
		h.fail(c, err, "failed to export document")
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+exportFilename(doc, "md")+`"`)
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(body))
}

func (h *Handler) ExportHTML(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	doc, body, err := h.service.ExportHTML(id)
	if err != nil {
		h.fail(c, err, "failed to export document")
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+exportFilename(doc, "html")+`"`)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(body))
}

// ExportTemplateJSON emits the render payload the Typst templates consume:
//
//	curl -o doc.json .../policies/12/export.json
//	cd templates && ./build.sh policy ../../doc.json
//
// Served as a download rather than inline JSON because that is how it is used —
// saved to a file and handed to the renderer.
func (h *Handler) ExportTemplateJSON(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	payload, err := h.service.ExportTemplateJSON(id)
	if err != nil {
		h.fail(c, err, "failed to export document")
		return
	}
	doc, docErr := h.service.GetDocument(id)
	if docErr == nil {
		c.Header("Content-Disposition", `attachment; filename="`+exportFilename(doc, "json")+`"`)
	}
	c.IndentedJSON(http.StatusOK, payload)
}

// ---- Mutations ----

func (h *Handler) CreateDocument(c *gin.Context) {
	var payload Document
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document payload"})
		return
	}
	// Overwritten, never taken from the body: an author a caller can choose is
	// no basis for refusing a self-approval.
	payload.Author = h.actingUser(c)
	created, err := h.service.CreateDocument(payload)
	if err != nil {
		h.fail(c, err, "failed to create document")
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) UpdateDocument(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var payload Document
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document payload"})
		return
	}
	updated, err := h.service.UpdateDocument(id, payload)
	if err != nil {
		h.fail(c, err, "failed to update document")
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) DeleteDocument(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteDocument(id); err != nil {
		h.fail(c, err, "failed to delete document")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) SubmitForReview(c *gin.Context) { h.runTransition(c, h.service.SubmitForReview) }
func (h *Handler) ReturnToDraft(c *gin.Context)   { h.runTransition(c, h.service.ReturnToDraft) }
func (h *Handler) Retire(c *gin.Context)          { h.runTransition(c, h.service.Retire) }

func (h *Handler) runTransition(c *gin.Context, fn func(int64) (Document, error)) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	doc, err := fn(id)
	if err != nil {
		h.fail(c, err, "failed to change document status")
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (h *Handler) Approve(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var payload struct {
		ApprovedBy    string `json:"approved_by"`
		ChangeSummary string `json:"change_summary"`
	}
	// An approval with no body is still meaningful: the service falls back to
	// requiring a non-empty approver and will reject it with a clear message.
	_ = c.ShouldBindJSON(&payload)

	approvedBy := payload.ApprovedBy
	// When the request is authenticated, the approver is who is signed in, not
	// who they say they are. A caller-supplied name would let an author approve
	// their own document simply by typing somebody else's, which would leave the
	// separation-of-duties check and the audit trail both worthless.
	if acting := h.actingUser(c); acting != "" {
		approvedBy = acting
	}

	doc, err := h.service.Approve(id, approvedBy, payload.ChangeSummary)
	if err != nil {
		h.fail(c, err, "failed to approve document")
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (h *Handler) CreateSection(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var payload Section
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid section payload"})
		return
	}
	payload.DocumentID = id
	created, err := h.service.CreateSection(payload)
	if err != nil {
		h.fail(c, err, "failed to create section")
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) UpdateSection(c *gin.Context) {
	docID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sectionID, ok := parseID(c, "sectionID")
	if !ok {
		return
	}
	var payload Section
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid section payload"})
		return
	}
	payload.DocumentID = docID
	updated, err := h.service.UpdateSection(docID, sectionID, payload)
	if err != nil {
		h.fail(c, err, "failed to update section")
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) DeleteSection(c *gin.Context) {
	docID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sectionID, ok := parseID(c, "sectionID")
	if !ok {
		return
	}
	if err := h.service.DeleteSection(docID, sectionID); err != nil {
		h.fail(c, err, "failed to delete section")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ReorderSections(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var payload struct {
		SectionIDs []int64 `json:"section_ids"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reorder payload"})
		return
	}
	sections, err := h.service.ReorderSections(id, payload.SectionIDs)
	if err != nil {
		h.fail(c, err, "failed to reorder sections")
		return
	}
	c.JSON(http.StatusOK, sections)
}

// ---- Control mapping ----

func (h *Handler) CoveragePage(c *gin.Context) { writeHTML(c, coveragePageHTML()) }

func (h *Handler) CoverageData(c *gin.Context) {
	clientID, _ := strconv.ParseInt(c.Query("client_id"), 10, 64)
	report, err := h.service.Coverage(CoverageFilter{
		Baseline:            c.Query("baseline"),
		Family:              c.Query("family"),
		ClientID:            clientID,
		IncludeEnhancements: c.Query("enhancements") == "1",
		OnlyGaps:            c.Query("gaps") == "1",
	})
	if err != nil {
		h.fail(c, err, "failed to build coverage report")
		return
	}
	c.JSON(http.StatusOK, report)
}

func (h *Handler) ControlSearch(c *gin.Context) {
	options, err := h.service.SearchControls(c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search controls"})
		return
	}
	c.JSON(http.StatusOK, options)
}

func (h *Handler) DocumentControlRefs(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	refs, err := h.service.ListControlRefsForDocument(id)
	if err != nil {
		h.fail(c, err, "failed to load control mappings")
		return
	}
	c.JSON(http.StatusOK, refs)
}

func (h *Handler) SectionControlRefs(c *gin.Context) {
	docID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sectionID, ok := parseID(c, "sectionID")
	if !ok {
		return
	}
	refs, err := h.service.ListControlRefsForSection(docID, sectionID)
	if err != nil {
		h.fail(c, err, "failed to load control mappings")
		return
	}
	c.JSON(http.StatusOK, refs)
}

func (h *Handler) AttachControl(c *gin.Context) {
	docID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sectionID, ok := parseID(c, "sectionID")
	if !ok {
		return
	}
	var payload ControlRef
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid control mapping payload"})
		return
	}
	created, err := h.service.AttachControl(docID, sectionID, payload)
	if err != nil {
		h.fail(c, err, "failed to map control")
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) DetachControl(c *gin.Context) {
	docID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sectionID, ok := parseID(c, "sectionID")
	if !ok {
		return
	}
	refID, ok := parseID(c, "refID")
	if !ok {
		return
	}
	if err := h.service.DetachControl(docID, sectionID, refID); err != nil {
		h.fail(c, err, "failed to unmap control")
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- Shared error mapping ----

func parseID(c *gin.Context, param string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// fail maps service errors onto status codes. ApprovalError gets 422 with the
// findings attached rather than a bare 400, because "these six things block
// approval" is the response the editor needs to render.
func (h *Handler) fail(c *gin.Context, err error, fallback string) {
	var approval ApprovalError
	switch {
	case errors.As(err, &approval):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":    "document is not ready for approval",
			"findings": approval.Findings,
		})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
	case IsValidation(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": fallback})
	}
}

// exportFilename builds a safe download name from the reference and title.
func exportFilename(doc Document, ext string) string {
	base := strings.TrimSpace(doc.Reference)
	if base == "" {
		base = doc.Title
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('_')
		}
	}
	name := strings.Trim(b.String(), "_-")
	if name == "" {
		name = "policy-document"
	}
	return name + "." + ext
}
