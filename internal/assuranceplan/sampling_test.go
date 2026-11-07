package assuranceplan

import (
	"math"
	"slices"
	"testing"
)

func TestStatisticalSizeReproducesTheAttributeSamplingTable(t *testing.T) {
	for _, c := range []struct {
		conf, tol float64
		want      int
	}{{0.95, 0.05, 59}, {0.95, 0.10, 29}, {0.90, 0.05, 45}, {0.90, 0.10, 22}} {
		got, err := StatisticalSize(c.conf, c.tol)
		if err != nil || got != c.want {
			t.Errorf("n0(%g, %g) = %d, %v; want %d", c.conf, c.tol, got, err, c.want)
		}
	}
	for _, bad := range [][2]float64{{0, 0.05}, {1, 0.05}, {0.95, 0}, {0.95, 1}, {-0.1, 0.05}} {
		if _, err := StatisticalSize(bad[0], bad[1]); err == nil {
			t.Errorf("n0(%g, %g) accepted", bad[0], bad[1])
		}
	}
}

func TestFiniteCorrectionFloorAndCap(t *testing.T) {
	if got := CorrectedSize(59, 80); got != 35 {
		t.Errorf("FPC N=80 at 95%%/5%% = %d, want 35", got)
	}
	if got := CorrectedSize(22, 240); got != 21 {
		t.Errorf("FPC N=240 at 90%%/10%% = %d, want 21", got)
	}
	if got := CorrectedSize(59, 0); got != 0 {
		t.Errorf("FPC N=0 = %d, want 0", got)
	}
	// The floor wins when it asks for more than the statistical size.
	s, err := AnnualSample(true, 0.90, 0.10, 0.25, 200)
	if err != nil || s.FPC != 20 || s.Floor != 50 || s.Annual != 50 {
		t.Errorf("floor case = %+v, %v; want FPC 20, floor 50, annual 50", s, err)
	}
	// The annual sample never exceeds the population.
	s, _ = AnnualSample(true, 0.95, 0.05, 1, 7)
	if s.Annual != 7 {
		t.Errorf("annual for N=7 with a 100%% floor = %d, want 7", s.Annual)
	}
	s, _ = AnnualSample(true, 0.95, 0.05, 0.25, 0)
	if s != (SampleSize{N0: 59}) {
		t.Errorf("empty stratum = %+v, want all zero but n0", s)
	}
	s, _ = AnnualSample(false, 0, 0, 0, 37)
	if s.Annual != 37 {
		t.Errorf("sampling off covers %d of 37", s.Annual)
	}
	if _, err := AnnualSample(true, 0.95, 0.05, 1.5, 10); err == nil {
		t.Error("a floor above 100% was accepted")
	}
}

var (
	tier1Waves = [4]float64{0.35, 0.35, 0.30, 0}
	tier2Waves = [4]float64{0.15, 0.30, 0.30, 0.25}
)

func TestAppendixAGoldenValues(t *testing.T) {
	t1, _ := AnnualSample(true, 0.95, 0.05, 0.25, 80)
	t2, _ := AnnualSample(true, 0.90, 0.10, 0.05, 240)
	if t1 != (SampleSize{N: 80, N0: 59, FPC: 35, Floor: 20, Annual: 35}) {
		t.Errorf("Tier 1 = %+v, want 59 → 35 → 20 → 35", t1)
	}
	if t2 != (SampleSize{N: 240, N0: 22, FPC: 21, Floor: 12, Annual: 21}) {
		t.Errorf("Tier 2 = %+v, want 22 → 21 → 12 → 21", t2)
	}
	for _, c := range []struct {
		name  string
		got   func() ([4]int, error)
		want  [4]int
		label string
	}{
		{"Tier 1", func() ([4]int, error) { return Allocate(35, tier1Waves) }, [4]int{13, 12, 10, 0}, "statistical"},
		{"Tier 2", func() ([4]int, error) { return Allocate(21, tier2Waves) }, [4]int{4, 6, 6, 5}, "statistical"},
		{"Tier 1", func() ([4]int, error) { return ReviewsDue(80, tier1Waves) }, [4]int{28, 28, 24, 0}, "reviews due"},
		{"Tier 2", func() ([4]int, error) { return ReviewsDue(240, tier2Waves) }, [4]int{36, 72, 72, 60}, "reviews due"},
	} {
		got, err := c.got()
		if err != nil || got != c.want {
			t.Errorf("%s %s = %v, %v; want %v", c.name, c.label, got, err, c.want)
		}
	}
}

