package controlcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"grc/internal/controlid"
)

// OSCALSource identifies one posted OSCAL file.
type OSCALSource struct {
	Title        string `json:"title"`
	Version      string `json:"version"`
	OSCALVersion string `json:"oscal_version"`
	LastModified string `json:"last_modified"`
	UUID         string `json:"uuid"`
	SHA256       string `json:"sha256"`
	Bytes        int    `json:"bytes"`
}

// OSCALControl is one control as an OSCAL catalog states it, already in the
// catalog's own shape. Params, Statement and Assessment are compact OSCAL JSON,
// "null" when the control has none.
type OSCALControl struct {
	OSCALID      string
	ID           string
	Type         string
	Name         string
	Requirements string
	Discussion   string
	Related      []string
	Withdrawn    bool
	Params       string
	Statement    string
	Assessment   string
	// KeepStatement is set when the file's statement is grc's own text
	// (grc's export writes one for a control whose text is not, or no longer,
	// the rendering of NIST's structure). Requirements then come from that text
	// and the stored statement structure is left as it is.
	KeepStatement bool
}

// OSCALCatalog is what an OSCALReader makes of the posted files.
type OSCALCatalog struct {
	Source    OSCALSource
	Profiles  map[string]OSCALSource
	Controls  []OSCALControl
	Baselines map[string][]string
	Problems  []string
}

// OSCALReader reads an OSCAL catalog and the baseline profiles posted with it
// ("low", "moderate", "high", "privacy"). internal/app supplies one built on
// internal/oscal, so this package never imports an OSCAL library.
type OSCALReader func(catalog []byte, profiles map[string][]byte) (OSCALCatalog, error)

// OSCALBaselineSlots are the profile slots, in the order the catalog lists
// baselines.
var OSCALBaselineSlots = []string{"low", "moderate", "high", "privacy"}

type OSCALFieldChange struct {
	Field string `json:"field"`
	// Before and After are given for the text fields, so the preview can show
	// what changes; the structure fields are named only.
	Before         string `json:"before,omitempty"`
	After          string `json:"after,omitempty"`
	WhitespaceOnly bool   `json:"whitespace_only,omitempty"`
}

type OSCALControlChange struct {
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	Fields []OSCALFieldChange `json:"fields"`
}

type OSCALAddedControl struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// OSCALImportResult is what an OSCAL import would do (a preview) or did.
type OSCALImportResult struct {
	Source   OSCALSource            `json:"source"`
	Profiles map[string]OSCALSource `json:"profiles"`
	Problems []string               `json:"problems"`
	Notes    []string               `json:"notes"`
	Added    []OSCALAddedControl    `json:"added"`
	Changed  []OSCALControlChange   `json:"changed"`
	// FieldCounts counts the changed controls per field, so a preview of a
	// thousand changes can say what kind of change they are.
	FieldCounts map[string]int `json:"field_counts"`
	Unchanged   []string       `json:"unchanged"`
	// Absent lists controls the catalog holds and the file does not. An OSCAL
	// import never deletes; they are only reported.
	Absent       []string `json:"absent"`
	Withdrawn    int      `json:"withdrawn"`
	Base         string   `json:"base"`
	Applied      bool     `json:"applied"`
	LinksRebuilt bool     `json:"links_rebuilt"`
}

// ErrOSCALConflict is returned by an apply whose preview no longer describes
// the catalog or the files.
var ErrOSCALConflict = errors.New("the catalog or the posted files changed since the preview; preview again")

// ErrOSCALProblems is returned by an apply of files that have problems.
var ErrOSCALProblems = errors.New("the files have problems; preview them to see each one")

// The field names a preview reports.
const (
	fieldType         = "type"
	fieldName         = "name"
	fieldRequirements = "requirements"
	fieldDiscussion   = "discussion"
	fieldRelated      = "related controls"
	fieldParams       = "parameters"
	fieldStatement    = "statement structure"
	fieldAssessment   = "assessment (800-53A)"
	fieldWithdrawn    = "withdrawn"
)

// PreviewOSCAL works out what importing in would do to the catalog, without
// writing anything.
func (s *Service) PreviewOSCAL(in OSCALCatalog) (OSCALImportResult, error) {
	result, _, err := s.planOSCAL(in)
	return result, err
}

