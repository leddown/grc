package policydocs

import (
	"sort"
	"strings"
)

// familyVisibleSQL mirrors internal/reports: a control whose family has been
// switched off in the Family Filters page is out of scope everywhere, and a
// coverage report that ignored that would count controls the practice has
// already declared irrelevant.
const familyVisibleSQL = `COALESCE((
		SELECT v.enabled
		FROM control_family_visibility v
		WHERE UPPER(TRIM(v.family)) = UPPER(TRIM(c.family))
		LIMIT 1
	), 1) = 1`

// controlRefColumns joins each mapping to the catalog. The LEFT JOIN is
// deliberate: a mapping whose control has since left the catalog must still be
// returned, flagged unknown, rather than silently disappearing from the list.
const controlRefColumns = `
	r.id, r.section_id, r.control_id, r.framework, r.coverage, r.note, r.created_at,
	COALESCE(c.name, ''), COALESCE(c.family, ''), COALESCE(c.control_type, ''),
	COALESCE(s.heading, ''), COALESCE(s.document_id, 0),
	CASE WHEN c.control_id IS NULL THEN 0 ELSE 1 END AS known`

func (r *SQLiteRepository) ListControlRefsForSection(sectionID int64) ([]ControlRef, error) {
	return r.queryControlRefs(`SELECT `+controlRefColumns+`
		FROM policy_section_controls r
		JOIN policy_sections s ON s.id = r.section_id
		LEFT JOIN rcsa_controls c ON UPPER(TRIM(c.control_id)) = UPPER(TRIM(r.control_id))
		WHERE r.section_id = ?
		ORDER BY UPPER(r.control_id) ASC`, sectionID)
}

func (r *SQLiteRepository) ListControlRefsForDocument(documentID int64) ([]ControlRef, error) {
	return r.queryControlRefs(`SELECT `+controlRefColumns+`
		FROM policy_section_controls r
		JOIN policy_sections s ON s.id = r.section_id
		LEFT JOIN rcsa_controls c ON UPPER(TRIM(c.control_id)) = UPPER(TRIM(r.control_id))
		WHERE s.document_id = ?
		ORDER BY s.ordinal ASC, UPPER(r.control_id) ASC`, documentID)
}

func (r *SQLiteRepository) GetControlRef(id int64) (ControlRef, error) {
	rows, err := r.queryControlRefs(`SELECT `+controlRefColumns+`
		FROM policy_section_controls r
		JOIN policy_sections s ON s.id = r.section_id
		LEFT JOIN rcsa_controls c ON UPPER(TRIM(c.control_id)) = UPPER(TRIM(r.control_id))
		WHERE r.id = ?`, id)
	if err != nil {
		return ControlRef{}, err
	}
	if len(rows) == 0 {
		return ControlRef{}, errNoRows()
	}
	return rows[0], nil
}

func (r *SQLiteRepository) CreateControlRef(ref ControlRef) (ControlRef, error) {
	id, err := r.db.Insert(`INSERT INTO policy_section_controls
		(section_id, control_id, framework, coverage, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ref.SectionID, ref.ControlID, ref.Framework, ref.Coverage, ref.Note, ref.CreatedAt)
	if err != nil {
		return ControlRef{}, err
	}
	return r.GetControlRef(id)
}

func (r *SQLiteRepository) DeleteControlRef(id int64) error {
	res, err := r.db.Exec(`DELETE FROM policy_section_controls WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ControlRefExists reports whether the section already claims that control, so
// the service can return a readable message instead of a unique-index error.
func (r *SQLiteRepository) ControlRefExists(sectionID int64, controlID string) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM policy_section_controls
		WHERE section_id = ? AND UPPER(TRIM(control_id)) = ?`,
		sectionID, strings.ToUpper(strings.TrimSpace(controlID))).Scan(&n)
	return n > 0, err
}

// LookupControl resolves a control id in the catalog.
func (r *SQLiteRepository) LookupControl(controlID string) (ControlOption, error) {
	var opt ControlOption
	err := r.db.QueryRow(`SELECT control_id, name, family, control_type
		FROM rcsa_controls WHERE UPPER(TRIM(control_id)) = ?`,
		strings.ToUpper(strings.TrimSpace(controlID))).Scan(
		&opt.ControlID, &opt.Name, &opt.Family, &opt.ControlType)
	return opt, err
}

// SearchControls backs the editor's control picker.
func (r *SQLiteRepository) SearchControls(query string, limit int) ([]ControlOption, error) {
	sql := `SELECT c.control_id, c.name, c.family, c.control_type,
			TRIM(
				CASE WHEN c.in_low = 1 THEN 'Low ' ELSE '' END ||
				CASE WHEN c.in_moderate = 1 THEN 'Moderate ' ELSE '' END ||
				CASE WHEN c.in_high = 1 THEN 'High ' ELSE '' END ||
				CASE WHEN c.in_privacy = 1 THEN 'Privacy' ELSE '' END
			) AS baselines
		FROM rcsa_controls c
		WHERE ` + familyVisibleSQL
	args := make([]any, 0, 3)
	if q := strings.TrimSpace(query); q != "" {
		sql += ` AND (UPPER(c.control_id) LIKE ? OR UPPER(c.name) LIKE ? OR UPPER(c.family) LIKE ?)`
		like := "%" + strings.ToUpper(q) + "%"
		args = append(args, like, like, like)
	}
	sql += ` ORDER BY UPPER(c.control_id) ASC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ControlOption, 0, limit)
	for rows.Next() {
		var opt ControlOption
		if err := rows.Scan(&opt.ControlID, &opt.Name, &opt.Family, &opt.ControlType, &opt.Baselines); err != nil {
			return nil, err
		}
		out = append(out, opt)
	}
	return out, rows.Err()
}

