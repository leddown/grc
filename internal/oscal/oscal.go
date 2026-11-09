// Package oscal is grc's OSCAL interchange layer: it reads NIST's OSCAL
// content into neutral types the catalog modules understand, and writes grc's
// catalogs and crosswalks out as OSCAL JSON.
// The database stays the system of record; OSCAL is a serializer around it.
//
// This is the only package that imports go-oscal. Domain modules never do:
// they receive the neutral types below through an adapter in internal/app, the
// way assuranceplan reaches doctemplate through a Renderer.
package oscal

import (
	oscaltypes "github.com/defenseunicorns/go-oscal/src/types/oscal-1-2-1"
)

// Version is the OSCAL version every document this package writes declares,
// and the model it reads with. 1.2.1 is the version compliance-trestle's model
// is built against. go-oscal's 1.2.1, 1.2.2 and 1.2.3 types are generated
// identically (only the package name and this constant differ), and the
// schemas differ only in two component-type enumerations, so NIST's 1.2.3
// catalog parses with these types without losing a field.
const Version = oscaltypes.Version

// Namespace is the props and parts namespace for content that is grc's own
// rather than OSCAL's or NIST's.
const Namespace = "https://carelockconsulting.com/ns/oscal/grc"

// The pinned model types. Everything else in the package names these, so a
// change of OSCAL version is the import line above and nothing more.
type (
	Document  = oscaltypes.OscalCompleteSchema
	Catalog   = oscaltypes.Catalog
	Control   = oscaltypes.Control
	Group     = oscaltypes.Group
	Part      = oscaltypes.Part
	Parameter = oscaltypes.Parameter
	// ParameterSelection is a parameter's choice list.
	ParameterSelection = oscaltypes.ParameterSelection
	Property           = oscaltypes.Property
	Link               = oscaltypes.Link
	Metadata           = oscaltypes.Metadata
	Profile            = oscaltypes.Profile
	BackMatter         = oscaltypes.BackMatter
	Resource           = oscaltypes.Resource
	ResourceLink       = oscaltypes.ResourceLink
	Citation           = oscaltypes.Citation
	// SelectControlByID is a profile's control selector.
	SelectControlByID = oscaltypes.SelectControlById
	Import            = oscaltypes.Import
	Merge             = oscaltypes.Merge

	Mapping                  = oscaltypes.Mapping
	Map                      = oscaltypes.Map
	MappingItem              = oscaltypes.MappingItem
	MappingProvenance        = oscaltypes.MappingProvenance
	MappingResourceReference = oscaltypes.MappingResourceReference
	GapSummary               = oscaltypes.GapSummary
	ConfidenceScore          = oscaltypes.ConfidenceScore
)
