# OSCAL

grc's database is the system of record. OSCAL (NIST's Open Security Controls
Assessment Language) is an interchange format around it: grc imports NIST's
official SP 800-53 catalog from OSCAL, and (in later phases of this work)
exports its catalogs, crosswalks and assurance results as OSCAL JSON for
other tools. OSCAL is a serializer, as `POLICY_MODULE_FRAMEWORK.md` puts it;
it never becomes the store.

Status: phase 1, the foundation and the 800-53 catalog import, is built.
The exports, the trestle check and the signing design follow.

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
| `in_low` … `in_privacy`, `mapping_baselines_json` | For each posted profile only: membership of its `include-controls` `with-ids` (with `with-child-controls`, less `exclude-controls`). A baseline whose profile was not posted is left as it is. |
| `params_json` | The control's `params` array, as OSCAL JSON. |
| `statement_json` | The `statement` part, as OSCAL JSON. |
| `assessment_json` | The `assessment-objective` and `assessment-method` parts (SP 800-53A), as OSCAL JSON, in document order. |
| `source_key` | Only for a control the catalog lacks: `<id>: <title>`, the shape of the shipped file's record keys. |

A withdrawn control (a `status` prop of `withdrawn`) is mapped like any
other, which keeps the representation the catalog has always had: there is no
withdrawn column, and NIST gives a withdrawn control a title and, except for
CP-10(3) and SC-19, no statement or guidance, so its requirements and
discussion stay empty. The name does change: the shipped file names a
withdrawn enhancement `<parent title> | <title>` (`Account Management |
Shared and Group Account Credential Change` for AC-2(10)), and the import
writes NIST's `title` alone. Against release 5.2.0 that renames 159 withdrawn
controls (157 of that shape; IR-10 and MA-4(2) carry other names) and 19 live
ones (18 en dashes become NIST's em dashes; IA-2(6) takes NIST's
`—separate`).

Never written by an OSCAL import, on any existing control: the CIA flags and
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

## Versions, identifiers and bytes

- **One OSCAL version.** grc reads and writes with the OSCAL 1.2.1 model
  (`oscal.Version`), the version compliance-trestle's model is built
  against. go-oscal's generated 1.2.1, 1.2.2 and 1.2.3 types are identical
  apart from the package name and the version constant, and the 1.2.1 and
  1.2.3 JSON schemas differ only in two component-type enumerations, so
  NIST's 1.2.3 catalog parses without losing a field. Every export will
  declare the version it was built with.
- **Deterministic UUIDs.** `oscal.UUID(kind, key)` is a UUIDv5 under grc's
  fixed namespace `2e98bbc8-0256-4a99-a220-1f7b4c3f60a6`, keyed on the kind
  of object and a stable key, so an unchanged object keeps its UUID across
  exports and installations.
- **grc's namespace.** Content that is grc's rather than OSCAL's or NIST's
  goes into props and parts with `ns` set to
  `https://carelockconsulting.com/ns/oscal/grc`.
- **Canonical bytes.** Exports are written through `oscal.Canonical`, RFC 8785
  (JSON Canonicalization Scheme), so the same data gives the same bytes.

## The dependency

`github.com/defenseunicorns/go-oscal` (Apache-2.0), only its generated types
package `src/types/oscal-1-2-1`, which imports nothing but `time`.
`internal/oscal` is its only importer; domain modules receive neutral types
through `internal/app/oscal_adapter.go`. The module's CLI (cobra), schema
generator and validator are not linked into the binary
(`TestBinaryLinksOnlyTheOSCALTypes`); the validator, with its embedded
schemas, is used by tests through `internal/oscal/oscaltest`.
compliance-trestle is not a dependency.
