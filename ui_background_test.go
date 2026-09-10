package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every theme the app ships is dark, and the palette layer only repaints the
// class names it knows. A page rule that hard-codes white or an off-white
// therefore shows through as a bright box on a dark page, so page CSS has to use
// the theme's surface tokens instead.
var whiteBackground = regexp.MustCompile(`(?i)background(-color)?\s*:\s*(white\b|#fff\b|#ffffff\b|#f[0-9a-f]{5}\b|rgba?\(\s*255\s*,\s*(255|252)\s*,\s*(255|246))`)

// Paper is the exception: these are printed or saved as a PDF, not looked at in
// the app's dark shell.
var paperFiles = map[string]bool{
	"internal/regcoverage/render.go": true,
}

var paperRules = map[string]string{
	"internal/doctemplate/pages.go": ".preview {",
}

func TestPageStylesHaveNoWhiteBackgrounds(t *testing.T) {
	err := filepath.WalkDir("internal", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		path = filepath.ToSlash(path)
		if paperFiles[path] {
			return nil
		}
		src, err := os.ReadFile(path) // #nosec G304 -- walks this repository's own source tree
		if err != nil {
			return err
		}
		for i, line := range strings.Split(withoutPrintBlocks(string(src)), "\n") {
			if !whiteBackground.MatchString(line) {
				continue
			}
			if rule, ok := paperRules[path]; ok && strings.Contains(line, rule) {
				continue
			}
			t.Errorf("%s:%d: white background; use var(--panel), var(--surface-strong) or var(--bg): %s",
				path, i+1, strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// withoutPrintBlocks blanks every @media print block, keeping its newlines so
// reported line numbers still match the file.
func withoutPrintBlocks(src string) string {
	var b strings.Builder
	for {
		at := strings.Index(src, "@media print")
		if at < 0 {
			break
		}
		open := strings.Index(src[at:], "{")
		if open < 0 {
			break
		}
		end, depth := len(src), 0
		for j := at + open; j < len(src); j++ {
			switch src[j] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				end = j + 1
				break
			}
		}
		b.WriteString(src[:at])
		b.WriteString(strings.Repeat("\n", strings.Count(src[at:end], "\n")))
		src = src[end:]
	}
	b.WriteString(src)
	return b.String()
}
