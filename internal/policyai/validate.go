package policyai

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"grc/internal/knowledge"
	"grc/internal/policydocs"
	"grc/internal/policystudio"
)

// Caps on one proposal. A model that returns more is told so in the result;
// the rest are recorded as refused, not silently dropped.
const (
	maxEdits            = 25
	maxReplacementRunes = 4000
)

var factKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Edit statuses.
const (
	EditOK       = "ok"
	EditRejected = "rejected"
)

// CheckedCitation is a citation after the catalog and verbatim checks.
type CheckedCitation struct {
	Citation
	// Known is whether the cited record exists here.
	Known bool `json:"known"`
	// Verified is whether the citation's quote appears verbatim in the cited
	// record. An unverifiable quote is removed, not shown as if it were one.
	Verified bool   `json:"verified"`
	Note     string `json:"note,omitempty"`
}

// CheckedEdit is an edit after validation, ready for the editor to place.
type CheckedEdit struct {
	SUID       string `json:"suid"`
	Op         string `json:"op"`
	BlockID    string `json:"block_id"`
	SectionUID string `json:"section_uid"`
	// Quote is the anchored text exactly as it stands in the block (the model's
	// quote matched it after normalization); BlockText is the block's whole
	// baseline text when the proposal was made.
	Quote     string `json:"quote"`
	BlockText string `json:"block_text"`
	// WholeBlock is set when the edit replaces or deletes the whole block.
	WholeBlock  bool   `json:"whole_block"`
	Replacement string `json:"replacement_markdown"`
	// Fragment is the replacement as schema nodes: inline content for a
	// replacement inside a block (Inline set), otherwise blocks.
	Fragment  []*policystudio.Node `json:"fragment"`
	Inline    bool                 `json:"inline"`
	Rationale string               `json:"rationale"`
	Citations []CheckedCitation    `json:"citations"`
	Warnings  []string             `json:"warnings"`
	Status    string               `json:"status"`
	Reason    string               `json:"reason,omitempty"`
}

// CheckedMapping is a proposed control mapping after the catalog check.
type CheckedMapping struct {
	ControlMapping
	ControlName string `json:"control_name,omitempty"`
	Kept        bool   `json:"kept"`
	Note        string `json:"note,omitempty"`
}

// sources is what validation checks a proposal against.
type sources struct {
	blocks   map[string]block
	scope    map[string]bool
	sections map[string]bool
	facts    map[string]string // recorded client facts, key -> value
	declared map[string]bool   // keys the template declares or the text uses
	lookup   func(kind, ref string) (knowledge.Item, bool)
	source   map[int]string // library passages, by number, when mapping one in
	newID    func() string
}

// check validates a proposal against the live document and the catalog.
func check(p *Proposal, src sources) ([]CheckedEdit, []CheckedMapping, []NewFact) {
	newFacts := []NewFact{}
	haveFact := map[string]bool{}
	for _, f := range p.NewFacts {
		f.Key = strings.TrimSpace(f.Key)
		if factKeyPattern.MatchString(f.Key) && !haveFact[f.Key] {
			haveFact[f.Key] = true
			newFacts = append(newFacts, NewFact{Key: f.Key, Description: strings.TrimSpace(f.Description)})
		}
	}
	edits := make([]CheckedEdit, 0, len(p.Edits))
	for i, e := range p.Edits {
		ce := CheckedEdit{SUID: policydocs.NewUID(), Op: e.Op, BlockID: strings.TrimSpace(e.BlockID), Replacement: e.ReplacementMarkdown,
			Rationale: strings.TrimSpace(e.Rationale), Citations: []CheckedCitation{}, Warnings: []string{}, Status: EditOK}
		if i >= maxEdits {
			ce.reject(fmt.Sprintf("over the limit of %d edits in one proposal", maxEdits))
		} else {
			src.checkEdit(&ce, e)
		}
		if ce.Status == EditOK {
			for _, key := range factKeys(ce.Fragment) {
				if _, recorded := src.facts[key]; recorded || src.declared[key] {
					continue
				}
				ce.Warnings = append(ce.Warnings, "introduces a client fact, "+key+", that is not recorded; it stays unresolved until someone fills it in")
				if !haveFact[key] {
					haveFact[key] = true
					newFacts = append(newFacts, NewFact{Key: key, Description: "Proposed by the AI in an edit"})
				}
			}
			for _, id := range controlRefs(ce.Fragment) {
				if _, ok := src.lookup(knowledge.KindControl, id); !ok {
					ce.Warnings = append(ce.Warnings, "refers to "+id+", which is not in the control catalog")
				}
			}
		}
		for _, c := range e.Citations {
			ce.Citations = append(ce.Citations, src.checkCitation(c))
		}
		edits = append(edits, ce)
	}

	mappings := make([]CheckedMapping, 0, len(p.ControlMappings))
	for _, m := range p.ControlMappings {
		cm := CheckedMapping{ControlMapping: m, Kept: true}
		cm.ControlID = strings.ToUpper(strings.TrimSpace(m.ControlID))
		item, known := src.lookup(knowledge.KindControl, cm.ControlID)
		switch {
		case !src.sections[m.SectionUID]:
			cm.Kept, cm.Note = false, "the section is not in this document"
		case !known:
			cm.Kept, cm.Note = false, "not in the control catalog"
		default:
			cm.ControlName = item.Title
			if m.Coverage == policydocs.CoverageFull {
				cm.Note = "full coverage is a claim a person has to make; check the section says everything the control requires"
			}
		}
		mappings = append(mappings, cm)
	}
	return edits, mappings, newFacts
}

