package oscal

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"grc/internal/controlid"
)

// ExportNFR is one Security NFR, as the NFR exports read it.
type ExportNFR struct {
	Key               string
	ID                string
	Summary           string
	IssueType         string
	Description       string
	NISTMapping       string
	AdditionalDetails string
	Implementation    string
	Domain            string
	Weight            int
}

// NFRCatalogExport is grc's Security NFR catalog as an export writes it.
type NFRCatalogExport struct {
	Title        string
	LastModified time.Time
	NFRs         []ExportNFR
}

// ExportNFRCatalog writes the Security NFR catalog as an OSCAL catalog: a group
// per domain (an NFR without one sits at the catalog's top level), a control
// per NFR. Everything but the title and the description is grc's own and goes
// into the grc namespace.
func ExportNFRCatalog(in NFRCatalogExport) ([]byte, error) {
	nfrs, err := sortedNFRs(in.NFRs)
	if err != nil {
		return nil, err
	}
	var loose []Control
	var groups []Group
	byDomain := map[string]int{}
	for _, n := range nfrs {
		c := nfrControl(n)
		domain := strings.TrimSpace(n.Domain)
		if domain == "" {
			loose = append(loose, c)
			continue
		}
		gi, ok := byDomain[domain]
		if !ok {
			groups = append(groups, Group{ID: Token("domain", "domain-"+domain), Class: "domain", Title: domain})
			gi = len(groups) - 1
			byDomain[domain] = gi
		}
		var list []Control
		if groups[gi].Controls != nil {
			list = *groups[gi].Controls
		}
		list = append(list, c)
		groups[gi].Controls = &list
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Title < groups[j].Title })

	doc := struct {
		Catalog *Catalog `json:"catalog"`
	}{Catalog: &Catalog{
		Metadata: NewMetadata(MetadataInput{
			Title:   in.Title,
			Remarks: "Exported from grc, whose database is the system of record for the Security NFR catalog.",
		}),
	}}
	if len(groups) > 0 {
		doc.Catalog.Groups = &groups
	}
	if len(loose) > 0 {
		doc.Catalog.Controls = &loose
	}
	return finishDocument("catalog", "security-nfrs", &doc, &doc.Catalog.UUID, &doc.Catalog.Metadata, in.LastModified)
}

func sortedNFRs(in []ExportNFR) ([]ExportNFR, error) {
	nfrs := append([]ExportNFR(nil), in...)
	sort.SliceStable(nfrs, func(i, j int) bool { return lessKey(nfrs[i].Key, nfrs[j].Key) })
	tokens := map[string]string{}
	for _, n := range nfrs {
		t := NFRToken(n.Key)
		if other, dup := tokens[t]; dup {
			return nil, fmt.Errorf("NFR keys %q and %q give the same OSCAL id %q", other, n.Key, t)
		}
		tokens[t] = n.Key
	}
	return nfrs, nil
}

// lessKey orders record keys numerically when both are numbers, the way the
// NFR catalog file numbers them, and as text otherwise.
func lessKey(a, b string) bool {
	ai, aerr := strconv.Atoi(strings.TrimSpace(a))
	bi, berr := strconv.Atoi(strings.TrimSpace(b))
	switch {
	case aerr == nil && berr == nil:
		return ai < bi
	case aerr == nil:
		return true
	case berr == nil:
		return false
	}
	return a < b
}

func nfrControl(n ExportNFR) Control {
	token := NFRToken(n.Key)
	label := strings.TrimSpace(n.ID)
	if label == "" {
		label = strings.TrimSpace(n.Key)
	}
	props := []Property{{Name: "label", Value: label}}
	props = append(props, GRCProps(
		NamedValue{Name: "record-key", Value: n.Key},
		NamedValue{Name: "issue-type", Value: n.IssueType},
		NamedValue{Name: "domain", Value: n.Domain},
		NamedValue{Name: "weight", Value: strconv.Itoa(n.Weight)},
	)...)
	var parts []Part
	if d := strings.TrimSpace(n.Description); d != "" {
		parts = append(parts, Part{ID: token + "_smt", Name: "statement", Prose: d})
	}
	for _, p := range []struct{ name, suffix, text string }{
		{"implementation", "_imp", n.Implementation},
		{"additional-details", "_add", n.AdditionalDetails},
		{"nist-mapping", "_map", n.NISTMapping},
	} {
		if t := strings.TrimSpace(p.text); t != "" {
			parts = append(parts, Part{ID: token + p.suffix, Name: p.name, Ns: Namespace, Prose: t})
		}
	}
	title := strings.TrimSpace(n.Summary)
	if title == "" {
		title = label
	}
	return Control{ID: token, Class: "security-nfr", Title: title, Props: &props, Parts: partsOrNil(parts)}
}

