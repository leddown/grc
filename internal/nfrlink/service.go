package nfrlink

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"carelockconsulting/internal/controlid"
	"carelockconsulting/internal/db"
)

type LinkRow struct {
	NFRKey           string `json:"nfr_key"`
	NFRID            string `json:"nfr_id"`
	NFRSummary       string `json:"nfr_summary"`
	NFRDomain        string `json:"nfr_domain"`
	NISTMappingRaw   string `json:"nist_mapping_raw"`
	MappingControlID string `json:"mapping_control_id"`
	ControlID        string `json:"control_id"`
	ControlName      string `json:"control_name"`
	ControlFamily    string `json:"control_family"`
	Matched          bool   `json:"matched"`
	OverrideApplied  bool   `json:"override_applied"`
}

type Service struct {
	db *db.Conn
}

func NewService(conn *db.Conn) *Service {
	return &Service{db: conn}
}

var mappingTokenPattern = regexp.MustCompile(`(?i)\b[A-Z]{2}-\d+(?:\(\d+\)|\.\d+)?(?:$|[^A-Z0-9])`)

func (s *Service) Rebuild() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM security_nfr_control_links`); err != nil {
		return err
	}

	rows, err := tx.Query(`SELECT record_key, nfr_id, summary, domain, nist_mapping FROM security_nfrs`)
	if err != nil {
		return err
	}
	defer rows.Close()

	controlsByID, err := loadControlsByID(tx)
	if err != nil {
		return err
	}
	overrides, err := loadOverrides(tx)
	if err != nil {
		return err
	}

	insertStmt, err := tx.Prepare(`
		INSERT INTO security_nfr_control_links (
			nfr_key, nfr_id, nfr_summary, nfr_domain, nist_mapping_raw,
			mapping_control_id, control_id, control_name, control_family, matched
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for rows.Next() {
		var nfrKey string
		var nfrID string
		var summary string
		var domain string
		var mappingRaw string
		if err := rows.Scan(&nfrKey, &nfrID, &summary, &domain, &mappingRaw); err != nil {
			return err
		}

		tokens := extractMappingTokens(mappingRaw)
		if len(tokens) == 0 {
			if _, err := insertStmt.Exec(
				nfrKey, nfrID, summary, domain, mappingRaw,
				"", "", "", "", 0,
			); err != nil {
				return err
			}
			continue
		}

		for _, token := range tokens {
			normalizedToken := controlid.Normalize(token)
			if override, ok := overrides[overrideKey(nfrKey, normalizedToken)]; ok {
				if override.Matched {
					control, exists := controlsByID[override.OverrideControlID]
					if !exists || strings.TrimSpace(control.ID) == "" {
						if _, err := insertStmt.Exec(
							nfrKey, nfrID, summary, domain, mappingRaw,
							normalizedToken, "", "", "", 0,
						); err != nil {
							return err
						}
						continue
					}
					if _, err := insertStmt.Exec(
						nfrKey, nfrID, summary, domain, mappingRaw,
						normalizedToken, control.ID, control.Name, control.Family, 1,
					); err != nil {
						return err
					}
				} else {
					if _, err := insertStmt.Exec(
						nfrKey, nfrID, summary, domain, mappingRaw,
						normalizedToken, "", "", "", 0,
					); err != nil {
						return err
					}
				}
				continue
			}
			if control, ok := controlsByID[normalizedToken]; ok {
				if _, err := insertStmt.Exec(
					nfrKey, nfrID, summary, domain, mappingRaw,
					normalizedToken, control.ID, control.Name, control.Family, 1,
				); err != nil {
					return err
				}
				continue
			}

			if _, err := insertStmt.Exec(
				nfrKey, nfrID, summary, domain, mappingRaw,
				normalizedToken, "", "", "", 0,
			); err != nil {
				return err
			}
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return tx.Commit()
}

type controlLookup struct {
	ID     string
	Name   string
	Family string
}

func loadControlsByID(tx *db.Tx) (map[string]controlLookup, error) {
	rows, err := tx.Query(`SELECT control_id, name, family FROM rcsa_controls`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]controlLookup)
	for rows.Next() {
		var item controlLookup
		if err := rows.Scan(&item.ID, &item.Name, &item.Family); err != nil {
			return nil, err
		}
		out[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) List(search string, onlyUnmatched bool) ([]LinkRow, error) {
	query := `SELECT
		nfr_key, nfr_id, nfr_summary, nfr_domain, nist_mapping_raw,
		mapping_control_id, control_id, control_name, control_family, matched
	FROM security_nfr_control_links
	WHERE 1=1`

	args := make([]any, 0, 5)
	if onlyUnmatched {
		query += ` AND matched = 0`
	}
	search = strings.TrimSpace(search)
	if search != "" {
		query += ` AND (
			UPPER(nfr_key) LIKE ? OR
			UPPER(nfr_id) LIKE ? OR
			UPPER(nfr_summary) LIKE ? OR
			UPPER(mapping_control_id) LIKE ? OR
			UPPER(control_id) LIKE ?
		)`
		like := "%" + strings.ToUpper(search) + "%"
		args = append(args, like, like, like, like, like)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overrideSet, err := s.loadOverrideSet()
	if err != nil {
		return nil, err
	}

	out := make([]LinkRow, 0)
	for rows.Next() {
		var row LinkRow
		var matchedInt int
		if err := rows.Scan(
			&row.NFRKey,
			&row.NFRID,
			&row.NFRSummary,
			&row.NFRDomain,
			&row.NISTMappingRaw,
			&row.MappingControlID,
			&row.ControlID,
			&row.ControlName,
			&row.ControlFamily,
			&matchedInt,
		); err != nil {
			return nil, err
		}
		row.Matched = matchedInt == 1
		_, row.OverrideApplied = overrideSet[overrideKey(row.NFRKey, row.MappingControlID)]
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		leftID, leftErr := strconv.ParseFloat(strings.TrimSpace(out[i].NFRID), 64)
		rightID, rightErr := strconv.ParseFloat(strings.TrimSpace(out[j].NFRID), 64)
		if leftErr == nil && rightErr == nil && leftID != rightID {
			return leftID < rightID
		}
		if out[i].NFRID != out[j].NFRID {
			return out[i].NFRID < out[j].NFRID
		}
		leftKey, leftKeyErr := strconv.Atoi(out[i].NFRKey)
		rightKey, rightKeyErr := strconv.Atoi(out[j].NFRKey)
		if leftKeyErr == nil && rightKeyErr == nil && leftKey != rightKey {
			return leftKey < rightKey
		}
		if out[i].NFRKey != out[j].NFRKey {
			return out[i].NFRKey < out[j].NFRKey
		}
		if out[i].MappingControlID != out[j].MappingControlID {
			return out[i].MappingControlID < out[j].MappingControlID
		}
		return out[i].ControlID < out[j].ControlID
	})

	return out, nil
}

func (s *Service) loadOverrideSet() (map[string]struct{}, error) {
	rows, err := s.db.Query(`SELECT nfr_key, mapping_control_id FROM nfr_control_link_overrides`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]struct{})
	for rows.Next() {
		var nfrKey string
		var mappingControlID string
		if err := rows.Scan(&nfrKey, &mappingControlID); err != nil {
			return nil, err
		}
		out[overrideKey(nfrKey, mappingControlID)] = struct{}{}
	}
	return out, rows.Err()
}

func extractMappingTokens(raw string) []string {
	matches := mappingTokenPattern.FindAllString(strings.ToUpper(raw), -1)
	if len(matches) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, value := range matches {
		value = strings.TrimRightFunc(value, func(r rune) bool {
			return !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '(' && r != ')' && r != '.'
		})
		token := controlid.Normalize(value)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	sort.Strings(out)
	return out
}
