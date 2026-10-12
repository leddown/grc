// Package crisisexercise plans, runs and reports the exercises that prove a
// financial entity can survive a bad day: a red-team detonation, the incident
// that follows, the moment someone has to say the word "crisis", the regulatory
// clocks that start with it, the board meeting, and the press statement.
//
// It exists because the parts of that arc are usually rehearsed separately and
// fail together. A SOC that handles the intrusion beautifully still fails if
// nobody classified the incident as major inside four hours; a board that makes
// the right call still fails if the call was made on information the crisis
// team never wrote down. This module holds the whole arc as one artefact so the
// seams are what gets tested.
//
// # Shape
//
// An Exercise carries Objectives, a Phase arc, and a Master Scenario Events
// List of Injects — the messages delivered to players on a clock. Play records
// a Response per inject, a Decision log, and the Clocks that the scenario's
// incident classification started. Afterwards the Findings and an immutable
// after-action Version.
//
// # Three rules shape the design
//
//   - Everything is referenceable. An objective, a phase, an inject, a decision
//     and a finding can each cite the controls, Security NFRs, regulation
//     clauses, policies, risks and authority sources it exercises — so "we
//     tested our incident response" becomes "we tested IR-4, IR-6, NFR-…, DORA
//     Art. 19 and our Crisis Management Policy §4, and here is where it failed".
//     See Reference.
//
//   - The regulator's clock is modelled, not narrated. A major-incident
//     classification under DORA starts a 4-hour, a 72-hour and a one-month
//     obligation, and the national NIS2 and GDPR clocks run alongside it on
//     different triggers. Clocks computes them from the scenario and the
//     exercise records whether each was met — which is the finding a supervisor
//     actually asks for.
//
//   - Nothing the model writes is presented as fact. A generated inject records
//     the model, the hash of the prompt it answered and its confidence, and a
//     generated reference is marked as the model's rather than a curator's.
package crisisexercise

import (
	"fmt"
	"strings"
)

// ---- exercise ----

// Exercise is one planned or delivered exercise. A re-run is a Clone, not a
// second run of the same row: an exercise that has been reported on cannot
// silently acquire a different set of observations.
type Exercise struct {
	ID int64 `json:"id"`
	// Reference is the human handle used in the after-action report and in any
	// supervisory correspondence about it ("CX-2026-03").
	Reference string `json:"reference"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`

	// Format is the exercise type, from the NIST SP 800-84 / ISO 22398
	// vocabulary plus the two testing formats DORA names (TLPT and purple
	// teaming). Kind derives from it.
	Format string `json:"format"`
	Kind   string `json:"kind"`
	// Audience is who is at the table, which is the single biggest driver of
	// how an inject should be written: a SOC analyst wants a SIEM alert, a
	// board member wants the consequence in euros and headlines.
	Audience string `json:"audience"`

	// EntityName, EntityType, Jurisdiction and Supervision fix the regulatory
	// perimeter. They are what Clocks reads to decide which authority is owed
	// what, and by when.
	EntityName   string `json:"entity_name"`
	EntityType   string `json:"entity_type"`
	Jurisdiction string `json:"jurisdiction"`
	Supervision  string `json:"supervision"`
	// CriticalFunctions are the critical or important functions in scope, one
	// per line. DORA scopes resilience testing by these, not by systems.
	CriticalFunctions string `json:"critical_functions"`

	// ThreatActor, ThreatNarrative and InitialVector are the intelligence the
	// scenario is built on. TIBER-EU insists a red-team test start from
	// targeted threat intelligence rather than a tester's imagination; a
	// discussion-based exercise deserves the same discipline.
	ThreatActor     string `json:"threat_actor"`
	ThreatNarrative string `json:"threat_narrative"`
	InitialVector   string `json:"initial_vector"`
	// ScenarioKey names the seeded scenario template this was built from, or
	// "" for a bespoke one.
	ScenarioKey string `json:"scenario_key"`

	Status string `json:"status"`
	// TLP is the Traffic Light Protocol marking. A live TLPT scoping document
	// is one of the most sensitive artefacts a bank holds, and the marking has
	// to travel with the record rather than live in someone's memory.
	TLP string `json:"tlp"`

	ScheduledFor    string `json:"scheduled_for"`
	DurationMinutes int    `json:"duration_minutes"`
	// StartedAt and EndedAt are wall-clock. Offsets on injects and clocks are
	// relative to StartedAt, so an exercise can be designed once and run on any
	// date.
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`

	Facilitator string `json:"facilitator"`
	ControlTeam string `json:"control_team"`
	Evaluators  string `json:"evaluators"`

	// AIGenerated, Model and PromptHash record provenance when the scenario was
	// designed by a model rather than written by hand.
	AIGenerated bool   `json:"ai_generated"`
	Model       string `json:"model,omitempty"`
	PromptHash  string `json:"prompt_hash,omitempty"`

	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`

	// Counts are filled on listing so the index does not have to load an
	// exercise to say how big it is.
	InjectCount   int `json:"inject_count"`
	FindingCount  int `json:"finding_count"`
	LatestVersion int `json:"latest_version"`
}

