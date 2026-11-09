# OSCAL

grc's database is the system of record. OSCAL (NIST's Open Security Controls
Assessment Language) is an interchange format around it: grc imports NIST's
official SP 800-53 catalog from OSCAL, and exports its catalogs and
crosswalks as OSCAL JSON for other tools. OSCAL is a serializer, as
`POLICY_MODULE_FRAMEWORK.md` puts it; it never becomes the store.

Status: phase 1 (the foundation and the 800-53 catalog import) and phase 2
(the catalog, profile, NFR and Regulation Coverage exports) are built. The
assurance results (phase 3) are designed below, for review before anything
is built; the trestle check and the signing design follow.

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

Documents that refer to one another do it the way NIST's profiles do: through
a back-matter resource, referenced as `#<resource uuid>`, whose one rlink is
the other export's root-relative route (`/controls/oscal/catalog.json`, media
type `application/oscal.catalog+json`) and whose title is the other document's.
The resource UUID is keyed on that href, so a document is the same resource in
every export that names it. A root-relative href resolves against the server
that served the document; a saved copy has to be pointed at its neighbours by
whoever saved it.

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
| `props` | `label` (`AC-2(1)`), `sort-id` (`ac-02.01`), and `status` `withdrawn` when `withdrawn` is 1. Then grc's: `confidentiality`, `integrity` and `availability` (`yes` or `no`, the stored flags), a `threat` per threat in stored order, and `potentially-common-inheritable` when set. |
| `params` | `params_json`, as stored. |
| `parts`: statement | `statement_json` as stored while it still renders to the stored `requirements`. Otherwise (a control never imported from OSCAL, or edited since) a statement whose prose is `requirements`, with the grc prop `prose` = `grc-text`. The database is the record: an edit wins over NIST's structure, and the structure stays stored for when the text is NIST's again. |
| `parts`: guidance | `discussion`, with the grc prop `prose` = `grc-text`: grc stores the text of NIST's guidance, not its part. |
| `parts`: assessment | `assessment_json`, as stored. |
| `parts`: grc `cia-justification` | The CIA justification, when there is one. |
| `parts`: grc `appendix` | A sub-part per non-empty appendix field, in name order (the stored appendix is an object): `title` the field's name as stored (`NIST CSF v2.0`), `name` a token of it (`nist-csf-v2.0`), `prose` its text. `References`, which the DORA Report and the Exceptions Workbook read, is one of them. |
| `links` | `rel="related"` to `#<token>` for each related control. |

grc's own parts and props are plain text, like the `grc-text` statements, and
the import ignores them: it never writes the CIA flags, threats, potentially
common/inheritable, justification or appendix, so they do not affect the
round trip. Not exported: the record key (`source_key`), and the baseline
flags and `mapping_baselines_json`, which the profiles carry. NIST's
back-matter is not stored, so the catalog has none.

### The baseline profiles

One per baseline, in the shape of NIST's own baseline profiles: `imports` the
catalog export through a back-matter resource, `include-controls` `with-ids`
every control the catalog marks in that baseline, and `merge` `as-is`. The
title names the baseline, so the import accepts it in its slot. A baseline no
control is in is a 404: an import with no selection reads as nothing, or to
some tools as everything.

The remarks record the Asset Types page's tier rule, which is not a profile of
its own: TIER 1 and TIER 2 take HIGH, TIER 3 MODERATE, TIER 4 LOW, and TIER 0
and Other no baseline; each profile also says which tiers it serves (PRIVACY,
none). The rule lives in that page's script (`baselineForTier` in
`internal/app/asset_types.go`), and a test holds the profile note to it.

### The Security NFR catalog

| OSCAL | From grc |
|---|---|
| `groups` | One per domain (`id` `domain-<slug>`, `class` `domain`), ordered by name. An NFR without a domain is a control at the catalog's top level. |
| control `id`, `class`, `title` | `nfr-<record key>`, `security-nfr`, the summary. |
| `props` | `label` the NFR id (`1.4`, the record key when there is none); grc's `record-key`, `issue-type`, `domain` and `weight`. |
| `parts` | `statement` the description; grc's `implementation`, `additional-details` and `nist-mapping` (the mapping text as written). |

### The NFR to SP 800-53 mapping collection

One mapping from the NFR catalog export to the 800-53 catalog export, both
named through back-matter resources, with a map per NFR and the control a
reference in its NIST mapping resolved to (the NFR-Control Links table,
overrides included).

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

