// Package oscaltest checks OSCAL JSON against the official OSCAL JSON schema,
// for tests. Only test files import it: the schema validator and the schemas
// it embeds never reach the grc binary, which links go-oscal's types package
// and nothing else of it (TestBinaryLinksOnlyTheOSCALTypes holds that).
package oscaltest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/defenseunicorns/go-oscal/src/pkg/validation"

	"grc/internal/oscal"
)

// Validate checks raw against the OSCAL JSON schema of oscal.Version, whatever
// oscal-version the document declares: grc reads and writes with that one
// model. It returns nil for a valid document and an error listing the first
// schema violations otherwise.
func Validate(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	v, err := validation.NewValidatorDesiredVersion(doc, oscal.Version)
	if err != nil {
		return fmt.Errorf("validator: %w", err)
	}
	if err := v.Validate(); err != nil {
		result, resultErr := v.GetValidationResult()
		if resultErr != nil || len(result.Errors) == 0 {
			return fmt.Errorf("invalid OSCAL %s: %w", oscal.Version, err)
		}
		var lines []string
		for i, e := range result.Errors {
			if i == 10 {
				lines = append(lines, fmt.Sprintf("and %d more", len(result.Errors)-i))
				break
			}
			lines = append(lines, e.InstanceLocation+": "+e.Error)
		}
		return fmt.Errorf("invalid OSCAL %s:\n%s", oscal.Version, strings.Join(lines, "\n"))
	}
	return nil
}
