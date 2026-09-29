package policystudio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/reearth/ygo/crdt"
	ygws "github.com/reearth/ygo/provider/websocket"

	"grc/internal/clientprofile"
	"grc/internal/policydocs"
)

// Structure changes -- add, remove, reorder, change kind, restore -- are
// explicit commands applied by the server to the live document. Typing and
// pasting in the editor cannot create, delete, merge or split a section (the
// editor refuses those transactions, and the projection ignores a section the
// server did not create), so policy_sections stays the system of record for
// which sections exist and what they claim.

const commandTimeout = 10 * time.Second

func invalid(format string, args ...any) error {
	return policydocs.ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// editableDraft loads a Studio draft for a structure command.
func (s *Service) editableDraft(documentID int64) (policydocs.Document, error) {
	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return doc, err
	}
	if doc.EditorFormat != policydocs.EditorStudio {
		return doc, invalid("open this document in the Studio first")
	}
	if doc.Status != policydocs.StatusDraft {
		return doc, invalid("the document is %s; return it to draft to change its sections", doc.Status)
	}
	return doc, nil
}

// apply runs fn against the live document's fragment in one transaction and
// then projects, so the rows reflect the change before the command returns.
func (s *Service) apply(documentID int64, fn func(frag *crdt.YXmlFragment, txn *crdt.Transaction) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var inner error
	err := s.collab.Apply(ctx, RoomName(documentID), func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
		frag := doc.GetXmlFragment(Fragment)
		transact(func(txn *crdt.Transaction) { inner = fn(frag, txn) })
	})
	if errors.Is(err, ygws.ErrNoChanges) {
		err = nil
	}
	if err == nil {
		err = inner
	}
	if err != nil {
		return err
	}
	return s.Project(documentID)
}

// sectionIndex finds a section element by uid; it must run inside a
// transaction, where reading the fragment is safe.
func sectionIndex(frag *crdt.YXmlFragment, uid string) (int, *crdt.YXmlElement) {
	for i, c := range frag.Children() {
		if el, ok := c.(*crdt.YXmlElement); ok {
			if v, _ := el.GetAttribute("uid"); v == uid {
				return i, el
			}
		}
	}
	return -1, nil
}

// AddSection creates a section row and its node, after afterUID (at the end
// when afterUID is empty or unknown).
func (s *Service) AddSection(documentID int64, afterUID, kind, heading string) (policydocs.Section, error) {
	if _, err := s.editableDraft(documentID); err != nil {
		return policydocs.Section{}, err
	}
	row, err := s.policies.CreateSection(policydocs.Section{
		DocumentID: documentID, UID: policydocs.NewUID(), Heading: heading, SectionKind: kind,
		Provenance: policydocs.ProvenanceHuman,
	})
	if err != nil {
		return policydocs.Section{}, err
	}
	node := &Node{Type: "policySection", Attrs: map[string]any{"uid": row.UID, "kind": row.SectionKind}, Content: []*Node{
		{Type: "sectionHeading", Attrs: map[string]any{"bid": newBlockID()}, Content: []*Node{{Type: "text", Text: row.Heading}}},
		{Type: "paragraph", Attrs: map[string]any{"bid": newBlockID()}},
	}}
	if err := Validate(node); err != nil {
		_ = s.policies.DeleteSection(documentID, row.ID)
		return policydocs.Section{}, err
	}
	err = s.apply(documentID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) error {
		at := frag.Len()
		if afterUID != "" {
			if i, _ := sectionIndex(frag, afterUID); i >= 0 {
				at = i + 1
			}
		}
		el, err := buildElement(node)
		if err != nil {
			return err
		}
		frag.InsertElement(txn, at, el)
		return nil
	})
	if err != nil {
		_ = s.policies.DeleteSection(documentID, row.ID)
		return policydocs.Section{}, err
	}
	return s.policies.GetSectionByUID(documentID, row.UID)
}

