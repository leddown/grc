package assuranceplan

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"grc/internal/auditfinding"
	"grc/internal/policydocs"
)

// Proposal is a population worked out from another module's records: the
// policy documents due for review in the period, or the open audit findings.
// It is CSV text in the import's own format, so it goes through the same
// preview, validation and diff as a paste, and nothing is written until the
// preview is applied. It is applied with merge on, so an item the source no
// longer lists (a finding closed since) stays in the plan.
type Proposal struct {
	Source  string   `json:"source"`
	CSV     string   `json:"csv"`
	Items   int      `json:"items"`
	New     int      `json:"new"`
	Skipped int      `json:"skipped"`
	Notes   []string `json:"notes"`
	Merge   bool     `json:"merge"`
}

var proposalColumns = []string{"item_ref", "name", "stratum", "owner", "business_line", "wave", "source_kind", "source_ref"}

type proposed struct {
	item PopulationItem
	due  string
}

// ProposePopulation works out the population from the plan's source, or the
// one named. An item already in the population keeps its stratum and wave:
// the proposal adds and describes, and a move stays a deliberate edit.
func (s *Service) ProposePopulation(id int64, source string) (Proposal, error) {
	p, err := s.Get(id)
	if err != nil {
		return Proposal{}, err
	}
	if p.Status == StatusClosed {
		return Proposal{}, conflict("%s is closed; its population no longer changes", p.Reference)
	}
	source = strings.ToLower(strings.TrimSpace(firstNonEmpty(source, p.PopulationSource)))
	switch source {
	case SourcePolicies, SourceFindings:
	case "":
		return Proposal{}, invalid("the plan has no population source: choose policies or findings in the plan header")
	default:
		return Proposal{}, invalid("the population source is %s", strings.Join(PopulationSources, " or "))
	}
	_, okStart := parseDate(p.PeriodStart)
	_, okEnd := parseDate(p.PeriodEnd)
	if !okStart || !okEnd {
		return Proposal{}, invalid("the plan needs its period start and end before anything can be placed in a quarter")
	}
	takes := map[string]string{}
	for _, st := range p.Strata {
		for _, v := range st.Takes {
			if _, dup := takes[strings.ToLower(v)]; !dup {
				takes[strings.ToLower(v)] = st.Code
			}
		}
	}
	if len(takes) == 0 {
		what := "document types (policy, standard, procedure, …)"
		if source == SourceFindings {
			what = "finding severities (Critical, High, Medium, Low)"
		}
		return Proposal{}, invalid("no stratum takes anything from the proposal: list the %s each stratum takes on the Sampling tab", what)
	}
	current, err := s.repo.Population(id)
	if err != nil {
		return Proposal{}, err
	}

	out := Proposal{Source: source, Notes: []string{}, Merge: true}
	var items []proposed
	unmapped := map[string]int{}
	switch source {
	case SourcePolicies:
		if s.in.Policies == nil {
			return Proposal{}, conflict("the policy library is not available to this service")
		}
		items, err = s.proposePolicies(p, takes, unmapped, &out)
	default:
		if s.in.Findings == nil {
			return Proposal{}, conflict("Audit Findings is not available to this service")
		}
		items, err = s.proposeFindings(p, takes, unmapped, &out)
	}
	if err != nil {
		return Proposal{}, err
	}
	keys := make([]string, 0, len(unmapped))
	for k := range unmapped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Notes = append(out.Notes, fmt.Sprintf("%d left out: no stratum takes %q", unmapped[k], k))
		out.Skipped += unmapped[k]
	}

	byRef := map[string]PopulationItem{}
	for _, it := range current {
		byRef[it.ItemRef] = it
	}
	used := map[string]bool{}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(proposalColumns)
	for _, pr := range items {
		it := pr.item
		if used[it.ItemRef] {
			it.ItemRef += "#" + it.SourceRef
		}
		used[it.ItemRef] = true
		if prev, ok := byRef[it.ItemRef]; ok {
			if prev.StratumCode != it.StratumCode || prev.WaveQuarter != it.WaveQuarter {
				out.Notes = append(out.Notes, fmt.Sprintf("%s: the source now places it in %s %s; it stays in %s %s (move it with the CSV import, or a change request once frozen)",
					it.ItemRef, it.StratumCode, quarterLabel(it.WaveQuarter), prev.StratumCode, quarterLabel(prev.WaveQuarter)))
			}
			it.StratumCode, it.WaveQuarter = prev.StratumCode, prev.WaveQuarter
		} else {
			out.New++
		}
		_ = w.Write([]string{it.ItemRef, it.Name, it.StratumCode, it.Owner, it.BusinessLine, "Q" + strconv.Itoa(it.WaveQuarter), it.SourceKind, it.SourceRef})
		out.Items++
	}
	w.Flush()
	out.CSV = buf.String()
	if out.Items == 0 {
		out.Notes = append(out.Notes, "nothing to propose")
	}
	return out, nil
}

