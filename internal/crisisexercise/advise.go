package crisisexercise

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"grc/internal/aiprovider"
)

// This file is the expert seat at the table: a facilitator asking a board
// trainer what a chair will actually ask, a communications lead handing a
// holding statement to a journalist and asking what they would do with it, an
// evaluator turning a page of observations into findings that will survive the
// debrief.
//
// Three operations, and the difference between them matters:
//
//   - Advise is a conversation with a named persona about the exercise.
//   - Probe is the hot seat: a persona is shown one inject and what the team
//     actually did with it, and pushes back. This is the one a facilitator uses
//     with the room waiting.
//   - DraftAfterAction has the evaluator persona propose findings from the
//     record. It writes drafts, marked as the model's, for a human to accept,
//     edit or delete — because a finding is an accusation about an
//     organisation and nobody should be able to say it was the software's.

// maxHistoryTurns bounds what is resent on a follow-up question. A persona
// conversation is short by nature; a facilitator who has been arguing with the
// board trainer for forty turns is having a different conversation and does not
// need the first thirty.
const maxHistoryTurns = 12

// standingConstraints is appended to every persona's system prompt.
//
// The first two exist because this module's output ends up in front of boards
// and supervisors, where a confidently invented threshold is worse than an
// admission of ignorance. The third exists because the adversary persona and
// the red team persona are both asked, in the ordinary course of their work,
// for things that would be attack capability outside the exercise.
const standingConstraints = `

Standing constraints for this conversation:

- Say when you do not know. Never invent a control identifier, a numeric threshold, a regulatory deadline, a case, or a figure. Where an exact figure matters, name the instrument that sets it and say to confirm it.
- Distinguish what the regulation requires from what good practice suggests from what you would personally do. A reader must be able to tell which is which.
- Everything here is a training exercise. Produce narrative, communications, analysis and advice — never exploit code, malware, working commands, or anything that would function as real attack capability outside this room.
- Be concise. The person asking is usually mid-exercise with people waiting.`

// Advise puts a question to one of the expert personas, in the context of this
// exercise, and records the turn.
func (s *Service) Advise(ctx context.Context, exerciseID int64, personaKey, scope, question, actor string) (ChatTurn, error) {
	if !s.Configured() {
		return ChatTurn{}, invalid("no AI provider is configured — set one in Settings")
	}
	question = trim(question)
	if question == "" {
		return ChatTurn{}, invalid("a question is required")
	}
	persona, ok := PersonaByKey(personaKey)
	if !ok {
		return ChatTurn{}, invalidf("unknown adviser %q", personaKey)
	}

	d, err := s.liveDossier(exerciseID)
	if err != nil {
		return ChatTurn{}, err
	}
	history, sessionID, err := s.personaHistory(exerciseID, persona.Key)
	if err != nil {
		return ChatTurn{}, err
	}

	prompt := question
	if len(history) == 0 {
		// The brief goes in once, with the first question. Resending it every
		// turn would crowd out the conversation on a provider that keeps its
		// own transcript, and duplicate it on one that does not.
		prompt = ExerciseBrief(d, scope) + "\n\n---\n\n" + question
	} else if s := trim(scope); s != "" {
		prompt = "About " + s + ":\n\n" + question
	}

	now := s.timestamp()
	if _, err := s.repo.AppendChat(ChatTurn{
		ExerciseID: exerciseID,
		Persona:    persona.Key,
		Role:       RoleUser,
		Content:    question,
		Actor:      trim(actor),
		Scope:      trim(scope),
		CreatedAt:  now,
	}); err != nil {
		return ChatTurn{}, err
	}

	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    persona.System + standingConstraints,
		History:   history,
		Prompt:    prompt,
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

	return s.repo.AppendChat(ChatTurn{
		ExerciseID: exerciseID,
		Persona:    persona.Key,
		Role:       RoleAssistant,
		Content:    resp.Text,
		Model:      resp.Model,
		Scope:      trim(scope),
		SessionID:  resp.SessionID,
		CreatedAt:  s.timestamp(),
	})
}

