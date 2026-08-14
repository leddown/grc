package regcoverage

import (
	"fmt"
	"sort"
	"strings"

	"grc/internal/nfrenrich"
	"grc/internal/regmap/nist"
	"grc/internal/securitynfr"
)

// Candidate is one item of this installation's compliance vocabulary — a
// Security NFR or an 800-53 control — in the shape retrieval and the prompt
// both need.
type Candidate struct {
	Kind  string
	Ref   string
	Title string
	// Text is what retrieval scores and what the prompt shows: enough to judge
	// applicability, not the entire control narrative.
	Text string
}

// NFRLister is the slice of the Security NFR service this module needs. It
// reads the catalog and nothing else — this module proposes mappings, it never
// edits an NFR.
type NFRLister interface {
	List(search, domain string) ([]securitynfr.NFR, error)
}

// Catalog is the candidate corpus, loaded once per analysis run.
type Catalog struct {
	Candidates []Candidate
	// byRef resolves a model-supplied reference back to a real catalog item,
	// which is how an invented control ID gets caught.
	byRef map[string]Candidate
	// Controls is the 800-53 catalog, kept for seed-crosswalk titles.
	Controls *nist.Catalog
}

// LoadCatalog builds the corpus from the Security NFR catalog and the embedded
// 800-53 dataset — the same controls the rest of the app serves, so a mapping
// names something the reader can click through to.
func LoadCatalog(nfrs NFRLister) (*Catalog, error) {
	controls, err := nist.LoadEmbedded(false)
	if err != nil {
		return nil, fmt.Errorf("load 800-53 catalog: %w", err)
	}

	cat := &Catalog{byRef: map[string]Candidate{}, Controls: controls}
	for _, c := range controls.Controls {
		cat.add(Candidate{
			Kind:  KindControl,
			Ref:   c.ID,
			Title: c.Title,
			Text:  truncate(c.ControlText, 600),
		})
	}

	if nfrs != nil {
		list, err := nfrs.List("", "")
		if err != nil {
			return nil, fmt.Errorf("load security NFR catalog: %w", err)
		}
		for _, n := range list {
			ref := strings.TrimSpace(n.Key)
			if ref == "" {
				continue
			}
			body := n.Description
			if n.Domain != "" {
				body = n.Domain + ". " + body
			}
			if n.NISTMapping != "" {
				body += " NIST: " + n.NISTMapping + "."
			}
			cat.add(Candidate{
				Kind:  KindNFR,
				Ref:   ref,
				Title: firstNonEmpty(n.Summary, n.ID, ref),
				Text:  truncate(body, 600),
			})
		}
	}

	sort.SliceStable(cat.Candidates, func(i, j int) bool {
		if cat.Candidates[i].Kind != cat.Candidates[j].Kind {
			return cat.Candidates[i].Kind < cat.Candidates[j].Kind
		}
		return cat.Candidates[i].Ref < cat.Candidates[j].Ref
	})
	return cat, nil
}

func (c *Catalog) add(cand Candidate) {
	c.Candidates = append(c.Candidates, cand)
	c.byRef[refKey(cand.Kind, cand.Ref)] = cand
}

// Lookup resolves a reference the model returned. The bool is the answer to
// "does this exist", which the report shows rather than quietly dropping.
func (c *Catalog) Lookup(kind, ref string) (Candidate, bool) {
	cand, ok := c.byRef[refKey(kind, ref)]
	if ok {
		return cand, true
	}
	// A model that puts a control ID under the NFR heading, or the reverse, has
	// still named something real; the report is more useful for correcting the
	// kind than for dropping the row.
	other := KindNFR
	if kind == KindNFR {
		other = KindControl
	}
	if cand, ok := c.byRef[refKey(other, ref)]; ok {
		return cand, true
	}
	return Candidate{}, false
}

// Size reports the corpus size, for the analysis log.
func (c *Catalog) Size() (controls, nfrs int) {
	for _, cand := range c.Candidates {
		if cand.Kind == KindControl {
			controls++
			continue
		}
		nfrs++
	}
	return controls, nfrs
}

func refKey(kind, ref string) string {
	return kind + "\x00" + strings.ToUpper(strings.TrimSpace(ref))
}

// Retriever shortlists catalog candidates for one section. It is an interface
// so a stronger retriever can replace the lexical one without touching the
// analysis code.
type Retriever interface {
	Shortlist(section Section, candidates []Candidate, perKind int) []Candidate
}

// LexicalRetriever ranks candidates with the BM25 scorer the NFR enrichment
// module already ships. The scorer is shared rather than reimplemented; what
// differs here is the direction — the query is a regulation article and the
// corpus is the catalog, rather than the other way round — and the selection
// rule, which takes the top few of each kind rather than applying that module's
// evidence threshold. A section is entitled to a shortlist even when nothing
// matches strongly: "nothing in the catalog covers this" is a finding, and it
// is one the model can only reach if it has seen the near misses.
type LexicalRetriever struct{ ranker nfrenrich.Retriever }

func NewLexicalRetriever() LexicalRetriever {
	return LexicalRetriever{ranker: nfrenrich.NewBM25Retriever()}
}

// Shortlist returns at most perKind candidates of each kind, best first.
func (r LexicalRetriever) Shortlist(section Section, candidates []Candidate, perKind int) []Candidate {
	if perKind <= 0 || len(candidates) == 0 {
		return nil
	}

	chunks := make([]nfrenrich.Chunk, 0, len(candidates))
	index := make(map[int64]Candidate, len(candidates))
	for i, cand := range candidates {
		id := int64(i)
		index[id] = cand
		chunks = append(chunks, nfrenrich.Chunk{
			ID:      id,
			Ordinal: i,
			Heading: cand.Ref + " " + cand.Title,
			Text:    cand.Text,
		})
	}

	// The title carries the section's subject in a few words; the body says the
	// same thing in several hundred. Marking the title high-signal keeps a long
	// article from retrieving on its incidental vocabulary. The "!" convention
	// is the ranker's.
	query := "!" + section.Label + " " + section.Title + "\n" + section.Body

	// Ranked over the whole corpus, then split per kind: ranking each kind
	// separately would compare BM25 scores computed over different corpora,
	// which are not comparable.
	ranked := r.ranker.Rank(query, chunks, 0)

	out := make([]Candidate, 0, perKind*2)
	counts := map[string]int{}
	for _, hit := range ranked {
		cand, ok := index[hit.Chunk.ID]
		if !ok || counts[cand.Kind] >= perKind {
			continue
		}
		counts[cand.Kind]++
		out = append(out, cand)
	}
	return out
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndexAny(cut, " \n"); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
