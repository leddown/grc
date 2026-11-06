// assurance-plan-pack.typ — the committee approval pack for an assurance
// plan: thirteen 16:9 slides that put the plan in front of the forum that
// approves it. Input is the plan's export with a "pack" object of what the
// slides show that is worked out rather than stored (DOCUMENT_TEMPLATES.md,
// "Assurance plan pack").
//
// A pack is read again by people who were not at the meeting, a supervisor
// among them, so each slide states its own numbers rather than leaning on a
// speaker. Every string from the payload is placed as data, never evaluated.
//
// Build:
//   typst compile --root .. --input data=../samples/assurance-plan-sample.json \
//                 assurance-plan-pack.typ ../out/assurance-plan-pack.pdf

#import "brand.typ": *
#import "lib.typ": *

#let source = sys.inputs.at("data", default: "../samples/assurance-plan-sample.json")
#let plan = json(source)
#let get(key, fallback: "") = plan.at(key, default: fallback)
#let pack = get("pack", fallback: (:))
#let classification = "Confidential"

#let months = ("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")
// day turns an ISO date into "31 Mar 2027"; anything else is printed as given.
#let day(iso) = {
  if type(iso) != str or iso.match(regex("^\d{4}-\d{2}-\d{2}")) == none { return if iso == none { "" } else { iso } }
  let m = int(iso.slice(5, 7))
  if m < 1 or m > 12 { return iso }
  str(int(iso.slice(8, 10))) + " " + months.at(m - 1) + " " + iso.slice(0, 4)
}
#let pct(x) = str(calc.round(x * 100, digits: 1)) + "%"
#let num(x) = str(calc.round(x, digits: 1))
#let first(items, n) = items.slice(0, calc.min(n, items.len()))
#let more(items, n, what) = if items.len() > n {
  text(size: brand.micro-size, fill: brand.muted)[…and #str(items.len() - n) more #what in the plan's export.]
}

#set document(title: get("reference") + " " + get("title") + " — approval pack", author: brand.firm)
#set text(font: brand.sans, size: 10pt, fill: brand.ink, lang: "en")
#set par(justify: false, leading: 0.55em, spacing: 0.8em)
#set list(indent: 4pt, spacing: 0.6em, marker: text(fill: brand.accent)[•])

#set page(
  paper: "presentation-16-9",
  margin: (x: 14mm, top: 17mm, bottom: 12mm),
  header: context {
    if counter(page).get().first() <= 1 { return }
    set text(size: brand.micro-size, fill: brand.muted)
    grid(
      columns: (auto, 1fr, auto),
      column-gutter: 8pt,
      wordmark(size: 8pt),
      align(left)[#get("reference") · #get("title")],
      text(fill: classification-colour(classification), weight: "medium")[#upper(classification)],
    )
    v(-0.45em)
    line(length: 100%, stroke: 0.4pt + brand.rule)
  },
  footer: context {
    if counter(page).get().first() <= 1 { return }
    set text(size: brand.micro-size, fill: brand.muted)
    grid(
      columns: (1fr, auto),
      [#get("function_name") · for the #get("approval_forum") · #brand.footer-note],
      [#counter(page).display("1 / 1", both: true)],
    )
  },
)

// slide starts a page with its kicker and title. The kicker numbers the
// slide's place in the argument, so a page printed on its own still says
// what it is part of.
#let slide(kicker, title, body) = {
  pagebreak(weak: true)
  label-text(kicker)
  v(-0.5em)
  text(size: 17pt, weight: "bold")[#title]
  v(-0.6em)
  line(length: 34mm, stroke: 1.2pt + brand.accent)
  v(0.3em)
  body
}

// heading-label is label-text as a block, so what follows starts a new line.
#let heading-label(body) = block(below: 0.45em, label-text(body))

#let small-table(columns: (), header: (), rows: ()) = {
  set text(size: 9pt)
  data-table(columns: columns, header: header, rows: rows)
}

#let card(title, body, tint: false) = block(
  width: 100%, height: 100%, inset: 8pt, radius: 2pt,
  fill: if tint { brand.table-zebra } else { none },
  stroke: (top: 1.6pt + brand.accent, rest: 0.4pt + brand.rule),
)[
  #text(size: 10.5pt, weight: "bold")[#title]
  #v(-0.3em)
  #body
]

#let strata = get("strata", fallback: ())
#let quarters = get("quarters", fallback: ())
#let params = get("parameters", fallback: (:))

// ---------------------------------------------------------------------------
// 1. Cover
// ---------------------------------------------------------------------------

#block(width: 100%, height: 100%)[
  #place(left + top, dx: -14mm, dy: -17mm, rect(width: 6mm, height: 100% + 29mm, fill: brand.accent))
  #wordmark(size: 12pt)
  #v(1fr)
  #label-text("Approval pack · " + get("approval_forum"))
  #v(-0.3em)
  #text(size: 24pt, weight: "bold")[#get("title")]
  #v(-0.2em)
  #text(size: 12pt, fill: brand.muted)[#get("function_name")#if get("client_name") != "" [ · #get("client_name")]]
  #v(1fr)
  #grid(
    columns: (auto, auto, auto, auto, auto),
    column-gutter: 14mm,
    field("Reference", get("reference")),
    field("Period", day(get("period_start")) + " – " + day(get("period_end"))),
    field("Plan", get("status") + if get("latest_version") != "" { ", " + get("latest_version") } else { "" }),
    field("Owner", get("owner_role")),
    field("Pack as of", day(pack.at("as_of", default: ""))),
  )
  #v(4mm)
  #text(size: brand.micro-size, fill: classification-colour(classification), weight: "medium", tracking: 0.6pt)[#upper(classification)]
]

