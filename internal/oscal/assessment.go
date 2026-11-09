package oscal

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The media types of the documents an assessment export points at. grc's own
// Assessment Plan is linked as generic OSCAL JSON, the type FedRAMP's content
// uses for OSCAL documents, rather than with a model-specific type OSCAL does
// not publish; the approval snapshot is plain JSON.
const (
	OSCALMediaType = "application/oscal+json"
	JSONMediaType  = "application/json"
)

// ErrNoReviewedControls is a plan whose test script names no control or NFR
// that resolved when it was approved. OSCAL requires an Assessment Plan to
// list the controls it reviews, and include-all would claim every control.
var ErrNoReviewedControls = errors.New("no attribute of the test script names a control or NFR")

// ExportPlan is one approval snapshot of an Assurance Plan: the plan's design
// and its population as approved. It never changes, so neither does its
// Assessment Plan.
type ExportPlan struct {
	Reference          string
	Title              string
	PlanType           string
	Function           string
	Client             string
	PeriodStart        string
	PeriodEnd          string
	Objective          string
	DecisionsRequested string
	CoverageBasis      string
	PopulationSource   string
	// SeedCommitment is the SHA-256 of the selection seed; the seed itself is
	// not in the snapshot.
	SeedCommitment string
	AIGenerated    bool
	Model          string
	PromptHash     string

	Version      int
	ApprovedBy   string
	ApprovedAt   time.Time
	ChangeReason string
	// SnapshotHref serves the snapshot's stored bytes, and SnapshotSHA256 is
	// the hash stored with them at approval.
	SnapshotHref   string
	SnapshotSHA256 string

	Parameters  []NamedValue
	Steps       []ExportPlanStep
	Quarters    []ExportQuarter
	Strata      []ExportStratum
	Attributes  []ExportAttribute
	Population  []ExportItem
	RACIRoles   []string
	RACI        []ExportRACIRow
	Cadence     []ExportCadence
	Escalations []ExportEscalation
	Basis       []ExportBasisRow
}

// ExportAttribute is one line of a plan's test script.
type ExportAttribute struct {
	Ref               string
	Domain            string
	Requirement       string
	Procedure         string
	Method            string
	EvidenceRequested string
	PassCriteria      string
	AppliesTo         []string
	RegulatoryRefs    string
	ControlIDs        []string
	NFRKey            string
	// UnresolvedControls and NFRUnresolved are the references that did not
	// resolve against grc's catalogs when the plan was read; they are left out
	// of what the plan says it reviews.
	UnresolvedControls []string
	NFRUnresolved      bool
	AIGenerated        bool
	Model              string
	PromptHash         string
}

// ExportItem is one item of a plan's scope population.
type ExportItem struct {
	Ref            string
	Name           string
	Stratum        string
	CIF            string
	BusinessLine   string
	Hosting        string
	InternetFacing bool
	Owner          string
	Wave           int
	LastTestedOn   string
	SourceKind     string
	SourceRef      string
}

type ExportPlanStep struct {
	Ref             string
	Phase           string
	Title           string
	Milestone       bool
	ResponsibleRole string
	AccountableRole string
	Start           string
	End             string
	Deliverable     string
	Refs            string
	Quarter         int
}

type ExportQuarter struct {
	Quarter          int
	LOD1WindowStart  string
	LOD1Deadline     string
	SelectionDate    string
	FieldworkStart   string
	FieldworkEnd     string
	FindingsAgreedBy string
	ReportDate       string
	Committee        string
	RetestDays       float64
	OverheadDays     float64
}

type ExportStratum struct {
	Code            string
	Name            string
	Definition      string
	SamplingEnabled bool
	Confidence      float64
	TolerableRate   float64
	FloorPct        float64
	EffortPerTest   float64
	EffortPerReview float64
	Waves           [4]float64
	TopUps          [4]int
	PlannedSize     int
	CycleYears      int
	Takes           []string
}

type ExportRACIRow struct {
	Activity string
	Cells    map[string]string
}

type ExportCadence struct {
	Forum     string
	Frequency string
	Content   string
	Owner     string
}

type ExportEscalation struct {
	Trigger    string
	EscalateTo string
	Within     string
}

type ExportBasisRow struct {
	Requirement string
	Reference   string
	How         string
}

// CatalogHrefs are where grc serves its two catalogs, with their titles, for
// the back-matter resources an assessment document points at.
type CatalogHrefs struct {
	Control      string
	ControlTitle string
	NFR          string
	NFRTitle     string
}

// reference is one control or NFR an attribute names, as an OSCAL control id.
type reference struct {
	id  string
	nfr bool
}

// references are the attribute's controls (in its order) and then its NFR,
// as OSCAL ids, without the ones that did not resolve, which come back as
// written.
func (a ExportAttribute) references() (refs []reference, left []string) {
	unresolved := map[string]bool{}
	for _, u := range a.UnresolvedControls {
		unresolved[strings.ToUpper(strings.TrimSpace(u))] = true
	}
	seen := map[string]bool{}
	add := func(r reference) {
		if !seen[r.id] {
			seen[r.id] = true
			refs = append(refs, r)
		}
	}
	for _, c := range a.ControlIDs {
		c = strings.TrimSpace(c)
		switch {
		case c == "":
		case unresolved[strings.ToUpper(c)]:
			left = append(left, c)
		default:
			add(reference{id: ControlToken(c)})
		}
	}
	if k := strings.TrimSpace(a.NFRKey); k != "" {
		if a.NFRUnresolved {
			left = append(left, "NFR "+k)
		} else {
			add(reference{id: NFRToken(k), nfr: true})
		}
	}
	return refs, left
}

func referenceIDs(refs []reference) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.id)
	}
	return out
}

// ObjectiveID is the id of the assessment-objective part an attribute's
// local objective carries in the Assessment Plan, which a finding in the
// Assessment Results targets.
func ObjectiveID(attrRef string) string {
	return Token("attr", attrRef) + "_obj"
}

// inventoryItemUUID is keyed on the plan and the item, not the version, so an
// item is the same inventory item in every version of the plan that holds it,
// and an Assessment Results subject resolves in any of them.
func inventoryItemUUID(planRef, itemRef string) string {
	return UUID("inventory-item", strings.TrimSpace(planRef)+"\x00"+strings.TrimSpace(itemRef))
}

// assessmentMethod is SP 800-53A's word, which OSCAL's method values use, for
// one of grc's methods. Observation is examining: 800-53A's EXAMINE covers
// observing a mechanism or an activity.
func assessmentMethod(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "inspection", "observation":
		return "EXAMINE"
	case "inquiry":
		return "INTERVIEW"
	case "re-performance":
		return "TEST"
	}
	return ""
}

// day is a grc date (YYYY-MM-DD) as an OSCAL date-time: midnight UTC.
func day(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	return t.UTC(), err == nil
}

func num(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func count(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// uniqueKey makes repeated references distinct, so two steps that share a
// reference still get two UUIDs.
type uniqueKey map[string]int

func (u uniqueKey) of(key string) string {
	u[key]++
	if n := u[key]; n > 1 {
		return key + "#" + strconv.Itoa(n)
	}
	return key
}

// planReviewed is what the plan's test script says it reviews: the SP 800-53
// controls and the NFRs, in the order the script first names them, and the
// references left out because they did not resolve.
func planReviewed(attrs []ExportAttribute) (controls, nfrs, left []string) {
	seen := map[string]bool{}
	for _, a := range attrs {
		refs, unresolved := a.references()
		for _, u := range unresolved {
			left = append(left, u+" ("+strings.TrimSpace(a.Ref)+")")
		}
		for _, r := range refs {
			if seen[r.id] {
				continue
			}
			seen[r.id] = true
			if r.nfr {
				nfrs = append(nfrs, r.id)
			} else {
				controls = append(controls, r.id)
			}
		}
	}
	return controls, nfrs, left
}

func assessed(ids []string) *[]AssessedControl {
	out := make([]AssessedControl, 0, len(ids))
	for _, id := range ids {
		out = append(out, AssessedControl{ControlId: id})
	}
	return &out
}

// reviewedControls lists the controls and the NFRs as one selection per
// catalog, each pointing at its catalog through a back-matter resource. It
// never uses include-all.
func reviewedControls(controls, nfrs []string, cats CatalogHrefs) (ReviewedControls, []Resource) {
	var rc ReviewedControls
	var resources []Resource
	if len(controls) > 0 {
		res, href := documentResource(cats.ControlTitle, cats.Control)
		resources = append(resources, res)
		rc.ControlSelections = append(rc.ControlSelections, AssessedControls{
			Description:     "SP 800-53 controls, as ids in grc's control catalog.",
			IncludeControls: assessed(controls),
			Links:           &[]Link{{Href: href, Rel: "reference", Text: oneLine(cats.ControlTitle)}},
		})
	}
	if len(nfrs) > 0 {
		res, href := documentResource(cats.NFRTitle, cats.NFR)
		resources = append(resources, res)
		rc.ControlSelections = append(rc.ControlSelections, AssessedControls{
			Description:     "Security NFRs, as ids in grc's Security NFR catalog.",
			IncludeControls: assessed(nfrs),
			Links:           &[]Link{{Href: href, Rel: "reference", Text: oneLine(cats.NFRTitle)}},
		})
	}
	return rc, resources
}

// ExportAssessmentPlan writes one approval snapshot as an OSCAL Assessment
// Plan. grc holds no system security plan: import-ssp points at the approval
// snapshot itself, a back-matter resource titled as what it is, with the
// SHA-256 stored at approval.
func ExportAssessmentPlan(in ExportPlan, cats CatalogHrefs) ([]byte, error) {
	ref := strings.TrimSpace(in.Reference)
	controls, nfrs, left := planReviewed(in.Attributes)
	if len(controls)+len(nfrs) == 0 {
		return nil, ErrNoReviewedControls
	}
	reviewed, catalogs := reviewedControls(controls, nfrs, cats)
	reviewed.Description = "The controls and Security NFRs the plan's test script names."
	if len(left) > 0 {
		reviewed.Remarks = "Left out: references that did not resolve against grc's catalogs: " + strings.Join(left, "; ") + "."
	}

	snapshot := snapshotResource(in)
	doc := struct {
		AssessmentPlan *AssessmentPlan `json:"assessment-plan"`
	}{AssessmentPlan: &AssessmentPlan{
		Metadata: NewMetadata(MetadataInput{
			Title:        in.Title,
			Version:      fmt.Sprintf("v%d", in.Version),
			LastModified: in.ApprovedAt,
			Remarks: strings.TrimSpace(strings.TrimSpace(in.Objective) + "\n\n" + fmt.Sprintf(
				"Exported from grc: version %d of %s as approved. grc holds no system security plan; the plan's scope is its population as approved.", in.Version, ref)),
			Props: []NamedValue{
				{Name: "reference", Value: ref},
				{Name: "plan-type", Value: in.PlanType},
				{Name: "function", Value: in.Function},
				{Name: "client", Value: in.Client},
				{Name: "period-start", Value: in.PeriodStart},
				{Name: "period-end", Value: in.PeriodEnd},
				{Name: "approval-version", Value: strconv.Itoa(in.Version)},
				{Name: "approved-by", Value: in.ApprovedBy},
				{Name: "change-reason", Value: in.ChangeReason},
				{Name: "coverage-basis", Value: in.CoverageBasis},
				{Name: "population-source", Value: in.PopulationSource},
				{Name: "seed-commitment", Value: in.SeedCommitment},
				{Name: "ai-generated", Value: yesOrEmpty(in.AIGenerated)},
				{Name: "model", Value: in.Model},
				{Name: "prompt-hash", Value: in.PromptHash},
			},
		}),
		ImportSsp: ImportSSP{
			Href: "#" + snapshot.UUID,
			Remarks: "grc holds no system security plan. This plan's scope is its population as approved: the back-matter resource " +
				"is the approval snapshot (the design and the population, with the SHA-256 stored at approval), and local-definitions " +
				"lists the population as inventory items.",
		},
		LocalDefinitions:   planDefinitions(in),
		ReviewedControls:   reviewed,
		AssessmentSubjects: planSubjects(in),
		Tasks:              planTasks(in),
		TermsAndConditions: planTerms(in),
		BackMatter:         backMatter(append([]Resource{snapshot}, catalogs...)...),
	}}
	return finishDocument("assessment-plan", ref+"/v"+strconv.Itoa(in.Version), &doc, &doc.AssessmentPlan.UUID, &doc.AssessmentPlan.Metadata, in.ApprovedAt)
}

func yesOrEmpty(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func snapshotResource(in ExportPlan) Resource {
	link := ResourceLink{Href: in.SnapshotHref, MediaType: JSONMediaType}
	if sum := strings.TrimSpace(in.SnapshotSHA256); sum != "" {
		link.Hashes = &[]Hash{{Algorithm: "SHA-256", Value: sum}}
	}
	return Resource{
		UUID:        UUID("resource", in.SnapshotHref),
		Title:       fmt.Sprintf("Scope population of %s as approved in version %d (not a system security plan)", strings.TrimSpace(in.Reference), in.Version),
		Description: "grc's approval snapshot: the plan's design and the population it was approved on, as stored when it was approved.",
		Props:       propsOrNil(grcProp("kind", "approval-snapshot")),
		Rlinks:      &[]ResourceLink{link},
	}
}

func itemProps(it ExportItem) []Property {
	props := GRCProps(
		NamedValue{Name: "item-ref", Value: it.Ref},
		NamedValue{Name: "stratum", Value: it.Stratum},
		NamedValue{Name: "cif", Value: it.CIF},
		NamedValue{Name: "business-line", Value: it.BusinessLine},
		NamedValue{Name: "hosting", Value: it.Hosting},
		NamedValue{Name: "internet-facing", Value: yesNo(it.InternetFacing)},
		NamedValue{Name: "owner", Value: it.Owner},
		NamedValue{Name: "wave", Value: waveName(it.Wave)},
		NamedValue{Name: "last-tested-on", Value: it.LastTestedOn},
		NamedValue{Name: "source-kind", Value: it.SourceKind},
		NamedValue{Name: "source-ref", Value: it.SourceRef},
	)
	return props
}

func waveName(q int) string {
	if q < 1 || q > 4 {
		return ""
	}
	return "Q" + strconv.Itoa(q)
}

func inventoryItem(planRef string, it ExportItem) InventoryItem {
	return InventoryItem{
		UUID:        inventoryItemUUID(planRef, it.Ref),
		Description: firstNonEmpty(it.Name, it.Ref),
		Props:       propsOrNil(itemProps(it)),
	}
}

func planDefinitions(in ExportPlan) *LocalDefinitions {
	ref := strings.TrimSpace(in.Reference)
	defs := &LocalDefinitions{}
	items := make([]InventoryItem, 0, len(in.Population))
	for _, it := range in.Population {
		items = append(items, inventoryItem(ref, it))
	}
	if len(items) > 0 {
		defs.InventoryItems = &items
		defs.Remarks = "The inventory items are the plan's scope population as approved, not components of a system security plan."
	}

	var objectives []LocalObjective
	var activities []Activity
	keys := uniqueKey{}
	for _, a := range in.Attributes {
		key := keys.of(ref + "\x00" + strings.TrimSpace(a.Ref))
		refs, _ := a.references()
		if len(refs) > 0 {
			objectives = append(objectives, localObjective(a, refs))
		}
		activities = append(activities, activity(a, refs, key))
	}
	if len(objectives) > 0 {
		defs.ObjectivesAndMethods = &objectives
	}
	if len(activities) > 0 {
		defs.Activities = &activities
	}
	return defs
}

func localObjective(a ExportAttribute, refs []reference) LocalObjective {
	props := grcProp("attribute", a.Ref)
	for _, r := range refs[1:] {
		appendProps(&props, grcProp("also-reviews", r.id)...)
	}
	parts := []Part{{
		ID:    ObjectiveID(a.Ref),
		Name:  "assessment-objective",
		Prose: firstNonEmpty(a.PassCriteria, a.Requirement),
	}}
	if e := strings.TrimSpace(a.EvidenceRequested); e != "" {
		parts = append(parts, Part{Name: "evidence-requested", Ns: Namespace, Prose: e})
	}
	return LocalObjective{
		ControlId:   refs[0].id,
		Description: strings.TrimSpace(a.Requirement),
		Props:       propsOrNil(props),
		Parts:       parts,
	}
}

func activity(a ExportAttribute, refs []reference, key string) Activity {
	var props []Property
	if m := assessmentMethod(a.Method); m != "" {
		props = append(props, Property{Name: "method", Value: m})
	}
	appendProps(&props, GRCProps(
		NamedValue{Name: "attribute", Value: a.Ref},
		NamedValue{Name: "method", Value: a.Method},
	)...)
	for _, code := range a.AppliesTo {
		appendProps(&props, grcProp("applies-to", code)...)
	}
	appendProps(&props, GRCProps(
		NamedValue{Name: "regulatory-refs", Value: a.RegulatoryRefs},
		NamedValue{Name: "ai-generated", Value: yesOrEmpty(a.AIGenerated)},
		NamedValue{Name: "model", Value: a.Model},
		NamedValue{Name: "prompt-hash", Value: a.PromptHash},
	)...)
	act := Activity{
		UUID:        UUID("activity", key),
		Title:       oneLine(a.Ref + " " + a.Domain),
		Description: firstNonEmpty(a.Requirement, a.Domain, a.Ref),
		Props:       propsOrNil(props),
	}
	if p := strings.TrimSpace(a.Procedure); p != "" {
		act.Steps = &[]Step{{UUID: UUID("activity-step", key), Title: "Procedure", Description: p}}
	}
	if len(refs) > 0 {
		act.RelatedControls = &ReviewedControls{ControlSelections: []AssessedControls{{IncludeControls: assessed(referenceIDs(refs))}}}
	}
	return act
}

// planSubjects is one subject per stratum that has items. A stratum approved
// on its planned size alone is not a subject: OSCAL would need include-all,
// which claims everything.
func planSubjects(in ExportPlan) *[]AssessmentSubject {
	ref := strings.TrimSpace(in.Reference)
	byStratum := map[string][]SelectSubjectByID{}
	for _, it := range in.Population {
		code := strings.TrimSpace(it.Stratum)
		byStratum[code] = append(byStratum[code], SelectSubjectByID{SubjectUuid: inventoryItemUUID(ref, it.Ref), Type: "inventory-item"})
	}
	var out []AssessmentSubject
	for _, s := range in.Strata {
		items := byStratum[strings.TrimSpace(s.Code)]
		if len(items) == 0 {
			continue
		}
		out = append(out, AssessmentSubject{
			Type:            "inventory-item",
			Description:     firstNonEmpty(s.Definition, s.Name, s.Code),
			Props:           propsOrNil(GRCProps(NamedValue{Name: "code", Value: s.Code}, NamedValue{Name: "name", Value: s.Name})),
			IncludeSubjects: &items,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func timing(start, end string) *EventTiming {
	s, okS := day(start)
	e, okE := day(end)
	switch {
	case okS && okE && e.After(s):
		return &EventTiming{WithinDateRange: &OnDateRange{Start: s, End: e}}
	case okS:
		return &EventTiming{OnDate: &OnDate{Date: s}}
	case okE:
		return &EventTiming{OnDate: &OnDate{Date: e}}
	}
	return nil
}

func planTasks(in ExportPlan) *[]Task {
	ref := strings.TrimSpace(in.Reference)
	keys := uniqueKey{}
	out := make([]Task, 0, len(in.Steps))
	for _, s := range in.Steps {
		kind := "action"
		if s.Milestone {
			kind = "milestone"
		}
		out = append(out, Task{
			UUID:   UUID("task", keys.of(ref+"\x00"+strings.TrimSpace(s.Ref))),
			Type:   kind,
			Title:  firstNonEmpty(oneLine(s.Title), "Step "+strings.TrimSpace(s.Ref)),
			Timing: timing(s.Start, s.End),
			Props: propsOrNil(GRCProps(
				NamedValue{Name: "step-ref", Value: s.Ref},
				NamedValue{Name: "phase", Value: s.Phase},
				NamedValue{Name: "quarter", Value: waveName(s.Quarter)},
				NamedValue{Name: "responsible-role", Value: s.ResponsibleRole},
				NamedValue{Name: "accountable-role", Value: s.AccountableRole},
				NamedValue{Name: "deliverable", Value: s.Deliverable},
				NamedValue{Name: "refs", Value: s.Refs},
			)),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func grcPart(name, title, prose string, props []Property, parts []AssessmentPart) AssessmentPart {
	p := AssessmentPart{Name: name, Ns: Namespace, Title: oneLine(title), Prose: strings.TrimSpace(prose), Props: propsOrNil(props)}
	if len(parts) > 0 {
		p.Parts = &parts
	}
	return p
}

// raciProps names each RACI cell by its letter and holds the role, since a
// role's name is not a token a prop could be named by.
func raciProps(roles []string, cells map[string]string) []Property {
	order := make([]string, 0, len(cells))
	listed := map[string]bool{}
	for _, r := range roles {
		if _, ok := cells[r]; ok && !listed[r] {
			listed[r] = true
			order = append(order, r)
		}
	}
	var rest []string
	for r := range cells {
		if !listed[r] {
			rest = append(rest, r)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)

	names := map[string][]string{
		"R": {"responsible"}, "A": {"accountable"}, "C": {"consulted"}, "I": {"informed"},
		"A/R": {"accountable", "responsible"},
	}
	var props []Property
	for _, role := range order {
		for _, name := range names[strings.ToUpper(strings.TrimSpace(cells[role]))] {
			appendProps(&props, grcProp(name, role)...)
		}
	}
	return props
}

func planTerms(in ExportPlan) *TermsAndConditions {
	var parts []AssessmentPart
	if props := GRCProps(in.Parameters...); len(props) > 0 {
		parts = append(parts, grcPart("parameters", "Plan parameters", "", props, nil))
	}

	var strata []AssessmentPart
	for _, s := range in.Strata {
		props := GRCProps(
			NamedValue{Name: "code", Value: s.Code},
			NamedValue{Name: "sampling-enabled", Value: yesNo(s.SamplingEnabled)},
			NamedValue{Name: "confidence", Value: num(s.Confidence)},
			NamedValue{Name: "tolerable-rate", Value: num(s.TolerableRate)},
			NamedValue{Name: "floor-pct", Value: num(s.FloorPct)},
			NamedValue{Name: "effort-per-test", Value: num(s.EffortPerTest)},
			NamedValue{Name: "effort-per-review", Value: num(s.EffortPerReview)},
		)
		for i, w := range s.Waves {
			appendProps(&props, grcProp(fmt.Sprintf("wave-q%d", i+1), num(w))...)
		}
		for i, n := range s.TopUps {
			appendProps(&props, grcProp(fmt.Sprintf("top-up-q%d", i+1), strconv.Itoa(n))...)
		}
		appendProps(&props, GRCProps(
			NamedValue{Name: "planned-size", Value: count(s.PlannedSize)},
			NamedValue{Name: "cycle-years", Value: count(s.CycleYears)},
		)...)
		for _, t := range s.Takes {
			appendProps(&props, grcProp("takes", t)...)
		}
		strata = append(strata, grcPart("stratum", s.Code+" "+s.Name, s.Definition, props, nil))
	}
	if len(strata) > 0 {
		parts = append(parts, grcPart("sampling", "Sampling", "", nil, strata))
	}

	var quarters []AssessmentPart
	for _, q := range in.Quarters {
		props := GRCProps(
			NamedValue{Name: "lod1-window-start", Value: q.LOD1WindowStart},
			NamedValue{Name: "lod1-deadline", Value: q.LOD1Deadline},
			NamedValue{Name: "selection-date", Value: q.SelectionDate},
			NamedValue{Name: "fieldwork-start", Value: q.FieldworkStart},
			NamedValue{Name: "fieldwork-end", Value: q.FieldworkEnd},
			NamedValue{Name: "findings-agreed-by", Value: q.FindingsAgreedBy},
			NamedValue{Name: "report-date", Value: q.ReportDate},
			NamedValue{Name: "committee", Value: q.Committee},
			NamedValue{Name: "retest-days", Value: num(q.RetestDays)},
			NamedValue{Name: "overhead-days", Value: num(q.OverheadDays)},
		)
		quarters = append(quarters, grcPart("quarter", "Q"+strconv.Itoa(q.Quarter), "", props, nil))
	}
	if len(quarters) > 0 {
		parts = append(parts, grcPart("quarterly-cycle", "Quarterly cycle", "", nil, quarters))
	}

	var raci []AssessmentPart
	for _, row := range in.RACI {
		raci = append(raci, grcPart("activity", firstNonEmpty(row.Activity, "Activity"), "", raciProps(in.RACIRoles, row.Cells), nil))
	}
	if len(raci) > 0 {
		parts = append(parts, grcPart("raci", "RACI", "", propsList("role", in.RACIRoles), raci))
	}

	var cadence []AssessmentPart
	for _, c := range in.Cadence {
		cadence = append(cadence, grcPart("forum", firstNonEmpty(c.Forum, "Forum"), c.Content, GRCProps(
			NamedValue{Name: "frequency", Value: c.Frequency},
			NamedValue{Name: "owner", Value: c.Owner},
		), nil))
	}
	if len(cadence) > 0 {
		parts = append(parts, grcPart("cadence", "Reporting cadence", "", nil, cadence))
	}

	var escalations []AssessmentPart
	for _, e := range in.Escalations {
		escalations = append(escalations, grcPart("escalation", "", e.Trigger, GRCProps(
			NamedValue{Name: "escalate-to", Value: e.EscalateTo},
			NamedValue{Name: "within", Value: e.Within},
		), nil))
	}
	if len(escalations) > 0 {
		parts = append(parts, grcPart("escalations", "Escalations", "", nil, escalations))
	}

	var basis []AssessmentPart
	for _, b := range in.Basis {
		basis = append(basis, grcPart("requirement", b.Requirement, b.How, grcProp("reference", b.Reference), nil))
	}
	if len(basis) > 0 {
		parts = append(parts, grcPart("regulatory-basis", "Regulatory basis", "", nil, basis))
	}

	if d := strings.TrimSpace(in.DecisionsRequested); d != "" {
		parts = append(parts, grcPart("decisions-requested", "Decisions requested", d, nil, nil))
	}
	if len(parts) == 0 {
		return nil
	}
	return &TermsAndConditions{Parts: &parts}
}

// propsList is one grc prop named name per value, in order.
func propsList(name string, values []string) []Property {
	var out []Property
	for _, v := range values {
		appendProps(&out, grcProp(name, v)...)
	}
	return out
}
