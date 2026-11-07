// Package assuranceplan is the Assurance Plans module: the annual plans a
// second-line ICT risk function builds and runs — a controls testing plan, a
// risk assessment cycle, a policy review plan, a resilience testing programme,
// a remediation roadmap — from design and committee approval through quarterly
// execution to the annual conclusion. See ASSURANCE_PLANS.md.
//
// A plan type is a template (data in templates/), not a code path: every type
// is the same record with different seeded steps, cycle and governance.
package assuranceplan

import "grc/internal/auditfinding"

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

// Selection bases. A coverage-only stratum (sampling off) selects every item
// of its wave as Full coverage, which is how one selection path serves the
// sampled and the coverage plan types.
const (
	BasisStatistical = "Statistical"
	BasisTopUp       = "Risk-based top-up"
	BasisFull        = "Full coverage"
)

var SelectionBases = []string{BasisStatistical, BasisTopUp, BasisFull}

// The 2LoD data-quality review of a 1LoD submission.
const (
	DQNotReviewed = "Not reviewed"
	DQInReview    = "In review"
	DQAccepted    = "Accepted"
	DQReturned    = "Returned to 1LoD"
)

var DQStatuses = []string{DQNotReviewed, DQInReview, DQAccepted, DQReturned}

// Assessment methods, as NIST SP 800-53A names them (examine, interview,
// test) in the terms audit practice uses.
const (
	MethodInspection    = "Inspection"
	MethodInquiry       = "Inquiry"
	MethodObservation   = "Observation"
	MethodReperformance = "Re-performance"
)

var Methods = []string{MethodInspection, MethodInquiry, MethodObservation, MethodReperformance}

// Attribute results. Blank is not yet assessed; N/A is not tested and does
// not count towards the tests performed.
const (
	ResultPass = "Pass"
	ResultFail = "Fail"
	ResultNA   = "N/A"
)

var Results = []string{ResultPass, ResultFail, ResultNA}

// A test's overall result, from its number of fails.
const (
	OverallNotStarted  = "Not started"
	OverallEffective   = "Effective"
	OverallPartial     = "Partially effective"
	OverallIneffective = "Ineffective"
)

var OverallResults = []string{OverallNotStarted, OverallEffective, OverallPartial, OverallIneffective}

// Deviation-rule conclusions per attribute and stratum, for the year.
const (
	ConclusionNotTested    = "Not tested"
	ConclusionEffective    = "No deviations / effective"
	ConclusionExtend       = "Deviation – extend sample"
	ConclusionNotEffective = "Not effective – thematic finding"
)

var Conclusions = []string{ConclusionNotTested, ConclusionEffective, ConclusionExtend, ConclusionNotEffective}

// Coverage bases: what counts an item as covered on the dashboard and in the
// reports. A controls testing or RCSA plan covers an item when 1LoD attests
// it; a resilience programme or remediation roadmap when its 2LoD test (the
// test of the system, the validation of the closure) is signed off by QA.
const (
	CoverageAttestation = "attestation"
	CoverageTests       = "tests"
)

var CoverageBases = []string{CoverageAttestation, CoverageTests}

// Population sources a plan can propose its population from. A stratum's
// Takes lists the values of the source it takes: document types for policies,
// severities for findings.
const (
	SourcePolicies = "policies"
	SourceFindings = "findings"
)

var PopulationSources = []string{SourcePolicies, SourceFindings}

