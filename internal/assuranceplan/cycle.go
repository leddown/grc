package assuranceplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultQuarters builds the default quarterly operating cycle for a plan
// period, offset from each quarter's 1LoD deadline (week 0):
//
//	the window opens on the quarter's first business day
//	the wave is due on the last Friday of the quarter's second month
//	the sample is selected the next business day
//	fieldwork starts a week after selection and is complete at +6 weeks
//	findings are agreed at +8 weeks and the quarterly report goes at +9 weeks
//
// For a calendar-year plan that reproduces the hand-built 2027 cycle for Q1 to
// Q3 exactly; Q4 runs into January and is usually compressed by hand.
func DefaultQuarters(periodStart time.Time, cal Calendar) []Quarter {
	out := make([]Quarter, 0, 4)
	for i := 0; i < 4; i++ {
		qStart := time.Date(periodStart.Year(), periodStart.Month()+time.Month(3*i), 1, 0, 0, 0, 0, time.UTC)
		deadline := cal.RollBack(lastFriday(qStart.Year(), qStart.Month()+1))
		selection := cal.NextBusinessDay(deadline)
		report := cal.Roll(deadline.AddDate(0, 0, 63))
		committee := time.Date(report.Year(), report.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		out = append(out, Quarter{
			Quarter:          i + 1,
			LOD1WindowStart:  formatDate(cal.Roll(qStart)),
			LOD1Deadline:     formatDate(deadline),
			SelectionDate:    formatDate(selection),
			FieldworkStart:   formatDate(cal.Roll(selection.AddDate(0, 0, 7))),
			FieldworkEnd:     formatDate(cal.Roll(deadline.AddDate(0, 0, 42))),
			FindingsAgreedBy: formatDate(cal.Roll(deadline.AddDate(0, 0, 56))),
			ReportDate:       formatDate(report),
			CommitteeLabel:   committee.Format("Jan 2006") + " (date TBC)",
		})
	}
	return out
}

func lastFriday(year int, month time.Month) time.Time {
	t := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	for t.Weekday() != time.Friday {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

// A date expression in a plan-type template names an anchor and an optional
// offset: "period_start-53d", "lod1_deadline+5bd", "q3.lod1_deadline",
// "closing_date-18bd". A literal YYYY-MM-DD is taken as it is.
var dateExpr = regexp.MustCompile(`^(?:q([1-4])\.)?([a-z0-9_]+)(?:([+-])(\d+)(bd|d|w))?$`)

type dateContext struct {
	cal      Calendar
	anchors  map[string]string
	quarter  *Quarter
	quarters map[int]Quarter
}

func quarterField(q Quarter, name string) (string, bool) {
	switch name {
	case "lod1_window_start":
		return q.LOD1WindowStart, true
	case "lod1_deadline":
		return q.LOD1Deadline, true
	case "selection_date":
		return q.SelectionDate, true
	case "fieldwork_start":
		return q.FieldworkStart, true
	case "fieldwork_end":
		return q.FieldworkEnd, true
	case "findings_agreed_by":
		return q.FindingsAgreedBy, true
	case "report_date":
		return q.ReportDate, true
	}
	return "", false
}

func (ctx dateContext) eval(expr string) (string, error) {
	expr = strings.ReplaceAll(strings.TrimSpace(expr), " ", "")
	if expr == "" {
		return "", nil
	}
	if t, ok := parseDate(expr); ok {
		return formatDate(t), nil
	}
	m := dateExpr.FindStringSubmatch(expr)
	if m == nil {
		return "", fmt.Errorf("date expression %q is not anchor[+|-]N(bd|d|w)", expr)
	}
	anchor := m[2]
	var base string
	var ok bool
	switch {
	case m[1] != "":
		n, _ := strconv.Atoi(m[1])
		q, found := ctx.quarters[n]
		if !found {
			return "", fmt.Errorf("date expression %q names Q%d, which the plan does not have", expr, n)
		}
		base, ok = quarterField(q, anchor)
	case ctx.quarter != nil:
		base, ok = quarterField(*ctx.quarter, anchor)
		if !ok {
			base, ok = ctx.anchors[anchor]
		}
	default:
		base, ok = ctx.anchors[anchor]
	}
	if !ok {
		return "", fmt.Errorf("date expression %q names an unknown anchor %q", expr, anchor)
	}
	t, valid := parseDate(base)
	if !valid {
		return "", nil
	}
	n := 0
	if m[4] != "" {
		n, _ = strconv.Atoi(m[4])
		if m[3] == "-" {
			n = -n
		}
	}
	switch m[5] {
	case "bd":
		if n == 0 {
			return formatDate(ctx.cal.Roll(t)), nil
		}
		return formatDate(ctx.cal.AddBusinessDays(t, n)), nil
	case "w":
		t = t.AddDate(0, 0, 7*n)
	default:
		t = t.AddDate(0, 0, n)
	}
	// A date at or before the period end must not roll into the next period.
	if anchor == "period_end" && n <= 0 {
		return formatDate(ctx.cal.RollBack(t)), nil
	}
	return formatDate(ctx.cal.Roll(t)), nil
}
