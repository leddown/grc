# Policy Authoring & Document Template Framework

Research and design proposal for two new modules:

- **`internal/policydocs`** — authoring ISMS / ISO 27001 / NIST / FedRAMP /
  PCI DSS policy, procedure and work-instruction documents for client
  engagements, with AI assistance and a corpus of uploaded reference documents.
- **`internal/doctemplate`** — a separate template registry that turns
  structured document content into professional PDF/DOCX output, and provides a
  migration path for existing LaTeX templates.

Phases 1, 2 and 5 are implemented -- `internal/policydocs` for authoring, and
`internal/doctemplate` for the template registry and rendering (see
[`DOCUMENT_TEMPLATES.md`](DOCUMENT_TEMPLATES.md)). Phases 3 and 4 -- the
uploaded-document corpus and AI drafting -- remain design only. The phasing in
§4.4 records what is done.

---

## Part 1 — What the research says about writing policy documents

### 1.1 The hierarchy is the product

Every source converges on the same four-tier structure, and it is the single
most important thing to get right because it determines the data model:

```
Policy        WHAT and WHY.  Board/management intent. Rarely changes.
  └ Standard     WHAT specifically. Mandatory, measurable, technology-bound.
      └ Procedure   HOW. Step-by-step, role-assigned, repeatable.
          └ Work Instruction  HOW exactly, for one task on one system.
Guideline     Recommended, non-mandatory. Hangs off any level.
Record        Evidence that the above was executed. Output, not authored.
```

The rule that makes this work in practice: **volatility increases as you go
down**. A policy that names a specific tool, port number, or scan frequency has
to be re-approved by management every time that detail changes. Push those into
standards and procedures, and the policy stays stable for years. This is the
most common defect in client policy sets and the thing an authoring tool can
structurally prevent — by simply not offering a field for it at the wrong tier.

Guidance is consistent that policies and standards get reviewed every 1–3 years,
but several frameworks (PCI DSS notably) require **annual** review, so review
cadence has to be a per-document field driven by the frameworks that document
is mapped to, not a global setting.
([FRSecure](https://frsecure.com/blog/differentiating-between-policies-standards-procedures-and-guidelines/),
[ComplianceForge](https://complianceforge.com/grc/policy-vs-standard-vs-control-vs-procedure),
[StandardFusion](https://www.standardfusion.com/blog/hierarchical-structure-of-compliance-documents))

### 1.2 Anatomy of a policy document

The section set below is the union of what ISO 27001, NIST 800-53 `-1` controls,
and FedRAMP all expect to see. FedRAMP is the strictest and effectively dictates
it: every family policy must address *purpose, scope, roles, responsibilities,
management commitment, coordination among organizational entities, and
compliance*, plus procedures to facilitate implementation.

| Section | Why it exists | Who supplies it |
|---|---|---|
| Document control block | Version, owner, approver, effective/review dates, classification | System |
| Purpose | The "why" — the risk being managed | AI draft |
| Scope | Systems, data, people, locations, third parties covered | **Client profile** |
| Policy statements | Normative requirements | AI draft + control text |
| Roles & responsibilities | Named roles, not named people | **Client profile** |
| Management commitment | Explicit leadership endorsement | Template boilerplate |
| Coordination among entities | Who else must be consulted | **Client profile** |
| Compliance & enforcement | Consequences, exception process | Template boilerplate |
| Exceptions | Pointer to the exception process | Link to `/exceptions` |
| Related documents | Downward links to standards/procedures | System |
| Control mapping | Which framework controls this satisfies | **Control database** |
| Revision history | Every approved version | System |

The "who supplies it" column is the important one. Roughly half of a policy is
organizational fact that a model cannot know and **must not guess** — see §2.2.

### 1.3 Normative language

Policy statements need a controlled vocabulary, RFC 2119 style, applied
consistently:

- **must / shall** — mandatory, auditable, no exceptions without a formal waiver
- **should** — expected; deviation requires documented justification
- **may** — permitted, discretionary
- Never "will", "is encouraged to", "strives to", "endeavours to" — an assessor
  cannot test these, and they are the fastest way to fail an audit on a policy
  that otherwise reads well.

This is mechanically checkable and belongs in the module as a linter, not as
advice in a help page.

### 1.4 What each framework specifically demands

**ISO 27001:2022 (clause 7.5, documented information).** The standard requires
documents be created, reviewed, approved, versioned, access-controlled, and
retained/disposed per defined rules — distribution, access, storage,
preservation, version control and retention are all named explicitly. The 2022
revision added a requirement to **control documented information of external
origin**, which maps directly onto the "upload a group of documents" feature:
uploaded client documents and third-party standards are external-origin
documented information and need the same identification and control as
authored ones.
([ISMS.online](https://www.isms.online/iso-27001/requirements-2022/7-5-documented-information-2022/),
[URM](https://www.urmconsulting.com/blog/iso-27001-clause-7-5-documented-information-explained))

**NIST SP 800-53 Rev 5.** Every control family has an `XX-1` "Policy and
Procedures" control, present in low, moderate and high baselines. Much of the
control text contains **organization-defined parameters (ODPs)** — the variable
part an organization instantiates during tailoring. A policy generator that
leaves ODPs unfilled produces something unusable; one that invents values
produces something dangerous. ODPs must be a first-class, per-client, typed
field.
([NIST SP 800-53r5](https://nvlpubs.nist.gov/nistpubs/specialpublications/NIST.SP.800-53r5.pdf),
[NIST glossary](https://csrc.nist.gov/glossary/term/organization_defined_control_parameter))

**FedRAMP Rev 5 / CR26.** 1014 active controls and enhancements across 20
families; a policy **and** supporting procedures are required per family for
Low/Moderate/High. The SSP plus all 17 appendices is mandatory. Final CR26
versions become mandatory in **January 2027**.
([FedRAMP Rev5 control guidance](https://www.fedramp.gov/2026/reference/rev5-control-guidance/),
[Secureframe](https://secureframe.com/hub/fedramp/compliance-requirements))

**OSCAL — the deadline that should shape the data model.** FedRAMP RFC-0024
requires machine-readable authorization packages, and **from 30 September 2026
every CSP must submit machine-readable authorization data** to maintain
certification. NIST CSWP 53 sets OSCAL's direction beyond that. The practical
consequence for this module: **author to structured content, render documents as
an output format.** If documents are authored as prose blobs, OSCAL export is a
rewrite; if they are authored as structured sections with control references,
OSCAL is a serializer.
([NIST OSCAL](https://pages.nist.gov/OSCAL/),
[FedRAMP RFC-0024 / Paramify](https://www.paramify.com/blog/oscal-ssp),
[CSWP 53](https://csrc.nist.gov/pubs/cswp/53/charting-the-course-for-nist-oscal/ipd))

**PCI DSS v4.0.1.** Requirement 12 is the policy requirement, and assessment is
a two-step validation: the QSA checks that written policy mandates the
requirement, *then* tests whether daily operations match the written word. v4
also introduces the **Targeted Risk Analysis (TRA)** — a documented analysis
per requirement, in two flavours (one setting the frequency of a control, one
supporting a customized-approach implementation). TRAs are a distinct document
type with their own review cycle, not a section inside a policy.
([SecurityMetrics](https://www.securitymetrics.com/blog/pci-dss-requirement-12-policies-and-documentation),
[PCI SSC blog](https://blog.pcisecuritystandards.org/just-published-pci-dss-v4-x-targeted-risk-analysis-guidance))

### 1.5 The crosswalk problem

A single client policy — say Access Control — simultaneously satisfies ISO 27001
Annex A controls, NIST `AC-1`, FedRAMP's AC family policy requirement, and PCI
DSS Req 7/8/12 clauses. Authoring one document per framework produces four
diverging documents that drift apart within a year.

**Author once, map many.** A document section carries a set of control
references; framework coverage is a query, not a copy. This also gives the
coverage report for free ("which required controls have no policy text?"), which
is the artifact consultants actually get paid for.

This repo is already well positioned here — `rcsa_controls` plus
`security_nfr_control_links` is exactly the mapping substrate, and the same join
later drives the assessment output.

---

## Part 2 — AI-assisted drafting: what works, what to guard against

### 2.1 The consensus position

The GRC literature is unanimous and blunt: AI drafts, humans approve. Named
risks are hallucinated justifications, incorrect control mappings, weak evidence
validation, inconsistent framework interpretation, and unclear ownership.
"Disclaimers alone are not enough" — the working pattern is curated libraries,
enforced citations, and reviewer workflows.
([SureCloud](https://www.surecloud.com/whitepaper/ai-in-grc-promise-pitfalls-and-a-practical-path-forward),
[CSO Online](https://www.csoonline.com/article/4143444/9-ways-cisos-can-combat-ai-hallucinations.html),
[Anecdotes](https://www.anecdotes.ai/learn/ai-in-grc-real-life-applications-pros-cons-and-best-practices))

For a consultancy this is not just quality control — it is liability. A
hallucinated control mapping in a client's ISMS that survives to an audit is a
billable-work failure with the consultancy's name on the cover page.

### 2.2 The rule that should be structural, not advisory

> **The model may draft language. The model may not invent facts about the
> client.**

Scope, role names, retention periods, review frequencies, ODP values, system
boundaries, approver names, third parties — these are all client facts. Every
one of them should come from a **client profile** record, and a draft that
references a profile field with no value must render a visible
`[[UNRESOLVED: retention_period]]` token that **blocks approval**, rather than
letting the model produce a plausible "90 days".

This one decision removes the majority of the hallucination surface, because
what remains for the model is prose composition — the thing it is actually good
at.

### 2.3 Provenance on every generated block

Each authored block should record where it came from: `human`, `template`,
`ai:<model>` with the prompt hash, or `imported:<document_id>` with the source
chunk. Rationale:

- Reviewers can see at a glance what needs the hardest look.
- "Which parts of this deliverable were AI-generated?" is increasingly a
  contractual question from clients.
- When a model version changes, you can find and re-review everything it wrote.

### 2.4 Retrieval over the uploaded corpus

For the "upload previously created policies, procedures and work instructions"
feature, the research points at **section-aware chunking** for policy documents
specifically — boundaries aligned to headings/page breaks, each chunk carrying
source document id, page range and section title as metadata, rather than a
fixed-size split. Recursive splitting at ~400–512 tokens with 10–20% overlap is
the general-purpose fallback; chunking should be tuned per document type rather
than set globally.
([Firecrawl](https://www.firecrawl.dev/blog/best-chunking-strategies-rag),
[Digital Applied](https://www.digitalapplied.com/blog/rag-chunking-strategies-2026-retrieval-quality-playbook),
[Citation-enforced RAG, arXiv](https://arxiv.org/pdf/2603.14170))

> **Implemented for the NFR catalog.** `internal/nfrenrich` applies this
> section to the Security NFR catalog rather than to policy documents: it
> ingests uploaded and fetched security documents, chunks them section-aware,
> retrieves with BM25, and proposes enrichments to an NFR field that a human
> accepts or rejects. §2.2's "may not invent facts" is enforced by requiring a
> verbatim quote from a supplied excerpt on every citation, and §2.3's
> provenance by storing the model and prompt hash on every proposal. It scores
> in Go rather than through FTS5 because FTS5 is a build tag here — see that
> package's doc comment. The policy-document corpus described below is still
> to come and should reuse the same package.

**Start with lexical search, not embeddings.** SQLite FTS5 with BM25 over
section-aware chunks is available in-process (`-tags fts5` on the existing
`mattn/go-sqlite3`, which already builds with `CGO_ENABLED=1`), needs no new
service, no embedding API spend, and no vector store. Compliance retrieval is
unusually keyword-friendly — users search for "AC-2", "cardholder data",
"clause 7.5". Add embeddings later behind the same interface if recall proves
insufficient; do not start there.
([go-sqlite3 FTS5](https://github.com/mattn/go-sqlite3/issues/340))

### 2.5 Reuse the existing AI plumbing

`internal/app/ai_chat.go` already has provider selection (Anthropic Claude, or
a self-hosted Wintermute server that routes the turn to a local model or on to
Claude), key handling and `ai_usage_log` cost tracking. The policy module
should call through that, not open a second path — one place for keys, one
place for spend.

The Wintermute path is worth considering for the bulk work specifically:
retrieval reranking and section classification over a policy corpus send a lot
of text somewhere, and routing that to a model on your own network keeps it
there while leaving the frontier model for drafting.

Its default model string is now `claude-opus-5`, the current Claude generation
(alongside `claude-sonnet-5`, `claude-fable-5` and Haiku 4.5). That default
suits long-form policy drafting, which is the work this module actually needs a
frontier model for. Retrieval reranking and section classification do not — the
model field is per request, so point those at a cheaper model or at a
Wintermute backend rather than paying Opus rates to rank chunks.

---

## Part 3 — Document templating

### 3.1 Separate content from presentation, permanently

The template module exists so that content lives in the database as structure
and presentation lives in a template. Consequences worth stating up front: one
document renders to client-branded PDF, plain DOCX for a client who wants to
edit it, and OSCAL JSON, with no content duplication.

### 3.2 Engine choice

| Engine | For | Against |
|---|---|---|
| **Typst** | Single static binary, Apache 2.0 (embeddable in a product with no licensing question), ~200–500ms compiles vs multiple seconds, **native JSON/CSV/XML data loading**, PDF/UA-1 accessible output as a target | Smaller ecosystem, newer |
| **LaTeX** | Ubiquitous, client templates already exist in it | >1GB distribution, slow, designed for hand-edited manuscripts not automated generation; programmatic generation means emitting source with error-prone scripts or LuaTeX |
| **Pandoc** | `--reference-doc` gives styled DOCX from a sample Word file — by far the cheapest route to "professional Word output" | Another external binary; less layout control than a real typesetter |

([Typst automated generation](https://typst.app/blog/2025/automated-generation/),
[Typst vs LaTeX 2026](https://www.typetex.app/comparisons/typst-vs-latex),
[Pandoc manual](https://pandoc.org/MANUAL.html))

**Recommendation: Typst as the primary engine, Pandoc for DOCX.** The
decisive factors for this repo are that Typst loads JSON natively — so the
renderer's job reduces to "write the document as JSON, invoke the template",
with the control database's existing JSON export usable directly — and that a
single static binary survives the goreleaser cross-build story in a way a 1GB
TeX distribution does not.

### 3.3 On "translate LaTeX into professional documents"

Two different things are worth separating, because they need different work:

1. **Rendering with an existing LaTeX template** — keep the template as-is,
   feed it variables, shell out to `latexmk`. Cheap, but inherits every LaTeX
   deployment problem, and the app then needs TeX on the host.
2. **Migrating a LaTeX template to Typst** — a one-time, per-template
   conversion. Typst publishes a LaTeX-user guide with command equivalences,
   and this is a task an AI assist genuinely suits: paste LaTeX, get Typst,
   render both, compare. Worth building as a tool *inside* the template module.

Recommendation: build (2) as the primary path with a rendered side-by-side
diff for verification, and treat (1) as an optional escape hatch gated on
whether `latexmk` is present on the host.

### 3.4 Deployment consequence — flag this now

The repo currently ships self-contained binaries via goreleaser for
linux/windows/darwin amd64, with no Docker path. Typst and Pandoc are external
binaries. Options, in order of preference:

1. **Discover at runtime**, degrade gracefully: HTML and Markdown export always
   work in-process; PDF/DOCX export is offered only when the binary is found.
   Keeps the single-binary story intact.
2. Ship them alongside in the release archive (bigger artifacts, license
   notices needed).
3. Introduce a container image for the render path only.

Option 1 is the right default and should be decided before any renderer code
gets written, because it dictates the interface.

---

## Part 4 — Proposed architecture

### 4.1 Packages

```
internal/policydocs    documents, sections, versions, approvals, control mapping
internal/policycorpus  upload, extract, section-aware chunk, FTS5 index, retrieval
internal/clientprofile per-client facts and ODP values (the anti-hallucination store)
internal/doctemplate   template registry, render pipeline, brand settings
```

Splitting the corpus out of `policydocs` matters: ingestion is the part most
likely to grow (more file types, better extraction), and it has no business
being entangled with authoring.

`clientprofile` keys off the existing `crm_clients` table, so "who is this
policy for" is already answered by the CRM module rather than being a second
notion of customer.

### 4.2 Schema sketch

Following the repo's dual `internal/db/sqlite.go` + `postgres.go` pattern:

```sql
policy_documents        id, client_id→crm_clients, doc_type (policy|standard|
                        procedure|work_instruction|guideline|tra), title,
                        status (draft|in_review|approved|retired),
                        owner_role, approver, classification,
                        effective_date, review_cadence_months, next_review_date,
                        parent_document_id, created_at, updated_at

policy_sections         id, document_id, ordinal, heading, body_markdown,
                        section_kind (purpose|scope|statements|roles|…),
                        provenance (human|template|ai|imported),
                        provenance_detail, updated_at

policy_section_controls section_id, control_id→rcsa_controls, framework,
                        coverage (full|partial|supporting)

policy_versions         id, document_id, version_label, rendered_snapshot,
                        approved_by, approved_at, change_summary

client_profile_facts    client_id, key, value, value_type, source, updated_at
                        -- scope, role names, ODPs, retention periods

corpus_documents        id, client_id, filename, mime, sha256, uploaded_by,
                        uploaded_at, origin (client|external_standard|internal),
                        ai_shareable INTEGER NOT NULL DEFAULT 0
corpus_chunks           id, corpus_document_id, section_path, page_from, page_to,
                        text
corpus_chunks_fts       FTS5 virtual table over corpus_chunks.text

doc_templates           id, name, engine (typst|latex|pandoc|html),
                        source, variable_schema_json, created_at, updated_at
```

`policy_section_controls` is the join that makes the whole thing pay off: it
drives the coverage matrix, the framework crosswalk, the OSCAL export, and later
the assessment generation the user described — all from one table.

### 4.3 Routes, matching existing conventions

```
/policies                      list / filter by client, framework, status
/policies/manage               editor  (mirrors /controls/manage)
/policies/:id                  document detail
/policies/coverage             framework coverage matrix from the control DB
/policy-corpus                 upload + browse uploaded reference documents
/policy-corpus/search          FTS5 retrieval, used by both UI and AI drafting
/templates                     template registry + render form
/templates/manage              brand editor
/templates/render              render → PDF, or a source bundle with no engine
```

As built, rendering lives under `/templates` rather than at
`/policies/:id/render`: one endpoint serves a stored document, a bundled sample
and (later) a corpus payload, and putting it under the policy module would have
meant that module owning a pipeline it does not use.

All added to `pageui.primaryTabs` / `homeTabs` and to the `allowed_pages` list
in `access_control.go` and `user_management.go`.

### 4.4 Suggested phasing

Each phase is independently useful, which matters given how large the whole
thing is.

1. ~~**Document model + editor + document control.**~~ **Done.** Hierarchy,
   sections, versioning, approval, review dates, Markdown/HTML export.
2. ~~**Control mapping + coverage matrix.**~~ **Done.** `policy_section_controls`
   wired to `rcsa_controls`, with the coverage report at `/policies/coverage`.
   Two decisions worth recording, because both cost something:
   - **Mapping is a draft-only edit**, like section text. A coverage claim is a
     compliance assertion, so changing what a policy claims to satisfy costs a
     revision cycle. The approval snapshot records the mappings alongside the
     text, which is what makes that coherent rather than merely strict.
   - **The report splits "approved" from "draft only" and "supporting only".**
     Both of the latter read as covered in a spreadsheet and neither survives an
     assessor asking which approved document says so. A single covered/uncovered
     count would have hidden exactly the gap the report exists to find.
3. **Corpus upload + FTS5 retrieval.** Ingest, extract, section-aware chunk,
   search. Still no AI — searching a client's existing document set is
   independently valuable, and it de-risks phase 4 by proving retrieval quality
   first.
4. **AI drafting**, through the existing `ai_chat.go` provider layer, with
   citation enforcement, client-profile fact resolution, unresolved-token
   blocking, and provenance recording.
5. ~~**Template module + Typst rendering**, then LaTeX conversion.~~ **Done.**
   `internal/doctemplate`: a three-entry registry, runtime engine detection,
   per-install brand settings and a sandboxed render pipeline, at `/templates`
   and `/templates/manage`. Three decisions worth recording:
   - **A missing typesetter returns the sources, not an error.** The app is one
     Go binary and the engines are separate installs, so "no engine" is an
     ordinary state. Treating it as a failure would have made the feature dead
     on most hosts, including every developer laptop without typst.
   - **The module does not import `internal/policydocs`.** The payload arrives
     as JSON matching the published contract, and `app.go` adapts one to the
     other. Ingestion from the phase-3 corpus then needs no change here.
   - **Brand values are validated as executable input.** They are interpolated
     into Typst and LaTeX source that a typesetter runs, so colours, font names,
     sizes and paper come from patterns and allowlists rather than being escaped
     and hoped for.
   The **LaTeX→Typst conversion** the original phase named was not built and
   should not be: the LaTeX template is generated from the payload instead, so
   the two engines render the same document from the same data and neither has
   to be converted into the other.
6. **OSCAL export.** Structural, if 1–2 are done right.

### 4.5 Open decisions I'd want settled before writing code

- Does policy content need to survive the SQLite↔Postgres sync path in
  `internal/dbsync`? Documents are much larger than anything currently synced.
- ~~Are uploaded client documents allowed to leave the host?~~ **Decided:**
  per-document, set at upload time via an `ai_shareable` flag. Today's uploads
  are the consultancy's own material and are shareable, but the flag exists from
  the first version so third-party client material can be ingested later without
  a schema change. Two design rules that follow from it, both worth fixing now
  rather than discovering later:
  - **Default off.** The upload checkbox starts unchecked, so forgetting to
    think about it fails closed. A shareable default means one mis-click sends
    client-confidential material to a hosted model API, which is not
    recoverable.
  - **Enforce at retrieval, not in the UI.** `ai_shareable = 0` must exclude a
    document's chunks from the query that builds AI context, so a non-shareable
    document can still be searched and read by a human in the app while being
    structurally unable to reach a model. A checkbox that only greys out a
    button is not a control.
- Retention and deletion for uploaded client material. This is client
  confidential data in a way nothing currently in the app is.
- ~~Single-user mode conflicts with author≠approver.~~ **Resolved.** Documents
  now record an `author` (taken from the session, never from the request body),
  and `Service.SetRequireSeparateApprover` refuses a self-approval. It is wired
  on only when multi-user authentication is active, because in single-user mode
  the author is necessarily the approver and enforcing it would make every
  document permanently unapprovable. Under authentication the approver is the
  signed-in identity rather than a free-text field -- otherwise an author could
  self-approve by typing somebody else's name, leaving both the control and the
  audit trail worthless.

---

## Sources

- [FRSecure — policies, standards, procedures, guidelines](https://frsecure.com/blog/differentiating-between-policies-standards-procedures-and-guidelines/)
- [ComplianceForge — policies vs standards vs controls vs procedures](https://complianceforge.com/grc/policy-vs-standard-vs-control-vs-procedure)
- [StandardFusion — compliance document hierarchy](https://www.standardfusion.com/blog/hierarchical-structure-of-compliance-documents)
- [ISMS.online — ISO 27001:2022 clause 7.5](https://www.isms.online/iso-27001/requirements-2022/7-5-documented-information-2022/)
- [URM Consulting — clause 7.5 explained](https://www.urmconsulting.com/blog/iso-27001-clause-7-5-documented-information-explained)
- [NIST SP 800-53 Rev 5](https://nvlpubs.nist.gov/nistpubs/specialpublications/NIST.SP.800-53r5.pdf)
- [NIST glossary — organization-defined control parameter](https://csrc.nist.gov/glossary/term/organization_defined_control_parameter)
- [FedRAMP — Rev5 control guidance (CR26)](https://www.fedramp.gov/2026/reference/rev5-control-guidance/)
- [Secureframe — FedRAMP requirements in 2026](https://secureframe.com/hub/fedramp/compliance-requirements)
- [NIST OSCAL project](https://pages.nist.gov/OSCAL/)
- [NIST CSWP 53 — Charting the Course for NIST OSCAL](https://csrc.nist.gov/pubs/cswp/53/charting-the-course-for-nist-oscal/ipd)
- [Paramify — FedRAMP RFC-0024 machine-readable SSPs](https://www.paramify.com/blog/oscal-ssp)
- [SecurityMetrics — PCI DSS Requirement 12](https://www.securitymetrics.com/blog/pci-dss-requirement-12-policies-and-documentation)
- [PCI SSC — Targeted Risk Analysis guidance](https://blog.pcisecuritystandards.org/just-published-pci-dss-v4-x-targeted-risk-analysis-guidance)
- [SureCloud — AI in GRC: promise, pitfalls, practical path](https://www.surecloud.com/whitepaper/ai-in-grc-promise-pitfalls-and-a-practical-path-forward)
- [CSO Online — 9 ways CISOs can combat AI hallucinations](https://www.csoonline.com/article/4143444/9-ways-cisos-can-combat-ai-hallucinations.html)
- [Anecdotes — AI in GRC best practices](https://www.anecdotes.ai/learn/ai-in-grc-real-life-applications-pros-cons-and-best-practices)
- [Firecrawl — best chunking strategies for RAG](https://www.firecrawl.dev/blog/best-chunking-strategies-rag)
- [Digital Applied — RAG chunking playbook 2026](https://www.digitalapplied.com/blog/rag-chunking-strategies-2026-retrieval-quality-playbook)
- [Citation-Enforced RAG for compliance documents (arXiv)](https://arxiv.org/pdf/2603.14170)
- [mattn/go-sqlite3 — enabling FTS5](https://github.com/mattn/go-sqlite3/issues/340)
- [Typst — automated PDF generation](https://typst.app/blog/2025/automated-generation/)
- [TypeTeX — Typst vs LaTeX 2026](https://www.typetex.app/comparisons/typst-vs-latex)
- [Typst — guide for LaTeX users](https://typst.app/docs/guides/for-latex-users/)
- [Pandoc user's guide](https://pandoc.org/MANUAL.html)
- [lu4p/cat — pure Go text extraction from docx/odt/rtf](https://github.com/lu4p/cat)