// The source_kind written on a proposed item.
const (
	SourceKindPolicy  = "policy_document"
	SourceKindFinding = "audit_finding"
)

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

	// TestTypes is the vocabulary a test's type is chosen from; the first is
	// the default for a new test. CoverageBasis is CoverageAttestation or
	// CoverageTests. PopulationSource is empty, SourcePolicies or
	// SourceFindings.
	TestTypes        []string `json:"test_types"`
	CoverageBasis    string   `json:"coverage_basis"`
	PopulationSource string   `json:"population_source"`

	AIGenerated bool   `json:"ai_generated"`
	Model       string `json:"model"`
	PromptHash  string `json:"prompt_hash"`

	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`

	Steps       []Step       `json:"steps"`
	Quarters    []Quarter    `json:"quarters"`
	Strata      []Stratum    `json:"strata"`
	Attributes  []Attribute  `json:"attributes"`
	RACI        []RACIRow    `json:"raci"`
	Cadence     []Cadence    `json:"cadence"`
	Escalations []Escalation `json:"escalations"`
	Basis       []BasisRow   `json:"regulatory_basis"`

	// Derived on read.
	DesignLocked   bool   `json:"design_locked"`
	VersionCount   int    `json:"version_count"`
	LatestVersion  string `json:"latest_version"`
	PopulationSize int    `json:"population_size"`
	FrozenAt       string `json:"population_frozen_at"`
	FrozenBy       string `json:"population_frozen_by"`
	// SeedCommitment is the SHA-256 of the seed. Readers see it from the
	// freeze on; the seed itself only once the last quarter is selected, so
	// nobody outside the testers can work out a later quarter's picks while
	// that wave is still attesting.
	SeedCommitment string `json:"seed_commitment"`
	SeedRevealed   bool   `json:"seed_revealed"`
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

// Stratum is a subdivision of the population with its own sampling
// parameters. Waves are the share of the stratum due for 1LoD attestation in
// each quarter (fractions adding up to 1); top-ups are the planned risk-based
// tests per quarter on top of the statistical sample.
type Stratum struct {
	ID              int64      `json:"id"`
	Ordinal         int        `json:"ordinal"`
	Code            string     `json:"code"`
	Name            string     `json:"name"`
	Definition      string     `json:"definition"`
	SamplingEnabled bool       `json:"sampling_enabled"`
	Confidence      float64    `json:"confidence"`
	TolerableRate   float64    `json:"tolerable_rate"`
	FloorPct        float64    `json:"floor_pct"`
	EffortPerTest   float64    `json:"effort_per_test"`
	EffortPerReview float64    `json:"effort_per_review"`
	Waves           [4]float64 `json:"waves"`
	TopUps          [4]int     `json:"top_ups"`
	// PlannedSize sizes the stratum until its population is loaded, so the
	// sampling can be approved before the inventory extract is final.
	PlannedSize int `json:"planned_size"`
	// CycleYears above 1 makes the stratum a multi-year cycle (TLPT every
	// three years): an item is due in the period only when its last test is
	// that many years old, and until then it counts as covered.
	CycleYears int `json:"cycle_years"`
	// Takes is what the stratum takes from a proposed population: document
	// types for SourcePolicies, finding severities for SourceFindings.
	Takes []string `json:"takes"`
}

// PopulationItem is one item in scope. Membership, stratum and wave decide
// the sampling; the rest describes the item or records its execution.
type PopulationItem struct {
	ID             int64  `json:"id"`
	ItemRef        string `json:"item_ref"`
	Name           string `json:"name"`
	StratumCode    string `json:"stratum"`
	CIF            string `json:"cif"`
	BusinessLine   string `json:"business_line"`
	Hosting        string `json:"hosting"`
	InternetFacing bool   `json:"internet_facing"`
	Owner          string `json:"owner"`
	WaveQuarter    int    `json:"wave"`
	AttestedOn     string `json:"attested_on"`
	DQStatus       string `json:"dq_status"`
	DQReviewedOn   string `json:"dq_reviewed_on"`
	TopUpQuarter   int    `json:"topup_quarter"`
	TopUpRationale string `json:"topup_rationale"`
	// LastTestedOn is the item's last test before the plan, which starts the
	// clock of a multi-year stratum.
	LastTestedOn string `json:"last_tested_on"`
	SourceKind   string `json:"source_kind"`
	SourceRef    string `json:"source_ref"`

	// Derived on read from the selections.
	SelectedQuarter int    `json:"selected_quarter"`
	SelectionBasis  string `json:"selection_basis"`
}

// Selection is an item picked for 2LoD testing in a quarter. It is written
// once by the selection action and never recomputed.
type Selection struct {
	ID           int64  `json:"id"`
	Quarter      int    `json:"quarter"`
	PopulationID int64  `json:"population_id"`
	ItemRef      string `json:"item_ref"`
	ItemName     string `json:"item_name"`
	StratumCode  string `json:"stratum"`
	Basis        string `json:"basis"`
	// RankOrder is the item's place in its stratum's wave by rank hash
	// (1 = lowest hash); 0 for a top-up, which is chosen, not drawn.
	RankOrder   int    `json:"rank_order"`
	RankHash    string `json:"rank_hash"`
	PlanVersion int    `json:"plan_version"`
	SelectedAt  string `json:"selected_at"`
	SelectedBy  string `json:"selected_by"`
}

// Attribute is one line of the test script: what is tested, how, on what
// evidence, and what passes. It is copied onto the plan, so a catalog edit
// does not change an approved plan.
type Attribute struct {
	ID                int64    `json:"id"`
	Ordinal           int      `json:"ordinal"`
	AttrRef           string   `json:"attr_ref"`
	Domain            string   `json:"domain"`
	Requirement       string   `json:"requirement"`
	Procedure         string   `json:"procedure"`
	Method            string   `json:"method"`
	EvidenceRequested string   `json:"evidence_requested"`
	PassCriteria      string   `json:"pass_criteria"`
	AppliesTo         []string `json:"applies_to"`
	RegulatoryRefs    string   `json:"regulatory_refs"`
	ControlIDs        []string `json:"control_ids"`
	NFRKey            string   `json:"nfr_key"`
	AIGenerated       bool     `json:"ai_generated"`
	Model             string   `json:"model"`
	PromptHash        string   `json:"prompt_hash"`

	// Derived on read: the references that do not resolve against the
	// catalogs, and whether they could be checked at all.
	Unresolved []string `json:"unresolved_refs"`
	Checked    bool     `json:"refs_checked"`
}

// appliesTo reports whether the attribute is tested in a stratum. An empty
// list means every stratum.
func (a Attribute) appliesTo(code string) bool {
	if len(a.AppliesTo) == 0 {
		return true
	}
	for _, c := range a.AppliesTo {
		if c == code {
			return true
		}
	}
	return false
}

// Test is the 2LoD test of one selected item.
type Test struct {
	ID                   int64  `json:"id"`
	SelectionID          int64  `json:"selection_id"`
	TestRef              string `json:"test_ref"`
	TestType             string `json:"test_type"`
	Tester               string `json:"tester"`
	EvidenceRequestedOn  string `json:"evidence_requested_on"`
	EvidenceDueOn        string `json:"evidence_due_on"`
	EvidenceReceivedOn   string `json:"evidence_received_on"`
	FieldworkCompletedOn string `json:"fieldwork_completed_on"`
	EvidenceSummary      string `json:"evidence_summary"`
	QAReviewer           string `json:"qa_reviewer"`
	QADate               string `json:"qa_date"`
	QANote               string `json:"qa_note"`
	Notes                string `json:"notes"`
	CreatedAt            string `json:"created_at"`
	CreatedBy            string `json:"created_by"`
	UpdatedAt            string `json:"updated_at"`
	UpdatedBy            string `json:"updated_by"`

	Results []Result `json:"results"`

	// From the selection.
	Quarter     int    `json:"quarter"`
	ItemID      int64  `json:"population_id"`
	ItemRef     string `json:"item_ref"`
	ItemName    string `json:"item_name"`
	ItemOwner   string `json:"item_owner"`
	StratumCode string `json:"stratum"`
	Basis       string `json:"basis"`

	// Derived on read.
	Fails         int    `json:"fails"`
	Passes        int    `json:"passes"`
	NotApplicable int    `json:"not_applicable"`
	Tested        int    `json:"tested"`
	Pending       int    `json:"pending"`
	Overall       string `json:"overall"`
	EvidenceLate  bool   `json:"evidence_late"`
}

// Result is one attribute's outcome on one test.
type Result struct {
	ID          int64  `json:"id"`
	TestID      int64  `json:"test_id"`
	AttributeID int64  `json:"attribute_id"`
	AttrRef     string `json:"attr_ref"`
	Result      string `json:"result"`
	Note        string `json:"note"`
	FindingID   int64  `json:"finding_id"`
	FindingRef  string `json:"finding_ref"`
	Severity    string `json:"severity"`
}

// Thematic is a not-effective conclusion pushed to the risk register.
type Thematic struct {
	ID          int64  `json:"id"`
	AttributeID int64  `json:"attribute_id"`
	AttrRef     string `json:"attr_ref"`
	StratumCode string `json:"stratum"`
	RiskID      int64  `json:"risk_id"`
	RiskRef     string `json:"risk_ref"`
	Deviations  int    `json:"deviations"`
	Note        string `json:"note"`
	PushedAt    string `json:"pushed_at"`
	PushedBy    string `json:"pushed_by"`
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
// BasisRow is one regulatory requirement the plan answers, and how. It is
// what the committee pack's regulatory slide is made of.
type BasisRow struct {
	ID          int64  `json:"id"`
	Ordinal     int    `json:"ordinal"`
	Requirement string `json:"requirement"`
	Reference   string `json:"reference"`
	How         string `json:"how"`
}

// Governance is replaced as a whole. A request without regulatory_basis keeps
// the plan's rows rather than clearing them.
type Governance struct {
	RACIRoles   []string     `json:"raci_roles"`
	RACI        []RACIRow    `json:"raci"`
	Cadence     []Cadence    `json:"cadence"`
	Escalations []Escalation `json:"escalations"`
	Basis       []BasisRow   `json:"regulatory_basis"`
}

type Vocabulary struct {
	Statuses       []string `json:"statuses"`
	StepTypes      []string `json:"step_types"`
	StepStatuses   []string `json:"step_statuses"`
	RACILetters    []string `json:"raci_letters"`
	DQStatuses     []string `json:"dq_statuses"`
	SelectionBases []string `json:"selection_bases"`
	Methods        []string `json:"methods"`
	Results        []string `json:"results"`
	OverallResults []string `json:"overall_results"`
	Conclusions    []string `json:"conclusions"`
	Severities     []string `json:"severities"`
	CoverageBases  []string `json:"coverage_bases"`
	Sources        []string `json:"population_sources"`
}

func Vocab() Vocabulary {
	return Vocabulary{Statuses: Statuses, StepTypes: StepTypes, StepStatuses: StepStatuses, RACILetters: RACILetters,
		DQStatuses: DQStatuses, SelectionBases: SelectionBases, Methods: Methods, Results: Results,
		OverallResults: OverallResults, Conclusions: Conclusions, Severities: auditfinding.Severities,
		CoverageBases: CoverageBases, Sources: PopulationSources}
}

// designEditable reports whether the plan's design may change: before its
// first approval, or while a change request's revision is in draft.
func designEditable(p Plan) bool {
	if p.Status == StatusDraft {
		return true
	}
	return (p.Status == StatusApproved || p.Status == StatusActive) && p.RevisionStatus == StatusDraft
}
