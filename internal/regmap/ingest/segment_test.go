package ingest

import (
	"strings"
	"testing"

	"grc/internal/regmap/profile"
	"grc/internal/regmap/requirement"
)

// The fixtures below are deliberately messy: inconsistent heading spacing,
// titles on their own line and inline, abbreviations, page-number noise and
// front matter that belongs to no requirement.

const doraSample = `REGULATION (EU) 2022/2554 OF THE EUROPEAN PARLIAMENT AND OF THE COUNCIL

Having regard to the Treaty on the Functioning of the European Union,

Article 5
Governance and organisation

1. Financial entities shall have in place an internal governance and control
framework that ensures an effective and prudent management of ICT risk, in
order to achieve a high level of digital operational resilience.

12

Article 6  ICT risk management framework

Financial entities shall have a sound, comprehensive and well-documented ICT
risk management framework as part of their overall risk management system,
which enables them to address ICT risk quickly and efficiently.

Art. 7
ICT systems, protocols and tools

In order to address and manage ICT risk, financial entities shall use and
maintain updated ICT systems, protocols and tools that are appropriate to the
magnitude of operations supporting the conduct of their activities.
`

const pciSample = `Requirement 3: Protect Stored Account Data

3.1 Processes and mechanisms for protecting stored account data are defined and
understood, documented, kept up to date, in use and known to all affected parties.

3.4.1 PAN is masked when displayed such that only personnel with a legitimate
business need can see more than the BIN and the last four digits of the PAN.

Page 42 of 360

3.5.1 PAN is rendered unreadable anywhere it is stored by using one-way hashes,
truncation, index tokens or strong cryptography with key management processes.
`

const craSample = `Annex I
ESSENTIAL CYBERSECURITY REQUIREMENTS

Part I
Cybersecurity requirements relating to the properties of products with digital elements

(1) Products with digital elements shall be designed, developed and produced in
such a way that they ensure an appropriate level of cybersecurity based on the
risks, and shall be made available on the market without any known exploitable
vulnerabilities.

Part II
Vulnerability handling requirements

Manufacturers of the products with digital elements shall identify and document
vulnerabilities and components contained in products, including by drawing up a
software bill of materials in a commonly used machine-readable format.
`

const customSample = `Preface material that no rule claims.

SEC-1 Scope of the standard

The standard applies to all operators of essential services within the sector
and to their designated critical suppliers under the relevant schedule.

SEC-2 Continuity obligations

Operators shall maintain, test and periodically review continuity arrangements
covering the loss of any single critical facility or supplier relationship.
`

func articleProfile() *profile.Profile {
	return &profile.Profile{
		ID:           "dora",
		DisplayName:  "DORA",
		SourceRef:    "EU 2022/2554",
		IDPrefix:     "DORA-ART",
		Segmentation: []profile.Strategy{{Type: profile.StrategyArticle, Label: "Article", MinBodyChars: 120}},
		Classification: profile.Classification{
			Categories: []string{"ICT Risk Management"},
			Rules: []profile.ClassificationRule{
				{Category: "ICT Risk Management", Match: profile.Matcher{Articles: "5-16"}},
			},
		},
	}
}

func numberedProfile() *profile.Profile {
	return &profile.Profile{
		ID:       "pci-dss",
		IDPrefix: "PCI",
		Segmentation: []profile.Strategy{
			{Type: profile.StrategyNumbered, MinDepth: 2, MaxDepth: 3, MinBodyChars: 60},
			{Type: profile.StrategyRegex, Pattern: `(?im)^[ \t]*Requirement[ \t]+(\d{1,2})\b[ \t]*:?[ \t]*`, Label: "Requirement", IDPrefix: "PCI-REQ", MinBodyChars: 60},
		},
		Classification: profile.Classification{
			Categories: []string{"Protect Stored Account Data"},
			Rules: []profile.ClassificationRule{
				{Category: "Protect Stored Account Data", Match: profile.Matcher{Sections: []string{"3."}, Articles: "3"}},
			},
		},
	}
}

