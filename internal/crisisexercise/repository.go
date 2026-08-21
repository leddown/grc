package crisisexercise

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"grc/internal/db"
)

// Repository is the persistence surface. It is an interface so the service can
// be tested without a database: the parts of this module worth testing — the
// classification rule, the clock arithmetic, the scoring, the reference
// resolution — are exactly the parts that must not be exercised only through
// SQL.
type Repository interface {
	CreateExercise(ex Exercise) (Exercise, error)
	UpdateExercise(ex Exercise) (Exercise, error)
	GetExercise(id int64) (Exercise, error)
	ExerciseByReference(reference string) (Exercise, error)
	ListExercises() ([]Exercise, error)
	DeleteExercise(id int64) error

	ReplaceObjectives(exerciseID int64, objectives []Objective) ([]Objective, error)
	ListObjectives(exerciseID int64) ([]Objective, error)
	SaveObjective(o Objective) (Objective, error)

	ReplacePhases(exerciseID int64, phases []Phase) ([]Phase, error)
	ListPhases(exerciseID int64) ([]Phase, error)
	GetPhase(id int64) (Phase, error)
	SavePhase(p Phase) (Phase, error)

	// AppendInjects adds injects to an exercise. Design runs append rather than
	// replace so a hand-written inject is never destroyed by a regeneration.
	AppendInjects(injects []Inject) ([]Inject, error)
	ListInjects(exerciseID int64) ([]Inject, error)
	GetInject(id int64) (Inject, error)
	SaveInject(in Inject) (Inject, error)
	DeleteInject(id int64) error

	SaveResponse(r Response) (Response, error)
	ListResponses(exerciseID int64) ([]Response, error)

	SaveDecision(d Decision) (Decision, error)
	ListDecisions(exerciseID int64) ([]Decision, error)
	DeleteDecision(id int64) error

	GetClassification(exerciseID int64) (Classification, error)
	SaveClassification(c Classification) (Classification, error)

	ReplaceClocks(exerciseID int64, clocks []Clock) ([]Clock, error)
	ListClocks(exerciseID int64) ([]Clock, error)
	SaveClock(c Clock) (Clock, error)

	SaveFinding(f Finding) (Finding, error)
	ListFindings(exerciseID int64) ([]Finding, error)
	DeleteFinding(id int64) error

	ReplaceParticipants(exerciseID int64, participants []Participant) ([]Participant, error)
	ListParticipants(exerciseID int64) ([]Participant, error)

	AddReference(r Reference) (Reference, error)
	ListReferences(exerciseID int64) ([]Reference, error)
	DeleteReference(id int64) error
	// ReplaceReferencesFor swaps the references of one owner, used when a
	// design run regenerates the citations for an inject.
	ReplaceReferencesFor(exerciseID int64, ownerKind string, ownerID int64, refs []Reference) error

	CreateVersion(v Version) (Version, error)
	SaveVersionSnapshot(versionID int64, snapshot string) error
	GetVersion(exerciseID int64, number int) (Version, error)
	LatestVersion(exerciseID int64) (Version, error)
	ListVersions(exerciseID int64) ([]Version, error)

	AppendChat(t ChatTurn) (ChatTurn, error)
	ListChat(exerciseID int64) ([]ChatTurn, error)
	ClearChat(exerciseID int64) error
}

type SQLiteRepository struct {
	conn *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{conn: conn}
}

// ---- exercises ----

// exerciseColumns is shared by every read so a column added in one place cannot
// be forgotten in another.
const exerciseColumns = `e.id, e.reference, e.title, e.summary, e.format, e.kind, e.audience,
	e.entity_name, e.entity_type, e.jurisdiction, e.supervision, e.critical_functions,
	e.threat_actor, e.threat_narrative, e.initial_vector, e.scenario_key,
	e.status, e.tlp, e.scheduled_for, e.duration_minutes, e.started_at, e.ended_at,
	e.facilitator, e.control_team, e.evaluators,
	e.ai_generated, e.model, e.prompt_hash,
	e.created_at, e.created_by, e.updated_at, e.updated_by,
	(SELECT COUNT(*) FROM crisis_ex_injects i WHERE i.exercise_id = e.id),
	(SELECT COUNT(*) FROM crisis_ex_findings f WHERE f.exercise_id = e.id),
	(SELECT COALESCE(MAX(v.number), 0) FROM crisis_ex_versions v WHERE v.exercise_id = e.id)`

func scanExercise(scan func(...any) error) (Exercise, error) {
	var ex Exercise
	var aiGenerated int
	err := scan(&ex.ID, &ex.Reference, &ex.Title, &ex.Summary, &ex.Format, &ex.Kind, &ex.Audience,
		&ex.EntityName, &ex.EntityType, &ex.Jurisdiction, &ex.Supervision, &ex.CriticalFunctions,
		&ex.ThreatActor, &ex.ThreatNarrative, &ex.InitialVector, &ex.ScenarioKey,
		&ex.Status, &ex.TLP, &ex.ScheduledFor, &ex.DurationMinutes, &ex.StartedAt, &ex.EndedAt,
		&ex.Facilitator, &ex.ControlTeam, &ex.Evaluators,
		&aiGenerated, &ex.Model, &ex.PromptHash,
		&ex.CreatedAt, &ex.CreatedBy, &ex.UpdatedAt, &ex.UpdatedBy,
		&ex.InjectCount, &ex.FindingCount, &ex.LatestVersion)
	ex.AIGenerated = aiGenerated != 0
	return ex, err
}

