package assuranceplan

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("assurance plan not found")

// ValidationError is a request the caller can fix; the handler maps it to 400.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// ConflictError is a request the plan's state refuses: an edit to a locked
// design, or a lifecycle move that is not an edge. The handler maps it to 409.
type ConflictError struct{ msg string }

func (e *ConflictError) Error() string { return e.msg }

func conflict(format string, args ...any) error {
	return &ConflictError{msg: fmt.Sprintf(format, args...)}
}

const maxFieldLength = 20000

type Service struct {
	repo Repository
	now  func() time.Time
	// requireSeparateApprover refuses an approval by the author of the
	// revision being approved. It is on only under multi-user authentication,
	// as in policydocs: in single-user mode the author is necessarily the
	// approver, and enforcing it would make every plan unapprovable.
	requireSeparateApprover bool
	in                      Integrations
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) SetRequireSeparateApprover(required bool) { s.requireSeparateApprover = required }

func (s *Service) SeparationEnforced() bool { return s.requireSeparateApprover }

func (s *Service) stamp() string { return s.now().UTC().Format(time.RFC3339) }

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func derive(p *Plan) {
	p.DesignLocked = !designEditable(*p)
	p.PlanTypeName = planTypeName(p.PlanType)
	if p.RACIRoles == nil {
		p.RACIRoles = []string{}
	}
	if p.Strata == nil {
		p.Strata = []Stratum{}
	}
	p.SeedCommitment = seedCommitment(p.SelectionSeed)
	p.SeedRevealed = seedRevealed(*p)
}

func (s *Service) List(f Filter) ([]Plan, error) {
	f.Search = strings.TrimSpace(f.Search)
	items, err := s.repo.List(f)
	if err != nil {
		return nil, err
	}
	for i := range items {
		derive(&items[i])
	}
	return items, nil
}

func (s *Service) Get(id int64) (Plan, error) {
	p, err := s.repo.Get(id)
	if err != nil {
		return Plan{}, mapNotFound(err)
	}
	derive(&p)
	if p.Attributes == nil {
		p.Attributes = []Attribute{}
	}
	s.resolveAttributes(p.Attributes)
	return p, nil
}

func (s *Service) Clients() ([]Client, error) { return s.repo.Clients() }

func (s *Service) History(id int64) ([]HistoryEntry, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	return s.repo.History(id)
}

func (s *Service) Versions(id int64) ([]Version, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	return s.repo.Versions(id)
}

func (s *Service) Version(id int64, number int) (Version, error) {
	v, err := s.repo.Version(id, number)
	return v, mapNotFound(err)
}

func (s *Service) Lint(id int64) ([]LintFinding, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return nil, err
	}
	return Lint(p, pop), nil
}

// ---- create ----

type CreateInput struct {
	Type            string `json:"type"`
	Example         string `json:"example"`
	Title           string `json:"title"`
	PeriodStart     string `json:"period_start"`
	PeriodEnd       string `json:"period_end"`
	ClientProfileID int64  `json:"client_profile_id"`
	FunctionName    string `json:"function_name"`
}

// Create seeds a plan from a plan type or a worked example. The status is
// always Draft whatever the request says: the only way to Approved is review.
func (s *Service) Create(in CreateInput, actor string) (Plan, error) {
	key := strings.TrimSpace(in.Example)
	if key == "" {
		key = strings.TrimSpace(in.Type)
	}
	if key == "" {
		key = "blank"
	}
	tpl, ok := lookupTemplate(key)
	if !ok {
		return Plan{}, invalid("unknown plan type or example %q", key)
	}
	if strings.TrimSpace(in.Example) != "" && !tpl.Example {
		return Plan{}, invalid("%q is a plan type, not an example", key)
	}
	p, err := instantiate(tpl, strings.TrimSpace(in.PeriodStart), strings.TrimSpace(in.PeriodEnd), s.now().UTC())
	if err != nil {
		return Plan{}, err
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		p.Title = t
	}
	if f := strings.TrimSpace(in.FunctionName); f != "" {
		p.FunctionName = f
	}
	p.ClientProfileID = in.ClientProfileID
	return s.insertNew(p, nil, actor, HistoryEntry{Event: "created", ToValue: StatusDraft, Note: "from " + tpl.Name})
}

func (s *Service) insertNew(p Plan, population []PopulationItem, actor string, created HistoryEntry) (Plan, error) {
	if err := normalizeHeader(&p); err != nil {
		return Plan{}, err
	}
	if err := normalizeStrata(p.Strata); err != nil {
		return Plan{}, err
	}
	if p.Attributes == nil {
		p.Attributes = []Attribute{}
	}
	if err := normalizeAttributes(p.Attributes, p.Strata); err != nil {
		return Plan{}, err
	}
	if err := normalizeSteps(p.Steps); err != nil {
		return Plan{}, err
	}
	if err := normalizeQuarters(p.Quarters); err != nil {
		return Plan{}, err
	}
	g := Governance{RACIRoles: p.RACIRoles, RACI: p.RACI, Cadence: p.Cadence, Escalations: p.Escalations, Basis: p.Basis}
	if err := normalizeGovernance(&g); err != nil {
		return Plan{}, err
	}
	p.RACIRoles, p.RACI, p.Cadence, p.Escalations, p.Basis = g.RACIRoles, g.RACI, g.Cadence, g.Escalations, g.Basis

	ref, err := s.nextReference(p.PeriodStart)
	if err != nil {
		return Plan{}, err
	}
	stamp := s.stamp()
	p.Reference = ref
	p.Status = StatusDraft
	p.Revision = 1
	p.RevisionStatus = ""
	p.RevisionAuthor = actor
	p.ChangeReason = ""
	p.Author = actor
	p.ApprovedBy, p.ApprovedAt, p.ActivatedAt, p.ClosedAt = "", "", "", ""
	p.CreatedAt, p.CreatedBy, p.UpdatedAt, p.UpdatedBy = stamp, actor, stamp, actor
	created.At, created.Actor = stamp, actor
	id, err := s.repo.Create(p, population, []HistoryEntry{created})
	if err != nil {
		return Plan{}, err
	}
	return s.Get(id)
}

