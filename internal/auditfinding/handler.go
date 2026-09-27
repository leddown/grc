package auditfinding

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"grc/internal/apiutil"
)

type Handler struct {
	service *Service
	actor   func(*gin.Context) string
}

func NewHandler(service *Service, actor func(*gin.Context) string) *Handler {
	return &Handler{service: service, actor: actor}
}

func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/audit-findings", h.Page)
	r.GET("/audit-findings/data", h.List)
	r.GET("/audit-findings/data/:id", h.GetOne)
	r.GET("/audit-findings/summary", h.SummaryData)
	r.GET("/audit-findings/vocabulary", h.VocabularyData)
}

// RegisterAdminRoutes carries every write. A finding's closure is evidence an
// auditor or regulator relies on, so recording it is not left open to anyone
// who can read the register.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/audit-findings", h.Create)
	r.PUT("/audit-findings/:id", h.Update)
	r.DELETE("/audit-findings/:id", h.Delete)
}

func (h *Handler) Page(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pageHTML()))
}

func (h *Handler) List(c *gin.Context) {
	items, err := h.service.List(Filter{
		Search:      c.Query("search"),
		Status:      c.Query("status"),
		Severity:    c.Query("severity"),
		Source:      c.Query("source"),
		OverdueOnly: c.Query("overdue") == "true",
		OpenOnly:    c.Query("open") == "true",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list audit findings"})
		return
	}
	page := apiutil.ParsePagination(c)
	if page.Enabled {
		paged, total := apiutil.PaginateSlice(items, page)
		c.JSON(http.StatusOK, gin.H{"items": paged, "total": total, "page": page.Page, "per_page": page.PerPage})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) GetOne(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	f, err := h.service.Get(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, f)
}

func (h *Handler) SummaryData(c *gin.Context) {
	sum, err := h.service.Summary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to summarise audit findings"})
		return
	}
	c.JSON(http.StatusOK, sum)
}

func (h *Handler) VocabularyData(c *gin.Context) {
	c.JSON(http.StatusOK, Vocab())
}

func (h *Handler) Create(c *gin.Context) {
	var f Finding
	if err := c.ShouldBindJSON(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	created, err := h.service.Create(f, h.actor(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f Finding
	if err := c.ShouldBindJSON(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	updated, err := h.service.Update(id, f, h.actor(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(id); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func writeError(c *gin.Context, err error) {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "audit finding storage failed"})
	}
}
