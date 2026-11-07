package assuranceplan

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"grc/internal/auditfinding"
	"grc/internal/controlcatalog"
	"grc/internal/controlid"
	"grc/internal/policydocs"
	"grc/internal/riskregister"
	"grc/internal/securitynfr"
)

// Integrations are the modules execution writes to and reads from: findings
// go to Audit Findings and thematic conclusions to the risk register rather
// than to registers of the plan's own, and attributes are checked against the
// catalogs. A nil member turns its feature off with a clear message.
type Integrations struct {
	Findings *auditfinding.Service
	Risks    *riskregister.Service
	Controls *controlcatalog.Service
	NFRs     *securitynfr.Service
	// Policies is read to propose a policy review plan's population.
	Policies *policydocs.Service
}

func (s *Service) WithIntegrations(in Integrations) *Service {
	s.in = in
	return s
}

// ---- test attributes ----

const maxAttrRef = 40

func normalizeAttributes(attrs []Attribute, strata []Stratum) error {
	codes := map[string]bool{}
	for _, st := range strata {
		codes[st.Code] = true
	}
	seen := map[string]bool{}
	for i := range attrs {
		a := &attrs[i]
		for _, f := range []*string{&a.AttrRef, &a.Domain, &a.Requirement, &a.Procedure, &a.Method, &a.EvidenceRequested,
			&a.PassCriteria, &a.RegulatoryRefs, &a.NFRKey, &a.Model, &a.PromptHash} {
			*f = strings.TrimSpace(*f)
			if len(*f) > maxFieldLength {
				return invalid("attribute %d: a field is longer than %d characters", i+1, maxFieldLength)
			}
		}
		switch {
		case a.AttrRef == "":
			return invalid("attribute %d has no reference", i+1)
		case len(a.AttrRef) > maxAttrRef || strings.ContainsAny(a.AttrRef, "/,;\n"):
			return invalid("attribute reference %q is too long or holds a separator", a.AttrRef)
		case seen[strings.ToLower(a.AttrRef)]:
			return invalid("attribute %s is listed twice", a.AttrRef)
		}
		seen[strings.ToLower(a.AttrRef)] = true
		if a.Method == "" {
			a.Method = MethodInspection
		}
		if !slices.Contains(Methods, a.Method) {
			return invalid("attribute %s: the method must be one of %s", a.AttrRef, strings.Join(Methods, ", "))
		}
		applies := []string{}
		for _, c := range a.AppliesTo {
			if c = strings.TrimSpace(c); c == "" || slices.Contains(applies, c) {
				continue
			}
			if !codes[c] {
				return invalid("attribute %s applies to %s, which is not one of the plan's strata", a.AttrRef, c)
			}
			applies = append(applies, c)
		}
		if len(applies) == len(strata) {
			applies = []string{}
		}
		a.AppliesTo = applies
		ids := []string{}
		for _, c := range a.ControlIDs {
			if c = controlid.Normalize(c); c != "" && !slices.Contains(ids, c) {
				ids = append(ids, c)
			}
		}
		a.ControlIDs = ids
		a.Ordinal, a.ID = i, 0
		a.Unresolved, a.Checked = nil, false
	}
	return nil
}

// resolveAttributes marks each attribute's control ids and NFR key that do
// not exist in the catalogs. Without the catalogs nothing is checked, and the
// attribute says so rather than claiming its references resolve.
func (s *Service) resolveAttributes(attrs []Attribute) {
	for i := range attrs {
		a := &attrs[i]
		a.Unresolved = []string{}
		if s.in.Controls == nil || s.in.NFRs == nil {
			continue
		}
		a.Checked = true
		for _, c := range a.ControlIDs {
			if _, err := s.in.Controls.Get(c); err != nil {
				a.Unresolved = append(a.Unresolved, c)
			}
		}
		if a.NFRKey != "" {
			if _, err := s.in.NFRs.Get(a.NFRKey); err != nil {
				a.Unresolved = append(a.Unresolved, "NFR "+a.NFRKey)
			}
		}
	}
}

func attributeDesign(a Attribute) string {
	a.ID, a.Ordinal, a.Unresolved, a.Checked = 0, 0, nil, false
	return fmt.Sprintf("%+v", a)
}

