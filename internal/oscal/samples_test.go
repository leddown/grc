package oscal

import (
	"testing"
	"time"

	"grc/internal/controlid"
)

// Exported from a test file, so the schema check in package oscal_test can
// build the same documents the tests here reason about.

var SampleTime = time.Date(2026, 11, 9, 10, 30, 0, 0, time.UTC)

func SampleNFRs() []ExportNFR {
	return []ExportNFR{
		{Key: "10", ID: "2.1", Summary: "2.1 Logs must be kept", IssueType: "Control", NISTMapping: "AU-2, AU-11, ZZ-9", Implementation: "Ship logs to the SIEM.", Domain: "Logging", Weight: 4},
		{Key: "2", ID: "1.1", Summary: "1.1 Applications must be free of known vulnerabilities", IssueType: "Control", Description: "Scan and patch.", NISTMapping: "SI-2, RA-5", AdditionalDetails: "SAST and SCA in the pipeline.", Domain: "Application Security", Weight: 5},
		{Key: "3", ID: "", Summary: "Unsorted requirement", Weight: 3},
	}
}

func SampleNFRLinks() []ExportNFRLink {
	return []ExportNFRLink{
		{NFRKey: "2", WrittenAs: "SI-2", ControlID: "SI-2"},
		{NFRKey: "2", WrittenAs: "RA-5", ControlID: "RA-5"},
		{NFRKey: "10", WrittenAs: "AU-2", ControlID: "AU-2"},
		{NFRKey: "10", WrittenAs: "AU-11", ControlID: "AU-12", Override: true},
		{NFRKey: "10", WrittenAs: "ZZ-9"},
		{NFRKey: "10", WrittenAs: "AU-2", ControlID: "AU-2"},
	}
}

func SampleNFRMapping() NFRMappingExport {
	return NFRMappingExport{
		Title:        "grc: Security NFRs to NIST SP 800-53",
		LastModified: SampleTime,
		NFRs:         SampleNFRs(),
		Links:        SampleNFRLinks(),
		ControlIDs:   []string{"SI-2", "RA-5", "AU-2", "AU-12", "AC-1"},
		SourceHref:   "/security-nfrs/oscal/catalog.json",
		SourceTitle:  "grc: Security NFR catalog",
		TargetHref:   "/controls/oscal/catalog.json",
		TargetTitle:  "grc: NIST SP 800-53 control catalog",
	}
}

func SampleRegulation(latest bool) ExportRegulation {
	return ExportRegulation{
		ID: 7, Title: "Digital Operational Resilience Act", Framework: "dora", FrameworkName: "DORA",
		SourceRef: "EU 2022/2554", TextSHA256: "3f1c0e5a", Version: 2, Latest: latest,
		VersionNote: "Revised from reviewer feedback on Article 6.", LastModified: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC),
		Sections: []ExportSection{
			{Ref: "DORA-ART-1", Label: "Article 1", Title: "Subject matter", Body: "This Regulation lays down uniform requirements.", Position: 0, Category: "scope", SegmentConfidence: "high",
				Finding: &ExportFinding{Relevant: false, Grounded: true, Requirement: "None: scope.", Confidence: "high", Model: "wintermute/grc", PromptHash: "abc123"}},
			{Ref: "DORA-ART-6", Label: "Article 6", Title: "ICT risk management framework", Body: "Financial entities shall have a sound ICT risk management framework.", Position: 1, Category: "ict-risk-management", SegmentConfidence: "high",
				Finding: &ExportFinding{Relevant: true, Grounded: true, Requirement: "Keep an ICT risk framework.", Commentary: "Auditors ask for the board approval.", Gaps: "No annual review NFR.", Quote: "a sound ICT risk management framework", Confidence: "medium", Model: "wintermute/grc", PromptHash: "def456", Revision: 1,
					Mappings: []ExportRegMapping{
						{Kind: "nfr", Ref: "2", Rationale: "Vulnerability management is part of ICT risk.", Confidence: "medium", Source: "model", Known: true},
						{Kind: "control", Ref: "RA-3", Rationale: "Risk assessment.", Confidence: "high", Source: "seed", Known: true},
						{Kind: "control", Ref: "ZZ-1", Rationale: "Invented.", Confidence: "low", Source: "model", Known: false},
					}}},
			{Ref: "DORA-ART-6", Label: "Article 6a", Title: "", Body: "A second segment the segmenter also called Article 6.", Position: 2, SegmentConfidence: "low",
				Finding: &ExportFinding{Relevant: true, Requirement: "Unclear.", Confidence: "low"}},
			{Ref: "DORA-ART-9", Label: "Article 9", Title: "Protection and prevention", Body: "Financial entities shall use ICT solutions.", Position: 3},
		},
	}
}

