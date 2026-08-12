package reporting

import "sort"

// withDerived returns a copy of the report with the summary and charts filled
// in from the results when the caller left them empty — mirroring the risk
// report so callers and fixtures stay terse.
func (r ControlAssessmentReport) withDerived() ControlAssessmentReport {
	out := r

	resultCounts := map[AssessmentResult]int{}
	for _, res := range r.Results {
		resultCounts[res.Result]++
	}

	if out.Summary.TotalControls == 0 {
		out.Summary.TotalControls = len(r.Results)
		out.Summary.Satisfied = resultCounts[ResultSatisfied]
		out.Summary.PartiallySatisfied = resultCounts[ResultPartiallySatisfied]
		out.Summary.NotSatisfied = resultCounts[ResultNotSatisfied]
		out.Summary.NotApplicable = resultCounts[ResultNotApplicable]
	}
	if out.Summary.Deficiencies == 0 {
		out.Summary.Deficiencies = len(r.Deficiencies)
	}

	if out.ResultChart == "" {
		out.ResultChart = DonutChartSVG([]ChartSegment{
			{Label: "Satisfied", Value: float64(resultCounts[ResultSatisfied]), Color: ResultSatisfied.Color()},
			{Label: "Partial", Value: float64(resultCounts[ResultPartiallySatisfied]), Color: ResultPartiallySatisfied.Color()},
			{Label: "Not Satisfied", Value: float64(resultCounts[ResultNotSatisfied]), Color: ResultNotSatisfied.Color()},
			{Label: "N/A", Value: float64(resultCounts[ResultNotApplicable]), Color: ResultNotApplicable.Color()},
		}, 150)
	}

	if out.FamilyChart == "" {
		out.FamilyChart = BarChartSVG(familySegments(r.Results), 240)
	}

	return out
}

// familySegments counts controls per family and returns the busiest families
// (descending, then alphabetical) capped for layout, as bar-chart segments.
func familySegments(results []ControlResult) []ChartSegment {
	const maxFamilies = 8
	counts := map[string]int{}
	for _, res := range results {
		family := res.Family
		if family == "" {
			family = "Unspecified"
		}
		counts[family]++
	}

	families := make([]string, 0, len(counts))
	for family := range counts {
		families = append(families, family)
	}
	sort.Slice(families, func(i, j int) bool {
		if counts[families[i]] != counts[families[j]] {
			return counts[families[i]] > counts[families[j]]
		}
		return families[i] < families[j]
	})
	if len(families) > maxFamilies {
		families = families[:maxFamilies]
	}

	segments := make([]ChartSegment, 0, len(families))
	for _, family := range families {
		segments = append(segments, ChartSegment{
			Label: family,
			Value: float64(counts[family]),
			Color: "#1f6f8b",
		})
	}
	return segments
}
