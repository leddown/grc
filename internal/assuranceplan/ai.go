package assuranceplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"grc/internal/aiprovider"
	"grc/internal/controlid"
	"grc/internal/knowledge"
)

// AI assist drafts a plan from a brief, proposes test attributes from a
// shortlist of the catalogs, and drafts committee narrative. Each answer is a
// proposal: it is checked, its references are resolved, and it is stored with
// the model, the prompt hash and the time. Nothing reaches the plan until a
// person accepts it, and what they accept goes through the same validation as
// a hand edit. Every other feature works with no model configured.

// Asker is the slice of internal/aiprovider this module needs, so the
// provider, its credentials, the Settings model and the shared ai_usage_log
// stay in one place. *aiprovider.Router satisfies it.
type Asker interface {
	Available() bool
	Describe() string
	Ask(ctx context.Context, req aiprovider.Request) (aiprovider.Response, error)
}

// Retriever reads the catalogs a proposal cites: Security NFRs, 800-53
// controls and Regulation Coverage clauses. *knowledge.Service satisfies it;
// its lexical search builds the shortlist the model judges.
type Retriever interface {
	Search(kind, query string, limit int) (*knowledge.SearchResult, error)
	Get(kind, ref string) (*knowledge.Item, error)
}

type aiDeps struct {
	asker     Asker
	retriever Retriever
	mu        sync.Mutex
	busy      map[int64]bool
}

// WithAI attaches the provider and the catalogs. Either may be nil.
func (s *Service) WithAI(asker Asker, retriever Retriever) *Service {
	s.ai = &aiDeps{asker: asker, retriever: retriever, busy: map[int64]bool{}}
	return s
}

// AIConfigured reports whether a model can be asked.
func (s *Service) AIConfigured() bool {
	return s.ai != nil && s.ai.asker != nil && s.ai.asker.Available()
}

// AIModel describes what would answer, for the page.
func (s *Service) AIModel() string {
	if !s.AIConfigured() {
		return ""
	}
	return s.ai.asker.Describe()
}

const (
	AIKindDraft      = "draft"
	AIKindAttributes = "attributes"
	AIKindNarrative  = "narrative"

	AIProposed = "proposed"
	AIAccepted = "accepted"
	AIRejected = "rejected"

	BasisRegulation   = "regulation"
	BasisGoodPractice = "good_practice"

	RefKindPlan = "plan"

	maxBrief         = 4000
	maxAnswerTokens  = 8000
	maxShortlistKind = 12
	maxAIText        = 4000
)

var AIKinds = []string{AIKindDraft, AIKindAttributes, AIKindNarrative}

// AIProposal is one answer and its decision. Payload is what the model
// proposed after the server's checks; Applied is what a person accepted, with
// their edits, and is empty until then.
type AIProposal struct {
	ID           int64           `json:"id"`
	PlanID       int64           `json:"plan_id"`
	Kind         string          `json:"kind"`
	Document     string          `json:"document"`
	Revision     int             `json:"revision"`
	Input        json.RawMessage `json:"input"`
	Model        string          `json:"model"`
	PromptHash   string          `json:"prompt_hash"`
	Payload      json.RawMessage `json:"payload"`
	Status       string          `json:"status"`
	Applied      json.RawMessage `json:"applied"`
	DecisionNote string          `json:"decision_note"`
	CreatedAt    string          `json:"created_at"`
	CreatedBy    string          `json:"created_by"`
	DecidedBy    string          `json:"decided_by"`
	DecidedAt    string          `json:"decided_at"`
}

// AIRef is a reference a proposal cites, as resolved: against the shortlist
// the model was shown, the catalogs, or the plan's own references. One that
// resolves nowhere is listed as unresolved and never stored on the plan.
type AIRef struct {
	Kind        string `json:"kind"`
	Ref         string `json:"ref"`
	Title       string `json:"title,omitempty"`
	URL         string `json:"url,omitempty"`
	OnShortlist bool   `json:"on_shortlist"`
}

