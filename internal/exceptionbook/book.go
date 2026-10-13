// Package exceptionbook writes the Exceptions page as a workbook that works on
// its own: the Security NFR catalog with its weights, the NIST controls, the
// links between them, and formulas that redo what the page does when
// requirements are selected -- the linked controls, the confidentiality,
// integrity and availability counts, the threat coverage and the detail view.
//
// The page's logic lives in its JavaScript (internal/app/exceptions.go). This
// package states it twice more: as spreadsheet formulas, and in Go (evaluate),
// which gives every formula the value it is stored with. The test that opens
// the workbook in LibreOffice holds the two to each other.
package exceptionbook

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"grc/internal/controlcatalog"
	"grc/internal/nfrlink"
	"grc/internal/securitynfr"
	"grc/internal/xlsx"
)

const (
	SheetGuide         = "How to use"
	SheetExceptions    = "Exceptions"
	SheetSummary       = "Summary"
	SheetNFRDetail     = "Selected NFRs"
	SheetControlDetail = "Selected controls"
	SheetLinks         = "Links"
	SheetControls      = "Controls"
)

// SelectedMark is what the Scope column holds for a requirement in the scope of the
// exercise.
const SelectedMark = "Yes"

// FullMark and PartialMark are what the Exception column holds. A partial
// exception counts PartialFactor of the weight; a full one counts all of it.
const (
	FullMark      = "Full"
	PartialMark   = "Partial"
	PartialFactor = 0.5
)

// The Exceptions sheet. Rows 1-4 are the title, the two live lines and the
// hint. The columns after exImplementation are hidden: nobody types in them.
const (
	exScope = iota + 1
	exSel
	exSummary
	exDomain
	exWeight
	exLinked
	exMapping
	exComments
	exDetails
	exImplementation
	// The working columns. exKey is the NFR's key: the Links sheet finds an
	// NFR's row by it, so it stays although it is not shown among the rest.
	exKey
	exInScope
	exSelected
	exFactor
	exOrder
	// What the row's NFR touches: 1 when a requirement it is linked to carries
	// the rating. The threats follow exTouchA, a column for each.
	exTouchC
	exTouchI
	exTouchA
	exFirstTouchThreat

	exHeaderRow = 5
	exFirstRow  = 6
)

const (
	lkKey = iota + 1
	lkNFRID
	lkSummary
	lkMapping
	lkControl
	lkControlName
	lkFamily
	lkMatched
	lkLinked
	lkExcepted
	lkNew
	lkOrder
	lkC
	lkI
	lkA
	lkThreats
	// A column for each threat: 1 when the link's requirement addresses it.
	lkFirstThreat

	lkFirstRow = 2
)

const (
	ctID = iota + 1
	ctName
	ctFamily
	ctType
	ctBaselines
	ctC
	ctI
	ctA
	ctThreats
	ctFirstThreat

	ctFirstRow = 2
)

// The Summary sheet. The threat rows start at smFirstThreat and everything
// below them moves down with their number.
const (
	// The columns of the lines for a rating or a threat.
	smColExcepted = 2
	smColScope    = 3
	smColShare    = 4
	smColCount    = 5
	smColumns     = 5

	smCatalog     = 5
	smScope       = 6
	smSelected    = 7
	smControls    = 8
	smC           = 11
	smI           = 12
	smA           = 13
	smFirstThreat = 16
)

const (
	detailHeaderRow = 4
	detailFirstRow  = 5
	// The last column of each detail sheet: the row of the source sheet that
	// the line is read from.
	nfrDetailSource = 12
	detailSource    = 13
)

// threatDisplayOrder is the order the Exceptions page lists threats in; any
// other threat follows alphabetically.
var threatDisplayOrder = []string{
	"Spoofing",
	"Tampering",
	"Repudiation",
	"Information Disclosure",
	"Denial of Service",
	"Elevation of Privilege",
}

// Data is what the Exceptions page loads: /security-nfrs/data,
// /security-nfrs/links/data and /controls/data.
type Data struct {
	NFRs     []securitynfr.NFR
	Links    []nfrlink.LinkRow
	Controls []controlcatalog.Control
}

type Options struct {
	// Selected is the keys of the requirements the workbook opens with in the
	// exception set. Keys that are not in the catalog are ignored, and so are
	// keys outside the scope: a requirement out of scope cannot be excepted.
	Selected []string
	// Scope is the keys of the requirements in the scope of the exercise. It
	// is read only when Scoped is true; otherwise the whole catalog is in scope.
	Scope  []string
	Scoped bool
	// Partial is the keys of the selected ones whose exception is partial.
	// A key that is not selected is ignored.
	Partial []string
	// Descriptions is what is said of the partial exceptions, by key. One for
	// a key that is not a partial exception is ignored.
	Descriptions map[string]string
	GeneratedAt  time.Time
}

// ThreatCount is one threat: how many of the linked controls address it, and
// the weight of the requirements that reach it.
type ThreatCount struct {
	Threat   string
	Controls int
	Weight   WeightedCoverage
}

// WeightedCoverage is how much weight touches one rating or one threat. A
// requirement touches it when a control it is linked to carries it, and then
// counts its whole weight once, however many such controls it has. Excepted
// is the weight of the excepted requirements that touch it, a partial
// exception counting half; Scope is the weight of the requirements in scope
// that do.
type WeightedCoverage struct {
	Excepted float64
	Scope    int
}

// Share is the excepted weight's share of the scope's as a whole percentage,
// a half rounding up. Nothing in scope touching it is no share.
func (w WeightedCoverage) Share() int {
	if w.Scope <= 0 {
		return 0
	}
	return roundHalfUp(w.Excepted * 100 / float64(w.Scope))
}

// text is the coverage as the application's pages word it.
func (w WeightedCoverage) text() string {
	return weightText(w.Excepted) + " of " + strconv.Itoa(w.Scope) + " (" + strconv.Itoa(w.Share()) + "%)"
}

// Result is what the workbook shows for the selection it was written with.
type Result struct {
	CatalogNFRs     int
	ScopeNFRs       int
	SelectedNFRs    int
	LinkedControls  int
	Confidentiality int
	Integrity       int
	Availability    int
	// The weight that touches each rating. These, and the weight of each
	// threat, are the figures the analysis is read by; the total weight below
	// is a minor one.
	ConfidentialityWeight WeightedCoverage
	IntegrityWeight       WeightedCoverage
	AvailabilityWeight    WeightedCoverage
	Threats               []ThreatCount
	SelectedWeight        float64
	ScopeWeight           int
	// ByWeight has one entry for each weight a requirement can carry, lowest
	// first.
	ByWeight []WeightCount
}

// WeightCount is how many requirements carry one weight: in the scope and in
// the selection.
type WeightCount struct {
	Weight   int
	Scope    int
	Selected int
}

// ScopeLine, CIAWeightLine, ThreatWeightLine, CIALine and ThreatLine are the
// summary lines exactly as the application words them: the
// Exceptions sheet has them at its top.
func (r Result) ScopeLine() string {
	return "Scope: " + strconv.Itoa(r.ScopeNFRs) + " of " + strconv.Itoa(r.CatalogNFRs) + " Security NFRs"
}

func (r Result) CIAWeightLine() string {
	return "CIA by weight: C: " + r.ConfidentialityWeight.text() +
		" | I: " + r.IntegrityWeight.text() +
		" | A: " + r.AvailabilityWeight.text()
}

