package settings

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"grc/internal/db"
)

// Preference keys. These are install-wide, non-secret configuration: unlike a
// credential they are shown and edited in the Settings page.
const (
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
	// PrefCrisisAgent names the Wintermute agent every Crisis Exercise question
	// goes to, so exercises can have a library and sources of their own. Empty
	// means the same agent as the rest of the application.
	PrefCrisisAgent = "ai.crisis.agent"
	// PrefCrisisSendExercise is "true" when a Crisis Exercise question carries
	// the exercise it is about instead of the agent fetching it from the
	// knowledge API — for an agent that cannot reach this server.
	PrefCrisisSendExercise = "ai.crisis.send_exercise"
	// PrefPolicyAgent names the Wintermute agent the Policy Studio's AI
	// proposals and its Ask AI questions go to. Empty means the same agent as
	// the rest of the application.
	PrefPolicyAgent = "ai.policy.agent"
	// PrefPolicySendDocument is "true" when every Ask AI question on a Studio
	// page carries the document to a Wintermute agent, rather than when the
	// conversation starts and whenever the document has changed.
	PrefPolicySendDocument = "ai.policy.send_document"
	// PrefStudioGuestLinks is "true" when administrators may invite people
	// from outside the installation into a Policy Studio document. Off by
	// default: the guest pages are reachable without an account.
	PrefStudioGuestLinks = "studio.guest_links"
	// PrefStudioInviteHours is how long a new invitation lasts unless its
	// creator says otherwise, in hours (1 to 168). Empty means 8.
	PrefStudioInviteHours = "studio.invite_hours"
)

// prefDefaults are the values used when nothing has been stored. There is no
// provider choice: every AI question goes to the Wintermute server, which
// reaches Claude as one of its backends (wintermute decision note 0005).
var prefDefaults = map[string]string{
	PrefWintermuteURL:      "",
	PrefWintermuteBackend:  "",
	PrefWintermuteModel:    "",
	PrefWintermuteAgent:    "",
	PrefCrisisAgent:        "",
	PrefCrisisSendExercise: "",
	PrefPolicyAgent:        "",
	PrefPolicySendDocument: "",
	PrefStudioGuestLinks:   "",
	PrefStudioInviteHours:  "",
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
	// Stored as "true" or nothing, so a value that merely looks like a yes
	// cannot be read as on by one reader and off by another.
	if key == PrefStudioInviteHours && value != "" {
		hours, err := strconv.Atoi(value)
		if err != nil || hours < 1 || hours > 168 {
			return fmt.Errorf("%s must be a whole number of hours from 1 to 168, not %q", key, value)
		}
	}
	if (key == PrefCrisisSendExercise || key == PrefPolicySendDocument || key == PrefStudioGuestLinks) && value != "" {
		on, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be true or false, not %q", key, value)
		}
		value = ""
		if on {
			value = "true"
		}
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
