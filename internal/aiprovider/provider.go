// Package aiprovider is the harness every AI field in the app asks questions
// through. It exists so a question can be served by a model on the local
// network as readily as by the cloud, without each feature growing its own
// client, its own credential handling and its own idea of what a model is.
//
// Two providers ship: Claude, which talks to api.anthropic.com, and Wintermute,
// which talks to a wintermuted server on the network that in turn routes to
// self-hosted models (llama.cpp, Ollama, vLLM) or on to Claude. A Router picks
// between them per the operator's Settings choice.
package aiprovider

import (
	"context"
	"errors"
	"fmt"
)

// Provider names, matching the values stored in settings.PrefAIProvider.
const (
	NameClaude     = "claude"
	NameWintermute = "wintermute"
)

// ErrNotConfigured reports that a provider has no usable configuration — no
// credential, or no server URL. It is not a failure of the request.
var ErrNotConfigured = errors.New("provider is not configured")

// Roles for a Message in a Request's History.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one earlier turn of a conversation. Role is RoleUser or
// RoleAssistant.
type Message struct {
	Role string
	Text string
}

// Request is one question. Most AI fields in this app ask a single
// self-contained question and use the answer, so History is optional and
// usually empty; the chat surfaces set it so a follow-up question means what it
// says. The harness holds no conversation state of its own — a caller that
// wants continuity either sends the transcript back on each turn (History) or,
// where the provider keeps the transcript itself, hands back the SessionID the
// previous answer carried.
type Request struct {
	// System is the instruction that frames the task. The Wintermute provider
	// folds it into the message text, because wintermuted derives its own
	// system prompt from its configuration.
	System string
	// History is the conversation so far, oldest first, excluding Prompt. The
	// caller is responsible for bounding it: nothing here trims a transcript
	// that has outgrown the model's context window.
	History []Message
	// Prompt is the question itself.
	Prompt string
	// SessionID continues a conversation a provider is itself holding, as
	// returned by a previous Response. It is opaque and provider-specific;
	// Claude has no such thing and ignores it. When it is set, the provider
	// already has the transcript and History is not resent.
	SessionID string
	// Model optionally overrides the provider's configured model.
	Model string
	// MaxTokens bounds the answer. Zero means the provider's default.
	MaxTokens int
}

// Usage is the token accounting for one answer, for the shared ai_usage_log.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Response is one answer, annotated with what actually served it.
type Response struct {
	Text string
	// Provider names the provider that answered.
	Provider string
	// Backend names the Wintermute backend that served the turn, when the
	// answer came from there. A wintermuted server retries against its
	// configured fallback when a backend fails, so what served a turn is not
	// always what was asked for — recording it keeps the usage log honest.
	Backend string
	// Model is the model that produced the answer.
	Model string
	// SessionID identifies the conversation this turn belongs to, for providers
	// that keep the transcript themselves. Passing it back on the next Request
	// continues that conversation. Empty for providers that do not.
	SessionID string
	Usage     Usage
	// Refused reports that the provider's safety classifiers declined the
	// request. This is a successful HTTP 200 with empty or partial content, so
	// a caller that reads Text without checking this misreads a refusal as a
	// malformed answer.
	Refused bool
}

// Provider answers questions.
type Provider interface {
	// Name identifies the provider.
	Name() string
	// Available reports whether the provider could serve a request right now.
	// It is cheap and does no network I/O — configuration only.
	Available() bool
	// Describe renders the provider's configuration for the Settings page. It
	// never includes credentials.
	Describe() string
	// Ask answers one question.
	Ask(ctx context.Context, req Request) (Response, error)
}

// Probe is what a connection test reports back.
type Probe struct {
	OK bool `json:"ok"`
	// Detail carries the failure reason, or a short success summary.
	Detail string `json:"detail"`
	// Backends lists the backends the server advertises, so an operator can
	// see which local models are reachable and copy a name into the settings.
	Backends []string `json:"backends,omitempty"`
	// DefaultBackend and Fallback are the server's own routing defaults.
	DefaultBackend string `json:"default_backend,omitempty"`
	Fallback       string `json:"fallback,omitempty"`
}

// Prober is implemented by providers that can be reached for a liveness and
// capability check. Claude does not implement it: a credential check would be
// a billable request, and the useful discovery here is which local models
// exist.
type Prober interface {
	Probe(ctx context.Context) Probe
}

// errNoAnswer is returned when a provider completes without producing text.
var errNoAnswer = fmt.Errorf("provider returned an empty answer")
