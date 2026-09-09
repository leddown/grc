// Package nfrenrich reviews the security documents in the AI agent's library
// and proposes enrichments to the security NFR catalog.
//
// The job: a document describes how encryption in transit is actually applied
// in some system; the NFR catalog has an encryption-in-transit entry whose
// Implementation field is thin. This module finds that passage, and proposes
// text for that field — as a *proposal*, which a human accepts or rejects.
//
// Three rules from POLICY_MODULE_FRAMEWORK.md are structural here rather than
// advisory, because they are what separates this from a plausible-sounding
// hallucination engine pointed at a compliance catalog:
//
//   - The model may draft language; it may not invent facts (§2.2). Every
//     proposal must cite chunks it was given, and a proposal citing anything
//     else is discarded on arrival rather than shown to a reviewer. The model
//     is composing prose from supplied source text, not recalling how
//     encryption in transit is usually done.
//   - Provenance on everything generated (§2.3). Each proposal records the
//     model that wrote it and a hash of the prompt that produced it, so a
//     model change makes its output findable and re-reviewable.
//   - AI drafts, humans approve (§2.1). Nothing in this package writes to the
//     NFR catalog except Accept, which is reached only from a review action.
//     Analysis of a thousand documents still changes no catalog row.
//
// Retrieval is lexical BM25 over section-aware chunks, per §2.4: no embeddings,
// no vector store, no new service. It runs in Go rather than through SQLite
// FTS5 because FTS5 is a build tag here (see internal/app/fts5_enabled.go) and
// a module that silently stops working on a binary built without the tag is
// worse than one that scores its own rows. Retriever is an interface so an
// FTS5-backed implementation can replace it without touching the analysis.
//
// The documents themselves are not held here. They are uploaded to an agent on
// the Wintermute server, which extracts and chunks them — with OCR behind it
// for scans, and LibreOffice for the office formats, neither of which this
// application had. Importing one copies its passages in so that retrieval,
// review and an accepted proposal's provenance keep working without that server
// being reachable; the original stays in the library that owns it.
package nfrenrich

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Errors surfaced to the handler.
var (
	ErrNotFound = errors.New("not found")
	// ErrNotConfigured means no model is available. Importing still works
	// without one; only analysis needs the model.
	ErrNotConfigured = errors.New("no AI provider is configured")
	// ErrEmptyDocument means the library holds no readable text for the
	// document — a scan whose OCR has not run, or has failed.
	ErrEmptyDocument = errors.New("the document contains no readable text")
	// ErrNoLibrary means there is no agent library to import from: no
	// Wintermute server configured, or no agent chosen. It is distinct from
	// ErrNotConfigured because the remedy is different — a model answers
	// questions, a library holds the documents — and a page that conflated them
	// would send someone to set an API key that changes nothing here.
	ErrNoLibrary = errors.New("no Wintermute document library is available")
	// ErrInvalid marks a failure the caller can fix by changing the request.
	// It exists so the handler can tell those apart from internal faults: an
	// unrecognised error is a 500 with a generic message, because a raw SQL or
	// filesystem error rendered into an API response is both a bad status code
	// and a disclosure.
	ErrInvalid = errors.New("invalid request")
)

// invalidf builds a caller-fixable error carrying its own message.
func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Proposal lifecycle.
//
// A proposal is superseded rather than deleted when its NFR field changes
// underneath it: the reviewer needs to see that a suggestion was overtaken,
// not find it silently gone.
const (
	StatusPending    = "pending"
	StatusAccepted   = "accepted"
	StatusRejected   = "rejected"
	StatusSuperseded = "superseded"
)

// ProposalStatuses is the ordered set used for validation and filters.
var ProposalStatuses = []string{StatusPending, StatusAccepted, StatusRejected, StatusSuperseded}

// Enrichable NFR fields.
//
// Only free-text fields a document can genuinely speak to are offered. Key, ID,
// IssueType, Domain and NISTMapping are catalog identity and taxonomy — an
// uploaded runbook has no standing to reassign an NFR's NIST mapping, and
// letting the model touch those is how a mapping error reaches an audit.
const (
	FieldImplementation    = "implementation"
	FieldAdditionalDetails = "additional_details"
	FieldDescription       = "description"
)

// EnrichableFields is the ordered set offered in the UI and accepted by the API.
var EnrichableFields = []string{FieldImplementation, FieldAdditionalDetails, FieldDescription}

