package reporting

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"strings"
)

// Charts are rendered server-side as static inline SVG. This is deterministic
// and reproducible: the exact same data always yields byte-identical markup,
// which keeps golden-file tests stable and avoids any client-side render-wait
// timing in the headless browser.
//
// Alternative (not used): a JS charting library (Chart.js, ECharts) drawing to
// <canvas>/<svg> at print time. That would require enabling JavaScript in the
// renderer and polling for a "chart ready" signal before calling printToPDF —
// more fragile and harder to make reproducible — so we generate SVG here.

// ChartSegment is one labelled, colored datum in a chart.
type ChartSegment struct {
	Label string
	Value float64
	Color string
}

// DonutChartSVG renders segments as a donut with a centered total and a legend.
// size is the SVG width/height of the donut in pixels.
func DonutChartSVG(segments []ChartSegment, size float64) template.HTML {
	var total float64
	for _, s := range segments {
		if s.Value > 0 {
			total += s.Value
		}
	}

	cx := size / 2
	cy := size / 2
	stroke := size * 0.22
	r := (size - stroke) / 2
	circumference := 2 * math.Pi * r

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" role="img">`, size, size, size, size)

	if total == 0 {
		fmt.Fprintf(&b, `<circle cx="%.2f" cy="%.2f" r="%.2f" fill="none" stroke="#e0dbcf" stroke-width="%.2f"/>`, cx, cy, r, stroke)
		fmt.Fprintf(&b, `<text x="%.2f" y="%.2f" text-anchor="middle" dominant-baseline="central" font-size="%.1f" fill="#5a6473">n/a</text>`, cx, cy, size*0.12)
		b.WriteString(`</svg>`)
		// #nosec G203 -- markup is fully generated from validated numeric/color values here.
		return template.HTML(b.String())
	}

	// Rotate so the first segment starts at 12 o'clock.
	fmt.Fprintf(&b, `<g transform="rotate(-90 %.2f %.2f)">`, cx, cy)
	var offset float64
	for _, s := range segments {
		if s.Value <= 0 {
			continue
		}
		segLen := (s.Value / total) * circumference
		fmt.Fprintf(&b,
			`<circle cx="%.2f" cy="%.2f" r="%.2f" fill="none" stroke="%s" stroke-width="%.2f" stroke-dasharray="%.3f %.3f" stroke-dashoffset="%.3f"/>`,
			cx, cy, r, safeColor(s.Color), stroke, segLen, circumference-segLen, -offset,
		)
		offset += segLen
	}
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<text x="%.2f" y="%.2f" text-anchor="middle" dominant-baseline="central" font-size="%.1f" font-weight="700" fill="#1f2a44">%.0f</text>`, cx, cy, size*0.2, total)
	b.WriteString(`</svg>`)

	writeLegend(&b, segments)
	// #nosec G203 -- markup is fully generated from validated numeric/color values and escaped labels.
	return template.HTML(b.String())
}

// BarChartSVG renders segments as horizontal bars with labels and values.
func BarChartSVG(segments []ChartSegment, width float64) template.HTML {
	var maxVal float64
	for _, s := range segments {
		if s.Value > maxVal {
			maxVal = s.Value
		}
	}

	const rowH = 22.0
	const labelW = 96.0
	const valueW = 30.0
	barArea := width - labelW - valueW
	height := rowH * float64(len(segments))
	if height == 0 {
		height = rowH
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" role="img">`, width, height, width, height)
	for i, s := range segments {
		y := float64(i) * rowH
		barLen := 0.0
		if maxVal > 0 && s.Value > 0 {
			barLen = (s.Value / maxVal) * barArea
		}
		fmt.Fprintf(&b, `<text x="0" y="%.2f" font-size="9" fill="#1c2431" dominant-baseline="central">%s</text>`, y+rowH/2, escapeText(s.Label))
		fmt.Fprintf(&b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="2" fill="#eee7da"/>`, labelW, y+4, barArea, rowH-8)
		if barLen > 0 {
			fmt.Fprintf(&b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="2" fill="%s"/>`, labelW, y+4, barLen, rowH-8, safeColor(s.Color))
		}
		fmt.Fprintf(&b, `<text x="%.2f" y="%.2f" font-size="9" font-weight="700" fill="#1c2431" dominant-baseline="central">%.0f</text>`, width-valueW+6, y+rowH/2, s.Value)
	}
	b.WriteString(`</svg>`)
	// #nosec G203 -- markup is fully generated from validated numeric/color values and escaped labels.
	return template.HTML(b.String())
}

func writeLegend(b *strings.Builder, segments []ChartSegment) {
	b.WriteString(`<div style="margin-top:8px; font-size:8pt; color:#1c2431;">`)
	for _, s := range segments {
		if s.Value <= 0 {
			continue
		}
		fmt.Fprintf(b,
			`<div style="display:inline-flex; align-items:center; margin:0 8px 2px 0;"><span style="display:inline-block; width:9px; height:9px; border-radius:2px; background:%s; margin-right:4px;"></span>%s (%.0f)</div>`,
			safeColor(s.Color), escapeText(s.Label), s.Value,
		)
	}
	b.WriteString(`</div>`)
}

// safeColor only allows validated hex colors into generated SVG/markup, so a
// bad color value can never inject attributes or markup.
func safeColor(c string) string {
	return sanitizeColor(c, "#5a544c")
}

func escapeText(s string) string {
	return html.EscapeString(s)
}
