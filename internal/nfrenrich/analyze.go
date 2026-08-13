package nfrenrich

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"grc/internal/securitynfr"
)

// DefaultModel matches internal/assistant: one model choice for the app, one
// edit to change it.
const DefaultModel = "claude-opus-5"

// maxTokens bounds one analysis reply. Adaptive thinking is on and max_tokens
// caps thinking plus response text together, so this is sized well above the
// JSON a proposal needs.
const maxTokens = 4096

// analysisSystemPrompt is the whole safety argument of this module, so it is
// worth reading as one.
//
// The instruction that does the most work is "quote": requiring a verbatim span
// from a supplied chunk turns an unverifiable claim into a checkable one, and
// the validation below rejects anything whose quote is not actually in the
// chunk it cites. A model inclined to write what encryption in transit usually
// looks like cannot produce a passing quote for it.
//
// The "return no proposal" instruction earns its place for the same reason the
// tool-calling one does in internal/assistant: without it, a model asked to
// find enrichment in an unrelated document will find some, because that is what
// it was asked to do. Most NFRs against most documents should produce nothing.
const analysisSystemPrompt = `You review security documentation and propose enrichments to a
catalog of security non-functional requirements (NFRs).

You will be given one NFR and numbered excerpts from a single source document.
Decide whether the excerpts describe how this specific requirement is applied,
implemented or configured in practice.

Rules:

1. Propose an enrichment ONLY if the excerpts contain concrete, specific
   material about this requirement — a configuration, a procedure, a technical
   mechanism, a named standard or parameter. General relevance to the same
   broad topic is not enough.
2. Every claim in your suggested text must come from the excerpts. Do not add
   what you know about how this control is usually implemented, and do not fill
   gaps with plausible values. If the excerpts do not give a figure, the
   suggested text does not contain one.
3. Cite the excerpts you used, and for each give a short verbatim quote copied
   exactly from that excerpt. A quote that is not present in the excerpt you
   cite invalidates the whole proposal.
4. Write the suggested text as catalog prose: direct, factual, no preamble, no
   reference to "the document" or "the excerpts". It replaces the field's
   current content, so include what should remain from it.
5. If nothing in the excerpts genuinely enriches this requirement, return
   {"propose": false} and nothing else. This is the expected outcome for most
   pairings and is always a valid answer.

Reply with JSON only, no markdown fence, in one of these two shapes:

{"propose": false, "reason": "<one sentence>"}

{"propose": true,
 "suggested_text": "<the replacement field text>",
 "rationale": "<one paragraph for the human reviewer>",
 "confidence": <0.0-1.0>,
 "citations": [{"excerpt": <number>, "quote": "<verbatim span from that excerpt>"}]}`

// UsageLogger records token spend. The service is given the app's existing
// ai_usage_log writer so this module's cost lands in the same dashboard as the
// AI Chat gateway's, per POLICY_MODULE_FRAMEWORK §2.5 — one place for spend.
type UsageLogger func(provider, model string, inputTokens, outputTokens int64)

// Analyzer asks the model whether a set of chunks enriches one NFR.
type Analyzer interface {
	Analyze(ctx context.Context, req AnalysisRequest) (AnalysisReply, error)
	Model() string
	Configured() bool
}

// AnalysisRequest is one NFR paired with the chunks retrieved for it.
type AnalysisRequest struct {
	NFR    securitynfr.NFR
	Field  string
	Chunks []ScoredChunk
}

// AnalysisReply is the model's decision, after parsing but before validation.
type AnalysisReply struct {
	Propose       bool
	Reason        string
	SuggestedText string
	Rationale     string
	Confidence    float64
	Citations     []replyCitation
	PromptHash    string
	Model         string
}

type replyCitation struct {
	Excerpt int    `json:"excerpt"`
	Quote   string `json:"quote"`
}

// KeyFunc returns the Anthropic API key to use for the next request. It is
// consulted per call rather than once at startup so a key saved in the Settings
// page takes effect immediately — this module used to tell the operator to set
// an environment variable and restart the service.
//
// It is a plain function rather than a settings dependency to keep this module
// from importing the settings store; the caller supplies the closure.
type KeyFunc func() string

// ClaudeAnalyzer implements Analyzer against the Anthropic Messages API.
type ClaudeAnalyzer struct {
	keyFunc  KeyFunc
	model    string
	logUsage UsageLogger

	// The SDK client is cached and rebuilt only when the resolved key changes,
	// so the common path does not construct one per request.
	mu     sync.Mutex
	api    anthropic.Client
	apiKey string
}

