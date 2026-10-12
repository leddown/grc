# Features

What each module and page does, the rules it enforces, and where the code is.
Organised as the sidebar is. Checked against the code on 2026-10-10. Routes are
in [HTTP_API.md](HTTP_API.md), tables in [DATA_MODEL.md](DATA_MODEL.md).

Three patterns hold almost everywhere, so they are stated once:

- **Reading needs a signed-in user; changing needs an admin.** Exceptions are
  named where they occur.
- **A page is a shell plus data routes.** The page's JavaScript fetches JSON
  and renders it; filters and selections are browser state unless a section
  says they are stored.
- **AI output is a proposal.** A model never writes to a catalog or a document.
  A person accepts or rejects what it produced, and the record keeps the model
  and a hash of the prompt.

---

## Catalog

### Controls (`/controls`)

The NIST SP 800-53 Rev 5 catalog: 1,193 controls and enhancements, seeded from
JSON embedded in the binary. Code: `internal/controlcatalog`.

- **List and detail.** Search by text, filter by baseline (low, moderate,
  high, privacy) and by rating (confidentiality, integrity, availability).
  The list loads a slim record; selecting a control fetches the full one:
  requirements, discussion, related controls, the threats it addresses, and
  the NIST appendix.
- **Detail page** (`/controls/detail/:controlID`): one control on a page of
  its own, linked from everywhere a control id is shown.
- **Export** (`/controls/export?format=csv|excel`): the filtered list as CSV
  or as Excel 2003 XML.
- **Control Editor** (`/controls/manage`, admin to save): edit any field of a
  control or delete one. A save or delete rebuilds the NFR-control links.
  With `ALLOW_JSON_SAVE=true`, "Rewrite JSON" writes the catalog back to the
  JSON file it was seeded from.
- **Hierarchy** (`/controls/hierarchy`): family → control → enhancement, with
  an RTF export of the selected control.
- **Family Filters** (`/controls/family-visibility`, admin to change): switch
  a control family off. A hidden family disappears from every control list in
  the application, including the data the Exceptions page and the reports
  read. Links to its controls remain.

A control has an id (`AC-2`, `AC-2(1)`), a type (control or control
enhancement), a family, baseline membership, three ratings held as text (a
non-empty value means the control carries that rating), and a list of threats.
The threats are the six STRIDE categories plus others present in the data,
such as Lateral Movement.

### Security NFRs (`/security-nfrs`)

The practice's catalog of security non-functional requirements: 109 records in
the seed. Code: `internal/securitynfr`.

- **List and detail panel.** Search by key, id or summary; filter by domain.
  The NIST mapping is shown as links to the control detail pages.
- **Security NFR Editor** (`/security-nfrs/manage`, admin to save): edit or
  delete an NFR. A save rebuilds the links.
- **Weights.** Every NFR has a weight from 1 (lowest) to 5 (highest), default
  3. The editor's list has a 1-5 control on each row that saves on click
  (`PUT /security-nfrs/:key/weight`), and the form has a Weight field. Nothing
  in the application computes with the weight yet except the Exceptions
  Workbook.
- **Reseeding.** When the embedded NFR file changes, the catalog is replaced
  from it and rows no longer in the file are deleted. A weight set in the
  application survives unless the file carries its own.

An NFR has a `record_key` (the stable identity), an NFR id such as `1.4`, a
summary, an issue type, a domain, description, additional details,
implementation, and its NIST mapping as free text.

### NFR-Control Links (`/security-nfrs/links`)

Shows how each NFR's NIST mapping resolved. Code: `internal/nfrlink`.

The link table is derived, never edited. `Rebuild` reads every NFR, extracts
control ids from the mapping text with a regular expression, normalises each,
and looks it up in the control catalog. Each token becomes a row marked
matched or unmatched. An NFR with no recognisable token gets one unmatched
row.

An admin can add an **override** for one NFR and one token: resolve it to a
named control, or mark it as not matching. Overrides are applied on every
rebuild. A rebuild runs at start-up when a catalog changed, after any control
or NFR edit, and on request.

### NFR Enrichment (`/nfr-enrichment`)

Proposes better text for thin NFR fields from a security document. Code:
`internal/nfrenrich`.

1. The document is uploaded to an agent's library on the Wintermute server,
   which extracts its text.
2. **Import** (admin) copies the document's passages in.
3. **Analyse** (admin) retrieves, for each NFR, the passages most relevant to
   it (BM25, at most six) and asks the model to propose text for one of three
   fields: description, additional details, or implementation.
