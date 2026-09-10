// Package profiles embeds the framework profiles — the rules that cut a
// regulation into its articles — so the server reads them from the binary
// rather than from a directory next to it. Adding a regulation means writing a
// YAML file here and rebuilding, not editing the segmenter.
package profiles

import "embed"

//go:embed *.yaml
var FS embed.FS
