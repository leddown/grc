package doctemplate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RenderTimeout bounds a single typesetting run. Typst renders a policy in well
// under a second; the headroom is for tectonic's first run, which fetches its
// package set before it can compile anything.
const RenderTimeout = 3 * time.Minute

// maxOutputBytes caps a produced PDF. A policy document is a few hundred
// kilobytes; anything approaching this is a runaway template, and reading it
// into memory to hand to a browser is how that becomes the app's problem.
const maxOutputBytes = 64 << 20

// maxEngineLog caps the compiler output kept for display.
const maxEngineLog = 64 << 10

// PDFStandards is the allowlist of --pdf-standard values, in the order the UI
// offers them. See DOCUMENT_TEMPLATES.md: a-2b embeds every font and forbids
// the features that make a PDF render differently on a machine that no longer
// has the originals, which is what a filed policy needs years later. Adding
// ua-1 turns on Typst's accessibility checks, which fail the build if the
// document is not genuinely accessible.
var PDFStandards = []struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Note  string `json:"note"`
}{
	{"a-2b", "PDF/A-2b (archival)", "Default. Embeds every font so the document renders identically years from now."},
	{"a-2b,ua-1", "PDF/A-2b + PDF/UA-1 (accessible)", "Adds tagged, screen-reader-accessible output. Typst fails the build if heading order or alt text is wrong."},
	{"", "Plain PDF 1.7", "No archival or accessibility guarantees. Smallest output."},
}

func validPDFStandard(value string) bool {
	for _, standard := range PDFStandards {
		if standard.Value == value {
			return true
		}
	}
	return false
}

// Request is one render.
type Request struct {
	// TemplateID names a registry entry.
	TemplateID string
	// Data is the JSON render payload.
	Data []byte
	// BaseName is the filename stem for the result, before extension. It is
	// sanitised — it reaches a Content-Disposition header and a zip entry.
	BaseName string
	// PDFStandard is one of PDFStandards. Ignored by the LaTeX path, which has
	// no equivalent switch.
	PDFStandard string
	// Bundle forces the source-bundle response even when the engine is present,
	// for someone who wants to build or tweak the document locally.
	Bundle bool
}

// Result is a rendered document or the sources to render it with.
type Result struct {
	ContentType string
	Filename    string
	Body        []byte
	// Bundled reports that Body is a source zip rather than a PDF, either
	// because the engine is missing or because the caller asked for one.
	Bundled bool
	// Reason explains a bundle that was not asked for.
	Reason string
	// Log is the typesetter's output, kept even on success: Typst reports
	// missing-font fallbacks and overfull boxes as warnings, and those are
	// exactly what someone checking a new brand needs to see.
	Log string
	// Engine and EngineVersion record what produced the result.
	Engine        string
	EngineVersion string
}

