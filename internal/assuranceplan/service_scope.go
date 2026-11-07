package assuranceplan

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ---- strata ----

func normalizeStrata(strata []Stratum) error {
	seen := map[string]bool{}
	for i := range strata {
		s := &strata[i]
		s.Code = strings.TrimSpace(s.Code)
		s.Name = strings.TrimSpace(s.Name)
		s.Definition = strings.TrimSpace(s.Definition)
		switch {
		case s.Code == "":
			return invalid("stratum %d has no code", i+1)
		case len(s.Code) > 40 || strings.ContainsAny(s.Code, ",;\t\n"):
			return invalid("stratum code %q is too long or holds a separator", s.Code)
		case seen[strings.ToLower(s.Code)]:
			return invalid("stratum code %s is used twice", s.Code)
		case len(s.Name) > maxItemField || len(s.Definition) > maxFieldLength:
			return invalid("stratum %s: a field is too long", s.Code)
		}
		seen[strings.ToLower(s.Code)] = true
		if s.Name == "" {
			s.Name = s.Code
		}
		if s.SamplingEnabled {
			if _, err := StatisticalSize(s.Confidence, s.TolerableRate); err != nil {
				return invalid("stratum %s: %v", s.Code, err)
			}
			if s.FloorPct < 0 || s.FloorPct > 1 {
				return invalid("stratum %s: the coverage floor is a share between 0%% and 100%%", s.Code)
			}
		}
		if err := ValidateWaves(s.Waves); err != nil {
			return invalid("stratum %s: %v", s.Code, err)
		}
		if s.EffortPerTest < 0 || s.EffortPerReview < 0 || s.EffortPerTest > 1000 || s.EffortPerReview > 1000 {
			return invalid("stratum %s: effort per test and per review are 0 to 1000 person-days", s.Code)
		}
		for q, n := range s.TopUps {
			if n < 0 || n > 10000 {
				return invalid("stratum %s: Q%d top-ups must be 0 to 10000", s.Code, q+1)
			}
		}
		if s.PlannedSize < 0 || s.PlannedSize > 10000000 {
			return invalid("stratum %s: the planned size cannot be negative", s.Code)
		}
		if s.CycleYears < 0 || s.CycleYears > 10 {
			return invalid("stratum %s: the cycle is 0 to 10 years (0 or 1 is every year)", s.Code)
		}
		if s.CycleYears == 1 {
			s.CycleYears = 0
		}
		takes := []string{}
		for _, v := range s.Takes {
			v = strings.TrimSpace(v)
			switch {
			case v == "":
				continue
			case len(v) > 40 || strings.ContainsAny(v, ",;\t\n"):
				return invalid("stratum %s: %q is too long or holds a separator", s.Code, clip(v, 20))
			case slices.ContainsFunc(takes, func(t string) bool { return strings.EqualFold(t, v) }):
				continue
			}
			takes = append(takes, v)
		}
		if len(takes) > 20 {
			return invalid("stratum %s takes at most 20 source values", s.Code)
		}
		s.Takes = takes
		s.Ordinal = i
		s.ID = 0
	}
	return nil
}

// cycleDue is when an item of a multi-year stratum is next due: its last test
// plus the cycle. It is empty for an annual stratum, or for an item never
// tested, which is due now.
func cycleDue(st Stratum, it PopulationItem) string {
	if st.CycleYears <= 1 || it.LastTestedOn == "" {
		return ""
	}
	t, ok := parseDate(it.LastTestedOn)
	if !ok {
		return ""
	}
	return formatDate(t.AddDate(st.CycleYears, 0, 0))
}

// dueInPeriod reports whether an item needs its test in the period: always in
// an annual stratum, and in a multi-year one when it was never tested or its
// cycle lapses by the period's end.
func dueInPeriod(st Stratum, it PopulationItem, periodEnd string) bool {
	if st.CycleYears <= 1 {
		return true
	}
	due := cycleDue(st, it)
	return due == "" || periodEnd == "" || due <= periodEnd
}

// inCycleAt reports whether an item's last test before the plan still covers
// it on a date.
func inCycleAt(st Stratum, it PopulationItem, date string) bool {
	due := cycleDue(st, it)
	return due != "" && date < due
}

