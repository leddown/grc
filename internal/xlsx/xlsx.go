// Package xlsx writes Excel workbooks (.xlsx): cells holding text, numbers and
// formulas, on sheets with frozen panes, filters, a dropdown and a highlight
// rule. It writes exactly what this application's exports use and reads
// nothing. It is here rather than a library because a workbook is a zip of a
// few XML files, and a spreadsheet library would be the largest dependency in
// the module for the sake of one download.
package xlsx

import (
	"archive/zip"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Style is one of the fixed cell formats in the workbook's stylesheet.
type Style int

const (
	Default Style = iota
	Title
	Header
	Label
	Wrap
	Input
	Percent
	Muted
	Center
	Section
	Decimal
	Left
	styleCount
)

// maxCellText is Excel's limit on the characters one cell holds.
const maxCellText = 32767

type cellKind int

const (
	kindEmpty cellKind = iota
	kindText
	kindNumber
	kindFormulaNumber
	kindFormulaText
)

// Cell is one cell's content and style.
type Cell struct {
	kind    cellKind
	text    string
	number  float64
	formula string
	style   Style
}

// Text is a cell holding s as text. It stays text whatever it looks like: a
// value beginning with "=" is not a formula and "1.10" is not a number.
func Text(s string) Cell { return Cell{kind: kindText, text: s} }

func Number(n float64) Cell { return Cell{kind: kindNumber, number: n} }

func Int(n int) Cell { return Number(float64(n)) }

// FormulaNumber and FormulaText are formulas, written without the leading "=".
// value is what the formula evaluates to as the workbook is written. A
// spreadsheet recalculates it on opening; a previewer, or Excel showing a
// downloaded file in Protected View, does not calculate and shows value.
func FormulaNumber(expr string, value float64) Cell {
	return Cell{kind: kindFormulaNumber, formula: expr, number: value}
}

func FormulaText(expr string, value string) Cell {
	return Cell{kind: kindFormulaText, formula: expr, text: value}
}

func (c Cell) With(style Style) Cell {
	c.style = style
	return c
}

type Workbook struct {
	sheets []*Sheet
	// Active is the index of the sheet shown when the workbook opens.
	Active int
	// OmitFormulaValues writes formulas with no stored result, so whatever
	// opens the workbook has to calculate them itself. For tests that check
	// the formulas rather than the stored results.
	OmitFormulaValues bool
}

type Sheet struct {
	name          string
	rows          [][]Cell
	widths        []float64
	freezeCols    int
	freezeRows    int
	filter        string
	validations   []listValidation
	highlights    []highlight
	textNumbers   []string
	fitPageToWide bool
	protected     bool
	hiddenFrom    int
	hiddenTo      int
}

type listValidation struct {
	ref    string
	values []string
}

type highlight struct {
	ref     string
	formula string
}

func New() *Workbook { return &Workbook{} }

func (w *Workbook) AddSheet(name string) *Sheet {
	s := &Sheet{name: name}
	w.sheets = append(w.sheets, s)
	return s
}

func (s *Sheet) Name() string { return s.name }

func (w *Workbook) Sheets() []*Sheet { return w.sheets }

// Size is the sheet's extent: its last used column and row.
func (s *Sheet) Size() (cols, rows int) {
	for _, line := range s.rows {
		if len(line) > cols {
			cols = len(line)
		}
	}
	return cols, len(s.rows)
}

// Stored is what a cell holds as the workbook is written: its text, or its
// number. For a formula that is the value written beside it.
func (s *Sheet) Stored(col, row int) (text string, number float64, isNumber bool) {
	if row < 1 || row > len(s.rows) || col < 1 || col > len(s.rows[row-1]) {
		return "", 0, false
	}
	c := s.rows[row-1][col-1]
	switch c.kind {
	case kindNumber, kindFormulaNumber:
		return "", c.number, true
	case kindText, kindFormulaText:
		return c.text, 0, false
	}
	return "", 0, false
}

// Formula is a cell's formula, or "" when it holds a value.
func (s *Sheet) Formula(col, row int) string {
	if row < 1 || row > len(s.rows) || col < 1 || col > len(s.rows[row-1]) {
		return ""
	}
	return s.rows[row-1][col-1].formula
}

// Set puts a cell at a 1-based column and row.
func (s *Sheet) Set(col, row int, c Cell) {
	if col < 1 || row < 1 {
		return
	}
	for len(s.rows) < row {
		s.rows = append(s.rows, nil)
	}
	line := s.rows[row-1]
	for len(line) < col {
		line = append(line, Cell{})
	}
	line[col-1] = c
	s.rows[row-1] = line
}

// SetRow puts cells in a row from column A.
func (s *Sheet) SetRow(row int, cells ...Cell) {
	for i, c := range cells {
		s.Set(i+1, row, c)
	}
}

// Widths sets column widths from column A, in characters.
func (s *Sheet) Widths(widths ...float64) { s.widths = widths }

// HideColumns hides the columns from one to another, both counted from 1. The
// cells stay, and so do the formulas in them: a hidden column still works.
func (s *Sheet) HideColumns(from, to int) { s.hiddenFrom, s.hiddenTo = from, to }

// Hidden reports whether a column is hidden.
func (s *Sheet) Hidden(col int) bool {
	return s.hiddenFrom > 0 && col >= s.hiddenFrom && col <= s.hiddenTo
}

// Freeze keeps the first cols columns and rows rows in view while scrolling.
func (s *Sheet) Freeze(cols, rows int) { s.freezeCols, s.freezeRows = cols, rows }

// AutoFilter puts filter buttons on the header row of ref.
func (s *Sheet) AutoFilter(ref string) { s.filter = ref }

// ListValidation restricts the cells of ref to values, offered as a dropdown.
// An empty cell stays allowed.
func (s *Sheet) ListValidation(ref string, values ...string) {
	s.validations = append(s.validations, listValidation{ref: ref, values: values})
}

// Highlight fills the cells of ref where formula, written for the top-left
// cell of ref, is true.
func (s *Sheet) Highlight(ref, formula string) {
	s.highlights = append(s.highlights, highlight{ref: ref, formula: formula})
}

// TextNumbers marks ref as text that is meant to look like numbers, so Excel
// does not flag each cell with a warning.
func (s *Sheet) TextNumbers(ref string) { s.textNumbers = append(s.textNumbers, ref) }

// FitPageWidth prints the sheet in landscape, one page wide.
func (s *Sheet) FitPageWidth() { s.fitPageToWide = true }

// Protect locks the sheet against editing. Its formulas still calculate and
// its filter still works. There is no password: Unprotect Sheet lifts it.
func (s *Sheet) Protect() { s.protected = true }

// Protected reports whether the sheet is locked.
func (s *Sheet) Protected() bool { return s.protected }

// Col is a column's letters: 1 is A, 27 is AA.
func Col(col int) string {
	name := ""
	for col > 0 {
		col--
		name = string(rune('A'+col%26)) + name
		col /= 26
	}
	return name
}

// Ref is a cell reference such as B7.
func Ref(col, row int) string { return Col(col) + strconv.Itoa(row) }

// Abs is an absolute cell reference such as $B$7.
func Abs(col, row int) string { return "$" + Col(col) + "$" + strconv.Itoa(row) }

// Range is a range such as A1:C9.
func Range(col1, row1, col2, row2 int) string { return Ref(col1, row1) + ":" + Ref(col2, row2) }

// AbsRange is an absolute range such as $A$1:$C$9.
func AbsRange(col1, row1, col2, row2 int) string { return Abs(col1, row1) + ":" + Abs(col2, row2) }

// SheetRef qualifies a reference with its sheet, quoting the name when a
// formula would not read it bare.
func SheetRef(sheet, ref string) string {
	bare := sheet != ""
	for i, r := range sheet {
		letter := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_'
		digit := r >= '0' && r <= '9'
		if !letter && !(digit && i > 0) {
			bare = false
		}
	}
	if bare {
		return sheet + "!" + ref
	}
	return "'" + strings.ReplaceAll(sheet, "'", "''") + "'!" + ref
}

func validSheetName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 31 {
		return false
	}
	return !strings.ContainsAny(name, `[]:*?/\`) && !strings.HasPrefix(name, "'") && !strings.HasSuffix(name, "'")
}

// Write writes the workbook as an .xlsx file.
func (w *Workbook) Write(out io.Writer) error {
	if len(w.sheets) == 0 {
		return fmt.Errorf("xlsx: a workbook needs at least one sheet")
	}
	seen := map[string]bool{}
	for _, s := range w.sheets {
		if !validSheetName(s.name) {
			return fmt.Errorf("xlsx: %q is not a valid sheet name", s.name)
		}
		if seen[strings.ToLower(s.name)] {
			return fmt.Errorf("xlsx: two sheets are named %q", s.name)
		}
		seen[strings.ToLower(s.name)] = true
	}
	active := w.Active
	if active < 0 || active >= len(w.sheets) {
		active = 0
	}

	archive := zip.NewWriter(out)
	add := func(name, content string) error {
		// A fixed timestamp: the same data gives the same bytes.
		file, err := archive.CreateHeader(&zip.FileHeader{
			Name:     name,
			Method:   zip.Deflate,
			Modified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			return fmt.Errorf("xlsx: %s: %w", name, err)
		}
		if _, err := io.WriteString(file, content); err != nil {
			return fmt.Errorf("xlsx: %s: %w", name, err)
		}
		return nil
	}

	if err := add("[Content_Types].xml", w.contentTypes()); err != nil {
		return err
	}
	if err := add("_rels/.rels", rootRels); err != nil {
		return err
	}
	if err := add("xl/workbook.xml", w.workbookXML(active)); err != nil {
		return err
	}
	if err := add("xl/_rels/workbook.xml.rels", w.workbookRels()); err != nil {
		return err
	}
	if err := add("xl/styles.xml", stylesXML); err != nil {
		return err
	}
	for i, s := range w.sheets {
		if err := add("xl/worksheets/sheet"+strconv.Itoa(i+1)+".xml", s.xml(i == active, w.OmitFormulaValues)); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("xlsx: %w", err)
	}
	return nil
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const mainNS = `http://schemas.openxmlformats.org/spreadsheetml/2006/main`

const rootRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

func (w *Workbook) contentTypes() string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	b.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := range w.sheets {
		b.WriteString(`<Override PartName="/xl/worksheets/sheet` + strconv.Itoa(i+1) + `.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`)
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func (w *Workbook) workbookXML(active int) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<workbook xmlns="` + mainNS + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	b.WriteString(`<bookViews><workbookView activeTab="` + strconv.Itoa(active) + `"/></bookViews>`)
	b.WriteString(`<sheets>`)
	for i, s := range w.sheets {
		id := strconv.Itoa(i + 1)
		b.WriteString(`<sheet name="` + escape(s.name) + `" sheetId="` + id + `" r:id="rId` + id + `"/>`)
	}
	b.WriteString(`</sheets>`)

	// Excel keeps a filter's range under this name as well as on the sheet.
	names := ""
	for i, s := range w.sheets {
		if s.filter == "" {
			continue
		}
		names += `<definedName name="_xlnm._FilterDatabase" localSheetId="` + strconv.Itoa(i) + `" hidden="1">` +
			escape(SheetRef(s.name, absolute(s.filter))) + `</definedName>`
	}
	if names != "" {
		b.WriteString(`<definedNames>` + names + `</definedNames>`)
	}
	b.WriteString(`<calcPr fullCalcOnLoad="1"/>`)
	b.WriteString(`</workbook>`)
	return b.String()
}

func (w *Workbook) workbookRels() string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := range w.sheets {
		id := strconv.Itoa(i + 1)
		b.WriteString(`<Relationship Id="rId` + id + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet` + id + `.xml"/>`)
	}
	b.WriteString(`<Relationship Id="rId` + strconv.Itoa(len(w.sheets)+1) + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	b.WriteString(`</Relationships>`)
	return b.String()
}

// The order of cellXfs is the order of the Style constants.
const stylesXML = xmlHeader + `<styleSheet xmlns="` + mainNS + `">` +
	`<fonts count="5">` +
	`<font><sz val="11"/><name val="Calibri"/></font>` +
	`<font><b/><sz val="11"/><name val="Calibri"/></font>` +
	`<font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font>` +
	`<font><b/><sz val="14"/><name val="Calibri"/></font>` +
	`<font><i/><sz val="11"/><color rgb="FF5E6672"/><name val="Calibri"/></font>` +
	`</fonts>` +
	`<fills count="5">` +
	`<fill><patternFill patternType="none"/></fill>` +
	`<fill><patternFill patternType="gray125"/></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FF2F3B52"/><bgColor indexed="64"/></patternFill></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFFFF2CC"/><bgColor indexed="64"/></patternFill></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFE4E8EF"/><bgColor indexed="64"/></patternFill></fill>` +
	`</fills>` +
	`<borders count="2">` +
	`<border><left/><right/><top/><bottom/><diagonal/></border>` +
	`<border><left style="thin"><color rgb="FFB0B7C3"/></left><right style="thin"><color rgb="FFB0B7C3"/></right>` +
	`<top style="thin"><color rgb="FFB0B7C3"/></top><bottom style="thin"><color rgb="FFB0B7C3"/></bottom><diagonal/></border>` +
	`</borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="12">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="3" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="2" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>` +
	`<xf numFmtId="0" fontId="1" fillId="3" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center"/></xf>` +
	`<xf numFmtId="9" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment horizontal="left"/></xf>` +
	`<xf numFmtId="0" fontId="4" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment horizontal="center"/></xf>` +
	`<xf numFmtId="0" fontId="1" fillId="4" borderId="0" xfId="0" applyFont="1" applyFill="1"/>` +
	`<xf numFmtId="2" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment horizontal="left"/></xf>` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment horizontal="left"/></xf>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`<dxfs count="1"><dxf><fill><patternFill><bgColor rgb="FFFFF2CC"/></patternFill></fill></dxf></dxfs>` +
	`</styleSheet>`

func (s *Sheet) xml(active, omitFormulaValues bool) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<worksheet xmlns="` + mainNS + `">`)
	if s.fitPageToWide {
		b.WriteString(`<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr>`)
	}

	lastCol := 1
	for _, line := range s.rows {
		if len(line) > lastCol {
			lastCol = len(line)
		}
	}
	lastRow := len(s.rows)
	if lastRow < 1 {
		lastRow = 1
	}
	b.WriteString(`<dimension ref="` + Range(1, 1, lastCol, lastRow) + `"/>`)

	b.WriteString(`<sheetViews><sheetView workbookViewId="0"`)
	if active {
		b.WriteString(` tabSelected="1"`)
	}
	if s.freezeCols > 0 || s.freezeRows > 0 {
		b.WriteString(`><pane`)
		pane := "bottomRight"
		if s.freezeCols > 0 {
			b.WriteString(` xSplit="` + strconv.Itoa(s.freezeCols) + `"`)
		} else {
			pane = "bottomLeft"
		}
		if s.freezeRows > 0 {
			b.WriteString(` ySplit="` + strconv.Itoa(s.freezeRows) + `"`)
		} else {
			pane = "topRight"
		}
		b.WriteString(` topLeftCell="` + Ref(s.freezeCols+1, s.freezeRows+1) + `" activePane="` + pane + `" state="frozen"/>`)
		b.WriteString(`<selection pane="` + pane + `"/></sheetView></sheetViews>`)
	} else {
		b.WriteString(`/></sheetViews>`)
	}
	b.WriteString(`<sheetFormatPr defaultRowHeight="15"/>`)

	if len(s.widths) > 0 {
		b.WriteString(`<cols>`)
		for i, width := range s.widths {
			col := strconv.Itoa(i + 1)
			hidden := ""
			if s.Hidden(i + 1) {
				hidden = ` hidden="1"`
			}
			b.WriteString(`<col min="` + col + `" max="` + col + `" width="` + formatNumber(width) + `" customWidth="1"` + hidden + `/>`)
		}
		b.WriteString(`</cols>`)
	}

	b.WriteString(`<sheetData>`)
	for r, line := range s.rows {
		empty := true
		for _, c := range line {
			if c.kind != kindEmpty {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		b.WriteString(`<row r="` + strconv.Itoa(r+1) + `">`)
		for c, cell := range line {
			writeCell(&b, Ref(c+1, r+1), cell, omitFormulaValues)
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if s.protected {
		b.WriteString(`<sheetProtection sheet="1" objects="1" scenarios="1" autoFilter="0"/>`)
	}

	// The schema fixes the order of what follows.
	if s.filter != "" {
		b.WriteString(`<autoFilter ref="` + escape(s.filter) + `"/>`)
	}
	for i, h := range s.highlights {
		b.WriteString(`<conditionalFormatting sqref="` + escape(h.ref) + `">`)
		b.WriteString(`<cfRule type="expression" dxfId="0" priority="` + strconv.Itoa(i+1) + `"><formula>` + escape(h.formula) + `</formula></cfRule>`)
		b.WriteString(`</conditionalFormatting>`)
	}
	if len(s.validations) > 0 {
		b.WriteString(`<dataValidations count="` + strconv.Itoa(len(s.validations)) + `">`)
		for _, v := range s.validations {
			b.WriteString(`<dataValidation type="list" allowBlank="1" showInputMessage="1" showErrorMessage="1" sqref="` + escape(v.ref) + `">`)
			b.WriteString(`<formula1>` + escape(`"`+strings.Join(v.values, ",")+`"`) + `</formula1></dataValidation>`)
		}
		b.WriteString(`</dataValidations>`)
	}
	b.WriteString(`<pageMargins left="0.5" right="0.5" top="0.6" bottom="0.6" header="0.3" footer="0.3"/>`)
	if s.fitPageToWide {
		b.WriteString(`<pageSetup orientation="landscape" fitToWidth="1" fitToHeight="0"/>`)
	}
	if len(s.textNumbers) > 0 {
		b.WriteString(`<ignoredErrors>`)
		for _, ref := range s.textNumbers {
			b.WriteString(`<ignoredError sqref="` + escape(ref) + `" numberStoredAsText="1"/>`)
		}
		b.WriteString(`</ignoredErrors>`)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func writeCell(b *strings.Builder, ref string, c Cell, omitFormulaValues bool) {
	if c.kind == kindText && c.text == "" {
		c.kind = kindEmpty
	}
	if c.kind == kindEmpty && c.style == Default {
		return
	}
	b.WriteString(`<c r="` + ref + `"`)
	if c.style > Default && c.style < styleCount {
		b.WriteString(` s="` + strconv.Itoa(int(c.style)) + `"`)
	}
	switch c.kind {
	case kindText:
		b.WriteString(` t="inlineStr"><is><t xml:space="preserve">` + escape(clip(c.text)) + `</t></is></c>`)
	case kindNumber:
		b.WriteString(`><v>` + formatNumber(c.number) + `</v></c>`)
	case kindFormulaNumber:
		b.WriteString(`><f>` + escape(c.formula) + `</f>`)
		if !omitFormulaValues {
			b.WriteString(`<v>` + formatNumber(c.number) + `</v>`)
		}
		b.WriteString(`</c>`)
	case kindFormulaText:
		b.WriteString(` t="str"><f>` + escape(c.formula) + `</f>`)
		if !omitFormulaValues {
			b.WriteString(`<v>` + escape(clip(c.text)) + `</v>`)
		}
		b.WriteString(`</c>`)
	default:
		b.WriteString(`/>`)
	}
}

func formatNumber(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxCellText {
		return s
	}
	return string([]rune(s)[:maxCellText])
}

// escape makes s safe as XML text or an attribute value. The characters XML
// 1.0 cannot carry at all are dropped: one of them in a cell makes the whole
// workbook unreadable.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\r':
			b.WriteString("&#13;")
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20, r == 0xFFFE, r == 0xFFFF, r == utf8.RuneError:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// absolute turns A6:N115 into $A$6:$N$115.
func absolute(ref string) string {
	parts := strings.Split(ref, ":")
	for i, part := range parts {
		split := strings.IndexAny(part, "0123456789")
		if split <= 0 {
			continue
		}
		parts[i] = "$" + part[:split] + "$" + part[split:]
	}
	return strings.Join(parts, ":")
}
