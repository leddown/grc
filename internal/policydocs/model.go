// Package policydocs is the policy-authoring module: ISMS / ISO 27001 / NIST /
// FedRAMP / PCI DSS policy, standard, procedure and work-instruction documents
// written for client engagements.
//
// The shape follows the hierarchy every source on security documentation
// converges on — policy -> standard -> procedure -> work instruction — where
// volatility increases as you descend. A policy that names a tool, a port or a
// scan frequency has to be re-approved by management every time that detail
// changes, so those details belong further down the tree. Documents therefore
// link to a parent rather than nesting content, and the section set offered for
// a document depends on its type.
//
// See POLICY_MODULE_FRAMEWORK.md for the research this is built on. Phases 1
// and 2 of that plan are implemented: the document model, editor, document
// control and approval workflow, plus section-level mapping to the control
// catalog and the framework coverage matrix. The uploaded-document corpus, AI
// drafting and template rendering are later phases and are deliberately absent.
//
// Documents map to controls "author once, map many": a single Access Control
// policy satisfies ISO Annex A, NIST AC-1, FedRAMP's AC family policy and PCI
// DSS clauses simultaneously, so framework coverage is a query over the mapping
// table rather than four documents that drift apart within a year.
package policydocs

// Document types, ordered from least to most volatile.
const (
	TypePolicy          = "policy"
	TypeStandard        = "standard"
	TypeProcedure       = "procedure"
	TypeWorkInstruction = "work_instruction"
	TypeGuideline       = "guideline"
	// TypeTRA is a PCI DSS v4 Targeted Risk Analysis. It is a distinct
	// document type rather than a section inside a policy because it carries
	// its own review cycle.
	TypeTRA = "tra"
)

// DocTypes is the ordered set offered in the UI.
var DocTypes = []string{TypePolicy, TypeStandard, TypeProcedure, TypeWorkInstruction, TypeGuideline, TypeTRA}

// Lifecycle states.
const (
	StatusDraft    = "draft"
	StatusInReview = "in_review"
	StatusApproved = "approved"
	StatusRetired  = "retired"
)

// Statuses is the ordered set offered in the UI.
var Statuses = []string{StatusDraft, StatusInReview, StatusApproved, StatusRetired}

// Section kinds. The set is the union of what ISO 27001, the NIST 800-53 "-1"
// controls and FedRAMP expect to see in a policy; FedRAMP is the strictest and
// effectively dictates it (purpose, scope, roles, responsibilities, management
// commitment, coordination among organizational entities, compliance).
const (
	KindPurpose              = "purpose"
	KindScope                = "scope"
	KindStatements           = "statements"
	KindRoles                = "roles"
	KindManagementCommitment = "management_commitment"
	KindCoordination         = "coordination"
	KindCompliance           = "compliance"
	KindExceptions           = "exceptions"
	KindDefinitions          = "definitions"
	KindRelatedDocuments     = "related_documents"
	KindProcedureSteps       = "procedure_steps"
	KindOther                = "other"
)

// SectionKinds is the ordered set offered in the UI.
var SectionKinds = []string{
	KindPurpose, KindScope, KindStatements, KindRoles, KindManagementCommitment,
	KindCoordination, KindCompliance, KindExceptions, KindDefinitions,
	KindRelatedDocuments, KindProcedureSteps, KindOther,
}

// SectionKindLabels renders a kind for display.
var SectionKindLabels = map[string]string{
	KindPurpose:              "Purpose",
	KindScope:                "Scope",
	KindStatements:           "Policy Statements",
	KindRoles:                "Roles and Responsibilities",
	KindManagementCommitment: "Management Commitment",
	KindCoordination:         "Coordination Among Entities",
	KindCompliance:           "Compliance and Enforcement",
	KindExceptions:           "Exceptions",
	KindDefinitions:          "Definitions",
	KindRelatedDocuments:     "Related Documents",
	KindProcedureSteps:       "Procedure Steps",
	KindOther:                "Other",
}