4. A proposal must cite passages it was given. One that cites anything else is
   discarded before a reviewer sees it.
5. **Accept** (admin) writes the text, optionally edited, into that field of
   the NFR, and marks other pending proposals for the same field superseded.
   **Reject** records the decision. Nothing else in the module writes to the
   catalog.

Each proposal keeps its rationale, confidence, citations, model and prompt
hash. Any signed-in user can read the queue.

### Asset Types (`/asset-types`)

A lookup, with no storage. Choose an asset tier and the page lists the
controls of the matching baseline: TIER 1 and TIER 2 map to High, TIER 3 to
Moderate, TIER 4 to Low. TIER 0 and Other have no baseline.

---

## Compliance & Risk

### Risk Register (`/risk-register`, editor at `/risk-register/manage`)

A register of security risks. Code: `internal/riskregister`; method in
`RISK_REGISTER_FRAMEWORK.md`.

Each risk records the scenario (asset, threat source, vulnerability), inherent
likelihood and impact, current controls, residual likelihood and impact, the
response strategy and action, owner, status, target and review dates, and
whether it is within risk appetite. Likelihood and impact are 1 to 5; the
inherent and residual scores are their products and are computed by the
service. An admin creates, edits and deletes.

### Audit Findings (`/audit-findings`)

Findings from audit, regulators and certification, tracked to a validated
close. Code: `internal/auditfinding`; detail in `AUDIT_FINDINGS.md`.

- A finding is written as **criteria, condition, cause, effect and
  recommendation**, graded by type (deficiency, significant deficiency,
  material weakness, nonconformities, observation) and severity, with an
  owner and a management response.
- It carries a dated **action plan**. Each action has an owner, due date,
  status and evidence.
- **Lifecycle:** Draft → Open → In Remediation → Pending Validation → Closed,
  or Risk Accepted. The service enforces what each step needs: remediation
  needs at least one action and a response that is not a disagreement;
  pending validation needs every live action implemented; closing needs a
  validator, a date and closure evidence; risk acceptance needs an approver, a
  rationale and an expiry in the future. A closed finding moved back is stored
  as Reopened.
- The first due date is kept as the original. Moving the due date later needs
  a reason and counts as an extension. With no due date, severity sets one:
  Critical 30 days, High 90, Medium 180, Low 365.
- Every status, date, owner, severity and action change is written to a
  history table. References are numbered `AF-YYYY-NNN`.
- The **dashboard** counts open, overdue, pending validation, critical and
  high, repeat findings, extended findings, risk acceptances (flagging
  expired ones) and closures in the last 90 days, and groups open findings by
  age.

### Exceptions (`/exceptions`, detail at `/exceptions/detail`)

Builds an **exception set**: the Security NFRs a system does not meet. Code:
`internal/app/exceptions.go`, entirely in the browser over the catalog routes.

Selecting NFRs shows, live:

- **Linked Controls**: the distinct controls the selected NFRs are linked to
  through matched links.
- **C / I / A**: how many of those controls carry each rating.
- **Threat coverage**: for each threat, how many of those controls address it.

"Select Filtered" and "Clear Filtered" act on the current search and domain
filter. The detail page shows each selected NFR in full with its linked
controls, and prints. The selection is not stored: it travels in the detail
page's URL as `key` parameters.

Because the page reads the visible control list, a control in a hidden family
still counts as linked but not towards the ratings or threats.

### Policy Library (`/policies`, editor at `/policies/manage`)

Controlled documents written for clients. Code: `internal/policydocs`;
research in `POLICY_MODULE_FRAMEWORK.md`.

- **Types:** policy, standard, procedure, work instruction, guideline, and
  TRA. A document can name a parent, giving the policy → standard → procedure
  → work instruction hierarchy. The section kinds offered depend on the type.
- **Sections** have a heading, a body, a kind (purpose, scope, statements,
  roles, management commitment, coordination, compliance, exceptions,
  definitions, related documents, procedure steps, other) and a provenance
  (human, template, ai, imported).
- **Control mapping:** a section can claim to satisfy controls, each with a
  framework and a coverage level (full, partial, supporting).
- **Lint** checks a document against rules: it has sections, the sections
  required for its type are present, no section is empty, a statements
  section uses must / shall / should / may, no unresolved fact placeholder
  remains, no suggestion is still open (Studio documents), declared frameworks
  have mappings, and no mapped control has left the catalog. Some findings
  block approval.
