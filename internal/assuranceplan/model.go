// Package assuranceplan is the Assurance Plans module: the annual plans a
// second-line ICT risk function builds and runs — a controls testing plan, a
// risk assessment cycle, a policy review plan, a resilience testing programme,
// a remediation roadmap — from design and committee approval through quarterly
// execution to the annual conclusion. See ASSURANCE_PLANS.md.
//
// A plan type is a template (data in templates/), not a code path: every type
// is the same record with different seeded steps, cycle and governance.
package assuranceplan

// Plan statuses. Approval is the only edge that runs a gate, and there is no
// edge from Draft to Approved.
const (
	StatusDraft    = "Draft"
	StatusInReview = "In review"
	StatusApproved = "Approved"
	StatusActive   = "Active"
	StatusClosed   = "Closed"
)

var Statuses = []string{StatusDraft, StatusInReview, StatusApproved, StatusActive, StatusClosed}

const (
	StepActivity  = "activity"
	StepMilestone = "milestone"
	StepOngoing   = "ongoing"
)

var StepTypes = []string{StepActivity, StepMilestone, StepOngoing}

const (
	StepNotStarted = "Not started"
	StepInProgress = "In progress"
	StepComplete   = "Complete"
	StepAtRisk     = "At risk"
	StepDelayed    = "Delayed"
)

var StepStatuses = []string{StepNotStarted, StepInProgress, StepComplete, StepAtRisk, StepDelayed}

// RACILetters are the values a RACI cell may hold. "A/R" is common practice
// for the one role that both does and owns an activity.
var RACILetters = []string{"R", "A", "C", "I", "A/R"}

type Plan struct {
	ID              int64  `json:"id"`
	Reference       string `json:"reference"`
	Title           string `json:"title"`
	PlanType        string `json:"plan_type"`
	PlanTypeName    string `json:"plan_type_name"`
	FunctionName    string `json:"function_name"`
	ClientProfileID int64  `json:"client_profile_id"`
	ClientName      string `json:"client_name"`

	PeriodStart   string `json:"period_start"`
	PeriodEnd     string `json:"period_end"`
	MobiliseStart string `json:"mobilise_start"`
	// ClosingDate is when the annual conclusion goes to the approval forum,
	// usually weeks after the period ends.
	ClosingDate string `json:"closing_date"`

	Objective          string `json:"objective"`
	DecisionsRequested string `json:"decisions_requested"`
	RegulatoryRefs     string `json:"regulatory_refs"`

	OwnerRole       string `json:"owner_role"`
	AccountableRole string `json:"accountable_role"`
	ApprovalForum   string `json:"approval_forum"`

	Status string `json:"status"`
	// Revision is the design revision being worked on. A change request on an
	// Approved or Active plan opens the next one in RevisionStatus, so the
	// plan's own status never moves backwards while testing continues.
	Revision       int    `json:"revision"`
	RevisionStatus string `json:"revision_status"`
	RevisionAuthor string `json:"revision_author"`
	ChangeReason   string `json:"change_reason"`
	Author         string `json:"author"`
	ApprovedBy     string `json:"approved_by"`
	ApprovedAt     string `json:"approved_at"`
	ActivatedAt    string `json:"activated_at"`
	ClosedAt       string `json:"closed_at"`

	SelectionSeed   string     `json:"selection_seed"`
	NonWorkingDates string     `json:"non_working_dates"`
	RACIRoles       []string   `json:"raci_roles"`
	Parameters      Parameters `json:"parameters"`

	AIGenerated bool   `json:"ai_generated"`
	Model       string `json:"model"`
	PromptHash  string `json:"prompt_hash"`

	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`

	Steps       []Step       `json:"steps"`
	Quarters    []Quarter    `json:"quarters"`
	RACI        []RACIRow    `json:"raci"`
	Cadence     []Cadence    `json:"cadence"`
	Escalations []Escalation `json:"escalations"`

	// Derived on read.
	DesignLocked  bool   `json:"design_locked"`
	VersionCount  int    `json:"version_count"`
	LatestVersion string `json:"latest_version"`
}

// Parameters are the per-plan thresholds. They are stored on the plan rather
// than in code so a plan approved with one set keeps it.
type Parameters struct {
	TesterFTE            float64 `json:"tester_fte"`
	DaysPerFTEQuarter    float64 `json:"days_per_fte_quarter"`
	UtilisationThreshold float64 `json:"utilisation_threshold"`
	RAGAmberPoints       float64 `json:"rag_amber_points"`
	ResultPartialMin     int     `json:"result_partial_min"`
	ResultIneffectiveMin int     `json:"result_ineffective_min"`
	DeviationExtendAt    int     `json:"deviation_extend_at"`
	DeviationThematicAt  int     `json:"deviation_thematic_at"`
	ExtendPct            float64 `json:"extend_pct"`
	EvidenceSLADays      int     `json:"evidence_sla_days"`
}

func DefaultParameters() Parameters {
	return Parameters{
		TesterFTE:            2.0,
		DaysPerFTEQuarter:    50,
		UtilisationThreshold: 0.85,
		RAGAmberPoints:       10,
		ResultPartialMin:     1,
		ResultIneffectiveMin: 3,
		DeviationExtendAt:    1,
		DeviationThematicAt:  2,
		ExtendPct:            50,
		EvidenceSLADays:      10,
	}
}

type Step struct {
	ID              int64  `json:"id"`
	Ordinal         int    `json:"ordinal"`
	StepRef         string `json:"step_ref"`
	Phase           string `json:"phase"`
	Title           string `json:"title"`
	StepType        string `json:"step_type"`
	ResponsibleRole string `json:"responsible_role"`
	AccountableRole string `json:"accountable_role"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	Deliverable     string `json:"deliverable"`
	Refs            string `json:"refs"`
	Status          string `json:"status"`
	// Quarter is set on steps generated from a quarter's cycle, 0 otherwise.
	Quarter   int    `json:"quarter"`
	Generated bool   `json:"generated"`
	Notes     string `json:"notes"`
}

