//go:build !fts5

package app

// FTS5Enabled reports whether SQLite full-text search was compiled in. See
// fts5_enabled.go for why this is a build-tag constant rather than a probe.
const FTS5Enabled = false
