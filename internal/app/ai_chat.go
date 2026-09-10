package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/aiprovider"
	"grc/internal/crisisexercise"
	"grc/internal/pageui"
	"grc/internal/settings"
)

// The Claude and Wintermute protocols, their endpoint rules and their session
// titles live in internal/aiprovider, which this page now asks through.

var aiProviderHTTPClient = &http.Client{Timeout: 90 * time.Second}

// aiChatRequest is one question from the page. It deliberately carries no
// credential: keys and tokens are set once in the Settings page and resolved
// server-side, so a secret never rides in a browser request to this endpoint.
// Nor does it carry a model: that is set in Settings too (see aiChatProvider).
type aiChatRequest struct {
	Provider string `json:"provider"`
	Question string `json:"question"`
	System   string `json:"system_prompt"`
	Endpoint string `json:"endpoint"`
	// Backend names a wintermuted backend (a local model server, or Claude)
	// for the wintermute provider. Empty means that server's default.
	Backend string `json:"backend"`
	// Agent names an agent profile on that server — the document library and
	// sources the answer is grounded in. It is a pointer because "no agent" and
	// "not specified" are different instructions: the AI Chat page always says
	// which agent it means, including none at all, while the AI dock omits the
	// field and gets the one configured in Settings.
	Agent *string `json:"agent"`
	// History is the conversation before Question, oldest first, so a follow-up
	// question can refer to what came before. The client holds the transcript —
	// this endpoint is stateless — and the server bounds what it will accept
	// (see boundedHistory).
	History []aiChatTurn `json:"history"`
	// SessionID continues a conversation the provider itself is holding, from a
	// previous answer's session_id. Wintermute keeps transcripts server-side;
	// when this is set, History is not resent.
	SessionID string `json:"session_id"`
	// Page is the path the AI dock was asked from. On a Crisis Exercises page
	// the question goes to that module's agent, about the exercise on screen
	// (see crisisDockTurn).
	Page string `json:"page"`
}

