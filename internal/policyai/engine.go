package policyai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"grc/internal/aiprovider"
	"grc/internal/db"
	"grc/internal/knowledge"
	"grc/internal/policydocs"
	"grc/internal/policystudio"
)

// Config wires the engine. The two settings functions are read per request.
type Config struct {
	Conn      *db.Conn
	Studio    *policystudio.Service
	Policies  *policydocs.Service
	Knowledge *knowledge.Service
	Router    *aiprovider.Router
	// Agent names the Wintermute agent for the Studio (ai.policy.agent);
	// empty keeps the agent Settings configures for everything else.
	Agent func() string
	// SendDocument sends the document with every dock question to a
	// Wintermute agent (ai.policy.send_document), rather than when its
	// conversation starts and whenever the document has changed.
	SendDocument func() bool
}

// Engine is the proposal engine.
type Engine struct {
	cfg Config
	now func() time.Time

	mu       sync.Mutex
	inflight map[string]bool
	recent   map[int64][]time.Time
	sent     map[string]string // dock session -> context fingerprint
}

// NewEngine returns an engine.
func NewEngine(cfg Config) *Engine {
	return &Engine{cfg: cfg, now: time.Now, inflight: map[string]bool{}, recent: map[int64][]time.Time{}, sent: map[string]string{}}
}

// Limits. One request at a time per person: a proposal takes the better part
// of a minute, and a second one against the same text would only race it.
const (
	requestTimeout    = 150 * time.Second
	ratePerDocument   = 8
	rateWindow        = time.Minute
	maxAnswerTokens   = 8000
	maxDockSessions   = 1000
	maxAnswerMarkdown = 20000
)

// Refusal is a request the engine will not run, with what to tell the person.
type Refusal struct {
	Status int
	Msg    string
}

func (r Refusal) Error() string { return r.Msg }

