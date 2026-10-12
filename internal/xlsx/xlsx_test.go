package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func writeParts(t *testing.T, w *Workbook) map[string]string {
	t.Helper()
	var file bytes.Buffer
	if err := w.Write(&file); err != nil {
		t.Fatalf("write: %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(file.Bytes()), int64(file.Len()))
	if err != nil {
		t.Fatalf("the workbook is not a zip: %v", err)
	}
	parts := map[string]string{}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatalf("%s: %v", entry.Name, err)
		}
		raw, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("%s: %v", entry.Name, err)
		}
		parts[entry.Name] = string(raw)

		decoder := xml.NewDecoder(bytes.NewReader(raw))
		for {
			if _, err := decoder.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed XML: %v", entry.Name, err)
			}
		}
	}
	return parts
}

func TestWorkbookParts(t *testing.T) {
	w := New()
	first := w.AddSheet("How to use")
	first.Set(1, 1, Text("Guide").With(Title))
	data := w.AddSheet("Data")
	data.SetRow(1, Text("Pick").With(Header), Text("Key").With(Header), Text("Weight").With(Header))
	data.SetRow(2, Cell{}.With(Input), Text("1.10"), Int(3))
	data.Set(4, 2, FormulaNumber(`IF(A2="Yes",1,0)`, 0))
	data.Set(5, 2, FormulaText(`B2&" & more"`, `1.10 & more`))
	data.Widths(8, 12.5)
	data.Freeze(1, 1)
	data.AutoFilter("A1:E2")
	data.ListValidation("A2:A2", "Yes")
	data.Highlight("B2:E2", `$A2="Yes"`)
	data.TextNumbers("B2:B2")
	data.FitPageWidth()
	w.Active = 1

	parts := writeParts(t, w)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels",
		"xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if parts[name] == "" {
			t.Fatalf("the workbook has no %s", name)
		}
	}

	workbook := parts["xl/workbook.xml"]
	for _, want := range []string{
		`<sheet name="How to use" sheetId="1" r:id="rId1"/>`,
		`<sheet name="Data" sheetId="2" r:id="rId2"/>`,
		`<workbookView activeTab="1"/>`,
		`<definedName name="_xlnm._FilterDatabase" localSheetId="1" hidden="1">Data!$A$1:$E$2</definedName>`,
		`fullCalcOnLoad="1"`,
	} {
		if !strings.Contains(workbook, want) {
			t.Errorf("workbook.xml lacks %s\n%s", want, workbook)
		}
	}

	sheet := parts["xl/worksheets/sheet2.xml"]
	for _, want := range []string{
		`<sheetView workbookViewId="0" tabSelected="1">`,
		`<pane xSplit="1" ySplit="1" topLeftCell="B2" activePane="bottomRight" state="frozen"/>`,
		`<col min="2" max="2" width="12.5" customWidth="1"/>`,
		`<c r="A2" s="5"/>`,
		`<c r="B2" t="inlineStr"><is><t xml:space="preserve">1.10</t></is></c>`,
		`<c r="C2"><v>3</v></c>`,
		`<c r="D2"><f>IF(A2=&quot;Yes&quot;,1,0)</f><v>0</v></c>`,
		`<c r="E2" t="str"><f>B2&amp;&quot; &amp; more&quot;</f><v>1.10 &amp; more</v></c>`,
		`<autoFilter ref="A1:E2"/>`,
		`<cfRule type="expression" dxfId="0" priority="1"><formula>$A2=&quot;Yes&quot;</formula></cfRule>`,
		`<dataValidation type="list" allowBlank="1" showInputMessage="1" showErrorMessage="1" sqref="A2:A2"><formula1>&quot;Yes&quot;</formula1></dataValidation>`,
		`<pageSetup orientation="landscape" fitToWidth="1" fitToHeight="0"/>`,
		`<ignoredError sqref="B2:B2" numberStoredAsText="1"/>`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("sheet2.xml lacks %s\n%s", want, sheet)
		}
	}
	if strings.Contains(parts["xl/worksheets/sheet1.xml"], "tabSelected") {
		t.Error("the sheet that is not active is marked selected")
	}

	// The order of the styles is the order of the constants.
	if got := strings.Count(parts["xl/styles.xml"], "<xf "); got != int(styleCount)+1 {
		t.Errorf("styles.xml has %d cell formats for %d styles", got-1, styleCount)
	}
}

