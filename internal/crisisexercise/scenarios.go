package crisisexercise

import "strings"

// This file holds the choices an exercise is built from — its format, its
// audience, the entity and jurisdiction it is run for — and the scenario
// library.
//
// The scenarios are written for a Baltic financial institution because that is
// a specific and unusually demanding place to run one: three small, highly
// digitised markets with concentrated banking sectors, a shared border with a
// hostile state, sustained hacktivist and hybrid pressure on exactly the
// infrastructure a bank depends on, and a supervisory perimeter where the
// national authority, the ECB, two CSIRTs and a data protection authority can
// all have a claim on the same incident within the first six hours.

// ---- formats ----

// Format is an exercise type. The ladder is NIST SP 800-84's, plus the two
// testing formats DORA and TIBER-EU name.
type Format struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	// Summary says what the format is good for, and Cost roughly what it takes
	// to run — the two facts that decide whether an entity picks it.
	Summary string `json:"summary"`
	Cost    string `json:"cost"`
	// DefaultMinutes is a realistic duration, used to lay out the arc.
	DefaultMinutes int `json:"default_minutes"`
}

// Formats is the exercise ladder, easiest first. An entity should climb it
// rather than start at the top: a full-scale exercise run by a team that has
// never done a tabletop produces chaos and a report nobody believes.
func Formats() []Format {
	return []Format{
		{"seminar", "Seminar", KindDiscussion,
			"Instructor-led orientation to a plan or a threat. No decisions, no clock. The right format when the plan is new or the audience is.",
			"Low — one facilitator, one to two hours.", 90},
		{"workshop", "Workshop", KindDiscussion,
			"Builds something: a playbook, a call tree, a classification threshold. Output-focused rather than assessment-focused.",
			"Low — half a day, with the people who own the output.", 180},
		{"tabletop", "Tabletop", KindDiscussion,
			"Discussion-based play against a scripted scenario with injects on a clock. The workhorse format, and the one most exercises should be.",
			"Moderate — a facilitator, an evaluator, a scripted MSEL, half a day of participants' time.", 240},
		{"game", "Decision game", KindDiscussion,
			"Competing teams or an adversarial facilitator, with consequences that follow from the players' choices rather than a fixed script. Best for board and executive audiences, who disengage from scripts.",
			"Moderate to high — needs a facilitator who can adjudicate consequences live.", 180},
		{"drill", "Drill", KindOperations,
			"A single capability performed for real: call-tree activation, failover of one service, restoration of one system from backup.",
			"Moderate — real systems, a maintenance window, an owner willing to be measured.", 120},
		{"functional", "Functional exercise", KindOperations,
			"Real people using real tools against a simulated event, coordinated from an exercise control cell. Systems respond; the impact is simulated.",
			"High — a control cell, a simulation environment, and a day of the response team's time.", 480},
		{"full_scale", "Full-scale exercise", KindOperations,
			"Multi-team, multi-site, real deployment and real recovery, usually with third parties and sometimes with authorities. The only format that finds the failures which live in scale.",
			"Very high — weeks of planning, executive sponsorship, and a genuine appetite to fail in front of people.", 600},
		{"tlpt", "Threat-led penetration test (TIBER-EU / DORA Art. 26)", KindOperations,
			"Intelligence-led red teaming against live production systems with an unwitting blue team, overseen by the authority's cyber team. Not an exercise in the training sense — an assessment — but the natural first phase of one.",
			"Very high — 12 weeks or more end to end, external threat intelligence and red team providers, board sign-off.", 600},
		{"purple_team", "Purple team replay", KindOperations,
			"Red and blue working the attack path together, replaying each step until detection fires. Converts a red-team report into detection engineering.",
			"Moderate — the red team's attack path, the blue team's tooling, and two to three days.", 480},
		{"hybrid", "Hybrid — red team into crisis exercise", KindOperations,
			"A red-team detonation whose impact is handed to a crisis management exercise at the moment of detection. The format this module is built for: it is the only one that tests the seam between the technical and the strategic.",
			"High — combines a testing engagement with a facilitated exercise, but tests what neither finds alone.", 480},
	}
}

// FormatByKey resolves a format.
func FormatByKey(key string) (Format, bool) {
	key = strings.TrimSpace(key)
	for _, f := range Formats() {
		if f.Key == key {
			return f, true
		}
	}
	return Format{}, false
}

// ---- audiences ----

// Audience decides how injects should be written, which matters more than any
// other single design choice: the same event delivered as a SIEM alert and as a
// journalist's voicemail exercises two different institutions.
type Audience struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Focus is what this audience should be tested on, and Pitfall the
	// characteristic way an exercise for them goes wrong.
	Focus   string `json:"focus"`
	Pitfall string `json:"pitfall"`
}

