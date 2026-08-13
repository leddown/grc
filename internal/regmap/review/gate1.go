package review

import (
	"strconv"
	"strings"

	"grc/internal/regmap/requirement"
)

const gate1Help = `
GATE 1 — segmentation review. Actions:
  a  accept    mark this requirement approved; only approved items advance
  e  edit      correct the id, title, category or body text
  s  skip      leave it in draft and come back later
  r  reject    discard it as noise (headers, footnotes, page numbers)
  m  merge     append this requirement onto another one and reject this one
  p  split     split this requirement into two at a line boundary
  v  view      print the full body text
  q  quit      save and exit; re-run to resume where you left off
  ?  help      show this list
`

// Gate1 walks the segmented requirements with the reviewer. Only requirements
// the reviewer accepts here reach status=approved and advance to mapping.
func Gate1(reqs *requirement.Set, opts Options) (Outcome, error) {
	s := newSession(opts)
	var out Outcome

	if opts.AutoApprove {
		for i := range *reqs {
			if (*reqs)[i].Status == requirement.StatusDraft {
				approveRequirement(&(*reqs)[i], opts.Reviewer)
				out.Approved++
			}
		}
		if err := s.save(); err != nil {
			return out, err
		}
		s.printf("--auto-approve: %d requirement(s) approved without review.\n", out.Approved)
		s.summary(1, out)
		return out, nil
	}

	processed := map[string]bool{}
	reviewed := 0
	showFull := false

	for {
		pending := pendingQueue(*reqs, processed)
		if len(pending) == 0 {
			break
		}
		i := pending[0]
		req := (*reqs)[i]
		total := reviewed + len(pending)

		s.rule()
		s.printf("GATE 1 · item %d/%d · confidence: %s\n", reviewed+1, total, req.Confidence)
		s.rule()
		s.printf("  id       : %s\n", req.ID)
		s.printf("  section  : %s\n", req.Section)
		s.printf("  title    : %s\n", orNone(req.Title))
		s.printf("  category : %s\n", orNone(req.Category))
		s.printf("  strategy : %s\n", req.Strategy)
		if len(req.Flags) > 0 {
			s.printf("  flags    : %s\n", strings.Join(req.Flags, ", "))
		}
		s.printf("  text     :\n")
		s.body(req.Text, showFull)
		showFull = false
		s.printf("\n")

		k, err := s.key("[a]ccept [e]dit [s]kip [r]eject [m]erge [p]split [v]iew [q]uit [?]help: ")
		if err != nil {
			return out, err
		}

		switch k {
		case "a":
			approveRequirement(&(*reqs)[i], opts.Reviewer)
			out.Approved++
			reviewed++
			processed[req.ID] = true
			s.printf("  → approved %s\n\n", req.ID)

		case "r":
			note, err := s.askDefault("  reason (optional)", "")
			if err != nil {
				return out, err
			}
			(*reqs)[i].Status = requirement.StatusRejected
			(*reqs)[i].ReviewedBy = opts.Reviewer
			(*reqs)[i].ReviewedAt = now()
			if note != "" {
				(*reqs)[i].Notes = note
			}
			out.Rejected++
			reviewed++
			processed[req.ID] = true
			s.printf("  → rejected %s\n\n", req.ID)

		case "s":
			out.Skipped++
			reviewed++
			processed[req.ID] = true
			s.printf("  → skipped %s (stays in draft)\n\n", req.ID)

		case "e":
			if err := s.editRequirement(&(*reqs)[i]); err != nil {
				return out, err
			}

		case "m":
			if err := s.mergeRequirement(reqs, i, opts.Reviewer, processed, &out, &reviewed); err != nil {
				return out, err
			}

		case "p":
			if err := s.splitRequirement(reqs, i); err != nil {
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
			s.printf("\nSaved. Re-run `regmap review --gate 1` to resume.\n")
			s.summary(1, out)
			return out, nil

		case "?", "h":
			s.printf("%s\n", gate1Help)
			continue

		default:
			s.printf("  Unknown action %q — press ? for help.\n\n", k)
			continue
		}

		if err := s.save(); err != nil {
			return out, err
		}
	}

	s.summary(1, out)
	return out, nil
}

// approveRequirement is the single place a requirement becomes approved.
func approveRequirement(r *requirement.Requirement, reviewer string) {
	r.Status = requirement.StatusApproved
	r.ReviewedBy = reviewer
	r.ReviewedAt = now()
}

func pendingQueue(reqs requirement.Set, processed map[string]bool) []int {
	var out []int
	for _, i := range queue(reqs) {
		if !processed[reqs[i].ID] {
			out = append(out, i)
		}
	}
	return out
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

func (s *session) editRequirement(req *requirement.Requirement) error {
	for {
		k, err := s.key("  edit [i]d [t]itle [c]ategory [x]text [d]one: ")
		if err != nil {
			return err
		}
		switch k {
		case "i":
			v, err := s.askDefault("  id", req.ID)
			if err != nil {
				return err
			}
			req.ID = v
		case "t":
			v, err := s.askDefault("  title", req.Title)
			if err != nil {
				return err
			}
			req.Title = v
		case "c":
			if s.opts.Profile != nil && len(s.opts.Profile.Classification.Categories) > 0 {
				s.printf("  categories: %s\n", strings.Join(s.opts.Profile.Classification.Categories, " | "))
			}
			v, err := s.askDefault("  category", req.Category)
			if err != nil {
				return err
			}
			req.Category = v
		case "x":
			v, err := s.editText("text", req.Text)
			if err != nil {
				return err
			}
			req.Text = v
		case "d", "":
			s.printf("\n")
			return nil
		default:
			s.printf("  Unknown field %q.\n", k)
		}
	}
}

func (s *session) mergeRequirement(reqs *requirement.Set, i int, reviewer string, processed map[string]bool, out *Outcome, reviewed *int) error {
	cur := (*reqs)[i]
	def := ""
	if i > 0 {
		def = (*reqs)[i-1].ID
	}
	target, err := s.askDefault("  merge into which requirement id?", def)
	if err != nil {
		return err
	}
	if target == "" || target == cur.ID {
		s.printf("  Merge cancelled.\n\n")
		return nil
	}
	j := reqs.Index(target)
	if j < 0 {
		s.printf("  No requirement with id %q — merge cancelled.\n\n", target)
		return nil
	}

	(*reqs)[j].Text = strings.TrimSpace((*reqs)[j].Text) + "\n\n" + cur.Section + "\n" + cur.Text
	(*reqs)[j].AddFlag("merged-from-" + cur.ID)
	// A merged-into requirement needs looking at again.
	if (*reqs)[j].Status == requirement.StatusApproved {
		(*reqs)[j].Status = requirement.StatusDraft
		delete(processed, (*reqs)[j].ID)
		s.printf("  %s was already approved; it is back in draft for re-review.\n", (*reqs)[j].ID)
	}

	(*reqs)[i].Status = requirement.StatusRejected
	(*reqs)[i].ReviewedBy = reviewer
	(*reqs)[i].ReviewedAt = now()
	(*reqs)[i].Notes = "merged into " + target
	out.Rejected++
	*reviewed++
	processed[cur.ID] = true
	s.printf("  → merged %s into %s\n\n", cur.ID, target)
	return nil
}

func (s *session) splitRequirement(reqs *requirement.Set, i int) error {
	cur := (*reqs)[i]
	lines := strings.Split(cur.Text, "\n")
	s.printf("\n")
	for n, l := range lines {
		s.printf("  %3d | %s\n", n+1, l)
	}
	raw, err := s.askDefault("  split before which line number? (blank to cancel)", "")
	if err != nil {
		return err
	}
	if raw == "" {
		s.printf("  Split cancelled.\n\n")
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 2 || n > len(lines) {
		s.printf("  Line number must be between 2 and %d — split cancelled.\n\n", len(lines))
		return nil
	}

	head := strings.TrimSpace(strings.Join(lines[:n-1], "\n"))
	tail := strings.TrimSpace(strings.Join(lines[n-1:], "\n"))

	newID, err := s.askDefault("  id for the second half", cur.ID+"-B")
	if err != nil {
		return err
	}
	if reqs.Index(newID) >= 0 {
		s.printf("  id %q already exists — split cancelled.\n\n", newID)
		return nil
	}
	newTitle, err := s.askDefault("  title for the second half", cur.Title)
	if err != nil {
		return err
	}

	(*reqs)[i].Text = head
	(*reqs)[i].AddFlag("split-at-line-" + raw)

	second := requirement.Requirement{
		ID:         newID,
		Framework:  cur.Framework,
		Section:    cur.Section,
		Key:        cur.Key,
		Title:      newTitle,
		Category:   cur.Category,
		Text:       tail,
		Status:     requirement.StatusDraft,
		Strategy:   cur.Strategy,
		Confidence: requirement.ConfidenceMedium,
	}
	second.AddFlag("split-from-" + cur.ID)
	*reqs = append(*reqs, second)
	reqs.Sort()
	s.printf("  → split into %s and %s; both need review.\n\n", cur.ID, newID)
	return nil
}