// nextReference is AP-YYYY-NNN, with YYYY the plan's own year rather than the
// year it was drafted in: a 2027 plan drafted in November 2026 is AP-2027-….
func (s *Service) nextReference(periodStart string) (string, error) {
	year := s.now().UTC().Year()
	if t, ok := parseDate(periodStart); ok {
		year = t.Year()
	}
	prefix := fmt.Sprintf("AP-%d-", year)
	refs, err := s.repo.ReferencesWithPrefix(prefix)
	if err != nil {
		return "", err
	}
	highest := 0
	for _, r := range refs {
		if n, err := strconv.Atoi(strings.TrimPrefix(r, prefix)); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%s%03d", prefix, highest+1), nil
}

// ---- design edits ----

func (s *Service) editable(id int64) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	if !designEditable(p) {
		if p.Status == StatusClosed {
			return Plan{}, conflict("%s is closed; its design can no longer change", p.Reference)
		}
		if p.RevisionStatus == StatusInReview || p.Status == StatusInReview {
			return Plan{}, conflict("%s is in review; return it to draft to change its design", p.Reference)
		}
		return Plan{}, conflict("%s's design is locked since approval: raise a change request with a reason to revise it", p.Reference)
	}
	return p, nil
}

// UpdateHeader saves the plan's header and parameters. Lifecycle fields,
// authorship and the seed are carried from the stored row: the request cannot
// set them.
func (s *Service) UpdateHeader(id int64, in Plan, actor string) (Plan, error) {
	prev, err := s.editable(id)
	if err != nil {
		return Plan{}, err
	}
	next := prev
	next.Title = in.Title
	next.FunctionName = in.FunctionName
	next.ClientProfileID = in.ClientProfileID
	next.PeriodStart = in.PeriodStart
	next.PeriodEnd = in.PeriodEnd
	next.MobiliseStart = in.MobiliseStart
	next.ClosingDate = in.ClosingDate
	next.Objective = in.Objective
	next.DecisionsRequested = in.DecisionsRequested
	next.RegulatoryRefs = in.RegulatoryRefs
	next.OwnerRole = in.OwnerRole
	next.AccountableRole = in.AccountableRole
	next.ApprovalForum = in.ApprovalForum
	next.NonWorkingDates = in.NonWorkingDates
	if in.Parameters != (Parameters{}) {
		next.Parameters = in.Parameters
	}
	// The plan type's settings are kept when the request leaves them out;
	// "none" clears the population source.
	if in.TestTypes != nil {
		next.TestTypes = in.TestTypes
	}
	if strings.TrimSpace(in.CoverageBasis) != "" {
		next.CoverageBasis = in.CoverageBasis
	}
	switch v := strings.TrimSpace(in.PopulationSource); v {
	case "":
	case "none":
		next.PopulationSource = ""
	default:
		next.PopulationSource = v
	}
	if err := normalizeHeader(&next); err != nil {
		return Plan{}, err
	}
	changed := headerChanges(prev, next)
	if len(changed) == 0 {
		return prev, nil
	}
	stamp := s.stamp()
	next.UpdatedAt, next.UpdatedBy = stamp, actor
	h := HistoryEntry{At: stamp, Actor: actor, Event: "design", Subject: "header", Note: strings.Join(changed, "; ")}
	if err := s.repo.SaveHeader(next, []HistoryEntry{h}, nil); err != nil {
		return Plan{}, mapNotFound(err)
	}
	return s.Get(id)
}

func headerChanges(a, b Plan) []string {
	var out []string
	cmp := func(label, x, y string) {
		if x != y {
			out = append(out, fmt.Sprintf("%s: %q → %q", label, clip(x, 60), clip(y, 60)))
		}
	}
	cmp("title", a.Title, b.Title)
	cmp("function", a.FunctionName, b.FunctionName)
	cmp("client", strconv.FormatInt(a.ClientProfileID, 10), strconv.FormatInt(b.ClientProfileID, 10))
	cmp("period start", a.PeriodStart, b.PeriodStart)
	cmp("period end", a.PeriodEnd, b.PeriodEnd)
	cmp("mobilise start", a.MobiliseStart, b.MobiliseStart)
	cmp("closing date", a.ClosingDate, b.ClosingDate)
	cmp("objective", a.Objective, b.Objective)
	cmp("decisions requested", a.DecisionsRequested, b.DecisionsRequested)
	cmp("regulatory references", a.RegulatoryRefs, b.RegulatoryRefs)
	cmp("owner role", a.OwnerRole, b.OwnerRole)
	cmp("accountable role", a.AccountableRole, b.AccountableRole)
	cmp("approval forum", a.ApprovalForum, b.ApprovalForum)
	cmp("non-working dates", a.NonWorkingDates, b.NonWorkingDates)
	cmp("test types", strings.Join(a.TestTypes, ", "), strings.Join(b.TestTypes, ", "))
	cmp("coverage basis", a.CoverageBasis, b.CoverageBasis)
	cmp("population source", firstNonEmpty(a.PopulationSource, "none"), firstNonEmpty(b.PopulationSource, "none"))
	if a.Parameters != b.Parameters {
		pa, _ := json.Marshal(a.Parameters)
		pb, _ := json.Marshal(b.Parameters)
		out = append(out, fmt.Sprintf("parameters: %s → %s", pa, pb))
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *Service) ReplaceSteps(id int64, steps []Step, actor string) (Plan, error) {
	prev, err := s.editable(id)
	if err != nil {
		return Plan{}, err
	}
	if steps == nil {
		steps = []Step{}
	}
	if err := normalizeSteps(steps); err != nil {
		return Plan{}, err
	}
	summary := stepChanges(prev.Steps, steps)
	if summary == "" {
		return prev, nil
	}
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "design", Subject: "steps",
		FromValue: strconv.Itoa(len(prev.Steps)), ToValue: strconv.Itoa(len(steps)), Note: summary}
	if err := s.repo.ReplaceSteps(id, steps, []HistoryEntry{h}); err != nil {
		return Plan{}, err
	}
	return s.Get(id)
}

