package oscaltest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TrestleBinary returns the compliance-trestle command, and skips t under
// -short or when trestle is not on PATH. trestle is an optional validator run
// in its own process, like Chrome and LibreOffice for other tests; grc never
// needs it, or Python, to build or run.
func TrestleBinary(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs compliance-trestle")
	}
	bin, err := exec.LookPath("trestle")
	if err != nil {
		t.Skip("trestle is not on PATH: compliance-trestle does not check these documents")
	}
	return bin
}

var trestleModelName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Trestle has compliance-trestle read the documents as its own model does: it
// writes each one to a file, imports it into a new trestle workspace under the
// given name (`trestle import -f <file> -o <name>`), then validates the whole
// workspace (`trestle validate -a`). It fails t when an import or validate
// fails, and when validate does not report every model valid. It also has
// trestle canonicalize each document (`trestle canonicalize -f <file>`, RFC
// 8785) and fails t unless that gives back the same bytes: grc writes
// canonical JSON, and a signature over an export (OSCAL.md, Signing) relies
// on it.
func Trestle(t testing.TB, docs map[string][]byte) {
	t.Helper()
	bin := TrestleBinary(t)
	dir := t.TempDir()
	// trestle refuses to import a file from inside its own workspace.
	exports, root := filepath.Join(dir, "exports"), filepath.Join(dir, "workspace")
	for _, d := range []string{exports, root} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := func(args ...string) *exec.Cmd {
		// #nosec G204 -- bin is compliance-trestle found by exec.LookPath; the
		// arguments are fixed subcommands, names checked against
		// trestleModelName and paths under t.TempDir.
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = root
		cmd.Env = trestleEnv()
		return cmd
	}
	run := func(args ...string) (string, error) {
		out, err := command(args...).CombinedOutput()
		return string(out), err
	}
	if out, err := run("init"); err != nil {
		t.Fatalf("trestle init: %v\n%s", err, out)
	}

	names := make([]string, 0, len(docs))
	for name := range docs {
		if !trestleModelName.MatchString(name) {
			t.Fatalf("%q is not a model name trestle takes", name)
		}
		names = append(names, name)
	}
	slices.Sort(names)
	// Each import writes only its own model directory, so they run side by
	// side: one at a time, trestle's start-up alone takes a second each.
	failures := make([]string, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		path := filepath.Join(exports, name+".json")
		if err := os.WriteFile(path, docs[name], 0o600); err != nil {
			t.Fatal(err)
		}
		wg.Go(func() {
			if out, err := run("import", "-f", path, "-o", name); err != nil {
				failures[i] = fmt.Sprintf("trestle import %s: %v\n%s", name, err, out)
				return
			}
			var stderr strings.Builder
			cmd := command("canonicalize", "-f", path)
			cmd.Stderr = &stderr
			canonical, err := cmd.Output()
			switch {
			case err != nil:
				failures[i] = fmt.Sprintf("trestle canonicalize %s: %v\n%s", name, err, stderr.String())
			case !bytes.Equal(canonical, docs[name]):
				failures[i] = fmt.Sprintf("%s is not RFC 8785 canonical: trestle canonicalize gives %d bytes for its %d", name, len(canonical), len(docs[name]))
			}
		})
	}
	wg.Wait()
	failed := false
	for _, f := range failures {
		if f != "" {
			t.Error(f)
			failed = true
		}
	}
	if failed {
		return
	}

	out, err := run("validate", "-a")
	if err != nil {
		t.Fatalf("trestle validate: %v\n%s", err, out)
	}
	valid := 0
	for line := range strings.Lines(out) {
		if strings.HasPrefix(line, "VALID: ") {
			valid++
		}
	}
	if valid != len(names) {
		t.Errorf("trestle validate reported %d of %d models valid:\n%s", valid, len(names), out)
	}
	t.Logf("trestle validate:\n%s", out)
}

func trestleEnv() []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	for _, key := range []string{"HOME", "TMPDIR", "VIRTUAL_ENV"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}
