package crisisexercise

import "strings"

// This file is the module's seeded expertise: the phase arc an exercise runs
// through, the seats at the table, the expert personas a facilitator can
// consult, and the catalog of frameworks and supervisory instruments that any
// part of an exercise can cite.
//
// It is data rather than configuration because it is the part a customer should
// not have to research. An institution buying this module is buying the arc and
// the citations; what it supplies is its own scenario.

// ---- phases ----

// PhaseTemplate is one leg of the default arc.
type PhaseTemplate struct {
	Key             string
	Name            string
	Purpose         string
	EntryCriteria   string
	ExitCriteria    string
	LeadRole        string
	OffsetMinutes   int
	DurationMinutes int
	// Authorities are the framework references seeded onto the phase, so a
	// freshly created exercise already cites the instruments each leg is
	// answerable to.
	Authorities []string
	// FacilitatorNotes is what a trainer needs to know to run this leg well. It
	// is shown on the run page, not in the report.
	FacilitatorNotes string
}

// DefaultPhases is the arc from a red-team detonation to a signed-off
// after-action report.
//
// The arc is longer than most exercise templates on purpose. The failures worth
// finding are almost never inside a phase — a SOC that can contain, a comms
// team that can write, a board that can decide. They are at the joins: the
// analyst who never told anyone this was more than a ticket, the incident lead
// who never said the word "crisis", the crisis team that ran for six hours
// without anyone starting the four-hour regulatory clock, the board briefed on
// a version of events three hours out of date. Each join here is its own phase
// with its own entry and exit criteria, because a join with no criteria is a
// join nobody is accountable for.
func DefaultPhases() []PhaseTemplate {
	return []PhaseTemplate{
		{
			Key:     PhaseThreatIntel,
			Name:    "Threat intelligence and scenario grounding",
			Purpose: "Establish that the scenario is what this entity would actually face, from named actors, current tradecraft and the entity's own attack surface — not from a tester's imagination.",
			EntryCriteria: "Targeted threat intelligence for the entity, its sector and its jurisdiction is available and current.\n" +
				"Critical or important functions are agreed and written down.",
			ExitCriteria: "Threat actor, initial access vector and attack path are agreed by the control team.\n" +
				"Scenario is judged plausible by someone who would have to defend against it.",
			LeadRole:        RoleThreatIntel,
			OffsetMinutes:   0,
			DurationMinutes: 20,
			Authorities:     []string{"tiber-eu", "dora-art-26", "enisa-finance-tl", "mitre-attack"},
			FacilitatorNotes: "For a board audience compress this to a five-minute briefing: who the actor is, why they would " +
				"pick this institution, and what they want. Board members disengage from tradecraft and re-engage instantly " +
				"at motive and consequence.",
		},
		{
			Key:     PhaseRedTeam,
			Name:    "Red team / adversary action",
			Purpose: "Play the adversary's campaign to the point of impact — reconnaissance, initial access, persistence, escalation, lateral movement, objective — so that what the defenders eventually see has a cause behind it.",
			EntryCriteria: "Scope and rules of engagement approved by the control team.\n" +
				"Blue team not forewarned (for a TLPT-style run) or explicitly forewarned (for a purple-team run).",
			ExitCriteria: "Adversary has achieved the scenario objective, or has been detected and evicted.\n" +
				"The attack path is documented step by step, whether or not anyone saw it.",
			LeadRole:        RoleRedTeam,
			OffsetMinutes:   20,
			DurationMinutes: 25,
			Authorities:     []string{"dora-art-26", "dora-art-27", "dora-rts-tlpt", "tiber-eu", "tiber-eu-purple", "mitre-attack"},
			FacilitatorNotes: "In a discussion-based exercise this phase is narration, not play: give the team the attack path as " +
				"established fact and spend the time on what would have caught it. Do not let a tabletop turn into a " +
				"debate about whether the attack was realistic — that argument is how a team avoids the uncomfortable part.",
		},
		{
			Key:           PhaseDetection,
			Name:          "Detection, triage and escalation",
			Purpose:       "Test whether the entity notices, and whether noticing turns into telling someone. This is the phase that most often fails quietly.",
			EntryCriteria: "First observable signal is available to the defenders (alert, ticket, customer report, third-party notification).",
			ExitCriteria: "The event has an owner, a severity and an escalation decision — or it has been closed, which is itself a finding.\n" +
				"Time from first signal to escalation is recorded.",
			LeadRole:        RoleSOCLead,
			OffsetMinutes:   45,
			DurationMinutes: 25,
			Authorities:     []string{"dora-art-17", "nist-sp-800-61r3", "nist-csf-2", "iso-27035"},
			FacilitatorNotes: "Record the clock time of first signal and of escalation, and put both in the after-action report. " +
				"Teams argue about detection quality; nobody argues with the gap between two timestamps.",
		},
		{
			Key:           PhaseIncident,
			Name:          "Security incident response",
			Purpose:       "Run the incident: scope it, contain it, preserve evidence, and decide what is being traded away by each containment option.",
			EntryCriteria: "An incident is declared and an incident manager is named.",
			ExitCriteria: "Containment approach chosen with its business cost stated.\n" +
				"Forensic preservation decided before anything is rebuilt.\n" +
				"An impact picture exists that is good enough to classify against.",
			LeadRole:        RoleIncidentLead,
			OffsetMinutes:   70,
			DurationMinutes: 35,
			Authorities:     []string{"dora-art-17", "dora-art-11", "nist-sp-800-61r3", "iso-27035", "fsb-cirr"},
			FacilitatorNotes: "Force the trade-off explicitly. 'Isolate the payment gateway' is a containment action and a service " +
				"outage; a team that says the first without the second has not made a decision, it has made a request.",
		},
		{
			Key:           PhaseClassification,
			Name:          "Incident classification and the regulatory clock",
			Purpose:       "Make the team classify the incident against the DORA materiality criteria, on the record, with the numbers they actually have — and start the notification clocks from that moment.",
			EntryCriteria: "An impact picture exists, however incomplete.",
			ExitCriteria: "A classification decision is recorded with its rationale and the time it was taken.\n" +
				"Every applicable notification clock is running and has an owner.",
			LeadRole:        RoleComplianceLead,
			OffsetMinutes:   105,
			DurationMinutes: 25,
			Authorities: []string{"dora-art-18", "dora-art-19", "dora-rts-classification", "dora-reporting-templates",
				"nis2-art-23", "gdpr-art-33"},
			FacilitatorNotes: "This is the highest-value twenty-five minutes in the whole exercise and the one most often skipped. " +
				"Do not let the team defer classification until the facts are clear: the four-hour clock runs from " +
				"classification, so a team that waits for certainty has not bought time, it has spent it. Make them " +
				"classify on partial information and revise later, which is exactly what the regulation contemplates.",
		},
		{
			Key:           PhaseCrisis,
			Name:          "Crisis declaration and crisis management team activation",
			Purpose:       "Test the transition from incident to crisis: who can declare it, what changes when they do, and whether the crisis team can be assembled when the usual channels are the thing that is broken.",
			EntryCriteria: "Impact has crossed, or is about to cross, the entity's crisis declaration threshold.",
			ExitCriteria: "Crisis is declared (or explicitly not declared, with reasons).\n" +
				"Crisis management team is quorate, with a named crisis director and a scribe.\n" +
				"Battle rhythm agreed: how often it meets, and what it decides between meetings.",
			LeadRole:        RoleCrisisDirector,
			OffsetMinutes:   130,
			DurationMinutes: 25,
			Authorities:     []string{"iso-22361", "iso-22301", "dora-art-11", "ecb-croe"},
			FacilitatorNotes: "Two questions expose most crisis frameworks. Who exactly is allowed to declare a crisis at 02:00 " +
				"on a Sunday, and what is the second name on that list? And: what does the organisation stop doing once a " +
				"crisis is declared? A framework that adds a meeting without removing an obligation has not been tested.",
		},
		{
			Key:           PhaseContinuity,
			Name:          "Business continuity and recovery activation",
			Purpose:       "Move from defending systems to sustaining the business: invoke continuity plans, run degraded and manual processes, and confirm that recovery objectives are achievable rather than aspirational.",
			EntryCriteria: "Crisis is declared, or an outage has passed its maximum tolerable disruption.",
			ExitCriteria: "Continuity options invoked with their capacity and duration limits stated.\n" +
				"Recovery point and recovery time objectives reconciled against what is actually recoverable.\n" +
				"Backup integrity assumption tested rather than assumed.",
			LeadRole:        RoleBCMLead,
			OffsetMinutes:   155,
			DurationMinutes: 30,
			Authorities:     []string{"iso-22301", "dora-art-11", "dora-art-12", "ecb-croe", "cpmi-iosco"},
			FacilitatorNotes: "Ask how long the manual workaround lasts. Almost every continuity plan is written for four hours " +
				"and every serious cyber event lasts days; the interesting failure is on day three, not hour two.",
		},
		{
			Key:           PhaseCommunications,
			Name:          "Crisis communications",
			Purpose:       "Produce, approve and release what the entity will actually say — to staff, customers, counterparties, the market and the press — while the facts are still moving.",
			EntryCriteria: "Crisis is declared, or the event is already public.",
			ExitCriteria: "A holding statement exists, has been approved, and names who approved it.\n" +
				"Internal communication has gone out before external.\n" +
				"Customer channel messaging and the contact-centre script are consistent with the public line.",
			LeadRole:        RoleCommsLead,
			OffsetMinutes:   185,
			DurationMinutes: 30,
			Authorities:     []string{"dora-art-14", "iso-22361", "gdpr-art-34"},
			FacilitatorNotes: "Make them write it, not describe it. Ninety per cent of communications phases dissolve into a " +
				"discussion of principles; give them ten minutes and a blank page and take what they produce into the " +
				"report. Then read it back as a hostile journalist would.",
		},
		{
			Key:           PhaseBoard,
			Name:          "Board and management body engagement",
			Purpose:       "Brief the management body on facts they can act on, and take the decisions only they can take. Under DORA the management body carries ultimate responsibility for ICT risk, which makes this phase a governance test, not a courtesy.",
			EntryCriteria: "Crisis is declared and an initial impact assessment exists.",
			ExitCriteria: "Board has been briefed on impact, options, cost and regulatory position.\n" +
				"Reserved decisions are taken and minuted, with the authority for each one identified.\n" +
				"Board has agreed what it will be told next, and when.",
			LeadRole:        RoleBoardChair,
			OffsetMinutes:   215,
			DurationMinutes: 30,
			Authorities:     []string{"dora-art-5", "iso-22361", "g7-elements", "ecb-croe"},
			FacilitatorNotes: "Give the board ten minutes of briefing and then take the briefer out of the room — 'she is on a " +
				"call with the regulator' — and make them decide anyway. A board that can only decide with the CISO " +
				"present has not been exercised. Reserved decisions to force: ransom, disclosure ahead of certainty, " +
				"suspending a service, and invoking cyber insurance.",
		},
		{
			Key:           PhaseAuthorities,
			Name:          "Authority, supervisory and law-enforcement interaction",
			Purpose:       "Deal with the outside: the competent authority, the national CSIRT, the data protection authority, law enforcement, payment schemes, and — where the entity is significant — the ECB. Test whether the entity can answer questions it was not expecting.",
			EntryCriteria: "A notification clock is running, or an authority has made contact unprompted.",
			ExitCriteria: "Each applicable notification is drafted, approved and sent, with its timestamp recorded.\n" +
				"A single named liaison owns each authority relationship.\n" +
				"The entity's answer to 'is this contained?' is consistent across every authority it has spoken to.",
			LeadRole:        RoleRegulatorLiaison,
			OffsetMinutes:   245,
			DurationMinutes: 30,
			Authorities: []string{"dora-art-19", "dora-reporting-templates", "nis2-art-23", "gdpr-art-33",
				"ecb-ssm", "eu-scicf", "esrb-recommendation"},
			FacilitatorNotes: "The supervisor's first question is rarely technical. It is 'when did you know, and why am I " +
				"hearing it now'. Rehearse the answer to that, and rehearse the harder one: what the entity says when " +
				"two authorities in two countries ask for inconsistent things at the same time.",
		},
		{
			Key:           PhaseRecovery,
			Name:          "Recovery, stand-down and return to business as usual",
			Purpose:       "Test the end of the crisis, which is a decision and not an event: verified recovery, controlled stand-down, and a handover of everything still open.",
			EntryCriteria: "Containment is holding and recovery is under way.",
			ExitCriteria: "Recovery is verified rather than assumed — the criteria for 'clean' are stated and met.\n" +
				"Stand-down is declared by the person who declared the crisis.\n" +
				"Open items, residual risk and continuing obligations are handed to named owners.",
			LeadRole:        RoleCrisisDirector,
			OffsetMinutes:   275,
			DurationMinutes: 20,
			Authorities:     []string{"dora-art-11", "iso-22301", "nist-sp-800-61r3", "fsb-cirr"},
			FacilitatorNotes: "Ask what evidence would justify declaring the environment clean, and then ask who would be " +
				"comfortable signing that. The gap between the two answers is usually the finding.",
		},
		{
			Key:           PhaseAfterAction,
			Name:          "Hot debrief and after-action review",
			Purpose:       "Capture what happened while it is still fresh, agree the findings, and assign them — before the exercise becomes a story people tell rather than a change anyone makes.",
			EntryCriteria: "Play has stopped and all participants are still in the room.",
			ExitCriteria: "Findings are agreed with severity, owner and due date.\n" +
				"Objectives are rated.\n" +
				"Findings that represent standing risk are recorded in the risk register rather than left in the report.",
			LeadRole:        RoleEvaluator,
			OffsetMinutes:   295,
			DurationMinutes: 25,
			Authorities:     []string{"dora-art-13", "iso-22398", "nist-sp-800-84", "isaca-exercise-guidance"},
			FacilitatorNotes: "Run the hot debrief before anyone leaves and before anyone has had time to construct a defensible " +
				"account. Start with what went well — it is not a courtesy, it is how you keep the room honest about the " +
				"rest. Then: what surprised you, what would you have wanted, what would you do differently at 02:00.",
		},
	}
}

