// brand.typ — the single file to edit when adapting these templates to a
// client's identity. Nothing below this file's `brand` dictionary should need
// changing to rebrand a document set: the templates read every colour, face and
// identity string from here.
//
// Typography choices are deliberate, not decorative:
//
//   * Serif body, sans headings. The contrast does the hierarchy work without
//     needing colour or rules, which is what keeps a document readable when a
//     client prints it in greyscale — which they will.
//   * 11pt body on a 150mm measure. Legibility research puts the comfortable
//     line length at 45–90 characters; 150mm at 11pt lands near 86, at the top
//     of that band, which is the widest a business document should run before
//     the reader starts losing their place between lines.
//   * One accent colour, used only for rules and table headers. Multiple accent
//     colours are the single fastest way to make a deliverable look amateur.
//   * Font stacks, not single names. The first face that exists on the build
//     machine wins, so a document still builds on a box that has neither
//     Libertinus nor the client's licensed face.

#let brand = (
  // ---- Identity -----------------------------------------------------------
  firm: "CareLock Consulting",
  firm-tagline: "Risk and Control Advisory",
  // Path to a logo, relative to the template file. Leave as none to render a
  // typographic wordmark instead — which looks deliberate, whereas a stretched
  // or low-resolution logo does not.
  logo: none,
  logo-width: 38mm,

  // ---- Colour -------------------------------------------------------------
  // One accent, plus neutrals. The accent is used for rules, table headers and
  // the cover band; body text is never coloured.
  accent: rgb("#7a1f2e"),      // deep maroon
  accent-dark: rgb("#5c1622"),
  ink: rgb("#1b1b1b"),         // body text — near-black, not pure black
  muted: rgb("#5f6368"),       // labels, captions, footer
  rule: rgb("#c8c5c0"),        // hairlines
  table-head: rgb("#f2f0ed"),  // table header fill
  table-zebra: rgb("#faf9f8"), // alternating row fill

  // Status colours. Restrained on purpose — these appear as small text or
  // pills, never as large fills.
  ok: rgb("#1d6b45"),
  warn: rgb("#8a6100"),
  risk: rgb("#a32b1c"),

  // ---- Type ---------------------------------------------------------------
  // Each is a fallback chain. Libertinus Serif is a free Times successor with
  // proper old-style figures; Liberation Serif/Sans are metric-compatible with
  // Times New Roman and Arial, so a document laid out here keeps its pagination
  // on a machine that only has the Microsoft-metric fonts.
  serif: ("Libertinus Serif", "Liberation Serif", "Times New Roman", "DejaVu Serif"),
  sans: ("Liberation Sans", "Arial", "Noto Sans", "DejaVu Sans"),
  mono: ("Liberation Mono", "DejaVu Sans Mono", "Courier New"),

  body-size: 11pt,
  small-size: 9pt,
  micro-size: 7.5pt,

  // ---- Page ---------------------------------------------------------------
  paper: "a4",
  margin: (x: 30mm, top: 28mm, bottom: 26mm),

  // ---- Footer -------------------------------------------------------------
  // Shown bottom-left on every page. Keep it short; it is a provenance mark,
  // not a disclaimer.
  footer-note: "Uncontrolled when printed.",
)

// classification-colour maps a classification label to its marking colour.
// Unknown labels fall back to the muted neutral rather than guessing, so a
// client's bespoke scheme degrades to something sober instead of alarming.
#let classification-colour(label) = {
  let key = lower(label)
  if key == "public" { brand.ok }
  else if key == "internal" { brand.muted }
  else if key == "confidential" { brand.warn }
  else if key == "restricted" or key == "secret" { brand.risk }
  else { brand.muted }
}

// wordmark renders the firm identity: the logo when one is configured,
// otherwise a typographic lockup. Used on covers and in page headers.
#let wordmark(size: 11pt, colour: none) = {
  let c = if colour == none { brand.accent } else { colour }
  if brand.logo != none {
    image(brand.logo, width: brand.logo-width)
  } else {
    text(font: brand.sans, size: size, weight: "bold", fill: c, tracking: 0.4pt)[
      #upper(brand.firm)
    ]
  }
}

// label-text is the small uppercase sans label used above every field value in
// the document-control block. Centralised so the letter-spacing stays identical
// everywhere — inconsistent tracking on small caps is the kind of detail that
// reads as sloppy without the reader being able to say why.
#let label-text(body) = text(
  font: brand.sans, size: brand.micro-size, fill: brand.muted,
  weight: "medium", tracking: 0.8pt,
)[#upper(body)]

// field renders one label/value pair for the cover block.
#let field(label, value) = block(breakable: false)[
  #label-text(label)
  #v(-0.35em)
  #text(size: brand.small-size)[#if value == none or value == "" { "—" } else { value }]
]

// pill renders a small status marker. Outlined rather than filled: a filled
// block of colour draws the eye away from the title on a cover page.
#let pill(body, colour: none) = {
  let c = if colour == none { brand.muted } else { colour }
  box(
    inset: (x: 5pt, y: 2.5pt), radius: 2pt,
    stroke: 0.6pt + c,
    text(font: brand.sans, size: brand.micro-size, fill: c, weight: "medium", tracking: 0.5pt)[
      #upper(body)
    ],
  )
}
