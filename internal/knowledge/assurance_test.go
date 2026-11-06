package knowledge

import "testing"

// Assurance plans are worked out by their module and read fresh: "what's due
// this quarter?" asked after a test is signed off must count it.
func TestAssuranceKindsAreSuppliedAndReadFresh(t *testing.T) {
	svc := newTestService(t)
	for _, kind := range []string{KindAssurancePlan, KindAssuranceTest} {
		items, err := svc.Items(kind)
		if err != nil || len(items) != 0 {
			t.Errorf("unwired %s = %d items, %v; want none and no error", kind, len(items), err)
		}
	}

	loads := 0
	plans := func() ([]Item, error) {
		loads++
		return []Item{{Kind: KindAssurancePlan, Ref: "AP-2027-001", Title: "Plan", Body: "Behind plan"}}, nil
	}
	svc.WithAssurancePlans(plans, func() ([]Item, error) { return []Item{}, nil })
	for range 2 {
		if _, err := svc.Items(KindAssurancePlan); err != nil {
			t.Fatal(err)
		}
	}
	if loads != 2 {
		t.Errorf("the plan corpus was loaded %d times for two reads; it must not be cached", loads)
	}
	index, err := svc.Index(KindAssurancePlan)
	if err != nil || len(index) != 1 || index[0].Body != "" {
		t.Errorf("plan index = %+v, %v", index, err)
	}
	if _, err := svc.Index(KindAssuranceTest); err == nil {
		t.Error("the test corpus has a compact index; it is searched instead")
	}
}
