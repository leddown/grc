package ingest

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"grc/internal/regmap/profile"
	"grc/internal/regmap/requirement"
)

// defaultMinBodyChars is the body length below which a segment is treated as
// low-confidence and surfaced first at GATE 1.
const defaultMinBodyChars = 80

// Segment is one chunk of the source document identified by a boundary rule.
type Segment struct {
	Key        string
	Section    string
	Title      string
	Body       string
	Strategy   string
	IDPrefix   string
	Offset     int
	Confidence requirement.Confidence
	Flags      []string
}

// boundary is a candidate split point found by one strategy.
type boundary struct {
	start    int
	end      int
	key      string
	strategy *profile.Strategy
	order    int // strategy declaration order, for deterministic tie-breaks
}

// Result reports what a segmentation run produced.
type Result struct {
	Requirements requirement.Set
	Log          []string
}

// SegmentText splits text using every strategy declared by the profile and
// merges the boundaries in document order. Text preceding the first boundary is
// kept as a flagged preamble segment — content is never dropped silently.
func SegmentText(text string, prof *profile.Profile) ([]Segment, error) {
	var bounds []boundary
	for i := range prof.Segmentation {
		strat := &prof.Segmentation[i]
		re, err := strat.Regexp()
		if err != nil {
			return nil, err
		}
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			if len(m) < 4 || m[2] < 0 {
				continue
			}
			bounds = append(bounds, boundary{
				start:    m[0],
				end:      m[1],
				key:      strings.TrimSpace(text[m[2]:m[3]]),
				strategy: strat,
				order:    i,
			})
		}
	}

	sort.SliceStable(bounds, func(i, j int) bool {
		if bounds[i].start != bounds[j].start {
			return bounds[i].start < bounds[j].start
		}
		return bounds[i].order < bounds[j].order
	})

	// Drop boundaries that start inside a previous boundary's own match, which
	// happens when two strategies fire on the same heading.
	var merged []boundary
	prevEnd := -1
	for _, b := range bounds {
		if b.start < prevEnd {
			continue
		}
		merged = append(merged, b)
		prevEnd = b.end
	}

	var segments []Segment

	// Anything before the first boundary is real content that no rule claimed.
	firstStart := len(text)
	if len(merged) > 0 {
		firstStart = merged[0].start
	}
	if pre := strings.TrimSpace(text[:firstStart]); pre != "" {
		seg := Segment{
			Key:      "preamble",
			Section:  "Preamble / front matter",
			Title:    firstLine(pre),
			Body:     pre,
			Strategy: "preamble",
			IDPrefix: strings.ToUpper(prof.ID),
			Offset:   0,
		}
		seg.Flags = append(seg.Flags, "no-boundary-rule-matched")
		segments = append(segments, seg)
	}

	for i, b := range merged {
		end := len(text)
		if i+1 < len(merged) {
			end = merged[i+1].start
		}
		rest := text[b.end:end]
		title, body := splitTitleBody(rest)

		seg := Segment{
			Key:      b.key,
			Section:  b.strategy.SectionLabel(b.key),
			Title:    title,
			Body:     strings.TrimSpace(body),
			Strategy: b.strategy.Type,
			IDPrefix: prof.PrefixFor(*b.strategy),
			Offset:   b.start,
		}
		if seg.Title == "" {
			seg.AddFlag("no-title-detected")
		}
		segments = append(segments, seg)
	}

	assessConfidence(segments, prof)
	return segments, nil
}

// AddFlag records a segmentation concern once.
func (s *Segment) AddFlag(flag string) {
	for _, f := range s.Flags {
		if f == flag {
			return
		}
	}
	s.Flags = append(s.Flags, flag)
}

var pageNumberLine = regexp.MustCompile(`^(?:page\s+)?\d+(\s*(/|of)\s*\d+)?$`)

