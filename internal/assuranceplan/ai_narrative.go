package assuranceplan

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// NarrativeInput names the document the narrative is for: the approval pack,
// a quarter's report or the annual conclusion.
type NarrativeInput struct {
	Document string `json:"document"`
	Quarter  int    `json:"quarter"`
}

// NarrativeProposal is committee narrative drafted from the plan's data: an
// executive summary, and speaker notes for the pack's slides. Flags are the
// figures and articles in the text that the data it was drafted from does not
// contain; they are for the person deciding, and are not removed.
type NarrativeProposal struct {
	Document         string        `json:"document"`
	ExecutiveSummary string        `json:"executive_summary"`
	SpeakerNotes     []SpeakerNote `json:"speaker_notes"`
	Unknowns         []string      `json:"unknowns"`
	Flags            []string      `json:"flags"`
	Notes            []string      `json:"notes"`
}

type SpeakerNote struct {
	Slide int    `json:"slide"`
	Title string `json:"title"`
	Notes string `json:"notes"`
}

type narrativeAnswer struct {
	ExecutiveSummary string        `json:"executive_summary"`
	SpeakerNotes     []SpeakerNote `json:"speaker_notes"`
	Unknowns         []string      `json:"unknowns"`
}

// PackNarrative is an accepted narrative as the pack carries it.
type PackNarrative struct {
	ExecutiveSummary string        `json:"executive_summary"`
	SpeakerNotes     []SpeakerNote `json:"speaker_notes"`
	Model            string        `json:"model"`
	AcceptedBy       string        `json:"accepted_by"`
	AcceptedAt       string        `json:"accepted_at"`
}

// PackSlides are the approval pack's slides, in order, as
// assurance-plan-pack.typ sets them.
var PackSlides = []string{"Cover", "Decisions requested", "Regulatory basis", "Objective", "Scope", "Approach", "Sampling",
	"Roadmap", "Quarterly targets", "Operating cycle", "Governance", "Resourcing and delivery risks", "Next steps"}

const (
	maxSummary = 4000
	maxNote    = 1500
)

func narrativeDocument(in NarrativeInput) (string, error) {
	switch strings.ToLower(strings.TrimSpace(in.Document)) {
	case "pack":
		return "pack", nil
	case "annual":
		return "annual", nil
	case "quarter", "quarterly":
		if in.Quarter < 1 || in.Quarter > 4 {
			return "", invalid("a quarterly report's narrative needs its quarter, 1 to 4")
		}
		return "q" + strconv.Itoa(in.Quarter), nil
	case "q1", "q2", "q3", "q4":
		return strings.ToLower(strings.TrimSpace(in.Document)), nil
	}
	return "", invalid("the document is pack, quarter or annual")
}

