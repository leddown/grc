package mapping

import (
	"strings"
	"testing"

	"grc/internal/regmap/profile"
	"grc/internal/regmap/requirement"
)

func testProfile() *profile.Profile {
	return &profile.Profile{
		ID:       "dora",
		IDPrefix: "DORA-ART",
		SeedCrosswalk: []profile.SeedEntry{
			{
				Group:     "ICT Risk Management (Arts 5-16)",
				Match:     profile.Matcher{Articles: "5-16"},
				Controls:  []string{"RA-3", "RA-5", "PM-9", "CM-2", "SI-4"},
				Rationale: "risk management framework",
			},
			{
				Group:    "Incident Mgmt (Arts 17-23)",
				Match:    profile.Matcher{Articles: "17-23"},
				Controls: []string{"IR-4", "IR-6", "IR-8", "AU-6"},
			},
			{
				Group:    "Keyword driven",
				Match:    profile.Matcher{Keywords: []string{"threat intelligence"}},
				Controls: []string{"PM-15"},
			},
		},
	}
}

func req(id, key, text string) requirement.Requirement {
	return requirement.Requirement{
		ID:        id,
		Key:       key,
		Framework: "dora",
		Text:      text,
		Status:    requirement.StatusApproved,
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name         string
		reqs         requirement.Set
		existing     Set
		wantControls map[string]string // reqID -> comma-joined controls, "" = UNMAPPED
		wantSeed     map[string]string
		wantStatus   map[string]requirement.Status
	}{
		{
			name: "numeric range selector resolves seed controls",
			reqs: requirement.Set{req("DORA-ART-6", "6", "ICT risk management framework")},
			wantControls: map[string]string{
				"DORA-ART-6": "CM-2,PM-9,RA-3,RA-5,SI-4",
			},
			wantSeed:   map[string]string{"DORA-ART-6": "ICT Risk Management (Arts 5-16)"},
			wantStatus: map[string]requirement.Status{"DORA-ART-6": requirement.StatusDraft},
		},
		{
			name: "a requirement outside every selector becomes UNMAPPED, not dropped",
			reqs: requirement.Set{req("DORA-ART-99", "99", "some unrelated provision")},
			wantControls: map[string]string{
				"DORA-ART-99": "",
			},
		},
		{
			name: "keyword selector matches on body text",
			reqs: requirement.Set{req("DORA-ART-45", "45", "Financial entities may exchange threat intelligence among themselves")},
			wantControls: map[string]string{
				"DORA-ART-45": "PM-15",
			},
		},
		{
			name: "first matching seed entry wins deterministically",
			reqs: requirement.Set{req("DORA-ART-17", "17", "incident management process and threat intelligence")},
			wantControls: map[string]string{
				"DORA-ART-17": "AU-6,IR-4,IR-6,IR-8",
			},
			wantSeed: map[string]string{"DORA-ART-17": "Incident Mgmt (Arts 17-23)"},
		},
		{
			name: "an approved mapping is never clobbered by a re-run",
			reqs: requirement.Set{req("DORA-ART-6", "6", "ICT risk management framework")},
			existing: Set{{
				ReqID:          "DORA-ART-6",
				Source:         SourceCurated,
				Status:         requirement.StatusApproved,
				NISTControlIDs: []string{"RA-3"},
				Rationale:      "reviewer trimmed this to one control",
				ReviewedBy:     "alice",
				ReviewedAt:     "2026-01-01T00:00:00Z",
			}},
			wantControls: map[string]string{"DORA-ART-6": "RA-3"},
			wantStatus:   map[string]requirement.Status{"DORA-ART-6": requirement.StatusApproved},
		},
		{
			name: "a rejected mapping is preserved for the audit trail",
			reqs: requirement.Set{req("DORA-ART-6", "6", "ICT risk management framework")},
			existing: Set{{
				ReqID:      "DORA-ART-6",
				Source:     SourceCurated,
				Status:     requirement.StatusRejected,
				ReviewedBy: "alice",
			}},
			wantStatus: map[string]requirement.Status{"DORA-ART-6": requirement.StatusRejected},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.reqs, testProfile(), tc.existing)

			for id, want := range tc.wantControls {
				i := got.Index(id, SourceCurated)
				if i < 0 {
					t.Fatalf("no curated entry for %s", id)
				}
				joined := strings.Join(got[i].NISTControlIDs, ",")
				if joined != want {
					t.Errorf("%s controls = %q, want %q", id, joined, want)
				}
				if want == "" && !got[i].IsUnmapped() {
					t.Errorf("%s should report as UNMAPPED", id)
				}
			}
			for id, want := range tc.wantSeed {
				i := got.Index(id, SourceCurated)
				if got[i].SeedGroup != want {
					t.Errorf("%s seedGroup = %q, want %q", id, got[i].SeedGroup, want)
				}
			}
			for id, want := range tc.wantStatus {
				i := got.Index(id, SourceCurated)
				if got[i].Status != want {
					t.Errorf("%s status = %q, want %q", id, got[i].Status, want)
				}
			}
		})
	}
}

// TestResolveNeverApproves is the property that matters most: the resolver
// produces drafts only, whatever the seed crosswalk says.
func TestResolveNeverApproves(t *testing.T) {
	reqs := requirement.Set{
		req("DORA-ART-6", "6", "risk"),
		req("DORA-ART-20", "20", "incident"),
		req("DORA-ART-99", "99", "nothing matches"),
	}
	for _, e := range Resolve(reqs, testProfile(), nil) {
		if e.Status != requirement.StatusDraft {
			t.Errorf("%s: Resolve produced status %q; only review may approve", e.ReqID, e.Status)
		}
		if e.Source != SourceCurated {
			t.Errorf("%s: source = %q, want curated", e.ReqID, e.Source)
		}
	}
}

func TestResolveIsIdempotent(t *testing.T) {
	reqs := requirement.Set{req("DORA-ART-6", "6", "risk"), req("DORA-ART-99", "99", "unmatched")}
	prof := testProfile()

	first := Resolve(reqs, prof, nil)
	second := Resolve(reqs, prof, first)
	if len(first) != len(second) {
		t.Fatalf("re-running Resolve changed the entry count: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ReqID != second[i].ReqID ||
			strings.Join(first[i].NISTControlIDs, ",") != strings.Join(second[i].NISTControlIDs, ",") ||
			first[i].Status != second[i].Status {
			t.Errorf("entry %d changed across runs: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestNormalizeControls(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"upper-cases and trims", []string{" ir-4 ", "au-6"}, "AU-6,IR-4"},
		{"de-duplicates", []string{"IR-4", "ir-4"}, "IR-4"},
		{"sorts naturally not lexically", []string{"IR-10", "IR-4"}, "IR-4,IR-10"},
		{"drops blanks", []string{"", "  ", "SC-7"}, "SC-7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(NormalizeControls(tc.in), ","); got != tc.want {
				t.Errorf("NormalizeControls(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
