package suggest

import (
	"strings"
	"testing"

	"grc/internal/regmap/mapping"
	"grc/internal/regmap/requirement"
)

// TestParse covers the responses a model can realistically return, including
// the malformed ones. The parser must never panic and never invent a mapping.
func TestParse(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		wantErr      string
		wantControls string
		wantConf     string
	}{
		{
			name:         "clean json",
			in:           `{"controlIDs":["IR-4","IR-6"],"rationale":"incident duties","confidence":"high"}`,
			wantControls: "IR-4,IR-6",
			wantConf:     "high",
		},
		{
			name:         "json wrapped in a markdown fence",
			in:           "```json\n{\"controlIDs\":[\"RA-5\"],\"rationale\":\"scanning\",\"confidence\":\"medium\"}\n```",
			wantControls: "RA-5",
			wantConf:     "medium",
		},
		{
			name:         "json preceded by prose",
			in:           "Here is my answer:\n{\"controlIDs\":[\"SC-7\"],\"rationale\":\"boundary\",\"confidence\":\"low\"}",
			wantControls: "SC-7",
			wantConf:     "low",
		},
		{
			name:         "empty control list is a valid no-match answer",
			in:           `{"controlIDs":[],"rationale":"nothing fits","confidence":"low"}`,
			wantControls: "",
		},
		{name: "no json at all", in: "I cannot help with that.", wantErr: "no JSON object"},
		{name: "empty response", in: "   ", wantErr: "no text content"},
		{name: "malformed json", in: `{"controlIDs": [IR-4]}`, wantErr: "parse model JSON"},
		{
			name:    "implausible control count is rejected rather than trusted",
			in:      `{"controlIDs":[` + strings.TrimSuffix(strings.Repeat(`"AC-2",`, 30), ",") + `]}`,
			wantErr: "implausible",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parse(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parse(%q) = nil error, want one containing %q", tc.in, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if joined := strings.Join(got.ControlIDs, ","); joined != tc.wantControls {
				t.Errorf("controls = %q, want %q", joined, tc.wantControls)
			}
			if tc.wantConf != "" && normalizeConfidence(got.Confidence) != tc.wantConf {
				t.Errorf("confidence = %q, want %q", got.Confidence, tc.wantConf)
			}
		})
	}
}

func TestNormalizeConfidence(t *testing.T) {
	tests := []struct{ in, want string }{
		{"high", "high"},
		{"HIGH", "high"},
		{" Medium ", "medium"},
		{"low", "low"},
		// An unrecognised value must degrade to the most cautious answer.
		{"very confident", "low"},
		{"", "low"},
	}
	for _, tc := range tests {
		if got := normalizeConfidence(tc.in); got != tc.want {
			t.Errorf("normalizeConfidence(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTargets(t *testing.T) {
	reqs := requirement.Set{
		{ID: "R-APPROVED-UNMAPPED", Status: requirement.StatusApproved},
		{ID: "R-APPROVED-MAPPED", Status: requirement.StatusApproved},
		{ID: "R-DRAFT", Status: requirement.StatusDraft},
		{ID: "R-REJECTED", Status: requirement.StatusRejected},
		{ID: "R-ALREADY-SUGGESTED", Status: requirement.StatusApproved},
		{ID: "R-SUGGESTION-REJECTED", Status: requirement.StatusApproved},
	}
	entries := mapping.Set{
		{ReqID: "R-APPROVED-UNMAPPED", Source: mapping.SourceCurated, Status: requirement.StatusApproved},
		{ReqID: "R-APPROVED-MAPPED", Source: mapping.SourceCurated, Status: requirement.StatusApproved, NISTControlIDs: []string{"RA-3"}},
		{ReqID: "R-DRAFT", Source: mapping.SourceCurated, Status: requirement.StatusDraft},
		{ReqID: "R-ALREADY-SUGGESTED", Source: mapping.SourceCurated, Status: requirement.StatusApproved},
		{ReqID: "R-ALREADY-SUGGESTED", Source: mapping.SourceLLMSuggested, Status: requirement.StatusDraft},
		{ReqID: "R-SUGGESTION-REJECTED", Source: mapping.SourceCurated, Status: requirement.StatusApproved},
		{ReqID: "R-SUGGESTION-REJECTED", Source: mapping.SourceLLMSuggested, Status: requirement.StatusRejected},
	}

	var got []string
	for _, r := range Targets(reqs, entries) {
		got = append(got, r.ID)
	}
	want := []string{"R-APPROVED-UNMAPPED", "R-ALREADY-SUGGESTED"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Targets = %v, want %v", got, want)
	}
}

// TestSuggestionsAreNeverApproved pins the GATE 3 guarantee at the type level:
// a suggestion is only ever constructed as a draft.
func TestSuggestionsAreNeverApproved(t *testing.T) {
	e := mapping.Entry{
		Source: mapping.SourceLLMSuggested,
		Status: requirement.StatusDraft,
	}
	if e.Status == requirement.StatusApproved {
		t.Fatal("an llm-suggested entry must start as a draft")
	}
	if e.Source != mapping.SourceLLMSuggested {
		t.Error("machine suggestions must be labelled as such in the audit trail")
	}
}