func annexProfile() *profile.Profile {
	return &profile.Profile{
		ID:       "cra",
		IDPrefix: "CRA-ART",
		Segmentation: []profile.Strategy{
			{Type: profile.StrategyArticle, Label: "Article", MinBodyChars: 120},
			{Type: profile.StrategyAnnex, Label: "Annex", IDPrefix: "CRA-ANNEX", MinBodyChars: 80},
			{Type: profile.StrategyRegex, Pattern: `(?m)^[ \t]*Part[ \t]+([IVX]+)\b[ \t]*:?[ \t]*`, Label: "Annex Part", IDPrefix: "CRA-ANNEX-PART", MinBodyChars: 80},
		},
		Classification: profile.Classification{
			Categories: []string{"Essential Cybersecurity Requirements", "Vulnerability Handling Requirements"},
			Rules: []profile.ClassificationRule{
				{Category: "Vulnerability Handling Requirements", Match: profile.Matcher{Keywords: []string{"software bill of materials"}}},
				{Category: "Essential Cybersecurity Requirements", Match: profile.Matcher{Annexes: []string{"I"}}},
			},
		},
	}
}

func customRegexProfile() *profile.Profile {
	return &profile.Profile{
		ID:       "custom",
		IDPrefix: "CUSTOM-SEC",
		Segmentation: []profile.Strategy{
			{Type: profile.StrategyRegex, Pattern: `(?m)^[ \t]*SEC-(\d+)[ \t]*`, Label: "Section", MinBodyChars: 80},
		},
	}
}

// The pieces below are what the Wintermute server returns for the samples above
// under the same profiles: its framework segmenter is a port of the one this
// package used to run, and wintermute's own tests pin it to these samples. What
// is tested here is this module's part — ids, titles, categories, flags.

func doraPieces() []Piece {
	return []Piece{
		{Strategy: "preamble", Label: "Preamble", Key: "preamble",
			Title: "REGULATION (EU) 2022/2554 OF THE EUROPEAN PARLIAMENT AND OF THE COUNCIL",
			Body:  "REGULATION (EU) 2022/2554 OF THE EUROPEAN PARLIAMENT AND OF THE COUNCIL\n\nHaving regard to the Treaty on the Functioning of the European Union,"},
		{Strategy: "article", Label: "Article 5", Key: "5", Title: "Governance and organisation",
			Body: "1. Financial entities shall have in place an internal governance and control\nframework that ensures an effective and prudent management of ICT risk, in\norder to achieve a high level of digital operational resilience.\n\n12"},
		{Strategy: "article", Label: "Article 6", Key: "6", Title: "ICT risk management framework",
			Body: "Financial entities shall have a sound, comprehensive and well-documented ICT\nrisk management framework as part of their overall risk management system,\nwhich enables them to address ICT risk quickly and efficiently."},
		{Strategy: "article", Label: "Article 7", Key: "7", Title: "ICT systems, protocols and tools",
			Body: "In order to address and manage ICT risk, financial entities shall use and\nmaintain updated ICT systems, protocols and tools that are appropriate to the\nmagnitude of operations supporting the conduct of their activities."},
	}
}

func pciPieces() []Piece {
	return []Piece{
		{Strategy: "regex", Label: "Requirement 3", Key: "3", Title: "Protect Stored Account Data"},
		{Strategy: "numbered", Label: "3.1", Key: "3.1",
			Title: "Processes and mechanisms for protecting stored account data are defined and",
			Body:  "understood, documented, kept up to date, in use and known to all affected parties."},
		{Strategy: "numbered", Label: "3.4.1", Key: "3.4.1",
			Title: "PAN is masked when displayed such that only personnel with a legitimate",
			Body:  "business need can see more than the BIN and the last four digits of the PAN.\n\nPage 42 of 360"},
		{Strategy: "numbered", Label: "3.5.1", Key: "3.5.1",
			Title: "PAN is rendered unreadable anywhere it is stored by using one-way hashes,",
			Body:  "truncation, index tokens or strong cryptography with key management processes."},
	}
}

