package assuranceplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func loadExamplePopulation(t *testing.T, svc *Service, id int64) PopulationDiff {
	t.Helper()
	text, err := svc.ExamplePopulationCSV(id)
	if err != nil {
		t.Fatalf("ExamplePopulationCSV: %v", err)
	}
	diff, err := svc.ApplyPopulation(id, PopulationImport{CSV: text}, "alice")
	if err != nil {
		t.Fatalf("ApplyPopulation: %v (%+v)", err, diff.Errors)
	}
	return diff
}

// frozenExample is the Appendix A plan, approved, with the example population
// of 80 Tier 1 and 240 Tier 2 assets loaded and frozen.
func frozenExample(t *testing.T, svc *Service) Plan {
	t.Helper()
	p := createExample(t, svc, "alice")
	loadExamplePopulation(t, svc, p.ID)
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(p.ID, "bob", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	p, err := svc.FreezePopulation(p.ID, "alice")
	if err != nil {
		t.Fatalf("FreezePopulation: %v", err)
	}
	return p
}

func itemByRef(t *testing.T, svc *Service, id int64, ref string) PopulationItem {
	t.Helper()
	items, err := svc.Population(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ItemRef == ref {
			return it
		}
	}
	t.Fatalf("no population item %s", ref)
	return PopulationItem{}
}

func TestExamplePlanIsSizedOnItsPlannedStrata(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	if len(p.Strata) != 2 || p.Strata[0].Code != "T1" || p.Strata[1].PlannedSize != 240 {
		t.Fatalf("strata = %+v", p.Strata)
	}
	v, err := svc.Sampling(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	t1, t2 := v.Strata[0], v.Strata[1]
	if t1.NSource != "planned" || t1.Size.Annual != 35 || t1.Tests != [4]int{14, 13, 11, 1} || t1.TotalTests != 39 {
		t.Errorf("Tier 1 = %+v", t1)
	}
	if t2.Size.Annual != 21 || t2.Tests != [4]int{5, 7, 7, 6} || t2.TotalTests != 25 {
		t.Errorf("Tier 2 = %+v", t2)
	}
	if v.TotalTests != 64 || pct(t1.Coverage) != "48.75%" || pct(t2.Coverage) != "10.42%" {
		t.Errorf("total %d, coverage %s / %s; want 64, 48.75%% / 10.42%%", v.TotalTests, pct(t1.Coverage), pct(t2.Coverage))
	}
	if t1.CumTarget != [4]float64{0.35, 0.7, 1, 1} || t2.CumTarget != [4]float64{0.15, 0.45, 0.75, 1} {
		t.Errorf("cumulative targets %v / %v", t1.CumTarget, t2.CumTarget)
	}
}

func TestPopulationImportPreviewsADiffAndAppliesIt(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	text, _ := svc.ExamplePopulationCSV(p.ID)
	preview, err := svc.PreviewPopulation(p.ID, PopulationImport{CSV: text})
	if err != nil || len(preview.Errors) != 0 || len(preview.Added) != 320 || preview.Applied {
		t.Fatalf("preview: %v, %d errors, %d added, applied=%v", err, len(preview.Errors), len(preview.Added), preview.Applied)
	}
	if items, _ := svc.Population(p.ID); len(items) != 0 {
		t.Fatal("a preview wrote the population")
	}
	if _, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: text, Base: preview.Base}, "alice"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	v, _ := svc.Sampling(p.ID)
	if v.Strata[0].NSource != "population" || v.Strata[0].WaveCounts != [4]int{28, 28, 24, 0} || v.Strata[1].WaveCounts != [4]int{36, 72, 72, 60} {
		t.Errorf("wave counts %v / %v", v.Strata[0].WaveCounts, v.Strata[1].WaveCounts)
	}
	if v.Strata[0].Size.Annual != 35 || v.TotalTests != 64 {
		t.Errorf("sized on the loaded population: %+v", v.Strata[0].Size)
	}
	for _, f := range Lint(mustGet(t, svc, p.ID), mustPopulation(t, svc, p.ID)) {
		if f.Code == "wave_counts" || f.Code == "stratum_empty" || f.Code == "no_wave" {
			t.Errorf("unexpected finding on the example population: %s", f.Message)
		}
	}

	// A stale preview is refused rather than applied over someone else's edit.
	if _, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: text, Base: preview.Base}, "alice"); err == nil {
		t.Error("an apply against a stale preview was accepted")
	} else {
		wantConflict(t, err, "changed since")
	}

	edit := "item_ref,name,stratum,wave,attested_on,internet_facing\n" +
		"T1-001,Renamed gateway,T1,Q1,,no\n" +
		"T1-081,New asset,Tier 1,Q3,,yes\n" +
		"T1-082,,T9,Q1,,\n" +
		"T1-083,,T1,Q5,,\n" +
		"T1-084,,T1,Q2,2027-02-30,\n" +
		"T1-085,,T1,Q2,,maybe\n" +
		"T1-081,Twice,T1,Q3,,\n"
	d, err := svc.PreviewPopulation(p.ID, PopulationImport{CSV: edit})
	if err != nil {
		t.Fatal(err)
	}
	messages := map[int]string{}
	for _, e := range d.Errors {
		messages[e.Line] += e.Message + " "
	}
	for line, want := range map[int]string{4: "not one of the plan's strata", 5: "not a quarter", 6: "not a date", 7: "not yes or no", 8: "listed twice"} {
		if !strings.Contains(messages[line], want) {
			t.Errorf("line %d: %q, want it to say %q", line, messages[line], want)
		}
	}
	if !slices.Equal(d.Added, []string{"T1-081"}) || len(d.Changed) != 1 || d.Changed[0].ItemRef != "T1-001" || len(d.Removed) != 319 {
		t.Errorf("diff added %v, changed %+v, removed %d", d.Added, d.Changed, len(d.Removed))
	}
	_, err = svc.ApplyPopulation(p.ID, PopulationImport{CSV: edit}, "alice")
	wantInvalid(t, err, "problem")
}

