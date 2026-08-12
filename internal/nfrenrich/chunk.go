package nfrenrich

import (
	"regexp"
	"strings"
	"unicode"
)

// Chunking is section-aware rather than fixed-size, per POLICY_MODULE_FRAMEWORK
// §2.4: a security document's headings are already the author's own statement
// of where one topic ends and the next begins, and a fixed 512-token window
// splits "TLS 1.2 is the minimum" from the heading that says what it applies
// to. A chunk that arrives at the model without that context invites exactly
// the confident, wrong attribution this module exists to avoid.
//
// A section longer than maxChunkRunes is split further, with overlap, because
// some documents put a whole standard under one heading.

const (
	// maxChunkRunes bounds one chunk. Roughly 2,000 tokens of English — large
	// enough to hold a complete subsection, small enough that a dozen chunks
	// still fit in a prompt alongside the NFR.
	maxChunkRunes = 8000
	// overlapRunes is carried from the end of an oversized section into the
	// next piece, so a requirement split across the boundary survives in one
	// of them intact.
	overlapRunes = 800
	// minChunkRunes drops fragments too small to be evidence. A heading with
	// no body under it produces one of these.
	minChunkRunes = 60
)

// headingPattern matches a Markdown ATX heading and captures its level and
// text. Setext headings (underlined with === or ---) are handled separately.
var headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)

// numberedHeadingPattern matches the numbered headings security documents use
// constantly ("4.2.1 Encryption in Transit") when they are not marked up as
// Markdown at all — the common shape of a .txt export from Word. It requires
// the line to be short and to not end in a sentence terminator, so an ordinary
// numbered list item is not mistaken for a section break.
var numberedHeadingPattern = regexp.MustCompile(`^\s*(\d+(?:\.\d+)*)\.?\s+(\S.{0,80})$`)

// ChunkText splits text into section-aware chunks, tracking the heading path
// above each one.
func ChunkText(text string) []Chunk {
	text = normalizeNewlines(text)
	lines := strings.Split(text, "\n")

	var (
		out     []Chunk
		path    []string
		body    []string
		ordinal int
	)

	flush := func() {
		joined := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]
		if len([]rune(joined)) < minChunkRunes {
			return
		}
		heading := strings.Join(path, " > ")
		for _, piece := range splitOversized(joined) {
			out = append(out, Chunk{
				Ordinal: ordinal,
				Heading: heading,
				Text:    piece,
			})
			ordinal++
		}
	}

	for i := 0; i < len(lines); i++ {
		level, title, ok := headingAt(lines, i)
		if !ok {
			body = append(body, lines[i])
			continue
		}
		// A heading ends the previous section. Setext headings consume the
		// underline line as well.
		flush()
		path = pushHeading(path, level, title)
		if isSetext(lines, i) {
			i++
		}
	}
	flush()

	return out
}

// headingAt reports whether line i starts a section, and at what level.
func headingAt(lines []string, i int) (int, string, bool) {
	line := lines[i]
	if m := headingPattern.FindStringSubmatch(line); m != nil {
		return len(m[1]), strings.TrimSpace(m[2]), true
	}
	if isSetext(lines, i) {
		level := 1
		if strings.HasPrefix(strings.TrimSpace(lines[i+1]), "-") {
			level = 2
		}
		return level, strings.TrimSpace(line), true
	}
	if m := numberedHeadingPattern.FindStringSubmatch(line); m != nil {
		title := strings.TrimSpace(m[2])
		if looksLikeProse(title) {
			return 0, "", false
		}
		// Depth comes from the number itself: "4.2.1" is a level-3 heading.
		return strings.Count(m[1], ".") + 1, strings.TrimSpace(m[1]) + " " + title, true
	}
	return 0, "", false
}