func SampleRegulationHrefs() RegulationHrefs {
	return RegulationHrefs{
		Catalog:             "/regulation-coverage/7/oscal/catalog.json?version=2",
		CatalogTitle:        "Digital Operational Resilience Act: Regulation Coverage, version 2",
		NFRCatalog:          "/security-nfrs/oscal/catalog.json",
		NFRCatalogTitle:     "grc: Security NFR catalog",
		ControlCatalog:      "/controls/oscal/catalog.json",
		ControlCatalogTitle: "grc: NIST SP 800-53 control catalog",
	}
}

// FixtureExportControls is the fixture catalog as grc stores it after an
// OSCAL import, in the export's input shape.
func FixtureExportControls(t testing.TB) []ExportControl {
	t.Helper()
	imp, err := ReadCatalogImport(readFixture(t, fixtureCatalog), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]ExportControl, 0, len(imp.Controls))
	for _, c := range imp.Controls {
		out = append(out, ExportControl{
			ID: c.ID, Name: c.Name, Family: controlid.Family(c.ID),
			Requirements: c.Requirements, Discussion: c.Discussion, Related: c.Related, Withdrawn: c.Withdrawn,
			ParamsJSON: c.ParamsJSON, StatementJSON: c.StatementJSON, AssessmentJSON: c.AssessmentJSON,
		})
	}
	return out
}

func SampleCatalogExport(t testing.TB) CatalogExport {
	return CatalogExport{
		Title:        "grc: NIST SP 800-53 control catalog",
		LastModified: SampleTime,
		Families:     map[string]string{"AC": "Access Control", "SA": "System and Services Acquisition"},
		Controls:     FixtureExportControls(t),
	}
}

func SampleCatalogHrefs() CatalogHrefs {
	return CatalogHrefs{
		Control:      "/controls/oscal/catalog.json",
		ControlTitle: "grc: NIST SP 800-53 control catalog",
		NFR:          "/security-nfrs/oscal/catalog.json",
		NFRTitle:     "grc: Security NFR catalog",
	}
}

