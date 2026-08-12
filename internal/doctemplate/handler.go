package doctemplate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// IdentityFunc resolves the acting user from a request, returning "" when there
// is none (local mode, or an unauthenticated route). A function rather than a
// dependency on internal/authn, matching internal/policydocs: this module has
// no other reason to know how sessions work.
type IdentityFunc func(c *gin.Context) string

type Handler struct {
	service  *Service
	identity IdentityFunc
}

func NewHandler(service *Service, identity IdentityFunc) *Handler {
	return &Handler{service: service, identity: identity}
}

func (h *Handler) actingUser(c *gin.Context) string {
	if h.identity == nil {
		return ""
	}
	return h.identity(c)
}

// RegisterReadRoutes attaches the pages and the render endpoints.
//
// Rendering is a read route. It produces no state change, and the payload is a
// document the caller can already fetch through /policies/:id/export.json — so
// gating it behind admin would only mean someone who may read a policy cannot
// get it as a PDF. What it does spend is CPU, which is bounded by RenderTimeout
// and by the workspace being torn down after every run.
func (h *Handler) RegisterReadRoutes(r gin.IRouter) {
	r.GET("/templates", h.GalleryPage)
	r.GET("/templates/manage", h.BrandPage)
	r.GET("/templates/data", h.CatalogData)
	r.GET("/templates/brand", h.BrandData)
	r.GET("/templates/render", h.RenderEndpoint)
}

// RegisterAdminRoutes attaches the mutating endpoints.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.PUT("/templates/brand", h.SaveBrand)
	r.POST("/templates/brand/reset", h.ResetBrand)
	r.POST("/templates/engines/refresh", h.RefreshEngines)
}

// ---- Pages ----

func writeHTML(c *gin.Context, body string) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(body))
}

func (h *Handler) GalleryPage(c *gin.Context) { writeHTML(c, galleryPageHTML()) }
func (h *Handler) BrandPage(c *gin.Context)   { writeHTML(c, brandPageHTML()) }

// ---- JSON ----

func (h *Handler) CatalogData(c *gin.Context) {
	catalog, err := h.service.Catalog()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

func (h *Handler) BrandData(c *gin.Context) {
	brand, err := h.service.Brand()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"brand":    brand,
		"defaults": DefaultBrand(),
		"papers":   Papers,
	})
}

func (h *Handler) SaveBrand(c *gin.Context) {
	var brand Brand
	if err := c.ShouldBindJSON(&brand); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid brand settings: %v", err)})
		return
	}
	saved, err := h.service.SaveBrand(brand, h.actingUser(c))
	if err != nil {
		// A validation failure is the user's to fix and has a message written
		// for them; a storage failure is not. Both are reported here, and the
		// status separates them.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"brand": saved})
}

func (h *Handler) ResetBrand(c *gin.Context) {
	brand, err := h.service.ResetBrand()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"brand": brand})
}

func (h *Handler) RefreshEngines(c *gin.Context) {
	statuses := RefreshEngines()
	engines := make([]EngineStatus, 0, len(statuses))
	for _, engine := range []Engine{EngineTypst, EngineLaTeX} {
		engines = append(engines, statuses[engine])
	}
	c.JSON(http.StatusOK, gin.H{"engines": engines})
}

// ---- Render ----

// RenderEndpoint serves a PDF, or the source bundle when the host has no
// engine. Query parameters:
//
//	template   registry id; defaults to the first template matching the payload
//	doc        policy document id to render
//	sample     "1" to render the template's bundled sample instead
//	standard   a PDFStandards value; defaults to a-2b
//	bundle     "1" to force the source zip even when an engine is present
//	inline     "1" to display in the browser rather than download
func (h *Handler) RenderEndpoint(c *gin.Context) {
	templateID := strings.TrimSpace(c.Query("template"))
	standard := c.DefaultQuery("standard", PDFStandards[0].Value)
	bundle := c.Query("bundle") == "1"

	var (
		result Result
		err    error
	)
	switch {
	case c.Query("sample") == "1":
		if templateID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "a template is required to render the sample"})
			return
		}
		result, err = h.service.RenderSample(c.Request.Context(), templateID, standard, bundle)
	default:
		documentID, parseErr := strconv.ParseInt(strings.TrimSpace(c.Query("doc")), 10, 64)
		if parseErr != nil || documentID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "a document id (doc) or sample=1 is required"})
			return
		}
		result, err = h.service.RenderDocument(c.Request.Context(), documentID, templateID, standard, bundle)
	}
	if err != nil {
		// The compiler log is the whole diagnostic value of a failed render —
		// a Typst error names the file and line — so it is returned rather than
		// only logged where the person who triggered it cannot see it.
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "log": result.Log})
		return
	}

	disposition := "attachment"
	if c.Query("inline") == "1" && !result.Bundled {
		disposition = "inline"
	}
	c.Header("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, result.Filename))
	// Surfaced as headers so the page can report what happened without a second
	// request — the body is a PDF or a zip and has nowhere to carry it.
	if result.Bundled {
		c.Header("X-GRC-Bundled", "1")
	}
	if result.Reason != "" {
		c.Header("X-GRC-Reason", headerSafe(result.Reason))
	}
	if result.Log != "" {
		c.Header("X-GRC-Render-Log", headerSafe(result.Log))
	}
	c.Data(http.StatusOK, result.ContentType, result.Body)
}

// headerSafe flattens a message for an HTTP header: newlines and control
// characters become spaces, and the result is truncated. A raw compiler log in
// a header would otherwise be a response-splitting vector.
func headerSafe(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			sb.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			continue
		default:
			sb.WriteRune(r)
		}
		if sb.Len() > 900 {
			sb.WriteString(" …")
			break
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// PolicyPayloadSource adapts a function that returns a policy TemplateExport
// into a PayloadSource. internal/app uses it to wire policydocs in without this
// package importing it.
func PolicyPayloadSource(export func(id int64) (payload any, baseName string, err error)) PayloadSource {
	return func(id int64) ([]byte, string, Kind, error) {
		payload, baseName, err := export(id)
		if err != nil {
			return nil, "", "", err
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, "", "", fmt.Errorf("encoding render payload: %w", err)
		}
		return data, baseName, KindPolicy, nil
	}
}