func (r Result) ThreatWeightLine() string {
	parts := []string{}
	for _, threat := range r.Threats {
		if threat.Weight.Excepted > 0 {
			parts = append(parts, threat.Threat+": "+threat.Weight.text())
		}
	}
	if len(parts) == 0 {
		return "Threats by weight: no linked threat mappings"
	}
	return "Threats by weight: " + strings.Join(parts, " | ")
}

func (r Result) CIALine() string {
	return "Linked Controls: " + strconv.Itoa(r.LinkedControls) +
		" | C: " + strconv.Itoa(r.Confidentiality) +
		" | I: " + strconv.Itoa(r.Integrity) +
		" | A: " + strconv.Itoa(r.Availability)
}

func (r Result) ThreatLine() string {
	parts := []string{}
	for _, threat := range r.Threats {
		if threat.Controls > 0 {
			parts = append(parts, threat.Threat+": "+strconv.Itoa(threat.Controls))
		}
	}
	if len(parts) == 0 {
		return "Threat coverage: no linked threat mappings"
	}
	return "Threat coverage: " + strings.Join(parts, " | ")
}

// SummarySection is one block of the Summary sheet: its title, the headings
// of its value columns where it has them, and its lines.
type SummarySection struct {
	Title   string
	Columns []string
	Rows    []SummaryRow
	// Minor marks a section that is shown as a lesser figure: the total
	// weight, beside the weight that touches each rating and threat.
	Minor bool
}

// SummaryRow is one line of a SummarySection: its label, and its values as
// they are shown.
type SummaryRow struct {
	Label  string
	Values []string
}

// Summary is the Summary sheet for r, as text, for a page that shows it. The
// sheet's working rows, which only word the lines of the Exceptions sheet,
// are left out. A test holds it to the sheet line for line.
func (r Result) Summary() []SummarySection {
	count := func(n int) []string { return []string{strconv.Itoa(n)} }
	// The weight that touches a rating or a threat, and then the number of
	// linked controls that carry it.
	coverageColumns := []string{"Excepted weight", "Scope weight", "Share", "Controls"}
	coverage := func(w WeightedCoverage, n int) []string {
		return []string{weightText(w.Excepted), strconv.Itoa(w.Scope), strconv.Itoa(w.Share()) + "%", strconv.Itoa(n)}
	}

	threats := make([]SummaryRow, 0, len(r.Threats))
	for _, threat := range r.Threats {
		threats = append(threats, SummaryRow{Label: threat.Threat, Values: coverage(threat.Weight, threat.Controls)})
	}

	share := 0
	if r.ScopeWeight > 0 {
		share = roundHalfUp(r.SelectedWeight * 100 / float64(r.ScopeWeight))
	}
	average := "-"
	if r.SelectedNFRs > 0 {
		average = strconv.FormatFloat(float64(roundHalfUp(r.SelectedWeight*100/float64(r.SelectedNFRs)))/100, 'f', 2, 64)
	}

	return []SummarySection{
		{Title: "Scope and selection", Rows: []SummaryRow{
			{Label: "Security NFRs in the catalog", Values: count(r.CatalogNFRs)},
			{Label: "Security NFRs in scope", Values: count(r.ScopeNFRs)},
			{Label: "Selected Security NFRs", Values: count(r.SelectedNFRs)},
			{Label: "Linked Controls", Values: count(r.LinkedControls)},
		}},
		{Title: "CIA coverage", Columns: coverageColumns, Rows: []SummaryRow{
			{Label: "Confidentiality (C)", Values: coverage(r.ConfidentialityWeight, r.Confidentiality)},
			{Label: "Integrity (I)", Values: coverage(r.IntegrityWeight, r.Integrity)},
			{Label: "Availability (A)", Values: coverage(r.AvailabilityWeight, r.Availability)},
		}},
		{Title: "Threat coverage", Columns: coverageColumns, Rows: threats},
		{Title: "Total weight (minor)", Minor: true, Rows: []SummaryRow{
			{Label: "Weight of the selection", Values: []string{weightText(r.SelectedWeight)}},
			{Label: "Weight of the scope", Values: count(r.ScopeWeight)},
			{Label: "Share of the scope's weight", Values: []string{strconv.Itoa(share) + "%"}},
			{Label: "Average weight of a selected NFR", Values: []string{average}},
		}},
		{Title: "Weight", Columns: []string{"In the scope", "In the selection"}, Minor: true, Rows: r.weightRows()},
	}
}

// weightRows is the table of the Summary sheet that counts the requirements at
// each weight.
func (r Result) weightRows() []SummaryRow {
	rows := make([]SummaryRow, 0, len(r.ByWeight))
	for _, count := range r.ByWeight {
		rows = append(rows, SummaryRow{
			Label:  strconv.Itoa(count.Weight),
			Values: []string{strconv.Itoa(count.Scope), strconv.Itoa(count.Selected)},
		})
	}
	return rows
}

type nfrRow struct {
	securitynfr.NFR
	inScope  bool
	selected bool
	partial  bool
	order    int
	linked   int
	// note is what is said of a partial exception.
	note string
	// What the requirement touches: the ratings and the threats that the
	// controls it is linked to carry.
	touchC, touchI, touchA bool
	touchThreats           map[string]bool
}

// touch records what a control the requirement is linked to carries.
func (n *nfrRow) touch(control controlRow) {
	n.touchC = n.touchC || strings.TrimSpace(control.Confidentiality) != ""
	n.touchI = n.touchI || strings.TrimSpace(control.Integrity) != ""
	n.touchA = n.touchA || strings.TrimSpace(control.Availability) != ""
	for threat := range control.threats {
		if n.touchThreats == nil {
			n.touchThreats = map[string]bool{}
		}
		n.touchThreats[threat] = true
	}
}

// add counts the requirement's weight in w: in the scope's when it is in
// scope, and in the excepted weight, halved for a partial exception, when it
// is excepted.
func (n nfrRow) add(w *WeightedCoverage) {
	if !n.inScope {
		return
	}
	w.Scope += n.Weight
	w.Excepted += float64(n.Weight) * n.factor()
}

// factor is the share of its weight an excepted NFR counts for.
func (n nfrRow) factor() float64 {
	switch {
	case !n.selected:
		return 0
	case n.partial:
		return PartialFactor
	default:
		return 1
	}
}

// comments is what the Comments column holds: what is said of the partial
// exception where something is, and otherwise the catalog's description.
func (n nfrRow) comments() string {
	if n.note != "" {
		return n.note
	}
	return n.Description
}

// mark is what the Exception column holds for the NFR.
func (n nfrRow) mark() string {
	switch {
	case !n.selected:
		return ""
	case n.partial:
		return PartialMark
	default:
		return FullMark
	}
}

// weightText writes a weight as the page does: whole, or with its half.
func weightText(weight float64) string {
	return strconv.FormatFloat(weight, 'f', -1, 64)
}

type linkRow struct {
	nfrlink.LinkRow
	key      string
	control  string
	linked   bool
	excepted bool
	isNew    bool
	order    int
}

type controlRow struct {
	controlcatalog.Control
	id       string
	threats  map[string]bool
	excepted bool
}

type model struct {
	nfrs     []nfrRow
	links    []linkRow
	controls []controlRow
	byID     map[string]int
	threats  []string
	result   Result
}

