package exceptionbook

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"grc/internal/xlsx"
)

// Regulation coverage is otherwise out of scope; this is the owner's decision
// of 2026-11-02. It reads what the catalog already holds, the References text
// of each control's appendix, and stores nothing.

const (
	doraRegulation     = "DORA"
	rtsRiskManagement  = "RTS on ICT risk management framework"
	referencesField    = "References"
	doraRankArticle    = 0
	doraRankSection    = 1
	doraRankChapter    = 2
	doraRankRegulation = 3
)

var (
	referenceSeparator = regexp.MustCompile(`[;\r\n]+`)
	referenceFramework = regexp.MustCompile(`\b(?:DORA|RTS|PCI DSS|SWIFT)\b`)
	referenceLabel     = regexp.MustCompile(`^[A-Za-z][\w ]*:`)
	rtsCitationStart   = regexp.MustCompile(`,?\s*\b(?:Article|Chapter|Section)\s+[\dIVXLC]`)
	articleCitation    = regexp.MustCompile(`^Article\s+(\d+)\b[,\s]*(.*)$`)
	sectionCitation    = regexp.MustCompile(`^Section\s+(\d+)\b[,\s]*(.*)$`)
	chapterCitation    = regexp.MustCompile(`^Chapter\s+([IVXLC]+)\b\s*(?:\(Articles?\s*(\d+)\s*[–-]\s*(\d+)\))?[,\s]*(.*)$`)
)

// doraCitation is one thing a control's References cite.
type doraCitation struct {
	regulation string
	// reference is "Article 6", "Section 7" or "Chapter II"; empty when the
	// text names only the regulation.
	reference string
	// articles is a chapter's range, "Articles 5-16", where the text gives it.
	articles string
	title    string
	rank     int
	number   int
}

func (c doraCitation) id() string { return c.regulation + "|" + c.reference }

// parseReferences reads the DORA and RTS citations out of a References text.
// A line that starts with its article and no regulation continues the one
// above it. An RTS article is not the DORA article of the same number, so the
// regulation is part of what a citation is.
func parseReferences(text string) []doraCitation {
	var found []doraCitation
	seen := map[string]bool{}
	add := func(citation doraCitation) {
		if !seen[citation.id()] {
			seen[citation.id()] = true
			found = append(found, citation)
		}
	}
	regulation := ""
	for _, piece := range referenceSeparator.Split(text, -1) {
		for _, part := range referenceParts(piece) {
			rest := part
			switch {
			case strings.HasPrefix(part, "DORA"):
				regulation = doraRegulation
				rest = strings.TrimLeft(strings.TrimPrefix(part, "DORA"), ": ")
			case strings.HasPrefix(part, "RTS"):
				name := part
				rest = ""
				if at := rtsCitationStart.FindStringIndex(part); at != nil {
					name, rest = part[:at[0]], strings.TrimLeft(part[at[0]:], ", ")
				}
				regulation = rtsName(name)
				if rest == "" {
					add(doraCitation{regulation: regulation, rank: doraRankRegulation})
					continue
				}
			case referenceLabel.MatchString(part):
				regulation = ""
				continue
			}
			if regulation == "" {
				continue
			}
			if citation, ok := parseCitation(rest); ok {
				citation.regulation = regulation
				add(citation)
			}
		}
	}
	return found
}