// narrativeFacts is the data a narrative is drafted from, as JSON, without
// the population, the test log or an accepted narrative.
func (s *Service) narrativeFacts(id int64, doc string) (string, error) {
	var v any
	switch doc {
	case "pack":
		pack, err := s.PackData(id)
		if err != nil {
			return "", err
		}
		pack.Pack.Narrative = nil
		pack.Steps, pack.Attributes = nil, nil
		v = struct {
			PackPayload
			Slides []string `json:"slides"`
		}{pack, PackSlides}
	case "annual":
		r, err := s.plainAnnualReport(id)
		if err != nil {
			return "", err
		}
		r.Appendices = nil
		v = r
	default:
		q, _ := strconv.Atoi(strings.TrimPrefix(doc, "q"))
		r, err := s.plainQuarterReport(id, q)
		if err != nil {
			return "", err
		}
		r.Appendices = nil
		v = r
	}
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// DraftNarrative drafts the narrative for one document from the plan's data.
func (s *Service) DraftNarrative(ctx context.Context, id int64, in NarrativeInput, actor string) (AIProposal, error) {
	doc, err := narrativeDocument(in)
	if err != nil {
		return AIProposal{}, err
	}
	p, err := s.Get(id)
	if err != nil {
		return AIProposal{}, err
	}
	done, err := s.begin(id)
	if err != nil {
		return AIProposal{}, err
	}
	defer done()
	facts, err := s.narrativeFacts(id, doc)
	if err != nil {
		return AIProposal{}, err
	}
	prompt := narrativePrompt(p, doc, facts)
	var ans narrativeAnswer
	resp, err := s.askJSON(ctx, prompt, func(text string) error {
		if err := decodeContract(text, &ans); err != nil {
			return err
		}
		if strings.TrimSpace(ans.ExecutiveSummary) == "" {
			return fmt.Errorf("executive_summary is empty")
		}
		return nil
	})
	if err != nil {
		return AIProposal{}, err
	}
	prop := checkNarrative(doc, ans, facts, p)
	return s.record(p, AIKindNarrative, doc, NarrativeInput{Document: doc}, prop, resp, promptHash(aiRules, prompt), actor)
}

func documentLabel(doc string) string {
	switch doc {
	case "pack":
		return "the committee approval pack"
	case "annual":
		return "the annual conclusion"
	}
	return "the " + strings.ToUpper(doc) + " progress report"
}

func narrativePrompt(p Plan, doc, facts string) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("Task: draft the committee narrative for %s of assurance plan %s, for the %s.", documentLabel(doc), p.Reference, firstNonEmpty(p.ApprovalForum, "approval forum"))
	line("Write for committee members who read it before the meeting: plain sentences, the position first, then what needs their attention, then what is asked of them.")
	line("Use only figures, dates and references that are in the data below. Where the data does not say something, put it in \"unknowns\" rather than writing around it.")
	if doc == "pack" {
		line("Write an executive summary of at most 250 words, and speaker notes of at most 80 words for each slide, numbered 1 to %d as in \"slides\".", len(PackSlides))
	} else {
		line("Write an executive summary of at most 300 words. Leave speaker_notes empty.")
	}
	line("\nThe data (JSON):\n%s", facts)
	line(`
Reply with one JSON object:
{"executive_summary": "...", "speaker_notes": [{"slide": 1, "title": "Cover", "notes": "..."}], "unknowns": ["..."]}`)
	return b.String()
}

func checkNarrative(doc string, ans narrativeAnswer, facts string, p Plan) NarrativeProposal {
	out := NarrativeProposal{Document: doc, ExecutiveSummary: clip(strings.TrimSpace(ans.ExecutiveSummary), maxSummary),
		SpeakerNotes: []SpeakerNote{}, Unknowns: cleanList(ans.Unknowns, 20, 500), Flags: []string{}, Notes: []string{}}
	if doc == "pack" {
		seen := map[int]bool{}
		for _, n := range ans.SpeakerNotes {
			text := clip(strings.TrimSpace(n.Notes), maxNote)
			if n.Slide < 1 || n.Slide > len(PackSlides) || seen[n.Slide] || text == "" {
				continue
			}
			seen[n.Slide] = true
			out.SpeakerNotes = append(out.SpeakerNotes, SpeakerNote{Slide: n.Slide, Title: PackSlides[n.Slide-1], Notes: text})
		}
		slices.SortFunc(out.SpeakerNotes, func(a, b SpeakerNote) int { return a.Slide - b.Slide })
		if missing := len(PackSlides) - len(out.SpeakerNotes); missing > 0 {
			out.Notes = append(out.Notes, fmt.Sprintf("%d slide(s) have no speaker notes", missing))
		}
	} else if len(ans.SpeakerNotes) > 0 {
		out.Notes = append(out.Notes, "speaker notes are for the pack; they were left out")
	}
	text := out.ExecutiveSummary
	for _, n := range out.SpeakerNotes {
		text += "\n" + n.Notes
	}
	out.Flags = narrativeFlags(text, facts, p)
	return out
}

var (
	numberPattern  = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	articlePattern = regexp.MustCompile(`(?i)\b(?:art\.?|article)\s*(\d+)`)
)