func (r *SQLiteRepository) CreateExercise(ex Exercise) (Exercise, error) {
	const q = `INSERT INTO crisis_ex_exercises
		(reference, title, summary, format, kind, audience, entity_name, entity_type,
		 jurisdiction, supervision, critical_functions, threat_actor, threat_narrative,
		 initial_vector, scenario_key, status, tlp, scheduled_for, duration_minutes,
		 started_at, ended_at, facilitator, control_team, evaluators,
		 ai_generated, model, prompt_hash, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	id, err := r.conn.Insert(q,
		ex.Reference, ex.Title, ex.Summary, ex.Format, ex.Kind, ex.Audience, ex.EntityName, ex.EntityType,
		ex.Jurisdiction, ex.Supervision, ex.CriticalFunctions, ex.ThreatActor, ex.ThreatNarrative,
		ex.InitialVector, ex.ScenarioKey, ex.Status, ex.TLP, ex.ScheduledFor, ex.DurationMinutes,
		ex.StartedAt, ex.EndedAt, ex.Facilitator, ex.ControlTeam, ex.Evaluators,
		boolToInt(ex.AIGenerated), ex.Model, ex.PromptHash, ex.CreatedAt, ex.CreatedBy, ex.UpdatedAt, ex.UpdatedBy)
	if err != nil {
		return Exercise{}, fmt.Errorf("insert exercise: %w", err)
	}
	ex.ID = id
	return ex, nil
}

func (r *SQLiteRepository) UpdateExercise(ex Exercise) (Exercise, error) {
	const q = `UPDATE crisis_ex_exercises SET
		reference = ?, title = ?, summary = ?, format = ?, kind = ?, audience = ?,
		entity_name = ?, entity_type = ?, jurisdiction = ?, supervision = ?, critical_functions = ?,
		threat_actor = ?, threat_narrative = ?, initial_vector = ?, scenario_key = ?,
		status = ?, tlp = ?, scheduled_for = ?, duration_minutes = ?, started_at = ?, ended_at = ?,
		facilitator = ?, control_team = ?, evaluators = ?,
		ai_generated = ?, model = ?, prompt_hash = ?, updated_at = ?, updated_by = ?
		WHERE id = ?`
	result, err := r.conn.Exec(q,
		ex.Reference, ex.Title, ex.Summary, ex.Format, ex.Kind, ex.Audience,
		ex.EntityName, ex.EntityType, ex.Jurisdiction, ex.Supervision, ex.CriticalFunctions,
		ex.ThreatActor, ex.ThreatNarrative, ex.InitialVector, ex.ScenarioKey,
		ex.Status, ex.TLP, ex.ScheduledFor, ex.DurationMinutes, ex.StartedAt, ex.EndedAt,
		ex.Facilitator, ex.ControlTeam, ex.Evaluators,
		boolToInt(ex.AIGenerated), ex.Model, ex.PromptHash, ex.UpdatedAt, ex.UpdatedBy, ex.ID)
	if err != nil {
		return Exercise{}, fmt.Errorf("update exercise: %w", err)
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return Exercise{}, notFound("exercise")
	}
	return r.GetExercise(ex.ID)
}

func (r *SQLiteRepository) GetExercise(id int64) (Exercise, error) {
	row := r.conn.QueryRow(`SELECT `+exerciseColumns+` FROM crisis_ex_exercises e WHERE e.id = ?`, id)
	ex, err := scanExercise(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Exercise{}, notFound("exercise")
	}
	return ex, err
}

func (r *SQLiteRepository) ExerciseByReference(reference string) (Exercise, error) {
	row := r.conn.QueryRow(`SELECT `+exerciseColumns+` FROM crisis_ex_exercises e WHERE e.reference = ?`, reference)
	ex, err := scanExercise(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Exercise{}, notFound("exercise")
	}
	return ex, err
}

func (r *SQLiteRepository) ListExercises() ([]Exercise, error) {
	rows, err := r.conn.Query(`SELECT ` + exerciseColumns + ` FROM crisis_ex_exercises e
		ORDER BY COALESCE(NULLIF(e.scheduled_for, ''), e.created_at) DESC, e.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Exercise{}
	for rows.Next() {
		ex, err := scanExercise(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, ex)
	}
	return out, rows.Err()
}