// ---------------------------------------------------------------------------
// 2. Decisions requested
// ---------------------------------------------------------------------------

#slide("1 · Decisions requested", "What we ask the " + get("approval_forum") + " to decide")[
  #let decisions = get("decisions_requested", fallback: ())
  #if decisions.len() == 0 [
    No decisions are recorded in the plan.
  ] else [
    #grid(
      columns: (1fr,) * calc.min(decisions.len(), 3),
      column-gutter: 8pt,
      rows: 46mm,
      ..first(decisions, 3).enumerate().map(((i, d)) => card(
        text(size: 22pt, fill: brand.accent)[#str(i + 1)],
        text(size: 11pt)[#d],
        tint: true,
      )),
    )
    #more(decisions, 3, "decisions")
  ]
  #v(1fr)
  #text(size: 9pt, fill: brand.muted)[Accountable: #get("accountable_role"). Plan owner: #get("owner_role").]
]

// ---------------------------------------------------------------------------
// 3. What DORA asks of the control function
// ---------------------------------------------------------------------------

#slide("2 · Regulatory basis", "What the regulation asks, and how the plan meets it")[
  #let basis = get("regulatory_basis", fallback: ())
  #if basis.len() == 0 [
    The plan records no regulatory basis rows. It cites: #get("regulatory_refs", fallback: "no references").
  ] else [
    #small-table(
      columns: (1fr, auto, 1.25fr),
      header: ("Requirement", "Reference", "How the plan meets it"),
      rows: first(basis, 6).map(b => (b.at("requirement", default: ""), b.at("reference", default: ""), b.at("how", default: ""))),
    )
    #more(basis, 6, "rows")
  ]
]

// ---------------------------------------------------------------------------
// 4. Objective
// ---------------------------------------------------------------------------

#slide("3 · Objective", "What the year delivers")[
  #v(4mm)
  #block(inset: (left: 10pt, y: 4pt), stroke: (left: 2.5pt + brand.accent))[
    #text(font: brand.serif, size: 14pt)[#get("objective", fallback: "The plan states no objective.")]
  ]
  #v(1fr)
  #grid(
    columns: (1fr,) * 4,
    column-gutter: 8pt,
    field("Plan type", get("plan_type_name")),
    field("Period", day(get("period_start")) + " – " + day(get("period_end"))),
    field("Mobilisation from", day(get("mobilise_start"))),
    field("Annual conclusion", day(get("closing_date"))),
  )
]

// ---------------------------------------------------------------------------
// 5. Scope
// ---------------------------------------------------------------------------