func refuse(status int, format string, args ...any) Refusal {
	return Refusal{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// Scopes.
const (
	ScopeSelection = "selection"
	ScopeSection   = "section"
	ScopeDocument  = "document"
)

// Selection is what the person had selected.
type Selection struct {
	SectionUID string   `json:"section_uid"`
	BlockIDs   []string `json:"block_ids"`
	Quote      string   `json:"quote"`
}

// Request is one proposal request from the Studio.
type Request struct {
	Instruction string    `json:"instruction"`
	Action      string    `json:"action"`
	Scope       string    `json:"scope"`
	Selection   Selection `json:"selection"`
	// LibraryDocumentID names the Wintermute library document a from_library
	// request maps in.
	LibraryDocumentID int64 `json:"library_document_id"`
}

// Result is a validated proposal, as the editor needs it.
type Result struct {
	ProposalID  int64            `json:"id"`
	Answer      string           `json:"answer_markdown"`
	HasProposal bool             `json:"has_proposal"`
	Summary     string           `json:"summary"`
	Destination string           `json:"destination"`
	AILabel     string           `json:"ai_label"`
	Provider    string           `json:"provider"`
	Model       string           `json:"model"`
	Edits       []CheckedEdit    `json:"edits"`
	Mappings    []CheckedMapping `json:"control_mappings"`
	NewFacts    []NewFact        `json:"new_facts"`
	Rejected    int              `json:"rejected"`
	SessionID   string           `json:"session_id,omitempty"`
}

// Status says whether proposals can be asked for on a document, and where
// they would go.
type Status struct {
	Available   bool   `json:"available"`
	Destination string `json:"destination"`
	Reason      string `json:"reason,omitempty"`
	AIPolicy    string `json:"ai_policy"`
}

// route decides where a document's request may go. It is the egress rule
// (ai_policy) and the structured-output rule (Q6) in one place, applied when
// the request is built and never after.
func (e *Engine) route(ctx context.Context, doc policydocs.Document) (aiprovider.Provider, string, error) {
	if doc.AIPolicy == policydocs.AIPolicyOff {
		return nil, "", refuse(http.StatusForbidden, "AI is switched off for this document. An administrator can change that under Document control.")
	}
	if e.cfg.Router == nil {
		return nil, "", refuse(http.StatusServiceUnavailable, "No AI provider is configured. Set one in Settings → AI providers.")
	}
	provider, err := e.cfg.Router.Selected()
	if err != nil {
		return nil, "", refuse(http.StatusServiceUnavailable, "No AI provider is available: %v", err)
	}
	switch p := provider.(type) {
	case *aiprovider.Claude:
		if doc.AIPolicy == policydocs.AIPolicyLocalOnly {
			return nil, "", refuse(http.StatusForbidden, "This document is local only, and AI requests go to Claude (cloud). Select Wintermute in Settings → AI providers, or change the document's AI setting.")
		}
		model := p.ResolvedModel()
		ok, err := p.SupportsStructuredOutputs(ctx, model)
		if err != nil {
			return nil, "", refuse(http.StatusBadGateway, "Couldn't confirm that %s supports structured outputs, which proposals need: %v", model, err)
		}
		if !ok {
			return nil, "", refuse(http.StatusUnprocessableEntity, "%s does not support structured outputs, which proposals need. Choose another Claude model in Settings.", model)
		}
		return provider, "AI · Claude (cloud) · " + model, nil
	default:
		agent := ""
		if e.cfg.Agent != nil {
			agent = strings.TrimSpace(e.cfg.Agent())
		}
		if agent == "" {
			// The server's configured agent answers; which one is Settings'
			// business, not something to guess at here.
			return provider, "AI · Wintermute", nil
		}
		return provider, "AI · Wintermute · " + agent, nil
	}
}

// Status reports availability for a document without asking a model.
func (e *Engine) Status(ctx context.Context, documentID int64) (Status, error) {
	doc, err := e.cfg.Policies.GetDocument(documentID)
	if err != nil {
		return Status{}, err
	}
	st := Status{AIPolicy: doc.AIPolicy}
	_, dest, err := e.route(ctx, doc)
	var r Refusal
	if errors.As(err, &r) {
		st.Reason = r.Msg
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Available, st.Destination = true, dest
	return st, nil
}

// acquire enforces one request per person and the per-document rate.
func (e *Engine) acquire(actor string, documentID int64) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inflight[actor] {
		return nil, refuse(http.StatusConflict, "Your previous AI request is still running. Wait for it, or cancel it, before asking again.")
	}
	cutoff := e.now().Add(-rateWindow)
	kept := e.recent[documentID][:0]
	for _, t := range e.recent[documentID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= ratePerDocument {
		e.recent[documentID] = kept
		return nil, refuse(http.StatusTooManyRequests, "This document has had %d AI requests in the last minute. Try again shortly.", ratePerDocument)
	}
	e.recent[documentID] = append(kept, e.now())
	e.inflight[actor] = true
	return func() {
		e.mu.Lock()
		delete(e.inflight, actor)
		e.mu.Unlock()
	}, nil
}

// prepared is a request ready to send.
type prepared struct {
	doc      policydocs.Document
	pc       promptContext
	src      sources
	provider aiprovider.Provider
	dest     string
	agent    string
}

func (e *Engine) prepare(ctx context.Context, documentID int64, scope string, sel Selection, sendAll bool) (prepared, error) {
	var p prepared
	doc, err := e.cfg.Policies.GetDocument(documentID)
	if err != nil {
		return p, err
	}
	if doc.EditorFormat != policydocs.EditorStudio {
		return p, refuse(http.StatusConflict, "Open this document in the Studio first.")
	}
	if doc.Status != policydocs.StatusDraft && doc.Status != policydocs.StatusInReview {
		return p, refuse(http.StatusConflict, "The document is %s; AI proposals are for drafts and documents in review.", doc.Status)
	}
	provider, dest, err := e.route(ctx, doc)
	if err != nil {
		return p, err
	}
	state, _, err := e.cfg.Studio.State(documentID, policystudio.Identity{Admin: true, CanReadPolicies: true})
	if err != nil {
		return p, err
	}
	sections, err := e.cfg.Studio.LiveSections(documentID)
	if err != nil {
		return p, err
	}
	blocks := documentBlocks(sections)
	byID := map[string]block{}
	for _, b := range blocks {
		byID[b.ID] = b
	}

	inScope := map[string]bool{}
	tag := "the whole document"
	switch {
	case sendAll || scope == ScopeDocument || scope == "":
		for _, b := range blocks {
			inScope[b.ID] = true
		}
	case scope == ScopeSelection && len(sel.BlockIDs) > 0:
		for _, id := range sel.BlockIDs {
			if _, ok := byID[id]; ok {
				inScope[id] = true
			}
		}
		tag = "the selected blocks"
		if len(inScope) == 0 {
			return p, refuse(http.StatusBadRequest, "The selection is no longer in the document. Select the text again.")
		}
	case scope == ScopeSection || scope == ScopeSelection:
		for _, b := range blocks {
			if b.SectionUID == sel.SectionUID {
				inScope[b.ID] = true
			}
		}
		tag = "one section"
		if len(inScope) == 0 {
			return p, refuse(http.StatusBadRequest, "That section is not in the document.")
		}
	default:
		return p, refuse(http.StatusBadRequest, "scope must be selection, section or document")
	}
	scopeSections := map[string]bool{}
	var query strings.Builder
	for _, b := range blocks {
		if inScope[b.ID] {
			scopeSections[b.SectionUID] = true
			query.WriteString(b.Text + "\n")
		}
	}

	var findings []policydocs.Finding
	sectionIDs := map[int64]bool{}
	for _, s := range state.Sections {
		if scopeSections[s.UID] {
			sectionIDs[s.ID] = true
		}
	}
	for _, f := range state.Findings {
		if f.SectionID == 0 || sectionIDs[f.SectionID] {
			findings = append(findings, f)
		}
	}
	facts := map[string]string{}
	declared := map[string]bool{}
	for _, f := range state.Facts {
		declared[f.Key] = true
		if f.Resolved {
			facts[f.Key] = f.Value
		}
	}
	sectionSet := map[string]bool{}
	for _, s := range state.Sections {
		if !s.Detached {
			sectionSet[s.UID] = true
		}
	}

	items := func(kind string) []knowledge.Item {
		if e.cfg.Knowledge == nil {
			return nil
		}
		out, err := e.cfg.Knowledge.Items(kind)
		if err != nil {
			return nil
		}
		return out
	}
	controls := items(knowledge.KindControl)
	agent := ""
	if e.cfg.Agent != nil {
		agent = strings.TrimSpace(e.cfg.Agent())
	}
	_, wintermute := provider.(*aiprovider.Wintermute)
	p = prepared{doc: doc, provider: provider, dest: dest, agent: agent}
	p.pc = promptContext{
		doc: doc, outline: state.Sections, blocks: blocks, scope: inScope, scopeTag: tag,
		pending: pendingIn(sections, inScope), findings: findings, facts: state.Facts,
		controls: shortlist(controls, query.String(), maxControls),
		clauses:  shortlist(items(knowledge.KindRegulationClause), query.String(), maxClauses),
		selected: strings.TrimSpace(sel.Quote), agentRef: wintermute,
	}
	lookups := map[string]map[string]knowledge.Item{}
	p.src = sources{
		blocks: byID, scope: inScope, sections: sectionSet, facts: facts, declared: declared, newID: policydocs.NewUID,
		lookup: func(kind, ref string) (knowledge.Item, bool) {
			m, ok := lookups[kind]
			if !ok {
				m = map[string]knowledge.Item{}
				for _, it := range items(kind) {
					m[it.Ref] = it
				}
				lookups[kind] = m
			}
			it, ok := m[ref]
			return it, ok
		},
	}
	return p, nil
}

// pendingIn describes the undecided suggestions inside the scope.
func pendingIn(sections []*policystudio.Node, scope map[string]bool) []string {
	var out []string
	var walk func(n *policystudio.Node, bid string)
	walk = func(n *policystudio.Node, bid string) {
		if b := n.Attr("bid"); b != "" {
			bid = b
		}
		for _, m := range n.Marks {
			if (m.Type == "insertion" || m.Type == "deletion") && scope[bid] {
				text := clip(n.TextContent(), 160)
				if text == "" {
					text = "a whole " + n.Type
				}
				who, _ := m.Attrs["authorName"].(string)
				out = append(out, fmt.Sprintf("[%s] %s %q by %s", bid, map[string]string{"insertion": "insert", "deletion": "delete"}[m.Type], text, firstNonEmpty(who, "someone")))
			}
		}
		for _, c := range n.Content {
			walk(c, bid)
		}
	}
	for _, s := range sections {
		walk(s, "")
	}
	if len(out) > 40 {
		out = append(out[:40], fmt.Sprintf("… and %d more", len(out)-40))
	}
	return out
}

// ask sends one request and returns a parsed answer, with at most one repair
// turn for an answer that is not the contract. A refusal or an answer cut off
// at the token limit is never parsed: it may look like the contract and not be
// it.
func (e *Engine) ask(ctx context.Context, p prepared, req aiprovider.Request, stream Stream) (Answer, aiprovider.Response, error) {
	req.System = standingRules
	req.OutputSchema = Schema()
	req.MaxTokens = maxAnswerTokens
	req.Agent = p.agent
	resp, err := e.cfg.Router.AskStream(ctx, req, previewTo(stream))
	if err != nil {
		return Answer{}, resp, refuse(http.StatusBadGateway, "The AI provider could not answer: %v", err)
	}
	if err := stopped(resp); err != nil {
		return Answer{}, resp, err
	}
	answer, perr := parseAnswer(resp.Text)
	if perr == nil {
		return answer, resp, nil
	}
	repair := aiprovider.Request{
		System: standingRules, OutputSchema: req.OutputSchema, MaxTokens: maxAnswerTokens, Agent: p.agent,
		Prompt: "Your reply did not match the contract: " + perr.Error() + ". Reply again with only the JSON object, nothing before or after it.",
	}
	if resp.SessionID != "" {
		repair.SessionID = resp.SessionID
	} else {
		repair.History = append(append([]aiprovider.Message(nil), req.History...),
			aiprovider.Message{Role: aiprovider.RoleUser, Text: req.Prompt}, aiprovider.Message{Role: aiprovider.RoleAssistant, Text: resp.Text})
	}
	first := resp
	if stream != nil {
		stream.Restart()
	}
	resp, err = e.cfg.Router.AskStream(ctx, repair, previewTo(stream))
	if err != nil {
		return Answer{}, first, refuse(http.StatusBadGateway, "The AI provider could not answer: %v", err)
	}
	resp.Usage.InputTokens += first.Usage.InputTokens
	resp.Usage.OutputTokens += first.Usage.OutputTokens
	if err := stopped(resp); err != nil {
		return Answer{}, resp, err
	}
	answer, perr = parseAnswer(resp.Text)
	if perr != nil {
		return Answer{}, resp, refuse(http.StatusBadGateway, "The AI's answer was not in the expected form, even after asking again (%v). Try again, or ask about a smaller part of the document.", perr)
	}
	return answer, resp, nil
}

// previewTo is the text callback that streams an answer's answer_markdown to
// stream, or nil (no streaming) when there is none.
func previewTo(stream Stream) func(string) {
	if stream == nil {
		return nil
	}
	r := &answerReader{out: stream.Answer}
	return r.write
}

func stopped(resp aiprovider.Response) error {
	switch {
	case resp.Refused || resp.StopReason == aiprovider.StopRefusal:
		return refuse(http.StatusUnprocessableEntity, "The model declined this request.")
	case resp.StopReason == aiprovider.StopMaxTokens:
		return refuse(http.StatusUnprocessableEntity, "The answer was cut off before it was complete. Ask about a smaller part of the document.")
	}
	return nil
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func aiLabel(resp aiprovider.Response) string {
	model := firstNonEmpty(resp.Model, resp.Backend, resp.Provider)
	return "AI · " + model
}

// Propose runs one request from the Studio's inline actions.
func (e *Engine) Propose(ctx context.Context, documentID int64, req Request, actor string) (Result, error) {
	return e.ProposeStream(ctx, documentID, req, actor, nil)
}

// ProposeStream is Propose with the answer's text previewed to stream as the
// model writes it.
func (e *Engine) ProposeStream(ctx context.Context, documentID int64, req Request, actor string, stream Stream) (Result, error) {
	if _, ok := actionInstructions[req.Action]; !ok || req.Action == "ask" && strings.TrimSpace(req.Instruction) == "" {
		return Result{}, refuse(http.StatusBadRequest, "unknown action %q", req.Action)
	}
	if len(req.Instruction) > 4000 {
		return Result{}, refuse(http.StatusBadRequest, "the instruction is over 4000 characters")
	}
	release, err := e.acquire(actor, documentID)
	if err != nil {
		return Result{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	p, err := e.prepare(ctx, documentID, req.Scope, req.Selection, false)
	if err != nil {
		return Result{}, err
	}
	if req.Action == "from_library" {
		if err := e.withSource(ctx, &p, req.LibraryDocumentID); err != nil {
			return Result{}, err
		}
		req.Instruction = strings.TrimSpace("library document " + strconv.FormatInt(req.LibraryDocumentID, 10) + ": " + p.pc.sourceOf + ". " + req.Instruction)
	}
	text := prompt(p.pc, req.Action, req.Instruction)
	answer, resp, err := e.ask(ctx, p, aiprovider.Request{Prompt: text}, stream)
	if err != nil {
		e.saveFailed(documentID, req, actor, p, resp, text, err)
		return Result{}, err
	}
	return e.finish(documentID, req, actor, p, resp, text, answer)
}

// finish validates an answer against the live document and records it.
func (e *Engine) finish(documentID int64, req Request, actor string, p prepared, resp aiprovider.Response, text string, answer Answer) (Result, error) {
	res := Result{Answer: clip2(answer.AnswerMarkdown, maxAnswerMarkdown), Destination: p.dest, AILabel: aiLabel(resp),
		Provider: resp.Provider, Model: resp.Model, Edits: []CheckedEdit{}, Mappings: []CheckedMapping{}, NewFacts: []NewFact{},
		SessionID: resp.SessionID}
	if answer.Proposal != nil {
		// The document may have moved while the model was thinking: anchor on
		// the text as it is now.
		if sections, err := e.cfg.Studio.LiveSections(documentID); err == nil {
			for _, b := range documentBlocks(sections) {
				p.src.blocks[b.ID] = b
			}
		}
		res.HasProposal = true
		res.Summary = answer.Proposal.Summary
		res.Edits, res.Mappings, res.NewFacts = check(answer.Proposal, p.src)
		for _, ed := range res.Edits {
			if ed.Status != EditOK {
				res.Rejected++
			}
		}
	}
	id, err := e.save(documentID, req, actor, p, resp, text, "proposed", res)
	if err != nil {
		return Result{}, err
	}
	res.ProposalID = id
	return res, nil
}

func clip2(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// withSource reads a library document into the request's context. The text was
// extracted by the Wintermute server that holds the library; nothing is
// uploaded or parsed here.
func (e *Engine) withSource(ctx context.Context, p *prepared, libraryID int64) error {
	if libraryID <= 0 {
		return refuse(http.StatusBadRequest, "Choose a library document to start from.")
	}
	lib, err := e.cfg.Router.Library()
	if err != nil {
		return refuse(http.StatusServiceUnavailable, "The document library is not available: %v", err)
	}
	content, err := lib.ReadLibraryDocument(ctx, libraryID)
	if err != nil {
		return refuse(http.StatusBadGateway, "The library document could not be read: %v", err)
	}
	if !content.Document.Ready() {
		return refuse(http.StatusConflict, "%q is still being read by the library. Try again when it is ready.", content.Document.Title)
	}
	p.pc.sourceOf = firstNonEmpty(content.Document.Title, content.Document.Filename, "the source")
	p.src.source = map[int]string{}
	used := 0
	chunks := content.Chunks
	if len(chunks) == 0 && strings.TrimSpace(content.Text) != "" {
		chunks = []aiprovider.LibraryChunk{{Ordinal: 1, Body: content.Text}}
	}
	for i, c := range chunks {
		n := c.Ordinal
		if n <= 0 {
			n = i + 1
		}
		body := strings.TrimSpace(c.Body)
		if used+len(body) > maxSourceChars {
			p.pc.sourceCut = len(chunks) - i
			break
		}
		used += len(body)
		p.pc.source = append(p.pc.source, sourcePassage{ordinal: n, heading: c.Heading, body: body})
		p.src.source[n] = c.Heading + "\n" + body
	}
	if len(p.pc.source) == 0 {
		return refuse(http.StatusUnprocessableEntity, "%q has no text the library could extract.", p.pc.sourceOf)
	}
	return nil
}

// LibraryEntry is one library document, as the new-document dialog lists it.
type LibraryEntry struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Ready bool   `json:"ready"`
}

// LibraryDocuments lists the library documents a new document can start from.
func (e *Engine) LibraryDocuments(ctx context.Context) ([]LibraryEntry, error) {
	if e.cfg.Router == nil {
		return nil, refuse(http.StatusServiceUnavailable, "No AI provider is configured.")
	}
	lib, err := e.cfg.Router.Library()
	if err != nil {
		return nil, refuse(http.StatusServiceUnavailable, "The document library is not available: %v", err)
	}
	docs, err := lib.LibraryDocuments(ctx)
	if err != nil {
		return nil, refuse(http.StatusBadGateway, "The library could not be listed: %v", err)
	}
	out := make([]LibraryEntry, 0, len(docs))
	for _, d := range docs {
		out = append(out, LibraryEntry{ID: d.ID, Title: firstNonEmpty(d.Title, d.Filename), Ready: d.Ready()})
	}
	return out, nil
}
