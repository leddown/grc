package policyai

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"grc/internal/aiprovider"
	"grc/internal/policydocs"
	"grc/internal/policystudio"
)

func (e *Engine) stamp() string { return e.now().UTC().Format(time.RFC3339Nano) }

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(raw)
}

// save records a proposal and its edits in one transaction.
func (e *Engine) save(documentID int64, req Request, actor string, p prepared, resp aiprovider.Response, text, status string, res Result) (int64, error) {
	tx, err := e.cfg.Conn.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := tx.Insert(`INSERT INTO policy_ai_proposals (document_id, requested_by, action, scope, instruction, provider, model,
		served_by, agent, prompt_sha256, context_fingerprint, status, summary, answer_markdown, mappings_json, new_facts_json,
		input_tokens, output_tokens, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		documentID, actor, req.Action, firstNonEmpty(req.Scope, ScopeDocument), req.Instruction, resp.Provider, resp.Model,
		resp.Backend, p.agent, hash(standingRules+"\n"+text), hash(p.pc.fingerprint()), status, res.Summary, res.Answer,
		mustJSON(res.Mappings), mustJSON(res.NewFacts), resp.Usage.InputTokens, resp.Usage.OutputTokens, e.stamp())
	if err != nil {
		return 0, err
	}
	for _, ed := range res.Edits {
		validation := EditOK
		if ed.Status != EditOK {
			validation = "rejected: " + ed.Reason
		}
		if _, err := tx.Exec(`INSERT INTO policy_ai_edits (proposal_id, suid, op, block_id, section_uid, quote, fragment_json,
			rationale, citations_json, validation, placement) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, ed.SUID, ed.Op, ed.BlockID, ed.SectionUID, ed.Quote, mustJSON(ed.Fragment), ed.Rationale,
			mustJSON(ed.Citations), validation, PlacementPending); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// saveFailed records a request that produced nothing usable, so what was
// spent and why it failed are on file.
func (e *Engine) saveFailed(documentID int64, req Request, actor string, p prepared, resp aiprovider.Response, text string, cause error) {
	_, _ = e.save(documentID, req, actor, p, resp, text, "failed: "+clip(cause.Error(), 300), Result{})
}

// fingerprint identifies what a context showed, without the per-request
// nonce, so an unchanged document is recognised as unchanged.
func (pc promptContext) fingerprint() string {
	var b strings.Builder
	b.WriteString(pc.doc.Title + "\n" + pc.doc.Status + "\n" + pc.scopeTag + "\n")
	for _, blk := range pc.blocks {
		if pc.scope[blk.ID] {
			b.WriteString(blk.ID + " " + blk.Text + "\n")
		}
	}
	for _, p := range pc.pending {
		b.WriteString(p + "\n")
	}
	for _, f := range pc.facts {
		b.WriteString(f.Key + "=" + f.Value + "\n")
	}
	return b.String()
}

// Placement statuses the editor reports for each edit.
const (
	PlacementPending   = "pending"
	PlacementPreviewed = "previewed"
	PlacementPlaced    = "placed"
	PlacementStale     = "stale"
	PlacementConflict  = "conflict"
	PlacementDiscarded = "discarded"
)

var placements = map[string]bool{PlacementPreviewed: true, PlacementPlaced: true, PlacementStale: true, PlacementConflict: true, PlacementDiscarded: true}

// Placement is what the editor did with one edit. For a comment edit that was
// placed, the anchors are where its rationale thread goes.
type Placement struct {
	SUID        string `json:"suid"`
	Status      string `json:"status"`
	SectionUID  string `json:"section_uid"`
	AnchorStart string `json:"anchor_start"`
	AnchorEnd   string `json:"anchor_end"`
	Quote       string `json:"quote"`
}

// RecordPlacements records where the editor placed a proposal's edits, and
// starts the rationale thread of each comment edit it placed. A guest records
// placements only for a proposal they asked for, and their rationale threads
// are shared, since an internal thread is one they could never see.
func (e *Engine) RecordPlacements(documentID, proposalID int64, list []Placement, actor string, guest bool) error {
	var model, requestedBy string
	var docID int64
	err := e.cfg.Conn.QueryRow(`SELECT document_id, model, requested_by FROM policy_ai_proposals WHERE id = ?`, proposalID).Scan(&docID, &model, &requestedBy)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (docID != documentID || guest && requestedBy != actor)) {
		return policydocs.ErrNotFound
	}
	if err != nil {
		return err
	}
	visibility := policystudio.VisibilityInternal
	if guest {
		visibility = policystudio.VisibilityShared
	}
	for _, pl := range list {
		if !placements[pl.Status] {
			return refuse(http.StatusBadRequest, "unknown placement status %q", pl.Status)
		}
		var op, rationale, validation, current string
		err := e.cfg.Conn.QueryRow(`SELECT op, rationale, validation, placement FROM policy_ai_edits WHERE proposal_id = ? AND suid = ?`,
			proposalID, pl.SUID).Scan(&op, &rationale, &validation, &current)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(http.StatusBadRequest, "edit %s is not part of this proposal", pl.SUID)
		}
		if err != nil {
			return err
		}
		if validation != EditOK {
			return refuse(http.StatusBadRequest, "edit %s was refused by validation and cannot be placed", pl.SUID)
		}
		if current == PlacementPlaced && pl.Status != PlacementPlaced {
			continue
		}
		if _, err := e.cfg.Conn.Exec(`UPDATE policy_ai_edits SET placement = ? WHERE proposal_id = ? AND suid = ?`, pl.Status, proposalID, pl.SUID); err != nil {
			return err
		}
		if op == OpComment && pl.Status == PlacementPlaced && current != PlacementPlaced {
			if _, err := e.cfg.Studio.CreateAIThread(documentID, policystudio.NewThread{
				SectionUID: pl.SectionUID, AnchorStart: pl.AnchorStart, AnchorEnd: pl.AnchorEnd, Quote: pl.Quote,
				Visibility: visibility, Body: rationale,
			}, pl.SUID, "AI · "+firstNonEmpty(model, "model"), actor); err != nil {
				return err
			}
		}
	}
	return nil
}