- **Workflow:** draft → in review → approved → retired, with a return to
  draft. **Approve** needs an approver name, an effective date, an owner role
  and no blocking lint finding. In a multi-user installation the author
  cannot approve their own document; in single-user and local mode that check
  is off because it could never be satisfied.
- **Versions:** approval snapshots the document, stores the snapshot's SHA-256
  and assigns the next version label. An approved document is read-only until
  it is reopened.
- **Compare** shows the difference between any approved version and another
  version or the current text, as JSON or as a typeset redline PDF.
- **Exports:** HTML, Markdown, and the JSON contract the typesetting
  templates consume.

### Policy Studio (`/policies/:id/studio`)

A WYSIWYG editor in which several people edit one policy at once. Code:
`internal/policystudio`, `internal/policyai`, `web/policy-studio`; full
design and operator guide in `POLICY_STUDIO.md`.

- **Live co-editing** with presence and carets. The server holds the
  authoritative document and projects it into the same section rows the rest
  of the policy module reads.
- **Structure is enforced:** sections with stable ids and kinds, headings,
  lists, tables, links (https, http and mailto only), **fact tokens** and
  **control chips**. The server validates the document against a schema; a
  browser cannot store content the schema does not allow.
- **Suggest mode** records changes as suggestions. The Suggestions panel
  accepts or rejects them, singly or in bulk, by keyboard. Decisions are
  audited. Open suggestions block approval.
- **Comments** are anchored on the live text, internal or shared with guests,
  and can be resolved and reopened.
- **Provenance** shows who wrote what: template, consultant, customer or AI,
  and who accepted it.
- **AI proposals** (admin): inline actions such as *Make testable* and
  *Draft this section*, a section review, and questions in the AI dock. The
  answer must fit a strict contract; a malformed answer gets one repair turn.
  The server validates and anchors every edit (at most 25 per proposal)
  against the live document, previews it privately, and on request places it
  as suggestions attributed to the AI. Citations are checked. The AI never
  edits the document.
- **`ai_policy`** per document: inherit, local only (refused while the server
  could answer on a cloud backend), or off.
- **Guests:** an admin creates an invitation link with a role (editor,
  commenter, viewer), an expiry (default 8 hours, at most 7 days), a use limit
  and whether AI is allowed. A guest joins with a display name, sees only that
  document and only shared comment threads, and can be removed. Off until
  switched on in Settings; refused in local mode.
- **Workshop mode:** larger type, focus, present-and-follow, and a
  customer-safe view.
- **Migration:** a document written in the section editor can be moved into
  the Studio; a snapshot of the original is kept and never pruned.

### Policy Templates (`/policies/templates/manage`) and client profiles

Code: `internal/policystudio` (templates), `internal/clientprofile`.

- **Built-in templates** ship in the binary: the ICT and Information Security
  Policy, and skeletons for a standard, a procedure and a work instruction.
- **Templates made in the application** (admin): copy, import, edit, publish,
  retire and restore, and export as JSON. A template can be **drafted from a
  library document**: the model generalises a sample policy, replacing the
  client's specifics with fact tokens, and an admin reviews and publishes it.
- **Starting a document from a template** fills in the client's known facts
  and leaves the rest as unresolved tokens. A document can also be started
  from a library document, with the model mapping its passages into the chosen
  template as suggestions.
- **Client profiles** hold the facts policies state about a client. A fact is
  never supplied by a model or a template. An unresolved token blocks
  approval.

### Policy Coverage (`/policies/coverage`)

A matrix of frameworks and controls against the policy sections that claim
them. A control is reported as covered by an approved document, covered only
by a draft, covered only by supporting mappings, or uncovered. It is a query
over the mapping table, so one policy can satisfy several frameworks without
being written four times.

### Regulation Coverage (`/regulation-coverage`)

Maps an EU regulation, article by article, to the Security NFR catalog and
NIST 800-53. Code: `internal/regcoverage`, `internal/regmap`; detail in
`REGULATION_COVERAGE.md`.

1. The regulation is uploaded to the agent's library on the Wintermute server,
   which extracts the text and cuts it into articles.
2. **Import** (admin) settles the framework (DORA, NIS2, CRA, PCI DSS, or a
   generic EU profile) and reads the articles back. No model is called.
3. **Analyse** (admin): one model call per section and one for the summary.
   For each section a BM25 ranker shortlists ten controls and ten NFRs; the
   model judges only that shortlist. Where the framework has a curated seed
   crosswalk, it is given as a prior to confirm.