// evaluate is the Exceptions page's logic. Where the page and a spreadsheet
// would differ, the comments say which way this goes.
func evaluate(data Data, opts Options) model {
	m := model{byID: map[string]int{}}

	chosen := map[string]bool{}
	for _, key := range opts.Selected {
		chosen[strings.TrimSpace(key)] = true
	}
	scope := map[string]bool{}
	for _, key := range opts.Scope {
		scope[strings.TrimSpace(key)] = true
	}
	partial := map[string]bool{}
	for _, key := range opts.Partial {
		partial[strings.TrimSpace(key)] = true
	}
	notes := map[string]string{}
	for key, text := range opts.Descriptions {
		notes[strings.TrimSpace(key)] = strings.TrimSpace(text)
	}

	seenKey := map[string]bool{}
	for _, nfr := range data.NFRs {
		nfr.Key = strings.TrimSpace(nfr.Key)
		if nfr.Key == "" || seenKey[nfr.Key] {
			continue
		}
		seenKey[nfr.Key] = true
		inScope := !opts.Scoped || scope[nfr.Key]
		selected := inScope && chosen[nfr.Key]
		row := nfrRow{NFR: nfr, inScope: inScope, selected: selected, partial: selected && partial[nfr.Key]}
		if row.partial {
			row.note = notes[nfr.Key]
		}
		m.nfrs = append(m.nfrs, row)
	}
	sort.SliceStable(m.nfrs, func(i, j int) bool { return nfrLess(m.nfrs[i].NFR, m.nfrs[j].NFR) })
	position := map[string]int{}
	for i := range m.nfrs {
		position[m.nfrs[i].Key] = i
	}

	// The page keeps controls in a map by id, so a repeated id is one control
	// and the later record is the one it reads.
	for _, control := range data.Controls {
		id := strings.TrimSpace(control.ID)
		if id == "" {
			continue
		}
		row := controlRow{Control: control, id: id, threats: map[string]bool{}}
		for _, threat := range control.Threats {
			if threat = strings.TrimSpace(threat); threat != "" {
				row.threats[threat] = true
			}
		}
		if at, ok := m.byID[id]; ok {
			m.controls[at] = row
			continue
		}
		m.byID[id] = len(m.controls)
		m.controls = append(m.controls, row)
	}

	for _, link := range data.Links {
		m.links = append(m.links, linkRow{
			LinkRow: link,
			key:     strings.TrimSpace(link.NFRKey),
			control: strings.TrimSpace(link.ControlID),
		})
	}
	// In the order the detail view reads them: by requirement, then control.
	sort.SliceStable(m.links, func(i, j int) bool {
		a, b := m.links[i], m.links[j]
		pa, oka := position[a.key]
		pb, okb := position[b.key]
		if oka != okb {
			return oka
		}
		if oka && pa != pb {
			return pa < pb
		}
		if !oka && a.key != b.key {
			return naturalLess(a.key, b.key)
		}
		if a.control != b.control {
			return naturalLess(a.control, b.control)
		}
		return naturalLess(a.MappingControlID, b.MappingControlID)
	})

	// A link counts when the mapping matched a control; a requirement that
	// reaches the same control through two mappings is linked to it once.
	pairs := map[[2]string]bool{}
	exceptedControls := map[string]bool{}
	order := 0
	for i := range m.links {
		link := &m.links[i]
		pair := [2]string{link.key, link.control}
		if !link.Matched || link.key == "" || link.control == "" || pairs[pair] {
			continue
		}
		pairs[pair] = true
		link.linked = true
		at, known := position[link.key]
		if !known {
			continue
		}
		m.nfrs[at].linked++
		// Only a control the catalog lists has ratings and threats.
		if index, listed := m.byID[link.control]; listed {
			m.nfrs[at].touch(m.controls[index])
		}
		if !m.nfrs[at].selected {
			continue
		}
		link.excepted = true
		order++
		link.order = order
		if !exceptedControls[link.control] {
			exceptedControls[link.control] = true
			link.isNew = true
			m.result.LinkedControls++
		}
	}

	extra := map[string]bool{}
	for i := range m.controls {
		control := &m.controls[i]
		for threat := range control.threats {
			extra[threat] = true
		}
		// "Linked Controls" above counts every linked control id. The three
		// counts below, and the threats, count the ones the catalog lists: a
		// control in a hidden family is linked but is not in /controls/data.
		if !exceptedControls[control.id] {
			continue
		}
		control.excepted = true
		if strings.TrimSpace(control.Confidentiality) != "" {
			m.result.Confidentiality++
		}
		if strings.TrimSpace(control.Integrity) != "" {
			m.result.Integrity++
		}
		if strings.TrimSpace(control.Availability) != "" {
			m.result.Availability++
		}
	}

	m.threats = append(m.threats, threatDisplayOrder...)
	for _, threat := range threatDisplayOrder {
		delete(extra, threat)
	}
	others := make([]string, 0, len(extra))
	for threat := range extra {
		others = append(others, threat)
	}
	sort.Slice(others, func(i, j int) bool {
		a, b := strings.ToLower(others[i]), strings.ToLower(others[j])
		if a != b {
			return a < b
		}
		return others[i] < others[j]
	})
	m.threats = append(m.threats, others...)
	for _, threat := range m.threats {
		count := 0
		for _, control := range m.controls {
			if control.excepted && control.threats[threat] {
				count++
			}
		}
		weight := WeightedCoverage{}
		for _, nfr := range m.nfrs {
			if nfr.touchThreats[threat] {
				nfr.add(&weight)
			}
		}
		m.result.Threats = append(m.result.Threats, ThreatCount{Threat: threat, Controls: count, Weight: weight})
	}
	for _, nfr := range m.nfrs {
		if nfr.touchC {
			nfr.add(&m.result.ConfidentialityWeight)
		}
		if nfr.touchI {
			nfr.add(&m.result.IntegrityWeight)
		}
		if nfr.touchA {
			nfr.add(&m.result.AvailabilityWeight)
		}
	}

	for weight := securitynfr.MinWeight; weight <= securitynfr.MaxWeight; weight++ {
		m.result.ByWeight = append(m.result.ByWeight, WeightCount{Weight: weight})
	}
	rank := 0
	for i := range m.nfrs {
		nfr := &m.nfrs[i]
		m.result.CatalogNFRs++
		// The figures are measured against the scope, not the whole catalog.
		if !nfr.inScope {
			continue
		}
		m.result.ScopeNFRs++
		m.result.ScopeWeight += nfr.Weight
		at := nfr.Weight - securitynfr.MinWeight
		counted := at >= 0 && at < len(m.result.ByWeight)
		if counted {
			m.result.ByWeight[at].Scope++
		}
		if !nfr.selected {
			continue
		}
		if counted {
			m.result.ByWeight[at].Selected++
		}
		rank++
		nfr.order = rank
		m.result.SelectedNFRs++
		m.result.SelectedWeight += float64(nfr.Weight) * nfr.factor()
	}
	return m
}

// Evaluate is the result for a scope and a selection without the workbook.
func Evaluate(data Data, opts Options) Result { return evaluate(data, opts).result }

// nfrLess is the order the page lists a selection in: by NFR id as a number
// where it is one, then as text with its digits read as numbers.
func nfrLess(a, b securitynfr.NFR) bool {
	left, leftErr := strconv.ParseFloat(strings.TrimSpace(a.ID), 64)
	right, rightErr := strconv.ParseFloat(strings.TrimSpace(b.ID), 64)
	if leftErr == nil && rightErr == nil && left != right {
		return left < right
	}
	if a.ID != b.ID {
		return naturalLess(a.ID, b.ID)
	}
	return naturalLess(a.Key, b.Key)
}

