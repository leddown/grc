package oscal

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// textToken matches the two inline constructs OSCAL markup-multiline prose
// carries in the NIST catalog: a parameter insertion and a markdown link to a
// fragment of the catalog ("[AC-16a](#ac-16_smt.a)").
var textToken = regexp.MustCompile(`\{\{\s*insert:\s*param,\s*([^\s}]+)\s*\}\}|\[([^\]]+)\]\(#[^)]*\)`)

// looseBeforeClose is the space NIST's source leaves between an inserted
// element and the punctuation after it ("{{ insert: param, x }} ; and"). The
// text grc's pages, reports and workbook read has none ("]; and").
var looseBeforeClose = regexp.MustCompile(`^\s+[;,.:)\]]`)

// maxParamDepth bounds parameters that insert parameters (a selection whose
// choice is an assignment), so a cycle in a bad file ends instead of
// recursing forever.
const maxParamDepth = 6

// textRenderer renders prose into the plain text shape grc's catalog uses:
// parameters as "[Assignment: organization-defined …]" and
// "[Selection (one or more): …; …]", links as their text.
type textRenderer struct {
	params  map[string]Parameter
	missing map[string]bool
}

func newTextRenderer(params map[string]Parameter) *textRenderer {
	return &textRenderer{params: params, missing: map[string]bool{}}
}

func (r *textRenderer) text(prose string, depth int) string {
	var b strings.Builder
	pos := 0
	for _, m := range textToken.FindAllStringSubmatchIndex(prose, -1) {
		b.WriteString(prose[pos:m[0]])
		if m[2] >= 0 {
			b.WriteString(r.param(prose[m[2]:m[3]], prose[m[0]:m[1]], depth))
		} else {
			b.WriteString(prose[m[4]:m[5]])
		}
		pos = m[1]
		if loose := looseBeforeClose.FindStringIndex(prose[pos:]); loose != nil {
			pos += loose[1] - 1
		}
	}
	b.WriteString(prose[pos:])
	return b.String()
}

func (r *textRenderer) param(id, original string, depth int) string {
	p, ok := r.params[id]
	if !ok || depth > maxParamDepth {
		r.missing[id] = true
		return original
	}
	if p.Select != nil {
		head := "Selection"
		if p.Select.HowMany == "one-or-more" {
			head = "Selection (one or more)"
		}
		var choices []string
		if p.Select.Choice != nil {
			for _, choice := range *p.Select.Choice {
				choices = append(choices, strings.TrimSpace(r.text(choice, depth+1)))
			}
		}
		return "[" + head + ": " + strings.Join(choices, "; ") + "]"
	}
	label := strings.TrimSpace(r.text(p.Label, depth+1))
	if label == "" {
		r.missing[id] = true
		return original
	}
	if !strings.HasPrefix(label, "organization-defined") {
		label = "organization-defined " + label
	}
	return "[Assignment: " + label + "]"
}

// statement renders a control's statement part: its own prose, then each item
// as "label prose" on its own line, depth first, the way the catalog file
// grc ships has always held nist_requirements.
func (r *textRenderer) statement(part *Part) string {
	if part == nil {
		return ""
	}
	var lines []string
	var walk func(p Part)
	walk = func(p Part) {
		if strings.TrimSpace(p.Prose) != "" {
			line := r.text(p.Prose, 0)
			if label := partLabel(p); label != "" {
				line = label + " " + line
			}
			lines = append(lines, strings.TrimSpace(line))
		}
		if p.Parts == nil {
			return
		}
		for _, child := range *p.Parts {
			if child.Name == "item" {
				walk(child)
			}
		}
	}
	walk(*part)
	return strings.Join(lines, "\n")
}

func (r *textRenderer) missingProblems(controlID string) []string {
	if len(r.missing) == 0 {
		return nil
	}
	ids := make([]string, 0, len(r.missing))
	for id := range r.missing {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	r.missing = map[string]bool{}
	return []string{fmt.Sprintf("%s: the text inserts parameter(s) %s, which the catalog does not define with a label or a selection", controlID, strings.Join(ids, ", "))}
}

func partLabel(p Part) string {
	if p.Props == nil {
		return ""
	}
	for _, prop := range *p.Props {
		if prop.Name == "label" && prop.Ns == "" {
			return strings.TrimSpace(prop.Value)
		}
	}
	return ""
}
