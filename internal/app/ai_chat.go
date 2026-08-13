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

type aiChatRequest struct {
	Provider string `json:"provider"`
	Question string `json:"question"`
	System   string `json:"system_prompt"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"`
	// Backend names a wintermuted backend (a local model server, or Claude)
	// for the wintermute provider. Empty means that server's default.
	Backend string `json:"backend"`
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
    .layout {
      margin-top: 18px;
      display: grid;
      grid-template-columns: minmax(320px, 0.9fr) minmax(0, 1.1fr);
      gap: 16px;
      align-items: start;
    }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: rgba(255,255,255,0.86);
      overflow: hidden;
    }
    .panel-header {
      padding: 14px 16px;
      border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
      font-family: Arial, sans-serif;
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .panel-body { padding: 16px; }
    .field { margin-bottom: 12px; }
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
      max-height: 62vh;
      overflow: auto;
      display: grid;
      gap: 10px;
    }
    .msg {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: white;
      padding: 10px 12px;
      white-space: pre-wrap;
      word-break: break-word;
    }
    .msg.user { border-left: 4px solid #8b3d2e; }
    .msg.ai { border-left: 4px solid #0b5d3b; }
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
      background: #fffaf0;
      color: var(--muted);
      font-size: 14px;
      margin-bottom: 14px;
    }
    .usage-panel {
      margin-top: 10px;
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
      .layout { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/ai-chat") + `
    <h1>AI Chat Gateway</h1>
    <p>Anthropic Claude API (key-based), or a Wintermute server that routes the question to a self-hosted model or on to Claude.</p>

    <div class="layout">
      <section class="panel">
        <div class="panel-header">Connection + Prompt</div>
        <div class="panel-body">
          <div id="wintermuteNotice" class="notice" style="display:none;">
            Wintermute runs the turn on your own <code>wintermuted</code> server, which decides whether a
            self-hosted model or Claude answers it. Set <code>WINTERMUTE_URL</code> and
            <code>WINTERMUTE_TOKEN</code> on this server to prefill these fields.
          </div>
          <form id="chatForm">
            <div class="field">
              <label for="provider">Provider</label>
              <select id="provider">
                <option value="claude">Anthropic Claude API</option>
                <option value="wintermute">Wintermute (self-hosted or Claude)</option>
              </select>
            </div>

            <div id="claudeFields">
              <div class="field">
                <label for="claudeApiKey">API Key</label>
                <input id="claudeApiKey" type="password" autocomplete="off" placeholder="Paste Anthropic API key">
              </div>
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
                <label for="wintermuteToken">Client Token</label>
                <input id="wintermuteToken" type="password" autocomplete="off" placeholder="Token from wintermuted -add-client">
              </div>
              <div class="field">
                <label for="wintermuteBackend">Backend (optional)</label>
                <input id="wintermuteBackend" class="mono" type="text" placeholder="server default">
              </div>
              <div class="field">
                <label for="wintermuteModel">Model (optional)</label>
                <input id="wintermuteModel" class="mono" type="text" placeholder="backend default">
              </div>
            </div>

            <div class="field">
              <label for="systemPrompt">System Prompt (optional)</label>
              <textarea id="systemPrompt" placeholder="You are a security controls assistant..."></textarea>
            </div>
            <div class="field">
              <label for="question">Question</label>
              <textarea id="question" required placeholder="Ask your question here"></textarea>
            </div>

            <div class="actions">
              <button id="sendBtn" type="submit">Ask</button>
              <button id="clearBtn" class="secondary" type="button">Clear Chat</button>
              <button id="usageBtn" class="secondary" type="button">Usage &#9656;</button>
            </div>
            <div id="usagePanel" class="usage-panel" style="display:none;"></div>
            <div id="status" class="status"></div>
          </form>
        </div>
      </section>

      <section class="panel">
        <div class="panel-header">Conversation</div>
        <div class="panel-body">
          <div id="chatBox" class="chat-box">
            <div class="msg">
              <div class="meta">System</div>
              Responses will appear here after you submit a question.
            </div>
          </div>
        </div>
      </section>
    </div>
  </main>

  <script>
    const provider = document.getElementById('provider');
    const claudeFields = document.getElementById('claudeFields');
    const claudeApiKey = document.getElementById('claudeApiKey');
    const claudeModel = document.getElementById('claudeModel');
    const claudeEndpoint = document.getElementById('claudeEndpoint');
    const wintermuteFields = document.getElementById('wintermuteFields');
    const wintermuteNotice = document.getElementById('wintermuteNotice');
    const wintermuteEndpoint = document.getElementById('wintermuteEndpoint');
    const wintermuteToken = document.getElementById('wintermuteToken');
    const wintermuteBackend = document.getElementById('wintermuteBackend');
    const wintermuteModel = document.getElementById('wintermuteModel');
    const systemPrompt = document.getElementById('systemPrompt');
    const question = document.getElementById('question');
    const chatForm = document.getElementById('chatForm');
    const sendBtn = document.getElementById('sendBtn');
    const clearBtn = document.getElementById('clearBtn');
    const statusEl = document.getElementById('status');
    const chatBox = document.getElementById('chatBox');
    const wintermute = { configured: false, token_configured: false };
    // Whether the server already has an Anthropic key (from Settings or the
    // environment). Assumed false until the status call answers, so a failed
    // status check falls back to demanding a key rather than submitting one
    // the server cannot fulfil.
    const claude = { configured: false };

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
      wintermuteNotice.style.display = p === 'wintermute' ? '' : 'none';
    }

    async function refreshWintermuteStatus() {
      try {
        const resp = await fetch('/ai-chat/wintermute/status');
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        wintermute.configured = Boolean(data.configured);
        wintermute.token_configured = Boolean(data.token_configured);
        claude.configured = Boolean(data.claude_configured);
        applyCredentialHints();
        if (!wintermuteEndpoint.value && data.default_endpoint) wintermuteEndpoint.value = data.default_endpoint;
        if (!wintermuteBackend.value && data.default_backend) wintermuteBackend.value = data.default_backend;
        if (!wintermuteModel.value && data.default_model) wintermuteModel.value = data.default_model;
      } catch (_) {
        wintermute.configured = false;
        wintermute.token_configured = false;
        claude.configured = false;
        applyCredentialHints();
      }
    }

    // Tells the operator the key fields are optional once a credential is
    // configured server-side, so an empty field does not read as "broken".
    function applyCredentialHints() {
      claudeApiKey.placeholder = claude.configured
        ? 'Using the key from Settings — paste one only to override it'
        : 'Paste Anthropic API key';
      wintermuteToken.placeholder = wintermute.token_configured
        ? 'Using the token from Settings — paste one only to override it'
        : 'Token from wintermuted -add-client';
    }

    provider.addEventListener('change', syncProviderView);
    syncProviderView();

    const query = new URLSearchParams(window.location.search);
    const prefilledQuestion = String(query.get('q') || '').trim();
    if (prefilledQuestion && !question.value.trim()) {
      question.value = prefilledQuestion;
    }

    clearBtn.addEventListener('click', () => {
      chatBox.innerHTML =
        '<div class="msg"><div class="meta">System</div>Responses will appear here after you submit a question.</div>';
      statusEl.textContent = '';
    });

    chatForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      const text = question.value.trim();
      if (!text) {
        statusEl.textContent = 'Question is required.';
        statusEl.className = 'status warn';
        return;
      }
      // Only block when the server has no credential either. A key set in the
      // Settings page (or inherited from the environment) is applied
      // server-side, so requiring one in this field would refuse a question
      // the server can answer.
      if (provider.value === 'claude' && !claudeApiKey.value.trim() && !claude.configured) {
        statusEl.textContent = 'Anthropic API key is required. Paste one here, or set one in Settings to use it everywhere.';
        statusEl.className = 'status warn';
        return;
      }
      if (provider.value === 'wintermute') {
        if (!wintermuteEndpoint.value.trim() && !wintermute.configured) {
          statusEl.textContent = 'Wintermute server URL is required.';
          statusEl.className = 'status warn';
          return;
        }
        if (!wintermuteToken.value.trim() && !wintermute.token_configured) {
          statusEl.textContent = 'Wintermute client token is required.';
          statusEl.className = 'status warn';
          return;
        }
      }

      const isClaude = provider.value === 'claude';
      const payload = {
        provider: provider.value,
        api_key: isClaude ? claudeApiKey.value.trim() : wintermuteToken.value.trim(),
        question: text,
        system_prompt: systemPrompt.value.trim(),
        model: isClaude ? claudeModel.value.trim() : wintermuteModel.value.trim(),
        endpoint: isClaude ? claudeEndpoint.value.trim() : wintermuteEndpoint.value.trim(),
        backend: wintermuteBackend.value.trim()
      };

      appendMessage('User', text);
      statusEl.textContent = 'Waiting for model response...';
      statusEl.className = 'status';
      sendBtn.disabled = true;

      try {
        const resp = await fetch('/ai-chat/ask', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        appendMessage('Assistant', data.answer || '(empty answer)');
        question.value = '';
        let served = data.provider || provider.value;
        if (data.backend) served += ' / ' + data.backend;
        if (data.model) served += ' (' + data.model + ')';
        statusEl.textContent = 'Response received from ' + served + '.';
      } catch (err) {
        appendMessage('Assistant', 'Error: ' + (err.message || 'request failed'));
        statusEl.textContent = 'Request failed.';
        statusEl.className = 'status warn';
      } finally {
        sendBtn.disabled = false;
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
// provider so the page knows the key fields may be left blank.
//
// The credentials themselves are never returned — only whether one exists.
// Availability comes from storedAICredential, so it covers a key set in the
// Settings page as well as one inherited from the environment: checking only
// the environment here would leave the page refusing to submit a question the
// server could actually answer.
func aiChatWintermuteStatus(c *gin.Context) {
	endpoint := strings.TrimSpace(os.Getenv("WINTERMUTE_URL"))
	c.JSON(http.StatusOK, gin.H{
		"configured":        endpoint != "",
		"token_configured":  storedAICredential("wintermute") != "",
		"claude_configured": storedAICredential("claude") != "",
		"default_endpoint":  endpoint,
		"default_backend":   strings.TrimSpace(os.Getenv("WINTERMUTE_BACKEND")),
		"default_model":     strings.TrimSpace(os.Getenv("WINTERMUTE_MODEL")),
	})
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
	var req aiChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	req.Question = strings.TrimSpace(req.Question)
	req.System = strings.TrimSpace(req.System)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Model = strings.TrimSpace(req.Model)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Backend = strings.TrimSpace(req.Backend)

	if req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}

	// A request that carries no credential falls back to the one stored in
	// Settings, so the key does not have to be pasted into the browser on every
	// visit. An explicit key in the request still wins, which keeps the page
	// usable for trying a different key without changing the install-wide one.
	if req.APIKey == "" {
		req.APIKey = storedAICredential(req.Provider)
	}

	provider, err := aiChatProvider(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := provider.Ask(c.Request.Context(), aiprovider.Request{
		System:    req.System,
		Prompt:    req.Question,
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
		"provider": resp.Provider,
		"answer":   resp.Text,
		"backend":  resp.Backend,
		"model":    resp.Model,
	})
}

// aiChatMaxTokens bounds one chat answer.
const aiChatMaxTokens = 4096

// aiChatProvider builds the provider for one request.
//
// This page is the one place a provider is chosen per question rather than per
// install: a reviewer may want to put a single question to a different model,
// or try a key before saving it. So it does not use the Settings router —
// it builds a provider from the request, falling back field by field to the
// stored configuration. The transport itself is the shared harness, so there
// is one implementation of each protocol rather than two.
func aiChatProvider(req aiChatRequest) (aiprovider.Provider, error) {
	switch req.Provider {
	case "", "claude":
		key := req.APIKey
		if key == "" {
			key = storedAICredential("claude")
		}
		if key == "" {
			return nil, fmt.Errorf("no Anthropic API key: paste one above, or set one in Settings to use it everywhere")
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
			Token:   req.APIKey,
			Backend: req.Backend,
			Model:   req.Model,
		}
		if cfg.URL == "" {
			cfg.URL = storedWintermuteURL()
		}
		if cfg.Token == "" {
			cfg.Token = storedAICredential("wintermute")
		}
		if cfg.URL == "" {
			return nil, fmt.Errorf("no Wintermute server URL: enter one above, or set one in Settings")
		}
		if cfg.Token == "" {
			return nil, fmt.Errorf("no Wintermute client token: enter one above, or set one in Settings")
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
