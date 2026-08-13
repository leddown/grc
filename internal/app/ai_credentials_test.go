package app

import (
	"testing"

	"grc/internal/secrets"
	"grc/internal/settings"
)

// memSettingsRepo is a minimal in-memory credential store for these tests.
type memSettingsRepo struct{ rows map[string]string }

func (m *memSettingsRepo) Get(name string) (settings.Record, error) {
	ciphertext, ok := m.rows[name]
	if !ok {
		return settings.Record{}, settings.ErrNotFound
	}
	return settings.Record{Name: name, Ciphertext: ciphertext}, nil
}

func (m *memSettingsRepo) Put(rec settings.Record) error {
	m.rows[rec.Name] = rec.Ciphertext
	return nil
}

func (m *memSettingsRepo) Delete(name string) error {
	delete(m.rows, name)
	return nil
}

func (m *memSettingsRepo) List() ([]settings.Record, error) {
	out := make([]settings.Record, 0, len(m.rows))
	for name, ciphertext := range m.rows {
		out = append(out, settings.Record{Name: name, Ciphertext: ciphertext})
	}
	return out, nil
}

func newTestSettings(t *testing.T) *settings.Service {
	t.Helper()
	ring, err := secrets.NewKeyForTesting(make([]byte, secrets.KeyLength))
	if err != nil {
		t.Fatalf("NewKeyForTesting: %v", err)
	}
	return settings.NewService(&memSettingsRepo{rows: map[string]string{}}, ring)
}

func TestStoredAICredential(t *testing.T) {
	// The gateway handlers are package-level, so the store is a configured
	// singleton; restore it so these tests do not leak into the others.
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })

	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("WINTERMUTE_TOKEN", "")

	// No store configured at all: every provider resolves to empty rather than
	// panicking, which is the state the other unit tests run in.
	configureAICredentials(nil)
	for _, provider := range []string{"", "claude", "wintermute"} {
		if got := storedAICredential(provider); got != "" {
			t.Errorf("with no store, provider %q resolved to %q", provider, got)
		}
	}

	svc := newTestSettings(t)
	if err := svc.Set(settings.AnthropicAPIKey, "sk-ant-stored-value", "alice"); err != nil {
		t.Fatalf("Set anthropic: %v", err)
	}
	if err := svc.Set(settings.WintermuteToken, "wintermute-stored-token", "alice"); err != nil {
		t.Fatalf("Set wintermute: %v", err)
	}
	configureAICredentials(svc)

	tests := []struct {
		provider string
		want     string
	}{
		// An empty provider means Claude, matching the switch in aiChatAsk.
		{"", "sk-ant-stored-value"},
		{"claude", "sk-ant-stored-value"},
		{"wintermute", "wintermute-stored-token"},
	}
	for _, tc := range tests {
		if got := storedAICredential(tc.provider); got != tc.want {
			t.Errorf("storedAICredential(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

// TestStoredAICredentialFallsBackToEnvironment covers an install that has not
// used the Settings page: the environment must still resolve, or upgrading
// would silently turn off every AI feature.
func TestStoredAICredentialFallsBackToEnvironment(t *testing.T) {
	original := activeSettings
	t.Cleanup(func() { activeSettings = original })

	t.Setenv("ANTHROPIC_API_KEY", "env-key-value")
	configureAICredentials(newTestSettings(t))

	if got := storedAICredential("claude"); got != "env-key-value" {
		t.Errorf("storedAICredential = %q, want the environment fallback", got)
	}
}
