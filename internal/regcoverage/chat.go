package regcoverage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"grc/internal/aiprovider"
)

// Chat bounds. The report itself is the bulk of the context, so the transcript
// is trimmed harder than the report is.
const (
	// chatContextSections caps how many sections of the report go into the
	// chat's system prompt. A long regulation would otherwise put its entire
	// analysis in front of every question.
	chatContextSections = 60
	chatHistoryTurns    = 12
	chatAnswerTokens    = 2000
)

const chatSystemPreamble = `You are a compliance analyst answering questions about a regulation coverage report you produced.

The report is supplied below. Answer from it, and from the regulation it analyses. When the report does not support an answer, say so rather than filling the gap — the reader is using this to make compliance decisions.

When the reader disagrees with a mapping or a piece of commentary, engage with the substance: say whether you agree, and if you do, say exactly which section should change and how. Tell them they can apply the change with "Revise section <ref>" so it lands in the next version of the report.

Be concise and specific. Reference sections by their number. No markdown headings or code fences.`

// Ask answers a question about a report and records both turns.
//
// The conversation is grounded in the report rather than in the raw
// regulation: the report is what the reader is looking at, and it already
// carries the mappings, the commentary and the gaps. The regulation text for a
// section is pulled in only when the question names one.
func (s *Service) Ask(ctx context.Context, id int64, question, actor string) (ChatTurn, error) {
	if !s.Configured() {
		return ChatTurn{}, ErrNotConfigured
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return ChatTurn{}, invalid("a question is required")
	}

	report, err := s.Report(id, 0)
	if err != nil {
		return ChatTurn{}, err
	}
	if report.Version.Number == 0 {
		return ChatTurn{}, invalid("this regulation has not been analysed yet")
	}

	prior, err := s.repo.ListChat(id)
	if err != nil {
		return ChatTurn{}, err
	}

	history, sessionID := chatContext(prior)
	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    chatSystemPreamble + "\n\n" + ReportContext(report),
		History:   history,
		Prompt:    question,
		SessionID: sessionID,
		MaxTokens: chatAnswerTokens,
	})
	if err != nil {
		return ChatTurn{}, err
	}
	if resp.Refused {
		return ChatTurn{}, fmt.Errorf("the model declined to answer this question")
	}

	now := s.timestamp()
	if _, err := s.repo.AppendChat(ChatTurn{
		RegulationID: id,
		Version:      report.Version.Number,
		Role:         RoleUser,
		Content:      question,
		Actor:        actor,
		CreatedAt:    now,
	}); err != nil {
		return ChatTurn{}, err
	}
	return s.repo.AppendChat(ChatTurn{
		RegulationID: id,
		Version:      report.Version.Number,
		Role:         RoleAssistant,
		Content:      strings.TrimSpace(resp.Text),
		Model:        firstNonEmpty(resp.Model, resp.Provider),
		SessionID:    resp.SessionID,
		CreatedAt:    s.timestamp(),
	})
}

func (s *Service) ListChat(id int64) ([]ChatTurn, error) { return s.repo.ListChat(id) }

func (s *Service) ClearChat(id int64) error { return s.repo.ClearChat(id) }

// chatContext turns the stored transcript into harness turns, and recovers the
// provider-held session id from the most recent answer that had one. A
// Wintermute server keeps the transcript itself, so resuming its session is
// both cheaper and more faithful than replaying our copy.
func chatContext(turns []ChatTurn) ([]aiprovider.Message, string) {
	if len(turns) > chatHistoryTurns {
		turns = turns[len(turns)-chatHistoryTurns:]
	}
	history := make([]aiprovider.Message, 0, len(turns))
	sessionID := ""
	for _, t := range turns {
		role := aiprovider.RoleUser
		if t.Role == RoleAssistant {
			role = aiprovider.RoleAssistant
			if t.SessionID != "" {
				sessionID = t.SessionID
			}
		}
		if strings.TrimSpace(t.Content) == "" {
			continue
		}
		history = append(history, aiprovider.Message{Role: role, Text: t.Content})
	}
	// A resumed session already holds the transcript; sending it again would
	// replay every earlier turn into the same conversation.
	if sessionID != "" {
		return nil, sessionID
	}
	return history, ""
}