// Exercise statuses. An exercise moves forward only: a completed exercise that
// needs another run is cloned.
const (
	StatusDraft      = "draft"
	StatusScheduled  = "scheduled"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusClosed     = "closed"
)

// Exercise kinds, per NIST SP 800-84: discussion-based events talk through a
// scenario, operations-based events move something real.
const (
	KindDiscussion = "discussion"
	KindOperations = "operations"
)

// Traffic Light Protocol markings (FIRST TLP 2.0).
const (
	TLPClear     = "TLP:CLEAR"
	TLPGreen     = "TLP:GREEN"
	TLPAmber     = "TLP:AMBER"
	TLPAmberStr  = "TLP:AMBER+STRICT"
	TLPRedMarker = "TLP:RED"
)

// ---- objectives ----

// Objective is what the exercise is for. Rating it afterwards is the point:
// an exercise that produced good conversation but cannot say which objective
// was met has not been evaluated, it has been enjoyed.
type Objective struct {
	ID         int64  `json:"id"`
	ExerciseID int64  `json:"exercise_id"`
	Ordinal    int    `json:"ordinal"`
	Code       string `json:"code"`
	Text       string `json:"text"`
	// Capability names the thing being tested — "major incident classification
	// within DORA timelines", "board authority to approve a ransom decision" —
	// which is what a finding attaches to.
	Capability      string `json:"capability"`
	SuccessCriteria string `json:"success_criteria"`
	Rating          string `json:"rating"`
	Notes           string `json:"notes"`

	References []Reference `json:"references,omitempty"`
}

// Objective ratings, from ISO 22398's evaluation vocabulary.
const (
	RatingUntested      = "untested"
	RatingMet           = "met"
	RatingPartiallyMet  = "partially_met"
	RatingNotMet        = "not_met"
	RatingNotApplicable = "not_applicable"
)

// ---- phases ----

