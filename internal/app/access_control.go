package app

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"grc/internal/authn"
)

// sessionUserContextKey is where pageAccessMiddleware stashes the resolved
// session user so later middleware in the same request (e.g.
// adminTokenMiddleware) can reuse it instead of querying the session table
// again.
const sessionUserContextKey = "authn_session_user"

var protectedPrefixes = []string{
	"/JSON_view",
	"/json-view",
	"/jira",
	"/controls",
	"/security-nfrs",
	"/nfr-enrichment",
	"/regulation-coverage",
	"/crisis-exercises",
	"/policies",
	"/templates",
	"/reports",
	"/risk-register",
	"/asset-types",
	"/exceptions",
	"/changelog",
	"/ai-chat",
	"/wiz-rules",
	"/docs",
	"/openapi.json",
	"/utilities",
}

func pageAccessMiddleware(authService *authn.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := strings.TrimSpace(c.Request.URL.Path)
		if !isProtectedPath(path) {
			c.Next()
			return
		}
		if authService == nil {
			c.Next()
			return
		}

		sessionToken, err := c.Cookie(authn.AuthSessionCookie)
		if err != nil || strings.TrimSpace(sessionToken) == "" {
			abortUnauthenticated(c)
			return
		}
		user, err := authService.GetSessionUser(sessionToken)
		if err != nil {
			abortUnauthenticated(c)
			return
		}
		c.Set(sessionUserContextKey, user)
		if user.IsAdmin {
			c.Next()
			return
		}
		if len(user.AllowedPages) == 0 {
			c.Next()
			return
		}

		for _, page := range user.AllowedPages {
			if grantCovers(page, path) {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access denied for this page"})
	}
}

// grantCovers reports whether an allowed_pages entry authorises a request path.
//
// An exact grant covers the page *and* the endpoints hanging off it, because a
// page is not usable without them: every page in this app renders a shell and
// then fetches "<page>/data". Granting "/controls" and then refusing
// "/controls/data" produced a page that loaded and immediately showed a load
// error, which is what the previous exact-match-only rule did.
//
// The one thing an exact grant must not do is reach a *sibling* page. Sub-paths
// of "/controls" include "/controls/manage", the editor — so a plain "/controls"
// grant deliberately covers only paths whose next segment is not itself a page
// listed in the grantable set. That keeps "read the controls page" from silently
// becoming "open the control editor".
//
// The explicit "/*" suffix still means "this page and everything under it",
// including sibling pages, for callers that want the broad grant.
func grantCovers(grant, path string) bool {
	grant = strings.TrimSpace(grant)
	if grant == "" {
		return false
	}
	if grant == path {
		return true
	}
	if strings.HasSuffix(grant, "/*") {
		// The base path is included. "/controls/*" meaning "everything under
		// /controls except /controls itself" is the same bug mirrored, and it
		// is not what anyone writing that grant intends.
		base := strings.TrimSuffix(grant, "/*")
		return path == base || strings.HasPrefix(path, base+"/")
	}
	if !strings.HasPrefix(path, grant+"/") {
		return false
	}
	// path is under grant. Allow it unless it is itself a separately grantable
	// page, which would make this a sibling rather than a supporting endpoint.
	return !isSeparatelyGrantablePage(path)
}

// isSeparatelyGrantablePage reports whether path is a page that appears in the
// grantable list in its own right. Those have to be granted explicitly.
func isSeparatelyGrantablePage(path string) bool {
	for _, page := range userManagementAllowedPages {
		if page == path {
			return true
		}
	}
	return false
}

// sessionUsername returns the signed-in username, or "" when the request is
// unauthenticated (local mode, or a public route).
//
// It reads the user pageAccessMiddleware already resolved and stashed, so it
// costs no extra session lookup. Passed to modules that need to attribute an
// action to a person — policy authorship and approval — without those modules
// having to know how sessions work.
func sessionUsername(c *gin.Context) string {
	value, ok := c.Get(sessionUserContextKey)
	if !ok {
		return ""
	}
	user, ok := value.(authn.User)
	if !ok {
		return ""
	}
	return strings.TrimSpace(user.Username)
}

func isProtectedPath(path string) bool {
	for _, prefix := range protectedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// abortUnauthenticated redirects browser page navigations to the login page
// and returns a JSON 401 for everything else (API/fetch calls), so existing
// API consumers keep their existing contract.
func abortUnauthenticated(c *gin.Context) {
	if isBrowserPageRequest(c) {
		redirectToLogin(c)
		return
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
}

func isBrowserPageRequest(c *gin.Context) bool {
	return c.Request.Method == http.MethodGet && strings.Contains(c.GetHeader("Accept"), "text/html")
}

func redirectToLogin(c *gin.Context) {
	target := "/login?next=" + url.QueryEscape(c.Request.URL.RequestURI())
	c.Redirect(http.StatusFound, target)
	c.Abort()
}
