package assuranceplan

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.json
var templateFS embed.FS

// PlanTemplate is a plan type, or a worked example of one. It seeds a new plan
// and is never read again by that plan: what the plan holds is what was
// approved, whatever later happens to the template.
type PlanTemplate struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Order       int    `json:"order"`
	// Example marks a worked instance with fixed dates; PlanType is then the
	// type it instantiates. For a type, PlanType is its own key.
	Example  bool             `json:"example"`
	PlanType string           `json:"plan_type"`
	Plan     templatePlan     `json:"plan"`
	Quarters templateQuarters `json:"quarters"`
	Steps    []templateStep   `json:"steps"`
	RACI     []RACIRow        `json:"raci"`
	Cadence  []Cadence        `json:"cadence"`
	Escal    []Escalation     `json:"escalations"`
}

type templatePlan struct {
	Title              string      `json:"title"`
	FunctionName       string      `json:"function_name"`
	Objective          string      `json:"objective"`
	DecisionsRequested string      `json:"decisions_requested"`
	RegulatoryRefs     string      `json:"regulatory_refs"`
	OwnerRole          string      `json:"owner_role"`
	AccountableRole    string      `json:"accountable_role"`
	ApprovalForum      string      `json:"approval_forum"`
	PeriodStart        string      `json:"period_start"`
	PeriodEnd          string      `json:"period_end"`
	MobiliseStart      string      `json:"mobilise_start"`
	ClosingDate        string      `json:"closing_date"`
	NonWorkingDates    string      `json:"non_working_dates"`
	RACIRoles          []string    `json:"raci_roles"`
	Parameters         *Parameters `json:"parameters"`
}

type templateQuarters struct {
	// Mode is "default" (generated from the period), "explicit" or "none".
	Mode         string    `json:"mode"`
	Explicit     []Quarter `json:"explicit"`
	RetestDays   []float64 `json:"retest_days"`
	OverheadDays []float64 `json:"overhead_days"`
}

type templateStep struct {
	StepRef         string `json:"step_ref"`
	Phase           string `json:"phase"`
	Title           string `json:"title"`
	StepType        string `json:"step_type"`
	ResponsibleRole string `json:"responsible_role"`
	AccountableRole string `json:"accountable_role"`
	Deliverable     string `json:"deliverable"`
	Refs            string `json:"refs"`
	From            string `json:"from"`
	To              string `json:"to"`
	// Quarterly steps are repeated for every quarter, with {q} replaced by the
	// quarter number and their dates resolved against that quarter.
	Quarterly    bool  `json:"quarterly"`
	SkipQuarters []int `json:"skip_quarters"`
}

// TemplateInfo is what the page lists.
type TemplateInfo struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Example     bool   `json:"example"`
	PlanType    string `json:"plan_type"`
}

var (
	templatesOnce sync.Once
	templatesList []PlanTemplate
	templatesErr  error
)

func loadTemplates() ([]PlanTemplate, error) {
	templatesOnce.Do(func() {
		entries, err := templateFS.ReadDir("templates")
		if err != nil {
			templatesErr = fmt.Errorf("read plan templates: %w", err)
			return
		}
		for _, e := range entries {
			raw, err := templateFS.ReadFile(path.Join("templates", e.Name()))
			if err != nil {
				templatesErr = fmt.Errorf("read plan template %s: %w", e.Name(), err)
				return
			}
			var t PlanTemplate
			if err := json.Unmarshal(raw, &t); err != nil {
				templatesErr = fmt.Errorf("parse plan template %s: %w", e.Name(), err)
				return
			}
			if !t.Example {
				t.PlanType = t.Key
			}
			templatesList = append(templatesList, t)
		}
		sort.SliceStable(templatesList, func(i, j int) bool { return templatesList[i].Order < templatesList[j].Order })
	})
	return templatesList, templatesErr
}

func Templates() []TemplateInfo {
	list, _ := loadTemplates()
	out := make([]TemplateInfo, 0, len(list))
	for _, t := range list {
		out = append(out, TemplateInfo{Key: t.Key, Name: t.Name, Description: t.Description, Example: t.Example, PlanType: t.PlanType})
	}
	return out
}

func lookupTemplate(key string) (PlanTemplate, bool) {
	list, _ := loadTemplates()
	for _, t := range list {
		if t.Key == key {
			return t, true
		}
	}
	return PlanTemplate{}, false
}

// planTypeName is the display name of a plan type key.
func planTypeName(key string) string {
	if t, ok := lookupTemplate(key); ok && !t.Example {
		return t.Name
	}
	return key
}

