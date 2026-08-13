// Package cli wires the regmap command tree. The commands here contain no
// framework-specific logic: everything about a source framework comes from its
// profile under regmap/profiles.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"grc/internal/regmap/nist"
	"grc/internal/regmap/profile"
	"grc/internal/regmap/state"
)

var (
	dataDir          string
	profilesDir      string
	catalogPath      string
	withEnhancements bool
	reviewer         string
)

// NewRootCommand builds the CLI.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "regmap",
		Short: "Map a cybersecurity regulation to NIST SP 800-53 Rev 5, with human review at every step",
		Long: `regmap ingests a cybersecurity or resilience regulation, maps its
requirements to NIST SP 800-53 Rev 5 controls, and produces an auditable
crosswalk.

The pipeline pauses at three review gates. Nothing reaches "approved" without
you:

  ingest -> [GATE 1 segmentation] -> map -> [GATE 2 mappings]
         -> suggest -> [GATE 3 suggestions] -> report

The source framework is not hardcoded. Each supported framework is a YAML
profile under ./profiles; adding a regulation means adding a file, not editing
the pipeline.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	def := os.Getenv("USER")
	if def == "" {
		def = "unknown"
	}

	pf := root.PersistentFlags()
	pf.StringVar(&dataDir, "data-dir", "regmap/data", "directory holding state.yaml, requirements.yaml and mappings.yaml")
	pf.StringVar(&profilesDir, "profiles-dir", "regmap/profiles", "directory holding framework profiles")
	pf.StringVar(&catalogPath, "catalog", "", "YAML control catalog to use instead of grc's embedded NIST dataset")
	pf.BoolVar(&withEnhancements, "with-enhancements", false, "include NIST control enhancements (AC-2(1) etc.) as mapping targets")
	pf.StringVar(&reviewer, "reviewer", def, "name recorded as reviewedBy on approved items")

	root.AddCommand(
		newFrameworksCommand(),
		newIngestCommand(),
		newReviewCommand(),
		newMapCommand(),
		newSuggestCommand(),
		newReportCommand(),
		newStatusCommand(),
	)
	return root
}

// env bundles the loaded runtime dependencies shared by most commands.
type env struct {
	Registry *profile.Registry
	Catalog  *nist.Catalog
	Store    *state.Store
	State    *state.State
}

func loadEnv() (*env, error) {
	reg, err := profile.LoadDir(profilesDir)
	if err != nil {
		return nil, err
	}
	// Default to grc's embedded NIST dataset so the crosswalk maps against the
	// same controls the rest of the application serves.
	cat, err := nist.LoadEmbedded(withEnhancements)
	if catalogPath != "" {
		cat, err = nist.Load(catalogPath)
	}
	if err != nil {
		return nil, err
	}
	store := state.New(dataDir)
	st, err := store.LoadState()
	if err != nil {
		return nil, err
	}
	return &env{Registry: reg, Catalog: cat, Store: store, State: st}, nil
}

// activeProfile returns the profile for the framework currently in the pipeline.
func (e *env) activeProfile() (*profile.Profile, error) {
	if e.State.Framework == "" {
		return nil, fmt.Errorf("no framework has been ingested yet — run `regmap ingest --in <file>` first")
	}
	return e.Registry.Get(e.State.Framework)
}
