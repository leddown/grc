package knowledge

import (
	"path/filepath"
	"strings"
	"testing"

	"grc/internal/db"
)

// newTestService builds a service over a real SQLite schema with a small,
// deliberately awkward catalog: records that share a word with the query but
// not its meaning, which is the case the counts exist to tell apart.
func newTestService(t *testing.T) *Service {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	nfrs := []struct{ key, id, summary, description, mapping, domain string }{
		{"56", "NFR-56", "Cardholder Data Environment must be isolated",
			"The CDE must be segmented from the corporate network by firewalls.", "SC-7", "Data Security"},
		{"61", "NFR-61", "Network-level security controls",
			"Segmentation must prevent components in the same subnet from communicating.", "SC-7", "Network Security"},
		{"67", "NFR-67", "Internal network connections",
			"Connections must originate in a higher security zone.", "SC-7", "Network Security"},
		{"12", "NFR-12", "Encryption of data in transit",
			"All traffic carrying personal data must use TLS 1.2 or higher.", "SC-8", "Cryptography"},
	}
	for _, n := range nfrs {
		if _, err := conn.Exec(`INSERT INTO security_nfrs
			(record_key, nfr_id, summary, description, nist_mapping, domain, issue_type,
			 additional_details, implementation)
			VALUES (?, ?, ?, ?, ?, ?, 'Requirement', '', '')`,
			n.key, n.id, n.summary, n.description, n.mapping, n.domain); err != nil {
			t.Fatalf("seed NFR %s: %v", n.key, err)
		}
	}

	if _, err := conn.Exec(`INSERT INTO security_nfr_control_links
		(nfr_key, control_id, matched) VALUES ('61', 'SC-7', 1), ('12', 'SC-8', 1), ('61', 'SC-7', 1)`); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO rcsa_controls
		(control_id, name, family, control_type, requirements, discussion,
		 in_low, in_moderate, in_high, in_privacy, mapping_baselines_json, threats_json)
		VALUES ('SC-7', 'Boundary Protection', 'SC', 'Technical',
		        'Monitor and control communications at external boundaries.',
		        'Boundary protection includes network segmentation.', 1, 1, 1, 0, '[]', '[]')`); err != nil {
		t.Fatalf("seed control: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO security_risk_register
		(risk_id, title, vulnerability, status, owner, inherent_score, residual_score)
		VALUES ('R-1', 'Flat network', 'The office network is not segmented.', 'Open', 'alice', 12, 9)`); err != nil {
		t.Fatalf("seed risk: %v", err)
	}

	return NewService(NewStore(conn))
}

