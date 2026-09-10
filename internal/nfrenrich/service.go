package nfrenrich

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"grc/internal/aiprovider"
	"grc/internal/securitynfr"
)

const (
	// maxChunksPerNFR bounds how much of a document is sent for one NFR. Beyond
	// roughly this many excerpts the signal from retrieval is gone and the
	// prompt is just the document again, which is both expensive and worse:
	// the model's job is to judge a shortlist, not to re-read everything.
	maxChunksPerNFR = 6
)

// NFRStore is the slice of the security NFR service this module needs. Keeping
// it narrow states the module's actual reach: it reads the catalog, and it
// writes exactly one thing, from exactly one place.
type NFRStore interface {
	List(search, domain string) ([]securitynfr.NFR, error)
	Get(key string) (securitynfr.NFR, error)
	Update(key string, nfr securitynfr.NFR) (securitynfr.NFR, error)
}

// LibraryFunc resolves the agent's document library, or says why there is
// none. Resolved per call rather than captured, so a Settings change applies
// without a restart — the same rule the AI provider harness follows.
type LibraryFunc func() (aiprovider.Library, error)

// Service orchestrates import, analysis and review.
type Service struct {
	repo      Repository
	nfrs      NFRStore
	analyzer  Analyzer
	retriever Retriever
	library   LibraryFunc
}

func NewService(repo Repository, nfrs NFRStore, analyzer Analyzer, library LibraryFunc) *Service {
	return &Service{
		repo:      repo,
		nfrs:      nfrs,
		analyzer:  analyzer,
		retriever: NewBM25Retriever(),
		library:   library,
	}
}

// Configured reports whether analysis is available. Importing and review work
// without a model; only the analysis step needs one.
func (s *Service) Configured() bool { return s.analyzer != nil && s.analyzer.Configured() }

// LibraryAvailable reports whether there is a library to import from, so the
// page can say what is missing rather than offering a button that fails.
func (s *Service) LibraryAvailable() bool {
	_, err := s.resolveLibrary()
	return err == nil
}

// LibraryURL is where the agent's library is managed, for a link out. Uploading
// and deleting happen there; this module only reads.
func (s *Service) LibraryURL() string {
	lib, err := s.resolveLibrary()
	if err != nil {
		return ""
	}
	return lib.LibraryURL()
}

func (s *Service) resolveLibrary() (aiprovider.Library, error) {
	if s.library == nil {
		return nil, ErrNoLibrary
	}
	lib, err := s.library()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoLibrary, err.Error())
	}
	return lib, nil
}

// Model reports the model analysis will run on, for the UI.
func (s *Service) Model() string {
	if s.analyzer == nil {
		return ""
	}
	return s.analyzer.Model()
}

// ---- documents ----

// ImportInput names a document in the agent's library to bring in.
type ImportInput struct {
	// LibraryDocID is the document's id on the Wintermute server. It is the
	// only way to name a document: there is no upload path here, because the
	// extraction that would follow one lives on that server.
	LibraryDocID int64
	// Title overrides the library's own title. Empty keeps it.
	Title      string
	ImportedBy string
}

// Import copies one library document's passages in, so they can be retrieved
// against the NFR catalog.
//
// What is copied is the text that server already extracted, not the file. The
// original stays in the library, which is what can re-read it when a better
// tool is installed — this module holds a reading of a document, and says which
// reading it was.
func (s *Service) Import(ctx context.Context, in ImportInput) (Document, error) {
	lib, err := s.resolveLibrary()
	if err != nil {
		return Document{}, err
	}
	if in.LibraryDocID <= 0 {
		return Document{}, invalidf("a library document is required")
	}

	// Checked before the read, which is the expensive part: importing the same
	// document twice produces two identical proposals for every NFR it matches,
	// doubling the review queue without adding a fact.
	if existing, err := s.repo.DocumentByLibraryID(in.LibraryDocID); err == nil {
		return Document{}, invalidf(
			"that document is already imported as %q (id %d)", existing.Title, existing.ID)
	}

	content, err := lib.ReadLibraryDocument(ctx, in.LibraryDocID)
	if err != nil {
		return Document{}, fmt.Errorf("read the document from wintermute: %w", err)
	}
	if !content.Document.Ready() {
		return Document{}, invalidf(
			"%q is still being read on the Wintermute server — import it once that has finished",
			content.Document.Title)
	}

	chunks := make([]Chunk, 0, len(content.Chunks))
	for _, c := range content.Chunks {
		if strings.TrimSpace(c.Body) == "" {
			continue
		}
		chunks = append(chunks, Chunk{
			Ordinal: len(chunks),
			Heading: strings.TrimSpace(c.Heading),
			Text:    c.Body,
		})
	}
	if len(chunks) == 0 {
		return Document{}, ErrEmptyDocument
	}

	sum := sha256.Sum256([]byte(content.Text))
	doc := Document{
		Title:        firstNonEmpty(strings.TrimSpace(in.Title), content.Document.Title, content.Document.Filename),
		LibraryDocID: content.Document.ID,
		URL:          content.Document.SourceURL,
		Filename:     content.Document.Filename,
		MediaType:    content.Document.MediaType,
		SHA256:       hex.EncodeToString(sum[:]),
		ByteSize:     content.Document.ByteSize,
		ExtractVia:   content.Document.ExtractVia,
		ImportedBy:   strings.TrimSpace(in.ImportedBy),
		CreatedAt:    nowStamp(),
	}
	return s.store(doc, chunks)
}

