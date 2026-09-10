package settings

import (
	"os"
	"path/filepath"
	"testing"

	"grc/internal/db"
	"grc/internal/secrets"
)

// openService builds the settings service the way app.newSettingsService does:
// a credential repository, a preference repository and a keyring resolved from
// the state directory. Calling it twice against the same directory is a restart.
func openService(t *testing.T, dir string) (*Service, func()) {
	t.Helper()
	conn, err := db.OpenSQLite(filepath.Join(dir, "settings.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	ring, err := secrets.Load(secrets.Options{StateDir: dir, AllowGenerate: true})
	if err != nil {
		t.Fatalf("secrets.Load: %v", err)
	}
	svc := NewService(NewSQLRepository(conn), ring).
		WithPreferences(NewSQLPreferenceRepository(conn))
	return svc, func() { _ = conn.Close() }
}

// TestAIProviderSettingsSurviveARestart is the guarantee the Settings page
// makes: what an operator configures there is still configured after the
// service is restarted.
//
// It restarts for real — the database is closed and the whole service, keyring
// included, is rebuilt from disk — because the parts that could break are the
// ones that only exist across a process boundary: a preference written to a
// connection that was never flushed, or a credential encrypted under a master
// key that was generated in memory and never persisted.
//
// The agent is named explicitly among them. Losing it is the quietest failure
// of the set: every question still gets answered, from the model's training
// data rather than from this installation's catalogs.
func TestAIProviderSettingsSurviveARestart(t *testing.T) {
	// The environment fallbacks must not be able to satisfy the assertions
	// below — what is read back has to have come off disk.
	for _, key := range []string{
		"GRC_SECRET_KEY", "ANTHROPIC_API_KEY", "WINTERMUTE_TOKEN",
		"WINTERMUTE_URL", "WINTERMUTE_BACKEND", "WINTERMUTE_MODEL", "WINTERMUTE_AGENT",
	} {
		t.Setenv(key, "")
	}

	dir := t.TempDir()
	want := map[string]string{
		PrefAIProvider:        ProviderWintermute,
		PrefClaudeModel:       "claude-sonnet-5",
		PrefWintermuteURL:     "https://wintermute.example.com",
		PrefWintermuteBackend: "workshop",
		PrefWintermuteModel:   "gemma3:12b",
		PrefWintermuteAgent:   "grc",
	}

	first, closeFirst := openService(t, dir)
	for key, value := range want {
		if err := first.SetPreference(key, value); err != nil {
			t.Fatalf("SetPreference(%q): %v", key, err)
		}
	}
	if err := first.Set(AnthropicAPIKey, "sk-ant-stored-value", "alice"); err != nil {
		t.Fatalf("Set anthropic key: %v", err)
	}
	if err := first.Set(WintermuteToken, "wintermute-token-value", "alice"); err != nil {
		t.Fatalf("Set wintermute token: %v", err)
	}
	closeFirst()

	// The restart.
	second, closeSecond := openService(t, dir)
	defer closeSecond()

	for key, value := range want {
		if got := second.Preference(key); got != value {
			t.Errorf("after restart %s = %q, want %q", key, got, value)
		}
	}
	if got := second.Preference(PrefWintermuteAgent); got != "grc" {
		t.Errorf("the agent did not survive the restart: %q", got)
	}

	// A credential is encrypted under a master key that also has to survive,
	// so this checks decryption rather than the presence of a row.
	if got, origin := second.Resolve(AnthropicAPIKey); got != "sk-ant-stored-value" || origin != OriginSettings {
		t.Errorf("anthropic key after restart = (%q, %q)", got, origin)
	}
	if got, origin := second.Resolve(WintermuteToken); got != "wintermute-token-value" || origin != OriginSettings {
		t.Errorf("wintermute token after restart = (%q, %q)", got, origin)
	}

	// The key file is what makes the credentials readable next time. Without
	// it the rows are unrecoverable ciphertext and every AI feature silently
	// falls back to the environment.
	if _, err := os.Stat(filepath.Join(dir, "secret.key")); err != nil {
		t.Errorf("no persisted master key beside the database: %v", err)
	}
}

// Clearing is a decision too, and it has to persist as firmly as setting does —
// an agent cleared on purpose must not come back on the next start.
func TestClearedAgentStaysCleared(t *testing.T) {
	t.Setenv("WINTERMUTE_AGENT", "")
	t.Setenv("GRC_SECRET_KEY", "")

	dir := t.TempDir()
	first, closeFirst := openService(t, dir)
	if err := first.SetPreference(PrefWintermuteAgent, "grc"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	if err := first.SetPreference(PrefWintermuteAgent, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	closeFirst()

	second, closeSecond := openService(t, dir)
	defer closeSecond()
	if got := second.Preference(PrefWintermuteAgent); got != "" {
		t.Errorf("cleared agent came back as %q", got)
	}
}

// An install configured through the environment keeps working, and a value
// stored in the page takes over from it — including across a restart, which is
// where the two sources could disagree.
func TestStoredAgentOutranksTheEnvironmentAcrossARestart(t *testing.T) {
	t.Setenv("GRC_SECRET_KEY", "")
	t.Setenv("WINTERMUTE_AGENT", "agent-from-the-environment")

	dir := t.TempDir()
	first, closeFirst := openService(t, dir)
	if got := first.Preference(PrefWintermuteAgent); got != "agent-from-the-environment" {
		t.Fatalf("with nothing stored the environment should supply the agent, got %q", got)
	}
	if err := first.SetPreference(PrefWintermuteAgent, "agent-from-the-page"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	closeFirst()

	second, closeSecond := openService(t, dir)
	defer closeSecond()
	if got := second.Preference(PrefWintermuteAgent); got != "agent-from-the-page" {
		t.Errorf("after restart the stored agent = %q, want the page's value to win", got)
	}
}
