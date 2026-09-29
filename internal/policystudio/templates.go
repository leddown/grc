package policystudio

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"grc/internal/policydocs"
)

// Templates are the starting points a Studio document can be created from:
// one JSON file each in templates/, embedded so the binary carries them.
// POLICY_STUDIO.md ("Adding a template") says how to write one.
//
//go:embed templates/*.json
var templateFS embed.FS

// Template is one starting point.
type Template struct {
	ID                  string            `json:"id"`
	Version             string            `json:"version"`
	Title               string            `json:"title"`
	DocType             string            `json:"doc_type"`
	Frameworks          []string          `json:"frameworks"`
	Description         string            `json:"description"`
	ReviewCadenceMonths int               `json:"review_cadence_months"`
	Classification      string            `json:"classification"`
	Default             bool              `json:"default"`
	Sections            []TemplateSection `json:"sections"`
	Facts               []TemplateFact    `json:"facts"`
}

// TemplateSection is one section of a template. Content is restricted
// Markdown with {{fact:key}} and [[control:ID]] tokens; Guidance is shown only
// in the Studio, never in the document.
type TemplateSection struct {
	UIDSeed          string            `json:"uid_seed"`
	Heading          string            `json:"heading"`
	Kind             string            `json:"kind"`
	Content          string            `json:"content"`
	Guidance         string            `json:"guidance"`
	ProposedMappings []ProposedMapping `json:"proposed_mappings"`
}

// ProposedMapping is a draft control claim a template suggests. Templates
// never claim full coverage: whether a client's text satisfies a control on its
// own is the consultant's call.
type ProposedMapping struct {
	ControlID string `json:"control_id"`
	Coverage  string `json:"coverage"`
	Note      string `json:"note"`
}

// TemplateFact declares a client fact a template's text refers to. Example
// is shown to the person filling it in and is never applied.
type TemplateFact struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Example     string `json:"example"`
	ValueType   string `json:"value_type"`
}

var (
	templateIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	factTokenInText   = regexp.MustCompile(`\{\{fact:([a-z][a-z0-9_]{0,63})\}\}`)
)

// loadTemplates reads and validates every embedded template. A template that
// does not validate is a build defect, so TestTemplatesAreValid exercises this.
func loadTemplates() ([]Template, error) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	var out []Template
	ids := map[string]bool{}
	for _, e := range entries {
		raw, err := templateFS.ReadFile(path.Join("templates", e.Name()))
		if err != nil {
			return nil, err
		}
		var t Template
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&t); err != nil {
			return nil, fmt.Errorf("template %s: %w", e.Name(), err)
		}
		if err := t.validate(); err != nil {
			return nil, fmt.Errorf("template %s: %w", e.Name(), err)
		}
		if ids[t.ID] {
			return nil, fmt.Errorf("template id %q is used twice", t.ID)
		}
		ids[t.ID] = true
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

func (t Template) validate() error {
	if problems := t.Problems(); len(problems) > 0 {
		return errors.New(problems[0])
	}
	return nil
}

// weakTemplateWording is what a template may not say: an assessor cannot test
// it, and a template repeats it into every document made from it.
var weakTemplateWording = []string{"will ", "strive", "endeavour", "where possible", "as appropriate", "is encouraged"}

