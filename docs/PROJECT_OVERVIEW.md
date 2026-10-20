# grc: project overview

Start here. This file says what the application is, what it contains, and
which document answers which question. It is written for a reader who has no
access to the code, including an AI agent asked to analyse the project.

Everything in `docs/` was written or checked against the code on 2026-10-10
(commit `99c8e53` plus the uncommitted Exceptions Workbook). Where a figure can
drift, the document says how it was obtained.

## What it is

`grc` is an internal web application for a security consulting practice doing
governance, risk and compliance work for clients, chiefly financial entities
in the EU. One Go binary serves it. It holds:

- two **catalogs**: NIST SP 800-53 Rev 5 controls (1,193 records) and the
  practice's own Security NFRs (non-functional requirements, 109 records), with
  each NFR linked to the controls its NIST mapping names;
- **registers**: a security risk register and an audit findings register;
- **policy authoring**: a policy library with document control and approval, a
  collaborative editor (the Policy Studio) in which a consultant and a client
  edit the same document live, templates, client fact sheets, and rendering to
  PDF and Word;
- **analysis modules** that use an AI model: Regulation Coverage (map an EU
  regulation article by article to the catalogs), NFR Enrichment (propose
  catalog text from a security document), and Risk & Crisis Exercises (design,
  run and report a crisis exercise, including the DORA notification clocks);
- **reporting and data tools**: a linked/unlinked controls report, the
  Exceptions Workbook, JSON views, a Jira connector, backups and exports;
- an **AI chat** page and an AI dock on every page.

The application does not call a model provider itself. Every AI question goes
to a separate server the same owner runs, called **Wintermute** (`wintermuted`),
which owns the agent loop, the document library with its text extraction and
OCR, and the routing to a self-hosted model or to Claude. This application
publishes a read-only **knowledge API** that a Wintermute agent queries, so
answers are grounded in this installation's own data.

## Who uses it

| Role | What they do |
|---|---|
| Consultant (admin account) | Maintains the catalogs, writes policies, runs analyses and exercises, manages users and settings. Most write actions need an admin. |
| Colleague or client user (non-admin account) | Reads. Can be limited to named pages. Can ask the AI chat. |
| Guest (no account) | Joins one Policy Studio document through an invitation link, as editor, commenter or viewer. Off by default. |
| A Wintermute agent (machine) | Reads the knowledge API with its own read-only token. |
| Single user on a laptop | Runs in local mode: no accounts, everything open. |

The shipped default is **single-user mode**: the authentication service
refuses to create a second account. See [SECURITY.md](SECURITY.md).

## The application at a glance

The sidebar groups are the product's own map of itself.

| Group | Pages |
|---|---|
| Catalog | Controls, Control Editor, Hierarchy, Family Filters, Security NFRs, Security NFR Editor, NFR-Control Links, NFR Enrichment, Asset Types |
| Compliance & Risk | Risk Register, Risk Register Editor, Audit Findings, Exceptions, Policy Library, Policy Editor, Policy Templates, Policy Coverage, Regulation Coverage, Wiz Rules |
| Crisis Exercises | Crisis Exercises |
| Reporting & Data | Reports, Exceptions Workbook, JSON_view, Jira JSON, Jira Reports, Change Log |
| AI Chat | AI Chat |
| Documents | Templates, Template Brand |
| Admin | Help, Settings, API Docs, Utilities (and User Management, outside local mode) |

[FEATURES.md](FEATURES.md) describes each page and the rules behind it.

## Technology

| Layer | Choice |
|---|---|
| Language | Go 1.26 (toolchain go1.26.9). Module name `grc`. No cgo. |
| HTTP | Gin. One process, one listener. |
| Storage | SQLite through `modernc.org/sqlite` (pure Go), or PostgreSQL through `pgx`. Same logical schema, chosen by configuration. |
| Pages | Each page is an HTML document held as a Go string literal, with its own CSS and JavaScript. No template engine and no front-end framework. A middleware injects the theme, the sidebar, the command palette and the AI dock into every HTML response. |
| Collaborative editor | Tiptap (ProseMirror) and Yjs in the browser, built with Node into a bundle that is committed and embedded; `ygo`, a pure-Go Yjs server, in the process. Node is not needed to build or run the application. |
| AI | Wintermute over HTTP (`internal/aiprovider`). |
| PDF and Word | Headless Chrome through `chromedp` for HTML reports; Typst, LaTeX (Tectonic or latexmk) and Pandoc as external programs for policy documents. All optional at run time. |
| Spreadsheet | A small writer in the repository (`internal/xlsx`). |
| Passwords and secrets | bcrypt; AES-256-GCM for stored credentials. |
| Security gates | `gosec` and `govulncheck` run as Go tests, so `go test ./...` is the gate. |

