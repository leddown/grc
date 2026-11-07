package assuranceplan

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"grc/internal/knowledge"
)

// DraftInput is the brief a plan is drafted from.
type DraftInput struct {
	Objective      string `json:"objective"`
	Regulation     string `json:"regulation"`
	PopulationSize int    `json:"population_size"`
	Constraints    string `json:"constraints"`
}

// DraftProposal is a drafted plan: its own steps (the quarterly cycle's steps
// are generated from the quarter dates, not drafted), the quarter dates and
// the RACI. References is the list the model was shown to cite from.
type DraftProposal struct {
	Objective  string         `json:"objective"`
	Steps      []DraftStep    `json:"steps"`
	Quarters   []DraftQuarter `json:"quarters"`
	RACIRoles  []string       `json:"raci_roles"`
	RACI       []DraftRACI    `json:"raci"`
	Unknowns   []string       `json:"unknowns"`
	Notes      []string       `json:"notes"`
	References []AIRef        `json:"references"`
}

type DraftStep struct {
	StepRef         string  `json:"step_ref"`
	Phase           string  `json:"phase"`
	Title           string  `json:"title"`
	StepType        string  `json:"step_type"`
	ResponsibleRole string  `json:"responsible_role"`
	AccountableRole string  `json:"accountable_role"`
	StartDate       string  `json:"start_date"`
	EndDate         string  `json:"end_date"`
	Deliverable     string  `json:"deliverable"`
	Basis           string  `json:"basis"`
	Cites           []AIRef `json:"cites"`
	Unresolved      []AIRef `json:"unresolved"`
}

type DraftQuarter struct {
	Quarter          int    `json:"quarter"`
	LOD1WindowStart  string `json:"lod1_window_start"`
	LOD1Deadline     string `json:"lod1_deadline"`
	SelectionDate    string `json:"selection_date"`
	FieldworkStart   string `json:"fieldwork_start"`
	FieldworkEnd     string `json:"fieldwork_end"`
	FindingsAgreedBy string `json:"findings_agreed_by"`
	ReportDate       string `json:"report_date"`
	CommitteeLabel   string `json:"committee_label"`
}

type DraftRACI struct {
	Activity string            `json:"activity"`
	Cells    map[string]string `json:"cells"`
}

type draftAnswer struct {
	Objective string         `json:"objective"`
	Steps     []draftStepIn  `json:"steps"`
	Quarters  []DraftQuarter `json:"quarters"`
	RACIRoles []string       `json:"raci_roles"`
	RACI      []DraftRACI    `json:"raci"`
	Unknowns  []string       `json:"unknowns"`
}

type draftStepIn struct {
	StepRef         string   `json:"step_ref"`
	Phase           string   `json:"phase"`
	Title           string   `json:"title"`
	StepType        string   `json:"step_type"`
	ResponsibleRole string   `json:"responsible_role"`
	AccountableRole string   `json:"accountable_role"`
	StartDate       string   `json:"start_date"`
	EndDate         string   `json:"end_date"`
	Deliverable     string   `json:"deliverable"`
	Basis           string   `json:"basis"`
	Cites           []aiCite `json:"cites"`
}

const (
	maxDraftSteps = 60
	maxDraftRACI  = 40
	maxRoles      = 12
)

func (in *DraftInput) normalize() error {
	in.Objective, in.Regulation, in.Constraints = strings.TrimSpace(in.Objective), strings.TrimSpace(in.Regulation), strings.TrimSpace(in.Constraints)
	if in.Objective == "" && in.Regulation == "" && in.Constraints == "" && in.PopulationSize == 0 {
		return invalid("the brief is empty: give the objective, the regulation, the population or the constraints")
	}
	for _, f := range []string{in.Objective, in.Regulation, in.Constraints} {
		if len(f) > maxBrief {
			return invalid("a field of the brief is longer than %d characters", maxBrief)
		}
	}
	if in.PopulationSize < 0 || in.PopulationSize > 10_000_000 {
		return invalid("the population size is a count of items, 0 to 10,000,000")
	}
	return nil
}

