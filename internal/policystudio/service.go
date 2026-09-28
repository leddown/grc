package policystudio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/reearth/ygo/crdt"
	ygws "github.com/reearth/ygo/provider/websocket"

	"grc/internal/clientprofile"
	"grc/internal/db"
	"grc/internal/policydocs"
)

// ErrNotStudio is returned for a Studio operation on a Markdown document.
var ErrNotStudio = errors.New("policystudio: document is not in the Studio")

// Options configures a Service.
type Options struct {
	// AllowedOrigins are origins, beyond the request's own host, that may
	// open the collaboration socket (-studio-allowed-origins).
	AllowedOrigins []string
	// SnapshotRetain is how many snapshots per document are kept (0: 50).
	SnapshotRetain int
	// Identity resolves who a request comes from.
	Identity IdentityFunc
	// OnProjected is told after a projection is stored, so caches of the
	// section rows (the knowledge API's) can be dropped.
	OnProjected func(documentID int64)
	// Clients resolves fact tokens against a document's client profile. Nil
	// leaves every fact unresolved.
	Clients *clientprofile.Service
}

// Service is the Policy Studio: the collaboration server, the persistence
// behind it, and the projection of each live document into its section rows.
type Service struct {
	policies    *policydocs.Service
	store       *Store
	collab      *ygws.Server
	identity    IdentityFunc
	origins     []string
	onProjected func(int64)
	clients     *clientprofile.Service
	templates   []Template
	log         *slog.Logger
	now         func() time.Time

	mu        sync.Mutex
	locks     map[int64]time.Time
	timers    map[int64]*time.Timer
	problems  map[int64][]Problem
	projectMu map[int64]*sync.Mutex
	closed    bool
}

// projectionDelay coalesces bursts of stored updates into one projection.
const projectionDelay = 750 * time.Millisecond

// NewService wires the Studio to the policy module. It installs the policy
// lifecycle hooks, so there must be exactly one per policy service.
func NewService(conn *db.Conn, policies *policydocs.Service, opts Options) *Service {
	s := &Service{
		policies:    policies,
		store:       NewStore(conn),
		identity:    opts.Identity,
		origins:     opts.AllowedOrigins,
		onProjected: opts.OnProjected,
		clients:     opts.Clients,
		log:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		now:         time.Now,
		locks:       map[int64]time.Time{},
		timers:      map[int64]*time.Timer{},
		problems:    map[int64][]Problem{},
		projectMu:   map[int64]*sync.Mutex{},
	}
	if s.identity == nil {
		s.identity = func(*http.Request) (Identity, bool) { return Identity{}, false }
	}
	if opts.SnapshotRetain > 0 {
		s.store.Retain = opts.SnapshotRetain
	}
	s.store.onStored = s.schedule
	templates, err := loadTemplates()
	if err != nil {
		// Embedded data that does not validate is a build defect, which
		// TestTemplatesAreValid catches; a running server offers none rather
		// than a half-valid set.
		s.log.Error("policy studio: templates", "error", err)
	}
	s.templates = templates
	s.collab = s.newCollabServer()
	if s.clients != nil {
		s.clients.OnFactsChanged(s.reprojectClient)
	}
	policies.SetHooks(policydocs.Hooks{
		BeforeTransition:  s.beforeTransition,
		AfterTransition:   s.afterTransition,
		TransitionAborted: s.transitionAborted,
		AfterDelete:       s.afterDelete,
	})
	return s
}

// Shutdown flushes every room and stops the projection timers.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	for _, t := range s.timers {
		t.Stop()
	}
	s.mu.Unlock()
	return s.collab.Shutdown(ctx)
}

// Live reports whether any Studio document is open, so readers of the section
// rows (the knowledge API) know the rows are moving.
func (s *Service) Live() bool { return len(s.collab.Rooms()) > 0 }

func newBlockID() string { return policydocs.NewUID() }

func encodeDoc(root *Node) ([]byte, error) {
	d := crdt.New()
	if err := WriteDoc(d, root); err != nil {
		return nil, err
	}
	return crdt.EncodeStateAsUpdateV1(d, nil), nil
}

