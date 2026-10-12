package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"grc/internal/aiprovider"
	"grc/internal/authn"
	"grc/internal/clientprofile"
	"grc/internal/db"
	"grc/internal/knowledge"
	"grc/internal/policyai"
	"grc/internal/policydocs"
	"grc/internal/policystudio"
	"grc/internal/settings"
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

// localStudioUser is who local mode's single user is in the Studio's
// suggestions, comments and decision audit, which would otherwise record no
// one.
const localStudioUser = "local"

// studioIdentity resolves a collaboration request's identity from the session
// cookie. Local mode has no identities: its single user is an admin, as
// everywhere else in local mode.
func studioIdentity(authService *authn.Service, localMode bool) policystudio.IdentityFunc {
	return func(r *http.Request) (policystudio.Identity, bool) {
		if localMode || authService == nil {
			return policystudio.Identity{Username: localStudioUser, Admin: true, CanReadPolicies: true}, true
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
	aiRouter *aiprovider.Router,
	settingsService *settings.Service,
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
		LocalMode: options.LocalMode,
		GuestLinks: func() bool {
			return settingsService != nil && settingsService.Preference(settings.PrefStudioGuestLinks) == "true"
		},
	})
	knowledgeService.WithLivePolicies(service.Live)

	actor := sessionUsername
	if options.LocalMode {
		actor = func(*gin.Context) string { return localStudioUser }
	}
	handler := policystudio.NewHandler(service, actor).WithGuestPage(policystudio.GuestPage{
		Chromeless:   renderWithoutGlobalChrome,
		InlineHashes: func(c *gin.Context) ([]string, []string) { return themeInlineHashes(currentTheme(c)) },
		Secure:       requestSecure,
	})
	handler.RegisterReadRoutes(r)
	handler.RegisterPublicRoutes(r)
	handler.RegisterGuestRoutes(r)
	clientHandler := clientprofile.NewHandler(clients, actor)
	clientHandler.RegisterReadRoutes(r)

	// The proposal engine. Its questions go to the Policy Studio agent
	// Settings names, and so do the AI dock's on Studio pages.
	preference := func(key string) string {
		if settingsService == nil {
			return ""
		}
		return settingsService.Preference(key)
	}
	engine := policyai.NewEngine(policyai.Config{
		Conn: conn, Studio: service, Policies: policyService, Knowledge: knowledgeService, Router: aiRouter,
		Agent:        func() string { return preference(settings.PrefPolicyAgent) },
		SendDocument: func() bool { return preference(settings.PrefPolicySendDocument) == "true" },
	})
	identity := studioIdentity(authService, options.LocalMode)
	configurePolicyDock(engine, identity)
	aiHandler := policyai.NewHandler(engine, actor)
	aiHandler.RegisterReadRoutes(r)
	aiHandler.RegisterGuestRoutes(r, handler.GuestGate)

	admin := r.Group("/")
	if !options.LocalMode {
		admin.Use(adminMiddleware)
	}
	handler.RegisterAdminRoutes(admin)
	handler.RegisterSharingRoutes(admin)
	clientHandler.RegisterAdminRoutes(admin)
	aiHandler.RegisterAdminRoutes(admin)
	return service, nil
}

// requestSecure reports whether a request arrived over HTTPS, trusting
// X-Forwarded-Proto only under -trust-proxy, the rule the sign-in cookie
// follows.
func requestSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return trustProxyHeaders && strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}

type inlineHashes struct{ scripts, styles []string }

var (
	inlineHashMu    sync.Mutex
	inlineHashCache = map[string]inlineHashes{}
)

// themeInlineHashes are the CSP hashes of the inline script and style blocks
// the theme layer injects into a chromeless page in this theme, computed from
// the same strings it injects, so the guest page's enforced CSP cannot drift
// from them (Q10).
func themeInlineHashes(theme string) (scripts, styles []string) {
	inlineHashMu.Lock()
	defer inlineHashMu.Unlock()
	if h, ok := inlineHashCache[theme]; ok {
		return h.scripts, h.styles
	}
	head := headInsertTag(theme)
	h := inlineHashes{scripts: blockHashes(head, "script"), styles: blockHashes(head, "style")}
	inlineHashCache[theme] = h
	return h.scripts, h.styles
}

// blockHashes hashes the content of every <tag>…</tag> block in s.
func blockHashes(s, tag string) []string {
	var out []string
	for {
		open := strings.Index(s, "<"+tag)
		if open < 0 {
			return out
		}
		start := strings.Index(s[open:], ">")
		end := strings.Index(s[open:], "</"+tag+">")
		if start < 0 || end < 0 || end < start {
			return out
		}
		sum := sha256.Sum256([]byte(s[open+start+1 : open+end]))
		out = append(out, "sha256-"+base64.StdEncoding.EncodeToString(sum[:]))
		s = s[open+end+len("</"+tag+">"):]
	}
}
