package crisisexercise

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"grc/internal/aiprovider"
)

// ---- classification ----

// The combination rule is the thing teams get wrong on a whiteboard, so it is
// the thing this module must not get wrong in code.
func TestEvaluateAppliesTheDORACombinationRule(t *testing.T) {
	cases := []struct {
		name  string
		in    Classification
		major bool
		want  string
	}{
		{
			name:  "no critical services means the criteria are never reached",
			in:    Classification{ClientsMaterial: true, DataLossesMaterial: true, ReputationalMaterial: true},
			major: false,
			want:  "basic condition",
		},
		{
			name:  "data losses alone is sufficient",
			in:    Classification{CriticalServicesAffected: true, DataLossesMaterial: true},
			major: true,
			want:  "sufficient on its own",
		},
		{
			name:  "one criterion is not enough",
			in:    Classification{CriticalServicesAffected: true, ClientsMaterial: true},
			major: false,
			want:  "only one criterion",
		},
		{
			name:  "two criteria are enough",
			in:    Classification{CriticalServicesAffected: true, ClientsMaterial: true, DurationMaterial: true},
			major: true,
			want:  "threshold of two",
		},
		{
			name:  "critical services with nothing else met",
			in:    Classification{CriticalServicesAffected: true},
			major: false,
			want:  "no materiality criterion is met",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.in)
			if got.Major != tc.major {
				t.Errorf("Major = %v, want %v (rationale: %s)", got.Major, tc.major, got.Rationale)
			}
			if !strings.Contains(got.Rationale, tc.want) {
				t.Errorf("rationale %q does not mention %q", got.Rationale, tc.want)
			}
		})
	}
}

// ---- clocks ----

// The four-hour clock runs from classification and the twenty-four-hour cap
// from awareness, and the deadline is the earlier of the two. A team that
// classifies late has spent its own time, not bought more — and this is the
// arithmetic that makes that visible.
func TestComputeClocksTakesTheEarlierOfTheTwoDORATriggers(t *testing.T) {
	ex := Exercise{Jurisdiction: "LT", Supervision: "national"}

	prompt := Evaluate(Classification{
		AwareOffset: 0, ClassifiedOffset: 60,
		CriticalServicesAffected: true, DataLossesMaterial: true,
	})
	clocks := ComputeClocks(ex, prompt)
	initial := findClock(t, clocks, RegimeDORAInitial)
	if initial.DueOffset != 60+4*60 {
		t.Errorf("prompt classification: initial due = %d, want %d", initial.DueOffset, 60+4*60)
	}

	// Classified at T+21h: four hours from there is T+25h, past the 24-hour cap
	// from awareness, so the cap binds.
	late := Evaluate(Classification{
		AwareOffset: 0, ClassifiedOffset: 21 * 60,
		CriticalServicesAffected: true, DataLossesMaterial: true,
	})
	clocks = ComputeClocks(ex, late)
	initial = findClock(t, clocks, RegimeDORAInitial)
	if initial.DueOffset != 24*60 {
		t.Errorf("late classification: initial due = %d, want %d", initial.DueOffset, 24*60)
	}
	if !strings.Contains(initial.Basis, "outer cap") {
		t.Errorf("late classification basis does not explain the cap: %q", initial.Basis)
	}
}

func TestComputeClocksTurnsOffTheDORAObligationWhenNotMajor(t *testing.T) {
	clocks := ComputeClocks(Exercise{Jurisdiction: "LV"}, Classification{CriticalServicesAffected: false})

	initial := findClock(t, clocks, RegimeDORAInitial)
	if initial.Status != ClockNotApplicable {
		t.Errorf("initial notification status = %q, want %q", initial.Status, ClockNotApplicable)
	}
	// The internal escalation is not a DORA obligation and must survive: an
	// incident serious enough to classify is serious enough to tell the
	// management body about.
	escalation := findClock(t, clocks, RegimeManagementBody)
	if escalation.Status != ClockPending {
		t.Errorf("management body escalation status = %q, want %q", escalation.Status, ClockPending)
	}
}

func TestComputeClocksNamesTheRightBalticAuthorities(t *testing.T) {
	cases := map[string]struct{ competent, csirt string }{
		"LT": {"Lietuvos bankas", "CERT-LT"},
		"LV": {"Latvijas Banka", "CERT.LV"},
		"EE": {"Finantsinspektsioon", "CERT-EE"},
	}
	for code, want := range cases {
		clocks := ComputeClocks(Exercise{Jurisdiction: code}, Classification{
			CriticalServicesAffected: true, DataLossesMaterial: true, NIS2Significant: true,
		})
		initial := findClock(t, clocks, RegimeDORAInitial)
		if !strings.Contains(initial.Authority, want.competent) {
			t.Errorf("%s: DORA notification owed to %q, want it to name %q", code, initial.Authority, want.competent)
		}
		early := findClock(t, clocks, RegimeNIS2Early)
		if !strings.Contains(early.Authority, want.csirt) {
			t.Errorf("%s: NIS2 early warning owed to %q, want it to name %q", code, early.Authority, want.csirt)
		}
	}
}

func TestComputeClocksAppliesECBOnlyToSignificantInstitutions(t *testing.T) {
	major := Classification{CriticalServicesAffected: true, DataLossesMaterial: true}

	lsi := findClock(t, ComputeClocks(Exercise{Jurisdiction: "EE", Supervision: "national"}, major), RegimeECBSSM)
	if lsi.Status != ClockNotApplicable {
		t.Errorf("less significant institution: ECB clock status = %q, want %q", lsi.Status, ClockNotApplicable)
	}
	si := findClock(t, ComputeClocks(Exercise{Jurisdiction: "EE", Supervision: "significant"}, major), RegimeECBSSM)
	if si.Status != ClockPending {
		t.Errorf("significant institution: ECB clock status = %q, want %q", si.Status, ClockPending)
	}
	// The placeholder must announce itself. A rehearsed deadline that turns out
	// to be wrong is worse than no deadline at all.
	if !strings.Contains(si.Notes, "placeholder") {
		t.Errorf("ECB clock does not flag its due time as a placeholder: %q", si.Notes)
	}
}

