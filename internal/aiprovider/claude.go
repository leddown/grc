package aiprovider

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultClaudeModel is used when neither the request nor the configuration
// names one.
const DefaultClaudeModel = "claude-opus-5"

// defaultMaxTokens bounds an answer when the caller does not.
const defaultMaxTokens = 4096

// Claude answers questions through api.anthropic.com.
type Claude struct {
	// keyFunc is consulted per request so a key saved in the Settings page
	// takes effect without a restart.
	keyFunc func() string
	model   string
	baseURL string

	// The SDK client is rebuilt only when the resolved key changes, so the
	// common path does not construct one per request.
	mu     sync.Mutex
	api    anthropic.Client
	apiKey string
}

// NewClaude returns a Claude provider. keyFunc is required; model may be empty,
// in which case DefaultClaudeModel is used.
func NewClaude(keyFunc func() string, model string) *Claude {
	if model == "" {
		model = DefaultClaudeModel
	}
	return &Claude{keyFunc: keyFunc, model: model}
}

// WithBaseURL overrides the API origin, for a caller that routes through a
// proxy. The caller is responsible for validating the value — this package
// does not decide which origins are acceptable.
func (c *Claude) WithBaseURL(baseURL string) *Claude {
	c.baseURL = strings.TrimSpace(baseURL)
	return c
}

func (c *Claude) Name() string { return NameClaude }

func (c *Claude) Available() bool { return c.key() != "" }

func (c *Claude) Describe() string {
	if !c.Available() {
		return "Claude: no API key"
	}
	return "Claude: " + c.model
}

func (c *Claude) key() string {
	if c.keyFunc == nil {
		return ""
	}
	return strings.TrimSpace(c.keyFunc())
}

func (c *Claude) client(key string) anthropic.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.apiKey != key {
		opts := []option.RequestOption{option.WithAPIKey(key)}
		if c.baseURL != "" {
			opts = append(opts, option.WithBaseURL(c.baseURL))
		}
		c.api = anthropic.NewClient(opts...)
		c.apiKey = key
	}
	return c.api
}

// Ask sends one question and returns the answer.
func (c *Claude) Ask(ctx context.Context, req Request) (Response, error) {
	key := c.key()
	if key == "" {
		return Response{}, ErrNotConfigured
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
	}
	if system := strings.TrimSpace(req.System); system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	// Bound to a variable first: Messages.New has a pointer receiver, so it
	// cannot be called on the value a function returns.
	api := c.client(key)
	msg, err := api.Messages.New(ctx, params)
	if err != nil {
		return Response{}, fmt.Errorf("claude request: %w", err)
	}

	usage := Usage{
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
	}

	// stop_reason before content: a refusal is a successful response with
	// empty or partial content, so reading content first misreads it.
	if msg.StopReason == anthropic.StopReasonRefusal {
		return Response{
			Provider: NameClaude, Model: model, Usage: usage, Refused: true,
		}, nil
	}

	var text strings.Builder
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	answer := strings.TrimSpace(text.String())
	if answer == "" {
		return Response{}, errNoAnswer
	}

	return Response{
		Text:     answer,
		Provider: NameClaude,
		Model:    model,
		Usage:    usage,
	}, nil
}