// Problems lists everything that keeps a template from being offered, in the
// order a person would fix them. Built-in templates must have none (a build
// defect otherwise); a template drafted in the app cannot be published until
// it has none.
func (t Template) Problems() []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if !templateIDPattern.MatchString(t.ID) {
		add("the id %q must be 2 to 64 lowercase letters, digits and hyphens, starting with a letter", t.ID)
	}
	if strings.TrimSpace(t.Version) == "" || strings.TrimSpace(t.Title) == "" {
		add("a version and a title are required")
	}
	if !containsString(policydocs.DocTypes, t.DocType) {
		add("unknown doc_type %q", t.DocType)
	}
	for _, f := range t.Frameworks {
		if !containsString(policydocs.Frameworks, f) {
			add("unknown framework %q", f)
		}
	}
	if len(t.Sections) == 0 {
		add("a template needs at least one section")
	}
	declared := map[string]bool{}
	for _, f := range t.Facts {
		if !factKeyPattern.MatchString(f.Key) || strings.TrimSpace(f.Label) == "" {
			add("fact %q needs a snake_case key and a label", f.Key)
		}
		if declared[f.Key] {
			add("fact %q is declared twice", f.Key)
		}
		declared[f.Key] = true
	}
	used := map[string]bool{}
	kinds := map[string]bool{}
	seeds := map[string]bool{}
	for _, s := range t.Sections {
		name := firstNonEmptyString(s.Heading, s.UIDSeed, "(untitled)")
		if s.UIDSeed == "" || seeds[s.UIDSeed] {
			add("section %q needs a unique uid_seed", name)
		}
		seeds[s.UIDSeed] = true
		if strings.TrimSpace(s.Heading) == "" {
			add("a section needs a heading")
		}
		if !containsString(policydocs.SectionKinds, s.Kind) {
			add("section %q has an unknown kind %q", name, s.Kind)
		}
		kinds[s.Kind] = true
		sec := SectionFromMarkdown("s", s.Kind, s.Heading, s.Content, newBlockID)
		if err := Validate(&Node{Type: "doc", Content: []*Node{sec}}); err != nil {
			add("section %q: %v", name, err)
		}
		for _, m := range factTokenInText.FindAllStringSubmatch(s.Content, -1) {
			used[m[1]] = true
		}
		lower := strings.ToLower(s.Content)
		for _, weak := range weakTemplateWording {
			if strings.Contains(lower, weak) {
				add("section %q says %q, which an assessor cannot test: use must, shall, should or may", name, strings.TrimSpace(weak))
			}
		}
		for _, m := range s.ProposedMappings {
			if m.Coverage != policydocs.CoveragePartial && m.Coverage != policydocs.CoverageSupporting {
				add("section %q proposes %s as %q; a template proposes partial or supporting coverage only", name, m.ControlID, m.Coverage)
			}
			if !controlPattern.MatchString(m.ControlID) {
				add("section %q proposes an invalid control id %q", name, m.ControlID)
			}
		}
	}
	for _, k := range sortedKeys(used) {
		if !declared[k] {
			add("the text uses {{fact:%s}}, which the template does not declare", k)
		}
	}
	for _, f := range t.Facts {
		if f.Key != "" && !used[f.Key] {
			add("fact %q is declared but never used", f.Key)
		}
	}
	for _, k := range policydocs.RequiredKinds(t.DocType) {
		if !kinds[k] {
			add("a %s needs a %s section", t.DocType, k)
		}
	}
	return out
}

