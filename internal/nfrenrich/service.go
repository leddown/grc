package nfrenrich

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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

// Service orchestrates ingestion, analysis and review.
type Service struct {
	repo      Repository
	nfrs      NFRStore
	analyzer  Analyzer
	retriever Retriever
	fetcher   *Fetcher
}

func NewService(repo Repository, nfrs NFRStore, analyzer Analyzer) *Service {
	return &Service{
		repo:      repo,
		nfrs:      nfrs,
		analyzer:  analyzer,
		retriever: NewBM25Retriever(),
		fetcher:   NewFetcher(),
	}
}

// Configured reports whether analysis is available. Ingestion and review work
// without a key; only the analysis step needs one.
func (s *Service) Configured() bool { return s.analyzer != nil && s.analyzer.Configured() }

// Model reports the model analysis will run on, for the UI.
func (s *Service) Model() string {
	if s.analyzer == nil {
		return ""
	}
	return s.analyzer.Model()
}

// ---- documents ----

// UploadInput is a document supplied as bytes.
type UploadInput struct {
	Title      string
	Filename   string
	MediaType  string
	Body       []byte
	UploadedBy string
}

// AddUpload ingests an uploaded document.
func (s *Service) AddUpload(in UploadInput) (Document, error) {
	doc, chunks, err := PrepareDocument(Document{
		Title:      strings.TrimSpace(in.Title),
		Origin:     OriginUpload,
		Filename:   strings.TrimSpace(in.Filename),
		MediaType:  strings.TrimSpace(in.MediaType),
		UploadedBy: strings.TrimSpace(in.UploadedBy),
		CreatedAt:  nowStamp(),
	}, in.Body)
	if err != nil {
		return Document{}, err
	}
	return s.store(doc, chunks)
}

// AddURL fetches and ingests a web document.
func (s *Service) AddURL(ctx context.Context, url, title, uploadedBy string) (Document, error) {
	body, mediaType, err := s.fetcher.Fetch(ctx, url)
	if err != nil {
		return Document{}, err
	}
	doc, chunks, err := PrepareDocument(Document{
		Title:      strings.TrimSpace(title),
		Origin:     OriginURL,
		URL:        strings.TrimSpace(url),
		MediaType:  mediaType,
		UploadedBy: strings.TrimSpace(uploadedBy),
		CreatedAt:  nowStamp(),
	}, body)
	if err != nil {
		return Document{}, err
	}
	return s.store(doc, chunks)
}

// store rejects a re-ingest of identical text. Two copies of one document
// produce two identical proposals for every NFR it matches, which doubles the
// review queue without adding a single new fact.
func (s *Service) store(doc Document, chunks []Chunk) (Document, error) {
	if existing, err := s.repo.DocumentBySHA(doc.SHA256); err == nil {
		return Document{}, invalidf(
			"this document is already in the corpus as %q (id %d)", existing.Title, existing.ID)
	}
	return s.repo.CreateDocument(doc, chunks)
}

func (s *Service) ListDocuments() ([]Document, error) { return s.repo.ListDocuments() }

func (s *Service) GetDocument(id int64) (Document, error) { return s.repo.GetDocument(id) }

// DeleteDocument removes a source and supersedes anything still pending
// against it: a proposal whose evidence has been deleted cannot be reviewed,
// and leaving it pending asks someone to accept a claim they can no longer
// check. Accepted proposals are kept as provenance — see the repository.
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