// aiChatTurn is one earlier message in the conversation.
type aiChatTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func aiChatPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AI Chat · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --panel: rgba(255,252,246,0.94);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
      --warn: #9a6700;
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
      box-shadow: 0 24px 60px rgba(28,36,49,0.12);
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
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; letter-spacing: -0.03em; }
    p { color: var(--muted); }
    .page-head {
      display: flex;
      gap: 16px 24px;
      flex-wrap: wrap;
      align-items: flex-end;
      justify-content: space-between;
    }
    .page-head p { margin: 6px 0 0; max-width: 78ch; font-size: 15px; }
    .creds {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      align-items: center;
      font-family: Arial, sans-serif;
      font-size: 12px;
    }
    /* The chips carry their own opaque surface and literal text colours: the
       global theme repaints --ink/--muted for the dark themes, which would
       otherwise leave pale text on these light pills. */
    .chip {
      display: inline-block;
      padding: 5px 11px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: var(--surface-strong);
      color: #5e6672;
      white-space: nowrap;
    }
    .chip.on { border-color: #7fae94; color: #0b5d3b; background: var(--surface-strong); }
    .chip.off { border-color: #d7b271; color: #9a6700; background: var(--surface-strong); }
    /* Transparent so the theme's own link colour stays legible on whichever
       background the page is painted with. */
    a.chip { text-decoration: none; background: transparent; }
    .layout {
      margin-top: 16px;
      display: flex;
      flex-direction: column;
      gap: 16px;
      /* Chat is the work; the page fills the viewport so the conversation gets
         the leftover height rather than stopping at a fixed box. */
      height: clamp(460px, calc(100dvh - 250px), 1400px);
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
      overflow: hidden;
      display: flex;
      flex-direction: column;
      min-height: 0;
    }
    .panel-header {
      padding: 12px 16px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 10px;
      flex: 0 0 auto;
    }
    .panel-header button {
      padding: 5px 12px;
      font-size: 12px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      font-family: Arial, sans-serif;
    }
    .panel-header-actions { display: flex; align-items: center; gap: 10px; }
    /* The transcript is resent with every question, so what it is costing is
       worth stating rather than leaving to be inferred from the bill. */
    .context-note { font-size: 11px; letter-spacing: 0.06em; }
    .panel-body { padding: 16px; overflow: auto; flex: 1; min-height: 0; }
    .panel.chat { flex: 1 1 auto; }
    .routing { flex: 0 0 auto; }
    .routing .panel-body { flex: 0 0 auto; padding-bottom: 10px; }
    .routing-row { display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-end; }
    .routing-row .field { flex: 1 1 240px; }
    .routing-row button { flex: 0 0 auto; }
    .routing.off .routing-row { opacity: 0.55; }
    .routing .hint { margin: 8px 0 0; }
    .field label {
      display: block;
      margin-bottom: 6px;
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-weight: 700;
    }
    input, select, textarea {
      width: 100%;
      padding: 11px 12px;
      border-radius: 12px;
      border: 1px solid var(--line);
      background: var(--bg);
      color: var(--ink);
      font: inherit;
    }
    textarea { min-height: 120px; resize: vertical; }
    .status { color: var(--muted); min-height: 22px; }
    .status.warn { color: var(--warn); }
    button {
      padding: 11px 14px;
      border-radius: 999px;
      border: 0;
      background: #dfc4b4;
      color: var(--ink);
      font: inherit;
      cursor: pointer;
    }
    button.secondary {
      background: var(--surface-strong);
      border: 1px solid var(--line);
    }
    button:disabled, select:disabled { cursor: not-allowed; }
    .chat-box {
      flex: 1;
      min-height: 0;
      overflow: auto;
      padding: 16px;
      display: flex;
      flex-direction: column;
      gap: 10px;
    }
    .msg {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: var(--surface-strong);
      padding: 10px 14px;
      white-space: pre-wrap;
      word-break: break-word;
      max-width: min(80ch, 88%);
    }
    .msg.user { align-self: flex-end; border-left: 4px solid #8b3d2e; }
    .msg.ai { align-self: flex-start; border-left: 4px solid #0b5d3b; }
    .composer {
      flex: 0 0 auto;
      border-top: 1px solid rgba(215,206,191,0.9);
      /* Transparent rather than tinted: the panel underneath is repainted by
         the global theme, and a fixed tint here reads as a grey band on the
         dark themes. */
      background: transparent;
      padding: 12px 16px;
    }
    .composer textarea { min-height: 70px; max-height: 34vh; }
    .composer-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      margin-top: 8px;
    }
    .hint { font-family: Arial, sans-serif; font-size: 12px; color: var(--muted); }
    .meta {
      color: var(--muted);
      font-family: Arial, sans-serif;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.06em;
      margin-bottom: 6px;
    }
    .usage-panel {
      margin: 12px 16px 0;
      flex: 0 0 auto;
      max-height: 40%;
      overflow: auto;
      border: 1px solid var(--line);
      border-radius: 12px;
      background: var(--surface-strong);
      padding: 10px 14px;
      font-family: Arial, sans-serif;
      font-size: 13px;
    }
    .usage-row {
      display: flex;
      justify-content: space-between;
      padding: 3px 0;
      border-bottom: 1px solid rgba(215,206,191,0.4);
      color: var(--ink);
    }
    .usage-row:last-child { border-bottom: none; }
    .usage-row span:first-child { color: var(--muted); }
    .usage-section-head {
      font-weight: 700;
      text-transform: uppercase;
      font-size: 11px;
      letter-spacing: 0.07em;
      color: var(--muted);
      padding: 6px 0 2px;
    }
    @media (max-width: 980px) {
      .layout { height: auto; }
      .chat { min-height: 70vh; }
      .msg { max-width: 100%; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/ai-chat") + `
    <header class="page-head">
      <div>
        <h1>AI Chat Gateway</h1>
        <p>Ask the AI provider configured in Settings: the Anthropic Claude API, or a Wintermute server that routes the question to a self-hosted model on your network or on to Claude.</p>
      </div>
      <div class="creds">
        <span id="claudeChip" class="chip">Anthropic key: checking&hellip;</span>
        <span id="wintermuteChip" class="chip">Wintermute token: checking&hellip;</span>
        <a class="chip" href="/settings">Keys &amp; tokens &rarr; Settings</a>
        <a class="chip" id="wintermuteDocsLink" href="/settings" hidden>Add documents in Wintermute &#8599;</a>
      </div>
    </header>

    <div class="layout">
      <section id="routing" class="panel routing">
        <div class="panel-header">
          <span>Backend &amp; agent</span>
          <span id="answeringWith" class="context-note">checking&hellip;</span>
        </div>
        <div class="panel-body">
          <div class="routing-row">
            <div class="field">
              <label for="wintermuteBackend">Backend</label>
              <select id="wintermuteBackend">
                <option value="">Server default</option>
              </select>
            </div>
            <div class="field">
              <label for="wintermuteAgent">Agent</label>
              <select id="wintermuteAgent">
                <option value="">No agent &mdash; general assistant</option>
              </select>
            </div>
            <button id="loadCatalog" class="secondary" type="button">Refresh backends &amp; agents</button>
          </div>
          <p id="routingNote" class="hint" hidden></p>
          <p class="hint"><span id="agentDetail"></span> <span id="catalogDetail"></span></p>
        </div>
      </section>

      <section class="panel chat">
        <div class="panel-header">
          <span>Conversation</span>
          <span class="panel-header-actions">
            <span id="contextNote" class="context-note"></span>
            <button id="usageBtn" class="secondary" type="button">Usage &#9656;</button>
            <button id="clearBtn" class="secondary" type="button">Clear</button>
          </span>
        </div>
        <div id="usagePanel" class="usage-panel" style="display:none;"></div>
        <div id="chatBox" class="chat-box">
          <div class="msg">
            <div class="meta">System</div>
            Responses will appear here after you submit a question.
          </div>
        </div>
        <form id="chatForm" class="composer">
          <textarea id="question" required placeholder="Ask your question here" aria-label="Question"></textarea>
          <div class="composer-row">
            <span id="status" class="status hint">Enter sends &middot; Shift+Enter for a new line</span>
            <button id="sendBtn" type="submit">Ask</button>
          </div>
        </form>
      </section>
    </div>
  </main>

  <script>
    const routing = document.getElementById('routing');
    const answeringWith = document.getElementById('answeringWith');
    const routingNote = document.getElementById('routingNote');
    const wintermuteBackend = document.getElementById('wintermuteBackend');
    const wintermuteAgent = document.getElementById('wintermuteAgent');
    const agentDetail = document.getElementById('agentDetail');
    const loadCatalogBtn = document.getElementById('loadCatalog');
    const catalogDetail = document.getElementById('catalogDetail');
    const claudeChip = document.getElementById('claudeChip');
    const wintermuteChip = document.getElementById('wintermuteChip');
    const wintermuteDocsLink = document.getElementById('wintermuteDocsLink');
    const question = document.getElementById('question');
    const chatForm = document.getElementById('chatForm');
    const sendBtn = document.getElementById('sendBtn');
    const clearBtn = document.getElementById('clearBtn');
    const statusEl = document.getElementById('status');
    const chatBox = document.getElementById('chatBox');
    const contextNote = document.getElementById('contextNote');
    const COMPOSER_HINT = 'Enter sends · Shift+Enter for a new line';

    // The conversation is carried, so a follow-up question means what it says.
    // /ai-chat/ask is stateless: the transcript goes back with each turn, except
    // on a Wintermute session, which the server holds and only needs its id.
    // The server bounds both.
    let history = [];
    let sessionID = '';

    // What Settings configures, from the status call. The provider and model are
    // not chosen on this page: a question goes wherever Settings routes every
    // other AI field. Until the status call answers nothing is assumed, so a
    // failed check reads as unknown rather than as a provider that is not there.
    const configured = {
      loaded: false,
      provider: '',
      claude: false,
      wintermute: false,
      claudeModel: '',
      wintermuteModel: '',
      backend: '',
      agent: '',
    };

    function setStatus(message, isWarning) {
      statusEl.textContent = message || COMPOSER_HINT;
      statusEl.className = 'status hint' + (isWarning ? ' warn' : '');
    }

    function esc(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function appendMessage(role, content) {
      const kind = role === 'User' ? 'user' : (role === 'Assistant' ? 'ai' : '');
      const row = document.createElement('div');
      row.className = 'msg ' + kind;
      row.innerHTML =
        '<div class="meta">' + esc(role) + '</div>' +
        esc(content || '').replaceAll('\n', '<br>');
      chatBox.appendChild(row);
      chatBox.scrollTop = chatBox.scrollHeight;
    }

    // "auto" prefers a configured Wintermute, which is what the router does, so
    // this is the provider a question would actually go to.
    function usesWintermute() {
      return configured.provider === 'wintermute'
        || (configured.provider === 'auto' && configured.wintermute);
    }

    // Names what will answer, since the page no longer has a field that says so.
    // Settings pins its Wintermute model within its own backend, so a different
    // backend picked here gets that backend's default — the rule the server
    // applies.
    function renderAnsweringWith() {
      if (!configured.loaded) return;
      if (!usesWintermute()) {
        answeringWith.textContent = 'Answering with Claude · ' + (configured.claudeModel || 'default model');
        return;
      }
      const backend = wintermuteBackend.value;
      let model;
      if (backend === configured.backend && configured.wintermuteModel) {
        model = configured.wintermuteModel;
      } else {
        model = backend ? 'default model for ' + backend : 'backend default model';
      }
      answeringWith.textContent = 'Answering with Wintermute · ' + model;
    }

    // A backend and an agent only mean something on Wintermute. On Claude they
    // stay in view, so the reader can see what is set, but cannot be changed:
    // a choice that silently does nothing is worse than none.
    function renderRouting() {
      if (!configured.loaded) return;
      const wm = usesWintermute();
      routing.classList.toggle('off', !wm);
      wintermuteBackend.disabled = !wm;
      wintermuteAgent.disabled = !wm;
      loadCatalogBtn.disabled = !wm;
      routingNote.hidden = wm;
      if (!wm) {
        routingNote.textContent =
          'Questions go to Claude, which has no backends or agents. ' +
          'Choose Wintermute or Auto under Settings → AI provider to use them.';
      }
      renderAnsweringWith();
    }

    // The backend comes from the server rather than being typed in. A name that
    // is one character out is not an error anyone sees here: the question fails
    // at ask time, or quietly goes to the server's default and reads as though
    // the pin worked.
    let catalog = { backends: [], default_backend: '', fallback: '' };

    // Keeps a select's current value selectable while its list is rebuilt, and
    // lets a stored default be shown before any list has arrived.
    function ensureOption(select, value, label) {
      if (!value) return;
      for (let i = 0; i < select.options.length; i++) {
        if (select.options[i].value === value) return;
      }
      const opt = document.createElement('option');
      opt.value = value;
      opt.textContent = label || value;
      select.appendChild(opt);
    }

    function clearOptions(select) {
      while (select.options.length > 1) select.remove(1);
    }

    function renderBackends(selected) {
      const want = selected !== undefined ? selected : wintermuteBackend.value;
      clearOptions(wintermuteBackend);
      wintermuteBackend.options[0].textContent = catalog.default_backend
        ? 'Server default — ' + catalog.default_backend
        : 'Server default';
      catalog.backends.forEach((backend) => {
        const opt = document.createElement('option');
        opt.value = backend.name;
        const notes = [];
        if (backend.kind) notes.push(backend.kind);
        if (backend.status && backend.status !== 'ok') notes.push(backend.status);
        opt.textContent = backend.name + (notes.length ? ' (' + notes.join(', ') + ')' : '');
        wintermuteBackend.appendChild(opt);
      });
      // A backend the server no longer has is shown as such rather than
      // dropped: it is the configured value, and losing it silently here is
      // how a question ends up somewhere nobody chose.
      ensureOption(wintermuteBackend, want, want + ' — not on this server');
      wintermuteBackend.value = want || '';
      renderAnsweringWith();
    }

    async function loadCatalog(selectedBackend) {
      catalogDetail.textContent = 'Loading backends…';
      try {
        const resp = await fetch('/ai-chat/wintermute/catalog');
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));

        catalog = {
          backends: data.backends || [],
          default_backend: data.default_backend || '',
          fallback: data.fallback || '',
        };
        renderBackends(selectedBackend);

        let detail = catalog.backends.length
          ? catalog.backends.length + ' backend(s) on this server.'
          : 'This server has no backends yet.';
        if (catalog.fallback) detail += ' Fallback: ' + catalog.fallback + '.';
        catalogDetail.textContent = detail;
      } catch (err) {
        catalogDetail.textContent = 'Could not list backends: ' + err.message;
      }
    }

    // Which agent answers decides which documents the answer is grounded in,
    // so it is a per-question choice here rather than only an installation
    // setting. The list comes from the server for the same reason the backend
    // list does: an agent id that is one character out is not an error anyone
    // sees — it is an ungrounded answer wearing the same confidence as a
    // grounded one.
    let agents = [];
    // Set once the reader picks an agent, so a status response arriving late
    // cannot move the choice under them.
    let agentTouched = false;
    let docsBase = '';

    // Documents live on the Wintermute server, which owns the library and the
    // search over it; this is the way there rather than a second upload page
    // here. It points at the agent the next question would use, since that is
    // the library a missing document would have to be added to.
    function syncDocsLink() {
      if (!docsBase) return;
      wintermuteDocsLink.href = docsBase.replace(/\/+$/, '') + '/#agents';
      wintermuteDocsLink.hidden = false;
      wintermuteDocsLink.target = '_blank';
      wintermuteDocsLink.rel = 'noopener noreferrer';
      const chosen = wintermuteAgent.value;
      const agent = agentByID(chosen);
      wintermuteDocsLink.textContent = chosen
        ? 'Documents for ' + ((agent && agent.name) || chosen) + ' ↗'
        : 'Add documents in Wintermute ↗';
    }

    function agentByID(id) {
      return agents.find((agent) => agent.id === id) || null;
    }

    // Says what the current choice means rather than only naming it: "no agent"
    // is a real option here, and the difference it makes to an answer is the
    // one thing this field has to make visible.
    function renderAgentDetail(note) {
      if (note) {
        agentDetail.textContent = note;
        return;
      }
      const chosen = wintermuteAgent.value;
      if (!chosen) {
        agentDetail.textContent = agents.length
          ? 'No agent: answered from the model’s training data, not from this installation’s documents.'
          : '';
        return;
      }
      const agent = agentByID(chosen);
      if (!agent) {
        agentDetail.textContent = 'This agent is not on the server — the question would be asked without one.';
        return;
      }
      const parts = [];
      if (agent.description) parts.push(agent.description);
      if (agent.sources && agent.sources.length) parts.push('Sources: ' + agent.sources.join(', ') + '.');
      agentDetail.textContent = parts.join(' ');
    }

    function renderAgents(selected) {
      const want = selected !== undefined ? selected : wintermuteAgent.value;
      clearOptions(wintermuteAgent);
      agents.forEach((agent) => {
        const opt = document.createElement('option');
        opt.value = agent.id;
        opt.textContent = agent.name || agent.id;
        wintermuteAgent.appendChild(opt);
      });
      // A configured agent the server no longer has is shown as such rather
      // than dropped: silently falling back to none is how a question stops
      // being grounded without anyone being told.
      ensureOption(wintermuteAgent, want, want + ' — not on this server');
      wintermuteAgent.value = want || '';
      renderAgentDetail();
      syncDocsLink();
    }

    async function loadAgents(selected) {
      renderAgentDetail('Loading agents…');
      try {
        const resp = await fetch('/ai-chat/wintermute/agents');
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        agents = data.agents || [];
        renderAgents(selected);
        if (!agents.length) {
          renderAgentDetail('This server has no agents yet — create one there first.');
        }
      } catch (err) {
        renderAgentDetail('Could not list agents: ' + err.message);
      }
    }

    function renderContextNote() {
      const turns = history.length / 2;
      contextNote.textContent = turns ? turns + (turns === 1 ? ' turn of context' : ' turns of context') : '';
    }

    // A Wintermute session is pinned to the backend and agent it was opened
    // with, so changing either has to start a new one. The transcript survives
    // that: it is the client's, and it is sent with the first question of the
    // new session.
    function dropSession() {
      sessionID = '';
    }

    // The credentials live in Settings, so this page only reports whether each
    // provider can answer — an operator should not have to submit a question to
    // find out that nothing is configured.
    function setChip(el, label, ok) {
      el.className = 'chip ' + (ok ? 'on' : 'off');
      el.textContent = label + (ok ? ': configured' : ': not configured');
    }

    async function refreshStatus() {
      try {
        const resp = await fetch('/ai-chat/wintermute/status');
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        configured.provider = String(data.provider || '');
        configured.claude = Boolean(data.claude_configured);
        configured.wintermute = Boolean(data.configured) && Boolean(data.token_configured);
        configured.claudeModel = String(data.claude_model || '');
        configured.wintermuteModel = String(data.default_model || '');
        configured.backend = String(data.default_backend || '');
        configured.agent = String(data.default_agent || '');
        configured.loaded = true;
        setChip(claudeChip, 'Anthropic key', configured.claude);
        setChip(wintermuteChip, 'Wintermute token', Boolean(data.token_configured));
        docsBase = String(data.default_endpoint || '');
        // Settings' own backend and agent are this page's starting point, and
        // are shown before the server has been asked for its lists so a slow or
        // failed lookup does not leave the fields looking unset. A reader who
        // changes nothing gets the answer the AI dock would give.
        if (!wintermuteBackend.value && configured.backend) {
          ensureOption(wintermuteBackend, configured.backend, configured.backend);
          wintermuteBackend.value = configured.backend;
        }
        if (!agentTouched && !wintermuteAgent.value && configured.agent) {
          ensureOption(wintermuteAgent, configured.agent, configured.agent);
          wintermuteAgent.value = configured.agent;
          renderAgentDetail();
        }
        syncDocsLink();
        renderRouting();
        // The lists are only fetched when a question would go to Wintermute:
        // on Claude there is nothing for them to change.
        if (usesWintermute()) {
          loadCatalog(wintermuteBackend.value).catch(() => { /* reported inline */ });
          loadAgents(wintermuteAgent.value).catch(() => { /* reported inline */ });
        }
      } catch (_) {
        setChip(claudeChip, 'Anthropic key', false);
        setChip(wintermuteChip, 'Wintermute token', false);
        answeringWith.textContent = 'provider unknown';
      }
    }

    // A Wintermute session is created with its agent, so changing the agent
    // starts a new one rather than carrying the question into the library the
    // previous answers came from.
    for (const field of [wintermuteBackend, wintermuteAgent]) {
      field.addEventListener('change', dropSession);
    }

    wintermuteAgent.addEventListener('change', () => {
      agentTouched = true;
      renderAgentDetail();
      syncDocsLink();
    });

    wintermuteBackend.addEventListener('change', renderAnsweringWith);

    loadCatalogBtn.addEventListener('click', () => {
      loadCatalog().catch((err) => { catalogDetail.textContent = err.message; });
      loadAgents().catch((err) => { renderAgentDetail(err.message); });
    });

    const query = new URLSearchParams(window.location.search);
    const prefilledQuestion = String(query.get('q') || '').trim();
    if (prefilledQuestion && !question.value.trim()) {
      question.value = prefilledQuestion;
    }

    clearBtn.addEventListener('click', () => {
      chatBox.innerHTML =
        '<div class="msg"><div class="meta">System</div>Responses will appear here after you submit a question.</div>';
      history = [];
      dropSession();
      renderContextNote();
      setStatus('');
    });

    // Enter sends, so the composer behaves like a chat box rather than a form
    // field; Shift+Enter still writes a multi-line question.
    question.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' && !event.shiftKey && !event.ctrlKey && !event.metaKey) {
        event.preventDefault();
        chatForm.requestSubmit();
      }
    });

    chatForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      const text = question.value.trim();
      if (!text) {
        setStatus('Question is required.', true);
        return;
      }
      const wm = usesWintermute();
      // Credentials are install-wide, so the only thing this page can check is
      // whether the server has one for the provider Settings routes to.
      if (configured.loaded && !wm && !configured.claude) {
        setStatus('No Anthropic API key is configured. Set one in Settings.', true);
        return;
      }
      if (configured.loaded && wm && !configured.wintermute) {
        setStatus('Wintermute needs a server URL and client token. Set them in Settings.', true);
        return;
      }

      const payload = {
        question: text,
        // A resumed Wintermute session already holds the transcript; sending it
        // again would replay every earlier turn into the same session.
        history: sessionID ? [] : history,
        session_id: sessionID
      };
      // On Claude the provider is left unnamed, so the router every AI field
      // asks through decides — the same request the AI dock sends. On
      // Wintermute it is named, because the backend and agent chosen above ride
      // with it. The agent is sent even when empty: asking without one is not
      // the same instruction as saying nothing and getting the Settings agent.
      if (wm) {
        payload.provider = 'wintermute';
        payload.backend = wintermuteBackend.value.trim();
        payload.agent = wintermuteAgent.value.trim();
      }

      appendMessage('User', text);
      setStatus('Waiting for model response...');
      sendBtn.disabled = true;

      try {
        const resp = await fetch('/ai-chat/ask', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        const answer = data.answer || '(empty answer)';
        appendMessage('Assistant', answer);
        // Recorded only on success: a question that never got an answer would
        // otherwise sit in the transcript as context for every later turn.
        history.push({ role: 'user', content: text });
        history.push({ role: 'assistant', content: answer });
        sessionID = data.session_id || '';
        renderContextNote();
        question.value = '';
        let served = data.provider || 'the AI provider';
        if (data.backend) served += ' / ' + data.backend;
        if (data.model) served += ' (' + data.model + ')';
        setStatus('Response received from ' + served + '.');
      } catch (err) {
        appendMessage('Assistant', 'Error: ' + (err.message || 'request failed'));
        setStatus('Request failed.', true);
      } finally {
        sendBtn.disabled = false;
        question.focus();
      }
    });

    refreshStatus();

    const usageBtn = document.getElementById('usageBtn');
    const usagePanel = document.getElementById('usagePanel');
    let usageOpen = false;

    function renderUsage(d) {
      const providerLabel = { 'claude': 'Claude', 'wintermute': 'Wintermute' };
      let html = '';
      if (d.providers && d.providers.length > 0) {
        for (const p of d.providers) {
          const label = providerLabel[p.provider] || p.provider;
          html += '<div class="usage-section-head">' + esc(label) + '</div>';
          html += '<div class="usage-row"><span>Today (requests)</span><span>' + p.today_request_count + '</span></div>';
          html += '<div class="usage-row"><span>Today (in / out tokens)</span><span>' + p.today_input_tokens + ' / ' + p.today_output_tokens + '</span></div>';
          html += '<div class="usage-row"><span>All-time (requests)</span><span>' + p.request_count + '</span></div>';
          html += '<div class="usage-row"><span>All-time (in / out tokens)</span><span>' + p.input_tokens + ' / ' + p.output_tokens + '</span></div>';
        }
        if (d.providers.length > 1) {
          const t = d.total;
          html += '<div class="usage-section-head">Total</div>';
          html += '<div class="usage-row"><span>Today (requests)</span><span>' + t.today_request_count + '</span></div>';
          html += '<div class="usage-row"><span>Today (in / out tokens)</span><span>' + t.today_input_tokens + ' / ' + t.today_output_tokens + '</span></div>';
          html += '<div class="usage-row"><span>All-time (requests)</span><span>' + t.request_count + '</span></div>';
          html += '<div class="usage-row"><span>All-time (in / out tokens)</span><span>' + t.input_tokens + ' / ' + t.output_tokens + '</span></div>';
        }
      } else {
        html = '<div class="usage-row"><span>No requests recorded yet.</span><span></span></div>';
      }
      usagePanel.innerHTML = html;
    }

    async function toggleUsage() {
      usageOpen = !usageOpen;
      usageBtn.innerHTML = usageOpen ? 'Usage &#9662;' : 'Usage &#9656;';
      if (!usageOpen) { usagePanel.style.display = 'none'; return; }
      usagePanel.style.display = '';
      usagePanel.innerHTML = '<div class="usage-row"><span>Loading…</span><span></span></div>';
      try {
        const resp = await fetch('/ai-chat/usage');
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        renderUsage(data);
      } catch (err) {
        usagePanel.innerHTML = '<div class="usage-row"><span>Error: ' + esc(err.message) + '</span><span></span></div>';
      }
    }

    usageBtn.addEventListener('click', toggleUsage);
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// aiChatWintermuteStatus reports where Settings routes questions and the
// Wintermute defaults, so the page can say what will answer and prefill its
// backend and agent, and whether a credential is available for each provider
// so the page can say which one can answer before a question is sent.
//
// The credentials themselves are never returned — only whether one exists.
// Availability comes from storedAICredential, so it covers a key set in the
// Settings page as well as one inherited from the environment.
//
// The defaults come from the Settings preferences rather than the environment
// directly: Preference already falls back to the same variables, so an install
// configured either way prefills, and one configured in Settings no longer
// leaves this page blank.
func aiChatWintermuteStatus(c *gin.Context) {
	endpoint := aiChatPreference(settings.PrefWintermuteURL, "WINTERMUTE_URL")
	c.JSON(http.StatusOK, gin.H{
		// Which provider Settings routes to. The page asks through it and uses
		// it to decide whether its backend and agent apply.
		"provider":          aiChatPreference(settings.PrefAIProvider, ""),
		"configured":        endpoint != "",
		"token_configured":  storedAICredential("wintermute") != "",
		"claude_configured": storedAICredential("claude") != "",
		// The model Settings configures for Claude. The page shows it; it cannot
		// change it.
		"claude_model":     aiChatClaudeModel(),
		"default_endpoint": endpoint,
		"default_backend":  aiChatPreference(settings.PrefWintermuteBackend, "WINTERMUTE_BACKEND"),
		"default_agent":    aiChatPreference(settings.PrefWintermuteAgent, "WINTERMUTE_AGENT"),
		// The agent the dock asks on Crisis Exercises pages, when one is set.
		"crisis_agent":  aiChatPreference(settings.PrefCrisisAgent, ""),
		"default_model": aiChatPreference(settings.PrefWintermuteModel, "WINTERMUTE_MODEL"),
	})
}

