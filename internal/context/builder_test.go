package context

import (
	stdcontext "context"
	"fmt"
	"strings"
	"testing"

	"neo-code/internal/provider"
)

func TestDefaultBuilderBuild(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	input := BuildInput{
		Messages: []provider.Message{
			{Role: "user", Content: "hello"},
		},
		Metadata: testMetadata(t.TempDir()),
	}

	got, err := builder.Build(stdcontext.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got.SystemPrompt == "" {
		t.Fatalf("expected non-empty system prompt")
	}
	if !strings.Contains(got.SystemPrompt, defaultSystemPrompt()) {
		t.Fatalf("expected default prompt to remain in composed prompt")
	}
	if !strings.Contains(got.SystemPrompt, "## System State") {
		t.Fatalf("expected system state section in composed prompt")
	}
	if strings.Contains(got.SystemPrompt, "## Project Rules") {
		t.Fatalf("did not expect project rules section without AGENTS.md")
	}
	if !strings.Contains(got.SystemPrompt, input.Metadata.Workdir) {
		t.Fatalf("expected workdir in system state section")
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}
	if &got.Messages[0] == &input.Messages[0] {
		t.Fatalf("expected messages slice to be cloned")
	}
}

func TestDefaultBuilderBuildHonorsCancellation(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	cancel()

	_, err := builder.Build(ctx, BuildInput{})
	if err != stdcontext.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestTrimMessagesPreservesToolPairs(t *testing.T) {
	t.Parallel()

	messages := make([]provider.Message, 0, maxContextTurns+4)
	for i := 0; i < 8; i++ {
		messages = append(messages, provider.Message{Role: "user", Content: fmt.Sprintf("u-%d", i)})
	}
	messages = append(messages,
		provider.Message{
			Role: "assistant",
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_edit", Arguments: "{}"},
			},
		},
		provider.Message{Role: "tool", ToolCallID: "call-1", Content: "tool-result"},
		provider.Message{Role: "assistant", Content: "after-tool"},
		provider.Message{Role: "user", Content: "latest"},
	)

	trimmed := trimMessages(messages)
	if len(trimmed) > len(messages) {
		t.Fatalf("trimmed messages should not grow")
	}

	foundAssistantToolCall := false
	foundToolResult := false
	for _, message := range trimmed {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			foundAssistantToolCall = true
		}
		if message.Role == "tool" && message.ToolCallID == "call-1" {
			foundToolResult = true
		}
	}
	if foundAssistantToolCall != foundToolResult {
		t.Fatalf("expected tool call and tool result to be preserved together, got %+v", trimmed)
	}
}

func TestTrimMessagesBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []provider.Message
		wantLen int
		assert  func(t *testing.T, original []provider.Message, trimmed []provider.Message)
	}{
		{
			name: "within max turns returns full cloned slice",
			input: []provider.Message{
				{Role: "user", Content: "one"},
				{Role: "assistant", Content: "two"},
			},
			wantLen: 2,
			assert: func(t *testing.T, original []provider.Message, trimmed []provider.Message) {
				t.Helper()
				if &trimmed[0] == &original[0] {
					t.Fatalf("expected trimmed slice to be cloned")
				}
			},
		},
		{
			name: "long message list with limited spans keeps full history",
			input: func() []provider.Message {
				messages := make([]provider.Message, 0, maxContextTurns+3)
				for i := 0; i < maxContextTurns-1; i++ {
					messages = append(messages, provider.Message{Role: "user", Content: fmt.Sprintf("u-%d", i)})
				}
				messages = append(messages,
					provider.Message{
						Role: "assistant",
						ToolCalls: []provider.ToolCall{
							{ID: "call-1", Name: "filesystem_edit", Arguments: "{}"},
						},
					},
					provider.Message{Role: "tool", ToolCallID: "call-1", Content: "tool-1"},
					provider.Message{Role: "tool", ToolCallID: "call-1", Content: "tool-2"},
				)
				return messages
			}(),
			wantLen: maxContextTurns + 2,
			assert: func(t *testing.T, original []provider.Message, trimmed []provider.Message) {
				t.Helper()
				if len(trimmed) != len(original) {
					t.Fatalf("expected full history to remain, got %d want %d", len(trimmed), len(original))
				}
			},
		},
		{
			name: "message count beyond limit trims by span count",
			input: func() []provider.Message {
				messages := make([]provider.Message, 0, maxContextTurns+5)
				for i := 0; i < maxContextTurns+1; i++ {
					messages = append(messages, provider.Message{Role: "user", Content: fmt.Sprintf("u-%d", i)})
				}
				messages = append(messages,
					provider.Message{
						Role: "assistant",
						ToolCalls: []provider.ToolCall{
							{ID: "call-2", Name: "filesystem_edit", Arguments: "{}"},
						},
					},
					provider.Message{Role: "tool", ToolCallID: "call-2", Content: "tool-result"},
				)
				return messages
			}(),
			wantLen: maxContextTurns + 1,
			assert: func(t *testing.T, original []provider.Message, trimmed []provider.Message) {
				t.Helper()
				if trimmed[0].Content != "u-2" {
					t.Fatalf("expected oldest spans to be removed, got first message %+v", trimmed[0])
				}
				if trimmed[len(trimmed)-1].Role != "tool" {
					t.Fatalf("expected trailing tool result to remain, got %+v", trimmed[len(trimmed)-1])
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			trimmed := trimMessages(tt.input)
			if len(trimmed) != tt.wantLen {
				t.Fatalf("expected len %d, got %d", tt.wantLen, len(trimmed))
			}
			if tt.assert != nil {
				tt.assert(t, tt.input, trimmed)
			}
		})
	}
}