// ReplaceAttributes saves the test script. An attribute with results cannot
// be removed, or stop applying to a stratum it has results in.
func (s *Service) ReplaceAttributes(id int64, attrs []Attribute, actor string) (Plan, error) {
	prev, err := s.editable(id)
	if err != nil {
		return Plan{}, err
	}
	if attrs == nil {
		attrs = []Attribute{}
	}
	if err := normalizeAttributes(attrs, prev.Strata); err != nil {
		return Plan{}, err
	}
	tests, err := s.repo.Tests(id)
	if err != nil {
		return Plan{}, err
	}
	used := map[string]map[string]int{}
	for _, t := range tests {
		for _, r := range t.Results {
			if r.Result == "" {
				continue
			}
			if used[r.AttrRef] == nil {
				used[r.AttrRef] = map[string]int{}
			}
			used[r.AttrRef][t.StratumCode]++
		}
	}
	for ref, strata := range used {
		i := slices.IndexFunc(attrs, func(a Attribute) bool { return a.AttrRef == ref })
		if i < 0 {
			return Plan{}, invalid("attribute %s has results on %d test(s) and stays in the test script", ref, sumCounts(strata))
		}
		for code := range strata {
			if !attrs[i].appliesTo(code) {
				return Plan{}, invalid("attribute %s has results on stratum %s tests, so it must keep applying to %s", ref, code, code)
			}
		}
	}
	note := listChanges(prev.Attributes, attrs, func(a Attribute) string { return a.AttrRef }, attributeDesign)
	if note == "" {
		return prev, nil
	}
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "design", Subject: "test script",
		FromValue: strconv.Itoa(len(prev.Attributes)), ToValue: strconv.Itoa(len(attrs)), Note: note}
	if err := s.repo.ReplaceAttributes(id, attrs, []HistoryEntry{h}); err != nil {
		return Plan{}, err
	}
	return s.Get(id)
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// listChanges summarises a list edit by reference: "added A, B; changed C;
// removed D".
func listChanges[T any](before, after []T, ref func(T) string, design func(T) string) string {
	old := map[string]T{}
	for _, x := range before {
		old[ref(x)] = x
	}
	var added, changed, removed []string
	for _, x := range after {
		if o, ok := old[ref(x)]; !ok {
			added = append(added, ref(x))
		} else if design(o) != design(x) {
			changed = append(changed, ref(x))
		}
		delete(old, ref(x))
	}
	for r := range old {
		removed = append(removed, r)
	}
	sort.Strings(removed)
	var parts []string
	for _, x := range []struct {
		label string
		refs  []string
	}{{"added", added}, {"changed", changed}, {"removed", removed}} {
		if len(x.refs) > 0 {
			parts = append(parts, x.label+" "+strings.Join(firstN(x.refs, 12), ", "))
		}
	}
	return strings.Join(parts, "; ")
}

// SeedInput names the catalog entries to copy into the test script.
type SeedInput struct {
	NFRKeys    []string `json:"nfr_keys"`
	ControlIDs []string `json:"control_ids"`
}

type SeedResult struct {
	Plan     Plan     `json:"plan"`
	Added    []string `json:"added"`
	Skipped  []string `json:"skipped"`
	NotFound []string `json:"not_found"`
}

// SeedAttributes copies Security NFRs and 800-53 controls into the test
// script. An NFR keeps its catalog key, so the attribute can be traced back
// to it; the copy does not change when the catalog does.
func (s *Service) SeedAttributes(id int64, in SeedInput, actor string) (SeedResult, error) {
	p, err := s.editable(id)
	if err != nil {
		return SeedResult{}, err
	}
	if s.in.NFRs == nil || s.in.Controls == nil {
		return SeedResult{}, conflict("the control and NFR catalogs are not available to this service")
	}
	res := SeedResult{Added: []string{}, Skipped: []string{}, NotFound: []string{}}
	attrs := append([]Attribute{}, p.Attributes...)
	have := func(ref string) bool {
		return slices.ContainsFunc(attrs, func(a Attribute) bool { return strings.EqualFold(a.AttrRef, ref) })
	}
	var catalog []securitynfr.NFR
	if len(in.NFRKeys) > 0 {
		if catalog, err = s.in.NFRs.List("", ""); err != nil {
			return SeedResult{}, fmt.Errorf("read the NFR catalog: %w", err)
		}
	}
	for _, key := range in.NFRKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		ni := slices.IndexFunc(catalog, func(n securitynfr.NFR) bool {
			id := strings.TrimSpace(n.ID)
			return n.Key == key || (id != "" && (strings.EqualFold(id, key) || strings.EqualFold("NFR-"+id, key)))
		})
		if ni < 0 {
			res.NotFound = append(res.NotFound, "NFR "+key)
			continue
		}
		n := catalog[ni]
		ref := "NFR-" + firstNonEmpty(strings.TrimSpace(n.ID), n.Key)
		if have(ref) {
			res.Skipped = append(res.Skipped, ref)
			continue
		}
		requirement := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(n.Summary), strings.TrimSpace(n.ID)))
		if d := strings.TrimSpace(n.Description); d != "" {
			requirement = strings.TrimSpace(requirement + "\n" + d)
		}
		attrs = append(attrs, Attribute{AttrRef: ref, Domain: n.Domain, Requirement: requirement,
			Procedure:    "Inspect the evidence for the item against the requirement and its implementation guidance.",
			Method:       MethodInspection,
			PassCriteria: strings.TrimSpace(n.Implementation),
			ControlIDs:   splitList(n.NISTMapping), NFRKey: n.Key})
		res.Added = append(res.Added, ref)
	}
	for _, raw := range in.ControlIDs {
		cid := controlid.Normalize(raw)
		if cid == "" {
			continue
		}
		c, err := s.in.Controls.Get(cid)
		if errors.Is(err, controlcatalog.ErrNotFound) {
			res.NotFound = append(res.NotFound, cid)
			continue
		}
		if err != nil {
			return SeedResult{}, fmt.Errorf("read control %s: %w", cid, err)
		}
		if have(c.ID) {
			res.Skipped = append(res.Skipped, c.ID)
			continue
		}
		attrs = append(attrs, Attribute{AttrRef: c.ID, Domain: c.Family, Requirement: c.Name,
			Procedure:    "Inspect the evidence that the control is implemented and operating for the item.",
			Method:       MethodInspection,
			PassCriteria: clip(strings.TrimSpace(c.Requirements), 4000),
			ControlIDs:   []string{c.ID}})
		res.Added = append(res.Added, c.ID)
	}
	if len(res.Added) == 0 {
		if len(res.NotFound)+len(res.Skipped) == 0 {
			return SeedResult{}, invalid("name at least one Security NFR key or 800-53 control id")
		}
		res.Plan = p
		return res, nil
	}
	if res.Plan, err = s.ReplaceAttributes(id, attrs, actor); err != nil {
		return SeedResult{}, err
	}
	return res, nil
}

