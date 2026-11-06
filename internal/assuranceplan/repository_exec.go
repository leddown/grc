package assuranceplan

import (
	"database/sql"
	"fmt"
	"strings"

	"grc/internal/db"
)

const attributeColumns = `attr_ref, domain, requirement, test_procedure, method, evidence_requested, pass_criteria,
	applies_to, regulatory_refs, control_ids, nfr_key, ai_generated, model, prompt_hash`

func attributeArgs(a Attribute) []any {
	return []any{a.AttrRef, a.Domain, a.Requirement, a.Procedure, a.Method, a.EvidenceRequested, a.PassCriteria,
		strings.Join(a.AppliesTo, ", "), a.RegulatoryRefs, strings.Join(a.ControlIDs, ", "), a.NFRKey,
		boolToInt(a.AIGenerated), a.Model, a.PromptHash}
}

func (r *SQLiteRepository) attributes(planID int64) ([]Attribute, error) {
	rows, err := r.db.Query(`SELECT id, ordinal, `+attributeColumns+`
		FROM assurance_plan_attributes WHERE plan_id = ? ORDER BY ordinal, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan attributes: %w", err)
	}
	defer rows.Close()
	out := []Attribute{}
	for rows.Next() {
		var a Attribute
		var applies, controls string
		var ai int
		if err := rows.Scan(&a.ID, &a.Ordinal, &a.AttrRef, &a.Domain, &a.Requirement, &a.Procedure, &a.Method,
			&a.EvidenceRequested, &a.PassCriteria, &applies, &a.RegulatoryRefs, &controls, &a.NFRKey,
			&ai, &a.Model, &a.PromptHash); err != nil {
			return nil, fmt.Errorf("scan plan attribute: %w", err)
		}
		a.AppliesTo, a.ControlIDs, a.AIGenerated = splitList(applies), splitList(controls), ai == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

func insertAttribute(tx *db.Tx, planID int64, ordinal int, a Attribute) error {
	if _, err := tx.Exec(`INSERT INTO assurance_plan_attributes (plan_id, ordinal, `+attributeColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append([]any{planID, ordinal}, attributeArgs(a)...)...); err != nil {
		return fmt.Errorf("insert attribute %s: %w", a.AttrRef, err)
	}
	return nil
}

func writeAttributes(tx *db.Tx, planID int64, attrs []Attribute) error {
	for i, a := range attrs {
		if err := insertAttribute(tx, planID, i, a); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceAttributes saves the test script as a whole list but keeps each
// attribute's row by reference, because results point at it: an attribute
// whose reference stays is updated in place, a new one is inserted and one
// left out is deleted (the service has refused that if it has results).
func (r *SQLiteRepository) ReplaceAttributes(planID int64, attrs []Attribute, history []HistoryEntry) error {
	existing, err := r.attributes(planID)
	if err != nil {
		return err
	}
	byRef := map[string]int64{}
	for _, a := range existing {
		byRef[a.AttrRef] = a.ID
	}
	return r.inTx(planID, history, func(tx *db.Tx) error {
		keep := map[int64]bool{}
		for _, a := range attrs {
			if id, ok := byRef[a.AttrRef]; ok {
				keep[id] = true
			}
		}
		for _, a := range existing {
			if !keep[a.ID] {
				if _, err := tx.Exec(`DELETE FROM assurance_plan_attributes WHERE id = ? AND plan_id = ?`, a.ID, planID); err != nil {
					return fmt.Errorf("remove attribute %s: %w", a.AttrRef, err)
				}
			}
		}
		for i, a := range attrs {
			id, ok := byRef[a.AttrRef]
			if !ok {
				if err := insertAttribute(tx, planID, i, a); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(`UPDATE assurance_plan_attributes SET ordinal = ?, attr_ref = ?, domain = ?, requirement = ?,
				test_procedure = ?, method = ?, evidence_requested = ?, pass_criteria = ?, applies_to = ?, regulatory_refs = ?,
				control_ids = ?, nfr_key = ?, ai_generated = ?, model = ?, prompt_hash = ? WHERE id = ? AND plan_id = ?`,
				append(append([]any{i}, attributeArgs(a)...), id, planID)...); err != nil {
				return fmt.Errorf("update attribute %s: %w", a.AttrRef, err)
			}
		}
		return nil
	})
}

const testColumns = `test_ref, tester, evidence_requested_on, evidence_due_on, evidence_received_on, fieldwork_completed_on,
	evidence_summary, qa_reviewer, qa_date, qa_note, notes`

func testArgs(t Test) []any {
	return []any{t.TestRef, t.Tester, t.EvidenceRequestedOn, t.EvidenceDueOn, t.EvidenceReceivedOn, t.FieldworkCompletedOn,
		t.EvidenceSummary, t.QAReviewer, t.QADate, t.QANote, t.Notes}
}

// Tests loads every test of the plan with its selection and its results.
func (r *SQLiteRepository) Tests(planID int64) ([]Test, error) {
	rows, err := r.db.Query(`SELECT t.id, t.selection_id, t.test_ref, t.tester, t.evidence_requested_on, t.evidence_due_on,
		t.evidence_received_on, t.fieldwork_completed_on, t.evidence_summary, t.qa_reviewer, t.qa_date, t.qa_note, t.notes,
		t.created_at, t.created_by, t.updated_at, t.updated_by,
		s.quarter, s.population_id, s.item_ref, COALESCE(x.name, ''), COALESCE(x.owner, ''), s.stratum_code, s.basis
		FROM assurance_plan_tests t
		JOIN assurance_plan_selections s ON s.id = t.selection_id
		LEFT JOIN assurance_plan_population x ON x.id = s.population_id
		WHERE t.plan_id = ? ORDER BY s.quarter, t.test_ref`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan tests: %w", err)
	}
	defer rows.Close()
	out := []Test{}
	index := map[int64]int{}
	for rows.Next() {
		var t Test
		if err := rows.Scan(&t.ID, &t.SelectionID, &t.TestRef, &t.Tester, &t.EvidenceRequestedOn, &t.EvidenceDueOn,
			&t.EvidenceReceivedOn, &t.FieldworkCompletedOn, &t.EvidenceSummary, &t.QAReviewer, &t.QADate, &t.QANote, &t.Notes,
			&t.CreatedAt, &t.CreatedBy, &t.UpdatedAt, &t.UpdatedBy,
			&t.Quarter, &t.ItemID, &t.ItemRef, &t.ItemName, &t.ItemOwner, &t.StratumCode, &t.Basis); err != nil {
			return nil, fmt.Errorf("scan plan test: %w", err)
		}
		t.Results = []Result{}
		index[t.ID] = len(out)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	results, err := r.results(planID)
	if err != nil {
		return nil, err
	}
	for _, res := range results {
		if i, ok := index[res.TestID]; ok {
			out[i].Results = append(out[i].Results, res)
		}
	}
	return out, nil
}

func (r *SQLiteRepository) results(planID int64) ([]Result, error) {
	rows, err := r.db.Query(`SELECT r.id, r.test_id, r.attribute_id, COALESCE(a.attr_ref, ''), r.result, r.note,
		r.finding_id, r.finding_ref, r.severity
		FROM assurance_plan_results r LEFT JOIN assurance_plan_attributes a ON a.id = r.attribute_id
		WHERE r.plan_id = ? ORDER BY r.test_id, a.ordinal, r.id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load test results: %w", err)
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var res Result
		if err := rows.Scan(&res.ID, &res.TestID, &res.AttributeID, &res.AttrRef, &res.Result, &res.Note,
			&res.FindingID, &res.FindingRef, &res.Severity); err != nil {
			return nil, fmt.Errorf("scan test result: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) CreateTests(planID int64, tests []Test, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		for _, t := range tests {
			if _, err := tx.Exec(`INSERT INTO assurance_plan_tests (plan_id, selection_id, `+testColumns+`,
				created_at, created_by, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				append(append([]any{planID, t.SelectionID}, testArgs(t)...), t.CreatedAt, t.CreatedBy, t.UpdatedAt, t.UpdatedBy)...); err != nil {
				return fmt.Errorf("insert test %s: %w", t.TestRef, err)
			}
		}
		return nil
	})
}

// SaveTest writes a test and its results. A result is matched on its
// attribute; its finding link is never written here, only by LinkFinding.
func (r *SQLiteRepository) SaveTest(planID int64, t Test, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		res, err := tx.Exec(`UPDATE assurance_plan_tests SET `+setList(testColumns)+`, updated_at = ?, updated_by = ?
			WHERE id = ? AND plan_id = ?`, append(testArgs(t), t.UpdatedAt, t.UpdatedBy, t.ID, planID)...)
		if err != nil {
			return fmt.Errorf("update test %s: %w", t.TestRef, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return sql.ErrNoRows
		}
		for _, rs := range t.Results {
			upd, err := tx.Exec(`UPDATE assurance_plan_results SET result = ?, note = ? WHERE test_id = ? AND attribute_id = ?`,
				rs.Result, rs.Note, t.ID, rs.AttributeID)
			if err != nil {
				return fmt.Errorf("update result %s: %w", rs.AttrRef, err)
			}
			if n, err := upd.RowsAffected(); err != nil {
				return err
			} else if n > 0 {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO assurance_plan_results (plan_id, test_id, attribute_id, result, note)
				VALUES (?, ?, ?, ?, ?)`, planID, t.ID, rs.AttributeID, rs.Result, rs.Note); err != nil {
				return fmt.Errorf("insert result %s: %w", rs.AttrRef, err)
			}
		}
		return nil
	})
}

func setList(columns string) string {
	parts := strings.Split(columns, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i]) + " = ?"
	}
	return strings.Join(parts, ", ")
}

