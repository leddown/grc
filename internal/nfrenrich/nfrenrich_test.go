package nfrenrich

import (
	"context"
	"errors"
	"fmt"
	"net"
	neturl "net/url"
	"strings"
	"testing"

	"grc/internal/securitynfr"
)

// ---- chunking ----

func TestChunkTextTracksHeadingPath(t *testing.T) {
	doc := `# Transport Security Standard

This standard covers protection of data while it moves between systems.
It applies to every externally reachable service in the estate.

## TLS Configuration

All external endpoints terminate TLS 1.2 or higher. TLS 1.0 and 1.1 are
disabled at the load balancer. Cipher suites are restricted to the approved
list maintained by the platform team.

### Certificate Management

Certificates are issued by the internal ACME service and rotate every 90 days.
Expiry is monitored and alerts fire at fourteen days remaining.

## Data at Rest

Volumes are encrypted with AES-256 using keys held in the managed KMS.
This section is about storage, not transport, and exists to be ignored.`

	chunks := ChunkText(doc)
	if len(chunks) < 4 {
		t.Fatalf("got %d chunks, want at least 4", len(chunks))
	}

	byHeading := map[string]Chunk{}
	for _, c := range chunks {
		byHeading[c.Heading] = c
	}

	// The nested heading must carry its ancestors, or a citation reads as
	// "Certificate Management" with no indication of which standard it is from.
	want := "Transport Security Standard > TLS Configuration > Certificate Management"
	cert, ok := byHeading[want]
	if !ok {
		t.Fatalf("missing heading path %q; got %v", want, headings(chunks))
	}
	if !strings.Contains(cert.Text, "90 days") {
		t.Fatalf("certificate chunk lost its body: %q", cert.Text)
	}

	// A sibling h2 must reset the path rather than nest under the previous one.
	if _, ok := byHeading["Transport Security Standard > Data at Rest"]; !ok {
		t.Fatalf("sibling heading did not reset the path; got %v", headings(chunks))
	}
}

func TestChunkTextRecognisesNumberedAndSetextHeadings(t *testing.T) {
	doc := `Encryption Standard
===================

Scope covers all production services handling customer information.

4.2 Encryption in Transit

Every service presents a certificate from the corporate CA and negotiates
TLS 1.3 where the client supports it, falling back to TLS 1.2.

1. Rotate the shared secret at least every ninety days.
2. Record each rotation in the change log.`

	chunks := ChunkText(doc)
	paths := headings(chunks)

	if !containsSubstring(paths, "Encryption Standard") {
		t.Fatalf("setext heading not detected; got %v", paths)
	}
	if !containsSubstring(paths, "4.2 Encryption in Transit") {
		t.Fatalf("numbered heading not detected; got %v", paths)
	}
	// The ordered list items are prose, not sections: promoting them would
	// shred the section they belong to into one-line fragments.
	for _, p := range paths {
		if strings.Contains(p, "Rotate the shared secret") {
			t.Fatalf("numbered list item was promoted to a heading: %v", paths)
		}
	}
}

func TestExtractTextRejectsBinaryAndKeepsHTMLHeadings(t *testing.T) {
	if _, err := ExtractText("application/pdf", []byte("%PDF-1.7\n\x00\x00binary")); !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("binary body was accepted; want ErrUnsupportedMedia")
	}

	html := `<html><head><title>Crypto Standard</title></head><body>
	<script>var x = "<h1>not a heading</h1>";</script>
	<h1>Crypto Standard</h1><p>Applies to all services.</p>
	<h2>In Transit</h2><p>TLS 1.2 minimum, enforced at the edge &amp; logged.</p>
	</body></html>`

	text, err := ExtractText("text/html", []byte(html))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "# Crypto Standard") || !strings.Contains(text, "## In Transit") {
		t.Fatalf("HTML headings were not converted to Markdown: %q", text)
	}
	if strings.Contains(text, "var x") {
		t.Fatalf("script contents survived extraction: %q", text)
	}
	if !strings.Contains(text, "edge & logged") {
		t.Fatalf("entities not decoded: %q", text)
	}
	if title := HTMLTitle([]byte(html)); title != "Crypto Standard" {
		t.Fatalf("HTMLTitle = %q", title)
	}
}

// ---- retrieval ----

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

func TestValidateFetchURLRefusesNonPublicTargets(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/standard",
		"http://localhost/standard",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.1.2.3/internal",
		"http://192.168.0.9/wiki",
		"http://[::1]/standard",
		"http://100.64.0.1/internal",
		"file:///etc/passwd",
		"http://user:pass@example.com/doc",
		"https://",
	}
	for _, raw := range blocked {
		t.Run(raw, func(t *testing.T) {
			u, err := parseForTest(raw)
			if err != nil {
				return // an unparseable URL is refused earlier
			}
			if err := validateFetchURL(u); err == nil {
				t.Fatalf("validateFetchURL(%q) allowed a non-public target", raw)
			}
		})
	}

	u, err := parseForTest("https://example.com/security-standard")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFetchURL(u); err != nil {
		t.Fatalf("a public https URL was refused: %v", err)
	}
}

func TestIsPublicIP(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.0.0.1": false, "172.16.0.1": false,
		"192.168.1.1": false, "169.254.169.254": false, "100.64.0.1": false,
		"::1": false, "::ffff:127.0.0.1": false, "0.0.0.0": false, "224.0.0.1": false,
	}
	for raw, want := range cases {
		if got := isPublicIP(parseIPForTest(raw)); got != want {
			t.Errorf("isPublicIP(%s) = %v, want %v", raw, got, want)
		}
	}
}

// ---- the review workflow ----

func TestAcceptProposalWritesTheFieldAndSupersedesSiblings(t *testing.T) {
	repo := newFakeRepo()
	nfrs := newFakeNFRs(securitynfr.NFR{
		Key: "NFR-1", Summary: "Encryption in transit", Implementation: "TBD",
	})
	svc := NewService(repo, nfrs, &stubAnalyzer{})

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
	svc := NewService(repo, nfrs, &stubAnalyzer{})

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
	svc := NewService(repo, nfrs, &stubAnalyzer{})

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
	svc := NewService(repo, nfrs, &stubAnalyzer{})

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
	}})

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
	}})

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
	svc := NewService(repo, nfrs, analyzer)

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
	svc := NewService(repo, nfrs, analyzer)

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

func TestPrepareDocumentRejectsDuplicatesByExtractedText(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeNFRs(), &stubAnalyzer{})

	body := []byte("# Standard\n\nAll external endpoints negotiate TLS 1.2 or higher at the edge of the estate.")
	if _, err := svc.AddUpload(UploadInput{Filename: "a.md", MediaType: "text/markdown", Body: body}); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	// Same text, different filename: the corpus gains nothing and the review
	// queue would double.
	_, err := svc.AddUpload(UploadInput{Filename: "b.md", MediaType: "text/markdown", Body: body})
	if err == nil {
		t.Fatal("a duplicate document was accepted")
	}
	if !strings.Contains(err.Error(), "already in the corpus") {
		t.Fatalf("unhelpful duplicate error: %v", err)
	}
}

func TestDeleteDocumentSupersedesPendingProposals(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeNFRs(), &stubAnalyzer{})

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

func parseForTest(raw string) (*neturl.URL, error) { return neturl.Parse(raw) }

func parseIPForTest(raw string) net.IP { return net.ParseIP(raw) }

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