// isSetext reports whether line i is a heading underlined by line i+1.
func isSetext(lines []string, i int) bool {
	if i+1 >= len(lines) {
		return false
	}
	title := strings.TrimSpace(lines[i])
	under := strings.TrimSpace(lines[i+1])
	if title == "" || len(under) < 3 {
		return false
	}
	// An underline is one repeated character, and the title must not itself
	// look like one (a --- rule followed by another --- rule is not a heading).
	if strings.Trim(title, "=-") == "" {
		return false
	}
	return strings.Trim(under, "=") == "" || strings.Trim(under, "-") == ""
}

// looksLikeProse rejects numbered list items that are really sentences, so
// "1. Rotate the key every 90 days." does not become a section heading.
func looksLikeProse(title string) bool {
	if strings.HasSuffix(title, ".") || strings.HasSuffix(title, ";") {
		return true
	}
	// Headings are short. Counting words rather than characters keeps a long
	// but genuine heading ("Encryption of Cardholder Data in Transit Over Open
	// Public Networks") from being rejected on length alone.
	return len(strings.Fields(title)) > 12
}

// pushHeading maintains the heading path: a level-2 heading replaces the
// previous level-2 and everything below it.
func pushHeading(path []string, level int, title string) []string {
	if level < 1 {
		level = 1
	}
	if level > len(path) {
		// Skipping a level (h1 straight to h3) pads rather than nesting
		// wrongly, so the path length keeps matching the level.
		for len(path) < level-1 {
			path = append(path, "")
		}
		return append(path, title)
	}
	path = path[:level-1]
	return append(path, title)
}

// splitOversized breaks a section that is too long for one chunk, preferring
// paragraph boundaries and carrying overlap across the cut.
func splitOversized(text string) []string {
	runes := []rune(text)
	if len(runes) <= maxChunkRunes {
		return []string{text}
	}

	var out []string
	for start := 0; start < len(runes); {
		end := start + maxChunkRunes
		if end >= len(runes) {
			out = append(out, strings.TrimSpace(string(runes[start:])))
			break
		}
		// Back off to the last paragraph break in the final quarter of the
		// window; a mid-sentence cut is the one that loses a requirement.
		cut := end
		if idx := strings.LastIndex(string(runes[start+maxChunkRunes*3/4:end]), "\n\n"); idx >= 0 {
			cut = start + maxChunkRunes*3/4 + len([]rune(string(runes[start+maxChunkRunes*3/4 : end])[:idx]))
		}
		out = append(out, strings.TrimSpace(string(runes[start:cut])))
		next := cut - overlapRunes
		if next <= start {
			next = cut
		}
		start = next
	}
	return out
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// ---- plain-text extraction ----

var (
	scriptStylePattern = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)>`)
	blockTagPattern    = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6]|section|article|header|footer|table)\b[^>]*>`)
	anyTagPattern      = regexp.MustCompile(`(?s)<[^>]*>`)
	titleTagPattern    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	headingTagPattern  = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*>(.*?)</h[1-6]>`)
	entityPattern      = regexp.MustCompile(`&(#?\w+);`)
	blankRunPattern    = regexp.MustCompile(`\n{3,}`)
)

// ExtractText turns a fetched or uploaded body into plain text.
//
// HTML gets its headings converted to Markdown ATX form before the tags are
// stripped, so a web page keeps the section structure the chunker depends on —
// without that step every web document collapses into one unheaded blob and
// citations lose the context that makes them checkable.
//
// Binary formats (PDF, DOCX) are deliberately not handled: extracting them
// correctly needs a parsing dependency, and a silent partial extraction is
// worse than a clear refusal. Callers get ErrUnsupportedMedia.
func ExtractText(mediaType string, body []byte) (string, error) {
	if isBinary(body) {
		return "", ErrUnsupportedMedia
	}

	text := string(body)
	if isHTML(mediaType, text) {
		text = htmlToText(text)
	}

	text = normalizeNewlines(text)
	text = blankRunPattern.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrEmptyDocument
	}
	return text, nil
}

