package policydocs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Finding severities. Errors block approval; warnings do not.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Finding is one lint result against a document.
type Finding struct {
	Severity  string `json:"severity"`
	SectionID int64  `json:"section_id"` // 0 = document-level
	Heading   string `json:"heading"`
	Message   string `json:"message"`
}

// unresolvedPattern matches the placeholder an unfilled client fact leaves
// behind, e.g. [[UNRESOLVED: retention_period]]. Phase 4 (AI drafting against a
// client profile) is what emits these, but the check ships now so the approval
// gate is already in place when that lands — a policy that reaches a client
// with a placeholder in it is worse than one that was never generated.
var unresolvedPattern = regexp.MustCompile(`\[\[UNRESOLVED:[^\]]*\]\]`)

// weakPhrases are non-testable constructions. An assessor cannot test "the
// organization will endeavour to" — there is no pass or fail — so these read
// fine and fail audits. The mandatory vocabulary is must/shall (mandatory),
// should (expected, deviation needs justification) and may (discretionary).
var weakPhrases = []struct {
	pattern *regexp.Regexp
	advice  string
}{
	{regexp.MustCompile(`(?i)\bendeavou?rs?\s+to\b`), `"endeavour to" is not testable — use "must" or "should"`},
	{regexp.MustCompile(`(?i)\bstrives?\s+to\b`), `"strive to" is not testable — use "must" or "should"`},
	{regexp.MustCompile(`(?i)\bis\s+encouraged\s+to\b`), `"is encouraged to" is not testable — use "should"`},
	{regexp.MustCompile(`(?i)\bare\s+encouraged\s+to\b`), `"are encouraged to" is not testable — use "should"`},
	{regexp.MustCompile(`(?i)\bwhere\s+possible\b`), `"where possible" removes the obligation — state the condition or drop it`},
	{regexp.MustCompile(`(?i)\bwhere\s+practical\b`), `"where practical" removes the obligation — state the condition or drop it`},
	{regexp.MustCompile(`(?i)\bas\s+appropriate\b`), `"as appropriate" leaves the requirement undefined — say who decides and on what basis`},
	{regexp.MustCompile(`(?i)\bif\s+necessary\b`), `"if necessary" leaves the requirement undefined — state the trigger`},
	{regexp.MustCompile(`(?i)\bbest\s+effort\b`), `"best effort" is not testable — state the actual obligation`},
	{regexp.MustCompile(`(?i)\bwill\s+be\s+(?:performed|reviewed|conducted|maintained)\b`), `prefer "must be" over "will be" so the obligation is explicit`},
}

// normativePattern is the vocabulary a policy statement is expected to use.
var normativePattern = regexp.MustCompile(`(?i)\b(must|shall|should|may)\b`)

// requiredKinds lists the sections a document type cannot be approved without.
// Policies carry the full FedRAMP-derived set; the lower tiers inherit only what
// is meaningful for them, because forcing a management-commitment section onto a
// work instruction produces boilerplate nobody reads.
var requiredKinds = map[string][]string{
	TypePolicy:          {KindPurpose, KindScope, KindStatements, KindRoles, KindManagementCommitment, KindCompliance},
	TypeStandard:        {KindPurpose, KindScope, KindStatements},
	TypeProcedure:       {KindPurpose, KindScope, KindProcedureSteps, KindRoles},
	TypeWorkInstruction: {KindPurpose, KindProcedureSteps},
	TypeGuideline:       {KindPurpose, KindScope},
	TypeTRA:             {KindPurpose, KindScope, KindStatements},
}

// RequiredKinds returns the mandatory section kinds for a document type.
func RequiredKinds(docType string) []string {
	return requiredKinds[docType]
}

