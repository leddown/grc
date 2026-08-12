package controlcatalog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"grc/internal/controlid"
	"grc/internal/db"
)

type Repository interface {
	Upsert(control Control) error
	UpsertMany(controls []Control) error
	List(search, baseline, cia string) ([]Control, error)
	ListByFamily(family string) ([]Control, error)
	GetByControlID(controlID string) (Control, error)
	DeleteByControlID(controlID string) error
	ListFamilyVisibility() ([]FamilyVisibility, error)
	SetFamilyVisibility(family string, enabled bool) error
}

type FamilyVisibility struct {
	Family  string `json:"family"`
	Enabled bool   `json:"enabled"`
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

const upsertControlSQL = `INSERT INTO rcsa_controls (
		source_key, control_id, control_type, name, family, in_low, in_moderate, in_high, in_privacy,
		mapping_baselines_json, threats_json,
		confidentiality_status, integrity_status, availability_status,
		justification, potentially_common_inheritable, requirements, discussion, related_controls_json, appendix_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(control_id) DO UPDATE SET
		source_key = excluded.source_key,
		control_type = excluded.control_type,
		name = excluded.name,
		family = excluded.family,
		in_low = excluded.in_low,
		in_moderate = excluded.in_moderate,
		in_high = excluded.in_high,
		in_privacy = excluded.in_privacy,
		mapping_baselines_json = excluded.mapping_baselines_json,
		threats_json = excluded.threats_json,
		confidentiality_status = excluded.confidentiality_status,
		integrity_status = excluded.integrity_status,
		availability_status = excluded.availability_status,
		justification = excluded.justification,
		potentially_common_inheritable = excluded.potentially_common_inheritable,
		requirements = excluded.requirements,
		discussion = excluded.discussion,
		related_controls_json = excluded.related_controls_json,
		appendix_json = excluded.appendix_json`

func upsertControlArgs(control Control) ([]any, error) {
	mappingBaselinesRaw, threatsRaw, relatedControlsRaw, appendixRaw, err := marshalControl(control)
	if err != nil {
		return nil, err
	}
	return []any{
		control.SourceKey,
		control.ID,
		control.Type,
		control.Name,
		control.Family,
		boolToInt(hasBaseline(control.Baselines, "Low")),
		boolToInt(hasBaseline(control.Baselines, "Moderate")),
		boolToInt(hasBaseline(control.Baselines, "High")),
		boolToInt(hasBaseline(control.Baselines, "Privacy")),
		mappingBaselinesRaw,
		threatsRaw,
		control.Confidentiality,
		control.Integrity,
		control.Availability,
		control.Justification,
		control.PotentiallyCommonInheritable,
		control.Requirements,
		control.Discussion,
		relatedControlsRaw,
		appendixRaw,
	}, nil
}

func (r *SQLiteRepository) Upsert(control Control) error {
	args, err := upsertControlArgs(control)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(upsertControlSQL, args...)
	return err
}

// UpsertMany upserts all controls within a single transaction, so a full
// catalog reseed costs one commit instead of one per control.
func (r *SQLiteRepository) UpsertMany(controls []Control) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(upsertControlSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, control := range controls {
		args, err := upsertControlArgs(control)
		if err != nil {
			return fmt.Errorf("upsert %s: %w", control.ID, err)
		}
		if _, err := stmt.Exec(args...); err != nil {
			return fmt.Errorf("upsert %s: %w", control.ID, err)
		}
	}
	return tx.Commit()
}

func (r *SQLiteRepository) List(search, baseline, cia string) ([]Control, error) {
	query := `SELECT
		source_key, control_id, control_type, name, family, in_low, in_moderate, in_high, in_privacy,
		mapping_baselines_json, threats_json,
		confidentiality_status, integrity_status, availability_status,
		justification, potentially_common_inheritable, requirements, discussion, related_controls_json, appendix_json
	FROM rcsa_controls WHERE 1=1`

	args := make([]any, 0, 4)
	if search != "" {
		query += ` AND (UPPER(control_id) LIKE ? OR UPPER(family) LIKE ?)`
		like := "%" + strings.ToUpper(search) + "%"
		args = append(args, like, like)
	}

	switch baseline {
	case "Low":
		query += ` AND in_low = 1`
	case "Moderate":
		query += ` AND in_moderate = 1`
	case "High":
		query += ` AND in_high = 1`
	case "Privacy":
		query += ` AND in_privacy = 1`
	}

	switch cia {
	case "c":
		query += ` AND confidentiality_status <> ''`
	case "i":
		query += ` AND integrity_status <> ''`
	case "a":
		query += ` AND availability_status <> ''`
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	controls := make([]Control, 0)
	for rows.Next() {
		control, err := scanControl(rows)
		if err != nil {
			return nil, err
		}
		controls = append(controls, control)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(controls, func(i, j int) bool {
		return controlid.Less(controls[i].ID, controls[j].ID)
	})

	return controls, nil
}

func (r *SQLiteRepository) ListByFamily(family string) ([]Control, error) {
	family = strings.ToUpper(strings.TrimSpace(family))
	if family == "" {
		return r.List("", "", "")
	}

	rows, err := r.db.Query(
		`SELECT
			source_key, control_id, control_type, name, family, in_low, in_moderate, in_high, in_privacy,
			mapping_baselines_json, threats_json,
			confidentiality_status, integrity_status, availability_status,
			justification, potentially_common_inheritable, requirements, discussion, related_controls_json, appendix_json
		FROM rcsa_controls
		WHERE UPPER(TRIM(family)) = ?`,
		family,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	controls := make([]Control, 0)
	for rows.Next() {
		control, err := scanControl(rows)
		if err != nil {
			return nil, err
		}
		controls = append(controls, control)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(controls, func(i, j int) bool {
		return controlid.Less(controls[i].ID, controls[j].ID)
	})

	return controls, nil
}

func (r *SQLiteRepository) GetByControlID(controlID string) (Control, error) {
	row := r.db.QueryRow(
		`SELECT
			source_key, control_id, control_type, name, family, in_low, in_moderate, in_high, in_privacy,
			mapping_baselines_json, threats_json,
			confidentiality_status, integrity_status, availability_status,
			justification, potentially_common_inheritable, requirements, discussion, related_controls_json, appendix_json
		FROM rcsa_controls
		WHERE control_id = ?`,
		controlID,
	)
	return scanControl(row)
}

func (r *SQLiteRepository) DeleteByControlID(controlID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM nist_stride_mappings WHERE control_id = ?`, controlID); err != nil {
		return err
	}

	result, err := tx.Exec(`DELETE FROM rcsa_controls WHERE control_id = ?`, controlID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return tx.Commit()
}

func (r *SQLiteRepository) ListFamilyVisibility() ([]FamilyVisibility, error) {
	rows, err := r.db.Query(
		`WITH families AS (
			SELECT DISTINCT UPPER(TRIM(family)) AS family
			FROM rcsa_controls
			WHERE TRIM(family) <> ''
			UNION
			SELECT UPPER(TRIM(family)) AS family
			FROM control_family_visibility
			WHERE TRIM(family) <> ''
		)
		SELECT f.family, COALESCE(v.enabled, 1) AS enabled
		FROM families f
		LEFT JOIN control_family_visibility v ON UPPER(TRIM(v.family)) = f.family
		ORDER BY f.family`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]FamilyVisibility, 0)
	for rows.Next() {
		var item FamilyVisibility
		var enabled int
		if err := rows.Scan(&item.Family, &enabled); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SQLiteRepository) SetFamilyVisibility(family string, enabled bool) error {
	_, err := r.db.Exec(
		`INSERT INTO control_family_visibility (family, enabled)
		VALUES (?, ?)
		ON CONFLICT(family) DO UPDATE SET enabled = excluded.enabled`,
		strings.ToUpper(strings.TrimSpace(family)),
		boolToInt(enabled),
	)
	return err
}

func scanControl(scanner interface{ Scan(dest ...any) error }) (Control, error) {
	var control Control
	var inLow int
	var inModerate int
	var inHigh int
	var inPrivacy int
	var mappingBaselinesRaw string
	var threatsRaw string
	var relatedControlsRaw string
	var appendixRaw string

	if err := scanner.Scan(
		&control.SourceKey,
		&control.ID,
		&control.Type,
		&control.Name,
		&control.Family,
		&inLow,
		&inModerate,
		&inHigh,
		&inPrivacy,
		&mappingBaselinesRaw,
		&threatsRaw,
		&control.Confidentiality,
		&control.Integrity,
		&control.Availability,
		&control.Justification,
		&control.PotentiallyCommonInheritable,
		&control.Requirements,
		&control.Discussion,
		&relatedControlsRaw,
		&appendixRaw,
	); err != nil {
		return Control{}, err
	}

	control.Baselines = make([]string, 0, 4)
	if inLow == 1 {
		control.Baselines = append(control.Baselines, "Low")
	}
	if inModerate == 1 {
		control.Baselines = append(control.Baselines, "Moderate")
	}
	if inHigh == 1 {
		control.Baselines = append(control.Baselines, "High")
	}
	if inPrivacy == 1 {
		control.Baselines = append(control.Baselines, "Privacy")
	}

	if err := json.Unmarshal([]byte(mappingBaselinesRaw), &control.MappingBaselines); err != nil {
		return Control{}, err
	}
	if err := json.Unmarshal([]byte(threatsRaw), &control.Threats); err != nil {
		return Control{}, err
	}
	if err := json.Unmarshal([]byte(relatedControlsRaw), &control.RelatedControls); err != nil {
		return Control{}, err
	}
	if strings.TrimSpace(appendixRaw) != "" && strings.TrimSpace(appendixRaw) != "null" {
		if err := json.Unmarshal([]byte(appendixRaw), &control.Appendix); err != nil {
			return Control{}, err
		}
	}

	normalizeControl(&control)
	return control, nil
}

func marshalControl(control Control) (string, string, string, string, error) {
	mappingBaselinesRaw, err := json.Marshal(control.MappingBaselines)
	if err != nil {
		return "", "", "", "", err
	}

	threatsRaw, err := json.Marshal(control.Threats)
	if err != nil {
		return "", "", "", "", err
	}
	relatedControlsRaw, err := json.Marshal(control.RelatedControls)
	if err != nil {
		return "", "", "", "", err
	}
	appendixRaw, err := json.Marshal(control.Appendix)
	if err != nil {
		return "", "", "", "", err
	}

	return string(mappingBaselinesRaw), string(threatsRaw), string(relatedControlsRaw), string(appendixRaw), nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
