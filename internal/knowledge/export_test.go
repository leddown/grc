package knowledge

import (
	"encoding/json"
	"strings"
	"testing"
)

// The bundle's whole purpose is that an agent reading the file knows what this
// installation holds, so it has to carry every kind and the full text of each
// record — not the trimmed items an index returns.
func TestBundleCarriesEveryKindWithFullBodies(t *testing.T) {
	svc := newTestService(t)

	bundle, err := svc.Bundle()
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	kinds := Kinds()
	if len(bundle.Corpora) != len(kinds) {
		t.Fatalf("bundle has %d corpora, want %d (one per kind)", len(bundle.Corpora), len(kinds))
	}
	for i, corpus := range bundle.Corpora {
		if corpus.Kind != kinds[i] {
			t.Errorf("corpus %d is %q, want %q", i, corpus.Kind, kinds[i])
		}
		if corpus.Count != len(corpus.Items) {
			t.Errorf("%s: count=%d but %d items", corpus.Kind, corpus.Count, len(corpus.Items))
		}
		if got := bundle.Overview.Counts[corpus.Kind]; got != corpus.Count {
			t.Errorf("%s: overview says %d, corpus holds %d", corpus.Kind, got, corpus.Count)
		}
	}

	nfrs := corpusOf(t, bundle, KindNFR)
	if nfrs.Count != 4 {
		t.Fatalf("nfr count=%d want 4", nfrs.Count)
	}
	// Index() strips bodies; the bundle must not, or an agent reading the file
	// gets a table of contents where it was promised the catalog.
	body := ""
	for _, item := range nfrs.Items {
		if item.Ref == "61" {
			body = item.Body
		}
	}
	if !strings.Contains(body, "Segmentation must prevent") {
		t.Errorf("NFR-61 body=%q, want the full description", body)
	}

	if bundle.TotalItems() == 0 {
		t.Fatal("bundle carries no records at all")
	}
	if !strings.Contains(bundle.Note, bundle.GeneratedAt) {
		t.Errorf("note should state the export date so an answer can cite it; got %q", bundle.Note)
	}
	if bundle.FormatVersion != BundleVersion {
		t.Errorf("format_version=%d want %d", bundle.FormatVersion, BundleVersion)
	}
}

// A backup taken ten seconds after an edit must contain the edit. The 45-second
// corpus cache is right for answering a question and wrong for an export, so
// Bundle reads through it.
func TestBundleIsNotServedFromTheCache(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.Bundle(); err != nil {
		t.Fatalf("first Bundle: %v", err)
	}

	if _, err := svc.store.conn.Exec(`INSERT INTO security_nfrs
		(record_key, nfr_id, summary, description, nist_mapping, domain, issue_type,
		 additional_details, implementation)
		VALUES ('99', 'NFR-99', 'Backup restoration testing', 'Restores must be exercised quarterly.',
		        'CP-9', 'Resilience', 'Requirement', '', '')`); err != nil {
		t.Fatalf("insert NFR after the first export: %v", err)
	}

	// The cached query surface is still on the old count — that is the cache
	// working, and the reason the export cannot use it.
	cached, err := svc.Items(KindNFR)
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(cached) != 4 {
		t.Fatalf("cached items=%d want 4 (test assumes the cache is still warm)", len(cached))
	}

	bundle, err := svc.Bundle()
	if err != nil {
		t.Fatalf("second Bundle: %v", err)
	}
	if got := corpusOf(t, bundle, KindNFR).Count; got != 5 {
		t.Fatalf("exported nfr count=%d want 5; the export missed an edit made after the cache was warmed", got)
	}
}

// The file has to be readable by something that is not this program: an agent's
// ingestion reads JSON, not Go.
func TestBundleMarshalsToJSON(t *testing.T) {
	svc := newTestService(t)
	bundle, err := svc.Bundle()
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	var decoded Bundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal bundle: %v", err)
	}
	if decoded.TotalItems() != bundle.TotalItems() {
		t.Fatalf("round-tripped bundle holds %d records, want %d", decoded.TotalItems(), bundle.TotalItems())
	}
	if name := BundleFilename(bundle); !strings.HasPrefix(name, "grc-knowledge-") || !strings.HasSuffix(name, ".json") {
		t.Fatalf("filename=%q, want a stamped grc-knowledge-*.json", name)
	}
}

func corpusOf(t *testing.T, bundle *Bundle, kind string) Corpus {
	t.Helper()
	for _, corpus := range bundle.Corpora {
		if corpus.Kind == kind {
			return corpus
		}
	}
	t.Fatalf("bundle has no %q corpus", kind)
	return Corpus{}
}
