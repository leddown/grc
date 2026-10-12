package policydocs

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is returned when a referenced record does not exist.
var ErrNotFound = errors.New("policydocs: record not found")

// ValidationError marks a bad-input error so handlers can map it to HTTP 400.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// IsValidation reports whether err is a ValidationError.
func IsValidation(err error) bool {
	var ve ValidationError
	return errors.As(err, &ve)
}

// nowStamp and todayStamp are overridable in tests for deterministic output.
var nowStamp = func() string { return time.Now().UTC().Format(time.RFC3339) }
var todayStamp = func() time.Time { return time.Now().UTC() }

func today() string { return todayStamp().Format("2006-01-02") }

type Service struct {
	repo Repository
	// requireSeparateApprover refuses an approval by the document's own author.
	//
	// Off by default, and deliberately so: the app ships in single-user mode,
	// where the author is always the approver, and enabling this unconditionally
	// would make every document unapprovable. The app wires it on only when
	// multi-user authentication is active, so the control appears exactly when
	// there is somebody else who could do the approving.
	requireSeparateApprover bool

	hooks Hooks
}

// Hooks lets the Policy Studio take part in a document's lifecycle without
// this package knowing it exists. Every field is optional.
type Hooks struct {
	// BeforeTransition runs before a status change (approval included, before
	// its lint gate). The Studio flushes the live document and projects it
	// here, so the gate and the snapshot read what people actually wrote.
	// An error aborts the transition.
	BeforeTransition func(doc Document, to string) error
	// AfterTransition runs once the new status is stored. The Studio
	// disconnects the room's peers so they reconnect with the access the new
	// status allows.
	AfterTransition func(doc Document, to string)
	// TransitionAborted runs when BeforeTransition succeeded but the change
	// was then refused (the approval lint gate, a storage error), so the
	// Studio can give its peers write access back at once.
	TransitionAborted func(doc Document, to string)
	// AfterDelete runs once a document is deleted.
	AfterDelete func(id int64)
}

// SetHooks installs the lifecycle hooks. Call it at wiring time.
func (s *Service) SetHooks(h Hooks) { s.hooks = h }

