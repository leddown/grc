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

// Exercises returns the crisis exercises this installation has designed or run.
//
// The body is written for a model to reason over rather than for a page to
// render: the scenario, what the exercise set out to test, how much of it was
// played, and — the part no other corpus carries — whether the notification
// clocks were met. An agent asked whether this institution has ever exercised
// its board is answering from evidence here or from imagination everywhere
// else.
func (s *Store) Exercises() ([]Item, error) {
	details, err := s.exerciseDetails()
	if err != nil {
		return nil, err
	}

	rows, err := s.conn.Query(`SELECT e.id, e.reference, e.title, e.summary, e.format, e.audience,
		e.entity_name, e.jurisdiction, e.status, e.scheduled_for, e.threat_actor, e.threat_narrative,
		e.critical_functions,
		(SELECT COUNT(*) FROM crisis_ex_injects i WHERE i.exercise_id = e.id),
		(SELECT COUNT(*) FROM crisis_ex_responses r WHERE r.exercise_id = e.id AND r.outcome <> 'not_played'),
		(SELECT COUNT(*) FROM crisis_ex_findings f WHERE f.exercise_id = e.id),
		(SELECT COUNT(*) FROM crisis_ex_clocks c WHERE c.exercise_id = e.id AND c.status = 'met'),
		(SELECT COUNT(*) FROM crisis_ex_clocks c WHERE c.exercise_id = e.id AND c.status = 'missed')
		FROM crisis_ex_exercises e ORDER BY e.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("read crisis exercises: %w", err)
	}
	defer func() { _ = rows.Close() }()

	objectives, err := s.exerciseObjectives()
	if err != nil {
		return nil, err
	}
	citations, err := s.exerciseCitations()
	if err != nil {
		return nil, err
	}

	out := []Item{}
	for rows.Next() {
		var id int64
		var reference, title, summary, format, audience, entity, jurisdiction, status string
		var scheduled, actor, narrative, functions string
		var injects, played, findings, clocksMet, clocksMissed int
		if err := rows.Scan(&id, &reference, &title, &summary, &format, &audience,
			&entity, &jurisdiction, &status, &scheduled, &actor, &narrative, &functions,
			&injects, &played, &findings, &clocksMet, &clocksMissed); err != nil {
			return nil, err
		}

		parts := []string{summary}
		if actor != "" {
			parts = append(parts, "Adversary: "+actor)
		}
		if narrative != "" {
			parts = append(parts, "Scenario: "+narrative)
		}
		if functions != "" {
			parts = append(parts, "Critical functions in scope: "+strings.ReplaceAll(functions, "\n", "; "))
		}
		if objs := objectives[id]; len(objs) > 0 {
			parts = append(parts, "Objectives: "+strings.Join(objs, " | "))
		}
		parts = append(parts, fmt.Sprintf("Delivery: %d of %d injects played. %d notification clocks met, %d missed. %d findings.",
			played, injects, clocksMet, clocksMissed, findings))
		if detail := details[id]; detail != "" {
			parts = append(parts, detail)
		}

		out = append(out, Item{
			Kind:    KindExercise,
			Ref:     reference,
			Title:   title,
			Summary: oneLine(firstNonEmpty(summary, title), 240),
			Body:    strings.TrimSpace(strings.Join(parts, "\n")),
			Group:   status,
			Related: citations[id],
			Fields: nonEmptyFields(map[string]string{
				"format":         format,
				"audience":       audience,
				"entity":         entity,
				"jurisdiction":   jurisdiction,
				"status":         status,
				"scheduled_for":  scheduled,
				"injects":        fmt.Sprint(injects),
				"injects_played": fmt.Sprint(played),
				"findings":       fmt.Sprint(findings),
				"clocks_met":     fmt.Sprint(clocksMet),
				"clocks_missed":  fmt.Sprint(clocksMissed),
			}),
			URL: fmt.Sprintf("/crisis-exercises/%d", id),
		})
	}
	return out, rows.Err()
}

// exerciseDetails renders, per exercise, what the summary leaves out: the phases
// and injects with what happened to each, the decisions, the classification,
// the notification clocks, the findings and who took part. Someone building an
// exercise asks an agent about exactly this — which phase has no decision
// point, whether an inject gives the team enough to classify on — and a count
// of injects answers none of it.
func (s *Store) exerciseDetails() (map[int64]string, error) {
	lines := map[int64][]string{}
	for _, load := range []func(map[int64][]string) error{
		s.exerciseRunSheet, s.exerciseDecisions, s.exerciseClassification,
		s.exerciseClocks, s.exerciseFindingLines, s.exerciseParticipants,
	} {
		if err := load(lines); err != nil {
			return nil, err
		}
	}
	out := make(map[int64]string, len(lines))
	for id, l := range lines {
		out[id] = strings.Join(l, "\n")
	}
	return out, nil
}

func (s *Store) exerciseRunSheet(lines map[int64][]string) error {
	rows, err := s.conn.Query(`SELECT p.exercise_id, p.id, p.name, p.status, p.offset_minutes,
		p.duration_minutes, p.lead_role, p.purpose,
		CASE WHEN i.id IS NULL THEN 0 ELSE 1 END,
		COALESCE(i.code, ''), COALESCE(i.offset_minutes, 0), COALESCE(i.inject_type, ''),
		COALESCE(i.channel, ''), COALESCE(i.from_actor, ''), COALESCE(i.to_actor, ''),
		COALESCE(i.title, ''), COALESCE(i.body, ''), COALESCE(i.expected_actions, ''),
		COALESCE(i.expected_decision, ''), COALESCE(r.outcome, ''), COALESCE(r.responded_offset, -1),
		COALESCE(r.actual_actions, ''), COALESCE(r.observations, '')
		FROM crisis_ex_phases p
		LEFT JOIN crisis_ex_injects i ON i.phase_id = p.id AND i.exercise_id = p.exercise_id
		LEFT JOIN crisis_ex_responses r ON r.inject_id = i.id
		ORDER BY p.exercise_id, p.ordinal, p.id, i.offset_minutes, i.ordinal, i.id`)
	if err != nil {
		return fmt.Errorf("read exercise phases and injects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	lastPhase := int64(0)
	for rows.Next() {
		var exerciseID, phaseID int64
		var offset, duration, hasInject, injectOffset, respondedOffset int
		var name, status, lead, purpose, code, injectType, channel, from, to string
		var title, body, expected, decision, outcome, actual, observations string
		if err := rows.Scan(&exerciseID, &phaseID, &name, &status, &offset, &duration, &lead, &purpose,
			&hasInject, &code, &injectOffset, &injectType, &channel, &from, &to, &title, &body,
			&expected, &decision, &outcome, &respondedOffset, &actual, &observations); err != nil {
			return err
		}
		if phaseID != lastPhase {
			lastPhase = phaseID
			line := fmt.Sprintf("Phase %s · %s (%s, %d min", exerciseOffset(offset), name, status, duration)
			if lead != "" {
				line += ", led by " + strings.ReplaceAll(lead, "_", " ")
			}
			line += ")"
			if text := flatText(purpose); text != "" {
				line += ": " + text
			}
			lines[exerciseID] = append(lines[exerciseID], line)
		}
		if hasInject == 0 {
			continue
		}

		line := fmt.Sprintf("  Inject %s at %s, %s", code, exerciseOffset(injectOffset), injectType)
		if channel != "" {
			line += " by " + strings.ReplaceAll(channel, "_", " ")
		}
		if from != "" || to != "" {
			line += ", " + orDash(from) + " → " + orDash(to)
		}
		if title != "" {
			line += ": " + title
		}
		line += "."
		for _, field := range [][2]string{{"Message", body}, {"Expected", expected}, {"Decision sought", decision}} {
			if text := flatText(field[1]); text != "" {
				line += " " + field[0] + ": " + text
			}
		}
		if outcome == "" || outcome == "not_played" {
			line += " Not played yet."
		} else {
			line += " Played: " + strings.ReplaceAll(outcome, "_", " ")
			if respondedOffset >= 0 {
				line += " at " + exerciseOffset(respondedOffset)
			}
			line += "."
			if text := flatText(actual); text != "" {
				line += " Observed: " + text
			}
			if text := flatText(observations); text != "" {
				line += " Evaluator: " + text
			}
		}
		lines[exerciseID] = append(lines[exerciseID], line)
	}
	return rows.Err()
}

func (s *Store) exerciseDecisions(lines map[int64][]string) error {
	rows, err := s.conn.Query(`SELECT exercise_id, offset_minutes, title, decision, rationale,
		made_by, role, authority, reversible, regulatory_implication
		FROM crisis_ex_decisions ORDER BY exercise_id, offset_minutes, id`)
	if err != nil {
		return fmt.Errorf("read exercise decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var exerciseID int64
		var offset, reversible int
		var title, decision, rationale, madeBy, role, authority, regulatory string
		if err := rows.Scan(&exerciseID, &offset, &title, &decision, &rationale,
			&madeBy, &role, &authority, &reversible, &regulatory); err != nil {
			return err
		}
		line := fmt.Sprintf("Decision at %s: %s", exerciseOffset(offset), title)
		if text := flatText(decision); text != "" {
			line += " — " + text
		}
		line += "."
		if text := flatText(rationale); text != "" {
			line += " Rationale: " + text
		}
		who := strings.TrimSpace(madeBy)
		if role != "" {
			who = strings.TrimSpace(who + " (" + strings.ReplaceAll(role, "_", " ") + ")")
		}
		if who != "" {
			line += " Made by " + who + "."
		}
		if text := flatText(authority); text != "" {
			line += " Authority: " + text + "."
		}
		if reversible == 0 {
			line += " Irreversible."
		}
		if text := flatText(regulatory); text != "" {
			line += " Regulatory implication: " + text
		}
		lines[exerciseID] = append(lines[exerciseID], line)
	}
	return rows.Err()
}

func (s *Store) exerciseClassification(lines map[int64][]string) error {
	rows, err := s.conn.Query(`SELECT exercise_id, aware_offset, classified_offset, major,
		critical_services_affected, personal_data_breach, nis2_significant, rationale, team_verdict
		FROM crisis_ex_classification ORDER BY exercise_id`)
	if err != nil {
		return fmt.Errorf("read exercise classification: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var exerciseID int64
		var aware, classified, major, critical, breach, nis2 int
		var rationale, verdict string
		if err := rows.Scan(&exerciseID, &aware, &classified, &major,
			&critical, &breach, &nis2, &rationale, &verdict); err != nil {
			return err
		}
		if classified <= 0 && critical == 0 && strings.TrimSpace(rationale) == "" {
			continue
		}
		line := fmt.Sprintf("Classification: aware at %s, classified at %s, ", exerciseOffset(aware), exerciseOffset(classified))
		if major != 0 {
			line += "a major incident."
		} else {
			line += "not major."
		}
		if critical != 0 {
			line += " Critical services affected."
		}
		if breach != 0 {
			line += " A personal data breach."
		}
		if nis2 != 0 {
			line += " Significant under NIS2."
		}
		if text := flatText(rationale); text != "" {
			line += " Rationale: " + text
		}
		if text := flatText(verdict); text != "" {
			line += " The team's verdict: " + text
		}
		lines[exerciseID] = append(lines[exerciseID], line)
	}
	return rows.Err()
}

func (s *Store) exerciseClocks(lines map[int64][]string) error {
	rows, err := s.conn.Query(`SELECT exercise_id, regime, authority, label, due_offset, actual_offset, status
		FROM crisis_ex_clocks ORDER BY exercise_id, ordinal, id`)
	if err != nil {
		return fmt.Errorf("read exercise clocks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var exerciseID int64
		var due, actual int
		var regime, authority, label, status string
		if err := rows.Scan(&exerciseID, &regime, &authority, &label, &due, &actual, &status); err != nil {
			return err
		}
		line := "Clock: " + firstNonEmpty(label, regime)
		if label != "" && regime != "" {
			line += " (" + regime + ")"
		}
		if authority != "" {
			line += " to " + authority
		}
		line += fmt.Sprintf(", due %s, %s", exerciseOffset(due), status)
		if actual >= 0 {
			line += " at " + exerciseOffset(actual)
		}
		lines[exerciseID] = append(lines[exerciseID], line+".")
	}
	return rows.Err()
}

func (s *Store) exerciseFindingLines(lines map[int64][]string) error {
	rows, err := s.conn.Query(`SELECT exercise_id, code, severity, title, status
		FROM crisis_ex_findings ORDER BY exercise_id, ordinal, id`)
	if err != nil {
		return fmt.Errorf("read exercise findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var exerciseID int64
		var code, severity, title, status string
		if err := rows.Scan(&exerciseID, &code, &severity, &title, &status); err != nil {
			return err
		}
		lines[exerciseID] = append(lines[exerciseID],
			fmt.Sprintf("Finding %s [%s]: %s (%s).", code, severity, title, status))
	}
	return rows.Err()
}

// exerciseParticipants names who takes part, and in what role. Contact details
// are left out: nothing an agent is asked about an exercise needs them.
func (s *Store) exerciseParticipants(lines map[int64][]string) error {
	rows, err := s.conn.Query(`SELECT exercise_id, name, role_key, org, player
		FROM crisis_ex_participants ORDER BY exercise_id, id`)
	if err != nil {
		return fmt.Errorf("read exercise participants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	people := map[int64][]string{}
	for rows.Next() {
		var exerciseID int64
		var player int
		var name, role, org string
		if err := rows.Scan(&exerciseID, &name, &role, &org, &player); err != nil {
			return err
		}
		person := firstNonEmpty(name, "unnamed")
		if role != "" {
			person += ", " + strings.ReplaceAll(role, "_", " ")
		}
		if org != "" {
			person += ", " + org
		}
		if player == 0 {
			person += " (not playing)"
		}
		people[exerciseID] = append(people[exerciseID], person)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for id, list := range people {
		lines[id] = append(lines[id], "Participants: "+strings.Join(list, "; ")+".")
	}
	return nil
}

// exerciseOffset renders minutes on the exercise clock the way the exercise
// pages do.
func exerciseOffset(minutes int) string {
	if minutes < 0 {
		return "—"
	}
	days, hours, mins := minutes/1440, (minutes%1440)/60, minutes%60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("T+%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("T+%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("T+%dh %02dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("T+%dh", hours)
	default:
		return fmt.Sprintf("T+%dm", mins)
	}
}

func flatText(s string) string { return strings.Join(strings.Fields(s), " ") }

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ExerciseFindings returns the gaps exercises have exposed, which is the part
// worth searching on its own: "what have we found about escalation" is a
// different question from "what exercises have we run".
func (s *Store) ExerciseFindings() ([]Item, error) {
	rows, err := s.conn.Query(`SELECT f.id, f.code, f.title, f.category, f.severity, f.description,
		f.root_cause, f.recommendation, f.owner, f.due_date, f.status, f.risk_ref,
		e.id, e.reference, e.title, COALESCE(p.phase_key, '')
		FROM crisis_ex_findings f
		JOIN crisis_ex_exercises e ON e.id = f.exercise_id
		LEFT JOIN crisis_ex_phases p ON p.id = f.phase_id
		ORDER BY f.exercise_id DESC, f.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("read exercise findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	citations, err := s.findingCitations()
	if err != nil {
		return nil, err
	}

	out := []Item{}
	for rows.Next() {
		var findingID, exerciseID int64
		var code, title, category, severity, description, rootCause, recommendation string
		var owner, dueDate, status, riskRef, exerciseRef, exerciseTitle, phaseKey string
		if err := rows.Scan(&findingID, &code, &title, &category, &severity, &description,
			&rootCause, &recommendation, &owner, &dueDate, &status, &riskRef,
			&exerciseID, &exerciseRef, &exerciseTitle, &phaseKey); err != nil {
			return nil, err
		}

		body := strings.TrimSpace(strings.Join([]string{
			description,
			"Root cause: " + rootCause,
			"Recommendation: " + recommendation,
			"Found in " + exerciseRef + " (" + exerciseTitle + ")",
		}, "\n"))

		out = append(out, Item{
			Kind:    KindExerciseFinding,
			Ref:     exerciseRef + "/" + code,
			Title:   title,
			Summary: oneLine(firstNonEmpty(description, title), 240),
			Body:    body,
			Group:   severity,
			Related: citations[findingID],
			Fields: nonEmptyFields(map[string]string{
				"severity": severity,
				"category": category,
				"status":   status,
				"owner":    owner,
				"due_date": dueDate,
				"risk_ref": riskRef,
				"phase":    phaseKey,
				"exercise": exerciseRef,
			}),
			URL: fmt.Sprintf("/crisis-exercises/%d#findings", exerciseID),
		})
	}
	return out, rows.Err()
}

func (s *Store) exerciseObjectives() (map[int64][]string, error) {
	rows, err := s.conn.Query(`SELECT exercise_id, code, text, rating
		FROM crisis_ex_objectives ORDER BY exercise_id, ordinal`)
	if err != nil {
		return nil, fmt.Errorf("read exercise objectives: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var code, text, rating string
		if err := rows.Scan(&id, &code, &text, &rating); err != nil {
			return nil, err
		}
		out[id] = append(out[id], code+": "+text+" ["+rating+"]")
	}
	return out, rows.Err()
}

// exerciseCitations collects every reference an exercise makes, which is what
// lets an agent answer "which of our controls have we actually exercised".
func (s *Store) exerciseCitations() (map[int64][]string, error) {
	rows, err := s.conn.Query(`SELECT DISTINCT exercise_id, ref_kind, ref
		FROM crisis_ex_references WHERE ref <> '' ORDER BY exercise_id, ref_kind, ref`)
	if err != nil {
		return nil, fmt.Errorf("read exercise references: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var kind, ref string
		if err := rows.Scan(&id, &kind, &ref); err != nil {
			return nil, err
		}
		out[id] = appendUnique(out[id], kind+":"+ref)
	}
	return out, rows.Err()
}

func (s *Store) findingCitations() (map[int64][]string, error) {
	rows, err := s.conn.Query(`SELECT owner_id, ref_kind, ref FROM crisis_ex_references
		WHERE owner_kind = 'finding' AND ref <> '' ORDER BY owner_id`)
	if err != nil {
		return nil, fmt.Errorf("read finding references: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var kind, ref string
		if err := rows.Scan(&id, &kind, &ref); err != nil {
			return nil, err
		}
		out[id] = appendUnique(out[id], kind+":"+ref)
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
