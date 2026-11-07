package assuranceplan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// sampleSeed replaces the freeze's random seed so the year's draws, and the
// samples written from them, are the same on every run.
const sampleSeed = "5a1d0c3e9b7f2468ace13579bdf02468ace13579bdf02468ace13579bdf02468"

func at(svc *Service, date string) {
	t, _ := time.Parse("2006-01-02", date)
	svc.now = func() time.Time { return t.Add(9 * time.Hour) }
}

// sampleYear runs the Appendix A plan through the given number of quarters:
// every quarter drawn and tested, the waves attested, a NFR-04 fail in Tier 1
// in Q1 raised as a finding and another in Q3 that makes it a thematic risk,
// a NFR-11 fail in Tier 2 that extends the sample, and the last two tests of
// the last quarter still in fieldwork.
func sampleYear(t *testing.T, svc *Service, quarters int) Plan {
	t.Helper()
	at(svc, "2026-11-06")
	p := frozenExample(t, svc)
	p.SelectionSeed = sampleSeed
	if err := svc.repo.SaveHeader(p, nil, nil); err != nil {
		t.Fatal(err)
	}
	if quarters == 0 {
		return mustGet(t, svc, p.ID)
	}
	at(svc, "2027-01-04")
	if _, err := svc.Activate(p.ID, "alice"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	p = mustGet(t, svc, p.ID)
	nfr04, nfr11 := attrByRef(t, p, "NFR-04"), attrByRef(t, p, "NFR-11")
	pop := mustPopulation(t, svc, p.ID)
	for q := 1; q <= quarters; q++ {
		qr := p.Quarters[q-1]
		at(svc, qr.SelectionDate)
		if _, err := svc.SelectQuarter(p.ID, q, "alice"); err != nil {
			t.Fatalf("SelectQuarter %d: %v", q, err)
		}
		tests, err := svc.CreateTests(p.ID, CreateTestsInput{Quarter: q, Tester: "tess", EvidenceRequestedOn: qr.FieldworkStart}, "alice")
		if err != nil {
			t.Fatalf("CreateTests %d: %v", q, err)
		}
		attested := 0
		for _, it := range pop {
			if it.WaveQuarter != q {
				continue
			}
			on, dq, by := qr.LOD1Deadline, DQAccepted, qr.LOD1Deadline
			attested++
			if attested%9 == 0 {
				continue
			}
			patch := PopulationPatch{AttestedOn: &on, DQStatus: &dq, DQReviewedOn: &by}
			if _, err := svc.UpdatePopulationItem(p.ID, it.ID, patch, "alice"); err != nil {
				t.Fatal(err)
			}
		}
		at(svc, qr.FieldworkEnd)
		firstT1, firstT2 := true, true
		for i, x := range tests {
			if q == quarters && i >= len(tests)-2 {
				break
			}
			except := map[string]string{}
			switch {
			case x.StratumCode == "T1" && firstT1 && (q == 1 || q == 3):
				except["NFR-04"] = ResultFail
				firstT1 = false
			case x.StratumCode == "T2" && firstT2 && q == 2:
				except["NFR-11"] = ResultFail
				firstT2 = false
			}
			saved, err := svc.SaveTest(p.ID, x.ID, TestInput{Tester: "tess", EvidenceReceivedOn: qr.FieldworkStart, FieldworkCompletedOn: qr.FieldworkEnd,
				EvidenceSummary: "Evidence received and walked through with the asset owner.", Results: allPass(p, except)}, "tess")
			if err != nil {
				t.Fatalf("SaveTest %s: %v", x.TestRef, err)
			}
			if except["NFR-04"] == ResultFail {
				r := resultFor(t, saved.Test, nfr04.ID)
				if _, err := svc.RaiseFinding(p.ID, r.ID, RaiseInput{Severity: "High", Owner: "Head of IAM", DueDate: qr.ReportDate}, "tess"); err != nil {
					t.Fatalf("RaiseFinding: %v", err)
				}
			}
			if except["NFR-11"] == ResultFail {
				r := resultFor(t, saved.Test, nfr11.ID)
				if _, err := svc.RaiseFinding(p.ID, r.ID, RaiseInput{Severity: "Medium", Owner: "Head of Infrastructure", DueDate: qr.ReportDate}, "tess"); err != nil {
					t.Fatalf("RaiseFinding: %v", err)
				}
			}
			if _, err := svc.SignOffQA(p.ID, x.ID, "quinn"); err != nil {
				t.Fatalf("SignOffQA %s: %v", x.TestRef, err)
			}
		}
		if q == 3 {
			if _, err := svc.PushThematic(p.ID, ThematicInput{AttributeID: nfr04.ID, Stratum: "T1", Owner: "CISO", Likelihood: 3, Impact: 4}, "alice"); err != nil {
				t.Fatalf("PushThematic: %v", err)
			}
		}
		p = mustGet(t, svc, p.ID)
	}
	return p
}

func TestPackPayload(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 0)
	at(svc, "2026-12-01")
	pack, err := svc.PackData(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Population) != 0 || len(pack.Tests) != 0 || pack.Population == nil || pack.Tests == nil {
		t.Errorf("the pack carries %d items and %d tests; it prints neither", len(pack.Population), len(pack.Tests))
	}
	if len(pack.Pack.Roadmap) != 5 || pack.Pack.Roadmap[0].Label != "Mobilise" || pack.Pack.Roadmap[4].Label != "Q4" {
		t.Errorf("roadmap = %+v", pack.Pack.Roadmap)
	}
	for _, card := range pack.Pack.Roadmap {
		if len(card.Lines) == 0 || card.Period == "" {
			t.Errorf("roadmap card %s is empty: %+v", card.Label, card)
		}
	}
	if len(pack.Pack.DeliveryRisks) != 0 {
		t.Errorf("delivery risks on a plan that passes its checks: %v", pack.Pack.DeliveryRisks)
	}
	if pack.Pack.PeakQuarter != 2 {
		t.Errorf("peak quarter = Q%d, want Q2 (81.8 person-days)", pack.Pack.PeakQuarter)
	}
	if n := len(pack.Pack.NextSteps); n == 0 || n > 6 {
		t.Errorf("%d next steps", n)
	}
	for _, st := range pack.Pack.NextSteps {
		if st.Status == StepComplete {
			t.Errorf("next steps list the completed %s", st.StepRef)
		}
	}
	if len(pack.DecisionsRequested) == 0 || len(pack.RegulatoryBasis) != 5 || len(pack.Strata) != 2 || len(pack.Attributes) != 16 {
		t.Errorf("pack: %d decisions, %d basis rows, %d strata, %d attributes", len(pack.DecisionsRequested), len(pack.RegulatoryBasis), len(pack.Strata), len(pack.Attributes))
	}
	checkSample(t, "assurance-plan-sample.json", pack)
}

