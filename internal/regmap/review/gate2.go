package review

import (
	"sort"
	"strings"

	"grc/internal/regmap/mapping"
	"grc/internal/regmap/requirement"
)

const gate2Help = `
GATE 2 — mapping review. Actions:
  a  accept    approve this requirement -> control mapping
  e  edit      add/remove control ids, edit the rationale or confidence
  u  unmapped  clear all controls and mark the requirement UNMAPPED
  s  skip      leave it in draft and come back later
  r  reject    reject this mapping outright
  v  view      print the full requirement text
  q  quit      save and exit; re-run to resume
  ?  help      show this list
`

const gate3Help = `
GATE 3 — LLM suggestion review. Everything here was proposed by a model and is
NOT approved until you accept it. Actions:
  a  accept    promote the machine suggestion to an approved mapping
  e  edit      correct the control ids or rationale before accepting
  s  skip      leave it in draft and come back later
  r  reject    discard the suggestion
  v  view      print the full requirement text
  q  quit      save and exit; re-run to resume
  ?  help      show this list
`

// Gate2 reviews curated (seed crosswalk) mappings.
func Gate2(reqs requirement.Set, entries *mapping.Set, opts Options) (Outcome, error) {
	return reviewMappings(2, mapping.SourceCurated, reqs, entries, opts)
}

// Gate3 reviews LLM-suggested mappings.
func Gate3(reqs requirement.Set, entries *mapping.Set, opts Options) (Outcome, error) {
	return reviewMappings(3, mapping.SourceLLMSuggested, reqs, entries, opts)
}

func reviewMappings(gate int, source mapping.Source, reqs requirement.Set, entries *mapping.Set, opts Options) (Outcome, error) {
	s := newSession(opts)
	var out Outcome

	help := gate2Help
	if gate == 3 {
		help = gate3Help
	}

	if opts.AutoApprove {
		for i := range *entries {
			e := &(*entries)[i]
			if e.Source == source && e.Status == requirement.StatusDraft {
				approveMapping(e, opts.Reviewer)
				out.Approved++
			}
		}
		if err := s.save(); err != nil {
			return out, err
		}
		s.printf("--auto-approve: %d mapping(s) approved without review.\n", out.Approved)
		s.summary(gate, out)
		return out, nil
	}

	byID := map[string]requirement.Requirement{}
	for _, r := range reqs {
		byID[r.ID] = r
	}

	pending := mappingQueue(*entries, source, gate)
	total := len(pending)
	if total == 0 {
		s.printf("Nothing to review at GATE %d.\n", gate)
		s.summary(gate, out)
		return out, nil
	}

	showFull := false
	for n := 0; n < len(pending); {
		i := pending[n]
		e := (*entries)[i]
		req := byID[e.ReqID]

		s.rule()
		label := "curated seed mapping"
		if gate == 3 {
			label = "MACHINE-SUGGESTED — not approved until you accept"
		}
		s.printf("GATE %d · item %d/%d · %s\n", gate, n+1, total, label)
		s.rule()
		s.printf("  requirement : %s  %s\n", req.ID, req.Section)
		s.printf("  title       : %s\n", orNone(req.Title))
		s.printf("  category    : %s\n", orNone(req.Category))
		if e.IsUnmapped() {
			s.printf("  controls    : *** UNMAPPED ***\n")
		} else {
			s.printf("  controls    :\n")
			for _, id := range e.NISTControlIDs {
				title := "(not in catalog)"
				if opts.Catalog != nil {
					title = opts.Catalog.Title(id)
				}
				s.printf("      %-8s %s\n", id, title)
			}
		}
		if len(e.Unknown) > 0 {
			s.printf("  unknown ids : %s (not present in the 800-53 catalog)\n", strings.Join(e.Unknown, ", "))
		}
		s.printf("  rationale   : %s\n", orNone(e.Rationale))
		s.printf("  confidence  : %s\n", orNone(e.Confidence))
		s.printf("  source      : %s\n", e.Source)
		if e.SeedGroup != "" {
			s.printf("  seed group  : %s\n", e.SeedGroup)
		}
		if e.Model != "" {
			s.printf("  model       : %s\n", e.Model)
		}
		s.printf("  requirement text:\n")
		s.body(req.Text, showFull)
		showFull = false
		s.printf("\n")

		prompt := "[a]ccept [e]dit [u]nmapped [s]kip [r]eject [v]iew [q]uit [?]help: "
		if gate == 3 {
			prompt = "[a]ccept [e]dit [s]kip [r]eject [v]iew [q]uit [?]help: "
		}
		k, err := s.key(prompt)
		if err != nil {
			return out, err
		}

		switch k {
		case "a":
			approveMapping(&(*entries)[i], opts.Reviewer)
			out.Approved++
			n++
			s.printf("  → approved mapping for %s\n\n", e.ReqID)

		case "r":
			(*entries)[i].Status = requirement.StatusRejected
			(*entries)[i].ReviewedBy = opts.Reviewer
			(*entries)[i].ReviewedAt = now()
			out.Rejected++
			n++
			s.printf("  → rejected mapping for %s\n\n", e.ReqID)

		case "s":
			out.Skipped++
			n++
			s.printf("  → skipped %s (stays in draft)\n\n", e.ReqID)

		case "u":
			if gate != 2 {
				s.printf("  Unknown action %q — press ? for help.\n\n", k)
				continue
			}
			(*entries)[i].NISTControlIDs = nil
			(*entries)[i].Confidence = mapping.ConfidenceHigh
			if (*entries)[i].Rationale == "" {
				(*entries)[i].Rationale = "Reviewer determined no 800-53 control applies."
			}
			s.printf("  → marked UNMAPPED; accept to record that decision.\n\n")

		case "e":
			if err := s.editMapping(&(*entries)[i]); err != nil {
				return out, err
			}

		case "v":
			showFull = true
			continue

		case "q":
			out.Quit = true
			if err := s.save(); err != nil {
				return out, err
			}
			s.printf("\nSaved. Re-run `regmap review --gate %d` to resume.\n", gate)
			s.summary(gate, out)
			return out, nil

		case "?", "h":
			s.printf("%s\n", help)
			continue

		default:
			s.printf("  Unknown action %q — press ? for help.\n\n", k)
			continue
		}

		if err := s.save(); err != nil {
			return out, err
		}
	}

	s.summary(gate, out)
	return out, nil
}