func mustGet(t *testing.T, svc *Service, id int64) Plan {
	t.Helper()
	p, err := svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustPopulation(t *testing.T, svc *Service, id int64) []PopulationItem {
	t.Helper()
	items, err := svc.Population(id)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestAPartialPasteChangesOnlyItsColumns(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	loadExamplePopulation(t, svc, p.ID)
	before := itemByRef(t, svc, p.ID, "T2-005")
	d, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: "Asset ID;1LoD attestation date;DQ status\nT2-005;2027-02-10;accepted\n", Merge: true}, "carol")
	if err != nil || !d.Applied || len(d.Removed) != 0 || len(d.Changed) != 1 {
		t.Fatalf("merge paste: %v %+v", err, d)
	}
	after := itemByRef(t, svc, p.ID, "T2-005")
	if after.AttestedOn != "2027-02-10" || after.DQStatus != DQAccepted || after.Name != before.Name || after.WaveQuarter != before.WaveQuarter {
		t.Errorf("after = %+v", after)
	}
	if n := len(mustPopulation(t, svc, p.ID)); n != 320 {
		t.Errorf("merge left %d items, want 320", n)
	}
	h, _ := svc.History(p.ID)
	if h[0].Event != "population" || h[0].Actor != "carol" || !strings.Contains(h[0].Note, "merge: added 0, changed 1, removed 0") {
		t.Errorf("history = %+v", h[0])
	}
}