// PhaseTemplateFor returns the seeded template for a key.
func PhaseTemplateFor(key string) (PhaseTemplate, bool) {
	for _, p := range DefaultPhases() {
		if p.Key == key {
			return p, true
		}
	}
	return PhaseTemplate{}, false
}

// ---- roles ----

// Role keys. The first block is the entity's own crisis structure; the second
// is the exercise machinery; the third is the TIBER-EU team structure, which
// uses different words for overlapping jobs and is worth keeping distinct so a
// TLPT-derived exercise reads the way its participants expect.
const (
	RoleCrisisDirector    = "crisis_director"
	RoleCEO               = "ceo"
	RoleBoardChair        = "board_chair"
	RoleNED               = "non_executive_director"
	RoleCISO              = "ciso"
	RoleIncidentLead      = "incident_lead"
	RoleSOCLead           = "soc_lead"
	RoleForensics         = "forensics"
	RoleITOps             = "it_operations"
	RoleBCMLead           = "bcm_lead"
	RoleCommsLead         = "communications_lead"
	RoleLegal             = "legal_counsel"
	RoleDPO               = "dpo"
	RoleComplianceLead    = "compliance_lead"
	RoleRegulatorLiaison  = "regulator_liaison"
	RoleHeadOfOperations  = "head_of_operations"
	RoleHeadOfRetail      = "head_of_retail"
	RoleTreasury          = "treasury"
	RoleHR                = "hr"
	RoleThirdPartyManager = "third_party_manager"
	RoleCustomerContact   = "contact_centre"
	RoleScribe            = "scribe"

	RoleFacilitator = "facilitator"
	RoleController  = "controller"
	RoleEvaluator   = "evaluator"
	RoleObserver    = "observer"
	RoleThreatIntel = "threat_intelligence"

	RoleRedTeam      = "red_team"
	RoleBlueTeam     = "blue_team"
	RoleWhiteTeam    = "white_team"
	RoleControlTeam  = "control_team"
	RolePurpleTeam   = "purple_team"
	RoleAuthorityRep = "authority_representative"
)

