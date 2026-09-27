# Audit Findings & Remediation

The module at `/audit-findings` (Compliance & Risk) records what an auditor,
examiner or assessor found, what management agreed to do about it, and the
evidence that the fix was validated before the finding closed. Code:
`internal/auditfinding`. Tables: `audit_findings`, `audit_finding_actions`,
`audit_finding_history`.

## Practice it follows

| Practice | Source | How the module applies it |
|---|---|---|
| A finding has five elements: **criteria, condition, cause, effect, recommendation** | GAO Yellow Book (Government Auditing Standards); IIA Global Internal Audit Standards | One field each, with guidance in the form. The recommendation should address the cause. |
| Management responds (agree / partially agree / disagree), and agreement comes with a **dated action plan and a named owner** | IIA; common internal-audit practice | `management_response`, and action plan items that each need a description and an owner. An item without its own due date inherits the finding's. |
| **Follow-up**: confirm the actions were implemented, and track progress until they are | IIA Global Internal Audit Standards, Standard 15.2 | Status lifecycle, overdue and aging figures, and a history of every status, due-date, owner, severity and action-plan change. |
| A finding closes on **validated evidence**, not on management's say-so. Validation covers design and operating effectiveness | IIA; practitioner guidance on issue validation | Closed needs `validated_by`, `validation_date` and `closure_evidence`, and the validator cannot be the owner. |
| **Extensions** are exceptions that need approval, and slippage stays visible | Common issue-management practice | `original_due_date` is fixed at the first commitment. Moving the due date later needs a new `extension_reason` and increments `extension_count`. The client cannot set either field. |
| When a finding will not be fixed, record a **formal, time-bound risk acceptance** | Common practice; ISO 27001 risk treatment | Risk Accepted needs an approver, a rationale and a future expiry. Expired acceptances are counted on the dashboard. |
| **Repeat findings** show that earlier remediation did not hold | IIA; audit committee reporting practice | `repeat_finding` + `prior_reference`, counted as "repeat findings open". |
| Grade a finding in the vocabulary it arrived in | SOX / PCAOB AS 2201 (deficiency, significant deficiency, material weakness); ISO 19011 (major / minor nonconformity, observation, opportunity for improvement) | `finding_type` covers both. Observations and OFIs allege no breached requirement, so they get no default due date and can close without an action plan. |
| Tell apart a control that was **never designed** to work and one that was **not operated** | SOX / ITGC testing practice | `deficiency_type`: Design, Operating Effectiveness or Documentation. |

## Fields

- **Identification:** reference (auto `AF-YYYY-NNN`), title, source, engagement,
  auditor, finding type, severity, deficiency type, identified date, report date.
- **The finding:** criteria, condition, cause, root cause category, effect,
  recommendation.
- **Ownership & response:** accountable owner, business unit, process area,
  management response, response detail.
- **Remediation:** due date, original due date, extension count and reason,
  and the action plan. Each action has a reference, description, type
  (corrective/preventive), owner, due date, status, completed date and evidence.
- **Links:** risk register IDs, control IDs, framework or regulation
  references, repeat flag, prior reference.
- **Closure:** validated by, validation date, closure evidence, validation
  notes, closed date. **Risk acceptance:** approver, rationale, expiry.
- **Audit trail:** created/updated at and by, and the history table.

## Lifecycle

`Draft → Open → In Remediation → Pending Validation → Closed`, with
`Risk Accepted` as the alternative exit. A closed finding moved back to any open
state is stored as `Reopened`.

| Into | Requires |
|---|---|
| In Remediation | Management does not disagree, and there is at least one action |
| Pending Validation | Every live (not cancelled) action is Implemented or Validated. Advisory types may have none |
| Closed | Independent validator, validation date, closure evidence, and every live action Implemented (these become Validated on close) |
| Risk Accepted | Approver, rationale, and an expiry in the future |

If a finding is raised without a due date, its severity sets the default:
Critical 30 days, High 90, Medium 180, Low 365. Any other due date you enter
takes priority.

## Dashboard

The dashboard shows how many findings are open and overdue, how many are
pending validation, how many critical or high findings are open, how many
repeat findings are open, and how many findings have been extended. It also
counts risk acceptances, flagging any that have expired, and findings closed in
the last 90 days, and shows the average number of days to close. Open findings
are grouped by age: 0-30, 31-90, 91-180, 181-365 and 365+ days.

## API

Reads are available to anyone with access to `/audit-findings`. Writes require
admin, the same split as the risk register.

- `GET /audit-findings/data?search=&status=&severity=&source=&open=true&overdue=true`
  (supports `page` / `per_page`)
- `GET /audit-findings/data/:id`: returns the finding with its actions and history
- `GET /audit-findings/summary`, `GET /audit-findings/vocabulary`
- `POST /audit-findings`, `PUT /audit-findings/:id` (the full finding, with the
  action plan as `actions`), `DELETE /audit-findings/:id`

## Backup

The findings are part of the JSON snapshot (snapshot version 9). Like Regulation
Coverage and the crisis exercises, they are excluded from `-sync-to` /
`-sync-from`, because their child rows are joined by autoincrement ids.

## Sources

- IIA, Global Internal Audit Standards (2024), Standard 15.2 "Confirming the Implementation of Recommendations or Action Plans"
- U.S. GAO, Government Auditing Standards (Yellow Book), elements of a finding — https://www.gao.gov/yellowbook
- ISO 19011, grading of audit findings — https://rigcert.education/resources/is-this-nonconfority-or-opportunity-for-improvement-understanding-the-difference
- Origami Risk, "From Audit Findings to Action" — https://www.origamirisk.com/resources/insights/from-audit-findings-to-action-best-practices-for-issues-management-and-remediation-tracking/
- Internal Audit Guide, "Issue Validation in Internal Audit" — https://internalauditguide.com/2025/03/issue-validation-in-internal-audit/
- Auditing Authority, "Audit Findings and Management Response" — https://auditingauthority.com/audit-findings-and-management-response/
- ISACA Now, "Follow-Up Audits and Follow-Up Process" (2025) — https://www.isaca.org/resources/news-and-trends/isaca-now-blog/2025/follow-up-audits-and-follow-up-process-the-auditors-impact-litmus-tool
