package knowledge

import (
	"strconv"
	"time"
)

// BundleVersion is the format version stamped into every Bundle. Bump it if
// the shape changes in a way a consumer that already ingested one would have to
// be told about; the item shape itself is the same one the four query endpoints
// return, so a change there is a change here.
const BundleVersion = 1

// Bundle is this whole installation's compliance data as one JSON document:
// every Security NFR, every 800-53 control, every regulation clause and its
// coverage finding, every policy section, every risk, and every crisis exercise
// and its findings — with full bodies, not the snippets a search result carries.
//
// It exists for a different job than the four query endpoints beside it. Those
// answer a question at the moment it is asked, which needs this application to
// be reachable from wherever the agent runs. A Bundle is the same data as a
// file, so it can be uploaded into a Wintermute agent's own library and read
// there: the agent then has the full current state without a round trip, which
// is what makes it able to answer about this installation rather than about
// compliance in general.
//
// The trade to be deliberate about is staleness. A Bundle is a point in time.
// The live API is authoritative and a Bundle is not, so GeneratedAt is stamped
// on the document and repeated in Note — an agent quoting an uploaded bundle
// should be able to say how old it is, and re-uploading after a round of edits
// is what keeps it honest.
//
// It is also a defensible backup of the readable content on its own: unlike the
// table snapshot in internal/dbsync, it is denormalized and free of database
// ids, so it stays readable without this application to load it into.
type Bundle struct {
	FormatVersion int    `json:"format_version"`
	GeneratedAt   string `json:"generated_at"`
	// Note tells whoever (or whatever) reads the file what it is and how old.
	Note string `json:"note"`
	// Overview is the same orientation payload the API's /overview returns:
	// counts per kind, NFR domains, control families, risk statuses.
	Overview *Overview `json:"overview"`
	// Corpora holds every kind, in Kinds() order, each with its full items.
	Corpora []Corpus `json:"corpora"`
}

// Corpus is one kind's complete set of records.
type Corpus struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Items []Item `json:"items"`
}

// TotalItems is how many records the bundle carries across all kinds.
func (b *Bundle) TotalItems() int {
	total := 0
	for _, c := range b.Corpora {
		total += c.Count
	}
	return total
}

// Bundle builds the full export.
//
// Every corpus is read from the database rather than from the 45-second cache.
// That cache is right for a question, where a slightly stale answer beats
// re-reading every table per tool call, and wrong for an export: a backup taken
// ten seconds after an edit that silently predates it is exactly the failure
// this is meant to prevent. The freshly read corpora replace the cached ones,
// so the export leaves the query surface warm rather than colder.
func (s *Service) Bundle() (*Bundle, error) {
	kinds := Kinds()
	corpora := make([]Corpus, 0, len(kinds))
	for _, kind := range kinds {
		items, err := s.refresh(kind)
		if err != nil {
			return nil, err
		}
		corpora = append(corpora, Corpus{Kind: kind, Count: len(items), Items: items})
	}

	// Reads through the cache the loop above just filled, so the counts it
	// reports are the counts of the items in this same document.
	overview, err := s.Overview()
	if err != nil {
		return nil, err
	}

	generated := s.now().UTC().Format(time.RFC3339)
	bundle := &Bundle{
		FormatVersion: BundleVersion,
		GeneratedAt:   generated,
		Overview:      overview,
		Corpora:       corpora,
	}
	bundle.Note = "Full export of one GRC installation's compliance data, taken at " + generated +
		" and holding " + strconv.Itoa(bundle.TotalItems()) + " records. It is a point-in-time copy: " +
		"anything created or edited in the application after that timestamp is not in this file, so say " +
		"the export date when answering from it. Each item's ref is the identifier to quote, and its url " +
		"is the page in the application where a person can see the record."
	return bundle, nil
}

// refresh loads one corpus from the database and replaces the cached copy.
func (s *Service) refresh(kind string) ([]Item, error) {
	load, err := s.loader(kind)
	if err != nil {
		return nil, err
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

// BundleFilename is the download name for a bundle, stamped with its export
// time so two of them in a downloads folder are told apart by the thing that
// distinguishes them.
func BundleFilename(b *Bundle) string {
	stamp := "export"
	if t, err := time.Parse(time.RFC3339, b.GeneratedAt); err == nil {
		stamp = t.Format("20060102-150405")
	}
	return "grc-knowledge-" + stamp + ".json"
}
