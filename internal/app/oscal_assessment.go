package app

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/assuranceplan"
	"grc/internal/auditfinding"
	"grc/internal/oscal"
)

// The assessment exports: an Assurance Plan's approval snapshot as an OSCAL
// Assessment Plan, a quarter's signed-off tests as Assessment Results, and the
// Audit Findings register as a POA&M. Like the catalog exports, they read
// through the modules' services and hand internal/oscal neutral input, and
// they are open to any signed-in user under the modules' own prefixes.

const oscalPOAMTitle = "grc: Audit Findings, plan of action and milestones"

var oscalCatalogs = oscal.CatalogHrefs{
	Control:      oscalCatalogPath,
	ControlTitle: oscalCatalogTitle,
	NFR:          oscalNFRCatalogPath,
	NFRTitle:     oscalNFRCatalogTitle,
}

func oscalAssessmentPlanPath(id int64, version int) string {
	return fmt.Sprintf("/assurance-plans/%d/oscal/assessment-plan.json?version=%d", id, version)
}

type oscalAssessments struct {
	plans    *assuranceplan.Service
	findings *auditfinding.Service
	now      func() time.Time
}

func registerOSCALAssessmentRoutes(r gin.IRouter, plans *assuranceplan.Service, findings *auditfinding.Service) {
	e := &oscalAssessments{plans: plans, findings: findings, now: time.Now}
	r.GET("/assurance-plans/:id/oscal/assessment-plan.json", e.assessmentPlan)
	r.GET("/assurance-plans/:id/oscal/assessment-results/:file", e.assessmentResults)
	r.GET("/audit-findings/oscal/poam.json", e.poam)
}

func oscalPlanID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plan id"})
		return 0, false
	}
	return id, true
}

// oscalPlanError answers a plan that cannot be read: 404 for an unknown plan
// or version, 409 for a plan in a state that has nothing to export.
func oscalPlanError(c *gin.Context, what string, err error) {
	var conflict *assuranceplan.ConflictError
	switch {
	case errors.Is(err, assuranceplan.ErrNotFound):
		// An unknown version wraps the sentinel with the reason, which is the
		// part worth showing.
		c.JSON(http.StatusNotFound, gin.H{"error": strings.TrimPrefix(err.Error(), assuranceplan.ErrNotFound.Error()+": ")})
	case errors.As(err, &conflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		oscalFailed(c, what, err)
	}
}

var oscalFileUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func oscalFileRef(ref string) string {
	return strings.Trim(oscalFileUnsafe.ReplaceAllString(strings.ToLower(ref), "-"), "-")
}

const oscalNoReferences = "no attribute of its test script names a control or NFR that resolved when it was approved, and OSCAL requires an " +
	"Assessment Plan to list the controls it reviews; give the attributes their references through a change request"

// assessmentPlan serves GET /assurance-plans/:id/oscal/assessment-plan.json
// (?version=N, the latest approval without it), built from the approval
// snapshot and dated by its approval.
func (e *oscalAssessments) assessmentPlan(c *gin.Context) {
	id, ok := oscalPlanID(c)
	if !ok {
		return
	}
	version := 0
	if v := strings.TrimSpace(c.Query("version")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version number"})
			return
		}
		version = n
	}
	ap, err := e.plans.ApprovedPlan(id, version)
	if err != nil {
		oscalPlanError(c, "the Assessment Plan", err)
		return
	}
	in := oscalExportPlan(id, ap)
	body, err := oscal.ExportAssessmentPlan(in, oscalCatalogs)
	if errors.Is(err, oscal.ErrNoReviewedControls) {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("version %d of %s cannot be exported as an Assessment Plan: %s", in.Version, in.Reference, oscalNoReferences)})
		return
	}
	if err != nil {
		oscalFailed(c, "the Assessment Plan", err)
		return
	}
	writeOSCAL(c, fmt.Sprintf("assessment-plan-%s-v%d", oscalFileRef(in.Reference), in.Version), in.ApprovedAt, body)
}

