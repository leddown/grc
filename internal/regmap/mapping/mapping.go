// Package mapping holds crosswalk entries and the seed-crosswalk resolver.
// The resolver is framework-agnostic: everything it needs comes from the
// profile's seedCrosswalk selectors.
package mapping

import (
	"sort"
	"strings"

	"grc/internal/regmap/profile"
	"grc/internal/regmap/requirement"
)

// Source records where a mapping came from.
type Source string

const (
	SourceCurated      Source = "curated"
	SourceLLMSuggested Source = "llm-suggested"
)

// Confidence levels for a mapping.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Entry is one requirement -> controls crosswalk row.
type Entry struct {
	ReqID          string             `yaml:"reqID"`
	Framework      string             `yaml:"framework"`
	NISTControlIDs []string           `yaml:"nistControlIDs"`
	Rationale      string             `yaml:"rationale"`
	Confidence     string             `yaml:"confidence"`
	Source         Source             `yaml:"source"`
	Status         requirement.Status `yaml:"status"`
	ReviewedBy     string             `yaml:"reviewedBy,omitempty"`
	ReviewedAt     string             `yaml:"reviewedAt,omitempty"`

	// SeedGroup names the profile seed entry that produced this mapping, so a
	// reviewer can see why the tool proposed these controls.
	SeedGroup string `yaml:"seedGroup,omitempty"`
	// Model records the LLM that suggested the mapping (source=llm-suggested).
	Model string `yaml:"model,omitempty"`
	// Unknown holds control IDs the model proposed that are not in the
	// catalog. They are kept for the audit trail, never silently dropped.
	Unknown []string `yaml:"unknownControlIDs,omitempty"`
}

// IsUnmapped reports whether the entry resolved to no controls.
func (e Entry) IsUnmapped() bool { return len(e.NISTControlIDs) == 0 }

// Controls renders the control list for display.
func (e Entry) Controls() string {
	if e.IsUnmapped() {
		return "UNMAPPED"
	}
	return strings.Join(e.NISTControlIDs, ", ")
}

// Set is a collection of mapping entries with deterministic ordering.
type Set []Entry

// Sort orders entries by requirement ID, then by source so curated entries
// precede LLM suggestions for the same requirement.
func (s Set) Sort() {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].ReqID != s[j].ReqID {
			return requirement.NaturalLess(s[i].ReqID, s[j].ReqID)
		}
		return s[i].Source < s[j].Source
	})
}

// Index returns the position of the entry for reqID from the given source, or -1.
func (s Set) Index(reqID string, source Source) int {
	for i := range s {
		if s[i].ReqID == reqID && s[i].Source == source {
			return i
		}
	}
	return -1
}

// ForRequirement returns every entry belonging to reqID.
func (s Set) ForRequirement(reqID string) Set {
	var out Set
	for _, e := range s {
		if e.ReqID == reqID {
			out = append(out, e)
		}
	}
	return out
}

// HasApproved reports whether reqID already has a human-approved mapping.
func (s Set) HasApproved(reqID string) bool {
	for _, e := range s {
		if e.ReqID == reqID && e.Status == requirement.StatusApproved {
			return true
		}
	}
	return false
}

// Resolve builds draft mapping entries for the given requirements from the
// profile's seed crosswalk. Requirements with no seed hit produce an UNMAPPED
// entry rather than being dropped.
//
// Resolve is idempotent and never clobbers human decisions: an existing entry
// that has been approved or rejected is carried through untouched, and edits to
// a draft entry's controls or rationale are preserved.
func Resolve(reqs requirement.Set, prof *profile.Profile, existing Set) Set {
	out := make(Set, 0, len(reqs))
	for _, req := range reqs {
		prev := existing.Index(req.ID, SourceCurated)
		if prev >= 0 && existing[prev].Status != requirement.StatusDraft {
			// Reviewed: preserve verbatim.
			out = append(out, existing[prev])
			continue
		}

		entry := Entry{
			ReqID:      req.ID,
			Framework:  req.Framework,
			Source:     SourceCurated,
			Status:     requirement.StatusDraft,
			Confidence: ConfidenceLow,
		}
		if seed, ok := prof.Seed(req.Key, req.Title+"\n"+req.Text); ok {
			entry.NISTControlIDs = normalizeControls(seed.Controls)
			entry.Rationale = seed.Rationale
			entry.Confidence = ConfidenceHigh
			entry.SeedGroup = seed.Group
			if entry.Rationale == "" {
				entry.Rationale = "Seed crosswalk group: " + seed.Group
			}
		} else {
			entry.Rationale = "No seed crosswalk entry matched this requirement."
		}

		// A draft the reviewer already edited keeps its edits; only the seed
		// provenance is refreshed.
		if prev >= 0 && existing[prev].Edited() {
			edited := existing[prev]
			edited.SeedGroup = entry.SeedGroup
			out = append(out, edited)
			continue
		}
		out = append(out, entry)
	}

	// Carry through entries for requirements that are no longer present (for
	// example rejected at GATE 1) only when a human reviewed them, so the
	// audit trail survives a re-run.
	for _, e := range existing {
		if reqs.Index(e.ReqID) >= 0 && e.Source == SourceCurated {
			continue
		}
		if e.Source == SourceCurated && e.Status == requirement.StatusDraft {
			continue
		}
		out = append(out, e)
	}

	out.Sort()
	return out
}

// Edited reports whether a reviewer has modified this entry away from its
// generated form.
func (e Entry) Edited() bool {
	return e.ReviewedBy != "" || e.ReviewedAt != ""
}

func normalizeControls(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		n := strings.ToUpper(strings.TrimSpace(id))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return requirement.NaturalLess(out[i], out[j])
	})
	return out
}

// NormalizeControls exposes control-ID normalization (upper-cased, de-duped,
// naturally sorted) to callers outside this package.
func NormalizeControls(ids []string) []string { return normalizeControls(ids) }
