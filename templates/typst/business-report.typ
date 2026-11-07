// business-report.typ — a consulting deliverable: assessment reports, gap
// analyses, control reviews and management reports.
//
// The structure follows what the professional-services literature consistently
// describes, and one principle drives the whole layout: a deliverable must be
// self-contained. It has to be readable and actionable by someone who was not
// in the room when it was presented, which is why every finding carries its own
// observation, risk and recommendation rather than relying on a verbal briefing
// to join them up.
//
// Build:
//   typst compile --root .. --input data=../samples/report-sample.json \
//                 business-report.typ ../out/report.pdf

#import "brand.typ": *
#import "lib.typ": *

#let source = sys.inputs.at("data", default: "../samples/report-sample.json")
#let doc = json(source)
#let get(key, fallback: "") = doc.at(key, default: fallback)

// Severity ordering and colour. Ratings are shown as outlined pills, never as
// filled red blocks: a page of solid colour reads as alarmist and clients stop
// distinguishing between the findings that matter and the ones that don't.
#let severity-rank = ("critical": 0, "high": 1, "medium": 2, "low": 3, "informational": 4)
#let severity-colour(sev) = {
  let s = lower(sev)
  if s == "critical" or s == "high" { brand.risk }
  else if s == "medium" { brand.warn }
  else if s == "low" { brand.ok }
  else { brand.muted }
}

#let findings = get("findings", fallback: ()).sorted(
  key: f => severity-rank.at(lower(f.at("severity", default: "low")), default: 9),
)

#let count-of(sev) = findings.filter(f => lower(f.at("severity", default: "")) == sev).len()

// section-tables sets the optional tables a section or appendix carries after
// its body: {caption, columns, rows}, every cell a string placed as data. A
// column of short values keeps its natural width; columns of running text
// share the rest in proportion to their longest cell. An auto-width text
// column would take the measure and crush its neighbours to a letter wide.
#let section-tables(s) = for t in s.at("tables", default: ()) {
  let cols = t.at("columns", default: ())
  if cols.len() == 0 { continue }
  let rows = t.at("rows", default: ()).map(r => range(cols.len()).map(i => r.at(i, default: "")))
  let width(i) = rows.fold(cols.at(i).len(), (m, r) => calc.max(m, r.at(i).len()))
  block(above: 0.9em, below: 0.9em)[
    #set par(justify: false)
    #if t.at("caption", default: "") != "" [
      #label-text(t.at("caption"))
      #v(-0.25em)
    ]
    #data-table(
      columns: range(cols.len()).map(i => if width(i) > 20 { calc.min(width(i), 90) * 1fr } else { auto }),
      header: cols,
      rows: rows,
    )
  ]
}

// finding-part prints one labelled part of a finding, or nothing when the
// payload leaves it empty: a finding raised from a test has an observation
// before it has a recommendation.
#let finding-part(label, value) = if value != none and value != "" [
  #v(0.4em)
  #label-text(label)
  #v(-0.2em)
  #value
]

#show: base-document.with(
  title: get("title"),
  reference: get("reference"),
  classification: get("classification", fallback: "Confidential"),
  keywords: get("frameworks", fallback: ()),
)

// ---------------------------------------------------------------------------
// Cover
// ---------------------------------------------------------------------------

#cover-page(
  title: get("title"),
  subtitle: get("subtitle", fallback: none),
  reference: get("reference"),
  version: get("version"),
  classification: get("classification", fallback: "Confidential"),
  client: get("client_name"),
  fields: (
    ("Reference", get("reference")),
    ("Engagement", get("engagement")),
    ("Version", get("version")),
    ("Prepared by", get("prepared_by")),
    ("Reviewed by", get("reviewed_by")),
    ("Report date", get("report_date")),
    ("Assessment period", get("period")),
    ("Frameworks", get("frameworks", fallback: ()).join(", ")),
    ("Distribution", get("distribution")),
  ),
)

// ---------------------------------------------------------------------------
// Executive summary
// ---------------------------------------------------------------------------

#heading(level: 1, numbering: none, outlined: true)[Executive Summary]

#get("executive_summary")

#v(0.8em)