// Phase is one leg of the arc, from red-team detonation to stand-down. The
// default arc is seeded (see DefaultPhases) because the seams between these
// legs — detection to incident, incident to crisis, crisis to board, board to
// regulator — are where real exercises fail, and an arc that omits one of them
// cannot find that failure.
type Phase struct {
	ID         int64  `json:"id"`
	ExerciseID int64  `json:"exercise_id"`
	Ordinal    int    `json:"ordinal"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	Purpose    string `json:"purpose"`
	// EntryCriteria and ExitCriteria are what has to be true to move on. In
	// play they are the facilitator's cue; in the report they are the evidence
	// that a handover happened rather than a conversation drifting.
	EntryCriteria string `json:"entry_criteria"`
	ExitCriteria  string `json:"exit_criteria"`
	LeadRole      string `json:"lead_role"`
	// OffsetMinutes is when this phase opens, relative to exercise start.
	OffsetMinutes   int    `json:"offset_minutes"`
	DurationMinutes int    `json:"duration_minutes"`
	Status          string `json:"status"`
	Notes           string `json:"notes"`

	References []Reference `json:"references,omitempty"`
	Injects    []Inject    `json:"injects,omitempty"`
}

// Phase keys of the default arc.
const (
	PhaseThreatIntel    = "threat_intel"
	PhaseRedTeam        = "red_team"
	PhaseDetection      = "detection"
	PhaseIncident       = "incident_response"
	PhaseClassification = "classification"
	PhaseCrisis         = "crisis_activation"
	PhaseContinuity     = "continuity"
	PhaseCommunications = "communications"
	PhaseBoard          = "board"
	PhaseAuthorities    = "authorities"
	PhaseRecovery       = "recovery"
	PhaseAfterAction    = "after_action"
)

// Phase statuses during delivery.
const (
	PhasePending   = "pending"
	PhaseActive    = "active"
	PhaseComplete  = "complete"
	PhaseSkipped   = "skipped"
	PhaseCurtailed = "curtailed"
)

// ---- injects ----

// Inject is one entry of the Master Scenario Events List: a message delivered
// to players at a point on the clock, and the behaviour it is meant to provoke.
//
// ExpectedActions is the field that turns an exercise into an assessment. ISACA
// puts it plainly: track expected against actual behaviour at each step, and
// when they diverge, decide whether to retrain the people or rewrite the
// playbook. An inject with no expected action is entertainment.
type Inject struct {
	ID         int64  `json:"id"`
	ExerciseID int64  `json:"exercise_id"`
	PhaseID    int64  `json:"phase_id"`
	PhaseKey   string `json:"phase_key,omitempty"`
	Ordinal    int    `json:"ordinal"`
	Code       string `json:"code"`
	// OffsetMinutes is T+ from exercise start.
	OffsetMinutes int    `json:"offset_minutes"`
	Title         string `json:"title"`
	// Body is what the player actually receives, written in the voice of the
	// channel: a SIEM alert reads like a SIEM alert, a journalist's email reads
	// like a journalist's email.
	Body    string `json:"body"`
	Channel string `json:"channel"`
	From    string `json:"from_actor"`
	To      string `json:"to_actor"`
	Type    string `json:"inject_type"`

	ExpectedActions  string `json:"expected_actions"`
	ExpectedDecision string `json:"expected_decision"`
	DecisionOwner    string `json:"decision_owner"`
	EvaluationNotes  string `json:"evaluation_notes"`
	// Difficulty lets a facilitator run the same MSEL at two levels: drop the
	// hard injects for a first outing, keep them for the annual one.
	Difficulty string `json:"difficulty"`

	AIGenerated bool   `json:"ai_generated"`
	Model       string `json:"model,omitempty"`
	PromptHash  string `json:"prompt_hash,omitempty"`
	Confidence  string `json:"confidence,omitempty"`
	CreatedAt   string `json:"created_at"`

	References []Reference `json:"references,omitempty"`
	Response   *Response   `json:"response,omitempty"`
}

// Inject types. The three that are not plain scenario events all come from the
// same observation — that real crises are not tidy — and an exercise without
// them tests a process nobody will ever get to run.
const (
	// InjectEvent advances the scenario.
	InjectEvent = "event"
	// InjectStress removes something the plan assumed: the incident lead is on
	// a plane, the out-of-band channel is down, the third party will not answer.
	InjectStress = "stress"
	// InjectAmbiguous is conflicting, incomplete or simply wrong information,
	// which is what the first hours of a real incident are made of.
	InjectAmbiguous = "ambiguous"
	// InjectDecision forces a call with no good option.
	InjectDecision = "decision"
	// InjectContingency is held back and delivered only if play stalls or goes
	// off the rails.
	InjectContingency = "contingency"
	// InjectInformation is context with no expected action, used to set a scene
	// without implying there is something to do about it.
	InjectInformation = "information"
)

// Inject delivery channels.
const (
	ChannelSIEM      = "siem_alert"
	ChannelTicket    = "ticket"
	ChannelEmail     = "email"
	ChannelPhone     = "phone"
	ChannelChat      = "chat"
	ChannelSMS       = "sms"
	ChannelNews      = "news"
	ChannelSocial    = "social_media"
	ChannelRegulator = "regulator"
	ChannelBoard     = "board"
	ChannelCustomer  = "customer"
	ChannelVendor    = "third_party"
	ChannelInPerson  = "in_person"
	ChannelLawEnf    = "law_enforcement"
)

// Difficulty tiers.
const (
	DifficultyFoundation = "foundation"
	DifficultyChallenge  = "challenge"
	DifficultyAdvanced   = "advanced"
)

// ---- play ----

// Response is what actually happened when an inject landed. Outcome is the
// judgement; ActualActions is the evidence for it.
type Response struct {
	ID          int64  `json:"id"`
	ExerciseID  int64  `json:"exercise_id"`
	InjectID    int64  `json:"inject_id"`
	DeliveredAt string `json:"delivered_at"`
	// RespondedOffset is T+ minutes at which the team acted, which is the
	// number that tells you whether a four-hour obligation was ever reachable.
	RespondedOffset int    `json:"responded_offset"`
	Outcome         string `json:"outcome"`
	ActualActions   string `json:"actual_actions"`
	Observations    string `json:"observations"`
	Evaluator       string `json:"evaluator"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

// Response outcomes.
const (
	OutcomeAsExpected = "as_expected"
	OutcomePartial    = "partial"
	OutcomeDeviation  = "deviation"
	OutcomeMissed     = "missed"
	OutcomeNotPlayed  = "not_played"
)

// Decision is one crisis decision, logged as it is taken. The options and the
// rationale matter more than the decision: a supervisor reviewing a real event
// asks what else was considered, and a team that has never had to write that
// down under time pressure writes it badly when it counts.
type Decision struct {
	ID            int64  `json:"id"`
	ExerciseID    int64  `json:"exercise_id"`
	PhaseID       int64  `json:"phase_id"`
	OffsetMinutes int    `json:"offset_minutes"`
	Title         string `json:"title"`
	Options       string `json:"options"`
	Decision      string `json:"decision"`
	Rationale     string `json:"rationale"`
	MadeBy        string `json:"made_by"`
	Role          string `json:"role"`
	// Authority records who was entitled to make this call. Half of all crisis
	// exercises discover that nobody knows.
	Authority string `json:"authority"`
	// Reversible flags a decision that cannot be walked back — paying a ransom,
	// shutting a payment rail, telling the market. Those are the ones the board
	// owns.
	Reversible            bool   `json:"reversible"`
	RegulatoryImplication string `json:"regulatory_implication"`
	CustomerImpact        string `json:"customer_impact"`
	CreatedAt             string `json:"created_at"`

	References []Reference `json:"references,omitempty"`
}

// ---- classification and clocks ----

// Classification is the DORA materiality assessment for the scenario incident,
// filled in by the players and then scored by this module.
//
// Modelling it rather than narrating it is deliberate. Under Delegated
// Regulation (EU) 2024/1772 an incident is major when critical services are
// affected *and* either the data-loss criterion is met or two or more of the
// remaining criteria are — and every exercise that has ever run this on a
// whiteboard has produced a different answer from the same facts. Making the
// team enter the numbers, and having the module apply the rule, is the whole
// exercise of the classification phase.
type Classification struct {
	ExerciseID int64 `json:"exercise_id"`
	// AwareOffset is T+ minutes at which the entity became aware. The DORA
	// 24-hour outer cap runs from here.
	AwareOffset int `json:"aware_offset"`
	// ClassifiedOffset is T+ minutes at which it was classified major. The
	// four-hour initial-notification clock runs from here, which is why a team
	// that classifies late has not bought itself time — it has spent it.
	ClassifiedOffset int `json:"classified_offset"`

	CriticalServicesAffected bool   `json:"critical_services_affected"`
	ClientsAffected          string `json:"clients_affected"`
	ClientsMaterial          bool   `json:"clients_material"`
	TransactionsAffected     string `json:"transactions_affected"`
	TransactionsMaterial     bool   `json:"transactions_material"`
	ReputationalImpact       string `json:"reputational_impact"`
	ReputationalMaterial     bool   `json:"reputational_material"`
	DowntimeMinutes          int    `json:"downtime_minutes"`
	DurationMaterial         bool   `json:"duration_material"`
	GeographicalSpread       string `json:"geographical_spread"`
	GeographicalMaterial     bool   `json:"geographical_material"`
	DataLosses               string `json:"data_losses"`
	DataLossesMaterial       bool   `json:"data_losses_material"`
	EconomicImpact           string `json:"economic_impact"`
	EconomicMaterial         bool   `json:"economic_material"`

	// PersonalDataBreach is a separate question from DORA materiality and it
	// starts its own clock. Teams routinely conflate the two and notify one
	// authority twice while missing the other entirely.
	PersonalDataBreach bool `json:"personal_data_breach"`
	// NIS2Significant is likewise separate: an entity in scope of the national
	// NIS2 transposition owes CERT an early warning on its own trigger.
	NIS2Significant bool `json:"nis2_significant"`

	// Major is computed by Evaluate, not entered.
	Major     bool   `json:"major"`
	Rationale string `json:"rationale"`
	// TeamVerdict is what the players concluded, kept beside the computed
	// answer so the gap between them is itself a finding.
	TeamVerdict string `json:"team_verdict"`
	Notes       string `json:"notes"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	UpdatedBy   string `json:"updated_by,omitempty"`
}

// Clock is one regulatory or contractual deadline the scenario started, and
// whether the team met it.
type Clock struct {
	ID         int64 `json:"id"`
	ExerciseID int64 `json:"exercise_id"`
	Ordinal    int   `json:"ordinal"`
	// Regime is the obligation's key ("dora_initial"), Authority who is owed
	// it, and Label how it reads in the report.
	Regime    string `json:"regime"`
	Authority string `json:"authority"`
	Label     string `json:"label"`
	// Basis says what starts the clock, in words, because the commonest failure
	// is a team running the right duration from the wrong event.
	Basis string `json:"basis"`
	// DueOffset is T+ minutes from exercise start, already resolved against the
	// classification's trigger offsets.
	DueOffset int `json:"due_offset"`
	// ActualOffset is when the notification actually went, or -1 if it never
	// did.
	ActualOffset int    `json:"actual_offset"`
	Status       string `json:"status"`
	Evidence     string `json:"evidence"`
	Notes        string `json:"notes"`
	// Source is the instrument that imposes it, for the report's citation.
	Source string `json:"source"`

	References []Reference `json:"references,omitempty"`
}

// Clock statuses.
const (
	ClockPending       = "pending"
	ClockMet           = "met"
	ClockMissed        = "missed"
	ClockNotApplicable = "not_applicable"
)

// ---- findings ----

// Finding is one gap the exercise exposed. Severity and Owner are what make it
// actionable; RiskRef is what stops it being forgotten, by pointing at the risk
// register entry that now carries it.
type Finding struct {
	ID         int64  `json:"id"`
	ExerciseID int64  `json:"exercise_id"`
	PhaseID    int64  `json:"phase_id"`
	PhaseKey   string `json:"phase_key,omitempty"`
	Ordinal    int    `json:"ordinal"`
	Code       string `json:"code"`
	Title      string `json:"title"`
	// Category separates the gap that a control would close from the one only a
	// mandate can. Buying a tool to fix a governance gap is the commonest
	// after-action mistake.
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	RootCause   string `json:"root_cause"`
	// Evidence is what was observed, quoted from the response log where
	// possible, so a finding cannot be argued away in the debrief.
	Evidence       string `json:"evidence"`
	Recommendation string `json:"recommendation"`
	Owner          string `json:"owner"`
	DueDate        string `json:"due_date"`
	Status         string `json:"status"`
	RiskRef        string `json:"risk_ref"`
	AIGenerated    bool   `json:"ai_generated"`
	Model          string `json:"model,omitempty"`
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`

	References []Reference `json:"references,omitempty"`
}

// Finding categories.
const (
	CategoryPeople        = "people"
	CategoryProcess       = "process"
	CategoryTechnology    = "technology"
	CategoryGovernance    = "governance"
	CategoryCommunication = "communication"
	CategoryThirdParty    = "third_party"
	CategoryRegulatory    = "regulatory"
)

// Finding severities, matching the risk register's vocabulary so a finding
// promoted to a risk does not have to be re-graded.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
	SeverityObserve  = "observation"
)

