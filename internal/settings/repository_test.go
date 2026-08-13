package settings

import (
	"errors"
	"path/filepath"
	"testing"

	"grc/internal/db"
	"grc/internal/secrets"
)

// newSQLiteRepo exercises the real schema and SQL rather than the in-memory
// double used elsewhere in this package: ON CONFLICT DO UPDATE has to behave on
// the backend that actually ships.
func newSQLiteRepo(t *testing.T) *SQLRepository {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewSQLRepository(conn)
}

func TestSQLRepositoryRoundTrip(t *testing.T) {
	repo := newSQLiteRepo(t)

	if _, err := repo.Get(AnthropicAPIKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on an empty table = %v, want ErrNotFound", err)
	}

	first := Record{Name: AnthropicAPIKey, Ciphertext: "ciphertext-one", UpdatedAt: "2026-08-13T00:00:00Z", UpdatedBy: "alice"}
	if err := repo.Put(first); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := repo.Get(AnthropicAPIKey)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != first {
		t.Errorf("Get = %+v, want %+v", got, first)
	}

	// Upsert: a second Put under the same name must replace, not duplicate or
	// fail on the primary key.
	second := Record{Name: AnthropicAPIKey, Ciphertext: "ciphertext-two", UpdatedAt: "2026-08-14T00:00:00Z", UpdatedBy: "bob"}
	if err := repo.Put(second); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	got, err = repo.Get(AnthropicAPIKey)
	if err != nil {
		t.Fatalf("Get after replace: %v", err)
	}
	if got != second {
		t.Errorf("after replace Get = %+v, want %+v", got, second)
	}
	rows, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("List returned %d rows after an upsert, want 1", len(rows))
	}

	if err := repo.Delete(AnthropicAPIKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(AnthropicAPIKey); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
	// Deleting something absent is not an error: the Settings page offers
	// "clear" whether or not a value is stored.
	if err := repo.Delete(AnthropicAPIKey); err != nil {
		t.Errorf("Delete on a missing row = %v, want nil", err)
	}
}

// TestServiceOverSQLite is the end-to-end path the app uses: encrypt, persist,
// reload with a fresh service, decrypt.
func TestServiceOverSQLite(t *testing.T) {
	repo := newSQLiteRepo(t)
	ring, err := secrets.NewKeyForTesting(make([]byte, secrets.KeyLength))
	if err != nil {
		t.Fatalf("NewKeyForTesting: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")

	svc := NewService(repo, ring)
	if err := svc.Set(AnthropicAPIKey, "sk-ant-persisted-value", "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A separate service over the same rows stands in for a process restart.
	reloaded := NewService(repo, ring)
	value, origin := reloaded.Resolve(AnthropicAPIKey)
	if value != "sk-ant-persisted-value" || origin != OriginSettings {
		t.Errorf("after reload = (%q, %q), want the stored value", value, origin)
	}
}
