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

// Result reports what a segmentation run produced.
type Result struct {
	Requirements requirement.Set
	Log          []string
}

// Piece is one passage of a regulation as the Wintermute server cut it: which
// strategy found it, the label and key the instrument numbers it by, its title,
// its text, and — when the server cut a long requirement into several passages
// — which part it is, from 1.
//
// Segmenting is reading, and reading is that server's (see AI_AGENT.md): it
// holds the framework profiles' boundary rules and cuts the document once.
// What stays here is this module's own — requirement ids, categories, the seed
// crosswalk and the review flags — applied to the requirements it cut.
type Piece struct {
	Strategy string
	Label    string
	Key      string
	Title    string
	Body     string
	Part     int
}

// SegmentsFrom turns the server's passages into segments, joining the parts of
// a requirement it cut into several, in document order. The preamble — text
// before the first requirement — is kept, never dropped, as it always was.
func SegmentsFrom(pieces []Piece, prof *profile.Profile) []Segment {
	var segments []Segment
	for _, p := range pieces {
		if p.Part > 1 && len(segments) > 0 {
			last := &segments[len(segments)-1]
			if last.Key == p.Key && last.Strategy == p.Strategy {
				last.Body = strings.TrimSpace(last.Body + "\n\n" + p.Body)
				continue
			}
		}
		seg := Segment{Key: p.Key, Title: p.Title, Body: strings.TrimSpace(p.Body), Strategy: p.Strategy}
		if p.Strategy == "preamble" {
			seg.Key, seg.Section = "preamble", "Preamble / front matter"
			seg.IDPrefix = strings.ToUpper(prof.ID)
			seg.AddFlag("no-boundary-rule-matched")
		} else {
			strat := strategyFor(prof, p)
			seg.Section, seg.IDPrefix = p.Label, prof.IDPrefix
			if strat != nil {
				seg.Section, seg.IDPrefix = strat.SectionLabel(p.Key), prof.PrefixFor(*strat)
			}
			if seg.Title == "" {
				seg.AddFlag("no-title-detected")
			}
		}
		segments = append(segments, seg)
	}
	assessConfidence(segments, prof)
	return segments
}

// strategyFor is the profile's strategy that produced a piece: the same type
// and, for a pattern of the profile's own, the label the server wrote in front
// of the key — which is what tells "Requirement 8" from "8.3.1" in PCI DSS, and
// gives each its id prefix.
func strategyFor(prof *profile.Profile, p Piece) *profile.Strategy {
	var fallback *profile.Strategy
	for i := range prof.Segmentation {
		s := &prof.Segmentation[i]
		if s.Type != p.Strategy {
			continue
		}
		if s.Label != "" && strings.HasPrefix(p.Label, s.Label+" ") {
			return s
		}
		if fallback == nil {
			fallback = s
		}
	}
	return fallback
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

// BuildRequirements mints requirement records from the requirements the
// Wintermute server cut a regulation into: IDs from the profile's prefixes,
// categories from its classification rules. Everything starts at status=draft.
func BuildRequirements(pieces []Piece, prof *profile.Profile) (*Result, error) {
	segments := SegmentsFrom(pieces, prof)

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
