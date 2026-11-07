package assuranceplan

import (
	"database/sql"
	"fmt"

	"grc/internal/db"
)

const aiProposalColumns = `id, plan_id, kind, document, revision, input_json, model, prompt_hash, payload_json,
	status, applied_json, decision_note, created_at, created_by, decided_by, decided_at`

func (r *SQLiteRepository) CreateAIProposal(pr AIProposal, history []HistoryEntry) (int64, error) {
	var id int64
	err := r.inTx(pr.PlanID, history, func(tx *db.Tx) error {
		var err error
		id, err = tx.Insert(`INSERT INTO assurance_plan_ai_proposals (plan_id, kind, document, revision, input_json, model,
			prompt_hash, payload_json, status, applied_json, decision_note, created_at, created_by, decided_by, decided_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			pr.PlanID, pr.Kind, pr.Document, pr.Revision, string(pr.Input), pr.Model, pr.PromptHash, string(pr.Payload),
			pr.Status, string(pr.Applied), pr.DecisionNote, pr.CreatedAt, pr.CreatedBy, pr.DecidedBy, pr.DecidedAt)
		if err != nil {
			return fmt.Errorf("record AI proposal: %w", err)
		}
		return nil
	})
	return id, err
}

// DecideAIProposal records a decision. Only a proposal still open is
// decided, so two people deciding at once cannot both apply it.
func (r *SQLiteRepository) DecideAIProposal(pr AIProposal, history []HistoryEntry) error {
	return r.inTx(pr.PlanID, history, func(tx *db.Tx) error {
		res, err := tx.Exec(`UPDATE assurance_plan_ai_proposals SET status = ?, applied_json = ?, decision_note = ?,
			decided_by = ?, decided_at = ? WHERE id = ? AND plan_id = ? AND status = ?`,
			pr.Status, string(pr.Applied), pr.DecisionNote, pr.DecidedBy, pr.DecidedAt, pr.ID, pr.PlanID, AIProposed)
		if err != nil {
			return fmt.Errorf("decide AI proposal: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return conflict("proposal %d was decided by someone else meanwhile", pr.ID)
		}
		return nil
	})
}

func (r *SQLiteRepository) AIProposals(planID int64) ([]AIProposal, error) {
	rows, err := r.db.Query(`SELECT `+aiProposalColumns+` FROM assurance_plan_ai_proposals WHERE plan_id = ? ORDER BY id DESC`, planID)
	if err != nil {
		return nil, fmt.Errorf("list AI proposals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []AIProposal{}
	for rows.Next() {
		pr, err := scanAIProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) AIProposal(planID, id int64) (AIProposal, error) {
	return scanAIProposal(r.db.QueryRow(`SELECT `+aiProposalColumns+` FROM assurance_plan_ai_proposals WHERE plan_id = ? AND id = ?`, planID, id))
}

func scanAIProposal(row interface{ Scan(...any) error }) (AIProposal, error) {
	var pr AIProposal
	var input, payload, applied string
	if err := row.Scan(&pr.ID, &pr.PlanID, &pr.Kind, &pr.Document, &pr.Revision, &input, &pr.Model, &pr.PromptHash, &payload,
		&pr.Status, &applied, &pr.DecisionNote, &pr.CreatedAt, &pr.CreatedBy, &pr.DecidedBy, &pr.DecidedAt); err != nil {
		if err == sql.ErrNoRows {
			return AIProposal{}, err
		}
		return AIProposal{}, fmt.Errorf("read AI proposal: %w", err)
	}
	pr.Input, pr.Payload = rawOr(input, "{}"), rawOr(payload, "{}")
	if applied != "" {
		pr.Applied = []byte(applied)
	}
	return pr, nil
}

func rawOr(s, empty string) []byte {
	if s == "" {
		s = empty
	}
	return []byte(s)
}
