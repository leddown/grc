package app

// FTS5Enabled reports whether SQLite full-text search is available.
//
// It used to be a build-tag constant: the cgo driver omitted FTS5 unless built
// with -tags fts5, and the failure was deferred to runtime — CREATE VIRTUAL
// TABLE ... USING fts5 returned "no such module: fts5" only when the corpus
// search first ran. modernc.org/sqlite compiles FTS5 in unconditionally, so
// there is no longer a build that can get this wrong.
//
// The constant stays because /version reports it and scripts/verify-install.sh
// checks it: a deployed binary predating this change can still answer false,
// and that is worth seeing. TestFTS5IsCompiledIn in internal/db proves the
// claim against a real database rather than trusting a flag.
const FTS5Enabled = true
