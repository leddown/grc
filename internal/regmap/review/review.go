// Package review implements the interactive terminal gates. It is the only
// package in the tool permitted to set a status to approved.
package review

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"grc/internal/regmap/nist"
	"grc/internal/regmap/profile"
	"grc/internal/regmap/requirement"
)

// Options configures a review session.
type Options struct {
	In          io.Reader
	Out         io.Writer
	Reviewer    string
	AutoApprove bool
	Catalog     *nist.Catalog
	Profile     *profile.Profile
	// Save is called after every decision so quitting never loses work.
	Save func() error
}

// Outcome summarizes what a gate run decided.
type Outcome struct {
	Approved int
	Rejected int
	Skipped  int
	Quit     bool
}

// session carries the shared prompt plumbing for all three gates.
type session struct {
	in   *bufio.Reader
	out  io.Writer
	opts Options
}

func newSession(opts Options) *session {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	return &session{in: bufio.NewReader(opts.In), out: opts.Out, opts: opts}
}

// IsInteractive reports whether stdin is a terminal. When it is not, the
// caller must refuse to run a gate rather than silently auto-approving.
func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func (s *session) printf(format string, args ...any) {
	fmt.Fprintf(s.out, format, args...)
}

func (s *session) rule() {
	s.printf("%s\n", strings.Repeat("─", 72))
}

// ask prints a prompt and reads one line, returning the trimmed input.
func (s *session) ask(prompt string) (string, error) {
	s.printf("%s", prompt)
	line, err := s.in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		if err == io.EOF {
			return "q", nil
		}
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// askDefault reads a line, returning def when the reviewer just presses enter.
func (s *session) askDefault(prompt, def string) (string, error) {
	if def != "" {
		prompt = fmt.Sprintf("%s [%s]: ", prompt, def)
	} else {
		prompt += ": "
	}
	v, err := s.ask(prompt)
	if err != nil {
		return "", err
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}

// key reads a single keystroke action (entered as a line).
func (s *session) key(prompt string) (string, error) {
	v, err := s.ask(prompt)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", nil
	}
	return strings.ToLower(v[:1]), nil
}

func (s *session) save() error {
	if s.opts.Save == nil {
		return nil
	}
	return s.opts.Save()
}

// resolveEditor turns $EDITOR into an executable path, or returns an error
// explaining why it will not be used.
//
// $EDITOR is local operator configuration, not attacker-controlled input — the
// same trust model git and kubectl use. It is still validated rather than
// executed as given: a bare command name only, resolved through PATH, with no
// embedded arguments or shell metacharacters, so nothing can smuggle extra
// commands in through the variable.
func resolveEditor() (string, error) {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		return "", errNoEditor
	}
	if strings.ContainsAny(editor, " \t;|&$`(){}<>\n\\\"'") {
		return "", fmt.Errorf("$EDITOR %q contains arguments or shell metacharacters; set it to a plain command name", editor)
	}
	path, err := exec.LookPath(editor)
	if err != nil {
		return "", fmt.Errorf("$EDITOR %q not found on PATH: %w", editor, err)
	}
	return path, nil
}

var errNoEditor = errors.New("$EDITOR is not set")

// editText opens $EDITOR on the given content when one is configured, and
// falls back to a single-line prompt otherwise.
func (s *session) editText(label, current string) (string, error) {
	editor, err := resolveEditor()
	if err != nil {
		if !errors.Is(err, errNoEditor) {
			s.printf("  %v\n", err)
		}
		s.printf("  Falling back to a single line (blank keeps the current value).\n")
		v, askErr := s.ask("  " + label + ": ")
		if askErr != nil {
			return "", askErr
		}
		if v == "" {
			return current, nil
		}
		return v, nil
	}

	tmp, err := os.CreateTemp("", "regmap-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file for editor: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.WriteString(current); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	// #nosec G204,G702 -- editor is a PATH-resolved bare command name (see
	// resolveEditor) invoked with one tool-generated temp path and no shell.
	cmd := exec.Command(editor, name)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run %s: %w", filepath.Base(editor), err)
	}
	// #nosec G304 -- name is the tool-generated temp file created just above.
	edited, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read edited file: %w", err)
	}
	return strings.TrimSpace(string(edited)), nil
}

const bodyPreviewChars = 1200

func (s *session) body(text string, full bool) {
	if full || len(text) <= bodyPreviewChars {
		s.printf("%s\n", indent(text, "    "))
		return
	}
	s.printf("%s\n", indent(text[:bodyPreviewChars], "    "))
	s.printf("    ... (%d more characters — press [v] to view in full)\n", len(text)-bodyPreviewChars)
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func (s *session) summary(gate int, o Outcome) {
	s.rule()
	state := "complete"
	if o.Quit {
		state = "paused"
	}
	s.printf("GATE %d %s — approved: %d   rejected: %d   skipped: %d\n", gate, state, o.Approved, o.Rejected, o.Skipped)
	if o.Skipped > 0 || o.Quit {
		s.printf("Unreviewed items stay in draft; re-run `regmap review --gate %d` to finish them.\n", gate)
	}
	s.rule()
}

// queue orders review items: unreviewed only, lowest confidence first, then by
// natural ID so a resumed session picks up predictably.
func queue(reqs requirement.Set) []int {
	var idx []int
	for i, r := range reqs {
		if r.Status == requirement.StatusDraft {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ra, rb := reqs[idx[a]], reqs[idx[b]]
		if ra.Confidence.Rank() != rb.Confidence.Rank() {
			return ra.Confidence.Rank() < rb.Confidence.Rank()
		}
		return requirement.NaturalLess(ra.ID, rb.ID)
	})
	return idx
}

// ConfirmFramework asks the reviewer to confirm the detected framework before
// any segmentation review happens.
func ConfirmFramework(opts Options, detected *profile.Profile, sourceFile string, detectionNote string) (bool, error) {
	s := newSession(opts)
	s.rule()
	s.printf("GATE 1 — confirm the source framework\n")
	s.rule()
	s.printf("  source file : %s\n", sourceFile)
	s.printf("  framework   : %s (%s)\n", detected.DisplayName, detected.ID)
	s.printf("  sourceRef   : %s\n", detected.SourceRef)
	s.printf("  profile     : %s\n", detected.Path())
	if detectionNote != "" {
		s.printf("  detected by : %s\n", detectionNote)
	}
	s.printf("\n")
	if opts.AutoApprove {
		s.printf("  --auto-approve: framework accepted without confirmation.\n\n")
		return true, nil
	}
	for {
		k, err := s.key("Is this the right framework? [y]es / [n]o: ")
		if err != nil {
			return false, err
		}
		switch k {
		case "y":
			s.printf("\n")
			return true, nil
		case "n", "q":
			return false, nil
		default:
			s.printf("  Please answer y or n.\n")
		}
	}
}