// FactKeysIn lists the fact keys a template text uses, in order of first use.
func FactKeysIn(content string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range factTokenInText.FindAllStringSubmatch(content, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func containsString(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// Templates lists the templates a document can be created from: the built-in
// ones, the default first, then those published in the app.
func (s *Service) Templates() []Template {
	out := append([]Template(nil), s.templates...)
	published, err := s.publishedTemplates(false)
	if err != nil {
		s.log.Error("policy studio: published templates", "error", err)
	}
	return append(out, published...)
}

// Template finds a template by id, built in or published in the app, retired
// ones included: a document made from a retired template still shows its
// guidance.
func (s *Service) Template(id string) (Template, bool) {
	for _, t := range s.templates {
		if t.ID == id {
			return t, true
		}
	}
	published, err := s.publishedTemplates(true)
	if err != nil {
		s.log.Error("policy studio: published templates", "error", err)
	}
	for _, t := range published {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// builtInTemplate reports whether an id belongs to a template in the binary.
func (s *Service) builtInTemplate(id string) bool {
	for _, t := range s.templates {
		if t.ID == id {
			return true
		}
	}
	return false
}

// templateProvenance is a template section's provenance_detail:
// "<id>@<version>#<uid_seed>". The Studio reads it back to show the section's
// guidance.
func templateProvenance(t Template, s TemplateSection) string {
	return t.ID + "@" + t.Version + "#" + s.UIDSeed
}

// guidanceFor returns the template guidance for a section row, if it came
// from a template this build still has.
func (s *Service) guidanceFor(row policydocs.Section) string {
	if row.Provenance != policydocs.ProvenanceTemplate {
		return ""
	}
	ref, seed, ok := strings.Cut(row.ProvenanceDetail, "#")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(ref, "@")
	t, ok := s.Template(id)
	if !ok {
		return ""
	}
	for _, sec := range t.Sections {
		if sec.UIDSeed == seed {
			return sec.Guidance
		}
	}
	return ""
}

// CreateRequest is what creating a document from a template takes.
type CreateRequest struct {
	TemplateID      string `json:"template_id"`
	ClientProfileID int64  `json:"client_profile_id"`
	Title           string `json:"title"`
	Reference       string `json:"reference"`
	OwnerRole       string `json:"owner_role"`
}

// CreateResult is the new document and the proposed mappings the catalog did
// not know, which were skipped rather than stored as claims that never resolve.
type CreateResult struct {
	Document        policydocs.Document `json:"document"`
	SkippedMappings []string            `json:"skipped_mappings"`
}

// CreateFromTemplate instantiates a template, on the server: the document,
// its section rows with their content, and the proposed mappings that the
// catalog knows as draft claims. Known client facts render by value from the
// start; unknown ones are unresolved tokens that block approval until filled.
func (s *Service) CreateFromTemplate(in CreateRequest, author string) (CreateResult, error) {
	t, ok := s.Template(in.TemplateID)
	if !ok || s.retiredTemplate(in.TemplateID) {
		return CreateResult{}, invalid("unknown template %q", in.TemplateID)
	}
	var clientName string
	if in.ClientProfileID > 0 {
		if s.clients == nil {
			return CreateResult{}, invalid("client profiles are not available")
		}
		p, err := s.clients.Get(in.ClientProfileID)
		if err != nil {
			return CreateResult{}, invalid("unknown client")
		}
		clientName = p.Name
	}
	facts, err := s.factValues(in.ClientProfileID)
	if err != nil {
		return CreateResult{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = t.Title
	}
	owner := strings.TrimSpace(in.OwnerRole)
	if owner == "" {
		owner = facts["policy_owner_role"]
	}
	doc, err := s.policies.CreateDocument(policydocs.Document{
		Title: title, Reference: in.Reference, DocType: t.DocType, Frameworks: t.Frameworks,
		ReviewCadenceMonths: t.ReviewCadenceMonths, Classification: t.Classification, OwnerRole: owner,
		ClientProfileID: in.ClientProfileID, ClientName: clientName, Summary: t.Description,
		TemplateID: t.ID, TemplateVersion: t.Version, EditorFormat: policydocs.EditorStudio, Author: author,
	})
	if err != nil {
		return CreateResult{}, err
	}
	result := CreateResult{Document: doc, SkippedMappings: []string{}}
	for _, ts := range t.Sections {
		uid := policydocs.NewUID()
		node := SectionFromMarkdown(uid, ts.Kind, ts.Heading, ts.Content, newBlockID)
		if err := Validate(node); err != nil {
			return result, fmt.Errorf("template section %q: %w", ts.Heading, err)
		}
		base := Resolve(node, Baseline)
		content, _ := json.Marshal(node)
		blocks, _ := json.Marshal(SectionBlocks(base, facts))
		row, err := s.policies.CreateSection(policydocs.Section{
			DocumentID: doc.ID, UID: uid, Heading: ts.Heading, SectionKind: ts.Kind,
			Body: SectionMarkdown(base, facts), Provenance: policydocs.ProvenanceTemplate,
			ProvenanceDetail: templateProvenance(t, ts), ContentJSON: string(content), BlocksJSON: string(blocks),
		})
		if err != nil {
			return result, err
		}
		for _, m := range ts.ProposedMappings {
			if _, err := s.policies.AttachControl(doc.ID, row.ID, policydocs.ControlRef{ControlID: m.ControlID, Coverage: m.Coverage, Note: m.Note}); err != nil {
				result.SkippedMappings = append(result.SkippedMappings, m.ControlID+" ("+ts.Heading+"): "+err.Error())
			}
		}
	}
	result.Document, err = s.policies.GetDocument(doc.ID)
	return result, err
}
