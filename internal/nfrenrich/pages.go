package nfrenrich

import "carelockconsulting/internal/pageui"

// pageHTML renders the review UI.
//
// The layout puts the citation next to the suggestion rather than behind a
// disclosure, because the reviewer's actual job is comparing the two: a
// proposal you accept without reading the quote it rests on is the failure mode
// this whole module is built to prevent, and a UI that makes the evidence one
// click away is a UI that gets clicked past.
func pageHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>NFR Enrichment · CareLock Consulting</title>
  <style>
    :root {
      color-scheme: light;
      --panel: rgba(255,252,246,0.94);
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --accent: #8b3d2e;
      --good: #0b5d3b;
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
    h1 { margin: 0 0 8px; font-size: clamp(2rem, 4vw, 3.2rem); line-height: 0.95; letter-spacing: -0.03em; }
    h2 { font-size: 1.1rem; margin: 0; }
    p { color: var(--muted); }
    .layout { margin-top: 18px; display: grid; grid-template-columns: minmax(300px, 0.8fr) minmax(0, 1.2fr); gap: 16px; align-items: start; }
    .panel { border: 1px solid rgba(215,206,191,0.9); border-radius: 18px; background: rgba(255,255,255,0.86); overflow: hidden; }
    .panel-header {
      padding: 14px 16px; border-bottom: 1px solid rgba(215,206,191,0.9);
      background: rgba(236,227,210,0.55);
      font-family: Arial, sans-serif; font-size: 12px; letter-spacing: 0.08em;
      text-transform: uppercase; color: var(--muted);
      display: flex; justify-content: space-between; align-items: center; gap: 8px;
    }
    .panel-body { padding: 16px; }
    .field { margin-bottom: 12px; }
    .field label {
      display: block; margin-bottom: 6px; font-size: 12px; letter-spacing: 0.08em;
      text-transform: uppercase; color: var(--muted); font-family: Arial, sans-serif; font-weight: 700;
    }
    input, select, textarea {
      width: 100%; padding: 10px 12px; border-radius: 12px; border: 1px solid var(--line);
      background: white; color: var(--ink); font: inherit;
    }
    textarea { min-height: 110px; resize: vertical; }
    .mono { font-family: "Courier New", Courier, monospace; font-size: 13px; }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
    button {
      padding: 10px 14px; border-radius: 999px; border: 0; background: #dfc4b4;
      color: var(--ink); font: inherit; cursor: pointer;
    }
    button.secondary { background: white; border: 1px solid var(--line); }
    button.danger { background: #f0d3cc; }
    button[disabled] { opacity: 0.5; cursor: not-allowed; }
    .status { color: var(--muted); min-height: 22px; font-family: Arial, sans-serif; font-size: 13px; }
    .status.warn { color: var(--warn); }
    .doc, .proposal {
      border: 1px solid var(--line); border-radius: 14px; background: white;
      padding: 12px 14px; margin-bottom: 12px;
    }
    .doc-title { font-weight: 700; }
    .meta {
      color: var(--muted); font-family: Arial, sans-serif; font-size: 12px;
      letter-spacing: 0.04em; margin-top: 4px;
    }
    .suggestion {
      border-left: 4px solid var(--good); background: #f7fbf8;
      padding: 10px 12px; border-radius: 8px; white-space: pre-wrap; margin: 10px 0;
    }
    .citation {
      border-left: 4px solid var(--accent); background: #fdf6f3;
      padding: 8px 12px; border-radius: 8px; margin: 8px 0; font-size: 14px;
    }
    .citation .where {
      font-family: Arial, sans-serif; font-size: 11px; text-transform: uppercase;
      letter-spacing: 0.06em; color: var(--muted); margin-bottom: 4px;
    }
    .citation blockquote { margin: 0; font-style: italic; }
    .rationale { color: var(--muted); font-size: 14px; margin: 8px 0; }
    .pill {
      display: inline-block; padding: 2px 10px; border-radius: 999px; font-size: 11px;
      font-family: Arial, sans-serif; text-transform: uppercase; letter-spacing: 0.06em;
      border: 1px solid var(--line); background: #f3ece0; color: var(--muted);
    }
    .pill.pending { background: #fff6e0; color: var(--warn); }
    .pill.accepted { background: #e6f3ec; color: var(--good); }
    .pill.rejected { background: #f6e7e3; color: var(--accent); }
    .notice {
      padding: 10px 12px; border: 1px dashed var(--line); border-radius: 12px;
      background: #fffaf0; color: var(--muted); font-size: 14px; margin-bottom: 14px;
    }
    .empty { color: var(--muted); font-style: italic; }
    @media (max-width: 1024px) { .layout { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/nfr-enrichment") + `
    <h1>NFR Enrichment</h1>
    <p>Upload or link a security document, analyse it against the Security NFR catalog, and review what the model proposes. Nothing reaches the catalog until you accept it.</p>

    <div id="configNotice" class="notice" style="display:none;"></div>

    <div class="layout">
      <section class="panel">
        <div class="panel-header"><h2>Source documents</h2></div>
        <div class="panel-body">
          <div class="field">
            <label for="docURL">Add by URL</label>
            <input id="docURL" class="mono" type="text" placeholder="https://example.com/security-standard">
          </div>
          <div class="field">
            <label for="docTitle">Title (optional)</label>
            <input id="docTitle" type="text" placeholder="derived from the document">
          </div>
          <div class="actions">
            <button id="addURLBtn" type="button">Fetch URL</button>
          </div>

          <div class="field" style="margin-top:16px;">
            <label for="docFile">Or upload a file (text, Markdown or HTML)</label>
            <input id="docFile" type="file" accept=".txt,.md,.markdown,.html,.htm,text/plain,text/markdown,text/html">
          </div>
          <div class="actions">
            <button id="uploadBtn" class="secondary" type="button">Upload</button>
          </div>

          <div id="docStatus" class="status"></div>
          <div id="docList" style="margin-top:14px;"></div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-header">
          <h2>Proposals</h2>
          <select id="statusFilter" style="width:auto;padding:6px 10px;font-size:12px;">
            <option value="pending">Pending</option>
            <option value="">All</option>
            <option value="accepted">Accepted</option>
            <option value="rejected">Rejected</option>
            <option value="superseded">Superseded</option>
          </select>
        </div>
        <div class="panel-body">
          <div id="proposalStatus" class="status"></div>
          <div id="proposalList"></div>
        </div>
      </section>
    </div>
  </main>

  <script>
    const docURL = document.getElementById('docURL');
    const docTitle = document.getElementById('docTitle');
    const docFile = document.getElementById('docFile');
    const addURLBtn = document.getElementById('addURLBtn');
    const uploadBtn = document.getElementById('uploadBtn');
    const docStatus = document.getElementById('docStatus');
    const docList = document.getElementById('docList');
    const proposalStatus = document.getElementById('proposalStatus');
    const proposalList = document.getElementById('proposalList');
    const statusFilter = document.getElementById('statusFilter');
    const configNotice = document.getElementById('configNotice');

    let fields = ['implementation'];
    let configured = false;

    function esc(v) {
      return String(v == null ? '' : v)
        .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;').replaceAll("'", '&#39;');
    }

    function setStatus(el, text, warn) {
      el.textContent = text || '';
      el.className = warn ? 'status warn' : 'status';
    }

    async function api(url, options) {
      const resp = await fetch(url, options);
      const data = await resp.json().catch(() => ({}));
      if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
      return data;
    }

    async function loadDocuments() {
      try {
        const data = await api('/nfr-enrichment/documents');
        configured = Boolean(data.configured);
        fields = data.fields && data.fields.length ? data.fields : fields;

        if (!configured) {
          configNotice.style.display = '';
          configNotice.textContent =
            'Documents can be added and proposals reviewed, but analysis needs an Anthropic API key: set ANTHROPIC_API_KEY and restart.';
        } else {
          configNotice.style.display = '';
          configNotice.textContent = 'Analysis runs on ' + (data.model || 'the configured model') +
            '. Retrieval shortlists sections first, so most requirements are never sent to the model.';
        }

        renderDocuments(data.documents || []);
      } catch (err) {
        setStatus(docStatus, 'Could not load documents: ' + err.message, true);
      }
    }

    function renderDocuments(docs) {
      if (!docs.length) {
        docList.innerHTML = '<div class="empty">No source documents yet.</div>';
        return;
      }
      docList.innerHTML = docs.map(function (d) {
        const where = d.origin === 'url'
          ? '<a href="' + esc(d.url) + '" rel="noreferrer noopener" target="_blank">' + esc(d.url) + '</a>'
          : esc(d.filename || 'uploaded file');
        const fieldOptions = fields.map(function (f) {
          return '<option value="' + esc(f) + '">' + esc(f.replaceAll('_', ' ')) + '</option>';
        }).join('');
        return '' +
          '<div class="doc" data-id="' + d.id + '">' +
            '<div class="doc-title">' + esc(d.title) + '</div>' +
            '<div class="meta">' + where + ' · ' + d.chunk_count + ' sections · ' + esc(d.created_at) + '</div>' +
            '<div class="actions">' +
              '<select class="field-select" style="width:auto;padding:6px 10px;font-size:12px;">' + fieldOptions + '</select>' +
              '<button type="button" class="analyze-btn"' + (configured ? '' : ' disabled') + '>Analyse</button>' +
              '<button type="button" class="secondary delete-btn">Delete</button>' +
            '</div>' +
          '</div>';
      }).join('');
    }

    docList.addEventListener('click', async function (event) {
      const card = event.target.closest('.doc');
      if (!card) return;
      const id = card.getAttribute('data-id');

      if (event.target.classList.contains('analyze-btn')) {
        const field = card.querySelector('.field-select').value;
        event.target.disabled = true;
        setStatus(docStatus, 'Analysing… this runs one model call per shortlisted requirement and can take a while.');
        try {
          const result = await api('/nfr-enrichment/documents/' + id + '/analyze', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ field: field })
          });
          let msg = result.proposed + ' proposal(s) from ' + result.considered +
            ' requirement(s); ' + result.skipped + ' skipped';
          if (result.discarded) msg += ', ' + result.discarded + ' discarded as unverifiable';
          msg += '.';
          setStatus(docStatus, msg, result.discarded > 0);
          if (result.warnings && result.warnings.length) {
            console.warn('nfr-enrichment warnings', result.warnings);
          }
          await loadProposals();
        } catch (err) {
          setStatus(docStatus, 'Analysis failed: ' + err.message, true);
        } finally {
          event.target.disabled = false;
        }
        return;
      }

      if (event.target.classList.contains('delete-btn')) {
        if (!window.confirm('Delete this source document? Pending proposals from it will be superseded.')) return;
        try {
          await api('/nfr-enrichment/documents/' + id, { method: 'DELETE' });
          setStatus(docStatus, 'Document deleted.');
          await loadDocuments();
          await loadProposals();
        } catch (err) {
          setStatus(docStatus, 'Delete failed: ' + err.message, true);
        }
      }
    });

    addURLBtn.addEventListener('click', async function () {
      const url = docURL.value.trim();
      if (!url) { setStatus(docStatus, 'A URL is required.', true); return; }
      addURLBtn.disabled = true;
      setStatus(docStatus, 'Fetching…');
      try {
        await api('/nfr-enrichment/documents', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url: url, title: docTitle.value.trim() })
        });
        docURL.value = ''; docTitle.value = '';
        setStatus(docStatus, 'Document added.');
        await loadDocuments();
      } catch (err) {
        setStatus(docStatus, 'Could not add the document: ' + err.message, true);
      } finally {
        addURLBtn.disabled = false;
      }
    });

    uploadBtn.addEventListener('click', async function () {
      const file = docFile.files && docFile.files[0];
      if (!file) { setStatus(docStatus, 'Choose a file first.', true); return; }
      const form = new FormData();
      form.append('file', file);
      if (docTitle.value.trim()) form.append('title', docTitle.value.trim());

      uploadBtn.disabled = true;
      setStatus(docStatus, 'Uploading…');
      try {
        await api('/nfr-enrichment/documents', { method: 'POST', body: form });
        docFile.value = ''; docTitle.value = '';
        setStatus(docStatus, 'Document added.');
        await loadDocuments();
      } catch (err) {
        setStatus(docStatus, 'Upload failed: ' + err.message, true);
      } finally {
        uploadBtn.disabled = false;
      }
    });

    async function loadProposals() {
      const status = statusFilter.value;
      try {
        const data = await api('/nfr-enrichment/proposals' + (status ? '?status=' + encodeURIComponent(status) : ''));
        renderProposals(data.proposals || []);
        setStatus(proposalStatus, '');
      } catch (err) {
        setStatus(proposalStatus, 'Could not load proposals: ' + err.message, true);
      }
    }

    function renderProposals(items) {
      if (!items.length) {
        proposalList.innerHTML = '<div class="empty">Nothing to review.</div>';
        return;
      }
      proposalList.innerHTML = items.map(function (p) {
        const citations = (p.citations || []).map(function (c) {
          return '<div class="citation">' +
            '<div class="where">' + esc(c.heading || 'source document') + '</div>' +
            '<blockquote>' + esc(c.quote) + '</blockquote>' +
          '</div>';
        }).join('') || '<div class="empty">No citations recorded.</div>';

        const pending = p.status === 'pending';
        const controls = pending
          ? '<div class="field" style="margin-top:10px;">' +
              '<label>Text to write to <strong>' + esc(p.field.replaceAll('_', ' ')) + '</strong> (edit before accepting if needed)</label>' +
              '<textarea class="edit-text">' + esc(p.suggested_text) + '</textarea>' +
            '</div>' +
            '<div class="field">' +
              '<label>Rejection note (optional)</label>' +
              '<input class="reject-note" type="text" placeholder="why this does not apply">' +
            '</div>' +
            '<div class="actions">' +
              '<button type="button" class="accept-btn">Accept &amp; write to NFR</button>' +
              '<button type="button" class="secondary reject-btn">Reject</button>' +
            '</div>'
          : '<div class="meta">' + esc(p.status) + ' ' + esc(p.decided_at || '') +
            (p.decided_by ? ' by ' + esc(p.decided_by) : '') +
            (p.decided_note ? ' — ' + esc(p.decided_note) : '') + '</div>';

        return '' +
          '<div class="proposal" data-id="' + p.id + '">' +
            '<div style="display:flex;justify-content:space-between;gap:8px;align-items:baseline;">' +
              '<strong>' + esc(p.nfr_key) + '</strong>' +
              '<span class="pill ' + esc(p.status) + '">' + esc(p.status) + '</span>' +
            '</div>' +
            '<div class="meta">' + esc(p.nfr_summary || '') + '</div>' +
            '<div class="meta">from ' + esc(p.document_title || ('document ' + p.document_id)) +
              ' · confidence ' + (Math.round((p.confidence || 0) * 100)) + '%' +
              ' · ' + esc(p.model) + ' · prompt ' + esc(String(p.prompt_hash || '').slice(0, 12)) + '</div>' +
            '<div class="suggestion">' + esc(p.suggested_text) + '</div>' +
            (p.rationale ? '<div class="rationale">' + esc(p.rationale) + '</div>' : '') +
            citations +
            controls +
          '</div>';
      }).join('');
    }

    proposalList.addEventListener('click', async function (event) {
      const card = event.target.closest('.proposal');
      if (!card) return;
      const id = card.getAttribute('data-id');

      if (event.target.classList.contains('accept-btn')) {
        const text = card.querySelector('.edit-text').value;
        event.target.disabled = true;
        try {
          await api('/nfr-enrichment/proposals/' + id + '/accept', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ text: text })
          });
          setStatus(proposalStatus, 'Accepted and written to the NFR.');
          await loadProposals();
        } catch (err) {
          setStatus(proposalStatus, 'Accept failed: ' + err.message, true);
          event.target.disabled = false;
        }
        return;
      }

      if (event.target.classList.contains('reject-btn')) {
        const note = card.querySelector('.reject-note').value;
        event.target.disabled = true;
        try {
          await api('/nfr-enrichment/proposals/' + id + '/reject', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ note: note })
          });
          setStatus(proposalStatus, 'Rejected.');
          await loadProposals();
        } catch (err) {
          setStatus(proposalStatus, 'Reject failed: ' + err.message, true);
          event.target.disabled = false;
        }
      }
    });

    statusFilter.addEventListener('change', loadProposals);

    loadDocuments().then(loadProposals);
  </script>
</body>
</html>`
}