// Audiences is the seat-height ladder.
func Audiences() []Audience {
	return []Audience{
		{"technical", "Technical — SOC, IR, IT operations",
			"Detection, triage, containment, evidence handling, and the decision to escalate.",
			"Letting the room solve the intrusion in satisfying detail and never reaching the escalation that was the point."},
		{"management", "Management — crisis management team",
			"Coordination, trade-offs between security and service, classification, and keeping one version of events.",
			"Rehearsing the meeting rather than the decisions. A crisis team that only agrees to reconvene has not been exercised."},
		{"board", "Board — management body and executive committee",
			"Reserved decisions, the entity's external position, appetite for risk under pressure, and whether the board is being given decisions or reassurance.",
			"Briefing the board instead of exercising it. If nobody in the room has to choose something uncomfortable, it was a presentation."},
		{"cross_entity", "Cross-entity — with third parties, group or authorities",
			"Handovers across organisational boundaries: the outsourced provider, the group parent, the competent authority, the CSIRT.",
			"Simulating the other party with someone from your own team, which quietly removes the friction that is the whole point."},
		{"full_organisation", "Full organisation — end to end",
			"The complete arc, from the first alert to the board's public position and the supervisory notification.",
			"Running out of time at the classification phase and never reaching the board, so the exercise reports on the half it managed."},
	}
}

// ---- entity types and jurisdictions ----

// EntityType is the DORA financial-entity category, which drives which
// obligations attach.
type EntityType struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Note  string `json:"note"`
}

// EntityTypes covers the categories a Baltic exercise is realistically run for.
func EntityTypes() []EntityType {
	return []EntityType{
		{"credit_institution", "Credit institution (bank)", "Full DORA scope; ECB-supervised if significant, otherwise supervised by the national competent authority within the SSM."},
		{"payment_institution", "Payment institution", "Full DORA scope. In Lithuania this is the largest population of licensed entities in the region and includes several pan-European e-money issuers."},
		{"emoney_institution", "Electronic money institution", "Full DORA scope; safeguarding-account access is usually the critical function that matters first."},
		{"investment_firm", "Investment firm", "Full DORA scope; market-facing obligations run alongside the ICT ones."},
		{"insurance", "Insurance or reinsurance undertaking", "Full DORA scope, supervised by the national authority; claims and policy administration are the usual critical functions."},
		{"fmi", "Financial market infrastructure (CSD, CCP, payment system)", "Also in scope of the ECB's Cyber Resilience Oversight Expectations and the CPMI-IOSCO two-hour resumption expectation."},
		{"ict_provider", "ICT third-party service provider", "Not a financial entity, but may be designated critical; exercises here are usually run at a client's request or under a contractual testing right."},
		{"group", "Cross-border group", "The interesting case in the Baltics: one licence in one country, branches or subsidiaries in the other two, and three CSIRTs with a claim on the same incident."},
	}
}

// Jurisdiction is a supervisory perimeter, with the authorities an incident in
// it has to be reported to.
//
// This is the part of the module that is genuinely Baltic-specific. The three
// countries look similar from outside and are not: they have separate CSIRTs on
// separate legal bases, separate data protection authorities, separate NIS2
// transpositions with different commencement dates, and three different
// languages in which a customer-facing statement has to be credible — with a
// substantial Russian-speaking population in two of them whose exclusion from
// a crisis communication is itself a reputational event.
type Jurisdiction struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// CompetentAuthority is the DORA competent authority for financial
	// entities. CSIRT is the national NIS2 recipient. DPA is the GDPR
	// supervisory authority.
	CompetentAuthority string `json:"competent_authority"`
	CSIRT              string `json:"csirt"`
	DPA                string `json:"dpa"`
	// DepositGuarantee matters because the first question a retail crisis
	// produces is whether deposits are safe, and the answer belongs to someone
	// outside the bank.
	DepositGuarantee string `json:"deposit_guarantee"`
	// Languages are the languages a customer-facing statement has to exist in
	// before it is released, not after.
	Languages []string `json:"languages"`
	// Notes carries the practitioner detail a facilitator needs.
	Notes string `json:"notes"`
}

