package assuranceplan

import (
	"fmt"
	"slices"
	"strings"
)

const (
	LintBlocking = "blocking"
	LintAdvisory = "advisory"
)

// LintFinding is one problem the approval gate found. Blocking findings stop
// an approval; advisory ones are shown to the approver and do not.
type LintFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Subject  string `json:"subject"`
	Message  string `json:"message"`
}

// ApprovalError is an approval refused by the gate, carrying what to fix. The
// handler answers it with 422, as policydocs does.
type ApprovalError struct {
	Findings []LintFinding
}

func (e ApprovalError) Error() string {
	return fmt.Sprintf("the plan has %d blocking issue(s)", len(e.Findings))
}

func BlockingFindings(all []LintFinding) []LintFinding {
	out := []LintFinding{}
	for _, f := range all {
		if f.Severity == LintBlocking {
			out = append(out, f)
		}
	}
	return out
}

// Lint is the approval gate over a whole plan and its population.
func Lint(p Plan, pop []PopulationItem) []LintFinding {
	out := []LintFinding{}
	add := func(sev, code, subject, format string, args ...any) {
		out = append(out, LintFinding{Severity: sev, Code: code, Subject: subject, Message: fmt.Sprintf(format, args...)})
	}

	for _, req := range []struct{ value, code, label string }{
		{p.OwnerRole, "owner_missing", "owner role"},
		{p.AccountableRole, "accountable_missing", "accountable role"},
		{p.ApprovalForum, "forum_missing", "approval forum"},
		{p.PeriodStart, "period_missing", "period start"},
		{p.PeriodEnd, "period_missing", "period end"},
	} {
		if strings.TrimSpace(req.value) == "" {
			add(LintBlocking, req.code, "plan", "The plan has no %s.", req.label)
		}
	}
	if p.PeriodStart != "" && p.PeriodEnd != "" && p.PeriodEnd < p.PeriodStart {
		add(LintBlocking, "period_order", "plan", "The period ends (%s) before it starts (%s).", p.PeriodEnd, p.PeriodStart)
	}

	if len(p.Steps) == 0 {
		add(LintBlocking, "no_steps", "plan", "The plan has no steps.")
	}
	windowStart := firstNonEmpty(p.MobiliseStart, p.PeriodStart)
	windowEnd := maxString(p.ClosingDate, p.PeriodEnd)
	for _, s := range p.Steps {
		subject := "step " + s.StepRef
		if strings.TrimSpace(s.ResponsibleRole) == "" {
			add(LintBlocking, "step_no_responsible", subject, "Step %s (%s) has no responsible role.", s.StepRef, s.Title)
		}
		if s.StepType == StepMilestone {
			if s.StartDate == "" && s.EndDate == "" {
				add(LintBlocking, "step_no_dates", subject, "Milestone %s (%s) has no date.", s.StepRef, s.Title)
			}
		} else if s.StartDate == "" || s.EndDate == "" {
			add(LintBlocking, "step_no_dates", subject, "Step %s (%s) needs a start and an end date.", s.StepRef, s.Title)
		}
		if s.StartDate != "" && s.EndDate != "" && s.EndDate < s.StartDate {
			add(LintBlocking, "step_date_order", subject, "Step %s ends (%s) before it starts (%s).", s.StepRef, s.EndDate, s.StartDate)
		}
		if windowStart != "" && windowEnd != "" {
			if (s.StartDate != "" && s.StartDate < windowStart) || (s.EndDate != "" && s.EndDate > windowEnd) {
				add(LintAdvisory, "step_outside_period", subject, "Step %s (%s to %s) falls outside the plan's span %s to %s.",
					s.StepRef, s.StartDate, s.EndDate, windowStart, windowEnd)
			}
		}
	}

	for _, q := range p.Quarters {
		subject := fmt.Sprintf("Q%d", q.Quarter)
		if q.LOD1Deadline == "" {
			add(LintBlocking, "quarter_no_deadline", subject, "Q%d has no 1LoD deadline.", q.Quarter)
		}
		if q.ReportDate == "" {
			add(LintBlocking, "quarter_no_report", subject, "Q%d has no report date.", q.Quarter)
		}
		order := []struct{ label, value string }{
			{"1LoD window start", q.LOD1WindowStart}, {"1LoD deadline", q.LOD1Deadline},
			{"selection", q.SelectionDate}, {"fieldwork start", q.FieldworkStart}, {"fieldwork end", q.FieldworkEnd},
			{"findings agreed", q.FindingsAgreedBy}, {"report", q.ReportDate},
		}
		prev := struct{ label, value string }{}
		for _, o := range order {
			if o.value == "" {
				continue
			}
			if prev.value != "" && o.value < prev.value {
				add(LintAdvisory, "quarter_date_order", subject, "Q%d: %s (%s) is before %s (%s).", q.Quarter, o.label, o.value, prev.label, prev.value)
			}
			prev = o
		}
	}

	for _, st := range p.Strata {
		subject := "stratum " + st.Code
		if err := ValidateWaves(st.Waves); err != nil {
			add(LintBlocking, "stratum_waves", subject, "Stratum %s: %v.", st.Code, err)
		}
		if st.SamplingEnabled {
			if _, err := StatisticalSize(st.Confidence, st.TolerableRate); err != nil {
				add(LintBlocking, "stratum_sampling", subject, "Stratum %s: %v.", st.Code, err)
			}
			if st.FloorPct < 0 || st.FloorPct > 1 {
				add(LintBlocking, "stratum_sampling", subject, "Stratum %s: the coverage floor %s%% is not between 0%% and 100%%.", st.Code, formatNumber(st.FloorPct*100))
			}
		}
	}
	view := computeSampling(p, pop, nil)
	for _, z := range view.Strata {
		subject := "stratum " + z.Code
		if z.NSource != "population" {
			sized := "so it is sized at zero"
			if z.NSource == "planned" {
				sized = fmt.Sprintf("so it is sized on its planned %d", z.N)
			}
			add(LintAdvisory, "stratum_empty", subject, "Stratum %s has no population items yet, %s.", z.Code, sized)
			continue
		}
		if z.Error == "" && z.WaveCounts != z.ReviewsDue {
			add(LintAdvisory, "wave_counts", subject, "Stratum %s: the waves hold %s items; the wave percentages of %d give %s.",
				z.Code, joinInts(z.WaveCounts), z.N, joinInts(z.ReviewsDue))
		}
		if z.NoWave > 0 {
			add(LintAdvisory, "no_wave", subject, "Stratum %s: %d item(s) have no 1LoD wave, so they are never due for attestation or drawn.", z.Code, z.NoWave)
		}
	}
	if len(p.Strata) > 0 {
		for _, l := range view.Quarters {
			if l.Over {
				add(LintAdvisory, "utilisation", fmt.Sprintf("Q%d", l.Quarter), "Q%d: effort of %s person-days against a capacity of %s is %s%% utilisation, above the %s%% threshold.",
					l.Quarter, formatNumber(l.Effort), formatNumber(l.Capacity), formatNumber(l.Utilisation*100), formatNumber(p.Parameters.UtilisationThreshold*100))
			}
		}
	}

	if len(p.Attributes) == 0 && slices.ContainsFunc(p.Strata, func(st Stratum) bool { return st.SamplingEnabled }) {
		add(LintAdvisory, "no_attributes", "test script", "The plan samples items but has no test attributes, so its tests have nothing to record.")
	}
	for _, a := range p.Attributes {
		subject := "attribute " + a.AttrRef
		if a.Checked && len(a.Unresolved) > 0 {
			add(LintAdvisory, "attribute_refs", subject, "Attribute %s: %s not found in the catalogs; they stay marked unresolved.", a.AttrRef, strings.Join(a.Unresolved, ", "))
		}
		if a.PassCriteria == "" {
			add(LintAdvisory, "attribute_criteria", subject, "Attribute %s has no pass criteria.", a.AttrRef)
		}
	}

	for _, row := range p.RACI {
		accountable := 0
		for _, v := range row.Cells {
			if strings.Contains(v, "A") {
				accountable++
			}
		}
		switch {
		case accountable == 0:
			add(LintAdvisory, "raci_no_accountable", "RACI", "RACI activity %q has no Accountable role.", row.Activity)
		case accountable > 1:
			add(LintAdvisory, "raci_many_accountable", "RACI", "RACI activity %q has %d Accountable roles; one is the norm.", row.Activity, accountable)
		}
	}
	return out
}

func joinInts(v [4]int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, "/")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func maxString(a, b string) string {
	if a > b {
		return a
	}
	return b
}