func TestFreezeDrawsTheSeedOnceAfterApproval(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	_, err := svc.FreezePopulation(p.ID, "alice")
	wantConflict(t, err, "once the plan is approved")
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(p.ID, "bob", ""); err != nil {
		t.Fatal(err)
	}
	_, err = svc.FreezePopulation(p.ID, "alice")
	wantInvalid(t, err, "population is empty")

	loadExamplePopulation(t, svc, p.ID)
	frozen, err := svc.FreezePopulation(p.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(frozen.SelectionSeed) != 64 || frozen.FrozenBy != "alice" || frozen.FrozenAt == "" {
		t.Fatalf("frozen plan: seed %q, frozen %s by %s", frozen.SelectionSeed, frozen.FrozenAt, frozen.FrozenBy)
	}
	sum := sha256.Sum256([]byte(frozen.SelectionSeed))
	if frozen.SeedCommitment != hex.EncodeToString(sum[:]) || frozen.SeedRevealed {
		t.Errorf("commitment %q, revealed %v", frozen.SeedCommitment, frozen.SeedRevealed)
	}
	if pub := publicPlan(frozen); pub.SelectionSeed != "" || pub.SeedCommitment == "" {
		t.Error("readers see the seed before the last quarter is selected")
	}
	_, err = svc.FreezePopulation(p.ID, "alice")
	wantConflict(t, err, "was frozen")
	h, _ := svc.History(p.ID)
	if h[0].Event != eventFrozen || !strings.Contains(h[0].Note, populationDigest(mustPopulation(t, svc, p.ID), true)) {
		t.Errorf("freeze history = %+v", h[0])
	}
}

func TestAFrozenPopulationChangesMembershipOnlyUnderAChangeRequest(t *testing.T) {
	svc := newTestService(t)
	p := frozenExample(t, svc)
	add := "item_ref,stratum,wave\nT2-241,T2,Q4\n"
	d, err := svc.PreviewPopulation(p.ID, PopulationImport{CSV: add, Merge: true})
	if err != nil || len(d.Errors) != 1 || !strings.Contains(d.Errors[0].Message, "needs a change request") {
		t.Fatalf("adding to a frozen population: %v %+v", err, d.Errors)
	}
	d, err = svc.ApplyPopulation(p.ID, PopulationImport{CSV: "item_ref,attested_on\nT1-001,2027-02-01\n", Merge: true}, "alice")
	if err != nil || !d.Applied {
		t.Fatalf("an attestation on a frozen population was refused: %v", err)
	}
	if _, err := svc.ChangeRequest(p.ID, "two new Tier 2 assets", "alice"); err != nil {
		t.Fatal(err)
	}
	d, err = svc.ApplyPopulation(p.ID, PopulationImport{CSV: add, Merge: true}, "alice")
	if err != nil || !d.Applied {
		t.Fatalf("adding under a change request: %v %+v", err, d.Errors)
	}
	_, err = svc.SelectQuarter(p.ID, 1, "alice")
	wantConflict(t, err, "approve or return the revision first")
}

func TestSelectionTakesEachStratumsAllocationAndFreezesTheQuarter(t *testing.T) {
	svc := newTestService(t)
	p := frozenExample(t, svc)
	for _, flag := range []struct {
		ref string
		q   int
	}{{"T1-040", 1}, {"T2-200", 1}} {
		it := itemByRef(t, svc, p.ID, flag.ref)
		q, why := flag.q, "Internet-facing payments asset with a prior finding"
		if _, err := svc.UpdatePopulationItem(p.ID, it.ID, PopulationPatch{TopUpQuarter: &q, TopUpRationale: &why}, "alice"); err != nil {
			t.Fatalf("flag %s: %v", flag.ref, err)
		}
	}
	res, err := svc.SelectQuarter(p.ID, 1, "tess")
	if err != nil {
		t.Fatalf("SelectQuarter: %v", err)
	}
	count := map[string]int{}
	for _, s := range res.Picks {
		count[s.StratumCode+" "+s.Basis]++
		if s.Basis == BasisStatistical && (s.RankHash == "" || s.RankOrder == 0 || s.PlanVersion != 1) {
			t.Errorf("pick %+v lacks its rank or version", s)
		}
	}
	if count["T1 Statistical"] != 13 || count["T2 Statistical"] != 4 || count["T1 Risk-based top-up"] != 1 || count["T2 Risk-based top-up"] != 1 {
		t.Errorf("Q1 picks = %v, want 13 + 1 Tier 1 and 4 + 1 Tier 2", count)
	}
	if res.Plan.Status != StatusActive || res.Plan.ActivatedAt == "" {
		t.Errorf("the first selection did not activate the plan: %s", res.Plan.Status)
	}
	if len(res.Warnings) != 0 || len(res.Shortfalls) != 0 {
		t.Errorf("warnings %v, shortfalls %v", res.Warnings, res.Shortfalls)
	}
	_, err = svc.SelectQuarter(p.ID, 1, "tess")
	wantConflict(t, err, "is frozen")

	// Internal Audit re-performs the draw from the seed and the population.
	seed, err := svc.Seed(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wave []string
	for _, it := range mustPopulation(t, svc, p.ID) {
		if it.StratumCode == "T1" && it.WaveQuarter == 1 {
			wave = append(wave, it.ItemRef)
		}
	}
	var want, got []string
	for _, r := range Rank(seed, "T1", wave)[:13] {
		want = append(want, r.ItemRef)
	}
	for _, s := range res.Picks {
		if s.StratumCode == "T1" && s.Basis == BasisStatistical {
			got = append(got, s.ItemRef)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("re-performed Tier 1 picks %v, recorded %v", want, got)
	}

	// Every stratum-quarter gets exactly its q_i over the year.
	for q := 2; q <= 4; q++ {
		if _, err := svc.SelectQuarter(p.ID, q, "tess"); err != nil {
			t.Fatalf("select Q%d: %v", q, err)
		}
	}
	v, _ := svc.Sampling(p.ID)
	stat := map[string]*[4]int{"T1": {}, "T2": {}}
	for _, s := range v.Selections {
		if s.Basis == BasisStatistical {
			stat[s.StratumCode][s.Quarter-1]++
		}
	}
	if *stat["T1"] != v.Strata[0].Statistical || *stat["T2"] != v.Strata[1].Statistical {
		t.Errorf("picks per quarter %v / %v, allocation %v / %v", *stat["T1"], *stat["T2"], v.Strata[0].Statistical, v.Strata[1].Statistical)
	}
	if !v.SeedRevealed || v.Seed != seed {
		t.Error("the seed is not revealed once every quarter is selected")
	}
	h, _ := svc.History(p.ID)
	if !slices.ContainsFunc(h, func(e HistoryEntry) bool { return e.Event == "selection" && e.Subject == "Q1" && e.Actor == "tess" }) {
		t.Error("the Q1 selection is not in the history")
	}
}

func TestATopUpThatDuplicatesAPickIsRefused(t *testing.T) {
	svc := newTestService(t)
	p := frozenExample(t, svc)
	seed, _ := svc.Seed(p.ID)
	var wave []string
	for _, it := range mustPopulation(t, svc, p.ID) {
		if it.StratumCode == "T1" && it.WaveQuarter == 1 {
			wave = append(wave, it.ItemRef)
		}
	}
	drawn := Rank(seed, "T1", wave)[0].ItemRef
	it := itemByRef(t, svc, p.ID, drawn)
	q, why := 1, "Prior finding"
	if _, err := svc.UpdatePopulationItem(p.ID, it.ID, PopulationPatch{TopUpQuarter: &q, TopUpRationale: &why}, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.SelectQuarter(p.ID, 1, "tess")
	wantConflict(t, err, drawn+" is a statistical pick of this Q1 draw")
	if v, _ := svc.Sampling(p.ID); v.Selection[0].SelectedAt != "" {
		t.Fatal("a refused selection froze the quarter")
	}

	none := 0
	if _, err := svc.UpdatePopulationItem(p.ID, it.ID, PopulationPatch{TopUpQuarter: &none}, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SelectQuarter(p.ID, 1, "tess"); err != nil {
		t.Fatal(err)
	}
	q = 2
	_, err = svc.UpdatePopulationItem(p.ID, it.ID, PopulationPatch{TopUpQuarter: &q, TopUpRationale: &why}, "alice")
	wantConflict(t, err, "already a statistical pick of Q1")
	q = 1
	other := itemByRef(t, svc, p.ID, "T2-239")
	_, err = svc.UpdatePopulationItem(p.ID, other.ID, PopulationPatch{TopUpQuarter: &q, TopUpRationale: &why}, "alice")
	wantConflict(t, err, "Q1 has been selected")
	q = 3
	_, err = svc.UpdatePopulationItem(p.ID, other.ID, PopulationPatch{TopUpQuarter: &q}, "alice")
	wantConflict(t, err, "needs its rationale")
}

func TestASelectedQuarterFreezesItsWave(t *testing.T) {
	svc := newTestService(t)
	p := frozenExample(t, svc)
	if _, err := svc.SelectQuarter(p.ID, 1, "tess"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChangeRequest(p.ID, "re-tiering", "alice"); err != nil {
		t.Fatal(err)
	}
	var picked string
	for _, it := range mustPopulation(t, svc, p.ID) {
		if it.SelectedQuarter == 1 {
			picked = it.ItemRef
			break
		}
	}
	for _, c := range []struct {
		csv, want string
		merge     bool
	}{
		{"item_ref,stratum,wave\nT1-099,T1,Q1\n", "cannot gain an item", true},
		{"item_ref,wave\nT1-002,Q2\n", "cannot leave its wave", true},
		{"item_ref,stratum\n" + picked + ",T2\n", "its stratum cannot change", true},
	} {
		d, err := svc.PreviewPopulation(p.ID, PopulationImport{CSV: c.csv, Merge: c.merge})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(d.Errors, func(e RowError) bool { return strings.Contains(e.Message, c.want) }) {
			t.Errorf("%q: errors %+v, want %q", c.csv, d.Errors, c.want)
		}
	}
	all := mustPopulation(t, svc, p.ID)
	var keep []PopulationItem
	for _, it := range all {
		if it.ItemRef != picked {
			keep = append(keep, it)
		}
	}
	d, _ := svc.PreviewPopulation(p.ID, PopulationImport{CSV: populationCSV(keep)})
	if !slices.ContainsFunc(d.Errors, func(e RowError) bool {
		return e.ItemRef == picked && strings.Contains(e.Message, "stays in the population")
	}) {
		t.Errorf("removing a selected item: %+v", d.Errors)
	}
	d, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: "item_ref,stratum,wave\nT2-241,T2,Q3\n", Merge: true}, "alice")
	if err != nil || !d.Applied {
		t.Errorf("adding to a later wave under a change request: %v %+v", err, d.Errors)
	}
}

func TestAShortWaveIsTakenWholeAndTheShortfallReported(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	strata := []Stratum{{Code: "T1", Name: "Tier 1", SamplingEnabled: true, Confidence: 0.95, TolerableRate: 0.05,
		EffortPerTest: 3, Waves: [4]float64{0.5, 0.5, 0, 0}},
		{Code: "C", Name: "Coverage", Waves: [4]float64{1, 0, 0, 0}, EffortPerTest: 1}}
	if _, err := svc.ReplaceStrata(p.ID, strata, "alice"); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("item_ref,stratum,wave\n")
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "T1-%02d,T1,Q1\n", i)
	}
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&b, "C-%02d,C,Q1\n", i)
	}
	if d, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: b.String()}, "alice"); err != nil {
		t.Fatalf("%v %+v", err, d.Errors)
	}
	if !slices.ContainsFunc(Lint(mustGet(t, svc, p.ID), mustPopulation(t, svc, p.ID)), func(f LintFinding) bool { return f.Code == "wave_counts" }) {
		t.Error("the gate did not note waves that differ from the wave percentages")
	}
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(p.ID, "bob", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FreezePopulation(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SelectQuarter(p.ID, 1, "tess")
	if err != nil {
		t.Fatal(err)
	}
	basis := map[string]int{}
	for _, s := range res.Picks {
		basis[s.StratumCode+" "+s.Basis]++
	}
	// N = 10 at 95%/5%: n = ⌈59 ÷ (1 + 58/10)⌉ = 9, split 5/4 by the 50/50 waves.
	if basis["T1 Statistical"] != 5 || basis["C Full coverage"] != 3 {
		t.Errorf("Q1 picks = %v, want 5 statistical and 3 full coverage", basis)
	}
	res, err = svc.SelectQuarter(p.ID, 2, "tess")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Picks) != 0 || len(res.Shortfalls) != 1 || !strings.Contains(res.Shortfalls[0], "T1: the Q2 wave has 0 item(s) not yet selected against a sample of 4") {
		t.Errorf("Q2: picks %v, shortfalls %v", res.Picks, res.Shortfalls)
	}
}

