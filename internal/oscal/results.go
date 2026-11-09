package oscal

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNoSignedOffTests is a quarter none of whose tests QA has signed off:
// there are no results to export yet.
var ErrNoSignedOffTests = errors.New("no test in the quarter is signed off by QA")

// ErrNoReviewedResults is a quarter whose signed-off tests have no Pass or
// Fail on an attribute that names a control or NFR, so the results review
// nothing OSCAL could list.
var ErrNoReviewedResults = errors.New("no signed-off result is on an attribute that names a control or NFR")

// ExportQuarterResults is one quarter of a plan's testing.
type ExportQuarterResults struct {
	Reference string
	PlanTitle string
	Quarter   int
	Committee string
	// FieldworkStart, SelectionDate and SelectedAt give the result's start, in
	// that order of preference; FieldworkEnd its end.
	FieldworkStart string
	FieldworkEnd   string
	SelectionDate  string
	SelectedAt     time.Time
	LastModified   time.Time

	// Attributes is the test script as it stands, for each result's method
	// and references.
	Attributes []ExportAttribute
	// Population is the plan's population as it stands, for an item tested
	// this quarter that the imported Assessment Plan does not list.
	Population []ExportItem
	// Tests are the quarter's tests, signed off or not, and Unopened the
	// items selected for it that have no test yet.
	Tests    []ExportTest
	Unopened []string
}

// ExportTest is the 2LoD test of one selected item.
type ExportTest struct {
	Ref                  string
	Type                 string
	Tester               string
	QAReviewer           string
	QADate               string
	FieldworkCompletedOn string
	EvidenceSummary      string
	ItemRef              string
	ItemName             string
	Stratum              string
	Basis                string
	Results              []ExportResult
}

// ExportResult is one attribute's outcome on one test. FindingRef is the
// Audit Findings record (AF-YYYY-NNN) a Fail was raised as.
type ExportResult struct {
	AttrRef    string
	Result     string
	Note       string
	FindingRef string
	Severity   string
}

// AssessmentPlanRef is the Assessment Plan a quarter's results import: the
// approval snapshot its sample was selected under, and where grc serves its
// export.
type AssessmentPlanRef struct {
	Plan  ExportPlan
	Href  string
	Title string
}

