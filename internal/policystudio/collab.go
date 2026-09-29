package policystudio

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	ygws "github.com/reearth/ygo/provider/websocket"

	"grc/internal/policydocs"
)

// Identity is who a request comes from, as the application's session layer
// resolved it.
type Identity struct {
	Username string
	Admin    bool
	// CanReadPolicies is the /policies page grant: the Studio is readable by
	// anyone who can read the policy library, and writable only by admins.
	CanReadPolicies bool
}

// IdentityFunc resolves a request's identity; ok is false for no session.
// In local mode the application returns an admin for every request.
type IdentityFunc func(r *http.Request) (Identity, bool)

// Decision is the collab authorization outcome for one upgrade.
type Decision struct {
	Allow    bool
	ReadOnly bool
	Reason   string
}

// decide is the single authorization decision for /collab/. Every upgrade
// goes through it; TestCollabDecision is its table.
//
// A browser upgrade must carry an Origin on the allowlist (the request's own
// host, plus the configured external origins) -- ygo alone would admit an
// upgrade with no Origin, and the session cookie would make that a cross-site
// WebSocket hijack. Read-write goes only to admins, and only while the
// document is a draft or in review and no status change is in progress. In
// review the editor switches to suggest mode; the server cannot tell a
// suggestion from a direct edit in a Yjs update, so what it guarantees is the
// approval block: nothing is approved while a suggestion is pending.
func (s *Service) decide(r *http.Request, documentID int64) Decision {
	if !s.originAllowed(r) {
		return Decision{Reason: "origin not allowed"}
	}
	id, ok := s.identity(r)
	if !ok {
		// A guest session reaches exactly one room: its link's document.
		// Read-write only for the editor role, and only on a draft.
		g, isGuest := s.Guest(r)
		if !isGuest {
			return Decision{Reason: "not signed in"}
		}
		if g.DocumentID != documentID {
			return Decision{Reason: "guest for another document"}
		}
		doc, err := s.policies.GetDocument(documentID)
		if err != nil || doc.EditorFormat != policydocs.EditorStudio {
			return Decision{Reason: "no such document"}
		}
		writable := g.Role == RoleEditor && doc.Status == policydocs.StatusDraft && !s.transitioning(documentID)
		return Decision{Allow: true, ReadOnly: !writable}
	}
	if !id.CanReadPolicies {
		return Decision{Reason: "no access to policies"}
	}
	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return Decision{Reason: "no such document"}
	}
	if doc.EditorFormat != policydocs.EditorStudio {
		return Decision{Reason: "document is not in the Studio"}
	}
	writable := id.Admin && projected(doc.Status) && !s.transitioning(documentID)
	return Decision{Allow: true, ReadOnly: !writable}
}

func (s *Service) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range s.origins {
		if strings.EqualFold(strings.TrimRight(allowed, "/"), origin) {
			return true
		}
	}
	return false
}

// newCollabServer configures ygo. Every cap is set explicitly: the defaults
// are sized for a multi-tenant service (64 MiB updates) or are unlimited.
func (s *Service) newCollabServer() *ygws.Server {
	a := adapter{store: s.store, prepare: s.prepare}
	srv := ygws.NewServerWithPersistence(a)
	srv.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv.Authorize = func(r *http.Request) (ygws.ConnectionConfig, bool) {
		id, ok := DocumentIDOf(r.PathValue("room"))
		if !ok {
			return ygws.ConnectionConfig{}, false
		}
		d := s.decide(r, id)
		if !d.Allow {
			// Never the content, never the cookie: who and why only.
			s.log.Info("policy studio: collab refused", "document", id, "reason", d.Reason)
		}
		return ygws.ConnectionConfig{ReadOnly: d.ReadOnly}, d.Allow
	}
	if len(s.origins) > 0 {
		// decide() already enforced the allowlist, request host included;
		// ygo's own check can only express a fixed list, which cannot name the
		// request's host, so it is opened here rather than duplicated wrongly.
		srv.AllowedOrigins = []string{"*"}
	}
	srv.MaxUpdateBytes = 2 << 20
	srv.MaxMessageBytes = 2 << 20
	srv.MaxPeersPerRoom = 12
	srv.MaxConnections = 200
	srv.MaxRooms = 500
	srv.HandshakeTimeout = 10 * time.Second
	srv.MaxAwarenessBytesPerRoom = 1 << 20
	srv.MaxAwarenessClientsPerRoom = 64
	srv.MessageRateLimit = 200
	srv.MessageRateBurst = 400
	srv.RoomIdleTimeout = 60 * time.Second
	srv.MaxResidentRooms = 50
	srv.PersistCoalesceWindow = time.Second
	srv.PersistCoalesceMaxWait = 5 * time.Second
	srv.CompactEvery = 50
	srv.AutoVersionEvery = 15 * time.Minute
	return srv
}

// statusLockTTL bounds how long a status change holds a document read-only if
// the change neither completes nor reports failing.
const statusLockTTL = 30 * time.Second

func (s *Service) lockForTransition(documentID int64) {
	s.mu.Lock()
	s.locks[documentID] = s.now().Add(statusLockTTL)
	s.mu.Unlock()
}

func (s *Service) unlockTransition(documentID int64) {
	s.mu.Lock()
	delete(s.locks, documentID)
	s.mu.Unlock()
}

func (s *Service) transitioning(documentID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.locks[documentID]
	if ok && s.now().After(until) {
		delete(s.locks, documentID)
		return false
	}
	return ok
}