// ApplyOSCAL imports in, provided base is the Base of a preview made against
// the same catalog and the same files. It writes only what OSCAL owns (see
// oscalWrite), inserts the controls the catalog lacks, and deletes nothing.
func (s *Service) ApplyOSCAL(in OSCALCatalog, base string) (OSCALImportResult, error) {
	result, plan, err := s.planOSCAL(in)
	if err != nil {
		return result, err
	}
	if len(result.Problems) > 0 {
		return result, ErrOSCALProblems
	}
	if strings.TrimSpace(base) == "" || base != result.Base {
		return result, ErrOSCALConflict
	}
	if len(plan.writes) == 0 {
		result.Applied = true
		return result, nil
	}
	if err := s.repo.applyOSCAL(plan.writes, plan.digest); err != nil {
		if errors.Is(err, errCatalogMoved) {
			return result, ErrOSCALConflict
		}
		return result, fmt.Errorf("apply the OSCAL import: %w", err)
	}
	result.Applied = true
	return result, nil
}

type oscalPlan struct {
	writes []oscalWrite
	digest string
}

func (s *Service) planOSCAL(in OSCALCatalog) (OSCALImportResult, oscalPlan, error) {
	result := OSCALImportResult{
		Source:      in.Source,
		Profiles:    in.Profiles,
		Problems:    append([]string{}, in.Problems...),
		Notes:       []string{},
		Added:       []OSCALAddedControl{},
		Changed:     []OSCALControlChange{},
		FieldCounts: map[string]int{},
		Unchanged:   []string{},
		Absent:      []string{},
	}
	if result.Profiles == nil {
		result.Profiles = map[string]OSCALSource{}
	}
	// The whole catalog, hidden families included: visibility is a display
	// setting, and an import must see every control it might change.
	existing, err := s.repo.List("", "", "")
	if err != nil {
		return result, oscalPlan{}, err
	}
	stored, err := s.repo.oscalColumns()
	if err != nil {
		return result, oscalPlan{}, err
	}
	digest, err := s.repo.catalogDigest()
	if err != nil {
		return result, oscalPlan{}, err
	}
	result.Base = oscalBase(digest, in)

	current := make(map[string]Control, len(existing))
	for _, c := range existing {
		current[c.ID] = c
	}

	posted := map[string]map[string]bool{}
	var postedNames []string
	for _, slot := range OSCALBaselineSlots {
		ids, ok := in.Baselines[slot]
		if !ok {
			continue
		}
		set := make(map[string]bool, len(ids))
		for _, id := range ids {
			set[controlid.Normalize(id)] = true
		}
		posted[slot] = set
		postedNames = append(postedNames, strings.ToUpper(slot))
	}
	switch {
	case len(posted) == 0:
		result.Notes = append(result.Notes, "No baseline profile was posted: the baseline flags of every control are left as they are.")
	case len(posted) < len(OSCALBaselineSlots):
		result.Notes = append(result.Notes, fmt.Sprintf("Baselines are set from the %s profile(s); the others are left as they are.", strings.Join(postedNames, ", ")))
	default:
		result.Notes = append(result.Notes, "Baselines are set from the LOW, MODERATE, HIGH and PRIVACY profiles.")
	}

	plan := oscalPlan{digest: digest}
	seen := map[string]bool{}
	for _, oc := range in.Controls {
		id := controlid.Normalize(oc.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if oc.Withdrawn {
			result.Withdrawn++
		}
		incoming := Control{
			ID:              id,
			Type:            strings.TrimSpace(oc.Type),
			Name:            strings.TrimSpace(oc.Name),
			Family:          controlFamily(id),
			Requirements:    strings.TrimSpace(oc.Requirements),
			Discussion:      strings.TrimSpace(oc.Discussion),
			RelatedControls: uniqueSorted(oc.Related),
			Withdrawn:       oc.Withdrawn,
		}
		cols := oscalColumns{Params: jsonOrNull(oc.Params), Statement: jsonOrNull(oc.Statement), Assessment: jsonOrNull(oc.Assessment)}
		if oc.KeepStatement {
			cols.Statement = jsonOrNull(stored[id].Statement)
		}

		have, exists := current[id]
		if !exists {
			incoming.SourceKey = id + ": " + incoming.Name
			var b *Baselines
			if len(posted) > 0 {
				nb := applyPostedBaselines(Baselines{}, id, posted)
				b = &nb
			}
			plan.writes = append(plan.writes, oscalWrite{Insert: true, Control: incoming, Columns: cols, Baselines: b})
			result.Added = append(result.Added, OSCALAddedControl{ID: id, Type: incoming.Type, Name: incoming.Name})
			continue
		}

		var fields []OSCALFieldChange
		text := func(field, before, after string) {
			if before == after {
				return
			}
			fields = append(fields, OSCALFieldChange{Field: field, Before: before, After: after,
				WhitespaceOnly: strings.Join(strings.Fields(before), " ") == strings.Join(strings.Fields(after), " ")})
		}
		text(fieldType, have.Type, incoming.Type)
		text(fieldName, have.Name, incoming.Name)
		text(fieldRequirements, have.Requirements, incoming.Requirements)
		text(fieldDiscussion, have.Discussion, incoming.Discussion)
		text(fieldRelated, strings.Join(have.RelatedControls, ", "), strings.Join(incoming.RelatedControls, ", "))
		if have.Withdrawn != incoming.Withdrawn {
			fields = append(fields, OSCALFieldChange{Field: fieldWithdrawn, Before: yesNo(have.Withdrawn), After: yesNo(incoming.Withdrawn)})
		}
		was := stored[id]
		for _, f := range []struct {
			field, before, after string
		}{
			{fieldParams, was.Params, cols.Params},
			{fieldStatement, was.Statement, cols.Statement},
			{fieldAssessment, was.Assessment, cols.Assessment},
		} {
			if jsonOrNull(f.before) != f.after {
				fields = append(fields, OSCALFieldChange{Field: f.field})
			}
		}
		var newBaselines *Baselines
		if len(posted) > 0 {
			nb := applyPostedBaselines(have.MappingBaselines, id, posted)
			for _, slot := range OSCALBaselineSlots {
				if posted[slot] == nil {
					continue
				}
				before, after := inBaseline(have.MappingBaselines, slot), inBaseline(nb, slot)
				if before != after {
					fields = append(fields, OSCALFieldChange{Field: "baseline " + strings.ToUpper(slot), Before: yesNo(before), After: yesNo(after)})
				}
			}
			newBaselines = &nb
		}
		if len(fields) == 0 {
			result.Unchanged = append(result.Unchanged, id)
			continue
		}
		for _, f := range fields {
			result.FieldCounts[f.Field]++
		}
		result.Changed = append(result.Changed, OSCALControlChange{ID: id, Name: incoming.Name, Fields: fields})
		plan.writes = append(plan.writes, oscalWrite{Control: incoming, Columns: cols, Baselines: newBaselines})
	}
	for _, c := range existing {
		if !seen[c.ID] {
			result.Absent = append(result.Absent, c.ID)
		}
	}
	if len(result.Absent) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("%d control(s) in the catalog are not in the file. An OSCAL import never deletes: they stay as they are.", len(result.Absent)))
	}
	result.Notes = append(result.Notes, "Never changed by an OSCAL import: the CIA flags and justification, threats, potentially common/inheritable, the appendix (grc's own content, References included) and the record key.")
	sortControlIDs(result.Unchanged)
	sortControlIDs(result.Absent)
	sort.SliceStable(result.Added, func(i, j int) bool { return controlid.Less(result.Added[i].ID, result.Added[j].ID) })
	sort.SliceStable(result.Changed, func(i, j int) bool { return controlid.Less(result.Changed[i].ID, result.Changed[j].ID) })
	return result, plan, nil
}

