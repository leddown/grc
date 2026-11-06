package assuranceplan

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// statusClass maps a status word to its pill class. Every pill prints its
// word: the colour is never the only carrier of the meaning.
func statusClass(word string) string {
	slug := strings.ToLower(strings.NewReplacer(" ", "-", "/", "-").Replace(strings.TrimSpace(word)))
	if slug == "" {
		slug = "none"
	}
	return "ap-st ap-st-" + slug
}

func statusPill(word string) string {
	if word == "" {
		return ""
	}
	return `<span class="` + statusClass(word) + `">` + html.EscapeString(word) + `</span>`
}

func displayDate(s string) string {
	t, ok := parseDate(s)
	if !ok {
		return s
	}
	return t.Format("2 Jan 2006")
}

func displayRange(a, b string) string {
	ta, okA := parseDate(a)
	tb, okB := parseDate(b)
	switch {
	case !okA && !okB:
		return ""
	case !okB || a == b:
		return displayDate(a)
	case !okA:
		return "– " + displayDate(b)
	case ta.Year() == tb.Year():
		return ta.Format("2 Jan") + " – " + tb.Format("2 Jan 2006")
	}
	return ta.Format("2 Jan 2006") + " – " + tb.Format("2 Jan 2006")
}

// ganttHTML draws the plan's steps against months, server-side: a bar for an
// activity, a diamond for a milestone and a grey band for an ongoing item.
// The legend names each mark in words, and each row prints its dates and
// status, so nothing on the chart is carried by shape or colour alone.
func ganttHTML(p Plan, today string) string {
	if len(p.Steps) == 0 {
		return `<p class="muted">No steps yet. Add them below, or regenerate the quarterly steps from the cycle on the Quarterly plan tab.</p>`
	}
	lo, hi := "", ""
	widen := func(d string) {
		if _, ok := parseDate(d); !ok {
			return
		}
		if lo == "" || d < lo {
			lo = d
		}
		if hi == "" || d > hi {
			hi = d
		}
	}
	for _, d := range []string{p.MobiliseStart, p.PeriodStart, p.PeriodEnd, p.ClosingDate} {
		widen(d)
	}
	for _, s := range p.Steps {
		widen(s.StartDate)
		widen(s.EndDate)
	}
	if lo == "" {
		return `<p class="muted">No step has a date yet, so there is nothing to draw.</p>`
	}
	first, _ := parseDate(lo)
	last, _ := parseDate(hi)
	first = time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)
	last = time.Date(last.Year(), last.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	totalDays := last.Sub(first).Hours()/24 + 1
	pos := func(d string, endOfDay bool) float64 {
		t, _ := parseDate(d)
		days := t.Sub(first).Hours() / 24
		if endOfDay {
			days++
		}
		return days / totalDays * 100
	}

	var months strings.Builder
	var grid strings.Builder
	for m := first; !m.After(last); m = m.AddDate(0, 1, 0) {
		next := m.AddDate(0, 1, 0)
		width := next.Sub(m).Hours() / 24 / totalDays * 100
		label := m.Format("Jan")
		if m.Month() == time.January {
			label = m.Format("Jan 06")
		}
		fmt.Fprintf(&months, `<span class="ap-month" style="width:%.3f%%">%s</span>`, width, label)
		if !m.Equal(first) {
			fmt.Fprintf(&grid, `<i class="ap-grid" style="left:%.3f%%"></i>`, pos(formatDate(m), false))
		}
	}
	todayMark := ""
	if today >= formatDate(first) && today <= formatDate(last) {
		todayMark = fmt.Sprintf(`<i class="ap-today" style="left:%.3f%%"></i>`, pos(today, false))
	}

	var b strings.Builder
	b.WriteString(`<div class="ap-gantt-wrap"><table class="ap-gantt"><thead><tr>
<th class="ap-c-ref">Ref</th><th class="ap-c-title">Step</th><th class="ap-c-role">Responsible</th><th class="ap-c-dates">Dates</th><th class="ap-c-status">Status</th>
<th class="ap-c-track"><div class="ap-months">`)
	b.WriteString(months.String())
	b.WriteString(`</div></th></tr></thead><tbody>`)
	phase := "\x00"
	for _, s := range p.Steps {
		if s.Phase != phase {
			phase = s.Phase
			label := phase
			if label == "" {
				label = "No phase"
			}
			b.WriteString(`<tr class="ap-phase-row"><td colspan="6">` + html.EscapeString(label) + `</td></tr>`)
		}
		mark := ""
		title := html.EscapeString(fmt.Sprintf("%s %s: %s (%s)", s.StepRef, s.Title, displayRange(s.StartDate, s.EndDate), s.StepType))
		risk := ""
		if s.Status == StepAtRisk || s.Status == StepDelayed {
			risk = " ap-risk"
		}
		switch {
		case s.StepType == StepMilestone && s.StartDate != "":
			mark = fmt.Sprintf(`<span class="ap-ms%s" style="left:%.3f%%" title="%s">◆</span>`, risk, pos(s.StartDate, false)+0.5/totalDays*100, title)
		case s.StartDate != "" && s.EndDate != "":
			left := pos(s.StartDate, false)
			width := pos(s.EndDate, true) - left
			if width < 0.3 {
				width = 0.3
			}
			cls := "ap-bar"
			if s.StepType == StepOngoing {
				cls = "ap-band"
			}
			if s.Status == StepComplete {
				cls += " ap-done"
			}
			mark = fmt.Sprintf(`<span class="%s%s" style="left:%.3f%%;width:%.3f%%" title="%s"></span>`, cls, risk, left, width, title)
		}
		fmt.Fprintf(&b, `<tr><td class="ap-c-ref">%s</td><td class="ap-c-title">%s</td><td class="ap-c-role">%s</td><td class="ap-c-dates">%s</td><td class="ap-c-status">%s</td><td class="ap-c-track"><div class="ap-lane">%s%s%s</div></td></tr>`,
			html.EscapeString(s.StepRef), html.EscapeString(s.Title), html.EscapeString(s.ResponsibleRole),
			html.EscapeString(displayRange(s.StartDate, s.EndDate)), statusPill(s.Status), grid.String(), todayMark, mark)
	}
	b.WriteString(`</tbody></table></div>`)
	todayNote := ""
	if todayMark != "" {
		todayNote = ` · <i class="ap-key-today"></i> thin vertical line: today (` + html.EscapeString(displayDate(today)) + `)`
	}
	b.WriteString(`<p class="ap-legend">Legend: <span class="ap-key-bar"></span> solid bar: activity · <span class="ap-key-ms">◆</span> diamond: milestone · <span class="ap-key-band"></span> grey band: ongoing · <span class="ap-key-risk"></span> outlined mark: step at risk or delayed · faded bar: complete` + todayNote + `. Each row also prints its dates and status.</p>`)
	return b.String()
}
