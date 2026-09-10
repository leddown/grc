package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/db"
	"grc/internal/dbsync"
	"grc/internal/knowledge"
	"grc/internal/pageui"
)

// registerUtilitiesRoutes wires the Utilities page and its full-database
// export/import/backup endpoints. Export/import move an entire data set
// between two disconnected deployments: one server exports a JSON snapshot,
// the other imports it to overwrite its database. Backup produces a raw
// engine-native copy of the live database for disaster recovery. The knowledge
// export is a third shape of the same data — denormalized and readable, for
// uploading into a Wintermute agent's library. Because all three expose every
// row (including password hashes, in the first two) and import is destructive,
// all routes sit behind the admin middleware (and are open only in local
// single-user mode).
func registerUtilitiesRoutes(r gin.IRouter, sqliteDB *db.Conn, knowledgeService *knowledge.Service, adminMiddleware gin.HandlerFunc, localMode bool) {
	group := r.Group("/")
	if !localMode {
		group.Use(adminMiddleware)
	}
	group.GET("/utilities", utilitiesPage)
	group.GET("/utilities/export", utilitiesExport(sqliteDB))
	group.POST("/utilities/import", utilitiesImport(sqliteDB))
	group.GET("/utilities/backup", utilitiesBackup(sqliteDB))
	group.GET("/utilities/knowledge-export", utilitiesKnowledgeExport(knowledgeService))
	group.GET("/utilities/integrity-check", utilitiesIntegrityCheck(sqliteDB))
}

// utilitiesKnowledgeExport downloads the AI-facing bundle: every NFR, control,
// regulation clause and coverage finding, policy section, risk and crisis
// exercise, denormalized into one readable JSON document (see
// knowledge.Bundle).
//
// It is the same payload as GET /api/knowledge/export, offered here because
// that route needs the knowledge token and this one is for a signed-in admin
// who wants the file in their browser — typically to upload it into a
// Wintermute agent's library so the agent knows this installation's current
// state without querying it live.
func utilitiesKnowledgeExport(knowledgeService *knowledge.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		bundle, err := knowledgeService.Bundle()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to build knowledge export: %v", err)})
			return
		}
		payload, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to encode knowledge export: %v", err)})
			return
		}
		c.Header("Content-Disposition", `attachment; filename="`+knowledge.BundleFilename(bundle)+`"`)
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	}
}

func utilitiesExport(sqliteDB *db.Conn) gin.HandlerFunc {
	return func(c *gin.Context) {
		snap, err := dbsync.Export(sqliteDB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to export data set: %v", err)})
			return
		}
		payload, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to encode data set: %v", err)})
			return
		}
		filename := "grc-data-export-" + time.Now().Format("20060102-150405") + ".json"
		c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	}
}

// maxImportBytes bounds an uploaded data set. A full export of this schema runs
// to single-digit megabytes, so 128 MiB is far past any legitimate snapshot
// while still stopping an unbounded upload from being read into memory and then
// held again inside the import transaction.
const maxImportBytes = 128 << 20

func utilitiesImport(sqliteDB *db.Conn) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Applied to the request body before FormFile, so it bounds both the
		// multipart and the raw-body path — FormFile reads through this reader.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes)

		var reader io.Reader
		if file, _, err := c.Request.FormFile("file"); err == nil {
			defer file.Close()
			reader = file
		} else {
			reader = c.Request.Body
		}

		var snap dbsync.Snapshot
		if err := json.NewDecoder(reader).Decode(&snap); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid data set file: %v", err)})
			return
		}
		// Only a *newer* snapshot is rejected. An older one imports fine —
		// Import reads whatever tables the snapshot carries and leaves the rest
		// empty — and rejecting it would mean every version bump silently
		// invalidated every backup taken before it, which is the opposite of
		// what a restore path is for.
		if snap.Version < 1 || snap.Version > dbsync.SnapshotVersion {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(
				"unsupported data set version %d (this build can import versions 1 to %d)",
				snap.Version, dbsync.SnapshotVersion)})
			return
		}

		report, err := dbsync.Import(sqliteDB, &snap)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("import failed and was rolled back: %v", err)})
			return
		}

		tables := make([]gin.H, 0, len(report.Tables))
		for _, t := range report.Tables {
			tables = append(tables, gin.H{"table": t.Table, "rows": t.Rows})
		}
		c.JSON(http.StatusOK, gin.H{"total": report.Total(), "tables": tables})
	}
}