func TestStrataAreValidatedAndCannotDropACodeInUse(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	loadExamplePopulation(t, svc, p.ID)
	_, err := svc.ReplaceStrata(p.ID, p.Strata[:1], "alice")
	wantInvalid(t, err, "240 population item(s) are in stratum T2")
	bad := slices.Clone(p.Strata)
	bad[0].TolerableRate = 0
	_, err = svc.ReplaceStrata(p.ID, bad, "alice")
	wantInvalid(t, err, "tolerable deviation rate")
	bad = slices.Clone(p.Strata)
	bad[1].Code = "t1"
	_, err = svc.ReplaceStrata(p.ID, bad, "alice")
	wantInvalid(t, err, "used twice")
	next := slices.Clone(p.Strata)
	next[0].TopUps = [4]int{2, 2, 2, 0}
	updated, err := svc.ReplaceStrata(p.ID, next, "alice")
	if err != nil || updated.Strata[0].TopUps != [4]int{2, 2, 2, 0} {
		t.Fatalf("ReplaceStrata: %v", err)
	}
	h, _ := svc.History(p.ID)
	if h[0].Subject != "strata" || h[0].Note != "changed T1" {
		t.Errorf("history = %+v", h[0])
	}
}

func TestApprovalSnapshotsTheStrataAndPopulationButNotTheSeed(t *testing.T) {
	svc := newTestService(t)
	p := frozenExample(t, svc)
	if _, err := svc.ChangeRequest(p.ID, "recalibrate Tier 2 top-ups", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Approve(p.ID, "bob", "")
	if err != nil {
		t.Fatal(err)
	}
	v, _ := svc.Version(p.ID, res.Version.Number)
	var snap struct {
		Strata         []Stratum        `json:"strata"`
		Population     []PopulationItem `json:"population"`
		SelectionSeed  string           `json:"selection_seed"`
		SeedCommitment string           `json:"seed_commitment"`
	}
	if err := json.Unmarshal([]byte(v.SnapshotJSON), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Strata) != 2 || len(snap.Population) != 320 || snap.SelectionSeed != "" || snap.SeedCommitment == "" {
		t.Errorf("snapshot: %d strata, %d items, seed %q, commitment %q", len(snap.Strata), len(snap.Population), snap.SelectionSeed, snap.SeedCommitment)
	}
}

func TestCloneCarriesStrataAndPopulationWithoutExecution(t *testing.T) {
	svc := newTestService(t)
	p := frozenExample(t, svc)
	if _, err := svc.ApplyPopulation(p.ID, PopulationImport{CSV: "item_ref,attested_on,dq_status,topup_quarter,topup_rationale\nT1-030,2027-03-01,Accepted,Q2,Prior finding\n", Merge: true}, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SelectQuarter(p.ID, 1, "tess"); err != nil {
		t.Fatal(err)
	}
	c, err := svc.Clone(p.ID, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Strata) != 2 || c.Strata[1].Waves != tier2Waves || c.SelectionSeed != "" || c.PopulationSize != 320 {
		t.Fatalf("clone: %d strata, seed %q, %d items", len(c.Strata), c.SelectionSeed, c.PopulationSize)
	}
	for _, it := range mustPopulation(t, svc, c.ID) {
		if it.AttestedOn != "" || it.DQStatus != "" || it.TopUpQuarter != 0 || it.SelectedQuarter != 0 {
			t.Fatalf("the clone kept execution data on %+v", it)
		}
	}
	if v, _ := svc.Sampling(c.ID); len(v.Selections) != 0 || v.Frozen {
		t.Error("the clone carried selections or the freeze")
	}
}