// NewClaudeAnalyzer builds an analyzer that resolves its credential through
// keyFunc. A nil keyFunc falls back to ANTHROPIC_API_KEY, which keeps a
// deployment that has only ever used the environment working untouched.
func NewClaudeAnalyzer(logUsage UsageLogger, keyFunc KeyFunc) *ClaudeAnalyzer {
	if keyFunc == nil {
		keyFunc = func() string { return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) }
	}
	model := strings.TrimSpace(os.Getenv("NFR_ENRICHMENT_MODEL"))
	if model == "" {
		model = DefaultModel
	}
	return &ClaudeAnalyzer{
		keyFunc:  keyFunc,
		model:    model,
		logUsage: logUsage,
	}
}

func (a *ClaudeAnalyzer) Model() string { return a.model }

// Configured reports whether a credential is available right now.
func (a *ClaudeAnalyzer) Configured() bool { return a.key() != "" }

func (a *ClaudeAnalyzer) key() string {
	if a.keyFunc == nil {
		return ""
	}
	return strings.TrimSpace(a.keyFunc())
}

// client returns an SDK client bound to the current key, rebuilding it only
// when the key has changed since the last call.
func (a *ClaudeAnalyzer) client(key string) anthropic.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.apiKey != key {
		a.api = anthropic.NewClient(option.WithAPIKey(key))
		a.apiKey = key
	}
	return a.api
}

// Analyze sends one NFR-and-excerpts prompt and parses the reply.
func (a *ClaudeAnalyzer) Analyze(ctx context.Context, req AnalysisRequest) (AnalysisReply, error) {
	// Resolved once per call and reused below, so a key cleared mid-request
	// cannot leave this method half-configured.
	key := a.key()
	if key == "" {
		return AnalysisReply{}, ErrNotConfigured
	}
	api := a.client(key)

	prompt := BuildPrompt(req)
	hash := sha256.Sum256([]byte(analysisSystemPrompt + "\n" + prompt))
	promptHash := hex.EncodeToString(hash[:])

	resp, err := api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: analysisSystemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return AnalysisReply{}, fmt.Errorf("calling the Anthropic API: %w", err)
	}

	// stop_reason before content: a refusal is a successful HTTP 200 with
	// empty or partial content, so reading content[0] first breaks on it.
	if resp.StopReason == anthropic.StopReasonRefusal {
		return AnalysisReply{}, fmt.Errorf("the model declined to analyse this document")
	}

	if a.logUsage != nil {
		a.logUsage("claude", a.model, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if variant, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(variant.Text)
		}
	}

	reply, err := ParseReply(text.String())
	if err != nil {
		return AnalysisReply{}, err
	}
	reply.PromptHash = promptHash
	reply.Model = a.model
	return reply, nil
}

// BuildPrompt renders the user turn. It is exported so the prompt can be
// inspected and tested without an API key — the prompt is the interface to the
// model, and an untested prompt is an untested interface.
func BuildPrompt(req AnalysisRequest) string {
	var b strings.Builder

	b.WriteString("# Security NFR\n\n")
	writeField(&b, "Key", req.NFR.Key)
	writeField(&b, "Summary", req.NFR.Summary)
	writeField(&b, "Domain", req.NFR.Domain)
	writeField(&b, "Description", req.NFR.Description)
	writeField(&b, "NIST mapping", req.NFR.NISTMapping)

	b.WriteString("\n## Field to enrich: ")
	b.WriteString(req.Field)
	b.WriteString("\n\nCurrent content of that field:\n\n")
	current := strings.TrimSpace(fieldValue(req.NFR, req.Field))
	if current == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(current)
		b.WriteString("\n")
	}

	b.WriteString("\n# Source document excerpts\n")
	for i, sc := range req.Chunks {
		fmt.Fprintf(&b, "\n## Excerpt %d\n", i+1)
		if sc.Chunk.Heading != "" {
			fmt.Fprintf(&b, "Section: %s\n", sc.Chunk.Heading)
		}
		b.WriteString("\n")
		b.WriteString(sc.Chunk.Text)
		b.WriteString("\n")
	}

	return b.String()
}

func writeField(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", label, value)
}

