package nfrenrich

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"grc/internal/aiprovider"
	"grc/internal/securitynfr"
)

// fakeLibrary stands in for the agent's document library on the Wintermute
// server, counting reads so a test can assert that one did not happen.
type fakeLibrary struct {
	docs  map[int64]aiprovider.LibraryContent
	reads int
	err   error
}

func (f *fakeLibrary) provide() (aiprovider.Library, error) { return f, f.err }

func (f *fakeLibrary) LibraryURL() string { return "https://wintermute.example/agents/acme" }

func (f *fakeLibrary) LibraryDocuments(context.Context) ([]aiprovider.LibraryDocument, error) {
	out := []aiprovider.LibraryDocument{}
	for _, content := range f.docs {
		out = append(out, content.Document)
	}
	return out, nil
}

func (f *fakeLibrary) ReadLibraryDocument(_ context.Context, id int64) (aiprovider.LibraryContent, error) {
	f.reads++
	content, ok := f.docs[id]
	if !ok {
		return aiprovider.LibraryContent{}, fmt.Errorf("no document %d", id)
	}
	return content, nil
}

// libraryDoc builds one ready library document out of a single passage.
func libraryDoc(id int64, filename, body string) aiprovider.LibraryContent {
	return aiprovider.LibraryContent{
		Document: aiprovider.LibraryDocument{
			ID: id, Title: filename, Filename: filename, MediaType: "text/markdown",
			ByteSize: int64(len(body)), TextChars: len(body), ChunkCount: 1,
			ExtractVia: "text layer",
		},
		Chunks: []aiprovider.LibraryChunk{{Ordinal: 0, Body: body}},
		Text:   body,
	}
}

func TestBM25RanksTheRelevantSectionFirst(t *testing.T) {
	chunks := []Chunk{
		{ID: 1, Ordinal: 0, Heading: "Standard > Data at Rest",
			Text: "Volumes are encrypted with AES-256 and keys are held in the managed KMS."},
		{ID: 2, Ordinal: 1, Heading: "Standard > Encryption in Transit",
			Text: "All external endpoints negotiate TLS 1.2 or higher. Plaintext HTTP is redirected."},
		{ID: 3, Ordinal: 2, Heading: "Standard > Access Reviews",
			Text: "Entitlements are reviewed quarterly by the system owner."},
	}

	hits := NewBM25Retriever().Rank("!Encryption of data in transit\nTLS must be enforced", chunks, 3)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Chunk.ID != 2 {
		t.Fatalf("top hit is chunk %d, want 2 (scores: %v)", hits[0].Chunk.ID, scoreList(hits))
	}
	if !containsSubstring(hits[0].Matched, "transit") {
		t.Fatalf("matched terms do not explain the hit: %v", hits[0].Matched)
	}

	// An unrelated requirement must not qualify. It will still *score* — the
	// word "access" appears in an unrelated heading — which is exactly why
	// Qualifies asks what matched rather than trusting the number.
	got := NewBM25Retriever().Rank("!Physical badge access to the data centre", chunks, 3)
	for _, hit := range got {
		if hit.Qualifies() {
			t.Fatalf("an unrelated requirement qualified on chunk %d (matched %v, strong %v, score %.2f)",
				hit.Chunk.ID, hit.Matched, hit.MatchedStrong, hit.Score)
		}
	}
}