func stratumDesign(s Stratum) string {
	s.ID, s.Ordinal = 0, 0
	return fmt.Sprintf("%+v", s)
}

// ReplaceStrata saves the strata as a whole list. A stratum that still holds
// population items cannot be removed or renamed out from under them.
func (s *Service) ReplaceStrata(id int64, strata []Stratum, actor string) (Plan, error) {
	prev, err := s.editable(id)
	if err != nil {
		return Plan{}, err
	}
	if strata == nil {
		strata = []Stratum{}
	}
	if err := normalizeStrata(strata); err != nil {
		return Plan{}, err
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return Plan{}, err
	}
	used := map[string]int{}
	for _, it := range pop {
		used[it.StratumCode]++
	}
	for code, n := range used {
		if !slices.ContainsFunc(strata, func(st Stratum) bool { return st.Code == code }) {
			return Plan{}, invalid("%d population item(s) are in stratum %s; move them to another stratum before removing or renaming it", n, code)
		}
	}
	old := map[string]Stratum{}
	for _, st := range prev.Strata {
		old[st.Code] = st
	}
	var added, changed, removed []string
	for _, st := range strata {
		if o, ok := old[st.Code]; !ok {
			added = append(added, st.Code)
		} else if stratumDesign(o) != stratumDesign(st) {
			changed = append(changed, st.Code)
		}
		delete(old, st.Code)
	}
	for code := range old {
		removed = append(removed, code)
	}
	sort.Strings(removed)
	var parts []string
	for _, x := range []struct {
		label string
		codes []string
	}{{"added", added}, {"changed", changed}, {"removed", removed}} {
		if len(x.codes) > 0 {
			parts = append(parts, x.label+" "+strings.Join(x.codes, ", "))
		}
	}
	if len(parts) == 0 {
		return prev, nil
	}
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "design", Subject: "strata",
		FromValue: strconv.Itoa(len(prev.Strata)), ToValue: strconv.Itoa(len(strata)), Note: strings.Join(parts, "; ")}
	if err := s.repo.ReplaceStrata(id, strata, []HistoryEntry{h}); err != nil {
		return Plan{}, err
	}
	return s.Get(id)
}

// ---- sampling view ----

// StratumSizing is a stratum's sample, allocation and coverage, with the
// inputs each figure came from.
type StratumSizing struct {
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	Sampling      bool       `json:"sampling_enabled"`
	N             int        `json:"n"`
	NSource       string     `json:"n_source"`
	Confidence    float64    `json:"confidence"`
	TolerableRate float64    `json:"tolerable_rate"`
	FloorPct      float64    `json:"floor_pct"`
	Size          SampleSize `json:"size"`
	Statistical   [4]int     `json:"statistical"`
	TopUps        [4]int     `json:"top_ups"`
	Tests         [4]int     `json:"tests"`
	TotalTests    int        `json:"total_tests"`
	Coverage      float64    `json:"coverage"`
	ReviewsDue    [4]int     `json:"reviews_due"`
	CumTarget     [4]float64 `json:"cumulative_target"`
	WaveCounts    [4]int     `json:"wave_counts"`
	NoWave        int        `json:"no_wave"`
	// CycleYears and InCycle are for a multi-year stratum: InCycle counts the
	// items whose last test is recent enough that they are not due in the
	// period. They are outside N and are not drawn unless given a wave.
	CycleYears int    `json:"cycle_years"`
	InCycle    int    `json:"in_cycle"`
	Flagged    [4]int `json:"flagged_top_ups"`
	Selected   [4]int `json:"selected"`
	Error      string `json:"error"`
}

type QuarterSelection struct {
	Quarter       int    `json:"quarter"`
	Exists        bool   `json:"exists"`
	SelectionDate string `json:"selection_date"`
	SelectedAt    string `json:"selected_at"`
	SelectedBy    string `json:"selected_by"`
	Picks         int    `json:"picks"`
}