func stepDesign(st Step) string {
	return strings.Join([]string{st.Phase, st.Title, st.StepType, st.ResponsibleRole, st.AccountableRole,
		st.StartDate, st.EndDate, st.Deliverable, st.Refs, st.Notes}, "\x1f")
}

// stepChanges summarises a step list edit by reference, for the history.
func stepChanges(before, after []Step) string {
	old := map[string]Step{}
	for _, st := range before {
		old[st.StepRef] = st
	}
	var added, changed []string
	seen := map[string]bool{}
	for _, st := range after {
		seen[st.StepRef] = true
		prev, ok := old[st.StepRef]
		switch {
		case !ok:
			added = append(added, st.StepRef)
		case stepDesign(prev) != stepDesign(st) || prev.Status != st.Status:
			changed = append(changed, st.StepRef)
		}
	}
	var removed []string
	for _, st := range before {
		if !seen[st.StepRef] {
			removed = append(removed, st.StepRef)
		}
	}
	reordered := false
	if len(added) == 0 && len(removed) == 0 && len(before) == len(after) {
		for i := range after {
			if before[i].StepRef != after[i].StepRef {
				reordered = true
				break
			}
		}
	}
	var parts []string
	list := func(label string, refs []string) {
		if len(refs) == 0 {
			return
		}
		if len(refs) > 12 {
			refs = append(refs[:12:12], fmt.Sprintf("and %d more", len(refs)-12))
		}
		parts = append(parts, label+" "+strings.Join(refs, ", "))
	}
	list("added", added)
	list("removed", removed)
	list("changed", changed)
	if reordered {
		parts = append(parts, "reordered")
	}
	return strings.Join(parts, "; ")
}

// UpdateStepStatus is execution data: it stays editable after approval, and
// only a closed plan refuses it.
func (s *Service) UpdateStepStatus(id, stepID int64, status, actor string) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	if p.Status == StatusClosed {
		return Plan{}, conflict("%s is closed", p.Reference)
	}
	status = strings.TrimSpace(status)
	if !slices.Contains(StepStatuses, status) {
		return Plan{}, invalid("step status must be one of %s", strings.Join(StepStatuses, ", "))
	}
	var step *Step
	for i := range p.Steps {
		if p.Steps[i].ID == stepID {
			step = &p.Steps[i]
		}
	}
	if step == nil {
		return Plan{}, ErrNotFound
	}
	if step.Status == status {
		return p, nil
	}
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "step status", Subject: step.StepRef, FromValue: step.Status, ToValue: status}
	if err := s.repo.UpdateStepStatus(id, stepID, status, []HistoryEntry{h}); err != nil {
		return Plan{}, mapNotFound(err)
	}
	return s.Get(id)
}

func (s *Service) ReplaceQuarters(id int64, quarters []Quarter, actor string) (Plan, error) {
	prev, err := s.editable(id)
	if err != nil {
		return Plan{}, err
	}
	if quarters == nil {
		quarters = []Quarter{}
	}
	if err := normalizeQuarters(quarters); err != nil {
		return Plan{}, err
	}
	keepSelection(prev.Quarters, quarters)
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "design", Subject: "quarters",
		FromValue: quarterSummary(prev.Quarters), ToValue: quarterSummary(quarters)}
	if h.FromValue == h.ToValue {
		return prev, nil
	}
	if err := s.repo.ReplaceQuarters(id, quarters, []HistoryEntry{h}); err != nil {
		return Plan{}, err
	}
	return s.Get(id)
}

// keepSelection carries a quarter's frozen-selection stamp across an edit of
// its dates: only the selection action sets it, and no edit clears it.
func keepSelection(prev, next []Quarter) {
	for i := range next {
		next[i].SelectedAt, next[i].SelectedBy = "", ""
		for _, q := range prev {
			if q.Quarter == next[i].Quarter {
				next[i].SelectedAt, next[i].SelectedBy = q.SelectedAt, q.SelectedBy
			}
		}
	}
}

func quarterSummary(qs []Quarter) string {
	parts := make([]string, 0, len(qs))
	for _, q := range qs {
		parts = append(parts, fmt.Sprintf("Q%d %s/%s/%s/%s-%s/%s/%s %q r%g o%g", q.Quarter, q.LOD1WindowStart, q.LOD1Deadline,
			q.SelectionDate, q.FieldworkStart, q.FieldworkEnd, q.FindingsAgreedBy, q.ReportDate, q.CommitteeLabel, q.RetestDays, q.OverheadDays))
	}
	return strings.Join(parts, "; ")
}

