package assuranceplan

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"grc/internal/knowledge"
)

// KnowledgePlans flattens every plan into one knowledge record, written to
// answer what the control function is asked: what is due this quarter, how
// coverage and testing stand against plan, and whether the year is behind.
func (s *Service) KnowledgePlans() ([]knowledge.Item, error) {
	out := []knowledge.Item{}
	err := s.eachExport(func(id int64, e PlanExport) {
		out = append(out, planItem(id, e, s.today()))
	})
	return out, err
}

// KnowledgeTests flattens every test of every plan, with its result per
// attribute, so "which Tier 1 assets failed NFR-04?" is a search.
func (s *Service) KnowledgeTests() ([]knowledge.Item, error) {
	out := []knowledge.Item{}
	err := s.eachExport(func(id int64, e PlanExport) {
		out = append(out, testItems(id, e)...)
	})
	return out, err
}

func (s *Service) eachExport(fn func(int64, PlanExport)) error {
	plans, err := s.List(Filter{})
	if err != nil {
		return fmt.Errorf("list assurance plans: %w", err)
	}
	for _, p := range plans {
		e, err := s.Export(p.ID)
		if err != nil {
			return fmt.Errorf("read assurance plan %s: %w", p.Reference, err)
		}
		fn(p.ID, e)
	}
	return nil
}

// currentQuarter is the plan quarter today falls in: 0 before the period, 5
// after it.
func currentQuarter(e PlanExport, today string) int {
	if today < e.PeriodStart {
		return 0
	}
	for _, q := range e.Quarters {
		if today <= q.QuarterEnd {
			return q.Quarter
		}
	}
	return 5
}

