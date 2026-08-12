package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// withDocsDirSearch resets the memoised resolution so a test can exercise the
// search, and restores it afterwards. docsDir caches for the life of the
// process, which is right in production and useless in a test.
func withDocsDirSearch(t *testing.T) {
	t.Helper()
	prevOverride := docsDirOverride
	resolveDocsDirOnce = sync.Once{}
	resolvedDocsDir = ""
	docsDirOverride = ""
	t.Cleanup(func() {
		// Leave a clean slate rather than restoring the memoised value: the
		// next test's working directory is not this one's.
		resolveDocsDirOnce = sync.Once{}
		resolvedDocsDir = ""
		docsDirOverride = prevOverride
	})
}

func TestDocPathUsesOverrideVerbatim(t *testing.T) {
	withDocsDirSearch(t)
	docsDirOverride = "/opt/docs"

	if got := docPath(changeLogPath); got != filepath.Join("/opt/docs", "CHANGELOG.md") {
		t.Fatalf("docPath = %q, want the override joined with the file name", got)
	}
}

// An operator who names a directory wants to be told it is wrong rather than
// silently served a different copy, so the override must not fall back to the
// search even when the file is missing there.
func TestDocPathOverrideDoesNotFallBackToSearch(t *testing.T) {
	withDocsDirSearch(t)
	docsDirOverride = filepath.Join(t.TempDir(), "nonexistent")

	if got := docPath(changeLogPath); !strings.HasPrefix(got, docsDirOverride) {
		t.Fatalf("docPath = %q, want it under the override %q", got, docsDirOverride)
	}
}

func TestDocsDirFindsMarkerInWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, docsDirMarker), []byte("# Change Log\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	t.Chdir(dir)
	withDocsDirSearch(t)

	if got := docsDir(); got != "." {
		t.Fatalf("docsDir = %q, want %q", got, ".")
	}
	if got := docPath(changeLogPath); got != changeLogPath {
		t.Fatalf("docPath = %q, want the bare name when the docs are in the working directory", got)
	}
}

// The bug this whole path exists for: under systemd the working directory is
// "/" and the docs are installed beside the binary. A later candidate must win
// when the working directory has nothing, and an earlier one must win when it
// does — a checkout is the more specific answer than a system-wide install.
func TestFindDocsDirPrefersTheEarliestCandidateHoldingTheMarker(t *testing.T) {
	empty := t.TempDir()
	installed := t.TempDir()
	checkout := t.TempDir()
	for _, dir := range []string{installed, checkout} {
		if err := os.WriteFile(filepath.Join(dir, docsDirMarker), nil, 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
	}

	if got := findDocsDir([]string{empty, installed}); got != installed {
		t.Fatalf("findDocsDir = %q, want the installed copy %q", got, installed)
	}
	if got := findDocsDir([]string{checkout, installed}); got != checkout {
		t.Fatalf("findDocsDir = %q, want the checkout %q", got, checkout)
	}
	if got := findDocsDir([]string{empty}); got != "." {
		t.Fatalf("findDocsDir = %q, want %q when no candidate holds the marker", got, ".")
	}
}

// A directory named CHANGELOG.md would satisfy a bare existence check and then
// fail every read.
func TestFindDocsDirIgnoresADirectoryNamedLikeTheMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, docsDirMarker), 0o755); err != nil {
		t.Fatalf("mkdir marker: %v", err)
	}

	if got := findDocsDir([]string{dir}); got != "." {
		t.Fatalf("findDocsDir = %q, want %q", got, ".")
	}
}

func TestDocsDirFallsBackToWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	withDocsDirSearch(t)

	if got := docsDir(); got != "." {
		t.Fatalf("docsDir = %q, want %q when nothing is found", got, ".")
	}
}