// ListLibrary lists what the agent's library holds, marking what is already
// imported so the picker does not offer the same document twice.
func (s *Service) ListLibrary(ctx context.Context) ([]LibraryEntry, error) {
	lib, err := s.resolveLibrary()
	if err != nil {
		return nil, err
	}
	docs, err := lib.LibraryDocuments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the wintermute library: %w", err)
	}

	imported, err := s.repo.ListDocuments()
	if err != nil {
		return nil, err
	}
	byLibraryID := make(map[int64]int64, len(imported))
	for _, doc := range imported {
		if doc.LibraryDocID > 0 {
			byLibraryID[doc.LibraryDocID] = doc.ID
		}
	}

	out := make([]LibraryEntry, 0, len(docs))
	for _, doc := range docs {
		entry := LibraryEntry{
			ID:         doc.ID,
			Title:      doc.Title,
			Filename:   doc.Filename,
			MediaType:  doc.MediaType,
			ByteSize:   doc.ByteSize,
			ChunkCount: doc.ChunkCount,
			ExtractVia: doc.ExtractVia,
			Ready:      doc.Ready(),
			ImportedAs: byLibraryID[doc.ID],
		}
		if doc.Processing != nil {
			entry.Processing = "being read"
			if doc.Processing.Failed {
				entry.Processing = "could not be read: " + doc.Processing.LastError
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// LibraryEntry is one library document as the import picker needs it.
type LibraryEntry struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Filename   string `json:"filename,omitempty"`
	MediaType  string `json:"media_type,omitempty"`
	ByteSize   int64  `json:"byte_size"`
	ChunkCount int    `json:"chunk_count"`
	ExtractVia string `json:"extract_via,omitempty"`
	Ready      bool   `json:"ready"`
	Processing string `json:"processing,omitempty"`
	// ImportedAs is the local document id when this one is already imported,
	// and zero when it is not.
	ImportedAs int64 `json:"imported_as,omitempty"`
}

// store rejects a re-import of identical text. Two library documents can be the
// same instrument exported twice, and two copies produce two identical
// proposals for every NFR they match — twice the review queue, no new facts.
func (s *Service) store(doc Document, chunks []Chunk) (Document, error) {
	if existing, err := s.repo.DocumentBySHA(doc.SHA256); err == nil {
		return Document{}, invalidf(
			"this document is already in the corpus as %q (id %d)", existing.Title, existing.ID)
	}
	doc.ChunkCount = len(chunks)
	return s.repo.CreateDocument(doc, chunks)
}

func (s *Service) ListDocuments() ([]Document, error) { return s.repo.ListDocuments() }

func (s *Service) GetDocument(id int64) (Document, error) { return s.repo.GetDocument(id) }

// DeleteDocument removes an imported source and supersedes anything still
// pending against it: a proposal whose evidence has been deleted cannot be
// reviewed, and leaving it pending asks someone to accept a claim they can no
// longer check. Accepted proposals are kept as provenance — see the repository.
//
// The library document itself is untouched. This removes a copy, not a source.
func (s *Service) DeleteDocument(id int64) error {
	pending, err := s.repo.ListProposals(ProposalFilter{Status: StatusPending, DocumentID: id})
	if err != nil {
		return err
	}
	now := nowStamp()
	for _, p := range pending {
		if err := s.repo.SetProposalDecision(
			p.ID, StatusSuperseded, now, "", "source document deleted"); err != nil {
			return err
		}
	}
	return s.repo.DeleteDocument(id)
}

// ---- analysis ----

// AnalyzeOptions selects what one run covers.
type AnalyzeOptions struct {
	DocumentID int64
	// Field is the NFR field to enrich. Empty means implementation, which is
	// the field this module exists for.
	Field string
	// Domain, when set, restricts the run to one NFR domain.
	Domain string
	// NFRKeys, when set, restricts the run to specific entries and overrides
	// Domain. This is how a reviewer re-runs one requirement after fixing it.
	NFRKeys []string
	// Limit caps how many NFRs are sent to the model in this run, after
	// retrieval has ranked them. Zero means no cap.
	Limit int
}

// Analyze reviews one document against the NFR catalog and records proposals.
//
// It writes nothing to the catalog. Every output is a pending proposal; the
// only path from here into a security NFR is AcceptProposal, which a person
// has to invoke.
func (s *Service) Analyze(ctx context.Context, opts AnalyzeOptions) (AnalysisResult, error) {
	if !s.Configured() {
		return AnalysisResult{}, ErrNotConfigured
	}

	field := strings.TrimSpace(opts.Field)
	if field == "" {
		field = FieldImplementation
	}
	if !containsString(EnrichableFields, field) {
		return AnalysisResult{}, invalidf("%q is not an enrichable field", field)
	}

	doc, err := s.repo.GetDocument(opts.DocumentID)
	if err != nil {
		return AnalysisResult{}, err
	}
	chunks, err := s.repo.ListChunks(doc.ID)
	if err != nil {
		return AnalysisResult{}, err
	}
	if len(chunks) == 0 {
		return AnalysisResult{}, ErrEmptyDocument
	}

	candidates, err := s.candidateNFRs(opts)
	if err != nil {
		return AnalysisResult{}, err
	}

	result := AnalysisResult{DocumentID: doc.ID, Proposals: []Proposal{}}

	// Retrieval first, for every candidate, so the run can be capped by
	// relevance rather than by catalog order — a Limit that took the first N
	// alphabetically would spend the budget on whatever starts with "A".
	type shortlisted struct {
		nfr    securitynfr.NFR
		chunks []ScoredChunk
		top    float64
	}
	var ranked []shortlisted
	for _, nfr := range candidates {
		result.Considered++
		hits := s.retriever.Rank(s.queryForNFR(nfr), chunks, maxChunksPerNFR)
		// ScoredChunk.Qualifies is the cheap filter that makes analysing one
		// document against a 700-entry catalog affordable: the overwhelming
		// majority of pairings are unrelated, and a model call to establish
		// that is a model call wasted.
		if len(hits) == 0 || !hits[0].Qualifies() {
			result.Skipped++
			continue
		}
		ranked = append(ranked, shortlisted{nfr: nfr, chunks: hits, top: hits[0].Score})
	}

	// Highest-scoring requirements first, so a Limit spends the run's budget
	// on the strongest matches rather than on whatever the catalog listed
	// first. Stable, so a re-run with the same corpus produces the same order.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].top > ranked[j].top })
	if opts.Limit > 0 && len(ranked) > opts.Limit {
		result.Skipped += len(ranked) - opts.Limit
		ranked = ranked[:opts.Limit]
	}

	for _, item := range ranked {
		if err := ctx.Err(); err != nil {
			result.Warnings = append(result.Warnings, "run cancelled before finishing")
			return result, nil
		}

		reply, err := s.analyzer.Analyze(ctx, AnalysisRequest{
			NFR: item.nfr, Field: field, Chunks: item.chunks,
		})
		if err != nil {
			// One NFR failing must not discard the proposals already produced
			// for the others: a run over a large catalog is expensive, and
			// throwing away twenty good proposals because the twenty-first
			// call timed out is the wrong trade.
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: %v", item.nfr.Key, err))
			continue
		}
		if !reply.Propose {
			result.Skipped++
			continue
		}

		citations, err := ValidateReply(reply, item.chunks)
		if err != nil {
			// This is the §2.2 gate doing its job. It is counted and surfaced
			// rather than silently dropped, because a rising discard rate is
			// how you find out a model or prompt change has gone wrong.
			result.Discarded++
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: discarded unverifiable proposal (%v)", item.nfr.Key, err))
			continue
		}

		proposal, err := s.repo.CreateProposal(Proposal{
			NFRKey:        item.nfr.Key,
			DocumentID:    doc.ID,
			Field:         field,
			SuggestedText: reply.SuggestedText,
			Rationale:     reply.Rationale,
			Confidence:    clampConfidence(reply.Confidence),
			Citations:     citations,
			Status:        StatusPending,
			Model:         reply.Model,
			PromptHash:    reply.PromptHash,
			CreatedAt:     nowStamp(),
		})
		if err != nil {
			return result, err
		}
		result.Proposed++
		result.Proposals = append(result.Proposals, proposal)
	}

	return result, nil
}

// candidateNFRs resolves which catalog entries a run covers.
func (s *Service) candidateNFRs(opts AnalyzeOptions) ([]securitynfr.NFR, error) {
	if len(opts.NFRKeys) > 0 {
		out := make([]securitynfr.NFR, 0, len(opts.NFRKeys))
		for _, key := range opts.NFRKeys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			nfr, err := s.nfrs.Get(key)
			if err != nil {
				return nil, fmt.Errorf("security NFR %q: %w", key, err)
			}
			out = append(out, nfr)
		}
		return out, nil
	}
	return s.nfrs.List("", strings.TrimSpace(opts.Domain))
}

// queryForNFR builds the retrieval query.
//
// The "!" prefix marks a high-signal line for queryTerms. Summary and the NIST
// mapping get it: the summary is the requirement's distinctive name, and the
// mapping carries control identifiers ("SC-8", "AC-2") that appear verbatim in
// well-written security documents and are among the most precise signals
// available. The description is included unweighted — it adds recall, and
// weighting it equally would let its shared vocabulary dominate.
func (s *Service) queryForNFR(nfr securitynfr.NFR) string {
	var b strings.Builder
	b.WriteString("!")
	b.WriteString(nfr.Summary)
	b.WriteString("\n!")
	b.WriteString(nfr.NISTMapping)
	b.WriteString("\n")
	b.WriteString(nfr.Description)
	b.WriteString("\n")
	b.WriteString(nfr.Domain)
	return b.String()
}

// ---- review ----

func (s *Service) ListProposals(filter ProposalFilter) ([]Proposal, error) {
	if filter.Status != "" && !containsString(ProposalStatuses, filter.Status) {
		return nil, invalidf("unknown status %q", filter.Status)
	}
	return s.repo.ListProposals(filter)
}

func (s *Service) GetProposal(id int64) (Proposal, error) { return s.repo.GetProposal(id) }

// AcceptProposal is the only path in this module that writes to the security
// NFR catalog.
//
// The reviewer may pass editedText to accept a corrected version of the
// suggestion. That is the normal case, not an escape hatch: "AI drafts, humans
// approve" means the human's edit is the authored text, and forcing an
// all-or-nothing decision would push reviewers into accepting text they would
// have improved. What is stored as the proposal's suggested_text is updated to
// what was actually written, so the provenance record matches the catalog.
func (s *Service) AcceptProposal(id int64, editedText, decidedBy string) (Proposal, error) {
	proposal, err := s.repo.GetProposal(id)
	if err != nil {
		return Proposal{}, err
	}
	if proposal.Status != StatusPending {
		return Proposal{}, invalidf("this proposal is already %s", proposal.Status)
	}

	text := strings.TrimSpace(editedText)
	if text == "" {
		text = strings.TrimSpace(proposal.SuggestedText)
	}
	if text == "" {
		return Proposal{}, invalidf("there is no text to apply")
	}

	nfr, err := s.nfrs.Get(proposal.NFRKey)
	if err != nil {
		return Proposal{}, fmt.Errorf("security NFR %q: %w", proposal.NFRKey, err)
	}
	updated, err := applyField(nfr, proposal.Field, text)
	if err != nil {
		return Proposal{}, err
	}
	if _, err := s.nfrs.Update(proposal.NFRKey, updated); err != nil {
		return Proposal{}, fmt.Errorf("updating security NFR %q: %w", proposal.NFRKey, err)
	}

	now := nowStamp()
	if err := s.repo.SetProposalDecision(id, StatusAccepted, now, decidedBy, ""); err != nil {
		return Proposal{}, err
	}
	// Best-effort: the catalog write and the decision are already committed,
	// and failing to tidy the siblings must not report the accept as failed.
	_, _ = s.repo.SupersedePending(proposal.NFRKey, proposal.Field, id, now)

	proposal.SuggestedText = text
	proposal.Status = StatusAccepted
	proposal.DecidedAt = now
	proposal.DecidedBy = decidedBy
	return proposal, nil
}

// RejectProposal records a decision without touching the catalog. The note is
// worth capturing: "the document describes a different system" is the kind of
// reason that should stop the same suggestion being re-accepted next quarter.
func (s *Service) RejectProposal(id int64, note, decidedBy string) (Proposal, error) {
	proposal, err := s.repo.GetProposal(id)
	if err != nil {
		return Proposal{}, err
	}
	if proposal.Status != StatusPending {
		return Proposal{}, invalidf("this proposal is already %s", proposal.Status)
	}

	now := nowStamp()
	if err := s.repo.SetProposalDecision(
		id, StatusRejected, now, decidedBy, strings.TrimSpace(note)); err != nil {
		return Proposal{}, err
	}
	proposal.Status = StatusRejected
	proposal.DecidedAt = now
	proposal.DecidedBy = decidedBy
	proposal.DecidedNote = strings.TrimSpace(note)
	return proposal, nil
}

// ---- helpers ----

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func containsString(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

func clampConfidence(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
