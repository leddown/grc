//go:build fts5

package app

// FTS5Enabled reports whether SQLite full-text search was compiled in.
//
// mattn/go-sqlite3 omits FTS5 unless built with -tags fts5, and the failure is
// deferred to runtime: CREATE VIRTUAL TABLE ... USING fts5 returns
// "no such module: fts5" only when the corpus search first runs. Surfacing it
// on /version means scripts/verify-install.sh catches a binary built without
// the tag at deploy time instead.
const FTS5Enabled = true
