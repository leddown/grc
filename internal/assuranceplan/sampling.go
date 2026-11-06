package assuranceplan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
)

// The sampling engine: attribute sampling with zero expected deviations, a
// finite-population correction, a coverage floor, cumulative quarter
// allocation and a hash ranking anyone holding the seed can re-perform. Every
// function here is pure; the service feeds them the plan and the population.

// guard rounds away the last bits of floating-point error before a ceiling or
// a rounding is taken, so 0.2+0.4+0.3+0.1 = 1.0000000000000002 does not add an
// item that nobody planned.
func guard(x float64) float64 { return math.Round(x*1e6) / 1e6 }

func ceilGuarded(x float64) int { return int(math.Ceil(guard(x))) }

// StatisticalSize is n0 = ceil(ln(1 − confidence) / ln(1 − tolerable rate)),
// the attribute-sampling size for zero expected deviations: 95%/5% → 59,
// 95%/10% → 29, 90%/5% → 45, 90%/10% → 22.
func StatisticalSize(confidence, tolerable float64) (int, error) {
	if !(confidence > 0 && confidence < 1) {
		return 0, fmt.Errorf("confidence %g is not between 0 and 1", confidence)
	}
	if !(tolerable > 0 && tolerable < 1) {
		return 0, fmt.Errorf("tolerable deviation rate %g is not between 0 and 1", tolerable)
	}
	return ceilGuarded(math.Log(1-confidence) / math.Log(1-tolerable)), nil
}

// CorrectedSize applies the finite-population correction,
// n = ceil(n0 / (1 + (n0 − 1)/N)), and is 0 for an empty population.
func CorrectedSize(n0, population int) int {
	if population <= 0 || n0 <= 0 {
		return 0
	}
	return ceilGuarded(float64(n0) / (1 + float64(n0-1)/float64(population)))
}

// FloorSize is the minimum coverage, ceil(floor % × N).
func FloorSize(floorPct float64, population int) int {
	if population <= 0 || floorPct <= 0 {
		return 0
	}
	return ceilGuarded(floorPct * float64(population))
}

// SampleSize is a stratum's annual statistical sample with each step of its
// derivation, so the rationale can be shown next to the figure.
type SampleSize struct {
	N      int `json:"n"`
	N0     int `json:"n0"`
	FPC    int `json:"fpc"`
	Floor  int `json:"floor"`
	Annual int `json:"annual"`
}

// AnnualSample is min(N, max(FPC-corrected n, floor)). With sampling off the
// whole stratum is covered.
func AnnualSample(sampling bool, confidence, tolerable, floorPct float64, population int) (SampleSize, error) {
	out := SampleSize{N: population}
	if population < 0 {
		return out, fmt.Errorf("population %d is negative", population)
	}
	if !sampling {
		out.Annual = population
		return out, nil
	}
	if floorPct < 0 || floorPct > 1 {
		return out, fmt.Errorf("coverage floor %g is not between 0 and 1", floorPct)
	}
	n0, err := StatisticalSize(confidence, tolerable)
	if err != nil {
		return out, err
	}
	out.N0 = n0
	out.FPC = CorrectedSize(n0, population)
	out.Floor = FloorSize(floorPct, population)
	out.Annual = min(population, max(out.FPC, out.Floor))
	return out, nil
}

const waveTolerance = 1e-6

// ValidateWaves checks a stratum's wave shares: each between 0 and 1, and
// together 100%.
func ValidateWaves(waves [4]float64) error {
	sum := 0.0
	for i, w := range waves {
		if w < 0 || w > 1 || math.IsNaN(w) {
			return fmt.Errorf("the Q%d wave %g%% is not between 0%% and 100%%", i+1, w*100)
		}
		sum += w
	}
	if math.Abs(sum-1) > waveTolerance {
		return fmt.Errorf("the waves add up to %s%%, not 100%%", formatNumber(sum*100))
	}
	return nil
}

// allocate spreads total over the quarters by cumulative share:
// q_i = f(total × cum_i) − f(total × cum_{i−1}), so the quarters always add up
// to total whatever the rounding f does.
func allocate(total int, waves [4]float64, f func(float64) float64) ([4]int, error) {
	var out [4]int
	if err := ValidateWaves(waves); err != nil {
		return out, err
	}
	cum, prev := 0.0, 0
	for i, w := range waves {
		cum += w
		if i == len(waves)-1 {
			cum = 1
		}
		next := int(f(guard(float64(total) * cum)))
		out[i] = next - prev
		prev = next
	}
	return out, nil
}