func htmlToText(s string) string {
	s = scriptStylePattern.ReplaceAllString(s, "\n")
	s = headingTagPattern.ReplaceAllStringFunc(s, func(m string) string {
		parts := headingTagPattern.FindStringSubmatch(m)
		if len(parts) < 3 {
			return m
		}
		level := strings.Repeat("#", int(parts[1][0]-'0'))
		inner := strings.TrimSpace(decodeEntities(anyTagPattern.ReplaceAllString(parts[2], "")))
		if inner == "" {
			return "\n"
		}
		return "\n\n" + level + " " + inner + "\n\n"
	})
	s = blockTagPattern.ReplaceAllString(s, "\n")
	s = anyTagPattern.ReplaceAllString(s, " ")
	return decodeEntities(s)
}

// HTMLTitle pulls a <title> for use as the document title when the caller did
// not supply one.
func HTMLTitle(body []byte) string {
	m := titleTagPattern.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(decodeEntities(anyTagPattern.ReplaceAllString(m[1], "")))
}

var namedEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": `"`, "apos": "'",
	"nbsp": " ", "mdash": "—", "ndash": "–", "hellip": "…", "rsquo": "'", "lsquo": "'",
	"ldquo": `"`, "rdquo": `"`,
}

func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return entityPattern.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.Trim(m, "&;")
		if v, ok := namedEntities[strings.ToLower(inner)]; ok {
			return v
		}
		if strings.HasPrefix(inner, "#") {
			if r := parseNumericEntity(inner[1:]); r != 0 {
				return string(r)
			}
		}
		return m
	})
}

func parseNumericEntity(s string) rune {
	base, digits := 10, s
	if len(s) > 1 && (s[0] == 'x' || s[0] == 'X') {
		base, digits = 16, s[1:]
	}
	var n int64
	for _, r := range digits {
		var d int64
		switch {
		case r >= '0' && r <= '9':
			d = int64(r - '0')
		case base == 16 && r >= 'a' && r <= 'f':
			d = int64(r-'a') + 10
		case base == 16 && r >= 'A' && r <= 'F':
			d = int64(r-'A') + 10
		default:
			return 0
		}
		n = n*int64(base) + d
		if n > 0x10FFFF {
			return 0
		}
	}
	return rune(n)
}

func isHTML(mediaType, body string) bool {
	if strings.Contains(strings.ToLower(mediaType), "html") {
		return true
	}
	head := strings.ToLower(body)
	if len(head) > 2048 {
		head = head[:2048]
	}
	return strings.Contains(head, "<!doctype html") || strings.Contains(head, "<html")
}

// isBinary looks for a NUL byte or a high proportion of non-printable bytes in
// the first block. A PDF is caught by its NUL bytes; so is a DOCX, which is a
// zip. This is a guard against garbage reaching the model as if it were prose,
// not a format detector.
func isBinary(body []byte) bool {
	head := body
	if len(head) > 4096 {
		head = head[:4096]
	}
	if len(head) == 0 {
		return false
	}
	nonPrintable := 0
	for _, b := range head {
		if b == 0 {
			return true
		}
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			nonPrintable++
		}
	}
	return nonPrintable*100/len(head) > 5
}

// tokenize lowercases and splits on non-alphanumerics, keeping the dots and
// hyphens inside control identifiers: "AC-2" and "1.2.3" must survive as single
// terms or the retrieval loses precisely the keywords compliance search runs on.
func tokenize(s string) []string {
	var (
		out  []string
		curr strings.Builder
	)
	flush := func() {
		if curr.Len() == 0 {
			return
		}
		term := strings.Trim(curr.String(), ".-")
		curr.Reset()
		if term != "" {
			out = append(out, term)
		}
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			curr.WriteRune(r)
		case r == '-' || r == '.' || r == '_':
			// Kept only between alphanumerics, which flush handles by trimming
			// the edges.
			if curr.Len() > 0 {
				curr.WriteRune(r)
			}
		default:
			flush()
		}
	}
	flush()
	return out
}
