package crisisexercise

import (
	"sort"
	"strings"
)

// memRepo is an in-memory Repository for the service tests.
//
// It exists so the parts of this module that matter — the classification rule,
// the clock arithmetic, the reference resolution, the clone — are tested
// directly rather than through SQL. A bug in ComputeClocks is a bug in what a
// bank tells its supervisor; it should not be reachable only via a database
// fixture.
type memRepo struct {
	exercises      map[int64]Exercise
	objectives     map[int64][]Objective
	phases         map[int64][]Phase
	injects        map[int64]Inject
	responses      map[int64]Response
	decisions      map[int64]Decision
	classification map[int64]Classification
	clocks         map[int64][]Clock
	findings       map[int64]Finding
	participants   map[int64][]Participant
	references     map[int64]Reference
	versions       map[int64][]Version
	chat           map[int64][]ChatTurn
	nextID         int64
}

func newMemRepo() *memRepo {
	return &memRepo{
		exercises:      map[int64]Exercise{},
		objectives:     map[int64][]Objective{},
		phases:         map[int64][]Phase{},
		injects:        map[int64]Inject{},
		responses:      map[int64]Response{},
		decisions:      map[int64]Decision{},
		classification: map[int64]Classification{},
		clocks:         map[int64][]Clock{},
		findings:       map[int64]Finding{},
		participants:   map[int64][]Participant{},
		references:     map[int64]Reference{},
		versions:       map[int64][]Version{},
		chat:           map[int64][]ChatTurn{},
	}
}

func (r *memRepo) id() int64 { r.nextID++; return r.nextID }

func (r *memRepo) CreateExercise(ex Exercise) (Exercise, error) {
	ex.ID = r.id()
	r.exercises[ex.ID] = ex
	return ex, nil
}

func (r *memRepo) UpdateExercise(ex Exercise) (Exercise, error) {
	if _, ok := r.exercises[ex.ID]; !ok {
		return Exercise{}, notFound("exercise")
	}
	r.exercises[ex.ID] = ex
	return r.GetExercise(ex.ID)
}

func (r *memRepo) GetExercise(id int64) (Exercise, error) {
	ex, ok := r.exercises[id]
	if !ok {
		return Exercise{}, notFound("exercise")
	}
	ex.InjectCount = len(r.injectsOf(id))
	ex.FindingCount = len(r.findingsOf(id))
	ex.LatestVersion = 0
	for _, v := range r.versions[id] {
		if v.Number > ex.LatestVersion {
			ex.LatestVersion = v.Number
		}
	}
	return ex, nil
}

func (r *memRepo) ExerciseByReference(reference string) (Exercise, error) {
	for _, ex := range r.exercises {
		if ex.Reference == reference {
			return ex, nil
		}
	}
	return Exercise{}, notFound("exercise")
}