func assessConfidence(segments []Segment, prof *profile.Profile) {
	minBody := map[string]int{}
	for _, s := range prof.Segmentation {
		n := s.MinBodyChars
		if n <= 0 {
			n = defaultMinBodyChars
		}
		minBody[s.Type] = n
	}

	seen := map[string]int{}
	for i := range segments {
		seg := &segments[i]

		threshold, ok := minBody[seg.Strategy]
		if !ok {
			threshold = defaultMinBodyChars
		}
		if len(seg.Body) < threshold {
			seg.AddFlag("short-body")
		}
		if pageNumberLine.MatchString(strings.ToLower(strings.TrimSpace(seg.Body))) {
			seg.AddFlag("possible-page-number-noise")
		}
		if seg.Strategy == "preamble" {
			seg.AddFlag("preamble")
		}

		dupKey := seg.Strategy + "/" + seg.Key
		seen[dupKey]++
		if seen[dupKey] > 1 {
			seg.AddFlag("duplicate-section-key")
		}

		switch {
		case hasAny(seg.Flags, "preamble", "duplicate-section-key", "possible-page-number-noise", "short-body", "no-title-detected"):
			seg.Confidence = requirement.ConfidenceLow
		case len(seg.Body) < threshold*3:
			seg.Confidence = requirement.ConfidenceMedium
		default:
			seg.Confidence = requirement.ConfidenceHigh
		}
	}
}

func hasAny(flags []string, want ...string) bool {
	for _, f := range flags {
		for _, w := range want {
			if f == w {
				return true
			}
		}
	}
	return false
}

// splitTitleBody takes the text following a heading marker and separates a
// plausible title from the body.
func splitTitleBody(rest string) (title, body string) {
	rest = strings.TrimLeft(rest, " \t")
	line, remainder, _ := strings.Cut(rest, "\n")
	line = strings.TrimSpace(line)

	// Heading number alone on its line: the title is the next non-empty line.
	if line == "" {
		next, after := nextNonEmptyLine(remainder)
		if next != "" && len(next) <= 160 {
			return cleanTitle(next), after
		}
		return "", remainder
	}
	// A long first line is body prose, not a title.
	if len(line) > 160 {
		return "", rest
	}
	return cleanTitle(line), remainder
}

func nextNonEmptyLine(s string) (line, rest string) {
	for {
		l, r, ok := strings.Cut(s, "\n")
		if strings.TrimSpace(l) != "" {
			return strings.TrimSpace(l), r
		}
		if !ok {
			return "", ""
		}
		s = r
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return cleanTitle(line)
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "-–—:.\t ")
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return strings.TrimSpace(s)
}

// BuildRequirements segments text and mints requirement records: IDs from the
// profile's prefixes, categories from its classification rules. Everything
// starts at status=draft.
func BuildRequirements(text string, prof *profile.Profile) (*Result, error) {
	segments, err := SegmentText(text, prof)
	if err != nil {
		return nil, err
	}

	res := &Result{}
	used := map[string]int{}
	byStrategy := map[string]int{}

	for _, seg := range segments {
		id := mintID(seg.IDPrefix, seg.Key)
		used[id]++
		if n := used[id]; n > 1 {
			id = fmt.Sprintf("%s-DUP%d", id, n)
		}

		req := requirement.Requirement{
			ID:         id,
			Framework:  prof.ID,
			Section:    seg.Section,
			Key:        seg.Key,
			Title:      seg.Title,
			Text:       seg.Body,
			Status:     requirement.StatusDraft,
			Strategy:   seg.Strategy,
			Confidence: seg.Confidence,
			Flags:      seg.Flags,
		}
		req.Category = prof.Classify(seg.Key, seg.Title+"\n"+seg.Body)
		if req.Category == "" {
			req.Category = requirement.Unclassified
			req.AddFlag("unclassified")
		}
		res.Requirements = append(res.Requirements, req)
		byStrategy[seg.Strategy]++
	}

	res.Requirements.Sort()

	strategies := make([]string, 0, len(byStrategy))
	for k := range byStrategy {
		strategies = append(strategies, k)
	}
	sort.Strings(strategies)
	for _, s := range strategies {
		res.Log = append(res.Log, fmt.Sprintf("strategy %-9s produced %d requirement(s)", s, byStrategy[s]))
	}

	var low, unclassified int
	for _, r := range res.Requirements {
		if r.Confidence == requirement.ConfidenceLow {
			low++
		}
		if r.IsUnclassified() {
			unclassified++
		}
	}
	res.Log = append(res.Log,
		fmt.Sprintf("%d requirement(s) flagged low-confidence — these are shown first at GATE 1", low),
		fmt.Sprintf("%d requirement(s) left unclassified — no classification rule matched", unclassified),
	)
	return res, nil
}

var idUnsafe = regexp.MustCompile(`[^A-Za-z0-9.\-]+`)

func mintID(prefix, key string) string {
	key = idUnsafe.ReplaceAllString(strings.TrimSpace(key), "-")
	key = strings.Trim(key, "-")
	key = strings.ToUpper(key)
	if key == "" {
		key = "UNKNOWN"
	}
	if prefix == "" {
		return key
	}
	return prefix + "-" + key
}
