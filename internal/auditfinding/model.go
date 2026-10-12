// Package auditfinding is the Audit Findings & Remediation module: the record
// of what an auditor, examiner or assessor found, what management agreed to do
// about it, and the evidence that the fix was validated before the finding was
// closed.
//
// The shape follows the published practice rather than inventing one — see
// AUDIT_FINDINGS.md for the sources. In short: a finding is written as
// criteria, condition, cause, effect and recommendation (GAO Yellow Book, IIA);
// management responds and commits to dated action plans with named owners; the
// finding is tracked to closure, with extensions and status changes recorded;
// and it closes only on independent validation (IIA Standard 15.2), or leaves
// the open list through a formal, expiring risk acceptance.
package auditfinding

// Finding statuses. Reopened counts as open everywhere; it is its own value so
// the register shows that a closed finding came back.
const (
	StatusDraft             = "Draft"
	StatusOpen              = "Open"
	StatusInRemediation     = "In Remediation"
	StatusPendingValidation = "Pending Validation"
	StatusClosed            = "Closed"
	StatusRiskAccepted      = "Risk Accepted"
	StatusReopened          = "Reopened"
)

var Statuses = []string{
	StatusDraft, StatusOpen, StatusInRemediation, StatusPendingValidation,
	StatusClosed, StatusRiskAccepted, StatusReopened,
}

var Severities = []string{"Critical", "High", "Medium", "Low"}

// severitySLADays is the default time allowed to remediate, used only when a
// finding is raised without a due date. These are the common industry defaults;
// a finding's own due date always wins.
var severitySLADays = map[string]int{
	"Critical": 30,
	"High":     90,
	"Medium":   180,
	"Low":      365,
}

var Sources = []string{
	"Internal Audit", "External Audit", "Regulatory Examination", "Certification Audit",
	"SOC 2 / Attestation", "Penetration Test", "Second Line Review", "Self-Identified",
}

// FindingTypes spans the two vocabularies findings actually arrive in: the
// control-deficiency grading of financial and internal audit, and the ISO 19011
// nonconformity grading of certification audits.
var FindingTypes = []string{
	"Control Deficiency", "Significant Deficiency", "Material Weakness",
	"Major Nonconformity", "Minor Nonconformity", "Observation", "Opportunity for Improvement",
}

// advisoryTypes allege no breached requirement, so they carry no obligation to
// agree an action plan and can be closed without one.
var advisoryTypes = map[string]bool{
	"Observation":                 true,
	"Opportunity for Improvement": true,
}

var DeficiencyTypes = []string{"Design", "Operating Effectiveness", "Documentation"}

var RootCauseCategories = []string{
	"People", "Process", "Technology", "Third Party", "Governance", "Resourcing",
}

const (
	ResponseAgree          = "Agree"
	ResponsePartiallyAgree = "Partially Agree"
	ResponseDisagree       = "Disagree"
)

var ManagementResponses = []string{ResponseAgree, ResponsePartiallyAgree, ResponseDisagree}

const (
	ActionNotStarted  = "Not Started"
	ActionInProgress  = "In Progress"
	ActionImplemented = "Implemented"
	ActionValidated   = "Validated"
	ActionCancelled   = "Cancelled"
)

var ActionStatuses = []string{ActionNotStarted, ActionInProgress, ActionImplemented, ActionValidated, ActionCancelled}

var ActionTypes = []string{"Corrective", "Preventive"}

