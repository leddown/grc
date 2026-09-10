package crisisexercise

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"grc/internal/aiprovider"
)

// Service orchestrates design, delivery, evaluation and reporting.
type Service struct {
	repo     Repository
	resolver CatalogResolver
	asker    Asker
	renderer Renderer
	agent    AgentConfig
	contexts contextLedger
	now      func() time.Time
}

// Asker is the slice of internal/aiprovider this module needs. Design, persona
// advice and after-action drafting all go through it, so provider choice,
// credentials and the shared ai_usage_log stay in one place — and so that a
// question can be served by a model on the local network, or by a wintermuted
// agent with tools over this installation's own catalogs, as readily as by
// Claude. *aiprovider.Router satisfies it.
type Asker interface {
	Available() bool
	Describe() string
	Ask(ctx context.Context, req aiprovider.Request) (aiprovider.Response, error)
}

func NewService(repo Repository, resolver CatalogResolver, asker Asker) *Service {
	return &Service{repo: repo, resolver: resolver, asker: asker, now: time.Now}
}

// WithRenderer attaches the PDF renderer. Without one the report is still a
// page: a deployment with no Chrome loses the download, not the feature.
func (s *Service) WithRenderer(r Renderer) *Service {
	s.renderer = r
	return s
}

// Configured reports whether the AI-assisted parts can run. Everything else —
// designing an exercise by hand, running it, recording it, reporting on it —
// works without a provider, which is deliberate: an exercise programme that
// stops when the model is unavailable is not a programme.
func (s *Service) Configured() bool { return s.asker != nil && s.asker.Available() }

// Model describes what will serve a generation request, for the UI.
func (s *Service) Model() string {
	if s.asker == nil {
		return ""
	}
	return s.asker.Describe()
}

// PDFAvailable reports whether the report download can be offered.
func (s *Service) PDFAvailable() bool { return s.renderer != nil }

func (s *Service) timestamp() string { return s.now().UTC().Format(time.RFC3339) }

// ---- creation ----

// CreateInput is a new exercise.
type CreateInput struct {
	Reference       string
	Title           string
	Summary         string
	Format          string
	Audience        string
	EntityName      string
	EntityType      string
	Jurisdiction    string
	Supervision     string
	CriticalFuncs   string
	ScenarioKey     string
	ThreatActor     string
	ThreatNarrative string
	InitialVector   string
	TLP             string
	ScheduledFor    string
	DurationMinutes int
	Facilitator     string
	ControlTeam     string
	Evaluators      string
	CreatedBy       string
	// PhaseKeys selects which legs of the default arc to include. Empty means
	// the whole arc — the right default, because the phases people drop are
	// reliably the ones they are least ready for.
	PhaseKeys []string
}

// Create builds an exercise: the row, the phase arc, the objectives and the
// seeded citations.
//
// A scenario key pre-fills the intelligence and the objectives from the
// library; anything the caller supplies overrides it, so the library is a
// starting point rather than a constraint.
func (s *Service) Create(in CreateInput) (Exercise, error) {
	in.Title = trim(in.Title)
	if in.Title == "" {
		return Exercise{}, invalid("a title is required")
	}

	scenario, hasScenario := ScenarioByKey(trim(in.ScenarioKey))
	if hasScenario {
		if trim(in.Summary) == "" {
			in.Summary = scenario.Summary
		}
		if trim(in.ThreatActor) == "" {
			in.ThreatActor = scenario.ThreatActor
		}
		if trim(in.ThreatNarrative) == "" {
			in.ThreatNarrative = scenario.Narrative
		}
		if trim(in.InitialVector) == "" {
			in.InitialVector = scenario.InitialVector
		}
		if trim(in.CriticalFuncs) == "" {
			in.CriticalFuncs = scenario.CriticalFunctions
		}
		if trim(in.Format) == "" {
			in.Format = scenario.SuggestedFormat
		}
		if trim(in.Audience) == "" {
			in.Audience = scenario.SuggestedAudience
		}
	}

	format, ok := FormatByKey(trim(in.Format))
	if !ok {
		format, _ = FormatByKey("tabletop")
	}
	duration := in.DurationMinutes
	if duration <= 0 {
		duration = format.DefaultMinutes
	}

	reference := trim(in.Reference)
	if reference == "" {
		var err error
		reference, err = s.nextReference()
		if err != nil {
			return Exercise{}, err
		}
	}
	if _, err := s.repo.ExerciseByReference(reference); err == nil {
		return Exercise{}, invalidf("reference %q is already in use", reference)
	} else if !isNotFound(err) {
		return Exercise{}, err
	}

	now := s.timestamp()
	ex := Exercise{
		Reference:         reference,
		Title:             in.Title,
		Summary:           trim(in.Summary),
		Format:            format.Key,
		Kind:              format.Kind,
		Audience:          audienceOrDefault(in.Audience),
		EntityName:        trim(in.EntityName),
		EntityType:        trim(in.EntityType),
		Jurisdiction:      strings.ToUpper(trim(in.Jurisdiction)),
		Supervision:       trim(in.Supervision),
		CriticalFunctions: trim(in.CriticalFuncs),
		ThreatActor:       trim(in.ThreatActor),
		ThreatNarrative:   trim(in.ThreatNarrative),
		InitialVector:     trim(in.InitialVector),
		ScenarioKey:       trim(in.ScenarioKey),
		Status:            StatusDraft,
		TLP:               tlpOrDefault(in.TLP),
		ScheduledFor:      trim(in.ScheduledFor),
		DurationMinutes:   duration,
		Facilitator:       trim(in.Facilitator),
		ControlTeam:       trim(in.ControlTeam),
		Evaluators:        trim(in.Evaluators),
		CreatedAt:         now,
		CreatedBy:         trim(in.CreatedBy),
		UpdatedAt:         now,
		UpdatedBy:         trim(in.CreatedBy),
	}

	created, err := s.repo.CreateExercise(ex)
	if err != nil {
		return Exercise{}, err
	}

	phases, err := s.seedPhases(created, in.PhaseKeys, duration)
	if err != nil {
		return Exercise{}, err
	}
	if err := s.seedObjectives(created, scenario, hasScenario); err != nil {
		return Exercise{}, err
	}
	if err := s.seedReferences(created, scenario, hasScenario, phases); err != nil {
		return Exercise{}, err
	}
	if _, err := s.repo.SaveClassification(Classification{ExerciseID: created.ID, UpdatedAt: now}); err != nil {
		return Exercise{}, err
	}
	if err := s.recomputeClocks(created.ID); err != nil {
		return Exercise{}, err
	}
	return s.repo.GetExercise(created.ID)
}