// GenerateQuarters rebuilds the quarter-generated steps from the plan type's
// quarterly step template, against the plan's quarters. With dates set, the
// quarters' dates are first reset to the default cycle for the period.
func (s *Service) GenerateQuarters(id int64, dates bool, actor string) (Plan, error) {
	prev, err := s.editable(id)
	if err != nil {
		return Plan{}, err
	}
	cal := NewCalendar(prev.NonWorkingDates)
	quarters := slices.Clone(prev.Quarters)
	if dates {
		start, ok := parseDate(prev.PeriodStart)
		if !ok {
			return Plan{}, invalid("set the period start before generating the quarterly cycle")
		}
		fresh := DefaultQuarters(start, cal)
		for i := range fresh {
			for _, q := range prev.Quarters {
				if q.Quarter == fresh[i].Quarter {
					fresh[i].RetestDays, fresh[i].OverheadDays = q.RetestDays, q.OverheadDays
				}
			}
		}
		keepSelection(prev.Quarters, fresh)
		quarters = fresh
	}

	var generated []Step
	if tpl, ok := lookupTemplate(prev.PlanType); ok {
		start, _ := parseDate(prev.PeriodStart)
		end, _ := parseDate(prev.PeriodEnd)
		ctx := dateContext{cal: cal, anchors: map[string]string{
			"period_start": prev.PeriodStart, "period_end": prev.PeriodEnd,
			"mobilise_start": prev.MobiliseStart, "closing_date": prev.ClosingDate,
		}, quarters: map[int]Quarter{}}
		for _, q := range quarters {
			ctx.quarters[q.Quarter] = q
		}
		for _, ts := range tpl.Steps {
			if !ts.Quarterly {
				continue
			}
			steps, err := ts.expand(ctx, quarters, textFiller(start, end))
			if err != nil {
				return Plan{}, err
			}
			generated = append(generated, steps...)
		}
	}
	status := map[string]string{}
	var steps []Step
	for _, st := range prev.Steps {
		if st.Generated {
			status[st.StepRef] = st.Status
			continue
		}
		steps = append(steps, st)
	}
	for _, st := range generated {
		if v, ok := status[st.StepRef]; ok {
			st.Status = v
		}
		steps = append(steps, st)
	}
	sortStepsByRef(steps)
	if err := normalizeSteps(steps); err != nil {
		return Plan{}, err
	}

	stamp := s.stamp()
	if dates {
		h := HistoryEntry{At: stamp, Actor: actor, Event: "design", Subject: "quarters",
			FromValue: quarterSummary(prev.Quarters), ToValue: quarterSummary(quarters), Note: "reset to the default cycle"}
		if err := s.repo.ReplaceQuarters(id, quarters, []HistoryEntry{h}); err != nil {
			return Plan{}, err
		}
	}
	h := HistoryEntry{At: stamp, Actor: actor, Event: "design", Subject: "steps",
		FromValue: strconv.Itoa(len(prev.Steps)), ToValue: strconv.Itoa(len(steps)),
		Note: "quarterly steps regenerated from the cycle; " + firstNonEmpty(stepChanges(prev.Steps, steps), "no change")}
	if err := s.repo.ReplaceSteps(id, steps, []HistoryEntry{h}); err != nil {
		return Plan{}, err
	}
	return s.Get(id)
}

func (s *Service) ReplaceGovernance(id int64, g Governance, actor string) (Plan, error) {
	prev, err := s.editable(id)
	if err != nil {
		return Plan{}, err
	}
	if g.Basis == nil {
		g.Basis = prev.Basis
	}
	if err := normalizeGovernance(&g); err != nil {
		return Plan{}, err
	}
	before, _ := json.Marshal(stripGovIDs(prev))
	after, _ := json.Marshal(g)
	if string(before) == string(after) {
		return prev, nil
	}
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "design", Subject: "governance",
		Note: fmt.Sprintf("%d RACI activities × %d roles, %d cadence rows, %d escalation triggers, %d regulatory basis rows",
			len(g.RACI), len(g.RACIRoles), len(g.Cadence), len(g.Escalations), len(g.Basis))}
	if err := s.repo.ReplaceGovernance(id, g.RACIRoles, g, []HistoryEntry{h}); err != nil {
		return Plan{}, err
	}
	return s.Get(id)
}

func stripGovIDs(p Plan) Governance {
	g := Governance{RACIRoles: p.RACIRoles}
	for i, r := range p.RACI {
		g.RACI = append(g.RACI, RACIRow{Ordinal: i, Activity: r.Activity, Cells: r.Cells})
	}
	for i, c := range p.Cadence {
		c.ID, c.Ordinal = 0, i
		g.Cadence = append(g.Cadence, c)
	}
	for i, e := range p.Escalations {
		e.ID, e.Ordinal = 0, i
		g.Escalations = append(g.Escalations, e)
	}
	for i, b := range p.Basis {
		b.ID, b.Ordinal = 0, i
		g.Basis = append(g.Basis, b)
	}
	if g.RACI == nil {
		g.RACI = []RACIRow{}
	}
	if g.Cadence == nil {
		g.Cadence = []Cadence{}
	}
	if g.Escalations == nil {
		g.Escalations = []Escalation{}
	}
	if g.Basis == nil {
		g.Basis = []BasisRow{}
	}
	return g
}

// Delete removes a draft that was never approved. Anything that has been
// approved is evidence and is closed, not deleted.
func (s *Service) Delete(id int64) error {
	p, err := s.Get(id)
	if err != nil {
		return err
	}
	if p.Status != StatusDraft || p.VersionCount > 0 {
		return conflict("%s has been submitted or approved; only a draft that was never approved can be deleted", p.Reference)
	}
	return mapNotFound(s.repo.Delete(id))
}

// ---- normalisation ----

func normalizeHeader(p *Plan) error {
	for _, f := range []*string{&p.Title, &p.FunctionName, &p.PeriodStart, &p.PeriodEnd, &p.MobiliseStart,
		&p.ClosingDate, &p.Objective, &p.DecisionsRequested, &p.RegulatoryRefs, &p.OwnerRole,
		&p.AccountableRole, &p.ApprovalForum, &p.NonWorkingDates} {
		*f = strings.TrimSpace(*f)
		if len(*f) > maxFieldLength {
			return invalid("a field is longer than %d characters", maxFieldLength)
		}
	}
	if p.Title == "" {
		return invalid("the plan needs a title")
	}
	for _, d := range []struct{ label, value string }{
		{"period start", p.PeriodStart}, {"period end", p.PeriodEnd},
		{"mobilisation start", p.MobiliseStart}, {"closing date", p.ClosingDate},
	} {
		if d.value != "" {
			if _, ok := parseDate(d.value); !ok {
				return invalid("%s %q is not a date (YYYY-MM-DD)", d.label, d.value)
			}
		}
	}
	if p.PeriodStart != "" && p.PeriodEnd != "" && p.PeriodEnd < p.PeriodStart {
		return invalid("the period ends (%s) before it starts (%s)", p.PeriodEnd, p.PeriodStart)
	}
	dates, err := parseNonWorkingDates(p.NonWorkingDates)
	if err != nil {
		return invalid("%s", err.Error())
	}
	p.NonWorkingDates = strings.Join(dates, ", ")
	if p.ClientProfileID < 0 {
		p.ClientProfileID = 0
	}
	if err := normalizePlanType(p); err != nil {
		return err
	}
	return validateParameters(p.Parameters)
}

