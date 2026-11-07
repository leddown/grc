package assuranceplan

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"grc/internal/db"
)

// PopulationChange is one population edit, applied in a single transaction.
type PopulationChange struct {
	Add    []PopulationItem
	Update []PopulationItem
	Remove []int64
}

// errQuarterTaken is a selection that lost the race to another one for the
// same quarter: the stamp is set only where it was empty.
var errQuarterTaken = errors.New("quarter already selected")

func (r *SQLiteRepository) strata(planID int64) ([]Stratum, error) {
	rows, err := r.db.Query(`SELECT id, ordinal, code, name, definition, sampling_enabled, confidence, tolerable_rate,
		floor_pct, effort_per_test, effort_per_review, wave_q1, wave_q2, wave_q3, wave_q4,
		topup_q1, topup_q2, topup_q3, topup_q4, planned_size, cycle_years, takes
		FROM assurance_plan_strata WHERE plan_id = ? ORDER BY ordinal, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan strata: %w", err)
	}
	defer rows.Close()
	out := []Stratum{}
	for rows.Next() {
		var s Stratum
		var sampling int
		var takes string
		if err := rows.Scan(&s.ID, &s.Ordinal, &s.Code, &s.Name, &s.Definition, &sampling, &s.Confidence, &s.TolerableRate,
			&s.FloorPct, &s.EffortPerTest, &s.EffortPerReview, &s.Waves[0], &s.Waves[1], &s.Waves[2], &s.Waves[3],
			&s.TopUps[0], &s.TopUps[1], &s.TopUps[2], &s.TopUps[3], &s.PlannedSize, &s.CycleYears, &takes); err != nil {
			return nil, fmt.Errorf("scan plan stratum: %w", err)
		}
		s.SamplingEnabled = sampling == 1
		s.Takes = splitList(takes)
		out = append(out, s)
	}
	return out, rows.Err()
}

func writeStrata(tx *db.Tx, planID int64, strata []Stratum) error {
	for i, s := range strata {
		if _, err := tx.Exec(`INSERT INTO assurance_plan_strata (plan_id, ordinal, code, name, definition, sampling_enabled,
			confidence, tolerable_rate, floor_pct, effort_per_test, effort_per_review, wave_q1, wave_q2, wave_q3, wave_q4,
			topup_q1, topup_q2, topup_q3, topup_q4, planned_size, cycle_years, takes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			planID, i, s.Code, s.Name, s.Definition, boolToInt(s.SamplingEnabled), s.Confidence, s.TolerableRate,
			s.FloorPct, s.EffortPerTest, s.EffortPerReview, s.Waves[0], s.Waves[1], s.Waves[2], s.Waves[3],
			s.TopUps[0], s.TopUps[1], s.TopUps[2], s.TopUps[3], s.PlannedSize, s.CycleYears, strings.Join(s.Takes, ", ")); err != nil {
			return fmt.Errorf("insert plan stratum %s: %w", s.Code, err)
		}
	}
	return nil
}

func (r *SQLiteRepository) ReplaceStrata(planID int64, strata []Stratum, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		if _, err := tx.Exec(`DELETE FROM assurance_plan_strata WHERE plan_id = ?`, planID); err != nil {
			return fmt.Errorf("clear plan strata: %w", err)
		}
		return writeStrata(tx, planID, strata)
	})
}

const populationColumns = `item_ref, name, stratum_code, cif, business_line, hosting, internet_facing, owner,
	wave_quarter, attested_on, dq_status, dq_reviewed_on, topup_quarter, topup_rationale, last_tested_on, source_kind, source_ref`

func populationArgs(it PopulationItem) []any {
	return []any{it.ItemRef, it.Name, it.StratumCode, it.CIF, it.BusinessLine, it.Hosting, boolToInt(it.InternetFacing), it.Owner,
		it.WaveQuarter, it.AttestedOn, it.DQStatus, it.DQReviewedOn, it.TopUpQuarter, it.TopUpRationale, it.LastTestedOn, it.SourceKind, it.SourceRef}
}

func (r *SQLiteRepository) Population(planID int64) ([]PopulationItem, error) {
	rows, err := r.db.Query(`SELECT x.id, x.item_ref, x.name, x.stratum_code, x.cif, x.business_line, x.hosting, x.internet_facing,
		x.owner, x.wave_quarter, x.attested_on, x.dq_status, x.dq_reviewed_on, x.topup_quarter, x.topup_rationale,
		x.last_tested_on, x.source_kind, x.source_ref, COALESCE(s.quarter, 0), COALESCE(s.basis, '')
		FROM assurance_plan_population x
		LEFT JOIN assurance_plan_selections s ON s.population_id = x.id
		WHERE x.plan_id = ? ORDER BY x.item_ref`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan population: %w", err)
	}
	defer rows.Close()
	out := []PopulationItem{}
	for rows.Next() {
		var it PopulationItem
		var internet int
		if err := rows.Scan(&it.ID, &it.ItemRef, &it.Name, &it.StratumCode, &it.CIF, &it.BusinessLine, &it.Hosting, &internet, &it.Owner,
			&it.WaveQuarter, &it.AttestedOn, &it.DQStatus, &it.DQReviewedOn, &it.TopUpQuarter, &it.TopUpRationale,
			&it.LastTestedOn, &it.SourceKind, &it.SourceRef, &it.SelectedQuarter, &it.SelectionBasis); err != nil {
			return nil, fmt.Errorf("scan population item: %w", err)
		}
		it.InternetFacing = internet == 1
		out = append(out, it)
	}
	return out, rows.Err()
}

