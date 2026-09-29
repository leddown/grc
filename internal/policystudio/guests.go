package policystudio

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"grc/internal/policydocs"
)

// Guest access: someone outside the installation -- the client's CISO in a
// workshop -- let into one document with one role, by a link an administrator
// created. POLICY_STUDIO.md ("Guests") is the operator guide.

// Guest roles. A suggester role is not offered: ygo has no per-connection hook
// that sees an update before it is applied, so the server could not hold a
// guest to suggestions, and a role the server cannot enforce is not a role.
const (
	RoleViewer    = "viewer"
	RoleCommenter = "commenter"
	RoleEditor    = "editor"
	AuthorGuest   = "guest"
)

var guestRoles = map[string]bool{RoleViewer: true, RoleCommenter: true, RoleEditor: true}

// GuestCookie holds a guest session's token. It is SameSite=Strict and HttpOnly,
// and scoped to the whole host so it reaches /collab/ as well as /shared/.
const GuestCookie = "grc_guest"

// Limits on links.
const (
	defaultInviteHours = 8
	maxInviteHours     = 7 * 24
	defaultMaxUses     = 20
	maxMaxUses         = 500
	maxLabelRunes      = 120
	maxNameRunes       = 60
	// guestRetention is how long an ended or expired guest session is kept
	// for the record before it is purged (Q8).
	guestRetention = 30 * 24 * time.Hour
	// janitorEvery bounds how long an expired guest stays connected.
	janitorEvery = 5 * time.Second
)

// Audit events for guest access.
const (
	EventLinkCreated = "share_link_created"
	EventLinkRevoked = "share_link_revoked"
	EventGuestJoined = "guest_joined"
	EventGuestLeft   = "guest_left"
	EventGuestKicked = "guest_removed"
	EventGuestEnded  = "guest_session_ended"
)

// ShareLink is an invitation to one document.
type ShareLink struct {
	ID         int64  `json:"id"`
	DocumentID int64  `json:"document_id"`
	Label      string `json:"label"`
	Role       string `json:"role"`
	AllowAI    bool   `json:"allow_ai"`
	MaxUses    int    `json:"max_uses"`
	Uses       int    `json:"uses"`
	CreatedBy  string `json:"created_by"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	RevokedAt  string `json:"revoked_at,omitempty"`
	// Status is active, expired, revoked or used_up.
	Status string `json:"status"`
}

// GuestSession is one person let in by a link.
type GuestSession struct {
	ID          int64  `json:"id"`
	LinkID      int64  `json:"link_id"`
	DocumentID  int64  `json:"document_id"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	AllowAI     bool   `json:"allow_ai"`
	CreatedAt   string `json:"created_at"`
	LastSeenAt  string `json:"last_seen_at"`
	ExpiresAt   string `json:"expires_at"`
	EndedAt     string `json:"ended_at,omitempty"`
	EndReason   string `json:"end_reason,omitempty"`
}

// Actor is how a guest appears in the audit, comments and suggestions: their
// display name, marked as a guest so no one mistakes it for an account.
func (g GuestSession) Actor() string { return g.DisplayName + " (guest)" }

// CanComment reports whether the role may comment.
func (g GuestSession) CanComment() bool { return g.Role == RoleCommenter || g.Role == RoleEditor }

// ErrGuestAccess is a guest request refused for a reason the person should
// see: the feature is off, the link has expired, and so on.
type ErrGuestAccess struct {
	Status int
	Msg    string
}

func (e ErrGuestAccess) Error() string { return e.Msg }