func TestTextStaysText(t *testing.T) {
	w := New()
	s := w.AddSheet("Data")
	s.Set(1, 1, Text(`=HYPERLINK("http://example.test","click")`))
	s.Set(1, 2, Text("a\x00b\x0bc <&> \"q\"\nnext"))
	s.Set(1, 3, Text(""))
	s.Set(1, 4, Text(strings.Repeat("x", maxCellText+10)))
	sheet := writeParts(t, w)["xl/worksheets/sheet1.xml"]

	if !strings.Contains(sheet, `<c r="A1" t="inlineStr"><is><t xml:space="preserve">=HYPERLINK(&quot;http://example.test&quot;,&quot;click&quot;)</t></is></c>`) {
		t.Errorf("text that starts with = was not written as text:\n%.400s", sheet)
	}
	if strings.Contains(sheet, "<f>") {
		t.Error("a text cell became a formula")
	}
	if !strings.Contains(sheet, "abc &lt;&amp;&gt; &quot;q&quot;\nnext") {
		t.Errorf("control characters were kept or the text was not escaped:\n%.400s", sheet)
	}
	if strings.Contains(sheet, `r="A3"`) {
		t.Error("an empty text cell was written")
	}
	if strings.Contains(sheet, strings.Repeat("x", maxCellText+1)) {
		t.Error("a cell holds more text than Excel accepts")
	}
}

func TestOmitFormulaValues(t *testing.T) {
	w := New()
	s := w.AddSheet("Data")
	s.Set(1, 1, FormulaNumber("1+1", 2))
	s.Set(2, 1, FormulaText(`"a"&"b"`, "ab"))
	w.OmitFormulaValues = true
	sheet := writeParts(t, w)["xl/worksheets/sheet1.xml"]
	if !strings.Contains(sheet, `<c r="A1"><f>1+1</f></c>`) || !strings.Contains(sheet, `<c r="B1" t="str"><f>&quot;a&quot;&amp;&quot;b&quot;</f></c>`) {
		t.Errorf("formulas kept their stored values:\n%s", sheet)
	}
	if text, number, isNumber := s.Stored(1, 1); !isNumber || number != 2 || text != "" {
		t.Errorf("Stored(A1) = %q, %v, %v", text, number, isNumber)
	}
	if text, _, isNumber := s.Stored(2, 1); isNumber || text != "ab" {
		t.Errorf("Stored(B1) = %q, number %v", text, isNumber)
	}
}

func TestReferences(t *testing.T) {
	for col, want := range map[int]string{1: "A", 26: "Z", 27: "AA", 52: "AZ", 53: "BA", 702: "ZZ", 703: "AAA"} {
		if got := Col(col); got != want {
			t.Errorf("Col(%d) = %s, want %s", col, got, want)
		}
	}
	if got := AbsRange(2, 7, 2, 115); got != "$B$7:$B$115" {
		t.Errorf("AbsRange = %s", got)
	}
	if got := SheetRef("Links", "A1"); got != "Links!A1" {
		t.Errorf("SheetRef = %s", got)
	}
	if got := SheetRef("Selected NFRs", "A1"); got != "'Selected NFRs'!A1" {
		t.Errorf("SheetRef = %s", got)
	}
	if got := SheetRef("It's", "A1"); got != "'It''s'!A1" {
		t.Errorf("SheetRef = %s", got)
	}
}

func TestSheetNamesAreChecked(t *testing.T) {
	for _, names := range [][]string{{}, {""}, {"a/b"}, {strings.Repeat("n", 32)}, {"Data", "data"}} {
		w := New()
		for _, name := range names {
			w.AddSheet(name)
		}
		if err := w.Write(io.Discard); err == nil {
			t.Errorf("a workbook with the sheets %q was written", names)
		}
	}
}