func insertPopulationItem(tx *db.Tx, planID int64, it PopulationItem) error {
	if _, err := tx.Exec(`INSERT INTO assurance_plan_population (plan_id, `+populationColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append([]any{planID}, populationArgs(it)...)...); err != nil {
		return fmt.Errorf("insert population item %s: %w", it.ItemRef, err)
	}
	return nil
}

func updatePopulationItem(tx *db.Tx, planID int64, it PopulationItem) error {
	res, err := tx.Exec(`UPDATE assurance_plan_population SET item_ref = ?, name = ?, stratum_code = ?, cif = ?,
		business_line = ?, hosting = ?, internet_facing = ?, owner = ?, wave_quarter = ?, attested_on = ?, dq_status = ?,
		dq_reviewed_on = ?, topup_quarter = ?, topup_rationale = ?, last_tested_on = ?, source_kind = ?, source_ref = ?
		WHERE id = ? AND plan_id = ?`, append(populationArgs(it), it.ID, planID)...)
	if err != nil {
		return fmt.Errorf("update population item %s: %w", it.ItemRef, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SQLiteRepository) ApplyPopulation(planID int64, change PopulationChange, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error {
		for _, id := range change.Remove {
			if _, err := tx.Exec(`DELETE FROM assurance_plan_population WHERE id = ? AND plan_id = ?`, id, planID); err != nil {
				return fmt.Errorf("remove population item: %w", err)
			}
		}
		for _, it := range change.Update {
			if err := updatePopulationItem(tx, planID, it); err != nil {
				return err
			}
		}
		for _, it := range change.Add {
			if err := insertPopulationItem(tx, planID, it); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *SQLiteRepository) UpdatePopulationItem(planID int64, item PopulationItem, history []HistoryEntry) error {
	return r.inTx(planID, history, func(tx *db.Tx) error { return updatePopulationItem(tx, planID, item) })
}

func (r *SQLiteRepository) Selections(planID int64) ([]Selection, error) {
	rows, err := r.db.Query(`SELECT s.id, s.quarter, s.population_id, s.item_ref, COALESCE(x.name, ''), s.stratum_code, s.basis,
		s.rank_order, s.rank_hash, s.plan_version, s.selected_at, s.selected_by
		FROM assurance_plan_selections s LEFT JOIN assurance_plan_population x ON x.id = s.population_id
		WHERE s.plan_id = ? ORDER BY s.quarter, s.id`, planID)
	if err != nil {
		return nil, fmt.Errorf("load plan selections: %w", err)
	}
	defer rows.Close()
	out := []Selection{}
	for rows.Next() {
		var s Selection
		if err := rows.Scan(&s.ID, &s.Quarter, &s.PopulationID, &s.ItemRef, &s.ItemName, &s.StratumCode, &s.Basis,
			&s.RankOrder, &s.RankHash, &s.PlanVersion, &s.SelectedAt, &s.SelectedBy); err != nil {
			return nil, fmt.Errorf("scan plan selection: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveSelection freezes a quarter: it stamps the quarter only if no other
// selection got there first, writes the picks, and moves an approved plan to
// Active when activate carries it.
func (r *SQLiteRepository) SaveSelection(planID int64, quarter int, picks []Selection, activate *Plan, history []HistoryEntry) error {
	at, by := latestStamp(history), latestActor(history)
	return r.inTx(planID, history, func(tx *db.Tx) error {
		res, err := tx.Exec(`UPDATE assurance_plan_quarters SET selected_at = ?, selected_by = ?
			WHERE plan_id = ? AND quarter = ? AND selected_at = ''`, at, by, planID, quarter)
		if err != nil {
			return fmt.Errorf("stamp quarter selection: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return errQuarterTaken
		}
		for _, s := range picks {
			if _, err := tx.Exec(`INSERT INTO assurance_plan_selections (plan_id, quarter, population_id, item_ref, stratum_code,
				basis, rank_order, rank_hash, plan_version, selected_at, selected_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				planID, quarter, s.PopulationID, s.ItemRef, s.StratumCode, s.Basis, s.RankOrder, s.RankHash, s.PlanVersion, at, by); err != nil {
				return fmt.Errorf("insert selection %s: %w", s.ItemRef, err)
			}
		}
		if activate != nil {
			if _, err := tx.Exec(`UPDATE assurance_plans SET status = ?, activated_at = ? WHERE id = ? AND status = ?`,
				StatusActive, activate.ActivatedAt, planID, StatusApproved); err != nil {
				return fmt.Errorf("activate plan: %w", err)
			}
		}
		return nil
	})
}