4. Each finding records whether the article is security-relevant, what it
   requires, the mappings with rationale and confidence, the gaps, and
   practitioner commentary.
5. **Checks on the output:** every mapped id is looked up in the catalog
   (otherwise tagged unknown); every finding must quote the section verbatim
   (otherwise tagged unverified quote); the model and prompt hash are stored.
6. The **report** is versioned and immutable, as a page, a PDF and JSON.
   **Chat** (admin) asks questions about it. **Revise** (admin) re-analyses
   one section with an instruction and writes the next version.

### Wiz Rules (`/wiz-rules`)

A form that drafts a runtime detection rule payload in Wiz's vocabulary
(event type, scope, severity, boolean conditions, string and regex
operators). It runs in the browser and stores nothing.

---

## Crisis Exercises (`/crisis-exercises`)

Designs, runs and reports a crisis exercise for a financial entity, from red
team through incident response, classification, crisis management,
communications, the board and the supervisory authorities. Code:
`internal/crisisexercise`; detail in `CRISIS_EXERCISE.md`.

- **Scenario library:** eight scenarios, each with a "trap door" the control
  team sees and players do not. Choosing one fills in the intelligence, the
  objectives and the citations.
- **The arc:** twelve phases seeded on creation, each with purpose, entry and
  exit criteria and a lead role.
- **MSEL:** injects delivered on a clock, each with expected actions and
  decisions. The model can generate them (admin); a generated inject records
  the model, prompt hash and confidence.
- **Delivery:** record the response to each inject, the decision log and the
  participants.
- **Regulatory clocks:** the team fills in the DORA materiality criteria; the
  module applies the combination rule of Delegated Regulation (EU) 2024/1772
  and derives each notification obligation with its own trigger (DORA initial,
  intermediate and final; NIS2; GDPR Article 33; the ECB channel; scheme and
  customer obligations). The team records when each notification went, and the
  report says which were met.
- **Jurisdictions:** Lithuania, Latvia, Estonia, a Baltic cross-border group,
  and a generic euro-area option. The jurisdiction decides the authorities
  named on each clock and the statement languages.
- **References:** an objective, phase, inject, decision, clock or finding can
  cite controls, NFRs, regulation clauses, policy clauses, risks and a seeded
  catalog of frameworks. The **Coverage** tab inverts that: what was tested,
  what cited it, and which of it a finding was raised against.
- **Findings** with severity, owner and due date, and immutable after-action
  **versions**.
- **Outputs:** the after-action report as PDF and JSON, the controller's MSEL
  as CSV marked with the TLP, and a player handout in Markdown with the
  answers removed.
- **AI:** generation, expert personas that advise from a role's point of
  view, an agent conversation, and the AI dock on these pages. Questions go to
  the Crisis Exercise agent named in Settings.

Every write, including recording what happened during delivery, needs an
admin: an exercise record is evidence a supervisor may read.

---

## Reporting & Data

### Reports (`/reports`)

One report: controls that no NFR links to, controls that at least one does,
or all, filtered to controls, enhancements or both, with the linked NFRs
beside each. Hidden families are left out. Code: `internal/reports`.

Two sample PDFs (`/reports/control-assessment.pdf`,
`/reports/risk-assessment.pdf`) demonstrate the HTML-to-PDF package
(`internal/reporting`) and are rendered from sample data.

### Exceptions Workbook (`/reports/exceptions-workbook`)

Downloads the Exceptions page as an `.xlsx` workbook that works without the
application. Code: `internal/exceptionbook`, `internal/xlsx`.

- It holds the NFR catalog with weights, the NIST controls and the links, and
  formulas that reproduce the page: choosing `Yes` beside an NFR updates the
  linked controls, the C / I / A counts, the threat coverage and the detail
  sheets.
- It adds what the page does not have: the weight of the selection, the
  weight of the catalog, the share, the average, and a count at each weight.
- Seven sheets: How to use, Exceptions, Summary, Selected NFRs, Selected
  controls, Links, Controls.
- Formulas use only functions Excel 2010 and LibreOffice share. Each is stored
  with its result, so a preview shows numbers before recalculating.
- `?key=` on the download opens the workbook with those NFRs selected.

The page's logic is JavaScript; the workbook restates it in Go and in
formulas. A change to one needs the other. A test has LibreOffice recalculate
the workbook and compares every cell.

### JSON_view (`/JSON_view`)

