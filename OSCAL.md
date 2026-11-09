# OSCAL

grc's database is the system of record. OSCAL (NIST's Open Security Controls
Assessment Language) is an interchange format around it: grc imports NIST's
official SP 800-53 catalog from OSCAL, and exports its catalogs and
crosswalks as OSCAL JSON for other tools. OSCAL is a serializer, as
`POLICY_MODULE_FRAMEWORK.md` puts it; it never becomes the store.

Status: phase 1 (the foundation and the 800-53 catalog import) and phase 2
(the catalog, profile, NFR and Regulation Coverage exports) are built. The
assurance results (phase 3, designed here before it is built), the trestle
check and the signing design follow.

## What is imported

| Route | Access | What it does |
|---|---|---|
| `POST /controls/import-oscal/preview` | admin | Reads the posted files and says what an import would do. Writes nothing. |
| `POST /controls/import-oscal` | admin | Applies a preview without problems, if neither the catalog nor the files changed since it. |

"Import OSCAL" on the Control Editor (`/controls/manage`) drives both. The
browser reads the chosen files and posts their text in one JSON body:
`{"catalog": "…", "profiles": {"low": "…", "moderate": "…", "high": "…",
"privacy": "…"}, "base": "…"}` (32 MB at most). Only the catalog is required.

The application never downloads anything. Get the files from
<https://github.com/usnistgov/oscal-content>, directory
`nist.gov/SP800-53/rev5/json/`:

- `NIST_SP-800-53_rev5_catalog.json` (about 10.4 MB; release 5.2.0 of
  2026-10-22 has 1,196 controls and enhancements, 182 of them withdrawn)
- `NIST_SP-800-53_rev5_LOW-baseline_profile.json`, `…_MODERATE-…`,
  `…_HIGH-…`, `…_PRIVACY-…`

JSON only: no XML, no YAML.

### How the catalog maps onto `nist_controls`

