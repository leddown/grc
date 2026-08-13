package settings

import (
	"errors"
	"strings"
	"testing"

	"grc/internal/secrets"
)

// memRepo is an in-memory Repository, so these tests exercise the service's
// resolution and encryption logic without a database.
type memRepo struct {
	rows    map[string]Record
	getErr  error
	putErr  error
	getHits int
}

func newMemRepo() *memRepo { return &memRepo{rows: map[string]Record{}} }

func (m *memRepo) Get(name string) (Record, error) {
	m.getHits++
	if m.getErr != nil {
		return Record{}, m.getErr
	}
	rec, ok := m.rows[name]
	if !ok {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

func (m *memRepo) Put(rec Record) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.rows[rec.Name] = rec
	return nil
}

func (m *memRepo) Delete(name string) error {
	delete(m.rows, name)
	return nil
}

func (m *memRepo) List() ([]Record, error) {
	out := make([]Record, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, nil
}

func testService(t *testing.T) (*Service, *memRepo) {
	t.Helper()
	ring, err := secrets.NewKeyForTesting(make([]byte, secrets.KeyLength))
	if err != nil {
		t.Fatalf("NewKeyForTesting: %v", err)
	}
	repo := newMemRepo()
	return NewService(repo, ring), repo
}

func TestResolvePrefersStoredOverEnvironment(t *testing.T) {
	svc, _ := testService(t)
	t.Setenv("ANTHROPIC_API_KEY", "env-key-value")

	// Environment only.
	value, origin := svc.Resolve(AnthropicAPIKey)
	if value != "env-key-value" || origin != OriginEnv {
		t.Fatalf("with env only: got (%q, %q), want (env-key-value, %q)", value, origin, OriginEnv)
	}

	// Stored wins once set.
	if err := svc.Set(AnthropicAPIKey, "stored-key-value", "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, origin = svc.Resolve(AnthropicAPIKey)
	if value != "stored-key-value" || origin != OriginSettings {
		t.Fatalf("with stored: got (%q, %q), want (stored-key-value, %q)", value, origin, OriginSettings)
	}

	// Clearing falls back to the environment rather than disabling the feature.
	if err := svc.Clear(AnthropicAPIKey); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	value, origin = svc.Resolve(AnthropicAPIKey)
	if value != "env-key-value" || origin != OriginEnv {
		t.Fatalf("after clear: got (%q, %q), want the env fallback", value, origin)
	}
}

func TestResolveWithNothingConfigured(t *testing.T) {
	svc, _ := testService(t)
	t.Setenv("ANTHROPIC_API_KEY", "")
	value, origin := svc.Resolve(AnthropicAPIKey)
	if value != "" || origin != OriginNone {
		t.Errorf("got (%q, %q), want empty and %q", value, origin, OriginNone)
	}
}

func TestStoredValueIsEncryptedAtRest(t *testing.T) {
	svc, repo := testService(t)
	const secret = "sk-ant-super-secret-value"
	if err := svc.Set(AnthropicAPIKey, secret, "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	rec := repo.rows[AnthropicAPIKey]
	if rec.Ciphertext == "" {
		t.Fatal("nothing was stored")
	}
	if strings.Contains(rec.Ciphertext, secret) {
		t.Error("the stored row contains the plaintext credential")
	}
	if rec.UpdatedBy != "alice" || rec.UpdatedAt == "" {
		t.Errorf("audit trail not recorded: updatedBy=%q updatedAt=%q", rec.UpdatedBy, rec.UpdatedAt)
	}
}

func TestSetValidation(t *testing.T) {
	svc, _ := testService(t)
	tests := []struct {
		name    string
		cred    string
		value   string
		wantErr string
	}{
		{"unknown credential", "not_a_credential", "a-long-enough-value", "unknown credential"},
		{"truncated paste", AnthropicAPIKey, "abc", "truncated"},
		{"whitespace only", AnthropicAPIKey, "        ", "truncated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.Set(tc.cred, tc.value, "alice")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Set error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestSetTrimsSurroundingWhitespace(t *testing.T) {
	svc, _ := testService(t)
	// Pasting from a terminal or a password manager routinely picks up a
	// trailing newline, which the provider would reject as a bad key.
	if err := svc.Set(AnthropicAPIKey, "  sk-ant-padded-value\n", "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := svc.Get(AnthropicAPIKey); got != "sk-ant-padded-value" {
		t.Errorf("stored value = %q, want the trimmed form", got)
	}
}

// TestWithoutKeyringStorageIsRefusedButEnvStillWorks covers a deployment where
// no master key could be resolved: AI features must keep working from the
// environment rather than the whole app failing.
func TestWithoutKeyringStorageIsRefusedButEnvStillWorks(t *testing.T) {
	svc := NewService(newMemRepo(), nil)
	t.Setenv("ANTHROPIC_API_KEY", "env-key-value")

	if svc.StorageAvailable() {
		t.Error("StorageAvailable reported true without a keyring")
	}
	if err := svc.Set(AnthropicAPIKey, "stored-value", "alice"); !errors.Is(err, ErrNoKeyring) {
		t.Errorf("Set error = %v, want ErrNoKeyring", err)
	}
	if value, origin := svc.Resolve(AnthropicAPIKey); value != "env-key-value" || origin != OriginEnv {
		t.Errorf("resolve = (%q, %q), want the env fallback to still work", value, origin)
	}
}

// TestUndecryptableRowFallsBackToEnv covers a rotated or replaced master key:
// the stored row cannot be opened, and that must degrade to the environment
// rather than take the feature down.
func TestUndecryptableRowFallsBackToEnv(t *testing.T) {
	svc, repo := testService(t)
	t.Setenv("ANTHROPIC_API_KEY", "env-key-value")
	repo.rows[AnthropicAPIKey] = Record{Name: AnthropicAPIKey, Ciphertext: "not-openable-ciphertext"}

	value, origin := svc.Resolve(AnthropicAPIKey)
	if value != "env-key-value" || origin != OriginEnv {
		t.Errorf("got (%q, %q), want the env fallback", value, origin)
	}
	st, err := svc.Status(AnthropicAPIKey)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Origin != OriginEnv {
		t.Errorf("status origin = %q, want %q", st.Origin, OriginEnv)
	}
}

func TestStatusNeverDisclosesTheCredential(t *testing.T) {
	svc, _ := testService(t)
	const secret = "sk-ant-super-secret-value"
	if err := svc.Set(AnthropicAPIKey, secret, "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	st, err := svc.Status(AnthropicAPIKey)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Configured || st.Origin != OriginSettings {
		t.Errorf("status = %+v, want configured from settings", st)
	}
	if strings.Contains(st.Hint, "super-secret") || len(st.Hint) > 8 {
		t.Errorf("hint %q discloses too much of the credential", st.Hint)
	}
	if !strings.HasSuffix(secret, strings.TrimPrefix(st.Hint, "****")) {
		t.Errorf("hint %q does not match the tail of the stored value", st.Hint)
	}
	if st.UpdatedBy != "alice" {
		t.Errorf("updatedBy = %q, want alice", st.UpdatedBy)
	}
}

func TestStatusesCoversEveryManagedCredential(t *testing.T) {
	svc, _ := testService(t)
	got, err := svc.Statuses()
	if err != nil {
		t.Fatalf("Statuses: %v", err)
	}
	if len(got) != len(definitions) {
		t.Fatalf("got %d statuses, want %d", len(got), len(definitions))
	}
	for i, st := range got {
		if st.Name != definitions[i].name {
			t.Errorf("status %d = %q, want %q (display order must be stable)", i, st.Name, definitions[i].name)
		}
		if st.EnvVar != definitions[i].envVar {
			t.Errorf("%s env var = %q, want %q", st.Name, st.EnvVar, definitions[i].envVar)
		}
	}
}

// TestResolveCachesLookups keeps the hot path — every AI request resolving a
// key — off the database, while a write still takes effect immediately.
func TestResolveCachesLookups(t *testing.T) {
	svc, repo := testService(t)
	if err := svc.Set(AnthropicAPIKey, "stored-key-value", "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	repo.getHits = 0
	for i := 0; i < 5; i++ {
		if got := svc.Get(AnthropicAPIKey); got != "stored-key-value" {
			t.Fatalf("Get = %q", got)
		}
	}
	if repo.getHits > 1 {
		t.Errorf("repository was read %d times for 5 resolutions; expected caching", repo.getHits)
	}

	if err := svc.Set(AnthropicAPIKey, "replacement-value", "bob"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := svc.Get(AnthropicAPIKey); got != "replacement-value" {
		t.Errorf("after replacement Get = %q, want the new value (cache not invalidated)", got)
	}
}
