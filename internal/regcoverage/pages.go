package regcoverage

import (
	"fmt"
	"strings"

	"grc/internal/pageui"
)

// The two pages share one stylesheet. The report page reuses the print
// document's markup for the report body itself (see reportBodyHTML), so what
// the reader sees on screen and what lands in the PDF cannot drift apart.
const pageStyles = `
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
    max-width: 1400px;
    margin: 24px auto;
    padding: 24px;
    background: var(--panel);
    border: 1px solid rgba(215,206,191,0.8);
    border-radius: 24px;
    box-shadow: 0 24px 60px rgba(28,36,49,0.12);
  }
  h1 { margin: 0 0 8px; font-size: clamp(1.8rem, 3.4vw, 2.8rem); line-height: 1; letter-spacing: -0.02em; }
  h2 { font-size: 1.05rem; margin: 22px 0 8px; padding-bottom: 4px; border-bottom: 1px solid var(--line); }
  h3 { font-size: 1rem; margin: 14px 0 4px; }
  p { margin: 0 0 8px; }
  a { color: var(--accent); }
  .lede { color: var(--muted); max-width: 80ch; }
  .toolbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 14px 0; }
  .card {
    border: 1px solid var(--line);
    border-radius: 16px;
    background: rgba(255,255,255,0.86);
    padding: 16px;
    margin-bottom: 16px;
  }
  label { display: block; font-family: Arial, sans-serif; font-size: 12px; letter-spacing: 0.07em;
          text-transform: uppercase; color: var(--muted); font-weight: 700; margin-bottom: 6px; }
  input, select, textarea {
    width: 100%; padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line);
    background: white; color: var(--ink); font: inherit;
  }
  textarea { min-height: 80px; resize: vertical; }
  button {
    padding: 10px 14px; border-radius: 999px; border: 0; background: #dfc4b4;
    color: var(--ink); font: inherit; cursor: pointer;
  }
  button.secondary { background: white; border: 1px solid var(--line); }
  button[disabled] { opacity: 0.55; cursor: not-allowed; }
  .grid { display: grid; gap: 14px; }
  .fields { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); }
  table.list { width: 100%; border-collapse: collapse; }
  table.list th, table.list td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--line); vertical-align: top; }
  table.list th { font-family: Arial, sans-serif; font-size: 11px; text-transform: uppercase;
                  letter-spacing: 0.06em; color: var(--muted); }
  .chip { display: inline-block; padding: 3px 10px; border-radius: 999px; border: 1px solid var(--line);
          font-family: Arial, sans-serif; font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; }
  .chip.on { border-color: #7fae94; color: var(--good); }
  .chip.off { border-color: #d7b271; color: var(--warn); }
  .status { font-family: Arial, sans-serif; font-size: 13px; color: var(--muted); min-height: 20px; }
  .status.warn { color: var(--warn); }
  .layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 420px); gap: 16px; align-items: start; }
  .report { background: var(--panel); color: var(--ink); border: 1px solid var(--line);
            border-radius: 16px; padding: 20px 22px;
            max-height: calc(100dvh - 220px); overflow: auto; }
  .side { position: sticky; top: 16px; display: grid; gap: 14px; }
  .chatlog { display: flex; flex-direction: column; gap: 8px; max-height: 46vh; overflow: auto; padding-right: 4px; }
  .msg { border: 1px solid var(--line); border-radius: 12px; padding: 8px 12px; white-space: pre-wrap;
         word-break: break-word; font-size: 14px; }
  .msg.user { align-self: flex-end; border-left: 4px solid var(--accent); background: #fdf8f2; }
  .msg.ai { align-self: flex-start; border-left: 4px solid var(--good); }
  .msg .who { display: block; font-family: Arial, sans-serif; font-size: 10px; text-transform: uppercase;
              letter-spacing: 0.07em; color: var(--muted); margin-bottom: 4px; }
  .viewer { width: 100%; height: 70vh; border: 1px solid var(--line); border-radius: 12px; background: white; }
  .revise-btn { margin-top: 8px; padding: 5px 12px; font-family: Arial, sans-serif; font-size: 11px;
                text-transform: uppercase; letter-spacing: 0.06em; background: white; border: 1px solid var(--line); }
  .hidden { display: none; }
  @media (max-width: 1100px) {
    .layout { grid-template-columns: 1fr; }
    .side { position: static; }
    .report { max-height: none; }
  }
`

