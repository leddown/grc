package oscal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"grc/internal/controlid"
)

// The four baselines NIST publishes as profiles, in the order grc lists them.
var Baselines = []string{"low", "moderate", "high", "privacy"}

// SourceInfo identifies a posted OSCAL file.
type SourceInfo struct {
	Title        string `json:"title"`
	Version      string `json:"version"`
	OSCALVersion string `json:"oscal_version"`
	LastModified string `json:"last_modified"`
	UUID         string `json:"uuid"`
	SHA256       string `json:"sha256"`
	Bytes        int    `json:"bytes"`
}

// ImportedControl is one control or enhancement read from a catalog, already
// in the shape grc's catalog stores. The three JSON fields hold the upstream
// OSCAL structure as compact JSON ("null" when the control has none), written
// the same way every time so a comparison with the stored value is a string
// comparison.
type ImportedControl struct {
	OSCALID        string
	ID             string
	Type           string
	Name           string
	Requirements   string
	Discussion     string
	Related        []string
	Withdrawn      bool
	ParamsJSON     string
	StatementJSON  string
	AssessmentJSON string
	// KeepStatement is set when the statement is grc's own text (the grc
	// prose prop): Requirements is that text verbatim, StatementJSON stays
	// "null", and the importer leaves the stored statement structure alone.
	KeepStatement bool
}

// CatalogImport is everything a catalog import reads from the posted files.
type CatalogImport struct {
	Source   SourceInfo
	Controls []ImportedControl
	// Profiles and Baselines are keyed by the baseline slot the file was posted
	// in ("low", ...); a slot with no file is absent from both.
	Profiles  map[string]SourceInfo
	Baselines map[string][]string
	// Problems lists everything wrong with the files. An import with any
	// problem is shown but not applied.
	Problems []string
}

// The control types grc's catalog uses; the import writes exactly these.
const (
	TypeControl     = "Control"
	TypeEnhancement = "Control Enhancement"
)

