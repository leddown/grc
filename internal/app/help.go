package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

// helpPage is the one page that answers "what can this application do, and how
// do I do it" without reading the repository.
//
// It is written as work rather than as a feature list: the modules here are
// steps in a few long processes — a control catalog feeds the requirements
// catalog, which feeds policy, regulation coverage and the exercises that cite
// them — and a page-by-page tour leaves the reader to infer the order. So the
// processes come first and the page reference second.
//
// It is deliberately not behind the login gate (see protectedPrefixes): it
// describes the application and carries none of its data, exactly like the home
// page, and a help page an operator cannot reach until they are already inside
// helps nobody.
func helpPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Help · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.92);
      --accent: #8b3d2e;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: Georgia, "Times New Roman", serif;
      color: var(--ink);
      background:
        radial-gradient(circle at top left, rgba(139,61,46,0.12), transparent 30%),
        radial-gradient(circle at bottom right, rgba(11,93,59,0.12), transparent 28%),
        linear-gradient(135deg, #f7f3eb, #ece4d6 55%, #e4d8c4);
    }
    main {
      max-width: 1320px;
      margin: 24px auto;
      padding: 24px;
      background: var(--panel);
      border: 1px solid rgba(215,206,191,0.8);
      border-radius: 24px;
    }
    .tabs { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 20px; }
    .tab {
      padding: 10px 14px;
      border-radius: 999px;
      background: #efe6d6;
      border: 1px solid var(--line);
      color: var(--ink);
      text-decoration: none;
      font-family: Arial, sans-serif;
      font-size: 13px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    .tab.active { background: #e1d0b7; border-color: #b89d78; }
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3rem); line-height: 1; letter-spacing: -0.02em; }
    h2 {
      margin: 34px 0 12px;
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.09em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .lede { max-width: 900px; margin: 0 0 6px; font-size: 17px; line-height: 1.55; color: var(--muted); }
    p { line-height: 1.55; }
    a { color: var(--accent); }
    .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 16px;
      background: var(--panel);
      overflow: hidden;
    }
    .panel-header {
      padding: 12px 16px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.07em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .panel-body { padding: 14px 16px 18px; }
    .panel-body p { margin: 0 0 10px; }
    .purpose { color: var(--muted); }
    ol, ul { margin: 0; padding-left: 20px; }
    li { margin: 6px 0; line-height: 1.5; }
    .note {
      margin: 12px 0 0;
      padding: 10px 12px;
      border-left: 3px solid var(--line);
      background: var(--surface-strong, rgba(236,227,210,0.4));
      font-size: 14px;
      color: var(--muted);
    }
    table { width: 100%; border-collapse: collapse; font-size: 15px; }
    th, td { text-align: left; padding: 9px 10px; border-bottom: 1px solid rgba(215,206,191,0.8); vertical-align: top; }
    th { font-family: Arial, sans-serif; font-size: 11px; letter-spacing: 0.07em; text-transform: uppercase; color: var(--muted); }
    td.page { white-space: nowrap; font-family: "Courier New", monospace; font-size: 13px; }
    td.section { white-space: nowrap; color: var(--muted); font-size: 14px; }
    code { background: #efe6d6; padding: 2px 6px; border-radius: 6px; font-size: 13px; }
    .table-wrap { overflow-x: auto; }
    @media (max-width: 900px) { .grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/help") + `

    <h1>Help</h1>
    <p class="lede">This is a Risk and Control Self-Assessment workbench. It holds one catalog of
    NIST SP 800-53 controls and one catalog of Security NFRs — the requirements this organisation
    actually writes down — links them to each other, and then uses both from every other module:
    risk, exceptions, policy, regulation coverage, crisis exercises and reporting. Each section
    below is a job you might have come here to do.</p>

    <h2>Finding your way around</h2>
    <section class="panel">
      <div class="panel-body">
        <ul>
          <li>The bar across the top holds the sections — Catalog, Compliance &amp; Risk, Crisis
            Exercises, Reporting &amp; Data, AI Chat, Documents, Admin. Picking one changes the sidebar to that
            section's pages; a section with a single page links straight to it.</li>
          <li><strong>Search pages</strong> in the sidebar (or <code>Ctrl K</code>) jumps to any page
            by name.</li>
          <li><strong>Ask AI</strong> on the right edge of every page asks a question without leaving
            the page you are on. It uses whatever provider and agent Settings configures; the
            <a href="/ai-chat">AI Chat</a> page is where those are chosen per question.</li>
          <li>The theme control at the top right switches palette, and <code>Aa</code> lifts text
            brightness — a per-browser choice, not a setting for everyone.</li>
        </ul>
      </div>
    </section>

    <h2>Building and maintaining the catalogs</h2>
    <div class="grid">
      <article class="panel">
        <div class="panel-header">The NIST 800-53 control catalog</div>
        <div class="panel-body">
          <p class="purpose">Every other module cites these controls, so this is the first thing to
          get right.</p>
          <ol>
            <li>Browse and filter at <a href="/controls">Controls</a> — by family, baseline (Low,
              Moderate, High, Privacy), CIA classification and free text. Any control opens a
              detail page of its own.</li>
            <li>Edit rows, add controls and correct classifications in
              <a href="/controls/manage">Control Editor</a>.</li>
            <li>Read the catalog as family → control → enhancement in
              <a href="/controls/hierarchy">Hierarchy</a>, and export that view as RTF for a
              document.</li>
            <li>Hide the families this organisation does not use from every filter in
              <a href="/controls/family-visibility">Family Filters</a>.</li>
            <li>Export the filtered catalog as Excel or CSV from the Controls page.</li>
          </ol>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">The Security NFR catalog</div>
        <div class="panel-body">
          <p class="purpose">The requirements as this organisation states them, each carrying a NIST
          mapping.</p>
          <ol>
            <li>Browse, filter by domain and inspect any requirement at
              <a href="/security-nfrs">Security NFRs</a>; its NIST mapping is rendered as links
              into the control catalog.</li>
            <li>Add and edit requirements in
              <a href="/security-nfrs/manage">Security NFR Editor</a>.</li>
            <li>See the whole cross-reference — which requirement covers which control — at
              <a href="/security-nfrs/links">NFR-Control Links</a>. The table is derived from the
              NIST mapping text, and an admin can override a single pairing or rebuild the whole
              table.</li>
          </ol>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Growing the catalog from a document</div>
        <div class="panel-body">
          <p class="purpose">Turn a standard, runbook or vendor requirement into catalog entries
          without typing them twice.</p>
          <ol>
            <li>Upload the document to the AI agent's library on the Wintermute server, then
              import it at <a href="/nfr-enrichment">NFR Enrichment</a>. That server does the
              reading &mdash; including scans, which it OCRs.</li>
            <li>Analyse it against the existing requirements. The model proposes additions and
              improvements; it does not reassign anyone's NIST mapping.</li>
            <li>Review each proposal and accept or reject it. <strong>Nothing reaches the catalog
              until you accept it.</strong></li>
          </ol>
          <p class="note">Needs a Wintermute server and an agent, and an AI provider, configured
          in <a href="/settings">Settings</a>. Documents are never uploaded here.</p>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Scoping by asset type</div>
        <div class="panel-body">
          <p class="purpose">Answer "which controls apply to this system" before an assessment
          starts.</p>
          <ol>
            <li>Pick one of the six supported asset types (<code>TIER 0</code> through
              <code>TIER 4</code>, or <code>Other</code>) at
              <a href="/asset-types">Asset Types</a>.</li>
            <li>The page resolves the applicable baseline and returns the matching controls in a
              side-by-side review layout.</li>
          </ol>
        </div>
      </article>
    </div>

    <h2>Compliance and risk work</h2>
    <div class="grid">
      <article class="panel">
        <div class="panel-header">The risk register</div>
        <div class="panel-body">
          <p class="purpose">The standing record of security risk, with the same controls behind
          it.</p>
          <ol>
            <li>Read the register and its dashboard at
              <a href="/risk-register">Risk Register</a>.</li>
            <li>Create, update and retire entries in
              <a href="/risk-register/manage">Risk Register Editor</a> (admin).</li>
          </ol>
          <p class="note">The structure and the scoring conventions are documented in
          <a href="/knowledge/risk-register">RISK_REGISTER_FRAMEWORK.md</a>.</p>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Exceptions</div>
        <div class="panel-body">
          <p class="purpose">Record what a system does not meet, as a set rather than as scattered
          notes.</p>
          <ol>
            <li>Search and filter the requirements at <a href="/exceptions">Exceptions</a> and
              select the ones the exception covers.</li>
            <li>Open the detail view for the set: every selected requirement with its linked
              controls, their CIA classification and threat mapping — a printable page for the
              approval record.</li>
          </ol>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Policy: draft to approved</div>
        <div class="panel-body">
          <p class="purpose">Policy text that cites the controls it implements, with a review state
          rather than a folder of drafts.</p>
          <ol>
            <li>Browse what exists at <a href="/policies">Policy Library</a>.</li>
            <li>Write in <a href="/policies/manage">Policy Editor</a>: sections in order, each able
              to cite the controls it satisfies.</li>
            <li>Move it through the workflow — draft → submitted for review → approved, with
              reopen and retire — and keep the version history behind it.</li>
            <li>Check what the corpus actually covers at
              <a href="/policies/coverage">Policy Coverage</a>: which catalog controls have
              approved policy text behind them, and which only have a draft.</li>
            <li>Export a document as Markdown, HTML or template JSON, or typeset it as a
              deliverable (see Documents below).</li>
          </ol>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Regulation coverage</div>
        <div class="panel-body">
          <p class="purpose">Answer "does what we have already satisfy this regulation" article by
          article.</p>
          <ol>
            <li>Upload the regulation to the AI agent's library on the Wintermute server, then
              import it at <a href="/regulation-coverage">Regulation Coverage</a>.</li>
            <li>Run the analysis: every article is mapped against this installation's Security NFRs
              and 800-53 — what it requires, what already satisfies it, what does not, and how to
              close the gap.</li>
            <li>Read the report, download it as PDF, and keep the version history.</li>
            <li>Question the report in conversation and have it revised where you disagree; each
              revision is a new version rather than an overwrite.</li>
          </ol>
          <p class="note">Needs a Wintermute server and an agent, and an AI provider, configured
          in <a href="/settings">Settings</a>. Documents are never uploaded here.</p>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Risk and crisis exercises</div>
        <div class="panel-body">
          <p class="purpose">Plan, run and report the exercise arc from a red-team detonation to
          the supervisor.</p>
          <ol>
            <li>Create an exercise at <a href="/crisis-exercises">Crisis Exercises</a>: objectives,
              phases, participants and injects, each able to cite the controls, requirements,
              regulation clauses, risks and frameworks it exercises.</li>
            <li>Deliver it: record responses to injects, the decisions taken, the incident
              classification and the notification clocks it starts.</li>
            <li>Record findings as they surface, and use the coverage view to see what the exercise
              actually tested.</li>
            <li>Talk an exercise through on its <strong>Agent</strong> tab, or with Ask AI on any
              Crisis Exercises page. Both ask the Crisis Exercise agent chosen in
              <a href="/settings">Settings</a>, which sees the exercise as it stands.</li>
            <li>Export the MSEL as CSV, a participant handout as Markdown, and the report as PDF or
              JSON. An exercise can be cloned as the starting point for the next one.</li>
          </ol>
          <p class="note">The whole write surface is admin-only: an exercise record is evidence a
          supervisor may read.</p>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Detection rules</div>
        <div class="panel-body">
          <p class="purpose">Draft a Wiz-style runtime rule without hand-writing the payload.</p>
          <ol>
            <li>Build the rule at <a href="/wiz-rules">Wiz Rules</a> — event type, scope, severity
              and boolean conditions with regex or string operators.</li>
            <li>Copy or download the generated JSON and create the rule through your own Wiz API
              workflow. This page drafts the payload; it does not talk to Wiz.</li>
          </ol>
        </div>
      </article>
    </div>

    <h2>Reporting, data and integrations</h2>
    <div class="grid">
      <article class="panel">
        <div class="panel-header">Reports and extracts</div>
        <div class="panel-body">
          <ol>
            <li><a href="/reports">Reports</a> — operational dashboards over the catalogs, starting
              with the controls that no Security NFR covers.</li>
            <li><a href="/JSON_view">JSON_view</a> — one output panel for Security NFR JSON, family
              JSON, and JSON documents saved into the database.</li>
            <li>Per-page exports: Excel and CSV from Controls, RTF from Hierarchy, Markdown/HTML
              from Policy, PDF from Regulation Coverage and Crisis Exercises.</li>
          </ol>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Jira</div>
        <div class="panel-body">
          <ol>
            <li><a href="/jira/json">Jira JSON</a> — connect with your Jira Cloud credentials, test
              the authentication, and inspect project, issue and search JSON directly.</li>
            <li><a href="/jira/reports">Jira Reports</a> — read-only report and search presets over
              the same connection.</li>
          </ol>
          <p class="note">The connector's design and setup are documented in
          <a href="/knowledge/jira">JIRA_CONNECTOR.md</a>.</p>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Asking the AI</div>
        <div class="panel-body">
          <ol>
            <li><a href="/ai-chat">AI Chat</a> — ask either the Anthropic Claude API or your own
              Wintermute server. Per question you choose the provider, the model, and for
              Wintermute the backend and the <strong>agent</strong>: which document library the
              answer is grounded in. No agent means an answer from the model's training data
              rather than from this installation's material.</li>
            <li>The <strong>Usage</strong> panel on that page reports what has been spent, per
              provider.</li>
            <li>The <strong>Ask AI</strong> dock on every other page asks the same gateway with the
              provider and agent Settings configures.</li>
          </ol>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Client-ready documents</div>
        <div class="panel-body">
          <ol>
            <li><a href="/templates">Templates</a> — typeset a policy document as a deliverable.
              Typst is the primary path; LaTeX is there for house styles that already use it.</li>
            <li><a href="/templates/manage">Template Brand</a> — identity, palette and type for
              every rendered deliverable, applied at render time.</li>
          </ol>
        </div>
      </article>
    </div>

    <h2>Running the installation</h2>
    <div class="grid">
      <article class="panel">
        <div class="panel-header">Settings and credentials</div>
        <div class="panel-body">
          <p><a href="/settings">Settings</a> holds the Anthropic API key and the Wintermute client
          token — the only place either is set — plus which provider answers, and the default
          backend, model and agent every AI field on this installation uses.</p>
          <p class="note">Keys are stored encrypted and are never returned to a browser; a page can
          only learn whether one exists.</p>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Users and access</div>
        <div class="panel-body">
          <p>User Management (admin, and only when login is enabled) creates and removes accounts,
          grants the admin flag, and sets which pages each account may open. Reading is generally
          open to a signed-in user; changing data is admin-only.</p>
          <p class="note">In local mode there is no login at all and every page and action is open.
          That mode is for a single-user laptop, never for a server.</p>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Backup, transfer and integrity</div>
        <div class="panel-body">
          <ol>
            <li><a href="/utilities">Utilities</a> → <strong>Full database backup</strong>: an
              engine-native copy of the live database, the recommended disaster-recovery
              backup.</li>
            <li><strong>Integrity check</strong>: the database's own consistency checks, read-only,
              to run before trusting a backup.</li>
            <li><strong>Export full data set</strong>: one JSON snapshot of every managed table, for
              moving data to a disconnected deployment or across a SQLite/PostgreSQL boundary.</li>
            <li><strong>Import &amp; overwrite</strong>: replace every managed table with a
              snapshot. This deletes rows that are not in the file — take a backup first.</li>
          </ol>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">Reference and machine surfaces</div>
        <div class="panel-body">
          <ol>
            <li><a href="/changelog">Change Log</a> — what changed in this application and when, the
              first place to look when something behaves differently.</li>
            <li><a href="/docs">API Docs</a> and <a href="/openapi.json">/openapi.json</a> — the
              HTTP API behind every page.</li>
            <li>The knowledge documents shipped with the app: <a href="/knowledge/runtime">runtime
              arguments</a>, <a href="/knowledge/agents">agent policy</a>,
              <a href="/knowledge/faq">FAQ</a>, <a href="/knowledge/jira">Jira connector</a> and
              <a href="/knowledge/risk-register">risk register framework</a>.</li>
            <li><code>/api/knowledge</code> — a read-only query surface over the NFRs, controls,
              regulation coverage, policies, risks and exercises, behind its own token, for an
              external AI agent.</li>
          </ol>
        </div>
      </article>
    </div>

    <h2>Every page, and what it is for</h2>
    <div class="table-wrap">
      <table>
        <thead>
          <tr><th>Section</th><th>Page</th><th>What it is for</th></tr>
        </thead>
        <tbody>
          <tr><td class="section">Catalog</td><td class="page"><a href="/controls">/controls</a></td><td>Browse and filter the 800-53 catalog; export Excel or CSV.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/controls/manage">/controls/manage</a></td><td>Add, edit and delete control rows.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/controls/hierarchy">/controls/hierarchy</a></td><td>Family → control → enhancement, exportable as RTF.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/controls/family-visibility">/controls/family-visibility</a></td><td>Choose which families appear in every filter.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/security-nfrs">/security-nfrs</a></td><td>Browse the Security NFR catalog and its NIST mappings.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/security-nfrs/manage">/security-nfrs/manage</a></td><td>Add, edit and delete requirements.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/security-nfrs/links">/security-nfrs/links</a></td><td>Requirement ↔ control cross-reference, with overrides and rebuild.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/nfr-enrichment">/nfr-enrichment</a></td><td>Propose catalog entries from a document in the agent's library, for review.</td></tr>
          <tr><td class="section">Catalog</td><td class="page"><a href="/asset-types">/asset-types</a></td><td>Resolve the baseline and controls for an asset tier.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/risk-register">/risk-register</a></td><td>The security risk register and its dashboard.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/risk-register/manage">/risk-register/manage</a></td><td>Create and edit risk entries.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/exceptions">/exceptions</a></td><td>Build an exception set from the requirements catalog.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/policies">/policies</a></td><td>The policy library and its review states.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/policies/manage">/policies/manage</a></td><td>Write policy sections and cite controls.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/policies/coverage">/policies/coverage</a></td><td>Which controls have policy text behind them.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/regulation-coverage">/regulation-coverage</a></td><td>Map a regulation article by article; report, question, revise.</td></tr>
          <tr><td class="section">Crisis Exercises</td><td class="page"><a href="/crisis-exercises">/crisis-exercises</a></td><td>Plan, deliver and report crisis and continuity exercises.</td></tr>
          <tr><td class="section">Compliance &amp; Risk</td><td class="page"><a href="/wiz-rules">/wiz-rules</a></td><td>Draft a Wiz-style detection rule payload.</td></tr>
          <tr><td class="section">Reporting &amp; Data</td><td class="page"><a href="/reports">/reports</a></td><td>Operational reporting over the catalogs.</td></tr>
          <tr><td class="section">Reporting &amp; Data</td><td class="page"><a href="/JSON_view">/JSON_view</a></td><td>One panel for requirement, family and stored JSON.</td></tr>
          <tr><td class="section">Reporting &amp; Data</td><td class="page"><a href="/jira/json">/jira/json</a></td><td>Inspect Jira project, issue and search JSON.</td></tr>
          <tr><td class="section">Reporting &amp; Data</td><td class="page"><a href="/jira/reports">/jira/reports</a></td><td>Read-only Jira report and search presets.</td></tr>
          <tr><td class="section">Reporting &amp; Data</td><td class="page"><a href="/changelog">/changelog</a></td><td>What changed in this application, and when.</td></tr>
          <tr><td class="section">AI Chat</td><td class="page"><a href="/ai-chat">/ai-chat</a></td><td>Ask Claude or your Wintermute server; choose provider, model and agent.</td></tr>
          <tr><td class="section">Documents</td><td class="page"><a href="/templates">/templates</a></td><td>Typeset a policy document as a deliverable.</td></tr>
          <tr><td class="section">Documents</td><td class="page"><a href="/templates/manage">/templates/manage</a></td><td>Brand identity, palette and type for rendered documents.</td></tr>
          <tr><td class="section">Admin</td><td class="page"><a href="/help">/help</a></td><td>This page.</td></tr>
          <tr><td class="section">Admin</td><td class="page"><a href="/settings">/settings</a></td><td>AI credentials, provider, backend, model and agent.</td></tr>
          <tr><td class="section">Admin</td><td class="page"><a href="/docs">/docs</a></td><td>The HTTP API behind every page.</td></tr>
          <tr><td class="section">Admin</td><td class="page"><a href="/utilities">/utilities</a></td><td>Backup, integrity check, export and import.</td></tr>
        </tbody>
      </table>
    </div>
  </main>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
