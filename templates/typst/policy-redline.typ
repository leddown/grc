// policy-redline.typ — what changed between two revisions of a policy
// document: an approved version and the current text, or two approved
// versions. Input is the comparison internal/policydocs computes (see
// DOCUMENT_TEMPLATES.md, "Redline"): sections aligned by heading, text units
// with their word-level changes. Every string is placed as data, never read as
// markup, like the policy template's blocks.
//
// Build:
//   typst compile --input data=../samples/redline-sample.json \
//                 policy-redline.typ ../out/redline.pdf

#import "brand.typ": *
#import "lib.typ": *

#let source = sys.inputs.at("data", default: "../samples/redline-sample.json")
#let cmp = json(source)
#let get(key, fallback: "") = cmp.at(key, default: fallback)

#let added-colour = rgb("#1f6f3f")
#let removed-colour = rgb("#a3261b")

#let ins(t) = underline(stroke: 0.6pt + added-colour, offset: 1.5pt, text(fill: added-colour, t))
#let del(t) = strike(stroke: 0.6pt + removed-colour, text(fill: removed-colour, t))

#let side(r) = {
  let label = r.at("label", default: "")
  let at = r.at("approved_at", default: "")
  let by = r.at("approved_by", default: "")
  if at != "" { label + ", approved " + at.slice(0, calc.min(10, at.len())) + (if by != "" { " by " + by } else { "" }) } else { label }
}

#show: base-document.with(
  title: "Changes to " + get("title"),
  reference: get("reference"),
  classification: get("classification", fallback: "Internal"),
  numbered: false,
)

#block(breakable: false)[
  #text(font: brand.sans, size: 18pt, weight: "bold", fill: brand.ink)[#get("title")]
  #v(0.3em)
  #text(font: brand.sans, size: 11pt, fill: brand.muted)[Changes from #side(get("from")) to #side(get("to"))]
  #v(0.6em)
  #text(font: brand.sans, size: 9.5pt)[
    #str(get("changed", fallback: 0)) section(s) changed, #str(get("added", fallback: 0)) added, #str(get("removed", fallback: 0)) removed.
    Inserted text is #ins("underlined")\; removed text is #del("struck through").
  ]
  #if not get("from").at("verified", default: true) [
    #v(0.4em)
    #text(font: brand.sans, size: 9.5pt, fill: removed-colour)[The earlier version no longer matches the hash recorded when it was approved.]
  ]
]

#let unit(u) = {
  let op = u.at("op")
  if op == "same" {
    par(text(fill: brand.muted, u.at("after", default: u.at("before", default: ""))))
  } else if op == "added" {
    par(ins(u.at("after", default: "")))
  } else if op == "removed" {
    par(del(u.at("before", default: "")))
  } else {
    par({
      for w in u.at("words", default: ()) {
        let t = w.at("text", default: "")
        if w.at("op") == "added" { ins(t) } else if w.at("op") == "removed" { del(t) } else { t }
      }
    })
  }
}

#for s in get("sections", fallback: ()) {
  let op = s.at("op")
  let note = if op == "added" { " (new section)" } else if op == "removed" { " (removed)" } else if s.at("heading_before", default: "") != "" { " (was: " + s.at("heading_before") + ")" } else if op == "same" { " (no changes)" } else { "" }
  heading(level: 1, s.at("heading") + note)
  if op != "same" {
    for u in s.at("units", default: ()) { unit(u) }
  }
}
