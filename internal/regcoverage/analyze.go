package regcoverage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"grc/internal/aiprovider"
)

// Analysis bounds. A section prompt carries a shortlist and one article, so it
// is small; the answer carries commentary for a whole article, so it is not.
const (
	// candidatesPerKind is how many controls and how many NFRs are shortlisted
	// per section. Beyond roughly this many the retrieval signal is gone and
	// the prompt is just the catalog again — the model's job here is to judge a
	// shortlist, not to browse.
	candidatesPerKind = 10
	// sectionBodyLimit bounds one article in a prompt. Long articles are
	// truncated rather than dropped, and the truncation is visible to the model.
	sectionBodyLimit = 12000
	// minAnalyzableBody is the shortest section worth a model call. Below this
	// there is nothing to analyse and nothing to quote.
	minAnalyzableBody = 40
	maxAnswerTokens   = 2000
	summaryTokens     = 1500
)

// Asker is the AI harness this module talks through — the same router the rest
// of the app uses, so keys, provider choice and usage accounting stay in one
// place.
type Asker interface {
	Ask(ctx context.Context, req aiprovider.Request) (aiprovider.Response, error)
	Available() bool
	Describe() string
}

// ErrNotConfigured reports that no AI provider is available.
var ErrNotConfigured = invalid("no AI provider is configured — set one in Settings")

const analysisSystemPrompt = `You are a compliance analyst mapping EU regulations to a security control catalog.

You are given one section of a regulation, and a shortlist of candidate Security NFRs and NIST SP 800-53 controls retrieved for it.

Rules:
- Map ONLY to references from the supplied shortlist. Never invent a control ID or an NFR key. If nothing on the shortlist fits, return no mappings and say so in "gaps".
- Judge the shortlist critically. A retrieved candidate is a suggestion from a keyword search, not evidence; most sections should map to a handful of items at most.
- Many sections of a regulation impose no security or resilience obligation at all — definitions, scope, addressees, entry into force, procedural and supervisory machinery. For those set "relevant" to false and do not map anything. Saying a section is not security-relevant is a correct and useful answer.
- "quote" must be copied verbatim from the section text supplied, 10 to 300 characters, and must be the phrase that carries the obligation. Do not paraphrase it.
- "commentary" is practitioner guidance on implementing this well: what good looks like, what evidence an auditor asks for, where implementations commonly fall short. Two to five sentences. Do not restate the requirement.
- "gaps" names what the catalog does not cover for this section, or is empty when it is fully covered.
- Be specific and concise. No preamble, no markdown, no code fences.

Reply with JSON only, in exactly this shape:
{
  "relevant": true,
  "requirement": "what the section obliges, in one or two sentences",
  "confidence": "high|medium|low",
  "quote": "verbatim phrase from the section",
  "commentary": "practitioner guidance",
  "gaps": "what the catalog does not cover, or an empty string",
  "mappings": [
    {"kind": "control", "ref": "AC-2", "rationale": "why this control satisfies the obligation", "confidence": "high|medium|low"},
    {"kind": "nfr", "ref": "NFR-KEY", "rationale": "why this NFR satisfies the obligation", "confidence": "high|medium|low"}
  ]
}`

// sectionReply is the model's answer for one section.
type sectionReply struct {
	Relevant    *bool  `json:"relevant"`
	Requirement string `json:"requirement"`
	Confidence  string `json:"confidence"`
	Quote       string `json:"quote"`
	Commentary  string `json:"commentary"`
	Gaps        string `json:"gaps"`
	Mappings    []struct {
		Kind       string `json:"kind"`
		Ref        string `json:"ref"`
		Rationale  string `json:"rationale"`
		Confidence string `json:"confidence"`
	} `json:"mappings"`
}

// SectionPrompt renders the user turn for one section. It is exported so the
// prompt can be inspected and tested without a provider: the prompt is this
// module's interface to the model, and an untested prompt is an untested
// interface.
func SectionPrompt(reg Regulation, section Section, candidates []Candidate, seed []Mapping) string {
	var b strings.Builder

	b.WriteString("# Regulation\n\n")
	writeField(&b, "Title", reg.Title)
	writeField(&b, "Framework", reg.FrameworkName)
	writeField(&b, "Reference", reg.SourceRef)

	b.WriteString("\n# Section under analysis\n\n")
	writeField(&b, "Ref", section.Ref)
	writeField(&b, "Heading", strings.TrimSpace(section.Label+" "+section.Title))
	writeField(&b, "Category", section.Category)

	body := section.Body
	if len(body) > sectionBodyLimit {
		body = body[:sectionBodyLimit] + "\n[...section truncated for length...]"
	}
	b.WriteString("\nText:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\"\"\"\n")

	if len(seed) > 0 {
		b.WriteString("\n# Curated prior\n\n")
		b.WriteString("A reviewed crosswalk for this framework already associates this section with:\n")
		for _, m := range seed {
			b.WriteString("- " + m.Ref)
			if m.Title != "" {
				b.WriteString(" — " + m.Title)
			}
			if m.Rationale != "" {
				b.WriteString(" (" + m.Rationale + ")")
			}
			b.WriteString("\n")
		}
		b.WriteString("Treat this as a strong prior, not as truth: confirm each against the section text and drop any that do not hold.\n")
	}

	b.WriteString("\n# Candidate catalog items\n\n")
	if len(candidates) == 0 {
		b.WriteString("(nothing retrieved — if this section is security-relevant, say so in \"gaps\")\n")
	}
	for _, c := range candidates {
		kind := "control"
		if c.Kind == KindNFR {
			kind = "nfr"
		}
		b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", kind, c.Ref, c.Title))
		if c.Text != "" {
			b.WriteString("  " + collapse(c.Text) + "\n")
		}
	}

	b.WriteString("\nAnalyse the section and reply with the JSON object.\n")
	return b.String()
}