// PlacedEdit is an AI edit that is in the document as a suggestion, with what
// the Suggestions panel shows beside it.
type PlacedEdit struct {
	SUID        string            `json:"suid"`
	ProposalID  int64             `json:"proposal_id"`
	Op          string            `json:"op"`
	Rationale   string            `json:"rationale"`
	Citations   []CheckedCitation `json:"citations"`
	Summary     string            `json:"summary"`
	Model       string            `json:"model"`
	Provider    string            `json:"provider"`
	RequestedBy string            `json:"requested_by"`
	CreatedAt   string            `json:"created_at"`
	Decision    string            `json:"decision,omitempty"`
	DecidedBy   string            `json:"decided_by,omitempty"`
	DecidedAt   string            `json:"decided_at,omitempty"`
}

// PlacedEdits lists a document's AI edits that were placed.
func (e *Engine) PlacedEdits(documentID int64) ([]PlacedEdit, error) {
	rows, err := e.cfg.Conn.Query(`SELECT ed.suid, p.id, ed.op, ed.rationale, ed.citations_json, p.summary, p.model, p.provider,
		p.requested_by, p.created_at, ed.decision, ed.decided_by, ed.decided_at
		FROM policy_ai_edits ed JOIN policy_ai_proposals p ON p.id = ed.proposal_id
		WHERE p.document_id = ? AND ed.placement = ? ORDER BY ed.id`, documentID, PlacementPlaced)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlacedEdit{}
	for rows.Next() {
		var pe PlacedEdit
		var citations string
		if err := rows.Scan(&pe.SUID, &pe.ProposalID, &pe.Op, &pe.Rationale, &citations, &pe.Summary, &pe.Model, &pe.Provider,
			&pe.RequestedBy, &pe.CreatedAt, &pe.Decision, &pe.DecidedBy, &pe.DecidedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(citations), &pe.Citations)
		if pe.Citations == nil {
			pe.Citations = []CheckedCitation{}
		}
		out = append(out, pe)
	}
	return out, rows.Err()
}