#slide("4 · Scope", "What is in scope")[
  #let attrs = get("attributes", fallback: ())
  #grid(
    columns: (1.15fr, 1fr),
    column-gutter: 10mm,
    [
      #heading-label("Population by stratum")
      #small-table(
        columns: (auto, 1fr, auto),
        header: ("Stratum", "Definition", "Items"),
        rows: strata.map(s => (s.at("name", default: ""), s.at("definition", default: ""), str(s.at("n", default: 0)))),
      )
      #text(size: brand.micro-size, fill: brand.muted)[
        #if get("population_frozen_at") != "" [Population frozen on #(day(get("population_frozen_at").split("T").first()) + ";") seed commitment SHA-256 #raw(get("seed_commitment").slice(0, calc.min(16, get("seed_commitment").len())))…] else [Population not yet frozen: the counts are the planned sizes or the items loaded so far.]
      ]
    ],
    [
      #heading-label(str(attrs.len()) + " test attributes, tested on every selected item")
      #if attrs.len() == 0 [
        The plan has no test attributes yet.
      ] else [
        #let shown = first(attrs, 24)
        #let half = calc.ceil(shown.len() / 2)
        #let entry(a) = block(below: 0.45em)[#text(weight: "bold")[#a.at("attr_ref", default: "")] #a.at("domain", default: "")]
        #set text(size: 8.5pt)
        #grid(
          columns: (1fr, 1fr),
          column-gutter: 6pt,
          shown.slice(0, half).map(entry).join(),
          shown.slice(half).map(entry).join(),
        )
        #more(attrs, 24, "attributes")
      ]
    ],
  )
]

// ---------------------------------------------------------------------------
// 6. Approach
// ---------------------------------------------------------------------------

#slide("5 · Approach", "Two tracks: every item reviewed, a sample tested on evidence")[
  #grid(
    columns: (1fr, 1fr),
    column-gutter: 8pt,
    rows: 64mm,
    card("Track 1 — 100% coverage review", [
      - 1LoD asset owners review and attest every item in scope, in quarterly waves.
      - 2LoD reviews the data quality of every submission and accepts it or returns it.
      - Coverage is measured against a cumulative target per stratum at each quarter end, rated Green, Amber (within #num(params.at("rag_amber_points", default: 0)) points) or Red.
      #for s in strata [
        - #s.at("name", default: ""): waves #s.at("waves", default: ()).map(w => pct(w)).join(" / ").
      ]
    ], tint: true),
    card("Track 2 — evidence-based sample testing", [
      - Each quarter 2LoD draws a sample from the frozen population and tests every attribute on evidence.
      - Samples are sized by attribute sampling at each stratum's confidence and tolerable deviation rate, with a coverage floor and risk-based top-ups.
      - The draw is ranked by a seed committed when the population is frozen, so any selection can be re-performed.
      - Every test is QA-reviewed before its result counts; fails are raised as findings in Audit Findings.
    ]),
  )
]

// ---------------------------------------------------------------------------
// 7. Sampling and the deviation rule
// ---------------------------------------------------------------------------

#slide("6 · Sampling", "Sample sizes and the deviation rule")[
  #small-table(
    columns: (1fr, auto, auto, auto, auto, auto, auto, auto, auto, auto),
    header: ("Stratum", "N", "Confidence", "Tolerable rate", "n₀", "With FPC", "Floor", "Annual sample", "Planned tests", "Coverage"),
    rows: strata.map(s => if s.at("sampling_enabled", default: true) {
      (
        s.at("name", default: ""), str(s.at("n", default: 0)), pct(s.at("confidence", default: 0)),
        pct(s.at("tolerable_rate", default: 0)), str(s.at("n0", default: 0)), str(s.at("fpc", default: 0)),
        str(s.at("floor", default: 0)) + " (" + pct(s.at("floor_pct", default: 0)) + ")",
        str(s.at("annual_sample", default: 0)), str(s.at("total_tests", default: 0)),
        num(s.at("coverage_pct", default: 0)) + "%",
      )
    } else {
      (s.at("name", default: ""), str(s.at("n", default: 0)), "—", "—", "—", "—", "—", "Not sampled", "0", "—")
    }),
  )
  #text(size: brand.micro-size, fill: brand.muted)[
    The annual sample is the larger of the statistical size after the finite population correction and the coverage floor. Planned tests add the risk-based top-ups.
  ]
  #v(2mm)
  #let extend-at = params.at("deviation_extend_at", default: 1)
  #let thematic-at = params.at("deviation_thematic_at", default: 2)
  #grid(
    columns: (1fr, 1fr, 1fr),
    column-gutter: 8pt,
    rows: 30mm,
    card(text(fill: brand.ok)[No deviations], [Effective for the attribute in that stratum.]),
    card(text(fill: brand.warn)[#str(extend-at) deviation#if extend-at != 1 [s]], [The sample is extended by #num(params.at("extend_pct", default: 0))% in the next quarter.]),
    card(text(fill: brand.risk)[#str(thematic-at) or more], [Not effective: a thematic finding, raised to the risk register.]),
  )
  #text(size: brand.micro-size, fill: brand.muted)[
    Counted per attribute and stratum, cumulatively over the year. A single test is Partially effective from #str(params.at("result_partial_min", default: 1)) fail and Ineffective from #str(params.at("result_ineffective_min", default: 3)).
  ]
]

// ---------------------------------------------------------------------------
// 8. Roadmap
// ---------------------------------------------------------------------------

#slide("7 · Roadmap", "The year at a glance")[
  #let cards = pack.at("roadmap", default: ())
  #grid(
    columns: (1fr,) * calc.max(1, cards.len()),
    column-gutter: 6pt,
    rows: 72mm,
    ..cards.map(c => card(c.at("label", default: ""), [
      #text(size: 9pt, fill: brand.muted)[#c.at("period", default: "")]
      #v(-0.2em)
      #set text(size: 9pt)
      #for l in first(c.at("lines", default: ()), 7) [
        - #l
      ]
    ], tint: c.at("label", default: "") == "Mobilise")),
  )
]

