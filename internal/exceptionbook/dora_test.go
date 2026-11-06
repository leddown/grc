package exceptionbook

import (
	"strconv"
	"strings"
	"testing"

	"grc/internal/controlcatalog"
	"grc/internal/nfrlink"
	"grc/internal/securitynfr"
)

func TestParseReferences(t *testing.T) {
	cases := []struct {
		name, text string
		want       []string
	}{
		{"one article", "DORA: Article 6, ICT risk management framework.", []string{"DORA|Article 6|ICT risk management framework"}},
		{"no colon, no comma", "DORA Article 9 Protection and Prevention.", []string{"DORA|Article 9|Protection and Prevention"}},
		{"a line continues the label above", "DORA: Article 6, Framework;\nArticle 7, Systems.", []string{"DORA|Article 6|Framework", "DORA|Article 7|Systems"}},
		{"the label alone on its line", "DORA: \nArticle 13, Learning and evolving;", []string{"DORA|Article 13|Learning and evolving"}},
		{"the same number under DORA and RTS", "DORA: Article 21, X;\nRTS on ICT risk management framework, Article 21, Access control.",
			[]string{"DORA|Article 21|X", rtsRiskManagement + "|Article 21|Access control"}},
		{"the RTS's three spellings are one", "RTS Risk Management Framework, Article 11, Data.\nRTS on ICT risk management, Article 19 Human resources policy.",
			[]string{rtsRiskManagement + "|Article 11|Data", rtsRiskManagement + "|Article 19|Human resources policy"}},
		{"a chapter is not its articles", "DORA: Chapter II (Articles 5 – 16), ICT risk management.", []string{"DORA|Chapter II|ICT risk management"}},
		{"a section", "RTS on ICT risk management framework, Section 7, ICT project and change management.", []string{rtsRiskManagement + "|Section 7|ICT project and change management"}},
		{"another RTS", "RTS on incident classification, Chapter III, Significant cyber threats.", []string{"RTS on incident classification|Chapter III|Significant cyber threats"}},
		{"only the regulation", "RTS on ICT third-party service provider policy", []string{"RTS on ICT third-party service provider policy||"}},
		{"another framework on the same line", "RTS Risk Management Framework, Article 11, Data. PCI DSS: 9.4 Media.", []string{rtsRiskManagement + "|Article 11|Data"}},
		{"another framework ends the label", "DORA: Article 6, F.\nPCI DSS: Control 7: Restrict.\nArticle 9, Not DORA.", []string{"DORA|Article 6|F"}},
		{"an article with no regulation is nothing", "Article 9, Protection", nil},
		{"nothing cited", "SWIFT: 7.2 Security Training and Awareness.", nil},
		{"said twice", "DORA: Article 6, A;\nArticle 6, A", []string{"DORA|Article 6|A"}},
	}
	for _, c := range cases {
		got := []string{}
		for _, citation := range parseReferences(c.text) {
			got = append(got, citation.regulation+"|"+citation.reference+"|"+citation.title)
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Everything the shipped catalog says of DORA in its References is read: a
// form the parser does not know would silently drop out of the counts.
func TestEveryDORAReferenceInTheCatalogIsRead(t *testing.T) {
	data := seededCatalog(t)
	articleLine := strings.NewReplacer("DORA:", "", "DORA", "")
	checked := 0
	for _, control := range data.Controls {
		text, _ := control.Appendix[referencesField].(string)
		if !strings.Contains(text, "DORA") && !strings.Contains(text, "RTS") {
			continue
		}
		checked++
		cited := map[string]bool{}
		citations := parseReferences(text)
		for _, citation := range citations {
			cited[citation.reference] = true
		}
		if len(citations) == 0 {
			t.Errorf("%s: nothing read from %q", control.ID, text)
		}
		for _, line := range referenceSeparator.Split(articleLine.Replace(text), -1) {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Article ") {
				number := strings.TrimRight(strings.Fields(line)[1], ",.")
				if !cited["Article "+number] {
					t.Errorf("%s: article %s of %q was not read", control.ID, number, line)
				}
			}
		}
	}
	// 155 controls, and the 559 enhancements of those controls, which carry
	// their control's References.
	if checked != 714 {
		t.Errorf("%d controls cite DORA or an RTS, want the 714 the catalog has", checked)
	}
}

func doraFixture() Data {
	control := func(id, references string) controlcatalog.Control {
		return controlcatalog.Control{ID: id, Name: id, Family: id[:2], Appendix: map[string]any{referencesField: references}}
	}
	link := func(key, id string) nfrlink.LinkRow {
		return nfrlink.LinkRow{NFRKey: key, ControlID: id, MappingControlID: id, Matched: true}
	}
	return Data{
		NFRs: []securitynfr.NFR{
			{Key: "1", ID: "1", Summary: "one", Weight: 3},
			{Key: "2", ID: "2", Summary: "two", Weight: 3},
			{Key: "3", ID: "3", Summary: "three", Weight: 3},
			{Key: "4", ID: "4", Summary: "four", Weight: 3},
		},
		Controls: []controlcatalog.Control{
			control("AC-1", "DORA: Article 6, Framework;\nRTS on ICT risk management framework, Article 6, Encryption."),
			control("AC-2", "DORA: Article 6, Framework.\nArticle 9, Protection."),
			control("AC-3", "DORA: Article 12, Backup."),
			control("AC-4", "PCI DSS: Control 7: Restrict."),
		},
		Links: []nfrlink.LinkRow{
			link("1", "AC-1"), link("1", "AC-2"),
			link("2", "AC-2"),
			link("3", "AC-3"),
			link("4", "AC-4"),
		},
	}
}

func TestDORAReportCounts(t *testing.T) {
	report := DORAReportFor(doraFixture(), Options{Scope: []string{"1", "2", "4"}, Scoped: true, Selected: []string{"1", "2", "3"}, Partial: []string{"2"}})

	if report.ScopeNFRs != 3 || report.Exceptions != 2 {
		t.Errorf("scope %d, exceptions %d: want 3 and 2, NFR 3 being out of scope", report.ScopeNFRs, report.Exceptions)
	}
	if report.DORAInScope != 2 || report.DORAWithExceptions != 2 || report.RTSInScope != 1 || report.RTSWithExceptions != 1 {
		t.Errorf("the counts of references are wrong: %+v", report)
	}
	got := []string{}
	for _, row := range report.Rows {
		got = append(got, row.Regulation+" "+row.Reference+" "+row.Title+" "+strings.Join([]string{strconv.Itoa(row.NFRsInScope), strconv.Itoa(row.ControlsInScope), strconv.Itoa(row.Exceptions), strconv.Itoa(row.MissedControls)}, "/"))
	}
	want := []string{
		// NFR 1 reaches it through two controls and counts once; NFR 2,
		// partial, counts as one. The two controls citing it are both missed.
		"DORA Article 6 Framework 2/2/2/2",
		"DORA Article 9 Protection 2/1/2/1",
		// RTS Article 6 is not DORA Article 6. DORA Article 12 is reached only
		// by an NFR out of scope, so it is not listed.
		rtsRiskManagement + " Article 6 Encryption 1/1/1/1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	nothing := DORAReportFor(doraFixture(), Options{})
	if nothing.Exceptions != 0 || len(nothing.Rows) != 4 {
		t.Errorf("the whole catalog with nothing selected: %+v", nothing)
	}
	for _, row := range nothing.Rows {
		if row.Exceptions != 0 || row.MissedControls != 0 {
			t.Errorf("%s has exceptions with nothing selected: %+v", row.Reference, row)
		}
	}
}

// A control is in scope when an NFR in scope is linked to it. With NFR 2
// alone in scope, AC-1 is out: only NFR 1, out of scope, is linked to it.
func TestDORADetailListsWhatTheRowCounts(t *testing.T) {
	opts := Options{Scope: []string{"2"}, Scoped: true}
	report := DORAReportFor(doraFixture(), opts)
	if len(report.Rows) != 2 || report.Rows[0].Reference != "Article 6" || report.Rows[0].ControlsInScope != 1 {
		t.Fatalf("rows: %+v", report.Rows)
	}
	detail, ok := DORADetailFor(doraFixture(), opts, "DORA", "Article 6")
	if !ok || len(detail.NFRs) != 1 || detail.NFRs[0].Key != "2" || len(detail.Controls) != 1 || detail.Controls[0].ID != "AC-2" {
		t.Errorf("detail: %+v, %v", detail, ok)
	}
	if _, ok := DORADetailFor(doraFixture(), opts, "DORA", "Article 12"); ok {
		t.Error("a citation only an NFR out of scope reaches has no detail")
	}
	if _, ok := DORADetailFor(doraFixture(), opts, "DORA", "Article 99"); ok {
		t.Error("a citation nothing reaches has no detail")
	}

	selected := Options{Selected: []string{"1", "3"}, Partial: []string{"1"}}
	whole, ok := DORADetailFor(doraFixture(), selected, "DORA", "Article 6")
	if !ok || len(whole.NFRs) != 2 {
		t.Fatalf("detail: %+v, %v", whole, ok)
	}
	if first := whole.NFRs[0]; first.Key != "1" || !first.Excepted || !first.Partial {
		t.Errorf("NFR 1 is a partial exception: %+v", first)
	}
	if second := whole.NFRs[1]; second.Key != "2" || second.Excepted {
		t.Errorf("NFR 2 is not an exception: %+v", second)
	}
	if !whole.Controls[0].Missed || !whole.Controls[1].Missed {
		t.Errorf("NFR 1 is linked to both: %+v", whole.Controls)
	}
}

// Over the shipped catalog, every row's detail lists exactly what it counts.
func TestDORADetailAgreesWithEveryRow(t *testing.T) {
	catalog := seededCatalog(t)
	opts := Options{}
	for i, nfr := range catalog.NFRs {
		if i%9 == 4 {
			opts.Selected = append(opts.Selected, nfr.Key)
		}
		if i%3 != 1 {
			opts.Scope = append(opts.Scope, nfr.Key)
		}
	}
	opts.Scoped = true
	report := DORAReportFor(catalog, opts)
	if len(report.Rows) == 0 {
		t.Fatal("no rows")
	}
	for _, row := range report.Rows {
		detail, ok := DORADetailFor(catalog, opts, row.Regulation, row.Reference)
		if !ok {
			t.Errorf("%s %s has no detail", row.Regulation, row.Reference)
			continue
		}
		excepted, missed := 0, 0
		for _, nfr := range detail.NFRs {
			if nfr.Excepted {
				excepted++
			}
		}
		for _, control := range detail.Controls {
			if control.Missed {
				missed++
			}
		}
		if len(detail.NFRs) != row.NFRsInScope || len(detail.Controls) != row.ControlsInScope || excepted != row.Exceptions || missed != row.MissedControls {
			t.Errorf("%s %s counts %+v, its detail lists %d NFRs (%d excepted) and %d controls (%d missed)",
				row.Regulation, row.Reference, row, len(detail.NFRs), excepted, len(detail.Controls), missed)
		}
	}
}