func guestRefusal(status int, msg string) error { return ErrGuestAccess{Status: status, Msg: msg} }

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) parseStamp(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func (s *Service) linkStatus(l ShareLink) string {
	switch {
	case l.RevokedAt != "":
		return "revoked"
	case !s.now().Before(s.parseStamp(l.ExpiresAt)):
		return "expired"
	case l.MaxUses > 0 && l.Uses >= l.MaxUses:
		return "used_up"
	}
	return "active"
}

// guestLinksAvailable is the one place the feature's two switches are read.
func (s *Service) guestLinksAvailable() error {
	if s.localMode {
		return guestRefusal(http.StatusForbidden, "Share links are not available in local mode: there are no accounts to tell a guest from anyone else.")
	}
	if s.guestLinks == nil || !s.guestLinks() {
		return guestRefusal(http.StatusForbidden, "Guest links are switched off. An administrator can enable them in Settings → Policy Studio.")
	}
	return nil
}

// NewShareLink is what creating a link takes.
type NewShareLink struct {
	Label        string `json:"label"`
	Role         string `json:"role"`
	ExpiresHours int    `json:"expires_hours"`
	AllowAI      bool   `json:"allow_ai"`
	MaxUses      int    `json:"max_uses"`
}

// CreateShareLink creates an invitation and returns it with its token, which
// is shown this once and stored only as a hash.
func (s *Service) CreateShareLink(documentID int64, in NewShareLink, actor string) (ShareLink, string, error) {
	if err := s.guestLinksAvailable(); err != nil {
		return ShareLink{}, "", err
	}
	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return ShareLink{}, "", err
	}
	if doc.EditorFormat != policydocs.EditorStudio {
		return ShareLink{}, "", invalid("open this document in the Studio before sharing it")
	}
	if in.Role == "" {
		in.Role = RoleViewer
	}
	if !guestRoles[in.Role] {
		return ShareLink{}, "", invalid("role must be viewer, commenter or editor")
	}
	if in.ExpiresHours == 0 {
		in.ExpiresHours = defaultInviteHours
	}
	if in.ExpiresHours < 1 || in.ExpiresHours > maxInviteHours {
		return ShareLink{}, "", invalid("a link lasts between 1 hour and 7 days")
	}
	if in.MaxUses == 0 {
		in.MaxUses = defaultMaxUses
	}
	if in.MaxUses < 1 || in.MaxUses > maxMaxUses {
		return ShareLink{}, "", invalid("a link can be used between 1 and %d times", maxMaxUses)
	}
	label := strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(label) > maxLabelRunes {
		return ShareLink{}, "", invalid("the label is at most %d characters", maxLabelRunes)
	}
	token, err := randomToken()
	if err != nil {
		return ShareLink{}, "", err
	}
	now := s.now().UTC()
	allow := 0
	if in.AllowAI {
		allow = 1
	}
	id, err := s.store.db.Insert(`INSERT INTO policy_share_links (document_id, token_sha256, label, role, allow_ai, max_uses, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, documentID, tokenHash(token), label, in.Role, allow, in.MaxUses, actor,
		now.Format(time.RFC3339Nano), now.Add(time.Duration(in.ExpiresHours)*time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return ShareLink{}, "", err
	}
	if err := s.audit(documentID, actor, EventLinkCreated, map[string]any{"link_id": id, "role": in.Role, "allow_ai": in.AllowAI,
		"expires_hours": in.ExpiresHours, "max_uses": in.MaxUses, "label": label}); err != nil {
		return ShareLink{}, "", err
	}
	link, err := s.shareLink(documentID, id)
	return link, token, err
}

const linkColumns = `id, document_id, label, role, allow_ai, max_uses, uses, created_by, created_at, expires_at, revoked_at`

type rowScanner interface{ Scan(dest ...any) error }

func (s *Service) scanLink(r rowScanner) (ShareLink, error) {
	var l ShareLink
	var allow int
	if err := r.Scan(&l.ID, &l.DocumentID, &l.Label, &l.Role, &allow, &l.MaxUses, &l.Uses, &l.CreatedBy, &l.CreatedAt, &l.ExpiresAt, &l.RevokedAt); err != nil {
		return ShareLink{}, err
	}
	l.AllowAI = allow != 0
	l.Status = s.linkStatus(l)
	return l, nil
}

func (s *Service) shareLink(documentID, linkID int64) (ShareLink, error) {
	l, err := s.scanLink(s.store.db.QueryRow(`SELECT `+linkColumns+` FROM policy_share_links WHERE id = ? AND document_id = ?`, linkID, documentID))
	if errors.Is(err, sql.ErrNoRows) {
		return ShareLink{}, policydocs.ErrNotFound
	}
	return l, err
}

// Sharing is a document's links and the guests they let in.
type Sharing struct {
	Enabled bool           `json:"enabled"`
	Reason  string         `json:"reason,omitempty"`
	Links   []ShareLink    `json:"links"`
	Guests  []GuestSession `json:"guests"`
}

// Sharing lists a document's links, newest first, and its guest sessions that
// have not ended.
func (s *Service) Sharing(documentID int64) (Sharing, error) {
	out := Sharing{Enabled: true, Links: []ShareLink{}, Guests: []GuestSession{}}
	if err := s.guestLinksAvailable(); err != nil {
		out.Enabled, out.Reason = false, err.Error()
	}
	rows, err := s.store.db.Query(`SELECT `+linkColumns+` FROM policy_share_links WHERE document_id = ? ORDER BY id DESC`, documentID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		l, err := s.scanLink(rows)
		if err != nil {
			_ = rows.Close()
			return out, err
		}
		out.Links = append(out.Links, l)
	}
	_ = rows.Close()
	sessions, err := s.guestSessions(`l.document_id = ? AND g.ended_at = ''`, documentID)
	if err != nil {
		return out, err
	}
	for _, g := range sessions {
		if s.now().Before(s.parseStamp(g.ExpiresAt)) {
			out.Guests = append(out.Guests, g)
		}
	}
	return out, nil
}

func (s *Service) guestSessions(where string, args ...any) ([]GuestSession, error) {
	rows, err := s.store.db.Query(`SELECT g.id, g.link_id, l.document_id, g.display_name, l.role, l.allow_ai, g.created_at, g.last_seen_at,
		g.expires_at, g.ended_at, g.end_reason FROM policy_guest_sessions g JOIN policy_share_links l ON l.id = g.link_id WHERE `+where+` ORDER BY g.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GuestSession
	for rows.Next() {
		var g GuestSession
		var allow int
		if err := rows.Scan(&g.ID, &g.LinkID, &g.DocumentID, &g.DisplayName, &g.Role, &allow, &g.CreatedAt, &g.LastSeenAt,
			&g.ExpiresAt, &g.EndedAt, &g.EndReason); err != nil {
			return nil, err
		}
		g.AllowAI = allow != 0
		out = append(out, g)
	}
	return out, rows.Err()
}

// RevokeShareLink ends a link and every session it let in; its guests are
// disconnected now, not at their next request.
func (s *Service) RevokeShareLink(documentID, linkID int64, actor string) error {
	if _, err := s.shareLink(documentID, linkID); err != nil {
		return err
	}
	stamp := s.store.stamp()
	if _, err := s.store.db.Exec(`UPDATE policy_share_links SET revoked_at = ? WHERE id = ? AND revoked_at = ''`, stamp, linkID); err != nil {
		return err
	}
	if _, err := s.store.db.Exec(`UPDATE policy_guest_sessions SET ended_at = ?, end_reason = 'revoked' WHERE link_id = ? AND ended_at = ''`, stamp, linkID); err != nil {
		return err
	}
	if err := s.audit(documentID, actor, EventLinkRevoked, map[string]any{"link_id": linkID}); err != nil {
		return err
	}
	s.closeRoom(documentID)
	return nil
}

// KickGuest ends one guest session and disconnects it.
func (s *Service) KickGuest(documentID, sessionID int64, actor string) error {
	sessions, err := s.guestSessions(`g.id = ? AND l.document_id = ?`, sessionID, documentID)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return policydocs.ErrNotFound
	}
	if _, err := s.store.db.Exec(`UPDATE policy_guest_sessions SET ended_at = ?, end_reason = 'removed' WHERE id = ? AND ended_at = ''`, s.store.stamp(), sessionID); err != nil {
		return err
	}
	if err := s.audit(documentID, actor, EventGuestKicked, map[string]any{"session_id": sessionID, "name": sessions[0].DisplayName}); err != nil {
		return err
	}
	s.closeRoom(documentID)
	return nil
}