// Jurisdictions covers the three Baltic states, the cross-border case, and a
// generic euro-area fallback.
func Jurisdictions() []Jurisdiction {
	return []Jurisdiction{
		{
			Key: "LT", Label: "Lithuania",
			CompetentAuthority: "Lietuvos bankas (Bank of Lithuania)",
			CSIRT:              "NKSC / CERT-LT (National Cyber Security Centre, Ministry of National Defence)",
			DPA:                "Valstybinė duomenų apsaugos inspekcija (State Data Protection Inspectorate)",
			DepositGuarantee:   "VĮ „Indėlių ir investicijų draudimas“",
			Languages:          []string{"Lithuanian", "English"},
			Notes: "The largest licensed fintech and payment-institution population in the region, much of it passporting across the EU, " +
				"which means a Lithuanian incident is frequently a multi-market incident on day one. Lietuvos bankas supervises the " +
				"non-significant institutions and participates in the SSM for the significant ones.",
		},
		{
			Key: "LV", Label: "Latvia",
			CompetentAuthority: "Latvijas Banka (which absorbed the FCMC in 2023)",
			CSIRT:              "CERT.LV",
			DPA:                "Datu valsts inspekcija (Data State Inspectorate)",
			DepositGuarantee:   "Noguldījumu garantiju fonds, administered by Latvijas Banka",
			Languages:          []string{"Latvian", "English", "Russian"},
			Notes: "CERT.LV expects an early warning within 24 hours and a full communication within 72 hours under the National " +
				"Cybersecurity Law. A statement issued only in Latvian will be read in Russian anyway, by someone else, with their " +
				"framing — plan the Russian-language line rather than inheriting one.",
		},
		{
			Key: "EE", Label: "Estonia",
			CompetentAuthority: "Finantsinspektsioon",
			CSIRT:              "CERT-EE, within the Information System Authority (RIA)",
			DPA:                "Andmekaitse Inspektsioon",
			DepositGuarantee:   "Tagatisfond (Guarantee Fund)",
			Languages:          []string{"Estonian", "English", "Russian"},
			Notes: "The most digitised of the three: e-ID and state authentication infrastructure sit inside the customer journey, " +
				"so an outage at a national service becomes a bank outage without the bank being attacked. Worth building at least " +
				"one scenario around a dependency the entity does not own and cannot fix.",
		},
		{
			Key: "BALTIC", Label: "Baltic cross-border group",
			CompetentAuthority: "Home-state competent authority, with host authorities in the other two states",
			CSIRT:              "CERT-LT, CERT.LV and CERT-EE, each with its own trigger and clock",
			DPA:                "Lead supervisory authority under GDPR one-stop-shop, plus concerned authorities",
			DepositGuarantee:   "Home-state scheme, with host-state disclosure obligations",
			Languages:          []string{"Lithuanian", "Latvian", "Estonian", "English", "Russian"},
			Notes: "The genuinely hard case, and the one worth exercising. A single core-banking outage becomes three national " +
				"notifications on three triggers, a lead-authority determination nobody has made in advance, and a consolidated " +
				"message that has to be true in three markets at once. The Nordic-Baltic crisis simulation exercises exist because " +
				"this does not work by itself.",
		},
		{
			Key: "EU", Label: "Euro area — generic",
			CompetentAuthority: "National competent authority, or the ECB for significant institutions",
			CSIRT:              "National CSIRT under the NIS2 transposition",
			DPA:                "National data protection supervisory authority",
			DepositGuarantee:   "National deposit guarantee scheme",
			Languages:          []string{"Local language", "English"},
			Notes:              "Use when the exercise is not tied to one of the Baltic states.",
		},
	}
}

// JurisdictionByKey resolves a jurisdiction, falling back to the generic euro
// area entry so a scenario always has authorities to name.
func JurisdictionByKey(key string) Jurisdiction {
	key = strings.ToUpper(strings.TrimSpace(key))
	list := Jurisdictions()
	for _, j := range list {
		if j.Key == key {
			return j
		}
	}
	return list[len(list)-1]
}

// ---- scenario library ----

// Scenario is a seeded starting point: the intelligence, the attack path and
// the objectives, ready to be adapted to an entity.
//
// Every one of these is drawn from something that has actually happened to a
// European financial institution or to Baltic critical infrastructure. That is
// ISACA's first design rule and it is not a stylistic preference: a scenario a
// board considers far-fetched gets argued with instead of played, and the
// argument is where the exercise dies.
type Scenario struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// ThreatActor and InitialVector seed the exercise's own fields.
	ThreatActor   string `json:"threat_actor"`
	InitialVector string `json:"initial_vector"`
	Narrative     string `json:"narrative"`
	// CriticalFunctions is a suggested scope, one per line.
	CriticalFunctions string `json:"critical_functions"`
	// Objectives are the capabilities this scenario is good at testing.
	Objectives []ScenarioObjective `json:"objectives"`
	// SuggestedFormat and SuggestedAudience are where this scenario works best.
	SuggestedFormat   string `json:"suggested_format"`
	SuggestedAudience string `json:"suggested_audience"`
	// Authorities seeds the exercise-level citations.
	Authorities []string `json:"authorities"`
	// Jurisdictions names where this scenario is most relevant; empty means
	// anywhere.
	Jurisdictions []string `json:"jurisdictions,omitempty"`
	// TrapDoor is the thing this scenario is really designed to expose — the
	// question the facilitator is steering towards. It is shown to the control
	// team and withheld from players.
	TrapDoor string `json:"trap_door"`
}

