package doctemplate

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const remoteToken = "test-token-0123456789abcdef0123456789"

// fakeTypst is a stand-in typesetter: it writes the names of the workspace
// files into the output, so a test can see what reached the render host, and
// fails when the entry asks it to.
const fakeTypst = `#!/bin/sh
case "$1" in --version) echo "typst 9.9.9 (fake)"; exit 0;; esac
for last; do :; done
if grep -q FAIL_ME *.typ 2>/dev/null; then echo "error: failed on purpose" >&2; exit 1; fi
printf '%%PDF-fake %s' "$(ls | tr '\n' ' ')" > "$last"
`

// withRemote points rendering at a grc-render served from this process,
// whose Typst is the stand-in, and puts everything back afterwards.
func withRemote(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewRemoteServer(remoteToken, 1, nil).Handler())
	t.Cleanup(srv.Close)
	configureRemote(t, RemoteConfig{URL: srv.URL, Token: remoteToken})
	return srv
}

// configureRemote sets the render host and pins the stand-in Typst as the
// engine the render host runs. ConfigureRemote clears the engine cache, and
// the render host here shares this process's: left empty, it would ask the
// render host -- itself -- for its engines.
func configureRemote(t *testing.T, cfg RemoteConfig) {
	t.Helper()
	if err := ConfigureRemote(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ConfigureRemote(RemoteConfig{}) })
	bin := filepath.Join(t.TempDir(), "typst")
	if err := os.WriteFile(bin, []byte(fakeTypst), 0o700); err != nil {
		t.Fatal(err)
	}
	engines := noEngines()
	engines[EngineTypst] = EngineStatus{Engine: EngineTypst, Command: "typst", Path: bin, Version: "typst 9.9.9 (fake)", Available: true}
	withEngines(t, engines)
}

