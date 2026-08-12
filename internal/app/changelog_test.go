package app

import (
	"strings"
	"testing"
)

func TestRenderChangeLogHTML_IncludesContentAndPath(t *testing.T) {
	html := string(renderChangeLogHTML("CHANGELOG.md", []byte("# heading\nentry"), nil))

	if !strings.Contains(html, "File: CHANGELOG.md") {
		t.Fatalf("expected changelog path in html, got %q", html)
	}
	if !strings.Contains(html, "# heading") || !strings.Contains(html, "entry") {
		t.Fatalf("expected changelog content in html, got %q", html)
	}
	if !strings.Contains(html, "max-width: 1320px;") {
		t.Fatalf("expected changelog page width to match reports page, got %q", html)
	}
}

func TestRenderChangeLogHTML_ShowsReadError(t *testing.T) {
	html := string(renderChangeLogHTML("/usr/local/share/grc/CHANGELOG.md", nil, assertErr{}))

	if !strings.Contains(html, "Failed to read change log: render failed") {
		t.Fatalf("expected read error in html, got %q", html)
	}
	if !strings.Contains(html, "No change log entries found.") {
		t.Fatalf("expected empty state in html, got %q", html)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "render failed" }
