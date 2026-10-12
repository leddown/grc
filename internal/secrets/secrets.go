// Package secrets holds the master key that encrypts stored credentials, and
// seals and opens values with it.
//
// The key never lives in the repository. It comes from the environment where
// one is configured, and otherwise from a file in the service's state
// directory — a previous incident put an encryption key beside the checkout,
// where a blanket `git add` committed it and made the encryption it protected
// worthless. Load reports which source won so that is visible at boot.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// KeyLength is the master key size: AES-256.
const KeyLength = 32

// EnvKey holds a base64-encoded master key. EnvKeyFile names the key file
// explicitly, overriding the search below.
const (
	EnvKey     = "GRC_SECRET_KEY"
	EnvKeyFile = "GRC_SECRET_KEY_FILE"
	// keyFileName is the basename used wherever the key file is placed.
	keyFileName = "secret.key"
)

// Source describes where the master key came from.
type Source string

const (
	SourceEnv       Source = "environment"
	SourceFile      Source = "key file"
	SourceGenerated Source = "generated key file"
)

// ErrNoKey reports that no master key is available and none could be created.
var ErrNoKey = errors.New("no master key available")

// Keyring seals and opens values with the master key.
type Keyring struct {
	aead   cipher.AEAD
	source Source
	path   string
}

// Options configures where Load looks for a key file.
type Options struct {
	// StateDir is the service's writable state directory. Defaults to
	// $STATE_DIRECTORY, which systemd sets from StateDirectory= in the unit.
	StateDir string
	// DBPath is the SQLite path, used to place the key beside the database
	// when there is no state directory. Ignored for postgres:// specs.
	DBPath string
	// AllowGenerate permits creating a key when none exists. Callers that must
	// never invent key material (a read-only diagnostic, say) set this false.
	AllowGenerate bool
}

// Load resolves the master key and returns a Keyring.
//
// Order: $GRC_SECRET_KEY, then an existing key file, then a freshly generated
// key file when opts.AllowGenerate is set.
func Load(opts Options) (*Keyring, error) {
	if encoded := strings.TrimSpace(os.Getenv(EnvKey)); encoded != "" {
		key, err := decodeKey(encoded)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", EnvKey, err)
		}
		return newKeyring(key, SourceEnv, "")
	}

	path := keyFilePath(opts)
	if path == "" {
		return nil, fmt.Errorf("%w: set %s or %s", ErrNoKey, EnvKey, EnvKeyFile)
	}

	switch key, err := readKeyFile(path); {
	case err == nil:
		return newKeyring(key, SourceFile, path)
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}

	if !opts.AllowGenerate {
		return nil, fmt.Errorf("%w: no key at %s and generating one is not permitted here", ErrNoKey, path)
	}
	key, err := generateKeyFile(path)
	if err != nil {
		return nil, err
	}
	return newKeyring(key, SourceGenerated, path)
}

// keyFilePath picks where the key file lives, preferring an explicit override,
// then the service state directory, then the database's own directory.
func keyFilePath(opts Options) string {
	if explicit := strings.TrimSpace(os.Getenv(EnvKeyFile)); explicit != "" {
		return explicit
	}
	stateDir := opts.StateDir
	if stateDir == "" {
		// systemd sets STATE_DIRECTORY from StateDirectory= in the unit. It may
		// carry several colon-separated paths; the first is ours.
		stateDir = strings.SplitN(os.Getenv("STATE_DIRECTORY"), ":", 2)[0]
	}
	if stateDir != "" {
		return filepath.Join(stateDir, keyFileName)
	}
	// Beside the database, which on a deployed host is also under the state
	// directory. Postgres specs carry no filesystem location.
	if db := strings.TrimSpace(opts.DBPath); db != "" && !strings.HasPrefix(db, "postgres") {
		if dir := filepath.Dir(db); dir != "" && dir != "." {
			return filepath.Join(dir, keyFileName)
		}
	}
	return keyFileName
}

func decodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Tolerate the URL-safe alphabet, which is easy to paste by mistake.
		key, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("not valid base64: %w", err)
		}
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("must decode to %d bytes, got %d", KeyLength, len(key))
	}
	return key, nil
}

// readKeyFile loads a raw 32-byte key, refusing one that other accounts on the
// host can read.
func readKeyFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	// Windows does not carry POSIX mode bits worth checking.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master key %s is readable by other accounts (mode %#o); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	// #nosec G304 -- the key file location is operator configuration, resolved
	// by keyFilePath from the environment and the configured database path.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read master key %s: %w", path, err)
	}
	key := raw
	// Tolerate a trailing newline from an operator writing the file by hand.
	if trimmed := strings.TrimRight(string(raw), "\r\n"); len(trimmed) == KeyLength {
		key = []byte(trimmed)
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("master key %s must be %d bytes, got %d", path, KeyLength, len(key))
	}
	return key, nil
}

func generateKeyFile(path string) ([]byte, error) {
	key := make([]byte, KeyLength)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create directory for master key: %w", err)
		}
	}
	// O_EXCL: never clobber a key that appeared between the read above and
	// here, which would silently strand every value encrypted under it.
	// #nosec G304 -- see readKeyFile.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create master key %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(key); err != nil {
		return nil, fmt.Errorf("write master key %s: %w", path, err)
	}
	return key, nil
}

func newKeyring(key []byte, source Source, path string) (*Keyring, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialise cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialise GCM: %w", err)
	}
	return &Keyring{aead: aead, source: source, path: path}, nil
}

// Seal encrypts value, binding the ciphertext to name so a stored value cannot
// be moved to a different setting and silently decrypt there.
func (k *Keyring) Seal(name, value string) (string, error) {
	if name == "" {
		return "", errors.New("seal: name is required")
	}
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := k.aead.Seal(nonce, nonce, []byte(value), []byte(name))
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value sealed under the same name.
func (k *Keyring) Open(name, token string) (string, error) {
	if name == "" {
		return "", errors.New("open: name is required")
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("stored value is not valid base64: %w", err)
	}
	nonceSize := k.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("stored value is too short to be valid ciphertext")
	}
	plaintext, err := k.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], []byte(name))
	if err != nil {
		// Wrong master key, a tampered row, or a value sealed under a
		// different name. All three are the same recovery: re-enter it.
		return "", fmt.Errorf("cannot decrypt %q with the current master key: %w", name, err)
	}
	return string(plaintext), nil
}

// Source reports where the master key came from.
func (k *Keyring) Source() Source { return k.source }

// Describe renders the key's provenance for logs and the settings page. It
// never includes key material.
func (k *Keyring) Describe() string {
	switch k.source {
	case SourceEnv:
		return "master key from $" + EnvKey
	case SourceGenerated:
		return "master key generated at " + k.path
	default:
		return "master key from " + k.path
	}
}

// NewKeyForTesting builds a Keyring from a caller-supplied key.
func NewKeyForTesting(key []byte) (*Keyring, error) {
	return newKeyring(key, SourceEnv, "")
}
