package crisisexercise

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"grc/internal/aiprovider"
)

// This file generates a Master Scenario Events List from a scenario brief.
//
// The design is the same one internal/regcoverage uses for a different problem,
// and for the same reasons. The model works one phase at a time rather than
// producing a whole exercise in one answer, because a single answer covering
// twelve phases degrades badly in the last four. It judges a shortlist of
// candidate references rather than browsing the catalog, which keeps the prompt
// small enough to reason over and makes every citation traceable to why the
// candidate was offered. And everything it produces carries the model, the hash
// of the prompt it answered and its own confidence, because an inject that will
// be delivered to a board deserves to be attributable.
//
// What the model is not asked to do is decide anything the regulation decides.
// It writes the inject that makes a team classify an incident; it does not
// classify it. That distinction is why the clock arithmetic in clocks.go is
// code and not a prompt.

// DesignInput controls a generation run.
type DesignInput struct {
	// PhaseKeys limits generation to certain phases. Empty means every phase
	// that currently has no injects, which makes a second run fill the gaps
	// rather than duplicate the work.
	PhaseKeys []string `json:"phase_keys"`
	// InjectsPerPhase bounds the model. Three to five is right for a tabletop;
	// more than six per phase produces an MSEL nobody can deliver.
	InjectsPerPhase int `json:"injects_per_phase"`
	// Difficulty sets the tier the phase is written at.
	Difficulty string `json:"difficulty"`
	// Guidance is the designer's steer: "the CISO is deliberately unavailable
	// from T+90", "keep the board phase to reserved decisions only".
	Guidance string `json:"guidance"`
	// Regenerate replaces the model-generated injects in the selected phases
	// instead of adding to them. Hand-written injects are never touched.
	Regenerate bool   `json:"regenerate"`
	Actor      string `json:"-"`
}

// DesignResult reports what a run produced.
type DesignResult struct {
	Exercise Exercise `json:"exercise"`
	Injects  []Inject `json:"injects"`
	Model    string   `json:"model"`
	// Failures names phases whose generation errored. A run continues past
	// them: one bad phase should not cost the other eleven.
	Failures []string `json:"failures,omitempty"`
}

// maxCandidatesPerKind bounds the reference shortlist offered per phase.
const maxCandidatesPerKind = 6

// Design generates injects for an exercise.
func (s *Service) Design(ctx context.Context, exerciseID int64, in DesignInput) (DesignResult, error) {
	if !s.Configured() {
		return DesignResult{}, invalid("no AI provider is configured — set one in Settings, or write the MSEL by hand")
	}
	ex, err := s.repo.GetExercise(exerciseID)
	if err != nil {
		return DesignResult{}, err
	}
	phases, err := s.repo.ListPhases(exerciseID)
	if err != nil {
		return DesignResult{}, err
	}
	if len(phases) == 0 {
		return DesignResult{}, invalid("this exercise has no phases to generate injects for")
	}
	existing, err := s.repo.ListInjects(exerciseID)
	if err != nil {
		return DesignResult{}, err
	}
	objectives, err := s.repo.ListObjectives(exerciseID)
	if err != nil {
		return DesignResult{}, err
	}

	perPhase := in.InjectsPerPhase
	if perPhase <= 0 {
		perPhase = 4
	}
	if perPhase > 8 {
		perPhase = 8
	}
	difficulty := oneOf(in.Difficulty, DifficultyChallenge,
		DifficultyFoundation, DifficultyChallenge, DifficultyAdvanced)

	selected := selectPhases(phases, in.PhaseKeys, existing)
	if len(selected) == 0 {
		return DesignResult{}, invalid("every selected phase already has injects — choose phases explicitly, or ask for a regeneration")
	}

	byPhase := map[int64][]Inject{}
	for _, i := range existing {
		byPhase[i.PhaseID] = append(byPhase[i.PhaseID], i)
	}

	result := DesignResult{Model: s.Model()}
	nextOrdinal := len(existing)
	for _, phase := range selected {
		if in.Regenerate {
			if err := s.dropGeneratedInjects(byPhase[phase.ID]); err != nil {
				return DesignResult{}, err
			}
		}
		injects, err := s.designPhase(ctx, ex, phase, objectives, byPhase[phase.ID], perPhase, difficulty, in.Guidance, &nextOrdinal)
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", phase.Name, err))
			continue
		}
		result.Injects = append(result.Injects, injects...)
	}

	if len(result.Injects) == 0 && len(result.Failures) > 0 {
		return result, fmt.Errorf("generation produced nothing: %s", strings.Join(result.Failures, "; "))
	}

	ex.AIGenerated = true
	ex.Model = s.Model()
	ex.UpdatedAt = s.timestamp()
	ex.UpdatedBy = trim(in.Actor)
	updated, err := s.repo.UpdateExercise(ex)
	if err != nil {
		return result, err
	}
	result.Exercise = updated
	return result, nil
}

