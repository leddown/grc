package app

import (
	"os"
	"path/filepath"
	"sync"
)

// The change log and the /knowledge/* pages serve markdown files that live at
// the repository root. They cannot be embedded: go:embed only reaches files
// inside the importing package's own directory, and moving the documentation
// under internal/ would take it away from the readers (and the tooling) that
// expect it at the root.
//
// So they are read from disk, and the path they were read from used to be the
// bare file name — resolved against the process working directory. That is
// only the checkout when the binary is launched from one. Under systemd the
// working directory is "/", so every deployed server answered /changelog with
// "Failed to read change log: open CHANGELOG.md: no such file or directory"
// and every /knowledge/* page with a 404. docsDir resolves the directory
// holding those files explicitly instead.

// docsDirOverride is set once in Run from Options.DocsDir. When set it is used
// as-is with no fallback search: an operator who names a directory wants to be
// told it is wrong, not to be silently served some other copy of the docs.
var docsDirOverride string

var (
	resolveDocsDirOnce sync.Once
	resolvedDocsDir    string
)

// docsDirMarker is the file whose presence identifies a candidate directory as
// the documentation root. CHANGELOG.md is the one file both consumers need.
const docsDirMarker = "CHANGELOG.md"

// docsDirCandidates lists the directories searched, in order, when no override
// is set: the working directory (a checkout, or `go run` during development),
// the directory holding the binary, and the share directories the deploy
// scripts install into.
func docsDirCandidates() []string {
	candidates := []string{"."}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		binDir := filepath.Dir(executable)
		candidates = append(candidates,
			binDir,
			filepath.Join(binDir, "..", "share", "grc"),
		)
	}
	return append(candidates,
		"/usr/local/share/grc",
		"/usr/share/grc",
	)
}

// docsDir returns the directory the markdown documentation is served from. The
// search runs once per process: the answer cannot change while the binary runs
// and doing it per request would stat up to five directories on every page
// load.
func docsDir() string {
	if docsDirOverride != "" {
		return docsDirOverride
	}
	resolveDocsDirOnce.Do(func() {
		resolvedDocsDir = findDocsDir(docsDirCandidates())
	})
	return resolvedDocsDir
}

// findDocsDir returns the first candidate holding the marker file, or "." when
// none does — so the failure is reported against the path a reader would
// expect rather than against the last directory searched.
func findDocsDir(candidates []string) string {
	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(candidate, docsDirMarker)); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "."
}

// docPath resolves a repository-root-relative documentation file name against
// the documentation directory. Callers pass a fixed name from a package-level
// map or constant, never user input.
func docPath(name string) string {
	dir := docsDir()
	if dir == "" || dir == "." {
		return name
	}
	return filepath.Join(dir, name)
}