// ExportAssessmentResults writes one quarter as OSCAL Assessment Results:
// only the tests QA has signed off, an observation per attribute result and a
// finding per Fail. import-ap is grc's own Assessment Plan export for the
// version the quarter was selected under.
func ExportAssessmentResults(q ExportQuarterResults, ap AssessmentPlanRef, cats CatalogHrefs) ([]byte, error) {
	ref := strings.TrimSpace(q.Reference)
	if c, n, _ := planReviewed(ap.Plan.Attributes); len(c)+len(n) == 0 {
		return nil, ErrNoReviewedControls
	}
	objectives := map[string]bool{}
	for _, a := range ap.Plan.Attributes {
		if refs, _ := a.references(); len(refs) > 0 {
			objectives[strings.TrimSpace(a.Ref)] = true
		}
	}
	planItems := map[string]bool{}
	for _, it := range ap.Plan.Population {
		planItems[strings.TrimSpace(it.Ref)] = true
	}
	attrs := map[string]ExportAttribute{}
	for _, a := range ap.Plan.Attributes {
		attrs[strings.TrimSpace(a.Ref)] = a
	}
	for _, a := range q.Attributes {
		attrs[strings.TrimSpace(a.Ref)] = a
	}
	population := map[string]ExportItem{}
	for _, it := range q.Population {
		population[strings.TrimSpace(it.Ref)] = it
	}

	tests := append([]ExportTest(nil), q.Tests...)
	sort.SliceStable(tests, func(i, j int) bool { return tests[i].Ref < tests[j].Ref })
	var (
		observations                        []Observation
		findings                            []Finding
		localItems                          []InventoryItem
		notSignedOff, noObjective, unraised []string
		signedOff, passes, fails, na        int
		reviewedSeen                        = map[string]bool{}
		reviewedCtls, reviewedNFRs          []string
		localSeen                           = map[string]bool{}
	)
	for _, t := range tests {
		if strings.TrimSpace(t.QADate) == "" {
			notSignedOff = append(notSignedOff, t.Ref)
			continue
		}
		signedOff++
		item := strings.TrimSpace(t.ItemRef)
		if !planItems[item] && !localSeen[item] {
			localSeen[item] = true
			it, ok := population[item]
			if !ok {
				it = ExportItem{Ref: item, Name: t.ItemName, Stratum: t.Stratum}
			}
			localItems = append(localItems, inventoryItem(ref, it))
		}
		collected, ok := day(t.FieldworkCompletedOn)
		if !ok {
			collected, _ = day(t.QADate)
		}
		for _, r := range t.Results {
			result := strings.TrimSpace(r.Result)
			if result == "" {
				continue
			}
			attrRef := strings.TrimSpace(r.AttrRef)
			a := attrs[attrRef]
			key := ref + "\x00" + t.Ref + "\x00" + attrRef
			obs := testObservation(t, r, a, ref, key, collected)
			observations = append(observations, obs)
			switch result {
			case "Pass":
				passes++
			case "Fail":
				fails++
			default:
				na++
			}
			if result == "Pass" || result == "Fail" {
				refs, _ := a.references()
				for _, rr := range refs {
					if reviewedSeen[rr.id] {
						continue
					}
					reviewedSeen[rr.id] = true
					if rr.nfr {
						reviewedNFRs = append(reviewedNFRs, rr.id)
					} else {
						reviewedCtls = append(reviewedCtls, rr.id)
					}
				}
			}
			if result != "Fail" {
				continue
			}
			label := t.Ref + " / " + attrRef
			if !objectives[attrRef] {
				noObjective = append(noObjective, label)
				continue
			}
			if strings.TrimSpace(r.FindingRef) == "" {
				unraised = append(unraised, label)
			}
			findings = append(findings, testFinding(t, r, a, key, obs.UUID))
		}
	}
	if signedOff == 0 {
		return nil, ErrNoSignedOffTests
	}
	if len(reviewedCtls)+len(reviewedNFRs) == 0 {
		return nil, ErrNoReviewedResults
	}
	reviewed, catalogs := reviewedControls(reviewedCtls, reviewedNFRs, cats)
	reviewed.Description = "The controls and Security NFRs of the attributes with a Pass or a Fail this quarter."

	start := q.SelectedAt.UTC()
	for _, d := range []string{q.FieldworkStart, q.SelectionDate} {
		if t, ok := day(d); ok {
			start = t
			break
		}
	}
	result := Result{
		UUID:  UUID("result", ref+"\x00q"+strconv.Itoa(q.Quarter)),
		Title: "Quarter " + strconv.Itoa(q.Quarter),
		Description: fmt.Sprintf("%s signed off by QA in Q%d: %s, %s and %s.", plural(signedOff, "test", "tests"), q.Quarter,
			plural(passes, "pass", "passes"), plural(fails, "fail", "fails"), plural(na, "result not applicable", "results not applicable")),
		Start:            start,
		ReviewedControls: reviewed,
		Props: propsOrNil(GRCProps(
			NamedValue{Name: "quarter", Value: "Q" + strconv.Itoa(q.Quarter)},
			NamedValue{Name: "plan-version", Value: strconv.Itoa(ap.Plan.Version)},
		)),
		Remarks: resultRemarks(notSignedOff, q.Unopened, noObjective, unraised, len(localItems)),
	}
	if end, ok := day(q.FieldworkEnd); ok && !end.Before(start) {
		result.End = &end
	}
	if len(observations) > 0 {
		result.Observations = &observations
	}
	if len(findings) > 0 {
		result.Findings = &findings
	}
	if len(localItems) > 0 {
		result.LocalDefinitions = &LocalDefinitions{InventoryItems: &localItems}
	}

	plan := Resource{
		UUID:   UUID("resource", ap.Href),
		Title:  ap.Title,
		Rlinks: &[]ResourceLink{{Href: ap.Href, MediaType: OSCALMediaType}},
	}
	doc := struct {
		AssessmentResults *AssessmentResults `json:"assessment-results"`
	}{AssessmentResults: &AssessmentResults{
		Metadata: NewMetadata(MetadataInput{
			Title:        fmt.Sprintf("%s: Q%d results", strings.TrimSpace(q.PlanTitle), q.Quarter),
			LastModified: q.LastModified,
			Remarks: fmt.Sprintf("Exported from grc: the tests of Q%d of %s that QA has signed off. A signed-off test can be reopened, "+
				"so this document is dated by the export.", q.Quarter, ref),
			Props: []NamedValue{
				{Name: "reference", Value: ref},
				{Name: "quarter", Value: "Q" + strconv.Itoa(q.Quarter)},
				{Name: "committee", Value: q.Committee},
			},
		}),
		ImportAp: ImportAP{
			Href:    "#" + plan.UUID,
			Remarks: fmt.Sprintf("grc's Assessment Plan export of version %d of %s, the version this quarter's sample was selected under.", ap.Plan.Version, ref),
		},
		Results:    []Result{result},
		BackMatter: backMatter(append([]Resource{plan}, catalogs...)...),
	}}
	return finishDocument("assessment-results", ref+"/q"+strconv.Itoa(q.Quarter), &doc, &doc.AssessmentResults.UUID, &doc.AssessmentResults.Metadata, q.LastModified)
}

