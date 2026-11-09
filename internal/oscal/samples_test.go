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
