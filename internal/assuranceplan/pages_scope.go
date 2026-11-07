package assuranceplan

import (
	"fmt"
	"strconv"
	"strings"
)

func pct(v float64) string { return formatNumber(v*100) + "%" }

func quarterCells(v [4]int) string {
	var b strings.Builder
	total := 0
	for _, n := range v {
		fmt.Fprintf(&b, `<td class="num">%d</td>`, n)
		total += n
	}
	fmt.Fprintf(&b, `<td class="num">%d</td>`, total)
	return b.String()
}

func utilisationWord(l QuarterLoad) string {
	if l.Over {
		return statusPill("Over threshold")
	}
	if l.Effort == 0 {
		return `<span class="muted">—</span>`
	}
	return statusPill("Within capacity")
}

// quarterTargetsHTML is the quarterly plan's sampling side: what each quarter
// tests and reviews, and what that costs against capacity.
func quarterTargetsHTML(v SamplingView) string {
	if len(v.Strata) == 0 {
		return `<h2>Quarterly targets</h2><p class="muted">No strata, so no sampling targets: define them on the Sampling tab.</p>`
	}
	var b strings.Builder
	b.WriteString(`<h2>Quarterly targets</h2><div class="table-wrap"><table><thead><tr><th>Q</th><th class="num">Statistical sample</th><th class="num">Top-ups planned</th><th class="num">Tests</th><th class="num">1LoD reviews due</th><th class="num">Effort (person-days)</th><th class="num">Capacity</th><th class="num">Utilisation</th><th>Against threshold</th><th>Selection</th></tr></thead><tbody>`)
	for q := range 4 {
		stat, top, reviews := 0, 0, 0
		for _, z := range v.Strata {
			stat += z.Statistical[q]
			top += z.TopUps[q]
			reviews += z.ReviewsDue[q]
		}
		l := v.Quarters[q]
		sel := v.Selection[q]
		state := statusPill("Not selected")
		if sel.SelectedAt != "" {
			state = statusPill("Selected") + ` <span class="muted">` + strconv.Itoa(sel.Picks) + ` picks</span>`
		} else if !sel.Exists {
			state = `<span class="muted">no Q` + strconv.Itoa(q+1) + ` row</span>`
		}
		fmt.Fprintf(&b, `<tr><td>Q%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%s</td><td class="num">%s</td><td class="num">%s</td><td>%s</td><td>%s</td></tr>`,
			q+1, stat, top, stat+top, reviews, formatNumber(l.Effort), formatNumber(l.Capacity), pct(l.Utilisation), utilisationWord(l), state)
	}
	fmt.Fprintf(&b, `</tbody></table></div><p class="ap-legend">Year: %d tests; %s of %s person-days (%s utilisation). Figures come from the strata on the Sampling tab.</p>`,
		v.TotalTests, formatNumber(v.TotalEffort), formatNumber(v.TotalCapacity), pct(v.Utilisation))
	return b.String()
}

