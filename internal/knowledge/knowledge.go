// Package knowledge is this installation's compliance data, in the shape an AI
// agent can consult: a small, read-only, machine-facing query surface over the
// Security NFR catalog, the NIST SP 800-53 controls and their links, the
// Regulation Coverage reports, the policy library, and the risk register.
//
// It exists because of a specific failure. Asked "how many Security NFRs are
// focused on network segmentation?", a model with no access to this data
// answers by describing how it *would* answer if someone pasted the catalog in
// — which is worse than useless, because it looks like an answer. The fix is
// not a longer prompt; it is giving the agent the catalog.
//
// # Where the agent lives
//
// Not here. wintermuted already owns an agent loop, a tool registry and a
// transcript store, and a second one in this application would mean two of
// each and two places to look when a model does something surprising. So this
// package publishes the data and stops: wintermute registers tools that call
// it, and grc's own AI pages reach it by routing their questions through
// wintermuted. See REGULATION_COVERAGE.md and wintermute's docs/agents.md.
//
// # Shape
//
// Four endpoints, because a small tool set is easier for a model to use well
// than a large one:
//
//   - Overview — how much of each kind exists, with the NFR domains and control
//     families and their counts. Orientation, and cheap.
//   - NFR index — the entire NFR catalog as one compact list. The catalog is
//     small enough (order 100) that a counting question is answered exactly by
//     reading all of it, which no top-k retrieval can promise.
//   - Search — lexical search within one kind, returning the matches *and the
//     total number of them*, so "how many" is answerable rather than guessed.
//   - Item — one full record by reference.
//
// Everything is read-only. The token that reaches it cannot write, and this
// package holds no mutating method to reach.
package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// Kinds of thing this API can search and fetch.
const (
	KindNFR              = "nfr"
	KindControl          = "control"
	KindRegulation       = "regulation"
	KindRegulationClause = "regulation_clause"
	KindPolicy           = "policy"
	KindPolicyClause     = "policy_clause"
	KindRisk             = "risk"
	KindExercise         = "exercise"
	KindExerciseFinding  = "exercise_finding"
)

// Kinds lists every searchable kind, in the order the overview reports them.
//
// Exceptions are absent deliberately: the Exceptions page is a saved view over
// the NFR catalog rather than a record set of its own, so there is nothing here
// to search that searching NFRs does not already cover.
//
// Crisis exercises and their findings are present because they answer a
// question none of the other corpora can: not what this installation requires,
// but what it has actually tested and what failed when it did. An agent asked
// "have we ever exercised our major-incident classification, and how did it
// go?" needs the exercise record, and without it will answer from the control
// catalog — which describes an intention, not an outcome.
func Kinds() []string {
	return []string{
		KindNFR, KindControl, KindRegulationClause, KindRegulation,
		KindPolicyClause, KindPolicy, KindRisk, KindExercise, KindExerciseFinding,
	}
}

// Item is one record from any of the catalogs, flattened into a common shape.
//
// The flattening is deliberate: a model handles one predictable record type
// better than eight bespoke ones, and the fields that differ per kind live in
// Fields rather than in a union of structs it has to learn.
type Item struct {
	Kind string `json:"kind"`
	// Ref is the stable identifier a follow-up call uses: an NFR key, a control
	// ID, a regulation section ref, a policy id, a risk id.
	Ref   string `json:"ref"`
	Title string `json:"title"`
	// Summary is one line — what a search result shows.
	Summary string `json:"summary,omitempty"`
	// Body is the full text, present on a fetched item and omitted from search
	// results.
	Body string `json:"body,omitempty"`
	// Group is the kind's natural grouping: an NFR domain, a control family, a
	// regulation title, a policy title, a risk status.
	Group string `json:"group,omitempty"`
	// Related names other records this one points at — the controls an NFR
	// maps to, the NFRs and controls a regulation clause was mapped to.
	Related []string `json:"related,omitempty"`
	// Fields carries the per-kind extras: baselines for a control, scores for a
	// risk, coverage for a regulation clause.
	Fields map[string]string `json:"fields,omitempty"`
	// URL is where a human can see this record in the application.
	URL string `json:"url,omitempty"`
}

// SearchHit is one result, with the evidence for why it matched.
type SearchHit struct {
	Item Item `json:"item"`
	// Score is the lexical relevance score. It is comparable within one
	// response and meaningless across responses.
	Score float64 `json:"score"`
	// Matched lists the query terms this record actually contains, which is
	// what makes "why did this come back?" answerable.
	Matched []string `json:"matched,omitempty"`
}

// SearchResult is a page of hits plus the count that makes counting questions
// answerable.
type SearchResult struct {
	Kind  string      `json:"kind"`
	Query string      `json:"query"`
	Hits  []SearchHit `json:"hits"`
	// TotalMatches is how many records matched at all, not how many were
	// returned. A model asked "how many NFRs mention segmentation" needs this
	// number and cannot derive it from a truncated page.
	TotalMatches int `json:"total_matches"`
	// TotalAllTerms is how many matched *every* term in the query. For a
	// two-word query the gap between the two numbers is the whole answer:
	// "network segmentation" matches 26 records on one word or the other and a
	// handful on both, and reporting only the first number would turn a precise
	// question into an inflated answer.
	TotalAllTerms int `json:"total_all_terms"`
	// Terms is what the query was actually searched as, after tokenising and
	// dropping stopwords, so a caller can see which words the counts refer to.
	Terms    []string `json:"terms,omitempty"`
	Returned int      `json:"returned"`
	// Truncated says plainly that there are more matches than were returned,
	// so an answer built on this page can say so too.
	Truncated bool `json:"truncated"`
	// Note carries a caveat the model should pass on — chiefly that lexical
	// search finds wording, not meaning.
	Note string `json:"note,omitempty"`
}

// Overview is the orientation call: what exists here, and how it is grouped.
type Overview struct {
	Counts map[string]int `json:"counts"`
	// NFRDomains and ControlFamilies are the two groupings worth knowing before
	// asking anything else.
	NFRDomains      []GroupCount `json:"nfr_domains"`
	ControlFamilies []GroupCount `json:"control_families"`
	Regulations     []Item       `json:"regulations"`
	RiskStatuses    []GroupCount `json:"risk_statuses"`
	// Note tells the agent how to use this surface well.
	Note string `json:"note"`
}

// GroupCount is one bucket of a grouping.
type GroupCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func sortedGroups(counts map[string]int) []GroupCount {
	out := make([]GroupCount, 0, len(counts))
	for name, n := range counts {
		if strings.TrimSpace(name) == "" {
			name = "(none)"
		}
		out = append(out, GroupCount{Name: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ErrNotFound reports an unknown kind or reference.
type ErrNotFound struct{ What string }

func (e ErrNotFound) Error() string { return e.What + " not found" }

// ErrInvalid reports a malformed request.
type ErrInvalid struct{ Message string }

func (e ErrInvalid) Error() string { return e.Message }

func invalidf(format string, args ...any) error {
	return ErrInvalid{Message: fmt.Sprintf(format, args...)}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// oneLine collapses text to a single line and caps it, for a search result.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}