// analyzeSection runs one section: prompt, ask, parse, validate.
func (s *Service) analyzeSection(ctx context.Context, reg Regulation, section Section, cat *Catalog, seed []Mapping) (Finding, error) {
	candidates := s.retriever.Shortlist(section, cat.Candidates, candidatesPerKind)
	prompt := SectionPrompt(reg, section, candidates, seed)
	hash := sha256.Sum256([]byte(analysisSystemPrompt + "\n" + prompt))

	resp, err := s.asker.Ask(ctx, aiprovider.Request{
		System:    analysisSystemPrompt,
		Prompt:    prompt,
		MaxTokens: maxAnswerTokens,
	})
	if err != nil {
		return Finding{}, fmt.Errorf("section %s: %w", section.Ref, err)
	}
	if resp.Refused {
		return Finding{}, fmt.Errorf("section %s: the model declined to analyse it", section.Ref)
	}

	reply, err := ParseSectionReply(resp.Text)
	if err != nil {
		return Finding{}, fmt.Errorf("section %s: %w", section.Ref, err)
	}

	finding := Finding{
		RegulationID: reg.ID,
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
	}
	finding.Grounded = quoteAppearsIn(finding.Quote, section.Body)

	// A section the model called not relevant carries no mappings, whatever it
	// returned: the two answers contradict each other, and the conservative
	// reading is the one that claims less.
	if finding.Relevant {
		finding.Mappings = resolveMappings(reply, cat, seed)
	}
	return finding, nil
}

// resolveMappings turns the model's references into catalog-checked mappings,
// keeping the curated seed entries the model confirmed and marking anything it
// named that does not exist.
func resolveMappings(reply *sectionReply, cat *Catalog, seed []Mapping) []Mapping {
	seen := map[string]bool{}
	out := []Mapping{}

	seedRefs := map[string]Mapping{}
	for _, m := range seed {
		seedRefs[refKey(m.Kind, m.Ref)] = m
	}

	for _, raw := range reply.Mappings {
		ref := strings.ToUpper(strings.TrimSpace(raw.Ref))
		if ref == "" {
			continue
		}
		kind := KindControl
		if strings.EqualFold(strings.TrimSpace(raw.Kind), "nfr") {
			kind = KindNFR
		}
		key := refKey(kind, ref)
		if seen[key] {
			continue
		}
		seen[key] = true

		mapping := Mapping{
			Kind:       kind,
			Ref:        ref,
			Rationale:  strings.TrimSpace(raw.Rationale),
			Confidence: normalizeConfidence(raw.Confidence),
			Source:     SourceModel,
		}
		if cand, ok := cat.Lookup(kind, ref); ok {
			mapping.Kind = cand.Kind
			mapping.Ref = cand.Ref
			mapping.Title = cand.Title
			mapping.Known = true
		}
		// A reference the curated crosswalk already proposed and the model then
		// confirmed is stronger evidence than either alone, and the report says
		// which it was.
		if _, ok := seedRefs[refKey(mapping.Kind, mapping.Ref)]; ok {
			mapping.Source = SourceSeed
		}
		out = append(out, mapping)
	}
	return out
}

// ParseSectionReply reads the model's JSON, tolerating the wrappers models add
// around it. Exported for the same reason as SectionPrompt.
func ParseSectionReply(raw string) (*sectionReply, error) {
	body := extractJSONObject(raw)
	if body == "" {
		return nil, fmt.Errorf("the model did not return a JSON object")
	}
	var reply sectionReply
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		return nil, fmt.Errorf("the model returned malformed JSON: %w", err)
	}
	if strings.TrimSpace(reply.Requirement) == "" && len(reply.Mappings) == 0 &&
		strings.TrimSpace(reply.Commentary) == "" {
		return nil, fmt.Errorf("the model returned an empty analysis")
	}
	return &reply, nil
}

// extractJSONObject pulls the outermost JSON object out of a reply that may be
// wrapped in a code fence or preceded by a sentence of preamble.
func extractJSONObject(raw string) string {
	s := strings.TrimSpace(raw)
	if fence := strings.Index(s, "```"); fence >= 0 {
		rest := s[fence+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		s = strings.TrimSpace(rest)
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// quoteAppearsIn is the grounding check: the model's quote has to be findable
// in the section it claims to have read. Whitespace and case are normalised
// because extraction introduces line breaks mid-sentence, and a model that
// re-wraps a quote has still quoted it. Anything more forgiving than that would
// stop catching the thing this exists to catch — a confident analysis of text
// that is not there.
func quoteAppearsIn(quote, body string) bool {
	q := normalizeForMatch(quote)
	if len(q) < 10 {
		return false
	}
	return strings.Contains(normalizeForMatch(body), q)
}

func normalizeForMatch(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsSpace(r):
			space = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			// Punctuation is dropped rather than treated as a boundary: models
			// normalise quotation marks and dashes, and a report should not
			// call a quote ungrounded over a typographic apostrophe.
			space = false
		}
	}
	return b.String()
}

func writeField(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(label + ": " + value + "\n")
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