// SamplingView is everything the Sampling tab shows, computed on read.
type SamplingView struct {
	Strata         []StratumSizing    `json:"strata"`
	Quarters       [4]QuarterLoad     `json:"quarters"`
	TotalEffort    float64            `json:"total_effort"`
	TotalCapacity  float64            `json:"total_capacity"`
	Utilisation    float64            `json:"utilisation"`
	Threshold      float64            `json:"utilisation_threshold"`
	TotalTests     int                `json:"total_tests"`
	PopulationSize int                `json:"population_size"`
	Frozen         bool               `json:"frozen"`
	FrozenAt       string             `json:"frozen_at"`
	FrozenBy       string             `json:"frozen_by"`
	SeedCommitment string             `json:"seed_commitment"`
	SeedRevealed   bool               `json:"seed_revealed"`
	Seed           string             `json:"seed"`
	Selection      []QuarterSelection `json:"selection"`
	Selections     []Selection        `json:"selections"`
}

// computeSampling sizes every stratum from the population (or, before it is
// loaded, from the stratum's planned size) and loads the quarters.
func computeSampling(p Plan, pop []PopulationItem, sels []Selection) SamplingView {
	v := SamplingView{Strata: []StratumSizing{}, Threshold: p.Parameters.UtilisationThreshold, PopulationSize: len(pop),
		Frozen: p.SelectionSeed != "", FrozenAt: p.FrozenAt, FrozenBy: p.FrozenBy, SeedCommitment: p.SeedCommitment,
		SeedRevealed: p.SeedRevealed, Selections: sels}
	if v.Selections == nil {
		v.Selections = []Selection{}
	}
	if p.SeedRevealed {
		v.Seed = p.SelectionSeed
	}
	var loads []StratumLoad
	for _, st := range p.Strata {
		z := StratumSizing{Code: st.Code, Name: st.Name, Sampling: st.SamplingEnabled, Confidence: st.Confidence,
			TolerableRate: st.TolerableRate, FloorPct: st.FloorPct, TopUps: st.TopUps, CycleYears: st.CycleYears}
		for _, it := range pop {
			if it.StratumCode != st.Code {
				continue
			}
			if it.WaveQuarter == 0 && !dueInPeriod(st, it, p.PeriodEnd) {
				z.InCycle++
				continue
			}
			z.N++
			if it.WaveQuarter >= 1 && it.WaveQuarter <= 4 {
				z.WaveCounts[it.WaveQuarter-1]++
			} else {
				z.NoWave++
			}
			if it.TopUpQuarter >= 1 && it.TopUpQuarter <= 4 {
				z.Flagged[it.TopUpQuarter-1]++
			}
		}
		switch {
		case z.N > 0 || z.InCycle > 0:
			z.NSource = "population"
		case st.PlannedSize > 0:
			z.N, z.NSource = st.PlannedSize, "planned"
		default:
			z.NSource = "none"
		}
		for _, s := range sels {
			if s.StratumCode == st.Code && s.Quarter >= 1 && s.Quarter <= 4 {
				z.Selected[s.Quarter-1]++
			}
		}
		size, err := AnnualSample(st.SamplingEnabled, st.Confidence, st.TolerableRate, st.FloorPct, z.N)
		z.Size = size
		if err == nil {
			z.Statistical, err = Allocate(size.Annual, st.Waves)
		}
		if err == nil {
			z.ReviewsDue, err = ReviewsDue(z.N, st.Waves)
		}
		if err != nil {
			z.Error = err.Error()
		}
		// A full-coverage stratum tests every item of each wave, so once its
		// population is loaded the waves as loaded are its quarterly plan,
		// whatever the wave percentages say. Items proposed from due dates
		// fall where they are due, not where the percentages put them.
		fullLoaded := !st.SamplingEnabled && z.NSource == "population" && z.Error == "" && z.N > 0
		if fullLoaded {
			z.Statistical, z.ReviewsDue = z.WaveCounts, z.WaveCounts
		}
		cum := 0.0
		running := 0
		for q := range 4 {
			z.Tests[q] = z.Statistical[q] + z.TopUps[q]
			z.TotalTests += z.Tests[q]
			cum += st.Waves[q]
			z.CumTarget[q] = guard(min(cum, 1))
			if fullLoaded {
				running += z.WaveCounts[q]
				z.CumTarget[q] = guard(float64(running) / float64(z.N))
			}
		}
		if z.N > 0 {
			z.Coverage = guard(float64(z.TotalTests) / float64(z.N))
		}
		v.TotalTests += z.TotalTests
		v.Strata = append(v.Strata, z)
		loads = append(loads, StratumLoad{Tests: z.Tests, Reviews: z.ReviewsDue, EffortPerTest: st.EffortPerTest, EffortPerReview: st.EffortPerReview})
	}
	var retest, overhead [4]float64
	for _, q := range p.Quarters {
		if q.Quarter >= 1 && q.Quarter <= 4 {
			retest[q.Quarter-1], overhead[q.Quarter-1] = q.RetestDays, q.OverheadDays
		}
	}
	v.Quarters = Capacity(loads, retest, overhead, p.Parameters.TesterFTE, p.Parameters.DaysPerFTEQuarter, p.Parameters.UtilisationThreshold)
	for _, l := range v.Quarters {
		v.TotalEffort += l.Effort
		v.TotalCapacity += l.Capacity
	}
	v.TotalEffort, v.TotalCapacity = guard(v.TotalEffort), guard(v.TotalCapacity)
	if v.TotalCapacity > 0 {
		v.Utilisation = guard(v.TotalEffort / v.TotalCapacity)
	}
	picks := map[int]int{}
	for _, s := range sels {
		picks[s.Quarter]++
	}
	for q := 1; q <= 4; q++ {
		qs := QuarterSelection{Quarter: q, Picks: picks[q]}
		for _, row := range p.Quarters {
			if row.Quarter == q {
				qs.Exists, qs.SelectionDate, qs.SelectedAt, qs.SelectedBy = true, row.SelectionDate, row.SelectedAt, row.SelectedBy
			}
		}
		v.Selection = append(v.Selection, qs)
	}
	return v
}

