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

// SelectedMark is what the Except column holds for a requirement in the
// exception set.
const SelectedMark = "Yes"

// The Exceptions sheet. Rows 1-5 are the title and the live summary lines.
const (
	exSel = iota + 1
	exKey
	exID
	exSummary
	exDomain
	exType
	exWeight
	exLinked
	exMapping
	exDescription
	exDetails
	exImplementation
	exSelected
	exOrder

	exHeaderRow = 6
	exFirstRow  = 7
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
	smSelected    = 5
	smControls    = 6
	smC           = 9
	smI           = 10
	smA           = 11
	smFirstThreat = 14
)

const (
	detailHeaderRow = 4
	detailFirstRow  = 5
	// The last column of both detail sheets: the row of the source sheet that
	// the line is read from.
	detailSource = 13
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
	// exception set. Keys that are not in the catalog are ignored.
	Selected    []string
	GeneratedAt time.Time
}

type ThreatCount struct {
	Threat   string
	Controls int
}

// Result is what the workbook shows for the selection it was written with.
type Result struct {
	SelectedNFRs    int
	LinkedControls  int
	Confidentiality int
	Integrity       int
	Availability    int
	Threats         []ThreatCount
	SelectedWeight  int
	CatalogWeight   int
}

// CIALine and ThreatLine are the two summary lines exactly as the Exceptions
// page words them.
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

