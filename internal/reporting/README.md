# reporting — HTML/CSS → PDF report generation

`internal/reporting` turns structured GRC data (risk registers, control
matrices, audit findings, assessment results) into high-fidelity, branded PDF
reports. It renders Go structs to HTML with `html/template`, then converts that
HTML to PDF through a pluggable `Renderer`.

```
data structs ─▶ html/template (auto-escaped) ─▶ Renderer ─▶ PDF bytes
                 base + cover + table + report      (chromedp today,
                 partials, server-rendered SVG       Gotenberg later)
```

## Quick start

```go
renderer := reporting.NewChromeRenderer()          // pooled headless Chrome
defer renderer.Close()
svc := reporting.NewService(renderer, logger)      // logger may be nil

pdf, err := svc.RenderRiskAssessment(ctx,
    reporting.SampleRiskAssessment(),              // your data
    reporting.SampleReportOptions(),               // branding/classification/etc.
)
```

The example HTTP endpoint (wired in `internal/app/app.go`) streams the sample
report:

```
GET /reports/risk-assessment.pdf            # inline, DRAFT watermark
GET /reports/risk-assessment.pdf?draft=false&download=1
```

## What it produces

- **Cover page**: title, subtitle, org logo, classification banner, author,
  owner, version, generated timestamp.
- **Running header/footer** on every page with **Page X of Y** (via chromedp
  `headerTemplate`/`footerTemplate` and its `pageNumber`/`totalPages` spans).
- **Classification banner** (cover + running header) and an optional diagonal
  **DRAFT watermark**, both toggled by `ReportOptions`.
- **Paginating tables** (risk register, control matrix) whose header row
  repeats on each page (`thead { display: table-header-group }`) and whose rows
  never split (`break-inside: avoid`).
- **Keep-together findings** so each finding block stays on one page.
- **Charts as inline SVG**, generated server-side (`charts.go`) for
  deterministic, reproducible output — no JS render-wait timing.

## Adding a new report type

1. **Model** — add your data struct(s) to `document.go` (or a new file). Keep
   any presentation helpers as methods (e.g. `Severity.Color`, `RiskRow.Score`).
   If your report has tables, add `MyReport.SomeTable() Table` methods (see
   `tables.go`) so the reusable `datatable` partial can render them.
2. **Template** — add `templates/reports/my_report.tmpl` defining `{{define
   "content"}}…{{end}}`. It receives your data as `.` (i.e. `Document.Data`).
   Reuse the `cover`, `datatable`, and header/footer partials; the base layout,
   theme CSS, watermark, and pagination rules come for free from `base.tmpl`.
3. **Service method** — add `RenderMyReport(ctx, data, opts)` to `service.go`
   that calls the shared `render(...)` pipeline with your template file name.
   Add a `MyReportHTML(...)` helper too if you want a browser-free HTML path.
4. **Handler** (optional) — expose it in `handler.go` / `RegisterRoutes`.
5. **Tests** — add a golden HTML test (fast, no browser) like
   `TestRiskAssessmentHTMLGolden`; the `pdf_smoke` test already covers the PDF
   path generically.

Everything in a template is auto-escaped by `html/template`. Only feed
`template.HTML`/`template.CSS`/`template.URL` values you generated yourself
(e.g. inline SVG charts), never untrusted input.

## Renderer backends: chromedp vs Gotenberg

The `Renderer` interface (`renderer.go`) decouples report building from the PDF
engine:

```go
type Renderer interface {
    Render(ctx context.Context, html []byte, opts RenderOptions) ([]byte, error)
}
```

| | **chromedp** (shipped) | **Gotenberg** (stub: `gotenberg_renderer.go`) |
|---|---|---|
| Model | Headless Chrome **in-process** via DevTools | Chrome wrapped behind an **HTTP service** |
| Setup | Just needs a Chrome binary on the host | Run/deploy a Gotenberg container |
| Fidelity | Full CSS (Paged Media, flex/grid, SVG) | Same (also Chromium) |
| Isolation | Browser runs inside the app process | Browser isolated in its own service |
| **PDF/A** | **Not supported** by `printToPDF` | **Supported** (PDF/A-1/2/3, PDF/UA) |
| Scaling | Bounded by the app process | Scale the service independently |

**Why both matter:** chromedp is the easiest possible start — no extra
infrastructure. Gotenberg trades a deployment dependency for process isolation
and, crucially, **PDF/A** output for long-term compliance archival/record
retention. The interface lets you switch by changing one line at startup
(`reporting.NewService(gotenberg, logger)`); no report code changes.

The Gotenberg implementation itself is intentionally out of scope for now —
`GotenbergRenderer` returns `ErrNotImplemented` and documents the intended
mapping (multipart form fields, `pdfa=PDF/A-2b`, header/footer files).

## Chrome/Chromium runtime dependency

The chromedp backend needs a Chrome/Chromium binary at runtime (not a Go
dependency — it drives an external browser).

- **Dev**: install Google Chrome or Chromium; chromedp auto-discovers it.
- **CI**: the unit/golden tests need **no** browser. The real-PDF smoke test is
  behind the `pdf_smoke` build tag and `t.Skip`s when no browser is found, so
  `go test ./...` (the security gate) stays browser-free. Run it explicitly:
  `go test -tags pdf_smoke ./internal/reporting/`.
- **Prod/containers**: install Chromium in the image. Containers often need
  `--no-sandbox`; set `REPORTING_CHROME_NO_SANDBOX=true` (weaker isolation —
  use only where required). Pin a specific binary with `REPORTING_CHROME_PATH`.

App-level environment variables (see `internal/app/app.go`):

| Variable | Effect |
|---|---|
| `REPORTING_CHROME_PATH` | Path to the Chrome/Chromium executable |
| `REPORTING_CHROME_NO_SANDBOX` | `true` to launch with `--no-sandbox` |

## Security model

- **Auto-escaping**: all report data flows through `html/template`, so untrusted
  GRC content (risk titles, finding text, owners) cannot inject markup.
  Branding colors/fonts are additionally validated (`theme.go`).
- **No JavaScript**: the renderer disables script execution unless
  `RenderOptions.EnableJavaScript` is set. Reports are static server-rendered
  HTML, so JS stays off — smaller attack surface, deterministic output.
- **No network / no SSRF**: the renderer blocks all `http(s)`, `file`, `ftp`,
  and `ws(s)` loads, and injects content with `setDocumentContent` instead of
  navigating to a URL. Assets must be **inlined as `data:` URIs** (the sample
  logo is). The browser can never be steered into SSRF or local-file
  exfiltration from report content.
- **Resource hygiene**: one pooled browser process; each render is bounded by a
  context timeout and honors caller cancellation; tabs/processes are cleaned up
  via the allocator/context cancel funcs.

## Observability

`Service.render` logs `reporting.generated` (report type, classification,
watermark flag, html/pdf sizes, elapsed) and the renderer logs
`reporting.render` at debug, both via the injected `*slog.Logger`.

## Known limitations / extension points (TODO)

- **PDF/A** requires the Gotenberg backend (chromedp `printToPDF` cannot emit
  it). Interface + docs only for now.
- **Digital signatures** are not implemented (sign the produced bytes
  downstream, e.g. with a PDF signing service).
- **Async/queued generation** for large batches is out of scope; renders are
  synchronous and serialized per browser. Add a worker pool / job queue if you
  need throughput.
- **No template-management UI**; report templates are compiled-in via `embed`.
- Webfonts must be **locally installed** for Chrome (no network); prefer system
  font stacks or inline `@font-face` with `data:` URIs.
