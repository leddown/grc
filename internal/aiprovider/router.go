package aiprovider

import (
	"context"
	"errors"
	"fmt"
)

// UsageLogger records what a turn cost, so every AI field's spend lands in the
// one ai_usage_log the rest of the app reads.
type UsageLogger func(provider, model string, inputTokens, outputTokens int)

// Router picks a provider per the operator's Settings choice and asks it.
//
// The choice is read per request rather than captured at construction, so
// changing it in Settings takes effect immediately.
type Router struct {
	claude     *Claude
	wintermute *Wintermute
	// preference returns settings.ProviderAuto / ProviderClaude /
	// ProviderWintermute.
	preference func() string
	logUsage   UsageLogger
}

// NewRouter returns a Router over the two providers.
func NewRouter(claude *Claude, wintermute *Wintermute, preference func() string, logUsage UsageLogger) *Router {
	return &Router{claude: claude, wintermute: wintermute, preference: preference, logUsage: logUsage}
}

// Provider names understood by the router, duplicated from the settings package
// to avoid importing it here — the harness should not depend on where the
// preference is stored.
const (
	preferAuto       = "auto"
	preferClaude     = "claude"
	preferWintermute = "wintermute"
)

// Selected returns the provider that would serve the next request, and whether
// one is available at all.
//
// "auto" prefers Wintermute when it is configured: a local model is the cheaper
// and more private option, and the operator who configured one meant to use it.
// It falls back to Claude so selecting auto cannot leave the app unable to
// answer. An explicit choice is honoured with no fallback — someone who picks
// Wintermute may be doing so because questions must not leave the network, and
// silently reaching for the cloud would break exactly that expectation.
func (r *Router) Selected() (Provider, error) {
	switch r.preferenceValue() {
	case preferClaude:
		if r.claude != nil && r.claude.Available() {
			return r.claude, nil
		}
		return nil, fmt.Errorf("%w: Claude is selected but has no API key", ErrNotConfigured)

	case preferWintermute:
		if r.wintermute != nil && r.wintermute.Available() {
			return r.wintermute, nil
		}
		return nil, fmt.Errorf("%w: Wintermute is selected but has no server URL and token", ErrNotConfigured)

	default: // auto
		if r.wintermute != nil && r.wintermute.Available() {
			return r.wintermute, nil
		}
		if r.claude != nil && r.claude.Available() {
			return r.claude, nil
		}
		return nil, fmt.Errorf("%w: set an Anthropic API key, or a Wintermute server and token, in Settings", ErrNotConfigured)
	}
}

func (r *Router) preferenceValue() string {
	if r.preference == nil {
		return preferAuto
	}
	switch value := r.preference(); value {
	case preferClaude, preferWintermute, preferAuto:
		return value
	default:
		return preferAuto
	}
}

// Available reports whether any provider could serve a request.
func (r *Router) Available() bool {
	_, err := r.Selected()
	return err == nil
}

// Describe renders the active provider's configuration for a status display.
func (r *Router) Describe() string {
	provider, err := r.Selected()
	if err != nil {
		return "no AI provider configured"
	}
	return provider.Describe()
}

// Ask routes one question and records its usage.
func (r *Router) Ask(ctx context.Context, req Request) (Response, error) {
	provider, err := r.Selected()
	if err != nil {
		return Response{}, err
	}

	resp, err := provider.Ask(ctx, req)
	if err != nil {
		return Response{}, err
	}
	if r.logUsage != nil {
		// The provider reports what actually served the turn, which for
		// Wintermute is not always what was asked for.
		r.logUsage(resp.Provider, resp.Model, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
	return resp, nil
}

// Probe runs a connection test against the Wintermute server. Claude has no
// probe: checking a key would be a billable request, and the discovery worth
// surfacing here is which local backends exist.
func (r *Router) Probe(ctx context.Context) Probe {
	if r.wintermute == nil {
		return Probe{Detail: "no Wintermute provider configured"}
	}
	return r.wintermute.Probe(ctx)
}

// Status summarises the harness for the Settings page.
type Status struct {
	Preference          string `json:"preference"`
	Active              string `json:"active"`
	Detail              string `json:"detail"`
	ClaudeAvailable     bool   `json:"claude_available"`
	WintermuteAvailable bool   `json:"wintermute_available"`
}

// Status describes the current routing state without making a request.
func (r *Router) Status() Status {
	st := Status{
		Preference:          r.preferenceValue(),
		ClaudeAvailable:     r.claude != nil && r.claude.Available(),
		WintermuteAvailable: r.wintermute != nil && r.wintermute.Available(),
	}
	provider, err := r.Selected()
	if err != nil {
		st.Detail = err.Error()
		if errors.Is(err, ErrNotConfigured) {
			st.Active = ""
		}
		return st
	}
	st.Active = provider.Name()
	st.Detail = provider.Describe()
	return st
}