type Quarter struct {
	ID               int64   `json:"id"`
	Quarter          int     `json:"quarter"`
	LOD1WindowStart  string  `json:"lod1_window_start"`
	LOD1Deadline     string  `json:"lod1_deadline"`
	SelectionDate    string  `json:"selection_date"`
	FieldworkStart   string  `json:"fieldwork_start"`
	FieldworkEnd     string  `json:"fieldwork_end"`
	FindingsAgreedBy string  `json:"findings_agreed_by"`
	ReportDate       string  `json:"report_date"`
	CommitteeLabel   string  `json:"committee_label"`
	RetestDays       float64 `json:"retest_days"`
	OverheadDays     float64 `json:"overhead_days"`
	// SelectedAt marks the quarter's sample as frozen. It is never set from a
	// request: only the selection action writes it.
	SelectedAt string `json:"selected_at"`
	SelectedBy string `json:"selected_by"`
}

type RACIRow struct {
	ID       int64             `json:"id"`
	Ordinal  int               `json:"ordinal"`
	Activity string            `json:"activity"`
	Cells    map[string]string `json:"cells"`
}

type Cadence struct {
	ID        int64  `json:"id"`
	Ordinal   int    `json:"ordinal"`
	Forum     string `json:"forum"`
	Frequency string `json:"frequency"`
	Content   string `json:"content"`
	Owner     string `json:"owner"`
}

type Escalation struct {
	ID         int64  `json:"id"`
	Ordinal    int    `json:"ordinal"`
	Trigger    string `json:"trigger"`
	EscalateTo string `json:"escalate_to"`
	Within     string `json:"within"`
}

type HistoryEntry struct {
	ID        int64  `json:"id"`
	PlanID    int64  `json:"plan_id"`
	At        string `json:"at"`
	Actor     string `json:"actor"`
	Event     string `json:"event"`
	Subject   string `json:"subject"`
	FromValue string `json:"from_value"`
	ToValue   string `json:"to_value"`
	Note      string `json:"note"`
}

// Version is an approval snapshot. It is written once and never updated.
type Version struct {
	ID             int64  `json:"id"`
	PlanID         int64  `json:"plan_id"`
	Number         int    `json:"number"`
	Label          string `json:"label"`
	ApprovedBy     string `json:"approved_by"`
	ApprovedAt     string `json:"approved_at"`
	ChangeReason   string `json:"change_reason"`
	SnapshotJSON   string `json:"snapshot_json,omitempty"`
	SnapshotSHA256 string `json:"snapshot_sha256"`
}

type Filter struct {
	PlanType string
	Status   string
	Year     string
	ClientID int64
	Search   string
}

type Client struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Governance is the three governance lists, saved together because the page
// edits them as one tab.
type Governance struct {
	RACIRoles   []string     `json:"raci_roles"`
	RACI        []RACIRow    `json:"raci"`
	Cadence     []Cadence    `json:"cadence"`
	Escalations []Escalation `json:"escalations"`
}

type Vocabulary struct {
	Statuses     []string `json:"statuses"`
	StepTypes    []string `json:"step_types"`
	StepStatuses []string `json:"step_statuses"`
	RACILetters  []string `json:"raci_letters"`
}

func Vocab() Vocabulary {
	return Vocabulary{Statuses: Statuses, StepTypes: StepTypes, StepStatuses: StepStatuses, RACILetters: RACILetters}
}

// designEditable reports whether the plan's design may change: before its
// first approval, or while a change request's revision is in draft.
func designEditable(p Plan) bool {
	if p.Status == StatusDraft {
		return true
	}
	return (p.Status == StatusApproved || p.Status == StatusActive) && p.RevisionStatus == StatusDraft
}