// aiChatWintermuteCatalog lists the backends and models on the Wintermute
// server, so this page's backend field is a choice rather than a typed name —
// the same list the Settings page offers.
//
// The optional url parameter overrides the server, and reaches no further than
// the ask path already does: the same ValidateEndpoint rules apply, and the
// client token comes from Settings rather than the request.
func aiChatWintermuteCatalog(c *gin.Context) {
	endpoint := strings.TrimSpace(c.Query("url"))
	if endpoint == "" {
		endpoint = aiChatPreference(settings.PrefWintermuteURL, "WINTERMUTE_URL")
	}
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute server URL: set one in Settings"})
		return
	}
	token := storedAICredential("wintermute")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute client token: set one in Settings"})
		return
	}
	if _, err := aiprovider.ValidateEndpoint(endpoint); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	catalog, err := aiprovider.NewWintermute(func() aiprovider.WintermuteConfig {
		return aiprovider.WintermuteConfig{URL: endpoint, Token: token}
	}).Catalog(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

// aiChatRequestAgent resolves which agent profile a question is asked under.
// An absent field means the installation's configured agent; a present one —
// including the empty string, which the AI Chat page sends for "no agent" — is
// what the reader chose for this question.
func aiChatRequestAgent(req aiChatRequest) string {
	if req.Agent == nil {
		return aiChatPreference(settings.PrefWintermuteAgent, "WINTERMUTE_AGENT")
	}
	return strings.TrimSpace(*req.Agent)
}

// aiChatWintermuteAgents lists the agent profiles on the Wintermute server, so
// the agent behind an answer is a choice made per question rather than whatever
// Settings was last set to. Which agent answers decides which documents the
// answer is grounded in, and that is a question-by-question decision here in a
// way the server URL and the token are not.
//
// Same shape as aiChatWintermuteCatalog: the optional url parameter overrides
// the server, the client token comes from Settings and never from the request,
// and the endpoint passes the same ValidateEndpoint rules the ask path applies.
func aiChatWintermuteAgents(c *gin.Context) {
	endpoint := strings.TrimSpace(c.Query("url"))
	if endpoint == "" {
		endpoint = aiChatPreference(settings.PrefWintermuteURL, "WINTERMUTE_URL")
	}
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute server URL: set one in Settings"})
		return
	}
	token := storedAICredential("wintermute")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Wintermute client token: set one in Settings"})
		return
	}
	if _, err := aiprovider.ValidateEndpoint(endpoint); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agents, err := aiprovider.NewWintermute(func() aiprovider.WintermuteConfig {
		return aiprovider.WintermuteConfig{URL: endpoint, Token: token}
	}).Agents(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"agents": agents})
}

