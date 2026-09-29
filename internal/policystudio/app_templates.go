package policystudio

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Templates made in the app sit next to the built-in ones: drafted by the AI
// from a library document, imported from JSON, or copied from another
// template. Each keeps a working copy (draft) and, once published, the copy
// New document offers, which only the next publish changes. They are held to
// the same Problems as a built-in template, and cannot be published with any.

// App template statuses.
const (
	TemplateDraft     = "draft"     // never published
	TemplatePublished = "published" // offered by New document
	TemplateRetired   = "retired"   // no longer offered; documents made from it keep their guidance
)

// App template origins.
const (
	OriginLibrary = "library"
	OriginImport  = "import"
	OriginCopy    = "copy"
)

// SourceRef is a library passage a drafted section drew on.
type SourceRef struct {
	N       int    `json:"n"`
	Heading string `json:"heading"`
}

// AppTemplate is a template made in the app, with its working copy.
type AppTemplate struct {
	ID               int64                  `json:"id"`
	TemplateID       string                 `json:"template_id"`
	Status           string                 `json:"status"`
	Draft            Template               `json:"draft"`
	PublishedVersion string                 `json:"published_version"`
	Changed          bool                   `json:"unpublished_changes"`
	Origin           string                 `json:"origin"`
	SourceLibraryID  int64                  `json:"source_library_id"`
	SourceTitle      string                 `json:"source_title"`
	Sources          map[string][]SourceRef `json:"sources"`
	Notes            []string               `json:"notes"`
	AIProvider       string                 `json:"ai_provider"`
	AIModel          string                 `json:"ai_model"`
	InputTokens      int                    `json:"input_tokens"`
	OutputTokens     int                    `json:"output_tokens"`
	CreatedBy        string                 `json:"created_by"`
	CreatedAt        string                 `json:"created_at"`
	UpdatedBy        string                 `json:"updated_by"`
	UpdatedAt        string                 `json:"updated_at"`
	PublishedBy      string                 `json:"published_by"`
	PublishedAt      string                 `json:"published_at"`
	Problems         []string               `json:"problems"`
	Documents        int                    `json:"documents"`
}

// NewAppTemplate is what creating one takes.
type NewAppTemplate struct {
	Template        Template
	Origin          string
	SourceLibraryID int64
	SourceTitle     string
	Sources         map[string][]SourceRef
	Notes           []string
	AIProvider      string
	AIModel         string
	InputTokens     int
	OutputTokens    int
}

// ErrTemplateProblems refuses a publish while the template has problems.
type ErrTemplateProblems struct{ Problems []string }

func (e ErrTemplateProblems) Error() string {
	return "the template cannot be published until these are fixed: " + strings.Join(e.Problems, "; ")
}

const appTemplateColumns = `id, template_id, status, draft_json, published_version, published_json, origin, source_library_id, source_title,
	sources_json, notes_json, ai_provider, ai_model, input_tokens, output_tokens, created_by, created_at, updated_by, updated_at,
	published_by, published_at`

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// TemplateSlug turns a title into a template id.
func TemplateSlug(title string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 56 {
		slug = strings.TrimRight(slug[:56], "-")
	}
	if slug == "" || slug[0] < 'a' || slug[0] > 'z' {
		slug = "template-" + slug
	}
	return strings.TrimRight(slug, "-")
}

// SectionSeed turns a heading into a uid_seed unique among taken.
func SectionSeed(heading string, taken map[string]bool) string {
	base := TemplateSlug(heading)
	seed := base
	for i := 2; taken[seed]; i++ {
		seed = base + "-" + strconv.Itoa(i)
	}
	taken[seed] = true
	return seed
}

func (s *Service) templateIDTaken(id string) (bool, error) {
	if s.builtInTemplate(id) {
		return true, nil
	}
	var n int
	err := s.store.db.QueryRow(`SELECT COUNT(*) FROM policy_templates WHERE template_id = ?`, id).Scan(&n)
	return n > 0, err
}

// freeTemplateID returns id, or id-2, id-3… when it is taken.
func (s *Service) freeTemplateID(id string) (string, error) {
	candidate := id
	for i := 2; ; i++ {
		taken, err := s.templateIDTaken(candidate)
		if err != nil || !taken {
			return candidate, err
		}
		candidate = strings.TrimRight(id, "-") + "-" + strconv.Itoa(i)
	}
}