// Render produces a PDF, or a source bundle when it cannot.
//
// The two outcomes are deliberately not an error and a success: a host without
// typst installed is an ordinary, expected state for this app — it ships as a
// single Go binary and the typesetters are separate installs — and the useful
// response there is the sources plus a build script, not a 500.
func (s *Service) Render(ctx context.Context, req Request) (Result, error) {
	tpl, ok := Lookup(req.TemplateID)
	if !ok {
		return Result{}, fmt.Errorf("unknown template %q", req.TemplateID)
	}
	if len(bytes.TrimSpace(req.Data)) == 0 {
		return Result{}, fmt.Errorf("no render payload supplied")
	}
	if !json.Valid(req.Data) {
		return Result{}, fmt.Errorf("render payload is not valid JSON")
	}
	if req.PDFStandard != "" && !validPDFStandard(req.PDFStandard) {
		return Result{}, fmt.Errorf("unsupported PDF standard %q", req.PDFStandard)
	}
	if !DirAvailable() {
		return Result{}, fmt.Errorf("document templates not found at %s — the templates/ directory is missing from this installation", Dir())
	}

	brand, err := s.Brand()
	if err != nil {
		return Result{}, err
	}
	// Re-validated on the way out, not only on the way in: a row written by an
	// older build of this code must not reach a typesetter unchecked.
	if err := brand.Validate(); err != nil {
		return Result{}, fmt.Errorf("saved brand settings are invalid: %w", err)
	}

	workspace, err := os.MkdirTemp("", "grc-render-")
	if err != nil {
		return Result{}, fmt.Errorf("creating render workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	if err := buildWorkspace(workspace, tpl, brand, req.Data); err != nil {
		return Result{}, err
	}

	base := sanitiseBaseName(req.BaseName)
	status := StatusFor(tpl.Engine)

	if req.Bundle || !status.Available {
		body, err := bundleWorkspace(workspace, tpl, brand, req.PDFStandard)
		if err != nil {
			return Result{}, err
		}
		result := Result{
			ContentType: "application/zip",
			Filename:    bundleFilename(base, tpl.ID),
			Body:        body,
			Bundled:     true,
			Engine:      string(tpl.Engine),
		}
		if !status.Available {
			result.Reason = fmt.Sprintf("No %s engine on this host (%s), so the sources are returned instead. Unzip and run ./build.sh.", tpl.Engine, status.Detail)
		}
		return result, nil
	}

	log, err := compile(ctx, workspace, tpl, status, req.PDFStandard)
	if err != nil {
		return Result{Log: log}, err
	}

	pdf, err := readOutput(filepath.Join(workspace, outputName))
	if err != nil {
		return Result{Log: log}, err
	}
	return Result{
		ContentType:   "application/pdf",
		Filename:      base + ".pdf",
		Body:          pdf,
		Log:           log,
		Engine:        status.Command,
		EngineVersion: status.Version,
	}, nil
}

// outputName is what every engine is told to write. Fixed so the read-back
// never has to guess, and so the LaTeX path cannot collide with the Typst one
// the way build.sh once did.
const outputName = "grc-output.pdf"

// dataName is the payload file inside the workspace. The Typst templates
// resolve their `--input data=` path relative to the template file, and both
// live at the workspace root.
const dataName = "data.json"

// buildWorkspace assembles a self-contained directory holding the template, its
// dependencies, the brand and the payload.
//
// Self-contained is the point: the Typst run is rooted here, so a template can
// only read files this function put in place. build.sh roots the compile at the
// repository, which is fine for a developer building their own document and not
// fine for a server rendering on request.
func buildWorkspace(workspace string, tpl Template, brand Brand, data []byte) error {
	templatesDir := Dir()
	for _, source := range tpl.Sources {
		// #nosec G304 -- source comes from the compile-time template registry,
		// not from request input; the directory is resolved configuration.
		content, err := os.ReadFile(filepath.Join(templatesDir, filepath.FromSlash(source)))
		if err != nil {
			return fmt.Errorf("reading template source %s: %w", source, err)
		}
		name := filepath.Base(source)
		if name == "brand.typ" {
			rewritten, err := ApplyToTypstBrand(string(content), brand)
			if err != nil {
				return fmt.Errorf("applying brand settings: %w", err)
			}
			content = []byte(rewritten)
		}
		// #nosec G703 -- name is filepath.Base of a compile-time registry entry,
		// so it carries no directory component, and the join target is the
		// temporary workspace this process just created. No request input
		// reaches the path.
		if err := os.WriteFile(filepath.Join(workspace, name), content, 0o600); err != nil {
			return fmt.Errorf("writing %s into the render workspace: %w", name, err)
		}
	}

	if tpl.Generated {
		payload, err := DecodePolicyPayload(data)
		if err != nil {
			return err
		}
		generated := GenerateLaTeX(payload, brand)
		if err := os.WriteFile(filepath.Join(workspace, tpl.Entry), []byte(generated), 0o600); err != nil {
			return fmt.Errorf("writing generated %s: %w", tpl.Entry, err)
		}
	}

	// Written for every template, including the generated LaTeX one: it is the
	// input the bundle's recipient needs to regenerate or diff the document.
	if err := os.WriteFile(filepath.Join(workspace, dataName), data, 0o600); err != nil {
		return fmt.Errorf("writing render payload: %w", err)
	}
	return nil
}

// compile runs the typesetter and returns its output.
func compile(ctx context.Context, workspace string, tpl Template, status EngineStatus, pdfStandard string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, RenderTimeout)
	defer cancel()

	var args []string
	switch tpl.Engine {
	case EngineTypst:
		args = []string{
			"compile",
			// Rooted at the workspace, so the template cannot read anything
			// this render did not put there.
			"--root", workspace,
			"--input", "data=" + dataName,
		}
		if pdfStandard != "" {
			args = append(args, "--pdf-standard", pdfStandard)
		}
		args = append(args, tpl.Entry, outputName)
	case EngineLaTeX:
		switch status.Command {
		case "tectonic":
			args = []string{"-X", "compile", "--outdir", ".", tpl.Entry}
		default:
			// XeLaTeX, not pdfLaTeX: grc.sty uses fontspec for the font
			// stack, and pdfLaTeX cannot load it.
			args = []string{"-xelatex", "-interaction=nonstopmode", "-outdir=.", tpl.Entry}
		}
	default:
		return "", fmt.Errorf("no compile command for engine %q", tpl.Engine)
	}

	log, runErr := run(ctx, workspace, status.Path, args)

	if tpl.Engine == EngineLaTeX {
		// Both LaTeX drivers name the output after the input, so it is renamed
		// rather than directed — neither accepts an output filename the way
		// typst does.
		produced := filepath.Join(workspace, strings.TrimSuffix(tpl.Entry, filepath.Ext(tpl.Entry))+".pdf")
		if _, statErr := os.Stat(produced); statErr == nil {
			if err := os.Rename(produced, filepath.Join(workspace, outputName)); err != nil {
				return log, fmt.Errorf("collecting the LaTeX output: %w", err)
			}
		}
	}

	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return log, fmt.Errorf("%s timed out after %s", status.Command, RenderTimeout)
		}
		return log, fmt.Errorf("%s failed: %w", status.Command, runErr)
	}
	return log, nil
}

