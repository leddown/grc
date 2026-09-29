package policystudio

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/policydocs"
)

// The guest side lives under /shared: the join page, the chromeless guest
// Studio and a JSON API scoped to the one document a guest session is for.
// None of it is behind the page gate, by decision (Q1); every route passes
// through guestGate or checks the link itself, and the document always comes
// from the session, never from the request.

// GuestPage lets the application give the guest pages what only it knows:
// how to render without its chrome, and the CSP hashes of the inline script
// and style blocks its theme layer will inject (Q10), for the enforced CSP.
type GuestPage struct {
	Chromeless   func(c *gin.Context)
	InlineHashes func(c *gin.Context) (scripts, styles []string)
	// Secure reports whether the request arrived over HTTPS, under the same
	// proxy rules as the sign-in cookie.
	Secure func(c *gin.Context) bool
}

// WithGuestPage configures the guest pages.
func (h *Handler) WithGuestPage(p GuestPage) *Handler {
	h.guest = p
	return h
}

// RegisterGuestRoutes attaches /shared. They are public routes with their own
// gate.
func (h *Handler) RegisterGuestRoutes(r gin.IRouter) {
	shared := r.Group("/shared", h.sharedHeaders)
	shared.GET("/p/:token", h.JoinPage)
	shared.POST("/p/:token", h.Join)
	shared.GET("/studio", h.GuestStudio)
	api := shared.Group("/api", h.GuestGate)
	api.GET("/state", h.GuestState)
	api.POST("/leave", h.GuestLeave)
	api.GET("/comments", h.GuestThreads)
	api.POST("/comments", h.GuestCreateThread)
	api.POST("/comments/:threadID/replies", h.GuestReply)
}

// RegisterSharingRoutes attaches the administrator's side: links and guests.
func (h *Handler) RegisterSharingRoutes(r gin.IRouter) {
	r.GET("/policies/:id/share-links", h.ListSharing)
	r.POST("/policies/:id/share-links", h.CreateShareLink)
	r.DELETE("/policies/:id/share-links/:linkID", h.RevokeShareLink)
	r.DELETE("/policies/:id/guests/:sessionID", h.KickGuest)
}

// sharedHeaders applies to every /shared response: a token in a URL must
// never leak through a Referer, and nothing here is cacheable.
func (h *Handler) sharedHeaders(c *gin.Context) {
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Next()
}

const guestContextKey = "grc_policy_guest"

// GuestGate admits a request only with a valid guest session, and records it
// for the handler. It is the one decision for every /shared/api route.
func (h *Handler) GuestGate(c *gin.Context) {
	g, ok := h.service.Guest(c.Request)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Your access to this document has ended. Ask for a new link."})
		return
	}
	c.Set(guestContextKey, g)
	c.Next()
}

// GuestFrom returns the session GuestGate admitted.
func GuestFrom(c *gin.Context) (GuestSession, bool) {
	v, ok := c.Get(guestContextKey)
	if !ok {
		return GuestSession{}, false
	}
	g, ok := v.(GuestSession)
	return g, ok
}

func (h *Handler) guestFail(c *gin.Context, err error) {
	var ga ErrGuestAccess
	if errors.As(err, &ga) {
		c.JSON(ga.Status, gin.H{"error": ga.Msg})
		return
	}
	h.fail(c, err)
}

func nonceValue() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.StdEncoding.EncodeToString(b[:])
}

// guestCSP is enforced, not report-only. Scripts come only from this origin
// (the embedded bundle, carrying the page's nonce) or are the theme layer's
// constant inline blocks, by hash; style elements likewise. Style attributes
// are allowed: the editor sets caret colours and decorations with them, and a
// style attribute cannot run script.
func (h *Handler) guestCSP(c *gin.Context, nonce string) string {
	scripts := []string{"'self'", "'nonce-" + nonce + "'"}
	styles := []string{"'self'", "'nonce-" + nonce + "'"}
	if h.guest.InlineHashes != nil {
		sh, st := h.guest.InlineHashes(c)
		for _, hash := range sh {
			scripts = append(scripts, "'"+hash+"'")
		}
		for _, hash := range st {
			styles = append(styles, "'"+hash+"'")
		}
	}
	scheme := "ws"
	if h.guest.Secure != nil && h.guest.Secure(c) {
		scheme = "wss"
	}
	return "default-src 'self'; script-src " + strings.Join(scripts, " ") + "; style-src " + strings.Join(styles, " ") +
		"; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' " + scheme + "://" + c.Request.Host +
		"; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; form-action 'self'"
}

