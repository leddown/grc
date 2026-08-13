// Package settings stores install-wide configuration that an admin sets in the
// UI rather than in the environment — currently the AI provider credentials
// every AI field in the app shares.
//
// Credentials are encrypted at rest by internal/secrets and are write-only:
// they can be set, replaced and cleared, but never read back out to a browser.
package settings

// Credential names. These identify a row in app_secrets and are the AAD values
// passed to the keyring, so renaming one strands every value already stored
// under the old name.
//
// gosec's G101 flags these because the Go identifiers contain "APIKey" and
// "Token". The values are row names, not credentials — the credentials
// themselves only ever exist encrypted in the database or in the environment.
const (
	// AnthropicAPIKey authenticates directly against api.anthropic.com.
	AnthropicAPIKey = "anthropic_api_key" // #nosec G101 -- a row name, not a credential
	// WintermuteToken authenticates against a Wintermute server, which routes
	// questions to self-hosted models or on to Claude.
	WintermuteToken = "wintermute_token" // #nosec G101 -- a row name, not a credential
)

// Origin says where a resolved credential came from. The Settings page shows
// it so an operator can tell a stored value from an inherited one.
type Origin string

const (
	// OriginSettings is a credential stored by an admin in the Settings page.
	OriginSettings Origin = "settings"
	// OriginEnv is a credential inherited from the process environment, which
	// remains supported so an existing deployment keeps working untouched.
	OriginEnv Origin = "environment"
	// OriginNone means no credential is available from either source.
	OriginNone Origin = "none"
)

// Status describes one credential without disclosing it.
type Status struct {
	Name string `json:"name"`
	// Configured reports whether a usable credential exists from any source.
	Configured bool `json:"configured"`
	// Origin names the source that would be used for the next request.
	Origin Origin `json:"origin"`
	// EnvVar names the environment variable consulted as a fallback.
	EnvVar string `json:"env_var,omitempty"`
	// EnvAvailable reports whether that variable is set, so the page can show
	// that clearing a stored value will fall back rather than disable the
	// feature.
	EnvAvailable bool `json:"env_available"`
	// Hint is a short, non-reversible fingerprint of a stored credential
	// (last four characters) so an operator can tell which key is loaded.
	Hint string `json:"hint,omitempty"`
	// UpdatedAt and UpdatedBy carry the audit trail for a stored credential.
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// Record is a stored credential as it exists in the database. It is exported
// so a Repository can be implemented outside this package.
type Record struct {
	Name       string
	Ciphertext string
	UpdatedAt  string
	UpdatedBy  string
}

// definition binds a credential name to the environment variable that acts as
// its fallback.
type definition struct {
	name   string
	envVar string
}

// definitions is the full set of credentials the Settings page manages, in
// display order.
var definitions = []definition{
	{name: AnthropicAPIKey, envVar: "ANTHROPIC_API_KEY"},
	{name: WintermuteToken, envVar: "WINTERMUTE_TOKEN"},
}

// lookup finds a credential definition by name.
func lookup(name string) (definition, bool) {
	for _, d := range definitions {
		if d.name == name {
			return d, true
		}
	}
	return definition{}, false
}

// hint renders the trailing characters of a credential so an operator can tell
// two keys apart without the value being recoverable from the page.
func hint(value string) string {
	const shown = 4
	if len(value) <= shown {
		return "****"
	}
	return "****" + value[len(value)-shown:]
}