The catalog has a group per category, in the order the categories first
appear, and a control per section, in position order:

| OSCAL | From the version's snapshot |
|---|---|
| `groups` | One per category: `id` `category-<slug>`, `class` `category`, `title` the category. A section without a category is a control at the catalog's top level. |
| control `id` | The section's ref as a token (`dora-art-6`), with `-<position>` when two refs give one id. |
| `title` | The section's title, or its label when it has none. |
| `props` | `label`, `sort-id` (the position); grc's `ref`, `category` and `segment-confidence`. |
| `parts`: statement | The section's text as extracted. |
| `parts`: grc `finding` | Props `relevant`, `grounded`, `confidence`, `model`, `prompt-hash`, `revision`; parts `requirement`, `commentary`, `gaps`, `quote`. |
| metadata | `title` the regulation's, `version` the report version, remarks the version's note; grc props `framework`, `framework-name`, `source-ref`, `text-sha256`. |

The mapping collection has one mapping to the NFR catalog export and one to
the 800-53 catalog export, each only when it has a map, with a map per
section and catalog item the finding maps it to. The source and targets are
named through back-matter resources.

- A map's `confidence-score` category is the report's (`high`, `medium`,
  `low`), its remarks the rationale, and its grc prop `source` says who made
  it: `seed` (a curated crosswalk) or `model`.
- The mapping's method follows: `human` for seeds only, `automation` for the
  model only, `hybrid` for both.
