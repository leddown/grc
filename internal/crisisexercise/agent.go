package crisisexercise

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"grc/internal/aiprovider"
)

// The Crisis Exercise agent is a Wintermute agent of this module's own, with its
// own document library and sources, chosen in Settings apart from the agent the
// rest of the application uses. Every question this module asks goes to it —
// MSEL generation, the advisers, the hot seat, after-action drafting — and it
// has a conversation of its own on each exercise, and in the AI dock on these
// pages.
//
// It learns about an exercise one of two ways. By default it is told which
// exercise is open and fetches the record itself from the knowledge API, which
// serves exercises in full, drafts included, and uncached, so what it reads is
// what is on screen. When the agent cannot reach this server, or the question
// is going to Claude, which has nothing to fetch with, the exercise is put into
// the conversation instead.

// PersonaAgent keys the conversation with the agent itself. It shares the
// adviser transcript table, where it is one more voice on the exercise.
const PersonaAgent = "agent"

// AgentConfig says where this module's questions go and how the agent learns
// about an exercise. Every field is read per question, so a change in Settings
// applies to the next one.
type AgentConfig struct {
	// Agent names the Wintermute agent. Empty leaves the agent Settings
	// configures for the rest of the application.
	Agent func() string
	// SendExercise puts the exercise into the conversation instead of leaving
	// the agent to fetch it, for an agent that cannot reach this server.
	SendExercise func() bool
	// Grounded reports whether questions are answered by something that can
	// fetch a record at all. Claude cannot.
	Grounded func() bool
}

// AgentInfo is what the exercise page says about who answers.
type AgentInfo struct {
	Agent         string `json:"agent"`
	Grounded      bool   `json:"grounded"`
	SendsExercise bool   `json:"sends_exercise"`
}

// WithAgent routes the module's questions to the Crisis Exercise agent.
func (s *Service) WithAgent(cfg AgentConfig) *Service {
	s.agent = cfg
	return s
}

func (s *Service) agentName() string {
	if s.agent.Agent == nil {
		return ""
	}
	return trim(s.agent.Agent())
}

// Agent reports the agent questions go to and how it sees an exercise.
func (s *Service) Agent() AgentInfo {
	grounded := s.agent.Grounded != nil && s.agent.Grounded()
	return AgentInfo{
		Agent:         s.agentName(),
		Grounded:      grounded,
		SendsExercise: !grounded || (s.agent.SendExercise != nil && s.agent.SendExercise()),
	}
}

// agentSystemPrompt frames the agent's own conversation. Unlike a persona it
// speaks for nobody in the room: it is the colleague who has read the record.
const agentSystemPrompt = `You are the crisis exercise agent for this GRC installation. You work with the people designing, delivering and reporting on crisis and continuity exercises: the scenario and objectives, the phases and injects of the master scenario events list, the decisions taken, the incident classification and its notification clocks, the findings, and the controls, requirements, regulation clauses, risks and policies an exercise cites. Answer about the exercise in front of you first, and draw on the rest of this installation's records where they bear on it.` + standingConstraints

// AskAgent puts a question to the Crisis Exercise agent about one exercise, and
// records the turn.
func (s *Service) AskAgent(ctx context.Context, exerciseID int64, question, actor string) (ChatTurn, error) {
	if !s.Configured() {
		return ChatTurn{}, invalid("no AI provider is configured — set one in Settings")
	}
	question = trim(question)
	if question == "" {
		return ChatTurn{}, invalid("a question is required")
	}

	d, err := s.liveDossier(exerciseID)
	if err != nil {
		return ChatTurn{}, err
	}
	history, sessionID, err := s.personaHistory(exerciseID, PersonaAgent)
	if err != nil {
		return ChatTurn{}, err
	}
	lead, fingerprint := s.exerciseContext(d, sessionID)

	if _, err := s.repo.AppendChat(ChatTurn{
		ExerciseID: exerciseID,
		Persona:    PersonaAgent,
		Role:       RoleUser,
		Content:    question,
		Actor:      trim(actor),
		CreatedAt:  s.timestamp(),
	}); err != nil {
		return ChatTurn{}, err
	}

	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    agentSystemPrompt,
		History:   history,
		Prompt:    withLead(lead, question),
		SessionID: sessionID,
		Agent:     s.agentName(),
		MaxTokens: 4000,
	})
	if err != nil {
		return ChatTurn{}, err
	}
	if resp.Refused {
		return ChatTurn{}, fmt.Errorf("the provider declined to answer this question")
	}
	s.contexts.remember(resp.SessionID, fingerprint)

	return s.repo.AppendChat(ChatTurn{
		ExerciseID: exerciseID,
		Persona:    PersonaAgent,
		Role:       RoleAssistant,
		Content:    resp.Text,
		Model:      resp.Model,
		SessionID:  resp.SessionID,
		CreatedAt:  s.timestamp(),
	})
}