// nextReference allocates the next CX-<year>-<n>. It scans the existing
// references rather than keeping a counter, because a counter in app_state
// would drift the first time an exercise was deleted or a database was
// restored, and a duplicated reference on a circulated report is worse than a
// gap in the sequence.
func (s *Service) nextReference() (string, error) {
	existing, err := s.repo.ListExercises()
	if err != nil {
		return "", err
	}
	year := s.now().UTC().Year()
	prefix := fmt.Sprintf("CX-%d-", year)
	highest := 0
	for _, ex := range existing {
		if !strings.HasPrefix(ex.Reference, prefix) {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimPrefix(ex.Reference, prefix), "%d", &n); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%s%02d", prefix, highest+1), nil
}

// seedPhases lays the arc out across the exercise's duration. The seeded
// offsets assume a five-hour run; a shorter or longer exercise scales them
// proportionally, so the arc still ends when the room does.
func (s *Service) seedPhases(ex Exercise, keys []string, duration int) ([]Phase, error) {
	templates := DefaultPhases()
	if len(keys) > 0 {
		wanted := map[string]bool{}
		for _, k := range keys {
			wanted[trim(k)] = true
		}
		var filtered []PhaseTemplate
		for _, t := range templates {
			if wanted[t.Key] {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return nil, invalid("none of the requested phases exist in the default arc")
		}
		templates = filtered
	}

	planned := 0
	for _, t := range templates {
		planned += t.DurationMinutes
	}
	scale := 1.0
	if planned > 0 && duration > 0 {
		scale = float64(duration) / float64(planned)
	}

	phases := make([]Phase, 0, len(templates))
	offset := 0
	for i, t := range templates {
		d := int(float64(t.DurationMinutes) * scale)
		if d < 5 {
			d = 5
		}
		phases = append(phases, Phase{
			ExerciseID:      ex.ID,
			Ordinal:         i + 1,
			Key:             t.Key,
			Name:            t.Name,
			Purpose:         t.Purpose,
			EntryCriteria:   t.EntryCriteria,
			ExitCriteria:    t.ExitCriteria,
			LeadRole:        t.LeadRole,
			OffsetMinutes:   offset,
			DurationMinutes: d,
			Status:          PhasePending,
			Notes:           t.FacilitatorNotes,
		})
		offset += d
	}
	return s.repo.ReplacePhases(ex.ID, phases)
}

func (s *Service) seedObjectives(ex Exercise, scenario Scenario, hasScenario bool) error {
	if !hasScenario || len(scenario.Objectives) == 0 {
		return nil
	}
	objectives := make([]Objective, 0, len(scenario.Objectives))
	for i, o := range scenario.Objectives {
		objectives = append(objectives, Objective{
			ExerciseID:      ex.ID,
			Ordinal:         i + 1,
			Code:            nextCode("OBJ", i+1),
			Text:            o.Text,
			Capability:      o.Capability,
			SuccessCriteria: o.SuccessCriteria,
			Rating:          RatingUntested,
		})
	}
	saved, err := s.repo.ReplaceObjectives(ex.ID, objectives)
	if err != nil {
		return err
	}
	for i, o := range saved {
		if i >= len(scenario.Objectives) {
			break
		}
		for _, key := range scenario.Objectives[i].Authorities {
			if _, err := s.addSeedReference(ex.ID, OwnerObjective, o.ID, RefAuthority, key); err != nil {
				return err
			}
		}
	}
	return nil
}

// seedReferences attaches the citations that come for free: the scenario's own,
// and each phase's. An exercise therefore arrives already able to say which
// instruments it is answerable to, which is the state most exercise
// documentation never reaches.
func (s *Service) seedReferences(ex Exercise, scenario Scenario, hasScenario bool, phases []Phase) error {
	if hasScenario {
		for _, key := range scenario.Authorities {
			if _, err := s.addSeedReference(ex.ID, OwnerExercise, ex.ID, RefAuthority, key); err != nil {
				return err
			}
		}
	}
	for _, p := range phases {
		t, ok := PhaseTemplateFor(p.Key)
		if !ok {
			continue
		}
		for _, key := range t.Authorities {
			if _, err := s.addSeedReference(ex.ID, OwnerPhase, p.ID, RefAuthority, key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) addSeedReference(exerciseID int64, ownerKind string, ownerID int64, refKind, ref string) (Reference, error) {
	r := Reference{
		ExerciseID: exerciseID,
		OwnerKind:  ownerKind,
		OwnerID:    ownerID,
		RefKind:    refKind,
		Ref:        ref,
		Source:     SourceSeed,
		CreatedAt:  s.timestamp(),
	}
	return s.repo.AddReference(Resolve(s.resolver, r))
}

// ---- reading ----

func (s *Service) ListExercises() ([]Exercise, error) { return s.repo.ListExercises() }

func (s *Service) GetExercise(id int64) (Exercise, error) { return s.repo.GetExercise(id) }

func (s *Service) DeleteExercise(id int64) error { return s.repo.DeleteExercise(id) }

// Dossier assembles the exercise and everything hanging off it.
//
// Version 0 means live: the current state of the record. A version number means
// the snapshot cut at that point, which is what a circulated report has to keep
// showing after somebody edits a finding.
func (s *Service) Dossier(id int64, version int) (Dossier, error) {
	if version > 0 {
		return s.snapshotDossier(id, version)
	}
	return s.liveDossier(id)
}

func (s *Service) liveDossier(id int64) (Dossier, error) {
	ex, err := s.repo.GetExercise(id)
	if err != nil {
		return Dossier{}, err
	}

	objectives, err := s.repo.ListObjectives(id)
	if err != nil {
		return Dossier{}, err
	}
	phases, err := s.repo.ListPhases(id)
	if err != nil {
		return Dossier{}, err
	}
	injects, err := s.repo.ListInjects(id)
	if err != nil {
		return Dossier{}, err
	}
	responses, err := s.repo.ListResponses(id)
	if err != nil {
		return Dossier{}, err
	}
	participants, err := s.repo.ListParticipants(id)
	if err != nil {
		return Dossier{}, err
	}
	classification, err := s.repo.GetClassification(id)
	if err != nil {
		return Dossier{}, err
	}
	clocks, err := s.repo.ListClocks(id)
	if err != nil {
		return Dossier{}, err
	}
	decisions, err := s.repo.ListDecisions(id)
	if err != nil {
		return Dossier{}, err
	}
	findings, err := s.repo.ListFindings(id)
	if err != nil {
		return Dossier{}, err
	}
	refs, err := s.repo.ListReferences(id)
	if err != nil {
		return Dossier{}, err
	}

	version, err := s.repo.LatestVersion(id)
	if err != nil && !isNotFound(err) {
		return Dossier{}, err
	}

	index := IndexReferences(refs)
	byInject := map[int64]Response{}
	for _, r := range responses {
		byInject[r.InjectID] = r
	}

	for i := range objectives {
		objectives[i].References = index.For(OwnerObjective, objectives[i].ID)
	}
	for i := range decisions {
		decisions[i].References = index.For(OwnerDecision, decisions[i].ID)
	}
	for i := range findings {
		findings[i].References = index.For(OwnerFinding, findings[i].ID)
	}
	for i := range clocks {
		clocks[i].References = index.For(OwnerClock, clocks[i].ID)
	}
	for i := range injects {
		injects[i].References = index.For(OwnerInject, injects[i].ID)
		if resp, ok := byInject[injects[i].ID]; ok {
			r := resp
			injects[i].Response = &r
		}
	}

	byPhase := map[int64][]Inject{}
	for _, in := range injects {
		byPhase[in.PhaseID] = append(byPhase[in.PhaseID], in)
	}
	for i := range phases {
		phases[i].References = index.For(OwnerPhase, phases[i].ID)
		phases[i].Injects = byPhase[phases[i].ID]
	}

	return Dossier{
		Exercise:       ex,
		Objectives:     objectives,
		Phases:         phases,
		Participants:   participants,
		Classification: Evaluate(classification),
		Clocks:         clocks,
		Decisions:      decisions,
		Findings:       findings,
		References:     refs,
		Version:        version,
		Stats:          computeStats(objectives, phases, injects, decisions, clocks, findings, refs),
		GeneratedAt:    s.timestamp(),
	}, nil
}

func (s *Service) snapshotDossier(id int64, number int) (Dossier, error) {
	v, err := s.repo.GetVersion(id, number)
	if err != nil {
		return Dossier{}, err
	}
	if trim(v.Snapshot) == "" {
		return Dossier{}, invalidf("version %d has no snapshot", number)
	}
	var d Dossier
	if err := json.Unmarshal([]byte(v.Snapshot), &d); err != nil {
		return Dossier{}, fmt.Errorf("read version %d snapshot: %w", number, err)
	}
	d.Version = v
	return d, nil
}

// computeStats is the arithmetic at the top of every view of an exercise.
func computeStats(objectives []Objective, phases []Phase, injects []Inject,
	decisions []Decision, clocks []Clock, findings []Finding, refs []Reference) Stats {

	st := Stats{
		Objectives: len(objectives),
		Phases:     len(phases),
		Injects:    len(injects),
		Decisions:  len(decisions),
		Findings:   len(findings),
		References: len(refs),
	}
	for _, o := range objectives {
		if o.Rating == RatingMet {
			st.ObjectivesMet++
		}
	}
	for _, in := range injects {
		switch in.Type {
		case InjectStress:
			st.StressInjects++
		case InjectAmbiguous:
			st.AmbiguityRatio++
		}
		if in.Response == nil || in.Response.Outcome == OutcomeNotPlayed {
			continue
		}
		st.InjectsPlayed++
		switch in.Response.Outcome {
		case OutcomeAsExpected:
			st.AsExpected++
		case OutcomeDeviation, OutcomePartial:
			st.Deviations++
		case OutcomeMissed:
			st.Missed++
		}
	}
	for _, c := range clocks {
		if c.Status == ClockNotApplicable {
			continue
		}
		st.Clocks++
		switch c.Status {
		case ClockMet:
			st.ClocksMet++
		case ClockMissed:
			st.ClocksMissed++
		}
	}
	for _, f := range findings {
		if f.Status == FindingOpen || f.Status == FindingInProgress {
			st.FindingsOpen++
		}
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			st.CriticalHigh++
		}
	}

	distinct := map[string]struct{}{}
	for _, r := range refs {
		distinct[r.RefKind+"/"+strings.ToLower(r.Ref)] = struct{}{}
		if !r.Known {
			st.UnknownRefs++
		}
	}
	st.DistinctRefs = len(distinct)
	return st
}

// ---- updating ----

// UpdateInput carries the editable fields of an exercise.
type UpdateInput struct {
	Title           string `json:"title"`
	Summary         string `json:"summary"`
	Format          string `json:"format"`
	Audience        string `json:"audience"`
	EntityName      string `json:"entity_name"`
	EntityType      string `json:"entity_type"`
	Jurisdiction    string `json:"jurisdiction"`
	Supervision     string `json:"supervision"`
	CriticalFuncs   string `json:"critical_functions"`
	ThreatActor     string `json:"threat_actor"`
	ThreatNarrative string `json:"threat_narrative"`
	InitialVector   string `json:"initial_vector"`
	TLP             string `json:"tlp"`
	ScheduledFor    string `json:"scheduled_for"`
	DurationMinutes *int   `json:"duration_minutes"`
	Facilitator     string `json:"facilitator"`
	ControlTeam     string `json:"control_team"`
	Evaluators      string `json:"evaluators"`
	Status          string `json:"status"`
	Actor           string `json:"-"`
}

// Update applies an edit. Fields left empty are left alone, so a caller that
// wants to change one thing does not have to resend the whole record.
func (s *Service) Update(id int64, in UpdateInput) (Exercise, error) {
	ex, err := s.repo.GetExercise(id)
	if err != nil {
		return Exercise{}, err
	}

	setIf(&ex.Title, in.Title)
	setIf(&ex.Summary, in.Summary)
	setIf(&ex.EntityName, in.EntityName)
	setIf(&ex.EntityType, in.EntityType)
	setIf(&ex.Supervision, in.Supervision)
	setIf(&ex.CriticalFunctions, in.CriticalFuncs)
	setIf(&ex.ThreatActor, in.ThreatActor)
	setIf(&ex.ThreatNarrative, in.ThreatNarrative)
	setIf(&ex.InitialVector, in.InitialVector)
	setIf(&ex.ScheduledFor, in.ScheduledFor)
	setIf(&ex.Facilitator, in.Facilitator)
	setIf(&ex.ControlTeam, in.ControlTeam)
	setIf(&ex.Evaluators, in.Evaluators)

	if j := trim(in.Jurisdiction); j != "" {
		ex.Jurisdiction = strings.ToUpper(j)
	}
	if f := trim(in.Format); f != "" {
		format, ok := FormatByKey(f)
		if !ok {
			return Exercise{}, invalidf("unknown exercise format %q", f)
		}
		ex.Format = format.Key
		ex.Kind = format.Kind
	}
	if a := trim(in.Audience); a != "" {
		ex.Audience = audienceOrDefault(a)
	}
	if t := trim(in.TLP); t != "" {
		ex.TLP = tlpOrDefault(t)
	}
	if in.DurationMinutes != nil && *in.DurationMinutes > 0 {
		ex.DurationMinutes = *in.DurationMinutes
	}
	if st := trim(in.Status); st != "" {
		ex = s.applyStatus(ex, st)
	}

	ex.UpdatedAt = s.timestamp()
	ex.UpdatedBy = trim(in.Actor)
	updated, err := s.repo.UpdateExercise(ex)
	if err != nil {
		return Exercise{}, err
	}
	// The jurisdiction and supervision determine which authorities are owed
	// what, so a change to either has to reach the clocks.
	if err := s.recomputeClocks(id); err != nil {
		return Exercise{}, err
	}
	return updated, nil
}

// applyStatus records the transition and stamps the wall-clock times that the
// T+ offsets are measured against.
func (s *Service) applyStatus(ex Exercise, status string) Exercise {
	status = oneOf(status, ex.Status, StatusDraft, StatusScheduled, StatusInProgress, StatusCompleted, StatusClosed)
	if status == ex.Status {
		return ex
	}
	now := s.timestamp()
	switch status {
	case StatusInProgress:
		if ex.StartedAt == "" {
			ex.StartedAt = now
		}
	case StatusCompleted, StatusClosed:
		if ex.EndedAt == "" {
			ex.EndedAt = now
		}
	}
	ex.Status = status
	return ex
}

// Clone copies an exercise's design — objectives, phases, injects and their
// citations — into a new draft, leaving every observation behind.
//
// This is how an exercise is re-run. Replaying into the original rows would
// overwrite the record a report was issued from; cloning keeps last year's
// findings intact and makes the year-on-year comparison possible, which is the
// whole point of running a campaign rather than an annual event.
func (s *Service) Clone(id int64, title, actor string) (Exercise, error) {
	source, err := s.repo.GetExercise(id)
	if err != nil {
		return Exercise{}, err
	}

	reference, err := s.nextReference()
	if err != nil {
		return Exercise{}, err
	}
	now := s.timestamp()
	clone := source
	clone.ID = 0
	clone.Reference = reference
	if t := trim(title); t != "" {
		clone.Title = t
	} else {
		clone.Title = source.Title + " (re-run)"
	}
	clone.Status = StatusDraft
	clone.StartedAt = ""
	clone.EndedAt = ""
	clone.ScheduledFor = ""
	clone.CreatedAt = now
	clone.CreatedBy = trim(actor)
	clone.UpdatedAt = now
	clone.UpdatedBy = trim(actor)

	created, err := s.repo.CreateExercise(clone)
	if err != nil {
		return Exercise{}, err
	}

	refs, err := s.repo.ListReferences(id)
	if err != nil {
		return Exercise{}, err
	}
	index := IndexReferences(refs)

	if err := s.cloneReferences(created.ID, index.For(OwnerExercise, id), OwnerExercise, created.ID); err != nil {
		return Exercise{}, err
	}

	objectives, err := s.repo.ListObjectives(id)
	if err != nil {
		return Exercise{}, err
	}
	for i := range objectives {
		objectives[i].ID = 0
		objectives[i].Rating = RatingUntested
		objectives[i].Notes = ""
	}
	newObjectives, err := s.repo.ReplaceObjectives(created.ID, objectives)
	if err != nil {
		return Exercise{}, err
	}
	sourceObjectives, _ := s.repo.ListObjectives(id)
	for i, o := range newObjectives {
		if i < len(sourceObjectives) {
			if err := s.cloneReferences(created.ID, index.For(OwnerObjective, sourceObjectives[i].ID), OwnerObjective, o.ID); err != nil {
				return Exercise{}, err
			}
		}
	}

	sourcePhases, err := s.repo.ListPhases(id)
	if err != nil {
		return Exercise{}, err
	}
	phases := make([]Phase, len(sourcePhases))
	copy(phases, sourcePhases)
	for i := range phases {
		phases[i].ID = 0
		phases[i].Status = PhasePending
	}
	newPhases, err := s.repo.ReplacePhases(created.ID, phases)
	if err != nil {
		return Exercise{}, err
	}
	phaseMap := map[int64]int64{}
	for i, p := range newPhases {
		phaseMap[sourcePhases[i].ID] = p.ID
		if err := s.cloneReferences(created.ID, index.For(OwnerPhase, sourcePhases[i].ID), OwnerPhase, p.ID); err != nil {
			return Exercise{}, err
		}
	}

	sourceInjects, err := s.repo.ListInjects(id)
	if err != nil {
		return Exercise{}, err
	}
	injects := make([]Inject, 0, len(sourceInjects))
	for _, in := range sourceInjects {
		copied := in
		copied.ID = 0
		copied.ExerciseID = created.ID
		copied.PhaseID = phaseMap[in.PhaseID]
		copied.Response = nil
		copied.References = nil
		copied.CreatedAt = now
		injects = append(injects, copied)
	}
	newInjects, err := s.repo.AppendInjects(injects)
	if err != nil {
		return Exercise{}, err
	}
	for i, in := range newInjects {
		if err := s.cloneReferences(created.ID, index.For(OwnerInject, sourceInjects[i].ID), OwnerInject, in.ID); err != nil {
			return Exercise{}, err
		}
	}

	if _, err := s.repo.SaveClassification(Classification{ExerciseID: created.ID, UpdatedAt: now}); err != nil {
		return Exercise{}, err
	}
	if err := s.recomputeClocks(created.ID); err != nil {
		return Exercise{}, err
	}
	return s.repo.GetExercise(created.ID)
}

func (s *Service) cloneReferences(exerciseID int64, refs []Reference, ownerKind string, ownerID int64) error {
	for _, ref := range refs {
		ref.ID = 0
		ref.ExerciseID = exerciseID
		ref.OwnerKind = ownerKind
		ref.OwnerID = ownerID
		ref.CreatedAt = s.timestamp()
		if _, err := s.repo.AddReference(ref); err != nil {
			return err
		}
	}
	return nil
}

// ---- objectives, phases, participants ----

func (s *Service) SaveObjective(exerciseID int64, o Objective) (Objective, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return Objective{}, err
	}
	o.ExerciseID = exerciseID
	if trim(o.Text) == "" {
		return Objective{}, invalid("an objective needs text")
	}
	o.Rating = oneOf(o.Rating, RatingUntested, RatingUntested, RatingMet, RatingPartiallyMet, RatingNotMet, RatingNotApplicable)
	if trim(o.Code) == "" {
		existing, err := s.repo.ListObjectives(exerciseID)
		if err != nil {
			return Objective{}, err
		}
		o.Code = nextCode("OBJ", len(existing)+1)
		o.Ordinal = len(existing) + 1
	}
	return s.repo.SaveObjective(o)
}

func (s *Service) ListObjectives(exerciseID int64) ([]Objective, error) {
	return s.repo.ListObjectives(exerciseID)
}

func (s *Service) SavePhase(exerciseID int64, p Phase) (Phase, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return Phase{}, err
	}
	p.ExerciseID = exerciseID
	p.Status = oneOf(p.Status, PhasePending, PhasePending, PhaseActive, PhaseComplete, PhaseSkipped, PhaseCurtailed)
	return s.repo.SavePhase(p)
}

func (s *Service) ListPhases(exerciseID int64) ([]Phase, error) { return s.repo.ListPhases(exerciseID) }

func (s *Service) SaveParticipants(exerciseID int64, participants []Participant) ([]Participant, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return nil, err
	}
	for i := range participants {
		participants[i].Name = trim(participants[i].Name)
		participants[i].RoleKey = trim(participants[i].RoleKey)
	}
	return s.repo.ReplaceParticipants(exerciseID, participants)
}

func (s *Service) ListParticipants(exerciseID int64) ([]Participant, error) {
	return s.repo.ListParticipants(exerciseID)
}

// ---- injects ----

// SaveInject writes one inject, allocating a code and a phase when the caller
// has not.
func (s *Service) SaveInject(exerciseID int64, in Inject) (Inject, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return Inject{}, err
	}
	if trim(in.Title) == "" && trim(in.Body) == "" {
		return Inject{}, invalid("an inject needs a title or a body")
	}
	in.ExerciseID = exerciseID
	in.Type = oneOf(in.Type, InjectEvent, InjectEvent, InjectStress, InjectAmbiguous,
		InjectDecision, InjectContingency, InjectInformation)
	in.Difficulty = oneOf(in.Difficulty, DifficultyChallenge,
		DifficultyFoundation, DifficultyChallenge, DifficultyAdvanced)

	if in.ID == 0 {
		existing, err := s.repo.ListInjects(exerciseID)
		if err != nil {
			return Inject{}, err
		}
		if trim(in.Code) == "" {
			in.Code = nextCode("INJ", len(existing)+1)
		}
		if in.Ordinal == 0 {
			in.Ordinal = len(existing) + 1
		}
		in.CreatedAt = s.timestamp()
	}
	return s.repo.SaveInject(in)
}