// LinkForToken finds a link by its token, for the join page.
func (s *Service) LinkForToken(token string) (ShareLink, policydocs.Document, error) {
	if len(token) < 20 || len(token) > 100 {
		return ShareLink{}, policydocs.Document{}, guestRefusal(http.StatusNotFound, "This link is not valid.")
	}
	want := tokenHash(token)
	var stored string
	var id, docID int64
	err := s.store.db.QueryRow(`SELECT id, document_id, token_sha256 FROM policy_share_links WHERE token_sha256 = ?`, want).Scan(&id, &docID, &stored)
	if errors.Is(err, sql.ErrNoRows) || err == nil && subtle.ConstantTimeCompare([]byte(stored), []byte(want)) != 1 {
		return ShareLink{}, policydocs.Document{}, guestRefusal(http.StatusNotFound, "This link is not valid.")
	}
	if err != nil {
		return ShareLink{}, policydocs.Document{}, err
	}
	link, err := s.shareLink(docID, id)
	if err != nil {
		return ShareLink{}, policydocs.Document{}, err
	}
	doc, err := s.policies.GetDocument(docID)
	if err != nil {
		return ShareLink{}, policydocs.Document{}, err
	}
	return link, doc, nil
}

func linkRefusal(status string) error {
	switch status {
	case "revoked":
		return guestRefusal(http.StatusGone, "This link has been withdrawn. Ask the person who sent it for a new one.")
	case "expired":
		return guestRefusal(http.StatusGone, "This link has expired. Ask the person who sent it for a new one.")
	case "used_up":
		return guestRefusal(http.StatusGone, "This link has been used as many times as it allows. Ask the person who sent it for a new one.")
	}
	return nil
}

