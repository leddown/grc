// Package policyai is the Policy Studio's AI proposal engine: it builds the
// context for a request, asks the configured provider for an answer in the
// EditProposal contract, validates every edit against the live document, and
// records the proposal. The AI never writes the document: an edit the engine
// accepts is placed by the requester's editor as a suggestion attributed to
// the AI, which a person accepts or rejects. See POLICY_STUDIO.md.
package policyai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// schemaJSON is the answer contract (the brief's Appendix A). It is sent with
// the request, and every answer is validated against it here, since no
// provider is trusted to have kept to it.
const schemaJSON = `{
  "type": "object", "additionalProperties": false, "required": ["answer_markdown", "proposal"],
  "properties": {
    "answer_markdown": { "type": "string" },
    "proposal": { "anyOf": [ { "type": "null" }, {
      "type": "object", "additionalProperties": false, "required": ["summary", "edits", "control_mappings", "new_facts"],
      "properties": {
        "summary": { "type": "string" },
        "edits": { "type": "array", "items": {
          "type": "object", "additionalProperties": false,
          "required": ["op", "block_id", "quote", "replacement_markdown", "rationale", "citations"],
          "properties": {
            "op": { "type": "string", "enum": ["replace", "insert_after", "insert_before", "delete", "comment"] },
            "block_id": { "type": "string" }, "quote": { "type": "string" },
            "replacement_markdown": { "type": "string" }, "rationale": { "type": "string" },
            "citations": { "type": "array", "items": {
              "type": "object", "additionalProperties": false, "required": ["kind", "ref", "quote"],
              "properties": {
                "kind": { "type": "string", "enum": ["control", "regulation_clause", "policy_clause", "nfr", "client_fact", "source_document"] },
                "ref": { "type": "string" }, "quote": { "type": "string" } } } } } } },
        "control_mappings": { "type": "array", "items": {
          "type": "object", "additionalProperties": false, "required": ["section_uid", "control_id", "coverage", "rationale"],
          "properties": {
            "section_uid": { "type": "string" }, "control_id": { "type": "string" },
            "coverage": { "type": "string", "enum": ["full", "partial", "supporting"] },
            "rationale": { "type": "string" } } } },
        "new_facts": { "type": "array", "items": {
          "type": "object", "additionalProperties": false, "required": ["key", "description"],
          "properties": { "key": { "type": "string" }, "description": { "type": "string" } } } } } } ] } } }`

// Schema returns the contract as a JSON Schema value, a fresh copy each call.
func Schema() map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &out); err != nil {
		panic(err)
	}
	return out
}

// Edit operations.
const (
	OpReplace      = "replace"
	OpInsertAfter  = "insert_after"
	OpInsertBefore = "insert_before"
	OpDelete       = "delete"
	OpComment      = "comment"
)

var ops = map[string]bool{OpReplace: true, OpInsertAfter: true, OpInsertBefore: true, OpDelete: true, OpComment: true}

var citationKinds = map[string]bool{"control": true, "regulation_clause": true, "policy_clause": true, "nfr": true, "client_fact": true, "source_document": true}

// Answer is the whole contract.
type Answer struct {
	AnswerMarkdown string    `json:"answer_markdown"`
	Proposal       *Proposal `json:"proposal"`
}

// Proposal is a set of edits and mapping suggestions.
type Proposal struct {
	Summary         string           `json:"summary"`
	Edits           []Edit           `json:"edits"`
	ControlMappings []ControlMapping `json:"control_mappings"`
	NewFacts        []NewFact        `json:"new_facts"`
}

// Edit is one proposed change, anchored on a block and a quote from it.
type Edit struct {
	Op                  string     `json:"op"`
	BlockID             string     `json:"block_id"`
	Quote               string     `json:"quote"`
	ReplacementMarkdown string     `json:"replacement_markdown"`
	Rationale           string     `json:"rationale"`
	Citations           []Citation `json:"citations"`
}

// Citation is a source an edit relies on.
type Citation struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Quote string `json:"quote"`
}

// ControlMapping proposes that a section satisfies a control.
type ControlMapping struct {
	SectionUID string `json:"section_uid"`
	ControlID  string `json:"control_id"`
	Coverage   string `json:"coverage"`
	Rationale  string `json:"rationale"`
}

// NewFact is a client fact the text needs and the client profile lacks.
type NewFact struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// errNotContract is wrapped by every parse failure, so a caller can tell a
// malformed answer (worth one repair turn) from anything else.
var errNotContract = errors.New("not the contract")

// parseAnswer is the gate every answer passes, whichever provider wrote it:
// exactly one JSON object, no unknown fields, both top-level keys present,
// every required field present, and enums from their sets (compared without
// regard to case, which models do not always keep).
func parseAnswer(text string) (Answer, error) {
	text = strings.TrimSpace(text)
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	var a Answer
	if err := dec.Decode(&a); err != nil {
		return Answer{}, fmt.Errorf("%w: %v", errNotContract, err)
	}
	if dec.More() {
		return Answer{}, fmt.Errorf("%w: there is text after the JSON object", errNotContract)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &top); err != nil {
		return Answer{}, fmt.Errorf("%w: %v", errNotContract, err)
	}
	for _, k := range []string{"answer_markdown", "proposal"} {
		if _, ok := top[k]; !ok {
			return Answer{}, fmt.Errorf("%w: %s is missing", errNotContract, k)
		}
	}
	if a.Proposal == nil {
		return a, nil
	}
	var prop map[string]json.RawMessage
	_ = json.Unmarshal(top["proposal"], &prop)
	for _, k := range []string{"summary", "edits", "control_mappings", "new_facts"} {
		if _, ok := prop[k]; !ok {
			return Answer{}, fmt.Errorf("%w: proposal.%s is missing", errNotContract, k)
		}
	}
	for i := range a.Proposal.Edits {
		e := &a.Proposal.Edits[i]
		e.Op = strings.ToLower(strings.TrimSpace(e.Op))
		if !ops[e.Op] {
			return Answer{}, fmt.Errorf("%w: edit %d has an unknown op %q", errNotContract, i+1, e.Op)
		}
		for j := range e.Citations {
			c := &e.Citations[j]
			c.Kind = strings.ToLower(strings.TrimSpace(c.Kind))
			if !citationKinds[c.Kind] {
				return Answer{}, fmt.Errorf("%w: edit %d cites an unknown kind %q", errNotContract, i+1, c.Kind)
			}
		}
	}
	for i := range a.Proposal.ControlMappings {
		m := &a.Proposal.ControlMappings[i]
		m.Coverage = strings.ToLower(strings.TrimSpace(m.Coverage))
		if m.Coverage != "full" && m.Coverage != "partial" && m.Coverage != "supporting" {
			return Answer{}, fmt.Errorf("%w: mapping %d has an unknown coverage %q", errNotContract, i+1, m.Coverage)
		}
	}
	return a, nil
}
