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
	"grc/internal/pageui"
	"grc/internal/settings"
)

// The Claude and Wintermute protocols, their endpoint rules and their session
// titles live in internal/aiprovider, which this page now asks through.

var aiProviderHTTPClient = &http.Client{Timeout: 90 * time.Second}

// aiChatRequest is one question from the page. It deliberately carries no
// credential: keys and tokens are set once in the Settings page and resolved
// server-side, so a secret never rides in a browser request to this endpoint.
type aiChatRequest struct {
	Provider string `json:"provider"`
	Question string `json:"question"`
	System   string `json:"system_prompt"`
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"`
	// Backend names a wintermuted backend (a local model server, or Claude)
	// for the wintermute provider. Empty means that server's default.
	Backend string `json:"backend"`
	// History is the conversation before Question, oldest first, so a follow-up
	// question can refer to what came before. The client holds the transcript —
	// this endpoint is stateless — and the server bounds what it will accept
	// (see boundedHistory).
	History []aiChatTurn `json:"history"`
	// SessionID continues a conversation the provider itself is holding, from a
	// previous answer's session_id. Wintermute keeps transcripts server-side;
	// when this is set, History is not resent.
	SessionID string `json:"session_id"`
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
      background: #fffaf0;
      color: #5e6672;
      white-space: nowrap;
    }
    .chip.on { border-color: #7fae94; color: #0b5d3b; background: #f0f7f2; }
    .chip.off { border-color: #d7b271; color: #9a6700; background: #fff8e8; }
    /* Transparent so the theme's own link colour stays legible on whichever
       background the page is painted with. */
    a.chip { text-decoration: none; background: transparent; }
    .layout {
      margin-top: 16px;
      display: grid;
      grid-template-columns: minmax(260px, 340px) minmax(0, 1fr);
      gap: 16px;
      align-items: stretch;
      /* Chat is the work; the page fills the viewport so the conversation gets
         the leftover height rather than stopping at a fixed box. */
      height: clamp(460px, calc(100dvh - 250px), 1400px);
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: rgba(255,255,255,0.86);
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
    .side .panel-body { display: flex; flex-direction: column; }
    .field { margin-bottom: 12px; }
    .field.grow { display: flex; flex-direction: column; flex: 1 1 auto; min-height: 120px; margin-bottom: 10px; }
    .field.grow textarea { flex: 1; min-height: 90px; }
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
      background: white;
      color: var(--ink);
      font: inherit;
    }
    textarea { min-height: 120px; resize: vertical; }
    .mono { font-family: "Courier New", Courier, monospace; }
    .status { color: var(--muted); min-height: 22px; }
    .status.warn { color: var(--warn); }
    .actions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-top: 10px;
    }
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
      background: white;
      border: 1px solid var(--line);
    }
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
      background: white;
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
    .notice {
      padding: 10px 12px;
      border: 1px dashed var(--line);
      border-radius: 12px;
      /* Transparent for the same reason as the composer: it sits on a panel the
         global theme repaints, and it contains a <code> chip the theme colours
         for that panel, not for a cream box. */
      background: transparent;
      color: var(--muted);
      font-size: 13px;
      margin-bottom: 12px;
    }
    .usage-panel {
      margin-top: 10px;
      flex: 0 0 auto;
      border: 1px solid var(--line);
      border-radius: 12px;
      background: rgba(255,255,255,0.7);
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
      .layout { grid-template-columns: 1fr; height: auto; }
      .side .panel-body { display: block; }
      .field.grow { display: block; min-height: 0; }
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
        <p>Ask the Anthropic Claude API directly, or a Wintermute server that routes the question to a self-hosted model on your network or on to Claude.</p>
      </div>
      <div class="creds">
        <span id="claudeChip" class="chip">Anthropic key: checking&hellip;</span>
        <span id="wintermuteChip" class="chip">Wintermute token: checking&hellip;</span>
        <a class="chip" href="/settings">Keys &amp; tokens &rarr; Settings</a>
      </div>
    </header>

    <div class="layout">
      <section class="panel side">
        <div class="panel-header"><span>Session</span></div>
        <div class="panel-body">
          <div class="field">
            <label for="provider">Provider</label>
            <select id="provider">
              <option value="claude">Anthropic Claude API</option>
              <option value="wintermute">Wintermute (self-hosted or Claude)</option>
            </select>
          </div>

          <div id="claudeFields">
            <div class="field">
              <label for="claudeModel">Model</label>
              <input id="claudeModel" class="mono" type="text" value="claude-opus-5">
            </div>
            <div class="field">
              <label for="claudeEndpoint">Endpoint (optional)</label>
              <input id="claudeEndpoint" class="mono" type="text" placeholder="https://api.anthropic.com/v1/messages">
            </div>
          </div>

          <div id="wintermuteFields" style="display:none;">
            <div class="field">
              <label for="wintermuteEndpoint">Server URL</label>
              <input id="wintermuteEndpoint" class="mono" type="text" placeholder="http://127.0.0.1:8080">
            </div>
            <div class="field">
              <label for="wintermuteBackend">Backend (optional)</label>
              <input id="wintermuteBackend" class="mono" type="text" placeholder="server default">
            </div>
            <div class="field">
              <label for="wintermuteModel">Model (optional)</label>
              <input id="wintermuteModel" class="mono" type="text" placeholder="backend default">
            </div>
            <div id="wintermuteNotice" class="notice">
              Wintermute runs the turn on your own <code>wintermuted</code> server, which decides whether a
              self-hosted model or Claude answers it.
            </div>
          </div>

          <div class="field grow">
            <label for="systemPrompt">System Prompt (optional)</label>
            <textarea id="systemPrompt" placeholder="You are a security controls assistant..."></textarea>
          </div>

          <div class="actions">
            <button id="usageBtn" class="secondary" type="button">Usage &#9656;</button>
          </div>
          <div id="usagePanel" class="usage-panel" style="display:none;"></div>
        </div>
      </section>

      <section class="panel chat">
        <div class="panel-header">
          <span>Conversation</span>
          <span class="panel-header-actions">
            <span id="contextNote" class="context-note"></span>
            <button id="clearBtn" class="secondary" type="button">Clear</button>
          </span>
        </div>
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
    const provider = document.getElementById('provider');
    const claudeFields = document.getElementById('claudeFields');
    const claudeModel = document.getElementById('claudeModel');
    const claudeEndpoint = document.getElementById('claudeEndpoint');
    const wintermuteFields = document.getElementById('wintermuteFields');
    const wintermuteEndpoint = document.getElementById('wintermuteEndpoint');
    const wintermuteBackend = document.getElementById('wintermuteBackend');
    const wintermuteModel = document.getElementById('wintermuteModel');
    const claudeChip = document.getElementById('claudeChip');
    const wintermuteChip = document.getElementById('wintermuteChip');
    const systemPrompt = document.getElementById('systemPrompt');
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
    const wintermute = { configured: false, token_configured: false };
    // Whether the server has an Anthropic key (from Settings or the
    // environment). Assumed false until the status call answers, so a failed
    // status check says "unknown" rather than promising a key that is not there.
    const claude = { configured: false };

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

    function syncProviderView() {
      const p = provider.value;
      claudeFields.style.display = p === 'claude' ? '' : 'none';
      wintermuteFields.style.display = p === 'wintermute' ? '' : 'none';
    }

    function renderContextNote() {
      const turns = history.length / 2;
      contextNote.textContent = turns ? turns + (turns === 1 ? ' turn of context' : ' turns of context') : '';
    }

    // A Wintermute session is pinned to the server, backend and model it was
    // opened with, so changing any of those has to start a new one. The
    // transcript survives that: it is the client's, and it is sent with the
    // first question of the new session.
    function dropSession() {
      sessionID = '';
    }

    async function refreshWintermuteStatus() {
      try {
        const resp = await fetch('/ai-chat/wintermute/status');
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        wintermute.configured = Boolean(data.configured);
        wintermute.token_configured = Boolean(data.token_configured);
        claude.configured = Boolean(data.claude_configured);
        applyCredentialChips();
        if (!wintermuteEndpoint.value && data.default_endpoint) wintermuteEndpoint.value = data.default_endpoint;
        if (!wintermuteBackend.value && data.default_backend) wintermuteBackend.value = data.default_backend;
        if (!wintermuteModel.value && data.default_model) wintermuteModel.value = data.default_model;
      } catch (_) {
        wintermute.configured = false;
        wintermute.token_configured = false;
        claude.configured = false;
        applyCredentialChips();
      }
    }

    // The credentials live in Settings, so this page only reports whether each
    // provider can answer — an operator should not have to submit a question to
    // find out that nothing is configured.
    function setChip(el, label, ok) {
      el.className = 'chip ' + (ok ? 'on' : 'off');
      el.textContent = label + (ok ? ': configured' : ': not configured');
    }

    function applyCredentialChips() {
      setChip(claudeChip, 'Anthropic key', claude.configured);
      setChip(wintermuteChip, 'Wintermute token', wintermute.token_configured);
    }

    provider.addEventListener('change', () => {
      syncProviderView();
      dropSession();
    });
    syncProviderView();

    for (const field of [wintermuteEndpoint, wintermuteBackend, wintermuteModel]) {
      field.addEventListener('change', dropSession);
    }

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
      // Credentials are install-wide now, so the only thing this page can check
      // is whether the server has one for the chosen provider.
      if (provider.value === 'claude' && !claude.configured) {
        setStatus('No Anthropic API key is configured. Set one in Settings.', true);
        return;
      }
      if (provider.value === 'wintermute') {
        if (!wintermuteEndpoint.value.trim() && !wintermute.configured) {
          setStatus('Wintermute server URL is required.', true);
          return;
        }
        if (!wintermute.token_configured) {
          setStatus('No Wintermute client token is configured. Set one in Settings.', true);
          return;
        }
      }

      const isClaude = provider.value === 'claude';
      const payload = {
        provider: provider.value,
        question: text,
        system_prompt: systemPrompt.value.trim(),
        model: isClaude ? claudeModel.value.trim() : wintermuteModel.value.trim(),
        endpoint: isClaude ? claudeEndpoint.value.trim() : wintermuteEndpoint.value.trim(),
        backend: wintermuteBackend.value.trim(),
        // A resumed Wintermute session already holds the transcript; sending it
        // again would replay every earlier turn into the same session.
        history: sessionID ? [] : history,
        session_id: sessionID
      };

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
        let served = data.provider || provider.value;
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

    refreshWintermuteStatus();

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

// aiChatWintermuteStatus reports the server-side Wintermute defaults so the
// page can prefill them, and whether a credential is available for each
// provider so the page can say which one can answer before a question is sent.
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
		"configured":        endpoint != "",
		"token_configured":  storedAICredential("wintermute") != "",
		"claude_configured": storedAICredential("claude") != "",
		"default_endpoint":  endpoint,
		"default_backend":   aiChatPreference(settings.PrefWintermuteBackend, "WINTERMUTE_BACKEND"),
		"default_model":     aiChatPreference(settings.PrefWintermuteModel, "WINTERMUTE_MODEL"),
	})
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
	req.Model = strings.TrimSpace(req.Model)
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

	resp, err := provider.Ask(c.Request.Context(), aiprovider.Request{
		System:    req.System,
		History:   boundedHistory(req.History),
		Prompt:    req.Question,
		SessionID: req.SessionID,
		Model:     req.Model,
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
// This page is the one place a provider is chosen per question rather than per
// install: a reviewer may want to put a single question to a different model or
// Wintermute backend. So it does not use the Settings router — it builds a
// provider from the request, falling back field by field to the stored
// configuration. The transport itself is the shared harness, so there is one
// implementation of each protocol rather than two.
//
// Credentials are the exception to "per question": they are never taken from
// the request. Both keys come from Settings (or the environment behind it), so
// there is one place to set them and no path that carries a secret through the
// browser.
func aiChatProvider(req aiChatRequest) (aiprovider.Provider, error) {
	switch req.Provider {
	case "", "claude":
		key := storedAICredential("claude")
		if key == "" {
			return nil, fmt.Errorf("no Anthropic API key: set one in Settings")
		}
		// The endpoint override stays restricted to Anthropic's own host.
		endpoint, err := validatedClaudeBaseURL(req.Endpoint)
		if err != nil {
			return nil, err
		}
		return aiprovider.NewClaude(func() string { return key }, req.Model).WithBaseURL(endpoint), nil

	case "wintermute":
		cfg := aiprovider.WintermuteConfig{
			URL:     req.Endpoint,
			Token:   storedAICredential("wintermute"),
			Backend: req.Backend,
			Model:   req.Model,
		}
		if cfg.URL == "" {
			cfg.URL = storedWintermuteURL()
		}
		if cfg.URL == "" {
			return nil, fmt.Errorf("no Wintermute server URL: enter one above, or set one in Settings")
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
