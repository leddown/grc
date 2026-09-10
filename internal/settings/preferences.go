package settings

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"grc/internal/db"
)

// Preference keys. These are install-wide, non-secret configuration: unlike a
// credential they are shown and edited in the Settings page.
const (
	// PrefAIProvider selects which provider answers AI questions.
	PrefAIProvider = "ai.provider"
	// PrefClaudeModel names the model every Claude question from this app is
	// asked on. Empty means aiprovider.DefaultClaudeModel.
	PrefClaudeModel = "ai.claude.model"
	// PrefWintermuteURL is the base URL of the Wintermute server that fronts
	// the local models on the network.
	PrefWintermuteURL = "ai.wintermute.url"
	// PrefWintermuteBackend pins a named backend on that server. Empty means
	// the server's own default.
	PrefWintermuteBackend = "ai.wintermute.backend"
	// PrefWintermuteModel pins a model within the chosen backend. Empty means
	// the backend's default.
	PrefWintermuteModel = "ai.wintermute.model"
	// PrefWintermuteAgent names an agent profile on that server — which
	// document library and which sources a question may be answered from.
	// Without one, the assistant answers from its training data, which for a
	// question about this installation's catalogs is no answer at all.
	PrefWintermuteAgent = "ai.wintermute.agent"
)

// Provider choices for PrefAIProvider.
const (
	// ProviderAuto prefers Wintermute when it is configured and falls back to
	// Claude, so a local model is used when one is available without the
	// cloud path disappearing when it is not.
	ProviderAuto = "auto"
	// ProviderClaude sends every question to api.anthropic.com.
	ProviderClaude = "claude"
	// ProviderWintermute sends every question to the Wintermute server, with
	// no cloud fallback. Choose this when questions must not leave the network.
	ProviderWintermute = "wintermute"
)

// prefDefaults are the values used when nothing has been stored. The default
// provider is Claude rather than auto so that adding this feature changes no
// existing install's behaviour until someone opts in.
var prefDefaults = map[string]string{
	PrefAIProvider:        ProviderClaude,
	PrefClaudeModel:       "",
	PrefWintermuteURL:     "",
	PrefWintermuteBackend: "",
	PrefWintermuteModel:   "",
	PrefWintermuteAgent:   "",
}

// prefEnvFallback maps a preference to the environment variable that supplies
// it when nothing is stored, so an install already configured through the
// environment keeps working.
var prefEnvFallback = map[string]string{
	PrefWintermuteURL:     "WINTERMUTE_URL",
	PrefWintermuteBackend: "WINTERMUTE_BACKEND",
	PrefWintermuteAgent:   "WINTERMUTE_AGENT",
	PrefWintermuteModel:   "WINTERMUTE_MODEL",
}

// ValidProvider reports whether v names a provider.
func ValidProvider(v string) bool {
	switch v {
	case ProviderAuto, ProviderClaude, ProviderWintermute:
		return true
	}
	return false
}

// PreferenceRepository reads and writes non-secret settings.
type PreferenceRepository interface {
	GetPreference(key string) (string, error)
	SetPreference(key, value string) error
}

// prefKeyPrefix namespaces these rows inside the shared app_state table so they
// cannot collide with the keys app.go already stores there.
const prefKeyPrefix = "setting."

// SQLPreferenceRepository stores preferences in the existing app_state table.
// A separate table would buy nothing: these are plain strings with no audit
// requirement, unlike the credentials in app_secrets.
type SQLPreferenceRepository struct {
	db *db.Conn
}

// NewSQLPreferenceRepository returns a preference repository.
func NewSQLPreferenceRepository(conn *db.Conn) *SQLPreferenceRepository {
	return &SQLPreferenceRepository{db: conn}
}

func (r *SQLPreferenceRepository) GetPreference(key string) (string, error) {
	var value string
	err := r.db.QueryRow(`SELECT value FROM app_state WHERE key = ?`, prefKeyPrefix+key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read preference %q: %w", key, err)
	}
	return value, nil
}

func (r *SQLPreferenceRepository) SetPreference(key, value string) error {
	_, err := r.db.Exec(`
		INSERT INTO app_state (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		prefKeyPrefix+key, value)
	if err != nil {
		return fmt.Errorf("store preference %q: %w", key, err)
	}
	return nil
}

// ErrUnknownPreference reports a key the Settings page does not manage.
var ErrUnknownPreference = errors.New("unknown preference")

// Preference returns a stored preference, falling back to the environment and
// then to the built-in default.
func (s *Service) Preference(key string) string {
	def, known := prefDefaults[key]
	if !known {
		return ""
	}
	if s.prefs != nil {
		if value, err := s.prefs.GetPreference(key); err == nil {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	if envVar, ok := prefEnvFallback[key]; ok {
		if value := strings.TrimSpace(getenv(envVar)); value != "" {
			return value
		}
	}
	return def
}

// SetPreference stores a preference. An empty value clears it, restoring the
// environment fallback or the default.
func (s *Service) SetPreference(key, value string) error {
	if _, known := prefDefaults[key]; !known {
		return fmt.Errorf("%w: %q", ErrUnknownPreference, key)
	}
	if s.prefs == nil {
		return errors.New("preferences are unavailable: no database")
	}
	value = strings.TrimSpace(value)
	if key == PrefAIProvider && value != "" && !ValidProvider(value) {
		return fmt.Errorf("unknown AI provider %q (want auto, claude or wintermute)", value)
	}
	return s.prefs.SetPreference(key, value)
}

// Preferences returns every managed preference and its effective value.
func (s *Service) Preferences() map[string]string {
	out := make(map[string]string, len(prefDefaults))
	for key := range prefDefaults {
		out[key] = s.Preference(key)
	}
	return out
}
