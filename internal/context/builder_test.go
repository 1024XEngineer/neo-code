package context

import (
	stdcontext "context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/context/internalcompact"
	"neo-code/internal/provider"
)

type stubPromptSectionSource struct {
	sections []promptSection
	err      error
}

func (s stubPromptSectionSource) Sections(ctx stdcontext.Context, input BuildInput) ([]promptSection, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]promptSection(nil), s.sections...), nil
}

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
	if !strings.Contains(got.SystemPrompt, "## Agent Identity") {
		t.Fatalf("expected core prompt sections to be included")
	}
	if !strings.Contains(got.SystemPrompt, "## System State") {
		t.Fatalf("expected system state section in composed prompt")
	}
	if strings.Contains(got.SystemPrompt, "## Project Rules") {
		t.Fatalf("did not expect project rules section without AGENTS.md")
	}
	if strings.Contains(got.SystemPrompt, "\n\n\n") {
		t.Fatalf("did not expect repeated blank lines in composed prompt")
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

func TestDefaultBuilderBuildComposesPromptSectionsInOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, projectRuleFileName), []byte("project-rules"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	builder := NewBuilder()
	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: []provider.Message{{Role: "user", Content: "hello"}},
		Metadata: testMetadata(root),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	identityIndex := strings.Index(got.SystemPrompt, "## Agent Identity")
	rulesIndex := strings.Index(got.SystemPrompt, "## Project Rules")
	stateIndex := strings.Index(got.SystemPrompt, "## System State")
	if identityIndex < 0 || rulesIndex < 0 || stateIndex < 0 {
		t.Fatalf("expected all prompt sections, got %q", got.SystemPrompt)
	}
	if !(identityIndex < rulesIndex && rulesIndex < stateIndex) {
		t.Fatalf("expected section order core -> project rules -> system state, got %q", got.SystemPrompt)
	}
}

func TestDefaultBuilderBuildUsesSpanTrimPolicyWhenTrimPolicyIsUnset(t *testing.T) {
	t.Parallel()

	messages := make([]provider.Message, 0, maxRetainedMessageSpans+2)
	for i := 0; i < maxRetainedMessageSpans+2; i++ {
		messages = append(messages, provider.Message{
			Role:    provider.RoleUser,
			Content: fmt.Sprintf("u-%d", i),
		})
	}

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{title: "Stub", content: "body"}}},
		},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{Messages: messages})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(got.Messages) != maxRetainedMessageSpans {
		t.Fatalf("expected %d retained messages, got %d", maxRetainedMessageSpans, len(got.Messages))
	}
	if got.Messages[0].Content != "u-2" {
		t.Fatalf("expected oldest messages to be trimmed, got first message %+v", got.Messages[0])
	}
}

func TestDefaultBuilderBuildReturnsPromptSourceError(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{err: fmt.Errorf("source failed")},
		},
	}

	_, err := builder.Build(stdcontext.Background(), BuildInput{})
	if err == nil || !strings.Contains(err.Error(), "source failed") {
		t.Fatalf("expected source error, got %v", err)
	}
}

func TestTrimMessagesPreservesToolPairs(t *testing.T) {
	t.Parallel()

	messages := make([]provider.Message, 0, maxRetainedMessageSpans+4)
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

func TestTrimMessagesProtectsLatestExplicitUserInstructionTail(t *testing.T) {
	t.Parallel()

	messages := make([]provider.Message, 0, maxRetainedMessageSpans+5)
	for i := 0; i < 2; i++ {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("old-%d", i)})
	}
	messages = append(messages,
		provider.Message{Role: provider.RoleUser, Content: "latest explicit instruction"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-1"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-2"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-3"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-4"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-5"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-6"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-7"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-8"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-9"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-10"},
		provider.Message{Role: provider.RoleAssistant, Content: "follow-up-11"},
	)

	trimmed := trimMessages(messages)
	if trimmed[0].Role != provider.RoleUser || trimmed[0].Content != "latest explicit instruction" {
		t.Fatalf("expected protected tail to keep latest explicit user instruction, got %+v", trimmed[0])
	}
	if len(trimmed) != 12 {
		t.Fatalf("expected protected tail to keep latest instruction and full assistant tail, got %d messages", len(trimmed))
	}
}

func TestTrimMessagesUsesSharedSpanModel(t *testing.T) {
	t.Parallel()

	messages := make([]provider.Message, 0, maxRetainedMessageSpans+6)
	for i := 0; i < 3; i++ {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("u-%d", i)})
	}
	messages = append(messages,
		provider.Message{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-2", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		provider.Message{Role: provider.RoleTool, ToolCallID: "call-2", Content: "tool-result"},
		provider.Message{Role: provider.RoleAssistant, Content: "after tool"},
		provider.Message{Role: provider.RoleUser, Content: "u-4"},
		provider.Message{Role: provider.RoleAssistant, Content: "a-5"},
		provider.Message{Role: provider.RoleUser, Content: "u-6"},
		provider.Message{Role: provider.RoleAssistant, Content: "a-7"},
		provider.Message{Role: provider.RoleUser, Content: "u-8"},
		provider.Message{Role: provider.RoleAssistant, Content: "a-9"},
		provider.Message{Role: provider.RoleUser, Content: "u-10"},
		provider.Message{Role: provider.RoleAssistant, Content: "a-11"},
	)

	spans := internalcompact.BuildMessageSpans(messages)
	trimmed := trimMessages(messages)

	start := spans[len(spans)-maxRetainedMessageSpans].Start
	if len(trimmed) == 0 || trimmed[0].Content != messages[start].Content {
		t.Fatalf("expected trim to start from shared span boundary %d, got %+v", start, trimmed)
	}
	if trimmed[0].Role != provider.RoleAssistant || len(trimmed[0].ToolCalls) != 1 {
		t.Fatalf("expected trim to keep whole tool block at shared boundary, got %+v", trimmed[0])
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
				messages := make([]provider.Message, 0, maxRetainedMessageSpans+3)
				for i := 0; i < maxRetainedMessageSpans-1; i++ {
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
			wantLen: maxRetainedMessageSpans + 2,
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
				messages := make([]provider.Message, 0, maxRetainedMessageSpans+5)
				for i := 0; i < maxRetainedMessageSpans+1; i++ {
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
			wantLen: maxRetainedMessageSpans + 1,
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
