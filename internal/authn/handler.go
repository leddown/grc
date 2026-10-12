package authn

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const AuthSessionCookie = "grc_auth_session"

type Handler struct {
	service           *Service
	trustProxyHeaders bool
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// SetTrustProxyHeaders controls whether X-Forwarded-Proto is honored when
// deciding if the session cookie should be marked Secure. Only enable this
// when the app sits behind a reverse proxy that sets/overwrites the header,
// since it is otherwise spoofable by any client.
func (h *Handler) SetTrustProxyHeaders(trust bool) {
	h.trustProxyHeaders = trust
}

type bootstrapRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) BootstrapAdmin(c *gin.Context) {
	expectedSetupToken := strings.TrimSpace(os.Getenv("SETUP_TOKEN"))
	if expectedSetupToken != "" {
		presented := strings.TrimSpace(c.GetHeader("X-Setup-Token"))
		if presented == "" || !constantTimeEquals(presented, expectedSetupToken) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "valid setup token is required"})
			return
		}
	}

	var req bootstrapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	user, err := h.service.BootstrapAdmin(req.Username, req.Password)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrAlreadyBootstrapped):
			status = http.StatusConflict
		case errors.Is(err, ErrInvalidInput):
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, user)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	user, err := h.service.Authenticate(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, ErrTooManyAttempts) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many failed login attempts, please try again later"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	session, err := h.service.CreateSession(user.ID, 24*time.Hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AuthSessionCookie, session.Token, int((24 * time.Hour).Seconds()), "/", "", h.requestIsHTTPS(c), true)
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) Logout(c *gin.Context) {
	token, err := c.Cookie(AuthSessionCookie)
	if err == nil && strings.TrimSpace(token) != "" {
		_ = h.service.DeleteSession(token)
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AuthSessionCookie, "", -1, "/", "", h.requestIsHTTPS(c), true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) Me(c *gin.Context) {
	token, err := c.Cookie(AuthSessionCookie)
	if err != nil || strings.TrimSpace(token) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	user, err := h.service.GetSessionUser(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	c.JSON(http.StatusOK, user)
}

type createUserRequest struct {
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	IsAdmin      bool     `json:"is_admin"`
	AllowedPages []string `json:"allowed_pages"`
}

func (h *Handler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	user, err := h.service.CreateUserWithAccess(req.Username, req.Password, req.IsAdmin, req.AllowedPages)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrInvalidInput):
			status = http.StatusBadRequest
		case errors.Is(err, ErrSingleUserMode):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, user)
}

type updateUserRequest struct {
	Password     *string   `json:"password"`
	IsAdmin      *bool     `json:"is_admin"`
	AllowedPages *[]string `json:"allowed_pages"`
}

func (h *Handler) UpdateUser(c *gin.Context) {
	username := c.Param("username")
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	user, err := h.service.UpdateUser(username, req.Password, req.IsAdmin, req.AllowedPages)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrInvalidInput):
			status = http.StatusBadRequest
		case errors.Is(err, ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrLastAdmin):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, user)
}

func (h *Handler) DeleteUser(c *gin.Context) {
	if err := h.service.DeleteUser(c.Param("username")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, ErrLastAdmin) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListUsers(c *gin.Context) {
	users, err := h.service.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list auth users"})
		return
	}
	c.JSON(http.StatusOK, users)
}

func constantTimeEquals(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (h *Handler) requestIsHTTPS(c *gin.Context) bool {
	if c.Request != nil && c.Request.TLS != nil {
		return true
	}
	if !h.trustProxyHeaders {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}
