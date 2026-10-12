package secrets

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testKeyring(t *testing.T) *Keyring {
	t.Helper()
	k, err := NewKeyForTesting(make([]byte, KeyLength))
	if err != nil {
		t.Fatalf("NewKeyForTesting: %v", err)
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	k := testKeyring(t)
	tests := []struct {
		name  string
		value string
	}{
		{"anthropic_api_key", "sk-ant-api03-abcdef"},
		{"empty value", ""},
		{"unicode", "clé-secrète-🔑"},
		{"long", strings.Repeat("x", 8192)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sealed, err := k.Seal("anthropic_api_key", tc.value)
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			if strings.Contains(sealed, tc.value) && tc.value != "" {
				t.Fatal("sealed token contains the plaintext")
			}
			got, err := k.Open("anthropic_api_key", sealed)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if got != tc.value {
				t.Errorf("round trip = %q, want %q", got, tc.value)
			}
		})
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	k := testKeyring(t)
	first, err := k.Seal("n", "same-value")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, err := k.Seal("n", "same-value")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// A fresh nonce per seal: identical plaintexts must not produce identical
	// ciphertexts, or the store leaks which settings share a value.
	if first == second {
		t.Error("sealing the same value twice produced identical ciphertext")
	}
}

// TestOpenRejectsCrossNameReuse is the reason name is used as AEAD additional
// data: a ciphertext moved to another row must not decrypt there.
func TestOpenRejectsCrossNameReuse(t *testing.T) {
	k := testKeyring(t)
	sealed, err := k.Seal("anthropic_api_key", "sk-ant-secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := k.Open("wintermute_token", sealed); err == nil {
		t.Error("a value sealed for anthropic_api_key opened as wintermute_token")
	}
}

func TestOpenRejectsTamperingAndWrongKey(t *testing.T) {
	k := testKeyring(t)
	sealed, err := k.Seal("n", "value")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	other, err := NewKeyForTesting([]byte(strings.Repeat("k", KeyLength)))
	if err != nil {
		t.Fatalf("NewKeyForTesting: %v", err)
	}
	if _, err := other.Open("n", sealed); err == nil {
		t.Error("ciphertext opened under a different master key")
	}

	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xff
	if _, err := k.Open("n", base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Error("tampered ciphertext opened successfully")
	}

	for _, bad := range []string{"", "not-base64!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := k.Open("n", bad); err == nil {
			t.Errorf("Open(%q) succeeded, want an error", bad)
		}
	}
}

func TestLoadFromEnv(t *testing.T) {
	key := make([]byte, KeyLength)
	for i := range key {
		key[i] = byte(i)
	}
	tests := []struct {
		name    string
		encoded string
		wantErr string
	}{
		{"standard base64", base64.StdEncoding.EncodeToString(key), ""},
		{"url-safe base64 is tolerated", base64.URLEncoding.EncodeToString(key), ""},
		{"not base64", "%%%%", "not valid base64"},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("too-short")), "must decode to 32 bytes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvKey, tc.encoded)
			ring, err := Load(Options{StateDir: t.TempDir()})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Load error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if ring.Source() != SourceEnv {
				t.Errorf("source = %q, want %q", ring.Source(), SourceEnv)
			}
			if strings.Contains(ring.Describe(), tc.encoded) {
				t.Error("Describe leaked the key material")
			}
		})
	}
}

func TestLoadGeneratesThenReusesKeyFile(t *testing.T) {
	t.Setenv(EnvKey, "")
	dir := t.TempDir()

	first, err := Load(Options{StateDir: dir, AllowGenerate: true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if first.Source() != SourceGenerated {
		t.Fatalf("source = %q, want %q", first.Source(), SourceGenerated)
	}

	path := filepath.Join(dir, keyFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %#o, want 0600", info.Mode().Perm())
	}

	// A second load must reuse the same key, or every previously sealed value
	// becomes permanently unreadable.
	second, err := Load(Options{StateDir: dir, AllowGenerate: true})
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if second.Source() != SourceFile {
		t.Errorf("second source = %q, want %q", second.Source(), SourceFile)
	}
	sealed, err := first.Seal("n", "value")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := second.Open("n", sealed)
	if err != nil {
		t.Fatalf("reloaded keyring could not open the earlier ciphertext: %v", err)
	}
	if got != "value" {
		t.Errorf("round trip across loads = %q, want %q", got, "value")
	}
}

func TestLoadRefusesWorldReadableKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	t.Setenv(EnvKey, "")
	dir := t.TempDir()
	path := filepath.Join(dir, keyFileName)
	if err := os.WriteFile(path, make([]byte, KeyLength), 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}
	_, err := Load(Options{StateDir: dir, AllowGenerate: true})
	if err == nil || !strings.Contains(err.Error(), "readable by other accounts") {
		t.Fatalf("Load error = %v, want a permissions complaint", err)
	}
}

func TestLoadWithoutGeneratePermission(t *testing.T) {
	t.Setenv(EnvKey, "")
	_, err := Load(Options{StateDir: t.TempDir(), AllowGenerate: false})
	if err == nil {
		t.Fatal("Load created key material despite AllowGenerate=false")
	}
}

func TestKeyFilePath(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		opts     Options
		wantBase string
		wantDir  string
	}{
		{
			name:     "explicit override wins",
			env:      map[string]string{EnvKeyFile: "/custom/place.key"},
			opts:     Options{StateDir: "/var/lib/grc"},
			wantBase: "place.key",
			wantDir:  "/custom",
		},
		{
			name:     "state directory next",
			opts:     Options{StateDir: "/var/lib/grc", DBPath: "/somewhere/users.db"},
			wantBase: keyFileName,
			wantDir:  "/var/lib/grc",
		},
		{
			name:     "systemd STATE_DIRECTORY may list several paths",
			env:      map[string]string{"STATE_DIRECTORY": "/var/lib/grc:/var/lib/other"},
			wantBase: keyFileName,
			wantDir:  "/var/lib/grc",
		},
		{
			name:     "falls back beside the database",
			opts:     Options{DBPath: "/var/lib/grc/users.db"},
			wantBase: keyFileName,
			wantDir:  "/var/lib/grc",
		},
		{
			name:     "a postgres spec carries no filesystem location",
			opts:     Options{DBPath: "postgres://user@host/db"},
			wantBase: keyFileName,
			wantDir:  ".",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvKeyFile, "")
			t.Setenv("STATE_DIRECTORY", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got := keyFilePath(tc.opts)
			if filepath.Base(got) != tc.wantBase {
				t.Errorf("base = %q, want %q", filepath.Base(got), tc.wantBase)
			}
			if filepath.Dir(got) != tc.wantDir {
				t.Errorf("dir = %q, want %q", filepath.Dir(got), tc.wantDir)
			}
		})
	}
}