// Finding statuses.
const (
	FindingOpen       = "open"
	FindingInProgress = "in_progress"
	FindingClosed     = "closed"
	FindingAccepted   = "accepted"
)

// ---- participants ----

// Participant is one person at the table, and which seat they were in. The
// roster is part of the evidence: a supervisor asking whether the management
// body has been exercised wants names, not an assurance.
type Participant struct {
	ID         int64  `json:"id"`
	ExerciseID int64  `json:"exercise_id"`
	Name       string `json:"name"`
	RoleKey    string `json:"role_key"`
	RoleLabel  string `json:"role_label"`
	Org        string `json:"org"`
	// Player distinguishes someone being exercised from someone running the
	// exercise. Counting a controller as a player flatters the attendance.
	Player   bool   `json:"player"`
	Attended bool   `json:"attended"`
	Contact  string `json:"contact"`
	Notes    string `json:"notes"`
}

// ---- references ----

// Reference is the link from a piece of an exercise to the thing it exercises:
// a control, a Security NFR, a regulation clause, a policy clause, a risk, or
// an authority source such as a DORA article or the TIBER-EU framework.
//
// It is polymorphic on both ends deliberately. Any part of an exercise can cite
// any part of the compliance vocabulary, and a design that gave each pairing
// its own table would have produced thirty tables and still missed the pairing
// somebody wanted next.
type Reference struct {
	ID         int64 `json:"id"`
	ExerciseID int64 `json:"exercise_id"`
	// OwnerKind and OwnerID say which part of the exercise is citing.
	OwnerKind string `json:"owner_kind"`
	OwnerID   int64  `json:"owner_id"`
	// RefKind and Ref say what is cited.
	RefKind string `json:"ref_kind"`
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	Note    string `json:"note"`
	// Source distinguishes a seeded link, a curator's link and a model's
	// suggestion, because they warrant different scrutiny.
	Source string `json:"source"`
	// Known reports whether Ref resolved against this installation's catalogs.
	// A model that invents a control ID is caught here rather than in front of
	// a supervisor.
	Known bool `json:"known"`
	// URL is where a reader can go and see the cited record.
	URL       string `json:"url,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Reference owner kinds.
const (
	OwnerExercise  = "exercise"
	OwnerObjective = "objective"
	OwnerPhase     = "phase"
	OwnerInject    = "inject"
	OwnerDecision  = "decision"
	OwnerFinding   = "finding"
	OwnerClock     = "clock"
)

// Reference target kinds. The first five mirror internal/knowledge's kinds so a
// reference resolves against the same catalogs the AI agent reads. RefAuthority
// is this module's own: the frameworks and supervisory instruments an exercise
// is built against, which are not records in this installation's database.
const (
	RefControl          = "control"
	RefNFR              = "nfr"
	RefRegulationClause = "regulation_clause"
	RefPolicyClause     = "policy_clause"
	RefRisk             = "risk"
	RefAuthority        = "authority"
)

// Reference sources.
const (
	SourceSeed   = "seed"
	SourceManual = "manual"
	SourceModel  = "model"
)

// ---- versions and chat ----

// Version is an immutable snapshot: the exercise as designed, or the
// after-action report as issued. A report that has been sent to a board cannot
// silently change when someone edits a finding, so revisions append.
type Version struct {
	ID         int64 `json:"id"`
	ExerciseID int64 `json:"exercise_id"`
	Number     int   `json:"number"`
	// Kind is VersionDesign or VersionAfterAction.
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	Snapshot  string `json:"-"`
}

// Version kinds.
const (
	VersionDesign      = "design"
	VersionAfterAction = "after_action"
)

// ChatTurn is one message in a conversation with an expert persona about this
// exercise. Persona is on the turn rather than on the conversation because a
// facilitator switches advisers mid-exercise — ask the CISO what containment
// costs, then ask the board chair how it will sound in the minutes.
type ChatTurn struct {
	ID         int64  `json:"id"`
	ExerciseID int64  `json:"exercise_id"`
	Persona    string `json:"persona"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	Model      string `json:"model"`
	Actor      string `json:"actor,omitempty"`
	// Scope names what the turn was about — "inject INJ-014", "phase board" —
	// so the transcript reads as a record of the exercise rather than a chat
	// log.
	Scope     string `json:"scope,omitempty"`
	SessionID string `json:"-"`
	CreatedAt string `json:"created_at"`
}

