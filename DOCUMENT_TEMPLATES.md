# Business & Policy Document Templates

Typeset templates for the two document classes this practice produces: **policy
documents** (ISMS, ISO 27001, NIST, FedRAMP, PCI DSS) and **business reports**
(assessments, gap analyses, control reviews).

Everything in `templates/` builds and has been rendered and visually checked.
This is phase 5 in [`POLICY_MODULE_FRAMEWORK.md`](POLICY_MODULE_FRAMEWORK.md),
and it is now wired into the app as well as buildable from the command line —
see [Rendering from the app](#rendering-from-the-app) below. `build.sh` remains
the right tool when you are working *on* a template; the app is the right one
when you want a deliverable.

```
templates/
  typst/
    brand.typ              ← the only file a rebrand needs to touch
    lib.typ                page furniture, cover, tables, callouts
    policy-document.typ    policy / standard / procedure / work instruction
    business-report.typ    assessment and consulting reports
  latex/
    carelock.sty           LaTeX equivalent of brand.typ + lib.typ
    policy-document.tex    LaTeX policy document
  samples/
    policy-sample.json     realistic input, matching internal/policydocs
    report-sample.json
  build.sh
```

---

## Quick start

Typst is a single static binary with no dependencies — download it from
[the releases page](https://github.com/typst/typst/releases), put it on `$PATH`,
and:

```sh
cd templates
./build.sh                # both Typst templates, from the sample data
./build.sh policy         # just the policy document
./build.sh report         # just the report
./build.sh latex          # the LaTeX policy document
```

Render a real document by passing its JSON:

```sh
./build.sh policy ../../exports/POL-AC-001.json
```

Outputs land in `templates/out/` (gitignored).

For the LaTeX path, `build.sh latex` prefers
[tectonic](https://tectonic-typesetting.github.io/) — also a single binary,
which downloads the TeX packages it needs on first run — and falls back to
`latexmk -xelatex` if you have a TeX distribution. **XeLaTeX or LuaLaTeX, not
pdfLaTeX**: the style uses `fontspec`.

### Archival and accessible output

```sh
PDF_STD=a-2b        ./build.sh policy   # default — archival
PDF_STD=a-2b,ua-1   ./build.sh policy   # archival + screen-reader accessible
PDF_STD=            ./build.sh policy   # plain PDF 1.7
```

`a-2b` (PDF/A-2b) embeds every font and forbids the features that make a PDF
render differently on a machine that no longer has the original fonts — which
matters because a policy gets filed and re-read years later. Adding `ua-1`
turns on Typst's accessibility checks: it verifies heading order, a document
title and alternative text, and **fails the build** if the document is not
genuinely accessible. Both templates pass. `ua-1` and `a-4` are mutually
exclusive — PDF/UA-1 requires PDF 1.7 or earlier, PDF/A-4 requires 2.0.

---

## Rendering from the app

`internal/doctemplate` serves two pages and one render endpoint.

| Route | What it does |
|---|---|
| `/templates` | Gallery: engine status, a render form over the policy documents, one card per template |
| `/templates/manage` | Brand editor — `brand.typ`'s dictionary as a form, stored per install |
| `GET /templates/render` | Renders. `doc=<id>` or `sample=1`, plus `template=`, `standard=`, `bundle=1`, `inline=1` |

The **Render PDF** action in the policy editor points at the same endpoint.

### No engine installed is a normal outcome

The app bundles no typesetter — it ships as one Go binary, and typst and
tectonic are separate installs. Where the engine a template needs is absent,
the render endpoint returns **a zip of the sources instead of a PDF**: the
template, its dependencies, the brand already applied, the payload, a
`build.sh` and a README. Unzip it and run `./build.sh`. The same bundle is
available on demand with `bundle=1`, which is the honest way to hand a designer
exactly what a render used.

Point `TYPST`, `TECTONIC` or `LATEXMK` at a binary to override the `PATH`
lookup. Detection is cached per process; **Re-check** on the gallery re-probes
after an install.

### What the app does differently from `build.sh`

- **The compile is rooted at a throwaway workspace**, not at the repository.
  `build.sh` passes `--root ..` so a template can reference `../samples/*.json`,
  which is right for a developer and wrong for a server: each render assembles
  a temporary directory holding only the files it needs, and the typesetter
  cannot read outside it.
- **The brand comes from the database.** The saved settings are spliced into
  `brand.typ`'s dictionary at render time; the file on disk stays the
  hand-editable original, and `DefaultBrand()` in `brand.go` mirrors it. Editing
  the file by hand still works and is still what a rebrand of the *defaults*
  should touch — but a value saved in the UI wins for anything the app renders.
- **The LaTeX `.tex` is generated from the payload**, by `latexgen.go`. The
  committed `templates/latex/policy-document.tex` is the hand-filled reference
  the generator mirrors, not the file that gets compiled.

---

## Part 1 — What the research says

### Margins and measure

One inch (25.4mm) all round is the professional standard; tighter feels cramped
and wider stretches the document out. These templates use 30mm left and right,
which is slightly generous, because it is what brings the **measure** into
range: legibility research puts comfortable line length at **45–90 characters**,
and 150mm of text at 11pt lands near 86. That is the widest a business document
should run before readers start losing their place between lines. If you narrow
the margins, drop the type size to match, or the document gets harder to read
while looking like it got denser.
([The Visual Communication Guy](https://thevisualcommunicationguy.com/2026/06/26/how-to-make-a-document-look-professional-5-practical-adjustments/),
[Lisa Furze](https://lisafurze.com/blog/5-typography-tips/))

### Typeface

Two faces, no more. Serif body, sans headings — that contrast carries the
hierarchy without needing colour or rules, which is what keeps a document
readable when a client prints it in greyscale, and they will. Decorative faces
read as amateur in formal contexts; more than two looks messy.
11–12pt for print, headings 14–18pt by level.
([Creative Market](https://creativemarket.com/blog/best-fonts-for-business-documents),
[Proofed](https://proofed.com/writing-tips/a-guide-to-use-of-fonts-in-formal-writing/))

The templates use **font stacks rather than single names**, so a document still
builds on a machine that has neither Libertinus nor a client's licensed face:

| Role | Stack |
|---|---|
| Body | Libertinus Serif → Liberation Serif → Times New Roman → DejaVu Serif |
| Headings | Liberation Sans → Arial → Noto Sans → DejaVu Sans |
| Mono | Liberation Mono → DejaVu Sans Mono → Courier New |

Liberation Serif and Sans are metric-compatible with Times New Roman and Arial,
so a document laid out on Linux keeps its pagination when rebuilt on a machine
that only has the Microsoft-metric fonts.

> Typst prints `unknown font family` warnings for the names in a stack that are
> absent locally, even when an earlier one matched. That is expected, and
> `build.sh` filters exactly those warnings and nothing else.

### Colour

One accent, used only for rules, table headers and the cover band. Body text is
never coloured. Multiple accent colours are the fastest way to make a
deliverable look amateur; a branded palette applied with restraint is what
signals a professional deliverable.

### Cover pages

Title most prominent, then subtitle, then author and date in decreasing weight.
Logo present but not overwhelming. Clean layout with real white space rather
than filling the page. Include a document reference number.
([Texas A&M — Front Matter](https://odp.library.tamu.edu/professionalwriting/chapter/common-report-sections-front-matter/),
[Venngage](https://venngage.com/blog/report-cover-page/))

The cover here is a top identity band, the title at roughly the upper third, and
the control metadata pinned to the bottom — so the eye lands on the title rather
than on a wall of fields.

### What a policy document must contain

Cover metadata should carry title, internal code (e.g. `POL-IS-001`), version,
date, classification, author, reviewer and approver. The body needs policy
statement and purpose, scope, responsibilities, references to related documents,
definitions, and review date and version. Revision history and change management
are standard.
([hightable.io](https://hightable.io/iso-27001-information-security-policy/),
[Secra](https://secra.es/en/blog/iso-27001-security-policy-template))

Two details the templates treat as load-bearing rather than decorative:

- **Classification is marked on every page**, in both header and footer — not
  just the cover. A page that gets separated from its document has to carry its
  own handling instruction; that is the entire purpose of the marking.
- **"Page X of Y", not "Page X".** "Page 4" tells someone holding a printout
  nothing about whether they have the whole document.

Sections are **numbered**. Auditors, exception records and management responses
all cite policy text by section number.

### What a consulting report must contain

The governing principle: a deliverable must be **self-contained** — readable and
actionable by someone who was not in the room when it was presented. That is why
every finding in `business-report.typ` carries its own observation, risk and
recommendation rather than relying on a verbal briefing to join them up.

A consistent format sets client expectations: they know what to look for and
where. The executive summary is for a reader making a funding or policy decision
who needs the position quickly, so the severity profile sits directly beneath it
rather than behind the methodology.
([NetSuite](https://www.netsuite.com/portal/resource/articles/accounting/consulting-services-deliverables.shtml),
[TCGen](https://www.tcgen.com/product-management/consulting-deliverables/))

The report template also carries a **Limitations and Basis of Reporting**
section. A professional deliverable states who may rely on it, what period it
covers, and that sample testing gives reasonable rather than absolute assurance.

### Normative language

Policy statements need a controlled vocabulary:

- **must / shall** — mandatory, auditable, no exceptions without a formal waiver
- **should** — expected; deviation requires documented justification
- **may** — permitted, discretionary

Never "will", "strives to", "is encouraged to", "where possible". An assessor
cannot test those — there is no pass or fail — so they read as requirements and
fail audits. The policy module's linter (`internal/policydocs/lint.go`) flags
them automatically.

---

## Part 2 — Why Typst is the primary path

| | Typst | LaTeX |
|---|---|---|
| Install | One static binary, Apache 2.0 | >1 GB distribution |
| Compile | 200–500ms | Multiple seconds, multiple passes |
| Data | **Native JSON/CSV/XML loading** | Emit `.tex` from a script, or LuaTeX |
| Accessibility | Tagged PDF by default; PDF/UA-1 checked at build | Careful configuration required |
| Templating | One consistent mechanism | Different conventions per package |

([Typst — automated generation](https://typst.app/blog/2025/automated-generation/),
[TypeTeX comparison](https://www.typetex.app/comparisons/typst-vs-latex),
[Typst accessibility guide](https://typst.app/docs/guides/accessibility/))

The decisive factor for this repo is **native JSON loading**. `policy-document.typ`
reads the same record shape `internal/policydocs` already produces, so the
renderer's job is "write the document as JSON, invoke the template" — no
intermediate transformation, no code generation. LaTeX was designed for
hand-edited manuscripts, and programmatic generation means emitting source with
error-prone scripts.

The LaTeX templates are supplied for clients whose house style is already LaTeX,
and are deliberately typographically identical so both produce documents that
look like they came from the same firm. **The LaTeX policy template does not
read the JSON** — fill it in by hand, or generate the `.tex` from a script if
you need it automated.

### Migrating an existing LaTeX template

Typst publishes a [guide for LaTeX users](https://typst.app/docs/guides/for-latex-users/)
with command equivalences. This is a task AI assistance suits well: paste the
LaTeX, get Typst, render both, compare side by side. Do the conversion once per
template rather than maintaining a LaTeX rendering path indefinitely.

---

## Part 3 — Customising

### Rebranding

Edit **`templates/typst/brand.typ`** and nothing else. It holds identity strings,
the colour palette, font stacks, sizes, page geometry and the footer note. The
templates read every one of those from it; `lib.typ` decides *structure*,
`brand.typ` decides *appearance*.

```typst
#let brand = (
  firm: "Your Firm",
  firm-tagline: "What You Do",
  logo: "assets/logo.svg",   // none renders a typographic wordmark instead
  logo-width: 38mm,
  accent: rgb("#1F3A5F"),
  // ...
)
```

Set `logo: none` to get a typographic wordmark. That looks deliberate; a
stretched or low-resolution logo does not.

For LaTeX, the same values live in the *Identity* and *Palette* blocks at the
top of `templates/latex/carelock.sty`.

### Adding a document type

Copy `policy-document.typ`. The pieces you compose from `lib.typ`:

| Helper | Purpose |
|---|---|
| `base-document(...)` | Page setup, type rules, headers, footers, widow control |
| `cover-page(...)` | Title page with a flexible metadata grid |
| `kv-table(pairs)` | Two-column document-control block |
| `data-table(...)` | House table style: hairlines, tinted header, zebra rows |
| `callout(title, body)` | Boxed aside for summaries and scope notes |
| `section-block(h, body, note)` | Numbered section with an optional trailing note |
| `approval-block(...)` | Signature lines |
| `toc(depth)` | Contents with an accent rule |

### The JSON contract

`policy-document.typ` expects the shape `internal/policydocs` models —
`samples/policy-sample.json` is a complete, realistic example:

```
title, reference, doc_type, status, classification, owner_role, approver,
client_name, frameworks[], effective_date, review_cadence_months,
next_review_date, summary,
sections[]  { heading, body, section_kind, controls[] { control_id, coverage } }
controls[]  { control_id, control_name, coverage, section_heading }
versions[]  { version_label, approved_by, approved_at, change_summary }
```

Every field is read with a default, so a partial record renders rather than
failing — missing values show as `—`.

The app emits exactly this shape at **`GET /policies/:id/export.json`** (the
"Export JSON" button in the policy editor), so the pipeline is end-to-end:

```sh
curl -o doc.json https://your-host/policies/12/export.json
cd templates && ./build.sh policy ../../doc.json
```

It is a dedicated `TemplateExport` type rather than the internal structs
serialised directly -- row ids and provenance have no business in a client
deliverable's data file, and a separate type means renaming an internal JSON tag
cannot silently produce PDFs with blank fields.

---

## Deployment note

Typst and tectonic are external binaries, and this repo ships self-contained
`goreleaser` artifacts with no Docker path. The render endpoint **discovers the
binary at runtime and degrades gracefully**: HTML and Markdown export always
work in-process, a PDF is produced when the binary is present, and the sources
are returned when it is not. That keeps the single-binary distribution story
intact. Shipping the binaries in the release archive is the alternative, at the
cost of artifact size and licence notices.

The templates themselves are data files read from disk, not embedded — `go:embed`
only reaches inside the importing package's directory, and they deliberately
live at `templates/` where `build.sh`, a designer editing `brand.typ` and the
app all see the same copy. So a deployed server needs them installed, and
`-templates-dir` / `TEMPLATES_DIR` names the directory when the search does not
find it. Same shape as `-docs-dir`; see `RUNTIME_ARGS.md`.

---

## Sources

- [The Visual Communication Guy — making a document look professional](https://thevisualcommunicationguy.com/2026/06/26/how-to-make-a-document-look-professional-5-practical-adjustments/)
- [Creative Market — best fonts for business documents](https://creativemarket.com/blog/best-fonts-for-business-documents)
- [Proofed — choosing a professional font](https://proofed.com/writing-tips/a-guide-to-use-of-fonts-in-formal-writing/)
- [Lisa Furze — typography tips for business](https://lisafurze.com/blog/5-typography-tips/)
- [Elite Research — corporate document formatting](https://eliteresearch.com/streamlining-corporate-document-formatting-5-best-practices-for-professional-reports/)
- [Texas A&M — common report sections, front matter](https://odp.library.tamu.edu/professionalwriting/chapter/common-report-sections-front-matter/)
- [Venngage — report cover page examples](https://venngage.com/blog/report-cover-page/)
- [NetSuite — developing consulting deliverables](https://www.netsuite.com/portal/resource/articles/accounting/consulting-services-deliverables.shtml)
- [TCGen — consulting deliverables guide](https://www.tcgen.com/product-management/consulting-deliverables/)
- [hightable.io — ISO 27001 information security policy](https://hightable.io/iso-27001-information-security-policy/)
- [Typst — automated PDF generation](https://typst.app/blog/2025/automated-generation/)
- [Typst — accessibility guide](https://typst.app/docs/guides/accessibility/)
- [Typst — PDF export reference](https://typst.app/docs/reference/pdf/)
- [Typst — guide for LaTeX users](https://typst.app/docs/guides/for-latex-users/)
- [TypeTeX — Typst vs LaTeX 2026](https://www.typetex.app/comparisons/typst-vs-latex)
- [Tectonic](https://tectonic-typesetting.github.io/)