func (h *Handler) guestHTML(c *gin.Context, status int, title, body string) {
	m, err := LoadManifest()
	if err != nil {
		c.String(http.StatusServiceUnavailable, "The Policy Studio editor bundle is missing from this build.")
		return
	}
	if h.guest.Chromeless != nil {
		h.guest.Chromeless(c)
	}
	nonce := nonceValue()
	c.Header("Content-Security-Policy", h.guestCSP(c, nonce))
	page := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>` + html.EscapeString(title) + `</title>
<link rel="stylesheet" href="/assets/policy-studio/` + html.EscapeString(m.Entry.CSS) + `">
` + strings.ReplaceAll(body, "{{nonce}}", nonce) + `
</html>`
	page = strings.ReplaceAll(page, "{{js}}", "/assets/policy-studio/"+html.EscapeString(m.Entry.JS))
	c.Data(status, "text/html; charset=utf-8", []byte(page))
}

func (h *Handler) guestMessage(c *gin.Context, status int, heading, msg string) {
	h.guestHTML(c, status, heading, `</head>
<body class="ps-guest-body">
<main class="ps-guest-join">
<h1>`+html.EscapeString(heading)+`</h1>
<p>`+html.EscapeString(msg)+`</p>
</main>
</body>`)
}

// JoinPage asks the person who opened a link for their name.
func (h *Handler) JoinPage(c *gin.Context) {
	if err := h.service.guestLinksAvailable(); err != nil {
		h.guestMessage(c, http.StatusForbidden, "Sharing is switched off", err.Error())
		return
	}
	link, doc, err := h.service.LinkForToken(c.Param("token"))
	if err == nil {
		err = linkRefusal(link.Status)
	}
	var ga ErrGuestAccess
	if errors.As(err, &ga) {
		h.guestMessage(c, ga.Status, "This link can't be used", ga.Msg)
		return
	}
	if err != nil {
		h.fail(c, err)
		return
	}
	roles := map[string]string{RoleViewer: "read it", RoleCommenter: "read and comment on it", RoleEditor: "read, comment on and edit it"}
	h.guestHTML(c, http.StatusOK, doc.Title, `</head>
<body class="ps-guest-body">
<main class="ps-guest-join">
<p class="ps-muted">You have been invited to</p>
<h1>`+html.EscapeString(doc.Title)+`</h1>
<p>You can `+roles[link.Role]+` with the people working on it, until `+html.EscapeString(humanTime(h.service.parseStamp(link.ExpiresAt)))+`.
Everyone in the document sees the name you give.</p>
<form method="post" class="ps-form">
<label for="guest-name">Your name <input id="guest-name" name="name" required maxlength="60" autocomplete="name"></label>
<button type="submit" class="ps-primary">Open the document</button>
</form>
</main>
</body>`)
}

func humanTime(t time.Time) string {
	if t.IsZero() {
		return "it expires"
	}
	return t.UTC().Format("2 January 2006, 15:04 UTC")
}

// Join redeems a link and sets the guest cookie.
func (h *Handler) Join(c *gin.Context) {
	g, token, err := h.service.Redeem(c.Param("token"), c.PostForm("name"))
	if err != nil {
		var ga ErrGuestAccess
		if errors.As(err, &ga) {
			h.guestMessage(c, ga.Status, "This link can't be used", ga.Msg)
			return
		}
		if policydocs.IsValidation(err) {
			h.guestMessage(c, http.StatusBadRequest, "Please try again", err.Error()+". Go back and enter your name.")
			return
		}
		h.fail(c, err)
		return
	}
	// Secure follows the sign-in cookie's rule (TLS, or X-Forwarded-Proto
	// under -trust-proxy), so the cookie still works on a plain-HTTP laptop
	// install and is never sent in the clear behind TLS.
	secure := h.guest.Secure != nil && h.guest.Secure(c)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(GuestCookie, token, int(time.Until(h.service.parseStamp(g.ExpiresAt)).Seconds()), "/", "", secure, true)
	c.Redirect(http.StatusSeeOther, "/shared/studio")
}

// GuestStudio is the chromeless Studio for a guest session.
func (h *Handler) GuestStudio(c *gin.Context) {
	g, ok := h.service.Guest(c.Request)
	if !ok {
		h.guestMessage(c, http.StatusUnauthorized, "Your access has ended", "This invitation has expired, been withdrawn, or was never opened in this browser. Ask the person who sent it for a new link.")
		return
	}
	doc, err := h.service.policies.GetDocument(g.DocumentID)
	if err != nil {
		h.guestMessage(c, http.StatusGone, "This document is no longer shared", "Ask the person who sent the link.")
		return
	}
	attr := html.EscapeString
	allow := "false"
	if g.AllowAI {
		allow = "true"
	}
	h.guestHTML(c, http.StatusOK, doc.Title+" · shared", `<script src="{{js}}" defer nonce="{{nonce}}"></script>
</head>
<body class="ps-guest-body">
<main>
<div id="policy-studio" class="ps-app ps-guest"
  data-document-id="`+strconv.FormatInt(doc.ID, 10)+`"
  data-user="`+attr(g.DisplayName)+`"
  data-guest="true"
  data-role="`+attr(g.Role)+`"
  data-allow-ai="`+allow+`"
  data-can-manage="false">
  <noscript>This document needs JavaScript.</noscript>
  <p class="ps-loading" role="status">Opening `+attr(doc.Title)+`…</p>
</div>
</main>
</body>`)
}

// GuestState is State reduced to what a guest may see: the document's text
// structure, the facts the text uses, and what the guest may do -- no lint, no
// control mappings, no template guidance, no internal review activity.
func (h *Handler) GuestState(c *gin.Context) {
	g, _ := GuestFrom(c)
	st, err := h.service.GuestState(g)
	if err != nil {
		h.guestFail(c, err)
		return
	}
	raw, _ := json.Marshal(st)
	sum := sha256.Sum256(raw)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	c.Header("ETag", etag)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}

func (h *Handler) GuestLeave(c *gin.Context) {
	g, _ := GuestFrom(c)
	if err := h.service.Leave(g); err != nil {
		h.guestFail(c, err)
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(GuestCookie, "", -1, "/", "", h.guest.Secure != nil && h.guest.Secure(c), true)
	c.JSON(http.StatusOK, gin.H{"left": true})
}

// GuestThreads returns the shared threads only; the filter is in the query.
func (h *Handler) GuestThreads(c *gin.Context) {
	g, _ := GuestFrom(c)
	threads, err := h.service.Threads(g.DocumentID, AudienceShared)
	if err != nil {
		h.guestFail(c, err)
		return
	}
	c.JSON(http.StatusOK, threads)
}

func (h *Handler) GuestCreateThread(c *gin.Context) {
	g, _ := GuestFrom(c)
	if !g.CanComment() {
		c.JSON(http.StatusForbidden, gin.H{"error": "This invitation lets you read, not comment."})
		return
	}
	var in NewThread
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	// A guest's thread is always shared: there is no internal for a guest.
	in.Visibility = VisibilityShared
	t, err := h.service.createThread(g.DocumentID, in, ThreadComment, "", g.Actor(), g.Actor(), AuthorGuest)
	if err != nil {
		h.guestFail(c, err)
		return
	}
	c.JSON(http.StatusCreated, t)
}

func (h *Handler) GuestReply(c *gin.Context) {
	g, _ := GuestFrom(c)
	if !g.CanComment() {
		c.JSON(http.StatusForbidden, gin.H{"error": "This invitation lets you read, not comment."})
		return
	}
	tid, ok := threadID(c)
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	t, err := h.service.reply(g.DocumentID, tid, in.Body, g.Actor(), AuthorGuest, true)
	if err != nil {
		h.guestFail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// ---- the administrator's side ----

func (h *Handler) ListSharing(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	if _, err := h.service.policies.GetDocument(id); err != nil {
		h.fail(c, err)
		return
	}
	out, err := h.service.Sharing(id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

func (h *Handler) CreateShareLink(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	var in NewShareLink
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	link, token, err := h.service.CreateShareLink(id, in, h.actor(c))
	if err != nil {
		h.guestFail(c, err)
		return
	}
	// The token is in this response and nowhere else, ever.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"link": link, "path": "/shared/p/" + token})
}

func (h *Handler) RevokeShareLink(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	lid, err := strconv.ParseInt(c.Param("linkID"), 10, 64)
	if err != nil || lid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link id"})
		return
	}
	if err := h.service.RevokeShareLink(id, lid, h.actor(c)); err != nil {
		h.guestFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": lid})
}

func (h *Handler) KickGuest(c *gin.Context) {
	id, ok := documentID(c)
	if !ok {
		return
	}
	sid, err := strconv.ParseInt(c.Param("sessionID"), 10, 64)
	if err != nil || sid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid guest id"})
		return
	}
	if err := h.service.KickGuest(id, sid, h.actor(c)); err != nil {
		h.guestFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": sid})
}