func TestScoreClockGradesAgainstTheDeadline(t *testing.T) {
	c := Clock{DueOffset: 240, Status: ClockPending}

	if got := ScoreClock(c, 200, false); got.Status != ClockMet {
		t.Errorf("on time: status = %q, want %q", got.Status, ClockMet)
	}
	late := ScoreClock(c, 315, false)
	if late.Status != ClockMissed {
		t.Errorf("late: status = %q, want %q", late.Status, ClockMissed)
	}
	if late.LatenessMinutes() != 75 {
		t.Errorf("lateness = %d, want 75", late.LatenessMinutes())
	}
	if got := ScoreClock(c, -1, false); got.Status != ClockPending {
		t.Errorf("unsent while running: status = %q, want %q", got.Status, ClockPending)
	}
	if got := ScoreClock(c, -1, true); got.Status != ClockMissed {
		t.Errorf("unsent after the exercise ended: status = %q, want %q", got.Status, ClockMissed)
	}
	na := Clock{Status: ClockNotApplicable, DueOffset: 240}
	if got := ScoreClock(na, 10, true); got.Status != ClockNotApplicable {
		t.Error("a not-applicable clock should not be re-scored")
	}
}

func TestFormatOffsetReadsLikeAnExerciseLog(t *testing.T) {
	cases := map[int]string{-1: "—", 0: "T+0m", 45: "T+45m", 60: "T+1h", 255: "T+4h 15m", 1440: "T+1d", 1500: "T+1d 1h"}
	for minutes, want := range cases {
		if got := FormatOffset(minutes); got != want {
			t.Errorf("FormatOffset(%d) = %q, want %q", minutes, got, want)
		}
	}
}

// ---- references ----

func TestResolveMarksAnInventedControlUnknown(t *testing.T) {
	resolver := newStubResolver(RefTarget{Kind: RefControl, Ref: "IR-4", Title: "Incident Handling", URL: "/controls/detail/IR-4"})

	real := Resolve(resolver, Reference{RefKind: RefControl, Ref: "IR-4"})
	if !real.Known {
		t.Error("IR-4 should have resolved")
	}
	if real.Title != "Incident Handling" || real.URL == "" {
		t.Errorf("resolved reference is missing its title or link: %+v", real)
	}

	invented := Resolve(resolver, Reference{RefKind: RefControl, Ref: "IR-99"})
	if invented.Known {
		t.Error("IR-99 does not exist and must not be reported as resolved")
	}
}

func TestResolveReadsTheSeededAuthorityCatalogWithoutADatabase(t *testing.T) {
	// The point of seeding the citation catalog is that an entity can cite DORA
	// Article 18 on a fresh installation, without first having uploaded DORA.
	ref := Resolve(nil, Reference{RefKind: RefAuthority, Ref: "dora-art-18"})
	if !ref.Known {
		t.Fatal("a seeded authority must resolve with no catalog resolver at all")
	}
	if !strings.Contains(ref.Title, "Article 18") {
		t.Errorf("title = %q, want it to name Article 18", ref.Title)
	}
	if ref.Note == "" || ref.URL == "" {
		t.Errorf("seeded authority lost its summary or link: %+v", ref)
	}
}

func TestResolveWithoutAResolverDoesNotClaimAReferenceIsGood(t *testing.T) {
	ref := Resolve(nil, Reference{RefKind: RefControl, Ref: "AC-2"})
	if ref.Known {
		t.Error("with no resolver a catalog reference is unverified, not verified")
	}
}

func TestSuggestOffersBothCatalogAndFrameworkCandidates(t *testing.T) {
	resolver := newStubResolver(
		RefTarget{Kind: RefControl, Ref: "IR-6", Title: "Incident Reporting", Summary: "Report incidents to authorities"},
		RefTarget{Kind: RefNFR, Ref: "NFR-INC-01", Title: "Incident reporting", Summary: "Report incidents within the regulatory window"},
	)
	got := Suggest(resolver, "incident reporting to the competent authority", 3)

	kinds := map[string]bool{}
	for _, c := range got {
		kinds[c.Kind] = true
	}
	if !kinds[RefControl] || !kinds[RefNFR] {
		t.Errorf("shortlist %v does not reach the installation's own catalogs", kinds)
	}
	if !kinds[RefAuthority] {
		t.Errorf("shortlist %v does not reach the seeded framework catalog", kinds)
	}
}

func TestCoverageInvertsTheCitationsWithoutOverstatingFailure(t *testing.T) {
	refs := []Reference{
		{RefKind: RefControl, Ref: "IR-4", Title: "Incident Handling", OwnerKind: OwnerInject, OwnerID: 1, Known: true},
		{RefKind: RefControl, Ref: "IR-4", OwnerKind: OwnerPhase, OwnerID: 2, Known: true},
		{RefKind: RefControl, Ref: "IR-4", OwnerKind: OwnerFinding, OwnerID: 3, Known: true},
		{RefKind: RefAuthority, Ref: "dora-art-19", Title: "DORA Article 19", OwnerKind: OwnerClock, OwnerID: 4, Known: true},
	}
	rows := Coverage(refs)

	var ir4 CoverageRow
	for _, r := range rows {
		if r.Ref == "IR-4" {
			ir4 = r
		}
	}
	if ir4.Citations != 3 {
		t.Errorf("IR-4 citations = %d, want 3", ir4.Citations)
	}
	if ir4.Findings != 1 {
		t.Errorf("IR-4 findings = %d, want 1 — only the finding's own citation counts", ir4.Findings)
	}
	if ir4.Title != "Incident Handling" {
		t.Errorf("IR-4 lost its title when a later citation had none: %q", ir4.Title)
	}
}

