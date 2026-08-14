package knowledge

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler serves the knowledge API.
type Handler struct {
	service *Service
	token   string
}

// NewHandler builds the handler. token is the read-only service token; an
// empty token leaves the API open, which is only acceptable in local mode and
// is the caller's decision to make (see registerKnowledgeRoutes).
func NewHandler(service *Service, token string) *Handler {
	return &Handler{service: service, token: strings.TrimSpace(token)}
}

// RegisterRoutes attaches the API.
//
// Four endpoints, deliberately: this is a tool surface, and a model uses a
// small vocabulary better than a large one. They are all GETs — there is no
// write path here to secure, forget to secure, or be talked into using.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	api := r.Group("/api/knowledge")
	api.Use(h.authenticate)
	api.GET("/overview", h.Overview)
	api.GET("/index/:kind", h.Index)
	api.GET("/search", h.Search)
	api.GET("/item", h.Get)
}

// authenticate checks the read-only service token.
//
// It is deliberately not the admin token: this credential lives in another
// process's configuration, and a read-only one that leaks costs the reader
// nothing it could not already ask a signed-in user for, while a write-capable
// one would cost the catalog.
func (h *Handler) authenticate(c *gin.Context) {
	if h.token == "" {
		c.Next()
		return
	}
	supplied := strings.TrimSpace(c.GetHeader("X-Knowledge-Token"))
	if supplied == "" {
		if value, ok := strings.CutPrefix(strings.TrimSpace(c.GetHeader("Authorization")), "Bearer "); ok {
			supplied = strings.TrimSpace(value)
		}
	}
	if supplied != "" && subtle.ConstantTimeCompare([]byte(supplied), []byte(h.token)) == 1 {
		c.Next()
		return
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error": "a valid knowledge token is required (X-Knowledge-Token or Bearer)",
	})
}

func (h *Handler) Overview(c *gin.Context) {
	overview, err := h.service.Overview()
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, overview)
}

func (h *Handler) Index(c *gin.Context) {
	kind := strings.TrimSpace(c.Param("kind"))
	items, err := h.service.Index(kind)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"kind": kind, "count": len(items), "items": items})
}

func (h *Handler) Search(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	if kind == "" {
		kind = KindNFR
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	result, err := h.service.Search(kind, c.Query("q"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Get(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	if kind == "" {
		kind = KindNFR
	}
	item, err := h.service.Get(kind, c.Query("ref"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func writeError(c *gin.Context, err error) {
	var notFound ErrNotFound
	var invalid ErrInvalid
	switch {
	case errors.As(err, &notFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.As(err, &invalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
