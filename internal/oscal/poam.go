package oscal

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Audit Findings statuses, as the POA&M reads them. A test in internal/app
// holds this list to auditfinding's.
const (
	FindingDraft             = "Draft"
	FindingOpen              = "Open"
	FindingInRemediation     = "In Remediation"
	FindingPendingValidation = "Pending Validation"
	FindingClosed            = "Closed"
	FindingRiskAccepted      = "Risk Accepted"
	FindingReopened          = "Reopened"
)

// POAMRiskStatus is the OSCAL risk status of each Audit Findings status the
// POA&M can carry. OSCAL has no "validating" state, so Pending Validation (the
// fix is in, its validation is not) is remediating. A risk acceptance is a
// deviation-approved risk while it is in force and open once it has expired.
// Draft has no entry: a draft has not been issued and is never exported.
var POAMRiskStatus = map[string]string{
	FindingOpen:              "open",
	FindingReopened:          "open",
	FindingInRemediation:     "remediating",
	FindingPendingValidation: "remediating",
	FindingRiskAccepted:      "deviation-approved",
	FindingClosed:            "closed",
}

// POAMDefaultStatuses are the statuses the POA&M carries without a filter:
// everything issued and not closed.
var POAMDefaultStatuses = []string{FindingOpen, FindingInRemediation, FindingPendingValidation, FindingReopened, FindingRiskAccepted}

var (
	// ErrUnknownFindingStatus is a status filter that is not an Audit
	// Findings status.
	ErrUnknownFindingStatus = errors.New("not an Audit Findings status")
	// ErrDraftNotExported is a filter asking for drafts, which have not been
	// issued.
	ErrDraftNotExported = errors.New("draft findings have not been issued and are not exported")
	// ErrNoFindings is a POA&M with no finding to carry.
	ErrNoFindings = errors.New("no audit finding matches")
)

// POAMSystemID is the POA&M's system-id: an identifier for grc's Audit
// Findings register, in grc's namespace. OSCAL asks a POA&M for an imported
// SSP or a unique system-id; grc holds no SSP, and this names the register,
// not a system.
const POAMSystemID = "audit-findings"

// ExportAuditFinding is one Audit Findings record.
type ExportAuditFinding struct {
	Reference               string
	Title                   string
	Source                  string
	Engagement              string
	Auditor                 string
	FindingType             string
	Severity                string
	DeficiencyType          string
	Criteria                string
	Condition               string
	Cause                   string
	Effect                  string
	Recommendation          string
	RootCauseCategory       string
	BusinessUnit            string
	ProcessArea             string
	Owner                   string
	ManagementResponse      string
	ManagementResponseText  string
	IdentifiedDate          string
	ReportDate              string
	OriginalDueDate         string
	DueDate                 string
	ExtensionCount          int
	ExtensionReason         string
	Status                  string
	RepeatFinding           bool
	PriorReference          string
	LinkedRisks             string
	LinkedControls          string
	FrameworkRefs           string
	OriginRef               string
	ValidatedBy             string
	ValidationDate          string
	ValidationNotes         string
	ClosureEvidence         string
	ClosedDate              string
	RiskAcceptedBy          string
	RiskAcceptanceRationale string
	RiskAcceptanceExpiry    string
	CreatedAt               time.Time
	Actions                 []ExportAction
}

// ExportAction is one step of a finding's management action plan.
type ExportAction struct {
	Reference     string
	Description   string
	ActionType    string
	Owner         string
	DueDate       string
	Status        string
	CompletedDate string
	Evidence      string
}

// POAMExport is the Audit Findings register as a POA&M. Status filters it to
// one status; Today is the date an acceptance's expiry is read against.
type POAMExport struct {
	Title        string
	LastModified time.Time
	Today        string
	Status       string
	Findings     []ExportAuditFinding
}

