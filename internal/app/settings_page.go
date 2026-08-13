package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"grc/internal/pageui"
)

// settingsPage renders the admin Settings page: one place to set the AI
// provider credentials every AI field in the app shares.
//
// The credentials are write-only here. A stored key is never sent back to the
// browser — the page shows only whether one is configured, which source is
// active, and the last four characters so an operator can tell two keys apart.
func settingsPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Settings · GRC</title>
  <style>
    body { margin:0; font-family: Arial, sans-serif; background:#0b1220; color:#e5e7eb; }
    main { max-width: 1320px; margin: 24px auto; padding: 24px; background:#111827; border:1px solid #334155; border-radius:16px; }
    h1 { margin:0 0 10px; }
    h2 { margin:24px 0 8px; font-size:16px; }
    p { color:#94a3b8; }
    input, button, select { font:inherit; }
    input, select { padding:8px 10px; border:1px solid #334155; border-radius:10px; background:#0f172a; color:#e2e8f0; }
    select { min-width:320px; }
    button { padding:8px 12px; border-radius:10px; border:1px solid #334155; background:#1e293b; color:#e2e8f0; cursor:pointer; }
    button.danger { border-color:#7f1d1d; }
    button:disabled { opacity:.5; cursor:not-allowed; }
    .cred { border:1px solid #334155; border-radius:12px; padding:16px; margin-bottom:16px; background:#0f172a; }
    .cred h3 { margin:0 0 4px; font-size:15px; }
    .row { display:flex; gap:8px; flex-wrap:wrap; align-items:center; margin-top:10px; }
    .row input { flex:1; min-width:280px; }
    .meta { font-size:12px; color:#94a3b8; margin:4px 0 0; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    .pill { display:inline-block; padding:2px 8px; border-radius:999px; font-size:12px; border:1px solid #334155; }
    .pill.on { background:#064e3b; border-color:#065f46; color:#a7f3d0; }
    .pill.off { background:#3f1d1d; border-color:#7f1d1d; color:#fecaca; }
    .pill.env { background:#1e3a5f; border-color:#1d4ed8; color:#bfdbfe; }
    .status { margin:10px 0; min-height:20px; color:#93c5fd; }
    .status.error { color:#fca5a5; }
    .notice { border:1px solid #7f1d1d; background:#3f1d1d; color:#fecaca; padding:12px; border-radius:10px; margin-bottom:16px; }
    .keyring { font-size:12px; color:#64748b; margin-top:24px; }
  </style>
</head>
<body>
` + pageui.Nav("/settings") + `
  <main>
    <h1>Settings</h1>
    <p>
      Credentials for the AI features. A key set here is used by every AI field in
      the app &mdash; AI Chat and NFR Enrichment today &mdash; and takes effect
      immediately, with no restart.
    </p>

    <div id="notice" class="notice" hidden></div>
    <div id="status" class="status"></div>
    <div id="creds"></div>

    <h2>Where questions are answered</h2>
    <p>
      Claude sends questions to Anthropic. Wintermute sends them to a server on
      your network, which routes each one to a self-hosted model or on to Claude
      &mdash; so a question can be answered without leaving the network.
    </p>

    <div class="cred">
      <h3>AI provider <span id="activePill" class="pill off">unknown</span></h3>
      <div class="row">
        <select id="provider" aria-label="AI provider">
          <option value="auto">Auto &mdash; prefer Wintermute, fall back to Claude</option>
          <option value="claude">Claude only</option>
          <option value="wintermute">Wintermute only (never leaves the network)</option>
        </select>
      </div>
      <p class="meta" id="providerDetail"></p>

      <div id="wintermuteFields">
        <div class="row">
          <input id="wmURL" type="text" class="mono" placeholder="http://wintermute.local:8080" aria-label="Wintermute server URL">
        </div>
        <div class="row">
          <input id="wmBackend" type="text" class="mono" placeholder="backend (blank = server default)" aria-label="Wintermute backend">
          <input id="wmModel" type="text" class="mono" placeholder="model (blank = backend default)" aria-label="Wintermute model">
        </div>
        <div class="row">
          <button id="saveProvider">Save</button>
          <button id="testProvider">Test connection</button>
        </div>
        <p class="meta" id="probeDetail"></p>
        <p class="meta" id="backendList"></p>
      </div>
    </div>

    <p class="keyring" id="keyring"></p>
  </main>

<script>
(function () {
  const credsEl = document.getElementById('creds');
  const statusEl = document.getElementById('status');
  const noticeEl = document.getElementById('notice');
  const keyringEl = document.getElementById('keyring');
  let storageAvailable = true;

  // Labels live here rather than in the API so the server never has to care
  // about presentation; the names themselves are the stable contract.
  const LABELS = {
    anthropic_api_key: {
      title: 'Anthropic API key',
      blurb: 'Used for Claude requests: AI Chat, and the NFR Enrichment analyzer.',
      placeholder: 'sk-ant-...',
    },
    wintermute_token: {
      title: 'Wintermute client token',
      blurb: 'Used to reach a Wintermute server, which routes questions to self-hosted models on your network or on to Claude.',
      placeholder: 'Token from wintermuted -add-client',
    },
  };

  function setStatus(message, isError) {
    statusEl.textContent = message || '';
    statusEl.classList.toggle('error', Boolean(isError));
  }

  function esc(value) {
    return String(value == null ? '' : value).replace(/[&<>"']/g, function (ch) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch];
    });
  }

  function pill(cred) {
    if (cred.origin === 'settings') return '<span class="pill on">configured here</span>';
    if (cred.origin === 'environment') return '<span class="pill env">from ' + esc(cred.env_var) + '</span>';
    return '<span class="pill off">not configured</span>';
  }

  function meta(cred) {
    const parts = [];
    if (cred.hint) parts.push('key ends ' + esc(cred.hint));
    if (cred.origin === 'settings' && cred.updated_by) {
      parts.push('set by ' + esc(cred.updated_by) + (cred.updated_at ? ' on ' + esc(cred.updated_at) : ''));
    }
    // Clearing a stored key does not necessarily disable the feature: say so
    // before someone presses the button.
    if (cred.origin === 'settings' && cred.env_available) {
      parts.push('clearing this falls back to ' + esc(cred.env_var));
    }
    if (cred.origin === 'environment') {
      parts.push('set a key here to take over from the environment');
    }
    return parts.join(' &middot; ');
  }

  function render(creds) {
    credsEl.innerHTML = creds.map(function (cred) {
      const label = LABELS[cred.name] || { title: cred.name, blurb: '', placeholder: '' };
      return '' +
        '<div class="cred" data-name="' + esc(cred.name) + '">' +
          '<h3>' + esc(label.title) + ' ' + pill(cred) + '</h3>' +
          '<p class="meta">' + esc(label.blurb) + '</p>' +
          '<div class="row">' +
            '<input type="password" autocomplete="off" spellcheck="false" ' +
              'placeholder="' + esc(label.placeholder) + '" aria-label="' + esc(label.title) + '">' +
            '<button class="save"' + (storageAvailable ? '' : ' disabled') + '>Save</button>' +
            '<button class="danger clear"' + (cred.origin === 'settings' ? '' : ' disabled') + '>Clear</button>' +
          '</div>' +
          '<p class="meta">' + meta(cred) + '</p>' +
        '</div>';
    }).join('');
  }

  async function load() {
    try {
      const res = await fetch('/api/settings/ai-credentials');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();
      storageAvailable = data.storage_available !== false;
      if (!storageAvailable) {
        noticeEl.hidden = false;
        noticeEl.textContent =
          'Credentials cannot be stored: no master key is available. Set GRC_SECRET_KEY, ' +
          'or give the service a writable state directory. Keys already set in the ' +
          'environment still work.';
      }
      keyringEl.textContent = 'Credential encryption: ' + (data.keyring || 'unknown');
      render(data.credentials || []);
    } catch (err) {
      setStatus('Could not load settings: ' + err.message, true);
    }
  }

  async function send(name, method, body) {
    const res = await fetch('/api/settings/ai-credentials/' + encodeURIComponent(name), {
      method: method,
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    });
    const data = await res.json().catch(function () { return {}; });
    if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
    return data;
  }

  credsEl.addEventListener('click', async function (event) {
    const button = event.target.closest('button');
    if (!button) return;
    const card = button.closest('.cred');
    const name = card.getAttribute('data-name');
    const input = card.querySelector('input');

    try {
      if (button.classList.contains('save')) {
        const value = input.value.trim();
        if (!value) { setStatus('Enter a key before saving.', true); return; }
        await send(name, 'PUT', { value: value });
        // Never leave the secret sitting in the DOM after a successful save.
        input.value = '';
        setStatus('Saved. It is in use now — no restart needed.');
      } else if (button.classList.contains('clear')) {
        if (!window.confirm('Clear the stored ' + name + '?')) return;
        await send(name, 'DELETE');
        input.value = '';
        setStatus('Cleared.');
      } else {
        return;
      }
      await load();
    } catch (err) {
      setStatus(err.message, true);
    }
  });

  // --- provider routing -----------------------------------------------------

  const providerEl = document.getElementById('provider');
  const wmURL = document.getElementById('wmURL');
  const wmBackend = document.getElementById('wmBackend');
  const wmModel = document.getElementById('wmModel');
  const activePill = document.getElementById('activePill');
  const providerDetail = document.getElementById('providerDetail');
  const probeDetail = document.getElementById('probeDetail');
  const backendList = document.getElementById('backendList');

  function renderProviders(data) {
    const prefs = data.preferences || {};
    providerEl.value = prefs['ai.provider'] || 'claude';
    wmURL.value = prefs['ai.wintermute.url'] || '';
    wmBackend.value = prefs['ai.wintermute.backend'] || '';
    wmModel.value = prefs['ai.wintermute.model'] || '';

    const st = data.status;
    if (!st) {
      activePill.className = 'pill off';
      activePill.textContent = 'unknown';
      providerDetail.textContent = '';
      return;
    }
    if (st.active) {
      activePill.className = 'pill on';
      activePill.textContent = 'answering with ' + st.active;
    } else {
      activePill.className = 'pill off';
      activePill.textContent = 'not configured';
    }
    providerDetail.textContent = st.detail || '';
  }

  async function loadProviders() {
    try {
      const res = await fetch('/api/settings/ai-providers');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      renderProviders(await res.json());
    } catch (err) {
      setStatus('Could not load provider settings: ' + err.message, true);
    }
  }

  document.getElementById('saveProvider').addEventListener('click', async function () {
    try {
      const res = await fetch('/api/settings/ai-providers', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: providerEl.value,
          wintermute_url: wmURL.value.trim(),
          wintermute_backend: wmBackend.value.trim(),
          wintermute_model: wmModel.value.trim(),
        }),
      });
      const data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
      renderProviders(data);
      setStatus('Saved. In use now — no restart needed.');
    } catch (err) {
      setStatus(err.message, true);
    }
  });

  document.getElementById('testProvider').addEventListener('click', async function () {
    probeDetail.textContent = 'Testing...';
    backendList.textContent = '';
    try {
      const res = await fetch('/api/settings/ai-providers/test', { method: 'POST' });
      const probe = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(probe.error || ('HTTP ' + res.status));
      probeDetail.textContent = (probe.ok ? '✓ ' : '✗ ') + (probe.detail || '');
      if (probe.backends && probe.backends.length) {
        // Showing the names lets an operator copy one into the backend field
        // rather than guessing at what the server calls its models.
        let line = 'Backends: ' + probe.backends.join(', ');
        if (probe.default_backend) line += ' · server default: ' + probe.default_backend;
        if (probe.fallback) line += ' · fallback: ' + probe.fallback;
        backendList.textContent = line;
      }
    } catch (err) {
      probeDetail.textContent = '✗ ' + err.message;
    }
  });

  load();
  loadProviders();
})();
</script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