// utilitiesBackup streams a raw, engine-native copy of the live SQLite
// database file. Unlike the JSON export, it is not table-aware — it is
// SQLite's own `VACUUM INTO`, so it always carries every table, index, and
// row the database actually has, including anything a future schema change
// forgets to add to dbsync's table list. That makes it the recommended
// "disaster recovery" backup; the JSON export is for moving data between
// two already-running deployments (e.g. across a SQLite/PostgreSQL boundary).
//
// `VACUUM INTO` also refuses to write to a path that already exists, and
// takes a read lock for the duration rather than copying the file out from
// under concurrent writers the way a plain file copy would risk doing against
// a WAL-mode database.
func utilitiesBackup(sqliteDB *db.Conn) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sqliteDB.Dialect() != db.DialectSQLite {
			c.JSON(http.StatusBadRequest, gin.H{"error": "raw database backup is only available for SQLite; this deployment is using PostgreSQL, so back it up with pg_dump against the configured connection instead"})
			return
		}

		// A temp *directory* rather than a temp file: VACUUM INTO refuses to
		// write to a path that already exists, so the destination has to be a
		// name nothing has created yet.
		tmpDir, err := os.MkdirTemp("", "grc-backup-*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to prepare backup file: %v", err)})
			return
		}
		defer os.RemoveAll(tmpDir)
		tmpPath := filepath.Join(tmpDir, "backup.db")

		if _, err := sqliteDB.Exec("VACUUM INTO ?", tmpPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to snapshot database: %v", err)})
			return
		}

		filename := "grc-sqlite-backup-" + time.Now().Format("20060102-150405") + ".db"
		c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
		c.File(tmpPath)
	}
}

// utilitiesIntegrityCheck runs SQLite's built-in consistency checks so a
// backup can be trusted before it's relied on: `PRAGMA integrity_check`
// catches structural corruption (bad pages, broken indexes), and
// `PRAGMA foreign_key_check` catches rows that reference a parent that no
// longer exists, which integrity_check does not.
func utilitiesIntegrityCheck(sqliteDB *db.Conn) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sqliteDB.Dialect() != db.DialectSQLite {
			c.JSON(http.StatusBadRequest, gin.H{"error": "integrity check is only available for SQLite"})
			return
		}

		integrity, err := runIntegrityCheck(sqliteDB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("integrity_check failed: %v", err)})
			return
		}

		foreignKeyIssues, err := runForeignKeyCheck(sqliteDB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("foreign_key_check failed: %v", err)})
			return
		}

		ok := len(integrity) == 1 && integrity[0] == "ok" && len(foreignKeyIssues) == 0
		c.JSON(http.StatusOK, gin.H{
			"ok":                 ok,
			"integrity_check":    integrity,
			"foreign_key_issues": foreignKeyIssues,
		})
	}
}

func runIntegrityCheck(sqliteDB *db.Conn) ([]string, error) {
	rows, err := sqliteDB.Query("PRAGMA integrity_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var integrity []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		integrity = append(integrity, line)
	}
	return integrity, rows.Err()
}

func runForeignKeyCheck(sqliteDB *db.Conn) ([]string, error) {
	rows, err := sqliteDB.Query("PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []string
	for rows.Next() {
		var table string
		var rowid sql.NullInt64
		var parent string
		var fkid int64
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return nil, err
		}
		issues = append(issues, fmt.Sprintf("%s row %v violates its foreign key to %s", table, rowid, parent))
	}
	return issues, rows.Err()
}

