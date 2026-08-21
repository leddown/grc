package crisisexercise

import (
	"sort"
	"strings"

	"grc/internal/knowledge"
)

// This file is the answer to "which of our controls does this exercise
// actually test?".
//
// An exercise that cannot answer it produces a report saying incident response
// was tested, which is not evidence of anything. An exercise that can produces
// a report saying that IR-4, IR-6, the entity's own NFRs on escalation and
// DORA Article 17 were exercised, that two of them failed, and where. That is
// the artefact an internal auditor, a supervisor and next year's testing
// programme all need.
//
// References resolve against the same catalogs internal/knowledge serves to the
// AI agent, so a citation in an exercise report and a citation in an agent's
// answer mean the same thing and point at the same record. The one kind this
// module adds is the authority catalog in catalog.go, which is seeded rather
// than stored because an entity must be able to cite DORA Article 18 without
// first having uploaded DORA.

// RefTarget is a resolved reference: enough to render a citation and link to it.
type RefTarget struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Group   string `json:"group,omitempty"`
	URL     string `json:"url,omitempty"`
}

// CatalogResolver reads this installation's compliance catalogs. It is the
// slice of internal/knowledge this module needs, expressed as an interface so
// the service can be tested without a database behind it.
type CatalogResolver interface {
	// Get returns one record, or an error when the reference does not resolve.
	Get(kind, ref string) (RefTarget, error)
	// Search returns candidates for a free-text query within one kind.
	Search(kind, query string, limit int) ([]RefTarget, error)
}

// ReferenceKinds lists the kinds a reference can point at, in the order the
// picker offers them.
func ReferenceKinds() []string {
	return []string{RefControl, RefNFR, RefAuthority, RefRegulationClause, RefPolicyClause, RefRisk}
}

// ReferenceKindLabel names a kind for display.
func ReferenceKindLabel(kind string) string {
	switch kind {
	case RefControl:
		return "NIST 800-53 control"
	case RefNFR:
		return "Security NFR"
	case RefRegulationClause:
		return "Regulation clause"
	case RefPolicyClause:
		return "Policy clause"
	case RefRisk:
		return "Risk register entry"
	case RefAuthority:
		return "Framework / authority"
	default:
		return kind
	}
}

// ---- knowledge adapter ----

// KnowledgeResolver adapts internal/knowledge's read-only service.
//
// Reusing it rather than querying the catalogs directly is deliberate: it
// already knows how to flatten six different record shapes into one, it caches
// each corpus briefly so an exercise resolving forty references does not read
// every table forty times, and it produces the URL a reader clicks through to.
// A second implementation would drift from it.
type KnowledgeResolver struct{ service *knowledge.Service }

// NewKnowledgeResolver wraps the knowledge service.
func NewKnowledgeResolver(service *knowledge.Service) *KnowledgeResolver {
	return &KnowledgeResolver{service: service}
}

func (k *KnowledgeResolver) Get(kind, ref string) (RefTarget, error) {
	if k == nil || k.service == nil {
		return RefTarget{}, notFound("catalog")
	}
	item, err := k.service.Get(kind, ref)
	if err != nil {
		return RefTarget{}, err
	}
	return fromKnowledgeItem(*item), nil
}

func (k *KnowledgeResolver) Search(kind, query string, limit int) ([]RefTarget, error) {
	if k == nil || k.service == nil {
		return nil, nil
	}
	result, err := k.service.Search(kind, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RefTarget, 0, len(result.Hits))
	for _, hit := range result.Hits {
		out = append(out, fromKnowledgeItem(hit.Item))
	}
	return out, nil
}

func fromKnowledgeItem(item knowledge.Item) RefTarget {
	return RefTarget{
		Kind:    item.Kind,
		Ref:     item.Ref,
		Title:   item.Title,
		Summary: item.Summary,
		Group:   item.Group,
		URL:     item.URL,
	}
}

// ---- resolution ----

// Resolve fills in a reference's title, URL and Known flag.
//
// Known is the important field. A model asked to cite the controls an inject
// exercises will occasionally produce a plausible identifier that does not
// exist in this installation's catalog, and a report that presents it beside
// the real ones is worse than a report with fewer citations. Resolution here
// is what catches it, and the page renders an unresolved reference as
// unresolved rather than dropping it — because "the model thinks this should
// exist and it does not" is itself worth seeing.
func Resolve(resolver CatalogResolver, ref Reference) Reference {
	ref.Ref = trim(ref.Ref)
	ref.RefKind = trim(ref.RefKind)
	if ref.Ref == "" {
		ref.Known = false
		return ref
	}

	if ref.RefKind == RefAuthority {
		if a, ok := AuthorityByKey(ref.Ref); ok {
			ref.Title = a.Name
			ref.URL = a.URL
			ref.Known = true
			if trim(ref.Note) == "" {
				ref.Note = a.Summary
			}
			return ref
		}
		ref.Known = false
		return ref
	}

	if resolver == nil {
		// Without a resolver the reference is recorded but unverified. Marking
		// it unknown would falsely accuse it; the alternative — claiming it
		// resolved — is worse.
		ref.Known = false
		return ref
	}

	target, err := resolver.Get(ref.RefKind, ref.Ref)
	if err != nil {
		ref.Known = false
		return ref
	}
	ref.Known = true
	if trim(ref.Title) == "" {
		ref.Title = target.Title
	}
	ref.URL = target.URL
	return ref
}

