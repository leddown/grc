package reporting

import (
	"html/template"
	"time"
)

// Classification is a document sensitivity marking rendered in the running
// header/footer and on the cover banner. The zero value renders no banner.
type Classification string

const (
	ClassificationNone         Classification = ""
	ClassificationPublic       Classification = "PUBLIC"
	ClassificationInternal     Classification = "INTERNAL"
	ClassificationConfidential Classification = "CONFIDENTIAL"
	ClassificationRestricted   Classification = "RESTRICTED"
)

// BannerColor returns a hex color used for the classification banner so the
// marking reads at a glance. Unknown values fall back to a neutral grey.
func (c Classification) BannerColor() string {
	switch c {
	case ClassificationPublic:
		return "#0b5d3b"
	case ClassificationInternal:
		return "#1f6f8b"
	case ClassificationConfidential:
		return "#8b3d2e"
	case ClassificationRestricted:
		return "#5a1d1d"
	default:
		return "#5a544c"
	}
}

func (c Classification) IsZero() bool { return c == ClassificationNone }

// Branding controls the look of generated reports. All values are optional;
// DefaultBranding supplies sensible fallbacks. LogoDataURI must be a self
// contained data: URI (e.g. "data:image/svg+xml;base64,...") — remote URLs are
// intentionally unsupported so the headless browser never makes network
// requests (see the chromedp renderer's SSRF hardening).
type Branding struct {
	OrgName      string
	LogoDataURI  template.URL
	PrimaryColor string // headings, cover accents
	AccentColor  string // table header / highlight
	TextColor    string // body text
	MutedColor   string // secondary text
	FontFamily   string // CSS font stack; must be locally available to Chrome
}

// DefaultBranding returns a neutral, self-contained branding profile that does
// not depend on any external assets or webfonts.
func DefaultBranding() Branding {
	return Branding{
		OrgName:      "Care Lock Consulting",
		PrimaryColor: "#1f2a44",
		AccentColor:  "#8b3d2e",
		TextColor:    "#1c2431",
		MutedColor:   "#5a6473",
		FontFamily:   `"Helvetica Neue", Helvetica, Arial, sans-serif`,
	}
}

// Cover holds the data shown on the report cover page.
type Cover struct {
	Title          string
	Subtitle       string
	Author         string
	Owner          string
	Version        string
	GeneratedAt    time.Time
	Classification Classification
}

// ReportOptions is the caller-facing configuration for a single report render.
// It composes branding, classification, the optional DRAFT watermark, and the
// page geometry, and is independent of which Renderer backend is in use.
type ReportOptions struct {
	Branding       Branding
	Classification Classification
	// Watermark, when non-empty, renders a diagonal repeating watermark on
	// every page (e.g. "DRAFT"). Empty disables it.
	Watermark string
	Cover     Cover
	// GeneratedAt stamps the document; defaults to time.Now() when zero.
	GeneratedAt time.Time
	// Page controls PDF paper geometry and margins.
	Page PageSetup
}

// PageSetup describes the physical PDF page. Defaults to ISO A4 portrait with
// margins that leave room for the running header and footer.
type PageSetup struct {
	WidthInches  float64
	HeightInches float64
	MarginTop    float64
	MarginBottom float64
	MarginLeft   float64
	MarginRight  float64
	Landscape    bool
}

// DefaultPageSetup returns A4 portrait with header/footer-friendly margins.
func DefaultPageSetup() PageSetup {
	return PageSetup{
		WidthInches:  8.27,
		HeightInches: 11.69,
		MarginTop:    0.7,
		MarginBottom: 0.7,
		MarginLeft:   0.6,
		MarginRight:  0.6,
	}
}

func (o *ReportOptions) applyDefaults() {
	if o.Branding.OrgName == "" {
		o.Branding = DefaultBranding()
	}
	def := DefaultBranding()
	if o.Branding.PrimaryColor == "" {
		o.Branding.PrimaryColor = def.PrimaryColor
	}
	if o.Branding.AccentColor == "" {
		o.Branding.AccentColor = def.AccentColor
	}
	if o.Branding.TextColor == "" {
		o.Branding.TextColor = def.TextColor
	}
	if o.Branding.MutedColor == "" {
		o.Branding.MutedColor = def.MutedColor
	}
	if o.Branding.FontFamily == "" {
		o.Branding.FontFamily = def.FontFamily
	}
	if o.GeneratedAt.IsZero() {
		o.GeneratedAt = time.Now()
	}
	if o.Cover.GeneratedAt.IsZero() {
		o.Cover.GeneratedAt = o.GeneratedAt
	}
	if o.Cover.Classification == ClassificationNone {
		o.Cover.Classification = o.Classification
	}
	if o.Page == (PageSetup{}) {
		o.Page = DefaultPageSetup()
	}
}