func (s *Service) aborted(doc Document, to string) {
	if s.hooks.TransitionAborted != nil {
		s.hooks.TransitionAborted(doc, to)
	}
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// SetRequireSeparateApprover turns author/approver separation on or off. Call
// it at wiring time, before serving.
func (s *Service) SetRequireSeparateApprover(required bool) {
	s.requireSeparateApprover = required
}

// ---- Documents ----

func (s *Service) ListDocuments(f Filter) ([]Document, error) {
	f.Search = strings.TrimSpace(f.Search)
	f.DocType = strings.TrimSpace(f.DocType)
	f.Status = strings.TrimSpace(f.Status)
	f.Framework = strings.TrimSpace(f.Framework)
	return s.repo.ListDocuments(f)
}

func (s *Service) GetDocument(id int64) (Document, error) {
	d, err := s.repo.GetDocument(id)
	return d, mapNotFound(err)
}

// CreateDocument records a new draft. The Author field is taken from the
// caller — the handler fills it from the session — and is never accepted from
// the request body, since a self-approval check that the author can set is not
// a check at all.
func (s *Service) CreateDocument(d Document) (Document, error) {
	normalizeDocument(&d)
	// A new document always starts in draft regardless of what the caller
	// sent; the only way into "approved" is through Approve, which runs the
	// lint gate.
	d.Status = StatusDraft
	if err := s.validateDocument(0, d); err != nil {
		return Document{}, err
	}
	d.CreatedAt = nowStamp()
	d.UpdatedAt = d.CreatedAt
	d.NextReviewDate = computeNextReview(d.EffectiveDate, d.ReviewCadenceMonths)
	return s.repo.CreateDocument(d)
}

func (s *Service) UpdateDocument(id int64, d Document) (Document, error) {
	existing, err := s.repo.GetDocument(id)
	if err != nil {
		return Document{}, mapNotFound(err)
	}

	normalizeDocument(&d)
	if err := s.validateDocument(id, d); err != nil {
		return Document{}, err
	}
	// Status and Author are owned elsewhere: status by the transition endpoints,
	// author by creation. Letting an edit rewrite the author would let somebody
	// clear it and then self-approve.
	d.Status = existing.Status
	d.Author = existing.Author
	d.UpdatedAt = nowStamp()
	d.NextReviewDate = computeNextReview(d.EffectiveDate, d.ReviewCadenceMonths)

	updated, err := s.repo.UpdateDocument(id, d)
	return updated, mapNotFound(err)
}

func (s *Service) DeleteDocument(id int64) error {
	doc, err := s.repo.GetDocument(id)
	if err != nil {
		return mapNotFound(err)
	}
	// Deleting a parent would orphan its children's parent_document_id, which
	// SQLite will not catch (the column carries no foreign key, because 0 is a
	// legitimate "no parent" value).
	if doc.ChildCount > 0 {
		return invalid("cannot delete %q while %d document(s) list it as their parent", doc.Title, doc.ChildCount)
	}
	if doc.VersionCount > 0 {
		return invalid("cannot delete %q — it has %d approved version(s); retire it instead", doc.Title, doc.VersionCount)
	}
	if err := s.repo.DeleteDocument(id); err != nil {
		return mapNotFound(err)
	}
	if s.hooks.AfterDelete != nil {
		s.hooks.AfterDelete(id)
	}
	return nil
}

// ---- Status transitions ----

// allowedTransitions is the document lifecycle. Approval is deliberately the
// only edge that runs a gate, and there is no edge from draft straight to
// approved: review is not skippable.
var allowedTransitions = map[string][]string{
	StatusDraft:    {StatusInReview},
	StatusInReview: {StatusDraft, StatusApproved},
	StatusApproved: {StatusDraft, StatusRetired},
	StatusRetired:  {StatusDraft},
}

// CanTransition reports whether from -> to is a legal move.
func CanTransition(from, to string) bool {
	for _, allowed := range allowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// SubmitForReview moves a draft into review.
func (s *Service) SubmitForReview(id int64) (Document, error) {
	return s.transition(id, StatusInReview)
}

// ReturnToDraft reopens a document for editing. From "approved" this is the
// start of a revision: the previously approved version snapshot survives in
// policy_versions, so reopening never destroys the approved text.
func (s *Service) ReturnToDraft(id int64) (Document, error) {
	return s.transition(id, StatusDraft)
}

// Retire withdraws an approved document.
func (s *Service) Retire(id int64) (Document, error) {
	return s.transition(id, StatusRetired)
}

func (s *Service) transition(id int64, to string) (Document, error) {
	doc, err := s.repo.GetDocument(id)
	if err != nil {
		return Document{}, mapNotFound(err)
	}
	if doc.Status == to {
		return doc, nil
	}
	if !CanTransition(doc.Status, to) {
		return Document{}, invalid("cannot move a %s document to %s", doc.Status, to)
	}
	if s.hooks.BeforeTransition != nil {
		if err := s.hooks.BeforeTransition(doc, to); err != nil {
			return Document{}, err
		}
	}
	if err := s.repo.SetStatus(id, to, doc.NextReviewDate, nowStamp()); err != nil {
		s.aborted(doc, to)
		return Document{}, mapNotFound(err)
	}
	if s.hooks.AfterTransition != nil {
		s.hooks.AfterTransition(doc, to)
	}
	return s.GetDocument(id)
}

// ApprovalError reports an approval refused by the lint gate. It carries the
// blocking findings so the UI can show what to fix rather than just "invalid".
type ApprovalError struct {
	Findings []Finding
}

func (e ApprovalError) Error() string {
	return fmt.Sprintf("document has %d blocking issue(s)", len(e.Findings))
}

// IsApproval reports whether err is an ApprovalError.
func IsApproval(err error) bool {
	var ae ApprovalError
	return errors.As(err, &ae)
}

// Approve promotes a document under review to approved. It refuses if the lint
// gate finds a blocking defect, and records an immutable snapshot of the text as
// approved — that snapshot, not the live sections, is what "version 3 of this
// policy" means once someone edits it again.
func (s *Service) Approve(id int64, approvedBy, changeSummary string) (Document, error) {
	doc, err := s.repo.GetDocument(id)
	if err != nil {
		return Document{}, mapNotFound(err)
	}
	if doc.Status == StatusApproved {
		return doc, nil
	}
	if !CanTransition(doc.Status, StatusApproved) {
		return Document{}, invalid("cannot approve a %s document — submit it for review first", doc.Status)
	}

	approvedBy = strings.TrimSpace(approvedBy)
	if approvedBy == "" {
		return Document{}, invalid("approver is required to approve a document")
	}
	// Separation of duties. Only enforced when the app is configured for it and
	// an author was actually recorded — a document created before the column
	// existed, or under no-auth local mode, has no author and is not blocked by
	// a check it could never satisfy.
	if s.requireSeparateApprover && doc.Author != "" &&
		strings.EqualFold(strings.TrimSpace(doc.Author), approvedBy) {
		return Document{}, invalid(
			"%q authored this document and cannot also approve it; a different reviewer must approve",
			doc.Author)
	}
	if strings.TrimSpace(doc.EffectiveDate) == "" {
		return Document{}, invalid("effective date is required to approve a document")
	}
	if strings.TrimSpace(doc.OwnerRole) == "" {
		return Document{}, invalid("owner role is required to approve a document")
	}
	if s.hooks.BeforeTransition != nil {
		if err := s.hooks.BeforeTransition(doc, StatusApproved); err != nil {
			return Document{}, err
		}
	}
	approved := false
	defer func() {
		if !approved {
			s.aborted(doc, StatusApproved)
		}
	}()

	sections, err := s.repo.ListSections(id)
	if err != nil {
		return Document{}, err
	}
	refs, err := s.repo.ListControlRefsForDocument(id)
	if err != nil {
		return Document{}, err
	}
	if blocking := BlockingFindings(Lint(doc, sections, refs)); len(blocking) > 0 {
		return Document{}, ApprovalError{Findings: blocking}
	}

	next, err := s.repo.NextVersionNumber(id)
	if err != nil {
		return Document{}, err
	}
	stamp := nowStamp()
	version := Version{
		DocumentID:    id,
		VersionLabel:  "v" + strconv.Itoa(next) + ".0",
		ApprovedBy:    approvedBy,
		ApprovedAt:    stamp,
		ChangeSummary: strings.TrimSpace(changeSummary),
		// The snapshot carries the control mappings as well as the text: the
		// coverage claim is part of what was approved, which is why mapping is
		// a draft-only edit.
		Snapshot: RenderMarkdown(doc, sections, refs),
	}
	if version.ContentJSON, err = approvedContent(sections, refs); err != nil {
		return Document{}, err
	}
	version.SnapshotSHA256 = version.ContentHash()
	if _, err := s.repo.CreateVersion(version); err != nil {
		return Document{}, err
	}

	// Approval is what starts the review clock, so the next review date is
	// recomputed here rather than at edit time.
	nextReview := computeNextReview(doc.EffectiveDate, doc.ReviewCadenceMonths)
	if err := s.repo.SetStatus(id, StatusApproved, nextReview, stamp); err != nil {
		return Document{}, mapNotFound(err)
	}
	approved = true
	if s.hooks.AfterTransition != nil {
		s.hooks.AfterTransition(doc, StatusApproved)
	}
	return s.GetDocument(id)
}

// ---- Sections ----

func (s *Service) ListSections(documentID int64) ([]Section, error) {
	return s.repo.ListSections(documentID)
}

func (s *Service) CreateSection(sec Section) (Section, error) {
	doc, err := s.repo.GetDocument(sec.DocumentID)
	if err != nil {
		return Section{}, mapNotFound(err)
	}
	if err := requireEditable(doc); err != nil {
		return Section{}, err
	}

	normalizeSection(&sec)
	if err := validateSection(sec); err != nil {
		return Section{}, err
	}
	if sec.UID == "" {
		sec.UID = NewUID()
	}

	existing, err := s.repo.ListSections(sec.DocumentID)
	if err != nil {
		return Section{}, err
	}
	sec.Ordinal = len(existing)
	sec.UpdatedAt = nowStamp()
	return s.repo.CreateSection(sec)
}

// UpdateSection edits a section. documentID is checked against the section's
// own document so a section can never be moved between documents by guessing an
// id in the URL.
func (s *Service) UpdateSection(documentID, id int64, sec Section) (Section, error) {
	existing, err := s.repo.GetSection(id)
	if err != nil {
		return Section{}, mapNotFound(err)
	}
	if existing.DocumentID != documentID {
		return Section{}, ErrNotFound
	}
	doc, err := s.repo.GetDocument(existing.DocumentID)
	if err != nil {
		return Section{}, mapNotFound(err)
	}
	if err := requireEditable(doc); err != nil {
		return Section{}, err
	}

	normalizeSection(&sec)
	if err := validateSection(sec); err != nil {
		return Section{}, err
	}
	sec.Ordinal = existing.Ordinal
	sec.UpdatedAt = nowStamp()

	updated, err := s.repo.UpdateSection(id, sec)
	return updated, mapNotFound(err)
}

func (s *Service) DeleteSection(documentID, id int64) error {
	existing, err := s.repo.GetSection(id)
	if err != nil {
		return mapNotFound(err)
	}
	if existing.DocumentID != documentID {
		return ErrNotFound
	}
	doc, err := s.repo.GetDocument(existing.DocumentID)
	if err != nil {
		return mapNotFound(err)
	}
	if err := requireEditable(doc); err != nil {
		return err
	}
	if err := s.repo.DeleteSection(id); err != nil {
		return mapNotFound(err)
	}

	// Close the gap the delete left, so ordinals stay dense.
	remaining, err := s.repo.ListSections(existing.DocumentID)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(remaining))
	for _, sec := range remaining {
		ids = append(ids, sec.ID)
	}
	return s.repo.ReorderSections(existing.DocumentID, ids, nowStamp())
}

// ReorderSections applies a new section order. Every existing section must
// appear exactly once, so a stale client cannot silently drop one.
func (s *Service) ReorderSections(documentID int64, orderedIDs []int64) ([]Section, error) {
	doc, err := s.repo.GetDocument(documentID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if err := requireEditable(doc); err != nil {
		return nil, err
	}

	existing, err := s.repo.ListSections(documentID)
	if err != nil {
		return nil, err
	}
	if len(existing) != len(orderedIDs) {
		return nil, invalid("reorder must list all %d sections, got %d", len(existing), len(orderedIDs))
	}
	known := make(map[int64]bool, len(existing))
	for _, sec := range existing {
		known[sec.ID] = true
	}
	seen := make(map[int64]bool, len(orderedIDs))
	for _, id := range orderedIDs {
		if !known[id] {
			return nil, invalid("section %d does not belong to this document", id)
		}
		if seen[id] {
			return nil, invalid("section %d listed twice", id)
		}
		seen[id] = true
	}

	if err := s.repo.ReorderSections(documentID, orderedIDs, nowStamp()); err != nil {
		return nil, err
	}
	return s.repo.ListSections(documentID)
}

// ---- Policy Studio ----

// ListAllSections returns every section row, including detached ones.
func (s *Service) ListAllSections(documentID int64) ([]Section, error) {
	return s.repo.ListAllSections(documentID)
}

// GetSectionByUID resolves a Studio section uid within one document.
func (s *Service) GetSectionByUID(documentID int64, uid string) (Section, error) {
	sec, err := s.repo.GetSectionByUID(documentID, uid)
	return sec, mapNotFound(err)
}

// ApplyProjection stores a Studio projection in the section rows. It is only
// ever called with what the server computed from its own copy of the live
// document.
func (s *Service) ApplyProjection(documentID int64, sections []SectionProjection) (int, error) {
	return s.repo.ApplyProjection(documentID, sections, nowStamp())
}

// MarkStudio hands a document's text to the Policy Studio. It is one way: the
// section rows become a projection of the Studio document, and the per-section
// editor refuses to write them from then on.
func (s *Service) MarkStudio(id int64) (Document, error) {
	doc, err := s.repo.GetDocument(id)
	if err != nil {
		return Document{}, mapNotFound(err)
	}
	if doc.EditorFormat == EditorStudio {
		return doc, nil
	}
	if err := s.repo.SetEditorFormat(id, EditorStudio, nowStamp()); err != nil {
		return Document{}, mapNotFound(err)
	}
	return s.GetDocument(id)
}

// ---- Versions, lint, export ----

func (s *Service) ListVersions(documentID int64) ([]Version, error) {
	return s.repo.ListVersions(documentID)
}

// Lint returns the findings for a document without changing it, so the editor
// can show what would block approval before anyone tries.
func (s *Service) Lint(documentID int64) ([]Finding, error) {
	doc, err := s.repo.GetDocument(documentID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	sections, err := s.repo.ListSections(documentID)
	if err != nil {
		return nil, err
	}
	refs, err := s.repo.ListControlRefsForDocument(documentID)
	if err != nil {
		return nil, err
	}
	return Lint(doc, sections, refs), nil
}

// ExportMarkdown renders the live document.
func (s *Service) ExportMarkdown(documentID int64) (Document, string, error) {
	doc, sections, refs, err := s.renderInputs(documentID)
	if err != nil {
		return Document{}, "", err
	}
	return doc, RenderMarkdown(doc, sections, refs), nil
}

// ExportHTML renders the live document as a standalone page.
func (s *Service) ExportHTML(documentID int64) (Document, string, error) {
	doc, sections, refs, err := s.renderInputs(documentID)
	if err != nil {
		return Document{}, "", err
	}
	return doc, RenderHTML(doc, sections, refs), nil
}

func (s *Service) renderInputs(documentID int64) (Document, []Section, []ControlRef, error) {
	doc, err := s.repo.GetDocument(documentID)
	if err != nil {
		return Document{}, nil, nil, mapNotFound(err)
	}
	sections, err := s.repo.ListSections(documentID)
	if err != nil {
		return Document{}, nil, nil, err
	}
	refs, err := s.repo.ListControlRefsForDocument(documentID)
	if err != nil {
		return Document{}, nil, nil, err
	}
	return doc, sections, refs, nil
}

// ---- Helpers ----

// requireEditable refuses content edits to a document that is not in draft.
// Editing approved text in place would silently change what was approved, which
// is exactly what ISO 27001 clause 7.5 version control exists to prevent —
// reopen it to draft first, which keeps the approved snapshot intact.
func requireEditable(doc Document) error {
	if doc.Status == StatusDraft {
		return nil
	}
	return invalid("document is %s — return it to draft before editing its sections", doc.Status)
}

func normalizeDocument(d *Document) {
	d.DocType = strings.ToLower(strings.TrimSpace(d.DocType))
	d.Reference = strings.TrimSpace(d.Reference)
	d.Title = strings.TrimSpace(d.Title)
	d.OwnerRole = strings.TrimSpace(d.OwnerRole)
	d.Approver = strings.TrimSpace(d.Approver)
	d.Classification = strings.TrimSpace(d.Classification)
	d.EffectiveDate = strings.TrimSpace(d.EffectiveDate)
	d.Summary = strings.TrimSpace(d.Summary)

	if d.DocType == "" {
		d.DocType = TypePolicy
	}
	if d.Classification == "" {
		d.Classification = "Internal"
	}
	if d.ReviewCadenceMonths <= 0 {
		d.ReviewCadenceMonths = 12
	}
	d.AIPolicy = strings.TrimSpace(d.AIPolicy)
	if d.AIPolicy == "" {
		d.AIPolicy = AIPolicyInherit
	}

	cleaned := make([]string, 0, len(d.Frameworks))
	seen := make(map[string]bool, len(d.Frameworks))
	for _, f := range d.Frameworks {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		cleaned = append(cleaned, f)
	}
	d.Frameworks = cleaned
}

func (s *Service) validateDocument(id int64, d Document) error {
	if d.Title == "" {
		return invalid("title is required")
	}
	if !contains(DocTypes, d.DocType) {
		return invalid("unknown document type %q", d.DocType)
	}
	if !contains(Classifications, d.Classification) {
		return invalid("unknown classification %q", d.Classification)
	}
	if !contains(AIPolicies, d.AIPolicy) {
		return invalid("ai_policy must be inherit, local_only or off")
	}
	if d.ReviewCadenceMonths < 1 || d.ReviewCadenceMonths > 120 {
		return invalid("review cadence must be between 1 and 120 months")
	}
	if d.EffectiveDate != "" {
		if _, err := time.Parse("2006-01-02", d.EffectiveDate); err != nil {
			return invalid("effective date must be YYYY-MM-DD")
		}
	}
	if d.ParentDocumentID != 0 {
		if id != 0 && d.ParentDocumentID == id {
			return invalid("a document cannot be its own parent")
		}
		parent, err := s.repo.GetDocument(d.ParentDocumentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return invalid("parent document %d does not exist", d.ParentDocumentID)
			}
			return err
		}
		// The hierarchy only runs one way; a policy hanging off a work
		// instruction is how document sets become unnavigable.
		if tierOf(d.DocType) <= tierOf(parent.DocType) {
			return invalid("a %s cannot hang off a %s — parents must sit higher in the hierarchy",
				docTypeLabel(d.DocType), docTypeLabel(parent.DocType))
		}
		if id != 0 {
			if err := s.checkNoCycle(id, d.ParentDocumentID); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkNoCycle walks up from parentID looking for id. The tier rule above makes
// a cycle nearly impossible already, but "nearly" is doing real work there:
// documents can be re-typed after they are linked.
func (s *Service) checkNoCycle(id, parentID int64) error {
	seen := make(map[int64]bool)
	for cursor := parentID; cursor != 0; {
		if cursor == id {
			return invalid("parent selection would create a cycle")
		}
		if seen[cursor] {
			return invalid("existing document hierarchy contains a cycle at document %d", cursor)
		}
		seen[cursor] = true

		parent, err := s.repo.GetDocument(cursor)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		cursor = parent.ParentDocumentID
	}
	return nil
}

// tierOf ranks a document type by how specific it is. Guidelines and TRAs are
// deliberately at the bottom: both hang off something more authoritative.
func tierOf(docType string) int {
	switch docType {
	case TypePolicy:
		return 0
	case TypeStandard:
		return 1
	case TypeProcedure:
		return 2
	case TypeWorkInstruction:
		return 3
	default:
		return 4
	}
}

func normalizeSection(s *Section) {
	s.Heading = strings.TrimSpace(s.Heading)
	s.SectionKind = strings.ToLower(strings.TrimSpace(s.SectionKind))
	s.Provenance = strings.ToLower(strings.TrimSpace(s.Provenance))
	s.ProvenanceDetail = strings.TrimSpace(s.ProvenanceDetail)
	s.Body = strings.ReplaceAll(s.Body, "\r\n", "\n")

	if s.SectionKind == "" {
		s.SectionKind = KindStatements
	}
	if s.Provenance == "" {
		s.Provenance = ProvenanceHuman
	}
	if s.Heading == "" {
		s.Heading = SectionKindLabels[s.SectionKind]
	}
}

func validateSection(s Section) error {
	if !contains(SectionKinds, s.SectionKind) {
		return invalid("unknown section kind %q", s.SectionKind)
	}
	if !contains(Provenances, s.Provenance) {
		return invalid("unknown provenance %q", s.Provenance)
	}
	if s.Heading == "" {
		return invalid("section heading is required")
	}
	return nil
}

// computeNextReview adds the cadence to the effective date. It returns an empty
// string when there is no effective date, because a document that has never
// taken effect has no review clock to run.
func computeNextReview(effectiveDate string, cadenceMonths int) string {
	if strings.TrimSpace(effectiveDate) == "" || cadenceMonths <= 0 {
		return ""
	}
	parsed, err := time.Parse("2006-01-02", effectiveDate)
	if err != nil {
		return ""
	}
	return parsed.AddDate(0, cadenceMonths, 0).Format("2006-01-02")
}

func contains(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// approvedSection is one section as an approval records it.
type approvedSection struct {
	Heading     string                 `json:"heading"`
	SectionKind string                 `json:"section_kind"`
	Body        string                 `json:"body"`
	Blocks      []Block                `json:"blocks,omitempty"`
	Controls    []TemplateSectionClaim `json:"controls"`
}

// approvedContent is the structured form of what an approval approved: the
// same sections the Markdown snapshot renders, with their blocks and claims.
func approvedContent(sections []Section, refs []ControlRef) (string, error) {
	claims := map[int64][]TemplateSectionClaim{}
	for _, r := range refs {
		claims[r.SectionID] = append(claims[r.SectionID], TemplateSectionClaim{ControlID: r.ControlID, Coverage: r.Coverage})
	}
	out := make([]approvedSection, 0, len(sections))
	for _, s := range sections {
		c := claims[s.ID]
		if c == nil {
			c = []TemplateSectionClaim{}
		}
		out = append(out, approvedSection{Heading: s.Heading, SectionKind: s.SectionKind, Body: s.Body, Blocks: decodeBlocks(s.BlocksJSON), Controls: c})
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

// ContentHash is the SHA-256 of the version's snapshot and structured content
// together. It is stored at approval; recomputing it later and comparing shows
// whether an approved version is still the one that was approved.
func (v Version) ContentHash() string {
	sum := sha256.Sum256([]byte(v.Snapshot + "\n" + v.ContentJSON))
	return hex.EncodeToString(sum[:])
}

// Verify reports whether a stored version still matches its hash. A version
// approved before hashes were recorded has none and verifies as false.
func (v Version) Verify() bool {
	return v.SnapshotSHA256 != "" && v.ContentHash() == v.SnapshotSHA256
}