// ReadCatalogImport reads an OSCAL catalog and the baseline profiles posted
// with it. It returns an error only when the catalog itself cannot be read as
// an OSCAL catalog; anything else wrong is a Problem, so a preview can list
// all of them at once.
func ReadCatalogImport(catalogJSON []byte, profiles map[string][]byte) (CatalogImport, error) {
	var doc Document
	if err := strictModel(catalogJSON, "catalog", &doc); err != nil {
		return CatalogImport{}, err
	}
	cat := doc.Catalog
	out := CatalogImport{
		Source:    sourceInfo(cat.Metadata, cat.UUID, catalogJSON),
		Profiles:  map[string]SourceInfo{},
		Baselines: map[string][]string{},
	}
	if !strings.HasPrefix(out.Source.OSCALVersion, "1.") {
		out.Problems = append(out.Problems, fmt.Sprintf("the catalog declares oscal-version %q; grc reads OSCAL 1.x", out.Source.OSCALVersion))
	}

	type found struct {
		control Control
		nested  bool
		parent  string
	}
	var all []found
	var walkControls func(cs *[]Control, parent string)
	walkControls = func(cs *[]Control, parent string) {
		if cs == nil {
			return
		}
		for _, c := range *cs {
			all = append(all, found{control: c, nested: parent != "", parent: parent})
			walkControls(c.Controls, c.ID)
		}
	}
	var walkGroups func(gs *[]Group)
	walkGroups = func(gs *[]Group) {
		if gs == nil {
			return
		}
		for _, g := range *gs {
			walkControls(g.Controls, "")
			walkGroups(g.Groups)
		}
	}
	walkControls(cat.Controls, "")
	walkGroups(cat.Groups)
	if len(all) == 0 {
		return CatalogImport{}, fmt.Errorf("the catalog holds no controls")
	}

	// Parameters are looked up across the whole catalog: an enhancement's
	// statement can insert a parameter its parent defines (IA-13(3) inserts
	// ia-13_odp.01). The control's own definition wins over another's.
	global := map[string]Parameter{}
	index := catalogIndex{tokenToID: map[string]string{}, children: map[string][]string{}}
	tokenToID := index.tokenToID
	for _, f := range all {
		if f.control.Params != nil {
			for _, p := range *f.control.Params {
				if _, seen := global[p.ID]; !seen {
					global[p.ID] = p
				}
			}
		}
		tokenToID[f.control.ID] = controlIDOf(f.control)
		if f.parent != "" {
			index.children[f.parent] = append(index.children[f.parent], f.control.ID)
		}
	}
	if cat.Params != nil {
		for _, p := range *cat.Params {
			if _, seen := global[p.ID]; !seen {
				global[p.ID] = p
			}
		}
	}

	seen := map[string]string{}
	for _, f := range all {
		c := f.control
		id := tokenToID[c.ID]
		if id == "" {
			out.Problems = append(out.Problems, fmt.Sprintf("control %q has no label and an id grc cannot read", c.ID))
			continue
		}
		if other, dup := seen[id]; dup {
			out.Problems = append(out.Problems, fmt.Sprintf("%s appears twice (OSCAL ids %s and %s)", id, other, c.ID))
			continue
		}
		seen[id] = c.ID
		local := map[string]Parameter{}
		for k, v := range global {
			local[k] = v
		}
		if c.Params != nil {
			for _, p := range *c.Params {
				local[p.ID] = p
			}
		}
		r := newTextRenderer(local)
		ic := ImportedControl{
			OSCALID:        c.ID,
			ID:             id,
			Type:           TypeControl,
			Name:           strings.TrimSpace(c.Title),
			Related:        []string{},
			Withdrawn:      hasProp(c.Props, "status", "withdrawn"),
			ParamsJSON:     "null",
			StatementJSON:  "null",
			AssessmentJSON: "null",
		}
		if f.nested {
			ic.Type = TypeEnhancement
		}
		if ic.Name == "" {
			out.Problems = append(out.Problems, fmt.Sprintf("%s has no title", id))
		}
		if c.Params != nil && len(*c.Params) > 0 {
			ic.ParamsJSON = compactJSON(*c.Params)
		}
		var assessment []Part
		var haveStatement, haveGuidance bool
		if c.Parts != nil {
			for i := range *c.Parts {
				part := (*c.Parts)[i]
				switch part.Name {
				case "statement":
					if haveStatement {
						continue
					}
					haveStatement = true
					if isGRCText(part.Props) {
						ic.Requirements = strings.TrimSpace(part.Prose)
						ic.KeepStatement = true
						continue
					}
					ic.StatementJSON = compactJSON(part)
					ic.Requirements = r.statement(&part)
				case "guidance":
					if haveGuidance {
						continue
					}
					haveGuidance = true
					if isGRCText(part.Props) {
						ic.Discussion = strings.TrimSpace(part.Prose)
						continue
					}
					ic.Discussion = strings.TrimSpace(r.text(part.Prose, 0))
				case "assessment-objective", "assessment-method":
					assessment = append(assessment, part)
				}
			}
		}
		if len(assessment) > 0 {
			ic.AssessmentJSON = compactJSON(assessment)
		}
		if c.Links != nil {
			for _, l := range *c.Links {
				if l.Rel != "related" {
					continue
				}
				token := strings.TrimPrefix(strings.TrimSpace(l.Href), "#")
				if rid := tokenToID[token]; rid != "" {
					ic.Related = append(ic.Related, rid)
				} else if rid := controlid.Normalize(token); rid != "" {
					ic.Related = append(ic.Related, rid)
				}
			}
		}
		out.Problems = append(out.Problems, r.missingProblems(id)...)
		out.Controls = append(out.Controls, ic)
	}

	for _, slot := range Baselines {
		raw, ok := profiles[slot]
		if !ok || len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		info, ids, problems := readBaselineProfile(slot, raw, index)
		out.Problems = append(out.Problems, problems...)
		if info != nil {
			out.Profiles[slot] = *info
			out.Baselines[slot] = ids
		}
	}
	for slot := range profiles {
		if !isBaseline(slot) {
			out.Problems = append(out.Problems, fmt.Sprintf("%q is not a baseline grc knows; post the low, moderate, high or privacy profile", slot))
		}
	}
	return out, nil
}