// DeleteSection removes a section's node and then its row, with the policy
// module's delete semantics: the row's control mappings go with it.
func (s *Service) DeleteSection(documentID int64, uid string) error {
	if _, err := s.editableDraft(documentID); err != nil {
		return err
	}
	row, err := s.policies.GetSectionByUID(documentID, uid)
	if err != nil {
		return err
	}
	attached, err := s.policies.ListSections(documentID)
	if err != nil {
		return err
	}
	if row.DetachedAt == "" && len(attached) <= 1 {
		return invalid("a document needs at least one section")
	}
	err = s.apply(documentID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) error {
		if i, _ := sectionIndex(frag, uid); i >= 0 {
			frag.Delete(txn, i, 1)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.policies.DeleteSection(documentID, row.ID)
}

// ReorderSections puts the sections in the given order, which must list every
// section in the document exactly once. Yjs has no move, so a section that
// changes place is re-created at its new position with the same content; only
// the sections that must move are touched.
func (s *Service) ReorderSections(documentID int64, uids []string) error {
	if _, err := s.editableDraft(documentID); err != nil {
		return err
	}
	// The moved sections are read from a private copy first: ygo's text reads
	// take the document's lock, which the transaction below already holds.
	data, err := s.currentState(documentID)
	if err != nil {
		return err
	}
	snapshot := crdt.New()
	if err := crdt.ApplyUpdateV1(snapshot, data, nil); err != nil {
		return err
	}
	byUID := map[string]*Node{}
	for _, r := range readSections(snapshot) {
		if r.Node != nil {
			byUID[r.Node.Attr("uid")] = r.Node
		}
	}
	return s.apply(documentID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) error {
		current := map[string]bool{}
		for _, c := range frag.Children() {
			if el, ok := c.(*crdt.YXmlElement); ok {
				uid, _ := el.GetAttribute("uid")
				current[uid] = true
			}
		}
		if len(uids) != len(current) {
			return invalid("the new order must list all %d sections", len(current))
		}
		seen := map[string]bool{}
		for _, u := range uids {
			if !current[u] || seen[u] || byUID[u] == nil {
				return invalid("the new order does not match the document's sections")
			}
			seen[u] = true
		}
		for target, uid := range uids {
			from, _ := sectionIndex(frag, uid)
			if from == target {
				continue
			}
			rebuilt, err := buildElement(byUID[uid])
			if err != nil {
				return err
			}
			frag.Delete(txn, from, 1)
			frag.InsertElement(txn, target, rebuilt)
		}
		return nil
	})
}

// SetSectionKind changes what a section is (purpose, scope, ...), which is
// what the lint gate's mandatory-section rule reads.
func (s *Service) SetSectionKind(documentID int64, uid, kind string) error {
	if _, err := s.editableDraft(documentID); err != nil {
		return err
	}
	if err := sectionKind(kind); err != nil {
		return invalid("unknown section kind %q", kind)
	}
	return s.apply(documentID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) error {
		_, el := sectionIndex(frag, uid)
		if el == nil {
			return invalid("no such section")
		}
		el.SetAttribute(txn, "kind", kind)
		return nil
	})
}

// RestoreSection puts a detached section's last projected content back at the
// end of the document.
func (s *Service) RestoreSection(documentID int64, uid string) error {
	if _, err := s.editableDraft(documentID); err != nil {
		return err
	}
	row, err := s.policies.GetSectionByUID(documentID, uid)
	if err != nil {
		return err
	}
	if row.DetachedAt == "" {
		return invalid("the section is not detached")
	}
	row.DetachedAt = ""
	root, err := DocumentFromRows([]policydocs.Section{row}, newBlockID)
	if err != nil {
		return err
	}
	return s.apply(documentID, func(frag *crdt.YXmlFragment, txn *crdt.Transaction) error {
		if i, _ := sectionIndex(frag, uid); i >= 0 {
			return nil
		}
		el, err := buildElement(root.Content[0])
		if err != nil {
			return err
		}
		frag.InsertElement(txn, frag.Len(), el)
		return nil
	})
}

// Migrate moves a Markdown document into the Studio, one way. The legacy
// sections are kept as a snapshot first; the Studio document itself is seeded
// from the rows when its room is first opened. Calling it again is a no-op.
func (s *Service) Migrate(documentID int64, actor string) (policydocs.Document, error) {
	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return doc, err
	}
	if doc.EditorFormat == policydocs.EditorStudio {
		return doc, nil
	}
	rows, err := s.policies.ListAllSections(documentID)
	if err != nil {
		return doc, err
	}
	if len(rows) == 0 {
		return doc, invalid("add a section before opening this document in the Studio")
	}
	legacy, err := json.Marshal(rows)
	if err != nil {
		return doc, err
	}
	if _, err := s.store.Snapshot(documentID, SnapshotPreMigration, SnapshotFormatLegacy, actor, legacy); err != nil {
		return doc, err
	}
	return s.policies.MarkStudio(documentID)
}

// SectionState is one section as the Studio shows it.
type SectionState struct {
	ID          int64                   `json:"id"`
	UID         string                  `json:"uid"`
	Heading     string                  `json:"heading"`
	Kind        string                  `json:"kind"`
	KindLabel   string                  `json:"kind_label"`
	Ordinal     int                     `json:"ordinal"`
	Detached    bool                    `json:"detached"`
	Controls    []policydocs.ControlRef `json:"controls"`
	Pending     int                     `json:"pending_suggestions"`
	Provenance  string                  `json:"provenance"`
	ProvDetails string                  `json:"provenance_detail,omitempty"`
	// Guidance is the template's note on what the section is for; shown in
	// the Studio only.
	Guidance string `json:"guidance,omitempty"`
}

