package reporting

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"time"
)

// sampleLogoDataURI is a tiny self-contained SVG logo encoded as a data URI, so
// the example needs no external asset and the renderer makes no network call.
func sampleLogoDataURI() template.URL {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="220" height="56" viewBox="0 0 220 56">` +
		`<rect width="56" height="56" rx="10" fill="#1f2a44"/>` +
		`<path d="M18 30 l8 8 l16 -18" fill="none" stroke="#8b3d2e" stroke-width="5" stroke-linecap="round" stroke-linejoin="round"/>` +
		`<text x="68" y="36" font-family="Helvetica,Arial,sans-serif" font-size="22" font-weight="700" fill="#1f2a44">CareLock</text>` +
		`</svg>`
	encoded := base64.StdEncoding.EncodeToString([]byte(svg))
	// #nosec G203 -- the SVG is a constant defined above; the data URI is fully server-generated, not user input.
	return template.URL("data:image/svg+xml;base64," + encoded)
}

// SampleReportOptions returns branded options that exercise the cover banner,
// CONFIDENTIAL classification, and DRAFT watermark.
func SampleReportOptions() ReportOptions {
	generated := time.Date(2026, time.June, 25, 9, 30, 0, 0, time.UTC)
	branding := DefaultBranding()
	branding.LogoDataURI = sampleLogoDataURI()
	return ReportOptions{
		Branding:       branding,
		Classification: ClassificationConfidential,
		Watermark:      "DRAFT",
		GeneratedAt:    generated,
		Cover: Cover{
			Title:    "Quarterly Risk Assessment Report",
			Subtitle: "Information Security & Operational Risk — Q2 2026",
			Author:   "Risk & Compliance Team",
			Owner:    "Chief Information Security Officer",
			Version:  "1.0",
		},
		Page: DefaultPageSetup(),
	}
}

// SampleRiskAssessment returns a deterministic Risk Assessment Report large
// enough to span multiple pages and exercise table pagination.
func SampleRiskAssessment() RiskAssessmentReport {
	return RiskAssessmentReport{
		Scope:           "Enterprise information systems, cloud workloads, and third-party integrations in scope for the FY26 RCSA cycle.",
		Period:          "01 April 2026 – 30 June 2026",
		Methodology:     "Risks were scored on a 5×5 likelihood/impact matrix per the NIST SP 800-30 methodology. Residual scores reflect the effect of existing controls assessed in the control matrix below. Findings are rated by severity and tracked to remediation owners.",
		ExecutiveReview: "Overall residual risk is within appetite, with two critical items requiring executive attention. Identity and third-party risk domains show the highest concentration of open findings.",
		Risks:           sampleRisks(),
		Controls:        sampleControls(),
		Findings:        sampleFindings(),
	}
}

func sampleRisks() []RiskRow {
	type seed struct {
		title, category, owner, treatment, status string
		likelihood, impact                        int
	}
	seeds := []seed{
		{"Unpatched internet-facing VPN appliance", "Vulnerability Mgmt", "Infra Security", "Mitigate", "Open", 5, 5},
		{"Privileged account without MFA", "Identity & Access", "IAM Team", "Mitigate", "Open", 4, 5},
		{"Third-party data processor breach exposure", "Third-Party Risk", "Vendor Risk", "Transfer", "Monitoring", 3, 5},
		{"Backup restoration not regularly tested", "Resilience", "IT Operations", "Mitigate", "In Progress", 3, 4},
		{"Excessive S3 bucket permissions", "Cloud Security", "Cloud Platform", "Mitigate", "Open", 4, 4},
		{"Phishing susceptibility in finance dept", "Human Risk", "Security Awareness", "Mitigate", "In Progress", 4, 3},
		{"End-of-life database engine in production", "Vulnerability Mgmt", "DBA Team", "Mitigate", "Open", 3, 4},
		{"Missing DLP on outbound email", "Data Protection", "Security Eng", "Mitigate", "Planned", 3, 3},
		{"Shadow IT SaaS adoption", "Governance", "IT Governance", "Mitigate", "Monitoring", 3, 2},
		{"Inadequate logging on payment service", "Detection", "SOC", "Mitigate", "In Progress", 2, 4},
		{"Weak TLS configuration on legacy API", "Network Security", "Platform Eng", "Mitigate", "Open", 3, 3},
		{"No formal incident response runbook", "Resilience", "SOC", "Mitigate", "Planned", 2, 4},
		{"Orphaned service accounts", "Identity & Access", "IAM Team", "Mitigate", "Open", 3, 3},
		{"Unencrypted PII in analytics store", "Data Protection", "Data Eng", "Mitigate", "Open", 2, 5},
		{"Single region cloud deployment", "Resilience", "Cloud Platform", "Accept", "Accepted", 2, 3},
		{"Outdated cryptographic library", "Vulnerability Mgmt", "AppSec", "Mitigate", "In Progress", 3, 3},
		{"Insufficient vendor SLA penalties", "Third-Party Risk", "Procurement", "Transfer", "Monitoring", 2, 2},
		{"No periodic access recertification", "Identity & Access", "IAM Team", "Mitigate", "Planned", 3, 3},
		{"Public code repo with stale secrets", "AppSec", "DevSecOps", "Mitigate", "Open", 4, 4},
		{"Lack of network segmentation in OT", "Network Security", "OT Security", "Mitigate", "In Progress", 3, 4},
		{"Manual change management process", "Governance", "Change Mgmt", "Accept", "Accepted", 2, 2},
		{"Unmonitored privileged API tokens", "Identity & Access", "Platform Eng", "Mitigate", "Open", 3, 4},
		{"No tabletop exercise in 12 months", "Resilience", "BCM", "Mitigate", "Planned", 2, 3},
		{"Vendor offboarding gaps", "Third-Party Risk", "Vendor Risk", "Avoid", "In Progress", 2, 3},
	}

	risks := make([]RiskRow, 0, len(seeds))
	for i, s := range seeds {
		score := s.likelihood * s.impact
		risks = append(risks, RiskRow{
			ID:         fmt.Sprintf("RISK-%03d", i+1),
			Title:      s.title,
			Category:   s.category,
			Owner:      s.owner,
			Likelihood: s.likelihood,
			Impact:     s.impact,
			Severity:   SeverityForScore(score),
			Treatment:  s.treatment,
			Status:     s.status,
		})
	}
	return risks
}

// SampleControlAssessmentOptions returns branded options for the control
// assessment example (INTERNAL classification, no watermark).
func SampleControlAssessmentOptions() ReportOptions {
	opts := SampleReportOptions()
	opts.Classification = ClassificationInternal
	opts.Watermark = ""
	opts.Cover = Cover{
		Title:    "Security Control Assessment Report",
		Subtitle: "NIST SP 800-53 Rev 5 — Moderate Baseline",
		Author:   "Security Assessment Team",
		Owner:    "Information System Security Officer",
		Version:  "1.0",
	}
	return opts
}

// SampleControlAssessment returns a deterministic Control Assessment Report
// large enough to span multiple pages and exercise table pagination.
func SampleControlAssessment() ControlAssessmentReport {
	return ControlAssessmentReport{
		Framework:      "NIST SP 800-53 Rev 5 (Moderate Baseline)",
		System:         "Client Engagement Platform (Production)",
		Scope:          "All security controls in the moderate baseline applicable to the production environment and its supporting cloud infrastructure.",
		AssessmentDate: "12 June 2026",
		Assessor:       "Care Lock Consulting",
		Narrative:      "Controls were assessed using the examine, interview, and test methods defined in NIST SP 800-53A. The system meets the majority of moderate-baseline objectives; identified deficiencies are concentrated in identity management and audit review and are tracked below with recommended remediation.",
		Results:        sampleControlResults(),
		Deficiencies:   sampleDeficiencies(),
	}
}

func sampleControlResults() []ControlResult {
	type seed struct {
		id, name, family, impl, method string
		result                         AssessmentResult
	}
	seeds := []seed{
		{"AC-2", "Account Management", "Access Control", "Implemented", "Test", ResultPartiallySatisfied},
		{"AC-3", "Access Enforcement", "Access Control", "Implemented", "Test", ResultSatisfied},
		{"AC-6", "Least Privilege", "Access Control", "Implemented", "Examine", ResultPartiallySatisfied},
		{"AC-17", "Remote Access", "Access Control", "Implemented", "Test", ResultSatisfied},
		{"AU-2", "Event Logging", "Audit & Accountability", "Implemented", "Examine", ResultSatisfied},
		{"AU-6", "Audit Record Review", "Audit & Accountability", "Planned", "Interview", ResultNotSatisfied},
		{"AU-9", "Protection of Audit Information", "Audit & Accountability", "Implemented", "Test", ResultSatisfied},
		{"CA-7", "Continuous Monitoring", "Assessment & Authorization", "Implemented", "Interview", ResultPartiallySatisfied},
		{"CM-2", "Baseline Configuration", "Configuration Management", "Implemented", "Examine", ResultSatisfied},
		{"CM-6", "Configuration Settings", "Configuration Management", "Implemented", "Test", ResultSatisfied},
		{"CM-8", "System Component Inventory", "Configuration Management", "Implemented", "Examine", ResultPartiallySatisfied},
		{"CP-9", "System Backup", "Contingency Planning", "Implemented", "Test", ResultPartiallySatisfied},
		{"CP-10", "System Recovery", "Contingency Planning", "Planned", "Interview", ResultNotSatisfied},
		{"IA-2", "Identification & Authentication", "Identification & Authentication", "Implemented", "Test", ResultNotSatisfied},
		{"IA-5", "Authenticator Management", "Identification & Authentication", "Implemented", "Examine", ResultPartiallySatisfied},
		{"IR-4", "Incident Handling", "Incident Response", "Implemented", "Interview", ResultSatisfied},
		{"IR-8", "Incident Response Plan", "Incident Response", "Implemented", "Examine", ResultSatisfied},
		{"RA-5", "Vulnerability Monitoring", "Risk Assessment", "Implemented", "Test", ResultSatisfied},
		{"SC-7", "Boundary Protection", "System & Communications", "Implemented", "Test", ResultSatisfied},
		{"SC-8", "Transmission Confidentiality", "System & Communications", "Implemented", "Test", ResultPartiallySatisfied},
		{"SC-13", "Cryptographic Protection", "System & Communications", "Implemented", "Examine", ResultSatisfied},
		{"SC-28", "Protection of Data at Rest", "System & Communications", "Planned", "Interview", ResultNotSatisfied},
		{"SI-2", "Flaw Remediation", "System & Information Integrity", "Implemented", "Test", ResultPartiallySatisfied},
		{"SI-4", "System Monitoring", "System & Information Integrity", "Implemented", "Test", ResultSatisfied},
		{"PE-3", "Physical Access Control", "Physical & Environmental", "Inherited", "Examine", ResultNotApplicable},
	}

	results := make([]ControlResult, 0, len(seeds))
	for _, s := range seeds {
		results = append(results, ControlResult{
			ControlID:            s.id,
			Name:                 s.name,
			Family:               s.family,
			ImplementationStatus: s.impl,
			Method:               s.method,
			Result:               s.result,
		})
	}
	return results
}

func sampleDeficiencies() []Deficiency {
	return []Deficiency{
		{
			ControlID:   "IA-2",
			Weakness:    "Multi-factor authentication is not enforced for all privileged accounts",
			Severity:    SeverityCritical,
			Remediation: "Enforce phishing-resistant MFA for every privileged account and disable password-only authentication paths.",
			Owner:       "IAM Team",
			Milestone:   "31 July 2026",
		},
		{
			ControlID:   "AU-6",
			Weakness:    "Audit records are collected but not reviewed on a defined cadence",
			Severity:    SeverityHigh,
			Remediation: "Implement a documented daily audit-review procedure with alerting on security-relevant events.",
			Owner:       "Security Operations",
			Milestone:   "31 August 2026",
		},
		{
			ControlID:   "SC-28",
			Weakness:    "Sensitive data is not encrypted at rest in the analytics store",
			Severity:    SeverityHigh,
			Remediation: "Enable storage-level encryption with managed keys and validate via configuration testing.",
			Owner:       "Data Engineering",
			Milestone:   "30 September 2026",
		},
		{
			ControlID:   "CP-10",
			Weakness:    "System recovery procedures have not been tested against the stated RTO",
			Severity:    SeverityMedium,
			Remediation: "Conduct and document a recovery test for tier-1 systems each quarter.",
			Owner:       "IT Operations",
			Milestone:   "15 September 2026",
		},
	}
}

func sampleControls() []ControlRow {
	return []ControlRow{
		{"AC-2", "Account Management", "Access Control", "IAM Team", "Implemented", "Effective"},
		{"AC-17", "Remote Access", "Access Control", "Infra Security", "Partial", "Needs Improvement"},
		{"AU-6", "Audit Review & Reporting", "Audit & Accountability", "SOC", "Partial", "Needs Improvement"},
		{"CM-2", "Baseline Configuration", "Configuration Mgmt", "Platform Eng", "Implemented", "Effective"},
		{"CP-9", "System Backup", "Contingency Planning", "IT Operations", "Partial", "Needs Improvement"},
		{"IA-2", "Identification & Authentication", "Identification & Auth", "IAM Team", "Partial", "Ineffective"},
		{"IR-8", "Incident Response Plan", "Incident Response", "SOC", "Planned", "Ineffective"},
		{"RA-5", "Vulnerability Monitoring", "Risk Assessment", "Infra Security", "Implemented", "Effective"},
		{"SC-8", "Transmission Confidentiality", "System & Comms", "Platform Eng", "Partial", "Needs Improvement"},
		{"SC-28", "Protection of Data at Rest", "System & Comms", "Data Eng", "Partial", "Needs Improvement"},
		{"SI-2", "Flaw Remediation", "System & Info Integrity", "Vuln Mgmt", "Partial", "Needs Improvement"},
		{"SI-4", "System Monitoring", "System & Info Integrity", "SOC", "Implemented", "Effective"},
	}
}

func sampleFindings() []Finding {
	return []Finding{
		{
			ID:             "FND-001",
			Title:          "Privileged access lacks enforced multi-factor authentication",
			Severity:       SeverityCritical,
			Description:    "Several administrative accounts on core infrastructure can authenticate with a password alone. This materially increases the likelihood of account takeover following credential compromise.",
			Recommendation: "Enforce phishing-resistant MFA for all privileged accounts and disable legacy password-only authentication paths within 30 days.",
			Owner:          "IAM Team",
			DueDate:        "31 July 2026",
		},
		{
			ID:             "FND-002",
			Title:          "Internet-facing VPN appliance missing critical patches",
			Severity:       SeverityCritical,
			Description:    "The remote-access VPN appliance is running firmware with known, actively exploited vulnerabilities. Exploitation could grant unauthenticated network access.",
			Recommendation: "Apply vendor security updates immediately and add the appliance to the emergency patch SLA tier.",
			Owner:          "Infra Security",
			DueDate:        "04 July 2026",
		},
		{
			ID:             "FND-003",
			Title:          "Backup restoration is not routinely tested",
			Severity:       SeverityHigh,
			Description:    "While backups are taken, there is no documented evidence of periodic restoration testing, leaving recovery capability unverified against the stated RTO/RPO.",
			Recommendation: "Establish a quarterly restoration test for tier-1 systems with documented results reviewed by IT Operations management.",
			Owner:          "IT Operations",
			DueDate:        "30 September 2026",
		},
		{
			ID:             "FND-004",
			Title:          "Sensitive data stored without encryption at rest",
			Severity:       SeverityHigh,
			Description:    "Personally identifiable information is persisted in an analytics store without encryption at rest, increasing impact in the event of unauthorized data store access.",
			Recommendation: "Enable storage-level encryption and rotate keys via the managed KMS; classify and minimize PII retained in analytics.",
			Owner:          "Data Engineering",
			DueDate:        "31 August 2026",
		},
		{
			ID:             "FND-005",
			Title:          "Secrets committed to a public code repository",
			Severity:       SeverityMedium,
			Description:    "Historical commits in a public repository contain credentials. Although rotated, the exposure highlights gaps in pre-commit secret scanning.",
			Recommendation: "Adopt automated secret scanning in CI and pre-commit hooks, and complete a repository history purge for the affected project.",
			Owner:          "DevSecOps",
			DueDate:        "15 August 2026",
		},
	}
}
