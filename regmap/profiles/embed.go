// Package profiles embeds the framework profiles so the server can read them
// without a directory next to the binary. The regmap CLI still loads them from
// disk (--profiles-dir), which is what makes a profile editable without a
// rebuild; the web module cannot rely on that and reads this copy instead.
//
// The files are the same ones either way — this package is an embed directive
// over them, not a second copy.
package profiles

import "embed"

//go:embed *.yaml
var FS embed.FS