type Finding struct {
	ID          int64  `json:"id"`
	Reference   string `json:"reference"`
	Title       string `json:"title"`
	Source      string `json:"source"`
	Engagement  string `json:"engagement"`
	Auditor     string `json:"auditor"`
	FindingType string `json:"finding_type"`
	Severity    string `json:"severity"`
	// DeficiencyType separates a control that was never designed to work from
	// one that was designed well and not operated — the fix is different.
	DeficiencyType string `json:"deficiency_type"`

	Criteria          string `json:"criteria"`
	Condition         string `json:"condition"`
	Cause             string `json:"cause"`
	Effect            string `json:"effect"`
	Recommendation    string `json:"recommendation"`
	RootCauseCategory string `json:"root_cause_category"`

	BusinessUnit string `json:"business_unit"`
	ProcessArea  string `json:"process_area"`
	Owner        string `json:"owner"`

	ManagementResponse     string `json:"management_response"`
	ManagementResponseText string `json:"management_response_text"`

	IdentifiedDate string `json:"identified_date"`
	ReportDate     string `json:"report_date"`
	// OriginalDueDate is fixed at the first commitment so slippage stays
	// visible however many times DueDate moves.
	OriginalDueDate string `json:"original_due_date"`
	DueDate         string `json:"due_date"`
	ExtensionCount  int    `json:"extension_count"`
	ExtensionReason string `json:"extension_reason"`
	Status          string `json:"status"`

	RepeatFinding  bool   `json:"repeat_finding"`
	PriorReference string `json:"prior_reference"`

	LinkedRisks    string `json:"linked_risks"`
	LinkedControls string `json:"linked_controls"`
	FrameworkRefs  string `json:"framework_refs"`

	ValidatedBy     string `json:"validated_by"`
	ValidationDate  string `json:"validation_date"`
	ValidationNotes string `json:"validation_notes"`
	ClosureEvidence string `json:"closure_evidence"`
	ClosedDate      string `json:"closed_date"`

	RiskAcceptedBy          string `json:"risk_accepted_by"`
	RiskAcceptanceRationale string `json:"risk_acceptance_rationale"`
	RiskAcceptanceExpiry    string `json:"risk_acceptance_expiry"`

	Notes     string `json:"notes"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`

	Actions []Action       `json:"actions"`
	History []HistoryEntry `json:"history,omitempty"`

	// Derived on read, never stored: they depend on today's date.
	DaysOpen    int    `json:"days_open"`
	Overdue     bool   `json:"overdue"`
	DaysOverdue int    `json:"days_overdue"`
	AgingBucket string `json:"aging_bucket"`
}

// Action is one step of the management action plan (a CAPA item).
type Action struct {
	ID            int64  `json:"id"`
	FindingID     int64  `json:"finding_id"`
	Ordinal       int    `json:"ordinal"`
	Reference     string `json:"reference"`
	Description   string `json:"description"`
	ActionType    string `json:"action_type"`
	Owner         string `json:"owner"`
	DueDate       string `json:"due_date"`
	Status        string `json:"status"`
	CompletedDate string `json:"completed_date"`
	Evidence      string `json:"evidence"`
	Overdue       bool   `json:"overdue"`
}

type HistoryEntry struct {
	ID        int64  `json:"id"`
	FindingID int64  `json:"finding_id"`
	At        string `json:"at"`
	Actor     string `json:"actor"`
	Event     string `json:"event"`
	FromValue string `json:"from_value"`
	ToValue   string `json:"to_value"`
	Note      string `json:"note"`
}

type Filter struct {
	Search      string
	Status      string
	Severity    string
	Source      string
	OverdueOnly bool
	OpenOnly    bool
}

// Summary is the dashboard an audit committee asks for: how many are open, how
// bad, how late, and whether the same problems keep coming back.
type Summary struct {
	Total             int            `json:"total"`
	Open              int            `json:"open"`
	Overdue           int            `json:"overdue"`
	PendingValidation int            `json:"pending_validation"`
	RiskAccepted      int            `json:"risk_accepted"`
	AcceptanceExpired int            `json:"acceptance_expired"`
	RepeatOpen        int            `json:"repeat_open"`
	Extended          int            `json:"extended"`
	ClosedLast90Days  int            `json:"closed_last_90_days"`
	AvgDaysToClose    int            `json:"avg_days_to_close"`
	ByStatus          map[string]int `json:"by_status"`
	OpenBySeverity    map[string]int `json:"open_by_severity"`
	OpenByAging       map[string]int `json:"open_by_aging"`
}

// Vocabulary is served to the page so its dropdowns and the server's
// validation read from one list.
type Vocabulary struct {
	Statuses            []string       `json:"statuses"`
	Severities          []string       `json:"severities"`
	SeveritySLADays     map[string]int `json:"severity_sla_days"`
	Sources             []string       `json:"sources"`
	FindingTypes        []string       `json:"finding_types"`
	DeficiencyTypes     []string       `json:"deficiency_types"`
	RootCauseCategories []string       `json:"root_cause_categories"`
	ManagementResponses []string       `json:"management_responses"`
	ActionStatuses      []string       `json:"action_statuses"`
	ActionTypes         []string       `json:"action_types"`
}

func Vocab() Vocabulary {
	return Vocabulary{
		Statuses:            Statuses,
		Severities:          Severities,
		SeveritySLADays:     severitySLADays,
		Sources:             Sources,
		FindingTypes:        FindingTypes,
		DeficiencyTypes:     DeficiencyTypes,
		RootCauseCategories: RootCauseCategories,
		ManagementResponses: ManagementResponses,
		ActionStatuses:      ActionStatuses,
		ActionTypes:         ActionTypes,
	}
}

func isOpenStatus(status string) bool {
	switch status {
	case StatusClosed, StatusRiskAccepted, StatusDraft:
		return false
	}
	return true
}