// DockTurn is a question from the AI dock, prepared for the Crisis Exercise
// agent.
type DockTurn struct {
	Agent  string
	Prompt string
	// Answered records what the conversation has been told, against the
	// session the answer came back on.
	Answered func(sessionID string)
}

// DockQuestion prepares a question asked in the AI dock on path, in the dock's
// conversation sessionID. ok is false for a page outside this module, whose
// questions go on as before.
//
// On an exercise page the question is about that exercise and is given what
// the Agent conversation would be given; anywhere else in the module the agent
// is told where the exercises are.
func (s *Service) DockQuestion(path, sessionID, question string) (DockTurn, bool, error) {
	rest, ok := strings.CutPrefix(strings.TrimRight(trim(path), "/"), "/crisis-exercises")
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) {
		return DockTurn{}, false, nil
	}
	sessionID = trim(sessionID)
	turn := DockTurn{Agent: s.agentName(), Prompt: question, Answered: func(string) {}}

	segment, _, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	id, err := strconv.ParseInt(segment, 10, 64)
	if err != nil || id <= 0 {
		if sessionID == "" {
			lead, err := s.exerciseListContext()
			if err != nil {
				return DockTurn{}, false, err
			}
			turn.Prompt = withLead(lead, question)
		}
		return turn, true, nil
	}

	d, err := s.liveDossier(id)
	if isNotFound(err) {
		return turn, true, nil
	}
	if err != nil {
		return DockTurn{}, false, err
	}
	lead, fingerprint := s.exerciseContext(d, sessionID)
	turn.Prompt = withLead(lead, question)
	turn.Answered = func(answeredOn string) { s.contexts.remember(answeredOn, fingerprint) }
	return turn, true, nil
}

// exerciseContext is what goes in front of a question about exercise d in the
// conversation a provider holds as sessionID — empty for a new conversation, or
// for a provider that holds none and so needs telling every time. It returns
// the text and, when that text is the record itself, its fingerprint.
//
// A continuing conversation is told again only when it needs to be. An agent
// that fetches the record was told where it is on the first turn. One that is
// sent the record is sent it again only when the exercise has changed:
// resending an unchanged record every turn would bury the conversation under
// copies of it.
func (s *Service) exerciseContext(d Dossier, sessionID string) (lead, fingerprint string) {
	if !s.Agent().SendsExercise {
		if sessionID != "" {
			return "", ""
		}
		return exercisePointer(d), ""
	}
	record := ExerciseContext(d)
	fingerprint = promptHash(record)
	switch {
	case sessionID == "":
		return record, fingerprint
	case s.contexts.sent(sessionID) == fingerprint:
		return "", fingerprint
	default:
		return "This is the exercise's current record. It replaces any earlier copy in this conversation.\n\n" + record, fingerprint
	}
}

// exercisePointer tells an agent that fetches records which exercise is open and
// where its record is, with the brief so it can orient before it looks.
func exercisePointer(d Dossier) string {
	ref := d.Exercise.Reference
	return ExerciseBrief(d, "") + "\nThe full current record of " + ref + " — phases and injects with what happened to each, " +
		"decisions, classification, notification clocks, findings and participants — is in this installation's GRC knowledge " +
		`as kind "exercise", ref "` + ref + `". It is being edited while you talk about it: fetch it before answering anything ` +
		"that depends on its detail, and fetch it again when the answer may depend on something that has changed.\n"
}