// ExportNFRLink is one control reference an NFR's NIST mapping makes, as the
// NFR-control links resolved it.
type ExportNFRLink struct {
	NFRKey string
	// WrittenAs is the reference as the NFR's mapping text gives it, normalised
	// ("AC-2(1)"); ControlID is the catalog control it resolved to, "" when it
	// resolved to none.
	WrittenAs string
	ControlID string
	// Override is set when an administrator's override decided the link.
	Override bool
}

// NFRMappingExport is the NFR→SP 800-53 crosswalk as an export writes it.
type NFRMappingExport struct {
	Title        string
	LastModified time.Time
	NFRs         []ExportNFR
	Links        []ExportNFRLink
	// ControlIDs are the catalog's controls, for the target gap summary.
	ControlIDs []string
	// Where the NFR catalog and the SP 800-53 catalog exports are served, and
	// their titles, for the back-matter resources the mapping points at.
	SourceHref  string
	SourceTitle string
	TargetHref  string
	TargetTitle string
}

// Relationship is the map relationship grc writes, in its own namespace: grc
// records that a target addresses a source, and does not assess the set
// relationship (equivalent, subset, superset, intersecting) NIST IR 8477 asks
// for. Claiming one would be inventing it.
const Relationship = "related"

// mappingCollectionDoc is an OSCAL mapping collection with its mappings as an
// array. The 1.2.1 schema allows one mapping or an array of them; go-oscal's
// type holds only one, and the Regulation Coverage needs two.
type mappingCollectionDoc struct {
	MappingCollection *mappingCollection `json:"mapping-collection"`
}

type mappingCollection struct {
	UUID       string            `json:"uuid"`
	Metadata   Metadata          `json:"metadata"`
	Provenance MappingProvenance `json:"provenance"`
	Mappings   []Mapping         `json:"mappings"`
	BackMatter *BackMatter       `json:"back-matter,omitempty"`
}

// ErrNothingMapped is returned for a mapping collection with no map in it,
// which OSCAL does not allow.
var ErrNothingMapped = fmt.Errorf("nothing is mapped")