func TestRemoteJobValidate(t *testing.T) {
	good := func() RemoteJob {
		return RemoteJob{Engine: EngineTypst, Entry: "letter.typ", PDFStandard: "a-2b", Files: map[string][]byte{"letter.typ": nil, "data.json": nil}}
	}
	if err := good().Validate(); err != nil {
		t.Fatalf("a good job was refused: %v", err)
	}
	for name, mutate := range map[string]func(*RemoteJob){
		"latex":             func(j *RemoteJob) { j.Engine = EngineLaTeX },
		"unknown standard":  func(j *RemoteJob) { j.PDFStandard = "x" },
		"no files":          func(j *RemoteJob) { j.Files = map[string][]byte{} },
		"entry missing":     func(j *RemoteJob) { j.Entry = "other.typ" },
		"a path":            func(j *RemoteJob) { j.Files["../etc/passwd"] = nil },
		"a subdirectory":    func(j *RemoteJob) { j.Files["a/b.typ"] = nil },
		"dot dot":           func(j *RemoteJob) { j.Files["a..b"] = nil },
		"a hidden file":     func(j *RemoteJob) { j.Files[".bashrc"] = nil },
		"an absolute entry": func(j *RemoteJob) { j.Entry = "/letter.typ"; j.Files["/letter.typ"] = nil },
	} {
		j := good()
		mutate(&j)
		if err := j.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRemoteServerRefusesWhatItShould(t *testing.T) {
	srv := withRemote(t)
	post := func(token, body string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/render", strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	job, _ := json.Marshal(RemoteJob{Engine: EngineTypst, Entry: "a.typ", Files: map[string][]byte{"a.typ": []byte("x")}})
	for name, c := range map[string]struct {
		token, body string
		want        int
	}{
		"no token":         {"", string(job), http.StatusUnauthorized},
		"wrong token":      {remoteToken + "x", string(job), http.StatusUnauthorized},
		"not JSON":         {remoteToken, "{", http.StatusBadRequest},
		"an unknown field": {remoteToken, `{"engine":"typst","entry":"a.typ","files":{"a.typ":"eA=="},"shell":"rm"}`, http.StatusBadRequest},
		"a path":           {remoteToken, `{"engine":"typst","entry":"../a.typ","files":{"../a.typ":"eA=="}}`, http.StatusBadRequest},
		"pandoc missing":   {remoteToken, `{"engine":"pandoc","entry":"a.md","files":{"a.md":"eA=="}}`, http.StatusServiceUnavailable},
		"too large":        {remoteToken, `{"engine":"typst","entry":"a.typ","files":{"a.typ":"` + strings.Repeat("A", maxRemoteRequest) + `"}}`, http.StatusRequestEntityTooLarge},
	} {
		if got := post(c.token, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/engines", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the engine list without a token: %v", err)
	}
}

// The workspace GRC prepared is what reaches the render host, and the output
// comes back as the result.
func TestRenderGoesToTheRenderHost(t *testing.T) {
	withRepoTemplates(t)
	srv := withRemote(t)
	result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "letter-typst", Data: mustSample(t, "letter-typst"), BaseName: "NWB-1", PDFStandard: "a-2b"})
	if err != nil {
		t.Fatalf("Render: %v\n%s", err, result.Log)
	}
	if result.Bundled || result.Filename != "NWB-1.pdf" || result.ContentType != "application/pdf" {
		t.Fatalf("got %q %q bundled=%v", result.Filename, result.ContentType, result.Bundled)
	}
	if want := "%PDF-fake brand.typ data.json letter.typ lib.typ "; string(result.Body) != want {
		t.Errorf("the render host saw %q, want %q", result.Body, want)
	}
	if host := strings.TrimPrefix(srv.URL, "http://"); result.Engine != "typst on "+host || result.EngineVersion != "typst 9.9.9 (fake)" {
		t.Errorf("engine %q %q", result.Engine, result.EngineVersion)
	}
}

func TestARenderHostCompileFailureCarriesItsLog(t *testing.T) {
	withRemote(t)
	tpl, _ := Lookup("letter-typst")
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "letter.typ"), []byte("FAIL_ME"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := remoteFor(EngineTypst).render(context.Background(), workspace, tpl, "")
	var unreachable errRemoteUnreachable
	if err == nil || errors.As(err, &unreachable) || !strings.Contains(err.Error(), "on 127.0.0.1") {
		t.Fatalf("a compile failure on the render host: %v", err)
	}
	if !strings.Contains(out.log, "failed on purpose") {
		t.Errorf("the log did not come back: %q", out.log)
	}
}

// A render host that does not answer is a missing engine: the sources come
// back, and nothing is rendered here instead.
func TestAnUnreachableRenderHostReturnsTheSources(t *testing.T) {
	withRepoTemplates(t)
	srv := withRemote(t)
	srv.Close()
	result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "letter-typst", Data: mustSample(t, "letter-typst"), BaseName: "NWB-1"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !result.Bundled || !strings.Contains(result.Reason, "The render host could not render it") {
		t.Errorf("bundled=%v reason=%q", result.Bundled, result.Reason)
	}
	if _, ok := zipEntries(t, result.Body)["build.sh"]; !ok {
		t.Error("the sources carry no build.sh")
	}
}

func TestEngineStatusesDescribeTheRenderHost(t *testing.T) {
	// The render host runs in this process, so what it reports is the engine
	// list withRemote pinned: the stand-in Typst, and no Pandoc.
	srv := withRemote(t)
	statuses := map[Engine]EngineStatus{}
	remoteStatuses(statuses, false)
	host := strings.TrimPrefix(srv.URL, "http://")
	if st := statuses[EngineTypst]; !st.Available || st.Version != "typst 9.9.9 (fake)" || st.Detail != "on "+host+" through grc-render" {
		t.Errorf("typst: %+v", st)
	}
	if st := statuses[EnginePandoc]; st.Available || !strings.HasPrefix(st.Detail, "not available on "+host) {
		t.Errorf("pandoc: %+v", st)
	}
	if _, ok := statuses[EngineLaTeX]; ok {
		t.Error("LaTeX is never reported for the render host")
	}

	srv.Close()
	statuses = map[Engine]EngineStatus{}
	remoteStatuses(statuses, true)
	if st := statuses[EnginePandoc]; st.Available || !strings.Contains(st.Detail, "is not answering") {
		t.Errorf("pandoc on a host that is down: %+v", st)
	}
}

func TestConfigureRemoteRefusesABadSetting(t *testing.T) {
	t.Cleanup(func() { _ = ConfigureRemote(RemoteConfig{}) })
	notPEM := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(notPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]RemoteConfig{
		"not a URL":         {URL: "coven:8892", Token: remoteToken},
		"another scheme":    {URL: "ftp://coven:8892", Token: remoteToken},
		"no token":          {URL: "https://coven:8892"},
		"a CA over http":    {URL: "http://coven:8892", Token: remoteToken, CAFile: notPEM},
		"a missing CA file": {URL: "https://coven:8892", Token: remoteToken, CAFile: notPEM + ".missing"},
		"a CA file not PEM": {URL: "https://coven:8892", Token: remoteToken, CAFile: notPEM},
	} {
		if err := ConfigureRemote(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := ConfigureRemote(RemoteConfig{URL: "https://coven.l3d.internal:8892/", Token: remoteToken}); err != nil || remoteFor(EngineTypst) == nil || remoteFor(EngineLaTeX) != nil {
		t.Errorf("a good setting: %v", err)
	}
}

// With a real typst (TYPST or PATH), a letter rendered through grc-render is
// the PDF typst makes.
func TestRealTypstThroughTheRenderHost(t *testing.T) {
	bin := os.Getenv("TYPST")
	if bin == "" {
		bin, _ = exec.LookPath("typst")
	}
	if bin == "" {
		t.Skip("typst is not installed")
	}
	withRepoTemplates(t)
	srv := httptest.NewServer(NewRemoteServer(remoteToken, 1, nil).Handler())
	t.Cleanup(srv.Close)
	if err := ConfigureRemote(RemoteConfig{URL: srv.URL, Token: remoteToken}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ConfigureRemote(RemoteConfig{}) })
	engines := noEngines()
	engines[EngineTypst] = EngineStatus{Engine: EngineTypst, Command: "typst", Path: bin, Available: true}
	withEngines(t, engines)

	result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "letter-typst", Data: mustSample(t, "letter-typst"), BaseName: "NWB-1", PDFStandard: "a-2b"})
	if err != nil {
		t.Fatalf("Render: %v\n%s", err, result.Log)
	}
	if result.Bundled || !bytes.HasPrefix(result.Body, []byte("%PDF-")) {
		t.Fatalf("not a PDF: bundled=%v %q", result.Bundled, result.Body[:min(16, len(result.Body))])
	}
}

// Over HTTPS, the render host's certificate is trusted through the CA file,
// and refused without it.
func TestRenderHostOverHTTPSTrustsTheCAFile(t *testing.T) {
	withRepoTemplates(t)
	srv := httptest.NewTLSServer(NewRemoteServer(remoteToken, 1, nil).Handler())
	t.Cleanup(srv.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	render := func() Result {
		t.Helper()
		result, err := newTestService(t).Render(context.Background(), Request{TemplateID: "letter-typst", Data: mustSample(t, "letter-typst"), BaseName: "NWB-1"})
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		return result
	}

	configureRemote(t, RemoteConfig{URL: srv.URL, Token: remoteToken})
	if result := render(); !result.Bundled || !strings.Contains(result.Reason, "certificate") {
		t.Errorf("an untrusted certificate was accepted: bundled=%v %q", result.Bundled, result.Reason)
	}

	configureRemote(t, RemoteConfig{URL: srv.URL, Token: remoteToken, CAFile: ca})
	if result := render(); result.Bundled || !strings.HasPrefix(string(result.Body), "%PDF-fake") {
		t.Errorf("over HTTPS with the CA: bundled=%v reason=%q", result.Bundled, result.Reason)
	}
}