func testObservation(t ExportTest, r ExportResult, a ExportAttribute, planRef, key string, collected time.Time) Observation {
	method := assessmentMethod(a.Method)
	if method == "" {
		method = "UNKNOWN"
	}
	obs := Observation{
		UUID:        UUID("observation", key),
		Title:       oneLine(t.Ref + " / " + r.AttrRef),
		Description: firstNonEmpty(r.Note, r.Result),
		Methods:     []string{method},
		Types:       &[]string{"control-objective"},
		Collected:   collected,
		Subjects: &[]SubjectReference{{
			SubjectUuid: inventoryItemUUID(planRef, t.ItemRef),
			Type:        "inventory-item",
			Title:       oneLine(firstNonEmpty(t.ItemName, t.ItemRef)),
		}},
		Props: propsOrNil(GRCProps(
			NamedValue{Name: "result", Value: r.Result},
			NamedValue{Name: "attribute", Value: r.AttrRef},
			NamedValue{Name: "test-ref", Value: t.Ref},
			NamedValue{Name: "test-type", Value: t.Type},
			NamedValue{Name: "tester", Value: t.Tester},
			NamedValue{Name: "qa-reviewer", Value: t.QAReviewer},
			NamedValue{Name: "qa-date", Value: t.QADate},
			NamedValue{Name: "stratum", Value: t.Stratum},
			NamedValue{Name: "basis", Value: t.Basis},
		)),
	}
	if e := strings.TrimSpace(t.EvidenceSummary); e != "" {
		obs.RelevantEvidence = &[]RelevantEvidence{{Description: e}}
	}
	return obs
}

func testFinding(t ExportTest, r ExportResult, a ExportAttribute, key, observation string) Finding {
	f := Finding{
		UUID:        UUID("finding", key),
		Title:       oneLine(t.Ref + " / " + r.AttrRef + ": Fail"),
		Description: firstNonEmpty(r.Note, a.Requirement, "The attribute failed."),
		Target: FindingTarget{
			Type:     "objective-id",
			TargetId: ObjectiveID(r.AttrRef),
			Status:   ObjectiveStatus{State: "not-satisfied", Reason: "fail"},
		},
		RelatedObservations: &[]RelatedObservation{{ObservationUuid: observation}},
		Props: propsOrNil(GRCProps(
			NamedValue{Name: "audit-finding", Value: r.FindingRef},
			NamedValue{Name: "severity", Value: r.Severity},
		)),
	}
	if strings.TrimSpace(r.FindingRef) == "" {
		f.Remarks = "Not yet raised in Audit Findings."
	}
	return f
}

func resultRemarks(notSignedOff, unopened, noObjective, unraised []string, local int) string {
	var parts []string
	if local > 0 {
		parts = append(parts, fmt.Sprintf("The result's local-definitions list %s tested this quarter that the imported Assessment Plan does not: "+
			"they joined the population after that version was approved.", plural(local, "item", "items")))
	}
	if len(notSignedOff) > 0 {
		parts = append(parts, fmt.Sprintf("Left out, not yet signed off by QA: %s (%s).", plural(len(notSignedOff), "test", "tests"), strings.Join(notSignedOff, ", ")))
	}
	if len(unopened) > 0 {
		parts = append(parts, fmt.Sprintf("Selected with no test opened yet: %s (%s).", plural(len(unopened), "item", "items"), strings.Join(unopened, ", ")))
	}
	if len(noObjective) > 0 {
		parts = append(parts, fmt.Sprintf("Observations with no finding, because the attribute has no objective in the imported Assessment Plan "+
			"(it names no control or NFR, or was added after that version): %s (%s).", plural(len(noObjective), "Fail", "Fails"), strings.Join(noObjective, ", ")))
	}
	if len(unraised) > 0 {
		parts = append(parts, fmt.Sprintf("Not yet raised in Audit Findings: %s.", strings.Join(unraised, ", ")))
	}
	return strings.Join(parts, " ")
}