var oscalQuarterFile = regexp.MustCompile(`^q([1-4])\.json$`)

// assessmentResults serves GET /assurance-plans/:id/oscal/assessment-results/q<N>.json:
// the quarter's tests QA has signed off, against the Assessment Plan of the
// version the quarter was selected under. A signed-off test can be reopened,
// so the document is dated by the export.
func (e *oscalAssessments) assessmentResults(c *gin.Context) {
	id, ok := oscalPlanID(c)
	if !ok {
		return
	}
	m := oscalQuarterFile.FindStringSubmatch(c.Param("file"))
	if m == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such quarter: use q1.json, q2.json, q3.json or q4.json"})
		return
	}
	quarter, _ := strconv.Atoi(m[1])
	plan, err := e.plans.Get(id)
	if err != nil {
		oscalPlanError(c, "the Assessment Results", err)
		return
	}
	var q *assuranceplan.Quarter
	for i := range plan.Quarters {
		if plan.Quarters[i].Quarter == quarter {
			q = &plan.Quarters[i]
		}
	}
	if q == nil || strings.TrimSpace(q.SelectedAt) == "" {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Q%d of %s has not been selected: it has no tests to report", quarter, plan.Reference)})
		return
	}
	selections, err := e.plans.Selections(id)
	if err != nil {
		oscalFailed(c, "the Assessment Results", err)
		return
	}
	version := 0
	var quarterSelections []assuranceplan.Selection
	for _, s := range selections {
		if s.Quarter != quarter {
			continue
		}
		quarterSelections = append(quarterSelections, s)
		version = max(version, s.PlanVersion)
	}
	ap, err := e.plans.ApprovedPlan(id, version)
	if err != nil {
		oscalPlanError(c, "the Assessment Results", err)
		return
	}
	tests, err := e.plans.Tests(id, quarter)
	if err != nil {
		oscalFailed(c, "the Assessment Results", err)
		return
	}
	population, err := e.plans.Population(id)
	if err != nil {
		oscalFailed(c, "the Assessment Results", err)
		return
	}
	opened := map[int64]bool{}
	for _, t := range tests {
		opened[t.SelectionID] = true
	}
	var unopened []string
	for _, s := range quarterSelections {
		if !opened[s.ID] {
			unopened = append(unopened, s.ItemRef)
		}
	}
	selectedAt, _ := time.Parse(time.RFC3339, q.SelectedAt)
	now := e.now()
	apIn := oscalExportPlan(id, ap)
	results := oscal.ExportQuarterResults{
		Reference:      plan.Reference,
		PlanTitle:      plan.Title,
		Quarter:        quarter,
		Committee:      q.CommitteeLabel,
		FieldworkStart: q.FieldworkStart,
		FieldworkEnd:   q.FieldworkEnd,
		SelectionDate:  q.SelectionDate,
		SelectedAt:     selectedAt,
		LastModified:   now,
		Attributes:     oscalAttributes(plan.Attributes),
		Population:     oscalItems(population),
		Unopened:       unopened,
	}
	for _, t := range tests {
		et := oscal.ExportTest{
			Ref: t.TestRef, Type: t.TestType, Tester: t.Tester, QAReviewer: t.QAReviewer, QADate: t.QADate,
			FieldworkCompletedOn: t.FieldworkCompletedOn, EvidenceSummary: t.EvidenceSummary,
			ItemRef: t.ItemRef, ItemName: t.ItemName, Stratum: t.StratumCode, Basis: t.Basis,
		}
		for _, r := range t.Results {
			et.Results = append(et.Results, oscal.ExportResult{AttrRef: r.AttrRef, Result: r.Result, Note: r.Note, FindingRef: r.FindingRef, Severity: r.Severity})
		}
		results.Tests = append(results.Tests, et)
	}
	body, err := oscal.ExportAssessmentResults(results, oscal.AssessmentPlanRef{
		Plan:  apIn,
		Href:  oscalAssessmentPlanPath(id, apIn.Version),
		Title: fmt.Sprintf("%s: Assessment Plan, version %d", strings.TrimSpace(apIn.Title), apIn.Version),
	}, oscalCatalogs)
	switch {
	case errors.Is(err, oscal.ErrNoReviewedControls):
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Q%d was selected under version %d of %s, which cannot be exported as an Assessment Plan: %s",
			quarter, apIn.Version, plan.Reference, oscalNoReferences)})
		return
	case errors.Is(err, oscal.ErrNoReviewedResults):
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Q%d of %s has no Pass or Fail signed off by QA on an attribute that names a control or NFR, so its results review nothing OSCAL can list", quarter, plan.Reference)})
		return
	case errors.Is(err, oscal.ErrNoSignedOffTests):
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no test of Q%d of %s is signed off by QA yet: there are no results to export", quarter, plan.Reference)})
		return
	case err != nil:
		oscalFailed(c, "the Assessment Results", err)
		return
	}
	writeOSCAL(c, fmt.Sprintf("assessment-results-%s-q%d", oscalFileRef(plan.Reference), quarter), now, body)
}

