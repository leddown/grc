package assuranceplan

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"grc/internal/knowledge"
)

// AttributesInput names the regulation or domain to propose attributes for.
type AttributesInput struct {
	Focus string `json:"focus"`
	Count int    `json:"count"`
}

// AttributesProposal is the proposed test script additions and the shortlist
// they were chosen from.
type AttributesProposal struct {
	Focus      string              `json:"focus"`
	Attributes []ProposedAttribute `json:"attributes"`
	Shortlist  []AIRef             `json:"shortlist"`
	Unknowns   []string            `json:"unknowns"`
	Notes      []string            `json:"notes"`
}

// ProposedAttribute is an attribute as proposed. Its NFR key, control ids and
// regulatory references are derived from the citations that resolve; the
// ones that do not are kept apart in Unresolved and never stored on the plan.
type ProposedAttribute struct {
	AttrRef           string   `json:"attr_ref"`
	Domain            string   `json:"domain"`
	Requirement       string   `json:"requirement"`
	Procedure         string   `json:"procedure"`
	Method            string   `json:"method"`
	EvidenceRequested string   `json:"evidence_requested"`
	PassCriteria      string   `json:"pass_criteria"`
	AppliesTo         []string `json:"applies_to"`
	Basis             string   `json:"basis"`
	Why               string   `json:"why"`
	Cites             []AIRef  `json:"cites"`
	Unresolved        []AIRef  `json:"unresolved"`
	NFRKey            string   `json:"nfr_key"`
	ControlIDs        []string `json:"control_ids"`
	RegulatoryRefs    string   `json:"regulatory_refs"`
}

type attributesAnswer struct {
	Attributes []attributeIn `json:"attributes"`
	Unknowns   []string      `json:"unknowns"`
}

type attributeIn struct {
	AttrRef           string   `json:"attr_ref"`
	Domain            string   `json:"domain"`
	Requirement       string   `json:"requirement"`
	Procedure         string   `json:"procedure"`
	Method            string   `json:"method"`
	EvidenceRequested string   `json:"evidence_requested"`
	PassCriteria      string   `json:"pass_criteria"`
	AppliesTo         []string `json:"applies_to"`
	Basis             string   `json:"basis"`
	Why               string   `json:"why"`
	Cites             []aiCite `json:"cites"`
}

const (
	defaultAttributeCount = 8
	maxAttributeCount     = 15
)

// ProposeAttributes builds a shortlist from the catalogs for the focus and
// asks the model which of it the plan should test, and how.
func (s *Service) ProposeAttributes(ctx context.Context, id int64, in AttributesInput, actor string) (AIProposal, error) {
	in.Focus = strings.TrimSpace(in.Focus)
	if in.Focus == "" {
		return AIProposal{}, invalid("name the regulation or domain to propose attributes for, such as \"DORA Art. 9 access control\" or \"backup and restore\"")
	}
	if len(in.Focus) > 500 {
		return AIProposal{}, invalid("the focus is at most 500 characters")
	}
	if in.Count <= 0 {
		in.Count = defaultAttributeCount
	}
	in.Count = min(in.Count, maxAttributeCount)
	p, err := s.editable(id)
	if err != nil {
		return AIProposal{}, err
	}
	if s.ai == nil || s.ai.retriever == nil {
		return AIProposal{}, conflict("the catalogs are not available to this service, so there is no shortlist to propose from")
	}
	done, err := s.begin(id)
	if err != nil {
		return AIProposal{}, err
	}
	defer done()

	sl, list, err := s.buildShortlist(in.Focus, map[string]int{knowledge.KindNFR: maxShortlistKind, knowledge.KindControl: maxShortlistKind, knowledge.KindRegulationClause: 8})
	if err != nil {
		return AIProposal{}, err
	}
	if len(sl.items) == 0 {
		return AIProposal{}, invalid("nothing in the NFR catalog, the 800-53 controls or Regulation Coverage matches %q: widen the focus, or import the catalogs first", in.Focus)
	}
	prompt := attributesPrompt(p, in, list)
	var ans attributesAnswer
	resp, err := s.askJSON(ctx, prompt, func(text string) error { return decodeContract(text, &ans) })
	if err != nil {
		return AIProposal{}, err
	}
	prop := s.checkAttributes(p, ans, sl, planRefs(p))
	prop.Focus, prop.Shortlist = in.Focus, sl.items
	return s.record(p, AIKindAttributes, "", in, prop, resp, promptHash(aiRules, prompt), actor)
}