// ---------------------------------------------------------------------------
// 9. Quarterly targets
// ---------------------------------------------------------------------------

#slide("8 · Quarterly targets", "What each quarter must deliver")[
  #let qcols = quarters.map(q => "Q" + str(q.at("quarter", default: 0)))
  #let rows = ()
  #for s in strata {
    rows.push((s.at("name", default: "") + ": attested by quarter end (cumulative)",) + s.at("cumulative_target_pct", default: ()).map(x => num(x) + "%"))
    rows.push((s.at("name", default: "") + ": items due in the wave",) + s.at("reviews_due", default: ()).map(x => str(x)))
    rows.push((s.at("name", default: "") + ": tests",) + s.at("tests", default: ()).map(x => str(x)))
  }
  #rows.push(("All strata: tests",) + quarters.map(q => str(q.at("planned_tests", default: 0))))
  #rows.push(("Report to the " + get("approval_forum"),) + quarters.map(q => day(q.at("report_date", default: ""))))
  #small-table(
    columns: (1fr,) + (auto,) * qcols.len(),
    header: ("Target",) + qcols,
    rows: rows,
  )
]

// ---------------------------------------------------------------------------
// 10. The quarterly operating cycle
// ---------------------------------------------------------------------------

#slide("9 · Operating cycle", "The same cycle every quarter")[
  #let stages = (
    ("1LoD window opens", "lod1_window_start"),
    ("1LoD deadline", "lod1_deadline"),
    ("Sample drawn", "selection_date"),
    ("Fieldwork starts", "fieldwork_start"),
    ("Fieldwork ends", "fieldwork_end"),
    ("Findings agreed", "findings_agreed_by"),
    ("Report", "report_date"),
  )
  #grid(
    columns: (1fr,) * stages.len(),
    column-gutter: 3pt,
    ..stages.enumerate().map(((i, s)) => block(
      width: 100%, inset: (x: 5pt, y: 6pt),
      fill: if calc.even(i) { brand.table-head } else { brand.table-zebra },
      stroke: (bottom: 1.4pt + brand.accent),
    )[
      #text(size: brand.micro-size, fill: brand.muted)[#str(i + 1)]
      #v(-0.4em)
      #text(size: 9pt, weight: "bold")[#s.at(0)]
    ]),
  )
  #v(2mm)
  #small-table(
    columns: (auto,) + (1fr,) * stages.len() + (auto,),
    header: ("",) + stages.map(s => s.at(0)) + ("Committee",),
    rows: quarters.map(q => ("Q" + str(q.at("quarter", default: 0)),) + stages.map(s => day(q.at(s.at(1), default: ""))) + (q.at("committee_label", default: ""),)),
  )
  #text(size: brand.micro-size, fill: brand.muted)[
    Dates fall on business days. Evidence is due #str(params.at("evidence_sla_days", default: 10)) business days after it is requested; later evidence is escalated.
  ]
]

// ---------------------------------------------------------------------------
// 11. Governance
// ---------------------------------------------------------------------------