// normalise applies what every app template is held to whatever the source:
// never the default, an id that matches its row, and no nil lists.
func normaliseTemplate(t Template, id string) Template {
	t.ID = id
	t.Default = false
	if t.Frameworks == nil {
		t.Frameworks = []string{}
	}
	if t.Facts == nil {
		t.Facts = []TemplateFact{}
	}
	if t.Sections == nil {
		t.Sections = []TemplateSection{}
	}
	for i := range t.Sections {
		if t.Sections[i].ProposedMappings == nil {
			t.Sections[i].ProposedMappings = []ProposedMapping{}
		}
	}
	return t
}

// CreateAppTemplate stores a new working copy. Its id is the template's own,
// or a free variant of it.
func (s *Service) CreateAppTemplate(in NewAppTemplate, actor string) (AppTemplate, error) {
	id := in.Template.ID
	if !templateIDPattern.MatchString(id) {
		id = TemplateSlug(in.Template.Title)
	}
	id, err := s.freeTemplateID(id)
	if err != nil {
		return AppTemplate{}, err
	}
	t := normaliseTemplate(in.Template, id)
	if strings.TrimSpace(t.Version) == "" {
		t.Version = "draft"
	}
	draft, _ := json.Marshal(t)
	sources, _ := json.Marshal(nonNilSources(in.Sources))
	notes, _ := json.Marshal(nonNilStrings(in.Notes))
	now := s.now().UTC().Format(time.RFC3339Nano)
	row, err := s.store.db.Insert(`INSERT INTO policy_templates (template_id, status, draft_json, origin, source_library_id, source_title,
		sources_json, notes_json, ai_provider, ai_model, input_tokens, output_tokens, created_by, created_at, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, TemplateDraft, string(draft), in.Origin, in.SourceLibraryID,
		in.SourceTitle, string(sources), string(notes), in.AIProvider, in.AIModel, in.InputTokens, in.OutputTokens, actor, now, actor, now)
	if err != nil {
		return AppTemplate{}, fmt.Errorf("store template: %w", err)
	}
	return s.AppTemplate(row)
}

func nonNilSources(m map[string][]SourceRef) map[string][]SourceRef {
	if m == nil {
		return map[string][]SourceRef{}
	}
	return m
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ImportTemplate stores a template given as JSON, the format of the built-in
// files and of Download JSON. An id already in use is refused rather than
// renamed: an import is usually the same template arriving from another
// installation, and two copies under different ids would drift apart.
func (s *Service) ImportTemplate(raw []byte, actor string) (AppTemplate, error) {
	var t Template
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return AppTemplate{}, invalid("that is not a template: %v", err)
	}
	if !templateIDPattern.MatchString(t.ID) {
		return AppTemplate{}, invalid("the template's id %q must be 2 to 64 lowercase letters, digits and hyphens", t.ID)
	}
	if taken, err := s.templateIDTaken(t.ID); err != nil {
		return AppTemplate{}, err
	} else if taken {
		return AppTemplate{}, ErrConflict{Msg: fmt.Sprintf("a template with the id %q already exists", t.ID)}
	}
	return s.CreateAppTemplate(NewAppTemplate{Template: t, Origin: OriginImport}, actor)
}

// CopyTemplate starts a working copy from an existing template.
func (s *Service) CopyTemplate(templateID, actor string) (AppTemplate, error) {
	t, ok := s.Template(templateID)
	if !ok {
		return AppTemplate{}, invalid("unknown template %q", templateID)
	}
	t.ID = TemplateSlug(templateID + "-copy")
	t.Title = "Copy of " + t.Title
	t.Version = "draft"
	return s.CreateAppTemplate(NewAppTemplate{Template: t, Origin: OriginCopy, Notes: []string{"Copied from " + templateID + "."}}, actor)
}

// AppTemplates lists the templates made in the app, working ones first.
func (s *Service) AppTemplates() ([]AppTemplate, error) {
	rows, err := s.store.db.Query(`SELECT ` + appTemplateColumns + ` FROM policy_templates ORDER BY
		CASE status WHEN 'draft' THEN 0 WHEN 'published' THEN 1 ELSE 2 END, updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []AppTemplate{}
	for rows.Next() {
		t, err := scanAppTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Documents, err = s.documentsFrom(out[i].TemplateID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AppTemplate returns one, with its problems and how many documents use it.
func (s *Service) AppTemplate(id int64) (AppTemplate, error) {
	t, err := scanAppTemplate(s.store.db.QueryRow(`SELECT `+appTemplateColumns+` FROM policy_templates WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AppTemplate{}, ErrTemplateNotFound
	}
	if err != nil {
		return AppTemplate{}, err
	}
	t.Documents, err = s.documentsFrom(t.TemplateID)
	return t, err
}

// ErrTemplateNotFound is an app template id that does not exist.
var ErrTemplateNotFound = errors.New("template not found")

func (s *Service) documentsFrom(templateID string) (int, error) {
	var n int
	err := s.store.db.QueryRow(`SELECT COUNT(*) FROM policy_documents WHERE template_id = ?`, templateID).Scan(&n)
	return n, err
}

func scanAppTemplate(r rowScanner) (AppTemplate, error) {
	var t AppTemplate
	var draft, published, sources, notes string
	if err := r.Scan(&t.ID, &t.TemplateID, &t.Status, &draft, &t.PublishedVersion, &published, &t.Origin, &t.SourceLibraryID,
		&t.SourceTitle, &sources, &notes, &t.AIProvider, &t.AIModel, &t.InputTokens, &t.OutputTokens, &t.CreatedBy, &t.CreatedAt,
		&t.UpdatedBy, &t.UpdatedAt, &t.PublishedBy, &t.PublishedAt); err != nil {
		return AppTemplate{}, err
	}
	if err := json.Unmarshal([]byte(draft), &t.Draft); err != nil {
		return AppTemplate{}, fmt.Errorf("template %s: %w", t.TemplateID, err)
	}
	_ = json.Unmarshal([]byte(sources), &t.Sources)
	_ = json.Unmarshal([]byte(notes), &t.Notes)
	t.Sources, t.Notes = nonNilSources(t.Sources), nonNilStrings(t.Notes)
	t.Draft = normaliseTemplate(t.Draft, t.TemplateID)
	t.Changed = t.Status != TemplateDraft && !sameTemplate(draft, published)
	t.Problems = nonNilStrings(t.Draft.Problems())
	return t, nil
}

// sameTemplate compares two stored templates, ignoring the version the
// working copy carries.
func sameTemplate(a, b string) bool {
	var ta, tb Template
	if json.Unmarshal([]byte(a), &ta) != nil || json.Unmarshal([]byte(b), &tb) != nil {
		return a == b
	}
	ta.Version, tb.Version = "", ""
	ra, _ := json.Marshal(ta)
	rb, _ := json.Marshal(tb)
	return string(ra) == string(rb)
}

// SaveAppTemplate replaces a working copy. It is kept whatever its problems:
// a draft is allowed to be unfinished. Its id can change until it is first
// published, and never after, because documents refer to it.
func (s *Service) SaveAppTemplate(id int64, t Template, actor string) (AppTemplate, error) {
	cur, err := s.AppTemplate(id)
	if err != nil {
		return AppTemplate{}, err
	}
	templateID := cur.TemplateID
	if t.ID != "" && t.ID != cur.TemplateID {
		if cur.Status != TemplateDraft {
			return AppTemplate{}, invalid("a published template's id cannot change: documents refer to it")
		}
		if !templateIDPattern.MatchString(t.ID) {
			return AppTemplate{}, invalid("the id %q must be 2 to 64 lowercase letters, digits and hyphens, starting with a letter", t.ID)
		}
		if taken, err := s.templateIDTaken(t.ID); err != nil {
			return AppTemplate{}, err
		} else if taken {
			return AppTemplate{}, ErrConflict{Msg: fmt.Sprintf("a template with the id %q already exists", t.ID)}
		}
		templateID = t.ID
	}
	t = normaliseTemplate(t, templateID)
	t.Version = cur.Draft.Version
	raw, _ := json.Marshal(t)
	if _, err := s.store.db.Exec(`UPDATE policy_templates SET template_id = ?, draft_json = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
		templateID, string(raw), actor, s.now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return AppTemplate{}, fmt.Errorf("save template: %w", err)
	}
	return s.AppTemplate(id)
}

// nextTemplateVersion is 1.0.0 for a first publish, then the next minor.
func nextTemplateVersion(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "1.0.0"
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return "1.0.0"
	}
	return strconv.Itoa(major) + "." + strconv.Itoa(minor+1) + ".0"
}

// PublishAppTemplate offers the working copy in New document, as the next
// version. A template with problems is refused. Documents already made from
// an earlier version keep their text; they record the version they came from.
func (s *Service) PublishAppTemplate(id int64, actor string) (AppTemplate, error) {
	cur, err := s.AppTemplate(id)
	if err != nil {
		return AppTemplate{}, err
	}
	if len(cur.Problems) > 0 {
		return AppTemplate{}, ErrTemplateProblems{Problems: cur.Problems}
	}
	if cur.Status == TemplatePublished && !cur.Changed {
		return cur, nil
	}
	t := cur.Draft
	t.Version = nextTemplateVersion(cur.PublishedVersion)
	raw, _ := json.Marshal(t)
	now := s.now().UTC().Format(time.RFC3339Nano)
	if _, err := s.store.db.Exec(`UPDATE policy_templates SET status = ?, draft_json = ?, published_json = ?, published_version = ?,
		published_by = ?, published_at = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
		TemplatePublished, string(raw), string(raw), t.Version, actor, now, actor, now, id); err != nil {
		return AppTemplate{}, fmt.Errorf("publish template: %w", err)
	}
	return s.AppTemplate(id)
}

// RetireAppTemplate stops offering a published template; RestoreAppTemplate
// offers it again, at its last published version.
func (s *Service) RetireAppTemplate(id int64, actor string) (AppTemplate, error) {
	return s.setTemplateStatus(id, TemplatePublished, TemplateRetired, actor)
}

func (s *Service) RestoreAppTemplate(id int64, actor string) (AppTemplate, error) {
	return s.setTemplateStatus(id, TemplateRetired, TemplatePublished, actor)
}

func (s *Service) setTemplateStatus(id int64, from, to, actor string) (AppTemplate, error) {
	cur, err := s.AppTemplate(id)
	if err != nil {
		return AppTemplate{}, err
	}
	if cur.Status != from {
		return AppTemplate{}, ErrConflict{Msg: fmt.Sprintf("the template is %s, not %s", cur.Status, from)}
	}
	if _, err := s.store.db.Exec(`UPDATE policy_templates SET status = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
		to, actor, s.now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return AppTemplate{}, fmt.Errorf("update template: %w", err)
	}
	return s.AppTemplate(id)
}

// DeleteAppTemplate removes a template no document was made from. One that
// documents use is retired instead, so they keep its guidance.
func (s *Service) DeleteAppTemplate(id int64) error {
	cur, err := s.AppTemplate(id)
	if err != nil {
		return err
	}
	if cur.Documents > 0 {
		return ErrConflict{Msg: fmt.Sprintf("%d document(s) were made from this template; retire it instead", cur.Documents)}
	}
	_, err = s.store.db.Exec(`DELETE FROM policy_templates WHERE id = ?`, id)
	return err
}

// publishedTemplates reads the published copies, retired ones too when asked.
func (s *Service) publishedTemplates(withRetired bool) ([]Template, error) {
	statuses := `'published'`
	if withRetired {
		statuses = `'published', 'retired'`
	}
	rows, err := s.store.db.Query(`SELECT template_id, published_json FROM policy_templates WHERE status IN (` + statuses + `) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Template
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var t Template
		if err := json.Unmarshal([]byte(raw), &t); err != nil {
			s.log.Error("policy studio: a published template does not decode", "template", id, "error", err)
			continue
		}
		out = append(out, normaliseTemplate(t, id))
	}
	return out, rows.Err()
}

func (s *Service) retiredTemplate(id string) bool {
	var n int
	_ = s.store.db.QueryRow(`SELECT COUNT(*) FROM policy_templates WHERE template_id = ? AND status = ?`, id, TemplateRetired).Scan(&n)
	return n > 0
}