// Document is one source imported from the agent's library.
type Document struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// LibraryDocID is this document's id in the agent's library on the
	// Wintermute server, which is where the original lives. It is what a link
	// back to the source is built from, and what a re-import recognises.
	LibraryDocID int64  `json:"library_doc_id"`
	URL          string `json:"url,omitempty"`
	Filename     string `json:"filename,omitempty"`
	MediaType    string `json:"media_type"`
	// SHA256 of the extracted text, not the raw bytes: the same PDF exported
	// twice should be recognised as the same document, and two files whose text
	// is identical have nothing different to say.
	SHA256   string `json:"sha256"`
	ByteSize int64  `json:"byte_size"`
	// ExtractVia names what read the text on that server — a PDF text layer,
	// OCR, LibreOffice. A proposal built on an OCR'd scan deserves to be read
	// differently from one built on a text layer, so it travels with the
	// document rather than being left behind in the library.
	ExtractVia string `json:"extract_via,omitempty"`
	ChunkCount int    `json:"chunk_count"`
	ImportedBy string `json:"imported_by"`
	CreatedAt  string `json:"created_at"`
}

// Chunk is one section-aware slice of a document.
//
// Heading carries the heading path ("Transport Security > TLS Configuration")
// rather than just the nearest heading, because a reviewer judging a proposal
// needs to know where in the document a claim came from, and a bare "TLS
// Configuration" appears in most security documents more than once.
type Chunk struct {
	ID         int64  `json:"id"`
	DocumentID int64  `json:"document_id"`
	Ordinal    int    `json:"ordinal"`
	Heading    string `json:"heading"`
	Text       string `json:"text"`
}

// Proposal is one suggested enrichment, awaiting a human decision.
type Proposal struct {
	ID            int64  `json:"id"`
	NFRKey        string `json:"nfr_key"`
	NFRSummary    string `json:"nfr_summary,omitempty"`
	DocumentID    int64  `json:"document_id"`
	DocumentTitle string `json:"document_title,omitempty"`
	Field         string `json:"field"`

	// SuggestedText is what would be written to Field on accept.
	SuggestedText string `json:"suggested_text"`
	// Rationale is the model's one-paragraph case for the enrichment. It is
	// shown to the reviewer and never written to the catalog.
	Rationale string `json:"rationale"`
	// Confidence is the model's own 0-1 estimate. Treated as a sort key for
	// reviewer attention, never as a threshold for skipping review.
	Confidence float64 `json:"confidence"`

	// Citations are the chunks this proposal is built from, validated against
	// what the model was actually given.
	Citations []Citation `json:"citations"`

	Status string `json:"status"`

	// Provenance (§2.3). Model is the exact model string; PromptHash is a
	// SHA-256 of the rendered prompt, so every proposal produced by one
	// prompt-and-model pairing can be found again after a model change.
	Model      string `json:"model"`
	PromptHash string `json:"prompt_hash"`

	CreatedAt   string `json:"created_at"`
	DecidedAt   string `json:"decided_at,omitempty"`
	DecidedBy   string `json:"decided_by,omitempty"`
	DecidedNote string `json:"decided_note,omitempty"`
}

// Citation is a resolved reference from a proposal back into the source.
type Citation struct {
	ChunkID int64  `json:"chunk_id"`
	Heading string `json:"heading"`
	// Quote is the span of chunk text the model leaned on. It is stored as the
	// model returned it and shown next to the suggestion, so a reviewer can
	// check the claim against the source without opening the document.
	Quote string `json:"quote"`
}

// AnalysisResult reports what one analysis run did. Skipped counts NFRs whose
// retrieval found nothing worth sending to the model — the common case for a
// narrow document against a broad catalog, and cheap.
type AnalysisResult struct {
	DocumentID int64      `json:"document_id"`
	Considered int        `json:"considered"`
	Skipped    int        `json:"skipped"`
	Proposed   int        `json:"proposed"`
	Discarded  int        `json:"discarded"`
	Proposals  []Proposal `json:"proposals"`
	// Warnings records per-NFR failures that did not stop the run: a model
	// error on one NFR should not discard the proposals already produced for
	// the others.
	Warnings []string `json:"warnings,omitempty"`
}

// marshalCitations and unmarshalCitations keep the JSON column handling in one
// place; a proposal with unreadable citations is returned with none rather than
// failing the whole list query, since the suggestion text is still reviewable.
func marshalCitations(cites []Citation) string {
	if len(cites) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(cites)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func unmarshalCitations(raw string) []Citation {
	if raw == "" {
		return nil
	}
	var out []Citation
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
