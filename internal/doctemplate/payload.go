package doctemplate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PolicyPayload is this package's view of the render contract described in
// DOCUMENT_TEMPLATES.md and produced by policydocs.TemplateExport.
//
// It is redeclared here rather than imported because the dependency has to
// point the other way — see the package comment. The duplication is the price
// of that, and it is the right price: the contract is a published JSON shape
// with two independent consumers (this package and the Typst templates), so a
// second Go declaration of it is one more place a breaking change has to be
// made on purpose. TestPolicyPayloadMatchesSample decodes the committed sample
// through this type and asserts nothing was dropped.
type PolicyPayload struct {
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

	Frameworks []string        `json:"frameworks"`
	Sections   []PolicySection `json:"sections"`
	Controls   []PolicyControl `json:"controls"`
	Versions   []PolicyVersion `json:"versions"`
}

type PolicySection struct {
	Heading     string        `json:"heading"`
	Body        string        `json:"body"`
	SectionKind string        `json:"section_kind"`
	Controls    []PolicyClaim `json:"controls"`
}

type PolicyClaim struct {
	ControlID string `json:"control_id"`
	Coverage  string `json:"coverage"`
}

type PolicyControl struct {
	ControlID      string `json:"control_id"`
	ControlName    string `json:"control_name"`
	Coverage       string `json:"coverage"`
	SectionHeading string `json:"section_heading"`
}

type PolicyVersion struct {
	VersionLabel  string `json:"version_label"`
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	ChangeSummary string `json:"change_summary"`
}

// DecodePolicyPayload parses a render payload and rejects one that is not a
// policy document. The check is deliberately shallow — a title and at least one
// section — because the point is to catch a payload of the wrong shape reaching
// the wrong template, not to re-validate what policydocs already enforced.
func DecodePolicyPayload(data []byte) (PolicyPayload, error) {
	var payload PolicyPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return PolicyPayload{}, fmt.Errorf("parsing policy payload: %w", err)
	}
	if strings.TrimSpace(payload.Title) == "" {
		return PolicyPayload{}, fmt.Errorf("policy payload has no title — is this a policy document export?")
	}
	return payload, nil
}

// LatestVersionLabel is what the cover page prints as the version. A document
// with no approval history is a draft, which the Typst template also assumes.
func (p PolicyPayload) LatestVersionLabel() string {
	if len(p.Versions) > 0 && strings.TrimSpace(p.Versions[0].VersionLabel) != "" {
		return p.Versions[0].VersionLabel
	}
	return "Draft"
}

// DocTypeLabel turns the stored slug into the cover-page label, matching the
// titlecasing templates/typst/policy-document.typ does. A cover reading
// "work_instruction" is the kind of detail that makes a deliverable look
// generated rather than authored.
func (p PolicyPayload) DocTypeLabel() string {
	switch p.DocType {
	case "":
		return "Policy"
	case "work_instruction":
		return "Work Instruction"
	case "tra":
		return "Targeted Risk Analysis"
	default:
		return titleCase(p.DocType)
	}
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}