const (
	maxTestType  = 60
	maxTestTypes = 20
)

// normalizePlanType checks the settings a plan type seeds: the test types,
// the coverage basis and the population source.
func normalizePlanType(p *Plan) error {
	types := []string{}
	seen := map[string]bool{}
	for _, t := range p.TestTypes {
		t = strings.TrimSpace(t)
		switch {
		case t == "":
			continue
		case len(t) > maxTestType:
			return invalid("test type %q is longer than %d characters", clip(t, 20), maxTestType)
		case seen[strings.ToLower(t)]:
			continue
		}
		seen[strings.ToLower(t)] = true
		types = append(types, t)
	}
	if len(types) > maxTestTypes {
		return invalid("a plan has at most %d test types", maxTestTypes)
	}
	p.TestTypes = types
	p.CoverageBasis = strings.ToLower(strings.TrimSpace(p.CoverageBasis))
	if p.CoverageBasis == "" {
		p.CoverageBasis = CoverageAttestation
	}
	if !slices.Contains(CoverageBases, p.CoverageBasis) {
		return invalid("the coverage basis is %s", strings.Join(CoverageBases, " or "))
	}
	p.PopulationSource = strings.ToLower(strings.TrimSpace(p.PopulationSource))
	if p.PopulationSource != "" && !slices.Contains(PopulationSources, p.PopulationSource) {
		return invalid("the population source is %s, or none", strings.Join(PopulationSources, " or "))
	}
	return nil
}

func validateParameters(pr Parameters) error {
	switch {
	case pr.TesterFTE < 0:
		return invalid("tester FTE cannot be negative")
	case pr.DaysPerFTEQuarter < 0:
		return invalid("productive days per FTE per quarter cannot be negative")
	case pr.UtilisationThreshold <= 0 || pr.UtilisationThreshold > 2:
		return invalid("the utilisation threshold is a fraction such as 0.85, above 0 and at most 2")
	case pr.RAGAmberPoints < 0 || pr.RAGAmberPoints > 100:
		return invalid("the amber band is 0 to 100 percentage points")
	case pr.ResultPartialMin < 1 || pr.ResultIneffectiveMin <= pr.ResultPartialMin:
		return invalid("test result thresholds need 1 ≤ partially effective < ineffective (fails)")
	case pr.DeviationExtendAt < 1 || pr.DeviationThematicAt <= pr.DeviationExtendAt:
		return invalid("deviation thresholds need 1 ≤ extend sample < not effective")
	case pr.ExtendPct < 0 || pr.ExtendPct > 1000:
		return invalid("the sample extension is 0 to 1000 percent")
	case pr.EvidenceSLADays < 1 || pr.EvidenceSLADays > 260:
		return invalid("the evidence SLA is 1 to 260 business days")
	}
	return nil
}

func normalizeSteps(steps []Step) error {
	seen := map[string]bool{}
	for i := range steps {
		st := &steps[i]
		for _, f := range []*string{&st.StepRef, &st.Phase, &st.Title, &st.StepType, &st.ResponsibleRole,
			&st.AccountableRole, &st.StartDate, &st.EndDate, &st.Deliverable, &st.Refs, &st.Status, &st.Notes} {
			*f = strings.TrimSpace(*f)
			if len(*f) > maxFieldLength {
				return invalid("a step field is longer than %d characters", maxFieldLength)
			}
		}
		if st.StepRef == "" {
			return invalid("step %d has no reference", i+1)
		}
		if seen[st.StepRef] {
			return invalid("step reference %s is used twice", st.StepRef)
		}
		seen[st.StepRef] = true
		if st.Title == "" {
			return invalid("step %s has no title", st.StepRef)
		}
		if st.StepType == "" {
			st.StepType = StepActivity
		}
		if !slices.Contains(StepTypes, st.StepType) {
			return invalid("step %s: type must be one of %s", st.StepRef, strings.Join(StepTypes, ", "))
		}
		if st.Status == "" {
			st.Status = StepNotStarted
		}
		if !slices.Contains(StepStatuses, st.Status) {
			return invalid("step %s: status must be one of %s", st.StepRef, strings.Join(StepStatuses, ", "))
		}
		for _, d := range []string{st.StartDate, st.EndDate} {
			if d != "" {
				if _, ok := parseDate(d); !ok {
					return invalid("step %s: %q is not a date (YYYY-MM-DD)", st.StepRef, d)
				}
			}
		}
		if st.StepType == StepMilestone {
			date := firstNonEmpty(st.StartDate, st.EndDate)
			st.StartDate, st.EndDate = date, date
		}
		if st.Quarter < 0 || st.Quarter > 4 {
			st.Quarter = 0
		}
		st.Ordinal = i
	}
	return nil
}

func normalizeQuarters(quarters []Quarter) error {
	seen := map[int]bool{}
	for i := range quarters {
		q := &quarters[i]
		if q.Quarter < 1 || q.Quarter > 4 {
			return invalid("quarter numbers are 1 to 4")
		}
		if seen[q.Quarter] {
			return invalid("Q%d is listed twice", q.Quarter)
		}
		seen[q.Quarter] = true
		for _, f := range []*string{&q.LOD1WindowStart, &q.LOD1Deadline, &q.SelectionDate, &q.FieldworkStart,
			&q.FieldworkEnd, &q.FindingsAgreedBy, &q.ReportDate} {
			*f = strings.TrimSpace(*f)
			if *f != "" {
				if _, ok := parseDate(*f); !ok {
					return invalid("Q%d: %q is not a date (YYYY-MM-DD)", q.Quarter, *f)
				}
			}
		}
		q.CommitteeLabel = strings.TrimSpace(q.CommitteeLabel)
		if q.RetestDays < 0 || q.OverheadDays < 0 {
			return invalid("Q%d: retest and overhead days cannot be negative", q.Quarter)
		}
	}
	slices.SortFunc(quarters, func(a, b Quarter) int { return a.Quarter - b.Quarter })
	return nil
}