- Only what the report's own mapping holds is used, and only a mapping whose
  reference the catalog holds (`known`) on a finding whose quote was verified
  against the text (`grounded`; the report shows the others as "unverified
  quote") becomes a map. Each mapping's remarks count what was left out and
  name each one, by reason. A catalog every mapping to which was left out has
  no mapping, and the collection's remarks say so.
- `source-gap-summary` lists the relevant sections with no map to that
  catalog.
- Status: `superseded` for any version but the latest. The latest is
  `complete` when every section has a finding and nothing was left out, and
  `not-complete` otherwise; a mapping's status counts only what it left out.
- The source href is the version's own catalog export
  (`…/oscal/catalog.json?version=N`); the targets are the live NFR and 800-53
  exports, which may have moved since the version was cut.

### The relationship

OSCAL requires a relationship on every map. The 1.2.1 metaschema
(`oscal_mapping-common_metaschema.xml`) allows six values for a relationship
in OSCAL's namespace, and defines them as set relations after NIST IR 8477:
`equal-to` (the same), `equivalent-to` (similar, not necessarily identical),
`subset-of`, `superset-of`, `intersects-with` ("some overlap, but each
includes content that the other does not") and `no-relationship`. The JSON
schema leaves the field a free token, and the constraint applies only to a
relationship in OSCAL's namespace.

grc's links are ungraded: an NFR's author, a curated crosswalk or the
analysis recorded that a target addresses a source, and nothing more. Every
OSCAL value says more than that. Even `intersects-with`, the weakest, says
that neither side contains the other. Every map therefore carries grc's own
relationship, `related`, with the map's `ns` set to grc's namespace, which the
schema provides for ("This allows different organizations to associate
distinct semantics for relationships with the same name"). If a consumer needs
an OSCAL value, `intersects-with` is the closest; the change is
`oscal.Relationship` and the map `ns`. The matching-rationale, which OSCAL
also requires, is `semantic`.

## Assurance results: design for review (phase 3, not built)

This section is the design for exporting Assurance Plans and Audit Findings
as an OSCAL Assessment Plan, Assessment Results and Plan of Action and
Milestones. Nothing in it is built yet; it is written down for review first.

### What OSCAL 1.2.1 requires

These requirements come from go-oscal's embedded 1.2.1 JSON schema, which is
the one the tests validate against, and from the 1.2.1 metaschemas.

| Model | Required by the schema | Required by the model's own remarks |
|---|---|---|
| assessment-plan | `uuid`, `metadata`, `import-ssp` (`href`), `reviewed-controls` with at least one `control-selection`, each `include-all` or `include-controls` | None. `import-ssp`'s `href` may be a back-matter fragment `#uuid` |
| assessment-results | `uuid`, `metadata`, `import-ap` (`href`), `results`, each with `uuid`, `title`, `description`, `start` and `reviewed-controls` | None |
| plan-of-action-and-milestones | `uuid`, `metadata`, `poam-items`, each with `title` and `description` | "Either an OSCAL-based SSP must be imported, or a unique system-id must be specified." A poam-item without a `uuid` is a warning |

The parts these documents are built from have requirements of their own:

| Part | Required |
|---|---|
| `observation` | `uuid`, `description`, `methods` (EXAMINE, INTERVIEW, TEST, UNKNOWN), `collected` |
| `finding` | `uuid`, `title`, `description`, and exactly one `target`: a `type` (`statement-id` or `objective-id`), a `target-id`, and `status.state` `satisfied` or `not-satisfied` |
| `risk` | `uuid`, `title`, `description`, `statement` (the impact), and `status`: `open`, `investigating`, `remediating`, `deviation-requested`, `deviation-approved` or `closed` |
| `response` (a remediation) | `uuid`, `title`, `description`, and `lifecycle`: `recommendation`, `planned` or `completed` |
| `task` | `uuid`, `title`, and `type` `milestone` or `action` |
| `assessment-subject` | `include-all` or `include-subjects` |
| `local-objective` | `control-id` and at least one part |

`poam-item`, `finding`, `observation` and `risk` have **no `parts`**. Their
long text slots are `description`, `statement` (risk) and `remarks`, and a
prop value cannot span lines.

### Meeting `import-ssp` and `import-ap` without an SSP

- **Assessment Plan, `import-ssp`.** The href is `#<uuid>`, pointing at a
  back-matter resource that is the plan's scope population, not an SSP:
  - The resource is titled "Scope population of AP-2027-001 as approved in
    version 2 (not a system security plan)" and carries the grc prop `kind`
    = `approval-snapshot`.
  - Its one rlink is `/assurance-plans/:id/versions/:n`, media type
    `application/json`. That route serves the approval snapshot's stored
    bytes (the plan's design and its population).
  - The rlink's `hashes` give the stored `snapshot_sha256` (SHA-256), so a
    reader can check that the bytes are the ones approved.
  - The `import-ssp` remarks say plainly that grc holds no system security
    plan, and that this plan's scope is its population as approved.

  The population items are also in the plan itself, as `local-definitions`
  inventory items. The model provides those for "data objects … that do not
  appear in the referenced SSP".
- **Assessment Results, `import-ap`.** The href is `#<uuid>`, pointing at a
  back-matter resource whose rlink is grc's own Assessment Plan export for
  the plan version the quarter's sample was selected under
  (`/assurance-plans/:id/oscal/assessment-plan.json?version=N`, read from
  the selections' `plan_version`). That plan is a real one, so nothing is
  pretended.
- **POA&M.** There is no `import-ssp`. The `system-id` has the grc namespace
  as its `identifier-type` and `audit-findings` as its value, and the
  remarks say the POA&M covers grc's Audit Findings register, not a system.
- **Not yet known:** whether trestle follows `import-ssp` and refuses a
  target that is not an SSP. Phase 4's trestle check will show it.

### Routes

| Route | Document | `last-modified` | Refused with |
|---|---|---|---|
| `GET /assurance-plans/:id/oscal/assessment-plan.json` (`?version=N`, the latest approval when absent) | Assessment Plan built from approval snapshot N | The version's `approved_at` (a snapshot never changes) | **409** when the plan was never approved, or no attribute names a control or NFR; 404 for an unknown version |
| `GET /assurance-plans/:id/oscal/assessment-results/q<N>.json` | Assessment Results for quarter N | The export (a signed-off test can still be reopened) | 409 when the quarter has no selection, or its Assessment Plan cannot be exported; 404 for a quarter that is not 1–4 |
| `GET /audit-findings/oscal/poam.json` (`?status=`) | POA&M | The export | 400 for an unknown status; 404 when no finding matches |

- Download names: `grc-assessment-plan-<ref>-v<n>-<date>`,
  `grc-assessment-results-<ref>-q<n>-<date>` and `grc-poam-<date>`, each
  `.oscal.json`.
- All three are read-only and open to signed-in users under the existing
  prefixes (both are protected). The tests check that each refuses an
  anonymous request.
- They are linked from the plan page, the Audit Findings page and Help.
- UUIDs are keyed on what each object is:
  - an inventory item on the plan reference and item ref, so it is the same
    item in every plan version that holds it, and an Assessment Results
    subject resolves in any of them;
  - an observation or a finding on the test ref and attribute ref;
  - a poam-item, its risk and its observation on the AF reference.

### Assessment Plan

| OSCAL | From the approval snapshot |
|---|---|
| metadata | `title` is the plan's title, `version` the approval number (`v2`), and `remarks` the objective. grc props: `reference`, `plan-type`, `function`, `client`, `period-start`, `period-end`, `approved-by`, `coverage-basis`, `population-source`, `seed-commitment` (the seed itself is not in the snapshot), and the AI provenance (`ai-generated`, `model`, `prompt-hash`). No parties and no roles. |
| `import-ssp` | The scope-population resource above. |
| `reviewed-controls` | One `control-selection` per catalog the attributes name: the SP 800-53 ids as NIST tokens, and the NFR keys as `nfr-<key>`. Each has a description naming the catalog and a link to its export. **`include-all` is never used.** When no attribute names a reference, the export is a 409 ("no attribute names a control or NFR"). |
| `local-definitions` / `inventory-items` | One per population item in the snapshot. `description` is the item name. grc props: `item-ref`, `stratum`, `cif`, `business-line`, `hosting`, `internet-facing`, `owner`, `wave`, `last-tested-on`, `source-kind`, `source-ref`. The owner is a prop, not a party. |
| `local-definitions` / `objectives-and-methods` | One `local-objective` per attribute that names a reference. `control-id` is its first reference (the 800-53 ids in order, then the NFR), and `description` is the requirement. Parts: `assessment-objective` with id `<attr>_obj` and the pass criteria as prose, and the grc part `evidence-requested`. The other references are grc props `also-reviews`. |
| `local-definitions` / `activities` | One per attribute. `title` is the attribute ref and domain, `description` the requirement, and one step holds the procedure. The prop `method` is OSCAL's: Inspection and Observation → `EXAMINE`, Inquiry → `INTERVIEW`, Re-performance → `TEST`; the grc prop `method` keeps grc's own word. `related-controls` lists its references. grc props: `applies-to` (a stratum code each), `regulatory-refs`, and the AI provenance. |
| `assessment-subjects` | One per stratum that has items in the snapshot: `type` `inventory-item`, `include-subjects` the stratum's items, `description` its definition, and grc props `code` and `name`. A stratum approved on its planned size alone, with no items yet, is not a subject: OSCAL would need `include-all`, which claims everything. |
| `tasks` | One per plan step, the generated quarter steps included. `type` is `milestone` for a milestone step and `action` otherwise. `timing` is `within-date-range` (start, end) or `on-date`. grc props: `step-ref`, `phase`, `responsible-role`, `accountable-role`, `deliverable`, `refs`, `status`. |
| `terms-and-conditions` / grc parts | These go in grc's namespace, as the prompt asks (the allowed-values constraint covers only parts in OSCAL's namespace): `parameters` (the per-plan thresholds as props); `sampling` (a sub-part per stratum, items or not, with confidence, tolerable rate, floor, efforts, waves Q1–Q4, top-ups, cycle years, planned size and takes); `quarterly-cycle` (a sub-part per quarter: the 1LoD window and deadline, selection, fieldwork start and end, findings agreed, report, committee, retest and overhead days); `raci` (a sub-part per activity, a prop per role holding its letter); `cadence`; `escalations`; `regulatory-basis`; `decisions-requested`. |
| `back-matter` | The scope-population resource. |

### Assessment Results (one per quarter)

| OSCAL | From the quarter's tests |
|---|---|
| metadata | `title` "<plan>: Q<n> results". grc props: `reference`, `quarter`, `committee`. |
| `import-ap` | The resource above, for the plan version the quarter was selected under. |
| `results` | One. `title` is "Quarter N" and `description` gives the counts. `start` is the quarter's fieldwork start (or its selection date), and `end` its fieldwork end. `reviewed-controls` holds the references of the attributes with results this quarter. |
| Which tests | Only tests signed off by QA (`qa_date` set). Tests selected but not signed off are counted and named in the result's remarks. |
| `observations` | One per attribute result of a signed-off test (Pass, Fail, N/A; a blank is skipped). `title` is "<test ref> / <attr ref>", and `description` the result note, or the result itself when there is no note. `methods` comes from the attribute, as above, and `types` is `control-objective`. `subjects` is the item, using the inventory-item UUID the plan defines. `collected` is the fieldwork completion date, or the QA date when that is blank. `relevant-evidence` is the evidence summary. grc props: `result`, `test-ref`, `test-type`, `tester`, `qa-reviewer`, `qa-date`, `stratum`, `basis`. |
| `findings` | One per Fail. `target` is `objective-id` `<attr>_obj`, the attribute's objective in the plan, with state `not-satisfied` and reason `fail`. `related-observations` is that observation. grc props: `audit-finding` (the `AF-YYYY-NNN` record the Fail was raised as) and `severity`; a Fail not yet raised has no such prop, and the remarks say so. A Fail on an attribute that names no reference has no objective to target, so it stays an observation and is counted in the remarks. |
| Not exported | Thematic conclusions, which go to the risk register (out of scope), and the deviation rule's yearly conclusions. |

### POA&M

The POA&M covers findings with status Open, In Remediation, Pending
Validation, Reopened or Risk Accepted. `?status=` picks one status instead.
Draft is never exported, because it has not been issued; Closed is exported
only when asked for.

| OSCAL | From the finding |
|---|---|
| `system-id` | As above. |
| `poam-items` | One per finding. `title` is the finding's title. **`description` holds the criteria, condition, cause and effect as four labelled paragraphs, verbatim and as plain text**, because a poam-item has no parts. grc props: `reference` (AF-YYYY-NNN), `status`, `severity`, `finding-type`, `deficiency-type`, `source`, `engagement`, `auditor`, `root-cause-category`, `business-unit`, `process-area`, `owner`, `management-response`, `identified-date`, `report-date`, `original-due-date`, `due-date`, `extension-count`, `repeat-finding`, `prior-reference`, `origin-ref`, and a `control` per linked control. `related-observations` and `related-risks` point to the two objects below. |
| `observations` | One per finding. `description` is the condition and `collected` the identified date. `methods` is `UNKNOWN`: grc does not record how an auditor found something, and OSCAL defines that value for this case. grc prop: `source`. |
| `risks` | One per finding. `title` is the title, `description` the condition, `statement` the effect, `status` per the table below, and `deadline` the due date. Remediations: the recommendation as a `recommendation` response, and the management action plan as a `planned` response (`completed` once every action is Validated or Cancelled and at least one is Validated). The plan's description is the management response text, and it has a `task` (type `action`) per action, with `on-date` timing at the action's due date. The task's grc props are `action-type`, `owner`, `status`, `completed-date` and `evidence`. Risk acceptance and closure go in grc props (`accepted-by`, `acceptance-expiry`, `validated-by`, `validation-date`), with the acceptance rationale and the closure evidence in `remarks`. |

| grc status | OSCAL risk status |
|---|---|
| Open, Reopened | `open` |
| In Remediation | `remediating` |
| Pending Validation | `remediating`: the fix is in, and OSCAL has no "validating" state. The grc `status` prop says Pending Validation. |
| Risk Accepted, acceptance in force | `deviation-approved` (OSCAL: an open risk with an approved deviation) |
| Risk Accepted, acceptance expired | `open`, with the grc prop `acceptance-expired` `yes` |
| Closed (only with `?status=Closed`) | `closed` |
| Draft | Not exported |

### Where this differs from the prompt, and what is open

1. **No parts.** The prompt asked for criteria, condition, cause and effect
   as parts, but none of poam-item, risk, observation or finding has parts
   in 1.2.1. The proposal uses the native slots instead: the condition as
   the observation and the risk's description, the effect as the risk's
   statement, the recommendation and the action plan as responses. All four
   texts also go, labelled, in the poam-item's description.
2. **Plans whose attributes name no control or NFR cannot honestly give an
   Assessment Plan.** That is 4 of the 6 built-in templates as shipped:
   policy review, RCSA, remediation roadmap and resilience testing. OSCAL
   requires reviewed controls, and `include-all` would claim every control.
   The proposal is a 409 that names the gap, and with no Assessment Plan
   there are no Assessment Results either. The alternative is to give
   those templates' attributes their references first.
3. **The POA&M's `system-id`** (`audit-findings` in grc's namespace) meets
   the "unique system-id" rule with an identifier for the register, not a
   system. It is unique to grc, not to the installation.
4. **An expired risk acceptance** exports as `open`, not
   `deviation-approved`.
5. **RACI** goes in the grc namespace, as the prompt asks, rather than as
   OSCAL roles, even though the role names are stored.
6. **Observation → EXAMINE.** 800-53A's EXAMINE includes observing.

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