| grc column | From the OSCAL catalog |
|---|---|
| `control_id` | The control's `label` prop with no class (`AC-2(1)`), through `controlid.Normalize`; the `id` token (`ac-2.1`) when there is no label. |
| `control_type` | `Control` for a control directly in a group, `Control Enhancement` for one nested in a control. |
| `name` | `title`. |
| `requirements` | The `statement` part rendered as text: each item as `label prose` on its own line, depth first (`a.`, `1.`, `(a)`); a parameter as `[Assignment: organization-defined <label>]` or `[Selection: …; …]` / `[Selection (one or more): …; …]`; a link (`[AC-16a](#ac-16_smt.a)`) as its text; no space before punctuation after either. A parameter may be defined by another control (IA-13(3) inserts `ia-13_odp.01`). |
| `discussion` | The `guidance` part's prose, rendered the same way. |
| `related_controls_json` | `links` with `rel="related"`, as grc ids. |
| `withdrawn` | 1 when the control has a `status` prop of `withdrawn`, else 0. |
| `in_low` … `in_privacy`, `mapping_baselines_json` | For each posted profile only: membership of its `include-controls` `with-ids` (with `with-child-controls`, less `exclude-controls`). A baseline whose profile was not posted is left as it is. |
| `params_json` | The control's `params` array, as OSCAL JSON. |
| `statement_json` | The `statement` part, as OSCAL JSON. A statement carrying the grc prop `prose` = `grc-text` (grc's own export writes one, below) is grc's text: `requirements` is its prose verbatim and `statement_json` is left as it is. A guidance part with that prop gives `discussion` verbatim too. |
| `assessment_json` | The `assessment-objective` and `assessment-method` parts (SP 800-53A), as OSCAL JSON, in document order. |
| `source_key` | Only for a control the catalog lacks: `<id>: <title>`, the shape of the shipped file's record keys. |

A withdrawn control (a `status` prop of `withdrawn`) is mapped like any
other and marked in `withdrawn`, which the detail page shows. NIST gives a
withdrawn control a title and, except for CP-10(3) and SC-19, no statement or
guidance, so its requirements and discussion stay empty, as in the shipped
file. The JSON catalog file carries no withdrawn flag, so a catalog never
imported from OSCAL has none marked. The name does change: the shipped file names a
withdrawn enhancement `<parent title> | <title>` (`Account Management |
Shared and Group Account Credential Change` for AC-2(10)), and the import
writes NIST's `title` alone. Against release 5.2.0 that renames 159 withdrawn
controls (157 of that shape; IR-10 and MA-4(2) carry other names) and 19 live
ones (18 en dashes become NIST's em dashes; IA-2(6) takes NIST's
`—separate`).

Not every column is OSCAL's. Never written by an OSCAL import, on any existing control: the CIA flags and
`justification`, `threats_json`, `potentially_common_inheritable`,
`appendix_json` (grc's own content, `References` included, which the DORA
Report and the Exceptions Workbook read) and `source_key`. The import has no
replace mode and never deletes; controls the file does not carry are listed
as absent and left alone. Every apply rebuilds the NFR-control links.

### Preview and apply

The preview lists every problem first (a profile posted in the wrong slot, a
profile naming a control the catalog lacks, an unreadable profile, a
parameter the text inserts but nobody defines, a duplicate id), then the
added controls, the changed ones with the fields that change (and, for the
text fields, before and after, marked when the change is whitespace only),
the unchanged ones and the absent ones. Its `base` is a SHA-256 over the
whole stored catalog and the posted files' hashes. Apply refuses with 400
while there is a problem and with 409 when the catalog or the files no longer
match `base`; the repository checks the catalog digest again inside the write
transaction. The result reports the source catalog's title, version,
`oscal-version`, `last-modified` and SHA-256, and each profile's.

## What is exported

| Route | Document | Download name | `last-modified` |
|---|---|---|---|
| `GET /controls/oscal/catalog.json` | catalog | `grc-sp800-53-catalog-<date>.oscal.json` | the export |
| `GET /controls/oscal/profile/<baseline>.json` | profile | `grc-sp800-53-<baseline>-profile-<date>.oscal.json` | the export |
| `GET /security-nfrs/oscal/catalog.json` | catalog | `grc-nfr-catalog-<date>.oscal.json` | the export |
| `GET /security-nfrs/oscal/mapping.json` | mapping collection | `grc-nfr-mapping-<date>.oscal.json` | the export |
| `GET /regulation-coverage/:id/oscal/catalog.json` | catalog | `grc-regulation-<id>-v<n>-catalog-<date>.oscal.json` | the version |
| `GET /regulation-coverage/:id/oscal/mapping.json` | mapping collection | `grc-regulation-<id>-v<n>-mapping-<date>.oscal.json` | the version |

Every export is read-only and open to any signed-in user, under the prefix
its module's pages already sit under (a user granted only `/controls` reads
the catalog exports and not the NFR ones). They are linked from the Controls
page, the Security NFRs page, the report page and the Help page, and answer
404 with a JSON error when there is nothing to export. The date in the name
is `last-modified`'s. A Regulation Coverage version is immutable, so its
exports are read from the version's snapshot, never from the live findings,
carry the version's own timestamp, and are the same bytes every time;
`?version=N` picks a version and its absence the latest. The live catalogs
are dated by the export.

Documents that refer to one another use root-relative hrefs to these routes
(`/controls/oscal/catalog.json`), which resolve against the server that
served them. A saved copy has to be pointed at its neighbours by whoever
saved it.

### The SP 800-53 catalog

The import's mapping run backwards, so an export imports back into the same
catalog with no problem, no change and no absent control. The tests check
this over the whole shipped catalog with the fixture's controls imported from
OSCAL and one edited afterwards.