func normalizeGovernance(g *Governance) error {
	roles := []string{}
	seen := map[string]bool{}
	for _, r := range g.RACIRoles {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		roles = append(roles, r)
	}
	g.RACIRoles = roles
	raci := []RACIRow{}
	for _, row := range g.RACI {
		activity := strings.TrimSpace(row.Activity)
		if activity == "" {
			continue
		}
		cells := map[string]string{}
		for role, v := range row.Cells {
			v = strings.ToUpper(strings.TrimSpace(v))
			if v == "R/A" {
				v = "A/R"
			}
			if v == "" || !seen[strings.TrimSpace(role)] {
				continue
			}
			if !slices.Contains(RACILetters, v) {
				return invalid("RACI %q: %q is not one of %s", activity, v, strings.Join(RACILetters, ", "))
			}
			cells[strings.TrimSpace(role)] = v
		}
		raci = append(raci, RACIRow{Ordinal: len(raci), Activity: activity, Cells: cells})
	}
	g.RACI = raci
	cadence := []Cadence{}
	for _, c := range g.Cadence {
		c = Cadence{Forum: strings.TrimSpace(c.Forum), Frequency: strings.TrimSpace(c.Frequency),
			Content: strings.TrimSpace(c.Content), Owner: strings.TrimSpace(c.Owner)}
		if c.Forum == "" && c.Frequency == "" && c.Content == "" {
			continue
		}
		c.Ordinal = len(cadence)
		cadence = append(cadence, c)
	}
	g.Cadence = cadence
	escalations := []Escalation{}
	for _, e := range g.Escalations {
		e = Escalation{Trigger: strings.TrimSpace(e.Trigger), EscalateTo: strings.TrimSpace(e.EscalateTo), Within: strings.TrimSpace(e.Within)}
		if e.Trigger == "" && e.EscalateTo == "" {
			continue
		}
		e.Ordinal = len(escalations)
		escalations = append(escalations, e)
	}
	g.Escalations = escalations
	basis := []BasisRow{}
	for _, b := range g.Basis {
		b = BasisRow{Requirement: strings.TrimSpace(b.Requirement), Reference: strings.TrimSpace(b.Reference), How: strings.TrimSpace(b.How)}
		if b.Requirement == "" && b.Reference == "" && b.How == "" {
			continue
		}
		for _, f := range []string{b.Requirement, b.Reference, b.How} {
			if len(f) > maxFieldLength {
				return invalid("regulatory basis row %d: a field is longer than %d characters", len(basis)+1, maxFieldLength)
			}
		}
		b.Ordinal = len(basis)
		basis = append(basis, b)
	}
	g.Basis = basis
	return nil
}

// ---- lifecycle ----

// Submit sends the plan, or the revision a change request opened, to review.
func (s *Service) Submit(id int64, actor string) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	switch {
	case p.Status == StatusDraft:
		return s.moveStatus(p, StatusInReview, actor, "")
	case p.RevisionStatus == StatusDraft:
		return s.moveRevision(p, StatusInReview, actor, "")
	}
	return Plan{}, conflict("%s is %s; only a draft can be submitted for review", p.Reference, describeState(p))
}

// Return sends a plan or revision under review back to draft.
func (s *Service) Return(id int64, note, actor string) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	note = strings.TrimSpace(note)
	switch {
	case p.Status == StatusInReview:
		return s.moveStatus(p, StatusDraft, actor, note)
	case p.RevisionStatus == StatusInReview:
		return s.moveRevision(p, StatusDraft, actor, note)
	}
	return Plan{}, conflict("%s is %s; only a plan in review can be returned to draft", p.Reference, describeState(p))
}

func (s *Service) Activate(id int64, actor string) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != StatusApproved {
		return Plan{}, conflict("%s is %s; only an approved plan can be activated", p.Reference, describeState(p))
	}
	p.ActivatedAt = s.stamp()
	return s.moveStatus(p, StatusActive, actor, "")
}

func (s *Service) Close(id int64, note, actor string) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != StatusActive {
		return Plan{}, conflict("%s is %s; only an active plan can be closed", p.Reference, describeState(p))
	}
	if p.RevisionStatus != "" {
		return Plan{}, conflict("%s has revision %d %s; approve or return it before closing the plan", p.Reference, p.Revision, strings.ToLower(p.RevisionStatus))
	}
	p.ClosedAt = s.stamp()
	return s.moveStatus(p, StatusClosed, actor, strings.TrimSpace(note))
}

// ChangeRequest opens the next design revision of an approved or active plan.
// The approved snapshot is untouched; the revision goes back through review
// and approval, and execution carries on against the plan meanwhile.
func (s *Service) ChangeRequest(id int64, reason, actor string) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != StatusApproved && p.Status != StatusActive {
		return Plan{}, conflict("%s is %s; a change request is for an approved or active plan (a draft is edited directly)", p.Reference, describeState(p))
	}
	if p.RevisionStatus != "" {
		return Plan{}, conflict("%s already has revision %d open (%s)", p.Reference, p.Revision, strings.ToLower(p.RevisionStatus))
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Plan{}, invalid("a change request needs a reason")
	}
	stamp := s.stamp()
	p.Revision++
	p.RevisionStatus = StatusDraft
	p.RevisionAuthor = actor
	p.ChangeReason = reason
	p.UpdatedAt, p.UpdatedBy = stamp, actor
	h := HistoryEntry{At: stamp, Actor: actor, Event: "change request", Subject: fmt.Sprintf("revision %d", p.Revision), Note: reason}
	if err := s.repo.SaveHeader(p, []HistoryEntry{h}, nil); err != nil {
		return Plan{}, mapNotFound(err)
	}
	return s.Get(id)
}

func (s *Service) moveStatus(p Plan, to, actor, note string) (Plan, error) {
	stamp := s.stamp()
	from := p.Status
	p.Status = to
	p.UpdatedAt, p.UpdatedBy = stamp, actor
	h := HistoryEntry{At: stamp, Actor: actor, Event: "status", Subject: "plan", FromValue: from, ToValue: to, Note: note}
	if err := s.repo.SaveHeader(p, []HistoryEntry{h}, nil); err != nil {
		return Plan{}, mapNotFound(err)
	}
	return s.Get(p.ID)
}

