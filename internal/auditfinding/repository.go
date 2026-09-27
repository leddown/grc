package auditfinding

import (
	"database/sql"
	"fmt"
	"strings"

	"grc/internal/db"
)

type Repository interface {
	List(filter Filter) ([]Finding, error)
	Get(id int64) (Finding, error)
	Create(f Finding, history []HistoryEntry) (int64, error)
	Update(id int64, f Finding, history []HistoryEntry) error
	Delete(id int64) error
	ReferencesWithPrefix(prefix string) ([]string, error)
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

const findingColumns = `reference, title, source, engagement, auditor, finding_type,
	severity, deficiency_type, criteria, condition_text, cause, effect,
	recommendation, root_cause_category, business_unit, process_area, owner,
	management_response, management_response_text, identified_date, report_date,
	original_due_date, due_date, extension_count, extension_reason, status,
	repeat_finding, prior_reference, linked_risks, linked_controls, framework_refs,
	validated_by, validation_date, validation_notes, closure_evidence, closed_date,
	risk_accepted_by, risk_acceptance_rationale, risk_acceptance_expiry, notes,
	created_at, created_by, updated_at, updated_by`

const findingColumnCount = 44

func findingArgs(f Finding) []any {
	return []any{
		f.Reference, f.Title, f.Source, f.Engagement, f.Auditor, f.FindingType,
		f.Severity, f.DeficiencyType, f.Criteria, f.Condition, f.Cause, f.Effect,
		f.Recommendation, f.RootCauseCategory, f.BusinessUnit, f.ProcessArea, f.Owner,
		f.ManagementResponse, f.ManagementResponseText, f.IdentifiedDate, f.ReportDate,
		f.OriginalDueDate, f.DueDate, f.ExtensionCount, f.ExtensionReason, f.Status,
		boolToInt(f.RepeatFinding), f.PriorReference, f.LinkedRisks, f.LinkedControls, f.FrameworkRefs,
		f.ValidatedBy, f.ValidationDate, f.ValidationNotes, f.ClosureEvidence, f.ClosedDate,
		f.RiskAcceptedBy, f.RiskAcceptanceRationale, f.RiskAcceptanceExpiry, f.Notes,
		f.CreatedAt, f.CreatedBy, f.UpdatedAt, f.UpdatedBy,
	}
}

func scanFinding(scanner interface{ Scan(dest ...any) error }) (Finding, error) {
	var f Finding
	var repeat int
	err := scanner.Scan(
		&f.ID,
		&f.Reference, &f.Title, &f.Source, &f.Engagement, &f.Auditor, &f.FindingType,
		&f.Severity, &f.DeficiencyType, &f.Criteria, &f.Condition, &f.Cause, &f.Effect,
		&f.Recommendation, &f.RootCauseCategory, &f.BusinessUnit, &f.ProcessArea, &f.Owner,
		&f.ManagementResponse, &f.ManagementResponseText, &f.IdentifiedDate, &f.ReportDate,
		&f.OriginalDueDate, &f.DueDate, &f.ExtensionCount, &f.ExtensionReason, &f.Status,
		&repeat, &f.PriorReference, &f.LinkedRisks, &f.LinkedControls, &f.FrameworkRefs,
		&f.ValidatedBy, &f.ValidationDate, &f.ValidationNotes, &f.ClosureEvidence, &f.ClosedDate,
		&f.RiskAcceptedBy, &f.RiskAcceptanceRationale, &f.RiskAcceptanceExpiry, &f.Notes,
		&f.CreatedAt, &f.CreatedBy, &f.UpdatedAt, &f.UpdatedBy,
	)
	f.RepeatFinding = repeat == 1
	return f, err
}

func (r *SQLiteRepository) List(filter Filter) ([]Finding, error) {
	query := `SELECT id, ` + findingColumns + ` FROM audit_findings WHERE 1=1`
	args := make([]any, 0, 8)
	if filter.Search != "" {
		query += ` AND (
			UPPER(reference) LIKE ? OR UPPER(title) LIKE ? OR UPPER(owner) LIKE ? OR
			UPPER(engagement) LIKE ? OR UPPER(business_unit) LIKE ? OR UPPER(linked_controls) LIKE ? OR
			UPPER(linked_risks) LIKE ?
		)`
		like := "%" + strings.ToUpper(filter.Search) + "%"
		args = append(args, like, like, like, like, like, like, like)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.Severity != "" {
		query += ` AND severity = ?`
		args = append(args, filter.Severity)
	}
	if filter.Source != "" {
		query += ` AND source = ?`
		args = append(args, filter.Source)
	}
	query += ` ORDER BY id DESC`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit findings: %w", err)
	}
	defer rows.Close()

	out := make([]Finding, 0)
	index := map[int64]int{}
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan audit finding: %w", err)
		}
		f.Actions = []Action{}
		index[f.ID] = len(out)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	actions, err := r.actions(`SELECT id, finding_id, ordinal, reference, description, action_type, owner,
		due_date, status, completed_date, evidence FROM audit_finding_actions ORDER BY finding_id, ordinal, id`)
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		if i, ok := index[a.FindingID]; ok {
			out[i].Actions = append(out[i].Actions, a)
		}
	}
	return out, nil
}