func TestQuarterReportPayload(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 1)
	at(svc, "2027-04-12")
	r, err := svc.QuarterReport(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reference != "AP-2027-001-Q1" || r.Title != "Q1 2027 progress report" || r.Limitations == "" || r.ExecutiveSummary == "" {
		t.Errorf("header = %q %q, limitations %d chars", r.Reference, r.Title, len(r.Limitations))
	}
	if len(r.Findings) != 1 || r.Findings[0].Severity != "high" || r.Findings[0].Owner != "Head of IAM" || r.Findings[0].ControlID == "" || r.Findings[0].Observation == "" {
		t.Errorf("findings = %+v", r.Findings)
	}
	heads := []string{}
	for _, s := range r.Sections {
		heads = append(heads, s.Heading)
		if s.Tables == nil {
			t.Errorf("section %q has null tables", s.Heading)
		}
	}
	if !slices.Equal(heads, []string{"Coverage against plan", "Testing in Q1", "Deviations so far this year", "Resourcing", "Next quarter"}) {
		t.Errorf("sections = %v", heads)
	}
	tests := r.Sections[1].Tables[0]
	if len(tests.Rows) != 17 || len(tests.Rows[0]) != len(tests.Columns) {
		t.Errorf("Q1 test table: %d rows, %d columns", len(tests.Rows), len(tests.Columns))
	}
	if !strings.Contains(r.Sections[1].Body, "signed off 15") {
		t.Errorf("testing body = %q", r.Sections[1].Body)
	}
	if _, err := svc.QuarterReport(p.ID, 5); err == nil {
		t.Error("Q5 report built")
	}
	checkSample(t, "assurance-quarterly-sample.json", r)
}

func TestAnnualReportPayload(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 4)
	at(svc, "2028-01-14")
	r, err := svc.AnnualReport(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Subtitle, "RTS (EU) 2024/1774 Art. 27") || !strings.Contains(r.ExecutiveSummary, "input to the ICT risk management framework review report") {
		t.Errorf("the annual does not say it is an input to the RMF review: %q", r.Subtitle)
	}
	if len(r.Findings) != 3 {
		t.Errorf("%d findings, want 3", len(r.Findings))
	}
	var conclusion string
	for _, s := range r.Sections {
		if s.Heading == "Conclusion" {
			conclusion = s.Body
		}
	}
	for _, want := range []string{"NFR-04", "not effective under the deviation rule", "Testing is incomplete"} {
		if !strings.Contains(conclusion, want) {
			t.Errorf("conclusion lacks %q:\n%s", want, conclusion)
		}
	}
	if !strings.Contains(r.Limitations, "does not replace the work of the other assurance plans") {
		t.Errorf("limitations = %q", r.Limitations)
	}
	checkSample(t, "assurance-annual-sample.json", r)

	at(svc, "2028-01-14")
	q1, err := svc.QuarterReport(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range q1.Sections {
		if s.Heading != "Deviations so far this year" {
			continue
		}
		rows := s.Tables[0].Rows
		if len(rows) != 1 || rows[0][0] != "NFR-04" || rows[0][3] != "1" || rows[0][4] != "1 deviation – extend sample" {
			t.Errorf("Q1 report read in January 2028 states later quarters' deviations: %v", rows)
		}
	}
}

// checkSample keeps templates/samples/<name> in step with its payload: the
// shape is compared on every run, and GRC_UPDATE_SAMPLES=1 writes the file
// afresh from the sample year.
func checkSample(t *testing.T, name string, payload any) {
	t.Helper()
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("..", "..", "templates", "samples", name)
	if os.Getenv("GRC_UPDATE_SAMPLES") == "1" {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (GRC_UPDATE_SAMPLES=1 writes it)", name, err)
	}
	if got, want := keyPaths(t, data), keyPaths(t, committed); !slices.Equal(got, want) {
		t.Errorf("%s no longer has the payload's shape; rerun with GRC_UPDATE_SAMPLES=1\nonly in payload: %v\nonly in sample: %v",
			name, missing(got, want), missing(want, got))
	}
}

func keyPaths(t *testing.T, data []byte) []string {
	t.Helper()
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				set[prefix+"."+k] = true
				walk(prefix+"."+k, child)
			}
		case []any:
			for _, child := range x {
				walk(prefix+"[]", child)
			}
		}
	}
	walk("", v)
	out := []string{}
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func missing(a, b []string) []string {
	out := []string{}
	for _, k := range a {
		if _, found := slices.BinarySearch(b, k); !found {
			out = append(out, k)
		}
	}
	return out
}