// aiCite is a reference as the model wrote it.
type aiCite struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// UpstreamError is a provider failure: the handler answers 502.
type UpstreamError struct{ msg string }

func (e *UpstreamError) Error() string { return e.msg }

// RefusedError is an answer that cannot be used as it stands, a refusal or one
// cut off: the handler answers 422.
type RefusedError struct{ msg string }

func (e *RefusedError) Error() string { return e.msg }

// aiRules are the standing rules of every assist. They are the system prompt,
// so no brief can talk the model out of them.
const aiRules = `You assist the second line of defence (an ICT risk control function at a regulated European financial institution) in planning and reporting its assurance work. Your output is a proposal a person will accept, edit or reject.

Standing rules:
1. Say when you do not know. Put anything you could not determine from what you were given in "unknowns", rather than guessing.
2. Never invent a control identifier, an NFR key, a regulatory article, a threshold or a deadline. Cite only references from the lists you are shown, exactly as written there. A figure, date or percentage you state must come from the data you were given, or from the brief.
3. Keep what the regulation requires separate from good practice. Mark each item "regulation" only when a cited regulation reference requires it; otherwise mark it "good_practice".
4. Dates you propose for the plan's own schedule are proposals for that schedule, never regulatory deadlines.
5. Reply with one JSON object in the contract you are given, and nothing before or after it.`

// begin claims the plan for one AI request at a time, so a double click does
// not pay for two answers.
func (s *Service) begin(id int64) (func(), error) {
	if !s.AIConfigured() {
		return nil, conflict("no AI provider is configured: set the Wintermute server in Settings, or do this by hand; every other feature works without one")
	}
	s.ai.mu.Lock()
	defer s.ai.mu.Unlock()
	if s.ai.busy[id] {
		return nil, conflict("an AI request for this plan is already running; wait for its answer")
	}
	s.ai.busy[id] = true
	return func() {
		s.ai.mu.Lock()
		delete(s.ai.busy, id)
		s.ai.mu.Unlock()
	}, nil
}

func promptHash(system, prompt string) string {
	sum := sha256.Sum256([]byte(system + "\x1f" + prompt))
	return hex.EncodeToString(sum[:])
}

// askJSON asks for an answer in a JSON contract and repairs once when the
// reply does not parse. A refusal or an answer cut off is never parsed: it can
// look like the contract and not be it.
func (s *Service) askJSON(ctx context.Context, prompt string, parse func(string) error) (aiprovider.Response, error) {
	req := aiprovider.Request{System: aiRules, Prompt: prompt, MaxTokens: maxAnswerTokens}
	resp, err := s.ai.asker.Ask(ctx, req)
	if err != nil {
		return resp, &UpstreamError{msg: fmt.Sprintf("the AI provider could not answer: %v", err)}
	}
	if err := unusable(resp); err != nil {
		return resp, err
	}
	perr := parse(resp.Text)
	if perr == nil {
		return resp, nil
	}
	repair := aiprovider.Request{System: aiRules, MaxTokens: maxAnswerTokens,
		Prompt: "Your reply did not match the contract: " + perr.Error() + ". Reply again with only the JSON object, nothing before or after it."}
	if resp.SessionID != "" {
		repair.SessionID = resp.SessionID
	} else {
		repair.History = []aiprovider.Message{{Role: aiprovider.RoleUser, Text: prompt}, {Role: aiprovider.RoleAssistant, Text: resp.Text}}
	}
	first := resp
	resp, err = s.ai.asker.Ask(ctx, repair)
	if err != nil {
		return first, &UpstreamError{msg: fmt.Sprintf("the AI provider could not answer: %v", err)}
	}
	if err := unusable(resp); err != nil {
		return resp, err
	}
	if perr = parse(resp.Text); perr != nil {
		return resp, &UpstreamError{msg: fmt.Sprintf("the AI's answer was not in the expected form, even after asking again (%v); nothing was stored", perr)}
	}
	if resp.Model == "" {
		resp.Model = first.Model
	}
	return resp, nil
}

