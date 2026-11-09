package oscal

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"grc/internal/controlid"
)

// ExportControl is one control of grc's SP 800-53 catalog, as the catalog
// export reads it. The three JSON fields are what an OSCAL import stored
// ("null" or empty when the control was never imported from OSCAL).
type ExportControl struct {
	ID             string
	Name           string
	Family         string
	Requirements   string
	Discussion     string
	Related        []string
	Withdrawn      bool
	ParamsJSON     string
	StatementJSON  string
	AssessmentJSON string

	// grc's own analysis of the control, which no OSCAL import writes: the CIA
	// flags, the threats, the potentially common/inheritable marker, the CIA
	// justification and the appendix (References included).
	Confidentiality              bool
	Integrity                    bool
	Availability                 bool
	Threats                      []string
	PotentiallyCommonInheritable string
	Justification                string
	Appendix                     map[string]any
}

// CatalogExport is grc's SP 800-53 catalog as an export writes it.
type CatalogExport struct {
	Title        string
	LastModified time.Time
	// Families names each family code ("AC" → "Access Control").
	Families map[string]string
	Controls []ExportControl
}

// ExportCatalog writes grc's SP 800-53 catalog as an OSCAL catalog.
//
// The mapping is the catalog import's, run backwards, so the export imports
// back into the same catalog without a change:
//   - a group per family, with an enhancement nested in its parent;
//   - the id is NIST's token ("ac-2.1"), the label prop grc's id ("AC-2(1)");
//   - a withdrawn control carries status "withdrawn";
//   - the stored params, statement and assessment parts are written as stored;
//   - the statement is NIST's structure while it still renders to the stored
//     requirements, and otherwise grc's text in a statement marked with the grc
//     prose prop, which the import takes verbatim;
//   - the guidance is the stored discussion, marked the same way: grc does not
//     store NIST's guidance part, only its text;
//   - grc's own analysis goes into the grc namespace: the CIA flags, each
//     threat and the potentially common/inheritable marker as props, the CIA
//     justification and the appendix as parts. The import ignores them.
func ExportCatalog(in CatalogExport) ([]byte, error) {
	controls := append([]ExportControl(nil), in.Controls...)
	for i := range controls {
		controls[i].ID = controlid.Normalize(controls[i].ID)
	}
	sortExportControls(controls)

	params := map[string]Parameter{}
	own := make([][]Parameter, len(controls))
	present := map[string]bool{}
	for i, c := range controls {
		if c.ID == "" {
			return nil, fmt.Errorf("a control has no id")
		}
		if present[c.ID] {
			return nil, fmt.Errorf("%s appears twice", c.ID)
		}
		present[c.ID] = true
		if raw := storedJSON(c.ParamsJSON); raw != "" {
			if err := json.Unmarshal([]byte(raw), &own[i]); err != nil {
				return nil, fmt.Errorf("%s: the stored parameters cannot be read: %w", c.ID, err)
			}
			for _, p := range own[i] {
				if _, seen := params[p.ID]; !seen {
					params[p.ID] = p
				}
			}
		}
	}

	built := make(map[string]*Control, len(controls))
	var order []string
	for i, c := range controls {
		oc, err := exportControl(c, own[i], params)
		if err != nil {
			return nil, err
		}
		built[c.ID] = oc
		order = append(order, c.ID)
	}

	// Enhancements go into their parent, last first so a nested slice is
	// complete before its parent is copied into the group.
	topLevel := map[string]bool{}
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		parent := parentID(id)
		if parent == "" || built[parent] == nil {
			topLevel[id] = true
			continue
		}
		p := built[parent]
		children := []Control{*built[id]}
		if p.Controls != nil {
			children = append(children, *p.Controls...)
		}
		p.Controls = &children
	}

	var groups []Group
	byFamily := map[string]int{}
	for _, id := range order {
		if !topLevel[id] {
			continue
		}
		family := controlid.Family(id)
		gi, ok := byFamily[family]
		if !ok {
			title := strings.TrimSpace(in.Families[family])
			if title == "" {
				title = family
			}
			groups = append(groups, Group{ID: strings.ToLower(family), Class: "family", Title: title})
			gi = len(groups) - 1
			byFamily[family] = gi
		}
		var list []Control
		if groups[gi].Controls != nil {
			list = *groups[gi].Controls
		}
		list = append(list, *built[id])
		groups[gi].Controls = &list
	}

	doc := struct {
		Catalog *Catalog `json:"catalog"`
	}{Catalog: &Catalog{
		Metadata: NewMetadata(MetadataInput{
			Title: in.Title,
			Remarks: "Exported from grc, whose database is the system of record for this catalog. " +
				"A statement or guidance part with the grc prop \"prose\" = \"grc-text\" holds grc's stored text verbatim " +
				"(plain text, not OSCAL markup); any other statement is NIST's structure as an OSCAL import stored it. " +
				"Props and parts in the grc namespace are grc's own analysis of each control (CIA flags, threats, " +
				"potentially common/inheritable, the CIA justification and the appendix, References included), as plain text.",
		}),
	}}
	if len(groups) > 0 {
		doc.Catalog.Groups = &groups
	}
	return finishDocument("catalog", "sp800-53", &doc, &doc.Catalog.UUID, &doc.Catalog.Metadata, in.LastModified)
}