// POAMIncludes reports whether a finding with this status is in a POA&M
// filtered to filter ("" for the default set). Draft never is; Closed only
// when asked for.
func POAMIncludes(status, filter string) bool {
	if filter != "" {
		return status == filter
	}
	for _, s := range POAMDefaultStatuses {
		if status == s {
			return true
		}
	}
	return false
}

// CheckPOAMStatus checks a status filter.
func CheckPOAMStatus(filter string) error {
	switch {
	case filter == "":
		return nil
	case filter == FindingDraft:
		return ErrDraftNotExported
	case POAMRiskStatus[filter] == "":
		return fmt.Errorf("%w: %q", ErrUnknownFindingStatus, filter)
	}
	return nil
}

// acceptanceExpired reports a risk acceptance whose expiry is before today,
// the rule the Audit Findings summary counts by.
func acceptanceExpired(f ExportAuditFinding, today string) bool {
	expiry := strings.TrimSpace(f.RiskAcceptanceExpiry)
	return f.Status == FindingRiskAccepted && expiry != "" && expiry < today
}

func listOf(s string) []string {
	var out []string
	for _, v := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// findingDescription is the criteria, condition, cause and effect as labelled
// paragraphs, verbatim: a poam-item has no parts to hold them apart.
func findingDescription(f ExportAuditFinding) string {
	var paras []string
	for _, p := range []struct{ label, text string }{
		{"Criteria", f.Criteria}, {"Condition", f.Condition}, {"Cause", f.Cause}, {"Effect", f.Effect},
	} {
		if t := strings.TrimSpace(p.text); t != "" {
			paras = append(paras, p.label+": "+t)
		}
	}
	if len(paras) == 0 {
		return "No criteria, condition, cause or effect is recorded for this finding."
	}
	return strings.Join(paras, "\n\n")
}

// ExportPOAM writes the Audit Findings register as an OSCAL Plan of Action and
// Milestones: a poam-item per finding, with an observation (the condition)
// and a risk (the effect, the status, the due date and the remediations).
func ExportPOAM(in POAMExport) ([]byte, error) {
	filter := strings.TrimSpace(in.Status)
	if err := CheckPOAMStatus(filter); err != nil {
		return nil, err
	}
	findings := append([]ExportAuditFinding(nil), in.Findings...)
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Reference < findings[j].Reference })

	var items []POAMItem
	var observations []Observation
	var risks []Risk
	for _, f := range findings {
		if !POAMIncludes(f.Status, filter) {
			continue
		}
		obs := findingObservation(f)
		risk := findingRisk(f, in.Today)
		observations = append(observations, obs)
		risks = append(risks, risk)
		items = append(items, POAMItem{
			UUID:                UUID("poam-item", f.Reference),
			Title:               firstNonEmpty(oneLine(f.Title), f.Reference),
			Description:         findingDescription(f),
			Props:               propsOrNil(findingProps(f, in.Today)),
			RelatedObservations: &[]RelatedObservation{{ObservationUuid: obs.UUID}},
			RelatedRisks:        &[]AssociatedRisk{{RiskUuid: risk.UUID}},
			Remarks:             findingRemarks(f),
		})
	}
	if len(items) == 0 {
		return nil, ErrNoFindings
	}

	scope := "findings that are Open, In Remediation, Pending Validation, Reopened or Risk Accepted"
	if filter != "" {
		scope = "findings with the status " + filter
	}
	doc := struct {
		POAM *POAM `json:"plan-of-action-and-milestones"`
	}{POAM: &POAM{
		Metadata: NewMetadata(MetadataInput{
			Title:        in.Title,
			LastModified: in.LastModified,
			Remarks: "Exported from grc's Audit Findings register: " + scope + ". Drafts are never exported. " +
				"The system-id names the register, not a system: grc holds no system security plan.",
			Props: []NamedValue{{Name: "status-filter", Value: filter}},
		}),
		SystemId:     &SystemID{IdentifierType: Namespace, ID: POAMSystemID},
		PoamItems:    items,
		Observations: &observations,
		Risks:        &risks,
	}}
	return finishDocument("plan-of-action-and-milestones", "audit-findings\x00"+filter, &doc, &doc.POAM.UUID, &doc.POAM.Metadata, in.LastModified)
}