// naturalLess orders AC-2 before AC-10.
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		digitsA, digitsB := leadingDigits(a), leadingDigits(b)
		if digitsA > 0 && digitsB > 0 {
			numA, numB := strings.TrimLeft(a[:digitsA], "0"), strings.TrimLeft(b[:digitsB], "0")
			if len(numA) != len(numB) {
				return len(numA) < len(numB)
			}
			if numA != numB {
				return numA < numB
			}
			a, b = a[digitsA:], b[digitsB:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return a == "" && b != ""
}

func leadingDigits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

func flag(on bool) float64 {
	if on {
		return 1
	}
	return 0
}

func rowsOrOne(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// layout is where everything is, for the formulas that point at it.
type layout struct {
	exLast, lkLast, ctLast int
	ctExcepted             int
	threats                int
}

func (l layout) ex(col int) string {
	return xlsx.SheetRef(SheetExceptions, xlsx.AbsRange(col, exFirstRow, col, l.exLast))
}

func (l layout) lk(col int) string {
	return xlsx.SheetRef(SheetLinks, xlsx.AbsRange(col, lkFirstRow, col, l.lkLast))
}

func (l layout) ct(col int) string {
	return xlsx.SheetRef(SheetControls, xlsx.AbsRange(col, ctFirstRow, col, l.ctLast))
}

func summaryCell(row int) string { return xlsx.SheetRef(SheetSummary, xlsx.Abs(2, row)) }

// The rows of the Summary sheet below the threat list.
func (l layout) smSelectedWeight() int { return smFirstThreat + l.threats + 2 }
func (l layout) smScopeWeight() int    { return l.smSelectedWeight() + 1 }
func (l layout) smShare() int          { return l.smSelectedWeight() + 2 }
func (l layout) smAverage() int        { return l.smSelectedWeight() + 3 }
func (l layout) smWeightTable() int    { return l.smSelectedWeight() + 5 }

// smLastRow is the last row of the Summary sheet: the heaviest weight's.
func (l layout) smLastRow() int {
	return l.smWeightTable() + securitynfr.MaxWeight - securitynfr.MinWeight + 1
}

// exLastCol is the last column of the Exceptions sheet: the last threat's.
func (l layout) exLastCol() int { return exFirstTouchThreat + l.threats - 1 }

// Build writes the workbook for data with the given scope and the given
// requirements selected.
func Build(data Data, opts Options) (*xlsx.Workbook, Result) {
	m := evaluate(data, opts)
	l := layout{
		exLast:     exFirstRow + rowsOrOne(len(m.nfrs)) - 1,
		lkLast:     lkFirstRow + rowsOrOne(len(m.links)) - 1,
		ctLast:     ctFirstRow + rowsOrOne(len(m.controls)) - 1,
		ctExcepted: ctFirstThreat + len(m.threats),
		threats:    len(m.threats),
	}

	book := xlsx.New()
	writeGuide(book.AddSheet(SheetGuide), m, opts)
	writeExceptions(book.AddSheet(SheetExceptions), m, l)
	writeSummary(book.AddSheet(SheetSummary), m, l)
	writeNFRDetail(book.AddSheet(SheetNFRDetail), m, l)
	writeControlDetail(book.AddSheet(SheetControlDetail), m, l)
	writeLinks(book.AddSheet(SheetLinks), m, l)
	writeControls(book.AddSheet(SheetControls), m, l)
	book.Active = 1
	return book, m.result
}

func writeGuide(s *xlsx.Sheet, m model, opts Options) {
	generated := opts.GeneratedAt
	if generated.IsZero() {
		generated = time.Now()
	}
	matched := 0
	for _, link := range m.links {
		if link.linked {
			matched++
		}
	}

	s.Widths(118)
	row := 1
	line := func(text string, style xlsx.Style) {
		s.Set(1, row, xlsx.Text(text).With(style))
		row++
	}
	line("Exceptions workbook", xlsx.Title)
	line("A working copy of the Exceptions page of the GRC application. Nothing here needs the application: every number is a formula over the data in this file.", xlsx.Wrap)
	line("Data as of "+generated.Format("2 January 2006 15:04 MST")+": "+
		strconv.Itoa(len(m.nfrs))+" Security NFRs with their weights, "+
		strconv.Itoa(len(m.controls))+" NIST controls, "+
		strconv.Itoa(matched)+" links between them. It is a snapshot and does not follow later changes in the application.", xlsx.Wrap)
	row++

	line("Building an exception set", xlsx.Section)
	line("1. Open the Exceptions sheet. Each row is a Security NFR.", xlsx.Wrap)
	line("2. Set the scope of the exercise first: "+SelectedMark+" in the Scope column ("+xlsx.Col(exScope)+") for every requirement the exercise covers. Clear the cell to take one out of scope. The workbook opens with the scope it was downloaded with. Downloaded with no selections, the column is empty and no requirement is in scope yet: this is where to start.", xlsx.Wrap)
	line("3. Choose "+FullMark+" or "+PartialMark+" in the Exception column ("+xlsx.Col(exSel)+") for every requirement of the scope in the exception set: "+FullMark+" when it is not met at all, "+PartialMark+" when it is met in part. Clear the cell to take one out. A requirement outside the scope is not counted, whatever its Exception cell holds.", xlsx.Wrap)
	line("4. Describe every partial exception in the Comments column ("+xlsx.Col(exComments)+"): what is met and what is not. A partial exception should not be left without a description. One typed on the Exceptions page of the application comes with the workbook.", xlsx.Wrap)
	line("5. The two lines at the top of the sheet say how many NFRs are in scope and how many are excepted. The figures are on the Summary sheet and change as you go: the weight that touches each of confidentiality, integrity and availability and each threat, the counts, and the total weight.", xlsx.Wrap)
	line("6. Selected NFRs and Selected controls are the detail view of the selection, ready to print.", xlsx.Wrap)
	row++

	line("The page's buttons, here", xlsx.Section)
	line("Search and the domain filter: the filter buttons on the header row of the Exceptions sheet.", xlsx.Wrap)
	line("Scope and Exceptions: the two steps of the page are the Scope column ("+xlsx.Col(exScope)+") and the Exception column ("+xlsx.Col(exSel)+"). The three lines below work on either.", xlsx.Wrap)
	line("Select All: filter the rows, select the visible cells of the column, type "+SelectedMark+" for the scope or "+FullMark+" for an exception, and press Ctrl+Enter.", xlsx.Wrap)
	line("Clear Filtered: filter the rows, select the visible cells of the column and press Delete.", xlsx.Wrap)
	line("Clear All: clear the filter, select the column from row "+strconv.Itoa(exFirstRow)+" down and press Delete.", xlsx.Wrap)
	line("Export Scope and Import Scope: save the workbook. Its Scope column is the scope, and it is kept with the file.", xlsx.Wrap)
	line("Open Details and Print Details: the Selected NFRs and Selected controls sheets.", xlsx.Wrap)
	row++

	line("The sheets", xlsx.Section)
	line(SheetExceptions+": the Security NFR catalog with its weights, and the Scope and Exception columns. The columns after Implementation are working columns and are hidden: the formulas of the other sheets read them.", xlsx.Wrap)
	line(SheetSummary+": the scope, the selection, the linked controls, the C / I / A counts, the threat coverage and the weights.", xlsx.Wrap)
	line(SheetNFRDetail+": each selected requirement in full.", xlsx.Wrap)
	line(SheetControlDetail+": each selected requirement's linked controls, with their ratings and threats.", xlsx.Wrap)
	line(SheetLinks+": which NIST control each requirement's NIST mapping resolves to. Columns I to L, and the columns after Threats, are working columns.", xlsx.Wrap)
	line(SheetControls+": the NIST controls, their ratings and the threats each addresses. The last column marks the controls in the exception set.", xlsx.Wrap)
	row++

	line("How the numbers are worked out", xlsx.Section)
	line("Linked Controls: the distinct controls that the selected requirements are linked to. A link counts when the requirement's NIST mapping matched a control.", xlsx.Wrap)
	line("C, I and A by weight, and each threat by weight: the figures to read the exception set by. An NFR touches a rating or a threat when one of the controls it is linked to carries it, and then counts its whole weight once, however many of them carry it. The figure is the weight of the excepted NFRs that touch it, a partial exception counting half, of the weight of the NFRs in scope that touch it.", xlsx.Wrap)
	line("C, I and A: how many of those controls have a confidentiality, integrity or availability rating.", xlsx.Wrap)
	line("Threat coverage: for each threat, how many of those controls address it.", xlsx.Wrap)
	line("Total weight, a minor figure: each requirement's weight is 1 (lowest) to 5 (highest). The weight of the selection is the sum of the weights of the selected requirements, a partial exception counting half its requirement's weight; its share is that sum over the sum for the requirements in scope. The Exceptions Report of the application shows the same figures.", xlsx.Wrap)
	line("A weight can be edited on the Exceptions sheet, and the numbers follow. The "+SheetLinks+" and "+SheetControls+" sheets are locked: they hold what the application exported. A link or a rating is changed in the application, and a new workbook is downloaded. Review > Unprotect Sheet lifts the lock; there is no password.", xlsx.Wrap)
}

func writeExceptions(s *xlsx.Sheet, m model, l layout) {
	widths := []float64{8, 11, 62, 30, 8, 10, 22, 50, 50, 50, 7, 9, 9, 8, 7, 10, 10, 10}
	for range m.threats {
		widths = append(widths, 12)
	}
	s.Widths(widths...)
	// The working columns hold the formulas every figure is worked out from.
	// They are hidden, not left out: a row keeps its own when the sheet is
	// sorted, which a copy of them on another sheet would not.
	s.HideColumns(exKey, l.exLastCol())
	s.Freeze(exSummary, exHeaderRow)

	r := m.result
	s.Set(1, 1, xlsx.Text("Exceptions").With(xlsx.Title))
	s.Set(1, 2, xlsx.FormulaText(
		`"Scope: "&`+summaryCell(smScope)+`&" of "&`+summaryCell(smCatalog)+`&" Security NFRs"`,
		r.ScopeLine()).With(xlsx.Label))
	// The figures are on the Summary sheet. Here are only how many NFRs are in
	// scope and how many are excepted, with what the exceptions are linked to.
	count := func(row int) string { return xlsx.SheetRef(SheetSummary, xlsx.Abs(smColCount, row)) }
	s.Set(1, 3, xlsx.FormulaText(
		`"Exceptions: "&`+summaryCell(smSelected)+`&" | Linked Controls: "&`+summaryCell(smControls)+
			`&" | C: "&`+count(smC)+`&" | I: "&`+count(smI)+`&" | A: "&`+count(smA),
		"Exceptions: "+strconv.Itoa(r.SelectedNFRs)+" | "+r.CIALine()).With(xlsx.Label))
	s.Set(1, 4, xlsx.Text("Choose "+SelectedMark+" in column "+xlsx.Col(exScope)+" to put a Security NFR in the scope of the exercise, and "+FullMark+" or "+PartialMark+" in column "+xlsx.Col(exSel)+" to put an NFR of the scope in the exception set. The filter buttons on the header row search and filter by domain.").With(xlsx.Muted))

	headers := []string{"Scope", "Exception", "Summary", "Domain", "Weight", "Linked Controls",
		"NIST Mapping", "Comments", "Additional Details", "Implementation", "Key", "In scope", "Selected", "Factor", "Order",
		"Touches C", "Touches I", "Touches A"}
	for _, threat := range m.threats {
		headers = append(headers, "Touches "+threat)
	}
	for i, header := range headers {
		s.Set(i+1, exHeaderRow, xlsx.Text(header).With(xlsx.Header))
	}

	for i, nfr := range m.nfrs {
		row := exFirstRow + i
		mark := xlsx.Cell{}
		if nfr.selected {
			mark = xlsx.Text(nfr.mark())
		}
		s.Set(exSel, row, mark.With(xlsx.Input))
		scopeMark := xlsx.Cell{}
		if nfr.inScope {
			scopeMark = xlsx.Text(SelectedMark)
		}
		s.Set(exScope, row, scopeMark.With(xlsx.Input))
		s.Set(exKey, row, xlsx.Text(nfr.Key))
		s.Set(exSummary, row, xlsx.Text(nfr.Summary))
		s.Set(exDomain, row, xlsx.Text(strings.TrimSpace(nfr.Domain)))
		s.Set(exWeight, row, xlsx.Int(nfr.Weight))
		s.Set(exLinked, row, xlsx.FormulaNumber(
			"SUMPRODUCT(("+l.lk(lkKey)+"="+xlsx.Ref(exKey, row)+")*"+l.lk(lkLinked)+")",
			float64(nfr.linked)))
		s.Set(exMapping, row, xlsx.Text(nfr.NISTMapping))
		s.Set(exComments, row, xlsx.Text(nfr.comments()))
		s.Set(exDetails, row, xlsx.Text(nfr.AdditionalDetails))
		s.Set(exImplementation, row, xlsx.Text(nfr.Implementation))
		s.Set(exInScope, row, xlsx.FormulaNumber(
			"IF("+xlsx.Ref(exScope, row)+`="`+SelectedMark+`",1,0)`,
			flag(nfr.inScope)))
		exception := xlsx.Ref(exSel, row)
		s.Set(exSelected, row, xlsx.FormulaNumber(
			"IF(AND(OR("+exception+`="`+FullMark+`",`+exception+`="`+PartialMark+`"),`+xlsx.Ref(exInScope, row)+"=1),1,0)",
			flag(nfr.selected)))
		s.Set(exFactor, row, xlsx.FormulaNumber(
			"IF("+xlsx.Ref(exSelected, row)+"=1,IF("+exception+`="`+PartialMark+`",`+weightText(PartialFactor)+",1),0)",
			nfr.factor()))
		selectedCell := xlsx.Ref(exSelected, row)
		s.Set(exOrder, row, xlsx.FormulaNumber(
			"IF("+selectedCell+"=1,SUM("+xlsx.Col(exSelected)+"$"+strconv.Itoa(exFirstRow)+":"+selectedCell+"),0)",
			float64(nfr.order)))

		// What the requirement touches, over the rows of the Links sheet that
		// are its own and count as links.
		linkedTo := "(" + l.lk(lkKey) + "=" + xlsx.Ref(exKey, row) + ")*" + l.lk(lkLinked)
		touches := func(col int, carried string, touched bool) {
			s.Set(col, row, xlsx.FormulaNumber("IF(SUMPRODUCT("+linkedTo+"*"+carried+")>0,1,0)", flag(touched)))
		}
		rating := func(col int) string { return "(LEN(TRIM(" + l.lk(col) + "))>0)" }
		touches(exTouchC, rating(lkC), nfr.touchC)
		touches(exTouchI, rating(lkI), nfr.touchI)
		touches(exTouchA, rating(lkA), nfr.touchA)
		for t, threat := range m.threats {
			touches(exFirstTouchThreat+t, l.lk(lkFirstThreat+t), nfr.touchThreats[threat])
		}
	}
	if len(m.nfrs) == 0 {
		s.Set(exSel, exFirstRow, xlsx.Cell{}.With(xlsx.Input))
		s.Set(exScope, exFirstRow, xlsx.Cell{}.With(xlsx.Input))
	}

	s.AutoFilter(xlsx.Range(1, exHeaderRow, l.exLastCol(), l.exLast))
	s.ListValidation(xlsx.Range(exScope, exFirstRow, exScope, l.exLast), SelectedMark)
	s.ListValidation(xlsx.Range(exSel, exFirstRow, exSel, l.exLast), FullMark, PartialMark)
	s.Highlight(xlsx.Range(exSummary, exFirstRow, exImplementation, l.exLast),
		"$"+xlsx.Col(exSelected)+strconv.Itoa(exFirstRow)+"=1")
	s.TextNumbers(xlsx.Range(exKey, exFirstRow, exKey, l.exLast))
}

func writeSummary(s *xlsx.Sheet, m model, l layout) {
	s.Widths(36, 16, 16, 10, 16)
	r := m.result
	selected := l.ex(exSelected)
	factor := l.ex(exFactor)
	inScope := l.ex(exInScope)
	weights := l.ex(exWeight)
	excepted := l.ct(l.ctExcepted)

	label := func(row int, text string) { s.Set(1, row, xlsx.Text(text)) }
	section := func(row int, cells ...string) {
		for i := 0; i < smColumns; i++ {
			text := ""
			if i < len(cells) {
				text = cells[i]
			}
			s.Set(i+1, row, xlsx.Text(text).With(xlsx.Section))
		}
	}
	// weighted writes the weight that touches a rating or a threat: the
	// excepted requirements', the scope's, and the share. touch is the column
	// of the Exceptions sheet that marks the requirements touching it. The
	// Factor column is 1 for a full exception, a half for a partial one and 0
	// for the rest.
	weighted := func(row, touch int, w WeightedCoverage) {
		touches := l.ex(touch)
		s.Set(smColExcepted, row, xlsx.FormulaNumber("SUMPRODUCT("+factor+"*"+weights+"*"+touches+")", w.Excepted).With(xlsx.Left))
		s.Set(smColScope, row, xlsx.FormulaNumber("SUMPRODUCT("+inScope+"*"+weights+"*"+touches+")", float64(w.Scope)).With(xlsx.Left))
		share := 0.0
		if w.Scope > 0 {
			share = w.Excepted / float64(w.Scope)
		}
		s.Set(smColShare, row, xlsx.FormulaNumber(
			"IF("+xlsx.Ref(smColScope, row)+"=0,0,"+xlsx.Ref(smColExcepted, row)+"/"+xlsx.Ref(smColScope, row)+")",
			share).With(xlsx.Percent))
	}

	s.Set(1, 1, xlsx.Text("Exception set summary").With(xlsx.Title))
	s.Set(1, 2, xlsx.Text("Follows the Scope and Exception columns of the Exceptions sheet.").With(xlsx.Muted))

	section(smCatalog-1, "Scope and selection")
	label(smCatalog, "Security NFRs in the catalog")
	s.Set(2, smCatalog, xlsx.FormulaNumber("SUMPRODUCT((LEN("+l.ex(exKey)+")>0)*1)", float64(r.CatalogNFRs)).With(xlsx.Left))
	label(smScope, "Security NFRs in scope")
	s.Set(2, smScope, xlsx.FormulaNumber("SUM("+inScope+")", float64(r.ScopeNFRs)).With(xlsx.Left))
	label(smSelected, "Selected Security NFRs")
	s.Set(2, smSelected, xlsx.FormulaNumber("SUM("+selected+")", float64(r.SelectedNFRs)).With(xlsx.Left))
	label(smControls, "Linked Controls")
	s.Set(2, smControls, xlsx.FormulaNumber("SUM("+l.lk(lkNew)+")", float64(r.LinkedControls)).With(xlsx.Left))

	section(smC-1, "CIA coverage", "Excepted weight", "Scope weight", "Share", "Controls")
	rated := func(row int, text string, col, touch, count int, w WeightedCoverage) {
		label(row, text)
		weighted(row, touch, w)
		s.Set(smColCount, row, xlsx.FormulaNumber("SUMPRODUCT("+excepted+"*(LEN(TRIM("+l.ct(col)+"))>0))", float64(count)).With(xlsx.Left))
	}
	rated(smC, "Confidentiality (C)", ctC, exTouchC, r.Confidentiality, r.ConfidentialityWeight)
	rated(smI, "Integrity (I)", ctI, exTouchI, r.Integrity, r.IntegrityWeight)
	rated(smA, "Availability (A)", ctA, exTouchA, r.Availability, r.AvailabilityWeight)

	section(smFirstThreat-1, "Threat coverage", "Excepted weight", "Scope weight", "Share", "Controls")
	for i, threat := range r.Threats {
		row := smFirstThreat + i
		label(row, threat.Threat)
		weighted(row, exFirstTouchThreat+i, threat.Weight)
		s.Set(smColCount, row, xlsx.FormulaNumber("SUMPRODUCT("+excepted+"*"+l.ct(ctFirstThreat+i)+")", float64(threat.Controls)).With(xlsx.Left))
	}

	// The total weight is a minor figure: it says how much is excepted, not
	// what the exceptions touch.
	weightRow := l.smSelectedWeight()
	section(weightRow-1, "Total weight (minor)")
	label(weightRow, "Weight of the selection")
	s.Set(2, weightRow, xlsx.FormulaNumber("SUMPRODUCT("+factor+"*"+weights+")", r.SelectedWeight).With(xlsx.Left))
	label(l.smScopeWeight(), "Weight of the scope")
	s.Set(2, l.smScopeWeight(), xlsx.FormulaNumber("SUMPRODUCT("+inScope+"*"+weights+")", float64(r.ScopeWeight)).With(xlsx.Left))
	label(l.smShare(), "Share of the scope's weight")
	share := 0.0
	if r.ScopeWeight > 0 {
		share = r.SelectedWeight / float64(r.ScopeWeight)
	}
	s.Set(2, l.smShare(), xlsx.FormulaNumber(
		"IF("+xlsx.Ref(2, l.smScopeWeight())+"=0,0,"+xlsx.Ref(2, weightRow)+"/"+xlsx.Ref(2, l.smScopeWeight())+")",
		share).With(xlsx.Percent))
	label(l.smAverage(), "Average weight of a selected NFR")
	average := "IF(" + xlsx.Ref(2, smSelected) + `=0,"",ROUND(` + xlsx.Ref(2, weightRow) + "/" + xlsx.Ref(2, smSelected) + ",2))"
	if r.SelectedNFRs == 0 {
		s.Set(2, l.smAverage(), xlsx.FormulaText(average, "").With(xlsx.Decimal))
	} else {
		value := float64(roundHalfUp(r.SelectedWeight*100/float64(r.SelectedNFRs))) / 100
		s.Set(2, l.smAverage(), xlsx.FormulaNumber(average, value).With(xlsx.Decimal))
	}

	table := l.smWeightTable()
	section(table, "Weight", "In the scope", "In the selection")
	for i, count := range r.ByWeight {
		row := table + 1 + i
		s.Set(1, row, xlsx.Int(count.Weight).With(xlsx.Left))
		s.Set(2, row, xlsx.FormulaNumber("SUMPRODUCT("+inScope+"*("+weights+"="+xlsx.Ref(1, row)+"))", float64(count.Scope)).With(xlsx.Left))
		s.Set(3, row, xlsx.FormulaNumber("SUMPRODUCT("+selected+"*("+weights+"="+xlsx.Ref(1, row)+"))", float64(count.Selected)).With(xlsx.Left))
	}
}

// detailLine writes the counter in column A of a detail sheet and the source
// row in its last column, and reports whether the line is filled as written.
func detailLine(s *xlsx.Sheet, row, sourceCol int, count string, orderRange string, filled bool, sourceRow int) {
	number := row - detailHeaderRow
	counter := "IF(ROW()-" + strconv.Itoa(detailHeaderRow) + "<=" + count + ",ROW()-" + strconv.Itoa(detailHeaderRow) + `,"")`
	source := "IF(" + xlsx.Ref(1, row) + `="","",MATCH(` + xlsx.Ref(1, row) + "," + orderRange + ",0))"
	if filled {
		s.Set(1, row, xlsx.FormulaNumber(counter, float64(number)).With(xlsx.Center))
		s.Set(sourceCol, row, xlsx.FormulaNumber(source, float64(sourceRow)).With(xlsx.Muted))
		return
	}
	s.Set(1, row, xlsx.FormulaText(counter, "").With(xlsx.Center))
	s.Set(sourceCol, row, xlsx.FormulaText(source, "").With(xlsx.Muted))
}

// lookup reads column rng of the source sheet for a detail line; a blank
// value shows as fallback, as it does on the page.
func lookup(row, sourceCol int, rng, fallback string) string {
	source := "$" + xlsx.Col(sourceCol) + strconv.Itoa(row)
	value := "INDEX(" + rng + "," + source + ")"
	if fallback == "" {
		return "IF(" + source + `="","",` + value + `&"")`
	}
	return "IF(" + source + `="","",IF(LEN(TRIM(` + value + `))=0,"` + fallback + `",` + value + `&""))`
}

func orFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func writeNFRDetail(s *xlsx.Sheet, m model, l layout) {
	s.Widths(5, 7, 60, 30, 8, 10, 10, 22, 60, 60, 60, 7)
	s.Freeze(0, detailHeaderRow)
	s.FitPageWidth()
	s.Set(1, 1, xlsx.Text("Exception detail: the selected Security NFRs").With(xlsx.Title))
	// The long text is on one line: a spreadsheet does not make a row taller
	// when the text a formula gives it grows.
	s.Set(1, 2, xlsx.Text("Filled from the Exceptions sheet. A requirement appears here when it is in scope and its Exception cell is "+FullMark+" or "+PartialMark+
		". To read or print the long text in full, select the rows, turn on Wrap Text and AutoFit Row Height.").With(xlsx.Muted))

	headers := []string{"#", "Key", "Summary", "Domain", "Weight", "Exception", "Linked Controls",
		"NIST Mapping", "Comments", "Additional Details", "Implementation", "Row"}
	for i, header := range headers {
		s.Set(i+1, detailHeaderRow, xlsx.Text(header).With(xlsx.Header))
	}

	selected := make([]int, 0, len(m.nfrs))
	for i, nfr := range m.nfrs {
		if nfr.selected {
			selected = append(selected, i)
		}
	}
	type column struct {
		col      int
		source   int
		fallback string
		value    func(nfrRow) string
	}
	text := []column{
		{2, exKey, "", func(n nfrRow) string { return n.Key }},
		{3, exSummary, "No summary", func(n nfrRow) string { return n.Summary }},
		{4, exDomain, "N/A", func(n nfrRow) string { return strings.TrimSpace(n.Domain) }},
		{6, exSel, "", func(n nfrRow) string { return n.mark() }},
		{8, exMapping, "N/A", func(n nfrRow) string { return n.NISTMapping }},
		{9, exComments, "", func(n nfrRow) string { return n.comments() }},
		{10, exDetails, "N/A", func(n nfrRow) string { return n.AdditionalDetails }},
		{11, exImplementation, "N/A", func(n nfrRow) string { return n.Implementation }},
	}
	numbers := []struct {
		col    int
		source int
		value  func(nfrRow) int
	}{
		{5, exWeight, func(n nfrRow) int { return n.Weight }},
		{7, exLinked, func(n nfrRow) int { return n.linked }},
	}

	for i := 0; i < rowsOrOne(len(m.nfrs)); i++ {
		row := detailFirstRow + i
		filled := i < len(selected)
		at := 0
		if filled {
			at = selected[i]
		}
		detailLine(s, row, nfrDetailSource, summaryCell(smSelected), l.ex(exOrder), filled, at+1)
		for _, c := range text {
			value := ""
			if filled {
				value = orFallback(c.value(m.nfrs[at]), c.fallback)
			}
			s.Set(c.col, row, xlsx.FormulaText(lookup(row, nfrDetailSource, l.ex(c.source), c.fallback), value))
		}
		for _, c := range numbers {
			source := "$" + xlsx.Col(nfrDetailSource) + strconv.Itoa(row)
			formula := "IF(" + source + `="","",INDEX(` + l.ex(c.source) + "," + source + "))"
			if filled {
				s.Set(c.col, row, xlsx.FormulaNumber(formula, float64(c.value(m.nfrs[at]))).With(xlsx.Center))
			} else {
				s.Set(c.col, row, xlsx.FormulaText(formula, "").With(xlsx.Center))
			}
		}
	}
}

func rated(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return "TRUE"
}

func writeControlDetail(s *xlsx.Sheet, m model, l layout) {
	s.Widths(5, 9, 9, 60, 8, 12, 48, 8, 7, 7, 7, 46, 7)
	s.Freeze(0, detailHeaderRow)
	s.FitPageWidth()
	s.Set(1, 1, xlsx.Text("Exception detail: the linked controls of each selected Security NFR").With(xlsx.Title))
	s.Set(1, 2, xlsx.Text("One line for each control a selected requirement is linked to. TRUE means the control carries that rating.").With(xlsx.Muted))

	headers := []string{"#", "NFR Key", "NFR ID", "NFR Summary", "Weight", "Control ID", "Control Name", "Family",
		"C", "I", "A", "Threats", "Row"}
	for i, header := range headers {
		s.Set(i+1, detailHeaderRow, xlsx.Text(header).With(xlsx.Header))
	}

	linked := 0
	excepted := make([]int, 0, len(m.links))
	for i, link := range m.links {
		if link.linked {
			linked++
		}
		if link.excepted {
			excepted = append(excepted, i)
		}
	}
	weightOf := map[string]int{}
	for _, nfr := range m.nfrs {
		weightOf[nfr.Key] = nfr.Weight
	}

	for i := 0; i < rowsOrOne(linked); i++ {
		row := detailFirstRow + i
		filled := i < len(excepted)
		var link linkRow
		at := 0
		control := controlRow{}
		if filled {
			at = excepted[i]
			link = m.links[at]
			if index, ok := m.byID[link.control]; ok {
				control = m.controls[index]
			}
		}
		detailLine(s, row, detailSource, "MAX("+l.lk(lkOrder)+")", l.lk(lkOrder), filled, at+1)
		source := "$" + xlsx.Col(detailSource) + strconv.Itoa(row)

		text := func(col, from int, fallback, value string) {
			if !filled {
				value = ""
			} else {
				value = orFallback(value, fallback)
			}
			s.Set(col, row, xlsx.FormulaText(lookup(row, detailSource, l.lk(from), fallback), value))
		}
		text(2, lkKey, "", link.key)
		text(3, lkNFRID, "", strings.TrimSpace(link.NFRID))
		text(4, lkSummary, "No summary", link.NFRSummary)
		text(6, lkControl, "", link.control)
		text(7, lkControlName, "No control name", link.ControlName)
		text(8, lkFamily, "", link.ControlFamily)
		text(12, lkThreats, "None", threatText(control))

		weight := "IF(" + source + `="","",SUMPRODUCT((` + l.ex(exKey) + "=INDEX(" + l.lk(lkKey) + "," + source + "))*" + l.ex(exWeight) + "))"
		if filled {
			s.Set(5, row, xlsx.FormulaNumber(weight, float64(weightOf[link.key])).With(xlsx.Center))
		} else {
			s.Set(5, row, xlsx.FormulaText(weight, "").With(xlsx.Center))
		}

		rating := func(col, from int, value string) {
			formula := "IF(" + source + `="","",IF(LEN(TRIM(INDEX(` + l.lk(from) + "," + source + `)))=0,"-","TRUE"))`
			shown := ""
			if filled {
				shown = rated(value)
			}
			s.Set(col, row, xlsx.FormulaText(formula, shown).With(xlsx.Center))
		}
		rating(9, lkC, control.Confidentiality)
		rating(10, lkI, control.Integrity)
		rating(11, lkA, control.Availability)
	}
}

// threatText lists a control's threats in the order the summary does.
func threatText(control controlRow) string {
	names := make([]string, 0, len(control.threats))
	for threat := range control.threats {
		names = append(names, threat)
	}
	rank := map[string]int{}
	for i, threat := range threatDisplayOrder {
		rank[threat] = i + 1
	}
	sort.Slice(names, func(i, j int) bool {
		ri, rj := rank[names[i]], rank[names[j]]
		if (ri > 0) != (rj > 0) {
			return ri > 0
		}
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return strings.Join(names, ", ")
}

func writeLinks(s *xlsx.Sheet, m model, l layout) {
	widths := []float64{8, 9, 50, 14, 12, 44, 9, 9, 8, 10, 12, 7, 16, 16, 16, 40}
	headers := []string{"NFR Key", "NFR ID", "NFR Summary", "Mapping", "Control ID", "Control Name", "Family", "Matched",
		"Linked", "Excepted", "New control", "Order", "Confidentiality", "Integrity", "Availability", "Threats"}
	for _, threat := range m.threats {
		widths = append(widths, 12)
		headers = append(headers, threat)
	}
	s.Widths(widths...)
	s.Freeze(0, 1)
	for i, header := range headers {
		s.Set(i+1, 1, xlsx.Text(header).With(xlsx.Header))
	}
	first := strconv.Itoa(lkFirstRow)
	running := func(col, row int) string {
		return xlsx.Col(col) + "$" + first + ":" + xlsx.Ref(col, row)
	}

	for i, link := range m.links {
		row := lkFirstRow + i
		s.Set(lkKey, row, xlsx.Text(link.key))
		s.Set(lkNFRID, row, xlsx.Text(strings.TrimSpace(link.NFRID)))
		s.Set(lkSummary, row, xlsx.Text(link.NFRSummary))
		s.Set(lkMapping, row, xlsx.Text(link.MappingControlID))
		s.Set(lkControl, row, xlsx.Text(link.control))
		s.Set(lkControlName, row, xlsx.Text(link.ControlName))
		s.Set(lkFamily, row, xlsx.Text(link.ControlFamily))
		s.Set(lkMatched, row, xlsx.Number(flag(link.Matched)))

		key, control, matched := xlsx.Ref(lkKey, row), xlsx.Ref(lkControl, row), xlsx.Ref(lkMatched, row)
		s.Set(lkLinked, row, xlsx.FormulaNumber(
			"IF(AND("+matched+"=1,"+key+`<>"",`+control+`<>"",SUMPRODUCT((`+running(lkKey, row)+"="+key+")*("+
				running(lkControl, row)+"="+control+")*("+running(lkMatched, row)+"=1))=1),1,0)",
			flag(link.linked)))
		linked, excepted := xlsx.Ref(lkLinked, row), xlsx.Ref(lkExcepted, row)
		s.Set(lkExcepted, row, xlsx.FormulaNumber(
			"IF("+linked+"=1,SUMPRODUCT(("+l.ex(exKey)+"="+key+")*"+l.ex(exSelected)+"),0)",
			flag(link.excepted)))
		s.Set(lkNew, row, xlsx.FormulaNumber(
			"IF(AND("+excepted+"=1,SUMPRODUCT(("+running(lkControl, row)+"="+control+")*("+running(lkExcepted, row)+"=1))=1),1,0)",
			flag(link.isNew)))
		s.Set(lkOrder, row, xlsx.FormulaNumber(
			"IF("+excepted+"=1,SUM("+running(lkExcepted, row)+"),0)",
			float64(link.order)))

		found := controlRow{}
		if index, ok := m.byID[link.control]; ok && link.control != "" {
			found = m.controls[index]
		}
		read := func(col, from int, value string) {
			s.Set(col, row, xlsx.FormulaText(
				"IFERROR(INDEX("+l.ct(from)+",MATCH("+control+","+l.ct(ctID)+`,0))&"","")`,
				value))
		}
		read(lkC, ctC, found.Confidentiality)
		read(lkI, ctI, found.Integrity)
		read(lkA, ctA, found.Availability)
		read(lkThreats, ctThreats, threatText(found))
		for t, threat := range m.threats {
			s.Set(lkFirstThreat+t, row, xlsx.FormulaNumber(
				"IFERROR(INDEX("+l.ct(ctFirstThreat+t)+",MATCH("+control+","+l.ct(ctID)+",0)),0)",
				flag(found.threats[threat])))
		}
	}
	s.TextNumbers(xlsx.Range(lkKey, lkFirstRow, lkNFRID, l.lkLast))
	// What the application exported: the links are changed there, not here.
	s.Protect()
}

func writeControls(s *xlsx.Sheet, m model, l layout) {
	widths := []float64{12, 50, 9, 14, 22, 16, 16, 16, 40}
	headers := []string{"Control ID", "Name", "Family", "Type", "Baselines", "Confidentiality", "Integrity", "Availability", "Threats"}
	for _, threat := range m.threats {
		widths = append(widths, 14)
		headers = append(headers, threat)
	}
	widths = append(widths, 10)
	headers = append(headers, "Excepted")
	s.Widths(widths...)
	s.Freeze(2, 1)
	for i, header := range headers {
		s.Set(i+1, 1, xlsx.Text(header).With(xlsx.Header))
	}

	for i, control := range m.controls {
		row := ctFirstRow + i
		s.Set(ctID, row, xlsx.Text(control.id))
		s.Set(ctName, row, xlsx.Text(control.Name))
		s.Set(ctFamily, row, xlsx.Text(control.Family))
		s.Set(ctType, row, xlsx.Text(control.Type))
		s.Set(ctBaselines, row, xlsx.Text(strings.Join(control.Baselines, ", ")))
		s.Set(ctC, row, xlsx.Text(control.Confidentiality))
		s.Set(ctI, row, xlsx.Text(control.Integrity))
		s.Set(ctA, row, xlsx.Text(control.Availability))
		s.Set(ctThreats, row, xlsx.Text(threatText(control)))
		for t, threat := range m.threats {
			s.Set(ctFirstThreat+t, row, xlsx.Number(flag(control.threats[threat])))
		}
		s.Set(l.ctExcepted, row, xlsx.FormulaNumber(
			"IF(SUMPRODUCT(("+l.lk(lkControl)+"="+xlsx.Ref(ctID, row)+")*"+l.lk(lkExcepted)+")>0,1,0)",
			flag(control.excepted)))
	}
	s.AutoFilter(xlsx.Range(1, 1, l.ctExcepted, l.ctLast))
	// What the application exported: the catalog is changed there, not here.
	s.Protect()
}

func roundHalfUp(value float64) int {
	return int(value + 0.5)
}
