package doctemplate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// EngineStatus is what the gallery shows for one typesetter, and what the
// render pipeline consults before deciding whether it can produce a PDF at all.
type EngineStatus struct {
	Engine    Engine `json:"engine"`
	Command   string `json:"command"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Available bool   `json:"available"`
	// Detail explains an unavailable engine, or carries the operational note a
	// available one needs (tectonic's first-run download, notably).
	Detail string `json:"detail"`
}

// engineCandidates lists the binaries that can drive each engine, in preference
// order, alongside the environment variable that overrides the lookup. The
// names mirror templates/build.sh so a host configured for one is configured
// for both.
var engineCandidates = map[Engine][]struct {
	Command string
	EnvVar  string
}{
	EngineTypst: {
		{Command: "typst", EnvVar: "TYPST"},
	},
	EngineLaTeX: {
		// tectonic first: a single binary that fetches the TeX packages it
		// needs, which is far less to install than a distribution. latexmk is
		// the fallback for hosts that already have one.
		{Command: "tectonic", EnvVar: "TECTONIC"},
		{Command: "latexmk", EnvVar: "LATEXMK"},
	},
}

// versionProbeTimeout bounds `binary --version`. Generous enough for a cold
// page cache, short enough that a wedged binary cannot hang the gallery.
const versionProbeTimeout = 10 * time.Second

// maxProbeOutput caps what a version probe may write back. A misconfigured
// binary that streams forever must not be able to grow the process heap.
const maxProbeOutput = 8 << 10

var (
	engineCacheMu sync.Mutex
	engineCache   map[Engine]EngineStatus
)

// EngineStatuses reports the state of every engine, cached after the first
// call. Detection shells out, and doing that on each page load would put a
// process spawn in the path of every render request.
//
// The cache is explicitly refreshable rather than time-based: an operator who
// has just installed typst wants the page to say so when they press the button,
// and nobody benefits from it flipping on its own between two page loads.
func EngineStatuses() map[Engine]EngineStatus {
	engineCacheMu.Lock()
	defer engineCacheMu.Unlock()
	if engineCache == nil {
		engineCache = detectEngines()
	}
	out := make(map[Engine]EngineStatus, len(engineCache))
	for k, v := range engineCache {
		out[k] = v
	}
	return out
}

// RefreshEngines clears the detection cache and probes again.
func RefreshEngines() map[Engine]EngineStatus {
	engineCacheMu.Lock()
	engineCache = detectEngines()
	out := make(map[Engine]EngineStatus, len(engineCache))
	for k, v := range engineCache {
		out[k] = v
	}
	engineCacheMu.Unlock()
	return out
}

// StatusFor returns the status of one engine.
func StatusFor(engine Engine) EngineStatus {
	return EngineStatuses()[engine]
}

func detectEngines() map[Engine]EngineStatus {
	out := make(map[Engine]EngineStatus, len(engineCandidates))
	for engine, candidates := range engineCandidates {
		out[engine] = detectEngine(engine, candidates)
	}
	return out
}

func detectEngine(engine Engine, candidates []struct {
	Command string
	EnvVar  string
}) EngineStatus {
	tried := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		path, err := resolveBinary(candidate.Command, candidate.EnvVar)
		if err != nil {
			tried = append(tried, candidate.Command)
			continue
		}
		status := EngineStatus{
			Engine:    engine,
			Command:   candidate.Command,
			Path:      path,
			Available: true,
			Version:   probeVersion(path),
		}
		if candidate.Command == "tectonic" {
			status.Detail = "tectonic downloads the TeX packages it needs on first run, so the first LaTeX render on this host needs outbound network access and will be slow."
		}
		return status
	}
	return EngineStatus{
		Engine:  engine,
		Command: candidates[0].Command,
		Detail:  fmt.Sprintf("none of %s found on PATH", strings.Join(tried, ", ")),
	}
}

// resolveBinary finds the executable for a candidate. An environment override
// is taken as an explicit path and checked for executability; otherwise the
// command name is looked up on PATH. Both are operator configuration, never
// request input — nothing a user types reaches this function.
func resolveBinary(command, envVar string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv(envVar)); configured != "" {
		// #nosec G703 -- the path is trusted operator configuration (TYPST,
		// TECTONIC, LATEXMK), read once at startup from the process
		// environment, and is never influenced by a request.
		info, err := os.Stat(configured)
		if err != nil {
			return "", fmt.Errorf("%s=%s: %w", envVar, configured, err)
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("%s=%s is not an executable file", envVar, configured)
		}
		return configured, nil
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("looking up %s: %w", command, err)
	}
	return path, nil
}

// probeVersion returns the first line of `binary --version`, or "" when the
// probe fails. A binary that exists but will not report a version is still
// reported as available: the render is the real test, and refusing to use it
// here would be guessing.
func probeVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	// #nosec G204 -- path comes from exec.LookPath over a compile-time command
	// allowlist or from an operator-set environment variable, never from
	// request input, and the argument is a constant.
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = sandboxEnv()
	var buf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &buf, remaining: maxProbeOutput}
	cmd.Stderr = &limitedWriter{w: &buf, remaining: maxProbeOutput}
	if err := cmd.Run(); err != nil && buf.Len() == 0 {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(buf.String()), "\n")
	return strings.TrimSpace(line)
}

// sandboxEnv is the environment a typesetter runs with. It is built from
// scratch rather than inherited: the app's own environment holds the Jira
// token, the AI provider key and the admin token, and none of that has any
// business being visible to a subprocess that renders a PDF.
//
// HOME is kept because tectonic caches its downloaded package set under it, and
// dropping it would make every LaTeX render re-fetch the world.
func sandboxEnv() []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
	}
	for _, key := range []string{"HOME", "TMPDIR", "XDG_CACHE_HOME", "XDG_DATA_HOME", "SOURCE_DATE_EPOCH"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// limitedWriter caps how much a subprocess can write back to us. Past the
// limit bytes are counted and dropped, so the caller can say the output was
// truncated instead of silently showing a prefix.
type limitedWriter struct {
	w         *bytes.Buffer
	remaining int
	dropped   int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		lw.dropped += len(p)
		return len(p), nil
	}
	if len(p) > lw.remaining {
		lw.w.Write(p[:lw.remaining])
		lw.dropped += len(p) - lw.remaining
		lw.remaining = 0
		return len(p), nil
	}
	lw.w.Write(p)
	lw.remaining -= len(p)
	return len(p), nil
}