func unusable(resp aiprovider.Response) error {
	switch {
	case resp.Refused || resp.StopReason == aiprovider.StopRefusal:
		return &RefusedError{msg: "the model declined this request; nothing was stored"}
	case resp.StopReason == aiprovider.StopMaxTokens:
		return &RefusedError{msg: "the answer was cut off before it was complete; ask for less (fewer attributes, a shorter brief) and try again"}
	}
	return nil
}

func modelOf(resp aiprovider.Response) string {
	return firstNonEmpty(resp.Model, resp.Backend, resp.Provider, "unknown model")
}

// decodeContract reads the JSON object out of an answer, allowing a fenced
// block or text around it, and refuses unknown shapes by decoding strictly.
func decodeContract(text string, out any) error {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("not the JSON object asked for: %w", err)
	}
	return nil
}

// shortlist is the candidates the model was shown, keyed by kind and ref.
type shortlist struct {
	items []AIRef
	byKey map[string]AIRef
}

func citeKey(kind, ref string) string {
	if kind == knowledge.KindControl {
		ref = controlid.Normalize(ref)
	}
	return kind + "|" + strings.ToLower(strings.TrimSpace(ref))
}

func normalizeCiteKind(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "nfr", "security_nfr":
		return knowledge.KindNFR
	case "control", "nist", "800-53":
		return knowledge.KindControl
	case "regulation_clause", "clause", "regulation", "article":
		return knowledge.KindRegulationClause
	case "plan", "plan_reference":
		return RefKindPlan
	}
	return strings.ToLower(strings.TrimSpace(k))
}

// buildShortlist searches each kind for the query. The model judges this
// list; it does not browse.
func (s *Service) buildShortlist(query string, kinds map[string]int) (shortlist, string, error) {
	sl := shortlist{byKey: map[string]AIRef{}}
	var b strings.Builder
	if s.ai.retriever == nil {
		return sl, "", nil
	}
	for _, kind := range []string{knowledge.KindNFR, knowledge.KindControl, knowledge.KindRegulationClause} {
		limit := kinds[kind]
		if limit == 0 {
			continue
		}
		res, err := s.ai.retriever.Search(kind, query, limit)
		if err != nil {
			return sl, "", fmt.Errorf("search the %s catalog: %w", kind, err)
		}
		if res == nil {
			continue
		}
		for _, h := range res.Hits {
			ref := AIRef{Kind: kind, Ref: h.Item.Ref, Title: h.Item.Title, URL: h.Item.URL, OnShortlist: true}
			key := citeKey(kind, ref.Ref)
			if _, dup := sl.byKey[key]; dup || ref.Ref == "" {
				continue
			}
			sl.byKey[key] = ref
			sl.items = append(sl.items, ref)
			line := clip(oneLine(firstNonEmpty(h.Item.Summary, h.Item.Title)), 300)
			if kind == knowledge.KindRegulationClause && h.Item.Fields["requirement"] != "" {
				line = clip(oneLine(h.Item.Fields["requirement"]), 300)
			}
			fmt.Fprintf(&b, "- {\"kind\": %q, \"ref\": %q} %s (%s): %s\n", kind, ref.Ref, clip(oneLine(h.Item.Title), 120), clip(h.Item.Group, 60), line)
		}
	}
	return sl, b.String(), nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// planRefs are the plan's own references: its header references, split at
// semicolons, and the references of its regulatory basis rows.
func planRefs(p Plan) []string {
	out := []string{}
	add := func(r string) {
		r = strings.TrimSpace(r)
		if r != "" && !slices.ContainsFunc(out, func(x string) bool { return strings.EqualFold(x, r) }) {
			out = append(out, r)
		}
	}
	for _, r := range strings.Split(p.RegulatoryRefs, ";") {
		add(r)
	}
	for _, b := range p.Basis {
		add(b.Reference)
	}
	return out
}

// resolveCites resolves each citation against the shortlist, then the
// catalogs, then the plan's own references. Nothing the model cites is
// trusted by its spelling alone.
func (s *Service) resolveCites(cites []aiCite, sl shortlist, own []string) (resolved, unresolved []AIRef) {
	resolved, unresolved = []AIRef{}, []AIRef{}
	seen := map[string]bool{}
	for _, c := range cites {
		kind, ref := normalizeCiteKind(c.Kind), strings.TrimSpace(c.Ref)
		if ref == "" {
			continue
		}
		key := citeKey(kind, ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		if hit, ok := sl.byKey[key]; ok {
			resolved = append(resolved, hit)
			continue
		}
		switch kind {
		case RefKindPlan:
			if i := slices.IndexFunc(own, func(x string) bool { return strings.EqualFold(x, ref) }); i >= 0 {
				resolved = append(resolved, AIRef{Kind: RefKindPlan, Ref: own[i], OnShortlist: true})
				continue
			}
		case knowledge.KindNFR, knowledge.KindControl, knowledge.KindRegulationClause:
			if s.ai.retriever != nil {
				lookup := ref
				if kind == knowledge.KindControl {
					lookup = controlid.Normalize(ref)
				}
				if item, err := s.ai.retriever.Get(kind, lookup); err == nil && item != nil {
					resolved = append(resolved, AIRef{Kind: kind, Ref: item.Ref, Title: item.Title, URL: item.URL})
					continue
				}
			}
		}
		unresolved = append(unresolved, AIRef{Kind: kind, Ref: clip(ref, 120)})
	}
	return resolved, unresolved
}

func citesOf(refs ...[]AIRef) []aiCite {
	out := []aiCite{}
	for _, list := range refs {
		for _, r := range list {
			out = append(out, aiCite{Kind: r.Kind, Ref: r.Ref})
		}
	}
	return out
}

func refList(refs []AIRef) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, r.Ref)
	}
	return strings.Join(parts, ", ")
}

