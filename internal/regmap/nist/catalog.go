// Package nist loads the NIST SP 800-53 Rev 5 control catalog. The catalog is
// the shared mapping target for every source framework.
//
// The application builds it from the controls in its database (FromControls),
// so the crosswalk maps against exactly the controls the rest of the
// application serves. A YAML file can be supplied instead when a run needs a
// narrower or custom catalog.
package nist

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

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

// FromControls builds the catalog from controls the caller already holds.
// Family is derived from the ID when it is blank, and an ID with a
// parenthesised part counts as an enhancement whatever the caller said.
//
// Control enhancements (AC-2(1) and friends) are excluded unless requested:
// a regulation crosswalk maps to base controls, and the full enhancement set
// would triple the control-ID list injected into every LLM suggestion prompt
// without improving the mapping.
func FromControls(controls []Control, includeEnhancements bool, source string) (*Catalog, error) {
	c := &Catalog{Source: source}
	for _, ctl := range controls {
		ctl.ID = strings.TrimSpace(ctl.ID)
		if ctl.ID == "" {
			continue
		}
		ctl.Enhancement = ctl.Enhancement || strings.Contains(ctl.ID, "(")
		if ctl.Enhancement && !includeEnhancements {
			continue
		}
		if ctl.Family == "" {
			ctl.Family = family(ctl.ID)
		}
		ctl.Title = strings.TrimSpace(ctl.Title)
		ctl.ControlText = strings.TrimSpace(ctl.ControlText)
		c.Controls = append(c.Controls, ctl)
	}
	if len(c.Controls) == 0 {
		return nil, fmt.Errorf("the NIST 800-53 catalog holds no controls")
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

// Load reads a catalog from a YAML file.
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
	// Callers hand controls over in whatever order their source had, so
	// ordering is imposed here for deterministic output.
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
