package doctemplate

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Remote rendering: Typst and Pandoc on another host, through grc-render
// (cmd/grc-render), for an installation whose own host has neither.
//
// GRC builds the workspace exactly as for a local render -- the template
// sources with the brand applied, the generated entry file, the payload -- and
// sends it. The render host writes it to a temporary directory, runs the same
// fixed command compile() runs here, and sends back the PDF or the .docx. It
// needs no templates, no brand and no database, so the two hosts cannot
// disagree about what a document looks like, and it keeps nothing after a
// request.
//
// It is a service with its own token rather than a job given to a fleet agent,
// in the shape of wintermute-ocr: the caller asks, it answers, and it can do
// nothing else. LaTeX stays local: tectonic fetches packages from the network
// on first use, which is not something to ask of a host that only typesets.

// RemoteJob is one render sent to grc-render. Files are the workspace by name;
// JSON carries their bytes as base64.
type RemoteJob struct {
	Engine      Engine            `json:"engine"`
	Entry       string            `json:"entry"`
	PDFStandard string            `json:"pdf_standard"`
	Files       map[string][]byte `json:"files"`
}

const (
	// maxRemoteRequest caps a job. A letter's workspace is under 200 KB and a
	// long policy's a few hundred; the headroom is for large tables.
	maxRemoteRequest = 32 << 20
	maxRemoteFiles   = 32
)

// remoteFileName is what a workspace file may be called: a flat name, so no
// job can write outside the directory made for it.
var remoteFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// remoteEngines are the engines grc-render runs.
func remoteEngine(engine Engine) bool { return engine == EngineTypst || engine == EnginePandoc }

// Validate checks a job before anything is written.
func (j RemoteJob) Validate() error {
	if !remoteEngine(j.Engine) {
		return fmt.Errorf("engine %q is not rendered remotely", j.Engine)
	}
	if j.PDFStandard != "" && !validPDFStandard(j.PDFStandard) {
		return fmt.Errorf("unsupported PDF standard %q", j.PDFStandard)
	}
	if len(j.Files) == 0 || len(j.Files) > maxRemoteFiles {
		return fmt.Errorf("a job carries 1 to %d files, not %d", maxRemoteFiles, len(j.Files))
	}
	for name := range j.Files {
		if !remoteFileName.MatchString(name) || strings.Contains(name, "..") {
			return fmt.Errorf("file name %q is not allowed", name)
		}
	}
	if _, ok := j.Files[j.Entry]; !ok {
		return fmt.Errorf("the entry %q is not among the files", j.Entry)
	}
	return nil
}

// ---- The service ----

// RemoteServer is grc-render's API.
type RemoteServer struct {
	token string
	slots chan struct{}
	log   *slog.Logger
}

// NewRemoteServer serves jobs authorised by token, at most concurrency at once.
func NewRemoteServer(token string, concurrency int, log *slog.Logger) *RemoteServer {
	if concurrency < 1 {
		concurrency = 1
	}
	return &RemoteServer{token: token, slots: make(chan struct{}, concurrency), log: log}
}

// Handler routes POST /v1/render and GET /v1/engines.
func (s *RemoteServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/render", s.authorised(s.render))
	mux.HandleFunc("GET /v1/engines", s.authorised(s.engines))
	return mux
}

func (s *RemoteServer) authorised(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeRemoteError(w, http.StatusUnauthorized, "a valid bearer token is required", "")
			return
		}
		next(w, r)
	}
}

// RemoteEngineStatus is an engine as grc-render reports it: no path, which
// means nothing to the caller.
type RemoteEngineStatus struct {
	Engine    Engine `json:"engine"`
	Command   string `json:"command"`
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Detail    string `json:"detail"`
}

func (s *RemoteServer) engines(w http.ResponseWriter, r *http.Request) {
	statuses := EngineStatuses()
	if r.URL.Query().Get("refresh") == "1" {
		statuses = RefreshEngines()
	}
	out := []RemoteEngineStatus{}
	for _, engine := range []Engine{EngineTypst, EnginePandoc} {
		st := statuses[engine]
		out = append(out, RemoteEngineStatus{Engine: engine, Command: st.Command, Version: st.Version, Available: st.Available, Detail: st.Detail})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"engines": out})
}