func (s *Service) moveRevision(p Plan, to, actor, note string) (Plan, error) {
	stamp := s.stamp()
	from := p.RevisionStatus
	p.RevisionStatus = to
	p.UpdatedAt, p.UpdatedBy = stamp, actor
	h := HistoryEntry{At: stamp, Actor: actor, Event: "revision status", Subject: fmt.Sprintf("revision %d", p.Revision),
		FromValue: from, ToValue: to, Note: note}
	if err := s.repo.SaveHeader(p, []HistoryEntry{h}, nil); err != nil {
		return Plan{}, mapNotFound(err)
	}
	return s.Get(p.ID)
}

func describeState(p Plan) string {
	if p.RevisionStatus != "" {
		return fmt.Sprintf("%s with revision %d %s", p.Status, p.Revision, strings.ToLower(p.RevisionStatus))
	}
	return p.Status
}

// ApprovalResult is an approval and anything the approver should see about it.
type ApprovalResult struct {
	Plan     Plan          `json:"plan"`
	Version  Version       `json:"version"`
	Warnings []string      `json:"warnings"`
	Advisory []LintFinding `json:"advisory"`
}

// Approve approves the plan under review, or the revision under review, and
// writes the immutable snapshot of what was approved.
func (s *Service) Approve(id int64, approver, note string) (ApprovalResult, error) {
	p, err := s.Get(id)
	if err != nil {
		return ApprovalResult{}, err
	}
	first := p.Status == StatusInReview
	revision := !first && p.RevisionStatus == StatusInReview
	if !first && !revision {
		return ApprovalResult{}, conflict("%s is %s; submit it for review before approving", p.Reference, describeState(p))
	}
	approver = strings.TrimSpace(approver)
	if approver == "" {
		return ApprovalResult{}, invalid("an approver is required")
	}
	result := ApprovalResult{Warnings: []string{}}
	author := strings.TrimSpace(p.RevisionAuthor)
	if author != "" && strings.EqualFold(author, approver) {
		if s.requireSeparateApprover {
			return ApprovalResult{}, invalid("%q wrote this revision and cannot also approve it; a different person must approve", author)
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"Approved by its author (%s): separation of duties is not enforced in single-user mode.", author))
	}
	pop, err := s.repo.Population(id)
	if err != nil {
		return ApprovalResult{}, err
	}
	findings := Lint(p, pop)
	if blocking := BlockingFindings(findings); len(blocking) > 0 {
		return ApprovalResult{}, ApprovalError{Findings: blocking}
	}
	for _, f := range findings {
		if f.Severity == LintAdvisory {
			result.Advisory = append(result.Advisory, f)
		}
	}

	stamp := s.stamp()
	reason := p.ChangeReason
	if first {
		p.Status = StatusApproved
	}
	p.RevisionStatus = ""
	p.ChangeReason = ""
	p.ApprovedBy, p.ApprovedAt = approver, stamp
	p.UpdatedAt, p.UpdatedBy = stamp, approver
	number := p.VersionCount + 1
	snapshot, sum, err := snapshotOf(p, pop)
	if err != nil {
		return ApprovalResult{}, err
	}
	version := Version{Number: number, Label: fmt.Sprintf("v%d", number), ApprovedBy: approver, ApprovedAt: stamp,
		ChangeReason: firstNonEmpty(reason, note), SnapshotJSON: snapshot, SnapshotSHA256: sum}
	h := []HistoryEntry{{At: stamp, Actor: approver, Event: "approved", Subject: version.Label,
		ToValue: StatusApproved, Note: strings.Join(append([]string{strings.TrimSpace(note)}, result.Warnings...), " ")}}
	if first {
		h = append([]HistoryEntry{{At: stamp, Actor: approver, Event: "status", Subject: "plan",
			FromValue: StatusInReview, ToValue: StatusApproved}}, h...)
	}
	if err := s.repo.SaveHeader(p, h, &version); err != nil {
		return ApprovalResult{}, mapNotFound(err)
	}
	if result.Plan, err = s.Get(id); err != nil {
		return ApprovalResult{}, err
	}
	result.Version = version
	result.Version.SnapshotJSON = ""
	if result.Advisory == nil {
		result.Advisory = []LintFinding{}
	}
	return result, nil
}

// planSnapshot is what an approval records: the plan's design and the
// population it was sized on.
type planSnapshot struct {
	Plan
	Population []PopulationItem `json:"population"`
}

// snapshotOf is the approved plan as JSON, with the derived fields that
// describe its state at the time cleared so the snapshot is only the design.
// The seed is not design and stays out; its commitment is kept. Each item
// keeps its membership and description, not its execution record.
func snapshotOf(p Plan, pop []PopulationItem) (string, string, error) {
	p.DesignLocked = true
	p.VersionCount = 0
	p.LatestVersion = ""
	p.SelectionSeed = ""
	p.SeedRevealed = false
	p.FrozenAt, p.FrozenBy = "", ""
	p.PopulationSize = len(pop)
	items := make([]PopulationItem, len(pop))
	for i, it := range pop {
		items[i] = designOf(it)
		items[i].ID = it.ID
	}
	raw, err := json.Marshal(planSnapshot{Plan: p, Population: items})
	if err != nil {
		return "", "", fmt.Errorf("encode plan snapshot: %w", err)
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:]), nil
}

// ---- clone ----