// reportBodyStyles restates the print document's own classes for the in-app
// page, which does not load the standalone document's stylesheet.
//
// The !important declarations are deliberate and narrow. The global theme
// layer (see internal/app/theme_middleware.go) paints every <p> on every page
// with --muted so that incidental page prose recedes; here the prose *is* the
// deliverable, and a report whose findings render as grey secondary text is
// unreadable in the dark themes. So the report's own paragraphs are pulled back
// to --ink, and only the genuinely secondary lines are left muted.
const reportBodyStyles = `
  .report h1, .report h2, .report h3 { color: var(--ink); }
  .report p, .report .summary, .report td { color: var(--ink) !important; }
  .report p.meta, .report .meta, .report th, .report .label,
  .report .footnote, .report .subtitle { color: var(--muted) !important; }
  .report .section.quiet, .report .section.quiet strong { color: var(--muted) !important; }
  .report .quote { color: var(--ink) !important; opacity: 0.85; }
  .report .stats { display: flex; flex-wrap: wrap; gap: 8px; margin: 14px 0; }
  .report .stat { border: 1px solid var(--line); border-radius: 10px; padding: 8px 14px;
                  font-family: Arial, sans-serif; min-width: 110px; background: var(--surface-strong); }
  .report .stat .n { display: block; font-size: 20px; font-weight: 700; color: var(--ink) !important; }
  .report .stat .l { font-size: 10px; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); }
  .report .meta { color: var(--muted); font-size: 12px; font-family: Arial, sans-serif; }
  .report .subtitle { color: var(--muted); }
  .report .summary { white-space: pre-wrap; }
  .report .section { margin: 0 0 14px; padding: 12px 14px; border: 1px solid var(--line); border-radius: 10px; }
  .report .section.quiet { border-style: dashed; color: var(--muted); padding: 7px 14px; }
  .report .label { font-family: Arial, sans-serif; font-size: 11px; text-transform: uppercase;
                   letter-spacing: 0.07em; color: var(--muted); margin: 10px 0 2px; }
  .report table { width: 100%; border-collapse: collapse; margin: 4px 0; font-size: 13px; }
  .report th, .report td { text-align: left; vertical-align: top; padding: 5px 7px; border-bottom: 1px solid var(--line); }
  .report th { font-family: Arial, sans-serif; font-size: 10px; text-transform: uppercase;
               letter-spacing: 0.06em; color: var(--muted); }
  .report td.ref { font-family: "Courier New", monospace; white-space: nowrap; }
  .report .tag { display: inline-block; font-family: Arial, sans-serif; font-size: 10px; text-transform: uppercase;
                 letter-spacing: 0.05em; padding: 1px 7px; border: 1px solid var(--line); border-radius: 999px; }
  .report .tag.low { border-color: #d7b271; color: var(--warn); }
  .report .tag.warn { border-color: #b3564a; color: #8b2f22; }
  .report .tag.seed { border-color: #7fae94; color: var(--good); }
  .report .quote { font-style: italic; border-left: 3px solid var(--line);
                   padding-left: 10px; margin: 6px 0; }
  .report .refs { font-family: "Courier New", monospace; font-size: 13px; line-height: 1.8; }
  .report .footnote { color: var(--muted); font-size: 12px; font-family: Arial, sans-serif;
                      margin-top: 20px; border-top: 1px solid var(--line); padding-top: 8px; }
`

