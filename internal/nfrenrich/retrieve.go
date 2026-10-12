package nfrenrich

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Retrieval is BM25 over the document's own chunks, per POLICY_MODULE_FRAMEWORK
// §2.4: lexical first, embeddings only if recall proves insufficient. It is a
// better fit here than it first looks — the queries are NFR summaries full of
// exactly the terms compliance documents use verbatim ("encryption in transit",
// "TLS", "AC-2", "cardholder data"), and a term match is evidence a reviewer
// can check, which a cosine distance is not.
//
// The scorer runs in Go over the rows of one document rather than through
// SQLite FTS5. FTS5 is a build tag in this repo, and the failure mode of using
// it unguarded is a runtime "no such module: fts5" the first time someone
// analyses a document on a binary built without the tag. One document's chunks
// number in the tens; scoring them in process costs nothing worth a build-tag
// dependency.

// BM25 parameters. These are the standard defaults; k1 controls how quickly
// term-frequency saturates and b how strongly length normalises.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Retriever selects the chunks worth sending to the model for a given query.
// It is an interface so an FTS5- or embedding-backed implementation can replace
// the scorer below without the analysis code changing.
type Retriever interface {
	// Rank returns chunks ordered by descending relevance, at most limit of
	// them, dropping anything scoring at or below zero.
	Rank(query string, chunks []Chunk, limit int) []ScoredChunk
}

// ScoredChunk is one retrieval hit.
type ScoredChunk struct {
	Chunk Chunk
	Score float64
	// Matched lists the query terms this chunk actually contains. It is what
	// makes a "why was this sent to the model" question answerable, and it is
	// shown in the analysis log.
	Matched []string
	// MatchedStrong is the subset of Matched drawn from the query's
	// high-signal lines — the NFR's summary and its NIST mapping. The
	// distinction is what Qualifies rests on: sharing a common word with a
	// requirement's long description is not evidence, matching its name is.
	MatchedStrong []string
}

// Qualifies reports whether a hit is strong enough to spend a model call on.
//
// A raw score threshold does not survive contact with real documents: BM25
// scores are not comparable across corpora, and a single incidental term — an
// "Access Reviews" section against a "badge access" requirement — can outscore
// a genuine two-term match in a longer section. So the rule is about *what*
// matched rather than how highly: at least two distinct query terms, at least
// one of them from the requirement's name or control mapping.
//
// One term is enough only when that term is a control identifier. "SC-8" is
// self-identifying: a section containing it is about SC-8. An ordinary word is
// not, however high it scores — a lone "access" matching an "Access Reviews"
// heading is the exact false positive this rule exists to refuse, and it can
// outscore a real two-term match in a longer section.
func (s ScoredChunk) Qualifies() bool {
	if len(s.MatchedStrong) == 0 {
		return false
	}
	if len(s.Matched) >= 2 {
		return s.Score >= minRetrievalScore
	}
	return looksLikeIdentifier(s.MatchedStrong[0]) && s.Score >= strongSingleTermScore
}

// looksLikeIdentifier reports whether a term is a control or clause reference
// rather than a word: it carries a digit and a separator, which is the shape of
// "sc-8", "ac-2", "8.2.1" and "pci-dss" and not the shape of English.
func looksLikeIdentifier(term string) bool {
	hasDigit := false
	hasSeparator := false
	for _, r := range term {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '-' || r == '.' || r == '_':
			hasSeparator = true
		}
	}
	return hasDigit && hasSeparator
}

const (
	// minRetrievalScore is the floor for a multi-term match.
	minRetrievalScore = 1.2
	// strongSingleTermScore is the higher bar a lone high-signal term must
	// clear on its own.
	strongSingleTermScore = 3.0
)

// BM25Retriever is the default lexical retriever.
type BM25Retriever struct{}

// NewBM25Retriever returns the default retriever.
func NewBM25Retriever() BM25Retriever { return BM25Retriever{} }

// stopwords are dropped from queries. The list is deliberately short and
// generic: dropping domain words like "security" or "control" would gut the
// signal in a catalog where every entry is about security controls.
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "in": true, "is": true,
	"it": true, "of": true, "on": true, "or": true, "that": true, "the": true,
	"this": true, "to": true, "was": true, "were": true, "will": true, "with": true,
	"must": true, "shall": true, "should": true,
}