// FactState is one client fact as the Studio's Facts panel shows it: every
// fact the text uses, and every fact the document's template declares.
type FactState struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
	ValueType   string `json:"value_type"`
	Value       string `json:"value"`
	Resolved    bool   `json:"resolved"`
	Used        bool   `json:"used"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// ClientState names the document's client profile.
type ClientState struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// State is what the Studio polls: everything about the document except its
// text, which travels over the collaboration socket.
type State struct {
	Document          policydocs.Document  `json:"document"`
	Sections          []SectionState       `json:"sections"`
	Findings          []policydocs.Finding `json:"findings"`
	Problems          []Problem            `json:"problems"`
	ProjectionVersion int                  `json:"projection_version"`
	PendingTotal      int                  `json:"pending_suggestions"`
	CanEdit           bool                 `json:"can_edit"`
	// SuggestOnly is set in review: edits are made as suggestions.
	SuggestOnly bool `json:"suggest_only"`
	// CanDecide and CanComment are what the caller may do in the review
	// layer; ReviewVersion changes when a thread or decision is recorded.
	CanDecide     bool             `json:"can_decide"`
	CanComment    bool             `json:"can_comment"`
	ReviewVersion string           `json:"review_version"`
	Role          string           `json:"role"`
	Transitioning bool             `json:"transitioning"`
	Client        *ClientState     `json:"client"`
	Facts         []FactState      `json:"facts"`
	Template      *TemplateSummary `json:"template"`
	// Guest is set on a guest's state: who they are and what their link allows.
	Guest *GuestInfo `json:"guest,omitempty"`
}

// GuestInfo is a guest's own session, as their page shows it.
type GuestInfo struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	AllowAI   bool   `json:"allow_ai"`
	ExpiresAt string `json:"expires_at"`
}

// GuestState is the document's state reduced to what a guest may see: its
// text structure and status, the client facts the text uses, and what the
// guest may do. Lint, control mappings, template guidance, provenance and every
// count of internal review activity are left out.
func (s *Service) GuestState(g GuestSession) (State, error) {
	st, _, err := s.State(g.DocumentID, Identity{})
	if err != nil {
		return State{}, err
	}
	st.Findings, st.Problems, st.Template = []policydocs.Finding{}, []Problem{}, nil
	d := st.Document
	st.Document = policydocs.Document{ID: d.ID, Title: d.Title, Reference: d.Reference, DocType: d.DocType, Status: d.Status,
		Classification: d.Classification, OwnerRole: d.OwnerRole, EffectiveDate: d.EffectiveDate, ClientName: d.ClientName,
		EditorFormat: d.EditorFormat, Frameworks: d.Frameworks, ProjectionVersion: d.ProjectionVersion}
	facts := make([]FactState, 0, len(st.Facts))
	for _, f := range st.Facts {
		if f.Used {
			facts = append(facts, FactState{Key: f.Key, Label: f.Label, ValueType: f.ValueType, Value: f.Value, Resolved: f.Resolved, Used: true})
		}
	}
	st.Facts = facts
	for i := range st.Sections {
		sec := &st.Sections[i]
		sec.Controls, sec.Guidance, sec.Provenance, sec.ProvDetails = []policydocs.ControlRef{}, "", "", ""
	}
	st.Role = "guest_" + g.Role
	st.CanEdit = g.Role == RoleEditor && d.Status == policydocs.StatusDraft && !st.Transitioning
	st.SuggestOnly, st.CanDecide = false, false
	st.CanComment = g.CanComment() && projected(d.Status)
	if st.ReviewVersion, err = s.sharedReviewVersion(g.DocumentID); err != nil {
		return State{}, err
	}
	st.Guest = &GuestInfo{Name: g.DisplayName, Role: g.Role, AllowAI: g.AllowAI, ExpiresAt: g.ExpiresAt}
	return st, nil
}

// TemplateSummary names the template a document came from.
type TemplateSummary struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Title   string `json:"title"`
}

// State assembles the document's state for one viewer. The second result is
// an ETag for it.
func (s *Service) State(documentID int64, id Identity) (State, string, error) {
	doc, err := s.policies.GetDocument(documentID)
	if err != nil {
		return State{}, "", err
	}
	st := State{Document: doc, ProjectionVersion: doc.ProjectionVersion, Role: "reader", Findings: []policydocs.Finding{}, Problems: s.Problems(documentID)}
	if id.Admin {
		st.Role = "admin"
	}
	st.Transitioning = s.transitioning(documentID)
	studio := doc.EditorFormat == policydocs.EditorStudio
	st.CanEdit = id.Admin && studio && projected(doc.Status) && !st.Transitioning
	st.SuggestOnly = st.CanEdit && doc.Status == policydocs.StatusInReview
	st.CanDecide = st.CanEdit
	st.CanComment = id.Admin && studio && projected(doc.Status)
	if st.ReviewVersion, err = s.reviewVersion(documentID); err != nil {
		return State{}, "", err
	}

	rows, err := s.policies.ListAllSections(documentID)
	if err != nil {
		return State{}, "", err
	}
	refs, err := s.policies.ListControlRefsForDocument(documentID)
	if err != nil {
		return State{}, "", err
	}
	bySection := map[int64][]policydocs.ControlRef{}
	for _, r := range refs {
		bySection[r.SectionID] = append(bySection[r.SectionID], r)
	}
	st.Sections = make([]SectionState, 0, len(rows))
	usedKeys := map[string]bool{}
	for _, r := range rows {
		sec := SectionState{
			ID: r.ID, UID: r.UID, Heading: r.Heading, Kind: r.SectionKind, KindLabel: policydocs.SectionKindLabels[r.SectionKind],
			Ordinal: r.Ordinal, Detached: r.DetachedAt != "", Controls: bySection[r.ID], Provenance: r.Provenance,
			ProvDetails: r.ProvenanceDetail, Guidance: s.guidanceFor(r),
		}
		if sec.Controls == nil {
			sec.Controls = []policydocs.ControlRef{}
		}
		if r.ContentJSON != "" {
			var n Node
			if json.Unmarshal([]byte(r.ContentJSON), &n) == nil {
				if r.DetachedAt == "" {
					sec.Pending = policydocs.PendingSuggestions(r.ContentJSON)
					st.PendingTotal += sec.Pending
					collectFactKeys(&n, usedKeys)
				}
			}
		}
		st.Sections = append(st.Sections, sec)
	}
	findings, err := s.policies.Lint(documentID)
	if err != nil {
		return State{}, "", err
	}
	st.Findings = append(findings, problemFindings(st.Problems)...)
	if st.Facts, st.Client, err = s.factState(doc, usedKeys); err != nil {
		return State{}, "", err
	}
	if t, ok := s.Template(doc.TemplateID); ok {
		st.Template = &TemplateSummary{ID: t.ID, Version: doc.TemplateVersion, Title: t.Title}
	}

	raw, err := json.Marshal(st)
	if err != nil {
		return State{}, "", err
	}
	sum := sha256.Sum256(raw)
	return st, `"` + hex.EncodeToString(sum[:16]) + `"`, nil
}

func collectFactKeys(n *Node, into map[string]bool) {
	if n.Type == "factToken" {
		into[n.Attr("key")] = true
	}
	for _, c := range n.Content {
		collectFactKeys(c, into)
	}
}

// factState lists the facts a document uses or its template declares, with
// their values from the document's client profile.
func (s *Service) factState(doc policydocs.Document, used map[string]bool) ([]FactState, *ClientState, error) {
	var client *ClientState
	recorded := map[string]clientprofile.Fact{}
	if s.clients != nil && doc.ClientProfileID > 0 {
		p, err := s.clients.Get(doc.ClientProfileID)
		if err == nil {
			client = &ClientState{ID: p.ID, Name: p.Name}
			if recorded, err = s.clients.Facts(p.ID); err != nil {
				return nil, nil, err
			}
		}
	}
	byKey := map[string]*FactState{}
	var order []string
	add := func(key string) *FactState {
		if f, ok := byKey[key]; ok {
			return f
		}
		f := &FactState{Key: key, Label: key, ValueType: "text"}
		byKey[key] = f
		order = append(order, key)
		return f
	}
	if t, ok := s.Template(doc.TemplateID); ok {
		for _, d := range t.Facts {
			f := add(d.Key)
			f.Label, f.Description, f.Example = d.Label, d.Description, d.Example
			if d.ValueType != "" {
				f.ValueType = d.ValueType
			}
		}
	}
	keys := make([]string, 0, len(used))
	for k := range used {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k).Used = true
	}
	out := make([]FactState, 0, len(order))
	for _, k := range order {
		f := byKey[k]
		if r, ok := recorded[k]; ok {
			f.Value, f.UpdatedBy, f.UpdatedAt = r.Value, r.UpdatedBy, r.UpdatedAt
			f.Resolved = strings.TrimSpace(r.Value) != ""
			if r.ValueType != "" {
				f.ValueType = r.ValueType
			}
		}
		out = append(out, *f)
	}
	return out, client, nil
}