// aiChatClaudeModel is the model Settings configures for Claude, or the harness
// default when none is set.
func aiChatClaudeModel() string {
	if model := aiChatPreference(settings.PrefClaudeModel, ""); model != "" {
		return model
	}
	return aiprovider.DefaultClaudeModel
}

// aiChatClaudeProvider builds the Claude provider for one request, on the model
// Settings configures.
func aiChatClaudeProvider(req aiChatRequest) (aiprovider.Provider, error) {
	key := storedAICredential("claude")
	if key == "" {
		return nil, fmt.Errorf("no Anthropic API key: set one in Settings")
	}
	// The endpoint override stays restricted to Anthropic's own host.
	endpoint, err := validatedClaudeBaseURL(req.Endpoint)
	if err != nil {
		return nil, err
	}
	return aiprovider.NewClaude(func() string { return key }, aiChatClaudeModel()).WithBaseURL(endpoint), nil
}

// aiChatWintermuteModel resolves the model a Wintermute question is asked on:
// the one Settings configures. Settings pins that model within its own backend,
// so a question this page sends to a different backend gets that backend's
// default rather than a model it may not serve.
func aiChatWintermuteModel(backend string) string {
	if strings.TrimSpace(backend) != aiChatPreference(settings.PrefWintermuteBackend, "WINTERMUTE_BACKEND") {
		return ""
	}
	return aiChatPreference(settings.PrefWintermuteModel, "WINTERMUTE_MODEL")
}