func (s *RemoteServer) render(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, maxRemoteRequest)
	var job RemoteJob
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&job); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeRemoteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a job is at most %d bytes", maxRemoteRequest), "")
			return
		}
		writeRemoteError(w, http.StatusBadRequest, "the job is not valid JSON: "+err.Error(), "")
		return
	}
	if err := job.Validate(); err != nil {
		writeRemoteError(w, http.StatusBadRequest, err.Error(), "")
		return
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-r.Context().Done():
		writeRemoteError(w, http.StatusServiceUnavailable, "the request ended while waiting for a free slot", "")
		return
	}

	status := StatusFor(job.Engine)
	if !status.Available {
		writeRemoteError(w, http.StatusServiceUnavailable, fmt.Sprintf("%s is not available on this host: %s", job.Engine, status.Detail), "")
		return
	}

	workspace, err := os.MkdirTemp("", "grc-render-")
	if err != nil {
		writeRemoteError(w, http.StatusInternalServerError, "creating the workspace failed", "")
		return
	}
	defer os.RemoveAll(workspace)
	for name, content := range job.Files {
		// #nosec G703 -- name passed Validate: a flat name from an allowlist
		// pattern, with no separator and no "..", joined to the temporary
		// directory this request just created.
		if err := os.WriteFile(filepath.Join(workspace, name), content, 0o600); err != nil {
			writeRemoteError(w, http.StatusInternalServerError, "writing the workspace failed", "")
			return
		}
	}

	tpl := Template{Engine: job.Engine, Entry: job.Entry}
	log, err := compile(r.Context(), workspace, tpl, status, job.PDFStandard)
	if err != nil {
		s.logf("render failed", job, started, 0, err)
		writeRemoteError(w, http.StatusUnprocessableEntity, err.Error(), log)
		return
	}
	output, contentType := outputName, "application/pdf"
	if job.Engine == EnginePandoc {
		output, contentType = docxOutputName, docxContentType
	}
	body, err := readOutput(filepath.Join(workspace, output))
	if err != nil {
		writeRemoteError(w, http.StatusUnprocessableEntity, err.Error(), log)
		return
	}
	s.logf("rendered", job, started, len(body), nil)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-GRC-Engine", status.Command)
	w.Header().Set("X-GRC-Engine-Version", headerSafe(status.Version))
	if log != "" {
		w.Header().Set("X-GRC-Render-Log", headerSafe(log))
	}
	_, _ = w.Write(body)
}

// logf records a job by its shape, never its content.
func (s *RemoteServer) logf(msg string, job RemoteJob, started time.Time, outBytes int, err error) {
	if s.log == nil {
		return
	}
	in := 0
	for _, f := range job.Files {
		in += len(f)
	}
	attrs := []any{"engine", job.Engine, "entry", job.Entry, "files", len(job.Files), "bytes_in", in,
		"bytes_out", outBytes, "ms", time.Since(started).Milliseconds()}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	s.log.Info(msg, attrs...)
}

func writeRemoteError(w http.ResponseWriter, status int, msg, log string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "log": log})
}

// ---- The client ----

// RemoteConfig names the render host GRC sends Typst and Pandoc work to.
type RemoteConfig struct {
	// URL is grc-render's base address, e.g. https://coven.l3d.internal:8892.
	URL string
	// Token is the bearer token grc-render was started with.
	Token string
	// CAFile, when set, is a PEM certificate to trust besides the system
	// roots: the internal CA that issued grc-render's certificate.
	CAFile string
}

type remoteClient struct {
	base  string
	host  string
	token string
	http  *http.Client
}

var (
	remoteMu sync.RWMutex
	remote   *remoteClient
)