// DraftPlan asks for the plan's steps, quarter dates and RACI from a brief.
func (s *Service) DraftPlan(ctx context.Context, id int64, in DraftInput, actor string) (AIProposal, error) {
	if err := in.normalize(); err != nil {
		return AIProposal{}, err
	}
	p, err := s.editable(id)
	if err != nil {
		return AIProposal{}, err
	}
	done, err := s.begin(id)
	if err != nil {
		return AIProposal{}, err
	}
	defer done()

	sl, clauses, err := s.buildShortlist(strings.Join([]string{in.Regulation, in.Objective, p.Objective}, " "),
		map[string]int{knowledge.KindRegulationClause: 8})
	if err != nil {
		return AIProposal{}, err
	}
	own := planRefs(p)
	prompt := draftPrompt(p, in, own, clauses)
	var ans draftAnswer
	resp, err := s.askJSON(ctx, prompt, func(text string) error { return decodeContract(text, &ans) })
	if err != nil {
		return AIProposal{}, err
	}
	prop := s.checkDraft(p, ans, sl, own)
	prop.References = append(slices.Clone(sl.items), ownRefs(own)...)
	return s.record(p, AIKindDraft, "", in, prop, resp, promptHash(aiRules, prompt), actor)
}

func ownRefs(own []string) []AIRef {
	out := []AIRef{}
	for _, r := range own {
		out = append(out, AIRef{Kind: RefKindPlan, Ref: r, OnShortlist: true})
	}
	return out
}

func draftPrompt(p Plan, in DraftInput, own []string, clauses string) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("Task: draft this assurance plan's own steps, its quarterly operating cycle dates and its RACI from the brief. A person will review the draft before anything is saved.")
	line("\nThe plan:")
	line("- %s %s, type %s, owned by %s", p.Reference, p.Title, planTypeName(p.PlanType), firstNonEmpty(p.FunctionName, "the control function"))
	line("- period %s to %s, mobilisation from %s, closing date %s", p.PeriodStart, p.PeriodEnd, firstNonEmpty(p.MobiliseStart, "not set"), firstNonEmpty(p.ClosingDate, "not set"))
	line("- owner %s, accountable %s, approval forum %s", firstNonEmpty(p.OwnerRole, "not set"), firstNonEmpty(p.AccountableRole, "not set"), firstNonEmpty(p.ApprovalForum, "not set"))
	line("- objective: %s", firstNonEmpty(oneLine(p.Objective), "not set"))
	if strings.TrimSpace(p.NonWorkingDates) != "" {
		line("- non-working dates: %s", oneLine(p.NonWorkingDates))
	}
	for _, st := range p.Strata {
		line("- stratum %s %s: planned size %d, sampling %v", st.Code, st.Name, st.PlannedSize, st.SamplingEnabled)
	}
	line("- RACI roles now: %s", strings.Join(p.RACIRoles, " | "))
	line("\nThe plan's own steps now (keep, revise or replace them):")
	for _, st := range p.Steps {
		if !st.Generated {
			line("- %s [%s] %s; %s; %s to %s; responsible %s; deliverable %s", st.StepRef, st.StepType, st.Title, st.Phase, st.StartDate, st.EndDate, st.ResponsibleRole, st.Deliverable)
		}
	}
	line("\nThe quarterly cycle's steps are generated from the quarter dates; do not list them. Their references are:")
	refs := []string{}
	for _, st := range p.Steps {
		if st.Generated {
			refs = append(refs, st.StepRef)
		}
	}
	line("%s", firstNonEmpty(strings.Join(refs, ", "), "none"))
	line("\nThe quarters now:")
	for _, q := range p.Quarters {
		line("- Q%d: 1LoD window %s, deadline %s, selection %s, fieldwork %s to %s, findings agreed by %s, report %s, committee %s",
			q.Quarter, q.LOD1WindowStart, q.LOD1Deadline, q.SelectionDate, q.FieldworkStart, q.FieldworkEnd, q.FindingsAgreedBy, q.ReportDate, q.CommitteeLabel)
	}
	line("\nThe brief:")
	line("- objective: %s", firstNonEmpty(in.Objective, "not given"))
	line("- regulation: %s", firstNonEmpty(in.Regulation, "not given"))
	if in.PopulationSize > 0 {
		line("- population: about %d items", in.PopulationSize)
	} else {
		line("- population: not given")
	}
	line("- constraints: %s", firstNonEmpty(in.Constraints, "none given"))
	line("\nReferences you may cite (cite them exactly, as {\"kind\", \"ref\"}):")
	for _, r := range own {
		line("- {\"kind\": \"plan\", \"ref\": %q}", r)
	}
	b.WriteString(clauses)
	if len(own) == 0 && clauses == "" {
		line("- none: cite nothing, and mark every step good_practice")
	}
	line(`
Reply with one JSON object:
{"objective": "the objective, revised from the brief, or empty to keep the plan's",
 "steps": [{"step_ref": "0.1", "phase": "...", "title": "...", "step_type": "activity|milestone|ongoing", "responsible_role": "one of raci_roles",
   "accountable_role": "one of raci_roles", "start_date": "YYYY-MM-DD", "end_date": "YYYY-MM-DD", "deliverable": "...",
   "basis": "regulation|good_practice", "cites": [{"kind": "plan|regulation_clause", "ref": "..."}]}],
 "quarters": [{"quarter": 1, "lod1_window_start": "YYYY-MM-DD", "lod1_deadline": "YYYY-MM-DD", "selection_date": "YYYY-MM-DD",
   "fieldwork_start": "YYYY-MM-DD", "fieldwork_end": "YYYY-MM-DD", "findings_agreed_by": "YYYY-MM-DD", "report_date": "YYYY-MM-DD", "committee_label": "..."}],
 "raci_roles": ["..."],
 "raci": [{"activity": "...", "cells": {"role": "R|A|C|I|A/R"}}],
 "unknowns": ["what the brief does not tell you"]}
Dates fall between the mobilisation start and the closing date, in each quarter's own order. A milestone has one date. Leave "quarters" empty to keep the plan's dates, and "raci" empty to keep its RACI.`)
	return b.String()
}