func craPieces() []Piece {
	return []Piece{
		{Strategy: "annex", Label: "Annex I", Key: "I", Title: "ESSENTIAL CYBERSECURITY REQUIREMENTS"},
		{Strategy: "regex", Label: "Annex Part I", Key: "I",
			Title: "Cybersecurity requirements relating to the properties of products with digital elements",
			Body:  "(1) Products with digital elements shall be designed, developed and produced in\nsuch a way that they ensure an appropriate level of cybersecurity based on the\nrisks, and shall be made available on the market without any known exploitable\nvulnerabilities."},
		{Strategy: "regex", Label: "Annex Part II", Key: "II", Title: "Vulnerability handling requirements",
			Body: "Manufacturers of the products with digital elements shall identify and document\nvulnerabilities and components contained in products, including by drawing up a\nsoftware bill of materials in a commonly used machine-readable format."},
	}
}

func customPieces() []Piece {
	return []Piece{
		{Strategy: "preamble", Label: "Preamble", Key: "preamble", Title: "Preface material that no rule claims",
			Body: "Preface material that no rule claims."},
		{Strategy: "regex", Label: "Section 1", Key: "1", Title: "Scope of the standard",
			Body: "The standard applies to all operators of essential services within the sector\nand to their designated critical suppliers under the relevant schedule."},
		{Strategy: "regex", Label: "Section 2", Key: "2", Title: "Continuity obligations",
			Body: "Operators shall maintain, test and periodically review continuity arrangements\ncovering the loss of any single critical facility or supplier relationship."},
	}
}

func TestBuildRequirementsPerStrategy(t *testing.T) {
	tests := []struct {
		name   string
		pieces []Piece
		prof   *profile.Profile

		wantIDs        []string
		wantTitles     map[string]string
		wantCategories map[string]string
		wantFlags      map[string]string
		wantStrategy   map[string]string
	}{
		{
			name:    "articles, with the front matter kept as a preamble",
			pieces:  doraPieces(),
			prof:    articleProfile(),
			wantIDs: []string{"DORA-ART-5", "DORA-ART-6", "DORA-ART-7", "DORA-PREAMBLE"},
			wantTitles: map[string]string{
				"DORA-ART-5": "Governance and organisation",
				"DORA-ART-6": "ICT risk management framework",
				"DORA-ART-7": "ICT systems, protocols and tools",
			},
			wantCategories: map[string]string{
				"DORA-ART-5": "ICT Risk Management",
				"DORA-ART-6": "ICT Risk Management",
				"DORA-ART-7": "ICT Risk Management",
				// Front matter is kept but never guessed at.
				"DORA-PREAMBLE": requirement.Unclassified,
			},
			wantFlags:    map[string]string{"DORA-PREAMBLE": "preamble"},
			wantStrategy: map[string]string{"DORA-ART-5": "article", "DORA-PREAMBLE": "preamble"},
		},
		{
			name:    "hierarchical numbers and top-level headings take their own prefixes",
			pieces:  pciPieces(),
			prof:    numberedProfile(),
			wantIDs: []string{"PCI-3.1", "PCI-3.4.1", "PCI-3.5.1", "PCI-REQ-3"},
			wantTitles: map[string]string{
				"PCI-REQ-3": "Protect Stored Account Data",
			},
			wantCategories: map[string]string{
				"PCI-3.4.1": "Protect Stored Account Data",
				"PCI-REQ-3": "Protect Stored Account Data",
			},
			wantStrategy: map[string]string{"PCI-3.4.1": "numbered", "PCI-REQ-3": "regex"},
		},
		{
			name:    "an annex and its parts",
			pieces:  craPieces(),
			prof:    annexProfile(),
			wantIDs: []string{"CRA-ANNEX-I", "CRA-ANNEX-PART-I", "CRA-ANNEX-PART-II"},
			wantCategories: map[string]string{
				"CRA-ANNEX-I":       "Essential Cybersecurity Requirements",
				"CRA-ANNEX-PART-I":  "Essential Cybersecurity Requirements",
				"CRA-ANNEX-PART-II": "Vulnerability Handling Requirements",
			},
			// The Annex heading itself carries almost no body text.
			wantFlags: map[string]string{"CRA-ANNEX-I": "short-body"},
		},
		{
			name:           "a profile's own pattern",
			pieces:         customPieces(),
			prof:           customRegexProfile(),
			wantIDs:        []string{"CUSTOM-PREAMBLE", "CUSTOM-SEC-1", "CUSTOM-SEC-2"},
			wantTitles:     map[string]string{"CUSTOM-SEC-1": "Scope of the standard", "CUSTOM-SEC-2": "Continuity obligations"},
			wantCategories: map[string]string{"CUSTOM-SEC-1": requirement.Unclassified},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := BuildRequirements(tc.pieces, tc.prof)
			if err != nil {
				t.Fatalf("BuildRequirements: %v", err)
			}

			var gotIDs []string
			for _, r := range res.Requirements {
				gotIDs = append(gotIDs, r.ID)
			}
			if strings.Join(gotIDs, ",") != strings.Join(tc.wantIDs, ",") {
				t.Errorf("ids = %v, want %v", gotIDs, tc.wantIDs)
			}

			for id, want := range tc.wantTitles {
				i := res.Requirements.Index(id)
				if i < 0 {
					t.Errorf("missing requirement %s", id)
					continue
				}
				if got := res.Requirements[i].Title; got != want {
					t.Errorf("%s title = %q, want %q", id, got, want)
				}
			}
			for id, want := range tc.wantCategories {
				i := res.Requirements.Index(id)
				if i < 0 {
					t.Errorf("missing requirement %s", id)
					continue
				}
				if got := res.Requirements[i].Category; got != want {
					t.Errorf("%s category = %q, want %q", id, got, want)
				}
			}
			for id, want := range tc.wantFlags {
				i := res.Requirements.Index(id)
				if i < 0 {
					t.Errorf("missing requirement %s", id)
					continue
				}
				if !hasAny(res.Requirements[i].Flags, want) {
					t.Errorf("%s flags = %v, want to contain %q", id, res.Requirements[i].Flags, want)
				}
			}
			for id, want := range tc.wantStrategy {
				i := res.Requirements.Index(id)
				if i < 0 {
					t.Errorf("missing requirement %s", id)
					continue
				}
				if got := res.Requirements[i].Strategy; got != want {
					t.Errorf("%s strategy = %q, want %q", id, got, want)
				}
			}

			// Every ingested requirement starts as a draft, without exception.
			for _, r := range res.Requirements {
				if r.Status != requirement.StatusDraft {
					t.Errorf("%s status = %q after ingest, want draft", r.ID, r.Status)
				}
			}
		})
	}
}

