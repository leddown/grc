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
    /* The credential cards are one short form each: side by side on a wide
       screen rather than a column of full-width cards with a lot of dead space
       to the right of every input. */
    #creds { display:grid; grid-template-columns:repeat(auto-fit, minmax(360px, 1fr)); gap:16px; align-items:start; }
    #creds .cred { margin-bottom:0; }
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
    /* The shared sidebar renders pageui.Nav as a.tab links; without these the
       page inherits the browser's underlined-link default instead of the pill
       every other page shows. */
    .tabs { display:flex; gap:8px; flex-wrap:wrap; margin-bottom:16px; }
    .tab { padding:9px 12px; border-radius:999px; border:1px solid #334155; background:#1e293b; color:#e2e8f0; text-decoration:none; font-size:13px; }
    .tab.active { background:#334155; border-color:#64748b; }
  </style>
</head>
<body>
  <main>
` + pageui.Nav("/settings") + `
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
          <select id="wmBackend" aria-label="Wintermute backend">
            <option value="">Server default backend</option>
          </select>
          <select id="wmModel" aria-label="Wintermute model">
            <option value="">Backend default model</option>
          </select>
          <button id="loadCatalog" class="secondary" type="button">Refresh backends &amp; models</button>
        </div>
        <p class="meta">
          A <em>backend</em> is one model server Wintermute can route to &mdash; a local
          llama.cpp, Ollama or vLLM host, or a cloud vendor it forwards on to. Both lists
          come from the server itself, so a name here is one it actually has.
          <span id="wmCatalogDetail"></span>
        </p>
        <div class="row">
          <select id="wmAgent" aria-label="Wintermute agent">
            <option value="">No agent &mdash; general assistant</option>
          </select>
          <button id="loadAgents" class="secondary" type="button">Refresh agents</button>
        </div>
        <p class="meta">
          An <em>agent</em> is a named set of documents and sources on the Wintermute server.
          Pick the one that can read this installation&rsquo;s catalogs and the documents for
          this work, and questions asked here are answered from them rather than from the
          model&rsquo;s training data. <span id="wmAgentDetail"></span>
        </p>
        <p class="meta" id="wmAgentLink"></p>
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
      blurb: 'Used for every Claude request: the AI Chat gateway, and the NFR Enrichment analyzer. This is the only place it is set.',
      placeholder: 'sk-ant-...',
    },
    wintermute_token: {
      title: 'Wintermute client token',
      blurb: 'Used to reach a Wintermute server, which routes questions to self-hosted models on your network or on to Claude. This is the only place it is set.',
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
    // A stored backend or model is shown before the server has been asked for
    // its lists, so the select carries the saved value even when the lookup is
    // slow or fails — otherwise pressing Save would quietly clear it.
    const backend = prefs['ai.wintermute.backend'] || '';
    const model = prefs['ai.wintermute.model'] || '';
    ensureOption(wmBackend, backend, backend);
    ensureOption(wmModel, model, model);
    wmBackend.value = backend;
    wmModel.value = model;
    const agent = prefs['ai.wintermute.agent'] || '';
    wmAgent.value = agent;
    if (wmURL.value.trim()) {
      loadAgents(agent).catch(function () { /* reported inline */ });
      loadCatalog(backend, model).catch(function () { /* reported inline */ });
    }

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

  const wmCatalogDetail = document.getElementById('wmCatalogDetail');

  // Keeps a select's current value selectable while its list is being rebuilt,
  // and lets a stored value be shown before any list has arrived.
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

  // The backend and model lists come from the Wintermute server rather than
  // being typed in, for the same reason the agent list does: a name that is
  // one character out is not an error anyone sees here — it is a question that
  // fails at ask time, or goes to the server's default and looks like it worked.
  let catalog = { backends: [], models: [], default_backend: '', fallback: '' };

  function renderBackends(selected) {
    const want = selected !== undefined ? selected : wmBackend.value;
    clearOptions(wmBackend);
    wmBackend.options[0].textContent = catalog.default_backend
      ? 'Server default backend \u2014 ' + catalog.default_backend
      : 'Server default backend';
    catalog.backends.forEach(function (backend) {
      const opt = document.createElement('option');
      opt.value = backend.name;
      let label = backend.name;
      const notes = [];
      if (backend.kind) notes.push(backend.kind);
      if (backend.status && backend.status !== 'ok') notes.push(backend.status);
      if (notes.length) label += ' (' + notes.join(', ') + ')';
      opt.textContent = label;
      wmBackend.appendChild(opt);
    });
    // A pinned backend the server no longer has must not be silently dropped:
    // it would look configured here and fail at ask time there.
    ensureOption(wmBackend, want, want + ' \u2014 not on this server');
    wmBackend.value = want || '';
  }

  function renderModels(selected) {
    const want = selected !== undefined ? selected : wmModel.value;
    const backend = wmBackend.value;
    clearOptions(wmModel);
    wmModel.options[0].textContent = backend
      ? 'Default model for ' + backend
      : 'Backend default model';
    catalog.models
      .filter(function (model) { return !backend || model.backend === backend; })
      .forEach(function (model) {
        const opt = document.createElement('option');
        opt.value = model.id;
        let label = model.id;
        const notes = [];
        // Without a backend chosen the same model id can come from more than
        // one of them, so say which reported it.
        if (!backend && model.backend) notes.push(model.backend);
        if (model.params_b) notes.push(model.params_b + 'B');
        if (model.loaded) notes.push('loaded');
        if (notes.length) label += ' (' + notes.join(', ') + ')';
        opt.textContent = label;
        wmModel.appendChild(opt);
      });
    ensureOption(wmModel, want, want + ' \u2014 not listed on this server');
    wmModel.value = want || '';
  }

  async function loadCatalog(selectedBackend, selectedModel) {
    const base = wmURL.value.trim();
    if (!base) {
      wmCatalogDetail.textContent = 'Enter the server URL to list its backends and models.';
      return;
    }
    wmCatalogDetail.textContent = 'Loading backends and models...';
    try {
      const res = await fetch('/api/settings/ai-providers/catalog');
      const data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));

      catalog = {
        backends: data.backends || [],
        models: data.models || [],
        default_backend: data.default_backend || '',
        fallback: data.fallback || '',
      };
      renderBackends(selectedBackend);
      renderModels(selectedModel);

      let detail = catalog.backends.length
        ? catalog.backends.length + ' backend(s), ' + catalog.models.length + ' model(s) on this server.'
        : 'This server has no backends yet — declare one there first.';
      if (catalog.fallback) detail += ' Fallback: ' + catalog.fallback + '.';
      // The model list is fetched separately and is allowed to fail on its own:
      // a backend is still choosable without it.
      if (data.models_error) detail += ' Models could not be listed: ' + data.models_error;
      wmCatalogDetail.textContent = detail;
    } catch (err) {
      wmCatalogDetail.textContent = 'Could not list backends: ' + err.message;
    }
  }

  wmBackend.addEventListener('change', function () {
    // The chosen model belongs to the backend that was chosen before, so keep
    // it only where the new backend serves it too: carrying it over silently
    // would pin a model this backend cannot answer with.
    const current = wmModel.value;
    const served = catalog.models.some(function (model) {
      return model.id === current && (!wmBackend.value || model.backend === wmBackend.value);
    });
    renderModels(served ? current : '');
  });

  document.getElementById('loadCatalog').addEventListener('click', function () {
    loadCatalog().catch(function (err) { wmCatalogDetail.textContent = err.message; });
  });

  const wmAgent = document.getElementById('wmAgent');
  const wmAgentDetail = document.getElementById('wmAgentDetail');
  const wmAgentLink = document.getElementById('wmAgentLink');

  // The agent list comes from the Wintermute server itself rather than being
  // typed in, because a mistyped agent id is the difference between a grounded
  // answer and a confident guess, and there is no way to tell from the answer
  // which happened.
  async function loadAgents(selected) {
    const base = wmURL.value.trim();
    wmAgentLink.textContent = '';
    if (!base) {
      wmAgentDetail.textContent = 'Enter the server URL to list its agents.';
      return;
    }
    wmAgentDetail.textContent = 'Loading agents...';
    try {
      const res = await fetch('/api/settings/ai-providers/agents');
      const data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));

      const agents = data.agents || [];
      const want = selected !== undefined ? selected : wmAgent.value;
      while (wmAgent.options.length > 1) wmAgent.remove(1);
      agents.forEach(function (agent) {
        const opt = document.createElement('option');
        opt.value = agent.id;
        opt.textContent = agent.name + (agent.description ? ' — ' + agent.description : '');
        wmAgent.appendChild(opt);
      });
      wmAgent.value = want || '';
      // A stored agent the server no longer has must not be silently dropped:
      // it would look configured here and answer ungrounded there.
      if (want && wmAgent.value !== want) {
        const opt = document.createElement('option');
        opt.value = want;
        opt.textContent = want + ' — not on this server';
        wmAgent.appendChild(opt);
        wmAgent.value = want;
      }
      wmAgentDetail.textContent = agents.length
        ? agents.length + ' agent(s) on this server.'
        : 'This server has no agents yet — create one there first.';
      renderAgentLink(base);
    } catch (err) {
      wmAgentDetail.textContent = 'Could not list agents: ' + err.message;
      renderAgentLink(base);
    }
  }

  // Documents are uploaded on the Wintermute server, not here: it owns the
  // library, the extraction and the search. This is the way there.
  function renderAgentLink(base) {
    if (!base) return;
    wmAgentLink.textContent = '';
    const link = document.createElement('a');
    link.href = base.replace(/\/+$/, '') + '/#agents';
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.textContent = 'Manage agents and upload documents on Wintermute \u2197';
    wmAgentLink.appendChild(link);
  }

  document.getElementById('loadAgents').addEventListener('click', function () {
    loadAgents().catch(function (err) { wmAgentDetail.textContent = err.message; });
  });

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
          wintermute_agent: wmAgent.value.trim(),
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
        // The dropdown above is the place a backend is chosen; this line is
        // what the server said just now, which is how a pinned backend that
        // has since disappeared shows up.
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