func (s *Service) Sampling(id int64) (SamplingView, error) {
	p, err := s.Get(id)
	if err != nil {
		return SamplingView{}, err
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return SamplingView{}, err
	}
	sels, err := s.repo.Selections(id)
	if err != nil {
		return SamplingView{}, err
	}
	return computeSampling(p, pop, sels), nil
}

func (s *Service) Selections(id int64) ([]Selection, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	return s.repo.Selections(id)
}

// ---- population ----

func (s *Service) Population(id int64) ([]PopulationItem, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	return s.repo.Population(id)
}

func (s *Service) PopulationCSV(id int64) (string, error) {
	items, err := s.Population(id)
	if err != nil {
		return "", err
	}
	return populationCSV(items), nil
}

// ExamplePopulationCSV is a made-up population sized to the plan's strata,
// for trying a plan out. It writes nothing.
func (s *Service) ExamplePopulationCSV(id int64) (string, error) {
	p, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if len(p.Strata) == 0 {
		return "", invalid("the plan has no strata: define them on the Sampling tab first")
	}
	return populationCSV(examplePopulation(p.Strata)), nil
}

func (s *Service) PreviewPopulation(id int64, in PopulationImport) (PopulationDiff, error) {
	return s.importPopulation(id, in, "", false)
}

func (s *Service) ApplyPopulation(id int64, in PopulationImport, actor string) (PopulationDiff, error) {
	return s.importPopulation(id, in, actor, true)
}