// approveMapping is the single place a mapping becomes approved.
func approveMapping(e *mapping.Entry, reviewer string) {
	e.Status = requirement.StatusApproved
	e.ReviewedBy = reviewer
	e.ReviewedAt = now()
}

// mappingQueue selects draft entries of the given source. At GATE 2, UNMAPPED
// requirements are shown first because they are the ones needing judgement.
func mappingQueue(entries mapping.Set, source mapping.Source, gate int) []int {
	var idx []int
	for i, e := range entries {
		if e.Source == source && e.Status == requirement.StatusDraft {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ea, eb := entries[idx[a]], entries[idx[b]]
		if gate == 2 && ea.IsUnmapped() != eb.IsUnmapped() {
			return ea.IsUnmapped()
		}
		return requirement.NaturalLess(ea.ReqID, eb.ReqID)
	})
	return idx
}

func (s *session) editMapping(e *mapping.Entry) error {
	for {
		k, err := s.key("  edit [c]ontrols [r]ationale [f]confidence [d]one: ")
		if err != nil {
			return err
		}
		switch k {
		case "c":
			s.printf("  Enter the full control list, comma separated (e.g. IR-4, IR-6, AU-6).\n")
			s.printf("  Leave blank to keep, or type UNMAPPED to clear.\n")
			v, err := s.askDefault("  controls", strings.Join(e.NISTControlIDs, ", "))
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(v), "unmapped") {
				e.NISTControlIDs = nil
				e.Unknown = nil
				s.printf("  → UNMAPPED\n")
				continue
			}
			ids := mapping.NormalizeControls(strings.Split(v, ","))
			if s.opts.Catalog != nil {
				known, unknown := s.opts.Catalog.Validate(ids)
				if len(unknown) > 0 {
					s.printf("  ! not in the 800-53 catalog: %s\n", strings.Join(unknown, ", "))
					keep, err := s.key("  keep them anyway? [y/n]: ")
					if err != nil {
						return err
					}
					if keep == "y" {
						e.NISTControlIDs = ids
						e.Unknown = unknown
						continue
					}
					ids = known
				}
				e.Unknown = nil
			}
			e.NISTControlIDs = ids

		case "r":
			v, err := s.editText("rationale", e.Rationale)
			if err != nil {
				return err
			}
			e.Rationale = v

		case "f":
			v, err := s.askDefault("  confidence (high|medium|low)", e.Confidence)
			if err != nil {
				return err
			}
			switch strings.ToLower(v) {
			case mapping.ConfidenceHigh, mapping.ConfidenceMedium, mapping.ConfidenceLow:
				e.Confidence = strings.ToLower(v)
			default:
				s.printf("  Confidence must be high, medium or low.\n")
			}

		case "d", "":
			s.printf("\n")
			return nil
		default:
			s.printf("  Unknown field %q.\n", k)
		}
	}
}
