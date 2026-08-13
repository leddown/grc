// Package nist loads the NIST SP 800-53 Rev 5 control catalog. The catalog is
// the shared mapping target for every source framework.
//
// By default it reads grc's embedded control dataset, so the crosswalk maps
// against exactly the same controls the rest of the application serves. A YAML
// file can be supplied instead when a run needs a narrower or custom catalog.
package nist

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	seeddata "grc/internal/data"
	"grc/internal/regmap/requirement"
)

// Control is a single 800-53 control.
type Control struct {
	ID          string `yaml:"id"`
	Family      string `yaml:"family"`
	Title       string `yaml:"title"`
	ControlText string `yaml:"controlText"`
	// Enhancement marks a control enhancement (e.g. AC-2(1)) rather than a
	// base control. Enhancements are excluded from the default catalog.
	Enhancement bool `yaml:"enhancement,omitempty"`
}

// Catalog is an indexed set of controls.
type Catalog struct {
	Controls []Control `yaml:"controls"`
	// Source describes where the catalog came from, for report provenance.
	Source string `yaml:"-"`

	byID map[string]Control
}

// seedEntry mirrors the shape of grc's embedded control dataset. Only the
// fields the crosswalk needs are decoded.
type seedEntry struct {
	NIST struct {
		ControlID        string `json:"control_id"`
		Type             string `json:"type"`
		Name             string `json:"name"`
		NISTRequirements string `json:"nist_requirements"`
	} `json:"nist"`
}

// LoadEmbedded builds the catalog from grc's embedded NIST dataset.
//
// Control enhancements (AC-2(1) and friends) are excluded unless requested:
// a regulation crosswalk maps to base controls, and the full enhancement set
// would triple the control-ID list injected into every LLM suggestion prompt
// without improving the mapping.
func LoadEmbedded(includeEnhancements bool) (*Catalog, error) {
	var entries map[string]seedEntry
	if err := json.Unmarshal(seeddata.ControlCatalogJSON(), &entries); err != nil {
		return nil, fmt.Errorf("parse embedded NIST control catalog: %w", err)
	}

	c := &Catalog{Source: "grc embedded NIST 800-53 dataset (base controls)"}
	if includeEnhancements {
		c.Source = "grc embedded NIST 800-53 dataset (base controls and enhancements)"
	}
	for _, e := range entries {
		id := strings.TrimSpace(e.NIST.ControlID)
		if id == "" {
			continue
		}
		enhancement := strings.Contains(id, "(") || e.NIST.Type == "Control Enhancement"
		if enhancement && !includeEnhancements {
			continue
		}
		c.Controls = append(c.Controls, Control{
			ID:          id,
			Family:      family(id),
			Title:       strings.TrimSpace(e.NIST.Name),
			ControlText: strings.TrimSpace(e.NIST.NISTRequirements),
			Enhancement: enhancement,
		})
	}
	if len(c.Controls) == 0 {
		return nil, fmt.Errorf("embedded NIST control catalog yielded no controls")
	}
	c.index()
	return c, nil
}

// family derives the control family from a control ID ("AC-2(1)" -> "AC").
func family(id string) string {
	if i := strings.IndexByte(id, '-'); i > 0 {
		return strings.ToUpper(id[:i])
	}
	return strings.ToUpper(id)
}

// Load reads a catalog from a YAML file, overriding the embedded dataset.
func Load(path string) (*Catalog, error) {
	// #nosec G304 -- an explicit --catalog override supplied by the operator.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read control catalog %s: %w", path, err)
	}
	var c Catalog
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse control catalog %s: %w", path, err)
	}
	if len(c.Controls) == 0 {
		return nil, fmt.Errorf("control catalog %s contains no controls", path)
	}
	for i := range c.Controls {
		if c.Controls[i].Family == "" {
			c.Controls[i].Family = family(c.Controls[i].ID)
		}
	}
	c.Source = path
	c.index()
	return &c, nil
}

func (c *Catalog) index() {
	c.byID = make(map[string]Control, len(c.Controls))
	for _, ctl := range c.Controls {
		c.byID[normalize(ctl.ID)] = ctl
	}
	// The embedded dataset decodes from a map, so ordering must be imposed
	// here for deterministic output.
	sort.SliceStable(c.Controls, func(i, j int) bool {
		return requirement.NaturalLess(c.Controls[i].ID, c.Controls[j].ID)
	})
}

func normalize(id string) string {
	return strings.ToUpper(strings.TrimSpace(id))
}

// Lookup returns the control with the given ID, case-insensitively.
func (c *Catalog) Lookup(id string) (Control, bool) {
	ctl, ok := c.byID[normalize(id)]
	return ctl, ok
}

// Title returns the control title, or a placeholder when the ID is unknown.
func (c *Catalog) Title(id string) string {
	if ctl, ok := c.Lookup(id); ok {
		return ctl.Title
	}
	return "(unknown control)"
}

// Validate splits ids into those present in the catalog and those that are not.
// Both slices preserve the caller's ordering after normalization.
func (c *Catalog) Validate(ids []string) (known, unknown []string) {
	for _, id := range ids {
		n := normalize(id)
		if n == "" {
			continue
		}
		if _, ok := c.byID[n]; ok {
			known = append(known, n)
		} else {
			unknown = append(unknown, n)
		}
	}
	return known, unknown
}

// IDs returns every control ID in catalog order.
func (c *Catalog) IDs() []string {
	out := make([]string, 0, len(c.Controls))
	for _, ctl := range c.Controls {
		out = append(out, ctl.ID)
	}
	return out
}

// Families returns the distinct control families, sorted.
func (c *Catalog) Families() []string {
	seen := map[string]bool{}
	var out []string
	for _, ctl := range c.Controls {
		if !seen[ctl.Family] {
			seen[ctl.Family] = true
			out = append(out, ctl.Family)
		}
	}
	sort.Strings(out)
	return out
}