// RoleDef is one seat: what it is called and what it is answerable for during
// an exercise.
type RoleDef struct {
	Key   string
	Label string
	// Group is how the roster is rendered: "entity", "exercise" or "testing".
	Group string
	// Remit is one line on what this seat is accountable for. It is shown next
	// to the name on the roster, because the commonest cause of a stalled
	// crisis team is two people who each thought the other had it.
	Remit string
}

// Roles is the seat catalog.
func Roles() []RoleDef {
	return []RoleDef{
		{RoleCrisisDirector, "Crisis director", "entity", "Declares and stands down the crisis; owns the battle rhythm and the decision log."},
		{RoleCEO, "Chief executive", "entity", "Owns the external position of the institution and any decision that changes it."},
		{RoleBoardChair, "Board chair", "entity", "Convenes the management body; owns reserved decisions and the board's record of them."},
		{RoleNED, "Non-executive director", "entity", "Challenges the executive account; tests whether the board is being given decisions or updates."},
		{RoleCISO, "CISO", "entity", "Owns the security assessment and the technical options put to the crisis team."},
		{RoleIncidentLead, "Incident manager", "entity", "Runs the incident: scope, containment, evidence, and the single agreed version of events."},
		{RoleSOCLead, "SOC lead", "entity", "Detection, triage and escalation; owns the timeline of what was seen and when."},
		{RoleForensics, "Digital forensics", "entity", "Evidence preservation and attack reconstruction; the constraint on any rebuild decision."},
		{RoleITOps, "IT operations", "entity", "Executes isolation, failover and rebuild; owns what is technically possible in the time available."},
		{RoleBCMLead, "Business continuity lead", "entity", "Invokes continuity plans; owns degraded-mode capacity and how long it lasts."},
		{RoleCommsLead, "Communications lead", "entity", "Drafts and releases every external and internal message; owns message consistency."},
		{RoleLegal, "Legal counsel", "entity", "Privilege, liability, contractual notification, and law-enforcement engagement."},
		{RoleDPO, "Data protection officer", "entity", "Personal data breach assessment and the supervisory-authority and data-subject clocks."},
		{RoleComplianceLead, "Compliance lead", "entity", "Incident classification against the regulatory criteria and the notification clocks."},
		{RoleRegulatorLiaison, "Supervisory liaison", "entity", "Single point of contact for the competent authority; owns consistency across authorities."},
		{RoleHeadOfOperations, "Head of operations", "entity", "Payments, settlement and back-office impact; owns the manual fallback."},
		{RoleHeadOfRetail, "Head of retail banking", "entity", "Customer impact, branch and channel decisions, and customer redress."},
		{RoleTreasury, "Treasury", "entity", "Liquidity position and funding under stress; the first place a deposit run shows up."},
		{RoleHR, "Human resources", "entity", "Staff welfare, shift rotation and internal messaging; insider-related handling."},
		{RoleThirdPartyManager, "Third-party manager", "entity", "Provider engagement, contractual rights and the register of information."},
		{RoleCustomerContact, "Contact centre lead", "entity", "What customers are actually told; the earliest warning of public sentiment."},
		{RoleScribe, "Scribe", "entity", "Maintains the decision log and the timeline. The most under-resourced seat in every real crisis."},

		{RoleFacilitator, "Facilitator", "exercise", "Runs the room, keeps the arc moving, and refuses to let the team solve the wrong problem."},
		{RoleController, "Controller", "exercise", "Delivers injects on the clock and holds the contingency injects."},
		{RoleEvaluator, "Evaluator", "exercise", "Records expected against actual behaviour; drafts the findings."},
		{RoleObserver, "Observer", "exercise", "Watches without participating; often the supervisor or an internal auditor."},
		{RoleThreatIntel, "Threat intelligence provider", "exercise", "Supplies the targeted intelligence the scenario is built on."},

		{RoleRedTeam, "Red team", "testing", "Executes the simulated attack against live production systems."},
		{RoleBlueTeam, "Blue team", "testing", "The entity's defenders, unaware that the activity is a test."},
		{RoleWhiteTeam, "White team", "testing", "Oversees the test and manages its risk; the only group that knows everything."},
		{RoleControlTeam, "Control team", "testing", "The entity-side team managing the test and its secrecy."},
		{RolePurpleTeam, "Purple team", "testing", "Red and blue working together to replay and improve detection after the covert phase."},
		{RoleAuthorityRep, "Authority representative", "testing", "The supervisory cyber team overseeing the test."},
	}
}