// SamplePlan is version 2 of a controls testing plan: four attributes (one
// with an NFR, one naming a control that did not resolve, one naming
// nothing), two strata of which one has no items yet, and the governance
// lists.
func SamplePlan() ExportPlan {
	return ExportPlan{
		Reference: "AP-2027-001", Title: "DORA NFR controls testing 2027", PlanType: "controls-testing",
		Function: "ICT Risk", Client: "Example Bank", PeriodStart: "2027-01-01", PeriodEnd: "2027-12-31",
		Objective: "Test the operating effectiveness of the NFR controls on Tier 1 assets.", DecisionsRequested: "Approve the plan.\nApprove the sampling.",
		CoverageBasis: "attestation", SeedCommitment: "9c1f", Version: 2, ApprovedBy: "bob",
		ApprovedAt: time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC), ChangeReason: "Added NFR-03.",
		SnapshotHref: "/assurance-plans/4/versions/2", SnapshotSHA256: "ab12cd34",
		Parameters: []NamedValue{{Name: "tester-fte", Value: "2"}, {Name: "evidence-sla-days", Value: "10"}},
		Steps: []ExportPlanStep{
			{Ref: "0.1", Phase: "Mobilise", Title: "Confirm mandate", ResponsibleRole: "PM", AccountableRole: "CISO", Start: "2026-11-09", End: "2026-11-13", Deliverable: "Mandate note"},
			{Ref: "1.0", Phase: "Approve", Title: "Committee approval", Milestone: true, Start: "2026-12-01", End: "2026-12-01"},
			{Ref: "Q1.1", Title: "Q1 selection", Start: "2027-03-01", Quarter: 1},
		},
		Quarters: []ExportQuarter{{Quarter: 1, LOD1WindowStart: "2027-01-04", LOD1Deadline: "2027-02-26", SelectionDate: "2027-03-01",
			FieldworkStart: "2027-03-02", FieldworkEnd: "2027-03-26", FindingsAgreedBy: "2027-04-02", ReportDate: "2027-04-09", Committee: "April ICT Risk Committee", RetestDays: 2, OverheadDays: 1.5}},
		Strata: []ExportStratum{
			{Code: "T1", Name: "Tier 1", Definition: "ICT assets supporting a critical or important function", SamplingEnabled: true,
				Confidence: 0.95, TolerableRate: 0.05, FloorPct: 0.25, EffortPerTest: 3, EffortPerReview: 0.25,
				Waves: [4]float64{0.35, 0.35, 0.3, 0}, TopUps: [4]int{1, 1, 1, 1}, PlannedSize: 80},
			{Code: "T2", Name: "Tier 2", Definition: "Other ICT assets", PlannedSize: 240, CycleYears: 3, Takes: []string{"High", "Critical"}},
		},
		Attributes: []ExportAttribute{
			{Ref: "NFR-01", Domain: "Asset inventory", Requirement: "The asset is in the inventory.", Procedure: "Trace the asset.", Method: "Inspection",
				EvidenceRequested: "Inventory extract.", PassCriteria: "Entry exists with an owner.", RegulatoryRefs: "DORA Art. 8(1)", ControlIDs: []string{"CM-8", "RA-2"}},
			{Ref: "NFR-04", Domain: "Access recertification", Requirement: "Accounts are recertified.", Method: "Re-performance",
				PassCriteria: "No leaver is active.", AppliesTo: []string{"T1"}, ControlIDs: []string{"AC-2(1)", "ZZ-9"}, UnresolvedControls: []string{"ZZ-9"}, NFRKey: "2",
				AIGenerated: true, Model: "wintermute/grc", PromptHash: "p1"},
			{Ref: "NFR-05", Domain: "Interviews", Requirement: "The owner knows the process.", Method: "Inquiry", ControlIDs: []string{"AC-2"}},
			{Ref: "NFR-06", Domain: "Walkthrough", Requirement: "Observe the backup run.", Method: "Observation"},
		},
		Population: []ExportItem{
			{Ref: "APP-001", Name: "Core ledger", Stratum: "T1", CIF: "Payments", BusinessLine: "Retail", Hosting: "On-prem", InternetFacing: true, Owner: "Ledger owner", Wave: 1},
			{Ref: "APP-002", Name: "Card switch", Stratum: "T1", Owner: "Cards owner", Wave: 2, LastTestedOn: "2025-06-30", SourceKind: "audit_finding", SourceRef: "AF-2026-001"},
		},
		RACIRoles:   []string{"CISO", "PM", "Internal Audit"},
		RACI:        []ExportRACIRow{{Activity: "Approve the plan", Cells: map[string]string{"CISO": "A/R", "PM": "C", "Internal Audit": "I", "Board": "A"}}},
		Cadence:     []ExportCadence{{Forum: "ICT Risk Committee", Frequency: "Quarterly", Content: "Results and findings", Owner: "CISO"}},
		Escalations: []ExportEscalation{{Trigger: "Evidence late by 10 days", EscalateTo: "CIO", Within: "2 days"}},
		Basis:       []ExportBasisRow{{Requirement: "Test the ICT risk framework", Reference: "DORA Art. 6(6)", How: "Quarterly testing of the NFR controls."}},
	}
}

// SampleQuarterResults is Q1 of SamplePlan: a signed-off test on APP-001
// with a raised Fail, a signed-off test on an item that joined after the
// approval with an unraised Fail and a Fail on an attribute that names
// nothing, a test not yet signed off, and an item with no test.
func SampleQuarterResults() ExportQuarterResults {
	plan := SamplePlan()
	return ExportQuarterResults{
		Reference: plan.Reference, PlanTitle: plan.Title, Quarter: 1, Committee: "April ICT Risk Committee",
		FieldworkStart: "2027-03-02", FieldworkEnd: "2027-03-26", SelectionDate: "2027-03-01",
		SelectedAt: time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC), LastModified: SampleTime,
		Attributes: plan.Attributes,
		Population: append(plan.Population, ExportItem{Ref: "APP-009", Name: "New gateway", Stratum: "T1", Owner: "Gateway owner"}),
		Tests: []ExportTest{
			{Ref: "T27-Q1-002", Type: "Operating effectiveness", Tester: "tess", QAReviewer: "quinn", QADate: "2027-03-28", FieldworkCompletedOn: "2027-03-25",
				EvidenceSummary: "Inventory extract and recertification log.", ItemRef: "APP-009", ItemName: "New gateway", Stratum: "T1", Basis: "Risk-based top-up",
				Results: []ExportResult{
					{AttrRef: "NFR-01", Result: "Pass"},
					{AttrRef: "NFR-04", Result: "Fail", Note: "Two leavers active."},
					{AttrRef: "NFR-05", Result: "N/A"},
					{AttrRef: "NFR-06", Result: "Fail", Note: "The backup did not run."},
				}},
			{Ref: "T27-Q1-001", Type: "Operating effectiveness", Tester: "tess", QAReviewer: "quinn", QADate: "2027-03-27", FieldworkCompletedOn: "2027-03-20",
				ItemRef: "APP-001", ItemName: "Core ledger", Stratum: "T1", Basis: "Statistical",
				Results: []ExportResult{
					{AttrRef: "NFR-01", Result: "Fail", Note: "No owner recorded.", FindingRef: "AF-2027-004", Severity: "High"},
					{AttrRef: "NFR-04", Result: "Pass", Note: "Recertified in January."},
					{AttrRef: "NFR-05", Result: ""},
				}},
			{Ref: "T27-Q1-003", Tester: "tess", ItemRef: "APP-002", ItemName: "Card switch", Stratum: "T1",
				Results: []ExportResult{{AttrRef: "NFR-01", Result: "Fail"}}},
		},
		Unopened: []string{"APP-007"},
	}
}