// selectPhases resolves which phases to generate for. With no explicit
// selection it takes the phases that have nothing yet, which makes repeated
// runs additive rather than destructive.
func selectPhases(phases []Phase, keys []string, existing []Inject) []Phase {
	populated := map[int64]bool{}
	for _, in := range existing {
		populated[in.PhaseID] = true
	}
	if len(keys) > 0 {
		wanted := map[string]bool{}
		for _, k := range keys {
			wanted[trim(k)] = true
		}
		var out []Phase
		for _, p := range phases {
			if wanted[p.Key] {
				out = append(out, p)
			}
		}
		return out
	}
	var out []Phase
	for _, p := range phases {
		if !populated[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// dropGeneratedInjects removes the model's previous work from a phase and
// leaves anything a person wrote. A designer who has hand-edited an inject has
// made a decision, and a regeneration should not overrule it silently.
func (s *Service) dropGeneratedInjects(injects []Inject) error {
	for _, in := range injects {
		if !in.AIGenerated {
			continue
		}
		if err := s.repo.DeleteInject(in.ID); err != nil && !isNotFound(err) {
			return err
		}
	}
	return nil
}

func (s *Service) designPhase(ctx context.Context, ex Exercise, phase Phase, objectives []Objective,
	existing []Inject, perPhase int, difficulty, guidance string, nextOrdinal *int) ([]Inject, error) {

	candidates := Suggest(s.resolver, phase.Name+" "+phase.Purpose+" "+ex.ThreatNarrative, maxCandidatesPerKind)
	prompt := PhasePrompt(ex, phase, objectives, existing, candidates, perPhase, difficulty, guidance)
	hash := promptHash(prompt)

	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    designSystemPrompt,
		Prompt:    prompt,
		MaxTokens: 8000,
	})
	if err != nil {
		return nil, err
	}
	if resp.Refused {
		return nil, fmt.Errorf("the provider declined to answer for this phase")
	}

	replies, err := ParseInjectReply(resp.Text)
	if err != nil {
		return nil, err
	}
	if len(replies) > perPhase {
		replies = replies[:perPhase]
	}

	built := make([]Inject, 0, len(replies))
	for _, reply := range replies {
		*nextOrdinal++
		offset := phase.OffsetMinutes + reply.OffsetWithinPhase
		if reply.OffsetWithinPhase < 0 || reply.OffsetWithinPhase > phase.DurationMinutes {
			// A model that puts an inject outside its own phase has lost track
			// of the clock. Clamping is better than dropping the inject: the
			// content is usually fine and the facilitator can move it.
			offset = phase.OffsetMinutes
		}
		built = append(built, Inject{
			ExerciseID:       ex.ID,
			PhaseID:          phase.ID,
			Ordinal:          *nextOrdinal,
			Code:             nextCode("INJ", *nextOrdinal),
			OffsetMinutes:    offset,
			Title:            trim(reply.Title),
			Body:             trim(reply.Body),
			Channel:          normalizeChannel(reply.Channel),
			From:             trim(reply.From),
			To:               trim(reply.To),
			Type:             normalizeInjectType(reply.Type),
			ExpectedActions:  trim(reply.ExpectedActions),
			ExpectedDecision: trim(reply.ExpectedDecision),
			DecisionOwner:    trim(reply.DecisionOwner),
			EvaluationNotes:  trim(reply.EvaluationNotes),
			Difficulty:       difficulty,
			AIGenerated:      true,
			Model:            resp.Model,
			PromptHash:       hash,
			Confidence:       normalizeConfidence(reply.Confidence),
			CreatedAt:        s.timestamp(),
		})
	}

	saved, err := s.repo.AppendInjects(built)
	if err != nil {
		return nil, err
	}

	// The model's citations are resolved against the catalog before they are
	// stored, so an invented control identifier is recorded as unresolved
	// rather than sitting in the report looking like the real ones.
	for i, in := range saved {
		if i >= len(replies) {
			break
		}
		refs := make([]Reference, 0, len(replies[i].References))
		for _, r := range replies[i].References {
			ref := Reference{
				ExerciseID: ex.ID,
				OwnerKind:  OwnerInject,
				OwnerID:    in.ID,
				RefKind:    normalizeRefKind(r.Kind),
				Ref:        trim(r.Ref),
				Note:       trim(r.Why),
				Source:     SourceModel,
				CreatedAt:  s.timestamp(),
			}
			if ref.Ref == "" {
				continue
			}
			refs = append(refs, Resolve(s.resolver, ref))
		}
		if len(refs) == 0 {
			continue
		}
		if err := s.repo.ReplaceReferencesFor(ex.ID, OwnerInject, in.ID, refs); err != nil {
			return nil, err
		}
		saved[i].References = refs
	}
	return saved, nil
}

// designSystemPrompt frames every generation call. It is the facilitator
// persona's expertise plus the constraints that make the output usable: write
// the message, not a description of it; every inject earns its place by
// provoking a behaviour; and cite only what you were shown.
const designSystemPrompt = `You design master scenario event lists for crisis exercises at regulated European financial institutions. You have run these for banks, payment institutions and market infrastructures, including threat-led penetration tests under TIBER-EU and full crisis simulations with boards and supervisors.

Rules you work to:

1. Write the inject as it would be delivered. A SIEM alert reads like a SIEM alert; a journalist's email reads like a journalist's email; a supervisor's call reads like a supervisor's call. Never write "the team receives a report that…" — write the report.
2. Every inject provokes a behaviour you can evaluate. State the expected action concretely enough that an evaluator can mark it. An inject with no expected action is scenery.
3. Difficulty comes from removing things. Stress injects take away what the plan assumed: the person, the channel, the tool, the time. Use them.
4. The first hours of a real incident are made of bad information. Include injects that are ambiguous, contradictory or simply wrong, and say in the evaluation note what the team should do about that.
5. Match the audience. A technical audience gets telemetry and tickets. A board gets consequence, cost and reputation. Writing a board inject in SOC language is the commonest way an exercise loses the room.
6. Be specific and plausible. Real figures, real timings, real names of roles. No placeholders like [BANK NAME] and no impossible technology.
7. Cite only from the candidate references you are given, using their exact identifiers. If nothing in the list fits, cite nothing. Never invent an identifier.
8. Produce narrative and communications only. Do not produce exploit code, malware, working commands, or any material that would function as real attack capability outside the exercise.

Answer with a JSON array and nothing else — no prose before it, no code fence around it.`

// PhasePrompt builds the request for one phase. It is exported so a test can
// assert on it without reaching into the service, and so an operator debugging
// a bad generation can see exactly what was asked.
func PhasePrompt(ex Exercise, phase Phase, objectives []Objective, existing []Inject,
	candidates []RefTarget, perPhase int, difficulty, guidance string) string {

	var b strings.Builder
	j := JurisdictionByKey(ex.Jurisdiction)

	b.WriteString("## The institution\n\n")
	writePromptField(&b, "Entity", ex.EntityName)
	writePromptField(&b, "Type", entityTypeLabel(ex.EntityType))
	writePromptField(&b, "Jurisdiction", j.Label)
	writePromptField(&b, "Competent authority", j.CompetentAuthority)
	writePromptField(&b, "National CSIRT", j.CSIRT)
	writePromptField(&b, "Data protection authority", j.DPA)
	writePromptField(&b, "Market languages", strings.Join(j.Languages, ", "))
	writePromptField(&b, "Supervision", ex.Supervision)
	writePromptField(&b, "Critical or important functions", ex.CriticalFunctions)
	if trim(j.Notes) != "" {
		b.WriteString("Regional context: " + j.Notes + "\n\n")
	}

	b.WriteString("## The exercise\n\n")
	format, _ := FormatByKey(ex.Format)
	writePromptField(&b, "Title", ex.Title)
	writePromptField(&b, "Format", format.Label)
	writePromptField(&b, "Audience", audienceLabel(ex.Audience))
	writePromptField(&b, "Total duration", fmt.Sprintf("%d minutes", ex.DurationMinutes))
	writePromptField(&b, "Difficulty tier", difficulty)

	b.WriteString("\n## The scenario\n\n")
	writePromptField(&b, "Threat actor", ex.ThreatActor)
	writePromptField(&b, "Initial vector", ex.InitialVector)
	if trim(ex.ThreatNarrative) != "" {
		b.WriteString("Narrative:\n" + ex.ThreatNarrative + "\n\n")
	}

	if len(objectives) > 0 {
		b.WriteString("## Objectives this exercise must test\n\n")
		for _, o := range objectives {
			b.WriteString("- " + o.Code + ": " + o.Text)
			if trim(o.SuccessCriteria) != "" {
				b.WriteString(" (success: " + o.SuccessCriteria + ")")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## The phase you are writing for\n\n")
	writePromptField(&b, "Phase", phase.Name)
	writePromptField(&b, "Opens at", FormatOffset(phase.OffsetMinutes))
	writePromptField(&b, "Runs for", fmt.Sprintf("%d minutes", phase.DurationMinutes))
	writePromptField(&b, "Lead role", RoleLabel(phase.LeadRole))
	writePromptField(&b, "Purpose", phase.Purpose)
	writePromptField(&b, "Entry criteria", phase.EntryCriteria)
	writePromptField(&b, "Exit criteria", phase.ExitCriteria)

	if len(existing) > 0 {
		b.WriteString("\n## Injects this phase already has — do not repeat them\n\n")
		for _, in := range existing {
			b.WriteString("- " + FormatOffset(in.OffsetMinutes) + " " + in.Title + "\n")
		}
	}

	if len(candidates) > 0 {
		b.WriteString("\n## Candidate references — cite only from this list, by exact identifier\n\n")
		for _, c := range candidates {
			line := "- kind=" + c.Kind + " ref=" + c.Ref + " — " + c.Title
			if trim(c.Summary) != "" {
				line += ": " + truncate(c.Summary, 220)
			}
			b.WriteString(line + "\n")
		}
	}

	if trim(guidance) != "" {
		b.WriteString("\n## The designer's steer\n\n" + guidance + "\n")
	}

	fmt.Fprintf(&b, `
## What to produce

Write %d injects for this phase and nothing else. Return a JSON array of objects with exactly these fields:

  "title"              short label for the control team
  "body"               the message as delivered, verbatim, in the voice of its channel
  "channel"            one of: siem_alert, ticket, email, phone, chat, sms, news, social_media,
                       regulator, board, customer, third_party, in_person, law_enforcement
  "from"               who it comes from
  "to"                 which role receives it
  "type"               one of: event, stress, ambiguous, decision, contingency, information
  "offset_within_phase" minutes after this phase opens (0 to %d)
  "expected_actions"   what the players should do, specifically enough to mark
  "expected_decision"  the decision this forces, or "" if it forces none
  "decision_owner"     the role entitled to take it, or ""
  "evaluation_notes"   what an evaluator should watch for, including the common wrong move
  "confidence"         "high", "medium" or "low" — your confidence that this inject fits this institution
  "references"         array of {"kind": "...", "ref": "...", "why": "..."} drawn only from the
                       candidate list, or [] if none fit

At least one inject in this phase must be a stress or ambiguous inject unless the phase is too short to carry one.
`, perPhase, phase.DurationMinutes)

	return b.String()
}

func writePromptField(b *strings.Builder, label, value string) {
	if trim(value) == "" {
		return
	}
	b.WriteString(label + ": " + value + "\n")
}

// ---- reply parsing ----

// injectReply is one inject as the model returns it.
type injectReply struct {
	Title             string           `json:"title"`
	Body              string           `json:"body"`
	Channel           string           `json:"channel"`
	From              string           `json:"from"`
	To                string           `json:"to"`
	Type              string           `json:"type"`
	OffsetWithinPhase int              `json:"offset_within_phase"`
	ExpectedActions   string           `json:"expected_actions"`
	ExpectedDecision  string           `json:"expected_decision"`
	DecisionOwner     string           `json:"decision_owner"`
	EvaluationNotes   string           `json:"evaluation_notes"`
	Confidence        string           `json:"confidence"`
	References        []referenceReply `json:"references"`
}

type referenceReply struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	Why  string `json:"why"`
}

// ParseInjectReply reads the model's answer, tolerating the two things models
// reliably do despite being told not to: wrap the array in a code fence, and
// put a sentence in front of it.
func ParseInjectReply(raw string) ([]injectReply, error) {
	body := extractJSONArray(raw)
	if body == "" {
		return nil, fmt.Errorf("the answer contained no JSON array")
	}
	var replies []injectReply
	if err := json.Unmarshal([]byte(body), &replies); err != nil {
		return nil, fmt.Errorf("could not read the answer as JSON: %w", err)
	}

	out := make([]injectReply, 0, len(replies))
	for _, r := range replies {
		// An inject with neither a title nor a body is a parse artefact, not an
		// inject, and putting it in the MSEL makes a facilitator hunt for
		// content that was never there.
		if trim(r.Title) == "" && trim(r.Body) == "" {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the answer contained no usable injects")
	}
	return out, nil
}

// extractJSONArray finds the outermost bracketed array in a reply.
func extractJSONArray(raw string) string {
	raw = strings.TrimSpace(raw)
	if fenced := stripCodeFence(raw); fenced != "" {
		raw = fenced
	}
	start := strings.Index(raw, "[")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// Brackets inside a string value are content, not structure.
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

func stripCodeFence(raw string) string {
	if !strings.HasPrefix(raw, "```") {
		return ""
	}
	rest := raw[3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	if end := strings.LastIndex(rest, "```"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

func normalizeChannel(raw string) string {
	return oneOf(strings.ReplaceAll(trim(raw), " ", "_"), ChannelEmail,
		ChannelSIEM, ChannelTicket, ChannelEmail, ChannelPhone, ChannelChat, ChannelSMS,
		ChannelNews, ChannelSocial, ChannelRegulator, ChannelBoard, ChannelCustomer,
		ChannelVendor, ChannelInPerson, ChannelLawEnf)
}

func normalizeInjectType(raw string) string {
	return oneOf(raw, InjectEvent, InjectEvent, InjectStress, InjectAmbiguous,
		InjectDecision, InjectContingency, InjectInformation)
}

func normalizeRefKind(raw string) string {
	return oneOf(raw, RefControl, ReferenceKinds()...)
}

// promptHash records what a finding answered, so a report can be traced back to
// the question that produced it even after the prompt template changes.
func promptHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:8])
}

func truncate(s string, n int) string {
	s = trim(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