// Coverage builds the matrix: every in-scope catalog control, plus whatever
// claims it. It runs as two queries rather than one big outer join because the
// claim set is sparse — most controls have none — and stitching in Go keeps the
// "uncovered" rows cheap.
func (r *SQLiteRepository) Coverage(f CoverageFilter) (CoverageReport, error) {
	report := CoverageReport{
		Baseline: f.Baseline,
		Family:   f.Family,
		ClientID: f.ClientID,
		Rows:     make([]CoverageRow, 0),
		Orphans:  make([]ControlRef, 0),
	}

	controlSQL := `SELECT c.control_id, c.name, c.family, c.control_type
		FROM rcsa_controls c
		WHERE ` + familyVisibleSQL
	args := make([]any, 0, 2)

	switch strings.ToLower(strings.TrimSpace(f.Baseline)) {
	case "low":
		controlSQL += ` AND c.in_low = 1`
	case "moderate":
		controlSQL += ` AND c.in_moderate = 1`
	case "high":
		controlSQL += ` AND c.in_high = 1`
	case "privacy":
		controlSQL += ` AND c.in_privacy = 1`
	}
	if fam := strings.TrimSpace(f.Family); fam != "" {
		controlSQL += ` AND UPPER(TRIM(c.family)) = ?`
		args = append(args, strings.ToUpper(fam))
	}
	if !f.IncludeEnhancements {
		controlSQL += ` AND LOWER(c.control_type) <> 'control enhancement'`
	}
	controlSQL += ` ORDER BY UPPER(c.control_id) ASC`

	rows, err := r.db.Query(controlSQL, args...)
	if err != nil {
		return report, err
	}
	defer rows.Close()

	ordered := make([]CoverageRow, 0)
	index := make(map[string]int)
	for rows.Next() {
		var row CoverageRow
		if err := rows.Scan(&row.ControlID, &row.Name, &row.Family, &row.ControlType); err != nil {
			return report, err
		}
		row.Claims = make([]CoverageClaim, 0)
		index[strings.ToUpper(strings.TrimSpace(row.ControlID))] = len(ordered)
		ordered = append(ordered, row)
	}
	if err := rows.Err(); err != nil {
		return report, err
	}

	claims, err := r.coverageClaims(f.ClientID)
	if err != nil {
		return report, err
	}

	// Track claims that matched no in-scope control. A claim on a control that
	// exists but sits outside the filter is simply out of scope; one on a
	// control the catalog does not have at all is an orphan.
	//
	// Resolving orphans needs the full catalog id set, which is a second scan of
	// rcsa_controls — so it is only loaded when at least one claim actually
	// failed to match the in-scope rows. In the common case (every claim lands
	// on an in-scope control) that scan never runs.
	unmatched := make([]coverageEntry, 0)
	for _, entry := range claims {
		key := strings.ToUpper(strings.TrimSpace(entry.controlID))
		if pos, ok := index[key]; ok {
			ordered[pos].Claims = append(ordered[pos].Claims, entry.claim)
			continue
		}
		unmatched = append(unmatched, entry)
	}
	if len(unmatched) > 0 {
		known, err := r.knownControlIDs()
		if err != nil {
			return report, err
		}
		for _, entry := range unmatched {
			if !known[strings.ToUpper(strings.TrimSpace(entry.controlID))] {
				report.Orphans = append(report.Orphans, entry.orphan)
			}
		}
	}

	for i := range ordered {
		ordered[i].Status = coverageStatus(ordered[i].Claims)
		switch ordered[i].Status {
		case CoverageStatusApproved:
			report.Approved++
		case CoverageStatusDraftOnly:
			report.DraftOnly++
		case CoverageStatusSupportingOnly:
			report.SupportingOnly++
		default:
			report.Uncovered++
		}
	}
	report.TotalControls = len(ordered)

	if f.OnlyGaps {
		gaps := make([]CoverageRow, 0, len(ordered))
		for _, row := range ordered {
			if row.Status != CoverageStatusApproved {
				gaps = append(gaps, row)
			}
		}
		ordered = gaps
	}
	report.Rows = ordered

	sort.SliceStable(report.Orphans, func(i, j int) bool {
		return report.Orphans[i].ControlID < report.Orphans[j].ControlID
	})
	return report, nil
}

