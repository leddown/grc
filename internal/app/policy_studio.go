package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/authn"
	"grc/internal/clientprofile"
	"grc/internal/db"
	"grc/internal/knowledge"
	"grc/internal/policydocs"
	"grc/internal/policystudio"
)

// studioPagePath stands for every Studio page when deciding whether a
// session's page grants reach it, so the collaboration socket admits exactly
// who pageAccessMiddleware admits to the page.
const studioPagePath = "/policies/0/studio"

// parseStudioOrigins validates -studio-allowed-origins. A malformed entry
// stops startup: an allowlist entry that silently matches nothing leaves the
// external hostname unable to connect, and one with a path or wildcard would
// not mean what the operator thinks.
func parseStudioOrigins(raw string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimRight(strings.TrimSpace(part), "/")
		if part == "" {
			continue
		}
		u, err := url.Parse(part)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "" || strings.Contains(part, "*") {
			return nil, fmt.Errorf("-studio-allowed-origins: %q is not an origin (scheme://host[:port])", part)
		}
		out = append(out, u.Scheme+"://"+strings.ToLower(u.Host))
	}
	return out, nil
}

// studioIdentity resolves a collaboration request's identity from the session
// cookie. Local mode has no identities: its single user is an admin, as
// everywhere else in local mode.
func studioIdentity(authService *authn.Service, localMode bool) policystudio.IdentityFunc {
	return func(r *http.Request) (policystudio.Identity, bool) {
		if localMode || authService == nil {
			return policystudio.Identity{Admin: true, CanReadPolicies: true}, true
		}
		cookie, err := r.Cookie(authn.AuthSessionCookie)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			return policystudio.Identity{}, false
		}
		user, err := authService.GetSessionUser(cookie.Value)
		if err != nil {
			return policystudio.Identity{}, false
		}
		canRead := user.IsAdmin || len(user.AllowedPages) == 0
		for _, grant := range user.AllowedPages {
			if grantCovers(grant, studioPagePath) {
				canRead = true
			}
		}
		return policystudio.Identity{Username: strings.TrimSpace(user.Username), Admin: user.IsAdmin, CanReadPolicies: canRead}, true
	}
}

// registerPolicyStudioRoutes wires the Policy Studio. Reads sit under
// /policies with the module's read-open / write-admin split; the
// collaboration socket and the static bundle are outside the page gate by
// design -- the socket has its own authorization decision
// (policystudio.Service.decide) and the bundle holds no data.
func registerPolicyStudioRoutes(
	r gin.IRouter,
	conn *db.Conn,
	policyService *policydocs.Service,
	authService *authn.Service,
	knowledgeService *knowledge.Service,
	adminMiddleware gin.HandlerFunc,
	options Options,
) (*policystudio.Service, error) {
	origins, err := parseStudioOrigins(options.StudioAllowedOrigins)
	if err != nil {
		return nil, err
	}
	clients := clientprofile.NewService(conn)
	service := policystudio.NewService(conn, policyService, policystudio.Options{
		Clients:        clients,
		AllowedOrigins: origins,
		SnapshotRetain: options.StudioSnapshotRetention,
		Identity:       studioIdentity(authService, options.LocalMode),
		OnProjected: func(int64) {
			knowledgeService.Invalidate(knowledge.KindPolicy, knowledge.KindPolicyClause)
		},
	})
	knowledgeService.WithLivePolicies(service.Live)

	handler := policystudio.NewHandler(service, sessionUsername)
	handler.RegisterReadRoutes(r)
	handler.RegisterPublicRoutes(r)
	clientHandler := clientprofile.NewHandler(clients, sessionUsername)
	clientHandler.RegisterReadRoutes(r)
	admin := r.Group("/")
	if !options.LocalMode {
		admin.Use(adminMiddleware)
	}
	handler.RegisterAdminRoutes(admin)
	clientHandler.RegisterAdminRoutes(admin)
	return service, nil
}