// checkDraft keeps what can be applied and says what it left out. The draft
// is validated the same way whether the model or a person wrote it.
func (s *Service) checkDraft(p Plan, ans draftAnswer, sl shortlist, own []string) DraftProposal {
	out := DraftProposal{Objective: clip(strings.TrimSpace(ans.Objective), maxAIText), Steps: []DraftStep{}, Quarters: []DraftQuarter{},
		RACIRoles: []string{}, RACI: []DraftRACI{}, Unknowns: cleanList(ans.Unknowns, 20, 500), Notes: []string{}, References: []AIRef{}}
	note := func(format string, args ...any) { out.Notes = append(out.Notes, fmt.Sprintf(format, args...)) }

	roles := cleanList(ans.RACIRoles, maxRoles, 120)
	out.RACIRoles = roles
	known := roles
	if len(known) == 0 {
		known = p.RACIRoles
	}
	isRole := func(r string) bool { return slices.Contains(known, r) }

	lo := firstNonEmpty(p.MobiliseStart, p.PeriodStart)
	hi := firstNonEmpty(p.ClosingDate, p.PeriodEnd)
	generated := map[string]bool{}
	for _, st := range p.Steps {
		if st.Generated {
			generated[st.StepRef] = true
		}
	}
	seen := map[string]bool{}
	for i, in := range ans.Steps {
		if len(out.Steps) == maxDraftSteps {
			note("only the first %d steps were kept", maxDraftSteps)
			break
		}
		st := DraftStep{StepRef: clip(strings.TrimSpace(in.StepRef), 20), Phase: clip(strings.TrimSpace(in.Phase), 200),
			Title: clip(strings.TrimSpace(in.Title), 300), StepType: strings.ToLower(strings.TrimSpace(in.StepType)),
			ResponsibleRole: strings.TrimSpace(in.ResponsibleRole), AccountableRole: strings.TrimSpace(in.AccountableRole),
			StartDate: strings.TrimSpace(in.StartDate), EndDate: strings.TrimSpace(in.EndDate),
			Deliverable: clip(strings.TrimSpace(in.Deliverable), 500)}
		if st.StepRef == "" {
			st.StepRef = "AI." + strconv.Itoa(i+1)
		}
		switch {
		case st.Title == "":
			note("step %s has no title and was left out", st.StepRef)
			continue
		case generated[st.StepRef]:
			note("step %s is generated from the quarter dates and was left out", st.StepRef)
			continue
		case seen[st.StepRef]:
			note("step %s is listed twice; the second was left out", st.StepRef)
			continue
		}
		if !slices.Contains(StepTypes, st.StepType) {
			st.StepType = StepActivity
		}
		if st.StepType == StepMilestone {
			d := firstNonEmpty(st.EndDate, st.StartDate)
			st.StartDate, st.EndDate = d, d
		}
		_, okS := parseDate(st.StartDate)
		_, okE := parseDate(st.EndDate)
		if !okS || !okE {
			note("step %s has no valid dates (YYYY-MM-DD) and was left out", st.StepRef)
			continue
		}
		if st.EndDate < st.StartDate {
			note("step %s ends before it starts and was left out", st.StepRef)
			continue
		}
		if (lo != "" && st.StartDate < lo) || (hi != "" && st.EndDate > hi) {
			note("step %s runs outside the plan's span (%s to %s)", st.StepRef, lo, hi)
		}
		for _, r := range []string{st.ResponsibleRole, st.AccountableRole} {
			if r != "" && !isRole(r) {
				note("step %s names %q, which is not one of the RACI roles", st.StepRef, r)
			}
		}
		if st.ResponsibleRole == "" {
			note("step %s has no responsible role", st.StepRef)
		}
		st.Cites, st.Unresolved = s.resolveCites(in.Cites, sl, own)
		if len(st.Unresolved) > 0 {
			note("step %s cites %s, which do not resolve and are not kept", st.StepRef, refList(st.Unresolved))
		}
		st.Basis = basisFor(in.Basis, st.Cites, func() {
			note("step %s is marked regulation but cites nothing that resolves; it is marked good practice", st.StepRef)
		})
		seen[st.StepRef] = true
		out.Steps = append(out.Steps, st)
	}

	seenQ := map[int]bool{}
	for _, q := range ans.Quarters {
		if q.Quarter < 1 || q.Quarter > 4 || seenQ[q.Quarter] {
			continue
		}
		seenQ[q.Quarter] = true
		q.CommitteeLabel = clip(strings.TrimSpace(q.CommitteeLabel), 120)
		dates := []string{q.LOD1WindowStart, q.LOD1Deadline, q.SelectionDate, q.FieldworkStart, q.FieldworkEnd, q.FindingsAgreedBy, q.ReportDate}
		ok, prev := true, ""
		for i, d := range dates {
			d = strings.TrimSpace(d)
			dates[i] = d
			if _, valid := parseDate(d); !valid || (lo != "" && d < lo) || (hi != "" && d > hi) || d < prev {
				ok = false
			}
			prev = d
		}
		if !ok {
			note("the Q%d dates are missing, out of order or outside the plan's span; the plan's Q%d dates are kept", q.Quarter, q.Quarter)
			continue
		}
		q.LOD1WindowStart, q.LOD1Deadline, q.SelectionDate, q.FieldworkStart, q.FieldworkEnd, q.FindingsAgreedBy, q.ReportDate =
			dates[0], dates[1], dates[2], dates[3], dates[4], dates[5], dates[6]
		out.Quarters = append(out.Quarters, q)
	}
	slices.SortFunc(out.Quarters, func(a, b DraftQuarter) int { return a.Quarter - b.Quarter })

	for _, row := range ans.RACI {
		if len(out.RACI) == maxDraftRACI {
			break
		}
		activity := clip(strings.TrimSpace(row.Activity), 300)
		if activity == "" {
			continue
		}
		cells := map[string]string{}
		accountable := 0
		for role, v := range row.Cells {
			role, v = strings.TrimSpace(role), strings.ToUpper(strings.TrimSpace(v))
			if v == "R/A" {
				v = "A/R"
			}
			if v == "" {
				continue
			}
			if !isRole(role) {
				note("RACI %q names %q, which is not one of the roles; the cell is left out", activity, role)
				continue
			}
			if !slices.Contains(RACILetters, v) {
				note("RACI %q gives %s %q, which is not R, A, C, I or A/R; the cell is left out", activity, role, v)
				continue
			}
			if v == "A" || v == "A/R" {
				accountable++
			}
			cells[role] = v
		}
		if accountable != 1 {
			note("RACI %q has %d accountable roles; it should have one", activity, accountable)
		}
		out.RACI = append(out.RACI, DraftRACI{Activity: activity, Cells: cells})
	}
	if len(out.RACI) > 0 && len(roles) == 0 {
		out.RACIRoles = slices.Clone(p.RACIRoles)
	}
	if len(roles) > 0 && len(out.RACI) == 0 {
		note("the draft names RACI roles but no RACI rows; the plan's RACI is kept")
		out.RACIRoles = []string{}
	}
	return out
}