// DeleteExercise removes the exercise; every child table cascades from it.
func (r *SQLiteRepository) DeleteExercise(id int64) error {
	result, err := r.conn.Exec(`DELETE FROM crisis_ex_exercises WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return notFound("exercise")
	}
	return nil
}

// ---- objectives ----

func (r *SQLiteRepository) ReplaceObjectives(exerciseID int64, objectives []Objective) ([]Objective, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM crisis_ex_objectives WHERE exercise_id = ?`, exerciseID); err != nil {
		return nil, err
	}
	for i := range objectives {
		objectives[i].ExerciseID = exerciseID
		id, err := tx.Insert(`INSERT INTO crisis_ex_objectives
			(exercise_id, ordinal, code, text, capability, success_criteria, rating, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			exerciseID, objectives[i].Ordinal, objectives[i].Code, objectives[i].Text,
			objectives[i].Capability, objectives[i].SuccessCriteria, objectives[i].Rating, objectives[i].Notes)
		if err != nil {
			return nil, fmt.Errorf("insert objective %s: %w", objectives[i].Code, err)
		}
		objectives[i].ID = id
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return objectives, nil
}

func (r *SQLiteRepository) ListObjectives(exerciseID int64) ([]Objective, error) {
	rows, err := r.conn.Query(`SELECT id, exercise_id, ordinal, code, text, capability,
		success_criteria, rating, notes FROM crisis_ex_objectives
		WHERE exercise_id = ? ORDER BY ordinal, id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Objective{}
	for rows.Next() {
		var o Objective
		if err := rows.Scan(&o.ID, &o.ExerciseID, &o.Ordinal, &o.Code, &o.Text,
			&o.Capability, &o.SuccessCriteria, &o.Rating, &o.Notes); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) SaveObjective(o Objective) (Objective, error) {
	if o.ID == 0 {
		id, err := r.conn.Insert(`INSERT INTO crisis_ex_objectives
			(exercise_id, ordinal, code, text, capability, success_criteria, rating, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			o.ExerciseID, o.Ordinal, o.Code, o.Text, o.Capability, o.SuccessCriteria, o.Rating, o.Notes)
		if err != nil {
			return Objective{}, err
		}
		o.ID = id
		return o, nil
	}
	result, err := r.conn.Exec(`UPDATE crisis_ex_objectives SET
		ordinal = ?, code = ?, text = ?, capability = ?, success_criteria = ?, rating = ?, notes = ?
		WHERE id = ?`,
		o.Ordinal, o.Code, o.Text, o.Capability, o.SuccessCriteria, o.Rating, o.Notes, o.ID)
	if err != nil {
		return Objective{}, err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return Objective{}, notFound("objective")
	}
	return o, nil
}

// ---- phases ----

const phaseColumns = `id, exercise_id, ordinal, phase_key, name, purpose, entry_criteria,
	exit_criteria, lead_role, offset_minutes, duration_minutes, status, notes`

func scanPhase(scan func(...any) error) (Phase, error) {
	var p Phase
	err := scan(&p.ID, &p.ExerciseID, &p.Ordinal, &p.Key, &p.Name, &p.Purpose, &p.EntryCriteria,
		&p.ExitCriteria, &p.LeadRole, &p.OffsetMinutes, &p.DurationMinutes, &p.Status, &p.Notes)
	return p, err
}

func (r *SQLiteRepository) ReplacePhases(exerciseID int64, phases []Phase) ([]Phase, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM crisis_ex_phases WHERE exercise_id = ?`, exerciseID); err != nil {
		return nil, err
	}
	for i := range phases {
		phases[i].ExerciseID = exerciseID
		id, err := tx.Insert(`INSERT INTO crisis_ex_phases
			(exercise_id, ordinal, phase_key, name, purpose, entry_criteria, exit_criteria,
			 lead_role, offset_minutes, duration_minutes, status, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			exerciseID, phases[i].Ordinal, phases[i].Key, phases[i].Name, phases[i].Purpose,
			phases[i].EntryCriteria, phases[i].ExitCriteria, phases[i].LeadRole,
			phases[i].OffsetMinutes, phases[i].DurationMinutes, phases[i].Status, phases[i].Notes)
		if err != nil {
			return nil, fmt.Errorf("insert phase %s: %w", phases[i].Key, err)
		}
		phases[i].ID = id
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return phases, nil
}

func (r *SQLiteRepository) ListPhases(exerciseID int64) ([]Phase, error) {
	rows, err := r.conn.Query(`SELECT `+phaseColumns+` FROM crisis_ex_phases
		WHERE exercise_id = ? ORDER BY ordinal, id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Phase{}
	for rows.Next() {
		p, err := scanPhase(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) GetPhase(id int64) (Phase, error) {
	row := r.conn.QueryRow(`SELECT `+phaseColumns+` FROM crisis_ex_phases WHERE id = ?`, id)
	p, err := scanPhase(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Phase{}, notFound("phase")
	}
	return p, err
}

func (r *SQLiteRepository) SavePhase(p Phase) (Phase, error) {
	if p.ID == 0 {
		id, err := r.conn.Insert(`INSERT INTO crisis_ex_phases
			(exercise_id, ordinal, phase_key, name, purpose, entry_criteria, exit_criteria,
			 lead_role, offset_minutes, duration_minutes, status, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ExerciseID, p.Ordinal, p.Key, p.Name, p.Purpose, p.EntryCriteria, p.ExitCriteria,
			p.LeadRole, p.OffsetMinutes, p.DurationMinutes, p.Status, p.Notes)
		if err != nil {
			return Phase{}, err
		}
		p.ID = id
		return p, nil
	}
	result, err := r.conn.Exec(`UPDATE crisis_ex_phases SET
		ordinal = ?, phase_key = ?, name = ?, purpose = ?, entry_criteria = ?, exit_criteria = ?,
		lead_role = ?, offset_minutes = ?, duration_minutes = ?, status = ?, notes = ?
		WHERE id = ?`,
		p.Ordinal, p.Key, p.Name, p.Purpose, p.EntryCriteria, p.ExitCriteria,
		p.LeadRole, p.OffsetMinutes, p.DurationMinutes, p.Status, p.Notes, p.ID)
	if err != nil {
		return Phase{}, err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return Phase{}, notFound("phase")
	}
	return p, nil
}

// ---- injects ----

const injectColumns = `i.id, i.exercise_id, i.phase_id, i.ordinal, i.code, i.offset_minutes,
	i.title, i.body, i.channel, i.from_actor, i.to_actor, i.inject_type,
	i.expected_actions, i.expected_decision, i.decision_owner, i.evaluation_notes, i.difficulty,
	i.ai_generated, i.model, i.prompt_hash, i.confidence, i.created_at,
	COALESCE(p.phase_key, '')`

func scanInject(scan func(...any) error) (Inject, error) {
	var in Inject
	var aiGenerated int
	err := scan(&in.ID, &in.ExerciseID, &in.PhaseID, &in.Ordinal, &in.Code, &in.OffsetMinutes,
		&in.Title, &in.Body, &in.Channel, &in.From, &in.To, &in.Type,
		&in.ExpectedActions, &in.ExpectedDecision, &in.DecisionOwner, &in.EvaluationNotes, &in.Difficulty,
		&aiGenerated, &in.Model, &in.PromptHash, &in.Confidence, &in.CreatedAt, &in.PhaseKey)
	in.AIGenerated = aiGenerated != 0
	return in, err
}

func (r *SQLiteRepository) AppendInjects(injects []Inject) ([]Inject, error) {
	if len(injects) == 0 {
		return injects, nil
	}
	tx, err := r.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	for i := range injects {
		id, err := tx.Insert(`INSERT INTO crisis_ex_injects
			(exercise_id, phase_id, ordinal, code, offset_minutes, title, body, channel,
			 from_actor, to_actor, inject_type, expected_actions, expected_decision,
			 decision_owner, evaluation_notes, difficulty, ai_generated, model, prompt_hash,
			 confidence, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			injects[i].ExerciseID, injects[i].PhaseID, injects[i].Ordinal, injects[i].Code,
			injects[i].OffsetMinutes, injects[i].Title, injects[i].Body, injects[i].Channel,
			injects[i].From, injects[i].To, injects[i].Type, injects[i].ExpectedActions,
			injects[i].ExpectedDecision, injects[i].DecisionOwner, injects[i].EvaluationNotes,
			injects[i].Difficulty, boolToInt(injects[i].AIGenerated), injects[i].Model,
			injects[i].PromptHash, injects[i].Confidence, injects[i].CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert inject %s: %w", injects[i].Code, err)
		}
		injects[i].ID = id
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return injects, nil
}

func (r *SQLiteRepository) ListInjects(exerciseID int64) ([]Inject, error) {
	rows, err := r.conn.Query(`SELECT `+injectColumns+` FROM crisis_ex_injects i
		LEFT JOIN crisis_ex_phases p ON p.id = i.phase_id
		WHERE i.exercise_id = ? ORDER BY i.offset_minutes, i.ordinal, i.id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Inject{}
	for rows.Next() {
		in, err := scanInject(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) GetInject(id int64) (Inject, error) {
	row := r.conn.QueryRow(`SELECT `+injectColumns+` FROM crisis_ex_injects i
		LEFT JOIN crisis_ex_phases p ON p.id = i.phase_id WHERE i.id = ?`, id)
	in, err := scanInject(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Inject{}, notFound("inject")
	}
	return in, err
}

func (r *SQLiteRepository) SaveInject(in Inject) (Inject, error) {
	if in.ID == 0 {
		saved, err := r.AppendInjects([]Inject{in})
		if err != nil {
			return Inject{}, err
		}
		return saved[0], nil
	}
	result, err := r.conn.Exec(`UPDATE crisis_ex_injects SET
		phase_id = ?, ordinal = ?, code = ?, offset_minutes = ?, title = ?, body = ?, channel = ?,
		from_actor = ?, to_actor = ?, inject_type = ?, expected_actions = ?, expected_decision = ?,
		decision_owner = ?, evaluation_notes = ?, difficulty = ?
		WHERE id = ?`,
		in.PhaseID, in.Ordinal, in.Code, in.OffsetMinutes, in.Title, in.Body, in.Channel,
		in.From, in.To, in.Type, in.ExpectedActions, in.ExpectedDecision,
		in.DecisionOwner, in.EvaluationNotes, in.Difficulty, in.ID)
	if err != nil {
		return Inject{}, err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return Inject{}, notFound("inject")
	}
	return r.GetInject(in.ID)
}

func (r *SQLiteRepository) DeleteInject(id int64) error {
	result, err := r.conn.Exec(`DELETE FROM crisis_ex_injects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return notFound("inject")
	}
	return nil
}

// ---- responses ----

// SaveResponse upserts by inject: an inject has one response, and re-recording
// it corrects the record rather than appending a second version of what
// happened.
func (r *SQLiteRepository) SaveResponse(resp Response) (Response, error) {
	var existingID int64
	err := r.conn.QueryRow(`SELECT id FROM crisis_ex_responses WHERE inject_id = ?`, resp.InjectID).Scan(&existingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		id, err := r.conn.Insert(`INSERT INTO crisis_ex_responses
			(exercise_id, inject_id, delivered_at, responded_offset, outcome, actual_actions,
			 observations, evaluator, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			resp.ExerciseID, resp.InjectID, resp.DeliveredAt, resp.RespondedOffset, resp.Outcome,
			resp.ActualActions, resp.Observations, resp.Evaluator, resp.CreatedAt, resp.UpdatedAt)
		if err != nil {
			return Response{}, err
		}
		resp.ID = id
		return resp, nil
	case err != nil:
		return Response{}, err
	}

	resp.ID = existingID
	if _, err := r.conn.Exec(`UPDATE crisis_ex_responses SET
		delivered_at = ?, responded_offset = ?, outcome = ?, actual_actions = ?,
		observations = ?, evaluator = ?, updated_at = ?
		WHERE id = ?`,
		resp.DeliveredAt, resp.RespondedOffset, resp.Outcome, resp.ActualActions,
		resp.Observations, resp.Evaluator, resp.UpdatedAt, existingID); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func (r *SQLiteRepository) ListResponses(exerciseID int64) ([]Response, error) {
	rows, err := r.conn.Query(`SELECT id, exercise_id, inject_id, delivered_at, responded_offset,
		outcome, actual_actions, observations, evaluator, created_at, updated_at
		FROM crisis_ex_responses WHERE exercise_id = ? ORDER BY id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Response{}
	for rows.Next() {
		var resp Response
		if err := rows.Scan(&resp.ID, &resp.ExerciseID, &resp.InjectID, &resp.DeliveredAt,
			&resp.RespondedOffset, &resp.Outcome, &resp.ActualActions, &resp.Observations,
			&resp.Evaluator, &resp.CreatedAt, &resp.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, resp)
	}
	return out, rows.Err()
}

// ---- decisions ----

func (r *SQLiteRepository) SaveDecision(d Decision) (Decision, error) {
	if d.ID == 0 {
		id, err := r.conn.Insert(`INSERT INTO crisis_ex_decisions
			(exercise_id, phase_id, offset_minutes, title, options, decision, rationale,
			 made_by, role, authority, reversible, regulatory_implication, customer_impact, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ExerciseID, d.PhaseID, d.OffsetMinutes, d.Title, d.Options, d.Decision, d.Rationale,
			d.MadeBy, d.Role, d.Authority, boolToInt(d.Reversible), d.RegulatoryImplication,
			d.CustomerImpact, d.CreatedAt)
		if err != nil {
			return Decision{}, err
		}
		d.ID = id
		return d, nil
	}
	result, err := r.conn.Exec(`UPDATE crisis_ex_decisions SET
		phase_id = ?, offset_minutes = ?, title = ?, options = ?, decision = ?, rationale = ?,
		made_by = ?, role = ?, authority = ?, reversible = ?, regulatory_implication = ?,
		customer_impact = ?
		WHERE id = ?`,
		d.PhaseID, d.OffsetMinutes, d.Title, d.Options, d.Decision, d.Rationale,
		d.MadeBy, d.Role, d.Authority, boolToInt(d.Reversible), d.RegulatoryImplication,
		d.CustomerImpact, d.ID)
	if err != nil {
		return Decision{}, err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return Decision{}, notFound("decision")
	}
	return d, nil
}

func (r *SQLiteRepository) ListDecisions(exerciseID int64) ([]Decision, error) {
	rows, err := r.conn.Query(`SELECT id, exercise_id, phase_id, offset_minutes, title, options,
		decision, rationale, made_by, role, authority, reversible, regulatory_implication,
		customer_impact, created_at
		FROM crisis_ex_decisions WHERE exercise_id = ? ORDER BY offset_minutes, id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Decision{}
	for rows.Next() {
		var d Decision
		var reversible int
		if err := rows.Scan(&d.ID, &d.ExerciseID, &d.PhaseID, &d.OffsetMinutes, &d.Title, &d.Options,
			&d.Decision, &d.Rationale, &d.MadeBy, &d.Role, &d.Authority, &reversible,
			&d.RegulatoryImplication, &d.CustomerImpact, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Reversible = reversible != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) DeleteDecision(id int64) error {
	result, err := r.conn.Exec(`DELETE FROM crisis_ex_decisions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return notFound("decision")
	}
	return nil
}

// ---- classification ----

func (r *SQLiteRepository) GetClassification(exerciseID int64) (Classification, error) {
	row := r.conn.QueryRow(`SELECT exercise_id, aware_offset, classified_offset,
		critical_services_affected, clients_affected, clients_material, transactions_affected,
		transactions_material, reputational_impact, reputational_material, downtime_minutes,
		duration_material, geographical_spread, geographical_material, data_losses,
		data_losses_material, economic_impact, economic_material, personal_data_breach,
		nis2_significant, major, rationale, team_verdict, notes, updated_at, updated_by
		FROM crisis_ex_classification WHERE exercise_id = ?`, exerciseID)

	var c Classification
	var critical, clients, transactions, reputational, duration, geographical, dataLosses,
		economic, personalData, nis2, major int
	err := row.Scan(&c.ExerciseID, &c.AwareOffset, &c.ClassifiedOffset,
		&critical, &c.ClientsAffected, &clients, &c.TransactionsAffected,
		&transactions, &c.ReputationalImpact, &reputational, &c.DowntimeMinutes,
		&duration, &c.GeographicalSpread, &geographical, &c.DataLosses,
		&dataLosses, &c.EconomicImpact, &economic, &personalData,
		&nis2, &major, &c.Rationale, &c.TeamVerdict, &c.Notes, &c.UpdatedAt, &c.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		// An exercise that has not reached its classification phase has an
		// empty assessment rather than a missing one, so the page can render
		// the form without a special case.
		return Classification{ExerciseID: exerciseID}, nil
	}
	if err != nil {
		return Classification{}, err
	}
	c.CriticalServicesAffected = critical != 0
	c.ClientsMaterial = clients != 0
	c.TransactionsMaterial = transactions != 0
	c.ReputationalMaterial = reputational != 0
	c.DurationMaterial = duration != 0
	c.GeographicalMaterial = geographical != 0
	c.DataLossesMaterial = dataLosses != 0
	c.EconomicMaterial = economic != 0
	c.PersonalDataBreach = personalData != 0
	c.NIS2Significant = nis2 != 0
	c.Major = major != 0
	return c, nil
}

func (r *SQLiteRepository) SaveClassification(c Classification) (Classification, error) {
	if _, err := r.conn.Exec(`DELETE FROM crisis_ex_classification WHERE exercise_id = ?`, c.ExerciseID); err != nil {
		return Classification{}, err
	}
	if _, err := r.conn.Exec(`INSERT INTO crisis_ex_classification
		(exercise_id, aware_offset, classified_offset, critical_services_affected,
		 clients_affected, clients_material, transactions_affected, transactions_material,
		 reputational_impact, reputational_material, downtime_minutes, duration_material,
		 geographical_spread, geographical_material, data_losses, data_losses_material,
		 economic_impact, economic_material, personal_data_breach, nis2_significant,
		 major, rationale, team_verdict, notes, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ExerciseID, c.AwareOffset, c.ClassifiedOffset, boolToInt(c.CriticalServicesAffected),
		c.ClientsAffected, boolToInt(c.ClientsMaterial), c.TransactionsAffected, boolToInt(c.TransactionsMaterial),
		c.ReputationalImpact, boolToInt(c.ReputationalMaterial), c.DowntimeMinutes, boolToInt(c.DurationMaterial),
		c.GeographicalSpread, boolToInt(c.GeographicalMaterial), c.DataLosses, boolToInt(c.DataLossesMaterial),
		c.EconomicImpact, boolToInt(c.EconomicMaterial), boolToInt(c.PersonalDataBreach),
		boolToInt(c.NIS2Significant), boolToInt(c.Major), c.Rationale, c.TeamVerdict, c.Notes,
		c.UpdatedAt, c.UpdatedBy); err != nil {
		return Classification{}, fmt.Errorf("save classification: %w", err)
	}
	return c, nil
}

// ---- clocks ----

const clockColumns = `id, exercise_id, ordinal, regime, authority, label, basis,
	due_offset, actual_offset, status, evidence, notes, source`

func scanClock(scan func(...any) error) (Clock, error) {
	var c Clock
	err := scan(&c.ID, &c.ExerciseID, &c.Ordinal, &c.Regime, &c.Authority, &c.Label, &c.Basis,
		&c.DueOffset, &c.ActualOffset, &c.Status, &c.Evidence, &c.Notes, &c.Source)
	return c, err
}

// ReplaceClocks rewrites the clock set after a reclassification, preserving the
// evidence a team has already recorded against a regime that survives the
// change. Losing "we notified at T+3h10" because someone corrected the
// downtime figure would destroy the exercise's only record of the notification.
func (r *SQLiteRepository) ReplaceClocks(exerciseID int64, clocks []Clock) ([]Clock, error) {
	existing, err := r.ListClocks(exerciseID)
	if err != nil {
		return nil, err
	}
	prior := make(map[string]Clock, len(existing))
	for _, c := range existing {
		prior[c.Regime] = c
	}

	tx, err := r.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM crisis_ex_clocks WHERE exercise_id = ?`, exerciseID); err != nil {
		return nil, err
	}
	for i := range clocks {
		clocks[i].ExerciseID = exerciseID
		if old, ok := prior[clocks[i].Regime]; ok {
			clocks[i].ActualOffset = old.ActualOffset
			clocks[i].Evidence = old.Evidence
			if clocks[i].Status != ClockNotApplicable {
				clocks[i] = ScoreClock(clocks[i], old.ActualOffset, false)
			}
		}
		id, err := tx.Insert(`INSERT INTO crisis_ex_clocks
			(exercise_id, ordinal, regime, authority, label, basis, due_offset, actual_offset,
			 status, evidence, notes, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			exerciseID, clocks[i].Ordinal, clocks[i].Regime, clocks[i].Authority, clocks[i].Label,
			clocks[i].Basis, clocks[i].DueOffset, clocks[i].ActualOffset, clocks[i].Status,
			clocks[i].Evidence, clocks[i].Notes, clocks[i].Source)
		if err != nil {
			return nil, fmt.Errorf("insert clock %s: %w", clocks[i].Regime, err)
		}
		clocks[i].ID = id
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return clocks, nil
}

func (r *SQLiteRepository) ListClocks(exerciseID int64) ([]Clock, error) {
	rows, err := r.conn.Query(`SELECT `+clockColumns+` FROM crisis_ex_clocks
		WHERE exercise_id = ? ORDER BY ordinal, id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Clock{}
	for rows.Next() {
		c, err := scanClock(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) SaveClock(c Clock) (Clock, error) {
	result, err := r.conn.Exec(`UPDATE crisis_ex_clocks SET
		actual_offset = ?, status = ?, evidence = ?, notes = ? WHERE id = ?`,
		c.ActualOffset, c.Status, c.Evidence, c.Notes, c.ID)
	if err != nil {
		return Clock{}, err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return Clock{}, notFound("clock")
	}
	return c, nil
}

// ---- findings ----

const findingColumns = `f.id, f.exercise_id, f.phase_id, f.ordinal, f.code, f.title, f.category,
	f.severity, f.description, f.root_cause, f.evidence, f.recommendation, f.owner,
	f.due_date, f.status, f.risk_ref, f.ai_generated, f.model, f.created_at, f.created_by,
	COALESCE(p.phase_key, '')`

func scanFinding(scan func(...any) error) (Finding, error) {
	var f Finding
	var aiGenerated int
	err := scan(&f.ID, &f.ExerciseID, &f.PhaseID, &f.Ordinal, &f.Code, &f.Title, &f.Category,
		&f.Severity, &f.Description, &f.RootCause, &f.Evidence, &f.Recommendation, &f.Owner,
		&f.DueDate, &f.Status, &f.RiskRef, &aiGenerated, &f.Model, &f.CreatedAt, &f.CreatedBy,
		&f.PhaseKey)
	f.AIGenerated = aiGenerated != 0
	return f, err
}

func (r *SQLiteRepository) SaveFinding(f Finding) (Finding, error) {
	if f.ID == 0 {
		id, err := r.conn.Insert(`INSERT INTO crisis_ex_findings
			(exercise_id, phase_id, ordinal, code, title, category, severity, description,
			 root_cause, evidence, recommendation, owner, due_date, status, risk_ref,
			 ai_generated, model, created_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.ExerciseID, f.PhaseID, f.Ordinal, f.Code, f.Title, f.Category, f.Severity,
			f.Description, f.RootCause, f.Evidence, f.Recommendation, f.Owner, f.DueDate,
			f.Status, f.RiskRef, boolToInt(f.AIGenerated), f.Model, f.CreatedAt, f.CreatedBy)
		if err != nil {
			return Finding{}, err
		}
		f.ID = id
		return f, nil
	}
	result, err := r.conn.Exec(`UPDATE crisis_ex_findings SET
		phase_id = ?, ordinal = ?, code = ?, title = ?, category = ?, severity = ?,
		description = ?, root_cause = ?, evidence = ?, recommendation = ?, owner = ?,
		due_date = ?, status = ?, risk_ref = ? WHERE id = ?`,
		f.PhaseID, f.Ordinal, f.Code, f.Title, f.Category, f.Severity, f.Description,
		f.RootCause, f.Evidence, f.Recommendation, f.Owner, f.DueDate, f.Status, f.RiskRef, f.ID)
	if err != nil {
		return Finding{}, err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return Finding{}, notFound("finding")
	}
	return f, nil
}

func (r *SQLiteRepository) ListFindings(exerciseID int64) ([]Finding, error) {
	rows, err := r.conn.Query(`SELECT `+findingColumns+` FROM crisis_ex_findings f
		LEFT JOIN crisis_ex_phases p ON p.id = f.phase_id
		WHERE f.exercise_id = ? ORDER BY f.ordinal, f.id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Finding{}
	for rows.Next() {
		f, err := scanFinding(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) DeleteFinding(id int64) error {
	result, err := r.conn.Exec(`DELETE FROM crisis_ex_findings WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return notFound("finding")
	}
	return nil
}

// ---- participants ----

func (r *SQLiteRepository) ReplaceParticipants(exerciseID int64, participants []Participant) ([]Participant, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM crisis_ex_participants WHERE exercise_id = ?`, exerciseID); err != nil {
		return nil, err
	}
	for i := range participants {
		participants[i].ExerciseID = exerciseID
		id, err := tx.Insert(`INSERT INTO crisis_ex_participants
			(exercise_id, name, role_key, org, player, attended, contact, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			exerciseID, participants[i].Name, participants[i].RoleKey, participants[i].Org,
			boolToInt(participants[i].Player), boolToInt(participants[i].Attended),
			participants[i].Contact, participants[i].Notes)
		if err != nil {
			return nil, err
		}
		participants[i].ID = id
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return participants, nil
}

func (r *SQLiteRepository) ListParticipants(exerciseID int64) ([]Participant, error) {
	rows, err := r.conn.Query(`SELECT id, exercise_id, name, role_key, org, player, attended,
		contact, notes FROM crisis_ex_participants WHERE exercise_id = ? ORDER BY id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Participant{}
	for rows.Next() {
		var p Participant
		var player, attended int
		if err := rows.Scan(&p.ID, &p.ExerciseID, &p.Name, &p.RoleKey, &p.Org, &player, &attended,
			&p.Contact, &p.Notes); err != nil {
			return nil, err
		}
		p.Player = player != 0
		p.Attended = attended != 0
		p.RoleLabel = RoleLabel(p.RoleKey)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- references ----

func (r *SQLiteRepository) AddReference(ref Reference) (Reference, error) {
	id, err := r.conn.Insert(`INSERT INTO crisis_ex_references
		(exercise_id, owner_kind, owner_id, ref_kind, ref, title, note, source, known, url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ref.ExerciseID, ref.OwnerKind, ref.OwnerID, ref.RefKind, ref.Ref, ref.Title,
		ref.Note, ref.Source, boolToInt(ref.Known), ref.URL, ref.CreatedAt)
	if err != nil {
		return Reference{}, fmt.Errorf("insert reference: %w", err)
	}
	ref.ID = id
	return ref, nil
}

func (r *SQLiteRepository) ListReferences(exerciseID int64) ([]Reference, error) {
	rows, err := r.conn.Query(`SELECT id, exercise_id, owner_kind, owner_id, ref_kind, ref,
		title, note, source, known, url, created_at
		FROM crisis_ex_references WHERE exercise_id = ?
		ORDER BY owner_kind, owner_id, ref_kind, ref`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Reference{}
	for rows.Next() {
		var ref Reference
		var known int
		if err := rows.Scan(&ref.ID, &ref.ExerciseID, &ref.OwnerKind, &ref.OwnerID, &ref.RefKind,
			&ref.Ref, &ref.Title, &ref.Note, &ref.Source, &known, &ref.URL, &ref.CreatedAt); err != nil {
			return nil, err
		}
		ref.Known = known != 0
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) DeleteReference(id int64) error {
	result, err := r.conn.Exec(`DELETE FROM crisis_ex_references WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return notFound("reference")
	}
	return nil
}

func (r *SQLiteRepository) ReplaceReferencesFor(exerciseID int64, ownerKind string, ownerID int64, refs []Reference) error {
	tx, err := r.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM crisis_ex_references
		WHERE exercise_id = ? AND owner_kind = ? AND owner_id = ?`, exerciseID, ownerKind, ownerID); err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := tx.Exec(`INSERT INTO crisis_ex_references
			(exercise_id, owner_kind, owner_id, ref_kind, ref, title, note, source, known, url, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			exerciseID, ownerKind, ownerID, ref.RefKind, ref.Ref, ref.Title, ref.Note,
			ref.Source, boolToInt(ref.Known), ref.URL, ref.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- versions ----

func (r *SQLiteRepository) CreateVersion(v Version) (Version, error) {
	tx, err := r.conn.Begin()
	if err != nil {
		return Version{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// The number is allocated inside the transaction that inserts the row, so
	// two concurrent report cuts cannot both become version 3.
	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(number), 0) + 1 FROM crisis_ex_versions
		WHERE exercise_id = ?`, v.ExerciseID).Scan(&next); err != nil {
		return Version{}, err
	}
	v.Number = next

	id, err := tx.Insert(`INSERT INTO crisis_ex_versions
		(exercise_id, number, kind, summary, note, snapshot, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ExerciseID, v.Number, v.Kind, v.Summary, v.Note, v.Snapshot, v.CreatedAt, v.CreatedBy)
	if err != nil {
		return Version{}, err
	}
	v.ID = id
	if err := tx.Commit(); err != nil {
		return Version{}, err
	}
	return v, nil
}

func (r *SQLiteRepository) SaveVersionSnapshot(versionID int64, snapshot string) error {
	_, err := r.conn.Exec(`UPDATE crisis_ex_versions SET snapshot = ? WHERE id = ?`, snapshot, versionID)
	return err
}

const versionColumns = `id, exercise_id, number, kind, summary, note, snapshot, created_at, created_by`

func scanVersion(scan func(...any) error) (Version, error) {
	var v Version
	err := scan(&v.ID, &v.ExerciseID, &v.Number, &v.Kind, &v.Summary, &v.Note, &v.Snapshot,
		&v.CreatedAt, &v.CreatedBy)
	return v, err
}

func (r *SQLiteRepository) GetVersion(exerciseID int64, number int) (Version, error) {
	row := r.conn.QueryRow(`SELECT `+versionColumns+` FROM crisis_ex_versions
		WHERE exercise_id = ? AND number = ?`, exerciseID, number)
	v, err := scanVersion(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, notFound("version")
	}
	return v, err
}

func (r *SQLiteRepository) LatestVersion(exerciseID int64) (Version, error) {
	row := r.conn.QueryRow(`SELECT `+versionColumns+` FROM crisis_ex_versions
		WHERE exercise_id = ? ORDER BY number DESC LIMIT 1`, exerciseID)
	v, err := scanVersion(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, notFound("version")
	}
	return v, err
}

func (r *SQLiteRepository) ListVersions(exerciseID int64) ([]Version, error) {
	rows, err := r.conn.Query(`SELECT id, exercise_id, number, kind, summary, note, '',
		created_at, created_by FROM crisis_ex_versions
		WHERE exercise_id = ? ORDER BY number DESC`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Version{}
	for rows.Next() {
		v, err := scanVersion(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- chat ----

func (r *SQLiteRepository) AppendChat(t ChatTurn) (ChatTurn, error) {
	id, err := r.conn.Insert(`INSERT INTO crisis_ex_chat
		(exercise_id, persona, role, content, model, actor, scope, session_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ExerciseID, t.Persona, t.Role, t.Content, t.Model, t.Actor, t.Scope, t.SessionID, t.CreatedAt)
	if err != nil {
		return ChatTurn{}, err
	}
	t.ID = id
	return t, nil
}

func (r *SQLiteRepository) ListChat(exerciseID int64) ([]ChatTurn, error) {
	rows, err := r.conn.Query(`SELECT id, exercise_id, persona, role, content, model, actor,
		scope, session_id, created_at FROM crisis_ex_chat
		WHERE exercise_id = ? ORDER BY id`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []ChatTurn{}
	for rows.Next() {
		var t ChatTurn
		if err := rows.Scan(&t.ID, &t.ExerciseID, &t.Persona, &t.Role, &t.Content, &t.Model,
			&t.Actor, &t.Scope, &t.SessionID, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) ClearChat(exerciseID int64) error {
	_, err := r.conn.Exec(`DELETE FROM crisis_ex_chat WHERE exercise_id = ?`, exerciseID)
	return err
}

// ---- helpers ----

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isNotFound reports whether err is this module's not-found error, so callers
// can distinguish "no such row" from a real database failure without matching
// on strings.
func isNotFound(err error) bool {
	var e ErrNotFound
	return errors.As(err, &e)
}

// nextCode formats a sequential code with a prefix, used for objective, inject
// and finding identifiers so a report can refer to "INJ-014" rather than to a
// database id that means nothing outside this installation.
func nextCode(prefix string, n int) string {
	return fmt.Sprintf("%s-%03d", strings.ToUpper(prefix), n)
}