type coverageEntry struct {
	controlID string
	claim     CoverageClaim
	orphan    ControlRef
}

func (r *SQLiteRepository) coverageClaims(clientID int64) ([]coverageEntry, error) {
	sql := `SELECT r.control_id, r.coverage, r.id, r.framework, r.note, r.created_at,
			d.id, d.reference, d.title, d.status, s.id, s.heading
		FROM policy_section_controls r
		JOIN policy_sections s ON s.id = r.section_id
		JOIN policy_documents d ON d.id = s.document_id
		WHERE d.status <> ?`
	args := []any{StatusRetired}
	if clientID > 0 {
		sql += ` AND d.client_id = ?`
		args = append(args, clientID)
	}
	sql += ` ORDER BY d.id ASC, s.ordinal ASC`

	rows, err := r.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]coverageEntry, 0)
	for rows.Next() {
		var entry coverageEntry
		var claim CoverageClaim
		var ref ControlRef
		if err := rows.Scan(&entry.controlID, &claim.Coverage, &ref.ID, &ref.Framework,
			&ref.Note, &ref.CreatedAt, &claim.DocumentID, &claim.Reference, &claim.Title,
			&claim.DocStatus, &claim.SectionID, &claim.SectionHeading); err != nil {
			return nil, err
		}
		ref.ControlID = entry.controlID
		ref.Coverage = claim.Coverage
		ref.SectionID = claim.SectionID
		ref.SectionHeading = claim.SectionHeading
		ref.DocumentID = claim.DocumentID
		entry.claim = claim
		entry.orphan = ref
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) knownControlIDs() (map[string]bool, error) {
	rows, err := r.db.Query(`SELECT control_id FROM rcsa_controls`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[strings.ToUpper(strings.TrimSpace(id))] = true
	}
	return out, rows.Err()
}

// coverageStatus reduces a control's claims to one status. Approved-and-real
// wins; a control claimed only by drafts, or only as "supporting", is reported
// separately because both read as covered in a spreadsheet and neither survives
// an assessor asking which document says so.
func coverageStatus(claims []CoverageClaim) string {
	if len(claims) == 0 {
		return CoverageStatusUncovered
	}
	sawAsserting := false
	for _, c := range claims {
		if c.Coverage == CoverageSupporting {
			continue
		}
		sawAsserting = true
		if c.DocStatus == StatusApproved {
			return CoverageStatusApproved
		}
	}
	if !sawAsserting {
		return CoverageStatusSupportingOnly
	}
	return CoverageStatusDraftOnly
}

func (r *SQLiteRepository) queryControlRefs(query string, args ...any) ([]ControlRef, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ControlRef, 0)
	for rows.Next() {
		var ref ControlRef
		var known int
		if err := rows.Scan(&ref.ID, &ref.SectionID, &ref.ControlID, &ref.Framework,
			&ref.Coverage, &ref.Note, &ref.CreatedAt, &ref.ControlName, &ref.ControlFamily,
			&ref.ControlType, &ref.SectionHeading, &ref.DocumentID, &known); err != nil {
			return nil, err
		}
		ref.Known = known == 1
		out = append(out, ref)
	}
	return out, rows.Err()
}