#slide("10 · Governance", "Who sees progress, and what is escalated")[
  #let gov = get("governance", fallback: (:))
  #let cadence = gov.at("cadence", default: ())
  #let esc = gov.at("escalations", default: ())
  #grid(
    columns: (1.1fr, 1fr),
    column-gutter: 8mm,
    [
      #heading-label("Reporting cadence")
      #if cadence.len() == 0 [No cadence recorded.] else [
        #small-table(
          columns: (auto, auto, 1fr),
          header: ("Forum", "Frequency", "Content"),
          rows: first(cadence, 7).map(c => (c.at("forum", default: ""), c.at("frequency", default: ""), c.at("content", default: ""))),
        )
        #more(cadence, 7, "forums")
      ]
    ],
    [
      #heading-label("Escalation")
      #if esc.len() == 0 [No escalation triggers recorded.] else [
        #small-table(
          columns: (1.4fr, 1fr, 0.8fr),
          header: ("Trigger", "Escalate to", "Within"),
          rows: first(esc, 6).map(e => (e.at("trigger", default: ""), e.at("escalate_to", default: ""), e.at("within", default: ""))),
        )
        #more(esc, 6, "triggers")
      ]
    ],
  )
]

// ---------------------------------------------------------------------------
// 12. Resourcing and delivery risks
// ---------------------------------------------------------------------------

#slide("11 · Resourcing", "Resourcing and delivery risks")[
  #let cap = get("capacity", fallback: (:))
  #let loads = cap.at("quarters", default: ())
  #let threshold = cap.at("utilisation_threshold", default: 0)
  #let risks = pack.at("delivery_risks", default: ())
  #grid(
    columns: (1.2fr, 1fr),
    column-gutter: 8mm,
    [
      #heading-label("Effort against capacity, person-days")
      #small-table(
        columns: (auto, 1fr, 1fr, 1fr, 1fr, 1fr, auto),
        header: ("", "Testing", "Reviews", "Retests", "Overhead", "Capacity", "Utilisation"),
        rows: loads.map(l => (
          "Q" + str(l.at("quarter", default: 0)), num(l.at("test_effort", default: 0)), num(l.at("review_effort", default: 0)),
          num(l.at("retest_days", default: 0)), num(l.at("overhead_days", default: 0)), num(l.at("capacity", default: 0)),
          if l.at("over_threshold", default: false) { text(fill: brand.risk, weight: "bold")[#pct(l.at("utilisation", default: 0)) over] } else { pct(l.at("utilisation", default: 0)) },
        )),
      )
      #text(size: brand.micro-size, fill: brand.muted)[
        The year needs #num(cap.at("total_effort", default: 0)) of #num(cap.at("total_capacity", default: 0)) person-days (#pct(cap.at("utilisation", default: 0))), from #num(params.at("tester_fte", default: 0)) tester FTE at #num(params.at("days_per_fte_quarter", default: 0)) days a quarter. The threshold is #(pct(threshold) + ";") the peak is Q#str(pack.at("peak_quarter", default: 1)).
      ]
    ],
    [
      #heading-label("Delivery risks")
      #if risks.len() == 0 [
        No delivery risk flagged by the plan's checks.
      ] else [
        #set text(size: 9pt)
        #for r in risks [
          - #r
        ]
      ]
    ],
  )
]

// ---------------------------------------------------------------------------
// 13. Next steps
// ---------------------------------------------------------------------------

#slide("12 · Next steps", "What happens next")[
  #let steps = pack.at("next_steps", default: ())
  #if steps.len() == 0 [
    Every step of the plan is complete.
  ] else [
    #small-table(
      columns: (auto, 1fr, auto, auto, 1fr),
      header: ("Step", "What", "Responsible", "When", "Deliverable"),
      rows: steps.map(s => (
        s.at("step_ref", default: ""), s.at("title", default: ""), s.at("responsible_role", default: ""),
        if s.at("start_date", default: "") == s.at("end_date", default: "") { day(s.at("end_date", default: "")) } else { day(s.at("start_date", default: "")) + " – " + day(s.at("end_date", default: "")) },
        s.at("deliverable", default: ""),
      )),
    )
  ]
  #v(1fr)
  #text(size: 9pt)[
    #heading-label("Decisions requested today")
    #for (i, d) in get("decisions_requested", fallback: ()).enumerate() [
      #str(i + 1). #d \
    ]
  ]
]
