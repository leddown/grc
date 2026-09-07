package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// sessionTitle labels the conversations this app opens on a wintermuted
// server, so they are distinguishable from a human's own transcripts.
const sessionTitle = "GRC question"

// wintermuteTimeout bounds a turn. A self-hosted model on modest hardware is
// far slower than a cloud endpoint — a long document analysis can take minutes
// — so this is generous compared with a typical HTTP client default.
const wintermuteTimeout = 10 * time.Minute

// probeTimeout bounds the connection test, which only reads configuration.
const probeTimeout = 10 * time.Second

// Wintermute answers questions through a wintermuted server on the network,
// which routes each turn to a self-hosted model or on to Claude.
//
// This app never sees a model endpoint or a vendor key for that path: it holds
// only the server URL and a client token.
type Wintermute struct {
	// Resolved per request so Settings changes apply without a restart.
	config func() WintermuteConfig
	client *http.Client
}

// WintermuteConfig is the resolved configuration for one request.
type WintermuteConfig struct {
	URL     string
	Token   string
	Backend string
	Model   string
	// Agent names an agent profile on the server: which document library and
	// which external sources the conversation may consult. Empty is that
	// server's general assistant.
	//
	// This is the difference between "the model answers from what it was
	// trained on" and "the model answers from this installation's catalogs",
	// so it is worth setting even though it is optional.
	Agent string
}

// NewWintermute returns a Wintermute provider.
func NewWintermute(config func() WintermuteConfig) *Wintermute {
	return &Wintermute{
		config: config,
		client: &http.Client{Timeout: wintermuteTimeout},
	}
}

func (w *Wintermute) Name() string { return NameWintermute }

func (w *Wintermute) resolve() WintermuteConfig {
	if w.config == nil {
		return WintermuteConfig{}
	}
	cfg := w.config()
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.Backend = strings.TrimSpace(cfg.Backend)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Agent = strings.TrimSpace(cfg.Agent)
	return cfg
}

// Available reports whether both a server URL and a token are configured.
func (w *Wintermute) Available() bool {
	cfg := w.resolve()
	return cfg.URL != "" && cfg.Token != ""
}

func (w *Wintermute) Describe() string {
	cfg := w.resolve()
	switch {
	case cfg.URL == "":
		return "Wintermute: no server URL"
	case cfg.Token == "":
		return "Wintermute: no client token"
	}
	target := cfg.URL
	if cfg.Agent != "" {
		target += " as " + cfg.Agent
	}
	if cfg.Backend != "" {
		target += " (" + cfg.Backend
		if cfg.Model != "" {
			target += "/" + cfg.Model
		}
		target += ")"
	}
	return "Wintermute: " + target
}

