package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"grc/internal/regmap/mapping"
	"grc/internal/regmap/report"
	"grc/internal/regmap/requirement"
	"grc/internal/regmap/state"
	"grc/internal/regmap/suggest"
)

func newMapCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "map",
		Short: "Resolve GATE 1-approved requirements against the framework's seed crosswalk",
		Long: `Resolve controls for every approved requirement using the seed crosswalk in the
framework profile. Requirements with no seed hit become UNMAPPED entries — they
are never dropped. All output is status=draft; GATE 2 is where you validate it.

Re-running is safe: approved and rejected mappings are preserved untouched.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}
			prof, err := e.activeProfile()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			reqs, err := e.Store.LoadRequirements()
			if err != nil {
				return err
			}
			if len(reqs) == 0 {
				return fmt.Errorf("no requirements found — run `regmap ingest --in <file>` first")
			}
			approved := reqs.Approved()
			if len(approved) == 0 {
				return fmt.Errorf("no requirements have been approved at GATE 1 yet — run `regmap review --gate 1` first")
			}
			if !e.State.GateComplete(state.GateSegmentation) {
				fmt.Fprintf(out, "note: GATE 1 is not finished; mapping only the %d requirement(s) approved so far.\n", len(approved))
			}

			existing, err := e.Store.LoadMappings()
			if err != nil {
				return err
			}
			entries := mapping.Resolve(approved, prof, existing)

			var seeded, unmapped, preserved int
			for _, en := range entries {
				switch {
				case en.Status != requirement.StatusDraft:
					preserved++
				case en.IsUnmapped():
					unmapped++
				default:
					seeded++
				}
			}

			if err := e.Store.SaveMappings(e.State.Framework, e.State.SourceRef, entries); err != nil {
				return err
			}
			if err := e.Store.SaveState(e.State); err != nil {
				return err
			}

			fmt.Fprintf(out, "Resolved %d requirement(s) against the %s seed crosswalk:\n", len(approved), prof.ID)
			fmt.Fprintf(out, "  %d draft mapping(s) from seed entries\n", seeded)
			fmt.Fprintf(out, "  %d UNMAPPED (no seed entry matched)\n", unmapped)
			fmt.Fprintf(out, "  %d already-reviewed mapping(s) left untouched\n", preserved)
			fmt.Fprintf(out, "\nWrote %s. Next: regmap review --gate 2\n", e.Store.MappingsPath())
			return nil
		},
	}
}

func newSuggestCommand() *cobra.Command {
	var withLLM bool
	var model string
	var limit int

	cmd := &cobra.Command{
		Use:   "suggest",
		Short: "Ask Claude to propose mappings for UNMAPPED requirements",
		Long: `Send GATE 1-approved requirements that resolved to UNMAPPED to the Anthropic API
and record the proposed controls as status=draft, source=llm-suggested.

Nothing here is approved. GATE 3 is where you accept, edit or reject each
suggestion. ANTHROPIC_API_KEY must be set in the environment.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !withLLM {
				return fmt.Errorf("suggest calls an external API, so it requires the explicit --with-llm flag")
			}
			e, err := loadEnv()
			if err != nil {
				return err
			}
			prof, err := e.activeProfile()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			reqs, err := e.Store.LoadRequirements()
			if err != nil {
				return err
			}
			entries, err := e.Store.LoadMappings()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return fmt.Errorf("no mappings found — run `regmap map` first")
			}

			targets := suggest.Targets(reqs, entries)
			if len(targets) == 0 {
				fmt.Fprintf(out, "Nothing to suggest: every approved requirement already has a mapping or a pending suggestion.\n")
				return nil
			}
			fmt.Fprintf(out, "%d UNMAPPED requirement(s) to send to %s.\n", len(targets), model)

			updated, added, runErr := suggest.Run(cmd.Context(), reqs, entries, suggest.Options{
				Model:   model,
				Catalog: e.Catalog,
				Profile: prof,
				Limit:   limit,
				Out:     out,
			})
			// Persist whatever was gathered before surfacing any error, so a
			// failed run is resumable rather than wasted.
			if added > 0 {
				if err := e.Store.SaveMappings(e.State.Framework, e.State.SourceRef, updated); err != nil {
					return err
				}
			}
			if runErr != nil {
				if added > 0 {
					fmt.Fprintf(out, "Saved %d suggestion(s) before the failure; re-run to continue.\n", added)
				}
				return runErr
			}

			fmt.Fprintf(out, "\nAdded %d machine-suggested mapping(s), all status=draft.\n", added)
			fmt.Fprintf(out, "Next: regmap review --gate 3\n")
			return nil
		},
	}

	cmd.Flags().BoolVar(&withLLM, "with-llm", false, "required: confirm you want to call the Anthropic API")
	cmd.Flags().StringVar(&model, "model", suggest.DefaultModel, "Anthropic model to query")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of requirements to send (0 = no limit)")
	return cmd
}