// Clone copies a plan's design for the next period: header, parameters,
// steps, quarters and governance, with every date moved one year and rolled
// onto a business day. Execution data does not come across, and the clone
// starts as a new draft with no seed: one is drawn when its population is
// frozen.
func (s *Service) Clone(id int64, title, actor string) (Plan, error) {
	src, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	startY, endY := s.now().UTC().Year(), s.now().UTC().Year()
	if t, ok := parseDate(src.PeriodStart); ok {
		startY = t.Year()
	}
	if t, ok := parseDate(src.PeriodEnd); ok {
		endY = t.Year()
	}
	years := yearShifter(startY-1, endY+1)

	c := src
	c.ID = 0
	c.PeriodStart = shiftYear(src.PeriodStart)
	c.PeriodEnd = shiftYear(src.PeriodEnd)
	shiftedHolidays := []string{}
	if dates, err := parseNonWorkingDates(src.NonWorkingDates); err == nil {
		for _, d := range dates {
			shiftedHolidays = append(shiftedHolidays, shiftYear(d))
		}
	}
	c.NonWorkingDates = strings.Join(shiftedHolidays, ", ")
	cal := NewCalendar(c.NonWorkingDates)
	move := func(d string) string { return shiftWorkingDate(d, c.PeriodEnd, cal) }

	c.MobiliseStart = move(src.MobiliseStart)
	c.ClosingDate = move(src.ClosingDate)
	if t := strings.TrimSpace(title); t != "" {
		c.Title = t
	} else {
		c.Title = years(src.Title)
	}
	c.Objective = years(src.Objective)
	c.DecisionsRequested = years(src.DecisionsRequested)
	c.SelectionSeed = ""
	c.AIGenerated, c.Model, c.PromptHash = false, "", ""

	c.Steps = make([]Step, len(src.Steps))
	for i, st := range src.Steps {
		st.ID = 0
		st.Title = years(st.Title)
		st.Deliverable = years(st.Deliverable)
		st.StartDate = move(st.StartDate)
		st.EndDate = move(st.EndDate)
		st.Status = StepNotStarted
		c.Steps[i] = st
	}
	c.Quarters = make([]Quarter, len(src.Quarters))
	for i, q := range src.Quarters {
		c.Quarters[i] = Quarter{
			Quarter:          q.Quarter,
			LOD1WindowStart:  move(q.LOD1WindowStart),
			LOD1Deadline:     move(q.LOD1Deadline),
			SelectionDate:    move(q.SelectionDate),
			FieldworkStart:   move(q.FieldworkStart),
			FieldworkEnd:     move(q.FieldworkEnd),
			FindingsAgreedBy: move(q.FindingsAgreedBy),
			ReportDate:       move(q.ReportDate),
			CommitteeLabel:   years(q.CommitteeLabel),
			RetestDays:       q.RetestDays,
			OverheadDays:     q.OverheadDays,
		}
	}
	gov := stripGovIDs(src)
	c.RACI, c.Cadence, c.Escalations, c.Basis = gov.RACI, gov.Cadence, gov.Escalations, gov.Basis
	c.Strata = make([]Stratum, len(src.Strata))
	for i, st := range src.Strata {
		st.ID = 0
		c.Strata[i] = st
	}
	srcPop, err := s.repo.Population(id)
	if err != nil {
		return Plan{}, err
	}
	tests, err := s.repo.Tests(id)
	if err != nil {
		return Plan{}, err
	}
	signedOff := map[int64]string{}
	for _, t := range tests {
		if t.QADate != "" && t.QADate > signedOff[t.ItemID] {
			signedOff[t.ItemID] = t.QADate
		}
	}
	pop := make([]PopulationItem, len(srcPop))
	for i, it := range srcPop {
		pop[i] = designOf(it)
		// This year's signed-off test is next year's last test, which is
		// what moves a multi-year cycle on.
		if d := signedOff[it.ID]; d > pop[i].LastTestedOn {
			pop[i].LastTestedOn = d
		}
	}
	note := "cloned from " + src.Reference
	if c.NonWorkingDates != "" {
		note += "; non-working dates moved one year: check movable holidays"
	}
	if len(pop) > 0 {
		note += fmt.Sprintf("; population of %d items copied without attestations, DQ reviews or top-ups", len(pop))
	}
	return s.insertNew(c, pop, actor, HistoryEntry{Event: "created", ToValue: StatusDraft, Note: note})
}

// designOf is an item's membership and description without its execution
// record: what a clone carries into next year and an approval snapshots.
func designOf(it PopulationItem) PopulationItem {
	return PopulationItem{ItemRef: it.ItemRef, Name: it.Name, StratumCode: it.StratumCode, CIF: it.CIF,
		BusinessLine: it.BusinessLine, Hosting: it.Hosting, InternetFacing: it.InternetFacing, Owner: it.Owner,
		WaveQuarter: it.WaveQuarter, LastTestedOn: it.LastTestedOn, SourceKind: it.SourceKind, SourceRef: it.SourceRef}
}

func shiftYear(d string) string {
	t, ok := parseDate(d)
	if !ok {
		return d
	}
	return formatDate(t.AddDate(1, 0, 0))
}

// shiftWorkingDate moves a working date one year and rolls it onto a business
// day: forward, unless that would carry a date inside the period past its end.
func shiftWorkingDate(d, periodEnd string, cal Calendar) string {
	t, ok := parseDate(d)
	if !ok {
		return d
	}
	t = t.AddDate(1, 0, 0)
	rolled := cal.Roll(t)
	if end, ok := parseDate(periodEnd); ok && !t.After(end) && rolled.After(end) {
		rolled = cal.RollBack(t)
	}
	return formatDate(rolled)
}

// yearShifter moves every year in [lo, hi] mentioned in a text on by one, so a
// cloned "Draft the 2028 plan" reads "Draft the 2029 plan". A year written as
// part of a citation such as 2024/1774 is left alone.
func yearShifter(lo, hi int) func(string) string {
	return func(text string) string {
		var out strings.Builder
		for i := 0; i < len(text); {
			if !isDigit(text[i]) {
				out.WriteByte(text[i])
				i++
				continue
			}
			j := i
			for j < len(text) && isDigit(text[j]) {
				j++
			}
			run := text[i:j]
			cited := (i > 0 && text[i-1] == '/') || (j < len(text) && text[j] == '/')
			if y, err := strconv.Atoi(run); err == nil && len(run) == 4 && !cited && y >= lo && y <= hi {
				run = strconv.Itoa(y + 1)
			}
			out.WriteString(run)
			i = j
		}
		return out.String()
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
