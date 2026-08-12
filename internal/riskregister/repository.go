package riskregister

import (
	"database/sql"
	"strings"

	"grc/internal/db"
)

type Repository interface {
	List(search, status string) ([]Risk, error)
	GetByID(id int64) (Risk, error)
	Create(risk Risk) (Risk, error)
	Update(id int64, risk Risk) (Risk, error)
	Delete(id int64) error
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

func (r *SQLiteRepository) List(search, status string) ([]Risk, error) {
	query := `SELECT
		id, risk_id, title, business_unit, asset, threat_source, vulnerability,
		likelihood, impact, inherent_score, current_controls,
		residual_likelihood, residual_impact, residual_score,
		response_strategy, response_action, owner, status,
		target_date, last_review_date, next_review_date, risk_appetite_aligned, notes
	FROM security_risk_register
	WHERE 1=1`

	args := make([]any, 0, 3)
	if search != "" {
		query += ` AND (
			UPPER(risk_id) LIKE ? OR
			UPPER(title) LIKE ? OR
			UPPER(asset) LIKE ? OR
			UPPER(owner) LIKE ?
		)`
		like := "%" + strings.ToUpper(search) + "%"
		args = append(args, like, like, like, like)
	}
	if status != "" {
		query += ` AND UPPER(status) = ?`
		args = append(args, strings.ToUpper(status))
	}
	query += ` ORDER BY residual_score DESC, inherent_score DESC, id ASC`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Risk, 0)
	for rows.Next() {
		item, err := scanRisk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SQLiteRepository) GetByID(id int64) (Risk, error) {
	row := r.db.QueryRow(`SELECT
		id, risk_id, title, business_unit, asset, threat_source, vulnerability,
		likelihood, impact, inherent_score, current_controls,
		residual_likelihood, residual_impact, residual_score,
		response_strategy, response_action, owner, status,
		target_date, last_review_date, next_review_date, risk_appetite_aligned, notes
	FROM security_risk_register WHERE id = ?`, id)
	return scanRisk(row)
}

func (r *SQLiteRepository) Create(risk Risk) (Risk, error) {
	id, err := r.db.Insert(`INSERT INTO security_risk_register (
		risk_id, title, business_unit, asset, threat_source, vulnerability,
		likelihood, impact, inherent_score, current_controls,
		residual_likelihood, residual_impact, residual_score,
		response_strategy, response_action, owner, status,
		target_date, last_review_date, next_review_date, risk_appetite_aligned, notes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		risk.RiskID, risk.Title, risk.BusinessUnit, risk.Asset, risk.ThreatSource, risk.Vulnerability,
		risk.Likelihood, risk.Impact, risk.InherentScore, risk.CurrentControls,
		risk.ResidualLikelihood, risk.ResidualImpact, risk.ResidualScore,
		risk.ResponseStrategy, risk.ResponseAction, risk.Owner, risk.Status,
		risk.TargetDate, risk.LastReviewDate, risk.NextReviewDate, boolToInt(risk.RiskAppetiteAligned), risk.Notes,
	)
	if err != nil {
		return Risk{}, err
	}
	return r.GetByID(id)
}

func (r *SQLiteRepository) Update(id int64, risk Risk) (Risk, error) {
	result, err := r.db.Exec(`UPDATE security_risk_register SET
		risk_id = ?, title = ?, business_unit = ?, asset = ?, threat_source = ?, vulnerability = ?,
		likelihood = ?, impact = ?, inherent_score = ?, current_controls = ?,
		residual_likelihood = ?, residual_impact = ?, residual_score = ?,
		response_strategy = ?, response_action = ?, owner = ?, status = ?,
		target_date = ?, last_review_date = ?, next_review_date = ?, risk_appetite_aligned = ?, notes = ?
	WHERE id = ?`,
		risk.RiskID, risk.Title, risk.BusinessUnit, risk.Asset, risk.ThreatSource, risk.Vulnerability,
		risk.Likelihood, risk.Impact, risk.InherentScore, risk.CurrentControls,
		risk.ResidualLikelihood, risk.ResidualImpact, risk.ResidualScore,
		risk.ResponseStrategy, risk.ResponseAction, risk.Owner, risk.Status,
		risk.TargetDate, risk.LastReviewDate, risk.NextReviewDate, boolToInt(risk.RiskAppetiteAligned), risk.Notes,
		id,
	)
	if err != nil {
		return Risk{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Risk{}, err
	}
	if affected == 0 {
		return Risk{}, sql.ErrNoRows
	}
	return r.GetByID(id)
}

func (r *SQLiteRepository) Delete(id int64) error {
	result, err := r.db.Exec(`DELETE FROM security_risk_register WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanRisk(scanner interface{ Scan(dest ...any) error }) (Risk, error) {
	var item Risk
	var appetiteInt int
	if err := scanner.Scan(
		&item.ID, &item.RiskID, &item.Title, &item.BusinessUnit, &item.Asset, &item.ThreatSource, &item.Vulnerability,
		&item.Likelihood, &item.Impact, &item.InherentScore, &item.CurrentControls,
		&item.ResidualLikelihood, &item.ResidualImpact, &item.ResidualScore,
		&item.ResponseStrategy, &item.ResponseAction, &item.Owner, &item.Status,
		&item.TargetDate, &item.LastReviewDate, &item.NextReviewDate, &appetiteInt, &item.Notes,
	); err != nil {
		return Risk{}, err
	}
	item.RiskAppetiteAligned = appetiteInt == 1
	return item, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
