package reporting

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ChromeRenderer is the primary, in-process Renderer. It drives a pooled
// headless Chrome/Chromium via the DevTools Protocol and produces PDFs with
// Page.printToPDF, giving high-fidelity CSS (Paged Media, flex/grid, SVG).
//
// Hardening (see Render):
//   - JavaScript is disabled unless RenderOptions.EnableJavaScript is set.
//   - All network loads are blocked, so the headless browser cannot be steered
//     into SSRF or local-file exfiltration via report content; every asset must
//     be inlined (data: URIs).
//   - Content is injected with Page.setDocumentContent rather than navigating to
//     a URL, so no remote/file navigation ever occurs.
//   - Each render is bounded by a context timeout and honors caller cancellation;
//     Chrome processes are cleaned up via the allocator/context cancel funcs.
type ChromeRenderer struct {
	allocCtx context.Context
	cancel   context.CancelFunc
	logger   *slog.Logger

	// browserMu serializes Run calls. chromedp tabs from one allocator share a
	// single browser process (the pool); serializing keeps printToPDF stable
	// and bounds Chrome memory. Swap for a bounded worker pool if throughput
	// becomes a concern.
	browserMu sync.Mutex
}

// blockedURLPatterns blocks every network/file scheme the headless browser
// could be steered into loading from report content, while leaving inline
// data: URIs (which have no "://" authority) usable for embedded assets.
var blockedURLPatterns = []string{
	"http://*",
	"https://*",
	"file://*",
	"ftp://*",
	"ws://*",
	"wss://*",
}

type chromeConfig struct {
	execPath  string
	noSandbox bool
	logger    *slog.Logger
	extra     []chromedp.ExecAllocatorOption
}

// ChromeOption configures a ChromeRenderer.
type ChromeOption func(*chromeConfig)

// WithChromePath pins a specific Chrome/Chromium executable.
func WithChromePath(path string) ChromeOption {
	return func(c *chromeConfig) { c.execPath = path }
}

// WithNoSandbox disables the Chrome sandbox. Required in some containers/CI,
// but it weakens isolation — leave it off when the environment permits.
func WithNoSandbox(noSandbox bool) ChromeOption {
	return func(c *chromeConfig) { c.noSandbox = noSandbox }
}

// WithChromeLogger sets the structured logger used for render diagnostics.
func WithChromeLogger(l *slog.Logger) ChromeOption {
	return func(c *chromeConfig) { c.logger = l }
}

// WithExecAllocatorOptions appends raw chromedp allocator options for advanced
// tuning (extra flags, proxy, etc.).
func WithExecAllocatorOptions(opts ...chromedp.ExecAllocatorOption) ChromeOption {
	return func(c *chromeConfig) { c.extra = append(c.extra, opts...) }
}

// NewChromeRenderer builds a renderer backed by a pooled headless browser. The
// browser process starts lazily on first render. Call Close to release it.
func NewChromeRenderer(opts ...ChromeOption) *ChromeRenderer {
	cfg := chromeConfig{logger: slog.Default()}
	for _, opt := range opts {
		opt(&cfg)
	}

	allocOpts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocOpts = append(allocOpts,
		chromedp.DisableGPU,
		// Defense in depth alongside the per-render network block.
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-default-apps", true),
	)
	if cfg.execPath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(cfg.execPath))
	}
	if cfg.noSandbox {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	allocOpts = append(allocOpts, cfg.extra...)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	return &ChromeRenderer{allocCtx: allocCtx, cancel: cancel, logger: cfg.logger}
}

// Close shuts down the pooled browser and frees its resources. Safe to call
// once; subsequent Render calls will fail.
func (r *ChromeRenderer) Close() error {
	r.cancel()
	return nil
}

// Render implements Renderer.
func (r *ChromeRenderer) Render(ctx context.Context, html []byte, opts RenderOptions) ([]byte, error) {
	if len(html) == 0 {
		return nil, fmt.Errorf("reporting: empty html supplied to renderer")
	}

	r.browserMu.Lock()
	defer r.browserMu.Unlock()

	start := time.Now()

	// Derive a browser tab from the pooled allocator, then bridge caller
	// cancellation and apply the render timeout. Cancelling browserCtx tears
	// down the tab; the allocator keeps the shared browser process alive.
	browserCtx, cancelBrowser := chromedp.NewContext(r.allocCtx)
	defer cancelBrowser()
	stopBridge := context.AfterFunc(ctx, cancelBrowser)
	defer stopBridge()

	runCtx, cancelTimeout := context.WithTimeout(browserCtx, opts.timeout())
	defer cancelTimeout()

	var pdf []byte
	actions := []chromedp.Action{
		network.Enable(),
		// Block remote and local-file schemes so report content can never
		// trigger SSRF or local-file exfiltration. Inline data: assets (logos,
		// fonts) carry no scheme separator and are intentionally still allowed.
		network.SetBlockedURLS(blockedURLPatterns),
		emulation.SetEmulatedMedia().WithMedia("print"),
	}
	if !opts.EnableJavaScript {
		actions = append(actions, emulation.SetScriptExecutionDisabled(true))
	}
	actions = append(actions,
		chromedp.Navigate("about:blank"),
		setDocumentContent(html),
		printToPDF(opts, &pdf),
	)

	if err := chromedp.Run(runCtx, actions...); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("reporting: render cancelled: %w", ctxErr)
		}
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("reporting: render timed out after %s: %w", opts.timeout(), runCtx.Err())
		}
		return nil, fmt.Errorf("reporting: chromedp render failed: %w", err)
	}

	r.logger.Debug("reporting.render",
		slog.String("engine", "chromedp"),
		slog.Int("html_bytes", len(html)),
		slog.Int("pdf_bytes", len(pdf)),
		slog.Duration("elapsed", time.Since(start)),
	)
	return pdf, nil
}

// setDocumentContent injects HTML into the current frame without navigation.
func setDocumentContent(html []byte) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		frameTree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return fmt.Errorf("get frame tree: %w", err)
		}
		if err := page.SetDocumentContent(frameTree.Frame.ID, string(html)).Do(ctx); err != nil {
			return fmt.Errorf("set document content: %w", err)
		}
		return nil
	})
}

// printToPDF runs Page.printToPDF with the given options into out.
func printToPDF(opts RenderOptions, out *[]byte) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		p := page.PrintToPDF().
			WithPrintBackground(opts.PrintBackground).
			WithPreferCSSPageSize(opts.PreferCSSPageSize).
			WithLandscape(opts.Landscape).
			WithPaperWidth(opts.PaperWidthInches).
			WithPaperHeight(opts.PaperHeightInches).
			WithMarginTop(opts.MarginTopInches).
			WithMarginBottom(opts.MarginBottomInches).
			WithMarginLeft(opts.MarginLeftInches).
			WithMarginRight(opts.MarginRightInches).
			WithDisplayHeaderFooter(opts.DisplayHeaderFooter)
		if opts.Scale > 0 {
			p = p.WithScale(opts.Scale)
		}
		if opts.DisplayHeaderFooter {
			p = p.WithHeaderTemplate(opts.HeaderHTML).WithFooterTemplate(opts.FooterHTML)
		}
		data, _, err := p.Do(ctx)
		if err != nil {
			return fmt.Errorf("print to pdf: %w", err)
		}
		*out = data
		return nil
	})
}

// Compile-time assertion that ChromeRenderer satisfies Renderer.
var _ Renderer = (*ChromeRenderer)(nil)