// ConfigureRemote sets the render host, or clears it with an empty URL. It is
// called once at startup, so a bad address or CA file stops the app there
// rather than at the first render.
func ConfigureRemote(cfg RemoteConfig) error {
	raw := strings.TrimSpace(cfg.URL)
	if raw == "" {
		setRemote(nil)
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("render URL %q must be an http or https address", raw)
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return fmt.Errorf("a render URL needs the render token grc-render was started with")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if ca := strings.TrimSpace(cfg.CAFile); ca != "" {
		if u.Scheme != "https" {
			return fmt.Errorf("a render CA file is only used with an https render URL")
		}
		// #nosec G304 -- the CA path is operator configuration read at
		// startup, never request input.
		pem, err := os.ReadFile(ca)
		if err != nil {
			return fmt.Errorf("reading the render CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return fmt.Errorf("the render CA file %s holds no PEM certificate", ca)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	setRemote(&remoteClient{
		base:  strings.TrimRight(u.String(), "/"),
		host:  u.Host,
		token: token,
		http:  &http.Client{Transport: transport, Timeout: RenderTimeout + 30*time.Second},
	})
	// The engine cache describes whichever host renders; it is wrong now.
	engineCacheMu.Lock()
	engineCache = nil
	engineCacheMu.Unlock()
	return nil
}

func setRemote(c *remoteClient) {
	remoteMu.Lock()
	remote = c
	remoteMu.Unlock()
}

// remoteFor returns the render host for an engine, or nil to render here.
func remoteFor(engine Engine) *remoteClient {
	if !remoteEngine(engine) {
		return nil
	}
	remoteMu.RLock()
	defer remoteMu.RUnlock()
	return remote
}

// errRemoteUnreachable is a render host that did not answer, or answered
// something other than a render or a compile failure. Render falls back to
// the source bundle for it, as for a missing engine.
type errRemoteUnreachable struct{ err error }

func (e errRemoteUnreachable) Error() string { return e.err.Error() }
func (e errRemoteUnreachable) Unwrap() error { return e.err }

// remoteOutput is what a render host sent back.
type remoteOutput struct {
	body    []byte
	log     string
	engine  string
	version string
}

// render sends a prepared workspace and returns the output. A compile failure
// on the far host comes back as an ordinary error with its log, as it would
// here; anything else is errRemoteUnreachable.
func (c *remoteClient) render(ctx context.Context, workspace string, tpl Template, pdfStandard string) (remoteOutput, error) {
	job := RemoteJob{Engine: tpl.Engine, Entry: tpl.Entry, PDFStandard: pdfStandard, Files: map[string][]byte{}}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return remoteOutput{}, fmt.Errorf("listing the render workspace: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// #nosec G304 -- the name comes from listing the temporary workspace
		// this process created, not from request input.
		content, err := os.ReadFile(filepath.Join(workspace, e.Name()))
		if err != nil {
			return remoteOutput{}, fmt.Errorf("reading %s for the render host: %w", e.Name(), err)
		}
		job.Files[e.Name()] = content
	}
	if err := job.Validate(); err != nil {
		return remoteOutput{}, fmt.Errorf("preparing the render job: %w", err)
	}
	body, err := json.Marshal(job)
	if err != nil {
		return remoteOutput{}, fmt.Errorf("encoding the render job: %w", err)
	}

	resp, err := c.do(ctx, http.MethodPost, "/v1/render", bytes.NewReader(body))
	if err != nil {
		return remoteOutput{}, errRemoteUnreachable{err}
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxOutputBytes+1))
	if err != nil {
		return remoteOutput{}, errRemoteUnreachable{fmt.Errorf("reading the answer from %s: %w", c.host, err)}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		if len(payload) > maxOutputBytes {
			return remoteOutput{}, fmt.Errorf("the rendered document from %s is over the %d byte limit", c.host, maxOutputBytes)
		}
		return remoteOutput{body: payload, log: resp.Header.Get("X-GRC-Render-Log"),
			engine: resp.Header.Get("X-GRC-Engine"), version: resp.Header.Get("X-GRC-Engine-Version")}, nil
	case resp.StatusCode == http.StatusUnprocessableEntity:
		var failure struct{ Error, Log string }
		_ = json.Unmarshal(payload, &failure)
		return remoteOutput{log: failure.Log}, fmt.Errorf("on %s: %s", c.host, firstNonEmptyString(failure.Error, "the render failed"))
	default:
		var failure struct{ Error string }
		_ = json.Unmarshal(payload, &failure)
		return remoteOutput{}, errRemoteUnreachable{fmt.Errorf("%s answered %s: %s", c.host, resp.Status, firstNonEmptyString(failure.Error, "no reason given"))}
	}
}

// engineStatuses asks the render host what it can run.
func (c *remoteClient) engineStatuses(refresh bool) (map[Engine]EngineStatus, error) {
	path := "/v1/engines"
	if refresh {
		path += "?refresh=1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteProbeTimeout)
	defer cancel()
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", c.host, resp.Status)
	}
	var answer struct {
		Engines []RemoteEngineStatus `json:"engines"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&answer); err != nil {
		return nil, fmt.Errorf("reading the engines from %s: %w", c.host, err)
	}
	out := map[Engine]EngineStatus{}
	for _, e := range answer.Engines {
		if !remoteEngine(e.Engine) {
			continue
		}
		detail := "on " + c.host + " through grc-render"
		if !e.Available {
			detail = "not available on " + c.host + ": " + e.Detail
		}
		out[e.Engine] = EngineStatus{Engine: e.Engine, Command: e.Command, Version: e.Version, Available: e.Available, Detail: detail}
	}
	return out, nil
}

// remoteProbeTimeout bounds the engine query, which the gallery waits on.
const remoteProbeTimeout = 5 * time.Second

func (c *remoteClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching %s: %w", c.host, err)
	}
	return resp, nil
}

// remoteStatuses fills in the engines a render host runs, for detectEngines.
func remoteStatuses(out map[Engine]EngineStatus, refresh bool) {
	c := remoteFor(EngineTypst)
	if c == nil {
		return
	}
	statuses, err := c.engineStatuses(refresh)
	for _, engine := range []Engine{EngineTypst, EnginePandoc} {
		st, ok := statuses[engine]
		if err != nil || !ok {
			reason := "it did not report the engine"
			if err != nil {
				reason = err.Error()
			}
			st = EngineStatus{Engine: engine, Command: string(engine), Detail: "the render host " + c.host + " is not answering: " + reason}
		}
		out[engine] = st
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