// Rank implements Retriever.
func (BM25Retriever) Rank(query string, chunks []Chunk, limit int) []ScoredChunk {
	terms := queryTerms(query)
	if len(terms) == 0 || len(chunks) == 0 {
		return nil
	}

	// Document frequency and length statistics over this document's chunks.
	// The corpus is the document, which is the right scope: a term appearing in
	// every chunk of *this* document tells you nothing about which chunk to
	// send, regardless of how rare it is across the library.
	docFreq := make(map[string]int, len(terms))
	tokenized := make([][]string, len(chunks))
	totalLen := 0
	for i, chunk := range chunks {
		// The heading is scored as part of the chunk: a section titled
		// "Encryption in Transit" whose body says only "see the TLS standard"
		// is still the right chunk to return for that query.
		tokens := tokenize(chunk.Heading + " " + chunk.Text)
		tokenized[i] = tokens
		totalLen += len(tokens)

		seen := make(map[string]bool, len(tokens))
		for _, tok := range tokens {
			if seen[tok] {
				continue
			}
			seen[tok] = true
			if _, wanted := terms[tok]; wanted {
				docFreq[tok]++
			}
		}
	}
	if totalLen == 0 {
		return nil
	}
	avgLen := float64(totalLen) / float64(len(chunks))
	n := float64(len(chunks))

	scored := make([]ScoredChunk, 0, len(chunks))
	for i, chunk := range chunks {
		tokens := tokenized[i]
		freq := make(map[string]int, len(tokens))
		for _, tok := range tokens {
			freq[tok]++
		}

		var (
			score   float64
			matched []string
			strong  []string
		)
		for term, weight := range terms {
			tf := float64(freq[term])
			if tf == 0 {
				continue
			}
			matched = append(matched, term)
			if weight > 1 {
				strong = append(strong, term)
			}

			df := float64(docFreq[term])
			// BM25's IDF with the +1 guard, so a term present in every chunk
			// scores near zero rather than negative.
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			norm := tf * (bm25K1 + 1) /
				(tf + bm25K1*(1-bm25B+bm25B*float64(len(tokens))/avgLen))
			score += weight * idf * norm
		}
		if score <= 0 {
			continue
		}
		sort.Strings(matched)
		sort.Strings(strong)
		scored = append(scored, ScoredChunk{
			Chunk: chunk, Score: score, Matched: matched, MatchedStrong: strong,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		// Ties break on document order, so a re-run produces the same prompt
		// and therefore the same prompt hash.
		return scored[i].Chunk.Ordinal < scored[j].Chunk.Ordinal
	})

	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

// queryTerms turns an NFR into weighted search terms.
//
// Weighting matters more than it looks: an NFR's Summary is a title written to
// be distinctive ("Encryption of data in transit"), while its Description is
// prose that shares vocabulary with every other entry in the catalog. Scoring
// them equally lets a long description drown the one phrase that identifies the
// requirement, and every document then retrieves for every NFR.
func queryTerms(query string) map[string]float64 {
	out := map[string]float64{}
	for _, part := range strings.Split(query, "\n") {
		weight := 1.0
		if strings.HasPrefix(part, "!") {
			// Caller-marked high-signal line (see Service.queryForNFR).
			weight, part = 3.0, part[1:]
		}
		for _, term := range tokenize(part) {
			if stopwords[term] || len(term) < 2 {
				continue
			}
			if weight > out[term] {
				out[term] = weight
			}
		}
	}
	return out
}

// tokenize lowercases and splits on non-alphanumerics, keeping the dots and
// hyphens inside control identifiers: "AC-2" and "1.2.3" must survive as single
// terms or the retrieval loses precisely the keywords compliance search runs on.
func tokenize(s string) []string {
	var (
		out  []string
		curr strings.Builder
	)
	flush := func() {
		if curr.Len() == 0 {
			return
		}
		term := strings.Trim(curr.String(), ".-")
		curr.Reset()
		if term != "" {
			out = append(out, term)
		}
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			curr.WriteRune(r)
		case r == '-' || r == '.' || r == '_':
			// Kept only between alphanumerics, which flush handles by trimming
			// the edges.
			if curr.Len() > 0 {
				curr.WriteRune(r)
			}
		default:
			flush()
		}
	}
	flush()
	return out
}