func (r *memRepo) ListExercises() ([]Exercise, error) {
	out := []Exercise{}
	for id := range r.exercises {
		ex, _ := r.GetExercise(id)
		out = append(out, ex)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (r *memRepo) DeleteExercise(id int64) error {
	if _, ok := r.exercises[id]; !ok {
		return notFound("exercise")
	}
	delete(r.exercises, id)
	return nil
}

func (r *memRepo) ReplaceObjectives(exerciseID int64, objectives []Objective) ([]Objective, error) {
	for i := range objectives {
		objectives[i].ID = r.id()
		objectives[i].ExerciseID = exerciseID
	}
	r.objectives[exerciseID] = objectives
	return objectives, nil
}

func (r *memRepo) ListObjectives(exerciseID int64) ([]Objective, error) {
	return append([]Objective{}, r.objectives[exerciseID]...), nil
}

func (r *memRepo) SaveObjective(o Objective) (Objective, error) {
	list := r.objectives[o.ExerciseID]
	if o.ID == 0 {
		o.ID = r.id()
		r.objectives[o.ExerciseID] = append(list, o)
		return o, nil
	}
	for i := range list {
		if list[i].ID == o.ID {
			list[i] = o
			return o, nil
		}
	}
	return Objective{}, notFound("objective")
}

func (r *memRepo) ReplacePhases(exerciseID int64, phases []Phase) ([]Phase, error) {
	for i := range phases {
		phases[i].ID = r.id()
		phases[i].ExerciseID = exerciseID
	}
	r.phases[exerciseID] = phases
	return phases, nil
}

func (r *memRepo) ListPhases(exerciseID int64) ([]Phase, error) {
	return append([]Phase{}, r.phases[exerciseID]...), nil
}

func (r *memRepo) GetPhase(id int64) (Phase, error) {
	for _, list := range r.phases {
		for _, p := range list {
			if p.ID == id {
				return p, nil
			}
		}
	}
	return Phase{}, notFound("phase")
}

func (r *memRepo) SavePhase(p Phase) (Phase, error) {
	list := r.phases[p.ExerciseID]
	if p.ID == 0 {
		p.ID = r.id()
		r.phases[p.ExerciseID] = append(list, p)
		return p, nil
	}
	for i := range list {
		if list[i].ID == p.ID {
			list[i] = p
			return p, nil
		}
	}
	return Phase{}, notFound("phase")
}

func (r *memRepo) AppendInjects(injects []Inject) ([]Inject, error) {
	for i := range injects {
		injects[i].ID = r.id()
		r.injects[injects[i].ID] = injects[i]
	}
	return injects, nil
}

func (r *memRepo) injectsOf(exerciseID int64) []Inject {
	out := []Inject{}
	for _, in := range r.injects {
		if in.ExerciseID == exerciseID {
			out = append(out, in)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OffsetMinutes != out[j].OffsetMinutes {
			return out[i].OffsetMinutes < out[j].OffsetMinutes
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *memRepo) ListInjects(exerciseID int64) ([]Inject, error) {
	return r.injectsOf(exerciseID), nil
}

func (r *memRepo) GetInject(id int64) (Inject, error) {
	in, ok := r.injects[id]
	if !ok {
		return Inject{}, notFound("inject")
	}
	return in, nil
}

func (r *memRepo) SaveInject(in Inject) (Inject, error) {
	if in.ID == 0 {
		saved, err := r.AppendInjects([]Inject{in})
		if err != nil {
			return Inject{}, err
		}
		return saved[0], nil
	}
	if _, ok := r.injects[in.ID]; !ok {
		return Inject{}, notFound("inject")
	}
	r.injects[in.ID] = in
	return in, nil
}

func (r *memRepo) DeleteInject(id int64) error {
	if _, ok := r.injects[id]; !ok {
		return notFound("inject")
	}
	delete(r.injects, id)
	return nil
}

func (r *memRepo) SaveResponse(resp Response) (Response, error) {
	if existing, ok := r.responses[resp.InjectID]; ok {
		resp.ID = existing.ID
	} else {
		resp.ID = r.id()
	}
	r.responses[resp.InjectID] = resp
	return resp, nil
}

func (r *memRepo) ListResponses(exerciseID int64) ([]Response, error) {
	out := []Response{}
	for _, resp := range r.responses {
		if resp.ExerciseID == exerciseID {
			out = append(out, resp)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *memRepo) SaveDecision(d Decision) (Decision, error) {
	if d.ID == 0 {
		d.ID = r.id()
	} else if _, ok := r.decisions[d.ID]; !ok {
		return Decision{}, notFound("decision")
	}
	r.decisions[d.ID] = d
	return d, nil
}

func (r *memRepo) ListDecisions(exerciseID int64) ([]Decision, error) {
	out := []Decision{}
	for _, d := range r.decisions {
		if d.ExerciseID == exerciseID {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OffsetMinutes < out[j].OffsetMinutes })
	return out, nil
}

func (r *memRepo) DeleteDecision(id int64) error {
	if _, ok := r.decisions[id]; !ok {
		return notFound("decision")
	}
	delete(r.decisions, id)
	return nil
}

func (r *memRepo) GetClassification(exerciseID int64) (Classification, error) {
	c, ok := r.classification[exerciseID]
	if !ok {
		return Classification{ExerciseID: exerciseID}, nil
	}
	return c, nil
}

func (r *memRepo) SaveClassification(c Classification) (Classification, error) {
	r.classification[c.ExerciseID] = c
	return c, nil
}

func (r *memRepo) ReplaceClocks(exerciseID int64, clocks []Clock) ([]Clock, error) {
	prior := map[string]Clock{}
	for _, c := range r.clocks[exerciseID] {
		prior[c.Regime] = c
	}
	for i := range clocks {
		clocks[i].ID = r.id()
		clocks[i].ExerciseID = exerciseID
		if old, ok := prior[clocks[i].Regime]; ok {
			clocks[i].ActualOffset = old.ActualOffset
			clocks[i].Evidence = old.Evidence
			if clocks[i].Status != ClockNotApplicable {
				clocks[i] = ScoreClock(clocks[i], old.ActualOffset, false)
			}
		}
	}
	r.clocks[exerciseID] = clocks
	return clocks, nil
}

func (r *memRepo) ListClocks(exerciseID int64) ([]Clock, error) {
	return append([]Clock{}, r.clocks[exerciseID]...), nil
}

func (r *memRepo) SaveClock(c Clock) (Clock, error) {
	list := r.clocks[c.ExerciseID]
	for i := range list {
		if list[i].ID == c.ID {
			list[i] = c
			return c, nil
		}
	}
	return Clock{}, notFound("clock")
}

func (r *memRepo) findingsOf(exerciseID int64) []Finding {
	out := []Finding{}
	for _, f := range r.findings {
		if f.ExerciseID == exerciseID {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ordinal < out[j].Ordinal })
	return out
}

func (r *memRepo) SaveFinding(f Finding) (Finding, error) {
	if f.ID == 0 {
		f.ID = r.id()
	} else if _, ok := r.findings[f.ID]; !ok {
		return Finding{}, notFound("finding")
	}
	r.findings[f.ID] = f
	return f, nil
}

func (r *memRepo) ListFindings(exerciseID int64) ([]Finding, error) {
	return r.findingsOf(exerciseID), nil
}

func (r *memRepo) DeleteFinding(id int64) error {
	if _, ok := r.findings[id]; !ok {
		return notFound("finding")
	}
	delete(r.findings, id)
	return nil
}

func (r *memRepo) ReplaceParticipants(exerciseID int64, participants []Participant) ([]Participant, error) {
	for i := range participants {
		participants[i].ID = r.id()
		participants[i].ExerciseID = exerciseID
		participants[i].RoleLabel = RoleLabel(participants[i].RoleKey)
	}
	r.participants[exerciseID] = participants
	return participants, nil
}

func (r *memRepo) ListParticipants(exerciseID int64) ([]Participant, error) {
	return append([]Participant{}, r.participants[exerciseID]...), nil
}

func (r *memRepo) AddReference(ref Reference) (Reference, error) {
	ref.ID = r.id()
	r.references[ref.ID] = ref
	return ref, nil
}

func (r *memRepo) ListReferences(exerciseID int64) ([]Reference, error) {
	out := []Reference{}
	for _, ref := range r.references {
		if ref.ExerciseID == exerciseID {
			out = append(out, ref)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *memRepo) DeleteReference(id int64) error {
	if _, ok := r.references[id]; !ok {
		return notFound("reference")
	}
	delete(r.references, id)
	return nil
}

func (r *memRepo) ReplaceReferencesFor(exerciseID int64, ownerKind string, ownerID int64, refs []Reference) error {
	for id, ref := range r.references {
		if ref.ExerciseID == exerciseID && ref.OwnerKind == ownerKind && ref.OwnerID == ownerID {
			delete(r.references, id)
		}
	}
	for _, ref := range refs {
		ref.ExerciseID = exerciseID
		ref.OwnerKind = ownerKind
		ref.OwnerID = ownerID
		if _, err := r.AddReference(ref); err != nil {
			return err
		}
	}
	return nil
}

func (r *memRepo) CreateVersion(v Version) (Version, error) {
	v.ID = r.id()
	v.Number = len(r.versions[v.ExerciseID]) + 1
	r.versions[v.ExerciseID] = append(r.versions[v.ExerciseID], v)
	return v, nil
}

func (r *memRepo) SaveVersionSnapshot(versionID int64, snapshot string) error {
	for id, list := range r.versions {
		for i := range list {
			if list[i].ID == versionID {
				r.versions[id][i].Snapshot = snapshot
				return nil
			}
		}
	}
	return notFound("version")
}

func (r *memRepo) GetVersion(exerciseID int64, number int) (Version, error) {
	for _, v := range r.versions[exerciseID] {
		if v.Number == number {
			return v, nil
		}
	}
	return Version{}, notFound("version")
}

func (r *memRepo) LatestVersion(exerciseID int64) (Version, error) {
	list := r.versions[exerciseID]
	if len(list) == 0 {
		return Version{}, notFound("version")
	}
	return list[len(list)-1], nil
}

func (r *memRepo) ListVersions(exerciseID int64) ([]Version, error) {
	return append([]Version{}, r.versions[exerciseID]...), nil
}

func (r *memRepo) AppendChat(t ChatTurn) (ChatTurn, error) {
	t.ID = r.id()
	r.chat[t.ExerciseID] = append(r.chat[t.ExerciseID], t)
	return t, nil
}

func (r *memRepo) ListChat(exerciseID int64) ([]ChatTurn, error) {
	return append([]ChatTurn{}, r.chat[exerciseID]...), nil
}

func (r *memRepo) ClearChat(exerciseID int64, agent bool) error {
	kept := []ChatTurn{}
	for _, t := range r.chat[exerciseID] {
		if (t.Persona == PersonaAgent) != agent {
			kept = append(kept, t)
		}
	}
	r.chat[exerciseID] = kept
	return nil
}

// ---- test doubles for the catalog and the model ----

// stubResolver resolves anything in its map and refuses everything else, which
// is how the "the model invented a control id" path gets tested.
type stubResolver struct{ known map[string]RefTarget }

func newStubResolver(targets ...RefTarget) *stubResolver {
	m := map[string]RefTarget{}
	for _, t := range targets {
		m[t.Kind+"/"+strings.ToUpper(t.Ref)] = t
	}
	return &stubResolver{known: m}
}

func (s *stubResolver) Get(kind, ref string) (RefTarget, error) {
	t, ok := s.known[kind+"/"+strings.ToUpper(ref)]
	if !ok {
		return RefTarget{}, notFound("catalog item")
	}
	return t, nil
}

// Search matches on any query term rather than the whole phrase, which is the
// behaviour the real lexical search has and the behaviour callers rely on: a
// shortlist is built from a sentence, not from a keyword.
func (s *stubResolver) Search(kind, query string, limit int) ([]RefTarget, error) {
	out := []RefTarget{}
	for _, t := range s.known {
		if t.Kind != kind {
			continue
		}
		haystack := strings.ToLower(t.Title + " " + t.Summary)
		for _, term := range strings.Fields(strings.ToLower(query)) {
			if len(term) >= 4 && strings.Contains(haystack, term) {
				out = append(out, t)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
