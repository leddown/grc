package policystudio

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/reearth/ygo/crdt"

	"grc/internal/policydocs"
)

// The review layer: suggestion decisions, comment threads and the audit that
// records both. Who acted always comes from the session; a request body never
// names an actor.

// Comment visibility, thread kinds and statuses.
const (
	VisibilityInternal = "internal"
	VisibilityShared   = "shared"
	ThreadComment      = "comment"
	ThreadAIRationale  = "ai_rationale"
	ThreadOpen         = "open"
	ThreadResolved     = "resolved"
	AuthorHuman        = "human"
	AuthorAI           = "ai"
)

// Audit events.
const (
	EventSuggestionAccepted = "suggestion_accepted"
	EventSuggestionRejected = "suggestion_rejected"
	EventThreadCreated      = "comment_thread_created"
	EventCommentAdded       = "comment_added"
	EventThreadResolved     = "comment_thread_resolved"
	EventThreadReopened     = "comment_thread_reopened"
)

// Audience selects which threads a response may carry. Guests (Phase 4) are
// AudienceShared; every other reader of the Studio is internal.
type Audience int

const (
	AudienceInternal Audience = iota
	AudienceShared
)

const (
	maxAnchorBytes  = 512
	maxQuoteRunes   = 1000
	maxCommentRunes = 5000
	maxDecisionIDs  = 500
)

// ErrConflict is a request that no longer matches the document -- a
// suggestion someone else already decided, say.
type ErrConflict struct{ Msg string }

func (e ErrConflict) Error() string { return e.Msg }

// ---- suggestions ----

// SuggestionInfo describes one suggestion as the server's copy of the document
// holds it. The author fields are what the suggesting client wrote into the
// mark; the server cannot verify them, which is why the decision -- who
// accepted or rejected it -- is taken from the session instead.
type SuggestionInfo struct {
	ID         string   `json:"id"`
	Types      []string `json:"types"`
	SectionUID string   `json:"section_uid"`
	BlockIDs   []string `json:"block_ids"`
	AuthorID   string   `json:"author_id,omitempty"`
	AuthorName string   `json:"author_name,omitempty"`
	AuthorKind string   `json:"author_kind,omitempty"`
	ProposalID string   `json:"proposal_id,omitempty"`
	CreatedAt  string   `json:"created_at,omitempty"`
	Inserted   string   `json:"inserted,omitempty"`
	Deleted    string   `json:"deleted,omitempty"`
	Changed    string   `json:"changed,omitempty"`
	// Blocks are the node types a suggestion adds or removes whole (a list
	// item, a table row), for the ones that carry no text of their own.
	Blocks []string `json:"blocks,omitempty"`
}

// collectSuggestions indexes every pending suggestion in the given sections by
// id, in document order.
func collectSuggestions(sections []*Node) (map[string]*SuggestionInfo, []string) {
	byID := map[string]*SuggestionInfo{}
	var order []string
	var walk func(n *Node, sectionUID, bid string)
	walk = func(n *Node, sectionUID, bid string) {
		if b := n.Attr("bid"); b != "" {
			bid = b
		}
		for _, m := range n.Marks {
			if !suggestionMarkTypes[m.Type] {
				continue
			}
			id, _ := m.Attrs["id"].(string)
			info, ok := byID[id]
			if !ok {
				str := func(k string) string { v, _ := m.Attrs[k].(string); return v }
				info = &SuggestionInfo{ID: id, SectionUID: sectionUID, AuthorID: str("authorId"), AuthorName: str("authorName"),
					AuthorKind: str("authorKind"), ProposalID: str("proposalId"), CreatedAt: str("createdAt")}
				byID[id] = info
				order = append(order, id)
			}
			if !containsString(info.Types, m.Type) {
				info.Types = append(info.Types, m.Type)
			}
			if !n.IsText() && n.Type != "factToken" && n.Type != "controlRef" && n.Type != "hardBreak" && !containsString(info.Blocks, n.Type) {
				info.Blocks = append(info.Blocks, n.Type)
			}
			if bid != "" && !containsString(info.BlockIDs, bid) {
				info.BlockIDs = append(info.BlockIDs, bid)
			}
			text := strings.ReplaceAll(n.TextContent(), zeroWidthSpace, "")
			switch m.Type {
			case "insertion":
				info.Inserted = excerpt(info.Inserted + text)
			case "deletion":
				info.Deleted = excerpt(info.Deleted + text)
			case "modification":
				name, _ := m.Attrs["attrName"].(string)
				info.Changed = excerpt(fmt.Sprintf("%s: %v → %v", name, m.Attrs["previousValue"], m.Attrs["newValue"]))
			}
		}
		for _, c := range n.Content {
			walk(c, sectionUID, bid)
		}
	}
	for _, sec := range sections {
		walk(sec, sec.Attr("uid"), "")
	}
	return byID, order
}

