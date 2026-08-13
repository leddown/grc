package settings

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"grc/internal/secrets"
)

// minCredentialLength rejects an obviously truncated paste. It is deliberately
// permissive: provider key formats change, and this is not the place to encode
// a guess about them.
const minCredentialLength = 8

// ErrUnknownCredential reports a name the Settings page does not manage.
var ErrUnknownCredential = errors.New("unknown credential")

// ErrNoKeyring reports that credentials cannot be stored because no master key
// is available. Reads still work — the environment fallback is unaffected.
var ErrNoKeyring = errors.New("credential storage is unavailable: no master key")

// Service resolves credentials for the whole app and manages the stored ones.
//
// Resolution prefers a stored credential and falls back to the environment, so
// an install that has always used ANTHROPIC_API_KEY keeps working untouched
// while the Settings page takes over the moment a key is saved there.
type Service struct {
	repo Repository
	// prefs holds the non-secret, operator-visible settings. It may be nil in
	// tests that only exercise credentials.
	prefs   PreferenceRepository
	keyring *secrets.Keyring

	// cache holds decrypted credentials so the common path — every AI request
	// resolving a key — does not decrypt on each call. Invalidated on write.
	mu    sync.RWMutex
	cache map[string]string
	// loaded tracks names whose absence is also cached, so a request for a
	// credential that is not stored does not hit the database every time.
	loaded map[string]bool
}

// NewService returns a Service. keyring may be nil, in which case stored
// credentials are unavailable and only the environment fallback is used; the
// app still starts, and the Settings page explains why storage is disabled.
func NewService(repo Repository, keyring *secrets.Keyring) *Service {
	return &Service{
		repo:    repo,
		keyring: keyring,
		cache:   map[string]string{},
		loaded:  map[string]bool{},
	}
}

// WithPreferences attaches the non-secret preference store and returns the
// service, so the two halves can be wired in one expression at startup.
func (s *Service) WithPreferences(prefs PreferenceRepository) *Service {
	s.prefs = prefs
	return s
}

// getenv is a variable so preference tests can stub the environment fallback
// without touching the process environment.
var getenv = os.Getenv

// StorageAvailable reports whether credentials can be written.
func (s *Service) StorageAvailable() bool { return s.keyring != nil }

// KeyringDescription renders the master key's provenance for the Settings page.
func (s *Service) KeyringDescription() string {
	if s.keyring == nil {
		return "unavailable — no master key"
	}
	return s.keyring.Describe()
}

// Resolve returns the credential to use for name, and where it came from.
// It never returns an error for a missing credential: callers treat an empty
// value as "this feature is not configured".
func (s *Service) Resolve(name string) (string, Origin) {
	def, ok := lookup(name)
	if !ok {
		return "", OriginNone
	}
	if value := s.stored(name); value != "" {
		return value, OriginSettings
	}
	if value := strings.TrimSpace(os.Getenv(def.envVar)); value != "" {
		return value, OriginEnv
	}
	return "", OriginNone
}

// Get returns just the credential value, for callers that do not care about
// provenance.
func (s *Service) Get(name string) string {
	value, _ := s.Resolve(name)
	return value
}

// stored returns the decrypted stored credential, or "" when there is none.
// A row that cannot be decrypted is treated as absent rather than fatal: a
// rotated master key should degrade to the environment fallback and a clear
// message in the UI, not take every AI feature down.
func (s *Service) stored(name string) string {
	if s.keyring == nil {
		return ""
	}

	s.mu.RLock()
	if s.loaded[name] {
		value := s.cache[name]
		s.mu.RUnlock()
		return value
	}
	s.mu.RUnlock()

	value := ""
	rec, err := s.repo.Get(name)
	if err == nil {
		if plaintext, decErr := s.keyring.Open(name, rec.Ciphertext); decErr == nil {
			value = plaintext
		}
	}

	s.mu.Lock()
	s.cache[name] = value
	s.loaded[name] = true
	s.mu.Unlock()
	return value
}

// Set stores a credential, replacing any existing one.
func (s *Service) Set(name, value, actor string) error {
	if _, ok := lookup(name); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCredential, name)
	}
	if s.keyring == nil {
		return ErrNoKeyring
	}
	value = strings.TrimSpace(value)
	if len(value) < minCredentialLength {
		return fmt.Errorf("credential looks truncated: it must be at least %d characters", minCredentialLength)
	}

	ciphertext, err := s.keyring.Seal(name, value)
	if err != nil {
		return fmt.Errorf("encrypt credential: %w", err)
	}
	if err := s.repo.Put(Record{
		Name:       name,
		Ciphertext: ciphertext,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		UpdatedBy:  strings.TrimSpace(actor),
	}); err != nil {
		return err
	}
	s.invalidate(name)
	return nil
}

// Clear removes a stored credential. The environment fallback, if any, takes
// over again.
func (s *Service) Clear(name string) error {
	if _, ok := lookup(name); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCredential, name)
	}
	if err := s.repo.Delete(name); err != nil {
		return err
	}
	s.invalidate(name)
	return nil
}

func (s *Service) invalidate(name string) {
	s.mu.Lock()
	delete(s.cache, name)
	delete(s.loaded, name)
	s.mu.Unlock()
}

// Status describes one credential without disclosing it.
func (s *Service) Status(name string) (Status, error) {
	def, ok := lookup(name)
	if !ok {
		return Status{}, fmt.Errorf("%w: %q", ErrUnknownCredential, name)
	}

	st := Status{
		Name:         def.name,
		EnvVar:       def.envVar,
		EnvAvailable: strings.TrimSpace(os.Getenv(def.envVar)) != "",
		Origin:       OriginNone,
	}

	if value := s.stored(def.name); value != "" {
		st.Configured = true
		st.Origin = OriginSettings
		st.Hint = hint(value)
		if rec, err := s.repo.Get(def.name); err == nil {
			st.UpdatedAt = rec.UpdatedAt
			st.UpdatedBy = rec.UpdatedBy
		}
		return st, nil
	}
	if st.EnvAvailable {
		st.Configured = true
		st.Origin = OriginEnv
		st.Hint = hint(strings.TrimSpace(os.Getenv(def.envVar)))
	}
	return st, nil
}

// Statuses describes every managed credential, in display order.
func (s *Service) Statuses() ([]Status, error) {
	out := make([]Status, 0, len(definitions))
	for _, def := range definitions {
		st, err := s.Status(def.name)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}
