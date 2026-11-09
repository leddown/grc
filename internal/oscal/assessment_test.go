package oscal

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func readAssessmentPlan(t *testing.T, body []byte) AssessmentPlan {
	t.Helper()
	var doc struct {
		AssessmentPlan AssessmentPlan `json:"assessment-plan"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.AssessmentPlan
}

func readAssessmentResults(t *testing.T, body []byte) AssessmentResults {
	t.Helper()
	var doc struct {
		AssessmentResults AssessmentResults `json:"assessment-results"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.AssessmentResults
}

func readPOAM(t *testing.T, body []byte) POAM {
	t.Helper()
	var doc struct {
		POAM POAM `json:"plan-of-action-and-milestones"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.POAM
}

func grcValues(props *[]Property, name string) []string {
	var out []string
	if props == nil {
		return nil
	}
	for _, p := range *props {
		if p.Ns == Namespace && p.Name == name {
			out = append(out, p.Value)
		}
	}
	return out
}

func selectedIDs(sel AssessedControls) []string {
	var out []string
	if sel.IncludeControls != nil {
		for _, c := range *sel.IncludeControls {
			out = append(out, c.ControlId)
		}
	}
	return out
}

func TestAssessmentPlanExport(t *testing.T) {
	in := SamplePlan()
	body, err := ExportAssessmentPlan(in, SampleCatalogHrefs())
	if err != nil {
		t.Fatal(err)
	}
	ap := readAssessmentPlan(t, body)

	if !ap.Metadata.LastModified.Equal(in.ApprovedAt) || ap.Metadata.Version != "v2" || ap.Metadata.Title != in.Title {
		t.Errorf("metadata = %q %q %v", ap.Metadata.Title, ap.Metadata.Version, ap.Metadata.LastModified)
	}
	if ap.Metadata.Parties != nil || ap.Metadata.Roles != nil || ap.Metadata.ResponsibleParties != nil {
		t.Error("the plan invents parties or roles")
	}

	// import-ssp is the approval snapshot, titled as what it is, with its hash.
	snap := resolveResource(t, ap.BackMatter, ap.ImportSsp.Href)
	if snap.Href != in.SnapshotHref || snap.MediaType != JSONMediaType || snap.Hashes == nil ||
		(*snap.Hashes)[0].Algorithm != "SHA-256" || (*snap.Hashes)[0].Value != in.SnapshotSHA256 {
		t.Errorf("snapshot rlink = %+v", snap)
	}
	for _, r := range *ap.BackMatter.Resources {
		if "#"+r.UUID == ap.ImportSsp.Href && (!strings.Contains(r.Title, "not a system security plan") || !reflect.DeepEqual(grcValues(r.Props, "kind"), []string{"approval-snapshot"})) {
			t.Errorf("snapshot resource = %q %v", r.Title, r.Props)
		}
	}
	if !strings.Contains(ap.ImportSsp.Remarks, "grc holds no system security plan") {
		t.Errorf("import-ssp remarks = %q", ap.ImportSsp.Remarks)
	}

	// One selection per catalog, never include-all; the control that did not
	// resolve is left out and named.
	sels := ap.ReviewedControls.ControlSelections
	if len(sels) != 2 {
		t.Fatalf("control selections = %+v", sels)
	}
	if got := selectedIDs(sels[0]); !reflect.DeepEqual(got, []string{"cm-8", "ra-2", "ac-2.1", "ac-2"}) {
		t.Errorf("800-53 selection = %v", got)
	}
	if got := selectedIDs(sels[1]); !reflect.DeepEqual(got, []string{"nfr-2"}) {
		t.Errorf("NFR selection = %v", got)
	}
	for i, s := range sels {
		if s.IncludeAll != nil {
			t.Errorf("selection %d includes all", i)
		}
		if s.Links == nil || resolveResource(t, ap.BackMatter, (*s.Links)[0].Href).Href == "" {
			t.Errorf("selection %d does not point at its catalog", i)
		}
	}
	if !strings.Contains(ap.ReviewedControls.Remarks, "ZZ-9 (NFR-04)") {
		t.Errorf("reviewed-controls remarks = %q", ap.ReviewedControls.Remarks)
	}

	defs := ap.LocalDefinitions
	objectives := *defs.ObjectivesAndMethods
	if len(objectives) != 3 {
		t.Fatalf("an objective per attribute that names a reference: %d", len(objectives))
	}
	nfr04 := objectives[1]
	if nfr04.ControlId != "ac-2.1" || nfr04.Parts[0].ID != "nfr-04_obj" || nfr04.Parts[0].Name != "assessment-objective" ||
		nfr04.Parts[0].Prose != "No leaver is active." || !reflect.DeepEqual(grcValues(nfr04.Props, "also-reviews"), []string{"nfr-2"}) {
		t.Errorf("NFR-04 objective = %+v", nfr04)
	}
	if p := objectives[0].Parts; len(p) != 2 || p[1].Name != "evidence-requested" || p[1].Ns != Namespace {
		t.Errorf("NFR-01 objective parts = %+v", p)
	}

	activities := *defs.Activities
	if len(activities) != 4 {
		t.Fatalf("an activity per attribute: %d", len(activities))
	}
	for i, want := range []string{"EXAMINE", "TEST", "INTERVIEW", "EXAMINE"} {
		var method string
		for _, p := range *activities[i].Props {
			if p.Name == "method" && p.Ns == "" {
				method = p.Value
			}
		}
		if method != want || grcValues(activities[i].Props, "method")[0] != in.Attributes[i].Method {
			t.Errorf("%s method = %q, want %q", in.Attributes[i].Ref, method, want)
		}
	}
	if activities[3].RelatedControls != nil {
		t.Error("an attribute that names nothing relates to controls")
	}

	items := *defs.InventoryItems
	if len(items) != 2 || items[0].Description != "Core ledger" || !reflect.DeepEqual(grcValues(items[0].Props, "owner"), []string{"Ledger owner"}) {
		t.Errorf("inventory items = %+v", items)
	}
	// An item is the same inventory item in every version of the plan.
	other := in
	other.Version = 3
	again, _ := ExportAssessmentPlan(other, SampleCatalogHrefs())
	if (*readAssessmentPlan(t, again).LocalDefinitions.InventoryItems)[0].UUID != items[0].UUID {
		t.Error("the inventory item's UUID changes with the version")
	}

	// T2 has no items yet, so it is not a subject.
	subjects := *ap.AssessmentSubjects
	if len(subjects) != 1 || subjects[0].IncludeAll != nil || len(*subjects[0].IncludeSubjects) != 2 ||
		!reflect.DeepEqual(grcValues(subjects[0].Props, "code"), []string{"T1"}) {
		t.Errorf("subjects = %+v", subjects)
	}

	tasks := *ap.Tasks
	if len(tasks) != 3 || tasks[0].Type != "action" || tasks[0].Timing.WithinDateRange == nil ||
		tasks[1].Type != "milestone" || tasks[1].Timing.OnDate == nil || tasks[2].Timing.OnDate == nil {
		t.Errorf("tasks = %+v", tasks)
	}

	terms := map[string]AssessmentPart{}
	for _, p := range *ap.TermsAndConditions.Parts {
		if p.Ns != Namespace {
			t.Errorf("terms part %s is not in the grc namespace", p.Name)
		}
		terms[p.Name] = p
	}
	for _, name := range []string{"parameters", "sampling", "quarterly-cycle", "raci", "cadence", "escalations", "regulatory-basis", "decisions-requested"} {
		if _, ok := terms[name]; !ok {
			t.Errorf("no %s part", name)
		}
	}
	raci := (*terms["raci"].Parts)[0]
	if !reflect.DeepEqual(grcValues(raci.Props, "accountable"), []string{"CISO", "Board"}) ||
		!reflect.DeepEqual(grcValues(raci.Props, "responsible"), []string{"CISO"}) ||
		!reflect.DeepEqual(grcValues(raci.Props, "informed"), []string{"Internal Audit"}) {
		t.Errorf("RACI props = %+v", raci.Props)
	}
	t2 := (*terms["sampling"].Parts)[1]
	if !reflect.DeepEqual(grcValues(t2.Props, "cycle-years"), []string{"3"}) || !reflect.DeepEqual(grcValues(t2.Props, "takes"), []string{"High", "Critical"}) {
		t.Errorf("T2 sampling = %+v", t2.Props)
	}

	// The same snapshot gives the same bytes.
	if twice, _ := ExportAssessmentPlan(in, SampleCatalogHrefs()); string(twice) != string(body) {
		t.Error("two exports of one snapshot differ")
	}
}

func TestAssessmentPlanRefusesAScriptWithNoReferences(t *testing.T) {
	in := SamplePlan()
	for i := range in.Attributes {
		in.Attributes[i].ControlIDs, in.Attributes[i].NFRKey = nil, ""
	}
	in.Attributes[0].ControlIDs, in.Attributes[0].UnresolvedControls = []string{"ZZ-1"}, []string{"ZZ-1"}
	if _, err := ExportAssessmentPlan(in, SampleCatalogHrefs()); !errors.Is(err, ErrNoReviewedControls) {
		t.Errorf("err = %v, want ErrNoReviewedControls", err)
	}
}

func TestAssessmentResultsExport(t *testing.T) {
	q := SampleQuarterResults()
	apRef := SampleAssessmentPlanRef()
	body, err := ExportAssessmentResults(q, apRef, SampleCatalogHrefs())
	if err != nil {
		t.Fatal(err)
	}
	ar := readAssessmentResults(t, body)
	if got := resolveResource(t, ar.BackMatter, ar.ImportAp.Href); got.Href != apRef.Href || got.MediaType != OSCALMediaType {
		t.Errorf("import-ap = %+v", got)
	}
	if len(ar.Results) != 1 {
		t.Fatalf("results = %d", len(ar.Results))
	}
	r := ar.Results[0]
	if r.Title != "Quarter 1" || !r.Start.Equal(time.Date(2027, 3, 2, 0, 0, 0, 0, time.UTC)) || r.End == nil {
		t.Errorf("result = %q %v %v", r.Title, r.Start, r.End)
	}

	// Signed-off tests only: four results on T27-Q1-002 and two on
	// T27-Q1-001 (its blank is skipped); T27-Q1-003 is not signed off.
	obs := *r.Observations
	if len(obs) != 6 {
		t.Fatalf("observations = %d, want 6", len(obs))
	}
	for _, o := range obs {
		if grcValues(o.Props, "test-ref")[0] == "T27-Q1-003" {
			t.Error("a test QA has not signed off is exported")
		}
	}
	first := obs[0]
	if first.Title != "T27-Q1-001 / NFR-01" || !reflect.DeepEqual(first.Methods, []string{"EXAMINE"}) ||
		!first.Collected.Equal(time.Date(2027, 3, 20, 0, 0, 0, 0, time.UTC)) || (*first.Types)[0] != "control-objective" {
		t.Errorf("first observation = %+v", first)
	}

	// A finding per Fail with an objective in the plan; NFR-06 names nothing,
	// so its Fail stays an observation.
	findings := *r.Findings
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(findings))
	}
	raised := findings[0]
	if raised.Target.Type != "objective-id" || raised.Target.TargetId != "nfr-01_obj" || raised.Target.Status.State != "not-satisfied" ||
		raised.Target.Status.Reason != "fail" || !reflect.DeepEqual(grcValues(raised.Props, "audit-finding"), []string{"AF-2027-004"}) ||
		(*raised.RelatedObservations)[0].ObservationUuid != first.UUID {
		t.Errorf("raised finding = %+v", raised)
	}
	if findings[1].Props != nil && len(grcValues(findings[1].Props, "audit-finding")) > 0 {
		t.Error("an unraised Fail names an audit finding")
	}
	for _, want := range []string{"T27-Q1-003", "APP-007", "1 Fail (T27-Q1-002 / NFR-06)", "Not yet raised in Audit Findings: T27-Q1-002 / NFR-04", "1 item tested"} {
		if !strings.Contains(r.Remarks, want) {
			t.Errorf("remarks lack %q: %s", want, r.Remarks)
		}
	}

	// Reviewed controls come from Passes and Fails only.
	if got := selectedIDs(r.ReviewedControls.ControlSelections[0]); !reflect.DeepEqual(got, []string{"cm-8", "ra-2", "ac-2.1"}) {
		t.Errorf("reviewed = %v", got)
	}

	// Every finding targets an objective the plan defines, and every subject
	// is an inventory item of the plan or of the result.
	plan, _ := ExportAssessmentPlan(apRef.Plan, SampleCatalogHrefs())
	ap := readAssessmentPlan(t, plan)
	objectiveIDs := map[string]bool{}
	for _, o := range *ap.LocalDefinitions.ObjectivesAndMethods {
		for _, p := range o.Parts {
			objectiveIDs[p.ID] = true
		}
	}
	itemIDs := map[string]bool{}
	for _, it := range *ap.LocalDefinitions.InventoryItems {
		itemIDs[it.UUID] = true
	}
	if r.LocalDefinitions == nil || len(*r.LocalDefinitions.InventoryItems) != 1 || (*r.LocalDefinitions.InventoryItems)[0].Description != "New gateway" {
		t.Fatalf("local items = %+v", r.LocalDefinitions)
	}
	for _, it := range *r.LocalDefinitions.InventoryItems {
		if itemIDs[it.UUID] {
			t.Error("the result redefines an item the plan lists")
		}
		itemIDs[it.UUID] = true
	}
	for _, f := range findings {
		if !objectiveIDs[f.Target.TargetId] {
			t.Errorf("finding targets %s, which the plan does not define", f.Target.TargetId)
		}
	}
	for _, o := range obs {
		if !itemIDs[(*o.Subjects)[0].SubjectUuid] {
			t.Errorf("%s's subject is not an inventory item", o.Title)
		}
	}

	none := q
	none.Tests = []ExportTest{q.Tests[2]}
	if _, err := ExportAssessmentResults(none, apRef, SampleCatalogHrefs()); !errors.Is(err, ErrNoSignedOffTests) {
		t.Errorf("no signed-off test: %v", err)
	}
	refless := q
	refless.Tests = []ExportTest{{Ref: "T1", QADate: "2027-03-27", FieldworkCompletedOn: "2027-03-27", ItemRef: "APP-001",
		Results: []ExportResult{{AttrRef: "NFR-06", Result: "Pass"}}}}
	if _, err := ExportAssessmentResults(refless, apRef, SampleCatalogHrefs()); !errors.Is(err, ErrNoReviewedResults) {
		t.Errorf("results on attributes naming nothing: %v", err)
	}
}

func TestPOAMExport(t *testing.T) {
	in := SamplePOAM()
	body, err := ExportPOAM(in)
	if err != nil {
		t.Fatal(err)
	}
	p := readPOAM(t, body)
	if p.SystemId == nil || p.SystemId.ID != POAMSystemID || p.SystemId.IdentifierType != Namespace || p.ImportSsp != nil {
		t.Errorf("system-id = %+v", p.SystemId)
	}
	byRef := map[string]POAMItem{}
	for _, it := range p.PoamItems {
		byRef[grcValues(it.Props, "reference")[0]] = it
	}
	if len(byRef) != 6 {
		t.Errorf("items = %d, want 6 (no draft, no closed)", len(byRef))
	}
	for _, ref := range []string{"AF-2027-002", "AF-2026-005"} {
		if _, ok := byRef[ref]; ok {
			t.Errorf("%s should not be in the default POA&M", ref)
		}
	}

	open := byRef["AF-2027-001"]
	for _, want := range []string{"Criteria: Access is removed", "Condition: Three leavers were still active.\nOne had admin rights.", "Cause: HR feed failed.", "Effect: Former staff"} {
		if !strings.Contains(open.Description, want) {
			t.Errorf("description lacks %q", want)
		}
	}
	if !reflect.DeepEqual(grcValues(open.Props, "control"), []string{"AC-2", "AC-6"}) || grcValues(open.Props, "severity")[0] != "High" ||
		grcValues(open.Props, "finding-type")[0] != "Control Deficiency" || grcValues(open.Props, "extension-count")[0] != "1" {
		t.Errorf("props = %+v", open.Props)
	}

	risks := map[string]Risk{}
	for _, r := range *p.Risks {
		risks[grcValues(r.Props, "reference")[0]] = r
	}
	for ref, want := range map[string]string{
		"AF-2027-001": "open", "AF-2026-006": "open", "AF-2026-007": "remediating", "AF-2027-003": "remediating",
		"AF-2026-011": "deviation-approved", "AF-2026-010": "open",
	} {
		if got := risks[ref].Status; got != want {
			t.Errorf("%s risk status = %q, want %q", ref, got, want)
		}
	}
	if got := grcValues(byRef["AF-2026-010"].Props, "acceptance-expired"); !reflect.DeepEqual(got, []string{"yes"}) {
		t.Errorf("the expired acceptance is not marked: %v", got)
	}
	if !strings.Contains(byRef["AF-2026-010"].Remarks, "Risk acceptance rationale: Decommissioned") {
		t.Errorf("remarks = %q", byRef["AF-2026-010"].Remarks)
	}
	risk := risks["AF-2027-001"]
	if risk.Statement != "Former staff could reach the ledger." || risk.Deadline == nil || !risk.Deadline.Equal(time.Date(2027, 7, 18, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("risk = %+v", risk)
	}
	rem := *risk.Remediations
	if len(rem) != 2 || rem[0].Lifecycle != "recommendation" || rem[1].Lifecycle != "planned" || len(*rem[1].Tasks) != 2 {
		t.Fatalf("remediations = %+v", rem)
	}
	task := (*rem[1].Tasks)[0]
	if task.Type != "action" || task.Timing == nil || task.Timing.OnDate == nil || !task.Timing.OnDate.Date.Equal(time.Date(2027, 5, 30, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("action task = %+v", task)
	}
	if got := (*risks["AF-2027-003"].Remediations)[0].Lifecycle; got != "completed" {
		t.Errorf("a plan of validated and cancelled actions is %q", got)
	}
	for _, o := range *p.Observations {
		if !reflect.DeepEqual(o.Methods, []string{"UNKNOWN"}) {
			t.Errorf("observation methods = %v", o.Methods)
		}
	}

	closed := in
	closed.Status = FindingClosed
	body, err = ExportPOAM(closed)
	if err != nil {
		t.Fatal(err)
	}
	if items := readPOAM(t, body).PoamItems; len(items) != 1 || items[0].Title != "Closed finding" {
		t.Errorf("?status=Closed = %+v", items)
	}
	for filter, want := range map[string]error{FindingDraft: ErrDraftNotExported, "Done": ErrUnknownFindingStatus} {
		bad := in
		bad.Status = filter
		if _, err := ExportPOAM(bad); !errors.Is(err, want) {
			t.Errorf("?status=%s: %v", filter, err)
		}
	}
	empty := in
	empty.Status = FindingOpen
	empty.Findings = in.Findings[1:2]
	if _, err := ExportPOAM(empty); !errors.Is(err, ErrNoFindings) {
		t.Errorf("nothing to carry: %v", err)
	}
}

func TestPropValuesAreOneLine(t *testing.T) {
	props := GRCProps(NamedValue{Name: "a", Value: " two\nlines  here "}, NamedValue{Name: "b", Value: " \n "})
	if len(props) != 1 || props[0].Value != "two lines here" {
		t.Errorf("props = %+v", props)
	}
}
