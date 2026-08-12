package doctemplate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Brand is the editable half of templates/typst/brand.typ, surfaced as a form
// at /templates/manage and stored per install.
//
// Every field here is interpolated into template source that a typesetter then
// executes, so validation is a security boundary, not a nicety: Typst markup
// can read files and LaTeX can do considerably worse. Validate rejects anything
// that is not a literal colour, a plain font name, a bounded number or a short
// line of prose, and the generators quote what remains. Nothing reaches a
// template unvalidated.
type Brand struct {
	Firm       string `json:"firm"`
	Tagline    string `json:"tagline"`
	FooterNote string `json:"footer_note"`

	Accent     string `json:"accent"`
	AccentDark string `json:"accent_dark"`
	Ink        string `json:"ink"`
	Muted      string `json:"muted"`
	Rule       string `json:"rule"`
	TableHead  string `json:"table_head"`
	TableZebra string `json:"table_zebra"`
	OK         string `json:"ok"`
	Warn       string `json:"warn"`
	Risk       string `json:"risk"`

	SerifStack []string `json:"serif_stack"`
	SansStack  []string `json:"sans_stack"`
	MonoStack  []string `json:"mono_stack"`

	BodySize  float64 `json:"body_size"`
	SmallSize float64 `json:"small_size"`
	MicroSize float64 `json:"micro_size"`

	Paper        string  `json:"paper"`
	MarginX      float64 `json:"margin_x"`
	MarginTop    float64 `json:"margin_top"`
	MarginBottom float64 `json:"margin_bottom"`

	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// DefaultBrand mirrors the dictionary in templates/typst/brand.typ. It is the
// fallback for a fresh install and the target of the Reset action, so the two
// have to stay in step — TestDefaultBrandMatchesBrandTyp asserts that they do.
func DefaultBrand() Brand {
	return Brand{
		Firm:       "CareLock Consulting",
		Tagline:    "Risk and Control Advisory",
		FooterNote: "Uncontrolled when printed.",

		Accent:     "#7a1f2e",
		AccentDark: "#5c1622",
		Ink:        "#1b1b1b",
		Muted:      "#5f6368",
		Rule:       "#c8c5c0",
		TableHead:  "#f2f0ed",
		TableZebra: "#faf9f8",
		OK:         "#1d6b45",
		Warn:       "#8a6100",
		Risk:       "#a32b1c",

		SerifStack: []string{"Libertinus Serif", "Liberation Serif", "Times New Roman", "DejaVu Serif"},
		SansStack:  []string{"Liberation Sans", "Arial", "Noto Sans", "DejaVu Sans"},
		MonoStack:  []string{"Liberation Mono", "DejaVu Sans Mono", "Courier New"},

		BodySize:  11,
		SmallSize: 9,
		MicroSize: 7.5,

		Paper:        "a4",
		MarginX:      30,
		MarginTop:    28,
		MarginBottom: 26,
	}
}

// Papers is the allowlist of page sizes. Short on purpose: the templates set
// their measure from the margin values, and a paper size the geometry was never
// checked against produces a document with a line length outside the legible
// band the templates exist to hold.
var Papers = []string{"a4", "us-letter"}

var (
	colourPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// A font name is letters, digits, spaces and a small set of punctuation
	// that real family names use ("PT Serif", "Noto Sans", "Helvetica-Light").
	// Anything else is rejected rather than escaped: there is no legitimate
	// family name containing a quote or a backslash, so one appearing is a sign
	// of an injection attempt rather than an unusual typeface.
	fontPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 .+\-]{0,63}$`)
)

// maxProseLen bounds the free-text identity fields. They sit on a cover page
// and in a running footer; anything longer is a paste accident.
const maxProseLen = 120

// maxStackLen bounds a font fallback chain. Typst tries each in order, and a
// long chain is mostly a way to make every build print warnings.
const maxStackLen = 8

// Validate normalises the brand in place and reports the first problem found.
// It is called on every save and again before every render, so a row written by
// an older version of this code cannot reach a template unchecked.
func (b *Brand) Validate() error {
	fields := []struct {
		name  string
		value *string
	}{
		{"firm", &b.Firm},
		{"tagline", &b.Tagline},
		{"footer note", &b.FooterNote},
	}
	for _, f := range fields {
		*f.value = strings.TrimSpace(*f.value)
		if strings.ContainsFunc(*f.value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("%s must not contain control characters", f.name)
		}
		// Quotes and backslashes are rejected rather than relied on being
		// escaped. The generators do escape them, and that is the defence that
		// matters — but these values are interpolated into two languages that
		// execute their input, and no real firm name or footer note contains
		// either character. Refusing them costs nothing and removes the whole
		// class of "did every generator remember to escape".
		if strings.ContainsAny(*f.value, "\"\\") {
			return fmt.Errorf(`%s must not contain quotes or backslashes`, f.name)
		}
		if len([]rune(*f.value)) > maxProseLen {
			return fmt.Errorf("%s must be %d characters or fewer", f.name, maxProseLen)
		}
	}
	if b.Firm == "" {
		return fmt.Errorf("firm name is required")
	}

	colours := []struct {
		name  string
		value *string
	}{
		{"accent", &b.Accent},
		{"accent dark", &b.AccentDark},
		{"ink", &b.Ink},
		{"muted", &b.Muted},
		{"rule", &b.Rule},
		{"table header", &b.TableHead},
		{"table zebra", &b.TableZebra},
		{"ok", &b.OK},
		{"warn", &b.Warn},
		{"risk", &b.Risk},
	}
	for _, c := range colours {
		*c.value = strings.ToLower(strings.TrimSpace(*c.value))
		if !colourPattern.MatchString(*c.value) {
			return fmt.Errorf("%s colour must be a six-digit hex value like #7a1f2e, got %q", c.name, *c.value)
		}
	}

	stacks := []struct {
		name  string
		value *[]string
	}{
		{"serif", &b.SerifStack},
		{"sans", &b.SansStack},
		{"mono", &b.MonoStack},
	}
	for _, s := range stacks {
		cleaned := make([]string, 0, len(*s.value))
		for _, name := range *s.value {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !fontPattern.MatchString(name) {
				return fmt.Errorf("%s font name %q is not a valid family name", s.name, name)
			}
			cleaned = append(cleaned, name)
		}
		if len(cleaned) == 0 {
			return fmt.Errorf("%s font stack must name at least one family", s.name)
		}
		if len(cleaned) > maxStackLen {
			return fmt.Errorf("%s font stack must have %d families or fewer", s.name, maxStackLen)
		}
		*s.value = cleaned
	}

	sizes := []struct {
		name  string
		value float64
	}{
		{"body size", b.BodySize},
		{"small size", b.SmallSize},
		{"micro size", b.MicroSize},
	}
	for _, s := range sizes {
		if s.value < 5 || s.value > 24 {
			return fmt.Errorf("%s must be between 5pt and 24pt, got %g", s.name, s.value)
		}
	}
	// Ordering, not just range. The templates use these three as a hierarchy —
	// body for prose, small for field values, micro for labels — and inverting
	// them produces a document whose labels shout over its text.
	if b.SmallSize > b.BodySize {
		return fmt.Errorf("small size (%g) must not exceed body size (%g)", b.SmallSize, b.BodySize)
	}
	if b.MicroSize > b.SmallSize {
		return fmt.Errorf("micro size (%g) must not exceed small size (%g)", b.MicroSize, b.SmallSize)
	}

	b.Paper = strings.ToLower(strings.TrimSpace(b.Paper))
	if !containsString(Papers, b.Paper) {
		return fmt.Errorf("paper must be one of %s, got %q", strings.Join(Papers, ", "), b.Paper)
	}

	margins := []struct {
		name  string
		value float64
	}{
		{"side margin", b.MarginX},
		{"top margin", b.MarginTop},
		{"bottom margin", b.MarginBottom},
	}
	for _, m := range margins {
		if m.value < 10 || m.value > 60 {
			return fmt.Errorf("%s must be between 10mm and 60mm, got %g", m.name, m.value)
		}
	}
	return nil
}

func containsString(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}

// ---- Typst generation ----

// typstString quotes a validated prose value as a Typst string literal. Validate
// has already rejected control characters, so escaping the two structural
// characters is sufficient.
func typstString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func typstStack(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, typstString(name))
	}
	// A one-element Typst array needs the trailing comma or it parses as a
	// parenthesised expression, and the templates would then index a string.
	if len(quoted) == 1 {
		return "(" + quoted[0] + ",)"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

// formatPt renders a size without a trailing ".0", so the generated source
// reads like something a person wrote.
func formatPt(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// TypstDict renders the brand as the `#let brand = (...)` dictionary that
// templates/typst/brand.typ defines. The key set is exactly the one the file
// ships with — the templates read every one, and omitting a key would fault the
// render at the point it is read rather than here.
func (b Brand) TypstDict() string {
	var sb strings.Builder
	sb.WriteString("#let brand = (\n")
	sb.WriteString("  // Generated by internal/doctemplate from the saved brand settings.\n")
	sb.WriteString("  // Edit at /templates/manage, not here — this file is a render-time copy.\n")
	fmt.Fprintf(&sb, "  firm: %s,\n", typstString(b.Firm))
	fmt.Fprintf(&sb, "  firm-tagline: %s,\n", typstString(b.Tagline))
	// Logo stays none: a path here would be read by the typesetter from inside
	// the sandboxed workspace, and shipping an upload path into that is a
	// separate feature with its own containment questions.
	sb.WriteString("  logo: none,\n")
	sb.WriteString("  logo-width: 38mm,\n")
	fmt.Fprintf(&sb, "  accent: rgb(%s),\n", typstString(b.Accent))
	fmt.Fprintf(&sb, "  accent-dark: rgb(%s),\n", typstString(b.AccentDark))
	fmt.Fprintf(&sb, "  ink: rgb(%s),\n", typstString(b.Ink))
	fmt.Fprintf(&sb, "  muted: rgb(%s),\n", typstString(b.Muted))
	fmt.Fprintf(&sb, "  rule: rgb(%s),\n", typstString(b.Rule))
	fmt.Fprintf(&sb, "  table-head: rgb(%s),\n", typstString(b.TableHead))
	fmt.Fprintf(&sb, "  table-zebra: rgb(%s),\n", typstString(b.TableZebra))
	fmt.Fprintf(&sb, "  ok: rgb(%s),\n", typstString(b.OK))
	fmt.Fprintf(&sb, "  warn: rgb(%s),\n", typstString(b.Warn))
	fmt.Fprintf(&sb, "  risk: rgb(%s),\n", typstString(b.Risk))
	fmt.Fprintf(&sb, "  serif: %s,\n", typstStack(b.SerifStack))
	fmt.Fprintf(&sb, "  sans: %s,\n", typstStack(b.SansStack))
	fmt.Fprintf(&sb, "  mono: %s,\n", typstStack(b.MonoStack))
	fmt.Fprintf(&sb, "  body-size: %spt,\n", formatPt(b.BodySize))
	fmt.Fprintf(&sb, "  small-size: %spt,\n", formatPt(b.SmallSize))
	fmt.Fprintf(&sb, "  micro-size: %spt,\n", formatPt(b.MicroSize))
	fmt.Fprintf(&sb, "  paper: %s,\n", typstString(b.Paper))
	fmt.Fprintf(&sb, "  margin: (x: %smm, top: %smm, bottom: %smm),\n",
		formatPt(b.MarginX), formatPt(b.MarginTop), formatPt(b.MarginBottom))
	fmt.Fprintf(&sb, "  footer-note: %s,\n", typstString(b.FooterNote))
	sb.WriteString(")")
	return sb.String()
}

// brandDictStart is the declaration the splice looks for in brand.typ.
const brandDictStart = "#let brand = ("

// ApplyToTypstBrand rewrites the `#let brand = (...)` dictionary in the source
// of templates/typst/brand.typ, leaving the helper functions below it untouched.
//
// A splice rather than generating the whole file: brand.typ is not only the
// dictionary, it also defines classification-colour, wordmark, label-text,
// field and pill, which close over `brand` at their definition site. Appending
// a redefinition would not reach them, and regenerating the file wholesale
// would mean this package owning code that belongs to whoever designs the
// templates. Rewriting exactly the data and nothing else is the narrow change.
//
// It returns an error rather than falling back to the file's own values when
// the block cannot be found: silently rendering the wrong brand is worse than a
// failed render, and TestApplyToTypstBrandFindsBlock keeps a reformat of
// brand.typ from reaching production as a surprise.
func ApplyToTypstBrand(source string, brand Brand) (string, error) {
	start := strings.Index(source, brandDictStart)
	if start < 0 {
		return "", fmt.Errorf("brand dictionary %q not found in brand.typ", brandDictStart)
	}
	// The dictionary is a top-level declaration, so its closing parenthesis is
	// the first one at column zero after the opening line. Scanning for that
	// rather than counting parens is what makes nested calls like rgb("#…") and
	// margin: (x: …) inside the literal harmless.
	rest := source[start:]
	end := strings.Index(rest, "\n)\n")
	if end < 0 {
		return "", fmt.Errorf("brand dictionary in brand.typ is not closed by a parenthesis at column zero")
	}
	return source[:start] + brand.TypstDict() + rest[end+2:], nil
}

// ---- LaTeX generation ----

// LaTeXPreamble renders the brand as overrides applied after
// \usepackage{carelock}. Redefinition rather than a rewritten .sty: \definecolor
// and \renewcommand both take effect at that point, so the package file stays
// exactly as shipped and the generated document carries the whole delta.
func (b Brand) LaTeXPreamble() string {
	var sb strings.Builder
	sb.WriteString("% ---- Brand overrides, generated from the saved brand settings ----\n")
	fmt.Fprintf(&sb, "\\renewcommand{\\clFirm}{%s}\n", latexEscape(b.Firm))
	fmt.Fprintf(&sb, "\\renewcommand{\\clTagline}{%s}\n", latexEscape(b.Tagline))
	fmt.Fprintf(&sb, "\\renewcommand{\\clFooterNote}{%s}\n", latexEscape(b.FooterNote))

	colours := []struct{ name, value string }{
		{"clAccent", b.Accent},
		{"clAccentDark", b.AccentDark},
		{"clInk", b.Ink},
		{"clMuted", b.Muted},
		{"clRule", b.Rule},
		{"clTableHead", b.TableHead},
		{"clTableZebra", b.TableZebra},
		{"clOk", b.OK},
		{"clWarn", b.Warn},
		{"clRisk", b.Risk},
	}
	for _, c := range colours {
		fmt.Fprintf(&sb, "\\definecolor{%s}{HTML}{%s}\n", c.name, strings.ToUpper(strings.TrimPrefix(c.value, "#")))
	}
	// Re-assert the body colour: \color{clInk} ran in the package with the old
	// definition, so redefining the colour alone would not change the text.
	sb.WriteString("\\color{clInk}\n")

	// Fonts are guarded exactly as carelock.sty guards them. An unavailable
	// family is a hard error in fontspec, which would turn a brand typo into a
	// failed build rather than a fallback.
	sb.WriteString("\\ifPDFTeX\\else\n")
	for _, f := range []struct {
		command string
		stack   []string
	}{
		{"setmainfont", b.SerifStack},
		{"setsansfont", b.SansStack},
		{"setmonofont", b.MonoStack},
	} {
		for _, family := range f.stack {
			fmt.Fprintf(&sb, "  \\IfFontExistsTF{%s}{\\%s{%s}}{}%%\n", family, f.command, family)
		}
	}
	sb.WriteString("\\fi\n")

	paper := "a4paper"
	if b.Paper == "us-letter" {
		paper = "letterpaper"
	}
	fmt.Fprintf(&sb, "\\geometry{%s, left=%smm, right=%smm, top=%smm, bottom=%smm,\n",
		paper, formatPt(b.MarginX), formatPt(b.MarginX), formatPt(b.MarginTop), formatPt(b.MarginBottom))
	sb.WriteString("          headheight=14pt, headsep=10mm, footskip=12mm}\n")
	return sb.String()
}

// LaTeXClassOptions maps the body size onto the article class options. LaTeX
// only offers 10, 11 and 12pt there, so an arbitrary size is snapped to the
// nearest — the alternative is loading extsizes, a package tectonic would have
// to fetch and a minimal TeX install may not have.
func (b Brand) LaTeXClassOptions() string {
	size := "11pt"
	switch {
	case b.BodySize < 10.5:
		size = "10pt"
	case b.BodySize >= 11.5:
		size = "12pt"
	}
	paper := "a4paper"
	if b.Paper == "us-letter" {
		paper = "letterpaper"
	}
	return size + "," + paper
}
