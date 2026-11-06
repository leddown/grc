package assuranceplan

import (
	"slices"
	"strings"
	"testing"

	"grc/internal/knowledge"
)

func TestKnowledgeKindsReturnASeededPlan(t *testing.T) {
	svc, _ := newIntegratedService(t)
	p := sampleYear(t, svc, 1)
	at(svc, "2027-05-10")
	ks := knowledge.NewService(knowledge.NewStore(svc.repo.(*SQLiteRepository).db)).WithAssurancePlans(svc.KnowledgePlans, svc.KnowledgeTests)

	plans, err := ks.Index(knowledge.KindAssurancePlan)
	if err != nil || len(plans) != 1 || plans[0].Ref != p.Reference {
		t.Fatalf("plan index = %+v, %v", plans, err)
	}
	plan, err := ks.Get(knowledge.KindAssurancePlan, "ap-2027-001")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Fields["current_quarter"] != "Q2 of the period" || plan.Fields["behind_plan"] != "yes" || plan.Fields["tests_opened"] != "17" {
		t.Errorf("fields = %v", plan.Fields)
	}
	for _, want := range []string{"Q2 (current quarter): 1LoD wave of 100 item(s) due 2027-05-28; sample of 20 test(s) drawn 2027-05-31",
		"Behind plan: Tier 1 coverage Amber at Q1 end (31.25% against 35%)", "NFR-04 (Access management & recertification) in T1: 1 deviation(s)"} {
		if !strings.Contains(plan.Body, want) {
			t.Errorf("plan body lacks %q:\n%s", want, plan.Body)
		}
	}

	tests, err := ks.Items(knowledge.KindAssuranceTest)
	if err != nil || len(tests) != 17 {
		t.Fatalf("%d tests, %v", len(tests), err)
	}
	var failedT1 []string
	for _, it := range tests {
		if it.Fields["stratum"] == "T1" && strings.Contains(it.Fields["failed_attributes"], "NFR-04") {
			failedT1 = append(failedT1, it.Fields["item_ref"])
		}
	}
	if len(failedT1) != 1 {
		t.Errorf("Tier 1 items that failed NFR-04: %v", failedT1)
	}
	hits, err := ks.Search(knowledge.KindAssuranceTest, "failed NFR-04", 5)
	if err != nil || len(hits.Hits) == 0 || hits.Hits[0].Item.Fields["item_ref"] != failedT1[0] {
		t.Errorf("searching for NFR-04 fails does not put %v first: %+v %v", failedT1, hits, err)
	}
	one, err := ks.Get(knowledge.KindAssuranceTest, "AP-2027-001/T27-Q1-001")
	if err != nil || !strings.Contains(one.Body, "NFR-04 Access management & recertification: Fail") || !slices.Contains(one.Related, "NFR-04") {
		t.Errorf("test record = %+v, %v", one, err)
	}

	bundle, err := ks.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Overview.Counts[knowledge.KindAssurancePlan] != 1 || bundle.Overview.Counts[knowledge.KindAssuranceTest] != 17 {
		t.Errorf("export counts = %v", bundle.Overview.Counts)
	}

	at(svc, "2026-12-01")
	plan, err = ks.Get(knowledge.KindAssurancePlan, p.Reference)
	if err != nil || plan.Fields["behind_plan"] != "not started" || plan.Fields["current_quarter"] != "mobilisation, before the period starts" {
		t.Errorf("before the period: %v, %v", plan.Fields, err)
	}
}
