package policystudio

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"grc/internal/policydocs"
)

// maxTemplateBytes bounds a template body. The largest built-in template is
// about 30 KB; a template ten times that is not a template.
const maxTemplateBytes = 512 << 10

// registerAppTemplateRoutes attaches the templates page and its API. All of it
// is administration: a published template is text every new document starts
// from.
func (h *Handler) registerAppTemplateRoutes(r gin.IRouter) {
	r.GET("/policies/templates/manage", h.TemplatesPage)
	r.GET("/policies/app-templates", h.ListAppTemplates)
	r.GET("/policies/app-templates/meta", h.TemplateMeta)
	r.POST("/policies/app-templates/import", h.ImportAppTemplate)
	r.POST("/policies/app-templates/copy", h.CopyAppTemplate)
	r.GET("/policies/app-templates/:tid", h.GetAppTemplate)
	r.GET("/policies/app-templates/:tid/template.json", h.ExportAppTemplate)
	r.PUT("/policies/app-templates/:tid", h.SaveAppTemplate)
	r.POST("/policies/app-templates/:tid/publish", h.PublishAppTemplate)
	r.POST("/policies/app-templates/:tid/retire", h.RetireAppTemplate)
	r.POST("/policies/app-templates/:tid/restore", h.RestoreAppTemplate)
	r.DELETE("/policies/app-templates/:tid", h.DeleteAppTemplate)
}

func appTemplateID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("tid"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid template id"})
		return 0, false
	}
	return id, true
}

func (h *Handler) ListAppTemplates(c *gin.Context) {
	list, err := h.service.AppTemplates()
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// TemplateMeta is the vocabulary the template editor offers.
func (h *Handler) TemplateMeta(c *gin.Context) {
	type kind struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}
	kinds := []kind{}
	for _, k := range policydocs.SectionKinds {
		kinds = append(kinds, kind{Value: k, Label: policydocs.SectionKindLabels[k]})
	}
	required := map[string][]string{}
	for _, t := range policydocs.DocTypes {
		required[t] = policydocs.RequiredKinds(t)
	}
	c.JSON(http.StatusOK, gin.H{
		"doc_types":       policydocs.DocTypes,
		"frameworks":      policydocs.Frameworks,
		"classifications": policydocs.Classifications,
		"section_kinds":   kinds,
		"required_kinds":  required,
		"value_types":     []string{"text", "number", "date", "duration", "list"},
		"coverages":       []string{policydocs.CoveragePartial, policydocs.CoverageSupporting},
	})
}

func (h *Handler) GetAppTemplate(c *gin.Context) {
	id, ok := appTemplateID(c)
	if !ok {
		return
	}
	t, err := h.service.AppTemplate(id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// ExportAppTemplate downloads the working copy in the built-in files' format,
// to import elsewhere or to commit as a built-in template.
func (h *Handler) ExportAppTemplate(c *gin.Context) {
	id, ok := appTemplateID(c)
	if !ok {
		return
	}
	t, err := h.service.AppTemplate(id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+t.TemplateID+`.json"`)
	c.IndentedJSON(http.StatusOK, t.Draft)
}

func (h *Handler) ImportAppTemplate(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxTemplateBytes))
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "the template is too large"})
		return
	}
	t, err := h.service.ImportTemplate(raw, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, t)
}

func (h *Handler) CopyAppTemplate(c *gin.Context) {
	var in struct {
		TemplateID string `json:"template_id"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	t, err := h.service.CopyTemplate(in.TemplateID, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, t)
}

func (h *Handler) SaveAppTemplate(c *gin.Context) {
	id, ok := appTemplateID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTemplateBytes)
	var t Template
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid template"})
		return
	}
	saved, err := h.service.SaveAppTemplate(id, t, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) templateAction(c *gin.Context, act func(int64, string) (AppTemplate, error)) {
	id, ok := appTemplateID(c)
	if !ok {
		return
	}
	t, err := act(id, h.actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *Handler) PublishAppTemplate(c *gin.Context) {
	h.templateAction(c, h.service.PublishAppTemplate)
}

func (h *Handler) RetireAppTemplate(c *gin.Context) { h.templateAction(c, h.service.RetireAppTemplate) }

func (h *Handler) RestoreAppTemplate(c *gin.Context) {
	h.templateAction(c, h.service.RestoreAppTemplate)
}

func (h *Handler) DeleteAppTemplate(c *gin.Context) {
	id, ok := appTemplateID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteAppTemplate(id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