Direct Go dependencies: gin, pgx, modernc sqlite, ygo, gorilla/websocket,
chromedp, goldmark, yaml.v3, x/crypto, x/text, and gosec and x/vuln as tools.

## Size

Measured on 2026-10-10 with `find` and `wc`, blank lines excluded.

| | |
|---|---|
| Go, not tests | 69,400 lines in 41 packages |
| Go tests | 22,100 lines, 570 test functions |
| Editor sources (JavaScript, CSS) | 2,800 lines in `web/policy-studio/src` |
| Database tables | 58 |
| HTTP routes | 278 |
| Commits | 61, from 2026-08-12 |
| Largest packages | `internal/app` (18,400 lines with tests), `internal/crisisexercise` (11,300), `internal/policystudio` (8,900), `internal/policydocs` (6,000) |

About a fifth of the non-test Go (12,900 lines in 34 files) is page markup,
CSS and JavaScript inside string literals. The theme layer adds about 2,600
more lines, mostly CSS and JavaScript.

## Repository layout

```
main.go, cmd/api/         the server entry points (identical)
cmd/userctl/              offline account tool (scripts/manage-user.sh)
internal/app/             routing, middleware, and the pages that have no package of their own
internal/<module>/        one package per module; see ARCHITECTURE.md
internal/data/            the two catalogs as embedded JSON
internal/db/              the schema, for SQLite and PostgreSQL
regmap/profiles/          framework profiles (DORA, NIS2, CRA, PCI DSS, generic) as embedded YAML
templates/                Typst and LaTeX document templates and sample payloads
web/policy-studio/        the editor's sources; built into internal/policystudio/assets
scripts/, deploy/         install, update, verify, release, database switching; systemd and nginx files
docs/                     this document set
*.md at the root          module documents, the change log, agent instructions
```

## Document map

Read in this order for a full picture. Sizes are approximate.

| Document | Answers | Lines |
|---|---|---|
| **docs/PROJECT_OVERVIEW.md** | What is this, and where is everything | 210 |
| **docs/ARCHITECTURE.md** | How the process is built: packages, startup, request path, pages, storage, AI, collaboration, rendering | 330 |
| **docs/FEATURES.md** | What each module and page does, and the rules it enforces | 500 |
| **docs/HTTP_API.md** | Every route and who can call it | 470 |
| **docs/DATA_MODEL.md** | Every table, its columns, and how it is backed up or synced | 550 |
| **docs/SECURITY.md** | Sign-in, authorisation, tokens, secrets, guests, AI data flows, and the open issues found in review | 230 |
| **docs/OPERATIONS.md** | Configuration, running, deploying, updating, backing up, releasing | 210 |
| **docs/DEVELOPMENT.md** | Conventions, tests, the security gate, and what a change has to touch | 150 |
| **docs/ANALYSIS_NOTES.md** | An assessment: strengths, risks, debt, inconsistencies, open questions | 200 |
| docs/DATABASE.md | Switching between SQLite and PostgreSQL, and syncing | 190 |

Module documents at the repository root go deeper than FEATURES.md:

| Document | Module |
|---|---|
| POLICY_STUDIO.md | Policy Studio: design record, threat model, operator guide (1,150 lines) |
| POLICY_MODULE_FRAMEWORK.md | The research behind policy authoring and AI-assisted drafting |
| DOCUMENT_TEMPLATES.md | The Typst and LaTeX templates and the JSON contract they render |
| LETTERS.md | Letters on a client's behalf (to an ECB JST): writing, AI, output, research |
| CRISIS_EXERCISE.md | Risk & Crisis Exercises |
| REGULATION_COVERAGE.md | Regulation Coverage |
| AUDIT_FINDINGS.md | Audit Findings & Remediation |
| RISK_REGISTER_FRAMEWORK.md | The risk register's model and scoring |
| JIRA_CONNECTOR.md | The Jira Cloud client |
| AI_AGENT.md | The knowledge API and the Wintermute agent |
| RUNTIME_ARGS.md | Every flag and environment variable, first-boot bootstrap, account management |
| internal/reporting/README.md | The HTML-to-PDF report package |
| Agents.md, CLAUDE.md | Instructions for coding agents working in the repository |
| CHANGELOG.md | Every change since the start, newest first (5,200 lines) |
| THIRD_PARTY_NOTICES.md | Licences of the bundled editor packages |