One panel that shows the NFR catalog as JSON (optionally one domain), one
control family as JSON, or a JSON document saved earlier. Saved documents are
in `stored_json_documents`.

### Jira JSON (`/jira/json`) and Jira Reports (`/jira/reports`)

A connector to Jira Cloud. Code: `internal/app/jira_pages.go`,
`internal/jira`; notes in `JIRA_CONNECTOR.md`.

The user enters a site URL (https only), an account e-mail and an API token.
They are sent with each request and are not stored on the server; the page
keeps the URL and e-mail in the browser and never the token. The pages test
the credentials, fetch a project, an issue or a JQL search as JSON, and run
preset reports. A result can be saved to the stored JSON documents. With
`ALLOW_JSON_SAVE=true` an admin can export a result to a file under
`exports/jira/` in the server's working directory.
The client retries on 429 and 503 and honours `Retry-After`.

### Change Log (`/changelog`)

Renders `CHANGELOG.md` from disk. The same mechanism serves ten of the
repository's markdown documents raw at `/knowledge/<name>`.

---

## AI Chat (`/ai-chat`) and the AI dock

Code: `internal/app/ai_chat.go`, `internal/aiprovider`; setup in
`AI_AGENT.md`.

- **AI Chat** is a conversation with the Wintermute server. The page chooses a
  backend and an agent from lists fetched from that server. A question is
  streamed: the page shows what the turn is doing while it runs, can stop it,
  and reports tokens in and out, the backend and model that answered, and the
  elapsed time. A usage panel sums `ai_usage_log`.
- **The AI dock** is on every page. It uses the agent set in Settings. On a
  Crisis Exercise page it asks that module's agent about the exercise on
  screen; on a Policy Studio page an admin's question goes to the proposal
  engine.
- **No credential and no model name travels from the browser.** Both are
  resolved on the server from Settings.
- Any signed-in user with the `/ai-chat` page grant can ask. Without a
  configured agent the model answers from its training data, which for
  questions about this installation is no answer.

---

## Documents

### Templates (`/templates`) and Template Brand (`/templates/manage`)

Typeset output for policies and reports. Code: `internal/doctemplate`,
`templates/`; detail in `DOCUMENT_TEMPLATES.md`.

Five templates: a policy document in Typst, a business report in Typst, a
redline in Typst, a policy document in LaTeX, and a policy document in Word
through Pandoc. The gallery renders a sample or a real policy. The brand page
(admin to save) sets the colours, fonts and names the templates use. Where no
engine is installed, a render returns a zip of the sources and a build script.
PDF output can target an archival standard.

---

## Admin

### Settings (`/settings`, admin)

- **Wintermute:** server URL, client token, backend, model and agent, with the
  lists fetched from that server and a test button.
- **Crisis Exercise agent** and whether to send the open exercise with each
  question. **Policy Studio agent** and whether to send the document with
  every question.
- **Policy Studio:** whether guest links may be created, and the default
  invitation length.

The token is stored encrypted and is never sent back to the browser; the page
shows where the value in use came from (stored or environment) and its last
four characters. If the master key cannot be loaded, saving a credential is
disabled and the environment fallback still works.

### User Management (`/admin/user-management`, admin, not in local mode)

Create, edit and delete sign-in accounts: password, admin flag, and the pages
a non-admin may open. In the shipped single-user mode a second account is
refused. `scripts/manage-user.sh` does the same offline.

### Utilities (`/utilities`, admin)

- **Export / Import** the whole data set as one JSON snapshot. Import is a
  full replace in one transaction.
- **Backup:** download a consistent copy of the SQLite file.
- **Integrity check:** SQLite's `integrity_check` and `foreign_key_check`.
- **Export for the AI agent:** the knowledge bundle, for uploading into an
  agent's library.

### Help (`/help`), API Docs (`/docs`), Home (`/`)

The help page explains each workflow and lists every page. API Docs renders a
hand-maintained OpenAPI description (`/openapi.json`) that covers the main
routes, not all 278. The home page lists pages and endpoints.

### Knowledge API (`/api/knowledge/*`)

Read-only access for an AI agent to the NFRs, controls, regulation reports and
clauses, policies and policy clauses, risks, and exercises and their findings.
Overview, a whole-catalog index, lexical search that returns both "any term"
and "every term" totals, one item by reference, and a full export. Registered
only with `KNOWLEDGE_TOKEN` set, or in local mode. Detail in `AI_AGENT.md`.
