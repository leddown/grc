package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"grc/internal/regmap/ingest"
	"grc/internal/regmap/profile"
	"grc/internal/regmap/state"
)

func newFrameworksCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "frameworks",
		Short: "List the framework profiles available on disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := profile.LoadDir(profilesDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-10s %-42s %-22s %s\n", "ID", "NAME", "SOURCE REF", "SEGMENTATION")
			for _, p := range reg.All() {
				var strategies []string
				for _, s := range p.Segmentation {
					strategies = append(strategies, s.Type)
				}
				fmt.Fprintf(out, "%-10s %-42s %-22s %s\n",
					p.ID, truncate(p.DisplayName, 42), truncate(p.SourceRef, 22), strings.Join(strategies, "+"))
			}
			fmt.Fprintf(out, "\n%d profile(s) in %s. Add a framework by adding a YAML file there.\n", len(reg.All()), profilesDir)
			return nil
		},
	}
}

func newIngestCommand() *cobra.Command {
	var in, framework string
	var force bool

	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Extract and segment a source document into data/requirements.yaml",
		Long: `Extract text from a PDF, DOCX or TXT document and segment it into requirements
using the selected framework profile. Everything lands at status=draft; GATE 1
is where segmentation errors get fixed.

The framework is auto-detected from the filename and content when possible, but
you can always override it with --framework and you confirm it at GATE 1.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if in == "" {
				return fmt.Errorf("--in is required")
			}
			e, err := loadEnv()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			existing, err := e.Store.LoadRequirements()
			if err != nil {
				return err
			}
			if len(existing) > 0 && !force {
				approved := len(existing.Approved())
				return fmt.Errorf("%s already holds %d requirement(s) (%d approved); re-ingesting would discard that review work — pass --force if that is what you want",
					e.Store.RequirementsPath(), len(existing), approved)
			}

			fmt.Fprintf(out, "Extracting %s ...\n", in)
			ex, err := ingest.Extract(in)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "  format: %s via %s (%d characters)\n", ex.Format, ex.Method, len(ex.Text))
			for _, n := range ex.Notes {
				fmt.Fprintf(out, "  note: %s\n", n)
			}

			var prof *profile.Profile
			note := ""
			if framework != "" {
				prof, err = e.Registry.Get(framework)
				if err != nil {
					return err
				}
				note = "--framework flag"
			} else {
				guesses := e.Registry.Detect(in, ex.Text)
				if len(guesses) == 0 {
					return fmt.Errorf("could not detect the framework from %s; re-run with --framework <id> (see `regmap frameworks`)", in)
				}
				prof = guesses[0].Profile
				note = fmt.Sprintf("auto-detected (score %d: %s)", guesses[0].Score, strings.Join(guesses[0].Reasons, "; "))
				if len(guesses) > 1 && guesses[1].Score == guesses[0].Score {
					return fmt.Errorf("framework detection was ambiguous between %s and %s; re-run with --framework <id>",
						guesses[0].Profile.ID, guesses[1].Profile.ID)
				}
				fmt.Fprintf(out, "  framework: %s — %s\n", prof.ID, note)
				for _, g := range guesses[1:] {
					fmt.Fprintf(out, "    (also considered %s, score %d)\n", g.Profile.ID, g.Score)
				}
			}

			res, err := ingest.BuildRequirements(ex.Text, prof)
			if err != nil {
				return err
			}
			if len(res.Requirements) == 0 {
				return fmt.Errorf("the %s profile's segmentation rules matched nothing in %s; check the profile or the extraction", prof.ID, in)
			}

			for _, l := range res.Log {
				fmt.Fprintf(out, "  %s\n", l)
			}

			if err := e.Store.SaveRequirements(prof.ID, prof.SourceRef, res.Requirements); err != nil {
				return err
			}

			e.State.Framework = prof.ID
			e.State.DisplayName = prof.DisplayName
			e.State.SourceRef = prof.SourceRef
			e.State.SourceFile = in
			e.State.DetectionNote = note
			e.State.FrameworkConfirmed = false
			e.State.IngestedAt = state.Now()
			e.State.Gates = nil
			e.State.Gate(state.GateSegmentation).Status = state.GatePending
			e.State.Gate(state.GateMapping).Status = state.GatePending
			e.State.Gate(state.GateSuggestion).Status = state.GatePending
			if err := e.Store.SaveState(e.State); err != nil {
				return err
			}

			fmt.Fprintf(out, "\nWrote %d requirement(s) to %s (all status=draft).\n", len(res.Requirements), e.Store.RequirementsPath())
			fmt.Fprintf(out, "Next: regmap review --gate 1\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&in, "in", "", "path to the source document (.pdf, .docx or .txt)")
	cmd.Flags().StringVar(&framework, "framework", "", "framework profile id (auto-detected when omitted)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing requirements, discarding review work")
	return cmd
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