// The severity profile goes immediately under the summary. An executive reads
// the count before the narrative, and burying it behind the methodology is the
// most common way a consulting report loses its reader.
#if findings.len() > 0 [
  #block(breakable: false)[
    #label-text("Findings by severity")
    #v(0.4em)
    #grid(
      columns: (1fr, 1fr, 1fr, 1fr, 1fr),
      column-gutter: 8pt,
      // "informational" is abbreviated in the tile: at this width the full word
      // hyphenates to "INFORMA-TIONAL", and a broken word in a summary tile is
      // the first thing a reader notices on the page.
      ..(
        ("critical", "Critical"), ("high", "High"), ("medium", "Medium"),
        ("low", "Low"), ("informational", "Info"),
      ).map(pair => block(
        width: 100%, inset: (x: 8pt, y: 7pt), radius: 2pt,
        stroke: 0.5pt + brand.rule,
      )[
        #text(font: brand.sans, size: 19pt, weight: "bold", fill: severity-colour(pair.at(0)))[
          #str(count-of(pair.at(0)))
        ]
        #v(-0.45em)
        #text(
          font: brand.sans, size: brand.micro-size, fill: brand.muted,
          tracking: 0.5pt, hyphenate: false,
        )[#upper(pair.at(1))]
      ]),
    )
  ]
]

#if get("key_messages", fallback: ()).len() > 0 [
  #v(1em)
  #callout(title: "Key messages")[
    #for m in get("key_messages", fallback: ()) [
      - #m
    ]
  ]
]

#pagebreak()
#toc(depth: 2)

// ---------------------------------------------------------------------------
// Narrative sections (scope, approach, conclusions)
// ---------------------------------------------------------------------------

#for s in get("sections", fallback: ()) {
  heading(level: 1, s.at("heading", default: ""))
  s.at("body", default: "")
  section-tables(s)
}

// ---------------------------------------------------------------------------
// Findings
// ---------------------------------------------------------------------------

#if findings.len() > 0 [
  #pagebreak()
  #heading(level: 1)[Findings]

  #data-table(
    columns: (auto, 1fr, auto, auto),
    header: ("Ref", "Finding", "Severity", "Control"),
    rows: findings.map(f => (
      raw(f.at("id", default: "")),
      f.at("title", default: ""),
      text(fill: severity-colour(f.at("severity", default: "")), weight: "medium")[
        #upper(f.at("severity", default: ""))
      ],
      raw(f.at("control_id", default: "—")),
    )),
  )

  #v(0.5em)

  // Each finding is a self-contained block: an assessor, a remediation owner
  // and an auditor each read a different part of it, and none of them should
  // have to reconstruct context from elsewhere in the document.
  #for f in findings [
    #block(breakable: false, above: 1.4em)[
      #heading(level: 2, f.at("id", default: "") + " " + f.at("title", default: ""))
      #v(-0.3em)
      #grid(
        columns: (auto, auto, 1fr),
        column-gutter: 7pt,
        pill(f.at("severity", default: ""), colour: severity-colour(f.at("severity", default: ""))),
        if f.at("control_id", default: "") != "" { pill(f.at("control_id", default: "")) } else { [] },
        if f.at("status", default: "") != "" { pill(f.at("status", default: "")) } else { [] },
      )
    ]

    #v(0.1em)
    #finding-part("Observation", f.at("observation", default: ""))
    #finding-part("Risk", f.at("risk", default: ""))
    #finding-part("Recommendation", f.at("recommendation", default: ""))

    #if f.at("management_response", default: "") != "" [
      #v(0.5em)
      #callout(title: "Management response")[
        #f.at("management_response", default: "")
        #if f.at("owner", default: "") != "" or f.at("target_date", default: "") != "" [
          #v(0.4em)
          #text(size: brand.micro-size, fill: brand.muted)[
            Owner: #f.at("owner", default: "—") #h(1em) Target date: #f.at("target_date", default: "—")
          ]
        ]
      ]
    ]
  ]
]

// ---------------------------------------------------------------------------
// Appendices
// ---------------------------------------------------------------------------

#let appendices = get("appendices", fallback: ())
#if appendices.len() > 0 [
  #pagebreak()
  // Appendices are lettered, not numbered, so a cross-reference to "Appendix B"
  // never collides with a reference to section 2.
  #set heading(numbering: "A.1")
  #counter(heading).update(0)
  #for a in appendices [
    #heading(level: 1, a.at("heading", default: ""))
    #a.at("body", default: "")
    #section-tables(a)
  ]
]

// ---------------------------------------------------------------------------
// Limitations — a required element of a professional deliverable
// ---------------------------------------------------------------------------

#if get("limitations") != "" [
  #v(1.2em)
  #block(breakable: false)[
    #set heading(numbering: none)
    #heading(level: 1, outlined: true)[Limitations and Basis of Reporting]
    #text(size: brand.small-size, fill: brand.muted)[#get("limitations")]
  ]
]
