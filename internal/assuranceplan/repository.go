package assuranceplan

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"grc/internal/db"
)

type Repository interface {
	List(filter Filter) ([]Plan, error)
	Get(id int64) (Plan, error)
	Create(p Plan, population []PopulationItem, history []HistoryEntry) (int64, error)
	SaveHeader(p Plan, history []HistoryEntry, version *Version) error
	ReplaceSteps(planID int64, steps []Step, history []HistoryEntry) error
	ReplaceQuarters(planID int64, quarters []Quarter, history []HistoryEntry) error
	ReplaceGovernance(planID int64, roles []string, g Governance, history []HistoryEntry) error
	UpdateStepStatus(planID, stepID int64, status string, history []HistoryEntry) error
	ReplaceStrata(planID int64, strata []Stratum, history []HistoryEntry) error
	Population(planID int64) ([]PopulationItem, error)
	ApplyPopulation(planID int64, change PopulationChange, history []HistoryEntry) error
	UpdatePopulationItem(planID int64, item PopulationItem, history []HistoryEntry) error
	Selections(planID int64) ([]Selection, error)
	SaveSelection(planID int64, quarter int, picks []Selection, activate *Plan, history []HistoryEntry) error
	ReplaceAttributes(planID int64, attrs []Attribute, history []HistoryEntry) error
	Tests(planID int64) ([]Test, error)
	CreateTests(planID int64, tests []Test, history []HistoryEntry) error
	SaveTest(planID int64, t Test, history []HistoryEntry) error
	LinkFinding(planID, resultID, findingID int64, findingRef, severity string, history []HistoryEntry) error
	Thematics(planID int64) ([]Thematic, error)
	AddThematic(planID int64, t Thematic, history []HistoryEntry) error
	AddHistory(planID int64, history []HistoryEntry) error
	Delete(id int64) error
	ReferencesWithPrefix(prefix string) ([]string, error)
	History(planID int64) ([]HistoryEntry, error)
	Versions(planID int64) ([]Version, error)
	Version(planID int64, number int) (Version, error)
	Clients() ([]Client, error)
	CreateAIProposal(pr AIProposal, history []HistoryEntry) (int64, error)
	DecideAIProposal(pr AIProposal, history []HistoryEntry) error
	AIProposals(planID int64) ([]AIProposal, error)
	AIProposal(planID, id int64) (AIProposal, error)
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

var planColumnList = []string{
	"reference", "title", "plan_type", "function_name", "client_profile_id",
	"period_start", "period_end", "mobilise_start", "closing_date",
	"objective", "decisions_requested", "regulatory_refs",
	"owner_role", "accountable_role", "approval_forum",
	"status", "revision", "revision_status", "revision_author", "change_reason",
	"author", "approved_by", "approved_at", "activated_at", "closed_at",
	"selection_seed", "non_working_dates", "raci_roles",
	"test_types", "coverage_basis", "population_source",
	"tester_fte", "days_per_fte_quarter", "utilisation_threshold", "rag_amber_points",
	"result_partial_min", "result_ineffective_min", "deviation_extend_at", "deviation_thematic_at",
	"extend_pct", "evidence_sla_days",
	"ai_generated", "model", "prompt_hash",
	"created_at", "created_by", "updated_at", "updated_by",
}

func prefixed(prefix string) string {
	cols := make([]string, len(planColumnList))
	for i, c := range planColumnList {
		cols[i] = prefix + c
	}
	return strings.Join(cols, ", ")
}

func planArgs(p Plan) ([]any, error) {
	roles := p.RACIRoles
	if roles == nil {
		roles = []string{}
	}
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return nil, fmt.Errorf("encode raci roles: %w", err)
	}
	types := p.TestTypes
	if types == nil {
		types = []string{}
	}
	typesJSON, err := json.Marshal(types)
	if err != nil {
		return nil, fmt.Errorf("encode test types: %w", err)
	}
	pr := p.Parameters
	return []any{
		p.Reference, p.Title, p.PlanType, p.FunctionName, p.ClientProfileID,
		p.PeriodStart, p.PeriodEnd, p.MobiliseStart, p.ClosingDate,
		p.Objective, p.DecisionsRequested, p.RegulatoryRefs,
		p.OwnerRole, p.AccountableRole, p.ApprovalForum,
		p.Status, p.Revision, p.RevisionStatus, p.RevisionAuthor, p.ChangeReason,
		p.Author, p.ApprovedBy, p.ApprovedAt, p.ActivatedAt, p.ClosedAt,
		p.SelectionSeed, p.NonWorkingDates, string(rolesJSON),
		string(typesJSON), firstNonEmpty(p.CoverageBasis, CoverageAttestation), p.PopulationSource,
		pr.TesterFTE, pr.DaysPerFTEQuarter, pr.UtilisationThreshold, pr.RAGAmberPoints,
		pr.ResultPartialMin, pr.ResultIneffectiveMin, pr.DeviationExtendAt, pr.DeviationThematicAt,
		pr.ExtendPct, pr.EvidenceSLADays,
		boolToInt(p.AIGenerated), p.Model, p.PromptHash,
		p.CreatedAt, p.CreatedBy, p.UpdatedAt, p.UpdatedBy,
	}, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanPlan(s scanner, extra ...any) (Plan, error) {
	var p Plan
	var roles, types string
	var ai int
	pr := &p.Parameters
	dest := []any{
		&p.ID,
		&p.Reference, &p.Title, &p.PlanType, &p.FunctionName, &p.ClientProfileID,
		&p.PeriodStart, &p.PeriodEnd, &p.MobiliseStart, &p.ClosingDate,
		&p.Objective, &p.DecisionsRequested, &p.RegulatoryRefs,
		&p.OwnerRole, &p.AccountableRole, &p.ApprovalForum,
		&p.Status, &p.Revision, &p.RevisionStatus, &p.RevisionAuthor, &p.ChangeReason,
		&p.Author, &p.ApprovedBy, &p.ApprovedAt, &p.ActivatedAt, &p.ClosedAt,
		&p.SelectionSeed, &p.NonWorkingDates, &roles,
		&types, &p.CoverageBasis, &p.PopulationSource,
		&pr.TesterFTE, &pr.DaysPerFTEQuarter, &pr.UtilisationThreshold, &pr.RAGAmberPoints,
		&pr.ResultPartialMin, &pr.ResultIneffectiveMin, &pr.DeviationExtendAt, &pr.DeviationThematicAt,
		&pr.ExtendPct, &pr.EvidenceSLADays,
		&ai, &p.Model, &p.PromptHash,
		&p.CreatedAt, &p.CreatedBy, &p.UpdatedAt, &p.UpdatedBy,
	}
	if err := s.Scan(append(dest, extra...)...); err != nil {
		return Plan{}, err
	}
	p.AIGenerated = ai == 1
	p.RACIRoles = []string{}
	if roles != "" {
		if err := json.Unmarshal([]byte(roles), &p.RACIRoles); err != nil {
			return Plan{}, fmt.Errorf("decode raci roles of %s: %w", p.Reference, err)
		}
	}
	p.TestTypes = []string{}
	if types != "" {
		if err := json.Unmarshal([]byte(types), &p.TestTypes); err != nil {
			return Plan{}, fmt.Errorf("decode test types of %s: %w", p.Reference, err)
		}
	}
	return p, nil
}

const planSelect = `SELECT p.id, %s, COALESCE(c.name, ''),
	(SELECT COUNT(*) FROM assurance_plan_versions v WHERE v.plan_id = p.id),
	(SELECT COALESCE(MAX(v.number), 0) FROM assurance_plan_versions v WHERE v.plan_id = p.id),
	(SELECT COUNT(*) FROM assurance_plan_population x WHERE x.plan_id = p.id),
	COALESCE((SELECT h.at FROM assurance_plan_history h WHERE h.plan_id = p.id AND h.event = '` + eventFrozen + `' ORDER BY h.id DESC LIMIT 1), ''),
	COALESCE((SELECT h.actor FROM assurance_plan_history h WHERE h.plan_id = p.id AND h.event = '` + eventFrozen + `' ORDER BY h.id DESC LIMIT 1), '')
	FROM assurance_plans p LEFT JOIN client_profiles c ON c.id = p.client_profile_id`

const eventFrozen = "population frozen"

func scanPlanRow(s scanner) (Plan, error) {
	var client string
	var count, latest, popSize int
	var frozenAt, frozenBy string
	p, err := scanPlan(s, &client, &count, &latest, &popSize, &frozenAt, &frozenBy)
	if err != nil {
		return Plan{}, err
	}
	p.ClientName = client
	p.PopulationSize = popSize
	p.FrozenAt, p.FrozenBy = frozenAt, frozenBy
	p.VersionCount = count
	if latest > 0 {
		p.LatestVersion = fmt.Sprintf("v%d", latest)
	}
	return p, nil
}

func (r *SQLiteRepository) List(f Filter) ([]Plan, error) {
	query := fmt.Sprintf(planSelect, prefixed("p.")) + ` WHERE 1=1`
	var args []any
	if f.PlanType != "" {
		query += ` AND p.plan_type = ?`
		args = append(args, f.PlanType)
	}
	if f.Status != "" {
		query += ` AND p.status = ?`
		args = append(args, f.Status)
	}
	if f.Year != "" {
		query += ` AND (p.period_start LIKE ? OR p.period_end LIKE ?)`
		args = append(args, f.Year+"%", f.Year+"%")
	}
	if f.ClientID > 0 {
		query += ` AND p.client_profile_id = ?`
		args = append(args, f.ClientID)
	}
	if f.Search != "" {
		like := "%" + strings.ToUpper(f.Search) + "%"
		query += ` AND (UPPER(p.reference) LIKE ? OR UPPER(p.title) LIKE ? OR UPPER(p.function_name) LIKE ?)`
		args = append(args, like, like, like)
	}
	query += ` ORDER BY p.period_start DESC, p.id DESC`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list assurance plans: %w", err)
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		p, err := scanPlanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan assurance plan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) Get(id int64) (Plan, error) {
	p, err := scanPlanRow(r.db.QueryRow(fmt.Sprintf(planSelect, prefixed("p."))+` WHERE p.id = ?`, id))
	if err != nil {
		return Plan{}, err
	}
	if p.Steps, err = r.steps(id); err != nil {
		return Plan{}, err
	}
	if p.Quarters, err = r.quarters(id); err != nil {
		return Plan{}, err
	}
	if p.Strata, err = r.strata(id); err != nil {
		return Plan{}, err
	}
	if p.Attributes, err = r.attributes(id); err != nil {
		return Plan{}, err
	}
	if p.RACI, err = r.raci(id); err != nil {
		return Plan{}, err
	}
	if p.Cadence, err = r.cadence(id); err != nil {
		return Plan{}, err
	}
	if p.Basis, err = r.basis(id); err != nil {
		return Plan{}, err
	}
	if p.Escalations, err = r.escalations(id); err != nil {
		return Plan{}, err
	}
	return p, nil
}

func (r *SQLiteRepository) steps(planID int64) ([]Step, error) {
	rows, err := r.db.Query(`SELECT id, ordinal, step_ref, phase, title, step_type, responsible_role,
		accountable_role, start_date, end_date, deliverable, refs, status, quarter, generated, notes
		FROM assurance_plan_steps WHERE plan_id = ? ORDER BY ordinal, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan steps: %w", err)
	}
	defer rows.Close()
	out := []Step{}
	for rows.Next() {
		var s Step
		var gen int
		if err := rows.Scan(&s.ID, &s.Ordinal, &s.StepRef, &s.Phase, &s.Title, &s.StepType, &s.ResponsibleRole,
			&s.AccountableRole, &s.StartDate, &s.EndDate, &s.Deliverable, &s.Refs, &s.Status, &s.Quarter, &gen, &s.Notes); err != nil {
			return nil, fmt.Errorf("scan plan step: %w", err)
		}
		s.Generated = gen == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) quarters(planID int64) ([]Quarter, error) {
	rows, err := r.db.Query(`SELECT id, quarter, lod1_window_start, lod1_deadline, selection_date,
		fieldwork_start, fieldwork_end, findings_agreed_by, report_date, committee_label,
		retest_days, overhead_days, selected_at, selected_by
		FROM assurance_plan_quarters WHERE plan_id = ? ORDER BY quarter`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan quarters: %w", err)
	}
	defer rows.Close()
	out := []Quarter{}
	for rows.Next() {
		var q Quarter
		if err := rows.Scan(&q.ID, &q.Quarter, &q.LOD1WindowStart, &q.LOD1Deadline, &q.SelectionDate,
			&q.FieldworkStart, &q.FieldworkEnd, &q.FindingsAgreedBy, &q.ReportDate, &q.CommitteeLabel,
			&q.RetestDays, &q.OverheadDays, &q.SelectedAt, &q.SelectedBy); err != nil {
			return nil, fmt.Errorf("scan plan quarter: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) raci(planID int64) ([]RACIRow, error) {
	rows, err := r.db.Query(`SELECT id, ordinal, activity, cells FROM assurance_plan_raci
		WHERE plan_id = ? ORDER BY ordinal, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan raci: %w", err)
	}
	defer rows.Close()
	out := []RACIRow{}
	for rows.Next() {
		var row RACIRow
		var cells string
		if err := rows.Scan(&row.ID, &row.Ordinal, &row.Activity, &cells); err != nil {
			return nil, fmt.Errorf("scan plan raci: %w", err)
		}
		row.Cells = map[string]string{}
		if cells != "" {
			if err := json.Unmarshal([]byte(cells), &row.Cells); err != nil {
				return nil, fmt.Errorf("decode raci cells: %w", err)
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) cadence(planID int64) ([]Cadence, error) {
	rows, err := r.db.Query(`SELECT id, ordinal, forum, frequency, content, owner FROM assurance_plan_cadence
		WHERE plan_id = ? ORDER BY ordinal, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan cadence: %w", err)
	}
	defer rows.Close()
	out := []Cadence{}
	for rows.Next() {
		var c Cadence
		if err := rows.Scan(&c.ID, &c.Ordinal, &c.Forum, &c.Frequency, &c.Content, &c.Owner); err != nil {
			return nil, fmt.Errorf("scan plan cadence: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) basis(planID int64) ([]BasisRow, error) {
	rows, err := r.db.Query(`SELECT id, ordinal, requirement, reference, how_met FROM assurance_plan_basis
		WHERE plan_id = ? ORDER BY ordinal, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan regulatory basis: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []BasisRow{}
	for rows.Next() {
		var b BasisRow
		if err := rows.Scan(&b.ID, &b.Ordinal, &b.Requirement, &b.Reference, &b.How); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) escalations(planID int64) ([]Escalation, error) {
	rows, err := r.db.Query(`SELECT id, ordinal, trigger_text, escalate_to, within_text FROM assurance_plan_escalations
		WHERE plan_id = ? ORDER BY ordinal, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan escalations: %w", err)
	}
	defer rows.Close()
	out := []Escalation{}
	for rows.Next() {
		var e Escalation
		if err := rows.Scan(&e.ID, &e.Ordinal, &e.Trigger, &e.EscalateTo, &e.Within); err != nil {
			return nil, fmt.Errorf("scan plan escalation: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) Create(p Plan, population []PopulationItem, history []HistoryEntry) (int64, error) {
	args, err := planArgs(p)
	if err != nil {
		return 0, err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// #nosec G202 -- the column list is a package constant, not input.
	id, err := tx.Insert(`INSERT INTO assurance_plans (`+prefixed("")+`) VALUES (`+placeholders(len(planColumnList))+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("insert assurance plan: %w", err)
	}
	if err := writeSteps(tx, id, p.Steps); err != nil {
		return 0, err
	}
	if err := writeQuarters(tx, id, p.Quarters); err != nil {
		return 0, err
	}
	if err := writeStrata(tx, id, p.Strata); err != nil {
		return 0, err
	}
	if err := writeAttributes(tx, id, p.Attributes); err != nil {
		return 0, err
	}
	for _, item := range population {
		if err := insertPopulationItem(tx, id, item); err != nil {
			return 0, err
		}
	}
	if err := writeGovernance(tx, id, Governance{RACI: p.RACI, Cadence: p.Cadence, Escalations: p.Escalations, Basis: p.Basis}); err != nil {
		return 0, err
	}
	if err := writeHistory(tx, id, history); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (r *SQLiteRepository) SaveHeader(p Plan, history []HistoryEntry, version *Version) error {
	args, err := planArgs(p)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	sets := make([]string, len(planColumnList))
	for i, c := range planColumnList {
		sets[i] = c + " = ?"
	}
	// #nosec G202 -- the column list is a package constant, not input.
	res, err := tx.Exec(`UPDATE assurance_plans SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, p.ID)...)
	if err != nil {
		return fmt.Errorf("update assurance plan: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	if version != nil {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_versions (plan_id, number, label, approved_by, approved_at,
			change_reason, snapshot_json, snapshot_sha256) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, version.Number, version.Label, version.ApprovedBy, version.ApprovedAt,
			version.ChangeReason, version.SnapshotJSON, version.SnapshotSHA256); err != nil {
			return fmt.Errorf("insert plan version: %w", err)
		}
	}
	if err := writeHistory(tx, p.ID, history); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRepository) inTx(planID int64, history []HistoryEntry, write func(tx *db.Tx) error) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := write(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE assurance_plans SET updated_at = ?, updated_by = ? WHERE id = ?`,
		latestStamp(history), latestActor(history), planID); err != nil {
		return fmt.Errorf("touch assurance plan: %w", err)
	}
	if err := writeHistory(tx, planID, history); err != nil {
		return err
	}
	return tx.Commit()
}

func latestStamp(history []HistoryEntry) string {
	if len(history) == 0 {
		return ""
	}
	return history[len(history)-1].At
}

func latestActor(history []HistoryEntry) string {
	if len(history) == 0 {
		return ""
	}
	return history[len(history)-1].Actor
}

func (r *SQLiteRepository) ReplaceSteps(planID int64, steps []Step, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		if _, err := tx.Exec(`DELETE FROM assurance_plan_steps WHERE plan_id = ?`, planID); err != nil {
			return fmt.Errorf("clear plan steps: %w", err)
		}
		return writeSteps(tx, planID, steps)
	})
}

func (r *SQLiteRepository) ReplaceQuarters(planID int64, quarters []Quarter, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		if _, err := tx.Exec(`DELETE FROM assurance_plan_quarters WHERE plan_id = ?`, planID); err != nil {
			return fmt.Errorf("clear plan quarters: %w", err)
		}
		return writeQuarters(tx, planID, quarters)
	})
}

func (r *SQLiteRepository) ReplaceGovernance(planID int64, roles []string, g Governance, history []HistoryEntry) error {
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return fmt.Errorf("encode raci roles: %w", err)
	}
	return r.inTx(planID, history, func(tx *db.Tx) error {
		if _, err := tx.Exec(`UPDATE assurance_plans SET raci_roles = ? WHERE id = ?`, string(rolesJSON), planID); err != nil {
			return fmt.Errorf("update raci roles: %w", err)
		}
		for _, table := range []string{"assurance_plan_raci", "assurance_plan_cadence", "assurance_plan_escalations", "assurance_plan_basis"} {
			// #nosec G202 -- the table names are the literals above.
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE plan_id = ?`, planID); err != nil {
				return fmt.Errorf("clear %s: %w", table, err)
			}
		}
		return writeGovernance(tx, planID, g)
	})
}

func (r *SQLiteRepository) UpdateStepStatus(planID, stepID int64, status string, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		res, err := tx.Exec(`UPDATE assurance_plan_steps SET status = ? WHERE id = ? AND plan_id = ?`, status, stepID, planID)
		if err != nil {
			return fmt.Errorf("update step status: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func writeSteps(tx *db.Tx, planID int64, steps []Step) error {
	for i, s := range steps {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_steps (plan_id, ordinal, step_ref, phase, title, step_type,
			responsible_role, accountable_role, start_date, end_date, deliverable, refs, status, quarter, generated, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			planID, i, s.StepRef, s.Phase, s.Title, s.StepType, s.ResponsibleRole, s.AccountableRole,
			s.StartDate, s.EndDate, s.Deliverable, s.Refs, s.Status, s.Quarter, boolToInt(s.Generated), s.Notes); err != nil {
			return fmt.Errorf("insert plan step %s: %w", s.StepRef, err)
		}
	}
	return nil
}

func writeQuarters(tx *db.Tx, planID int64, quarters []Quarter) error {
	for _, q := range quarters {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_quarters (plan_id, quarter, lod1_window_start, lod1_deadline,
			selection_date, fieldwork_start, fieldwork_end, findings_agreed_by, report_date, committee_label,
			retest_days, overhead_days, selected_at, selected_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			planID, q.Quarter, q.LOD1WindowStart, q.LOD1Deadline, q.SelectionDate, q.FieldworkStart, q.FieldworkEnd,
			q.FindingsAgreedBy, q.ReportDate, q.CommitteeLabel, q.RetestDays, q.OverheadDays, q.SelectedAt, q.SelectedBy); err != nil {
			return fmt.Errorf("insert plan quarter %d: %w", q.Quarter, err)
		}
	}
	return nil
}

func writeGovernance(tx *db.Tx, planID int64, g Governance) error {
	for i, row := range g.RACI {
		cells := row.Cells
		if cells == nil {
			cells = map[string]string{}
		}
		raw, err := json.Marshal(cells)
		if err != nil {
			return fmt.Errorf("encode raci cells: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO assurance_plan_raci (plan_id, ordinal, activity, cells) VALUES (?, ?, ?, ?)`,
			planID, i, row.Activity, string(raw)); err != nil {
			return fmt.Errorf("insert raci row: %w", err)
		}
	}
	for i, c := range g.Cadence {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_cadence (plan_id, ordinal, forum, frequency, content, owner)
			VALUES (?, ?, ?, ?, ?, ?)`, planID, i, c.Forum, c.Frequency, c.Content, c.Owner); err != nil {
			return fmt.Errorf("insert cadence row: %w", err)
		}
	}
	for i, e := range g.Escalations {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_escalations (plan_id, ordinal, trigger_text, escalate_to, within_text)
			VALUES (?, ?, ?, ?, ?)`, planID, i, e.Trigger, e.EscalateTo, e.Within); err != nil {
			return fmt.Errorf("insert escalation row: %w", err)
		}
	}
	for i, b := range g.Basis {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_basis (plan_id, ordinal, requirement, reference, how_met)
			VALUES (?, ?, ?, ?, ?)`, planID, i, b.Requirement, b.Reference, b.How); err != nil {
			return fmt.Errorf("insert regulatory basis row: %w", err)
		}
	}
	return nil
}

func writeHistory(tx *db.Tx, planID int64, history []HistoryEntry) error {
	for _, h := range history {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_history (plan_id, at, actor, event, subject, from_value, to_value, note)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, planID, h.At, h.Actor, h.Event, h.Subject, h.FromValue, h.ToValue, h.Note); err != nil {
			return fmt.Errorf("insert plan history: %w", err)
		}
	}
	return nil
}

func (r *SQLiteRepository) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM assurance_plans WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete assurance plan: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SQLiteRepository) ReferencesWithPrefix(prefix string) ([]string, error) {
	rows, err := r.db.Query(`SELECT reference FROM assurance_plans WHERE reference LIKE ?`, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("read plan references: %w", err)
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

func (r *SQLiteRepository) History(planID int64) ([]HistoryEntry, error) {
	rows, err := r.db.Query(`SELECT id, plan_id, at, actor, event, subject, from_value, to_value, note
		FROM assurance_plan_history WHERE plan_id = ? ORDER BY id DESC`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan history: %w", err)
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.ID, &h.PlanID, &h.At, &h.Actor, &h.Event, &h.Subject, &h.FromValue, &h.ToValue, &h.Note); err != nil {
			return nil, fmt.Errorf("scan plan history: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) Versions(planID int64) ([]Version, error) {
	rows, err := r.db.Query(`SELECT id, plan_id, number, label, approved_by, approved_at, change_reason, snapshot_sha256
		FROM assurance_plan_versions WHERE plan_id = ? ORDER BY number DESC`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan versions: %w", err)
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.PlanID, &v.Number, &v.Label, &v.ApprovedBy, &v.ApprovedAt, &v.ChangeReason, &v.SnapshotSHA256); err != nil {
			return nil, fmt.Errorf("scan plan version: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) Version(planID int64, number int) (Version, error) {
	var v Version
	err := r.db.QueryRow(`SELECT id, plan_id, number, label, approved_by, approved_at, change_reason, snapshot_json, snapshot_sha256
		FROM assurance_plan_versions WHERE plan_id = ? AND number = ?`, planID, number).Scan(
		&v.ID, &v.PlanID, &v.Number, &v.Label, &v.ApprovedBy, &v.ApprovedAt, &v.ChangeReason, &v.SnapshotJSON, &v.SnapshotSHA256)
	return v, err
}

func (r *SQLiteRepository) Clients() ([]Client, error) {
	rows, err := r.db.Query(`SELECT id, name FROM client_profiles ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("list client profiles: %w", err)
	}
	defer rows.Close()
	out := []Client{}
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
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