// Nothing the server cut is lost on the way to requirements: every piece's text
// is in some requirement, including the preamble and page-number noise kept for
// review.
func TestNothingTheServerCutIsLost(t *testing.T) {
	res, err := BuildRequirements(doraPieces(), articleProfile())
	if err != nil {
		t.Fatalf("BuildRequirements: %v", err)
	}
	var got strings.Builder
	for _, r := range res.Requirements {
		got.WriteString(r.Title)
		got.WriteString(r.Text)
	}
	joined := squash(got.String())
	for _, fragment := range []string{
		"Having regard to the Treaty", "internal governance and control",
		"sound, comprehensive and well-document", "maintain updated ICT systems", "12",
	} {
		if !strings.Contains(joined, squash(fragment)) {
			t.Errorf("requirements dropped %q", fragment)
		}
	}
}

// A long article the server cut into parts is one requirement again.
func TestPartsAreOneRequirement(t *testing.T) {
	pieces := []Piece{
		{Strategy: "article", Label: "Article 5", Key: "5", Title: "Governance", Body: "First half of a long article.", Part: 1},
		{Strategy: "article", Label: "Article 5", Key: "5", Title: "Governance", Body: "Second half of it.", Part: 2},
		{Strategy: "article", Label: "Article 6", Key: "6", Title: "Framework", Body: "Another article entirely."},
	}
	res, err := BuildRequirements(pieces, articleProfile())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Requirements) != 2 {
		t.Fatalf("requirements %+v", res.Requirements)
	}
	if i := res.Requirements.Index("DORA-ART-5"); i < 0 ||
		res.Requirements[i].Text != "First half of a long article.\n\nSecond half of it." {
		t.Errorf("article 5 %+v", res.Requirements)
	}
}

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }
