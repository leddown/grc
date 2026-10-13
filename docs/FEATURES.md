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
  (`PUT /security-nfrs/:key/weight`), and the form has a Weight field. The
  Exceptions page and the Exceptions Workbook compute with the weight.
- **Reseeding.** When the embedded NFR file changes, the catalog is replaced
  from it and rows no longer in the file are deleted. A weight set in the
  application survives unless the file carries its own.

An NFR has a `record_key` (the stable identity), an NFR id such as `1.4`, a
summary, an issue type, a domain, description, additional details,
implementation, and its NIST mapping as free text.

### NFR-Control Links (`/security-nfrs/links`)

Shows the links both ways, switched by two buttons. Code: `internal/nfrlink`.

- **By NFR**: how each NFR's NIST mapping resolved, one row for each token.
  "Show unmatched only" keeps the rows whose token matched no control.
- **Unlinked Controls**: the controls that no NFR is linked to, with their
  name, family and type, each a link to its detail page. A control is linked
  when a mapping of some NFR matched it, which is what the Exceptions page
  counts, so the list is what no exception can reach. It is worked out in the
  browser from the link rows and the control list the Controls page reads, so
  a control in a hidden family is not in it. With the seed, 1,090 of the
  1,193 controls are unlinked.

The search box searches the view that is shown.

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

The page has two steps, switched by two buttons above the catalog list:

1. **Scope.** Tick the Security NFRs the exercise covers. The page opens with
   the whole catalog in scope, and a line beside the buttons reads
   `Scope: 40 of 109 Security NFRs`.
2. **Exceptions.** The list holds the NFRs in scope only; tick the ones the
   system does not meet. Taking an NFR out of scope takes it out of the
   exception set. Each selected NFR has a Full / Partial choice beside it:
   full when the NFR is not met at all, partial when it is met in part.

Each ticked NFR of step 2 has a Full / Partial choice beside its checkbox.
It is the same setting as the choice on that NFR's card in the Selected
Security NFRs panel: changing either changes the other.

A partial exception can be described. Setting an exception to Partial opens a
description box on its card in the Selected Security NFRs panel, for what is
met and what is not, up to 1,000 characters. Set back to Full, the box closes
and keeps its text for as long as the NFR stays an exception; an NFR taken out
of the exception set loses it. The descriptions are kept in the browser for
the tab's session, beside the exercise. They are not in the address and not in
the scope file, and the application does not store them: it is sent them only
with a request for the Exceptions Workbook, where each is the Comments cell of
its NFR.

The page builds the exception set and does not report on it. The figures of
the set are on the Exceptions Report ([Exceptions Report](#exceptions-report-reportsexceptions-report)), which opens from the menu with the
scope and the exceptions of the browser tab: the weight that touches each
rating and each threat, the counts of controls, and, as a minor figure, the
total weight. An NFR *touches* a rating or a threat when a control it is
linked to carries it, and then counts its whole weight once; a partial
exception counts half. Each NFR is listed here with its weight.

A linked control's card shows the letters of the ratings it carries, `C`, `I`
or `A`, and leaves out the ones it does not.

"Select All" and "Clear Filtered" act on the current search and domain
filter, in the step the list is on. The detail page shows the scope line, the
same three weight lines and each selected NFR in full with its weight,
whether its exception is full or partial, and its linked controls, and
prints.

Neither the scope nor the selection is stored. Both travel in the address of
the page and of the detail page: a `scope` parameter for each NFR in scope and
a `key` parameter for each selected one, with a `partial` parameter as well
for each selected one excepted in part. No `scope` parameter means the whole
catalog is in scope, and an empty one (`?scope=`) means nothing is. A `key`
outside the scope is ignored, and so is a `partial` that is not a `key`. The address follows the page as it changes, so
a reload or a bookmark returns to the same exercise.

**Scope file.** In step 1, "Export Scope" saves the scope as a local JSON
file (`security-nfr-scope-YYYY-MM-DD.json`) and "Import Scope" reads one back
and makes it the scope, replacing the one on the page. Both happen in the
browser: nothing is sent to the application and nothing is stored in it. The
file lists every Security NFR of the catalog with `"in_scope": true` or
`false`, and its id and summary for whoever reads it; the key and that mark
are what is read back. It holds no exceptions: an import leaves the exception
set alone, but for the NFRs that are no longer in scope.

Import Scope reads a file only if it is what Export Scope of this application
writes, unchanged. Before the file is read: its name ends in `.json`, it is
not empty and not over 5 MB, and its declared type, if it has one, is
`application/json`. Then, in this order, and stopping at the first that fails:

- it is JSON, an object whose `format` is `security-nfr-scope`;
- its `version` is the whole number 2;
- it has the fields `format`, `version`, `application`, `exported_at`, `scope`
  and `checksum`, and no other;
- `application` is this application's name: a file exported from another
  application is refused;
- `exported_at` is a time as the export writes it;
- `checksum` is `sha256:` and 64 hexadecimal digits;
- `scope` is a list of at most 10,000 entries, each an object with `key`,
  `id`, `summary` and `in_scope` and no other field: a key of 1 to 256
  characters with no space at either end and no control character, an id and
  a summary that are text, and `in_scope` that is `true` or `false`;
- no key is named twice;
- the checksum is the SHA-256 of the content, so a file edited after it was
  exported is refused;
- every key is a Security NFR of the catalog.

A refused file changes nothing on the page, and the message says which check
failed without showing anything from the file. An NFR the catalog has and the
file does not list is out of scope, and the message counts those. Export
Scope reads back what it is about to save and saves nothing it would refuse.

The checksum shows that a file was changed after export. It is not a
signature: it is worked out in the page, so someone who reads the page can
work out a checksum for a file of their own. Proof of who wrote a file would
need a secret kept by the application and a route to sign with, and there is
neither.

**The session's exercise.** The scope and the exceptions on the page are kept
in the browser for the tab's session (`sessionStorage`), in the form the
address carries them. Opening `/exceptions` with a plain address comes back
to them, and the Exceptions Workbook page offers a workbook with them.
Putting the whole catalog back in scope and clearing the selection forgets
them. The application is not told and stores nothing.

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
  formulas that reproduce the page: choosing `Yes` in the Scope column (A)
  puts an NFR in the scope of the exercise, choosing `Full` or `Partial` in
  the Exception column (B) puts an NFR of the scope in the exception set, and
  the linked controls, the C / I / A counts, the threat coverage and the
  detail sheets follow. An NFR outside the scope is not counted, whatever its
  Exception cell holds.
- The Exceptions sheet shows Scope, Exception, Summary, Domain,
  Weight, Linked Controls, NIST Mapping, Comments, Additional Details and
  Implementation, and nothing after it: the NFR's key and the other working
  columns are there, hidden, because every figure is worked out from them and
  a row has to keep its own when the sheet is sorted. Above the header are
  two live lines, how many NFRs are in scope and how many are excepted; the
  figures themselves are on the Summary sheet. Comments holds the NFR's description from the catalog or, for a partial
  exception described on the Exceptions page, that description. The How to
  use sheet asks for a description there of every partial exception.
- The page has two downloads of one workbook. The first has nothing selected in
  it: the Scope and Exception columns are empty. The second, shown when a scope or exceptions
  are set on the Exceptions page in the same browser tab, opens with them: it
  is the same download with the tab's `scope`, `key` and `partial` parameters.
  What was typed on the Exceptions page of the partial exceptions goes with
  it, in the body of the request and not in its address.
- The Links and Controls sheets are locked, without a password: they hold
  what the application exported, and a link or a rating is changed in the
  application. Their formulas still calculate and the filter on Controls
  still works. The other sheets are open.
- The weight lines are the page's, with the same sums, the same rounding and
  the same wording: the weight of the selection, the weight of the scope, the
  share, the average, and a count at each weight, measured against the NFRs
  in scope. A partial exception counts half its NFR's weight, and in every
  other figure counts as any exception does.
- Seven sheets: How to use, Exceptions, Summary, Selected NFRs, Selected
  controls, Links, Controls.
- Formulas use only functions Excel 2010 and LibreOffice share. Each is stored
  with its result, so a preview shows numbers before recalculating.
- `?scope=`, `?key=` and `?partial=` on the download open the workbook with
  that scope and those NFRs selected, read as the page's own address is. With
  none of the three the workbook has nothing selected; with `key` and no
  `scope` the whole catalog is in scope, as on the page.
- A `POST` to the download is the same request with the descriptions of the
  partial exceptions in its body: a form (`application/x-www-form-urlencoded`)
  with one `description.<key>` field for each. A description is at most 1,000
  characters and is written as text, never as a formula; control characters
  other than a line break and a tab are dropped. A body with any other field,
  a field given twice, a description that is too long or is not UTF-8, or a
  body over 2 MB is refused with `400` and no workbook. A description for a
  key that is not a partial exception of the request is left out. Nothing of
  the request is stored.

The page's logic is JavaScript; the workbook restates it in Go and in
formulas. A change to one needs the other. A test has LibreOffice recalculate
the workbook and compares every cell.

### Exceptions Report (`/reports/exceptions-report`)

The Summary sheet of the Exceptions Workbook, on a page: five tables for a
scope and an exception set. Code: `internal/app/exceptions_report.go`, over
`Result.Summary` in `internal/exceptionbook`.

- **Scope and selection**: the Security NFRs in the catalog, in scope and
  selected, and the linked controls.
- **CIA coverage**: for each rating, the excepted weight that touches it, the
  scope's weight that does, the share, and how many of the linked controls
  carry it.
- **Threat coverage**: the same four figures for every threat in the catalog,
  zero included.
- **Total weight (minor)**: the weight of the selection, the weight of the
  scope, the share as a whole percentage, and the average weight of a
  selected NFR. Shown smaller, as the minor figure it is.
- **Weight**: for each weight from 1 to 5, the NFRs in the scope and in the
  selection. Minor as well.

The figures are worked out on the server by the code that writes the sheet
(`exceptionbook.Evaluate`), so the page and the sheet cannot differ; a test
holds the two together line for line. The sheet's working rows are left out.

The scope and the exception set are read from the address as the Exceptions
page writes them (`scope`, `key`, `partial`). Opened from the menu, the page
goes to the exercise of the browser tab if there is one; without one it
reports on the whole catalog with nothing selected. Like the Exceptions page,
it needs a signed-in user and stores nothing.

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