// poam serves GET /audit-findings/oscal/poam.json (?status=, one status
// instead of every issued and unclosed one).
func (e *oscalAssessments) poam(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	if err := oscal.CheckPOAMStatus(status); err != nil {
		msg := err.Error()
		if errors.Is(err, oscal.ErrUnknownFindingStatus) {
			var known []string
			for _, s := range auditfinding.Statuses {
				if s != auditfinding.StatusDraft {
					known = append(known, s)
				}
			}
			msg += "; use one of " + strings.Join(known, ", ")
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	findings, err := e.findings.List(auditfinding.Filter{Status: status})
	if err != nil {
		oscalFailed(c, "the POA&M", err)
		return
	}
	now := e.now()
	in := oscal.POAMExport{Title: oscalPOAMTitle, LastModified: now, Today: now.UTC().Format("2006-01-02"), Status: status}
	for _, f := range findings {
		in.Findings = append(in.Findings, oscalAuditFinding(f))
	}
	body, err := oscal.ExportPOAM(in)
	if errors.Is(err, oscal.ErrNoFindings) {
		scope := "no audit finding is Open, In Remediation, Pending Validation, Reopened or Risk Accepted"
		if status != "" {
			scope = "no audit finding has the status " + status
		}
		c.JSON(http.StatusNotFound, gin.H{"error": scope + ": the POA&M would be empty"})
		return
	}
	if err != nil {
		oscalFailed(c, "the POA&M", err)
		return
	}
	writeOSCAL(c, "poam", now, body)
}

func oscalExportPlan(id int64, ap assuranceplan.ApprovedPlan) oscal.ExportPlan {
	p := ap.Plan
	approved, _ := time.Parse(time.RFC3339, ap.Version.ApprovedAt)
	pm := p.Parameters
	in := oscal.ExportPlan{
		Reference:          p.Reference,
		Title:              p.Title,
		PlanType:           p.PlanType,
		Function:           p.FunctionName,
		Client:             p.ClientName,
		PeriodStart:        p.PeriodStart,
		PeriodEnd:          p.PeriodEnd,
		Objective:          p.Objective,
		DecisionsRequested: p.DecisionsRequested,
		CoverageBasis:      p.CoverageBasis,
		PopulationSource:   p.PopulationSource,
		SeedCommitment:     p.SeedCommitment,
		AIGenerated:        p.AIGenerated,
		Model:              p.Model,
		PromptHash:         p.PromptHash,
		Version:            ap.Version.Number,
		ApprovedBy:         ap.Version.ApprovedBy,
		ApprovedAt:         approved,
		ChangeReason:       ap.Version.ChangeReason,
		SnapshotHref:       fmt.Sprintf("/assurance-plans/%d/versions/%d", id, ap.Version.Number),
		SnapshotSHA256:     ap.Version.SnapshotSHA256,
		Parameters: []oscal.NamedValue{
			{Name: "tester-fte", Value: oscalNum(pm.TesterFTE)},
			{Name: "days-per-fte-quarter", Value: oscalNum(pm.DaysPerFTEQuarter)},
			{Name: "utilisation-threshold", Value: oscalNum(pm.UtilisationThreshold)},
			{Name: "rag-amber-points", Value: oscalNum(pm.RAGAmberPoints)},
			{Name: "result-partial-min", Value: strconv.Itoa(pm.ResultPartialMin)},
			{Name: "result-ineffective-min", Value: strconv.Itoa(pm.ResultIneffectiveMin)},
			{Name: "deviation-extend-at", Value: strconv.Itoa(pm.DeviationExtendAt)},
			{Name: "deviation-thematic-at", Value: strconv.Itoa(pm.DeviationThematicAt)},
			{Name: "extend-pct", Value: oscalNum(pm.ExtendPct)},
			{Name: "evidence-sla-days", Value: strconv.Itoa(pm.EvidenceSLADays)},
		},
		Attributes: oscalAttributes(p.Attributes),
		Population: oscalItems(ap.Population),
		RACIRoles:  p.RACIRoles,
	}
	for _, s := range p.Steps {
		in.Steps = append(in.Steps, oscal.ExportPlanStep{
			Ref: s.StepRef, Phase: s.Phase, Title: s.Title, Milestone: s.StepType == assuranceplan.StepMilestone,
			ResponsibleRole: s.ResponsibleRole, AccountableRole: s.AccountableRole, Start: s.StartDate, End: s.EndDate,
			Deliverable: s.Deliverable, Refs: s.Refs, Quarter: s.Quarter,
		})
	}
	for _, q := range p.Quarters {
		in.Quarters = append(in.Quarters, oscal.ExportQuarter{
			Quarter: q.Quarter, LOD1WindowStart: q.LOD1WindowStart, LOD1Deadline: q.LOD1Deadline, SelectionDate: q.SelectionDate,
			FieldworkStart: q.FieldworkStart, FieldworkEnd: q.FieldworkEnd, FindingsAgreedBy: q.FindingsAgreedBy,
			ReportDate: q.ReportDate, Committee: q.CommitteeLabel, RetestDays: q.RetestDays, OverheadDays: q.OverheadDays,
		})
	}
	for _, s := range p.Strata {
		in.Strata = append(in.Strata, oscal.ExportStratum{
			Code: s.Code, Name: s.Name, Definition: s.Definition, SamplingEnabled: s.SamplingEnabled,
			Confidence: s.Confidence, TolerableRate: s.TolerableRate, FloorPct: s.FloorPct,
			EffortPerTest: s.EffortPerTest, EffortPerReview: s.EffortPerReview, Waves: s.Waves, TopUps: s.TopUps,
			PlannedSize: s.PlannedSize, CycleYears: s.CycleYears, Takes: s.Takes,
		})
	}
	for _, r := range p.RACI {
		in.RACI = append(in.RACI, oscal.ExportRACIRow{Activity: r.Activity, Cells: r.Cells})
	}
	for _, c := range p.Cadence {
		in.Cadence = append(in.Cadence, oscal.ExportCadence{Forum: c.Forum, Frequency: c.Frequency, Content: c.Content, Owner: c.Owner})
	}
	for _, x := range p.Escalations {
		in.Escalations = append(in.Escalations, oscal.ExportEscalation{Trigger: x.Trigger, EscalateTo: x.EscalateTo, Within: x.Within})
	}
	for _, b := range p.Basis {
		in.Basis = append(in.Basis, oscal.ExportBasisRow{Requirement: b.Requirement, Reference: b.Reference, How: b.How})
	}
	return in
}

func oscalNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// oscalAttributes converts a test script. assuranceplan records an NFR that
// did not resolve as "NFR <key>" among the unresolved references, and a
// control as its id.
func oscalAttributes(attrs []assuranceplan.Attribute) []oscal.ExportAttribute {
	out := make([]oscal.ExportAttribute, 0, len(attrs))
	for _, a := range attrs {
		ea := oscal.ExportAttribute{
			Ref: a.AttrRef, Domain: a.Domain, Requirement: a.Requirement, Procedure: a.Procedure, Method: a.Method,
			EvidenceRequested: a.EvidenceRequested, PassCriteria: a.PassCriteria, AppliesTo: a.AppliesTo,
			RegulatoryRefs: a.RegulatoryRefs, ControlIDs: a.ControlIDs, NFRKey: a.NFRKey,
			AIGenerated: a.AIGenerated, Model: a.Model, PromptHash: a.PromptHash,
		}
		for _, u := range a.Unresolved {
			if strings.HasPrefix(u, "NFR ") {
				ea.NFRUnresolved = ea.NFRUnresolved || strings.TrimSpace(strings.TrimPrefix(u, "NFR ")) == strings.TrimSpace(a.NFRKey)
				continue
			}
			ea.UnresolvedControls = append(ea.UnresolvedControls, u)
		}
		out = append(out, ea)
	}
	return out
}

func oscalItems(items []assuranceplan.PopulationItem) []oscal.ExportItem {
	out := make([]oscal.ExportItem, 0, len(items))
	for _, it := range items {
		out = append(out, oscal.ExportItem{
			Ref: it.ItemRef, Name: it.Name, Stratum: it.StratumCode, CIF: it.CIF, BusinessLine: it.BusinessLine,
			Hosting: it.Hosting, InternetFacing: it.InternetFacing, Owner: it.Owner, Wave: it.WaveQuarter,
			LastTestedOn: it.LastTestedOn, SourceKind: it.SourceKind, SourceRef: it.SourceRef,
		})
	}
	return out
}

func oscalAuditFinding(f auditfinding.Finding) oscal.ExportAuditFinding {
	created, _ := time.Parse(time.RFC3339, f.CreatedAt)
	out := oscal.ExportAuditFinding{
		Reference: f.Reference, Title: f.Title, Source: f.Source, Engagement: f.Engagement, Auditor: f.Auditor,
		FindingType: f.FindingType, Severity: f.Severity, DeficiencyType: f.DeficiencyType,
		Criteria: f.Criteria, Condition: f.Condition, Cause: f.Cause, Effect: f.Effect, Recommendation: f.Recommendation,
		RootCauseCategory: f.RootCauseCategory, BusinessUnit: f.BusinessUnit, ProcessArea: f.ProcessArea, Owner: f.Owner,
		ManagementResponse: f.ManagementResponse, ManagementResponseText: f.ManagementResponseText,
		IdentifiedDate: f.IdentifiedDate, ReportDate: f.ReportDate, OriginalDueDate: f.OriginalDueDate, DueDate: f.DueDate,
		ExtensionCount: f.ExtensionCount, ExtensionReason: f.ExtensionReason, Status: f.Status,
		RepeatFinding: f.RepeatFinding, PriorReference: f.PriorReference,
		LinkedRisks: f.LinkedRisks, LinkedControls: f.LinkedControls, FrameworkRefs: f.FrameworkRefs, OriginRef: f.OriginRef,
		ValidatedBy: f.ValidatedBy, ValidationDate: f.ValidationDate, ValidationNotes: f.ValidationNotes,
		ClosureEvidence: f.ClosureEvidence, ClosedDate: f.ClosedDate,
		RiskAcceptedBy: f.RiskAcceptedBy, RiskAcceptanceRationale: f.RiskAcceptanceRationale, RiskAcceptanceExpiry: f.RiskAcceptanceExpiry,
		CreatedAt: created,
	}
	for _, a := range f.Actions {
		out.Actions = append(out.Actions, oscal.ExportAction{
			Reference: a.Reference, Description: a.Description, ActionType: a.ActionType, Owner: a.Owner,
			DueDate: a.DueDate, Status: a.Status, CompletedDate: a.CompletedDate, Evidence: a.Evidence,
		})
	}
	return out
}
