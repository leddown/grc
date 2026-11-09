package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"grc/internal/auditfinding"
	"grc/internal/oscal/oscaltest"
	"grc/internal/regcoverage"
)

// Every document the app serves, from a database seeded with the fixtures,
// goes through compliance-trestle's own import and validate. trestle is an
// optional validator in its own process: the test is skipped under -short and
// when trestle is not on PATH.
func TestOSCALExportsPassTrestle(t *testing.T) {
	oscaltest.TrestleBinary(t)
	a := newFullApp(t, Options{})

	shipped, err := os.ReadFile(filepath.Join("..", "data", "merged_nist_controls_master_replaced_from_controls_all.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := a.asAdmin(http.MethodPost, "/controls/import", string(shipped)); rec.Code != http.StatusOK {
		t.Fatalf("control import: %d %s", rec.Code, rec.Body.String())
	}
	applyOSCAL(t, a, map[string]any{
		"catalog": oscalFixture(t, "NIST_SP-800-53_rev5_catalog_cut.json"),
		"profiles": map[string]string{
			"low":      oscalFixture(t, "NIST_SP-800-53_rev5_LOW-baseline_profile_cut.json"),
			"moderate": oscalFixture(t, "NIST_SP-800-53_rev5_MODERATE-baseline_profile_cut.json"),
		},
	})
	nfrs, err := os.ReadFile(filepath.Join("..", "data", "NFR_incremental_keys_with_domain.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := a.asAdmin(http.MethodPost, "/security-nfrs/import", string(nfrs)); rec.Code != http.StatusOK {
		t.Fatalf("NFR import: %d %s", rec.Code, rec.Body.String())
	}
	regs := regcoverage.NewSQLiteRepository(a.conn)
	reg := doraRegulation(t, regs)
	cutRegulationVersion(t, regs, reg, "Initial analysis", "2026-10-01T08:00:00Z", true, "RA-3", "PM-9", "NFR 1")

	plans, findings := oscalPlanServices(a)
	p := approvedExample(t, plans)
	failAndSignOff(t, plans, p.ID, selectQ1(t, plans, p.ID))
	if _, err := findings.Create(auditfinding.Finding{Title: "Backups are not restored in testing", Severity: "Medium", Source: "Internal Audit",
		Owner: "Head of IT operations", Criteria: "Backups are restored and checked each quarter.", Condition: "No restore test in 2026.",
		Cause: "No owner for the test.", Effect: "A restore may fail when it is needed.",
		Actions: []auditfinding.Action{{Description: "Name an owner and run a restore test.", Owner: "Head of IT operations", DueDate: "2027-01-31"}}}, "admin"); err != nil {
		t.Fatal(err)
	}

	docs := map[string][]byte{}
	for name, path := range map[string]string{
		"controls-catalog":      "/controls/oscal/catalog.json",
		"profile-low":           "/controls/oscal/profile/low.json",
		"profile-moderate":      "/controls/oscal/profile/moderate.json",
		"profile-high":          "/controls/oscal/profile/high.json",
		"profile-privacy":       "/controls/oscal/profile/privacy.json",
		"nfr-catalog":           "/security-nfrs/oscal/catalog.json",
		"nfr-mapping":           "/security-nfrs/oscal/mapping.json",
		"regulation-catalog":    fmt.Sprintf("/regulation-coverage/%d/oscal/catalog.json", reg.ID),
		"regulation-mapping":    fmt.Sprintf("/regulation-coverage/%d/oscal/mapping.json", reg.ID),
		"assessment-plan":       fmt.Sprintf("/assurance-plans/%d/oscal/assessment-plan.json", p.ID),
		"assessment-results-q1": fmt.Sprintf("/assurance-plans/%d/oscal/assessment-results/q1.json", p.ID),
		"poam":                  "/audit-findings/oscal/poam.json",
	} {
		rec := a.asAdmin(http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
		docs[name] = rec.Body.Bytes()
	}
	oscaltest.Trestle(t, docs)
}