// activeAIRouter is the shared provider harness, wired at startup. It follows
// the same configured-singleton shape as activeSettings because these handlers
// are package-level functions rather than methods on a service.
//
// Usage is logged by aiChatAsk rather than by the router: Selected returns the
// provider itself, so a docked question is counted once, in the same place a
// page question is.
var activeAIRouter *aiprovider.Router

// configureAIRouter wires the harness the AI dock routes through. A nil router
// means the dock falls back to Claude, which is what the unit tests exercise.
func configureAIRouter(router *aiprovider.Router) { activeAIRouter = router }

// crisisDock is the slice of the Crisis Exercise module the AI dock needs.
type crisisDock interface {
	DockQuestion(path, sessionID, question string) (crisisexercise.DockTurn, bool, error)
}

// activeCrisisDock is wired at startup, like activeAIRouter. Nil leaves every
// docked question as it was asked.
var activeCrisisDock crisisDock

func configureCrisisDock(dock crisisDock) { activeCrisisDock = dock }

// crisisDockTurn prepares a question the AI dock asked from a Crisis Exercises
// page for that module's agent. Any other question — from another page, or
// from the AI Chat page, which names its own provider and agent — goes as it
// was asked.
func crisisDockTurn(req aiChatRequest) (crisisexercise.DockTurn, error) {
	asked := crisisexercise.DockTurn{Prompt: req.Question, Answered: func(string) {}}
	if req.Provider != "" || activeCrisisDock == nil {
		return asked, nil
	}
	turn, ok, err := activeCrisisDock.DockQuestion(req.Page, req.SessionID, req.Question)
	if err != nil || !ok {
		return asked, err
	}
	return turn, nil
}