func (s *Service) importPopulation(id int64, in PopulationImport, actor string, apply bool) (PopulationDiff, error) {
	p, err := s.Get(id)
	if err != nil {
		return PopulationDiff{}, err
	}
	if p.Status == StatusClosed {
		return PopulationDiff{}, conflict("%s is closed; its population no longer changes", p.Reference)
	}
	if len(p.Strata) == 0 {
		return PopulationDiff{}, invalid("the plan has no strata: define them on the Sampling tab before loading a population")
	}
	current, err := s.repo.Population(id)
	if err != nil {
		return PopulationDiff{}, err
	}
	parsed, err := parsePopulationCSV(in.CSV, p.Strata)
	if err != nil {
		return PopulationDiff{}, err
	}
	change, diff := diffPopulation(p, current, parsed.rows, in.Merge)
	diff.Errors = append(append([]RowError{}, parsed.errs...), diff.Errors...)
	sort.SliceStable(diff.Errors, func(i, j int) bool { return diff.Errors[i].Line < diff.Errors[j].Line })
	if len(diff.Errors) > maxPopulationErrors {
		extra := len(diff.Errors) - maxPopulationErrors
		diff.Errors = append(diff.Errors[:maxPopulationErrors:maxPopulationErrors], RowError{Message: fmt.Sprintf("and %d more", extra)})
	}
	diff.Rows = parsed.total
	diff.Columns, diff.Ignored = parsed.cols, parsed.ignored
	if diff.Ignored == nil {
		diff.Ignored = []string{}
	}
	diff.Base = populationDigest(current, false)
	if !apply {
		return diff, nil
	}
	if in.Base != "" && in.Base != diff.Base {
		return diff, conflict("the population changed since this paste was previewed; preview it again")
	}
	if len(diff.Errors) > 0 {
		return diff, invalid("the paste has %d problem(s); preview it to see each one", len(diff.Errors))
	}
	if len(change.Add)+len(change.Update)+len(change.Remove) == 0 {
		return diff, nil
	}
	mode := "replace"
	if in.Merge {
		mode = "merge"
	}
	note := fmt.Sprintf("%s: added %d, changed %d, removed %d", mode, len(diff.Added), len(diff.Changed), len(diff.Removed))
	for _, x := range []struct {
		label string
		refs  []string
	}{{"added", diff.Added}, {"removed", diff.Removed}} {
		if len(x.refs) > 0 {
			refs := x.refs
			if len(refs) > 12 {
				refs = append(refs[:12:12], fmt.Sprintf("and %d more", len(x.refs)-12))
			}
			note += "; " + x.label + " " + strings.Join(refs, ", ")
		}
	}
	after := len(current) + len(change.Add) - len(change.Remove)
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "population", Subject: "import",
		FromValue: strconv.Itoa(len(current)), ToValue: strconv.Itoa(after), Note: note}
	if err := s.repo.ApplyPopulation(id, change, []HistoryEntry{h}); err != nil {
		return diff, err
	}
	diff.Applied = true
	return diff, nil
}

// PopulationPatch edits one item. Membership, stratum and wave change only
// through an import, where the whole population's diff is visible.
type PopulationPatch struct {
	Name           *string `json:"name"`
	CIF            *string `json:"cif"`
	BusinessLine   *string `json:"business_line"`
	Hosting        *string `json:"hosting"`
	InternetFacing *bool   `json:"internet_facing"`
	Owner          *string `json:"owner"`
	AttestedOn     *string `json:"attested_on"`
	DQStatus       *string `json:"dq_status"`
	DQReviewedOn   *string `json:"dq_reviewed_on"`
	TopUpQuarter   *int    `json:"topup_quarter"`
	TopUpRationale *string `json:"topup_rationale"`
}