func attributesPrompt(p Plan, in AttributesInput, list string) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("Task: propose up to %d test attributes for this assurance plan's test script, for: %s", in.Count, in.Focus)
	line("A test attribute is one thing 2LoD tests on every selected item: the requirement, the test procedure, the method, the evidence requested and the pass criteria.")
	line("\nThe plan: %s %s (%s). Objective: %s", p.Reference, p.Title, planTypeName(p.PlanType), firstNonEmpty(oneLine(p.Objective), "not set"))
	for _, st := range p.Strata {
		line("- stratum %s: %s. %s", st.Code, st.Name, oneLine(st.Definition))
	}
	if len(p.Attributes) > 0 {
		line("\nAttributes it already has (do not repeat them):")
		for _, a := range p.Attributes {
			line("- %s %s: %s", a.AttrRef, a.Domain, clip(oneLine(a.Requirement), 160))
		}
	}
	line("\nThe shortlist, retrieved from this installation's catalogs. Choose from it; cite entries exactly as written:")
	b.WriteString(list)
	line(`
Reply with one JSON object:
{"attributes": [{"attr_ref": "short id, unique", "domain": "...", "requirement": "what is required, in a sentence or two",
   "procedure": "how 2LoD tests it on one item", "method": "Inspection|Inquiry|Observation|Re-performance",
   "evidence_requested": "...", "pass_criteria": "what the evidence must show",
   "applies_to": ["stratum codes, or empty for every stratum"], "basis": "regulation|good_practice",
   "why": "why this belongs in the plan", "cites": [{"kind": "nfr|control|regulation_clause", "ref": "..."}]}],
 "unknowns": ["..."]}
Pass criteria state only thresholds that appear in the shortlist or in the plan; otherwise they describe what the evidence must show without a number.`)
	return b.String()
}

// checkAttributes normalises each proposed attribute and resolves its
// citations. An attribute reference already in the plan, or used twice, is
// renamed rather than dropped.
func (s *Service) checkAttributes(p Plan, ans attributesAnswer, sl shortlist, own []string) AttributesProposal {
	out := AttributesProposal{Attributes: []ProposedAttribute{}, Shortlist: []AIRef{}, Unknowns: cleanList(ans.Unknowns, 20, 500), Notes: []string{}}
	note := func(format string, args ...any) { out.Notes = append(out.Notes, fmt.Sprintf(format, args...)) }
	codes := map[string]bool{}
	for _, st := range p.Strata {
		codes[st.Code] = true
	}
	taken := map[string]bool{}
	for _, a := range p.Attributes {
		taken[strings.ToLower(a.AttrRef)] = true
	}
	for i, in := range ans.Attributes {
		if len(out.Attributes) == maxAttributeCount {
			note("only the first %d attributes were kept", maxAttributeCount)
			break
		}
		a := ProposedAttribute{AttrRef: strings.TrimSpace(in.AttrRef), Domain: clip(strings.TrimSpace(in.Domain), 200),
			Requirement: clip(strings.TrimSpace(in.Requirement), maxAIText), Procedure: clip(strings.TrimSpace(in.Procedure), maxAIText),
			Method: strings.TrimSpace(in.Method), EvidenceRequested: clip(strings.TrimSpace(in.EvidenceRequested), maxAIText),
			PassCriteria: clip(strings.TrimSpace(in.PassCriteria), maxAIText), Why: clip(strings.TrimSpace(in.Why), 1000), AppliesTo: []string{}}
		if a.Requirement == "" {
			note("attribute %d has no requirement and was left out", i+1)
			continue
		}
		if a.AttrRef == "" || len(a.AttrRef) > maxAttrRef || strings.ContainsAny(a.AttrRef, "/,;\n") {
			a.AttrRef = fmt.Sprintf("AI-%02d", i+1)
		}
		if taken[strings.ToLower(a.AttrRef)] {
			base := a.AttrRef
			for n := 2; taken[strings.ToLower(a.AttrRef)]; n++ {
				a.AttrRef = fmt.Sprintf("%s-%d", clip(base, maxAttrRef-4), n)
			}
			note("%s is already used; the proposal is named %s", base, a.AttrRef)
		}
		taken[strings.ToLower(a.AttrRef)] = true
		if m := slices.IndexFunc(Methods, func(x string) bool { return strings.EqualFold(x, a.Method) }); m >= 0 {
			a.Method = Methods[m]
		} else {
			if a.Method != "" {
				note("%s: %q is not a method; it is Inspection", a.AttrRef, a.Method)
			}
			a.Method = MethodInspection
		}
		for _, c := range in.AppliesTo {
			c = strings.TrimSpace(c)
			switch {
			case c == "" || slices.Contains(a.AppliesTo, c):
			case !codes[c]:
				note("%s: %s is not one of the plan's strata and was left out of applies-to", a.AttrRef, c)
			default:
				a.AppliesTo = append(a.AppliesTo, c)
			}
		}
		a.Cites, a.Unresolved = s.resolveCites(in.Cites, sl, own)
		for _, c := range a.Cites {
			if !c.OnShortlist {
				note("%s cites %s %s, which is in the catalog but was not on the shortlist", a.AttrRef, c.Kind, c.Ref)
			}
		}
		if len(a.Unresolved) > 0 {
			note("%s cites %s, which do not resolve and are not kept", a.AttrRef, refList(a.Unresolved))
		}
		regulatory := slices.DeleteFunc(slices.Clone(a.Cites), func(r AIRef) bool { return r.Kind != knowledge.KindRegulationClause && r.Kind != RefKindPlan })
		a.Basis = basisFor(in.Basis, regulatory, func() {
			note("%s is marked regulation but cites no regulation clause that resolves; it is marked good practice", a.AttrRef)
		})
		a.ControlIDs = []string{}
		var clauses []string
		for _, c := range a.Cites {
			switch c.Kind {
			case knowledge.KindNFR:
				if a.NFRKey == "" {
					a.NFRKey = c.Ref
				} else if a.NFRKey != c.Ref {
					note("%s cites more than one NFR; it keeps %s", a.AttrRef, a.NFRKey)
				}
			case knowledge.KindControl:
				a.ControlIDs = append(a.ControlIDs, c.Ref)
			default:
				clauses = append(clauses, c.Ref)
			}
		}
		a.RegulatoryRefs = strings.Join(clauses, "; ")
		out.Attributes = append(out.Attributes, a)
	}
	if len(out.Attributes) == 0 {
		note("the answer proposed no attribute that could be kept")
	}
	return out
}