func (r *SQLiteRepository) LinkFinding(planID, resultID, findingID int64, findingRef, severity string, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		res, err := tx.Exec(`UPDATE assurance_plan_results SET finding_id = ?, finding_ref = ?, severity = ?
			WHERE id = ? AND plan_id = ?`, findingID, findingRef, severity, resultID, planID)
		if err != nil {
			return fmt.Errorf("link finding: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func (r *SQLiteRepository) Thematics(planID int64) ([]Thematic, error) {
	rows, err := r.db.Query(`SELECT t.id, t.attribute_id, COALESCE(a.attr_ref, ''), t.stratum_code, t.risk_id, t.risk_ref,
		t.deviations, t.note, t.pushed_at, t.pushed_by
		FROM assurance_plan_thematic t LEFT JOIN assurance_plan_attributes a ON a.id = t.attribute_id
		WHERE t.plan_id = ? ORDER BY t.id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load thematic pushes: %w", err)
	}
	defer rows.Close()
	out := []Thematic{}
	for rows.Next() {
		var t Thematic
		if err := rows.Scan(&t.ID, &t.AttributeID, &t.AttrRef, &t.StratumCode, &t.RiskID, &t.RiskRef, &t.Deviations,
			&t.Note, &t.PushedAt, &t.PushedBy); err != nil {
			return nil, fmt.Errorf("scan thematic push: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) AddThematic(planID int64, t Thematic, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_thematic (plan_id, attribute_id, stratum_code, risk_id, risk_ref,
			deviations, note, pushed_at, pushed_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			planID, t.AttributeID, t.StratumCode, t.RiskID, t.RiskRef, t.Deviations, t.Note, t.PushedAt, t.PushedBy); err != nil {
			return fmt.Errorf("record thematic push: %w", err)
		}
		return nil
	})
}

// AddHistory writes history alone, for an outcome recorded in another module
// (a finding that failed to link, say) that still belongs on the plan's trail.
func (r *SQLiteRepository) AddHistory(planID int64, history []HistoryEntry) error {
	return r.inTx(planID, history, func(*db.Tx) error { return nil })
}
