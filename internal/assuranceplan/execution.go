package assuranceplan

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// The functions in this file are the execution rules, kept pure so they can
// be tested against the thresholds in ASSURANCE_PLANS.md.

// OverallResult grades a test by its fails: nothing tested is Not started,
// then Effective, Partially effective and Ineffective at the plan's
// thresholds.
func OverallResult(fails, tested int, pr Parameters) string {
	switch {
	case tested == 0:
		return OverallNotStarted
	case fails >= pr.ResultIneffectiveMin:
		return OverallIneffective
	case fails >= pr.ResultPartialMin:
		return OverallPartial
	}
	return OverallEffective
}

// Deviation kinds, which the page turns into pill colours.
const (
	KindNotTested    = "not-tested"
	KindEffective    = "effective"
	KindExtend       = "extend"
	KindNotEffective = "not-effective"
)

// Conclude applies the deviation rule to one attribute in one stratum, over
// the year so far: no deviations, deviations that extend the sample, or a
// not-effective conclusion that is a thematic finding.
func Conclude(fails, tested int, pr Parameters) (kind, text string) {
	switch {
	case tested == 0:
		return KindNotTested, ConclusionNotTested
	case fails >= pr.DeviationThematicAt:
		return KindNotEffective, ConclusionNotEffective
	case fails >= pr.DeviationExtendAt && fails > 0:
		word := "deviations"
		if fails == 1 {
			word = "deviation"
		}
		return KindExtend, fmt.Sprintf("%d %s – extend sample", fails, word)
	}
	return KindEffective, ConclusionEffective
}

// ExtendBy is the extra tests the deviation rule suggests for an attribute:
// extend_pct of the next quarter's tests in the stratum, rounded up.
func ExtendBy(nextQuarterTests int, extendPct float64) int {
	if nextQuarterTests <= 0 || extendPct <= 0 {
		return 0
	}
	return int(math.Ceil(guard(float64(nextQuarterTests) * extendPct / 100)))
}

// RAG grades coverage against plan in percentage points: on or above plan is
// Green, within amberPoints below is Amber, further below is Red, and a
// quarter that has not ended is Not due.
func RAG(plannedPct, actualPct, amberPoints float64, due bool) string {
	if !due {
		return "Not due"
	}
	gap := guard(plannedPct - actualPct)
	switch {
	case gap <= 0:
		return "Green"
	case gap <= amberPoints:
		return "Amber"
	}
	return "Red"
}

// QuarterEnd is the last day of the plan's quarter q, counting three-month
// blocks from the period start.
func QuarterEnd(periodStart string, q int) (string, bool) {
	start, ok := parseDate(periodStart)
	if !ok || q < 1 || q > 4 {
		return "", false
	}
	return formatDate(start.AddDate(0, 3*q, -1)), true
}

// EvidenceDue is the requested date plus the SLA in business days.
func EvidenceDue(requested string, slaDays int, cal Calendar) string {
	t, ok := parseDate(requested)
	if !ok {
		return ""
	}
	return formatDate(cal.AddBusinessDays(t, slaDays))
}

// EvidenceLate reports evidence received after its due date, or still
// missing once the due date has passed.
func EvidenceLate(due, received, today string) bool {
	if due == "" {
		return false
	}
	if received != "" {
		return received > due
	}
	return today > due
}

// testRef is T<yy>-Q<q>-<nnn>: the plan's year, the quarter and a number in
// that quarter.
func testRef(periodStart string, quarter, n int) string {
	yy := time.Now().UTC().Year() % 100
	if t, ok := parseDate(periodStart); ok {
		yy = t.Year() % 100
	}
	return fmt.Sprintf("T%02d-Q%d-%03d", yy, quarter, n)
}

// splitList reads a comma, semicolon or newline separated list, trimmed, with
// blanks and repeats dropped.
func splitList(raw string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		part = strings.TrimSpace(part)
		if part != "" && !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}