type nfrRow struct {
	securitynfr.NFR
	selected bool
	order    int
	linked   int
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
func evaluate(data Data, selected []string) model {
	m := model{byID: map[string]int{}}

	chosen := map[string]bool{}
	for _, key := range selected {
		chosen[strings.TrimSpace(key)] = true
	}

	seenKey := map[string]bool{}
	for _, nfr := range data.NFRs {
		nfr.Key = strings.TrimSpace(nfr.Key)
		if nfr.Key == "" || seenKey[nfr.Key] {
			continue
		}
		seenKey[nfr.Key] = true
		m.nfrs = append(m.nfrs, nfrRow{NFR: nfr, selected: chosen[nfr.Key]})
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
		m.result.Threats = append(m.result.Threats, ThreatCount{Threat: threat, Controls: count})
	}

	rank := 0
	for i := range m.nfrs {
		nfr := &m.nfrs[i]
		m.result.CatalogWeight += nfr.Weight
		if !nfr.selected {
			continue
		}
		rank++
		nfr.order = rank
		m.result.SelectedNFRs++
		m.result.SelectedWeight += nfr.Weight
	}
	return m
}

// Evaluate is the result for a selection without the workbook.
func Evaluate(data Data, selected []string) Result { return evaluate(data, selected).result }

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
func (l layout) smCatalogWeight() int  { return l.smSelectedWeight() + 1 }
func (l layout) smShare() int          { return l.smSelectedWeight() + 2 }
func (l layout) smAverage() int        { return l.smSelectedWeight() + 3 }
func (l layout) smWeightTable() int    { return l.smSelectedWeight() + 5 }
func (l layout) smCIALine() int {
	return l.smWeightTable() + securitynfr.MaxWeight - securitynfr.MinWeight + 4
}
func (l layout) smThreatLine() int { return l.smCIALine() + 1 }
func (l layout) smThreatList() int { return l.smCIALine() + 2 }

// Build writes the workbook for data with the given requirements selected.
func Build(data Data, opts Options) (*xlsx.Workbook, Result) {
	m := evaluate(data, opts.Selected)
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
	line("2. Choose "+SelectedMark+" in the Except column (A) for every requirement in the exception set. Clear the cell to take one out.", xlsx.Wrap)
	line("3. The lines at the top of the sheet, and the Summary sheet, change as you go: the controls the selection is linked to, how many of them carry a confidentiality, integrity or availability rating, the threats they address, and the weight of the selection.", xlsx.Wrap)
	line("4. Selected NFRs and Selected controls are the detail view of the selection, ready to print.", xlsx.Wrap)
	row++

	line("The page's buttons, here", xlsx.Section)
	line("Search and the domain filter: the filter buttons on the header row of the Exceptions sheet.", xlsx.Wrap)
	line("Select Filtered: filter the rows, select the visible cells of column A, type "+SelectedMark+" and press Ctrl+Enter.", xlsx.Wrap)
	line("Clear Filtered: filter the rows, select the visible cells of column A and press Delete.", xlsx.Wrap)
	line("Clear All: clear the filter, select column A from row "+strconv.Itoa(exFirstRow)+" down and press Delete.", xlsx.Wrap)
	line("Open Details and Print Details: the Selected NFRs and Selected controls sheets.", xlsx.Wrap)
	row++

	line("The sheets", xlsx.Section)
	line(SheetExceptions+": the Security NFR catalog with its weights, and the Except column. Columns M and N are working columns.", xlsx.Wrap)
	line(SheetSummary+": the selection, the linked controls, the C / I / A counts, the threat coverage and the weights.", xlsx.Wrap)
	line(SheetNFRDetail+": each selected requirement in full.", xlsx.Wrap)
	line(SheetControlDetail+": each selected requirement's linked controls, with their ratings and threats.", xlsx.Wrap)
	line(SheetLinks+": which NIST control each requirement's NIST mapping resolves to. Columns I to L are working columns.", xlsx.Wrap)
	line(SheetControls+": the NIST controls, their ratings and the threats each addresses. The last column marks the controls in the exception set.", xlsx.Wrap)
	row++

	line("How the numbers are worked out", xlsx.Section)
	line("Linked Controls: the distinct controls that the selected requirements are linked to. A link counts when the requirement's NIST mapping matched a control.", xlsx.Wrap)
	line("C, I and A: how many of those controls have a confidentiality, integrity or availability rating.", xlsx.Wrap)
	line("Threat coverage: for each threat, how many of those controls address it.", xlsx.Wrap)
	line("Weight: each requirement's weight is 1 (lowest) to 5 (highest). The weight of the selection is the sum of the weights of the selected requirements; its share is that sum over the sum for the whole catalog. The Exceptions page shows no weights: these figures are in the workbook only.", xlsx.Wrap)
	line("The data can be edited. Change a weight, a rating or a link and the numbers follow.", xlsx.Wrap)
}

func writeExceptions(s *xlsx.Sheet, m model, l layout) {
	s.Widths(9, 7, 9, 62, 30, 14, 8, 10, 22, 50, 50, 50, 9, 7)
	s.Freeze(exSummary, exHeaderRow)

	r := m.result
	share := 0
	if r.CatalogWeight > 0 {
		share = roundHalfUp(float64(r.SelectedWeight) * 100 / float64(r.CatalogWeight))
	}
	s.Set(1, 1, xlsx.Text("Exceptions").With(xlsx.Title))
	s.Set(1, 2, xlsx.FormulaText(
		`"Selected Security NFRs: "&`+summaryCell(smSelected)+`&" | "&`+xlsx.SheetRef(SheetSummary, xlsx.Abs(1, l.smCIALine())),
		"Selected Security NFRs: "+strconv.Itoa(r.SelectedNFRs)+" | "+r.CIALine()).With(xlsx.Label))
	s.Set(1, 3, xlsx.FormulaText(
		xlsx.SheetRef(SheetSummary, xlsx.Abs(1, l.smThreatLine()))+`&""`,
		r.ThreatLine()).With(xlsx.Label))
	s.Set(1, 4, xlsx.FormulaText(
		`"Weight of the selection: "&`+summaryCell(l.smSelectedWeight())+`&" of "&`+summaryCell(l.smCatalogWeight())+
			`&" ("&ROUND(`+summaryCell(l.smShare())+`*100,0)&"%)"`,
		"Weight of the selection: "+strconv.Itoa(r.SelectedWeight)+" of "+strconv.Itoa(r.CatalogWeight)+" ("+strconv.Itoa(share)+"%)").With(xlsx.Label))
	s.Set(1, 5, xlsx.Text("Choose "+SelectedMark+" in column A to put a Security NFR in the exception set. The filter buttons on the header row search and filter by domain.").With(xlsx.Muted))

	headers := []string{"Except", "Key", "NFR ID", "Summary", "Domain", "Issue Type", "Weight", "Linked Controls",
		"NIST Mapping", "Description", "Additional Details", "Implementation", "Selected", "Order"}
	for i, header := range headers {
		s.Set(i+1, exHeaderRow, xlsx.Text(header).With(xlsx.Header))
	}

	for i, nfr := range m.nfrs {
		row := exFirstRow + i
		mark := xlsx.Cell{}
		if nfr.selected {
			mark = xlsx.Text(SelectedMark)
		}
		s.Set(exSel, row, mark.With(xlsx.Input))
		s.Set(exKey, row, xlsx.Text(nfr.Key))
		s.Set(exID, row, xlsx.Text(strings.TrimSpace(nfr.ID)))
		s.Set(exSummary, row, xlsx.Text(nfr.Summary))
		s.Set(exDomain, row, xlsx.Text(strings.TrimSpace(nfr.Domain)))
		s.Set(exType, row, xlsx.Text(nfr.IssueType))
		s.Set(exWeight, row, xlsx.Int(nfr.Weight))
		s.Set(exLinked, row, xlsx.FormulaNumber(
			"SUMPRODUCT(("+l.lk(lkKey)+"="+xlsx.Ref(exKey, row)+")*"+l.lk(lkLinked)+")",
			float64(nfr.linked)))
		s.Set(exMapping, row, xlsx.Text(nfr.NISTMapping))
		s.Set(exDescription, row, xlsx.Text(nfr.Description))
		s.Set(exDetails, row, xlsx.Text(nfr.AdditionalDetails))
		s.Set(exImplementation, row, xlsx.Text(nfr.Implementation))
		s.Set(exSelected, row, xlsx.FormulaNumber(
			"IF("+xlsx.Ref(exSel, row)+`="`+SelectedMark+`",1,0)`,
			flag(nfr.selected)))
		selectedCell := xlsx.Ref(exSelected, row)
		s.Set(exOrder, row, xlsx.FormulaNumber(
			"IF("+selectedCell+"=1,SUM("+xlsx.Col(exSelected)+"$"+strconv.Itoa(exFirstRow)+":"+selectedCell+"),0)",
			float64(nfr.order)))
	}
	if len(m.nfrs) == 0 {
		s.Set(exSel, exFirstRow, xlsx.Cell{}.With(xlsx.Input))
	}

	s.AutoFilter(xlsx.Range(1, exHeaderRow, exOrder, l.exLast))
	s.ListValidation(xlsx.Range(exSel, exFirstRow, exSel, l.exLast), SelectedMark)
	s.Highlight(xlsx.Range(exKey, exFirstRow, exImplementation, l.exLast),
		"$"+xlsx.Col(exSel)+strconv.Itoa(exFirstRow)+`="`+SelectedMark+`"`)
	s.TextNumbers(xlsx.Range(exKey, exFirstRow, exID, l.exLast))
}

func writeSummary(s *xlsx.Sheet, m model, l layout) {
	s.Widths(36, 16, 16)
	r := m.result
	selected := l.ex(exSelected)
	weights := l.ex(exWeight)
	excepted := l.ct(l.ctExcepted)

	label := func(row int, text string) { s.Set(1, row, xlsx.Text(text)) }
	section := func(row int, cells ...string) {
		for i := 0; i < 3; i++ {
			text := ""
			if i < len(cells) {
				text = cells[i]
			}
			s.Set(i+1, row, xlsx.Text(text).With(xlsx.Section))
		}
	}

	s.Set(1, 1, xlsx.Text("Exception set summary").With(xlsx.Title))
	s.Set(1, 2, xlsx.Text("Follows the Except column of the Exceptions sheet.").With(xlsx.Muted))

	section(smSelected-1, "Selection")
	label(smSelected, "Selected Security NFRs")
	s.Set(2, smSelected, xlsx.FormulaNumber("SUM("+selected+")", float64(r.SelectedNFRs)).With(xlsx.Left))
	label(smControls, "Linked Controls")
	s.Set(2, smControls, xlsx.FormulaNumber("SUM("+l.lk(lkNew)+")", float64(r.LinkedControls)).With(xlsx.Left))

	section(smC-1, "CIA coverage", "Controls")
	rated := func(row int, text string, col int, value int) {
		label(row, text)
		s.Set(2, row, xlsx.FormulaNumber("SUMPRODUCT("+excepted+"*(LEN(TRIM("+l.ct(col)+"))>0))", float64(value)).With(xlsx.Left))
	}
	rated(smC, "Confidentiality (C)", ctC, r.Confidentiality)
	rated(smI, "Integrity (I)", ctI, r.Integrity)
	rated(smA, "Availability (A)", ctA, r.Availability)

	section(smFirstThreat-1, "Threat coverage", "Controls")
	threatList := ""
	for i, threat := range r.Threats {
		row := smFirstThreat + i
		label(row, threat.Threat)
		s.Set(2, row, xlsx.FormulaNumber("SUMPRODUCT("+excepted+"*"+l.ct(ctFirstThreat+i)+")", float64(threat.Controls)).With(xlsx.Left))
		if threatList != "" {
			threatList += "&"
		}
		threatList += "IF(" + xlsx.Ref(2, row) + ">0," + xlsx.Ref(1, row) + `&": "&` + xlsx.Ref(2, row) + `&" | ","")`
	}

	weightRow := l.smSelectedWeight()
	section(weightRow-1, "Weights")
	label(weightRow, "Weight of the selection")
	s.Set(2, weightRow, xlsx.FormulaNumber("SUMPRODUCT("+selected+"*"+weights+")", float64(r.SelectedWeight)).With(xlsx.Left))
	label(l.smCatalogWeight(), "Weight of the whole catalog")
	s.Set(2, l.smCatalogWeight(), xlsx.FormulaNumber("SUM("+weights+")", float64(r.CatalogWeight)).With(xlsx.Left))
	label(l.smShare(), "Share of the catalog's weight")
	share := 0.0
	if r.CatalogWeight > 0 {
		share = float64(r.SelectedWeight) / float64(r.CatalogWeight)
	}
	s.Set(2, l.smShare(), xlsx.FormulaNumber(
		"IF("+xlsx.Ref(2, l.smCatalogWeight())+"=0,0,"+xlsx.Ref(2, weightRow)+"/"+xlsx.Ref(2, l.smCatalogWeight())+")",
		share).With(xlsx.Percent))
	label(l.smAverage(), "Average weight of a selected NFR")
	average := "IF(" + xlsx.Ref(2, smSelected) + `=0,"",ROUND(` + xlsx.Ref(2, weightRow) + "/" + xlsx.Ref(2, smSelected) + ",2))"
	if r.SelectedNFRs == 0 {
		s.Set(2, l.smAverage(), xlsx.FormulaText(average, "").With(xlsx.Decimal))
	} else {
		value := float64(roundHalfUp(float64(r.SelectedWeight)*100/float64(r.SelectedNFRs))) / 100
		s.Set(2, l.smAverage(), xlsx.FormulaNumber(average, value).With(xlsx.Decimal))
	}

	table := l.smWeightTable()
	section(table, "Weight", "In the catalog", "In the selection")
	for weight := securitynfr.MinWeight; weight <= securitynfr.MaxWeight; weight++ {
		row := table + 1 + weight - securitynfr.MinWeight
		inCatalog, inSelection := 0, 0
		for _, nfr := range m.nfrs {
			if nfr.Weight != weight {
				continue
			}
			inCatalog++
			if nfr.selected {
				inSelection++
			}
		}
		s.Set(1, row, xlsx.Int(weight).With(xlsx.Left))
		s.Set(2, row, xlsx.FormulaNumber("COUNTIF("+weights+","+xlsx.Ref(1, row)+")", float64(inCatalog)).With(xlsx.Left))
		s.Set(3, row, xlsx.FormulaNumber("SUMPRODUCT("+selected+"*("+weights+"="+xlsx.Ref(1, row)+"))", float64(inSelection)).With(xlsx.Left))
	}

	section(l.smCIALine()-1, "As the Exceptions page words it")
	s.Set(1, l.smCIALine(), xlsx.FormulaText(
		`"Linked Controls: "&`+xlsx.Ref(2, smControls)+`&" | C: "&`+xlsx.Ref(2, smC)+`&" | I: "&`+xlsx.Ref(2, smI)+`&" | A: "&`+xlsx.Ref(2, smA),
		r.CIALine()))
	list := xlsx.Ref(2, l.smThreatList())
	s.Set(1, l.smThreatLine(), xlsx.FormulaText(
		`"Threat coverage: "&IF(LEN(`+list+`)=0,"no linked threat mappings",LEFT(`+list+`,LEN(`+list+`)-3))`,
		r.ThreatLine()))
	raw := ""
	for _, threat := range r.Threats {
		if threat.Controls > 0 {
			raw += threat.Threat + ": " + strconv.Itoa(threat.Controls) + " | "
		}
	}
	s.Set(1, l.smThreatList(), xlsx.Text("Threat list (working cell)").With(xlsx.Muted))
	s.Set(2, l.smThreatList(), xlsx.FormulaText(threatList, raw).With(xlsx.Muted))
}

// detailLine writes the counter in column A of a detail sheet and the source
// row in its last column, and reports whether the line is filled as written.
func detailLine(s *xlsx.Sheet, row int, count string, orderRange string, filled bool, sourceRow int) {
	number := row - detailHeaderRow
	counter := "IF(ROW()-" + strconv.Itoa(detailHeaderRow) + "<=" + count + ",ROW()-" + strconv.Itoa(detailHeaderRow) + `,"")`
	source := "IF(" + xlsx.Ref(1, row) + `="","",MATCH(` + xlsx.Ref(1, row) + "," + orderRange + ",0))"
	if filled {
		s.Set(1, row, xlsx.FormulaNumber(counter, float64(number)).With(xlsx.Center))
		s.Set(detailSource, row, xlsx.FormulaNumber(source, float64(sourceRow)).With(xlsx.Muted))
		return
	}
	s.Set(1, row, xlsx.FormulaText(counter, "").With(xlsx.Center))
	s.Set(detailSource, row, xlsx.FormulaText(source, "").With(xlsx.Muted))
}

// lookup reads column rng of the source sheet for a detail line; a blank
// value shows as fallback, as it does on the page.
func lookup(row int, rng, fallback string) string {
	source := "$" + xlsx.Col(detailSource) + strconv.Itoa(row)
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
	s.Widths(5, 7, 9, 60, 30, 12, 8, 10, 22, 60, 60, 60, 7)
	s.Freeze(0, detailHeaderRow)
	s.FitPageWidth()
	s.Set(1, 1, xlsx.Text("Exception detail: the selected Security NFRs").With(xlsx.Title))
	// The long text is on one line: a spreadsheet does not make a row taller
	// when the text a formula gives it grows.
	s.Set(1, 2, xlsx.Text("Filled from the Exceptions sheet. A requirement appears here when its Except cell is "+SelectedMark+
		". To read or print the long text in full, select the rows, turn on Wrap Text and AutoFit Row Height.").With(xlsx.Muted))

	headers := []string{"#", "Key", "NFR ID", "Summary", "Domain", "Issue Type", "Weight", "Linked Controls",
		"NIST Mapping", "Description", "Additional Details", "Implementation", "Row"}
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
		{3, exID, "", func(n nfrRow) string { return strings.TrimSpace(n.ID) }},
		{4, exSummary, "No summary", func(n nfrRow) string { return n.Summary }},
		{5, exDomain, "N/A", func(n nfrRow) string { return strings.TrimSpace(n.Domain) }},
		{6, exType, "N/A", func(n nfrRow) string { return n.IssueType }},
		{9, exMapping, "N/A", func(n nfrRow) string { return n.NISTMapping }},
		{10, exDescription, "N/A", func(n nfrRow) string { return n.Description }},
		{11, exDetails, "N/A", func(n nfrRow) string { return n.AdditionalDetails }},
		{12, exImplementation, "N/A", func(n nfrRow) string { return n.Implementation }},
	}
	numbers := []struct {
		col    int
		source int
		value  func(nfrRow) int
	}{
		{7, exWeight, func(n nfrRow) int { return n.Weight }},
		{8, exLinked, func(n nfrRow) int { return n.linked }},
	}

	for i := 0; i < rowsOrOne(len(m.nfrs)); i++ {
		row := detailFirstRow + i
		filled := i < len(selected)
		at := 0
		if filled {
			at = selected[i]
		}
		detailLine(s, row, summaryCell(smSelected), l.ex(exOrder), filled, at+1)
		for _, c := range text {
			value := ""
			if filled {
				value = orFallback(c.value(m.nfrs[at]), c.fallback)
			}
			s.Set(c.col, row, xlsx.FormulaText(lookup(row, l.ex(c.source), c.fallback), value))
		}
		for _, c := range numbers {
			source := "$" + xlsx.Col(detailSource) + strconv.Itoa(row)
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
		detailLine(s, row, "MAX("+l.lk(lkOrder)+")", l.lk(lkOrder), filled, at+1)
		source := "$" + xlsx.Col(detailSource) + strconv.Itoa(row)

		text := func(col, from int, fallback, value string) {
			if !filled {
				value = ""
			} else {
				value = orFallback(value, fallback)
			}
			s.Set(col, row, xlsx.FormulaText(lookup(row, l.lk(from), fallback), value))
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
	s.Widths(8, 9, 50, 14, 12, 44, 9, 9, 8, 10, 12, 7, 16, 16, 16, 40)
	s.Freeze(0, 1)
	headers := []string{"NFR Key", "NFR ID", "NFR Summary", "Mapping", "Control ID", "Control Name", "Family", "Matched",
		"Linked", "Excepted", "New control", "Order", "Confidentiality", "Integrity", "Availability", "Threats"}
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
	}
	s.TextNumbers(xlsx.Range(lkKey, lkFirstRow, lkNFRID, l.lkLast))
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
}

func roundHalfUp(value float64) int {
	return int(value + 0.5)
}