// RoleLabel resolves a role key for display, falling back to the raw key so an
// unrecognised value is visible rather than blank.
func RoleLabel(key string) string {
	key = strings.TrimSpace(key)
	for _, r := range Roles() {
		if r.Key == key {
			return r.Label
		}
	}
	if key == "" {
		return ""
	}
	return key
}

// ---- authority and framework catalog ----

// Authority is one framework, regulation or supervisory instrument an exercise
// can cite.
//
// These are seeded rather than read from the Regulation Coverage module because
// they must be citable on a fresh installation. An entity that has never
// uploaded DORA still needs to say that its classification phase tests Article
// 18; requiring an upload first would make the citation a function of what
// somebody happened to load.
type Authority struct {
	Key string `json:"key"`
	// Name is the citation as it should appear in a report.
	Name string `json:"name"`
	// Issuer is who publishes it, which is what tells a reader how much weight
	// it carries: a regulation is not a good practice note.
	Issuer string `json:"issuer"`
	// Instrument distinguishes binding law from supervisory expectation from
	// professional guidance. A report that cites all three in the same voice
	// is misleading.
	Instrument string `json:"instrument"`
	// Summary says what it requires or recommends, in one or two sentences.
	Summary string `json:"summary"`
	URL     string `json:"url"`
	// Topics are the phases and themes it bears on, used to offer the right
	// citations when someone is editing an inject.
	Topics []string `json:"topics,omitempty"`
}