// proposePolicies takes the documents in force (approved, or approved and in
// review) whose review falls due by the period's end: the stored next review
// date, or the effective date plus the review cadence when none is stored.
// A review already overdue goes into the first wave. Letters have no review
// cycle and are never proposed.
func (s *Service) proposePolicies(p Plan, takes map[string]string, unmapped map[string]int, out *Proposal) ([]proposed, error) {
	docs, err := s.in.Policies.ListDocuments(policydocs.Filter{})
	if err != nil {
		return nil, fmt.Errorf("list policy documents: %w", err)
	}
	var items []proposed
	later, noClock, overdue := 0, 0, 0
	for _, d := range docs {
		if d.DocType == policydocs.TypeLetter || (d.Status != policydocs.StatusApproved && d.Status != policydocs.StatusInReview) {
			continue
		}
		if d.ClientProfileID != p.ClientProfileID {
			continue
		}
		due := d.NextReviewDate
		if due == "" && d.EffectiveDate != "" && d.ReviewCadenceMonths > 0 {
			if t, ok := parseDate(d.EffectiveDate); ok {
				due = formatDate(t.AddDate(0, d.ReviewCadenceMonths, 0))
			}
		}
		if due == "" {
			noClock++
			continue
		}
		if due > p.PeriodEnd {
			later++
			continue
		}
		code, ok := takes[strings.ToLower(d.DocType)]
		if !ok {
			unmapped[d.DocType]++
			continue
		}
		if due < p.PeriodStart {
			overdue++
		}
		ref := strings.TrimSpace(d.Reference)
		if ref == "" {
			ref = "DOC-" + strconv.FormatInt(d.ID, 10)
		}
		items = append(items, proposed{due: due, item: PopulationItem{ItemRef: ref, Name: d.Title, StratumCode: code,
			Owner: d.OwnerRole, BusinessLine: d.ClientName, WaveQuarter: waveFor(p.PeriodStart, due),
			SourceKind: SourceKindPolicy, SourceRef: strconv.FormatInt(d.ID, 10)}})
	}
	sortProposed(items)
	if overdue > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d document(s) were already overdue for review at the period start and are in the Q1 wave", overdue))
	}
	if later > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d document(s) are next due for review after the period ends", later))
	}
	if noClock > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d document(s) in force have no next review date and no effective date with a cadence: they have no review clock to plan by", noClock))
		out.Skipped += noClock
	}
	if p.ClientProfileID == 0 {
		out.Notes = append(out.Notes, "only documents without a client profile are proposed, as the plan has none")
	}
	return items, nil
}

// proposeFindings takes the open audit findings, each in the wave of the
// quarter its remediation is due. An overdue finding, or one with no due
// date, goes into the first wave; one due after the period is left for next
// year's roadmap.
func (s *Service) proposeFindings(p Plan, takes map[string]string, unmapped map[string]int, out *Proposal) ([]proposed, error) {
	findings, err := s.in.Findings.List(auditfinding.Filter{OpenOnly: true})
	if err != nil {
		return nil, fmt.Errorf("list open findings: %w", err)
	}
	var items []proposed
	later, overdue, undated := 0, 0, 0
	for _, f := range findings {
		due := f.DueDate
		switch {
		case due == "":
			undated++
			due = p.PeriodStart
		case due > p.PeriodEnd:
			later++
			continue
		case due < p.PeriodStart:
			overdue++
		}
		code, ok := takes[strings.ToLower(f.Severity)]
		if !ok {
			unmapped[f.Severity]++
			continue
		}
		items = append(items, proposed{due: due, item: PopulationItem{ItemRef: f.Reference, Name: f.Title, StratumCode: code,
			Owner: f.Owner, BusinessLine: f.BusinessUnit, WaveQuarter: waveFor(p.PeriodStart, due),
			SourceKind: SourceKindFinding, SourceRef: strconv.FormatInt(f.ID, 10)}})
	}
	sortProposed(items)
	if overdue > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d finding(s) were already overdue at the period start and are in the Q1 wave", overdue))
	}
	if undated > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d finding(s) have no due date and are in the Q1 wave", undated))
	}
	if later > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d open finding(s) are due after the period ends: they are left for next year's roadmap", later))
		out.Skipped += later
	}
	return items, nil
}

// waveFor is the quarter of the period a date falls in, the first quarter for
// a date before the period.
func waveFor(periodStart, date string) int {
	for q := 1; q <= 4; q++ {
		if end, ok := QuarterEnd(periodStart, q); ok && date <= end {
			return q
		}
	}
	return 4
}

func sortProposed(items []proposed) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].due != items[j].due {
			return items[i].due < items[j].due
		}
		return items[i].item.ItemRef < items[j].item.ItemRef
	})
}