func exportControl(c ExportControl, own []Parameter, global map[string]Parameter) (*Control, error) {
	token := ControlToken(c.ID)
	oc := &Control{ID: token, Title: strings.TrimSpace(c.Name), Class: "SP800-53"}
	if parentID(c.ID) != "" {
		oc.Class = "SP800-53-enhancement"
	}
	props := []Property{{Name: "label", Value: c.ID}}
	if sortID := sortIDOf(c.ID); sortID != "" {
		props = append(props, Property{Name: "sort-id", Value: sortID})
	}
	if c.Withdrawn {
		props = append(props, Property{Name: "status", Value: "withdrawn"})
	}
	props = append(props, GRCProps(
		NamedValue{Name: "confidentiality", Value: yesNo(c.Confidentiality)},
		NamedValue{Name: "integrity", Value: yesNo(c.Integrity)},
		NamedValue{Name: "availability", Value: yesNo(c.Availability)},
	)...)
	for _, t := range c.Threats {
		props = append(props, grcProp("threat", t)...)
	}
	props = append(props, grcProp("potentially-common-inheritable", c.PotentiallyCommonInheritable)...)
	oc.Props = &props
	if len(own) > 0 {
		oc.Params = &own
	}

	var parts []Part
	if st, err := exportStatement(c, token, own, global); err != nil {
		return nil, err
	} else if st != nil {
		parts = append(parts, *st)
	}
	if d := strings.TrimSpace(c.Discussion); d != "" {
		parts = append(parts, Part{ID: token + "_gdn", Name: "guidance", Prose: d, Props: &[]Property{proseProp()}})
	}
	if raw := storedJSON(c.AssessmentJSON); raw != "" {
		var assessment []Part
		if err := json.Unmarshal([]byte(raw), &assessment); err != nil {
			return nil, fmt.Errorf("%s: the stored assessment parts cannot be read: %w", c.ID, err)
		}
		parts = append(parts, assessment...)
	}
	if j := strings.TrimSpace(c.Justification); j != "" {
		parts = append(parts, Part{ID: token + "_cia", Name: "cia-justification", Ns: Namespace, Prose: j})
	}
	if appendix, err := appendixPart(c, token); err != nil {
		return nil, err
	} else if appendix != nil {
		parts = append(parts, *appendix)
	}
	oc.Parts = partsOrNil(parts)

	var links []Link
	for _, r := range c.Related {
		if id := controlid.Normalize(r); id != "" {
			links = append(links, Link{Href: "#" + ControlToken(id), Rel: "related"})
		}
	}
	if len(links) > 0 {
		oc.Links = &links
	}
	return oc, nil
}

// appendixPart writes the appendix as a grc part with a sub-part per field:
// the field's name as the title (it carries spaces and capitals no token can),
// a token of it as the part name, and its text as the prose. Fields are in name
// order, since the stored appendix is an object; an empty field is left out.
func appendixPart(c ExportControl, token string) (*Part, error) {
	keys := make([]string, 0, len(c.Appendix))
	for k := range c.Appendix {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var fields []Part
	for _, k := range keys {
		var text string
		switch v := c.Appendix[k].(type) {
		case nil:
			continue
		case string:
			text = v
		default:
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("%s: appendix field %q cannot be written: %w", c.ID, k, err)
			}
			text = string(raw)
		}
		if text = strings.TrimSpace(text); text == "" {
			continue
		}
		fields = append(fields, Part{Name: Token("field", k), Ns: Namespace, Title: strings.TrimSpace(k), Prose: text})
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return &Part{ID: token + "_apx", Name: "appendix", Ns: Namespace, Parts: &fields}, nil
}

// exportStatement writes NIST's statement structure when it still renders to
// the stored requirements, and grc's text otherwise.
func exportStatement(c ExportControl, token string, own []Parameter, global map[string]Parameter) (*Part, error) {
	requirements := strings.TrimSpace(c.Requirements)
	raw := storedJSON(c.StatementJSON)
	if raw != "" {
		var st Part
		if err := json.Unmarshal([]byte(raw), &st); err != nil {
			return nil, fmt.Errorf("%s: the stored statement cannot be read: %w", c.ID, err)
		}
		local := make(map[string]Parameter, len(global)+len(own))
		for k, v := range global {
			local[k] = v
		}
		for _, p := range own {
			local[p.ID] = p
		}
		r := newTextRenderer(local)
		if strings.TrimSpace(r.statement(&st)) == requirements && len(r.missing) == 0 {
			return &st, nil
		}
	}
	if requirements == "" && raw == "" {
		return nil, nil
	}
	return &Part{ID: token + "_smt", Name: "statement", Prose: requirements, Props: &[]Property{proseProp()}}, nil
}

// storedJSON is a stored structure column, or "" when it holds nothing.
func storedJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "null" {
		return ""
	}
	return raw
}

