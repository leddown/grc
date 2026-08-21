package knowledge

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"grc/internal/nfrenrich"
)

// Search bounds.
const (
	defaultSearchLimit = 10
	maxSearchLimit     = 50
	// snippetLimit caps the body text shown per hit. A model reading ten hits
	// needs enough to judge each one, not the whole record — that is what a
	// follow-up item call is for.
	snippetLimit = 400
)

// cacheTTL is how long a loaded corpus is reused. The catalogs change when
// someone edits them in the UI, which is rare compared with how often an agent
// asks a question, and a stale answer for under a minute is a better trade than
// re-reading every table per tool call.
const cacheTTL = 45 * time.Second

// Service answers the four knowledge questions over grc's own data.
type Service struct {
	store  *Store
	ranker nfrenrich.Retriever

	mu     sync.Mutex
	cached map[string]cacheEntry
	now    func() time.Time
}

type cacheEntry struct {
	items  []Item
	loaded time.Time
}

func NewService(store *Store) *Service {
	return &Service{
		store:  store,
		ranker: nfrenrich.NewBM25Retriever(),
		cached: map[string]cacheEntry{},
		now:    time.Now,
	}
}

// loader returns the corpus for one kind.
func (s *Service) loader(kind string) (func() ([]Item, error), error) {
	switch kind {
	case KindNFR:
		return s.store.NFRs, nil
	case KindControl:
		return s.store.Controls, nil
	case KindRegulation:
		return s.store.Regulations, nil
	case KindRegulationClause:
		return s.store.RegulationClauses, nil
	case KindPolicy:
		return s.store.Policies, nil
	case KindPolicyClause:
		return s.store.PolicyClauses, nil
	case KindRisk:
		return s.store.Risks, nil
	case KindExercise:
		return s.store.Exercises, nil
	case KindExerciseFinding:
		return s.store.ExerciseFindings, nil
	default:
		return nil, invalidf("unknown kind %q (known kinds: %s)", kind, strings.Join(Kinds(), ", "))
	}
}

// Items returns one corpus, from cache when it is fresh.
func (s *Service) Items(kind string) ([]Item, error) {
	load, err := s.loader(kind)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	entry, ok := s.cached[kind]
	fresh := ok && s.now().Sub(entry.loaded) < cacheTTL
	s.mu.Unlock()
	if fresh {
		return entry.items, nil
	}

	items, err := load()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cached[kind] = cacheEntry{items: items, loaded: s.now()}
	s.mu.Unlock()
	return items, nil
}

// Overview reports what exists, so an agent can orient before searching.
func (s *Service) Overview() (*Overview, error) {
	out := &Overview{Counts: map[string]int{}}

	nfrDomains := map[string]int{}
	controlFamilies := map[string]int{}
	riskStatuses := map[string]int{}

	for _, kind := range Kinds() {
		items, err := s.Items(kind)
		if err != nil {
			return nil, err
		}
		out.Counts[kind] = len(items)
		for _, item := range items {
			switch kind {
			case KindNFR:
				nfrDomains[item.Group]++
			case KindControl:
				controlFamilies[item.Group]++
			case KindRisk:
				riskStatuses[item.Group]++
			}
		}
		if kind == KindRegulation {
			out.Regulations = items
		}
	}

	out.NFRDomains = sortedGroups(nfrDomains)
	out.ControlFamilies = sortedGroups(controlFamilies)
	out.RiskStatuses = sortedGroups(riskStatuses)
	out.Note = "Counting questions about Security NFRs should use the full NFR index rather than search: " +
		"the catalog is small enough to read whole, and search finds wording rather than meaning."
	return out, nil
}

// Index returns an entire corpus in compact form — no bodies, just what is
// needed to read the whole catalog and count.
//
// It is offered for the small catalogs, and refused for the ones where reading
// everything is not a sane thing for an agent to do: dumping several thousand
// regulation clauses into a context window is not an index, it is a denial of
// service against the answer.
func (s *Service) Index(kind string) ([]Item, error) {
	switch kind {
	// Exercises belong here for the same reason NFRs do: an installation holds
	// a handful per year, and "how many exercises have we run, and how many
	// reached the board phase" is answered by reading all of them and counting,
	// which no top-k search can promise.
	case KindNFR, KindControl, KindRegulation, KindPolicy, KindRisk, KindExercise:
	default:
		return nil, invalidf("no compact index for kind %q; use search instead", kind)
	}

	items, err := s.Items(kind)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(items))
	for _, item := range items {
		item.Body = ""
		out = append(out, item)
	}
	return out, nil
}