func planItem(id int64, e PlanExport, today string) knowledge.Item {
	cur := currentQuarter(e, today)
	stage := map[int]string{0: "mobilisation, before the period starts", 5: "after the period ends"}[cur]
	if stage == "" {
		stage = "Q" + strconv.Itoa(cur) + " of the period"
	}
	fields := map[string]string{
		"status": e.Status, "plan_type": e.PlanTypeName, "period_start": e.PeriodStart, "period_end": e.PeriodEnd,
		"as_of": today, "current_quarter": stage, "approval_forum": e.ApprovalForum, "owner": e.OwnerRole,
		"version": e.LatestVersion, "population": strconv.Itoa(e.Totals.PopulationSize),
		"tests_planned": strconv.Itoa(e.Totals.PlannedTests), "tests_opened": strconv.Itoa(e.Totals.TestsOpened),
		"tests_signed_off": strconv.Itoa(e.Totals.TestsSignedOff), "attribute_fails": strconv.Itoa(e.Totals.AttributeFails),
		"not_effective": strconv.Itoa(e.Totals.NotEffective), "findings_open": strconv.Itoa(e.Totals.FindingsOpen),
		"findings_overdue": strconv.Itoa(e.Totals.FindingsOverdue),
	}
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("%s %s (%s), %s, %s. Status %s%s. As of %s: %s.", e.Reference, e.Title, e.PlanTypeName,
		firstNonEmpty(e.FunctionName, "control function"), displayRange(e.PeriodStart, e.PeriodEnd), e.Status, versionNote(e.LatestVersion), today, stage)
	if e.Objective != "" {
		line("Objective: %s", e.Objective)
	}
	line("Owner %s; accountable %s; approved by the %s.", e.OwnerRole, e.AccountableRole, firstNonEmpty(e.ApprovalForum, "approval forum"))

	line("\nQuarterly cycle:")
	for _, q := range e.Quarters {
		marker := ""
		if q.Quarter == cur {
			marker = " (current quarter)"
		}
		line("Q%d%s: 1LoD wave of %d item(s) due %s; sample of %d test(s) drawn %s; fieldwork %s; findings agreed by %s; report %s; committee %s.",
			q.Quarter, marker, q.ReviewsDue, q.LOD1Deadline, q.PlannedTests, q.SelectionDate, displayRange(q.FieldworkStart, q.FieldworkEnd),
			q.FindingsAgreedBy, q.ReportDate, firstNonEmpty(q.CommitteeLabel, "—"))
	}

	var behind []string
	line("\nCoverage against plan (attested by each quarter end, cumulative):")
	for _, c := range e.Coverage {
		cells := []string{}
		for i, cell := range c.Cells {
			cells = append(cells, fmt.Sprintf("Q%d %s%% of %s%% %s", i+1, formatNumber(cell.ActualPct), formatNumber(cell.PlannedPct), cell.RAG))
			if cell.RAG == "Amber" || cell.RAG == "Red" {
				behind = append(behind, fmt.Sprintf("%s coverage %s at Q%d end (%s%% against %s%%)", c.Name, cell.RAG, i+1, formatNumber(cell.ActualPct), formatNumber(cell.PlannedPct)))
			}
		}
		line("%s (%s, %d items): %s. Attested to date %d (%s%%), data quality accepted %d.", c.Name, c.Code, c.N, strings.Join(cells, "; "),
			c.Attested, formatNumber(c.AttestedPct), c.DQAccepted)
		fields["coverage_"+strings.ToLower(c.Code)+"_pct"] = formatNumber(c.AttestedPct)
	}

	line("\nTesting by quarter:")
	for _, t := range e.Testing {
		line("Q%d: %d planned, %d drawn, %d opened, %d with fieldwork complete, %d signed off by QA; %d effective, %d partially effective, %d ineffective.",
			t.Quarter, t.Planned, t.Selected, t.Opened, t.Fieldwork, t.QA, t.ByResult[OverallEffective], t.ByResult[OverallPartial], t.ByResult[OverallIneffective])
		if i := t.Quarter - 1; i >= 0 && i < len(e.Quarters) && e.Quarters[i].FieldworkEnd != "" && e.Quarters[i].FieldworkEnd < today {
			if t.Opened < t.Planned {
				behind = append(behind, fmt.Sprintf("Q%d testing: %d of %d planned tests opened after fieldwork closed", t.Quarter, t.Opened, t.Planned))
			}
			if t.QA < t.Opened {
				behind = append(behind, fmt.Sprintf("Q%d: %d test(s) not yet signed off by QA", t.Quarter, t.Opened-t.QA))
			}
		}
	}

	var overdue, dueSoon []string
	soon := today
	if t, ok := parseDate(today); ok {
		soon = formatDate(t.AddDate(0, 0, 30))
	}
	for _, st := range e.Steps {
		if st.Status == StepComplete || st.EndDate == "" {
			continue
		}
		label := fmt.Sprintf("%s %s (%s, %s, due %s)", st.StepRef, st.Title, st.Status, firstNonEmpty(st.ResponsibleRole, "—"), st.EndDate)
		switch {
		case st.EndDate < today:
			overdue = append(overdue, label)
		case firstNonEmpty(st.StartDate, st.EndDate) <= soon:
			dueSoon = append(dueSoon, label)
		}
	}
	if len(overdue) > 0 {
		behind = append(behind, fmt.Sprintf("%d step(s) past their end date and not complete", len(overdue)))
		line("\nSteps past their end date and not complete:\n%s", bullets(overdue))
	}
	line("\nSteps under way or starting in the next 30 days:\n%s", firstNonEmpty(bullets(dueSoon), "- none"))

	devs := []string{}
	for _, d := range e.Deviations {
		if d.Deviations == 0 {
			continue
		}
		next := d.Conclusion
		if d.RiskRef != "" {
			next += ", risk " + d.RiskRef
		}
		devs = append(devs, fmt.Sprintf("%s (%s) in %s: %d deviation(s) in %d test(s), %s; failed on %s", d.AttrRef, d.Domain, d.Stratum, d.Deviations, d.Tested,
			next, strings.Join(d.Failures, ", ")))
	}
	line("\nDeviations (cumulative, per attribute and stratum):\n%s", firstNonEmpty(bullets(devs), "- none"))
	if e.Totals.FindingsOverdue > 0 {
		behind = append(behind, fmt.Sprintf("%d finding(s) overdue", e.Totals.FindingsOverdue))
	}

	related := []string{}
	findings := []string{}
	for _, f := range e.Findings {
		state := f.Status
		if f.Overdue {
			state += ", overdue"
		}
		if f.Missing {
			state = "deleted in Audit Findings"
		}
		findings = append(findings, fmt.Sprintf("%s %s (%s, %s, due %s), from %s / %s", f.Reference, f.Title, f.Severity, state, firstNonEmpty(f.DueDate, "—"), f.TestRef, f.AttrRef))
		related = append(related, f.Reference)
	}
	line("\nFindings raised from the plan:\n%s", firstNonEmpty(bullets(findings), "- none"))
	for _, d := range e.Deviations {
		if d.RiskRef != "" {
			related = append(related, d.RiskRef)
		}
	}

	status := "On plan."
	fields["behind_plan"] = "no"
	switch {
	case cur == 0:
		status = "Not started: the period has not begun."
		fields["behind_plan"] = "not started"
	case len(behind) > 0:
		status = "Behind plan: " + strings.Join(behind, "; ") + "."
		fields["behind_plan"] = "yes"
	}
	body := status + "\n\n" + b.String()

	summary := fmt.Sprintf("%s, %s, %s. %d of %d planned tests opened, %d attribute fail(s), %d finding(s) open. %s",
		e.PlanTypeName, displayRange(e.PeriodStart, e.PeriodEnd), e.Status, e.Totals.TestsOpened, e.Totals.PlannedTests,
		e.Totals.AttributeFails, e.Totals.FindingsOpen, status)
	return knowledge.Item{Kind: knowledge.KindAssurancePlan, Ref: e.Reference, Title: e.Title, Summary: clip(summary, 400), Body: body,
		Group: e.Status, Related: related, Fields: fields, URL: "/assurance-plans/" + strconv.FormatInt(id, 10)}
}

