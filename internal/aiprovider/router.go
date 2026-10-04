package aiprovider

import (
	"context"
	"fmt"
)

// UsageLogger records what a turn cost, so every AI field's spend lands in the
// one ai_usage_log the rest of the app reads.
type UsageLogger func(provider, model string, inputTokens, outputTokens int)

// Router is the harness every AI field asks through. There is one provider
// behind it: the Wintermute server, which routes each question to a model on
// the network or on to Claude, and makes every question an agent turn that is
// audited and whose writes wait for a person. This application used to call
// Claude directly as well; it no longer does (wintermute decision note 0005) —
// Claude is reached as one of that server's backends, with its routing, audit
// and approvals, rather than around them.
type Router struct {
	wintermute *Wintermute
	logUsage   UsageLogger
}

// NewRouter returns the Router.
func NewRouter(wintermute *Wintermute, logUsage UsageLogger) *Router {
	return &Router{wintermute: wintermute, logUsage: logUsage}
}

// Selected returns the provider that would serve the next request, or why
// there is none.
func (r *Router) Selected() (Provider, error) {
	if r.wintermute != nil && r.wintermute.Available() {
		return r.wintermute, nil
	}
	return nil, fmt.Errorf("%w: set a Wintermute server URL and client token in Settings", ErrNotConfigured)
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
	return r.AskStream(ctx, req, nil)
}

// AskStream is Ask with the answer's text streamed to onText (see the
// package's AskStream). A nil onText is Ask.
func (r *Router) AskStream(ctx context.Context, req Request, onText func(string)) (Response, error) {
	provider, err := r.Selected()
	if err != nil {
		return Response{}, err
	}

	var resp Response
	if onText == nil {
		resp, err = provider.Ask(ctx, req)
	} else {
		resp, err = AskStream(ctx, provider, req, onText)
	}
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

// Probe runs a connection test against the Wintermute server.
func (r *Router) Probe(ctx context.Context) Probe {
	if r.wintermute == nil {
		return Probe{Detail: "no Wintermute provider configured"}
	}
	return r.wintermute.Probe(ctx)
}

// Status summarises the harness for the Settings page.
type Status struct {
	Active              string `json:"active"`
	Detail              string `json:"detail"`
	WintermuteAvailable bool   `json:"wintermute_available"`
}

// Status describes the current routing state without making a request.
func (r *Router) Status() Status {
	st := Status{WintermuteAvailable: r.wintermute != nil && r.wintermute.Available()}
	provider, err := r.Selected()
	if err != nil {
		st.Detail = err.Error()
		return st
	}
	st.Active = provider.Name()
	st.Detail = provider.Describe()
	return st
}