func utilitiesPage(c *gin.Context) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Utilities · GRC</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #1c2431;
      --muted: #5e6672;
      --line: #d7cebf;
      --panel: rgba(255,252,246,0.92);
      --accent: #efe6d6;
      --accent-strong: #dfc4b4;
      --warn: #8b3d2e;
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
      max-width: 1100px;
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
    .lede { max-width: 820px; margin-bottom: 22px; font-size: 17px; line-height: 1.5; }
    .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
    .panel {
      border: 1px solid rgba(215,206,191,0.9);
      border-radius: 18px;
      background: var(--panel);
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
    .panel-body { padding: 18px 16px; }
    .section-note { margin: 0 0 16px; color: var(--muted); font-size: 15px; line-height: 1.5; }
    .actions { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 18px; }
    button, a.btn {
      padding: 12px 16px;
      border: 0;
      border-radius: 999px;
      background: var(--accent-strong);
      color: var(--ink);
      font: inherit;
      cursor: pointer;
      text-decoration: none;
      display: inline-block;
    }
    button:hover, a.btn:hover { background: #d1b09d; }
    button:disabled { opacity: 0.55; cursor: not-allowed; }
    .danger { background: #d8a99c; }
    .danger:hover { background: #cf9384; }
    input[type="file"] {
      width: 100%;
      padding: 12px 14px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: var(--bg);
      font: inherit;
      color: var(--ink);
    }
    .warning {
      margin-top: 14px;
      padding: 12px 14px;
      border-radius: 12px;
      border: 1px solid var(--danger);
      background: var(--surface-strong);
      color: var(--danger);
      font-size: 14px;
      line-height: 1.5;
    }
    .status { margin-top: 16px; color: var(--muted); line-height: 1.5; min-height: 22px; }
    .status.ok { color: #0b5d3b; }
    .status.err { color: var(--warn); }
    .report {
      margin-top: 14px;
      font-family: "SFMono-Regular", Menlo, Consolas, monospace;
      font-size: 13px;
      background: var(--surface-strong);
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 12px 14px;
      max-height: 320px;
      overflow: auto;
      white-space: pre;
    }
    @media (max-width: 860px) { .grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <main>
    ` + pageui.Nav("/utilities") + `

    <h1>Utilities</h1>
    <p class="lede">Back up this server's database, move an entire data set between two disconnected deployments, hand the whole data set to an AI agent, or verify the database is not corrupted before relying on a backup.</p>

    <section class="grid">
      <section class="panel">
        <div class="panel-header">Full database backup</div>
        <div class="panel-body">
          <p class="section-note">Download a raw, engine-native copy of the live database (SQLite's <code>VACUUM INTO</code>). This is the recommended disaster-recovery backup: it is not limited to a fixed table list, so it always carries every row the database actually has, including data added by a future version of this app. Not available when running against PostgreSQL &mdash; use <code>pg_dump</code> there instead.</p>
          <div class="actions">
            <a class="btn" href="/utilities/backup">Download database backup</a>
          </div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-header">Database integrity check</div>
        <div class="panel-body">
          <p class="section-note">Run SQLite's built-in consistency checks (<code>PRAGMA integrity_check</code> and <code>PRAGMA foreign_key_check</code>) before trusting a backup, or if something looks wrong. Read-only &mdash; nothing is changed.</p>
          <div class="actions">
            <button id="integrityBtn" class="secondary" type="button">Run integrity check</button>
          </div>
          <div id="integrityStatus" class="status">Not run yet.</div>
          <div id="integrityReport" class="report" style="display:none;"></div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-header">Export full data set</div>
        <div class="panel-body">
          <p class="section-note">Download a single JSON snapshot containing every managed table &mdash; controls, security NFRs and their links, the risk register, the policy library, Regulation Coverage, Risk &amp; Crisis Exercises, users and stored JSON. Use this to move data to a disconnected deployment, including across a SQLite/PostgreSQL boundary &mdash; the raw backup above only works within the same engine. Deployment configuration and stored credentials are deliberately excluded, so set those again on the destination.</p>
          <div class="actions">
            <a class="btn" href="/utilities/export">Download data set</a>
          </div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-header">Export for the AI agent</div>
        <div class="panel-body">
          <p class="section-note">Download every record this installation holds &mdash; security NFRs, 800-53 controls, regulation clauses and their coverage findings, policy sections, risks, and crisis exercises and their findings &mdash; as one readable JSON document, each record with its full text, its identifier and a link back to its page here. Upload it into a Wintermute agent's library so the agent knows this installation's current state. It is a point-in-time copy stamped with its export date: take a fresh one after a round of edits. An agent that can reach this server should pull <code>/api/knowledge/export</code> instead of being handed a file.</p>
          <div class="actions">
            <a class="btn" href="/utilities/knowledge-export">Download knowledge export</a>
          </div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-header">Import &amp; overwrite database</div>
        <div class="panel-body">
          <p class="section-note">Upload a snapshot exported from another server. Every managed table is replaced with the file's contents.</p>
          <form id="importForm">
            <input id="fileInput" type="file" name="file" accept="application/json,.json" required>
            <div class="warning">This <strong>overwrites</strong> the current database. Any rows on this server that are not in the imported file are permanently deleted. Existing login sessions are cleared, so you may need to sign in again afterward. Take a backup first.</div>
            <div class="actions">
              <button id="importBtn" class="danger" type="submit">Import &amp; overwrite</button>
            </div>
          </form>
          <div id="status" class="status">No import run yet.</div>
          <div id="report" class="report" style="display:none;"></div>
        </div>
      </section>
    </section>
  </main>

  <script>
    const form = document.getElementById('importForm');
    const fileInput = document.getElementById('fileInput');
    const importBtn = document.getElementById('importBtn');
    const status = document.getElementById('status');
    const report = document.getElementById('report');
    const integrityBtn = document.getElementById('integrityBtn');
    const integrityStatus = document.getElementById('integrityStatus');
    const integrityReport = document.getElementById('integrityReport');

    function setStatus(message, kind) {
      status.textContent = message;
      status.className = 'status' + (kind ? ' ' + kind : '');
    }

    function setIntegrityStatus(message, kind) {
      integrityStatus.textContent = message;
      integrityStatus.className = 'status' + (kind ? ' ' + kind : '');
    }

    integrityBtn.addEventListener('click', async () => {
      integrityReport.style.display = 'none';
      integrityReport.textContent = '';
      integrityBtn.disabled = true;
      setIntegrityStatus('Running integrity check...', null);

      try {
        const response = await fetch('/utilities/integrity-check');
        const result = await response.json().catch(() => ({}));
        if (!response.ok) {
          throw new Error(result.error || ('HTTP ' + response.status));
        }
        setIntegrityStatus(result.ok ? 'Database is consistent.' : 'Problems found — see details below.', result.ok ? 'ok' : 'err');
        const lines = [].concat(result.integrity_check || [], result.foreign_key_issues || []);
        if (lines.length) {
          integrityReport.style.display = '';
          integrityReport.textContent = lines.join('\n');
        }
      } catch (error) {
        setIntegrityStatus('Integrity check failed: ' + error.message, 'err');
      } finally {
        integrityBtn.disabled = false;
      }
    });

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const file = fileInput.files[0];
      if (!file) {
        setStatus('Choose a data set file to import.', 'err');
        return;
      }
      if (!window.confirm('Overwrite this server\'s database with "' + file.name + '"? This cannot be undone.')) {
        return;
      }

      report.style.display = 'none';
      report.textContent = '';
      importBtn.disabled = true;
      setStatus('Importing and overwriting database...', null);

      try {
        const body = new FormData();
        body.append('file', file);
        const response = await fetch('/utilities/import', { method: 'POST', body });
        const result = await response.json().catch(() => ({}));
        if (!response.ok) {
          throw new Error(result.error || ('HTTP ' + response.status));
        }
        setStatus('Import complete: ' + result.total + ' rows written across ' + result.tables.length + ' tables.', 'ok');
        report.style.display = '';
        report.textContent = result.tables.map((t) => t.rows.toString().padStart(8, ' ') + '  ' + t.table).join('\n');
      } catch (error) {
        setStatus('Import failed: ' + error.message + ' (the database was left unchanged).', 'err');
      } finally {
        importBtn.disabled = false;
      }
    });
  </script>
</body>
</html>`

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