func (ce *CheckedEdit) reject(reason string) {
	ce.Status, ce.Reason = EditRejected, reason
	ce.Fragment = nil
}

func (src sources) checkEdit(ce *CheckedEdit, e Edit) {
	b, ok := src.blocks[ce.BlockID]
	if !ok {
		ce.reject("the block " + ce.BlockID + " is not in the document")
		return
	}
	ce.SectionUID, ce.BlockText = b.SectionUID, b.Text
	if !src.scope[ce.BlockID] {
		ce.reject("the block is outside what the request covered")
		return
	}
	if b.Pending {
		ce.reject("the block is itself a pending suggestion; decide it first")
		return
	}
	if utf8.RuneCountInString(e.ReplacementMarkdown) > maxReplacementRunes {
		ce.reject(fmt.Sprintf("the replacement is over %d characters", maxReplacementRunes))
		return
	}

	anchor := func(required bool) bool {
		if strings.TrimSpace(e.Quote) == "" {
			if required {
				ce.reject("the edit does not quote the text it changes")
				return false
			}
			ce.Quote, ce.WholeBlock = b.Text, true
			return true
		}
		from, to, reason := findQuote(b.Text, e.Quote)
		if reason != "" {
			ce.reject(reason)
			return false
		}
		if b.overlapsPending(from, to) {
			ce.reject("the quoted text overlaps a pending suggestion")
			return false
		}
		ce.Quote = b.Text[from:to]
		ce.WholeBlock = strings.TrimSpace(ce.Quote) == strings.TrimSpace(b.Text)
		return true
	}

	switch ce.Op {
	case OpReplace:
		if !anchor(true) {
			return
		}
		blocks, err := policystudio.RestrictedMarkdown(e.ReplacementMarkdown, src.newID)
		if err != nil {
			ce.reject("the replacement is not allowed: " + err.Error())
			return
		}
		if len(blocks) == 1 && blocks[0].Type == "paragraph" {
			ce.Fragment, ce.Inline = blocks[0].Content, true
			if len(ce.Fragment) == 0 {
				ce.reject("the replacement is empty; a removal is a delete")
				return
			}
			if b.Type == "sectionHeading" && !plainText(ce.Fragment) {
				ce.reject("a section heading takes plain text only")
			}
			return
		}
		if !ce.WholeBlock || b.Type != "paragraph" {
			ce.reject("only a whole paragraph can be replaced by lists or several paragraphs")
			return
		}
		ce.Fragment = blocks
	case OpDelete:
		if !anchor(true) {
			return
		}
		if ce.WholeBlock && b.Type == "sectionHeading" {
			ce.reject("a section is removed from the outline, not by deleting its heading")
		}
	case OpInsertAfter, OpInsertBefore:
		if b.Type == "sectionHeading" && ce.Op == OpInsertBefore {
			ce.reject("nothing can go before a section's heading")
			return
		}
		blocks, err := policystudio.RestrictedMarkdown(e.ReplacementMarkdown, src.newID)
		if err != nil {
			ce.reject("the inserted text is not allowed: " + err.Error())
			return
		}
		ce.Fragment, ce.Quote = blocks, ""
	case OpComment:
		if ce.Rationale == "" {
			ce.reject("a comment edit needs its comment in the rationale")
			return
		}
		anchor(false)
	}
}

func plainText(nodes []*policystudio.Node) bool {
	for _, n := range nodes {
		if !n.IsText() || len(n.Marks) > 0 {
			return false
		}
	}
	return true
}

func walkNodes(nodes []*policystudio.Node, fn func(*policystudio.Node)) {
	for _, n := range nodes {
		fn(n)
		walkNodes(n.Content, fn)
	}
}

func factKeys(nodes []*policystudio.Node) []string {
	var out []string
	walkNodes(nodes, func(n *policystudio.Node) {
		if n.Type == "factToken" {
			out = append(out, n.Attr("key"))
		}
	})
	return out
}

func controlRefs(nodes []*policystudio.Node) []string {
	var out []string
	walkNodes(nodes, func(n *policystudio.Node) {
		if n.Type == "controlRef" {
			out = append(out, strings.ToUpper(n.Attr("controlId")))
		}
	})
	return out
}

// checkCitation looks the cited record up and checks the quote against it.
func (src sources) checkCitation(c Citation) CheckedCitation {
	out := CheckedCitation{Citation: Citation{Kind: c.Kind, Ref: strings.TrimSpace(c.Ref), Quote: strings.TrimSpace(c.Quote)}}
	var body string
	switch c.Kind {
	case "client_fact":
		v, ok := src.facts[out.Ref]
		out.Known, body = ok, v
	case "source_document":
		n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(out.Ref), "S"))
		text, ok := src.source[n]
		out.Known, body = ok && err == nil, text
	default:
		kind := map[string]string{"control": knowledge.KindControl, "regulation_clause": knowledge.KindRegulationClause,
			"policy_clause": knowledge.KindPolicyClause, "nfr": knowledge.KindNFR}[c.Kind]
		ref := out.Ref
		if c.Kind == "control" {
			ref = strings.ToUpper(ref)
		}
		item, ok := src.lookup(kind, ref)
		out.Known, body = ok, item.Title+"\n"+item.Summary+"\n"+item.Body
	}
	if !out.Known {
		out.Note = "not found in this installation"
		out.Quote = ""
		return out
	}
	if out.Quote == "" {
		return out
	}
	if strings.Contains(normalize(body).s, normalize(out.Quote).s) {
		out.Verified = true
		return out
	}
	out.Quote, out.Note = "", "the quote does not appear in the cited record, so it was removed"
	return out
}
