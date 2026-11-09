package oscal

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ExportRegulation is one version of a Regulation Coverage report, as the
// regulation exports read it. A version is immutable, so its exports are too:
// LastModified is the version's own timestamp.
type ExportRegulation struct {
	ID            int64
	Title         string
	Framework     string
	FrameworkName string
	SourceRef     string
	// TextSHA256 is the hash of the regulation's extracted text, which
	// identifies the instrument across installations.
	TextSHA256   string
	Version      int
	Latest       bool
	VersionNote  string
	LastModified time.Time
	Sections     []ExportSection
}

// ExportSection is one article, annex or clause, with grc's finding on it.
type ExportSection struct {
	Ref               string
	Label             string
	Title             string
	Category          string
	Body              string
	Position          int
	SegmentConfidence string
	Finding           *ExportFinding
}

// ExportFinding is grc's analysis of a section.
type ExportFinding struct {
	Relevant    bool
	Grounded    bool
	Requirement string
	Commentary  string
	Gaps        string
	Quote       string
	Confidence  string
	Model       string
	PromptHash  string
	Revision    int
	Mappings    []ExportRegMapping
}

// ExportRegMapping ties a section to a Security NFR (Kind "nfr", Ref the
// record key) or an SP 800-53 control (Kind "control", Ref the control id).
type ExportRegMapping struct {
	Kind       string
	Ref        string
	Rationale  string
	Confidence string
	// Source is "seed" for a curated crosswalk, "model" for the model's.
	Source string
	// Known reports whether the catalog holds Ref.
	Known bool
}

// RegulationHrefs are where the documents a regulation mapping joins are
// served.
type RegulationHrefs struct {
	Catalog        string
	NFRCatalog     string
	ControlCatalog string
}

func (r ExportRegulation) key() string {
	if sha := strings.TrimSpace(r.TextSHA256); sha != "" {
		return "regulation/" + sha
	}
	return "regulation/" + strconv.FormatInt(r.ID, 10)
}

// sectionTokens gives each section an OSCAL id from its reference, in
// position order, suffixing the position when two references give one id.
func sectionTokens(sections []ExportSection) []string {
	out := make([]string, len(sections))
	used := map[string]bool{}
	for i, s := range sections {
		t := Token("section", s.Ref)
		if used[t] {
			t = t + "-" + strconv.Itoa(s.Position)
		}
		used[t] = true
		out[i] = t
	}
	return out
}

// ExportRegulationCatalog writes a report version as an OSCAL catalog: a
// control per section, its text as the statement, and grc's finding (what the
// section requires, the commentary, the gaps, the grounding quote, and who
// produced it) as a part in the grc namespace.
func ExportRegulationCatalog(r ExportRegulation) ([]byte, error) {
	tokens := sectionTokens(r.Sections)
	controls := make([]Control, 0, len(r.Sections))
	for i, s := range r.Sections {
		controls = append(controls, sectionControl(s, tokens[i]))
	}
	remarks := fmt.Sprintf("Version %d of grc's Regulation Coverage report on this regulation: its sections as grc read them, each with grc's finding.", r.Version)
	if note := strings.TrimSpace(r.VersionNote); note != "" {
		remarks += " Why this version exists: " + note
	}
	doc := struct {
		Catalog *Catalog `json:"catalog"`
	}{Catalog: &Catalog{
		Metadata: NewMetadata(MetadataInput{
			Title:   r.Title,
			Version: strconv.Itoa(r.Version),
			Remarks: remarks,
			Props: []NamedValue{
				{Name: "framework", Value: r.Framework},
				{Name: "framework-name", Value: r.FrameworkName},
				{Name: "source-ref", Value: r.SourceRef},
				{Name: "text-sha256", Value: r.TextSHA256},
			},
		}),
	}}
	if len(controls) > 0 {
		doc.Catalog.Controls = &controls
	}
	return finishDocument("catalog", r.key(), &doc, &doc.Catalog.UUID, &doc.Catalog.Metadata, r.LastModified)
}