// aiChatPreference reads a Settings preference, falling back to its
// environment variable when no store is configured (the unit tests, and any
// build that runs without the settings service wired up).
func aiChatPreference(key, envVar string) string {
	if activeSettings == nil {
		return strings.TrimSpace(os.Getenv(envVar))
	}
	return strings.TrimSpace(activeSettings.Preference(key))
}

// activeSettings is the install-wide credential store, wired at startup by
// configureAICredentials. It follows the same configured-singleton shape as
// activeAIUsageStore because the AI Chat gateway handlers are package-level
// functions rather than methods on a service.
var activeSettings *settings.Service

// configureAICredentials wires the credential store the AI gateway falls back
// to. A nil service simply means no stored credentials, which is what the unit
// tests exercise.
func configureAICredentials(svc *settings.Service) { activeSettings = svc }

// storedAICredential returns the stored credential for a provider, or "".
func storedAICredential(provider string) string {
	if activeSettings == nil {
		return ""
	}
	switch provider {
	case "wintermute":
		return activeSettings.Get(settings.WintermuteToken)
	default:
		return activeSettings.Get(settings.AnthropicAPIKey)
	}
}

// storedWintermuteURL returns the server URL configured in Settings, or "".
func storedWintermuteURL() string {
	if activeSettings == nil {
		return ""
	}
	return activeSettings.Preference(settings.PrefWintermuteURL)
}