// Instrument classes.
const (
	InstrumentRegulation  = "regulation"
	InstrumentDirective   = "directive"
	InstrumentRTS         = "technical_standard"
	InstrumentSupervisory = "supervisory_expectation"
	InstrumentStandard    = "standard"
	InstrumentGuidance    = "guidance"
	InstrumentNational    = "national_law"
)

// Authorities is the seeded citation catalog.
func Authorities() []Authority {
	return []Authority{
		// ---- DORA ----
		{
			Key: "dora-art-5", Name: "DORA Article 5 — Governance and organisation",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "The management body bears ultimate responsibility for managing ICT risk and must define, approve and oversee the ICT risk management framework. Members must keep their knowledge current through regular, proportionate training, and must allocate and review the budget for resilience.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_5.html",
			Topics:  []string{PhaseBoard, "governance", "training"},
		},
		{
			Key: "dora-art-11", Name: "DORA Article 11 — Response and recovery",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Requires an ICT business continuity policy, response and recovery plans, and testing of them at least yearly, including for scenarios of switching to and operating from a secondary infrastructure.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_11.html",
			Topics:  []string{PhaseCrisis, PhaseContinuity, PhaseRecovery},
		},
		{
			Key: "dora-art-12", Name: "DORA Article 12 — Backup policies and restoration",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Backup policies, restoration and recovery procedures and methods, with restoration tested periodically and segregation of backup systems from source systems.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_12.html",
			Topics:  []string{PhaseContinuity, PhaseRecovery},
		},
		{
			Key: "dora-art-13", Name: "DORA Article 13 — Learning and evolving",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Requires post-incident reviews after major incidents, capabilities to gather information on vulnerabilities and lessons from testing and real incidents, and that findings feed back into the ICT risk management framework and are reported to the competent authority on request.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_13.html",
			Topics:  []string{PhaseAfterAction, "lessons_learned"},
		},
		{
			Key: "dora-art-14", Name: "DORA Article 14 — Communication",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Requires crisis communication plans for responsible disclosure of major incidents to clients, counterparts and the public; communication policies that distinguish staff who need to act from staff who need to know; and at least one named person tasked with implementing the communication strategy and fulfilling the public and media function.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_14.html",
			Topics:  []string{PhaseCommunications},
		},
		{
			Key: "dora-art-17", Name: "DORA Article 17 — ICT-related incident management process",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Requires a documented process to detect, manage and notify ICT-related incidents, with early warning indicators, roles and responsibilities per incident type, escalation procedures including to senior management and the management body, and procedures for handling internal and external communication.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_17.html",
			Topics:  []string{PhaseDetection, PhaseIncident},
		},
		{
			Key: "dora-art-18", Name: "DORA Article 18 — Classification of ICT-related incidents and cyber threats",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Requires incidents to be classified against criteria including the number of clients and financial counterparts affected, the geographical spread, data losses, criticality of services affected, duration and service downtime, and economic impact.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_18.html",
			Topics:  []string{PhaseClassification},
		},
		{
			Key: "dora-art-19", Name: "DORA Article 19 — Reporting of major ICT-related incidents",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Major ICT-related incidents must be reported to the competent authority as an initial notification, an intermediate report, and a final report. Voluntary notification of significant cyber threats is also provided for.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_19.html",
			Topics:  []string{PhaseClassification, PhaseAuthorities},
		},
		{
			Key: "dora-art-24", Name: "DORA Article 24 — Digital operational resilience testing programme",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Requires a sound, comprehensive testing programme as an integral part of the ICT risk management framework, risk-based, with independent testers and procedures to prioritise, classify and remediate what testing finds.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_24.html",
			Topics:  []string{"programme", PhaseRedTeam},
		},
		{
			Key: "dora-art-25", Name: "DORA Article 25 — Testing of ICT tools and systems",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Names the tests the programme may use, explicitly including scenario-based tests, end-to-end testing and penetration testing alongside vulnerability assessment, gap analysis and source code review, applied at least yearly to ICT systems supporting critical or important functions.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_25.html",
			Topics:  []string{"programme", "scenario_testing"},
		},
		{
			Key: "dora-art-26", Name: "DORA Article 26 — Advanced testing based on threat-led penetration testing",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Financial entities identified by the competent authority must carry out threat-led penetration testing at least every three years, covering several or all critical or important functions, performed on live production systems, including functions outsourced to ICT third-party providers.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_26.html",
			Topics:  []string{PhaseRedTeam, PhaseThreatIntel},
		},
		{
			Key: "dora-art-27", Name: "DORA Article 27 — Requirements for TLPT testers",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Sets the suitability, independence, certification and professional-indemnity requirements for testers carrying out threat-led penetration testing, and the conditions under which internal testers may be used.",
			URL:     "https://www.digital-operational-resilience-act.com/Article_27.html",
			Topics:  []string{PhaseRedTeam},
		},
		{
			Key: "dora-rts-classification", Name: "Commission Delegated Regulation (EU) 2024/1772 — RTS on incident classification",
			Issuer: "European Commission", Instrument: InstrumentRTS,
			Summary: "Specifies the materiality thresholds for the classification criteria in DORA Article 18. An incident is major where critical services are affected and either the data-losses criterion is met or two or more of the remaining criteria are.",
			URL:     "https://eur-lex.europa.eu/eli/reg_del/2024/1772/oj",
			Topics:  []string{PhaseClassification},
		},
		{
			Key: "dora-reporting-templates", Name: "DORA major incident reporting — content and templates",
			Issuer: "European Commission / ESAs", Instrument: InstrumentRTS,
			Summary: "The technical standards under DORA Article 20 fixing the content, forms and templates of the initial notification, intermediate report and final report, and the timelines on which each is due.",
			URL:     "https://www.eba.europa.eu/",
			Topics:  []string{PhaseAuthorities, PhaseClassification},
		},
		{
			Key: "dora-rts-tlpt", Name: "Commission Delegated Regulation (EU) 2025/1190 — RTS on threat-led penetration testing",
			Issuer: "European Commission", Instrument: InstrumentRTS,
			Summary: "Supplements DORA Article 26: the criteria for identifying entities required to perform TLPT, the requirements for test scope, methodology, phases and results, and the co-operation between authorities. Applicable across the Union since July 2025.",
			URL:     "https://eur-lex.europa.eu/eli/reg_del/2025/1190/oj",
			Topics:  []string{PhaseRedTeam, PhaseThreatIntel},
		},

		// ---- ECB / Eurosystem ----
		{
			Key: "tiber-eu", Name: "TIBER-EU Framework",
			Issuer: "European Central Bank", Instrument: InstrumentSupervisory,
			Summary: "The European framework for threat intelligence-based ethical red teaming. Runs in preparation, testing and closure phases, with a control (white) team, an unwitting blue team, a red team, a threat intelligence provider and the authority's TIBER cyber team, and fixed deliverables from the scope specification through to attestation for mutual recognition.",
			URL:     "https://www.ecb.europa.eu/paym/cyber-resilience/tiber-eu/html/index.en.html",
			Topics:  []string{PhaseThreatIntel, PhaseRedTeam},
		},
		{
			Key: "tiber-eu-purple", Name: "TIBER-EU Purple Teaming Best Practices",
			Issuer: "European Central Bank", Instrument: InstrumentGuidance,
			Summary: "How and when to introduce purple teaming into the testing or closure phase of a TIBER test, so that the red team's findings become detection improvements rather than a report.",
			URL:     "https://www.ecb.europa.eu/pub/pdf/other/ecb.tiber_eu_purple_best_practices.20220809~0b677a75c7.en.pdf",
			Topics:  []string{PhaseRedTeam, PhaseDetection},
		},
		{
			Key: "ecb-croe", Name: "Cyber Resilience Oversight Expectations for financial market infrastructures",
			Issuer: "European Central Bank", Instrument: InstrumentSupervisory,
			Summary: "The Eurosystem's expectations for FMI cyber resilience, built on the CPMI-IOSCO guidance, with a three-tier maturity model (evolving, advancing, innovating) applied on a meet-or-explain basis and covering governance, response and recovery, testing and situational awareness.",
			URL:     "https://www.ecb.europa.eu/paym/pdf/cons/cyberresilience/Cyber_resilience_oversight_expectations_for_financial_market_infrastructures.pdf",
			Topics:  []string{PhaseCrisis, PhaseContinuity, PhaseBoard, "maturity"},
		},
		{
			Key: "ecb-ssm", Name: "ECB Banking Supervision — cyber incident reporting for significant institutions",
			Issuer: "European Central Bank", Instrument: InstrumentSupervisory,
			Summary: "Significant institutions under direct ECB supervision report cyber incidents to the ECB through their Joint Supervisory Team, in addition to and separately from national and DORA obligations. Knowing which report goes where is the first thing a cross-border exercise should test.",
			URL:     "https://www.bankingsupervision.europa.eu/",
			Topics:  []string{PhaseAuthorities},
		},
		{
			Key: "esrb-recommendation", Name: "ESRB Recommendation on a pan-European systemic cyber incident coordination framework",
			Issuer: "European Systemic Risk Board", Instrument: InstrumentSupervisory,
			Summary: "Recommends that the ESAs build a coordination framework for systemic cyber incidents, on the reasoning that a cyber incident can become a financial stability event faster than existing crisis machinery can convene.",
			URL:     "https://www.esrb.europa.eu/news/pr/date/2022/html/esrb.pr.220127~f1548f677e.en.html",
			Topics:  []string{PhaseAuthorities, "systemic"},
		},
		{
			Key: "eu-scicf", Name: "EU Systemic Cyber Incident Coordination Framework (EU-SCICF)",
			Issuer: "Joint Committee of the ESAs", Instrument: InstrumentSupervisory,
			Summary: "The framework established by the ESAs to coordinate financial authorities' response to a systemic cyber incident, comprising a secretariat, a forum for maturing the arrangements, and crisis coordination during an event.",
			URL:     "https://www.esma.europa.eu/press-news/esma-news/esas-establish-framework-strengthen-coordination-case-systemic-cyber-incidents",
			Topics:  []string{PhaseAuthorities, "systemic"},
		},

		// ---- other EU law ----
		{
			Key: "nis2-art-23", Name: "NIS2 Directive (EU) 2022/2555 Article 23 — reporting obligations",
			Issuer: "European Union", Instrument: InstrumentDirective,
			Summary: "Significant incidents require an early warning to the CSIRT or competent authority within 24 hours, an incident notification within 72 hours, and a final report within one month. Financial entities are largely carved out in favour of DORA, but group companies, ICT providers and non-financial subsidiaries often are not — which is where a group exercise finds a gap.",
			URL:     "https://eur-lex.europa.eu/eli/dir/2022/2555/oj",
			Topics:  []string{PhaseAuthorities, PhaseClassification},
		},
		{
			Key: "gdpr-art-33", Name: "GDPR Article 33 — notification of a personal data breach to the supervisory authority",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "A personal data breach must be notified to the supervisory authority without undue delay and where feasible within 72 hours of becoming aware of it, unless it is unlikely to result in a risk to individuals.",
			URL:     "https://eur-lex.europa.eu/eli/reg/2016/679/oj",
			Topics:  []string{PhaseClassification, PhaseAuthorities},
		},
		{
			Key: "gdpr-art-34", Name: "GDPR Article 34 — communication of a personal data breach to the data subject",
			Issuer: "European Union", Instrument: InstrumentRegulation,
			Summary: "Where a breach is likely to result in a high risk to individuals, they must be told without undue delay, in clear and plain language. This is a communications obligation as much as a legal one, and it collides with every instinct a crisis communications team has.",
			URL:     "https://eur-lex.europa.eu/eli/reg/2016/679/oj",
			Topics:  []string{PhaseCommunications},
		},
		{
			Key: "eba-ict-guidelines", Name: "EBA Guidelines on ICT and security risk management (EBA/GL/2019/04)",
			Issuer: "European Banking Authority", Instrument: InstrumentSupervisory,
			Summary: "Supervisory expectations on ICT and security risk governance, incident management and business continuity for institutions, the vocabulary much of DORA's detail was built on.",
			URL:     "https://www.eba.europa.eu/",
			Topics:  []string{PhaseIncident, PhaseContinuity},
		},

		// ---- standards ----
		{
			Key: "iso-22301", Name: "ISO 22301 — Business continuity management systems",
			Issuer: "ISO", Instrument: InstrumentStandard,
			Summary: "The management-system standard for business continuity: business impact analysis, continuity strategies, plans, and the exercise and testing programme that validates them.",
			URL:     "https://www.iso.org/standard/75106.html",
			Topics:  []string{PhaseContinuity, PhaseRecovery},
		},
		{
			Key: "iso-22361", Name: "ISO 22361 — Crisis management guidelines for a strategic capability",
			Issuer: "ISO", Instrument: InstrumentStandard,
			Summary: "Guidance on building a strategic crisis management capability: the principles, the crisis management team and its decision-making, communication, and — in clause 9 — training, exercising and validation.",
			URL:     "https://www.iso.org/standard/50267.html",
			Topics:  []string{PhaseCrisis, PhaseBoard, PhaseCommunications},
		},
		{
			Key: "iso-22398", Name: "ISO 22398 — Guidelines for exercises",
			Issuer: "ISO", Instrument: InstrumentStandard,
			Summary: "The standard for the exercise programme itself: planning, conducting, evaluating, reporting and improving exercises, and the exercise designs used to assess readiness.",
			URL:     "https://www.iso.org/standard/50294.html",
			Topics:  []string{PhaseAfterAction, "programme"},
		},
		{
			Key: "iso-27035", Name: "ISO/IEC 27035 — Information security incident management",
			Issuer: "ISO/IEC", Instrument: InstrumentStandard,
			Summary: "The incident management lifecycle: plan and prepare, detect and report, assess and decide, respond, and learn lessons.",
			URL:     "https://www.iso.org/standard/78973.html",
			Topics:  []string{PhaseDetection, PhaseIncident},
		},
		{
			Key: "nist-sp-800-84", Name: "NIST SP 800-84 — Guide to Test, Training and Exercise Programs",
			Issuer: "NIST", Instrument: InstrumentGuidance,
			Summary: "The reference for exercise programme design: the discussion-based ladder (seminar, workshop, tabletop, game) and the operations-based ladder (drill, functional, full-scale), and how to design, conduct and evaluate each.",
			URL:     "https://csrc.nist.gov/pubs/sp/800/84/final",
			Topics:  []string{"programme", PhaseAfterAction},
		},
		{
			Key: "nist-sp-800-61r3", Name: "NIST SP 800-61r3 — Incident Response Recommendations and Considerations",
			Issuer: "NIST", Instrument: InstrumentGuidance,
			Summary: "The 2025 revision, recast as a CSF 2.0 community profile: incident response is treated as an outcome of all six CSF functions rather than a lifecycle bolted on beside them.",
			URL:     "https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.800-61r3.pdf",
			Topics:  []string{PhaseDetection, PhaseIncident, PhaseRecovery},
		},
		{
			Key: "nist-csf-2", Name: "NIST Cybersecurity Framework 2.0",
			Issuer: "NIST", Instrument: InstrumentGuidance,
			Summary: "Govern, Identify, Protect, Detect, Respond, Recover. The Govern function added in 2.0 is the one an exercise most often finds missing.",
			URL:     "https://www.nist.gov/cyberframework",
			Topics:  []string{"governance", PhaseDetection, PhaseRecovery},
		},
		{
			Key: "mitre-attack", Name: "MITRE ATT&CK",
			Issuer: "MITRE", Instrument: InstrumentGuidance,
			Summary: "The tactic and technique taxonomy that lets a scenario's attack path be written in the same language the defenders' detection coverage is measured in.",
			URL:     "https://attack.mitre.org/",
			Topics:  []string{PhaseRedTeam, PhaseDetection, PhaseThreatIntel},
		},

		// ---- professional guidance ----
		{
			Key: "isaca-exercise-guidance", Name: "ISACA — Cybersecurity Incident Response Exercise Guidance",
			Issuer: "ISACA", Instrument: InstrumentGuidance,
			Summary: "Design a scenario specific to the organisation's own systems, scope the roles and partners involved, set objectives tied to the incident response plan, and produce an after-action report carrying findings, observations, recommendations, lessons learned and an evaluation. At least annually.",
			URL:     "https://www.isaca.org/resources/isaca-journal/issues/2022/volume-1/cybersecurity-incident-response-exercise-guidance",
			Topics:  []string{"programme", PhaseAfterAction},
		},
		{
			Key: "isaca-five-considerations", Name: "ISACA — Five key considerations for crisis management and incident response tabletop exercises",
			Issuer: "ISACA", Instrument: InstrumentGuidance,
			Summary: "Use relatable, recently-precedented events; run a campaign of progressively harder exercises rather than a fresh one each year; document expected behaviour and track it against actual; inject stress events that remove what the plan assumed; and inject ambiguous and contradictory information, because that is what the first 72 hours are made of. Exercises the organisation always wins teach nothing.",
			URL:     "https://www.isaca.org/resources/news-and-trends/newsletters/atisaca/2023/volume-48/five-key-considerations-for-developing-crisis-management-and-incident-response-tabletop-exercises",
			Topics:  []string{"design", "stress", "ambiguity"},
		},
		{
			Key: "fsb-cirr", Name: "FSB Cyber Incident Response and Recovery toolkit",
			Issuer: "Financial Stability Board", Instrument: InstrumentGuidance,
			Summary: "Effective practices for cyber incident response and recovery across governance, planning, analysis, mitigation, restoration, coordination and improvement, written for financial institutions and their authorities.",
			URL:     "https://www.fsb.org/",
			Topics:  []string{PhaseIncident, PhaseRecovery, PhaseAuthorities},
		},
		{
			Key: "cpmi-iosco", Name: "CPMI-IOSCO Guidance on cyber resilience for financial market infrastructures",
			Issuer: "CPMI-IOSCO", Instrument: InstrumentGuidance,
			Summary: "The two-hour resumption expectation for critical FMI services after a cyber attack, and the governance, testing and situational awareness expected around it. The CROE is built on this.",
			URL:     "https://www.bis.org/cpmi/publ/d146.htm",
			Topics:  []string{PhaseContinuity, PhaseRecovery},
		},
		{
			Key: "g7-elements", Name: "G7 Fundamental Elements of Cybersecurity for the Financial Sector",
			Issuer: "G7 Cyber Expert Group", Instrument: InstrumentGuidance,
			Summary: "Non-binding building blocks covering strategy, governance, risk assessment, monitoring, response, recovery, information sharing and continuous learning — including dedicated elements on threat-led penetration testing and third-party cyber risk.",
			URL:     "https://home.treasury.gov/",
			Topics:  []string{"governance", PhaseBoard},
		},
		{
			Key: "enisa-finance-tl", Name: "ENISA Threat Landscape: Finance Sector",
			Issuer: "ENISA", Instrument: InstrumentGuidance,
			Summary: "The European threat picture for the financial sector, and the empirical basis for choosing a scenario that reflects what is actually happening rather than what is memorable.",
			URL:     "https://www.enisa.europa.eu/",
			Topics:  []string{PhaseThreatIntel},
		},

		// ---- Baltic national ----
		{
			Key: "lt-nksc", Name: "Lithuania — National Cyber Security Centre (NKSC) and CERT-LT",
			Issuer: "Republic of Lithuania", Instrument: InstrumentNational,
			Summary: "The national cyber security authority and CSIRT under the Ministry of National Defence, receiving incident notifications under the Lithuanian cyber security law transposing NIS2. Lietuvos bankas remains the competent authority for financial entities under DORA.",
			URL:     "https://www.nksc.lt/",
			Topics:  []string{PhaseAuthorities},
		},
		{
			Key: "lv-certlv", Name: "Latvia — CERT.LV and the National Cybersecurity Law",
			Issuer: "Republic of Latvia", Instrument: InstrumentNational,
			Summary: "Latvia's national CSIRT, receiving an early warning within 24 hours and a full incident communication within 72 hours from entities in scope of the National Cybersecurity Law transposing NIS2. Latvijas Banka is the competent authority for financial entities.",
			URL:     "https://cert.lv/",
			Topics:  []string{PhaseAuthorities},
		},
		{
			Key: "ee-ria", Name: "Estonia — RIA and CERT-EE",
			Issuer: "Republic of Estonia", Instrument: InstrumentNational,
			Summary: "The Information System Authority supervises the Estonian cybersecurity regime and CERT-EE receives significant incident reports. Finantsinspektsioon is the competent authority for financial entities.",
			URL:     "https://www.ria.ee/",
			Topics:  []string{PhaseAuthorities},
		},
		{
			Key: "nordic-baltic-exercise", Name: "Nordic-Baltic financial crisis simulation exercises",
			Issuer: "Nordic-Baltic Stability Group", Instrument: InstrumentGuidance,
			Summary: "The recurring cross-border exercises run by the ministries, central banks, supervisors and resolution authorities of Denmark, Estonia, Finland, Iceland, Latvia, Lithuania, Norway and Sweden with EU authorities, testing coordinated decision-making across borders under time pressure. The reference point for what a Baltic entity's own exercise should be able to plug into.",
			URL:     "https://www.riksbank.se/en-gb/press-and-published/notices-and-press-releases/notices/2025/report-from-nordic-baltic-financial-crisis-exercise/",
			Topics:  []string{PhaseAuthorities, "cross_border"},
		},
	}
}

// AuthorityByKey resolves a citation key.
func AuthorityByKey(key string) (Authority, bool) {
	key = strings.TrimSpace(key)
	for _, a := range Authorities() {
		if a.Key == key {
			return a, true
		}
	}
	return Authority{}, false
}

// AuthoritiesForTopic returns the citations bearing on a phase key or theme,
// which is what the reference picker offers first when someone is editing that
// part of an exercise.
func AuthoritiesForTopic(topic string) []Authority {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil
	}
	var out []Authority
	for _, a := range Authorities() {
		for _, t := range a.Topics {
			if t == topic {
				out = append(out, a)
				break
			}
		}
	}
	return out
}
