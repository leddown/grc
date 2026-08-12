// policy-document.typ — renders an ISMS / ISO 27001 / NIST / FedRAMP / PCI DSS
// policy, standard, procedure or work instruction.
//
// Input is a JSON file matching what `internal/policydocs` already models, so
// the same record that drives the Policy Editor drives this PDF with no
// intermediate transformation. See templates/samples/policy-sample.json.
//
// Build:
//   typst compile --input data=../samples/policy-sample.json \
//                 policy-document.typ ../out/policy.pdf
//
// The section order below is the union of what ISO 27001 clause 7.5, the NIST
// 800-53 "-1" controls and FedRAMP expect a family policy to contain. FedRAMP
// is the strictest and effectively dictates it: purpose, scope, roles,
// responsibilities, management commitment, coordination among organizational
// entities, and compliance.

#import "brand.typ": *
#import "lib.typ": *

// Data is passed with --input so one template serves every document. Falling
// back to the sample keeps `typst watch policy-document.typ` working while
// someone is editing the layout.
#let source = sys.inputs.at("data", default: "../samples/policy-sample.json")
#let doc = json(source)

#let get(key, fallback: "") = doc.at(key, default: fallback)

// The upstream model stores status and doc type as lowercase slugs; a cover
// page reading "approved" instead of "Approved" is the sort of detail that
// makes a deliverable look generated rather than authored.
#let titlecase(s) = if s == "" { s } else { upper(s.at(0)) + s.slice(1) }

#let doc-type-label = {
  let t = get("doc_type", fallback: "policy")
  if t == "work_instruction" { "Work Instruction" }
  else if t == "tra" { "Targeted Risk Analysis" }
  else { upper(t.at(0)) + t.slice(1) }
}

#let latest-version = {
  let versions = get("versions", fallback: ())
  if versions.len() > 0 { versions.at(0).at("version_label", default: "") } else { "Draft" }
}

#show: base-document.with(
  title: get("title"),
  reference: get("reference"),
  classification: get("classification", fallback: "Internal"),
  keywords: get("frameworks", fallback: ()),
)

// ---------------------------------------------------------------------------
// Cover
// ---------------------------------------------------------------------------

#cover-page(
  title: get("title"),
  subtitle: doc-type-label,
  reference: get("reference"),
  version: latest-version,
  classification: get("classification", fallback: "Internal"),
  status: titlecase(get("status")),
  client: get("client_name"),
  fields: (
    ("Reference", get("reference")),
    ("Document type", doc-type-label),
    ("Version", latest-version),
    ("Document owner", get("owner_role")),
    ("Approver", get("approver")),
    ("Effective date", get("effective_date")),
    ("Review cycle", {
      let m = get("review_cadence_months", fallback: 0)
      if m == 0 { "" } else { str(m) + " months" }
    }),
    ("Next review", get("next_review_date")),
    ("Frameworks", get("frameworks", fallback: ()).join(", ")),
  ),
)

// ---------------------------------------------------------------------------
// Document control
// ---------------------------------------------------------------------------

#heading(level: 1, numbering: none, outlined: false)[Document Control]

#kv-table((
  ("Reference", get("reference")),
  ("Title", get("title")),
  ("Document type", doc-type-label),
  ("Classification", get("classification", fallback: "Internal")),
  ("Status", titlecase(get("status"))),
  ("Document owner", get("owner_role")),
  ("Approver", get("approver")),
  ("Client", get("client_name")),
  ("Frameworks", get("frameworks", fallback: ()).join(", ")),
  ("Effective date", get("effective_date")),
  ("Next review", get("next_review_date")),
))

#if get("summary") != "" [
  #v(1em)
  #callout(title: "Summary")[#get("summary")]
]

// Revision history is a clause 7.5 requirement, not a nicety: an auditor asks
// what changed between versions and who approved it.
#let versions = get("versions", fallback: ())
#if versions.len() > 0 [
  #v(1.2em)
  #heading(level: 1, numbering: none, outlined: false)[Revision History]
  #data-table(
    columns: (auto, auto, auto, 1fr),
    header: ("Version", "Approved", "Approved by", "Change summary"),
    rows: versions.map(v => (
      v.at("version_label", default: ""),
      v.at("approved_at", default: "").slice(0, calc.min(10, v.at("approved_at", default: "").len())),
      v.at("approved_by", default: ""),
      v.at("change_summary", default: "—"),
    )),
  )
]

#pagebreak()
#toc(depth: 2)

// ---------------------------------------------------------------------------
// Body
// ---------------------------------------------------------------------------

#let sections = get("sections", fallback: ())

#for s in sections {
  let controls = s.at("controls", default: ())
  let note = if controls.len() > 0 {
    "Satisfies: " + controls.map(c => {
      let cid = c.at("control_id", default: "")
      let cov = c.at("coverage", default: "full")
      if cov == "full" { cid } else { cid + " (" + cov + ")" }
    }).join("; ")
  } else { none }

  section-block(s.at("heading", default: ""), s.at("body", default: ""), note: note)
}

// ---------------------------------------------------------------------------
// Control mapping
// ---------------------------------------------------------------------------

#let all-controls = get("controls", fallback: ())
#if all-controls.len() > 0 [
  #pagebreak()
  #heading(level: 1)[Control Mapping]

  This document is mapped to the following catalog controls. Coverage is stated
  per control: #emph[full] means this document satisfies the control on its own,
  #emph[partial] means further documents are required, and #emph[supporting]
  means the text contributes context without satisfying the control.

  #v(0.6em)
  #data-table(
    columns: (auto, 1fr, auto, auto),
    header: ("Control", "Name", "Coverage", "Section"),
    rows: all-controls.map(c => (
      raw(c.at("control_id", default: "")),
      c.at("control_name", default: "—"),
      titlecase(c.at("coverage", default: "full")),
      c.at("section_heading", default: "—"),
    )),
  )
]

// ---------------------------------------------------------------------------
// Approval
// ---------------------------------------------------------------------------

#v(1.5em)
#approval-block(
  approver: get("approver"),
  owner: get("owner_role"),
  effective: get("effective_date"),
)