func aiChatAsk(c *gin.Context) {
	// Bounded because the body now carries a client-held transcript, so its
	// size is no longer a function of one typed question.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, aiChatMaxBodyBytes)

	var req aiChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	req.Question = strings.TrimSpace(req.Question)
	req.System = strings.TrimSpace(req.System)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Backend = strings.TrimSpace(req.Backend)
	req.SessionID = strings.TrimSpace(req.SessionID)

	if req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}

	provider, err := aiChatProvider(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dock, err := crisisDockTurn(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp, err := provider.Ask(c.Request.Context(), aiprovider.Request{
		System:    req.System,
		History:   boundedHistory(req.History),
		Prompt:    dock.Prompt,
		SessionID: req.SessionID,
		Agent:     dock.Agent,
		MaxTokens: aiChatMaxTokens,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if resp.Refused {
		c.JSON(http.StatusBadGateway, gin.H{"error": "the model declined to answer this question"})
		return
	}

	dock.Answered(resp.SessionID)

	// Logged against what actually served the turn: a wintermuted server
	// retries a failed backend against its fallback, so the model that
	// answered is not always the one that was asked for.
	logAIUsage(resp.Provider, resp.Model, int64(resp.Usage.InputTokens), int64(resp.Usage.OutputTokens))
	c.JSON(http.StatusOK, gin.H{
		"provider":   resp.Provider,
		"answer":     resp.Text,
		"backend":    resp.Backend,
		"model":      resp.Model,
		"session_id": resp.SessionID,
	})
}

// aiChatMaxTokens bounds one chat answer.
const aiChatMaxTokens = 4096

// The transcript a client may send back is bounded on both axes: a long
// conversation would otherwise grow every request until the model rejects it,
// and the token cost of a turn is paid by the operator. The oldest turns are
// dropped first, so a follow-up keeps the context that is actually near it.
const (
	aiChatMaxHistoryTurns = 20
	aiChatMaxHistoryChars = 24000
	// Comfortably above a full transcript at those limits, so the reader hits
	// the trim above rather than a rejected request.
	aiChatMaxBodyBytes = 512 << 10
)

// boundedHistory converts the client's transcript into harness turns, dropping
// blanks and unknown roles, and keeping only the most recent turns within the
// limits above.
func boundedHistory(turns []aiChatTurn) []aiprovider.Message {
	if len(turns) == 0 {
		return nil
	}

	kept := make([]aiprovider.Message, 0, len(turns))
	for _, turn := range turns {
		text := strings.TrimSpace(turn.Content)
		if text == "" {
			continue
		}
		role := strings.TrimSpace(strings.ToLower(turn.Role))
		if role != aiprovider.RoleUser && role != aiprovider.RoleAssistant {
			continue
		}
		kept = append(kept, aiprovider.Message{Role: role, Text: text})
	}

	if len(kept) > aiChatMaxHistoryTurns {
		kept = kept[len(kept)-aiChatMaxHistoryTurns:]
	}

	// Walk back from the newest turn, taking what fits.
	budget := aiChatMaxHistoryChars
	first := len(kept)
	for i := len(kept) - 1; i >= 0; i-- {
		if len(kept[i].Text) > budget {
			break
		}
		budget -= len(kept[i].Text)
		first = i
	}
	return kept[first:]
}

// aiChatProvider builds the provider for one request.
//
// A request that names no provider goes through the Settings router, as the AI
// dock's and the AI Chat page's Claude questions do. A request that names one
// builds it from the request, falling back field by field to the stored
// configuration — which is how the AI Chat page puts a question to a different
// Wintermute backend or agent than Settings configures. The transport itself is the shared harness, so there is one
// implementation of each protocol rather than two.
//
// Credentials and the model are the exceptions to "per question": they are
// never taken from the request. Both keys and the model come from Settings (or
// the environment behind it), so there is one place to set them and every AI
// field in the app answers on the same model.
func aiChatProvider(req aiChatRequest) (aiprovider.Provider, error) {
	switch req.Provider {
	case "":
		// No provider named means "whatever Settings says" — the AI dock, which
		// has no provider control and should not have one. It used to mean
		// Claude, so an install set to Wintermute still sent every docked
		// question to Anthropic and nothing in the UI said so.
		//
		// The router is the same one every other AI field asks through, so it
		// carries the stored backend, model and agent, and honours "auto".
		if activeAIRouter != nil {
			return activeAIRouter.Selected()
		}
		return aiChatClaudeProvider(req)

	case "claude":
		return aiChatClaudeProvider(req)

	case "wintermute":
		cfg := aiprovider.WintermuteConfig{
			URL:     req.Endpoint,
			Token:   storedAICredential("wintermute"),
			Backend: req.Backend,
			Model:   aiChatWintermuteModel(req.Backend),
			// A question asked without an agent is answered from the model's
			// training data rather than from this installation's catalogs, and
			// reads exactly like a grounded answer — so a caller that says
			// nothing about the agent gets the one Settings configured, and
			// only a caller that names one (the AI Chat page, which shows what
			// it will use) overrides it.
			Agent: aiChatRequestAgent(req),
		}
		if cfg.URL == "" {
			cfg.URL = storedWintermuteURL()
		}
		if cfg.URL == "" {
			return nil, fmt.Errorf("no Wintermute server URL: set one in Settings")
		}
		if cfg.Token == "" {
			return nil, fmt.Errorf("no Wintermute client token: set one in Settings")
		}
		if _, err := aiprovider.ValidateEndpoint(cfg.URL); err != nil {
			return nil, err
		}
		return aiprovider.NewWintermute(func() aiprovider.WintermuteConfig { return cfg }), nil

	default:
		return nil, fmt.Errorf("provider must be claude or wintermute")
	}
}

// validatedClaudeBaseURL turns the page's optional endpoint override into an
// API origin. Empty means the SDK default. The host allowlist is kept from the
// previous implementation: this field can retarget the path, not the server.
func validatedClaudeBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	endpoint, err := validatedEndpoint(raw, []string{"api.anthropic.com"})
	if err != nil {
		return "", err
	}
	parsed, err := neturl.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func stringField(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}

func validatedEndpoint(raw string, allowedHosts []string) (string, error) {
	endpoint := strings.TrimSpace(raw)
	parsed, err := neturl.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("endpoint must use https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("endpoint host is required")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("endpoint must not include user info")
	}
	if len(allowedHosts) > 0 && !hostAllowed(parsed.Hostname(), allowedHosts) {
		return "", fmt.Errorf("endpoint host is not allowed")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func hostAllowed(host string, allowedHosts []string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	for _, allowed := range allowedHosts {
		if host == strings.TrimSpace(strings.ToLower(allowed)) {
			return true
		}
	}
	return false
}

func postJSON(endpoint string, headers map[string]string, payload any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	// #nosec G704 -- endpoint is validated against an allowed scheme and host set before reaching this helper.
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// #nosec G704 -- endpoint is validated against an allowed scheme and host set before reaching this helper.
	resp, err := aiProviderHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read provider response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(respBody))
		if len(snippet) > 240 {
			snippet = snippet[:240] + "..."
		}
		if snippet == "" {
			snippet = resp.Status
		}
		return nil, fmt.Errorf("provider error: %s", snippet)
	}

	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("failed to decode provider response: %w", err)
	}
	return data, nil
}
