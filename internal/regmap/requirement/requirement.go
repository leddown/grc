// Package requirement holds the core domain type produced by ingestion: a
// single requirement extracted from a source framework, plus the review status
// vocabulary shared across the pipeline.
package requirement

import (
	"fmt"
	"sort"
	"strings"
)

// Status is the review state of any reviewable item. Nothing reaches
// StatusApproved except through the interactive review gates.
type Status string

const (
	StatusDraft    Status = "draft"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// Valid reports whether s is one of the three known states.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusApproved, StatusRejected:
		return true
	}
	return false
}

// Confidence describes how sure the segmenter was about a requirement boundary.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Rank orders confidence values so low-confidence items can be surfaced first.
func (c Confidence) Rank() int {
	switch c {
	case ConfidenceLow:
		return 0
	case ConfidenceMedium:
		return 1
	default:
		return 2
	}
}

// Unclassified is the category assigned when no classification rule matches.
// The tool never guesses a category silently.
const Unclassified = "unclassified"

// Requirement is one segmented unit of a source framework.
type Requirement struct {
	ID        string `yaml:"id"`
	Framework string `yaml:"framework"`
	// Section is the human-readable locator ("Article 5", "Annex I", "3.4.1").
	Section string `yaml:"section"`
	// Key is the normalized matching token derived from Section ("5", "I",
	// "3.4.1"). Profile matchers work against Key, never against Section.
	Key      string `yaml:"key"`
	Title    string `yaml:"title"`
	Category string `yaml:"category"`
	Text     string `yaml:"text"`
	Status   Status `yaml:"status"`

	// Segmentation metadata, carried for the audit trail and for ordering the
	// GATE 1 review queue.
	Strategy   string     `yaml:"strategy"`
	Confidence Confidence `yaml:"confidence"`
	// Flags records why the segmenter was unsure (short machine-ish strings).
	Flags []string `yaml:"flags,omitempty"`

	ReviewedBy string `yaml:"reviewedBy,omitempty"`
	ReviewedAt string `yaml:"reviewedAt,omitempty"`
	Notes      string `yaml:"notes,omitempty"`
}

// IsUnclassified reports whether the requirement still lacks a category.
func (r Requirement) IsUnclassified() bool {
	return r.Category == "" || r.Category == Unclassified
}

// Summary is a one-line description used in review prompts and reports.
func (r Requirement) Summary() string {
	title := r.Title
	if title == "" {
		title = "(no title)"
	}
	return fmt.Sprintf("%s  %s — %s", r.ID, r.Section, title)
}

// AddFlag appends a segmentation flag if it is not already present.
func (r *Requirement) AddFlag(flag string) {
	for _, f := range r.Flags {
		if f == flag {
			return
		}
	}
	r.Flags = append(r.Flags, flag)
}

// Set is a collection of requirements with deterministic ordering.
type Set []Requirement

// Sort orders requirements by ID using a natural ordering so that
// "DORA-ART-2" sorts before "DORA-ART-10" and diffs stay stable.
func (s Set) Sort() {
	sort.SliceStable(s, func(i, j int) bool {
		return NaturalLess(s[i].ID, s[j].ID)
	})
}

// Index returns the position of id in s, or -1.
func (s Set) Index(id string) int {
	for i := range s {
		if s[i].ID == id {
			return i
		}
	}
	return -1
}

// Approved returns only the requirements that a human signed off at GATE 1.
func (s Set) Approved() Set {
	var out Set
	for _, r := range s {
		if r.Status == StatusApproved {
			out = append(out, r)
		}
	}
	return out
}

// CountByStatus tallies requirements per status.
func (s Set) CountByStatus() map[Status]int {
	counts := map[Status]int{}
	for _, r := range s {
		counts[r.Status]++
	}
	return counts
}

// NaturalLess compares two identifiers so embedded digit runs compare
// numerically ("ART-2" < "ART-10") while the rest compares lexically.
func NaturalLess(a, b string) bool {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		ac, bc := a[ai], b[bi]
		if isDigit(ac) && isDigit(bc) {
			as, ae := ai, ai
			for ae < len(a) && isDigit(a[ae]) {
				ae++
			}
			bs, be := bi, bi
			for be < len(b) && isDigit(b[be]) {
				be++
			}
			an := strings.TrimLeft(a[as:ae], "0")
			bn := strings.TrimLeft(b[bs:be], "0")
			if len(an) != len(bn) {
				return len(an) < len(bn)
			}
			if an != bn {
				return an < bn
			}
			ai, bi = ae, be
			continue
		}
		if ac != bc {
			return ac < bc
		}
		ai++
		bi++
	}
	return len(a)-ai < len(b)-bi
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