func SampleAssessmentPlanRef() AssessmentPlanRef {
	return AssessmentPlanRef{Plan: SamplePlan(), Href: "/assurance-plans/4/oscal/assessment-plan.json?version=2", Title: "DORA NFR controls testing 2027: Assessment Plan, version 2"}
}

// SamplePOAM holds one finding per status, an accepted risk past its expiry
// and one in force, and a finding with a completed action plan.
func SamplePOAM() POAMExport {
	created := time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC)
	return POAMExport{
		Title: "grc: Audit Findings, plan of action and milestones", LastModified: SampleTime, Today: "2026-11-09",
		Findings: []ExportAuditFinding{
			{Reference: "AF-2027-001", Title: "Leavers keep access", Status: FindingOpen, Severity: "High", FindingType: "Control Deficiency",
				DeficiencyType: "Operating Effectiveness", Source: "Second Line Review", Engagement: "AP-2027-001 DORA NFR controls testing 2027",
				Criteria: "Access is removed on the day a person leaves.", Condition: "Three leavers were still active.\nOne had admin rights.",
				Cause: "HR feed failed.", Effect: "Former staff could reach the ledger.", Recommendation: "Repair the feed and recertify.",
				Owner: "IAM lead", ManagementResponse: "Agree", ManagementResponseText: "We will repair the feed.",
				IdentifiedDate: "2027-03-20", OriginalDueDate: "2027-06-18", DueDate: "2027-07-18", ExtensionCount: 1, ExtensionReason: "Vendor delay",
				LinkedControls: "AC-2, AC-6", LinkedRisks: "R-12", OriginRef: "AP-2027-001 / T27-Q1-001 / NFR-04", CreatedAt: created,
				Actions: []ExportAction{
					{Reference: "AF-2027-001-A1", Description: "Repair the HR feed.", ActionType: "Corrective", Owner: "IAM lead", DueDate: "2027-05-30", Status: "In Progress"},
					{Reference: "AF-2027-001-A2", Description: "Recertify all accounts.", ActionType: "Preventive", Owner: "IAM lead", DueDate: "2027-07-18", Status: "Not Started"},
				}},
			{Reference: "AF-2027-002", Title: "Draft finding", Status: FindingDraft, CreatedAt: created},
			{Reference: "AF-2027-003", Title: "Backups untested", Status: FindingPendingValidation, Severity: "Medium", Condition: "No restore test in 2026.",
				Effect: "Restores may fail.", DueDate: "2027-02-28", CreatedAt: created,
				Actions: []ExportAction{{Reference: "A1", Description: "Run a restore test.", DueDate: "2027-02-28", Status: "Validated", CompletedDate: "2027-02-20"}, {Reference: "A2", Status: "Cancelled"}}},
			{Reference: "AF-2026-010", Title: "Legacy TLS", Status: FindingRiskAccepted, Severity: "Low", RiskAcceptedBy: "CIO",
				RiskAcceptanceRationale: "Decommissioned in Q2.", RiskAcceptanceExpiry: "2026-10-31", CreatedAt: created},
			{Reference: "AF-2026-011", Title: "Old cipher", Status: FindingRiskAccepted, Severity: "Low", RiskAcceptedBy: "CIO", RiskAcceptanceExpiry: "2027-03-31", CreatedAt: created},
			{Reference: "AF-2026-005", Title: "Closed finding", Status: FindingClosed, ValidatedBy: "auditor", ValidationDate: "2026-09-01",
				ClosureEvidence: "Retest passed.", ClosedDate: "2026-09-01", CreatedAt: created},
			{Reference: "AF-2026-006", Title: "Back again", Status: FindingReopened, CreatedAt: created},
			{Reference: "AF-2026-007", Title: "Being fixed", Status: FindingInRemediation, IdentifiedDate: "2026-08-01", CreatedAt: created},
		},
	}
}