// ReportContext renders the report as the chat's grounding context. Exported so
// the context can be inspected and tested without a provider.
func ReportContext(report *Report) string {
	var b strings.Builder
	b.WriteString("# Report under discussion\n\n")
	writeField(&b, "Regulation", report.Regulation.Title)
	writeField(&b, "Framework", report.Regulation.FrameworkName)
	writeField(&b, "Reference", report.Regulation.SourceRef)
	b.WriteString(fmt.Sprintf("Version: %d (%s)\n", report.Version.Number, report.Version.CreatedAt))
	b.WriteString(fmt.Sprintf("Coverage: %d sections, %d security-relevant, %d mapped, %d unmapped.\n",
		report.Stats.Sections, report.Stats.Relevant, report.Stats.Mapped, report.Stats.Unmapped))

	if report.Version.Summary != "" {
		b.WriteString("\n## Executive summary\n\n")
		b.WriteString(report.Version.Summary)
		b.WriteString("\n")
	}

	b.WriteString("\n## Sections\n\n")
	shown := 0
	for _, entry := range report.Sections {
		if entry.Finding == nil {
			continue
		}
		// Sections the analysis found irrelevant are listed but not expanded:
		// the reader may still ask why one was dismissed, and the answer is in
		// the section reference and that verdict.
		if !entry.Finding.Relevant {
			b.WriteString(fmt.Sprintf("- %s (%s): not security-relevant\n",
				entry.Section.Ref, firstNonEmpty(entry.Section.Label, entry.Section.Title)))
			continue
		}
		if shown >= chatContextSections {
			continue
		}
		shown++
		b.WriteString(fmt.Sprintf("\n### %s — %s %s\n",
			entry.Section.Ref, entry.Section.Label, entry.Section.Title))
		if entry.Finding.Requirement != "" {
			b.WriteString("Requires: " + collapse(entry.Finding.Requirement) + "\n")
		}
		for _, m := range entry.Finding.Mappings {
			b.WriteString(fmt.Sprintf("Maps to [%s] %s — %s (%s confidence, %s)\n",
				m.Kind, m.Ref, m.Title, m.Confidence, m.Source))
		}
		if len(entry.Finding.Mappings) == 0 {
			b.WriteString("Maps to: nothing in the catalog\n")
		}
		if entry.Finding.Commentary != "" {
			b.WriteString("Commentary: " + collapse(entry.Finding.Commentary) + "\n")
		}
		if entry.Finding.Gaps != "" {
			b.WriteString("Gap: " + collapse(entry.Finding.Gaps) + "\n")
		}
	}
	if shown >= chatContextSections {
		b.WriteString(fmt.Sprintf("\n[%d further sections omitted from this context; ask about one by its reference to see it]\n",
			report.Stats.Relevant-shown))
	}
	return b.String()
}

// ---- revisions ----

const reviseSystemPrompt = `You are revising one section of a regulation coverage report in response to a reviewer's correction.

You are given the section of the regulation, the current analysis of it, the shortlist of candidate catalog items, and the reviewer's instruction. The reviewer is the authority on what is wrong; your job is to produce the corrected analysis, not to defend the previous one.

Rules:
- Apply the reviewer's correction. Where the instruction does not reach, keep the existing analysis as it stands rather than rewriting it.
- Map ONLY to references from the supplied shortlist or ones already in the current analysis. Never invent a control ID or an NFR key.
- "quote" must be copied verbatim from the section text supplied.
- Reply with the same JSON object shape as the original analysis, complete — it replaces the section entirely, so anything you omit is lost.

Reply with JSON only. No preamble, no markdown, no code fences.

{
  "relevant": true,
  "requirement": "...",
  "confidence": "high|medium|low",
  "quote": "verbatim phrase from the section",
  "commentary": "...",
  "gaps": "...",
  "mappings": [{"kind": "control|nfr", "ref": "...", "rationale": "...", "confidence": "high|medium|low"}]
}`

