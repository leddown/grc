package reporting

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by renderer backends that are stubs.
var ErrNotImplemented = errors.New("reporting: renderer not implemented")

// GotenbergRenderer is a placeholder for a second Renderer backed by a
// Gotenberg service (https://gotenberg.dev) — an HTTP API wrapping Chromium and
// LibreOffice. It is intentionally unimplemented (out of scope for now); the
// type exists to lock in the extension point and document the intended config.
//
// Why a second backend matters:
//   - It isolates the browser dependency into a separately deployed/scaled
//     service instead of running Chrome in-process.
//   - Gotenberg can emit PDF/A (and PDF/UA), which the in-process chromedp
//     printToPDF path cannot — relevant for long-term compliance archival.
//
// To implement: POST the rendered HTML (plus inlined assets) to the Gotenberg
// Chromium route, map ReportOptions/RenderOptions onto the multipart form
// fields (paperWidth, marginTop, printBackground, header/footer files,
// pdfa=PDF/A-2b, ...), and stream back the response body. Keep the Render
// signature identical so callers are unaffected.
type GotenbergRenderer struct {
	// BaseURL is the Gotenberg endpoint, e.g. "http://gotenberg:3000".
	BaseURL string
	// PDFA, when set (e.g. "PDF/A-2b"), requests archival output.
	PDFA string
	// TODO: http client, auth, per-render timeout, asset bundling.
}

// Render implements Renderer. TODO: build the multipart request and call the
// Gotenberg Chromium convert route.
func (g *GotenbergRenderer) Render(_ context.Context, _ []byte, _ RenderOptions) ([]byte, error) {
	return nil, ErrNotImplemented
}

// Compile-time assertion that GotenbergRenderer satisfies Renderer.
var _ Renderer = (*GotenbergRenderer)(nil)