## Glossary

| Term | Meaning here |
|---|---|
| NFR, Security NFR | A security non-functional requirement in the practice's catalog, such as "Sessions must be protected from attacks". Identified by a `record_key`. |
| Control | A NIST SP 800-53 Rev 5 control or enhancement, such as `AC-2` or `AC-2(1)`. |
| NIST mapping | The free-text list of control ids on an NFR. The link table is derived from it. |
| Weight | A number from 1 to 5 on each NFR saying how much it counts. Default 3. |
| Exception set | A selection of NFRs that a system does not meet. The Exceptions page shows which controls, ratings and threats the selection touches. |
| C / I / A | Whether a control carries a confidentiality, integrity or availability rating. |
| Wintermute, `wintermuted` | The owner's separate AI server: agents, backends, document library. Not in this repository. |
| Agent | A profile on the Wintermute server: a document library plus the sources a question may be answered from. This application can name three: a general one, one for Crisis Exercises and one for the Policy Studio. |
| Library | An agent's documents on the Wintermute server. This application reads extracted text from it and never accepts an upload itself. |
| Knowledge API | `/api/knowledge/*`: this installation's data, read-only, for an agent. |
| Studio document | A policy edited in the Policy Studio. Held as a Yjs document and projected into section rows. |
| Projection | The server's validated copy of a Studio document in `policy_sections`. |
| Suggestion | A tracked change in the Studio that a person accepts or rejects. AI edits always arrive as suggestions. |
| Fact token | A placeholder in a policy for a fact about the client. Unresolved tokens block approval. |
| MSEL, inject | Master Scenario Events List; one scripted message delivered to exercise players. |
| DORA, NIS2, CRA | EU regulations the modules know by profile: Digital Operational Resilience Act, the Network and Information Security Directive, the Cyber Resilience Act. |
| TLP | Traffic Light Protocol marking on exercise material. |
| Local mode | `LOCAL_MODE=true`: no accounts, every page and action open. For one person on one machine. |
| Break-glass token | `ADMIN_TOKEN`: a bearer token that passes the admin gate without a session. |

## Using these documents in a Claude Project

Add to the project's knowledge, in this order of value:

1. All ten files in `docs/` (about 3,000 lines).
2. `AI_AGENT.md`, `RUNTIME_ARGS.md`, `REGULATION_COVERAGE.md`,
   `AUDIT_FINDINGS.md`, `CRISIS_EXERCISE.md`, `RISK_REGISTER_FRAMEWORK.md`,
   `JIRA_CONNECTOR.md`, `DOCUMENT_TEMPLATES.md`.
3. `POLICY_STUDIO.md` and `POLICY_MODULE_FRAMEWORK.md` when the question is
   about policy authoring. They are long.
4. The top of `CHANGELOG.md` (the most recent few hundred lines) when the
   question is about recent work. The whole file is about 350 KB and is better
   left out.

Suggested project instructions:

> You are analysing `grc`, an internal Go web application for governance, risk
> and compliance consulting. The project knowledge describes the code as it
> stood on 2026-10-10; you cannot see the code itself. Answer from the
> documents and say which one you used. When they do not settle a question,
> say so and name what would have to be checked in the code (ARCHITECTURE.md
> gives the package for each topic, HTTP_API.md the handler for each route).
> Keep three things apart: what the documents state as verified, what
> ANALYSIS_NOTES.md and SECURITY.md record as observations or open issues, and
> your own inference. Do not describe an open issue as fixed.

What the documents cannot tell an agent: anything about the Wintermute server
beyond the calls this application makes to it, the content of a live database,
and whether an issue listed as open has since been fixed.