// Search runs lexical search within one kind.
func (s *Service) Search(kind, query string, limit int) (*SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalid{Message: "a query is required"}
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	items, err := s.Items(kind)
	if err != nil {
		return nil, err
	}

	// The ranker scores chunks; each record becomes one chunk, with its title
	// and identifier marked high-signal so a query naming a control matches the
	// control rather than everything that mentions it in passing.
	chunks := make([]nfrenrich.Chunk, 0, len(items))
	for i, item := range items {
		chunks = append(chunks, nfrenrich.Chunk{
			ID:      int64(i),
			Ordinal: i,
			Heading: item.Ref + " " + item.Title,
			Text:    searchableText(item),
		})
	}

	ranked := s.ranker.Rank(markedQuery(query), chunks, 0)
	terms := queryTerms(query)

	allTerms := 0
	for _, hit := range ranked {
		if matchedEvery(hit.Matched, terms) {
			allTerms++
		}
	}

	result := &SearchResult{
		Kind:          kind,
		Query:         query,
		Terms:         terms,
		TotalMatches:  len(ranked),
		TotalAllTerms: allTerms,
		Hits:          []SearchHit{},
		Note: "Lexical search: these are the records whose wording matches the query. " +
			"total_matches counts records matching any term and total_all_terms counts those " +
			"matching every term — for a multi-word query the second is usually the honest one. " +
			"Word forms are not stemmed, so a record saying \"segmented\" does not match " +
			"\"segmentation\"; search the other forms too before trusting a count. Both numbers " +
			"are floors rather than definitive answers, and each record's matched terms show " +
			"which of them it actually used.",
	}

	for i, hit := range ranked {
		if i >= limit {
			break
		}
		item := items[hit.Chunk.ID]
		item.Body = oneLine(item.Body, snippetLimit)
		result.Hits = append(result.Hits, SearchHit{
			Item:    item,
			Score:   round2(hit.Score),
			Matched: hit.Matched,
		})
	}
	result.Returned = len(result.Hits)
	result.Truncated = result.TotalMatches > result.Returned
	return result, nil
}

// markedQuery marks every query line high-signal for the ranker, which weights
// a "!"-prefixed line above the rest. Here the whole query is the signal —
// there is no long description to drown it, which is the case that convention
// was built for.
func markedQuery(query string) string {
	lines := strings.Split(query, "\n")
	for i, line := range lines {
		lines[i] = "!" + line
	}
	return strings.Join(lines, "\n")
}

// searchTermStopwords mirrors the ranker's own list. It is duplicated rather
// than exported from there because the two serve different purposes: the ranker
// drops these from scoring, and this drops them from the terms a *count* is
// reported against. Counting "records containing 'the'" would make
// total_all_terms meaningless.
var searchTermStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "in": true, "is": true,
	"it": true, "of": true, "on": true, "or": true, "that": true, "the": true,
	"this": true, "to": true, "was": true, "were": true, "will": true, "with": true,
	"must": true, "shall": true, "should": true,
}

// queryTerms is the query as the counts refer to it.
func queryTerms(query string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, field := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	}) {
		term := strings.Trim(field, "-")
		if len(term) < 2 || searchTermStopwords[term] || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

// matchedEvery reports whether a hit matched every term of the query. The
// ranker reports matches on its own tokenisation, so a term this function knows
// about but the ranker split differently simply does not count as matched —
// which errs towards the smaller, more defensible number.
func matchedEvery(matched, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	have := make(map[string]bool, len(matched))
	for _, m := range matched {
		have[m] = true
	}
	for _, term := range terms {
		if !have[term] {
			return false
		}
	}
	return true
}

// searchableText is what the ranker scores: everything about a record that a
// person might search for, including its group and the references it points at,
// so "which NFRs map to SC-7" finds them.
func searchableText(item Item) string {
	parts := []string{item.Title, item.Summary, item.Body, item.Group}
	parts = append(parts, item.Related...)
	for key, value := range item.Fields {
		// Field names are not content; their values are.
		_ = key
		parts = append(parts, value)
	}
	return strings.Join(parts, "\n")
}

// Get returns one full record.
func (s *Service) Get(kind, ref string) (*Item, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrInvalid{Message: "a ref is required"}
	}
	items, err := s.Items(kind)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if strings.EqualFold(item.Ref, ref) {
			found := item
			return &found, nil
		}
	}
	// A model that guesses "AC2" for "AC-2" should be corrected rather than
	// told the control does not exist, so near misses come back in the error.
	if near := nearRefs(items, ref); len(near) > 0 {
		return nil, ErrNotFound{What: fmt.Sprintf("%s %q (did you mean: %s?)",
			kind, ref, strings.Join(near, ", "))}
	}
	return nil, ErrNotFound{What: fmt.Sprintf("%s %q", kind, ref)}
}

func nearRefs(items []Item, ref string) []string {
	needle := strings.ToLower(strings.NewReplacer("-", "", " ", "", "_", "").Replace(ref))
	if needle == "" {
		return nil
	}
	var out []string
	for _, item := range items {
		flat := strings.ToLower(strings.NewReplacer("-", "", " ", "", "_", "").Replace(item.Ref))
		if flat == needle || strings.HasPrefix(flat, needle) {
			out = append(out, item.Ref)
		}
		if len(out) >= 5 {
			break
		}
	}
	sort.Strings(out)
	return out
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