// Chat roles, matching aiprovider's.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// ---- aggregate ----

// Dossier is an exercise and everything hanging off it: what the design page,
// the run page, the after-action report and the PDF are all built from.
type Dossier struct {
	Exercise       Exercise       `json:"exercise"`
	Objectives     []Objective    `json:"objectives"`
	Phases         []Phase        `json:"phases"`
	Participants   []Participant  `json:"participants"`
	Classification Classification `json:"classification"`
	Clocks         []Clock        `json:"clocks"`
	Decisions      []Decision     `json:"decisions"`
	Findings       []Finding      `json:"findings"`
	References     []Reference    `json:"references"`
	Version        Version        `json:"version"`
	Stats          Stats          `json:"stats"`
	GeneratedAt    string         `json:"generated_at"`
}

// Stats is the arithmetic shown at the top of the report.
type Stats struct {
	Objectives     int `json:"objectives"`
	ObjectivesMet  int `json:"objectives_met"`
	Phases         int `json:"phases"`
	Injects        int `json:"injects"`
	InjectsPlayed  int `json:"injects_played"`
	AsExpected     int `json:"as_expected"`
	Deviations     int `json:"deviations"`
	Missed         int `json:"missed"`
	Decisions      int `json:"decisions"`
	Clocks         int `json:"clocks"`
	ClocksMet      int `json:"clocks_met"`
	ClocksMissed   int `json:"clocks_missed"`
	Findings       int `json:"findings"`
	FindingsOpen   int `json:"findings_open"`
	CriticalHigh   int `json:"critical_high"`
	References     int `json:"references"`
	UnknownRefs    int `json:"unknown_refs"`
	DistinctRefs   int `json:"distinct_refs"`
	StressInjects  int `json:"stress_injects"`
	AmbiguityRatio int `json:"ambiguity_ratio"`
}