// Lint checks a document, its sections and its control mappings. Errors block
// approval, warnings are advisory. The split matters: a missing mandatory
// section or an unresolved placeholder is an objective defect an assessor would
// find, whereas weak wording is a judgement call the author is allowed to
// overrule. refs may be nil when mappings are not being considered.
func Lint(doc Document, sections []Section, refs []ControlRef) []Finding {
	findings := make([]Finding, 0)

	if len(sections) == 0 {
		return append(findings, Finding{
			Severity: SeverityError,
			Message:  "document has no sections",
		})
	}

	findings = append(findings, lintControlRefs(doc, refs)...)

	present := make(map[string]bool, len(sections))
	for _, s := range sections {
		if strings.TrimSpace(s.Body) != "" {
			present[s.SectionKind] = true
		}
	}
	for _, kind := range requiredKinds[doc.DocType] {
		if present[kind] {
			continue
		}
		findings = append(findings, Finding{
			Severity: SeverityError,
			Message:  fmt.Sprintf("missing required section for a %s: %s", docTypeLabel(doc.DocType), SectionKindLabels[kind]),
		})
	}

	for _, s := range sections {
		label := strings.TrimSpace(s.Heading)
		if label == "" {
			label = SectionKindLabels[s.SectionKind]
		}

		for _, match := range unresolvedPattern.FindAllString(s.Body, -1) {
			findings = append(findings, Finding{
				Severity:  SeverityError,
				SectionID: s.ID,
				Heading:   label,
				Message:   "unresolved placeholder " + match + " — supply the client fact before approval",
			})
		}

		if strings.TrimSpace(s.Body) == "" {
			findings = append(findings, Finding{
				Severity:  SeverityWarning,
				SectionID: s.ID,
				Heading:   label,
				Message:   "section is empty",
			})
			continue
		}

		for _, weak := range weakPhrases {
			if !weak.pattern.MatchString(s.Body) {
				continue
			}
			findings = append(findings, Finding{
				Severity:  SeverityWarning,
				SectionID: s.ID,
				Heading:   label,
				Message:   weak.advice,
			})
		}

		// Only statement-bearing sections are expected to carry obligations;
		// a Purpose or Definitions section legitimately has none.
		if (s.SectionKind == KindStatements || s.SectionKind == KindProcedureSteps) && !normativePattern.MatchString(s.Body) {
			findings = append(findings, Finding{
				Severity:  SeverityWarning,
				SectionID: s.ID,
				Heading:   label,
				Message:   "no must/shall/should/may in a statements section — the obligation is not stated",
			})
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Severity == SeverityError && findings[j].Severity != SeverityError
	})
	return findings
}

// BlockingFindings returns only the findings that prevent approval.
func BlockingFindings(findings []Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if f.Severity == SeverityError {
			out = append(out, f)
		}
	}
	return out
}

// lintControlRefs reports on the document's coverage claims.
//
// Both findings are warnings, not errors. An unmapped policy is legitimate —
// mapping is a separate pass and a guideline may never map to anything — and a
// stale ref is a fact about the catalog rather than a defect in the text. Making
// either block approval would make the linter something authors route around.
func lintControlRefs(doc Document, refs []ControlRef) []Finding {
	findings := make([]Finding, 0)

	if len(refs) == 0 {
		// Only worth saying when the document claims a framework: that is a
		// declaration that it satisfies something, with nothing recording what.
		if len(doc.Frameworks) > 0 && doc.DocType != TypeGuideline {
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Message: "declares " + strings.Join(doc.Frameworks, ", ") +
					" but maps no controls — the coverage matrix will not count it",
			})
		}
		return findings
	}

	for _, ref := range refs {
		if ref.Known {
			continue
		}
		findings = append(findings, Finding{
			Severity:  SeverityWarning,
			SectionID: ref.SectionID,
			Heading:   ref.SectionHeading,
			Message:   "maps " + ref.ControlID + ", which is no longer in the control catalog",
		})
	}
	return findings
}

func docTypeLabel(docType string) string {
	switch docType {
	case TypeWorkInstruction:
		return "work instruction"
	case TypeTRA:
		return "targeted risk analysis"
	default:
		return docType
	}
}