// The prompt's own example of the drift, 0.35+0.35+0.30, sums to exactly 1
// in IEEE doubles; 0.2+0.4+0.3+0.1 is a set of waves that really does reach
// 1.0000000000000002, and without the guard it adds an item.
func TestAllocationGuardsFloatingPointDrift(t *testing.T) {
	waves := []float64{0.2, 0.4, 0.3, 0.1}
	cum := 0.0
	for _, w := range waves {
		cum += w
	}
	if cum <= 1 {
		t.Fatalf("expected the running sum to drift above 1, got %v", cum)
	}
	if naive := math.Ceil(1 * cum); naive != 2 {
		t.Fatalf("expected the unguarded ceiling to round up an extra item, got %v", naive)
	}
	got, err := Allocate(1, [4]float64(waves))
	if err != nil || got != [4]int{1, 0, 0, 0} {
		t.Errorf("Allocate(1, 20/40/30/10) = %v, %v; want [1 0 0 0]", got, err)
	}
	// Mid-year the drift bites too: 5 × 0.6000000000000001 rounds up to 4
	// unguarded, which would move an item from Q3 into Q2.
	if naive := math.Ceil(5 * (waves[0] + waves[1])); naive != 4 {
		t.Fatalf("expected the unguarded Q2 cumulative to round up to 4, got %v", naive)
	}
	got, err = Allocate(5, [4]float64(waves))
	if err != nil || got != [4]int{1, 2, 2, 0} {
		t.Errorf("Allocate(5, 20/40/30/10) = %v, %v; want [1 2 2 0]", got, err)
	}

	// Every split in 5% steps, for every total up to 300, adds up exactly.
	for a := 0; a <= 20; a++ {
		for b := 0; a+b <= 20; b++ {
			for c := 0; a+b+c <= 20; c++ {
				w := [4]float64{float64(a) * 0.05, float64(b) * 0.05, float64(c) * 0.05, float64(20-a-b-c) * 0.05}
				for total := 0; total <= 300; total += 7 {
					q, err := Allocate(total, w)
					if err != nil {
						t.Fatalf("Allocate(%d, %v): %v", total, w, err)
					}
					if q[0]+q[1]+q[2]+q[3] != total || slices.Min(q[:]) < 0 {
						t.Fatalf("Allocate(%d, %v) = %v does not add up", total, w, q)
					}
				}
			}
		}
	}
	q, _ := Allocate(20, tier1Waves)
	if q != [4]int{7, 7, 6, 0} {
		t.Errorf("Allocate(20, 35/35/30/0) = %v, want [7 7 6 0]", q)
	}
}

func TestWaveSumsOtherThan100AreRejected(t *testing.T) {
	bad := [4]float64{0.35, 0.35, 0.20, 0}
	if err := ValidateWaves(bad); err == nil {
		t.Fatal("waves adding up to 90% were accepted")
	}
	if _, err := Allocate(10, bad); err == nil {
		t.Error("Allocate accepted waves adding up to 90%")
	}
	if err := ValidateWaves([4]float64{1.2, -0.2, 0, 0}); err == nil {
		t.Error("a negative wave was accepted")
	}
	err := normalizeStrata([]Stratum{{Code: "T1", SamplingEnabled: true, Confidence: 0.95, TolerableRate: 0.05, Waves: bad}})
	wantInvalid(t, err, "100%")
	findings := Lint(Plan{Strata: []Stratum{{Code: "T1", Waves: bad}}}, nil)
	found := false
	for _, f := range findings {
		found = found || (f.Code == "stratum_waves" && f.Severity == LintBlocking)
	}
	if !found {
		t.Errorf("the gate did not block waves adding up to 90%%: %+v", findings)
	}
}

func TestRankIsDeterministicAndAddingAnItemMovesNothing(t *testing.T) {
	refs := []string{"T1-001", "T1-002", "T1-003", "T1-004", "T1-005", "T1-006"}
	a := Rank("seed-1", "T1", refs)
	b := Rank("seed-1", "T1", slices.Clone(refs))
	if !slices.Equal(a, b) {
		t.Fatal("the same seed and population ranked differently")
	}
	withNew := Rank("seed-1", "T1", append(slices.Clone(refs), "T1-007"))
	var kept []Ranked
	for _, r := range withNew {
		if r.ItemRef != "T1-007" {
			kept = append(kept, r)
		}
	}
	if !slices.Equal(kept, a) {
		t.Error("adding an item changed the order of the others")
	}
	if slices.Equal(Rank("seed-2", "T1", refs), a) {
		t.Error("a different seed gave the same ranking")
	}
	if RankHash("s", "T1", "0X") == RankHash("s", "T10", "X") {
		t.Error("the separator does not keep the stratum and the reference apart")
	}
	if got := RankHash("abc", "T1", "T1-001"); len(got) != 64 {
		t.Errorf("rank hash %q is not hex SHA-256", got)
	}
}

func TestCapacityForTheAppendixAPlan(t *testing.T) {
	svc := newTestService(t)
	p := createExample(t, svc, "alice")
	v, err := svc.Sampling(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := [4]float64{74.4, 81.8, 76.8, 47.0}
	for q, l := range v.Quarters {
		if math.Abs(l.Effort-want[q]) > 1e-9 || l.Capacity != 100 || l.Over {
			t.Errorf("Q%d effort %v of %v (over=%v), want %v of 100", q+1, l.Effort, l.Capacity, l.Over, want[q])
		}
	}
	if v.TotalEffort != 280 || v.TotalCapacity != 400 || v.Utilisation != 0.7 {
		t.Errorf("year = %v of %v (%v), want 280 of 400 (0.7)", v.TotalEffort, v.TotalCapacity, v.Utilisation)
	}
	loads := Capacity([]StratumLoad{{Tests: [4]int{14, 13, 11, 1}, Reviews: [4]int{28, 28, 24, 0}, EffortPerTest: 3, EffortPerReview: 0.25},
		{Tests: [4]int{5, 7, 7, 6}, Reviews: [4]int{36, 72, 72, 60}, EffortPerTest: 2, EffortPerReview: 0.15}},
		[4]float64{0, 3, 5, 8}, [4]float64{10, 8, 8, 15}, 2, 50, 0.80)
	if !loads[1].Over || loads[0].Over || loads[2].Over || loads[3].Over {
		t.Errorf("at an 80%% threshold only Q2 (81.8%%) should be flagged: %+v", loads)
	}
}