func cleanList(in []string, max, each int) []string {
	out := []string{}
	for _, v := range in {
		if v = clip(strings.TrimSpace(v), each); v != "" && len(out) < max {
			out = append(out, v)
		}
	}
	return out
}

// record stores a proposal. The input and payload are stored as JSON.
func (s *Service) record(p Plan, kind, document string, input, payload any, resp aiprovider.Response, hash, actor string) (AIProposal, error) {
	in, err := json.Marshal(input)
	if err != nil {
		return AIProposal{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AIProposal{}, err
	}
	pr := AIProposal{PlanID: p.ID, Kind: kind, Document: document, Revision: p.Revision, Input: in, Model: modelOf(resp),
		PromptHash: hash, Payload: body, Status: AIProposed, CreatedAt: s.stamp(), CreatedBy: actor}
	h := HistoryEntry{At: pr.CreatedAt, Actor: actor, Event: "ai proposal", Subject: kind,
		ToValue: AIProposed, Note: fmt.Sprintf("%s, prompt %s", pr.Model, hash[:12])}
	id, err := s.repo.CreateAIProposal(pr, []HistoryEntry{h})
	if err != nil {
		return AIProposal{}, err
	}
	return s.repo.AIProposal(p.ID, id)
}

// AIProposals lists a plan's proposals, newest first.
func (s *Service) AIProposals(id int64) ([]AIProposal, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	return s.repo.AIProposals(id)
}

func (s *Service) AIProposal(id, pid int64) (AIProposal, error) {
	pr, err := s.repo.AIProposal(id, pid)
	return pr, mapNotFound(err)
}

// DecideInput is a person's decision. Payload, when given, is the proposal
// as they edited it, in the proposal's own shape; it is checked again as if
// the model had written it. Parts picks what a draft applies (steps,
// quarters, raci, objective) and Attributes which attributes are kept; empty
// means all of them.
type DecideInput struct {
	Decision   string          `json:"decision"`
	Note       string          `json:"note"`
	Payload    json.RawMessage `json:"payload"`
	Parts      []string        `json:"parts"`
	Attributes []string        `json:"attributes"`
}

type DecideResult struct {
	Proposal AIProposal `json:"proposal"`
	Plan     Plan       `json:"plan"`
	Notes    []string   `json:"notes"`
}

// Decide accepts or rejects a proposal, once.
func (s *Service) Decide(id, pid int64, in DecideInput, actor string) (DecideResult, error) {
	pr, err := s.AIProposal(id, pid)
	if err != nil {
		return DecideResult{}, err
	}
	if pr.Status != AIProposed {
		return DecideResult{}, conflict("proposal %d was already %s by %s", pr.ID, pr.Status, firstNonEmpty(pr.DecidedBy, "someone"))
	}
	note := clip(strings.TrimSpace(in.Note), maxAIText)
	switch strings.ToLower(strings.TrimSpace(in.Decision)) {
	case "reject", AIRejected:
		pr.Status, pr.DecisionNote, pr.DecidedBy, pr.DecidedAt = AIRejected, note, actor, s.stamp()
		h := HistoryEntry{At: pr.DecidedAt, Actor: actor, Event: "ai proposal", Subject: pr.Kind,
			FromValue: AIProposed, ToValue: AIRejected, Note: firstNonEmpty(note, fmt.Sprintf("proposal %d", pr.ID))}
		if err := s.repo.DecideAIProposal(pr, []HistoryEntry{h}); err != nil {
			return DecideResult{}, err
		}
		p, err := s.Get(id)
		if err != nil {
			return DecideResult{}, err
		}
		pr, err = s.AIProposal(id, pid)
		return DecideResult{Proposal: pr, Plan: p, Notes: []string{}}, err
	case "accept", AIAccepted:
	default:
		return DecideResult{}, invalid("the decision is accept or reject")
	}
	payload := pr.Payload
	if len(in.Payload) > 0 && string(in.Payload) != "null" {
		payload = in.Payload
	}
	var applied any
	var notes []string
	switch pr.Kind {
	case AIKindDraft:
		applied, notes, err = s.acceptDraft(id, pr, payload, in.Parts, actor)
	case AIKindAttributes:
		applied, notes, err = s.acceptAttributes(id, pr, payload, in.Attributes, actor)
	case AIKindNarrative:
		applied, notes, err = s.acceptNarrative(id, pr, payload)
	default:
		return DecideResult{}, conflict("proposal %d is of an unknown kind %q", pr.ID, pr.Kind)
	}
	if err != nil {
		return DecideResult{}, err
	}
	body, err := json.Marshal(applied)
	if err != nil {
		return DecideResult{}, err
	}
	pr.Status, pr.Applied, pr.DecisionNote, pr.DecidedBy, pr.DecidedAt = AIAccepted, body, note, actor, s.stamp()
	h := HistoryEntry{At: pr.DecidedAt, Actor: actor, Event: "ai proposal", Subject: pr.Kind, FromValue: AIProposed, ToValue: AIAccepted,
		Note: strings.Join(append([]string{fmt.Sprintf("proposal %d, %s, prompt %s", pr.ID, pr.Model, clip(pr.PromptHash, 12))}, notes...), "; ")}
	if err := s.repo.DecideAIProposal(pr, []HistoryEntry{h}); err != nil {
		return DecideResult{}, err
	}
	p, err := s.Get(id)
	if err != nil {
		return DecideResult{}, err
	}
	pr, err = s.AIProposal(id, pid)
	if notes == nil {
		notes = []string{}
	}
	return DecideResult{Proposal: pr, Plan: p, Notes: notes}, err
}

// decodePayload reads a stored or edited payload strictly.
func decodePayload(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return invalid("the edited proposal is not in the proposal's shape: %v", err)
	}
	return nil
}