// Revise applies reviewer feedback to one section and cuts a new version.
//
// A revision never edits a version in place: the previous version keeps its
// snapshot and stays downloadable, because a report that has already been
// circulated has to keep saying what it said.
func (s *Service) Revise(ctx context.Context, id int64, sectionRef, instruction, actor string) (*Report, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return nil, invalid("say what should change")
	}

	reg, err := s.repo.GetRegulation(id)
	if err != nil {
		return nil, err
	}
	if reg.LatestVersion == 0 {
		return nil, invalid("this regulation has not been analysed yet")
	}

	section, err := s.repo.SectionByRef(id, strings.TrimSpace(sectionRef))
	if err != nil {
		return nil, err
	}

	current, err := s.repo.CurrentFindings(id)
	if err != nil {
		return nil, err
	}
	var existing *Finding
	for i := range current {
		if current[i].SectionID == section.ID {
			existing = &current[i]
			break
		}
	}

	cat, err := LoadCatalog(s.nfrs)
	if err != nil {
		return nil, err
	}
	prof, err := s.profileFor(reg)
	if err != nil {
		return nil, err
	}

	candidates := s.retriever.Shortlist(section, cat.Candidates, candidatesPerKind)
	prompt := RevisePrompt(reg, section, existing, candidates, instruction)
	hash := sha256.Sum256([]byte(reviseSystemPrompt + "\n" + prompt))

	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    reviseSystemPrompt,
		Prompt:    prompt,
		MaxTokens: maxAnswerTokens,
	})
	if err != nil {
		return nil, err
	}
	if resp.Refused {
		return nil, fmt.Errorf("the model declined to revise this section")
	}
	reply, err := ParseSectionReply(resp.Text)
	if err != nil {
		return nil, err
	}

	revised := Finding{
		RegulationID: id,
		SectionID:    section.ID,
		SectionRef:   section.Ref,
		Relevant:     reply.Relevant == nil || *reply.Relevant,
		Requirement:  strings.TrimSpace(reply.Requirement),
		Commentary:   strings.TrimSpace(reply.Commentary),
		Gaps:         strings.TrimSpace(reply.Gaps),
		Quote:        strings.TrimSpace(reply.Quote),
		Confidence:   normalizeConfidence(reply.Confidence),
		Model:        firstNonEmpty(resp.Model, resp.Provider),
		PromptHash:   hex.EncodeToString(hash[:]),
		CreatedAt:    s.timestamp(),
	}
	revised.Grounded = quoteAppearsIn(revised.Quote, section.Body)
	if revised.Relevant {
		revised.Mappings = resolveMappings(reply, cat, seedMappings(prof, section, cat))
	}

	if _, err := s.repo.SaveFinding(revised); err != nil {
		return nil, err
	}

	note := fmt.Sprintf("Revised %s: %s", section.Ref, collapse(truncate(instruction, 160)))
	if _, err := s.cutVersion(id, "", note, actor); err != nil {
		return nil, err
	}
	return s.Report(id, 0)
}

// RevisePrompt renders the revision turn. Exported for testing.
func RevisePrompt(reg Regulation, section Section, existing *Finding, candidates []Candidate, instruction string) string {
	var b strings.Builder

	b.WriteString("# Reviewer instruction\n\n")
	b.WriteString(instruction)
	b.WriteString("\n\n# Regulation\n\n")
	writeField(&b, "Title", reg.Title)
	writeField(&b, "Framework", reg.FrameworkName)

	b.WriteString("\n# Section being revised\n\n")
	writeField(&b, "Ref", section.Ref)
	writeField(&b, "Heading", strings.TrimSpace(section.Label+" "+section.Title))

	body := section.Body
	if len(body) > sectionBodyLimit {
		body = body[:sectionBodyLimit] + "\n[...section truncated for length...]"
	}
	b.WriteString("\nText:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\"\"\"\n")

	b.WriteString("\n# Current analysis\n\n")
	if existing == nil {
		b.WriteString("(this section has no analysis yet)\n")
	} else {
		b.WriteString(fmt.Sprintf("Security-relevant: %t\n", existing.Relevant))
		writeField(&b, "Requires", existing.Requirement)
		writeField(&b, "Commentary", existing.Commentary)
		writeField(&b, "Gaps", existing.Gaps)
		writeField(&b, "Quote", existing.Quote)
		writeField(&b, "Confidence", existing.Confidence)
		if len(existing.Mappings) == 0 {
			b.WriteString("Mappings: none\n")
		}
		for _, m := range existing.Mappings {
			b.WriteString(fmt.Sprintf("Mapping: [%s] %s — %s (%s) %s\n",
				m.Kind, m.Ref, m.Title, m.Confidence, m.Rationale))
		}
	}

	b.WriteString("\n# Candidate catalog items\n\n")
	for _, c := range candidates {
		kind := "control"
		if c.Kind == KindNFR {
			kind = "nfr"
		}
		b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", kind, c.Ref, c.Title))
	}

	b.WriteString("\nApply the instruction and reply with the complete JSON object for this section.\n")
	return b.String()
}
