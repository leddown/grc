package app

import (
	"flag"
	"os"
	"strconv"
)

const (
	sqliteDBPath        = "users.db"
	controlDataPath     = "internal/data/merged_nist_controls_master_replaced_from_controls_all.json"
	securityNFRDataPath = "internal/data/NFR_incremental_keys_with_domain.json"
	changeLogPath       = "CHANGELOG.md"
	listenAddr          = ":8080"
)

// localModeEnabled is set once in Run from Options.LocalMode and read by
// page handlers (e.g. homePage) that need to hide login/user-management
// links when the app is running with no auth at all.
var localModeEnabled bool

// trustProxyHeaders is set once in Run from Options.TrustProxyHeaders. When
// false (the default), X-Forwarded-Proto is ignored when deciding whether to
// mark auth cookies Secure, since that header is otherwise client-spoofable
// on any deployment that isn't sitting behind a proxy which overwrites it.
var trustProxyHeaders bool

// BindFlags registers the runtime flags (with environment variable fallbacks)
// shared by every entrypoint binary. Callers must still invoke flag.Parse().
func BindFlags() *Options {
	options := DefaultOptions()
	flag.StringVar(&options.ListenAddr, "listen-addr", envOrDefault("LISTEN_ADDR", options.ListenAddr), "HTTP listen address")
	flag.StringVar(&options.SQLitePath, "sqlite-path", envOrDefault("SQLITE_PATH", options.SQLitePath), "path to SQLite database file (ignored when -database-url is set)")
	flag.StringVar(&options.DatabaseURL, "database-url", envOrDefault("DATABASE_URL", options.DatabaseURL), "PostgreSQL connection string (e.g. postgres://user:pass@host:5432/db?sslmode=require); when set, uses Postgres instead of SQLite")
	flag.BoolVar(&options.AllowJSONSave, "allow-json-save", envBoolOrDefault("ALLOW_JSON_SAVE", false), "enable JSON save endpoints (/controls/save, /security-nfrs/save, and /jira/json/export)")
	flag.StringVar(&options.AdminToken, "admin-token", envOrDefault("ADMIN_TOKEN", options.AdminToken), "admin bearer token for protected mutation endpoints")
	flag.BoolVar(&options.LocalMode, "local-mode", envBoolOrDefault("LOCAL_MODE", false), "run with no login/user management; every page and admin action is open (intended for single-user laptop use)")
	flag.StringVar(&options.SyncTo, "sync-to", envOrDefault("SYNC_TO", ""), "one-shot: copy the active database up to this target (postgres:// URL or SQLite path) with upsert/merge, then exit")
	flag.StringVar(&options.SyncFrom, "sync-from", envOrDefault("SYNC_FROM", ""), "one-shot: copy from this source (postgres:// URL or SQLite path) into the active database with upsert/merge, then exit")
	flag.BoolVar(&options.TrustProxyHeaders, "trust-proxy", envBoolOrDefault("TRUST_PROXY", false), "trust X-Forwarded-Proto from the immediate connection when deciding whether auth cookies are Secure; only enable behind a reverse proxy that sets/overwrites this header")
	flag.StringVar(&options.DocsDir, "docs-dir", envOrDefault("DOCS_DIR", ""), "directory holding CHANGELOG.md and the other markdown docs served by /changelog and /knowledge/*; when unset, the working directory, the binary's directory and the share directories are searched")
	flag.StringVar(&options.TemplatesDir, "templates-dir", envOrDefault("TEMPLATES_DIR", ""), "directory holding the Typst/LaTeX document templates rendered by /templates; when unset, ./templates, the binary's directory and the share directories are searched")
	return &options
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBoolOrDefault(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