// indexPageHTML is the upload-and-list page.
func indexPageHTML(frameworks []Framework, configured bool, model string) string {
	var options strings.Builder
	options.WriteString(`<option value="">Detect from the document</option>`)
	for _, f := range frameworks {
		label := f.Name
		if f.SourceRef != "" {
			label += " (" + f.SourceRef + ")"
		}
		options.WriteString(`<option value="` + esc(f.ID) + `">` + esc(label) + `</option>`)
	}

	aiChip := `<span class="chip off">AI: not configured</span>`
	if configured {
		aiChip = `<span class="chip on">AI: ` + esc(model) + `</span>`
	}

	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Regulation Coverage · GRC</title>
  <style>` + pageStyles + `</style>
</head>
<body>
  <main>
    ` + pageui.Nav("/regulation-coverage") + `
    <h1>Regulation Coverage</h1>
    <p class="lede">Upload an EU regulation and have every article analysed against this
    installation's Security NFR catalog and NIST SP 800-53: what the article requires, what
    already satisfies it, what does not, and how to meet it well. The result is a versioned
    report you can download as a PDF and then interrogate and correct in conversation.</p>
    <div class="toolbar">` + aiChip + `
      <a class="chip" href="/security-nfrs">Security NFRs</a>
      <a class="chip" href="/controls">800-53 controls</a>
      <a class="chip" href="/settings">Settings</a>
    </div>

    <section class="card">
      <h2>Upload a regulation</h2>
      <form id="uploadForm" class="grid">
        <div class="fields">
          <div>
            <label for="file">Document (PDF, DOCX, TXT, MD — max ` + humanBytes(MaxUploadBytes) + `)</label>
            <input id="file" name="file" type="file" accept=".pdf,.docx,.txt,.md" required>
          </div>
          <div>
            <label for="title">Title (optional)</label>
            <input id="title" name="title" type="text" placeholder="Taken from the filename">
          </div>
          <div>
            <label for="framework">Framework profile</label>
            <select id="framework" name="framework">` + options.String() + `</select>
          </div>
        </div>
        <div class="toolbar">
          <button type="submit">Upload &amp; segment</button>
          <span id="uploadStatus" class="status"></span>
        </div>
      </form>
      <p class="status">Uploading only extracts and segments the document — nothing is sent to a
      model until you run the analysis, so a document that segmented badly can be deleted first.</p>
    </section>

    <section class="card">
      <h2>Regulations</h2>
      <table class="list">
        <thead><tr><th>Regulation</th><th>Framework</th><th>Sections</th><th>Status</th><th>Report</th><th></th></tr></thead>
        <tbody id="rows"><tr><td colspan="6" class="status">Loading…</td></tr></tbody>
      </table>
    </section>
  </main>

  <script>
    const rows = document.getElementById('rows');
    const uploadForm = document.getElementById('uploadForm');
    const uploadStatus = document.getElementById('uploadStatus');

    function esc(value) {
      return String(value == null ? '' : value)
        .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;').replaceAll("'", '&#39;');
    }

    function setStatus(el, message, isWarning) {
      el.textContent = message || '';
      el.className = 'status' + (isWarning ? ' warn' : '');
    }

    function statusCell(reg) {
      if (reg.status === 'analyzed') return 'Analysed';
      if (reg.status === 'analyzing') return 'Analysing…';
      if (reg.status === 'failed') return 'Failed: ' + esc(reg.status_detail || 'see the log');
      return 'Segmented, not analysed';
    }

    async function load() {
      try {
        const resp = await fetch('/regulation-coverage/regulations');
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        const list = data.regulations || [];
        if (!list.length) {
          rows.innerHTML = '<tr><td colspan="6" class="status">Nothing uploaded yet.</td></tr>';
          return;
        }
        rows.innerHTML = list.map((reg) => {
          const report = reg.latest_version
            ? '<a href="/regulation-coverage/' + reg.id + '">v' + reg.latest_version + '</a>'
            : '—';
          const framework = esc(reg.framework_name) + (reg.detected ? '' : ' <span class="chip">generic</span>');
          return '<tr>' +
            '<td><a href="/regulation-coverage/' + reg.id + '">' + esc(reg.title) + '</a><br>' +
              '<span class="status">' + esc(reg.filename) + '</span></td>' +
            '<td>' + framework + '</td>' +
            '<td>' + reg.section_count + '</td>' +
            '<td>' + statusCell(reg) + '</td>' +
            '<td>' + report + '</td>' +
            '<td><button class="secondary" data-analyze="' + reg.id + '">' +
              (reg.latest_version ? 'Re-analyse' : 'Analyse') + '</button> ' +
              '<button class="secondary" data-delete="' + reg.id + '">Delete</button></td>' +
            '</tr>';
        }).join('');
      } catch (err) {
        rows.innerHTML = '<tr><td colspan="6" class="status warn">' + esc(err.message) + '</td></tr>';
      }
    }

    uploadForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      const file = document.getElementById('file').files[0];
      if (!file) { setStatus(uploadStatus, 'Choose a file first.', true); return; }
      const body = new FormData();
      body.append('file', file);
      body.append('title', document.getElementById('title').value);
      body.append('framework', document.getElementById('framework').value);

      setStatus(uploadStatus, 'Extracting and segmenting…');
      try {
        const resp = await fetch('/regulation-coverage/regulations', { method: 'POST', body });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        setStatus(uploadStatus, 'Segmented into ' + data.section_count + ' section(s).');
        uploadForm.reset();
        load();
      } catch (err) {
        setStatus(uploadStatus, err.message, true);
      }
    });

    rows.addEventListener('click', async (event) => {
      const analyze = event.target.getAttribute('data-analyze');
      const remove = event.target.getAttribute('data-delete');
      if (analyze) {
        if (!confirm('Analyse every section? This makes one model call per section.')) return;
        event.target.disabled = true;
        event.target.textContent = 'Analysing…';
        try {
          const resp = await fetch('/regulation-coverage/' + analyze + '/analyze', { method: 'POST' });
          const data = await resp.json();
          if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
          window.location.href = '/regulation-coverage/' + analyze;
        } catch (err) {
          alert('Analysis failed: ' + err.message);
          load();
        }
        return;
      }
      if (remove) {
        if (!confirm('Delete this regulation, its report and its conversation?')) return;
        const resp = await fetch('/regulation-coverage/' + remove, { method: 'DELETE' });
        if (!resp.ok && resp.status !== 204) {
          const data = await resp.json().catch(() => ({}));
          alert('Delete failed: ' + (data.error || resp.status));
        }
        load();
      }
    });

    load();
  </script>