var controlIDShape = regexp.MustCompile(`^([A-Z]{2})-(\d+)(?:\((\d+)\))?$`)

// parentID is the control an enhancement belongs to ("AC-2" for "AC-2(1)"),
// or "" for a base control.
func parentID(id string) string {
	if m := controlIDShape.FindStringSubmatch(id); m != nil && m[3] != "" {
		return m[1] + "-" + m[2]
	}
	return ""
}

// sortIDOf is NIST's sort-id: "ac-02.01" for AC-2(1).
func sortIDOf(id string) string {
	m := controlIDShape.FindStringSubmatch(id)
	if m == nil {
		return ""
	}
	pad := func(s string) string {
		n, _ := strconv.Atoi(s)
		return fmt.Sprintf("%02d", n)
	}
	out := strings.ToLower(m[1]) + "-" + pad(m[2])
	if m[3] != "" {
		out += "." + pad(m[3])
	}
	return out
}

func sortExportControls(cs []ExportControl) {
	ids := make([]string, len(cs))
	byID := make(map[string][]ExportControl, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
		byID[c.ID] = append(byID[c.ID], c)
	}
	controlid.Sort(ids)
	out := cs[:0:0]
	done := map[string]bool{}
	for _, id := range ids {
		if done[id] {
			continue
		}
		done[id] = true
		out = append(out, byID[id]...)
	}
	copy(cs, out)
}

// ErrEmptyBaseline is returned for a profile of a baseline no control is in:
// an OSCAL import with no selection would read as nothing, or, to some tools,
// as everything.
var ErrEmptyBaseline = errors.New("no control is in this baseline")

// ProfileExport is one of grc's baselines as an export writes it.
type ProfileExport struct {
	Baseline     string
	Title        string
	LastModified time.Time
	// CatalogHref is where the profile's catalog is served, and CatalogTitle
	// its title, for the back-matter resource the import points at.
	CatalogHref  string
	CatalogTitle string
	// TierNote says which asset tiers grc applies the baseline to. It goes into
	// the remarks: a tier is not a profile of its own.
	TierNote   string
	ControlIDs []string
}

// ExportProfile writes one baseline as an OSCAL profile of grc's catalog, in
// the shape of NIST's own baseline profiles: an import of the catalog through
// a back-matter resource, including each control in the baseline by id, with
// the controls merged as they are.
func ExportProfile(in ProfileExport) ([]byte, error) {
	if !isBaseline(in.Baseline) {
		return nil, fmt.Errorf("%q is not a baseline grc knows", in.Baseline)
	}
	ids := make([]string, 0, len(in.ControlIDs))
	seen := map[string]bool{}
	for _, id := range in.ControlIDs {
		if id = controlid.Normalize(id); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, ErrEmptyBaseline
	}
	controlid.Sort(ids)
	tokens := make([]string, len(ids))
	for i, id := range ids {
		tokens[i] = ControlToken(id)
	}
	remarks := "Exported from grc: the controls grc's catalog marks as in the " + strings.ToUpper(in.Baseline) + " baseline."
	if note := strings.TrimSpace(in.TierNote); note != "" {
		remarks += " " + note
	}
	catalog, ref := documentResource(in.CatalogTitle, in.CatalogHref)
	doc := struct {
		Profile *Profile `json:"profile"`
	}{Profile: &Profile{
		Metadata: NewMetadata(MetadataInput{Title: in.Title, Remarks: remarks}),
		Imports: []Import{{
			Href:            ref,
			IncludeControls: &[]SelectControlByID{{WithIds: &tokens}},
		}},
		Merge:      &Merge{AsIs: true},
		BackMatter: backMatter(catalog),
	}}
	return finishDocument("profile", "sp800-53-"+in.Baseline, &doc, &doc.Profile.UUID, &doc.Profile.Metadata, in.LastModified)
}