// Provenance records where a section's text came from. Phase 1 only ever writes
// "human" or "template", but the field exists from the first version so that
// AI-drafted and imported text are distinguishable the day those phases land —
// retrofitting provenance onto existing rows would mean guessing.
const (
	ProvenanceHuman    = "human"
	ProvenanceTemplate = "template"
	ProvenanceAI       = "ai"
	ProvenanceImported = "imported"
)

// Provenances is the accepted set.
var Provenances = []string{ProvenanceHuman, ProvenanceTemplate, ProvenanceAI, ProvenanceImported}

// Classifications offered for the document control block.
var Classifications = []string{"Public", "Internal", "Confidential", "Restricted"}

// Frameworks a document can be declared against. A document maps to many:
// one Access Control policy typically satisfies ISO Annex A, NIST AC-1,
// FedRAMP's AC family policy and PCI DSS clauses at the same time, and
// authoring one document per framework produces copies that diverge.
var Frameworks = []string{"ISO 27001", "NIST 800-53", "NIST CSF", "FedRAMP", "PCI DSS", "SOC 2", "HIPAA"}

// Document is an authored policy-family document.
type Document struct {
	ID       int64 `json:"id"`
	ClientID int64 `json:"client_id"` // 0 = internal / unassigned
	// ClientName is stored on the document rather than resolved from a client
	// record. The CRM that held those records now lives in wintermute, and an
	// issued deliverable's cover page should not change because a client was
	// renamed after approval.
	ClientName          string   `json:"client_name"`
	DocType             string   `json:"doc_type"`
	Reference           string   `json:"reference"` // e.g. POL-AC-001
	Title               string   `json:"title"`
	Status              string   `json:"status"`
	OwnerRole           string   `json:"owner_role"` // a role, never a person
	Approver            string   `json:"approver"`
	Classification      string   `json:"classification"`
	Frameworks          []string `json:"frameworks"`
	EffectiveDate       string   `json:"effective_date"`
	ReviewCadenceMonths int      `json:"review_cadence_months"`
	NextReviewDate      string   `json:"next_review_date"`
	ParentDocumentID    int64    `json:"parent_document_id"`
	Summary             string   `json:"summary"`
	// Author is the identity that created the document. Recorded so approval can
	// refuse a self-approval; empty when the app runs without authentication.
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// Derived (read-only) fields populated by list/get queries.
	ParentTitle  string `json:"parent_title"`
	SectionCount int    `json:"section_count"`
	VersionCount int    `json:"version_count"`
	LatestLabel  string `json:"latest_version_label"`
	ChildCount   int    `json:"child_count"`
}

// Section is one block of a document. Body is authored text; phase 1 renders it
// preformatted rather than parsing Markdown, because the rendering pipeline
// belongs to the template module (phase 5) and a half-implemented Markdown
// parser here would have to be thrown away when it lands.
type Section struct {
	ID               int64  `json:"id"`
	DocumentID       int64  `json:"document_id"`
	Ordinal          int    `json:"ordinal"`
	Heading          string `json:"heading"`
	Body             string `json:"body"`
	SectionKind      string `json:"section_kind"`
	Provenance       string `json:"provenance"`
	ProvenanceDetail string `json:"provenance_detail"`
	UpdatedAt        string `json:"updated_at"`
}

// Version is an immutable snapshot taken at approval. The snapshot is the
// rendered Markdown of the document as approved, so the approved text survives
// later edits to the live sections — which is the whole point of ISO 27001
// clause 7.5 version control.
type Version struct {
	ID            int64  `json:"id"`
	DocumentID    int64  `json:"document_id"`
	VersionLabel  string `json:"version_label"`
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	ChangeSummary string `json:"change_summary"`
	Snapshot      string `json:"snapshot"`
}

// Coverage levels for a section-to-control mapping. The distinction matters in
// the coverage report: a control that only ever attracts "supporting" claims has
// no document actually asserting it, which reads as covered until someone looks.
const (
	// CoverageFull means this text satisfies the control on its own.
	CoverageFull = "full"
	// CoveragePartial means this text satisfies part of the control; something
	// else has to carry the rest.
	CoveragePartial = "partial"
	// CoverageSupporting means this text contributes context but does not
	// itself satisfy the control.
	CoverageSupporting = "supporting"
)

