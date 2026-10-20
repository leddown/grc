# Data model

The 58 tables the application creates, what each holds, and how each travels
between databases. Taken from a database created by the current code on
2026-10-10 (`internal/db/sqlite.go`; `internal/db/postgres.go` creates the
same logical schema for PostgreSQL).

## How the schema is managed

- There are no migration files. The schema is one `CREATE TABLE IF NOT EXISTS`
  script per dialect, run on every start, followed by column additions for
  databases created by an older version (`ensureColumn` on SQLite,
  `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` on PostgreSQL). A change to a table
  has to be made in both files.
- Booleans are `INTEGER` 0/1. Timestamps are text in RFC 3339. JSON is stored as
  text in `*_json` columns. Binary columns exist only for the Policy Studio
  documents.
- SQLite runs in WAL mode. Queries are written with `?` placeholders and
  rewritten to `$1, $2` for PostgreSQL by `db.Conn`.
- The two catalogs are seeded from JSON embedded in the binary
  (`internal/data`). A hash of the seed is kept in `app_state`; when it changes
  the catalog is reseeded and the NFR-control links are rebuilt.

## How tables travel

Three mechanisms move data, and they cover different sets of tables
(`internal/dbsync`):

| Mechanism | What it is | Covers |
|---|---|---|
| Sync (`-sync-to`, `-sync-from`) | Copies between two live databases, SQLite or PostgreSQL in either direction, table by table. | Tables marked *merged by key* below, and the derived link table, which is rebuilt. |
| JSON snapshot (Utilities → Export / Import; format version 15) | One file, imported as a full replace in one transaction. | Every table except the five marked *not transferred*. |
| Raw backup (Utilities → Backup) | A `VACUUM INTO` copy of the SQLite file. SQLite only. | Everything. |