// Ask runs one question through the server: open a conversation, post the
// question, read the reply. The server owns the transcript and decides which
// model answers.
func (w *Wintermute) Ask(ctx context.Context, req Request) (Response, error) {
	cfg := w.resolve()
	if cfg.URL == "" || cfg.Token == "" {
		return Response{}, ErrNotConfigured
	}
	base, err := ValidateEndpoint(cfg.URL)
	if err != nil {
		return Response{}, err
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = cfg.Model
	}

	// The server owns the transcript, so a conversation continues by posting to
	// the session the last answer came from rather than by resending it.
	sessionID := strings.TrimSpace(req.SessionID)
	resumed := sessionID != ""
	if !resumed {
		session, err := w.postJSON(ctx, base+"/api/v1/sessions", cfg.Token, map[string]any{
			"title":   sessionTitle,
			"backend": cfg.Backend,
			"model":   model,
			"agent":   cfg.Agent,
		})
		if err != nil {
			return Response{}, fmt.Errorf("wintermute session: %w", err)
		}
		sessionID = stringField(session, "id")
		if sessionID == "" {
			return Response{}, fmt.Errorf("wintermute did not return a session id")
		}
	}

	// wintermuted derives its own system prompt from its configuration and
	// takes only message text, so the instruction is prepended to the question.
	text := req.Prompt
	// A History with no SessionID is a transcript this server has never seen —
	// a caller that kept its own, or one whose session has expired — so it is
	// folded into the first message. On a resumed session the server already
	// has it and resending would duplicate every earlier turn.
	if !resumed && len(req.History) > 0 {
		text = transcriptPrefix(req.History) + text
	}
	if system := strings.TrimSpace(req.System); system != "" {
		text = system + "\n\n" + text
	}

	turn, err := w.postJSON(ctx,
		base+"/api/v1/sessions/"+neturl.PathEscape(sessionID)+"/messages",
		cfg.Token, map[string]any{"text": text})
	if err != nil {
		return Response{}, fmt.Errorf("wintermute turn: %w", err)
	}

	answer := stringField(turn, "reply")
	if answer == "" {
		// A turn waiting on client-side tool calls has no reply. This app
		// declares no client tools, so that means the model asked for
		// something only an agent harness can run.
		if status := stringField(turn, "status"); status != "" && status != "complete" {
			return Response{}, fmt.Errorf("wintermute turn ended with status %q and no reply", status)
		}
		return Response{}, errNoAnswer
	}

	return Response{
		Text:      answer,
		Provider:  NameWintermute,
		Backend:   stringField(turn, "backend"),
		Model:     stringField(turn, "model"),
		SessionID: sessionID,
		Usage:     extractUsage(turn),
	}, nil
}