// basisFor marks an item regulation only when it cites something that
// resolves; anything else is good practice.
func basisFor(claimed string, cites []AIRef, downgraded func()) string {
	if strings.EqualFold(strings.TrimSpace(claimed), BasisRegulation) {
		if len(cites) > 0 {
			return BasisRegulation
		}
		downgraded()
	}
	return BasisGoodPractice
}

func draftAnswerOf(d DraftProposal) draftAnswer {
	ans := draftAnswer{Objective: d.Objective, Quarters: d.Quarters, RACIRoles: d.RACIRoles, RACI: d.RACI, Unknowns: d.Unknowns}
	for _, st := range d.Steps {
		ans.Steps = append(ans.Steps, draftStepIn{StepRef: st.StepRef, Phase: st.Phase, Title: st.Title, StepType: st.StepType,
			ResponsibleRole: st.ResponsibleRole, AccountableRole: st.AccountableRole, StartDate: st.StartDate, EndDate: st.EndDate,
			Deliverable: st.Deliverable, Basis: st.Basis, Cites: citesOf(st.Cites, st.Unresolved)})
	}
	return ans
}

func shortlistOf(refs []AIRef) (shortlist, []string) {
	sl := shortlist{byKey: map[string]AIRef{}}
	own := []string{}
	for _, r := range refs {
		if r.Kind == RefKindPlan {
			own = append(own, r.Ref)
			continue
		}
		sl.byKey[citeKey(r.Kind, r.Ref)] = r
		sl.items = append(sl.items, r)
	}
	return sl, own
}