func (s *Service) ListInjects(exerciseID int64) ([]Inject, error) {
	return s.repo.ListInjects(exerciseID)
}

func (s *Service) GetInject(id int64) (Inject, error) { return s.repo.GetInject(id) }

func (s *Service) DeleteInject(id int64) error { return s.repo.DeleteInject(id) }

// RecordResponse records what the team actually did with an inject.
func (s *Service) RecordResponse(injectID int64, resp Response) (Response, error) {
	inject, err := s.repo.GetInject(injectID)
	if err != nil {
		return Response{}, err
	}
	resp.InjectID = injectID
	resp.ExerciseID = inject.ExerciseID
	resp.Outcome = oneOf(resp.Outcome, OutcomeNotPlayed,
		OutcomeAsExpected, OutcomePartial, OutcomeDeviation, OutcomeMissed, OutcomeNotPlayed)
	now := s.timestamp()
	if resp.CreatedAt == "" {
		resp.CreatedAt = now
	}
	resp.UpdatedAt = now
	if trim(resp.DeliveredAt) == "" {
		resp.DeliveredAt = now
	}
	return s.repo.SaveResponse(resp)
}

// ---- decisions ----

func (s *Service) SaveDecision(exerciseID int64, d Decision) (Decision, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return Decision{}, err
	}
	if trim(d.Title) == "" {
		return Decision{}, invalid("a decision needs a title")
	}
	d.ExerciseID = exerciseID
	if d.ID == 0 {
		d.CreatedAt = s.timestamp()
	}
	return s.repo.SaveDecision(d)
}