// CoverageLevels is the accepted set, strongest first.
var CoverageLevels = []string{CoverageFull, CoveragePartial, CoverageSupporting}

// ControlRef maps one document section to one catalog control. Mapping lives on
// the section rather than the document so the coverage report can point at the
// text that makes the claim, which is what an assessor asks for.
type ControlRef struct {
	ID        int64  `json:"id"`
	SectionID int64  `json:"section_id"`
	ControlID string `json:"control_id"`
	Framework string `json:"framework"`
	Coverage  string `json:"coverage"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`

	// Derived (read-only) fields joined from the control catalog and the
	// owning document.
	ControlName    string `json:"control_name"`
	ControlFamily  string `json:"control_family"`
	ControlType    string `json:"control_type"`
	SectionHeading string `json:"section_heading"`
	DocumentID     int64  `json:"document_id"`
	// Known is false when control_id no longer resolves in the catalog — the
	// control was deleted or renamed after the mapping was made.
	Known bool `json:"known"`
}

// ControlOption is a lightweight catalog row for the editor's control picker.
type ControlOption struct {
	ControlID   string `json:"control_id"`
	Name        string `json:"name"`
	Family      string `json:"family"`
	ControlType string `json:"control_type"`
	Baselines   string `json:"baselines"`
}

// Coverage statuses, in the order a report should worry about them.
const (
	// CoverageStatusApproved means at least one approved document claims the
	// control with full or partial coverage.
	CoverageStatusApproved = "approved"
	// CoverageStatusDraftOnly means the only claims come from documents that
	// are not approved. This is the status worth reporting on: it looks like
	// coverage in a spreadsheet and is worth nothing in an audit.
	CoverageStatusDraftOnly = "draft_only"
	// CoverageStatusSupportingOnly means every claim is "supporting", so
	// nothing actually asserts the control.
	CoverageStatusSupportingOnly = "supporting_only"
	// CoverageStatusUncovered means no document references the control.
	CoverageStatusUncovered = "uncovered"
)

// CoverageClaim is one document's claim against a control.
type CoverageClaim struct {
	DocumentID     int64  `json:"document_id"`
	Reference      string `json:"reference"`
	Title          string `json:"title"`
	DocStatus      string `json:"doc_status"`
	SectionID      int64  `json:"section_id"`
	SectionHeading string `json:"section_heading"`
	Coverage       string `json:"coverage"`
}

// CoverageRow is one catalog control and everything claiming it.
type CoverageRow struct {
	ControlID   string          `json:"control_id"`
	Name        string          `json:"name"`
	Family      string          `json:"family"`
	ControlType string          `json:"control_type"`
	Status      string          `json:"status"`
	Claims      []CoverageClaim `json:"claims"`
}

// CoverageReport is the framework coverage matrix — the artifact the module
// exists to produce. The counts, not the rows, are what a client reads first.
type CoverageReport struct {
	Baseline       string        `json:"baseline"`
	Family         string        `json:"family"`
	ClientID       int64         `json:"client_id"`
	TotalControls  int           `json:"total_controls"`
	Approved       int           `json:"approved"`
	DraftOnly      int           `json:"draft_only"`
	SupportingOnly int           `json:"supporting_only"`
	Uncovered      int           `json:"uncovered"`
	Rows           []CoverageRow `json:"rows"`
	// Orphans are mappings pointing at control ids the catalog no longer has.
	// They are reported rather than deleted: a stale claim is a finding, and
	// quietly dropping it would hide that the policy set drifted from the
	// catalog.
	Orphans []ControlRef `json:"orphans"`
}

// CoverageFilter narrows a coverage report.
type CoverageFilter struct {
	Baseline string // "", "low", "moderate", "high", "privacy"
	Family   string
	ClientID int64
	// IncludeEnhancements controls whether control enhancements are counted.
	// Off by default: at Moderate the enhancements outnumber the base controls
	// and drown the report.
	IncludeEnhancements bool
	// OnlyGaps limits rows to anything not approved-covered.
	OnlyGaps bool
}

// Filter narrows a document listing.
type Filter struct {
	ClientID  int64
	DocType   string
	Status    string
	Framework string
	Search    string
	DueReview bool // only documents at or past their next review date
}