func sectionControl(s ExportSection, token string) Control {
	label := strings.TrimSpace(s.Label)
	if label == "" {
		label = strings.TrimSpace(s.Ref)
	}
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = label
	}
	if title == "" {
		title = token
	}
	var props []Property
	if label != "" {
		props = append(props, Property{Name: "label", Value: label})
	}
	props = append(props, Property{Name: "sort-id", Value: fmt.Sprintf("%05d", s.Position)})
	props = append(props, GRCProps(
		NamedValue{Name: "ref", Value: s.Ref},
		NamedValue{Name: "category", Value: s.Category},
		NamedValue{Name: "segment-confidence", Value: s.SegmentConfidence},
	)...)
	var parts []Part
	if body := strings.TrimSpace(s.Body); body != "" {
		parts = append(parts, Part{ID: token + "_smt", Name: "statement", Prose: body})
	}
	if f := s.Finding; f != nil {
		var children []Part
		for _, p := range []struct{ name, suffix, text string }{
			{"requirement", "_req", f.Requirement},
			{"commentary", "_com", f.Commentary},
			{"gaps", "_gap", f.Gaps},
			{"quote", "_quo", f.Quote},
		} {
			if t := strings.TrimSpace(p.text); t != "" {
				children = append(children, Part{ID: token + p.suffix, Name: p.name, Ns: Namespace, Prose: t})
			}
		}
		findingProps := GRCProps(
			NamedValue{Name: "relevant", Value: yesNo(f.Relevant)},
			NamedValue{Name: "grounded", Value: yesNo(f.Grounded)},
			NamedValue{Name: "confidence", Value: f.Confidence},
			NamedValue{Name: "model", Value: f.Model},
			NamedValue{Name: "prompt-hash", Value: f.PromptHash},
			NamedValue{Name: "revision", Value: strconv.Itoa(f.Revision)},
		)
		parts = append(parts, Part{ID: token + "_fnd", Name: "finding", Ns: Namespace, Props: propsOrNil(findingProps), Parts: partsOrNil(children)})
	}
	return Control{ID: token, Title: title, Props: &props, Parts: partsOrNil(parts)}
}