var draftParts = []string{"objective", "steps", "quarters", "raci"}

// acceptDraft applies the parts chosen, after checking the draft again. The
// shortlist is the one stored with the proposal, never one from the edit.
func (s *Service) acceptDraft(id int64, pr AIProposal, payload []byte, parts []string, actor string) (DraftProposal, []string, error) {
	p, err := s.editable(id)
	if err != nil {
		return DraftProposal{}, nil, err
	}
	var stored, edited DraftProposal
	if err := decodePayload(pr.Payload, &stored); err != nil {
		return DraftProposal{}, nil, err
	}
	if err := decodePayload(payload, &edited); err != nil {
		return DraftProposal{}, nil, err
	}
	sl, own := shortlistOf(stored.References)
	d := s.checkDraft(p, draftAnswerOf(edited), sl, own)
	d.References = stored.References
	want := map[string]bool{}
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if !slices.Contains(draftParts, part) {
			return DraftProposal{}, nil, invalid("a draft's parts are %s", strings.Join(draftParts, ", "))
		}
		want[part] = true
	}
	all := len(want) == 0
	apply := func(part string, present bool) bool { return (all || want[part]) && present }
	notes := []string{}

	var steps []Step
	if apply("steps", len(d.Steps) > 0) {
		status := map[string]string{}
		for _, st := range p.Steps {
			status[st.StepRef] = st.Status
		}
		for _, st := range d.Steps {
			steps = append(steps, Step{StepRef: st.StepRef, Phase: st.Phase, Title: st.Title, StepType: st.StepType,
				ResponsibleRole: st.ResponsibleRole, AccountableRole: st.AccountableRole, StartDate: st.StartDate, EndDate: st.EndDate,
				Deliverable: st.Deliverable, Refs: strings.Join(refsOf(st.Cites), "; "), Status: status[st.StepRef]})
		}
		for _, st := range p.Steps {
			if st.Generated {
				steps = append(steps, st)
			}
		}
		sortStepsByRef(steps)
		if err := normalizeSteps(slices.Clone(steps)); err != nil {
			return DraftProposal{}, nil, err
		}
	}
	var quarters []Quarter
	if apply("quarters", len(d.Quarters) > 0) {
		quarters = slices.Clone(p.Quarters)
		for _, dq := range d.Quarters {
			i := slices.IndexFunc(quarters, func(q Quarter) bool { return q.Quarter == dq.Quarter })
			if i < 0 {
				quarters = append(quarters, Quarter{Quarter: dq.Quarter})
				i = len(quarters) - 1
			}
			q := &quarters[i]
			q.LOD1WindowStart, q.LOD1Deadline, q.SelectionDate, q.FieldworkStart = dq.LOD1WindowStart, dq.LOD1Deadline, dq.SelectionDate, dq.FieldworkStart
			q.FieldworkEnd, q.FindingsAgreedBy, q.ReportDate = dq.FieldworkEnd, dq.FindingsAgreedBy, dq.ReportDate
			if dq.CommitteeLabel != "" {
				q.CommitteeLabel = dq.CommitteeLabel
			}
		}
		if err := normalizeQuarters(quarters); err != nil {
			return DraftProposal{}, nil, err
		}
	}
	var gov *Governance
	if apply("raci", len(d.RACI) > 0) {
		g := stripGovIDs(p)
		g.RACIRoles = d.RACIRoles
		g.RACI = []RACIRow{}
		for _, r := range d.RACI {
			g.RACI = append(g.RACI, RACIRow{Activity: r.Activity, Cells: r.Cells})
		}
		if err := normalizeGovernance(&g); err != nil {
			return DraftProposal{}, nil, err
		}
		gov = &g
	}

	applied := DraftProposal{Steps: []DraftStep{}, Quarters: []DraftQuarter{}, RACIRoles: []string{}, RACI: []DraftRACI{},
		Unknowns: d.Unknowns, Notes: d.Notes, References: d.References}
	if gov != nil {
		if _, err := s.ReplaceGovernance(id, *gov, actor); err != nil {
			return DraftProposal{}, nil, err
		}
		applied.RACIRoles, applied.RACI = d.RACIRoles, d.RACI
		notes = append(notes, fmt.Sprintf("RACI: %d activities × %d roles", len(d.RACI), len(d.RACIRoles)))
	}
	if quarters != nil {
		if _, err := s.ReplaceQuarters(id, quarters, actor); err != nil {
			return DraftProposal{}, nil, err
		}
		applied.Quarters = d.Quarters
		notes = append(notes, fmt.Sprintf("quarter dates for %d quarter(s)", len(d.Quarters)))
	}
	if steps != nil {
		if _, err := s.ReplaceSteps(id, steps, actor); err != nil {
			return DraftProposal{}, nil, err
		}
		applied.Steps = d.Steps
		notes = append(notes, fmt.Sprintf("%d step(s)", len(d.Steps)))
	}
	if quarters != nil {
		if _, err := s.GenerateQuarters(id, false, actor); err != nil {
			return DraftProposal{}, nil, err
		}
		notes = append(notes, "quarterly steps regenerated from the new dates")
	}
	cur, err := s.Get(id)
	if err != nil {
		return DraftProposal{}, nil, err
	}
	next := cur
	if apply("objective", d.Objective != "" && d.Objective != cur.Objective) {
		next.Objective = d.Objective
		applied.Objective = d.Objective
		notes = append(notes, "objective")
	}
	if len(notes) == 0 {
		return DraftProposal{}, nil, invalid("nothing in the draft can be applied: %s", firstNonEmpty(strings.Join(d.Notes, "; "), "it is empty"))
	}
	next.AIGenerated, next.Model, next.PromptHash = true, pr.Model, pr.PromptHash
	next.UpdatedAt, next.UpdatedBy = s.stamp(), actor
	if err := normalizeHeader(&next); err != nil {
		return DraftProposal{}, nil, err
	}
	var h []HistoryEntry
	if changed := headerChanges(cur, next); len(changed) > 0 {
		h = append(h, HistoryEntry{At: next.UpdatedAt, Actor: actor, Event: "design", Subject: "header", Note: strings.Join(changed, "; ")})
	}
	if err := s.repo.SaveHeader(next, h, nil); err != nil {
		return DraftProposal{}, nil, mapNotFound(err)
	}
	return applied, notes, nil
}

func refsOf(refs []AIRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Ref)
	}
	return out
}
