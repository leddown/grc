//go:build pdf_smoke

// Package reporting's PDF smoke test renders a real PDF through headless Chrome.
// It is gated behind the `pdf_smoke` build tag so it never runs in the default
// `go test ./...` security gate (where Chrome may be absent); run it explicitly
// with: go test -tags pdf_smoke ./internal/reporting/ -run RealPDF
package reporting

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRenderRealPDF(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("no Chrome/Chromium found on PATH and REPORTING_CHROME_PATH unset; skipping PDF smoke test")
	}

	var chromeOpts []ChromeOption
	if path := os.Getenv("REPORTING_CHROME_PATH"); path != "" {
		chromeOpts = append(chromeOpts, WithChromePath(path))
	}
	if os.Getenv("REPORTING_CHROME_NO_SANDBOX") == "true" {
		chromeOpts = append(chromeOpts, WithNoSandbox(true))
	}

	renderer := NewChromeRenderer(chromeOpts...)
	defer renderer.Close()
	svc := NewService(renderer, nil)

	cases := []struct {
		name   string
		render func(context.Context) ([]byte, error)
	}{
		{"risk_assessment", func(ctx context.Context) ([]byte, error) {
			return svc.RenderRiskAssessment(ctx, SampleRiskAssessment(), SampleReportOptions())
		}},
		{"control_assessment", func(ctx context.Context) ([]byte, error) {
			return svc.RenderControlAssessment(ctx, SampleControlAssessment(), SampleControlAssessmentOptions())
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			pdf, err := tc.render(ctx)
			if err != nil {
				t.Fatalf("render %s: %v", tc.name, err)
			}
			if len(pdf) == 0 {
				t.Fatal("rendered PDF is empty")
			}
			if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
				t.Errorf("output is not a PDF (missing %%PDF- header): % x", pdf[:min(8, len(pdf))])
			}
			if !bytes.Contains(pdf, []byte("%%EOF")) {
				t.Error("PDF missing EOF trailer")
			}
			t.Logf("rendered %s PDF: %d bytes", tc.name, len(pdf))

			// Opt-in: write to <REPORTING_SMOKE_OUT>/<name>.pdf for inspection.
			if dir := os.Getenv("REPORTING_SMOKE_OUT"); dir != "" {
				path := filepath.Join(dir, tc.name+".pdf")
				if err := os.WriteFile(path, pdf, 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				t.Logf("wrote PDF to %s", path)
			}
		})
	}
}

func chromeAvailable() bool {
	if os.Getenv("REPORTING_CHROME_PATH") != "" {
		return true
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "chrome"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}