func samplingHTML(d planPageData) string {
	p, v := d.Plan, d.Sampling
	var b strings.Builder
	b.WriteString(`<h2>Sample sizes</h2>
<p class="lede">Attribute sampling with zero expected deviations: n0 = ⌈ln(1 − confidence) ÷ ln(1 − tolerable rate)⌉; finite-population
correction n = ⌈n0 ÷ (1 + (n0 − 1) ÷ N)⌉; floor = ⌈floor % × N⌉; annual statistical sample = min(N, max(n, floor)).
Risk-based top-ups are tested on top of it. A stratum with sampling off is covered in full.</p>`)
	if len(v.Strata) == 0 {
		b.WriteString(`<p class="muted">No strata yet.</p>`)
	} else {
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Stratum</th><th class="num">N</th><th>N from</th><th class="num">Confidence</th><th class="num">Tolerable rate</th><th class="num">n0</th><th class="num">FPC n</th><th class="num">Floor</th><th class="num">Annual statistical</th><th class="num">Top-ups</th><th class="num">Total tests</th><th class="num">Coverage</th></tr></thead><tbody>`)
		for _, z := range v.Strata {
			source := map[string]string{"population": "population", "planned": "planned size", "none": "nothing yet"}[z.NSource]
			if !z.Sampling {
				fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td><td>%s</td><td colspan="5">%s</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%s</td></tr>`,
					esc(z.Name), z.N, esc(source), statusPill(BasisFull), z.Size.Annual, sum4(z.TopUps), z.TotalTests, pct(z.Coverage))
				continue
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td><td>%s</td><td class="num">%s</td><td class="num">%s</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num"><strong>%d</strong></td><td class="num">%d</td><td class="num">%d</td><td class="num">%s</td></tr>`,
				esc(z.Name), z.N, esc(source), pct(z.Confidence), pct(z.TolerableRate), z.Size.N0, z.Size.FPC, z.Size.Floor, z.Size.Annual,
				sum4(z.TopUps), z.TotalTests, pct(z.Coverage))
			if z.Error != "" {
				b.WriteString(`<tr><td colspan="12">` + statusPill("Blocking") + ` ` + esc(z.Error) + `</td></tr>`)
			}
		}
		fmt.Fprintf(&b, `</tbody></table></div><p class="ap-legend">%d tests in the year. Coverage is total tests over N.`, v.TotalTests)
		for _, z := range v.Strata {
			if z.CycleYears > 1 {
				fmt.Fprintf(&b, ` %s runs a %d-year cycle: N counts the items due in the period; %d more are inside their cycle and are not drawn.`, esc(z.Name), z.CycleYears, z.InCycle)
			}
		}
		b.WriteString(`</p>`)

		b.WriteString(`<h2>Quarterly allocation</h2>
<p class="lede">Cumulative allocation, so the quarters always add up to the year: q<sub>i</sub> = ⌈annual × cum<sub>i</sub>⌉ − ⌈annual × cum<sub>i−1</sub>⌉,
with cum the running total of the wave percentages. 1LoD reviews due use the same scheme over N, rounded rather than rounded up.</p>
<div class="table-wrap"><table><thead><tr><th>Stratum</th><th>Measure</th><th class="num">Q1</th><th class="num">Q2</th><th class="num">Q3</th><th class="num">Q4</th><th class="num">Year</th></tr></thead><tbody>`)
		for i, z := range v.Strata {
			st := p.Strata[i]
			waves := ""
			for _, w := range st.Waves {
				waves += `<td class="num">` + pct(w) + `</td>`
			}
			targets := ""
			for _, c := range z.CumTarget {
				targets += `<td class="num">` + pct(c) + `</td>`
			}
			rows := []struct{ label, cells string }{
				{"Wave (share due)", waves + `<td class="num">` + pct(st.Waves[0]+st.Waves[1]+st.Waves[2]+st.Waves[3]) + `</td>`},
				{"1LoD reviews due", quarterCells(z.ReviewsDue)},
				{"Cumulative coverage target", targets + `<td></td>`},
				{"Items in the wave (population)", quarterCells(z.WaveCounts)},
				{"Statistical sample", quarterCells(z.Statistical)},
				{"Top-ups planned", quarterCells(z.TopUps)},
				{"Tests", quarterCells(z.Tests)},
				{"Items flagged as top-ups", quarterCells(z.Flagged)},
				{"Selected", quarterCells(z.Selected)},
			}
			for j, r := range rows {
				name := ""
				if j == 0 {
					name = esc(z.Name)
				}
				b.WriteString(`<tr><td>` + name + `</td><td>` + esc(r.label) + `</td>` + r.cells + `</tr>`)
			}
		}
		b.WriteString(`</tbody></table></div>`)

		fmt.Fprintf(&b, `<h2>Capacity</h2><p class="lede">Effort per quarter = tests × effort per test + reviews due × effort per review + retest allowance + overhead,
against %s tester FTE × %s productive days a quarter. Above %s utilisation a quarter is flagged.</p>`,
			formatNumber(p.Parameters.TesterFTE), formatNumber(p.Parameters.DaysPerFTEQuarter), pct(p.Parameters.UtilisationThreshold))
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Person-days</th><th class="num">Q1</th><th class="num">Q2</th><th class="num">Q3</th><th class="num">Q4</th><th class="num">Year</th></tr></thead><tbody>`)
		line := func(label string, f func(QuarterLoad) float64, total float64) {
			b.WriteString(`<tr><td>` + esc(label) + `</td>`)
			for _, l := range v.Quarters {
				b.WriteString(`<td class="num">` + formatNumber(f(l)) + `</td>`)
			}
			b.WriteString(`<td class="num">` + formatNumber(total) + `</td></tr>`)
		}
		var tests, reviews, retest, overhead float64
		for _, l := range v.Quarters {
			tests += l.TestEffort
			reviews += l.ReviewEffort
			retest += l.RetestDays
			overhead += l.OverheadDays
		}
		line("Testing", func(l QuarterLoad) float64 { return l.TestEffort }, tests)
		line("Data-quality reviews", func(l QuarterLoad) float64 { return l.ReviewEffort }, reviews)
		line("Retest allowance", func(l QuarterLoad) float64 { return l.RetestDays }, retest)
		line("Overhead", func(l QuarterLoad) float64 { return l.OverheadDays }, overhead)
		line("Total effort", func(l QuarterLoad) float64 { return l.Effort }, v.TotalEffort)
		line("Capacity", func(l QuarterLoad) float64 { return l.Capacity }, v.TotalCapacity)
		b.WriteString(`<tr><td>Utilisation</td>`)
		for _, l := range v.Quarters {
			b.WriteString(`<td class="num">` + pct(l.Utilisation) + ` ` + utilisationWord(l) + `</td>`)
		}
		b.WriteString(`<td class="num">` + pct(v.Utilisation) + `</td></tr></tbody></table></div>`)
	}

	b.WriteString(strataHTML(p))
	b.WriteString(selectionHTML(d))
	return b.String()
}

func sum4(v [4]int) int { return v[0] + v[1] + v[2] + v[3] }

func strataHTML(p Plan) string {
	var b strings.Builder
	b.WriteString(`<h2>Strata and parameters</h2>`)
	b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Code</th><th>Name</th><th>Definition</th><th>Sampling</th><th class="num">Confidence</th><th class="num">Tolerable</th><th class="num">Floor</th><th class="num">Effort / test</th><th class="num">Effort / review</th><th class="num">Waves Q1–Q4</th><th class="num">Top-ups Q1–Q4</th><th class="num">Planned size</th></tr></thead><tbody>`)
	for _, st := range p.Strata {
		sampling := "On"
		if !st.SamplingEnabled {
			sampling = "Off (full coverage)"
		}
		waves := make([]string, 4)
		tops := make([]string, 4)
		for i := range 4 {
			waves[i] = formatNumber(st.Waves[i] * 100)
			tops[i] = strconv.Itoa(st.TopUps[i])
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td class="num">%s</td><td class="num">%s</td><td class="num">%s</td><td class="num">%s</td><td class="num">%s</td><td class="num">%s%%</td><td class="num">%s</td><td class="num">%d</td></tr>`,
			esc(st.Code), esc(st.Name), esc(st.Definition), sampling, pct(st.Confidence), pct(st.TolerableRate), pct(st.FloorPct),
			formatNumber(st.EffortPerTest), formatNumber(st.EffortPerReview), strings.Join(waves, " / "), strings.Join(tops, " / "), st.PlannedSize)
	}
	if len(p.Strata) == 0 {
		b.WriteString(`<tr><td colspan="12" class="muted">No strata. A plan with no strata has no sampling and no scope population.</td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	if !p.DesignLocked {
		b.WriteString(`<h3>Edit strata</h3><p class="lede">Percentages are entered as percent (95 for 95%). The waves of each stratum must add up to 100.
The planned size sizes a stratum until its population is loaded.</p><div id="strataEditor"></div>
<div class="toolbar"><button class="primary" type="button" id="saveStrata">Save strata</button></div>`)
	} else {
		b.WriteString(lockNotice(p))
	}
	return b.String()
}

func selectionHTML(d planPageData) string {
	p, v := d.Plan, d.Sampling
	var b strings.Builder
	b.WriteString(`<h2>Freeze and selection</h2>`)
	canFreeze := (p.Status == StatusApproved || p.Status == StatusActive) && p.RevisionStatus == ""
	switch {
	case !v.Frozen:
		b.WriteString(`<p class="notice">The population is not frozen. Once the plan is approved, freezing records the population's fingerprint and draws
the seed that ranks every quarter's selection (step 0.9 of the reference plan). After the freeze, adding, removing or moving an item between
strata or waves needs a change request.</p>`)
		if canFreeze {
			b.WriteString(`<div class="toolbar"><button type="button" class="primary" id="freezePop">Freeze population and draw the seed</button></div>`)
		}
	default:
		b.WriteString(`<dl class="kv"><dt>Frozen</dt><dd>` + esc(firstNonEmpty(v.FrozenAt, "—")) + ` by ` + esc(firstNonEmpty(v.FrozenBy, "—")) + `</dd>`)
		b.WriteString(`<dt>Seed commitment</dt><dd><code>` + esc(v.SeedCommitment) + `</code> <span class="muted">(SHA-256 of the seed)</span></dd>`)
		if v.SeedRevealed {
			b.WriteString(`<dt>Seed</dt><dd><code>` + esc(v.Seed) + `</code></dd>`)
		} else {
			b.WriteString(`<dt>Seed</dt><dd><span class="muted">Shown here once the last quarter is selected, so a later wave cannot be predicted from it.
Administrators can read it now, to give to Internal Audit.</span> <button type="button" class="mini" id="showSeed">Show seed</button> <code id="seedOut"></code></dd>`)
		}
		b.WriteString(`</dl>`)
	}

	b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Q</th><th>Selection date</th><th>State</th><th class="num">Picks</th><th>Selected</th><th></th></tr></thead><tbody>`)
	for _, qs := range v.Selection {
		state, when, action := statusPill("Not selected"), "", ""
		switch {
		case !qs.Exists:
			state = `<span class="muted">no Q` + strconv.Itoa(qs.Quarter) + ` in the quarterly plan</span>`
		case qs.SelectedAt != "":
			state = statusPill("Selected")
			when = esc(qs.SelectedAt) + " by " + esc(firstNonEmpty(qs.SelectedBy, "—"))
		case v.Frozen && canFreeze:
			action = fmt.Sprintf(`<button type="button" class="primary mini" data-select="%d">Select Q%d sample</button>`, qs.Quarter, qs.Quarter)
		}
		fmt.Fprintf(&b, `<tr><td>Q%d</td><td>%s</td><td>%s</td><td class="num">%d</td><td>%s</td><td>%s</td></tr>`,
			qs.Quarter, esc(displayDate(qs.SelectionDate)), state, qs.Picks, when, action)
	}
	b.WriteString(`</tbody></table></div>`)
	b.WriteString(`<p class="ap-legend">To re-perform a draw: in each stratum, rank the items of the quarter's wave by SHA-256 of the seed, the byte 0x1F, the stratum code,
0x1F and the item reference (hex, lowest first); pass over any item selected in an earlier quarter; take the quarter's statistical sample.
Top-ups are the items flagged for the quarter, and one that is already a pick is refused. A selected quarter is frozen and never redrawn.</p>`)

	byQ := map[int][]Selection{}
	for _, s := range v.Selections {
		byQ[s.Quarter] = append(byQ[s.Quarter], s)
	}
	for q := 1; q <= 4; q++ {
		picks := byQ[q]
		if len(picks) == 0 {
			continue
		}
		fmt.Fprintf(&b, `<details><summary>Q%d selection: %d items</summary><div class="table-wrap"><table><thead><tr><th>Item</th><th>Name</th><th>Stratum</th><th>Basis</th><th class="num">Rank in wave</th><th>Rank hash</th><th>Plan version</th></tr></thead><tbody>`, q, len(picks))
		for _, s := range picks {
			rank := "—"
			if s.RankOrder > 0 {
				rank = strconv.Itoa(s.RankOrder)
			}
			version := "—"
			if s.PlanVersion > 0 {
				version = "v" + strconv.Itoa(s.PlanVersion)
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td class="num">%s</td><td><code>%s</code></td><td>%s</td></tr>`,
				esc(s.ItemRef), esc(s.ItemName), esc(s.StratumCode), statusPill(s.Basis), rank, esc(clip(s.RankHash, 16)), version)
		}
		b.WriteString(`</tbody></table></div></details>`)
	}
	return b.String()
}