// PlayedPercent is the share of injects that were actually delivered. An
// exercise that ran out of time at 40% has not tested its later phases, and the
// report should say so rather than quietly reporting on the part that ran.
func (s Stats) PlayedPercent() int {
	if s.Injects == 0 {
		return 0
	}
	return int(float64(s.InjectsPlayed) / float64(s.Injects) * 100)
}

// PerformancePercent is the share of played injects the team handled as
// expected. It is deliberately computed over played injects only: crediting an
// undelivered inject either way would be an invention.
func (s Stats) PerformancePercent() int {
	if s.InjectsPlayed == 0 {
		return 0
	}
	return int(float64(s.AsExpected) / float64(s.InjectsPlayed) * 100)
}

// ---- errors ----

// ErrNotFound is returned when an exercise, inject, phase or version does not
// exist.
type ErrNotFound struct{ What string }

func (e ErrNotFound) Error() string { return e.What + " not found" }

// ErrInvalid reports a caller mistake — an empty title, an unknown phase key.
type ErrInvalid struct{ Message string }

func (e ErrInvalid) Error() string { return e.Message }

func invalid(message string) error { return ErrInvalid{Message: message} }

func invalidf(format string, args ...any) error {
	return ErrInvalid{Message: fmt.Sprintf(format, args...)}
}

func notFound(what string) error { return ErrNotFound{What: what} }

// ---- vocabulary normalisation ----

// Confidence values, ordered, matching internal/regcoverage so a reader moving
// between the two reports reads the same words.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

func normalizeConfidence(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ConfidenceHigh:
		return ConfidenceHigh
	case ConfidenceLow:
		return ConfidenceLow
	default:
		return ConfidenceMedium
	}
}

// oneOf returns value when it is in allowed, and fallback otherwise. Every
// enumerated field on the way in goes through it, so a hand-written API call
// cannot put a status in the database that no page knows how to render.
func oneOf(value string, fallback string, allowed ...string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	for _, a := range allowed {
		if v == a {
			return a
		}
	}
	return fallback
}

func trim(s string) string { return strings.TrimSpace(s) }
