package policydocs

// TemplateExport is the render contract between this module and the document
// templates in templates/. It is a deliberate, explicit type rather than the
// Document/Section/ControlRef structs serialised directly, for two reasons:
//
//   - Internal fields have no business in a render payload. Row ids, ordinals
//     and provenance detail would leak into a client deliverable's data file.
//   - The templates read fixed field names. Renaming a JSON tag on Document
//     would silently produce PDFs with blank fields; a separate type makes the
//     contract something a change has to break on purpose.
//
// The shape is documented in DOCUMENT_TEMPLATES.md and exercised by
// templates/samples/policy-sample.json, which is a hand-written instance of
// exactly this structure.
type TemplateExport struct {
	Reference           string `json:"reference"`
	Title               string `json:"title"`
	DocType             string `json:"doc_type"`
	Status              string `json:"status"`
	Classification      string `json:"classification"`
	OwnerRole           string `json:"owner_role"`
	Approver            string `json:"approver"`
	ClientName          string `json:"client_name"`
	EffectiveDate       string `json:"effective_date"`
	ReviewCadenceMonths int    `json:"review_cadence_months"`
	NextReviewDate      string `json:"next_review_date"`
	Summary             string `json:"summary"`

	// Frameworks and the slices below are always non-nil so the templates can
	// iterate without a presence check — Typst distinguishes an empty array
	// from null, and null would fault the loop.
	Frameworks []string `json:"frameworks"`

	Sections []TemplateSection `json:"sections"`
	Controls []TemplateControl `json:"controls"`
	Versions []TemplateVersion `json:"versions"`
}

// TemplateSection is one rendered section plus the control claims made by it,
// which the template turns into the inline "Satisfies:" line.
type TemplateSection struct {
	Heading     string                 `json:"heading"`
	Body        string                 `json:"body"`
	SectionKind string                 `json:"section_kind"`
	Controls    []TemplateSectionClaim `json:"controls"`
}

// TemplateSectionClaim is the per-section form: just enough to render the
// inline note without repeating the catalog metadata carried by Controls.
type TemplateSectionClaim struct {
	ControlID string `json:"control_id"`
	Coverage  string `json:"coverage"`
}

// TemplateControl is a row of the document-level Control Mapping table.
type TemplateControl struct {
	ControlID      string `json:"control_id"`
	ControlName    string `json:"control_name"`
	Coverage       string `json:"coverage"`
	SectionHeading string `json:"section_heading"`
}

// TemplateVersion is a row of the revision history, newest first.
type TemplateVersion struct {
	VersionLabel  string `json:"version_label"`
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	ChangeSummary string `json:"change_summary"`
}

// ExportTemplateJSON assembles the render payload for one document.
func (s *Service) ExportTemplateJSON(documentID int64) (TemplateExport, error) {
	doc, sections, refs, err := s.renderInputs(documentID)
	if err != nil {
		return TemplateExport{}, err
	}
	versions, err := s.repo.ListVersions(documentID)
	if err != nil {
		return TemplateExport{}, err
	}
	return buildTemplateExport(doc, sections, refs, versions), nil
}

func buildTemplateExport(doc Document, sections []Section, refs []ControlRef, versions []Version) TemplateExport {
	claimsBySection := make(map[int64][]TemplateSectionClaim, len(refs))
	controls := make([]TemplateControl, 0, len(refs))
	for _, ref := range refs {
		claimsBySection[ref.SectionID] = append(claimsBySection[ref.SectionID], TemplateSectionClaim{
			ControlID: ref.ControlID,
			Coverage:  ref.Coverage,
		})
		name := ref.ControlName
		if !ref.Known {
			// Surfaced rather than blanked: a deliverable that silently omits a
			// stale mapping hides that the policy set drifted from the catalog.
			name = "(not in catalog)"
		}
		controls = append(controls, TemplateControl{
			ControlID:      ref.ControlID,
			ControlName:    name,
			Coverage:       ref.Coverage,
			SectionHeading: ref.SectionHeading,
		})
	}

	exportSections := make([]TemplateSection, 0, len(sections))
	for _, sec := range sections {
		claims := claimsBySection[sec.ID]
		if claims == nil {
			claims = []TemplateSectionClaim{}
		}
		exportSections = append(exportSections, TemplateSection{
			Heading:     sec.Heading,
			Body:        sec.Body,
			SectionKind: sec.SectionKind,
			Controls:    claims,
		})
	}

	exportVersions := make([]TemplateVersion, 0, len(versions))
	for _, v := range versions {
		exportVersions = append(exportVersions, TemplateVersion{
			VersionLabel:  v.VersionLabel,
			ApprovedBy:    v.ApprovedBy,
			ApprovedAt:    v.ApprovedAt,
			ChangeSummary: v.ChangeSummary,
		})
	}

	frameworks := doc.Frameworks
	if frameworks == nil {
		frameworks = []string{}
	}

	return TemplateExport{
		Reference:           doc.Reference,
		Title:               doc.Title,
		DocType:             doc.DocType,
		Status:              doc.Status,
		Classification:      doc.Classification,
		OwnerRole:           doc.OwnerRole,
		Approver:            doc.Approver,
		ClientName:          doc.ClientName,
		EffectiveDate:       doc.EffectiveDate,
		ReviewCadenceMonths: doc.ReviewCadenceMonths,
		NextReviewDate:      doc.NextReviewDate,
		Summary:             doc.Summary,
		Frameworks:          frameworks,
		Sections:            exportSections,
		Controls:            controls,
		Versions:            exportVersions,
	}
}