// ---- tests ----

// executable is a plan whose tests may be recorded: approved or active, not
// closed. Tests follow the plan of record while a revision is open.
func (s *Service) executable(id int64) (Plan, error) {
	p, err := s.Get(id)
	if err != nil {
		return Plan{}, err
	}
	switch p.Status {
	case StatusApproved, StatusActive:
		return p, nil
	case StatusClosed:
		return Plan{}, conflict("%s is closed; its tests no longer change", p.Reference)
	}
	return Plan{}, conflict("%s is %s; tests are recorded once the plan is approved and its sample selected", p.Reference, describeState(p))
}

func (s *Service) today() string { return s.now().UTC().Format(dateLayout) }

// deriveTest fills a test's counts and overall result from the attributes
// that apply to its stratum.
func deriveTest(p Plan, t *Test, today string) {
	byAttr := map[int64]Result{}
	for _, r := range t.Results {
		byAttr[r.AttributeID] = r
	}
	t.Fails, t.Passes, t.NotApplicable, t.Pending = 0, 0, 0, 0
	for _, a := range p.Attributes {
		if !a.appliesTo(t.StratumCode) {
			continue
		}
		switch byAttr[a.ID].Result {
		case ResultPass:
			t.Passes++
		case ResultFail:
			t.Fails++
		case ResultNA:
			t.NotApplicable++
		default:
			t.Pending++
		}
	}
	t.Tested = t.Passes + t.Fails
	t.Overall = OverallResult(t.Fails, t.Tested, p.Parameters)
	t.EvidenceLate = EvidenceLate(t.EvidenceDueOn, t.EvidenceReceivedOn, today)
}

func (s *Service) Tests(id int64, quarter int) ([]Test, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	return s.testsOf(p, quarter)
}

func (s *Service) testsOf(p Plan, quarter int) ([]Test, error) {
	tests, err := s.repo.Tests(p.ID)
	if err != nil {
		return nil, err
	}
	today := s.today()
	out := []Test{}
	for _, t := range tests {
		if quarter > 0 && t.Quarter != quarter {
			continue
		}
		deriveTest(p, &t, today)
		out = append(out, t)
	}
	return out, nil
}

// CreateTestsInput opens tests for a quarter's selections: every selection of
// the quarter that has no test yet, or only the ones listed.
type CreateTestsInput struct {
	Quarter             int     `json:"quarter"`
	SelectionIDs        []int64 `json:"selection_ids"`
	Tester              string  `json:"tester"`
	EvidenceRequestedOn string  `json:"evidence_requested_on"`
}

func (s *Service) CreateTests(id int64, in CreateTestsInput, actor string) ([]Test, error) {
	p, err := s.executable(id)
	if err != nil {
		return nil, err
	}
	if in.Quarter < 1 || in.Quarter > 4 {
		return nil, invalid("the quarter is 1 to 4")
	}
	in.Tester = strings.TrimSpace(in.Tester)
	if len(in.Tester) > maxItemField {
		return nil, invalid("the tester's name is too long")
	}
	if in.EvidenceRequestedOn = strings.TrimSpace(in.EvidenceRequestedOn); in.EvidenceRequestedOn != "" {
		if _, ok := parseDate(in.EvidenceRequestedOn); !ok {
			return nil, invalid("evidence requested on %q is not a date (YYYY-MM-DD)", in.EvidenceRequestedOn)
		}
	}
	sels, err := s.repo.Selections(id)
	if err != nil {
		return nil, err
	}
	existing, err := s.repo.Tests(id)
	if err != nil {
		return nil, err
	}
	tested := map[int64]bool{}
	highest := 0
	prefix := testRef(p.PeriodStart, in.Quarter, 0)
	prefix = prefix[:len(prefix)-3]
	for _, t := range existing {
		tested[t.SelectionID] = true
		if n, err := strconv.Atoi(strings.TrimPrefix(t.TestRef, prefix)); err == nil && strings.HasPrefix(t.TestRef, prefix) && n > highest {
			highest = n
		}
	}
	want := map[int64]bool{}
	for _, sid := range in.SelectionIDs {
		want[sid] = true
	}
	stamp := s.stamp()
	cal := NewCalendar(p.NonWorkingDates)
	var tests []Test
	var refs []string
	for _, sel := range sels {
		if sel.Quarter != in.Quarter || tested[sel.ID] || (len(want) > 0 && !want[sel.ID]) {
			continue
		}
		delete(want, sel.ID)
		highest++
		t := Test{SelectionID: sel.ID, TestRef: testRef(p.PeriodStart, in.Quarter, highest), TestType: firstOf(p.TestTypes),
			Tester: firstNonEmpty(in.Tester, actor), EvidenceRequestedOn: in.EvidenceRequestedOn,
			CreatedAt: stamp, CreatedBy: actor, UpdatedAt: stamp, UpdatedBy: actor}
		t.EvidenceDueOn = EvidenceDue(t.EvidenceRequestedOn, p.Parameters.EvidenceSLADays, cal)
		tests = append(tests, t)
		refs = append(refs, t.TestRef)
	}
	for sid := range want {
		if !slices.ContainsFunc(sels, func(x Selection) bool { return x.ID == sid && x.Quarter == in.Quarter }) {
			return nil, invalid("selection %d is not one of Q%d's picks", sid, in.Quarter)
		}
	}
	if len(tests) == 0 {
		if !slices.ContainsFunc(sels, func(x Selection) bool { return x.Quarter == in.Quarter }) {
			return nil, conflict("Q%d has no selection yet: select its sample first", in.Quarter)
		}
		return nil, conflict("every Q%d pick already has a test", in.Quarter)
	}
	span := refs[0]
	if len(refs) > 1 {
		span += " to " + refs[len(refs)-1]
	}
	h := HistoryEntry{At: stamp, Actor: actor, Event: "test", Subject: fmt.Sprintf("Q%d", in.Quarter),
		ToValue: fmt.Sprintf("%d test(s) opened", len(tests)), Note: span}
	if err := s.repo.CreateTests(id, tests, []HistoryEntry{h}); err != nil {
		return nil, err
	}
	all, err := s.testsOf(p, in.Quarter)
	if err != nil {
		return nil, err
	}
	out := []Test{}
	for _, t := range all {
		if slices.Contains(refs, t.TestRef) {
			out = append(out, t)
		}
	}
	return out, nil
}

