package policyai

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"grc/internal/aiprovider"
)

// The AI dock asks from any page; on a Studio page its questions come here,
// with what the page says is in view. The page context is a hint for scope
// only: which document is asked about comes from the path, and the caller
// has already checked the person may use AI on it.

var studioPath = regexp.MustCompile(`^/policies/([0-9]+)/studio/?$`)

// StudioDocument returns the document a dock question was asked about, from
// the page path; ok is false for any other page.
func StudioDocument(path string) (int64, bool) {
	m := studioPath.FindStringSubmatch(strings.TrimSpace(path))
	if m == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	return id, err == nil && id > 0
}

// PageContext is what the Studio registers with the dock
// (window.GRCAskAIContext).
type PageContext struct {
	Kind       string    `json:"kind"`
	DocumentID int64     `json:"document_id"`
	SectionUID string    `json:"section_uid"`
	Selection  Selection `json:"selection"`
	Mode       string    `json:"mode"`
}

// maxPageContextBytes bounds what the dock may send.
const maxPageContextBytes = 8 << 10

// ParsePageContext reads the dock's page context, ignoring it when it is too
// large or malformed rather than failing the question.
func ParsePageContext(raw json.RawMessage) PageContext {
	var pc PageContext
	if len(raw) == 0 || len(raw) > maxPageContextBytes {
		return pc
	}
	_ = json.Unmarshal(raw, &pc)
	if len(pc.Selection.BlockIDs) > 200 {
		pc.Selection.BlockIDs = pc.Selection.BlockIDs[:200]
	}
	return pc
}

// Dock answers a question asked in the AI dock on a Studio page: an answer,
// and a validated proposal when the question asked for changes.
func (e *Engine) Dock(ctx context.Context, documentID int64, question, sessionID string, history []aiprovider.Message, page PageContext, actor string) (Result, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return Result{}, refuse(http.StatusBadRequest, "question is required")
	}
	release, err := e.acquire(actor, documentID)
	if err != nil {
		return Result{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// A dock question is about the document ("make §3 testable" is asked from
	// wherever the cursor happens to be), so the whole document is its scope;
	// what the page says is selected or in view is a hint for the model.
	sel := page.Selection
	if page.DocumentID != documentID {
		sel = Selection{}
	}
	p, err := e.prepare(ctx, documentID, ScopeDocument, sel, true)
	if err != nil {
		return Result{}, err
	}
	if page.DocumentID == documentID && page.SectionUID != "" {
		p.pc.inView = page.SectionUID
	}

	// A conversation the provider keeps (Wintermute) is given the document
	// when it starts and again whenever the document has changed -- or with
	// every question, when Settings says so. Claude keeps no conversation, so
	// every question carries it.
	fingerprint := hash(p.pc.fingerprint())
	lead := p.pc.render()
	_, wintermute := p.provider.(*aiprovider.Wintermute)
	sendAlways := e.cfg.SendDocument != nil && e.cfg.SendDocument()
	if wintermute && !sendAlways && sessionID != "" && e.sentOn(sessionID) == fingerprint {
		lead = "The document has not changed since the context you were last given.\n"
	}
	text := lead + "\n## The request\n" + actionInstructions["ask"] + "\n\nThe person asked: " + question + "\n"
	req := aiprovider.Request{Prompt: text, SessionID: sessionID}
	if sessionID == "" {
		req.History = history
	}
	answer, resp, err := e.ask(ctx, p, req)
	if err != nil {
		e.saveFailed(documentID, Request{Action: "ask", Scope: ScopeDocument, Instruction: question}, actor, p, resp, text, err)
		return Result{}, err
	}
	e.remember(resp.SessionID, fingerprint)
	return e.finish(documentID, Request{Action: "ask", Scope: ScopeDocument, Selection: sel, Instruction: question}, actor, p, resp, text, answer)
}

func (e *Engine) sentOn(sessionID string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sent[sessionID]
}

func (e *Engine) remember(sessionID, fingerprint string) {
	if sessionID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.sent) >= maxDockSessions {
		e.sent = map[string]string{}
	}
	e.sent[sessionID] = fingerprint
}
