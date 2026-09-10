package aiprovider

import (
	"strings"
	"testing"
)

// flatten renders the assembled messages as "role: text" lines, which is all
// these tests care about.
func flatten(t *testing.T, history []Message, prompt string) []string {
	t.Helper()
	out := []string{}
	for _, msg := range claudeMessages(history, prompt) {
		var text strings.Builder
		for _, block := range msg.Content {
			if block.OfText != nil {
				text.WriteString(block.OfText.Text)
			}
		}
		out = append(out, string(msg.Role)+": "+text.String())
	}
	return out
}

// The model is resolved per question, so a model saved in the Settings page
// takes effect without a restart, and clearing it restores the default.
func TestClaudeModelFuncIsResolvedPerQuestion(t *testing.T) {
	configured := "claude-sonnet-5"
	c := NewClaude(func() string { return "sk-ant-key" }, "").
		WithModelFunc(func() string { return configured })
	if got := c.Describe(); got != "Claude: claude-sonnet-5" {
		t.Errorf("Describe() = %q, want the configured model", got)
	}
	configured = "  "
	if got := c.Describe(); got != "Claude: "+DefaultClaudeModel {
		t.Errorf("Describe() with the setting cleared = %q, want the default", got)
	}
}

func TestClaudeMessages(t *testing.T) {
	tests := []struct {
		name    string
		history []Message
		prompt  string
		want    []string
	}{
		{
			name:   "no history is one user turn",
			prompt: "why?",
			want:   []string{"user: why?"},
		},
		{
			name: "a conversation is replayed in order",
			history: []Message{
				{Role: RoleUser, Text: "name a control family"},
				{Role: RoleAssistant, Text: "AC — Access Control"},
			},
			prompt: "and the second?",
			want: []string{
				"user: name a control family",
				"assistant: AC — Access Control",
				"user: and the second?",
			},
		},
		{
			// A turn that errored leaves a question with no answer after it,
			// and the API rejects two user turns in a row.
			name: "consecutive same-role turns are joined",
			history: []Message{
				{Role: RoleUser, Text: "first"},
				{Role: RoleUser, Text: "second"},
			},
			prompt: "third",
			want:   []string{"user: first\n\nsecond\n\nthird"},
		},
		{
			// The API requires the first message to be a user turn.
			name:    "a leading assistant turn is dropped",
			history: []Message{{Role: RoleAssistant, Text: "unprompted"}},
			prompt:  "hello",
			want:    []string{"user: hello"},
		},
		{
			name: "blank turns are dropped, unknown roles count as user",
			history: []Message{
				{Role: RoleUser, Text: "kept"},
				{Role: RoleAssistant, Text: "   "},
				{Role: "system", Text: "smuggled"},
			},
			prompt: "next",
			want:   []string{"user: kept\n\nsmuggled\n\nnext"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flatten(t, tt.history, tt.prompt)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d messages %q, want %d %q", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("message %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