// personaHistory returns the conversation so far with one persona, and the
// provider-held session to continue where there is one.
func (s *Service) personaHistory(exerciseID int64, persona string) ([]aiprovider.Message, string, error) {
	turns, err := s.repo.ListChat(exerciseID)
	if err != nil {
		return nil, "", err
	}
	var history []aiprovider.Message
	sessionID := ""
	for _, t := range turns {
		if t.Persona != persona {
			continue
		}
		role := aiprovider.RoleUser
		if t.Role == RoleAssistant {
			role = aiprovider.RoleAssistant
			if t.SessionID != "" {
				sessionID = t.SessionID
			}
		}
		history = append(history, aiprovider.Message{Role: role, Text: t.Content})
	}
	if len(history) > maxHistoryTurns {
		history = history[len(history)-maxHistoryTurns:]
	}
	return history, sessionID, nil
}

// ProbeInject is the hot seat: the persona is given one inject and what the
// team actually did with it, and responds in character.
//
// It is the single most useful thing in this module during delivery. A team
// that has just written a holding statement learns more from thirty seconds of
// a journalist's follow-up question than from an hour of discussion about
// communications principles.
func (s *Service) ProbeInject(ctx context.Context, injectID int64, personaKey, actor string) (ChatTurn, error) {
	if !s.Configured() {
		return ChatTurn{}, invalid("no AI provider is configured — set one in Settings")
	}
	persona, ok := PersonaByKey(personaKey)
	if !ok {
		return ChatTurn{}, invalidf("unknown adviser %q", personaKey)
	}
	inject, err := s.repo.GetInject(injectID)
	if err != nil {
		return ChatTurn{}, err
	}
	d, err := s.liveDossier(inject.ExerciseID)
	if err != nil {
		return ChatTurn{}, err
	}

	responses, err := s.repo.ListResponses(inject.ExerciseID)
	if err != nil {
		return ChatTurn{}, err
	}
	for _, r := range responses {
		if r.InjectID == injectID {
			resp := r
			inject.Response = &resp
			break
		}
	}

	prompt := probePrompt(d, inject)
	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    persona.System + standingConstraints,
		Prompt:    prompt,
		Agent:     s.agentName(),
		MaxTokens: 2500,
	})
	if err != nil {
		return ChatTurn{}, err
	}
	if resp.Refused {
		return ChatTurn{}, fmt.Errorf("the provider declined to answer for this inject")
	}

	scope := "inject " + inject.Code
	now := s.timestamp()
	if _, err := s.repo.AppendChat(ChatTurn{
		ExerciseID: inject.ExerciseID,
		Persona:    persona.Key,
		Role:       RoleUser,
		Content:    "Pressure-test the team's handling of " + inject.Code + " (" + inject.Title + ").",
		Actor:      trim(actor),
		Scope:      scope,
		CreatedAt:  now,
	}); err != nil {
		return ChatTurn{}, err
	}
	return s.repo.AppendChat(ChatTurn{
		ExerciseID: inject.ExerciseID,
		Persona:    persona.Key,
		Role:       RoleAssistant,
		Content:    resp.Text,
		Model:      resp.Model,
		Scope:      scope,
		SessionID:  resp.SessionID,
		CreatedAt:  s.timestamp(),
	})
}

func probePrompt(d Dossier, in Inject) string {
	var b strings.Builder
	b.WriteString(ExerciseBrief(d, "inject "+in.Code))
	b.WriteString("\n\n## The inject in front of you\n\n")
	b.WriteString(FormatOffset(in.OffsetMinutes) + " · " + in.Code + " · via " + channelLabel(in.Channel) + "\n\n")
	if in.Title != "" {
		b.WriteString("**" + in.Title + "**\n\n")
	}
	b.WriteString(in.Body + "\n\n")

	if in.Response != nil && trim(in.Response.ActualActions) != "" {
		b.WriteString("## What the team actually did\n\n")
		if in.Response.RespondedOffset >= 0 {
			b.WriteString("At " + FormatOffset(in.Response.RespondedOffset) + ": ")
		}
		b.WriteString(in.Response.ActualActions + "\n\n")
		if trim(in.Response.Observations) != "" {
			b.WriteString("Evaluator's note: " + in.Response.Observations + "\n\n")
		}
		b.WriteString("Respond in character to what they did. Push where it deserves pushing. " +
			"If they handled it well, say so and say why — and then ask the harder follow-up.\n")
	} else {
		b.WriteString("The team has not yet responded. Respond in character to the inject itself: " +
			"what you would say, ask or do next if you were on the other side of it.\n")
	}
	return b.String()
}