</body>
</html>`
}

// reportPageData is everything the report page renders.
type reportPageData struct {
	Report       *Report
	Versions     []Version
	Chat         []ChatTurn
	AIConfigured bool
	Model        string
	PDFAvailable bool
}

// reportPageHTML puts the report, the original document and the conversation on
// one page. They belong together: a reviewer challenging a mapping wants the
// article in front of them, and a correction they agree with should be one
// click from landing in the next version.
func reportPageHTML(data reportPageData) string {
	report := data.Report
	id := report.Regulation.ID

	var versionOptions strings.Builder
	for _, v := range data.Versions {
		selected := ""
		if v.Number == report.Version.Number {
			selected = " selected"
		}
		label := fmt.Sprintf("v%d — %s", v.Number, v.Note)
		versionOptions.WriteString(fmt.Sprintf(`<option value="%d"%s>%s</option>`,
			v.Number, selected, esc(truncate(label, 70))))
	}

	pdfButton := ""
	if data.PDFAvailable {
		pdfButton = fmt.Sprintf(
			`<a class="chip" href="/regulation-coverage/%d/report.pdf?version=%d">Download PDF</a>`,
			id, report.Version.Number)
	}

	aiChip := `<span class="chip off">AI: not configured</span>`
	if data.AIConfigured {
		aiChip = `<span class="chip on">AI: ` + esc(data.Model) + `</span>`
	}

	var chatLog strings.Builder
	for _, turn := range data.Chat {
		kind := "ai"
		who := "Assistant"
		if turn.Role == RoleUser {
			kind, who = "user", "You"
		}
		if turn.Role == RoleAssistant && turn.Model != "" {
			who += " · " + turn.Model
		}
		chatLog.WriteString(`<div class="msg ` + kind + `"><span class="who">` + esc(who) +
			`</span>` + esc(turn.Content) + `</div>`)
	}

	// The original is shown in an iframe rather than an object/embed so the
	// sandbox and CSP the source route sets actually apply to it.
	// The link below the frame is not redundant: a browser configured to
	// download PDFs rather than display them renders the frame blank, and
	// without it the original would look unavailable.
	viewer := fmt.Sprintf(
		`<iframe class="viewer" src="/regulation-coverage/%d/source" title="Original document" loading="lazy"></iframe>`+
			`<p class="status">Not displaying? <a href="/regulation-coverage/%d/source">Open the original in a new tab</a>.</p>`,
		id, id)
	if report.Regulation.MediaType != "application/pdf" &&
		!strings.HasPrefix(report.Regulation.MediaType, "text/") {
		viewer = fmt.Sprintf(
			`<p class="status">This document type cannot be displayed in the browser. `+
				`<a href="/regulation-coverage/%d/source">Download the original</a>.</p>`, id)
	}

	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + esc(report.Regulation.Title) + ` · Coverage · GRC</title>
  <style>` + pageStyles + reportBodyStyles + `</style>