// referenceParts cuts a line before each framework it names: one line can run
// an RTS citation straight into a PCI DSS one.
func referenceParts(piece string) []string {
	starts := []int{0}
	for _, at := range referenceFramework.FindAllStringIndex(piece, -1) {
		if at[0] > 0 {
			starts = append(starts, at[0])
		}
	}
	parts := make([]string, 0, len(starts))
	for i, start := range starts {
		end := len(piece)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if part := strings.TrimSpace(piece[start:end]); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// rtsName is the one name of an RTS the catalog writes in several ways.
func rtsName(name string) string {
	name = strings.Trim(name, " .,:")
	if strings.Contains(strings.ToLower(name), "risk management") {
		return rtsRiskManagement
	}
	return name
}

func parseCitation(text string) (doraCitation, bool) {
	title := func(raw string) string { return strings.Trim(raw, " .,;:") }
	if match := articleCitation.FindStringSubmatch(text); match != nil {
		number, _ := strconv.Atoi(match[1])
		return doraCitation{reference: "Article " + match[1], title: title(match[2]), rank: doraRankArticle, number: number}, true
	}
	if match := sectionCitation.FindStringSubmatch(text); match != nil {
		number, _ := strconv.Atoi(match[1])
		return doraCitation{reference: "Section " + match[1], title: title(match[2]), rank: doraRankSection, number: number}, true
	}
	if match := chapterCitation.FindStringSubmatch(text); match != nil {
		citation := doraCitation{reference: "Chapter " + match[1], title: title(match[4]), rank: doraRankChapter, number: romanValue(match[1])}
		if match[2] != "" {
			citation.articles = "Articles " + match[2] + "-" + match[3]
		}
		return citation, true
	}
	return doraCitation{}, false
}

func romanValue(roman string) int {
	values := map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100}
	total := 0
	for i := 0; i < len(roman); i++ {
		if i+1 < len(roman) && values[roman[i]] < values[roman[i+1]] {
			total -= values[roman[i]]
		} else {
			total += values[roman[i]]
		}
	}
	return total
}

// DORARow is one article, or other citation, that the catalog's links reach.
type DORARow struct {
	Regulation string
	// Reference is "Article 6" or "Chapter II (Articles 5-16)"; empty when the
	// References name only the regulation.
	Reference string
	Title     string
	RTS       bool
	// NFRsInScope and Exceptions count Security NFRs linked to a control
	// that cites this, each once however many such controls it has. A
	// partial exception counts as one.
	NFRsInScope int
	Exceptions  int
	// ControlsInScope counts the distinct controls that cite this and
	// that an NFR in scope is linked to.
	ControlsInScope int
	// MissedControls counts the distinct controls that cite this and
	// that an exception is linked to.
	MissedControls int
}

// DORAReport is what the DORA Report page and the DORA sheet show.
type DORAReport struct {
	ScopeNFRs          int
	Exceptions         int
	DORAInScope        int
	DORAWithExceptions int
	RTSInScope         int
	RTSWithExceptions  int
	// Rows has the citations that an NFR in scope reaches, DORA first.
	Rows []DORARow
}

// Summary is the report's figures, as the page lays them out.
func (r DORAReport) Summary() SummarySection {
	count := func(n int) []string { return []string{strconv.Itoa(n)} }
	return SummarySection{Title: "Scope and selection", Rows: []SummaryRow{
		{Label: "Security NFRs in scope", Values: count(r.ScopeNFRs)},
		{Label: "Exceptions", Values: count(r.Exceptions)},
		{Label: "DORA references in scope", Values: count(r.DORAInScope)},
		{Label: "DORA references with exceptions", Values: count(r.DORAWithExceptions)},
		{Label: "RTS references in scope", Values: count(r.RTSInScope)},
		{Label: "RTS references with exceptions", Values: count(r.RTSWithExceptions)},
	}}
}

// Line is the Summary's figures as the DORA sheet words them.
func (r DORAReport) Line() string {
	return "Security NFRs in scope: " + strconv.Itoa(r.ScopeNFRs) +
		" | Exceptions: " + strconv.Itoa(r.Exceptions) +
		" | DORA references: " + strconv.Itoa(r.DORAInScope) + " in scope, " + strconv.Itoa(r.DORAWithExceptions) + " with exceptions" +
		" | RTS references: " + strconv.Itoa(r.RTSInScope) + " in scope, " + strconv.Itoa(r.RTSWithExceptions) + " with exceptions"
}

// doraPair is an NFR, or a control, and a citation it reaches: pairs[i]
// names rows[citation]. The workbook counts over these so that the scope and
// the exceptions can change there.
type doraPair struct {
	name     string
	citation int
	// inScope and excepted are what the workbook's formulas work out.
	inScope, excepted bool
}

type doraModel struct {
	// rows holds every citation a link reaches, in or out of scope: the scope
	// can change in the workbook. report.Rows is those in scope.
	rows     []DORARow
	nfrPairs []doraPair
	controls []doraPair
	// reach is each control that cites something, with each NFR linked to
	// it: what says in the workbook whether an NFR in scope reaches it.
	reach  []doraReach
	report DORAReport
}

type doraReach struct {
	control, key string
	inScope      bool
}

// doraOf works the report out from an evaluated exercise.
func doraOf(m model) doraModel {
	titles := map[string]map[string]int{}
	articles := map[string]string{}
	byControl := map[string][]doraCitation{}
	for _, control := range m.controls {
		text, _ := control.Appendix[referencesField].(string)
		citations := parseReferences(text)
		byControl[control.id] = citations
		for _, citation := range citations {
			if titles[citation.id()] == nil {
				titles[citation.id()] = map[string]int{}
			}
			if citation.title != "" {
				titles[citation.id()][citation.title]++
			}
			if citation.articles != "" {
				articles[citation.id()] = citation.articles
			}
		}
	}

	nfrs := map[string]nfrRow{}
	for _, nfr := range m.nfrs {
		nfrs[nfr.Key] = nfr
	}
	type reached struct {
		name string
		id   string
	}
	citations := map[string]doraCitation{}
	var nfrLinks, controlLinks []reached
	var reach []doraReach
	controlInScope := map[string]bool{}
	seen := map[reached]bool{}
	note := func(list *[]reached, pair reached) {
		if !seen[pair] {
			seen[pair] = true
			*list = append(*list, pair)
		}
	}
	for _, link := range m.links {
		nfr, known := nfrs[link.key]
		if !known || !link.linked {
			continue
		}
		if len(byControl[link.control]) > 0 {
			reach = append(reach, doraReach{control: link.control, key: link.key, inScope: nfr.inScope})
			controlInScope[link.control] = controlInScope[link.control] || nfr.inScope
		}
		for _, citation := range byControl[link.control] {
			citations[citation.id()] = citation
			note(&nfrLinks, reached{"nfr|" + link.key, citation.id()})
			note(&controlLinks, reached{"control|" + link.control, citation.id()})
		}
	}

	ordered := make([]doraCitation, 0, len(citations))
	for _, citation := range citations {
		ordered = append(ordered, citation)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if (a.regulation == doraRegulation) != (b.regulation == doraRegulation) {
			return a.regulation == doraRegulation
		}
		if a.regulation != b.regulation {
			return a.regulation < b.regulation
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return a.number < b.number
	})

	d := doraModel{reach: reach}
	index := map[string]int{}
	for i, citation := range ordered {
		index[citation.id()] = i
		reference := citation.reference
		if span := articles[citation.id()]; span != "" {
			reference += " (" + span + ")"
		}
		d.rows = append(d.rows, DORARow{
			Regulation: citation.regulation,
			Reference:  reference,
			Title:      commonestTitle(titles[citation.id()]),
			RTS:        citation.regulation != doraRegulation,
		})
	}

	excepted := map[string]bool{}
	for _, control := range m.controls {
		excepted[control.id] = control.excepted
	}
	for _, pair := range nfrLinks {
		key := strings.TrimPrefix(pair.name, "nfr|")
		nfr, at := nfrs[key], index[pair.id]
		d.nfrPairs = append(d.nfrPairs, doraPair{name: key, citation: at, inScope: nfr.inScope, excepted: nfr.selected})
		if nfr.inScope {
			d.rows[at].NFRsInScope++
		}
		if nfr.selected {
			d.rows[at].Exceptions++
		}
	}
	for _, pair := range controlLinks {
		id := strings.TrimPrefix(pair.name, "control|")
		at := index[pair.id]
		d.controls = append(d.controls, doraPair{name: id, citation: at, inScope: controlInScope[id], excepted: excepted[id]})
		if controlInScope[id] {
			d.rows[at].ControlsInScope++
		}
		if excepted[id] {
			d.rows[at].MissedControls++
		}
	}

	d.report = DORAReport{ScopeNFRs: m.result.ScopeNFRs, Exceptions: m.result.SelectedNFRs}
	for _, row := range d.rows {
		if row.NFRsInScope == 0 {
			continue
		}
		d.report.Rows = append(d.report.Rows, row)
		if row.RTS {
			d.report.RTSInScope++
		} else {
			d.report.DORAInScope++
		}
		if row.Exceptions > 0 {
			if row.RTS {
				d.report.RTSWithExceptions++
			} else {
				d.report.DORAWithExceptions++
			}
		}
	}
	return d
}

// commonestTitle settles what an article is called: the catalog words one
// article in several ways.
func commonestTitle(counts map[string]int) string {
	best, most := "", 0
	for title, count := range counts {
		if count > most || (count == most && title < best) {
			best, most = title, count
		}
	}
	return best
}

// DORAReportFor is the DORA report for a scope and a selection.
func DORAReportFor(data Data, opts Options) DORAReport {
	return doraOf(evaluate(data, opts)).report
}

// DORANFR is a Security NFR in scope that reaches a citation. Excepted says
// whether it is in the exception set; Partial is only true if it is.
type DORANFR struct {
	Exception
	Excepted bool
}

// DORAControl is a control that cites a citation and that an NFR in
// scope is linked to. Missed says whether an exception is linked to it.
type DORAControl struct {
	MissedControl
	Missed bool
}

// DORADetail is what stands behind one row of the report: the NFRs its
// NFRsInScope counts and the controls its ControlsInScope counts.
type DORADetail struct {
	Row      DORARow
	NFRs     []DORANFR
	Controls []DORAControl
}

// DORADetailFor is the detail of the row with this regulation and reference.
// ok is false when the report for this scope and selection has no such row.
func DORADetailFor(data Data, opts Options, regulation, reference string) (DORADetail, bool) {
	m := evaluate(data, opts)
	d := doraOf(m)
	at := -1
	for i, row := range d.rows {
		if row.Regulation == regulation && row.Reference == reference && row.NFRsInScope > 0 {
			at = i
			break
		}
	}
	if at < 0 {
		return DORADetail{}, false
	}
	detail := DORADetail{Row: d.rows[at]}

	nfrs := map[string]nfrRow{}
	for _, nfr := range m.nfrs {
		nfrs[nfr.Key] = nfr
	}
	for _, pair := range d.nfrPairs {
		if pair.citation != at || !pair.inScope {
			continue
		}
		nfr := nfrs[pair.name]
		detail.NFRs = append(detail.NFRs, DORANFR{
			Exception: Exception{Key: nfr.Key, ID: strings.TrimSpace(nfr.ID), Summary: nfr.Summary, Domain: nfr.Domain, Weight: nfr.Weight, Partial: nfr.partial},
			Excepted:  nfr.selected,
		})
	}
	for _, pair := range d.controls {
		if pair.citation != at || !pair.inScope {
			continue
		}
		control := m.controls[m.byID[pair.name]]
		detail.Controls = append(detail.Controls, DORAControl{
			MissedControl: MissedControl{
				ID:              pair.name,
				Name:            control.Name,
				Family:          control.Family,
				Confidentiality: strings.TrimSpace(control.Confidentiality) != "",
				Integrity:       strings.TrimSpace(control.Integrity) != "",
				Availability:    strings.TrimSpace(control.Availability) != "",
				Listed:          true,
			},
			Missed: pair.excepted,
		})
	}
	sort.SliceStable(detail.Controls, func(i, j int) bool {
		return naturalLess(detail.Controls[i].ID, detail.Controls[j].ID)
	})
	return detail, true
}

// The DORA data sheet holds four tables side by side: the citations, each NFR
// with a citation it reaches, each control with a citation it carries, and
// each of those controls with each NFR linked to it.
const (
	ddNumber = iota + 1
	ddRegulation
	ddReference
	ddTitle
	ddKind
	ddInScope
	ddControlsInScope
	ddExceptions
	ddMissed
	ddOrder
	_
	ddPairKey
	ddPairCitation
	ddPairInScope
	ddPairExcepted
	_
	ddControlID
	ddControlCitation
	ddControlInScope
	ddControlExcepted
	_
	ddReachControl
	ddReachKey
	ddReachInScope

	ddFirstRow = 2

	ddKindDORA = 1
	ddKindRTS  = 2

	// The last column of the DORA sheet: the row of the citations table that
	// the line is read from.
	doraSource = 9
)

func doraDataRange(col, last int) string {
	return xlsx.SheetRef(SheetDORAData, xlsx.AbsRange(col, ddFirstRow, col, last))
}

func (d doraModel) citationsLast() int { return ddFirstRow + rowsOrOne(len(d.rows)) - 1 }
func (d doraModel) pairsLast() int     { return ddFirstRow + rowsOrOne(len(d.nfrPairs)) - 1 }
func (d doraModel) controlsLast() int {
	return ddFirstRow + rowsOrOne(len(d.controls)) - 1
}
func (d doraModel) reachLast() int { return ddFirstRow + rowsOrOne(len(d.reach)) - 1 }

func writeDORA(s *xlsx.Sheet, d doraModel) {
	s.Widths(5, 38, 28, 60, 12, 12, 12, 12, 7)
	s.Freeze(0, detailHeaderRow)
	s.FitPageWidth()
	s.Set(1, 1, xlsx.Text("DORA: the articles the controls cite, and the exceptions against each").With(xlsx.Title))
	s.Set(1, 2, xlsx.Text("An NFR counts against an article when a control it is linked to cites that article in its References. One NFR can count against several articles, so the Exceptions column does not add up to the number of exceptions. A partial exception counts as one.").With(xlsx.Muted))

	kind := doraDataRange(ddKind, d.citationsLast())
	inScope := doraDataRange(ddInScope, d.citationsLast())
	exceptions := doraDataRange(ddExceptions, d.citationsLast())
	figure := func(kindValue int, column string) string {
		return "SUMPRODUCT((" + kind + "=" + strconv.Itoa(kindValue) + ")*(" + column + ">0))"
	}
	s.Set(1, 3, xlsx.FormulaText(`"Security NFRs in scope: "&`+summaryCell(smScope)+
		`&" | Exceptions: "&`+summaryCell(smSelected)+
		`&" | DORA references: "&`+figure(ddKindDORA, inScope)+`&" in scope, "&`+figure(ddKindDORA, exceptions)+
		`&" with exceptions | RTS references: "&`+figure(ddKindRTS, inScope)+`&" in scope, "&`+figure(ddKindRTS, exceptions)+`&" with exceptions"`,
		d.report.Line()))

	headers := []string{"#", "Regulation", "Article", "Title", "NFRs in scope", "Controls in scope", "Exceptions", "Missed Controls", "Row"}
	for i, header := range headers {
		s.Set(i+1, detailHeaderRow, xlsx.Text(header).With(xlsx.Header))
	}

	position := map[string]int{}
	for i, row := range d.rows {
		position[row.Regulation+"|"+row.Reference] = i
	}
	order := doraDataRange(ddOrder, d.citationsLast())
	for i := 0; i < rowsOrOne(len(d.rows)); i++ {
		row := detailFirstRow + i
		filled := i < len(d.report.Rows)
		shown := DORARow{}
		at := 0
		if filled {
			shown = d.report.Rows[i]
			at = position[shown.Regulation+"|"+shown.Reference]
		}
		detailLine(s, row, doraSource, "MAX("+order+")", order, ddFirstRow, filled, at+1)
		text := func(col, from int, fallback, value string) {
			stored := ""
			if filled {
				stored = orFallback(value, fallback)
			}
			s.Set(col, row, xlsx.FormulaText(lookup(row, doraSource, doraDataRange(from, d.citationsLast()), fallback), stored))
		}
		text(2, ddRegulation, "", shown.Regulation)
		text(3, ddReference, "-", shown.Reference)
		text(4, ddTitle, "-", shown.Title)
		number := func(col, from, value int) {
			source := "$" + xlsx.Col(doraSource) + strconv.Itoa(row)
			formula := "IF(" + source + `="","",INDEX(` + doraDataRange(from, d.citationsLast()) + "," + source + "))"
			if filled {
				s.Set(col, row, xlsx.FormulaNumber(formula, float64(value)).With(xlsx.Center))
				return
			}
			s.Set(col, row, xlsx.FormulaText(formula, "").With(xlsx.Center))
		}
		number(5, ddInScope, shown.NFRsInScope)
		number(6, ddControlsInScope, shown.ControlsInScope)
		number(7, ddExceptions, shown.Exceptions)
		number(8, ddMissed, shown.MissedControls)
	}
}

func writeDORAData(s *xlsx.Sheet, d doraModel, l layout) {
	s.Widths(6, 38, 28, 50, 6, 10, 12, 10, 10, 7, 3, 8, 8, 8, 8, 3, 14, 8, 8, 8, 3, 14, 8, 8)
	s.Freeze(0, 1)
	for col, header := range map[int]string{
		ddNumber: "#", ddRegulation: "Regulation", ddReference: "Article", ddTitle: "Title", ddKind: "Kind",
		ddInScope: "NFRs in scope", ddControlsInScope: "Controls in scope", ddExceptions: "Exceptions", ddMissed: "Missed Controls", ddOrder: "Order",
		ddPairKey: "NFR Key", ddPairCitation: "Cites #", ddPairInScope: "In scope", ddPairExcepted: "Excepted",
		ddControlID: "Control ID", ddControlCitation: "Cites #", ddControlInScope: "In scope", ddControlExcepted: "Excepted",
		ddReachControl: "Control ID", ddReachKey: "NFR Key", ddReachInScope: "In scope",
	} {
		s.Set(col, 1, xlsx.Text(header).With(xlsx.Header))
	}

	inSheet := func(col, last int) string { return xlsx.AbsRange(col, ddFirstRow, col, last) }
	first := strconv.Itoa(ddFirstRow)
	order := 0
	for i, citation := range d.rows {
		row := ddFirstRow + i
		number := xlsx.Ref(ddNumber, row)
		count := func(citeCol, valueCol, last int) string {
			return "SUMPRODUCT((" + inSheet(citeCol, last) + "=" + number + ")*" + inSheet(valueCol, last) + ")"
		}
		s.Set(ddNumber, row, xlsx.Int(i+1))
		s.Set(ddRegulation, row, xlsx.Text(citation.Regulation))
		s.Set(ddReference, row, xlsx.Text(citation.Reference))
		s.Set(ddTitle, row, xlsx.Text(citation.Title))
		kind := ddKindDORA
		if citation.RTS {
			kind = ddKindRTS
		}
		s.Set(ddKind, row, xlsx.Int(kind))
		s.Set(ddInScope, row, xlsx.FormulaNumber(count(ddPairCitation, ddPairInScope, d.pairsLast()), float64(citation.NFRsInScope)))
		s.Set(ddControlsInScope, row, xlsx.FormulaNumber(count(ddControlCitation, ddControlInScope, d.controlsLast()), float64(citation.ControlsInScope)))
		s.Set(ddExceptions, row, xlsx.FormulaNumber(count(ddPairCitation, ddPairExcepted, d.pairsLast()), float64(citation.Exceptions)))
		s.Set(ddMissed, row, xlsx.FormulaNumber(count(ddControlCitation, ddControlExcepted, d.controlsLast()), float64(citation.MissedControls)))
		shown := 0
		if citation.NFRsInScope > 0 {
			order++
			shown = order
		}
		cell := xlsx.Ref(ddInScope, row)
		s.Set(ddOrder, row, xlsx.FormulaNumber(
			"IF("+cell+">0,SUMPRODUCT(("+xlsx.Col(ddInScope)+"$"+first+":"+cell+">0)*1),0)", float64(shown)))
	}

	for i, pair := range d.nfrPairs {
		row := ddFirstRow + i
		key := xlsx.Ref(ddPairKey, row)
		s.Set(ddPairKey, row, xlsx.Text(pair.name))
		s.Set(ddPairCitation, row, xlsx.Int(pair.citation+1))
		s.Set(ddPairInScope, row, xlsx.FormulaNumber("SUMPRODUCT(("+l.ex(exKey)+"="+key+")*"+l.ex(exInScope)+")", flag(pair.inScope)))
		s.Set(ddPairExcepted, row, xlsx.FormulaNumber("SUMPRODUCT(("+l.ex(exKey)+"="+key+")*"+l.ex(exSelected)+")", flag(pair.excepted)))
	}
	for i, pair := range d.controls {
		row := ddFirstRow + i
		id := xlsx.Ref(ddControlID, row)
		s.Set(ddControlID, row, xlsx.Text(pair.name))
		s.Set(ddControlCitation, row, xlsx.Int(pair.citation+1))
		s.Set(ddControlInScope, row, xlsx.FormulaNumber(
			"IF(SUMPRODUCT(("+inSheet(ddReachControl, d.reachLast())+"="+id+")*"+inSheet(ddReachInScope, d.reachLast())+")>0,1,0)", flag(pair.inScope)))
		s.Set(ddControlExcepted, row, xlsx.FormulaNumber(
			"IF(SUMPRODUCT(("+l.lk(lkControl)+"="+id+")*"+l.lk(lkExcepted)+")>0,1,0)", flag(pair.excepted)))
	}
	for i, reach := range d.reach {
		row := ddFirstRow + i
		s.Set(ddReachControl, row, xlsx.Text(reach.control))
		s.Set(ddReachKey, row, xlsx.Text(reach.key))
		s.Set(ddReachInScope, row, xlsx.FormulaNumber(
			"SUMPRODUCT(("+l.ex(exKey)+"="+xlsx.Ref(ddReachKey, row)+")*"+l.ex(exInScope)+")", flag(reach.inScope)))
	}
	s.TextNumbers(xlsx.Range(ddPairKey, ddFirstRow, ddPairKey, d.pairsLast()))
	s.TextNumbers(xlsx.Range(ddReachKey, ddFirstRow, ddReachKey, d.reachLast()))
	// What the application exported: the citations are changed there, not here.
	s.Protect()
}
