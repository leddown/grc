// Package regcoverage analyses an uploaded regulation against this
// installation's compliance vocabulary: the Security NFR catalog and NIST SP
// 800-53. It is the web counterpart to the regmap CLI, and reuses that tool's
// extraction, segmentation and framework profiles rather than growing a second
// implementation of them.
//
// What it adds over regmap is the part a crosswalk alone does not answer. For
// each article it records whether the article is security-relevant at all, what
// it actually requires, which NFRs and controls satisfy it, what is missing,
// and a piece of practitioner commentary on how to meet it well. The result is
// a report: an immutable version, downloadable as PDF, that a reviewer can then
// interrogate and correct through a conversation with the model, each accepted
// correction writing the next version.
//
// Three rules shape the design:
//
//   - Nothing the model says is presented as fact without provenance. Every
//     finding records the model that produced it, the hash of the prompt it
//     answered, and its own confidence; every mapping records whether it came
//     from a curated seed crosswalk or from the model.
//   - The model judges a shortlist, it does not browse the catalog. Candidate
//     NFRs and controls are retrieved lexically first, which keeps the prompt
//     small enough to reason over and the mapping traceable to why a candidate
//     was considered.
//   - A report that has been circulated cannot silently change. Revisions
//     append a version; they never overwrite one.
package regcoverage

import (
	"fmt"
	"strings"
)

// Regulation is one uploaded instrument.
type Regulation struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Framework is the regmap profile that segmented it ("dora", "nis2", or
	// "eu-generic" when nothing matched), and FrameworkName its display name.
	Framework     string `json:"framework"`
	FrameworkName string `json:"framework_name"`
	SourceRef     string `json:"source_ref"`
	// Detected records whether the framework was recognised from the document
	// or fell back to the generic profile, because that materially changes how
	// much curated knowledge went into the analysis.
	Detected  bool   `json:"detected"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	ByteSize  int64  `json:"byte_size"`
	// ExtractMethod names what read the text (pdftotext, the pure-Go reader, a
	// direct read), which is the first thing to check when segmentation looks
	// wrong.
	ExtractMethod string   `json:"extract_method"`
	ExtractNotes  []string `json:"extract_notes,omitempty"`
	TextChars     int      `json:"text_chars"`
	Status        string   `json:"status"`
	StatusDetail  string   `json:"status_detail,omitempty"`
	SectionCount  int      `json:"section_count"`
	UploadedBy    string   `json:"uploaded_by"`
	CreatedAt     string   `json:"created_at"`
	AnalyzedAt    string   `json:"analyzed_at,omitempty"`
	// LatestVersion is 0 until the first analysis completes.
	LatestVersion int `json:"latest_version"`
}

// Regulation statuses.
const (
	StatusIngested  = "ingested"
	StatusAnalyzing = "analyzing"
	StatusAnalyzed  = "analyzed"
	StatusFailed    = "failed"
)

// Section is one article, annex or numbered clause of a regulation.
type Section struct {
	ID           int64  `json:"id"`
	RegulationID int64  `json:"regulation_id"`
	Ref          string `json:"ref"`
	Label        string `json:"label"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	Body         string `json:"body"`
	Position     int    `json:"position"`
	// Confidence is the segmenter's, not the model's: it flags sections whose
	// boundaries look wrong, which is worth knowing before trusting a finding
	// built on them.
	Confidence string `json:"confidence"`
}

// Finding is the model's analysis of one section.
type Finding struct {
	ID           int64  `json:"id"`
	RegulationID int64  `json:"regulation_id"`
	SectionID    int64  `json:"section_id"`
	SectionRef   string `json:"section_ref"`
	// Relevant reports whether the section imposes a security or resilience
	// obligation at all. Definitions, scope and procedural articles do not, and
	// saying so is more useful than mapping them to something tangential.
	Relevant bool `json:"relevant"`
	// Requirement is what the section demands, in plain terms.
	Requirement string `json:"requirement"`
	// Commentary is the practitioner note: how this is met well, what auditors
	// look for, where implementations usually fall short.
	Commentary string `json:"commentary"`
	// Gaps is what the catalog does not cover for this section.
	Gaps string `json:"gaps"`
	// Quote is verbatim from the section body, and is validated against it. It
	// is the grounding check: a finding that cannot quote the text it claims to
	// analyse is flagged rather than trusted.
	Quote      string `json:"quote"`
	Grounded   bool   `json:"grounded"`
	Confidence string `json:"confidence"`
	Model      string `json:"model"`
	PromptHash string `json:"prompt_hash"`
	// Revision is 0 for the original analysis and increments each time this
	// finding is revised from reviewer feedback.
	Revision  int    `json:"revision"`
	CreatedAt string `json:"created_at"`

	Mappings []Mapping `json:"mappings"`
}