// readBaselineProfile resolves a profile to the grc control ids it selects
// from the catalog. It reads with-ids selections (with their children when the
// profile asks) and exclusions; pattern matching is reported, not guessed at.
func readBaselineProfile(slot string, raw []byte, index catalogIndex) (*SourceInfo, []string, []string) {
	tokenToID, children := index.tokenToID, index.children
	label := strings.ToUpper(slot)
	var doc Document
	if err := strictModel(raw, "profile", &doc); err != nil {
		return nil, nil, []string{fmt.Sprintf("the %s profile: %v", label, err)}
	}
	p := doc.Profile
	info := sourceInfo(p.Metadata, p.UUID, raw)
	var problems []string
	title := strings.ToUpper(p.Metadata.Title)
	if !strings.Contains(title, label) {
		for _, other := range Baselines {
			if other != slot && strings.Contains(title, strings.ToUpper(other)) {
				problems = append(problems, fmt.Sprintf("the file posted as the %s profile is titled %q; post it in the %s slot", label, p.Metadata.Title, strings.ToUpper(other)))
				break
			}
		}
	}
	include := map[string]bool{}
	exclude := map[string]bool{}
	var unknown []string
	selectIDs := func(sel []SelectControlByID, into map[string]bool) {
		for _, s := range sel {
			if s.Matching != nil && len(*s.Matching) > 0 {
				problems = append(problems, fmt.Sprintf("the %s profile selects controls by pattern, which grc does not resolve", label))
			}
			if s.WithIds == nil {
				continue
			}
			for _, token := range *s.WithIds {
				token = strings.TrimSpace(token)
				if _, ok := tokenToID[token]; !ok {
					unknown = append(unknown, token)
					continue
				}
				into[token] = true
				if s.WithChildControls == "yes" {
					var add func(t string)
					add = func(t string) {
						for _, child := range children[t] {
							into[child] = true
							add(child)
						}
					}
					add(token)
				}
			}
		}
	}
	if len(p.Imports) == 0 {
		problems = append(problems, fmt.Sprintf("the %s profile imports nothing", label))
	}
	for _, imp := range p.Imports {
		if imp.IncludeAll != nil {
			for token := range tokenToID {
				include[token] = true
			}
		}
		if imp.IncludeControls != nil {
			selectIDs(*imp.IncludeControls, include)
		}
		if imp.ExcludeControls != nil {
			selectIDs(*imp.ExcludeControls, exclude)
		}
	}
	if len(unknown) > 0 {
		problems = append(problems, fmt.Sprintf("the %s profile names %d control(s) the catalog does not hold: %s", label, len(unknown), strings.Join(unknown, ", ")))
	}
	ids := make([]string, 0, len(include))
	for token := range include {
		if !exclude[token] {
			ids = append(ids, tokenToID[token])
		}
	}
	controlid.Sort(ids)
	return &info, ids, problems
}

// catalogIndex maps the catalog's OSCAL tokens ("ac-2.1") to grc ids and to
// the tokens nested under them, which a profile's with-child-controls needs.
type catalogIndex struct {
	tokenToID map[string]string
	children  map[string][]string
}

// strictModel decodes an OSCAL document and checks it holds the model asked
// for, so a profile posted as the catalog is refused with a clear message.
func strictModel(raw []byte, model string, doc *Document) error {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if err := json.Unmarshal(raw, doc); err != nil {
		return fmt.Errorf("not OSCAL JSON: %w", err)
	}
	switch model {
	case "catalog":
		if doc.Catalog == nil {
			return fmt.Errorf("not an OSCAL catalog: the document has no %q root", model)
		}
	case "profile":
		if doc.Profile == nil {
			return fmt.Errorf("not an OSCAL profile: the document has no %q root", model)
		}
	}
	return nil
}

func sourceInfo(m Metadata, uuid string, raw []byte) SourceInfo {
	sum := sha256.Sum256(raw)
	info := SourceInfo{
		Title:        strings.TrimSpace(m.Title),
		Version:      strings.TrimSpace(m.Version),
		OSCALVersion: strings.TrimSpace(m.OscalVersion),
		UUID:         strings.TrimSpace(uuid),
		SHA256:       hex.EncodeToString(sum[:]),
		Bytes:        len(raw),
	}
	if !m.LastModified.IsZero() {
		info.LastModified = m.LastModified.Format(time.RFC3339Nano)
	}
	return info
}

// controlIDOf reads the control's display id: the label prop with no class
// ("AC-2(1)"), which is the form grc's catalog uses, falling back to the OSCAL
// token ("ac-2.1") when a catalog carries no label.
func controlIDOf(c Control) string {
	if c.Props != nil {
		for _, p := range *c.Props {
			if p.Name == "label" && p.Class == "" && p.Ns == "" {
				return controlid.Normalize(p.Value)
			}
		}
	}
	return controlid.Normalize(c.ID)
}

func hasProp(props *[]Property, name, value string) bool {
	if props == nil {
		return false
	}
	for _, p := range *props {
		if p.Name == name && p.Ns == "" && strings.EqualFold(strings.TrimSpace(p.Value), value) {
			return true
		}
	}
	return false
}

func isBaseline(slot string) bool {
	for _, b := range Baselines {
		if b == slot {
			return true
		}
	}
	return false
}

// compactJSON is how the import writes upstream structure for storage: compact,
// in struct field order, with <, > and & left as they are, so the same input
// always gives the same string.
func compactJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "null"
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