func newReportCommand() *cobra.Command {
	var format, outPath string
	var approvedOnly bool

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Render the crosswalk as Markdown, JSON or CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}
			if e.State.Framework == "" {
				return fmt.Errorf("no framework has been ingested yet — run `regmap ingest --in <file>` first")
			}
			reqs, err := e.Store.LoadRequirements()
			if err != nil {
				return err
			}
			entries, err := e.Store.LoadMappings()
			if err != nil {
				return err
			}

			in := report.Input{
				Framework:    e.State.Framework,
				DisplayName:  e.State.DisplayName,
				SourceRef:    e.State.SourceRef,
				SourceFile:   e.State.SourceFile,
				GeneratedAt:  state.Now(),
				ApprovedOnly: approvedOnly,
				Requirements: reqs,
				Mappings:     entries,
				Catalog:      e.Catalog,
			}

			w := cmd.OutOrStdout()
			if outPath != "" {
				// #nosec G304 -- the report destination named by --out.
				// 0600: the crosswalk carries regulatory content and reviewer
				// identities, so it is not world-readable by default.
				f, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
				if err != nil {
					return fmt.Errorf("create %s: %w", outPath, err)
				}
				defer f.Close()
				w = f
			}
			if err := report.Render(format, in, w); err != nil {
				return err
			}
			if outPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%s, %s).\n", outPath, format, scopeLabel(approvedOnly))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "md", "output format: md, json or csv")
	cmd.Flags().StringVar(&outPath, "out", "", "write to this file instead of stdout")
	cmd.Flags().BoolVar(&approvedOnly, "approved-only", false, "emit only the human-approved crosswalk")
	return cmd
}

func scopeLabel(approvedOnly bool) string {
	if approvedOnly {
		return "approved only"
	}
	return "all items"
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show pipeline state and per-item counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if e.State.Framework == "" {
				fmt.Fprintf(out, "No framework ingested yet.\nStart with: regmap ingest --in <file>\n")
				return nil
			}
			reqs, err := e.Store.LoadRequirements()
			if err != nil {
				return err
			}
			entries, err := e.Store.LoadMappings()
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "Framework   : %s (%s)\n", e.State.DisplayName, e.State.Framework)
			fmt.Fprintf(out, "Source ref  : %s\n", e.State.SourceRef)
			fmt.Fprintf(out, "Source file : %s\n", e.State.SourceFile)
			fmt.Fprintf(out, "Ingested    : %s\n", e.State.IngestedAt)
			fmt.Fprintf(out, "Framework confirmed by reviewer: %v\n\n", e.State.FrameworkConfirmed)

			rc := reqs.CountByStatus()
			fmt.Fprintf(out, "Requirements: %d total — approved %d, draft %d, rejected %d\n",
				len(reqs), rc[requirement.StatusApproved], rc[requirement.StatusDraft], rc[requirement.StatusRejected])

			curated := countBy(entries, mapping.SourceCurated)
			llm := countBy(entries, mapping.SourceLLMSuggested)
			fmt.Fprintf(out, "Mappings    : %d total\n", len(entries))
			fmt.Fprintf(out, "  curated       — approved %d, draft %d, rejected %d\n",
				curated[requirement.StatusApproved], curated[requirement.StatusDraft], curated[requirement.StatusRejected])
			fmt.Fprintf(out, "  llm-suggested — approved %d, draft %d, rejected %d\n\n",
				llm[requirement.StatusApproved], llm[requirement.StatusDraft], llm[requirement.StatusRejected])

			fmt.Fprintf(out, "%-6s %-12s %-9s %-9s %-9s %s\n", "GATE", "STATUS", "APPROVED", "REJECTED", "PENDING", "COMPLETED")
			names := map[int]string{1: "segmentation", 2: "mappings", 3: "suggestions"}
			for _, n := range []int{1, 2, 3} {
				g := e.State.Gate(n)
				flag := ""
				if g.AutoApproved {
					flag = "  (auto-approved)"
				}
				fmt.Fprintf(out, "%-6d %-12s %-9d %-9d %-9d %s%s\n",
					n, g.Status, g.Approved, g.Rejected, g.Skipped, dash(g.CompletedAt), flag)
				_ = names[n]
			}

			fmt.Fprintf(out, "\nNext: %s\n", nextStep(e, reqs, entries))
			return nil
		},
	}
}

func countBy(entries mapping.Set, source mapping.Source) map[requirement.Status]int {
	out := map[requirement.Status]int{}
	for _, e := range entries {
		if e.Source == source {
			out[e.Status]++
		}
	}
	return out
}

func nextStep(e *env, reqs requirement.Set, entries mapping.Set) string {
	switch {
	case !e.State.GateComplete(state.GateSegmentation):
		return "regmap review --gate 1"
	case len(entries) == 0:
		return "regmap map"
	case countPending(entries, mapping.SourceCurated) > 0:
		return "regmap review --gate 2"
	case len(suggest.Targets(reqs, entries)) > 0:
		return "regmap suggest --with-llm"
	case countPending(entries, mapping.SourceLLMSuggested) > 0:
		return "regmap review --gate 3"
	default:
		return "regmap report --format md --out crosswalk.md"
	}
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
