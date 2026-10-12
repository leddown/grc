package profile

import (
	"testing"

	"grc/regmap/profiles"
)

func TestMatcher(t *testing.T) {
	tests := []struct {
		name    string
		matcher Matcher
		key     string
		text    string
		want    bool
	}{
		{"range hit at the lower bound", Matcher{Articles: "5-16"}, "5", "", true},
		{"range hit at the upper bound", Matcher{Articles: "5-16"}, "16", "", true},
		{"range miss above", Matcher{Articles: "5-16"}, "17", "", false},
		{"range miss below", Matcher{Articles: "5-16"}, "4", "", false},
		{"single value", Matcher{Articles: "45"}, "45", "", true},
		{"comma list", Matcher{Articles: "5,7,9"}, "7", "", true},
		{"comma list miss", Matcher{Articles: "5,7,9"}, "8", "", false},
		{"mixed list and range", Matcher{Articles: "5-16,45"}, "45", "", true},
		{"lettered article key uses its leading number", Matcher{Articles: "5-16"}, "6a", "", true},
		{"section prefix", Matcher{Sections: []string{"3."}}, "3.4.1", "", true},
		{"section prefix does not match a sibling", Matcher{Sections: []string{"3."}}, "31.4", "", false},
		{"annex exact match is case-insensitive", Matcher{Annexes: []string{"i"}}, "I", "", true},
		{"annex miss", Matcher{Annexes: []string{"I"}}, "II", "", false},
		{"keyword match is case-insensitive", Matcher{Keywords: []string{"Threat Intelligence"}}, "", "sharing threat intelligence widely", true},
		{"keyword miss", Matcher{Keywords: []string{"threat intelligence"}}, "", "unrelated text", false},
		{"selectors are OR-ed", Matcher{Articles: "1-2", Keywords: []string{"backup"}}, "99", "daily backup duties", true},
		{"empty matcher never matches", Matcher{}, "5", "anything", false},
		{"non-numeric key against a range", Matcher{Articles: "5-16"}, "I", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.matcher.Matches(tc.key, tc.text); got != tc.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", tc.key, tc.text, got, tc.want)
			}
		})
	}
}

func TestStrategyPatterns(t *testing.T) {
	tests := []struct {
		name     string
		strategy Strategy
		text     string
		wantKeys []string
	}{
		{
			name:     "article matches full word and abbreviation",
			strategy: Strategy{Type: StrategyArticle},
			text:     "Article 5\nbody\nArt. 12 title\nbody\nArticle 6a\n",
			wantKeys: []string{"5", "12", "6a"},
		},
		{
			name:     "article label is configurable",
			strategy: Strategy{Type: StrategyArticle, Label: "Regulation"},
			text:     "Regulation 3 something\nArticle 4 ignored\n",
			wantKeys: []string{"3"},
		},
		{
			// Numbers shallower or deeper than the configured bounds are left
			// alone rather than being truncated into a bogus boundary.
			name:     "numbered honours the depth bounds",
			strategy: Strategy{Type: StrategyNumbered, MinDepth: 2, MaxDepth: 3},
			text:     "1 not deep enough\n3.1 yes\n3.4.1 yes\n3.4.1.2 too deep\n",
			wantKeys: []string{"3.1", "3.4.1"},
		},
		{
			name:     "annex accepts roman and arabic numerals",
			strategy: Strategy{Type: StrategyAnnex},
			text:     "Annex I\nbody\nAnnex 2\nbody\n",
			wantKeys: []string{"I", "2"},
		},
		{
			name:     "regex strategy uses the supplied pattern",
			strategy: Strategy{Type: StrategyRegex, Pattern: `(?m)^SEC-(\d+)`},
			text:     "SEC-1 scope\nSEC-2 duties\n",
			wantKeys: []string{"1", "2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			re, err := tc.strategy.Regexp()
			if err != nil {
				t.Fatalf("Regexp: %v", err)
			}
			var keys []string
			for _, m := range re.FindAllStringSubmatch(tc.text, -1) {
				keys = append(keys, m[1])
			}
			if len(keys) != len(tc.wantKeys) {
				t.Fatalf("keys = %v, want %v", keys, tc.wantKeys)
			}
			for i := range keys {
				if keys[i] != tc.wantKeys[i] {
					t.Errorf("key %d = %q, want %q", i, keys[i], tc.wantKeys[i])
				}
			}
		})
	}
}

func TestValidateRejectsBadProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		wantErr string
	}{
		{
			name:    "missing idPrefix",
			profile: Profile{ID: "x", Segmentation: []Strategy{{Type: StrategyArticle}}},
			wantErr: "idPrefix is required",
		},
		{
			name:    "no segmentation",
			profile: Profile{ID: "x", IDPrefix: "X"},
			wantErr: "at least one segmentation strategy",
		},
		{
			name:    "unknown strategy",
			profile: Profile{ID: "x", IDPrefix: "X", Segmentation: []Strategy{{Type: "chapters"}}},
			wantErr: "unknown segmentation strategy",
		},
		{
			name:    "regex without a pattern",
			profile: Profile{ID: "x", IDPrefix: "X", Segmentation: []Strategy{{Type: StrategyRegex}}},
			wantErr: "regex strategy requires a pattern",
		},
		{
			name: "regex without a capture group",
			profile: Profile{ID: "x", IDPrefix: "X", Segmentation: []Strategy{
				{Type: StrategyRegex, Pattern: `^Section \d+`},
			}},
			wantErr: "capture group",
		},
		{
			name: "seed entry with no controls",
			profile: Profile{ID: "x", IDPrefix: "X",
				Segmentation:  []Strategy{{Type: StrategyArticle}},
				SeedCrosswalk: []SeedEntry{{Group: "g", Match: Matcher{Articles: "1"}}}},
			wantErr: "has no controls",
		},
		{
			name: "seed entry with no selector",
			profile: Profile{ID: "x", IDPrefix: "X",
				Segmentation:  []Strategy{{Type: StrategyArticle}},
				SeedCrosswalk: []SeedEntry{{Group: "g", Controls: []string{"RA-3"}}}},
			wantErr: "no match selector",
		},
		{
			name: "classification rule using an undeclared category",
			profile: Profile{ID: "x", IDPrefix: "X",
				Segmentation: []Strategy{{Type: StrategyArticle}},
				Classification: Classification{
					Categories: []string{"Known"},
					Rules:      []ClassificationRule{{Category: "Typo", Match: Matcher{Articles: "1"}}},
				}},
			wantErr: "not declared in categories",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.profile.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestShippedProfilesAreValid keeps the four starter profiles honest: they all
// have to load and validate through the same code path.
func TestShippedProfilesAreValid(t *testing.T) {
	reg, err := LoadDir("../../../regmap/profiles")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	want := map[string]bool{"cra": false, "dora": false, "nis2": false, "pci-dss": false}
	for _, p := range reg.All() {
		if _, ok := want[p.ID]; ok {
			want[p.ID] = true
		}
		// eu-generic is the fallback for an instrument that has no profile of
		// its own, so there is no one act for it to name — a report built on it
		// takes its header from the uploaded document instead. Every profile
		// that does name a specific framework must say which.
		if p.SourceRef == "" && p.ID != "eu-generic" {
			t.Errorf("profile %s has no sourceRef; reports name it in the header", p.ID)
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("starter profile %q is missing from profiles/", id)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// The server reads the same profiles from the embedded copy rather than from a
// directory next to the binary, so the two paths must agree.
func TestLoadFSMatchesLoadDir(t *testing.T) {
	fromDisk, err := LoadDir("../../../regmap/profiles")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	fromEmbed, err := LoadFS(profiles.FS, ".")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(fromEmbed.All()) != len(fromDisk.All()) {
		t.Fatalf("embedded set has %d profiles, disk has %d", len(fromEmbed.All()), len(fromDisk.All()))
	}
	for i, p := range fromEmbed.All() {
		if got, want := p.ID, fromDisk.All()[i].ID; got != want {
			t.Errorf("profile %d = %q, want %q", i, got, want)
		}
	}
}