func findingProps(f ExportAuditFinding, today string) []Property {
	props := GRCProps(
		NamedValue{Name: "reference", Value: f.Reference},
		NamedValue{Name: "status", Value: f.Status},
		NamedValue{Name: "severity", Value: f.Severity},
		NamedValue{Name: "finding-type", Value: f.FindingType},
		NamedValue{Name: "deficiency-type", Value: f.DeficiencyType},
		NamedValue{Name: "source", Value: f.Source},
		NamedValue{Name: "engagement", Value: f.Engagement},
		NamedValue{Name: "auditor", Value: f.Auditor},
		NamedValue{Name: "root-cause-category", Value: f.RootCauseCategory},
		NamedValue{Name: "business-unit", Value: f.BusinessUnit},
		NamedValue{Name: "process-area", Value: f.ProcessArea},
		NamedValue{Name: "owner", Value: f.Owner},
		NamedValue{Name: "management-response", Value: f.ManagementResponse},
		NamedValue{Name: "identified-date", Value: f.IdentifiedDate},
		NamedValue{Name: "report-date", Value: f.ReportDate},
		NamedValue{Name: "original-due-date", Value: f.OriginalDueDate},
		NamedValue{Name: "due-date", Value: f.DueDate},
		NamedValue{Name: "extension-count", Value: count(f.ExtensionCount)},
		NamedValue{Name: "extension-reason", Value: f.ExtensionReason},
		NamedValue{Name: "repeat-finding", Value: yesOrEmpty(f.RepeatFinding)},
		NamedValue{Name: "prior-reference", Value: f.PriorReference},
		NamedValue{Name: "origin-ref", Value: f.OriginRef},
		NamedValue{Name: "framework-refs", Value: f.FrameworkRefs},
	)
	if acceptanceExpired(f, today) {
		appendProps(&props, grcProp("acceptance-expired", "yes")...)
	}
	appendProps(&props, propsList("control", listOf(f.LinkedControls))...)
	appendProps(&props, propsList("linked-risk", listOf(f.LinkedRisks))...)
	return props
}

// findingRemarks holds the acceptance rationale and the closure record. A
// risk has no remarks in OSCAL 1.2.1, so they sit on the poam-item.
func findingRemarks(f ExportAuditFinding) string {
	var remarks []string
	for _, r := range []struct{ label, text string }{
		{"Risk acceptance rationale", f.RiskAcceptanceRationale},
		{"Validation notes", f.ValidationNotes},
		{"Closure evidence", f.ClosureEvidence},
	} {
		if t := strings.TrimSpace(r.text); t != "" {
			remarks = append(remarks, r.label+": "+t)
		}
	}
	return strings.Join(remarks, "\n\n")
}

func findingCollected(f ExportAuditFinding) time.Time {
	for _, d := range []string{f.IdentifiedDate, f.ReportDate} {
		if t, ok := day(d); ok {
			return t
		}
	}
	return f.CreatedAt.UTC().Truncate(time.Second)
}

// findingObservation is the condition the auditor found. grc does not record
// how it was found, so the method is UNKNOWN, which OSCAL defines for this.
func findingObservation(f ExportAuditFinding) Observation {
	return Observation{
		UUID:        UUID("poam-observation", f.Reference),
		Title:       oneLine(f.Reference + ": condition"),
		Description: firstNonEmpty(f.Condition, "No condition is recorded."),
		Methods:     []string{"UNKNOWN"},
		Types:       &[]string{"finding"},
		Collected:   findingCollected(f),
		Props:       propsOrNil(GRCProps(NamedValue{Name: "source", Value: f.Source}, NamedValue{Name: "reference", Value: f.Reference})),
	}
}

func findingRisk(f ExportAuditFinding, today string) Risk {
	status := POAMRiskStatus[f.Status]
	if acceptanceExpired(f, today) {
		status = "open"
	}
	risk := Risk{
		UUID:                UUID("poam-risk", f.Reference),
		Title:               firstNonEmpty(oneLine(f.Title), f.Reference),
		Description:         firstNonEmpty(f.Condition, "No condition is recorded."),
		Statement:           firstNonEmpty(f.Effect, "No effect is recorded."),
		Status:              status,
		RelatedObservations: &[]RelatedObservation{{ObservationUuid: UUID("poam-observation", f.Reference)}},
		Props: propsOrNil(GRCProps(
			NamedValue{Name: "reference", Value: f.Reference},
			NamedValue{Name: "status", Value: f.Status},
			NamedValue{Name: "accepted-by", Value: f.RiskAcceptedBy},
			NamedValue{Name: "acceptance-expiry", Value: f.RiskAcceptanceExpiry},
			NamedValue{Name: "validated-by", Value: f.ValidatedBy},
			NamedValue{Name: "validation-date", Value: f.ValidationDate},
			NamedValue{Name: "closed-date", Value: f.ClosedDate},
		)),
	}
	if d, ok := day(f.DueDate); ok {
		risk.Deadline = &d
	}

	var remediations []Response
	if rec := strings.TrimSpace(f.Recommendation); rec != "" {
		remediations = append(remediations, Response{
			UUID:        UUID("poam-recommendation", f.Reference),
			Lifecycle:   "recommendation",
			Title:       "Recommendation",
			Description: rec,
		})
	}
	if plan := actionPlan(f); plan != nil {
		remediations = append(remediations, *plan)
	}
	if len(remediations) > 0 {
		risk.Remediations = &remediations
	}
	return risk
}

// actionPlan is management's action plan as a response: planned, or completed
// once every action is Validated or Cancelled and at least one is Validated.
func actionPlan(f ExportAuditFinding) *Response {
	text := strings.TrimSpace(f.ManagementResponseText)
	if text == "" && len(f.Actions) == 0 {
		return nil
	}
	lifecycle := "planned"
	validated, settled := 0, 0
	var tasks []Task
	keys := uniqueKey{}
	for i, a := range f.Actions {
		switch a.Status {
		case "Validated":
			validated++
			settled++
		case "Cancelled":
			settled++
		}
		label := firstNonEmpty(a.Reference, "Action "+strconv.Itoa(i+1))
		task := Task{
			UUID:        UUID("poam-action", keys.of(f.Reference+"\x00"+label)),
			Type:        "action",
			Title:       oneLine(label),
			Description: strings.TrimSpace(a.Description),
			Props: propsOrNil(GRCProps(
				NamedValue{Name: "action-type", Value: a.ActionType},
				NamedValue{Name: "owner", Value: a.Owner},
				NamedValue{Name: "status", Value: a.Status},
				NamedValue{Name: "completed-date", Value: a.CompletedDate},
				NamedValue{Name: "evidence", Value: a.Evidence},
			)),
		}
		if d, ok := day(a.DueDate); ok {
			task.Timing = &EventTiming{OnDate: &OnDate{Date: d}}
		}
		tasks = append(tasks, task)
	}
	if len(f.Actions) > 0 && settled == len(f.Actions) && validated > 0 {
		lifecycle = "completed"
	}
	if text == "" {
		text = "The management action plan has " + plural(len(f.Actions), "action", "actions") + "."
	}
	r := &Response{
		UUID:        UUID("poam-action-plan", f.Reference),
		Lifecycle:   lifecycle,
		Title:       "Management action plan",
		Description: text,
		Props:       propsOrNil(GRCProps(NamedValue{Name: "management-response", Value: f.ManagementResponse})),
	}
	if len(tasks) > 0 {
		r.Tasks = &tasks
	}
	return r
}