func (r *SQLiteRepository) Get(id int64) (Finding, error) {
	f, err := scanFinding(r.db.QueryRow(`SELECT id, `+findingColumns+` FROM audit_findings WHERE id = ?`, id))
	if err != nil {
		return Finding{}, err
	}
	f.Actions, err = r.actions(`SELECT id, finding_id, ordinal, reference, description, action_type, owner,
		due_date, status, completed_date, evidence FROM audit_finding_actions WHERE finding_id = ? ORDER BY ordinal, id`, id)
	if err != nil {
		return Finding{}, err
	}

	rows, err := r.db.Query(`SELECT id, finding_id, at, actor, event, from_value, to_value, note
		FROM audit_finding_history WHERE finding_id = ? ORDER BY id DESC`, id)
	if err != nil {
		return Finding{}, fmt.Errorf("load audit finding history: %w", err)
	}
	defer rows.Close()
	f.History = []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.ID, &h.FindingID, &h.At, &h.Actor, &h.Event, &h.FromValue, &h.ToValue, &h.Note); err != nil {
			return Finding{}, fmt.Errorf("scan audit finding history: %w", err)
		}
		f.History = append(f.History, h)
	}
	return f, rows.Err()
}

func (r *SQLiteRepository) actions(query string, args ...any) ([]Action, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("load audit finding actions: %w", err)
	}
	defer rows.Close()
	out := []Action{}
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.FindingID, &a.Ordinal, &a.Reference, &a.Description, &a.ActionType,
			&a.Owner, &a.DueDate, &a.Status, &a.CompletedDate, &a.Evidence); err != nil {
			return nil, fmt.Errorf("scan audit finding action: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) Create(f Finding, history []HistoryEntry) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	id, err := tx.Insert(`INSERT INTO audit_findings (`+findingColumns+`) VALUES (`+placeholders(findingColumnCount)+`)`, findingArgs(f)...)
	if err != nil {
		return 0, fmt.Errorf("insert audit finding: %w", err)
	}
	if err := writeChildren(tx, id, f.Actions, history); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Update replaces the action plan wholesale: the page edits it as one list, and
// the history entries the service writes alongside are what keep the trail.
func (r *SQLiteRepository) Update(id int64, f Finding, history []HistoryEntry) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	sets := strings.Split(findingColumns, ",")
	for i := range sets {
		sets[i] = strings.TrimSpace(sets[i]) + " = ?"
	}
	args := append(findingArgs(f), id)
	// #nosec G202 -- the column list is a package constant, not input.
	result, err := tx.Exec(`UPDATE audit_findings SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update audit finding: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM audit_finding_actions WHERE finding_id = ?`, id); err != nil {
		return fmt.Errorf("clear audit finding actions: %w", err)
	}
	if err := writeChildren(tx, id, f.Actions, history); err != nil {
		return err
	}
	return tx.Commit()
}

func writeChildren(tx *db.Tx, findingID int64, actions []Action, history []HistoryEntry) error {
	for i, a := range actions {
		if _, err := tx.Exec(`INSERT INTO audit_finding_actions (finding_id, ordinal, reference, description,
			action_type, owner, due_date, status, completed_date, evidence) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			findingID, i, a.Reference, a.Description, a.ActionType, a.Owner, a.DueDate, a.Status, a.CompletedDate, a.Evidence,
		); err != nil {
			return fmt.Errorf("insert audit finding action: %w", err)
		}
	}
	for _, h := range history {
		if _, err := tx.Exec(`INSERT INTO audit_finding_history (finding_id, at, actor, event, from_value, to_value, note)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, findingID, h.At, h.Actor, h.Event, h.FromValue, h.ToValue, h.Note,
		); err != nil {
			return fmt.Errorf("insert audit finding history: %w", err)
		}
	}
	return nil
}

func (r *SQLiteRepository) Delete(id int64) error {
	result, err := r.db.Exec(`DELETE FROM audit_findings WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SQLiteRepository) ReferencesWithPrefix(prefix string) ([]string, error) {
	rows, err := r.db.Query(`SELECT reference FROM audit_findings WHERE reference LIKE ?`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