func (s *Service) UpdatePopulationItem(id, itemID int64, patch PopulationPatch, actor string) (PopulationItem, error) {
	p, err := s.Get(id)
	if err != nil {
		return PopulationItem{}, err
	}
	if p.Status == StatusClosed {
		return PopulationItem{}, conflict("%s is closed; its population no longer changes", p.Reference)
	}
	items, err := s.repo.Population(id)
	if err != nil {
		return PopulationItem{}, err
	}
	idx := slices.IndexFunc(items, func(it PopulationItem) bool { return it.ID == itemID })
	if idx < 0 {
		return PopulationItem{}, ErrNotFound
	}
	prev := items[idx]
	next := prev
	text := func(dst *string, v *string) error {
		if v == nil {
			return nil
		}
		t := strings.TrimSpace(*v)
		if len(t) > maxItemField {
			return invalid("a field is longer than %d characters", maxItemField)
		}
		*dst = t
		return nil
	}
	for _, f := range []struct {
		dst *string
		v   *string
	}{{&next.Name, patch.Name}, {&next.CIF, patch.CIF}, {&next.BusinessLine, patch.BusinessLine}, {&next.Hosting, patch.Hosting},
		{&next.Owner, patch.Owner}, {&next.AttestedOn, patch.AttestedOn}, {&next.DQReviewedOn, patch.DQReviewedOn},
		{&next.TopUpRationale, patch.TopUpRationale}} {
		if err := text(f.dst, f.v); err != nil {
			return PopulationItem{}, err
		}
	}
	if patch.InternetFacing != nil {
		next.InternetFacing = *patch.InternetFacing
	}
	if patch.DQStatus != nil {
		status, ok := parseDQStatus(*patch.DQStatus)
		if !ok {
			return PopulationItem{}, invalid("DQ status must be one of %s", strings.Join(DQStatuses, ", "))
		}
		next.DQStatus = status
	}
	if patch.TopUpQuarter != nil {
		if *patch.TopUpQuarter < 0 || *patch.TopUpQuarter > 4 {
			return PopulationItem{}, invalid("the top-up quarter is 1 to 4, or 0 for none")
		}
		next.TopUpQuarter = *patch.TopUpQuarter
		if next.TopUpQuarter == 0 && patch.TopUpRationale == nil {
			next.TopUpRationale = ""
		}
	}
	for label, d := range map[string]string{"attested on": next.AttestedOn, "DQ reviewed on": next.DQReviewedOn} {
		if d != "" {
			if _, ok := parseDate(d); !ok {
				return PopulationItem{}, invalid("%s %q is not a date (YYYY-MM-DD)", label, d)
			}
		}
	}
	changes := itemChanges(prev, next)
	if len(changes) == 0 {
		return prev, nil
	}
	if reasons := rulesFor(p).check(&prev, &next); len(reasons) > 0 {
		return PopulationItem{}, conflict("%s: %s", prev.ItemRef, strings.Join(reasons, "; "))
	}
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "population", Subject: prev.ItemRef, Note: strings.Join(changes, "; ")}
	if err := s.repo.UpdatePopulationItem(id, next, []HistoryEntry{h}); err != nil {
		return PopulationItem{}, mapNotFound(err)
	}
	return next, nil
}

// ---- freeze and selection ----

