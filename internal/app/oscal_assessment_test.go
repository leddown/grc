package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"grc/internal/assuranceplan"
	"grc/internal/auditfinding"
	"grc/internal/controlcatalog"
	"grc/internal/oscal"
	"grc/internal/securitynfr"
)

// oscalPlanServices are the Assurance Plans and Audit Findings services over
// the app's own database, wired as app.go wires them, to drive a plan through
// approval and testing before reading the exports over HTTP.
func oscalPlanServices(a *fullApp) (*assuranceplan.Service, *auditfinding.Service) {
	findings := auditfinding.NewService(auditfinding.NewSQLiteRepository(a.conn))
	plans := assuranceplan.NewService(assuranceplan.NewSQLiteRepository(a.conn)).WithIntegrations(assuranceplan.Integrations{
		Findings: findings,
		Controls: controlcatalog.NewService(controlcatalog.NewSQLiteRepository(a.conn), ""),
		NFRs:     securitynfr.NewService(securitynfr.NewSQLiteRepository(a.conn), ""),
	})
	return plans, findings
}

func approvedExample(t *testing.T, plans *assuranceplan.Service) assuranceplan.Plan {
	t.Helper()
	p, err := plans.Create(assuranceplan.CreateInput{Example: "dora-nfr-2027"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	csv, err := plans.ExamplePopulationCSV(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plans.ApplyPopulation(p.ID, assuranceplan.PopulationImport{CSV: csv}, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := plans.Submit(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := plans.Approve(p.ID, "bob", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	return p
}

func oscalStatus(t *testing.T, a *fullApp, path string, want int, fragment string) {
	t.Helper()
	rec := a.asAdmin(http.MethodGet, path, "")
	if rec.Code != want || !strings.Contains(rec.Body.String(), fragment) {
		t.Errorf("GET %s: %d %s, want %d with %q", path, rec.Code, rec.Body.String(), want, fragment)
	}
}

// A plan runs from approval through a quarter's testing to a raised finding,
// and each stage exports: the Assessment Plan from the approval snapshot,
// the quarter's signed-off results against it, and the finding in the POA&M.
func TestOSCALAssessmentExportsFollowAPlanThroughItsQuarter(t *testing.T) {
	a := newFullApp(t, Options{})
	plans, findings := oscalPlanServices(a)

	draft, err := plans.Create(assuranceplan.CreateInput{Example: "dora-nfr-2027"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/assurance-plans/%d/oscal/", draft.ID)
	oscalStatus(t, a, base+"assessment-plan.json", http.StatusConflict, "never been approved")
	oscalStatus(t, a, base+"assessment-results/q1.json", http.StatusConflict, "has not been selected")

	// Approved before the catalog is loaded, no reference resolves: there is
	// nothing the plan can honestly say it reviews.
	early := approvedExample(t, plans)
	oscalStatus(t, a, fmt.Sprintf("/assurance-plans/%d/oscal/assessment-plan.json", early.ID), http.StatusConflict, "names a control or NFR")

	shipped, err := os.ReadFile(filepath.Join("..", "data", "merged_nist_controls_master_replaced_from_controls_all.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := a.asAdmin(http.MethodPost, "/controls/import", string(shipped)); rec.Code != http.StatusOK {
		t.Fatalf("control import: %d %s", rec.Code, rec.Body.String())
	}
	p := approvedExample(t, plans)
	base = fmt.Sprintf("/assurance-plans/%d/oscal/", p.ID)

	body := oscalDownload(t, a, base+"assessment-plan.json", "assessment-plan-"+strings.ToLower(p.Reference)+"-v1")
	var ap struct {
		AssessmentPlan oscal.AssessmentPlan `json:"assessment-plan"`
	}
	if err := json.Unmarshal(body, &ap); err != nil {
		t.Fatal(err)
	}
	version, err := plans.Version(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := ap.AssessmentPlan.Metadata.LastModified.Format("2006-01-02T15:04:05Z07:00"); got != version.ApprovedAt {
		t.Errorf("last-modified %s, want the approval %s", got, version.ApprovedAt)
	}
	// The snapshot import-ssp names is the one the route serves, and its hash
	// is the hash of those bytes.
	var snapshot oscal.ResourceLink
	for _, r := range *ap.AssessmentPlan.BackMatter.Resources {
		if "#"+r.UUID == ap.AssessmentPlan.ImportSsp.Href {
			snapshot = (*r.Rlinks)[0]
		}
	}
	rec := a.asAdmin(http.MethodGet, snapshot.Href, "")
	sum := sha256.Sum256(rec.Body.Bytes())
	if rec.Code != http.StatusOK || snapshot.Hashes == nil || (*snapshot.Hashes)[0].Value != hex.EncodeToString(sum[:]) ||
		rec.Header().Get("X-Snapshot-SHA256") != (*snapshot.Hashes)[0].Value {
		t.Errorf("snapshot %s: %d, hash %v", snapshot.Href, rec.Code, snapshot.Hashes)
	}
	if objectives := *ap.AssessmentPlan.LocalDefinitions.ObjectivesAndMethods; len(objectives) != 16 {
		t.Errorf("objectives = %d, want one per attribute of the example", len(objectives))
	}
	if again := oscalDownload(t, a, base+"assessment-plan.json?version=1", "assessment-plan-"+strings.ToLower(p.Reference)+"-v1"); string(again) != string(body) {
		t.Error("the Assessment Plan of one approval is not the same bytes twice")
	}
	oscalStatus(t, a, base+"assessment-plan.json?version=9", http.StatusNotFound, "no version 9")
	oscalStatus(t, a, base+"assessment-plan.json?version=x", http.StatusBadRequest, "invalid version")

	// Q1: select, test one item with one Fail, raise it, and have QA sign off.
	if _, err := plans.FreezePopulation(p.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := plans.SelectQuarter(p.ID, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	tests, err := plans.CreateTests(p.ID, assuranceplan.CreateTestsInput{Quarter: 1, Tester: "tess", EvidenceRequestedOn: "2027-03-02"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	oscalStatus(t, a, base+"assessment-results/q1.json", http.StatusNotFound, "signed off by QA yet")

	current, err := plans.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	test := tests[0]
	var results []assuranceplan.ResultInput
	var nfr04 int64
	for _, attr := range current.Attributes {
		if len(attr.AppliesTo) > 0 && !slices.Contains(attr.AppliesTo, test.StratumCode) {
			continue
		}
		result := assuranceplan.ResultPass
		if attr.AttrRef == "NFR-04" {
			result, nfr04 = assuranceplan.ResultFail, attr.ID
		}
		results = append(results, assuranceplan.ResultInput{AttributeID: attr.ID, Result: result, Note: "note on " + attr.AttrRef})
	}
	saved, err := plans.SaveTest(p.ID, test.ID, assuranceplan.TestInput{Tester: "tess", FieldworkCompletedOn: "2027-03-20",
		EvidenceSummary: "Recertification log.", Results: results}, "tess")
	if err != nil {
		t.Fatal(err)
	}
	var failID int64
	for _, r := range saved.Test.Results {
		if r.AttributeID == nfr04 {
			failID = r.ID
		}
	}
	raised, err := plans.RaiseFinding(p.ID, failID, assuranceplan.RaiseInput{Severity: "High"}, "tess")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plans.SignOffQA(p.ID, test.ID, "quinn"); err != nil {
		t.Fatal(err)
	}

	body = oscalDownload(t, a, base+"assessment-results/q1.json", "assessment-results-"+strings.ToLower(p.Reference)+"-q1")
	var ar struct {
		AssessmentResults oscal.AssessmentResults `json:"assessment-results"`
	}
	if err := json.Unmarshal(body, &ar); err != nil {
		t.Fatal(err)
	}
	result := ar.AssessmentResults.Results[0]
	if result.Observations == nil || len(*result.Observations) != len(results) {
		t.Fatalf("observations = %v, want %d", result.Observations, len(results))
	}
	if result.Findings == nil || len(*result.Findings) != 1 {
		t.Fatalf("findings = %v", result.Findings)
	}
	finding := (*result.Findings)[0]
	var afRef string
	for _, prop := range *finding.Props {
		if prop.Name == "audit-finding" {
			afRef = prop.Value
		}
	}
	if afRef != raised.Finding.Reference || finding.Target.TargetId != "nfr-04_obj" {
		t.Errorf("finding names %q targeting %s, want %s", afRef, finding.Target.TargetId, raised.Finding.Reference)
	}
	if !strings.Contains(result.Remarks, "not yet signed off by QA") {
		t.Errorf("the tests not signed off are not counted: %q", result.Remarks)
	}
	for _, r := range *ar.AssessmentResults.BackMatter.Resources {
		if "#"+r.UUID == ar.AssessmentResults.ImportAp.Href && (*r.Rlinks)[0].Href != oscalAssessmentPlanPath(p.ID, 1) {
			t.Errorf("import-ap points at %s", (*r.Rlinks)[0].Href)
		}
	}
	oscalStatus(t, a, base+"assessment-results/q2.json", http.StatusConflict, "has not been selected")
	oscalStatus(t, a, base+"assessment-results/q5.json", http.StatusNotFound, "no such quarter")

	// The POA&M carries the raised finding; drafts are never exported.
	body = oscalDownload(t, a, "/audit-findings/oscal/poam.json", "poam")
	var poam struct {
		POAM oscal.POAM `json:"plan-of-action-and-milestones"`
	}
	if err := json.Unmarshal(body, &poam); err != nil {
		t.Fatal(err)
	}
	if len(poam.POAM.PoamItems) != 1 || !strings.Contains(string(body), raised.Finding.Reference) {
		t.Errorf("POA&M items = %+v", poam.POAM.PoamItems)
	}
	if _, err := findings.Create(auditfinding.Finding{Title: "Not issued yet", Status: auditfinding.StatusDraft, Severity: "Low", Source: "Self-Identified", Owner: "IAM lead"}, "admin"); err != nil {
		t.Fatal(err)
	}
	if again := oscalDownload(t, a, "/audit-findings/oscal/poam.json", "poam"); strings.Contains(string(again), "Not issued yet") {
		t.Error("a draft finding is in the POA&M")
	}
	oscalStatus(t, a, "/audit-findings/oscal/poam.json?status=Draft", http.StatusBadRequest, "not exported")
	oscalStatus(t, a, "/audit-findings/oscal/poam.json?status=Done", http.StatusBadRequest, "use one of")
	oscalStatus(t, a, "/audit-findings/oscal/poam.json?status=Closed", http.StatusNotFound, "no audit finding has the status Closed")
}

// The POA&M maps every Audit Findings status but Draft to an OSCAL risk
// status, and reads the statuses by the same names.
func TestOSCALPOAMCoversEveryFindingStatus(t *testing.T) {
	var mapped []string
	for _, s := range auditfinding.Statuses {
		if s == auditfinding.StatusDraft {
			if _, ok := oscal.POAMRiskStatus[s]; ok {
				t.Error("Draft has a risk status")
			}
			continue
		}
		if oscal.POAMRiskStatus[s] == "" {
			t.Errorf("status %q has no OSCAL risk status", s)
		}
		mapped = append(mapped, s)
	}
	if len(mapped) != len(oscal.POAMRiskStatus) {
		t.Errorf("the POA&M maps statuses Audit Findings does not have: %v", oscal.POAMRiskStatus)
	}
	want := []string{auditfinding.StatusOpen, auditfinding.StatusInRemediation, auditfinding.StatusPendingValidation, auditfinding.StatusReopened, auditfinding.StatusRiskAccepted}
	if !reflect.DeepEqual(oscal.POAMDefaultStatuses, want) {
		t.Errorf("default statuses = %v", oscal.POAMDefaultStatuses)
	}
}
