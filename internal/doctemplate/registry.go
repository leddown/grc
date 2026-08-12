// Package doctemplate is the web front end for the typesetting templates in
// templates/: the registry of what can be rendered, the brand settings that
// parameterise them, and the pipeline that turns a policy document into a PDF.
//
// It deliberately knows nothing about internal/policydocs. The render payload
// arrives as JSON matching the contract in DOCUMENT_TEMPLATES.md, and the
// wiring in internal/app is what connects a document id to this package. That
// keeps the dependency pointing outward and lets the same pipeline render a
// payload that came from somewhere else later (the corpus, an import, a test).
package doctemplate

import (
	"os"
	"path/filepath"
	"sync"
)

// Engine identifies the typesetter a template needs. The distinction matters to
// callers because the two have completely different availability stories:
// Typst is one static binary, LaTeX is either tectonic (which downloads its
// package set on first run) or a full TeX distribution.
type Engine string

const (
	EngineTypst Engine = "typst"
	EngineLaTeX Engine = "latex"
)

// Kind is the shape of the render payload a template expects. Templates and
// payloads are matched on it so the UI cannot offer to render a policy through
// the business-report template, which would silently produce a PDF with every
// field blank.
type Kind string

const (
	KindPolicy Kind = "policy"
	KindReport Kind = "report"
)

// Template describes one renderable document template.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Engine      Engine `json:"engine"`
	Kind        Kind   `json:"kind"`
	Description string `json:"description"`

	// Sources are copied into the render workspace, given as paths relative to
	// the templates directory. They are flattened: templates/typst/lib.typ
	// lands in the workspace as lib.typ, which is what makes the templates'
	// same-directory imports resolve inside the sandbox.
	Sources []string `json:"-"`

	// Entry is the workspace file the engine compiles.
	Entry string `json:"entry"`

	// Generated marks a template whose entry file this package writes from the
	// render payload rather than copying from disk. Only the LaTeX policy
	// template is: LaTeX cannot load JSON, so the .tex is produced by
	// latexgen.go. See templates/latex/policy-document.tex, which is the
	// hand-filled reference the generator mirrors.
	Generated bool `json:"generated"`

	// SampleData is the templates-dir-relative payload used for brand previews
	// and for the "render the sample" action on the gallery.
	SampleData string `json:"-"`
}

// Templates is the registry, in the order the gallery lists them.
var Templates = []Template{
	{
		ID:          "policy-typst",
		Name:        "Policy document (Typst)",
		Engine:      EngineTypst,
		Kind:        KindPolicy,
		Description: "Policy, standard, procedure or work instruction. Cover page, document control block, table of contents, per-section control claims, control mapping table and revision history. The primary path — this is the one to use unless a client's house toolchain is LaTeX.",
		Sources:     []string{"typst/policy-document.typ", "typst/brand.typ", "typst/lib.typ"},
		Entry:       "policy-document.typ",
		SampleData:  "samples/policy-sample.json",
	},
	{
		ID:          "report-typst",
		Name:        "Business report (Typst)",
		Engine:      EngineTypst,
		Kind:        KindReport,
		Description: "Assessment, gap analysis and control review reports. Executive summary callout, findings tables and rated observations. Has no in-app data producer yet, so it renders from a supplied payload or from the bundled sample.",
		Sources:     []string{"typst/business-report.typ", "typst/brand.typ", "typst/lib.typ"},
		Entry:       "business-report.typ",
		SampleData:  "samples/report-sample.json",
	},
	{
		ID:          "policy-latex",
		Name:        "Policy document (LaTeX)",
		Engine:      EngineLaTeX,
		Kind:        KindPolicy,
		Description: "The same policy document typeset through XeLaTeX, for clients whose house style is already LaTeX. The .tex is generated from the payload rather than read from disk, because LaTeX has no native data loading.",
		Sources:     []string{"latex/carelock.sty"},
		Entry:       "policy-document.tex",
		Generated:   true,
		SampleData:  "samples/policy-sample.json",
	},
}

// Lookup returns the template with the given id.
func Lookup(id string) (Template, bool) {
	for _, tpl := range Templates {
		if tpl.ID == id {
			return tpl, true
		}
	}
	return Template{}, false
}

// DefaultTemplateFor returns the template used when a caller names a payload
// kind but not a specific template.
func DefaultTemplateFor(kind Kind) (Template, bool) {
	for _, tpl := range Templates {
		if tpl.Kind == kind {
			return tpl, true
		}
	}
	return Template{}, false
}

// ---- Templates directory resolution ----

// The templates are data files, not embeddable assets: go:embed only reaches
// inside the importing package's directory, and the templates deliberately live
// at templates/ where build.sh, a designer editing brand.typ, and this package
// can all see the same copy. So the directory is resolved at runtime, the same
// way internal/app resolves the markdown docs — a server started by systemd has
// "/" as its working directory, and a bare relative path would find nothing.

// DirOverride is set once at startup from the -templates-dir flag. When set it
// is used as-is with no fallback search: an operator who names a directory
// wants to be told it is wrong rather than silently served another copy.
var DirOverride string

var (
	resolveDirOnce sync.Once
	resolvedDir    string
)

// dirMarker identifies a candidate directory as the templates root.
const dirMarker = "typst/brand.typ"

// dirCandidates lists the directories searched when no override is set: the
// working directory (a checkout, or `go run` during development), a templates/
// child of it, the directory holding the binary, and the share directories the
// deploy scripts install into.
func dirCandidates() []string {
	candidates := []string{"templates", "."}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		binDir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(binDir, "templates"),
			filepath.Join(binDir, "..", "share", "carelockconsulting", "templates"),
		)
	}
	return append(candidates,
		"/usr/local/share/carelockconsulting/templates",
		"/usr/share/carelockconsulting/templates",
	)
}

// Dir returns the directory the document templates are read from. The search
// runs once per process: the answer cannot change while the binary runs, and
// doing it per request would stat up to six directories on every render.
func Dir() string {
	if DirOverride != "" {
		return DirOverride
	}
	resolveDirOnce.Do(func() {
		resolvedDir = findDir(dirCandidates())
	})
	return resolvedDir
}

// findDir returns the first candidate holding the marker file, or "templates"
// when none does — so a failure is reported against the path a reader would
// expect rather than against the last directory searched.
func findDir(candidates []string) string {
	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(candidate, dirMarker)); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "templates"
}

// DirAvailable reports whether the templates directory was actually found. The
// gallery surfaces this: without it every render fails, and the reason is an
// installation problem rather than anything the user did.
func DirAvailable() bool {
	info, err := os.Stat(filepath.Join(Dir(), dirMarker))
	return err == nil && !info.IsDir()
}