*Backup only* tables are records made of rows joined by autoincrement ids
(a regulation with its findings, an exercise with its injects, an audit
finding with its actions) or state that means nothing away from its origin
(a Studio document's update log). They cannot be merged into another
database, so Sync skips them and the snapshot carries them.

In the tables below, **Key** is the primary key and, for merged tables, the
natural key rows are matched on.


## Installation state and accounts

Code: `internal/db`, `internal/authn`, `internal/settings`, `internal/user`.

### `app_state`

Key-value store. Holds the seed hashes (`seed.control_catalog_hash`, `seed.security_nfr_hash`) that decide whether a catalog is reseeded at startup, and every Settings preference under a `setting.` prefix (Wintermute URL, backend, model, the three agents, the guest-link switch).

- **Key:** `key`. **Travels:** not transferred.
- **Columns** (text unless a type is given): `key`, `value`.

### `app_secrets`

Stored credentials, encrypted with AES-256-GCM under the master key (`internal/secrets`). One row today: `wintermute_token`. Write-only from the browser's side; the page sees a four-character hint.

- **Key:** `name`. **Travels:** not transferred.
- **Columns** (text unless a type is given): `name`, `ciphertext`, `updated_at`, `updated_by`.

### `auth_users`

Sign-in accounts: bcrypt password hash, admin flag, and `allowed_pages_json`, the list of page paths a non-admin may open (empty means all).

- **Key:** `id`. **Travels:** merged by key (`username`).
- **Columns** (text unless a type is given): `id integer`, `username`, `password_hash`, `is_admin integer`, `allowed_pages_json`, `created_at`, `updated_at`.

### `auth_sessions`

One row per signed-in browser: the session token itself (not a hash), the user and the expiry (24 hours from sign-in).

- **Key:** `session_token`. **Travels:** not transferred.
- **References:** `user_id` → `auth_users.id` (on delete cascade).
- **Columns** (text unless a type is given): `session_token`, `user_id integer`, `expires_at`.

### `users`

The original sample CRUD table (name, e-mail). Served by `/users`. Nothing else reads it.

- **Key:** `id`. **Travels:** merged by key (`email`).
- **Columns** (text unless a type is given): `id integer`, `name`, `email`.

### `ai_usage_log`

One row per model call: provider, model, input and output tokens, time. The AI Chat usage panel sums it.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `provider`, `model`, `input_tokens integer`, `output_tokens integer`, `created_at`.

### `stored_json_documents`

JSON documents saved from the JSON view and the Jira pages (for example a Jira search result), with a name and a source label.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `name`, `source`, `content_json`, `created_at`.


## Catalogs

Code: `internal/controlcatalog`, `internal/securitynfr`, `internal/nfrlink`.

### `nist_controls`

The NIST SP 800-53 Rev 5 control catalog: one row per control or enhancement, with its family, baseline membership (low, moderate, high, privacy), the confidentiality / integrity / availability ratings, the threats it addresses (`threats_json`), requirements, discussion, related controls and the NIST appendix. Seeded from the embedded JSON; reseeded when that file's hash changes.

- **Key:** `id`. **Travels:** merged by key (`control_id`).
- **Columns** (text unless a type is given): `id integer`, `source_key`, `control_id`, `control_type`, `name`, `family`, `in_low integer`, `in_moderate integer`, `in_high integer`, `in_privacy integer`, `mapping_baselines_json`, `threats_json`, `confidentiality_status`, `integrity_status`, `availability_status`, `justification`, `potentially_common_inheritable`, `requirements`, `discussion`, `related_controls_json`, `appendix_json`.

### `nist_stride_mappings`

Legacy. Still created, synced and cleaned up when a control is deleted, but nothing reads it: a control's baselines and threats are on `nist_controls`.

- **Key:** `id`. **Travels:** merged by key (`control_id`).
- **Columns** (text unless a type is given): `id integer`, `control_id`, `baselines_json`, `threats_json`.

### `control_family_visibility`

One row per control family with an `enabled` flag. A hidden family is left out of every control list.

- **Key:** `family`. **Travels:** merged by key (`family`).
- **Columns** (text unless a type is given): `family`, `enabled integer`.

### `security_nfrs`

The Security NFR catalog: `record_key` (the stable key), `nfr_id`, summary, issue type, description, the NIST mapping as free text, additional details, implementation, domain, and `weight` (1 to 5, default 3). Seeded from the embedded JSON; a reseed keeps weights set in the application.

- **Key:** `id`. **Travels:** merged by key (`record_key`).
- **Columns** (text unless a type is given): `id integer`, `record_key`, `nfr_id`, `summary`, `issue_type`, `description`, `nist_mapping`, `additional_details`, `implementation`, `domain`, `weight integer`.

### `nfr_control_link_overrides`

Manual corrections to the automatic NFR-to-control matching: for one NFR and one mapping token, the control it should resolve to, or that it should not match.

- **Key:** `nfr_key, mapping_control_id`. **Travels:** merged by key (`nfr_key, mapping_control_id`).
- **Columns** (text unless a type is given): `nfr_key`, `mapping_control_id`, `override_control_id`, `matched integer`.

### `security_nfr_control_links`

Derived. One row per NFR and mapping token, with the control it resolved to and `matched`. Rebuilt from the two catalogs and the overrides; never edited.

- **Key:** `id`. **Travels:** rebuilt on the destination.
- **Columns** (text unless a type is given): `id integer`, `nfr_key`, `nfr_id`, `nfr_summary`, `nfr_domain`, `nist_mapping_raw`, `mapping_control_id`, `control_id`, `control_name`, `control_family`, `matched integer`.


## NFR Enrichment

Code: `internal/nfrenrich`.

### `nfr_source_documents`

A security document imported from the agent's library: title, the library document id, how it was extracted, hash and size.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `title`, `origin`, `url`, `filename`, `media_type`, `sha256`, `byte_size integer`, `library_doc_id integer`, `extract_via`, `uploaded_by`, `created_at`.

### `nfr_source_chunks`

The document's passages, copied in so retrieval and review work without the Wintermute server.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `ordinal integer`, `heading`, `body`.

### `nfr_enrichment_proposals`

An AI proposal for one field of one NFR: suggested text, rationale, confidence, the cited chunks, the model and prompt hash, and the reviewer's decision.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `nfr_key`, `document_id integer`, `field`, `suggested_text`, `rationale`, `confidence real`, `citations_json`, `status`, `model`, `prompt_hash`, `created_at`, `decided_at`, `decided_by`, `decided_note`.


## Risk register and audit findings

Code: `internal/riskregister`, `internal/auditfinding`.

### `security_risk_register`

One row per risk: scenario, inherent and residual likelihood and impact (1 to 5) with their computed scores, response, owner, status and review dates.

- **Key:** `id`. **Travels:** merged by key (`risk_id`).
- **Columns** (text unless a type is given): `id integer`, `risk_id`, `title`, `business_unit`, `asset`, `threat_source`, `vulnerability`, `likelihood integer`, `impact integer`, `inherent_score integer`, `current_controls`, `residual_likelihood integer`, `residual_impact integer`, `residual_score integer`, `response_strategy`, `response_action`, `owner`, `status`, `target_date`, `last_review_date`, `next_review_date`, `risk_appetite_aligned integer`, `notes`.

### `audit_findings`

One row per finding: the five elements (criteria, condition, cause, effect, recommendation), grading, ownership, management response, due dates and extensions, links to risks, controls and frameworks, validation and closure, risk acceptance, and who changed it.

- **Key:** `id`. **Travels:** backup only.
- **Columns** (text unless a type is given): `id integer`, `reference`, `title`, `source`, `engagement`, `auditor`, `finding_type`, `severity`, `deficiency_type`, `criteria`, `condition_text`, `cause`, `effect`, `recommendation`, `root_cause_category`, `business_unit`, `process_area`, `owner`, `management_response`, `management_response_text`, `identified_date`, `report_date`, `original_due_date`, `due_date`, `extension_count integer`, `extension_reason`, `status`, `repeat_finding integer`, `prior_reference`, `linked_risks`, `linked_controls`, `framework_refs`, `validated_by`, `validation_date`, `validation_notes`, `closure_evidence`, `closed_date`, `risk_accepted_by`, `risk_acceptance_rationale`, `risk_acceptance_expiry`, `notes`, `created_at`, `created_by`, `updated_at`, `updated_by`.

### `audit_finding_actions`

The action plan: one row per corrective or preventive action with owner, due date, status and evidence.

- **Key:** `id`. **Travels:** backup only.
- **References:** `finding_id` → `audit_findings.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `finding_id integer`, `ordinal integer`, `reference`, `description`, `action_type`, `owner`, `due_date`, `status`, `completed_date`, `evidence`.

### `audit_finding_history`

Append-only history of status, due-date, owner, severity and action-plan changes.

- **Key:** `id`. **Travels:** backup only.
- **References:** `finding_id` → `audit_findings.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `finding_id integer`, `at`, `actor`, `event`, `from_value`, `to_value`, `note`.


## Policy library

Code: `internal/policydocs`, `internal/clientprofile`, `internal/doctemplate`.

### `policy_documents`

A policy, standard, procedure, work instruction, guideline, TRA or letter: type, reference, status (draft, in_review, approved, retired), owner role, approver, classification, declared frameworks, review cadence, parent document, the client profile and template it was started from, `editor_format` (section editor or Studio), the projection bookkeeping, and `ai_policy` (inherit, local only, off).

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `client_id integer`, `client_name`, `doc_type`, `reference`, `title`, `status`, `owner_role`, `approver`, `classification`, `frameworks_json`, `effective_date`, `review_cadence_months integer`, `next_review_date`, `parent_document_id integer`, `summary`, `author`, `created_at`, `updated_at`, `editor_format`, `projection_version integer`, `projection_token`, `client_profile_id integer`, `template_id`, `template_version`, `ai_policy`.

### `policy_sections`

A document's sections in order: heading, body, kind, provenance (human, template, ai, imported), and for a Studio document the stable `uid`, the rich content (`content_json`, `blocks_json`) and `detached_at` for a removed section. These rows are the system of record that lint, coverage, exports, the knowledge API and approval read.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `ordinal integer`, `heading`, `body`, `section_kind`, `provenance`, `provenance_detail`, `updated_at`, `uid`, `content_json`, `detached_at`, `blocks_json`.

### `policy_section_controls`

The claim that a section satisfies a control: framework, control id, coverage (full, partial, supporting) and a note.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **References:** `section_id` → `policy_sections.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `section_id integer`, `control_id`, `framework`, `coverage`, `note`, `created_at`.

### `policy_versions`

An approved version: label, approver, change summary, the snapshot and its SHA-256, and the rich content.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `version_label`, `approved_by`, `approved_at`, `change_summary`, `snapshot`, `snapshot_sha256`, `content_json`.

### `client_profiles`

The clients policies are written for, with an optional reference to the CRM client on the Wintermute server.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `name`, `crm_ref`, `notes`, `created_at`, `updated_at`.

### `client_profile_facts`

Facts about a client (names, roles, systems, periods). A policy's fact tokens resolve from here; a missing fact blocks approval.

- **Key:** `client_id, key`. **Travels:** merged by key (`client_id, key`).
- **References:** `client_id` → `client_profiles.id` (on delete cascade).
- **Columns** (text unless a type is given): `client_id integer`, `key`, `value`, `value_type`, `source`, `updated_by`, `updated_at`.

### `policy_templates`

Document templates made in the application: draft and published JSON, version, origin, the library document a draft came from, and the model that drafted it.

- **Key:** `id`. **Travels:** merged by key (`template_id`).
- **Columns** (text unless a type is given): `id integer`, `template_id`, `status`, `draft_json`, `published_json`, `published_version`, `origin`, `source_library_id integer`, `source_title`, `sources_json`, `notes_json`, `ai_provider`, `ai_model`, `input_tokens integer`, `output_tokens integer`, `created_by`, `created_at`, `updated_by`, `updated_at`, `published_by`, `published_at`.

### `doc_template_brand`

The brand settings (one row) that parameterise the Typst and LaTeX output.

- **Key:** `id`. **Travels:** merged by key (`id`).
- **Columns** (text unless a type is given): `id integer`, `settings_json`, `updated_at`, `updated_by`.


## Policy Studio

Code: `internal/policystudio`, `internal/policyai`.

### `policy_doc_state`

The live Yjs document of a Studio policy, as a binary state, with the token that ties it to the projected section rows.

- **Key:** `document_id`. **Travels:** backup only.
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `document_id integer`, `state blob`, `projection_token`, `updated_at`.

### `policy_doc_updates`

The Yjs update log written between state compactions.

- **Key:** `id`. **Travels:** backup only.
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `upd blob`, `created_at`.

### `policy_doc_snapshots`

Point-in-time copies of a Studio document: periodic, at each status change, and before a document is moved into the Studio.

- **Key:** `id`. **Travels:** backup only.
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `reason`, `format`, `state blob`, `created_by`, `created_at`.

### `policy_comment_threads`

A comment thread anchored on the live text (Yjs relative positions and the quoted text), with visibility (internal or shared with guests), kind and status.

- **Key:** `id`. **Travels:** backup only.
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `section_uid`, `anchor_start`, `anchor_end`, `quote`, `visibility`, `kind`, `suggestion_suid`, `status`, `created_by`, `created_at`, `updated_at`, `resolved_by`, `resolved_at`.

### `policy_comments`

The messages in a thread, with the author and the kind of author (`author_kind`).

- **Key:** `id`. **Travels:** backup only.
- **References:** `thread_id` → `policy_comment_threads.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `thread_id integer`, `author`, `author_kind`, `body`, `created_at`.

### `policy_studio_audit`

Append-only record of review events: suggestions accepted and rejected, comment threads created, resolved and reopened, guests joining, leaving and being removed, share links created and revoked.

- **Key:** `id`. **Travels:** backup only.
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `actor`, `actor_kind`, `event`, `detail_json`, `created_at`.

### `policy_ai_proposals`

One AI request on a document: who asked, the action and scope, the provider, model and agent that answered, hashes of the prompt and context, token counts, and the answer.

- **Key:** `id`. **Travels:** backup only.
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `requested_by`, `action`, `scope`, `instruction`, `provider`, `model`, `served_by`, `agent`, `prompt_sha256`, `context_fingerprint`, `status`, `summary`, `answer_markdown`, `mappings_json`, `new_facts_json`, `input_tokens integer`, `output_tokens integer`, `created_at`.

### `policy_ai_edits`

Each edit in a proposal: the operation, the block and section it targets, the quote it anchors on, the replacement, rationale, citations, how validation and placement went, and the person's decision.

- **Key:** `id`. **Travels:** backup only.
- **References:** `proposal_id` → `policy_ai_proposals.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `proposal_id integer`, `suid`, `op`, `block_id`, `section_uid`, `quote`, `fragment_json`, `rationale`, `citations_json`, `validation`, `placement`, `decision`, `decided_by`, `decided_at`.

### `policy_share_links`

A guest invitation: the SHA-256 of its token, role (editor, commenter, viewer), whether AI is allowed, use limit, expiry and revocation.

- **Key:** `id`. **Travels:** not transferred.
- **References:** `document_id` → `policy_documents.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `document_id integer`, `token_sha256`, `label`, `role`, `allow_ai integer`, `max_uses integer`, `uses integer`, `created_by`, `created_at`, `expires_at`, `revoked_at`.

### `policy_guest_sessions`

A guest who joined through a link: the SHA-256 of the session cookie, display name, last seen, expiry and how the session ended.

- **Key:** `id`. **Travels:** not transferred.
- **References:** `link_id` → `policy_share_links.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `link_id integer`, `session_sha256`, `display_name`, `created_at`, `last_seen_at`, `expires_at`, `ended_at`, `end_reason`.


## Regulation Coverage

Code: `internal/regcoverage`.

### `reg_coverage_regulations`

An imported regulation: title, framework and how it was recognised, the library document it came from, extraction method and notes, status and when it was analysed.

- **Key:** `id`. **Travels:** backup only.
- **Columns** (text unless a type is given): `id integer`, `title`, `framework`, `framework_name`, `source_ref`, `detected integer`, `library_doc_id integer`, `filename`, `media_type`, `sha256`, `byte_size integer`, `extract_method`, `extract_notes`, `body_text`, `status`, `status_detail`, `uploaded_by`, `created_at`, `analyzed_at`.

### `reg_coverage_sections`

The articles and annexes the regulation was cut into.

- **Key:** `id`. **Travels:** backup only.
- **References:** `regulation_id` → `reg_coverage_regulations.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `regulation_id integer`, `ref`, `label`, `title`, `category`, `body`, `position integer`, `confidence`.

### `reg_coverage_findings`

The analysis of one section in one revision: relevance, what it requires, commentary, gaps, the verbatim quote and whether it was found in the text, confidence, model and prompt hash.

- **Key:** `id`. **Travels:** backup only.
- **References:** `section_id` → `reg_coverage_sections.id` (on delete cascade); `regulation_id` → `reg_coverage_regulations.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `regulation_id integer`, `section_id integer`, `relevant integer`, `requirement`, `commentary`, `gaps`, `quote`, `grounded integer`, `confidence`, `model`, `prompt_hash`, `revision integer`, `created_at`.

### `reg_coverage_mappings`

What a finding maps to: an NFR or a control, with rationale, confidence, whether it came from the curated crosswalk or the model, and whether the reference exists in the catalog.

- **Key:** `id`. **Travels:** backup only.
- **References:** `finding_id` → `reg_coverage_findings.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `finding_id integer`, `kind`, `ref`, `title`, `rationale`, `confidence`, `source`, `known integer`.

### `reg_coverage_versions`

An immutable JSON snapshot of the report per version.

- **Key:** `id`. **Travels:** backup only.
- **References:** `regulation_id` → `reg_coverage_regulations.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `regulation_id integer`, `number integer`, `summary`, `note`, `snapshot`, `created_at`, `created_by`.

### `reg_coverage_chat`

The conversation about a report.

- **Key:** `id`. **Travels:** backup only.
- **References:** `regulation_id` → `reg_coverage_regulations.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `regulation_id integer`, `version integer`, `role`, `content`, `model`, `actor`, `session_id`, `created_at`.

### `reg_coverage_sources`

Legacy: the uploaded bytes from when this application read documents itself. No longer written.

- **Key:** `regulation_id`. **Travels:** backup only.
- **References:** `regulation_id` → `reg_coverage_regulations.id` (on delete cascade).
- **Columns** (text unless a type is given): `regulation_id integer`, `media_type`, `filename`, `byte_size integer`, `content blob`.


## Risk & Crisis Exercises

Code: `internal/crisisexercise`.

### `crisis_ex_exercises`

An exercise: scenario, entity and jurisdiction, format, status, TLP marking, schedule, the control team, and provenance if the design was generated.

- **Key:** `id`. **Travels:** backup only.
- **Columns** (text unless a type is given): `id integer`, `reference`, `title`, `summary`, `format`, `kind`, `audience`, `entity_name`, `entity_type`, `jurisdiction`, `supervision`, `critical_functions`, `threat_actor`, `threat_narrative`, `initial_vector`, `scenario_key`, `status`, `tlp`, `scheduled_for`, `duration_minutes integer`, `started_at`, `ended_at`, `facilitator`, `control_team`, `evaluators`, `ai_generated integer`, `model`, `prompt_hash`, `created_at`, `created_by`, `updated_at`, `updated_by`.

### `crisis_ex_objectives`

What the exercise sets out to test, with success criteria and the rating given afterwards.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `ordinal integer`, `code`, `text`, `capability`, `success_criteria`, `rating`, `notes`.

### `crisis_ex_phases`

The arc: twelve phases seeded on creation, each with purpose, entry and exit criteria, lead role and timing.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `ordinal integer`, `phase_key`, `name`, `purpose`, `entry_criteria`, `exit_criteria`, `lead_role`, `offset_minutes integer`, `duration_minutes integer`, `status`, `notes`.

### `crisis_ex_injects`

The Master Scenario Events List: messages delivered to players on a clock, with expected actions and decisions, and model provenance if generated.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `phase_id integer`, `ordinal integer`, `code`, `offset_minutes integer`, `title`, `body`, `channel`, `from_actor`, `to_actor`, `inject_type`, `expected_actions`, `expected_decision`, `decision_owner`, `evaluation_notes`, `difficulty`, `ai_generated integer`, `model`, `prompt_hash`, `confidence`, `created_at`.

### `crisis_ex_responses`

What the players actually did with an inject, and the evaluator's observations.

- **Key:** `id`. **Travels:** backup only.
- **References:** `inject_id` → `crisis_ex_injects.id` (on delete cascade); `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `inject_id integer`, `delivered_at`, `responded_offset integer`, `outcome`, `actual_actions`, `observations`, `evaluator`, `created_at`, `updated_at`.

### `crisis_ex_decisions`

The decision log: options, the decision, who made it and with what authority.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `phase_id integer`, `offset_minutes integer`, `title`, `options`, `decision`, `rationale`, `made_by`, `role`, `authority`, `reversible integer`, `regulatory_implication`, `customer_impact`, `created_at`.

### `crisis_ex_classification`

The DORA major-incident classification the team recorded: each materiality criterion, the awareness and classification times, and the team's verdict against the computed one.

- **Key:** `exercise_id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `exercise_id integer`, `aware_offset integer`, `classified_offset integer`, `critical_services_affected integer`, `clients_affected`, `clients_material integer`, `transactions_affected`, `transactions_material integer`, `reputational_impact`, `reputational_material integer`, `downtime_minutes integer`, `duration_material integer`, `geographical_spread`, `geographical_material integer`, `data_losses`, `data_losses_material integer`, `economic_impact`, `economic_material integer`, `personal_data_breach integer`, `nis2_significant integer`, `major integer`, `rationale`, `team_verdict`, `notes`, `updated_at`, `updated_by`.

### `crisis_ex_clocks`

The notification obligations the scenario started (DORA, NIS2, GDPR, ECB, schemes, customers), each with its due time, the actual time and whether it was met.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `ordinal integer`, `regime`, `authority`, `label`, `basis`, `due_offset integer`, `actual_offset integer`, `status`, `evidence`, `notes`, `source`.

### `crisis_ex_findings`

After-action findings with severity, root cause, recommendation, owner and due date.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `phase_id integer`, `ordinal integer`, `code`, `title`, `category`, `severity`, `description`, `root_cause`, `evidence`, `recommendation`, `owner`, `due_date`, `status`, `risk_ref`, `ai_generated integer`, `model`, `created_at`, `created_by`.

### `crisis_ex_participants`

Who took part, in which role.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `name`, `role_key`, `org`, `player integer`, `attended integer`, `contact`, `notes`.

### `crisis_ex_references`

Citations: any objective, phase, inject, decision, clock or finding pointing at a control, NFR, regulation clause, policy, risk or framework.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `owner_kind`, `owner_id integer`, `ref_kind`, `ref`, `title`, `note`, `source`, `known integer`, `url`, `created_at`.

### `crisis_ex_versions`

Immutable after-action report versions.

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `number integer`, `kind`, `summary`, `note`, `snapshot`, `created_at`, `created_by`.

### `crisis_ex_chat`

The module's AI conversations (expert personas and the agent).

- **Key:** `id`. **Travels:** backup only.
- **References:** `exercise_id` → `crisis_ex_exercises.id` (on delete cascade).
- **Columns** (text unless a type is given): `id integer`, `exercise_id integer`, `persona`, `role`, `content`, `model`, `actor`, `scope`, `session_id`, `created_at`.

## Tables that are not transferred, and why

| Table | Reason |
|---|---|
| `app_state` | This installation's own configuration: which Wintermute server and agents it asks. It should not follow the data to another server. |
| `app_secrets` | Ciphertext whose key file is never in a snapshot. Copying it would move an unreadable value and hide that the credential has to be set again. |
| `auth_sessions` | Live sign-in sessions. |
| `policy_share_links`, `policy_guest_sessions` | Live credentials for people outside the installation. A restored link would let someone back into a document on a server that never invited them. |

## Relationships worth knowing

- **NFR → control** is not a foreign key. An NFR's `nist_mapping` is free text
  ("PL-8, CM-8"). `nfrlink.Rebuild` extracts the control ids from it with a
  regular expression, normalises them, applies any override, looks each up in
  `nist_controls`, and writes `security_nfr_control_links`. The Exceptions
  page, the reports, the knowledge API and the Exceptions Workbook all read
  that derived table.
- **Policy section → control** (`policy_section_controls`) stores the control
  id as text with its framework, so a section can claim a control that is later
  removed from the catalog. Lint reports that.
- **Cross-module links are text.** An audit finding's linked risks and
  controls, a crisis exercise reference, and a regulation mapping all hold ids
  as text and check them against the catalogs when read. Deleting a control
  does not cascade anywhere.
- **Studio documents exist twice on purpose.** `policy_doc_state` is the live
  collaborative document; `policy_sections` is its validated projection. The
  server writes the projection; nothing a browser sends is stored as content
  directly.