func excerpt(s string) string {
	const max = 240
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// LiveSections reads the document as it stands now: the sections the server
// accepts, from a private copy of the live room (or the store).
//
// A document nobody has opened in the editor yet has no state; it is seeded
// from its section rows first, exactly as opening it would.
func (s *Service) LiveSections(documentID int64) ([]*Node, error) {
	if s.collab.GetDoc(RoomName(documentID)) == nil {
		if _, exists, err := s.store.stateToken(documentID); err != nil {
			return nil, err
		} else if !exists {
			if err := s.prepare(documentID); err != nil {
				return nil, err
			}
		}
	}
	data, err := s.currentState(documentID)
	if err != nil {
		return nil, err
	}
	ydoc := crdt.New()
	if len(data) > 0 {
		if err := crdt.ApplyUpdateV1(ydoc, data, nil); err != nil {
			return nil, fmt.Errorf("read document %d: %w", documentID, err)
		}
	}
	var out []*Node
	for _, r := range readSections(ydoc) {
		if r.Node != nil {
			out = append(out, r.Node)
		}
	}
	return out, nil
}

// reviewable loads a Studio document whose suggestions may be decided and
// whose text may be commented on: a draft, or a document in review.
func (s *Service) reviewable(documentID int64, what string) (policydocs.Document, error) {
	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return doc, err
	}
	if doc.EditorFormat != policydocs.EditorStudio {
		return doc, invalid("open this document in the Studio first")
	}
	if doc.Status != policydocs.StatusDraft && doc.Status != policydocs.StatusInReview {
		return doc, invalid("the document is %s; %s only while it is a draft or in review", doc.Status, what)
	}
	return doc, nil
}

// DecisionRequest accepts or rejects suggestions by id.
type DecisionRequest struct {
	Decision      string   `json:"decision"`
	SuggestionIDs []string `json:"suggestion_ids"`
}