// prepare runs when ygo loads a document's room, before any peer sees it. A
// document with no state yet is seeded from its section rows, exactly once. A
// document whose rows no longer carry the token its state was paired with had
// its rows replaced outside the Studio -- a sync from another deployment, a
// restore -- and the rows win: the state is rebuilt from them and the old one
// kept as a snapshot.
func (s *Service) prepare(documentID int64) error {
	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return err
	}
	if doc.EditorFormat != policydocs.EditorStudio {
		return ErrNotStudio
	}
	token, exists, err := s.store.stateToken(documentID)
	if err != nil {
		return err
	}
	if exists && token == doc.ProjectionToken {
		return nil
	}
	rows, err := s.policies.ListAllSections(documentID)
	if err != nil {
		return err
	}
	root, err := DocumentFromRows(rows, newBlockID)
	if err != nil {
		return err
	}
	update, err := encodeDoc(root)
	if err != nil {
		return err
	}
	if !exists {
		_, err := s.store.SeedIfAbsent(documentID, update)
		return err
	}
	s.log.Warn("policy studio: section rows changed outside the Studio; rebuilding the document from them",
		"document", documentID)
	return s.store.Replace(documentID, update, SnapshotSuperseded)
}

// schedule debounces a projection after a stored update.
func (s *Service) schedule(documentID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if t, ok := s.timers[documentID]; ok {
		t.Reset(projectionDelay)
		return
	}
	s.timers[documentID] = time.AfterFunc(projectionDelay, func() {
		s.mu.Lock()
		delete(s.timers, documentID)
		s.mu.Unlock()
		if err := s.Project(documentID); err != nil && !errors.Is(err, policydocs.ErrNotFound) {
			s.log.Error("policy studio: projection failed", "document", documentID, "error", err)
		}
	})
}

func (s *Service) docLock(documentID int64) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.projectMu[documentID]
	if !ok {
		m = &sync.Mutex{}
		s.projectMu[documentID] = m
	}
	return m
}

// currentState is the document as it stands: the live room when it is loaded
// (encoded under the document's lock, then read from a private copy -- ygo's
// read methods take no lock), otherwise the store.
func (s *Service) currentState(documentID int64) ([]byte, error) {
	if live := s.collab.GetDoc(RoomName(documentID)); live != nil {
		return crdt.EncodeStateAsUpdateV1(live, nil), nil
	}
	return s.store.Load(documentID)
}

// Project computes the section rows from the server's copy of the document
// and stores them in one transaction. Only drafts are projected: once a
// document is submitted, its rows are what review and approval read.
func (s *Service) Project(documentID int64) error {
	lock := s.docLock(documentID)
	lock.Lock()
	defer lock.Unlock()

	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return err
	}
	if doc.EditorFormat != policydocs.EditorStudio || doc.Status != policydocs.StatusDraft {
		return nil
	}
	data, err := s.currentState(documentID)
	if err != nil || len(data) == 0 {
		return err
	}
	ydoc := crdt.New()
	if err := crdt.ApplyUpdateV1(ydoc, data, nil); err != nil {
		return fmt.Errorf("read document %d: %w", documentID, err)
	}
	reads := readSections(ydoc)

	rows, err := s.policies.ListAllSections(documentID)
	if err != nil {
		return err
	}
	facts, err := s.factValues(doc.ClientProfileID)
	if err != nil {
		return err
	}
	byUID := map[string]policydocs.Section{}
	for _, r := range rows {
		byUID[r.UID] = r
	}
	seen := map[string]bool{}
	var problems []Problem
	var out []policydocs.SectionProjection
	for _, r := range reads {
		if r.Problem != nil {
			problems = append(problems, *r.Problem)
			// A section the server refused keeps its last good row, in its
			// place, so a client that writes something invalid cannot make a
			// section vanish from the policy.
			if row, ok := byUID[r.Problem.SectionUID]; ok && !seen[row.UID] && row.DetachedAt == "" {
				seen[row.UID] = true
				out = append(out, policydocs.SectionProjection{
					UID: row.UID, Heading: row.Heading, Body: row.Body, SectionKind: row.SectionKind,
					ContentJSON: row.ContentJSON, BlocksJSON: row.BlocksJSON,
				})
			}
			continue
		}
		sec := r.Node
		uid := sec.Attr("uid")
		if seen[uid] {
			problems = append(problems, Problem{SectionUID: uid, Message: "the section appears twice; only the first copy is kept"})
			continue
		}
		if _, ok := byUID[uid]; !ok {
			problems = append(problems, Problem{SectionUID: uid, Message: "a section the server did not create was ignored"})
			continue
		}
		seen[uid] = true
		base := Resolve(sec, Baseline)
		content, err := json.Marshal(sec)
		if err != nil {
			return err
		}
		blocks, err := json.Marshal(SectionBlocks(base, facts))
		if err != nil {
			return err
		}
		out = append(out, policydocs.SectionProjection{
			UID: uid, Heading: HeadingText(base), Body: SectionMarkdown(base, facts),
			SectionKind: sec.Attr("kind"), ContentJSON: string(content), BlocksJSON: string(blocks),
		})
	}
	if _, err := s.policies.ApplyProjection(documentID, out); err != nil {
		return err
	}
	s.mu.Lock()
	s.problems[documentID] = problems
	s.mu.Unlock()
	if s.onProjected != nil {
		s.onProjected(documentID)
	}
	return nil
}