// instantiate builds a plan from a template. The period comes from the
// request when given, else from the template (examples), else it is the next
// calendar year.
func instantiate(t PlanTemplate, periodStart, periodEnd string, now time.Time) (Plan, error) {
	if periodStart == "" {
		periodStart = t.Plan.PeriodStart
	}
	if periodEnd == "" {
		periodEnd = t.Plan.PeriodEnd
	}
	if periodStart == "" {
		periodStart = fmt.Sprintf("%d-01-01", now.Year()+1)
	}
	start, ok := parseDate(periodStart)
	if !ok {
		return Plan{}, invalid("period start %q is not a date (YYYY-MM-DD)", periodStart)
	}
	if periodEnd == "" {
		periodEnd = formatDate(start.AddDate(1, 0, -1))
	}
	end, ok := parseDate(periodEnd)
	if !ok {
		return Plan{}, invalid("period end %q is not a date (YYYY-MM-DD)", periodEnd)
	}
	if end.Before(start) {
		return Plan{}, invalid("the period ends (%s) before it starts (%s)", periodEnd, periodStart)
	}

	fill := textFiller(start, end)
	p := Plan{
		Title:              fill(t.Plan.Title),
		PlanType:           t.PlanType,
		FunctionName:       t.Plan.FunctionName,
		Objective:          fill(t.Plan.Objective),
		DecisionsRequested: fill(t.Plan.DecisionsRequested),
		RegulatoryRefs:     t.Plan.RegulatoryRefs,
		OwnerRole:          t.Plan.OwnerRole,
		AccountableRole:    t.Plan.AccountableRole,
		ApprovalForum:      t.Plan.ApprovalForum,
		PeriodStart:        formatDate(start),
		PeriodEnd:          formatDate(end),
		NonWorkingDates:    t.Plan.NonWorkingDates,
		RACIRoles:          slices.Clone(t.Plan.RACIRoles),
		Parameters:         DefaultParameters(),
	}
	if t.Plan.Parameters != nil {
		p.Parameters = *t.Plan.Parameters
	}
	cal := NewCalendar(p.NonWorkingDates)
	ctx := dateContext{cal: cal, anchors: map[string]string{"period_start": p.PeriodStart, "period_end": p.PeriodEnd}}
	var err error
	if p.MobiliseStart, err = ctx.eval(t.Plan.MobiliseStart); err != nil {
		return Plan{}, err
	}
	if p.ClosingDate, err = ctx.eval(t.Plan.ClosingDate); err != nil {
		return Plan{}, err
	}
	ctx.anchors["mobilise_start"] = p.MobiliseStart
	ctx.anchors["closing_date"] = p.ClosingDate

	switch t.Quarters.Mode {
	case "default":
		p.Quarters = DefaultQuarters(start, cal)
	case "explicit":
		p.Quarters = slices.Clone(t.Quarters.Explicit)
	}
	for i := range p.Quarters {
		if i < len(t.Quarters.RetestDays) {
			p.Quarters[i].RetestDays = t.Quarters.RetestDays[i]
		}
		if i < len(t.Quarters.OverheadDays) {
			p.Quarters[i].OverheadDays = t.Quarters.OverheadDays[i]
		}
	}
	ctx.quarters = map[int]Quarter{}
	for _, q := range p.Quarters {
		ctx.quarters[q.Quarter] = q
	}

	for _, ts := range t.Steps {
		if !ts.Quarterly {
			s, err := ts.build(ctx, fill)
			if err != nil {
				return Plan{}, err
			}
			p.Steps = append(p.Steps, s)
			continue
		}
		generated, err := ts.expand(ctx, p.Quarters, fill)
		if err != nil {
			return Plan{}, err
		}
		p.Steps = append(p.Steps, generated...)
	}
	sortStepsByRef(p.Steps)

	p.RACI = slices.Clone(t.RACI)
	p.Cadence = slices.Clone(t.Cadence)
	p.Escalations = slices.Clone(t.Escal)
	return p, nil
}

func (ts templateStep) build(ctx dateContext, fill func(string) string) (Step, error) {
	s := Step{
		StepRef:         fill(ts.StepRef),
		Phase:           fill(ts.Phase),
		Title:           fill(ts.Title),
		StepType:        ts.StepType,
		ResponsibleRole: ts.ResponsibleRole,
		AccountableRole: ts.AccountableRole,
		Deliverable:     fill(ts.Deliverable),
		Refs:            ts.Refs,
		Status:          StepNotStarted,
	}
	var err error
	if s.StartDate, err = ctx.eval(ts.From); err != nil {
		return Step{}, fmt.Errorf("step %s: %w", s.StepRef, err)
	}
	if s.EndDate, err = ctx.eval(ts.To); err != nil {
		return Step{}, fmt.Errorf("step %s: %w", s.StepRef, err)
	}
	if s.StepType == StepMilestone && s.EndDate == "" {
		s.EndDate = s.StartDate
	}
	if s.EndDate != "" && s.StartDate != "" && s.EndDate < s.StartDate {
		s.EndDate = s.StartDate
	}
	return s, nil
}

func (ts templateStep) expand(ctx dateContext, quarters []Quarter, fill func(string) string) ([]Step, error) {
	var out []Step
	for _, q := range quarters {
		if slices.Contains(ts.SkipQuarters, q.Quarter) {
			continue
		}
		qn := strconv.Itoa(q.Quarter)
		qfill := func(s string) string { return fill(strings.ReplaceAll(s, "{q}", qn)) }
		qctx := ctx
		qc := q
		qctx.quarter = &qc
		s, err := ts.build(qctx, qfill)
		if err != nil {
			return nil, err
		}
		s.Quarter = q.Quarter
		s.Generated = true
		out = append(out, s)
	}
	return out, nil
}

// textFiller fills {year}, {next_year} and {period_end_long} in a
// template's text.
func textFiller(start, end time.Time) func(string) string {
	r := strings.NewReplacer(
		"{year}", strconv.Itoa(start.Year()),
		"{next_year}", strconv.Itoa(start.Year()+1),
		"{period_end_long}", end.Format("2 January 2006"),
	)
	return r.Replace
}

// sortStepsByRef orders steps by their reference read as numbers, with any
// lettered reference (O.1, the ongoing steps) after the numbered ones.
func sortStepsByRef(steps []Step) {
	sort.SliceStable(steps, func(i, j int) bool { return refLess(steps[i].StepRef, steps[j].StepRef) })
	for i := range steps {
		steps[i].Ordinal = i
	}
}

func refLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		switch {
		case ea == nil && eb == nil:
			if na != nb {
				return na < nb
			}
		case ea == nil:
			return true
		case eb == nil:
			return false
		default:
			if pa[i] != pb[i] {
				return pa[i] < pb[i]
			}
		}
	}
	return len(pa) < len(pb)
}