// seedCommitment is the SHA-256 of the seed's hex text.
func seedCommitment(seed string) string {
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func seedRevealed(p Plan) bool {
	if p.SelectionSeed == "" {
		return false
	}
	if p.Status == StatusClosed {
		return true
	}
	if len(p.Quarters) == 0 {
		return false
	}
	for _, q := range p.Quarters {
		if q.SelectedAt == "" {
			return false
		}
	}
	return true
}

// FreezePopulation records the population's fingerprint and draws the seed
// every selection of the year is ranked by. It happens after approval (step
// 0.9 of the reference plan) and once.
func (s *Service) FreezePopulation(id int64, actor string) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	if p.SelectionSeed != "" {
		return Plan{}, conflict("%s's population was frozen on %s by %s", p.Reference, firstNonEmpty(p.FrozenAt, "an earlier date"), firstNonEmpty(p.FrozenBy, "—"))
	}
	if p.Status != StatusApproved && p.Status != StatusActive {
		return Plan{}, conflict("%s is %s; the population is frozen once the plan is approved", p.Reference, describeState(p))
	}
	if p.RevisionStatus != "" {
		return Plan{}, conflict("%s has revision %d open; approve or return it before freezing the population", p.Reference, p.Revision)
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return Plan{}, err
	}
	if len(pop) == 0 {
		return Plan{}, invalid("the population is empty: load it on the Scope population tab first")
	}
	strata := map[string]Stratum{}
	for _, st := range p.Strata {
		strata[st.Code] = st
	}
	var noWave []string
	for _, it := range pop {
		if it.WaveQuarter == 0 && dueInPeriod(strata[it.StratumCode], it, p.PeriodEnd) {
			noWave = append(noWave, it.ItemRef)
		}
	}
	if len(noWave) > 0 {
		return Plan{}, invalid("%d item(s) have no 1LoD wave (%s); every item due in the period needs one before the freeze", len(noWave), strings.Join(firstN(noWave, 8), ", "))
	}
	for _, st := range p.Strata {
		if err := ValidateWaves(st.Waves); err != nil {
			return Plan{}, invalid("stratum %s: %v", st.Code, err)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Plan{}, fmt.Errorf("draw selection seed: %w", err)
	}
	p.SelectionSeed = hex.EncodeToString(raw)
	stamp := s.stamp()
	p.UpdatedAt, p.UpdatedBy = stamp, actor
	h := HistoryEntry{At: stamp, Actor: actor, Event: eventFrozen, Subject: "population", ToValue: fmt.Sprintf("%d items", len(pop)),
		Note: fmt.Sprintf("membership SHA-256 %s; seed commitment SHA-256 %s", populationDigest(pop, true), seedCommitment(p.SelectionSeed))}
	if err := s.repo.SaveHeader(p, []HistoryEntry{h}, nil); err != nil {
		return Plan{}, mapNotFound(err)
	}
	return s.Get(id)
}

func firstN(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return append(items[:n:n], fmt.Sprintf("and %d more", len(items)-n))
}

// Seed is the selection seed, for administrators: Internal Audit re-performs
// the draw from it and the frozen population.
func (s *Service) Seed(id int64) (string, error) {
	p, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if p.SelectionSeed == "" {
		return "", conflict("%s's population has not been frozen, so there is no seed yet", p.Reference)
	}
	return p.SelectionSeed, nil
}

// SelectionResult is a quarter's frozen draw.
type SelectionResult struct {
	Plan       Plan        `json:"plan"`
	Quarter    int         `json:"quarter"`
	Picks      []Selection `json:"picks"`
	Shortfalls []string    `json:"shortfalls"`
	Warnings   []string    `json:"warnings"`
}

// SelectQuarter draws a quarter's sample and freezes it. In each stratum the
// items of the quarter's wave are ranked by RankHash and the first q_i taken,
// passing over any item already selected earlier in the year; the items
// flagged as the quarter's top-ups are added, and a top-up that is already a
// pick is refused. A second run for the same quarter is refused, never
// recomputed. The first selection moves an approved plan to Active.
func (s *Service) SelectQuarter(id int64, quarter int, actor string) (SelectionResult, error) {
	p, err := s.Get(id)
	if err != nil {
		return SelectionResult{}, err
	}
	if quarter < 1 || quarter > 4 {
		return SelectionResult{}, invalid("the quarter is 1 to 4")
	}
	if p.Status != StatusApproved && p.Status != StatusActive {
		return SelectionResult{}, conflict("%s is %s; a sample is selected from an approved plan", p.Reference, describeState(p))
	}
	if p.RevisionStatus != "" {
		return SelectionResult{}, conflict("%s has revision %d open (%s); the sample is drawn from the approved design, so approve or return the revision first",
			p.Reference, p.Revision, strings.ToLower(p.RevisionStatus))
	}
	if p.SelectionSeed == "" {
		return SelectionResult{}, conflict("freeze the population first: the freeze records the seed the draw is ranked by")
	}
	qi := slices.IndexFunc(p.Quarters, func(q Quarter) bool { return q.Quarter == quarter })
	if qi < 0 {
		return SelectionResult{}, invalid("the plan has no Q%d in its quarterly plan", quarter)
	}
	if q := p.Quarters[qi]; q.SelectedAt != "" {
		return SelectionResult{}, conflict("Q%d was selected on %s by %s and is frozen", quarter, q.SelectedAt, firstNonEmpty(q.SelectedBy, "—"))
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return SelectionResult{}, err
	}
	sels, err := s.repo.Selections(id)
	if err != nil {
		return SelectionResult{}, err
	}
	view := computeSampling(p, pop, sels)

	result := SelectionResult{Quarter: quarter, Picks: []Selection{}, Shortfalls: []string{}, Warnings: []string{}}
	picked := map[int64]bool{}
	byRef := map[string]PopulationItem{}
	for _, it := range pop {
		byRef[it.ItemRef] = it
	}
	var summary []string
	for i, st := range p.Strata {
		z := view.Strata[i]
		if z.Error != "" {
			return SelectionResult{}, invalid("stratum %s cannot be sized: %s", st.Code, z.Error)
		}
		var refs []string
		for _, it := range pop {
			if it.StratumCode == st.Code && it.WaveQuarter == quarter {
				refs = append(refs, it.ItemRef)
			}
		}
		want := z.Statistical[quarter-1]
		basis := BasisStatistical
		if !st.SamplingEnabled {
			want, basis = len(refs), BasisFull
		}
		taken := 0
		for pos, r := range Rank(p.SelectionSeed, st.Code, refs) {
			if taken == want {
				break
			}
			it := byRef[r.ItemRef]
			if it.SelectedQuarter > 0 {
				continue
			}
			result.Picks = append(result.Picks, Selection{Quarter: quarter, PopulationID: it.ID, ItemRef: it.ItemRef, ItemName: it.Name,
				StratumCode: st.Code, Basis: basis, RankOrder: pos + 1, RankHash: r.Hash, PlanVersion: p.VersionCount})
			picked[it.ID] = true
			taken++
		}
		if taken < want {
			result.Shortfalls = append(result.Shortfalls, fmt.Sprintf("%s: the Q%d wave has %d item(s) not yet selected against a sample of %d; all were taken, and the shortfall is not borrowed from another wave",
				st.Code, quarter, taken, want))
		}
		if z.Flagged[quarter-1] != st.TopUps[quarter-1] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %d top-up(s) planned for Q%d, %d item(s) flagged", st.Code, st.TopUps[quarter-1], quarter, z.Flagged[quarter-1]))
		}
		summary = append(summary, fmt.Sprintf("%s %d", st.Code, taken))
	}
	var refused []string
	topups := 0
	for _, it := range pop {
		if it.TopUpQuarter != quarter {
			continue
		}
		switch {
		case it.SelectedQuarter > 0:
			refused = append(refused, fmt.Sprintf("%s is already a %s pick of Q%d", it.ItemRef, strings.ToLower(it.SelectionBasis), it.SelectedQuarter))
		case picked[it.ID]:
			refused = append(refused, fmt.Sprintf("%s is a statistical pick of this Q%d draw", it.ItemRef, quarter))
		default:
			result.Picks = append(result.Picks, Selection{Quarter: quarter, PopulationID: it.ID, ItemRef: it.ItemRef, ItemName: it.Name,
				StratumCode: it.StratumCode, Basis: BasisTopUp, PlanVersion: p.VersionCount})
			topups++
		}
	}
	if len(refused) > 0 {
		return SelectionResult{}, conflict("a top-up must not duplicate a statistical pick: %s. Flag another item for the top-up, or none, and select again",
			strings.Join(refused, "; "))
	}

	stamp := s.stamp()
	note := strings.Join(append(append([]string{}, result.Shortfalls...), result.Warnings...), " ")
	history := []HistoryEntry{{At: stamp, Actor: actor, Event: "selection", Subject: fmt.Sprintf("Q%d", quarter),
		ToValue: fmt.Sprintf("%d picks (%s; %d top-up)", len(result.Picks), strings.Join(summary, ", "), topups),
		Note:    strings.TrimSpace(fmt.Sprintf("seed commitment %s… under %s. %s", clip(p.SeedCommitment, 16), firstNonEmpty(p.LatestVersion, "the approved design"), note))}}
	var activate *Plan
	if p.Status == StatusApproved {
		p.ActivatedAt = stamp
		activate = &p
		history = append(history, HistoryEntry{At: stamp, Actor: actor, Event: "status", Subject: "plan",
			FromValue: StatusApproved, ToValue: StatusActive, Note: fmt.Sprintf("activated by the Q%d selection", quarter)})
	}
	if err := s.repo.SaveSelection(id, quarter, result.Picks, activate, history); err != nil {
		if errors.Is(err, errQuarterTaken) {
			return SelectionResult{}, conflict("Q%d was selected by someone else a moment ago; reload to see it", quarter)
		}
		return SelectionResult{}, err
	}
	for i := range result.Picks {
		result.Picks[i].SelectedAt, result.Picks[i].SelectedBy = stamp, actor
	}
	if result.Plan, err = s.Get(id); err != nil {
		return SelectionResult{}, err
	}
	return result, nil
}

// publicPlan is the plan as a reader may see it: the seed stays hidden behind
// its commitment until it is revealed.
func publicPlan(p Plan) Plan {
	if !p.SeedRevealed {
		p.SelectionSeed = ""
	}
	return p
}