func TestQualifiesRequiresAStrongTermAndCorroboration(t *testing.T) {
	tests := []struct {
		name string
		hit  ScoredChunk
		want bool
	}{
		{"two terms, one strong", ScoredChunk{
			Score: 2.0, Matched: []string{"encryption", "transit"}, MatchedStrong: []string{"transit"},
		}, true},
		{"two terms, none strong", ScoredChunk{
			Score: 9.0, Matched: []string{"data", "access"},
		}, false},
		{"lone control identifier, weak score", ScoredChunk{
			Score: 2.0, Matched: []string{"sc-8"}, MatchedStrong: []string{"sc-8"},
		}, false},
		{"lone control identifier, strong score", ScoredChunk{
			Score: 4.0, Matched: []string{"sc-8"}, MatchedStrong: []string{"sc-8"},
		}, true},
		{"lone ordinary word, however high it scores", ScoredChunk{
			Score: 99.0, Matched: []string{"access"}, MatchedStrong: []string{"access"},
		}, false},
		{"two terms, strong, below the floor", ScoredChunk{
			Score: 0.4, Matched: []string{"encryption", "transit"}, MatchedStrong: []string{"transit"},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hit.Qualifies(); got != tt.want {
				t.Fatalf("Qualifies() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBM25HeadingCountsTowardTheMatch(t *testing.T) {
	chunks := []Chunk{
		{ID: 1, Ordinal: 0, Heading: "Policy > Logging",
			Text: "Events are shipped to the central collector within one minute."},
		{ID: 2, Ordinal: 1, Heading: "Policy > Encryption in Transit",
			Text: "See the platform standard for the approved parameters."},
	}
	hits := NewBM25Retriever().Rank("!encryption in transit", chunks, 2)
	if len(hits) == 0 || hits[0].Chunk.ID != 2 {
		t.Fatalf("heading text did not contribute to the score: %v", scoreList(hits))
	}
}

// ---- the citation gate ----

func TestValidateReplyAcceptsAVerbatimQuote(t *testing.T) {
	chunks := []ScoredChunk{{Chunk: Chunk{
		ID: 7, Heading: "Standard > Encryption in Transit",
		Text: "All external endpoints negotiate TLS 1.2 or higher.\nPlaintext HTTP is redirected to HTTPS.",
	}}}

	cites, err := ValidateReply(AnalysisReply{
		Propose:       true,
		SuggestedText: "External endpoints negotiate TLS 1.2 or higher; plaintext HTTP is redirected.",
		Citations: []replyCitation{
			// Whitespace differs from the source, which is the normal harmless
			// difference when a model collapses a line wrap.
			{Excerpt: 1, Quote: "negotiate TLS 1.2   or higher"},
		},
	}, chunks)
	if err != nil {
		t.Fatalf("ValidateReply rejected a genuine quote: %v", err)
	}
	if len(cites) != 1 || cites[0].ChunkID != 7 {
		t.Fatalf("citations = %+v", cites)
	}
}

// This is the §2.2 gate: prose the model composed from its own knowledge cannot
// produce a verbatim span of a document that does not contain it.
func TestValidateReplyRejectsAFabricatedQuote(t *testing.T) {
	chunks := []ScoredChunk{{Chunk: Chunk{
		ID: 7, Heading: "Standard > Encryption in Transit",
		Text: "All external endpoints negotiate TLS 1.2 or higher.",
	}}}

	_, err := ValidateReply(AnalysisReply{
		Propose:       true,
		SuggestedText: "Keys are rotated every 90 days using the corporate HSM.",
		Citations: []replyCitation{
			{Excerpt: 1, Quote: "keys are rotated every 90 days using the corporate HSM"},
		},
	}, chunks)
	if err == nil {
		t.Fatal("a fabricated quote passed validation")
	}
	if !strings.Contains(err.Error(), "does not appear") {
		t.Fatalf("error does not name the failure: %v", err)
	}
}

func TestValidateReplyRejectsUnsuppliedAndEmptyCitations(t *testing.T) {
	chunks := []ScoredChunk{{Chunk: Chunk{ID: 7, Text: "TLS 1.2 is the minimum."}}}

	tests := []struct {
		name  string
		reply AnalysisReply
	}{
		{"out of range excerpt", AnalysisReply{
			SuggestedText: "x",
			Citations:     []replyCitation{{Excerpt: 4, Quote: "TLS 1.2"}},
		}},
		{"zero excerpt", AnalysisReply{
			SuggestedText: "x",
			Citations:     []replyCitation{{Excerpt: 0, Quote: "TLS 1.2"}},
		}},
		{"no citations at all", AnalysisReply{SuggestedText: "x"}},
		{"empty quote", AnalysisReply{
			SuggestedText: "x",
			Citations:     []replyCitation{{Excerpt: 1, Quote: "   "}},
		}},
		{"no suggested text", AnalysisReply{
			Citations: []replyCitation{{Excerpt: 1, Quote: "TLS 1.2"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ValidateReply(tt.reply, chunks); err == nil {
				t.Fatal("validation unexpectedly passed")
			}
		})
	}
}

func TestParseReply(t *testing.T) {
	declined, err := ParseReply(`{"propose": false, "reason": "unrelated"}`)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	if declined.Propose {
		t.Fatal("a declining reply parsed as a proposal")
	}

	fenced, err := ParseReply("```json\n{\"propose\": true, \"suggested_text\": \"x\", \"confidence\": 0.8}\n```")
	if err != nil {
		t.Fatalf("ParseReply with fence: %v", err)
	}
	if !fenced.Propose || fenced.SuggestedText != "x" || fenced.Confidence != 0.8 {
		t.Fatalf("fenced reply parsed wrong: %+v", fenced)
	}

	if _, err := ParseReply("I think this document covers encryption."); err == nil {
		t.Fatal("prose parsed as JSON")
	}
}

func TestBuildPromptCarriesCurrentFieldAndNumbersExcerpts(t *testing.T) {
	prompt := BuildPrompt(AnalysisRequest{
		NFR: securitynfr.NFR{
			Key: "NFR-1", Summary: "Encryption in transit",
			Implementation: "TBD", NISTMapping: "SC-8",
		},
		Field: FieldImplementation,
		Chunks: []ScoredChunk{
			{Chunk: Chunk{ID: 1, Heading: "A", Text: "first"}},
			{Chunk: Chunk{ID: 2, Heading: "B", Text: "second"}},
		},
	})

	for _, want := range []string{"NFR-1", "SC-8", "implementation", "TBD", "## Excerpt 1", "## Excerpt 2"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// The model is told what the field already says so it can preserve what
	// should remain; without it every accepted proposal silently truncates.
	if strings.Contains(prompt, "(empty)") {
		t.Fatalf("non-empty field reported as empty:\n%s", prompt)
	}
}

// ---- SSRF guard ----

// ---- the review workflow ----

func TestAcceptProposalWritesTheFieldAndSupersedesSiblings(t *testing.T) {
	repo := newFakeRepo()
	nfrs := newFakeNFRs(securitynfr.NFR{
		Key: "NFR-1", Summary: "Encryption in transit", Implementation: "TBD",
	})
	svc := NewService(repo, nfrs, &stubAnalyzer{}, nil)

	accepted, _ := repo.CreateProposal(Proposal{
		NFRKey: "NFR-1", DocumentID: 1, Field: FieldImplementation,
		SuggestedText: "TLS 1.2 minimum on all external endpoints.", Status: StatusPending,
	})
	sibling, _ := repo.CreateProposal(Proposal{
		NFRKey: "NFR-1", DocumentID: 2, Field: FieldImplementation,
		SuggestedText: "Something else.", Status: StatusPending,
	})

	got, err := svc.AcceptProposal(accepted.ID, "", "reviewer@example.com")
	if err != nil {
		t.Fatalf("AcceptProposal: %v", err)
	}
	if got.Status != StatusAccepted || got.DecidedBy != "reviewer@example.com" {
		t.Fatalf("decision not recorded: %+v", got)
	}

	updated, _ := nfrs.Get("NFR-1")
	if updated.Implementation != "TLS 1.2 minimum on all external endpoints." {
		t.Fatalf("implementation field = %q", updated.Implementation)
	}
	// Only the named field moves. A proposal for implementation must not
	// rewrite the requirement's own description.
	if updated.Summary != "Encryption in transit" {
		t.Fatalf("accept touched a field it was not for: %+v", updated)
	}

	after, _ := repo.GetProposal(sibling.ID)
	if after.Status != StatusSuperseded {
		t.Fatalf("sibling pending proposal is still %q", after.Status)
	}
}

func TestAcceptProposalUsesTheReviewersEdit(t *testing.T) {
	repo := newFakeRepo()
	nfrs := newFakeNFRs(securitynfr.NFR{Key: "NFR-1"})
	svc := NewService(repo, nfrs, &stubAnalyzer{}, nil)

	p, _ := repo.CreateProposal(Proposal{
		NFRKey: "NFR-1", Field: FieldImplementation,
		SuggestedText: "model text", Status: StatusPending,
	})

	got, err := svc.AcceptProposal(p.ID, "  reviewer's corrected text  ", "reviewer")
	if err != nil {
		t.Fatalf("AcceptProposal: %v", err)
	}

	updated, _ := nfrs.Get("NFR-1")
	if updated.Implementation != "reviewer's corrected text" {
		t.Fatalf("the reviewer's edit was not what got written: %q", updated.Implementation)
	}
	// The stored proposal has to match what was actually applied, or the
	// provenance record describes text that is not in the catalog.
	if got.SuggestedText != "reviewer's corrected text" {
		t.Fatalf("returned proposal still shows the model's text: %q", got.SuggestedText)
	}
}

func TestRejectProposalLeavesTheCatalogAlone(t *testing.T) {
	repo := newFakeRepo()
	nfrs := newFakeNFRs(securitynfr.NFR{Key: "NFR-1", Implementation: "original"})
	svc := NewService(repo, nfrs, &stubAnalyzer{}, nil)

	p, _ := repo.CreateProposal(Proposal{
		NFRKey: "NFR-1", Field: FieldImplementation,
		SuggestedText: "replacement", Status: StatusPending,
	})

	if _, err := svc.RejectProposal(p.ID, "describes a different system", "reviewer"); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	updated, _ := nfrs.Get("NFR-1")
	if updated.Implementation != "original" {
		t.Fatalf("reject wrote to the catalog: %q", updated.Implementation)
	}
	if nfrs.updates != 0 {
		t.Fatalf("reject called Update %d times", nfrs.updates)
	}
}

func TestDecidingTwiceIsRefused(t *testing.T) {
	repo := newFakeRepo()
	nfrs := newFakeNFRs(securitynfr.NFR{Key: "NFR-1"})
	svc := NewService(repo, nfrs, &stubAnalyzer{}, nil)

	p, _ := repo.CreateProposal(Proposal{
		NFRKey: "NFR-1", Field: FieldImplementation,
		SuggestedText: "text", Status: StatusPending,
	})
	if _, err := svc.AcceptProposal(p.ID, "", "reviewer"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptProposal(p.ID, "", "reviewer"); err == nil {
		t.Fatal("a decided proposal was accepted a second time")
	}
	if _, err := svc.RejectProposal(p.ID, "", "reviewer"); err == nil {
		t.Fatal("a decided proposal was rejected after acceptance")
	}
}

// Analysis produces proposals and nothing else. This is the rule the whole
// module rests on, so it is asserted directly rather than inferred.
func TestAnalyzeNeverWritesToTheCatalog(t *testing.T) {
	repo := newFakeRepo()
	doc, _ := repo.CreateDocument(Document{Title: "Standard"}, []Chunk{
		{ID: 1, Ordinal: 0, Heading: "Encryption in Transit",
			Text: "All external endpoints negotiate TLS 1.2 or higher at the edge."},
	})
	nfrs := newFakeNFRs(securitynfr.NFR{
		Key: "NFR-1", Summary: "Encryption in transit", NISTMapping: "SC-8",
	})

	svc := NewService(repo, nfrs, &stubAnalyzer{reply: AnalysisReply{
		Propose:       true,
		SuggestedText: "External endpoints negotiate TLS 1.2 or higher.",
		Confidence:    0.9,
		Model:         "test-model",
		PromptHash:    "abc123",
		Citations:     []replyCitation{{Excerpt: 1, Quote: "negotiate TLS 1.2 or higher"}},
	}}, nil)

	result, err := svc.Analyze(context.Background(), AnalyzeOptions{DocumentID: doc.ID})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Proposed != 1 {
		t.Fatalf("proposed = %d, want 1 (warnings: %v)", result.Proposed, result.Warnings)
	}
	if nfrs.updates != 0 {
		t.Fatalf("analysis wrote to the catalog %d times", nfrs.updates)
	}
	if result.Proposals[0].Status != StatusPending {
		t.Fatalf("proposal was not left pending: %q", result.Proposals[0].Status)
	}
	if result.Proposals[0].Model != "test-model" || result.Proposals[0].PromptHash != "abc123" {
		t.Fatalf("provenance not recorded: %+v", result.Proposals[0])
	}
}

func TestAnalyzeDiscardsUnverifiableProposals(t *testing.T) {
	repo := newFakeRepo()
	doc, _ := repo.CreateDocument(Document{Title: "Standard"}, []Chunk{
		{ID: 1, Ordinal: 0, Heading: "Encryption in Transit",
			Text: "All external endpoints negotiate TLS 1.2 or higher at the edge."},
	})
	nfrs := newFakeNFRs(securitynfr.NFR{Key: "NFR-1", Summary: "Encryption in transit"})

	svc := NewService(repo, nfrs, &stubAnalyzer{reply: AnalysisReply{
		Propose:       true,
		SuggestedText: "Keys rotate every 90 days in the corporate HSM.",
		Citations:     []replyCitation{{Excerpt: 1, Quote: "keys rotate every 90 days"}},
	}}, nil)

	result, err := svc.Analyze(context.Background(), AnalyzeOptions{DocumentID: doc.ID})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Proposed != 0 || result.Discarded != 1 {
		t.Fatalf("proposed=%d discarded=%d, want 0/1", result.Proposed, result.Discarded)
	}
	// A silent discard would hide a prompt or model regression.
	if len(result.Warnings) == 0 {
		t.Fatal("a discarded proposal produced no warning")
	}
	if got, _ := repo.ListProposals(ProposalFilter{}); len(got) != 0 {
		t.Fatalf("an unverifiable proposal was stored: %+v", got)
	}
}

func TestAnalyzeSkipsUnrelatedNFRsWithoutCallingTheModel(t *testing.T) {
	repo := newFakeRepo()
	doc, _ := repo.CreateDocument(Document{Title: "Standard"}, []Chunk{
		{ID: 1, Ordinal: 0, Heading: "Encryption in Transit",
			Text: "All external endpoints negotiate TLS 1.2 or higher at the edge."},
	})
	nfrs := newFakeNFRs(securitynfr.NFR{
		Key: "NFR-9", Summary: "Physical badge access to data centre floors",
		Description: "Visitors are escorted at all times.",
	})

	analyzer := &stubAnalyzer{}
	svc := NewService(repo, nfrs, analyzer, nil)

	result, err := svc.Analyze(context.Background(), AnalyzeOptions{DocumentID: doc.ID})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if analyzer.calls != 0 {
		t.Fatalf("the model was called %d times for an unrelated NFR", analyzer.calls)
	}
	if result.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1", result.Skipped)
	}
}

func TestAnalyzeContinuesAfterOneNFRFails(t *testing.T) {
	repo := newFakeRepo()
	doc, _ := repo.CreateDocument(Document{Title: "Standard"}, []Chunk{
		{ID: 1, Ordinal: 0, Heading: "Encryption in Transit",
			Text: "All external endpoints negotiate TLS 1.2 or higher at the edge."},
	})
	nfrs := newFakeNFRs(
		securitynfr.NFR{Key: "NFR-1", Summary: "Encryption in transit TLS"},
		securitynfr.NFR{Key: "NFR-2", Summary: "TLS endpoints external"},
	)

	analyzer := &stubAnalyzer{
		failFirst: true,
		reply: AnalysisReply{
			Propose:       true,
			SuggestedText: "External endpoints negotiate TLS 1.2 or higher.",
			Citations:     []replyCitation{{Excerpt: 1, Quote: "negotiate TLS 1.2 or higher"}},
		},
	}
	svc := NewService(repo, nfrs, analyzer, nil)

	result, err := svc.Analyze(context.Background(), AnalyzeOptions{DocumentID: doc.ID})
	if err != nil {
		t.Fatalf("Analyze returned an error instead of continuing: %v", err)
	}
	if result.Proposed != 1 {
		t.Fatalf("proposed = %d, want 1 — one failure discarded the whole run", result.Proposed)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly the one failure", result.Warnings)
	}
}

// Two library documents can be the same instrument exported twice. Importing
// both would produce two identical proposals for every NFR they match, which
// doubles the review queue without adding a fact.
func TestImportRejectsDuplicatesByExtractedText(t *testing.T) {
	repo := newFakeRepo()
	lib := &fakeLibrary{docs: map[int64]aiprovider.LibraryContent{
		1: libraryDoc(1, "a.md", "All external endpoints negotiate TLS 1.2 or higher."),
		2: libraryDoc(2, "b.md", "All external endpoints negotiate TLS 1.2 or higher."),
	}}
	svc := NewService(repo, newFakeNFRs(), &stubAnalyzer{}, lib.provide)

	if _, err := svc.Import(context.Background(), ImportInput{LibraryDocID: 1}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	_, err := svc.Import(context.Background(), ImportInput{LibraryDocID: 2})
	if err == nil {
		t.Fatal("a duplicate document was accepted")
	}
	if !strings.Contains(err.Error(), "already in the corpus") {
		t.Fatalf("unhelpful duplicate error: %v", err)
	}
}

// Importing the same library document twice is caught before it is read, so the
// message names the copy that already exists rather than the text.
func TestImportRejectsTheSameLibraryDocumentTwice(t *testing.T) {
	repo := newFakeRepo()
	lib := &fakeLibrary{docs: map[int64]aiprovider.LibraryContent{
		7: libraryDoc(7, "dora.pdf", "Article 17\n\nAn entity shall report a major incident."),
	}}
	svc := NewService(repo, newFakeNFRs(), &stubAnalyzer{}, lib.provide)

	if _, err := svc.Import(context.Background(), ImportInput{LibraryDocID: 7}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	reads := lib.reads
	if _, err := svc.Import(context.Background(), ImportInput{LibraryDocID: 7}); err == nil {
		t.Fatal("the same library document was imported twice")
	}
	if lib.reads != reads {
		t.Errorf("the duplicate was read from wintermute before being refused (%d reads)", lib.reads)
	}
}

// A document the server is still reading has only some of its passages. An
// import there would silently analyse a fraction of the document.
func TestImportRefusesADocumentStillBeingRead(t *testing.T) {
	content := libraryDoc(3, "scan.pdf", "Article 1\n\nSomething.")
	content.Document.Processing = &aiprovider.LibraryProcessing{Attempts: 1}
	lib := &fakeLibrary{docs: map[int64]aiprovider.LibraryContent{3: content}}
	svc := NewService(newFakeRepo(), newFakeNFRs(), &stubAnalyzer{}, lib.provide)

	_, err := svc.Import(context.Background(), ImportInput{LibraryDocID: 3})
	if err == nil {
		t.Fatal("a document still being read was imported")
	}
	if !strings.Contains(err.Error(), "still being read") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

// With no Wintermute server or agent there is nothing to import from, and the
// message has to say that rather than pointing at an API key.
func TestImportWithoutALibrarySaysSo(t *testing.T) {
	svc := NewService(newFakeRepo(), newFakeNFRs(), &stubAnalyzer{}, nil)

	_, err := svc.Import(context.Background(), ImportInput{LibraryDocID: 1})
	if !errors.Is(err, ErrNoLibrary) {
		t.Fatalf("Import without a library = %v, want ErrNoLibrary", err)
	}
	if svc.LibraryAvailable() {
		t.Error("LibraryAvailable() is true with no library configured")
	}
}

func TestDeleteDocumentSupersedesPendingProposals(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeNFRs(), &stubAnalyzer{}, nil)

	doc, _ := repo.CreateDocument(Document{Title: "Standard"}, []Chunk{{ID: 1, Text: "x"}})
	pending, _ := repo.CreateProposal(Proposal{
		NFRKey: "NFR-1", DocumentID: doc.ID, Field: FieldImplementation,
		SuggestedText: "text", Status: StatusPending,
	})

	if err := svc.DeleteDocument(doc.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	after, err := repo.GetProposal(pending.ID)
	if err != nil {
		t.Fatalf("the proposal was deleted along with its document: %v", err)
	}
	if after.Status != StatusSuperseded {
		t.Fatalf("pending proposal is %q after its source was deleted", after.Status)
	}
}

// ---- fakes ----

// stubAnalyzer stands in for the model. It counts calls, because "the model was
// never asked" is the assertion behind the retrieval floor.
type stubAnalyzer struct {
	reply     AnalysisReply
	calls     int
	failFirst bool
}

func (s *stubAnalyzer) Model() string    { return "test-model" }
func (s *stubAnalyzer) Configured() bool { return true }

func (s *stubAnalyzer) Analyze(context.Context, AnalysisRequest) (AnalysisReply, error) {
	s.calls++
	if s.failFirst && s.calls == 1 {
		return AnalysisReply{}, errors.New("model timeout")
	}
	return s.reply, nil
}

type fakeNFRs struct {
	items   map[string]securitynfr.NFR
	order   []string
	updates int
}

func newFakeNFRs(items ...securitynfr.NFR) *fakeNFRs {
	f := &fakeNFRs{items: map[string]securitynfr.NFR{}}
	for _, item := range items {
		f.items[item.Key] = item
		f.order = append(f.order, item.Key)
	}
	return f
}

func (f *fakeNFRs) List(_, domain string) ([]securitynfr.NFR, error) {
	out := make([]securitynfr.NFR, 0, len(f.order))
	for _, key := range f.order {
		item := f.items[key]
		if domain != "" && item.Domain != domain {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (f *fakeNFRs) Get(key string) (securitynfr.NFR, error) {
	item, ok := f.items[key]
	if !ok {
		return securitynfr.NFR{}, ErrNotFound
	}
	return item, nil
}

func (f *fakeNFRs) Update(key string, nfr securitynfr.NFR) (securitynfr.NFR, error) {
	if _, ok := f.items[key]; !ok {
		return securitynfr.NFR{}, ErrNotFound
	}
	f.updates++
	nfr.Key = key
	f.items[key] = nfr
	return nfr, nil
}

type fakeRepo struct {
	docs      map[int64]Document
	chunks    map[int64][]Chunk
	proposals map[int64]Proposal
	nextDoc   int64
	nextProp  int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		docs:      map[int64]Document{},
		chunks:    map[int64][]Chunk{},
		proposals: map[int64]Proposal{},
	}
}

func (r *fakeRepo) CreateDocument(doc Document, chunks []Chunk) (Document, error) {
	r.nextDoc++
	doc.ID = r.nextDoc
	doc.ChunkCount = len(chunks)
	for i := range chunks {
		chunks[i].DocumentID = doc.ID
		if chunks[i].ID == 0 {
			chunks[i].ID = int64(i + 1)
		}
	}
	r.docs[doc.ID] = doc
	r.chunks[doc.ID] = chunks
	return doc, nil
}

func (r *fakeRepo) GetDocument(id int64) (Document, error) {
	doc, ok := r.docs[id]
	if !ok {
		return Document{}, ErrNotFound
	}
	return doc, nil
}

func (r *fakeRepo) DocumentByLibraryID(libraryDocID int64) (Document, error) {
	for _, doc := range r.docs {
		if doc.LibraryDocID != 0 && doc.LibraryDocID == libraryDocID {
			return doc, nil
		}
	}
	return Document{}, ErrNotFound
}

func (r *fakeRepo) DocumentBySHA(sha string) (Document, error) {
	for _, doc := range r.docs {
		if doc.SHA256 == sha && sha != "" {
			return doc, nil
		}
	}
	return Document{}, ErrNotFound
}

func (r *fakeRepo) ListDocuments() ([]Document, error) {
	out := make([]Document, 0, len(r.docs))
	for _, doc := range r.docs {
		out = append(out, doc)
	}
	return out, nil
}

func (r *fakeRepo) DeleteDocument(id int64) error {
	if _, ok := r.docs[id]; !ok {
		return ErrNotFound
	}
	delete(r.docs, id)
	delete(r.chunks, id)
	return nil
}

func (r *fakeRepo) ListChunks(documentID int64) ([]Chunk, error) {
	return r.chunks[documentID], nil
}

func (r *fakeRepo) CreateProposal(p Proposal) (Proposal, error) {
	r.nextProp++
	p.ID = r.nextProp
	r.proposals[p.ID] = p
	return p, nil
}

func (r *fakeRepo) GetProposal(id int64) (Proposal, error) {
	p, ok := r.proposals[id]
	if !ok {
		return Proposal{}, ErrNotFound
	}
	return p, nil
}

func (r *fakeRepo) ListProposals(filter ProposalFilter) ([]Proposal, error) {
	out := []Proposal{}
	for _, p := range r.proposals {
		if filter.Status != "" && p.Status != filter.Status {
			continue
		}
		if filter.NFRKey != "" && p.NFRKey != filter.NFRKey {
			continue
		}
		if filter.DocumentID > 0 && p.DocumentID != filter.DocumentID {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *fakeRepo) SetProposalDecision(id int64, status, decidedAt, decidedBy, note string) error {
	p, ok := r.proposals[id]
	if !ok {
		return ErrNotFound
	}
	p.Status, p.DecidedAt, p.DecidedBy, p.DecidedNote = status, decidedAt, decidedBy, note
	r.proposals[id] = p
	return nil
}

func (r *fakeRepo) SupersedePending(nfrKey, field string, exceptID int64, at string) (int64, error) {
	var n int64
	for id, p := range r.proposals {
		if id == exceptID || p.NFRKey != nfrKey || p.Field != field || p.Status != StatusPending {
			continue
		}
		p.Status, p.DecidedAt = StatusSuperseded, at
		r.proposals[id] = p
		n++
	}
	return n, nil
}

// ---- small helpers ----

func headings(chunks []Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Heading)
	}
	return out
}

func scoreList(hits []ScoredChunk) string {
	parts := make([]string, 0, len(hits))
	for _, h := range hits {
		parts = append(parts, fmt.Sprintf("%d:%.2f", h.Chunk.ID, h.Score))
	}
	return strings.Join(parts, " ")
}

func containsSubstring(values []string, want string) bool {
	for _, v := range values {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}