// numbersIn is every number a text contains, with the forms a writer may
// round it to: a share as a percentage, and to the unit and the tenth.
func numbersIn(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range numberPattern.FindAllString(text, -1) {
		f, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", ""), 64)
		if err != nil {
			continue
		}
		forms := []float64{f}
		if f > 0 && f < 1 {
			forms = append(forms, f*100)
		}
		for _, x := range forms {
			out[strconv.FormatFloat(x, 'f', -1, 64)] = true
			out[strconv.FormatFloat(math.Round(x), 'f', -1, 64)] = true
			out[strconv.FormatFloat(math.Round(x*10)/10, 'f', -1, 64)] = true
		}
	}
	return out
}

// narrativeFlags lists the figures and articles a narrative states that the
// data it was drafted from does not contain. A number in the text has to be
// one of the data's numbers, rounded or as a percentage; an article number
// has to be in the plan's references.
func narrativeFlags(text, facts string, p Plan) []string {
	have := numbersIn(facts)
	flags := []string{}
	seen := map[string]bool{}
	for _, m := range numberPattern.FindAllString(text, -1) {
		f, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", ""), 64)
		if err != nil {
			continue
		}
		key := strconv.FormatFloat(f, 'f', -1, 64)
		if have[key] || seen[key] {
			continue
		}
		seen[key] = true
		flags = append(flags, fmt.Sprintf("the figure %s is not in the plan's data", m))
	}
	refs := numbersIn(strings.Join(planRefs(p), " ") + " " + facts)
	for _, m := range articlePattern.FindAllStringSubmatch(text, -1) {
		if !refs[m[1]] && !seen["art"+m[1]] {
			seen["art"+m[1]] = true
			flags = append(flags, fmt.Sprintf("%s is not among the plan's references", strings.TrimSpace(m[0])))
		}
	}
	return flags
}

func (s *Service) acceptNarrative(id int64, pr AIProposal, payload []byte) (NarrativeProposal, []string, error) {
	p, err := s.Get(id)
	if err != nil {
		return NarrativeProposal{}, nil, err
	}
	var edited NarrativeProposal
	if err := decodePayload(payload, &edited); err != nil {
		return NarrativeProposal{}, nil, err
	}
	if strings.TrimSpace(edited.ExecutiveSummary) == "" {
		return NarrativeProposal{}, nil, invalid("the executive summary is empty")
	}
	facts, err := s.narrativeFacts(id, pr.Document)
	if err != nil {
		return NarrativeProposal{}, nil, err
	}
	n := checkNarrative(pr.Document, narrativeAnswer{ExecutiveSummary: edited.ExecutiveSummary, SpeakerNotes: edited.SpeakerNotes, Unknowns: edited.Unknowns}, facts, p)
	notes := []string{"narrative for " + documentLabel(pr.Document)}
	if len(n.Flags) > 0 {
		notes = append(notes, fmt.Sprintf("accepted with %d flag(s)", len(n.Flags)))
	}
	return n, notes, nil
}

// acceptedNarrative is the newest accepted narrative for a document.
func (s *Service) acceptedNarrative(id int64, doc string) (NarrativeProposal, AIProposal, bool) {
	list, err := s.repo.AIProposals(id)
	if err != nil {
		return NarrativeProposal{}, AIProposal{}, false
	}
	for _, pr := range list {
		if pr.Kind != AIKindNarrative || pr.Document != doc || pr.Status != AIAccepted || len(pr.Applied) == 0 {
			continue
		}
		var n NarrativeProposal
		if json.Unmarshal(pr.Applied, &n) == nil && n.ExecutiveSummary != "" {
			return n, pr, true
		}
	}
	return NarrativeProposal{}, AIProposal{}, false
}

// withNarrative puts an accepted executive summary in a report, and says in
// its limitations how it was written.
func (s *Service) withNarrative(id int64, doc string, r *ReportPayload) {
	n, pr, ok := s.acceptedNarrative(id, doc)
	if !ok {
		return
	}
	r.ExecutiveSummary = n.ExecutiveSummary
	r.Limitations = strings.TrimSpace(r.Limitations + fmt.Sprintf(" The executive summary was drafted with AI assistance (%s) from the plan's data, and reviewed and accepted by %s on %s.",
		pr.Model, firstNonEmpty(pr.DecidedBy, "a reviewer"), displayDate(clip(pr.DecidedAt, 10))))
}