// ---- after-action drafting ----

// AfterActionResult is what a drafting run produced.
type AfterActionResult struct {
	Findings []Finding `json:"findings"`
	Summary  string    `json:"summary"`
	Model    string    `json:"model"`
}

// DraftAfterAction has the evaluator persona propose findings from the record.
//
// It reads only what was observed: the injects, what the team did with them,
// the decisions taken, and whether the notification clocks were met. It does
// not read the objectives' ratings, because a model told in advance which
// objectives failed will write findings that justify the rating rather than
// findings that follow from the evidence.
func (s *Service) DraftAfterAction(ctx context.Context, exerciseID int64, actor string) (AfterActionResult, error) {
	if !s.Configured() {
		return AfterActionResult{}, invalid("no AI provider is configured — set one in Settings")
	}
	d, err := s.liveDossier(exerciseID)
	if err != nil {
		return AfterActionResult{}, err
	}
	if d.Stats.InjectsPlayed == 0 {
		return AfterActionResult{}, invalid("nothing has been played yet — record what happened against the injects first")
	}

	persona, _ := PersonaByKey(PersonaEvaluator)
	prompt := afterActionPrompt(d)
	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    persona.System + standingConstraints,
		Prompt:    prompt,
		Agent:     s.agentName(),
		MaxTokens: 8000,
	})
	if err != nil {
		return AfterActionResult{}, err
	}
	if resp.Refused {
		return AfterActionResult{}, fmt.Errorf("the provider declined to draft the findings")
	}

	drafts, err := parseFindingReply(resp.Text)
	if err != nil {
		return AfterActionResult{}, err
	}

	phaseByKey := map[string]int64{}
	for _, p := range d.Phases {
		phaseByKey[p.Key] = p.ID
	}
	existing, err := s.repo.ListFindings(exerciseID)
	if err != nil {
		return AfterActionResult{}, err
	}

	out := make([]Finding, 0, len(drafts))
	for i, draft := range drafts {
		f := Finding{
			ExerciseID:     exerciseID,
			PhaseID:        phaseByKey[trim(draft.PhaseKey)],
			Ordinal:        len(existing) + i + 1,
			Code:           nextCode("F", len(existing)+i+1),
			Title:          trim(draft.Title),
			Category:       oneOf(draft.Category, CategoryProcess, CategoryPeople, CategoryProcess, CategoryTechnology, CategoryGovernance, CategoryCommunication, CategoryThirdParty, CategoryRegulatory),
			Severity:       oneOf(draft.Severity, SeverityMedium, SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityObserve),
			Description:    trim(draft.Description),
			RootCause:      trim(draft.RootCause),
			Evidence:       trim(draft.Evidence),
			Recommendation: trim(draft.Recommendation),
			Owner:          trim(draft.SuggestedOwner),
			Status:         FindingOpen,
			AIGenerated:    true,
			Model:          resp.Model,
			CreatedAt:      s.timestamp(),
			CreatedBy:      trim(actor),
		}
		if f.Title == "" {
			continue
		}
		saved, err := s.repo.SaveFinding(f)
		if err != nil {
			return AfterActionResult{}, err
		}
		for _, r := range draft.References {
			ref := Reference{
				ExerciseID: exerciseID,
				OwnerKind:  OwnerFinding,
				OwnerID:    saved.ID,
				RefKind:    normalizeRefKind(r.Kind),
				Ref:        trim(r.Ref),
				Note:       trim(r.Why),
				Source:     SourceModel,
				CreatedAt:  s.timestamp(),
			}
			if ref.Ref == "" {
				continue
			}
			if _, err := s.repo.AddReference(Resolve(s.resolver, ref)); err != nil {
				return AfterActionResult{}, err
			}
		}
		out = append(out, saved)
	}

	return AfterActionResult{
		Findings: out,
		Summary: fmt.Sprintf("%d draft findings written from the record. Every one is the model's proposal: read, edit, reassign or delete before the report is cut.",
			len(out)),
		Model: resp.Model,
	}, nil
}