// factValues is a client's recorded facts, key -> value.
func (s *Service) factValues(clientID int64) (map[string]string, error) {
	if s.clients == nil || clientID <= 0 {
		return map[string]string{}, nil
	}
	return s.clients.Values(clientID)
}

// reprojectClient recomputes every Studio draft written for a client after its
// facts change: the tokens render by reference, so a new value reaches the
// rows, the lint gate and the exports without anyone editing the text.
func (s *Service) reprojectClient(clientID int64) {
	docs, err := s.policies.ListDocuments(policydocs.Filter{})
	if err != nil {
		s.log.Error("policy studio: list documents for a fact change", "error", err)
		return
	}
	for _, d := range docs {
		if d.ClientProfileID == clientID && d.EditorFormat == policydocs.EditorStudio && d.Status == policydocs.StatusDraft {
			if err := s.Project(d.ID); err != nil {
				s.log.Error("policy studio: reproject after a fact change", "document", d.ID, "error", err)
			}
		}
	}
}

// Problems are the content refusals of the last projection.
func (s *Service) Problems(documentID int64) []Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Problem(nil), s.problems[documentID]...)
}

// closeRoom disconnects a room's peers after durably flushing it; they
// reconnect, and are authorized afresh.
func (s *Service) closeRoom(documentID int64) {
	if err := s.collab.CloseRoom(RoomName(documentID), true); err != nil && !errors.Is(err, ygws.ErrRoomNotFound) {
		s.log.Warn("policy studio: close room", "document", documentID, "error", err)
	}
}

// beforeTransition flushes and projects a Studio document before its status
// changes, so the lint gate and the approval snapshot read what people wrote,
// and holds it read-only until the change completes: an edit arriving between
// this projection and the new status would otherwise reach neither.
func (s *Service) beforeTransition(doc policydocs.Document, to string) error {
	if doc.EditorFormat != policydocs.EditorStudio {
		return nil
	}
	s.lockForTransition(doc.ID)
	s.closeRoom(doc.ID)
	if err := s.Project(doc.ID); err != nil {
		s.transitionAborted(doc, to)
		return fmt.Errorf("could not save the latest Studio edits before the status change: %w", err)
	}
	if to == policydocs.StatusApproved {
		if problems := s.Problems(doc.ID); len(problems) > 0 {
			s.transitionAborted(doc, to)
			return policydocs.ApprovalError{Findings: problemFindings(problems)}
		}
	}
	if data, err := s.store.Load(doc.ID); err == nil {
		if _, err := s.store.Snapshot(doc.ID, SnapshotStatus+":"+to, SnapshotFormatYjs, "", data); err != nil {
			s.log.Warn("policy studio: status snapshot", "document", doc.ID, "error", err)
		}
	}
	return nil
}

func (s *Service) afterTransition(doc policydocs.Document, _ string) {
	if doc.EditorFormat != policydocs.EditorStudio {
		return
	}
	s.unlockTransition(doc.ID)
	s.closeRoom(doc.ID)
}

func (s *Service) transitionAborted(doc policydocs.Document, _ string) {
	if doc.EditorFormat != policydocs.EditorStudio {
		return
	}
	s.unlockTransition(doc.ID)
	s.closeRoom(doc.ID)
}

func (s *Service) afterDelete(id int64) {
	s.closeRoom(id)
	s.mu.Lock()
	delete(s.problems, id)
	delete(s.projectMu, id)
	s.mu.Unlock()
}

func problemFindings(problems []Problem) []policydocs.Finding {
	out := make([]policydocs.Finding, 0, len(problems))
	for _, p := range problems {
		out = append(out, policydocs.Finding{
			Severity: policydocs.SeverityError,
			Message:  "the server refused part of this section's content (" + p.Message + "); remove it in the Studio",
			Heading:  p.SectionUID,
		})
	}
	return out
}