func attributesAnswerOf(a AttributesProposal) attributesAnswer {
	ans := attributesAnswer{Unknowns: a.Unknowns}
	for _, x := range a.Attributes {
		ans.Attributes = append(ans.Attributes, attributeIn{AttrRef: x.AttrRef, Domain: x.Domain, Requirement: x.Requirement,
			Procedure: x.Procedure, Method: x.Method, EvidenceRequested: x.EvidenceRequested, PassCriteria: x.PassCriteria,
			AppliesTo: x.AppliesTo, Basis: x.Basis, Why: x.Why, Cites: citesOf(x.Cites, x.Unresolved)})
	}
	return ans
}

// acceptAttributes appends the chosen attributes to the test script, marked
// as AI-generated with the proposal's model and prompt hash.
func (s *Service) acceptAttributes(id int64, pr AIProposal, payload []byte, chosen []string, actor string) (AttributesProposal, []string, error) {
	p, err := s.editable(id)
	if err != nil {
		return AttributesProposal{}, nil, err
	}
	var stored, edited AttributesProposal
	if err := decodePayload(pr.Payload, &stored); err != nil {
		return AttributesProposal{}, nil, err
	}
	if err := decodePayload(payload, &edited); err != nil {
		return AttributesProposal{}, nil, err
	}
	sl, own := shortlistOf(stored.Shortlist)
	if len(chosen) > 0 {
		keep := map[string]bool{}
		for _, c := range chosen {
			keep[strings.ToLower(strings.TrimSpace(c))] = true
		}
		edited.Attributes = slices.DeleteFunc(edited.Attributes, func(a ProposedAttribute) bool { return !keep[strings.ToLower(a.AttrRef)] })
	}
	checked := s.checkAttributes(p, attributesAnswerOf(edited), sl, append(own, planRefs(p)...))
	if len(checked.Attributes) == 0 {
		return AttributesProposal{}, nil, invalid("no attribute was chosen")
	}
	checked.Focus, checked.Shortlist = stored.Focus, stored.Shortlist
	attrs := slices.Clone(p.Attributes)
	refs := []string{}
	for _, a := range checked.Attributes {
		attrs = append(attrs, Attribute{AttrRef: a.AttrRef, Domain: a.Domain, Requirement: a.Requirement, Procedure: a.Procedure,
			Method: a.Method, EvidenceRequested: a.EvidenceRequested, PassCriteria: a.PassCriteria, AppliesTo: a.AppliesTo,
			RegulatoryRefs: a.RegulatoryRefs, ControlIDs: a.ControlIDs, NFRKey: a.NFRKey,
			AIGenerated: true, Model: pr.Model, PromptHash: pr.PromptHash})
		refs = append(refs, a.AttrRef)
	}
	if _, err := s.ReplaceAttributes(id, attrs, actor); err != nil {
		return AttributesProposal{}, nil, err
	}
	return checked, append([]string{"added " + strings.Join(refs, ", ")}, checked.Notes...), nil
}
