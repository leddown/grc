package reporting

import (
	"context"
	"time"
)

// Renderer converts a complete HTML document into PDF bytes. Keeping this
// behind an interface lets the rest of the app stay agnostic about the PDF
// engine: the in-process chromedp implementation ships today, and a Gotenberg
// (HTTP/Chromium, PDF/A-capable) implementation can be dropped in later without
// touching any caller. See README.md for the tradeoff.
type Renderer interface {
	Render(ctx context.Context, html []byte, opts RenderOptions) ([]byte, error)
}

// RenderOptions is the engine-level configuration for a single conversion. It
// is derived from the higher-level ReportOptions by the Service; callers
// generating reports do not normally construct it directly.
type RenderOptions struct {
	// Paper geometry, in inches.
	PaperWidthInches   float64
	PaperHeightInches  float64
	MarginTopInches    float64
	MarginBottomInches float64
	MarginLeftInches   float64
	MarginRightInches  float64
	Landscape          bool

	// PrintBackground includes background colors/images (needed for banners).
	PrintBackground bool
	// Scale is the page rendering scale; 0 means engine default (1.0).
	Scale float64
	// PreferCSSPageSize lets an @page rule in CSS override the paper geometry.
	PreferCSSPageSize bool

	// DisplayHeaderFooter toggles the running header/footer. When set, the
	// engine substitutes pageNumber/totalPages/title/date placeholder spans.
	DisplayHeaderFooter bool
	HeaderHTML          string
	FooterHTML          string

	// EnableJavaScript allows scripts to run during rendering. Default false:
	// reports are static server-rendered HTML, so JS stays off to shrink the
	// attack surface and keep output deterministic.
	EnableJavaScript bool

	// Timeout bounds a single render; 0 falls back to DefaultRenderTimeout.
	Timeout time.Duration
}

// DefaultRenderTimeout bounds a single PDF render when none is supplied.
const DefaultRenderTimeout = 30 * time.Second

func (o RenderOptions) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return DefaultRenderTimeout
}
