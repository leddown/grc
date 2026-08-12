package reports

import (
	"encoding/json"
	"sort"
	"strings"

	"grc/internal/controlid"
	"grc/internal/db"
)

type ControlSummary struct {
	ControlID          string              `json:"control_id"`
	Name               string              `json:"name"`
	Family             string              `json:"family"`
	Threats            []string            `json:"threats"`
	Confidentiality    string              `json:"confidentiality"`
	Integrity          string              `json:"integrity"`
	Availability       string              `json:"availability"`
	LinkedSecurityNFRs []LinkedSecurityNFR `json:"linked_security_nfrs"`
}

type LinkedSecurityNFR struct {
	Key string `json:"key"`
	ID  string `json:"id"`
}

type Service struct {
	db *db.Conn
}

type ReportData struct {
	Items         []ControlSummary
	UnlinkedCount int
	LinkedCount   int
	TotalControls int
}

func NewService(conn *db.Conn) *Service {
	return &Service{db: conn}
}

type controlTypeFilter int
type reportModeFilter int

const (
	controlTypeAll controlTypeFilter = iota
	controlTypeControl
	controlTypeEnhancement
)

const (
	reportModeAll reportModeFilter = iota
	reportModeUnlinked
	reportModeLinked
)

const familyVisibleSQL = `COALESCE((
			SELECT v.enabled
			FROM control_family_visibility v
			WHERE UPPER(TRIM(v.family)) = UPPER(TRIM(c.family))
			LIMIT 1
		), 1) = 1`

func parseControlType(raw string) controlTypeFilter {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "control":
		return controlTypeControl
	case "control enhancement":
		return controlTypeEnhancement
	default:
		return controlTypeAll
	}
}

func controlTypeWhereSQL(controlType controlTypeFilter) string {
	switch controlType {
	case controlTypeControl:
		return ` AND LOWER(c.control_type) = 'control'`
	case controlTypeEnhancement:
		return ` AND LOWER(c.control_type) = 'control enhancement'`
	default:
		return ""
	}
}

func parseReportMode(raw string) reportModeFilter {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "linked":
		return reportModeLinked
	case "unlinked":
		return reportModeUnlinked
	default:
		return reportModeAll
	}
}

func (s *Service) UnlinkedControls(controlType string) ([]ControlSummary, error) {
	return s.listControlsByLinkStatus(parseControlType(controlType), false)
}

func (s *Service) LinkedControls(controlType string) ([]ControlSummary, error) {
	return s.listControlsByLinkStatus(parseControlType(controlType), true)
}

func (s *Service) TotalControls(controlType string) (int, error) {
	return s.totalControlsByFilter(parseControlType(controlType))
}

func (s *Service) ReportData(controlType string, mode string) (ReportData, error) {
	filter := parseControlType(controlType)
	reportMode := parseReportMode(mode)
	unlinkedItems, err := s.listControlsByLinkStatus(filter, false)
	if err != nil {
		return ReportData{}, err
	}
	linkedItems, err := s.listControlsByLinkStatus(filter, true)
	if err != nil {
		return ReportData{}, err
	}
	totalControls, err := s.totalControlsByFilter(filter)
	if err != nil {
		return ReportData{}, err
	}

	items := append([]ControlSummary(nil), unlinkedItems...)
	switch reportMode {
	case reportModeLinked:
		items = linkedItems
	case reportModeAll:
		items = append(items, linkedItems...)
		sort.Slice(items, func(i, j int) bool {
			return controlid.Less(items[i].ControlID, items[j].ControlID)
		})
	default:
		items = unlinkedItems
	}

	return ReportData{
		Items:         items,
		UnlinkedCount: len(unlinkedItems),
		LinkedCount:   len(linkedItems),
		TotalControls: totalControls,
	}, nil
}

func (s *Service) totalControlsByFilter(controlType controlTypeFilter) (int, error) {
	query := `SELECT COUNT(*)
		FROM rcsa_controls c
		WHERE ` + familyVisibleSQL
	// #nosec G202 -- controlTypeWhereSQL returns one of a fixed set of constant SQL fragments.
	query += controlTypeWhereSQL(controlType)

	var total int
	if err := s.db.QueryRow(query).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Service) listControlsByLinkStatus(controlType controlTypeFilter, linked bool) ([]ControlSummary, error) {
	join := `LEFT JOIN (
			SELECT DISTINCT control_id
			FROM security_nfr_control_links
			WHERE matched = 1 AND TRIM(control_id) <> ''
		) linked ON linked.control_id = c.control_id`
	if linked {
		join = `INNER JOIN (
			SELECT DISTINCT control_id
			FROM security_nfr_control_links
			WHERE matched = 1 AND TRIM(control_id) <> ''
		) linked ON linked.control_id = c.control_id`
	}

	query := `SELECT c.control_id, c.name, c.family, c.threats_json,
		c.confidentiality_status, c.integrity_status, c.availability_status,
		COALESCE((
			SELECT ` + s.db.GroupConcatDistinct("TRIM(l.nfr_key) || '|' || TRIM(l.nfr_id)") + `
			FROM security_nfr_control_links l
			WHERE l.matched = 1
				AND TRIM(l.control_id) = TRIM(c.control_id)
				AND TRIM(l.nfr_key) <> ''
				AND TRIM(l.nfr_id) <> ''
		), '') AS linked_nfr_refs
		FROM rcsa_controls c
		` + join + `
		WHERE ` + familyVisibleSQL
	if !linked {
		query += ` AND linked.control_id IS NULL`
	}
	// #nosec G202 -- controlTypeWhereSQL returns one of a fixed set of constant SQL fragments.
	query += controlTypeWhereSQL(controlType)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ControlSummary, 0)
	for rows.Next() {
		var item ControlSummary
		var threatsRaw string
		var linkedNFRRefsRaw string
		if err := rows.Scan(
			&item.ControlID,
			&item.Name,
			&item.Family,
			&threatsRaw,
			&item.Confidentiality,
			&item.Integrity,
			&item.Availability,
			&linkedNFRRefsRaw,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(threatsRaw), &item.Threats); err != nil {
			return nil, err
		}
		item.LinkedSecurityNFRs = splitLinkedNFRRefs(linkedNFRRefsRaw)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		return controlid.Less(out[i].ControlID, out[j].ControlID)
	})
	return out, nil
}

func splitLinkedNFRRefs(raw string) []LinkedSecurityNFR {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]LinkedSecurityNFR, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		key, id, ok := strings.Cut(value, "|")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		id = strings.TrimSpace(id)
		if key == "" || id == "" {
			continue
		}
		dedupeKey := key + "|" + id
		if _, exists := seen[dedupeKey]; exists {
			continue
		}
		seen[dedupeKey] = struct{}{}
		out = append(out, LinkedSecurityNFR{Key: key, ID: id})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Key < out[j].Key
		}
		return out[i].ID < out[j].ID
	})
	return out
}
