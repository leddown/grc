package oscal

import (
	"strings"
	"time"
)

// MetadataInput is what an export knows about the document it is writing.
type MetadataInput struct {
	Title string
	// Version is the document's version. Left empty, the export writes the
	// first 12 hex digits of its content digest, so the version changes exactly
	// when the content does.
	Version string
	// LastModified is the source's own timestamp for an immutable source (a
	// report version, an approval snapshot) and the time of the export
	// otherwise. It is the only field allowed to differ between two exports of
	// unchanged data.
	LastModified time.Time
	Remarks      string
	// Props are grc-namespaced name/value pairs, written in the order given.
	Props []NamedValue
}

// NamedValue is one grc prop.
type NamedValue struct {
	Name  string
	Value string
}

// NewMetadata builds the metadata block every grc export carries. It declares
// Version as the document's oscal-version and adds no parties, roles or
// responsible parties: grc does not hold them, and an export does not invent
// them.
func NewMetadata(in MetadataInput) Metadata {
	m := Metadata{
		Title:        strings.TrimSpace(in.Title),
		Version:      strings.TrimSpace(in.Version),
		OscalVersion: Version,
		LastModified: in.LastModified.UTC().Truncate(time.Second),
		Remarks:      strings.TrimSpace(in.Remarks),
	}
	if props := GRCProps(in.Props...); len(props) > 0 {
		m.Props = &props
	}
	return m
}

// GRCProps turns name/value pairs into props in the grc namespace, skipping
// empty values: a prop with no value is not a statement of anything.
func GRCProps(values ...NamedValue) []Property {
	out := make([]Property, 0, len(values))
	for _, v := range values {
		value := strings.TrimSpace(v.Value)
		if value == "" {
			continue
		}
		out = append(out, Property{Name: v.Name, Ns: Namespace, Value: value})
	}
	return out
}
