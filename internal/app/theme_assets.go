package app

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// themeAssetsFS holds what a theme needs beyond CSS: the Trench Crusade
// theme's two faces and its ornaments. A blackletter is the one face no system
// font stack can be trusted to have, so the faces are bundled, with their SIL
// OFL licences served beside them.
//
//go:embed themeassets
var themeAssetsFS embed.FS

// themeAssetTypes is the allowlist: only these files are served, each with the
// type it is.
var themeAssetTypes = map[string]string{
	"EBGaramond-Roman.woff2":  "font/woff2",
	"EBGaramond-Italic.woff2": "font/woff2",
	"GrenzeGotisch.woff2":     "font/woff2",
	"OFL-EBGaramond.txt":      "text/plain; charset=utf-8",
	"OFL-GrenzeGotisch.txt":   "text/plain; charset=utf-8",
	"grain.svg":               "image/svg+xml",
	"wire.svg":                "image/svg+xml",
	"cross.svg":               "image/svg+xml",
	"idol.svg":                "image/svg+xml",
}

// themeAsset serves /theme-assets/:file. The path is outside the protected
// prefixes on purpose: the sign-in page is themed too, before anyone has a
// session.
func themeAsset(c *gin.Context) {
	name := c.Param("file")
	contentType, ok := themeAssetTypes[name]
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	data, err := themeAssetsFS.ReadFile("themeassets/" + name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=604800")
	c.Header("X-Content-Type-Options", "nosniff")
	// An SVG opened on its own is a document; this one has nothing to run.
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	c.Data(http.StatusOK, contentType, data)
}
