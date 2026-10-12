package policystudio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/reearth/ygo/crdt"

	"grc/internal/db"
	"grc/internal/policydocs"
)

// forEachDialect runs a test on SQLite, and on PostgreSQL when
// GRC_TEST_POSTGRES_URL names a throwaway database (the schema is created in
// it, and the documents a test creates are deleted again).
func forEachDialect(t *testing.T, fn func(t *testing.T, conn *db.Conn)) {
	t.Run("sqlite", func(t *testing.T) {
		conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "studio.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		fn(t, conn)
	})
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("GRC_TEST_POSTGRES_URL")
		if url == "" {
			t.Skip("GRC_TEST_POSTGRES_URL is not set")
		}
		conn, err := db.OpenPostgres(url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		fn(t, conn)
	})
}

func newDocument(t *testing.T, conn *db.Conn, sections ...policydocs.Section) (*policydocs.Service, policydocs.Document) {
	t.Helper()
	svc := policydocs.NewService(policydocs.NewSQLiteRepository(conn))
	doc, err := svc.CreateDocument(policydocs.Document{Title: "Studio test " + t.Name(), DocType: policydocs.TypePolicy, OwnerRole: "CISO"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = conn.Exec(`DELETE FROM policy_documents WHERE id = ?`, doc.ID) })
	for _, s := range sections {
		s.DocumentID = doc.ID
		if _, err := svc.CreateSection(s); err != nil {
			t.Fatal(err)
		}
	}
	return svc, doc
}

func sampleUpdate(t *testing.T, text string) []byte {
	t.Helper()
	root := SectionFromMarkdown("s-"+text, "purpose", "Purpose", text, counter())
	d := crdt.New()
	if err := WriteDoc(d, &Node{Type: "doc", Content: []*Node{root}}); err != nil {
		t.Fatal(err)
	}
	return crdt.EncodeStateAsUpdateV1(d, nil)
}

func projectBytes(t *testing.T, update []byte) string {
	t.Helper()
	d := crdt.New()
	if err := crdt.ApplyUpdateV1(d, update, nil); err != nil {
		t.Fatal(err)
	}
	root, problems := ReadDoc(d)
	if len(problems) > 0 {
		t.Fatalf("problems: %+v", problems)
	}
	raw, _ := json.Marshal(root)
	return string(raw)
}

func TestStoreSeedsExactlyOnce(t *testing.T) {
	forEachDialect(t, func(t *testing.T, conn *db.Conn) {
		svc, doc := newDocument(t, conn)
		store := NewStore(conn)
		var wins int
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ok, err := store.SeedIfAbsent(doc.ID, sampleUpdate(t, "seed"))
				if err != nil {
					t.Error(err)
					return
				}
				if ok {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("%d seeds won, want exactly 1", wins)
		}
		token, exists, err := store.stateToken(doc.ID)
		if err != nil || !exists {
			t.Fatalf("no state after seeding: %v", err)
		}
		after, _ := svc.GetDocument(doc.ID)
		if token == "" || token != after.ProjectionToken {
			t.Fatalf("seed did not pair the tokens: state %q, document %q", token, after.ProjectionToken)
		}
	})
}

func TestStoreAppendCompactLoad(t *testing.T) {
	forEachDialect(t, func(t *testing.T, conn *db.Conn) {
		_, doc := newDocument(t, conn)
		store := NewStore(conn)
		live := crdt.New()
		root := SectionFromMarkdown("s1", "purpose", "Purpose", "first", counter())
		if err := WriteDoc(live, &Node{Type: "doc", Content: []*Node{root}}); err != nil {
			t.Fatal(err)
		}
		if ok, err := store.SeedIfAbsent(doc.ID, crdt.EncodeStateAsUpdateV1(live, nil)); err != nil || !ok {
			t.Fatalf("seed: %v %v", ok, err)
		}
		// Three edits, stored the way ygo stores them: incremental updates.
		var stored int
		live.OnUpdate(func(update []byte, _ any) {
			if err := store.Append(doc.ID, update); err != nil {
				t.Error(err)
			}
			stored++
		})
		frag := live.GetXmlFragment(Fragment)
		for _, text := range []string{"alpha", "beta", "gamma"} {
			sec := SectionFromMarkdown("s-"+text, "scope", text, text, counter())
			el, err := buildElement(sec)
			if err != nil {
				t.Fatal(err)
			}
			live.Transact(func(txn *crdt.Transaction) { frag.InsertElement(txn, frag.Len(), el) })
		}
		if stored != 3 {
			t.Fatalf("stored %d updates, want 3", stored)
		}
		want := projectBytes(t, crdt.EncodeStateAsUpdateV1(live, nil))
		before, err := store.Load(doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := projectBytes(t, before); got != want {
			t.Fatalf("state + log differs from the live document")
		}
		if err := store.Compact(doc.ID); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM policy_doc_updates WHERE document_id = ?`, doc.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d updates left after compaction", n)
		}
		after, err := store.Load(doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := projectBytes(t, after); got != want {
			t.Fatal("compaction changed the document")
		}
	})
}

func TestStoreReplaceAndSnapshots(t *testing.T) {
	forEachDialect(t, func(t *testing.T, conn *db.Conn) {
		svc, doc := newDocument(t, conn)
		store := NewStore(conn)
		store.Retain = 3
		if _, err := store.SeedIfAbsent(doc.ID, sampleUpdate(t, "old")); err != nil {
			t.Fatal(err)
		}
		oldToken, _, _ := store.stateToken(doc.ID)
		if _, err := store.Snapshot(doc.ID, SnapshotPreMigration, SnapshotFormatLegacy, "admin", []byte(`[]`)); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			if _, err := store.Snapshot(doc.ID, SnapshotInterval, SnapshotFormatYjs, "", sampleUpdate(t, "snap")); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Replace(doc.ID, sampleUpdate(t, "new"), SnapshotSuperseded); err != nil {
			t.Fatal(err)
		}
		got, err := store.Load(doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if projectBytes(t, got) != projectBytes(t, sampleUpdate(t, "new")) {
			t.Fatal("replace did not replace the document")
		}
		token, _, _ := store.stateToken(doc.ID)
		after, _ := svc.GetDocument(doc.ID)
		if token == oldToken || token != after.ProjectionToken {
			t.Fatal("replace must re-pair the tokens")
		}
		snaps, err := store.ListSnapshots(doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		reasons := map[string]int{}
		for _, s := range snaps {
			reasons[s.Reason]++
		}
		// Retention counts the superseded copy (written by Replace, pruned on
		// the next Snapshot) and never prunes the pre-migration copy.
		if reasons[SnapshotPreMigration] != 1 || reasons[SnapshotSuperseded] != 1 || reasons[SnapshotInterval] > 3 {
			t.Fatalf("snapshots after retention: %v", reasons)
		}
		if _, err := store.Snapshot(doc.ID, SnapshotInterval, SnapshotFormatYjs, "", sampleUpdate(t, "x")); err != nil {
			t.Fatal(err)
		}
		snaps, _ = store.ListSnapshots(doc.ID)
		reasons = map[string]int{}
		for _, s := range snaps {
			reasons[s.Reason]++
		}
		if reasons[SnapshotPreMigration] != 1 || len(snaps) != 4 {
			t.Fatalf("retention of 3 plus the pre-migration copy, got %v", reasons)
		}
		if !reflect.DeepEqual(snaps[len(snaps)-1].Reason, SnapshotPreMigration) {
			t.Fatalf("oldest kept snapshot is %q, want the pre-migration copy", snaps[len(snaps)-1].Reason)
		}
	})
}