// Confidence values, ordered.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Mapping ties a section to one catalog item.
type Mapping struct {
	ID        int64 `json:"id"`
	FindingID int64 `json:"finding_id"`
	// Kind is KindControl or KindNFR.
	Kind string `json:"kind"`
	// Ref is the control ID ("AC-2") or the NFR key.
	Ref        string `json:"ref"`
	Title      string `json:"title"`
	Rationale  string `json:"rationale"`
	Confidence string `json:"confidence"`
	// Source distinguishes a curated seed crosswalk hit from a model
	// suggestion, because they warrant different levels of scrutiny.
	Source string `json:"source"`
	// Known reports whether Ref was found in the catalog. A model that invents
	// a control ID is caught here rather than in front of a client.
	Known bool `json:"known"`
}

// Mapping kinds and sources.
const (
	KindControl = "control"
	KindNFR     = "nfr"

	SourceSeed  = "seed"
	SourceModel = "model"
)

// Version is an immutable snapshot of a report.
type Version struct {
	ID           int64  `json:"id"`
	RegulationID int64  `json:"regulation_id"`
	Number       int    `json:"number"`
	Summary      string `json:"summary"`
	// Note says why this version exists: the initial analysis, or the feedback
	// that produced it.
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	// Snapshot is the full report as JSON at the moment the version was cut, so
	// a circulated report can still be reproduced after later revisions.
	Snapshot string `json:"-"`
}

// ChatTurn is one message in the conversation about a report.
type ChatTurn struct {
	ID           int64 `json:"id"`
	RegulationID int64 `json:"regulation_id"`
	// Version records which report version the turn was about.
	Version int    `json:"version"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Model   string `json:"model"`
	Actor   string `json:"actor,omitempty"`
	// SessionID carries a provider-held conversation (Wintermute) across turns.
	SessionID string `json:"-"`
	CreatedAt string `json:"created_at"`
}

// Chat roles, matching aiprovider's.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Report is a regulation, its sections and findings, and the version they were
// snapshotted at. It is what the page, the PDF and the chat context are all
// built from.
type Report struct {
	Regulation Regulation `json:"regulation"`
	Version    Version    `json:"version"`
	Sections   []Entry    `json:"sections"`
	Stats      Stats      `json:"stats"`
	// GeneratedAt is when this rendering happened, not when the version was
	// cut; the version carries that.
	GeneratedAt string `json:"generated_at"`
}

// Entry pairs a section with its finding, which is how the report reads.
type Entry struct {
	Section Section  `json:"section"`
	Finding *Finding `json:"finding,omitempty"`
}

// Stats is the coverage arithmetic shown at the top of the report.
type Stats struct {
	Sections        int `json:"sections"`
	Analyzed        int `json:"analyzed"`
	Relevant        int `json:"relevant"`
	Mapped          int `json:"mapped"`
	Unmapped        int `json:"unmapped"`
	NotRelevant     int `json:"not_relevant"`
	ControlMappings int `json:"control_mappings"`
	NFRMappings     int `json:"nfr_mappings"`
	LowConfidence   int `json:"low_confidence"`
	Ungrounded      int `json:"ungrounded"`
	UnknownRefs     int `json:"unknown_refs"`
	// DistinctControls and DistinctNFRs count the catalog items this regulation
	// touches, which is the number that answers "how much of our catalog does
	// this regulation reach".
	DistinctControls int `json:"distinct_controls"`
	DistinctNFRs     int `json:"distinct_nfrs"`
}

// CoveragePercent is the share of security-relevant sections that mapped to at
// least one catalog item. It returns 0 when nothing is relevant, rather than
// dividing by zero to claim full coverage of nothing.
func (s Stats) CoveragePercent() int {
	if s.Relevant == 0 {
		return 0
	}
	return int(float64(s.Mapped) / float64(s.Relevant) * 100)
}

// ---- errors ----

// ErrNotFound is returned when a regulation, section or version does not exist.
type ErrNotFound struct{ What string }

func (e ErrNotFound) Error() string { return e.What + " not found" }

// ErrInvalid reports a caller mistake — a bad upload, an empty question.
type ErrInvalid struct{ Message string }

func (e ErrInvalid) Error() string { return e.Message }

func invalid(message string) error { return ErrInvalid{Message: message} }

func invalidf(format string, args ...any) error {
	return ErrInvalid{Message: fmt.Sprintf(format, args...)}
}

func notFound(what string) error { return ErrNotFound{What: what} }

// normalizeConfidence keeps the model's answer inside the vocabulary the report
// renders, defaulting to medium rather than inventing a value.
func normalizeConfidence(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ConfidenceHigh:
		return ConfidenceHigh
	case ConfidenceLow:
		return ConfidenceLow
	default:
		return ConfidenceMedium
	}
}