// TestInput is a test's record as the editor saves it. The evidence due date
// is not in it: it is always the requested date plus the SLA.
// TestInput is a test's record. A blank test type keeps the test's type.
type TestInput struct {
	TestType             string        `json:"test_type"`
	Tester               string        `json:"tester"`
	EvidenceRequestedOn  string        `json:"evidence_requested_on"`
	EvidenceReceivedOn   string        `json:"evidence_received_on"`
	FieldworkCompletedOn string        `json:"fieldwork_completed_on"`
	EvidenceSummary      string        `json:"evidence_summary"`
	Notes                string        `json:"notes"`
	Results              []ResultInput `json:"results"`
}

type ResultInput struct {
	AttributeID int64  `json:"attribute_id"`
	Result      string `json:"result"`
	Note        string `json:"note"`
}

func firstOf(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

// matchTestType finds a type in the plan's list without regard to case. A plan
// with no list takes any short type.
func matchTestType(types []string, v string) (string, bool) {
	if len(types) == 0 {
		return v, len(v) <= maxTestType
	}
	for _, t := range types {
		if strings.EqualFold(t, v) {
			return t, true
		}
	}
	return "", false
}

type TestSaveResult struct {
	Test        Test   `json:"test"`
	QAWithdrawn bool   `json:"qa_withdrawn"`
	Changes     string `json:"changes"`
}

func (s *Service) loadTest(p Plan, testID int64) (Test, error) {
	tests, err := s.testsOf(p, 0)
	if err != nil {
		return Test{}, err
	}
	i := slices.IndexFunc(tests, func(t Test) bool { return t.ID == testID })
	if i < 0 {
		return Test{}, ErrNotFound
	}
	return tests[i], nil
}

// SaveTest records a test's evidence and results. A change to a test that
// has QA sign-off withdraws the sign-off: QA signed what was there before.
func (s *Service) SaveTest(id, testID int64, in TestInput, actor string) (TestSaveResult, error) {
	p, err := s.executable(id)
	if err != nil {
		return TestSaveResult{}, err
	}
	prev, err := s.loadTest(p, testID)
	if err != nil {
		return TestSaveResult{}, err
	}
	next := prev
	for _, f := range []struct {
		dst *string
		v   string
		max int
	}{{&next.Tester, in.Tester, maxItemField}, {&next.EvidenceRequestedOn, in.EvidenceRequestedOn, 10},
		{&next.EvidenceReceivedOn, in.EvidenceReceivedOn, 10}, {&next.FieldworkCompletedOn, in.FieldworkCompletedOn, 10},
		{&next.EvidenceSummary, in.EvidenceSummary, maxFieldLength}, {&next.Notes, in.Notes, maxFieldLength}} {
		v := strings.TrimSpace(f.v)
		if len(v) > f.max {
			return TestSaveResult{}, invalid("a field of %s is too long", prev.TestRef)
		}
		*f.dst = v
	}
	for label, d := range map[string]string{"evidence requested on": next.EvidenceRequestedOn,
		"evidence received on": next.EvidenceReceivedOn, "fieldwork completed on": next.FieldworkCompletedOn} {
		if d != "" {
			if _, ok := parseDate(d); !ok {
				return TestSaveResult{}, invalid("%s %q is not a date (YYYY-MM-DD)", label, d)
			}
		}
	}
	next.EvidenceDueOn = EvidenceDue(next.EvidenceRequestedOn, p.Parameters.EvidenceSLADays, NewCalendar(p.NonWorkingDates))
	if v := strings.TrimSpace(in.TestType); v != "" {
		typ, ok := matchTestType(p.TestTypes, v)
		if !ok {
			return TestSaveResult{}, invalid("test type %q is not one of the plan's (%s)", v, strings.Join(p.TestTypes, ", "))
		}
		next.TestType = typ
	}

	attrByID := map[int64]Attribute{}
	for _, a := range p.Attributes {
		attrByID[a.ID] = a
	}
	current := map[int64]Result{}
	for _, r := range prev.Results {
		current[r.AttributeID] = r
	}
	var changes []string
	merged := map[int64]Result{}
	for id, r := range current {
		merged[id] = r
	}
	for _, in := range in.Results {
		a, ok := attrByID[in.AttributeID]
		if !ok {
			return TestSaveResult{}, invalid("attribute %d is not in the plan's test script", in.AttributeID)
		}
		if !a.appliesTo(prev.StratumCode) {
			return TestSaveResult{}, invalid("%s does not apply to stratum %s", a.AttrRef, prev.StratumCode)
		}
		value := strings.TrimSpace(in.Result)
		if strings.EqualFold(value, "na") || strings.EqualFold(value, "n/a") {
			value = ResultNA
		}
		if value != "" && !slices.Contains(Results, value) {
			return TestSaveResult{}, invalid("%s: the result must be Pass, Fail, N/A or blank", a.AttrRef)
		}
		note := strings.TrimSpace(in.Note)
		if len(note) > maxFieldLength {
			return TestSaveResult{}, invalid("%s: the note is too long", a.AttrRef)
		}
		old := current[a.ID]
		if old.FindingID > 0 && value != ResultFail {
			return TestSaveResult{}, conflict("%s on %s raised finding %s, so it stays a Fail; close the finding in Audit Findings instead",
				a.AttrRef, prev.TestRef, old.FindingRef)
		}
		if value != old.Result {
			changes = append(changes, fmt.Sprintf("%s %s → %s", a.AttrRef, firstNonEmpty(old.Result, "blank"), firstNonEmpty(value, "blank")))
		} else if note != old.Note {
			changes = append(changes, a.AttrRef+" note")
		}
		old.TestID, old.AttributeID, old.AttrRef, old.Result, old.Note = testID, a.ID, a.AttrRef, value, note
		merged[a.ID] = old
	}
	next.Results = []Result{}
	for _, a := range p.Attributes {
		if r, ok := merged[a.ID]; ok {
			next.Results = append(next.Results, r)
		}
	}
	for _, x := range []struct{ label, a, b string }{
		{"test type", prev.TestType, next.TestType}, {"tester", prev.Tester, next.Tester}, {"evidence requested", prev.EvidenceRequestedOn, next.EvidenceRequestedOn},
		{"evidence received", prev.EvidenceReceivedOn, next.EvidenceReceivedOn},
		{"fieldwork completed", prev.FieldworkCompletedOn, next.FieldworkCompletedOn},
		{"evidence summary", prev.EvidenceSummary, next.EvidenceSummary}, {"notes", prev.Notes, next.Notes}} {
		if x.a != x.b {
			if len(x.b) <= 40 && len(x.a) <= 40 {
				changes = append(changes, fmt.Sprintf("%s %s → %s", x.label, firstNonEmpty(x.a, "blank"), firstNonEmpty(x.b, "blank")))
			} else {
				changes = append(changes, x.label)
			}
		}
	}
	if len(changes) == 0 {
		return TestSaveResult{Test: prev}, nil
	}
	stamp := s.stamp()
	next.UpdatedAt, next.UpdatedBy = stamp, actor
	result := TestSaveResult{Changes: strings.Join(changes, "; ")}
	history := []HistoryEntry{{At: stamp, Actor: actor, Event: "test", Subject: prev.TestRef,
		FromValue: prev.Overall, Note: result.Changes}}
	if prev.QADate != "" {
		next.QAReviewer, next.QADate, next.QANote = "", "", ""
		result.QAWithdrawn = true
		history = append(history, HistoryEntry{At: stamp, Actor: actor, Event: "qa", Subject: prev.TestRef,
			FromValue: "signed off by " + prev.QAReviewer, ToValue: "withdrawn", Note: "the test changed after QA sign-off"})
	}
	deriveTest(p, &next, s.today())
	history[0].ToValue = next.Overall
	if err := s.repo.SaveTest(id, next, history); err != nil {
		return TestSaveResult{}, mapNotFound(err)
	}
	if result.Test, err = s.loadTest(p, testID); err != nil {
		return TestSaveResult{}, err
	}
	return result, nil
}

type QAResult struct {
	Test    Test   `json:"test"`
	Warning string `json:"warning"`
}

// SignOffQA is the four-eyes review: the signed-in user signs the test off,
// and may not be its tester when separation of duties is enforced.
func (s *Service) SignOffQA(id, testID int64, actor string) (QAResult, error) {
	p, err := s.executable(id)
	if err != nil {
		return QAResult{}, err
	}
	t, err := s.loadTest(p, testID)
	if err != nil {
		return QAResult{}, err
	}
	switch {
	case t.QADate != "":
		return QAResult{}, conflict("%s was signed off by %s on %s", t.TestRef, firstNonEmpty(t.QAReviewer, "—"), t.QADate)
	case t.FieldworkCompletedOn == "":
		return QAResult{}, invalid("%s has no fieldwork completion date: QA signs off finished fieldwork", t.TestRef)
	case t.Pending > 0:
		return QAResult{}, invalid("%s has %d attribute(s) without a result", t.TestRef, t.Pending)
	}
	res := QAResult{}
	sameAsTester := actor != "" && strings.EqualFold(actor, t.Tester)
	if sameAsTester || (actor == "" && t.Tester == "") {
		if s.requireSeparateApprover {
			return QAResult{}, conflict("%s was tested by %s; QA sign-off must come from someone else", t.TestRef, firstNonEmpty(t.Tester, "the same user"))
		}
		res.Warning = "QA signed off by the tester: separation of duties is not enforced in single-user mode."
		t.QANote = res.Warning
	}
	stamp := s.stamp()
	t.QAReviewer, t.QADate = actor, s.today()
	t.UpdatedAt, t.UpdatedBy = stamp, actor
	h := HistoryEntry{At: stamp, Actor: actor, Event: "qa", Subject: t.TestRef, ToValue: "signed off",
		Note: strings.TrimSpace(fmt.Sprintf("%s, tester %s. %s", t.Overall, firstNonEmpty(t.Tester, "—"), res.Warning))}
	if err := s.repo.SaveTest(id, t, []HistoryEntry{h}); err != nil {
		return QAResult{}, mapNotFound(err)
	}
	if res.Test, err = s.loadTest(p, testID); err != nil {
		return QAResult{}, err
	}
	return res, nil
}

// ---- findings ----

// RaiseInput is the tester's part of a finding; the rest is filled from the
// plan, the test and the attribute.
type RaiseInput struct {
	Severity    string `json:"severity"`
	Owner       string `json:"owner"`
	Title       string `json:"title"`
	DueDate     string `json:"due_date"`
	Status      string `json:"status"`
	FindingType string `json:"finding_type"`
}

type RaiseResult struct {
	Result  Result               `json:"result"`
	Finding auditfinding.Finding `json:"finding"`
}

func originRef(p Plan, t Test, a Attribute) string {
	return p.Reference + " / " + t.TestRef + " / " + a.AttrRef
}

// RaiseFinding turns a Fail into a finding in Audit Findings, written as
// criteria (the attribute) and condition (what the test found), and links the
// two both ways: the finding's reference on the result, the plan, test and
// attribute in the finding's origin.
func (s *Service) RaiseFinding(id, resultID int64, in RaiseInput, actor string) (RaiseResult, error) {
	if s.in.Findings == nil {
		return RaiseResult{}, conflict("Audit Findings is not available to this service")
	}
	p, err := s.executable(id)
	if err != nil {
		return RaiseResult{}, err
	}
	tests, err := s.testsOf(p, 0)
	if err != nil {
		return RaiseResult{}, err
	}
	var t Test
	var r Result
	for _, x := range tests {
		for _, y := range x.Results {
			if y.ID == resultID {
				t, r = x, y
			}
		}
	}
	if r.ID == 0 {
		return RaiseResult{}, ErrNotFound
	}
	ai := slices.IndexFunc(p.Attributes, func(a Attribute) bool { return a.ID == r.AttributeID })
	if ai < 0 {
		return RaiseResult{}, ErrNotFound
	}
	a := p.Attributes[ai]
	if r.Result != ResultFail {
		return RaiseResult{}, conflict("%s on %s is %s; a finding is raised on a Fail", a.AttrRef, t.TestRef, firstNonEmpty(r.Result, "not assessed"))
	}
	if r.FindingID > 0 {
		if _, err := s.in.Findings.Get(r.FindingID); err == nil {
			return RaiseResult{}, conflict("%s on %s already raised %s", a.AttrRef, t.TestRef, r.FindingRef)
		} else if !errors.Is(err, auditfinding.ErrNotFound) {
			return RaiseResult{}, err
		}
	}
	in.Severity = strings.TrimSpace(in.Severity)
	if !slices.Contains(auditfinding.Severities, in.Severity) {
		return RaiseResult{}, invalid("choose the severity: %s", strings.Join(auditfinding.Severities, ", "))
	}
	owner := firstNonEmpty(strings.TrimSpace(in.Owner), t.ItemOwner)
	if owner == "" {
		return RaiseResult{}, invalid("%s has no owner in the population: name the finding's owner", t.ItemRef)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = fmt.Sprintf("%s %s not met on %s %s", a.AttrRef, firstNonEmpty(a.Domain, "attribute"), t.ItemRef, t.ItemName)
	}
	criteria := a.Requirement
	if a.PassCriteria != "" {
		criteria = strings.TrimSpace(criteria + "\n\nPass criteria: " + a.PassCriteria)
	}
	condition := strings.TrimSpace(strings.Join(nonEmpty(t.EvidenceSummary, r.Note), "\n\n"))
	if condition == "" {
		condition = fmt.Sprintf("%s failed %s in 2LoD test %s.", t.ItemRef, a.AttrRef, t.TestRef)
	}
	framework := nonEmpty(a.RegulatoryRefs)
	if a.NFRKey != "" {
		framework = append(framework, "Security NFR "+a.NFRKey)
	}
	f := auditfinding.Finding{
		Title:          clip(title, 500),
		Source:         "Second Line Review",
		Engagement:     p.Reference + " " + p.Title,
		Auditor:        t.Tester,
		FindingType:    strings.TrimSpace(in.FindingType),
		Severity:       in.Severity,
		DeficiencyType: "Operating Effectiveness",
		Criteria:       criteria,
		Condition:      condition,
		BusinessUnit:   p.FunctionName,
		Owner:          owner,
		IdentifiedDate: firstNonEmpty(t.FieldworkCompletedOn, s.today()),
		DueDate:        strings.TrimSpace(in.DueDate),
		Status:         strings.TrimSpace(in.Status),
		LinkedControls: strings.Join(a.ControlIDs, ", "),
		FrameworkRefs:  strings.Join(framework, "; "),
		OriginRef:      originRef(p, t, a),
		Notes:          fmt.Sprintf("Item %s %s, stratum %s, %s pick of Q%d.", t.ItemRef, t.ItemName, t.StratumCode, strings.ToLower(t.Basis), t.Quarter),
	}
	if f.Status != "" && f.Status != auditfinding.StatusDraft && f.Status != auditfinding.StatusOpen {
		return RaiseResult{}, invalid("a raised finding starts as Draft or Open")
	}
	created, err := s.in.Findings.Create(f, actor)
	if err != nil {
		var ve *auditfinding.ValidationError
		if errors.As(err, &ve) {
			return RaiseResult{}, invalid("Audit Findings refused the finding: %s", ve.Error())
		}
		return RaiseResult{}, fmt.Errorf("raise finding: %w", err)
	}
	h := HistoryEntry{At: s.stamp(), Actor: actor, Event: "finding", Subject: t.TestRef + " " + a.AttrRef,
		ToValue: created.Reference, Note: fmt.Sprintf("%s, owner %s, due %s", created.Severity, created.Owner, firstNonEmpty(created.DueDate, "—"))}
	if err := s.repo.LinkFinding(id, r.ID, created.ID, created.Reference, created.Severity, []HistoryEntry{h}); err != nil {
		return RaiseResult{}, fmt.Errorf("finding %s was raised but not linked to %s: %w", created.Reference, t.TestRef, err)
	}
	r.FindingID, r.FindingRef, r.Severity = created.ID, created.Reference, created.Severity
	return RaiseResult{Result: r, Finding: created}, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func nonEmpty(values ...string) []string {
	out := []string{}
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ---- deviation rule ----

// Deviation is the rule's conclusion for one attribute in one stratum.
type Deviation struct {
	AttributeID   int64    `json:"attribute_id"`
	AttrRef       string   `json:"attr_ref"`
	Domain        string   `json:"domain"`
	StratumCode   string   `json:"stratum"`
	Tested        int      `json:"tested"`
	Fails         int      `json:"fails"`
	Kind          string   `json:"kind"`
	Conclusion    string   `json:"conclusion"`
	Failures      []string `json:"failures"`
	ExtendQuarter int      `json:"extend_quarter"`
	ExtendBy      int      `json:"extend_by"`
	// FullCoverage marks a stratum with sampling off: every item is tested,
	// so a deviation below the not-effective threshold has no sample to
	// extend and stands as the findings raised on it.
	FullCoverage bool      `json:"full_coverage"`
	Thematic     *Thematic `json:"thematic,omitempty"`
}

// computeDeviations applies the deviation rule to every attribute in every
// stratum it applies to, cumulatively over the year's tests.
func computeDeviations(p Plan, tests []Test, view SamplingView, pushed []Thematic) []Deviation {
	out := []Deviation{}
	tests = slices.Clone(tests)
	sort.SliceStable(tests, func(i, j int) bool { return tests[i].TestRef < tests[j].TestRef })
	for _, a := range p.Attributes {
		for si, st := range p.Strata {
			if !a.appliesTo(st.Code) {
				continue
			}
			d := Deviation{AttributeID: a.ID, AttrRef: a.AttrRef, Domain: a.Domain, StratumCode: st.Code, Failures: []string{}}
			lastQ := 0
			for _, t := range tests {
				if t.StratumCode != st.Code {
					continue
				}
				for _, r := range t.Results {
					if r.AttributeID != a.ID {
						continue
					}
					switch r.Result {
					case ResultFail:
						d.Fails++
						d.Failures = append(d.Failures, t.TestRef+" "+t.ItemRef)
					case ResultPass:
					default:
						continue
					}
					d.Tested++
					lastQ = max(lastQ, t.Quarter)
				}
			}
			d.Kind, d.Conclusion = Conclude(d.Fails, d.Tested, p.Parameters)
			d.FullCoverage = !st.SamplingEnabled
			if d.Kind == KindExtend && d.FullCoverage {
				d.Conclusion = fmt.Sprintf("%d %s – full coverage, raise as findings", d.Fails, plural(d.Fails, "deviation", "deviations"))
			}
			if d.Kind == KindExtend && !d.FullCoverage && lastQ < 4 && si < len(view.Strata) {
				d.ExtendQuarter = lastQ + 1
				d.ExtendBy = ExtendBy(view.Strata[si].Tests[lastQ], p.Parameters.ExtendPct)
			}
			for i := range pushed {
				if pushed[i].AttributeID == a.ID && pushed[i].StratumCode == st.Code {
					th := pushed[i]
					d.Thematic = &th
				}
			}
			out = append(out, d)
		}
	}
	return out
}

func (s *Service) Deviations(id int64) ([]Deviation, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	return s.deviationsOf(p)
}

func (s *Service) deviationsOf(p Plan) ([]Deviation, error) {
	tests, err := s.testsOf(p, 0)
	if err != nil {
		return nil, err
	}
	view, err := s.Sampling(p.ID)
	if err != nil {
		return nil, err
	}
	pushed, err := s.repo.Thematics(p.ID)
	if err != nil {
		return nil, err
	}
	return computeDeviations(p, tests, view, pushed), nil
}

// ThematicInput is what the register needs that the plan cannot know: who
// owns the risk and how likely and severe it is.
type ThematicInput struct {
	AttributeID int64  `json:"attribute_id"`
	Stratum     string `json:"stratum"`
	Owner       string `json:"owner"`
	Likelihood  int    `json:"likelihood"`
	Impact      int    `json:"impact"`
}

type ThematicResult struct {
	Thematic Thematic          `json:"thematic"`
	Risk     riskregister.Risk `json:"risk"`
}

var riskIDUnsafe = regexp.MustCompile(`[^A-Za-z0-9.-]+`)

// PushThematic records a not-effective conclusion in the risk register. The
// residual rating starts at the inherent one: the register's owner, not the
// plan, decides what the controls are worth.
func (s *Service) PushThematic(id int64, in ThematicInput, actor string) (ThematicResult, error) {
	if s.in.Risks == nil {
		return ThematicResult{}, conflict("the risk register is not available to this service")
	}
	p, err := s.executable(id)
	if err != nil {
		return ThematicResult{}, err
	}
	devs, err := s.deviationsOf(p)
	if err != nil {
		return ThematicResult{}, err
	}
	in.Stratum = strings.TrimSpace(in.Stratum)
	di := slices.IndexFunc(devs, func(d Deviation) bool { return d.AttributeID == in.AttributeID && d.StratumCode == in.Stratum })
	if di < 0 {
		return ThematicResult{}, ErrNotFound
	}
	d := devs[di]
	switch {
	case d.Thematic != nil:
		return ThematicResult{}, conflict("%s in %s was pushed to the risk register as %s", d.AttrRef, d.StratumCode, d.Thematic.RiskRef)
	case d.Kind != KindNotEffective:
		return ThematicResult{}, conflict("%s in %s is %q; only a not-effective conclusion is a thematic finding", d.AttrRef, d.StratumCode, d.Conclusion)
	}
	in.Owner = strings.TrimSpace(in.Owner)
	if in.Owner == "" {
		return ThematicResult{}, invalid("name the risk's owner")
	}
	if in.Likelihood < 1 || in.Likelihood > 5 || in.Impact < 1 || in.Impact > 5 {
		return ThematicResult{}, invalid("likelihood and impact are 1 to 5")
	}
	ai := slices.IndexFunc(p.Attributes, func(a Attribute) bool { return a.ID == d.AttributeID })
	a := p.Attributes[ai]
	stratumName := d.StratumCode
	if i := slices.IndexFunc(p.Strata, func(st Stratum) bool { return st.Code == d.StratumCode }); i >= 0 {
		stratumName = p.Strata[i].Name
	}
	summary := fmt.Sprintf("%d deviation(s) in %d tests of %s in %s: %s", d.Fails, d.Tested, d.AttrRef, stratumName, strings.Join(firstN(d.Failures, 10), ", "))
	risk := riskregister.Risk{
		RiskID:             strings.Trim(riskIDUnsafe.ReplaceAllString(p.Reference+"-"+d.AttrRef+"-"+d.StratumCode, "-"), "-"),
		Title:              clip(fmt.Sprintf("%s %s not effective for %s", d.AttrRef, firstNonEmpty(a.Domain, ""), stratumName), 500),
		BusinessUnit:       p.FunctionName,
		Asset:              fmt.Sprintf("%s (%s)", stratumName, d.StratumCode),
		Vulnerability:      summary,
		CurrentControls:    strings.Join(a.ControlIDs, ", "),
		Likelihood:         in.Likelihood,
		Impact:             in.Impact,
		ResidualLikelihood: in.Likelihood,
		ResidualImpact:     in.Impact,
		Owner:              in.Owner,
		Status:             "Open",
		Notes: fmt.Sprintf("Thematic finding from assurance plan %s: attribute %s concluded not effective for stratum %s under the deviation rule (%d or more deviations in the year). Residual rating set equal to inherent until the owner assesses it.",
			p.Reference, d.AttrRef, d.StratumCode, p.Parameters.DeviationThematicAt),
	}
	created, err := s.in.Risks.Create(risk)
	if err != nil {
		return ThematicResult{}, invalid("the risk register refused the risk: %v", err)
	}
	stamp := s.stamp()
	th := Thematic{AttributeID: d.AttributeID, AttrRef: d.AttrRef, StratumCode: d.StratumCode, RiskID: created.ID,
		RiskRef: created.RiskID, Deviations: d.Fails, Note: summary, PushedAt: stamp, PushedBy: actor}
	h := HistoryEntry{At: stamp, Actor: actor, Event: "thematic", Subject: d.AttrRef + " " + d.StratumCode,
		ToValue: created.RiskID, Note: summary}
	if err := s.repo.AddThematic(id, th, []HistoryEntry{h}); err != nil {
		return ThematicResult{}, fmt.Errorf("risk %s was created but not recorded on %s: %w", created.RiskID, p.Reference, err)
	}
	return ThematicResult{Thematic: th, Risk: created}, nil
}