func run(ctx context.Context, dir, binary string, args []string) (string, error) {
	// #nosec G204 -- binary is resolved by engine.go from a compile-time
	// command allowlist or operator-set environment variable, and every
	// argument is either a constant or a path inside the temporary workspace
	// this process just created. No request input reaches the argument list.
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = sandboxEnv()
	// No stdin: latexmk prompts on error and would otherwise block until the
	// context deadline rather than failing immediately.
	cmd.Stdin = bytes.NewReader(nil)

	var buf bytes.Buffer
	writer := &limitedWriter{w: &buf, remaining: maxEngineLog}
	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()
	log := buf.String()
	if writer.dropped > 0 {
		log += fmt.Sprintf("\n… %d further bytes of output suppressed.", writer.dropped)
	}
	return filterEngineLog(log), err
}

// fontWarning matches the "unknown font family" lines Typst prints for every
// name in a fallback chain that is absent locally, even when an earlier one
// matched. build.sh filters exactly these and nothing else; the reason is the
// same here. They are expected and not actionable, and leaving them in makes a
// reader distrust the warnings that are.
var fontWarning = regexp.MustCompile(`(?i)unknown font family`)

// diagnosticContext matches the framed source excerpt Typst prints beneath a
// diagnostic — the box-drawing gutter, the numbered source line and the caret
// underline.
var diagnosticContext = regexp.MustCompile(`^\s*(?:[┌│└├┘─]|\d+ *│|\^+\s*$)`)

// isDiagnosticContext reports whether a line belongs to the diagnostic above it
// rather than starting a new one. A blank line counts, because it is the
// separator Typst puts at the end of the block.
func isDiagnosticContext(line string) bool {
	return strings.TrimSpace(line) == "" || diagnosticContext.MatchString(line)
}

func filterEngineLog(log string) string {
	lines := strings.Split(log, "\n")
	kept := make([]string, 0, len(lines))
	suppressed := 0
	for i := 0; i < len(lines); i++ {
		if !fontWarning.MatchString(lines[i]) {
			kept = append(kept, lines[i])
			continue
		}
		suppressed++
		// The warning's source excerpt goes with it. Dropping the warning line
		// alone leaves a frame pointing at brand.typ with nothing saying why,
		// which reads as an error in a file the user did not write.
		for i+1 < len(lines) && isDiagnosticContext(lines[i+1]) {
			i++
		}
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if suppressed > 0 {
		if out != "" {
			out += "\n\n"
		}
		out += fmt.Sprintf("(%d 'unknown font family' warnings hidden — expected, the brand fonts are fallback chains and the typesetter names every family it skipped.)", suppressed)
	}
	return out
}

func readOutput(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("the typesetter reported success but produced no PDF: %w", err)
	}
	if info.Size() > maxOutputBytes {
		return nil, fmt.Errorf("rendered PDF is %d bytes, over the %d byte limit", info.Size(), maxOutputBytes)
	}
	// #nosec G304 -- path is the fixed output name inside the temporary
	// workspace this process created.
	pdf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the rendered PDF: %w", err)
	}
	return pdf, nil
}

// bundleFilename names a source zip. The template id is part of it so the two
// bundles for one document — the Typst one and the LaTeX one — do not collide
// in a downloads folder, but it is not repeated when the stem already carries
// it, which is the case for a rendered sample.
func bundleFilename(base, templateID string) string {
	if strings.Contains(base, templateID) {
		return base + "-sources.zip"
	}
	return base + "-" + templateID + "-sources.zip"
}

// unsafeFilenameChars matches everything not allowed in a download name;
// repeatedHyphens tidies the runs that collapsing them leaves behind.
var (
	unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	repeatedHyphens     = regexp.MustCompile(`-{2,}`)
)

// sanitiseBaseName reduces a caller-supplied stem to something safe for a
// Content-Disposition header and a zip entry. Reference strings are the usual
// input ("POL-AC-001") and survive unchanged; anything else collapses to
// hyphens rather than being rejected, because a document with an awkward title
// should still download.
func sanitiseBaseName(name string) string {
	name = unsafeFilenameChars.ReplaceAllString(strings.TrimSpace(name), "-")
	name = repeatedHyphens.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-._")
	if len(name) > 80 {
		name = strings.Trim(name[:80], "-._")
	}
	if name == "" {
		return "document"
	}
	return name
}
