package aiprovider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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
	// modelFunc, when set, is consulted per request for the same reason as
	// keyFunc; an empty result falls back to model.
	modelFunc func() string
	baseURL   string

	// The SDK client is rebuilt only when the resolved key changes, so the
	// common path does not construct one per request.
	mu     sync.Mutex
	api    anthropic.Client
	apiKey string
	caps   map[string]capability
}

// NewClaude returns a Claude provider. keyFunc is required; model may be empty,
// in which case DefaultClaudeModel is used.
func NewClaude(keyFunc func() string, model string) *Claude {
	if model == "" {
		model = DefaultClaudeModel
	}
	return &Claude{keyFunc: keyFunc, model: model}
}

// WithModelFunc resolves the model per request, so a model chosen in the
// Settings page takes effect without a restart and clearing it restores the
// model given to NewClaude.
func (c *Claude) WithModelFunc(fn func() string) *Claude {
	c.modelFunc = fn
	return c
}

func (c *Claude) resolvedModel() string {
	if c.modelFunc != nil {
		if model := strings.TrimSpace(c.modelFunc()); model != "" {
			return model
		}
	}
	return c.model
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
	return "Claude: " + c.resolvedModel()
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

// ClaudeModel is one model the Anthropic API offers, as a page needs it to
// present a choice.
type ClaudeModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	// MaxInputTokens is the context window and MaxTokens the answer ceiling.
	// They are what distinguishes two models on a list where the ids alone say
	// little about which one a long document will fit in.
	MaxInputTokens int64 `json:"max_input_tokens,omitempty"`
	MaxTokens      int64 `json:"max_tokens,omitempty"`
}

// modelListLimit asks for the whole list in one page. The API's default is 20,
// which silently truncates a catalog that is longer than that.
const modelListLimit = 1000

// Models lists the models this key can address, newest first as the API orders
// them.
//
// This is a metadata call, not a question: nothing here is billed as tokens,
// and a model id typed by hand is a request that fails with a 404 at ask time
// rather than a choice that was never available.
func (c *Claude) Models(ctx context.Context) ([]ClaudeModel, error) {
	key := c.key()
	if key == "" {
		return nil, ErrNotConfigured
	}

	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()

	api := c.client(key)
	pager := api.Models.ListAutoPaging(ctx, anthropic.ModelListParams{
		Limit: anthropic.Int(modelListLimit),
	})
	models := []ClaudeModel{}
	for pager.Next() {
		info := pager.Current()
		models = append(models, ClaudeModel{
			ID:             info.ID,
			DisplayName:    info.DisplayName,
			MaxInputTokens: info.MaxInputTokens,
			MaxTokens:      info.MaxTokens,
		})
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("list Claude models: %w", err)
	}
	return models, nil
}

// claudeMessages turns a transcript and the new question into the message list
// the Messages API accepts.
//
// The API requires the roles to alternate and the first message to be a user
// turn, which a transcript from a chat page does not always satisfy: a turn
// that errored leaves a question with no answer after it, and a cleared-then-
// resent history can start on an assistant turn. Rather than let that surface
// as an HTTP 400, consecutive same-role turns are joined and a leading
// assistant turn is dropped.
func claudeMessages(history []Message, prompt string) []anthropic.MessageParam {
	type turn struct {
		role string
		text []string
	}
	turns := make([]turn, 0, len(history)+1)

	add := func(role, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if role != RoleAssistant {
			role = RoleUser
		}
		if len(turns) == 0 && role == RoleAssistant {
			return
		}
		if n := len(turns); n > 0 && turns[n-1].role == role {
			turns[n-1].text = append(turns[n-1].text, text)
			return
		}
		turns = append(turns, turn{role: role, text: []string{text}})
	}

	for _, msg := range history {
		add(msg.Role, msg.Text)
	}
	add(RoleUser, prompt)

	messages := make([]anthropic.MessageParam, 0, len(turns))
	for _, t := range turns {
		block := anthropic.NewTextBlock(strings.Join(t.text, "\n\n"))
		if t.role == RoleAssistant {
			messages = append(messages, anthropic.NewAssistantMessage(block))
			continue
		}
		messages = append(messages, anthropic.NewUserMessage(block))
	}
	return messages
}

// Ask sends one question and returns the answer.
func (c *Claude) Ask(ctx context.Context, req Request) (Response, error) {
	key := c.key()
	if key == "" {
		return Response{}, ErrNotConfigured
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.resolvedModel()
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		Messages:  claudeMessages(req.History, req.Prompt),
	}
	if system := strings.TrimSpace(req.System); system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}
	if req.OutputSchema != nil {
		params.OutputConfig = anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: req.OutputSchema},
		}
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
			Provider: NameClaude, Model: model, Usage: usage, Refused: true, StopReason: StopRefusal,
		}, nil
	}

	var text strings.Builder
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	answer := strings.TrimSpace(text.String())
	stop := string(msg.StopReason)
	if answer == "" && stop != StopMaxTokens {
		return Response{}, errNoAnswer
	}

	return Response{
		Text:       answer,
		Provider:   NameClaude,
		Model:      model,
		Usage:      usage,
		StopReason: stop,
	}, nil
}

// capabilityTTL is how long a model's reported capabilities are trusted before
// they are asked for again.
const capabilityTTL = time.Hour

type capability struct {
	structured bool
	at         time.Time
}

// SupportsStructuredOutputs reports whether a model accepts output_config
// schemas, from the Models API's own capability record rather than a list in
// this code, which would go stale with the next model. It is a metadata call,
// not billed as tokens, and the answer is cached per model.
func (c *Claude) SupportsStructuredOutputs(ctx context.Context, model string) (bool, error) {
	key := c.key()
	if key == "" {
		return false, ErrNotConfigured
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = c.resolvedModel()
	}
	c.mu.Lock()
	if cached, ok := c.caps[model]; ok && time.Since(cached.at) < capabilityTTL {
		c.mu.Unlock()
		return cached.structured, nil
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	api := c.client(key)
	info, err := api.Models.Get(ctx, model, anthropic.ModelGetParams{})
	if err != nil {
		return false, fmt.Errorf("look up Claude model %s: %w", model, err)
	}
	supported := info.Capabilities.StructuredOutputs.Supported
	c.mu.Lock()
	if c.caps == nil {
		c.caps = map[string]capability{}
	}
	c.caps[model] = capability{structured: supported, at: time.Now()}
	c.mu.Unlock()
	return supported, nil
}

// ResolvedModel is the model a request that names none is sent to.
func (c *Claude) ResolvedModel() string { return c.resolvedModel() }