// fieldValue reads the enrichable field named by field.
func fieldValue(nfr securitynfr.NFR, field string) string {
	switch field {
	case FieldImplementation:
		return nfr.Implementation
	case FieldAdditionalDetails:
		return nfr.AdditionalDetails
	case FieldDescription:
		return nfr.Description
	default:
		return ""
	}
}

// applyField returns a copy of nfr with field set to value.
func applyField(nfr securitynfr.NFR, field, value string) (securitynfr.NFR, error) {
	switch field {
	case FieldImplementation:
		nfr.Implementation = value
	case FieldAdditionalDetails:
		nfr.AdditionalDetails = value
	case FieldDescription:
		nfr.Description = value
	default:
		return nfr, invalidf("%q is not an enrichable field", field)
	}
	return nfr, nil
}

// ParseReply decodes the model's JSON, tolerating a markdown fence.
//
// The fence tolerance is not politeness: instructing "JSON only" is followed
// almost always, and a run that discards one proposal in fifty over three
// backticks costs a real enrichment for no safety gain. The checks that matter
// are in ValidateReply, and they are not relaxed.
func ParseReply(raw string) (AnalysisReply, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return AnalysisReply{}, fmt.Errorf("the model returned an empty reply")
	}
	text = stripFence(text)

	var parsed struct {
		Propose       bool            `json:"propose"`
		Reason        string          `json:"reason"`
		SuggestedText string          `json:"suggested_text"`
		Rationale     string          `json:"rationale"`
		Confidence    float64         `json:"confidence"`
		Citations     []replyCitation `json:"citations"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return AnalysisReply{}, fmt.Errorf("the model reply was not valid JSON: %w", err)
	}

	return AnalysisReply{
		Propose:       parsed.Propose,
		Reason:        strings.TrimSpace(parsed.Reason),
		SuggestedText: strings.TrimSpace(parsed.SuggestedText),
		Rationale:     strings.TrimSpace(parsed.Rationale),
		Confidence:    parsed.Confidence,
		Citations:     parsed.Citations,
	}, nil
}

func stripFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = text[idx+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "```"))
}

// ValidateReply is the gate between a model reply and a stored proposal.
//
// It enforces §2.2 mechanically. A reply survives only if it cites at least one
// excerpt that was actually supplied, and every quote it gives is really
// present in the excerpt it is attributed to. That last check is the one that
// catches a fabricated implementation detail: the model can write any prose it
// likes, but it cannot produce a verbatim span of a document that does not
// contain it.
func ValidateReply(reply AnalysisReply, chunks []ScoredChunk) ([]Citation, error) {
	if strings.TrimSpace(reply.SuggestedText) == "" {
		return nil, fmt.Errorf("the proposal has no suggested text")
	}
	if len(reply.Citations) == 0 {
		return nil, fmt.Errorf("the proposal cites no source excerpt")
	}

	var (
		out  []Citation
		seen = map[int64]bool{}
	)
	for _, cite := range reply.Citations {
		// Excerpts are 1-based in the prompt, which is what the model sees.
		idx := cite.Excerpt - 1
		if idx < 0 || idx >= len(chunks) {
			return nil, fmt.Errorf("the proposal cites excerpt %d, which was not supplied", cite.Excerpt)
		}
		chunk := chunks[idx].Chunk

		quote := strings.TrimSpace(cite.Quote)
		if quote == "" {
			return nil, fmt.Errorf("the citation of excerpt %d has no quote", cite.Excerpt)
		}
		if !containsNormalized(chunk.Text, quote) {
			return nil, fmt.Errorf(
				"the quote cited for excerpt %d does not appear in that excerpt", cite.Excerpt)
		}

		if seen[chunk.ID] {
			continue
		}
		seen[chunk.ID] = true
		out = append(out, Citation{ChunkID: chunk.ID, Heading: chunk.Heading, Quote: quote})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ChunkID < out[j].ChunkID })
	return out, nil
}

// containsNormalized compares ignoring whitespace runs and case.
//
// Exact byte matching would reject honest quotes over a line wrap the model
// collapsed to a space, which is the most common harmless difference and would
// train a reader to distrust the check. Normalising whitespace and case still
// makes an invented sentence impossible to pass — the words and their order
// must be present.
func containsNormalized(haystack, needle string) bool {
	return strings.Contains(normalizeSpace(haystack), normalizeSpace(needle))
}

func normalizeSpace(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