// oscalBase ties an apply to its preview: the catalog's digest and the posted
// files' hashes. Either changing makes the apply refuse.
func oscalBase(digest string, in OSCALCatalog) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "catalog-db:%s\ncatalog-file:%s\n", digest, in.Source.SHA256)
	for _, slot := range OSCALBaselineSlots {
		if p, ok := in.Profiles[slot]; ok {
			_, _ = fmt.Fprintf(h, "profile-%s:%s\n", slot, p.SHA256)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// applyPostedBaselines sets the posted baselines' membership of id and keeps
// the others as they were.
func applyPostedBaselines(b Baselines, id string, posted map[string]map[string]bool) Baselines {
	set := func(current []string, slot string) []string {
		members, ok := posted[slot]
		if !ok {
			return current
		}
		if members[id] {
			return []string{id}
		}
		return []string{}
	}
	return normalizeBaselines(Baselines{
		Low:      set(b.Low, "low"),
		Moderate: set(b.Moderate, "moderate"),
		High:     set(b.High, "high"),
		Privacy:  set(b.Privacy, "privacy"),
	})
}

func inBaseline(b Baselines, slot string) bool {
	switch slot {
	case "low":
		return len(b.Low) > 0
	case "moderate":
		return len(b.Moderate) > 0
	case "high":
		return len(b.High) > 0
	case "privacy":
		return len(b.Privacy) > 0
	}
	return false
}

func jsonOrNull(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "null"
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func sortControlIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool { return controlid.Less(ids[i], ids[j]) })
}