// The question this whole API was built for: a count that distinguishes
// records about network segmentation from records that merely say "network".
func TestSearchReportsBothCounts(t *testing.T) {
	svc := newTestService(t)

	result, err := svc.Search(KindNFR, "network segmentation", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := result.Terms; len(got) != 2 || got[0] != "network" || got[1] != "segmentation" {
		t.Fatalf("terms = %v", got)
	}
	// One record uses both words. Another says "segmented" rather than
	// "segmentation" and so matches only "network" — the search does not stem,
	// which is exactly why the note tells the agent to try other word forms.
	if result.TotalAllTerms != 1 {
		t.Errorf("total_all_terms = %d, want the 1 record using both words", result.TotalAllTerms)
	}
	if result.TotalMatches <= result.TotalAllTerms {
		t.Errorf("total_matches = %d, want more than the %d matching every term",
			result.TotalMatches, result.TotalAllTerms)
	}

	// Every hit says which terms it matched, which is what makes the borderline
	// ones visible rather than silently counted.
	for _, hit := range result.Hits {
		if len(hit.Matched) == 0 {
			t.Errorf("hit %s reports no matched terms", hit.Item.Ref)
		}
	}
	if !strings.Contains(result.Note, "not stemmed") {
		t.Errorf("the caveat does not mention word forms: %q", result.Note)
	}

	// The unstemmed record must still be reachable, or the caveat would be
	// advice the caller cannot act on.
	stemmed, err := svc.Search(KindNFR, "segmented", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if stemmed.TotalMatches == 0 {
		t.Error(`searching "segmented" found nothing, so the advice to try other forms is useless`)
	}
}

func TestSearchPagesAndSaysSo(t *testing.T) {
	svc := newTestService(t)

	result, err := svc.Search(KindNFR, "network", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Returned != 1 {
		t.Fatalf("returned %d hits, want the limit of 1", result.Returned)
	}
	if !result.Truncated || result.TotalMatches <= 1 {
		t.Errorf("a truncated page did not say so: %+v", result)
	}
	// The body is a snippet on a search result; the whole record is what Get
	// is for.
	if len(result.Hits[0].Item.Body) > snippetLimit+10 {
		t.Errorf("search returned a full body (%d chars)", len(result.Hits[0].Item.Body))
	}
}

func TestSearchRequiresAQueryAndAKnownKind(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Search(KindNFR, "   ", 0); err == nil {
		t.Error("Search accepted an empty query")
	}
	if _, err := svc.Search("nonsense", "x", 0); err == nil {
		t.Error("Search accepted an unknown kind")
	}
}

// The index is the honest way to answer "how many", and is deliberately not
// offered for the corpora where reading everything is not a sane thing to do.
func TestIndexIsCompleteAndBounded(t *testing.T) {
	svc := newTestService(t)

	items, err := svc.Index(KindNFR)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("index has %d items, want the whole catalog of 4", len(items))
	}
	for _, item := range items {
		if item.Body != "" {
			t.Errorf("%s carries a body in the compact index", item.Ref)
		}
		if item.Ref == "" || item.Title == "" {
			t.Errorf("index entry is unusable: %+v", item)
		}
	}

	if _, err := svc.Index(KindRegulationClause); err == nil {
		t.Error("Index offered to dump every regulation clause")
	}
}

func TestGetReturnsTheWholeRecord(t *testing.T) {
	svc := newTestService(t)

	item, err := svc.Get(KindNFR, "61")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(item.Body, "same subnet") {
		t.Errorf("Get returned no body: %+v", item)
	}
	if item.Fields["nist_mapping"] != "SC-7" {
		t.Errorf("fields = %+v", item.Fields)
	}
	// The link table is deduplicated, so a doubled row does not double the
	// related list.
	if len(item.Related) != 1 || item.Related[0] != "SC-7" {
		t.Errorf("related = %v, want one SC-7", item.Related)
	}

	control, err := svc.Get(KindControl, "sc-7")
	if err != nil {
		t.Fatalf("Get control by lowercase ref: %v", err)
	}
	if control.Title != "Boundary Protection" || control.Fields["baselines"] != "low, moderate, high" {
		t.Errorf("control = %+v", control)
	}
}

// A model that guesses "SC7" should be corrected rather than told the control
// does not exist — the second answer sends it off inventing.
func TestGetSuggestsNearMisses(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.Get(KindControl, "SC7")
	if err == nil {
		t.Fatal("Get accepted a malformed ref")
	}
	if !strings.Contains(err.Error(), "did you mean") || !strings.Contains(err.Error(), "SC-7") {
		t.Errorf("error = %q, want it to suggest SC-7", err)
	}

	if _, err := svc.Get(KindControl, "ZZ-99"); err == nil ||
		strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error = %v, want a plain not-found for a ref nothing resembles", err)
	}
}

func TestOverviewCountsAndGroups(t *testing.T) {
	svc := newTestService(t)

	overview, err := svc.Overview()
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if overview.Counts[KindNFR] != 4 || overview.Counts[KindControl] != 1 {
		t.Errorf("counts = %+v", overview.Counts)
	}
	if len(overview.NFRDomains) == 0 || overview.NFRDomains[0].Name != "Network Security" {
		t.Errorf("domains = %+v, want Network Security first (it has the most)", overview.NFRDomains)
	}
	if overview.Counts[KindRisk] != 1 {
		t.Errorf("risks = %d", overview.Counts[KindRisk])
	}
	if overview.Note == "" {
		t.Error("overview offers no guidance on how to use the surface")
	}
}

// Searching a risk by the words in its vulnerability is the case that makes the
// register worth exposing at all.
func TestSearchAcrossKinds(t *testing.T) {
	svc := newTestService(t)

	risks, err := svc.Search(KindRisk, "segmented office network", 5)
	if err != nil {
		t.Fatalf("Search risks: %v", err)
	}
	if risks.TotalMatches == 0 || risks.Hits[0].Item.Ref != "R-1" {
		t.Errorf("risk search = %+v", risks)
	}

	controls, err := svc.Search(KindControl, "segmentation boundaries", 5)
	if err != nil {
		t.Fatalf("Search controls: %v", err)
	}
	if controls.TotalMatches == 0 || controls.Hits[0].Item.Ref != "SC-7" {
		t.Errorf("control search = %+v", controls)
	}
}

func TestQueryTerms(t *testing.T) {
	got := queryTerms("How many of the NFRs are about multi-factor auth?")
	for _, want := range []string{"how", "many", "nfrs", "multi-factor", "auth"} {
		if !contains(got, want) {
			t.Errorf("terms %v are missing %q", got, want)
		}
	}
	for _, unwanted := range []string{"of", "the", "are"} {
		if contains(got, unwanted) {
			t.Errorf("terms %v kept the stopword %q", got, unwanted)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
