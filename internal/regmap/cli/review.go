package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"grc/internal/regmap/mapping"
	"grc/internal/regmap/requirement"
	"grc/internal/regmap/review"
	"grc/internal/regmap/state"
)

func newReviewCommand() *cobra.Command {
	var gate int
	var autoApprove bool

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Step through a review gate interactively",
		Long: `Review the pipeline's output one item at a time.

  --gate 1   segmentation: confirm the framework, then fix ids, titles,
             categories, merges, splits and noise
  --gate 2   mappings: confirm or correct the controls resolved from the
             framework's seed crosswalk
  --gate 3   suggestions: accept, edit or reject machine-proposed mappings

Quitting with [q] saves progress; re-running resumes where you left off.
--auto-approve is per-gate, explicit, and off by default.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if gate < 1 || gate > 3 {
				return fmt.Errorf("--gate must be 1, 2 or 3")
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

			if !autoApprove && !review.IsInteractive() {
				return fmt.Errorf("stdin is not a terminal, so GATE %d cannot be reviewed here.\n"+
					"Run `regmap review --gate %d` from an interactive shell.\n"+
					"If you really intend to approve without reading each item, pass --auto-approve explicitly",
					gate, gate)
			}

			opts := review.Options{
				Out:         out,
				Reviewer:    reviewer,
				AutoApprove: autoApprove,
				Catalog:     e.Catalog,
				Profile:     prof,
			}

			switch gate {
			case 1:
				return runGate1(e, opts)
			case 2:
				return runMappingGate(e, opts, state.GateMapping)
			default:
				return runMappingGate(e, opts, state.GateSuggestion)
			}
		},
	}

	cmd.Flags().IntVar(&gate, "gate", 0, "which gate to review (1, 2 or 3)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "approve every pending item at this gate without reading it")
	return cmd
}

func runGate1(e *env, opts review.Options) error {
	prof, err := e.activeProfile()
	if err != nil {
		return err
	}
	reqs, err := e.Store.LoadRequirements()
	if err != nil {
		return err
	}
	if len(reqs) == 0 {
		return fmt.Errorf("no requirements found in %s — run `regmap ingest --in <file>` first", e.Store.RequirementsPath())
	}

	if !e.State.FrameworkConfirmed {
		ok, err := review.ConfirmFramework(opts, prof, e.State.SourceFile, e.State.DetectionNote)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("framework not confirmed; re-run ingest with the right one:\n"+
				"  regmap ingest --in %s --framework <id> --force\n"+
				"(`regmap frameworks` lists the available ids)", e.State.SourceFile)
		}
		e.State.FrameworkConfirmed = true
		if err := e.Store.SaveState(e.State); err != nil {
			return err
		}
	}

	opts.Save = func() error {
		return e.Store.SaveRequirements(e.State.Framework, e.State.SourceRef, reqs)
	}

	gs := e.State.Gate(state.GateSegmentation)
	gs.Status = state.GateInProgress
	if err := e.Store.SaveState(e.State); err != nil {
		return err
	}

	outcome, err := review.Gate1(&reqs, opts)
	if err != nil {
		return err
	}
	if err := e.Store.SaveRequirements(e.State.Framework, e.State.SourceRef, reqs); err != nil {
		return err
	}

	recordGate(e, state.GateSegmentation, outcome, remainingRequirements(reqs), opts.AutoApprove)
	if err := e.Store.SaveState(e.State); err != nil {
		return err
	}
	if e.State.GateComplete(state.GateSegmentation) {
		fmt.Fprintf(opts.Out, "Next: regmap map\n")
	}
	return nil
}

func runMappingGate(e *env, opts review.Options, gate int) error {
	reqs, err := e.Store.LoadRequirements()
	if err != nil {
		return err
	}
	entries, err := e.Store.LoadMappings()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		if gate == state.GateMapping {
			return fmt.Errorf("no mappings found in %s — run `regmap map` first", e.Store.MappingsPath())
		}
		return fmt.Errorf("no mappings found in %s — run `regmap suggest --with-llm` first", e.Store.MappingsPath())
	}

	source := mapping.SourceCurated
	if gate == state.GateSuggestion {
		source = mapping.SourceLLMSuggested
	}
	if countPending(entries, source) == 0 {
		fmt.Fprintf(opts.Out, "Nothing pending at GATE %d.\n", gate)
		if gate == state.GateSuggestion {
			fmt.Fprintf(opts.Out, "Run `regmap suggest --with-llm` to generate suggestions for UNMAPPED requirements.\n")
		}
		return nil
	}

	opts.Save = func() error {
		return e.Store.SaveMappings(e.State.Framework, e.State.SourceRef, entries)
	}

	gs := e.State.Gate(gate)
	gs.Status = state.GateInProgress
	if err := e.Store.SaveState(e.State); err != nil {
		return err
	}

	var outcome review.Outcome
	if gate == state.GateMapping {
		outcome, err = review.Gate2(reqs, &entries, opts)
	} else {
		outcome, err = review.Gate3(reqs, &entries, opts)
	}
	if err != nil {
		return err
	}
	if err := e.Store.SaveMappings(e.State.Framework, e.State.SourceRef, entries); err != nil {
		return err
	}

	recordGate(e, gate, outcome, countPending(entries, source), opts.AutoApprove)
	if err := e.Store.SaveState(e.State); err != nil {
		return err
	}
	if e.State.GateComplete(gate) {
		if gate == state.GateMapping {
			fmt.Fprintf(opts.Out, "Next: regmap suggest --with-llm   (or go straight to `report`)\n")
		} else {
			fmt.Fprintf(opts.Out, "Next: regmap report --format md --out crosswalk.md\n")
		}
	}
	return nil
}

// recordGate accumulates the gate tally and marks it complete only when nothing
// is left in draft.
func recordGate(e *env, gate int, o review.Outcome, remaining int, auto bool) {
	gs := e.State.Gate(gate)
	gs.Approved += o.Approved
	gs.Rejected += o.Rejected
	gs.Skipped = remaining
	gs.ReviewedBy = reviewer
	gs.AutoApproved = gs.AutoApproved || auto
	if remaining == 0 {
		gs.Status = state.GateComplete
		gs.CompletedAt = state.Now()
	} else {
		gs.Status = state.GateInProgress
	}
}

func remainingRequirements(reqs requirement.Set) int {
	n := 0
	for _, r := range reqs {
		if r.Status == requirement.StatusDraft {
			n++
		}
	}
	return n
}

func countPending(entries mapping.Set, source mapping.Source) int {
	n := 0
	for _, e := range entries {
		if e.Source == source && e.Status == requirement.StatusDraft {
			n++
		}
	}
	return n
}
