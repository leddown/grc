# Assurance Plans

The module at `/assurance-plans` (Compliance & Risk) builds and runs the annual
plans a second-line ICT risk function produces: what it will review, when, on
which population, with what sample, and with what result. The reference plan is
a 2LoD Security NFR controls testing plan. Every Tier 1 and Tier 2 asset's
Security NFR data is attested by 1LoD during the year. 2LoD reviews the data
quality of every submission and sample-tests the NFRs each quarter. The other
plan types (risk assessment cycle, policy review, resilience testing,
remediation roadmap) use the same record. They differ in the template they are
seeded from and in whether sampling is on.

Code: `internal/assuranceplan`. Tables: `assurance_plans` and the
`assurance_plan_*` tables listed under [Data model](#data-model).

**Build status.** The module is built in phases on `feature/assurance-plans`.
Phase 1 (core) is in: the plan, its steps and Gantt, the quarterly cycle,
governance, lifecycle and approval gate, versions, history, clone, and the
Controls testing and Blank types with the worked 2027 example. Phase 2
(sampling and scope) is in: strata, the scope population and its CSV paste
import, the sampling engine, quarterly allocation and capacity, the freeze and
the frozen quarterly selection. Phase 3 (execution) is in: the test script and
its seeding from the catalogs, tests and results with the evidence SLA and QA
sign-off, the deviation rule, "Raise finding" into Audit Findings, the thematic
push to the risk register, and the dashboard. Phase 4 (outputs) is in: the
regulatory basis, the JSON and CSV exports, the committee approval pack, the
quarterly and annual reports, and the knowledge kinds. Phase 5 (plan types) is
in: the ICT risk assessment cycle (RCSA), policy review, resilience testing and
remediation roadmap types, the plan type settings and stratum flags they run
on, multi-year cycles, coverage measured on signed-off tests, and populations
proposed from the policy library and from Audit Findings. Phase 6 (AI assist)
is in: a plan drafted from a brief, test attributes proposed from a shortlist of
the catalogs, and committee narrative for the pack and the reports. Every answer
is a stored proposal that a person accepts, edits or rejects.

The module produces:

- the working plan in the UI
- a committee approval pack (16:9 PDF)
- a quarterly progress report
- an annual conclusion that feeds the yearly ICT risk management framework
  review
- JSON and CSV exports

## Practice it follows

| Practice | Source | How the module applies it |
|---|---|---|
| The control function plans its work, risk-based, and the plan is **approved by the governing body or its committee** before work starts | IIA Global Internal Audit Standards, Standards 9.4 and 9.5; DORA Art. 6(4), 6(5) | Lifecycle Draft → In review → Approved → Active → Closed. There is no edge that skips review. Approval runs a gate and stores an immutable snapshot. |
| The ICT risk control function is **independent** of the functions it reviews | DORA Art. 6(4); RTS 2024/1774 Art. 2(2)(c) | Separation of duties: the approver cannot be the author, and the QA reviewer cannot be the tester, whenever multi-user authentication is on. In single-user mode both names are recorded and a warning is shown. |
| The ICT asset inventory, with **classification and its link to critical or important functions**, is complete and kept up to date | DORA Art. 8(1), 8(4); RTS 2024/1774 Arts. 4–5 | The scope population carries a stratum and the CIF it supports. 100% of each stratum is attested by 1LoD in waves, and 2LoD reviews the data quality of every submission. |
| Each test records its **population, sampling method and sample-size rationale**, and the sample is selected so that it is representative | AICPA Audit Guide *Audit Sampling* (attribute sampling); ISACA IS Audit and Assurance Guideline 2208 *Sampling*; IIA Standard 14.3 | Sizes come from confidence and tolerable deviation rate, with finite-population correction and a coverage floor. Every figure is shown with its formula and inputs. |
| The selection can be **re-performed** by someone else | AICPA / ISACA (sampling documentation); IIA Standard 14.6 (documentation sufficient to support conclusions) | Hash-ranked selection from a seed stored on the plan. Selected items are frozen and written to history. Internal Audit re-performs the selection from the seed and the frozen population. |
| An attribute test concludes on the **deviations** found, and a deviation either extends the sample or leads to a not-effective conclusion | AICPA attribute sampling (zero-expected-deviation plans); common SOX/ITGC practice | A deviation rule per attribute per stratum, cumulative for the year: 0 → effective, 1 → extend the sample, ≥2 → not effective (thematic finding). The thresholds are plan parameters. |
| Assessment uses defined **methods**: examine, interview, test | NIST SP 800-53A Rev. 5 §2.3 (assessment methods and objects) | Every test attribute names its method: Inspection, Inquiry, Observation or Re-performance. It also states the evidence requested and the pass criteria. |
| Each **testing cadence records its risk rationale**, not only its dates | DORA Arts. 24–25 (risk-based testing programme); RTS 2024/1774 Art. 27 | Each stratum states its confidence, tolerable rate and floor, and each risk-based top-up carries its rationale. All of these are on the Sampling tab and in the pack. |
| Every exception becomes a **finding with an owner and a due date**, tracked to validated closure | IIA Standard 15.2; DORA Art. 6(5) | "Raise finding" on a failed attribute creates a finding in Audit Findings (deficiency type Operating Effectiveness). It does not create a parallel register. |
| Progress is **reported to the governing body** on a fixed cadence, with escalation triggers agreed in advance | IIA Standard 11.3; DORA Art. 5(2) | Quarterly report, governance cadence, escalation triggers and a dashboard with coverage RAG. Status colour always comes with its word. |
| The year's results feed the **annual review of the ICT risk management framework** | DORA Art. 6(5); RTS 2024/1774 Art. 27 | The annual conclusion report states population, coverage achieved, tests performed, results per attribute and stratum, findings and the conclusion. It is labelled as an input to the RMF review report. |

## Concepts

- **Plan**: one annual (or multi-year) plan. A plan has a reference
  (`AP-YYYY-NNN`), a type, the function that owns it (for example "2LoD CISO
  Office"), and optionally a client.
- **Plan type**: a template stored as data, one embedded JSON file per type
  in `internal/assuranceplan/templates/` (like the Policy Studio templates). It
  seeds phases, steps, a default quarterly cycle, strata, RACI and governance,
  and it sets which features are on. There is no per-type code path. A worked
  example is a template with `"example": true` and fixed dates.

  A template's dates are expressions, resolved against the plan's period and
  quarters and rolled onto business days:

  - `period_start-53d`, `period_end`, `closing_date-18bd`: an anchor, then an
    optional offset in days (`d`), weeks (`w`) or business days (`bd`). The
    anchors are `period_start`, `period_end`, `mobilise_start` and
    `closing_date`.
  - `lod1_deadline+5bd`, `q3.lod1_deadline`: a quarter's date, the quarter's own
    for a step marked `quarterly`, or a named quarter's (`q1.` to `q4.`).
  - A date anchored on `period_end` with no positive offset rolls back, so a
    year-end milestone never slips into the next year.

  A `quarterly` step is repeated for each quarter with `{q}` replaced by the
  quarter number (`{q}.3` becomes 1.3, 2.3…); `skip_quarters` leaves some out
  (the Q4 report is the annual conclusion). `{year}`, `{next_year}` and
  `{period_end_long}` fill text.
- **Steps**: the plan itself. Each step is an activity, a milestone or an
  ongoing item, with roles, dates and a deliverable. The Gantt is drawn from
  the steps.
- **Quarters**: the operating cycle, one row per quarter. A row holds the 1LoD
  window and deadline, the selection date, fieldwork, the date findings are
  agreed, the report date and the committee date.
- **Strata**: subdivisions of the population (Tier 1, Tier 2). Each stratum has
  its own sampling parameters, waves and top-ups, and a planned size that sizes
  it until its population is loaded.
- **Population**: the items in scope (assets, business units, policies,
  systems, findings), each in one stratum and one 1LoD wave.
- **Test attributes**: the test script. They are stored on the plan, so a later
  catalog edit does not change an approved plan.
- **Selections**: the items picked for 2LoD testing in a quarter, frozen when
  picked.
- **Tests**: one per selection, with one result per applicable attribute.
- **Governance**: a RACI matrix, a reporting cadence, escalation triggers and
  the regulatory basis. The regulatory basis is a table of what the
  regulation asks of the control function, its reference, and how the plan
  meets it. It is the approval pack's regulatory slide. Every built-in type
  carries its own rows; they paraphrase the articles and standards and are no
  substitute for the text.
- **History** and **versions**: an append-only event log, and an immutable JSON
  snapshot at each approval.

## Plan types

Six types ship, each a JSON file in `internal/assuranceplan/templates/`. They
differ in their data, not in code: the same steps, strata, population,
selection, tests, deviation rule, reports and exports serve all of them. What
sets them apart is a handful of settings on the plan and flags on each stratum.

| Type | Population | Strata | Sampling | Coverage measured on | Test types |
|---|---|---|---|---|---|
| Controls testing (`controls-testing`) | ICT assets | Tier 1, Tier 2 | on | 1LoD attestation | Controls test |
| ICT risk assessment cycle (`rcsa`) | Business units or processes | Critical (BU-C), Other (BU-O) | on, 90%/10% and 80%/15%: a smaller sample than controls testing, for 2LoD review and challenge | 1LoD attestation (the self-assessment submitted) | Review and challenge |
| Policy review (`policy-review`) | Policy documents due for review, proposed from the policy library | Policies (POL), Standards and procedures (STD) | off | Test signed off by QA | Policy review |
| Resilience testing (`resilience-testing`) | ICT systems supporting critical or important functions | Systems (SYS), TLPT scopes (TLPT, a three-year cycle) | off | Test signed off by QA | Vulnerability assessment, Vulnerability scan, Penetration test, Scenario-based test, BCP / DR test, Threat-led penetration test (TLPT) |
| Remediation roadmap (`remediation-roadmap`) | Open audit findings, proposed from Audit Findings | Critical and High (F-CH), Medium and Low (F-ML) | off | Test signed off by QA (the closure validated) | Closure validation |
| Blank (`blank`) | — | none | — | 1LoD attestation | none |

The worked 2027 example (`dora-nfr-2027`) is the controls testing type with
Appendix A's data.

**Plan settings** (on the header, under "Plan type settings"):

- `test_types`: the kinds of test the plan runs, one per line. A test records
  one of them; the first is the default for a new test. A plan with none
  accepts any type of up to 60 characters. At most 20 types.
- `coverage_basis`: what counts an item as covered for the coverage RAG, the
  dashboard, the reports and the knowledge record.
  - `attestation` (the default): the item's 1LoD attestation date. The
    controls testing and RCSA types work this way: 1LoD covers 100% and 2LoD
    samples.
  - `tests`: the date the item's test was signed off by QA. The types without
    sampling work this way: the 2LoD test is the coverage.
- `population_source`: where a population can be proposed from, `policies`,
  `findings` or none (see [Proposed populations](#proposed-populations)).

A header saved without these keys keeps them; `"population_source": "none"`
clears the source.

**Stratum flags** (on the Sampling tab's strata editor):

- `sampling_enabled` off: every item due is tested, with basis Full coverage
  (see [Sampling method](#sampling-method)).
- `cycle_years` (0–10; 0 or 1 is every year): a multi-year cycle. An item
  tested on its last test date is not due again until that date plus the
  cycle, and counts as covered meanwhile. The resilience type's TLPT stratum
  uses 3, for DORA Art. 26(1)'s threat-led penetration test at least every
  three years.
- `takes`: the values a proposal places in the stratum: document types
  (`policy`, `standard`, `procedure`, `work_instruction`, `guideline`, `tra`)
  for the policies source, severities (`Critical`, `High`, `Medium`, `Low`) for
  the findings source. At most 20, each up to 40 characters without commas or
  semicolons; matched without regard to case.

**Item and test fields:**

- An item's `last_tested_on` is the date it was last tested, the anchor of its
  cycle. A clone sets it to the latest QA sign-off of the year being cloned,
  so this year's test is next year's last test.
- A test's `test_type` is one of the plan's test types, matched without regard
  to case and stored in the plan's spelling. A save without a type keeps the
  test's type; a type the plan does not list is refused (400).

## Data model

All tables are `assurance_plan*`. Child rows join on `plan_id` (autoincrement),
which is why the module is excluded from `-sync-to` / `-sync-from` (see
[Backup and sync](#backup-and-sync)).

| Table | Holds | Key columns |
|---|---|---|
| `assurance_plans` | The plan header and its parameters | `reference` (unique), `title`, `plan_type`, `function_name`, `client_profile_id` (0 = none), `period_start`, `period_end`, `mobilise_start`, `closing_date`, `objective`, `decisions_requested`, `owner_role`, `accountable_role`, `approval_forum`, `regulatory_refs`, `test_types` (JSON list), `coverage_basis` (attestation / tests), `population_source` (empty / policies / findings), `status`, `revision`, `revision_status`, `revision_author`, `change_reason`, `author`, `approved_by`, `approved_at`, `activated_at`, `closed_at`, `selection_seed`, `non_working_dates`; the parameters listed below; `ai_generated`, `model`, `prompt_hash`; `created_*`, `updated_*` |
| `assurance_plan_steps` | Plan steps | `plan_id`, `ordinal`, `step_ref`, `phase`, `title`, `step_type` (activity / milestone / ongoing), `responsible_role`, `accountable_role`, `start_date`, `end_date`, `deliverable`, `refs`, `status`, `quarter` (0 = not tied to a quarter), `generated` |
| `assurance_plan_quarters` | The operating cycle | `plan_id`, `quarter` (1–4), `lod1_window_start`, `lod1_deadline`, `selection_date`, `fieldwork_start`, `fieldwork_end`, `findings_agreed_by`, `report_date`, `committee_label`, `retest_days`, `overhead_days`, `selected_at`, `selected_by` |
| `assurance_plan_strata` | Strata and their sampling parameters | `plan_id`, `ordinal`, `code` (unique per plan), `name`, `definition`, `sampling_enabled`, `confidence`, `tolerable_rate`, `floor_pct`, `effort_per_test`, `effort_per_review`, `wave_q1`..`wave_q4`, `topup_q1`..`topup_q4`, `planned_size`, `cycle_years` (0 = every year), `takes` (comma-separated) |
| `assurance_plan_population` | The scope population | `plan_id`, `item_ref` (unique per plan), `name`, `stratum_code`, `cif`, `business_line`, `hosting`, `internet_facing`, `owner`, `wave_quarter`, `attested_on`, `dq_status`, `dq_reviewed_on`, `topup_quarter`, `topup_rationale`, `last_tested_on`, `source_kind` (`policy_document` / `audit_finding`), `source_ref` (the record's id) |
| `assurance_plan_attributes` | The test script | `plan_id`, `ordinal`, `attr_ref` (unique per plan), `domain`, `requirement`, `test_procedure`, `method` (Inspection / Inquiry / Observation / Re-performance), `evidence_requested`, `pass_criteria`, `applies_to` (stratum codes, comma-separated; empty = every stratum), `regulatory_refs`, `control_ids`, `nfr_key` (empty = plan-local), `ai_generated`, `model`, `prompt_hash`. Unresolved references are worked out on read, not stored. |
| `assurance_plan_selections` | Frozen picks | `plan_id`, `quarter`, `population_id` (on delete cascade), `item_ref`, `stratum_code`, `basis` (Statistical / Risk-based top-up / Full coverage), `rank_order` (the item's place in its stratum and wave's ranking; 0 for a top-up), `rank_hash`, `plan_version` (the approved version the draw was made under), `selected_at`, `selected_by`; unique (`plan_id`, `population_id`). The seed is on the plan, not on each pick. |
| `assurance_plan_tests` | One test per selection | `plan_id`, `selection_id` (unique, on delete cascade), `test_ref` (unique per plan), `test_type`, `tester`, `evidence_requested_on`, `evidence_due_on`, `evidence_received_on`, `fieldwork_completed_on`, `evidence_summary`, `qa_reviewer`, `qa_date`, `qa_note`, `notes`, `created_*`, `updated_*` |
| `assurance_plan_results` | One result per attribute per test | `plan_id`, `test_id`, `attribute_id` (unique together, both on delete cascade), `result` (Pass / Fail / N/A / empty), `note`, `finding_id`, `finding_ref`, `severity` |
| `assurance_plan_thematic` | Not-effective conclusions pushed to the risk register | `plan_id`, `attribute_id`, `stratum_code` (unique together), `risk_id`, `risk_ref`, `deviations`, `note`, `pushed_at`, `pushed_by` |
| `assurance_plan_raci` | RACI rows | `plan_id`, `ordinal`, `activity`, `cells` (JSON object, role → R, A, C, I or A/R). The plan stores the role columns in `raci_roles`. |
| `assurance_plan_cadence` | Reporting cadence | `plan_id`, `ordinal`, `forum`, `frequency`, `content`, `owner` |
| `assurance_plan_escalations` | Escalation triggers | `plan_id`, `ordinal`, `trigger_text`, `escalate_to`, `within_text` |
| `assurance_plan_basis` | Regulatory basis (phase 4) | `plan_id`, `ordinal`, `requirement`, `reference`, `how_met` |
| `assurance_plan_history` | Every design, status, selection and result change | `plan_id`, `at`, `actor`, `event`, `subject`, `from_value`, `to_value`, `note` |
| `assurance_plan_versions` | Approval snapshots, immutable | `plan_id`, `number`, `label`, `approved_by`, `approved_at`, `change_reason`, `snapshot_json`, `snapshot_sha256` |
| `assurance_plan_ai_proposals` | AI proposals and their decisions | `plan_id`, `kind` (draft / attributes / narrative), `document` (a narrative's `pack`, `q1`..`q4` or `annual`), `revision` (the plan's design revision when asked), `input_json` (the brief), `model`, `prompt_hash` (SHA-256 of the system prompt and the prompt), `payload_json` (the answer after the server's checks), `status` (proposed / accepted / rejected), `applied_json` (what was accepted, with the person's edits), `decision_note`, `created_*`, `decided_by`, `decided_at` |

Phase 3 adds one column to an existing table: `audit_findings.origin_ref` (for
example `AP-2027-001 / T27-Q1-004 / NFR-04`). It is the plan side of the
two-way link. It is set when the finding is created and an edit in Audit
Findings never changes it; the finding's page shows it as "Raised from", linked
back to the plan. Existing databases get it through `ensureColumn` and
`ADD COLUMN IF NOT EXISTS`.

Phase 5 adds columns to the module's own tables, the same way:
`assurance_plans.test_types`, `coverage_basis` and `population_source`,
`assurance_plan_strata.cycle_years` and `takes`,
`assurance_plan_population.last_tested_on`, and `assurance_plan_tests.test_type`.
A plan from before phase 5 reads as an attestation-basis plan with no test
types and no source.

**Plan parameters** (columns on `assurance_plans`, defaults in brackets):

- capacity: `tester_fte` (2.0), `days_per_fte_quarter` (50),
  `utilisation_threshold` (0.85)
- dashboard: `rag_amber_points` (10)
- test result: `result_partial_min` (1), `result_ineffective_min` (3)
- deviation rule: `deviation_extend_at` (1), `deviation_thematic_at` (2),
  `extend_pct` (50)
- evidence: `evidence_sla_days` (10)

**Computed on read, never stored:**

- sample sizes, quarter allocation, reviews due and capacity
- a test's counts and overall result
- deviation conclusions
- coverage RAG
- step lateness

## Lifecycle

`Draft → In review → Approved → Active → Closed`

| Move | Requires |
|---|---|
| Draft → In review | — |
| In review → Draft | — (returned for changes) |
| In review → Approved | The approval gate (below) and separation of duties. Writes version *n* and records `approved_by` / `approved_at`. |
| Approved → Active | Admin action. Execution can start on an Approved plan, and the first quarterly selection activates it. |
| Active → Closed | Admin action, refused while a revision is open. After closing, nothing on the plan changes. |

Only a Draft that was never approved can be deleted; an approved plan is
evidence and is closed instead.

- Create ignores any status the client sends, and there is no edge from Draft
  to Approved. Both follow `policydocs`.
- `author` is taken from the session on create and never from the request
  body. Neither are status, revision, approval fields or the seed on a header
  edit: they are carried from the stored row.

**Design lock.** Once the plan is Approved, its design is locked: steps (other
than their status), quarters, strata (including their planned top-up counts),
parameters, attributes and governance. Execution data stays editable:

- step status
- attestation dates and DQ status
- the top-up flags on items, until their quarter is selected
- selections
- tests and results
- QA sign-off
- finding links

**The population** follows its own rules, because the reference plan completes
it after approval and freezes it at step 0.9:

- Membership is adding, removing, or moving an item to another stratum or
  wave. It is open while the plan is a Draft, closed while the plan or a
  revision is In review, open again on an approved plan until the freeze, and
  after the freeze open only while a change request's revision is a Draft.
- A wave whose quarter has been selected cannot gain, lose or move an item.
- A selected item stays in the population, and its stratum is fixed.
- A top-up flag needs its rationale. It cannot be set for a quarter that has
  been selected, or on an item that is already a pick. A top-up that has been
  selected cannot be withdrawn.
- Descriptions (name, CIF, business line, hosting, internet-facing, owner),
  attestation dates and the DQ review stay editable until the plan is closed.

The approval snapshot records the population at that point. The draw is made
from the population that was frozen, and the freeze writes that population's
membership SHA-256 to history.

**Change control.** A design change on an Approved or Active plan starts with
a change request that gives a reason. The request sets `revision_status` to
Draft and increments `revision`. Design fields unlock, and execution carries on
against the plan of record. The revision then goes through In review →
Approved again, which writes version *n+1*. The plan's own `status` never moves
backwards, so an Active plan stays Active while it is being revised. The
approved snapshots in `assurance_plan_versions` are never rewritten.

**Separation of duties.** It is enforced when multi-user authentication is
active, wired the same way as `policydocs.SetRequireSeparateApprover`:

- Under authentication, the approver is the signed-in identity.
- The approver cannot be the author of the revision being approved: the plan's
  creator for version 1, the person who raised the change request for later
  versions.
- QA sign-off records the signed-in user as QA reviewer, and refuses when that
  user is the test's tester.
- In single-user mode both names are recorded and the page shows a warning.

### Approval gate

A refused approval is an HTTP 422 response with the findings list, the same
shape as `policydocs.ApprovalError`.

**Blocking findings:**

- owner role, accountable role, approval forum, period start or period end
  missing, or a period that ends before it starts
- a plan with no steps
- a stratum's waves do not sum to 100%. This applies to every stratum, sampled
  or not, because the waves also schedule the 1LoD attestations.
- a sampled stratum's confidence is outside (0,1), its tolerable rate is
  outside (0,1), or its floor is outside [0,1]
- a step with no responsible role, with no dates (a milestone needs one
  date), or ending before it starts
- a quarter missing its 1LoD deadline or its report date

**Advisory findings (shown, not blocking):**

- a step outside the plan's span (mobilisation start to closing date)
- a quarter whose dates are out of order
- a RACI activity with no Accountable role, or with more than one
- utilisation above the threshold in any quarter
- attribute references that do not resolve against the control or NFR
  catalogs, one finding per attribute (only when both catalogs are wired in;
  without them nothing is checked and nothing is claimed)
- a plan with strata but no test attributes
- an attribute with no pass criteria
- a stratum with no population yet (the finding says whether it is sized on its
  planned size or at zero)
- a sampled stratum whose wave counts differ from its wave percentages, giving
  both (a stratum without sampling is planned by its wave counts)
- items with no 1LoD wave, which are never due for attestation or drawn
- items inside their multi-year cycle, not due in the period (`in_cycle`)
- a stratum that takes a value its plan's source does not have, such as a
  severity of "Severe" (`takes_unknown`)
- a plan with a population source where no stratum takes anything from it
  (`source_unmapped`)

Planned top-ups with no items flagged for them are not a gate finding. Items are
flagged for a top-up during the year, so the mismatch is a warning when that
quarter is selected, in the selection's result and in history.

### Clone for next year

Clone carries over:

- the header and parameters
- strata, attributes, governance, quarters and the step template

It also:

- moves the period one year, without rolling: a period boundary is a date, not
  a working day
- moves every working date (steps, quarters, mobilisation, closing date) one
  year and rolls it forward to a business day, or back when rolling forward
  would carry a date inside the period past its end (31 December 2028 is a
  Sunday, so the year-end milestone lands on Friday 29 December)
- moves the non-working dates one year, and says in the clone's history to
  check movable holidays such as Easter
- moves on by one every year mentioned in the title, objective, decisions,
  step titles, deliverables and committee labels ("Draft the 2028 plan"
  becomes "Draft the 2029 plan"); a year inside a citation such as
  `2024/1774` is left alone, and regulatory references are not touched
- keeps the population: reference, name, stratum, wave, CIF, business line,
  hosting, internet-facing, owner, last tested date and source
- sets each item's last tested date to the QA sign-off date of its test this
  year, when that is later, so a multi-year cycle runs on

It drops:

- step status (every step starts Not started)
- attestation dates, DQ status and top-up flags (a top-up is a risk call for
  the year it was made in)
- selections, tests, results, finding links and thematic pushes
- history, versions and AI proposals, and the plan's own AI-generated mark. An
  attribute added from an AI proposal keeps its marker, model and prompt hash,
  because its text is still what was proposed and accepted.

The clone starts in Draft with a new reference (in the new year), a new
author and no seed: a new seed is drawn when its population is frozen. Its
history notes how many population items were copied.

## Sampling method

The functions are in `sampling.go`. They are pure, so they can be tested
against the textbook tables.

**Statistical size** (attribute sampling, zero expected deviations):

    n0 = ceil( ln(1 − confidence) / ln(1 − tolerable_rate) )

95%/5% → 59, 95%/10% → 29, 90%/5% → 45, 90%/10% → 22.

**N** is the number of the stratum's population items. Before the population
is loaded, the stratum's planned size stands in, so a plan can be sized,
reviewed and approved on the inventory's expected count. The Sampling tab says
which one each figure uses.

**Finite-population correction:**

    n = ceil( n0 / (1 + (n0 − 1) / N) ),   n = 0 when N = 0

**Floor and annual sample:**

    floor  = ceil(floor_pct × N)
    annual = min(N, max(n, floor))
    tests  = annual + Σ top-ups

When sampling is off for a stratum (the coverage-only plan types), `annual = N`
and every item is selected with basis "Full coverage". This is how one code
path serves all six plan types. Once its population is loaded, such a stratum
is planned by the items in each wave, not by the wave percentages: the tests
per quarter, the reviews due and the cumulative target are the wave counts, so
the plan and the selection agree. Before the population is loaded, the planned
size and the percentages stand in.

**Multi-year cycle.** In a stratum with `cycle_years` above 1, an item is due
in the period when it has never been tested or its cycle lapses by the period
end (last tested + cycle years ≤ period end). An item not due and with no wave
is counted as in cycle and left out of N: it is not sized, scheduled or drawn,
and it counts as covered until its cycle lapses. An item not due that has a
wave anyway is sized and drawn like the others. A TLPT scope last tested on
1 October 2024 lapses on 1 October 2027 and is due in the 2027 plan; one tested
on 15 June 2025 is in cycle until 15 June 2028.

**Quarter allocation** uses cumulative rounding, so the quarters always sum to
the annual figure:

    cum_i = Σ_{k≤i} wave_k
    q_i   = ceil(round(annual × cum_i, 6)) − ceil(round(annual × cum_{i−1}, 6))

The last cumulative is taken as exactly 1, so the quarters always sum to the
annual figure.

The `round(…, 6)` matters. Without it, waves of 20/40/30/10% accumulate to
1.0000000000000002, so an annual sample of 1 is allocated as 2. Mid-year the
drift moves items between quarters: 5 × (0.2 + 0.4) is 3.0000000000000004, which
puts a third item into Q2 that belongs in Q3. The common 35/35/30 split happens
to sum to exactly 1 in IEEE doubles, so it is not the example the tests use.
Wave sums are checked with a tolerance of 10⁻⁶.

**1LoD reviews due** use the same scheme over N, with `round` in place of
`ceil`.

**Capacity per quarter:**

    effort_q    = Σ_strata (q_i + topup_i) × effort_per_test
                + Σ_strata reviews_due_i × effort_per_review
                + retest_days_q + overhead_days_q
    capacity_q  = tester_fte × days_per_fte_quarter
    utilisation = effort_q / capacity_q    (flagged above utilisation_threshold)

**Selection** is deterministic and can be re-performed. Within each (stratum,
wave quarter), items are ranked by

    SHA-256( seed ‖ 0x1F ‖ stratum_code ‖ 0x1F ‖ item_ref )

lowest first, and the first `q_i` that have not already been selected this year
are taken. The seed enters the hash as its hex text.

- The 0x1F unit separator is there so that `T1`+`0` and `T10`+`` cannot hash
  alike.
- A hash, not `math/rand`: it needs no gosec exception. Each item's rank
  depends only on the seed and its own reference, so adding an item never
  reshuffles the others.
- The seed is 32 random bytes from `crypto/rand`, hex-encoded and recorded when
  the population is frozen (step 0.9 of the example plan). Drawing it then,
  and not when the plan is created, means nobody can know the seed while the
  population can still change.

**The freeze** (`POST …/population/freeze`, "Freeze population and record
seed") happens once. It needs:

- an Approved or Active plan with no revision open
- a population that is not empty
- a wave on every item due in the period (an item inside its multi-year cycle
  needs none)
- every stratum's waves summing to 100%

It writes a `population frozen` history event with the item count, the
membership SHA-256 (reference, stratum and wave of every item) and the seed
commitment.

**Commit, then reveal.** Anyone with access to the page sees the seed's
commitment, the SHA-256 of its hex text. They see the seed itself only once
every quarter has been selected or the plan is closed. Until then, every plan
JSON the module serves (list, plan, mutations, the selection result) leaves the
seed out. A reader who knew the seed could work out which items the next
quarter's draw will take. That includes the 1LoD owner of an item, before
their evidence is requested. Administrators can read the seed at any time
through `GET …/seed` ("Show seed"). Internal Audit can re-perform a draw, and
check the revealed seed against the commitment in the freeze event.

The selection itself:

- "Select Q*n* sample" is an explicit admin action on an Approved or Active
  plan whose population is frozen and which has no revision open. It writes
  the selections, stamps `selected_at` / `selected_by` on the quarter, and
  records them in history with the seed commitment and the plan version. After
  that the quarter is frozen. A second run is refused rather than recomputed.
  So is a concurrent one: the stamp is only set where it is still empty.
- Each pick records its basis, its rank in its stratum and wave, its hash and
  the plan version. A stratum with sampling off takes its whole wave, with
  basis Full coverage.
- Top-ups are the items flagged with `topup_quarter = n`, with basis
  Risk-based top-up. A top-up that is already a pick, from an earlier quarter
  or from this draw, refuses the whole selection with a message that names the
  item and its pick.
- When the planned top-ups differ from the items flagged, the selection still
  runs and warns.
- If a wave holds fewer items than `q_i`, the whole wave is taken and the
  shortfall is reported. The selection does not borrow from another wave.
- The first selection moves an Approved plan to Active.

**Business days** are weekdays that are not in the plan's `non_working_dates`.

- "Next business day" means the first business day strictly after a date.
- "+k business days" counts k business days after the start date.
- Rolling a date (used by clone) keeps it if it is a business day; otherwise it
  becomes the next business day.
- Evidence due = requested + `evidence_sla_days` business days.

**Default quarterly cycle.** For a new plan it generates the quarter dates and
the per-quarter steps. Offsets are from the 1LoD deadline (week 0):

| Offset | Event |
|---|---|
| Week 0 | Wave deadline |
| Next business day | Sample selected, seed recorded |
| Requests + 10 business days | Evidence due |
| +6 weeks | Fieldwork complete |
| +7 weeks | QA four-eyes sign-off |
| +8 weeks | Findings agreed with owners |
| +9 weeks | Quarterly report |

The 2LoD data-quality review runs from about one week after the window opens
until about one week after the deadline.

### Test results and the deviation rule

**Tests.** A test is opened for each pick of a selected quarter ("Open N
test(s) for Qn" on the Tests tab, or `POST …/tests`), only while the plan is
Approved or Active. Tests follow the plan of record while a change request is
open; a closed plan's tests no longer change.

- Test references run per quarter: `T27-Q1-001`, `T27-Q1-002`, … (the period's
  two-digit year, the quarter, a three-digit number after the highest one
  already used).
- The tester defaults to the person opening the tests. Evidence is due
  `evidence_sla_days` (10) business days after it is requested, counting
  neither the request day nor the plan's non-working dates; 2 March 2027 gives
  16 March 2027. Evidence received after the due date, or not received by
  then, is flagged late.
- The QA sign-off is four-eyes. It needs the fieldwork completion date and a
  result for every attribute that applies. When more than one person can sign
  in (the same switch as the approval gate), the tester cannot sign off their
  own test (409). In single-user or local mode the sign-off goes through with a
  warning, stored in `qa_note`.
- Any change to a signed-off test withdraws the sign-off, and the answer says
  so (`qa_withdrawn`).
- A result that has a finding stays a Fail (409): the finding is closed in
  Audit Findings, not by editing the test.

**Test result.** Each test has one result per attribute that applies to its
stratum.

- `# Tested = Pass + Fail`. N/A does not count as tested.
- The overall result:

| Fails | Overall result |
|---|---|
| none tested | Not started |
| 0 | Effective |
| 1–2 | Partially effective |
| ≥ 3 | Ineffective |

The thresholds are the plan parameters `result_partial_min` and
`result_ineffective_min`.

**Deviation rule.** It is applied per attribute per stratum, cumulative for the
year:

| Deviations | Conclusion |
|---|---|
| none tested | Not tested |
| 0 | No deviations / effective |
| 1 | 1 deviation – extend sample. Suggests +50% of the next quarter's sample for that attribute. |
| ≥ 2 | Not effective – thematic finding |

The thresholds are `deviation_extend_at` and `deviation_thematic_at`. The
extension is `extend_pct` of the stratum's planned tests in the quarter after
the last one tested, rounded up: one Tier 1 deviation in Q1 suggests +7 (half
of Q2's 13), one Tier 2 deviation +4 (half of 7). A deviation in Q4 has no later
quarter that year and is carried into next year's sample. The extension is a
suggestion shown on the dashboard; the frozen selection is not changed.

A stratum without sampling tests every item due, so there is no sample to
extend. One deviation there reads "1 deviation – full coverage, raise as
findings" (`full_coverage` in the JSON), and the next step is to raise each
Fail as a finding. Two or more are still Not effective, for the risk register.

Every individual Fail is still an asset-level finding.

### Coverage RAG

Per stratum and quarter, the cumulative wave target (35 / 70 / 100 / 100% for
Tier 1) is compared with the share of the stratum's items covered on or before
the calendar quarter end. What covers an item is the plan's coverage basis: its
attestation date, or the QA sign-off date of its test. An item inside its
multi-year cycle at the quarter end counts as covered, and in a cycle stratum
the target for a quarter is the items in cycle then plus the items with a wave
up to that quarter.

| RAG | When |
|---|---|
| Green | On or above plan |
| Amber | Within `rag_amber_points` (10) points below plan |
| Red | More than that below plan |
| Not due | Before the quarter end, or the stratum has no items |

The word is always printed next to the colour.

### Dashboard

The Dashboard tab (and `GET …/dashboard`) shows:

- cards: tests planned, items selected, tests opened, signed off by QA,
  attribute fails, findings raised, open and overdue
- coverage against plan, "1LoD coverage" or "Test coverage" by the plan's
  basis: each stratum's four quarter cells with their RAG, and the year-end
  tracker (items attested, tested and signed off, in cycle, and data quality
  accepted, against every item of every stratum)
- 2LoD testing progress per quarter: planned, selected, opened, fieldwork done,
  QA signed off, evidence late, and the tests by overall result
- deviations by attribute and stratum, with the conclusion, the failing tests
  and the next step (the extension, "Push to risk register", or the risk
  already raised)
- findings raised from the plan by severity (raised, draft, open, overdue,
  closed or risk accepted), and the list of linked findings. These are read
  from Audit Findings, so their status, due date and overdue flag are that
  module's. A Draft is not counted as open. A linked finding that was deleted
  in Audit Findings is shown as Missing, and its Fail can be raised again.
- utilisation per quarter against the threshold

### Golden values (Appendix A, the reference plan)

| | Tier 1 (N=80, 95%/5%, floor 25%) | Tier 2 (N=240, 90%/10%, floor 5%) |
|---|---|---|
| n0 → FPC → floor → annual | 59 → 35 → 20 → **35** | 22 → 21 → 12 → **21** |
| Statistical per quarter | 13 / 12 / 10 / 0 | 4 / 6 / 6 / 5 |
| Tests per quarter (with top-ups) | 14 / 13 / 11 / 1 | 5 / 7 / 7 / 6 |
| Total tests (coverage) | 39 (49%) | 25 (10%) |
| 1LoD reviews due | 28 / 28 / 24 / 0 | 36 / 72 / 72 / 60 |
| Cumulative coverage target | 35 / 70 / 100 / 100% | 15 / 45 / 75 / 100% |

The two tiers together give 64 tests.

Capacity is 2.0 FTE × 50 days, or 100 person-days a quarter. Effort per quarter
is 74.4, 81.8, 76.8 and 47.0 person-days. The year's total is 280 of 400 (70%),
so no quarter is flagged at 85%.

## Integration (reuse, not duplication)

### Raise finding

"Raise finding" on a Fail result calls `auditfinding.Service.Create` with these
fields:

| Field | Value |
|---|---|
| `title` | "NFR-04 Access management & recertification not met on T1-021 Example Core banking ledger 02", unless one is given |
| `source` | "Second Line Review" |
| `engagement` | "AP-2027-001 2027 Security NFR Controls Testing Plan" |
| `origin_ref` | plan / test / attribute reference |
| `auditor` | the tester |
| `owner` | the one given, else the item's owner in the population (400 if neither) |
| `business_unit` | the plan's function |
| `linked_controls` | the attribute's 800-53 ids |
| `framework_refs` | the attribute's regulatory references and "Security NFR <key>" |
| `deficiency_type` | Operating Effectiveness |
| `condition` | the test's evidence summary and the result note |
| `criteria` | the attribute's requirement and pass criteria |
| `severity` | the tester's choice (required) |
| `identified_date` | the fieldwork completion date, else today |
| `status` | Open, or Draft |

`source` is a closed vocabulary in Audit Findings, so the plan reference goes in
`engagement` and `origin_ref` instead. With no due date given, the finding's
own SLA defaults apply (High: 90 days from the identified date).

The finding's id, reference and severity are stored on the result, and the plan
reference on the finding, so the link runs both ways. A Fail raises one
finding; a second raise answers 409 unless that finding was deleted. Raising
needs the plan to be Approved or Active, and records a `finding` event in the
plan's history.

### Thematic push to the risk register

A Not effective conclusion can be pushed to the risk register: "Push to risk
register" creates a `riskregister.Risk` through the register's own service. The
person pushing names the owner and rates likelihood and impact (1 to 5).

- Risk id: plan, attribute and stratum, `AP-2027-001-NFR-04-T1`.
- Title: "NFR-04 Access management & recertification not effective for Tier 1".
- Asset: the stratum. Business unit: the plan's function.
- Vulnerability: the deviation summary, naming the failing tests.
- Current controls: the attribute's 800-53 ids.
- Residual likelihood and impact start equal to the inherent ones. The plan
  does not judge what the controls are worth; the risk's owner does.
- Notes: the plan reference and the rule that concluded it.

Only a Not effective conclusion can be pushed (409 otherwise), and only once:
the risk's id and reference are stored in `assurance_plan_thematic` and shown on
the deviation's row. Crisis exercises only cite risks and do not create them,
so this is a new, small write path through the register's existing service.

### Test attributes from the catalogs

Attributes are seeded from chosen Security NFRs or from chosen 800-53 controls
(the "Copy from the catalogs" box on the Test script tab, or
`POST …/attributes/seed`). They are copied onto the plan, so a later catalog
edit does not change an approved plan.

- An NFR is found by its record key, its ID (`0.1`) or `NFR-` and its ID. The
  attribute's reference is `NFR-0.1` and it keeps the record key in `nfr_key`.
  The requirement is the NFR's summary (without its ID) and description, the
  pass criteria its implementation text, the 800-53 ids its NIST mapping.
- A control becomes an attribute with its id as the reference, its family as
  the domain, its name as the requirement and its requirement text as the pass
  criteria.
- A reference already in the test script is skipped, and one not in the catalog
  is reported as not found. The answer lists what was added, skipped and not
  found.

On every read, each attribute's control ids and NFR key are checked against
`nist_controls` and `security_nfrs`. Any that do not resolve are marked
Unresolved on the page and in the gate's advisory findings; they are never
presented as resolved.

The test script is part of the design: it is edited while the design is open
and locked after approval, like the steps and strata. Under a change request an
attribute that already has results cannot be removed, nor narrowed off a
stratum where it has results; editing its text keeps its results attached.

The shipped NFR catalog numbers its entries `0.1`, `1.1` and so on. Appendix A's
`NFR-01`..`NFR-16` do not exist there, so they are plan-local attributes,
carried by the worked example. A plan from the Controls testing type starts
with no attributes (the gate says so) and is seeded from the catalogs. A
plan-local attribute can be linked to a catalog NFR later by setting its NFR
key.

### Proposed populations

A plan with a population source can propose its population ("Propose from …"
on the Scope population tab, `POST …/population/propose?source=`). The Policy
review type proposes from the policy library, the Remediation roadmap type from
Audit Findings; any plan can name either source in its header.

The proposal is CSV text in the import's own format (`item_ref`, `name`,
`stratum`, `owner`, `business_line`, `wave`, `source_kind`, `source_ref`) with
notes. The page puts it in the paste box with merge on and runs the preview, so
it goes through the same validation and diff as a paste, and nothing is
written until it is applied. Merge, because an item the source no longer lists
(a finding closed since) stays in the plan.

Each item lands in the stratum that `takes` its document type or severity, and
in the wave of the quarter it is due. A value no stratum takes is left out and
counted in a note.

- **Policies**: the documents in force (approved, or approved and in review)
  of the plan's client profile, or without one when the plan has none. A
  document is due on its `next_review_date`, or on its effective date plus its
  review cadence when none is stored. Those due by the period end are
  proposed; a review already overdue goes into Q1. A document with neither date
  has no review clock: it is left out and counted. Letters have no review
  cycle and are never proposed. The reference is the document's, or
  `DOC-<id>`; the owner is its owner role.
- **Findings**: the open audit findings (not Draft, Closed or Risk
  accepted). An overdue finding, or one with no due date, goes into Q1; one
  due after the period is left for next year's roadmap and counted. The owner
  and business unit come from the finding.

An item already in the population keeps its stratum and wave. When the source
now places it elsewhere, a note says so: a move stays a deliberate edit, by
import or, once frozen, by change request. A second proposal therefore adds
only what is new, and its preview of the unchanged items is empty.

The proposal refuses (400) a plan with no source, no period dates, or no
stratum that takes anything, and a closed plan (409). Without the policy
library or Audit Findings wired in, it answers 409. No model is involved.

### Population import

There is no upload. The population is structured data, pasted as CSV text into
the textarea on the Scope population tab and posted as JSON
(`{"csv": …, "merge": …, "base": …}`). No file is accepted and nothing is
extracted from a document, so this is not document ingestion.

**Columns** (in the order the page writes them): `item_ref`, `name`, `stratum`,
`cif`, `business_line`, `hosting`, `internet_facing`, `owner`, `wave`,
`attested_on`, `dq_status`, `dq_reviewed_on`, `topup_quarter`,
`topup_rationale`, `last_tested_on`, `source_kind`, `source_ref`.

- Headers are matched without regard to case, spaces or punctuation.
- Common names are accepted: `Asset ID`, `Tier`, `Critical or important
  function`, `1LoD wave`, `Attestation date`, `Top-up quarter`, `Last test
  date` and others.
- An unknown column is listed as ignored.
- Only `item_ref` is required. A column left out of the paste keeps each
  existing item's value, so a paste of `item_ref,attested_on` records
  attestations and changes nothing else.

**Delimiter and values:**

- The delimiter is a comma, a semicolon or a tab, whichever the header uses.
- A leading byte-order mark is dropped.
- Quarters are written `Q1`..`Q4` or `1`..`4`. Blank, `0`, `-` or `none` means
  no quarter.
- Yes/no takes `yes`, `y`, `true`, `1` or `x`, against `no`, `n`, `false`,
  `0`, `-` or blank.
- Dates are `YYYY-MM-DD`.
- DQ status is one of Not reviewed, In review, Accepted or Returned to 1LoD.

**Merge or replace.** Replace (the default) removes the items the paste leaves
out. Merge keeps them.

**Preview** returns:

- the row count and the columns read and ignored
- each problem, with its line and reference: an unknown stratum, a duplicate
  reference, a bad wave, date or yes/no, a top-up with no rationale, or an
  edit the population rules refuse
- the added, changed (with the fields) and removed references
- the unchanged count and the count per stratum
- `base`, the current population's digest

**Apply** writes nothing while any problem remains. It refuses with 409 when
`base` no longer matches, because someone changed the population after the
preview. One history event records the mode and the counts.

**Limits.** A paste is at most 50,000 rows and 16 MiB. The preview lists the
first 200 problems.

The tab can also download the population as CSV (`population.csv`). It can fill
the box with the current population, or with an example population built from
the strata's planned sizes and waves (`population/example.csv`). One item can be
edited on its own (`PATCH …/population/:itemID`): its description, attestation,
DQ review and top-up. Stratum, wave and membership change only through an
import, where the whole diff is visible.

## API

Reads need access to `/assurance-plans`. Writes, the selection action and AI
spend need admin, the same split as the risk register and Audit Findings.

**Read**

- `GET /assurance-plans`: the list page. `GET /assurance-plans/:id`: the plan
  page, with tabs.
- `GET /assurance-plans/data?type=&status=&year=&client=` and
  `GET /assurance-plans/data/:id`. The second returns the plan with steps,
  quarters, strata, attributes, governance and its counts.
- `GET /assurance-plans/vocabulary` and `GET /assurance-plans/types`
- `GET /assurance-plans/:id/population` (JSON, each item with its selection),
  `GET /assurance-plans/:id/population.csv`,
  `GET /assurance-plans/:id/population/example.csv`
- `GET /assurance-plans/:id/sampling`: sizes, allocation, reviews due,
  capacity, each with its inputs and where N came from; the freeze, the seed
  commitment and each quarter's selection
- `GET /assurance-plans/:id/selections`: every frozen pick
- `GET /assurance-plans/:id/tests?quarter=`: the tests with their item,
  results, counts, overall result and evidence lateness
- `GET /assurance-plans/:id/deviations`: the deviation rule per attribute and
  stratum, with the extension or the risk pushed
- `GET /assurance-plans/:id/lint`: the approval gate's findings, without
  approving
- `GET /assurance-plans/:id/dashboard`: coverage RAG, testing progress,
  deviations, linked findings and utilisation
- `GET /assurance-plans/:id/history`, `GET /assurance-plans/:id/versions`,
  `GET /assurance-plans/:id/versions/:n`. A version is served as the bytes
  stored at approval, with their SHA-256 in `X-Snapshot-SHA256`, so a reader
  can check the copy against the hash.
- `GET /assurance-plans/:id/ai/proposals`: `{"configured", "model",
  "proposals"}`, newest first; `GET /assurance-plans/:id/ai/proposals/:pid`:
  one proposal (see [AI assist](#ai-assist-optional))
- `GET /assurance-plans/:id/export.json`: the whole plan (see below)
- `GET /assurance-plans/:id/export/:file`: one table as CSV. `:file` is
  `steps.csv`, `quarters.csv`, `population.csv`, `tests.csv` or
  `deviations.csv`; any other name ending in `.csv` answers 400, and one that
  does not end in `.csv` answers 404.
- `GET /assurance-plans/:id/pack.pdf`, `GET /assurance-plans/:id/report/quarter/:q`
  and `GET /assurance-plans/:id/report/annual.pdf`. `:q` is `1`..`4` or
  `Q1`..`Q4`, with or without `.pdf`, so `/report/quarter/Q2.pdf` works. Gin
  matches a parameter to a whole path segment, so a route cannot be
  `:table.csv` or `:q.pdf`; the handler strips the suffix instead.

  These render through `internal/doctemplate` (see [Documents](#documents)).
  With `?bundle=1`, or when no Typst engine is found here or on the render
  host, they return the sources zip instead. `?inline=1` asks the browser to
  show the PDF. With `?format=json` they return the payload the template would
  receive, without rendering. A compile failure answers 422 with
  `{"error", "log"}`, like the redline. With no renderer wired (a test
  harness), the PDF routes answer 503 and `?format=json` still works.

**Admin**

- `POST /assurance-plans` (`{"type": …}` or `{"example": "dora-nfr-2027"}`),
  `PUT /assurance-plans/:id` (the header), `DELETE /assurance-plans/:id` (a
  Draft with no versions only)
- `PUT /assurance-plans/:id/steps | quarters | governance | strata | attributes`
  replaces the whole list. These answer 409 while the
  design is locked. A governance body without `regulatory_basis` keeps the
  plan's rows; an empty list clears them. A quarter keeps its selection stamp across an edit. A
  stratum that still holds items cannot be removed or renamed.
- `POST /assurance-plans/:id/quarters/generate` (`{"dates": true}`): resets
  the quarters to the default cycle, keeping each quarter's retest and overhead
  days, and with `dates` regenerates the template's quarterly steps from them,
  keeping each step's status by reference
- `PATCH /assurance-plans/:id/steps/:stepID` (`{"status": …}`): a step's status,
  which can move on a locked design; refused only once the plan is closed
- `POST /assurance-plans/:id/population/preview` and
  `POST /assurance-plans/:id/population`: the CSV text as JSON, then apply (see
  [Population import](#population-import)).
- `POST /assurance-plans/:id/population/propose?source=policies|findings`: the
  proposed population as CSV with its counts and notes
  (`{"source", "csv", "items", "new", "skipped", "notes", "merge"}`); writes
  nothing. Without `source`, the plan's own. See
  [Proposed populations](#proposed-populations).
- `PATCH /assurance-plans/:id/population/:itemID`: one item's description,
  attestation, DQ review and top-up
- `POST /assurance-plans/:id/population/freeze`: freeze and draw the seed
- `POST /assurance-plans/:id/quarters/:q/select` (`:q` is `1`..`4` or
  `Q1`..`Q4`): the quarter's picks, shortfalls and warnings
- `GET /assurance-plans/:id/seed`: the seed, for re-performance
- `POST /assurance-plans/:id/attributes/seed`
  (`{"nfr_keys": […], "control_ids": […]}`): 409 while the design is locked
- `POST /assurance-plans/:id/tests`
  (`{"quarter", "selection_ids", "tester", "evidence_requested_on"}`; 201):
  opens a test for each pick of the quarter that has none, or for the listed
  picks. 409 when every pick already has a test or the quarter is not selected.
- `PUT /assurance-plans/:id/tests/:testID`: the test's type, dates, tester,
  evidence summary and notes, and `results` (`[{"attribute_id", "result", "note"}]`).
  Results not listed keep their value. The answer carries the test, the changes
  and whether the QA sign-off was withdrawn.
- `POST /assurance-plans/:id/tests/:testID/qa`: the signed-in user signs off
- `POST /assurance-plans/:id/results/:resultID/finding`
  (`{"severity", "owner", "title", "due_date", "status"}`; 201): raise the Fail
  as a finding in Audit Findings
- `POST /assurance-plans/:id/thematic`
  (`{"attribute_id", "stratum", "owner", "likelihood", "impact"}`; 201): push a
  Not effective conclusion to the risk register
- `POST /assurance-plans/:id/submit | return | approve | activate | close | change-request | clone`.
  `approve` records the signed-in user as approver; a name in the body counts
  only when there is no session (local mode). A blocked approval answers 422
  with `{"error", "findings"}`; a move the lifecycle does not allow answers
  409.
- `POST /assurance-plans/:id/ai/draft`
  (`{"objective", "regulation", "population_size", "constraints"}`),
  `POST /assurance-plans/:id/ai/attributes` (`{"focus", "count"}`) and
  `POST /assurance-plans/:id/ai/narrative` (`{"document": "pack" | "quarter" |
  "annual", "quarter"}`): ask the model, and answer 201 with the stored
  proposal. With no provider configured, or while another request for the
  plan is running, they answer 409; a draft or attributes on a locked design
  answer 409 too. A refusal or an answer cut off answers 422; a provider
  failure, or an answer still malformed after one repair turn, answers 502.
  Nothing is stored in either case.
- `POST /assurance-plans/:id/ai/proposals/:pid/decide`
  (`{"decision": "accept" | "reject", "note", "parts", "attributes",
  "payload"}`): `parts` chooses what of a draft to apply, `attributes` which
  proposed attributes to add, and `payload` is the proposal as edited. Answers
  `{"proposal", "plan", "notes"}`; 409 once decided.

The JSON export is a dedicated type in the `TemplateExport` style
(`PlanExport` in `export.go`), so renaming an internal JSON tag cannot silently
blank a field in the pack. The export:

- carries `format_version` (1), which is bumped when a field is renamed or
  removed
- carries no internal ids: rows refer to each other by `step_ref`, `item_ref`,
  `attr_ref` and `test_ref`
- has collections that are never null
- has, besides the design, what is computed from it:
  - each stratum's sizes (n0, FPC, floor, annual sample, tests per quarter,
    cumulative targets)
  - the capacity per quarter
  - each population item's selection: quarter, basis, rank, test and result
  - the tests with their results
  - the deviation conclusions, coverage, testing progress and findings
  - the approved versions with their SHA-256
  - the gate's findings
  - totals
- carries the plan type settings (`test_types`, `coverage_basis`,
  `population_source`), each stratum's `cycle_years`, `takes` and `in_cycle`,
  each item's `last_tested_on`, `cycle_due` and source, and each test's
  `test_type`. The population CSV adds `last_tested_on`, `cycle_due`,
  `source_kind` and `source_ref`; the tests CSV adds `test_type`.
- has `seed` only once every quarter is drawn or the plan is closed, as on the
  page; `seed_commitment` is always there.

Every CSV data cell that starts with `=`, `+`, `-`, `@`, a tab or a carriage
return is prefixed with `'`. A spreadsheet then reads the cell as text, not as
a formula. Names and notes come from pasted CSV and from testers, so a cell
like `=HYPERLINK(…)` is not trusted. The header row is fixed and is not
prefixed.

## Documents

Three documents render through `internal/doctemplate`, with its engine
discovery, the render host and the sources-zip fallback:

- the approval pack
- the quarterly progress report
- the annual conclusion

The module does not import `doctemplate`. `internal/app` passes it a
`Renderer` (`Handler.WithRenderer`) that answers like the redline does. The
**Documents and exports** table on the Overview tab links each document with
its sources and its data, and links the JSON and CSV exports.

**Committee approval pack** (`pack.pdf`)

- Template `assurance-pack-typst`, kind `assurance_pack`, file
  `templates/typst/assurance-plan-pack.typ`. It is built from `lib.typ` and
  `brand.typ` on the `presentation-16-9` paper size.
- Its payload is the export without the population and the test log, plus a
  `pack` object:
  - `as_of`
  - `roadmap`: a Mobilise card, then one card per quarter with the wave, the
    tests, the milestones and the report date
  - `next_steps`: up to six steps not complete and not past their end date, in
    date order
  - `delivery_risks`: the gate's blocking findings and the advisories that
    bear on delivery (empty strata, wave counts, items without a wave, no
    attributes, quarter dates out of order), and any quarter over the
    utilisation threshold
  - `peak_quarter`
  - `narrative`, only once a person has accepted a pack narrative (see
    [AI assist](#ai-assist-optional)): `executive_summary`, `speaker_notes`
    (`[{slide, title, notes}]`, `slide` counting the cover as 1), `model`,
    `accepted_by` and `accepted_at`
- The slides, in order:
  1. cover
  2. decisions requested
  3. the regulatory basis: what the regulation asks and how the plan meets it.
     A plan with no basis rows lists its regulatory references instead.
  4. objective
  5. scope: strata and their counts, the attribute list
  6. approach: the 100% coverage track and the sample-testing track
  7. sampling table and deviation rule
  8. roadmap
  9. quarterly targets
  10. the operating cycle
  11. governance: cadence and escalation
  12. resourcing and delivery risks. With none, the slide says "No delivery
      risk flagged by the plan's checks."
  13. next steps
- A long list is cut on its slide and says how many more are in the export.
- With a `narrative`, a "Presenter's notes" page follows the slides: the
  executive summary and the speaker notes, under a line that says they were
  drafted with AI assistance, by which model, and who accepted them when. The
  slides themselves do not change.

**Quarterly progress report** (`report/quarter/:q`) and **annual conclusion**
(`report/annual.pdf`)

Both are `business-report.typ` (template `report-typst`). They fill its
contract, including the limitations and basis-of-reporting section. Phase 4
added optional `tables` (`[{caption, columns, rows}]`) to a section and an
appendix; see DOCUMENT_TEMPLATES.md.

Findings are read back from Audit Findings, so the report states what that
module holds:

| Report field | Audit Findings field |
|---|---|
| observation | Condition |
| risk | Effect |
| recommendation | Recommendation |
| management response | the response, then its text |
| owner | Owner |
| target date | the due date |
| control | the linked controls |

A finding deleted there is left out, and the report says how many were.

The quarterly report is as at its quarter:

- Coverage and DQ accepted count only items dated by the quarter end.
- The test table holds that quarter's tests.
- The deviation rule is applied to the tests of quarters 1 to q, with the
  thematic risks pushed by that quarter's report date.

So the Q1 report downloaded in October says what it said in April. The
exception is the findings' status, which is Audit Findings' current status. Its
sections:

1. Coverage against plan, with RAG
2. Testing in the quarter
3. Deviations so far this year
4. Resourcing
5. Next quarter

The appendix is the test attributes.

Once a person has accepted a narrative for a report (`q1`..`q4` or `annual`),
its executive summary replaces the generated one, and the limitations add that
it was drafted with AI assistance from the plan's data, the model, and who
accepted it when. The newest accepted narrative for that report wins; the
sections, tables and figures are generated as before.

The **annual conclusion**:

- is subtitled "Input to the ICT risk management framework review report
  (RTS (EU) 2024/1774 Art. 27)"
- states:
  - the population and the coverage achieved
  - the tests performed, by quarter and by stratum
  - the results per attribute and stratum, with the risk raised
  - the findings by severity and in full
  - the conclusion
- has the test attributes and the regulatory basis as appendices

The conclusion's wording is generated per stratum from the deviation rule:

- effective, with no deviations
- effective, with the attributes whose sample was extended
- effective except for the attributes concluded not effective

It also says plainly when a stratum had no tests, or when testing is
incomplete. The wording is what the data supports. The control function signs
it off and may reword it in the sources (`?bundle=1`).

The limitations state:

- the records' date
- that sampling gives reasonable, not absolute, assurance
- that results not signed off by QA are provisional

The annual's limitations add that the conclusion does not replace the work of
the other assurance plans, such as resilience testing, or of internal audit. A
plan without sampling says that every item due is tested rather than sampled,
and the source of coverage follows the plan's basis.

On the tests basis the annual's coverage table reads "Tested and signed off",
"In cycle" and "Covered" in place of the attestation columns. A plan with two
or more test types adds a "Tests by type" table: each type's tests opened,
signed off by QA, and with a fail.

**Samples**

`templates/samples/assurance-plan-sample.json`,
`assurance-quarterly-sample.json` and `assurance-annual-sample.json` are the
Appendix A plan run through a sample year by `documents_test.go`:

- every quarter drawn and tested
- the waves attested, one item in nine left out
- an NFR-04 fail in Tier 1 in Q1, raised as a finding, and another in Q3,
  pushed as a thematic risk
- an NFR-11 fail in Tier 2 that extends the sample
- the last two Q4 tests still in fieldwork

Each sample's shape is checked on every `go test`. To write them afresh, run
`GRC_UPDATE_SAMPLES=1 go test ./internal/assuranceplan -run 'Payload'`. The
values (finding references carry the current year) vary from run to run; the
shape does not.

`templates/build.sh pack | quarterly | annual` renders them (they are also part
of `all`). The reference renders are in `templates/out/`.
`internal/doctemplate` compiles all three with Typst when it is installed:
from the samples, and from the same payloads with every string replaced by
Typst code, which must print rather than run.

## Knowledge API

There are two read-only kinds in `internal/knowledge`, both in
`/api/knowledge/export`:

- `assurance_plan`: one record per plan, ref `AP-2027-001`.
- `assurance_plan_test`: one per test, ref `AP-2027-001/T27-Q1-001`.

The plan record's body opens with **On plan**, **Behind plan: …** or **Not
started**, then gives:

- the plan's stage (mobilisation, Q1 to Q4 of the period, after the period
  ends)
- each quarter's dates, marking the current quarter
- coverage against plan per stratum and quarter, with RAG, on the plan's
  coverage basis (attested, or tested and signed off, and in cycle)
- testing per quarter
- the steps past their end date, and the steps under way or starting within
  30 days
- the deviations with their conclusion and risk
- the findings with status and due date

"Behind" means at least one of these:

- a due coverage cell is Amber or Red
- a quarter's fieldwork has closed with tests not opened or not signed off
- a step is past its end date
- a finding is overdue

The record's fields carry the same figures for counting: `current_quarter`,
`behind_plan`, `coverage_<stratum>_pct` (the share covered on the plan's
basis), `tests_planned`, `tests_opened`,
`tests_signed_off`, `attribute_fails`, `not_effective`, `findings_open` and
`findings_overdue`. The plan kind has a compact index.

A test record has:

- the item, its stratum, CIF and business line
- the quarter, the tester, the evidence dates and the QA sign-off
- each attribute's result, with its note and finding reference

Its fields are `stratum`, `item_ref`, `overall`, `fails`, `failed_attributes`
and `findings`, and its related refs are the failed attributes and the
findings. So "which Tier 1 assets failed NFR-04?" is a search, or a filter on
those fields. The test kind is searched; it has no index.

Coverage and the deviation rule are computed, not stored, so the module
supplies the records (`Service.KnowledgePlans` and `KnowledgeTests`). The
knowledge service takes them through `WithAssurancePlans`, and does not read
the tables with its own SQL. Both kinds are read fresh, like exercises, and not
from the 45-second cache: they change while the year runs.

## AI assist (optional)

Three assists, on the plan's **AI assist** tab. None is required: every
feature of the module works with no model configured, and the tab then says so
and disables its buttons.

- **Draft plan from a brief.** The brief is an objective, a regulation, a
  population size and constraints. The answer proposes the plan's own steps
  (not the quarterly steps generated from the quarter dates), quarter dates
  and a RACI, and may propose an objective.
- **Propose test attributes** for a regulation or a domain. The server
  retrieves a shortlist lexically through the knowledge service (up to 12
  NFRs, 12 controls and 8 Regulation Coverage clauses) and puts it in the
  prompt. The model judges the shortlist; it does not browse. With nothing on
  the shortlist the request answers 400 and the model is not asked.
- **Draft committee narrative**: an executive summary and, for the pack,
  speaker notes per slide, from the plan's data. That data is the pack payload
  or the report payload, as JSON, without the appendices. It works on a plan
  in any status.

**The model.** Every call goes through `aiprovider.Router`, the same router
the Policy Studio and Regulation Coverage use. It answers with the Wintermute
model chosen in Settings, and logs each call to `ai_usage_log`. Only one
request per plan runs at a time, so a double click does not pay twice.

**The standing rules** are the system prompt of every assist, so no brief can
talk the model out of them:

1. Say when you do not know: what could not be determined goes in `unknowns`.
2. Never invent a control identifier, an NFR key, a regulatory article, a
   threshold or a deadline. Cite only references from the lists shown. A
   figure, date or percentage must come from the data given or the brief.
3. Keep what the regulation requires separate from good practice: an item is
   `regulation` only when a cited regulation reference requires it, otherwise
   `good_practice`.
4. Dates for the plan's own schedule are proposals, never regulatory
   deadlines.
5. Answer with one JSON object in the contract given.

**The answer.** It is parsed strictly, with unknown fields refused. A malformed
answer gets one repair turn that quotes the parse error; a second malformed
answer is a 502. A refusal or an answer cut off at the token limit is never
parsed: it answers 422. Nothing is stored for a failure.

**The proposal.** A usable answer is checked and stored in
`assurance_plan_ai_proposals` with:

- the model that answered
- the prompt hash: SHA-256 of the system prompt and the prompt, so the exact
  question can be shown to be the one asked
- the timestamp and who asked
- the brief, and the plan's design revision

The plan is not changed. The proposal lists the server's checks ("Checks"), the
model's unknowns, and for a narrative its flags.

**References.** Every reference a draft step or an attribute cites is
resolved, in this order:

1. the shortlist the model was shown
2. the catalogs, through the knowledge service. A hit that was not on the
   shortlist is kept, and noted.
3. the plan's own references: its regulatory references and the references of
   its regulatory basis rows

A reference that resolves nowhere is listed as **unresolved**, shown struck
through, and never stored on the plan. An item marked `regulation` that cites
nothing that resolves (for an attribute, no regulation clause and no plan
reference) is turned to `good_practice`, with a note.

**The checks**, besides references, leave out what cannot be applied and note
what a person should look at:

- draft steps: left out without a title or valid dates, when they end before
  they start, repeat a reference or take the reference of a generated step;
  noted when they run outside the plan's span, name a role that is not a RACI
  role, or have no responsible role. An unknown step type becomes an activity.
- draft quarters: all seven dates, in order, inside the plan's span; otherwise
  the quarter keeps its current dates, with a note
- draft RACI: a cell with an unknown role, or a letter other than R, A, C, I or
  A/R, is left out; an activity with other than one accountable role is noted
- attributes: left out without a requirement. A reference already used
  becomes `<ref>-2`, a method that is not one of the four becomes Inspection,
  and an applies-to code that is not a stratum of the plan is dropped, each
  with a note. The first NFR cited becomes the attribute's NFR, the controls
  its control ids, and the clauses and plan references its regulatory
  references.
- narrative: speaker notes only for the pack's 13 slides, one per slide. Flags
  for each figure in the text that is not in the plan's data (as given, rounded,
  or as a percentage of a share) and each article that is not among the plan's
  references or data. A flag is a warning to the reader, not a refusal.

**The decision** (admin; `.../ai/proposals/:pid/decide`). A proposal is decided
once.

- **Reject** stores the decision and an optional note. The plan does not
  change.
- **Accept** applies the proposal as the person edited it. An edited payload is
  checked again against the references stored with the proposal, never against
  references taken from the edit, so an edit cannot slip in a reference the
  model was not shown or that does not resolve.
  - A draft applies the parts chosen, through the same validation as the
    editors: the objective, the steps (replacing the plan's own steps; the
    generated quarterly steps stay, with their statuses), the quarters' dates
    (the generated steps are regenerated from them) and the RACI. It needs an
    editable design. The plan is marked AI-generated, with the model and the
    prompt hash.
  - Attributes add the ones chosen to the test script, each with `ai_generated`,
    the model and the prompt hash. The test script shows them with an **AI**
    marker. It needs an editable design.
  - A narrative takes the text as edited on the tab. It is used in its
    document, as [Documents](#documents) describes.
- `applied_json` keeps what was accepted, apart from `payload_json`, what the
  model proposed. Both decisions, and the proposal itself, are in the plan's
  history.

## Backup and sync

- Every `assurance_plan*` table is in the JSON snapshot, in parent-first order,
  with `idKeyed` and `strat: skipInSync`. `SnapshotVersion` went from 15 to 16
  with the module. Phase 2's strata, population and selections joined the same
  version, because no build with phase 1 alone was released. A v15-or-older
  backup restores with the module empty. A phase that adds tables after a
  release bumps the version again.
- The seed is in the snapshot, because a restored plan must still re-perform
  its draws. A snapshot is an administrator's export.
- Phase 3's attributes, tests, results and thematic pushes joined version 16
  too, for the same reason. Tests and results are `skipInSync` like the rest.
- `audit_findings.origin_ref` joins the snapshot's column list for that table.
  An older backup restores with it blank.
- Phase 4's `assurance_plan_basis` joins version 16 too, `idKeyed` and
  `skipInSync`, for the same reason: no build with the module has been released.
- Phase 5's columns join the snapshot's column lists at version 16 too. A
  snapshot without them restores with each column's default: attestation
  basis, no test types, no source, no cycle.
- Phase 6's `assurance_plan_ai_proposals` joins version 16 too, `idKeyed` and
  `skipInSync`, for the same reason. A restored proposal keeps its model,
  prompt hash and decision.
- The tables are excluded from `-sync-to` / `-sync-from` for the same reason as
  crisis exercises and Audit Findings: each plan is a tree of rows joined by
  autoincrement ids. To move plans between deployments, use the snapshot
  export/import.

## Sources

- IIA, Global Internal Audit Standards (2024): Standards 9.4 (Internal Audit Plan), 9.5, 11.3, 13.x (engagement planning), 14.3 (evaluation of findings), 14.6 (engagement documentation), 15.2 (confirming implementation)
- AICPA, Audit Guide: *Audit Sampling* (attribute sampling, sample-size tables for tests of controls)
- ISACA, IS Audit and Assurance Guideline 2208 *Sampling*; ISACA IT Assurance Framework (ITAF)
- Regulation (EU) 2022/2554 (DORA): Art. 5 (governance and organisation; 5(2)(b) policies), Art. 6 (ICT risk management framework; 6(4) control function independence, 6(5) annual review, 6(7) follow-up of audit findings), Art. 8 (identification; 8(1) inventory and classification, 8(2)–(3) risk assessment, 8(4) CIF mapping), Art. 9(4)(a) (information security policy), Art. 11(6) (testing of business continuity and response and recovery plans), Art. 24 (general requirements for testing), Art. 25 (testing of ICT tools and systems), Art. 26 (advanced testing based on TLPT)
- ISO/IEC 27001:2022 Annex A 5.1 (policies for information security: reviewed at planned intervals)
- Commission Delegated Regulation (EU) 2024/1774 (RTS on ICT risk management framework): Arts. 2–27, in particular Art. 2(2)(c), Arts. 4–5 (ICT asset management) and Art. 27 (report on the ICT risk management framework review)
- NIST SP 800-53A Rev. 5, *Assessing Security and Privacy Controls in Information Systems and Organizations*: assessment methods (examine, interview, test) and objects
- NIST SP 800-53 Rev. 5: control identifiers referenced by test attributes