func afterActionPrompt(d Dossier) string {
	var b strings.Builder
	b.WriteString(ExerciseBrief(d, ""))

	b.WriteString("\n\n## What happened, inject by inject\n\n")
	for _, p := range d.Phases {
		played := false
		for _, in := range p.Injects {
			if in.Response != nil && in.Response.Outcome != OutcomeNotPlayed {
				played = true
				break
			}
		}
		if !played {
			continue
		}
		b.WriteString("### " + p.Name + "\n\n")
		for _, in := range p.Injects {
			if in.Response == nil || in.Response.Outcome == OutcomeNotPlayed {
				continue
			}
			b.WriteString("- **" + in.Code + "** (" + FormatOffset(in.OffsetMinutes) + ", " + injectTypeLabel(in.Type) + ") " + in.Title + "\n")
			if trim(in.ExpectedActions) != "" {
				b.WriteString("  - Expected: " + in.ExpectedActions + "\n")
			}
			b.WriteString("  - Observed (" + outcomeLabel(in.Response.Outcome) + "): " + orDash(in.Response.ActualActions) + "\n")
			if trim(in.Response.Observations) != "" {
				b.WriteString("  - Evaluator note: " + in.Response.Observations + "\n")
			}
			if len(in.References) > 0 {
				b.WriteString("  - Cites: " + refsPlain(in.References) + "\n")
			}
		}
		b.WriteString("\n")
	}

	if len(d.Decisions) > 0 {
		b.WriteString("## Decisions taken\n\n")
		for _, dec := range d.Decisions {
			b.WriteString("- " + FormatOffset(dec.OffsetMinutes) + " " + dec.Title + ": " + dec.Decision)
			if trim(dec.MadeBy) != "" {
				b.WriteString(" (by " + dec.MadeBy + ", " + RoleLabel(dec.Role) + ")")
			}
			if trim(dec.Rationale) != "" {
				b.WriteString(" — " + dec.Rationale)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Classification and notification clocks\n\n")
	b.WriteString("Computed classification: " + map[bool]string{true: "major incident", false: "not a major incident"}[d.Classification.Major] + ". " + d.Classification.Rationale + "\n")
	if trim(d.Classification.TeamVerdict) != "" {
		b.WriteString("What the team concluded: " + d.Classification.TeamVerdict + "\n")
	}
	b.WriteString("\n")
	for _, c := range d.Clocks {
		if c.Status == ClockNotApplicable {
			continue
		}
		b.WriteString("- " + c.Label + " (" + c.Authority + "): due " + FormatOffset(c.DueOffset) +
			", sent " + FormatOffset(c.ActualOffset) + " — " + clockStatusLabel(c) + "\n")
	}

	b.WriteString(`
## What to produce

Write the findings this record supports, and only those. Do not manufacture a finding to fill a category, and do not soften one because it names a senior function.

Return a JSON array and nothing else, with objects of exactly these fields:

  "title"           the finding in one line, about the system rather than the person
  "phase_key"       which phase it arose in, from the phase list above, or ""
  "category"        one of: people, process, technology, governance, communication, third_party, regulatory
  "severity"        one of: critical, high, medium, low, observation — graded by consequence in a real event
  "description"     what was observed
  "evidence"        quoted from the record above
  "root_cause"      why it happened, not what happened
  "recommendation"  what to change, specifically enough to assign
  "suggested_owner" the role that could actually close it
  "references"      array of {"kind": "...", "ref": "...", "why": "..."} drawn only from the citations
                    that appear in the record above, or []

Include at least one finding about something that went well, titled as such, with severity "observation".
`)
	return b.String()
}

type findingReply struct {
	Title          string           `json:"title"`
	PhaseKey       string           `json:"phase_key"`
	Category       string           `json:"category"`
	Severity       string           `json:"severity"`
	Description    string           `json:"description"`
	Evidence       string           `json:"evidence"`
	RootCause      string           `json:"root_cause"`
	Recommendation string           `json:"recommendation"`
	SuggestedOwner string           `json:"suggested_owner"`
	References     []referenceReply `json:"references"`
}

func parseFindingReply(raw string) ([]findingReply, error) {
	body := extractJSONArray(raw)
	if body == "" {
		return nil, fmt.Errorf("the answer contained no JSON array")
	}
	var replies []findingReply
	if err := json.Unmarshal([]byte(body), &replies); err != nil {
		return nil, fmt.Errorf("could not read the answer as JSON: %w", err)
	}
	if len(replies) == 0 {
		return nil, fmt.Errorf("the answer contained no findings")
	}
	return replies, nil
}

// ---- shared context ----

// briefHeading opens ExerciseBrief. ExerciseContext reuses the brief under a
// heading of its own.
const briefHeading = "## The exercise you are advising on\n\n"

// ExerciseBrief renders the compact description of an exercise that every
// persona call starts from.
//
// It is deliberately shorter than the report. A persona asked what a chair will
// ask does not need the MSEL; it needs the institution, the scenario, the
// clock and where the exercise has got to. Sending the whole dossier would cost
// tokens and bury the question.
func ExerciseBrief(d Dossier, scope string) string {
	ex := d.Exercise
	j := JurisdictionByKey(ex.Jurisdiction)
	format, _ := FormatByKey(ex.Format)

	var b strings.Builder
	b.WriteString(briefHeading)
	b.WriteString(ex.Reference + " — " + ex.Title + "\n")
	writePromptField(&b, "Format", format.Label)
	writePromptField(&b, "Audience", audienceLabel(ex.Audience))
	writePromptField(&b, "Entity", ex.EntityName)
	writePromptField(&b, "Type", entityTypeLabel(ex.EntityType))
	writePromptField(&b, "Jurisdiction", j.Label)
	writePromptField(&b, "Competent authority", j.CompetentAuthority)
	writePromptField(&b, "National CSIRT", j.CSIRT)
	writePromptField(&b, "Market languages", strings.Join(j.Languages, ", "))
	writePromptField(&b, "Critical functions in scope", ex.CriticalFunctions)
	writePromptField(&b, "Status", ex.Status)

	if trim(ex.ThreatNarrative) != "" || trim(ex.ThreatActor) != "" {
		b.WriteString("\n### Scenario\n\n")
		writePromptField(&b, "Threat actor", ex.ThreatActor)
		writePromptField(&b, "Initial vector", ex.InitialVector)
		if trim(ex.ThreatNarrative) != "" {
			b.WriteString(ex.ThreatNarrative + "\n")
		}
	}

	if len(d.Objectives) > 0 {
		b.WriteString("\n### Objectives\n\n")
		for _, o := range d.Objectives {
			b.WriteString("- " + o.Code + ": " + o.Text + "\n")
		}
	}

	if d.Classification.ClassifiedOffset > 0 || d.Classification.CriticalServicesAffected {
		b.WriteString("\n### Classification\n\n")
		b.WriteString("Aware at " + FormatOffset(d.Classification.AwareOffset) +
			", classified at " + FormatOffset(d.Classification.ClassifiedOffset) + ". ")
		b.WriteString(d.Classification.Rationale + "\n")
		open := 0
		for _, c := range d.Clocks {
			if c.Status == ClockPending {
				open++
			}
		}
		if open > 0 {
			fmt.Fprintf(&b, "%d notification clocks are running and unmet.\n", open)
		}
	}

	if d.Stats.InjectsPlayed > 0 {
		fmt.Fprintf(&b, "\n### Progress\n\n%d of %d injects played; %d handled as expected, %d deviations, %d missed. %d findings recorded.\n",
			d.Stats.InjectsPlayed, d.Stats.Injects, d.Stats.AsExpected, d.Stats.Deviations,
			d.Stats.Missed, d.Stats.Findings)
	}

	if s := trim(scope); s != "" {
		b.WriteString("\nThe question is about: " + s + "\n")
	}
	return b.String()
}
