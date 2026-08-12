package app

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"carelockconsulting/internal/authn"
	"github.com/gin-gonic/gin"
)

// adminTokenMiddleware gates a route to admins only. It distinguishes two
// failure cases that look identical over HTTP but are not: no valid session
// at all (send to login), versus a valid session for a non-admin user (send
// to an explanation, not back through login). Collapsing them into one
// "redirect to /login" used to mean a signed-in non-admin hitting an
// admin-only page like /utilities saw exactly what a logged-out visitor
// sees, which reads as "I got signed out" rather than "this page needs an
// admin account."
func adminTokenMiddleware(authService *authn.Service, adminToken string) gin.HandlerFunc {
	adminToken = strings.TrimSpace(adminToken)
	return func(c *gin.Context) {
		authenticated := false
		if cached, ok := c.Get(sessionUserContextKey); ok {
			if user, ok := cached.(authn.User); ok {
				authenticated = true
				if user.IsAdmin {
					c.Next()
					return
				}
			}
		} else if authService != nil {
			if sessionToken, err := c.Cookie(authn.AuthSessionCookie); err == nil && strings.TrimSpace(sessionToken) != "" {
				if user, err := authService.GetSessionUser(sessionToken); err == nil {
					authenticated = true
					if user.IsAdmin {
						c.Next()
						return
					}
				}
			}
		}

		if adminToken != "" {
			token := strings.TrimSpace(c.GetHeader("X-Admin-Token"))
			if token == "" {
				authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
				if value, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
					token = strings.TrimSpace(value)
				}
			}
			if token != "" && constantTimeEquals(token, adminToken) {
				c.Next()
				return
			}
		}

		if isBrowserPageRequest(c) {
			if authenticated {
				adminRequiredPage(c)
				return
			}
			redirectToLogin(c)
			return
		}
		if authenticated {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "admin authentication required"})
	}
}

// adminRequiredPage tells a signed-in, non-admin user why the page they
// navigated to did not load, instead of silently sending them back through
// /login as if their session had expired.
func adminRequiredPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Admin Access Required · CareLock Consulting</title>
  <style>
    body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center; font-family: Arial, sans-serif; background:#0b1220; color:#e5e7eb; }
    main { width:100%; max-width:420px; padding:28px; background:#111827; border:1px solid #334155; border-radius:16px; }
    h1 { margin:0 0 6px; font-size:20px; }
    p { margin:0 0 16px; color:#94a3b8; font-size:14px; line-height:1.5; }
    .actions { display:flex; gap:10px; flex-wrap:wrap; }
    a.btn, button { padding:10px 14px; border-radius:10px; border:1px solid #334155; background:#1f2937; color:#e2e8f0; font:inherit; text-decoration:none; cursor:pointer; display:inline-block; }
    a.btn:hover, button:hover { background:#2563eb; border-color:#2563eb; }
  </style>
</head>
<body>
  <main>
    <h1>Admin Access Required</h1>
    <p>You are signed in, but this page is restricted to admin accounts. Ask an administrator to grant your account admin access, or sign in with an admin account.</p>
    <div class="actions">
      <a class="btn" href="/">Back to Home</a>
      <button id="switchAccountBtn" type="button">Sign In as Someone Else</button>
    </div>
  </main>
  <script>
    document.getElementById('switchAccountBtn').addEventListener('click', async () => {
      await fetch('/auth/logout', { method: 'POST' });
      window.location.href = '/login';
    });
  </script>
</body>
</html>`
	c.Data(http.StatusForbidden, "text/html; charset=utf-8", []byte(html))
	c.Abort()
}

// noAuthMiddleware allows every request through; used in local mode where
// there is no concept of users or admin sessions.
func noAuthMiddleware(c *gin.Context) {
	c.Next()
}

func constantTimeEquals(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
