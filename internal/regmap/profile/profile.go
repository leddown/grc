// Package profile loads framework profiles. A profile is the *only* place
// framework-specific knowledge lives: segmentation rules, classification rules,
// and the seed crosswalk. Adding a framework means adding a YAML file here, not
// editing the pipeline.
package profile

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Segmentation strategy names.
const (
	StrategyArticle  = "article"
	StrategyNumbered = "numbered"
	StrategyAnnex    = "annex"
	StrategyRegex    = "regex"
)

// Profile describes one source framework.
type Profile struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"displayName"`
	SourceRef   string `yaml:"sourceRef"`
	// IDPrefix is the default prefix for minted requirement IDs. A
	// segmentation strategy may override it (e.g. articles vs annexes).
	IDPrefix string `yaml:"idPrefix"`

	Detect         Detect         `yaml:"detect"`
	Segmentation   []Strategy     `yaml:"segmentation"`
	Classification Classification `yaml:"classification"`
	SeedCrosswalk  []SeedEntry    `yaml:"seedCrosswalk"`

	// path records where the profile was loaded from, for error messages.
	path string `yaml:"-"`
}

// Detect holds hints used to guess the framework from a filename or the
// extracted text. Detection is always confirmed by a human at GATE 1.
type Detect struct {
	FilenamePatterns []string `yaml:"filenamePatterns"`
	ContentPatterns  []string `yaml:"contentPatterns"`
}

// Strategy is one segmentation rule. Strategies compose: a profile may declare
// several and the segmenter merges their boundaries in document order.
type Strategy struct {
	Type string `yaml:"type"`
	// Label is the heading word for article/annex strategies ("Article",
	// "Annex", "Requirement"). Defaults to the capitalized strategy type.
	Label string `yaml:"label,omitempty"`
	// Pattern is the boundary regexp for type "regex". It must contain at
	// least one capture group; group 1 becomes the section key.
	Pattern string `yaml:"pattern,omitempty"`
	// MinDepth/MaxDepth bound the hierarchy depth for "numbered" (a depth of
	// 2 matches "3.4", 3 matches "3.4.1").
	MinDepth int `yaml:"minDepth,omitempty"`
	MaxDepth int `yaml:"maxDepth,omitempty"`
	// IDPrefix overrides Profile.IDPrefix for requirements from this strategy.
	IDPrefix string `yaml:"idPrefix,omitempty"`
	// MinBodyChars marks shorter segments low-confidence for GATE 1 triage.
	MinBodyChars int `yaml:"minBodyChars,omitempty"`

	compiled *regexp.Regexp
}

// Classification maps requirements to a framework's own category/pillar labels.
type Classification struct {
	// Categories is the declared vocabulary, used to validate edits at GATE 1.
	Categories []string             `yaml:"categories"`
	Rules      []ClassificationRule `yaml:"rules"`
}

// ClassificationRule assigns a category to requirements matching its selector.
type ClassificationRule struct {
	Category string  `yaml:"category"`
	Match    Matcher `yaml:"match"`
}

// SeedEntry is a known requirement-group -> NIST control mapping. Seed hits are
// starting points with confidence=high and status=draft; GATE 2 validates them.
type SeedEntry struct {
	Group     string   `yaml:"group"`
	Match     Matcher  `yaml:"match"`
	Controls  []string `yaml:"controls"`
	Rationale string   `yaml:"rationale"`
}

// Matcher selects requirements by section number, section prefix, or keyword.
// It is shared by classification rules and seed crosswalk entries so that both
// use one generic, framework-agnostic selector implementation.
type Matcher struct {
	// Articles is a numeric selector over the section key: "5-16", "17",
	// "5,7,9", or a combination ("5-16,45").
	Articles string `yaml:"articles,omitempty"`
	// Sections matches section keys by prefix ("3." matches "3.4.1").
	Sections []string `yaml:"sections,omitempty"`
	// Annexes matches annex keys exactly, case-insensitively ("I", "II").
	Annexes []string `yaml:"annexes,omitempty"`
	// Keywords match case-insensitively against title + body text.
	Keywords []string `yaml:"keywords,omitempty"`
}

// IsZero reports whether the matcher would select nothing.
func (m Matcher) IsZero() bool {
	return m.Articles == "" && len(m.Sections) == 0 && len(m.Annexes) == 0 && len(m.Keywords) == 0
}

// Matches reports whether a requirement (identified by its section key and
// text) satisfies the selector. Selectors are OR-ed: any hit matches.
func (m Matcher) Matches(key, text string) bool {
	key = strings.TrimSpace(key)
	if m.Articles != "" && matchNumericRange(m.Articles, key) {
		return true
	}
	for _, p := range m.Sections {
		if p != "" && strings.HasPrefix(key, p) {
			return true
		}
	}
	for _, a := range m.Annexes {
		if strings.EqualFold(strings.TrimSpace(a), key) {
			return true
		}
	}
	if len(m.Keywords) > 0 {
		lower := strings.ToLower(text)
		for _, kw := range m.Keywords {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if kw != "" && strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

var leadingNumber = regexp.MustCompile(`^(\d+)`)

// matchNumericRange reports whether key's leading number falls inside spec,
// which is a comma-separated list of integers and inclusive ranges.
func matchNumericRange(spec, key string) bool {
	m := leadingNumber.FindStringSubmatch(key)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, ok := strings.Cut(part, "-")
		if !ok {
			if v, err := strconv.Atoi(strings.TrimSpace(lo)); err == nil && v == n {
				return true
			}
			continue
		}
		lv, err1 := strconv.Atoi(strings.TrimSpace(lo))
		hv, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if err1 == nil && err2 == nil && n >= lv && n <= hv {
			return true
		}
	}
	return false
}

// Classify returns the first matching category, or requirement.Unclassified's
// value when nothing matches. Rules are evaluated in declaration order so the
// result is deterministic.
func (p *Profile) Classify(key, text string) string {
	for _, rule := range p.Classification.Rules {
		if rule.Match.IsZero() {
			continue
		}
		if rule.Match.Matches(key, text) {
			return rule.Category
		}
	}
	return ""
}

// Seed returns the first seed crosswalk entry matching the requirement.
func (p *Profile) Seed(key, text string) (SeedEntry, bool) {
	for _, entry := range p.SeedCrosswalk {
		if entry.Match.IsZero() {
			continue
		}
		if entry.Match.Matches(key, text) {
			return entry, true
		}
	}
	return SeedEntry{}, false
}

// PrefixFor returns the ID prefix to use for a given strategy.
func (p *Profile) PrefixFor(s Strategy) string {
	if s.IDPrefix != "" {
		return s.IDPrefix
	}
	return p.IDPrefix
}

// Path returns the file the profile was loaded from.
func (p *Profile) Path() string { return p.path }

// Validate checks a profile for the mistakes that would otherwise surface as
// confusing runtime behaviour.
func (p *Profile) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("profile %s: id is required", p.path)
	}
	if p.IDPrefix == "" {
		return fmt.Errorf("profile %s: idPrefix is required", p.ID)
	}
	if len(p.Segmentation) == 0 {
		return fmt.Errorf("profile %s: at least one segmentation strategy is required", p.ID)
	}
	for i := range p.Segmentation {
		s := &p.Segmentation[i]
		switch s.Type {
		case StrategyArticle, StrategyAnnex, StrategyNumbered:
		case StrategyRegex:
			if s.Pattern == "" {
				return fmt.Errorf("profile %s: regex strategy requires a pattern", p.ID)
			}
		default:
			return fmt.Errorf("profile %s: unknown segmentation strategy %q (want article, numbered, annex or regex)", p.ID, s.Type)
		}
		if _, err := s.Regexp(); err != nil {
			return fmt.Errorf("profile %s: segmentation strategy %s: %w", p.ID, s.Type, err)
		}
	}
	known := map[string]bool{}
	for _, c := range p.Classification.Categories {
		known[c] = true
	}
	for _, r := range p.Classification.Rules {
		if r.Category == "" {
			return fmt.Errorf("profile %s: classification rule without a category", p.ID)
		}
		if len(known) > 0 && !known[r.Category] {
			return fmt.Errorf("profile %s: classification rule uses category %q which is not declared in categories", p.ID, r.Category)
		}
	}
	for _, s := range p.SeedCrosswalk {
		if len(s.Controls) == 0 {
			return fmt.Errorf("profile %s: seed crosswalk group %q has no controls", p.ID, s.Group)
		}
		if s.Match.IsZero() {
			return fmt.Errorf("profile %s: seed crosswalk group %q has no match selector", p.ID, s.Group)
		}
	}
	return nil
}

// Regexp returns the compiled boundary pattern for a strategy.
func (s *Strategy) Regexp() (*regexp.Regexp, error) {
	if s.compiled != nil {
		return s.compiled, nil
	}
	pattern, err := s.buildPattern()
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile boundary pattern %q: %w", pattern, err)
	}
	s.compiled = re
	return re, nil
}

func (s *Strategy) buildPattern() (string, error) {
	switch s.Type {
	case StrategyArticle:
		label := s.Label
		if label == "" {
			label = "Article"
		}
		// "Article 5", "Article 5a", "Art. 12" at the start of a line.
		return `(?im)^[ \t]*(?:` + regexp.QuoteMeta(label) + `|` + regexp.QuoteMeta(abbrev(label)) + `\.?)[ \t]+(\d+[a-z]?)\b[ \t]*:?[ \t]*`, nil
	case StrategyAnnex:
		label := s.Label
		if label == "" {
			label = "Annex"
		}
		// "Annex I", "Annex 1", optionally with a lettered/numbered point.
		return `(?im)^[ \t]*` + regexp.QuoteMeta(label) + `[ \t]+([IVXLCDM]+|\d+)\b[ \t]*:?[ \t]*`, nil
	case StrategyNumbered:
		minDepth := s.MinDepth
		if minDepth < 1 {
			minDepth = 1
		}
		maxDepth := s.MaxDepth
		if maxDepth < minDepth {
			maxDepth = minDepth + 2
		}
		// A hierarchical requirement number followed by body text, e.g.
		// "3.4.1 Render PAN unreadable...". minDepth-1..maxDepth-1 dot groups.
		// Go's regexp is RE2, so no lookahead: the trailing run of spaces is
		// simply consumed as part of the boundary.
		return fmt.Sprintf(`(?m)^[ \t]*(\d+(?:\.\d+){%d,%d})[ \t]+`, minDepth-1, maxDepth-1), nil
	case StrategyRegex:
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return "", fmt.Errorf("invalid pattern: %w", err)
		}
		if re.NumSubexp() < 1 {
			return "", fmt.Errorf("pattern %q must contain a capture group for the section key", s.Pattern)
		}
		return s.Pattern, nil
	}
	return "", fmt.Errorf("unknown strategy type %q", s.Type)
}

// abbrev makes a short form for a heading label ("Article" -> "Art").
func abbrev(label string) string {
	if len(label) <= 3 {
		return label
	}
	return label[:3]
}

// SectionLabel renders the display form of a section for this strategy.
func (s *Strategy) SectionLabel(key string) string {
	switch s.Type {
	case StrategyArticle:
		label := s.Label
		if label == "" {
			label = "Article"
		}
		return label + " " + key
	case StrategyAnnex:
		label := s.Label
		if label == "" {
			label = "Annex"
		}
		return label + " " + key
	default:
		if s.Label != "" {
			return s.Label + " " + key
		}
		return key
	}
}

// Load reads a single profile file.
func Load(path string) (*Profile, error) {
	// #nosec G304 -- profiles are operator-authored configuration read from the
	// --profiles-dir the operator chose.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", path, err)
	}
	return Parse(path, raw)
}

// Parse builds a profile from YAML already in memory. name is used for error
// messages and as the profile's reported path; it need not be a real file, so
// a profile can come from an embedded filesystem or a literal as readily as
// from the operator's --profiles-dir.
func Parse(name string, raw []byte) (*Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse profile %s: %w", name, err)
	}
	p.path = name
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Registry is the set of profiles available on disk.
type Registry struct {
	profiles []*Profile
}

// NewRegistry builds a registry from profiles already in memory, for a caller
// that assembles its own set rather than reading a directory.
func NewRegistry(profiles ...*Profile) *Registry {
	reg := &Registry{profiles: profiles}
	reg.sort()
	return reg
}

// LoadDir loads every *.yaml/*.yml profile in dir.
func LoadDir(dir string) (*Registry, error) {
	return loadFS(os.DirFS(dir), ".", dir)
}

// LoadFS loads every *.yaml/*.yml profile in dir within fsys. It is what the
// server uses: the web module ships the profiles embedded in the binary rather
// than depending on a directory next to it.
func LoadFS(fsys fs.FS, dir string) (*Registry, error) {
	return loadFS(fsys, dir, dir)
}

func loadFS(fsys fs.FS, dir, label string) (*Registry, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read profiles directory %s: %w", label, err)
	}
	reg := &Registry{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		name := path.Join(dir, e.Name())
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read profile %s: %w", name, err)
		}
		p, err := Parse(filepath.Join(label, e.Name()), raw)
		if err != nil {
			return nil, err
		}
		reg.profiles = append(reg.profiles, p)
	}
	if len(reg.profiles) == 0 {
		return nil, fmt.Errorf("no framework profiles found in %s", label)
	}
	reg.sort()
	return reg, nil
}

func (r *Registry) sort() {
	sort.SliceStable(r.profiles, func(i, j int) bool {
		return r.profiles[i].ID < r.profiles[j].ID
	})
}

// All returns every loaded profile, sorted by ID.
func (r *Registry) All() []*Profile { return r.profiles }

// Get returns the profile with the given ID.
func (r *Registry) Get(id string) (*Profile, error) {
	for _, p := range r.profiles {
		if strings.EqualFold(p.ID, id) {
			return p, nil
		}
	}
	var ids []string
	for _, p := range r.profiles {
		ids = append(ids, p.ID)
	}
	return nil, fmt.Errorf("unknown framework %q (available: %s)", id, strings.Join(ids, ", "))
}

// Guess is a scored framework detection result.
type Guess struct {
	Profile *Profile
	Score   int
	Reasons []string
}

// Detect scores every profile against a filename and the extracted text and
// returns the candidates in descending score order. A caller must always let
// the human confirm or override the top guess.
func (r *Registry) Detect(filename, text string) []Guess {
	base := strings.ToLower(filepath.Base(filename))
	// Only the head of the document is scanned: title pages carry the
	// identifying references and this keeps detection fast on large PDFs.
	head := text
	if len(head) > 20000 {
		head = head[:20000]
	}
	lowerHead := strings.ToLower(head)

	var guesses []Guess
	for _, p := range r.profiles {
		g := Guess{Profile: p}
		for _, pat := range p.Detect.FilenamePatterns {
			if pat == "" {
				continue
			}
			if strings.Contains(base, strings.ToLower(pat)) {
				g.Score += 3
				g.Reasons = append(g.Reasons, fmt.Sprintf("filename contains %q", pat))
			}
		}
		for _, pat := range p.Detect.ContentPatterns {
			if pat == "" {
				continue
			}
			if strings.Contains(lowerHead, strings.ToLower(pat)) {
				g.Score += 2
				g.Reasons = append(g.Reasons, fmt.Sprintf("text contains %q", pat))
			}
		}
		if g.Score > 0 {
			guesses = append(guesses, g)
		}
	}
	sort.SliceStable(guesses, func(i, j int) bool {
		if guesses[i].Score != guesses[j].Score {
			return guesses[i].Score > guesses[j].Score
		}
		return guesses[i].Profile.ID < guesses[j].Profile.ID
	})
	return guesses
}
