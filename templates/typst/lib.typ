// lib.typ — the shared chassis both templates sit on: page furniture, cover
// page, document-control block, revision history and table styling.
//
// Everything visual is driven by brand.typ. This file decides *structure*;
// brand.typ decides *appearance*. Keeping that split means rebranding never
// requires reading this file.

#import "brand.typ": *

// ---------------------------------------------------------------------------
// Page furniture
// ---------------------------------------------------------------------------

// running-header carries the classification marking and the document
// reference. Security documents are marked on every page, not only the cover:
// a page that gets separated from its document has to carry its own handling
// instruction, which is the whole point of the marking.
#let running-header(reference: "", title: "", classification: "") = context {
  // The cover page is its own layout and gets no running header.
  if counter(page).get().first() <= 1 { return }
  set text(font: brand.sans, size: brand.micro-size, fill: brand.muted)
  grid(
    columns: (1fr, auto),
    align: (left, right),
    [#reference #if title != "" [ · #title ]],
    text(fill: classification-colour(classification), weight: "medium")[
      #upper(classification)
    ],
  )
  v(-0.5em)
  line(length: 100%, stroke: 0.5pt + brand.rule)
}

// running-footer carries provenance and "Page X of Y". The total matters:
// "Page 4" tells a reader nothing about whether they are holding the whole
// document, which is exactly what someone reviewing a printed policy needs.
#let running-footer(classification: "", reference: "") = context {
  if counter(page).get().first() <= 1 { return }
  line(length: 100%, stroke: 0.5pt + brand.rule)
  v(-0.3em)
  set text(font: brand.sans, size: brand.micro-size, fill: brand.muted)
  grid(
    columns: (1fr, auto, 1fr),
    align: (left, center, right),
    brand.footer-note,
    text(fill: classification-colour(classification), weight: "medium")[
      #upper(classification)
    ],
    [Page #counter(page).display("1 of 1", both: true)],
  )
}

// ---------------------------------------------------------------------------
// Tables
// ---------------------------------------------------------------------------

