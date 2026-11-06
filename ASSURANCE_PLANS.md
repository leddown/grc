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
Controls testing and Blank types with the worked 2027 example. Strata, the
population and sampling (phase 2), test attributes, tests and findings
(phase 3), exports and documents (phase 4), the other plan types (phase 5) and
AI assist (phase 6) are described below as designed, and marked with their
phase where the code does not have them yet.

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
  its own sampling parameters, waves and top-ups.
- **Population**: the items in scope (assets, business units, policies,
  systems, findings), each in one stratum and one 1LoD wave.
- **Test attributes**: the test script. They are stored on the plan, so a later
  catalog edit does not change an approved plan.
- **Selections**: the items picked for 2LoD testing in a quarter, frozen when
  picked.
- **Tests**: one per selection, with one result per applicable attribute.
- **Governance**: a RACI matrix, a reporting cadence and escalation triggers.
- **History** and **versions**: an append-only event log, and an immutable JSON
  snapshot at each approval.

## Data model

All tables are `assurance_plan*`. Child rows join on `plan_id` (autoincrement),
which is why the module is excluded from `-sync-to` / `-sync-from` (see
[Backup and sync](#backup-and-sync)).

| Table | Holds | Key columns |
|---|---|---|
| `assurance_plans` | The plan header and its parameters | `reference` (unique), `title`, `plan_type`, `function_name`, `client_profile_id` (0 = none), `period_start`, `period_end`, `mobilise_start`, `closing_date`, `objective`, `decisions_requested`, `owner_role`, `accountable_role`, `approval_forum`, `regulatory_refs`, `status`, `revision`, `revision_status`, `revision_author`, `change_reason`, `author`, `approved_by`, `approved_at`, `activated_at`, `closed_at`, `selection_seed`, `non_working_dates`; the parameters listed below; `ai_generated`, `model`, `prompt_hash`; `created_*`, `updated_*` |
| `assurance_plan_steps` | Plan steps | `plan_id`, `ordinal`, `step_ref`, `phase`, `title`, `step_type` (activity / milestone / ongoing), `responsible_role`, `accountable_role`, `start_date`, `end_date`, `deliverable`, `refs`, `status`, `quarter` (0 = not tied to a quarter), `generated` |
| `assurance_plan_quarters` | The operating cycle | `plan_id`, `quarter` (1–4), `lod1_window_start`, `lod1_deadline`, `selection_date`, `fieldwork_start`, `fieldwork_end`, `findings_agreed_by`, `report_date`, `committee_label`, `retest_days`, `overhead_days`, `selected_at`, `selected_by` |
| `assurance_plan_strata` (phase 2) | Strata and their sampling parameters | `plan_id`, `ordinal`, `code`, `name`, `definition`, `sampling_enabled`, `confidence`, `tolerable_rate`, `floor_pct`, `effort_per_test`, `effort_per_review`, `wave_q1`..`wave_q4`, `topup_q1`..`topup_q4` |
| `assurance_plan_population` (phase 2) | The scope population | `plan_id`, `item_ref` (unique per plan), `name`, `stratum_code`, `cif`, `business_line`, `hosting`, `internet_facing`, `owner`, `wave_quarter`, `attested_on`, `dq_status`, `dq_reviewed_on`, `topup_quarter`, `topup_rationale`, `source_kind`, `source_ref` (for populations proposed from policies or findings) |
| `assurance_plan_attributes` (phase 3) | The test script | `plan_id`, `ordinal`, `attr_ref`, `domain`, `requirement`, `procedure`, `method`, `evidence_requested`, `pass_criteria`, `applies_to` (stratum codes), `regulatory_refs`, `control_ids`, `nfr_key` (empty = plan-local), `unresolved_refs`, AI provenance |
| `assurance_plan_selections` (phase 2) | Frozen picks | `plan_id`, `quarter`, `population_id`, `item_ref`, `stratum_code`, `basis` (Statistical / Risk-based top-up / Full coverage), `rank_hash`, `seed`, `selected_at`, `selected_by`; unique (`plan_id`, `population_id`) |
| `assurance_plan_tests` (phase 3) | One test per selection | `plan_id`, `selection_id` (unique), `test_ref`, `test_type`, `tester`, `evidence_requested_on`, `evidence_due_on`, `evidence_received_on`, `fieldwork_completed_on`, `evidence_summary`, `qa_reviewer`, `qa_date`, `notes` |
| `assurance_plan_results` (phase 3) | One result per attribute per test | `test_id`, `attribute_id` (unique together), `result` (Pass / Fail / N/A / empty), `note`, `finding_id`, `finding_ref`, `severity` |
| `assurance_plan_thematic` (phase 3) | Not-effective conclusions pushed to the risk register | `plan_id`, `attribute_id`, `stratum_code`, `risk_id`, `pushed_at`, `pushed_by` |
| `assurance_plan_raci` | RACI rows | `plan_id`, `ordinal`, `activity`, `cells` (JSON object, role → R, A, C, I or A/R). The plan stores the role columns in `raci_roles`. |
| `assurance_plan_cadence` | Reporting cadence | `plan_id`, `ordinal`, `forum`, `frequency`, `content`, `owner` |
| `assurance_plan_escalations` | Escalation triggers | `plan_id`, `ordinal`, `trigger_text`, `escalate_to`, `within_text` |
| `assurance_plan_history` | Every design, status, selection and result change | `plan_id`, `at`, `actor`, `event`, `subject`, `from_value`, `to_value`, `note` |
| `assurance_plan_versions` | Approval snapshots, immutable | `plan_id`, `number`, `label`, `approved_by`, `approved_at`, `change_reason`, `snapshot_json`, `snapshot_sha256` |
| `assurance_plan_ai_proposals` | AI proposals and their decisions (phase 6) | `plan_id`, `kind`, `model`, `prompt_hash`, `payload_json`, `status` (proposed / accepted / rejected), `created_*`, `decided_by`, `decided_at` |

Phase 3 adds one column to an existing table: `audit_findings.origin_ref` (for
example `AP-2027-001 / T27-Q1-004 / NFR-04`). It is the plan side of the
two-way link. Existing databases get it through `ensureColumn` and
`ADD COLUMN IF NOT EXISTS`.

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
| Approved → Active | Admin action. Execution can start on an Approved plan, and the first selection activates it (phase 2). |
| Active → Closed | Admin action, refused while a revision is open. After closing, nothing on the plan changes. |

Only a Draft that was never approved can be deleted; an approved plan is
evidence and is closed instead.

- Create ignores any status the client sends, and there is no edge from Draft
  to Approved. Both follow `policydocs`.
- `author` is taken from the session on create and never from the request
  body. Neither are status, revision, approval fields or the seed on a header
  edit: they are carried from the stored row.

**Design lock.** Once the plan is Approved, its design is locked: steps (other
than their status), quarters, strata, parameters, attributes, the population's
design fields (stratum, wave, top-up, identity) and governance. Execution data
stays editable:

- step status
- attestation dates and DQ status
- selections
- tests and results
- QA sign-off
- finding links

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
- a sampled stratum's waves (phase 2) do not sum to 100%, or its confidence is outside
  (0,1), its tolerable rate is outside (0,1), or its floor is outside [0,1]
- a step with no responsible role, with no dates (a milestone needs one
  date), or ending before it starts
- a quarter missing its 1LoD deadline or its report date

**Advisory findings (shown, not blocking):**

- a step outside the plan's span (mobilisation start to closing date)
- a quarter whose dates are out of order
- a RACI activity with no Accountable role, or with more than one
- utilisation above the threshold in any quarter (phase 2)
- attribute references that do not resolve against the control or NFR
  catalogs (phase 3)
- strata with zero population (phase 2)
- a population whose wave counts differ from the plan's wave percentages
  (phase 2)
- planned top-ups with no items flagged for them (phase 2)

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
- keeps the population's design fields (phase 2)

It drops:

- step status (every step starts Not started)
- attestation dates and DQ status
- selections, tests, results, finding links and thematic pushes
- history and versions, and the AI provenance

The clone starts in Draft with a new reference (in the new year), a new
author and no seed: a new seed is drawn when its population is frozen.

## Sampling method

The functions are in `sampling.go`. They are pure, so they can be tested
against the textbook tables.

**Statistical size** (attribute sampling, zero expected deviations):

    n0 = ceil( ln(1 − confidence) / ln(1 − tolerable_rate) )

95%/5% → 59, 95%/10% → 29, 90%/5% → 45, 90%/10% → 22.

**Finite-population correction:**

    n = ceil( n0 / (1 + (n0 − 1) / N) ),   n = 0 when N = 0

**Floor and annual sample:**

    floor  = ceil(floor_pct × N)
    annual = min(N, max(n, floor))
    tests  = annual + Σ top-ups

When sampling is off for a stratum (the coverage-only plan types), `annual = N`
and every item is selected with basis "Full coverage". This is how one code
path serves all six plan types.

**Quarter allocation** uses cumulative rounding, so the quarters always sum to
the annual figure:

    cum_i = Σ_{k≤i} wave_k
    q_i   = ceil(round(annual × cum_i, 6)) − ceil(round(annual × cum_{i−1}, 6))

The `round(…, 6)` matters. Without it, 0.35 + 0.35 + 0.30 accumulates to
1.0000000000000002, which rounds up one extra item.

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

and the first `q_i` are taken.

- The 0x1F unit separator is there so that `T1`+`0` and `T10`+`` cannot hash
  alike.
- A hash, not `math/rand`: it needs no gosec exception. Each item's rank
  depends only on the seed and its own reference, so adding an item never
  reshuffles the others.
- The seed is 32 random bytes from `crypto/rand`, hex-encoded and recorded when
  the population is frozen (step 0.9 of the example plan). Drawing it then,
  and not when the plan is created, means nobody can know the seed while the
  population can still change.

The selection itself:

- "Select Q*n* sample" is an explicit admin action. It writes the selections,
  stamps `selected_at` / `selected_by` on the quarter, and records them in
  history. After that the quarter is frozen. A second run is refused rather
  than recomputed.
- Top-ups are the items flagged with `topup_quarter = n`. A top-up that is
  already a statistical pick (in any quarter) is refused with a message that
  names the item and the quarter it was picked in.
- If a wave holds fewer items than `q_i`, the whole wave is taken and the
  shortfall is reported. The selection does not borrow from another wave.

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
| 0 | No deviations / effective |
| 1 | 1 deviation – extend sample. Suggests +50% of the next quarter's sample for that attribute. |
| ≥ 2 | Not effective – thematic finding |

Every individual Fail is still an asset-level finding.

### Coverage RAG

Per stratum and quarter, the cumulative planned figure (cum wave % × N) is
compared with the items actually attested by the calendar quarter end:

| RAG | When |
|---|---|
| Green | On or above plan |
| Amber | Within `rag_amber_points` (10) points below plan |
| Red | More than that below plan |
| Not due | Before the quarter end |

The word is always printed next to the colour.

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
| `source` | "Second Line Review" |
| `engagement` | "AP-2027-001 2027 Security NFR Controls Testing Plan" |
| `origin_ref` | plan / test / attribute reference |
| `linked_controls` | the attribute's 800-53 ids |
| `framework_refs` | the attribute's regulatory references and NFR key |
| `deficiency_type` | Operating Effectiveness |
| `condition` | the test's evidence summary and the result note |
| `criteria` | the attribute's requirement and pass criteria |
| `severity` | the tester's choice |

`source` is a closed vocabulary in Audit Findings, so the plan reference goes in
`engagement` and `origin_ref` instead. With no due date given, the finding's
own SLA defaults apply.

The finding's id and reference are stored on the result. The dashboard reads
open and overdue counts by severity for the findings linked to the plan.

### Thematic push to the risk register

A Not effective conclusion can be pushed to the risk register: "Push to risk
register" creates a `riskregister.Risk`.

- Title: attribute and stratum.
- Asset: the stratum.
- Vulnerability: the deviation summary.
- Notes: the plan reference.

The risk id is stored in `assurance_plan_thematic`. Crisis exercises only cite
risks and do not create them, so this is a new, small write path through the
register's existing service.

### Test attributes from the catalogs

Attributes are seeded from chosen Security NFRs (`nfr_key` = the NFR's record
key) or from chosen 800-53 controls. They are copied onto the plan. Control ids
are checked against `nist_controls`, and any that do not resolve go in
`unresolved_refs`. They are never presented as resolved.

The shipped NFR catalog numbers its entries `0.1`, `1.1` and so on. Appendix A's
`NFR-01`..`NFR-16` do not exist there, so they are seeded as plan-local
attributes. A plan-local attribute can be linked to a catalog NFR later.

### Proposed populations

The Policy review and Remediation roadmap types can propose a population:

- Policy review: from `policy_documents` whose `next_review_date` falls in the
  period, plus any that the review cadence makes due.
- Remediation roadmap: from open audit findings.

Like a CSV paste, the proposal goes through preview, validation and a diff
before it is applied.

### Population import

There is no upload. The population is structured data pasted as CSV text into
a textarea and posted as JSON. The preview step:

- parses the text and validates each row (unknown stratum, duplicate ref,
  invalid wave or date)
- returns a diff against the current population: added, changed and removed
  rows

Nothing is written until the apply step. This is not document ingestion. No
file is accepted, and nothing is extracted from a document.

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
- `GET /assurance-plans/:id/population` (phase 2),
  `GET /assurance-plans/:id/tests?quarter=` (phase 3)
- `GET /assurance-plans/:id/sampling` (phase 2): sizes, allocation, reviews
  due, capacity, each with its inputs
- `GET /assurance-plans/:id/lint`: the approval gate's findings, without
  approving
- `GET /assurance-plans/:id/dashboard` (phase 3)
- `GET /assurance-plans/:id/history`, `GET /assurance-plans/:id/versions`,
  `GET /assurance-plans/:id/versions/:n`. A version is served as the bytes
  stored at approval, with their SHA-256 in `X-Snapshot-SHA256`, so a reader
  can check the copy against the hash.
- Phase 4: `GET /assurance-plans/:id/export.json` and
  `GET /assurance-plans/:id/export/:table.csv` (steps, quarters, population,
  tests, deviations)
- `GET /assurance-plans/:id/pack.pdf`, `GET /assurance-plans/:id/report/quarter/:q.pdf`
  and `GET /assurance-plans/:id/report/annual.pdf`. With `?bundle=1`, or when no
  Typst engine is found, these return the sources zip instead.

**Admin**

- `POST /assurance-plans` (`{"type": …}` or `{"example": "appendix-a"}`),
  `PUT /assurance-plans/:id` (the header), `DELETE /assurance-plans/:id` (a
  Draft with no versions only)
- `PUT /assurance-plans/:id/steps | quarters | governance` (and, in phases 2
  and 3, `strata | attributes`) replaces the whole list. These answer 409 while
  the design is locked. A quarter keeps its selection stamp across an edit.
- `POST /assurance-plans/:id/quarters/generate` (`{"dates": true}`): resets
  the quarters to the default cycle, keeping each quarter's retest and overhead
  days, and with `dates` regenerates the template's quarterly steps from them,
  keeping each step's status by reference
- `PATCH /assurance-plans/:id/steps/:stepID` (`{"status": …}`): a step's status,
  which can move on a locked design; refused only once the plan is closed
- Phase 2: `POST /assurance-plans/:id/population/preview` and
  `POST /assurance-plans/:id/population`: the CSV text as JSON, then apply.
  `POST /assurance-plans/:id/population/propose?source=policies|findings`.
- `PATCH /assurance-plans/:id/population/:itemID`: execution fields only.
- `POST /assurance-plans/:id/quarters/:q/select`
- Phase 3: `POST /assurance-plans/:id/attributes/seed`
  (`{"nfr_keys": […], "control_ids": […]}`)
- `POST /assurance-plans/:id/tests` and `PUT /assurance-plans/:id/tests/:testID`
  (results included), `POST /assurance-plans/:id/tests/:testID/qa`
- `POST /assurance-plans/:id/results/:resultID/finding`,
  `POST /assurance-plans/:id/thematic`
- `POST /assurance-plans/:id/submit | return | approve | activate | close | change-request | clone`.
  `approve` records the signed-in user as approver; a name in the body counts
  only when there is no session (local mode). A blocked approval answers 422
  with `{"error", "findings"}`; a move the lifecycle does not allow answers
  409.
- AI (phase 6): `POST /assurance-plans/:id/ai/draft | attributes | narrative`
  and `POST /assurance-plans/:id/ai/proposals/:pid/decide`

The JSON export is a dedicated type in the `TemplateExport` style. It carries
no internal ids: rows refer to each other by `step_ref`, `item_ref`,
`attr_ref` and `test_ref`. Its collections are never null.

## Documents

- **Committee approval pack.** `templates/typst/assurance-plan-pack.typ`, built
  from `lib.typ` and `brand.typ`, 16:9 landscape. It is registered in
  `internal/doctemplate` as a new kind, `assurance_pack`. The slides, in order:
  cover; decisions requested; what DORA asks of the control function; objective;
  scope; approach; sampling and deviation rule; roadmap; quarterly targets;
  operating cycle; governance; resourcing and delivery risks; next steps.
- **Quarterly progress report** and **annual conclusion**. Both use
  `business-report.typ` (kind `report`), with the basis-of-reporting and
  limitations section. The annual conclusion is labelled "Input to the ICT
  risk management framework review report (RTS (EU) 2024/1774 Art. 27)".
- **Samples**: `templates/samples/assurance-plan-sample.json` and the two
  report samples are generated from Appendix A and included in
  `templates/build.sh`.

## Knowledge API

There are two read-only kinds:

- `assurance_plan`: the plan with its quarter targets, coverage and
  conclusions
- `assurance_plan_test`: one per test, with item, stratum, quarter, the
  per-attribute results and the finding references

Both are read fresh, like exercises, because they change during execution.
Both are included in `/api/knowledge/export`.

## AI assist (optional)

AI calls go through `aiprovider.Router`, are logged to `ai_usage_log`, and use
the model selected in Settings. Every feature works with no model configured.

The assists:

- draft a plan from a brief
- propose test attributes from a lexically retrieved shortlist of NFRs,
  controls and Regulation Coverage clauses (the model judges the shortlist; it
  does not browse)
- draft committee narrative

Rules for every output:

- It is a proposal with the model, the prompt hash and the timestamp. A person
  accepts, edits or rejects it.
- Every reference is resolved against the catalogs. A reference that does not
  resolve is marked unresolved.
- The standing prompt says:
  - say when you do not know
  - never invent a control id, threshold, deadline or article
  - keep what the regulation requires separate from good practice

## Backup and sync

- Every `assurance_plan*` table is in the JSON snapshot, in parent-first order,
  with `idKeyed` and `strat: skipInSync`. `SnapshotVersion` went from 15 to 16
  with phase 1's eight tables, and a v15-or-older backup restores with the
  module empty. A later phase that adds tables adds them to the same list and
  bumps the version again.
- `audit_findings.origin_ref` (phase 3) joins the snapshot's column list for
  that table. An older backup restores with it blank.
- The tables are excluded from `-sync-to` / `-sync-from` for the same reason as
  crisis exercises and Audit Findings: each plan is a tree of rows joined by
  autoincrement ids. To move plans between deployments, use the snapshot
  export/import.

## Sources

- IIA, Global Internal Audit Standards (2024): Standards 9.4 (Internal Audit Plan), 9.5, 11.3, 13.x (engagement planning), 14.3 (evaluation of findings), 14.6 (engagement documentation), 15.2 (confirming implementation)
- AICPA, Audit Guide: *Audit Sampling* (attribute sampling, sample-size tables for tests of controls)
- ISACA, IS Audit and Assurance Guideline 2208 *Sampling*; ISACA IT Assurance Framework (ITAF)
- Regulation (EU) 2022/2554 (DORA): Art. 5 (governance and organisation), Art. 6 (ICT risk management framework; 6(4) control function independence, 6(5) annual review), Art. 8 (identification; 8(1) inventory and classification, 8(4) CIF mapping), Art. 24 (general requirements for testing), Art. 25 (testing of ICT tools and systems)
- Commission Delegated Regulation (EU) 2024/1774 (RTS on ICT risk management framework): Arts. 2–27, in particular Art. 2(2)(c), Arts. 4–5 (ICT asset management) and Art. 27 (report on the ICT risk management framework review)
- NIST SP 800-53A Rev. 5, *Assessing Security and Privacy Controls in Information Systems and Organizations*: assessment methods (examine, interview, test) and objects
- NIST SP 800-53 Rev. 5: control identifiers referenced by test attributes