// cleanName keeps a display name to printable text of a sensible length.
func cleanName(name string) (string, error) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if out == "" {
		return "", invalid("enter your name as the others should see it")
	}
	if utf8.RuneCountInString(out) > maxNameRunes {
		return "", invalid("a name is at most %d characters", maxNameRunes)
	}
	return out, nil
}

// Redeem turns a link into a guest session for the person who opened it,
// returning the session token for their cookie.
func (s *Service) Redeem(token, displayName string) (GuestSession, string, error) {
	if err := s.guestLinksAvailable(); err != nil {
		return GuestSession{}, "", err
	}
	link, doc, err := s.LinkForToken(token)
	if err != nil {
		return GuestSession{}, "", err
	}
	if err := linkRefusal(link.Status); err != nil {
		return GuestSession{}, "", err
	}
	if doc.EditorFormat != policydocs.EditorStudio {
		return GuestSession{}, "", guestRefusal(http.StatusGone, "This document is no longer shared.")
	}
	name, err := cleanName(displayName)
	if err != nil {
		return GuestSession{}, "", err
	}
	// The use is counted in the same statement that checks it, so two people
	// redeeming the last use at once cannot both get in.
	res, err := s.store.db.Exec(`UPDATE policy_share_links SET uses = uses + 1 WHERE id = ? AND revoked_at = '' AND uses < max_uses`, link.ID)
	if err != nil {
		return GuestSession{}, "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return GuestSession{}, "", linkRefusal("used_up")
	}
	sessionToken, err := randomToken()
	if err != nil {
		return GuestSession{}, "", err
	}
	stamp := s.store.stamp()
	id, err := s.store.db.Insert(`INSERT INTO policy_guest_sessions (link_id, session_sha256, display_name, created_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, link.ID, tokenHash(sessionToken), name, stamp, stamp, link.ExpiresAt)
	if err != nil {
		return GuestSession{}, "", err
	}
	g := GuestSession{ID: id, LinkID: link.ID, DocumentID: doc.ID, DisplayName: name, Role: link.Role, AllowAI: link.AllowAI,
		CreatedAt: stamp, LastSeenAt: stamp, ExpiresAt: link.ExpiresAt}
	if err := s.audit(doc.ID, g.Actor(), EventGuestJoined, map[string]any{"session_id": id, "link_id": link.ID, "role": link.Role}); err != nil {
		return GuestSession{}, "", err
	}
	return g, sessionToken, nil
}

// lastSeenEvery throttles the last-seen write to one per session per interval.
const lastSeenEvery = 30 * time.Second

// Guest resolves the guest session a request carries, if it is valid now:
// the feature on, the session neither ended nor expired, and its link not
// revoked.
func (s *Service) Guest(r *http.Request) (GuestSession, bool) {
	if s.guestLinksAvailable() != nil {
		return GuestSession{}, false
	}
	cookie, err := r.Cookie(GuestCookie)
	if err != nil || len(cookie.Value) < 20 || len(cookie.Value) > 100 {
		return GuestSession{}, false
	}
	sessions, err := s.guestSessions(`g.session_sha256 = ?`, tokenHash(cookie.Value))
	if err != nil || len(sessions) != 1 {
		return GuestSession{}, false
	}
	g := sessions[0]
	link, err := s.shareLink(g.DocumentID, g.LinkID)
	if err != nil || g.EndedAt != "" || link.RevokedAt != "" || !s.now().Before(s.parseStamp(g.ExpiresAt)) {
		return GuestSession{}, false
	}
	if s.now().Sub(s.parseStamp(g.LastSeenAt)) > lastSeenEvery {
		stamp := s.store.stamp()
		_, _ = s.store.db.Exec(`UPDATE policy_guest_sessions SET last_seen_at = ? WHERE id = ?`, stamp, g.ID)
		g.LastSeenAt = stamp
	}
	return g, true
}

// Leave ends a guest's own session.
func (s *Service) Leave(g GuestSession) error {
	if _, err := s.store.db.Exec(`UPDATE policy_guest_sessions SET ended_at = ?, end_reason = 'left' WHERE id = ? AND ended_at = ''`, s.store.stamp(), g.ID); err != nil {
		return err
	}
	return s.audit(g.DocumentID, g.Actor(), EventGuestLeft, map[string]any{"session_id": g.ID})
}

// sweepGuests ends the sessions whose time is up, disconnecting their rooms,
// and purges old ones. It runs on a timer, so an expired guest is out within
// janitorEvery rather than at their next request.
func (s *Service) sweepGuests() {
	if s.localMode {
		return
	}
	stamp := s.store.stamp()
	expired, err := s.guestSessions(`g.ended_at = '' AND (g.expires_at <= ? OR l.revoked_at <> '')`, stamp)
	if err != nil {
		s.log.Warn("policy studio: guest sweep", "error", err)
		return
	}
	docs := map[int64]bool{}
	for _, g := range expired {
		if _, err := s.store.db.Exec(`UPDATE policy_guest_sessions SET ended_at = ?, end_reason = 'expired' WHERE id = ? AND ended_at = ''`, stamp, g.ID); err == nil {
			_ = s.audit(g.DocumentID, g.Actor(), EventGuestEnded, map[string]any{"session_id": g.ID, "reason": "expired"})
			docs[g.DocumentID] = true
		}
	}
	for id := range docs {
		s.closeRoom(id)
	}
	cutoff := s.now().Add(-guestRetention).UTC().Format(time.RFC3339Nano)
	_, _ = s.store.db.Exec(`DELETE FROM policy_guest_sessions WHERE expires_at < ?`, cutoff)
}

func (s *Service) runGuestJanitor() {
	t := time.NewTicker(janitorEvery)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.sweepGuests()
		}
	}
}
