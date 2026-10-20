// letter.typ — renders a formal letter written on a client's behalf, such as
// a letter to a banking supervisor.
//
// Input is the same JSON as policy-document.typ (internal/policydocs'
// TemplateExport) for a document of type "letter". The letter's parts are
// sections found by their kind, not their headings: sender, addressee,
// letter_body, signatories, enclosures, copy_to and annex. Only an annex
// prints its heading. The client's name heads the page. The title is the subject line, effective_date the date
// and reference the sender's reference. See templates/samples/letter-sample.json.
//
// Build:
//   typst compile --input data=../samples/letter-sample.json \
//                 letter.typ ../out/letter.pdf
//
// The page is the client's, not the firm's: no wordmark, no firm footer. The
// brand contributes only type, colour and paper.

#import "brand.typ": *
#import "lib.typ": render-blocks, render-runs

#let source = sys.inputs.at("data", default: "../samples/letter-sample.json")
#let doc = json(source)

#let get(key, fallback: "") = doc.at(key, default: fallback)

#let sections = get("sections", fallback: ())
#let of-kind(k) = sections.filter(s => s.at("section_kind", default: "") == k)

// The kinds laid out in a place of their own. A section of any other kind is
// part of the body, in document order, so nothing written in a letter is
// left off the page.
#let placed = ("sender", "addressee", "signatories", "enclosures", "copy_to", "annex")

#let classification = get("classification", fallback: "Internal")
#let final = get("status") == "approved"

// body-of sets a section from its blocks, or from its plain body for a letter
// written in the section editor: a blank line between paragraphs, a newline
// within one.
#let body-of(s) = {
  let blocks = s.at("blocks", default: ())
  if blocks.len() > 0 {
    render-blocks(blocks)
  } else {
    for p in s.at("body", default: "").split("\n\n") {
      if p.trim() != "" { par(p.trim().split("\n").map(l => [#l]).join(linebreak())) }
    }
  }
}

#let months = ("January", "February", "March", "April", "May", "June", "July",
  "August", "September", "October", "November", "December")

// long-date writes 2026-10-28 as 28 October 2026. Anything else is printed
// as given.
#let long-date(iso) = {
  if iso.match(regex("^\d{4}-\d{2}-\d{2}$")) == none { return iso }
  let parts = iso.split("-")
  let month = int(parts.at(1))
  if month < 1 or month > 12 { return iso }
  str(int(parts.at(2))) + " " + months.at(month - 1) + " " + parts.at(0)
}

#set document(
  title: get("title"),
  author: if get("client_name") == "" { () } else { get("client_name") },
)

// The marking is on every page, the first included: a page separated from
// its letter keeps its handling instruction. A letter that is not approved
// says so on every page, so a draft cannot be sent by mistake.
#set page(
  paper: brand.paper,
  margin: (left: 25mm, right: 20mm, top: 24mm, bottom: 24mm),
  header: context {
    set text(font: brand.sans, size: brand.micro-size, fill: brand.muted)
    set par(justify: false)
    let first = counter(page).get().first() == 1
    grid(
      columns: (1fr, auto),
      align: (left, right),
      if first { [] } else [#get("reference")#if get("reference") != "" [ · ]#get("title")],
      [
        #if not final { text(fill: brand.risk, weight: "bold", tracking: 0.6pt)[DRAFT — NOT APPROVED · ] }
        #text(fill: classification-colour(classification), weight: "medium", tracking: 0.6pt)[#upper(classification)]
      ],
    )
  },
  footer: context {
    let total = counter(page).final().first()
    if total > 1 {
      set text(font: brand.sans, size: brand.micro-size, fill: brand.muted)
      align(right)[Page #counter(page).display("1 of 1", both: true)]
    }
  },
)

#set text(font: brand.serif, size: brand.body-size, fill: brand.ink, lang: "en")
#set par(justify: true, leading: 0.65em, spacing: 1em, first-line-indent: 0pt)
#set list(indent: 6pt, spacing: 0.7em, marker: [–])
#set enum(indent: 6pt, spacing: 0.7em)
#show table: set par(justify: false)
#show link: set text(fill: brand.accent-dark)
#show raw: set text(font: brand.mono, size: 9.5pt)
#show heading.where(level: 1): it => block(above: 0em, below: 1em, breakable: false)[
  #set text(font: brand.sans, size: 13pt, weight: "bold")
  #it.body
]
#show heading.where(level: 2): it => block(above: 1.2em, below: 0.6em, breakable: false)[
  #set text(font: brand.sans, size: 10.5pt, weight: "bold")
  #it.body
]
#show heading.where(level: 3): it => block(above: 1.1em, below: 0.5em, breakable: false)[
  #set text(font: brand.sans, size: 10pt, weight: "bold")
  #it.body
]
// A template's [TO DO] stands out in a draft; lint keeps it from approval.
#show regex("\[TO DO\b[^\]]*\]"): it => highlight(fill: brand.warn.lighten(80%), it)