// ---- service ----

func newTestService(t *testing.T, resolver CatalogResolver, asker Asker) (*Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	svc := NewService(repo, resolver, asker)
	// A fixed clock keeps the generated references deterministic.
	svc.now = func() time.Time { return time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC) }
	return svc, repo
}

func TestCreateFromAScenarioSeedsTheArcObjectivesAndCitations(t *testing.T) {
	svc, repo := newTestService(t, nil, nil)

	ex, err := svc.Create(CreateInput{
		Title:        "Annual crisis simulation",
		ScenarioKey:  "ransomware-core-banking",
		Jurisdiction: "LV",
		EntityName:   "Example Bank AS",
		CreatedBy:    "auditor",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if ex.Reference != "CX-2026-01" {
		t.Errorf("reference = %q, want CX-2026-01", ex.Reference)
	}
	if ex.ThreatActor == "" || ex.CriticalFunctions == "" {
		t.Error("the scenario's intelligence did not reach the exercise")
	}

	phases, _ := repo.ListPhases(ex.ID)
	if len(phases) != len(DefaultPhases()) {
		t.Errorf("phases = %d, want the whole arc (%d)", len(phases), len(DefaultPhases()))
	}
	// The seams are the point of the arc; losing one of them silently would be
	// the most damaging regression this module could have.
	for _, key := range []string{PhaseClassification, PhaseCrisis, PhaseBoard, PhaseAuthorities} {
		if !hasPhase(phases, key) {
			t.Errorf("the default arc is missing the %q phase", key)
		}
	}

	objectives, _ := repo.ListObjectives(ex.ID)
	if len(objectives) == 0 {
		t.Fatal("the scenario's objectives did not reach the exercise")
	}
	if objectives[0].Code != "OBJ-001" || objectives[0].Rating != RatingUntested {
		t.Errorf("first objective = %+v, want OBJ-001 untested", objectives[0])
	}

	refs, _ := repo.ListReferences(ex.ID)
	byOwner := map[string]int{}
	for _, r := range refs {
		byOwner[r.OwnerKind]++
	}
	for _, owner := range []string{OwnerExercise, OwnerObjective, OwnerPhase, OwnerClock} {
		if byOwner[owner] == 0 {
			t.Errorf("nothing was cited at the %q level; a new exercise should already know what it answers to", owner)
		}
	}
}

func TestCreateAllocatesSequentialReferences(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	for i, want := range []string{"CX-2026-01", "CX-2026-02", "CX-2026-03"} {
		ex, err := svc.Create(CreateInput{Title: "Exercise"})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if ex.Reference != want {
			t.Errorf("reference %d = %q, want %q", i, ex.Reference, want)
		}
	}
}

func TestSaveClassificationRebuildsTheClocksAndKeepsTheEvidence(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, err := svc.Create(CreateInput{Title: "Ransomware", Jurisdiction: "LT", Supervision: "national"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, clocks, err := svc.SaveClassification(ex.ID, Classification{
		AwareOffset: 30, ClassifiedOffset: 90,
		CriticalServicesAffected: true, ClientsMaterial: true, DurationMaterial: true,
	}, "facilitator")
	if err != nil {
		t.Fatalf("SaveClassification: %v", err)
	}

	initial := findClock(t, clocks, RegimeDORAInitial)
	if initial.DueOffset != 90+240 {
		t.Errorf("initial due = %d, want %d", initial.DueOffset, 90+240)
	}

	if _, err := svc.RecordNotification(ex.ID, initial.ID, 200, "sent by the compliance lead through the portal"); err != nil {
		t.Fatalf("RecordNotification: %v", err)
	}

	// Reclassifying is normal — facts develop. What must not happen is losing
	// the record that a notification was sent, because that record is the only
	// evidence the exercise produced about it.
	_, clocks, err = svc.SaveClassification(ex.ID, Classification{
		AwareOffset: 30, ClassifiedOffset: 90,
		CriticalServicesAffected: true, ClientsMaterial: true, DurationMaterial: true,
		DataLossesMaterial: true,
	}, "facilitator")
	if err != nil {
		t.Fatalf("re-classify: %v", err)
	}
	initial = findClock(t, clocks, RegimeDORAInitial)
	if initial.ActualOffset != 200 {
		t.Errorf("reclassification lost the notification time: got %d, want 200", initial.ActualOffset)
	}
	if initial.Evidence == "" {
		t.Error("reclassification lost the evidence recorded against the clock")
	}
	if initial.Status != ClockMet {
		t.Errorf("status after reclassification = %q, want %q", initial.Status, ClockMet)
	}
}

func TestDossierAssemblesTheExerciseAndCountsHonestly(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop", Jurisdiction: "EE"})
	phases, _ := svc.ListPhases(ex.ID)

	first, err := svc.SaveInject(ex.ID, Inject{
		PhaseID: phases[0].ID, Title: "First alert", Body: "EDR: suspicious archive staged",
		OffsetMinutes: 10, Type: InjectEvent, ExpectedActions: "Triage and escalate",
	})
	if err != nil {
		t.Fatalf("SaveInject: %v", err)
	}
	if _, err := svc.SaveInject(ex.ID, Inject{
		PhaseID: phases[0].ID, Title: "Analyst on leave", Body: "…", OffsetMinutes: 20, Type: InjectStress,
	}); err != nil {
		t.Fatalf("SaveInject stress: %v", err)
	}

	if _, err := svc.RecordResponse(first.ID, Response{Outcome: OutcomeAsExpected, RespondedOffset: 22,
		ActualActions: "Escalated to the incident manager"}); err != nil {
		t.Fatalf("RecordResponse: %v", err)
	}

	d, err := svc.Dossier(ex.ID, 0)
	if err != nil {
		t.Fatalf("Dossier: %v", err)
	}
	if d.Stats.Injects != 2 || d.Stats.InjectsPlayed != 1 || d.Stats.AsExpected != 1 {
		t.Errorf("stats = %+v, want 2 injects, 1 played, 1 as expected", d.Stats)
	}
	if d.Stats.StressInjects != 1 {
		t.Errorf("stress injects = %d, want 1", d.Stats.StressInjects)
	}
	// Performance is computed over what was played, not over what was written:
	// crediting an undelivered inject either way would be an invention.
	if d.Stats.PerformancePercent() != 100 || d.Stats.PlayedPercent() != 50 {
		t.Errorf("performance = %d%%, played = %d%%; want 100%% and 50%%",
			d.Stats.PerformancePercent(), d.Stats.PlayedPercent())
	}

	if len(d.Phases[0].Injects) != 2 {
		t.Errorf("injects did not attach to their phase: %d", len(d.Phases[0].Injects))
	}
	if d.Phases[0].Injects[0].Response == nil {
		t.Error("the response did not attach to its inject")
	}
}

func TestCloneCarriesTheDesignAndLeavesTheObservationsBehind(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, _ := svc.Create(CreateInput{Title: "Annual simulation", ScenarioKey: "ransomware-core-banking", Jurisdiction: "LT"})
	phases, _ := svc.ListPhases(ex.ID)

	inject, _ := svc.SaveInject(ex.ID, Inject{PhaseID: phases[0].ID, Title: "Detonation", Body: "…", OffsetMinutes: 5})
	if _, err := svc.RecordResponse(inject.ID, Response{Outcome: OutcomeMissed, ActualActions: "nobody noticed"}); err != nil {
		t.Fatalf("RecordResponse: %v", err)
	}
	if _, err := svc.SaveFinding(ex.ID, Finding{Title: "No out-of-hours escalation path", Severity: SeverityHigh}); err != nil {
		t.Fatalf("SaveFinding: %v", err)
	}
	objectives, _ := svc.ListObjectives(ex.ID)
	objectives[0].Rating = RatingNotMet
	if _, err := svc.SaveObjective(ex.ID, objectives[0]); err != nil {
		t.Fatalf("SaveObjective: %v", err)
	}

	clone, err := svc.Clone(ex.ID, "", "facilitator")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if clone.ID == ex.ID || clone.Reference == ex.Reference {
		t.Fatal("a clone must be a new exercise with its own reference")
	}
	if clone.Status != StatusDraft {
		t.Errorf("clone status = %q, want %q", clone.Status, StatusDraft)
	}

	d, err := svc.Dossier(clone.ID, 0)
	if err != nil {
		t.Fatalf("clone dossier: %v", err)
	}
	if d.Stats.Injects != 1 {
		t.Errorf("clone injects = %d, want the design carried over", d.Stats.Injects)
	}
	if d.Stats.InjectsPlayed != 0 {
		t.Error("the clone inherited last year's observations, which would corrupt the comparison")
	}
	if d.Stats.Findings != 0 {
		t.Error("the clone inherited last year's findings")
	}
	for _, o := range d.Objectives {
		if o.Rating != RatingUntested {
			t.Errorf("objective %s came across already rated %q", o.Code, o.Rating)
		}
	}
	if len(d.References) == 0 {
		t.Error("the clone lost the citations, which is most of what makes it worth cloning")
	}

	// The original must be untouched: last year's report has to keep saying
	// what it said.
	original, _ := svc.Dossier(ex.ID, 0)
	if original.Stats.Findings != 1 || original.Stats.InjectsPlayed != 1 {
		t.Errorf("cloning disturbed the original: %+v", original.Stats)
	}
}

func TestCutVersionSnapshotsTheExerciseAgainstLaterEdits(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, _ := svc.Create(CreateInput{Title: "Simulation", Jurisdiction: "LV"})
	if _, err := svc.SaveFinding(ex.ID, Finding{Title: "Escalation depends on one person", Severity: SeverityHigh}); err != nil {
		t.Fatalf("SaveFinding: %v", err)
	}

	v, err := svc.CutVersion(ex.ID, VersionAfterAction, "issued to the board", "facilitator")
	if err != nil {
		t.Fatalf("CutVersion: %v", err)
	}
	if v.Number != 1 || v.Snapshot == "" {
		t.Fatalf("version = %+v, want a numbered snapshot", v)
	}

	if _, err := svc.SaveFinding(ex.ID, Finding{Title: "Added afterwards", Severity: SeverityLow}); err != nil {
		t.Fatalf("SaveFinding: %v", err)
	}

	snapshot, err := svc.Dossier(ex.ID, 1)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(snapshot.Findings) != 1 {
		t.Errorf("the circulated version changed under the reader: %d findings, want 1", len(snapshot.Findings))
	}
	live, _ := svc.Dossier(ex.ID, 0)
	if len(live.Findings) != 2 {
		t.Errorf("live record = %d findings, want 2", len(live.Findings))
	}
}

func TestAddReferenceRejectsAnEmptyIdentifierAndResolvesTheRest(t *testing.T) {
	resolver := newStubResolver(RefTarget{Kind: RefNFR, Ref: "NFR-IR-02", Title: "Escalation", URL: "/security-nfrs?nfr=NFR-IR-02"})
	svc, _ := newTestService(t, resolver, nil)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop"})

	if _, err := svc.AddReference(ex.ID, Reference{OwnerKind: OwnerExercise, OwnerID: ex.ID, RefKind: RefNFR, Ref: "  "}); err == nil {
		t.Error("an empty reference should be refused")
	}

	saved, err := svc.AddReference(ex.ID, Reference{OwnerKind: OwnerExercise, OwnerID: ex.ID, RefKind: RefNFR, Ref: "NFR-IR-02"})
	if err != nil {
		t.Fatalf("AddReference: %v", err)
	}
	if !saved.Known || saved.Title != "Escalation" || saved.Source != SourceManual {
		t.Errorf("reference = %+v, want a resolved manual citation", saved)
	}
}

// ---- design ----

// stubAsker returns a canned answer and records what it was asked.
type stubAsker struct {
	reply   string
	refused bool
	err     error
	prompts []string
	systems []string
}

func (s *stubAsker) Available() bool  { return true }
func (s *stubAsker) Describe() string { return "stub model" }
func (s *stubAsker) Ask(_ context.Context, req aiprovider.Request) (aiprovider.Response, error) {
	s.prompts = append(s.prompts, req.Prompt)
	s.systems = append(s.systems, req.System)
	if s.err != nil {
		return aiprovider.Response{}, s.err
	}
	return aiprovider.Response{Text: s.reply, Model: "stub-1", Refused: s.refused}, nil
}

func TestDesignStoresProvenanceAndRefusesInventedReferences(t *testing.T) {
	resolver := newStubResolver(RefTarget{Kind: RefControl, Ref: "IR-4", Title: "Incident Handling"})
	asker := &stubAsker{reply: "Here you go:\n```json\n" + `[
	  {"title":"EDR alert","body":"Suspicious archive staged on FS01","channel":"siem_alert",
	   "from":"SOC tooling","to":"soc_lead","type":"event","offset_within_phase":5,
	   "expected_actions":"Triage within 15 minutes and escalate","expected_decision":"",
	   "decision_owner":"","evaluation_notes":"Watch for closure as a false positive",
	   "confidence":"high",
	   "references":[{"kind":"control","ref":"IR-4","why":"incident handling"},
	                 {"kind":"control","ref":"XX-99","why":"invented"}]}
	]` + "\n```"}

	svc, _ := newTestService(t, resolver, asker)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop", Jurisdiction: "LT"})

	result, err := svc.Design(context.Background(), ex.ID, DesignInput{
		PhaseKeys: []string{PhaseDetection}, InjectsPerPhase: 1,
	})
	if err != nil {
		t.Fatalf("Design: %v", err)
	}
	if len(result.Injects) != 1 {
		t.Fatalf("injects = %d, want 1", len(result.Injects))
	}

	in := result.Injects[0]
	if !in.AIGenerated || in.Model != "stub-1" || in.PromptHash == "" {
		t.Errorf("provenance missing: generated=%v model=%q hash=%q", in.AIGenerated, in.Model, in.PromptHash)
	}
	if in.Confidence != ConfidenceHigh {
		t.Errorf("confidence = %q, want %q", in.Confidence, ConfidenceHigh)
	}
	if in.Code != "INJ-001" {
		t.Errorf("code = %q, want INJ-001", in.Code)
	}

	refs, _ := svc.ListReferences(ex.ID)
	var real, invented *Reference
	for i := range refs {
		if refs[i].OwnerKind != OwnerInject {
			continue
		}
		switch refs[i].Ref {
		case "IR-4":
			real = &refs[i]
		case "XX-99":
			invented = &refs[i]
		}
	}
	if real == nil || !real.Known {
		t.Error("the real control reference did not resolve")
	}
	if invented == nil {
		t.Fatal("the invented reference was dropped; it should be kept and flagged so a reviewer sees it")
	}
	if invented.Known {
		t.Error("an invented control identifier was recorded as if it resolved")
	}
	if invented.Source != SourceModel {
		t.Errorf("source = %q, want %q", invented.Source, SourceModel)
	}
}

func TestDesignPromptCarriesTheJurisdictionAndTheGuidance(t *testing.T) {
	asker := &stubAsker{reply: `[{"title":"x","body":"y","offset_within_phase":0}]`}
	svc, _ := newTestService(t, nil, asker)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop", Jurisdiction: "LV", EntityName: "Example Banka"})

	if _, err := svc.Design(context.Background(), ex.ID, DesignInput{
		PhaseKeys: []string{PhaseCommunications}, InjectsPerPhase: 1,
		Guidance: "the CISO is unreachable from T+90",
	}); err != nil {
		t.Fatalf("Design: %v", err)
	}
	if len(asker.prompts) != 1 {
		t.Fatalf("asked %d times, want 1", len(asker.prompts))
	}
	prompt := asker.prompts[0]
	for _, want := range []string{"Latvijas Banka", "CERT.LV", "Russian", "Example Banka", "unreachable from T+90"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not carry %q — the model cannot write a locally credible inject without it", want)
		}
	}
	// The safety framing is not optional on a module whose adversary persona is
	// asked, by design, what an attacker does next.
	if !strings.Contains(asker.systems[0], "never invent an identifier") &&
		!strings.Contains(strings.ToLower(asker.systems[0]), "never invent an identifier") {
		t.Error("the design system prompt no longer forbids invented identifiers")
	}
}

func TestDesignLeavesPopulatedPhasesAloneUnlessAskedToRegenerate(t *testing.T) {
	asker := &stubAsker{reply: `[{"title":"generated","body":"y","offset_within_phase":0}]`}
	svc, _ := newTestService(t, nil, asker)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop"})
	phases, _ := svc.ListPhases(ex.ID)

	handwritten, err := svc.SaveInject(ex.ID, Inject{PhaseID: phases[0].ID, Title: "written by a person", Body: "…"})
	if err != nil {
		t.Fatalf("SaveInject: %v", err)
	}

	// With no explicit selection, generation fills the phases that have nothing.
	if _, err := svc.Design(context.Background(), ex.ID, DesignInput{InjectsPerPhase: 1}); err != nil {
		t.Fatalf("Design: %v", err)
	}
	injects, _ := svc.ListInjects(ex.ID)
	if !hasInject(injects, handwritten.ID) {
		t.Fatal("generation destroyed a hand-written inject in a phase it should have skipped")
	}

	// And a regeneration of that phase must still leave the hand-written one.
	if _, err := svc.Design(context.Background(), ex.ID, DesignInput{
		PhaseKeys: []string{phases[0].Key}, InjectsPerPhase: 1, Regenerate: true,
	}); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	injects, _ = svc.ListInjects(ex.ID)
	if !hasInject(injects, handwritten.ID) {
		t.Error("regeneration overwrote a hand-written inject; a designer's edit is a decision")
	}
}

func TestDesignRefusesWithoutAProvider(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop"})

	_, err := svc.Design(context.Background(), ex.ID, DesignInput{})
	var invalidErr ErrInvalid
	if err == nil || !asInvalid(err, &invalidErr) {
		t.Fatalf("err = %v, want an ErrInvalid explaining that no provider is configured", err)
	}
	if !strings.Contains(err.Error(), "by hand") {
		t.Errorf("the error should point at the manual path: %q", err.Error())
	}
}

func TestParseInjectReplyToleratesFencesAndProseAndDropsEmptyInjects(t *testing.T) {
	raw := "Sure — here are the injects.\n```json\n" + `[
	  {"title":"Real","body":"content"},
	  {"title":"","body":""},
	  {"title":"Also real","body":"more"}
	]` + "\n```\nLet me know if you want more."

	replies, err := ParseInjectReply(raw)
	if err != nil {
		t.Fatalf("ParseInjectReply: %v", err)
	}
	if len(replies) != 2 {
		t.Errorf("got %d injects, want 2 — the empty one is a parse artefact, not an inject", len(replies))
	}

	if _, err := ParseInjectReply("I would rather not."); err == nil {
		t.Error("a prose-only answer should be an error, not an empty MSEL")
	}
}

func TestExtractJSONArrayIgnoresBracketsInsideStrings(t *testing.T) {
	raw := `[{"body":"the alert said [CRITICAL] and then ] nothing"}]`
	if got := extractJSONArray(raw); got != raw {
		t.Errorf("extractJSONArray truncated at a bracket inside a string:\ngot  %s\nwant %s", got, raw)
	}
	var out []map[string]string
	if err := json.Unmarshal([]byte(extractJSONArray(raw)), &out); err != nil {
		t.Fatalf("the extracted text is not valid JSON: %v", err)
	}
}

// ---- personas ----

func TestEveryPersonaHasASystemPromptAndSomethingToAskIt(t *testing.T) {
	for _, p := range Personas() {
		if strings.TrimSpace(p.System) == "" {
			t.Errorf("persona %q has no system prompt", p.Key)
		}
		if len(p.AskAbout) == 0 {
			t.Errorf("persona %q has no example questions; a persona nobody knows how to use is not offered", p.Key)
		}
		if strings.TrimSpace(p.Remit) == "" {
			t.Errorf("persona %q has no remit", p.Key)
		}
	}
	if len(InPlayPersonas()) == 0 {
		t.Error("no persona is available during delivery, which is when they matter most")
	}
}

// The adversary persona is asked, by design, what an attacker does next. Its
// prompt has to carry the constraint that keeps that narrative rather than
// operational.
func TestAdversaryPersonaRefusesToProduceCapability(t *testing.T) {
	p, ok := PersonaByKey(PersonaAdversary)
	if !ok {
		t.Fatal("the adversary persona is missing")
	}
	lower := strings.ToLower(p.System)
	for _, want := range []string{"exploit code", "malware", "narrative"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the adversary prompt no longer mentions %q", want)
		}
	}
}

func TestAdviseRecordsBothSidesAndKeepsPersonasSeparate(t *testing.T) {
	asker := &stubAsker{reply: "The chair will ask when you knew."}
	svc, _ := newTestService(t, nil, asker)
	ex, _ := svc.Create(CreateInput{Title: "Board simulation", Jurisdiction: "EE"})

	if _, err := svc.Advise(context.Background(), ex.ID, PersonaBoardTrainer, "the board phase",
		"What will the chair ask at T+4h?", "facilitator"); err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if _, err := svc.Advise(context.Background(), ex.ID, PersonaJournalist, "", "What is your follow-up?", "facilitator"); err != nil {
		t.Fatalf("Advise journalist: %v", err)
	}

	turns, _ := svc.ListChat(ex.ID)
	if len(turns) != 4 {
		t.Fatalf("turns = %d, want 4 (a question and an answer for each adviser)", len(turns))
	}

	// The brief goes in with the first question to each persona and not again.
	if !strings.Contains(asker.prompts[0], "Finantsinspektsioon") {
		t.Error("the first question to an adviser did not carry the exercise brief")
	}
	if !strings.Contains(asker.systems[0], "non-executive director") {
		t.Error("the board trainer's system prompt was not used")
	}
	if asker.systems[0] == asker.systems[1] {
		t.Error("two different advisers were given the same system prompt")
	}
	if !strings.Contains(asker.systems[0], "Say when you do not know") {
		t.Error("the standing constraints were not appended to the persona prompt")
	}
}

func TestAdviseRefusesAnEmptyQuestionAndAnUnknownAdviser(t *testing.T) {
	svc, _ := newTestService(t, nil, &stubAsker{reply: "…"})
	ex, _ := svc.Create(CreateInput{Title: "Tabletop"})

	if _, err := svc.Advise(context.Background(), ex.ID, PersonaCISO, "", "   ", ""); err == nil {
		t.Error("an empty question should be refused")
	}
	if _, err := svc.Advise(context.Background(), ex.ID, "nobody", "", "hello", ""); err == nil {
		t.Error("an unknown adviser should be refused")
	}
}

func TestDraftAfterActionNeedsSomethingToHaveHappened(t *testing.T) {
	svc, _ := newTestService(t, nil, &stubAsker{reply: "[]"})
	ex, _ := svc.Create(CreateInput{Title: "Tabletop"})

	_, err := svc.DraftAfterAction(context.Background(), ex.ID, "evaluator")
	if err == nil || !strings.Contains(err.Error(), "record what happened") {
		t.Fatalf("err = %v, want a refusal pointing at the empty observation log", err)
	}
}

func TestDraftAfterActionMarksItsFindingsAsTheModelsWork(t *testing.T) {
	asker := &stubAsker{reply: `[
	  {"title":"Escalation depended on one person","phase_key":"detection","category":"process",
	   "severity":"high","description":"No second name in the escalation path.",
	   "evidence":"INJ-001 observed: nobody escalated","root_cause":"The runbook names a person, not a rota",
	   "recommendation":"Add a deputy and a rota","suggested_owner":"soc_lead",
	   "references":[{"kind":"authority","ref":"dora-art-17","why":"escalation procedures"}]}
	]`}
	svc, _ := newTestService(t, nil, asker)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop", Jurisdiction: "LT"})
	phases, _ := svc.ListPhases(ex.ID)

	in, _ := svc.SaveInject(ex.ID, Inject{PhaseID: phases[0].ID, Title: "First alert", Body: "…"})
	if _, err := svc.RecordResponse(in.ID, Response{Outcome: OutcomeMissed, ActualActions: "nobody escalated"}); err != nil {
		t.Fatalf("RecordResponse: %v", err)
	}

	result, err := svc.DraftAfterAction(context.Background(), ex.ID, "evaluator")
	if err != nil {
		t.Fatalf("DraftAfterAction: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(result.Findings))
	}
	f := result.Findings[0]
	if !f.AIGenerated || f.Model != "stub-1" {
		t.Errorf("a drafted finding must be attributable: %+v", f)
	}
	if f.Severity != SeverityHigh || f.Code != "F-001" || f.Status != FindingOpen {
		t.Errorf("finding = %+v, want F-001 high and open", f)
	}
	if !strings.Contains(result.Summary, "model's proposal") {
		t.Errorf("summary should say these are drafts: %q", result.Summary)
	}

	refs, _ := svc.ListReferences(ex.ID)
	found := false
	for _, r := range refs {
		if r.OwnerKind == OwnerFinding && r.Ref == "dora-art-17" && r.Known {
			found = true
		}
	}
	if !found {
		t.Error("the finding's citation did not resolve against the seeded authority catalog")
	}
}

// ---- documents ----

func TestPlayerHandoutWithholdsTheAnswers(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop", ScenarioKey: "hacktivist-ddos-wave", Jurisdiction: "LV"})
	phases, _ := svc.ListPhases(ex.ID)
	if _, err := svc.SaveInject(ex.ID, Inject{
		PhaseID: phases[0].ID, Title: "Telegram post", Body: "Target list published",
		ExpectedActions: "SECRET-EXPECTED-ACTION", EvaluationNotes: "SECRET-EVALUATION-NOTE",
	}); err != nil {
		t.Fatalf("SaveInject: %v", err)
	}

	d, _ := svc.Dossier(ex.ID, 0)
	handout := PlayerHandoutMarkdown(d)

	for _, secret := range []string{"SECRET-EXPECTED-ACTION", "SECRET-EVALUATION-NOTE"} {
		if strings.Contains(handout, secret) {
			t.Errorf("the player handout leaks %q — an exercise whose players have read the answers is a rehearsal", secret)
		}
	}
	scenario, _ := ScenarioByKey("hacktivist-ddos-wave")
	if strings.Contains(handout, scenario.TrapDoor) {
		t.Error("the player handout leaks the control team's trap door")
	}
	if !strings.Contains(handout, "Ground rules") {
		t.Error("the player handout has no ground rules")
	}
}

func TestMSELCSVCarriesTheMarkingAndTheControllerColumns(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop", TLP: TLPAmberStr})
	phases, _ := svc.ListPhases(ex.ID)
	if _, err := svc.SaveInject(ex.ID, Inject{
		PhaseID: phases[0].ID, Title: "Alert", Body: "line one\nline two",
		ExpectedActions: "Escalate", OffsetMinutes: 15,
	}); err != nil {
		t.Fatalf("SaveInject: %v", err)
	}

	d, _ := svc.Dossier(ex.ID, 0)
	body, err := MSELCSV(d)
	if err != nil {
		t.Fatalf("MSELCSV: %v", err)
	}
	csv := string(body)
	if !strings.Contains(csv, TLPAmberStr) {
		t.Error("the controller's copy is not marked; a printed copy left on a table should say what it is")
	}
	if !strings.Contains(csv, "expected_actions") || !strings.Contains(csv, "Escalate") {
		t.Error("the controller's copy is missing the expected action, which is the column it exists for")
	}
	if !strings.Contains(csv, "T+15m") {
		t.Error("the controller's copy is missing the readable clock time")
	}
}

func TestReportHTMLShowsTheCoverageAndTheMissedClocks(t *testing.T) {
	resolver := newStubResolver(RefTarget{Kind: RefControl, Ref: "IR-6", Title: "Incident Reporting"})
	svc, _ := newTestService(t, resolver, nil)
	ex, _ := svc.Create(CreateInput{Title: "Simulation", Jurisdiction: "LT", Supervision: "national"})

	if _, err := svc.AddReference(ex.ID, Reference{OwnerKind: OwnerExercise, OwnerID: ex.ID, RefKind: RefControl, Ref: "IR-6"}); err != nil {
		t.Fatalf("AddReference: %v", err)
	}
	_, clocks, err := svc.SaveClassification(ex.ID, Classification{
		AwareOffset: 0, ClassifiedOffset: 60, CriticalServicesAffected: true, DataLossesMaterial: true,
	}, "facilitator")
	if err != nil {
		t.Fatalf("SaveClassification: %v", err)
	}
	initial := findClock(t, clocks, RegimeDORAInitial)
	if _, err := svc.RecordNotification(ex.ID, initial.ID, 500, "sent late"); err != nil {
		t.Fatalf("RecordNotification: %v", err)
	}

	d, _ := svc.Dossier(ex.ID, 0)
	html := ReportHTML(d)

	for _, want := range []string{"IR-6", "Incident Reporting", "Coverage", "Lietuvos bankas", "Late by", "Major incident"} {
		if !strings.Contains(html, want) {
			t.Errorf("the report does not contain %q", want)
		}
	}
	if !strings.Contains(html, d.Exercise.TLP) {
		t.Error("the report is not marked")
	}
}

func TestExercisePageEscapesUserSuppliedText(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ex, _ := svc.Create(CreateInput{Title: "Tabletop"})
	phases, _ := svc.ListPhases(ex.ID)
	if _, err := svc.SaveInject(ex.ID, Inject{
		PhaseID: phases[0].ID, Title: "Pasted from an incident report",
		Body: `</script><img src=x onerror=alert(1)>`,
	}); err != nil {
		t.Fatalf("SaveInject: %v", err)
	}

	d, _ := svc.Dossier(ex.ID, 0)
	page := exercisePageHTML(exercisePageData{Dossier: d})

	if strings.Contains(page, "</script><img") {
		t.Error("an inject body escaped the JSON payload block; this is a cross-site scripting bug")
	}
	if !strings.Contains(page, `<`) {
		t.Error("the payload does not appear to be escaped at all")
	}
}

// ---- catalog integrity ----

// Every citation the seeded phases and scenarios make must exist. A dangling
// key renders as an unresolved reference in a client's report, which is exactly
// the failure this module claims to prevent.
func TestSeededCitationsAllResolve(t *testing.T) {
	for _, p := range DefaultPhases() {
		for _, key := range p.Authorities {
			if _, ok := AuthorityByKey(key); !ok {
				t.Errorf("phase %q cites unknown authority %q", p.Key, key)
			}
		}
	}
	for _, s := range ScenarioLibrary() {
		for _, key := range s.Authorities {
			if _, ok := AuthorityByKey(key); !ok {
				t.Errorf("scenario %q cites unknown authority %q", s.Key, key)
			}
		}
		for _, o := range s.Objectives {
			for _, key := range o.Authorities {
				if _, ok := AuthorityByKey(key); !ok {
					t.Errorf("scenario %q objective cites unknown authority %q", s.Key, key)
				}
			}
		}
		if _, ok := FormatByKey(s.SuggestedFormat); !ok {
			t.Errorf("scenario %q suggests unknown format %q", s.Key, s.SuggestedFormat)
		}
		if s.TrapDoor == "" {
			t.Errorf("scenario %q has no trap door; a scenario with nothing to expose is a story", s.Key)
		}
	}
	for _, c := range ComputeClocks(Exercise{Jurisdiction: "LT", Supervision: "significant"},
		Classification{CriticalServicesAffected: true, DataLossesMaterial: true, PersonalDataBreach: true, NIS2Significant: true}) {
		if c.Source == "" {
			t.Errorf("clock %q cites no instrument", c.Regime)
			continue
		}
		if _, ok := AuthorityByKey(c.Source); !ok {
			t.Errorf("clock %q cites unknown authority %q", c.Regime, c.Source)
		}
	}
}

func TestEveryPhaseLeadRoleExists(t *testing.T) {
	for _, p := range DefaultPhases() {
		if RoleLabel(p.LeadRole) == p.LeadRole {
			t.Errorf("phase %q names lead role %q, which is not in the seat catalog", p.Key, p.LeadRole)
		}
	}
}

func TestJurisdictionByKeyFallsBackRatherThanReturningNothing(t *testing.T) {
	if got := JurisdictionByKey("LV"); got.CSIRT == "" || !strings.Contains(got.CSIRT, "CERT.LV") {
		t.Errorf("LV = %+v", got)
	}
	fallback := JurisdictionByKey("ZZ")
	if fallback.CompetentAuthority == "" || fallback.CSIRT == "" {
		t.Error("an unknown jurisdiction must still name authorities, so a scenario always has someone to notify")
	}
}

// ---- helpers ----

func findClock(t *testing.T, clocks []Clock, regime string) Clock {
	t.Helper()
	for _, c := range clocks {
		if c.Regime == regime {
			return c
		}
	}
	t.Fatalf("no clock for regime %q", regime)
	return Clock{}
}

func hasPhase(phases []Phase, key string) bool {
	for _, p := range phases {
		if p.Key == key {
			return true
		}
	}
	return false
}

func hasInject(injects []Inject, id int64) bool {
	for _, in := range injects {
		if in.ID == id {
			return true
		}
	}
	return false
}

func asInvalid(err error, target *ErrInvalid) bool {
	if e, ok := err.(ErrInvalid); ok {
		*target = e
		return true
	}
	return false
}

// The page script lives in a Go raw string literal, where a backslash is
// literal. Writing `\\'` there — the instinct when escaping an apostrophe —
// reaches the browser as an escaped backslash followed by a quote that ends the
// JavaScript string, and the whole page silently stops working: it renders, the
// payload is there, and nothing is interactive. Go's compiler cannot see it and
// no Go test exercises the browser, so the sequence is checked directly.
func TestPageScriptHasNoOverEscapedQuotes(t *testing.T) {
	for _, seq := range []string{`\\'`, `\\"`} {
		if idx := strings.Index(exercisePageScript, seq); idx >= 0 {
			start := idx - 60
			if start < 0 {
				start = 0
			}
			t.Errorf("exercisePageScript contains %s, which ends the JavaScript string early: …%s…",
				seq, exercisePageScript[start:idx+20])
		}
	}
}

// A crude structural check on the same file: every function the tab table names
// has to exist, because a typo there produces a page whose tabs do nothing.
func TestPageScriptDefinesEveryTabRenderer(t *testing.T) {
	for _, fn := range []string{
		"renderBrief", "renderRun", "renderClassification", "renderDecisions",
		"renderFindings", "renderCoverage", "renderAdvisers",
	} {
		if !strings.Contains(exercisePageScript, "function "+fn+"(") {
			t.Errorf("the tab table names %s but the script does not define it", fn)
		}
	}
}
