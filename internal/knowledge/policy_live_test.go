package knowledge

import (
	"path/filepath"
	"testing"

	"grc/internal/db"
)

// While a Policy Studio document is open the policy corpora are read fresh,
// and a projection invalidates them; otherwise they are cached like the rest.
func TestPolicyCorporaFollowTheStudio(t *testing.T) {
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "knowledge-live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO policy_documents (id, title, status) VALUES (1, 'ICT policy', 'draft')`)
	mustExec(`INSERT INTO policy_sections (document_id, ordinal, heading, body, section_kind, uid) VALUES (1, 0, 'Purpose', 'Why.', 'purpose', 'u1')`)

	svc := NewService(NewStore(conn))
	count := func() int {
		t.Helper()
		items, err := svc.Items(KindPolicyClause)
		if err != nil {
			t.Fatal(err)
		}
		return len(items)
	}
	first := count()
	mustExec(`INSERT INTO policy_sections (document_id, ordinal, heading, body, section_kind, uid) VALUES (1, 1, 'Scope', 'All staff.', 'scope', 'u2')`)
	if count() != first {
		t.Fatal("expected the cached corpus while nothing is open")
	}
	svc.Invalidate(KindPolicy, KindPolicyClause)
	if count() != first+1 {
		t.Fatal("Invalidate did not drop the cached corpus")
	}

	live := true
	svc.WithLivePolicies(func() bool { return live })
	mustExec(`INSERT INTO policy_sections (document_id, ordinal, heading, body, section_kind, uid) VALUES (1, 2, 'Roles', 'The CISO.', 'roles', 'u3')`)
	mustExec(`INSERT INTO policy_sections (document_id, ordinal, heading, body, section_kind, uid, detached_at) VALUES (1, 3, 'Gone', 'Removed text.', 'other', 'u4', '2026-09-28T00:00:00Z')`)
	if count() != first+2 {
		t.Fatal("an open Studio document must bypass the cache, and a detached section must not be served")
	}
}