// ExportNFRMapping writes the NFR→SP 800-53 links as an OSCAL mapping
// collection: one map per NFR and control it resolved to.
//
// The provenance is what grc knows: an NFR's author wrote the mapping (method
// "human"), matching the NFR's meaning to the control's ("semantic"). It is
// "complete" when every NFR resolved at least one control and every reference
// resolved, and "not-complete" otherwise.
func ExportNFRMapping(in NFRMappingExport) ([]byte, error) {
	nfrs, err := sortedNFRs(in.NFRs)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{}
	for _, n := range nfrs {
		labels[n.Key] = strings.TrimSpace(n.ID)
		if labels[n.Key] == "" {
			labels[n.Key] = n.Key
		}
	}
	links := append([]ExportNFRLink(nil), in.Links...)
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].NFRKey != links[j].NFRKey {
			return lessKey(links[i].NFRKey, links[j].NFRKey)
		}
		if links[i].ControlID != links[j].ControlID {
			return controlid.Less(links[i].ControlID, links[j].ControlID)
		}
		return controlid.Less(links[i].WrittenAs, links[j].WrittenAs)
	})

	var maps []Map
	mapped := map[string]bool{}
	targeted := map[string]bool{}
	seen := map[string]bool{}
	var unresolved []string
	for _, l := range links {
		if _, known := labels[l.NFRKey]; !known {
			continue
		}
		control := controlid.Normalize(l.ControlID)
		if control == "" {
			if w := strings.TrimSpace(l.WrittenAs); w != "" {
				unresolved = append(unresolved, fmt.Sprintf("NFR %s names %s", labels[l.NFRKey], w))
			}
			continue
		}
		pair := l.NFRKey + "\x00" + control
		if seen[pair] {
			continue
		}
		seen[pair] = true
		mapped[l.NFRKey] = true
		targeted[control] = true
		var props []Property
		if l.Override {
			props = append(props, grcProp("override", "yes")...)
		}
		if written := controlid.Normalize(l.WrittenAs); written != "" && written != control {
			props = append(props, grcProp("written-as", written)...)
		}
		maps = append(maps, Map{
			UUID:         UUID("nfr-map", pair),
			Relationship: Relationship,
			Ns:           Namespace,
			Sources:      []MappingItem{{Type: "control", IDRef: NFRToken(l.NFRKey)}},
			Targets:      []MappingItem{{Type: "control", IDRef: ControlToken(control)}},
			Props:        propsOrNil(props),
		})
	}
	if len(maps) == 0 {
		return nil, ErrNothingMapped
	}

	var unmappedNFRs []string
	for _, n := range nfrs {
		if !mapped[n.Key] {
			unmappedNFRs = append(unmappedNFRs, NFRToken(n.Key))
		}
	}
	var untargeted []string
	controlIDs := append([]string(nil), in.ControlIDs...)
	controlid.Sort(controlIDs)
	for _, id := range controlIDs {
		if id = controlid.Normalize(id); id != "" && !targeted[id] {
			untargeted = append(untargeted, ControlToken(id))
		}
	}

	status := "complete"
	if len(unmappedNFRs) > 0 || len(unresolved) > 0 {
		status = "not-complete"
	}
	description := "Which SP 800-53 controls each Security NFR maps to, as the NFR's NIST mapping states it and the NFR-control links resolved it in grc."
	source, sourceRef := documentResource(in.SourceTitle, in.SourceHref)
	target, targetRef := documentResource(in.TargetTitle, in.TargetHref)
	mapping := Mapping{
		UUID:               UUID("mapping", "security-nfrs/sp800-53"),
		Method:             "human",
		MatchingRationale:  "semantic",
		Status:             status,
		MappingDescription: description,
		SourceResource:     MappingResourceReference{Type: "catalog", Href: sourceRef},
		TargetResource:     MappingResourceReference{Type: "catalog", Href: targetRef},
		Maps:               maps,
		SourceGapSummary:   gapSummary("nfr-gap", "security-nfrs/sp800-53/source", unmappedNFRs),
		TargetGapSummary:   gapSummary("nfr-gap", "security-nfrs/sp800-53/target", untargeted),
	}
	if len(unresolved) > 0 {
		mapping.Remarks = "References that resolved to no control in the catalog, left out of the maps: " + strings.Join(unresolved, "; ") + "."
	}
	doc := mappingCollectionDoc{MappingCollection: &mappingCollection{
		Metadata: NewMetadata(MetadataInput{
			Title: in.Title,
			Remarks: "Exported from grc. The relationship of every map is grc's own token \"" + Relationship + "\" (ns " + Namespace + "): " +
				"the NFR addresses the control. grc does not assess the NIST IR 8477 set relationship.",
		}),
		Provenance: MappingProvenance{
			Method:             "human",
			MatchingRationale:  "semantic",
			Status:             status,
			MappingDescription: description,
		},
		Mappings:   []Mapping{mapping},
		BackMatter: backMatter(source, target),
	}}
	return finishDocument("mapping-collection", "security-nfrs/sp800-53", &doc, &doc.MappingCollection.UUID, &doc.MappingCollection.Metadata, in.LastModified)
}

func gapSummary(kind, key string, tokens []string) *GapSummary {
	if len(tokens) == 0 {
		return nil
	}
	return &GapSummary{UUID: UUID(kind, key), UnmappedControls: []SelectControlByID{{WithIds: &tokens}}}
}