// proposeButton offers the plan's population source, if it has one.
func proposeButton(p Plan) string {
	label := map[string]string{SourcePolicies: "Propose from policy documents due for review", SourceFindings: "Propose from open audit findings"}[p.PopulationSource]
	if label == "" {
		return ""
	}
	return `<button type="button" id="popPropose" data-source="` + esc(p.PopulationSource) + `" title="Fill the box with the proposed population and preview it; nothing is saved until you apply">` + esc(label) + `</button>`
}

func populationHTML(d planPageData) string {
	p, v := d.Plan, d.Sampling
	var b strings.Builder
	fmt.Fprintf(&b, `<h2>Scope population</h2><p class="lede">%d item(s) in scope.`, len(d.Population))
	if v.Frozen {
		b.WriteString(` Frozen on ` + esc(firstNonEmpty(v.FrozenAt, "—")) + `: adding, removing or moving an item between strata or waves needs a change request; descriptions, attestations, data-quality reviews and top-up flags stay editable.`)
	}
	b.WriteString(`</p>`)

	attested := map[string]int{}
	accepted := map[string]int{}
	for _, it := range d.Population {
		if it.AttestedOn != "" {
			attested[it.StratumCode]++
		}
		if it.DQStatus == DQAccepted {
			accepted[it.StratumCode]++
		}
	}
	b.WriteString(`<div class="table-wrap"><table><thead><tr><th>Stratum</th><th class="num">Wave Q1</th><th class="num">Wave Q2</th><th class="num">Wave Q3</th><th class="num">Wave Q4</th><th class="num">No wave</th><th class="num">In cycle</th><th class="num">Total</th><th class="num">Attested</th><th class="num">DQ accepted</th><th class="num">Selected</th></tr></thead><tbody>`)
	for _, z := range v.Strata {
		n := 0
		if z.NSource == "population" {
			n = z.N + z.InCycle
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td></tr>`,
			esc(z.Name), z.WaveCounts[0], z.WaveCounts[1], z.WaveCounts[2], z.WaveCounts[3], z.NoWave, z.InCycle, n, attested[z.Code], accepted[z.Code], sum4(z.Selected))
	}
	if len(v.Strata) == 0 {
		b.WriteString(`<tr><td colspan="11" class="muted">Define the strata on the Sampling tab before loading a population.</td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)

	strataOpts := `<option value="">All strata</option>`
	for _, st := range p.Strata {
		strataOpts += `<option value="` + esc(st.Code) + `">` + esc(st.Name) + `</option>`
	}
	b.WriteString(`<div class="toolbar">
  <input id="popQ" type="search" placeholder="Search reference, name, owner, business line…" style="min-width:260px">
  <select id="popStratum">` + strataOpts + `</select>
  <select id="popWave"><option value="">All waves</option><option value="1">Wave Q1</option><option value="2">Wave Q2</option><option value="3">Wave Q3</option><option value="4">Wave Q4</option><option value="0">No wave</option></select>
  <select id="popDQ"><option value="">Any DQ status</option>` + optionList(DQStatuses, "", "") + `</select>
  <select id="popSel"><option value="">Selected or not</option><option value="yes">Selected</option><option value="no">Not selected</option><option value="topup">Flagged as a top-up</option><option value="unattested">Not attested</option></select>
  <span class="spacer"></span><span id="popCount" class="muted"></span>
  <a href="/assurance-plans/` + strconv.FormatInt(p.ID, 10) + `/population.csv">Download CSV</a>
</div>
<div id="popEdit"></div>
<div class="table-wrap" style="max-height:520px"><table class="ap-nowrap"><thead><tr><th>Item</th><th>Name</th><th>Stratum</th><th>CIF</th><th>Business line</th><th>Hosting</th><th>Internet-facing</th><th>Owner</th><th>Wave</th><th>Last tested</th><th>Attested</th><th>DQ status</th><th>DQ reviewed</th><th>Top-up</th><th>Selected</th><th>Source</th><th></th></tr></thead><tbody id="popRows"></tbody></table></div>`)

	if p.Status != StatusClosed {
		b.WriteString(`<fieldset><legend>Load or update the population from CSV text</legend>
<p class="lede">Paste CSV text: a header row, then one row per item. Copy it from a spreadsheet or an inventory export; no file is uploaded.
Columns: <code>` + strings.Join(populationFields, ", ") + `</code>. Each row needs an item_ref, and a new item a stratum (code or name).
A column the paste leaves out keeps each item's current value, so a two-column paste of item_ref and attested_on records attestations.
Comma, semicolon or tab separated; dates as YYYY-MM-DD; waves and top-ups as Q1 to Q4.</p>
<textarea id="popCSV" spellcheck="false" style="min-height:160px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px"></textarea>
<div class="toolbar">
  <label class="ap-check"><input type="checkbox" id="popMerge"> Keep the items the paste leaves out (otherwise they are removed)</label>
  <span class="spacer"></span>
  ` + proposeButton(p) + `<button type="button" id="popFillCurrent">Fill with the current population</button>
  <button type="button" id="popFillExample" title="A made-up population sized to the strata's planned sizes, spread over the waves">Fill with an example population</button>
  <button type="button" id="popPreview">Preview changes</button>
  <button type="button" class="primary" id="popApply" disabled>Apply</button>
</div>
<div id="popDiff"></div>
</fieldset>`)
	}
	return b.String()
}

// scopeScript drives the strata editor, the freeze and selection actions and
// the population table, item editor and CSV import.
const scopeScript = `
const POP = JSON.parse(document.getElementById('population-data').textContent);

const strataHost = document.getElementById('strataEditor');
if (strataHost) {
  const toRow = s => ({
    code: s.code, name: s.name, definition: s.definition, sampling: s.sampling_enabled ? 'yes' : 'no',
    confidence: +(s.confidence * 100).toFixed(4), tolerable: +(s.tolerable_rate * 100).toFixed(4), floor: +(s.floor_pct * 100).toFixed(4),
    effort_per_test: s.effort_per_test, effort_per_review: s.effort_per_review,
    w1: +(s.waves[0] * 100).toFixed(4), w2: +(s.waves[1] * 100).toFixed(4), w3: +(s.waves[2] * 100).toFixed(4), w4: +(s.waves[3] * 100).toFixed(4),
    t1: s.top_ups[0], t2: s.top_ups[1], t3: s.top_ups[2], t4: s.top_ups[3], planned_size: s.planned_size,
    cycle_years: s.cycle_years || 0, takes: (s.takes || []).join(', '),
  });
  const fromRow = r => ({
    code: r.code, name: r.name, definition: r.definition, sampling_enabled: r.sampling === 'yes',
    confidence: r.confidence / 100, tolerable_rate: r.tolerable / 100, floor_pct: r.floor / 100,
    effort_per_test: r.effort_per_test, effort_per_review: r.effort_per_review,
    waves: [r.w1 / 100, r.w2 / 100, r.w3 / 100, r.w4 / 100], top_ups: [r.t1, r.t2, r.t3, r.t4], planned_size: r.planned_size,
    cycle_years: r.cycle_years, takes: String(r.takes || '').split(/[,;]/).map(x => x.trim()).filter(Boolean),
  });
  const n = (key, label) => ({ key, label, type: 'number', width: '54px' });
  const sed = editor(strataHost, [
    { key: 'code', label: 'Code', width: '48px' }, { key: 'name', label: 'Name', width: '90px' },
    { key: 'definition', label: 'Definition', type: 'textarea', width: '160px' },
    { key: 'sampling', label: 'Sampling', type: 'select', options: ['yes', 'no'] },
    n('confidence', 'Confidence %'), n('tolerable', 'Tolerable %'), n('floor', 'Floor %'),
    n('effort_per_test', 'Effort / test'), n('effort_per_review', 'Effort / review'),
    n('w1', 'Wave Q1 %'), n('w2', 'Wave Q2 %'), n('w3', 'Wave Q3 %'), n('w4', 'Wave Q4 %'),
    n('t1', 'Top-ups Q1'), n('t2', 'Top-ups Q2'), n('t3', 'Top-ups Q3'), n('t4', 'Top-ups Q4'),
    n('planned_size', 'Planned size'), n('cycle_years', 'Cycle (years)'),
    { key: 'takes', label: 'Takes from the proposal', width: '110px' },
  ], PLAN.strata.map(toRow), () => ({ sampling: 'yes', confidence: 95, tolerable: 5, floor: 0, effort_per_test: 1, effort_per_review: 0.25, w1: 25, w2: 25, w3: 25, w4: 25, t1: 0, t2: 0, t3: 0, t4: 0, planned_size: 0, cycle_years: 0, takes: '' }));
  document.getElementById('saveStrata').addEventListener('click', () =>
    act('PUT', base + '/strata', sed.rows().map(fromRow), () => { location.hash = 'sampling'; location.reload(); }));
}

const freezeBtn = document.getElementById('freezePop');
if (freezeBtn) freezeBtn.addEventListener('click', () => {
  if (!confirm('Freeze the population of ' + POP.length + ' items and draw the selection seed? After this, adding, removing or moving an item between strata or waves needs a change request.')) return;
  act('POST', base + '/population/freeze', {}, () => { location.hash = 'sampling'; location.reload(); });
});
const seedBtn = document.getElementById('showSeed');
if (seedBtn) seedBtn.addEventListener('click', async () => {
  try { const r = await call('GET', base + '/seed'); document.getElementById('seedOut').textContent = r.seed; } catch (e) { say(e.message, true); }
});
document.querySelectorAll('[data-select]').forEach(b => b.addEventListener('click', () => {
  const q = b.dataset.select;
  if (!confirm('Select the Q' + q + ' sample now? The draw is frozen once made and is never redrawn.')) return;
  act('POST', base + '/quarters/' + q + '/select', {}, r => {
    const lines = (r.shortfalls || []).concat(r.warnings || []);
    alert('Q' + q + ': ' + r.picks.length + ' items selected.' + (lines.length ? '\n\n' + lines.join('\n') : ''));
    location.hash = 'sampling'; location.reload();
  });
}));

const popRows = document.getElementById('popRows');
const filters = ['popQ', 'popStratum', 'popWave', 'popDQ', 'popSel'].map(id => document.getElementById(id));
const qLabel = n => n ? 'Q' + n : '';
function drawPopulation() {
  const [q, st, wave, dq, sel] = filters.map(f => f ? f.value.trim().toLowerCase() : '');
  const rows = POP.filter(it => {
    if (st && it.stratum.toLowerCase() !== st) return false;
    if (wave !== '' && String(it.wave) !== wave) return false;
    if (dq && (it.dq_status || '').toLowerCase() !== dq) return false;
    if (sel === 'yes' && !it.selected_quarter) return false;
    if (sel === 'no' && it.selected_quarter) return false;
    if (sel === 'topup' && !it.topup_quarter) return false;
    if (sel === 'unattested' && it.attested_on) return false;
    if (q && ![it.item_ref, it.name, it.owner, it.business_line, it.cif, it.hosting].join(' ').toLowerCase().includes(q)) return false;
    return true;
  });
  const shown = rows.slice(0, 1000);
  popRows.innerHTML = shown.map(it => '<tr><td>' + h(it.item_ref) + '</td><td>' + h(it.name) + '</td><td>' + h(it.stratum) + '</td><td>' + h(it.cif) +
    '</td><td>' + h(it.business_line) + '</td><td>' + h(it.hosting) + '</td><td>' + (it.internet_facing ? 'Yes' : 'No') + '</td><td>' + h(it.owner) +
    '</td><td>' + qLabel(it.wave) + '</td><td>' + h(it.last_tested_on) + '</td><td>' + h(it.attested_on) + '</td><td>' + (it.dq_status ? pill(it.dq_status) : '') + '</td><td>' + h(it.dq_reviewed_on) +
    '</td><td>' + (it.topup_quarter ? qLabel(it.topup_quarter) + ' <span class="muted">' + h(it.topup_rationale) + '</span>' : '') +
    '</td><td>' + (it.selected_quarter ? qLabel(it.selected_quarter) + ' ' + pill(it.selection_basis) : '') +
    '</td><td>' + sourceLink(it) +
    '</td><td class="ctl">' + (PLAN.status === 'Closed' ? '' : '<button type="button" class="link-btn" data-edit="' + it.id + '" title="Edit ' + h(it.item_ref) + '">Edit</button>') + '</td></tr>').join('') ||
    '<tr><td colspan="17" class="muted">No items' + (POP.length ? ' match the filters.' : ' yet: paste the population below.') + '</td></tr>';
  document.getElementById('popCount').textContent = rows.length + ' of ' + POP.length + ' items' + (rows.length > shown.length ? ' (first 1000 shown)' : '');
  popRows.querySelectorAll('[data-edit]').forEach(b => b.addEventListener('click', () => editItem(POP.find(it => it.id === Number(b.dataset.edit)))));
}
function sourceLink(it) {
  if (it.source_kind === 'policy_document' && /^\d+$/.test(it.source_ref)) return '<a href="/policies/' + it.source_ref + '/view">Policy document</a>';
  if (it.source_kind === 'audit_finding' && /^\d+$/.test(it.source_ref)) return '<a href="/audit-findings#' + it.source_ref + '">Audit finding</a>';
  return h([it.source_kind, it.source_ref].filter(Boolean).join(' '));
}
function pill(word) { return '<span class="ap-st ap-st-' + String(word).toLowerCase().replace(/[ \/]/g, '-') + '">' + h(word) + '</span>'; }
filters.forEach(f => f && f.addEventListener('input', drawPopulation));
if (popRows) drawPopulation();

function editItem(it) {
  const host = document.getElementById('popEdit');
  const f = (label, key, type) => '<label>' + h(label) + '<input data-i="' + key + '" type="' + (type || 'text') + '" value="' + h(it[key]) + '"></label>';
  const sel = (label, key, opts) => '<label>' + h(label) + '<select data-i="' + key + '">' + opts.map(([v, l]) => '<option value="' + h(v) + '"' + (String(it[key]) === String(v) ? ' selected' : '') + '>' + h(l) + '</option>').join('') + '</select></label>';
  host.innerHTML = '<fieldset><legend>' + h(it.item_ref) + ' · ' + h(it.stratum) + ' · wave ' + (qLabel(it.wave) || 'none') + '</legend><div class="grid">' +
    f('Name', 'name') + f('Critical or important function', 'cif') + f('Business line', 'business_line') + f('Hosting', 'hosting') +
    sel('Internet-facing', 'internet_facing', [['false', 'No'], ['true', 'Yes']]) + f('Owner', 'owner') +
    f('1LoD attested on', 'attested_on', 'date') + sel('2LoD DQ status', 'dq_status', [['', '—']].concat(VOCAB.dq_statuses.map(s => [s, s]))) +
    f('DQ reviewed on', 'dq_reviewed_on', 'date') +
    sel('Risk-based top-up', 'topup_quarter', [['0', 'None'], ['1', 'Q1'], ['2', 'Q2'], ['3', 'Q3'], ['4', 'Q4']]) +
    '</div><label>Top-up rationale (required for a top-up)<textarea data-i="topup_rationale">' + h(it.topup_rationale) + '</textarea></label>' +
    '<div class="toolbar"><button type="button" class="primary" id="popSaveItem">Save item</button><button type="button" id="popCancelItem">Cancel</button>' +
    '<span class="muted">Stratum and wave change through the CSV import, where the whole population\'s diff is shown.</span></div></fieldset>';
  host.scrollIntoView({ block: 'nearest' });
  document.getElementById('popCancelItem').addEventListener('click', () => { host.innerHTML = ''; });
  document.getElementById('popSaveItem').addEventListener('click', () => {
    const body = {};
    host.querySelectorAll('[data-i]').forEach(el => {
      const k = el.dataset.i;
      body[k] = k === 'internet_facing' ? el.value === 'true' : k === 'topup_quarter' ? Number(el.value) : el.value;
    });
    act('PATCH', base + '/population/' + it.id, body, saved => { Object.assign(it, saved); host.innerHTML = ''; drawPopulation(); say('Saved ' + it.item_ref + '.'); });
  });
}

const csvBox = document.getElementById('popCSV');
if (csvBox) {
  const applyBtn = document.getElementById('popApply');
  const diffHost = document.getElementById('popDiff');
  let previewed = null;
  const body = () => ({ csv: csvBox.value, merge: document.getElementById('popMerge').checked });
  const invalidate = () => { previewed = null; applyBtn.disabled = true; };
  let proposalNote = '';
  csvBox.addEventListener('input', () => { proposalNote = ''; invalidate(); });
  document.getElementById('popMerge').addEventListener('change', invalidate);
  async function fill(url) {
    try { const res = await fetch(url); if (!res.ok) throw new Error((await res.json()).error || 'HTTP ' + res.status); csvBox.value = await res.text(); proposalNote = ''; invalidate(); say('Filled. Preview it to see what would change.'); }
    catch (e) { say(e.message, true); }
  }
  document.getElementById('popFillCurrent').addEventListener('click', () => fill(base + '/population.csv'));
  const proposeBtn = document.getElementById('popPropose');
  if (proposeBtn) proposeBtn.addEventListener('click', async () => {
    try {
      say('Working out the proposal…');
      const r = await call('POST', base + '/population/propose?source=' + encodeURIComponent(proposeBtn.dataset.source), {});
      csvBox.value = r.csv;
      document.getElementById('popMerge').checked = true;
      invalidate();
      proposalNote = '<h3>Proposal: ' + r.items + ' item(s), ' + r.new + ' new</h3>' +
        (r.notes.length ? '<ul>' + r.notes.map(n => '<li>' + h(n) + '</li>').join('') + '</ul>' : '');
      diffHost.innerHTML = proposalNote;
      if (r.items) document.getElementById('popPreview').click(); else say('Nothing to propose.');
    } catch (e) { say(e.message, true); }
  });
  document.getElementById('popFillExample').addEventListener('click', () => fill(base + '/population/example.csv'));
  const list = (label, items) => items.length ? '<h3>' + h(label) + ' (' + items.length + ')</h3><p>' + items.slice(0, 60).map(h).join(', ') + (items.length > 60 ? ' … and ' + (items.length - 60) + ' more' : '') + '</p>' : '';
  function showDiff(d) {
    const byStratum = Object.entries(d.by_stratum || {}).map(([k, v]) => h(k) + ' ' + v).join(', ');
    let html = '<p>' + d.rows + ' row(s) read; columns: ' + h(d.columns.join(', ')) + (d.ignored_columns.length ? '; ignored: ' + h(d.ignored_columns.join(', ')) : '') +
      '. After the change: ' + (byStratum || 'no items') + '.</p>';
    if (d.errors.length) {
      html += '<h3>' + pill('Blocking') + ' ' + d.errors.length + ' problem(s): nothing can be applied until they are fixed</h3><div class="table-wrap"><table><thead><tr><th>Line</th><th>Item</th><th>Problem</th></tr></thead><tbody>' +
        d.errors.map(e => '<tr><td>' + (e.line || '—') + '</td><td>' + h(e.item_ref) + '</td><td>' + h(e.message) + '</td></tr>').join('') + '</tbody></table></div>';
    }
    html += list('Added', d.added) + list('Removed', d.removed);
    if (d.changed.length) {
      html += '<h3>Changed (' + d.changed.length + ')</h3><div class="table-wrap" style="max-height:260px"><table><thead><tr><th>Item</th><th>Changes</th></tr></thead><tbody>' +
        d.changed.slice(0, 300).map(c => '<tr><td>' + h(c.item_ref) + '</td><td>' + h(c.fields.join('; ')) + '</td></tr>').join('') + '</tbody></table></div>';
    }
    html += '<p class="muted">' + d.unchanged + ' item(s) unchanged.</p>';
    diffHost.innerHTML = proposalNote + html;
  }
  document.getElementById('popPreview').addEventListener('click', async () => {
    try {
      say('Checking…');
      const d = await call('POST', base + '/population/preview', body());
      showDiff(d);
      const changes = d.added.length + d.changed.length + d.removed.length;
      previewed = d.errors.length || !changes ? null : d.base;
      applyBtn.disabled = previewed === null;
      say(d.errors.length ? 'The paste has problems: see the list.' : changes ? 'Preview ready: ' + d.added.length + ' added, ' + d.changed.length + ' changed, ' + d.removed.length + ' removed. Apply to save.' : 'Nothing would change.');
    } catch (e) { say(e.message, true); }
  });
  applyBtn.addEventListener('click', () => {
    if (previewed === null) return;
    act('POST', base + '/population', Object.assign(body(), { base: previewed }), () => { location.hash = 'population'; location.reload(); });
  });
}
`
