package policydocs

import (
	"database/sql"
	"errors"
	"strings"
)

// maxControlSearchResults caps the picker. The catalog runs to ~1200 rows and
// the picker is a type-ahead, not a browser.
const maxControlSearchResults = 50

// ListControlRefsForDocument returns every control claim the document makes.
func (s *Service) ListControlRefsForDocument(documentID int64) ([]ControlRef, error) {
	if _, err := s.repo.GetDocument(documentID); err != nil {
		return nil, mapNotFound(err)
	}
	return s.repo.ListControlRefsForDocument(documentID)
}

// ListControlRefsForSection returns the claims made by one section.
func (s *Service) ListControlRefsForSection(documentID, sectionID int64) ([]ControlRef, error) {
	if _, err := s.sectionInDocument(documentID, sectionID); err != nil {
		return nil, err
	}
	return s.repo.ListControlRefsForSection(sectionID)
}

// SearchControls backs the editor's control picker.
func (s *Service) SearchControls(query string) ([]ControlOption, error) {
	return s.repo.SearchControls(strings.TrimSpace(query), maxControlSearchResults)
}

// AttachControl records that a section satisfies a catalog control.
//
// Mapping is only allowed while the document is in draft, for the same reason
// section text is: a coverage claim is a compliance assertion, and changing what
// a policy claims to satisfy without re-approving it is exactly the drift the
// approval workflow exists to catch. Adding a mapping to an approved document
// therefore costs a revision cycle — deliberately.
func (s *Service) AttachControl(documentID, sectionID int64, ref ControlRef) (ControlRef, error) {
	doc, err := s.repo.GetDocument(documentID)
	if err != nil {
		return ControlRef{}, mapNotFound(err)
	}
	if _, err := s.sectionInDocument(documentID, sectionID); err != nil {
		return ControlRef{}, err
	}
	if err := requireEditable(doc); err != nil {
		return ControlRef{}, err
	}

	ref.ControlID = strings.ToUpper(strings.TrimSpace(ref.ControlID))
	ref.Coverage = strings.ToLower(strings.TrimSpace(ref.Coverage))
	ref.Framework = strings.TrimSpace(ref.Framework)
	ref.Note = strings.TrimSpace(ref.Note)

	if ref.ControlID == "" {
		return ControlRef{}, invalid("control id is required")
	}
	if ref.Coverage == "" {
		ref.Coverage = CoverageFull
	}
	if !contains(CoverageLevels, ref.Coverage) {
		return ControlRef{}, invalid("unknown coverage level %q", ref.Coverage)
	}
	if ref.Framework != "" && !contains(Frameworks, ref.Framework) {
		return ControlRef{}, invalid("unknown framework %q", ref.Framework)
	}

	// The control has to exist now. Refs are allowed to go stale later (the
	// catalog is reseeded and controls can be deleted), but creating one
	// against a typo would produce a claim that never resolves.
	control, err := s.repo.LookupControl(ref.ControlID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ControlRef{}, invalid("control %q is not in the catalog", ref.ControlID)
		}
		return ControlRef{}, err
	}
	ref.ControlID = control.ControlID

	exists, err := s.repo.ControlRefExists(sectionID, ref.ControlID)
	if err != nil {
		return ControlRef{}, err
	}
	if exists {
		return ControlRef{}, invalid("this section already maps %s", ref.ControlID)
	}

	ref.SectionID = sectionID
	ref.CreatedAt = nowStamp()
	created, err := s.repo.CreateControlRef(ref)
	if err != nil {
		return ControlRef{}, err
	}
	s.touchDocument(documentID)
	return created, nil
}

// DetachControl removes a claim.
func (s *Service) DetachControl(documentID, sectionID, refID int64) error {
	doc, err := s.repo.GetDocument(documentID)
	if err != nil {
		return mapNotFound(err)
	}
	if _, err := s.sectionInDocument(documentID, sectionID); err != nil {
		return err
	}
	if err := requireEditable(doc); err != nil {
		return err
	}

	ref, err := s.repo.GetControlRef(refID)
	if err != nil {
		return mapNotFound(err)
	}
	if ref.SectionID != sectionID {
		return ErrNotFound
	}
	if err := s.repo.DeleteControlRef(refID); err != nil {
		return mapNotFound(err)
	}
	s.touchDocument(documentID)
	return nil
}

// Coverage builds the framework coverage matrix.
func (s *Service) Coverage(f CoverageFilter) (CoverageReport, error) {
	f.Baseline = strings.ToLower(strings.TrimSpace(f.Baseline))
	f.Family = strings.TrimSpace(f.Family)
	switch f.Baseline {
	case "", "low", "moderate", "high", "privacy":
	default:
		return CoverageReport{}, invalid("unknown baseline %q", f.Baseline)
	}
	return s.repo.Coverage(f)
}

// sectionInDocument resolves a section and verifies it belongs to the document,
// so an id guessed in a URL cannot reach another document's section.
func (s *Service) sectionInDocument(documentID, sectionID int64) (Section, error) {
	sec, err := s.repo.GetSection(sectionID)
	if err != nil {
		return Section{}, mapNotFound(err)
	}
	if sec.DocumentID != documentID {
		return Section{}, ErrNotFound
	}
	return sec, nil
}

// touchDocument bumps updated_at after a mapping change. Best-effort: failing to
// refresh a timestamp must not undo a mapping that already committed.
func (s *Service) touchDocument(documentID int64) {
	doc, err := s.repo.GetDocument(documentID)
	if err != nil {
		return
	}
	_ = s.repo.SetStatus(documentID, doc.Status, doc.NextReviewDate, nowStamp())
}