// Allocate is the statistical sample per quarter, rounding the cumulative
// figure up so a sample is taken no later than its wave reaches it.
func Allocate(total int, waves [4]float64) ([4]int, error) {
	return allocate(total, waves, math.Ceil)
}

// ReviewsDue is the number of 1LoD attestations, and so 2LoD data-quality
// reviews, due in each quarter: the same cumulative scheme with rounding.
func ReviewsDue(population int, waves [4]float64) ([4]int, error) {
	return allocate(population, waves, math.Round)
}

// RankHash is the item's position key in its stratum's draw:
// SHA-256(seed ‖ 0x1F ‖ stratum ‖ 0x1F ‖ item_ref), hex. The unit separator
// keeps "T1"+"0…" and "T10"+"…" apart; the hash depends on nothing but the seed
// and the item's own reference, so adding an item never moves the others.
func RankHash(seed, stratum, itemRef string) string {
	h := sha256.New()
	h.Write([]byte(seed))
	h.Write([]byte{0x1f})
	h.Write([]byte(stratum))
	h.Write([]byte{0x1f})
	h.Write([]byte(itemRef))
	return hex.EncodeToString(h.Sum(nil))
}

// Ranked is an item reference with its rank hash.
type Ranked struct {
	ItemRef string
	Hash    string
}

// Rank orders references by their rank hash, lowest first.
func Rank(seed, stratum string, refs []string) []Ranked {
	out := make([]Ranked, len(refs))
	for i, r := range refs {
		out[i] = Ranked{ItemRef: r, Hash: RankHash(seed, stratum, r)}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hash != out[j].Hash {
			return out[i].Hash < out[j].Hash
		}
		return out[i].ItemRef < out[j].ItemRef
	})
	return out
}

// QuarterLoad is one quarter's effort against the testers' capacity.
type QuarterLoad struct {
	Quarter      int     `json:"quarter"`
	TestEffort   float64 `json:"test_effort"`
	ReviewEffort float64 `json:"review_effort"`
	RetestDays   float64 `json:"retest_days"`
	OverheadDays float64 `json:"overhead_days"`
	Effort       float64 `json:"effort"`
	Capacity     float64 `json:"capacity"`
	Utilisation  float64 `json:"utilisation"`
	Over         bool    `json:"over_threshold"`
}

// StratumLoad is what one stratum asks of the testers in each quarter.
type StratumLoad struct {
	Tests           [4]int
	Reviews         [4]int
	EffortPerTest   float64
	EffortPerReview float64
}

// Capacity is effort_q = Σ tests × effort per test + Σ reviews due × effort
// per review + retest + overhead, against FTE × productive days, flagged
// above the threshold.
func Capacity(strata []StratumLoad, retest, overhead [4]float64, fte, daysPerFTE, threshold float64) [4]QuarterLoad {
	var out [4]QuarterLoad
	capacity := fte * daysPerFTE
	for q := range out {
		l := QuarterLoad{Quarter: q + 1, RetestDays: retest[q], OverheadDays: overhead[q], Capacity: capacity}
		for _, s := range strata {
			l.TestEffort += float64(s.Tests[q]) * s.EffortPerTest
			l.ReviewEffort += float64(s.Reviews[q]) * s.EffortPerReview
		}
		l.TestEffort, l.ReviewEffort = guard(l.TestEffort), guard(l.ReviewEffort)
		l.Effort = guard(l.TestEffort + l.ReviewEffort + l.RetestDays + l.OverheadDays)
		if capacity > 0 {
			l.Utilisation = guard(l.Effort / capacity)
			l.Over = l.Utilisation > threshold
		} else {
			l.Over = l.Effort > 0
		}
		out[q] = l
	}
	return out
}

// formatNumber prints a figure with at most two decimals and no trailing
// zeros: 74.4, 280, 0.25.
func formatNumber(v float64) string {
	s := fmt.Sprintf("%.2f", guard(v))
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	if s == "-0" {
		return "0"
	}
	return s
}