// data-table is the house table style: hairline rules, a tinted header row and
// zebra striping. Zebra rather than full gridlines because a dense compliance
// table with every cell boxed reads as noise; the stripe carries the eye across
// a wide row without adding ink.
#let data-table(columns: (), header: (), rows: (), align-spec: none) = {
  let cells = ()
  for h in header {
    cells.push(text(font: brand.sans, size: brand.small-size, weight: "bold")[#h])
  }
  for r in rows {
    for c in r { cells.push(text(size: brand.small-size)[#c]) }
  }
  table(
    columns: columns,
    align: if align-spec == none { left + top } else { align-spec },
    inset: (x: 7pt, y: 5.5pt),
    stroke: (x, y) => (
      top: if y == 0 { 0.8pt + brand.accent } else { 0.4pt + brand.rule },
      bottom: 0.4pt + brand.rule,
      left: none, right: none,
    ),
    fill: (x, y) => {
      if y == 0 { brand.table-head }
      else if calc.odd(y) { brand.table-zebra }
      else { none }
    },
    ..cells,
  )
}

// kv-table renders a two-column label/value block — the document-control table.
#let kv-table(pairs) = {
  let cells = ()
  for p in pairs {
    cells.push(text(font: brand.sans, size: brand.small-size, weight: "medium", fill: brand.muted)[#p.at(0)])
    cells.push(text(size: brand.small-size)[#if p.at(1) == none or p.at(1) == "" { "—" } else { p.at(1) }])
  }
  table(
    columns: (38mm, 1fr),
    inset: (x: 7pt, y: 5pt),
    stroke: (x, y) => (top: 0.4pt + brand.rule, bottom: 0.4pt + brand.rule, left: none, right: none),
    fill: (x, y) => if calc.odd(y) { brand.table-zebra } else { none },
    ..cells,
  )
}

// ---------------------------------------------------------------------------
// Cover page
// ---------------------------------------------------------------------------

// cover-page renders the title page. The layout is a top identity band, a large
// title anchored roughly a third down the page, and the control metadata pinned
// to the bottom — so the eye lands on the title, not on a wall of fields.
#let cover-page(
  title: "",
  subtitle: none,
  reference: "",
  version: "",
  classification: "Internal",
  status: none,
  client: none,
  fields: (),
) = {
  block(width: 100%)[
    #wordmark(size: 12pt)
    #v(-0.2em)
    #text(font: brand.sans, size: brand.micro-size, fill: brand.muted, tracking: 0.6pt)[
      #upper(brand.firm-tagline)
    ]
  ]

  v(1fr)

  // The accent rule above the title is the only decorative element on the page.
  line(length: 46mm, stroke: 2.5pt + brand.accent)
  v(0.6em)

  block(width: 100%)[
    #set par(justify: false, leading: 0.42em)
    #text(font: brand.sans, size: 30pt, weight: "bold", fill: brand.ink)[#title]
    #if subtitle != none [
      #v(0.5em)
      #text(font: brand.sans, size: 13pt, weight: "regular", fill: brand.muted)[#subtitle]
    ]
  ]

  if client != none and client != "" {
    v(1.1em)
    label-text("Prepared for")
    v(-0.35em)
    text(size: 12pt)[#client]
  }

  v(0.8em)
  grid(
    columns: (auto, auto, auto),
    column-gutter: 7pt,
    pill(classification, colour: classification-colour(classification)),
    if status != none and status != "" { pill(status) } else { [] },
    if version != "" { pill(version) } else { [] },
  )

  v(2fr)

  line(length: 100%, stroke: 0.5pt + brand.rule)
  v(0.7em)
  // Cover metadata runs in a flexible grid so a template can pass three fields
  // or nine without the layout needing to know which.
  grid(
    columns: (1fr, 1fr, 1fr),
    column-gutter: 10pt,
    row-gutter: 11pt,
    ..fields.map(f => field(f.at(0), f.at(1))),
  )

  pagebreak()
}

// ---------------------------------------------------------------------------
// Document body helpers
// ---------------------------------------------------------------------------

// section-block renders one numbered section with an optional trailing note
// (used for the "Satisfies:" control mapping line).
#let section-block(heading-text, body, note: none) = {
  heading(level: 1, heading-text)
  body
  if note != none {
    v(0.3em)
    block(
      width: 100%,
      inset: (left: 8pt),
      stroke: (left: 1.5pt + brand.accent),
      text(font: brand.sans, size: brand.micro-size, fill: brand.muted)[#note],
    )
  }
}

// callout is a boxed aside — used for executive summaries and scope notes.
#let callout(title: none, body) = block(
  width: 100%,
  inset: 10pt,
  radius: 2pt,
  fill: brand.table-zebra,
  stroke: (left: 2.5pt + brand.accent, rest: 0.4pt + brand.rule),
)[
  #if title != none [
    #label-text(title)
    #v(-0.2em)
  ]
  #body
]

// approval-block renders the signature area. Real signature lines matter: a
// policy that cannot be wet-signed is one a client cannot put in an audit file.
#let approval-block(approver: "", owner: "", effective: "") = block(breakable: false)[
  #heading(level: 1, "Approval")
  #v(0.3em)
  #grid(
    columns: (1fr, 1fr),
    column-gutter: 16mm,
    row-gutter: 3pt,
    [#label-text("Approved by") #v(-0.3em) #text(size: brand.small-size)[#approver]],
    [#label-text("Document owner") #v(-0.3em) #text(size: brand.small-size)[#owner]],
    [#v(9mm) #line(length: 100%, stroke: 0.5pt + brand.ink) #label-text("Signature")],
    [#v(9mm) #line(length: 100%, stroke: 0.5pt + brand.ink) #label-text("Date")],
  )
  #v(0.6em)
  #text(size: brand.micro-size, fill: brand.muted)[
    Effective from #if effective == "" { "the date of signature" } else { effective }.
  ]
]

// ---------------------------------------------------------------------------
// Base document setup, shared by both templates
// ---------------------------------------------------------------------------

#let base-document(
  title: "",
  reference: "",
  classification: "Internal",
  author: none,
  keywords: (),
  numbered: true,
  body,
) = {
  // The PDF metadata title is not cosmetic: PDF/UA-1 conformance fails without
  // it, and it is what a reader's tab and file manager show.
  set document(title: title, author: if author == none { brand.firm } else { author }, keywords: keywords)

  set page(
    paper: brand.paper,
    margin: brand.margin,
    header: running-header(reference: reference, title: title, classification: classification),
    footer: running-footer(classification: classification, reference: reference),
  )

  set text(font: brand.serif, size: brand.body-size, fill: brand.ink, lang: "en")
  set par(justify: true, leading: 0.68em, spacing: 1.1em, first-line-indent: 0pt)

  // Widow and orphan control. A single line of a paragraph stranded at a page
  // break is the most visible difference between a typeset document and one
  // that came out of a word processor with default settings.
  set block(breakable: true)
  show par: set block(sticky: false)

  // A `set` rule inside an `if` block only applies within that block, so this
  // has to be a conditional value rather than a conditional set — otherwise the
  // numbering silently never applies. Numbered sections are not cosmetic here:
  // auditors and exception records cite policy text by section number.
  set heading(numbering: if numbered { "1.1" } else { none })

  show heading.where(level: 1): it => block(above: 1.5em, below: 0.7em, breakable: false)[
    #set text(font: brand.sans, size: 13pt, weight: "bold", fill: brand.ink)
    #it
  ]
  show heading.where(level: 2): it => block(above: 1.1em, below: 0.5em, breakable: false)[
    #set text(font: brand.sans, size: 11pt, weight: "bold", fill: brand.ink)
    #it
  ]
  show heading.where(level: 3): it => block(above: 0.9em, below: 0.4em, breakable: false)[
    #set text(font: brand.sans, size: 10pt, weight: "bold", fill: brand.muted)
    #it
  ]

  show link: set text(fill: brand.accent-dark)
  show raw: set text(font: brand.mono, size: 9.5pt)

  set list(indent: 8pt, spacing: 0.75em, marker: text(fill: brand.accent)[•])
  set enum(indent: 8pt, spacing: 0.75em)

  body
}

// toc renders a table of contents with a rule under the title.
#let toc(depth: 2) = {
  block(breakable: false)[
    #text(font: brand.sans, size: 13pt, weight: "bold")[Contents]
    #v(0.2em)
    #line(length: 100%, stroke: 0.8pt + brand.accent)
  ]
  v(0.6em)
  outline(title: none, depth: depth, indent: 1.1em)
  pagebreak()
}