func testItems(id int64, e PlanExport) []knowledge.Item {
	names := map[string]string{}
	for _, st := range e.Strata {
		names[st.Code] = st.Name
	}
	items := map[string]ExportItem{}
	for _, it := range e.Population {
		items[it.ItemRef] = it
	}
	domains := map[string]string{}
	for _, a := range e.Attributes {
		domains[a.AttrRef] = a.Domain
	}
	out := []knowledge.Item{}
	for _, t := range e.Tests {
		stratum := firstNonEmpty(names[t.Stratum], t.Stratum)
		it := items[t.ItemRef]
		failed, findings, lines := []string{}, []string{}, []string{}
		for _, r := range t.Results {
			l := fmt.Sprintf("%s %s: %s", r.AttrRef, domains[r.AttrRef], firstNonEmpty(r.Result, "Pending"))
			if r.Note != "" {
				l += ". " + strings.Join(strings.Fields(r.Note), " ")
			}
			if r.FindingRef != "" {
				l += fmt.Sprintf(" Finding %s (%s).", r.FindingRef, r.Severity)
				findings = append(findings, r.FindingRef)
			}
			if r.Result == ResultFail {
				failed = append(failed, r.AttrRef)
			}
			lines = append(lines, "- "+l)
		}
		qa := "awaiting QA"
		if t.QADate != "" {
			qa = "QA signed off " + t.QADate + " by " + firstNonEmpty(t.QAReviewer, "—")
		}
		failedText := "no fails"
		if len(failed) > 0 {
			failedText = "failed " + strings.Join(failed, ", ")
		}
		summary := fmt.Sprintf("%s Q%d, %s, %s %s: %s, %s, %s.", e.Reference, t.Quarter, stratum, t.ItemRef, t.ItemName, firstNonEmpty(t.Overall, "Not started"), failedText, qa)
		body := fmt.Sprintf("Test %s of assurance plan %s (%s), Q%d. Item %s %s, stratum %s (%s)%s. Selected as %s. Tester %s; evidence requested %s, due %s, received %s; fieldwork completed %s; %s.\nResult: %s, %d pass, %d fail, %d not applicable, %d pending.\n%s\nAttribute results:\n%s",
			t.TestRef, e.Reference, e.Title, t.Quarter, t.ItemRef, t.ItemName, stratum, t.Stratum, itemNote(it), firstNonEmpty(t.Basis, "—"),
			firstNonEmpty(t.Tester, "—"), firstNonEmpty(t.EvidenceRequestedOn, "—"), firstNonEmpty(t.EvidenceDueOn, "—"), firstNonEmpty(t.EvidenceReceivedOn, "—"),
			firstNonEmpty(t.FieldworkCompletedOn, "—"), qa, firstNonEmpty(t.Overall, "Not started"), t.Passes, t.Fails, t.NotApplicable, t.Pending,
			firstNonEmpty(t.EvidenceSummary, ""), strings.Join(lines, "\n"))
		late := "no"
		if t.EvidenceLate {
			late = "yes"
		}
		out = append(out, knowledge.Item{
			Kind: knowledge.KindAssuranceTest, Ref: e.Reference + "/" + t.TestRef, Title: t.TestRef + " " + t.ItemRef + " " + t.ItemName,
			Summary: clip(summary, 400), Body: body, Group: fmt.Sprintf("%s Q%d %s", e.Reference, t.Quarter, stratum),
			Related: slices.Concat(failed, findings),
			Fields: map[string]string{"plan": e.Reference, "quarter": strconv.Itoa(t.Quarter), "stratum": t.Stratum, "stratum_name": stratum,
				"item_ref": t.ItemRef, "item_name": t.ItemName, "cif": it.CIF, "business_line": it.BusinessLine, "overall": t.Overall,
				"fails": strconv.Itoa(t.Fails), "failed_attributes": strings.Join(failed, ", "), "findings": strings.Join(findings, ", "),
				"qa_date": t.QADate, "evidence_late": late},
			URL: "/assurance-plans/" + strconv.FormatInt(id, 10) + "#tests",
		})
	}
	return out
}

func itemNote(it ExportItem) string {
	parts := []string{}
	if it.CIF != "" {
		parts = append(parts, "supports "+it.CIF)
	}
	if it.BusinessLine != "" {
		parts = append(parts, it.BusinessLine)
	}
	if it.Owner != "" {
		parts = append(parts, "owner "+it.Owner)
	}
	if len(parts) == 0 {
		return ""
	}
	return "; " + strings.Join(parts, ", ")
}

func versionNote(v string) string {
	if v == "" {
		return ""
	}
	return ", approved version " + v
}

func bullets(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "- " + strings.Join(items, "\n- ")
}