| OSCAL | From grc |
|---|---|
| `groups` | One per family: `id` the family code in lower case, `class` `family`, `title` the family's name. Hidden families are included. |
| `controls` | A control in its family, an enhancement nested in its parent (at the family's top level when the parent is missing, which reads back as a Control). |
| `id`, `class` | NIST's token (`ac-2.1`); `SP800-53` or `SP800-53-enhancement`. |
| `title` | `name`. |
| `props` | `label` (`AC-2(1)`), `sort-id` (`ac-02.01`), and `status` `withdrawn` when `withdrawn` is 1. |
| `params` | `params_json`, as stored. |
| `parts`: statement | `statement_json` as stored while it still renders to the stored `requirements`. Otherwise (a control never imported from OSCAL, or edited since) a statement whose prose is `requirements`, with the grc prop `prose` = `grc-text`. The database is the record: an edit wins over NIST's structure, and the structure stays stored for when the text is NIST's again. |
| `parts`: guidance | `discussion`, with the grc prop `prose` = `grc-text`: grc stores the text of NIST's guidance, not its part. |
| `parts`: assessment | `assessment_json`, as stored. |
| `links` | `rel="related"` to `#<token>` for each related control. |

Not exported: the CIA ratings, justification, threats, potentially
common/inheritable, the appendix and the record key, which are grc's own
analysis of a control rather than the catalog. NIST's back-matter is not
stored, so the export has none.

### The baseline profiles

One per baseline: `imports` the catalog export by href, `include-controls`
`with-ids` every control the catalog marks in that baseline, and `merge`
`as-is`, the shape of NIST's own baseline profiles. The title names the
baseline, so the import accepts it in its slot. A baseline no control is in
is a 404: an import with no selection reads as nothing, or to some tools as
everything.

### The Security NFR catalog

| OSCAL | From grc |
|---|---|
| `groups` | One per domain (`id` `domain-<slug>`, `class` `domain`), ordered by name. An NFR without a domain is a control at the catalog's top level. |
| control `id`, `class`, `title` | `nfr-<record key>`, `security-nfr`, the summary. |
| `props` | `label` the NFR id (`1.4`, the record key when there is none); grc's `record-key`, `issue-type` and `weight`. |
| `parts` | `statement` the description; grc's `implementation`, `additional-details` and `nist-mapping` (the mapping text as written). |

### The NFR to SP 800-53 mapping collection

One mapping from the NFR catalog export to the 800-53 catalog export, with a
map per NFR and the control a reference in its NIST mapping resolved to (the
NFR-Control Links table, overrides included).

- A map's grc props: `override` `yes` when an administrator's override
  decided it, and `written-as` when the NFR wrote another id (`AU-11`
  overridden to `AU-12`).
- `source-gap-summary` lists the NFRs that resolved no control,
  `target-gap-summary` the catalog's controls no NFR maps to.
- A reference that resolved to nothing is not a map; the mapping's remarks
  name it.
- Provenance: method `human` (an NFR's author wrote the mapping),
  matching-rationale `semantic` (meaning matched to meaning), status
  `complete` when every NFR resolved a control and every reference resolved,
  `not-complete` otherwise.

### A Regulation Coverage version

The catalog has a control per section, in position order:

| OSCAL | From the version's snapshot |
|---|---|
| control `id` | The section's ref as a token (`dora-art-6`), with `-<position>` when two refs give one id. |
| `title` | The section's title, or its label when it has none. |
| `props` | `label`, `sort-id` (the position); grc's `ref`, `category` and `segment-confidence`. |
| `parts`: statement | The section's text as extracted. |
| `parts`: grc `finding` | Props `relevant`, `grounded`, `confidence`, `model`, `prompt-hash`, `revision`; parts `requirement`, `commentary`, `gaps`, `quote`. |
| metadata | `title` the regulation's, `version` the report version, remarks the version's note; grc props `framework`, `framework-name`, `source-ref`, `text-sha256`. |

The mapping collection has one mapping to the NFR catalog export and one to
the 800-53 catalog export, each only when it has a map, with a map per
section and catalog item the finding maps it to.

- A map's `confidence-score` category is the report's (`high`, `medium`,
  `low`), its remarks the rationale, and its grc prop `source` says who made
  it: `seed` (a curated crosswalk) or `model`.
- The mapping's method follows: `human` for seeds only, `automation` for the
  model only, `hybrid` for both.
- A mapping to a reference the catalog does not hold (`known` false) is not a
  map; the remarks name it.
- `source-gap-summary` lists the relevant sections with no map to that
  catalog.
- Status: `superseded` for any version but the latest; the latest is
  `complete` when every section has a finding, `not-complete` otherwise.
- The source href is the version's own catalog export
  (`…/oscal/catalog.json?version=N`); the targets are the live NFR and 800-53
  exports, which may have moved since the version was cut.

### The relationship

OSCAL requires a relationship on every map, and NIST IR 8477 defines them as
set relations: equivalent, subset, superset, intersecting. grc does not
assess that: it records that a target addresses a source. Every map
therefore carries grc's own relationship, `related`, with `ns` set to grc's
namespace, which OSCAL allows for exactly this ("different organizations to
associate distinct semantics"). Picking an IR 8477 value would be inventing
one. The matching-rationale, which OSCAL also requires, is `semantic`.

## Versions, identifiers and bytes

- **One OSCAL version.** grc reads and writes with the OSCAL 1.2.1 model
  (`oscal.Version`), the version compliance-trestle's model is built
  against. go-oscal's generated 1.2.1, 1.2.2 and 1.2.3 types are identical
  apart from the package name and the version constant, and the 1.2.1 and
  1.2.3 JSON schemas differ only in two component-type enumerations, so
  NIST's 1.2.3 catalog parses without losing a field. Every export
  declares the version it was built with.
- **Deterministic UUIDs.** `oscal.UUID(kind, key)` is a UUIDv5 under grc's
  fixed namespace `2e98bbc8-0256-4a99-a220-1f7b4c3f60a6`, keyed on the kind
  of object and a stable key, so an unchanged object keeps its UUID across
  exports and installations. A mapping or map is keyed on what it relates
  (an NFR key and a control id; a regulation's text hash, a section ref and a
  catalog item), so it keeps its UUID across revisions, as OSCAL asks. A
  document is keyed on its content digest: the SHA-256 of its canonical bytes
  with the UUID left out and `last-modified` zeroed. So the same data gives
  the same UUID, and a revised document a new one, as OSCAL also asks.
- **The metadata version** is the document's own where it has one (a report
  version's number), and otherwise the first 12 hex digits of that content
  digest, which changes exactly when the content does.
- **Same data, same bytes.** Two exports of unchanged data differ only in
  `last-modified`; input order does not matter (the tests export reversed
  input and compare).
- **grc's namespace.** Content that is grc's rather than OSCAL's or NIST's
  goes into props and parts with `ns` set to
  `https://carelockconsulting.com/ns/oscal/grc`.
- **Canonical bytes.** Exports are written through `oscal.Canonical`, RFC 8785
  (JSON Canonicalization Scheme), so the same data gives the same bytes.
- **Mappings as an array.** The 1.2.1 schema lets a mapping collection's
  `mappings` be one mapping or an array of them; go-oscal's generated type
  holds only one. grc always writes an array, through a small struct of its
  own around go-oscal's `Mapping` type, because a Regulation Coverage version
  has two.

## The dependency

`github.com/defenseunicorns/go-oscal` (Apache-2.0), only its generated types
package `src/types/oscal-1-2-1`, which imports nothing but `time`.
`internal/oscal` is its only importer; domain modules receive neutral types
through `internal/app/oscal_adapter.go`. The module's CLI (cobra), schema
generator and validator are not linked into the binary
(`TestBinaryLinksOnlyTheOSCALTypes`); the validator, with its embedded
schemas, is used by tests through `internal/oscal/oscaltest`.
compliance-trestle is not a dependency.