</head>
<body>
  <main>
    ` + pageui.Nav("/regulation-coverage") + `
    <div class="toolbar">
      <a class="chip" href="/regulation-coverage">&larr; All regulations</a>
      ` + aiChip + pdfButton + `
      <a class="chip" href="/regulation-coverage/` + fmt.Sprint(id) + `/report.json?version=` +
		fmt.Sprint(report.Version.Number) + `">JSON</a>
      <a class="chip" href="/regulation-coverage/` + fmt.Sprint(id) + `/source">Original document</a>
      <select id="versionPicker" style="width:auto">` + versionOptions.String() + `</select>
    </div>

    <div class="layout">
      <section class="report">` + reportBodyHTML(report, true) + `</section>

      <div class="side">
        <section class="card">
          <h2 style="margin-top:0">Ask about this report</h2>
          <div id="chatLog" class="chatlog">` + chatLog.String() + `</div>
          <form id="chatForm" style="margin-top:10px">
            <textarea id="question" placeholder="Which articles are we least covered against?" aria-label="Question"></textarea>
            <div class="toolbar">
              <button type="submit" id="askBtn">Ask</button>
              <button type="button" class="secondary" id="clearChat">Clear</button>
              <span id="chatStatus" class="status"></span>
            </div>
          </form>
        </section>

        <section class="card">
          <h2 style="margin-top:0">Revise a section</h2>
          <p class="status">A revision re-analyses one section with your correction and writes the
          next version. Earlier versions stay downloadable exactly as they were.</p>
          <form id="reviseForm" class="grid">
            <div>
              <label for="reviseSection">Section reference</label>
              <input id="reviseSection" type="text" placeholder="e.g. ` + esc(firstSectionRef(report)) + `" required>
            </div>
            <div>
              <label for="reviseInstruction">What is wrong, and what should it say</label>
              <textarea id="reviseInstruction" required
                placeholder="AC-2 does not belong here — this is about incident reporting timelines, not accounts."></textarea>
            </div>
            <div class="toolbar">
              <button type="submit" id="reviseBtn">Apply as a new version</button>
              <span id="reviseStatus" class="status"></span>
            </div>
          </form>
        </section>

        <section class="card">
          <h2 style="margin-top:0">Original document</h2>
          ` + viewer + `
        </section>
      </div>
    </div>
  </main>

  <script>
    const regulationID = ` + fmt.Sprint(id) + `;
    const chatLog = document.getElementById('chatLog');
    const chatForm = document.getElementById('chatForm');
    const question = document.getElementById('question');
    const askBtn = document.getElementById('askBtn');
    const chatStatus = document.getElementById('chatStatus');
    const reviseForm = document.getElementById('reviseForm');
    const reviseSection = document.getElementById('reviseSection');
    const reviseInstruction = document.getElementById('reviseInstruction');
    const reviseBtn = document.getElementById('reviseBtn');
    const reviseStatus = document.getElementById('reviseStatus');
    const versionPicker = document.getElementById('versionPicker');

    function setStatus(el, message, isWarning) {
      el.textContent = message || '';
      el.className = 'status' + (isWarning ? ' warn' : '');
    }

    function addMessage(who, text, kind) {
      const row = document.createElement('div');
      row.className = 'msg ' + kind;
      const label = document.createElement('span');
      label.className = 'who';
      label.textContent = who;
      row.appendChild(label);
      row.appendChild(document.createTextNode(text));
      chatLog.appendChild(row);
      chatLog.scrollTop = chatLog.scrollHeight;
      return row;
    }

    chatLog.scrollTop = chatLog.scrollHeight;

    versionPicker.addEventListener('change', () => {
      window.location.href = '/regulation-coverage/' + regulationID + '?version=' + versionPicker.value;
    });

    chatForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      const text = question.value.trim();
      if (!text) return;
      question.value = '';
      addMessage('You', text, 'user');
      const pending = addMessage('Assistant', 'Thinking…', 'ai');
      askBtn.disabled = true;
      setStatus(chatStatus, '');
      try {
        const resp = await fetch('/regulation-coverage/' + regulationID + '/chat', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ question: text })
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        pending.remove();
        addMessage('Assistant' + (data.model ? ' · ' + data.model : ''), data.content, 'ai');
      } catch (err) {
        pending.remove();
        setStatus(chatStatus, err.message, true);
      } finally {
        askBtn.disabled = false;
      }
    });

    document.getElementById('clearChat').addEventListener('click', async () => {
      if (!confirm('Clear this conversation?')) return;
      await fetch('/regulation-coverage/' + regulationID + '/chat', { method: 'DELETE' });
      chatLog.replaceChildren();
    });

    // Every section carries its own revise button, so a correction starts from
    // the section the reader is looking at rather than from a reference they
    // have to copy.
    document.querySelector('.report').addEventListener('click', (event) => {
      const ref = event.target.getAttribute('data-ref');
      if (!ref) return;
      reviseSection.value = ref;
      reviseInstruction.focus();
      reviseForm.scrollIntoView({ behavior: 'smooth', block: 'center' });
    });

    reviseForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      const section = reviseSection.value.trim();
      const instruction = reviseInstruction.value.trim();
      if (!section || !instruction) return;
      reviseBtn.disabled = true;
      setStatus(reviseStatus, 'Re-analysing ' + section + '…');
      try {
        const resp = await fetch('/regulation-coverage/' + regulationID + '/revise', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ section: section, instruction: instruction })
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
        window.location.href = '/regulation-coverage/' + regulationID;
      } catch (err) {
        setStatus(reviseStatus, err.message, true);
      } finally {
        reviseBtn.disabled = false;
      }
    });
  </script>
</body>
</html>`
}

// firstSectionRef supplies a real example for the revision field's placeholder,
// because the reference format differs per framework profile.
func firstSectionRef(report *Report) string {
	for _, entry := range report.Sections {
		if entry.Finding != nil && entry.Finding.Relevant {
			return entry.Section.Ref
		}
	}
	if len(report.Sections) > 0 {
		return report.Sections[0].Section.Ref
	}
	return "ART-1"
}