// exerciseListContext orients a question asked where no one exercise is open.
func (s *Service) exerciseListContext() (string, error) {
	if !s.Agent().SendsExercise {
		return `The question was asked from the Crisis Exercises pages of this GRC installation. Every exercise, including those still being designed, is in this installation's GRC knowledge as kind "exercise", and what exercises have found as kind "exercise_finding".`, nil
	}
	exercises, err := s.ListExercises()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("## The crisis exercises in this installation\n\n")
	if len(exercises) == 0 {
		b.WriteString("None yet.\n")
	}
	for _, ex := range exercises {
		fmt.Fprintf(&b, "- %s — %s (%s)", ex.Reference, ex.Title, ex.Status)
		if trim(ex.Summary) != "" {
			b.WriteString(": " + ex.Summary)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func withLead(lead, question string) string {
	if lead == "" {
		return question
	}
	return lead + "\n\n---\n\n" + question
}

// contextLedger remembers, per provider-held conversation, the fingerprint of
// the exercise record last put into it. It is memory only: after a restart a
// continuing conversation is sent the record once more, which costs tokens and
// misleads nobody.
type contextLedger struct {
	mu   sync.Mutex
	seen map[string]string
}

// maxLedgerSessions bounds the ledger. Emptying it when full only means some
// conversations are sent the record once more.
const maxLedgerSessions = 1000

func (l *contextLedger) sent(sessionID string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[sessionID]
}

func (l *contextLedger) remember(sessionID, fingerprint string) {
	if sessionID == "" || fingerprint == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil || len(l.seen) >= maxLedgerSessions {
		l.seen = map[string]string{}
	}
	l.seen[sessionID] = fingerprint
}

// ExerciseContext renders an exercise's whole current record for a model to
// reason over: the brief, then everything the brief leaves out. It carries no
// timestamps of its own, so an unchanged exercise renders identically.
func ExerciseContext(d Dossier) string {
	ex := d.Exercise
	var b strings.Builder
	b.WriteString("## The exercise, as it stands now\n\n")
	b.WriteString(strings.TrimPrefix(ExerciseBrief(d, ""), briefHeading))

	var plan strings.Builder
	writePromptField(&plan, "Summary", ex.Summary)
	writePromptField(&plan, "Scheduled for", ex.ScheduledFor)
	if ex.DurationMinutes > 0 {
		writePromptField(&plan, "Duration", strconv.Itoa(ex.DurationMinutes)+" minutes")
	}
	writePromptField(&plan, "Facilitator", ex.Facilitator)
	writePromptField(&plan, "Control team", ex.ControlTeam)
	writePromptField(&plan, "Evaluators", ex.Evaluators)
	writeSection(&b, "Arrangements", plan.String())

	var objectives strings.Builder
	for _, o := range d.Objectives {
		if trim(o.SuccessCriteria) == "" && trim(o.Rating) == "" {
			continue
		}
		objectives.WriteString("- " + o.Code + "\n")
		writeIndented(&objectives, "Success looks like", o.SuccessCriteria)
		writeIndented(&objectives, "Rated", o.Rating)
	}
	writeSection(&b, "How the objectives are judged", objectives.String())

	var people strings.Builder
	for _, p := range d.Participants {
		people.WriteString("- " + firstOf(p.Name, "(unnamed)"))
		if role := firstOf(p.RoleLabel, humanise(p.RoleKey)); role != "" {
			people.WriteString(", " + role)
		}
		if trim(p.Org) != "" {
			people.WriteString(", " + p.Org)
		}
		if !p.Player {
			people.WriteString(" (not playing)")
		}
		people.WriteString("\n")
	}
	writeSection(&b, "Participants", people.String())

	var msel strings.Builder
	for _, p := range d.Phases {
		fmt.Fprintf(&msel, "#### %s · %s — %s", FormatOffset(p.OffsetMinutes), p.Name, p.Status)
		if p.DurationMinutes > 0 {
			fmt.Fprintf(&msel, ", %d min", p.DurationMinutes)
		}
		if trim(p.LeadRole) != "" {
			msel.WriteString(", led by " + humanise(p.LeadRole))
		}
		msel.WriteString("\n")
		writePromptField(&msel, "Purpose", p.Purpose)
		writePromptField(&msel, "Entry", p.EntryCriteria)
		writePromptField(&msel, "Exit", p.ExitCriteria)
		writePromptField(&msel, "Facilitator note", p.Notes)
		if len(p.Injects) == 0 {
			msel.WriteString("No injects yet.\n")
		}
		for _, in := range p.Injects {
			writeInjectContext(&msel, in)
		}
		msel.WriteString("\n")
	}
	writeSection(&b, "Phases and injects", msel.String())

	var decisions strings.Builder
	for _, dec := range d.Decisions {
		fmt.Fprintf(&decisions, "- %s: %s\n", FormatOffset(dec.OffsetMinutes), firstOf(dec.Title, "(untitled)"))
		writeIndented(&decisions, "Options", dec.Options)
		writeIndented(&decisions, "Decided", dec.Decision)
		writeIndented(&decisions, "Rationale", dec.Rationale)
		madeBy := dec.MadeBy
		if trim(dec.Role) != "" {
			madeBy = trim(madeBy + " (" + humanise(dec.Role) + ")")
		}
		writeIndented(&decisions, "Made by", madeBy)
		writeIndented(&decisions, "Authority", dec.Authority)
		if !dec.Reversible {
			writeIndented(&decisions, "Reversible", "no")
		}
		writeIndented(&decisions, "Regulatory implication", dec.RegulatoryImplication)
		writeIndented(&decisions, "Customer impact", dec.CustomerImpact)
	}
	writeSection(&b, "Decision log", decisions.String())

	writeSection(&b, "Incident classification", classificationContext(d.Classification))

	var clocks strings.Builder
	for _, k := range d.Clocks {
		clocks.WriteString("- " + firstOf(k.Label, k.Regime))
		if trim(k.Authority) != "" {
			clocks.WriteString(" to " + k.Authority)
		}
		fmt.Fprintf(&clocks, ": due %s, %s", FormatOffset(k.DueOffset), k.Status)
		if k.ActualOffset >= 0 {
			clocks.WriteString(" at " + FormatOffset(k.ActualOffset))
		}
		clocks.WriteString("\n")
		writeIndented(&clocks, "Basis", k.Basis)
		writeIndented(&clocks, "Evidence", k.Evidence)
	}
	writeSection(&b, "Notification clocks", clocks.String())

	var findings strings.Builder
	for _, f := range d.Findings {
		fmt.Fprintf(&findings, "- %s [%s] %s — %s\n", firstOf(f.Code, "(no code)"), f.Severity, f.Title, f.Status)
		writeIndented(&findings, "Description", f.Description)
		writeIndented(&findings, "Root cause", f.RootCause)
		writeIndented(&findings, "Recommendation", f.Recommendation)
		owner := f.Owner
		if trim(f.DueDate) != "" {
			owner = trim(owner + ", due " + f.DueDate)
		}
		writeIndented(&findings, "Owner", owner)
		writeIndented(&findings, "Risk", f.RiskRef)
		if f.AIGenerated {
			writeIndented(&findings, "Drafted by", "the model, for a person to accept or reject")
		}
	}
	writeSection(&b, "Findings", findings.String())

	var cites strings.Builder
	seen := map[string]bool{}
	for _, r := range d.References {
		key := r.RefKind + "/" + r.Ref
		if seen[key] {
			continue
		}
		seen[key] = true
		cites.WriteString("- " + r.RefKind + " " + r.Ref)
		if trim(r.Title) != "" {
			cites.WriteString(": " + r.Title)
		}
		if !r.Known {
			cites.WriteString(" (not found in this installation)")
		}
		cites.WriteString("\n")
	}
	writeSection(&b, "What it cites", cites.String())
	return b.String()
}

func writeInjectContext(b *strings.Builder, in Inject) {
	fmt.Fprintf(b, "- %s at %s, %s", firstOf(in.Code, "(no code)"), FormatOffset(in.OffsetMinutes), firstOf(in.Type, "event"))
	if trim(in.Channel) != "" {
		b.WriteString(" by " + humanise(in.Channel))
	}
	if trim(in.From) != "" || trim(in.To) != "" {
		b.WriteString(", " + firstOf(in.From, "—") + " → " + firstOf(in.To, "—"))
	}
	if trim(in.Title) != "" {
		b.WriteString(": " + in.Title)
	}
	b.WriteString("\n")
	writeIndented(b, "Message", in.Body)
	writeIndented(b, "Expected", in.ExpectedActions)
	decision := in.ExpectedDecision
	if trim(decision) != "" && trim(in.DecisionOwner) != "" {
		decision += " (" + humanise(in.DecisionOwner) + ")"
	}
	writeIndented(b, "Decision sought", decision)
	writeIndented(b, "Watch for", in.EvaluationNotes)
	if r := in.Response; r != nil && r.Outcome != OutcomeNotPlayed {
		played := humanise(r.Outcome)
		if r.RespondedOffset >= 0 {
			played += " at " + FormatOffset(r.RespondedOffset)
		}
		writeIndented(b, "Played", played)
		writeIndented(b, "Observed", r.ActualActions)
		writeIndented(b, "Evaluator", r.Observations)
	}
}

type classificationCriterion struct {
	label, value string
	material     bool
}

func classificationContext(c Classification) string {
	if c.ClassifiedOffset <= 0 && !c.CriticalServicesAffected && trim(c.Rationale) == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Aware at %s, classified at %s.", FormatOffset(c.AwareOffset), FormatOffset(c.ClassifiedOffset))
	if c.Major {
		b.WriteString(" A major incident.\n")
	} else {
		b.WriteString(" Not major.\n")
	}
	if c.CriticalServicesAffected {
		b.WriteString("Critical services affected.\n")
	}
	criteria := []classificationCriterion{
		{"Clients", c.ClientsAffected, c.ClientsMaterial},
		{"Transactions", c.TransactionsAffected, c.TransactionsMaterial},
		{"Reputational impact", c.ReputationalImpact, c.ReputationalMaterial},
		{"Geographical spread", c.GeographicalSpread, c.GeographicalMaterial},
		{"Data losses", c.DataLosses, c.DataLossesMaterial},
		{"Economic impact", c.EconomicImpact, c.EconomicMaterial},
	}
	if c.DowntimeMinutes > 0 || c.DurationMaterial {
		criteria = append(criteria, classificationCriterion{"Downtime", strconv.Itoa(c.DowntimeMinutes) + " minutes", c.DurationMaterial})
	}
	for _, k := range criteria {
		value := trim(k.value)
		if k.material {
			value = trim(value + " (material)")
		}
		writePromptField(&b, k.label, value)
	}
	if c.PersonalDataBreach {
		b.WriteString("A personal data breach.\n")
	}
	if c.NIS2Significant {
		b.WriteString("Significant under NIS2.\n")
	}
	writePromptField(&b, "Rationale", c.Rationale)
	writePromptField(&b, "The team's own verdict", c.TeamVerdict)
	return b.String()
}

func writeSection(b *strings.Builder, heading, body string) {
	if trim(body) == "" {
		return
	}
	b.WriteString("\n### " + heading + "\n\n" + body)
}

func writeIndented(b *strings.Builder, label, value string) {
	value = trim(value)
	if value == "" {
		return
	}
	b.WriteString("  " + label + ": " + strings.ReplaceAll(value, "\n", "\n    ") + "\n")
}

func firstOf(values ...string) string {
	for _, v := range values {
		if trim(v) != "" {
			return v
		}
	}
	return ""
}

func humanise(key string) string { return strings.ReplaceAll(trim(key), "_", " ") }
