package app

import (
	"grc/internal/controlcatalog"
	"grc/internal/oscal"
)

// readOSCALCatalog is the control catalog's OSCALReader: internal/oscal reads
// the files, and this turns its neutral result into the catalog's input type,
// so controlcatalog never imports an OSCAL library.
func readOSCALCatalog(catalog []byte, profiles map[string][]byte) (controlcatalog.OSCALCatalog, error) {
	imp, err := oscal.ReadCatalogImport(catalog, profiles)
	if err != nil {
		return controlcatalog.OSCALCatalog{}, err
	}
	out := controlcatalog.OSCALCatalog{
		Source:    oscalSource(imp.Source),
		Profiles:  make(map[string]controlcatalog.OSCALSource, len(imp.Profiles)),
		Controls:  make([]controlcatalog.OSCALControl, 0, len(imp.Controls)),
		Baselines: imp.Baselines,
		Problems:  imp.Problems,
	}
	for slot, p := range imp.Profiles {
		out.Profiles[slot] = oscalSource(p)
	}
	for _, c := range imp.Controls {
		out.Controls = append(out.Controls, controlcatalog.OSCALControl{
			OSCALID:      c.OSCALID,
			ID:           c.ID,
			Type:         c.Type,
			Name:         c.Name,
			Requirements: c.Requirements,
			Discussion:   c.Discussion,
			Related:      c.Related,
			Withdrawn:    c.Withdrawn,
			Params:       c.ParamsJSON,
			Statement:    c.StatementJSON,
			Assessment:   c.AssessmentJSON,
		})
	}
	return out, nil
}

func oscalSource(s oscal.SourceInfo) controlcatalog.OSCALSource {
	return controlcatalog.OSCALSource{
		Title:        s.Title,
		Version:      s.Version,
		OSCALVersion: s.OSCALVersion,
		LastModified: s.LastModified,
		UUID:         s.UUID,
		SHA256:       s.SHA256,
		Bytes:        s.Bytes,
	}
}