// ExportRegulationMapping writes a report version's mappings as an OSCAL
// mapping collection: one mapping to the Security NFR catalog and one to the
// SP 800-53 catalog (each only when it has a map), a map per section and
// catalog item.
//
// Provenance is what the report records. A map's grc "source" prop says
// whether a curated crosswalk ("seed") or the model the finding names
// ("model") made it, which decides the method: human, automation, or hybrid
// for a mix. A superseded version is "superseded"; the latest is "complete"
// when every section has a finding and "not-complete" otherwise. A reference
// the catalog does not hold is left out of the maps and named in the remarks.
func ExportRegulationMapping(r ExportRegulation, hrefs RegulationHrefs) ([]byte, error) {
	tokens := sectionTokens(r.Sections)
	status := "complete"
	for _, s := range r.Sections {
		if s.Finding == nil {
			status = "not-complete"
			break
		}
	}
	if !r.Latest {
		status = "superseded"
	}

	kinds := []struct {
		kind, name, target string
		token              func(string) string
	}{
		{"nfr", "Security NFRs", hrefs.NFRCatalog, NFRToken},
		{"control", "SP 800-53 controls", hrefs.ControlCatalog, ControlToken},
	}
	var mappings []Mapping
	sources := map[string]bool{}
	for _, k := range kinds {
		var maps []Map
		var unresolved, gaps []string
		used := map[string]bool{}
		for i, s := range r.Sections {
			if s.Finding == nil {
				continue
			}
			mappedHere := false
			for _, m := range s.Finding.Mappings {
				if m.Kind != k.kind {
					continue
				}
				ref := strings.TrimSpace(m.Ref)
				if ref == "" {
					continue
				}
				if !m.Known {
					unresolved = append(unresolved, fmt.Sprintf("%s → %s", firstNonEmpty(s.Label, s.Ref), ref))
					continue
				}
				pair := s.Ref + "\x00" + ref
				if used[pair] {
					mappedHere = true
					continue
				}
				used[pair] = true
				mappedHere = true
				source := strings.TrimSpace(m.Source)
				sources[source] = true
				mp := Map{
					UUID:         UUID("regulation-map", r.key()+"\x00"+k.kind+"\x00"+pair),
					Relationship: Relationship,
					Ns:           Namespace,
					Sources:      []MappingItem{{Type: "control", IDRef: tokens[i]}},
					Targets:      []MappingItem{{Type: "control", IDRef: k.token(ref)}},
					Props:        propsOrNil(grcProp("source", source)),
					Remarks:      strings.TrimSpace(m.Rationale),
				}
				if c := strings.TrimSpace(m.Confidence); c != "" {
					mp.ConfidenceScore = &ConfidenceScore{Category: c}
				}
				maps = append(maps, mp)
			}
			if s.Finding.Relevant && !mappedHere {
				gaps = append(gaps, tokens[i])
			}
		}
		if len(maps) == 0 {
			continue
		}
		mapping := Mapping{
			UUID:               UUID("mapping", r.key()+"/"+k.kind),
			Method:             methodOf(maps),
			MatchingRationale:  "semantic",
			Status:             status,
			MappingDescription: fmt.Sprintf("Which %s address each section of %s, from version %d of grc's Regulation Coverage report.", k.name, strings.TrimSpace(r.Title), r.Version),
			SourceResource:     MappingResourceReference{Type: "catalog", Href: hrefs.Catalog},
			TargetResource:     MappingResourceReference{Type: "catalog", Href: k.target},
			Maps:               maps,
			SourceGapSummary:   gapSummary("regulation-gap", r.key()+"/"+k.kind, gaps),
		}
		if len(unresolved) > 0 {
			mapping.Remarks = "Mappings to references the catalog does not hold, left out of the maps: " + strings.Join(unresolved, "; ") + "."
		}
		mappings = append(mappings, mapping)
	}
	if len(mappings) == 0 {
		return nil, ErrNothingMapped
	}

	method := "hybrid"
	switch {
	case sources["seed"] && !sources["model"]:
		method = "human"
	case sources["model"] && !sources["seed"]:
		method = "automation"
	}
	doc := mappingCollectionDoc{MappingCollection: &mappingCollection{
		Metadata: NewMetadata(MetadataInput{
			Title:   r.Title + ": Regulation Coverage mappings",
			Version: strconv.Itoa(r.Version),
			Remarks: "Exported from grc. The relationship of every map is grc's own token \"" + Relationship + "\" (ns " + Namespace + "): " +
				"the target addresses the section. grc does not assess the NIST IR 8477 set relationship. A map's confidence-score is the report's. " +
				"The targets are grc's live Security NFR and SP 800-53 catalog exports, which may have changed since this version was cut.",
		}),
		Provenance: MappingProvenance{
			Method:             method,
			MatchingRationale:  "semantic",
			Status:             status,
			MappingDescription: fmt.Sprintf("Version %d of grc's Regulation Coverage report on %s: which Security NFRs and SP 800-53 controls address each section.", r.Version, strings.TrimSpace(r.Title)),
		},
		Mappings: mappings,
	}}
	return finishDocument("mapping-collection", r.key(), &doc, &doc.MappingCollection.UUID, &doc.MappingCollection.Metadata, r.LastModified)
}

func methodOf(maps []Map) string {
	var seed, model bool
	for _, m := range maps {
		if m.Props == nil {
			continue
		}
		for _, p := range *m.Props {
			if p.Name == "source" && p.Ns == Namespace {
				seed = seed || p.Value == "seed"
				model = model || p.Value == "model"
			}
		}
	}
	switch {
	case seed && !model:
		return "human"
	case model && !seed:
		return "automation"
	}
	return "hybrid"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