// Decide records a decision on pending suggestions. The editor applies the
// decision to the shared document itself (through the suggest-changes
// commands); this is the record of who decided, taken before the change so it
// can be checked against the server's copy: every id must still be pending
// there, and what is recorded about each suggestion -- its text, author and
// blocks -- is read from that copy, not from the request.
func (s *Service) Decide(documentID int64, req DecisionRequest, actor string) ([]SuggestionInfo, error) {
	event := map[string]string{"accept": EventSuggestionAccepted, "reject": EventSuggestionRejected}[req.Decision]
	if event == "" {
		return nil, invalid("decision must be accept or reject")
	}
	if len(req.SuggestionIDs) == 0 || len(req.SuggestionIDs) > maxDecisionIDs {
		return nil, invalid("name between 1 and %d suggestions", maxDecisionIDs)
	}
	if _, err := s.reviewable(documentID, "suggestions can be decided"); err != nil {
		return nil, err
	}
	sections, err := s.LiveSections(documentID)
	if err != nil {
		return nil, err
	}
	pending, _ := collectSuggestions(sections)
	out := make([]SuggestionInfo, 0, len(req.SuggestionIDs))
	seen := map[string]bool{}
	for _, id := range req.SuggestionIDs {
		info, ok := pending[id]
		if !ok || id == "" {
			return nil, ErrConflict{Msg: "a suggestion was already decided or withdrawn; the list has been refreshed"}
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, *info)
		}
	}
	tx, err := s.store.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := s.store.stamp()
	for _, info := range out {
		detail, err := json.Marshal(info)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO policy_studio_audit (document_id, actor, actor_kind, event, detail_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, documentID, actor, AuthorHuman, event, string(detail), stamp); err != nil {
			return nil, err
		}
		// A suggestion an AI proposal placed carries its edit's suid as its
		// id; the proposal's own record of the decision is kept with it.
		if _, err := tx.Exec(`UPDATE policy_ai_edits SET decision = ?, decided_by = ?, decided_at = ? WHERE suid = ?`,
			req.Decision, actor, stamp, info.ID); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

// ---- audit and provenance ----

// AuditEvent is one row of the Studio's audit.
type AuditEvent struct {
	ID        int64           `json:"id"`
	Actor     string          `json:"actor"`
	ActorKind string          `json:"actor_kind"`
	Event     string          `json:"event"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt string          `json:"created_at"`
}

func (s *Service) audit(documentID int64, actor, event string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = s.store.db.Exec(`INSERT INTO policy_studio_audit (document_id, actor, actor_kind, event, detail_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, documentID, actor, AuthorHuman, event, string(raw), s.store.stamp())
	return err
}

func (s *Service) auditEvents(documentID int64, events ...string) ([]AuditEvent, error) {
	q := `SELECT id, actor, actor_kind, event, detail_json, created_at FROM policy_studio_audit WHERE document_id = ?`
	args := []any{documentID}
	if len(events) > 0 {
		q += ` AND event IN (?` + strings.Repeat(`, ?`, len(events)-1) + `)`
		for _, e := range events {
			args = append(args, e)
		}
	}
	rows, err := s.store.db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		var detail string
		if err := rows.Scan(&e.ID, &e.Actor, &e.ActorKind, &e.Event, &detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Detail = json.RawMessage(detail)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DecisionRecord is one decided suggestion, as provenance shows it.
type DecisionRecord struct {
	SuggestionInfo
	Decision  string `json:"decision"`
	DecidedBy string `json:"decided_by"`
	DecidedAt string `json:"decided_at"`
}

// SectionProvenance says where a section came from and what was decided in
// it.
type SectionProvenance struct {
	UID    string `json:"uid"`
	Origin string `json:"origin"`
	// OriginLabel reads as a sentence fragment: "the ICT and Information
	// Security Policy template (1.0.0)", "added in the Studio", ...
	OriginLabel string           `json:"origin_label"`
	Decisions   []DecisionRecord `json:"decisions"`
}

// Provenance is who wrote what: each section's origin and every decided
// suggestion, with who suggested it, who decided it and when. Pending
// suggestions carry their authors in the document itself.
func (s *Service) Provenance(documentID int64) ([]SectionProvenance, error) {
	rows, err := s.policies.ListAllSections(documentID)
	if err != nil {
		return nil, err
	}
	events, err := s.auditEvents(documentID, EventSuggestionAccepted, EventSuggestionRejected)
	if err != nil {
		return nil, err
	}
	bySection := map[string][]DecisionRecord{}
	for _, e := range events {
		var rec DecisionRecord
		if json.Unmarshal(e.Detail, &rec.SuggestionInfo) != nil {
			continue
		}
		rec.Decision = "accepted"
		if e.Event == EventSuggestionRejected {
			rec.Decision = "rejected"
		}
		rec.DecidedBy, rec.DecidedAt = e.Actor, e.CreatedAt
		bySection[rec.SectionUID] = append(bySection[rec.SectionUID], rec)
	}
	out := make([]SectionProvenance, 0, len(rows))
	for _, r := range rows {
		p := SectionProvenance{UID: r.UID, Origin: r.Provenance, OriginLabel: s.originLabel(r), Decisions: bySection[r.UID]}
		if p.Decisions == nil {
			p.Decisions = []DecisionRecord{}
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Service) originLabel(r policydocs.Section) string {
	switch r.Provenance {
	case policydocs.ProvenanceTemplate:
		ref, _, _ := strings.Cut(r.ProvenanceDetail, "#")
		id, version, _ := strings.Cut(ref, "@")
		if t, ok := s.Template(id); ok {
			return "the " + t.Title + " template (" + version + ")"
		}
		return "template " + ref
	case policydocs.ProvenanceHuman:
		return "written by hand"
	}
	if r.Provenance == "" {
		return "unknown"
	}
	return r.Provenance
}

// ---- comments ----

// Thread is a comment thread anchored on a span of the live document.
type Thread struct {
	ID           int64     `json:"id"`
	SectionUID   string    `json:"section_uid"`
	AnchorStart  string    `json:"anchor_start"`
	AnchorEnd    string    `json:"anchor_end"`
	Quote        string    `json:"quote"`
	Visibility   string    `json:"visibility"`
	Kind         string    `json:"kind"`
	SuggestionID string    `json:"suggestion_id,omitempty"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    string    `json:"created_at"`
	UpdatedAt    string    `json:"updated_at"`
	ResolvedBy   string    `json:"resolved_by,omitempty"`
	ResolvedAt   string    `json:"resolved_at,omitempty"`
	Comments     []Comment `json:"comments"`
}

// Comment is one message in a thread. Bodies are plain text; the Studio
// renders them with textContent.
type Comment struct {
	ID         int64  `json:"id"`
	Author     string `json:"author"`
	AuthorKind string `json:"author_kind"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
}

// NewThread is what starting a thread takes.
type NewThread struct {
	SectionUID  string `json:"section_uid"`
	AnchorStart string `json:"anchor_start"`
	AnchorEnd   string `json:"anchor_end"`
	Quote       string `json:"quote"`
	Visibility  string `json:"visibility"`
	Body        string `json:"body"`
}

func validAnchor(a string) bool {
	if a == "" || len(a) > maxAnchorBytes*2 {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(a)
	return err == nil && len(raw) > 0 && len(raw) <= maxAnchorBytes
}

func commentBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", invalid("write something first")
	}
	if !utf8.ValidString(body) || utf8.RuneCountInString(body) > maxCommentRunes {
		return "", invalid("a comment is at most %d characters", maxCommentRunes)
	}
	return body, nil
}

// CreateThread starts a thread on a span of a section.
func (s *Service) CreateThread(documentID int64, in NewThread, actor string) (Thread, error) {
	return s.createThread(documentID, in, ThreadComment, "", actor, actor, AuthorHuman)
}

// CreateAIThread records the rationale of an AI edit as a thread anchored on
// the text it is about, linked to the edit's suggestion. The thread is started
// by the person who placed the proposal; its comment is the AI's.
func (s *Service) CreateAIThread(documentID int64, in NewThread, suid, aiLabel, placedBy string) (Thread, error) {
	return s.createThread(documentID, in, ThreadAIRationale, suid, placedBy, aiLabel, AuthorAI)
}

func (s *Service) createThread(documentID int64, in NewThread, kind, suid, actor, author, authorKind string) (Thread, error) {
	if _, err := s.reviewable(documentID, "comments can be added"); err != nil {
		return Thread{}, err
	}
	body, err := commentBody(in.Body)
	if err != nil {
		return Thread{}, err
	}
	if in.Visibility == "" {
		in.Visibility = VisibilityInternal
	}
	if in.Visibility != VisibilityInternal && in.Visibility != VisibilityShared {
		return Thread{}, invalid("visibility must be internal or shared")
	}
	if !validAnchor(in.AnchorStart) || !validAnchor(in.AnchorEnd) {
		return Thread{}, invalid("the comment's position in the text is missing or malformed; select the text again")
	}
	quote := strings.TrimSpace(in.Quote)
	if !utf8.ValidString(quote) {
		return Thread{}, invalid("the quoted text is not valid text")
	}
	if utf8.RuneCountInString(quote) > maxQuoteRunes {
		quote = string([]rune(quote)[:maxQuoteRunes]) + "…"
	}
	row, err := s.policies.GetSectionByUID(documentID, in.SectionUID)
	if err != nil || row.DetachedAt != "" {
		return Thread{}, invalid("comment on a section that is in the document")
	}
	stamp := s.store.stamp()
	tx, err := s.store.db.Begin()
	if err != nil {
		return Thread{}, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := tx.Insert(`INSERT INTO policy_comment_threads (document_id, section_uid, anchor_start, anchor_end, quote,
		visibility, kind, suggestion_suid, status, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		documentID, row.UID, in.AnchorStart, in.AnchorEnd, quote, in.Visibility, kind, suid, ThreadOpen, actor, stamp, stamp)
	if err != nil {
		return Thread{}, err
	}
	if _, err := tx.Exec(`INSERT INTO policy_comments (thread_id, author, author_kind, body, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, author, authorKind, body, stamp); err != nil {
		return Thread{}, err
	}
	if err := tx.Commit(); err != nil {
		return Thread{}, err
	}
	if err := s.audit(documentID, actor, EventThreadCreated, map[string]any{"thread_id": id, "section_uid": row.UID, "visibility": in.Visibility, "kind": kind}); err != nil {
		return Thread{}, err
	}
	return s.thread(documentID, id)
}

// Reply adds a comment to a thread; replying to a resolved thread reopens it.
func (s *Service) Reply(documentID, threadID int64, text, actor string) (Thread, error) {
	if _, err := s.reviewable(documentID, "comments can be added"); err != nil {
		return Thread{}, err
	}
	body, err := commentBody(text)
	if err != nil {
		return Thread{}, err
	}
	if _, err := s.thread(documentID, threadID); err != nil {
		return Thread{}, err
	}
	stamp := s.store.stamp()
	tx, err := s.store.db.Begin()
	if err != nil {
		return Thread{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO policy_comments (thread_id, author, author_kind, body, created_at) VALUES (?, ?, ?, ?, ?)`,
		threadID, actor, AuthorHuman, body, stamp); err != nil {
		return Thread{}, err
	}
	if _, err := tx.Exec(`UPDATE policy_comment_threads SET status = ?, resolved_by = '', resolved_at = '', updated_at = ? WHERE id = ?`,
		ThreadOpen, stamp, threadID); err != nil {
		return Thread{}, err
	}
	if err := tx.Commit(); err != nil {
		return Thread{}, err
	}
	if err := s.audit(documentID, actor, EventCommentAdded, map[string]any{"thread_id": threadID}); err != nil {
		return Thread{}, err
	}
	return s.thread(documentID, threadID)
}

// SetThreadStatus resolves or reopens a thread.
func (s *Service) SetThreadStatus(documentID, threadID int64, status, actor string) (Thread, error) {
	if status != ThreadOpen && status != ThreadResolved {
		return Thread{}, invalid("status must be open or resolved")
	}
	if _, err := s.reviewable(documentID, "threads can be resolved"); err != nil {
		return Thread{}, err
	}
	if _, err := s.thread(documentID, threadID); err != nil {
		return Thread{}, err
	}
	stamp := s.store.stamp()
	by, at, event := "", "", EventThreadReopened
	if status == ThreadResolved {
		by, at, event = actor, stamp, EventThreadResolved
	}
	if _, err := s.store.db.Exec(`UPDATE policy_comment_threads SET status = ?, resolved_by = ?, resolved_at = ?, updated_at = ? WHERE id = ? AND document_id = ?`,
		status, by, at, stamp, threadID, documentID); err != nil {
		return Thread{}, err
	}
	if err := s.audit(documentID, actor, event, map[string]any{"thread_id": threadID}); err != nil {
		return Thread{}, err
	}
	return s.thread(documentID, threadID)
}

func (s *Service) thread(documentID, threadID int64) (Thread, error) {
	threads, err := s.threads(documentID, AudienceInternal, threadID)
	if err != nil {
		return Thread{}, err
	}
	if len(threads) == 0 {
		return Thread{}, policydocs.ErrNotFound
	}
	return threads[0], nil
}

// Threads lists a document's threads for an audience. The filter is here, in
// the query, so no handler can forget it: an internal thread never leaves this
// function for AudienceShared.
func (s *Service) Threads(documentID int64, audience Audience) ([]Thread, error) {
	return s.threads(documentID, audience, 0)
}

func (s *Service) threads(documentID int64, audience Audience, only int64) ([]Thread, error) {
	q := `SELECT id, section_uid, anchor_start, anchor_end, quote, visibility, kind, suggestion_suid, status,
		created_by, created_at, updated_at, resolved_by, resolved_at FROM policy_comment_threads WHERE document_id = ?`
	args := []any{documentID}
	if audience != AudienceInternal {
		q += ` AND visibility = ?`
		args = append(args, VisibilityShared)
	}
	if only > 0 {
		q += ` AND id = ?`
		args = append(args, only)
	}
	rows, err := s.store.db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	out := []Thread{}
	index := map[int64]int{}
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.ID, &t.SectionUID, &t.AnchorStart, &t.AnchorEnd, &t.Quote, &t.Visibility, &t.Kind, &t.SuggestionID,
			&t.Status, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.ResolvedBy, &t.ResolvedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		t.Comments = []Comment{}
		index[t.ID] = len(out)
		out = append(out, t)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	crows, err := s.store.db.Query(`SELECT c.id, c.thread_id, c.author, c.author_kind, c.body, c.created_at
		FROM policy_comments c JOIN policy_comment_threads t ON t.id = c.thread_id
		WHERE t.document_id = ? ORDER BY c.id`, documentID)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var c Comment
		var threadID int64
		if err := crows.Scan(&c.ID, &threadID, &c.Author, &c.AuthorKind, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		if i, ok := index[threadID]; ok {
			out[i].Comments = append(out[i].Comments, c)
		}
	}
	return out, crows.Err()
}

// reviewVersion changes whenever a thread or a decision is recorded, so the
// Studio knows to fetch comments and provenance again.
func (s *Service) reviewVersion(documentID int64) (string, error) {
	var threads, audit int64
	var updated sql.NullString
	if err := s.store.db.QueryRow(`SELECT COUNT(*), MAX(updated_at) FROM policy_comment_threads WHERE document_id = ?`, documentID).Scan(&threads, &updated); err != nil {
		return "", err
	}
	var maxAudit sql.NullInt64
	if err := s.store.db.QueryRow(`SELECT COUNT(*), MAX(id) FROM policy_studio_audit WHERE document_id = ?`, documentID).Scan(&audit, &maxAudit); err != nil {
		return "", err
	}
	// AI edits placed as suggestions bring their rationale with them.
	var placed int64
	if err := s.store.db.QueryRow(`SELECT COUNT(*) FROM policy_ai_edits ed JOIN policy_ai_proposals p ON p.id = ed.proposal_id
		WHERE p.document_id = ? AND ed.placement = 'placed'`, documentID).Scan(&placed); err != nil {
		return "", err
	}
	return strconv.FormatInt(threads, 10) + ":" + updated.String + ":" + strconv.FormatInt(maxAudit.Int64, 10) + ":" + strconv.FormatInt(placed, 10), nil
}