func (s *Service) ListDecisions(exerciseID int64) ([]Decision, error) {
	return s.repo.ListDecisions(exerciseID)
}

func (s *Service) DeleteDecision(id int64) error { return s.repo.DeleteDecision(id) }

// ---- classification and clocks ----

// SaveClassification stores the team's materiality assessment, applies the DORA
// combination rule to it, and rebuilds the clock set from the result.
func (s *Service) SaveClassification(exerciseID int64, c Classification, actor string) (Classification, []Clock, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return Classification{}, nil, err
	}
	c.ExerciseID = exerciseID
	c.UpdatedAt = s.timestamp()
	c.UpdatedBy = trim(actor)
	c = Evaluate(c)

	saved, err := s.repo.SaveClassification(c)
	if err != nil {
		return Classification{}, nil, err
	}
	if err := s.recomputeClocks(exerciseID); err != nil {
		return Classification{}, nil, err
	}
	clocks, err := s.repo.ListClocks(exerciseID)
	if err != nil {
		return Classification{}, nil, err
	}
	return saved, clocks, nil
}

func (s *Service) GetClassification(exerciseID int64) (Classification, error) {
	c, err := s.repo.GetClassification(exerciseID)
	if err != nil {
		return Classification{}, err
	}
	return Evaluate(c), nil
}

