package reporting

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
)

// hexColor matches #rgb, #rrggbb and #rrggbbaa.
var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// fontStackChars restricts a CSS font stack to a safe character set so a
// branding override cannot break out of the CSS context (defense in depth on
// top of html/template; branding may be operator-supplied).
var fontStackChars = regexp.MustCompile(`[^a-zA-Z0-9 ,"'\-]`)

// sanitizeColor returns c if it is a valid hex color, otherwise fallback.
func sanitizeColor(c, fallback string) string {
	if hexColor.MatchString(strings.TrimSpace(c)) {
		return strings.TrimSpace(c)
	}
	return fallback
}

// sanitizeFontStack strips anything outside a conservative allowlist.
func sanitizeFontStack(f, fallback string) string {
	cleaned := fontStackChars.ReplaceAllString(f, "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

// ThemeCSS builds the :root custom-property block from branding. It returns
// template.CSS because the values are validated here; the surrounding
// stylesheet in the template is static and therefore safe.
func (d Document) ThemeCSS() template.CSS {
	def := DefaultBranding()
	primary := sanitizeColor(d.Branding.PrimaryColor, def.PrimaryColor)
	accent := sanitizeColor(d.Branding.AccentColor, def.AccentColor)
	text := sanitizeColor(d.Branding.TextColor, def.TextColor)
	muted := sanitizeColor(d.Branding.MutedColor, def.MutedColor)
	font := sanitizeFontStack(d.Branding.FontFamily, def.FontFamily)
	banner := d.Classification.BannerColor()

	css := fmt.Sprintf(
		":root{--primary:%s;--accent:%s;--text:%s;--muted:%s;--banner:%s;--font:%s;}",
		primary, accent, text, muted, banner, font,
	)
	// #nosec G203 -- values are validated by sanitizeColor/sanitizeFontStack above.
	return template.CSS(css)
}