// Suggest returns candidate references for a piece of exercise text, best
// first, across every kind the resolver serves plus the authority catalog.
//
// It is used in two places: the reference picker on the design page, and the
// shortlist handed to the model when it is asked to cite what an inject
// exercises. The model judges a shortlist rather than browsing the catalog —
// which keeps the prompt small enough to reason over, and keeps every citation
// traceable to why the candidate was considered in the first place.
func Suggest(resolver CatalogResolver, text string, perKind int) []RefTarget {
	text = trim(text)
	if text == "" || perKind <= 0 {
		return nil
	}

	out := make([]RefTarget, 0, perKind*3)
	if resolver != nil {
		for _, kind := range []string{RefControl, RefNFR, RefRegulationClause, RefPolicyClause, RefRisk} {
			hits, err := resolver.Search(kind, text, perKind)
			if err != nil {
				continue
			}
			out = append(out, hits...)
		}
	}
	out = append(out, suggestAuthorities(text, perKind)...)
	return out
}

// suggestAuthorities ranks the seeded citation catalog by term overlap. The
// catalog is a few dozen entries, so a full scan with a simple score is both
// adequate and easier to reason about than reusing the BM25 ranker over a
// corpus this small.
func suggestAuthorities(text string, limit int) []RefTarget {
	terms := termsOf(text)
	if len(terms) == 0 {
		return nil
	}

	type scored struct {
		target RefTarget
		score  int
	}
	var ranked []scored
	for _, a := range Authorities() {
		haystack := strings.ToLower(a.Name + " " + a.Summary + " " + strings.Join(a.Topics, " "))
		score := 0
		for term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		ranked = append(ranked, scored{
			target: RefTarget{
				Kind:    RefAuthority,
				Ref:     a.Key,
				Title:   a.Name,
				Summary: a.Summary,
				Group:   a.Issuer,
				URL:     a.URL,
			},
			score: score,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	out := make([]RefTarget, 0, limit)
	for i, r := range ranked {
		if i >= limit {
			break
		}
		out = append(out, r.target)
	}
	return out
}

// termsOf lowercases and splits a query, dropping words too short or too common
// to discriminate. The stop list is small on purpose: an over-eager one would
// strip "data", "risk" and "control", which in this domain are the terms that
// matter.
func termsOf(text string) map[string]struct{} {
	stop := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
		"from": true, "has": true, "have": true, "are": true, "was": true, "were": true,
		"will": true, "not": true, "but": true, "what": true, "when": true, "which": true,
		"their": true, "they": true, "them": true, "into": true, "been": true, "than": true,
	}
	out := map[string]struct{}{}
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-'
	}) {
		if len(raw) < 4 || stop[raw] {
			continue
		}
		out[raw] = struct{}{}
	}
	return out
}

// ---- grouping for rendering ----

// ReferenceIndex groups an exercise's references by owner, which is how every
// page and the report read them: "what does this inject cite" rather than
// "list all references".
type ReferenceIndex map[string][]Reference

// IndexReferences builds the lookup. The key is ownerKind + "/" + ownerID.
func IndexReferences(refs []Reference) ReferenceIndex {
	index := ReferenceIndex{}
	for _, ref := range refs {
		key := refOwnerKey(ref.OwnerKind, ref.OwnerID)
		index[key] = append(index[key], ref)
	}
	return index
}

// For returns the references attached to one owner.
func (i ReferenceIndex) For(ownerKind string, ownerID int64) []Reference {
	return i[refOwnerKey(ownerKind, ownerID)]
}

func refOwnerKey(ownerKind string, ownerID int64) string {
	return ownerKind + "/" + itoa(ownerID)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// CoverageRow is one cited item and everywhere in the exercise that cites it.
// It is the coverage view: which controls this exercise touched, how often, and
// whether the parts that cited them passed.
type CoverageRow struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Known bool   `json:"known"`
	// Citations counts how many parts of the exercise cite this item.
	Citations int `json:"citations"`
	// Owners names them, for the report's cross-reference table.
	Owners []string `json:"owners"`
	// Findings counts the findings raised against parts that cite this item —
	// the number that turns a coverage list into an assessment.
	Findings int `json:"findings"`
}

// Coverage inverts the reference table: from "what does this inject cite" to
// "what cites this control, and did any of it fail".
//
// The Findings count is deliberately narrow — it counts findings that cite the
// item themselves, not findings raised anywhere in a phase that happens to cite
// it. The broader count would be easy to produce and would overstate the case:
// a finding about the call tree is not evidence against every control the
// crisis phase touches, and a coverage table that implies otherwise is the kind
// of thing an auditor takes apart.
func Coverage(refs []Reference) []CoverageRow {
	rows := map[string]*CoverageRow{}
	for _, ref := range refs {
		key := ref.RefKind + "/" + strings.ToLower(ref.Ref)
		row, ok := rows[key]
		if !ok {
			row = &CoverageRow{Kind: ref.RefKind, Ref: ref.Ref, Title: ref.Title, URL: ref.URL, Known: ref.Known}
			rows[key] = row
		}
		row.Citations++
		if row.Title == "" {
			row.Title = ref.Title
		}
		if row.URL == "" {
			row.URL = ref.URL
		}
		row.Known = row.Known || ref.Known
		row.Owners = appendUnique(row.Owners, ref.OwnerKind)
		if ref.OwnerKind == OwnerFinding {
			row.Findings++
		}
	}

	out := make([]CoverageRow, 0, len(rows))
	for _, row := range rows {
		sort.Strings(row.Owners)
		out = append(out, *row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

func appendUnique(list []string, value string) []string {
	for _, v := range list {
		if v == value {
			return list
		}
	}
	return append(list, value)
}