func (s *Service) recomputeClocks(exerciseID int64) error {
	ex, err := s.repo.GetExercise(exerciseID)
	if err != nil {
		return err
	}
	c, err := s.repo.GetClassification(exerciseID)
	if err != nil {
		return err
	}
	clocks := ComputeClocks(ex, c)
	saved, err := s.repo.ReplaceClocks(exerciseID, clocks)
	if err != nil {
		return err
	}
	// Each clock cites the instrument that imposes it, so the report's timeline
	// and its citations are the same fact rather than two things that have to
	// be kept in step by hand.
	for _, clk := range saved {
		if clk.Source == "" {
			continue
		}
		if _, err := s.addSeedReference(exerciseID, OwnerClock, clk.ID, RefAuthority, clk.Source); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListClocks(exerciseID int64) ([]Clock, error) { return s.repo.ListClocks(exerciseID) }

// RecordNotification records when a notification actually went, and scores the
// clock against its deadline.
func (s *Service) RecordNotification(exerciseID, clockID int64, actualOffset int, evidence string) (Clock, error) {
	ex, err := s.repo.GetExercise(exerciseID)
	if err != nil {
		return Clock{}, err
	}
	clocks, err := s.repo.ListClocks(exerciseID)
	if err != nil {
		return Clock{}, err
	}
	for _, c := range clocks {
		if c.ID != clockID {
			continue
		}
		ended := ex.Status == StatusCompleted || ex.Status == StatusClosed
		c = ScoreClock(c, actualOffset, ended)
		if e := trim(evidence); e != "" {
			c.Evidence = e
		}
		return s.repo.SaveClock(c)
	}
	return Clock{}, notFound("clock")
}

// ---- findings ----

func (s *Service) SaveFinding(exerciseID int64, f Finding) (Finding, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return Finding{}, err
	}
	if trim(f.Title) == "" {
		return Finding{}, invalid("a finding needs a title")
	}
	f.ExerciseID = exerciseID
	f.Severity = oneOf(f.Severity, SeverityMedium,
		SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityObserve)
	f.Status = oneOf(f.Status, FindingOpen, FindingOpen, FindingInProgress, FindingClosed, FindingAccepted)
	f.Category = oneOf(f.Category, CategoryProcess, CategoryPeople, CategoryProcess, CategoryTechnology,
		CategoryGovernance, CategoryCommunication, CategoryThirdParty, CategoryRegulatory)

	if f.ID == 0 {
		existing, err := s.repo.ListFindings(exerciseID)
		if err != nil {
			return Finding{}, err
		}
		if trim(f.Code) == "" {
			f.Code = nextCode("F", len(existing)+1)
		}
		if f.Ordinal == 0 {
			f.Ordinal = len(existing) + 1
		}
		f.CreatedAt = s.timestamp()
	}
	return s.repo.SaveFinding(f)
}

func (s *Service) ListFindings(exerciseID int64) ([]Finding, error) {
	return s.repo.ListFindings(exerciseID)
}

func (s *Service) DeleteFinding(id int64) error { return s.repo.DeleteFinding(id) }

// ---- references ----

// AddReference attaches a citation to a part of an exercise, resolving it
// against the catalogs first.
func (s *Service) AddReference(exerciseID int64, ref Reference) (Reference, error) {
	if _, err := s.repo.GetExercise(exerciseID); err != nil {
		return Reference{}, err
	}
	ref.ExerciseID = exerciseID
	ref.OwnerKind = oneOf(ref.OwnerKind, OwnerExercise, OwnerExercise, OwnerObjective,
		OwnerPhase, OwnerInject, OwnerDecision, OwnerFinding, OwnerClock)
	ref.RefKind = oneOf(ref.RefKind, RefControl, ReferenceKinds()...)
	if trim(ref.Ref) == "" {
		return Reference{}, invalid("a reference needs an identifier")
	}
	if ref.Source == "" {
		ref.Source = SourceManual
	}
	if ref.OwnerID == 0 {
		ref.OwnerID = exerciseID
		ref.OwnerKind = OwnerExercise
	}
	ref.CreatedAt = s.timestamp()
	return s.repo.AddReference(Resolve(s.resolver, ref))
}

func (s *Service) ListReferences(exerciseID int64) ([]Reference, error) {
	return s.repo.ListReferences(exerciseID)
}

func (s *Service) DeleteReference(id int64) error { return s.repo.DeleteReference(id) }

// SuggestReferences returns candidate citations for a piece of text, for the
// picker on the design page.
func (s *Service) SuggestReferences(text string, perKind int) []RefTarget {
	if perKind <= 0 {
		perKind = 5
	}
	return Suggest(s.resolver, text, perKind)
}

// Coverage answers "what did this exercise test", inverted from the reference
// table.
func (s *Service) Coverage(exerciseID int64) ([]CoverageRow, error) {
	refs, err := s.repo.ListReferences(exerciseID)
	if err != nil {
		return nil, err
	}
	return Coverage(refs), nil
}

// ---- versions ----

// CutVersion snapshots the exercise as it currently stands.
func (s *Service) CutVersion(exerciseID int64, kind, note, actor string) (Version, error) {
	d, err := s.liveDossier(exerciseID)
	if err != nil {
		return Version{}, err
	}
	kind = oneOf(kind, VersionDesign, VersionDesign, VersionAfterAction)

	v := Version{
		ExerciseID: exerciseID,
		Kind:       kind,
		Summary:    versionSummary(kind, d),
		Note:       trim(note),
		CreatedAt:  s.timestamp(),
		CreatedBy:  trim(actor),
	}
	created, err := s.repo.CreateVersion(v)
	if err != nil {
		return Version{}, err
	}

	d.Version = created
	snapshot, err := json.Marshal(d)
	if err != nil {
		return Version{}, err
	}
	if err := s.repo.SaveVersionSnapshot(created.ID, string(snapshot)); err != nil {
		return Version{}, err
	}
	created.Snapshot = string(snapshot)
	return created, nil
}

func versionSummary(kind string, d Dossier) string {
	if kind == VersionAfterAction {
		return fmt.Sprintf("%d of %d injects played, %d handled as expected, %d clocks met of %d, %d findings (%d critical or high).",
			d.Stats.InjectsPlayed, d.Stats.Injects, d.Stats.AsExpected,
			d.Stats.ClocksMet, d.Stats.Clocks, d.Stats.Findings, d.Stats.CriticalHigh)
	}
	return fmt.Sprintf("%d phases, %d injects, %d objectives, %d citations across %d distinct references.",
		d.Stats.Phases, d.Stats.Injects, d.Stats.Objectives, d.Stats.References, d.Stats.DistinctRefs)
}

func (s *Service) ListVersions(exerciseID int64) ([]Version, error) {
	return s.repo.ListVersions(exerciseID)
}

// ---- chat ----

func (s *Service) ListChat(exerciseID int64) ([]ChatTurn, error) {
	return s.repo.ListChat(exerciseID)
}

// ClearChat clears one of an exercise's conversations: the agent's when agent is
// true, every adviser's otherwise. They are cleared from different tabs, and
// clearing the advisers must not take the agent's conversation with it.
func (s *Service) ClearChat(exerciseID int64, agent bool) error {
	return s.repo.ClearChat(exerciseID, agent)
}

// ---- small helpers ----

func setIf(dst *string, value string) {
	if v := trim(value); v != "" {
		*dst = v
	}
}

func audienceOrDefault(value string) string {
	value = trim(value)
	for _, a := range Audiences() {
		if a.Key == value {
			return value
		}
	}
	return "management"
}

func tlpOrDefault(value string) string {
	value = strings.ToUpper(trim(value))
	switch value {
	case TLPClear, TLPGreen, TLPAmber, TLPAmberStr, TLPRedMarker:
		return value
	default:
		return TLPAmber
	}
}

// sortInjectsByClock orders a slice the way a controller reads it during
// delivery, which is not the order it was written in.
func sortInjectsByClock(injects []Inject) {
	sort.SliceStable(injects, func(i, j int) bool {
		if injects[i].OffsetMinutes != injects[j].OffsetMinutes {
			return injects[i].OffsetMinutes < injects[j].OffsetMinutes
		}
		return injects[i].Ordinal < injects[j].Ordinal
	})
}