// transcriptPrefix renders earlier turns as labelled text, for the one case
// where this provider cannot lean on the server's own transcript.
func transcriptPrefix(history []Message) string {
	var b strings.Builder
	b.WriteString("Conversation so far:\n\n")
	for _, msg := range history {
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		label := "User"
		if msg.Role == RoleAssistant {
			label = "Assistant"
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	b.WriteString("Now answer this:\n\n")
	return b.String()
}

// Probe checks the server is reachable and reports the backends it advertises,
// so an operator can see which local models are available and copy a name into
// the settings.
func (w *Wintermute) Probe(ctx context.Context) Probe {
	cfg := w.resolve()
	if cfg.URL == "" {
		return Probe{Detail: "no server URL configured"}
	}
	if cfg.Token == "" {
		return Probe{Detail: "no client token configured"}
	}
	base, err := ValidateEndpoint(cfg.URL)
	if err != nil {
		return Probe{Detail: err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// /api/v1/me is authenticated and returns the router's view of the world,
	// so one call tests the URL, the token and discovery together.
	body, err := w.request(ctx, http.MethodGet, base+"/api/v1/me", cfg.Token, nil)
	if err != nil {
		return Probe{Detail: err.Error()}
	}

	probe := Probe{
		OK:             true,
		DefaultBackend: stringField(body, "default_backend"),
		Fallback:       stringField(body, "fallback"),
	}
	if raw, ok := body["backends"].([]any); ok {
		for _, item := range raw {
			if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
				probe.Backends = append(probe.Backends, strings.TrimSpace(name))
			}
		}
	}

	name := stringField(body, "name")
	switch {
	case len(probe.Backends) == 0:
		probe.Detail = fmt.Sprintf("connected as %q, but the server advertises no backends", name)
	default:
		probe.Detail = fmt.Sprintf("connected as %q; %d backend(s) available", name, len(probe.Backends))
	}

	// A pinned backend that the server does not have would fail at ask time
	// with a less obvious message, so say so now.
	if cfg.Backend != "" && len(probe.Backends) > 0 && !contains(probe.Backends, cfg.Backend) {
		probe.OK = false
		probe.Detail = fmt.Sprintf("the server does not have a backend named %q; available: %s",
			cfg.Backend, strings.Join(probe.Backends, ", "))
	}
	return probe
}

// Agent is one agent profile on a wintermuted server: which document library
// and which external sources a conversation opened against it may consult.
type Agent struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Sources     []string `json:"sources"`
}

// Agents lists the agent profiles the server has, so an agent is chosen from
// what exists rather than typed. A name that is one character out is not an
// error anyone sees: it is an answer from the model's training data wearing the
// same confidence as one from this installation's catalogs.
func (w *Wintermute) Agents(ctx context.Context) ([]Agent, error) {
	cfg := w.resolve()
	if cfg.URL == "" || cfg.Token == "" {
		return nil, ErrNotConfigured
	}
	base, err := ValidateEndpoint(cfg.URL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()

	var payload struct {
		Agents []Agent `json:"agents"`
	}
	if err := w.requestInto(ctx, http.MethodGet, base+"/api/v1/agents",
		cfg.Token, nil, &payload); err != nil {
		var status *statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return nil, fmt.Errorf("this Wintermute server has no agents endpoint — it predates agent profiles")
		}
		return nil, err
	}
	if payload.Agents == nil {
		payload.Agents = []Agent{}
	}
	return payload.Agents, nil
}

// catalogTimeout bounds the backend and model lookups. They read configuration
// off a server on the operator's own network; nothing here waits on a model.
const catalogTimeout = 15 * time.Second

// Backend is one backend on a wintermuted server: a model server it can route
// a question to, or a cloud vendor it forwards one on to.
type Backend struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Status is the server's last health verdict — "ok", "unreachable" or
	// "unknown" — so a backend that answers nothing is visible as such before
	// it is pinned rather than at ask time.
	Status string `json:"status"`
	Cloud  bool   `json:"cloud"`
	// Model is the model this backend pins for itself, where it pins one.
	Model string `json:"model,omitempty"`
}

// Model is one model a backend reported it can serve.
type Model struct {
	ID      string  `json:"id"`
	Backend string  `json:"backend"`
	Family  string  `json:"family,omitempty"`
	ParamsB float64 `json:"params_b,omitempty"`
	Loaded  bool    `json:"loaded"`
}

// Catalog is what a server can route to, as a page needs it to offer a backend
// and a model as choices rather than as free text.
//
// It carries only what is rendered. The server's own backend records hold base
// URLs and the names of the environment variables holding vendor keys, and none
// of that has any business reaching a browser.
type Catalog struct {
	Backends []Backend `json:"backends"`
	Models   []Model   `json:"models"`
	// DefaultBackend and Fallback are the server's own routing defaults, so
	// "leave it to the server" can say what that means.
	DefaultBackend string `json:"default_backend,omitempty"`
	Fallback       string `json:"fallback,omitempty"`
	// ModelsError reports a model list that could not be fetched. It is not
	// fatal: the backends are still a usable choice, and a backend with no
	// listed models still answers on its own default.
	ModelsError string `json:"models_error,omitempty"`
}

// Catalog lists the backends the server has and the models they reported.
//
// A backend or model named by hand is the difference between a question going
// to the model someone meant and it going somewhere else — or nowhere, as an
// ask-time error in a feature nobody is watching.
func (w *Wintermute) Catalog(ctx context.Context) (Catalog, error) {
	cfg := w.resolve()
	if cfg.URL == "" || cfg.Token == "" {
		return Catalog{}, ErrNotConfigured
	}
	base, err := ValidateEndpoint(cfg.URL)
	if err != nil {
		return Catalog{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()

	var backends struct {
		Backends []Backend `json:"backends"`
		Default  string    `json:"default"`
		Fallback string    `json:"fallback"`
	}
	if err := w.requestInto(ctx, http.MethodGet, base+"/api/v1/backends",
		cfg.Token, nil, &backends); err != nil {
		return Catalog{}, missingEndpoint(err, "backends")
	}

	catalog := Catalog{
		Backends:       backends.Backends,
		Models:         []Model{},
		DefaultBackend: backends.Default,
		Fallback:       backends.Fallback,
	}
	if catalog.Backends == nil {
		catalog.Backends = []Backend{}
	}

	// The model list is fetched second and separately: a server that cannot
	// produce one — an older API, an unreachable backend — should still leave a
	// backend choosable rather than failing the whole lookup.
	var models struct {
		Models []Model `json:"models"`
	}
	if err := w.requestInto(ctx, http.MethodGet, base+"/api/v1/models",
		cfg.Token, nil, &models); err != nil {
		catalog.ModelsError = missingEndpoint(err, "models").Error()
	} else if models.Models != nil {
		catalog.Models = models.Models
	}
	return catalog, nil
}

// missingEndpoint turns a 404 into the reason there is one: an older server
// that never had the endpoint, rather than a wrong URL.
func missingEndpoint(err error, what string) error {
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return fmt.Errorf("this Wintermute server has no %s endpoint", what)
	}
	return err
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func (w *Wintermute) postJSON(ctx context.Context, url, token string, payload map[string]any) (map[string]any, error) {
	return w.request(ctx, http.MethodPost, url, token, payload)
}

func (w *Wintermute) request(ctx context.Context, method, url, token string, payload map[string]any) (map[string]any, error) {
	var decoded map[string]any
	if err := w.requestInto(ctx, method, url, token, payload, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// statusError reports a non-2xx reply, so a caller can tell an endpoint this
// server does not have from a request that failed.
type statusError struct {
	code   int
	detail string
}

func (e *statusError) Error() string {
	if e.code == http.StatusUnauthorized {
		return "wintermute rejected the client token (HTTP 401)"
	}
	return fmt.Sprintf("wintermute returned HTTP %d: %s", e.code, e.detail)
}

// requestInto performs one request and decodes the JSON reply into out.
func (w *Wintermute) requestInto(ctx context.Context, method, url, token string, payload map[string]any, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("reach wintermute: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded so a misconfigured URL pointing at something enormous cannot
	// exhaust memory here.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read wintermute response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(raw))
		if len(detail) > 300 {
			detail = detail[:300] + "..."
		}
		return &statusError{code: resp.StatusCode, detail: detail}
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("wintermute returned a non-JSON response: %w", err)
	}
	return nil
}

// ValidateEndpoint normalises a Wintermute base URL and rejects the shapes that
// would turn a misconfiguration into a request sent somewhere unintended.
func ValidateEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(strings.TrimRight(raw, "/"))
	if raw == "" {
		return "", fmt.Errorf("server URL is required")
	}
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("server URL is not valid: %w", err)
	}
	// The scheme is checked first: "nas.local:8080" parses as scheme
	// "nas.local" with an empty host, so a host check here would report "no
	// host" for what is really a missing http:// prefix.
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("server URL must be http or https, got %q — try http://%s", parsed.Scheme, raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("server URL has no host")
	}
	// The client token rides on every request, so plaintext is allowed only
	// where it cannot leave the local network.
	if scheme == "http" && !isPrivateHost(parsed.Hostname()) {
		return "", fmt.Errorf("server URL must use https unless the host is loopback or private (got %q)", parsed.Hostname())
	}
	// Credentials in the URL would be sent on every request and logged; the
	// client token belongs in Settings.
	if parsed.User != nil {
		return "", fmt.Errorf("server URL must not contain credentials")
	}
	if host, _, err := net.SplitHostPort(parsed.Host); err == nil && host == "" {
		return "", fmt.Errorf("server URL has no host")
	}
	return parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.Path, "/"), nil
}

// isPrivateHost reports whether host is a literal address on a loopback,
// link-local or private range, or the name "localhost". Names other than
// "localhost" are rejected rather than resolved: a DNS lookup here would be
// both a TOCTOU race and a request to an attacker-chosen name.
//
// The practical consequence is that a Wintermute server reached by hostname
// needs https, or its IP address.
func isPrivateHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func stringField(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}

// extractUsage reads the token accounting a wintermuted turn reports. Fields
// are optional: a self-hosted backend may not report them at all.
func extractUsage(data map[string]any) Usage {
	usage, _ := data["usage"].(map[string]any)
	if usage == nil {
		return Usage{}
	}
	return Usage{
		InputTokens:  intField(usage, "input_tokens"),
		OutputTokens: intField(usage, "output_tokens"),
	}
}

func intField(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
