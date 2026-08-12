package nfrlink

import (
	"database/sql"
	"fmt"
	"strings"

	"grc/internal/controlid"
	"grc/internal/db"
)

type Override struct {
	NFRKey            string `json:"nfr_key"`
	MappingControlID  string `json:"mapping_control_id"`
	OverrideControlID string `json:"override_control_id"`
	Matched           bool   `json:"matched"`
}

func (s *Service) ListOverrides() ([]Override, error) {
	rows, err := s.db.Query(`SELECT nfr_key, mapping_control_id, override_control_id, matched FROM nfr_control_link_overrides ORDER BY nfr_key, mapping_control_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Override, 0)
	for rows.Next() {
		var item Override
		var matched int
		if err := rows.Scan(&item.NFRKey, &item.MappingControlID, &item.OverrideControlID, &matched); err != nil {
			return nil, err
		}
		item.Matched = matched == 1
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) SetOverride(item Override) error {
	item.NFRKey = strings.TrimSpace(item.NFRKey)
	item.MappingControlID = controlid.Normalize(item.MappingControlID)
	item.OverrideControlID = controlid.Normalize(item.OverrideControlID)
	if item.NFRKey == "" {
		return fmt.Errorf("nfr_key is required")
	}
	if item.MappingControlID == "" {
		return fmt.Errorf("mapping_control_id is required")
	}
	if item.Matched {
		if item.OverrideControlID == "" {
			return fmt.Errorf("override_control_id is required when matched is true")
		}
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM rcsa_controls WHERE control_id = ?`, item.OverrideControlID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("override_control_id does not exist")
		}
	}

	_, err := s.db.Exec(
		`INSERT INTO nfr_control_link_overrides (nfr_key, mapping_control_id, override_control_id, matched)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(nfr_key, mapping_control_id) DO UPDATE SET
			override_control_id = excluded.override_control_id,
			matched = excluded.matched`,
		item.NFRKey,
		item.MappingControlID,
		item.OverrideControlID,
		boolToInt(item.Matched),
	)
	return err
}

func (s *Service) DeleteOverride(nfrKey string, mappingControlID string) error {
	result, err := s.db.Exec(
		`DELETE FROM nfr_control_link_overrides WHERE nfr_key = ? AND mapping_control_id = ?`,
		strings.TrimSpace(nfrKey),
		controlid.Normalize(mappingControlID),
	)
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
	return nil
}

func loadOverrides(tx *db.Tx) (map[string]Override, error) {
	rows, err := tx.Query(`SELECT nfr_key, mapping_control_id, override_control_id, matched FROM nfr_control_link_overrides`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]Override)
	for rows.Next() {
		var item Override
		var matched int
		if err := rows.Scan(&item.NFRKey, &item.MappingControlID, &item.OverrideControlID, &matched); err != nil {
			return nil, err
		}
		item.Matched = matched == 1
		out[overrideKey(item.NFRKey, item.MappingControlID)] = item
	}
	return out, rows.Err()
}

func overrideKey(nfrKey string, mappingControlID string) string {
	return strings.TrimSpace(nfrKey) + "|" + controlid.Normalize(mappingControlID)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
