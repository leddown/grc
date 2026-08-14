package knowledge

import (
	"fmt"
	"strings"

	"grc/internal/db"
)

// Store reads the catalogs. Every method is a SELECT: this package has no way
// to write, which is the guarantee the read-only token rests on.
type Store struct{ conn *db.Conn }

func NewStore(conn *db.Conn) *Store { return &Store{conn: conn} }

// ---- Security NFRs ----

// NFRs returns the whole catalog, each with the controls its NIST mapping
// resolved to. The catalog is small (order 100), so it is read whole rather
// than paged: a question like "how many mention segmentation" is only
// answerable exactly by looking at all of it.
func (s *Store) NFRs() ([]Item, error) {
	links, err := s.nfrControlLinks()
	if err != nil {
		return nil, err
	}

	rows, err := s.conn.Query(`SELECT record_key, nfr_id, summary, description, nist_mapping,
		additional_details, implementation, domain, issue_type
		FROM security_nfrs ORDER BY record_key`)
	if err != nil {
		return nil, fmt.Errorf("read security NFRs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Item{}
	for rows.Next() {
		var key, id, summary, description, mapping, details, implementation, domain, issueType string
		if err := rows.Scan(&key, &id, &summary, &description, &mapping,
			&details, &implementation, &domain, &issueType); err != nil {
			return nil, err
		}
		body := strings.TrimSpace(strings.Join([]string{description, details, implementation}, "\n\n"))
		out = append(out, Item{
			Kind:    KindNFR,
			Ref:     key,
			Title:   firstNonEmpty(summary, id, key),
			Summary: oneLine(firstNonEmpty(summary, description), 240),
			Body:    body,
			Group:   domain,
			Related: links[key],
			Fields: nonEmptyFields(map[string]string{
				"nfr_id":       id,
				"domain":       domain,
				"issue_type":   issueType,
				"nist_mapping": mapping,
			}),
			URL: "/security-nfrs?key=" + key,
		})
	}
	return out, rows.Err()
}

// nfrControlLinks maps each NFR key to the control IDs its mapping resolved to.
func (s *Store) nfrControlLinks() (map[string][]string, error) {
	rows, err := s.conn.Query(`SELECT nfr_key, control_id FROM security_nfr_control_links
		WHERE matched = 1 AND control_id <> '' ORDER BY nfr_key, control_id`)
	if err != nil {
		return nil, fmt.Errorf("read NFR-control links: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]string{}
	for rows.Next() {
		var key, control string
		if err := rows.Scan(&key, &control); err != nil {
			return nil, err
		}
		out[key] = appendUnique(out[key], control)
	}
	return out, rows.Err()
}

// ---- 800-53 controls ----

func (s *Store) Controls() ([]Item, error) {
	rows, err := s.conn.Query(`SELECT control_id, name, family, control_type, requirements, discussion,
		in_low, in_moderate, in_high, in_privacy
		FROM rcsa_controls ORDER BY control_id`)
	if err != nil {
		return nil, fmt.Errorf("read controls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Item{}
	for rows.Next() {
		var id, name, family, controlType, requirements, discussion string
		var low, moderate, high, privacy int
		if err := rows.Scan(&id, &name, &family, &controlType, &requirements, &discussion,
			&low, &moderate, &high, &privacy); err != nil {
			return nil, err
		}
		out = append(out, Item{
			Kind:    KindControl,
			Ref:     id,
			Title:   name,
			Summary: oneLine(firstNonEmpty(requirements, discussion, name), 240),
			Body:    strings.TrimSpace(requirements + "\n\n" + discussion),
			Group:   family,
			Fields: nonEmptyFields(map[string]string{
				"family":    family,
				"type":      controlType,
				"baselines": baselineList(low, moderate, high, privacy),
			}),
			URL: "/controls?control=" + id,
		})
	}
	return out, rows.Err()
}

func baselineList(low, moderate, high, privacy int) string {
	var out []string
	if low != 0 {
		out = append(out, "low")
	}
	if moderate != 0 {
		out = append(out, "moderate")
	}
	if high != 0 {
		out = append(out, "high")
	}
	if privacy != 0 {
		out = append(out, "privacy")
	}
	return strings.Join(out, ", ")
}

// ---- regulation coverage ----

func (s *Store) Regulations() ([]Item, error) {
	rows, err := s.conn.Query(`SELECT g.id, g.title, g.framework_name, g.source_ref, g.status,
		(SELECT COUNT(*) FROM reg_coverage_sections s WHERE s.regulation_id = g.id),
		(SELECT COALESCE(MAX(v.number), 0) FROM reg_coverage_versions v WHERE v.regulation_id = g.id)
		FROM reg_coverage_regulations g ORDER BY g.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("read regulations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Item{}
	for rows.Next() {
		var id int64
		var title, framework, sourceRef, status string
		var sections, version int
		if err := rows.Scan(&id, &title, &framework, &sourceRef, &status, &sections, &version); err != nil {
			return nil, err
		}
		out = append(out, Item{
			Kind:    KindRegulation,
			Ref:     fmt.Sprintf("%d", id),
			Title:   title,
			Summary: oneLine(fmt.Sprintf("%s · %d sections · %s", framework, sections, status), 240),
			Group:   framework,
			Fields: nonEmptyFields(map[string]string{
				"framework":      framework,
				"source_ref":     sourceRef,
				"status":         status,
				"sections":       fmt.Sprint(sections),
				"report_version": fmt.Sprint(version),
			}),
			URL: fmt.Sprintf("/regulation-coverage/%d", id),
		})
	}
	return out, rows.Err()
}

// RegulationClauses returns every analysed article, carrying the finding that
// was made about it: what it requires, what it mapped to, and what it did not.
// This is the record that answers "where are we exposed under this regulation".
func (s *Store) RegulationClauses() ([]Item, error) {
	mappings, err := s.clauseMappings()
	if err != nil {
		return nil, err
	}

	rows, err := s.conn.Query(`SELECT s.id, s.regulation_id, s.ref, s.label, s.title, s.body, s.category,
		g.title, g.framework_name,
		COALESCE(f.relevant, -1), COALESCE(f.requirement, ''), COALESCE(f.commentary, ''),
		COALESCE(f.gaps, ''), COALESCE(f.confidence, ''), COALESCE(f.id, 0)
		FROM reg_coverage_sections s
		JOIN reg_coverage_regulations g ON g.id = s.regulation_id
		LEFT JOIN reg_coverage_findings f ON f.section_id = s.id
			AND f.revision = (SELECT MAX(f2.revision) FROM reg_coverage_findings f2 WHERE f2.section_id = s.id)
		ORDER BY s.regulation_id DESC, s.position`)
	if err != nil {
		return nil, fmt.Errorf("read regulation clauses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Item{}
	for rows.Next() {
		var sectionID, regulationID, findingID int64
		var ref, label, title, body, category, regTitle, framework string
		var relevant int
		var requirement, commentary, gaps, confidence string
		if err := rows.Scan(&sectionID, &regulationID, &ref, &label, &title, &body, &category, &regTitle, &framework,
			&relevant, &requirement, &commentary, &gaps, &confidence, &findingID); err != nil {
			return nil, err
		}

		fields := map[string]string{
			"regulation": regTitle,
			"framework":  framework,
			"category":   category,
			"confidence": confidence,
		}
		switch relevant {
		case 1:
			fields["security_relevant"] = "yes"
		case 0:
			fields["security_relevant"] = "no"
		default:
			fields["security_relevant"] = "not analysed"
		}
		fields["requirement"] = oneLine(requirement, 600)
		fields["commentary"] = oneLine(commentary, 900)
		fields["gap"] = oneLine(gaps, 400)

		out = append(out, Item{
			Kind:    KindRegulationClause,
			Ref:     ref,
			Title:   strings.TrimSpace(label + " " + title),
			Summary: oneLine(firstNonEmpty(requirement, title, body), 240),
			Body:    body,
			Group:   regTitle,
			Related: mappings[findingID],
			Fields:  nonEmptyFields(fields),
			URL:     fmt.Sprintf("/regulation-coverage/%d#section-%s", regulationID, ref),
		})
	}
	return out, rows.Err()
}

func (s *Store) clauseMappings() (map[int64][]string, error) {
	rows, err := s.conn.Query(`SELECT finding_id, kind, ref FROM reg_coverage_mappings ORDER BY finding_id, kind, ref`)
	if err != nil {
		return nil, fmt.Errorf("read clause mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]string{}
	for rows.Next() {
		var findingID int64
		var kind, ref string
		if err := rows.Scan(&findingID, &kind, &ref); err != nil {
			return nil, err
		}
		out[findingID] = appendUnique(out[findingID], kind+":"+ref)
	}
	return out, rows.Err()
}

// ---- policies ----

func (s *Store) Policies() ([]Item, error) {
	rows, err := s.conn.Query(`SELECT id, reference, title, doc_type, status, summary, owner_role,
		classification, next_review_date, client_name
		FROM policy_documents ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("read policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Item{}
	for rows.Next() {
		var id int64
		var reference, title, docType, status, summary, owner, classification, review, client string
		if err := rows.Scan(&id, &reference, &title, &docType, &status, &summary, &owner,
			&classification, &review, &client); err != nil {
			return nil, err
		}
		out = append(out, Item{
			Kind:    KindPolicy,
			Ref:     firstNonEmpty(reference, fmt.Sprint(id)),
			Title:   title,
			Summary: oneLine(firstNonEmpty(summary, title), 240),
			Body:    summary,
			Group:   docType,
			Fields: nonEmptyFields(map[string]string{
				"status":         status,
				"owner_role":     owner,
				"classification": classification,
				"next_review":    review,
				"client":         client,
				"document_id":    fmt.Sprint(id),
			}),
			URL: fmt.Sprintf("/policies?document=%d", id),
		})
	}
	return out, rows.Err()
}

// PolicyClauses returns policy sections with the controls each claims to
// satisfy, which is what makes "which policy covers AC-2?" answerable.
func (s *Store) PolicyClauses() ([]Item, error) {
	claims, err := s.policyClaims()
	if err != nil {
		return nil, err
	}

	rows, err := s.conn.Query(`SELECT s.id, s.heading, s.body, s.section_kind, s.provenance,
		d.id, d.title, d.reference, d.status
		FROM policy_sections s
		JOIN policy_documents d ON d.id = s.document_id
		ORDER BY d.id DESC, s.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("read policy clauses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Item{}
	for rows.Next() {
		var sectionID, documentID int64
		var heading, body, kind, provenance, title, reference, status string
		if err := rows.Scan(&sectionID, &heading, &body, &kind, &provenance,
			&documentID, &title, &reference, &status); err != nil {
			return nil, err
		}
		out = append(out, Item{
			Kind:    KindPolicyClause,
			Ref:     fmt.Sprintf("%s#%d", firstNonEmpty(reference, fmt.Sprint(documentID)), sectionID),
			Title:   firstNonEmpty(heading, title),
			Summary: oneLine(body, 240),
			Body:    body,
			Group:   title,
			Related: claims[sectionID],
			Fields: nonEmptyFields(map[string]string{
				"policy":       title,
				"policy_ref":   reference,
				"status":       status,
				"section_kind": kind,
				"provenance":   provenance,
			}),
			URL: fmt.Sprintf("/policies?document=%d", documentID),
		})
	}
	return out, rows.Err()
}

func (s *Store) policyClaims() (map[int64][]string, error) {
	rows, err := s.conn.Query(`SELECT section_id, control_id, coverage FROM policy_section_controls
		ORDER BY section_id, control_id`)
	if err != nil {
		return nil, fmt.Errorf("read policy control claims: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]string{}
	for rows.Next() {
		var sectionID int64
		var control, coverage string
		if err := rows.Scan(&sectionID, &control, &coverage); err != nil {
			return nil, err
		}
		label := control
		if coverage != "" && coverage != "full" {
			label += " (" + coverage + ")"
		}
		out[sectionID] = appendUnique(out[sectionID], label)
	}
	return out, rows.Err()
}

// ---- risk register ----

func (s *Store) Risks() ([]Item, error) {
	rows, err := s.conn.Query(`SELECT risk_id, title, business_unit, asset, threat_source, vulnerability,
		current_controls, response_strategy, response_action, owner, status,
		inherent_score, residual_score, target_date, notes
		FROM security_risk_register ORDER BY residual_score DESC, risk_id`)
	if err != nil {
		return nil, fmt.Errorf("read risk register: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Item{}
	for rows.Next() {
		var riskID, title, unit, asset, threat, vulnerability, controls, strategy, action, owner, status, target, notes string
		var inherent, residual int
		if err := rows.Scan(&riskID, &title, &unit, &asset, &threat, &vulnerability,
			&controls, &strategy, &action, &owner, &status,
			&inherent, &residual, &target, &notes); err != nil {
			return nil, err
		}
		body := strings.TrimSpace(strings.Join([]string{
			"Threat: " + threat, "Vulnerability: " + vulnerability,
			"Current controls: " + controls, "Response: " + action, notes,
		}, "\n"))
		out = append(out, Item{
			Kind:    KindRisk,
			Ref:     riskID,
			Title:   title,
			Summary: oneLine(firstNonEmpty(vulnerability, title), 240),
			Body:    body,
			Group:   status,
			Fields: nonEmptyFields(map[string]string{
				"status":         status,
				"owner":          owner,
				"business_unit":  unit,
				"asset":          asset,
				"strategy":       strategy,
				"inherent_score": fmt.Sprint(inherent),
				"residual_score": fmt.Sprint(residual),
				"target_date":    target,
			}),
			URL: "/risk-register?risk=" + riskID,
		})
	}
	return out, rows.Err()
}

func appendUnique(list []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return list
	}
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func nonEmptyFields(fields map[string]string) map[string]string {
	for key, value := range fields {
		if strings.TrimSpace(value) == "" {
			delete(fields, key)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}