// ---------------------------------------------------------------------------
// Sender
// ---------------------------------------------------------------------------

// The client's name heads the page; a sender section adds the lines under
// it: the address, and anything else the letterhead carries.
#block(width: 100%)[
  #set text(font: brand.sans, size: brand.small-size)
  #set par(justify: false, spacing: 0.45em)
  #if get("client_name") != "" { par(text(size: 11pt, weight: "bold")[#get("client_name")]) }
  #for s in of-kind("sender") { body-of(s) }
]

#v(14mm)

// ---------------------------------------------------------------------------
// Addressee, reference and date
// ---------------------------------------------------------------------------

#grid(
  columns: (1fr, 62mm),
  column-gutter: 8mm,
  block[
    #set par(justify: false, spacing: 0.45em)
    #for s in of-kind("addressee") { body-of(s) }
  ],
  align(right + bottom)[
    #set text(size: brand.small-size)
    #set par(justify: false, spacing: 0.45em)
    #if get("reference") != "" [Our reference: #get("reference") \ ]
    #if get("effective_date") == "" {
      text(fill: brand.risk, weight: "bold")[Date not set]
    } else {
      long-date(get("effective_date"))
    }
  ],
)

#v(10mm)

// ---------------------------------------------------------------------------
// Subject and body
// ---------------------------------------------------------------------------

// The subject is the letter's first heading: PDF/UA requires one of level 1
// before any other.
#[
  #show heading: it => block(above: 0em, below: 1.2em, breakable: false)[
    #set par(justify: false)
    #set text(font: brand.sans, size: 11pt, weight: "bold")
    #it.body
  ]
  #heading(level: 1, numbering: none, outlined: false, get("title"))
]

#let body-sections = sections.filter(s => s.at("section_kind", default: "") not in placed)

// The closing formula ("Yours sincerely,") is the body's last paragraph. It is
// set with the signatures, so a page break cannot part them.
#let last-blocks = if body-sections.len() > 0 { body-sections.last().at("blocks", default: ()) } else { () }
#let closing = if last-blocks.len() > 1 and last-blocks.last().at("type", default: "") == "paragraph" {
  last-blocks.last()
} else { none }

#for (i, s) in body-sections.enumerate() {
  if s.at("section_kind", default: "") != "letter_body" and s.at("heading", default: "") != "" {
    heading(level: 2, numbering: none, outlined: false, s.at("heading"))
  }
  if closing != none and i == body-sections.len() - 1 {
    render-blocks(last-blocks.slice(0, -1))
  } else {
    body-of(s)
  }
}

// ---------------------------------------------------------------------------
// Signatures
// ---------------------------------------------------------------------------

// A signatories section is a table of name and function, a header row
// skipped, or one paragraph per signatory.
#let signers = ()
#for s in of-kind("signatories") {
  let blocks = s.at("blocks", default: ())
  for b in blocks {
    let kind = b.at("type", default: "")
    if kind == "table" {
      for r in b.at("rows", default: ()) {
        if r.at("header", default: false) { continue }
        let cells = r.at("cells", default: ())
        if cells.len() == 0 { continue }
        signers.push((
          render-blocks(cells.at(0)),
          if cells.len() > 1 { render-blocks(cells.at(1)) } else { [] },
        ))
      }
    } else if kind == "paragraph" {
      signers.push((render-runs(b.at("runs", default: ())), []))
    }
  }
  if blocks.len() == 0 {
    for p in s.at("body", default: "").split("\n\n") {
      if p.trim() != "" { signers.push((p.trim().split("\n").map(l => [#l]).join(linebreak()), [])) }
    }
  }
}

#block(breakable: false)[
  #if closing != none { par(render-runs(closing.at("runs", default: ()))) }
  #if signers.len() > 0 {
    grid(
      columns: (1fr,) * calc.min(signers.len(), 2),
      column-gutter: 14mm,
      row-gutter: 6mm,
      ..signers.map(((name, role)) => block[
        #set par(justify: false, spacing: 0.35em)
        #v(16mm)
        #line(length: 85%, stroke: 0.5pt + brand.ink)
        #strong(name)
        #text(size: brand.small-size, fill: brand.muted)[#role]
      ]),
    )
  }
]

// ---------------------------------------------------------------------------
// Enclosures and copies
// ---------------------------------------------------------------------------

#let listing(label, kind) = {
  let found = of-kind(kind)
  if found.len() > 0 {
    v(6mm)
    block(breakable: false)[
      #set text(size: brand.small-size)
      #set par(justify: false, spacing: 0.45em)
      #text(font: brand.sans, weight: "bold")[#label]
      #for s in found { body-of(s) }
    ]
  }
}

#listing("Enclosures", "enclosures")
#listing("Copy to", "copy_to")

// ---------------------------------------------------------------------------
// Annexes, each from a new page
// ---------------------------------------------------------------------------

#for s in of-kind("annex") {
  pagebreak()
  heading(level: 1, numbering: none, outlined: false, s.at("heading", default: "Annex"))
  body-of(s)
}
