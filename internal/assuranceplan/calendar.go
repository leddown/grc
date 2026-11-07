package assuranceplan

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	return t, err == nil
}

func formatDate(t time.Time) string { return t.Format(dateLayout) }

// Calendar knows which days are business days: weekdays that are not in the
// plan's list of non-working dates.
type Calendar struct {
	holidays map[string]bool
}

// parseNonWorkingDates reads a plan's non-working dates, separated by commas,
// semicolons, spaces or new lines. It returns them sorted and de-duplicated.
func parseNonWorkingDates(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\r' || r == '\t'
	})
	seen := map[string]bool{}
	var out, bad []string
	for _, f := range fields {
		t, ok := parseDate(f)
		if !ok {
			bad = append(bad, f)
			continue
		}
		d := formatDate(t)
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("non-working dates must be YYYY-MM-DD: %s", strings.Join(bad, ", "))
	}
	sort.Strings(out)
	return out, nil
}

func NewCalendar(nonWorkingDates string) Calendar {
	dates, _ := parseNonWorkingDates(nonWorkingDates)
	c := Calendar{holidays: map[string]bool{}}
	for _, d := range dates {
		c.holidays[d] = true
	}
	return c
}

func (c Calendar) IsBusinessDay(t time.Time) bool {
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return !c.holidays[formatDate(t)]
}

// NextBusinessDay is the first business day strictly after t.
func (c Calendar) NextBusinessDay(t time.Time) time.Time {
	t = t.AddDate(0, 0, 1)
	for !c.IsBusinessDay(t) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func (c Calendar) previousBusinessDay(t time.Time) time.Time {
	t = t.AddDate(0, 0, -1)
	for !c.IsBusinessDay(t) {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

// AddBusinessDays counts n business days from t: forward for n > 0, backward
// for n < 0. t itself is never counted.
func (c Calendar) AddBusinessDays(t time.Time, n int) time.Time {
	for ; n > 0; n-- {
		t = c.NextBusinessDay(t)
	}
	for ; n < 0; n++ {
		t = c.previousBusinessDay(t)
	}
	return t
}

// Roll keeps a business day and moves anything else to the next business day.
func (c Calendar) Roll(t time.Time) time.Time {
	if c.IsBusinessDay(t) {
		return t
	}
	return c.NextBusinessDay(t)
}

// RollBack keeps a business day and moves anything else to the previous one,
// for a date that must not slip past a boundary such as the period end.
func (c Calendar) RollBack(t time.Time) time.Time {
	if c.IsBusinessDay(t) {
		return t
	}
	return c.previousBusinessDay(t)
}

func (c Calendar) rollString(s string) string {
	t, ok := parseDate(s)
	if !ok {
		return s
	}
	return formatDate(c.Roll(t))
}