// ScenarioObjective is a seeded objective with its success criteria.
type ScenarioObjective struct {
	Text            string   `json:"text"`
	Capability      string   `json:"capability"`
	SuccessCriteria string   `json:"success_criteria"`
	Authorities     []string `json:"authorities,omitempty"`
}

// ScenarioLibrary is the seeded scenario set.
func ScenarioLibrary() []Scenario {
	return []Scenario{
		{
			Key:     "ransomware-core-banking",
			Name:    "Ransomware in the core banking platform",
			Summary: "A financially motivated group encrypts the core banking and card authorisation estate over a weekend, having first exfiltrated customer data, and demands payment against a public leak-site countdown.",
			ThreatActor: "A ransomware-as-a-service affiliate with an established leak site and a track record against European mid-market banks. " +
				"Double extortion: exfiltrate first, encrypt second, publish on a timer.",
			InitialVector: "Credentials for a third-party maintenance account, bought from an initial access broker and used through a VPN concentrator with no phishing-resistant second factor.",
			Narrative: "Intrusion begins on a Thursday evening. The actor moves laterally through a flat administrative network to the virtualisation layer, " +
				"identifies and deletes the online backup catalogue, exfiltrates 340 GB over four days, and detonates at 02:10 on Sunday — timed for the " +
				"thinnest staffing of the week. Card authorisation degrades first and is the first thing customers notice. By 07:00 the leak site carries " +
				"the institution's name and a seven-day countdown.",
			CriticalFunctions: "Core banking ledger\nCard authorisation and switching\nInternet and mobile banking\nSEPA payment initiation and settlement\nCustomer data platform",
			Objectives: []ScenarioObjective{
				{"Detect and contain a ransomware detonation outside business hours without destroying the evidence needed to answer the regulator.",
					"Out-of-hours detection, containment and forensic preservation",
					"Containment decision taken within 60 minutes of first alert, with the forensic preservation decision explicitly recorded before any rebuild begins.",
					[]string{"dora-art-17", "nist-sp-800-61r3"}},
				{"Classify the incident against the DORA materiality criteria on partial information and start every applicable clock.",
					"Major incident classification under time pressure",
					"A classification decision with rationale is recorded, and the initial notification is drafted within four hours of it.",
					[]string{"dora-art-18", "dora-rts-classification", "dora-art-19"}},
				{"Take the ransom decision at the level of the organisation entitled to take it.",
					"Reserved decision authority in the management body",
					"The decision is made and minuted with the sanctions, legal and insurance positions stated, and the authority for it identified.",
					[]string{"dora-art-5", "iso-22361"}},
				{"Sustain retail payments in degraded mode for longer than the continuity plan assumes.",
					"Degraded-mode operation beyond planned duration",
					"The team states the capacity and duration limit of each workaround, and identifies what fails on day three.",
					[]string{"iso-22301", "dora-art-11"}},
			},
			SuggestedFormat:   "hybrid",
			SuggestedAudience: "full_organisation",
			Authorities:       []string{"dora-art-17", "dora-art-18", "dora-art-19", "dora-art-11", "iso-22301", "nist-sp-800-61r3"},
			TrapDoor: "The backup catalogue was deleted before encryption, so the recovery time objective everyone has been quoting is fiction. " +
				"Steer the exercise to the moment someone asks how the RTO was last validated, and let the answer be 'in a document'.",
		},
		{
			Key:     "hacktivist-ddos-wave",
			Name:    "Sustained hacktivist DDoS against public banking channels",
			Summary: "A pro-Russian hacktivist collective runs a multi-day DDoS campaign against Baltic banks, coordinated on Telegram and timed to a NATO exercise, with a claimed data breach layered on top to amplify the effect.",
			ThreatActor: "A volunteer-crowdsourced DDoS collective in the NoName057(16) mould: thousands of participants running a distributed attack tool, " +
				"target lists published in advance, and a propaganda channel that treats any outage as proof of success.",
			InitialVector: "Volumetric and application-layer DDoS against public web and mobile banking endpoints, rotating targets across the sector every few hours.",
			Narrative: "The campaign is announced the day before on Telegram, naming six institutions across the three Baltic states. Day one takes the public " +
				"website down for 40 minutes before scrubbing engages. Day two shifts to the mobile banking API — below the volumetric threshold, above the " +
				"application one. On day three the channel claims to have exfiltrated customer data and posts a file of plausible-looking records. National " +
				"media pick up the claim before the bank has assessed it, and the contact centre queue triples.",
			CriticalFunctions: "Internet banking\nMobile banking API\nPublic website and customer authentication\nContact centre telephony",
			Objectives: []ScenarioObjective{
				{"Distinguish a service-availability event from a data breach when a hostile source asserts both.",
					"Assessment of unverified breach claims",
					"The team states publicly only what it has verified, and has a documented basis for that verification within the first four hours.",
					[]string{"dora-art-14", "gdpr-art-33"}},
				{"Decide whether a repeated availability event crosses the major-incident threshold when no single occurrence does.",
					"Cumulative and recurring incident classification",
					"The team considers recurrence explicitly against the classification criteria rather than assessing each day in isolation.",
					[]string{"dora-art-18", "dora-rts-classification"}},
				{"Communicate in every language the customer base actually uses, on the same clock.",
					"Multilingual crisis communication",
					"Customer-facing statements are released in all market languages simultaneously, and the contact-centre script matches them.",
					[]string{"dora-art-14"}},
				{"Coordinate with the national CSIRT and with peer institutions under coordinated attack.",
					"Sector coordination and CSIRT engagement",
					"Contact with the national CSIRT is made and logged, and the entity knows what it can and cannot share with peers.",
					[]string{"lv-certlv", "lt-nksc", "ee-ria", "nis2-art-23"}},
			},
			SuggestedFormat:   "tabletop",
			SuggestedAudience: "management",
			Authorities:       []string{"dora-art-14", "dora-art-18", "nis2-art-23", "enisa-finance-tl"},
			Jurisdictions:     []string{"LT", "LV", "EE", "BALTIC"},
			TrapDoor: "The breach claim is false, but the bank cannot prove it quickly, and its instinct is to deny. Watch for a denial issued " +
				"before verification — the reputational damage in this scenario comes from the correction, not the attack.",
		},
		{
			Key:           "core-provider-compromise",
			Name:          "Compromise at the outsourced core banking provider",
			Summary:       "The regional provider that runs core banking for several Baltic institutions is compromised. The bank is not attacked, is not in control of the investigation, and is nonetheless the entity its customers and its supervisor will hold responsible.",
			ThreatActor:   "An access-broker-to-espionage pipeline targeting managed service providers for the reach a single compromise gives across their client base.",
			InitialVector: "Compromise of the provider's shared administrative tooling, giving simultaneous access into multiple client environments.",
			Narrative: "The provider notifies the bank at 16:40 on a Friday: 'a security event affecting our hosting environment, investigation ongoing, no evidence of " +
				"client data access at this time'. No indicators of compromise. No timeline. The provider will not permit the bank's own forensic team on site. " +
				"Two other banks in the region receive the same notice. By Monday a journalist has connected them.",
			CriticalFunctions: "Core banking ledger (outsourced)\nPayment initiation (outsourced)\nCustomer data hosted at the provider\nProvider administrative access paths into the bank",
			Objectives: []ScenarioObjective{
				{"Manage an incident the entity cannot investigate itself.",
					"Third-party incident governance and contractual leverage",
					"The team identifies its contractual audit, access and notification rights and exercises them, rather than waiting for updates.",
					[]string{"dora-art-17", "fsb-cirr"}},
				{"Classify and report an incident whose facts are held by someone else.",
					"Classification on third-party-supplied facts",
					"The entity classifies on what it knows, states its uncertainty in the notification, and does not treat the provider's reassurance as a finding of fact.",
					[]string{"dora-art-18", "dora-art-19"}},
				{"Establish what the entity would do if the provider's environment could not be trusted at all.",
					"Exit and substitutability under stress",
					"The team states how long it could operate without the provider, and what the realistic exit path is.",
					[]string{"dora-art-11", "iso-22301"}},
				{"Decide what to tell customers about an event at a company they have never heard of.",
					"Communicating third-party incidents",
					"A statement exists that does not shift blame to the provider and does not overstate the entity's own knowledge.",
					[]string{"dora-art-14"}},
			},
			SuggestedFormat:   "tabletop",
			SuggestedAudience: "management",
			Authorities:       []string{"dora-art-17", "dora-art-19", "dora-art-11", "fsb-cirr"},
			TrapDoor: "The bank's own incident process assumes it controls the investigation. Every step of its playbook — scope, contain, eradicate — " +
				"is an instruction to do something it has no ability to do. Let the team run the playbook until they notice.",
		},
		{
			Key:     "hybrid-infrastructure-isolation",
			Name:    "Hybrid attack — connectivity loss and a data centre cut off",
			Summary: "Submarine cable damage and regional power disruption isolate the bank's primary data centre while a cyber intrusion runs concurrently, and the state's response occupies the same channels the bank needs.",
			ThreatActor: "State-aligned hybrid activity: physical interference with subsea infrastructure, GNSS interference across the region, and concurrent cyber " +
				"operations against financial and energy targets. Deniable by design.",
			InitialVector: "Concurrent physical and cyber pressure — cable damage affecting international connectivity, and a cyber intrusion that the connectivity loss makes harder to investigate.",
			Narrative: "Two submarine cables are damaged within six hours. International connectivity degrades, latency to cloud services becomes unusable, and the bank's " +
				"primary data centre falls back to a secondary link already at capacity. During the same window the SOC loses telemetry from the affected site — " +
				"which may be the connectivity, or may be an actor clearing the way. National authorities are managing a national event and the bank is not their " +
				"first priority. Customers begin asking whether their money is safe.",
			CriticalFunctions: "Data centre interconnect and international connectivity\nCloud-hosted security monitoring\nCard authorisation to international schemes\nBranch and ATM network\nCustomer confidence and deposit stability",
			Objectives: []ScenarioObjective{
				{"Operate the incident process with degraded telemetry and no assurance that the degradation is benign.",
					"Response under lost observability",
					"The team plans on the assumption that the outage may be adversarial, without asserting it publicly.",
					[]string{"nist-sp-800-61r3", "dora-art-17"}},
				{"Reach the crisis team and the authorities when the usual channels are affected.",
					"Out-of-band communication and assembly",
					"An out-of-band channel is used successfully, and it is one people had used before that day.",
					[]string{"iso-22361", "dora-art-14"}},
				{"Manage a deposit-confidence event that has no cyber cause and every cyber symptom.",
					"Liquidity and confidence under public uncertainty",
					"Treasury and communications act on the same picture, and the deposit guarantee position is stated correctly and early.",
					[]string{"iso-22361"}},
				{"Interact with authorities who are running a national event of their own.",
					"Authority engagement during a national crisis",
					"The entity knows who to reach, what it owes them, and what it is entitled to ask for.",
					[]string{"nis2-art-23", "eu-scicf", "esrb-recommendation"}},
			},
			SuggestedFormat:   "game",
			SuggestedAudience: "board",
			Authorities:       []string{"iso-22361", "eu-scicf", "esrb-recommendation", "nordic-baltic-exercise"},
			Jurisdictions:     []string{"LT", "LV", "EE", "BALTIC"},
			TrapDoor: "Nobody can say whether this is an attack or an accident, and the board wants to be told. The exercise is about deciding and " +
				"communicating without that answer — and about noticing that the honest public position is 'we do not yet know', which almost " +
				"nobody is willing to say out loud.",
		},
		{
			Key:           "payment-fraud-insider",
			Name:          "Insider-enabled payment fraud discovered mid-settlement",
			Summary:       "A senior operations employee, recruited through a personal-pressure channel, has been altering payment instructions. Discovery comes from a counterparty during a settlement window.",
			ThreatActor:   "An organised group using recruitment and coercion of insiders in preference to intrusion, targeting operations staff with payment authority.",
			InitialVector: "Legitimate access, used illegitimately. No malware, no intrusion, and nothing for the SOC to detect.",
			Narrative: "A correspondent bank queries three high-value transfers with beneficiary details that do not match the instructions on file. Reconstruction shows " +
				"eleven altered payments over five weeks, totalling several million euro, all authorised within policy by a long-serving employee who is in the " +
				"building. Legal wants privilege, HR wants process, operations wants the payment rail kept open, and the police want the person not to know.",
			CriticalFunctions: "Payment initiation and authorisation\nSettlement with correspondent banks\nAccess governance for privileged operations roles\nEvidence handling and employment process",
			Objectives: []ScenarioObjective{
				{"Run an incident where the subject is an employee present in the building.",
					"Insider incident handling with legal and HR constraints",
					"Evidence preservation, access suspension and employment process are sequenced deliberately rather than improvised.",
					[]string{"iso-27035", "dora-art-17"}},
				{"Decide whether to suspend a payment rail that is functioning correctly.",
					"Balancing containment against service continuity",
					"The decision is taken by someone with authority to take it, with the customer and counterparty impact stated.",
					[]string{"iso-22361"}},
				{"Classify a fraud event that is also an ICT-related incident, and notify the right authorities.",
					"Classification across overlapping regimes",
					"The team separates the fraud, ICT and personal-data obligations rather than treating them as one notification.",
					[]string{"dora-art-18", "gdpr-art-33"}},
				{"Coordinate with law enforcement without compromising the internal investigation.",
					"Law enforcement engagement",
					"The entity knows who it calls, what it hands over, and what it must not do first.",
					[]string{"fsb-cirr"}},
			},
			SuggestedFormat:   "tabletop",
			SuggestedAudience: "management",
			Authorities:       []string{"dora-art-17", "dora-art-18", "gdpr-art-33", "iso-27035"},
			TrapDoor: "Every technical control worked. The exercise is about what the institution does when the failure is a person, and about " +
				"the four-way collision between legal privilege, HR process, criminal investigation and the regulator's four-hour clock.",
		},
		{
			Key:           "supply-chain-update",
			Name:          "Malicious update from a trusted software vendor",
			Summary:       "A signed update from a widely deployed vendor carries a backdoor. The bank installed it inside its change window, correctly, on schedule, everywhere.",
			ThreatActor:   "A state-aligned espionage actor with a long dwell time and an interest in payment flows and correspondent relationships rather than in disruption.",
			InitialVector: "A legitimately signed vendor update, distributed through the vendor's own channel and installed by the bank's own change process.",
			Narrative: "A national CSIRT advisory names the vendor at 08:00. The bank's asset inventory says the product is deployed on 340 systems, and the inventory " +
				"was last verified eleven months ago. The dwell time may be four months. There is no indication whether this institution was targeted or merely " +
				"included. The vendor's advice is to await a patch. The supervisor asks, by lunchtime, what the bank's exposure is.",
			CriticalFunctions: "Software supply chain and change management\nAsset inventory and configuration management\nPayment infrastructure\nPrivileged access paths",
			Objectives: []ScenarioObjective{
				{"Answer 'what is our exposure' in hours rather than weeks.",
					"Asset inventory under operational pressure",
					"The team produces a defensible exposure statement with its uncertainty quantified, and knows why the inventory is not better.",
					[]string{"nist-csf-2", "dora-art-25"}},
				{"Hunt for an actor who has had months, using indicators published minutes ago.",
					"Retrospective threat hunting",
					"Log retention is checked against the suspected dwell time before the hunt is scoped, not after it fails.",
					[]string{"nist-sp-800-61r3", "mitre-attack"}},
				{"Decide whether to keep a compromised-but-essential product running.",
					"Risk acceptance under active compromise",
					"An explicit, time-bounded, documented acceptance is taken by someone entitled to take it.",
					[]string{"dora-art-5"}},
				{"Report to the supervisor when the honest answer is that the entity does not yet know.",
					"Supervisory communication under uncertainty",
					"The notification states what is known, what is not, and by when the entity will know more — and the entity then meets that date.",
					[]string{"dora-art-19", "dora-reporting-templates"}},
			},
			SuggestedFormat:   "functional",
			SuggestedAudience: "technical",
			Authorities:       []string{"dora-art-25", "nist-sp-800-61r3", "mitre-attack", "dora-art-19"},
			TrapDoor: "Log retention is 30 days and the dwell time is 120. The question the exercise is steering towards is what the institution " +
				"is prepared to say publicly about a period it cannot see into.",
		},
		{
			Key:           "tlpt-detonation",
			Name:          "TIBER-EU red team reaches the payment rail",
			Summary:       "A threat-led penetration test under the TIBER-EU framework reaches its flag. The blue team never detected it. The exercise begins at the moment the control team decides to tell them.",
			ThreatActor:   "A red team executing an attack path derived from targeted threat intelligence on the entity, emulating a named actor's tactics, techniques and procedures.",
			InitialVector: "As specified in the red team test plan — typically a spear-phishing or supply-chain foothold, escalated over weeks to the target flag.",
			Narrative: "Over eleven weeks the red team achieves the flag: the ability to originate a payment instruction. The blue team raised two low-severity alerts, " +
				"both closed as false positives. The control team now runs the replay: what should have fired, what did fire, what was closed and why. Then the " +
				"harder question — the same access, in a real actor's hands, on a real day.",
			CriticalFunctions: "Payment origination and authorisation\nPrivileged access management\nDetection and response coverage\nIdentity infrastructure",
			Objectives: []ScenarioObjective{
				{"Replay the attack path step by step and establish where detection should have fired.",
					"Purple team replay and detection engineering",
					"Every step is mapped to a detection outcome — fired, fired and dismissed, not fired, not possible — with an owner for each gap.",
					[]string{"tiber-eu-purple", "mitre-attack"}},
				{"Turn an undetected test into a remediation plan with dates and owners.",
					"Remediation planning under TIBER closure",
					"A remediation plan exists with named owners and dates, and the board has seen it.",
					[]string{"tiber-eu", "dora-art-26", "dora-rts-tlpt"}},
				{"Brief the board on a failed test without either minimising it or triggering an overreaction.",
					"Board communication of adverse test results",
					"The board receives the finding, its business meaning, the remediation plan and its cost, and takes a decision on the budget.",
					[]string{"dora-art-5", "dora-art-24"}},
				{"Run the crisis exercise the test made possible: the same access, but real.",
					"Crisis response to a proven attack path",
					"The full arc is exercised from the point of detonation, using the real attack path rather than an invented one.",
					[]string{"dora-art-11", "iso-22361"}},
			},
			SuggestedFormat:   "hybrid",
			SuggestedAudience: "full_organisation",
			Authorities:       []string{"tiber-eu", "tiber-eu-purple", "dora-art-26", "dora-art-27", "dora-rts-tlpt"},
			TrapDoor: "Two alerts fired and were closed. The interesting failure is not the missing detection, it is the analyst who had the " +
				"evidence and the process that told them it was noise — and that is a governance finding, not a tooling one.",
		},
		{
			Key:           "disinformation-deposit-run",
			Name:          "Coordinated disinformation and a digital deposit run",
			Summary:       "A fabricated claim that the bank is insolvent spreads through social channels and messaging apps, amplified deliberately, and instant payments make the resulting outflow faster than any crisis process.",
			ThreatActor:   "An influence operation using fabricated documents, synthetic audio of a named executive, and coordinated amplification — with or without an accompanying technical component.",
			InitialVector: "Information, not intrusion. The technical estate may be entirely healthy throughout.",
			Narrative: "A forged supervisory letter appears on a messaging channel at 19:00 on a Friday, alongside synthetic audio of the CFO discussing a capital shortfall. " +
				"Within ninety minutes it is on regional news aggregators. Instant payment outflows rise sharply and the mobile app slows under load, which the " +
				"channel presents as proof. The supervisor calls. Correcting the record requires saying things about the bank's capital position that are " +
				"price-sensitive, and it is Friday evening.",
			CriticalFunctions: "Deposit base and liquidity\nInstant payment rails\nMobile and internet banking capacity\nPublic communications and executive reputation\nSupervisory relationship",
			Objectives: []ScenarioObjective{
				{"Respond to a crisis with no technical cause using a crisis process built for technical causes.",
					"Crisis activation for non-technical events",
					"The crisis is declared on impact rather than on cause, and the right people are in the room despite there being no incident ticket.",
					[]string{"iso-22361"}},
				{"Correct a fabricated claim faster than it spreads, in every relevant language.",
					"Rapid public correction and multilingual reach",
					"A correction is issued within 90 minutes, in every market language, on the channels where the claim is actually spreading.",
					[]string{"dora-art-14"}},
				{"Coordinate with the supervisor when the correction is itself price-sensitive.",
					"Disclosure under market-sensitivity constraints",
					"The entity coordinates its statement with the competent authority rather than issuing and informing.",
					[]string{"ecb-ssm", "dora-art-19"}},
				{"Manage liquidity when outflow is instant and continuous.",
					"Liquidity management under a digital run",
					"Treasury has a position, a threshold, and a pre-agreed escalation to the central bank that does not have to be invented at 22:00.",
					[]string{"iso-22361", "nordic-baltic-exercise"}},
			},
			SuggestedFormat:   "game",
			SuggestedAudience: "board",
			Authorities:       []string{"dora-art-14", "iso-22361", "ecb-ssm", "nordic-baltic-exercise"},
			Jurisdictions:     []string{"LT", "LV", "EE", "BALTIC"},
			TrapDoor: "There is no incident, so the incident process never starts, so the crisis team is never convened, and by the time anyone " +
				"convenes it the outflow is three hours old. Watch for how long it takes the room to declare a crisis with nothing to point at.",
		},
	}
}

// ScenarioByKey resolves a seeded scenario.
func ScenarioByKey(key string) (Scenario, bool) {
	key = strings.TrimSpace(key)
	for _, s := range ScenarioLibrary() {
		if s.Key == key {
			return s, true
		}
	}
	return Scenario{}, false
}