// The share directories are the contract between internal/app and the deploy
// scripts: setup.sh and update.sh install into <bindir>/../share/grc.
// Dropping one here would break deployed servers without breaking any other test.
func TestDocsDirCandidatesIncludeInstallLocations(t *testing.T) {
	candidates := docsDirCandidates()

	if len(candidates) == 0 || candidates[0] != "." {
		t.Fatalf("candidates = %v, want the working directory searched first", candidates)
	}

	want := []string{"/usr/local/share/grc", "/usr/share/grc"}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		binDir := filepath.Dir(executable)
		want = append(want, binDir, filepath.Join(binDir, "..", "share", "grc"))
	}

	for _, dir := range want {
		found := false
		for _, candidate := range candidates {
			if candidate == dir {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("candidates %v missing %q", candidates, dir)
		}
	}
}

func TestChangeLogPageServesContentFromDocsDir(t *testing.T) {
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, changeLogPath), []byte("## 2026-08-02 entry body"), 0o600); err != nil {
		t.Fatalf("write change log: %v", err)
	}
	t.Chdir(t.TempDir())
	withDocsDirSearch(t)
	docsDirOverride = docs

	rec := getPath(t, "/changelog", changeLogPage)

	body := rec.Body.String()
	if !strings.Contains(body, "2026-08-02 entry body") {
		t.Fatalf("expected change log content in the page, got %q", body)
	}
	if strings.Contains(body, "No change log entries found.") {
		t.Fatalf("page rendered the empty state despite readable content")
	}
}

// The empty page gave no clue why it was empty, which is what made this take a
// deploy to notice. The failure must name the path it looked at and the knob.
func TestChangeLogPageReportsResolvedPathAndRemedy(t *testing.T) {
	docs := filepath.Join(t.TempDir(), "missing")
	withDocsDirSearch(t)
	docsDirOverride = docs

	rec := getPath(t, "/changelog", changeLogPage)

	body := rec.Body.String()
	if !strings.Contains(body, filepath.Join(docs, "CHANGELOG.md")) {
		t.Fatalf("expected the resolved path in the error, got %q", body)
	}
	if !strings.Contains(body, "DOCS_DIR") {
		t.Fatalf("expected the DOCS_DIR remedy in the error, got %q", body)
	}
}

func TestKnowledgeDocPageServesFileFromDocsDir(t *testing.T) {
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "FAQ.md"), []byte("faq body"), 0o600); err != nil {
		t.Fatalf("write faq: %v", err)
	}
	t.Chdir(t.TempDir())
	withDocsDirSearch(t)
	docsDirOverride = docs

	rec := getKnowledgeDoc(t, "faq")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "faq body") {
		t.Fatalf("unexpected body %q", rec.Body.String())
	}
}

// A missing file used to be indistinguishable from an unknown doc name — both
// a bare 404 — which is why the working-directory bug read as a routing fault.
func TestKnowledgeDocPageDistinguishesMissingFileFromUnknownName(t *testing.T) {
	withDocsDirSearch(t)
	docsDirOverride = filepath.Join(t.TempDir(), "missing")

	missing := getKnowledgeDoc(t, "faq")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", missing.Code)
	}
	if !strings.Contains(missing.Body.String(), "FAQ.md") || !strings.Contains(missing.Body.String(), "DOCS_DIR") {
		t.Fatalf("expected the file name and the DOCS_DIR remedy, got %q", missing.Body.String())
	}

	unknown := getKnowledgeDoc(t, "not-a-doc")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", unknown.Code)
	}
	if strings.Contains(unknown.Body.String(), "DOCS_DIR") {
		t.Fatalf("an unknown doc name is not a deployment problem, got %q", unknown.Body.String())
	}
}

// Every entry in the map must name a file that exists at the repository root,
// or the deploy scripts install a set that cannot satisfy it and the page 404s.
func TestKnowledgeDocsAllExistInRepository(t *testing.T) {
	for name, file := range markdownKnowledgeDocs {
		if _, err := os.Stat(filepath.Join("..", "..", file)); err != nil {
			t.Errorf("/knowledge/%s points at %s, which is not at the repository root: %v", name, file, err)
		}
	}
}

func getPath(t *testing.T, path string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET(path, handler)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func getKnowledgeDoc(t *testing.T, name string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/knowledge/:name", markdownKnowledgeDocPage)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/knowledge/"+name, nil))
	return rec
}
