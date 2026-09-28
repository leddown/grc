package clientprofile

import (
	"path/filepath"
	"testing"

	"grc/internal/db"
)

func newService(t *testing.T) (*Service, *db.Conn) {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "clients.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewService(conn), conn
}

func TestProfilesAndFacts(t *testing.T) {
	s, conn := newService(t)
	changed := 0
	s.OnFactsChanged(func(int64) { changed++ })

	if _, err := s.Create(Profile{Name: "  "}); err == nil {
		t.Fatal("a profile needs a name")
	}
	p, err := s.Create(Profile{Name: " Example Bank AG ", CRMRef: "42"})
	if err != nil || p.Name != "Example Bank AG" {
		t.Fatalf("create: %+v %v", p, err)
	}
	for _, bad := range []struct{ key, typ string }{{"Bad Key", "text"}, {"1starts_with_digit", "text"}, {"ok_key", "colour"}} {
		if _, err := s.SetFact(p.ID, bad.key, "x", bad.typ, "alice"); err == nil {
			t.Errorf("SetFact(%q, %q) accepted", bad.key, bad.typ)
		}
	}
	if _, err := s.SetFact(p.ID, "legal_entity_name", "Example Bank AG", "", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetFact(p.ID, "legal_entity_name", "Example Bank Aktiengesellschaft", "text", "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetFact(p.ID, "scope_exclusions", "", "text", "bob"); err != nil {
		t.Fatal(err)
	}
	facts, _ := s.Facts(p.ID)
	if f := facts["legal_entity_name"]; f.Value != "Example Bank Aktiengesellschaft" || f.UpdatedBy != "bob" || f.ValueType != "text" {
		t.Fatalf("upsert: %+v", f)
	}
	values, _ := s.Values(p.ID)
	if _, ok := values["scope_exclusions"]; ok {
		t.Fatal("an empty value must count as unresolved")
	}
	if changed != 3 {
		t.Fatalf("facts-changed callback ran %d times, want 3", changed)
	}
	if _, err := s.SetFact(9999, "k", "v", "text", "a"); err != ErrNotFound {
		t.Fatalf("a fact for an unknown client: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO policy_documents (title, client_profile_id) VALUES ('Doc', ?)`, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(p.ID); err == nil {
		t.Fatal("a profile a document uses must not be deleted")
	}
	if _, err := conn.Exec(`UPDATE policy_documents SET client_profile_id = 0`); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if facts, _ := s.Facts(p.ID); len(facts) != 0 {
		t.Fatal("deleting a profile deletes its facts")
	}
}
