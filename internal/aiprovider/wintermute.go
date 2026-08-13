package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
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

	session, err := w.postJSON(ctx, base+"/api/v1/sessions", cfg.Token, map[string]any{
		"title":   sessionTitle,
		"backend": cfg.Backend,
		"model":   model,
	})
	if err != nil {
		return Response{}, fmt.Errorf("wintermute session: %w", err)
	}
	sessionID := stringField(session, "id")
	if sessionID == "" {
		return Response{}, fmt.Errorf("wintermute did not return a session id")
	}

	// wintermuted derives its own system prompt from its configuration and
	// takes only message text, so the instruction is prepended to the question.
	text := req.Prompt
	if system := strings.TrimSpace(req.System); system != "" {
		text = system + "\n\n" + req.Prompt
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
		Text:     answer,
		Provider: NameWintermute,
		Backend:  stringField(turn, "backend"),
		Model:    stringField(turn, "model"),
		Usage:    extractUsage(turn),
	}, nil
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
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach wintermute: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded so a misconfigured URL pointing at something enormous cannot
	// exhaust memory here.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read wintermute response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(raw))
		if len(detail) > 300 {
			detail = detail[:300] + "..."
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("wintermute rejected the client token (HTTP 401)")
		}
		return nil, fmt.Errorf("wintermute returned HTTP %d: %s", resp.StatusCode, detail)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("wintermute returned a non-JSON response: %w", err)
	}
	return decoded, nil
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
